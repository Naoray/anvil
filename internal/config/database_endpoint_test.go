package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnedEndpointHistoryIsPreservedAndCannotBeRetargeted(t *testing.T) {
	root := t.TempDir()
	first := &DatabaseEndpoint{Driver: "herd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
	second := &DatabaseEndpoint{Driver: "yerd", Service: "mariadb", Host: "127.0.0.1", Port: "3307"}
	for _, db := range []OwnedDatabase{{Name: "original", Engine: "mysql", Role: DbRoleApplication, Endpoint: first}, {Name: "later", Engine: "mysql", Role: DbRoleApplication, Endpoint: second}} {
		require.NoError(t, WriteLocalState(root, LocalState{Databases: []OwnedDatabase{db}}))
	}
	state, err := ReadLocalState(root)
	require.NoError(t, err)
	require.Len(t, state.Databases, 2)
	require.Equal(t, first, state.Databases[0].Endpoint)
	require.Equal(t, second, state.Databases[1].Endpoint)
	require.Equal(t, DbRoleAuxiliary, state.Databases[1].Role)
	require.NoError(t, WriteLocalState(root, LocalState{Databases: []OwnedDatabase{{Name: "original", Engine: "mysql", Role: DbRoleApplication}}}))
	state, err = ReadLocalState(root)
	require.NoError(t, err)
	require.Equal(t, first, state.Databases[0].Endpoint)
	before, err := os.ReadFile(filepath.Join(root, LocalStateFile))
	require.NoError(t, err)
	require.ErrorContains(t, WriteLocalState(root, LocalState{Databases: []OwnedDatabase{{Name: "original", Engine: "mysql", Role: DbRoleApplication, Endpoint: second}}}), "cannot be replaced")
	after, err := os.ReadFile(filepath.Join(root, LocalStateFile))
	require.NoError(t, err)
	require.Equal(t, before, after)
}
