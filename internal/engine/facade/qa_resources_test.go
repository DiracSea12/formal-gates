package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func TestQAResourcesIntentReleaseAndTerminalEvidence(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "sh", Args: []string{"-c", `test -d "$FORMAL_GATES_QA_WORK" && test "$TMPDIR" = "$FORMAL_GATES_QA_TMP" && test -d "$FORMAL_GATES_QA_CACHE" || exit 99
printf delivered > "$FORMAL_GATES_QA_OUTPUT/result.txt"
printf actual; printf failure >&2; exit 7`}})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	var prepare protocol.TaskOperation
	var target protocol.TaskOperationReceipt
	for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
		q := operationRequest(t, f, x.Claim.ActionID, kind, kind)
		r, err := f.ExecuteTaskOperation(q, false)
		if err != nil || r.Status != "COMPLETED" {
			t.Fatal(kind, r, err)
		}
		run, _ := f.Show()
		if kind == "qa.prepare" {
			prepare = run.Executions[x.Claim.ActionID].Operations[r.ID]
			if prepare.Resources == nil || prepare.Request.RunID != run.RunID || prepare.Request.Binding.Candidate == "" {
				t.Fatal("missing intent ownership", prepare)
			}
			if _, err := os.Stat(f.operationResources(prepare).Temp); err != nil {
				t.Fatal(err)
			}
		}
		if kind == "qa.target" {
			target = r
		}
		if kind == "qa.collect" && (r.Output != "actual" || r.Stderr != "failure" || r.ExitCode != 7) {
			t.Fatal(r)
		}
	}
	runBefore, _ := f.Show()
	// A finished owner still cannot release a scene needed by a later consumer.
	qa := runBefore.Regular.Preparation.QA["blackbox"]
	consumer := qa.Design.Manifest.Cases[0]
	consumer.CaseID = "BLACKBOX:CONSUMER"
	consumer.Dependencies = []string{prepare.Request.CaseID}
	qa.Design.Manifest.Cases = append(qa.Design.Manifest.Cases, consumer)
	finished := runBefore.Executions[x.Claim.ActionID]
	finished.Finished = true
	runBefore.Executions[x.Claim.ActionID] = finished
	if err := f.releaseCollectedResources(runBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.operationResources(prepare).Work); err != nil {
		t.Fatal("dependency scene lost", err)
	}
	qa.Design.Manifest.Cases = qa.Design.Manifest.Cases[:len(qa.Design.Manifest.Cases)-1]
	if err := f.releaseCollectedResources(runBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.operationResources(prepare).Root); !os.IsNotExist(err) {
		t.Fatal("case scene retained", err)
	}
	delivered := filepath.Join(f.stateDir, "artifacts", "qa-output", strings.TrimPrefix(prepare.ID, "hact:task:"), "result.txt")
	if b, err := os.ReadFile(delivered); err != nil || string(b) != "delivered" {
		t.Fatal(string(b), err)
	}
	run, _ := f.Show()
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal(err)
	}
	if err := f.cleanupTaskOperationFiles(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stdout.log", "stderr.log", "process.json", "observed.json"} {
		if _, err := os.ReadFile(filepath.Join(f.taskOperationDir(target.ID), name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := os.ReadFile(delivered); err != nil {
		t.Fatal(err)
	}
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal("cleanup resume", err)
	}
}

func TestQAResourcesPrepareInterruptionKeepsIntent(t *testing.T) {
	f, action := operationFixture(t)
	q := operationRequest(t, f, action, "qa.prepare", "interrupted")
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			return os.ErrClosed
		}
		return nil
	}
	r, err := f.ExecuteTaskOperation(q, false)
	if err == nil {
		t.Fatal("fault not injected")
	}
	run, _ := f.Show()
	op := run.Executions[action].Operations[protocol.TaskOperationID(q)]
	if op.Resources == nil || op.Receipt != nil {
		t.Fatal("intent lost", op)
	}
	f.fault = nil
	resumed, err := f.ExecuteTaskOperation(q, true)
	if err != nil || resumed.Workspace != r.Workspace {
		t.Fatal(resumed, err)
	}
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.operationResources(op).Root); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestQAResourcesCleanupFailureRetainsSceneAndRetries(t *testing.T) {
	f, action := operationFixture(t)
	q := operationRequest(t, f, action, "qa.prepare", "prepare")
	r, err := f.ExecuteTaskOperation(q, false)
	if err != nil || r.Status != "COMPLETED" {
		t.Fatal(r, err)
	}
	run, _ := f.Show()
	op := run.Executions[action].Operations[r.ID]
	resource := f.operationResources(op)
	if err := os.WriteFile(filepath.Join(resource.Output, "answer"), []byte("evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(f.stateDir, "artifacts", "qa-output")
	if err := os.MkdirAll(filepath.Dir(blocker), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("normal path collision"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.cleanupPreparationResources(run); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	if _, err := os.Stat(resource.Work); err != nil {
		t.Fatal("scene deleted before evidence saved", err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(blocker, strings.TrimPrefix(op.ID, "hact:task:"), "answer")); err != nil || string(b) != "evidence" {
		t.Fatal(string(b), err)
	}
}

func TestQAResourcesAbortStopsInterruptedSharedPreparation(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "sh", Args: []string{"-c", "true"}}, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "wait", Procedure: "owned wait", Command: &coverage.Command{Executable: "sleep", Args: []string{"30"}}}}
		m.Cases[0].SharedSteps = []string{"wait"}
	})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	q := operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "interrupted-shared")
	done := make(chan error, 1)
	go func() { _, err := f.ExecuteTaskOperation(q, false); done <- err }()
	path := filepath.Join(f.taskOperationDir(protocol.TaskOperationID(q)), "preparation", "0", "process.json")
	var process struct {
		PID int `json:"pid"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &process)
			if process.PID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if process.PID == 0 {
		t.Fatal("prepare process not observed")
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "Stop owned preparation"})
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.abortTaskOperations(run); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if codexProcessAlive(process.PID) {
		t.Fatal("prepare process survived abort")
	}
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	op := run.Executions[x.Claim.ActionID].Operations[protocol.TaskOperationID(q)]
	if op.Receipt == nil || op.Receipt.Status != "FAILED" {
		t.Fatal("interrupted prepare reported success", op)
	}
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal(err)
	}
	if err := f.cleanupTaskOperationFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal("failed preparation evidence lost", err)
	}
}

func TestQAResourcesInheritToolCaches(t *testing.T) {
	t.Setenv("GOCACHE", "/project/existing/build-cache")
	t.Setenv("GOMODCACHE", "/project/existing/module-cache")
	f := &Facade{stateDir: t.TempDir()}
	for _, independent := range []bool{false, true} {
		op := protocol.TaskOperation{ID: "cache-test", Preparation: []coverage.SharedStep{{Independent: independent}}}
		env := strings.Join(f.operationEnvironment(op, ""), "\n")
		for _, entry := range []string{"GOCACHE=/project/existing/build-cache", "GOMODCACHE=/project/existing/module-cache"} {
			if !strings.Contains(env, entry+"\n") && !strings.HasSuffix(env, entry) {
				t.Fatalf("tool cache overwritten: %s", entry)
			}
		}
		if err := f.createOperationResources(op); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(f.operationResources(op).Cache, "go-build")); !os.IsNotExist(err) {
			t.Fatal("generic resources created a language cache", err)
		}
	}
}
