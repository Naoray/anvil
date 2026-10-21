package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/wtstate"
)

// Merge states against origin/<default>.
const (
	mergeMerged    = "merged"
	mergeNotMerged = "not_merged"
	mergeUnknown   = "unknown"
)

// untrackedSampleSize bounds the untracked paths listed as evidence.
const untrackedSampleSize = 10

// ghTimeout bounds one `gh pr list` call.
const ghTimeout = 30 * time.Second

type mergeEvidence struct {
	State  string `json:"state"`
	Target string `json:"target"`
	Error  string `json:"error,omitempty"`
}

// prEvidence is optional GitHub pull request state. "unavailable" is not a
// blocker by itself; "open" is.
type prEvidence struct {
	State    string `json:"state"` // open|merged|merged_other_base|closed|none|unavailable
	Number   int    `json:"number,omitempty"`
	Base     string `json:"base,omitempty"`
	MergedAt string `json:"merged_at,omitempty"`
	HeadOID  string `json:"head_oid,omitempty"`
	Source   string `json:"source,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type remoteEvidence struct {
	State           string `json:"state"` // pushed|unpushed|unknown
	UnpushedCommits int    `json:"unpushed_commits"`
	Error           string `json:"error,omitempty"`
}

type countEvidence struct {
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

type untrackedEvidence struct {
	Count    int      `json:"count"`
	Sample   []string `json:"sample"`
	Excluded []string `json:"excluded"`
	Error    string   `json:"error,omitempty"`
}

// ignoredEvidence summarizes gitignored paths removal would delete. Count
// and Sample cover paths not allowed by allow_ignored; they block.
type ignoredEvidence struct {
	Count    int        `json:"count"`
	Allowed  int        `json:"allowed"`
	Sample   []string   `json:"sample"`
	NewestAt *time.Time `json:"newest_at,omitempty"`
	Excluded []string   `json:"excluded"`
	Error    string     `json:"error,omitempty"`
}

type leaseEvidence struct {
	State     string     `json:"state"` // active|stale|none|unknown
	Owner     string     `json:"owner,omitempty"`
	TaskID    string     `json:"task_id,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type protectedEvidence struct {
	Protected bool   `json:"protected"`
	Pattern   string `json:"pattern,omitempty"`
	Matched   string `json:"matched,omitempty"` // branch|name
}

// activitySources are the newest observed times per source.
type activitySources struct {
	HeadReflog   *time.Time `json:"head_reflog,omitempty"`
	Files        *time.Time `json:"files,omitempty"`
	LeaseRenewed *time.Time `json:"lease_renewed,omitempty"`
	CheckResult  *time.Time `json:"check_result,omitempty"`
	IgnoredFiles *time.Time `json:"ignored_files,omitempty"`
	// Preparation is the latest preparation start or finish; LocalState
	// the .anvil.local modification time (identity, lease, preparation).
	Preparation *time.Time `json:"preparation,omitempty"`
	LocalState  *time.Time `json:"local_state,omitempty"`
}

type activityEvidence struct {
	State            string          `json:"state"` // recent|idle|unknown
	LatestAt         *time.Time      `json:"latest_at,omitempty"`
	LatestSource     string          `json:"latest_source,omitempty"`
	ThresholdSeconds int64           `json:"threshold_seconds"`
	Sources          activitySources `json:"sources"`
	RunningChecks    []string        `json:"running_checks"`
	// Warnings note missing sources that do not block on their own.
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type openFilesEvidence struct {
	Method  string         `json:"method"` // procfs|lsof|unsupported
	Holders []procs.Holder `json:"holders"`
	// SkippedOtherUsers is null where other users' processes cannot even be
	// counted (lsof).
	SkippedOtherUsers *int            `json:"skipped_other_users"`
	UninspectableOwn  []procs.Process `json:"uninspectable_own"`
	// ForeignNamespaceOwn are own processes in another mount namespace;
	// their open files cannot be mapped to this worktree.
	ForeignNamespaceOwn []procs.Process `json:"foreign_namespace_own"`
	Error               string          `json:"error,omitempty"`
}

// pruneCollectors gather each kind of safety evidence. Every collector
// reports its own failure in the evidence; a failure is a blocker.
type pruneCollectors struct {
	merge     func(branch string) mergeEvidence
	pr        func(branch string) prEvidence
	remote    func(branch string) remoteEvidence
	tracked   func(path string) countEvidence
	untracked func(path string) untrackedEvidence
	ignored   func(path string) ignoredEvidence
	lease     func(path string) leaseEvidence
	activity  func(path, worktreeID string) activityEvidence
	openFiles func(path string) openFilesEvidence
}

// collectorOptions configure the default collectors.
type collectorOptions struct {
	GitDir        string
	RepoPath      string
	MergeTarget   string
	DefaultBranch string
	AllowIgnored  []string
	Recent        time.Duration
	PRLookup      bool
	// Snapshot returns the open-file snapshot; candidates share one,
	// apply takes a fresh one for every recheck.
	Snapshot func() (procs.Snapshot, error)
	Now      func() time.Time
}

func defaultPruneCollectors(opts collectorOptions) pruneCollectors {
	return pruneCollectors{
		merge: func(branch string) mergeEvidence { return collectMerge(opts.GitDir, opts.MergeTarget, branch) },
		pr: func(branch string) prEvidence {
			return collectPullRequest(opts.PRLookup, opts.RepoPath, branch, opts.DefaultBranch)
		},
		remote:    func(branch string) remoteEvidence { return collectRemote(opts.GitDir, branch) },
		tracked:   collectTracked,
		untracked: collectUntracked,
		ignored:   func(path string) ignoredEvidence { return collectIgnored(path, opts.AllowIgnored) },
		lease:     func(path string) leaseEvidence { return collectLease(path, opts.Now()) },
		activity: func(path, id string) activityEvidence {
			return collectActivity(path, id, opts.Now(), opts.Recent)
		},
		openFiles: func(path string) openFilesEvidence { return collectOpenFiles(opts.Snapshot, path) },
	}
}

// sharedSnapshot scans open files once, on first use.
func sharedSnapshot() func() (procs.Snapshot, error) {
	var once sync.Once
	var snapshot procs.Snapshot
	var err error
	return func() (procs.Snapshot, error) {
		once.Do(func() { snapshot, err = procs.ScanOpenFiles() })
		return snapshot, err
	}
}

// evidenceGit runs git for read-only evidence. --no-optional-locks keeps
// commands such as 'git status' from refreshing (rewriting) the index.
func evidenceGit(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

func gitOutput(dir string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := evidenceGit(dir, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// gitTest runs a git predicate: exit 0 is true, exit 1 false, anything
// else an error.
func gitTest(dir string, args ...string) (bool, error) {
	var stderr bytes.Buffer
	cmd := evidenceGit(dir, args...)
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
}

func collectMerge(gitDir, target, branch string) mergeEvidence {
	evidence := mergeEvidence{State: mergeUnknown, Target: target}
	exists, err := gitTest(gitDir, "rev-parse", "--verify", "--quiet", "refs/remotes/"+target+"^{commit}")
	if err != nil {
		evidence.Error = err.Error()
		return evidence
	}
	if !exists {
		evidence.Error = fmt.Sprintf("%s does not exist; run 'git fetch' or pass --fetch", target)
		return evidence
	}
	merged, err := gitTest(gitDir, "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/"+target)
	if err != nil {
		evidence.Error = err.Error()
		return evidence
	}
	evidence.State = mergeNotMerged
	if merged {
		evidence.State = mergeMerged
	}
	return evidence
}

func collectRemote(gitDir, branch string) remoteEvidence {
	out, err := gitOutput(gitDir, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return remoteEvidence{State: mergeUnknown, Error: err.Error()}
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return remoteEvidence{State: mergeUnknown, Error: fmt.Sprintf("parsing rev-list count %q: %v", out, err)}
	}
	if count > 0 {
		return remoteEvidence{State: "unpushed", UnpushedCommits: count}
	}
	return remoteEvidence{State: "pushed"}
}

func collectTracked(path string) countEvidence {
	out, err := gitOutput(path, "status", "--porcelain=v1", "-z", "--untracked-files=no", "--ignore-submodules=none")
	if err != nil {
		return countEvidence{Error: err.Error()}
	}
	return countEvidence{Count: countPorcelainEntries(out)}
}

// countPorcelainEntries counts `git status --porcelain=v1 -z` entries; a
// rename or copy carries its source path as an extra field.
func countPorcelainEntries(out []byte) int {
	fields := strings.Split(string(out), "\x00")
	count := 0
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 3 {
			continue
		}
		count++
		if entry[0] == 'R' || entry[0] == 'C' {
			i++
		}
	}
	return count
}

// excludedDir reports whether a path lies in a dependency or Git directory
// that never counts as the user's own work.
func excludedDir(rel string, names ...string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		for _, name := range names {
			if part == name {
				return true
			}
		}
	}
	return false
}

func collectUntracked(path string) untrackedEvidence {
	evidence := untrackedEvidence{Sample: []string{}, Excluded: []string{"node_modules"}}
	out, err := gitOutput(path, "ls-files", "-z", "--others", "--exclude-standard", "--exclude=node_modules/")
	if err != nil {
		evidence.Error = err.Error()
		return evidence
	}
	var files []string
	for _, file := range strings.Split(string(out), "\x00") {
		if file != "" && !excludedDir(file, "node_modules") {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	evidence.Count = len(files)
	if len(files) > untrackedSampleSize {
		files = files[:untrackedSampleSize]
	}
	evidence.Sample = append(evidence.Sample, files...)
	return evidence
}

// ignoredExcluded are never reported as ignored: dependencies and Anvil's
// own state.
var ignoredExcluded = []string{"node_modules", "vendor", config.LocalStateFile}

// scanIgnored lists gitignored paths (ignored directories collapsed) outside
// node_modules and vendor, except .anvil.local, and the newest mtime of any
// file beneath them.
func scanIgnored(path string) ([]string, *time.Time, error) {
	out, err := gitOutput(path, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, nil, err
	}
	var entries []string
	var newestAt *time.Time
	for _, entry := range strings.Split(string(out), "\x00") {
		trimmed := strings.TrimSuffix(entry, "/")
		if trimmed == "" || trimmed == config.LocalStateFile || excludedDir(trimmed, "node_modules", "vendor") {
			continue
		}
		entries = append(entries, entry)
		err := filepath.WalkDir(filepath.Join(path, filepath.FromSlash(trimmed)), func(_ string, d os.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			newestAt = newest(newestAt, info.ModTime())
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("reading ignored %s: %w", entry, err)
		}
	}
	sort.Strings(entries)
	return entries, newestAt, nil
}

// ignoredAllowed matches rel, or any parent directory of it, against the
// allow_ignored glob patterns.
func ignoredAllowed(rel string, patterns []string) bool {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
	for candidate := rel; candidate != "." && candidate != "/" && candidate != ""; candidate = pathpkg.Dir(candidate) {
		for _, pattern := range patterns {
			if matched, err := pathpkg.Match(pattern, candidate); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func collectIgnored(path string, allow []string) ignoredEvidence {
	evidence := ignoredEvidence{Sample: []string{}, Excluded: append([]string{}, ignoredExcluded...)}
	entries, newestAt, err := scanIgnored(path)
	if err != nil {
		evidence.Error = err.Error()
		return evidence
	}
	evidence.NewestAt = newestAt
	for _, entry := range entries {
		if ignoredAllowed(entry, allow) {
			evidence.Allowed++
			continue
		}
		evidence.Count++
		if len(evidence.Sample) < untrackedSampleSize {
			evidence.Sample = append(evidence.Sample, entry)
		}
	}
	return evidence
}

func collectLease(path string, now time.Time) leaseEvidence {
	lease, err := wtstate.ReadLease(path)
	if err != nil {
		return leaseEvidence{State: mergeUnknown, Error: err.Error()}
	}
	evidence := leaseEvidence{State: lease.State(now)}
	if evidence.State != wtstate.LeaseNone {
		expires := lease.ExpiresAt().UTC()
		evidence.Owner, evidence.TaskID, evidence.ExpiresAt = lease.Owner, lease.TaskID, &expires
	}
	return evidence
}

func newest(current *time.Time, candidate time.Time) *time.Time {
	if candidate.IsZero() || (current != nil && !candidate.After(*current)) {
		return current
	}
	value := candidate.UTC()
	return &value
}

// collectActivity finds the newest of: the HEAD reflog, tracked and
// untracked (non-ignored) files outside node_modules, vendor and .git, the
// lease renewal (or release), and the latest check result.
func collectActivity(path, worktreeID string, now time.Time, threshold time.Duration) activityEvidence {
	sources, running, err := activitySourcesOf(path, worktreeID)
	if err != nil {
		return activityEvidence{State: mergeUnknown, ThresholdSeconds: int64(threshold / time.Second), RunningChecks: []string{}, Error: err.Error()}
	}
	evidence := classifyActivity(sources, now, threshold)
	evidence.RunningChecks = running
	if sources.HeadReflog == nil {
		evidence.Warnings = append(evidence.Warnings, "reflog unavailable: the worktree has no HEAD reflog; activity relies on the other sources")
	}
	return evidence
}

func activitySourcesOf(path, worktreeID string) (activitySources, []string, error) {
	var sources activitySources
	running := []string{}
	gitDir, err := config.WorktreeGitDir(path)
	if err != nil {
		return sources, nil, fmt.Errorf("resolving Git directory: %w", err)
	}
	if info, err := os.Lstat(filepath.Join(gitDir, "logs", "HEAD")); err == nil {
		sources.HeadReflog = newest(nil, info.ModTime())
	} else if !errors.Is(err, os.ErrNotExist) {
		return sources, nil, fmt.Errorf("reading HEAD reflog: %w", err)
	}

	out, err := gitOutput(path, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--exclude=node_modules/", "--exclude=vendor/")
	if err != nil {
		return sources, nil, err
	}
	for _, file := range strings.Split(string(out), "\x00") {
		if file == "" || excludedDir(file, "node_modules", "vendor", ".git") {
			continue
		}
		info, err := os.Lstat(filepath.Join(path, filepath.FromSlash(file)))
		if errors.Is(err, os.ErrNotExist) {
			continue // a deleted tracked file is reported as a tracked change
		}
		if err != nil {
			return sources, nil, fmt.Errorf("reading %s: %w", file, err)
		}
		sources.Files = newest(sources.Files, info.ModTime())
	}
	if _, sources.IgnoredFiles, err = scanIgnored(path); err != nil {
		return sources, nil, err
	}

	lease, err := wtstate.ReadLease(path)
	if err != nil {
		return sources, nil, fmt.Errorf("reading lease: %w", err)
	}
	if lease != nil {
		sources.LeaseRenewed = newest(nil, lease.RenewedAt)
		if lease.Previous != nil {
			sources.LeaseRenewed = newest(sources.LeaseRenewed, lease.Previous.EndedAt)
		}
	}

	prepare, err := wtstate.ReadPrepare(path)
	if err != nil {
		return sources, nil, fmt.Errorf("reading preparation record: %w", err)
	}
	if prepare != nil {
		sources.Preparation = newest(nil, prepare.StartedAt)
		if prepare.FinishedAt != nil {
			sources.Preparation = newest(sources.Preparation, *prepare.FinishedAt)
		}
	}
	if info, err := os.Lstat(filepath.Join(path, config.LocalStateFile)); err == nil {
		sources.LocalState = newest(nil, info.ModTime())
	} else if !errors.Is(err, os.ErrNotExist) {
		return sources, nil, fmt.Errorf("reading %s: %w", config.LocalStateFile, err)
	}

	if worktreeID == "" {
		return sources, running, nil
	}
	root, err := resources.Root()
	if err != nil {
		return sources, nil, err
	}
	dir, err := resources.WorktreeDir(root, worktreeID)
	if err != nil {
		return sources, nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "checks"))
	if errors.Is(err, os.ErrNotExist) {
		return sources, running, nil
	}
	if err != nil {
		return sources, nil, fmt.Errorf("reading check results: %w", err)
	}
	store := checks.Store{RuntimeDir: dir}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		latest, err := store.Latest(entry.Name())
		if err != nil {
			return sources, nil, fmt.Errorf("reading check %s: %w", entry.Name(), err)
		}
		if latest == nil {
			continue
		}
		sources.CheckResult = newest(sources.CheckResult, latest.StartedAt)
		if latest.FinishedAt != nil {
			sources.CheckResult = newest(sources.CheckResult, *latest.FinishedAt)
		}
	}
	if running, err = checks.RunningChecks(dir); err != nil {
		return sources, nil, fmt.Errorf("inspecting running checks: %w", err)
	}
	return sources, running, nil
}

// classifyActivity marks activity newer than threshold as recent. A zero
// threshold disables the check.
func classifyActivity(sources activitySources, now time.Time, threshold time.Duration) activityEvidence {
	evidence := activityEvidence{State: "idle", ThresholdSeconds: int64(threshold / time.Second), Sources: sources, RunningChecks: []string{}}
	for _, source := range []struct {
		name string
		at   *time.Time
	}{{"head_reflog", sources.HeadReflog}, {"files", sources.Files}, {"lease_renewed", sources.LeaseRenewed}, {"check_result", sources.CheckResult}, {"ignored_files", sources.IgnoredFiles}, {"preparation", sources.Preparation}, {"local_state", sources.LocalState}} {
		if source.at != nil && (evidence.LatestAt == nil || source.at.After(*evidence.LatestAt)) {
			evidence.LatestAt, evidence.LatestSource = source.at, source.name
		}
	}
	if threshold > 0 && evidence.LatestAt != nil && evidence.LatestAt.After(now.Add(-threshold)) {
		evidence.State = "recent"
	}
	return evidence
}

func collectOpenFiles(snapshot func() (procs.Snapshot, error), path string) openFilesEvidence {
	evidence := openFilesEvidence{Method: "unsupported", Holders: []procs.Holder{}, UninspectableOwn: []procs.Process{}, ForeignNamespaceOwn: []procs.Process{}}
	scan, err := snapshot()
	if err != nil {
		if scan.Method != "" {
			evidence.Method = scan.Method
		}
		evidence.Error = err.Error()
		return evidence
	}
	canonical, err := wtstate.CanonicalPath(path)
	if err != nil {
		evidence.Method, evidence.Error = scan.Method, err.Error()
		return evidence
	}
	evidence.Method = scan.Method
	evidence.Holders = scan.Holders(canonical)
	if scan.OtherUsersCounted {
		skipped := scan.SkippedOtherUsers
		evidence.SkippedOtherUsers = &skipped
	}
	evidence.UninspectableOwn = append(evidence.UninspectableOwn, scan.UninspectableOwn...)
	evidence.ForeignNamespaceOwn = append(evidence.ForeignNamespaceOwn, scan.ForeignNamespaceOwn...)
	return evidence
}

// ghPullRequest is one `gh pr list --json` entry.
type ghPullRequest struct {
	State       string  `json:"state"`
	Number      int     `json:"number"`
	MergedAt    *string `json:"mergedAt"`
	HeadRefOid  string  `json:"headRefOid"`
	BaseRefName string  `json:"baseRefName"`
}

func collectPullRequest(enabled bool, repoPath, branch, defaultBranch string) prEvidence {
	if !enabled {
		return prEvidence{State: "unavailable", Reason: "pull request lookup disabled (--no-pr)"}
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return prEvidence{State: "unavailable", Reason: "gh is not installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, gh, "pr", "list", "--head", branch, "--state", "all", "--json", "state,number,mergedAt,headRefOid,baseRefName", "--limit", "50")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		reason := strings.TrimSpace(stderr.String())
		if line, _, found := strings.Cut(reason, "\n"); found {
			reason = line
		}
		if reason == "" {
			reason = err.Error()
		}
		return prEvidence{State: "unavailable", Source: "gh", Reason: reason}
	}
	var pulls []ghPullRequest
	if err := json.Unmarshal(out, &pulls); err != nil {
		return prEvidence{State: "unavailable", Source: "gh", Reason: "malformed gh output: " + err.Error()}
	}
	return selectPullRequest(pulls, defaultBranch)
}

// selectPullRequest reports an open PR (into any base) first, then one
// merged into the default branch, then one merged elsewhere
// (merged_other_base, never treated as merged), then a closed one.
func selectPullRequest(pulls []ghPullRequest, defaultBranch string) prEvidence {
	kinds := []struct {
		state string
		match func(ghPullRequest) bool
	}{
		{"open", func(p ghPullRequest) bool { return strings.EqualFold(p.State, "OPEN") }},
		{"merged", func(p ghPullRequest) bool {
			return strings.EqualFold(p.State, "MERGED") && p.BaseRefName == defaultBranch
		}},
		{"merged_other_base", func(p ghPullRequest) bool { return strings.EqualFold(p.State, "MERGED") }},
		{"closed", func(p ghPullRequest) bool { return strings.EqualFold(p.State, "CLOSED") }},
	}
	for _, kind := range kinds {
		for _, pull := range pulls {
			if !kind.match(pull) {
				continue
			}
			evidence := prEvidence{State: kind.state, Number: pull.Number, Base: pull.BaseRefName, HeadOID: pull.HeadRefOid, Source: "gh"}
			if pull.MergedAt != nil {
				evidence.MergedAt = *pull.MergedAt
			}
			return evidence
		}
	}
	return prEvidence{State: "none", Source: "gh"}
}
