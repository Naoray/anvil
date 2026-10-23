package warm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) (string, *Install) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{}}`), 0644))
	i, err := prepare(root, "node_modules", "toolchain", []string{"ci"})
	require.NoError(t, err)
	require.NotNil(t, i)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", ".bin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "value"), []byte("original"), 0755))
	if err := os.Symlink("../value", filepath.Join(root, "node_modules", ".bin", "value")); err != nil {
		t.Log("symlink unavailable:", err)
	}
	require.NoError(t, i.Record())
	return root, i
}
func target(t *testing.T, source string) *Install {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"package.json", "package-lock.json"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, name), b, 0644))
	}
	i, err := prepare(root, "node_modules", "toolchain", []string{"ci"})
	require.NoError(t, err)
	require.NotNil(t, i)
	return i
}
func TestReuseIsolatedAndAttested(t *testing.T) {
	src, _ := fixture(t)
	first, second := target(t, src), target(t, src)
	for _, i := range []*Install{first, second} {
		result, err := i.Reuse(src, "copy")
		require.NoError(t, err)
		require.Equal(t, "copied", result)
		b, err := os.ReadFile(filepath.Join(i.root, "node_modules", "value"))
		require.NoError(t, err)
		require.Equal(t, "original", string(b))
		require.NoError(t, i.Record())
	}
	require.NoError(t, os.WriteFile(filepath.Join(first.root, "node_modules", "value"), []byte("changed"), 0644))
	for _, root := range []string{src, second.root} {
		b, err := os.ReadFile(filepath.Join(root, "node_modules", "value"))
		require.NoError(t, err)
		require.Equal(t, "original", string(b))
	}
	require.NoError(t, os.WriteFile(filepath.Join(src, "node_modules", "value"), []byte("stale"), 0755))
	result, err := target(t, src).Reuse(src, "copy")
	require.NoError(t, err)
	require.Equal(t, "cold", result)
}
func TestInputAndTreeChangesGoCold(t *testing.T) {
	for _, change := range []string{"lock", "manifest", "mode", "symlink", "toolchain", "missing stamp"} {
		t.Run(change, func(t *testing.T) {
			src, _ := fixture(t)
			i := target(t, src)
			switch change {
			case "lock":
				require.NoError(t, os.WriteFile(filepath.Join(i.root, "package-lock.json"), []byte(`{"new":true}`), 0644))
			case "manifest":
				require.NoError(t, os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"new":true}`), 0644))
			case "mode":
				path := filepath.Join(src, "node_modules", "value")
				before, e := os.Stat(path)
				require.NoError(t, e)
				require.NoError(t, os.Chmod(path, 0600))
				after, e := os.Stat(path)
				require.NoError(t, e)
				if before.Mode() == after.Mode() {
					t.Skip("filesystem does not expose this permission change")
				}

			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(src, "node_modules", "escape")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "toolchain":
				i.toolchain = "different-platform"
			case "missing stamp":
				require.NoError(t, os.Remove(filepath.Join(src, ".anvil.local")))
			}
			result, err := i.Reuse(src, "copy")
			require.NoError(t, err)
			require.Equal(t, "cold", result)
			_, err = os.Lstat(filepath.Join(i.root, "node_modules"))
			require.True(t, os.IsNotExist(err))
		})
	}
}
func TestInstallInputsMustStayUnchanged(t *testing.T) {
	src, i := fixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"changed":true}`), 0644))
	require.NoError(t, i.Record())
	result, err := target(t, src).Reuse(src, "copy")
	require.NoError(t, err)
	require.Equal(t, "cold", result)
}
func TestStagedMutationAndIOFailureNeverPublish(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "mutation", true: "io"}[failure], func(t *testing.T) {
			src, _ := fixture(t)
			i := target(t, src)
			result, err := i.reuse(src, "copy", func(a, b string, mode os.FileMode, copyMode string) (string, error) {
				if failure {
					return "", os.ErrPermission
				}
				r, e := copyFile(a, b, mode, copyMode)
				if e == nil {
					e = os.WriteFile(b, []byte("mutation"), mode)
				}
				return r, e
			})
			if failure {
				require.ErrorIs(t, err, os.ErrPermission)
			} else {
				require.NoError(t, err)
				require.Equal(t, "cold", result)
			}
			_, err = os.Lstat(filepath.Join(i.root, "node_modules"))
			require.True(t, os.IsNotExist(err))
			matches, e := filepath.Glob(filepath.Join(i.root, ".anvil-warm-*"))
			require.NoError(t, e)
			require.Empty(t, matches)
		})
	}
}
func TestCollisionPreserved(t *testing.T) {
	// An unstamped node_modules (e.g. left by a killed warm rebuild) is never
	// reused or certified: it goes cold and the normal installer replaces it.
	src, _ := fixture(t)
	i := target(t, src)
	require.NoError(t, os.Mkdir(filepath.Join(i.root, "node_modules"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(i.root, "node_modules", "mine"), []byte("keep"), 0644))
	result, err := i.Reuse(src, "auto")
	require.NoError(t, err)
	require.Equal(t, ColdUnverified, result)
	require.True(t, IsCold(result))
	data, err := os.ReadFile(filepath.Join(i.root, "node_modules", "mine"))
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	stored, err := readStamp(i.root, "node_modules")
	require.NoError(t, err)
	require.Empty(t, stored.Inputs)

	// A stamped (verified) destination tree is still a collision.
	stamped := target(t, src)
	require.NoError(t, os.Mkdir(filepath.Join(stamped.root, "node_modules"), 0755))
	require.NoError(t, stamped.Record())
	recorded, err := readStamp(stamped.root, "node_modules")
	require.NoError(t, err)
	require.NotEmpty(t, recorded.Inputs)
	_, err = stamped.Reuse(src, "auto")
	require.Error(t, err)

	// Composer trees keep refusing any existing destination.
	for _, name := range []string{"composer.json", "composer.lock"} {
		require.NoError(t, os.WriteFile(filepath.Join(src, name), []byte(`{}`), 0644))
	}
	vendorRoot := t.TempDir()
	for _, name := range []string{"composer.json", "composer.lock"} {
		require.NoError(t, os.WriteFile(filepath.Join(vendorRoot, name), []byte(`{}`), 0644))
	}
	vendor, err := prepare(vendorRoot, "vendor", "toolchain", []string{"install"})
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(vendorRoot, "vendor"), 0755))
	_, err = vendor.Reuse(src, "auto")
	require.Error(t, err)
}
func TestUnverifiableInstallsNotStamped(t *testing.T) {
	for _, content := range []string{`{"scripts":{"postinstall":"custom"}}`, `{"workspaces":["packages/*"]}`, `{"dependencies":{"local":"file:../local"}}`} {
		t.Run(content, func(t *testing.T) {
			src, _ := fixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(src, "package.json"), []byte(content), 0644))
			i, err := prepare(src, "node_modules", "toolchain", []string{"ci"})
			require.NoError(t, err)
			require.Nil(t, i)
		})
	}
}
func TestCopyFallbackAndUnexpectedError(t *testing.T) {
	src, _ := fixture(t)
	dest := filepath.Join(t.TempDir(), "out")
	result, err := copyWithClone(filepath.Join(src, "node_modules", "value"), dest, 0755, "auto", func(string, string) error { return errCloneUnsupported })
	require.NoError(t, err)
	require.Equal(t, "copied", result)
	_, err = copyWithClone(filepath.Join(src, "node_modules", "value"), filepath.Join(t.TempDir(), "out"), 0755, "auto", func(string, string) error { return os.ErrPermission })
	require.True(t, errors.Is(err, os.ErrPermission))
}

func TestIndependentTreesAndExcludedFiles(t *testing.T) {
	src, _ := fixture(t)
	for _, name := range []string{"composer.json", "composer.lock"} {
		require.NoError(t, os.WriteFile(filepath.Join(src, name), []byte(`{}`), 0644))
	}
	vendor, err := prepare(src, "vendor", "toolchain", []string{"install"})
	require.NoError(t, err)
	require.NotNil(t, vendor)
	require.NoError(t, os.Mkdir(filepath.Join(src, "vendor"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "vendor", "value"), []byte("composer"), 0644))
	require.NoError(t, vendor.Record())
	i := target(t, src)
	for _, name := range []string{"composer.json", "composer.lock"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(i.root, name), b, 0644))
	}
	v, err := prepare(i.root, "vendor", "toolchain", []string{"install"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(i.root, "package-lock.json"), []byte(`{"changed":true}`), 0644))
	result, err := i.Reuse(src, "copy")
	require.NoError(t, err)
	require.Equal(t, Cold, result)
	result, err = v.Reuse(src, "copy")
	require.NoError(t, err)
	require.Equal(t, "copied", result)
	for _, name := range []string{".git", ".env", ".anvil.local", "database.sqlite", "runtime.db"} {
		t.Run(name, func(t *testing.T) {
			source, install := fixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(source, "node_modules", name), []byte("private"), 0644))
			require.NoError(t, install.Record())
			result, err := target(t, source).Reuse(source, "copy")
			require.NoError(t, err)
			require.Equal(t, Cold, result)
		})
	}
}
func TestSourceAndTargetMutationDuringCopy(t *testing.T) {
	for _, kind := range []string{"source", "target input", "absolute path"} {
		t.Run(kind, func(t *testing.T) {
			src, _ := fixture(t)
			i := target(t, src)
			if kind == "absolute path" {
				require.NoError(t, os.WriteFile(filepath.Join(src, "node_modules", "value"), []byte(src+"/generated.php"), 0644))
				sourceInstall, err := prepare(src, "node_modules", "toolchain", []string{"ci"})
				require.NoError(t, err)
				require.NoError(t, sourceInstall.Record())
			}
			result, err := i.reuse(src, "copy", func(a, b string, m os.FileMode, mode string) (string, error) {
				result, err := copyFile(a, b, m, mode)
				if err == nil {
					if kind == "source" {
						err = os.WriteFile(a, []byte("source changed while copying"), m)
					} else {
						err = os.WriteFile(filepath.Join(i.root, "package.json"), []byte(`{"changed":true}`), 0644)
					}
				}
				return result, err
			})
			require.NoError(t, err)
			require.Equal(t, Cold, result)
			_, err = os.Stat(filepath.Join(i.root, "node_modules"))
			require.True(t, os.IsNotExist(err))
		})
	}
}
func TestNativeCloneIsolation(t *testing.T) {
	src, _ := fixture(t)
	i := target(t, src)
	result, err := i.Reuse(src, "auto")
	require.NoError(t, err)
	if result != "cloned" {
		t.Skipf("native clone not supported on this filesystem: %s", result)
	}
	require.NoError(t, os.WriteFile(filepath.Join(i.root, "node_modules", "value"), []byte("independent"), 0755))
	b, err := os.ReadFile(filepath.Join(src, "node_modules", "value"))
	require.NoError(t, err)
	require.Equal(t, "original", string(b))
}

func TestCloneOfRacingSymlinkNeverChmodsOutsideFile(t *testing.T) {
	src, _ := fixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0600))
	before, e := os.Stat(outside)
	require.NoError(t, e)
	dest := filepath.Join(t.TempDir(), "cloned")
	_, err := copyWithClone(filepath.Join(src, "node_modules", "value"), dest, 0755, "auto", func(_, out string) error { return os.Symlink(outside, out) })
	if err != nil && !errors.Is(err, errSourceChanged) {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.ErrorIs(t, err, errSourceChanged)
	info, err := os.Stat(outside)
	require.NoError(t, err)
	require.Equal(t, before.Mode(), info.Mode())
}

func TestReadonlyPartialTreeIsCleaned(t *testing.T) {
	src, install := fixture(t)
	dir := filepath.Join(src, "node_modules")
	require.NoError(t, os.Chmod(dir, 0555))
	t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0755)) })
	require.NoError(t, install.Record())
	i := target(t, src)
	result, err := i.reuse(src, "copy", func(a, b string, m os.FileMode, mode string) (string, error) {
		r, e := copyFile(a, b, m, mode)
		if e == nil {
			e = os.WriteFile(b, []byte("changed"), m)
		}
		return r, e
	})
	require.NoError(t, err)
	require.Equal(t, Cold, result)
	stages, err := filepath.Glob(filepath.Join(i.root, ".anvil-warm-*"))
	require.NoError(t, err)
	require.Empty(t, stages)
}

func TestNpmUnattestedLifecycleAndOldLockStayCold(t *testing.T) {
	for _, script := range []string{"prepublish", "preprepare", "postprepare"} {
		t.Run(script, func(t *testing.T) {
			src, _ := fixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"scripts":{"`+script+`":"custom"}}`), 0644))
			i, err := prepare(src, "node_modules", "toolchain", []string{"ci"})
			require.NoError(t, err)
			require.Nil(t, i)
		})
	}
	for _, lock := range []string{`{"lockfileVersion":1,"dependencies":{"a":{"version":"1"}}}`, `{"lockfileVersion":3,"packages":{"node_modules/a":{"version":"1"}}}`} {
		t.Run(lock, func(t *testing.T) {
			src, _ := fixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(src, "package-lock.json"), []byte(lock), 0644))
			i, err := prepare(src, "node_modules", "toolchain", []string{"ci"})
			require.NoError(t, err)
			require.Nil(t, i)
		})
	}
}

func TestShrinkwrapRequiresColdAndInvalidatesProvenance(t *testing.T) {
	for _, kind := range []string{"regular", "directory", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			src, i := fixture(t)
			path := filepath.Join(src, "npm-shrinkwrap.json")
			switch kind {
			case "regular":
				require.NoError(t, os.WriteFile(path, []byte(`{}`), 0644))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0755))
			case "dangling symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			prepared, err := prepare(src, "node_modules", "toolchain", []string{"ci"})
			require.NoError(t, err)
			require.Nil(t, prepared)
			require.NoError(t, i.Record())
			stored, err := readStamp(src, "node_modules")
			require.NoError(t, err)
			require.Empty(t, stored.Inputs, "unsupported input appearing during install must clear prior provenance")
			result, err := target(t, src).Reuse(src, "copy")
			require.NoError(t, err)
			require.Equal(t, Cold, result)
		})
	}
}

func TestAlternateComposerManifestRequiresColdBeforeProbes(t *testing.T) {
	t.Setenv("COMPOSER", "alternate.json")
	called := false
	i := &Install{root: t.TempDir(), tree: "vendor", probe: func(string) (string, error) { called = true; return "toolchain", nil }}
	_, err := i.fingerprint()
	require.Error(t, err)
	require.False(t, called, "unsupported manifest must be rejected before eligibility queries")
	require.Nil(t, Prepare(i.root, "php.composer", "composer", []string{"install"}))
}

func TestAlternateComposerInputDuringInstallCannotBeStamped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("COMPOSER", "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"name":"fixture/project"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "composer.lock"), []byte(`{"packages":[]}`), 0644))
	i, err := prepare(root, "vendor", "toolchain", []string{"install"})
	require.NoError(t, err)
	require.NotNil(t, i)
	require.NoError(t, os.Mkdir(filepath.Join(root, "vendor"), 0755))
	require.NoError(t, i.Record())
	stored, err := readStamp(root, "vendor")
	require.NoError(t, err)
	require.NotEmpty(t, stored.Inputs)
	t.Setenv("COMPOSER", "alternate.json")
	require.NoError(t, i.Record())
	stored, err = readStamp(root, "vendor")
	require.NoError(t, err)
	require.Empty(t, stored.Inputs)
}
