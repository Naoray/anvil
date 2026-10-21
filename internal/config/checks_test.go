package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadYAML(t *testing.T, content string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(content), 0o644))
	return LoadProject(dir)
}

func TestCheckSharedResourceIdentityDecodes(t *testing.T) {
	cfg, err := loadYAML(t, `resources:
  shared:
    - name: models
      path: /opt/models
      identity: {file: SHA256SUMS}
    - name: fixtures
      path: /opt/fixtures
      identity: {command: "git rev-parse HEAD"}
    - name: weights
      path: /opt/weights
      identity: {directory: true}
    - name: docs
      path: /opt/docs
checks:
  fast:
    commands: ["true"]
    resources: [weights, models]
`)
	require.NoError(t, err)
	require.Equal(t, &SharedIdentity{File: "SHA256SUMS"}, cfg.Resources.Shared[0].Identity)
	require.Equal(t, "file", cfg.Resources.Shared[0].Identity.Method())
	require.Equal(t, "command", cfg.Resources.Shared[1].Identity.Method())
	require.Equal(t, "directory", cfg.Resources.Shared[2].Identity.Method())
	require.Nil(t, cfg.Resources.Shared[3].Identity, "resources without identity stay valid until a check depends on them")
	require.Equal(t, []string{"weights", "models"}, cfg.Checks[0].Resources)
}

func TestCheckSharedResourceValidation(t *testing.T) {
	for name, tc := range map[string]struct{ shared, check, want string }{
		"unknown resource":     {"identity: {file: SUMS}", "resources: [nope]", `unknown shared resource "nope"`},
		"duplicate resource":   {"identity: {file: SUMS}", "resources: [models, models]", `duplicate resource "models"`},
		"no identity method":   {"required: false", "resources: [models]", `shared resource "models" declares no identity`},
		"two methods":          {"identity: {file: SUMS, directory: true}", "resources: []", "exactly one of file, command or directory"},
		"empty identity":       {"identity: {}", "resources: []", "exactly one of file, command or directory"},
		"blank command":        {`identity: {command: "  "}`, "resources: []", "exactly one of file, command or directory"},
		"file leaves":          {"identity: {file: ../SUMS}", "resources: []", "leaves"},
		"absolute file":        {"identity: {file: /etc/passwd}", "resources: []", "must be relative"},
		"directory false only": {"identity: {directory: false}", "resources: []", "exactly one of file, command or directory"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, "resources:\n  shared:\n    - name: models\n      path: /opt/models\n      "+tc.shared+
				"\nchecks:\n  fast:\n    commands: [\"true\"]\n    "+tc.check+"\n")
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCheckReuseConfigDecodes(t *testing.T) {
	cfg, err := loadYAML(t, `roles:
  implementation:
    reuse_trust: same-worktree
  review:
    env_unset: [DEPLOY_TOKEN]
    reuse_trust: same-role
    reuse_exclude_roles: [implementation]
checks:
  fast:
    commands: ["true"]
    identity_env: [FEATURE_FLAG]
`)
	require.NoError(t, err)
	require.Equal(t, []string{"FEATURE_FLAG"}, cfg.Checks[0].IdentityEnv)
	require.Equal(t, RoleReuseConfig{ReuseTrust: ReuseTrustSameWorktree}, cfg.Roles.Reuse("implementation"))
	require.Equal(t, RoleReuseConfig{ReuseTrust: ReuseTrustSameRole, ReuseExcludeRoles: []string{"implementation"}}, cfg.Roles.Reuse("review"))
	require.Equal(t, []string{"DEPLOY_TOKEN"}, cfg.Roles.Review.EnvUnset)
	require.Equal(t, RoleReuseConfig{}, cfg.Roles.Reuse(""), "unlabelled worktrees use the default")
}

func TestCheckReuseConfigValidation(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"trust":        {"roles:\n  review:\n    reuse_trust: everyone\n", `roles.review.reuse_trust: invalid value "everyone"`},
		"exclude role": {"roles:\n  implementation:\n    reuse_exclude_roles: [qa]\n", `roles.implementation.reuse_exclude_roles: unknown role "qa"`},
		"identity_env": {"checks:\n  fast:\n    commands: [\"true\"]\n    identity_env: [\"BAD-NAME\"]\n", `identity_env: invalid variable name "BAD-NAME"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, tc.yaml)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
