package protocol

import (
	"crypto/sha256"
	"fmt"
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
	t.Parallel()
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
	if !s.cleanupStarted(false) || !s.nativeEffectApplicable(intent, cd) || s.verifyPhaseAgainstLedgers() != nil {
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
	if !s.cleanupStarted(false) || !s.ResourcesCleaned() || s.verifyPhaseAgainstLedgers() != nil {
		t.Fatal("accepted receipt discarded original cleanup authority")
	}
	if err := engine.applyResourceReceipt(s, intent, receipt); err != nil || s.NativeResources[resource.ID] != resource {
		t.Fatal("replayed cleanup receipt changed the registered resource")
	}
	delete(s.ReconciledEffects, intent.ActionID)
	if s.cleanupStarted(false) || s.verifyPhaseAgainstLedgers() == nil {
		t.Fatal("cleanup phase passed without its original intent")
	}
}

// Resource selection follows confirmed business work, including explicit carry
// and redo decisions. Native materialization is covered by installed tests.
func TestDevelopmentResourcesFollowConfirmedWork(t *testing.T) {
	t.Parallel()
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
		Checks:   []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Command: "go build ./..."}, {ID: "tests", Purpose: CheckFullTests, Command: "go test ./..."}},
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
	binding := WorkBinding{Task: spikeTask, WorkspaceID: spike.ID, RequirementRevision: "rev-1", Generation: 1, PlanRequest: "route", BatchID: batch.ID}
	s.DispatchBindings["investigator"] = binding
	s.Validation.Candidate = &coverage.ValidationCandidate{Identity: "candidate-identity", Digest: "candidate-tree"}
	if got := s.taskResource(spikeTask, cd); got != spike {
		t.Fatal("an issued binding changed resource after a candidate became available")
	}
	s.Results["investigator"] = WorkerResult{ActionID: "investigator", Outcome: OutcomePass,
		Validation: &ValidationResult{Binding: binding, Status: "PASS", Development: &DevelopmentEvidence{
			Paths: []string{"experiment.go"}, Summary: "Input uncertainty resolved",
			Completion: &DevelopmentCompletion{DoD: map[string]string{"behavior": "Input confirmed"}, Handoff: "Use the input conclusion"},
		}}}
	recordDevelopmentChecksFixture(t, s, "investigator", batch)

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
	t.Parallel()
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

// Queued workspace replacement uses the confirmed task and original resource
// inputs even when no worker dispatch has supplied a workspace binding.
func TestQueuedWorkspaceReplacementRequiresExactAuthorization(t *testing.T) {
	t.Parallel()
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
		Checks:   []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Command: "go build ./..."}, {ID: "tests", Purpose: CheckFullTests, Command: "go test ./..."}},
		Rollback: "Discard experiment", Recovery: "Run the check again", Handoff: "Input conclusion", ContextCost: "One package",
	}
	plan := &GranularityReview{Reason: "Resolve uncertainty before implementation", VerificationTier: "FINAL", Batches: []DevelopmentBatch{batch}}
	if err := ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	s := &State{
		RunID: "delivery", NativeRoot: t.TempDir(), BaseSnapshot: "base-identity", InitialSnapshot: "initial-identity",
		Validation:      &Validation{RequirementRevision: "rev-1", Generation: 1},
		NativeResources: map[string]NativeResource{},
		Decisions: map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Revision: 5,
			Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness", Digest: planContractDigest(plan)}}}}},
		Results: map[string]WorkerResult{"readiness": {ActionID: "readiness", Outcome: OutcomePass,
			Review: &ReviewResultEvidence{Action: review.StartReadiness, RequirementRevision: "rev-1", GranularityReview: plan}}},
	}
	spikeTask := planTask(cd, plan, batch.ID)
	spike := s.taskResource(spikeTask, cd)
	s.NativeResources[spike.ID] = spike
	s.Validation.Candidate = &coverage.ValidationCandidate{Identity: "candidate-identity", Digest: "candidate-tree"}
	s.RequirementRevisions = []ConfirmedRevision{{Revision: "rev-2", StateRevision: 12, WorkDecisions: []RequirementWorkDecision{{
		Work: RequirementWork{Task: spikeTask, RequirementRevision: "rev-1"}, Disposition: RequirementWorkRedo,
	}}}}
	s.Validation.RequirementRevision, s.Validation.Generation = "rev-2", 2
	newSpikeTask := s.currentPlanTask(cd, plan, batch.ID)
	newSpike := s.taskResource(newSpikeTask, cd)

	intentFor := func(id string, revision uint64, task runtime.TaskKey, resource NativeResource) HostActionIntent {
		values, err := normalizeHostActionParams(WorkspaceParameters{Task: &task, Resource: resource})
		if err != nil {
			t.Fatal(err)
		}
		return HostActionIntent{ActionID: id, Revision: revision, Correlation: resource.ID, PayloadDigest: digestOfCanonical(values), Adapter: &AdapterHostAction{Operation: "op.workspace", Params: values}}
	}
	original := intentFor("original", 8, spikeTask, spike)
	replacement := intentFor("replacement", 13, newSpikeTask, newSpike)
	if !s.nativeReplacementAuthorized(original, replacement, cd) {
		t.Fatal("confirmed queued REDO cannot settle the original responsibility")
	}
	otherTask := newSpikeTask
	otherTask.Scope += ":other"
	if s.nativeReplacementAuthorized(original, intentFor("other", 13, otherTask, newSpike), cd) {
		t.Fatal("the same resource authorized a different task")
	}
	changedSource := newSpike
	changedSource.Source = "different-source"
	if s.nativeReplacementAuthorized(original, intentFor("different-source", 13, newSpikeTask, changedSource), cd) {
		t.Fatal("replacement accepted a different source")
	}
	if s.nativeReplacementAuthorized(original, intentFor("earlier", 8, newSpikeTask, newSpike), cd) {
		t.Fatal("replacement ignored original revision chronology")
	}
	s.RequirementRevisions = s.RequirementRevisions[:len(s.RequirementRevisions)-1]
	if s.nativeReplacementAuthorized(original, replacement, cd) {
		t.Fatal("replacement was authorized without REDO")
	}
}

func recordDevelopmentChecksFixture(t *testing.T, s *State, action string, batch DevelopmentBatch) {
	t.Helper()
	raw := s.Results[action]
	binding := raw.Validation.Binding
	sourceParams := CandidateParameters{Stage: "CHECK_SOURCE", SourceTasks: []runtime.TaskKey{binding.Task}, WorkspaceID: binding.WorkspaceID, Paths: raw.Validation.Development.Paths, RequirementRevision: binding.RequirementRevision, Generation: binding.Generation}
	params, err := normalizeHostActionParams(sourceParams)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := action + ":source"
	intent := HostActionIntent{ActionID: sourceID, PayloadDigest: digestOfCanonical(params), Adapter: &AdapterHostAction{Operation: "op.candidate", Params: params}}
	proof := CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: binding.WorkspaceID, Identity: "verified-source:" + action, Digest: "verified-tree:" + action, Paths: raw.Validation.Development.Paths}
	values, err := normalizeHostActionParams(proof)
	if err != nil {
		t.Fatal(err)
	}
	if s.PendingHostActions == nil {
		s.PendingHostActions = map[string]HostActionIntent{}
	}
	if s.HostActionReceipts == nil {
		s.HostActionReceipts = map[string]HostActionReceipt{}
	}
	s.PendingHostActions[sourceID] = intent
	s.HostActionReceipts[sourceID] = HostActionReceipt{ActionID: sourceID, AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
	if s.NativeResources == nil {
		s.NativeResources = map[string]NativeResource{}
	}
	resource := s.checkResource(action, proof)
	resource.Status, resource.Identity = "READY", proof.Identity
	s.NativeResources[resource.ID] = resource
	for _, check := range batch.Checks {
		p := CheckParameters{SourceActionID: action, Binding: binding, Check: check, SourceIdentity: proof.Identity, SourceDigest: proof.Digest, WorkspaceID: resource.ID}
		params, err := normalizeHostActionParams(p)
		if err != nil {
			t.Fatal(err)
		}
		id := action + ":check:" + check.ID
		call := HostActionIntent{ActionID: id, PayloadDigest: digestOfCanonical(params), Adapter: &AdapterHostAction{Operation: "op.check", Params: params}}
		evidence := CheckEvidence{ParametersDigest: call.PayloadDigest, SourceActionID: action, SourceIdentity: proof.Identity, SourceDigest: proof.Digest, WorkspaceID: resource.ID, RequestDigest: digestOfCanonical(p), Directory: resource.Path, Stdout: CheckStreamProof{SHA256: fmt.Sprintf("%x", sha256.Sum256(nil))}, Stderr: CheckStreamProof{SHA256: fmt.Sprintf("%x", sha256.Sum256(nil))}}
		values, err := normalizeHostActionParams(evidence)
		if err != nil {
			t.Fatal(err)
		}
		s.PendingHostActions[id] = call
		s.HostActionReceipts[id] = HostActionReceipt{ActionID: id, AdapterOperation: "op.check", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
	}
}

func TestWhiteboxAuthoringReusesOnlyCurrentDevelopmentSource(t *testing.T) {
	t.Parallel()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	original := pipelineTask(cd, "engine.qa.design.v6", "3:WHITEBOX:1:")
	revised := pipelineTask(cd, "engine.qa.design.v6", "3:WHITEBOX:2:clarify")
	resource := NativeResource{ID: "authoring", Owner: "run", Role: "whitebox-authoring", Path: "/run/authoring", Source: "development", Identity: "test-candidate", Status: "READY"}
	s := &State{RunID: "run", NativeRoot: "/run", SelectedGates: []string{"whitebox"}, Validation: &Validation{DevelopmentSnapshot: "development", RequirementRevision: "confirmed", Generation: 3, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewWhitebox: {DesignTask: original, Status: "DRAFT"}}}, NativeResources: map[string]NativeResource{resource.ID: resource}, DispatchBindings: map[string]WorkBinding{"original": {Task: original, WorkspaceID: resource.ID}}}
	if actual := s.taskResource(revised, cd); actual != resource {
		t.Fatal("reopened design did not retain original material owner", actual)
	}
	s.Validation.DevelopmentSnapshot = "changed-development"
	actual := s.taskResource(revised, cd)
	if actual.ID == resource.ID || actual.Source != "changed-development" || actual.Role != "whitebox-authoring" {
		t.Fatal("changed implementation reused obsolete production source", actual)
	}
}

// Workspace qualification uses the Controller's required task frontier and
// exact resource/material inputs; route context does not create another effect.
func TestWorkspaceQualificationUsesCurrentPhysicalInputs(t *testing.T) {
	t.Parallel()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	for _, withTask := range []bool{false, true} {
		t.Run(fmt.Sprintf("task-bound=%t", withTask), func(t *testing.T) {
			key := pipelineTask(cd, "engine.development.batch.v6", "plan:command")
			resource := NativeResource{ID: "implementation", Owner: "run", Role: "development", Path: "/work/implementation", Ref: "refs/implementation", Source: "source", Status: "PLANNED"}
			s := &State{RunID: "run", NativeRoot: "/work", RouteConfirmed: true, SelectedGates: []string{"quality"}, Validation: &Validation{RequirementRevision: "confirmed", Generation: 1}, NativeResources: map[string]NativeResource{resource.ID: resource}, DispatchBindings: map[string]WorkBinding{"developer": {Task: key, WorkspaceID: resource.ID, RequirementRevision: "confirmed", Generation: 1}}}
			required := map[authoring.StepID][]decision.RequiredTask{key.Step: {{Task: key}}}
			p := WorkspaceParameters{Resource: resource, RequirementRevision: "confirmed", Generation: 1, RouteScope: s.validationRouteDigest()}
			if withTask {
				p.Task = &key
			}
			s.SelectedGates = []string{"quality", "security"}
			if p.RouteScope == s.validationRouteDigest() || !s.workspaceParametersCurrent(p, cd, required) {
				t.Fatal("unchanged physical preparation lost qualification after route selection")
			}
			for _, change := range []string{"source", "path", "requirement", "generation", "task", "manifest", "removed-task", "cleanup"} {
				t.Run(change, func(t *testing.T) {
					changed := p
					frontier := required
					switch change {
					case "source":
						changed.Resource.Source = "other-source"
					case "path":
						changed.Resource.Path = "/work/other"
					case "requirement":
						changed.RequirementRevision = "other-requirement"
					case "generation":
						changed.Generation++
					case "task":
						other := key
						other.Scope = "other-work"
						changed.Task = &other
					case "manifest":
						changed.Manifest = &coverage.AcceptanceManifest{}
					case "removed-task":
						frontier = map[authoring.StepID][]decision.RequiredTask{}
					case "cleanup":
						values, err := normalizeHostActionParams(CleanupParameters{Resources: []NativeResource{resource}})
						if err != nil {
							t.Fatal(err)
						}
						s.PendingHostActions = map[string]HostActionIntent{"cleanup": {Adapter: &AdapterHostAction{Operation: "op.cleanup", Params: values}}}
						defer func() { s.PendingHostActions = nil }()
					}
					if s.workspaceParametersCurrent(changed, cd, frontier) {
						t.Fatal("route context admitted different or unavailable workspace inputs")
					}
				})
			}
		})
	}

	t.Run("blackbox mirror", func(t *testing.T) {
		key := pipelineTask(cd, "engine.qa.design.v6", "1:BLACKBOX")
		manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{RouteScope: "issued-route"}, Cases: []coverage.AcceptanceCase{{CaseID: "normal", Oracle: "accepted"}}}
		resource := NativeResource{ID: "design", Owner: "run", Role: "blackbox-design", Path: "/work/design", Source: "source", Status: "PLANNED"}
		s := &State{RunID: "run", NativeRoot: "/work", Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {Manifest: manifest}}}, NativeResources: map[string]NativeResource{resource.ID: resource}, DispatchBindings: map[string]WorkBinding{"designer": {Task: key, WorkspaceID: resource.ID, RequirementRevision: "confirmed", Generation: 1}}}
		required := map[authoring.StepID][]decision.RequiredTask{key.Step: {{Task: key}}}
		p := WorkspaceParameters{Task: &key, Resource: resource, RequirementRevision: "confirmed", Generation: 1, Manifest: &manifest, RouteScope: "issued-route"}
		if !s.workspaceParametersCurrent(p, cd, required) {
			t.Fatal("exact blackbox manifest lost qualification")
		}
		for _, change := range []string{"case-content", "route-binding", "missing-manifest"} {
			copy := manifest
			copy.Cases = append([]coverage.AcceptanceCase(nil), manifest.Cases...)
			changed := p
			changed.Manifest = &copy
			switch change {
			case "case-content":
				copy.Cases[0].Oracle = "different outcome"
			case "route-binding":
				copy.Binding.RouteScope = "different-route"
			case "missing-manifest":
				changed.Manifest = nil
			}
			if s.workspaceParametersCurrent(changed, cd, required) {
				t.Fatal("different blackbox mirror received execution authority", change)
			}
		}
	})

	t.Run("child target", func(t *testing.T) {
		topology := TopologyBinding{Decision: "split"}
		resource := NativeResource{ID: "child", Owner: "master", Role: "child-target", Path: "/targets/child", Source: "source", Status: "PLANNED"}
		s := &State{RunID: "master", Topology: &topology, ChildCreation: &ChildCreation{Topology: topology, RequirementRevision: "confirmed", Source: "source", Children: []ChildPreparation{{ResourceID: resource.ID}}}, NativeResources: map[string]NativeResource{resource.ID: resource}}
		p := WorkspaceParameters{Resource: resource, RequirementRevision: "confirmed"}
		if !s.workspaceParametersCurrent(p, cd) {
			t.Fatal("exact child preparation lost qualification")
		}
		for _, change := range []string{"source", "generation", "requirement", "route"} {
			changed := p
			switch change {
			case "source":
				changed.Resource.Source = "other-source"
			case "generation":
				changed.Generation = 1
			case "requirement":
				changed.RequirementRevision = "other-requirement"
			case "route":
				changed.RouteScope = "nonempty-route"
			}
			if s.workspaceParametersCurrent(changed, cd) {
				t.Fatal("different child preparation received execution authority", change)
			}
		}
	})

	t.Run("merge workspace", func(t *testing.T) {
		s := &State{RunID: "master", NativeRoot: "/work", RouteConfirmed: true, Topology: &TopologyBinding{Decision: "split"}, ChildCreation: &ChildCreation{Source: "source"}, Validation: &Validation{RequirementRevision: "confirmed", Generation: 1}, NativeResources: map[string]NativeResource{}}
		r := s.integrationResource()
		s.NativeResources[r.ID] = r
		p := WorkspaceParameters{Resource: r, RequirementRevision: "confirmed", Generation: 1, RouteScope: "signed-context"}
		if !s.workspaceParametersCurrent(p, cd) {
			t.Fatal("exact merge preparation lost qualification through route context")
		}
		p.Generation++
		if s.workspaceParametersCurrent(p, cd) {
			t.Fatal("different merge generation received execution authority")
		}
	})
}

// Workspace receipts initialize creation facts without replacing a later
// candidate identity or an already materialized exact manifest.
func TestWorkspaceReceiptPreservesCurrentResourceFacts(t *testing.T) {
	t.Parallel()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	resource := NativeResource{ID: "design", Owner: "run", Role: "blackbox-design", Path: "/work/design", Ref: "refs/design", Source: "source", Status: "PLANNED"}
	currentManifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{RouteScope: "current"}}
	registered := resource
	registered.Identity, registered.Status, registered.MirrorDigest = "candidate", "READY", BlackboxCasesDigest(resource.Owner, currentManifest)
	s := &State{RunID: resource.Owner, NativeResources: map[string]NativeResource{resource.ID: registered}, Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {Manifest: currentManifest}}}}
	engine := &Engine{cfg: Config{Definition: cd}}
	for _, manifest := range []*coverage.AcceptanceManifest{nil, {Binding: coverage.ManifestBinding{RouteScope: "obsolete"}}} {
		p := WorkspaceParameters{Resource: resource, RequirementRevision: "confirmed", Generation: 1, Manifest: manifest}
		params, err := normalizeHostActionParams(p)
		if err != nil {
			t.Fatal(err)
		}
		intent := HostActionIntent{Adapter: &AdapterHostAction{Operation: "op.workspace", Params: params}, PayloadDigest: "original-input"}
		proof := resource
		proof.Identity, proof.Status = resource.Source, "READY"
		if manifest != nil {
			proof.MirrorDigest = BlackboxCasesDigest(resource.Owner, *manifest)
		}
		values, err := normalizeHostActionParams(WorkspaceEvidence{ParametersDigest: intent.PayloadDigest, Resource: proof, Digest: "source-values", SourceDigest: "source-values"})
		if err != nil {
			t.Fatal(err)
		}
		receipt := HostActionReceipt{Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
		if err := engine.applyResourceReceipt(s, intent, receipt); err != nil || s.NativeResources[resource.ID] != registered {
			t.Fatal("original creation or obsolete mirror replaced later accepted native facts", err)
		}
	}
}
