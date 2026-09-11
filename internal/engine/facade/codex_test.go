package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in live acceptance invokes the installed Codex executable through the same
// claim → native exec → thread observation → result → source path as the CLI.
// It is deliberately not an ordinary unit-test dependency or a fake provider.
func TestRegularCodexNative(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("set FORMAL_GATES_REAL_CODEX=1 for the bounded native review")
	}
	f := readyRegularFacade(t)
	run, _ := f.Show()
	action := ""
	for id := range run.Executions {
		action = id
	}
	result, err := f.ExecuteNative(action, false)
	if err != nil {
		t.Fatal(err)
	}
	execution := result.Executions[action]
	if execution.Identity == "" || !execution.Finished || execution.Source == nil || execution.Source.Status != "STOPPED" {
		t.Fatalf("native execution missing completion/source: %+v", execution)
	}
	if result.Regular.Reviews["product-review"].Completed != 1 || len(result.Cost.Dispatches) != 1 {
		t.Fatalf("native result not admitted: %+v", result)
	}
	if _, err = f.ExecuteNative(action, false); err == nil || !strings.Contains(err.Error(), "DUPLICATE_ACTION") {
		t.Fatalf("duplicate native call=%v", err)
	}
	if evidence := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); evidence != "" {
		data, _ := json.MarshalIndent(result, "", "  ")
		if err = os.MkdirAll(filepath.Dir(evidence), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(evidence, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("native action=%s identity=%s transcript=%s input=%d output=%d", action, execution.Identity, execution.Source.TranscriptPath, result.Cost.TotalInputTokens, result.Cost.OutputTokens)
}

func TestRegularCodexNativeChild(t *testing.T) {
	if os.Getenv("FORMAL_GATES_NATIVE_CHILD") != "1" {
		t.Skip("native subprocess helper")
	}
	f, err := Open(os.Getenv("FORMAL_GATES_NATIVE_ROOT"), "regular")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.ExecuteNative(os.Getenv("FORMAL_GATES_NATIVE_ACTION"), false)
	if err != nil {
		t.Fatal(err)
	}
}
func TestRegularCodexNativeInterruptedResume(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in real Codex interruption")
	}
	f := readyRegularFacade(t)
	run, _ := f.Show()
	id := ""
	for action := range run.Executions {
		id = action
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestRegularCodexNativeChild$", "-test.v")
	child.Env = append(os.Environ(), "FORMAL_GATES_NATIVE_CHILD=1", "FORMAL_GATES_NATIVE_ROOT="+f.root, "FORMAL_GATES_NATIVE_ACTION="+id)
	log, err := os.Create(filepath.Join(t.TempDir(), "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	child.Stdout = log
	child.Stderr = log
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	deadline := time.Now().Add(45 * time.Second)
	identity := ""
	var receipt codexProcess
	for time.Now().Before(deadline) {
		current, readErr := f.Show()
		if readErr == nil && current.Executions[id].Identity != "" {
			identity = current.Executions[id].Identity
			data, err := os.ReadFile(filepath.Join(f.codexJob(id), "process.json"))
			journal, _ := os.ReadFile(filepath.Join(f.codexJob(id), "stdout.jsonl"))
			// A created thread can precede its first durable provider turn. This
			// recovery case interrupts an actually started turn, while the separate
			// before-call case covers the unobserved/UNKNOWN interval.
			if err == nil && json.Unmarshal(data, &receipt) == nil && strings.Contains(string(journal), `"method":"turn/started"`) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if identity == "" || receipt.PID == 0 {
		t.Fatal("native startup did not expose bound identity and process receipt")
	}
	// End the orchestration process first: native completion cannot be turned into
	// a synthetic result by the original adapter after the injected interruption.
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if err = stopCodexProcess(receipt.PID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for codexProcessAlive(receipt.PID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if codexProcessAlive(receipt.PID) {
		t.Fatal("native process group did not stop")
	}
	reopened, err := Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := reopened.ExecuteNative(id, true)
	if err != nil {
		t.Fatal(err)
	}
	x := resumed.Executions[id]
	if x.Identity != identity || !x.ResumeClaimed || !x.Finished || x.Source == nil || x.Source.Status != "STOPPED" || resumed.Regular.Reviews["product-review"].Completed != 1 || len(resumed.Executions) != 1 {
		t.Fatalf("resume did not preserve one task/native identity: %+v", resumed)
	}
	if _, err = reopened.ExecuteNative(id, true); err == nil || !strings.Contains(err.Error(), "DUPLICATE_ACTION") {
		t.Fatalf("duplicate resume=%v", err)
	}
	if evidence := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); evidence != "" {
		data, _ := json.MarshalIndent(resumed, "", "  ")
		if err = os.WriteFile(strings.TrimSuffix(evidence, ".json")+"-resume.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real interrupted action=%s same-native-identity=%s input=%d output=%d", id, identity, resumed.Cost.TotalInputTokens, resumed.Cost.OutputTokens)
}

func TestRegularCodexNativeInterruptedBeforeCall(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in actual host process interruption")
	}
	f := readyRegularFacade(t)
	r, _ := f.Show()
	id := ""
	for action := range r.Executions {
		id = action
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestRegularCodexNativeChild$", "-test.v")
	child.Env = append(os.Environ(), "FORMAL_GATES_NATIVE_CHILD=1", "FORMAL_GATES_NATIVE_ROOT="+f.root, "FORMAL_GATES_NATIVE_ACTION="+id)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	// Observe the real atomic state replacement directly, without waiting for the
	// writer lock. This catches the durable claim before the adapter gets its
	// response and creates the execution directory (which precedes exec.Start).
	seen := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
		var doc struct {
			Content struct {
				Executions map[string]protocol.RegularExecution `json:"regularExecutions"`
			} `json:"content"`
		}
		if json.Unmarshal(data, &doc) == nil && doc.Content.Executions[id].Claimed {
			seen = true
			_ = child.Process.Kill()
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
	_ = child.Wait()
	if !seen {
		t.Fatal("did not observe durable claim")
	}
	if _, err = os.Stat(f.codexJob(id)); !os.IsNotExist(err) {
		// Keep the opt-in interruption bounded even if a slow observer missed this
		// narrow pre-call window; this is a failed experiment, never zero-call proof.
		data, _ := os.ReadFile(filepath.Join(f.codexJob(id), "process.json"))
		var p codexProcess
		if json.Unmarshal(data, &p) == nil && p.PID > 0 {
			_ = stopCodexProcess(p.PID)
		}
		t.Fatal("missed pre-call window: execution directory already exists")
	}
	f, err = Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ExecuteNative(id, true); err == nil || !strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatalf("unknown claim resumed: %v", err)
	}
	if _, err = os.Stat(f.codexJob(id)); !os.IsNotExist(err) {
		t.Fatal("UNKNOWN recovery made a native call")
	}
	// The independent observation above proves the missing receipt is a pre-call
	// interruption. Record that finite unavailable source after user abort, then
	// settle the original attempt and let ordinary terminal cleanup finish.
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "End pre-call interruption validation"})
	r, _ = f.Show()
	x := r.Executions[id]
	src := protocol.ExecutionSource{ActionID: id, AttemptID: x.Claim.AttemptID, Provider: "codex", Status: "UNAVAILABLE", Reason: "Observer killed actual host after durable claim; transport directory never existed, so exec.Start was not reached"}
	if err = f.submitSource(src); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	raw := protocol.RawReviewCandidate{ActionID: id, AttemptID: x.Claim.AttemptID, Provider: "codex", Action: "product-review", RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "RUNTIME_ERROR", Message: src.Reason}
	ev, _ := protocol.NewRegularEvent("pre-call-result", protocol.RegularEvent{Operation: "REVIEW_RESULT", ExpectedRevision: r.Revision, Review: &raw})
	if _, _, err = f.Submit(ev); err != nil {
		t.Fatal(err)
	}
	r, err = f.Drive()
	if err != nil || r.Status != "COMPLETE" {
		t.Fatalf("pre-call cleanup %s %v", r.Status, err)
	}
	t.Logf("actual host pid=%d killed after claim; native execution directory absent before and after UNKNOWN recovery; cleanup COMPLETE", child.Process.Pid)
}

func TestRegularCodexNativeAbortCollectsOnce(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in native cancellation race")
	}
	f := readyRegularFacade(t)
	r, _ := f.Show()
	action := ""
	for id := range r.Executions {
		action = id
	}
	done := make(chan error, 1)
	go func() { _, err := f.ExecuteNative(action, false); done <- err }()
	deadline := time.Now().Add(45 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(f.codexJob(action), "stdout.jsonl"))
		if strings.Contains(string(data), `"method":"turn/started"`) {
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		t.Fatal("native turn did not start")
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "Cancel this actual native canary turn and collect its observed failure once"})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if r.Regular.Terminal != "ABORTED" || r.Status != "COMPLETE" {
		t.Fatalf("abort did not settle: %+v", r.Next)
	}
	if len(r.Regular.RejectedResults) != 1 || !r.Executions[action].Finished {
		t.Fatal("cancellation did not retain exactly one failed native result")
	}
	if path := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); path != "" {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err = os.WriteFile(strings.TrimSuffix(path, ".json")+"-abort.json", b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// The native contract must carry known bindings and the existing plan semantics;
// it must not ask a model to infer either from generic string fields.
func TestNativeReviewContractUsesClaimAndBatchIDs(t *testing.T) {
	f := readyRegularFacade(t)
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var claim protocol.ExecutionClaim
	for _, x := range r.Executions {
		claim = x.Claim
	}
	for _, step := range []string{"product-review", "start-readiness"} {
		claim.Task.Step = authoring.StepID(step)
		schema := reviewSchema(claim)
		props := schema["properties"].(map[string]any)
		for field, want := range map[string]string{"actionId": claim.ActionID, "attemptId": claim.AttemptID, "provider": claim.Provider, "identity": "NATIVE_IDENTITY", "action": step, "requirementRevision": claim.RequirementRevision, "candidate": claim.Candidate} {
			spec := props[field].(map[string]any)
			enum, ok := spec["enum"].([]string)
			if !ok || len(enum) != 1 || enum[0] != want || spec["type"] != "string" {
				t.Fatalf("%s %s permits an invented binding: %+v", step, field, spec)
			}
		}
		prompt := reviewPrompt(r, claim)
		for _, binding := range []string{"action=" + step, "requirementRevision=" + claim.RequirementRevision, "Set identity to NATIVE_IDENTITY"} {
			if !strings.Contains(prompt, binding) {
				t.Fatalf("prompt omits binding %q", binding)
			}
		}
		if step == "start-readiness" {
			for _, meaning := range []string{"only other Batch IDs declared in this plan", "use [] when there are no cross-batch dependencies", "ordered subtasks array, not dependencies", "Do not put Subtask IDs, environment prerequisites or explanatory sentences"} {
				if !strings.Contains(prompt, meaning) {
					t.Fatalf("technical prompt omits %q", meaning)
				}
			}
			plan := props["granularityReview"].(map[string]any)["properties"].(map[string]any)
			if got := plan["requirementRevision"].(map[string]any)["enum"].([]string); len(got) != 1 || got[0] != claim.RequirementRevision {
				t.Fatalf("plan revision is not bound: %v", got)
			}
			batch := plan["batches"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			desc, _ := batch["dependencies"].(map[string]any)["description"].(string)
			for _, meaning := range []string{"Only other Batch IDs declared in this plan", "use []", "ordered subtasks", "Exclude Subtask IDs, environment prerequisites and explanatory sentences"} {
				if !strings.Contains(desc, meaning) {
					t.Fatalf("dependency schema omits %q: %s", meaning, desc)
				}
			}
		}
	}
}
