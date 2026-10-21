package harness

// The editor extension bundle is another claude install form: the current
// layout's bundled native binary launches directly, the earlier cli.js
// layout runs under node as the launch's script operand.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

// claudeExtensionFixture builds a fake home with an editor extensions root
// and returns both; tests create the versioned extension trees they probe.
func claudeExtensionFixture(t *testing.T) (home, extensions string) {
	t.Helper()
	home = resolveNativePath(t.TempDir())
	extensions = filepath.Join(home, ".vscode", "extensions")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return home, extensions
}

// A bundled native binary under the extension's resources tree is a direct
// launch with no script operand, sourced "bundle"; the glob picks the
// greatest numeric version directory.
func TestClaudeResolvePathsBindsExtensionBinary(t *testing.T) {
	home, extensions := claudeExtensionFixture(t)
	binary := filepath.Join(extensions, "anthropic.claude-code-2.10.303-win32-x64", "resources", "native-binary")
	for _, version := range []string{"anthropic.claude-code-2.9.303-win32-x64", "anthropic.claude-code-2.10.303-win32-x64"} {
		dir := filepath.Join(extensions, version, "resources", "native-binary")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"claude", "claude.exe"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(version), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", t.TempDir())
	native, sources := map[string]string{}, map[string]string{}
	if err := claudeResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(binary, "claude")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if native["claudeExecutable"] != want {
		t.Fatalf("claudeExecutable = %q, want the bundled %q", native["claudeExecutable"], want)
	}
	if native["claudeScript"] != "" {
		t.Fatalf("claudeScript = %q, want none for a direct binary", native["claudeScript"])
	}
	if sources["claudeExecutable"] != "bundle" {
		t.Fatalf("claudeExecutable source = %q, want bundle", sources["claudeExecutable"])
	}
	if native["claudeHome"] != home {
		t.Fatalf("claudeHome = %q, want %q", native["claudeHome"], home)
	}
	binding, err := claudeConfiguredBinding(settings.Values{Native: native})
	if err != nil {
		t.Fatal(err)
	}
	launch := objectOrEmpty(getOf(binding, "launch"))
	if textOf(getOf(launch, "executable")) != want {
		t.Fatalf("binding launch = %v, want the bundled binary", launch)
	}
	if _, present := launch["script"]; present {
		t.Fatalf("binding launch = %v, want no script operand", launch)
	}
}

// The earlier layout's claude-code/cli.js is a node script: the launch binds
// node as the executable and the script as its operand.
func TestClaudeResolvePathsDerivesScriptFromExtensionCliJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	node = resolveNativePath(node)
	home, extensions := claudeExtensionFixture(t)
	script := filepath.Join(extensions, "anthropic.claude-code-2.1.302-win32-x64", "resources", "claude-code", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("// cli"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(node))
	native, sources := map[string]string{}, map[string]string{}
	if err := claudeResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	if native["claudeExecutable"] != node || native["claudeScript"] != script {
		t.Fatalf("resolved = %q + %q, want node + %q", native["claudeExecutable"], native["claudeScript"], script)
	}
	if sources["claudeExecutable"] != "PATH" || sources["claudeScript"] != "bundle" {
		t.Fatalf("sources = %v, want executable PATH and script bundle", sources)
	}
	binding, err := claudeConfiguredBinding(settings.Values{Native: native})
	if err != nil {
		t.Fatal(err)
	}
	launch := objectOrEmpty(getOf(binding, "launch"))
	if textOf(getOf(launch, "executable")) != node || textOf(getOf(launch, "script")) != script {
		t.Fatalf("binding launch = %v, want node + cli.js", launch)
	}
	argv, _, err := claudeArguments(map[string]any{"read_only": true}, t.TempDir(), launch, home, map[string]string{"CLAUDE_CONFIG_DIR": native["claudeConfigRoot"]}, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 3 || argv[0] != node || argv[1] != script || argv[2] != "-p" {
		t.Fatalf("spawn argv = %v, want [node cli.js -p ...]", argv)
	}
}

// Without a CLI on PATH or any extension bundle the resolve stays the honest
// unavailable refusal.
func TestClaudeResolvePathsWithoutExtensionBundle(t *testing.T) {
	home := resolveNativePath(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("PATH", t.TempDir())
	err := claudeResolvePaths(map[string]string{}, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_UNAVAILABLE") {
		t.Fatalf("resolve = %v, want CLAUDE_UNAVAILABLE", err)
	}
}
