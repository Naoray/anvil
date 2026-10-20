package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/wtstate"
)

// runtimeCleanupPlan lists the runtime resources a worktree owns. It is
// computed before any effect because removing the worktree deletes the
// .anvil.local that identifies them.
type runtimeCleanupPlan struct {
	WorktreeID   string                  `json:"worktree_id,omitempty"`
	WorktreePath string                  `json:"worktree_path"`
	Processes    []wtstate.ProcessRecord `json:"processes"`
	Ports        []resources.PortState   `json:"ports"`
	RuntimeDir   string                  `json:"runtime_dir,omitempty"`
	// Skipped explains why runtime resources are left untouched.
	Skipped string `json:"skipped,omitempty"`
	root    string
}

// planRuntimeCleanup inspects owned runtime resources without changing them.
func planRuntimeCleanup(worktreePath string) (*runtimeCleanupPlan, error) {
	canonical, err := wtstate.CanonicalPath(worktreePath)
	if err != nil {
		canonical = worktreePath
	}
	plan := &runtimeCleanupPlan{WorktreePath: canonical, Processes: []wtstate.ProcessRecord{}, Ports: []resources.PortState{}}
	if _, statErr := os.Stat(worktreePath); errors.Is(statErr, os.ErrNotExist) {
		// Without .anvil.local nothing identifies owned resources; port
		// reservations of a vanished worktree are reclaimed as stale.
		plan.Skipped = "worktree directory is missing; no runtime resources can be identified"
		return plan, nil
	}
	identity, status, err := wtstate.ReadIdentity(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("reading worktree identity: %w", err)
	}
	switch {
	case identity == nil:
		plan.Skipped = "worktree has no Anvil identity; no runtime resources are recorded"
		return plan, nil
	case status == wtstate.IdentityCopied:
		plan.Skipped = "worktree state was copied from " + identity.Path + "; the original's resources are left untouched"
		return plan, nil
	}
	plan.WorktreeID = identity.ID
	records, err := wtstate.ReadProcesses(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("reading process records: %w", err)
	}
	for _, record := range records {
		if record.WorktreeID == identity.ID {
			plan.Processes = append(plan.Processes, record)
		}
	}
	root, err := resources.Root()
	if err != nil {
		return nil, err
	}
	plan.root = root
	ports, err := resources.NewPorts(root)
	if err != nil {
		return nil, err
	}
	if plan.Ports, err = ports.Lookup(identity.ID); err != nil {
		return nil, err
	}
	if plan.Ports == nil {
		plan.Ports = []resources.PortState{}
	}
	dir, err := resources.WorktreeDir(root, identity.ID)
	if err != nil {
		return nil, err
	}
	if owner, err := resources.ReadOwner(dir); err == nil && owner.WorktreeID == identity.ID {
		plan.RuntimeDir = dir
	}
	return plan, nil
}

// describe renders the plan for dry-run and progress output.
func (p *runtimeCleanupPlan) describe(prefix string) []string {
	if p.Skipped != "" {
		return nil
	}
	var lines []string
	for _, record := range p.Processes {
		lines = append(lines, fmt.Sprintf("%sstop managed process %s (pid %d, %s)", prefix, record.Name, record.PID, procs.Check(record.PID, record.Identity)))
	}
	for _, port := range p.Ports {
		lines = append(lines, fmt.Sprintf("%srelease port %d (%s)", prefix, port.Port, port.Name))
	}
	if p.RuntimeDir != "" {
		lines = append(lines, fmt.Sprintf("%sremove runtime directory %s (shared resources are never touched)", prefix, p.RuntimeDir))
	}
	return lines
}

// stopProcesses stops verified owned processes before any other cleanup;
// a failure aborts removal so nothing is half-removed around a live process.
func (p *runtimeCleanupPlan) stopProcesses(cfg *config.Config) ([]processStatus, error) {
	if p.Skipped != "" || len(p.Processes) == 0 {
		return nil, nil
	}
	return stopRecordedProcesses(p.WorktreePath, p.WorktreeID, cfg, nil, false)
}

// release frees ports and deletes the owned runtime directory after the
// worktree itself has been removed.
func (p *runtimeCleanupPlan) release() error {
	if p.Skipped != "" || p.WorktreeID == "" {
		return nil
	}
	var problems []error
	if len(p.Ports) > 0 {
		ports, err := resources.NewPorts(p.root)
		if err == nil {
			_, err = ports.Release(p.WorktreeID)
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("releasing ports: %w", err))
		}
	}
	if p.RuntimeDir != "" {
		if _, err := resources.RemoveDirectories(p.root, p.WorktreeID, p.WorktreePath); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
