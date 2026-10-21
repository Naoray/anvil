package resources

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

const idA, idB = "wt_aaaaaaaaaaaaaaaa", "wt_bbbbbbbbbbbbbbbb"

func testPorts(t *testing.T, live map[string]bool) *Ports {
	t.Helper()
	return &Ports{
		Root: t.TempDir(), Low: 41000, High: 41009,
		Owner:    func(r Reservation) bool { return live[r.WorktreeID] },
		Bindable: canBind,
	}
}

func TestPortsStableUniqueAndReleased(t *testing.T) {
	ports := testPorts(t, map[string]bool{idA: true, idB: true})
	a, err := ports.Allocate(idA, "/a", []string{"web", "api"})
	require.NoError(t, err)
	require.Len(t, a, 2)
	require.NotEqual(t, a[0].Port, a[1].Port)

	again, err := ports.Allocate(idA, "/a", []string{"web", "api"})
	require.NoError(t, err)
	require.Equal(t, a[0].Port, again[0].Port, "allocations are stable")
	require.Equal(t, a[1].Port, again[1].Port)

	b, err := ports.Allocate(idB, "/b", []string{"web"})
	require.NoError(t, err)
	require.NotContains(t, []int{a[0].Port, a[1].Port}, b[0].Port)

	shrunk, err := ports.Allocate(idA, "/a", []string{"web"})
	require.NoError(t, err)
	require.Len(t, shrunk, 1, "undeclared names are released")
	states, err := ports.Lookup(idA)
	require.NoError(t, err)
	require.Len(t, states, 1)

	released, err := ports.Release(idA)
	require.NoError(t, err)
	require.Len(t, released, 1)
	states, err = ports.Lookup(idA)
	require.NoError(t, err)
	require.Empty(t, states)
	states, err = ports.Lookup(idB)
	require.NoError(t, err)
	require.Len(t, states, 1, "other worktrees keep their reservations")
}

func TestPortsReclaimStaleAndSkipBoundPorts(t *testing.T) {
	live := map[string]bool{idA: true}
	ports := testPorts(t, live)
	a, err := ports.Allocate(idA, "/a", []string{"web"})
	require.NoError(t, err)

	// An unrelated listener occupies every other port in a 2-port range.
	ports.Low, ports.High = a[0].Port, a[0].Port+1
	other := a[0].Port + 1
	if a[0].Port == ports.High {
		other = a[0].Port - 1
		ports.Low, ports.High = other, a[0].Port
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(other))
	require.NoError(t, err)
	defer listener.Close()
	_, err = ports.Allocate(idB, "/b", []string{"web"})
	require.ErrorContains(t, err, "no free port", "reserved and bound ports are both unavailable")

	live[idA] = false // owner removed: its reservation is stale and reclaimable
	b, err := ports.Allocate(idB, "/b", []string{"web"})
	require.NoError(t, err)
	require.Equal(t, a[0].Port, b[0].Port)
	states, err := ports.Lookup(idB)
	require.NoError(t, err)
	require.False(t, states[0].Listening)
}

func TestPortsConcurrentAllocationAcrossProcesses(t *testing.T) {
	if root := os.Getenv("ANVIL_PORTS_CHILD_ROOT"); root != "" {
		ports := &Ports{Root: root, Low: 42000, High: 42999, Owner: func(Reservation) bool { return true }, Bindable: canBind}
		id := os.Getenv("ANVIL_PORTS_CHILD_ID")
		reservations, err := ports.Allocate(id, "/"+id, []string{"web", "api", "db"})
		require.NoError(t, err)
		require.Len(t, reservations, 3)
		return
	}
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestPortsConcurrentAllocationAcrossProcesses$")
			cmd.Env = append(os.Environ(), "ANVIL_PORTS_CHILD_ROOT="+root, "ANVIL_PORTS_CHILD_ID=wt_"+strings.Repeat(strconv.Itoa(i), 16))
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs[i] = fmt.Errorf("child %d: %w\n%s", i, err, out)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	ports := &Ports{Root: root}
	reg, err := ports.load()
	require.NoError(t, err)
	require.Len(t, reg.Reservations, 24)
	seen := map[int]bool{}
	for _, r := range reg.Reservations {
		require.False(t, seen[r.Port], "port %d allocated twice", r.Port)
		seen[r.Port] = true
	}
}

func TestPortVariablesAndRange(t *testing.T) {
	cfg := &config.Config{Resources: config.ResourcesConfig{Ports: []config.PortConfig{{Name: "web-ui", Env: "VITE_PORT"}, {Name: "unallocated"}}}}
	vars := PortVariables(cfg, []Reservation{{Name: "web-ui", Port: 20001}})
	require.Len(t, vars, 2)
	require.Equal(t, "ANVIL_PORT_WEB_UI", vars[0].Name)
	require.Equal(t, "20001", vars[1].Value)

	t.Setenv(PortRangeEnv, "30000-30010")
	low, high, err := portRange()
	require.NoError(t, err)
	require.Equal(t, []int{30000, 30010}, []int{low, high})
	t.Setenv(PortRangeEnv, "80-90")
	_, _, err = portRange()
	require.Error(t, err)
}

func TestDirectoriesOwnershipAndRemoval(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Resources: config.ResourcesConfig{Directories: []string{"index-out"}}}
	dirs, vars, err := DescribeDirectories(root, idA, cfg)
	require.NoError(t, err)
	require.False(t, dirs.Exists, "describe never creates")
	require.NoDirExists(t, dirs.Root)
	names := map[string]string{}
	for _, v := range vars {
		names[v.Name] = v.Value
	}
	require.Equal(t, filepath.Join(root, "worktrees", idA, "tmp"), names["ANVIL_TMP_DIR"])
	require.Equal(t, filepath.Join(root, "worktrees", idA, "dirs", "index-out"), names["ANVIL_DIR_INDEX_OUT"])

	_, _, err = ProvisionDirectories(root, idA, "/a", cfg)
	require.NoError(t, err)
	for _, path := range dirs.Named {
		require.DirExists(t, path)
	}
	_, _, err = ProvisionDirectories(root, idA, "/a", cfg)
	require.NoError(t, err, "idempotent")

	// A foreign owner record blocks provisioning and removal.
	require.NoError(t, writeOwner(dirs.Root, Owner{WorktreeID: idB, WorktreePath: "/b"}))
	_, _, err = ProvisionDirectories(root, idA, "/a", cfg)
	require.ErrorContains(t, err, "belongs to")
	removed, err := RemoveDirectories(root, Owner{WorktreeID: idA, WorktreePath: "/a"})
	require.Error(t, err)
	require.False(t, removed)
	require.DirExists(t, dirs.Root)

	// An owner record that changed since it was validated is left alone.
	require.NoError(t, writeOwner(dirs.Root, Owner{WorktreeID: idA, WorktreePath: "/moved"}))
	removed, err = RemoveDirectories(root, Owner{WorktreeID: idA, WorktreePath: "/a"})
	require.ErrorContains(t, err, "leaving it in place")
	require.False(t, removed)

	// The validated record matches even when it names an old path.
	removed, err = RemoveDirectories(root, Owner{WorktreeID: idA, WorktreePath: "/moved"})
	require.NoError(t, err)
	require.True(t, removed)
	require.NoDirExists(t, dirs.Root)
	removed, err = RemoveDirectories(root, Owner{WorktreeID: idA, WorktreePath: "/moved"})
	require.NoError(t, err)
	require.False(t, removed)

	_, err = WorktreeDir(root, "../escape")
	require.Error(t, err)
}

func TestSharedResourcesAreNeverCreated(t *testing.T) {
	existing := t.TempDir()
	cfg := &config.Config{Resources: config.ResourcesConfig{Shared: []config.SharedResource{
		{Name: "models", Path: existing, Env: "SEYK_MODELS", Required: true},
		{Name: "optional", Path: filepath.Join(existing, "absent")},
	}}}
	statuses, vars, err := DescribeShared(cfg)
	require.NoError(t, err)
	require.True(t, statuses[0].Exists)
	require.False(t, statuses[1].Exists)
	require.NoDirExists(t, filepath.Join(existing, "absent"))
	require.Equal(t, "ANVIL_SHARED_MODELS", vars[0].Name)
	require.Equal(t, "SEYK_MODELS", vars[1].Name)

	cfg.Resources.Shared[1].Required = true
	_, _, err = DescribeShared(cfg)
	require.ErrorContains(t, err, "required shared resources are missing")

	cfg.Resources.Shared = []config.SharedResource{{Name: "rel", Path: "relative"}}
	_, _, err = DescribeShared(cfg)
	require.ErrorContains(t, err, "must be absolute")
}
