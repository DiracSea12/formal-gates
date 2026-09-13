package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func TestDeveloperRecoveryRejectsHostReasonBeforeNativeCall(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	recovery := protocol.DeveloperRecovery{Claim: x.Claim, Identity: "host-reported-unrecoverable", NativeMethod: "thread/resume", NativeResponse: json.RawMessage(`{"id":3,"error":{"code":-32600,"message":"no rollout found for thread id host-reported-unrecoverable"}}`)}
	if _, _, err := f.submitAdapterRegular("forged-recovery", protocol.RegularEvent{Operation: "DEVELOPER_RECOVERY", DeveloperRecovery: &recovery}); err == nil {
		t.Fatal("host reason authorized replacement")
	}
	if _, err := os.Stat(filepath.Join(f.codexJob(x.Claim.ActionID), "process.json")); !os.IsNotExist(err) {
		t.Fatal("native process was created")
	}
}

// The predevelopment approvals are the explicit deterministic fixture above.
// Every development call, identity, process and recovery response below is native.
func TestDeveloperNativeContinuation(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in: actual native continuation, interruption and missing-thread replacement")
	}
	for _, mode := range []string{"normal", "self-test", "interrupted", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			task := "Implement the hello Go command. This is a deliberately two-turn preparation exercise. If go.mod does not exist, create only go.mod with module example.invalid/hello and go 1.23, then return CONTINUE immediately with manifest [go.mod] and concrete remaining work. Do not create main.go on that first turn. If go.mod already exists, implement main.go printing exactly hello with no newline, run go run . and go test ./..., and return PASS with the complete manifest including go.mod and main.go and S1 validation."
			if mode == "self-test" {
				task = "Implement the hello Go command in two turns. If go.mod does not exist, create only go.mod (module example.invalid/hello; go 1.23), actually execute go test ./... which will fail because no Go package exists yet, then return FAIL immediately with manifest [go.mod], the exact observed validation failure and remaining main.go work. Do not fix it during that first turn. If go.mod already exists, finish main.go printing exactly hello with no newline, run go run . and go test ./..., and return PASS with manifest go.mod and main.go and S1 validation."
			}
			if mode == "interrupted" {
				task = "Implement main.go and go.mod for the exact hello command. On binding.operation spawn, first execute sleep 20 before doing any work. On binding.operation resume proceed immediately to implement and validate go run . and go test ./..., then return PASS with manifest go.mod and main.go and S1 validation."
			}
			f := facadePreparationTask(t, nil, 1, task)
			t.Cleanup(func() {
				if dir := os.Getenv("FORMAL_GATES_CONTINUITY_EVIDENCE"); dir != "" {
					r, err := f.Show()
					if err != nil {
						t.Error(err)
						return
					}
					if err = os.MkdirAll(dir, 0700); err != nil {
						t.Error(err)
						return
					}
					if err = writeJSONAtomic(filepath.Join(dir, mode+".json"), r); err != nil {
						t.Error(err)
					}
				}
			})
			x := facadeExecution(t, f, "development-worker")
			var first Run
			var err error
			if mode == "interrupted" {
				done := make(chan error, 1)
				go func() { _, e := f.ExecuteNative(x.Claim.ActionID, false); done <- e }()
				deadline := time.Now().Add(time.Minute)
				stopped := false
				for time.Now().Before(deadline) {
					r, e := f.Show()
					if e != nil {
						t.Fatal(e)
					}
					if r.Executions[x.Claim.ActionID].Identity != "" {
						data, e := os.ReadFile(filepath.Join(f.codexJob(x.Claim.ActionID), "process.json"))
						if e != nil {
							t.Fatal(e)
						}
						var p codexProcess
						if json.Unmarshal(data, &p) != nil {
							t.Fatal("invalid process receipt")
						}
						if e = stopCodexProcess(p.PID); e != nil {
							t.Fatal(e)
						}
						stopped = true
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if !stopped {
					t.Fatal("native identity was not observed before interruption")
				}
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				first, err = f.Show()
			} else {
				first, err = f.ExecuteNative(x.Claim.ActionID, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			original := first.Executions[x.Claim.ActionID]
			expected := "CONTINUE"
			if mode == "self-test" {
				expected = "FAIL"
			}
			if mode == "interrupted" {
				expected = "RUNTIME_ERROR"
			}
			if original.Identity == "" || original.PreparationResult == nil || original.PreparationResult.Status != expected || len(first.Regular.Preparation.Batches) != 0 {
				t.Fatalf("first native result is not unfinished %s: %+v", expected, original)
			}
			next := facadeExecution(t, f, "development-worker")
			if next.Identity != original.Identity || next.Claim.Operation != "resume" {
				t.Fatal("new attempt did not retain native identity")
			}
			if _, err = f.ExecuteNative(next.Claim.ActionID, false); err == nil {
				t.Fatal("direct fresh spawn accepted")
			}
			if _, e := os.Stat(filepath.Join(f.codexJob(next.Claim.ActionID), "process.json")); !os.IsNotExist(e) {
				t.Fatal("rejected fresh spawn created a native process")
			}
			if mode == "replacement" {
				// This identity was created by this test invocation. Deleting only that
				// disposable test thread supplies a real, reproducible irrecoverable case.
				raw, e := codexThreadRequest(first.Regular.Settings.Values.NativeString("codexExecutable"), "thread/delete", original.Identity, original.Claim.Context, first.Regular.Preparation.Resources["development"])
				if e != nil {
					t.Fatal(e)
				}
				var response struct {
					Error  any             `json:"error"`
					Result json.RawMessage `json:"result"`
				}
				if json.Unmarshal(raw, &response) != nil || response.Error != nil || response.Result == nil {
					t.Fatalf("native test-session deletion failed: %s", raw)
				}
				if dir := os.Getenv("FORMAL_GATES_CONTINUITY_EVIDENCE"); dir != "" {
					if e = os.WriteFile(filepath.Join(dir, "replacement-delete.json"), raw, 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			r, err := f.ExecuteNative(next.Claim.ActionID, true)
			if err != nil {
				t.Fatal(err)
			}
			continued := r.Executions[next.Claim.ActionID]
			if continued.PreparationResult == nil || continued.PreparationResult.Status != "PASS" || len(r.Regular.Preparation.Batches) != 1 {
				t.Fatalf("native continuation did not complete: %+v errors=%+v", continued, r.Regular.PreparationErrors)
			}
			if mode == "replacement" {
				if continued.Identity == original.Identity || !continued.ReplacementClaimed || continued.Recovery == nil || continued.Recovery.Outcome() != "UNRECOVERABLE" {
					t.Fatal("replacement lacks bound actual native failure")
				}
			} else if continued.Identity != original.Identity || continued.ReplacementClaimed || continued.Recovery == nil || continued.Recovery.Outcome() != "RESUMABLE" {
				t.Fatal("continuation changed native identity")
			}
			if _, err = f.ExecuteNative(next.Claim.ActionID, true); err == nil {
				t.Fatal("completed continuation replayed")
			}
			if _, err = f.Drive(); err != nil {
				t.Fatal(err)
			}
			if r, err = f.Drive(); err != nil || r.Regular.Preparation.Candidate == nil {
				t.Fatalf("candidate after continuation: %v %+v", err, r.Regular.Preparation)
			}
			t.Logf("native %s first=%s next=%s identity=%s -> %s", mode, x.Claim.ActionID, next.Claim.ActionID, original.Identity, continued.Identity)
		})
	}
}

func TestDeveloperResumeResponseMatchesActualRequestID(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("local shell protocol fixture unavailable")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "native-protocol")
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"method":"initialize"'*) printf '%s\n' '{"id":1,"result":{}}' ;;
*'"method":"config/read"'*) printf '%s\n' '{"id":2,"result":{"config":{}}}' ;;
*'"method":"thread/resume"'*) printf '%s\n' '{"id":3,"result":{"thread":{"id":"fixture-native"}}}' ;;
esac
done
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	binding, err := captureRuntimeContext("codex", settings.Values{Native: map[string]string{"codexExecutable": executable}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codexThreadRequest(executable, "thread/resume", "fixture-native", binding, root)
	if err != nil {
		t.Fatal(err)
	}
	receipt := protocol.DeveloperRecovery{Claim: protocol.ExecutionClaim{Provider: "codex"}, Identity: "fixture-native", NativeMethod: "thread/resume", NativeResponse: raw}
	if receipt.Outcome() != "RESUMABLE" {
		t.Fatalf("successful native resume classified %s: %s", receipt.Outcome(), raw)
	}
}

func TestDeveloperReplacementRecoveryDoesNotRequireUncreatedProcess(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("local shell protocol fixture unavailable")
	}
	tools := t.TempDir()
	// The probe returns the native missing-rollout shape. Fresh model startup
	// deliberately fails initialization, letting this test prove replacement
	// admission and process creation without running a model or inventing a PASS.
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"version":"5"'*) printf '%s\n' '{"id":1,"error":{"code":-32600,"message":"fixture stops before model turn"}}' ;;
*'"method":"initialize"'*) printf '%s\n' '{"id":1,"result":{}}' ;;
*'"method":"config/read"'*) printf '%s\n' '{"id":2,"result":{"config":{}}}' ;;
*'"method":"thread/resume"'*) printf '%s\n' '{"id":3,"error":{"code":-32600,"message":"no rollout found for thread id original-native"}}' ;;
esac
done
`
	if err := os.WriteFile(filepath.Join(tools, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FORMAL_GATES_TEST_CODEX_EXECUTABLE", filepath.Join(tools, "codex"))
	t.Setenv("HOME", t.TempDir())
	f := facadePreparation(t, nil)
	first := facadeExecution(t, f, "development-worker")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &first.Claim})
	source := protocol.ExecutionSource{ActionID: first.Claim.ActionID, AttemptID: first.Claim.AttemptID, Provider: "codex", Identity: "original-native", Status: "RUNNING"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
	result := protocol.PreparationResult{ActionID: first.Claim.ActionID, AttemptID: first.Claim.AttemptID, Provider: "codex", Identity: source.Identity, RequirementRevision: first.Claim.RequirementRevision, Candidate: first.Claim.Candidate, Status: "CONTINUE", Message: "implementation remains unfinished"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	source.Status = "UNAVAILABLE"
	source.Reason = "protocol fixture without model usage"
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
	next := facadeExecution(t, f, "development-worker")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &next.Claim})
	if err := os.MkdirAll(f.codexJob(first.Claim.ActionID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(f.codexJob(first.Claim.ActionID), "process.json"), codexProcess{ActionID: first.Claim.ActionID, PID: os.Getpid(), Stopped: true}); err != nil {
		t.Fatal(err)
	}
	// The current action has captured its inputs before native process startup.
	if err := os.MkdirAll(f.codexJob(next.Claim.ActionID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(f.codexJob(next.Claim.ActionID), "sources.json"), []SubagentSource{}); err != nil {
		t.Fatal(err)
	}

	run, _ := f.Show()
	recovery, err := f.observeNativeDeveloperRecovery(run, next, false, codexRuntimeAdapter{})
	if err != nil || recovery.Outcome() != "UNRECOVERABLE" {
		t.Fatalf("%+v %v", recovery, err)
	}
	if _, err = os.Stat(filepath.Join(f.codexJob(next.Claim.ActionID), "process.json")); !os.IsNotExist(err) {
		t.Fatal("probe unexpectedly created a model process")
	}
	_, err = f.ExecuteNative(next.Claim.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	run, _ = f.Show()
	if !run.Executions[next.Claim.ActionID].ReplacementClaimed {
		t.Fatal("stored irrecoverability failed to authorize replacement")
	}
	if _, err = os.Stat(filepath.Join(f.codexJob(next.Claim.ActionID), "process.json")); err != nil {
		t.Fatal("replacement did not reach native process startup", err)
	}
}
