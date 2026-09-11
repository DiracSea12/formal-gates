package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func recordCoverage(t *testing.T, e *Engine, x RegularExecution) {
	t.Helper()
	snap, _ := e.Load()
	bound := snap.State.RegularExecutions[x.Claim.ActionID].Claim
	bound.Identity = snap.State.RegularExecutions[x.Claim.ActionID].Identity
	q := TaskOperationRequest{RunID: snap.State.RunID, ExpectedRevision: snap.Revision, Binding: bound, RequestID: "coverage", Kind: "whitebox.coverage", Prompt: `{"coverageJudgment":"independent behavior coverage","behaviorOracles":[{"behavior":"hello command","oracle":"stdout exactly hello"}]}`}
	submitRegular(t, e, RegularEvent{Operation: "TASK_OPERATION_CLAIM", TaskOperation: &q})
	receipt := TaskOperationReceipt{ID: TaskOperationID(q), ActionID: bound.ActionID, RequestDigest: TaskOperationDigest(q), Status: "COMPLETED", Output: q.Prompt}
	submitRegular(t, e, RegularEvent{Operation: "TASK_OPERATION_RECEIPT", TaskOperationReceipt: &receipt})
}

func TestRepairCheckpointBlocksCompletedSideEffectsAndAllowsInheritedDependency(t *testing.T) {
	for _, status := range []string{"PASS", "FAIL"} {
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			t.Run(status+kind, func(t *testing.T) {
				e := verificationEngine(t)
				x := currentExecution(t, e, "blackbox-execution")
				claimAndAttach(t, e, x)
				snap, _ := e.Load()
				s := snap.State
				bound := s.RegularExecutions[x.Claim.ActionID].Claim
				bound.Identity = s.RegularExecutions[x.Claim.ActionID].Identity
				w := currentWave(s.Regular)
				w.Checkpoints["blackbox"] = VerificationResult{Digest: "saved", Cases: []CaseExecution{{CaseID: "BLACKBOX:C1", Status: status}}}
				q := TaskOperationRequest{RunID: s.RunID, ExpectedRevision: snap.Revision, Binding: bound, RequestID: "repeat", Kind: kind, CaseID: "BLACKBOX:C1"}
				before, _ := json.Marshal(s.RegularExecutions[bound.ActionID].Operations)
				err := e.claimTaskOperation(s, q, snap.Revision)
				if err == nil || !strings.Contains(err.Error(), "DUPLICATE_ACTION") {
					t.Fatalf("repeat %s admitted: %v", kind, err)
				}
				after, _ := json.Marshal(s.RegularExecutions[bound.ActionID].Operations)
				if string(before) != string(after) {
					t.Fatal("rejection changed state before side effect")
				}
			})
		}
	}
	e := verificationEngine(t)
	x := currentExecution(t, e, "blackbox-execution")
	claimAndAttach(t, e, x)
	snap, _ := e.Load()
	s := snap.State
	bound := s.RegularExecutions[x.Claim.ActionID].Claim
	bound.Identity = s.RegularExecutions[x.Claim.ActionID].Identity
	s.Regular.Preparation.QA["blackbox"].Design.Manifest.Cases[0].Dependencies = []string{"BLACKBOX:C0"}
	waveTarget(currentWave(s.Regular), "blackbox").Inherited["BLACKBOX:C0"] = CaseExecution{CaseID: "BLACKBOX:C0", Status: "PASS"}
	q := TaskOperationRequest{RunID: s.RunID, ExpectedRevision: snap.Revision, Binding: bound, RequestID: "dependent", Kind: "qa.prepare", CaseID: "BLACKBOX:C1"}
	if err := e.claimTaskOperation(s, q, snap.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestRepairRevisionKeepsUnaffectedReviewAcrossFailAndRepeatedRevision(t *testing.T) {
	q := &PreparedQA{Design: &QADesign{}, Snapshot: "Q", Review: &coverage.ReviewResult{CaseDecisions: []coverage.ReviewDecision{{ID: "A", Status: coverage.StatusFail}}}, RetainedReview: &coverage.ReviewResult{CaseDecisions: []coverage.ReviewDecision{{ID: "B", Status: coverage.StatusPass}, {ID: "C", Status: coverage.StatusPass}}}}
	mergeQAReview(q.Review, *q.RetainedReview)
	reviseQA(q)
	reviseQA(q)
	if q.PreviousDesign == nil || q.RetainedReview == nil || len(q.RetainedReview.CaseDecisions) != 3 || q.Revision != 1 {
		t.Fatalf("lost approvals: %+v", q)
	}
}

func TestRepairMixedFailureRetainsOnlyUnaffectedPasses(t *testing.T) {
	e := verificationEngine(t)
	snap, _ := e.Load()
	r := snap.State.Regular
	w := currentWave(r)
	w.Results["blackbox"] = VerificationResult{Status: "FAIL", Cases: []CaseExecution{{CaseID: "BLACKBOX:C1", Status: "FAIL"}, {CaseID: "BLACKBOX:C2", Status: "PASS"}}}
	delete(r.Verification.Effective, "blackbox")
	candidate := *r.Preparation.Candidate
	candidate.Candidate.Identity = "repaired"
	freezeWave(r, &candidate, []string{"BLACKBOX:C1"})
	target := waveTarget(currentWave(r), "blackbox")
	if target.Inherited["BLACKBOX:C2"].Status != "PASS" || target.Inherited["BLACKBOX:C1"].Status != "" {
		t.Fatalf("inheritance=%+v", target.Inherited)
	}
}

func TestRepairEvidenceRejectsTemplateAndAllowsActuallyBlockedWorkspace(t *testing.T) {
	e := verificationEngine(t)
	snap, _ := e.Load()
	r := snap.State.Regular
	x := currentExecution(t, e, "blackbox-execution")
	result := verificationResult(x)
	result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
	result.Cases[0].Output = "符合预期"
	if err := validateVerificationResult(r, x, result, false); err == nil {
		t.Fatal("template evidence accepted")
	}
	claimAndAttach(t, e, x)
	result.Status = "CHECKPOINT"
	result.Message = "dependency failed"
	result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "FAIL"), executedCase("BLACKBOX:C2", "BLOCKED")}
	result.Cases[1].Workspace = ""
	result.Cases[1].Dependencies = []string{"BLACKBOX:C1"}
	waveTarget(currentWave(r), "blackbox").Expected = []string{"BLACKBOX:C1", "BLACKBOX:C2"}
	recordQAOperations(t, e, x, result.Cases)
	latest, _ := e.Load()
	x = latest.State.RegularExecutions[x.Claim.ActionID]
	if err := validateVerificationResult(r, x, result, true); err != nil {
		t.Fatal(err)
	}
}

// These protocol fixtures supply adapter receipts through the real operation
// admission events; they do not claim to execute a native target process.
func recordQAOperations(t *testing.T, e *Engine, x RegularExecution, rows []CaseExecution) {
	t.Helper()
	for _, c := range rows {
		if c.Status == "BLOCKED" {
			continue
		}
		snap, _ := e.Load()
		live := snap.State.RegularExecutions[x.Claim.ActionID]
		if findTaskOperation(live, "qa.collect", c.CaseID) != nil {
			continue
		}
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			snap, _ := e.Load()
			live = snap.State.RegularExecutions[x.Claim.ActionID]
			binding := live.Claim
			binding.Identity = live.Identity
			q := TaskOperationRequest{RunID: snap.State.RunID, ExpectedRevision: snap.Revision, Binding: binding, RequestID: kind + c.CaseID, Kind: kind, CaseID: c.CaseID}
			submitRegular(t, e, RegularEvent{Operation: "TASK_OPERATION_CLAIM", TaskOperation: &q})
			receipt := TaskOperationReceipt{ID: TaskOperationID(q), ActionID: binding.ActionID, RequestDigest: TaskOperationDigest(q), Status: "COMPLETED", Workspace: c.Workspace, Output: c.Output}
			submitRegular(t, e, RegularEvent{Operation: "TASK_OPERATION_RECEIPT", TaskOperationReceipt: &receipt})
		}
	}
}

func TestRepairQARequiresActualOperationsAndRuntimeFailureStaysRetryable(t *testing.T) {
	for _, failed := range []bool{false, true} {
		e := verificationEngine(t)
		x := currentExecution(t, e, "blackbox-execution")
		claimAndAttach(t, e, x)
		result := verificationResult(x)
		result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
		if failed {
			recordQAOperations(t, e, x, result.Cases)
			snap, _ := e.Load()
			state := snap.State
			live := state.RegularExecutions[x.Claim.ActionID]
			op := findTaskOperation(live, "qa.target", "BLACKBOX:C1")
			op.Receipt.Status = "FAILED"
			op.Receipt.ExitCode = -1
			op.Receipt.Message = "executable missing"
			live.Operations[op.ID] = *op
			state.RegularExecutions[x.Claim.ActionID] = live
			result.Status = "FAIL"
			result.Cases[0].Status = "FAIL"
			status, err := e.acceptVerification(state, result, snap.Revision)
			if err != nil || status != "REJECTED_RESULT" || !strings.Contains(state.Regular.VerificationErrors[0].Reason, "RUNTIME_ERROR") {
				t.Fatalf("%s %v", status, err)
			}
			if len(currentWave(state.Regular).Results) != 0 {
				t.Fatal("invocation error became semantic result")
			}
		} else {
			if a := submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result}); a.Status != "REJECTED_RESULT" {
				t.Fatal(a)
			}
			snap, _ := e.Load()
			if len(currentWave(snap.State.Regular).Results) != 0 {
				t.Fatal("unexecuted PASS accepted")
			}
		}
	}
}
func TestRepairProductAdvisoriesRequirePass(t *testing.T) {
	f := ReviewFinding{ID: "minor", Severity: "P2", Problem: "minor text ambiguity", NormalPath: "read complete requirement", Consequence: "minor clarification needed", SeverityRationale: "core behavior covered", Remedy: "clarify wording"}
	v := RawReviewCandidate{Action: "product-review", Status: "FAIL", Complete: true, Message: "complete document inspected", Findings: []ReviewFinding{f}}
	if validateReview(nil, v) == nil {
		t.Fatal("advisories incorrectly blocked")
	}
	v.Status = "PASS"
	if err := validateReview(nil, v); err != nil {
		t.Fatal(err)
	}
}
func TestRepairRouteAddKeepsExistingQA(t *testing.T) {
	e := verificationEngine(t)
	snap, _ := e.Load()
	r := snap.State.Regular
	q := RegularRequest{Action: "route-add", Route: &RegularRoute{Kind: "custom", QA: append([]string{}, r.Route.QA...), Gates: append(append([]string{}, r.Route.Gates...), "complexity-gate")}, Reason: "add independent gate"}
	if err := validateRouteChangeAfterDevelopment(snap.State, q); err != nil {
		t.Fatal(err)
	}
	regularChoice(t, e, q, "confirm")
	snap, _ = e.Load()
	if len(snap.State.Regular.Route.QA) != 1 || len(snap.State.Regular.Route.Gates) != 2 {
		t.Fatal("lost existing obligations")
	}
}
func TestRepairPlanChangeRetiresStoppedClaim(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	x := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, x)
	sourceUnavailable(t, e, x)
	regularChoice(t, e, RegularRequest{Action: "plan-change", Reason: "interface contract changed"}, "confirm")
	snap, _ := e.Load()
	if !snap.State.RegularExecutions[x.Claim.ActionID].Finished {
		t.Fatal("stale stopped execution occupies capacity")
	}
	review := currentExecution(t, e, "start-readiness")
	claimAndAttach(t, e, review)
	v := resultFor(review)
	v.GranularityReview = planForReview(review.Claim.RequirementRevision)
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
	sourceUnavailable(t, e, review)
	adjudicatePass(t, e, "start-readiness")
	snap, _ = e.Load()
	regularChoice(t, e, RegularRequest{Action: "topology", PlanDigest: snap.State.Regular.Plan.Digest, Reason: "updated plan"}, "confirm")
	next := currentExecution(t, e, "development-worker")
	if next.Identity != "native-"+x.Claim.ActionID {
		t.Fatal("plan change lost original developer")
	}
}
