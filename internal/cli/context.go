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
	BarePath      string
	ProjectPath   string
	Config        *config.Config
	DefaultBranch string

	presetManager   *presets.Manager
	scaffoldManager *scaffold.ScaffoldManager
	managersInit    sync.Once
}

func OpenProjectFromCWD() (*ProjectContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current directory: %w", err)
	}

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
	}, nil
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
