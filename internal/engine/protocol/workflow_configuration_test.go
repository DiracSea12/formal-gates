package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func selectConfiguration(t *testing.T, e *Engine, id string, selected ConfigurationSelection) (Event, Event) {
	t.Helper()
	gates := []string{"blackbox", "whitebox", "quality"}
	request := Event{ID: EventID(id), Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectRoute, RouteGates: gates, Configuration: &selected, Options: routeOptions()}}
	if _, err := e.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := e.Freshness(RequestID(id))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewReviewDecideEvent(EventID(id+":decision"), RequestID(id), token, "approve", ReviewDecisionPayload{Route: &RouteDecision{Gates: gates, Selected: selected.SelectedGates(gates), Configuration: &selected}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(decision, Observation{}); err != nil {
		t.Fatal(err)
	}
	return request, decision
}

func TestConfigurationStopsClosedReviewAndReopensThroughOriginalContinuation(t *testing.T) {
	t.Parallel()
	e, dir := selectedReviewEngine(t, nil)
	original := issueReady(t, e)[0]
	for _, event := range []Event{spawnReceipt(t, "claimed-review", original, "original-review-owner"), lifecycleEvent(t, "review-start", "original-review-owner", LifecycleStart)} {
		if _, err := e.Submit(event, Observation{}); err != nil {
			t.Fatal(err)
		}
	}
	originalAttempt := e.LoadMustSucceed(t).State.Attempts[e.LoadMustSucceed(t).State.PendingActions[original].Task]
	selected := ConfigurationSelection{Name: "chosen", Configuration: settings.DefaultConfiguration()}
	selected.Configuration.FlowSelection = &FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}
	_, decision := selectConfiguration(t, e, "close-review", selected)
	closed := e.LoadMustSucceed(t).State
	if _, known := closed.ObsoleteActions[original]; !known || closed.Decisions["close-review"].Continuation.AppliedRevision != 0 || closed.Topology != nil || closed.RouteConfirmed {
		t.Fatal("configuration did not retain the original owner until its stop", closed.Decisions["close-review"])
	}
	if closed.Attempts[originalAttempt.Task].ID != originalAttempt.ID {
		t.Fatal("closing a claimed review discarded its Attempt")
	}
	if _, err := e.Submit(lifecycleEvent(t, "review-stop", "original-review-owner", LifecycleStop), Observation{}); err != nil {
		t.Fatal(err)
	}
	late, err := NewReviewWorkerResultEvent("late-closed-review", original, testProvider, "late-digest", reviewEvidence(review.ProductReview, "late-dispatch", "", "PASS", "late closed work", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(late, Observation{}); err != nil {
		t.Fatal(err)
	}
	stopped := e.LoadMustSucceed(t).State
	if stopped.Decisions["close-review"].Continuation.AppliedRevision == 0 || stopped.ReviewSeries.ProductReview.Completed != 0 || len(stopped.ObsoleteResults[original]) != 1 {
		t.Fatal("closed review became effective or its evidence disappeared")
	}
	selected.Configuration.FlowSelection = &FlowSelection{Disabled: []string{"start-readiness", "development"}}
	selectConfiguration(t, e, "open-review", selected)
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	actions := issueReady(t, reopened)
	if len(actions) == 0 {
		for id := range reopened.LoadMustSucceed(t).State.PendingActions {
			actions = append(actions, id)
		}
	}
	if len(actions) != 1 || actions[0] == original {
		t.Fatalf("reopened review did not get a fresh task: %v", actions)
	}
	submitVerifiedReviewResult(t, reopened, "reopened-review-pass", actions[0], reviewEvidence(review.ProductReview, "reopened-dispatch", "", "PASS", "current reopened review", nil))
	if reopened.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Completed != 1 {
		t.Fatal("reopened review did not complete its own round")
	}
	preview, err := reopened.NodeContinuationPreview("product-review")
	if err != nil || preview.To.Unavailable != "" {
		t.Fatal("re-enabled past review is not a continuation target", err, preview)
	}
	request := Event{ID: "continue-earlier-review", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlContinueNode, Continuation: &NodeContinuationRequest{Target: preview.To.ID, StateRevision: preview.StateRevision, PreviewDigest: preview.Digest}, Options: []AskOption{{ID: "approve", Label: "Continue"}}}}
	if _, err := reopened.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := reopened.Freshness(RequestID(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	resume := Event{ID: "continue-earlier-review:decision", Kind: KindDecide, Decide: &DecidePayload{Request: RequestID(request.ID), Token: token, Choice: "approve", Continuation: &NodeContinuationDecision{PreviewDigest: preview.Digest, FreshnessToken: token, Reason: "Run the earlier review again", Work: preview.Work}}}
	if _, err := reopened.Submit(resume, Observation{}); err != nil {
		t.Fatal(err)
	}
	next := reopened.LoadMustSucceed(t).State
	var fresh string
	for id := range next.PendingActions {
		if id != original && id != actions[0] {
			fresh = id
		}
	}
	if fresh == "" {
		freshActions := issueReady(t, reopened)
		if len(freshActions) > 0 {
			fresh = freshActions[0]
		}
	}
	if fresh == "" || fresh == actions[0] {
		t.Fatal("continuation did not dispatch the earlier responsibility")
	}
	submitVerifiedReviewResult(t, reopened, "continued-review-pass", fresh, reviewEvidence(review.ProductReview, "continued-dispatch", "", "PASS", "continued earlier review", nil))
	before := stateBytesOf(t, dir)
	if _, err := reopened.Submit(decision, Observation{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("configuration replay changed recovered work or accounting")
	}
}

func TestConfigurationTupleUsesRevisionWithoutStoppingClaimedWork(t *testing.T) {
	t.Parallel()
	e, _ := selectedReviewEngine(t, nil)
	snapshot := e.LoadMustSucceed(t)
	snapshot.State.RunProvider = host.Codex
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	action := issueReady(t, e)[0]
	claim, err := NewSpawnReceiptEvent("claimed-tuple", action, host.Codex, "tuple-owner", SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(claim, Observation{}); err != nil {
		t.Fatal(err)
	}
	before := e.LoadMustSucceed(t).State
	attempt := before.Attempts[before.PendingActions[action].Task]
	selected := ConfigurationSelection{Configuration: settings.DefaultConfiguration()}
	selected.Configuration.Execution = map[string]ExecutionChoice{"product-review": {Provider: host.Claude, Model: "suite-model", ReasoningEffort: "high"}}
	selectConfiguration(t, e, "suite-execution", selected)
	after := e.LoadMustSucceed(t).State
	if !reflect.DeepEqual(after.Attempts[attempt.Task], attempt) || len(after.ObsoleteActions) != 0 || after.EffectiveExecutionChoice("product-review") != selected.Configuration.Execution["product-review"] {
		t.Fatal("suite tuple change interrupted the current original execution")
	}
	selectExecution(t, e, "later-node-execution", ExecutionSelection{Node: "product-review", Choice: ExecutionChoice{Provider: host.Codex, Model: "node-model"}})
	current := e.LoadMustSucceed(t).State.EffectiveConfiguration()
	if current.Configuration.Execution["product-review"].Model != "node-model" {
		t.Fatal("current suite lost a later node choice")
	}
	current.Configuration.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
	selectConfiguration(t, e, "close-future-repair", current)
	after = e.LoadMustSucceed(t).State
	if after.EffectiveExecutionChoice("product-review").Model != "node-model" || !reflect.DeepEqual(after.Attempts[attempt.Task], attempt) {
		t.Fatal("an unrelated node edit reset the tuple or claimed Attempt")
	}
	selectConfiguration(t, e, "named-suite-after-node", selected)
	after = e.LoadMustSucceed(t).State
	if after.EffectiveExecutionChoice("product-review") != selected.Configuration.Execution["product-review"] || !reflect.DeepEqual(after.Attempts[attempt.Task], attempt) || !reflect.DeepEqual(after.Cost, before.Cost) {
		t.Fatal("a later suite did not own future tuples or changed the claimed execution/accounting")
	}
}

func TestConfigurationPreviewKeepsCompletedDevelopmentAndReplacesOnlyOpenWork(t *testing.T) {
	t.Parallel()
	e, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v6")
	selected := ConfigurationSelection{Configuration: settings.DefaultConfiguration()}
	selected.Configuration.FlowSelection = &FlowSelection{Disabled: []string{"development"}}
	preview, err := e.configurationPreview(state, selected, []string{"quality"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	var disposition string
	for _, choice := range preview.Work {
		if choice.Work.ActionID == pending.ActionID {
			disposition = choice.Disposition
		}
	}
	if disposition != RequirementWorkRedo {
		t.Fatal("closed in-flight development did not use the original replacement owner")
	}
	state.Results[pending.ActionID] = WorkerResult{ActionID: pending.ActionID, Outcome: OutcomePass, Digest: "completed-development"}
	delete(state.PendingActions, pending.ActionID)
	delete(state.Attempts, pending.Task)
	state.Tasks[pending.Task] = runtime.TaskTerminal
	state.Validation.DevelopmentSnapshot = "completed-native-source"
	preview, err = e.configurationPreview(state, selected, []string{"quality"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range preview.Work {
		if choice.Work.ActionID == pending.ActionID {
			found = true
			if choice.Disposition != RequirementWorkCarry || choice.Work.ResultDigest != "completed-development" {
				t.Fatal("closing future development invalidated an already completed source")
			}
		}
	}
	if !found || state.Validation.DevelopmentSnapshot != "completed-native-source" {
		t.Fatal("completed development evidence was lost")
	}
}

func TestConfigurationKeepsTemporaryNativeOmissionsThroughUnrelatedEdits(t *testing.T) {
	t.Parallel()
	state := State{RunProvider: host.Codex, Settings: &settings.Effective{Values: settings.Values{Model: "configured-model", ReasoningEffort: "high"}}, Decisions: map[string]RecordedDecision{"native": {Control: ControlSelectExecution, Choice: "approve", Revision: 2, Execution: &ExecutionSelection{Node: "development", Choice: ExecutionChoice{Provider: host.Codex}}}}}
	selected := state.EffectiveConfiguration()
	selected.Configuration.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
	state.Decisions["edit"] = RecordedDecision{Control: ControlSelectRoute, Choice: "approve", Revision: 3, Review: &ReviewDecisionPayload{Route: &RouteDecision{Configuration: &selected}}}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(body, &recovered); err != nil {
		t.Fatal(err)
	}
	if got := recovered.EffectiveExecutionChoice("development"); got != (ExecutionChoice{Provider: host.Codex}) {
		t.Fatalf("current suite refilled native omissions: %+v", got)
	}
}

func TestConfirmedRouteChangesRequirePinnedConfiguration(t *testing.T) {
	t.Parallel()
	e, dir := sliceChildEngine(t)
	gates := []string{"quality", "security"}
	askAndDecide(t, e, ControlSelectRoute, "initial-selection", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Gates: gates, Selected: []string{"quality"}}})
	request, err := NewRouteRequestEvent("unpreviewed-change", gates, routeOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	if _, err := e.Submit(request, Observation{}); err == nil {
		t.Fatal("confirmed route accepted a change without its complete configuration preview")
	}
	if !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("unpreviewed change wrote state")
	}
}

func TestNewlySelectedQAUsesPreparationBeforeFreezingValidation(t *testing.T) {
	t.Parallel()
	cd := compiledCanonicalWorkflow(t)
	e := &Engine{cfg: Config{Definition: cd}}
	candidate := coverage.ValidationCandidate{Identity: "native-source", Digest: "tree"}
	state := &State{RunID: "run", RouteConfirmed: true, SelectedGates: []string{"quality"}, Validation: &Validation{Candidate: &candidate, Sets: map[coverage.ReviewKind]*CaseSet{}}}
	if !e.freezeValidationWave(state, coverage.ScopeFull, "", nil) {
		t.Fatal("ordinary gate wave did not freeze")
	}
	original := state.currentValidationWave().ID
	state.SelectedGates = []string{"blackbox", "whitebox", "quality"}
	e.rebindValidationRoute(state)
	if state.Validation.Candidate != nil || state.currentValidationWave().ID != original || state.currentValidationWave().Status != "INVALIDATED" || len(state.Validation.Sets) != 0 {
		t.Fatal("new QA bypassed its original design and review preparation")
	}
}

func TestEquivalentFlowSelectionsRetainTheValidationRoute(t *testing.T) {
	t.Parallel()
	state := State{RouteConfirmed: true, SelectedGates: []string{"quality"}}
	original := state.validationRouteDigest()
	state.FlowSelection = &FlowSelection{}
	if state.validationRouteDigest() != original {
		t.Fatal("enabling an already enabled responsibility changed the route")
	}
	state.FlowSelection.Disabled = []string{"repair", "development"}
	original = state.validationRouteDigest()
	state.FlowSelection.Disabled = []string{"development", "repair"}
	if state.validationRouteDigest() != original {
		t.Fatal("equivalent workflow selections changed validation obligations")
	}
}
