package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func developmentCheckFixture(t *testing.T, checks ...protocol.DevelopmentCheck) (*Facade, *protocol.State, string) {
	t.Helper()
	root := t.TempDir()
	snapshotRepoCommits(t, root)
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "app.txt"}, {"commit", "-qm", "Check source"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("check source %v %s", err, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(out))
	d := nativeMasterAt(t, root, base, nil)
	binary := filepath.Join(d.f.admission.Target, "bin", "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build check runtime %v %s", err, out)
	}
	plan := commandPlan()
	if len(checks) > 0 {
		plan.Batches[0].Checks = checks
	}
	d.reviewPass(plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One approved check contract"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	action, input := planBatchAction(d, "command")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main // delivered input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	if _, err := d.f.Drive(); err != nil {
		t.Fatal(err)
	}
	state := driverState(t, d)
	t.Cleanup(func() {
		checks := filepath.Join(d.f.stateDir, "checks")
		entries, err := os.ReadDir(checks)
		if err != nil {
			t.Error(err)
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			directory := filepath.Join(checks, entry.Name())
			if _, err := os.Stat(filepath.Join(directory, "request.json")); err != nil {
				continue
			}
			var request map[string]any
			if err := readCheckDocument(filepath.Join(directory, "request.json"), &request); err != nil {
				t.Error(err)
				continue
			}
			digest, err := d.f.checkRequestDigest(request)
			if err != nil {
				t.Error(err)
				continue
			}
			for {
				var receipt struct {
					RequestDigest string `json:"request_digest"`
					ExitCode      *int   `json:"exit_code"`
				}
				if err := readCheckDocument(filepath.Join(directory, "exit.json"), &receipt); err == nil {
					if receipt.RequestDigest != digest || receipt.ExitCode == nil {
						t.Error("original check exit does not match its request")
					}
					break
				}
				var process struct {
					Alive bool `json:"alive"`
				}
				unlock, err := d.f.acquireWriteWindow()
				if err != nil {
					t.Error(err)
					break
				}
				err = d.f.checkBridge("supervisor-process", map[string]any{"directory": directory, "requestDigest": digest, "cancel": false}, &process)
				unlock()
				if err != nil {
					t.Error(err)
					break
				}
				if !process.Alive {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
	return d.f, state, base
}

func developmentCheckIntent(t *testing.T, f *Facade, s *protocol.State, base, action, command string) protocol.HostActionIntent {
	t.Helper()
	intents := []protocol.HostActionIntent{}
	for _, intent := range s.PendingHostActions {
		intents = append(intents, intent)
	}
	for _, effect := range s.ReconciledEffects {
		if effect.Intent != nil {
			intents = append(intents, *effect.Intent)
		}
	}
	for _, intent := range intents {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
			continue
		}
		var parameters protocol.CheckParameters
		if err := decodeNativeParams(intent, &parameters); err != nil {
			t.Fatal(err)
		}
		if parameters.Check.ID == action && parameters.Check.Command == command {
			return intent
		}
	}
	t.Fatalf("check %s is not in the original approved contract", action)
	return protocol.HostActionIntent{}
}

func observeDevelopmentCheck(t *testing.T, f *Facade, s *protocol.State, intent protocol.HostActionIntent, execute bool) protocol.CheckEvidence {
	t.Helper()
	invoked := false
	observe := func(mayWrite bool) (nativeResult, bool, error) {
		unlock, err := f.acquireWriteWindow()
		if err != nil {
			return nativeResult{}, false, err
		}
		defer unlock()
		return f.checkEvidence(s, intent, mayWrite, &invoked)
	}
	result, done, err := observe(execute)
	if err != nil {
		t.Fatal(err)
	}

	for !done {
		time.Sleep(10 * time.Millisecond)
		result, done, err = observe(false)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !done || result.Check == nil {
		t.Fatal("check did not produce original execution evidence")
	}
	return *result.Check
}

func TestDevelopmentCheckExactCommandAndObservedReceipt(t *testing.T) {
	command := "printf '%s' 'a b' | cat > generated; printf x >> executions; printf '%s' 'a b'"
	f, s, base := developmentCheckFixture(t, protocol.DevelopmentCheck{ID: "quoted", Purpose: protocol.CheckFullBuild, Command: command}, protocol.DevelopmentCheck{ID: "dependent", Purpose: protocol.CheckFullTests, Command: "test \"$(cat generated)\" = 'a b'; printf 'dependent'"})
	intent := developmentCheckIntent(t, f, s, base, "quoted", command)
	first := observeDevelopmentCheck(t, f, s, intent, true)
	if first.ExitCode != 0 || first.Error != "" {
		t.Fatalf("actual first check: %#v", first)
	}
	body, err := os.ReadFile(filepath.Join(first.Directory, "stdout.log"))
	if err != nil || string(body) != "a b" {
		t.Fatalf("exact stdout %q %v", body, err)
	}
	var request struct {
		Argv []string `json:"argv"`
		Cwd  string   `json:"cwd"`
	}
	if err := readCheckDocument(filepath.Join(first.Directory, "request.json"), &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Argv) != 3 || request.Argv[2] != command || request.Cwd != first.WorkingDirectory {
		t.Fatalf("actual command transport %#v", request)
	}
	toolBytes, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	tool := fmt.Sprintf("/bin/sh sha256:%x", sha256.Sum256(toolBytes))
	var receipt struct {
		Tool        string `json:"tool_identity"`
		Environment string `json:"environment_identity"`
	}
	if err := readCheckDocument(filepath.Join(first.Directory, "exit.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	invocation, err := json.Marshal(request.Argv)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := hex.DecodeString(first.Environment)
	if err != nil || len(environment) != sha256.Size {
		t.Fatal("receipt lacks actual environment digest")
	}
	if first.Tool != tool || first.Tool != receipt.Tool || first.Environment != receipt.Environment || first.Invocation != string(invocation) || first.WorkingDirectory != request.Cwd {
		t.Fatalf("actual process identities differ from bound request/receipt: %+v", first)
	}
	// A reopened caller observes the same owned receipt, without repeating effects.
	reopened := *f
	second := observeDevelopmentCheck(t, &reopened, s, intent, true)
	if second != first {
		t.Fatalf("recovered execution differs: %#v %#v", first, second)
	}
	count, err := os.ReadFile(filepath.Join(first.WorkingDirectory, "executions"))
	if err != nil || string(count) != "x" {
		t.Fatalf("check repeated effect %q %v", count, err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	s = snap.State
	dependent := developmentCheckIntent(t, f, s, base, "dependent", "test \"$(cat generated)\" = 'a b'; printf 'dependent'")
	if result := observeDevelopmentCheck(t, f, s, dependent, true); result.ExitCode != 0 {
		t.Fatalf("same batch generator dependency lost %#v", result)
	}
	// Changed command requests and changed raw streams refuse evidence admission.
	requestPath := filepath.Join(first.Directory, "request.json")
	original, _ := os.ReadFile(requestPath)
	var altered map[string]any
	if err := json.Unmarshal(original, &altered); err != nil {
		t.Fatal(err)
	}
	altered["argv"] = []string{"/bin/sh", "-c", "true"}
	changed, _ := json.Marshal(altered)
	if err := os.WriteFile(requestPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	invoked := false
	if _, _, err := f.checkEvidence(s, intent, false, &invoked); err == nil {
		t.Fatal("changed approved command request admitted")
	}
	if err := os.WriteFile(requestPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Directory, "stdout.log"), []byte("altered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.checkEvidence(s, intent, false, &invoked); err == nil {
		t.Fatal("changed raw stdout admitted")
	}
}

func TestDevelopmentCheckActualFailureAndUnknownOwnership(t *testing.T) {
	f, s, base := developmentCheckFixture(t, protocol.DevelopmentCheck{ID: "exit-comparison", Purpose: protocol.CheckFullBuild, Command: "sh -c 'exit 2'; test $? -eq 1"}, protocol.DevelopmentCheck{ID: "tests", Purpose: protocol.CheckFullTests, Command: "true"})
	intent := developmentCheckIntent(t, f, s, base, "exit-comparison", "sh -c 'exit 2'; test $? -eq 1")
	result := observeDevelopmentCheck(t, f, s, intent, true)
	if result.ExitCode != 1 || result.Error != "" {
		t.Fatalf("approved comparison was not faithfully failed %#v", result)
	}
	if err := os.Remove(filepath.Join(result.Directory, "exit.json")); err != nil {
		t.Fatal(err)
	}
	invoked := false
	if _, done, err := f.checkEvidence(s, intent, true, &invoked); err == nil || done || invoked {
		t.Fatalf("unknown owned effect was rerun: done %v invoked %v err %v", done, invoked, err)
	}
}

func TestDevelopmentCheckAbortSettlesProcess(t *testing.T) {
	f, s, base := developmentCheckFixture(t, protocol.DevelopmentCheck{ID: "cancel", Purpose: protocol.CheckFullBuild, Command: "printf 'started'; sleep 10"}, protocol.DevelopmentCheck{ID: "tests", Purpose: protocol.CheckFullTests, Command: "true"})
	intent := developmentCheckIntent(t, f, s, base, "cancel", "printf 'started'; sleep 10")
	invoked := false
	unlock, err := f.acquireWriteWindow()
	if err != nil {
		t.Fatal(err)
	}
	_, done, err := f.checkEvidence(s, intent, true, &invoked)
	unlock()
	if err != nil || done {
		t.Fatalf("start check: %v %v", done, err)
	}
	directory := filepath.Join(f.stateDir, "checks", strings.ReplaceAll(intent.ActionID, ":", "-"))
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(directory, "process.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no persisted process")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Decisions = map[string]protocol.RecordedDecision{"abort": {Control: protocol.ControlAbort, Choice: "abort", Revision: 1}}
	result := observeDevelopmentCheck(t, f, s, intent, false)
	if result.ExitCode != 130 || result.Error != "owned job was cancelled" {
		t.Fatalf("abort actual receipt %#v", result)
	}
}

func TestDevelopmentCheckPublicWorkflowReceiptsOwnCompletion(t *testing.T) {
	for _, successful := range []bool{true, false} {
		t.Run(map[bool]string{true: "pass", false: "fail"}[successful], func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			plan := commandPlan()
			command := "printf '%s' 'a b' | cat; sh -c 'exit 2'; test $? -eq 2"
			if !successful {
				command = "printf '%s' 'a b' | cat; sh -c 'exit 2'; test $? -eq 1"
			}
			plan.Batches[0].Checks[0].Command = command
			consumer := plan.Batches[0]
			consumer.ID = "consumer"
			consumer.DependsOn = []string{plan.Batches[0].ID}
			consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consume", Description: "Consume verified command"}}
			consumer.Artifacts = []string{"consumer.go"}
			plan.Batches = append(plan.Batches, consumer)
			d := nativeMasterAt(t, root, base, nil)
			binary := filepath.Join(d.f.admission.Target, "bin", "formal-gates")
			build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
			build.Dir = "../../.."
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build runtime %v %s", err, out)
			}
			d.reviewPass(plan)
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command with a verified dependent"}})
			d.confirmRoute(routeDecisionForGates([]string{"quality"}))
			d.drive()
			action, input := planBatchAction(d, "command")
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main // implemented input\n"), 0600); err != nil {
				t.Fatal(err)
			}
			d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
			sourceState := driverState(t, d)
			if sourceState.Results[action].Outcome != protocol.OutcomePass {
				t.Fatal("original editor result changed")
			}
			var receipt protocol.HostActionReceipt
			var executed protocol.CheckEvidence
			for {
				d.drive()
				state := driverState(t, d)
				for _, observed := range state.HostActionReceipts {
					if observed.AdapterOperation != "op.check" || observed.Status != protocol.HostActionStatusExecuted {
						continue
					}
					intent := state.ReconciledEffects[observed.ActionID].Intent
					if intent == nil {
						t.Fatal("executed check lacks its original signed intent")
					}
					var parameters protocol.CheckParameters
					if err := decodeNativeParams(*intent, &parameters); err != nil {
						t.Fatal(err)
					}
					if parameters.SourceActionID != action || parameters.Check.ID != plan.Batches[0].Checks[0].ID || parameters.Check.Command != command {
						continue
					}
					receipt = observed
					if err := decodeReceiptEvidence(observed, &executed); err != nil {
						t.Fatal(err)
					}
					break
				}
				if receipt.ActionID != "" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if receipt.ActionID == "" {
				t.Fatalf("public workflow produced no check receipt: %#v", d.run.Next)
			}
			expectedExit := 0
			if !successful {
				expectedExit = 1
			}
			if executed.ExitCode != expectedExit || executed.Error != "" || executed.SourceActionID != action || executed.ParametersDigest != receipt.PayloadDigest {
				t.Fatalf("admitted actual execution %#v receipt %#v", executed, receipt)
			}
			stdout, err := os.ReadFile(filepath.Join(executed.Directory, "stdout.log"))
			if err != nil || string(stdout) != "a b" {
				t.Fatalf("actual stdout %q %v", stdout, err)
			}
			state := driverState(t, d)
			if state.Results[action].Outcome != protocol.OutcomePass {
				t.Fatal("program changed the original worker transport result")
			}
			consumerIssued := false
			for _, issued := range state.DispatchBindings {
				if issued.BatchID == "consumer" {
					consumerIssued = true
				}
			}
			if consumerIssued != successful {
				t.Fatalf("actual check did not own dependency: successful=%v issued=%v next=%#v", successful, consumerIssued, d.run.Next)
			}
			// Reopen through the public loader and retain the exact admitted execution.
			reopened, err := Open(root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			run, err := reopened.Drive()
			if err != nil {
				t.Fatal(err)
			}
			_ = run
			snap, err := reopened.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.HostActionReceipts[receipt.ActionID].Digest != receipt.Digest || snap.State.Results[action].Outcome != protocol.OutcomePass {
				t.Fatal("reopen changed execution or raw editor result")
			}
			retainNativeEvidence(t, reopened)
		})
	}
}

func TestDevelopmentCheckSourceChangeCannotProveInput(t *testing.T) {
	f, s, base := developmentCheckFixture(t, protocol.DevelopmentCheck{ID: "source-change", Purpose: protocol.CheckFullBuild, Command: "printf changed > app.txt; printf 'exit zero'"}, protocol.DevelopmentCheck{ID: "tests", Purpose: protocol.CheckFullTests, Command: "true"})
	intent := developmentCheckIntent(t, f, s, base, "source-change", "printf changed > app.txt; printf 'exit zero'")
	result := observeDevelopmentCheck(t, f, s, intent, true)
	if result.ExitCode != 0 || result.Error != "check changed an immutable source input: app.txt" {
		t.Fatalf("changed source falsely verified %#v", result)
	}
}
