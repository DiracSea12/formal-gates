package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func operationRequest(t *testing.T, f *Facade, action, kind, requestID string) protocol.TaskOperationRequest {
	t.Helper()
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	x := r.Executions[action]
	binding := x.Claim
	binding.Identity = x.Identity
	return protocol.TaskOperationRequest{RunID: r.RunID, ExpectedRevision: r.Revision, Binding: binding, RequestID: requestID, Kind: kind, CaseID: "BLACKBOX:C1"}
}
func operationFixture(t *testing.T) (*Facade, string) {
	t.Helper()
	f := frozenVerificationFixture(t, false)
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	return f, x.Claim.ActionID
}
func TestTaskOperationQASequenceAdmissionAndReplay(t *testing.T) {
	f, action := operationFixture(t)
	// The frozen target is the actual Go application. Refused requests cannot
	// create its process journal; successful execution produces the real stdout.
	for _, kind := range []string{"qa.target", "qa.collect"} {
		q := operationRequest(t, f, action, kind, "skipped-"+kind)
		if _, err := f.ExecuteTaskOperation(q, false); err == nil || !strings.Contains(err.Error(), "PREREQUISITE_FAILED") {
			t.Fatalf("skip %s: %v", kind, err)
		}
		if _, err := os.Stat(f.taskOperationDir(protocol.TaskOperationID(q))); !os.IsNotExist(err) {
			t.Fatalf("refused %s created native invocation files", kind)
		}
	}
	for _, name := range []string{"unsigned", "provider", "candidate", "scope", "stale", "payload"} {
		q := operationRequest(t, f, action, "qa.prepare", "invalid-"+name)
		switch name {
		case "unsigned":
			q.Binding.ActionID = "not-issued"
		case "provider":
			q.Binding.Provider = "other"
		case "candidate":
			q.Binding.Candidate = "old"
		case "scope":
			q.CaseID = "BLACKBOX:OUTSIDE"
		case "stale":
			q.ExpectedRevision--
		case "payload":
			q.Binding.PayloadDigest = "wrong"
		}
		if _, err := f.ExecuteTaskOperation(q, false); err == nil {
			t.Fatalf("%s admitted", name)
		}
		if _, err := os.Stat(f.taskOperationDir(protocol.TaskOperationID(q))); !os.IsNotExist(err) {
			t.Fatalf("refused %s created a native invocation", name)
		}
	}
	var prepared, finished protocol.TaskOperationReceipt
	for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
		q := operationRequest(t, f, action, kind, kind)
		result, err := f.ExecuteTaskOperation(q, false)
		if err != nil || result.Status != "COMPLETED" {
			t.Fatalf("%s: %+v %v", kind, result, err)
		}
		if kind == "qa.prepare" {
			prepared = result
		}
		if kind == "qa.collect" {
			finished = result
		}
		if _, err = f.ExecuteTaskOperation(q, false); err == nil {
			t.Fatalf("%s replay admitted", kind)
		}
	}
	if finished.Output != "hello" || finished.Workspace != prepared.Workspace || finished.ExitCode != 0 {
		t.Fatalf("real command observation: %+v", finished)
	}
	r, _ := f.Show()
	if len(r.Executions[action].Operations) != 3 {
		t.Fatal("refusals allocated operations")
	}
	for _, op := range r.Executions[action].Operations {
		if op.Request.Binding.Task != r.Executions[action].Claim.Task {
			t.Fatal("new TaskKey created")
		}
	}
	// A new request ID cannot replay a completed QA stage either.
	if _, err := f.ExecuteTaskOperation(operationRequest(t, f, action, "qa.target", "new-id"), false); err == nil {
		t.Fatal("completed stage repeated")
	}
	t.Logf("actual frozen argv go run .; stdout=%q; prepared=%s; internal operations=3; outer tasks=%d", finished.Output, prepared.Workspace, len(r.Executions))
}

func TestTaskOperationConcurrentClaimAndUnknown(t *testing.T) {
	f, action := operationFixture(t)
	q := operationRequest(t, f, action, "qa.prepare", "concurrent")
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.ExecuteTaskOperation(q, false); errors <- err }()
	}
	wg.Wait()
	close(errors)
	ok := 0
	for err := range errors {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("successful claims=%d", ok)
	}
	r, _ := f.Show()
	op := r.Executions[action].Operations[protocol.TaskOperationID(q)]
	if op.Receipt == nil || len(r.Executions[action].Operations) != 1 {
		t.Fatal("claim was not singular")
	}
	// The normal typed CLAIM entry can be interrupted before the adapter starts.
	// Resume observes UNKNOWN and creates no process or replacement task.
	q = operationRequest(t, f, action, "qa.target", "before-call")
	event, _ := protocol.NewRegularEvent("interrupted-before-call", protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", ExpectedRevision: q.ExpectedRevision, TaskOperation: &q})
	if _, _, err := f.Submit(event); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ExecuteTaskOperation(q, true); err == nil || !strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatalf("recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.taskOperationDir(protocol.TaskOperationID(q)), "process.json")); !os.IsNotExist(err) {
		t.Fatal("UNKNOWN repeated native call")
	}
}

func TestTaskOperationFailedPreparationStopsTarget(t *testing.T) {
	f, action := operationFixture(t)
	q := operationRequest(t, f, action, "qa.prepare", "failure")
	// A normal pre-existing directory with user files is not an empty candidate
	// workspace. Preparation fails before clone; the target remains uncallable.
	workspace := filepath.Join(f.stateDir, "resources", "qa-operations", strings.TrimPrefix(protocol.TaskOperationID(q), "hact:task:"), "work")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "user-file"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := f.ExecuteTaskOperation(q, false)
	if err != nil || result.Status != "FAILED" {
		t.Fatalf("prepare %+v %v", result, err)
	}
	target := operationRequest(t, f, action, "qa.target", "blocked")
	if _, err = f.ExecuteTaskOperation(target, false); err == nil || !strings.Contains(err.Error(), "PREREQUISITE_FAILED") {
		t.Fatalf("failed prerequisite: %v", err)
	}
	if _, err = os.Stat(f.taskOperationDir(protocol.TaskOperationID(target))); !os.IsNotExist(err) {
		t.Fatal("failed prerequisite invoked target")
	}
	raw, _ := json.Marshal(result)
	t.Log(fmt.Sprintf("real preparation failure and zero target invocation: %s", raw))
}

// The child exits at the adapter's existing private fault seam, after the real
// process completed but before observed.json or the engine receipt exists.
// There is no installed testing switch or substitute provider in this test.
func TestTaskOperationWindowChild(t *testing.T) {
	if os.Getenv("FORMAL_GATES_OPERATION_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	data, err := os.ReadFile(os.Getenv("FORMAL_GATES_OPERATION_REQUEST"))
	if err != nil {
		t.Fatal(err)
	}
	var q protocol.TaskOperationRequest
	if err = json.Unmarshal(data, &q); err != nil {
		t.Fatal(err)
	}
	f, err := Open(os.Getenv("FORMAL_GATES_OPERATION_ROOT"), q.RunID)
	if err != nil {
		t.Fatal(err)
	}
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			os.Exit(73)
		}
		return nil
	}
	if _, err = f.ExecuteTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	t.Fatal("success-before-receipt window was not reached")
}
func interruptTaskOperationAfterSuccess(t *testing.T, f *Facade, q protocol.TaskOperationRequest) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "request.json")
	data, _ := json.Marshal(q)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestTaskOperationWindowChild$", "-test.v")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_OPERATION_CHILD=1", "FORMAL_GATES_OPERATION_ROOT="+f.root, "FORMAL_GATES_OPERATION_REQUEST="+path)
	started := time.Now().UTC()
	out, err := cmd.CombinedOutput()
	if err == nil || cmd.ProcessState.ExitCode() != 73 {
		t.Fatalf("window child: %v %s", err, out)
	}
	id := protocol.TaskOperationID(q)
	if _, err := os.Stat(filepath.Join(f.taskOperationDir(id), "observed.json")); !os.IsNotExist(err) {
		t.Fatal("receipt already persisted")
	}
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	op := r.Executions[q.Binding.ActionID].Operations[id]
	if op.Receipt != nil {
		t.Fatal("engine receipt already persisted")
	}
	process, err := os.ReadFile(filepath.Join(f.taskOperationDir(id), "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Stopped  bool `json:"stopped"`
		ExitCode int  `json:"exitCode"`
	}
	if json.Unmarshal(process, &observed) != nil || !observed.Stopped || observed.ExitCode != 0 {
		t.Fatalf("native completion not observed: %s", process)
	}
	return map[string]any{"startedAt": started, "interruptedAt": time.Now().UTC(), "childPID": cmd.ProcessState.Pid(), "childExitCode": 73, "process": json.RawMessage(process), "operationBeforeRecovery": op, "formalReceiptAbsent": true, "nativeObservedReceiptAbsent": true}
}
func writeOperationEvidence(t *testing.T, kind string, evidence map[string]any) {
	t.Helper()
	if path := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); path != "" {
		path += "." + kind + ".json"
		data, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("window evidence: %s", path)
	}
}
func TestTaskOperationGitCommitSuccessBeforeReceipt(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "git", Args: []string{"-c", "user.email=qa@example.invalid", "-c", "user.name=QA operation", "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "independent target invocation"}})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	prepare, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "git-prepare"), false)
	if err != nil || prepare.Status != "COMPLETED" {
		t.Fatal(prepare, err)
	}
	before, _ := gitprep.Head(prepare.Workspace)
	q := operationRequest(t, f, x.Claim.ActionID, "qa.target", "git-target")
	evidence := interruptTaskOperationAfterSuccess(t, f, q)
	after, _ := gitprep.Head(prepare.Workspace)
	count := func() string {
		cmd := exec.Command("git", "rev-list", "--count", before+"..HEAD")
		cmd.Dir = prepare.Workspace
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	if after == before || count() != "1" {
		t.Fatalf("actual Git commits before recovery: %s -> %s count=%s", before, after, count())
	}
	receipt, err := f.ExecuteTaskOperation(q, true)
	if err != nil || receipt.Status != "COMPLETED" {
		t.Fatal(receipt, err)
	}
	if count() != "1" {
		t.Fatal("recovery repeated actual Git commit")
	}
	for _, kind := range []string{"qa.prepare", "qa.target"} {
		if _, err = f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, kind, "replay-"+kind), false); err == nil {
			t.Fatalf("repeated %s", kind)
		}
	}
	if count() != "1" {
		t.Fatal("refused stage called Git")
	}
	collect, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "qa.collect", "git-collect"), false)
	if err != nil || collect.Status != "COMPLETED" || !strings.Contains(collect.Output, "independent target invocation") {
		t.Fatal(collect, err)
	}
	evidence["gitBefore"] = before
	evidence["gitAfter"] = after
	evidence["actualCommitCountBeforeRecovery"] = 1
	evidence["actualCommitCountAfterRecoveryAndRefusals"] = count()
	evidence["receiptAfterRecovery"] = receipt
	evidence["collected"] = collect
	writeOperationEvidence(t, "git", evidence)
	t.Logf("actual Git commit %s, independent rev-list count=1 before/after recovery and rejected replays", after)
}
func modelTranscriptMatches(t *testing.T, marker string) []string {
	t.Helper()
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		root, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(root, ".codex")
	}
	var matches []string
	err := filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), marker) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
func TestTaskOperationModelNativeSuccessBeforeReceipt(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in native model call")
	}
	f := readyRegularFacade(t)
	r, _ := f.Show()
	var x protocol.RegularExecution
	for _, v := range r.Executions {
		x = v
	}
	nativeFixtureClaim(t, f, x)
	marker := fmt.Sprintf("formal-gates-inner-model-%d", time.Now().UnixNano())
	q := operationRequest(t, f, x.Claim.ActionID, "model.run", marker)
	q.CaseID = ""
	q.Prompt = "Reply with exactly " + marker + ". Do not call tools."
	for _, name := range []string{"unsigned", "provider", "stale", "candidate", "payload", "scope"} {
		bad := q
		bad.RequestID = name
		switch name {
		case "unsigned":
			bad.Binding.ActionID = "unissued"
		case "provider":
			bad.Binding.Provider = "other"
		case "stale":
			bad.ExpectedRevision--
		case "candidate":
			bad.Binding.Candidate = "old"
		case "payload":
			bad.Binding.PayloadDigest = "wrong"
		case "scope":
			bad.Binding.Task.Scope = "outside"
		}
		if _, err := f.ExecuteTaskOperation(bad, false); err == nil {
			t.Fatalf("%s model call admitted", name)
		}
	}
	if got := modelTranscriptMatches(t, marker); len(got) != 0 {
		t.Fatalf("refused calls reached native provider: %v", got)
	}
	evidence := interruptTaskOperationAfterSuccess(t, f, q)
	matches := modelTranscriptMatches(t, marker)
	if len(matches) != 1 {
		t.Fatalf("actual native transcript count before resume: %v", matches)
	}
	receipt, err := f.ExecuteTaskOperation(q, true)
	if err != nil || receipt.Status != "COMPLETED" || !strings.Contains(receipt.Output, marker) {
		t.Fatal(receipt, err)
	}
	if matches[0] != receipt.TranscriptPath {
		t.Fatal("receipt not bound to the independent native transcript")
	}
	if _, err = f.ExecuteTaskOperation(q, false); err == nil {
		t.Fatal("native model replay admitted")
	}
	if got := modelTranscriptMatches(t, marker); len(got) != 1 {
		t.Fatalf("recovery/refusal created another native call: %v", got)
	}
	r, _ = f.Show()
	if len(r.Executions) != 1 || len(r.Executions[x.Claim.ActionID].Operations) != 1 {
		t.Fatal("internal model allocated another outer task")
	}
	entry := r.Cost.Dispatches[receipt.ID]
	if entry.TotalInputTokens <= 0 {
		t.Fatal("native usage not settled in existing cost ledger")
	}
	evidence["nativeTranscripts"] = matches
	evidence["refusedNativeCallCount"] = 0
	evidence["nativeCallCountBeforeRecovery"] = 1
	evidence["nativeCallCountAfterRecoveryAndReplay"] = 1
	evidence["receiptAfterRecovery"] = receipt
	evidence["cost"] = entry
	writeOperationEvidence(t, "model", evidence)
	t.Logf("actual model identity=%s transcript=%s; independent native count=1; input=%d output=%d", receipt.Identity, receipt.TranscriptPath, entry.TotalInputTokens, entry.OutputTokens)
}
func TestTaskOperationNativeBridgeQA(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in native dynamic tool")
	}
	f := frozenVerificationFixture(t, false)
	x := facadeExecution(t, f, "blackbox-execution")
	r, err := f.ExecuteNative(x.Claim.ActionID, false)
	if err != nil {
		t.Fatal(err)
	}
	x = r.Executions[x.Claim.ActionID]
	if !x.Finished || x.Identity == "" || len(x.Operations) != 3 {
		t.Fatalf("native QA bridge did not complete all three operations: %+v", x)
	}
	for _, op := range x.Operations {
		if op.Receipt == nil || op.Receipt.Status != "COMPLETED" {
			t.Fatalf("incomplete operation %+v", op)
		}
	}
	w := verificationWave(r)
	if w.Results["blackbox"].Status != "PASS" {
		t.Fatalf("native QA result: %+v rejected=%+v", w.Results, r.Regular.RejectedResults)
	}
	writeOperationEvidence(t, "native-qa", map[string]any{"execution": x, "result": w.Results["blackbox"], "cost": r.Cost.Dispatches[x.Claim.ActionID]})
	t.Logf("actual native QA identity=%s, frozen target go run ., three completed operations, result PASS", x.Identity)
}

func TestTaskOperationChangedCheckpointReopensOnlyNamedCase(t *testing.T) {
	f, action := operationFixture(t)
	var collected protocol.TaskOperationReceipt
	runCase := func(prefix string) {
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			var err error
			collected, err = f.ExecuteTaskOperation(operationRequest(t, f, action, kind, prefix+kind), false)
			if err != nil || collected.Status != "COMPLETED" {
				t.Fatal(kind, collected, err)
			}
		}
	}
	runCase("initial-")
	r, _ := f.Show()
	x := r.Executions[action]
	checkpoint := protocol.VerificationResult{ActionID: action, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: x.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: "CHECKPOINT", Message: "Save completed evidence before host update", Cases: []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "PASS", Procedure: "go run .", Output: collected.Output, Comparison: "exact hello", Environment: "host-A", Tool: "go-old", Workspace: collected.Workspace}}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	if _, err := f.ExecuteTaskOperation(operationRequest(t, f, action, "qa.prepare", "unchanged-replay"), false); err == nil {
		t.Fatal("unchanged checkpoint repeated target preparation")
	}
	checkpoint.Cases = nil
	checkpoint.Remaining = []string{"BLACKBOX:C1"}
	checkpoint.Invalidated = []protocol.CheckpointInvalidation{{CaseID: "BLACKBOX:C1", PreviousTool: "go-old", PreviousEnvironment: "host-A", Tool: "go-new", Environment: "host-A", Reason: "Actual tool version changed between checkpoints"}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	// A later incremental checkpoint does not repeat the invalidation payload.
	// Its accepted authorization must remain available until preparation uses it.
	checkpoint.Invalidated = nil
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	firstWorkspace := collected.Workspace
	runCase("changed-")
	if collected.Workspace == firstWorkspace {
		t.Fatal("changed case reused contaminated old target workspace")
	}
	if _, err := f.ExecuteTaskOperation(operationRequest(t, f, action, "qa.prepare", "second-replay"), false); err == nil {
		t.Fatal("same invalidation authorized a second rerun")
	}
	r, _ = f.Show()
	if len(r.Executions[action].Operations) != 6 || len(verificationWave(r).CheckpointHistory["blackbox"]) != 2 {
		t.Fatal("checkpoint/raw operation history was not retained")
	}
}
func TestTaskOperationAbortStopsOwnedTarget(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "sleep", Args: []string{"30"}})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	if _, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "prepare"), false); err != nil {
		t.Fatal(err)
	}
	q := operationRequest(t, f, x.Claim.ActionID, "qa.target", "target")
	done := make(chan error, 1)
	go func() { _, err := f.ExecuteTaskOperation(q, false); done <- err }()
	path := filepath.Join(f.taskOperationDir(protocol.TaskOperationID(q)), "process.json")
	var process struct {
		PID int `json:"pid"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &process) == nil && process.PID > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if process.PID == 0 || !codexProcessAlive(process.PID) {
		t.Fatal("actual target did not start")
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "Stop current owned target"})
	r, _ := f.Show()
	if err := f.abortTaskOperations(r); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if codexProcessAlive(process.PID) {
		t.Fatal("abort left owned target alive")
	}
	if _, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "model.run", "cancelled"), false); err == nil {
		t.Fatal("cancelled action invoked model")
	}
}

func TestRepairTaskOperationUsesWorkingEditsAndRecovery(t *testing.T) {
	f := frozenVerificationFixture(t, true)
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	r, _ := f.Show()
	w := verificationWave(r)
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &protocol.WaveAdjudication{WaveID: w.ID, Target: "blackbox", ResultDigest: w.Results["blackbox"].Digest, Decisions: []protocol.WaveFindingDecision{{FindingID: "hello-output", Decision: "NEW", ProblemID: "wrong-hello", Severity: "P1", Reason: "documented public output differs", Evidence: "actual go run . prints wrong", AffectedCases: []string{"BLACKBOX:C1"}}}}})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	plan := verificationRepair(r)
	source := r.Regular.Preparation.Resources[plan.ID]
	x := facadeExecution(t, f, "repair-worker")
	nativeFixtureClaim(t, f, x)
	// Exercise a tracked modification, deletion and untracked addition. A clean
	// old candidate would still print wrong, so it cannot pass this oracle.
	writePreparationFile(t, source, "go.mod", "module example.invalid/hello\n\ngo 1.23\n// repaired input\n")
	if err = os.Remove(filepath.Join(source, "main.go")); err != nil {
		t.Fatal(err)
	}
	writePreparationFile(t, source, "fixed.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
	for _, kind := range []string{"qa.target", "qa.collect"} {
		q := operationRequest(t, f, x.Claim.ActionID, kind, "skip-"+kind)
		if _, err = f.ExecuteTaskOperation(q, false); err == nil || !strings.Contains(err.Error(), "PREREQUISITE_FAILED") {
			t.Fatalf("skip %s: %v", kind, err)
		}
		if _, err = os.Stat(f.taskOperationDir(protocol.TaskOperationID(q))); !os.IsNotExist(err) {
			t.Fatal("refused operation created invocation files")
		}
	}
	for _, bad := range []string{"case", "plan", "stale"} {
		q := operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "bad-"+bad)
		switch bad {
		case "case":
			q.CaseID = "BLACKBOX:OUTSIDE"
		case "plan":
			q.Binding.PrerequisiteDigest = "old-plan"
		case "stale":
			q.ExpectedRevision--
		}
		if _, err = f.ExecuteTaskOperation(q, false); err == nil {
			t.Fatalf("admitted %s", bad)
		}
		if _, err = os.Stat(f.taskOperationDir(protocol.TaskOperationID(q))); !os.IsNotExist(err) {
			t.Fatal("refused operation created invocation files")
		}
	}
	prepare := operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "prepare")
	// Stop after actual overlay completion but before its engine/native receipt.
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			return fmt.Errorf("test interruption after prepared overlay")
		}
		return nil
	}
	if _, err = f.ExecuteTaskOperation(prepare, false); err == nil {
		t.Fatal("interruption not reached")
	}
	f.fault = nil
	prepared, err := f.ExecuteTaskOperation(prepare, true)
	if err != nil || prepared.Status != "COMPLETED" || prepared.Workspace == source {
		t.Fatalf("repair preparation: %+v %v", prepared, err)
	}
	if head, err := gitprep.Head(prepared.Workspace); err != nil || head != x.Claim.Candidate {
		t.Fatal(head, err)
	}
	if _, err = os.Stat(filepath.Join(prepared.Workspace, "main.go")); !os.IsNotExist(err) {
		t.Fatal("deleted source retained")
	}
	if data, err := os.ReadFile(filepath.Join(prepared.Workspace, "go.mod")); err != nil || !strings.Contains(string(data), "repaired input") {
		t.Fatal("tracked edit missing", err)
	}
	if _, err = os.Stat(filepath.Join(prepared.Workspace, "fixed.go")); err != nil {
		t.Fatal("new file missing", err)
	}
	if strings.Contains(prepared.Output, "immutable candidate") {
		t.Fatal("repair copy mislabeled immutable candidate")
	}
	q := operationRequest(t, f, x.Claim.ActionID, "qa.target", "target")
	window := interruptTaskOperationAfterSuccess(t, f, q)
	recovered, err := f.ExecuteTaskOperation(q, true)
	if err != nil || recovered.Status != "COMPLETED" || recovered.Workspace != prepared.Workspace {
		t.Fatalf("target recovery: %+v %v", recovered, err)
	}
	processPath := filepath.Join(f.taskOperationDir(protocol.TaskOperationID(q)), "process.json")
	before, _ := os.ReadFile(processPath)
	if _, err = f.ExecuteTaskOperation(q, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(processPath)
	if string(before) != string(after) {
		t.Fatal("completed target was invoked again")
	}
	collect := operationRequest(t, f, x.Claim.ActionID, "qa.collect", "collect")
	result, err := f.ExecuteTaskOperation(collect, false)
	if err != nil || result.Output != "hello" || result.ExitCode != 0 {
		t.Fatalf("actual repaired output: %+v %v", result, err)
	}
	if _, err = f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "repeat"), false); err == nil {
		t.Fatal("completed prepare stage repeated")
	}
	if head, _ := gitprep.Head(source); head != x.Claim.Candidate {
		t.Fatal("repair self-test committed source")
	}
	if index, _ := gitprep.Git(source, "diff", "--cached", "--name-only"); index != "" {
		t.Fatal("repair self-test staged source")
	}
	r, _ = f.Show()
	if len(r.Executions[x.Claim.ActionID].Operations) != 3 {
		t.Fatal("refusals allocated operations")
	}
	raw, _ := json.Marshal(window)
	t.Logf("actual repair edits copied; stdout=%q; target recovery=%s; original candidate=%s", result.Output, raw, x.Claim.Candidate)
}

func TestProcedureExecutionUsesEngineAdmissionAndRealEvidence(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) {
		m.Cases[0].Command = nil
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	action := x.Claim.ActionID
	pending := runBatch(t, f, action)
	if len(pending.Pending.Input) != 1 || pending.Pending.Input[0] != "BLACKBOX:C1" || len(pending.Pending.Blocked) != 0 {
		t.Fatalf("approved procedure should await executor input after automatic prepare: %+v", pending)
	}
	// A procedure awaiting its execution command is still unfinished work.
	// The progress view must count it even though no target can start yet.
	progress, err := f.ProgressSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := progress.PendingCases[action].Remaining; got != 1 {
		t.Fatalf("awaiting command omitted from remaining count: %d", got)
	}
	command := &coverage.Command{Executable: "sh", Args: []string{"-c", "go run .; printf observed-stderr >&2; exit 7"}, ExpectedExitCode: 7}
	q := operationRequest(t, f, action, "qa.target", "outside")
	q.CaseID = "BLACKBOX:OUTSIDE"
	q.Command = command
	if _, err := f.ExecuteTaskOperation(q, false); err == nil || !strings.Contains(err.Error(), "SCOPE_MISMATCH") {
		t.Fatalf("out-of-scope procedure admitted: %v", err)
	}
	run, _ := f.Show()
	input, _ := json.Marshal(map[string]any{"kind": "qa.target", "caseId": "BLACKBOX:C1", "prompt": "", "command": command})
	if _, err := f.dispatchNativeOperation(action, run.Executions[action].Identity, "procedure", input); err != nil {
		t.Fatal(err)
	}
	pending = runBatch(t, f, action)
	if len(pending.Pending.Judgment) != 1 || len(pending.Pending.Input) != 0 {
		t.Fatalf("missing semantic judgment: %+v", pending)
	}
	run, _ = f.Show()
	collected := latestTaskOperation(run.Executions[action], "qa.collect", "BLACKBOX:C1")
	if collected == nil || collected.Receipt.Output != "hello" || collected.Receipt.Stderr != "observed-stderr" || collected.Receipt.ExitCode != 7 {
		t.Fatalf("actual streams and exit not preserved: %+v", collected)
	}
	target := latestTaskOperation(run.Executions[action], "qa.target", "BLACKBOX:C1")
	if target.Command == nil || target.Request.Command == nil || target.Command.Executable != "sh" {
		t.Fatal("execution command not persisted in original intent")
	}
	q = operationRequest(t, f, action, "qa.target", "repeat")
	q.Command = command
	if _, err := f.ExecuteTaskOperation(q, false); err == nil {
		t.Fatal("procedure repeated after collection")
	}
}

func TestApprovedCommandCannotBeReplacedByExecution(t *testing.T) {
	f, action := operationFixture(t)
	q := operationRequest(t, f, action, "qa.target", "replace")
	q.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", "echo replacement"}}
	if _, err := f.ExecuteTaskOperation(q, false); err == nil || !strings.Contains(err.Error(), "differs from approved command") {
		t.Fatalf("approved command replacement: %v", err)
	}
}
