package protocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/host"
	"formal-gates/internal/requirement"
	"formal-gates/internal/settings"
)

// boundLibrary serves one named configuration and records each requested name.
type boundLibrary struct {
	name  string
	value settings.Configuration
	err   error
	reads []string
}

func (l *boundLibrary) read(name string) (settings.Configuration, error) {
	l.reads = append(l.reads, name)
	if l.err != nil {
		return settings.Configuration{}, l.err
	}
	if name != l.name {
		return settings.Configuration{}, &ConfigurationUnavailable{Reason: "bound configuration " + name + " does not exist"}
	}
	return settings.CloneConfiguration(l.value), nil
}

// bindLibrary records the initial applied value of the built-in default name.
func bindLibrary(t *testing.T, e *Engine, value settings.Configuration) *boundLibrary {
	t.Helper()
	library := &boundLibrary{name: settings.DefaultConfigurationName, value: value}
	e.cfg.Configuration = library.read
	reconcile(t, e)
	return library
}

func reconcile(t *testing.T, e *Engine) {
	t.Helper()
	snapshot := e.LoadMustSucceed(t)
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
}

func executionLibrary(node string, choice ExecutionChoice) settings.Configuration {
	return settings.Configuration{QAModes: []string{}, Execution: map[string]ExecutionChoice{node: choice}}
}

func readyReviewEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	e, dir := selectedReviewEngine(t, nil)
	snapshot := e.LoadMustSucceed(t)
	snapshot.State.Validation = &Validation{RequirementRevision: snapshot.State.RequirementRevision(), Generation: snapshot.State.requirementGeneration(), Document: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}}
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	return e, dir
}

func TestRunCreationWritesConfigurationAndLimitHistory(t *testing.T) {
	e, dir := readyReviewEngine(t)
	before := stateBytesOf(t, dir)
	state := e.LoadMustSucceed(t).State
	if len(state.AppliedConfigurations) != 1 || state.ConfigurationName() != settings.DefaultConfigurationName {
		t.Fatal("a run was created without its initial configuration history")
	}
	entry := state.AppliedConfigurations[0]
	if entry.Revision != 1 || entry.Preview == nil || entry.Effect == nil || entry.Effect.AppliedRevision != 1 || entry.Changed != nil {
		t.Fatal("initial configuration facts were not recorded at creation", entry)
	}
	for _, series := range []review.Series{state.ReviewSeries.ProductReview, state.ReviewSeries.StartReadiness} {
		if !reflect.DeepEqual(series.LimitHistory, []review.LimitInterval{{Value: settings.DefaultRoundLimit, Revision: 1}}) {
			t.Fatal("initial round limit history was not recorded at creation", series.LimitHistory)
		}
	}
	if !slices.Equal(before, stateBytesOf(t, dir)) || len(state.Decisions) != 0 {
		t.Fatal("loading backfilled run facts or fabricated a user decision")
	}
}

func TestLibraryExecutionRebindsOnlyUnclaimedDispatch(t *testing.T) {
	t.Parallel()
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "claimed"}[claimed], func(t *testing.T) {
			e, dir := readyReviewEngine(t)
			library := bindLibrary(t, e, settings.Configuration{QAModes: []string{}})
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
			saved := ExecutionChoice{Provider: host.Claude, Model: "chosen-native-model", ReasoningEffort: "high"}
			library.value = executionLibrary("product-review", saved)
			if _, _, err := e.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			after := e.LoadMustSucceed(t).State
			want := saved
			if claimed {
				want = before.executionChoice(original.Bindings)
			}
			if got, ok := after.ActionExecutionChoice(action); !ok || got != want {
				t.Fatalf("execution binding = %+v/%t, want %+v", got, ok, want)
			}
			if after.EffectiveExecutionChoice("product-review") != saved || after.PendingActions[action] != pending || after.Attempts[pending.Task].ID != original.ID || after.Attempts[pending.Task].Attempts != original.Attempts || after.RunProvider != before.RunProvider || !reflect.DeepEqual(after.FlowSelection, before.FlowSelection) {
				t.Fatal("library value changed execution identity, policy, controller or flow selection")
			}
			if input, ok := after.WorkerInput(action, e.cfg.Definition); !ok || input.Execution != want || input.Provider != want.Provider {
				t.Fatal("native material did not project its action's execution choice", input)
			}
			history := after.AppliedConfigurations
			if len(history) != 2 || !slices.Equal(history[1].Changed, []string{"execution.product-review"}) || !reflect.DeepEqual(history[1].Configuration, library.value) {
				t.Fatalf("applied history = %+v", history)
			}
			reopened, err := New(mustStore(t, dir), e.cfg)
			if err != nil {
				t.Fatal(err)
			}
			recovered := reopened.LoadMustSucceed(t).State
			if got, _ := recovered.ActionExecutionChoice(action); got != want || recovered.EffectiveExecutionChoice("product-review") != saved {
				t.Fatal("restart changed original or future execution choice")
			}
		})
	}
}

func TestSavedLibraryValueReachesNextDispatch(t *testing.T) {
	t.Parallel()
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{}})
	revision := e.LoadMustSucceed(t).Revision
	if _, _, err := e.Plan(Observation{}); err != nil {
		t.Fatal(err)
	}
	if e.LoadMustSucceed(t).Revision != revision {
		t.Fatal("a read-only plan wrote the library value")
	}
	saved := ExecutionChoice{Provider: host.Claude, Model: "saved-model", ReasoningEffort: "low"}
	library.value = executionLibrary("product-review", saved)
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if len(state.PendingActions) != 1 {
		t.Fatal("drive did not issue the next dispatch", state.PendingActions)
	}
	for _, pending := range state.PendingActions {
		bindings := state.Attempts[pending.Task].Bindings
		if bindings.Responsibility != saved.Provider || *bindings.Model != saved.Model || *bindings.ReasoningEffort != saved.ReasoningEffort {
			t.Fatal("dispatch record does not carry the saved library value", bindings)
		}
	}
	settled := e.LoadMustSucceed(t).Revision
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if e.LoadMustSucceed(t).Revision != settled || len(e.LoadMustSucceed(t).State.AppliedConfigurations) != 2 {
		t.Fatal("an unchanged library produced a commit or history entry")
	}
}

func TestInitialSnapshotAndFieldLevelHistory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cd := compiledCanonicalWorkflow(t)
	initial := settings.Configuration{QAModes: []string{"blackbox"}, Execution: map[string]ExecutionChoice{"development": {Model: "dev-model"}}}
	library := &boundLibrary{name: "甲", value: initial}
	effective := &settings.Effective{ConfigurationName: "甲", Configuration: initial, ConfigurationSource: "/project/.gates/config.json"}
	e, err := New(mustStore(t, dir), Config{Settings: effective, Configuration: library.read, Definition: cd, Registry: definition.Registry(), Capacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.InitWithMetadata(view, testProvider, Observation{}, "bound-run", "regular", reviewConfirmation(), "", "", nil); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if len(state.AppliedConfigurations) != 1 || state.AppliedConfigurations[0].Revision != 1 || state.AppliedConfigurations[0].Changed != nil || !reflect.DeepEqual(state.AppliedConfigurations[0].Configuration, initial) {
		t.Fatalf("initial applied snapshot = %+v", state.AppliedConfigurations)
	}
	if state.ConfigurationName() != "甲" || !reflect.DeepEqual(state.Settings.Configuration, settings.Configuration{}) || state.Settings.ConfigurationSource != "" {
		t.Fatal("run kept a frozen configuration beside its binding", state.Settings)
	}
	gates := []string{"security"}
	library.value = settings.Configuration{
		FlowSelection: &settings.FlowSelection{Disabled: []string{"repair"}},
		QAModes:       []string{"blackbox", "whitebox"},
		Gates:         &gates,
		Execution:     map[string]ExecutionChoice{"development": {Model: "dev-model"}, "qa-whitebox": {Provider: host.Claude}},
	}
	reconcile(t, e)
	state = e.LoadMustSucceed(t).State
	latest := state.AppliedConfigurations[len(state.AppliedConfigurations)-1]
	want := []string{"execution.qa-whitebox", "flowSelection", "gates", "qaModes"}
	if len(state.AppliedConfigurations) != 2 || !slices.Equal(latest.Changed, want) || !reflect.DeepEqual(latest.Configuration, library.value) || latest.Revision != e.LoadMustSucceed(t).Revision {
		t.Fatalf("field-level history = %+v, want changed %v", latest, want)
	}
	if !slices.Equal(library.reads[len(library.reads)-1:], []string{"甲"}) {
		t.Fatal("reconciliation did not read the bound name", library.reads)
	}
}

func TestUnavailableConfigurationHoldsNewDispatchUntilResolved(t *testing.T) {
	t.Parallel()
	e, _ := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{}})
	assertHeld := func(detail string) {
		t.Helper()
		revision := e.LoadMustSucceed(t).Revision
		for _, plan := range []func() (*decision.Plan, uint64, error){func() (*decision.Plan, uint64, error) { return e.Plan(Observation{}) }, func() (*decision.Plan, uint64, error) { return e.Drive(Observation{}) }} {
			got, _, err := plan()
			if err != nil || got.Next.Kind != decision.KindWait || got.Next.Wait.Reason != decision.WaitConfigurationUnavailable || got.Next.Wait.Detail != detail {
				t.Fatalf("unavailable configuration plan = %+v %v", got, err)
			}
		}
		if state := e.LoadMustSucceed(t); state.Revision != revision || len(state.State.PendingActions) != 0 {
			t.Fatal("an unavailable configuration issued work or persisted a stop")
		}
	}
	library.err = &ConfigurationUnavailable{Reason: "config.json: expected a JSON object"}
	assertHeld("config.json: expected a JSON object")
	library.err, library.name = nil, "renamed"
	assertHeld("bound configuration 默认 does not exist")

	library.name, library.err = settings.DefaultConfigurationName, errors.New("config.json: file is being replaced")
	library.value = executionLibrary("product-review", ExecutionChoice{Provider: host.Claude, Model: "unread-model"})
	plan, _, err := e.Plan(Observation{})
	if err != nil || plan.Next.Kind != decision.KindReady {
		t.Fatal("a transient read stopped dispatch", plan, err)
	}
	reconcile(t, e)
	if state := e.LoadMustSucceed(t).State; len(state.AppliedConfigurations) != 1 || state.EffectiveExecutionChoice("product-review") != (ExecutionChoice{Provider: testProvider}) {
		t.Fatal("a transient read replaced the applied value", state.AppliedConfigurations)
	}

	library.err = nil
	library.value = executionLibrary("product-review", ExecutionChoice{Provider: host.Claude, Model: "restored-model"})
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	if len(state.PendingActions) != 1 {
		t.Fatal("resolved configuration did not resume dispatch")
	}
	for id := range state.PendingActions {
		if got, _ := state.ActionExecutionChoice(id); got.Model != "restored-model" {
			t.Fatal("resumed dispatch did not use the restored value", got)
		}
	}
}

func TestMergeQAExecutionFollowsSelectedQAModes(t *testing.T) {
	t.Parallel()
	black := ExecutionChoice{Provider: host.Codex, Model: "black-model", ReasoningEffort: "medium"}
	white := ExecutionChoice{Provider: host.Claude, Model: "white-model"}
	cd := compiledCanonicalWorkflow(t)
	for _, sample := range []struct {
		modes []string
		want  ExecutionChoice
	}{{[]string{"whitebox"}, white}, {[]string{"blackbox", "whitebox"}, white}, {[]string{"blackbox"}, black}} {
		state := &State{RunProvider: host.Codex, SliceChild: &SliceChildBinding{MasterRunID: "master", SliceID: "core"}, SelectedGates: []string{mergeQAGateID}, Validation: &Validation{Generation: 1}}
		state.AppliedConfigurations = []AppliedConfiguration{{Configuration: settings.Configuration{QAModes: sample.modes, Execution: map[string]ExecutionChoice{"qa-blackbox": black, "qa-whitebox": white}}}}
		task := pipelineTask(cd, "engine.qa.design.v6", state.nextDesignScope(coverage.ReviewMerge, cd))
		if node := state.ExecutionNode(task, cd); state.EffectiveExecutionChoice(node) != sample.want {
			t.Fatalf("merge QA with modes %v used %q %+v", sample.modes, node, state.EffectiveExecutionChoice(node))
		}
	}
	if (&State{}).ExecutionNode(runtime.TaskKey{}, cd) != "" {
		t.Fatal("unknown task invented a major node")
	}
}

func TestExecutionBindingsPreserveFieldPresence(t *testing.T) {
	t.Parallel()
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
			s.AppliedConfigurations = []AppliedConfiguration{{Configuration: executionLibrary("product-review", ExecutionChoice{Provider: host.Codex, Model: "future-model", ReasoningEffort: "low"})}}
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
	t.Parallel()
	e, dir := readyReviewEngine(t)
	snapshot := e.LoadMustSucceed(t)
	snapshot.State.Settings = &settings.Effective{ConfigurationName: settings.DefaultConfigurationName, Values: settings.Values{Model: "gpt-6.1-sol", ReasoningEffort: "high"}}
	if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{}})
	action := issueReady(t, e)[0]
	library.value = executionLibrary("product-review", ExecutionChoice{Provider: host.Claude})
	reconcile(t, e)
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

func TestLibraryExecutionPreservesParallelAndAuxiliaryResponsibilities(t *testing.T) {
	t.Parallel()
	e, before, issued := parallelValidationInputState(t)
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{"blackbox", "whitebox"}})
	saved := ExecutionChoice{Provider: host.Claude, Model: "qa-model", ReasoningEffort: "off"}
	library.value = settings.Configuration{QAModes: []string{"blackbox", "whitebox"}, Execution: map[string]ExecutionChoice{"qa-blackbox": saved}}
	reconcile(t, e)
	after := e.LoadMustSucceed(t).State
	qa, other := 0, 0
	for _, action := range issued {
		original, _ := before.ActionExecutionChoice(action.ActionID)
		want := original
		if after.ExecutionNode(action.Task, e.cfg.Definition) == "qa-blackbox" {
			want = saved
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

func TestMixedProviderEvidenceUsesOriginalActionAndLifecyclePair(t *testing.T) {
	t.Parallel()
	e, _, issued := parallelValidationInputState(t)
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{"blackbox", "whitebox"}})
	library.value = settings.Configuration{QAModes: []string{"blackbox", "whitebox"}, Execution: map[string]ExecutionChoice{"qa-blackbox": {Provider: host.Claude, Model: "qa-model"}}}
	reconcile(t, e)
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

func TestResumedWorkerKeepsOriginalChoiceAndExactStop(t *testing.T) {
	t.Parallel()
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
	s.AppliedConfigurations = append(s.AppliedConfigurations, AppliedConfiguration{Revision: 4, Configuration: executionLibrary("development", ExecutionChoice{Provider: host.Codex, Model: "future-model"})})
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

func TestQALibraryExecutionRefreshesOnlyUnclaimedGroup(t *testing.T) {
	t.Parallel()
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
	library := bindLibrary(t, e, settings.Configuration{QAModes: []string{"blackbox", "whitebox"}})
	e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p.ActionID == claimed {
			return &SpawnReceiptPayload{ActionID: claimed, Provider: a.Bindings.Responsibility, Correlation: "original-owner", Status: SpawnStatusUnknown}, nil
		}
		return nil, nil
	}
	black := ExecutionChoice{Provider: host.Claude, Model: "black-model", ReasoningEffort: "off"}
	white := ExecutionChoice{Provider: host.Codex, Model: "white-model", ReasoningEffort: "high"}
	library.value = settings.Configuration{QAModes: []string{"blackbox", "whitebox"}, Execution: map[string]ExecutionChoice{"qa-blackbox": black, "qa-whitebox": white}}
	reconcile(t, e)
	after := e.LoadMustSucceed(t).State
	whiteCount := 0
	for _, action := range issued {
		want, _ := before.ActionExecutionChoice(action.ActionID)
		if before.validationMode(action.Task) == coverage.ReviewWhitebox {
			want = white
			whiteCount++
		}
		if got, ok := after.ActionExecutionChoice(action.ActionID); !ok || got != want {
			t.Fatal("group value changed another group or native-owned call", action.Task, got, want)
		}
	}
	if whiteCount == 0 || after.EffectiveExecutionChoice("qa-blackbox") != black || after.EffectiveExecutionChoice("qa-whitebox") != white {
		t.Fatal("QA group values were not independent")
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

func TestConfigurationExecutionInheritsOnlyTheSelectedPlatformDefaults(t *testing.T) {
	t.Parallel()
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
			state := State{RunProvider: host.Codex, Settings: &settings.Effective{Values: settings.Values{Model: "run-model", ReasoningEffort: "high"}}, AppliedConfigurations: []AppliedConfiguration{{Configuration: executionLibrary("qa-blackbox", sample.configured)}}}
			body, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var recovered State
			if err := json.Unmarshal(body, &recovered); err != nil {
				t.Fatal(err)
			}
			if got := recovered.EffectiveExecutionChoice("qa-blackbox"); got != sample.want {
				t.Fatalf("configuration choice = %+v, want %+v", got, sample.want)
			}
		})
	}
}
