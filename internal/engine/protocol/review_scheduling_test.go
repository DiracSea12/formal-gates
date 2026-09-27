package protocol

import (
	"fmt"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	definition "formal-gates/internal/engine/testkit/workflow"
)

func TestReviewIssuanceRespectsZeroCapacity(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 0}, fingerprintOf(t, engine))
	if err != nil {
		t.Fatal(err)
	}
	state := engine.LoadMustSucceed(t).State
	if len(issued) != 0 || len(state.PendingActions) != 0 || state.ReviewSeries.ProductReview.Issued != 0 {
		t.Fatalf("zero capacity signed work: issued=%+v, pending=%+v", issued, state.PendingActions)
	}
}

func TestReviewPlanWaitsForItsWorkerDependency(t *testing.T) {
	cd, reg := agentStepsDefinition(t, func(name string) authoring.AgentSpec {
		spec := authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute}
		if name == "product.review" {
			spec.Reason = authoring.ReasonIndependentReview
			spec.ReviewSeries = authoring.ReviewSeriesProductReview
		}
		return spec
	}, "prepare.document", "product.review")
	engine, _ := reviewEngineCapacity(t, cd, reg, 1)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindReady || len(plan.Next.Ready.Tasks) != 1 || plan.Next.Ready.Tasks[0].Step != "prepare.document" {
		t.Fatalf("review escaped dependency in plan: %+v", plan.Next)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fingerprintOf(t, engine))
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 1 || issued[0].Task != plan.Next.Ready.Tasks[0].Task || issued[0].Step != "prepare.document" {
		t.Fatalf("issuance changed the eligible set: %+v", issued)
	}
	state := engine.LoadMustSucceed(t).State
	if len(state.PendingActions) != 1 || state.ReviewSeries.ProductReview.Issued != 0 {
		t.Fatalf("ineligible review persisted: %+v", state.PendingActions)
	}
}

func TestReviewRedoRemainsInPlanUntilAccepted(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	first := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "initial-pass", first, reviewEvidenceAt(review.ProductReview, first, "rev-1", "PASS", "initial review", nil))
	redoProductReview(t, engine, "requirement-change")
	state := engine.LoadMustSucceed(t).State
	fresh := pendingActionID(t, engine)
	if !state.ReviewSeries.ProductReview.FreshOwed || state.PendingActions[fresh].Task == state.SpawnReceipts[first].Task {
		t.Fatalf("redo did not retain its new work identity: %+v", state.PendingActions)
	}
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindWait {
		t.Fatalf("pending redo plan = %s, want WAIT", plan.Next.Kind)
	}
	submitReviewResult(t, engine, "fresh-pass", fresh, reviewEvidenceAt(review.ProductReview, fresh, state.RequirementRevision(), "PASS", "new revision reviewed", nil))
	plan, _, err = engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindComplete {
		t.Fatalf("settled redo plan = %s, want COMPLETE", plan.Next.Kind)
	}
}

func TestReviewInvalidationOrderPreservesEvidenceAndNewWork(t *testing.T) {
	for _, order := range [][]int{{0, 1}, {1, 0}, {0, 2, 1}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			engine, _ := reviewEngine(t, cd, reg)
			for i := 0; i < len(order); i++ {
				action := ""
				if i == 0 {
					action = issueReady(t, engine)[0]
				} else {
					redoProductReview(t, engine, fmt.Sprintf("revision-%d", i))
					action = pendingActionID(t, engine)
				}
				revision := engine.LoadMustSucceed(t).State.RequirementRevision()
				submitReviewResult(t, engine, fmt.Sprintf("result-%d", i), action, reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "review complete", nil))
			}
			candidates := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.RawCandidates
			for _, i := range order {
				invalidateRoundOf(t, engine, fmt.Sprintf("invalidate-%d", i), candidates[i].DispatchID)
			}
			state := engine.LoadMustSucceed(t).State
			series := state.ReviewSeries.ProductReview
			if series.Completed != 0 || series.Effective.Status != "PENDING" || series.Effective.ResultDigest != candidates[len(candidates)-1].ResultDigest || len(series.RawCandidates) != len(order) || len(series.Corrections) != len(order) {
				t.Fatalf("invalidation lost evidence or conclusion: %+v", series)
			}
			for _, candidate := range candidates {
				if !review.ResultExcluded(series, candidate.ResultDigest) {
					t.Fatalf("candidate remains effective: %+v", candidate)
				}
			}
			plan, _, err := engine.Plan()
			if err != nil {
				t.Fatal(err)
			}
			if plan.Next.Kind == decision.KindComplete {
				t.Fatal("invalidated review returned COMPLETE")
			}
			fresh := pendingActionID(t, engine)
			submitReviewResult(t, engine, "replacement-pass", fresh, reviewEvidenceAt(review.ProductReview, fresh, state.RequirementRevision(), "PASS", "replacement review", nil))
			series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			if series.Completed != 1 || series.Effective.Status != "PASS" || len(series.RawCandidates) != len(order)+1 || len(series.Corrections) != len(order) {
				t.Fatalf("replacement did not preserve history: %+v", series)
			}
		})
	}
}

func TestReviewUsesEligibleSiblingCarrier(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "prepare.document", deps: []string{"prep.parse"}},
		dagStep{name: "review.a", deps: []string{"prepare.document"}},
		dagStep{name: "review.b", deps: []string{"prep.parse"}},
	)
	for i := range cd.Steps {
		if cd.Steps[i].Header.ID == "prepare.document" {
			payload := cd.Steps[i].Payload.(compiler.CompiledAgentStep)
			payload.ReviewSeries = ""
			payload.Reason = authoring.ReasonSemanticJudgment
			cd.Steps[i].Payload = payload
		}
	}
	engine, _ := reviewEngineCapacity(t, cd, reg, 2)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindReady || len(plan.Next.Ready.Tasks) != 2 || plan.Next.Ready.Tasks[0].Step != "prepare.document" || plan.Next.Ready.Tasks[1].Step != "review.b" {
		t.Fatalf("independent eligible carrier was suppressed: %+v", plan.Next)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 2}, fingerprintOf(t, engine))
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 2 || issued[0].Task != plan.Next.Ready.Tasks[0].Task || issued[1].Task != plan.Next.Ready.Tasks[1].Task {
		t.Fatalf("issued set differs from plan: %+v", issued)
	}
	state := engine.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.Issued != 1 || len(state.PendingActions) != 2 {
		t.Fatalf("one series obligation was not preserved: %+v", state.PendingActions)
	}
}

func reviewDependencyDefinition(t *testing.T, steps ...dagStep) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	reg := compiler.NewRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	must(reg.RegisterHandler("engine.test.prep", authoring.RunnerEngineLocal))
	must(reg.RegisterHandler("engine.test.reviewer", authoring.RunnerAgentWorker))
	must(reg.RegisterCodec("codec.any.in"))
	must(reg.RegisterCodec("codec.any.out"))
	must(reg.RegisterPredicate("pred.test.post"))
	header := func(id string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: "review", Dependencies: ds, DefinitionVersion: definition.Version}
	}
	io := func(bindings ...string) authoring.IO {
		inputs := make([]authoring.InputBinding, 0, len(bindings))
		for _, b := range bindings {
			inputs = append(inputs, authoring.InputBinding{From: authoring.StepID(b), OutputField: "out", ToField: "in"})
		}
		return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
	}
	reviewStep := func(name string, deps ...string) authoring.Step {
		stepIO := io(deps...)
		stepIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
		spec := authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview, ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute}
		if name == "consume.review" {
			spec.ReviewSeries = ""
			spec.Reason = authoring.ReasonSemanticJudgment
		}
		step, err := authoring.NewAgentStep(header(name, deps...), stepIO, spec)
		must(err)
		return step
	}
	prep, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.test.prep"})
	must(err)
	definitionSteps := []authoring.Step{prep}
	for _, step := range steps {
		definitionSteps = append(definitionSteps, reviewStep(step.name, step.deps...))
	}
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: definitionSteps}, reg)
	must(err)
	return cd, reg
}

func TestSharedReviewObligationHoldsSiblingDependents(t *testing.T) {
	cd, reg := reviewDependencyDefinition(t,
		dagStep{name: "review.a", deps: []string{"prep.parse"}},
		dagStep{name: "review.b", deps: []string{"prep.parse"}},
		dagStep{name: "consume.review", deps: []string{"review.b"}},
	)
	e, _ := reviewEngineCapacity(t, cd, reg, 0)
	plan, _, err := e.Plan()
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := e.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fingerprintOf(t, e))
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 1 {
		t.Fatalf("bad initial frontier: %+v", issued)
	}
	id := issued[0].ActionID
	submitReviewResult(t, e, "first-pass", id, reviewEvidenceAt(review.ProductReview, id, "rev-1", "PASS", "complete", nil))
	redoProductReview(t, e, "review-updated")
	plan, _, err = e.Plan()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plan=%+v required=%+v", plan.Next.Ready, e.LoadMustSucceed(t).State.decisionView(cd).RequiredTasks)
	issued, _, err = e.IssueFromPlan(plan, decision.Admission{Capacity: 2}, fingerprintOf(t, e))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("issued=%+v", issued)
	for _, task := range issued {
		if task.Step == "consume.review" {
			t.Fatal("downstream issued while its review series owes a fresh result")
		}
	}
}
