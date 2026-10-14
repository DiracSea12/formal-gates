package testkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
	definition "formal-gates/internal/engine/testkit/workflow"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestHarnessReviewResultsAcceptedAndPreservedAfterRestart(t *testing.T) {
	for _, test := range []struct {
		name    string
		options HarnessOptions
		wantErr error
	}{
		{name: "smoke", options: HarnessOptions{Scenario: "smoke"}},
		{name: "semantic-recovery", options: HarnessOptions{Scenario: "failure-routing", FailureClass: string(authoring.FailureAgentRecoverable), Declared: "true"}},
		{name: "response-loss", options: HarnessOptions{Scenario: "fault", Fault: string(persistence.FaultSubmitResponseLost)}, wantErr: ErrInjected},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.options.ProjectRoot = root
			report, err := RunHarness(test.options)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("scenario error = %v, want %v", err, test.wantErr)
			}
			wantStatus := "PASS"
			if test.wantErr != nil {
				wantStatus = "ERROR"
			}
			if report.Status != wantStatus || len(report.Actions) != 1 || report.Snapshot == nil || report.Summary.Results != 1 {
				t.Fatalf("scenario report = %+v", report)
			}
			actionID := report.Actions[0].ActionID
			accepted := false
			for _, acceptance := range report.Acceptances {
				if acceptance.Kind == "WORKER_RESULT" {
					if acceptance.ActionID != actionID || acceptance.Status != "ACCEPTED" {
						t.Fatalf("result acceptance = %+v", acceptance)
					}
					accepted = true
				}
			}
			if !accepted {
				t.Fatal("no accepted worker result in report")
			}
			state := report.Snapshot.State
			result := state.Results[actionID]
			if result.ActionID != actionID || result.Outcome != protocol.OutcomePass || result.Review == nil || result.Review.Action != review.ProductReview || result.Review.DispatchID != actionID || result.Review.RequirementRevision != state.RequirementRevision() {
				t.Fatalf("persisted result = %+v", result)
			}
			fixture, err := NewProtocolFixture(root)
			if err != nil {
				t.Fatal(err)
			}
			verifyFakeReviewFixture(t, fixture.Engine, result)
			verified, err := fixture.Engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			state = verified.State
			series := state.ReviewSeries.ProductReview
			if series.Completed != 1 || series.Effective.Status != "PASS" || len(series.RawCandidates) != 1 || series.RawCandidates[0].DispatchID != actionID {
				t.Fatalf("persisted review series = %+v", series)
			}
			restarted, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "recover"})
			if err != nil {
				t.Fatal(err)
			}
			if restarted.Snapshot == nil || restarted.Summary.Results != 1 || !reflect.DeepEqual(restarted.Snapshot.State.Results[actionID], result) || !reflect.DeepEqual(restarted.Snapshot.State.ReviewSeries.ProductReview, series) {
				t.Fatalf("restart lost accepted review evidence: %+v", restarted)
			}
		})
	}
}

func TestHarnessEnvelopeBarrierDoesNotCreateTarget(t *testing.T) {
	for _, field := range []string{"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest", "packageDigest"} {
		t.Run(field, func(t *testing.T) {
			root := t.TempDir()
			report, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "envelope", MissingField: field})
			if err != nil {
				t.Fatalf("envelope scenario: %v", err)
			}
			if len(report.Errors) != 1 || report.Errors[0].Code != persistence.UnsupportedRunVersionCode {
				t.Fatalf("errors = %+v", report.Errors)
			}
			if _, err := os.Stat(report.StatePath); !os.IsNotExist(err) {
				t.Fatalf("write barrier created target: %v", err)
			}
		})
	}
}

func TestHarnessEnvelopeWriteReportsBarrierBytesAndValidWrite(t *testing.T) {
	root := t.TempDir()
	blocked, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "envelope-write", MissingField: "definition-digest"})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Metadata["errorCode"] != persistence.UnsupportedRunVersionCode || blocked.Metadata["targetBytes"] != int64(0) {
		t.Fatalf("blocked envelope report = %+v", blocked)
	}
	if info, statErr := os.Stat(blocked.StatePath); statErr != nil || info.Size() != 0 {
		t.Fatalf("blocked target = info=%v err=%v, want fresh zero-byte target", info, statErr)
	}
	validRoot := t.TempDir()
	valid, err := RunHarness(HarnessOptions{ProjectRoot: validRoot, Scenario: "envelope-write"})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Metadata["targetBytes"].(int64) == 0 {
		t.Fatalf("valid envelope did not write target: %+v", valid.Metadata)
	}
	before, err := os.ReadFile(valid.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	blockedExisting, err := RunHarness(HarnessOptions{ProjectRoot: validRoot, Scenario: "envelope-write", MissingField: "definition-digest"})
	if err != nil {
		t.Fatal(err)
	}
	if blockedExisting.Metadata["targetBytes"] != int64(len(before)) {
		t.Fatalf("existing target size changed in report: %+v", blockedExisting.Metadata)
	}
	after, err := os.ReadFile(valid.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected envelope rewrote an existing target")
	}
}

func TestHarnessUnknownReceiptUsesLifecycleMatches(t *testing.T) {
	for _, matches := range []string{"0", "2"} {
		report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "unknown-receipt", LifecycleMatches: matches})
		if err != nil {
			t.Fatalf("unknown receipt matches=%s: %v", matches, err)
		}
		if len(report.RecoveryPlan) != 1 || report.RecoveryPlan[0].Action != protocol.RecoveryOperator || report.RecoveryPlan[0].LifecycleMatches != atoiTest(t, matches) {
			t.Fatalf("unknown receipt matches=%s report=%+v", matches, report)
		}
	}
}

func TestHarnessDefinitionFixtureDeclaredness(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "definition.json")
	data, _ := json.Marshal(map[string]any{"declared": true})
	if err := os.WriteFile(fixture, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "definition-declaredness", DefinitionFixture: fixture})
	if err != nil || report.Metadata["declared"] != true || len(report.RecoveryPlan) != 1 || report.RecoveryPlan[0].Action != protocol.RecoveryAgent {
		t.Fatalf("declared fixture = %+v err=%v", report, err)
	}
	data, _ = json.Marshal(map[string]any{"declared": false})
	if err := os.WriteFile(fixture, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "definition-declaredness", DefinitionFixture: fixture}); err == nil {
		t.Fatal("undeclared semantic recovery accepted")
	}
}

func atoiTest(t *testing.T, value string) int {
	t.Helper()
	if value == "2" {
		return 2
	}
	return 0
}

func TestHarnessCapacityOneRefillsSecondEligibleTask(t *testing.T) {
	root := t.TempDir()
	initial, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "capacity-refill", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Actions) != 1 || len(initial.Acceptances) != 0 || len(initial.Next) != 1 || initial.Next[0].Kind != decision.KindReady {
		t.Fatalf("capacity initial report = %+v", initial)
	}
	if initial.Snapshot == nil || len(initial.Snapshot.State.Expected) != 2 || len(initial.Snapshot.State.PendingActions) != 1 {
		t.Fatalf("capacity initial snapshot = %+v", initial.Snapshot)
	}
	var first protocol.PendingAction
	for _, pending := range initial.Snapshot.State.PendingActions {
		first = pending
	}
	spawn, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-spawn", EventID: "capacity-spawn-1", ActionID: first.ActionID, Provider: "fake-host", Correlation: first.AttemptID, Status: protocol.SpawnStatusSpawned})
	if err != nil || len(spawn.Acceptances) != 1 || spawn.Acceptances[0].Kind != "SPAWN_RECEIPT" {
		t.Fatalf("capacity spawn = %+v err=%v", spawn, err)
	}
	result, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-worker", EventID: "capacity-worker-result-1", ActionID: first.ActionID, Provider: "fake-host", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:capacity-worker-result-1"})
	if err != nil || len(result.Acceptances) != 1 || len(result.Acceptances[0].Refill) != 1 || len(result.Next) != 1 || result.Next[0].Kind != decision.KindReady {
		t.Fatalf("capacity first result = %+v err=%v", result, err)
	}
	if result.Snapshot == nil || len(result.Snapshot.State.Expected) != 1 || len(result.Snapshot.State.PendingActions) != 1 || result.Snapshot.State.Expected[0].Step != "TASK_2" {
		t.Fatalf("capacity refilled snapshot = %+v", result.Snapshot)
	}
	var second protocol.PendingAction
	for _, pending := range result.Snapshot.State.PendingActions {
		second = pending
	}
	spawn, err = RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-spawn", EventID: "capacity-spawn-2", ActionID: second.ActionID, Provider: "fake-host", Correlation: second.AttemptID, Status: protocol.SpawnStatusSpawned})
	if err != nil {
		t.Fatalf("capacity second spawn: %v", err)
	}
	final, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-worker", EventID: "capacity-worker-result-2", ActionID: second.ActionID, Provider: "fake-host", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:capacity-worker-result-2"})
	if err != nil || len(final.Acceptances) != 1 || len(final.Next) != 1 || final.Next[0].Kind != decision.KindComplete {
		t.Fatalf("capacity final result = %+v err=%v", final, err)
	}
	if final.Snapshot == nil || len(final.Snapshot.State.Expected) != 0 || len(final.Snapshot.State.PendingActions) != 0 {
		t.Fatalf("capacity final snapshot = %+v", final.Snapshot)
	}
	replay, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-worker", EventID: "capacity-worker-result-1", ActionID: first.ActionID, Provider: "fake-host", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:capacity-worker-result-1"})
	if err != nil || len(replay.Acceptances) != 1 || replay.Acceptances[0].Status != "DUPLICATE" || len(replay.Next) != 0 {
		t.Fatalf("capacity historical result replay = %+v err=%v", replay, err)
	}
	payloadReplay, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-worker", EventID: "capacity-worker-result-payload-replay", ActionID: first.ActionID, Provider: "fake-host", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:capacity-worker-result-1"})
	if err != nil || len(payloadReplay.Acceptances) != 1 || payloadReplay.Acceptances[0].Status != "DUPLICATE" || len(payloadReplay.Next) != 0 {
		t.Fatalf("capacity payload-level result replay = %+v err=%v", payloadReplay, err)
	}
}

func TestHarnessResultBeforeReceiptSettlesAfterRestart(t *testing.T) {
	report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "result-before-receipt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Acceptances) != 2 || report.Acceptances[0].Status != "STAGED" || report.Recovery.Outcome != persistence.RecoveryClean {
		t.Fatalf("result-before-receipt report = %+v", report)
	}
	if report.Summary.PendingResults != 0 || report.Summary.Results != 1 || report.SideEffects["fakeHost.spawn"] != 1 {
		t.Fatalf("settled report = %+v", report)
	}
}

func TestHarnessTerminalReplayUsesDurableSummaryWithoutWrites(t *testing.T) {
	root := t.TempDir()
	terminal, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "terminal-replay"})
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Acceptances) != 1 || !reflect.DeepEqual(replay.Acceptances[0], terminal.Terminal.LastEventReceipt) || replay.Next[0].Kind != decision.KindComplete {
		t.Fatalf("terminal replay = %+v", replay)
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal replay restored active state: %v", err)
	}
	if _, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-request", EventID: "after-terminal"}); err == nil {
		t.Fatal("terminal run accepted a new request")
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal submit recreated active state: %v", err)
	}
	if _, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "terminal-replay", PayloadDigest: "sha256:mismatch"}); err == nil {
		t.Fatal("terminal event digest mismatch was accepted")
	} else {
		var rejected *protocol.RejectedError
		if !errors.As(err, &rejected) || rejected.Code != protocol.CodeDuplicateEventMismatch {
			t.Fatalf("mismatch error = %v", err)
		}
	}
	after, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("terminal replay changed durable summary")
	}
}

func TestHarnessTerminalSummaryRequiresVersionEnvelope(t *testing.T) {
	root := t.TempDir()
	report, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(report.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"writer": "engine",`), nil, 1)
	if err := os.WriteFile(report.SummaryPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), data...)
	if _, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "query-terminal"}); err == nil {
		t.Fatal("terminal summary without writer was accepted")
	}
	if _, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-request", EventID: "advance-invalid-terminal"}); err == nil {
		t.Fatal("terminal summary without writer permitted an advance")
	}
	if _, err := os.Stat(report.StatePath); !os.IsNotExist(err) {
		t.Fatalf("rejected terminal advance created active state: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, "engine-state", terminalSummaryName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected terminal query rewrote summary")
	}
}

func TestHarnessReplaceAfterRecoveryPreservesFakeVCSCount(t *testing.T) {
	root := t.TempDir()
	crashed, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "fault", Fault: string(persistence.FaultReplaceAfter)})
	if !errors.Is(err, persistence.ErrInjectedCrash) {
		t.Fatalf("replace-after error = %v", err)
	}
	if crashed.SideEffects["fakeVCS.fault-replace-after"] != 1 {
		t.Fatalf("crashed side effects = %+v", crashed.SideEffects)
	}
	recovered, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "recover"})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Recovery.Outcome != persistence.RecoveryCommitted || recovered.SideEffects["fakeVCS.fault-replace-after"] != 1 {
		t.Fatalf("recovery report = %+v", recovered)
	}
	continued, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-request", EventID: "after-recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(continued.Acceptances) != 1 || continued.SideEffects["fakeVCS.fault-replace-after"] != 1 {
		t.Fatalf("continued report = %+v", continued)
	}
	// write.lock 是持久的目录互斥工件（内核文件锁永不删除），不属于协议
	// 残留；.tmp 与 intent 才是必须清除的协议字节。
	for _, path := range continued.Paths {
		if filepath.Ext(path) == ".tmp" || path == "engine-state/state.json.intent" {
			t.Fatalf("recovery left protocol artifact %q", path)
		}
	}
}

func TestHarnessInvalidEventsPreserveRevisionAndBytesOracle(t *testing.T) {
	root := t.TempDir()
	initialized, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := os.ReadFile(initialized.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "invalid-events"})
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, err := os.ReadFile(report.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("invalid event submissions changed the state bytes")
	}
	if len(report.Acceptances) != 0 {
		t.Fatalf("invalid event acceptances = %+v, want none", report.Acceptances)
	}
	if len(report.Revisions) != 2 || report.Revisions[0] != report.Revisions[1] {
		t.Fatalf("invalid event revisions = %v, want an unchanged revision pair", report.Revisions)
	}
	for _, issue := range report.Errors {
		if issue.Code == protocol.CodeStaleFreshness {
			t.Fatalf("invalid event report included stale freshness error: %+v", issue)
		}
	}
}

func TestHarnessFailureRoutingTransientExhaustionOffersDecision(t *testing.T) {
	root := t.TempDir()
	report, err := RunHarness(HarnessOptions{
		ProjectRoot:  root,
		Scenario:     "failure-routing",
		FailureClass: string(authoring.FailureTransientEngine),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Metadata["retryPolicyMaxAttempts"] != 3 || report.Metadata["retryAttemptsExhausted"] != 3 {
		t.Fatalf("transient retry counters = %+v, want 3/3", report.Metadata)
	}
	if got := report.Metadata["retryAttemptSequence"]; !reflect.DeepEqual(got, []int{1, 2, 3}) || report.Metadata["finalFailureKind"] != string(authoring.FailureTransientEngine) || report.Metadata["recoveryAction"] != string(protocol.RecoveryWait) {
		t.Fatalf("transient retry trace = %+v", report.Metadata)
	}
	if len(report.RecoveryPlan) != 1 || report.RecoveryPlan[0].Action != protocol.RecoveryWait {
		t.Fatalf("transient recovery plan = %+v, want one WAIT plan", report.RecoveryPlan)
	}
	if len(report.Next) != 1 || report.Next[0].Kind != decision.KindAsk {
		t.Fatalf("transient Next records = %+v, want one ASK record", report.Next)
	}
	if report.SideEffects["spawn"] != 2 || report.SideEffects["initialSpawn"] != 1 || report.Metadata["newSpawns"] != 2 {
		t.Fatalf("transient spawn accounting = sideEffects=%+v metadata=%+v", report.SideEffects, report.Metadata)
	}
	registry := definition.Registry()
	step, err := authoring.NewAgentStep(authoring.Header{ID: "retry.worker", NodeID: "retry", DefinitionVersion: definition.Version}, authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Postconditions: []authoring.PredicateRef{{ID: "pred.review.post"}}}, authoring.AgentSpec{Handler: "engine.review.worker.v3", Reason: authoring.ReasonIndependentReview, ReviewSeries: authoring.ReviewSeriesUnaffiliated, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 3}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "retry", Steps: []authoring.Step{step}}, registry)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := NewProtocolFixtureWithDefinition(root, compiled, registry, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := fixture.Engine.Plan(protocol.Observation{})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(plan.Next.Ask)
	got, _ := json.Marshal(report.Next[0].Payload)
	if plan.Next.Ask == nil || !bytes.Equal(want, got) {
		t.Fatalf("reported Ask=%s actual=%s", got, want)
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.State, report.Snapshot.State) {
		t.Fatal("report did not retain the actual persisted state")
	}
	ended := snapshot.State.EndedAttempts[runtime.TaskKey{Node: "retry", Step: "retry.worker"}]
	if ended.Attempts != 3 || ended.MaxAttempts != 3 || !ended.RetryExhausted || len(snapshot.State.RejectedResults) != 3 {
		t.Fatal("retry report disagrees with the original attempt/results")
	}
	for _, rejected := range snapshot.State.RejectedResults {
		if rejected.Payload.FailureClass != authoring.FailureTransientEngine || rejected.Payload.Outcome != protocol.OutcomeRuntimeError {
			t.Fatal("retry report lost original failure content")
		}
	}
}

func TestHarnessRegisteredScenarioCensus(t *testing.T) {
	for _, scenario := range []string{
		"revision-sequence", "idempotency", "freshness", "concurrent-submit", "cas",
		"lock-recovery", "lifecycle", "invalid-events",
		"result-before-receipt", "next-sequence", "failure-routing",
	} {
		t.Run(scenario, func(t *testing.T) {
			report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: scenario})
			if err != nil {
				t.Fatalf("%s: %v\nreport=%+v", scenario, err, report)
			}
			if report.Status != "PASS" {
				t.Fatalf("%s status = %s", scenario, report.Status)
			}
		})
	}
	for _, mode := range []string{"transient", "nontransient", "unknown", "receipt-one", "receipt-multiple", "receipt-none"} {
		t.Run("interruption/"+mode, func(t *testing.T) {
			report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "interruption", Interruption: mode})
			if err != nil {
				t.Fatalf("%s: %v\nreport=%+v", mode, err, report)
			}
			if len(report.RecoveryPlan) != 1 {
				t.Fatalf("%s recovery plans = %+v", mode, report.RecoveryPlan)
			}
		})
	}
	t.Run("request-decision", func(t *testing.T) {
		root := t.TempDir()
		request, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-request", EventID: "census-request"})
		if err != nil {
			t.Fatal(err)
		}
		token := request.Freshness["census-request"]
		decisionReport, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "submit-decision", EventID: "census-decision", RequestID: "census-request", PayloadDigest: token})
		if err != nil {
			t.Fatal(err)
		}
		if len(decisionReport.Acceptances) != 1 || decisionReport.Acceptances[0].Status != "ACCEPTED" {
			t.Fatalf("decision report = %+v", decisionReport)
		}
	})
}

func TestHarnessNamedFaultCensus(t *testing.T) {
	persistencePoints := []persistence.FaultPoint{
		persistence.FaultIntentBefore, persistence.FaultIntentAfter,
		persistence.FaultTempSyncBefore, persistence.FaultTempSyncAfter,
		persistence.FaultExecuteBefore, persistence.FaultExecuteAfter,
		persistence.FaultObserveBefore, persistence.FaultReconcileBefore,
		persistence.FaultReplaceBefore, persistence.FaultReplaceAfter,
		persistence.FaultCommitResponseLost,
	}
	for _, point := range persistencePoints {
		t.Run(string(point), func(t *testing.T) {
			root := t.TempDir()
			if _, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "fault", Fault: string(point)}); !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("fault %s error = %v", point, err)
			}
			recovered, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "recover"})
			if err != nil {
				t.Fatalf("recover %s: %v", point, err)
			}
			for _, path := range recovered.Paths {
				if filepath.Ext(path) == ".tmp" || path == "engine-state/state.json.intent" {
					t.Fatalf("fault %s left artifact %q", point, path)
				}
			}
		})
	}
	for _, point := range []persistence.FaultPoint{
		persistence.FaultSpawnAfterAttach, persistence.FaultResultBeforeReceipt,
		persistence.FaultSubmitResponseLost,
	} {
		t.Run(string(point), func(t *testing.T) {
			if _, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "fault", Fault: string(point)}); !errors.Is(err, ErrInjected) {
				t.Fatalf("fault %s error = %v", point, err)
			}
		})
	}
}
