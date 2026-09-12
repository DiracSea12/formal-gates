package facade

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

// This is a local protocol fixture, not independent product/technical/QA PASS.
// It proves a new platform inherits the existing workflow transitions without
// implementing any of those transitions in its adapter.
func TestNativePlatformDevelopmentResumeAndWhiteboxPreflight(t *testing.T) {
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	a := &fixtureRuntime{id: "cycle-fixture-" + suffix, executableOption: "cycleExecutable" + suffix, stateOption: "cycleState" + suffix, serverVersion: 1, adapterVersion: 1}
	if err := registerNativePlatform(a.descriptor(), a); err != nil {
		t.Fatal(err)
	}
	f := startPlatformFixture(t, a)
	developmentTurns, coverageTurns, authorTurns := 0, 0, 0
	var coverageDirectory, authorDirectory string
	a.answer = func(q nativeTurn) any {
		c := q.Claim
		if q.NoTools {
			if c.Task.Step != "whitebox-design" {
				t.Fatal("unexpected preflight")
			}
			coverageTurns++
			coverageDirectory = q.Job
			return map[string]any{"coverageJudgment": "local fixture checks exact hello output", "behaviorOracles": []map[string]string{{"behavior": "hello command", "oracle": "exit zero and stdout exactly hello"}}}
		}
		if c.Task.Step == "product-review" || c.Task.Step == "start-readiness" {
			result := protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "Explicit local protocol fixture, not a semantic review", Findings: []protocol.ReviewFinding{}}
			if c.Task.Step == "start-readiness" {
				result.GranularityReview = &protocol.DevelopmentPlan{RequirementRevision: c.RequirementRevision, Rationale: "one local command fixture", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "Create hello command", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "Create and execute command"}}, Dependencies: []string{}, Interfaces: "stdout", StateBoundary: "local files only", Risks: "wrong output", DefinitionOfDone: "hello command exits successfully", Validation: "go run .", Recovery: "reuse original identity", Handoff: "whitebox after code", ContextCost: "one tiny module"}}}
			}
			return result
		}
		r := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "PASS", Message: "Local protocol fixture with actual tiny Go execution", Manifest: []string{}, Handoff: "No unresolved fixture work"}
		switch c.Task.Step {
		case "development-worker":
			developmentTurns++
			if developmentTurns == 1 {
				r.Status = "CONTINUE"
				r.Message = "Explicit local fixture continuation; S1 remains"
				return r
			}
			if !q.Resume || c.Identity == "" {
				t.Fatal("continuation lost original native session")
			}
			writePreparationFile(t, q.Workspace, "go.mod", "module example.invalid/platform-fixture\n\ngo 1.22\n")
			writePreparationFile(t, q.Workspace, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = q.Workspace
			output, err := cmd.CombinedOutput()
			if err != nil || string(output) != "hello" {
				t.Fatalf("fixture command: %q %v", output, err)
			}
			r.Manifest = []string{"go.mod", "main.go"}
			r.Validation = []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "go run .", Observation: "exit zero and stdout exactly hello", Result: "PASS"}}
		case "whitebox-design":
			current, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			if !protocol.WhiteboxCoverageReady(current.Executions[c.ActionID]) || coverageTurns != 1 || !q.Resume {
				t.Fatal("authoring ran without the engine-owned preflight and same-session resume")
			}
			authorTurns++
			authorDirectory = q.Job
			if filepath.Dir(coverageDirectory) != authorDirectory {
				t.Fatal("preflight and authoring do not have distinct owned directories")
			}
			if _, err := os.Stat(filepath.Join(q.Job, fixturePrivateArtifact(1))); !os.IsNotExist(err) {
				t.Fatal("preflight private state leaked into the authoring invocation")
			}
			writePreparationFile(t, q.Workspace, "main_test.go", "package main\nimport (\"testing\";\"os/exec\")\nfunc TestHello(t *testing.T){out,err:=exec.Command(\"go\",\"run\",\".\").CombinedOutput();if err!=nil||string(out)!=\"hello\"{t.Fatalf(\"%s %v\",out,err)}}\n")
			cmd := exec.Command("go", "test", "-run", "^TestHello$", ".")
			cmd.Dir = q.Workspace
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture regression: %s %v", output, err)
			}
			design := facadeDesign(current, "whitebox")
			r.Design = &design
			r.Manifest = []string{"main_test.go"}
			r.Reuse = []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "local exact output assertion", Procedure: "go test -run ^TestHello$ .", Observation: string(output), Comparison: "exact hello and exit zero", Result: "EXECUTED_PASS"}}
		default:
			t.Fatalf("unexpected local fixture action %s", c.Task.Step)
		}
		return r
	}
	for _, step := range []string{"product-review", "start-readiness"} {
		x := facadeExecution(t, f, step)
		run, err := f.ExecuteNative(x.Claim.ActionID, false)
		if err != nil {
			t.Fatal(err)
		}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: step, ResultDigest: run.Regular.Reviews[step].CurrentDigest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "Explicit fixture setup, not independent review evidence"}})
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", PlanDigest: run.Regular.Plan.Digest, Reason: "single tiny local fixture"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "custom", QA: []string{"whitebox"}, Gates: []string{"implementation-quality-gate"}}})
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	first := facadeExecution(t, f, "development-worker")
	run, err = f.ExecuteNative(first.Claim.ActionID, false)
	if err != nil {
		t.Fatal(err)
	}
	original := run.Executions[first.Claim.ActionID].Identity
	next := facadeExecution(t, f, "development-worker")
	if next.Claim.Operation != "resume" || next.Identity != original {
		t.Fatal("engine did not retain developer identity")
	}
	run, err = f.ExecuteNative(next.Claim.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := run.Regular.Preparation.Batches["B1"]; !ok {
		t.Fatalf("developer completion not accepted: turns=%d errors=%+v", developmentTurns, run.Regular.PreparationErrors)
	}
	// Batch commit and whitebox workspace are separate engine-owned Git intents.
	// Consume those mechanical intents; do not manufacture an authoring action.
	for i := 0; i < 3 && run.Regular.Preparation.Resources["whitebox"] == ""; i++ {
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
	}
	wb := facadeExecution(t, f, "whitebox-design")
	run, err = f.ExecuteNative(wb.Claim.ActionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if developmentTurns != 2 || coverageTurns != 1 || authorTurns != 1 || run.Regular.Preparation.QA["whitebox"].Design == nil {
		t.Fatalf("common stages not completed: dev=%d coverage=%d author=%d errors=%+v", developmentTurns, coverageTurns, authorTurns, run.Regular.PreparationErrors)
	}
	for _, invocation := range []struct{ directory, resume string }{
		{coverageDirectory, ""}, {authorDirectory, run.Executions[wb.Claim.ActionID].Identity},
	} {
		private, err := os.ReadFile(filepath.Join(invocation.directory, fixturePrivateArtifact(1)))
		if err != nil || !strings.HasSuffix(string(private), "\nresume="+invocation.resume) {
			t.Fatalf("opaque invocation artifact was lost or overwritten: %s %v", private, err)
		}
	}
	if _, err = f.ExecuteNative(wb.Claim.ActionID, false); err == nil {
		t.Fatal("completed whitebox action repeated")
	}
	t.Log(fmt.Sprintf("one adapter: developer CONTINUE/resume, %d automatic preflight, %d authoring turn, result acceptance; no model calls", coverageTurns, authorTurns))
}
