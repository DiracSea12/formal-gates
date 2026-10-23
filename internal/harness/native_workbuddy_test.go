package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

// Desktop discovery binds WorkBuddy's own runtime, independent of system Node.
// The native catalogue retains all model IDs and their original selection data.
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
	if err := os.WriteFile(script, []byte(`
if (process.env.CODEBUDDY_FORCE_LITE_WB_BUNDLE !== "1") process.exit(91);
const args = process.argv.slice(2);
if (args.length === 1 && args[0] === "--help") {
  console.log("--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk");
} else {
  const expected = ["--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--tools", "", "--strict-mcp-config", "--mcp-config", '{"mcpServers":{}}', "--settings", '{"disableAllHooks":true}'];
  if (JSON.stringify(args) !== JSON.stringify(expected)) process.exit(92);
  const replies = [
    ["initialize", {currentModelId: "native-default", account: {token: "must-not-return"}}],
    ["get_available_models", {rawModels: [
      {id: "native-default", name: "Default", supportsReasoning: false},
      {id: "native-fast", name: "Fast", supportsReasoning: false},
      {id: "native-balanced", name: "Balanced", supportsReasoning: false},
      {id: "native-selected", name: "Same label", supportsReasoning: true, reasoning: {defaultEffort: "native-high", supportedEfforts: ["native-low", "native-high"]}},
      {id: "native-other", name: "Same label", supportsReasoning: false}
    ]}]
  ];
  let requests = 0;
  require("readline").createInterface({input: process.stdin}).on("line", line => {
    const frame = JSON.parse(line), next = replies.shift();
    if (!next || frame.type !== "control_request" || frame.request_id !== "models-" + (++requests) || JSON.stringify(frame.request) !== JSON.stringify({subtype: next[0]})) process.exit(93);
    console.log(JSON.stringify({type: "control_response", response: {subtype: "success", request_id: frame.request_id, response: next[1]}}));
  });
}
`), 0600); err != nil {
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
	t.Setenv("CODEBUDDY_FORCE_LITE_WB_BUNDLE", "")
	native, sources := map[string]string{}, map[string]string{}
	if err := workbuddyResolvePaths(native, sources); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(native["workbuddyExecutable"]) != root || !strings.HasPrefix(filepath.Base(native["workbuddyExecutable"]), "WorkBuddy") {
		t.Fatalf("workbuddyExecutable = %q, want the desktop runtime", native["workbuddyExecutable"])
	}
	if native["workbuddyScript"] != script {
		t.Fatalf("workbuddyScript = %q, want the PATH-anchored %q", native["workbuddyScript"], script)
	}
	if sources["workbuddyExecutable"] != "PATH" || sources["workbuddyScript"] != "PATH" {
		t.Fatalf("sources = %v, want PATH for executable and script", sources)
	}
	launch := map[string]any{"executable": node, "script": script}
	binding, err := buildCaptureBinding("workbuddy", launch)
	if err != nil {
		t.Fatal(err)
	}
	if textOf(getOf(launch, "executable")) != node || textOf(getOf(launch, "script")) != script {
		t.Fatalf("binding launch = %v, want node + script", launch)
	}
	files := objectOrEmpty(getOf(binding, "files"))
	if getOf(files, node) == nil || getOf(files, script) == nil {
		t.Fatalf("binding file proofs = %v, want node and script", files)
	}
	environment := nativeBindingEnvironment(binding)
	environment["CODEBUDDY_FORCE_LITE_WB_BUNDLE"] = "1"
	for _, target := range []string{"", "native-selected"} {
		catalogue, err := workbuddySelectionOptions(launch, home, t.TempDir(), environment, target)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, model := range catalogue.Models {
			ids = append(ids, model.Value)
		}
		if !reflect.DeepEqual(ids, []string{"native-default", "native-fast", "native-balanced", "native-selected", "native-other"}) || catalogue.Default != (host.ModelSelection{Model: "native-default"}) || catalogue.Current != catalogue.Default {
			t.Fatalf("native catalogue: %+v", catalogue)
		}
		if target == "native-selected" && (!reflect.DeepEqual(catalogue.Models[3].Efforts, []string{"native-low", "native-high"}) || catalogue.Models[3].DefaultEffort != "native-high") {
			t.Fatalf("selected native reasoning: %+v", catalogue.Models[3])
		}
		if target == "" && !reflect.DeepEqual(catalogue.Models[0].Efforts, []string{}) {
			t.Fatalf("nonreasoning native default: %+v", catalogue.Models[0])
		}
		encoded, _ := json.Marshal(catalogue)
		if strings.Contains(string(encoded), "must-not-return") {
			t.Fatal("native catalogue exposed account metadata")
		}
	}
	argv, _, err := workbuddyArguments(map[string]any{"read_only": true}, t.TempDir(), launch, home, map[string]string{"CODEBUDDY_CONFIG_DIR": native["workbuddyConfigRoot"]}, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 3 || argv[0] != node || argv[1] != script || argv[2] != "--print" {
		t.Fatalf("spawn argv = %v, want [node script --print ...]", argv)
	}
}

// A codebuddy CLI on PATH resolves directly with no script operand; desktop discovery requires
// its bundled script and does not require system Node.
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
		native := map[string]string{}
		if err := workbuddyResolvePaths(native, map[string]string{}); err != nil || native["workbuddyScript"] != script {
			t.Fatalf("resolve = %v, native = %v, want the desktop runtime and script", err, native)
		}
	})
}

func TestWorkBuddyRuntimeArgumentsPreserveCapturedLaunch(t *testing.T) {
	root, err := nativePackageRoot()
	if err != nil {
		t.Fatal(err)
	}
	launch := map[string]any{
		"executable": "desktop-runtime", "script": "original-cli",
		"bundle": "original-bundle", "archive": "original-archive",
	}
	before, err := CanonicalJSON(launch)
	if err != nil {
		t.Fatal(err)
	}
	configRoot := t.TempDir()
	identity := "native-session"
	record := filepath.Join(configRoot, "projects", "workspace", identity+".jsonl")
	if err := os.MkdirAll(filepath.Dir(record), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte("native session\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, resumed := range []bool{false, true} {
		request := map[string]any{"read_only": true, "model": "selected-model", "reasoning_effort": "high"}
		if resumed {
			request["identity"] = identity
		}
		argv, workspace, err := workbuddyArguments(request, t.TempDir(), launch, t.TempDir(), map[string]string{"CODEBUDDY_CONFIG_DIR": configRoot}, "prompt")
		if err != nil {
			t.Fatal(err)
		}
		defer workbuddyCleanup(workspace)
		if len(argv) < 3 || argv[0] != "desktop-runtime" || argv[1] != filepath.Join(root, "internal", "harness", "native_workbuddy.mjs") || argv[2] != "--print" {
			t.Fatalf("runtime argv: %v", argv)
		}
		options := map[string]string{}
		for i := 2; i+1 < len(argv); i++ {
			options[argv[i]] = argv[i+1]
		}
		if options["--model"] != "selected-model" || options["--effort"] != "high" || resumed && options["--resume"] != identity || !resumed && options["--session-id"] == "" {
			t.Fatalf("normal/resume selection changed: %v", argv)
		}
	}
	after, err := CanonicalJSON(launch)
	if err != nil || string(after) != string(before) {
		t.Fatalf("runtime arguments changed captured host identity: before=%s after=%s err=%v", before, after, err)
	}
}
