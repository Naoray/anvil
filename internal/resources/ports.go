package resources

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/workenv"
	"github.com/naoray/anvil/internal/wtstate"
)

// PortRangeEnv overrides the allocation range ("LOW-HIGH").
const PortRangeEnv = "ANVIL_PORT_RANGE"

const (
	defaultPortLow  = 20000
	defaultPortHigh = 29999
	portsFile       = "ports.json"
	portsLockFile   = "ports.lock"
)

// Reservation records one port owned by one worktree identity.
type Reservation struct {
	Port         int       `json:"port"`
	Name         string    `json:"name"`
	WorktreeID   string    `json:"worktree_id"`
	WorktreePath string    `json:"worktree_path"`
	ReservedAt   time.Time `json:"reserved_at"`
}

type registry struct {
	Version      int           `json:"version"`
	Reservations []Reservation `json:"reservations"`
}

// PortState is the observed state of a reservation. A reservation is an
// advisory agreement among Anvil processes: it cannot stop an unrelated
// program from binding the port between allocation and the service starting.
type PortState struct {
	Reservation
	// Listening is true when something currently accepts connections on
	// 127.0.0.1:port. It may be this worktree's own service.
	Listening bool `json:"listening"`
}

// OwnerCheck reports whether a reservation's owner is still a live worktree.
type OwnerCheck func(Reservation) bool

// LiveOwner treats a reservation as active while the owner path holds the
// same Anvil identity. Removed or re-identified worktrees are stale.
func LiveOwner(r Reservation) bool {
	identity, status, err := wtstate.ReadIdentity(r.WorktreePath)
	if err != nil || identity == nil {
		return false
	}
	return identity.ID == r.WorktreeID && (status == wtstate.IdentityOK || status == wtstate.IdentityMoved)
}

// Ports manages the machine-wide port registry under root.
type Ports struct {
	Root  string
	Low   int
	High  int
	Owner OwnerCheck
	// Bindable probes whether a port can currently be bound.
	Bindable func(port int) bool
}

// NewPorts returns a registry using the configured range.
func NewPorts(root string) (*Ports, error) {
	low, high, err := portRange()
	if err != nil {
		return nil, err
	}
	return &Ports{Root: root, Low: low, High: high, Owner: LiveOwner, Bindable: canBind}, nil
}

func portRange() (int, int, error) {
	value := os.Getenv(PortRangeEnv)
	if value == "" {
		return defaultPortLow, defaultPortHigh, nil
	}
	lowText, highText, ok := strings.Cut(value, "-")
	low, lowErr := strconv.Atoi(strings.TrimSpace(lowText))
	high, highErr := strconv.Atoi(strings.TrimSpace(highText))
	if !ok || lowErr != nil || highErr != nil || low < 1024 || high > 65535 || low > high {
		return 0, 0, fmt.Errorf("%s must be LOW-HIGH within 1024-65535, got %q", PortRangeEnv, value)
	}
	return low, high, nil
}

func canBind(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	return listener.Close() == nil
}

func isListening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	return conn.Close() == nil
}

func (p *Ports) path() string { return filepath.Join(p.Root, portsFile) }

func (p *Ports) load() (registry, error) {
	content, err := os.ReadFile(p.path())
	if errors.Is(err, os.ErrNotExist) {
		return registry{Version: 1}, nil
	}
	if err != nil {
		return registry{}, fmt.Errorf("reading port registry: %w", err)
	}
	var reg registry
	if err := json.Unmarshal(content, &reg); err != nil {
		return registry{}, fmt.Errorf("malformed port registry %s: %w", p.path(), err)
	}
	return reg, nil
}

func (p *Ports) save(reg registry) error {
	sort.Slice(reg.Reservations, func(i, j int) bool { return reg.Reservations[i].Port < reg.Reservations[j].Port })
	content, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Root, 0o700); err != nil {
		return err
	}
	return writeAtomic(p.path(), content)
}

func (p *Ports) withLock(fn func(*registry) error) (err error) {
	lock, err := lockfile.Acquire(filepath.Join(p.Root, portsLockFile))
	if err != nil {
		return fmt.Errorf("locking port registry: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	reg, err := p.load()
	if err != nil {
		return err
	}
	if err := fn(&reg); err != nil {
		return err
	}
	return p.save(reg)
}

// preferred spreads worktrees across the range deterministically so a
// worktree tends to receive the same port even after its record is lost.
func (p *Ports) preferred(worktreeID, name string) int {
	sum := sha256.Sum256([]byte(worktreeID + "\x00" + name))
	span := p.High - p.Low + 1
	return p.Low + int(binary.BigEndian.Uint32(sum[:4])%uint32(span))
}

// Allocate reserves every requested name for the worktree. Existing
// reservations are kept (stable ports); stale reservations of removed
// worktrees are reclaimed; names no longer requested are released. New
// ports must be unreserved and currently bindable on 127.0.0.1.
func (p *Ports) Allocate(worktreeID, worktreePath string, names []string) ([]Reservation, error) {
	var result []Reservation
	err := p.withLock(func(reg *registry) error {
		wanted := map[string]bool{}
		for _, name := range names {
			wanted[name] = true
		}
		kept := reg.Reservations[:0]
		taken := map[int]bool{}
		mine := map[string]Reservation{}
		for _, r := range reg.Reservations {
			switch {
			case r.WorktreeID == worktreeID:
				if !wanted[r.Name] {
					continue
				}
				r.WorktreePath = worktreePath
				mine[r.Name] = r
			case !p.Owner(r):
				continue // stale: owner removed or re-identified
			}
			kept = append(kept, r)
			taken[r.Port] = true
		}
		reg.Reservations = kept
		for _, name := range names {
			if r, ok := mine[name]; ok {
				result = append(result, r)
				continue
			}
			port, err := p.pick(worktreeID, name, taken)
			if err != nil {
				return err
			}
			r := Reservation{Port: port, Name: name, WorktreeID: worktreeID, WorktreePath: worktreePath, ReservedAt: time.Now().UTC().Truncate(time.Second)}
			reg.Reservations = append(reg.Reservations, r)
			taken[port] = true
			result = append(result, r)
		}
		return nil
	})
	return result, err
}

func (p *Ports) pick(worktreeID, name string, taken map[int]bool) (int, error) {
	span := p.High - p.Low + 1
	start := p.preferred(worktreeID, name)
	for offset := 0; offset < span; offset++ {
		port := p.Low + (start-p.Low+offset)%span
		if taken[port] {
			continue
		}
		if p.Bindable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d-%d", p.Low, p.High)
}

// Release drops every reservation owned by the worktree identity.
func (p *Ports) Release(worktreeID string) ([]Reservation, error) {
	var released []Reservation
	err := p.withLock(func(reg *registry) error {
		kept := reg.Reservations[:0]
		for _, r := range reg.Reservations {
			if r.WorktreeID == worktreeID {
				released = append(released, r)
				continue
			}
			kept = append(kept, r)
		}
		reg.Reservations = kept
		return nil
	})
	return released, err
}

// Lookup returns the worktree's reservations without locking or writing.
func (p *Ports) Lookup(worktreeID string) ([]PortState, error) {
	reg, err := p.load()
	if err != nil {
		return nil, err
	}
	var out []PortState
	for _, r := range reg.Reservations {
		if r.WorktreeID == worktreeID {
			out = append(out, PortState{Reservation: r, Listening: isListening(r.Port)})
		}
	}
	return out, nil
}

// PortVariables exports reservations as ANVIL_PORT_<NAME> plus aliases.
func PortVariables(cfg *config.Config, reservations []Reservation) []workenv.Variable {
	byName := map[string]int{}
	for _, r := range reservations {
		byName[r.Name] = r.Port
	}
	var vars []workenv.Variable
	for _, port := range cfg.Resources.Ports {
		value, ok := byName[port.Name]
		if !ok {
			continue
		}
		text := strconv.Itoa(value)
		vars = append(vars, workenv.Variable{Name: "ANVIL_PORT_" + config.ResourceEnvSuffix(port.Name), Value: text, Source: workenv.SourceResource})
		if port.Env != "" {
			vars = append(vars, workenv.Variable{Name: port.Env, Value: text, Source: workenv.SourceResource})
		}
	}
	return vars
}
