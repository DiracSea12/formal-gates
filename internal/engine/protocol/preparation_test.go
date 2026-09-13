package protocol

import (
	"fmt"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/gitprep"
)

func preparationEngine(t *testing.T, modes ...string) *Engine {
	return preparationEngineWithPlan(t, nil, modes...)
}

func preparationEngineWithPlan(t *testing.T, edit func(*DevelopmentPlan), modes ...string) *Engine {
	t.Helper()
	e := regularEngine(t, nil)
	regularStart(t, e)
	for _, step := range reviewActions {
		x := currentExecution(t, e, step)
		claimAndAttach(t, e, x)
		v := resultFor(x)
		if step == "start-readiness" {
			v.GranularityReview = planForReview(x.Claim.RequirementRevision)
			b := v.GranularityReview.Batches[0]
			b.ID = "B2"
			b.Subtasks = []DevelopmentSubtask{{ID: "B2-S1", Task: "Finish delivery"}}
			b.Dependencies = []string{"B1"}
			v.GranularityReview.Batches = append(v.GranularityReview.Batches, b)
			if edit != nil {
				edit(v.GranularityReview)
			}
		}
		submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
		sourceUnavailable(t, e, x)
		adjudicatePass(t, e, step)
	}
	s, _ := e.Load()
	regularChoice(t, e, RegularRequest{Action: "topology", PlanDigest: s.State.Regular.Plan.Digest, Reason: "one repository"}, "confirm")
	regularChoice(t, e, RegularRequest{Action: "route", Route: &RegularRoute{Kind: "custom", QA: modes, Gates: []string{"implementation-quality-gate"}}}, "confirm")
	return e
}
func gitReceipt(t *testing.T, e *Engine, after string, ownership ...string) {
	t.Helper()
	if _, _, err := e.Drive(fingerprintOf(t, e)); err != nil {
		t.Fatal(err)
	}
	s, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	op := s.State.Regular.Preparation.Operation
	if op == nil {
		t.Fatal("no Git operation")
	}
	submitRegular(t, e, RegularEvent{Operation: "GIT_CLAIM", GitClaim: op})
	s, _ = e.Load()
	op = s.State.Regular.Preparation.Operation
	receipt := GitPreparationReceipt{Operation: *op, Resources: map[string]string{}, Git: gitprep.Receipt{OperationID: op.ID, Before: op.Before, After: after, View: gitprep.View{Identity: after, Digest: "sha256:view"}}}
	switch op.Kind {
	case "workspaces":
		receipt.Resources = map[string]string{"development": "/isolated/dev", "blackbox": "/isolated/baseline"}
	case "whitebox-workspace":
		receipt.Resources["whitebox"] = "/isolated/whitebox"
	case "whitebox-commit":
		receipt.Ownership = &gitprep.Ownership{Status: "TEST_ONLY"}
		if len(ownership) > 0 {
			receipt.Ownership.Status = ownership[0]
		}
	case "finalize":
		receipt.Artifacts = map[string]string{}
		for mode, q := range s.State.Regular.Preparation.QA {
			receipt.Artifacts[PreparationArtifact(s.State.RunID, mode)] = gitprep.Digest(q.Design)
		}
	}
	submitRegular(t, e, RegularEvent{Operation: "GIT_RECEIPT", GitReceipt: &receipt})
}
func preparationResult(x RegularExecution) PreparationResult {
	return PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-" + x.Claim.ActionID, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS"}
}
func completeBatch(t *testing.T, e *Engine, id string) {
	x := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, x)
	v := preparationResult(x)
	v.Manifest = []string{id + ".go"}
	v.Handoff = "No unresolved decisions"
	v.Validation = []BatchValidation{{SubtaskID: id + "-S1", Procedure: "go test ./...", Observation: "all tests passed", Result: "PASS"}}
	a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	if a.Status != "ACCEPTED" {
		t.Fatal(a)
	}
	sourceUnavailable(t, e, x)
}
func testDesign(r *RegularState, mode string) QADesign {
	m := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: r.RequiredSources.Binding, ReviewKind: coverage.ReviewKind(mode), RouteScope: digestOfCanonical(r.Route), TopologyScope: r.Topology}, Sources: []string{r.Requirement.Path}, Points: []coverage.AcceptancePoint{{PointID: mode + ":AC-001", ObservableBehavior: "display result", Oracle: "hello"}}, Cases: []coverage.AcceptanceCase{{CaseID: mode + ":C1", Command: &coverage.Command{Executable: "echo", Args: []string{"hello"}}, Mode: coverage.CaseMode(mode), PublicEntry: "app", Preconditions: "installed", Steps: "invoke", Oracle: "hello"}}, Edges: []coverage.CoverageEdge{{SourceID: r.Requirement.Path, PointID: mode + ":AC-001", CaseID: mode + ":C1"}}}
	if mode == "WHITEBOX" {
		m.Cases[0] = coverage.AcceptanceCase{CaseID: mode + ":C1", Command: &coverage.Command{Executable: "go", Args: []string{"test", "./..."}}, Mode: coverage.CaseWhitebox, TestRef: "main_test.go::TestHello", Oracle: "hello"}
	}
	d := QADesign{Manifest: m, CoverageJudgment: "Independently mapped the output requirement and exact oracle"}
	if mode == "WHITEBOX" {
		d.TestFiles = []string{"main_test.go"}
	}
	return d
}
func completeQA(t *testing.T, e *Engine, mode, kind string) {
	x := currentExecution(t, e, mode+"-"+kind)
	claimAndAttach(t, e, x)
	v := preparationResult(x)
	s, _ := e.Load()
	if kind == "design" {
		d := testDesign(s.State.Regular, string(coverage.ReviewBlackbox))
		if mode == "whitebox" {
			recordCoverage(t, e, x)
			d = testDesign(s.State.Regular, string(coverage.ReviewWhitebox))
			v.Reuse = []TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "asserts exact output", Procedure: "go test -run TestHello", Observation: "PASS", Comparison: "hello equals hello", Result: "EXECUTED_PASS"}}
		}
		v.Design = &d
	} else {
		q := s.State.Regular.Preparation.QA[mode]
		v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: s.State.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: string(q.Design.Manifest.Binding.ReviewKind) + ":AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: string(q.Design.Manifest.Binding.ReviewKind) + ":C1", Status: coverage.StatusPass}}}
	}
	a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	if a.Status != "ACCEPTED" {
		t.Fatal(a)
	}
	sourceUnavailable(t, e, x)
}
func TestPreparationConcurrentBatchesNoIntermediateGates(t *testing.T) {
	e := preparationEngine(t, "blackbox", "whitebox")
	gitReceipt(t, e, "git-base")
	s, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.State.PendingActions) != 2 {
		t.Fatalf("development and baseline QA not simultaneously issued: %v", s.State.PendingActions)
	}
	dev := currentExecution(t, e, "development-worker")
	black := currentExecution(t, e, "blackbox-design")
	if dev.Claim.Candidate != "git-base" || black.Claim.Candidate != "git-base" || dev.Claim.Task == black.Claim.Task {
		t.Fatal("bad isolation claims")
	}
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	s, _ = e.Load()
	for _, x := range s.State.RegularExecutions {
		if x.Claim.Task.Step == "whitebox-design" || x.Claim.Task.Step == "implementation-quality-gate" {
			t.Fatal("premature whitebox/gate")
		}
	}
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	completeQA(t, e, "blackbox", "design")
	completeQA(t, e, "blackbox", "review")
	gitReceipt(t, e, "D2")
	completeQA(t, e, "whitebox", "design")
	gitReceipt(t, e, "Q0")
	completeQA(t, e, "whitebox", "review")
	gitReceipt(t, e, "Q1")
	s, err = e.Load()
	if err != nil {
		t.Fatal(err)
	}
	candidate := s.State.Regular.Preparation.Candidate
	if candidate == nil || !candidate.ReviewReady || candidate.DevelopmentSnapshot != "D2" || len(candidate.Coverage.Manifests) != 2 {
		t.Fatalf("candidate=%+v", candidate)
	}
	plan, _, err := e.Plan()
	if err != nil || plan.Next.Kind != decision.KindWait || currentWave(s.State.Regular) == nil {
		t.Fatalf("boundary=%+v %v", plan, err)
	}
}
func TestPreparationNoWhiteboxAndPlanInvalidation(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	old := currentExecution(t, e, "development-worker")
	regularChoice(t, e, RegularRequest{Action: "plan-change", Reason: "Batch interface changed"}, "confirm")
	s, _ := e.Load()
	if s.State.Regular.Plan.ConfirmedRevision != "" || s.State.Regular.Topology != "" {
		t.Fatal("old plan still confirmed")
	}
	snap, _ := e.Load()
	ev, _ := NewRegularEvent("stale-claim", RegularEvent{Operation: "CLAIM", ExpectedRevision: snap.Revision, Claim: &old.Claim})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("old plan claim accepted")
	}
	e = preparationEngine(t)
	gitReceipt(t, e, "git-base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	gitReceipt(t, e, "Q1")
	s, _ = e.Load()
	if s.State.Regular.Preparation.Candidate == nil {
		t.Fatal("no-whitebox candidate missing")
	}
}
func TestSharedStepAndSingleCaseInvalidation(t *testing.T) {
	old := coverage.AcceptanceManifest{SharedSteps: []coverage.SharedStep{{ID: "setup", Procedure: "install"}}, Cases: []coverage.AcceptanceCase{{CaseID: "one", Oracle: "one", SharedSteps: []string{"setup"}}, {CaseID: "two", Oracle: "two", SharedSteps: []string{"setup"}}, {CaseID: "three", Oracle: "three"}}}
	new := old
	new.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "install new"}}
	review := coverage.ReviewResult{CaseDecisions: []coverage.ReviewDecision{{ID: "one", Status: coverage.StatusPass}, {ID: "two", Status: coverage.StatusPass}, {ID: "three", Status: coverage.StatusPass}}, SharedStepDecisions: []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass}}}
	got := ChangedQA(old, new, review)
	if len(got.CaseDecisions) != 1 || got.CaseDecisions[0].ID != "three" || len(got.SharedStepDecisions) != 0 {
		t.Fatal(got)
	}
	_ = fmt.Sprintf("%v", got)
}

func TestPreparationCoverageAndSuggestionBarriers(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	completeQA(t, e, "blackbox", "design")
	x := currentExecution(t, e, "blackbox-review")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	q := s.State.Regular.Preparation.QA["blackbox"]
	v := preparationResult(x)
	v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: "requirements.md", Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:C1", Status: coverage.StatusPass}}}
	v.Suggestions = []QASuggestion{{ID: "SG-1", Severity: "P3", Problem: "Improve a case explanation", Cases: []string{"BLACKBOX:C1"}}}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	sourceUnavailable(t, e, x)
	s, _ = e.Load()
	if s.State.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("undisposed suggestion froze")
	}
	for _, request := range []RegularRequest{{Action: "qa-suggestion", Target: "blackbox", FindingID: "SG-1", Meaning: "deferred"}, {Action: "qa-suggestion", Target: "blackbox", FindingID: "unknown", Meaning: "deferred", Reason: "not necessary"}} {
		snap, _ := e.Load()
		ev, _ := NewRegularEvent(EventID(fmt.Sprintf("bad-%d", snap.Revision)), RegularEvent{Operation: "REQUEST", ExpectedRevision: snap.Revision, Request: &request})
		if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
			t.Fatal("invalid suggestion disposition accepted")
		}
	}
	regularChoice(t, e, RegularRequest{Action: "qa-suggestion", Target: "blackbox", FindingID: "SG-1", Meaning: "deferred", Reason: "Current wording is sufficient"}, "confirm")
	s, _ = e.Load()
	if !s.State.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("disposed valid QA failed to freeze")
	}
	invalid := testDesign(s.State.Regular, "BLACKBOX")
	invalid.Manifest.Edges = nil
	if err := validateQADesign(s.State.Regular, "blackbox", invalid); err == nil {
		t.Fatal("missing AC accepted")
	}
	invalid = testDesign(s.State.Regular, "BLACKBOX")
	invalid.Manifest.Points[0].PointID = "other"
	invalid.Manifest.Edges[0].PointID = "other"
	if err := validateQADesign(s.State.Regular, "blackbox", invalid); err == nil {
		t.Fatal("invented point replaced AC")
	}
}
func TestPreparationGitReceiptCannotFreezeMissingInputs(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	s, _ := e.Load()
	p := s.State.Regular.Preparation
	op := GitPreparation{ID: "unissued", Kind: "finalize", Before: p.DevelopmentSnapshot, BindingDigest: p.BindingDigest, Claimed: true}
	receipt := GitPreparationReceipt{Operation: op, Git: gitprep.Receipt{OperationID: op.ID, Before: op.Before, After: "fake", View: gitprep.View{Identity: "fake", Digest: "sha256:view"}}}
	ev, _ := NewRegularEvent("early-candidate", RegularEvent{Operation: "GIT_RECEIPT", ExpectedRevision: s.Revision, GitReceipt: &receipt})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("unissued finalization accepted")
	}
	after, _ := e.Load()
	if after.Revision != s.Revision || after.State.Regular.Preparation.Candidate != nil {
		t.Fatal("failed receipt changed state")
	}
}

func TestPreparationIncrementalReviewRetainsUnchangedApprovals(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	completeQA(t, e, "blackbox", "design")
	completeQA(t, e, "blackbox", "review")
	regularChoice(t, e, RegularRequest{Action: "qa-revise", Target: "blackbox", Reason: "Clarify raw output comparison"}, "confirm")
	x := currentExecution(t, e, "blackbox-design")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	design := testDesign(s.State.Regular, "BLACKBOX")
	design.Manifest.Cases[0].Steps = "invoke and preserve raw output without normalization"
	result := preparationResult(x)
	result.Design = &design
	if a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result}); a.Status != "ACCEPTED" {
		t.Fatal(a)
	}
	sourceUnavailable(t, e, x)
	s, _ = e.Load()
	qa := s.State.Regular.Preparation.QA["blackbox"]
	if qa.ReviewScope != coverage.ScopeAffected || qa.RetainedReview == nil || len(qa.RetainedReview.SourceDecisions) != 1 || len(qa.RetainedReview.PointDecisions) != 1 || len(qa.RetainedReview.CaseDecisions) != 0 {
		t.Fatalf("incremental input lost its exact review scope/approvals: %+v", qa)
	}
	x = currentExecution(t, e, "blackbox-review")
	claimAndAttach(t, e, x)
	result = preparationResult(x)
	result.Review = &coverage.ReviewResult{Binding: design.Manifest.Binding, Scope: coverage.ScopeAffected, CaseDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:C1", Status: coverage.StatusPass}}}
	if a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result}); a.Status != "ACCEPTED" {
		t.Fatal(a)
	}
	s, _ = e.Load()
	qa = s.State.Regular.Preparation.QA["blackbox"]
	if !qa.Frozen || qa.Review.Scope != coverage.ScopeAffected || len(qa.Review.SourceDecisions) != 1 || len(qa.Review.CaseDecisions) != 1 {
		t.Fatalf("changed case did not complete the retained approval set: %+v", qa)
	}
}

func failQAReview(t *testing.T, e *Engine, mode string) {
	t.Helper()
	x := currentExecution(t, e, mode+"-review")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	q := s.State.Regular.Preparation.QA[mode]
	v := preparationResult(x)
	v.Status = "FAIL"
	v.Message = "The case does not compare the actual output with the required hello oracle"
	kind := string(q.Design.Manifest.Binding.ReviewKind)
	v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SetStatus: coverage.StatusFail, SourceDecisions: []coverage.ReviewDecision{{ID: s.State.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: kind + ":AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: kind + ":C1", Status: coverage.StatusFail}}}
	if got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v}); got.Status != "ACCEPTED" {
		t.Fatal(got)
	}
	sourceUnavailable(t, e, x)
}
func chooseQARecovery(t *testing.T, e *Engine, choice string) {
	t.Helper()
	s, _ := e.Load()
	a := s.State.Regular.Ask
	if a == nil || a.QA == nil {
		t.Fatal("missing typed QA recovery Ask")
	}
	token := freshnessToken(s.Revision, RequestID(a.ID))
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: token, Digest: a.Digest, Choice: choice, Reason: "Explicit disposition of this current QA action and exact inputs"}})
}
func TestBlackboxFailureRecoversDuringActiveDevelopment(t *testing.T) {
	e := preparationEngine(t, "blackbox", "whitebox")
	gitReceipt(t, e, "git-base")
	dev := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, dev)
	for n := 1; n <= 3; n++ {
		completeQA(t, e, "blackbox", "design")
		failQAReview(t, e, "blackbox")
		s, _ := e.Load()
		q := s.State.Regular.Preparation.QA["blackbox"]
		if q.ConsecutiveFailures != n {
			t.Fatalf("semantic count %d: %+v", n, q)
		}
		if s.State.RegularExecutions[dev.Claim.ActionID].Finished {
			t.Fatal("QA failure terminated independent development")
		}
		if s.State.Regular.Preparation.QA["whitebox"].Design != nil {
			t.Fatal("whitebox started before development complete")
		}
		if n < 3 {
			next := currentExecution(t, e, "blackbox-design")
			if next.Claim.Candidate != "git-base" || next.Claim.Task == dev.Claim.Task {
				t.Fatal("redesign lost baseline isolation")
			}
			if s.State.Regular.Ask != nil {
				t.Fatal("first two FAIL should auto-redesign")
			}
		} else {
			a := s.State.Regular.Ask
			if a == nil || a.Kind != "qa-recovery" || a.QA.Candidate != "git-base" || a.QA.ConsecutiveFailures != 3 || a.QA.CaseSetDigest != digestOfCanonical(q.Design) || a.QA.RetryExhausted {
				t.Fatalf("wrong recovery binding: %+v", a)
			}
			if s.State.Regular.Preparation.QA["whitebox"].ConsecutiveFailures != 0 {
				t.Fatal("QA counters crossed modes")
			}
		}
	}
	chooseQARecovery(t, e, "redesign")
	completeQA(t, e, "blackbox", "design")
	// Review scope is explicitly AFFECTED after a failed design. Complete it with
	// real item-level approvals; the PASS clears this mode's consecutive failures.
	x := currentExecution(t, e, "blackbox-review")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	q := s.State.Regular.Preparation.QA["blackbox"]
	v := preparationResult(x)
	v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SourceDecisions: []coverage.ReviewDecision{{ID: s.State.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:C1", Status: coverage.StatusPass}}}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	sourceUnavailable(t, e, x)
	s, _ = e.Load()
	if !s.State.Regular.Preparation.QA["blackbox"].Frozen || s.State.Regular.Preparation.QA["blackbox"].ConsecutiveFailures != 0 || s.State.RegularExecutions[dev.Claim.ActionID].Finished {
		t.Fatal("recovery failed to PASS alongside the same active developer")
	}
}
func TestBlackboxRuntimeExhaustionIsAskAndBounded(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	dev := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, dev)
	first := currentExecution(t, e, "blackbox-design")
	for n := 1; n <= 3; n++ {
		x := currentExecution(t, e, "blackbox-design")
		if x.Claim.Task != first.Claim.Task {
			t.Fatal("runtime retry changed TaskKey")
		}
		claimAndAttach(t, e, x)
		v := preparationResult(x)
		v.Status = "RUNTIME_ERROR"
		v.Message = "native session ended before a complete structured result"
		submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
		sourceUnavailable(t, e, x)
	}
	s, _ := e.Load()
	if s.State.Regular.Ask == nil || !s.State.Regular.Ask.QA.RetryExhausted || s.State.Regular.Ask.QA.ConsecutiveFailures != 0 {
		t.Fatal("exhausted runtime was hidden as WAIT/semantic failure")
	}
	if s.State.RegularExecutions[dev.Claim.ActionID].Finished {
		t.Fatal("failed QA stopped developer")
	}
	chooseQARecovery(t, e, "retry")
	x := currentExecution(t, e, "blackbox-design")
	if x.Number != 4 || x.Claim.Task != first.Claim.Task {
		t.Fatal("additional attempt not bound to same task")
	}
	claimAndAttach(t, e, x)
	v := preparationResult(x)
	v.Status = "RUNTIME_ERROR"
	v.Message = "same unavailable operation"
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	sourceUnavailable(t, e, x)
	s, _ = e.Load()
	if s.State.Regular.Ask == nil || !s.State.Regular.Ask.QA.RetryExhausted || s.State.Regular.Preparation.ExtraAttempts[first.Claim.Task.String()] != 1 {
		t.Fatal("one extra failure silently expanded allowance")
	}
}
func TestPrewaveWaiverDoesNotForgePASS(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	for n := 0; n < 3; n++ {
		completeQA(t, e, "blackbox", "design")
		failQAReview(t, e, "blackbox")
	}
	chooseQARecovery(t, e, "waiver")
	s, _ := e.Load()
	q := s.State.Regular.Preparation.QA["blackbox"]
	if q.ReviewWaiver == nil || q.Review.CaseDecisions[0].Status != coverage.StatusFail || q.Whitelist != nil || s.State.Regular.Guarantee.Status != "WAIVED" {
		t.Fatal("waiver fabricated review approval")
	}
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	gitReceipt(t, e, "Q1")
	s, _ = e.Load()
	w := currentWave(s.State.Regular)
	if w == nil || waveTarget(w, "blackbox") == nil || len(waveTarget(w, "blackbox").Expected) != 1 {
		t.Fatal("review waiver fabricated or skipped execution")
	}
	if len(s.State.Regular.Preparation.Candidate.Whitelist.Entries) != 0 {
		t.Fatal("failed cases exposed as approved")
	}
}

func TestQADesignRejectsShellCommandBeforeReviewAndAllowsRevision(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	x := currentExecution(t, e, "blackbox-design")
	claimAndAttach(t, e, x)
	s, _ := e.Load()
	d := testDesign(s.State.Regular, "BLACKBOX")
	d.Manifest.Cases[0].Command = &coverage.Command{Executable: "go test", Args: []string{"./..."}}
	v := preparationResult(x)
	v.Design = &d
	a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	if a.Status != "REJECTED_RESULT" {
		t.Fatalf("shell command admitted: %+v", a)
	}
	s, _ = e.Load()
	if s.State.Regular.Preparation.QA["blackbox"].Design != nil || s.State.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("invalid argv reached review/freeze")
	}
	if len(s.State.Regular.PreparationErrors) != 1 {
		t.Fatal("actual invalid declaration not retained")
	}
	sourceUnavailable(t, e, x)
	completeQA(t, e, "blackbox", "design")
	completeQA(t, e, "blackbox", "review")
	s, _ = e.Load()
	if !s.State.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("corrected native declaration did not reach ordinary review/freeze")
	}
	// Program names are not Go-specific; explicit file paths can contain spaces.
	for _, executable := range []string{"go", "python3", "/tmp/tool directory/my tool", "./my tool"} {
		d.Manifest.Cases[0].Command.Executable = executable
		if err := validateQADesign(s.State.Regular, "blackbox", d); err != nil {
			t.Fatalf("valid executable %q: %v", executable, err)
		}
	}
}

func TestWhiteboxReuseFailureMustMatchActualCaseEvidence(t *testing.T) {
	manifest := coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello"}}}
	valid := TestReuse{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "Independent hello output requirement", Procedure: "go test -run TestHello", Observation: "stdout BAD; expected hello", Comparison: "Exact output differs", Result: "EXECUTED_FAIL"}
	for _, tc := range []struct {
		name      string
		failed    bool
		mutate    func(*TestReuse)
		wantError bool
	}{
		{"actual_failure", true, func(*TestReuse) {}, false},
		{"failure_cannot_be_PASS", false, func(*TestReuse) {}, true},
		{"FAIL_needs_failed_test", true, func(r *TestReuse) { r.Result = "EXECUTED_PASS" }, true},
		{"missing_observation", true, func(r *TestReuse) { r.Observation = "" }, true},
		{"wrong_test", true, func(r *TestReuse) { r.TestRef = "other_test.go::TestOther" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			err := validateReuse(manifest, []TestReuse{r}, tc.failed)
			if (err != nil) != tc.wantError {
				t.Fatalf("validation=%v", err)
			}
		})
	}
}
