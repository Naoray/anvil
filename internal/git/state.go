package git

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// WorkingState identifies exactly what is checked out: HEAD plus the content
// of every path that differs from it (tracked modifications, deletions and
// untracked, non-ignored files). Two states share a Digest only when HEAD and
// all differing contents are identical; a dirty tree never shares a digest
// with its clean HEAD.
type WorkingState struct {
	Head string `json:"head"`
	// Tree is HEAD's tree object: the committed content independent of the
	// commit (two commits with identical content share it). It is not part
	// of Digest, so it never changes whether earlier results are current.
	Tree       string `json:"tree,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Dirty      bool   `json:"dirty"`
	DirtyPaths int    `json:"dirty_paths"`
	Digest     string `json:"state_digest"`
}

// excludedStatePaths never contribute to identity: Anvil's own local state
// changes as commands run and must not make results stale.
var excludedStatePaths = map[string]bool{".anvil.local": true}

// ReadWorkingState computes the working state of a worktree without writing
// to the repository (no index refresh, no objects).
func ReadWorkingState(worktreePath string) (WorkingState, error) {
	head, err := HeadCommit(worktreePath)
	if err != nil {
		return WorkingState{}, err
	}
	tree, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", head+"^{tree}").Output()
	if err != nil {
		return WorkingState{}, fmt.Errorf("resolving HEAD tree: %w", err)
	}
	state := WorkingState{Head: head, Tree: strings.TrimSpace(string(tree))}
	if branch, err := exec.Command("git", "-C", worktreePath, "symbolic-ref", "--quiet", "--short", "HEAD").Output(); err == nil {
		state.Branch = strings.TrimSpace(string(branch))
	}
	cmd := exec.Command("git", "--no-optional-locks", "-C", worktreePath, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--ignore-submodules=none")
	out, err := cmd.Output()
	if err != nil {
		return WorkingState{}, fmt.Errorf("reading working tree status: %w", err)
	}
	var entries []string
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) < 4 {
			continue
		}
		code, path := string(record[:2]), string(record[3:])
		if excludedStatePaths[path] {
			continue
		}
		content, err := pathDigest(filepath.Join(worktreePath, filepath.FromSlash(path)))
		if err != nil {
			return WorkingState{}, err
		}
		entries = append(entries, path+"\x00"+code+"\x00"+content)
	}
	sort.Strings(entries)
	state.Dirty = len(entries) > 0
	state.DirtyPaths = len(entries)
	h := sha256.New()
	writeField(h, "anvil-working-state-v1")
	writeField(h, head)
	for _, entry := range entries {
		writeField(h, entry)
	}
	state.Digest = hex.EncodeToString(h.Sum(nil))
	return state, nil
}

func writeField(w io.Writer, value string) {
	if _, err := fmt.Fprintf(w, "%d:%s\n", len(value), value); err != nil {
		panic(err) // hash writers never fail
	}
}

func pathDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "deleted", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return "link:" + target, nil
	case info.IsDir():
		// The parent porcelain status cannot identify recursive dirty contents.
		return "", fmt.Errorf("cannot identify dirty submodule or nested repository at %s; clean it before running or reusing checks", path)
	case !info.Mode().IsRegular():
		return "special", nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return "", err
	}
	mode := "file"
	if info.Mode()&0o111 != 0 {
		mode = "exec"
	}
	return mode + ":" + hex.EncodeToString(h.Sum(nil)), nil
}
