package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageRejectsScriptRuntimeFiles(t *testing.T) {
	root := copyPackageFixture(t)
	mustWriteValidateTest(t, filepath.Join(root, "examples", "legacy-demo.ps1"), "legacy powershell\n")
	mustWriteValidateTest(t, filepath.Join(root, "hooks", "legacy-hook.sh"), "legacy shell\n")

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject script runtime files")
	}
	if !resultHasPath(result, "examples/legacy-demo.ps1") || !resultHasPath(result, "hooks/legacy-hook.sh") {
		t.Fatalf("expected script runtime file failures, got %#v", result.Failures)
	}
}

func TestPackageRejectsScriptsDirectory(t *testing.T) {
	root := copyPackageFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}

	result := Package(root)
	if result.OK() {
		t.Fatal("expected package validation to reject scripts directory")
	}
	if !resultHasPath(result, "scripts") {
		t.Fatalf("expected scripts directory failure, got %#v", result.Failures)
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
	copyValidateTestFile(t, source, target, "formal-gates.manifest.json")
	copyValidateTestFile(t, source, target, "test-prompts.json")
	copyValidateTestFile(t, source, target, "README.md")
	copyValidateTestFile(t, source, target, "README_EN.md")
	copyValidateTestFile(t, source, target, ".github/workflows/portable-validation.yml")
	return target
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
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
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
