package testkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

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
	valid, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "envelope-write"})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Metadata["targetBytes"].(int64) == 0 {
		t.Fatalf("valid envelope did not write target: %+v", valid.Metadata)
	}
}

func TestHarnessRegisteredUnknownReceiptAndHostReconcileAliases(t *testing.T) {
	for _, matches := range []string{"0", "2"} {
		report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "unknown-receipt", LifecycleMatches: matches})
		if err != nil {
			t.Fatalf("unknown receipt matches=%s: %v", matches, err)
		}
		if len(report.RecoveryPlan) != 1 || report.RecoveryPlan[0].Action != protocol.RecoveryOperator || report.RecoveryPlan[0].LifecycleMatches != atoiTest(t, matches) {
			t.Fatalf("unknown receipt matches=%s report=%+v", matches, report)
		}
	}
	fulfilled, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "reconcile-host-action", Fact: "sha256:observed", Expected: "sha256:observed"})
	if err != nil || len(fulfilled.RecoveryPlan) == 0 || fulfilled.RecoveryPlan[0].Action != protocol.RecoveryReconcile {
		t.Fatalf("fulfilled host reconcile = %+v err=%v", fulfilled, err)
	}
	conflict, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "reconcile-host-action", Conflict: "true"})
	if err != nil || len(conflict.RecoveryPlan) == 0 || conflict.RecoveryPlan[0].Action != protocol.RecoveryOperator {
		t.Fatalf("conflict host reconcile = %+v err=%v", conflict, err)
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
	report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "capacity-refill", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 2 || len(report.Acceptances) != 2 || len(report.Acceptances[1].Refill) != 1 {
		t.Fatalf("capacity report = %+v", report)
	}
	if report.Snapshot == nil || len(report.Snapshot.State.Expected) != 1 || len(report.Snapshot.State.PendingActions) != 1 {
		t.Fatalf("refilled snapshot = %+v", report.Snapshot)
	}
	if report.SideEffects["fakeHost.spawn"] != 1 {
		t.Fatalf("side effects = %+v", report.SideEffects)
	}
	if report.PreResultSnapshot == nil || report.PreResultSnapshot.State.Expected[0].Step != "TASK_1" || report.Actions[1].ActionID != "act:capacity/TASK_2" {
		t.Fatalf("pre-result/task identifiers = %+v actions=%+v", report.PreResultSnapshot, report.Actions)
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

func TestHarnessHostUnknownReconcilesAtMostOnce(t *testing.T) {
	report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "host-action", Status: protocol.HostActionStatusUnknown, Choice: "fulfilled"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SideEffects) != 1 || len(report.RecoveryPlan) != 2 {
		t.Fatalf("host action report = %+v", report)
	}
	for _, calls := range report.SideEffects {
		if calls != 1 {
			t.Fatalf("host action was re-executed %d times", calls)
		}
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
	for _, path := range continued.Paths {
		if filepath.Ext(path) == ".tmp" || path == "engine-state/state.json.intent" || path == "engine-state/write.lock" {
			t.Fatalf("recovery left protocol artifact %q", path)
		}
	}
}

func TestHarnessFingerprintRetryDoesNotRepeatStaleOperation(t *testing.T) {
	report, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "fingerprint"})
	if err != nil {
		t.Fatal(err)
	}
	if count, ok := report.SideEffects["stale-operation"]; !ok || count != 0 {
		t.Fatalf("stale operation side effects = %v (present=%v), want 0", count, ok)
	}
	if count := report.SideEffects["retry-operation"]; count != 1 {
		t.Fatalf("retry operation side effects = %d, want 1", count)
	}
	if len(report.Acceptances) != 1 || report.Acceptances[0].Status != "ACCEPTED" {
		t.Fatalf("fingerprint retry receipt = %+v, want one ACCEPTED receipt", report.Acceptances)
	}
	if report.Snapshot == nil || len(report.Snapshot.State.PendingHostActions) != 0 {
		t.Fatalf("fingerprint retry left pending host action: %+v", report.Snapshot)
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

func TestHarnessFailureRoutingTransientExhaustionWaits(t *testing.T) {
	report, err := RunHarness(HarnessOptions{
		ProjectRoot:  t.TempDir(),
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
	if len(report.Next) != 1 || report.Next[0].Kind != decision.KindWait {
		t.Fatalf("transient Next records = %+v, want one WAIT record", report.Next)
	}
	if report.SideEffects["spawn"] != 0 || report.SideEffects["initialSpawn"] != 1 || report.Metadata["newSpawns"] != 0 {
		t.Fatalf("transient spawn accounting = sideEffects=%+v metadata=%+v", report.SideEffects, report.Metadata)
	}
}

func TestHarnessRegisteredScenarioCensus(t *testing.T) {
	for _, scenario := range []string{
		"revision-sequence", "idempotency", "freshness", "concurrent-submit", "cas",
		"fingerprint", "lock-recovery", "host-action", "lifecycle", "invalid-events",
		"result-before-receipt", "next-sequence", "failure-routing", "full",
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
				if filepath.Ext(path) == ".tmp" || path == "engine-state/state.json.intent" || path == "engine-state/write.lock" {
					t.Fatalf("fault %s left artifact %q", point, path)
				}
			}
		})
	}
	for _, point := range []persistence.FaultPoint{
		persistence.FaultSpawnAfterAttach, persistence.FaultResultBeforeReceipt,
		persistence.FaultSubmitResponseLost, persistence.FaultHostObserveBefore,
		persistence.FaultHostReconcileBefore, persistence.FaultHostCommitBefore,
		persistence.FaultHostCommitAfter,
	} {
		t.Run(string(point), func(t *testing.T) {
			if _, err := RunHarness(HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "fault", Fault: string(point)}); !errors.Is(err, ErrInjected) {
				t.Fatalf("fault %s error = %v", point, err)
			}
		})
	}
}

func TestHarnessFullIsDeterministic(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	first, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "full"})
	if err != nil {
		t.Fatal(err)
	}
	firstSummary, err := os.ReadFile(first.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	second, err := RunHarness(HarnessOptions{ProjectRoot: root, Scenario: "full"})
	if err != nil {
		t.Fatal(err)
	}
	secondSummary, err := os.ReadFile(second.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("full reports differ:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if !bytes.Equal(firstSummary, secondSummary) {
		t.Fatal("full terminal summary bytes are not deterministic")
	}
}
