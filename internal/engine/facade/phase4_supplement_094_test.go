package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

type runningTaskResult struct {
	receipt protocol.TaskOperationReceipt
	err     error
}

func waitForRunningTaskPID(t *testing.T, f *Facade, q protocol.TaskOperationRequest) int {
	t.Helper()
	path := filepath.Join(f.taskOperationDir(protocol.TaskOperationID(q)), "process.json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var process struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(data, &process) == nil && process.PID > 0 && codexProcessAlive(process.PID) {
				return process.PID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("actual QA target did not expose a live process.json PID: %s", path)
	return 0
}

func submitChoiceWhilePIDAlive(t *testing.T, f *Facade, pid int, q protocol.RegularRequest, label string) {
	t.Helper()
	if !codexProcessAlive(pid) {
		t.Fatalf("%s: PID %d was not alive before REQUEST", label, pid)
	}
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("%s-request-%d", label, r.Revision)), protocol.RegularEvent{
		Operation:        "REQUEST",
		ExpectedRevision: r.Revision,
		Request:          &q,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, pending, err := f.Submit(ev)
	if err != nil {
		t.Fatalf("%s REQUEST: %v", label, err)
	}
	if pending.Regular == nil || pending.Regular.Ask == nil {
		t.Fatalf("%s REQUEST did not expose a typed confirmation", label)
	}
	if !codexProcessAlive(pid) {
		t.Fatalf("%s: PID %d was not alive before DECIDE", label, pid)
	}
	a := pending.Regular.Ask
	decision, err := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("%s-decide-%d", label, pending.Revision)), protocol.RegularEvent{
		Operation:        "DECIDE",
		ExpectedRevision: pending.Revision,
		Decision: &protocol.RegularDecision{
			RequestID: a.ID,
			Token:     a.Token,
			Digest:    a.Digest,
			Choice:    "confirm",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(decision); err != nil {
		t.Fatalf("%s DECIDE: %v", label, err)
	}
}

func startRunningQATarget(t *testing.T) (*Facade, protocol.RegularExecution, protocol.TaskOperationRequest, int, <-chan runningTaskResult) {
	t.Helper()
	f := frozenVerificationCommandFixture(t, false, &coverage.Command{Executable: "sleep", Args: []string{"30"}})
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	x, err := func() (protocol.RegularExecution, error) {
		r, err := f.Show()
		if err != nil {
			return protocol.RegularExecution{}, err
		}
		return r.Executions[x.Claim.ActionID], nil
	}()
	if err != nil {
		t.Fatal(err)
	}
	prepare := operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "running-window-prepare")
	if receipt, err := f.ExecuteTaskOperation(prepare, false); err != nil || receipt.Status != "COMPLETED" {
		t.Fatalf("qa.prepare: %+v %v", receipt, err)
	}
	q := operationRequest(t, f, x.Claim.ActionID, "qa.target", "running-window-target")
	done := make(chan runningTaskResult, 1)
	go func() {
		receipt, err := f.ExecuteTaskOperation(q, false)
		done <- runningTaskResult{receipt: receipt, err: err}
	}()
	pid := waitForRunningTaskPID(t, f, q)
	return f, x, q, pid, done
}

func finishRunningQATarget(t *testing.T, f *Facade, pid int, done <-chan runningTaskResult) runningTaskResult {
	t.Helper()
	if !codexProcessAlive(pid) {
		t.Fatalf("running-window target PID %d ended before teardown", pid)
	}
	if err := stopCodexProcess(pid); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("original QA target driver: %v", result.err)
		}
		return result
	case <-time.After(8 * time.Second):
		t.Fatal("original QA target driver did not persist its receipt after teardown")
		return runningTaskResult{}
	}
}

func taskOperationDirectoryCount(t *testing.T, f *Facade) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.stateDir, "artifacts", "task-operations"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return count
}

func assertStaleQAOperationsDoNotAllocate(t *testing.T, f *Facade, x protocol.RegularExecution, original protocol.TaskOperationRequest, pid int, result runningTaskResult) {
	t.Helper()
	if result.receipt.Status != "COMPLETED" {
		t.Fatalf("original target did not persist an observed completion: %+v", result.receipt)
	}
	first, err := f.ExecuteTaskOperation(original, true)
	if err != nil {
		t.Fatalf("resume=true after invalidation: %v", err)
	}
	second, err := f.ExecuteTaskOperation(original, true)
	if err != nil {
		t.Fatalf("second resume=true after invalidation: %v", err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, result.receipt) {
		t.Fatalf("resume returned a different stored observation: first=%+v second=%+v original=%+v", first, second, result.receipt)
	}

	beforeRun, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	beforeOps := len(beforeRun.Executions[x.Claim.ActionID].Operations)
	beforeDirs := taskOperationDirectoryCount(t, f)

	staleCollect := operationRequest(t, f, x.Claim.ActionID, "qa.collect", "stale-collect")
	if _, err = f.ExecuteTaskOperation(staleCollect, false); err == nil {
		t.Fatal("stale qa.collect unexpectedly admitted")
	}
	freshTarget := operationRequest(t, f, x.Claim.ActionID, "qa.target", "fresh-target-after-invalidation")
	if _, err = f.ExecuteTaskOperation(freshTarget, false); err == nil {
		t.Fatal("fresh qa.target unexpectedly admitted against the invalidated claim")
	}
	afterRun, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	afterOps := len(afterRun.Executions[x.Claim.ActionID].Operations)
	afterDirs := taskOperationDirectoryCount(t, f)
	if beforeOps != afterOps || beforeDirs != afterDirs {
		t.Fatalf("stale/reissued QA requests allocated state: operations %d->%d directories %d->%d", beforeOps, afterOps, beforeDirs, afterDirs)
	}

	late := protocol.VerificationResult{
		ActionID:            x.Claim.ActionID,
		AttemptID:           x.Claim.AttemptID,
		Provider:            x.Claim.Provider,
		Identity:            x.Identity,
		RequirementRevision: x.Claim.RequirementRevision,
		Candidate:           x.Claim.Candidate,
		BindingDigest:       x.Claim.PrerequisiteDigest,
		Status:              "PASS",
		Message:             "late result from the invalidated running QA target",
		Cases: []protocol.CaseExecution{{
			CaseID:      "BLACKBOX:C1",
			Status:      "PASS",
			Procedure:   "sleep 30",
			Output:      "",
			Comparison:  "target was stopped only after the typed invalidation",
			Environment: "isolated current candidate",
			Tool:        "sleep",
			Workspace:   result.receipt.Workspace,
		}},
	}
	acceptance, _, err := f.submitAdapterRegular("late-running-window", protocol.RegularEvent{
		Operation:          "VERIFICATION_RESULT",
		VerificationResult: &late,
	})
	if err != nil {
		t.Fatalf("late verification submission: %v", err)
	}
	if acceptance.Status != "REJECTED_RESULT" {
		t.Fatalf("late invalidated verification was not rejected: %+v", acceptance)
	}
	final, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if final.Regular.Verification != nil {
		if wave := verificationWave(final); wave != nil {
			if got, ok := wave.Results["blackbox"]; ok && got.Status == "PASS" {
				t.Fatal("invalidated late result entered effective verification")
			}
		}
	}
	t.Logf("running-window evidence pid=%d operation=%s exit=%d receipt=%s stale requests rejected without new operation/process", pid, protocol.TaskOperationID(original), result.receipt.ExitCode, result.receipt.Status)
}

func TestTaskOperationRunningTargetInvalidationBarriers(t *testing.T) {
	t.Run("requirement-apply", func(t *testing.T) {
		f, x, original, pid, done := startRunningQATarget(t)
		cleaned := false
		defer func() {
			if !cleaned {
				_ = stopCodexProcess(pid)
				select {
				case <-done:
				case <-time.After(8 * time.Second):
				}
			}
		}()
		before, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		oldRevision := before.Regular.Requirement.Revision
		changedText := strings.ReplaceAll(before.Regular.Requirement.Text, "hello", "goodbye")
		if changedText == before.Regular.Requirement.Text {
			t.Fatal("requirement fixture did not contain a changed normal phrase")
		}
		submitChoiceWhilePIDAlive(t, f, pid, protocol.RegularRequest{
			Action: "requirement-change", Origin: "USER_INITIATED_CHANGE",
			Reason: "replace the current normal acceptance text while QA is running",
		}, "requirement-change")
		if err := os.WriteFile(filepath.Join(f.root, before.Regular.Requirement.Path), []byte(changedText), 0600); err != nil {
			t.Fatal(err)
		}
		doc := protocol.Document(before.Regular.Requirement.Path, []byte(changedText))
		submitChoiceWhilePIDAlive(t, f, pid, protocol.RegularRequest{
			Action: "apply-requirement", Document: &doc, Meaning: "changed", Origin: "USER_INITIATED_CHANGE",
			Impact: map[string]string{"product-review": "affected", "start-readiness": "affected"},
			Reason: "accept the complete changed requirement while the original target is still running",
		}, "apply-requirement")
		after, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		if after.Regular.Requirement.Revision == oldRevision || after.Regular.ChangePending || after.Regular.Guarantee.Status == "ACTIVE" {
			t.Fatalf("requirement apply did not atomically bind the new inactive revision: %+v", after.Regular)
		}
		result := finishRunningQATarget(t, f, pid, done)
		cleaned = true
		assertStaleQAOperationsDoNotAllocate(t, f, x, original, pid, result)
		t.Logf("requirement/apply window hit with PID alive before each typed request/decision; old=%s new=%s", oldRevision, after.Regular.Requirement.Revision)
	})

	t.Run("adopt-external", func(t *testing.T) {
		f, x, original, pid, done := startRunningQATarget(t)
		cleaned := false
		defer func() {
			if !cleaned {
				_ = stopCodexProcess(pid)
				select {
				case <-done:
				case <-time.After(8 * time.Second):
				}
			}
		}()
		before, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		oldCandidate := before.Regular.Candidate
		if oldCandidate == "" || before.Regular.BaseSnapshot == "" {
			t.Fatalf("fixture lacks old candidate/base: candidate=%q base=%q", oldCandidate, before.Regular.BaseSnapshot)
		}
		path := filepath.Join(f.root, "external-adoption.txt")
		if err := os.WriteFile(path, []byte("external descendant evidence\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "external-adoption.txt"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "external descendant for running window"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = f.root
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s", args, out)
			}
		}
		headBytes, err := exec.Command("git", "-C", f.root, "rev-parse", "--verify", "HEAD^{commit}").Output()
		if err != nil {
			t.Fatal(err)
		}
		newHead := strings.TrimSpace(string(headBytes))
		diff, err := exec.Command("git", "-C", f.root, "diff", "--binary", oldCandidate, newHead, "--").Output()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(diff)
		impact := map[string]string{"product-review": "affected", "start-readiness": "affected"}
		submitChoiceWhilePIDAlive(t, f, pid, protocol.RegularRequest{
			Action:   "adopt-external",
			External: &protocol.ExternalObservation{Identity: newHead, Lineage: before.Regular.BaseSnapshot, DiffDigest: "sha256:" + hex.EncodeToString(sum[:])},
			Impact:   impact,
			Reason:   "adopt the observed descendant candidate while QA is running",
		}, "adopt-external")
		after, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		if after.Regular.Candidate != newHead || after.Regular.Guarantee.Status == "ACTIVE" {
			t.Fatalf("external adoption did not bind the new inactive candidate: candidate=%q new=%q guarantee=%+v", after.Regular.Candidate, newHead, after.Regular.Guarantee)
		}
		result := finishRunningQATarget(t, f, pid, done)
		cleaned = true
		assertStaleQAOperationsDoNotAllocate(t, f, x, original, pid, result)
		t.Logf("adopt-external window hit with PID alive before each typed request/decision; old=%s new=%s", oldCandidate, newHead)
	})
}
