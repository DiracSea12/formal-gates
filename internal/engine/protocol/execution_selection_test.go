package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/host"
	"formal-gates/internal/requirement"
	"formal-gates/internal/settings"
)

func selectExecution(t *testing.T, e *Engine, id string, selection ExecutionSelection) (Event, Event) {
	t.Helper()
	request := Event{ID: EventID(id), Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectExecution, Execution: &selection, Options: []AskOption{{ID: "approve", Label: "Apply"}}}}
	accepted, err := e.Submit(request, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.Freshness(RequestID(accepted.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	decision := Event{ID: EventID(id + ":decision"), Kind: KindDecide, Decide: &DecidePayload{Request: RequestID(accepted.RequestID), Token: token, Choice: "approve", Execution: &ExecutionSelectionDecision{Digest: selection.CanonicalDigest()}}}
	if _, err := e.Submit(decision, Observation{}); err != nil {
		t.Fatal(err)
	}
	return request, decision
}

func TestExecutionSelectionChangesReadyOnlyBeforeNativeClaim(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "claimed"}[claimed], func(t *testing.T) {
			e, dir := selectedReviewEngine(t, nil)
			snapshot := e.LoadMustSucceed(t)
			snapshot.State.Validation = &Validation{RequirementRevision: snapshot.State.RequirementRevision(), Generation: snapshot.State.requirementGeneration(), Document: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}}
			if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
				t.Fatal(err)
			}
			action := issueReady(t, e)[0]
			before := e.LoadMustSucceed(t).State
			pending := before.PendingActions[action]
			original := before.Attempts[pending.Task]
			e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
				if p.ActionID != pending.ActionID || a.ID != original.ID {
					t.Fatalf("claim observation changed execution identity: %+v %+v", p, a)
				}
				if claimed {
					return &SpawnReceiptPayload{ActionID: action, Provider: original.Bindings.Responsibility, Correlation: "owned-job", Status: SpawnStatusUnknown}, nil
				}
				return nil, nil
			}
			selection := ExecutionSelection{Node: "product-review", Choice: ExecutionChoice{Provider: host.Claude, Model: "chosen-native-model", ReasoningEffort: "high"}}
			request, decision := selectExecution(t, e, "choose-product", selection)
			after := e.LoadMustSucceed(t).State
			want := selection.Choice
			if claimed {
				want = before.executionChoice(original.Bindings)
			}
			if got, ok := after.ActionExecutionChoice(action); !ok || got != want {
				t.Fatalf("execution binding = %+v/%t, want %+v", got, ok, want)
			}
			if after.EffectiveExecutionChoice(selection.Node) != selection.Choice || after.PendingActions[action] != pending || after.Attempts[pending.Task].ID != original.ID || after.Attempts[pending.Task].Attempts != original.Attempts || after.RunProvider != before.RunProvider || !reflect.DeepEqual(after.FlowSelection, before.FlowSelection) {
				t.Fatal("choice changed execution identity, policy, controller or flow selection")
			}
			input, ok := after.WorkerInput(action, e.cfg.Definition)
			if !ok || input.Execution != want || input.Provider != want.Provider {
				t.Fatal("native material did not project its action's execution choice", input)
			}
			persisted := stateBytesOf(t, dir)
			for _, event := range []Event{request, decision} {
				if _, err := e.Submit(event, Observation{}); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(persisted, stateBytesOf(t, dir)) {
				t.Fatal("accepted replay changed the durable decision or binding")
			}
			reopened, err := New(mustStore(t, dir), e.cfg)
			if err != nil {
				t.Fatal(err)
			}
			recovered := reopened.LoadMustSucceed(t).State
			if got, _ := recovered.ActionExecutionChoice(action); got != want || recovered.EffectiveExecutionChoice(selection.Node) != selection.Choice {
				t.Fatal("restart changed original or future execution choice")
			}
		})
	}
}

func TestExecutionBindingsPreserveFieldPresence(t *testing.T) {
	for _, test := range []struct {
		name, fields string
		want         ExecutionChoice
	}{
		{"omitted", `{}`, ExecutionChoice{Provider: host.Claude, Model: "gpt-6.1-sol", ReasoningEffort: "high"}},
		{"omitted-model", `{"reasoningEffort":"off"}`, ExecutionChoice{Provider: host.Claude, Model: "gpt-6.1-sol", ReasoningEffort: "off"}},
		{"omitted-effort", `{"model":"original-model"}`, ExecutionChoice{Provider: host.Claude, Model: "original-model", ReasoningEffort: "high"}},
		{"native-defaults", `{"model":"","reasoningEffort":""}`, ExecutionChoice{Provider: host.Claude}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var bindings AttemptBindings
			if err := json.Unmarshal([]byte(test.fields), &bindings); err != nil {
				t.Fatal(err)
			}
			s := richState(t)
			s.Settings = &settings.Effective{Values: settings.Values{Model: "gpt-6.1-sol", ReasoningEffort: "high"}}
			s.Decisions["future-review"] = RecordedDecision{Control: ControlSelectExecution, Choice: "approve", Revision: 99, Execution: &ExecutionSelection{Node: "product-review", Choice: ExecutionChoice{Provider: host.Codex, Model: "future-model", ReasoningEffort: "low"}}}
			pending := s.PendingActions[reviewAction]
			bindings.Task, bindings.Responsibility = pending.Task, host.Claude
			attempt := s.Attempts[pending.Task]
			attempt.Bindings = bindings
			s.Attempts[pending.Task] = attempt
			body, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var recovered State
			if err := json.Unmarshal(body, &recovered); err != nil {
				t.Fatal(err)
			}
			if got, ok := recovered.ActionExecutionChoice(reviewAction); !ok || got != test.want {
				t.Fatal("original binding lost frozen value or explicit omission", got, ok, test.want)
			}
			if !bindings.Equal(recovered.Attempts[pending.Task].Bindings) {
				t.Fatal("equal binding values changed identity across serialization")
			}
		})
	}
	empty := ""
	if (AttemptBindings{}).Equal(AttemptBindings{Model: &empty}) || (AttemptBindings{}).Equal(AttemptBindings{ReasoningEffort: &empty}) || (AttemptBindings{Model: &empty}).Equal(AttemptBindings{Model: &empty, ReasoningEffort: &empty}) {
		t.Fatal("field presence was discarded from binding identity")
	}
	if got := (&State{RunProvider: host.Codex}).executionChoice(AttemptBindings{Responsibility: host.Claude}); got != (ExecutionChoice{Provider: host.Claude}) {
		t.Fatal("missing fields invented a model or effort", got)
	}
}

func TestPlatformSelectionPreservesNativeOmissionAfterRestart(t *testing.T) {
	e, dir := selectedReviewEngine(t, nil)
	snapshot := e.LoadMustSucceed(t)
	snapshot.State.Settings = &settings.Effective{Values: settings.Values{Model: "gpt-6.1-sol", ReasoningEffort: "high"}}
	snapshot.State.Validation = &Validation{RequirementRevision: snapshot.State.RequirementRevision(), Generation: snapshot.State.requirementGeneration(), Document: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}}
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	action := issueReady(t, e)[0]
	selectExecution(t, e, "native-platform-defaults", ExecutionSelection{Node: "product-review", Choice: ExecutionChoice{Provider: host.Claude}})
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := reopened.LoadMustSucceed(t).State
	bindings := s.Attempts[s.PendingActions[action].Task].Bindings
	if bindings.Model == nil || bindings.ReasoningEffort == nil || *bindings.Model != "" || *bindings.ReasoningEffort != "" {
		t.Fatal("platform-only selection did not persist explicit native omission", bindings)
	}
	if input, ok := s.WorkerInput(action, e.cfg.Definition); !ok || input.Execution != (ExecutionChoice{Provider: host.Claude}) {
		t.Fatal("restart substituted run settings for native defaults", input.Execution, ok)
	}
}

func TestExecutionSelectionPreservesParallelAndAuxiliaryResponsibilities(t *testing.T) {
	e, before, issued := parallelValidationInputState(t)
	selection := ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Claude, Model: "qa-model", ReasoningEffort: "off"}}
	selectExecution(t, e, "choose-qa", selection)
	after := e.LoadMustSucceed(t).State
	qa, other := 0, 0
	for _, action := range issued {
		original, _ := before.ActionExecutionChoice(action.ActionID)
		want := original
		if after.ExecutionNode(action.Task, e.cfg.Definition) == "qa-blackbox" {
			want = selection.Choice
			qa++
		} else {
			other++
		}
		input, ok := after.WorkerInput(action.ActionID, e.cfg.Definition)
		if !ok || input.Execution != want {
			t.Fatal("parallel responsibility used another stage's choice", action.Task, input.Execution, want)
		}
	}
	if qa == 0 || other == 0 {
		t.Fatal("fixture did not exercise independent parallel responsibilities")
	}
	for _, handler := range []string{"engine.merge.resolve.v3", "engine.validation.observation.v2"} {
		task := pipelineTask(e.cfg.Definition, handler, "auxiliary")
		if node := after.ExecutionNode(task, e.cfg.Definition); node != "" || after.EffectiveExecutionChoice(node) != before.EffectiveExecutionChoice("") {
			t.Fatal("auxiliary worker became a different selectable responsibility", node)
		}
	}
}

func TestExecutionSelectionRejectsWrongDecisionWithoutChangingChoice(t *testing.T) {
	e, dir := selectedReviewEngine(t, nil)
	selection := ExecutionSelection{Node: "development", Choice: ExecutionChoice{Provider: host.Codex, Model: "native-model"}}
	request := Event{ID: "execution-offer", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectExecution, Execution: &selection, Options: []AskOption{{ID: "approve", Label: "Apply"}}}}
	accepted, err := e.Submit(request, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	decision := Event{ID: "wrong-execution", Kind: KindDecide, Decide: &DecidePayload{Request: RequestID(accepted.RequestID), Token: accepted.FreshnessToken, Choice: "approve", Execution: &ExecutionSelectionDecision{Digest: "different-choice"}}}
	if _, err := e.Submit(decision, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("wrong selection digest changed durable state", err)
	}
	request.ID, request.Request.Execution = "invalid-node", &ExecutionSelection{Node: "invented-node", Choice: selection.Choice}
	if _, err := e.Submit(request, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("unknown node changed durable state", err)
	}
}

func TestMixedProviderEvidenceUsesOriginalActionAndLifecyclePair(t *testing.T) {
	e, _, issued := parallelValidationInputState(t)
	selection := ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Claude, Model: "qa-model"}}
	selectExecution(t, e, "qa-provider", selection)
	var action string
	for _, item := range issued {
		if e.LoadMustSucceed(t).State.ExecutionNode(item.Task, e.cfg.Definition) == "qa-blackbox" {
			action = item.ActionID
			break
		}
	}
	claims := 0
	e.cfg.ClaimDispatch = func(run, id, identity, provider string) error {
		if run != e.LoadMustSucceed(t).State.RunID || id != action || identity != "shared-session" || provider != host.Claude {
			t.Fatal("dispatch claim lost original executor", run, id, identity, provider)
		}
		claims++
		return nil
	}
	wrong, err := NewSpawnReceiptEvent("wrong-platform", action, testProvider, "shared-session", SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(wrong, Observation{}); err == nil || rejectionCode(t, err) != CodeProviderMismatch {
		t.Fatal("controller provider was accepted as this worker's provider", err)
	}
	for _, edge := range []struct{ id, provider, event string }{{"controller-start", testProvider, LifecycleStart}, {"worker-wrong-stop", host.Claude, LifecycleStop}, {"worker-start", host.Claude, LifecycleStart}, {"worker-stop", host.Claude, LifecycleStop}} {
		event, err := NewCorrelatedLifecycleEvent(EventID(edge.id), edge.provider, "shared-session", "shared-session", edge.event)
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.Submit(event, Observation{})
		if edge.id == "worker-wrong-stop" {
			if err == nil || rejectionCode(t, err) != CodeEventSchemaInvalid {
				t.Fatal("one provider's start paired with another provider's stop", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	spawn, _ := NewSpawnReceiptEvent("actual-worker", action, host.Claude, "shared-session", SpawnStatusSpawned)
	if _, err := e.Submit(spawn, Observation{}); err != nil || claims != 1 {
		t.Fatal("native worker provider did not bind exactly once", err, claims)
	}
	s := e.LoadMustSucceed(t).State
	if len(s.lifecycleMatches(testProvider, "shared-session")) != 0 || !reflect.DeepEqual(s.lifecycleMatches(host.Claude, "shared-session"), []string{"shared-session"}) {
		t.Fatal("provider-scoped lifecycle proof was mixed")
	}
	e.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
		if ref.ActionID != action || ref.Provider != host.Claude || ref.Correlation != "shared-session" {
			t.Fatal("cost source used controller or future choice", ref)
		}
		return ResolvedCostSource{State: CostSourceReady, Usage: cost.Usage{OutputTokens: 7}}
	}
	if e.dispatchCostHeld(s, action) || s.Cost == nil || s.Cost.OutputTokens != 7 {
		t.Fatal("original worker usage did not settle", s.Cost)
	}
	wrongResult, _ := NewInvalidWorkerResultEvent("wrong-result-platform", action, testProvider, "output", "invalid native output")
	if _, err := e.Submit(wrongResult, Observation{}); err == nil || rejectionCode(t, err) != CodeProviderMismatch {
		t.Fatal("worker result used a different platform", err)
	}
}

func TestChildExecutionChoiceIsCreationSnapshot(t *testing.T) {
	parent := &State{RunProvider: host.Codex, Decisions: map[string]RecordedDecision{"chosen": {Control: ControlSelectExecution, Choice: "approve", Revision: 1, Execution: &ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Claude, Model: "parent-model"}}}}}
	parent.ChildCreation = &ChildCreation{Execution: parent.ExecutionSelections()}
	start := parent.ChildStart(ChildPreparation{SliceID: "core"})
	parent.Decisions["later"] = RecordedDecision{Control: ControlSelectExecution, Choice: "approve", Revision: 2, Execution: &ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Codex, Model: "later-model"}}}
	retried := parent.ChildStart(ChildPreparation{SliceID: "core"})
	if !reflect.DeepEqual(start.Binding.InheritedExecution, retried.Binding.InheritedExecution) {
		t.Fatal("retrying child creation changed the inherited execution choices")
	}
	child := &State{RunProvider: host.Codex, SliceChild: &start.Binding, Decisions: map[string]RecordedDecision{}}
	want := ExecutionChoice{Provider: host.Claude, Model: "parent-model"}
	if child.EffectiveExecutionChoice("qa-blackbox") != want || len(child.Decisions) != 0 {
		t.Fatal("child lost its inherited value or forged a user decision")
	}
	body, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(body, &recovered); err != nil || recovered.EffectiveExecutionChoice("qa-blackbox") != want {
		t.Fatal("restart changed inherited execution choice", err)
	}
	start.Binding.InheritedExecution[0].Choice.Model = "local-change"
	if reflect.DeepEqual(parent.ChildCreation.Execution, start.Binding.InheritedExecution) {
		t.Fatal("child binding shared the parent's immutable creation plan")
	}
	if parent.ExecutionNode(runtime.TaskKey{}, compiledCanonicalWorkflow(t)) != "" {
		t.Fatal("unknown task invented a major node")
	}
}

func TestResumedWorkerKeepsOriginalChoiceAndExactStop(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v6")
	s.ReconciledEffects = map[string]ReconciledEffect{}
	choice := ExecutionChoice{Provider: host.Claude, Model: "original-model", ReasoningEffort: "high"}
	attempt := s.Attempts[pending.Task]
	attempt.Bindings = AttemptBindings{Task: pending.Task, Responsibility: choice.Provider, Model: &choice.Model, ReasoningEffort: &choice.ReasoningEffort}
	s.Attempts[pending.Task] = attempt
	receipt := s.SpawnReceipts[pending.ActionID]
	receipt.Provider = choice.Provider
	s.SpawnReceipts[pending.ActionID] = receipt
	identity := receipt.agentIdentity()
	resume, err := e.agentHostIntent(s, HostActionResumeAgent, pending.ActionID, identity, "Resume the original work", "recover-original", 3)
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[resume.ActionID] = resume
	s.LifecycleEvents = []LifecycleEventRecord{{Provider: choice.Provider, Correlation: identity, Identity: identity, Event: LifecycleStart}, {Provider: choice.Provider, Correlation: identity, Identity: identity, Event: LifecycleStop}}
	resumed := &HostActionReceiptPayload{ActionID: resume.ActionID, Operation: HostActionResumeAgent, Provider: choice.Provider, PayloadDigest: resume.PayloadDigest, Status: HostActionStatusExecuted, LifecycleEvidence: &LifecycleEvidence{Identity: identity, Event: LifecycleStart}}
	if err := e.validateHostActionEvidence(s, resume, resumed); err == nil {
		t.Fatal("the original call's historical start proved a new resume")
	}
	s.LifecycleEvents = append(s.LifecycleEvents, LifecycleEventRecord{Provider: choice.Provider, Correlation: resume.ActionID, Identity: identity, Event: LifecycleStart})
	if err := e.validateHostActionEvidence(s, resume, resumed); err != nil {
		t.Fatal(err)
	}
	if err := s.checkHostActionProvider(resume.ActionID, s.RunProvider); err == nil {
		t.Fatal("resume accepted the controller as the original executor")
	}
	s.ReconciledEffects[resume.ActionID] = ReconciledEffect{Intent: &resume, Status: "FULFILLED"}
	s.HostActionReceipts[resume.ActionID] = HostActionReceipt{ActionID: resume.ActionID, Operation: HostActionResumeAgent, Provider: choice.Provider, Status: HostActionStatusExecuted}
	delete(s.PendingHostActions, resume.ActionID)
	s.Decisions["future-development"] = RecordedDecision{Control: ControlSelectExecution, Choice: "approve", Revision: 4, Execution: &ExecutionSelection{Node: "development", Choice: ExecutionChoice{Provider: host.Codex, Model: "future-model"}}}
	input, nativeIdentity, err := s.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, resume.ActionID, e.cfg.Definition)
	if err != nil || nativeIdentity != identity || input.Execution != choice {
		t.Fatal("settled resume lost its original execution selection", input.Execution, nativeIdentity, err)
	}
	if _, err := e.stopControlledWorkers(s, "stop-original", "Stop ", 5, true); err != nil {
		t.Fatal(err)
	}
	if s.PendingActions[pending.ActionID] != pending || s.ObsoleteActions[pending.ActionID].ActionID != "" {
		t.Fatal("a historical stop retired the current resumed call")
	}
	s.LifecycleEvents = append(s.LifecycleEvents, LifecycleEventRecord{Provider: host.Codex, Correlation: resume.ActionID, Identity: identity, Event: LifecycleStop})
	if s.hasCorrelatedLifecycleEvent(choice.Provider, resume.ActionID, identity, LifecycleStop) {
		t.Fatal("another provider's stop proved completion of this resume")
	}
	s.LifecycleEvents = append(s.LifecycleEvents, LifecycleEventRecord{Provider: choice.Provider, Correlation: resume.ActionID, Identity: identity, Event: LifecycleStop})
	if _, err := e.stopControlledWorkers(s, "stop-original", "Stop ", 6, true); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.ActionExecutionChoice(pending.ActionID); !ok || got != choice || s.executionChoice(s.ObsoleteActions[pending.ActionID].Bindings) != choice {
		t.Fatal("retirement changed the original execution selection", got, ok)
	}
}

func TestQAExecutionSelectionsUseLatestApplicableDecision(t *testing.T) {
	shared := ExecutionChoice{Provider: host.Claude, Model: "shared-model", ReasoningEffort: "high"}
	black := ExecutionChoice{Provider: host.Codex, Model: "black-model", ReasoningEffort: "medium"}
	white := ExecutionChoice{Provider: host.Claude, Model: "white-model"}
	state := &State{RunProvider: host.Codex, Decisions: map[string]RecordedDecision{}}
	for _, selection := range []ExecutionSelection{{Node: "qa", Choice: shared}, {Node: "qa-blackbox", Choice: black}, {Node: "qa-whitebox", Choice: white}, {Node: "qa", Choice: shared}} {
		revision := uint64(len(state.Decisions) + 1)
		state.Decisions[strconv.FormatUint(revision, 10)] = RecordedDecision{Control: ControlSelectExecution, Choice: "approve", Revision: revision, Execution: &selection}
		body, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var recovered State
		if err := json.Unmarshal(body, &recovered); err != nil {
			t.Fatal(err)
		}
		wantBlack, wantWhite := shared, shared
		if revision == 2 || revision == 3 {
			wantBlack = black
		}
		if revision == 3 {
			wantWhite = white
		}
		if recovered.EffectiveExecutionChoice("qa-blackbox") != wantBlack || recovered.EffectiveExecutionChoice("qa-whitebox") != wantWhite || recovered.EffectiveExecutionChoice("qa") != shared {
			t.Fatal("saved QA choices did not use their original applicable revisions", revision, recovered.ExecutionSelections())
		}
	}
	state.SliceChild = &SliceChildBinding{InheritedExecution: []ExecutionSelection{{Node: "qa-whitebox", Choice: white}, {Node: "qa", Choice: shared}}}
	state.Decisions = nil
	if state.EffectiveExecutionChoice("qa-blackbox") != shared || state.EffectiveExecutionChoice("qa-whitebox") != white {
		t.Fatal("shared child snapshot replaced its specific group value")
	}
	state.SliceChild.InheritedExecution = []ExecutionSelection{{Node: "qa", Choice: shared}}
	if state.EffectiveExecutionChoice("qa-blackbox") != shared || state.EffectiveExecutionChoice("qa-whitebox") != shared {
		t.Fatal("shared child snapshot lost either QA group")
	}
}

func TestQAExecutionSelectionRefreshesOnlyUnclaimedGroup(t *testing.T) {
	e, before, issued := parallelValidationInputState(t)
	var claimed string
	for _, action := range issued {
		if before.validationMode(action.Task) == coverage.ReviewBlackbox {
			claimed = action.ActionID
		}
	}
	if claimed == "" {
		t.Fatal("fixture did not issue real blackbox work")
	}
	e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p.ActionID == claimed {
			return &SpawnReceiptPayload{ActionID: claimed, Provider: a.Bindings.Responsibility, Correlation: "original-owner", Status: SpawnStatusUnknown}, nil
		}
		return nil, nil
	}
	black := ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Claude, Model: "black-model", ReasoningEffort: "off"}}
	white := ExecutionSelection{Node: "qa-whitebox", Choice: ExecutionChoice{Provider: host.Codex, Model: "white-model", ReasoningEffort: "high"}}
	selectExecution(t, e, "black-group", black)
	selectExecution(t, e, "white-group", white)
	after := e.LoadMustSucceed(t).State
	whiteCount := 0
	for _, action := range issued {
		want, _ := before.ActionExecutionChoice(action.ActionID)
		if before.validationMode(action.Task) == coverage.ReviewWhitebox {
			want = white.Choice
			whiteCount++
		}
		if got, ok := after.ActionExecutionChoice(action.ActionID); !ok || got != want {
			t.Fatal("group selection changed another group or native-owned call", action.Task, got, want)
		}
	}
	if whiteCount == 0 || after.EffectiveExecutionChoice(black.Node) != black.Choice || after.EffectiveExecutionChoice(white.Node) != white.Choice {
		t.Fatal("QA group choices were not independent")
	}
	entry := ConfirmedRevision{StateRevision: 100, Revision: "revised-requirement"}
	for _, action := range issued {
		entry.WorkDecisions = append(entry.WorkDecisions, RequirementWorkDecision{Work: RequirementWork{Task: action.Task}, Disposition: RequirementWorkRedo})
	}
	after.RequirementRevisions = append(after.RequirementRevisions, entry)
	for index, action := range issued {
		if got, want := after.ExecutionNode(replacementWorkTask(entry, index), e.cfg.Definition), before.ExecutionNode(action.Task, e.cfg.Definition); got != want {
			t.Fatal("authorized replacement lost its source QA group", got, want)
		}
	}
}

func TestSharedQARequestReplaysAndCompletesOriginalAsk(t *testing.T) {
	e, before, issued := parallelValidationInputState(t)
	selection := ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Claude, Model: "shared-native-model"}}
	request := Event{ID: "saved-shared-request", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectExecution, Execution: &selection, Options: []AskOption{{ID: "approve", Label: "Apply"}}}}
	accepted, err := e.Submit(request, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	// The saved request and Ask carry the shared QA format.
	snapshot := e.LoadMustSucceed(t)
	selection.Node = "qa"
	ask := snapshot.State.PendingAsks[accepted.RequestID]
	ask.Execution = &selection
	snapshot.State.PendingAsks[accepted.RequestID] = ask
	record := snapshot.State.Events[string(request.ID)]
	record.Digest, err = request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.State.Events[string(request.ID)] = record
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(e.store, e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Submit(request, Observation{}); err != nil {
		t.Fatal("saved shared request was not replayable", err)
	}
	token, err := reopened.Freshness(RequestID(accepted.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	decision := Event{ID: "saved-shared-decision", Kind: KindDecide, Decide: &DecidePayload{Request: RequestID(accepted.RequestID), Token: token, Choice: "approve", Execution: &ExecutionSelectionDecision{Digest: selection.CanonicalDigest()}}}
	if _, err := reopened.Submit(decision, Observation{}); err != nil {
		t.Fatal("saved shared Ask did not complete", err)
	}
	after := reopened.LoadMustSucceed(t).State
	for _, action := range issued {
		want, _ := before.ActionExecutionChoice(action.ActionID)
		if before.validationMode(action.Task) == coverage.ReviewBlackbox || before.validationMode(action.Task) == coverage.ReviewWhitebox {
			want = selection.Choice
		}
		if got, _ := after.ActionExecutionChoice(action.ActionID); got != want {
			t.Fatal("shared Ask lost its original QA scope", action.Task, got, want)
		}
	}
	request.ID = "new-shared-request"
	if _, err := reopened.Submit(request, Observation{}); err == nil {
		t.Fatal("new request accepted an ambiguous shared QA node")
	}
}

func TestSharedQACreationSnapshotPreservesMergeExecution(t *testing.T) {
	shared := ExecutionChoice{Provider: host.Claude, Model: "shared-qa-model", ReasoningEffort: "high"}
	parent := &State{RunProvider: host.Codex, Decisions: map[string]RecordedDecision{"shared": {Control: ControlSelectExecution, Choice: "approve", Revision: 1, Execution: &ExecutionSelection{Node: "qa", Choice: shared}}}}
	parent.ChildCreation = &ChildCreation{Execution: parent.ExecutionSelections()}
	start := parent.ChildStart(ChildPreparation{SliceID: "core"})
	parent.Decisions["later"] = RecordedDecision{Control: ControlSelectExecution, Choice: "approve", Revision: 2, Execution: &ExecutionSelection{Node: "qa-blackbox", Choice: ExecutionChoice{Provider: host.Codex, Model: "black-model"}}}
	child := &State{RunProvider: host.Codex, SliceChild: &start.Binding, SelectedGates: []string{mergeQAGateID}, Validation: &Validation{Generation: 1}}
	body, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(body, &recovered); err != nil {
		t.Fatal(err)
	}
	cd := compiledCanonicalWorkflow(t)
	task := pipelineTask(cd, "engine.qa.design.v6", recovered.nextDesignScope(coverage.ReviewMerge, cd))
	if recovered.ExecutionNode(task, cd) != "qa" || recovered.EffectiveExecutionChoice(recovered.ExecutionNode(task, cd)) != shared || recovered.EffectiveExecutionChoice("qa-blackbox") != shared || recovered.EffectiveExecutionChoice("qa-whitebox") != shared {
		t.Fatal("child creation lost shared QA's original modes or merge execution")
	}
}

func TestConfigurationExecutionInheritsOnlyTheSelectedPlatformDefaults(t *testing.T) {
	for _, sample := range []struct {
		name       string
		configured ExecutionChoice
		want       ExecutionChoice
	}{
		{"run-defaults", ExecutionChoice{}, ExecutionChoice{Provider: host.Codex, Model: "run-model", ReasoningEffort: "high"}},
		{"same-platform", ExecutionChoice{Provider: host.Codex}, ExecutionChoice{Provider: host.Codex, Model: "run-model", ReasoningEffort: "high"}},
		{"inherited-platform", ExecutionChoice{Model: "node-model"}, ExecutionChoice{Provider: host.Codex, Model: "node-model", ReasoningEffort: "high"}},
		{"different-platform", ExecutionChoice{Provider: host.Claude}, ExecutionChoice{Provider: host.Claude}},
		{"different-model", ExecutionChoice{Provider: host.Claude, Model: "node-model"}, ExecutionChoice{Provider: host.Claude, Model: "node-model"}},
		{"different-effort", ExecutionChoice{Provider: host.Claude, ReasoningEffort: "low"}, ExecutionChoice{Provider: host.Claude, ReasoningEffort: "low"}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			state := State{RunProvider: host.Codex, Settings: &settings.Effective{Values: settings.Values{Model: "run-model", ReasoningEffort: "high"}, Configuration: settings.Configuration{Execution: map[string]ExecutionChoice{"qa-blackbox": sample.configured}}}}
			if got := state.EffectiveExecutionChoice("qa-blackbox"); got != sample.want {
				t.Fatalf("configuration choice = %+v, want %+v", got, sample.want)
			}
			inherited := state.ExecutionSelections()
			state.Settings.Configuration.Execution["qa-blackbox"] = ExecutionChoice{Provider: host.ZCode, Model: "future-model"}
			child := State{RunProvider: host.Codex, Settings: state.Settings, SliceChild: &SliceChildBinding{InheritedExecution: inherited}}
			body, err := json.Marshal(child)
			if err != nil {
				t.Fatal(err)
			}
			var recovered State
			if err := json.Unmarshal(body, &recovered); err != nil {
				t.Fatal(err)
			}
			if got := recovered.EffectiveExecutionChoice("qa-blackbox"); got != sample.want {
				t.Fatalf("recovered child choice = %+v, want creation snapshot %+v", got, sample.want)
			}
		})
	}
}
