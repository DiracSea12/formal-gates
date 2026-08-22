package validate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestInstallableMetadataNoAutoIntake(t *testing.T) {
	root := copyPackageFixture(t)
	data, err := os.ReadFile(filepath.Join(root, "agents", "openai.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, obsolete := range []string{"GateWorkflow", "formal handoff", "every project-content modification"} {
		if strings.Contains(text, obsolete) {
			t.Fatalf("installable metadata retains obsolete %q instruction", obsolete)
		}
	}
	// 新触发模型下 "lightweight" 是正式流程内的路线，openai.yaml 会合法出现；断言其
	// 携带 V2 的关键指令（默认提醒一次、不自行触发、明确要求才完整受理、轻量不验证）。
	for _, current := range []string{"mention once", "not self-trigger", "before writes", "on explicit user request", "lightweight"} {
		if !strings.Contains(text, current) {
			t.Fatalf("installable metadata is missing current %q instruction", current)
		}
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
	mutateWorkflow(t, root, "macos-26-intel", "macos-custom-intel")

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
	actions, err := os.ReadDir(filepath.Join(source, "prompts", "actions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.Type().IsRegular() {
			copyValidateTestFile(t, source, target, filepath.ToSlash(filepath.Join("prompts", "actions", action.Name())))
		}
	}
	for _, rel := range []string{"install.command", "install.ps1", "install.bat"} {
		copyValidateTestFile(t, source, target, rel)
	}
	mustWriteValidateTest(t, filepath.Join(target, "bin", nativeBinaryName()), "#!/usr/bin/env sh\nexit 0\n")
	return target
}

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
