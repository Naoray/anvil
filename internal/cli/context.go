package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/michaeldyrynda/arbor/internal/config"
	arborerrors "github.com/michaeldyrynda/arbor/internal/errors"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/presets"
	"github.com/michaeldyrynda/arbor/internal/scaffold"
)

type ProjectContext struct {
	CWD           string
	BarePath      string // For legacy projects, this is the .bare path; for linked projects, this is the .git path
	ProjectPath   string
	Config        *config.Config
	DefaultBranch string

	// Linked project fields
	IsLinked     bool
	ProjectName  string
	WorktreeBase string
	GlobalConfig *config.GlobalConfig

	presetManager   *presets.Manager
	scaffoldManager *scaffold.ScaffoldManager
	managersInit    sync.Once
}

func OpenProjectFromCWD() (*ProjectContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current directory: %w", err)
	}

	// First, check if we're in a linked project
	globalCfg, err := config.LoadOrCreateGlobalConfig()
	if err == nil {
		projectName, projectInfo := globalCfg.FindLinkedProjectFromPath(cwd)
		if projectInfo != nil {
			return openLinkedProject(cwd, projectName, projectInfo, globalCfg)
		}

		// Also check if we're in a worktree of a linked project
		worktreeBase, err := globalCfg.GetWorktreeBaseExpanded()
		if err == nil && worktreeBase != "" {
			pc, err := openLinkedProjectFromWorktree(cwd, worktreeBase, globalCfg)
			if err == nil && pc != nil {
				return pc, nil
			}
		}
	}

	// Fall back to legacy .bare project detection
	barePath, err := git.FindBarePath(cwd)
	if err != nil {
		return nil, fmt.Errorf("finding bare repository: %w", err)
	}

	projectPath := filepath.Dir(barePath)
	cfg, err := config.LoadProject(projectPath)
	if err != nil {
		return nil, fmt.Errorf("loading project config: %w", err)
	}

	defaultBranch := cfg.DefaultBranch
	if defaultBranch == "" {
		defaultBranch, _ = git.GetDefaultBranch(barePath)
		if defaultBranch == "" {
			defaultBranch = config.DefaultBranch
		}
	}

	return &ProjectContext{
		CWD:           cwd,
		BarePath:      barePath,
		ProjectPath:   projectPath,
		Config:        cfg,
		DefaultBranch: defaultBranch,
		IsLinked:      false,
		GlobalConfig:  globalCfg,
	}, nil
}

// openLinkedProject creates a ProjectContext for a linked project
func openLinkedProject(cwd, projectName string, projectInfo *config.ProjectInfo, globalCfg *config.GlobalConfig) (*ProjectContext, error) {
	gitDir, _, err := git.FindGitDir(projectInfo.Path)
	if err != nil {
		return nil, fmt.Errorf("finding git directory for linked project: %w", err)
	}

	defaultBranch := projectInfo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch, _ = git.GetDefaultBranch(gitDir)
		if defaultBranch == "" {
			defaultBranch = config.DefaultBranch
		}
	}

	worktreeBase, _ := globalCfg.GetWorktreeBaseExpanded()

	// Create a synthetic config for the linked project
	cfg := &config.Config{
		SiteName:      projectInfo.SiteName,
		Preset:        projectInfo.Preset,
		DefaultBranch: defaultBranch,
	}

	return &ProjectContext{
		CWD:           cwd,
		BarePath:      gitDir,
		ProjectPath:   projectInfo.Path,
		Config:        cfg,
		DefaultBranch: defaultBranch,
		IsLinked:      true,
		ProjectName:   projectName,
		WorktreeBase:  worktreeBase,
		GlobalConfig:  globalCfg,
	}, nil
}

// openLinkedProjectFromWorktree creates a ProjectContext when inside a worktree of a linked project
func openLinkedProjectFromWorktree(cwd, worktreeBase string, globalCfg *config.GlobalConfig) (*ProjectContext, error) {
	// Check if cwd is under worktreeBase
	rel, err := filepath.Rel(worktreeBase, cwd)
	if err != nil || len(rel) == 0 || rel[0] == '.' {
		return nil, nil // Not under worktree base
	}

	// Extract project name from path (first component after worktreeBase)
	parts := filepath.SplitList(rel)
	if len(parts) == 0 {
		// Try splitting by separator
		for i := 0; i < len(rel); i++ {
			if rel[i] == filepath.Separator {
				parts = []string{rel[:i]}
				break
			}
		}
		if len(parts) == 0 {
			parts = []string{rel}
		}
	}

	// Get first path component
	projectName := ""
	for i := 0; i < len(rel); i++ {
		if rel[i] == filepath.Separator {
			projectName = rel[:i]
			break
		}
	}
	if projectName == "" {
		projectName = rel
	}

	// Look up the project
	projectInfo := globalCfg.GetLinkedProjectByName(projectName)
	if projectInfo == nil {
		return nil, nil // Project not found in config
	}

	return openLinkedProject(cwd, projectName, projectInfo, globalCfg)
}

// GetWorktreePath returns the path where worktrees should be created for this project
func (pc *ProjectContext) GetWorktreePath(branch string) string {
	sanitizedBranch := sanitizeBranchName(branch)

	if pc.IsLinked && pc.WorktreeBase != "" {
		// Linked project: use centralized worktree location
		return filepath.Join(pc.WorktreeBase, pc.ProjectName, sanitizedBranch)
	}

	// Legacy project: worktrees are siblings to .bare
	return filepath.Join(pc.ProjectPath, sanitizedBranch)
}

// sanitizeBranchName converts branch name to a safe directory name
func sanitizeBranchName(branch string) string {
	// Replace / with - for feature branches
	result := ""
	for _, c := range branch {
		if c == '/' {
			result += "-"
		} else {
			result += string(c)
		}
	}
	return result
}

func (pc *ProjectContext) IsInWorktree() bool {
	// Check if .bare exists in parent hierarchy
	barePath, err := git.FindBarePath(pc.CWD)
	if err != nil {
		return false
	}

	// Check if CWD is inside a worktree directory (not the project root)
	projectPath := filepath.Dir(barePath)

	// If CWD equals project path, we're in the project root, not a worktree
	cwdAbs, err := filepath.Abs(pc.CWD)
	if err != nil {
		return false
	}

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return false
	}

	// If we're in the project root or its direct child .bare, we're not in a worktree
	if cwdAbs == projectAbs || cwdAbs == filepath.Join(projectAbs, ".bare") {
		return false
	}

	// We're somewhere under the project root but not the root itself
	// Check if we're actually in a worktree by seeing if CWD is within a worktree path
	return true
}

func (pc *ProjectContext) MustBeInWorktree() error {
	if !pc.IsInWorktree() {
		return arborerrors.ErrWorktreeNotFound
	}
	return nil
}

func (pc *ProjectContext) PresetManager() *presets.Manager {
	pc.managersInit.Do(func() {
		pc.presetManager = presets.NewManager()
		pc.scaffoldManager = scaffold.NewScaffoldManager()
		presets.RegisterAllWithScaffold(pc.scaffoldManager)
	})
	return pc.presetManager
}

func (pc *ProjectContext) ScaffoldManager() *scaffold.ScaffoldManager {
	pc.managersInit.Do(func() {
		pc.presetManager = presets.NewManager()
		pc.scaffoldManager = scaffold.NewScaffoldManager()
		presets.RegisterAllWithScaffold(pc.scaffoldManager)
	})
	return pc.scaffoldManager
}
