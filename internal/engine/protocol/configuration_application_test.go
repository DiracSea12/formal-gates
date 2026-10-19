package protocol

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

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
		t.Fatal("closed unfinished development lost its replacement disposition")
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
				t.Fatal("configuration invalidated completed development")
			}
		}
	}
	if !found || state.Validation.DevelopmentSnapshot != "completed-native-source" {
		t.Fatal("completed development evidence was lost")
	}
}

func TestWorkProvenanceComesFromConfigurationOrNodeDecision(t *testing.T) {
	for _, owner := range []string{"configuration", "node-decision"} {
		t.Run(owner, func(t *testing.T) {
			_, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v6")
			state.normalizeCollections()
			decisions := len(state.Decisions)
			work := RequirementWorkDecision{Work: RequirementWork{ActionID: pending.ActionID, Task: pending.Task}, Disposition: RequirementWorkRedo, Reason: "Replace unfinished work"}
			preview := &NodeContinuationPreview{To: BusinessNode{Kind: "development"}, RequirementRevision: state.RequirementRevision(), Work: []RequirementWorkDecision{work}}
			effect := &NodeContinuationDecision{AppliedRevision: 2, Work: preview.Work}
			if owner == "configuration" {
				state.AppliedConfigurations = append(state.AppliedConfigurations, AppliedConfiguration{Revision: 2, Preview: preview, Effect: effect})
			} else {
				state.PendingAsks["continue-work"] = PendingAsk{Control: ControlContinueNode, Continuation: preview, Resolved: true}
				state.Decisions["continue-work"] = RecordedDecision{RequestID: "continue-work", Control: ControlContinueNode, Choice: "approve", Revision: 2, Continuation: effect}
			}
			replacement := replacementWorkTask(ConfirmedRevision{StateRevision: 2, WorkDecisions: preview.Work}, 0)
			if source := state.sourceValidationTask(replacement); source != pending.Task {
				t.Fatal("replacement lost its issued work source", source, pending.Task)
			}
			if owner == "configuration" && len(state.Decisions) != decisions {
				t.Fatal("a configuration application fabricated a user decision")
			}
		})
	}
}

func TestNewlySelectedQAUsesPreparationBeforeFreezingValidation(t *testing.T) {
	t.Parallel()
	e := &Engine{cfg: Config{Definition: compiledCanonicalWorkflow(t)}}
	candidate := coverage.ValidationCandidate{Identity: "native-source", Digest: "tree"}
	state := &State{RunID: "run", RouteConfirmed: true, SelectedGates: []string{"quality"}, Validation: &Validation{Candidate: &candidate, Sets: map[coverage.ReviewKind]*CaseSet{}}}
	if !e.freezeValidationWave(state, coverage.ScopeFull, "", nil) {
		t.Fatal("ordinary gate wave did not freeze")
	}
	original := state.currentValidationWave().ID
	state.SelectedGates = []string{"blackbox", "whitebox", "quality"}
	e.rebindValidationRoute(state)
	if state.Validation.Candidate != nil || state.currentValidationWave().ID != original || state.currentValidationWave().Status != "INVALIDATED" || len(state.Validation.Sets) != 0 {
		t.Fatal("new QA bypassed design and review preparation")
	}
}

func TestEquivalentFlowSelectionsRetainTheValidationRoute(t *testing.T) {
	t.Parallel()
	state := State{RouteConfirmed: true, SelectedGates: []string{"quality"}}
	original := state.validationRouteDigest()
	state.FlowSelection = &FlowSelection{}
	if state.validationRouteDigest() != original {
		t.Fatal("enabling an enabled responsibility changed the route")
	}
	state.FlowSelection.Disabled = []string{"repair", "development"}
	original = state.validationRouteDigest()
	state.FlowSelection.Disabled = []string{"development", "repair"}
	if state.validationRouteDigest() != original {
		t.Fatal("equivalent selections changed the validation obligation")
	}
}

func TestSliceReceiptQualificationUsesItsAppliedConfigurationRevision(t *testing.T) {
	topology := TopologyBinding{Decision: topologySplit, Slices: []string{"core"}}
	parent := &State{RunID: "master", Topology: &topology, SliceChildren: map[string]string{"core": "child"}}
	receipt := SliceReceipt{Receipt: SliceTerminalReceipt{Status: runtime.PhaseSliceReady, ChildRunID: "child", MasterRunID: "master", SliceID: "core", Topology: topology, ConfigurationRevision: 1}}
	if !parent.SliceReceiptQualified(receipt) {
		t.Fatal("matching checkpoint was not qualified")
	}
	frozen := receipt.Receipt
	receipt.Configuration = &AppliedConfiguration{Revision: 2, RouteChanged: true, Effect: &NodeContinuationDecision{AppliedRevision: 2}}
	if parent.SliceReceiptQualified(receipt) || !reflect.DeepEqual(receipt.Receipt, frozen) {
		t.Fatal("a new route retained the old qualification or rewrote its checkpoint")
	}
	receipt.Receipt.ConfigurationRevision = 2
	if !parent.SliceReceiptQualified(receipt) {
		t.Fatal("checkpoint from the applied route did not regain qualification")
	}
}

func TestTransientLibraryReadStillIssuesTheLastAppliedModel(t *testing.T) {
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, executionLibrary("product-review", ExecutionChoice{Model: "applied-model"}))
	before := e.LoadMustSucceed(t)
	library.value.Execution["product-review"] = ExecutionChoice{Model: "unread-model"}
	library.err = errors.New("configuration file is being replaced")
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if len(state.PendingActions) != 1 || !reflect.DeepEqual(state.AppliedConfigurations, before.State.AppliedConfigurations) {
		t.Fatal("transient read stopped issuance or changed applied history")
	}
	for id := range state.PendingActions {
		choice, known := state.ActionExecutionChoice(id)
		if !known || choice.Model != "applied-model" {
			t.Fatal("transient read did not use the last applied model", choice)
		}
	}
}

func TestUnavailableLibraryAcceptsIssuedResultsWithoutRefilling(t *testing.T) {
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	action := issueReady(t, e)[0]
	library.err = &ConfigurationUnavailable{Reason: "config.json: invalid JSON"}
	submitVerifiedReviewResult(t, e, "result-while-unavailable", action, plannedReviewEvidence(review.ProductReview, action, "", "PASS", "complete", nil))
	state := e.LoadMustSucceed(t).State
	if len(state.ReviewSeries.ProductReview.RawCandidates) != 1 || len(state.PendingActions) != 0 {
		t.Fatal("configuration failure rejected an issued result or refilled new work")
	}
	plan, _, err := e.Plan(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Detail != "config.json: invalid JSON" {
		t.Fatal("result acceptance hid the configuration cause", plan, err)
	}
	library.err = nil
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if len(e.LoadMustSucceed(t).State.PendingActions) != 1 {
		t.Fatal("repairing the library did not resume the remaining responsibility")
	}
}

func TestConfigurationNilGatesTracksCatalogueGrowth(t *testing.T) {
	e, _ := readyReviewEngine(t)
	catalogue := []string{"quality"}
	e.cfg.RouteGates = func() ([]string, error) { return slices.Clone(catalogue), nil }
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	before := e.LoadMustSucceed(t)
	catalogue = append(catalogue, "security")
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	after := e.LoadMustSucceed(t)
	entry := after.State.AppliedConfigurations[len(after.State.AppliedConfigurations)-1]
	if after.Revision <= before.Revision || !slices.Equal(after.State.SelectedGates, catalogue) || !slices.Equal(entry.Changed, []string{"gates"}) || entry.Configuration.Gates != nil {
		t.Fatal("all-gates binding did not include the new discovered gate", entry)
	}
	none := []string{}
	library.value.Gates = &none
	reconcile(t, e)
	count := len(e.LoadMustSucceed(t).State.AppliedConfigurations)
	catalogue = append(catalogue, "another")
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if len(state.SelectedGates) != 0 || len(state.AppliedConfigurations) != count || state.AppliedConfiguration().Gates == nil {
		t.Fatal("explicit no-gates silently became all-gates")
	}
}

func TestConfigurationClosurePreservesClaimedReviewUntilNormalResult(t *testing.T) {
	for _, nativeClaim := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "native-claimed"}[nativeClaim], func(t *testing.T) {
			e, _ := readyReviewEngine(t)
			library := bindLibrary(t, e, settings.DefaultConfiguration())
			action := issueReady(t, e)[0]
			original := e.LoadMustSucceed(t).State
			pending := original.PendingActions[action]
			e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
				if p.ActionID != action || a.ID != pending.AttemptID {
					t.Fatal("claim lookup lost action identity")
				}
				if nativeClaim {
					return &SpawnReceiptPayload{ActionID: action, Provider: testProvider, Status: SpawnStatusUnknown}, nil
				}
				return nil, nil
			}
			library.value.FlowSelection = &FlowSelection{Disabled: []string{"product-review"}}
			reconcile(t, e)
			closing := e.LoadMustSucceed(t).State
			entry := closing.AppliedConfigurations[len(closing.AppliedConfigurations)-1]
			if nativeClaim && len(closing.ObsoleteActions) != 0 {
				t.Fatal("configuration withdrew a signed call")
			}
			for _, intent := range closing.PendingHostActions {
				if intent.Terminate != nil {
					t.Fatal("configuration requested worker termination")
				}
			}
			if nativeClaim {
				if _, exists := closing.PendingActions[action]; !exists || !slices.Contains(entry.PendingNodes, "product-review") || entry.Effect.AppliedRevision != 0 {
					t.Fatal("native-claimed review did not remain closing", entry)
				}
				if !reflect.DeepEqual(closing.Attempts[pending.Task], original.Attempts[pending.Task]) {
					t.Fatal("closing changed the original attempt")
				}
				submitVerifiedReviewResult(t, e, "closing-result", action, plannedReviewEvidence(review.ProductReview, action, "", "PASS", "complete", nil))
				after := e.LoadMustSucceed(t).State
				if after.FlowNodeEnabled("product-review") || len(after.ReviewSeries.ProductReview.RawCandidates) != 1 {
					t.Fatal("closing did not accept the normal result and then disable the node")
				}
			} else if _, exists := closing.PendingActions[action]; exists || closing.FlowNodeEnabled("product-review") || entry.Effect.AppliedRevision == 0 {
				t.Fatal("unclaimed dispatch was not retired and the node disabled", entry)
			}
			if len(closing.Decisions) != 0 {
				t.Fatal("configuration application fabricated a user decision")
			}
		})
	}
}

func TestChildConfigurationWaitsForClaimsThenRecordsMainlineBypass(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatal(err)
	}
	child := NewState(*view)
	child.RunID = "child"
	child.SliceChild = &SliceChildBinding{MasterRunID: "master", SliceID: "core"}
	initial := settings.DefaultConfiguration()
	child.AppliedConfigurations = []AppliedConfiguration{{Revision: 1, Configuration: initial, Effect: &NodeContinuationDecision{AppliedRevision: 1}}}
	parent := NewState(*view)
	parent.RunID = "master"
	parent.SliceChildren = map[string]string{"core": "child"}
	intent := HostActionIntent{ActionID: "merge-child", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: map[string]any{
		"stage": "MERGE_FINISH", "base": "prefix", "integration": map[string]any{"childRunId": "child", "incoming": "incoming"},
	}}}
	parent.PendingHostActions[intent.ActionID] = intent
	gates := []string{"security"}
	current := settings.Configuration{QAModes: []string{}, Gates: &gates}
	e := &Engine{cfg: Config{Definition: cd, Configuration: func(string) (settings.Configuration, error) { return current, nil }, RouteGates: func() ([]string, error) { return []string{"quality", "security"}, nil }, ParentSnapshot: func(id string) (Snapshot, error) {
		if id != "master" {
			t.Fatal("configuration used another parent", id)
		}
		return Snapshot{Revision: 4, State: parent}, nil
	}}}
	if changed, err := e.reconcileConfiguration(child, 2); err != nil || changed || len(child.AppliedConfigurations) != 1 {
		t.Fatal("a claimed child route was applied before integration settled", changed, err)
	}
	delete(parent.PendingHostActions, intent.ActionID)
	parent.ReconciledEffects[intent.ActionID] = ReconciledEffect{Intent: &intent}
	parent.HostActionReceipts[intent.ActionID] = HostActionReceipt{Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: map[string]any{
		"integration": map[string]any{"incoming": "incoming", "prefix": "prefix"},
	}}}
	if !parent.SliceEnteredMainline("child") {
		t.Fatal("fixture did not prove actual mainline entry")
	}
	if changed, err := e.reconcileConfiguration(child, 2); err != nil || !changed {
		t.Fatal("bypassed configuration was not recorded", changed, err)
	}
	last := child.AppliedConfigurations[len(child.AppliedConfigurations)-1]
	if !last.Bypassed || !last.Preview.EnteredMainline || last.Preview.ParentRevision != 4 || !reflect.DeepEqual(last.Configuration, current) || !reflect.DeepEqual(child.AppliedConfiguration(), initial) {
		t.Fatal("mainline bypass changed the child's applied value or lost its parent proof", last)
	}
	if changed, err := e.reconcileConfiguration(child, 3); err != nil || changed {
		t.Fatal("unchanged bypass created another application", changed, err)
	}
}

func TestExecutionNodeProjectionIncludesBlockedAndClaimedObligations(t *testing.T) {
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	nodes, err := e.executionNodeStates(e.LoadMustSucceed(t).State)
	if err != nil {
		t.Fatal(err)
	}
	if !nodes[0].Owed || !nodes[0].Editable || nodes[0].Completed || nodes[0].Running {
		t.Fatal("an unissued review obligation was marked completed", nodes[0])
	}
	action := issueReady(t, e)[0]
	e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p.ActionID != action {
			t.Fatal("claim lookup did not bind the original action")
		}
		return &SpawnReceiptPayload{ActionID: action, Provider: testProvider}, nil
	}
	library.value.FlowSelection = &FlowSelection{Disabled: []string{"product-review"}}
	reconcile(t, e)
	nodes, err = e.executionNodeStates(e.LoadMustSucceed(t).State)
	if err != nil || !nodes[0].Running || !nodes[0].Closing || nodes[0].Editable || nodes[0].Completed {
		t.Fatal("native ownership was not shown as closing", nodes, err)
	}
}

func TestConfigurationWithNoEnabledOwedNodeWaitsAndRecovers(t *testing.T) {
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	library.value.FlowSelection = &FlowSelection{Disabled: FlowNodeIDs()}
	plan, _, err := e.Plan(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Reason != decision.WaitConfigurationUnavailable {
		t.Fatal("read-only evaluation missed the unavailable node set", plan, err)
	}
	plan, _, err = e.Drive(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Reason != decision.WaitConfigurationUnavailable {
		t.Fatal("all-disabled application did not remain recoverably stopped", plan, err)
	}
	if len(e.LoadMustSucceed(t).State.PendingActions) != 0 {
		t.Fatal("all-disabled configuration issued work")
	}
	library.value.FlowSelection = nil
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if len(e.LoadMustSucceed(t).State.PendingActions) != 1 {
		t.Fatal("restoring an enabled obligation did not resume dispatch")
	}
}

func TestConfigurationApplicationRecordsPreviewAndWorkEffect(t *testing.T) {
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	library.value.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
	library.value.RoundLimits = map[string]int{"qa-whitebox": 5}
	reconcile(t, e)
	state := e.LoadMustSucceed(t).State
	entry := state.AppliedConfigurations[len(state.AppliedConfigurations)-1]
	if entry.Preview == nil || entry.Effect == nil || entry.Effect.PreviewDigest != entry.Preview.Digest || entry.Effect.AppliedRevision == 0 || !slices.Equal(entry.Changed, []string{"flowSelection", "roundLimits.qa-whitebox"}) {
		t.Fatal("application lost its preview, effect or field-level values", entry)
	}
	if len(state.Decisions) != 0 {
		t.Fatal("application was recorded as a user decision")
	}
}

func TestRuntimeRouteRequestsCannotChangeConfiguration(t *testing.T) {
	e, _ := readyReviewEngine(t)
	event := Event{ID: "runtime-configuration", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectRoute, RouteGates: []string{"quality"}, Options: routeOptions()}}
	if _, err := e.Submit(event, Observation{}); err == nil {
		t.Fatal("a runtime configuration Ask was accepted")
	}
}

func TestInitialRouteApprovalReadsCurrentLibraryWithoutChoosingASubset(t *testing.T) {
	e, _ := canonicalReviewEngine(t)
	product := issueReady(t, e)[0]
	submitVerifiedReviewResult(t, e, "library-product", product, plannedReviewEvidence(review.ProductReview, product, "", "PASS", "complete", nil))
	readiness := pendingActionID(t, e)
	submitVerifiedReviewResult(t, e, "library-readiness", readiness, plannedReviewEvidence(review.StartReadiness, readiness, "", "PASS", "complete", nil))
	askAndDecide(t, e, ControlConfirmTopology, "library-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "One cohesive development contract"}})
	catalogue := []string{"blackbox", "whitebox", "quality", "security"}
	e.cfg.RouteGates = func() ([]string, error) { return slices.Clone(catalogue), nil }
	gates := []string{"quality"}
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{"blackbox"}, Gates: &gates})
	request, err := NewRouteRequestEvent("initial-library-route", catalogue, routeOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	route := RouteDecision{Gates: catalogue, Selected: []string{"blackbox", "quality"}}
	preview, err := e.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	gates = []string{"security"}
	library.value.Gates = &gates
	library.value.QAModes = []string{"whitebox"}
	token, err := e.Freshness(RequestID(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewReviewDecideEvent("initial-library-approval", RequestID(request.ID), token, "approve", ReviewDecisionPayload{Route: &route})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(decision, Observation{}); err != nil {
		t.Fatal("approval did not recompute current library gates", err)
	}
	state := e.LoadMustSucceed(t).State
	if !slices.Equal(state.SelectedGates, []string{"whitebox", "security"}) {
		t.Fatal("initial approval retained a configuration snapshot or per-run subset")
	}
	if _, err := e.Submit(decision, Observation{}); err != nil {
		t.Fatal("recomputed approval changed original event replay", err)
	}
}
