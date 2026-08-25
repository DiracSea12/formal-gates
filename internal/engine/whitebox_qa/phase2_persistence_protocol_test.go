package whitebox_qa

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
)

const (
	phase2PackageDigest = "sha256:phase2-whitebox-package"
	phase2Fingerprint   = "sha256:phase2-whitebox-facts"
	phase2Provider      = "phase2-whitebox-host"
)

func phase2Store(t *testing.T, dir string, injector func(persistence.FaultPoint) error) *persistence.Store {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{
		PackageDigest: phase2PackageDigest,
		FaultInjector: injector,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func phase2Save(t *testing.T, store *persistence.Store, revision uint64, content any, collect func() (string, error)) persistence.SaveResult {
	t.Helper()
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision:    revision,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint:  collect,
		Content:             content,
	})
	if err != nil {
		t.Fatalf("Save revision %d: %v", revision, err)
	}
	return result
}

func phase2Engine(t *testing.T, collect func() (string, error)) *protocol.Engine {
	t.Helper()
	store := phase2Store(t, t.TempDir(), nil)
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
	if err := engine.Init(view, phase2Provider, phase2Fingerprint); err != nil {
		t.Fatalf("Engine.Init: %v", err)
	}
	return engine
}

func phase2Submit(t *testing.T, engine *protocol.Engine, event protocol.Event) protocol.Acceptance {
	t.Helper()
	acceptance, err := engine.Submit(event, phase2Fingerprint)
	if err != nil {
		t.Fatalf("Submit %s: %v", event.ID, err)
	}
	return acceptance
}

func phase2LifecycleEvent(t *testing.T, id protocol.EventID, identity, name string) protocol.Event {
	t.Helper()
	event, err := protocol.NewLifecycleEventEvent(id, "fake-host", identity, name)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	return event
}

func phase2RejectionCode(t *testing.T, err error) string {
	t.Helper()
	var rejected *protocol.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error %v is not a protocol rejection", err)
	}
	return rejected.Code
}

// Every envelope identity field is a pre-write fence. Missing and mismatched
// values must be classified before fingerprint collection or protocol writes.
func TestPhase2WhiteboxEnvelopeIdentityRejectsEveryFieldBeforeWrite(t *testing.T) {
	seedDir := t.TempDir()
	seedStore := phase2Store(t, seedDir, nil)
	stableCollect := func() (string, error) { return phase2Fingerprint, nil }
	phase2Save(t, seedStore, 0, map[string]string{"value": "seed"}, stableCollect)
	seedBytes, err := os.ReadFile(filepath.Join(seedDir, "state.json"))
	if err != nil {
		t.Fatalf("read seed state: %v", err)
	}
	var seed map[string]any
	if err := json.Unmarshal(seedBytes, &seed); err != nil {
		t.Fatalf("decode seed state: %v", err)
	}

	cases := []struct {
		name    string
		field   string
		missing bool
		value   string
	}{
		{"missing writer", "writer", true, ""},
		{"wrong writer", "writer", false, "legacy"},
		{"missing state schema", "stateSchemaVersion", true, ""},
		{"old state schema", "stateSchemaVersion", false, "0"},
		{"missing workflow definition", "workflowDefinitionVersion", true, ""},
		{"old workflow definition", "workflowDefinitionVersion", false, "1"},
		{"missing definition digest", "definitionDigest", true, ""},
		{"wrong definition digest", "definitionDigest", false, "sha256:foreign-definition"},
		{"missing package digest", "packageDigest", true, ""},
		{"wrong package digest", "packageDigest", false, "sha256:foreign-package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			state := make(map[string]any, len(seed))
			for key, value := range seed {
				state[key] = value
			}
			if tc.missing {
				delete(state, tc.field)
			} else {
				state[tc.field] = tc.value
			}
			invalidBytes, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				t.Fatalf("encode invalid state: %v", err)
			}
			statePath := filepath.Join(dir, "state.json")
			if err := os.WriteFile(statePath, append(invalidBytes, '\n'), 0o600); err != nil {
				t.Fatalf("write invalid state: %v", err)
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read invalid state before Save: %v", err)
			}
			store := phase2Store(t, dir, nil)
			collectCalls := 0
			_, err = store.Save(persistence.Transaction{
				ExpectedRevision:    1,
				ExpectedFingerprint: phase2Fingerprint,
				CollectFingerprint: func() (string, error) {
					collectCalls++
					return phase2Fingerprint, nil
				},
				Content: map[string]string{"value": "must-not-write"},
			})
			var unsupported *persistence.UnsupportedRunVersionError
			if !errors.As(err, &unsupported) || unsupported.Field != tc.field {
				t.Fatalf("Save error = %v, want unsupported %s", err, tc.field)
			}
			if collectCalls != 0 {
				t.Fatalf("invalid envelope collected external facts %d times", collectCalls)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read invalid state after Save: %v", err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("invalid envelope was rewritten")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read invalid state directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "state.json" {
				t.Fatalf("invalid envelope left protocol artifacts: %v", entries)
			}
		})
	}
}

// Each named crash window must recover to one whole revision: either the
// previous committed state or the complete next state, never a partial mix.
func TestPhase2WhiteboxEveryDurableCrashWindowConverges(t *testing.T) {
	cases := []struct {
		point          persistence.FaultPoint
		outcome        persistence.RecoveryOutcome
		revision       uint64
		committedValue string
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
			stableCollect := func() (string, error) { return phase2Fingerprint, nil }
			phase2Save(t, phase2Store(t, dir, nil), 0, map[string]string{"value": "baseline"}, stableCollect)
			faultedStore := phase2Store(t, dir, func(point persistence.FaultPoint) error {
				if point == tc.point {
					return &persistence.InjectedCrashError{Point: point}
				}
				return nil
			})
			_, err := faultedStore.Save(persistence.Transaction{
				ExpectedRevision:    1,
				ExpectedFingerprint: phase2Fingerprint,
				CollectFingerprint:  stableCollect,
				Content:             map[string]string{"value": "candidate"},
			})
			if !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("Save error = %v, want injected crash", err)
			}
			cleanStore := phase2Store(t, dir, nil)
			report, err := cleanStore.Recover()
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if report.Outcome != tc.outcome || report.Revision != tc.revision {
				t.Fatalf("recovery report = %+v, want outcome=%s revision=%d", report, tc.outcome, tc.revision)
			}
			snapshot, err := cleanStore.Load()
			if err != nil {
				t.Fatalf("Load after recovery: %v", err)
			}
			var content map[string]string
			if err := json.Unmarshal(snapshot.Content, &content); err != nil {
				t.Fatalf("decode recovered content: %v", err)
			}
			if snapshot.Revision != tc.revision || content["value"] != tc.committedValue {
				t.Fatalf("recovered snapshot = revision %d content %v", snapshot.Revision, content)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read recovered directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "state.json" {
				t.Fatalf("recovery left protocol artifacts: %v", entries)
			}
		})
	}
}

// Failure classification is a closed routing table. Engine and invariant
// failures may not drift onto the agent edge; only the explicit agent-
// recoverable class permits that route.
func TestPhase2WhiteboxFailureRoutingIsClosedAndNonAgentByDefault(t *testing.T) {
	cases := []struct {
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
	for _, tc := range cases {
		route := protocol.RouteFailure(tc.class)
		if route.Class != tc.class || route.Action != tc.action {
			t.Fatalf("RouteFailure(%s) = %+v, want %s", tc.class, route, tc.action)
		}
		if route.Action == protocol.RecoveryAgent && tc.class != authoring.FailureAgentRecoverable {
			t.Fatalf("failure class %s dynamically downgraded to agent", tc.class)
		}
	}

	if _, err := protocol.NewWorkerResultEvent("phase2-missing-failure-class", "action", phase2Provider, protocol.OutcomeFail, "sha256:result", ""); err == nil {
		t.Fatal("failed worker result without explicit failure class was accepted")
	}

	recoveryCases := []struct {
		name         string
		interruption protocol.Interruption
		action       protocol.RecoveryAction
	}{
		{"stable transient", protocol.Interruption{Class: authoring.FailureTransientEngine}, protocol.RecoveryResumeAttempt},
		{"known transient", protocol.Interruption{Class: authoring.FailureTransientEngine, CauseKnown: true}, protocol.RecoveryNewAttempt},
		{"unknown stable cause", protocol.Interruption{}, protocol.RecoveryAsk},
		{"unknown receipt one match", protocol.Interruption{ReceiptUnknown: true, LifecycleMatches: 1}, protocol.RecoveryAttachReceipt},
		{"unknown receipt zero matches", protocol.Interruption{ReceiptUnknown: true}, protocol.RecoveryOperator},
		{"unknown receipt multiple matches", protocol.Interruption{ReceiptUnknown: true, LifecycleMatches: 2}, protocol.RecoveryOperator},
	}
	for _, tc := range recoveryCases {
		t.Run(tc.name, func(t *testing.T) {
			plan := protocol.DecideRecovery(tc.interruption)
			if plan.Action != tc.action {
				t.Fatalf("DecideRecovery(%+v) = %+v, want %s", tc.interruption, plan, tc.action)
			}
			if plan.Action == protocol.RecoveryAgent {
				t.Fatalf("interruption %+v dynamically downgraded to agent", tc.interruption)
			}
		})
	}
}

// Provider and Attempt bindings fence every unified admission family before
// ledger mutation. Rejections across receipt, result, task, lifecycle, and
// HostAction paths must leave the same revision and state projection.
func TestPhase2WhiteboxProviderAndAttemptRejectionsAreZeroWrite(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
	}
	action := issued[0]
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint host action: %v", err)
	}
	intent, _, err := fixture.Engine.ExecuteHostAction(
		"op.fan.transport", map[string]any{"target": "phase2"}, fingerprint)
	if err != nil {
		t.Fatalf("ExecuteHostAction: %v", err)
	}
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before rejected events: %v", err)
	}

	spawn, err := protocol.NewSpawnReceiptEvent(
		"phase2-wrong-provider-spawn", action.ActionID, "wrong-provider", "agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent: %v", err)
	}
	result, err := protocol.NewWorkerResultEvent(
		"phase2-wrong-provider-result", action.ActionID, "wrong-provider", protocol.OutcomePass, "sha256:result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent: %v", err)
	}
	task, err := protocol.NewTaskEvent(
		"phase2-stale-attempt-task", action.Task, "att:stale", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("NewTaskEvent: %v", err)
	}
	lifecycle, err := protocol.NewLifecycleEventEvent(
		"phase2-wrong-provider-lifecycle", "wrong-provider", "agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	hostReceipt, err := protocol.NewHostActionReceiptEvent(
		"phase2-wrong-provider-host-receipt", intent.ActionID, intent.Adapter.Operation,
		"wrong-provider", "host-correlation", intent.PayloadDigest, protocol.HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("NewHostActionReceiptEvent: %v", err)
	}
	cases := []struct {
		name  string
		event protocol.Event
		code  string
	}{
		{"spawn provider", spawn, protocol.CodeProviderMismatch},
		{"result provider", result, protocol.CodeProviderMismatch},
		{"task attempt", task, protocol.CodeStaleAttempt},
		{"lifecycle provider", lifecycle, protocol.CodeProviderMismatch},
		{"host receipt provider", hostReceipt, protocol.CodeProviderMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fingerprint, err := fixture.Engine.ObserveFingerprint()
			if err != nil {
				t.Fatalf("ObserveFingerprint: %v", err)
			}
			if _, err := fixture.Engine.Submit(tc.event, fingerprint); err == nil {
				t.Fatalf("event %s was accepted", tc.event.ID)
			} else if code := phase2RejectionCode(t, err); code != tc.code {
				t.Fatalf("event %s rejection = %s, want %s", tc.event.ID, code, tc.code)
			}
			after, err := fixture.Engine.Load()
			if err != nil {
				t.Fatalf("Load after rejection: %v", err)
			}
			if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
				t.Fatalf("rejected event %s changed durable state", tc.event.ID)
			}
		})
	}
}

// Constructing the phase-2 engine with an explicit isolated project root must
// leave host, resource, and stable namespaces untouched. The only durable file
// is the engine state selected by the test-only fixture.
func TestPhase2WhiteboxEngineWritesStayInsideExplicitIsolationRoot(t *testing.T) {
	project, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatalf("NewIsolatedProject: %v", err)
	}
	before, err := project.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot before engine: %v", err)
	}
	fixture, err := testkit.NewProtocolFixture(project.Root)
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
	}
	if _, err := fixture.Host.Spawn(issued[0].ActionID, "phase2-isolated-agent", protocol.SpawnStatusSpawned); err != nil {
		t.Fatalf("FakeHost.Spawn: %v", err)
	}
	if _, err := fixture.Worker.Result(issued[0].ActionID, protocol.OutcomePass, "sha256:isolated-result", ""); err != nil {
		t.Fatalf("FakeWorker.Result: %v", err)
	}
	after, err := project.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after engine: %v", err)
	}
	changes := (&testkit.FakeVCS{Root: project.Root}).Diff(before, after)
	if len(changes) != 1 || changes[0].Path != "engine-state/state.json" {
		t.Fatalf("isolated engine changes = %+v", changes)
	}
	for _, dir := range []string{
		project.HostConfig,
		project.State,
		project.Resources,
		project.StableState,
		project.StableRun,
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read namespace %s: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("isolated engine modified namespace %s: %v", dir, entries)
		}
	}
}

// A stale writer must fail its revision comparison before collecting external
// facts or creating any persistence protocol artifacts.
func TestPhase2WhiteboxCASConflictSkipsFingerprintAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	store := phase2Store(t, dir, nil)
	stableCollect := func() (string, error) { return phase2Fingerprint, nil }
	phase2Save(t, store, 0, map[string]string{"value": "committed"}, stableCollect)

	before, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read committed state: %v", err)
	}
	collectCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint: func() (string, error) {
			collectCalls++
			return phase2Fingerprint, nil
		},
		Content: map[string]string{"value": "stale-writer"},
	})
	var conflict *persistence.RevisionConflictError
	if !errors.As(err, &conflict) || conflict.Expected != 0 || conflict.Observed != 1 {
		t.Fatalf("stale Save error = %v, want revision 0 -> 1 conflict", err)
	}
	if collectCalls != 0 {
		t.Fatalf("stale Save collected external facts %d times, want 0", collectCalls)
	}
	after, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read state after conflict: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("revision conflict changed committed state bytes")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"state.json"}) {
		t.Fatalf("revision conflict left protocol artifacts: %v", names)
	}
}

// Recovery may only rename a staged document after validating the staged
// runtime envelope. A foreign staged package is residual, not a new state.
func TestPhase2WhiteboxRecoveryQuarantinesStagedEnvelopeMismatch(t *testing.T) {
	dir := t.TempDir()
	stableCollect := func() (string, error) { return phase2Fingerprint, nil }
	store := phase2Store(t, dir, nil)
	phase2Save(t, store, 0, map[string]string{"value": "baseline"}, stableCollect)

	crashing := phase2Store(t, dir, func(point persistence.FaultPoint) error {
		if point == persistence.FaultIntentAfter {
			return &persistence.InjectedCrashError{Point: point}
		}
		return nil
	})
	_, err := crashing.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint:  stableCollect,
		Content:             map[string]string{"value": "uncommitted"},
	})
	if !errors.Is(err, persistence.ErrInjectedCrash) {
		t.Fatalf("faulted Save error = %v, want injected crash", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read crashed state directory: %v", err)
	}
	stagedPath := ""
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".state.json.") &&
			strings.HasSuffix(entry.Name(), ".tmp") {
			stagedPath = filepath.Join(dir, entry.Name())
			break
		}
	}
	if stagedPath == "" {
		t.Fatal("crash after intent did not leave a staged document")
	}
	stagedBytes, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("read staged document: %v", err)
	}
	var staged map[string]any
	if err := json.Unmarshal(stagedBytes, &staged); err != nil {
		t.Fatalf("decode staged document: %v", err)
	}
	staged["packageDigest"] = "sha256:foreign-runtime"
	stagedBytes, err = json.MarshalIndent(staged, "", "  ")
	if err != nil {
		t.Fatalf("encode foreign staged document: %v", err)
	}
	if err := os.WriteFile(stagedPath, append(stagedBytes, '\n'), 0o600); err != nil {
		t.Fatalf("write foreign staged document: %v", err)
	}

	report, err := store.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Outcome != persistence.RecoveryResidual || report.Revision != 1 {
		t.Fatalf("recovery report = %+v, want residual at revision 1", report)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load recovered state: %v", err)
	}
	var content map[string]string
	if err := json.Unmarshal(snapshot.Content, &content); err != nil {
		t.Fatalf("decode recovered content: %v", err)
	}
	if snapshot.Revision != 1 || !reflect.DeepEqual(content, map[string]string{"value": "baseline"}) {
		t.Fatalf("foreign staged envelope replaced committed state: revision=%d content=%v", snapshot.Revision, content)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read recovered state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("residual recovery did not clean protocol artifacts: %v", entries)
	}
}

// Event-ledger replay is a read-only path: once an acceptance is durable, a
// changed or unavailable external fingerprint cannot turn the same event into
// a second write attempt.
func TestPhase2WhiteboxReplaySkipsFingerprintRevalidation(t *testing.T) {
	collectCalls := 0
	var collectErr error
	engine := phase2Engine(t, func() (string, error) {
		collectCalls++
		if collectErr != nil {
			return "", collectErr
		}
		return phase2Fingerprint, nil
	})
	request, err := protocol.NewRequestEvent("phase2-replay-request", protocol.ControlReset,
		protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	first := phase2Submit(t, engine, request)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before replay: %v", err)
	}
	callsBeforeReplay := collectCalls
	collectErr = errors.New("external facts unavailable")
	replayed, err := engine.Submit(request, "sha256:obsolete-caller-fingerprint")
	if err != nil {
		t.Fatalf("same-event replay revalidated external facts: %v", err)
	}
	if !reflect.DeepEqual(replayed, first) {
		t.Fatalf("replayed acceptance = %+v, want durable %+v", replayed, first)
	}
	if collectCalls != callsBeforeReplay {
		t.Fatalf("same-event replay called fingerprint collector: before=%d after=%d", callsBeforeReplay, collectCalls)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after replay: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("same-event replay changed durable state")
	}
}

// A duplicate payload under a fresh event ID must durably occupy that ID.
// This rotates revision-bound freshness, and the occupied ID can never be
// reused for different canonical bytes.
func TestPhase2WhiteboxNewEventIDDuplicatePayloadIsDurablyOccupied(t *testing.T) {
	engine := phase2Engine(t, func() (string, error) { return phase2Fingerprint, nil })
	request, err := protocol.NewRequestEvent("phase2-freshness-request", protocol.ControlAbort,
		protocol.AskOption{ID: "keep", Label: "keep"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	phase2Submit(t, engine, request)
	firstLifecycle, err := protocol.NewLifecycleEventEvent(
		"phase2-lifecycle-first", phase2Provider, "phase2-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	phase2Submit(t, engine, firstLifecycle)
	token, err := engine.Freshness("phase2-freshness-request")
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before duplicate: %v", err)
	}
	duplicate, err := protocol.NewLifecycleEventEvent(
		"phase2-lifecycle-duplicate", phase2Provider, "phase2-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent duplicate: %v", err)
	}
	duplicateAcceptance := phase2Submit(t, engine, duplicate)
	if duplicateAcceptance.Status != "DUPLICATE" || duplicateAcceptance.Revision != before.Revision+1 {
		t.Fatalf("payload duplicate acceptance = %+v", duplicateAcceptance)
	}
	afterDuplicate, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after duplicate: %v", err)
	}
	if afterDuplicate.Revision != before.Revision+1 {
		t.Fatalf("payload duplicate revision: before=%d after=%d", before.Revision, afterDuplicate.Revision)
	}
	if _, recorded := afterDuplicate.State.Events["phase2-lifecycle-duplicate"]; !recorded {
		t.Fatal("payload duplicate event ID was not durably occupied")
	}
	if len(afterDuplicate.State.Events) != len(before.State.Events)+1 {
		t.Fatalf("payload duplicate ledger size: before=%d after=%d", len(before.State.Events), len(afterDuplicate.State.Events))
	}
	conflicting, err := protocol.NewLifecycleEventEvent(
		"phase2-lifecycle-duplicate", phase2Provider, "phase2-agent", protocol.LifecycleStop)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent conflicting: %v", err)
	}
	if _, err := engine.Submit(conflicting, phase2Fingerprint); err == nil {
		t.Fatal("occupied duplicate event ID accepted different payload")
	} else if code := phase2RejectionCode(t, err); code != protocol.CodeDuplicateEventMismatch {
		t.Fatalf("occupied ID conflict code = %s", code)
	}
	decide, err := protocol.NewDecideEvent(
		"phase2-freshness-decision", "phase2-freshness-request", token, "keep")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	if _, err := engine.Submit(decide, phase2Fingerprint); err == nil {
		t.Fatal("payload duplicate did not rotate freshness")
	} else if code := phase2RejectionCode(t, err); code != protocol.CodeStaleFreshness {
		t.Fatalf("stale decision code = %s", code)
	}
	freshToken, err := engine.Freshness("phase2-freshness-request")
	if err != nil {
		t.Fatalf("Freshness after duplicate: %v", err)
	}
	freshDecide, err := protocol.NewDecideEvent(
		"phase2-freshness-decision-fresh", "phase2-freshness-request", freshToken, "keep")
	if err != nil {
		t.Fatalf("NewDecideEvent fresh: %v", err)
	}
	accepted := phase2Submit(t, engine, freshDecide)
	if accepted.Status != "ACCEPTED" {
		t.Fatalf("decision after payload duplicate = %+v", accepted)
	}
}

// UNKNOWN receipt reconciliation may attach only lifecycle evidence matching
// that receipt's correlation. An unrelated unique pair must not release a
// staged worker result or advance the workflow.
func TestPhase2WhiteboxUnknownReceiptRequiresMatchingLifecycleEvidence(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
	}
	actionID := issued[0].ActionID
	staged, err := fixture.Worker.ResultBeforeReceipt(actionID, protocol.OutcomePass, "sha256:phase2-result", "")
	if err != nil {
		t.Fatalf("result before receipt: %v", err)
	}
	if staged.Status != "STAGED" {
		t.Fatalf("result-before-receipt acceptance = %+v", staged)
	}
	unknown, err := protocol.NewSpawnReceiptEvent(
		"phase2-unknown-receipt", actionID, "fake-host", "phase2-expected-agent", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	if acceptance, err := fixture.Engine.Submit(unknown, fingerprint); err != nil {
		t.Fatalf("submit UNKNOWN receipt: %v", err)
	} else if acceptance.RecoveryAction != string(protocol.RecoveryOperator) {
		t.Fatalf("UNKNOWN receipt initial route = %+v", acceptance)
	}
	for _, lifecycle := range []struct {
		id   protocol.EventID
		name string
	}{
		{"phase2-unrelated-lifecycle-start", protocol.LifecycleStart},
		{"phase2-unrelated-lifecycle-stop", protocol.LifecycleStop},
	} {
		fingerprint, err = fixture.Engine.ObserveFingerprint()
		if err != nil {
			t.Fatalf("ObserveFingerprint unrelated lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(
			phase2LifecycleEvent(t, lifecycle.id, "phase2-unrelated-agent", lifecycle.name),
			fingerprint,
		); err != nil {
			t.Fatalf("submit unrelated lifecycle %s: %v", lifecycle.name, err)
		}
	}
	beforeReconcile, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before unrelated reconciliation: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint reconcile: %v", err)
	}
	plan, _, err := fixture.Engine.ReconcileUnknownReceipt(actionID, fingerprint)
	if err != nil {
		t.Fatalf("reconcile unrelated lifecycle evidence: %v", err)
	}
	if plan.Action != protocol.RecoveryOperator || plan.LifecycleMatches != 0 {
		t.Fatalf("unrelated lifecycle evidence attached UNKNOWN receipt: %+v", plan)
	}
	afterUnrelated, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after unrelated reconciliation: %v", err)
	}
	if _, waiting := afterUnrelated.State.StagedResults[actionID]; !waiting {
		t.Fatal("unrelated lifecycle evidence released staged result")
	}
	if _, pending := afterUnrelated.State.PendingActions[actionID]; !pending {
		t.Fatal("unrelated lifecycle evidence retired the pending action")
	}
	if afterUnrelated.Revision != beforeReconcile.Revision+1 {
		t.Fatalf("operator reconciliation revision = %d, want %d", afterUnrelated.Revision, beforeReconcile.Revision+1)
	}

	for _, lifecycle := range []struct {
		id   protocol.EventID
		name string
	}{
		{"phase2-matching-lifecycle-start", protocol.LifecycleStart},
		{"phase2-matching-lifecycle-stop", protocol.LifecycleStop},
	} {
		fingerprint, err = fixture.Engine.ObserveFingerprint()
		if err != nil {
			t.Fatalf("ObserveFingerprint matching lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(
			phase2LifecycleEvent(t, lifecycle.id, "phase2-expected-agent", lifecycle.name),
			fingerprint,
		); err != nil {
			t.Fatalf("submit matching lifecycle %s: %v", lifecycle.name, err)
		}
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint matching reconcile: %v", err)
	}
	plan, _, err = fixture.Engine.ReconcileUnknownReceipt(actionID, fingerprint)
	if err != nil {
		t.Fatalf("reconcile matching lifecycle evidence: %v", err)
	}
	if plan.Action != protocol.RecoveryAttachReceipt || plan.LifecycleMatches != 1 {
		t.Fatalf("matching lifecycle reconciliation = %+v", plan)
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after matching reconciliation: %v", err)
	}
	if _, waiting := final.State.StagedResults[actionID]; waiting {
		t.Fatal("matching lifecycle evidence did not release staged result")
	}
	if _, recorded := final.State.Results[actionID]; !recorded {
		t.Fatal("released result was not recorded")
	}
	if _, pending := final.State.PendingActions[actionID]; pending {
		t.Fatal("completed action remained pending")
	}
}

// An interruption with unchanged bindings and an unknown cause must persist a
// recovery Ask while preserving the current Attempt. The subsequent decision
// records user intent but does not silently replace that Attempt.
func TestPhase2WhiteboxRecoveryAskPreservesCurrentAttempt(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
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
	if err != nil {
		t.Fatalf("RecoverAttempt: %v", err)
	}
	if plan.Action != protocol.RecoveryAsk || plan.RequestID == "" {
		t.Fatalf("unknown-cause recovery plan = %+v", plan)
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
		t.Fatalf("recovery Ask replaced current Attempt: before=%+v after=%+v", attempt, got)
	}
	if _, stillPending := asked.State.PendingActions[actionID]; !stillPending {
		t.Fatal("recovery Ask retired current action")
	}
	if _, obsolete := asked.State.ObsoleteActions[actionID]; obsolete {
		t.Fatal("recovery Ask marked current action obsolete")
	}
	ask, ok := asked.State.PendingAsks[plan.RequestID]
	if !ok || ask.Resolved || ask.Control != protocol.ControlRecovery {
		t.Fatalf("durable recovery Ask = %+v ok=%v", ask, ok)
	}
	token, err := fixture.Engine.Freshness(protocol.RequestID(plan.RequestID))
	if err != nil {
		t.Fatalf("Freshness recovery Ask: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent(
		"phase2-recovery-decision", protocol.RequestID(plan.RequestID), token, "fresh")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint decision: %v", err)
	}
	if acceptance, err := fixture.Engine.Submit(decisionEvent, fingerprint); err != nil {
		t.Fatalf("submit recovery decision: %v", err)
	} else if acceptance.Status != "ACCEPTED" {
		t.Fatalf("recovery decision acceptance = %+v", acceptance)
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after recovery decision: %v", err)
	}
	if got := final.State.Attempts[pending.Task]; got.ID != attempt.ID || got.ActionID != attempt.ActionID {
		t.Fatalf("recovery decision silently replaced current Attempt: before=%+v after=%+v", attempt, got)
	}
	if choice := final.State.Decisions[plan.RequestID].Choice; choice != "fresh" {
		t.Fatalf("recorded recovery choice = %q, want fresh", choice)
	}
}

// A corrupt authoritative document is not a recoverable crash artifact. Load,
// Save, and Recover must all surface the integrity failure without rewriting
// the state or starting a new transaction.
func TestPhase2WhiteboxIntegrityMismatchFencesEveryStateOperation(t *testing.T) {
	dir := t.TempDir()
	store := phase2Store(t, dir, nil)
	stableCollect := func() (string, error) { return phase2Fingerprint, nil }
	phase2Save(t, store, 0, map[string]string{"value": "trusted"}, stableCollect)

	statePath := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read trusted state: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode trusted state: %v", err)
	}
	content, ok := document["content"].(map[string]any)
	if !ok {
		t.Fatalf("state content has type %T, want object", document["content"])
	}
	content["value"] = "tampered"
	corrupt, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode corrupt state: %v", err)
	}
	corrupt = append(corrupt, '\n')
	if err := os.WriteFile(statePath, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	assertIntegrity := func(operation string, err error) {
		t.Helper()
		var mismatch *persistence.IntegrityMismatchError
		if !errors.As(err, &mismatch) || mismatch.Path != statePath {
			t.Fatalf("%s error = %v, want integrity mismatch for %s", operation, err, statePath)
		}
		if !strings.HasPrefix(err.Error(), persistence.StateIntegrityCode+":") {
			t.Fatalf("%s error = %q, want code %s", operation, err, persistence.StateIntegrityCode)
		}
	}
	_, err = store.Load()
	assertIntegrity("Load", err)

	collectCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint: func() (string, error) {
			collectCalls++
			return phase2Fingerprint, nil
		},
		Content: map[string]string{"value": "replacement"},
	})
	assertIntegrity("Save", err)
	if collectCalls != 0 {
		t.Fatalf("integrity failure collected external facts %d times", collectCalls)
	}
	_, err = store.Recover()
	assertIntegrity("Recover", err)

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read corrupt state after operations: %v", err)
	}
	if !reflect.DeepEqual(after, corrupt) {
		t.Fatal("integrity failure path rewrote authoritative state")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("integrity failure left protocol artifacts: %v", entries)
	}
}

// External facts are checked on both sides of the atomic replacement. A
// pre-write change is zero-write; a post-write change reports that the whole
// new revision committed and requires re-observation rather than rollback.
func TestPhase2WhiteboxFingerprintChangesDistinguishRejectedAndCommittedWrites(t *testing.T) {
	dir := t.TempDir()
	store := phase2Store(t, dir, nil)
	stableCollect := func() (string, error) { return phase2Fingerprint, nil }
	phase2Save(t, store, 0, map[string]string{"value": "baseline"}, stableCollect)
	statePath := filepath.Join(dir, "state.json")
	baseline, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	beforeCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint: func() (string, error) {
			beforeCalls++
			return "sha256:changed-before", nil
		},
		Content: map[string]string{"value": "must-not-commit"},
	})
	var changedBefore *persistence.FingerprintChangedError
	if !errors.As(err, &changedBefore) || changedBefore.Phase != persistence.FingerprintPhaseBefore || changedBefore.Committed {
		t.Fatalf("pre-write fingerprint error = %v", err)
	}
	if beforeCalls != 1 {
		t.Fatalf("pre-write collector calls = %d, want 1", beforeCalls)
	}
	afterRejected, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after pre-write rejection: %v", err)
	}
	if !reflect.DeepEqual(afterRejected, baseline) {
		t.Fatal("pre-write fingerprint change modified state")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read state directory after pre-write rejection: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("pre-write fingerprint rejection left protocol artifacts: %v", entries)
	}

	afterCalls := 0
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: phase2Fingerprint,
		CollectFingerprint: func() (string, error) {
			afterCalls++
			if afterCalls == 1 {
				return phase2Fingerprint, nil
			}
			return "sha256:changed-after", nil
		},
		Content: map[string]string{"value": "candidate"},
	})
	var changedAfter *persistence.FingerprintChangedError
	if !errors.As(err, &changedAfter) || changedAfter.Phase != persistence.FingerprintPhaseAfter || !changedAfter.Committed {
		t.Fatalf("post-write fingerprint error = %v", err)
	}
	if result.Revision != 2 || afterCalls != 2 {
		t.Fatalf("post-write result = %+v collector calls = %d", result, afterCalls)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load committed candidate: %v", err)
	}
	var content map[string]string
	if err := json.Unmarshal(snapshot.Content, &content); err != nil {
		t.Fatalf("decode committed candidate: %v", err)
	}
	if snapshot.Revision != 2 || content["value"] != "candidate" {
		t.Fatalf("post-write state = revision %d content %v", snapshot.Revision, content)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("post-write fingerprint path left protocol artifacts: %v", entries)
	}
}

// Event identity is a hard ledger key. Reusing an ID with different canonical
// bytes must fail before fingerprint collection and leave the first durable
// acceptance untouched.
func TestPhase2WhiteboxSameEventIDDifferentDigestIsZeroWrite(t *testing.T) {
	collectCalls := 0
	engine := phase2Engine(t, func() (string, error) {
		collectCalls++
		return phase2Fingerprint, nil
	})
	first, err := protocol.NewRequestEvent("phase2-digest-key", protocol.ControlReset,
		protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("NewRequestEvent first: %v", err)
	}
	phase2Submit(t, engine, first)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before conflicting replay: %v", err)
	}
	callsBeforeConflict := collectCalls
	conflicting, err := protocol.NewRequestEvent("phase2-digest-key", protocol.ControlAbort,
		protocol.AskOption{ID: "confirm", Label: "different-payload"})
	if err != nil {
		t.Fatalf("NewRequestEvent conflicting: %v", err)
	}
	if _, err := engine.Submit(conflicting, phase2Fingerprint); err == nil {
		t.Fatal("same event ID with different digest was accepted")
	} else if code := phase2RejectionCode(t, err); code != protocol.CodeDuplicateEventMismatch {
		t.Fatalf("conflicting replay code = %s", code)
	}
	if collectCalls != callsBeforeConflict {
		t.Fatalf("conflicting replay collected external facts: before=%d after=%d", callsBeforeConflict, collectCalls)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after conflicting replay: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("different-digest replay changed durable state")
	}
}

// Freshness is revision-bound. A real typed Operator observation rotates the
// token; the stale decision is rejected without ledger mutation, while a
// decision using the re-fetched token is accepted.
func TestPhase2WhiteboxInterveningCommitInvalidatesFreshnessToken(t *testing.T) {
	engine := phase2Engine(t, func() (string, error) { return phase2Fingerprint, nil })
	request, err := protocol.NewRequestEvent("phase2-stale-request", protocol.ControlReset,
		protocol.AskOption{ID: "proceed", Label: "proceed"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	phase2Submit(t, engine, request)
	staleToken, err := engine.Freshness("phase2-stale-request")
	if err != nil {
		t.Fatalf("Freshness before observation: %v", err)
	}
	observation, err := protocol.NewOperatorObservationEvent(
		"phase2-operator-observation", "phase2-reconcile-subject",
		decision.Fact{Source: decision.SourceVCS, Key: "current", Value: "snapshot-2"})
	if err != nil {
		t.Fatalf("NewOperatorObservationEvent: %v", err)
	}
	phase2Submit(t, engine, observation)
	beforeStale, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before stale decision: %v", err)
	}
	if len(beforeStale.State.OperatorObservations) != 1 ||
		beforeStale.State.OperatorObservations[0].Subject != "phase2-reconcile-subject" {
		t.Fatalf("typed Operator observation not recorded: %+v", beforeStale.State.OperatorObservations)
	}
	staleDecision, err := protocol.NewDecideEvent(
		"phase2-stale-decision", "phase2-stale-request", staleToken, "proceed")
	if err != nil {
		t.Fatalf("NewDecideEvent stale: %v", err)
	}
	if _, err := engine.Submit(staleDecision, phase2Fingerprint); err == nil {
		t.Fatal("stale freshness token was accepted")
	} else if code := phase2RejectionCode(t, err); code != protocol.CodeStaleFreshness {
		t.Fatalf("stale decision code = %s", code)
	}
	afterStale, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after stale decision: %v", err)
	}
	if afterStale.Revision != beforeStale.Revision || !reflect.DeepEqual(afterStale.State, beforeStale.State) {
		t.Fatal("stale freshness rejection changed durable state")
	}

	currentToken, err := engine.Freshness("phase2-stale-request")
	if err != nil {
		t.Fatalf("Freshness after observation: %v", err)
	}
	if currentToken == staleToken {
		t.Fatal("intervening commit did not rotate freshness token")
	}
	freshDecision, err := protocol.NewDecideEvent(
		"phase2-fresh-decision", "phase2-stale-request", currentToken, "proceed")
	if err != nil {
		t.Fatalf("NewDecideEvent fresh: %v", err)
	}
	if acceptance := phase2Submit(t, engine, freshDecision); acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh decision acceptance = %+v", acceptance)
	}
}

// Two writers may race after reading the same revision, but the directory
// lock and CAS must prevent a lost update. Retrying only the rejected caller
// converges to two durable events and two pending Asks.
func TestPhase2WhiteboxConcurrentSubmitConvergesWithoutLostUpdate(t *testing.T) {
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
	first, err := protocol.NewRequestEvent("phase2-concurrent-first", protocol.ControlReset,
		protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("NewRequestEvent first: %v", err)
	}
	second, err := protocol.NewRequestEvent("phase2-concurrent-second", protocol.ControlAbort,
		protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("NewRequestEvent second: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}
	events := []protocol.Event{first, second}
	results := testkit.SubmitConcurrently(
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(first, fingerprint) },
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(second, fingerprint) },
	)
	initialSuccesses := 0
	for index, submitErr := range results {
		if submitErr == nil {
			initialSuccesses++
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
	if initialSuccesses == 0 {
		t.Fatal("both concurrent writers failed before committing")
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load converged state: %v", err)
	}
	if final.Revision != baseline.Revision+2 || len(final.State.Events) != 2 || len(final.State.PendingAsks) != 2 {
		t.Fatalf("converged state = revision %d events %d asks %d", final.Revision, len(final.State.Events), len(final.State.PendingAsks))
	}
	for _, id := range []string{"phase2-concurrent-first", "phase2-concurrent-second"} {
		if _, recorded := final.State.Events[id]; !recorded {
			t.Fatalf("concurrent event %s was lost", id)
		}
		if _, pending := final.State.PendingAsks[id]; !pending {
			t.Fatalf("concurrent request %s has no pending Ask", id)
		}
	}
}

// A worker result that arrives before its SpawnReceipt must survive an engine
// restart. The later SPAWNED receipt settles the staged result exactly once.
func TestPhase2WhiteboxResultBeforeReceiptSurvivesRestartAndSettlesOnce(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
	}
	actionID := issued[0].ActionID
	staged, err := fixture.Worker.ResultBeforeReceipt(
		actionID, protocol.OutcomePass, "sha256:phase2-restart-result", "")
	if err != nil {
		t.Fatalf("ResultBeforeReceipt: %v", err)
	}
	if staged.Status != "STAGED" {
		t.Fatalf("result-before-receipt acceptance = %+v", staged)
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
	if _, waiting := afterRestart.State.StagedResults[actionID]; !waiting {
		t.Fatal("restart lost staged worker result")
	}
	if _, completed := afterRestart.State.Results[actionID]; completed {
		t.Fatal("result completed without a SpawnReceipt")
	}
	spawned, err := restarted.Host.Spawn(actionID, "phase2-restarted-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("Spawn receipt after restart: %v", err)
	}
	if spawned.Acceptance.Status != "ACCEPTED" {
		t.Fatalf("Spawn receipt acceptance = %+v", spawned.Acceptance)
	}
	settled, err := restarted.Engine.Load()
	if err != nil {
		t.Fatalf("Load settled state: %v", err)
	}
	if _, waiting := settled.State.StagedResults[actionID]; waiting {
		t.Fatal("SPAWNED receipt did not clear staged result")
	}
	result, completed := settled.State.Results[actionID]
	if !completed || result.PayloadDigest != "sha256:phase2-restart-result" {
		t.Fatalf("settled result = %+v completed=%v", result, completed)
	}
	if _, pending := settled.State.PendingActions[actionID]; pending {
		t.Fatal("settled action remained pending")
	}

	duplicate, err := protocol.NewSpawnReceiptEvent(
		"phase2-restarted-spawn-duplicate", actionID, "fake-host", "phase2-restarted-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent duplicate: %v", err)
	}
	fingerprint, err := restarted.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint duplicate: %v", err)
	}
	beforeDuplicate := settled.Revision
	acceptance, err := restarted.Engine.Submit(duplicate, fingerprint)
	if err != nil {
		t.Fatalf("submit duplicate SpawnReceipt: %v", err)
	}
	if acceptance.Status != "DUPLICATE" || acceptance.Revision != beforeDuplicate+1 {
		t.Fatalf("duplicate SpawnReceipt acceptance = %+v", acceptance)
	}
}

// HostAction side effects are preceded by a durable intent. An UNKNOWN
// receipt may wait, enter Operator, or reconcile as fulfilled, but none of
// those paths may execute the side effect a second time.
func TestPhase2WhiteboxHostActionUnknownReconcilesWithoutReexecution(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	if _, err := fixture.PrepareReady(); err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	fixture.Faults.Arm(persistence.FaultHostCommitAfter)
	intent, err := fixture.Host.Execute(
		"op.fan.transport", map[string]any{"target": "phase2-host-action"})
	if !errors.Is(err, testkit.ErrInjected) || intent.ActionID == "" {
		t.Fatalf("Host.Execute response loss intent=%+v err=%v", intent, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("host side-effect calls = %d, want 1", fixture.Host.ActionCalls(intent.ActionID))
	}
	afterExecute, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after response loss: %v", err)
	}
	if _, pending := afterExecute.State.PendingHostActions[intent.ActionID]; !pending {
		t.Fatal("host side effect ran without a durable pending intent")
	}
	unknown, err := fixture.Host.Receipt(intent, protocol.HostActionStatusUnknown)
	if err != nil {
		t.Fatalf("UNKNOWN HostAction receipt: %v", err)
	}
	if unknown.RecoveryAction != string(protocol.RecoveryReconcile) {
		t.Fatalf("UNKNOWN receipt acceptance = %+v", unknown)
	}

	waitPlan, err := fixture.Host.Reconcile(intent.ActionID, "sha256:not-yet", false, false)
	if err != nil || waitPlan.Action != protocol.RecoveryWait {
		t.Fatalf("unfulfilled reconciliation = %+v err=%v", waitPlan, err)
	}
	operatorPlan, err := fixture.Host.Reconcile(intent.ActionID, "sha256:conflict", false, true)
	if err != nil || operatorPlan.Action != protocol.RecoveryOperator {
		t.Fatalf("conflicting reconciliation = %+v err=%v", operatorPlan, err)
	}
	fulfilledPlan, err := fixture.Host.Reconcile(intent.ActionID, "sha256:fulfilled", true, false)
	if err != nil || fulfilledPlan.Action != protocol.RecoveryReconcile {
		t.Fatalf("fulfilled reconciliation = %+v err=%v", fulfilledPlan, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("reconciliation repeated host side effect %d times", fixture.Host.ActionCalls(intent.ActionID))
	}
	settled, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load reconciled state: %v", err)
	}
	if _, pending := settled.State.PendingHostActions[intent.ActionID]; pending {
		t.Fatal("fulfilled reconciliation left HostAction pending")
	}
	effect, reconciled := settled.State.ReconciledEffects[intent.ActionID]
	if !reconciled || effect.Status != "FULFILLED" || effect.ObservationDigest != "sha256:fulfilled" {
		t.Fatalf("reconciled effect = %+v reconciled=%v", effect, reconciled)
	}
	if receipt := settled.State.HostActionReceipts[intent.ActionID]; receipt.Status != "RECONCILED" {
		t.Fatalf("reconciled HostAction receipt = %+v", receipt)
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
		t.Fatalf("reconciliation replay changed revision/calls: revision %d -> %d, calls=%d",
			settled.Revision, afterReplay.Revision, fixture.Host.ActionCalls(intent.ActionID))
	}
}

// Binding changes retire the old execution instance before installing a new
// Attempt. A late result remains visible as OBSOLETE_RESULT and cannot advance
// or replace the current Attempt.
func TestPhase2WhiteboxReplacementAttemptQuarantinesLateResult(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("NewProtocolFixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("PrepareReady: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %d, want 1", len(issued))
	}
	oldActionID := issued[0].ActionID
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before replacement: %v", err)
	}
	oldPending := before.State.PendingActions[oldActionID]
	oldAttempt := before.State.Attempts[oldPending.Task]
	if err := fixture.VCS.Write("bindings/snapshot.txt", []byte("changed")); err != nil {
		t.Fatalf("change fake VCS snapshot: %v", err)
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint recovery: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(oldActionID, protocol.Interruption{
		Class: authoring.FailureTransientEngine,
	}, fingerprint)
	if err != nil {
		t.Fatalf("RecoverAttempt: %v", err)
	}
	if plan.Action != protocol.RecoveryNewAttempt {
		t.Fatalf("binding-change recovery plan = %+v", plan)
	}
	replaced, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load replacement Attempt: %v", err)
	}
	currentAttempt := replaced.State.Attempts[oldPending.Task]
	if currentAttempt.ID == "" || currentAttempt.ID == oldAttempt.ID || currentAttempt.ActionID == oldActionID {
		t.Fatalf("replacement Attempt = %+v, old = %+v", currentAttempt, oldAttempt)
	}
	if _, pending := replaced.State.PendingActions[oldActionID]; pending {
		t.Fatal("old action remained pending after replacement")
	}
	if _, pending := replaced.State.PendingActions[currentAttempt.ActionID]; !pending {
		t.Fatal("replacement action is not pending")
	}
	if currentAttempt.Bindings.Snapshot != fingerprint || currentAttempt.Bindings.Task != oldPending.Task || currentAttempt.Bindings.Responsibility != "fake-host" {
		t.Fatalf("replacement bindings = %+v", currentAttempt.Bindings)
	}
	obsolete, retired := replaced.State.ObsoleteActions[oldActionID]
	if !retired || obsolete.ReplacedBy != currentAttempt.ActionID || obsolete.AttemptID != oldAttempt.ID {
		t.Fatalf("obsolete action = %+v retired=%v", obsolete, retired)
	}
	if obsolete.Bindings != oldAttempt.Bindings || obsolete.Plan != oldAttempt.Plan {
		t.Fatalf("obsolete Attempt lost identity: obsolete=%+v old=%+v", obsolete, oldAttempt)
	}

	late, err := protocol.NewWorkerResultEvent(
		"phase2-late-result", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late-result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent late: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint late result: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(late, fingerprint)
	if err != nil {
		t.Fatalf("submit late result: %v", err)
	}
	if acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late result acceptance = %+v", acceptance)
	}
	afterLate, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load after late result: %v", err)
	}
	if got := afterLate.State.Attempts[oldPending.Task]; got.ID != currentAttempt.ID || got.ActionID != currentAttempt.ActionID {
		t.Fatalf("late result replaced current Attempt: before=%+v after=%+v", currentAttempt, got)
	}
	if result, recorded := afterLate.State.ObsoleteResults[oldActionID]; !recorded || result.PayloadDigest != "sha256:late-result" {
		t.Fatalf("obsolete result = %+v recorded=%v", result, recorded)
	}
	if _, completed := afterLate.State.Results[currentAttempt.ActionID]; completed {
		t.Fatal("late result was attached to replacement action")
	}

	duplicate, err := protocol.NewWorkerResultEvent(
		"phase2-late-result-duplicate", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late-result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent duplicate: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint duplicate late result: %v", err)
	}
	duplicateAcceptance, err := fixture.Engine.Submit(duplicate, fingerprint)
	if err != nil {
		t.Fatalf("submit duplicate late result: %v", err)
	}
	if duplicateAcceptance.Status != "OBSOLETE_RESULT" || duplicateAcceptance.Revision != afterLate.Revision+1 {
		t.Fatalf("duplicate late result acceptance = %+v", duplicateAcceptance)
	}
}
