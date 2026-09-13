package facade

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

func TestSharedPreparationBoundCommandAndSemanticHandoff(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "sh", Args: []string{"-c", `"$FORMAL_GATES_QA_SHARED/bin/hello"; printf 'semantic mismatch' >&2`}}, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "build", Procedure: "Build actual approved source once", Command: &coverage.Command{Executable: "go", Args: []string{"build", "-o", "bin/hello", "."}}}}
		m.Cases[0].SharedSteps = []string{"build"}
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	var preparation protocol.TaskOperationReceipt
	for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
		receipt, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, kind, kind), false)
		if err != nil || receipt.Status != "COMPLETED" {
			t.Fatalf("%s: %+v %v", kind, receipt, err)
		}
		if kind == "qa.prepare" {
			preparation = receipt
		}
		if kind == "qa.collect" && (receipt.Output != "hello" || receipt.Stderr != "semantic mismatch" || receipt.ExitCode != 0) {
			t.Fatalf("actual streams: %+v", receipt)
		}
	}
	if preparation.InputDigest == "" || preparation.SharedWorkspace == "" || preparation.PreparationReused {
		t.Fatal(preparation)
	}
	run, _ := f.Show()
	if verificationWave(run).Results["blackbox"].Status == "PASS" {
		t.Fatal("exit zero invented semantic PASS")
	}
	if run.Regular.Preparation.Resources["shared:"+preparation.InputDigest] != preparation.SharedWorkspace {
		t.Fatal("shared resource not owned by preparation")
	}
	delta, _ := json.Marshal(map[string]any{"cases": []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "FAIL", Procedure: "frozen command", Output: "hello", Comparison: "semantic mismatch on stderr despite exit zero", Environment: "local", Tool: "sh", Workspace: preparation.Workspace}}, "remaining": []string{}, "message": "independent semantic failure"})
	q := operationRequest(t, f, x.Claim.ActionID, "qa.checkpoint", "semantic-failure")
	q.CaseID = ""
	q.Prompt = string(delta)
	if _, err := f.RunTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	run, _ = f.Show()
	row := verificationWave(run).Checkpoints["blackbox"].Cases[0]
	if row.Status != "FAIL" || row.ExitCode == nil || *row.ExitCode != 0 || row.StderrPath == "" {
		t.Fatal("semantic FAIL lost actual zero exit evidence", row)
	}
}

func TestSharedPreparationConcurrentReuseAndWorkingChanges(t *testing.T) {
	f, action := operationFixture(t)
	run, _ := f.Show()
	q := operationRequest(t, f, action, "qa.prepare", "shared-probe")
	op := protocol.TaskOperation{ID: protocol.TaskOperationID(q), Request: q, Preparation: []coverage.SharedStep{{ID: "build", Procedure: "Build actual source", Artifacts: []string{"bin/hello"}, Command: &coverage.Command{Executable: "go", Args: []string{"build", "-o", "bin/hello", "."}}}}}
	if err := os.MkdirAll(f.taskOperationDir(op.ID), 0700); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	receipts := make([]protocol.TaskOperationReceipt, 2)
	errs := make([]error, 2)
	for i := range receipts {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = f.prepareShared(context.Background(), run, op, &receipts[i]) }(i)
	}
	wg.Wait()
	reused := 0
	for i, r := range receipts {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if r.PreparationReused {
			reused++
		}
	}
	if reused != 1 || receipts[0].SharedWorkspace != receipts[1].SharedWorkspace {
		t.Fatalf("receipts=%+v", receipts)
	}
	artifact := filepath.Join(receipts[0].SharedWorkspace, "bin", "hello")
	original, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	var missing protocol.TaskOperationReceipt
	if err := f.prepareShared(context.Background(), run, op, &missing); err == nil {
		t.Fatal("missing prepared artifact reused")
	}
	if err := os.WriteFile(artifact, original, 0755); err != nil {
		t.Fatal(err)
	}
	// An independent preparation must really execute even with identical inputs.
	op.Preparation[0].Independent = true
	var independent protocol.TaskOperationReceipt
	if err := f.prepareShared(context.Background(), run, op, &independent); err != nil {
		t.Fatal(err)
	}
	if independent.PreparationReused || independent.SharedWorkspace == receipts[0].SharedWorkspace {
		t.Fatal(independent)
	}
	// Use the registered source boundary with uncommitted edits; both the first
	// overlay and the subsequent same-HEAD change must produce distinct binaries.
	source := filepath.Join(f.stateDir, "resources", "shared-repair-test")
	if err := gitprep.Workspace(f.root, source, q.Binding.Candidate); err != nil {
		t.Fatal(err)
	}
	op.Request.Binding.Task.Step = "repair-worker"
	op.Request.Binding.PrerequisiteDigest = "shared-repair-test"
	run.Regular.Preparation.Resources["shared-repair-test"] = source
	op.Preparation[0].Independent = false
	previous := ""
	for _, value := range []string{"fixed-one", "fixed-two"} {
		writePreparationFile(t, source, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\""+value+"\")}\n")
		var r protocol.TaskOperationReceipt
		if err := f.prepareShared(context.Background(), run, op, &r); err != nil {
			t.Fatal(err)
		}
		if r.PreparationReused || r.SharedWorkspace == previous {
			t.Fatal("reused stale repair", r)
		}
		output, err := exec.Command(filepath.Join(r.SharedWorkspace, "bin", "hello")).CombinedOutput()
		if err != nil || string(output) != value {
			t.Fatalf("actual repaired binary: %q %v", output, err)
		}
		previous = r.SharedWorkspace
	}
}

func TestTaskOperationIndependentPendingAndSemanticDependency(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) {
		for _, id := range []string{"BLACKBOX:C2", "BLACKBOX:C3"} {
			c := m.Cases[0]
			c.CaseID = id
			if id == "BLACKBOX:C3" {
				c.Dependencies = []string{m.Cases[0].CaseID}
			}
			m.Cases = append(m.Cases, c)
			edge := m.Edges[0]
			edge.CaseID = id
			m.Edges = append(m.Edges, edge)
		}
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	call := func(caseID, kind, id string) (protocol.TaskOperationReceipt, error) {
		q := operationRequest(t, f, x.Claim.ActionID, kind, id)
		q.CaseID = caseID
		return f.ExecuteTaskOperation(q, false)
	}
	if r, e := call("BLACKBOX:C1", "qa.prepare", "a-prepare"); e != nil || r.Status != "COMPLETED" {
		t.Fatal(r, e)
	}
	pending := operationRequest(t, f, x.Claim.ActionID, "qa.target", "a-target")
	event, _ := protocol.NewRegularEvent("pending-independent-probe", protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", ExpectedRevision: pending.ExpectedRevision, TaskOperation: &pending})
	if _, _, err := f.Submit(event); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
		if r, e := call("BLACKBOX:C2", kind, "b-"+kind); e != nil || r.Status != "COMPLETED" {
			t.Fatal(kind, r, e)
		}
	}
	if _, err := call("BLACKBOX:C3", "qa.prepare", "c-blocked"); err == nil {
		t.Fatal("dependent case advanced while prerequisite is pending")
	}
	// Execute the already claimed intent at the adapter seam, then reconcile its
	// observation through the normal receipt path. This is a local crash fixture.
	run, _ := f.Show()
	op := run.Executions[x.Claim.ActionID].Operations[protocol.TaskOperationID(pending)]
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
	if _, err := f.ExecuteTaskOperation(pending, true); err != nil {
		t.Fatal(err)
	}
	if r, e := call("BLACKBOX:C1", "qa.collect", "a-collect"); e != nil || r.ExitCode != 0 {
		t.Fatal(r, e)
	}
	if _, err := call("BLACKBOX:C3", "qa.prepare", "c-semantic"); err == nil {
		t.Fatal("exit zero substituted for accepted semantic PASS")
	}
	run, _ = f.Show()
	attached := run.Executions[x.Claim.ActionID]
	checkpoint := protocol.VerificationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: attached.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: "CHECKPOINT", Message: "independent judgment of actual hello evidence", Cases: []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "PASS", Procedure: "go run .", Output: "hello", Comparison: "stdout exactly hello, stderr empty, exit 0", Environment: "local", Tool: "go", Workspace: receipt.Workspace}}, Remaining: []string{"BLACKBOX:C2", "BLACKBOX:C3"}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	if r, err := call("BLACKBOX:C3", "qa.prepare", "c-after-judgment"); err != nil || r.Status != "COMPLETED" {
		t.Fatal(r, err)
	}
}

func TestQAFixtureBuildConsumesPreparedSource(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "cmd", "formal-gates"), 0700); err != nil {
		t.Fatal(err)
	}
	writePreparationFile(t, source, "go.mod", "module fixture\n\ngo 1.23\n")
	writePreparationFile(t, source, "cmd/formal-gates/main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"prepared input\")}\n")
	script, err := filepath.Abs("../../../scripts/phase3-qa-fixture.sh")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(source, "candidate")
	output, err := exec.Command("sh", script, "build", "--source", source, "--output", binary).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture build: %s %v", output, err)
	}
	output, err = exec.Command(binary).CombinedOutput()
	if err != nil || string(output) != "prepared input" {
		t.Fatalf("actual binary: %s %v", output, err)
	}
	if _, err := os.Stat(filepath.Join(source, ".gates")); !os.IsNotExist(err) {
		t.Fatal("build created workflow state")
	}
}
