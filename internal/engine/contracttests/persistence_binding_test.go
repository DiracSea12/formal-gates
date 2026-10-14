package contracttests

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	definition "formal-gates/internal/engine/testkit/workflow"
)

func askBindingDefinition(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	version := definition.Version
	header := func(id string, deps ...authoring.StepID) authoring.Header {
		return authoring.Header{
			ID: authoring.StepID(id), NodeID: "ask-binding", Dependencies: deps, DefinitionVersion: version,
		}
	}

	parse, err := authoring.NewLocalStep(header("ask-binding.parse"), authoring.IO{
		InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
	}, authoring.LocalSpec{Handler: "engine.entry.parse"})
	if err != nil {
		t.Fatalf("create parse step: %v", err)
	}
	newAsk := func(id string) authoring.HumanAskStep {
		step, err := authoring.NewHumanAskStep(header(id, "ask-binding.parse"), authoring.HumanAskSpec{
			AskKind: "decision", RequestSchema: "schema.ask.decision.request",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: time.Minute,
		})
		if err != nil {
			t.Fatalf("create Ask step %s: %v", id, err)
		}
		return step
	}

	compiled, err := compiler.Compile(&compiler.Definition{
		Version: version, EntryNode: "ask-binding",
		Steps: []authoring.Step{
			parse,
			newAsk("ask-binding.ask.one"), newAsk("ask-binding.ask.two"),
		},
	}, definition.Registry())
	if err != nil {
		t.Fatalf("compile binding definition: %v", err)
	}
	return compiled
}

func TestPersistenceProtocolBindsRepeatedAsksPerStep(t *testing.T) {
	compiled := askBindingDefinition(t)
	fixture, err := testkit.NewProtocolFixtureWithDefinition(t.TempDir(), compiled, definition.Registry(), 4)
	if err != nil {
		t.Fatalf("NewProtocolFixtureWithDefinition: %v", err)
	}
	view, err := decision.NewState(compiled.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("decision.NewState: %v", err)
	}
	if err := view.CompleteStep("ask-binding.parse", compiled); err != nil {
		t.Fatalf("complete ask-binding.parse: %v", err)
	}
	if err := fixture.Initialize(view, "fake-host"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	submit := func(event protocol.Event) protocol.Acceptance {
		t.Helper()
		observedInput, err := fixture.VCS.Observe()
		if err != nil {
			t.Fatalf("Fingerprint: %v", err)
		}
		acceptance, err := fixture.Engine.Submit(event, observedInput)
		if err != nil {
			t.Fatalf("Submit %s: %v", event.ID, err)
		}
		return acceptance
	}

	requestOne, err := protocol.NewRequestEvent("ask-binding-request-one", protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("create first request: %v", err)
	}
	submit(requestOne)
	requestTwo, err := protocol.NewRequestEvent("ask-binding-request-two", protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}
	submit(requestTwo)

	snapshot, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load pending Asks: %v", err)
	}
	if snapshot.State.PendingAsks[string(requestOne.ID)].Step != "ask-binding.ask.one" ||
		snapshot.State.PendingAsks[string(requestTwo.ID)].Step != "ask-binding.ask.two" {
		t.Fatalf("pending Ask bindings = %+v", snapshot.State.PendingAsks)
	}

	decide := func(request protocol.Event, eventID protocol.EventID) {
		t.Helper()
		token, err := fixture.Engine.Freshness(protocol.RequestID(request.ID))
		if err != nil {
			t.Fatalf("Freshness %s: %v", request.ID, err)
		}
		event, err := protocol.NewDecideEvent(eventID, protocol.RequestID(request.ID), token, "cancel")
		if err != nil {
			t.Fatalf("create decision %s: %v", request.ID, err)
		}
		submit(event)
	}
	decide(requestOne, "ask-binding-decision-one")
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after first decision: %v", err)
	}
	firstAsk, firstAskPending := snapshot.State.PendingAsks[string(requestOne.ID)]
	secondAsk, secondAskPending := snapshot.State.PendingAsks[string(requestTwo.ID)]
	if !firstAskPending || !firstAsk.Resolved || !secondAskPending || secondAsk.Resolved ||
		!persistenceStepCompleted(snapshot.State, "ask-binding.ask.one") || persistenceStepCompleted(snapshot.State, "ask-binding.ask.two") ||
		snapshot.State.Decisions[string(requestOne.ID)].Step != "ask-binding.ask.one" {
		t.Fatalf("first decision crossed Ask boundary: asks=%+v completed=%v decisions=%+v", snapshot.State.PendingAsks, snapshot.State.Completed, snapshot.State.Decisions)
	}
	decide(requestTwo, "ask-binding-decision-two")
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after second decision: %v", err)
	}
	firstAsk, firstAskPending = snapshot.State.PendingAsks[string(requestOne.ID)]
	secondAsk, secondAskPending = snapshot.State.PendingAsks[string(requestTwo.ID)]
	if !firstAskPending || !firstAsk.Resolved || !secondAskPending || !secondAsk.Resolved ||
		!persistenceStepCompleted(snapshot.State, "ask-binding.ask.one") || !persistenceStepCompleted(snapshot.State, "ask-binding.ask.two") ||
		snapshot.State.Decisions[string(requestTwo.ID)].Step != "ask-binding.ask.two" {
		t.Fatalf("Ask decisions = asks=%+v decisions=%+v completed=%v", snapshot.State.PendingAsks, snapshot.State.Decisions, snapshot.State.Completed)
	}

}

func persistenceStepCompleted(state *protocol.State, wanted authoring.StepID) bool {
	for _, step := range state.Completed {
		if step == wanted {
			return true
		}
	}
	return false
}

func persistenceFixtureRejectCode(t *testing.T, err error, want string) {
	t.Helper()
	var rejected *protocol.RejectedError
	if !errors.As(err, &rejected) || rejected.Code != want {
		t.Fatalf("error = %v, want protocol code %s", err, want)
	}
}

func persistenceFixtureUnchanged(t *testing.T, before protocol.Snapshot, engine *protocol.Engine) {
	t.Helper()
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after rejected operation: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatalf("rejected operation changed state: revision %d -> %d", before.Revision, after.Revision)
	}
}

func persistenceFixtureLiveLedger(t *testing.T, engine *protocol.Engine, task runtime.TaskKey, attempt protocol.Attempt, pending protocol.PendingAction, status runtime.TaskStatus, revision uint64) {
	t.Helper()
	snapshot, err := engine.Load()
	if err != nil {
		t.Fatalf("Load live task state: %v", err)
	}
	if snapshot.Revision != revision || snapshot.State.TaskStatusOf(task) != status {
		t.Fatalf("live task revision/status = %d/%s, want %d/%s", snapshot.Revision, snapshot.State.TaskStatusOf(task), revision, status)
	}
	if len(snapshot.State.Expected) != 1 || snapshot.State.Expected[0] != task {
		t.Fatalf("expected ledger = %v, want [%s]", snapshot.State.Expected, task.String())
	}
	currentAttempt, ok := snapshot.State.Attempts[task]
	if !ok || !reflect.DeepEqual(currentAttempt, attempt) || len(snapshot.State.Attempts) != 1 {
		t.Fatalf("current Attempt = %+v present=%v, want exactly %+v", currentAttempt, ok, attempt)
	}
	currentPending, ok := snapshot.State.PendingActions[pending.ActionID]
	if !ok || !reflect.DeepEqual(currentPending, pending) || len(snapshot.State.PendingActions) != 1 {
		t.Fatalf("pending action = %+v present=%v, want exactly %+v", currentPending, ok, pending)
	}
}

// persistenceFixtureStateArtifacts 是状态目录的期望常驻集合：state.json 之外，
// write.lock 是持久的目录互斥工件（内核文件锁永不删除）。
var persistenceFixtureStateArtifacts = []string{"state.json", "write.lock"}

func persistenceFixtureArtifactNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestPersistenceProtocolTaskProgressMaintainsExpectedAttemptLedger(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	action := issued[0]
	baseline, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load issued state: %v", err)
	}
	attempt, ok := baseline.State.Attempts[action.Task]
	if !ok || attempt.ActionID != action.ActionID || attempt.ID == "" || attempt.Bindings.Task != action.Task || attempt.Bindings.Snapshot == "" || attempt.Bindings.Responsibility != "fake-host" || attempt.Plan.PlanDigest == "" || attempt.Plan.DefinitionDigest == "" || attempt.Plan.StateDigest == "" || attempt.Plan.ObservationDigest == "" {
		t.Fatalf("issued Attempt = %+v present=%v", attempt, ok)
	}
	pending, ok := baseline.State.PendingActions[action.ActionID]
	if !ok || pending.ActionID != action.ActionID || pending.Task != action.Task || pending.Step != string(attempt.Step) || pending.AttemptID != attempt.ID {
		t.Fatalf("pending action = %+v present=%v", pending, ok)
	}
	persistenceFixtureLiveLedger(t, fixture.Engine, action.Task, attempt, pending, runtime.TaskIssued, baseline.Revision)
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	notCurrent, err := protocol.NewTaskEvent("task-not-current", testkit.TaskKey("other", "entry.parse"), "att:missing", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("not-current event: %v", err)
	}
	if _, err := fixture.Engine.Submit(notCurrent, observedInput); err == nil {
		t.Fatal("not-current task progress was accepted")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeEventNotCurrent)
	}
	persistenceFixtureUnchanged(t, baseline, fixture.Engine)
	illegal, err := protocol.NewTaskEvent("task-illegal-transition", action.Task, attempt.ID, runtime.TaskValidating)
	if err != nil {
		t.Fatalf("illegal transition event: %v", err)
	}
	if _, err := fixture.Engine.Submit(illegal, observedInput); err == nil {
		t.Fatal("ISSUED to VALIDATING was accepted")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeIllegalTransition)
	}
	persistenceFixtureUnchanged(t, baseline, fixture.Engine)
	statuses := []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal}
	for index, status := range statuses {
		event, err := protocol.NewTaskEvent(protocol.EventID("task-progress-"+string(status)), action.Task, attempt.ID, status)
		if err != nil {
			t.Fatalf("progress event %s: %v", status, err)
		}
		acceptance, err := fixture.Engine.Submit(event, observedInput)
		if err != nil || acceptance.Status != "ACCEPTED" || acceptance.Revision != baseline.Revision+uint64(index)+1 {
			t.Fatalf("progress %s = %+v err=%v", status, acceptance, err)
		}
		if status != runtime.TaskTerminal {
			persistenceFixtureLiveLedger(t, fixture.Engine, action.Task, attempt, pending, status, acceptance.Revision)
		}
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load terminal state: %v", err)
	}
	if final.State.TaskStatusOf(action.Task) != runtime.TaskTerminal || len(final.State.Expected) != 0 || len(final.State.Attempts) != 0 || len(final.State.PendingActions) != 0 {
		t.Fatalf("terminal ledger = status=%s expected=%v attempts=%v pending=%v", final.State.TaskStatusOf(action.Task), final.State.Expected, final.State.Attempts, final.State.PendingActions)
	}
	if _, ok := final.State.Events[string(notCurrent.ID)]; ok {
		t.Fatal("not-current rejection entered event ledger")
	}
	if _, ok := final.State.Events[string(illegal.ID)]; ok {
		t.Fatal("illegal transition rejection entered event ledger")
	}
}
