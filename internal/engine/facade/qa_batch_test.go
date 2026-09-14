package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func batchFixture(t *testing.T) (*Facade, string) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) {
		first := m.Cases[0]
		edge := m.Edges[0]
		m.Cases = nil
		m.Edges = nil
		for i := 1; i <= 7; i++ {
			c := first
			c.CaseID = fmt.Sprintf("BLACKBOX:C%d", i)
			c.WorkspaceMode = "isolated"
			c.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", "printf actual; printf semantic >&2"}}
			if i == 2 {
				c.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", "printf actual; printf semantic >&2; exit 3"}}
			}
			if i == 3 {
				c.Dependencies = []string{"BLACKBOX:C2"}
			}
			m.Cases = append(m.Cases, c)
			e := edge
			e.CaseID = c.CaseID
			m.Edges = append(m.Edges, e)
		}
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	return f, x.Claim.ActionID
}
func runBatch(t *testing.T, f *Facade, action string) QABatchReceipt {
	t.Helper()
	q := operationRequest(t, f, action, "qa.run", "batch-call")
	q.CaseID = ""
	result, err := f.RunTaskOperation(q, false)
	if err != nil {
		t.Fatal(err)
	}
	return result.(QABatchReceipt)
}
func batchResult(t *testing.T, f *Facade, action string, status string, ids, remaining []string) protocol.VerificationResult {
	t.Helper()
	run, _ := f.Show()
	x := run.Executions[action]
	result := protocol.VerificationResult{ActionID: action, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: x.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: status, Message: "incremental independent judgment", Remaining: remaining}
	for _, id := range ids {
		op := latestTaskOperation(x, "qa.collect", id)
		result.Cases = append(result.Cases, protocol.CaseExecution{CaseID: id, Status: "PASS", Procedure: "frozen shell assertion", Output: op.Receipt.Output, Comparison: "independent semantic comparison", Environment: "local", Tool: "sh", Workspace: op.Receipt.Workspace})
	}
	return result
}
func TestQABatchContinuousQueueRecoveryAndDelta(t *testing.T) {
	f, action := batchFixture(t)
	q := operationRequest(t, f, action, "qa.run", "initial")
	q.CaseID = ""
	snapshot, _ := f.engine.Load()
	plan, err := protocol.PendingQA(snapshot.State, q.Binding, "")
	if err != nil {
		t.Fatal(err)
	}
	// Fill the configured four operation slots with interrupted prepare claims.
	var claimed []protocol.TaskOperationRequest
	for _, request := range plan.Ready[:4] {
		run, _ := f.Show()
		request.ExpectedRevision = run.Revision
		event, _ := protocol.NewRegularEvent(protocol.EventID("queue-probe:"+request.CaseID), protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", ExpectedRevision: request.ExpectedRevision, TaskOperation: &request})
		if _, _, err := f.Submit(event); err != nil {
			t.Fatal(err)
		}
		claimed = append(claimed, request)
	}
	blocked := runBatch(t, f, action)
	queued := false
	for _, reason := range blocked.Pending.Blocked {
		queued = queued || strings.Contains(reason, "CAPACITY_QUEUED")
	}
	if !queued {
		t.Fatalf("no capacity queue: %+v", blocked)
	}
	run, _ := f.Show()
	if len(run.Executions[action].Operations) != 4 {
		t.Fatal("queue allocated extra claims")
	}
	for _, request := range claimed {
		run, _ = f.Show()
		op := run.Executions[action].Operations[protocol.TaskOperationID(request)]
		job := f.taskOperationDir(op.ID)
		if err := os.MkdirAll(job, 0700); err != nil {
			t.Fatal(err)
		}
		receipt := f.performTaskOperation(run, op, job)
		if receipt.Status != "COMPLETED" {
			t.Fatal(receipt)
		}
		if err := writeJSONAtomic(filepath.Join(job, "observed.json"), receipt); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ExecuteTaskOperation(request, true); err != nil {
			t.Fatal(err)
		}
	}
	consumed := runBatch(t, f, action)
	if len(consumed.Pending.Judgment) != 6 || len(consumed.Pending.Blocked) != 1 {
		t.Fatalf("pending: %+v", consumed.Pending)
	}
	run, _ = f.Show()
	if len(run.Executions[action].Operations) != 18 {
		t.Fatalf("operations=%d", len(run.Executions[action].Operations))
	}
	for _, op := range run.Executions[action].Operations {
		if op.Receipt == nil {
			t.Fatal("missing per-operation receipt")
		}
		if op.Request.Kind == "qa.prepare" {
			if _, err := os.Stat(filepath.Join(op.Receipt.Workspace, "main.go")); !os.IsNotExist(err) {
				t.Fatal("isolated case copied full source")
			}
		}
	}
	if len(verificationWave(run).Results) != 0 {
		t.Fatal("mechanical exits manufactured semantic results")
	}
	// Save an actual semantic FAIL with its real nonzero exit and stderr, together
	// with an independent PASS. Later checkpoints send only their new rows.
	first := batchResult(t, f, action, "CHECKPOINT", []string{"BLACKBOX:C1", "BLACKBOX:C2"}, []string{"BLACKBOX:C3", "BLACKBOX:C4", "BLACKBOX:C5", "BLACKBOX:C6", "BLACKBOX:C7"})
	first.Cases[1].Status = "FAIL"
	delta, _ := json.Marshal(map[string]any{"cases": first.Cases, "remaining": first.Remaining, "message": first.Message})
	checkpointCall := operationRequest(t, f, action, "qa.checkpoint", "first-checkpoint")
	checkpointCall.CaseID = ""
	checkpointCall.Prompt = string(delta)
	if _, err := f.RunTaskOperation(checkpointCall, false); err != nil {
		t.Fatal(err)
	}
	run, _ = f.Show()
	old := verificationWave(run).Checkpoints["blackbox"]
	oldBytes, _ := json.Marshal(old.Cases)
	second := batchResult(t, f, action, "CHECKPOINT", []string{"BLACKBOX:C4"}, []string{"BLACKBOX:C3", "BLACKBOX:C5", "BLACKBOX:C6", "BLACKBOX:C7"})
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &second})
	run, _ = f.Show()
	checkpoint := verificationWave(run).Checkpoints["blackbox"]
	retainedBytes, _ := json.Marshal(checkpoint.Cases[:2])
	if string(oldBytes) != string(retainedBytes) || len(checkpoint.Cases) != 3 {
		t.Fatal("delta changed old observations")
	}
	if checkpoint.Cases[1].ExitCode == nil || *checkpoint.Cases[1].ExitCode != 3 || checkpoint.Cases[1].StderrPath == "" {
		t.Fatal("missing raw failure streams/exit")
	}
	before := len(run.Executions[action].Operations)
	resumed := runBatch(t, f, action)
	run, _ = f.Show()
	if len(run.Executions[action].Operations) != before || len(resumed.Receipts) != 0 {
		t.Fatal("batch reran retained or collected work")
	}
	// A missing file or conflicting exit rejects before altering the writer.
	bad := batchResult(t, f, action, "CHECKPOINT", []string{"BLACKBOX:C5"}, []string{"BLACKBOX:C3", "BLACKBOX:C6", "BLACKBOX:C7"})
	code := 99
	bad.Cases[0].ExitCode = &code
	event, _ := protocol.NewRegularEvent("conflicting-exit", protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &bad})
	if _, _, err := f.Submit(event); err == nil {
		t.Fatal("conflicting exit accepted")
	}
	bad.Cases[0].ExitCode = nil
	target := latestTaskOperation(run.Executions[action], "qa.target", "BLACKBOX:C5")
	path := filepath.Join(f.taskOperationDir(target.ID), "stderr.log")
	data, _ := os.ReadFile(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(event); err == nil {
		t.Fatal("missing stderr accepted")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	after, _ := f.Show()
	if !reflect.DeepEqual(verificationWave(after).Checkpoints["blackbox"], checkpoint) {
		t.Fatal("invalid evidence changed checkpoint")
	}
	final := batchResult(t, f, action, "FAIL", []string{"BLACKBOX:C5", "BLACKBOX:C6", "BLACKBOX:C7"}, nil)
	final.Cases = append(final.Cases, protocol.CaseExecution{CaseID: "BLACKBOX:C3", Status: "BLOCKED", Procedure: "dependency C2", Output: "C2 failed with actual exit 3", Comparison: "requires accepted C2 PASS", Environment: "local", Tool: "sh", Dependencies: []string{"BLACKBOX:C2"}})
	final.Findings = []protocol.ReviewFinding{{ID: "exit-C2", Severity: "P1", Category: "normal-use", Requirement: "BLACKBOX:C2", Problem: "expected exit 0 but actual exit 3", NormalPath: "run frozen C2 command", Consequence: "declared command contract fails", SeverityRationale: "required exit behavior differs", Remedy: "correct the target's exit"}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &final})
	ended, _ := f.Show()
	saved := verificationWave(ended).Results["blackbox"]
	if saved.Status != "FAIL" || len(saved.Cases) != 7 {
		t.Fatalf("final delta: %+v", saved)
	}
	bytes, _ := json.Marshal(saved.Cases[:2])
	if string(bytes) != string(oldBytes) {
		t.Fatal("final submit rewrote old PASS/FAIL")
	}
}

func TestQASharedMutableResourceWaitsForOwner(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "fixture", Procedure: "create shared mutable fixture", Command: &coverage.Command{Executable: "sh", Args: []string{"-c", "printf stable > tool; : > mutable"}}, Artifacts: []string{"tool"}}}
		m.Cases[0].SharedSteps = []string{"fixture"}
		m.Cases[0].WorkspaceMode = "isolated"
		m.Cases[0].Command = &coverage.Command{Executable: "sh", Args: []string{"-c", `printf A >> "$FORMAL_GATES_QA_SHARED/mutable"`}}
		c := m.Cases[0]
		c.CaseID = "BLACKBOX:C2"
		c.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", `printf B >> "$FORMAL_GATES_QA_SHARED/mutable"`}}
		m.Cases = append(m.Cases, c)
		edge := m.Edges[0]
		edge.CaseID = c.CaseID
		m.Edges = append(m.Edges, edge)
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	var first protocol.TaskOperationReceipt
	for _, id := range []string{"BLACKBOX:C1", "BLACKBOX:C2"} {
		q := operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "prepare-"+id)
		q.CaseID = id
		r, err := f.ExecuteTaskOperation(q, false)
		if err != nil || r.Status != "COMPLETED" {
			t.Fatal(r, err)
		}
		if id == "BLACKBOX:C1" {
			first = r
		} else if !r.PreparationReused || r.SharedWorkspace != first.SharedWorkspace {
			t.Fatal("fixture not shared", r)
		}
	}
	q := operationRequest(t, f, x.Claim.ActionID, "qa.target", "owner-target")
	event, _ := protocol.NewRegularEvent("owner-target", protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", ExpectedRevision: q.ExpectedRevision, TaskOperation: &q})
	if _, _, err := f.Submit(event); err != nil {
		t.Fatal(err)
	}
	other := operationRequest(t, f, x.Claim.ActionID, "qa.target", "blocked-target")
	other.CaseID = "BLACKBOX:C2"
	if _, err := f.ExecuteTaskOperation(other, false); err == nil || !strings.Contains(err.Error(), "RESOURCE_QUEUED") {
		t.Fatalf("resource admission: %v", err)
	}
	run, _ := f.Show()
	op := run.Executions[x.Claim.ActionID].Operations[protocol.TaskOperationID(q)]
	job := f.taskOperationDir(op.ID)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	receipt := f.performTaskOperation(run, op, job)
	if receipt.Status != "COMPLETED" {
		t.Fatal(receipt)
	}
	if err := writeJSONAtomic(filepath.Join(job, "observed.json"), receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ExecuteTaskOperation(q, true); err != nil {
		t.Fatal(err)
	}
	result := runBatch(t, f, x.Claim.ActionID)
	if len(result.Pending.Judgment) != 2 {
		t.Fatal(result)
	}
	content, err := os.ReadFile(filepath.Join(first.SharedWorkspace, "mutable"))
	if err != nil || string(content) != "AB" {
		t.Fatalf("shared resource ordering: %q %v", content, err)
	}
}

func TestRepairCheckpointKeepsAttachedWorker(t *testing.T) {
	f := frozenVerificationFixture(t, true)
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	run, _ := f.Show()
	wave := verificationWave(run)
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &protocol.WaveAdjudication{WaveID: wave.ID, Target: "blackbox", ResultDigest: wave.Results["blackbox"].Digest, Decisions: []protocol.WaveFindingDecision{{FindingID: "hello-output", Decision: "NEW", ProblemID: "wrong-hello", Severity: "P1", Reason: "documented output differs", Evidence: "actual go run . prints wrong", AffectedCases: []string{"BLACKBOX:C1"}}}}})
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	plan := verificationRepair(run)
	source := run.Regular.Preparation.Resources[plan.ID]
	x := facadeExecution(t, f, "repair")
	nativeFixtureClaim(t, f, x)
	writePreparationFile(t, source, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
	result := runBatch(t, f, x.Claim.ActionID)
	if len(result.Pending.Judgment) != 1 {
		t.Fatal(result)
	}
	run, _ = f.Show()
	collect := latestTaskOperation(run.Executions[x.Claim.ActionID], "qa.collect", "BLACKBOX:C1")
	delta, _ := json.Marshal(map[string]any{"cases": []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "PASS", Procedure: "go run .", Output: collect.Receipt.Output, Comparison: "exact hello, empty stderr, exit 0", Environment: "local repair", Tool: "go", Workspace: collect.Receipt.Workspace}}, "remaining": []string{}, "message": "actual repaired behavior judged"})
	q := operationRequest(t, f, x.Claim.ActionID, "qa.checkpoint", "repair-judgment")
	q.CaseID = ""
	q.Prompt = string(delta)
	if _, err := f.RunTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	run, _ = f.Show()
	attached := run.Executions[x.Claim.ActionID]
	if attached.Finished || attached.Identity != q.Binding.Identity || len(attached.Operations) != 3 {
		t.Fatal("checkpoint changed worker or replayed targets")
	}
	checkpoint := verificationWave(run).Checkpoints["repair"]
	if len(checkpoint.Cases) != 1 || checkpoint.BindingDigest != plan.ID || checkpoint.Cases[0].OperationID != collect.ID {
		t.Fatal("repair checkpoint lacks exact source binding", checkpoint)
	}
	if verificationWave(run).Results["blackbox"].Status != "FAIL" {
		t.Fatal("repair self-test replaced formal QA evidence")
	}
	bad := q
	bad.RequestID = "bad-delta"
	bad.Prompt = `{"cases":[],"remaining":["OUTSIDE"],"message":"wrong scope"}`
	if _, err := f.RunTaskOperation(bad, false); err == nil {
		t.Fatal("invalid repair delta accepted")
	}
	now, _ := f.Show()
	if now.Executions[x.Claim.ActionID].Finished {
		t.Fatal("malformed delta ended repair worker")
	}
}
