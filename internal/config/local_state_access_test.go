package config

import (
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalStateReadersWaitForAtomicUpdate(t *testing.T) {
	for _, kind := range []string{"raw", "typed"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, WriteLocalState(dir, LocalState{DbSuffix: "before"}))
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			defer finish()
			written := make(chan error, 1)
			go func() {
				written <- UpdateLocalState(dir, func(doc map[string]any) error {
					close(entered)
					<-release
					doc["db_suffix"] = "after"
					return nil
				})
			}()
			<-entered
			type snapshot struct {
				suffix string
				err    error
			}
			started, read := make(chan struct{}), make(chan snapshot, 1)
			go func() {
				close(started)
				if kind == "raw" {
					doc, err := ReadLocalStateMap(dir)
					suffix, _ := doc["db_suffix"].(string)
					read <- snapshot{suffix: suffix, err: err}
					return
				}
				state, err := ReadLocalState(dir)
				value := snapshot{err: err}
				if state != nil {
					value.suffix = state.DbSuffix
				}
				read <- value
			}()
			<-started
			select {
			case value := <-read:
				t.Fatalf("reader bypassed the state lock during an update: %+v", value)
			case <-time.After(100 * time.Millisecond):
				// A reader must wait until the existing writer commits.
			}
			finish()
			require.NoError(t, <-written)
			value := <-read
			require.NoError(t, value.err)
			require.Equal(t, "after", value.suffix)
		})
	}
}

func TestLocalStateCrossProcessReadersAndWriters(t *testing.T) {
	if dir := os.Getenv("ANVIL_STATE_ACCESS_DIR"); dir != "" {
		for i := 0; i < 30; i++ {
			if os.Getenv("ANVIL_STATE_ACCESS_MODE") == "write" {
				incrementCounter(t, dir)
				continue
			}
			_, err := ReadLocalStateMap(dir)
			require.NoError(t, err)
			state, err := ReadLocalState(dir)
			require.NoError(t, err)
			require.Equal(t, "stable", state.DbSuffix)
		}
		return
	}
	dir := t.TempDir()
	require.NoError(t, WriteLocalState(dir, LocalState{DbSuffix: "stable"}))
	var commands []*exec.Cmd
	for _, mode := range []string{"write", "read", "write", "read"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLocalStateCrossProcessReadersAndWriters$")
		cmd.Env = append(os.Environ(), "ANVIL_STATE_ACCESS_DIR="+dir, "ANVIL_STATE_ACCESS_MODE="+mode)
		commands = append(commands, cmd)
		require.NoError(t, cmd.Start())
	}
	for _, cmd := range commands {
		require.NoError(t, cmd.Wait(), "all concurrent readers and writers must succeed")
	}
	doc, err := ReadLocalStateMap(dir)
	require.NoError(t, err)
	require.Equal(t, 60, doc["counter"])
}
