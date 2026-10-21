package cli

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/lifecycle"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/procs"
	"github.com/naoray/anvil/internal/scaffold"
)

// safeCollectors report a merged, pushed, clean, idle, unleased worktree.
func safeCollectors() pruneCollectors {
	return pruneCollectors{
		merge:  func(string) mergeEvidence { return mergeEvidence{State: mergeMerged, Target: "origin/main"} },
		pr:     func(string) prEvidence { return prEvidence{State: "none", Source: "gh"} },
		remote: func(string) remoteEvidence { return remoteEvidence{State: "pushed"} },
		tracked: func(string) countEvidence {
			return countEvidence{}
		},
		untracked: func(string) untrackedEvidence { return untrackedEvidence{Sample: []string{}} },
		ignored:   func(string) ignoredEvidence { return ignoredEvidence{Sample: []string{}} },
		lease:     func(string) leaseEvidence { return leaseEvidence{State: "none"} },
		activity: func(string, string) activityEvidence {
			return activityEvidence{State: "idle"}
		},
		openFiles: func(string) openFilesEvidence {
			return openFilesEvidence{Method: procs.MethodProcfs, Holders: []procs.Holder{}, SkippedOtherUsers: intPointer(0)}
		},
	}
}

func intPointer(value int) *int { return &value }

func evaluateSafe(t *testing.T, collectors pruneCollectors, settings pruneSettings) pruneCandidate {
	t.Helper()
	path := t.TempDir()
	wt := git.Worktree{Path: path, Branch: "feature/x"}
	summary := worktreeSummary{ID: "wt_0123456789abcdef", IdentityStatus: "ok", Name: "x", Path: path, Branch: "feature/x", Head: "abc"}
	scan := pruneScan{DefaultBranch: "main", MergeTarget: "origin/main", Settings: settings, collectors: collectors}
	return scan.evaluate(wt, false, summary)
}

func hasBlocker(candidate pruneCandidate, substring string) bool {
	for _, blocker := range candidate.Blockers {
		if strings.Contains(blocker, substring) {
			return true
		}
	}
	return false
}

func TestPruneEvaluateSafeWorktreeIsCandidate(t *testing.T) {
	candidate := evaluateSafe(t, safeCollectors(), pruneSettings{RecentSeconds: 86400})
	require.True(t, candidate.Candidate, candidate.Blockers)
	require.Empty(t, candidate.Blockers)
	require.NotEmpty(t, candidate.Reasons)
}

func TestPruneEvaluateEvidenceFailureIsABlocker(t *testing.T) {
	cases := map[string]func(*pruneCollectors){
		"merge state": func(c *pruneCollectors) {
			c.merge = func(string) mergeEvidence { return mergeEvidence{State: mergeUnknown, Error: "no origin/main"} }
		},
		"remote commits": func(c *pruneCollectors) {
			c.remote = func(string) remoteEvidence { return remoteEvidence{State: "unknown", Error: "rev-list failed"} }
		},
		"tracked changes": func(c *pruneCollectors) {
			c.tracked = func(string) countEvidence { return countEvidence{Error: "status failed"} }
		},
		"untracked files": func(c *pruneCollectors) {
			c.untracked = func(string) untrackedEvidence { return untrackedEvidence{Error: "ls-files failed"} }
		},
		"ignored files": func(c *pruneCollectors) {
			c.ignored = func(string) ignoredEvidence { return ignoredEvidence{Error: "ls-files failed"} }
		},
		"lease": func(c *pruneCollectors) {
			c.lease = func(string) leaseEvidence { return leaseEvidence{State: "unknown", Error: "malformed lease"} }
		},
		"recent activity": func(c *pruneCollectors) {
			c.activity = func(string, string) activityEvidence {
				return activityEvidence{State: "unknown", Error: "stat failed"}
			}
		},
		"open files": func(c *pruneCollectors) {
			c.openFiles = func(string) openFilesEvidence {
				return openFilesEvidence{Method: "unsupported", Holders: []procs.Holder{}, Error: procs.ErrHoldersUnsupported.Error()}
			}
		},
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			collectors := safeCollectors()
			inject(&collectors)
			candidate := evaluateSafe(t, collectors, pruneSettings{RecentSeconds: 86400})
			require.False(t, candidate.Candidate)
			require.True(t, hasBlocker(candidate, "evidence unavailable: "+name), candidate.Blockers)
		})
	}
}

func TestPruneEvaluateProcessVisibility(t *testing.T) {
	collectors := safeCollectors()
	collectors.openFiles = func(string) openFilesEvidence {
		return openFilesEvidence{Method: procs.MethodProcfs, Holders: []procs.Holder{}, UninspectableOwn: []procs.Process{{PID: 42, Command: "ssh-agent"}}, SkippedOtherUsers: intPointer(0)}
	}
	candidate := evaluateSafe(t, collectors, pruneSettings{})
	require.False(t, candidate.Candidate)
	require.True(t, hasBlocker(candidate, "evidence unavailable: open files"), candidate.Blockers)
	candidate = evaluateSafe(t, collectors, pruneSettings{AllowUninspectable: []string{"ssh-agent"}})
	require.True(t, candidate.Candidate, "an explicitly vouched-for process only warns: %v", candidate.Blockers)
	require.NotEmpty(t, candidate.Warnings)

	collectors.openFiles = func(string) openFilesEvidence {
		return openFilesEvidence{Method: procs.MethodProcfs, Holders: []procs.Holder{}, SkippedOtherUsers: intPointer(3)}
	}
	candidate = evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, candidate.Candidate, candidate.Blockers)
	require.NotEmpty(t, candidate.Warnings)

	candidate = evaluateSafe(t, collectors, pruneSettings{StrictProcesses: true})
	require.False(t, candidate.Candidate)
	require.True(t, hasBlocker(candidate, "other users"), candidate.Blockers)
}

func TestPruneEvaluatePullRequestEvidence(t *testing.T) {
	collectors := safeCollectors()
	collectors.pr = func(string) prEvidence { return prEvidence{State: "unavailable", Reason: "gh not found"} }
	require.True(t, evaluateSafe(t, collectors, pruneSettings{}).Candidate, "an unavailable PR lookup alone never blocks")

	collectors.pr = func(string) prEvidence { return prEvidence{State: "open", Number: 7, Source: "gh"} }
	candidate := evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, hasBlocker(candidate, "pull request #7 is open"), candidate.Blockers)

	// A squash-merged PR whose head is exactly the local tip counts as merged.
	collectors = safeCollectors()
	collectors.merge = func(string) mergeEvidence { return mergeEvidence{State: mergeNotMerged, Target: "origin/main"} }
	collectors.pr = func(string) prEvidence { return prEvidence{State: "merged", Number: 9, HeadOID: "abc", Source: "gh"} }
	candidate = evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, candidate.Candidate, candidate.Blockers)

	collectors.pr = func(string) prEvidence { return prEvidence{State: "merged", Number: 9, HeadOID: "def", Source: "gh"} }
	candidate = evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, hasBlocker(candidate, "not merged"), "commits after the merged PR head keep the branch unmerged")
}

func TestPruneEvaluateNonBranchWorktrees(t *testing.T) {
	scan := pruneScan{DefaultBranch: "main", MergeTarget: "origin/main", collectors: safeCollectors()}
	path := t.TempDir()
	cases := []struct {
		wt     git.Worktree
		main   bool
		kind   string
		reason string
	}{
		{git.Worktree{Path: path, Branch: "main"}, true, "main", "main checkout"},
		{git.Worktree{Path: path, Bare: true}, false, "bare", "bare"},
		{git.Worktree{Path: path, Detached: true}, false, "detached", "detached HEAD"},
		{git.Worktree{Path: path, Branch: "x", Locked: true}, false, "locked", "locked worktree"},
		{git.Worktree{Path: path + "/gone", Branch: "x"}, false, "missing", "directory is missing"},
		{git.Worktree{Path: path, Branch: "main"}, false, "default_branch", "default branch"},
	}
	for _, tc := range cases {
		candidate := scan.evaluate(tc.wt, tc.main, worktreeSummary{IdentityStatus: "ok", ID: "wt_0123456789abcdef"})
		require.False(t, candidate.Candidate, tc.kind)
		require.Equal(t, tc.kind, candidate.Kind)
		require.True(t, hasBlocker(candidate, tc.reason), "%s: %v", tc.kind, candidate.Blockers)
	}
}

func TestPruneActivityThreshold(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	evidence := classifyActivity(activitySources{Files: &recent}, now, 24*time.Hour)
	require.Equal(t, "recent", evidence.State)
	require.Equal(t, "files", evidence.LatestSource)
	evidence = classifyActivity(activitySources{Files: &recent}, now, 30*time.Minute)
	require.Equal(t, "idle", evidence.State)
	evidence = classifyActivity(activitySources{}, now, 24*time.Hour)
	require.Equal(t, "idle", evidence.State)
}

func TestCountPorcelainEntries(t *testing.T) {
	require.Equal(t, 0, countPorcelainEntries([]byte("")))
	require.Equal(t, 2, countPorcelainEntries([]byte(" M a\x00R  new\x00old\x00")))
}

func TestSelectPullRequest(t *testing.T) {
	pr := selectPullRequest([]ghPullRequest{{State: "CLOSED", Number: 1}, {State: "MERGED", Number: 2, HeadRefOid: "abc", BaseRefName: "main"}}, "main")
	require.Equal(t, "merged", pr.State)
	require.Equal(t, 2, pr.Number)
	require.Equal(t, "main", pr.Base)
	pr = selectPullRequest([]ghPullRequest{{State: "MERGED", Number: 2, BaseRefName: "main"}, {State: "OPEN", Number: 3, BaseRefName: "develop"}}, "main")
	require.Equal(t, "open", pr.State, "an open PR into any base blocks")
	require.Equal(t, "none", selectPullRequest(nil, "main").State)
	// A PR merged into another branch never counts as merged.
	pr = selectPullRequest([]ghPullRequest{{State: "MERGED", Number: 4, HeadRefOid: "abc", BaseRefName: "develop"}}, "main")
	require.Equal(t, "merged_other_base", pr.State)
	require.Equal(t, "develop", pr.Base)
}

func TestPruneEvaluateIgnoredFilesBlock(t *testing.T) {
	collectors := safeCollectors()
	collectors.ignored = func(string) ignoredEvidence {
		return ignoredEvidence{Count: 2, Sample: []string{".env", "storage/"}}
	}
	candidate := evaluateSafe(t, collectors, pruneSettings{})
	require.False(t, candidate.Candidate)
	require.True(t, hasBlocker(candidate, "2 ignored path(s) would be deleted"), candidate.Blockers)
}

func TestPruneEvaluateLsofCannotSeeOtherUsers(t *testing.T) {
	collectors := safeCollectors()
	collectors.openFiles = func(string) openFilesEvidence {
		return openFilesEvidence{Method: procs.MethodLsof, Holders: []procs.Holder{}}
	}
	candidate := evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, candidate.Candidate, candidate.Blockers)
	require.Contains(t, strings.Join(candidate.Warnings, "\n"), "invisible to lsof")
	candidate = evaluateSafe(t, collectors, pruneSettings{StrictProcesses: true})
	require.True(t, hasBlocker(candidate, "other users' processes cannot be inspected on this platform"), candidate.Blockers)
}

func TestPruneEvaluateActivityWarningsSurface(t *testing.T) {
	collectors := safeCollectors()
	collectors.activity = func(string, string) activityEvidence {
		return activityEvidence{State: "idle", Warnings: []string{"reflog unavailable"}}
	}
	candidate := evaluateSafe(t, collectors, pruneSettings{})
	require.True(t, candidate.Candidate, candidate.Blockers)
	require.Contains(t, candidate.Warnings, "reflog unavailable")
}

func TestPruneEvaluatePullRequestMergedIntoOtherBaseIsNotMerged(t *testing.T) {
	collectors := safeCollectors()
	collectors.merge = func(string) mergeEvidence { return mergeEvidence{State: mergeNotMerged, Target: "origin/main"} }
	collectors.pr = func(string) prEvidence {
		return prEvidence{State: "merged_other_base", Number: 9, HeadOID: "abc", Base: "develop", Source: "gh"}
	}
	require.True(t, hasBlocker(evaluateSafe(t, collectors, pruneSettings{}), "not merged"))
}

func TestIgnoredAllowed(t *testing.T) {
	require.True(t, ignoredAllowed(".env", []string{".env"}))
	require.True(t, ignoredAllowed("storage/logs/a.log", []string{"storage"}))
	require.True(t, ignoredAllowed("db/test.sqlite", []string{"db/*.sqlite"}))
	require.False(t, ignoredAllowed("config/.env", []string{".env"}))
	require.False(t, ignoredAllowed(".env.local", []string{".env"}))
}

func TestRemoveLifecycleRecheckRunsUnderExclusiveGateBeforeAnyEffect(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	out, err := exec.Command("git", "init", repo).CombinedOutput()
	require.NoError(t, err, string(out))
	target := filepath.Join(base, "target")
	out, err = exec.Command("git", "-C", repo, "worktree", "add", "--orphan", target).CombinedOutput()
	require.NoError(t, err, string(out))
	marker := filepath.Join(base, "cleanup-effect")
	pc := &ProjectContext{GitDir: filepath.Join(repo, ".git"), Config: &config.Config{Cleanup: config.CleanupConfig{Steps: []config.CleanupStep{{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"command": "touch " + marker}}}}}}}
	removed := false
	deps := removeLifecycleDependencies{
		worktreeGate:   lifecycle.TryExclusive,
		readLocalState: config.ReadLocalState,
		resolvePreset: func(*ProjectContext, string, string) presets.ResolvedPreset {
			return presets.NewManager().Resolve("", "", target)
		},
		scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return scaffold.NewScaffoldManager() },
		removeWorktree: func(string, string, bool) error {
			removed = true
			return nil
		},
		printInfo: func(string) {},
	}
	var gateHeld bool
	recheck := func() error {
		shared, err := lifecycle.AcquireShared(target)
		if err == nil {
			return errors.Join(errors.New("gate not held during recheck"), shared.Release())
		}
		gateHeld = errors.Is(err, lockfile.ErrLocked)
		return &applySkip{code: applyCodeBlocked, reason: "blocked now"}
	}
	err = runRemoveLifecycle(pc, git.Worktree{Path: target, Branch: "target"}, removeLifecycleOptions{Quiet: true, Recheck: recheck}, scaffold.CleanupOptions{}, nil, deps)
	var skip *applySkip
	require.ErrorAs(t, err, &skip)
	require.True(t, gateHeld, "the recheck must run while the exclusive lifecycle gate is held")
	require.False(t, removed)
	require.NoFileExists(t, marker, "no cleanup step runs after a failed recheck")
	require.DirExists(t, target)
}
