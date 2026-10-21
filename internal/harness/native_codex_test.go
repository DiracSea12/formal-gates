package harness

// The codex CLI has two bundled install forms besides a PATH install: the
// chatgpt editor extension's platform binary and the desktop app's versioned
// release directory. Both are direct launches sourced "bundle".

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

// codexHomeFixture pins a fake home as the extensions root parent and clears
// the ambient configuration inputs a resolve could otherwise adopt.
func codexHomeFixture(t *testing.T) (home, extensions string) {
	t.Helper()
	home = resolveNativePath(t.TempDir())
	extensions = filepath.Join(home, ".vscode", "extensions")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	return home, extensions
}

// The extension's platform binary binds as a direct launch: the glob picks
// the greatest numeric version directory under the editor root.
func TestCodexResolvePathsBindsExtensionBinary(t *testing.T) {
	platform := codexExtensionBinDirs[runtime.GOOS+"/"+runtime.GOARCH]
	if platform == "" {
		t.Skip("the extension bundle ships no binary for this platform")
	}
	if runtime.GOOS+"/"+runtime.GOARCH == "windows/amd64" && platform != "windows-x86_64" {
		t.Fatalf("windows bundle dir = %q, want windows-x86_64", platform)
	}
	home, extensions := codexHomeFixture(t)
	binary := filepath.Join(extensions, "openai.chatgpt-26.1007.21434-win32-x64", "bin", platform)
	for _, version := range []string{"openai.chatgpt-26.1007.21434-win32-x64", "openai.chatgpt-26.721.30844-win32-x64"} {
		dir := filepath.Join(extensions, version, "bin", platform)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"codex", "codex.exe"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(version), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	native, sources := map[string]string{}, map[string]string{}
	if err := codexResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(binary, "codex")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if native["codexExecutable"] != want {
		t.Fatalf("codexExecutable = %q, want the bundled %q", native["codexExecutable"], want)
	}
	if sources["codexExecutable"] != "bundle" {
		t.Fatalf("codexExecutable source = %q, want bundle", sources["codexExecutable"])
	}
	if native["codexHome"] != home {
		t.Fatalf("codexHome = %q, want %q", native["codexHome"], home)
	}
	binding, err := codexConfiguredBinding(settings.Values{Native: native})
	if err != nil {
		t.Fatal(err)
	}
	launch := objectOrEmpty(getOf(binding, "launch"))
	if textOf(getOf(launch, "executable")) != want {
		t.Fatalf("binding launch = %v, want the bundled binary", launch)
	}
}

// The desktop app's release directory is a fallback after the extension
// bundle; its glob picks the last sorted release hash.
func TestCodexResolvePathsBindsDesktopRelease(t *testing.T) {
	codexHomeFixture(t)
	local := resolveNativePath(t.TempDir())
	t.Setenv("LOCALAPPDATA", local)
	for _, release := range []string{"896a0166b774037a", "ce4759538b7f2f1a"} {
		dir := filepath.Join(local, "OpenAI", "Codex", "bin", release)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "codex.exe"), []byte(release), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	native, sources := map[string]string{}, map[string]string{}
	if err := codexResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "OpenAI", "Codex", "bin", "ce4759538b7f2f1a", "codex.exe")
	if native["codexExecutable"] != want {
		t.Fatalf("codexExecutable = %q, want the last release %q", native["codexExecutable"], want)
	}
	if sources["codexExecutable"] != "bundle" {
		t.Fatalf("codexExecutable source = %q, want bundle", sources["codexExecutable"])
	}
}

// Without a CLI on PATH or any bundled install the resolve stays the honest
// unavailable refusal.
func TestCodexResolvePathsWithoutBundles(t *testing.T) {
	codexHomeFixture(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	err := codexResolvePaths(map[string]string{}, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "CODEX_UNAVAILABLE") {
		t.Fatalf("resolve = %v, want CODEX_UNAVAILABLE", err)
	}
}
