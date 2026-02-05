package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/artisanexperiences/arbor/internal/git"
)

func SelectBranchInteractive(barePath string, localBranches, remoteBranches []string) (string, error) {
	var selected string

	options := []huh.Option[string]{
		huh.NewOption("Create new branch...", "__new__"),
	}

	for _, b := range localBranches {
		options = append(options, huh.NewOption(b, b))
	}

	for _, b := range remoteBranches {
		options = append(options, huh.NewOption("↓ "+b, b))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a branch").
				Description("Choose an existing branch or create a new one").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	if selected == "__new__" {
		return PromptNewBranch()
	}

	return selected, nil
}

func PromptNewBranch() (string, error) {
	var name string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("New branch name").
				Placeholder("feature/my-feature").
				Value(&name).
				Validate(validateBranchName),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	return name, nil
}

func validateBranchName(s string) error {
	if s == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if len(s) < 2 {
		return fmt.Errorf("branch name must be at least 2 characters")
	}
	return nil
}

func SelectWorktreesToPrune(removable []git.Worktree) ([]git.Worktree, error) {
	if len(removable) == 0 {
		return nil, nil
	}

	options := make([]huh.Option[string], len(removable))
	for i, wt := range removable {
		label := fmt.Sprintf("%s (%s)", wt.Branch, filepath.Base(wt.Path))
		options[i] = huh.NewOption(label, wt.Branch)
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select worktrees to remove").
				Description("Space to toggle, Enter to confirm").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return nil, NormalizeAbort(err)
	}

	if len(selected) == 0 {
		return nil, nil
	}

	var result []git.Worktree
	for _, branch := range selected {
		for _, wt := range removable {
			if wt.Branch == branch {
				result = append(result, wt)
				break
			}
		}
	}

	return result, nil
}

func ConfirmRemoval(count int) (bool, error) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Remove worktrees").
				Description(fmt.Sprintf("Remove %d selected worktree(s)?", count)).
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}

func Confirm(message string) (bool, error) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(message).
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}

func PromptRepoURL() (string, error) {
	var repo string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Repository").
				Description("GitHub URL or owner/repo format").
				Placeholder("owner/repo or git@github.com:owner/repo.git").
				Value(&repo).
				Validate(validateRepoURL),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	return repo, nil
}

func validateRepoURL(s string) error {
	if s == "" {
		return fmt.Errorf("repository URL cannot be empty")
	}
	if len(s) < 3 {
		return fmt.Errorf("repository URL must be at least 3 characters")
	}
	return nil
}

func SelectWorktreeToRemove(worktrees []git.Worktree) (*git.Worktree, error) {
	var removable []git.Worktree
	for _, wt := range worktrees {
		if !wt.IsMain {
			removable = append(removable, wt)
		}
	}

	if len(removable) == 0 {
		return nil, fmt.Errorf("no worktrees available to remove")
	}

	options := make([]huh.Option[string], len(removable))
	for i, wt := range removable {
		status := ""
		if wt.IsMerged {
			status = " (merged)"
		}
		label := fmt.Sprintf("%s%s", wt.Branch, status)
		options[i] = huh.NewOption(label, wt.Branch)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select worktree to remove").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return nil, NormalizeAbort(err)
	}

	for _, wt := range removable {
		if wt.Branch == selected {
			return &wt, nil
		}
	}

	return nil, fmt.Errorf("worktree not found")
}

// SelectProjectToDestroy scans immediate children of cwd for arbor projects and returns selected path
// Checks for both arbor.yaml and .bare folder to confirm valid project
func SelectProjectToDestroy(cwd string) (string, error) {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return "", err
	}

	var projects []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(cwd, e.Name())
		yamlPath := filepath.Join(path, "arbor.yaml")
		barePath := filepath.Join(path, ".bare")
		if _, err := os.Stat(yamlPath); err == nil {
			if _, err := os.Stat(barePath); err == nil {
				projects = append(projects, e.Name())
			}
		}
	}

	if len(projects) == 0 {
		return "", fmt.Errorf("no arbor projects found in %s", cwd)
	}

	options := make([]huh.Option[string], len(projects))
	for i, p := range projects {
		options[i] = huh.NewOption(p, p)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a project to destroy").
				Description("Choose an arbor project to completely remove").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	return filepath.Join(cwd, selected), nil
}

// ConfirmDestroy shows confirmation dialog with worktree list
func ConfirmDestroy(projectName string, worktrees []git.Worktree) (bool, error) {
	var worktreeList string
	for _, wt := range worktrees {
		worktreeList += fmt.Sprintf("  • %s\n", wt.Branch)
	}

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Destroy project").
				Description(fmt.Sprintf("Destroy project %q?\n\nWorktrees to be removed:\n%s\nThis cannot be undone.", projectName, worktreeList)).
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}

// SelectWorktreeToScaffold allows selecting a worktree to scaffold
func SelectWorktreeToScaffold(worktrees []git.Worktree) (*git.Worktree, error) {
	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no worktrees available to scaffold")
	}

	options := make([]huh.Option[string], len(worktrees))
	for i, wt := range worktrees {
		label := fmt.Sprintf("%s (%s)", wt.Branch, filepath.Base(wt.Path))
		if wt.IsCurrent {
			label += " [current]"
		}
		if wt.IsMain {
			label += " [main]"
		}
		options[i] = huh.NewOption(label, wt.Path)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select worktree to scaffold").
				Description("Choose a worktree to run scaffold steps").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return nil, NormalizeAbort(err)
	}

	for _, wt := range worktrees {
		if wt.Path == selected {
			return &wt, nil
		}
	}

	return nil, fmt.Errorf("worktree not found")
}

// ConfirmScaffold prompts user to confirm scaffolding current worktree
func ConfirmScaffold(branch string) (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Scaffold current worktree").
				Description(fmt.Sprintf("Run scaffold steps for worktree %q?", branch)).
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}

// SelectSyncStrategy prompts user to choose between rebase and merge
func SelectSyncStrategy(defaultStrategy string) (string, error) {
	selected := defaultStrategy

	options := []huh.Option[string]{
		huh.NewOption("rebase (cleaner history)", "rebase"),
		huh.NewOption("merge (preserves all commits)", "merge"),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select sync strategy").
				Description("Choose how to integrate upstream changes").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	return selected, nil
}

// SelectUpstreamBranch prompts user to select an upstream branch
// Defaults to the defaultBranch option if available
func SelectUpstreamBranch(localBranches, remoteBranches []string, defaultBranch string) (string, error) {
	var selected string

	options := []huh.Option[string]{}

	// Add local branches first
	for _, b := range localBranches {
		// Skip the default branch if it's in the list - we'll add it at the top
		if b == defaultBranch {
			continue
		}
		options = append(options, huh.NewOption(b, b))
	}

	// Add remote branches (strip remote prefix)
	for _, b := range remoteBranches {
		// Skip remote HEAD references
		if strings.HasSuffix(b, "/HEAD") {
			continue
		}
		// Extract the branch name from "origin/branch"
		parts := strings.SplitN(b, "/", 2)
		if len(parts) == 2 {
			branchName := parts[1]
			// Skip if already in local branches or is default branch
			if branchName == defaultBranch {
				continue
			}
			// Check if already added
			alreadyAdded := false
			for _, opt := range options {
				if opt.Value == branchName {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				options = append(options, huh.NewOption(branchName+" (from remote)", branchName))
			}
		}
	}

	// Insert default branch at the beginning if it exists
	if defaultBranch != "" {
		defaultOption := huh.NewOption(defaultBranch+" (default)", defaultBranch)
		options = append([]huh.Option[string]{defaultOption}, options...)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select upstream branch").
				Description("Choose the branch to sync against").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return "", NormalizeAbort(err)
	}

	return selected, nil
}

// ConfirmSync prompts user to confirm running sync operation
func ConfirmSync(currentBranch, upstream, strategy string) (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm sync operation").
				Description(fmt.Sprintf("Sync branch %q with upstream %q using %s?", currentBranch, upstream, strategy)).
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}

// ConfirmSaveSyncConfig asks user if they want to save sync settings to arbor.yaml
func ConfirmSaveSyncConfig() (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save sync settings").
				Description("Save the selected upstream and strategy to arbor.yaml for future syncs?").
				Value(&confirmed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, NormalizeAbort(err)
	}

	return confirmed, nil
}
