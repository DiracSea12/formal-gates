package whitebox_qa

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/validate"
)

// A fulfilled observation is a durable completion fact for the concrete
// HostAction step. The normal settle/refill path must continue from it without
// executing the adapter a second time.
func TestQAPersistenceProtocolReconciledHostActionContinuesFrontier(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
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

	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "qa-reconciled"})
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
			Operation: "op.fan.transport", Schema: "schema.host.fan.transport", Timeout: time.Minute,
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

func TestQAPersistenceProtocolBindsRepeatedAsksAndOperationsPerStep(t *testing.T) {
	compiled := qaRepairBindingDefinition(t)
	fixture, err := testkit.NewProtocolFixtureWithDefinition(t.TempDir(), compiled, definition.Registry(), 4)
	if err != nil {
		t.Fatalf("NewProtocolFixtureWithDefinition: %v", err)
	}
	view, err := decision.NewState(compiled.Version, runtime.PhaseDevelopmentParallel)
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

	requestOne, err := protocol.NewRequestEvent("qa-repair-request-one", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("create first request: %v", err)
	}
	submit(requestOne)
	requestTwo, err := protocol.NewRequestEvent("qa-repair-request-two", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
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
		event, err := protocol.NewDecideEvent(eventID, protocol.RequestID(request.ID), token, "confirm")
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

	first, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "first"})
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

	second, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "second"})
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

const (
	qaP2PackageDigest = "sha256:qa-persistence-package"
	qaP2Fingerprint   = "sha256:qa-persistence-facts"
	qaP2Provider      = "qa-persistence-host"
)

func qaP2Store(t *testing.T, dir string, injector func(persistence.FaultPoint) error) *persistence.Store {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{
		PackageDigest: qaP2PackageDigest,
		FaultInjector: injector,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func qaP2Save(t *testing.T, store *persistence.Store, revision uint64, content any, collect func() (string, error)) persistence.SaveResult {
	t.Helper()
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision:    revision,
		ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint:  collect,
		Content:             content,
	})
	if err != nil {
		t.Fatalf("Save revision %d: %v", revision, err)
	}
	return result
}

func qaP2Engine(t *testing.T, collect func() (string, error)) *protocol.Engine {
	t.Helper()
	store := qaP2Store(t, t.TempDir(), nil)
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	engine, err := protocol.New(store, protocol.Config{
		Definition: compiled,
		Registry:   definition.Registry(),
		Capacity:   2,
	}, collect)
	if err != nil {
		t.Fatalf("protocol.New: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("decision.NewState: %v", err)
	}
	if err := engine.Init(view, qaP2Provider, qaP2Fingerprint); err != nil {
		t.Fatalf("Engine.Init: %v", err)
	}
	return engine
}

func qaP2Submit(t *testing.T, engine *protocol.Engine, event protocol.Event) protocol.Acceptance {
	t.Helper()
	acceptance, err := engine.Submit(event, qaP2Fingerprint)
	if err != nil {
		t.Fatalf("Submit %s: %v", event.ID, err)
	}
	return acceptance
}

func qaP2Lifecycle(t *testing.T, id protocol.EventID, identity, eventName string) protocol.Event {
	t.Helper()
	event, err := protocol.NewLifecycleEventEvent(id, qaP2Provider, identity, eventName)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	return event
}

func qaP2RejectCode(t *testing.T, err error, want string) {
	t.Helper()
	var rejected *protocol.RejectedError
	if !errors.As(err, &rejected) || rejected.Code != want {
		t.Fatalf("error = %v, want protocol code %s", err, want)
	}
}

func qaP2Unchanged(t *testing.T, before protocol.Snapshot, engine *protocol.Engine) {
	t.Helper()
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after rejected operation: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatalf("rejected operation changed state: revision %d -> %d", before.Revision, after.Revision)
	}
}

func qaP2LiveLedger(t *testing.T, engine *protocol.Engine, task runtime.TaskKey, attempt protocol.Attempt, pending protocol.PendingAction, status runtime.TaskStatus, revision uint64) {
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

func qaP2ArtifactNames(t *testing.T, dir string) []string {
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

func TestQAPersistenceProtocolEnvelopeIdentityRejectsEveryFieldBeforeWrite(t *testing.T) {
	seedDir := t.TempDir()
	stable := func() (string, error) { return qaP2Fingerprint, nil }
	qaP2Save(t, qaP2Store(t, seedDir, nil), 0, map[string]string{"value": "trusted"}, stable)
	statePath := filepath.Join(seedDir, "state.json")
	seedBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read seed state: %v", err)
	}
	var seed map[string]any
	if err := json.Unmarshal(seedBytes, &seed); err != nil {
		t.Fatalf("decode seed state: %v", err)
	}

	variants := []struct {
		name  string
		field string
		value any
		omit  bool
	}{
		{name: "missing writer", field: "writer", omit: true},
		{name: "foreign writer", field: "writer", value: "legacy"},
		{name: "missing schema", field: "stateSchemaVersion", omit: true},
		{name: "old schema", field: "stateSchemaVersion", value: "0"},
		{name: "missing definition version", field: "workflowDefinitionVersion", omit: true},
		{name: "old definition version", field: "workflowDefinitionVersion", value: "0"},
		{name: "missing definition digest", field: "definitionDigest", omit: true},
		{name: "foreign definition", field: "definitionDigest", value: "sha256:foreign-definition"},
		{name: "missing package digest", field: "packageDigest", omit: true},
		{name: "foreign package", field: "packageDigest", value: "sha256:foreign-package"},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			dir := t.TempDir()
			document := make(map[string]any, len(seed))
			for key, value := range seed {
				document[key] = value
			}
			if variant.omit {
				delete(document, variant.field)
			} else {
				document[variant.field] = variant.value
			}
			invalid, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatalf("encode invalid state: %v", err)
			}
			path := filepath.Join(dir, "state.json")
			invalid = append(invalid, '\n')
			if err := os.WriteFile(path, invalid, 0o600); err != nil {
				t.Fatalf("write invalid state: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read invalid state: %v", err)
			}
			collectorCalls := 0
			_, err = qaP2Store(t, dir, nil).Save(persistence.Transaction{
				ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
				CollectFingerprint: func() (string, error) {
					collectorCalls++
					return qaP2Fingerprint, nil
				},
				Content: map[string]string{"value": "must-not-write"},
			})
			var unsupported *persistence.UnsupportedRunVersionError
			if !errors.As(err, &unsupported) || unsupported.Field != variant.field {
				t.Fatalf("Save error = %v, want unsupported field %s", err, variant.field)
			}
			if collectorCalls != 0 {
				t.Fatalf("collector calls = %d, want zero before envelope validation", collectorCalls)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read state after rejection: %v", err)
			}
			if !bytes.Equal(before, after) || !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
				t.Fatalf("rejected envelope changed bytes or artifacts: names=%v", qaP2ArtifactNames(t, dir))
			}
		})
	}
}

func TestQAPersistenceProtocolEveryDurableCrashWindowConverges(t *testing.T) {
	cases := []struct {
		point    persistence.FaultPoint
		outcome  persistence.RecoveryOutcome
		revision uint64
		value    string
	}{
		{persistence.FaultTempSyncBefore, persistence.RecoveryClean, 1, "baseline"},
		{persistence.FaultTempSyncAfter, persistence.RecoveryClean, 1, "baseline"},
		{persistence.FaultIntentBefore, persistence.RecoveryClean, 1, "baseline"},
		{persistence.FaultIntentAfter, persistence.RecoveryRecovered, 2, "candidate"},
		{persistence.FaultReplaceBefore, persistence.RecoveryRecovered, 2, "candidate"},
		{persistence.FaultExecuteBefore, persistence.RecoveryRecovered, 2, "candidate"},
		{persistence.FaultReplaceAfter, persistence.RecoveryCommitted, 2, "candidate"},
		{persistence.FaultExecuteAfter, persistence.RecoveryCommitted, 2, "candidate"},
		{persistence.FaultObserveBefore, persistence.RecoveryCommitted, 2, "candidate"},
		{persistence.FaultReconcileBefore, persistence.RecoveryCommitted, 2, "candidate"},
		{persistence.FaultCommitResponseLost, persistence.RecoveryClean, 2, "candidate"},
	}
	for _, tc := range cases {
		t.Run(string(tc.point), func(t *testing.T) {
			dir := t.TempDir()
			stable := func() (string, error) { return qaP2Fingerprint, nil }
			qaP2Save(t, qaP2Store(t, dir, nil), 0, map[string]string{"value": "baseline"}, stable)
			faulted := qaP2Store(t, dir, func(point persistence.FaultPoint) error {
				if point == tc.point {
					return &persistence.InjectedCrashError{Point: point}
				}
				return nil
			})
			_, err := faulted.Save(persistence.Transaction{
				ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
				CollectFingerprint: stable, Content: map[string]string{"value": "candidate"},
			})
			if !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("faulted Save error = %v, want injected crash", err)
			}
			clean := qaP2Store(t, dir, nil)
			report, err := clean.Recover()
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if report.Outcome != tc.outcome || report.Revision != tc.revision {
				t.Fatalf("recovery report = %+v, want outcome=%s revision=%d", report, tc.outcome, tc.revision)
			}
			snapshot, err := clean.Load()
			if err != nil {
				t.Fatalf("Load recovered state: %v", err)
			}
			var content map[string]string
			if err := json.Unmarshal(snapshot.Content, &content); err != nil {
				t.Fatalf("decode recovered content: %v", err)
			}
			if snapshot.Revision != tc.revision || content["value"] != tc.value {
				t.Fatalf("recovered snapshot = revision %d content=%v", snapshot.Revision, content)
			}
			if !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
				t.Fatalf("recovery artifacts = %v", qaP2ArtifactNames(t, dir))
			}
		})
	}
}

func TestQAPersistenceProtocolFailureRoutingIsClosedAndNonAgentByDefault(t *testing.T) {
	routes := []struct {
		class  authoring.FailureClass
		action protocol.RecoveryAction
	}{
		{authoring.FailureTransientEngine, protocol.RecoveryResumeAttempt},
		{authoring.FailureBusinessReject, protocol.RecoveryFail},
		{authoring.FailureUserActionRequired, protocol.RecoveryAsk},
		{authoring.FailureSideEffectUnknown, protocol.RecoveryReconcile},
		{authoring.FailureInvariantViolation, protocol.RecoveryFail},
		{authoring.FailureBlockedBug, protocol.RecoveryFail},
		{authoring.FailureAgentRecoverable, protocol.RecoveryAgent},
		{authoring.FailureClass("UNRECOGNIZED"), protocol.RecoveryOperator},
	}
	for _, route := range routes {
		got := protocol.RouteFailure(route.class)
		if got.Class != route.class || got.Action != route.action {
			t.Fatalf("RouteFailure(%s) = %+v, want %s", route.class, got, route.action)
		}
		if got.Action == protocol.RecoveryAgent && route.class != authoring.FailureAgentRecoverable {
			t.Fatalf("failure class %s reached agent route", route.class)
		}
	}
	interruptions := []struct {
		name string
		in   protocol.Interruption
		want protocol.RecoveryAction
	}{
		{"stable transient", protocol.Interruption{Class: authoring.FailureTransientEngine}, protocol.RecoveryResumeAttempt},
		{"known transient", protocol.Interruption{Class: authoring.FailureTransientEngine, CauseKnown: true}, protocol.RecoveryNewAttempt},
		{"unknown stable", protocol.Interruption{}, protocol.RecoveryAsk},
		{"one receipt match", protocol.Interruption{ReceiptUnknown: true, LifecycleMatches: 1}, protocol.RecoveryAttachReceipt},
		{"no receipt match", protocol.Interruption{ReceiptUnknown: true}, protocol.RecoveryOperator},
		{"many receipt matches", protocol.Interruption{ReceiptUnknown: true, LifecycleMatches: 2}, protocol.RecoveryOperator},
	}
	for _, tc := range interruptions {
		t.Run(tc.name, func(t *testing.T) {
			plan := protocol.DecideRecovery(tc.in)
			if plan.Action != tc.want || plan.Action == protocol.RecoveryAgent {
				t.Fatalf("DecideRecovery(%+v) = %+v, want %s and non-agent default", tc.in, plan, tc.want)
			}
		})
	}
}

func TestQAPersistenceProtocolProviderAndAttemptRejectionsAreZeroWrite(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	action := issued[0]
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	intent, _, err := fixture.Engine.ExecuteHostAction("op.fan.transport", map[string]any{"target": "qa-provider"}, fingerprint)
	if err != nil {
		t.Fatalf("ExecuteHostAction: %v", err)
	}
	baseline, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load baseline: %v", err)
	}
	spawn, err := protocol.NewSpawnReceiptEvent("qa-wrong-provider-spawn", action.ActionID, "other-host", "agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("spawn event: %v", err)
	}
	result, err := protocol.NewWorkerResultEvent("qa-wrong-provider-result", action.ActionID, "other-host", protocol.OutcomePass, "sha256:result", "")
	if err != nil {
		t.Fatalf("result event: %v", err)
	}
	task, err := protocol.NewTaskEvent("qa-stale-attempt-task", action.Task, "att:stale", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	lifecycle, err := protocol.NewLifecycleEventEvent("qa-wrong-provider-lifecycle", "other-host", "agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("lifecycle event: %v", err)
	}
	hostReceipt, err := protocol.NewHostActionReceiptEvent("qa-wrong-provider-host-receipt", intent.ActionID, intent.Adapter.Operation, "other-host", "correlation", intent.PayloadDigest, protocol.HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("host receipt event: %v", err)
	}
	checks := []struct {
		name  string
		event protocol.Event
		code  string
	}{
		{"spawn provider", spawn, protocol.CodeProviderMismatch},
		{"worker provider", result, protocol.CodeProviderMismatch},
		{"task attempt", task, protocol.CodeStaleAttempt},
		{"lifecycle provider", lifecycle, protocol.CodeProviderMismatch},
		{"host receipt provider", hostReceipt, protocol.CodeProviderMismatch},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			fp, err := fixture.Engine.ObserveFingerprint()
			if err != nil {
				t.Fatalf("ObserveFingerprint: %v", err)
			}
			_, err = fixture.Engine.Submit(tc.event, fp)
			if err == nil {
				t.Fatalf("event %s was accepted", tc.event.ID)
			}
			qaP2RejectCode(t, err, tc.code)
			qaP2Unchanged(t, baseline, fixture.Engine)
		})
	}
}

func TestQAPersistenceProtocolEngineWritesStayInsideExplicitIsolationRoot(t *testing.T) {
	project, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatalf("NewIsolatedProject: %v", err)
	}
	before, err := project.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot before: %v", err)
	}
	fixture, err := testkit.NewProtocolFixture(project.Root)
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	if _, err := fixture.Host.Spawn(issued[0].ActionID, "qa-isolated-agent", protocol.SpawnStatusSpawned); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := fixture.Worker.Result(issued[0].ActionID, protocol.OutcomePass, "sha256:isolated", ""); err != nil {
		t.Fatalf("Result: %v", err)
	}
	after, err := project.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after: %v", err)
	}
	changes := (&testkit.FakeVCS{Root: project.Root}).Diff(before, after)
	if len(changes) != 1 || changes[0].Path != "engine-state/state.json" {
		t.Fatalf("isolated engine changes = %+v", changes)
	}
	for _, dir := range []string{project.HostConfig, project.State, project.Resources, project.StableState, project.StableRun} {
		if names := qaP2ArtifactNames(t, dir); len(names) != 0 {
			t.Fatalf("engine wrote namespace %s: %v", dir, names)
		}
	}
}

func TestQAPersistenceProtocolCASConflictSkipsFingerprintAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	stable := func() (string, error) { return qaP2Fingerprint, nil }
	qaP2Save(t, qaP2Store(t, dir, nil), 0, map[string]string{"value": "committed"}, stable)
	path := filepath.Join(dir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed state: %v", err)
	}
	collectorCalls := 0
	_, err = qaP2Store(t, dir, nil).Save(persistence.Transaction{
		ExpectedRevision: 0, ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint: func() (string, error) {
			collectorCalls++
			return qaP2Fingerprint, nil
		},
		Content: map[string]string{"value": "stale"},
	})
	var conflict *persistence.RevisionConflictError
	if !errors.As(err, &conflict) || conflict.Expected != 0 || conflict.Observed != 1 {
		t.Fatalf("stale Save error = %v, want revision conflict 0 -> 1", err)
	}
	if collectorCalls != 0 {
		t.Fatalf("collector calls = %d, want zero on CAS conflict", collectorCalls)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state after conflict: %v", err)
	}
	if !bytes.Equal(before, after) || !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
		t.Fatalf("CAS conflict changed bytes or artifacts: %v", qaP2ArtifactNames(t, dir))
	}
}

func TestQAPersistenceProtocolRecoveryQuarantinesStagedEnvelopeMismatch(t *testing.T) {
	dir := t.TempDir()
	stable := func() (string, error) { return qaP2Fingerprint, nil }
	qaP2Save(t, qaP2Store(t, dir, nil), 0, map[string]string{"value": "baseline"}, stable)
	faulted := qaP2Store(t, dir, func(point persistence.FaultPoint) error {
		if point == persistence.FaultIntentAfter {
			return &persistence.InjectedCrashError{Point: point}
		}
		return nil
	})
	_, err := faulted.Save(persistence.Transaction{
		ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint: stable, Content: map[string]string{"value": "staged"},
	})
	if !errors.Is(err, persistence.ErrInjectedCrash) {
		t.Fatalf("faulted Save error = %v, want injected crash", err)
	}
	stagedPath := ""
	for _, entry := range mustReadDir(t, dir) {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".state.json.") && strings.HasSuffix(entry.Name(), ".tmp") {
			stagedPath = filepath.Join(dir, entry.Name())
			break
		}
	}
	if stagedPath == "" {
		t.Fatal("fault after intent did not leave staged document")
	}
	stagedBytes, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("read staged document: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(stagedBytes, &document); err != nil {
		t.Fatalf("decode staged document: %v", err)
	}
	document["packageDigest"] = "sha256:foreign-runtime"
	stagedBytes, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode foreign staged document: %v", err)
	}
	if err := os.WriteFile(stagedPath, append(stagedBytes, '\n'), 0o600); err != nil {
		t.Fatalf("write foreign staged document: %v", err)
	}
	report, err := qaP2Store(t, dir, nil).Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Outcome != persistence.RecoveryResidual || report.Revision != 1 {
		t.Fatalf("recovery report = %+v, want residual revision 1", report)
	}
	snapshot, err := qaP2Store(t, dir, nil).Load()
	if err != nil {
		t.Fatalf("Load after recovery: %v", err)
	}
	var content map[string]string
	if err := json.Unmarshal(snapshot.Content, &content); err != nil {
		t.Fatalf("decode committed content: %v", err)
	}
	if snapshot.Revision != 1 || content["value"] != "baseline" {
		t.Fatalf("foreign staged document replaced baseline: revision=%d content=%v", snapshot.Revision, content)
	}
	if !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
		t.Fatalf("recovery left artifacts: %v", qaP2ArtifactNames(t, dir))
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	return entries
}

func TestQAPersistenceProtocolReplaySkipsFingerprintRevalidation(t *testing.T) {
	collectorCalls := 0
	var collectorErr error
	engine := qaP2Engine(t, func() (string, error) {
		collectorCalls++
		if collectorErr != nil {
			return "", collectorErr
		}
		return qaP2Fingerprint, nil
	})
	request, err := protocol.NewRequestEvent("qa-replay-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	first := qaP2Submit(t, engine, request)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before replay: %v", err)
	}
	callsBeforeReplay := collectorCalls
	collectorErr = errors.New("external facts unavailable")
	replayed, err := engine.Submit(request, "sha256:caller-value-is-ignored-on-replay")
	if err != nil {
		t.Fatalf("event replay error = %v", err)
	}
	if !reflect.DeepEqual(replayed, first) || collectorCalls != callsBeforeReplay {
		t.Fatalf("replay = %+v collector calls %d, want acceptance %+v and no new collection", replayed, collectorCalls, first)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after replay: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("same event replay changed durable state")
	}
}

func TestQAPersistenceProtocolNewEventIDDuplicatePayloadIsDurablyOccupied(t *testing.T) {
	engine := qaP2Engine(t, func() (string, error) { return qaP2Fingerprint, nil })
	request, err := protocol.NewRequestEvent("qa-duplicate-source", protocol.ControlAbort, protocol.AskOption{ID: "keep", Label: "keep"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	qaP2Submit(t, engine, request)
	firstLifecycle, err := protocol.NewLifecycleEventEvent("qa-lifecycle-original", qaP2Provider, "qa-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("first lifecycle: %v", err)
	}
	qaP2Submit(t, engine, firstLifecycle)
	token, err := engine.Freshness("qa-duplicate-source")
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before duplicate payload: %v", err)
	}
	duplicate, err := protocol.NewLifecycleEventEvent("qa-lifecycle-new-id", qaP2Provider, "qa-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("duplicate lifecycle: %v", err)
	}
	acceptance := qaP2Submit(t, engine, duplicate)
	if acceptance.Status != "DUPLICATE" || acceptance.Revision != before.Revision+1 {
		t.Fatalf("new-ID duplicate acceptance = %+v", acceptance)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after duplicate: %v", err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("duplicate did not consume a revision: %d -> %d", before.Revision, after.Revision)
	}
	if _, recorded := after.State.Events[string(duplicate.ID)]; !recorded {
		t.Fatal("new event ID was not durably occupied")
	}
	conflicting, err := protocol.NewLifecycleEventEvent(duplicate.ID, qaP2Provider, "qa-agent", protocol.LifecycleStop)
	if err != nil {
		t.Fatalf("conflicting lifecycle: %v", err)
	}
	if _, err := engine.Submit(conflicting, qaP2Fingerprint); err == nil {
		t.Fatal("occupied event ID accepted different payload")
	} else {
		qaP2RejectCode(t, err, protocol.CodeDuplicateEventMismatch)
	}
	stale, err := protocol.NewDecideEvent("qa-duplicate-stale-decision", "qa-duplicate-source", token, "keep")
	if err != nil {
		t.Fatalf("stale decision: %v", err)
	}
	if _, err := engine.Submit(stale, qaP2Fingerprint); err == nil {
		t.Fatal("duplicate payload failed to rotate freshness")
	} else {
		qaP2RejectCode(t, err, protocol.CodeStaleFreshness)
	}
	fresh, err := engine.Freshness("qa-duplicate-source")
	if err != nil {
		t.Fatalf("freshness after duplicate: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent("qa-duplicate-fresh-decision", "qa-duplicate-source", fresh, "keep")
	if err != nil {
		t.Fatalf("fresh decision: %v", err)
	}
	if acceptance := qaP2Submit(t, engine, decisionEvent); acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh decision acceptance = %+v", acceptance)
	}
}

func TestQAPersistenceProtocolUnknownReceiptRequiresMatchingLifecycleEvidence(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	actionID := issued[0].ActionID
	staged, err := fixture.Worker.ResultBeforeReceipt(actionID, protocol.OutcomePass, "sha256:waiting-result", "")
	if err != nil || staged.Status != "STAGED" {
		t.Fatalf("result-before-receipt = %+v err=%v", staged, err)
	}
	unknown, err := protocol.NewSpawnReceiptEvent("qa-unknown-spawn", actionID, "fake-host", "expected-agent", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatalf("unknown receipt: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(unknown, fingerprint)
	if err != nil || acceptance.RecoveryAction != string(protocol.RecoveryOperator) {
		t.Fatalf("unknown receipt acceptance = %+v err=%v", acceptance, err)
	}
	for _, event := range []struct {
		id       protocol.EventID
		identity string
		name     string
	}{
		{"qa-unrelated-start", "unrelated-agent", protocol.LifecycleStart},
		{"qa-unrelated-stop", "unrelated-agent", protocol.LifecycleStop},
	} {
		lifecycle, err := protocol.NewLifecycleEventEvent(event.id, "fake-host", event.identity, event.name)
		if err != nil {
			t.Fatalf("unrelated lifecycle %s: %v", event.name, err)
		}
		fingerprint, err = fixture.Engine.ObserveFingerprint()
		if err != nil {
			t.Fatalf("ObserveFingerprint unrelated lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(lifecycle, fingerprint); err != nil {
			t.Fatalf("submit unrelated lifecycle: %v", err)
		}
	}
	beforeUnrelated, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before unrelated reconcile: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint reconcile: %v", err)
	}
	plan, _, err := fixture.Engine.ReconcileUnknownReceipt(actionID, fingerprint)
	if err != nil || plan.Action != protocol.RecoveryOperator || plan.LifecycleMatches != 0 {
		t.Fatalf("unrelated lifecycle plan = %+v err=%v", plan, err)
	}
	afterUnrelated, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after unrelated reconcile: %v", err)
	}
	if afterUnrelated.Revision != beforeUnrelated.Revision+1 {
		t.Fatalf("operator reconcile revision = %d, want %d", afterUnrelated.Revision, beforeUnrelated.Revision+1)
	}
	if _, ok := afterUnrelated.State.StagedResults[actionID]; !ok {
		t.Fatal("unrelated lifecycle evidence released staged result")
	}
	if _, ok := afterUnrelated.State.PendingActions[actionID]; !ok {
		t.Fatal("unrelated lifecycle evidence retired pending action")
	}
	for _, event := range []struct {
		id       protocol.EventID
		identity string
		name     string
	}{
		{"qa-matching-start", "expected-agent", protocol.LifecycleStart},
		{"qa-matching-stop", "expected-agent", protocol.LifecycleStop},
	} {
		lifecycle, err := protocol.NewLifecycleEventEvent(event.id, "fake-host", event.identity, event.name)
		if err != nil {
			t.Fatalf("matching lifecycle %s: %v", event.name, err)
		}
		fingerprint, err = fixture.Engine.ObserveFingerprint()
		if err != nil {
			t.Fatalf("ObserveFingerprint matching lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(lifecycle, fingerprint); err != nil {
			t.Fatalf("submit matching lifecycle: %v", err)
		}
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint matching reconcile: %v", err)
	}
	plan, _, err = fixture.Engine.ReconcileUnknownReceipt(actionID, fingerprint)
	if err != nil || plan.Action != protocol.RecoveryAttachReceipt || plan.LifecycleMatches != 1 {
		t.Fatalf("matching lifecycle plan = %+v err=%v", plan, err)
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after matching reconcile: %v", err)
	}
	if _, ok := final.State.StagedResults[actionID]; ok {
		t.Fatal("matching evidence left staged result")
	}
	if _, ok := final.State.Results[actionID]; !ok {
		t.Fatal("matching evidence did not record worker result")
	}
	if _, ok := final.State.PendingActions[actionID]; ok {
		t.Fatal("matching evidence left completed action pending")
	}
}

func TestQAPersistenceProtocolRecoveryAskPreservesCurrentAttempt(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	actionID := issued[0].ActionID
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before recovery: %v", err)
	}
	pending := before.State.PendingActions[actionID]
	attempt := before.State.Attempts[pending.Task]
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{}, fingerprint)
	if err != nil || plan.Action != protocol.RecoveryAsk || plan.RequestID == "" {
		t.Fatalf("unknown-cause recovery plan = %+v err=%v", plan, err)
	}
	optionIDs := make([]protocol.AskOptionID, 0, len(plan.Options))
	for _, option := range plan.Options {
		optionIDs = append(optionIDs, option.ID)
	}
	if !reflect.DeepEqual(optionIDs, []protocol.AskOptionID{"resume", "fresh", "abort"}) {
		t.Fatalf("recovery Ask options = %v", optionIDs)
	}
	asked, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load recovery Ask: %v", err)
	}
	if got := asked.State.Attempts[pending.Task]; got.ID != attempt.ID || got.ActionID != attempt.ActionID {
		t.Fatalf("recovery Ask changed current Attempt: before=%+v after=%+v", attempt, got)
	}
	if _, ok := asked.State.PendingActions[actionID]; !ok {
		t.Fatal("recovery Ask retired current action")
	}
	if _, ok := asked.State.ObsoleteActions[actionID]; ok {
		t.Fatal("recovery Ask marked current action obsolete")
	}
	ask, ok := asked.State.PendingAsks[plan.RequestID]
	if !ok || ask.Resolved || ask.Control != protocol.ControlRecovery {
		t.Fatalf("durable recovery Ask = %+v present=%v", ask, ok)
	}
	token, err := fixture.Engine.Freshness(protocol.RequestID(plan.RequestID))
	if err != nil {
		t.Fatalf("Freshness recovery Ask: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent("qa-recovery-decision", protocol.RequestID(plan.RequestID), token, "fresh")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint recovery decision: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(decisionEvent, fingerprint)
	if err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("recovery decision acceptance = %+v err=%v", acceptance, err)
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after recovery decision: %v", err)
	}
	if got := final.State.Attempts[pending.Task]; got.ID != attempt.ID || got.ActionID != attempt.ActionID {
		t.Fatalf("recovery decision replaced current Attempt: before=%+v after=%+v", attempt, got)
	}
	if final.State.Decisions[plan.RequestID].Choice != "fresh" {
		t.Fatalf("recorded recovery choice = %q", final.State.Decisions[plan.RequestID].Choice)
	}
}

func TestQAPersistenceProtocolIntegrityMismatchFencesEveryStateOperation(t *testing.T) {
	dir := t.TempDir()
	stable := func() (string, error) { return qaP2Fingerprint, nil }
	store := qaP2Store(t, dir, nil)
	qaP2Save(t, store, 0, map[string]string{"value": "trusted"}, stable)
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trusted state: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode trusted state: %v", err)
	}
	content, ok := document["content"].(map[string]any)
	if !ok {
		t.Fatalf("state content type = %T, want object", document["content"])
	}
	content["value"] = "tampered"
	corrupt, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode corrupt state: %v", err)
	}
	corrupt = append(corrupt, '\n')
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	assertMismatch := func(operation string, operationErr error) {
		t.Helper()
		var mismatch *persistence.IntegrityMismatchError
		if !errors.As(operationErr, &mismatch) || mismatch.Path != path || !strings.HasPrefix(operationErr.Error(), persistence.StateIntegrityCode+":") {
			t.Fatalf("%s error = %v, want %s integrity mismatch", operation, operationErr, persistence.StateIntegrityCode)
		}
	}
	_, err = store.Load()
	assertMismatch("Load", err)
	collectorCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint: func() (string, error) {
			collectorCalls++
			return qaP2Fingerprint, nil
		},
		Content: map[string]string{"value": "replacement"},
	})
	assertMismatch("Save", err)
	if collectorCalls != 0 {
		t.Fatalf("integrity failure collected fingerprint %d times", collectorCalls)
	}
	_, err = store.Recover()
	assertMismatch("Recover", err)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corrupt state after operations: %v", err)
	}
	if !bytes.Equal(after, corrupt) || !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
		t.Fatalf("integrity fence changed bytes or artifacts: %v", qaP2ArtifactNames(t, dir))
	}
}

func TestQAPersistenceProtocolFingerprintChangesDistinguishRejectedAndCommittedWrites(t *testing.T) {
	dir := t.TempDir()
	stable := func() (string, error) { return qaP2Fingerprint, nil }
	store := qaP2Store(t, dir, nil)
	qaP2Save(t, store, 0, map[string]string{"value": "baseline"}, stable)
	path := filepath.Join(dir, "state.json")
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	beforeCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint: func() (string, error) {
			beforeCalls++
			return "sha256:changed-before", nil
		},
		Content: map[string]string{"value": "rejected"},
	})
	var beforeErr *persistence.FingerprintChangedError
	if !errors.As(err, &beforeErr) || beforeErr.Phase != persistence.FingerprintPhaseBefore || beforeErr.Committed || beforeCalls != 1 {
		t.Fatalf("pre-write fingerprint result = %v calls=%d", err, beforeCalls)
	}
	afterRejected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state after pre-write change: %v", err)
	}
	if !bytes.Equal(afterRejected, baseline) || !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
		t.Fatalf("pre-write change modified state/artifacts: %v", qaP2ArtifactNames(t, dir))
	}
	afterCalls := 0
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision: 1, ExpectedFingerprint: qaP2Fingerprint,
		CollectFingerprint: func() (string, error) {
			afterCalls++
			if afterCalls == 1 {
				return qaP2Fingerprint, nil
			}
			return "sha256:changed-after", nil
		},
		Content: map[string]string{"value": "candidate"},
	})
	var afterErr *persistence.FingerprintChangedError
	if !errors.As(err, &afterErr) || afterErr.Phase != persistence.FingerprintPhaseAfter || !afterErr.Committed || result.Revision != 2 || afterCalls != 2 {
		t.Fatalf("post-write fingerprint result=%+v err=%v calls=%d", result, err, afterCalls)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load committed candidate: %v", err)
	}
	var content map[string]string
	if err := json.Unmarshal(snapshot.Content, &content); err != nil {
		t.Fatalf("decode candidate: %v", err)
	}
	if snapshot.Revision != 2 || content["value"] != "candidate" || !reflect.DeepEqual(qaP2ArtifactNames(t, dir), []string{"state.json"}) {
		t.Fatalf("post-write state = revision %d content=%v artifacts=%v", snapshot.Revision, content, qaP2ArtifactNames(t, dir))
	}
}

func TestQAPersistenceProtocolSameEventIDDifferentDigestIsZeroWrite(t *testing.T) {
	collectorCalls := 0
	engine := qaP2Engine(t, func() (string, error) {
		collectorCalls++
		return qaP2Fingerprint, nil
	})
	first, err := protocol.NewRequestEvent("qa-event-key", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	qaP2Submit(t, engine, first)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before conflict: %v", err)
	}
	callsBefore := collectorCalls
	conflicting, err := protocol.NewRequestEvent("qa-event-key", protocol.ControlAbort, protocol.AskOption{ID: "confirm", Label: "different"})
	if err != nil {
		t.Fatalf("conflicting request: %v", err)
	}
	if _, err := engine.Submit(conflicting, qaP2Fingerprint); err == nil {
		t.Fatal("same event ID with different digest was accepted")
	} else {
		qaP2RejectCode(t, err, protocol.CodeDuplicateEventMismatch)
	}
	if collectorCalls != callsBefore {
		t.Fatalf("conflicting event collected fingerprint: %d -> %d", callsBefore, collectorCalls)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after conflict: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("different-digest replay changed durable state")
	}
}

func TestQAPersistenceProtocolInterveningCommitInvalidatesFreshnessToken(t *testing.T) {
	engine := qaP2Engine(t, func() (string, error) { return qaP2Fingerprint, nil })
	request, err := protocol.NewRequestEvent("qa-freshness-request", protocol.ControlReset, protocol.AskOption{ID: "proceed", Label: "proceed"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	qaP2Submit(t, engine, request)
	staleToken, err := engine.Freshness("qa-freshness-request")
	if err != nil {
		t.Fatalf("Freshness before intervening commit: %v", err)
	}
	observation, err := protocol.NewCorrelatedLifecycleEvent("qa-intervening-observation", qaP2Provider, "qa-correlation", "qa-identity", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("observation: %v", err)
	}
	qaP2Submit(t, engine, observation)
	intervening, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after intervening commit: %v", err)
	}
	if len(intervening.State.LifecycleEvents) != 1 || intervening.State.LifecycleEvents[0].Correlation != "qa-correlation" {
		t.Fatalf("intervening lifecycle = %+v", intervening.State.LifecycleEvents)
	}
	stale, err := protocol.NewDecideEvent("qa-stale-decision", "qa-freshness-request", staleToken, "proceed")
	if err != nil {
		t.Fatalf("stale decision: %v", err)
	}
	if _, err := engine.Submit(stale, qaP2Fingerprint); err == nil {
		t.Fatal("stale freshness token was accepted")
	} else {
		qaP2RejectCode(t, err, protocol.CodeStaleFreshness)
	}
	qaP2Unchanged(t, intervening, engine)
	freshToken, err := engine.Freshness("qa-freshness-request")
	if err != nil || freshToken == staleToken {
		t.Fatalf("freshness after intervening commit = %q err=%v, stale=%q", freshToken, err, staleToken)
	}
	fresh, err := protocol.NewDecideEvent("qa-fresh-decision", "qa-freshness-request", freshToken, "proceed")
	if err != nil {
		t.Fatalf("fresh decision: %v", err)
	}
	if acceptance := qaP2Submit(t, engine, fresh); acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh decision acceptance = %+v", acceptance)
	}
}

func TestQAPersistenceProtocolConcurrentSubmitConvergesWithoutLostUpdate(t *testing.T) {
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
	first, err := protocol.NewRequestEvent("qa-concurrent-first", protocol.ControlReset, protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := protocol.NewRequestEvent("qa-concurrent-second", protocol.ControlAbort, protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	results := testkit.SubmitConcurrently(
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(first, fingerprint) },
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(second, fingerprint) },
	)
	events := []protocol.Event{first, second}
	successes := 0
	for index, submitErr := range results {
		if submitErr == nil {
			successes++
			continue
		}
		var conflict *persistence.RevisionConflictError
		var held *persistence.LockHeldError
		if !errors.As(submitErr, &conflict) && !errors.As(submitErr, &held) {
			t.Fatalf("concurrent submit %d error = %v", index, submitErr)
		}
		if _, retryErr := fixture.Engine.Submit(events[index], fingerprint); retryErr != nil {
			t.Fatalf("retry concurrent submit %d: %v", index, retryErr)
		}
	}
	if successes == 0 {
		t.Fatal("both concurrent submissions failed")
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load converged state: %v", err)
	}
	if final.Revision != baseline.Revision+2 || len(final.State.Events) != 2 || len(final.State.PendingAsks) != 2 {
		t.Fatalf("converged state = revision %d events=%d asks=%d", final.Revision, len(final.State.Events), len(final.State.PendingAsks))
	}
	for _, id := range []string{"qa-concurrent-first", "qa-concurrent-second"} {
		if _, ok := final.State.Events[id]; !ok {
			t.Fatalf("event %s was lost", id)
		}
		if _, ok := final.State.PendingAsks[id]; !ok {
			t.Fatalf("Ask %s was lost", id)
		}
	}
}

func TestQAPersistenceProtocolResultBeforeReceiptSurvivesRestartAndSettlesOnce(t *testing.T) {
	root := t.TempDir()
	fixture, err := testkit.NewProtocolFixture(root)
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	actionID := issued[0].ActionID
	staged, err := fixture.Worker.ResultBeforeReceipt(actionID, protocol.OutcomePass, "sha256:restart-result", "")
	if err != nil || staged.Status != "STAGED" {
		t.Fatalf("result-before-receipt = %+v err=%v", staged, err)
	}
	restarted, report, err := fixture.Restart()
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if report.Outcome != persistence.RecoveryClean {
		t.Fatalf("restart recovery = %+v, want clean", report)
	}
	afterRestart, err := restarted.Engine.Load()
	if err != nil {
		t.Fatalf("Load after restart: %v", err)
	}
	if _, ok := afterRestart.State.StagedResults[actionID]; !ok {
		t.Fatal("restart lost staged result")
	}
	if _, ok := afterRestart.State.Results[actionID]; ok {
		t.Fatal("staged result settled before spawn receipt")
	}
	spawned, err := restarted.Host.Spawn(actionID, "qa-restarted-agent", protocol.SpawnStatusSpawned)
	if err != nil || spawned.Acceptance.Status != "ACCEPTED" {
		t.Fatalf("spawn after restart = %+v err=%v", spawned, err)
	}
	settled, err := restarted.Engine.Load()
	if err != nil {
		t.Fatalf("Load settled state: %v", err)
	}
	result, ok := settled.State.Results[actionID]
	if !ok || result.PayloadDigest != "sha256:restart-result" {
		t.Fatalf("settled result = %+v present=%v", result, ok)
	}
	if _, ok := settled.State.StagedResults[actionID]; ok {
		t.Fatal("settled result remained staged")
	}
	if _, ok := settled.State.PendingActions[actionID]; ok {
		t.Fatal("settled action remained pending")
	}
	duplicate, err := protocol.NewSpawnReceiptEvent("qa-restart-spawn-duplicate", actionID, "fake-host", "qa-restarted-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("duplicate spawn receipt: %v", err)
	}
	fingerprint, err := restarted.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint duplicate: %v", err)
	}
	acceptance, err := restarted.Engine.Submit(duplicate, fingerprint)
	if err != nil || acceptance.Status != "DUPLICATE" || acceptance.Revision != settled.Revision+1 {
		t.Fatalf("duplicate spawn receipt = %+v err=%v", acceptance, err)
	}
}

func TestQAPersistenceProtocolHostActionUnknownReconcilesWithoutReexecution(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	if _, err := fixture.PrepareReady(); err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	fixture.Faults.Arm(persistence.FaultHostCommitAfter)
	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "qa-unknown"})
	if !errors.Is(err, testkit.ErrInjected) || intent.ActionID == "" {
		t.Fatalf("host execute response loss = intent=%+v err=%v", intent, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("host side-effect calls = %d, want 1", fixture.Host.ActionCalls(intent.ActionID))
	}
	state, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after response loss: %v", err)
	}
	if _, ok := state.State.PendingHostActions[intent.ActionID]; !ok {
		t.Fatal("response loss left no durable HostAction intent")
	}
	unknown, err := fixture.Host.Receipt(intent, protocol.HostActionStatusUnknown)
	if err != nil || unknown.RecoveryAction != string(protocol.RecoveryReconcile) {
		t.Fatalf("UNKNOWN receipt = %+v err=%v", unknown, err)
	}
	for _, tc := range []struct {
		digest    string
		fulfilled bool
		conflict  bool
		want      protocol.RecoveryAction
	}{
		{"sha256:not-yet", false, false, protocol.RecoveryWait},
		{"sha256/conflict", false, true, protocol.RecoveryOperator},
	} {
		plan, err := fixture.Host.Reconcile(intent.ActionID, tc.digest, tc.fulfilled, tc.conflict)
		if err != nil || plan.Action != tc.want {
			t.Fatalf("reconcile %s = %+v err=%v, want %s", tc.digest, plan, err, tc.want)
		}
	}
	fulfilled, err := fixture.Host.Reconcile(intent.ActionID, "sha256:fulfilled", true, false)
	if err != nil || fulfilled.Action != protocol.RecoveryReconcile {
		t.Fatalf("fulfilled reconciliation = %+v err=%v", fulfilled, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("reconciliation re-executed side effect: calls=%d", fixture.Host.ActionCalls(intent.ActionID))
	}
	settled, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load reconciled state: %v", err)
	}
	if _, ok := settled.State.PendingHostActions[intent.ActionID]; ok {
		t.Fatal("fulfilled reconciliation left intent pending")
	}
	effect, ok := settled.State.ReconciledEffects[intent.ActionID]
	if !ok || effect.Status != "FULFILLED" || effect.ObservationDigest != "sha256:fulfilled" {
		t.Fatalf("reconciled effect = %+v present=%v", effect, ok)
	}
	if receipt := settled.State.HostActionReceipts[intent.ActionID]; receipt.Status != protocol.HostActionStatusReconciled {
		t.Fatalf("reconciled receipt = %+v", receipt)
	}
	replayed, err := fixture.Host.Reconcile(intent.ActionID, "sha256:fulfilled", true, false)
	if err != nil || replayed.Action != protocol.RecoveryReconcile {
		t.Fatalf("reconciliation replay = %+v err=%v", replayed, err)
	}
	afterReplay, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after reconciliation replay: %v", err)
	}
	if afterReplay.Revision != settled.Revision || fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("reconciliation replay changed revision/calls: %d -> %d, calls=%d", settled.Revision, afterReplay.Revision, fixture.Host.ActionCalls(intent.ActionID))
	}
}

func TestQAPersistenceProtocolReplacementAttemptQuarantinesLateResult(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil || len(issued) != 1 {
		t.Fatalf("PrepareReady: actions=%v err=%v", issued, err)
	}
	oldActionID := issued[0].ActionID
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before replacement: %v", err)
	}
	oldPending := before.State.PendingActions[oldActionID]
	oldAttempt := before.State.Attempts[oldPending.Task]
	if err := fixture.VCS.Write("bindings/changed.txt", []byte("changed")); err != nil {
		t.Fatalf("change VCS binding: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint replacement: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(oldActionID, protocol.Interruption{Class: authoring.FailureTransientEngine}, fingerprint)
	if err != nil || plan.Action != protocol.RecoveryNewAttempt {
		t.Fatalf("replacement plan = %+v err=%v", plan, err)
	}
	replaced, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load replacement: %v", err)
	}
	currentAttempt := replaced.State.Attempts[oldPending.Task]
	if currentAttempt.ID == "" || currentAttempt.ID == oldAttempt.ID || currentAttempt.ActionID == oldActionID {
		t.Fatalf("replacement Attempt = %+v, old = %+v", currentAttempt, oldAttempt)
	}
	if _, ok := replaced.State.PendingActions[oldActionID]; ok {
		t.Fatal("old action remained pending")
	}
	if _, ok := replaced.State.PendingActions[currentAttempt.ActionID]; !ok {
		t.Fatal("replacement action is not pending")
	}
	if currentAttempt.Bindings.Snapshot != fingerprint || currentAttempt.Bindings.Task != oldPending.Task || currentAttempt.Bindings.Responsibility != "fake-host" {
		t.Fatalf("replacement bindings = %+v", currentAttempt.Bindings)
	}
	obsolete, ok := replaced.State.ObsoleteActions[oldActionID]
	if !ok || obsolete.ReplacedBy != currentAttempt.ActionID || obsolete.AttemptID != oldAttempt.ID || obsolete.Bindings != oldAttempt.Bindings || obsolete.Plan != oldAttempt.Plan {
		t.Fatalf("obsolete action = %+v present=%v old=%+v", obsolete, ok, oldAttempt)
	}
	late, err := protocol.NewWorkerResultEvent("qa-late-result", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late", "")
	if err != nil {
		t.Fatalf("late result: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint late: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(late, fingerprint)
	if err != nil || acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late result acceptance = %+v err=%v", acceptance, err)
	}
	afterLate, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after late result: %v", err)
	}
	if got := afterLate.State.Attempts[oldPending.Task]; got.ID != currentAttempt.ID || got.ActionID != currentAttempt.ActionID {
		t.Fatalf("late result changed current Attempt: %+v", got)
	}
	if result, ok := afterLate.State.ObsoleteResults[oldActionID]; !ok || result.PayloadDigest != "sha256:late" {
		t.Fatalf("obsolete result = %+v present=%v", result, ok)
	}
	if _, ok := afterLate.State.Results[currentAttempt.ActionID]; ok {
		t.Fatal("late result attached to replacement action")
	}
	duplicate, err := protocol.NewWorkerResultEvent("qa-late-result-duplicate", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late", "")
	if err != nil {
		t.Fatalf("duplicate late result: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint duplicate late: %v", err)
	}
	dupAcceptance, err := fixture.Engine.Submit(duplicate, fingerprint)
	if err != nil || dupAcceptance.Status != "OBSOLETE_RESULT" || dupAcceptance.Revision != afterLate.Revision+1 {
		t.Fatalf("duplicate late result = %+v err=%v", dupAcceptance, err)
	}
}

func TestQAPersistenceProtocolHostActionSchemasFenceIntentAndReceiptWrites(t *testing.T) {
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
		{"wrong parameter type", "op.fan.transport", map[string]any{"target": 42}, protocol.CodeOperationSchemaInvalid},
	}
	for _, tc := range invalidIntents {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := fixture.Engine.ExecuteHostAction(tc.operation, tc.params, fingerprint)
			if err == nil {
				t.Fatal("invalid HostAction intent was accepted")
			}
			qaP2RejectCode(t, err, tc.code)
			qaP2Unchanged(t, baseline, fixture.Engine)
		})
	}
	intent, revision, err := fixture.Engine.ExecuteHostAction("op.fan.transport", map[string]any{"target": "qa-schema", "retries": 2}, fingerprint)
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
	if !ok || pending.Adapter == nil || pending.Adapter.Operation != intent.Adapter.Operation || pending.PayloadDigest != intent.PayloadDigest || pending.Adapter.Params["target"] != "qa-schema" || pending.Adapter.Params["retries"] != float64(2) {
		t.Fatalf("pending HostAction = %+v present=%v", pending, ok)
	}
	invalidReceipt, err := protocol.NewAdapterHostActionReceiptEvent("qa-invalid-host-receipt", intent.ActionID, intent.Adapter.Operation, "fake-host", "qa-correlation", intent.PayloadDigest, protocol.HostActionStatusExecuted, "", map[string]any{
		"identity": "qa-host", "observationDigest": "sha256:observed", "undeclared": true,
	})
	if err != nil {
		t.Fatalf("invalid receipt constructor: %v", err)
	}
	if _, err := fixture.Engine.Submit(invalidReceipt, fingerprint); err == nil {
		t.Fatal("receipt with undeclared evidence was accepted")
	} else {
		qaP2RejectCode(t, err, protocol.CodeOperationSchemaInvalid)
	}
	qaP2Unchanged(t, afterIntent, fixture.Engine)
	wantEvidence := map[string]any{"identity": "qa-receipt", "observationDigest": "sha256:receipt"}
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

func TestQAPersistenceProtocolTaskProgressMaintainsExpectedAttemptLedger(t *testing.T) {
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
	qaP2LiveLedger(t, fixture.Engine, action.Task, attempt, pending, runtime.TaskIssued, baseline.Revision)
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
		qaP2RejectCode(t, err, protocol.CodeEventNotCurrent)
	}
	qaP2Unchanged(t, baseline, fixture.Engine)
	illegal, err := protocol.NewTaskEvent("qa-task-illegal", action.Task, attempt.ID, runtime.TaskValidating)
	if err != nil {
		t.Fatalf("illegal transition event: %v", err)
	}
	if _, err := fixture.Engine.Submit(illegal, fingerprint); err == nil {
		t.Fatal("ISSUED to VALIDATING was accepted")
	} else {
		qaP2RejectCode(t, err, protocol.CodeIllegalTransition)
	}
	qaP2Unchanged(t, baseline, fixture.Engine)
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
			qaP2LiveLedger(t, fixture.Engine, action.Task, attempt, pending, status, acceptance.Revision)
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

func TestQAPersistenceProtocolTerminalSummaryIsVersionBoundAndReplayOnly(t *testing.T) {
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
	if gotEnvelope != wantEnvelope || summary.Status != "COMPLETE" || summary.Next.Kind != decision.KindComplete {
		t.Fatalf("terminal envelope/status = %+v/%s/%s", gotEnvelope, summary.Status, summary.Next.Kind)
	}
	request, err := protocol.NewRequestEvent("terminal-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("terminal request: %v", err)
	}
	requestDigest, err := request.Digest()
	if err != nil {
		t.Fatalf("request digest: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent("terminal-decision", "terminal-request", summary.LastRequestReceipt.FreshnessToken, "confirm")
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
		qaP2RejectCode(t, err, protocol.CodeDuplicateEventMismatch)
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

func TestQAPersistenceProtocolRawDiagnoseIsReadOnlyForUnsupportedInputs(t *testing.T) {
	supported := validate.VersionEnvelope{
		Writer:                    persistence.Writer,
		StateSchemaVersion:        encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion,
		DefinitionSource:          validate.CurrentWorkflowDefinitionSource,
		DefinitionDigest:          definition.WorkflowDefinitionDigest,
		PackageDigest:             testkit.HarnessPackageDigest,
	}
	cases := []struct {
		name    string
		content string
		check   func(*testing.T, validate.DiagnoseReport)
	}{
		{
			name:    "legacy readable summary",
			content: "{\n  \"status\": \"SEALED\",\n  \"runId\": \"legacy-run\"\n}\n",
			check: func(t *testing.T, report validate.DiagnoseReport) {
				if !report.JSONReadable || report.Integrity != "unsupported" || report.Summary["status"] != "SEALED" || report.Summary["runId"] != "legacy-run" || len(report.DetectedVersions) != 0 || report.Recommendation != validate.UnsupportedRunVersionCode+": state has no version envelope; rebuild it with the owning writer" {
					t.Fatalf("legacy report = %+v", report)
				}
			},
		},
		{
			name: "mismatched engine envelope",
			content: "{\n" +
				"  \"writer\": \"engine\",\n" +
				"  \"stateSchemaVersion\": \"1\",\n" +
				"  \"workflowDefinitionVersion\": \"old\",\n" +
				"  \"definitionSource\": \"definitions/workflow.json\",\n" +
				"  \"definitionDigest\": \"sha256:old\",\n" +
				"  \"packageDigest\": \"sha256:old\",\n" +
				"  \"status\": \"ACTIVE\"\n" +
				"}\n",
			check: func(t *testing.T, report validate.DiagnoseReport) {
				wantDetected := map[string]any{"writer": "engine", "stateSchemaVersion": "1", "workflowDefinitionVersion": "old", "definitionSource": "definitions/workflow.json", "definitionDigest": "sha256:old", "packageDigest": "sha256:old"}
				wantRecommendation := (&persistence.UnsupportedRunVersionError{Field: "workflowDefinitionVersion", Expected: supported.WorkflowDefinitionVersion, Observed: "old"}).Error() + "; rebuild it with the owning writer"
				if !report.JSONReadable || report.Integrity != "readable" || !reflect.DeepEqual(report.DetectedVersions, wantDetected) || report.Summary["status"] != "ACTIVE" || report.Recommendation != wantRecommendation {
					t.Fatalf("mismatched report = %+v", report)
				}
			},
		},
		{
			name:    "malformed JSON",
			content: "{\"writer\":\"engine\"",
			check: func(t *testing.T, report validate.DiagnoseReport) {
				if report.JSONReadable || report.Integrity != "unknown" || report.DetectedVersions != nil || report.Summary != nil || report.Recommendation != "rebuild the state with the owning writer" {
					t.Fatalf("malformed report = %+v", report)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o640); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			beforeInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat fixture: %v", err)
			}
			report, err := validate.DiagnoseStateWithSupported(path, supported)
			if err != nil {
				t.Fatalf("DiagnoseStateWithSupported: %v", err)
			}
			if report.Path != filepath.Clean(path) || report.Supported != supported {
				t.Fatalf("diagnose identity = %q / %+v", report.Path, report.Supported)
			}
			tc.check(t, report)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read diagnosed fixture: %v", err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat diagnosed fixture: %v", err)
			}
			if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() || beforeInfo.Size() != afterInfo.Size() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatalf("raw diagnose changed input: before=%+v after=%+v", beforeInfo, afterInfo)
			}
			if !reflect.DeepEqual(qaP2ArtifactNames(t, root), []string{"state.json"}) {
				t.Fatalf("raw diagnose created artifacts: %v", qaP2ArtifactNames(t, root))
			}
		})
	}
}
