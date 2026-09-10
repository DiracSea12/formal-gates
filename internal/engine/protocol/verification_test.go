package protocol

import (
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"testing"
)

func verificationEngine(t *testing.T) *Engine {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	completeQA(t, e, "blackbox", "design")
	completeQA(t, e, "blackbox", "review")
	gitReceipt(t, e, "Q1")
	return e
}
func verificationResult(x RegularExecution) VerificationResult {
	return VerificationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-" + x.Claim.ActionID, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: "PASS", Message: "deterministic protocol fixture"}
}
func executedCase(id, status string) CaseExecution {
	return CaseExecution{CaseID: id, Status: status, Procedure: "run command", Output: "actual fixture output", Comparison: "compared expected output", Environment: "fixture-env", Tool: "fixture-tool", Workspace: "/isolated/" + id}
}
func TestVerificationWaveFailureIsolationCheckpointAndRepair(t *testing.T) {
	e := verificationEngine(t)
	s, _ := e.Load()
	w := currentWave(s.State.Regular)
	if w == nil || len(w.Targets) != 2 {
		t.Fatal("wave not frozen with all selected targets")
	}
	digest := w.ExpectedDigest
	qa := currentExecution(t, e, "blackbox-execution")
	gate := currentExecution(t, e, "implementation-quality-gate")
	claimAndAttach(t, e, qa)
	claimAndAttach(t, e, gate)
	checkpoint := verificationResult(qa)
	checkpoint.Status = "CHECKPOINT"
	checkpoint.Message = "interrupted after known failure"
	checkpoint.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "FAIL")}
	recordQAOperations(t, e, qaOrExecution(e, checkpoint.ActionID), checkpoint.Cases)
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	s, _ = e.Load()
	if s.State.RegularExecutions[qa.Claim.ActionID].Finished || s.State.Attempts[qa.Claim.Task].Attempts > 1 {
		t.Fatal("checkpoint ended attempt")
	}
	result := verificationResult(qa)
	result.Status = "FAIL"
	result.Cases = checkpoint.Cases
	result.Findings = []ReviewFinding{{ID: "output-failure", Severity: "P1", Requirement: "BLACKBOX:C1", Problem: "wrong output", NormalPath: "run command", Consequence: "wrong result", SeverityRationale: "documented output breaks", Remedy: "correct output"}}
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result})
	sourceUnavailable(t, e, qa)
	s, _ = e.Load()
	if currentWave(s.State.Regular).Complete || currentRepair(s.State.Regular) != nil || s.State.RegularExecutions[gate.Claim.ActionID].Finished {
		t.Fatal("QA failure terminated independent gate or opened repair early")
	}
	g := verificationResult(gate)
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &g})
	s, _ = e.Load()
	if currentWave(s.State.Regular).Complete {
		t.Fatal("wave completed before source settlement")
	}
	sourceUnavailable(t, e, gate)
	s, _ = e.Load()
	w = currentWave(s.State.Regular)
	if !w.Complete || w.ExpectedDigest != digest || currentRepair(s.State.Regular) != nil {
		t.Fatal("wave/adjudication barrier differs")
	}
	a := WaveAdjudication{WaveID: w.ID, Target: "blackbox", ResultDigest: w.Results["blackbox"].Digest, Decisions: []WaveFindingDecision{{FindingID: "output-failure", Decision: "NEW", ProblemID: "wrong-output", Severity: "P1", Reason: "documented behavior fails", Evidence: "actual run matched reproduced failure", AffectedCases: []string{"BLACKBOX:C1"}}}}
	submitRegular(t, e, RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &a})
	s, _ = e.Load()
	repair := currentRepair(s.State.Regular)
	if repair == nil || repair.PureP2 || len(repair.Problems) != 1 || repair.Problems[0] != "wrong-output" {
		t.Fatal("unique repair omits QA failure")
	}
	if _, _, err := e.Drive(fingerprintOf(t, e)); err != nil {
		t.Fatal(err)
	}
	s, _ = e.Load()
	if len(s.State.Regular.Verification.Repairs) != 1 {
		t.Fatal("repeated drive created second logical repair")
	}
}
func TestVerificationRejectsIncompleteCheckpointAndRawObligations(t *testing.T) {
	e := verificationEngine(t)
	x := currentExecution(t, e, "blackbox-execution")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	r := s.State.Regular
	v := verificationResult(x)
	v.Cases = []CaseExecution{executedCase("unknown", "PASS")}
	if validateVerificationResult(r, x, v, false) == nil {
		t.Fatal("unknown case accepted")
	}
	v.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "FAIL")}
	if validateVerificationResult(r, x, v, false) == nil {
		t.Fatal("PASS accepted FAIL")
	}
	v.Status = "CHECKPOINT"
	v.Message = "interruption"
	v.Cases = nil
	v.Remaining = []string{"BLACKBOX:C1"}
	if err := validateVerificationResult(r, x, v, true); err != nil {
		t.Fatal(err)
	}
	recordQAOperations(t, e, qaOrExecution(e, v.ActionID), v.Cases)
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &v})
	s, _ = e.Load()
	if len(s.State.Regular.Verification.Problems) > 0 || len(s.State.Regular.Verification.Repairs) > 0 {
		t.Fatal("raw checkpoint invented obligation")
	}
}
func TestVerificationAffectedInheritanceReadyToPromote(t *testing.T) {
	e := verificationEngine(t)
	s, _ := e.Load()
	r := s.State.Regular
	p := r.Preparation.Candidate
	r.Verification.Effective["blackbox"] = ValidationEvidence{Target: "blackbox", Candidate: "Q1", Scope: coverage.ScopeFull, RequirementRevision: r.Requirement.Revision, Cases: []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}}
	next := *p
	next.Candidate.Identity = "Q2"
	freezeWave(r, &next, []string{})
	w := currentWave(r)
	target := waveTarget(w, "blackbox")
	if target.Scope != coverage.ScopeAffected || len(target.Expected) != 0 || len(target.Inherited) != 1 || !r.Preparation.QA["blackbox"].Frozen {
		t.Fatal("affected execution altered frozen design or inheritance")
	}
	if verificationReadyToPromote(r) {
		t.Fatal("old full validation carried to changed candidate")
	}
	r.Preparation.Candidate = &next
	if r.Guarantee.Status != "ACTIVE" {
		t.Fatal("fixture must exercise active guarantee")
	}
	for _, target := range w.Targets {
		w.Results[target.Target] = VerificationResult{Status: "PASS", Digest: "current-" + target.Target}
	}
	w.Complete = true
	updateEffective(r, w)
	if !verificationReadyToPromote(r) {
		t.Fatal("current AFFECTED PASS with inherited cases should permit promotion")
	}
	ev := r.Verification.Effective["blackbox"]
	if ev.Scope != coverage.ScopeAffected || len(ev.Cases) != 1 || ev.Cases[0].CaseID != "BLACKBOX:C1" {
		t.Fatal("inherited case lost or misrepresented as FULL")
	}
	w.Complete = false
	if verificationReadyToPromote(r) {
		t.Fatal("unfinished wave permitted promotion")
	}
	w.Complete = true
	delete(w.Results, "blackbox")
	if verificationReadyToPromote(r) {
		t.Fatal("missing result permitted promotion")
	}
	target.Expected = []string{"BLACKBOX:C1"}
	target.Inherited = nil
	w.Results["blackbox"] = VerificationResult{Status: "FAIL", Cases: []CaseExecution{executedCase("BLACKBOX:C1", "FAIL")}}
	delete(r.Verification.Effective, "blackbox")
	updateEffective(r, w)
	if verificationReadyToPromote(r) {
		t.Fatal("failed target permitted promotion")
	}
}

func TestVerificationControlsFreezeAndKeepBindings(t *testing.T) {
	e := verificationEngine(t)
	s, _ := e.Load()
	r := s.State.Regular
	original := currentWave(r).ID
	r.Verification.Effective["blackbox"] = ValidationEvidence{Target: "blackbox", ResultDigest: "raw-pass", WaveID: original, Candidate: "Q1", RequirementRevision: r.Requirement.Revision, Scope: coverage.ScopeFull, Cases: []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}}
	doc := Document(r.Requirement.Path, []byte(r.Requirement.Text+"\nNew explanation.\n"))
	q := RegularRequest{Action: "apply-requirement", Document: &doc, Meaning: "changed", Origin: "USER_INITIATED_CHANGE", Impact: map[string]string{"product-review": "unknown", "start-readiness": "unknown", "blackbox": "affected"}, Keep: []string{"product-review", "start-readiness", "blackbox"}, Reason: "retain explicitly selected existing validation"}
	if err := validateRequirementChange(r, q); err != nil {
		t.Fatal(err)
	}
	classifyVerificationChange(r, q, 100)
	if len(r.Verification.Effective) != 1 || r.Verification.Effective["blackbox"].RequirementRevision != doc.Revision || len(r.Verification.Inheritance) != 1 {
		t.Fatal("authorization did not preserve source separately")
	}
	applyRequirementChange(r, q, 100, s.State.RunID)
	candidate := *r.Preparation.Candidate
	candidate.Candidate.Identity = "Q1"
	bindVerificationInheritance(r, &candidate)
	ev, ok := r.Verification.Effective["blackbox"]
	if !ok || ev.Candidate != "Q1" || ev.RequirementRevision != doc.Revision {
		t.Fatal("kept PASS did not bind new input")
	}
	in := r.Verification.Inheritance[0]
	if in.Source.Candidate != "Q1" || in.Candidate != "Q1" || in.Kind != "USER_KEEP" || in.StateRevision != 100 {
		t.Fatal("inheritance erased actual evidence", in)
	}
	candidate.Candidate.Identity = "Q3"
	bindVerificationInheritance(r, &candidate)
	if r.Verification.Effective["blackbox"].Candidate != "Q1" {
		t.Fatal("retention authorization expanded to later candidate")
	}
}
func TestVerificationLateResultAndRouteAddition(t *testing.T) {
	e := verificationEngine(t)
	x := currentExecution(t, e, "blackbox-execution")
	claimAndAttach(t, e, x)
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "new requirement"}})
	result := verificationResult(x)
	result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
	a := submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result})
	if a.Status != "REJECTED_RESULT" {
		t.Fatal("late old result promoted")
	}
	s, _ := e.Load()
	if currentWave(s.State.Regular).Invalidated == "" || len(s.State.Regular.Verification.Effective) > 0 {
		t.Fatal("Phase A didn't invalidate wave")
	}
	e = verificationEngine(t)
	s, _ = e.Load()
	old := currentWave(s.State.Regular).ID
	regularChoice(t, e, RegularRequest{Action: "route-add", Route: &RegularRoute{Kind: "custom", Gates: []string{"complexity-gate"}}, Reason: "add independent complexity check"}, "confirm")
	s, _ = e.Load()
	w := currentWave(s.State.Regular)
	if w.ID == old || len(w.Targets) != 3 || s.State.Regular.Verification.Waves[0].Invalidated == "" {
		t.Fatal("target set mutated in place")
	}
	s, _ = e.Load()
	ev, _ := NewRegularEvent("invalid-late-qa-route", RegularEvent{Operation: "REQUEST", ExpectedRevision: s.Revision, Request: &RegularRequest{Action: "route-add", Route: &RegularRoute{Kind: "custom", QA: []string{"whitebox"}}, Reason: "late QA"}})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("late QA addition accepted")
	}
}

func TestVerificationPureP2CarryDoesNotCreateWave(t *testing.T) {
	e := verificationEngine(t)
	qa := currentExecution(t, e, "blackbox-execution")
	gate := currentExecution(t, e, "implementation-quality-gate")
	for _, x := range []RegularExecution{qa, gate} {
		claimAndAttach(t, e, x)
		v := verificationResult(x)
		if x.Claim.Task.Step == "blackbox-execution" {
			v.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
		} else {
			v.Findings = []ReviewFinding{{ID: "minor", Severity: "P2", Category: "quality", Requirement: "REQ-001", Problem: "duplicate expression", NormalPath: "read maintained code", Consequence: "unnecessary repetition", SeverityRationale: "minor quality issue", Remedy: "remove duplicate"}}
		}
		recordQAOperations(t, e, qaOrExecution(e, v.ActionID), v.Cases)
		submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &v})
		sourceUnavailable(t, e, x)
	}
	s, _ := e.Load()
	w := currentWave(s.State.Regular)
	a := WaveAdjudication{WaveID: w.ID, Target: "implementation-quality-gate", ResultDigest: w.Results["implementation-quality-gate"].Digest, Decisions: []WaveFindingDecision{{FindingID: "minor", Decision: "NEW", ProblemID: "minor-quality", Severity: "P2", Reason: "small local quality problem", Evidence: "verified exact duplicate", AffectedCases: []string{"BLACKBOX:C1"}}}}
	submitRegular(t, e, RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &a})
	s, _ = e.Load()
	r := s.State.Regular
	repair := currentRepair(r)
	if repair == nil || !repair.PureP2 {
		t.Fatal("minor gate PASS did not select P2 carry")
	}
	repair.Result = &RepairResult{Status: "PASS"}
	receipt := GitPreparationReceipt{Operation: GitPreparation{Kind: "repair-commit", Target: repair.ID, Revision: 90}, Git: gitprep.Receipt{After: "Q2", View: gitprep.View{Identity: "Q2", Digest: "sha256:new"}}}
	if err := acceptVerificationGit(s.State, receipt); err != nil {
		t.Fatal(err)
	}
	refreshVerification(s.State)
	if len(r.Verification.Waves) != 1 || r.Verification.Completed != 1 {
		t.Fatal("pure P2 added a semantic review wave")
	}
	if r.Verification.Effective["blackbox"].Candidate != "Q2" || r.Verification.Effective["implementation-quality-gate"].Candidate != "Q2" {
		t.Fatal("carry overwritten by original result")
	}
	if len(r.Verification.Inheritance) != 2 {
		t.Fatal("missing structured carry receipts")
	}
}

func TestWaveRecoveryOptionsAreExactAndNontransferable(t *testing.T) {
	for _, choice := range []string{"additional-wave", "retry-qa", "waiver", "change-requirement", "abort"} {
		t.Run(choice, func(t *testing.T) {
			e := verificationEngine(t)
			snap, _ := e.Load()
			s := snap.State
			r := s.Regular
			v := r.Verification
			w := currentWave(r)
			// Explicit bounded precondition: three complete, adjudicated failed waves.
			v.Completed = 3
			v.Limit = 3
			w.Complete = true
			w.Counted = true
			result := VerificationResult{Status: "FAIL", Digest: "failed-result", Findings: []ReviewFinding{{ID: "failure", Requirement: "BLACKBOX:C1"}}}
			w.Results["blackbox"] = result
			w.Results["implementation-quality-gate"] = VerificationResult{Status: "PASS", Digest: "gate-pass"}
			v.Adjudications = []WaveAdjudication{{WaveID: w.ID, Target: "blackbox", ResultDigest: result.Digest, Decisions: []WaveFindingDecision{{FindingID: "failure", ProblemID: "output", Decision: "NEW", Severity: "P1"}}}}
			v.Problems["output"] = CanonicalProblem{ID: "output", Severity: "P1", Status: "OPEN", AffectedCases: []string{"BLACKBOX:C1"}}
			s.Cost.Dispatches["unavailable-task"] = cost.DispatchCost{Source: cost.SourceUnavailable}
			q := RegularRequest{Action: "authorize-wave", Reason: "third completed wave still has the exact output obligation"}
			if err := e.regularRequest(s, "wave-choice", q, snap.Revision); err != nil {
				t.Fatal(err)
			}
			a := r.Ask
			if a.Preview == nil || a.Preview.Cost == nil || a.Preview.Cost.Dispatches["unavailable-task"].Source != cost.SourceUnavailable {
				t.Fatal("Ask concealed unavailable cost source")
			}
			if !equalStrings(a.Options, []string{"additional-wave", "retry-qa", "waiver", "change-requirement", "abort", "cancel"}) || len(a.Request.Keep) != 1 || a.Request.Keep[0] != "output" || a.Request.Target != w.ID {
				t.Fatal("wave Ask omitted exact obligations/choices")
			}
			d := RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: choice, Reason: "Current exact obligation disposition"}
			if err := e.regularDecision(s, d, snap.Revision+1, "wave-decision"); err != nil {
				t.Fatal(err)
			}
			if v.Completed != 3 {
				t.Fatal("control rewrote completed rounds")
			}
			switch choice {
			case "additional-wave", "retry-qa":
				if v.Limit != 4 {
					t.Fatal("not exactly one additional wave")
				}
			case "waiver":
				if r.Guarantee.Status != "WAIVED" || v.Problems["output"].Status != "WAIVED" || len(v.Waivers) != 1 || !waivedVerificationTarget(r, "blackbox", w.Candidate.Candidate.Identity) {
					t.Fatal("missing explicit waiver binding")
				}
				if _, ok := v.Effective["blackbox"]; ok {
					t.Fatal("waiver forged PASS")
				}
			case "change-requirement":
				if !r.ChangePending || w.Invalidated == "" {
					t.Fatal("change did not isolate current wave")
				}
			case "abort":
				if r.Terminal != "ABORTING" {
					t.Fatal("abort bypassed normal reconciliation")
				}
			}
		})
	}
}

func TestCheckpointChangedEnvironmentRetainsRawFailureAndReruns(t *testing.T) {
	e := verificationEngine(t)
	x := currentExecution(t, e, "blackbox-execution")
	claimAndAttach(t, e, x)
	checkpoint := verificationResult(x)
	checkpoint.Status = "CHECKPOINT"
	checkpoint.Message = "Stopped after an actual failure"
	checkpoint.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "FAIL")}
	recordQAOperations(t, e, qaOrExecution(e, checkpoint.ActionID), checkpoint.Cases)
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
	result := verificationResult(x)
	result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
	result.Cases[0].Environment = "restored test environment"
	result.Invalidated = []CheckpointInvalidation{{CaseID: "BLACKBOX:C1", PreviousTool: "fixture-tool", PreviousEnvironment: "fixture-env", Tool: "fixture-tool", Environment: "restored test environment", Reason: "Dependency fixture restored; rerun this exact case against the same candidate"}}
	if got := submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result}); got.Status != "ACCEPTED" {
		t.Fatal(got)
	}
	sourceUnavailable(t, e, x)
	s, _ := e.Load()
	w := currentWave(s.State.Regular)
	if len(w.CheckpointHistory["blackbox"]) != 1 || w.CheckpointHistory["blackbox"][0].Cases[0].Status != "FAIL" || w.Results["blackbox"].Cases[0].Status != "PASS" || s.State.RegularExecutions[x.Claim.ActionID].Number != 1 {
		t.Fatalf("precise invalidation erased raw FAIL or consumed a new attempt: history=%+v result=%+v attempts=%+v", w.CheckpointHistory, w.Results, s.State.Attempts[x.Claim.Task])
	}
}

func TestCheckpointMayResumeAgainWithoutNewAttempt(t *testing.T) {
	e := verificationEngine(t)
	x := currentExecution(t, e, "blackbox-execution")
	claimAndAttach(t, e, x)
	for n := 0; n < 2; n++ {
		checkpoint := verificationResult(x)
		checkpoint.Status = "CHECKPOINT"
		checkpoint.Message = "Known interruption, no incomplete item marked PASS"
		checkpoint.Remaining = []string{"BLACKBOX:C1"}
		recordQAOperations(t, e, qaOrExecution(e, checkpoint.ActionID), checkpoint.Cases)
		submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &checkpoint})
		sourceUnavailable(t, e, x)
		c := x.Claim
		c.Operation = "resume"
		c.Identity = "native-" + x.Claim.ActionID
		submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &c})
		submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: "codex", Identity: c.Identity, Status: "RUNNING"}})
	}
	result := verificationResult(x)
	result.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
	submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result})
	sourceUnavailable(t, e, x)
	s, _ := e.Load()
	final := s.State.RegularExecutions[x.Claim.ActionID]
	if !final.Finished || final.ResumeCount != 2 || final.Number != 1 || final.Identity != "native-"+x.Claim.ActionID {
		t.Fatal("repeated checkpoint changed TaskKey/attempt/native identity or remained stuck")
	}
}

func TestCurrentWaveAdjudicationMergeCorrectionAndAdvisory(t *testing.T) {
	e := verificationEngine(t)
	snap, _ := e.Load()
	r := snap.State.Regular
	w := currentWave(r)
	w.Complete = true
	w.Results["blackbox"] = VerificationResult{Digest: "raw-qa", Findings: []ReviewFinding{{ID: "failed-output", Severity: "P1"}}}
	w.Results["implementation-quality-gate"] = VerificationResult{Digest: "raw-gate", Findings: []ReviewFinding{{ID: "same-output", Severity: "P1"}, {ID: "advice", Severity: "P3"}}}
	a := WaveAdjudication{WaveID: w.ID, Target: "blackbox", ResultDigest: "raw-qa", Decisions: []WaveFindingDecision{{FindingID: "failed-output", Decision: "NEW", ProblemID: "output", Severity: "P1", Reason: "required behavior fails", Evidence: "go run . observed wrong output", AffectedCases: []string{"BLACKBOX:C1"}}}}
	if err := adjudicateWave(r, a, 100); err != nil {
		t.Fatal(err)
	}
	b := WaveAdjudication{WaveID: w.ID, Target: "implementation-quality-gate", ResultDigest: "raw-gate", Decisions: []WaveFindingDecision{{FindingID: "same-output", Decision: "SAME_AS", ProblemID: "output", Severity: "P1", Reason: "same underlying output defect", Evidence: "same command and consequence", AffectedCases: []string{"BLACKBOX:C1"}}, {FindingID: "advice", Decision: "NEW", ProblemID: "optional", Severity: "P3", Reason: "optional naming change", Evidence: "does not affect the required output"}}}
	if err := adjudicateWave(r, b, 101); err != nil {
		t.Fatal(err)
	}
	if len(r.Verification.Problems["output"].Findings) != 2 || r.Verification.Problems["optional"].Severity != "P3" {
		t.Fatal("merge lost membership or advisory severity")
	}
	b.Decisions[0].Decision = "DISMISSED"
	b.Decisions[0].ProblemID = ""
	b.Decisions[0].Reason = "normal path was rechecked and this finding is redundant"
	b.ReplacesRevision = 101
	if err := adjudicateWave(r, b, 102); err == nil {
		t.Fatal("stale correction accepted")
	}
	b.ReplacesRevision = 102
	if err := adjudicateWave(r, b, 103); err != nil {
		t.Fatal(err)
	}
	if len(r.Verification.Adjudications) != 3 || len(r.Verification.Problems["output"].Findings) != 1 || w.Results["implementation-quality-gate"].Digest != "raw-gate" {
		t.Fatal("correction rewrote raw history or retained dismissed membership")
	}
	refreshVerification(snap.State)
	repair := currentRepair(r)
	if repair == nil || len(repair.Problems) != 1 || repair.Problems[0] != "output" {
		t.Fatalf("P3 entered repair: %+v", repair)
	}
	a.ReplacesRevision = 101
	a.Decisions[0].Severity = "P3"
	if err := adjudicateWave(r, a, 104); err != nil {
		t.Fatal(err)
	}
	if r.Verification.Problems["output"].Severity != "P3" || len(repair.Problems) != 0 || len(repair.AffectedCases) != 0 {
		t.Fatalf("accepted downgrade retained blocking obligations: problem=%+v repair=%+v", r.Verification.Problems["output"], repair)
	}
}

func qaOrExecution(e *Engine, id string) RegularExecution {
	s, _ := e.Load()
	return s.State.RegularExecutions[id]
}
