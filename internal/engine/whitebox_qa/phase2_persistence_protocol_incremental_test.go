package whitebox_qa

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/validate"
)

func requirePhase2ProtocolCode(t *testing.T, err error, want string) {
	t.Helper()
	var rejected *protocol.RejectedError
	if !errors.As(err, &rejected) || rejected.Code != want {
		t.Fatalf("error = %v, want protocol code %s", err, want)
	}
}

func requirePhase2SnapshotUnchanged(t *testing.T, before protocol.Snapshot, engine *protocol.Engine) {
	t.Helper()
	after, err := engine.Load()
	if err != nil {
		t.Fatalf("Load after rejected operation: %v", err)
	}
	if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatalf("rejected operation changed state: revision %d -> %d", before.Revision, after.Revision)
	}
}

func requirePhase2LiveTaskLedger(t *testing.T, engine *protocol.Engine, task runtime.TaskKey, attempt protocol.Attempt, pending protocol.PendingAction, status runtime.TaskStatus, revision uint64) {
	t.Helper()
	live, err := engine.Load()
	if err != nil {
		t.Fatalf("Load live task state: %v", err)
	}
	if live.Revision != revision {
		t.Fatalf("live task revision = %d, want %d", live.Revision, revision)
	}
	if got := live.State.TaskStatusOf(task); got != status {
		t.Fatalf("live task status = %s, want %s", got, status)
	}
	if len(live.State.Expected) != 1 || live.State.Expected[0] != task {
		t.Fatalf("live expected ledger = %v, want [%s]", live.State.Expected, task.String())
	}
	currentAttempt, exists := live.State.Attempts[task]
	if !exists || !reflect.DeepEqual(currentAttempt, attempt) || len(live.State.Attempts) != 1 {
		t.Fatalf("live current Attempt = %+v present=%v, want exactly %+v", currentAttempt, exists, attempt)
	}
	currentPending, exists := live.State.PendingActions[pending.ActionID]
	if !exists || !reflect.DeepEqual(currentPending, pending) || len(live.State.PendingActions) != 1 {
		t.Fatalf("live pending action = %+v present=%v, want exactly %+v", currentPending, exists, pending)
	}
}

// Adapter HostAction intents and receipts are both closed by the registered
// operation and its exact parameter/evidence schemas. Rejected forms must not
// create an intent or consume a revision.
func TestPhase2WhiteboxHostActionSchemasFenceIntentAndReceiptWrites(t *testing.T) {
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
		{name: "empty operation", operation: "", params: map[string]any{}, code: protocol.CodeFreeCommandForm},
		{name: "unregistered operation", operation: "op.not.registered", params: map[string]any{}, code: protocol.CodeOperationNotRegistered},
		{name: "free command params", operation: "op.fan.transport", params: "rm -rf delivery", code: protocol.CodeFreeCommandForm},
		{name: "undeclared parameter", operation: "op.fan.transport", params: map[string]any{"command": "echo bypass"}, code: protocol.CodeOperationSchemaInvalid},
		{name: "wrong parameter type", operation: "op.fan.transport", params: map[string]any{"target": 42}, code: protocol.CodeOperationSchemaInvalid},
	}
	for _, tc := range invalidIntents {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := fixture.Engine.ExecuteHostAction(tc.operation, tc.params, fingerprint)
			requirePhase2ProtocolCode(t, err, tc.code)
			requirePhase2SnapshotUnchanged(t, baseline, fixture.Engine)
		})
	}

	intent, revision, err := fixture.Engine.ExecuteHostAction(
		"op.fan.transport",
		map[string]any{"target": "phase2-whitebox", "retries": 2},
		fingerprint,
	)
	if err != nil {
		t.Fatalf("ExecuteHostAction valid intent: %v", err)
	}
	if revision != baseline.Revision+1 || intent.Operation != protocol.HostActionExecuteAdapterOperation ||
		intent.Adapter == nil || intent.Resume != nil || intent.Terminate != nil || intent.PayloadDigest == "" {
		t.Fatalf("typed HostAction intent = %+v revision=%d", intent, revision)
	}
	afterIntent, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load persisted intent: %v", err)
	}
	if got, ok := afterIntent.State.PendingHostActions[intent.ActionID]; !ok ||
		got.ActionID != intent.ActionID || got.Operation != intent.Operation || got.PayloadDigest != intent.PayloadDigest ||
		got.Resume != nil || got.Terminate != nil || got.Adapter == nil || got.Adapter.Operation != intent.Adapter.Operation ||
		got.Adapter.Params["target"] != "phase2-whitebox" || got.Adapter.Params["retries"] != float64(2) {
		t.Fatalf("pending HostAction = %+v present=%v, want durable adapter intent %+v", got, ok, intent)
	}

	invalidReceipt, err := protocol.NewAdapterHostActionReceiptEvent(
		"phase2-host-schema-invalid",
		intent.ActionID,
		intent.Adapter.Operation,
		"fake-host",
		"phase2-host-correlation",
		intent.PayloadDigest,
		protocol.HostActionStatusExecuted,
		"",
		map[string]any{
			"identity":          "phase2-host",
			"observationDigest": "sha256:observed",
			"undeclared":        true,
		},
	)
	if err != nil {
		t.Fatalf("NewAdapterHostActionReceiptEvent invalid evidence: %v", err)
	}
	if _, err := fixture.Engine.Submit(invalidReceipt, fingerprint); err == nil {
		t.Fatal("receipt with undeclared evidence was accepted")
	} else {
		requirePhase2ProtocolCode(t, err, protocol.CodeOperationSchemaInvalid)
	}
	requirePhase2SnapshotUnchanged(t, afterIntent, fixture.Engine)

	validProvider := "fake-host"
	validCorrelation := "receipt-correlation-exact"
	validStatus := protocol.HostActionStatusExecuted
	wantEvidence := map[string]any{
		"identity":          "receipt-evidence-identity",
		"observationDigest": "sha256:receipt-evidence",
	}
	validReceipt, err := protocol.NewAdapterHostActionReceiptEvent(
		"phase2-host-schema-valid",
		intent.ActionID,
		intent.Adapter.Operation,
		validProvider,
		validCorrelation,
		intent.PayloadDigest,
		validStatus,
		"",
		wantEvidence,
	)
	if err != nil {
		t.Fatalf("NewAdapterHostActionReceiptEvent valid evidence: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(validReceipt, fingerprint)
	if err != nil {
		t.Fatalf("Submit valid HostAction receipt: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || acceptance.Revision != afterIntent.Revision+1 {
		t.Fatalf("valid HostAction receipt acceptance = %+v", acceptance)
	}
	settled, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load settled HostAction: %v", err)
	}
	if _, pending := settled.State.PendingHostActions[intent.ActionID]; pending {
		t.Fatal("executed HostAction receipt left its intent pending")
	}
	receipt, ok := settled.State.HostActionReceipts[intent.ActionID]
	if !ok || len(settled.State.HostActionReceipts) != 1 {
		t.Fatalf("settled HostAction receipts = %+v present=%v, want one receipt", settled.State.HostActionReceipts, ok)
	}
	if receipt.ActionID != intent.ActionID || receipt.Operation != protocol.HostActionExecuteAdapterOperation ||
		receipt.AdapterOperation != intent.Adapter.Operation || receipt.Provider != validProvider ||
		receipt.Correlation != validCorrelation || receipt.PayloadDigest != intent.PayloadDigest ||
		receipt.Status != validStatus || receipt.FailureClass != "" || receipt.LifecycleEvidence != nil ||
		receipt.AdapterEvidence == nil || !reflect.DeepEqual(receipt.AdapterEvidence.Values, wantEvidence) || receipt.Digest == "" {
		t.Fatalf("settled HostAction receipt fields = %+v", receipt)
	}
}

// The expected-task ledger, current Attempt, and pending action are one
// transactionally consistent index. Only legal progress for that current
// task may advance it, and TERMINAL removes all three live entries together.
func TestPhase2WhiteboxTaskProgressMaintainsExpectedAttemptLedger(t *testing.T) {
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
	baseline, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load issued state: %v", err)
	}
	attempt, ok := baseline.State.Attempts[action.Task]
	if !ok || attempt.ActionID != action.ActionID || attempt.ID == "" ||
		attempt.Bindings.Task != action.Task || attempt.Bindings.Snapshot == "" || attempt.Bindings.Responsibility != "fake-host" ||
		attempt.Plan.PlanDigest == "" || attempt.Plan.DefinitionDigest == "" || attempt.Plan.StateDigest == "" || attempt.Plan.ObservationDigest == "" {
		t.Fatalf("issued Attempt binding = %+v present=%v", attempt, ok)
	}
	if len(baseline.State.Expected) != 1 || baseline.State.Expected[0] != action.Task {
		t.Fatalf("expected task ledger = %v", baseline.State.Expected)
	}
	pending, ok := baseline.State.PendingActions[action.ActionID]
	if !ok || pending.ActionID != action.ActionID || pending.Task != action.Task ||
		pending.Step != string(attempt.Step) || pending.AttemptID != attempt.ID {
		t.Fatalf("pending action = %+v present=%v", pending, ok)
	}
	requirePhase2LiveTaskLedger(t, fixture.Engine, action.Task, attempt, pending, runtime.TaskIssued, baseline.Revision)
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("ObserveFingerprint: %v", err)
	}

	notCurrent, err := protocol.NewTaskEvent(
		"phase2-task-not-current",
		testkit.TaskKey("entry", "entry.parse"),
		"att:not-current",
		runtime.TaskRunning,
	)
	if err != nil {
		t.Fatalf("NewTaskEvent not current: %v", err)
	}
	if _, err := fixture.Engine.Submit(notCurrent, fingerprint); err == nil {
		t.Fatal("progress for a task outside the expected set was accepted")
	} else {
		requirePhase2ProtocolCode(t, err, protocol.CodeEventNotCurrent)
	}
	requirePhase2SnapshotUnchanged(t, baseline, fixture.Engine)

	illegal, err := protocol.NewTaskEvent(
		"phase2-task-illegal-transition",
		action.Task,
		attempt.ID,
		runtime.TaskValidating,
	)
	if err != nil {
		t.Fatalf("NewTaskEvent illegal transition: %v", err)
	}
	if _, err := fixture.Engine.Submit(illegal, fingerprint); err == nil {
		t.Fatal("ISSUED to VALIDATING transition was accepted")
	} else {
		requirePhase2ProtocolCode(t, err, protocol.CodeIllegalTransition)
	}
	requirePhase2SnapshotUnchanged(t, baseline, fixture.Engine)

	for index, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		event, err := protocol.NewTaskEvent(
			protocol.EventID("phase2-task-progress-"+string(status)),
			action.Task,
			attempt.ID,
			status,
		)
		if err != nil {
			t.Fatalf("NewTaskEvent %s: %v", status, err)
		}
		acceptance, err := fixture.Engine.Submit(event, fingerprint)
		if err != nil {
			t.Fatalf("Submit %s: %v", status, err)
		}
		if acceptance.Status != "ACCEPTED" || acceptance.Revision != baseline.Revision+uint64(index)+1 {
			t.Fatalf("%s acceptance = %+v", status, acceptance)
		}
		if status != runtime.TaskTerminal {
			requirePhase2LiveTaskLedger(t, fixture.Engine, action.Task, attempt, pending, status, acceptance.Revision)
		}
	}
	final, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("Load terminal task state: %v", err)
	}
	if final.State.TaskStatusOf(action.Task) != runtime.TaskTerminal {
		t.Fatalf("task status = %s, want TERMINAL", final.State.TaskStatusOf(action.Task))
	}
	if len(final.State.Expected) != 0 {
		t.Fatalf("terminal task remained expected: %v", final.State.Expected)
	}
	if len(final.State.Attempts) != 0 {
		t.Fatalf("terminal task retained current Attempt index entries: %+v", final.State.Attempts)
	}
	if len(final.State.PendingActions) != 0 {
		t.Fatalf("terminal task retained pending action index entries: %+v", final.State.PendingActions)
	}
	if _, recorded := final.State.Events[string(notCurrent.ID)]; recorded {
		t.Fatal("not-current rejection entered the event ledger")
	}
	if _, recorded := final.State.Events[string(illegal.ID)]; recorded {
		t.Fatal("illegal transition rejection entered the event ledger")
	}
}

// Terminal fallback is valid only through an exact engine envelope and the
// retained final receipts. Query and replay are read-only and can never
// recreate an active state after completion.
func TestPhase2WhiteboxTerminalSummaryIsVersionBoundAndReplayOnly(t *testing.T) {
	root := t.TempDir()
	terminal, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "terminal"})
	if err != nil {
		t.Fatalf("terminal harness: %v", err)
	}
	if terminal.Terminal == nil {
		t.Fatal("terminal scenario returned no durable summary")
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
	if gotEnvelope != wantEnvelope || summary.Next.Kind != decision.KindComplete || summary.Status != "COMPLETE" {
		t.Fatalf("terminal identity/status = envelope %+v next %s status %s", gotEnvelope, summary.Next.Kind, summary.Status)
	}
	request, err := protocol.NewRequestEvent(
		"terminal-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		t.Fatalf("NewRequestEvent: %v", err)
	}
	requestDigest, err := request.Digest()
	if err != nil {
		t.Fatalf("request digest: %v", err)
	}
	decisionEvent, err := protocol.NewDecideEvent(
		"terminal-decision", "terminal-request", summary.LastRequestReceipt.FreshnessToken, "confirm")
	if err != nil {
		t.Fatalf("NewDecideEvent: %v", err)
	}
	decisionDigest, err := decisionEvent.Digest()
	if err != nil {
		t.Fatalf("decision digest: %v", err)
	}
	if summary.LastRequestReceipt.EventID != string(request.ID) || summary.LastRequestDigest != requestDigest ||
		summary.LastEventReceipt.EventID != string(decisionEvent.ID) || summary.LastEventDigest != decisionDigest {
		t.Fatalf("terminal receipts/digests = %+v", summary)
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal run retained active state: %v", err)
	}
	before, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatalf("read terminal summary: %v", err)
	}
	query, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "query-terminal"})
	if err != nil {
		t.Fatalf("query terminal: %v", err)
	}
	if query.Terminal == nil || query.Terminal.Writer != summary.Writer ||
		query.Terminal.StateSchemaVersion != summary.StateSchemaVersion ||
		query.Terminal.WorkflowDefinitionVersion != summary.WorkflowDefinitionVersion ||
		query.Terminal.DefinitionDigest != summary.DefinitionDigest ||
		query.Terminal.PackageDigest != summary.PackageDigest ||
		query.Terminal.Status != summary.Status || query.Terminal.Revision != summary.Revision ||
		!reflect.DeepEqual(query.Terminal.LastRequestReceipt, summary.LastRequestReceipt) ||
		query.Terminal.LastRequestDigest != summary.LastRequestDigest ||
		!reflect.DeepEqual(query.Terminal.LastEventReceipt, summary.LastEventReceipt) ||
		query.Terminal.LastEventDigest != summary.LastEventDigest ||
		len(query.Next) != 1 || query.Next[0].Kind != decision.KindComplete {
		t.Fatalf("terminal query = %+v", query)
	}
	replay, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: root, Scenario: "terminal-replay"})
	if err != nil {
		t.Fatalf("terminal replay: %v", err)
	}
	if len(replay.Acceptances) != 1 || !reflect.DeepEqual(replay.Acceptances[0], summary.LastEventReceipt) ||
		len(replay.Next) != 1 || replay.Next[0].Kind != decision.KindComplete {
		t.Fatalf("terminal replay result = %+v", replay)
	}
	if _, err := testkit.RunHarness(testkit.HarnessOptions{
		ProjectRoot: root, Scenario: "terminal-replay", PayloadDigest: "sha256:mismatch",
	}); err == nil {
		t.Fatal("terminal replay accepted a different event digest")
	} else {
		requirePhase2ProtocolCode(t, err, protocol.CodeDuplicateEventMismatch)
	}
	if _, err := testkit.RunHarness(testkit.HarnessOptions{
		ProjectRoot: root, Scenario: "submit-request", EventID: "phase2-after-terminal",
	}); err == nil {
		t.Fatal("terminal run recreated an active submit path")
	}
	after, err := os.ReadFile(terminal.SummaryPath)
	if err != nil {
		t.Fatalf("read terminal summary after replay: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("terminal query or replay changed durable summary bytes")
	}
	if _, err := os.Stat(terminal.StatePath); !os.IsNotExist(err) {
		t.Fatalf("terminal query or replay recreated active state: %v", err)
	}

	for _, field := range []string{
		"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest", "packageDigest",
	} {
		t.Run("missing "+field, func(t *testing.T) {
			invalidRoot := t.TempDir()
			invalid, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: invalidRoot, Scenario: "terminal"})
			if err != nil {
				t.Fatalf("terminal harness: %v", err)
			}
			data, err := os.ReadFile(invalid.SummaryPath)
			if err != nil {
				t.Fatalf("read terminal summary: %v", err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("decode terminal summary: %v", err)
			}
			delete(document, field)
			invalidBytes, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatalf("encode invalid terminal summary: %v", err)
			}
			invalidBytes = append(invalidBytes, '\n')
			if err := os.WriteFile(invalid.SummaryPath, invalidBytes, 0o600); err != nil {
				t.Fatalf("write invalid terminal summary: %v", err)
			}
			if _, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: invalidRoot, Scenario: "query-terminal"}); err == nil {
				t.Fatalf("terminal query accepted missing %s", field)
			} else {
				var unsupported *persistence.UnsupportedRunVersionError
				if !errors.As(err, &unsupported) || unsupported.Field != field ||
					!strings.HasPrefix(err.Error(), persistence.UnsupportedRunVersionCode+":") {
					t.Fatalf("missing %s error = %v", field, err)
				}
			}
			unchanged, err := os.ReadFile(invalid.SummaryPath)
			if err != nil {
				t.Fatalf("read rejected terminal summary: %v", err)
			}
			if !bytes.Equal(unchanged, invalidBytes) {
				t.Fatalf("missing %s query rewrote terminal summary", field)
			}
			if _, err := os.Stat(invalid.StatePath); !os.IsNotExist(err) {
				t.Fatalf("missing %s query created active state: %v", field, err)
			}
		})
	}
}

// The raw diagnose path must preserve unsupported and malformed inputs while
// reporting the bytes it can safely understand. It never invokes the normal
// versioned loader or creates recovery artifacts.
func TestPhase2WhiteboxRawDiagnoseIsReadOnlyForUnsupportedInputs(t *testing.T) {
	supported := validate.VersionEnvelope{
		Writer:                    persistence.Writer,
		StateSchemaVersion:        encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion,
		DefinitionSource:          validate.CurrentWorkflowDefinitionSource,
		DefinitionDigest:          definition.WorkflowDefinitionDigest,
		PackageDigest:             testkit.HarnessPackageDigest,
	}
	fixtures := []struct {
		name    string
		content string
		assert  func(*testing.T, validate.DiagnoseReport)
	}{
		{
			name:    "legacy summary without envelope",
			content: "{\n  \"status\": \"SEALED\",\n  \"runId\": \"legacy-run\"\n}\n",
			assert: func(t *testing.T, report validate.DiagnoseReport) {
				if !report.JSONReadable || report.Integrity != "unsupported" ||
					report.Summary["status"] != "SEALED" || report.Summary["runId"] != "legacy-run" ||
					len(report.DetectedVersions) != 0 ||
					report.Recommendation != validate.UnsupportedRunVersionCode+": state has no version envelope; rebuild it with the owning writer" {
					t.Fatalf("legacy diagnose report = %+v", report)
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
			assert: func(t *testing.T, report validate.DiagnoseReport) {
				wantDetected := map[string]any{
					"writer":                    "engine",
					"stateSchemaVersion":        "1",
					"workflowDefinitionVersion": "old",
					"definitionSource":          "definitions/workflow.json",
					"definitionDigest":          "sha256:old",
					"packageDigest":             "sha256:old",
				}
				if !report.JSONReadable || report.Integrity != "readable" || !reflect.DeepEqual(report.DetectedVersions, wantDetected) ||
					report.Summary["status"] != "ACTIVE" ||
					report.Recommendation != (&persistence.UnsupportedRunVersionError{
						Field: "workflowDefinitionVersion", Expected: supported.WorkflowDefinitionVersion, Observed: "old",
					}).Error()+"; rebuild it with the owning writer" {
					t.Fatalf("mismatched diagnose report = %+v", report)
				}
			},
		},
		{
			name:    "malformed JSON",
			content: "{\"writer\":\"engine\"",
			assert: func(t *testing.T, report validate.DiagnoseReport) {
				if report.JSONReadable || report.Integrity != "unknown" || report.DetectedVersions != nil || report.Summary != nil ||
					report.Recommendation != "rebuild the state with the owning writer" {
					t.Fatalf("malformed diagnose report = %+v", report)
				}
			},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, []byte(fixture.content), 0o640); err != nil {
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
				t.Fatalf("diagnose identity = path %q supported %+v", report.Path, report.Supported)
			}
			fixture.assert(t, report)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read diagnosed fixture: %v", err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat diagnosed fixture: %v", err)
			}
			if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() ||
				beforeInfo.Size() != afterInfo.Size() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatalf("raw diagnose changed its input: before=%+v after=%+v", beforeInfo, afterInfo)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read diagnose directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "state.json" {
				t.Fatalf("raw diagnose created artifacts: %v", entries)
			}
		})
	}
}
