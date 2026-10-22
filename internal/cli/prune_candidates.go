package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

// pruneCandidatesSchema versions the candidate file `prune apply` reads.
const pruneCandidatesSchema = "anvil.prune-candidates/v1"

// defaultRecentActivity is the default --recent threshold.
const defaultRecentActivity = 24 * time.Hour

// minimumRecentActivity is the smallest accepted --recent window; the
// recent-activity guarantee cannot be switched off.
const minimumRecentActivity = 10 * time.Minute

// Worktree kinds in a candidate file; only "branch" can be a candidate.
const (
	kindBranch        = "branch"
	kindMain          = "main"
	kindBare          = "bare"
	kindDetached      = "detached"
	kindLocked        = "locked"
	kindMissing       = "missing"
	kindDefaultBranch = "default_branch"
)

type pruneRepository struct {
	CommonDir     string `json:"common_dir"`
	Project       string `json:"project"`
	ProjectPath   string `json:"project_path"`
	DefaultBranch string `json:"default_branch"`
	MergeTarget   string `json:"merge_target"`
}

// pruneSettings record the thresholds a file was generated with. Apply
// only ever strengthens its own settings with them.
type pruneSettings struct {
	RecentSeconds   int64    `json:"recent_seconds"`
	Protected       []string `json:"protected"`
	Fetched         bool     `json:"fetched"`
	PRLookup        bool     `json:"pr_lookup"`
	StrictProcesses bool     `json:"strict_processes"`
	// AllowIgnored holds the gitignored paths removal may delete. Apply
	// uses the intersection of the file's and its own.
	AllowIgnored []string `json:"allow_ignored"`
	// AllowUninspectable names own processes (by command) the operator
	// vouches cannot use the worktree. Apply never takes it from a file.
	AllowUninspectable []string `json:"allow_uninspectable"`
}

type pruneCandidateFile struct {
	Schema      string           `json:"schema"`
	GeneratedAt time.Time        `json:"generated_at"`
	Repository  pruneRepository  `json:"repository"`
	Settings    pruneSettings    `json:"settings"`
	Worktrees   []pruneCandidate `json:"worktrees"`
}

type pruneEvidence struct {
	Merge     mergeEvidence     `json:"merge"`
	PR        prEvidence        `json:"pr"`
	Remote    remoteEvidence    `json:"remote"`
	Tracked   countEvidence     `json:"tracked"`
	Untracked untrackedEvidence `json:"untracked"`
	Ignored   ignoredEvidence   `json:"ignored"`
	Lease     leaseEvidence     `json:"lease"`
	Protected protectedEvidence `json:"protected"`
	Activity  activityEvidence  `json:"activity"`
	OpenFiles openFilesEvidence `json:"open_files"`
}

type pruneCandidate struct {
	Worktree  worktreeSummary `json:"worktree"`
	Kind      string          `json:"kind"`
	Candidate bool            `json:"candidate"`
	Reasons   []string        `json:"reasons"`
	Blockers  []string        `json:"blockers"`
	Warnings  []string        `json:"warnings"`
	Evidence  *pruneEvidence  `json:"evidence,omitempty"`
}

// pruneScan evaluates worktrees of one repository.
type pruneScan struct {
	DefaultBranch string
	MergeTarget   string
	Settings      pruneSettings
	collectors    pruneCollectors
}

func (s pruneScan) recent() time.Duration {
	return time.Duration(s.Settings.RecentSeconds) * time.Second
}

// evaluate collects all evidence for one worktree and decides whether it is
// a candidate. Missing, failed or unsupported evidence is a blocker: it is
// never taken as proof of inactivity.
func (s pruneScan) evaluate(wt git.Worktree, isMain bool, summary worktreeSummary) pruneCandidate {
	c := pruneCandidate{Worktree: summary, Kind: kindBranch, Reasons: []string{}, Blockers: []string{}, Warnings: []string{}}
	if c.Worktree.Name == "" {
		c.Worktree.Name, c.Worktree.Path = filepath.Base(wt.Path), wt.Path
	}
	block := func(format string, args ...any) { c.Blockers = append(c.Blockers, fmt.Sprintf(format, args...)) }
	switch {
	case wt.Bare:
		c.Kind = kindBare
		block("bare repository entry")
	case isMain:
		c.Kind = kindMain
		block("main checkout is never pruned")
	case wt.Detached:
		c.Kind = kindDetached
		block("detached HEAD: no branch to evaluate (remove it with 'anvil remove')")
	case wt.Locked:
		c.Kind = kindLocked
		if wt.LockReason != "" {
			block("locked worktree (%s)", wt.LockReason)
		} else {
			block("locked worktree")
		}
	case wt.Branch == s.DefaultBranch:
		c.Kind = kindDefaultBranch
		block("default branch %q is always protected", s.DefaultBranch)
	}
	if c.Kind != kindBranch {
		return c
	}
	if _, err := os.Stat(wt.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Kind = kindMissing
			block("worktree directory is missing (run 'git worktree prune')")
			return c
		}
		block("evidence unavailable: worktree directory: %v", err)
		return c
	}
	switch {
	case summary.IdentityStatus == "":
		block("evidence unavailable: identity: .anvil.local cannot be read")
	case summary.IdentityStatus == string(wtstate.IdentityMissing):
		block("no Anvil identity (run 'anvil adopt %s')", c.Worktree.Name)
	case summary.IdentityStatus != string(wtstate.IdentityOK) || summary.ID == "":
		block("worktree identity is %s (run 'anvil adopt %s')", summary.IdentityStatus, c.Worktree.Name)
	}
	if summary.Head == "" {
		block("evidence unavailable: HEAD commit could not be read")
	}

	ev := &pruneEvidence{
		Merge:     s.collectors.merge(wt.Branch),
		PR:        s.collectors.pr(wt.Branch),
		Remote:    s.collectors.remote(wt.Branch),
		Tracked:   s.collectors.tracked(wt.Path),
		Untracked: s.collectors.untracked(wt.Path),
		Ignored:   s.collectors.ignored(wt.Path),
		Lease:     s.collectors.lease(wt.Path),
		Protected: s.protectedBy(wt),
		Activity:  s.collectors.activity(wt.Path, summary.ID),
		OpenFiles: s.collectors.openFiles(wt.Path),
	}
	c.Evidence = ev
	reason := func(format string, args ...any) { c.Reasons = append(c.Reasons, fmt.Sprintf(format, args...)) }
	unavailable := func(what, err string) { block("evidence unavailable: %s: %s", what, err) }

	switch {
	case ev.Merge.State == mergeMerged:
		reason("branch merged into %s", ev.Merge.Target)
	case ev.Merge.State == mergeNotMerged && ev.PR.State == "merged" && ev.PR.HeadOID != "" && ev.PR.HeadOID == summary.Head:
		reason("pull request #%d merged at the branch's current head (squash or rebase merge)", ev.PR.Number)
	case ev.Merge.State == mergeNotMerged:
		block("branch not merged into %s", ev.Merge.Target)
	default:
		unavailable("merge state", orUnknown(ev.Merge.Error))
	}
	if ev.PR.State == "open" {
		block("pull request #%d is open", ev.PR.Number)
	}
	switch ev.Remote.State {
	case "pushed":
		reason("all commits exist on a remote")
	case "unpushed":
		block("%d commit(s) not on any remote", ev.Remote.UnpushedCommits)
	default:
		unavailable("remote commits", orUnknown(ev.Remote.Error))
	}
	switch {
	case ev.Tracked.Error != "":
		unavailable("tracked changes", ev.Tracked.Error)
	case ev.Tracked.Count > 0:
		block("%d tracked change(s)", ev.Tracked.Count)
	default:
		reason("no tracked changes")
	}
	switch {
	case ev.Untracked.Error != "":
		unavailable("untracked files", ev.Untracked.Error)
	case ev.Untracked.Count > 0:
		block("%d untracked file(s) outside node_modules (%s)", ev.Untracked.Count, strings.Join(ev.Untracked.Sample, ", "))
	default:
		reason("no untracked files outside node_modules")
	}
	switch {
	case ev.Ignored.Error != "":
		unavailable("ignored files", ev.Ignored.Error)
	case ev.Ignored.Count > 0:
		block("%d ignored path(s) would be deleted (%s); allow them with --allow-ignored GLOB or prune.allow_ignored", ev.Ignored.Count, strings.Join(ev.Ignored.Sample, ", "))
	default:
		reason("no ignored files outside node_modules and vendor (besides allowed ones)")
	}
	switch ev.Lease.State {
	case wtstate.LeaseNone:
		reason("no active lease")
	case wtstate.LeaseActive:
		block("active lease by %q (task %q) until %s", ev.Lease.Owner, ev.Lease.TaskID, ev.Lease.ExpiresAt.Format(time.RFC3339))
	case wtstate.LeaseStale:
		reason("no active lease")
		c.Warnings = append(c.Warnings, fmt.Sprintf("stale lease by %q (task %q)", ev.Lease.Owner, ev.Lease.TaskID))
	default:
		unavailable("lease", orUnknown(ev.Lease.Error))
	}
	if ev.Protected.Protected {
		block("protected by pattern %q (%s)", ev.Protected.Pattern, ev.Protected.Matched)
	}
	switch {
	case ev.Activity.Error != "" || ev.Activity.State == mergeUnknown:
		unavailable("recent activity", orUnknown(ev.Activity.Error))
	case len(ev.Activity.RunningChecks) > 0:
		block("checks running: %s", strings.Join(ev.Activity.RunningChecks, ", "))
	case ev.Activity.State == "recent":
		block("recent activity: %s at %s (within %s)", ev.Activity.LatestSource, ev.Activity.LatestAt.Format(time.RFC3339), s.recent())
	default:
		reason("no activity within %s", s.recent())
	}
	c.Warnings = append(c.Warnings, ev.Activity.Warnings...)
	s.assessOpenFiles(&c, ev.OpenFiles)
	c.Candidate = len(c.Blockers) == 0
	return c
}

func (s pruneScan) assessOpenFiles(c *pruneCandidate, ev openFilesEvidence) {
	block := func(format string, args ...any) { c.Blockers = append(c.Blockers, fmt.Sprintf(format, args...)) }
	if ev.Error != "" {
		block("evidence unavailable: open files: %s", ev.Error)
		return
	}
	if ev.Method != procs.MethodProcfs && ev.Method != procs.MethodLsof {
		block("evidence unavailable: open files: unsupported inspection method %q", ev.Method)
		return
	}
	var unknown, allowed []string
	for _, process := range ev.UninspectableOwn {
		label := fmt.Sprintf("pid %d (%s)", process.PID, orUnknown(process.Command))
		if process.Command != "" && slices.Contains(s.Settings.AllowUninspectable, process.Command) {
			allowed = append(allowed, label)
		} else {
			unknown = append(unknown, label)
		}
	}
	if len(unknown) > 0 {
		block("evidence unavailable: open files: %d of your own processes could not be inspected: %s (if they cannot use this worktree, pass --allow-uninspectable NAME)", len(unknown), strings.Join(unknown, ", "))
	}
	if len(allowed) > 0 {
		c.Warnings = append(c.Warnings, "not inspected (--allow-uninspectable): "+strings.Join(allowed, ", "))
	}
	for _, process := range ev.ForeignNamespaceOwn {
		message := fmt.Sprintf("process %d (%s) runs in another mount namespace; its open files cannot be mapped to this worktree", process.PID, orUnknown(process.Command))
		if process.Command != "" && slices.Contains(s.Settings.AllowUninspectable, process.Command) {
			c.Warnings = append(c.Warnings, message+" (allowed by --allow-uninspectable)")
		} else {
			block("%s (use leases for containers sharing worktrees; pass --allow-uninspectable NAME if it cannot use any worktree)", message)
			unknown = append(unknown, process.Command)
		}
	}
	switch {
	case ev.SkippedOtherUsers == nil:
		// lsof silently omits processes it may not inspect.
		if s.Settings.StrictProcesses {
			block("evidence unavailable: open files: other users' processes cannot be inspected on this platform (--strict-processes)")
		} else {
			c.Warnings = append(c.Warnings, "other users' processes are invisible to lsof; files they hold open (e.g. root-owned containers) are not detected")
		}
	case *ev.SkippedOtherUsers > 0:
		message := fmt.Sprintf("%d processes of other users could not be inspected", *ev.SkippedOtherUsers)
		if s.Settings.StrictProcesses {
			block("evidence unavailable: open files: %s (--strict-processes)", message)
		} else {
			c.Warnings = append(c.Warnings, message+"; files they hold open (e.g. root-owned containers) are not detected")
		}
	}
	if len(ev.Holders) > 0 {
		holders := make([]string, 0, len(ev.Holders))
		for _, holder := range ev.Holders {
			holders = append(holders, fmt.Sprintf("pid %d (%s) via %s", holder.PID, holder.Command, strings.Join(holder.Via, "+")))
		}
		block("%d process(es) hold files open beneath the worktree: %s", len(ev.Holders), strings.Join(holders, "; "))
	}
	if len(ev.Holders) == 0 && len(unknown) == 0 {
		c.Reasons = append(c.Reasons, "no inspected process holds files open beneath the worktree")
	}
}

// protectedBy matches the configured patterns against the branch and the
// folder name.
func (s pruneScan) protectedBy(wt git.Worktree) protectedEvidence {
	for _, pattern := range s.Settings.Protected {
		if matched, err := path.Match(pattern, wt.Branch); err == nil && matched {
			return protectedEvidence{Protected: true, Pattern: pattern, Matched: "branch"}
		}
		if matched, err := path.Match(pattern, filepath.Base(wt.Path)); err == nil && matched {
			return protectedEvidence{Protected: true, Pattern: pattern, Matched: "name"}
		}
	}
	return protectedEvidence{}
}

func orUnknown(message string) string {
	if message == "" {
		return "unknown"
	}
	return message
}

// openPruneProject opens --project NAME or the project of the current
// directory.
func openPruneProject(name string) (*ProjectContext, error) {
	if name == "" {
		return openAgentProject()
	}
	globalCfg, err := config.LoadOrCreateGlobalConfig()
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
	}
	info := globalCfg.GetLinkedProjectByName(name)
	if info == nil {
		return nil, agentapi.Errorf(agentapi.CodeProjectNotFound, "no linked project named %q", name)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	pc, err := openProject(cwd, name, info, globalCfg)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeProjectNotFound, err)
	}
	return pc, nil
}

func pruneRepositoryOf(pc *ProjectContext) (pruneRepository, error) {
	commonDir, err := dblifecycle.CommonDir(pc.ProjectPath)
	if err != nil {
		return pruneRepository{}, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	return pruneRepository{
		CommonDir: commonDir, Project: pc.ProjectName, ProjectPath: pc.ProjectPath,
		DefaultBranch: pc.DefaultBranch, MergeTarget: config.DefaultRemote + "/" + pc.DefaultBranch,
	}, nil
}

// pruneSettingsFromFlags reads the shared evidence flags and the protected
// patterns of the global and project configuration.
func pruneSettingsFromFlags(cmd *cobra.Command, pc *ProjectContext) (pruneSettings, error) {
	recent, err := parseRecentWindow(cmd)
	if err != nil {
		return pruneSettings{}, err
	}
	protect, err := cmd.Flags().GetStringArray("protect")
	if err != nil {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	if err := config.ValidateProtectedPatterns("--protect", protect); err != nil {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	allow, err := cmd.Flags().GetStringArray("allow-uninspectable")
	if err != nil {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	allowIgnored, err := cmd.Flags().GetStringArray("allow-ignored")
	if err != nil {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	if err := config.ValidateProtectedPatterns("--allow-ignored", allowIgnored); err != nil {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	var patterns, ignoredPatterns []string
	if pc.GlobalConfig != nil {
		if err := pc.GlobalConfig.Prune.Validate(); err != nil {
			return pruneSettings{}, agentapi.Wrap(agentapi.CodeConfigInvalid, fmt.Errorf("global configuration: %w", err))
		}
		patterns = append(patterns, pc.GlobalConfig.Prune.Protected...)
		ignoredPatterns = append(ignoredPatterns, pc.GlobalConfig.Prune.AllowIgnored...)
	}
	if _, statErr := os.Stat(filepath.Join(pc.ProjectPath, config.ProjectConfigFile)); statErr == nil {
		cfg, err := config.LoadProject(pc.ProjectPath)
		if err != nil {
			return pruneSettings{}, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
		}
		patterns = append(patterns, cfg.Prune.Protected...)
		ignoredPatterns = append(ignoredPatterns, cfg.Prune.AllowIgnored...)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return pruneSettings{}, agentapi.Wrap(agentapi.CodeConfigInvalid, statErr)
	}
	return pruneSettings{
		RecentSeconds:      int64(recent / time.Second),
		Protected:          uniqueStrings(append(patterns, protect...)),
		PRLookup:           !mustGetBool(cmd, "no-pr"),
		StrictProcesses:    mustGetBool(cmd, "strict-processes"),
		AllowUninspectable: uniqueStrings(allow),
		AllowIgnored:       uniqueStrings(append(ignoredPatterns, allowIgnored...)),
	}, nil
}

// parseRecentWindow reads --recent and enforces the minimum window.
func parseRecentWindow(cmd *cobra.Command) (time.Duration, error) {
	recent, err := cmd.Flags().GetDuration("recent")
	if err != nil {
		return 0, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	if recent < minimumRecentActivity {
		return 0, agentapi.Errorf(agentapi.CodeInvalidArguments, "--recent must be at least %s (got %s); the recent-activity check cannot be disabled", minimumRecentActivity, recent)
	}
	return recent, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func newPruneScanFor(pc *ProjectContext, repo pruneRepository, settings pruneSettings, snapshot func() (procs.Snapshot, error)) pruneScan {
	scan := pruneScan{DefaultBranch: repo.DefaultBranch, MergeTarget: repo.MergeTarget, Settings: settings}
	scan.collectors = defaultPruneCollectors(collectorOptions{
		GitDir: pc.GitDir, RepoPath: pc.ProjectPath, MergeTarget: repo.MergeTarget,
		DefaultBranch: repo.DefaultBranch, AllowIgnored: settings.AllowIgnored,
		Recent: scan.recent(), PRLookup: settings.PRLookup, Snapshot: snapshot, Now: time.Now,
	})
	return scan
}

func addPruneEvidenceFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", "Linked project name (default: the project of the current directory)")
	cmd.Flags().Duration("recent", defaultRecentActivity, "Activity newer than this blocks pruning (minimum 10m)")
	cmd.Flags().StringArray("protect", nil, "Glob matched against branch and folder name that is never pruned (repeatable)")
	cmd.Flags().Bool("no-pr", false, "Skip the GitHub pull request lookup (gh)")
	cmd.Flags().Bool("strict-processes", false, "Also block when other users' processes cannot be inspected")
	cmd.Flags().StringArray("allow-uninspectable", nil, "Command name (comm) of an own process that cannot be inspected but cannot use any worktree (repeatable)")
	cmd.Flags().StringArray("allow-ignored", nil, "Glob for gitignored paths (relative to the worktree, or a parent directory) that may be deleted, e.g. .env (repeatable)")
}

var pruneCandidatesAgent = agentCommand{Name: "prune candidates", Human: printPruneCandidatesHuman}

func newPruneCandidatesCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "candidates",
		Short: "List prune candidates with safety evidence (read-only)",
		Long: `List every worktree of one linked project with whether it is safe to prune
(candidate), why (reasons[]) and why not (blockers[]), plus the evidence:

  - merge state of the branch against origin/<default> (merged, not_merged,
    unknown), and the GitHub pull request via 'gh pr list' when gh works
    (an open PR blocks; an unavailable lookup does not; only a PR merged into
    the default branch counts as merged)
  - whether every commit exists on a remote (git rev-list --not --remotes)
  - tracked changes and untracked files (node_modules excluded)
  - gitignored paths removal would delete (.env, SQLite files, storage/ ...;
    node_modules, vendor and .anvil.local excluded): any path not allowed by
    --allow-ignored GLOB or prune.allow_ignored blocks, e.g.
    'anvil prune candidates --allow-ignored .env --allow-ignored storage'
  - the advisory lease (active blocks; stale warns)
  - protected patterns: prune.protected in the global or project anvil.yaml
    and --protect (glob on branch or folder name); the default branch always
  - recent activity: newest HEAD reflog entry (a missing reflog only warns),
    tracked, untracked and ignored file mtimes (node_modules, vendor and .git
    excluded), lease renewal, check result, preparation record and
    .anvil.local modification, within --recent (default 24h,
    minimum 10m; the check cannot be disabled)
  - processes holding anything beneath the worktree open: cwd, root, exe,
    file descriptors or memory maps (/proc on Linux, lsof on macOS)

Evidence that is missing, failed or unsupported is a blocker, never proof of
inactivity. Git runs with --no-optional-locks, so the scan never rewrites
an index.

Known gaps in process detection: other users' processes that cannot be
inspected (on Linux counted as skipped_other_users; on macOS invisible to
lsof and reported as null) only warn by default, so a root-owned container
(Docker, Sail) or another account using the worktree is NOT detected unless
--strict-processes is given (which then blocks; on macOS it always blocks).
Processes in another mount namespace (containers, snap/flatpak apps) see
namespace-local paths that cannot be mapped to this worktree: your own block
("runs in another mount namespace"), other users' follow the rule above.
Kernel threads are ignored. Blocking other users' processes by default would make
prune unusable for non-root users, because system daemons are always
uninspectable. Containers sharing worktrees should hold leases. Your own
uninspectable processes block; --allow-uninspectable NAME exempts every
process with that command name (the kernel's comm, truncated to 15
characters and settable by any process of the same user) in every worktree,
which snap/flatpak apps may need.

The main checkout, bare, detached, locked and missing worktrees
and the default branch are listed as non-candidates. Worktrees without an
Anvil identity are blocked until adopted ('anvil adopt --all').

Read-only: never fetches unless --fetch is given (then 'git fetch origin').
--output FILE also writes the candidate document (schema
anvil.prune-candidates/v1) for 'anvil prune apply --from FILE'.`,
		Args: func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.NoArgs(cmd, args)) },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentCommand(cmd, pruneCandidatesAgent, func() (agentResult, error) {
				return runPruneCandidates(cmd)
			})
		},
	}
	addJSONFlag(command, pruneCandidatesAgent)
	addPruneEvidenceFlags(command)
	command.Flags().String("output", "", "Also write the candidate document to this file")
	command.Flags().Bool("fetch", false, "Run 'git fetch origin' before evaluating merge state")
	return command
}

func runPruneCandidates(cmd *cobra.Command) (agentResult, error) {
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
	if mustGetBool(cmd, "fetch") {
		if err := git.FetchOrigin(pc.GitDir); err != nil {
			return agentResult{}, agentapi.Wrap(agentapi.CodeGitFailed, err)
		}
		settings.Fetched = true
	}
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return agentResult{}, agentapi.Wrap(agentapi.CodeGitFailed, err)
	}
	scan := newPruneScanFor(pc, repo, settings, sharedSnapshot())
	doc := pruneCandidateFile{Schema: pruneCandidatesSchema, GeneratedAt: time.Now().UTC(), Repository: repo, Settings: settings, Worktrees: []pruneCandidate{}}
	var warnings []string
	for index, wt := range worktrees {
		summary := worktreeSummary{Name: filepath.Base(wt.Path), Path: wt.Path, Branch: wt.Branch, Detached: wt.Detached}
		if _, statErr := os.Stat(wt.Path); !wt.Bare && statErr == nil {
			var summaryWarnings []string
			summary, summaryWarnings = summarizeWorktree(wt)
			warnings = append(warnings, summaryWarnings...)
		}
		doc.Worktrees = append(doc.Worktrees, scan.evaluate(wt, isMainWorktree(pc, wt, index), summary))
	}
	if output := mustGetString(cmd, "output"); output != "" {
		if err := writeCandidateFile(output, doc); err != nil {
			return agentResult{Data: doc, Warnings: warnings}, agentapi.Wrap(agentapi.CodeInternal, err)
		}
	}
	return agentResult{Data: doc, Warnings: warnings}, nil
}

// writeCandidateFile replaces path atomically with the indented document.
func writeCandidateFile(path string, doc pruneCandidateFile) (err error) {
	content, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding candidate file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("writing candidate file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	if _, err := tmp.Write(append(content, '\n')); err != nil {
		return errors.Join(fmt.Errorf("writing candidate file: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing candidate file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing candidate file: %w", err)
	}
	return nil
}

func printPruneCandidatesHuman(result agentResult) error {
	doc, ok := result.Data.(pruneCandidateFile)
	if !ok {
		return nil
	}
	count := 0
	for _, entry := range doc.Worktrees {
		if entry.Candidate {
			count++
			ui.PrintSuccess(fmt.Sprintf("%s: candidate", entry.Worktree.Name))
			continue
		}
		ui.PrintInfo(fmt.Sprintf("%s: blocked: %s", entry.Worktree.Name, strings.Join(entry.Blockers, "; ")))
	}
	ui.PrintDone(fmt.Sprintf("%d of %d worktree(s) are prune candidates", count, len(doc.Worktrees)))
	return nil
}
