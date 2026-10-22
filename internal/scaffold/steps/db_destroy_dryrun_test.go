package steps

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// A dry run never starts a database service: when it is not running, the
// exact recorded targets are listed and worker enumeration is skipped.
func TestDbDestroyStep_DryRunDoesNotStartStoppedService(t *testing.T) {
	dir := t.TempDir()
	app := "app_top_provider"
	writeOwnedState(t, dir, []config.OwnedDatabase{{Name: app, Engine: "mysql", Role: config.DbRoleApplication}})
	client := NewMockDatabaseClient()
	client.SetRunning(false)
	client.AddDatabase(app + "_test_1")
	var output bytes.Buffer
	report := &types.CleanupReport{}
	step := NewDbDestroyStepWithFactoryAndWriter(config.StepConfig{}, MockClientFactory(client), &output)

	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir, CleanupReport: report}, types.StepOptions{DryRun: true}))
	assert.Equal(t, 0, client.GetPingCalls(), "dry run must not start the service")
	assert.Empty(t, client.GetListCalls())
	assert.Empty(t, client.GetDropCalls())
	assert.Equal(t, "Would drop database: "+app+"\nparallel-worker databases not enumerated: service not running\n", output.String())
	assert.Equal(t, []string{app}, report.Databases())
	assert.Equal(t, []string{"parallel-worker databases not enumerated: service not running"}, report.Warnings())
}

// A running service is enumerated without Ping; targets are reported.
func TestDbDestroyStep_DryRunEnumeratesRunningServiceWithoutPing(t *testing.T) {
	dir := t.TempDir()
	app := "app_top_provider"
	writeOwnedState(t, dir, []config.OwnedDatabase{{Name: app, Engine: "mysql", Role: config.DbRoleApplication}})
	client := NewMockDatabaseClient()
	client.AddDatabase(app + "_test_1")
	report := &types.CleanupReport{}
	step := NewDbDestroyStepWithFactoryAndWriter(config.StepConfig{}, MockClientFactory(client), &bytes.Buffer{})

	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir, CleanupReport: report}, types.StepOptions{DryRun: true}))
	assert.Equal(t, 0, client.GetPingCalls())
	assert.Empty(t, client.GetDropCalls())
	assert.Equal(t, []string{app, app + "_test_1"}, report.Databases())
}

func TestDbDestroyStep_LegacyDryRunDoesNotStartStoppedService(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
	client := NewMockDatabaseClient()
	client.SetRunning(false)
	var output bytes.Buffer
	step := NewDbDestroyStepWithFactoryAndWriter(config.StepConfig{Type: "mysql"}, MockClientFactory(client), &output)

	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{DryRun: true}))
	assert.Equal(t, 0, client.GetPingCalls())
	assert.Empty(t, client.GetListCalls())
	assert.Equal(t, "legacy databases not enumerated: service not running\n", output.String())
}

func TestYerdRunningNeverStartsService(t *testing.T) {
	commander := &yerdScriptedCommander{responses: []yerdCommandResponse{
		{output: []byte(`{"services":[{"service":"mysql","state":"stopped","installed_versions":["8.4"]}]}`)},
	}}
	client, err := NewYerdDatabaseClientFactory(commander)("mysql", DatabaseOptions{})
	require.NoError(t, err)
	running, err := client.Running()
	require.NoError(t, err)
	assert.False(t, running)
	require.Len(t, commander.calls, 1)
	assert.Equal(t, []string{"--json", "services"}, commander.calls[0].args)
}

func TestHerdRunningProbesListenerWithoutStarting(t *testing.T) {
	commander := &dryRunCommander{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	client, err := NewHerdDatabaseClientFactory(commander)("pgsql", DatabaseOptions{Endpoint: &config.DatabaseEndpoint{Driver: "herd", Service: "postgresql", Host: "127.0.0.1", Port: port}})
	require.NoError(t, err)
	running, err := client.Running()
	require.NoError(t, err)
	assert.True(t, running)

	require.NoError(t, listener.Close())
	running, err = client.Running()
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, commander.CallCount(), "the probe never runs herd")
}
