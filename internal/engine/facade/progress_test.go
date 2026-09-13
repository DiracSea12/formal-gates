package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirements"
)

func TestProgressIncrementalReceiptAndActualMessages(t *testing.T) {
	f, action := operationFixture(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	job := f.codexJob(action)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	message := "actual native input with candidate " + run.Executions[action].Claim.Candidate
	if err := os.WriteFile(filepath.Join(job, "prompt.txt"), []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := f.ProgressSince(0)
	if err != nil {
		t.Fatal(err)
	}
	q := operationRequest(t, f, action, "qa.prepare", "progress-prepare")
	receipt, err := f.ExecuteTaskOperation(q, false)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.ProgressSince(before.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.NewResults) != 1 || after.NewResults[0].OperationID != receipt.ID {
		t.Fatalf("new receipt omitted: %+v", after.NewResults)
	}
	if after.PendingCases[action].Remaining != 1 {
		t.Fatalf("native identity pending view missing: %+v", after.PendingCases)
	}
	if len(after.Messages) != 1 {
		t.Fatalf("missing actual message: %+v", after.Messages)
	}
	actual, err := os.ReadFile(after.Messages[0].Path)
	if err != nil || string(actual) != message {
		t.Fatalf("message not accessible: %q %v", actual, err)
	}
	if after.Messages[0].Candidate != q.Binding.Candidate {
		t.Fatal("candidate drift")
	}
	repeat, err := f.ProgressSince(after.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeat.NewResults) != 0 || repeat.Revision != after.Revision {
		t.Fatalf("progress mutated/repeated: %+v", repeat)
	}
	if after.ElapsedMillis == nil || after.NewResults[0].DurationMillis == nil {
		t.Fatal("new run/preparation lost elapsed timing")
	}
	if after.PreparationExecuted+after.PreparationReused != 1 {
		t.Fatalf("prepare counted incorrectly: %+v", after)
	}
	if _, err := f.ProgressSince(after.Revision + 1); err == nil {
		t.Fatal("future cursor accepted")
	}
}

func TestOperationDurationIncludesWaitAndMissingIsUnavailable(t *testing.T) {
	job := t.TempDir()
	now := time.Now().UTC()
	if value, _ := operationDuration(job, now); value != nil {
		t.Fatal("missing start filled with zero")
	}
	for _, stopped := range []bool{false, true} {
		process := codexProcess{StartedAt: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Stopped: stopped}
		if stopped {
			process.CompletedAt = now.Format(time.RFC3339Nano)
		}
		if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
			t.Fatal(err)
		}
		duration, _ := operationDuration(job, now)
		if duration == nil || *duration != 2000 {
			t.Fatalf("wall wait lost: %v", duration)
		}
	}
}

func TestPromptKeepsCompleteTextAndCurrentBindingWithoutDerivedDuplication(t *testing.T) {
	for _, candidate := range []string{"candidate-a", "candidate-b"} {
		requirement := protocol.RequirementDocument{Path: "requirement.md", Revision: "revision", Text: "Full REQ and AC text", Confirmed: true, Projection: &requirements.FrozenRequirementProjection{}}
		input := map[string]any{"requirement": requirement, "binding": protocol.ExecutionClaim{Candidate: candidate, Identity: "same-native"}}
		prompt, err := assemblePrompt("instructions", input)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, requirement.Text) || !strings.Contains(prompt, candidate) || !strings.Contains(prompt, "same-native") {
			t.Fatalf("context lost: %s", prompt)
		}
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(prompt, "instructions\n\n")), &parsed); err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		_ = json.Unmarshal(parsed["requirement"], &doc)
		if _, ok := doc["projection"]; ok {
			t.Fatal("duplicated projection")
		}
		if input["requirement"] != requirement {
			t.Fatal("authority input mutated")
		}
	}
}

func TestStaticCheckerDoesNotExecuteInspectedScripts(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	checker, err := filepath.Abs("../../../scripts/qa-static-check.py")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "side-effect")
	py := filepath.Join(dir, "helper.py")
	sh := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(py, []byte("from pathlib import Path\nPath("+"'"+sentinel+"'"+").write_text('executed')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sh, []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(python, checker, py, sh).CombinedOutput(); err != nil {
		t.Fatalf("static check: %s %v", output, err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("inspection executed script")
	}
	if err := os.WriteFile(py, []byte("if broken syntax\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(python, checker, py).CombinedOutput(); err == nil || !strings.Contains(string(output), "FAIL") {
		t.Fatalf("invalid script accepted: %s %v", output, err)
	}
}

func TestQAReadinessInstructionsReachBothRoles(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox", "whitebox"})
	x := facadeExecution(t, f, "blackbox-design")
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	run.Regular.Preparation.Resources["blackbox"] = t.TempDir()
	run.Regular.Preparation.Resources["whitebox"] = t.TempDir()
	for _, step := range []string{"blackbox-design", "whitebox-design", "blackbox-review", "whitebox-review"} {
		claim := x.Claim
		claim.Task.Step = authoring.StepID(step)
		prompt, _, err := preparationPrompt(run, claim)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"前态", "取证", "静态检查"} {
			if !strings.Contains(prompt, text) {
				t.Fatalf("%s missing readiness material %q", step, text)
			}
		}
		if strings.HasSuffix(step, "review") && !strings.Contains(prompt, "先 Seal/abort") {
			t.Fatalf("%s omits concrete prestate conflict", step)
		}
	}
}

func TestProgressRepeatReasonsBindActualCandidateAndNeverInventReason(t *testing.T) {
	first := protocol.TaskOperation{ID: "first", ClaimedRevision: 1, Request: protocol.TaskOperationRequest{Kind: "qa.target", CaseID: "case", Binding: protocol.ExecutionClaim{Candidate: "a"}}, Receipt: &protocol.TaskOperationReceipt{Status: "COMPLETED"}}
	second := first
	second.ID = "second"
	second.ClaimedRevision = 2
	second.Request.Binding.Candidate = "b"
	run := Run{Executions: map[string]protocol.RegularExecution{"action": {Operations: map[string]protocol.TaskOperation{"first": first, "second": second}}}}
	rows := repeatedOperations(run)
	if len(rows) != 1 || !strings.Contains(rows[0].Reason, "a -> b") {
		t.Fatalf("candidate change not traceable: %+v", rows)
	}
	second.Request.Binding.Candidate = "a"
	run.Executions["action"].Operations["second"] = second
	rows = repeatedOperations(run)
	if len(rows) != 1 || !strings.HasPrefix(rows[0].Reason, "unavailable:") {
		t.Fatalf("invented reason: %+v", rows)
	}
}

func TestQAReadinessTerminalPrestateCannotReachTarget(t *testing.T) {
	f, action := operationFixture(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	regular := *run.Regular
	regular.Terminal = "SEALED"
	snapshot := &protocol.State{RunID: run.RunID, Regular: &regular, RegularExecutions: run.Executions}
	binding := run.Executions[action].Claim
	binding.Identity = run.Executions[action].Identity
	if _, err := protocol.PendingQA(snapshot, binding, ""); err == nil || !strings.Contains(err.Error(), "EVENT_NOT_CURRENT") {
		t.Fatalf("terminal prestate appeared reachable: %v", err)
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != run.Revision || len(after.Executions[action].Operations) != len(run.Executions[action].Operations) {
		t.Fatal("readiness probe executed a target")
	}
}

func TestProgressCoverageLifecycle(t *testing.T) {
	f, action := operationFixture(t)
	job := f.codexJob(action)
	coverage := filepath.Join(job, "coverage")
	if err := os.MkdirAll(coverage, 0700); err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(calls int, operations []string, missing, evidence, stage string) {
		t.Helper()
		p, err := f.ProgressSince(0)
		if err != nil {
			t.Fatal(err)
		}
		if p.Revision != run.Revision || p.NativeCalls != calls || len(p.Messages) != len(operations) {
			t.Fatalf("unexpected progress: %+v", p)
		}
		for i, operation := range operations {
			m := p.Messages[i]
			if m.Operation != operation || m.ActionID != action || m.Identity != run.Executions[action].Identity || m.Candidate != run.Executions[action].Claim.Candidate {
				t.Fatalf("message binding/type lost: %+v", m)
			}
			if data, err := os.ReadFile(m.Path); err != nil || len(data) == 0 {
				t.Fatalf("actual prompt inaccessible: %+v, %v", m, err)
			}
		}
		var missingSources []string
		for _, path := range p.NativeCallSourcesUnavailable {
			if strings.HasPrefix(path, job+string(filepath.Separator)) {
				missingSources = append(missingSources, path)
			}
		}
		if missing == "" {
			if len(missingSources) != 0 {
				t.Fatalf("false missing source: %v", missingSources)
			}
		} else if len(missingSources) != 1 || missingSources[0] != missing {
			t.Fatalf("missing source mismatch: %v", missingSources)
		}
		if len(p.Current) != 1 || p.Current[0].Evidence != evidence || p.Current[0].Stage != stage {
			t.Fatalf("current invocation mismatch: %+v", p.Current)
		}
		if calls > 0 && p.Current[0].DurationMillis == nil {
			t.Fatalf("invocation timing unavailable: %+v", p.Current)
		}
	}
	check(0, nil, filepath.Join(job, "process.json"), job, "native wait")
	write(coverage, "prompt.txt", "independent whitebox coverage input")
	check(0, []string{"coverage.spawn"}, filepath.Join(coverage, "process.json"), coverage, "whitebox.coverage")
	process := codexProcess{ActionID: action, StartedAt: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}
	if err := writeJSONAtomic(filepath.Join(coverage, "process.json"), process); err != nil {
		t.Fatal(err)
	}
	check(1, []string{"coverage.spawn"}, "", coverage, "whitebox.coverage")
	process.Stopped = true
	process.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeJSONAtomic(filepath.Join(coverage, "process.json"), process); err != nil {
		t.Fatal(err)
	}
	check(1, []string{"coverage.spawn"}, "", coverage, "whitebox.coverage result handling")
	write(job, "prompt.txt", "main whitebox design input")
	process.Stopped = false
	process.CompletedAt = ""
	if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
		t.Fatal(err)
	}
	check(2, []string{"coverage.spawn", "resume"}, "", job, "native wait")
}

func TestNativeProgressCoverageOnContinuation(t *testing.T) {
	base := t.TempDir()
	resume := codexResumeJob(base, 1)
	coverage := filepath.Join(resume, "coverage")
	if err := os.MkdirAll(coverage, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(base, "process.json"), filepath.Join(coverage, "prompt.txt"), filepath.Join(coverage, "process.json")} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	jobs := nativeProgressJobs(base, 1)
	if len(jobs) != 3 || jobs[0].operation != "spawn" || jobs[1].operation != "coverage.resume" || jobs[1].job != coverage || !jobs[1].process || jobs[2].expected {
		t.Fatalf("coverage continuation omitted or unstarted main expected: %+v", jobs)
	}
	if err := os.WriteFile(filepath.Join(resume, "process.json"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	jobs = nativeProgressJobs(base, 1)
	if !jobs[2].process || !jobs[2].expected || jobs[2].operation != "resume" {
		t.Fatalf("main continuation not projected: %+v", jobs)
	}
	// A later ordinary continuation must not inherit the earlier coverage call.
	jobs = nativeProgressJobs(base, 2)
	if len(jobs) != 4 || jobs[3].coverage || jobs[3].operation != "resume" {
		t.Fatalf("coverage leaked into subsequent continuation: %+v", jobs)
	}
}
