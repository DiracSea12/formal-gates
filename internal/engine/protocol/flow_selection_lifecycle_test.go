package protocol

import (
	"bytes"
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
)

func TestChildStartInheritsIndependentFlowSelection(t *testing.T) {
	t.Parallel()
	confirmation := reviewConfirmation()
	child := ChildPreparation{SliceID: "core", RunID: "child", ResourceID: "workspace"}
	parent := &State{RunID: "master", Route: "regular", FlowSelection: &FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}, IntakeConfirmationReceipt: confirmation,
		NativeResources: map[string]NativeResource{"workspace": {}}, SliceChildren: map[string]string{"core": "child"},
		ChildCreation: &ChildCreation{RequirementRevision: confirmation.RequirementRevision, Intake: confirmation, Topology: TopologyBinding{Decision: topologySplit, SplitCount: 2, Slices: []string{"core", "tools"}, Parallel: "two slices"}, Children: []ChildPreparation{child}}}
	start := parent.ChildStart(child)
	want := &FlowSelection{Disabled: append([]string{}, parent.FlowSelection.Disabled...)}
	start.FlowSelection.Disabled[0] = "repair"
	if !reflect.DeepEqual(parent.FlowSelection, want) {
		t.Fatal("child start selection shared parent memory")
	}
	start = parent.ChildStart(child)
	cd := compiledCanonicalWorkflow(t)
	engine, err := New(mustStore(t, t.TempDir()), Config{FlowSelection: &FlowSelection{Disabled: []string{"repair"}}, Definition: cd, Registry: definition.Registry()})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhasePreparing)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.InitWithMetadata(view, testProvider, Observation{}, child.RunID, "regular", confirmation, "", "", start); err != nil {
		t.Fatal(err)
	}
	start.FlowSelection.Disabled[0] = "repair"
	if err := engine.SyncParentRequirement(parent, Observation{}); err != nil {
		t.Fatal(err)
	}
	s := engine.LoadMustSucceed(t).State
	if !reflect.DeepEqual(s.FlowSelection, want) || s.SliceChild.ParentReviewPending || s.dualReviewPass() || !s.PreDevelopmentReviewsSatisfied() {
		t.Fatal("child lost selection or manufactured inherited PASS", s.FlowSelection, s.SliceChild)
	}
	if s.ReviewSeries.ProductReview.Completed != 0 || s.ReviewSeries.StartReadiness.Completed != 0 || len(s.ReviewSeries.ProductReview.RawCandidates) != 0 || len(s.ReviewSeries.StartReadiness.RawCandidates) != 0 {
		t.Fatal("child created its own review evidence")
	}
}

func TestNodeContinuationCannotEnableDisabledResponsibility(t *testing.T) {
	t.Parallel()
	e, dir := selectedReviewEngine(t, &FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}})
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	p, err := e.NodeContinuationPreview("product-review")
	if err != nil || p.To.Unavailable == "" {
		t.Fatal("disabled responsibility was selectable", err, p.To)
	}
	event := Event{ID: "continue-disabled", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlContinueNode, Options: []AskOption{{ID: "approve", Label: "Continue"}, {ID: "decline", Label: "Keep the flow"}}, Continuation: &NodeContinuationRequest{Target: p.To.ID, StateRevision: p.StateRevision, PreviewDigest: p.Digest}}}
	if _, err := e.Submit(event, Observation{}); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if options := state.PendingAsks[string(event.ID)].Options; len(options) != 1 || options[0].ID != "decline" {
		t.Fatal("disabled node Ask offered authorization to reopen it", options)
	}
	before := stateBytesOf(t, dir)
	token, err := e.Freshness(RequestID(event.ID))
	if err != nil {
		t.Fatal(err)
	}
	approve, err := NewDecideEvent("approve-disabled", RequestID(event.ID), token, "approve")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(approve, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("disabled continuation changed authority", err)
	}
}

func TestRequirementRedoCannotEnableDisabledReview(t *testing.T) {
	t.Parallel()
	e, dir := selectedReviewEngine(t, &FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}})
	for _, product := range []bool{true, false} {
		pkg := requirementPackageOf(nil)
		pkg.RedoProductReview, pkg.RedoStartReadiness = product, !product
		request, err := NewRequirementRequestEvent("disabled-redo", pkg, requirementOptions()...)
		if err != nil {
			t.Fatal(err)
		}
		before := stateBytesOf(t, dir)
		if _, err := e.Submit(request, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
			t.Fatal("disabled review redo was recorded", err)
		}
	}
}
