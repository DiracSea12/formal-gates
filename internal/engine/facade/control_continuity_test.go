package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestContinuousPendingControlResumesCommitsAndFillsSuccessor(t *testing.T) {
	for _, control := range []string{"qa-revise", "qa-recovery", "qa-review-fail"} {
		t.Run(control, func(t *testing.T) {
			b2 := protocol.DevelopmentBatch{ID: "B2", Task: "second command", Subtasks: []protocol.DevelopmentSubtask{{ID: "S2", Task: "execute second command"}}, Dependencies: []string{"B1"}, Interfaces: "stdout", StateBoundary: "local", Risks: "output", DefinitionOfDone: "second", Validation: "sh second.sh", Recovery: "same identity", Handoff: "none", ContextCost: "tiny"}
			f, a := continuousPreparationFixture(t, b2)
			turns := 0
			a.answer = func(q nativeTurn) any {
				c := q.Claim
				r := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "PASS", Message: "actual shell fixture", Manifest: []string{}, Handoff: "none"}
				if c.Task.Step != "development-worker" {
					if control == "qa-review-fail" {
						current, err := f.Show()
						if err != nil {
							t.Fatal(err)
						}
						if c.Task.Step == "blackbox-design" {
							r.Status = "DRAFT"
							design := facadeDesign(current, "blackbox")
							design.Manifest.Cases[0].Steps = fmt.Sprintf("execute command with revised comparison %d", current.Regular.Preparation.QA["blackbox"].Revision)
							r.Design = &design
						} else {
							r.Status = "FAIL"
							r.Review = facadeReview(current, "blackbox")
							r.Review.Scope = current.Regular.Preparation.QA["blackbox"].ReviewScope
							r.Review.SetStatus = coverage.StatusFail
							r.Review.CaseDecisions[0].Status = coverage.StatusFail
							r.Message = "fixture review rejects missing comparison"
						}
					} else {
						r.Status = "RUNTIME_ERROR"
					}
					return r
				}
				turns++
				if turns == 1 {
					r.Status = "CONTINUE"
					r.Message = "command implementation remains"
					return r
				}
				if turns == 2 && (!q.Resume || c.Identity == "") {
					t.Error("continuation did not resume original identity")
				}
				file, word, sub := "first.sh", "first", "S1"
				if turns == 3 {
					file, word, sub = "second.sh", "second", "S2"
				}
				writePreparationFile(t, q.Workspace, file, "printf "+word+"\n")
				cmd := exec.Command("sh", file)
				cmd.Dir = q.Workspace
				out, err := cmd.CombinedOutput()
				if err != nil || string(out) != word {
					t.Errorf("actual command %q: %v", out, err)
				}
				r.Manifest = []string{file}
				r.Validation = []protocol.BatchValidation{{SubtaskID: sub, Procedure: "sh " + file, Observation: string(out), Result: "PASS"}}
				return r
			}
			first := facadeExecution(t, f, "development-worker")
			if _, err := f.ExecuteNative(first.Claim.ActionID, false); err != nil {
				t.Fatal(err)
			}
			next := facadeExecution(t, f, "development-worker")
			if next.Claimed || next.Claim.Operation != "resume" {
				t.Fatal("expected unclaimed CONTINUE attempt")
			}
			if control == "qa-revise" {
				preparationSubmit(t, f, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: control, Target: "blackbox", Reason: "revise independent executable coverage"}})
			} else {
				for i := 0; i < 10; i++ {
					run, err := f.Drive()
					if err != nil {
						t.Fatal(err)
					}
					if run.Regular.Ask != nil {
						break
					}
					step := "blackbox-design"
					if run.Regular.Preparation.QA["blackbox"].Design != nil {
						step = "blackbox-review"
					}
					x := facadeExecution(t, f, step)
					if _, err = f.ExecuteNative(x.Claim.ActionID, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			expectedControl := control
			if control == "qa-review-fail" {
				expectedControl = "qa-recovery"
			}
			if before.Regular.Ask == nil || before.Regular.Ask.Kind != expectedControl {
				t.Fatalf("missing public control: %+v", before.Regular.Ask)
			}
			if control == "qa-review-fail" && before.Regular.Ask.QA.ConsecutiveFailures != 3 {
				t.Fatalf("semantic failures not retained: ask=%+v errors=%+v", before.Regular.Ask.QA, before.Regular.PreparationErrors)
			}
			if control == "qa-revise" {
				blocked := facadeExecution(t, f, "blackbox-design")
				if _, err := f.ExecuteNative(blocked.Claim.ActionID, false); err == nil || !strings.Contains(err.Error(), "EVENT_NOT_CURRENT") {
					t.Fatalf("affected action started: %v", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			result, err := f.DriveContinuous(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if turns != 3 || result.Boundary != "ASK" || result.Run.Regular.Ask == nil || result.Run.Regular.Ask.ID != before.Regular.Ask.ID {
				t.Fatalf("turns=%d boundary=%s blocked=%v ask=%+v", turns, result.Boundary, result.Blocked, result.Run.Regular.Ask)
			}
			p := result.Run.Regular.Preparation
			if p.Commits["B1"].After == "" || p.Commits["B2"].After == "" {
				t.Fatalf("commits missing: %+v", p.Commits)
			}
			for _, x := range result.Run.Executions {
				if x.Claim.Task.Step == "development-worker" && (!x.Finished || x.Source == nil || x.Source.Status == "RUNNING") {
					t.Fatalf("unsettled development: %+v", x)
				}
			}
			t.Log("public control preserved; CONTINUE resumed, two actual shell commands ran, both batches committed and sources settled")
		})
	}
}

func TestProgressPendingInputJudgmentAndCompletion(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) { m.Cases[0].Command = nil })
	x := facadeExecution(t, f, "blackbox-execution")
	nativeFixtureClaim(t, f, x)
	action := x.Claim.ActionID
	check := func(remaining, input, judgment int) {
		t.Helper()
		p, err := f.ProgressSince(0)
		if err != nil {
			t.Fatal(err)
		}
		v := p.PendingCases[action]
		if v.Remaining != remaining || v.AwaitingInput != input || v.AwaitingJudgment != judgment {
			t.Fatalf("projection: %+v", v)
		}
	}
	check(1, 0, 0)
	batch := runBatch(t, f, action)
	if len(batch.Pending.Input) != 1 {
		t.Fatalf("missing input: %+v", batch.Pending)
	}
	check(1, 1, 0)
	q := operationRequest(t, f, action, "qa.target", "provide-command")
	q.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", "printf actual"}}
	if _, err := f.ExecuteTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	q = operationRequest(t, f, action, "qa.collect", "collect-command")
	if _, err := f.ExecuteTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	check(1, 0, 1)
	result := batchResult(t, f, action, "CHECKPOINT", []string{"BLACKBOX:C1"}, []string{})
	delta, _ := json.Marshal(map[string]any{"cases": result.Cases, "remaining": result.Remaining, "message": result.Message})
	q = operationRequest(t, f, action, "qa.checkpoint", "judge-command")
	q.CaseID = ""
	q.Prompt = string(delta)
	if _, err := f.RunTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
	check(0, 0, 0)
}
