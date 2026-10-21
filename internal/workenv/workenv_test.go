package workenv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestBuildLayersAndExpands(t *testing.T) {
	t.Setenv("ANVIL_TEST_INHERITED", "outer")
	env := Build(Options{
		Identity: Identity{ID: "wt_0123456789abcdef", Path: "/w/demo/feature-x", Branch: "feature/x", Project: "Demo"},
		Config: &config.Config{
			Env:       map[string]string{"APP_URL": "http://localhost/${ANVIL_INSTANCE}", "FROM_OUTER": "${ANVIL_TEST_INHERITED}", "STRIPE_SECRET": "sk", "PLAIN": "v", "MARKED": "m"},
			SecretEnv: []string{"MARKED"},
		},
		State: &config.LocalState{Databases: []config.OwnedDatabase{
			{Name: "demo_app", Role: config.DbRoleApplication}, {Name: "demo_app_test", Role: config.DbRoleTesting},
		}},
		Component: &ComponentEnv{Name: "web", Env: map[string]string{"PLAIN": "component-wins"}},
		Resources: []Variable{{Name: "ANVIL_STATE_DIR", Value: "/s", Source: SourceResource}},
	})
	values := env.Map()
	require.Equal(t, "wt_0123456789abcdef", values["ANVIL_WORKTREE_ID"])
	require.Equal(t, "demo-feature-x-012345", values["ANVIL_INSTANCE"])
	require.Equal(t, "http://localhost/demo-feature-x-012345", values["APP_URL"])
	require.Equal(t, "outer", values["FROM_OUTER"])
	require.Equal(t, "component-wins", values["PLAIN"])
	require.Equal(t, "demo_app_test", values["ANVIL_TEST_DB_DATABASE"])
	require.Equal(t, "/s", values["ANVIL_STATE_DIR"])
	require.NotContains(t, values, "DB_DATABASE", "run never takes over exec's variable")

	redacted := map[string]Variable{}
	for _, v := range env.Redacted() {
		redacted[v.Name] = v
	}
	require.True(t, redacted["STRIPE_SECRET"].Secret)
	require.Empty(t, redacted["STRIPE_SECRET"].Value)
	require.True(t, redacted["MARKED"].Secret)
	require.Equal(t, SourceComponent, redacted["PLAIN"].Source, "component values override project values")
}

func TestEnvironOverlayAndUnset(t *testing.T) {
	env := &Environment{}
	env.Set(Variable{Name: "A", Value: "new"})
	out := env.Environ([]string{"A=old", "B=keep", "TOKEN=x", "a=lower"}, []string{"TOKEN"}, false)
	require.Equal(t, []string{"B=keep", "a=lower", "A=new"}, out)
	out = env.Environ([]string{"a=lower", "B=keep"}, nil, true)
	require.Equal(t, []string{"B=keep", "A=new"}, out, "Windows keys are case-insensitive")
}

func TestInstanceSlug(t *testing.T) {
	slug := InstanceSlug("My Project!", strings.Repeat("very-long-branch-", 5), "")
	require.LessOrEqual(t, len(slug), maxSlug)
	require.Regexp(t, `^[a-z0-9-]+$`, slug)
	require.Equal(t, slug, InstanceSlug("My Project!", strings.Repeat("very-long-branch-", 5), ""), "stable")
	require.NotEqual(t, InstanceSlug("p", "a", "wt_aaaaaa"), InstanceSlug("p", "a", "wt_bbbbbb"))
	require.True(t, IsSecretName("GITHUB_TOKEN", nil))
	require.False(t, IsSecretName("RUST_LOG", nil))
}
