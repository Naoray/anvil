package wtstate

import (
	"fmt"
	"sort"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
)

const processesKey = "processes"

// RunLockFile serializes managed-process start and stop per worktree. In the
// lock order it comes after resource locks and before the local state lock.
const RunLockFile = "anvil-run.lock"

// AcquireRunLock blocks until this process may start or stop the worktree's
// managed processes. Holding it across check, launch and record keeps
// concurrent starts from launching the same process twice.
func AcquireRunLock(worktreePath string) (*lockfile.Lock, error) {
	return lockfile.Acquire(config.WorktreeLockPath(worktreePath, RunLockFile))
}

// ProcessRecord is a managed process Anvil started for this worktree. PID
// alone is never trusted: Identity is the kernel start identity captured at
// launch, and WorktreeID ties the record to its owner.
type ProcessRecord struct {
	Name       string    `yaml:"name" json:"name"`
	Command    string    `yaml:"command" json:"command"`
	Dir        string    `yaml:"dir" json:"dir"`
	PID        int       `yaml:"pid" json:"pid"`
	Identity   string    `yaml:"identity" json:"-"`
	LogPath    string    `yaml:"log" json:"log"`
	WorktreeID string    `yaml:"worktree_id" json:"worktree_id"`
	StartedAt  time.Time `yaml:"started_at" json:"started_at"`
}

func decodeProcesses(doc map[string]any) (map[string]ProcessRecord, error) {
	records := map[string]ProcessRecord{}
	raw, ok := doc[processesKey]
	if !ok {
		return records, nil
	}
	if err := config.DecodeSection(raw, &records); err != nil {
		return nil, fmt.Errorf("malformed process records: %w", err)
	}
	return records, nil
}

// ReadProcesses returns process records sorted by name.
func ReadProcesses(worktreePath string) ([]ProcessRecord, error) {
	doc, err := config.ReadLocalStateMap(worktreePath)
	if err != nil {
		return nil, err
	}
	records, err := decodeProcesses(doc)
	if err != nil {
		return nil, err
	}
	out := make([]ProcessRecord, 0, len(records))
	for _, record := range records {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func updateProcesses(worktreePath string, fn func(map[string]ProcessRecord) error) error {
	return config.UpdateLocalState(worktreePath, func(doc map[string]any) error {
		records, err := decodeProcesses(doc)
		if err != nil {
			return err
		}
		if err := fn(records); err != nil {
			return err
		}
		if len(records) == 0 {
			delete(doc, processesKey)
			return nil
		}
		encoded, err := config.EncodeSection(records)
		if err != nil {
			return err
		}
		doc[processesKey] = encoded
		return nil
	})
}

// PutProcess records a started process.
func PutProcess(worktreePath string, record ProcessRecord) error {
	return updateProcesses(worktreePath, func(records map[string]ProcessRecord) error {
		records[record.Name] = record
		return nil
	})
}

// RemoveProcess drops a record only if it still describes the same launch,
// so a concurrent restart is never forgotten.
func RemoveProcess(worktreePath string, record ProcessRecord) error {
	return updateProcesses(worktreePath, func(records map[string]ProcessRecord) error {
		if current, ok := records[record.Name]; ok && current.PID == record.PID && current.Identity == record.Identity {
			delete(records, record.Name)
		}
		return nil
	})
}
