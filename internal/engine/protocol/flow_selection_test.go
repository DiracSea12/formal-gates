package protocol

import (
	"bytes"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func TestQualityRoleOwnsOrdinaryAndMergeGateTargets(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	e := &Engine{cfg: Config{Definition: cd}}
	gateTask := pipelineTask(cd, "engine.gate.v3", "candidate:quality")
	for _, step := range cd.Steps {
		if step.Header.ID == gateTask.Step && flowStepNode(step) != "gate" {
			t.Fatal("quality role is absent from the same flow-node catalog")
		}
	}
	for _, gate := range []string{"quality", mergeGateID} {
		for _, enabled := range []bool{true, false} {
			s := &State{RunID: "run", SelectedGates: []string{gate}, Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Candidate: &coverage.ValidationCandidate{Identity: "candidate", Digest: "tree"}, Sets: map[coverage.ReviewKind]*CaseSet{}}}
			if !enabled {
				s.FlowSelection = &FlowSelection{Disabled: []string{"gate"}}
			}
			if !e.freezeValidationWave(s, coverage.ScopeFull, "", nil) {
				t.Fatal("closed gate selection blocked candidate freezing")
			}
			wave := s.currentValidationWave()
			if len(wave.Targets) != map[bool]int{true: 1, false: 0}[enabled] {
				t.Fatal("gate selection failed to project actual obligations", gate, enabled, wave.Targets)
			}
			for _, target := range wave.Targets {
				if target.Kind != "GATE" || target.Status != "PENDING" || target.ResultAction != "" {
					t.Fatal("gate selection manufactured quality evidence", target)
				}
			}
		}
	}
	for _, enabled := range []bool{true, false} {
		s := &State{RunID: "master", RouteConfirmed: true, Topology: &TopologyBinding{Decision: topologySplit, SplitCount: 2, Slices: []string{"core", "tools"}}, SliceChildren: map[string]string{"core": "core-run", "tools": "tools-run"}, SliceReceipts: map[string]SliceReceipt{"core-run": {Receipt: SliceTerminalReceipt{ChildRunID: "core-run", SelectedGates: []string{"quality"}}}, "tools-run": {Receipt: SliceTerminalReceipt{ChildRunID: "tools-run", SelectedGates: []string{"security"}}}}}
		if !enabled {
			s.FlowSelection = &FlowSelection{Disabled: []string{"gate"}}
		}
		wave := ValidationWave{ID: "merged", Targets: map[string]*ValidationTarget{}}
		s.addChildValidationTargets(&wave, cd)
		if len(wave.Targets) != map[bool]int{true: 2, false: 0}[enabled] {
			t.Fatal("gate role did not own child gate projection", enabled, wave.Targets)
		}
		for _, target := range wave.Targets {
			if target.Status != "PENDING" || target.ResultAction != "" {
				t.Fatal("omitted child review fabricated a PASS", target)
			}
		}
	}
}

func selectedReviewEngine(t *testing.T, selection *FlowSelection) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	cd := compiledCanonicalWorkflow(t)
	e, err := New(mustStore(t, dir), Config{FlowSelection: selection, Definition: cd, Registry: definition.Registry(), Capacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := view.CompleteStep("entry.registered", cd); err != nil {
		t.Fatal(err)
	}
	confirmation := reviewConfirmation()
	if err := e.InitWithMetadata(view, testProvider, Observation{}, "selected-flow", "regular", confirmation, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, Observation{}); err != nil {
		t.Fatal(err)
	}
	return e, dir
}

func TestDisabledProductReviewKeepsTechnicalReviewAndPersistentChoice(t *testing.T) {
	selection := &FlowSelection{Disabled: []string{"product-review"}}
	e, dir := selectedReviewEngine(t, selection)
	selection.Disabled[0] = "start-readiness"
	plan, _, err := e.Drive(Observation{})
	if err != nil || plan.Next.Kind != decision.KindWait {
		t.Fatalf("technical review was not issued: %+v %v", plan, err)
	}
	s := e.LoadMustSucceed(t).State
	if !reflect.DeepEqual(s.FlowSelection.Disabled, []string{"product-review"}) {
		t.Fatalf("persisted selection changed with caller memory: %+v", s.FlowSelection)
	}
	if series := s.ReviewSeries.ProductReview; series.Effective.Status == "PASS" || series.FirstPass || series.Issued != 0 || series.Completed != 0 || len(series.RawCandidates) != 0 {
		t.Fatalf("disabled product review gained evidence: %+v", series)
	}
	action := pendingActionByStep(t, e, "review.readiness")
	before := stateBytesOf(t, dir)
	reopened, err := New(mustStore(t, dir), Config{Definition: e.cfg.Definition, Registry: definition.Registry(), Capacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	restarted, _, err := reopened.Plan(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := e.Plan(Observation{})
	if err != nil || !reflect.DeepEqual(restarted, want) || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("restart changed choice, plan or state", err)
	}
	submitVerifiedReviewResult(t, reopened, "selected-technical", action, plannedReviewEvidence(review.StartReadiness, action, "", "PASS", "ready", nil))
	s = reopened.LoadMustSucceed(t).State
	if s.Phase != runtime.PhaseTopologyAndRoute || s.NextWorkflowChoice(e.cfg.Definition) != ControlConfirmTopology {
		t.Fatalf("technical review did not unlock topology: phase %s choice %s", s.Phase, s.NextWorkflowChoice(e.cfg.Definition))
	}
	if s.dualReviewPass() || !s.PreDevelopmentReviewsSatisfied() {
		t.Fatal("selected review satisfaction was confused with actual dual PASS")
	}
}

func TestDisabledTechnicalReviewWaitsForDevelopmentPreparation(t *testing.T) {
	e, dir := selectedReviewEngine(t, &FlowSelection{Disabled: []string{"start-readiness"}})
	actions := issueReady(t, e)
	if len(actions) != 1 {
		t.Fatalf("product frontier = %v", actions)
	}
	submitVerifiedReviewResult(t, e, "selected-product", actions[0], plannedReviewEvidence(review.ProductReview, actions[0], "", "PASS", "ready", nil))
	s := e.LoadMustSucceed(t).State
	if s.Phase != runtime.PhaseStartReadiness || s.NextWorkflowChoice(e.cfg.Definition) != "" || s.readinessPreparationReady() {
		t.Fatalf("topology opened before preparation: phase %s choice %s", s.Phase, s.NextWorkflowChoice(e.cfg.Definition))
	}
	if series := s.ReviewSeries.StartReadiness; series.Effective.Status == "PASS" || series.FirstPass || series.Issued != 0 || len(series.RawCandidates) != 0 {
		t.Fatalf("disabled technical review gained evidence: %+v", series)
	}
	request, err := NewRequestEvent("unprepared-topology", ControlConfirmTopology, topologyOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := e.Submit(request, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	s = e.LoadMustSucceed(t).State
	if !s.PendingAsks[string(request.ID)].Resolved || s.Topology != nil || s.Phase != runtime.PhaseStartReadiness || len(s.Decisions) != 0 {
		t.Fatal("unprepared topology request authorized work or escaped retirement")
	}
	before := stateBytesOf(t, dir)
	decision, err := NewReviewDecideEvent("authorize-unprepared-topology", RequestID(request.ID), accepted.FreshnessToken, "no-split", ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single task"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(decision, Observation{}); err == nil || rejectionCode(t, err) != CodeRequestResolved || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("retired topology request accepted a decision or changed state", err)
	}
}

func TestFlowSelectionRejectsUnknownAndRepeatedNodes(t *testing.T) {
	for _, disabled := range [][]string{{"unknown"}, {"product-review", "product-review"}, {"cleanup"}} {
		if err := ValidateFlowSelection(&FlowSelection{Disabled: disabled}); err == nil {
			t.Fatalf("invalid selection accepted: %v", disabled)
		}
	}
	if err := ValidateFlowSelection(nil); err != nil {
		t.Fatal(err)
	}
}

func TestQAGroupKeepsSelectedModesAndOwnsTheCompleteChain(t *testing.T) {
	if err := ValidateFlowSelection(&FlowSelection{Disabled: []string{"qa"}}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"qa-design", "qa-review", "qa-execution"} {
		if err := ValidateFlowSelection(&FlowSelection{Disabled: []string{phase}}); err == nil {
			t.Fatal("a QA phase became an independent main-flow choice", phase)
		}
	}
	cd := compiledCanonicalWorkflow(t)
	for _, handler := range []string{"engine.qa.design.v6", "engine.qa.review.rationale.v2", "engine.qa.execution.v4"} {
		step, ok := pipelineStep(cd, handler)
		if !ok || flowStepNode(step) != "qa" {
			t.Fatal("the original QA phase is outside its whole-group responsibility", handler)
		}
	}
	s := &State{SelectedGates: []string{"blackbox", "whitebox", "quality"}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}}}
	if !reflect.DeepEqual(s.validationKinds(), []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}) {
		t.Fatal("whole-group choice replaced the original mode owner")
	}
	selected := append([]string{}, s.SelectedGates...)
	s.FlowSelection = &FlowSelection{Disabled: []string{"qa"}}
	if len(s.validationKinds()) != 0 || !s.ValidationNotGuaranteed() || !reflect.DeepEqual(selected, s.SelectedGates) {
		t.Fatal("disabling QA changed selected modes or retained QA work")
	}
	e, _ := selectedReviewEngine(t, &FlowSelection{Disabled: []string{"qa"}})
	for _, node := range e.LoadMustSucceed(t).State.BusinessNodes(cd) {
		if (node.Kind == "qa-design" || node.Kind == "qa-review" || node.Kind == "qa-execution") && node.Unavailable == "" {
			t.Fatal("node continuation offered a disabled QA phase", node)
		}
	}
}

func TestDisabledPreDevelopmentNodesOpenTopologyWithoutReviewEvidence(t *testing.T) {
	e, _ := selectedReviewEngine(t, &FlowSelection{Disabled: []string{"product-review", "start-readiness", "development", "repair"}})
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	s := e.LoadMustSucceed(t).State
	if s.Phase != runtime.PhaseTopologyAndRoute || s.NextWorkflowChoice(e.cfg.Definition) != ControlConfirmTopology {
		t.Fatalf("disabled nodes blocked topology: %s %s", s.Phase, s.NextWorkflowChoice(e.cfg.Definition))
	}
	if len(s.Results) != 0 || len(s.PendingActions) != 0 || s.dualReviewPass() || s.ReviewSeries.ProductReview.FirstPass || s.ReviewSeries.StartReadiness.FirstPass {
		t.Fatalf("disabled nodes gained execution evidence: %+v", s)
	}
}
