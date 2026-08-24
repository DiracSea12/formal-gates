package validate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 2026-08-21 事故回归（阶段 1 批次 0A）：两次测试运行把真实用户安装的
// ~/.formal-gates/releases/0.1.0-macos-arm64/bin/formal-gates 与
// ~/.local/bin/formal-gates 替换成 25 字节桩（与 copyPackageFixture 写入
// package_test.go 的桩逐字节一致），并向真实 ~/.formal-gates/registry.json
// 写入测试记录。根因：Install/Uninstall 的共享 registry 与稳定 launcher 默认
// 都按 HOME 解析（installRegistryPath / defaultStableLauncherPath ->
// installHomeDir），个别测试未隔离 HOME。以下用例锁死两条不变量：
//   - 故障注入的安装事务绝不触碰真实 HOME 下的事故路径；
//   - 事故形态（源包桩替换稳定 launcher + 写 registry）只落在注入的临时 HOME 内。

// isolationWatchedRealPaths returns the incident paths under the real user
// home. It must be called before the test replaces HOME with t.Setenv.
func isolationWatchedRealPaths(t *testing.T) []string {
	t.Helper()
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		t.Skip("HOME is not set: cannot fingerprint the real user installation")
	}
	return []string{
		filepath.Join(home, ".formal-gates", "registry.json"),
		filepath.Join(home, ".local", "bin", nativeBinaryName()),
		filepath.Join(home, ".formal-gates", "releases"),
	}
}

// isolationSnapshot digests each watched path; directories are digested
// recursively over sorted relative paths so any file inside them is covered.
// Missing paths map to a stable marker so "still absent" and "still identical"
// are both assertable. It only reads: fingerprinting the real installation
// must never mutate it.
func isolationSnapshot(t *testing.T, watched []string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, len(watched))
	for _, path := range watched {
		digest, err := isolationPathDigest(path)
		if err != nil {
			t.Fatalf("isolation snapshot could not read %s: %v", path, err)
		}
		snapshot[path] = digest
	}
	return snapshot
}

func isolationPathDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "<absent>", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		digest, err := fileDigest(path)
		if os.IsNotExist(err) {
			return "<absent>", nil
		}
		return digest, err
	}
	hasher := sha256.New()
	err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(path, current)
		if relErr != nil {
			return relErr
		}
		fmt.Fprintf(hasher, "%s\x00", filepath.ToSlash(rel))
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(current)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return nil
			}
			return readErr
		}
		hasher.Write(data)
		hasher.Write([]byte{'\x00'})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return "<absent>", nil
		}
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func assertRealPathsUntouched(t *testing.T, watched []string, before map[string]string) {
	t.Helper()
	after := isolationSnapshot(t, watched)
	for _, path := range watched {
		if before[path] != after[path] {
			t.Fatalf("test touched real user path %s: sha256 %s -> %s", path, before[path], after[path])
		}
	}
}

// Case: the incident's fault-injection shape — an install whose source carries
// the 25-byte stub launcher and whose transaction would replace the stable
// launcher — must never touch the real user home. The fault fires at the
// launcher publish boundary ("pointer"), the exact replacement step that
// corrupted the real installation on 2026-08-21.
func TestInstallFaultInjectionNeverTouchesRealUserHome(t *testing.T) {
	watched := isolationWatchedRealPaths(t)
	before := isolationSnapshot(t, watched)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "pointer")

	source := copyPackageFixture(t)
	project := t.TempDir()
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
		t.Fatalf("expected the injected pointer fault, got %v", err)
	}
	// The rolled-back transaction may leave nothing behind; whatever it did, the
	// real incident paths keep their byte-for-byte fingerprints.
	assertRealPathsUntouched(t, watched, before)
}

// Case: an explicit replica of the incident payload. With HOME injected into a
// temporary root, the stub-replacement and registry writes must land only
// inside that root — the structural proof that the resolution honors the
// injected home instead of the real one.
func TestInstallIncidentStubReplacementLandsOnlyInInjectedHome(t *testing.T) {
	watched := isolationWatchedRealPaths(t)
	before := isolationSnapshot(t, watched)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	source := copyPackageFixture(t)
	project := t.TempDir()
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, Force: true})
	if err != nil {
		t.Fatalf("isolated install failed: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("expected exactly one install target, got %d", len(report.Targets))
	}

	// The stable launcher replacement (the exact write that corrupted the real
	// installation) happened, but only under the injected home.
	stubPath := filepath.Join(home, ".local", "bin", nativeBinaryName())
	stub, err := os.ReadFile(stubPath)
	if err != nil {
		t.Fatalf("stable launcher was not published inside the injected home: %v", err)
	}
	if !bytes.Equal(stub, stubBinaryPayload(t)) {
		t.Fatalf("published launcher payload changed: %q (first bytes)", stub[:min(len(stub), 32)])
	}

	// The shared registry write also stayed inside the injected home.
	document, err := LoadRegistry(filepath.Join(home, ".formal-gates", "registry.json"))
	if err != nil {
		t.Fatalf("isolated registry was not written: %v", err)
	}
	if len(document.Records) == 0 {
		t.Fatal("isolated registry has no install record")
	}

	// And the real incident paths never moved.
	assertRealPathsUntouched(t, watched, before)
}
