package validate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPackageRejectsInvalidGatePrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		rel  string
		data []byte
	}{
		{name: "bad id", rel: "gates/Not-A-Gate.md", data: []byte("prompt\n")},
		{name: "empty", rel: "gates/empty.md", data: []byte(" \n\t")},
		{name: "invalid utf8", rel: "gates/invalid.md", data: []byte{0xff, 0xfe}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyPackageFixture(t)
			path := filepath.Join(root, filepath.FromSlash(tc.rel))
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			result := Package(root)
			if result.OK() || !resultHasPath(result, "prompts/gates") {
				t.Fatalf("expected gate validation failure at %s, got %#v", tc.rel, result.Failures)
			}
		})
	}
}

func TestPackageRequiresNativeHostScripts(t *testing.T) {
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		t.Run(name, func(t *testing.T) {
			root := copyPackageFixture(t)
			if result := Package(root); !result.OK() {
				t.Fatalf("complete package is invalid: %+v", result.Failures)
			}
			rel := "internal/harness/" + name
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
			result := Package(root)
			if result.OK() || !resultHasPath(result, rel) {
				t.Fatalf("package accepted a missing native host script %s: %+v", rel, result.Failures)
			}
		})
	}
}

func TestPackageManifestRequiresNativeHostScripts(t *testing.T) {
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		t.Run(name, func(t *testing.T) {
			root := copyPackageFixture(t)
			rel := "internal/harness/" + name
			// Remove only the package_parts member, keeping the actual script.
			mutateFile(t, root, "formal-gates.manifest.json", ",\n    \""+rel+"\"", "")
			result := Package(root)
			if result.OK() || !resultHasPath(result, "formal-gates.manifest.json") {
				t.Fatalf("manifest accepted an omitted native host script %s: %+v", rel, result.Failures)
			}
		})
	}
}

func TestPackageRejectsWorkflowLevelWritePermission(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "permissions:\n  contents: read", "permissions:\n  # comment should not hide write access\n  contents: write")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject workflow-level write permission")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageRejectsNonReleaseJobWritePermission(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "go-validation:\n    name:", "go-validation:\n    permissions:\n      contents: write\n    name:")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject non-release contents write permission")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageRejectsReleaseJobMissingWritePermission(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "release-evidence:\n    name:", "release-evidence:\n    permissions:\n      contents: read\n    name:")
	mutateWorkflow(t, root, "    permissions:\n      contents: write\n", "")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject release job without write permission")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageRejectsReleaseJobWriteAllPermission(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "    permissions:\n      contents: write", "    permissions: write-all")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject release job write-all permission")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageRejectsReleaseJobExtraWritePermission(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "    permissions:\n      contents: write", "    permissions:\n      contents: write\n      issues: write")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject release job extra write permission")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageRejectsReleaseJobWithoutReleaseCondition(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "    if: github.event_name == 'release'\n", "")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject release job without release condition")
	}
	requireWorkflowFailure(t, result)
}

func TestPackageAcceptsPermissionIndentAndCommentVariations(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "permissions:\n  contents: read", "permissions:\n  # top-level workflow token stays read-only\n  contents: read")
	mutateWorkflow(t, root, "    permissions:\n      contents: write", "    permissions:\n      # release upload is the only write-scoped job\n      contents: write")

	var result Result
	validateCI(root, &result)
	if !result.OK() {
		t.Fatalf("expected package validation to accept semantic permission layout, got %#v", result.Failures)
	}
}

func TestPackageAllowsMacOSRunnerLabelChanges(t *testing.T) {
	root := copyPackageFixture(t)
	mutateWorkflow(t, root, "macos-latest", "macos-custom-arm64")
	mutateWorkflow(t, root, "macos-15-intel", "macos-custom-intel")

	result := Package(root)
	if !result.OK() {
		t.Fatalf("expected package validation to allow renamed macOS runners, got %#v", result.Failures)
	}
}

func TestPackageRejectsBootstrapDarwinSuffix(t *testing.T) {
	root := copyPackageFixture(t)
	mutateFile(t, root, "install.command", `Darwin) os="macos" ;;`, `Darwin) os="darwin" ;;`)

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject darwin bootstrap suffix")
	}
	if !resultHasPath(result, "install.command") {
		t.Fatalf("expected install.command failure, got %#v", result.Failures)
	}
}

func TestPackageRejectsBootstrapUnpublishedArmSuffixes(t *testing.T) {
	root := copyPackageFixture(t)
	mutateFile(t, root, "install.command", "macos-arm64|macos-amd64|linux-amd64", "macos-arm64|macos-amd64|linux-amd64|linux-arm64")
	mutateFile(t, root, "install.ps1", `$suffix -ne "windows-amd64"`, `$suffix -ne "windows-arm64"`)

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject unpublished bootstrap suffixes")
	}
	if !resultHasPath(result, "install.command") || !resultHasPath(result, "install.ps1") {
		t.Fatalf("expected bootstrap script failures, got %#v", result.Failures)
	}
}

func TestPackageRejectsBootstrapCanaryChecksumOmitted(t *testing.T) {
	root := copyPackageFixture(t)
	mutateFile(t, root, "install.ps1", `foreach ($file in @($asset, $canary))`, `foreach ($file in @($asset))`)

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject missing canary checksum validation")
	}
	if !resultHasPath(result, "install.ps1") {
		t.Fatalf("expected install.ps1 failure, got %#v", result.Failures)
	}
}

func TestPackageAllowsInstalledRuntimeWithoutBootstrapScripts(t *testing.T) {
	root := copyPackageFixture(t)
	for _, rel := range []string{"install.command", "install.ps1", "install.bat"} {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}

	result := Package(root)
	if !result.OK() {
		t.Fatalf("expected installed runtime package validation to pass without bootstrap scripts, got %#v", result.Failures)
	}
}

func TestPackageRejectsPartialBootstrapScripts(t *testing.T) {
	for _, rel := range []string{"install.command", "install.ps1", "install.bat"} {
		t.Run(rel, func(t *testing.T) {
			root := copyPackageFixture(t)
			if err := os.Remove(filepath.Join(root, rel)); err != nil {
				t.Fatal(err)
			}

			result := Package(root)
			if result.OK() {
				t.Fatal("expected package validation to reject incomplete bootstrap script set")
			}
			if !resultHasPath(result, rel) {
				t.Fatalf("expected %s failure, got %#v", rel, result.Failures)
			}
		})
	}
}

func copyPackageFixture(t *testing.T) string {
	t.Helper()
	source := repoRootValidateTest(t)
	target := t.TempDir()
	for _, rel := range requiredDirs {
		if err := os.MkdirAll(filepath.Join(target, filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range requiredFiles {
		copyValidateTestFile(t, source, target, rel)
	}
	gates, err := os.ReadDir(filepath.Join(source, "gates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range gates {
		if gate.Type().IsRegular() {
			copyValidateTestFile(t, source, target, filepath.ToSlash(filepath.Join("gates", gate.Name())))
		}
	}
	for _, directory := range []string{"actions", "roles"} {
		entries, err := os.ReadDir(filepath.Join(source, "prompts", directory))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				copyValidateTestFile(t, source, target, filepath.ToSlash(filepath.Join("prompts", directory, entry.Name())))
			}
		}
	}
	for _, rel := range []string{"install.command", "install.ps1", "install.bat"} {
		copyValidateTestFile(t, source, target, rel)
	}
	mustWriteValidateTest(t, filepath.Join(target, "bin", nativeBinaryName()), string(stubBinaryPayload(t)))
	if err := os.Chmod(filepath.Join(target, "bin", nativeBinaryName()), 0o700); err != nil {
		t.Fatal(err)
	}
	return target
}

// stubBinaryPayload provides the native fixture package owner's real protocol.
func stubBinaryPayload(t *testing.T) []byte {
	t.Helper()
	data, err := packageOwnerBinary()
	if err != nil {
		t.Fatalf("build fixture package owner: %v", err)
	}
	return data
}

var packageOwnerBinary = sync.OnceValues(func() ([]byte, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("could not locate the test source")
	}
	dir, err := os.MkdirTemp("", "formal-gates-package-owner-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, nativeBinaryName())
	build := exec.Command("go", "build", "-o", path, filepath.Join(filepath.Dir(sourceFile), "testdata", "package_owner.go"))
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build package owner: %w: %s", err, out)
	}
	return os.ReadFile(path)
})

func mutateWorkflow(t *testing.T, root, old, new string) {
	t.Helper()
	mutateFile(t, root, ".github/workflows/portable-validation.yml", old, new)
}

func mutateFile(t *testing.T, root, rel, old, new string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(text), old, new, 1)
	if updated == string(text) {
		t.Fatalf("%s fixture did not contain %q", rel, old)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireWorkflowFailure(t *testing.T, result Result) {
	t.Helper()
	if !resultHasPath(result, ".github/workflows/portable-validation.yml") {
		t.Fatalf("expected workflow failure, got %#v", result.Failures)
	}
}

func copyValidateTestFile(t *testing.T, sourceRoot, targetRoot, rel string) {
	t.Helper()
	source := filepath.Join(sourceRoot, filepath.FromSlash(rel))
	target := filepath.Join(targetRoot, filepath.FromSlash(rel))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", rel, err)
	}
}

func mustWriteValidateTest(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRootValidateTest(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the package test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func resultHasPath(result Result, expected string) bool {
	expected = filepath.ToSlash(expected)
	for _, failure := range result.Failures {
		if strings.EqualFold(filepath.ToSlash(failure.Path), expected) {
			return true
		}
	}
	return false
}
