package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// An unresolved requirement Ask freezes new writes while the original cleanup
// intent and its exact resource receipt retain the native responsibility.
func TestCleanupAuthoritySurvivesRequirementBarrier(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	resource := NativeResource{ID: "work", Owner: "delivery", Role: "development", Path: "/work/delivery", Ref: "refs/delivery", Source: "base", Identity: "candidate", Status: "READY"}
	values, err := normalizeHostActionParams(CleanupParameters{Resources: []NativeResource{resource}})
	if err != nil {
		t.Fatal(err)
	}
	intent := HostActionIntent{ActionID: "cleanup", Adapter: &AdapterHostAction{Operation: "op.cleanup", Params: values}, PayloadDigest: "parameters"}
	s := &State{State: decision.State{Phase: runtime.PhaseFinalizingCleanup}, RunID: resource.Owner,
		NativeResources: map[string]NativeResource{resource.ID: resource}, PendingHostActions: map[string]HostActionIntent{intent.ActionID: intent},
		PendingAsks: map[string]PendingAsk{"change": {Control: ControlRequestRequirementChange}},
	}
	if s.CleanupEligible(cd) || s.NativeActionCurrent(intent, cd) {
		t.Fatal("unresolved requirement Ask authorized a new cleanup write")
	}
	if !s.cleanupStarted(false) || !s.nativeEffectApplicable(intent, cd) || s.verifyPhaseAgainstLedgers(cd) != nil {
		t.Fatal("requirement Ask discarded original cleanup authority")
	}
	resource.Status = "CLEANED"
	proof, err := normalizeHostActionParams(WorkspaceCleanupReceipt{ParametersDigest: intent.PayloadDigest, Resources: []NativeResource{resource}})
	if err != nil {
		t.Fatal(err)
	}
	receipt := HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.cleanup", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: proof}}
	engine := &Engine{cfg: Config{Definition: cd}}
	if err := engine.applyResourceReceipt(s, intent, receipt); err != nil {
		t.Fatal(err)
	}
	delete(s.PendingHostActions, intent.ActionID)
	s.HostActionReceipts = map[string]HostActionReceipt{intent.ActionID: receipt}
	s.ReconciledEffects = map[string]ReconciledEffect{intent.ActionID: {Intent: &intent, Status: "FULFILLED"}}
	if !s.cleanupStarted(false) || !s.ResourcesCleaned() || s.verifyPhaseAgainstLedgers(cd) != nil {
		t.Fatal("accepted receipt discarded original cleanup authority")
	}
	if err := engine.applyResourceReceipt(s, intent, receipt); err != nil || s.NativeResources[resource.ID] != resource {
		t.Fatal("replayed cleanup receipt changed the registered resource")
	}
	delete(s.ReconciledEffects, intent.ActionID)
	if s.cleanupStarted(false) || s.verifyPhaseAgainstLedgers(cd) == nil {
		t.Fatal("cleanup phase passed without its original intent")
	}
}

// Resource selection follows confirmed business work, including explicit carry
// and redo decisions. Native materialization is covered by installed tests.
func TestDevelopmentResourcesFollowConfirmedWork(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	batch := DevelopmentBatch{
		ID: "investigate", Spike: true,
		Subtasks:    []DevelopmentSubtask{{ID: "input", Description: "Check the input boundary"}},
		Deliverable: "Input boundary conclusion", PublicEntry: "app", Artifacts: []string{"experiment.go"},
		Interfaces: "Command arguments", StateBoundary: "No persistent state", Risk: "Invalid input",
		DoD:      []DevelopmentCriterion{{ID: "behavior", Description: "Input contract holds"}},
		Checks:   []DevelopmentCheck{{ID: "build", Command: "go build ./..."}},
		Rollback: "Discard experiment", Recovery: "Run the check again", Handoff: "Input conclusion", ContextCost: "One package",
	}
	command := batch
	command.ID, command.Spike, command.DependsOn, command.Artifacts = "command", false, []string{batch.ID}, []string{"app.go"}
	command.Subtasks = []DevelopmentSubtask{{ID: "validate", Description: "Implement the confirmed input boundary"}}
	extension := command
	extension.ID, extension.DependsOn, extension.Artifacts = "extension", []string{command.ID}, []string{"extension.go"}
	extension.Subtasks = []DevelopmentSubtask{{ID: "extend", Description: "Consume the implemented input boundary"}}
	plan := &GranularityReview{Reason: "Resolve uncertainty before implementation", VerificationTier: "FINAL", Batches: []DevelopmentBatch{batch, command, extension}}
	if err := ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	s := &State{
		RunID: "delivery", NativeRoot: t.TempDir(), BaseSnapshot: "base-identity", InitialSnapshot: "initial-identity",
		Validation:      &Validation{RequirementRevision: "rev-1", Generation: 1},
		NativeResources: map[string]NativeResource{}, DispatchBindings: map[string]WorkBinding{},
		Decisions: map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Revision: 5,
			Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness", Digest: planContractDigest(plan)}}}}},
		Results: map[string]WorkerResult{"readiness": {ActionID: "readiness", Outcome: OutcomePass,
			Review: &ReviewResultEvidence{Action: review.StartReadiness, RequirementRevision: "rev-1", GranularityReview: plan}}},
	}
	spikeTask, commandTask, extensionTask := planTask(cd, plan, batch.ID), planTask(cd, plan, command.ID), planTask(cd, plan, extension.ID)
	spike, delivery := s.taskResource(spikeTask, cd), s.taskResource(commandTask, cd)
	if spike.ID == delivery.ID || spike.Path == delivery.Path || spike.Ref == delivery.Ref || spike.Source != s.InitialSnapshot || spike.Owner != s.RunID || spike.Role != "development" {
		t.Fatalf("spike lacks its own admitted development resource: %+v %+v", spike, delivery)
	}
	if got := s.taskResource(extensionTask, cd); got != delivery {
		t.Fatalf("ordinary members lost their shared delivery resource: %+v %+v", got, delivery)
	}
	spike.Status, spike.Identity = "READY", "observed-spike-identity"
	s.NativeResources[spike.ID], s.NativeResources[delivery.ID] = spike, delivery
	if got := s.taskResource(spikeTask, cd); got != spike {
		t.Fatal("repeat selection replaced the registered native fact")
	}
	binding := WorkBinding{Task: spikeTask, WorkspaceID: spike.ID, RequirementRevision: "rev-1", Generation: 1}
	s.DispatchBindings["investigator"] = binding
	s.Validation.Candidate = &coverage.ValidationCandidate{Identity: "candidate-identity", Digest: "candidate-tree"}
	if got := s.taskResource(spikeTask, cd); got != spike {
		t.Fatal("an issued binding changed resource after a candidate became available")
	}
	s.Results["investigator"] = WorkerResult{ActionID: "investigator", Outcome: OutcomePass,
		Validation: &ValidationResult{Binding: binding, Status: "PASS", Development: &DevelopmentEvidence{
			Paths: []string{"experiment.go"}, Summary: "Input uncertainty resolved", Build: []string{"build passed"}, Tests: []string{"input checked"},
			Completion: &DevelopmentCompletion{DoD: map[string]string{"behavior": "Input confirmed"}, Checks: map[string]string{"build": "Build passed"}, Handoff: "Use the input conclusion"},
		}}}
	carry := ConfirmedRevision{Revision: "rev-2", StateRevision: 9}
	for _, task := range []runtime.TaskKey{spikeTask, commandTask, extensionTask} {
		carry.WorkDecisions = append(carry.WorkDecisions, RequirementWorkDecision{Work: RequirementWork{Task: task, RequirementRevision: "rev-1"}, Disposition: RequirementWorkCarry})
	}
	s.RequirementRevisions = append(s.RequirementRevisions, carry)
	s.Validation.RequirementRevision, s.Validation.Generation = carry.Revision, 2
	if s.currentPlanTask(cd, plan, batch.ID) != spikeTask || s.DispatchBindings["investigator"] != binding || !reflect.DeepEqual(s.developmentInput(commandTask, cd).Dependencies[batch.ID], s.Results["investigator"]) {
		t.Fatal("carry changed the original execution identity, workspace binding or conclusion")
	}
	redo := ConfirmedRevision{Revision: "rev-3", StateRevision: 12, WorkDecisions: []RequirementWorkDecision{
		{Work: RequirementWork{Task: spikeTask, RequirementRevision: "rev-1"}, Disposition: RequirementWorkRedo},
		{Work: RequirementWork{Task: commandTask, RequirementRevision: "rev-1"}, Disposition: RequirementWorkCarry},
		{Work: RequirementWork{Task: extensionTask, RequirementRevision: "rev-1"}, Disposition: RequirementWorkCarry},
	}}
	s.RequirementRevisions = append(s.RequirementRevisions, redo)
	s.Validation.RequirementRevision, s.Validation.Generation = redo.Revision, 3
	newSpikeTask := s.currentPlanTask(cd, plan, batch.ID)
	newSpike, newDelivery := s.taskResource(newSpikeTask, cd), s.taskResource(commandTask, cd)
	if newSpikeTask == spikeTask || newSpike.ID == spike.ID || newSpike.ID == newDelivery.ID || newSpike.Source != s.Validation.Candidate.Identity || s.taskResource(extensionTask, cd) != newDelivery {
		t.Fatal("redo lost the spike business identity or shared delivery boundary")
	}
	if s.NativeResources[spike.ID] != spike || s.NativeResources[delivery.ID] != delivery {
		t.Fatal("requirement decisions discarded outstanding native resources")
	}
}

// Topology replacement retires only the previous membership's removed targets.
// Worker and native bindings keep the particular resources they still use live.
func TestTopologyCleanupFollowsRetiredResources(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	retired := NativeResource{ID: "retired", Owner: "master", Role: "child-target", Path: "/targets/retired", Ref: "refs/retired", Source: "source", Identity: "source", Status: "READY"}
	carried := retired
	carried.ID, carried.Path, carried.Ref = "carried", "/targets/carried", "refs/carried"
	current := retired
	current.ID, current.Path, current.Ref, current.Status = "current", "/targets/current", "refs/current", "PLANNED"
	design := retired
	design.ID, design.Role, design.Path, design.Ref = "design", "merge-design", "/work/design", "refs/design"
	s := &State{RunID: "master", SliceChildren: map[string]string{"core": "core-run"},
		ChildCreation:   &ChildCreation{Previous: &ChildCreation{Children: []ChildPreparation{{RunID: "retired-run", ResourceID: retired.ID}, {RunID: "core-run", ResourceID: carried.ID}}}, Children: []ChildPreparation{{RunID: "core-run", ResourceID: carried.ID, Carried: true}, {RunID: "current-run", ResourceID: current.ID}}},
		NativeResources: map[string]NativeResource{retired.ID: retired, carried.ID: carried, current.ID: current, design.ID: design},
		PendingActions:  map[string]PendingAction{"designer": {ActionID: "designer"}}, DispatchBindings: map[string]WorkBinding{"designer": {WorkspaceID: design.ID}},
		EndedAttempts: map[runtime.TaskKey]Attempt{}, PendingHostActions: map[string]HostActionIntent{}}
	p := CleanupParameters{Resources: []NativeResource{retired}}
	if !s.cleanupAuthorized(p, cd) || !s.cleanupParametersCurrent(p) || !reflect.DeepEqual(s.replacedTargetResources(), p.Resources) {
		t.Fatal("carried work blocked the exact retired-target debt")
	}
	for _, r := range []NativeResource{carried, current, design} {
		if s.cleanupParametersCurrent(CleanupParameters{Resources: []NativeResource{retired, r}}) {
			t.Fatal("cleanup admitted a current resource", r.ID)
		}
	}
	s.DispatchBindings["designer"] = WorkBinding{WorkspaceID: retired.ID}
	if s.cleanupAuthorized(p, cd) {
		t.Fatal("cleanup admitted a target with an active worker")
	}
	delete(s.PendingActions, "designer")
	key := runtime.TaskKey{Step: "develop"}
	s.EndedAttempts[key] = Attempt{AwaitingResult: true, Work: &WorkBinding{WorkspaceID: retired.ID}}
	if s.cleanupAuthorized(p, cd) {
		t.Fatal("cleanup admitted a target awaiting its worker result")
	}
	delete(s.EndedAttempts, key)
	for _, input := range []struct {
		operation string
		params    any
	}{
		{"op.workspace", WorkspaceParameters{Resource: retired}},
		{"op.candidate", CandidateParameters{WorkspaceID: retired.ID}},
		{"op.candidate", CandidateParameters{Promotion: &PromotionParameters{SourceWorkspace: design.ID, TargetRoot: retired.Path}}},
	} {
		values, err := normalizeHostActionParams(input.params)
		if err != nil {
			t.Fatal(err)
		}
		s.PendingHostActions["writer"] = HostActionIntent{Adapter: &AdapterHostAction{Operation: authoring.OperationID(input.operation), Params: values}}
		if s.cleanupAuthorized(p, cd) {
			t.Fatal("cleanup admitted a target with an unresolved native writer", input.operation)
		}
	}
	delete(s.PendingHostActions, "writer")
	values, err := normalizeHostActionParams(p)
	if err != nil {
		t.Fatal(err)
	}
	intent := HostActionIntent{ActionID: "cleanup", Adapter: &AdapterHostAction{Operation: "op.cleanup", Params: values}}
	s.PendingHostActions[intent.ActionID] = intent
	if !s.cleanupParametersCurrent(p) || !s.NativeActionCurrent(intent, cd) || !s.nativeEffectApplicable(intent, cd) {
		t.Fatal("issued cleanup lost its shared execution and observation authority")
	}
	s.NativeResources[retired.ID] = NativeResource{ID: retired.ID, Owner: retired.Owner, Role: retired.Role, Path: retired.Path, Ref: retired.Ref, Source: retired.Source, Identity: retired.Identity, Status: "CLEANED"}
	delete(s.PendingHostActions, intent.ActionID)
	if len(s.replacedTargetResources()) != 0 || s.cleanupParametersCurrent(p) || s.NativeResources[carried.ID] != carried || s.NativeResources[current.ID] != current || s.NativeResources[design.ID] != design {
		t.Fatal("cleanup replay renewed debt or changed current resources")
	}
}
