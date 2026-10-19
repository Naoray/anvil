package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// SetBranchUpstream configures a branch to track a remote.
// This is idempotent - safe to call multiple times.
func SetBranchUpstream(gitDir, branch, remote string) error {
	cmd := exec.Command("git", "-C", gitDir, "config",
		fmt.Sprintf("branch.%s.remote", branch), remote)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting branch remote: %w\n%s", err, string(output))
	}

	cmd = exec.Command("git", "-C", gitDir, "config",
		fmt.Sprintf("branch.%s.merge", branch), fmt.Sprintf("refs/heads/%s", branch))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting branch merge: %w\n%s", err, string(output))
	}

	return nil
}

// HasBranchTracking checks if a branch has upstream tracking configured.
func HasBranchTracking(gitDir, branch string) (bool, error) {
	cmd := exec.Command("git", "-C", gitDir, "config", "--get", fmt.Sprintf("branch.%s.remote", branch))
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("checking branch tracking: %w", err)
	}
	return true, nil
}

// GetBranchRefs returns all local and remote branch names.
// Local branches are returned as-is (e.g., "main", "feature/foo").
// Remote branches are returned with remote prefix (e.g., "origin/main").
func GetBranchRefs(gitDir string) (local []string, remote []string, err error) {
	// Get local branches
	cmd := exec.Command("git", "-C", gitDir, "for-each-ref",
		"--format=%(refname)", "refs/heads/")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("listing local branches: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			local = append(local, strings.TrimPrefix(line, "refs/heads/"))
		}
	}

	// Get remote branches
	cmd = exec.Command("git", "-C", gitDir, "for-each-ref",
		"--format=%(refname)", "refs/remotes/")
	output, err = cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("listing remote branches: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		name := strings.TrimPrefix(line, "refs/remotes/")
		if name != line && !strings.HasSuffix(name, "/HEAD") {
			remote = append(remote, name)
		}
	}

	return local, remote, nil
}

// WorkBranch identifies the local branch and selected remote start point.
type WorkBranch struct {
	Branch   string
	Base     string
	Remote   string
	Existing bool
}

// ResolveWorkBranch prefers an exact local name, then a qualified remote ref,
// then a unique remote branch with the requested name. Unknown names are new branches.
func ResolveWorkBranch(gitDir, selected string) (WorkBranch, error) {
	local, remoteRefs, err := GetBranchRefs(gitDir)
	if err != nil {
		return WorkBranch{}, err
	}
	hasLocal := func(name string) bool {
		for _, branch := range local {
			if branch == name {
				return true
			}
		}
		return false
	}
	if hasLocal(selected) {
		return WorkBranch{Branch: selected, Existing: true}, nil
	}
	cmd := exec.Command("git", "-C", gitDir, "remote")
	output, err := cmd.Output()
	if err != nil {
		return WorkBranch{}, fmt.Errorf("listing remotes: %w", err)
	}
	remotes := strings.Fields(string(output))
	var candidates []WorkBranch
	qualified := strings.TrimPrefix(selected, "refs/remotes/")
	for _, ref := range remoteRefs {
		owner := ""
		for _, remote := range remotes {
			if strings.HasPrefix(ref, remote+"/") && len(remote) > len(owner) {
				owner = remote
			}
		}
		if owner == "" {
			continue
		}
		branch := strings.TrimPrefix(ref, owner+"/")
		candidate := WorkBranch{Branch: branch, Base: "refs/remotes/" + ref, Remote: owner, Existing: hasLocal(branch)}
		if qualified == ref {
			return candidate, nil
		}
		if selected == branch {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) > 1 {
		return WorkBranch{}, fmt.Errorf("ambiguous branch %q: specify a remote-qualified ref", selected)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if qualified != selected {
		return WorkBranch{}, fmt.Errorf("remote branch %q not found", selected)
	}
	return WorkBranch{Branch: selected}, nil
}
