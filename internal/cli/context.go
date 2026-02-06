package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/steps"
)

type ProjectContext struct {
	CWD           string
	GitDir        string
	ProjectPath   string
	Config        *config.Config
	DefaultBranch string

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

	// Load global config and find linked project
	globalCfg, err := config.LoadOrCreateGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("loading global config: %w", err)
	}

	// Check if we're in a linked project
	projectName, projectInfo := globalCfg.FindLinkedProjectFromPath(cwd)
	if projectInfo != nil {
		return openProject(cwd, projectName, projectInfo, globalCfg)
	}

	// Check if we're in a worktree of a linked project
	worktreeBase, err := globalCfg.GetWorktreeBaseExpanded()
	if err == nil && worktreeBase != "" {
		pc, err := openProjectFromWorktree(cwd, worktreeBase, globalCfg)
		if err == nil && pc != nil {
			return pc, nil
		}
	}

	return nil, fmt.Errorf("not in a linked anvil project (run 'anvil link' first)")
}

// openProject creates a ProjectContext for a linked project
func openProject(cwd, projectName string, projectInfo *config.ProjectInfo, globalCfg *config.GlobalConfig) (*ProjectContext, error) {
	gitDir, err := git.FindGitDir(projectInfo.Path)
	if err != nil {
		return nil, fmt.Errorf("finding git directory: %w", err)
	}

	defaultBranch := projectInfo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch, _ = git.GetDefaultBranch(gitDir)
		if defaultBranch == "" {
			defaultBranch = config.DefaultBranch
		}
	}

	worktreeBase, _ := globalCfg.GetWorktreeBaseExpanded()

	cfg := &config.Config{
		SiteName:      projectInfo.SiteName,
		Preset:        projectInfo.Preset,
		DefaultBranch: defaultBranch,
	}

	return &ProjectContext{
		CWD:           cwd,
		GitDir:        gitDir,
		ProjectPath:   projectInfo.Path,
		Config:        cfg,
		DefaultBranch: defaultBranch,
		ProjectName:   projectName,
		WorktreeBase:  worktreeBase,
		GlobalConfig:  globalCfg,
	}, nil
}

// openProjectFromWorktree creates a ProjectContext when inside a worktree of a linked project
func openProjectFromWorktree(cwd, worktreeBase string, globalCfg *config.GlobalConfig) (*ProjectContext, error) {
	// Check if cwd is under worktreeBase
	rel, err := filepath.Rel(worktreeBase, cwd)
	if err != nil || len(rel) == 0 || rel[0] == '.' {
		return nil, nil // Not under worktree base
	}

	// Get first path component as project name
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

	return openProject(cwd, projectName, projectInfo, globalCfg)
}

// GetWorktreePath returns the path where worktrees should be created for this project
func (pc *ProjectContext) GetWorktreePath(branch string) string {
	sanitizedBranch := sanitizeBranchName(branch)
	if pc.WorktreeBase != "" {
		return filepath.Join(pc.WorktreeBase, pc.ProjectName, sanitizedBranch)
	}
	return filepath.Join(pc.ProjectPath, sanitizedBranch)
}

// sanitizeBranchName converts branch name to a safe directory name
func sanitizeBranchName(branch string) string {
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
	// Use git to check if we're inside a worktree
	gitDir, err := git.FindGitDir(pc.CWD)
	if err != nil {
		// No .git found - check if CWD is under worktree base
		if pc.WorktreeBase != "" {
			rel, err := filepath.Rel(pc.WorktreeBase, pc.CWD)
			if err == nil && len(rel) > 0 && rel[0] != '.' {
				return true
			}
		}
		return false
	}

	// If the gitDir is a file-based reference (worktree), we're in a worktree
	_ = gitDir
	cwdAbs, _ := filepath.Abs(pc.CWD)
	projectAbs, _ := filepath.Abs(pc.ProjectPath)

	// If we're in the project root itself, we're not in a worktree
	if cwdAbs == projectAbs {
		return false
	}

	return true
}

func (pc *ProjectContext) MustBeInWorktree() error {
	if !pc.IsInWorktree() {
		return fmt.Errorf("not inside a worktree")
	}
	return nil
}

func (pc *ProjectContext) PresetManager() *presets.Manager {
	pc.managersInit.Do(func() {
		pc.initManagers()
	})
	return pc.presetManager
}

func (pc *ProjectContext) ScaffoldManager() *scaffold.ScaffoldManager {
	pc.managersInit.Do(func() {
		pc.initManagers()
	})
	return pc.scaffoldManager
}

func (pc *ProjectContext) initManagers() {
	stepRegistry := steps.NewRegistry()
	stepRegistry.RegisterDefaults()

	pc.presetManager = presets.NewManager()
	pc.scaffoldManager = scaffold.NewScaffoldManagerWithRegistry(stepRegistry)
	presets.RegisterAllWithScaffold(pc.scaffoldManager)
}
