package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/wtstate"
)

// Producer identifies the worktree that recorded a result: its stable ID and
// path, its declared role and the owner of its active lease, if any. Roles
// and lease owners are descriptive metadata, not authentication.
type Producer struct {
	WorktreeID string `json:"worktree_id"`
	Path       string `json:"path"`
	Role       string `json:"role,omitempty"`
	LeaseOwner string `json:"lease_owner,omitempty"`
}

// ReusedFrom names the run whose commands produced a reused outcome.
type ReusedFrom struct {
	WorktreeID string `json:"worktree_id"`
	Path       string `json:"path"`
	Role       string `json:"role,omitempty"`
	LeaseOwner string `json:"lease_owner,omitempty"`
	RunID      string `json:"run_id"`
	Check      string `json:"check"`
}

func (r *ReusedFrom) producer() Producer {
	return Producer{WorktreeID: r.WorktreeID, Path: r.Path, Role: r.Role, LeaseOwner: r.LeaseOwner}
}

// producerOf returns who recorded result; results recorded before producers
// were tracked are attributed to their worktree without a role.
func producerOf(result *Result) Producer {
	if result.Producer != nil {
		return *result.Producer
	}
	return Producer{WorktreeID: result.Worktree.ID, Path: result.Worktree.Path}
}

func reusedFromResult(result *Result) *ReusedFrom {
	producer := producerOf(result)
	return &ReusedFrom{WorktreeID: producer.WorktreeID, Path: producer.Path, Role: producer.Role, LeaseOwner: producer.LeaseOwner, RunID: result.RunID, Check: result.Check}
}

// Reuse trust levels (see config.RoleReuseConfig).
const (
	TrustSameWorktree = config.ReuseTrustSameWorktree
	TrustSameRole     = config.ReuseTrustSameRole
	TrustAny          = config.ReuseTrustAny
)

// ReusePolicy restricts which producers' results a worktree reuses. The zero
// value trusts any worktree of the project.
type ReusePolicy struct {
	Trust string
	// ExcludeRoles refuses results produced by other worktrees with these
	// roles. A worktree's own results are never excluded.
	ExcludeRoles []string
}

// admits decides whether self may reuse a result producer recorded.
func (p ReusePolicy) admits(self, producer Producer) (bool, string) {
	if producer.WorktreeID == self.WorktreeID {
		return true, ""
	}
	for _, role := range p.ExcludeRoles {
		if producer.Role == role {
			return false, fmt.Sprintf("results produced by %s worktrees are excluded", role)
		}
	}
	switch p.Trust {
	case TrustSameWorktree:
		return false, "reuse trust same-worktree accepts only this worktree's results"
	case TrustSameRole:
		if producer.Role != self.Role {
			return false, fmt.Sprintf("producer role %q differs from this worktree's role %q (reuse trust same-role)", producer.Role, self.Role)
		}
	}
	return true, ""
}

// ReuseIndex locates the machine-wide index of reusable results for one
// project, keyed by the canonical Git common directory its worktrees share.
// The index only points at results, and every hit is revalidated against the
// producer's own records, so a stale entry causes a miss rather than a false
// pass. Index and results are trusted as written by the same user's Anvil
// processes; the validation guards against staleness and mistakes, not
// against a user deliberately forging records.
type ReuseIndex struct {
	Root      string
	CommonDir string
}

const reuseIndexSchema = "anvil.check-reuse-index/v1"

type reuseIndexEntry struct {
	Schema        string    `json:"schema"`
	Check         string    `json:"check"`
	ContentDigest string    `json:"content_digest"`
	WorktreeID    string    `json:"worktree_id"`
	WorktreePath  string    `json:"worktree_path"`
	RunID         string    `json:"run_id"`
	Result        string    `json:"result"`
	RecordedAt    time.Time `json:"recorded_at"`
}

func (x ReuseIndex) enabled() bool { return x.Root != "" && x.CommonDir != "" }

// checkDir is <root>/reuse/<sha256(common dir)>/<check>. Below it, one
// directory per content digest holds one entry per producing worktree.
func (x ReuseIndex) checkDir(check string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(x.CommonDir)))
	return filepath.Join(x.Root, "reuse", hex.EncodeToString(sum[:]), check)
}

func (x ReuseIndex) dir(check, digest string) string {
	return filepath.Join(x.checkDir(check), digest)
}

// indexLockWait bounds every wait for a reuse index lock, so a stopped or
// wedged holder never blocks a check, a lookup or a removal: recording
// degrades to a warning, a lookup to a miss and forgetting to a warning.
var indexLockWait = 2 * time.Second

// lockIndex serializes changes to one check's index entries (writes and
// removals; readers need no lock because entries are replaced atomically).
// It is a leaf lock: nothing else is acquired while it is held.
func lockIndex(checkDir string) (*lockfile.Lock, error) {
	if err := os.MkdirAll(checkDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(checkDir, ".lock")
	lock, err := lockfile.AcquireTimeout(path, indexLockWait)
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, fmt.Errorf("reuse index lock %s is busy (held by another Anvil process for more than %s)", path, indexLockWait)
	}
	return lock, err
}

// removeStaleTemps deletes writeJSON temporary files older than staleTempAge
// (left behind by a crashed writer) in an index content directory (called with the index lock held). Newer
// ones may belong to a write in progress elsewhere and are left alone.
func removeStaleTemps(dir string, now time.Time) error {
	temps, err := filepath.Glob(filepath.Join(dir, ".result-*"))
	if err != nil {
		return err
	}
	var problems []error
	for _, temp := range temps {
		info, err := os.Lstat(temp)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !info.Mode().IsRegular() || now.Sub(info.ModTime()) < staleTempAge {
			continue
		}
		if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// record points the index at a clean passing result this worktree executed.
func (x ReuseIndex) record(result *Result, now time.Time) (err error) {
	digest := result.Identity.ContentDigest
	if !x.enabled() || digest == "" || result.Status != StatusPassed || result.ReusedFrom != nil || !validWorktreeID(result.Worktree.ID) {
		return nil
	}
	lock, err := lockIndex(x.checkDir(result.Check))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	cleanErr := removeStaleTemps(x.dir(result.Check, digest), time.Now())
	if errors.Is(cleanErr, os.ErrNotExist) {
		cleanErr = nil
	}
	return errors.Join(cleanErr, writeJSON(filepath.Join(x.dir(result.Check, digest), result.Worktree.ID+".json"), reuseIndexEntry{
		Schema: reuseIndexSchema, Check: result.Check, ContentDigest: digest, WorktreeID: result.Worktree.ID, WorktreePath: result.Worktree.Path,
		RunID: result.RunID, Result: filepath.Join(result.RunDir, "result.json"), RecordedAt: now.UTC(),
	}))
}

// forgetVanished removes an entry whose result file no longer exists (pruned
// run, released runtime directory). Under the index lock it removes the entry
// only if it is unchanged and its result is still missing, so a producer that
// re-recorded meanwhile keeps its new entry.
func (x ReuseIndex) forgetVanished(entry reuseIndexEntry) (err error) {
	lock, err := lockIndex(x.checkDir(entry.Check))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	path := filepath.Join(x.dir(entry.Check, entry.ContentDigest), entry.WorktreeID+".json")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var current reuseIndexEntry
	if err := json.Unmarshal(content, &current); err != nil || current != entry {
		return nil
	}
	if _, err := os.Lstat(entry.Result); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return removeEntry(path)
}

// removeEntry deletes an index entry, stale temporary files next to it and
// its content directory once empty.
func removeEntry(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removeStaleTemps(filepath.Dir(path), time.Now()); err != nil {
		return err
	}
	if err := os.Remove(filepath.Dir(path)); err != nil && !errors.Is(err, os.ErrNotExist) && !isNotEmpty(err) {
		return err
	}
	return nil
}

func isNotEmpty(err error) bool {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return false
	}
	if entries, readErr := os.ReadDir(pathErr.Path); readErr == nil && len(entries) > 0 {
		return true
	}
	return false
}

// ForgetWorktree removes every reuse index entry the worktree produced, in
// every project and check. Runtime cleanup calls it when the worktree's
// runtime directory is released, so the index does not grow with removed
// worktrees.
func ForgetWorktree(root, worktreeID string) error {
	if !validWorktreeID(worktreeID) {
		// A malformed legacy ID can never have produced index entries.
		return nil
	}
	checkDirs, err := filepath.Glob(filepath.Join(root, "reuse", "*", "*"))
	if err != nil {
		return err
	}
	var problems []error
	for _, checkDir := range checkDirs {
		if info, err := os.Stat(checkDir); err != nil || !info.IsDir() {
			continue
		}
		problems = append(problems, forgetInCheck(checkDir, worktreeID))
	}
	return errors.Join(problems...)
}

func forgetInCheck(checkDir, worktreeID string) (err error) {
	lock, err := lockIndex(checkDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	entries, err := filepath.Glob(filepath.Join(checkDir, "*", worktreeID+".json"))
	if err != nil {
		return err
	}
	var problems []error
	for _, entry := range entries {
		problems = append(problems, removeEntry(entry))
	}
	return errors.Join(problems...)
}

// candidates returns index entries for the content digest, newest first.
// Unreadable entries become reasons, not errors.
func (x ReuseIndex) candidates(check, digest string) ([]reuseIndexEntry, []string) {
	dir := x.dir(check, digest)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{"reuse index unreadable: " + err.Error()}
	}
	var entries []reuseIndexEntry
	var reasons []string
	for _, file := range files {
		name := file.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		var entry reuseIndexEntry
		if err == nil {
			err = json.Unmarshal(content, &entry)
		}
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("index entry %s unreadable: %v", name, err))
			continue
		}
		if entry.Schema != reuseIndexSchema || entry.Check != check || entry.ContentDigest != digest || entry.WorktreeID+".json" != name {
			reasons = append(reasons, fmt.Sprintf("index entry %s does not describe this check and content", name))
			continue
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].RecordedAt.Equal(entries[j].RecordedAt) {
			return entries[i].RecordedAt.After(entries[j].RecordedAt)
		}
		return entries[i].WorktreeID < entries[j].WorktreeID
	})
	return entries, reasons
}

func validWorktreeID(id string) bool {
	if !strings.HasPrefix(id, "wt_") || len(id) < 4 {
		return false
	}
	for _, r := range id[3:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// reuseLookup is everything findReusable consults besides the check and its
// current identity.
type reuseLookup struct {
	Store  Store
	Index  ReuseIndex
	Self   Producer
	Policy ReusePolicy
}

// findReusable returns a stored passing result that may stand in for running
// check at identity, and the run that produced it:
//
//  1. this worktree's latest result when its full identity is current and
//     its artifacts are intact (a record reused from another worktree must
//     still be admitted by the policy);
//  2. otherwise, unless the policy is same-worktree, a clean passing result
//     another registered worktree of the project recorded at the same
//     content digest (see validateIndexed).
//
// A miss returns its reason. Only failures to read this worktree's own store
// are errors; nothing about another worktree ever blocks running.
func findReusable(ctx context.Context, lookup reuseLookup, check config.CheckConfig, identity Identity) (*Result, *ReusedFrom, string, error) {
	local, localReason, err := lookup.Store.reusable(check, identity)
	if err != nil {
		return nil, nil, "", err
	}
	if local != nil {
		from := local.ReusedFrom
		if from == nil {
			from = reusedFromResult(local)
		}
		ok, why := lookup.Policy.admits(lookup.Self, from.producer())
		if ok {
			return local, from, "", nil
		}
		localReason = fmt.Sprintf("the latest result was reused from worktree %s: %s", from.WorktreeID, why)
	}
	if !check.Reusable {
		return nil, nil, localReason, nil
	}
	miss := func(reason string) (*Result, *ReusedFrom, string, error) {
		return nil, nil, "this worktree: " + localReason + "; other worktrees: " + reason, nil
	}
	switch {
	case lookup.Policy.Trust == TrustSameWorktree:
		return miss("not consulted (reuse trust same-worktree)")
	case !lookup.Index.enabled():
		return nil, nil, localReason, nil
	case identity.ContentDigest == "":
		return miss("this worktree has uncommitted changes; reuse across worktrees requires a clean checkout")
	}
	entries, reasons := lookup.Index.candidates(check.Name, identity.ContentDigest)
	registered := registeredWorktrees(ctx, lookup.Self.Path)
	for _, entry := range entries {
		if entry.WorktreeID == lookup.Self.WorktreeID {
			continue
		}
		result, reason := validateIndexed(lookup, entry, identity, registered)
		if result != nil {
			return result, reusedFromResult(result), "", nil
		}
		reasons = append(reasons, fmt.Sprintf("worktree %s: %s", entry.WorktreeID, reason))
	}
	if len(reasons) == 0 {
		return miss("no producer recorded at this content identity")
	}
	return miss(strings.Join(reasons, "; "))
}

// validateIndexed revalidates an index entry against the producer's own
// records. The referenced result must live in the producer's runtime
// directory, have passed on a clean checkout whose recorded identity yields
// this content digest, and come from a worktree that is still registered
// with this repository under the same identity; the policy must admit the
// producer and its artifacts must be intact.
func validateIndexed(lookup reuseLookup, entry reuseIndexEntry, identity Identity, registered func() (map[string]bool, error)) (*Result, string) {
	if !validWorktreeID(entry.WorktreeID) {
		return nil, fmt.Sprintf("invalid worktree id %q", entry.WorktreeID)
	}
	producerDir, err := filepath.EvalSymlinks(filepath.Join(lookup.Index.Root, "worktrees", entry.WorktreeID))
	if err != nil {
		return nil, "producer runtime directory unavailable: " + err.Error()
	}
	resultPath, err := filepath.EvalSymlinks(entry.Result)
	if errors.Is(err, os.ErrNotExist) {
		reason := "the producer's result no longer exists (pruned or removed)"
		if forgetErr := lookup.Index.forgetVanished(entry); forgetErr != nil {
			reason += "; removing its index entry: " + forgetErr.Error()
		}
		return nil, reason
	}
	if err != nil {
		return nil, "producer result unavailable: " + err.Error()
	}
	if !within(producerDir, resultPath) || filepath.Base(resultPath) != "result.json" {
		return nil, fmt.Sprintf("result path %s is outside the producer's runtime directory", entry.Result)
	}
	content, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, "producer result unavailable: " + err.Error()
	}
	var result Result
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, "producer result malformed: " + err.Error()
	}
	switch {
	case result.Status != StatusPassed:
		return nil, "the producer's run is " + result.Status
	case result.Check != entry.Check || result.RunID != entry.RunID || result.Worktree.ID != entry.WorktreeID:
		return nil, "the producer's result does not match its index entry"
	case result.ReusedFrom != nil:
		return nil, "the producer's result was itself reused"
	case result.Identity.Git.Dirty:
		return nil, "the producer's checkout was dirty"
	}
	if digest := ContentDigest(result.Identity); digest == "" || digest != result.Identity.ContentDigest || digest != identity.ContentDigest {
		return nil, "the producer's recorded identity does not match this content identity"
	}
	paths, err := registered()
	if err != nil {
		return nil, "cannot list registered worktrees: " + err.Error()
	}
	if !paths[canonicalOrClean(result.Worktree.Path)] {
		return nil, fmt.Sprintf("producer worktree %s is no longer a registered worktree of this repository", result.Worktree.Path)
	}
	recorded, status, err := wtstate.ReadIdentity(result.Worktree.Path)
	if err != nil {
		return nil, "producer worktree identity unreadable: " + err.Error()
	}
	if recorded == nil || recorded.ID != entry.WorktreeID || status != wtstate.IdentityOK {
		return nil, "producer worktree identity changed"
	}
	if ok, why := lookup.Policy.admits(lookup.Self, producerOf(&result)); !ok {
		return nil, why
	}
	if intact, reason := ArtifactsIntact(&result); !intact {
		return nil, reason
	}
	return &result, ""
}

// registeredWorktrees lists, once and only when needed, the canonical paths
// of the worktrees registered with the repository containing path.
func registeredWorktrees(ctx context.Context, path string) func() (map[string]bool, error) {
	var paths map[string]bool
	var listErr error
	listed := false
	return func() (map[string]bool, error) {
		if listed {
			return paths, listErr
		}
		listed = true
		out, err := exec.CommandContext(ctx, "git", "-C", path, "worktree", "list", "--porcelain", "-z").Output()
		if err != nil {
			listErr = fmt.Errorf("listing worktrees: %w", err)
			return nil, listErr
		}
		paths = map[string]bool{}
		for _, field := range strings.Split(string(out), "\x00") {
			if worktree, ok := strings.CutPrefix(field, "worktree "); ok {
				paths[canonicalOrClean(worktree)] = true
			}
		}
		return paths, nil
	}
}

func canonicalOrClean(path string) string {
	if canonical, err := wtstate.CanonicalPath(filepath.FromSlash(path)); err == nil {
		return canonical
	}
	return filepath.Clean(filepath.FromSlash(path))
}

// recordReuse writes a consumer-side record of a result reused from another
// worktree: this worktree's own identity, the producer's outcome and
// artifacts, no commands, and provenance naming the producing run. It makes
// `check --list` and `status` report the check as current here.
//
// Like an executed run, started_at excludes a slot queue wait (a hit found
// only after queueing), which is reported as queued_ms.
func recordReuse(req RunRequest, store Store, identity Identity, produced *Result, from *ReusedFrom, runID string, started time.Time, queued time.Duration) (*Result, error) {
	runDir := store.runDir(req.Check.Name, runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	finished := req.now().UTC()
	record := &Result{
		Schema: ResultSchema, RunID: runID, Check: req.Check.Name, Status: StatusPassed,
		StartedAt: started.Add(queued), QueuedMS: queued.Milliseconds(), FinishedAt: &finished, Worktree: req.Worktree, Identity: identity,
		Commands: []CommandResult{}, Artifacts: append([]Artifact{}, produced.Artifacts...), Locks: append([]string{}, req.Check.Locks...),
		AnvilVersion: req.AnvilVersion, RunDir: runDir, Producer: req.producer(), ReusedFrom: from,
	}
	if err := store.save(record); err != nil {
		return nil, err
	}
	record.Reused = true
	return record, store.prune(req.Check.Name)
}
