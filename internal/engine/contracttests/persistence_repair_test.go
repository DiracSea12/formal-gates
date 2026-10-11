package contracttests

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	definition "formal-gates/internal/engine/testkit/workflow"
)

// A fulfilled observation is a durable completion fact for the concrete
// HostAction step. The normal settle/refill path must continue from it without
// executing the adapter a second time.
func TestPersistenceProtocolReconciledHostActionContinuesFrontier(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("decision.NewState: %v", err)
	}
	for _, step := range []authoring.StepID{"entry.parse", "entry.persist", "fan.split"} {
		if err := view.CompleteStep(step, fixture.Definition); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}
	if err := fixture.Initialize(view, "fake-host"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"stage": "qa-reconciled"})
	if err != nil {
		t.Fatalf("Execute host action: %v", err)
	}
	if intent.Step != "fan.transport" {
		t.Fatalf("intent step = %q, want fan.transport", intent.Step)
	}
	if _, err := fixture.Host.Receipt(intent, protocol.HostActionStatusUnknown); err != nil {
		t.Fatalf("submit UNKNOWN receipt: %v", err)
	}

	plan, err := fixture.Host.Reconcile(intent.ActionID, "sha256:qa-observation", true, false)
	if err != nil {
		t.Fatalf("ReconcileHostAction: %v", err)
	}
	if plan.Action != protocol.RecoveryReconcile {
		t.Fatalf("reconciliation plan = %+v, want RECONCILE", plan)
	}
	if got := fixture.Host.ActionCalls(intent.ActionID); got != 1 {
		t.Fatalf("adapter side effects = %d, want exactly 1", got)
	}

	snapshot, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load reconciled state: %v", err)
	}
	done := make(map[authoring.StepID]bool, len(snapshot.State.Completed))
	for _, step := range snapshot.State.Completed {
		done[step] = true
	}
	for _, step := range []authoring.StepID{"fan.transport", "fan.slice", "fan.join"} {
		if !done[step] {
			t.Fatalf("completed steps = %v, missing reconciled frontier step %s", snapshot.State.Completed, step)
		}
	}
	if _, pending := snapshot.State.PendingHostActions[intent.ActionID]; pending {
		t.Fatalf("reconciled intent remains pending: %s", intent.ActionID)
	}
	receipt, ok := snapshot.State.HostActionReceipts[intent.ActionID]
	if !ok || receipt.Step != "fan.transport" || receipt.Status != protocol.HostActionStatusReconciled {
		t.Fatalf("reconciled receipt = %+v, want fan.transport/RECONCILED", receipt)
	}

	var refilled []protocol.PendingAction
	for _, pending := range snapshot.State.PendingActions {
		if pending.Step == "review.worker" {
			refilled = append(refilled, pending)
		}
	}
	if len(refilled) != 1 || refilled[0].AttemptID == "" {
		t.Fatalf("post-reconciliation pending actions = %+v, want one review.worker refill", snapshot.State.PendingActions)
	}
}

func qaRepairBindingDefinition(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	version := definition.Version
	header := func(id string, deps ...authoring.StepID) authoring.Header {
		return authoring.Header{
			ID: authoring.StepID(id), NodeID: "repair", Dependencies: deps, DefinitionVersion: version,
		}
	}
	input := func(from authoring.StepID) authoring.IO {
		return authoring.IO{
			InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
			Inputs: []authoring.InputBinding{{From: from, OutputField: "out", ToField: "in"}},
		}
	}

	parse, err := authoring.NewLocalStep(header("repair.parse"), authoring.IO{
		InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
	}, authoring.LocalSpec{Handler: "engine.entry.parse"})
	if err != nil {
		t.Fatalf("create parse step: %v", err)
	}
	newAsk := func(id string) authoring.HumanAskStep {
		step, err := authoring.NewHumanAskStep(header(id, "repair.parse"), authoring.HumanAskSpec{
			AskKind: "decision", RequestSchema: "schema.ask.decision.request",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: time.Minute,
		})
		if err != nil {
			t.Fatalf("create Ask step %s: %v", id, err)
		}
		return step
	}
	newHostAction := func(id string) authoring.HostActionStep {
		step, err := authoring.NewHostActionStep(header(id, "repair.parse"), input("repair.parse"), authoring.HostActionSpec{
			Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI,
			Operation: "op.fan.transport", Schema: "schema.candidate.review-range", Timeout: time.Minute,
		})
		if err != nil {
			t.Fatalf("create HostAction step %s: %v", id, err)
		}
		return step
	}

	compiled, err := compiler.Compile(&compiler.Definition{
		Version: version, EntryNode: "repair",
		Steps: []authoring.Step{
			parse,
			newAsk("repair.ask.one"), newAsk("repair.ask.two"),
			newHostAction("repair.host.one"), newHostAction("repair.host.two"),
		},
	}, definition.Registry())
	if err != nil {
		t.Fatalf("compile binding definition: %v", err)
	}
	return compiled
}

func TestPersistenceProtocolBindsRepeatedAsksAndOperationsPerStep(t *testing.T) {
	compiled := qaRepairBindingDefinition(t)
	fixture, err := testkit.NewProtocolFixtureWithDefinition(t.TempDir(), compiled, definition.Registry(), 4)
	if err != nil {
		t.Fatalf("NewProtocolFixtureWithDefinition: %v", err)
	}
	view, err := decision.NewState(compiled.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("decision.NewState: %v", err)
	}
	if err := view.CompleteStep("repair.parse", compiled); err != nil {
		t.Fatalf("complete repair.parse: %v", err)
	}
	if err := fixture.Initialize(view, "fake-host"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	submit := func(event protocol.Event) protocol.Acceptance {
		t.Helper()
		fingerprint, err := fixture.Engine.ObserveFingerprint()
		if err != nil {
			t.Fatalf("ObserveFingerprint: %v", err)
		}
		acceptance, err := fixture.Engine.Submit(event, fingerprint)
		if err != nil {
			t.Fatalf("Submit %s: %v", event.ID, err)
		}
		return acceptance
	}

	requestOne, err := protocol.NewRequestEvent("qa-repair-request-one", protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("create first request: %v", err)
	}
	submit(requestOne)
	requestTwo, err := protocol.NewRequestEvent("qa-repair-request-two", protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}
	submit(requestTwo)

	snapshot, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load pending Asks: %v", err)
	}
	if snapshot.State.PendingAsks[string(requestOne.ID)].Step != "repair.ask.one" ||
		snapshot.State.PendingAsks[string(requestTwo.ID)].Step != "repair.ask.two" {
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
	decide(requestOne, "qa-repair-decision-one")
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after first decision: %v", err)
	}
	firstAsk, firstAskPending := snapshot.State.PendingAsks[string(requestOne.ID)]
	secondAsk, secondAskPending := snapshot.State.PendingAsks[string(requestTwo.ID)]
	if !firstAskPending || !firstAsk.Resolved || !secondAskPending || secondAsk.Resolved ||
		!qaStepCompleted(snapshot.State, "repair.ask.one") || qaStepCompleted(snapshot.State, "repair.ask.two") ||
		snapshot.State.Decisions[string(requestOne.ID)].Step != "repair.ask.one" {
		t.Fatalf("first decision crossed Ask boundary: asks=%+v completed=%v decisions=%+v", snapshot.State.PendingAsks, snapshot.State.Completed, snapshot.State.Decisions)
	}
	decide(requestTwo, "qa-repair-decision-two")
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after second decision: %v", err)
	}
	firstAsk, firstAskPending = snapshot.State.PendingAsks[string(requestOne.ID)]
	secondAsk, secondAskPending = snapshot.State.PendingAsks[string(requestTwo.ID)]
	if !firstAskPending || !firstAsk.Resolved || !secondAskPending || !secondAsk.Resolved ||
		!qaStepCompleted(snapshot.State, "repair.ask.one") || !qaStepCompleted(snapshot.State, "repair.ask.two") ||
		snapshot.State.Decisions[string(requestTwo.ID)].Step != "repair.ask.two" {
		t.Fatalf("Ask decisions = asks=%+v decisions=%+v completed=%v", snapshot.State.PendingAsks, snapshot.State.Decisions, snapshot.State.Completed)
	}

	first, err := fixture.Host.Execute("op.fan.transport", map[string]any{"stage": "first"})
	if err != nil {
		t.Fatalf("execute first repeated operation: %v", err)
	}
	if first.Step != "repair.host.one" {
		t.Fatalf("first operation step = %q, want repair.host.one", first.Step)
	}
	if _, err := fixture.Host.Receipt(first, protocol.HostActionStatusExecuted); err != nil {
		t.Fatalf("receipt for first operation: %v", err)
	}
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after first operation: %v", err)
	}
	firstReceipt := snapshot.State.HostActionReceipts[first.ActionID]
	if firstReceipt.Step != "repair.host.one" || firstReceipt.Status != protocol.HostActionStatusExecuted ||
		!qaStepCompleted(snapshot.State, "repair.host.one") || qaStepCompleted(snapshot.State, "repair.host.two") {
		t.Fatalf("first operation crossed HostAction boundary: receipt=%+v completed=%v", firstReceipt, snapshot.State.Completed)
	}

	second, err := fixture.Host.Execute("op.fan.transport", map[string]any{"stage": "second"})
	if err != nil {
		t.Fatalf("execute second repeated operation: %v", err)
	}
	if second.Step != "repair.host.two" {
		t.Fatalf("second operation step = %q, want repair.host.two", second.Step)
	}
	if _, err := fixture.Host.Receipt(second, protocol.HostActionStatusExecuted); err != nil {
		t.Fatalf("receipt for second operation: %v", err)
	}
	snapshot, err = fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after second operation: %v", err)
	}
	for _, step := range []authoring.StepID{"repair.host.one", "repair.host.two"} {
		if !qaStepCompleted(snapshot.State, step) {
			t.Fatalf("final completed steps = %v, missing %s", snapshot.State.Completed, step)
		}
	}
	if snapshot.State.HostActionReceipts[first.ActionID].Step != "repair.host.one" ||
		snapshot.State.HostActionReceipts[second.ActionID].Step != "repair.host.two" {
		t.Fatalf("final HostAction receipt bindings = %+v", snapshot.State.HostActionReceipts)
	}
}

func qaStepCompleted(state *protocol.State, wanted authoring.StepID) bool {
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

func TestPersistenceProtocolHostActionSchemasFenceIntentAndReceiptWrites(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	if _, err := fixture.PrepareReady(); err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	baseline, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load baseline: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	invalidIntents := []struct {
		name      string
		operation authoring.OperationID
		params    any
		code      string
	}{
		{"empty operation", "", map[string]any{}, protocol.CodeFreeCommandForm},
		{"unregistered operation", "op.not.registered", map[string]any{}, protocol.CodeOperationNotRegistered},
		{"free command form", "op.fan.transport", "run arbitrary command", protocol.CodeFreeCommandForm},
		{"undeclared parameter", "op.fan.transport", map[string]any{"command": "echo bypass"}, protocol.CodeOperationSchemaInvalid},
		{"wrong parameter type", "op.fan.transport", map[string]any{"stage": 42}, protocol.CodeOperationSchemaInvalid},
	}
	for _, tc := range invalidIntents {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := fixture.Engine.ExecuteHostAction(tc.operation, tc.params, fingerprint)
			if err == nil {
				t.Fatal("invalid HostAction intent was accepted")
			}
			persistenceFixtureRejectCode(t, err, tc.code)
			persistenceFixtureUnchanged(t, baseline, fixture.Engine)
		})
	}
	intent, revision, err := fixture.Engine.ExecuteHostAction("op.fan.transport", map[string]any{"stage": "qa-schema", "generation": 2}, fingerprint)
	if err != nil {
		t.Fatalf("valid HostAction intent: %v", err)
	}
	if revision != baseline.Revision+1 || intent.Operation != protocol.HostActionExecuteAdapterOperation || intent.Adapter == nil || intent.Resume != nil || intent.Terminate != nil || intent.PayloadDigest == "" {
		t.Fatalf("typed HostAction intent = %+v revision=%d", intent, revision)
	}
	afterIntent, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load persisted intent: %v", err)
	}
	pending, ok := afterIntent.State.PendingHostActions[intent.ActionID]
	if !ok || pending.Adapter == nil || pending.Adapter.Operation != intent.Adapter.Operation || pending.PayloadDigest != intent.PayloadDigest || pending.Adapter.Params["stage"] != "qa-schema" || pending.Adapter.Params["generation"] != float64(2) {
		t.Fatalf("pending HostAction = %+v present=%v", pending, ok)
	}
	invalidReceipt, err := protocol.NewAdapterHostActionReceiptEvent("qa-invalid-host-receipt", intent.ActionID, intent.Adapter.Operation, "fake-host", "qa-correlation", intent.PayloadDigest, protocol.HostActionStatusExecuted, "", map[string]any{
		"identity": "qa-host", "digest": "sha256:observed", "undeclared": true,
	})
	if err != nil {
		t.Fatalf("invalid receipt constructor: %v", err)
	}
	if _, err := fixture.Engine.Submit(invalidReceipt, fingerprint); err == nil {
		t.Fatal("receipt with undeclared evidence was accepted")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeOperationSchemaInvalid)
	}
	persistenceFixtureUnchanged(t, afterIntent, fixture.Engine)
	wantEvidence := map[string]any{"identity": "qa-receipt", "digest": "sha256:receipt"}
	validReceipt, err := protocol.NewAdapterHostActionReceiptEvent("qa-valid-host-receipt", intent.ActionID, intent.Adapter.Operation, "fake-host", "qa-correlation", intent.PayloadDigest, protocol.HostActionStatusExecuted, "", wantEvidence)
	if err != nil {
		t.Fatalf("valid receipt constructor: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint valid receipt: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(validReceipt, fingerprint)
	if err != nil || acceptance.Status != "ACCEPTED" || acceptance.Revision != afterIntent.Revision+1 {
		t.Fatalf("valid receipt acceptance = %+v err=%v", acceptance, err)
	}
	settled, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load settled HostAction: %v", err)
	}
	if _, ok := settled.State.PendingHostActions[intent.ActionID]; ok {
		t.Fatal("valid receipt left intent pending")
	}
	receipt, ok := settled.State.HostActionReceipts[intent.ActionID]
	if !ok || receipt.ActionID != intent.ActionID || receipt.Step != pending.Step || receipt.Operation != protocol.HostActionExecuteAdapterOperation || receipt.AdapterOperation != intent.Adapter.Operation || receipt.Provider != "fake-host" || receipt.Correlation != "qa-correlation" || receipt.PayloadDigest != intent.PayloadDigest || receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterEvidence == nil || !reflect.DeepEqual(receipt.AdapterEvidence.Values, wantEvidence) || receipt.Digest == "" {
		t.Fatalf("settled receipt = %+v present=%v", receipt, ok)
	}
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
	if !ok || pending.Task != action.Task || pending.Step != string(attempt.Step) || pending.AttemptID != attempt.ID {
		t.Fatalf("pending action = %+v present=%v", pending, ok)
	}
	persistenceFixtureLiveLedger(t, fixture.Engine, action.Task, attempt, pending, runtime.TaskIssued, baseline.Revision)
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	notCurrent, err := protocol.NewTaskEvent("qa-task-not-current", testkit.TaskKey("other", "entry.parse"), "att:missing", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("not-current event: %v", err)
	}
	if _, err := fixture.Engine.Submit(notCurrent, fingerprint); err == nil {
		t.Fatal("not-current task progress was accepted")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeEventNotCurrent)
	}
	persistenceFixtureUnchanged(t, baseline, fixture.Engine)
	illegal, err := protocol.NewTaskEvent("qa-task-illegal", action.Task, attempt.ID, runtime.TaskValidating)
	if err != nil {
		t.Fatalf("illegal transition event: %v", err)
	}
	if _, err := fixture.Engine.Submit(illegal, fingerprint); err == nil {
		t.Fatal("ISSUED to VALIDATING was accepted")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeIllegalTransition)
	}
	persistenceFixtureUnchanged(t, baseline, fixture.Engine)
	statuses := []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal}
	for index, status := range statuses {
		event, err := protocol.NewTaskEvent(protocol.EventID("qa-task-progress-"+string(status)), action.Task, attempt.ID, status)
		if err != nil {
			t.Fatalf("progress event %s: %v", status, err)
		}
		acceptance, err := fixture.Engine.Submit(event, fingerprint)
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

func TestPersistenceProtocolTerminalSummaryIsVersionBoundAndReplayOnly(t *testing.T) {
	root := t.TempDir()
	terminal, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "terminal"})
	if err != nil || terminal.Terminal == nil {
		t.Fatalf("terminal harness = %+v err=%v", terminal, err)
	}
	summary := *terminal.Terminal
	wantEnvelope, err := persistence.ExpectedEnvelope(testkit.HarnessPackageDigest)
	if err != nil {
		t.Fatalf("ExpectedEnvelope: %v", err)
	}
	gotEnvelope := persistence.Envelope{
		Writer: summary.Writer, StateSchemaVersion: summary.StateSchemaVersion,
		WorkflowDefinitionVersion: summary.WorkflowDefinitionVersion,
		DefinitionDigest:          summary.DefinitionDigest, PackageDigest: summary.PackageDigest,
	}
	if gotEnvelope != wantEnvelope || summary.Status != "ABORTED" || summary.Next.Kind != decision.KindComplete {
		t.Fatalf("terminal envelope/status = %+v/%s/%s", gotEnvelope, summary.Status, summary.Next.Kind)
	}
	request, err := protocol.NewRequestEvent("terminal-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if err != nil {
		t.Fatalf("terminal request: %v", err)
	}
	requestDigest, err := request.Digest()
	if err != nil {
		t.Fatalf("request digest: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent("terminal-decision", "terminal-request", summary.LastRequestReceipt.FreshnessToken, "abort")
	if err != nil {
		t.Fatalf("terminal decision: %v", err)
	}
	decisionDigest, err := decisionEvent.Digest()
	if err != nil {
		t.Fatalf("decision digest: %v", err)
	}
	if summary.LastRequestReceipt.EventID != string(request.ID) || summary.LastRequestDigest != requestDigest || summary.LastEventReceipt.EventID != string(decisionEvent.ID) || summary.LastEventDigest != decisionDigest {
		t.Fatalf("terminal receipt/digest binding = %+v", summary)
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal retained active state: %v", err)
	}
	before, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatalf("read terminal summary: %v", err)
	}
	query, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "query-terminal"})
	if err != nil || query.Terminal == nil || query.Terminal.Writer != summary.Writer || query.Terminal.StateSchemaVersion != summary.StateSchemaVersion || query.Terminal.WorkflowDefinitionVersion != summary.WorkflowDefinitionVersion || query.Terminal.DefinitionDigest != summary.DefinitionDigest || query.Terminal.PackageDigest != summary.PackageDigest || query.Terminal.Status != summary.Status || query.Terminal.Revision != summary.Revision || !reflect.DeepEqual(query.Terminal.LastRequestReceipt, summary.LastRequestReceipt) || query.Terminal.LastRequestDigest != summary.LastRequestDigest || !reflect.DeepEqual(query.Terminal.LastEventReceipt, summary.LastEventReceipt) || query.Terminal.LastEventDigest != summary.LastEventDigest || len(query.Next) != 1 || query.Next[0].Kind != decision.KindComplete {
		t.Fatalf("terminal query = %+v err=%v", query, err)
	}
	replay, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "terminal-replay"})
	if err != nil || len(replay.Acceptances) != 1 || !reflect.DeepEqual(replay.Acceptances[0], summary.LastEventReceipt) || len(replay.Next) != 1 || replay.Next[0].Kind != decision.KindComplete {
		t.Fatalf("terminal replay = %+v err=%v", replay, err)
	}
	if _, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "terminal-replay", PayloadDigest: "sha256:wrong"}); err == nil {
		t.Fatal("terminal replay accepted a different digest")
	} else {
		persistenceFixtureRejectCode(t, err, protocol.CodeDuplicateEventMismatch)
	}
	if _, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "submit-request", EventID: "qa-after-terminal"}); err == nil {
		t.Fatal("terminal path recreated active state")
	}
	after, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatalf("read summary after replay: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("terminal query/replay changed summary bytes")
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal query/replay recreated active state: %v", err)
	}
	for _, field := range []string{"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest", "packageDigest"} {
		t.Run("missing "+field, func(t *testing.T) {
			invalidRoot := t.TempDir()
			created, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: invalidRoot, Scenario: "terminal"})
			if err != nil {
				t.Fatalf("terminal fixture: %v", err)
			}
			data, err := os.ReadFile(created.SummaryPath)
			if err != nil {
				t.Fatalf("read summary: %v", err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("decode summary: %v", err)
			}
			delete(document, field)
			invalid, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatalf("encode summary: %v", err)
			}
			invalid = append(invalid, '\n')
			if err := os.WriteFile(created.SummaryPath, invalid, 0o600); err != nil {
				t.Fatalf("write invalid summary: %v", err)
			}
			if _, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: invalidRoot, Scenario: "query-terminal"}); err == nil {
				t.Fatalf("query accepted missing %s", field)
			} else {
				var unsupported *persistence.UnsupportedRunVersionError
				if !errors.As(err, &unsupported) || unsupported.Field != field || !strings.HasPrefix(err.Error(), persistence.UnsupportedRunVersionCode+":") {
					t.Fatalf("missing %s error = %v", field, err)
				}
			}
			after, err := os.ReadFile(created.SummaryPath)
			if err != nil {
				t.Fatalf("read invalid summary: %v", err)
			}
			if !bytes.Equal(after, invalid) {
				t.Fatalf("missing %s query rewrote summary", field)
			}
			if _, err := os.Stat(created.StatePath); !os.IsNotExist(err) {
				t.Fatalf("missing %s query recreated active state: %v", field, err)
			}
		})
	}
}
