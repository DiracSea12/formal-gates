package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

// The WorkBuddy desktop launcher on PATH anchors the install root; the bundled
// CLI under its resources tree is a node script, so the captured launch runs
// it through node with the script as operand.
func TestWorkBuddyResolvePathsDerivesScriptFromPathAnchor(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	node = resolveNativePath(node)
	root := resolveNativePath(t.TempDir())
	for _, name := range []string{"WorkBuddy", "WorkBuddy.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("launcher"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(root, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(`console.log("--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk")`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	home := resolveNativePath(t.TempDir())
	product := filepath.Join(home, ".workbuddy", "cache", "acc-product-config-v3.json")
	if err := os.MkdirAll(filepath.Dir(product), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(product, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+filepath.Dir(node))
	t.Setenv("PATHEXT", ".EXE")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	native, sources := map[string]string{}, map[string]string{}
	if err := workbuddyResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	if native["workbuddyExecutable"] != node {
		t.Fatalf("workbuddyExecutable = %q, want node %q", native["workbuddyExecutable"], node)
	}
	if native["workbuddyScript"] != script {
		t.Fatalf("workbuddyScript = %q, want the PATH-anchored %q", native["workbuddyScript"], script)
	}
	if sources["workbuddyExecutable"] != "PATH" || sources["workbuddyScript"] != "PATH" {
		t.Fatalf("sources = %v, want PATH for executable and script", sources)
	}
	binding, err := workbuddyConfiguredBinding(settings.Values{Native: native})
	if err != nil {
		t.Fatal(err)
	}
	launch := objectOrEmpty(getOf(binding, "launch"))
	if textOf(getOf(launch, "executable")) != node || textOf(getOf(launch, "script")) != script {
		t.Fatalf("binding launch = %v, want node + script", launch)
	}
	files := objectOrEmpty(getOf(binding, "files"))
	if getOf(files, node) == nil || getOf(files, script) == nil {
		t.Fatalf("binding file proofs = %v, want node and script", files)
	}
	argv, _, err := workbuddyArguments(map[string]any{"read_only": true}, t.TempDir(), launch, home, map[string]string{"CODEBUDDY_CONFIG_DIR": native["workbuddyConfigRoot"]}, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 3 || argv[0] != node || argv[1] != script || argv[2] != "--print" {
		t.Fatalf("spawn argv = %v, want [node script --print ...]", argv)
	}
}

// Without the desktop anchor the original tiers still apply: a codebuddy CLI
// on PATH resolves directly with no script operand, and an anchor without its
// bundled script or without node resolves nothing.
func TestWorkBuddyResolvePathsWithoutPathAnchor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	t.Setenv("PATHEXT", ".EXE")
	writeLauncher := func(dir string) {
		t.Helper()
		for _, name := range []string{"WorkBuddy", "WorkBuddy.exe"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("launcher"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("cbc on PATH", func(t *testing.T) {
		dir := resolveNativePath(t.TempDir())
		for _, name := range []string{"cbc", "cbc.exe"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("cli"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", dir)
		native, sources := map[string]string{}, map[string]string{}
		if err := workbuddyResolvePaths(native, sources); err != nil {
			t.Fatal(err)
		}
		if native["workbuddyExecutable"] != filepath.Join(dir, "cbc") && native["workbuddyExecutable"] != filepath.Join(dir, "cbc.exe") {
			t.Fatalf("workbuddyExecutable = %q, want the cbc CLI", native["workbuddyExecutable"])
		}
		if native["workbuddyScript"] != "" {
			t.Fatalf("workbuddyScript = %q, want none for a direct CLI", native["workbuddyScript"])
		}
		if sources["workbuddyExecutable"] != "PATH" {
			t.Fatalf("workbuddyExecutable source = %q, want PATH", sources["workbuddyExecutable"])
		}
	})
	t.Run("codebuddy on PATH", func(t *testing.T) {
		dir := resolveNativePath(t.TempDir())
		for _, name := range []string{"codebuddy", "codebuddy.exe"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("cli"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", dir)
		native, sources := map[string]string{}, map[string]string{}
		if err := workbuddyResolvePaths(native, sources); err != nil {
			t.Fatal(err)
		}
		if native["workbuddyExecutable"] != filepath.Join(dir, "codebuddy") && native["workbuddyExecutable"] != filepath.Join(dir, "codebuddy.exe") {
			t.Fatalf("workbuddyExecutable = %q, want the PATH CLI", native["workbuddyExecutable"])
		}
		if native["workbuddyScript"] != "" {
			t.Fatalf("workbuddyScript = %q, want none for a direct CLI", native["workbuddyScript"])
		}
		if sources["workbuddyExecutable"] != "PATH" {
			t.Fatalf("workbuddyExecutable source = %q, want PATH", sources["workbuddyExecutable"])
		}
	})
	t.Run("no anchor", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("an installed WorkBuddy.app supplies the platform default")
		}
		t.Setenv("PATH", t.TempDir())
		if err := workbuddyResolvePaths(map[string]string{}, map[string]string{}); err == nil || !strings.Contains(err.Error(), "WORKBUDDY_UNAVAILABLE") {
			t.Fatalf("resolve = %v, want WORKBUDDY_UNAVAILABLE", err)
		}
	})
	t.Run("anchor without bundled script", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("an installed WorkBuddy.app supplies the platform default")
		}
		dir := resolveNativePath(t.TempDir())
		writeLauncher(dir)
		t.Setenv("PATH", dir)
		if err := workbuddyResolvePaths(map[string]string{}, map[string]string{}); err == nil || !strings.Contains(err.Error(), "WORKBUDDY_UNAVAILABLE") {
			t.Fatalf("resolve = %v, want WORKBUDDY_UNAVAILABLE", err)
		}
	})
	t.Run("anchor and script without node", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("an installed WorkBuddy.app supplies the platform default")
		}
		dir := resolveNativePath(t.TempDir())
		writeLauncher(dir)
		script := filepath.Join(dir, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
		if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(script, []byte("cli"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		if err := workbuddyResolvePaths(map[string]string{}, map[string]string{}); err == nil || !strings.Contains(err.Error(), "WORKBUDDY_UNAVAILABLE") {
			t.Fatalf("resolve = %v, want WORKBUDDY_UNAVAILABLE", err)
		}
	})
}
