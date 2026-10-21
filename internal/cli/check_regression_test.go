package cli

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckReuseFingerprintsEffectiveEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("checks require Unix supervision")
	}
	for _, scope := range []string{"component", "command", "inherited"} {
		t.Run(scope, func(t *testing.T) {
			yaml := "components:\n  - name: app\n    path: app\n    env: {MODE: '${CHECK_MODE}'}\nchecks:\n  env:\n    reusable: true\n    commands:\n"
			switch scope {
			case "component":
				yaml += "      - run: test \"$MODE\" = pass\n        component: app\n"
			case "command":
				yaml += "      - run: test \"$MODE\" = pass\n        env: {MODE: '${CHECK_MODE}'}\n"
			case "inherited":
				yaml += "      - test \"$CHECK_MODE\" = pass\n"
			}
			f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
			base := append([]string{}, f.env...)
			for i, mode := range []string{"pass", "pass", "fail"} {
				f.env = append(append([]string{}, base...), fmt.Sprintf("CHECK_MODE=%s", mode))
				envelope, stderr, code := f.runJSON(f.repo, "check", "env", "--reuse")
				if mode == "fail" {
					require.Equal(t, 1, code, stderr)
				} else {
					require.Equal(t, 0, code, stderr)
				}
				require.Equal(t, i == 1, field(t, envelope, "data", "results", 0, "reused"))
			}
		})
	}
}
