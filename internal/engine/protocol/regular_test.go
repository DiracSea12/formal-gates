package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const regularDocument = "# Task\n\n## 需求点\n\n### REQ-001：显示结果\n\n#### 要求\n显示绑定的结果。\n\n#### 验收条件\n- AC-001：输出包含结果。\n\n#### 来源\n用户要求。\n"

func regularEngine(t *testing.T, cap *int64) *Engine {
	t.Helper()
	return regularEngineAt(t, cap, t.TempDir())
}
func regularEngineAt(t *testing.T, cap *int64, dir string) *Engine {
	t.Helper()
	return regularEngineForProvider(t, cap, dir, "codex")
}
func regularEngineForProvider(t *testing.T, cap *int64, dir, provider string, capacities ...int) *Engine {
	t.Helper()
	capacity := 4
	if len(capacities) > 0 {
		capacity = capacities[0]
	}
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:b1"})
	if err != nil {
		t.Fatal(err)
	}
	cd, err := compiler.Compile(definition.RegularWorkflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(store, Config{Definition: cd, Registry: definition.Registry(), Capacity: capacity}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := decision.NewState(cd.Version, runtime.PhaseIntakeRegistered)
	if err = e.InitRegular(view, provider, fingerprintOf(t, e), "b1", RegularState{Requirement: Document("requirements.md", []byte(regularDocument)), BaseSnapshot: "git-base", Candidate: "git-base", Settings: settings.Effective{Values: settings.Values{Capacity: capacity, TokenCap: cap}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.Drive(fingerprintOf(t, e)); err != nil {
		t.Fatal(err)
	}
	return e
}
func submitRegular(t *testing.T, e *Engine, v RegularEvent) Acceptance {
	t.Helper()
	s, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	v.ExpectedRevision = s.Revision
	ev, err := NewRegularEvent(EventID(fmt.Sprintf("event-%d", s.Revision)), v)
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.Submit(ev, fingerprintOf(t, e))
	if err != nil {
		t.Fatalf("%s: %v", v.Operation, err)
	}
	return a
}
func regularChoice(t *testing.T, e *Engine, q RegularRequest, choice string) {
	t.Helper()
	// Fixture users explicitly delegate their scenario impact classifications.
	if q.Action == "apply-requirement" {
		snap, _ := e.Load()
		if d := snap.State.Regular.RequirementImpact; d != nil {
			if q.Impact == nil {
				q.Impact = map[string]string{}
			}
			for name, choice := range d.Choices {
				if choice == "delegate" && q.Impact[name] == "" {
					q.Impact[name] = "unknown"
				}
			}
		}
	}
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &q})
	s, _ := e.Load()
	a := s.State.Regular.Ask
	dispositions := map[string]string{}
	for _, item := range a.ImpactItems {
		dispositions[item.ID] = "delegate"
	}
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: RegularFreshness(s.Revision, a.ID), Digest: a.Digest, Choice: choice, Dispositions: dispositions}})
}
func regularStart(t *testing.T, e *Engine) {
	regularChoice(t, e, RegularRequest{Action: "confirm-requirement"}, "confirm")
	regularChoice(t, e, RegularRequest{Action: "mode", Mode: "regular"}, "confirm")
}
func currentExecution(t *testing.T, e *Engine, action string) RegularExecution {
	t.Helper()
	s, _ := e.Load()
	for _, x := range s.State.RegularExecutions {
		if string(x.Claim.Task.Step) == action && !x.Finished {
			return x
		}
	}
	t.Fatalf("no execution for %s in %+v", action, s.State.Regular)
	return RegularExecution{}
}
func claimAndAttach(t *testing.T, e *Engine, x RegularExecution) {
	t.Helper()
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Status: "RUNNING"}})
}
func resultFor(x RegularExecution) RawReviewCandidate {
	return RawReviewCandidate{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Action: string(x.Claim.Task.Step), RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Message: "All requirements examined against their normal usage and acceptance entries.", Complete: true, Findings: []ReviewFinding{}}
}
func sourceUnavailable(t *testing.T, e *Engine, x RegularExecution) {
	t.Helper()
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Status: "UNAVAILABLE", Reason: "This deterministic fixture has no provider transcript."}})
}
func adjudicatePass(t *testing.T, e *Engine, action string) {
	t.Helper()
	s, _ := e.Load()
	submitRegular(t, e, RegularEvent{Operation: "ADJUDICATE", Adjudication: &ReviewAdjudication{Action: action, ResultDigest: s.State.Regular.Reviews[action].CurrentDigest, Findings: []FindingAdjudication{}, CompletenessEvidence: "Checked the full current document, requirement coverage, normal paths and result binding."}})
}
func planForReview(revision string) *DevelopmentPlan {
	return &DevelopmentPlan{RequirementRevision: revision, Rationale: "One capability with a shared state boundary.", Batches: []DevelopmentBatch{{ID: "B1", Task: "Deliver result output", Subtasks: []DevelopmentSubtask{{ID: "B1-S1", Task: "Implement and validate result output"}}, Interfaces: "CLI output", StateBoundary: "Result projection", Risks: "Wrong result binding", DefinitionOfDone: "AC-001 passes", Validation: "Run result fixture", Recovery: "Revert one batch commit", Handoff: "No unresolved decisions", ContextCost: "Shared output semantics"}}}
}
func TestRegularIntakeReviewRouteAndReplay(t *testing.T) {
	e := regularEngine(t, nil)
	s, _ := e.Load()
	if s.State.Regular.Requirement.Confirmed || s.State.Route != "" {
		t.Fatal("start preconfirmed intake")
	}
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	claimAndAttach(t, e, x)
	v := resultFor(x)
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
	s, _ = e.Load()
	if s.State.Regular.Guard.Status != "COST_PENDING" || s.State.Regular.Reviews["product-review"].Completed != 1 {
		t.Fatal("result did not stop before source settlement")
	}
	adjudicatePass(t, e, "product-review")
	s, _ = e.Load()
	if len(s.State.RegularExecutions) != 1 {
		t.Fatal("refilled before cost")
	}
	sourceUnavailable(t, e, x)
	y := currentExecution(t, e, "start-readiness")
	claimAndAttach(t, e, y)
	technical := resultFor(y)
	technical.GranularityReview = planForReview(y.Claim.RequirementRevision)
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &technical})
	sourceUnavailable(t, e, y)
	adjudicatePass(t, e, "start-readiness")
	s, _ = e.Load()
	regularChoice(t, e, RegularRequest{Action: "topology", PlanDigest: s.State.Regular.Plan.Digest, Reason: "Single repository; no slice boundaries."}, "confirm")
	regularChoice(t, e, RegularRequest{Action: "route", Route: &RegularRoute{Kind: "full"}}, "confirm")
	s, loadErr := e.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if s.State.Regular.Guarantee.Status != "ACTIVE" || s.State.Regular.Plan.ConfirmedRevision != s.State.Regular.Requirement.Revision {
		t.Fatalf("route/guarantee not atomically ready: %+v", s.State.Regular)
	}
	p, _, err := e.Plan()
	if err != nil || p.Next.Kind != decision.KindWait || p.Next.Wait.Reason != "GIT_PREPARATION" {
		t.Fatalf("boundary=%+v err=%v", p, err)
	}
	if _, _, err = e.Drive(fingerprintOf(t, e)); err != nil {
		t.Fatal(err)
	}
	s, _ = e.Load()
	last := s.Revision
	for i := 0; i < 3; i++ {
		if _, _, err = e.Drive(fingerprintOf(t, e)); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = e.Load()
	if s.Revision != last {
		t.Fatal("ready replay mutated state")
	}
}
func TestRegularRejectsBeforeClaimAndIndependentRetry(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	s, _ := e.Load()
	bad := x.Claim
	bad.Provider = "other"
	ev, _ := NewRegularEvent("bad", RegularEvent{Operation: "CLAIM", ExpectedRevision: s.Revision, Claim: &bad})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("provider mismatch admitted")
	}
	s2, _ := e.Load()
	if s2.Revision != s.Revision {
		t.Fatal("rejected claim wrote state")
	}
	for attempt := 0; attempt < 3; attempt++ {
		x = currentExecution(t, e, "product-review")
		claimAndAttach(t, e, x)
		v := resultFor(x)
		v.Complete = false
		a := submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
		if a.Status != "REJECTED_RESULT" {
			t.Fatal(a)
		}
		sourceUnavailable(t, e, x)
	}
	s, _ = e.Load()
	if s.State.Regular.Reviews["product-review"].Completed != 0 || s.State.Regular.Reviews["start-readiness"].Completed != 0 {
		t.Fatal("invalid results consumed semantic rounds")
	}
	p, _, _ := e.Plan()
	if p.Next.Kind != decision.KindAsk {
		t.Fatal("retry exhaustion masqueraded as wait")
	}
	regularChoice(t, e, RegularRequest{Action: "authorize-retry", ReviewAction: "product-review", Reason: "Retry the corrected result contract once."}, "confirm")
	x = currentExecution(t, e, "product-review")
	if x.Number != 4 {
		t.Fatalf("extra retry number %d", x.Number)
	}
}
func TestRegularTokenCapStopsBeforeRefillAndAllowsOneTask(t *testing.T) {
	cap := int64(10)
	e := regularEngine(t, &cap)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	claimAndAttach(t, e, x)
	v := resultFor(x)
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
	adjudicatePass(t, e, "product-review")
	sourceFile := filepath.Join(t.TempDir(), "rollout.jsonl")
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": "native-" + x.Claim.ActionID}})
	data := string(meta) + "\n" + `{"type":"token_usage_record","payload":{"response_id":"resp-one","usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(sourceFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Status: "STOPPED", TranscriptPath: sourceFile}})
	s, _ := e.Load()
	if len(s.State.RegularExecutions) != 1 || s.State.Regular.Guard.UsedTokens != 15 || s.State.Regular.Guard.Status != "TOKEN_CAP" {
		t.Fatalf("cap=%+v", s.State.Regular.Guard)
	}
	regularChoice(t, e, RegularRequest{Action: "continue", Reason: "One additional review within the confirmed task set."}, "confirm")
	y := currentExecution(t, e, "start-readiness")
	claimAndAttach(t, e, y)
}
func TestRegularRequestFreshnessAndFrozenDocument(t *testing.T) {
	e := regularEngine(t, nil)
	s, _ := e.Load()
	q := RegularRequest{Action: "mode", Mode: "regular"}
	ev, _ := NewRegularEvent("early-mode", RegularEvent{Operation: "REQUEST", ExpectedRevision: s.Revision, Request: &q})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("mode selected before document confirmation")
	}
	q = RegularRequest{Action: "confirm-requirement"}
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &q})
	s, _ = e.Load()
	a := s.State.Regular.Ask
	d := RegularDecision{RequestID: a.ID, Token: "old", Digest: a.Digest, Choice: "confirm"}
	ev, _ = NewRegularEvent("stale-confirm", RegularEvent{Operation: "DECIDE", ExpectedRevision: s.Revision, Decision: &d})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "STALE_FRESHNESS") {
		t.Fatalf("stale confirm=%v", err)
	}
}

func TestRegularConcurrentClaimExactlyOnce(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	snap, _ := e.Load()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev, _ := NewRegularEvent(EventID(fmt.Sprintf("concurrent-%d", i)), RegularEvent{Operation: "CLAIM", ExpectedRevision: snap.Revision, Claim: &x.Claim})
			_, err := e.Submit(ev, fingerprintOf(t, e))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("claimed=%d", accepted)
	}
}
func TestRegularSourceUsageGuardRefillFaultWindows(t *testing.T) {
	points := []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultTempSyncBefore, persistence.FaultTempSyncAfter, persistence.FaultIntentAfter, persistence.FaultExecuteBefore, persistence.FaultReplaceAfter, persistence.FaultObserveBefore, persistence.FaultReconcileBefore, persistence.FaultCommitResponseLost}
	for _, point := range points {
		t.Run(string(point), func(t *testing.T) {
			dir := t.TempDir()
			e := regularEngineAt(t, nil, dir)
			regularStart(t, e)
			x := currentExecution(t, e, "product-review")
			claimAndAttach(t, e, x)
			raw := resultFor(x)
			submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
			adjudicatePass(t, e, "product-review")
			before, _ := e.Load()
			faultStore, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:b1", FaultInjector: func(p persistence.FaultPoint) error {
				if p == point {
					return &persistence.InjectedCrashError{Point: p}
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			crashed, err := New(faultStore, e.cfg, e.collect)
			if err != nil {
				t.Fatal(err)
			}
			src := ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Status: "UNAVAILABLE", Reason: "Fixture source diagnosed absent"}
			ev, _ := NewRegularEvent("fault-source", RegularEvent{Operation: "SOURCE", ExpectedRevision: before.Revision, Source: &src})
			if _, err = crashed.Submit(ev, fingerprintOf(t, e)); !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("fault not hit: %v", err)
			}
			store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:b1"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.Recover(); err != nil {
				t.Fatal(err)
			}
			recovered, err := New(store, e.cfg, e.collect)
			if err != nil {
				t.Fatal(err)
			}
			snap, err := recovered.Load()
			if err != nil {
				t.Fatal(err)
			}
			_, settled := snap.State.Cost.Dispatches[x.Claim.ActionID]
			refilled := len(snap.State.RegularExecutions) == 2
			if settled != refilled || (settled && snap.State.Regular.Guard.UsedAttempts != 2) || (!settled && snap.State.Regular.Guard.Status != "COST_PENDING") {
				t.Fatalf("partial usage/guard/refill: %+v", snap.State.Regular.Guard)
			}
			if _, err = recovered.Submit(ev, fingerprintOf(t, recovered)); err != nil {
				t.Fatal(err)
			}
			final, _ := recovered.Load()
			if len(final.State.RegularExecutions) != 2 || len(final.State.Cost.Dispatches) != 1 {
				t.Fatal("replay duplicated or lost work")
			}
		})
	}
}
func TestRegularAbortRetainsTerminalHistory(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	regularChoice(t, e, RegularRequest{Action: "abort", Reason: "User ended intake"}, "confirm")
	s, _ := e.Load()
	if s.State.Regular.Terminal != "ABORTED" || len(s.State.RegularExecutions) != 1 {
		t.Fatalf("abort=%+v", s.State.Regular)
	}
}

func TestRegularMinorRevisionThenCrossStageInvalidation(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	claimAndAttach(t, e, x)
	raw := resultFor(x)
	raw.Findings = []ReviewFinding{{ID: "P2-1", Severity: "P2", Category: "wording", Requirement: "REQ-001", Problem: "Acceptance omits exact output", NormalPath: "User runs output command", Consequence: "Output cannot be compared exactly", SeverityRationale: "Local acceptance wording", Remedy: "Specify hello as exact result"}}
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
	sourceUnavailable(t, e, x)
	snap, _ := e.Load()
	digest := snap.State.Regular.Reviews["product-review"].CurrentDigest
	submitRegular(t, e, RegularEvent{Operation: "ADJUDICATE", Adjudication: &ReviewAdjudication{Action: "product-review", ResultDigest: digest, CompletenessEvidence: "Complete requirement read", Findings: []FindingAdjudication{{FindingID: "P2-1", Decision: "VALID", Severity: "P2", Category: "wording", Reason: "Exact output is missing", Evidence: "AC-001 text"}}}})
	regularChoice(t, e, RegularRequest{Action: "finding", ReviewAction: "product-review", ResultDigest: digest, FindingID: "P2-1", Reason: "Specify exact output"}, "confirm")
	regularChoice(t, e, RegularRequest{Action: "requirement-change", Origin: "REVIEW_FINDING", Reason: "Apply confirmed finding"}, "confirm")
	doc := Document("requirements.md", []byte(strings.Replace(regularDocument, "输出包含结果。", "输出包含结果 hello。", 1)))
	regularChoice(t, e, RegularRequest{Action: "apply-requirement", Document: &doc, Meaning: "preserved", Origin: "REVIEW_FINDING", Impact: map[string]string{"product-review": "unaffected", "start-readiness": "affected"}, Reason: "Complete minor remedy and explicitly retain corrected product evidence", Delta: []ReviewDeltaHunk{{Old: "输出包含结果。", New: "输出包含结果 hello。", Source: "product-review:" + digest + ":P2-1", Reason: "Replaces the one acceptance line"}}}, "confirm")
	snap, _ = e.Load()
	series := snap.State.Regular.Reviews["product-review"]
	if series.Completed != 1 || series.Fresh || effectiveReview(snap.State.Regular, series) != "PASS" {
		t.Fatal("minor revision consumed review or failed to carry")
	}
	technical := currentExecution(t, e, "start-readiness")
	claimAndAttach(t, e, technical)
	regularChoice(t, e, RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "User changes intended result"}, "confirm")
	next := Document("requirements.md", []byte(strings.Replace(doc.Text, "hello", "goodbye", 1)))
	regularChoice(t, e, RegularRequest{Action: "apply-requirement", Document: &next, Meaning: "changed", Origin: "USER_INITIATED_CHANGE", Impact: map[string]string{"product-review": "affected", "start-readiness": "affected"}, Reason: "New exact output"}, "confirm")
	late := resultFor(technical)
	late.GranularityReview = planForReview(technical.Claim.RequirementRevision)
	accepted := submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &late})
	sourceUnavailable(t, e, technical)
	snap, _ = e.Load()
	if accepted.Status != "QUARANTINED" || snap.State.Regular.Reviews["start-readiness"].Completed != 0 || snap.State.Regular.Reviews["product-review"].Completed != 1 {
		t.Fatal("late cross-stage result changed semantic counts")
	}
	_ = currentExecution(t, e, "product-review")
}
func TestRegularThreeRoundsStayIndependent(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	for i := 0; i < 3; i++ {
		x := currentExecution(t, e, "product-review")
		claimAndAttach(t, e, x)
		raw := resultFor(x)
		submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
		sourceUnavailable(t, e, x)
		if i < 2 {
			regularChoice(t, e, RegularRequest{Action: "review-again", ReviewAction: "product-review", Reason: "User explicitly requests another complete review"}, "confirm")
		}
	}
	s, _ := e.Load()
	if s.State.Regular.Reviews["product-review"].Completed != 3 || s.State.Regular.Reviews["start-readiness"].Completed != 0 {
		t.Fatal("review series not independent")
	}
	q := RegularRequest{Action: "review-again", ReviewAction: "product-review", Reason: "Fourth requires explicit allowance"}
	ev, _ := NewRegularEvent("fourth", RegularEvent{Operation: "REQUEST", ExpectedRevision: s.Revision, Request: &q})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("unbounded fourth review")
	}
	regularChoice(t, e, RegularRequest{Action: "authorize-round", ReviewAction: "product-review", Reason: "User authorizes exactly one fourth product review"}, "confirm")
	s, _ = e.Load()
	if s.State.Regular.Reviews["product-review"].Limit != 4 || s.State.Regular.Reviews["start-readiness"].Limit != 3 {
		t.Fatal("extra round not action-specific")
	}
}

func TestRegularSourceFirstAndBindingRejectionRemainFinite(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	claimAndAttach(t, e, x)
	sourceUnavailable(t, e, x)
	s, _ := e.Load()
	if len(s.State.Cost.Dispatches) != 0 || len(s.State.RegularExecutions) != 1 {
		t.Fatal("stop alone settled or refilled a running result")
	}
	raw := resultFor(x)
	raw.Candidate = "wrong-model-echo"
	a := submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
	s, _ = e.Load()
	if a.Status != "REJECTED_RESULT" || len(s.State.Cost.Dispatches) != 1 || len(s.State.RegularExecutions) != 2 || s.State.Regular.Reviews["product-review"].Completed != 0 || s.State.Regular.RejectedResults[0].Raw.Candidate != "wrong-model-echo" {
		t.Fatal("invalid binding did not end one finite attempt and retain raw result")
	}
}
func TestRegularCorrectionInvalidatesDependenciesWithoutNewRound(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	x := currentExecution(t, e, "product-review")
	claimAndAttach(t, e, x)
	raw := resultFor(x)
	raw.Status = "FAIL"
	raw.Findings = []ReviewFinding{{ID: "P1", Severity: "P1", Category: "requirement-conflict", Problem: "Two output meanings", NormalPath: "Run command", Consequence: "Conflicting success condition", SeverityRationale: "Requirement conflict", Remedy: "Select one output"}}
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
	sourceUnavailable(t, e, x)
	s, _ := e.Load()
	digest := s.State.Regular.Reviews["product-review"].CurrentDigest
	submitRegular(t, e, RegularEvent{Operation: "ADJUDICATE", Adjudication: &ReviewAdjudication{Action: "product-review", ResultDigest: digest, CompletenessEvidence: "All conditions read", Findings: []FindingAdjudication{{FindingID: "P1", Decision: "DISMISSED", Severity: "P1", Category: "requirement-conflict", Reason: "Initial interpretation", Evidence: "Full requirement"}}}})
	y := currentExecution(t, e, "start-readiness")
	correction := ReviewCorrectionReceipt{Action: "product-review", ResultDigest: digest, FindingID: "P1", OldDecision: "DISMISSED", NewDecision: "VALID", OldSeverity: "P1", NewSeverity: "P1", Reason: "User corrects the interpretation against the two conditions"}
	submitRegular(t, e, RegularEvent{Operation: "CORRECT", Correction: &correction})
	s, _ = e.Load()
	if s.State.Regular.Reviews["product-review"].Completed != 1 || !s.State.Regular.Reviews["start-readiness"].Fresh {
		t.Fatal("correction counted a round or kept dependent result current")
	}
	ev, _ := NewRegularEvent("stale-technical", RegularEvent{Operation: "CLAIM", ExpectedRevision: s.Revision, Claim: &y.Claim})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("stale technical prerequisite executed")
	}
	ev, _ = NewRegularEvent("conflicting-correction", RegularEvent{Operation: "CORRECT", ExpectedRevision: s.Revision, Correction: &correction})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatal("conflicting correction accepted")
	}
}

// Historical evidence/classification promises now validate through the current
// engine result contract, with each missing field rejected independently.
func TestRegularReviewEvidenceAndTechnicalClassification(t *testing.T) {
	f := ReviewFinding{ID: "normal-output", Severity: "P1", Category: "acceptance-entry", Requirement: "AC-001", Problem: "output differs", NormalPath: "go run .", Consequence: "the documented result is wrong", SeverityRationale: "the sole required behavior fails", Remedy: "correct the output"}
	v := RawReviewCandidate{Action: "start-readiness", RequirementRevision: "revision", Status: "FAIL", Complete: true, Message: "entire requirement inspected", Findings: []ReviewFinding{f}, GranularityReview: planForReview("revision")}
	for _, category := range []string{"requirement-conflict", "responsibility-dependency", "acceptance-entry", "implementation-detail", "unknown"} {
		for _, severity := range []string{"P0", "P1", "P2", "P3"} {
			t.Run(category+"/"+severity, func(t *testing.T) {
				q := v
				q.Findings = []ReviewFinding{f}
				q.Findings[0].Category = category
				q.Findings[0].Severity = severity
				if severity == "P2" || severity == "P3" {
					q.Status = "PASS"
				}
				err := validateReview(nil, q)
				want := category != "unknown" && (category != "implementation-detail" || (severity == "P2" || severity == "P3"))
				if (err == nil) != want {
					t.Fatalf("classification acceptance=%v want=%v: %v", err == nil, want, err)
				}
			})
		}
	}
	for _, field := range []string{"problem", "normalPath", "consequence", "severityRationale", "remedy"} {
		t.Run(field, func(t *testing.T) {
			q := v
			b, _ := json.Marshal(f)
			var data map[string]any
			_ = json.Unmarshal(b, &data)
			data[field] = ""
			b, _ = json.Marshal(data)
			var missing ReviewFinding
			_ = json.Unmarshal(b, &missing)
			q.Findings = []ReviewFinding{missing}
			if validateReview(nil, q) == nil {
				t.Fatalf("missing %s accepted", field)
			}
		})
	}
}

// These are protocol fixtures, not native review claims. Their wrong values
// reproduce the observed answers without changing those original results.
func TestTechnicalReviewPreservesBindingsAndBatchDependencies(t *testing.T) {
	for _, scenario := range []string{"single-batch", "cross-batch", "wrong-action", "wrong-revision", "wrong-provider", "wrong-candidate", "subtask-dependency", "environment-dependency", "prose-dependency"} {
		t.Run(scenario, func(t *testing.T) {
			e := regularEngine(t, nil)
			regularStart(t, e)
			product := currentExecution(t, e, "product-review")
			claimAndAttach(t, e, product)
			v := resultFor(product)
			submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
			sourceUnavailable(t, e, product)
			adjudicatePass(t, e, "product-review")
			x := currentExecution(t, e, "start-readiness")
			claimAndAttach(t, e, x)
			v = resultFor(x)
			v.GranularityReview = planForReview(x.Claim.RequirementRevision)
			v.GranularityReview.Batches[0].Dependencies = []string{}
			v.GranularityReview.Batches[0].Subtasks = []DevelopmentSubtask{{ID: "S1", Task: "Create the root command"}, {ID: "S2", Task: "Validate its exact process output"}}
			switch scenario {
			case "cross-batch":
				second := v.GranularityReview.Batches[0]
				second.ID = "B2"
				second.Subtasks = []DevelopmentSubtask{{ID: "S3", Task: "Validate the committed command"}}
				second.Dependencies = []string{"B1"}
				v.GranularityReview.Batches = append(v.GranularityReview.Batches, second)
			case "wrong-action":
				v.Action = "start-readiness-review"
			case "wrong-revision":
				v.RequirementRevision = v.RequirementRevision[:48] + v.RequirementRevision[49:]
			case "wrong-provider":
				v.Provider = "other"
			case "wrong-candidate":
				v.Candidate = "different-candidate"
			case "subtask-dependency":
				v.GranularityReview.Batches[0].Dependencies = []string{"S1"}
			case "environment-dependency":
				v.GranularityReview.Batches[0].Dependencies = []string{"Go toolchain availability"}
			case "prose-dependency":
				v.GranularityReview.Batches[0].Dependencies = []string{"S2 depends on S1 completing the root go.mod and main.go."}
			}
			a := submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
			s, err := e.Load()
			if err != nil {
				t.Fatal(err)
			}
			series := s.State.Regular.Reviews["start-readiness"]
			if scenario == "single-batch" || scenario == "cross-batch" {
				if a.Status == "REJECTED_RESULT" || series.CurrentDigest == "" || series.Completed != 1 {
					t.Fatalf("valid plan refused: %+v", s.State.Regular.RejectedResults)
				}
			} else {
				rejected := s.State.Regular.RejectedResults
				if a.Status != "REJECTED_RESULT" || len(rejected) != 1 || series.CurrentDigest != "" || series.Completed != 0 || !s.State.RegularExecutions[x.Claim.ActionID].Finished {
					t.Fatalf("invalid answer accepted or left live: %+v", a)
				}
				raw := rejected[0].Raw
				if raw.Action != v.Action || raw.RequirementRevision != v.RequirementRevision || raw.Provider != v.Provider || raw.Candidate != v.Candidate || strings.Join(raw.GranularityReview.Batches[0].Dependencies, "|") != strings.Join(v.GranularityReview.Batches[0].Dependencies, "|") {
					t.Fatal("rejected answer was normalized")
				}
			}
		})
	}
}
