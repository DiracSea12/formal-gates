package facade

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

// A local endpoint records actual Codex adapter requests and exercises ordinary
// file/shell work. It is not a model or a test of Codex sandbox enforcement.
func TestCodexCoverageProtocolEndpoint(t *testing.T) {
	if os.Getenv("FORMAL_GATES_COVERAGE_ENDPOINT") != "1" {
		return
	}
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
	log, err := os.OpenFile(os.Getenv("FORMAL_GATES_COVERAGE_REQUESTS"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fail(err)
	}
	enc := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 65536), 4*1024*1024)
	resume, sandbox := false, ""
	const identity = "coverage-fixture-thread"
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage
			Method string
			Params map[string]json.RawMessage
		}
		if err := json.Unmarshal(scan.Bytes(), &q); err != nil {
			fail(err)
		}
		if _, err := log.Write(append(append([]byte{}, scan.Bytes()...), '\n')); err != nil {
			fail(err)
		}
		reply := func(v any) {
			if err := enc.Encode(map[string]any{"id": q.ID, "result": v}); err != nil {
				fail(err)
			}
		}
		switch q.Method {
		case "initialize":
			reply(map[string]any{})
		case "initialized":
		case "config/read":
			reply(map[string]any{"config": map[string]any{}})
		case "thread/start", "thread/resume":
			resume = q.Method == "thread/resume"
			_ = json.Unmarshal(q.Params["sandbox"], &sandbox)
			if q.Params["dynamicTools"] != nil {
				fail(fmt.Errorf("coverage thread received workflow tools"))
			}
			if resume {
				var id string
				_ = json.Unmarshal(q.Params["threadId"], &id)
				if id != identity || sandbox != "workspace-write" {
					fail(fmt.Errorf("authoring identity or permissions changed"))
				}
			} else if sandbox != "read-only" {
				fail(fmt.Errorf("coverage is not read-only"))
			}
			reply(map[string]any{"thread": map[string]string{"id": identity}})
		case "turn/start":
			if strings.Contains(string(q.Params["outputSchema"]), "uniqueItems") {
				fail(fmt.Errorf("unsupported output schema"))
			}
			body, err := os.ReadFile("behavior.txt")
			if err != nil || string(body) != "hello\n" {
				fail(fmt.Errorf("cannot read candidate: %v", err))
			}
			if resume {
				if err := os.WriteFile("probe.sh", []byte("#!/bin/sh\nset -eu\ntest \"$(cat behavior.txt)\" = hello\nprintf 'checked\\n'\n"), 0600); err != nil {
					fail(err)
				}
				out, err := exec.Command("sh", "probe.sh").CombinedOutput()
				if err != nil || string(out) != "checked\n" {
					fail(fmt.Errorf("trial failed: %s %v", out, err))
				}
				if err := os.WriteFile("trial.txt", out, 0600); err != nil {
					fail(err)
				}
			}
			reply(map[string]any{"turn": map[string]string{"id": "turn"}})
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]string{"type": "agentMessage", "text": "{}"}}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "completed"}}})
		}
	}
	if err := scan.Err(); err != nil {
		fail(err)
	}
	os.Exit(0)
}

func TestCodexCoverageThenAuthoringKeepsIdentityWithoutWorkflowTools(t *testing.T) {
	f, x := whiteboxInputFixture(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	job := t.TempDir()
	input, err := f.prepareNativeActionInput(run, x.Claim, x, job)
	if err != nil {
		t.Fatal(err)
	}
	if input.coverage == nil {
		t.Fatal("missing independent coverage phase")
	}
	if err := input.preflight(x.Claim, codexRuntimeAdapter{}); err != nil {
		t.Fatal(err)
	}
	workspace := input.material.workspace
	if err := os.WriteFile(filepath.Join(workspace, "behavior.txt"), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestCodexCoverageProtocolEndpoint$ -- \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	requests := filepath.Join(job, "requests.jsonl")
	claim := x.Claim
	claim.Context = &protocol.HostRuntimeContext{Provider: "codex", Executable: fake, Environment: map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir(), "CODEX_HOME": t.TempDir(), "FORMAL_GATES_COVERAGE_ENDPOINT": "1", "FORMAL_GATES_COVERAGE_REQUESTS": requests}}
	attached := []string{}
	turn := nativeTurn{Claim: claim, Settings: settings.Values{Model: "fixture"}, Workspace: workspace, ToolDescription: input.toolDescription, Attached: func(id string) error { attached = append(attached, id); return nil }, Operation: func(string, string, json.RawMessage) (any, error) {
		t.Error("unexpected workflow operation")
		return nil, nil
	}}
	for _, coverage := range []bool{true, false} {
		turn.Job = t.TempDir()
		turn.NoTools, turn.Resume = coverage, !coverage
		if coverage {
			turn.Prompt, turn.Schema = input.coverage.prompt, input.coverage.schema
		} else {
			turn.Prompt, turn.Schema = input.prompt, input.schema
		}
		id, err := (codexRuntimeAdapter{}).turn(turn)
		if err != nil {
			t.Fatal(err)
		}
		turn.Claim.Identity = id
	}
	if len(attached) != 2 || attached[0] != attached[1] {
		t.Fatalf("identity changed: %v", attached)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "trial.txt"))
	if err != nil || string(data) != "checked\n" {
		t.Fatalf("authoring trial unavailable: %s %v", data, err)
	}
	data, err = os.ReadFile(requests)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var q struct {
			Method string
			Params json.RawMessage
		}
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Fatal(err)
		}
		counts[q.Method]++
		if q.Method == "thread/start" || q.Method == "thread/resume" || q.Method == "turn/start" {
			t.Logf("received method=%s params_chars=%d", q.Method, len([]rune(string(q.Params))))
		}
	}
	if counts["thread/start"] != 1 || counts["thread/resume"] != 1 || counts["turn/start"] != 2 {
		t.Fatalf("unexpected calls: %v", counts)
	}
}
