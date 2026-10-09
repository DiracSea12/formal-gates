package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"testing"
)

// The setup helper submits normal facade events; the oracle is G AC-017.
func TestAcceptedAdjudicationRemainsCorrectable(t *testing.T) {
	d, finding, wave := validationWithGateFinding(t)
	resolveGateFinding(d, finding, wave)
	old := d.run.Validation.Findings[finding].History[0]
	old.Decision = "DISMISSED"
	old.ProblemID = ""
	old.Supersedes = 1
	old.Reason = "Independent reproduction confirms the documented output permits either order"
	old.RootCause = "initial operator classification used a stronger order rule than the requirement"
	old.Consequence = "candidate is correct; the report is not a repair obligation"
	t.Logf("before correction wave=%s history=%d repair=%+v", d.run.Validation.Waves[0].Status, len(d.run.Validation.Findings[finding].History), d.run.Validation.Waves[0].Repair)
	_, _, err := d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{old}}}})
	if err != nil {
		t.Fatalf("G AC-017 requires an audit-preserving correction after accepted adjudication; normal facade rejected it: %v", err)
	}
}

// C REQ-001 allows unchanged shared instructions to retain their approval.
func TestUnchangedSharedApprovalInheritance(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	m.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "initialize a clean project once"}}
	for i := range m.Cases {
		m.Cases[i].SharedSteps = []string{"setup"}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	r.SharedStepDecisions = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "Shared initialization is available and produces the required clean project"}}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "clarify invalid input expected diagnostic"}}})
	d.drive()
	sg := d.run.Validation.Sets[coverage.ReviewBlackbox].Suggestions[0]
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "case clarification", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: sg.ID, Decision: "ACCEPT", Reason: "clarify this case only", AffectedCases: []string{"invalid"}}}})
	d.drive()
	id, in = d.action("engine.qa.design")
	m = in.Manifest
	m.Cases[1].Oracle = "rejects invalid input with the documented diagnostic"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	r = approvedReview(m)
	r.Scope = coverage.ScopeAffected
	r.SharedStepDecisions = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "Shared initialization is available and produces the required clean project", Inherited: true}}
	d.receipt(id)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "unchanged-shared-inheritance", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r}}})
	t.Logf("acceptance=%s rejected=%+v", a.Status, d.run.RejectedResults)
	if a.Status != "ACCEPTED" {
		t.Fatalf("C REQ-001 unchanged shared approval cannot inherit; status=%s", a.Status)
	}
	if len(in.InheritableSharedReviews) != 1 || in.InheritableSharedReviews[0].ID != "setup" || in.InheritableSharedReviews[0].Reason != r.SharedStepDecisions[0].Reason {
		t.Fatal("input lost original shared approval and rationale")
	}
	d.reopen()
	if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
		t.Fatal("inherited shared approval did not freeze the reviewed set")
	}
}

func TestResolvedProblemDoesNotReopenRepair(t *testing.T) {
	d, finding, wave := validationWithGateFinding(t)
	resolveGateFinding(d, finding, wave)
	original := d.run.Validation.Findings[finding].History[0]
	id, in := d.action("engine.repair")
	repair := gateRepairResult(in)
	d.business(id, repair)
	d.verifyRepair(repair)
	d.materialize()
	d.materialize()
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
	for i := 0; i < 2; i++ {
		id, in = d.action("engine.qa.execution")
		r := executionFor(in)
		d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r}))
		d.drive()
	}
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if current.Status != "COMPLETE" {
		t.Fatalf("resolved baseline not complete: %s", current.Status)
	}
	unlockRouteByRequirementRedo(d)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.drive()
	id, in = d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "duplicate report of the ordering defect already fixed in this candidate"}}})
	d.drive()
	current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var fid string
	for key, f := range d.run.Validation.Findings {
		if f.WaveID == current.ID {
			fid = key
		}
	}
	if fid == "" {
		t.Fatal("missing duplicate finding")
	}
	original.FindingID = fid
	original.Decision = "SAME_AS"
	original.Supersedes = 0
	original.Reason = "independent reproduction confirms existing repair has resolved this exact problem; duplicate report"
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: current.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{original}}}})
	d.drive()
	current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	t.Logf("after SAME_AS resolved problem status=%s repair=%+v", current.Status, current.Repair)
	if current.Status == "REPAIR" {
		t.Fatal("G AC-014: SAME_AS resolved problem created another repair obligation")
	}
}

func TestAdjudicationRevisionConflict(t *testing.T) {
	d, finding, wave := validationWithGateFinding(t)
	r := protocol.FindingResolution{FindingID: finding, Decision: "DISMISSED", Supersedes: 1, Requirement: "AC-001", NormalPath: "app run valid-input", Module: "output", RootCause: "original report misread ordering requirement", Consequence: "no defect", Severity: "P2", SeverityRationale: "documented behavior is available", Verification: review.FindingVerification{Scope: "output", Severity: "limited", Causality: "none", Reproduction: "actual allowed output", Classification: "non-defect"}, Reason: "observed requirement permits output order"}
	_, _, err := d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
	if err == nil {
		t.Fatal("incorrect history revision accepted")
	}
	t.Logf("negative control: wrong Supersedes rejected: %v", err)
	r.Supersedes = 0
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" || len(d.run.Validation.Problems) != 0 {
		t.Fatal("valid initial dismissal created repair")
	}
	t.Log("control: initial valid DISMISSED completes without problem/repair")
}

func gateImpactBoundary(t *testing.T) *validationDriver {
	d := newValidationDriver(t, "blackbox", "quality", "security")
	d.reviewReady()
	id, in := d.action("engine.qa.execution")
	r := executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r}))
	id, in = d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "authorization rejection diagnostic differs from requirement"}}})
	d.drive()
	wave := d.run.Validation.Waves[0]
	var fid string
	for key := range d.run.Validation.Findings {
		fid = key
	}
	resolution := protocol.FindingResolution{FindingID: fid, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run invalid authorization input", Module: "authorization", RootCause: "authorization rejection diagnostic formatting", Consequence: "incorrect rejection diagnostic", Severity: "P2", SeverityRationale: "authorization still rejects input; diagnostic is inaccurate", Verification: review.FindingVerification{Scope: "documented command validation", Severity: "limited diagnostic defect", Causality: "authorization rejection branch", Reproduction: "run invalid authorization input and compare diagnostic", Classification: "implementation"}, Reason: "observed documented rejection but wrong diagnostic"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
	id, in = d.action("engine.repair")
	repair := gateRepairResult(in)
	repair.Development.Paths = []string{"authorization.go"}
	repair.Development.Summary = "correct authorization rejection logic and diagnostic"
	repair.Development.AffectedCases = []string{"invalid"}
	repair.Development.CaseRuns = []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "invalid", Result: coverage.ExecutedPass, Procedure: "app run invalid authorization input", Observation: "request rejected with documented diagnostic", Comparison: "exact diagnostic matches requirement"}}
	d.business(id, repair)
	d.verifyRepair(repair)
	d.materialize()
	d.materialize()
	wave = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	for _, target := range wave.Targets {
		if target.Gate == "security" {
			t.Logf("changed authorization.go without any gate impact/carry receipt: security status=%s action=%s", target.Status, target.ResultAction)
			if target.Status == "PASS" {
				t.Fatal("G REQ-005: unrelated gate carried unconditionally across production change without impact evidence")
			}
		}
	}
	return d
}

func TestOtherGateCarryRequiresImpact(t *testing.T) { gateImpactBoundary(t) }

func TestCorrectionBeforeDispatchUsesOnlyCurrentObligation(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			d, finding, wave := validationWithGateFinding(t)
			request := holdRequirement(t, d)
			resolveGateFinding(d, finding, wave)
			original := d.run.Validation.Findings[finding]
			if reopen {
				d.reopen()
			}
			correction := original.History[0]
			correction.Supersedes = 1
			correction.Decision = "DISMISSED"
			correction.ProblemID = ""
			correction.Reason = "The documented ordering permits the observed sequence"
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{correction}}}})
			releaseRequirement(t, d, request)
			d.drive()
			current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if current.Status != "COMPLETE" || current.Repair != nil {
				t.Fatal("superseded diagnosis issued a repair")
			}
			f := d.run.Validation.Findings[finding]
			if len(f.History) != 2 || f.History[0].ProblemID != original.History[0].ProblemID || f.History[1].AcceptedRevision <= f.History[0].AcceptedRevision {
				t.Fatal("correction lost original history or transaction order")
			}
			if p := d.run.Validation.Problems[original.History[0].ProblemID]; p.ResolvedWave != "" {
				t.Fatal("dismissal fabricated a repair completion")
			}
		})
	}
}
