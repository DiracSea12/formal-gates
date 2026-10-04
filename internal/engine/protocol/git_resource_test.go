package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

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
	if err := validateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	s := &State{
		RunID: "delivery", NativeRoot: t.TempDir(), BaseSnapshot: "base-identity",
		Validation:   &Validation{RequirementRevision: "rev-1", Generation: 1},
		GitResources: map[string]GitResource{}, DispatchBindings: map[string]WorkBinding{},
		Decisions: map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Revision: 5,
			Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness", Digest: planContractDigest(plan)}}}}},
		Results: map[string]WorkerResult{"readiness": {ActionID: "readiness", Outcome: OutcomePass,
			Review: &ReviewResultEvidence{Action: review.StartReadiness, RequirementRevision: "rev-1", GranularityReview: plan}}},
	}
	spikeTask, commandTask, extensionTask := planTask(cd, plan, batch.ID), planTask(cd, plan, command.ID), planTask(cd, plan, extension.ID)
	spike, delivery := s.taskResource(spikeTask, cd), s.taskResource(commandTask, cd)
	if spike.ID == delivery.ID || spike.Path == delivery.Path || spike.Ref == delivery.Ref || spike.Source != s.BaseSnapshot || spike.Owner != s.RunID || spike.Role != "development" {
		t.Fatalf("spike lacks its own admitted development resource: %+v %+v", spike, delivery)
	}
	if got := s.taskResource(extensionTask, cd); got != delivery {
		t.Fatalf("ordinary members lost their shared delivery resource: %+v %+v", got, delivery)
	}
	spike.Status, spike.Identity = "READY", "observed-spike-identity"
	s.GitResources[spike.ID], s.GitResources[delivery.ID] = spike, delivery
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
	if s.GitResources[spike.ID] != spike || s.GitResources[delivery.ID] != delivery {
		t.Fatal("requirement decisions discarded outstanding native resources")
	}
}
