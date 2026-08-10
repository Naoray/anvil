package cli

import (
	"path/filepath"
	"testing"
)

func TestWorktreeSiteName(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		branch          string
		defaultBranch   string
		projectSiteName string
		want            string
	}{
		{
			name:            "default branch uses configured project site name",
			path:            filepath.Join("tmp", "main"),
			branch:          "main",
			defaultBranch:   "main",
			projectSiteName: "custom-app",
			want:            "custom-app",
		},
		{
			name:            "feature branch uses worktree folder",
			path:            filepath.Join("tmp", "feature-auth"),
			branch:          "feature/auth",
			defaultBranch:   "main",
			projectSiteName: "custom-app",
			want:            "feature-auth",
		},
		{
			name:          "default branch without configured name uses folder",
			path:          filepath.Join("tmp", "main"),
			branch:        "main",
			defaultBranch: "main",
			want:          "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := worktreeSiteName(tt.path, tt.branch, tt.defaultBranch, tt.projectSiteName); got != tt.want {
				t.Fatalf("worktreeSiteName() = %q, want %q", got, tt.want)
			}
		})
	}
}
