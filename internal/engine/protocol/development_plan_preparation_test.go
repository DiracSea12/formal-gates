package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirement"
)

func developmentPreparationEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	cd := compiledCanonicalWorkflow(t)
	e, err := New(mustStore(t, dir), Config{Definition: cd, Registry: definition.Registry(), Capacity: 4, FlowSelection: &FlowSelection{Disabled: []string{"product-review", "start-readiness"}}})
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
	document := "# Requirement\n\n## 需求点\n\n### REQ-001：运行\n\n#### 要求\n实现公开运行入口。\n\n#### 验收条件\n- AC-001：公开运行入口返回结果。\n\n#### 来源\n用户要求。\n"
	projection, err := requirement.ParseRequirementProjection("requirement.md", []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	confirmation := reviewConfirmation()
	confirmation.RequirementRevision = "sha256:" + projection.ContentDigest
	confirmation.Artifacts = []IntakeArtifact{{Path: "requirement.md", Revision: confirmation.RequirementRevision}}
	if err := e.InitWithMetadata(view, testProvider, Observation{}, "selected-development", "regular", confirmation, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: digestOfCanonical(confirmation)}, Observation{}); err != nil {
		t.Fatal(err)
	}
	observation := Event{ID: "confirmed-document", Kind: KindOperatorObservation, Observation: &OperatorObservationPayload{Subject: confirmation.RequirementRevision, Validation: &ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: confirmation.RequirementRevision, Document: document}}}
	if _, err := e.Submit(observation, Observation{}); err != nil {
		t.Fatal(err)
	}
	return e, dir
}

func TestDevelopmentPreparationBindsOriginalResultAndConfirmationAfterReload(t *testing.T) {
	t.Parallel()
	e, dir := developmentPreparationEngine(t)
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	s := e.LoadMustSucceed(t).State
	if len(s.PendingActions) != 1 {
		plan, _, err := e.Plan(Observation{})
		t.Fatalf("development preparation frontier = %+v, phase=%s, validation=%+v, required=%+v, plan=%+v, error=%v", s.PendingActions, s.Phase, s.Validation, s.decisionView(e.cfg.Definition).RequiredTasks, plan, err)
	}
	action := pendingActionByStep(t, e, "development.plan")
	input, current := s.WorkerInput(action, e.cfg.Definition)
	if !current || input.Handler != "engine.development.plan.v1" || input.Review != nil || input.Validation == nil || input.Validation.Binding != s.DispatchBindings[action] || len(input.RequirementPoints) == 0 {
		t.Fatalf("preparation lost its original selected duty: %+v", input)
	}
	if s.NextWorkflowChoice(e.cfg.Definition) != "" || s.readinessPreparationReady() {
		t.Fatal("preparation bypassed user plan confirmation prerequisites")
	}
	before := stateBytesOf(t, dir)
	reopened, err := New(mustStore(t, dir), Config{Definition: e.cfg.Definition, Registry: definition.Registry(), Capacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	reloaded := reopened.LoadMustSucceed(t).State
	replayed, current := reloaded.WorkerInput(action, e.cfg.Definition)
	if !current || !reflect.DeepEqual(input, replayed) || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("reload changed original preparation identity or input")
	}
	proposal := plannedReviewEvidence(review.StartReadiness, action, "", "PASS", "prepare", nil).GranularityReview
	proposal.Batches[0].RequirementPoints = requirement.PointIDs(input.RequirementPoints)
	for i := range proposal.Batches[0].Checks {
		check := &proposal.Batches[0].Checks[i]
		check.Command, check.Goal = "", "Prepare "+check.Purpose+" invocation"
	}
	if _, err := reopened.Submit(spawnReceipt(t, "prepare-spawn", action, "original-worker"), Observation{}); err != nil {
		t.Fatal(err)
	}
	result := Event{ID: "prepare-result", Kind: KindWorkerResult, Result: &WorkerResultPayload{ActionID: action, Provider: testProvider, Outcome: OutcomePass, PayloadDigest: "sha256:original-proposal", Validation: &ValidationResult{Binding: input.Validation.Binding, Status: OutcomePass, GranularityReview: proposal}}}
	if acceptance, err := reopened.Submit(result, Observation{}); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatal("original preparation was not accepted", acceptance, err)
	}
	s = reopened.LoadMustSucceed(t).State
	if original := s.Results[action]; original.Review != nil || !reflect.DeepEqual(original.Validation.GranularityReview, proposal) || s.ReviewSeries.StartReadiness.Completed != 0 || s.ReviewSeries.StartReadiness.Effective.Status == "PASS" {
		t.Fatal("preparation replaced the proposal or fabricated technical review")
	}
	if !s.readinessPreparationReady() || s.NextWorkflowChoice(e.cfg.Definition) != ControlConfirmTopology || len(s.PendingActions) != 0 {
		t.Fatal("preparation bypassed confirmation or did not unlock its original confirmation owner")
	}
	askAndDecide(t, reopened, ControlConfirmTopology, "prepared-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "one cohesive responsibility"}})
	route := RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality"}, Gates: []string{"blackbox", "whitebox", "gate.quality"}}
	preview, err := reopened.DevelopmentPlan(route)
	if err != nil || preview.Confirmation.ReviewActionID != action || !reflect.DeepEqual(preview.Proposal, *proposal) {
		t.Fatal("confirmation did not bind the original prepared proposal", preview, err)
	}
	confirmPlannedRoute(t, reopened, "prepared-route", route)
	s = reopened.LoadMustSucceed(t).State
	request, confirmed, revision := s.confirmedDevelopmentPlan()
	if request != "prepared-route" || !reflect.DeepEqual(confirmed, proposal) || revision != input.Validation.Binding.RequirementRevision {
		t.Fatal("confirmed execution contract changed its original request/revision", request, revision)
	}
	copy := *s
	copy.Decisions = make(map[string]RecordedDecision, len(s.Decisions)+1)
	for id, value := range s.Decisions {
		copy.Decisions[id] = value
	}
	duplicate := copy.Decisions[request]
	duplicate.Revision++
	copy.Decisions["same-contract-route"] = duplicate
	if original, _, originalRevision := copy.confirmedDevelopmentPlan(); original != request || originalRevision != revision {
		t.Fatal("reconfirming the same contract replaced original authorization")
	}
	stored, err := json.Marshal(&copy)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	if original, plan, originalRevision := recovered.confirmedDevelopmentPlan(); original != request || originalRevision != revision || !reflect.DeepEqual(plan, proposal) {
		t.Fatal("recovery changed confirmed proposal or original authorization")
	}
}

func TestDisabledDevelopmentUsesCurrentNativeInputAndExistingGuard(t *testing.T) {
	t.Parallel()
	e, s, _ := guardPipelineFixture(t)
	s.FlowSelection = &FlowSelection{Disabled: []string{"development"}}
	s.InitialSnapshot = "verified-initial-native-head"
	sources, paths, base, complete := s.developmentSources(e.cfg.Definition)
	if !complete || len(sources) != 0 || len(paths) != 0 || base != s.InitialSnapshot || len(s.planTasks(e.cfg.Definition)) != 0 || len(s.fullProjectChecks()) != 0 {
		t.Fatal("disabled development projected batches or lost the verified native input")
	}
	delete(s.Results, "act:readiness-plan")
	s.Decisions["route-1"] = RecordedDecision{Control: ControlSelectRoute, Review: &ReviewDecisionPayload{Route: &RouteDecision{}}}
	budget, err := e.deriveGuardBudget(s)
	if err != nil || budget == nil || budget.RequestAllowance != 2 {
		t.Fatal("selected QA and gate lost their existing dispatch guard", budget, err)
	}
	params, ready, err := s.currentCandidateParameters(e.cfg.Definition)
	if err != nil || !ready || params.Base != s.InitialSnapshot || params.WorkspaceID == "" || len(params.SourceTasks) != 0 {
		t.Fatal("candidate preparation invented development or lost the native base", params, ready, err)
	}
	resource := s.candidateResource(params, e.cfg.Definition)
	if resource.Role != "candidate" || resource.Source != s.InitialSnapshot {
		t.Fatal("current project validation selected a development workspace", resource)
	}
	s.InitialSnapshot = ""
	if _, _, _, complete := s.developmentSources(e.cfg.Definition); complete {
		t.Fatal("missing verified native input was treated as a complete source")
	}
}
