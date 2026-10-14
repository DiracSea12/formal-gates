package contracttests

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
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	definition "formal-gates/internal/engine/testkit/workflow"
)

const (
	protocolPackageDigest = "sha256:protocol-fixture-package"
	protocolFingerprint   = "sha256:protocol-fixture-facts"
	protocolProvider      = "protocol-fixture-host"
)

func protocolStore(t *testing.T, dir string, injector func(persistence.FaultPoint) error) *persistence.Store {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{
		PackageDigest: protocolPackageDigest,
		FaultInjector: injector,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func protocolSave(t *testing.T, store *persistence.Store, revision uint64, content any, collect func() (string, error)) persistence.SaveResult {
	t.Helper()
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision:    revision,
		ExpectedFingerprint: protocolFingerprint,
		CollectFingerprint:  collect,
		Content:             content,
	})
	if err != nil {
		t.Fatalf("Save revision %d: %v", revision, err)
	}
	return result
}

func protocolEngine(t *testing.T, collect func() (string, error)) (*protocol.Engine, protocol.Observation) {
	t.Helper()
	store := protocolStore(t, t.TempDir(), nil)
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	engine, err := protocol.New(store, protocol.Config{InternalSteps: testkit.InternalSteps(),
		Definition: compiled,
		Registry:   definition.Registry(),
		Capacity:   2,
	})
	if err != nil {
		t.Fatalf("protocol.New: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("decision.NewState: %v", err)
	}
	input := protocolObservation(collect)
	if err := engine.InitWithMetadata(view, protocolProvider, input, "", "", nil, "", "", nil); err != nil {
		t.Fatalf("Engine.InitWithMetadata: %v", err)
	}
	return engine, input
}

func protocolSubmit(t *testing.T, engine *protocol.Engine, input protocol.Observation, event protocol.Event) protocol.Acceptance {
	t.Helper()
	acceptance, err := engine.Submit(event, input)
	if err != nil {
		t.Fatalf("Submit %s: %v", event.ID, err)
	}
	return acceptance
}

func protocolLifecycleEvent(t *testing.T, id protocol.EventID, identity, name string) protocol.Event {
	t.Helper()
	event, err := protocol.NewLifecycleEventEvent(id, "fake-host", identity, name)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	return event
}

func protocolRejectionCode(t *testing.T, err error) string {
	t.Helper()
	var rejected *protocol.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error %v is not a protocol rejection", err)
	}
	return rejected.Code
}

// The five base envelope identity fields are pre-write fences. Missing and
// mismatched values must be classified before fingerprint collection or protocol writes.
func TestEngineBaseEnvelopeIdentityRejectsEachFieldBeforeWrite(t *testing.T) {
	seedDir := t.TempDir()
	seedStore := protocolStore(t, seedDir, nil)
	stableCollect := func() (string, error) { return protocolFingerprint, nil }
	protocolSave(t, seedStore, 0, map[string]string{"value": "seed"}, stableCollect)
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
		{"zero workflow definition", "workflowDefinitionVersion", false, "0"},
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
			store := protocolStore(t, dir, nil)
			collectCalls := 0
			_, err = store.Save(persistence.Transaction{
				ExpectedRevision:    1,
				ExpectedFingerprint: protocolFingerprint,
				CollectFingerprint: func() (string, error) {
					collectCalls++
					return protocolFingerprint, nil
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
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			// 被拒路径不落协议字节；目录里只有 state.json 与持久的锁工件。
			if !reflect.DeepEqual(names, []string{"state.json", "write.lock"}) {
				t.Fatalf("invalid envelope left protocol artifacts: %v", names)
			}
		})
	}
}

// Each named crash window must recover to one whole revision: either the
// previous committed state or the complete next state, never a partial mix.
func TestEngineEveryDurableCrashWindowConverges(t *testing.T) {
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
			stableCollect := func() (string, error) { return protocolFingerprint, nil }
			protocolSave(t, protocolStore(t, dir, nil), 0, map[string]string{"value": "baseline"}, stableCollect)
			faultedStore := protocolStore(t, dir, func(point persistence.FaultPoint) error {
				if point == tc.point {
					return &persistence.InjectedCrashError{Point: point}
				}
				return nil
			})
			_, err := faultedStore.Save(persistence.Transaction{
				ExpectedRevision:    1,
				ExpectedFingerprint: protocolFingerprint,
				CollectFingerprint:  stableCollect,
				Content:             map[string]string{"value": "candidate"},
			})
			if !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("Save error = %v, want injected crash", err)
			}
			cleanStore := protocolStore(t, dir, nil)
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
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, persistenceFixtureStateArtifacts) {
				t.Fatalf("recovery left protocol artifacts: %v", names)
			}
		})
	}
}

// Failure classification is a closed routing table. Engine and invariant
// failures may not drift onto the agent edge; only the explicit agent-
// recoverable class permits that route.
func TestEngineFailureRoutingIsClosedAndNonAgentByDefault(t *testing.T) {
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

	if _, err := protocol.NewWorkerResultEvent("protocol-missing-failure-class", "action", protocolProvider, protocol.OutcomeFail, "sha256:result", ""); err == nil {
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
// ledger mutation. Rejections across receipt, result, task, and lifecycle
// paths must leave the same revision and state projection.
func TestEngineProviderAndAttemptRejectionsAreZeroWrite(t *testing.T) {
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
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before rejected events: %v", err)
	}

	spawn, err := protocol.NewSpawnReceiptEvent(
		"protocol-wrong-provider-spawn", action.ActionID, "wrong-provider", "agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent: %v", err)
	}
	result, err := protocol.NewWorkerResultEvent(
		"protocol-wrong-provider-result", action.ActionID, "wrong-provider", protocol.OutcomePass, "sha256:result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent: %v", err)
	}
	task, err := protocol.NewTaskEvent(
		"protocol-stale-attempt-task", action.Task, "att:stale", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("NewTaskEvent: %v", err)
	}
	lifecycle, err := protocol.NewLifecycleEventEvent(
		"protocol-wrong-provider-lifecycle", "wrong-provider", "agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observedInput, err := fixture.VCS.Observe()
			if err != nil {
				t.Fatalf("Fingerprint: %v", err)
			}
			if _, err := fixture.Engine.Submit(tc.event, observedInput); err == nil {
				t.Fatalf("event %s was accepted", tc.event.ID)
			} else if code := protocolRejectionCode(t, err); code != tc.code {
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

// Constructing the persistent engine with an explicit isolated project root must
// leave host, resource, and stable namespaces untouched. The only durable file
// is the engine state selected by the test-only fixture.
func TestEngineEngineWritesStayInsideExplicitIsolationRoot(t *testing.T) {
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
	if _, err := fixture.Host.Spawn(issued[0].ActionID, "protocol-isolated-agent", protocol.SpawnStatusSpawned); err != nil {
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
	// 引擎只允许改动隔离根内的状态与锁工件：state.json 必须写入，
	// write.lock 是持久的目录互斥工件（每次持锁刷新诊断内容）。
	allowed := map[string]bool{"engine-state/state.json": true, "engine-state/write.lock": true}
	changedPaths := map[string]bool{}
	for _, change := range changes {
		if !allowed[change.Path] {
			t.Fatalf("isolated engine changes = %+v", changes)
		}
		changedPaths[change.Path] = true
	}
	if !changedPaths["engine-state/state.json"] {
		t.Fatalf("engine state was not written inside the isolation root: %+v", changes)
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
func TestEngineCASConflictSkipsFingerprintAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	store := protocolStore(t, dir, nil)
	stableCollect := func() (string, error) { return protocolFingerprint, nil }
	protocolSave(t, store, 0, map[string]string{"value": "committed"}, stableCollect)

	before, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read committed state: %v", err)
	}
	store = protocolStore(t, dir, nil)
	collectCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: protocolFingerprint,
		CollectFingerprint: func() (string, error) {
			collectCalls++
			return protocolFingerprint, nil
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
	if !reflect.DeepEqual(names, persistenceFixtureStateArtifacts) {
		t.Fatalf("revision conflict left protocol artifacts: %v", names)
	}
}

// Recovery may only rename a staged document after validating the staged
// runtime envelope. A foreign staged package is residual, not a new state.
func TestEngineRecoveryQuarantinesStagedEnvelopeMismatch(t *testing.T) {
	dir := t.TempDir()
	stableCollect := func() (string, error) { return protocolFingerprint, nil }
	store := protocolStore(t, dir, nil)
	protocolSave(t, store, 0, map[string]string{"value": "baseline"}, stableCollect)

	crashing := protocolStore(t, dir, func(point persistence.FaultPoint) error {
		if point == persistence.FaultIntentAfter {
			return &persistence.InjectedCrashError{Point: point}
		}
		return nil
	})
	_, err := crashing.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: protocolFingerprint,
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

	store = protocolStore(t, dir, nil)
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
	if names := persistenceFixtureArtifactNames(t, dir); !reflect.DeepEqual(names, persistenceFixtureStateArtifacts) {
		t.Fatalf("residual recovery did not clean protocol artifacts: %v", names)
	}
}

// Event-ledger replay is a read-only path: once an acceptance is durable, a
// changed or unavailable external fingerprint cannot turn the same event into
// a second write attempt.
func TestEngineReplaySkipsFingerprintRevalidation(t *testing.T) {
	collectCalls := 0
	var collectErr error
	engine, input := protocolEngine(t, func() (string, error) {
		collectCalls++
		if collectErr != nil {
			return "", collectErr
		}
		return protocolFingerprint, nil
	})
	request, err := protocol.NewRequestEvent("protocol-replay-request", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	first := protocolSubmit(t, engine, input, request)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before replay: %v", err)
	}
	callsBeforeReplay := collectCalls
	collectErr = errors.New("external facts unavailable")
	replayInput := input
	replayInput.Facts = decision.Observation{Facts: []decision.Fact{{Source: decision.SourceFile, Key: "protocol-fixture", Value: "sha256:obsolete-caller-fingerprint"}}}
	replayed, err := engine.Submit(request, replayInput)
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
func TestEngineNewEventIDDuplicatePayloadIsDurablyOccupied(t *testing.T) {
	engine, input := protocolEngine(t, func() (string, error) { return protocolFingerprint, nil })
	request, err := protocol.NewRequestEvent("protocol-freshness-request", protocol.ControlAbort,
		protocol.AskOption{ID: "keep", Label: "keep"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	protocolSubmit(t, engine, input, request)
	firstLifecycle, err := protocol.NewLifecycleEventEvent(
		"protocol-lifecycle-first", protocolProvider, "protocol-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent: %v", err)
	}
	protocolSubmit(t, engine, input, firstLifecycle)
	token, err := engine.Freshness("protocol-freshness-request")
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before duplicate: %v", err)
	}
	duplicate, err := protocol.NewLifecycleEventEvent(
		"protocol-lifecycle-duplicate", protocolProvider, "protocol-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent duplicate: %v", err)
	}
	duplicateAcceptance := protocolSubmit(t, engine, input, duplicate)
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
	if _, recorded := afterDuplicate.State.Events["protocol-lifecycle-duplicate"]; !recorded {
		t.Fatal("payload duplicate event ID was not durably occupied")
	}
	if len(afterDuplicate.State.Events) != len(before.State.Events)+1 {
		t.Fatalf("payload duplicate ledger size: before=%d after=%d", len(before.State.Events), len(afterDuplicate.State.Events))
	}
	conflicting, err := protocol.NewLifecycleEventEvent(
		"protocol-lifecycle-duplicate", protocolProvider, "protocol-agent", protocol.LifecycleStop)
	if err != nil {
		t.Fatalf("NewLifecycleEventEvent conflicting: %v", err)
	}
	if _, err := engine.Submit(conflicting, input); err == nil {
		t.Fatal("occupied duplicate event ID accepted different payload")
	} else if code := protocolRejectionCode(t, err); code != protocol.CodeDuplicateEventMismatch {
		t.Fatalf("occupied ID conflict code = %s", code)
	}
	decide, err := protocol.NewDecideEvent(
		"protocol-freshness-decision", "protocol-freshness-request", token, "keep")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	if _, err := engine.Submit(decide, input); err == nil {
		t.Fatal("payload duplicate did not rotate freshness")
	} else if code := protocolRejectionCode(t, err); code != protocol.CodeStaleFreshness {
		t.Fatalf("stale decision code = %s", code)
	}
	freshToken, err := engine.Freshness("protocol-freshness-request")
	if err != nil {
		t.Fatalf("Freshness after duplicate: %v", err)
	}
	freshDecide, err := protocol.NewDecideEvent(
		"protocol-freshness-decision-fresh", "protocol-freshness-request", freshToken, "keep")
	if err != nil {
		t.Fatalf("NewDecideEvent fresh: %v", err)
	}
	accepted := protocolSubmit(t, engine, input, freshDecide)
	if accepted.Status != "ACCEPTED" {
		t.Fatalf("decision after payload duplicate = %+v", accepted)
	}
}

// UNKNOWN receipt reconciliation may attach only lifecycle evidence matching
// that receipt's correlation. An unrelated unique pair must not release a
// staged worker result or advance the workflow.
func TestEngineUnknownReceiptRequiresMatchingLifecycleEvidence(t *testing.T) {
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
	issuedState, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	staged, err := fixture.Worker.ReviewResultBeforeReceipt(actionID, issuedState.State.PendingActions[actionID].ReviewSeries, "sha256:protocol-result")
	if err != nil {
		t.Fatalf("result before receipt: %v", err)
	}
	if staged.Status != "STAGED" {
		t.Fatalf("result-before-receipt acceptance = %+v", staged)
	}
	unknown, err := protocol.NewSpawnReceiptEvent(
		"protocol-unknown-receipt", actionID, "fake-host", "protocol-expected-agent", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent: %v", err)
	}
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if acceptance, err := fixture.Engine.Submit(unknown, observedInput); err != nil {
		t.Fatalf("submit UNKNOWN receipt: %v", err)
	} else if acceptance.RecoveryAction != string(protocol.RecoveryOperator) {
		t.Fatalf("UNKNOWN receipt initial route = %+v", acceptance)
	}
	for _, lifecycle := range []struct {
		id   protocol.EventID
		name string
	}{
		{"protocol-unrelated-lifecycle-start", protocol.LifecycleStart},
		{"protocol-unrelated-lifecycle-stop", protocol.LifecycleStop},
	} {
		observedInput, err = fixture.VCS.Observe()
		if err != nil {
			t.Fatalf("Fingerprint unrelated lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(
			protocolLifecycleEvent(t, lifecycle.id, "protocol-unrelated-agent", lifecycle.name),
			observedInput,
		); err != nil {
			t.Fatalf("submit unrelated lifecycle %s: %v", lifecycle.name, err)
		}
	}
	beforeReconcile, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before unrelated reconciliation: %v", err)
	}
	observedInput, err = fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint reconcile: %v", err)
	}
	plan, _, err := fixture.Engine.ReconcileUnknownReceipt(actionID, observedInput)
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
		{"protocol-matching-lifecycle-start", protocol.LifecycleStart},
		{"protocol-matching-lifecycle-stop", protocol.LifecycleStop},
	} {
		observedInput, err = fixture.VCS.Observe()
		if err != nil {
			t.Fatalf("Fingerprint matching lifecycle: %v", err)
		}
		if _, err := fixture.Engine.Submit(
			protocolLifecycleEvent(t, lifecycle.id, "protocol-expected-agent", lifecycle.name),
			observedInput,
		); err != nil {
			t.Fatalf("submit matching lifecycle %s: %v", lifecycle.name, err)
		}
	}
	observedInput, err = fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint matching reconcile: %v", err)
	}
	plan, _, err = fixture.Engine.ReconcileUnknownReceipt(actionID, observedInput)
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
func TestEngineRecoveryAskPreservesCurrentAttempt(t *testing.T) {
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
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{}, observedInput)
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
		"protocol-recovery-decision", protocol.RequestID(plan.RequestID), token, "fresh")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	observedInput, err = fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint decision: %v", err)
	}
	if acceptance, err := fixture.Engine.Submit(decisionEvent, observedInput); err != nil {
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
func TestEngineIntegrityMismatchFencesEveryStateOperation(t *testing.T) {
	dir := t.TempDir()
	store := protocolStore(t, dir, nil)
	stableCollect := func() (string, error) { return protocolFingerprint, nil }
	protocolSave(t, store, 0, map[string]string{"value": "trusted"}, stableCollect)

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
		ExpectedFingerprint: protocolFingerprint,
		CollectFingerprint: func() (string, error) {
			collectCalls++
			return protocolFingerprint, nil
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
	if names := persistenceFixtureArtifactNames(t, dir); !reflect.DeepEqual(names, persistenceFixtureStateArtifacts) {
		t.Fatalf("integrity failure left protocol artifacts: %v", names)
	}
}

// External facts are checked on both sides of the atomic replacement. A
// pre-write change is zero-write; a post-write change reports that the whole
// new revision committed and requires re-observation rather than rollback.
func TestEngineFingerprintChangesDistinguishRejectedAndCommittedWrites(t *testing.T) {
	dir := t.TempDir()
	store := protocolStore(t, dir, nil)
	stableCollect := func() (string, error) { return protocolFingerprint, nil }
	protocolSave(t, store, 0, map[string]string{"value": "baseline"}, stableCollect)
	statePath := filepath.Join(dir, "state.json")
	baseline, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	beforeCalls := 0
	_, err = store.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: protocolFingerprint,
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
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	// 被拒路径不落协议字节；目录里只有 state.json 与持久的锁工件。
	if !reflect.DeepEqual(names, []string{"state.json", "write.lock"}) {
		t.Fatalf("pre-write fingerprint rejection left protocol artifacts: %v", names)
	}

	afterCalls := 0
	result, err := store.Save(persistence.Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: protocolFingerprint,
		CollectFingerprint: func() (string, error) {
			afterCalls++
			if afterCalls == 1 {
				return protocolFingerprint, nil
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
	if names := persistenceFixtureArtifactNames(t, dir); !reflect.DeepEqual(names, persistenceFixtureStateArtifacts) {
		t.Fatalf("post-write fingerprint path left protocol artifacts: %v", names)
	}
}

// Event identity is a hard ledger key. Reusing an ID with different canonical
// bytes must fail before fingerprint collection and leave the first durable
// acceptance untouched.
func TestEngineSameEventIDDifferentDigestIsZeroWrite(t *testing.T) {
	collectCalls := 0
	engine, input := protocolEngine(t, func() (string, error) {
		collectCalls++
		return protocolFingerprint, nil
	})
	first, err := protocol.NewRequestEvent("protocol-digest-key", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("NewRequestEvent first: %v", err)
	}
	protocolSubmit(t, engine, input, first)
	before, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before conflicting replay: %v", err)
	}
	callsBeforeConflict := collectCalls
	conflicting, err := protocol.NewRequestEvent("protocol-digest-key", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "different-payload"})
	if err != nil {
		t.Fatalf("NewRequestEvent conflicting: %v", err)
	}
	if _, err := engine.Submit(conflicting, input); err == nil {
		t.Fatal("same event ID with different digest was accepted")
	} else if code := protocolRejectionCode(t, err); code != protocol.CodeDuplicateEventMismatch {
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

// Freshness is revision-bound. A correlated lifecycle event rotates the token;
// the stale decision is rejected without ledger mutation, while a decision
// using the re-fetched token is accepted.
func TestEngineInterveningCommitInvalidatesFreshnessToken(t *testing.T) {
	engine, input := protocolEngine(t, func() (string, error) { return protocolFingerprint, nil })
	request, err := protocol.NewRequestEvent("protocol-stale-request", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	protocolSubmit(t, engine, input, request)
	staleToken, err := engine.Freshness("protocol-stale-request")
	if err != nil {
		t.Fatalf("Freshness before observation: %v", err)
	}
	observation, err := protocol.NewCorrelatedLifecycleEvent(
		"protocol-operator-observation", protocolProvider, "protocol-rotation", "protocol-identity", protocol.LifecycleStart)
	if err != nil {
		t.Fatalf("NewCorrelatedLifecycleEvent: %v", err)
	}
	protocolSubmit(t, engine, input, observation)
	beforeStale, err := engine.Load()
	if err != nil {
		t.Fatalf("Load before stale decision: %v", err)
	}
	if len(beforeStale.State.LifecycleEvents) != 1 ||
		beforeStale.State.LifecycleEvents[0].Correlation != "protocol-rotation" {
		t.Fatalf("intervening lifecycle observation not recorded: %+v", beforeStale.State.LifecycleEvents)
	}
	staleDecision, err := protocol.NewDecideEvent(
		"protocol-stale-decision", "protocol-stale-request", staleToken, "cancel")
	if err != nil {
		t.Fatalf("NewDecideEvent stale: %v", err)
	}
	if _, err := engine.Submit(staleDecision, input); err == nil {
		t.Fatal("stale freshness token was accepted")
	} else if code := protocolRejectionCode(t, err); code != protocol.CodeStaleFreshness {
		t.Fatalf("stale decision code = %s", code)
	}
	afterStale, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after stale decision: %v", err)
	}
	if afterStale.Revision != beforeStale.Revision || !reflect.DeepEqual(afterStale.State, beforeStale.State) {
		t.Fatal("stale freshness rejection changed durable state")
	}

	currentToken, err := engine.Freshness("protocol-stale-request")
	if err != nil {
		t.Fatalf("Freshness after observation: %v", err)
	}
	if currentToken == staleToken {
		t.Fatal("intervening commit did not rotate freshness token")
	}
	freshDecision, err := protocol.NewDecideEvent(
		"protocol-fresh-decision", "protocol-stale-request", currentToken, "cancel")
	if err != nil {
		t.Fatalf("NewDecideEvent fresh: %v", err)
	}
	if acceptance := protocolSubmit(t, engine, input, freshDecision); acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh decision acceptance = %+v", acceptance)
	}
}

// Two writers may race after reading the same revision, but the directory
// lock and CAS must prevent a lost update. Retrying only the rejected caller
// converges to two durable events and two pending Asks.
func TestEngineConcurrentSubmitConvergesWithoutLostUpdate(t *testing.T) {
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
	first, err := protocol.NewRequestEvent("protocol-concurrent-first", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("NewRequestEvent first: %v", err)
	}
	second, err := protocol.NewRequestEvent("protocol-concurrent-second", protocol.ControlAbort,
		protocol.AskOption{ID: "cancel", Label: "Keep running"})
	if err != nil {
		t.Fatalf("NewRequestEvent second: %v", err)
	}
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	events := []protocol.Event{first, second}
	results := testkit.SubmitConcurrently(
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(first, observedInput) },
		func() (protocol.Acceptance, error) { return fixture.Engine.Submit(second, observedInput) },
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
		if _, retryErr := fixture.Engine.Submit(events[index], observedInput); retryErr != nil {
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
	for _, id := range []string{"protocol-concurrent-first", "protocol-concurrent-second"} {
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
func TestEngineResultBeforeReceiptSurvivesRestartAndSettlesOnce(t *testing.T) {
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
	issuedState, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	staged, err := fixture.Worker.ReviewResultBeforeReceipt(actionID, issuedState.State.PendingActions[actionID].ReviewSeries, "sha256:protocol-restart-result")
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
	spawned, err := restarted.Host.Spawn(actionID, "protocol-restarted-agent", protocol.SpawnStatusSpawned)
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
	if !completed || result.PayloadDigest != "sha256:protocol-restart-result" {
		t.Fatalf("settled result = %+v completed=%v", result, completed)
	}
	if _, pending := settled.State.PendingActions[actionID]; pending {
		t.Fatal("settled action remained pending")
	}

	duplicate, err := protocol.NewSpawnReceiptEvent(
		"protocol-restarted-spawn-duplicate", actionID, "fake-host", "protocol-restarted-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("NewSpawnReceiptEvent duplicate: %v", err)
	}
	observedInput, err := restarted.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint duplicate: %v", err)
	}
	beforeDuplicate := settled.Revision
	acceptance, err := restarted.Engine.Submit(duplicate, observedInput)
	if err != nil {
		t.Fatalf("submit duplicate SpawnReceipt: %v", err)
	}
	if acceptance.Status != "DUPLICATE" || acceptance.Revision != beforeDuplicate+1 {
		t.Fatalf("duplicate SpawnReceipt acceptance = %+v", acceptance)
	}
}

// Binding changes retire the old execution instance before installing a new
// Attempt. A late result remains visible as OBSOLETE_RESULT and cannot advance
// or replace the current Attempt.
func TestEngineReplacementAttemptQuarantinesLateResult(t *testing.T) {
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
	if _, err := fixture.Host.Spawn(oldActionID, "replacement-session", protocol.SpawnStatusSpawned); err != nil {
		t.Fatal(err)
	}
	start, err := protocol.NewLifecycleEventEvent("replacement-start", "fake-host", "replacement-session", protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	startFingerprint, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Engine.Submit(start, startFingerprint); err != nil {
		t.Fatal(err)
	}

	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load before replacement: %v", err)
	}
	oldPending := before.State.PendingActions[oldActionID]
	oldAttempt := before.State.Attempts[oldPending.Task]
	if err := fixture.VCS.Write("bindings/snapshot.txt", []byte("changed")); err != nil {
		t.Fatalf("change fake VCS snapshot: %v", err)
	}
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint recovery: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(oldActionID, protocol.Interruption{
		Class: authoring.FailureTransientEngine,
	}, observedInput)
	if err != nil {
		t.Fatalf("RecoverAttempt: %v", err)
	}
	if plan.Action != protocol.RecoveryNewAttempt {
		t.Fatalf("binding-change recovery plan = %+v", plan)
	}
	termination, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := termination.State.Attempts[oldPending.Task]; got.ID != oldAttempt.ID || got.ActionID != oldActionID {
		t.Fatal("attempt replaced before native termination receipt")
	}
	terminations := 0
	for _, intent := range termination.State.PendingHostActions {
		if intent.Operation != protocol.HostActionTerminateAgent {
			continue
		}
		terminations++
		if intent.Terminate == nil || intent.Terminate.WorkerActionID != oldActionID || intent.Terminate.AttemptID != oldAttempt.ID || intent.Terminate.Identity != "replacement-session" {
			t.Fatal("termination does not bind the original spawned attempt")
		}
		stop, err := protocol.NewLifecycleEventEvent("replacement-stop", "fake-host", "replacement-session", protocol.LifecycleStop)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Engine.Submit(stop, observedInput); err != nil {
			t.Fatal(err)
		}
		event := protocol.Event{ID: "replacement-terminated", Kind: protocol.KindHostActionReceipt, HostAction: &protocol.HostActionReceiptPayload{ActionID: intent.ActionID, Operation: intent.Operation, Provider: "fake-host", Correlation: intent.Correlation, PayloadDigest: intent.PayloadDigest, Status: protocol.HostActionStatusExecuted, LifecycleEvidence: &protocol.LifecycleEvidence{Identity: "replacement-session", Event: protocol.LifecycleStop}}}
		if _, err := fixture.Engine.Submit(event, observedInput); err != nil {
			t.Fatal(err)
		}
	}
	if terminations != 1 {
		t.Fatalf("native termination intents = %d, want exactly one", terminations)
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
	sampleDigest, _ := observedInput.Facts.Digest()
	if currentAttempt.Bindings.Snapshot != sampleDigest || currentAttempt.Bindings.Task != oldPending.Task || currentAttempt.Bindings.Responsibility != "fake-host" {
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
		"protocol-late-result", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late-result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent late: %v", err)
	}
	observedInput, err = fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint late result: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(late, observedInput)
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
	if result, recorded := afterLate.State.ObsoleteResults[oldActionID]; !recorded || len(result) != 1 || result[0].PayloadDigest != "sha256:late-result" {
		t.Fatalf("obsolete result = %+v recorded=%v", result, recorded)
	}
	if _, completed := afterLate.State.Results[currentAttempt.ActionID]; completed {
		t.Fatal("late result was attached to replacement action")
	}

	duplicate, err := protocol.NewWorkerResultEvent(
		"protocol-late-result-duplicate", oldActionID, "fake-host", protocol.OutcomePass, "sha256:late-result", "")
	if err != nil {
		t.Fatalf("NewWorkerResultEvent duplicate: %v", err)
	}
	observedInput, err = fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("Fingerprint duplicate late result: %v", err)
	}
	duplicateAcceptance, err := fixture.Engine.Submit(duplicate, observedInput)
	if err != nil {
		t.Fatalf("submit duplicate late result: %v", err)
	}
	if duplicateAcceptance.Status != "OBSOLETE_RESULT" || duplicateAcceptance.Revision != afterLate.Revision+1 {
		t.Fatalf("duplicate late result acceptance = %+v", duplicateAcceptance)
	}
}

func protocolObservation(read func() (string, error)) protocol.Observation {
	facts := func(value string) decision.Observation {
		return decision.Observation{Facts: []decision.Fact{{Source: decision.SourceFile, Key: "protocol-fixture", Value: value}}}
	}
	return protocol.Observation{Facts: facts(protocolFingerprint), Read: func() (decision.Observation, error) { value, err := read(); return facts(value), err }}
}
