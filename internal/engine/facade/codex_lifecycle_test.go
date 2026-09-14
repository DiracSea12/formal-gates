package facade

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

// The endpoint owns a persistent writer marker until stdin EOF. SIGKILL cannot
// release it. Every subsequent resume must acquire it, including recovery probes.
func TestCodexLifecycleEndpoint(t *testing.T) {
	root := os.Getenv("FORMAL_GATES_LIFECYCLE_ENDPOINT")
	if root == "" {
		return
	}
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
	enc := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	owned := false
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage
			Method string
			Params map[string]json.RawMessage
		}
		if err := json.Unmarshal(scan.Bytes(), &q); err != nil {
			fail(err)
		}
		reply := func(result any) {
			if err := enc.Encode(map[string]any{"id": q.ID, "result": result}); err != nil {
				fail(err)
			}
		}
		switch q.Method {
		case "initialize":
			reply(map[string]any{})
		case "config/read":
			reply(map[string]any{"config": map[string]any{}})
		case "model/list":
			reply(map[string]any{"data": []any{map[string]any{"model": "fixture", "isDefault": true}}})
		case "thread/start", "thread/resume":
			if q.Method == "thread/resume" && string(q.Params["threadId"]) != `"fixture-thread"` {
				fail(fmt.Errorf("wrong resume identity"))
			}
			_, rejectErr := os.Stat(filepath.Join(root, "reject"))
			_, resumeErr := os.Stat(filepath.Join(root, "reject-resume"))
			if rejectErr == nil || (q.Method == "thread/resume" && resumeErr == nil) {
				_ = enc.Encode(map[string]any{"id": q.ID, "error": map[string]any{"message": "fixture startup rejected"}})
				continue
			}
			file, err := os.OpenFile(filepath.Join(root, "writer"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				_ = enc.Encode(map[string]any{"id": q.ID, "error": map[string]any{"message": "fixture thread already has an active writer"}})
				continue
			}
			_ = file.Close()
			owned = true
			reply(map[string]any{"thread": map[string]string{"id": "fixture-thread"}})
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "fixture-turn"}})
			if _, err := os.Stat(filepath.Join(root, "steer")); err == nil {
				continue
			}
			answer := `{"fixture":true}`
			if data, err := os.ReadFile(filepath.Join(root, "answer.json")); err == nil {
				answer = string(data)
			}
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]string{"type": "agentMessage", "text": answer}}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "completed"}}})
		case "turn/steer":
			if string(q.Params["threadId"]) != `"fixture-thread"` || string(q.Params["expectedTurnId"]) != `"fixture-turn"` {
				fail(fmt.Errorf("wrong steer binding"))
			}
			if err := os.WriteFile(filepath.Join(root, "steered.json"), scan.Bytes(), 0600); err != nil {
				fail(err)
			}
			if _, err := os.Stat(filepath.Join(root, "steer-fail")); err == nil {
				_ = enc.Encode(map[string]any{"id": q.ID, "error": map[string]string{"message": "fixture delivery rejected"}})
			} else {
				reply(map[string]string{"turnId": "fixture-turn"})
			}
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]string{"type": "agentMessage", "text": `{"fixture":true}`}}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "completed"}}})
		}
	}
	if err := scan.Err(); err != nil {
		fail(err)
	}
	if owned {
		if err := os.Remove(filepath.Join(root, "writer")); err != nil {
			fail(err)
		}
		// Model a competing client acquiring ownership between invocations.
		if _, err := os.Stat(filepath.Join(root, "external-writer")); err == nil {
			if err := os.WriteFile(filepath.Join(root, "writer"), []byte("other client"), 0600); err != nil {
				fail(err)
			}
		}
	}
	// Shutdown output must be drained and, for turns, preserved in the journal.
	_ = enc.Encode(map[string]string{"method": "fixture/released"})
	if _, err := os.Stat(filepath.Join(root, "exit-error")); err == nil {
		os.Exit(7)
	}
	os.Exit(0)
}

func codexLifecycleContext(t *testing.T) (*protocol.HostRuntimeContext, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	launcher := filepath.Join(root, "codex")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestCodexLifecycleEndpoint$ -- \"$@\"\n"
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return &protocol.HostRuntimeContext{Provider: "codex", Executable: launcher, Environment: map[string]string{"HOME": root, "CODEX_HOME": root, "PATH": os.Getenv("PATH"), "FORMAL_GATES_LIFECYCLE_ENDPOINT": root}}, root
}

func TestCodexLifecycleTurnsAndProbeReleaseWriter(t *testing.T) {
	binding, root := codexLifecycleContext(t)
	q := nativeTurn{Claim: protocol.ExecutionClaim{ActionID: "fixture-action", Context: binding}, Settings: settings.Values{Model: "fixture"}, Workspace: root, Prompt: "fixture input", Schema: map[string]any{"type": "object"}, Attached: func(id string) error {
		if id != "fixture-thread" {
			return fmt.Errorf("unexpected identity %q", id)
		}
		return nil
	}}
	for i := 0; i < 3; i++ {
		q.Job = t.TempDir()
		q.Resume, q.NoTools = i > 0, i == 0
		id, err := (codexRuntimeAdapter{}).turn(q)
		if err != nil {
			t.Fatal(err)
		}
		q.Claim.Identity = id
		assertCodexLifecycleStopped(t, q.Job)
		data, err := os.ReadFile(filepath.Join(q.Job, "stdout.jsonl"))
		if err != nil || !strings.Contains(string(data), "fixture/released") {
			t.Fatalf("shutdown output missing: %s %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(root, "writer")); !os.IsNotExist(err) {
			t.Fatalf("writer retained after turn: %v", err)
		}
		raw, err := codexThreadRequest(binding.Executable, "thread/resume", id, binding, root)
		if err != nil || !strings.Contains(string(raw), id) {
			t.Fatalf("probe: %s %v", raw, err)
		}
		if _, err := os.Stat(filepath.Join(root, "writer")); !os.IsNotExist(err) {
			t.Fatalf("probe retained writer: %v", err)
		}
	}
}

func assertCodexLifecycleStopped(t *testing.T, job string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt codexProcess
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Stopped || receipt.CompletedAt == "" || codexProcessAlive(receipt.PID) {
		t.Fatalf("process not reaped: %+v", receipt)
	}
}

func TestCodexLifecycleFailuresPersistStoppedReceipt(t *testing.T) {
	for _, mode := range []string{"reject", "attach", "exit-error"} {
		t.Run(mode, func(t *testing.T) {
			binding, root := codexLifecycleContext(t)
			if mode != "attach" {
				if err := os.WriteFile(filepath.Join(root, mode), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			q := nativeTurn{Claim: protocol.ExecutionClaim{ActionID: "fixture-action", Context: binding}, Settings: settings.Values{Model: "fixture"}, Job: t.TempDir(), Workspace: root, Prompt: "fixture", Attached: func(string) error {
				if mode == "attach" {
					return fmt.Errorf("fixture attach rejected")
				}
				return nil
			}}
			id, err := (codexRuntimeAdapter{}).turn(q)
			if err == nil {
				t.Fatal("failure lost")
			}
			if (mode == "reject" && id != "") || (mode != "reject" && id != "fixture-thread") {
				t.Fatalf("identity=%q error=%v", id, err)
			}
			assertCodexLifecycleStopped(t, q.Job)
			data, readErr := os.ReadFile(filepath.Join(q.Job, "process.json"))
			var receipt struct {
				ExitCode *int `json:"exitCode"`
			}
			if readErr != nil || json.Unmarshal(data, &receipt) != nil || receipt.ExitCode == nil {
				t.Fatalf("exit evidence unavailable: %s %v", data, readErr)
			}
			if mode == "exit-error" && *receipt.ExitCode != 7 {
				t.Fatalf("exit error lost: %+v", receipt)
			}
			if _, err := os.Stat(filepath.Join(root, "writer")); !os.IsNotExist(err) {
				t.Fatalf("failed invocation retained writer: %v", err)
			}
		})
	}
}

func TestCodexCoverageAuthoringStartFailureSettlesBoundExecution(t *testing.T) {
	for _, mode := range []string{"reject-resume", "external-writer"} {
		t.Run(mode, func(t *testing.T) {
			binding, root := codexLifecycleContext(t)
			t.Setenv("FORMAL_GATES_TEST_CODEX_EXECUTABLE", binding.Executable)
			t.Setenv("FORMAL_GATES_LIFECYCLE_ENDPOINT", root)
			t.Setenv("CODEX_HOME", root)
			writePreparationFile(t, root, "answer.json", `{"coverageJudgment":"fixture independent coverage","behaviorOracles":[{"behavior":"fixture output","oracle":"exact bytes"}]}`)
			writePreparationFile(t, root, mode, "fixture authoring startup failure")
			f, x := whiteboxInputFixture(t)
			run, err := f.executeNativeTransport(x.Claim.ActionID, false, savedCoverageTransport{})
			if err != nil {
				t.Fatal(err)
			}
			got := run.Executions[x.Claim.ActionID]
			if !got.Finished || got.Identity != "fixture-thread" || got.Claim.Identity != "" || !protocol.WhiteboxCoverageReady(got) || len(got.Operations) != 1 {
				t.Fatalf("coverage/authoring handoff not retained: %+v", got)
			}
			want := "fixture startup rejected"
			if mode == "external-writer" {
				want = "already has an active writer"
			}
			if got.Source == nil || got.Source.Identity != got.Identity || got.Source.Status != "UNAVAILABLE" || got.PreparationResult == nil || got.PreparationResult.Identity != got.Identity || got.PreparationResult.Status != "RUNTIME_ERROR" || !strings.Contains(got.PreparationResult.Message, want) {
				t.Fatalf("original authoring startup error not settled: %+v", got)
			}
			job := f.codexJob(x.Claim.ActionID)
			assertCodexLifecycleStopped(t, filepath.Join(job, "coverage"))
			assertCodexLifecycleStopped(t, job)
			if mode == "external-writer" {
				data, err := os.ReadFile(filepath.Join(root, "writer"))
				if err != nil || string(data) != "other client" {
					t.Fatalf("competing writer was removed: %s %v", data, err)
				}
			}
		})
	}
}

func TestFailedNativeStartUsesAcceptedIdentity(t *testing.T) {
	for _, kind := range []string{"review", "preparation", "verification", "unattached"} {
		t.Run(kind, func(t *testing.T) {
			var f *Facade
			var x protocol.RegularExecution
			switch kind {
			case "preparation":
				f, x = whiteboxInputFixture(t)
			case "verification":
				f = frozenVerificationFixture(t, false)
				x = facadeExecution(t, f, "blackbox-execution")
			default:
				f = readyRegularFacade(t)
				x = facadeExecution(t, f, "product-review")
			}
			identity := ""
			if kind == "unattached" {
				preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
			} else {
				identity = nativeFixtureClaim(t, f, x).Identity
			}
			if x.Claim.Identity != "" {
				t.Fatal("fixture must retain the original empty claim identity")
			}
			if kind != "unattached" {
				bad := x.Claim
				bad.Identity = "wrong-thread"
				if _, err := f.failedNativeStart(bad.ActionID, bad, fmt.Errorf("fixture failure")); err == nil || !strings.Contains(err.Error(), "INTENT_MISMATCH") {
					t.Fatalf("identity check lost: %v", err)
				}
				bad = x.Claim
				bad.AttemptID += "-stale"
				if _, err := f.failedNativeStart(bad.ActionID, bad, fmt.Errorf("fixture failure")); err == nil || !strings.Contains(err.Error(), "INTENT_MISMATCH") {
					t.Fatalf("attempt check lost: %v", err)
				}
			}
			run, err := f.failedNativeStart(x.Claim.ActionID, x.Claim, fmt.Errorf("fixture startup rejected"))
			if err != nil {
				t.Fatal(err)
			}
			got := run.Executions[x.Claim.ActionID]
			if !got.Finished || got.Identity != identity || got.Source == nil || got.Source.Identity != identity || got.Source.Status != "UNAVAILABLE" || !strings.Contains(got.Source.Reason, "fixture startup rejected") {
				t.Fatalf("failure not settled: %+v", got)
			}
			var status, message, resultIdentity string
			switch kind {
			case "preparation":
				status, message, resultIdentity = got.PreparationResult.Status, got.PreparationResult.Message, got.PreparationResult.Identity
			case "verification":
				status, message, resultIdentity = got.VerificationResult.Status, got.VerificationResult.Message, got.VerificationResult.Identity
			default:
				rows := run.Regular.RejectedResults
				if len(rows) != 1 {
					t.Fatalf("missing runtime rejection: %+v", rows)
				}
				last := rows[0].Raw
				status, message, resultIdentity = last.Status, last.Message, last.Identity
			}
			if status != "RUNTIME_ERROR" || resultIdentity != identity || !strings.Contains(message, "fixture startup rejected") {
				t.Fatalf("original failure lost: %s %s %s", status, resultIdentity, message)
			}
		})
	}
}

func TestCodexActiveMessageUsesOwningConnection(t *testing.T) {
	for _, failDelivery := range []bool{false, true} {
		t.Run(fmt.Sprint(failDelivery), func(t *testing.T) {
			binding, root := codexLifecycleContext(t)
			if err := os.WriteFile(filepath.Join(root, "steer"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if failDelivery {
				if err := os.WriteFile(filepath.Join(root, "steer-fail"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pending := true
			var receipts []protocol.TaskMessage
			q := nativeTurn{Claim: protocol.ExecutionClaim{ActionID: "fixture-action", Context: binding}, Settings: settings.Values{Model: "fixture"}, Workspace: root, Job: t.TempDir(), Prompt: "initial text", Schema: map[string]any{"type": "object"}, Attached: func(string) error { return nil }, PendingMessage: func(identity string) (*protocol.TaskMessage, error) {
				if !pending {
					return nil, nil
				}
				pending = false
				return &protocol.TaskMessage{ID: "note", Text: "Observe the amber output.", Identity: identity, Status: "SENDING"}, nil
			}, MessageReceipt: func(m protocol.TaskMessage) error { receipts = append(receipts, m); return nil }}
			id, err := (codexRuntimeAdapter{}).turn(q)
			if err != nil || id != "fixture-thread" {
				t.Fatalf("%s %v", id, err)
			}
			want := "ACCEPTED"
			if failDelivery {
				want = "FAILED"
			}
			if len(receipts) != 1 || receipts[0].Status != want {
				t.Fatalf("receipts: %+v", receipts)
			}
			raw, err := os.ReadFile(filepath.Join(root, "steered.json"))
			if err != nil || !strings.Contains(string(raw), "Observe the amber output.") {
				t.Fatalf("actual steer: %s %v", raw, err)
			}
			if _, err := os.Stat(filepath.Join(root, "writer")); !os.IsNotExist(err) {
				t.Fatal("writer not released")
			}
		})
	}
}
