package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func nativePathsFixture(t *testing.T) (string, func(string, string) string) {
	t.Helper()
	workspace := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(body))
		}
	}
	for _, name := range []string{"left.txt", "right.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("initial\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "Fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(body))
		}
	}
	return workspace, func(name, artifact string) string {
		job := filepath.Join(t.TempDir(), "native")
		if err := os.Mkdir(job, 0700); err != nil {
			t.Fatal(err)
		}
		material := worker.Material{Input: protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, VCS: "git", Handler: "engine.development.batch.v6", Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}, Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{Artifacts: []string{artifact}}}}}}
		materialPath := filepath.Join(job, "material.json")
		body, _ := json.Marshal(material)
		if err := os.WriteFile(materialPath, body, 0600); err != nil {
			t.Fatal(err)
		}
		request := map[string]any{"workspace": workspace, "read_only": false, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath}, "runId": name}}
		if err := writeAtomicJSON(filepath.Join(job, "native-request.json"), request); err != nil {
			t.Fatal(err)
		}
		if err := claimJob(job, request); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"native-source.json", "native-events.jsonl"} {
			if err := os.WriteFile(filepath.Join(job, file), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return job
	}
}

func TestNativeTerminalPathsPreserveSharedWorkspaceAndOriginalSource(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	left, right := job("left", "left.txt"), job("right", "right.txt")
	for _, name := range []string{"left.txt", "right.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("delivered\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{left, right} {
		if err := submitTerminalPathFixture(t, directory); err != nil {
			t.Fatal(err)
		}
		source, err := evidenceSource(directory, "fixture", "session", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyNativeSource(directory, source, "fixture"); err != nil {
			t.Fatal(err)
		}
		if !valuesEqual(getOf(getOf(source, "effects"), "paths"), []any{"left.txt", "right.txt"}) {
			t.Fatal("workspace diff was filtered by an individual batch", source)
		}
		if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "later.txt"), []byte("later writer\n"), 0600); err != nil {
			t.Fatal(err)
		}
		recovered, err := evidenceSource(directory, "fixture", "session", nil)
		if err != nil || !valuesEqual(source, recovered) {
			t.Fatal("lost response absorbed later workspace changes", err)
		}
		effects := objectOrEmpty(recovered["effects"])
		effects["workspace"] = filepath.Join(workspace, "other")
		if err := verifyNativeSource(directory, recovered, "fixture"); err == nil {
			t.Fatal("source ignored the captured workspace binding")
		}
		if err := os.Remove(filepath.Join(workspace, "later.txt")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeTerminalPathsMissingAtCompletedExitNeverRecapture(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	directory := job("completed", "left.txt")
	if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "future.txt"), []byte("future\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := vcs.NewNative(workspace, "git", nil).ChangedPaths()
	if err != nil || !reflect.DeepEqual(actual, []string{"future.txt"}) {
		t.Fatal("negative control lacks real later diff", actual, err)
	}
	if err := submitTerminalPathFixture(t, directory); err == nil || !strings.Contains(err.Error(), "terminated") {
		t.Fatal("completed exit was allowed to recapture future state", err)
	}
	for _, file := range []string{nativePathsFile, "answer.raw.txt"} {
		if _, err := os.Stat(filepath.Join(directory, file)); !os.IsNotExist(err) {
			t.Fatal("recovery created completion evidence", file, err)
		}
	}
}

func TestNativeTerminalPathsBindOriginalRequestAndSubmittedSemantics(t *testing.T) {
	_, job := nativePathsFixture(t)
	directory := job("empty", "left.txt")
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	facts, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || facts.Paths == nil || len(facts.Paths) != 0 {
		t.Fatal("captured empty diff must be an explicit empty array", facts, err)
	}
	if _, err := nativePathEvidence(directory, `{"changed":true}`, false); err == nil {
		t.Fatal("captured facts accepted another native answer")
	}
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), []byte(`{"workspace":"different","read_only":false,"engine":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false); err == nil {
		t.Fatal("captured facts accepted another native request")
	}
}

func TestWhiteboxTerminalPathsIncludeCommittedMaterialsAndActualRemovals(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(body))
		}
		return strings.TrimSpace(string(body))
	}
	base := git("rev-parse", "HEAD")
	for _, name := range []string{"retained_test.txt", "removed_test.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("assert actual behavior\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "--all")
	git("commit", "-qm", "Test materials")
	directory := job("whitebox", "")
	materialPath := filepath.Join(directory, "material.json")
	body, err := os.ReadFile(materialPath)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	material.Input.Handler = "engine.qa.design.v6"
	material.Input.Validation.Workspace.Source = base
	material.Input.Validation.ReviewBase = base
	body, err = json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	answer, err := os.ReadFile(filepath.Join(directory, "answer.json"))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := nativePathEvidence(directory, string(answer), false)
	if err != nil || !reflect.DeepEqual(facts.Paths, []string{"removed_test.txt", "retained_test.txt"}) {
		t.Fatalf("semantic revision lost committed test materials: %+v %v", facts, err)
	}
	if err := os.Remove(filepath.Join(workspace, "removed_test.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "added_test.txt"), []byte("new assertion\n"), 0600); err != nil {
		t.Fatal(err)
	}
	directory = job("whitebox", "")
	materialPath = filepath.Join(directory, "material.json")
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	facts, err = nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || !reflect.DeepEqual(facts.Paths, []string{"added_test.txt", "retained_test.txt"}) {
		t.Fatalf("complete material diff retained a file deleted back to development: %+v %v", facts, err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "later_test.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || !reflect.DeepEqual(facts, recovered) {
		t.Fatal("recovery changed original complete paths", recovered, err)
	}
}

func submitTerminalPathFixture(t *testing.T, directory string) error {
	t.Helper()
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeSemanticTool, "arguments": map[string]any{"interruptionReason": "Path evidence fixture ended"}}})
	var out, errout bytes.Buffer
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		return fmt.Errorf("native tools: %s", &errout)
	}
	return writeSubmittedAnswer(directory)
}

func TestNativePathFreezePreservesSelectedPathsAndCurrentProof(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture")
	}
	for _, provider := range []string{"claude", "codex", "dsh"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			fixture := func(name string) (string, string, string) {
				bin, home, config := filepath.Join(root, name, "bin"), filepath.Join(root, name, "home"), filepath.Join(root, name, "independent-config")
				if err := os.MkdirAll(bin, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(home, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bin, provider), []byte("#!/bin/sh\nexit 91\n"), 0755); err != nil {
					t.Fatal(err)
				}
				return bin, home, config
			}
			aBin, aHome, aRoot := fixture("a")
			bBin, bHome, bRoot := fixture("b")
			envKey := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "dsh": "DSH_HOME"}[provider]
			t.Setenv("PATH", aBin)
			t.Setenv("HOME", aHome)
			t.Setenv(envKey, aRoot)
			effective := settings.Effective{Values: settings.Values{Native: map[string]string{"cursorExecutable": "/missing/unselected", "zcodeExecutable": "/missing/unselected", "workbuddyExecutable": "/missing/unselected"}}, Sources: map[string]string{provider + "Executable": "default", provider + "Home": "default"}}
			frozen, err := settings.Freeze(provider, effective)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(frozen)
			if err != nil {
				t.Fatal(err)
			}
			var restored settings.Effective
			if err := json.Unmarshal(body, &restored); err != nil || !reflect.DeepEqual(restored, frozen) {
				t.Fatalf("persisted paths = %#v, %v", restored, err)
			}
			t.Setenv("PATH", bBin)
			t.Setenv("HOME", bHome)
			t.Setenv(envKey, bRoot)
			var original map[string]any
			for i := 0; i < 2; i++ {
				binding, err := NativeBinding(provider, restored.Values)
				if err != nil {
					t.Fatal(err)
				}
				if getOf(binding["launch"], "executable") != resolveNativePath(filepath.Join(aBin, provider)) || !reflect.DeepEqual(binding["environment"], map[string]any{"HOME": resolveNativePath(aHome), envKey: resolveNativePath(aRoot)}) {
					t.Fatalf("binding followed changed environment: %#v", binding)
				}
				if original != nil && !reflect.DeepEqual(original, binding) {
					t.Fatal("repeated binding changed frozen paths", binding)
				}
				original = binding
			}
			if restored.Sources[provider+"Executable"] != "PATH" || restored.Sources[provider+"Home"] != "HOME" || restored.Sources[provider+"ConfigRoot"] != envKey {
				t.Fatal("sources do not describe discovery", restored.Sources)
			}
			if _, err := NativeBinding(provider, effective.Values); err == nil {
				t.Fatal("missing frozen paths were discovered late")
			}
			if err := os.WriteFile(filepath.Join(aBin, provider), []byte("#!/bin/sh\nexit 92\n"), 0755); err != nil {
				t.Fatal(err)
			}
			impl, _ := nativeImplementationFor(provider)
			if err := impl.validateBinding(original); err == nil {
				t.Fatal("changed executable proof was accepted")
			}
			updated, err := NativeBinding(provider, restored.Values)
			if err != nil || reflect.DeepEqual(updated["files"], original["files"]) {
				t.Fatal("new invocation did not prove current executable", updated, err)
			}
			fresh, err := settings.Freeze(provider, effective)
			if err != nil || fresh.Values.Native[provider+"Home"] != resolveNativePath(bHome) || fresh.Values.Native[provider+"ConfigRoot"] != resolveNativePath(bRoot) {
				t.Fatal("fresh run did not resolve B", fresh, err)
			}
			maintenance, err := impl.capture()
			if err != nil || getOf(maintenance["launch"], "executable") != resolveNativePath(filepath.Join(bBin, provider)) || !reflect.DeepEqual(maintenance["environment"], map[string]any{"HOME": resolveNativePath(bHome), envKey: resolveNativePath(bRoot)}) {
				t.Fatal("maintenance capture did not use current mapper", maintenance, err)
			}
			if os.Getenv("HOME") != bHome || os.Getenv(envKey) != bRoot {
				t.Fatal("binding modified process environment")
			}
		})
	}
}

func TestNativePathFreezePreservesHomePrecedenceAndSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture")
	}
	for _, provider := range []string{"claude", "codex", "dsh"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			executable := filepath.Join(root, provider)
			if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 91\n"), 0755); err != nil {
				t.Fatal(err)
			}
			home, explicitHome, independent := filepath.Join(root, "home"), filepath.Join(root, "explicit"), filepath.Join(root, "independent")
			envKey := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "dsh": "DSH_HOME"}[provider]
			t.Setenv("HOME", home)
			for _, tc := range []struct{ name, home, env, wantRoot, source string }{
				{"independent", "", independent, independent, envKey},
				{"derived", "", "", filepath.Join(home, "."+provider), provider + "Home"},
				{"explicit", explicitHome, independent, filepath.Join(explicitHome, "."+provider), provider + "Home"},
			} {
				if provider == "dsh" && tc.home != "" {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					t.Setenv(envKey, tc.env)
					effective := settings.Effective{Values: settings.Values{Native: map[string]string{provider + "Executable": executable, provider + "Home": tc.home}}, Sources: map[string]string{provider + "Executable": "project-config", provider + "Home": "default"}}
					if tc.home != "" {
						effective.Sources[provider+"Home"] = "home-config"
					}
					frozen, err := settings.Freeze(provider, effective)
					if err != nil {
						t.Fatal(err)
					}
					wantHome := home
					if tc.home != "" {
						wantHome = tc.home
					}
					if frozen.Values.Native[provider+"Home"] != resolveNativePath(wantHome) || frozen.Values.Native[provider+"ConfigRoot"] != resolveNativePath(tc.wantRoot) || frozen.Sources[provider+"ConfigRoot"] != tc.source || frozen.Sources[provider+"Executable"] != "project-config" || (tc.home != "" && frozen.Sources[provider+"Home"] != "home-config") {
						t.Fatal("native precedence/source changed", frozen)
					}
				})
			}
			if provider == "dsh" {
				for _, value := range []string{"~/store", `~\store`, " ", "~"} {
					t.Setenv(envKey, value)
					frozen, err := settings.Freeze(provider, settings.Effective{Values: settings.Values{Native: map[string]string{"dshExecutable": executable}}})
					want := filepath.Join(home, "store")
					if value == " " {
						want = filepath.Join(home, ".dsh")
					}
					if value == "~" {
						want = home
					}
					if err != nil || frozen.Values.Native["dshConfigRoot"] != resolveNativePath(want) {
						t.Fatal("DSH native home expansion differs", value, frozen, err)
					}
				}
			}
		})
	}
}

func TestClaudeArgumentsRequireFrozenConfigRoot(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, directory := range []string{a, b} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "settings.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", b)
	t.Setenv("HOME", b)
	args, cwd, err := claudeArguments(map[string]any{"identity": "original-session"}, root, "frozen-executable", root, map[string]string{"CLAUDE_CONFIG_DIR": a}, "exact prompt")
	if err != nil || cwd != root {
		t.Fatal(args, cwd, err)
	}
	if !reflect.DeepEqual(args[len(args)-4:], []string{"--resume", "original-session", "--settings", filepath.Join(a, "settings.json")}) {
		t.Fatal("arguments lost frozen config root/session", args)
	}
	if _, _, err := claudeArguments(map[string]any{}, root, "frozen-executable", root, map[string]string{}, "prompt"); err == nil {
		t.Fatal("missing frozen root used ambient environment")
	}
}

func TestDSHPathFreezeCarriesEnvironmentToNativeSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 ACP peer unavailable")
	}
	root := t.TempDir()
	bin, home, config, workspace := filepath.Join(root, "bin"), filepath.Join(root, "home-a"), filepath.Join(root, "root-a"), filepath.Join(root, "workspace")
	for _, directory := range []string{bin, home, config, workspace} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	records := filepath.Join(root, "calls.jsonl")
	quoted := func(v string) string { body, _ := json.Marshal(v); return string(body) }
	script := "#!" + python + "\n" + `import json,os,sys
expected_home = ` + quoted(resolveNativePath(home)) + `
expected_root = ` + quoted(resolveNativePath(config)) + `
expected_workspace = ` + quoted(resolveNativePath(workspace)) + `
assert os.environ["HOME"] == expected_home
assert os.environ["DSH_HOME"] == expected_root
assert os.getcwd() == expected_workspace
assert sys.argv[1:4] == ["--profile","acp","--patch"] and len(sys.argv) == 5
assert os.path.isfile(sys.argv[4])
identity = expected_root + "/session"
for line in sys.stdin:
    req = json.loads(line)
    method,params = req["method"],req["params"]
    with open(` + quoted(records) + `,"a") as out:
        out.write(json.dumps({"method":method,"params":params,"home":os.environ["HOME"],"root":os.environ["DSH_HOME"],"argv":sys.argv[1:]})+"\n")
    if method == "initialize":
        assert params == {"protocolVersion":1,"clientCapabilities":{}}
        result = {"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{},"close":{}}}}
    elif method in ["session/new","session/resume"]:
        expected = {"cwd":expected_workspace,"mcpServers":[]}
        if method == "session/resume": expected["sessionId"] = identity
        assert params == expected
        result = {"sessionId":identity,"configOptions":[{"id":"model","currentValue":"selected-native-model"}]}
    elif method == "session/close":
        assert params == {"sessionId":identity}
        result = {}
    else:
        raise AssertionError("unexpected/model method: "+method)
    print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":result}),flush=True)
`
	if err := os.WriteFile(filepath.Join(bin, "dsh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)
	t.Setenv("DSH_HOME", config)
	frozen, err := settings.Freeze("dsh", settings.Effective{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "home-b"))
	t.Setenv("DSH_HOME", filepath.Join(root, "root-b"))
	t.Setenv("PATH", filepath.Join(root, "absent-bin"))
	identity := resolveNativePath(config) + "/session"
	for _, session := range []string{"", identity} {
		binding, err := NativeBinding("dsh", frozen.Values)
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		result, err := dshExecute(map[string]any{"binding": binding, "workspace": workspace, "read_only": true, "identity": session}, directory, true)
		if err != nil || result["model_calls"] != 0 {
			t.Fatal("native probe", result, err)
		}
		persisted, err := readJSONFile(filepath.Join(directory, "identity.json"))
		if err != nil || getOf(persisted, "identity") != identity || getOf(persisted, "workspace") != resolveNativePath(workspace) {
			t.Fatal("native identity facts differ", persisted, err)
		}
	}
	body, err := os.ReadFile(records)
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, record["method"].(string))
		if record["home"] != resolveNativePath(home) || record["root"] != resolveNativePath(config) {
			t.Fatal("native process used ambient path", record)
		}
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "session/new", "session/close", "initialize", "session/resume", "session/close"}) {
		t.Fatal("native session protocol differs", methods)
	}
}

func TestNativeFrozenPathsKeepEditableBoundary(t *testing.T) {
	for _, field := range []string{"claudeConfigRoot", "codexConfigRoot", "dshHome", "dshConfigRoot", "cursorConfigRoot", "cursorDataRoot", "cursorCompileCache", "cursorCredentialStore"} {
		t.Run(field, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".gates"), 0700); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]string{field: filepath.Join(root, "selected")})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := settings.Load(root, home); err == nil || !strings.Contains(err.Error(), "unknown configuration field "+strconv.Quote(field)) {
				t.Fatal("internal carrier became editable", err)
			}
		})
	}
}

func TestNativeBindingRejectsIncompleteFrozenPaths(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "dsh"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			executable := filepath.Join(root, provider)
			if err := os.WriteFile(executable, []byte("native executable proof"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, missing := range []string{"Executable", "Home", "ConfigRoot"} {
				native := map[string]string{provider + "Executable": executable, provider + "Home": root, provider + "ConfigRoot": filepath.Join(root, "config")}
				delete(native, provider+missing)
				if _, err := NativeBinding(provider, settings.Values{Native: native}); err == nil || !strings.Contains(err.Error(), "frozen native paths required") {
					t.Fatal("incomplete frozen paths accepted", missing, err)
				}
			}
		})
	}
}
