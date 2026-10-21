package cli

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

// Adoption actions.
const (
	adoptAdopted    = "adopted"
	adoptUnchanged  = "unchanged"
	adoptUpdated    = "updated"
	adoptReassigned = "reassigned"
	adoptSkipped    = "skipped"
)

var adoptAgentCommand = agentCommand{Name: "adopt", Mutating: true, DryRun: true, Human: printAdoptHuman}

var adoptCmd = &cobra.Command{
	Use:   "adopt [WORKTREE]",
	Short: "Register Anvil identity for existing worktrees without scaffolding",
	Long: `Adopt existing Git worktrees of the linked project: record an Anvil worktree
identity (and, with --lease-owner, an advisory lease) without running any
scaffold or preparation step.

WORKTREE is matched exactly by folder name, branch, path or worktree ID;
without it the worktree containing the current directory is adopted. --all
adopts every registered worktree that exists on disk, except bare entries
and the main checkout (name the main checkout explicitly to adopt it).

Safe to retry; existing state is preserved exactly as 'anvil ensure' does:
  adopted     no identity was recorded; a new ID is assigned
  unchanged   the identity already matches this path
  updated     the worktree moved; its ID (and lease) are kept, path updated
  reassigned  the state was copied from another live worktree; a new ID is
              assigned and the original's lease and process records are
              dropped from the copy
  skipped     not adopted (bare, main checkout with --all, missing directory)

Lease flags work as for 'anvil ensure': an active lease held by another owner
is never replaced (lease_held, exit 7); renew with ANVIL_LEASE_TOKEN.

--dry-run previews the actions and lease conflicts and writes nothing.
Mutating. Use --json for one versioned JSON document on stdout.`,
	Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, adoptAgentCommand, func() (agentResult, error) {
			return runAdopt(cmd, args)
		})
	},
}

type adoptEntry struct {
	Worktree worktreeSummary     `json:"worktree"`
	Action   string              `json:"action"`
	Reason   string              `json:"reason"`
	Lease    *leaseView          `json:"lease"`
	Error    *agentapi.ErrorBody `json:"error,omitempty"`
}

type adoptData struct {
	DryRun    bool         `json:"dry_run"`
	Worktrees []adoptEntry `json:"worktrees"`
}

func runAdopt(cmd *cobra.Command, args []string) (agentResult, error) {
	all := mustGetBool(cmd, "all")
	dryRun := mustGetBool(cmd, "dry-run")
	if all && len(args) > 0 {
		return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "--all and WORKTREE are mutually exclusive")
	}
	opts, err := parseLeaseOptions(cmd)
	if err != nil {
		return agentResult{}, err
	}
	pc, err := openAgentProject()
	if err != nil {
		return agentResult{}, err
	}
	var targets []git.Worktree
	if all {
		worktrees, err := git.ListWorktrees(pc.GitDir)
		if err != nil {
			return agentResult{}, agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		targets = worktrees
	} else {
		target, err := selectAgentWorktree(pc, args)
		if err != nil {
			return agentResult{}, err
		}
		targets = []git.Worktree{*target}
	}

	data := adoptData{DryRun: dryRun, Worktrees: []adoptEntry{}}
	var warnings []string
	var failures []error
	for index, wt := range targets {
		entry, entryWarnings, err := adoptOne(pc, wt, all && isMainWorktree(pc, wt, index), opts, dryRun)
		for _, warning := range entryWarnings {
			if all {
				// Several worktrees report; name each warning by its entry's reported path.
				warning = entry.Worktree.Path + ": " + warning
			}
			warnings = append(warnings, warning)
		}
		if err != nil {
			classified := agentapi.Wrap(agentapi.CodeInternal, err)
			entry.Error = &agentapi.ErrorBody{Code: classified.Code, Message: classified.Message}
			failures = append(failures, classified)
		}
		data.Worktrees = append(data.Worktrees, entry)
	}
	result := agentResult{Data: data, Warnings: warnings}
	switch len(failures) {
	case 0:
		return result, nil
	case 1:
		return result, failures[0]
	default:
		first := agentapi.Wrap(agentapi.CodeInternal, failures[0])
		return result, &agentapi.Error{Code: first.Code, Message: fmt.Sprintf("%d worktrees could not be adopted; first: %s", len(failures), first.Message), Details: first.Details, Cause: errors.Join(failures...)}
	}
}

// isMainWorktree reports whether wt is the repository's main checkout (Git
// lists it first).
func isMainWorktree(pc *ProjectContext, wt git.Worktree, index int) bool {
	return index == 0 || git.PathsEqual(wt.Path, pc.ProjectPath)
}

// adoptOne records identity and the requested lease for one worktree, or
// predicts both with dryRun.
func adoptOne(pc *ProjectContext, wt git.Worktree, skipMain bool, opts leaseOptions, dryRun bool) (adoptEntry, []string, error) {
	entry := adoptEntry{Worktree: worktreeSummary{Name: filepath.Base(wt.Path), Path: wt.Path, Branch: wt.Branch, Detached: wt.Detached, IdentityStatus: identityUnknown}, Action: adoptSkipped}
	switch {
	case wt.Bare:
		entry.Reason = "bare repository entry"
		return entry, nil, nil
	case skipMain:
		entry.Reason = "main checkout (name it explicitly to adopt it)"
		summary, warnings := summarizeWorktree(wt)
		entry.Worktree = withKnownIdentityStatus(summary)
		return entry, warnings, nil
	}
	if _, err := os.Stat(wt.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			entry.Reason = "worktree directory is missing (run 'git worktree prune', or 'git worktree repair' if it moved)"
			return entry, nil, nil
		}
		return entry, nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	now := time.Now()
	var warnings []string
	if dryRun {
		identity, status, err := wtstate.ReadIdentity(wt.Path)
		if err != nil {
			return entry, nil, agentapi.Wrap(agentapi.CodeInternal, err)
		}
		entry.Worktree, warnings = summarizeWorktree(wt)
		entry.Worktree = withKnownIdentityStatus(entry.Worktree)
		entry.Action, entry.Reason = describeAdoption(identity, status)
		entry.Lease, err = previewLeaseOptions(wt.Path, opts, now, &warnings)
		return entry, warnings, err
	}

	gate, err := acquireWorktreeUse(pc, wt)
	if err != nil {
		return entry, nil, err
	}
	// The pre-adoption record only names where a moved or copied state came
	// from; EnsureIdentity decides the action under the state lock.
	observed, _, err := wtstate.ReadIdentity(wt.Path)
	if err == nil {
		var identity wtstate.Identity
		var status wtstate.IdentityStatus
		identity, status, err = wtstate.EnsureIdentity(wt.Path)
		if err == nil {
			entry.Action, entry.Reason = describeAdoption(observed, status)
			entry.Lease, err = applyLeaseOptions(wt.Path, identity.ID, opts, "", now, &warnings)
		} else {
			err = agentapi.Wrap(agentapi.CodeInternal, err)
		}
	} else {
		err = agentapi.Wrap(agentapi.CodeInternal, err)
	}
	summary, summaryWarnings := summarizeWorktree(wt)
	entry.Worktree = withKnownIdentityStatus(summary)
	warnings = append(warnings, summaryWarnings...)
	if entry.Lease == nil && err != nil {
		// Report the lease that blocked acquisition, never a token.
		if lease, readErr := wtstate.ReadLease(wt.Path); readErr == nil {
			entry.Lease = newLeaseView(lease, now, "")
		}
	}
	return entry, warnings, errors.Join(err, gate.Release())
}

// describeAdoption maps the identity status found before adoption to the
// action adoption takes.
// identityUnknown marks a worktree whose identity could not be read (for
// example because its directory is missing).
const identityUnknown = "unknown"

func withKnownIdentityStatus(summary worktreeSummary) worktreeSummary {
	if summary.IdentityStatus == "" {
		summary.IdentityStatus = identityUnknown
	}
	return summary
}

func describeAdoption(identity *wtstate.Identity, status wtstate.IdentityStatus) (string, string) {
	switch status {
	case wtstate.IdentityOK:
		return adoptUnchanged, "identity already recorded for this path"
	case wtstate.IdentityMoved:
		from := ""
		if identity != nil {
			from = " from " + identity.Path
		}
		return adoptUpdated, "worktree moved" + from + "; ID and lease kept, path updated"
	case wtstate.IdentityCopied:
		from := ""
		if identity != nil {
			from = " from " + identity.Path
		}
		return adoptReassigned, "state copied" + from + "; new ID assigned, the original's lease and process records dropped"
	default:
		return adoptAdopted, "identity recorded"
	}
}

// previewLeaseOptions predicts applyLeaseOptions without writing: it fails
// exactly when acquisition would fail.
func previewLeaseOptions(path string, opts leaseOptions, now time.Time, warnings *[]string) (*leaseView, error) {
	lease, err := wtstate.ReadLease(path)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	view := newLeaseView(lease, now, "")
	state := lease.State(now)
	if opts.Owner == "" {
		if state == wtstate.LeaseActive {
			*warnings = append(*warnings, fmt.Sprintf("worktree is leased by %q (task %q); leases are advisory", lease.Owner, lease.TaskID))
		}
		return view, nil
	}
	// No lease record means nothing to hold or conflict with.
	holder := lease != nil && opts.Token != "" && subtle.ConstantTimeCompare([]byte(lease.TokenHash), []byte(wtstate.HashToken(opts.Token))) == 1
	switch {
	case state == wtstate.LeaseActive && !holder:
		return view, leaseAPIError(&wtstate.LeaseError{Err: wtstate.ErrLeaseHeld, Lease: lease})
	case state == wtstate.LeaseStale && !holder && !opts.Takeover:
		return view, leaseAPIError(&wtstate.LeaseError{Err: wtstate.ErrLeaseStale, Lease: lease})
	}
	return view, nil
}

func printAdoptHuman(result agentResult) error {
	data, ok := result.Data.(adoptData)
	if !ok {
		return nil
	}
	prefix := ""
	if data.DryRun {
		prefix = "[DRY RUN] "
	}
	for _, entry := range data.Worktrees {
		line := fmt.Sprintf("%s%s: %s (%s)", prefix, entry.Worktree.Name, entry.Action, entry.Reason)
		if entry.Worktree.ID != "" {
			line += " " + entry.Worktree.ID
		}
		ui.PrintInfo(line)
		if entry.Lease != nil && entry.Lease.Token != "" {
			fmt.Printf("Lease token for %s (shown once; export %s): %s\n", entry.Worktree.Name, LeaseTokenEnv, entry.Lease.Token)
		}
	}
	return nil
}

func init() {
	addJSONFlag(adoptCmd, adoptAgentCommand)
	adoptCmd.Flags().Bool("all", false, "Adopt every registered worktree of the project (except bare and the main checkout)")
	addLeaseFlags(adoptCmd)
	rootCmd.AddCommand(adoptCmd)
}
