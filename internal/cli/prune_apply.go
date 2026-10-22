package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

// Apply outcomes and skip codes.
const (
	applyRemoved     = "removed"
	applyWouldRemove = "would_remove"
	applySkipped     = "skipped"
	applyFailed      = "failed"

	applyCodeNotCandidate  = "not_candidate"
	applyCodeTargetChanged = "target_mismatch"
	applyCodeChanged       = "changed"
	applyCodeBlocked       = "blocked"
)

var pruneApplyAgent = agentCommand{Name: "prune apply", Mutating: true, DryRun: true, Human: printPruneApplyHuman}

type pruneApplyResult struct {
	Worktree worktreeSummary `json:"worktree"`
	Outcome  string          `json:"outcome"`
	Code     string          `json:"code,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Blockers []string        `json:"blockers,omitempty"`
	// Warnings are the latest evaluation's nonfatal findings.
	Warnings []string `json:"warnings"`
}

type pruneApplyData struct {
	DryRun     bool               `json:"dry_run"`
	From       string             `json:"from"`
	Repository pruneRepository    `json:"repository"`
	Settings   pruneSettings      `json:"settings"`
	Results    []pruneApplyResult `json:"results"`
}

// applySkip is a refusal that leaves the worktree untouched.
type applySkip struct {
	code     string
	reason   string
	blockers []string
}

func (e *applySkip) Error() string { return e.reason }

func newPruneApplyCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "apply --from FILE",
		Short: "Remove the candidates of a prune candidate file after rechecking them",
		Long: `Remove the worktrees a candidate file (from 'anvil prune candidates
--output FILE', or its --json envelope) marked candidate=true.

The file is validated (schema anvil.prune-candidates/v1, same Git common
directory as the current linked project). Each entry must still be a
registered worktree of this repository at the canonical path in the file,
with the same Anvil worktree ID, branch and HEAD; otherwise it is skipped
(target_mismatch or changed). All evidence is then collected again, and once
more under the exclusive lifecycle gate immediately before removal; any
blocker skips the entry. A stale or edited file can never override current
safety checks: the thresholds used are this command's flags, only ever
strengthened by the file's (protected patterns are combined, the larger
--recent wins (minimum 10m), --strict-processes if either set it, and an
ignored path may be deleted only if both the file and --allow-ignored or
prune.allow_ignored allow it). A file listing a path or worktree ID twice
is refused. Process-detection gaps are those of 'anvil prune candidates'.

Removal uses the same lifecycle as 'anvil remove' (lease, checks,
preparation and database borrower checks included); --ignore-lease is never
applied and branches are kept. Without --dry-run, --force is required
(invalid_arguments otherwise), as for 'anvil remove --json'. Per-entry outcome: removed, would_remove
(--dry-run), skipped (with code and reason) or failed.

Residual race: a process that starts using a worktree outside Anvil after
the final recheck and before 'git worktree remove' completes is not seen.
Anvil commands (ensure, check, lease) are excluded by the lifecycle gate.`,
		Args: func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.NoArgs(cmd, args)) },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentCommand(cmd, pruneApplyAgent, func() (agentResult, error) {
				return runPruneApply(cmd)
			})
		},
	}
	addJSONFlag(command, pruneApplyAgent)
	addPruneEvidenceFlags(command)
	command.Flags().String("from", "", "Candidate file written by 'anvil prune candidates --output'")
	command.Flags().Bool("keep-db", false, "Keep owned databases and parallel-worker databases")
	command.Flags().BoolP("force", "f", false, "Required to remove anything (never prompts); preview with --dry-run instead")
	return command
}

// readCandidateFile accepts the candidate document or a --json envelope
// carrying it.
func readCandidateFile(path string) (pruneCandidateFile, error) {
	var doc pruneCandidateFile
	content, err := os.ReadFile(path)
	if err != nil {
		return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "reading candidate file: %v", err)
	}
	var probe struct {
		Schema string          `json:"schema"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(content, &probe); err != nil {
		return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file is not JSON: %v", err)
	}
	if probe.Schema == agentapi.SchemaVersion {
		content = probe.Data
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "malformed candidate file: %v", err)
	}
	if doc.Schema != pruneCandidatesSchema {
		return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file schema is %q, want %q", doc.Schema, pruneCandidatesSchema)
	}
	if doc.Repository.CommonDir == "" {
		return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file names no repository")
	}
	paths, ids := map[string]bool{}, map[string]bool{}
	for _, entry := range doc.Worktrees {
		if path := entry.Worktree.Path; path != "" {
			if paths[path] {
				return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file lists path %s more than once (duplicate entries are refused)", path)
			}
			paths[path] = true
		}
		if id := entry.Worktree.ID; id != "" {
			if ids[id] {
				return doc, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file lists worktree ID %s more than once (duplicate entries are refused)", id)
			}
			ids[id] = true
		}
	}
	return doc, nil
}

func runPruneApply(cmd *cobra.Command) (agentResult, error) {
	from := mustGetString(cmd, "from")
	if from == "" {
		return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "--from FILE is required")
	}
	if _, err := parseRecentWindow(cmd); err != nil {
		return agentResult{}, err
	}
	if !mustGetBool(cmd, "force") && !mustGetBool(cmd, "dry-run") {
		return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "prune apply removes worktrees: pass --force, or preview with --dry-run")
	}
	doc, err := readCandidateFile(from)
	if err != nil {
		return agentResult{}, err
	}
	pc, err := openPruneProject(mustGetString(cmd, "project"))
	if err != nil {
		return agentResult{}, err
	}
	settings, err := pruneSettingsFromFlags(cmd, pc)
	if err != nil {
		return agentResult{}, err
	}
	repo, err := pruneRepositoryOf(pc)
	if err != nil {
		return agentResult{}, err
	}
	if doc.Repository.CommonDir != repo.CommonDir {
		return agentResult{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file belongs to repository %s, not the current project's %s", doc.Repository.CommonDir, repo.CommonDir)
	}
	// The file may only strengthen the current safety settings.
	if err := validateFileRecentWindow(doc.Settings.RecentSeconds); err != nil {
		return agentResult{}, err
	}
	if err := validateFilePatterns(doc.Settings.Protected); err != nil {
		return agentResult{}, err
	}
	if err := validateFilePatterns(doc.Settings.AllowIgnored); err != nil {
		return agentResult{}, err
	}
	// Ignored paths may be deleted only when both the file and this
	// invocation allow them.
	settings.AllowIgnored = intersectStrings(settings.AllowIgnored, doc.Settings.AllowIgnored)
	settings.Protected = uniqueStrings(append(settings.Protected, doc.Settings.Protected...))
	settings.RecentSeconds = max(settings.RecentSeconds, doc.Settings.RecentSeconds)
	settings.StrictProcesses = settings.StrictProcesses || doc.Settings.StrictProcesses
	// settings.AllowUninspectable stays this invocation's own: a file never
	// relaxes process inspection.

	dryRun := mustGetBool(cmd, "dry-run")
	data := pruneApplyData{DryRun: dryRun, From: from, Repository: repo, Settings: settings, Results: make([]pruneApplyResult, len(doc.Worktrees))}
	// Every recheck takes a fresh open-file snapshot.
	scan := newPruneScanFor(pc, repo, settings, procs.ScanOpenFiles)

	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return agentResult{Data: data}, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	var targets []git.Worktree
	index := map[string]int{}
	for i, entry := range doc.Worktrees {
		data.Results[i] = pruneApplyResult{Worktree: entry.Worktree, Warnings: []string{}}
		if !entry.Candidate {
			data.Results[i].skip(&applySkip{code: applyCodeNotCandidate, reason: "not a candidate when the file was generated"})
			continue
		}
		target, warnings, skip := recheckApplyTarget(pc, scan, entry, worktrees)
		data.Results[i].Warnings = append(data.Results[i].Warnings, warnings...)
		if skip != nil {
			data.Results[i].skip(skip)
			continue
		}
		index[target.Path] = i
		targets = append(targets, *target)
	}
	if err := orderDatabaseRemovals(targets); err != nil {
		return agentResult{Data: data}, agentapi.Wrap(agentapi.CodeCleanupBlocked, err)
	}

	keepDB := mustGetBool(cmd, "keep-db")
	verbose, quiet := mustGetBool(cmd, "verbose"), mustGetBool(cmd, "quiet")
	deps := defaultRemoveCommandDependencies().lifecycleDependencies()
	var failures []error
	for _, target := range targets {
		i := index[target.Path]
		entry := doc.Worktrees[i]
		ui.PrintStep(fmt.Sprintf("Removing %s...", entry.Worktree.Name))
		warnings, err := applyRemoval(pc, scan, entry, target, removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet}, keepDB, deps)
		if warnings != nil {
			data.Results[i].Warnings = warnings
		}
		var skip *applySkip
		switch {
		case errors.As(err, &skip):
			data.Results[i].skip(skip)
		case err != nil:
			classified := agentapi.Wrap(agentapi.CodeInternal, classifyRemoveError(err))
			if classified.Code == agentapi.CodeLeaseHeld || classified.Code == agentapi.CodeCleanupBlocked {
				// Refused before any effect.
				data.Results[i].skip(&applySkip{code: classified.Code, reason: classified.Message})
				continue
			}
			data.Results[i].Outcome, data.Results[i].Code, data.Results[i].Reason = applyFailed, classified.Code, classified.Message
			failures = append(failures, fmt.Errorf("%s: %w", target.Path, classified))
		case dryRun:
			data.Results[i].Outcome = applyWouldRemove
		default:
			data.Results[i].Outcome = applyRemoved
			ui.PrintSuccessPath("Removed", target.Path)
		}
	}
	if len(failures) > 0 {
		first := agentapi.Wrap(agentapi.CodeInternal, failures[0])
		return agentResult{Data: data}, &agentapi.Error{Code: first.Code, Message: fmt.Sprintf("%d removal(s) failed; first: %v", len(failures), failures[0]), Cause: errors.Join(failures...)}
	}
	return agentResult{Data: data}, nil
}

func validateFilePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := filepath.Match(pattern, ""); err != nil || pattern == "" {
			return agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file has an invalid pattern %q", pattern)
		}
	}
	return nil
}

func validateFileRecentWindow(seconds int64) error {
	minimum := int64(minimumRecentActivity / time.Second)
	maximum := (1<<63 - 1) / int64(time.Second)
	if seconds < minimum || seconds > maximum {
		return agentapi.Errorf(agentapi.CodeInvalidArguments, "candidate file recent_seconds %d is outside the supported range %d..%d", seconds, minimum, maximum)
	}
	return nil
}

func intersectStrings(values, allowed []string) []string {
	out := []string{}
	for _, value := range values {
		if slices.Contains(allowed, value) {
			out = append(out, value)
		}
	}
	return out
}

func (r *pruneApplyResult) skip(skip *applySkip) {
	r.Outcome, r.Code, r.Reason, r.Blockers = applySkipped, skip.code, skip.reason, skip.blockers
}

// applyRemoval removes one revalidated target through the remove lifecycle,
// rechecking target and evidence again under the exclusive gate.
// It returns the in-gate evaluation's warnings (nil when it did not run).
func applyRemoval(pc *ProjectContext, scan pruneScan, entry pruneCandidate, target git.Worktree, options removeLifecycleOptions, keepDB bool, deps removeLifecycleDependencies) ([]string, error) {
	cleanupOpts, messages, err := planWorktreeCleanup(target.Path, keepDB, options.DryRun, false, deps.readLocalState)
	if err != nil {
		return nil, removeRefusal{code: agentapi.CodeCleanupBlocked, err: err}
	}
	var warnings []string
	options.IgnoreLease = false
	options.Recheck = func() error {
		worktrees, err := git.ListWorktrees(pc.GitDir)
		if err != nil {
			return agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		_, current, skip := recheckApplyTarget(pc, scan, entry, worktrees)
		warnings = append([]string{}, current...)
		if skip != nil {
			return skip
		}
		return nil
	}
	err = runRemoveLifecycle(pc, target, options, cleanupOpts, messages, deps)
	return warnings, err
}

// recheckApplyTarget validates one file entry against the current
// repository and collects all evidence again.
func recheckApplyTarget(pc *ProjectContext, scan pruneScan, entry pruneCandidate, worktrees []git.Worktree) (*git.Worktree, []string, *applySkip) {
	mismatch := func(format string, args ...any) (*git.Worktree, []string, *applySkip) {
		return nil, nil, &applySkip{code: applyCodeTargetChanged, reason: fmt.Sprintf(format, args...)}
	}
	path := entry.Worktree.Path
	if path == "" || !filepath.IsAbs(path) {
		return mismatch("path %q is not absolute", path)
	}
	canonical, err := wtstate.CanonicalPath(path)
	if err != nil {
		return mismatch("path %s cannot be resolved: %v", path, err)
	}
	if canonical != path {
		return mismatch("path %s is not canonical (resolves to %s)", path, canonical)
	}
	var target *git.Worktree
	var main bool
	for i, wt := range worktrees {
		if wt.Bare {
			continue
		}
		if registered, err := wtstate.CanonicalPath(wt.Path); err == nil && registered == canonical {
			copied := wt
			target, main = &copied, isMainWorktree(pc, wt, i)
			break
		}
	}
	if target == nil {
		return mismatch("%s is not a registered worktree of this repository", path)
	}
	identity, status, err := wtstate.ReadIdentity(canonical)
	if err != nil {
		return mismatch("cannot read worktree identity: %v", err)
	}
	if entry.Worktree.ID == "" || identity == nil || status != wtstate.IdentityOK || identity.ID != entry.Worktree.ID {
		current := "none"
		if identity != nil {
			current = identity.ID + " (" + string(status) + ")"
		}
		return mismatch("worktree ID %q in the file does not match the current identity %s", entry.Worktree.ID, current)
	}
	if target.Branch != entry.Worktree.Branch || target.Detached != entry.Worktree.Detached {
		return nil, nil, &applySkip{code: applyCodeChanged, reason: fmt.Sprintf("branch changed since the candidate file was generated (was %q, now %q)", entry.Worktree.Branch, target.Branch)}
	}
	head, err := git.HeadCommit(canonical)
	if err != nil {
		return nil, nil, &applySkip{code: applyCodeBlocked, reason: "evidence unavailable: HEAD: " + err.Error()}
	}
	if head != entry.Worktree.Head {
		return nil, nil, &applySkip{code: applyCodeChanged, reason: fmt.Sprintf("HEAD changed since the candidate file was generated (was %s, now %s)", entry.Worktree.Head, head)}
	}
	summary, warnings := summarizeWorktree(*target)
	current := scan.evaluate(*target, main, summary)
	warnings = append(warnings, current.Warnings...)
	if !current.Candidate {
		return nil, warnings, &applySkip{code: applyCodeBlocked, reason: "blocked by current evidence: " + strings.Join(current.Blockers, "; "), blockers: current.Blockers}
	}
	return target, warnings, nil
}

func printPruneApplyHuman(result agentResult) error {
	data, ok := result.Data.(pruneApplyData)
	if !ok {
		return nil
	}
	removed := 0
	for _, entry := range data.Results {
		switch entry.Outcome {
		case applyRemoved, applyWouldRemove:
			removed++
			ui.PrintSuccess(fmt.Sprintf("%s: %s", entry.Worktree.Name, entry.Outcome))
		default:
			ui.PrintInfo(fmt.Sprintf("%s: %s (%s: %s)", entry.Worktree.Name, entry.Outcome, entry.Code, entry.Reason))
		}
	}
	verb := "removed"
	if data.DryRun {
		verb = "would be removed"
	}
	ui.PrintDone(fmt.Sprintf("%d of %d worktree(s) %s", removed, len(data.Results), verb))
	return nil
}
