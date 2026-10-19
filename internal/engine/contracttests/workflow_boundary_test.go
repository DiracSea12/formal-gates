package contracttests

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/settings"
)

func configureLightweight(t *testing.T, root string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "记录", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "记录", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: protocol.FlowNodeIDs()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "记录", nil); err != nil {
		t.Fatal(err)
	}
}

// workflowIntakeReceipt is intentionally independent of the façade package's test
// fixture.  The revisions are opaque bindings here because this test does not
// use ArtifactRoot; artifact byte recomputation is covered by the public
// façade's ArtifactRoot path.
func workflowIntakeReceipt() facade.IntakeConfirmationReceipt {
	return facade.IntakeConfirmationReceipt{
		Source:              facade.DefaultIntakeSource,
		Authority:           facade.DefaultIntakeAuthority,
		Transport:           facade.DefaultIntakeTransport,
		RequirementSource:   "requirements.md",
		RequirementRevision: "req-r1",
		Artifacts: []facade.IntakeArtifact{
			{Path: "requirements.md", Revision: "req-r1"},
			{Path: "design.md", Revision: "sol-r1"},
		},
		SolutionRevision: "sol-r1",
		SolutionDigest:   "sha256:solution-r1",
	}
}

func verticalStartRequest(runID string, receipt facade.IntakeConfirmationReceipt) facade.StartRequest {
	return facade.StartRequest{
		RunID:                     runID,
		Route:                     "lightweight",
		Provider:                  "engine",
		DefinitionSource:          facade.DefaultDefinitionSource,
		DefinitionDigest:          definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt,
	}
}

// TestFacadeAdmissionAndPreWriteInputFence proves that runtime identity
// comes from launcher admission, while mismatched caller fields, unsupported
// routes, invalid run IDs, and negative capacity are rejected before the
// engine namespace is created.
func TestFacadeAdmissionAndPreWriteInputFence(t *testing.T) {
	root := t.TempDir()
	configureLightweight(t, root)
	admission := fixtureAdmission(t, root, "sha256:admitted", "target-qa")
	_, run, err := facade.Start(facade.StartOptions{
		Root: root, Request: verticalStartRequest("admission-ok", workflowIntakeReceipt()), Admission: admission,
	})
	if err != nil {
		t.Fatalf("admitted start: %v", err)
	}
	if run.Runtime != facade.RuntimeEngine || run.Status != "ACTIVE" || run.Envelope.Writer != persistence.Writer ||
		run.Envelope.StateSchemaVersion != encoder.StateSchemaVersion ||
		run.Envelope.WorkflowDefinitionVersion != definition.WorkflowDefinitionVersion ||
		run.Envelope.PackageDigest != admission.PackageDigest ||
		run.Envelope.InstalledTargetIdentity != admission.InstalledTargetIdentity {
		t.Fatalf("admission-derived run envelope = %+v", run)
	}

	rows := []struct {
		name   string
		mutate func(facade.StartRequest, *facade.StartOptions) facade.StartRequest
		want   string
	}{
		{name: "package digest mismatch", mutate: func(req facade.StartRequest, _ *facade.StartOptions) facade.StartRequest {
			req.PackageDigest = "sha256:caller-selected"
			return req
		}, want: facade.UnregisteredInstall},
		{name: "target identity mismatch", mutate: func(req facade.StartRequest, _ *facade.StartOptions) facade.StartRequest {
			req.InstalledTargetIdentity = "caller-selected-target"
			return req
		}, want: facade.UnregisteredInstall},
		{name: "invalid run id", mutate: func(req facade.StartRequest, _ *facade.StartOptions) facade.StartRequest {
			req.RunID = "bad/run"
			return req
		}, want: "run id"},
		{name: "unsupported route", mutate: func(req facade.StartRequest, _ *facade.StartOptions) facade.StartRequest {
			req.Route = "unrecognized"
			return req
		}, want: "differs from configuration route"},
		{name: "negative capacity", mutate: func(req facade.StartRequest, options *facade.StartOptions) facade.StartRequest {
			options.Capacity = -1
			return req
		}, want: "capacity must be non-negative"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			caseRoot := t.TempDir()
			configureLightweight(t, caseRoot)
			options := facade.StartOptions{Root: caseRoot, Admission: fixtureAdmission(t, caseRoot, admission.PackageDigest, admission.InstalledTargetIdentity)}
			request := row.mutate(verticalStartRequest("reject-"+strings.ReplaceAll(row.name, " ", "-"), workflowIntakeReceipt()), &options)
			options.Request = request
			if _, _, err := facade.Start(options); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(row.want)) {
				t.Fatalf("start error = %v, want substring %q", err, row.want)
			}
			if _, statErr := os.Stat(filepath.Join(caseRoot, facade.EngineNamespace)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected start created engine namespace: %v", statErr)
			}
		})
	}
}

// TestProtocolIntakeReceiptIsExactlyOnce checks that the first-drive
// intake projection is bound to the typed confirmation, that exact replay is
// revision-idempotent, and that a changed receipt is rejected without bytes
// changing.
func TestProtocolIntakeReceiptIsExactlyOnce(t *testing.T) {
	root := t.TempDir()
	store, err := persistence.NewStore(root, persistence.Config{PackageDigest: "sha256:vertical-package"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	fp := protocol.Observation{}
	engine, err := protocol.New(store, protocol.Config{InternalSteps: testkit.InternalSteps(), Definition: compiled, Registry: definition.Registry(), Capacity: 0})
	if err != nil {
		t.Fatalf("new protocol engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	confirmation := workflowIntakeReceipt()
	if err := engine.InitWithMetadata(view, "engine", fp, "vertical-receipt", "lightweight", &confirmation, "", "", nil); err != nil {
		t.Fatalf("init with confirmation: %v", err)
	}
	digest, err := facade.IntakeDigest(confirmation)
	if err != nil {
		t.Fatalf("intake digest: %v", err)
	}
	receipt := protocol.IntakeReceipt{Confirmation: confirmation, IntakeDigest: digest}
	firstRevision, err := engine.RecordIntakeReceipt(receipt, fp)
	if err != nil || firstRevision != 2 {
		t.Fatalf("first intake receipt revision=%d err=%v, want revision 2", firstRevision, err)
	}
	first, err := engine.Load()
	if err != nil {
		t.Fatalf("load first receipt: %v", err)
	}
	if first.State.IntakeReceipt == nil || first.State.IntakeReceipt.IntakeDigest != digest || first.State.IntakeReceipt.Revision != 2 {
		t.Fatalf("persisted intake receipt = %+v", first.State.IntakeReceipt)
	}
	durableFirst, err := store.Load()
	if err != nil {
		t.Fatalf("load durable first receipt: %v", err)
	}
	beforeReplay := append([]byte(nil), durableFirst.Content...)
	replayedRevision, err := engine.RecordIntakeReceipt(receipt, fp)
	if err != nil || replayedRevision != first.Revision {
		t.Fatalf("exact receipt replay revision=%d err=%v, want %d/no error", replayedRevision, err, first.Revision)
	}
	replayed, err := engine.Load()
	if err != nil {
		t.Fatalf("load replayed receipt: %v", err)
	}
	durableReplay, err := store.Load()
	if err != nil {
		t.Fatalf("load durable replayed receipt: %v", err)
	}
	if replayed.Revision != first.Revision || !bytes.Equal(beforeReplay, durableReplay.Content) {
		t.Fatal("exact receipt replay changed revision or state bytes")
	}
	changed := receipt
	changed.IntakeDigest = "sha256:changed-intake"
	if _, err := engine.RecordIntakeReceipt(changed, fp); err == nil || !strings.Contains(err.Error(), protocol.CodeDuplicateEventMismatch) {
		t.Fatalf("changed receipt error = %v, want %s", err, protocol.CodeDuplicateEventMismatch)
	}
	afterReject, err := engine.Load()
	if err != nil {
		t.Fatalf("load after changed receipt: %v", err)
	}
	durableAfterReject, err := store.Load()
	if err != nil {
		t.Fatalf("load durable after changed receipt: %v", err)
	}
	if afterReject.Revision != first.Revision || !bytes.Equal(beforeReplay, durableAfterReject.Content) {
		t.Fatal("rejected changed receipt changed revision or state bytes")
	}
}

// TestFacadeOpenTerminalSummaryReplayIsReadOnly proves that a completed
// engine run can be reopened after active state cleanup from its immutable
// summary, and that Show/Status/Next do not rewrite terminal bytes.
func TestFacadeOpenTerminalSummaryReplayIsReadOnly(t *testing.T) {
	root := t.TempDir()
	configureLightweight(t, root)
	runID := "terminal-open-replay"
	f, _, err := facade.Start(facade.StartOptions{
		Root: root, Request: verticalStartRequest(runID, workflowIntakeReceipt()),
		Admission: fixtureAdmission(t, root, "sha256:pkg", "target"),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	completed, err := f.Drive()
	if err != nil || completed.Status != "COMPLETE" || !completed.Unverified || completed.Next.Kind != decision.KindComplete {
		t.Fatalf("drive terminal projection = %+v err=%v", completed, err)
	}
	statePath := filepath.Join(root, facade.EngineNamespace, runID, "state.json")
	summaryPath := filepath.Join(root, facade.EngineNamespace, runID, "terminal-summary.json")
	summaryBefore, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read terminal summary: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("terminal drive retained active state: %v", err)
	}
	opened, err := facade.Open(root, runID)
	if err != nil {
		t.Fatalf("open summary-only run: %v", err)
	}
	for _, query := range []func() (facade.Run, error){opened.Show, opened.Status} {
		view, err := query()
		if err != nil || view.Status != "COMPLETE" || view.Next.Kind != decision.KindComplete || !view.Unverified || view.Revision != completed.Revision {
			t.Fatalf("summary-only query = %+v err=%v", view, err)
		}
	}
	next, err := opened.Next()
	if err != nil || next.Kind != decision.KindComplete || next.Complete == nil {
		t.Fatalf("summary-only next = %+v err=%v", next, err)
	}
	summaryAfter, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read terminal summary after replay: %v", err)
	}
	if !bytes.Equal(summaryBefore, summaryAfter) {
		t.Fatal("summary-only terminal queries rewrote immutable summary bytes")
	}
}

// TestFacadeDiagnoseRawVersionBarrierIsReadOnly checks the deliberately
// minimal diagnostic parser for malformed JSON and a readable but unsupported
// envelope, including exact detected fields and no file mutation.
func TestFacadeDiagnoseRawVersionBarrierIsReadOnly(t *testing.T) {
	root := t.TempDir()
	malformed := filepath.Join(root, "malformed-state.json")
	if err := os.WriteFile(malformed, []byte(`{"writer":"engine",`), 0o600); err != nil {
		t.Fatal(err)
	}
	malformedBefore, _ := os.ReadFile(malformed)
	report, err := facade.Diagnose(malformed, "sha256:pkg", "target")
	if err != nil || report.JSONReadable || report.Integrity != "unknown" || report.Recommendation != "rebuild the state with the owning writer" {
		t.Fatalf("malformed diagnose = %+v err=%v", report, err)
	}
	malformedAfter, _ := os.ReadFile(malformed)
	if !bytes.Equal(malformedBefore, malformedAfter) {
		t.Fatal("malformed diagnose changed input bytes")
	}

	unsupportedPath := filepath.Join(root, "unsupported-state.json")
	unsupportedJSON := `{"writer":"legacy","stateSchemaVersion":"0","workflowDefinitionVersion":"old","definitionSource":"definitions/old.json","definitionDigest":"sha256:old","packageDigest":"sha256:oldpkg","installedTargetIdentity":"old-target"}`
	if err := os.WriteFile(unsupportedPath, []byte(unsupportedJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	unsupportedBefore, _ := os.ReadFile(unsupportedPath)
	report, err = facade.Diagnose(unsupportedPath, "sha256:pkg", "target")
	if err != nil || !report.JSONReadable || report.Integrity != "unsupported" {
		t.Fatalf("unsupported diagnose = %+v err=%v", report, err)
	}
	wantDetected := map[string]any{"writer": "legacy", "stateSchemaVersion": "0", "workflowDefinitionVersion": "old", "definitionSource": "definitions/old.json", "definitionDigest": "sha256:old", "packageDigest": "sha256:oldpkg", "installedTargetIdentity": "old-target"}
	if !reflect.DeepEqual(report.DetectedVersions, wantDetected) || !strings.Contains(report.Recommendation, persistence.UnsupportedRunVersionCode) {
		t.Fatalf("unsupported detection = %+v recommendation=%q", report.DetectedVersions, report.Recommendation)
	}
	unsupportedAfter, _ := os.ReadFile(unsupportedPath)
	if !bytes.Equal(unsupportedBefore, unsupportedAfter) {
		t.Fatal("unsupported diagnose changed input bytes")
	}
}

// TestTestkitNextResultSequenceHasExactlySixTypedBoundaries exercises
// the test-only sequence harness and checks each closed NextResult kind has
// exactly the payload branch associated with that kind.
func TestTestkitNextResultSequenceHasExactlySixTypedBoundaries(t *testing.T) {
	report, err := testkit.RunHarness(testkit.HarnessOptions{ProjectRoot: t.TempDir(), Scenario: "next-sequence"})
	if err != nil {
		t.Fatalf("next-sequence harness: %v", err)
	}
	want := []decision.Kind{decision.KindReady, decision.KindAsk, decision.KindWait, decision.KindHostAction, decision.KindOperator, decision.KindComplete}
	if report.Status != "PASS" || report.Phase != "decision-regression" || len(report.Next) != len(want) {
		t.Fatalf("sequence report header/length = %+v", report)
	}
	for i, wantKind := range want {
		if report.Next[i].Kind != wantKind || report.Next[i].Payload == nil {
			t.Fatalf("sequence[%d] = %+v, want kind %s with payload", i, report.Next[i], wantKind)
		}
	}
}

// TestSubmitFencesUnknownKindAndProviderBeforeWrite proves Submit is a
// closed typed event channel: free kinds and mismatched providers are rejected
// before an event ledger or revision change.
func TestSubmitFencesUnknownKindAndProviderBeforeWrite(t *testing.T) {
	fixture, err := testkit.NewProtocolFixture(t.TempDir())
	if err != nil {
		t.Fatalf("new protocol fixture: %v", err)
	}
	if _, err := fixture.PrepareReady(); err != nil {
		t.Fatalf("prepare ready: %v", err)
	}
	baseline, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	observedInput, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatalf("observe fingerprint: %v", err)
	}
	rogue := protocol.Event{ID: protocol.EventID("qa-rogue"), Kind: protocol.EventKind("USER_WRITE")}
	if _, err := fixture.Engine.Submit(rogue, observedInput); err == nil || !strings.Contains(err.Error(), protocol.CodeUnknownEventKind) {
		t.Fatalf("unknown event error = %v, want %s", err, protocol.CodeUnknownEventKind)
	}
	worker, err := protocol.NewWorkerResultEvent("qa-provider-mismatch", "act:review/review.worker", "wrong-provider", protocol.OutcomePass, "sha256:payload", "")
	if err != nil {
		t.Fatalf("construct provider mismatch event: %v", err)
	}
	if _, err := fixture.Engine.Submit(worker, observedInput); err == nil || !strings.Contains(err.Error(), protocol.CodeProviderMismatch) {
		t.Fatalf("provider mismatch error = %v, want %s", err, protocol.CodeProviderMismatch)
	}
	after, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("load after rejected submits: %v", err)
	}
	if after.Revision != baseline.Revision || !reflect.DeepEqual(after.State, baseline.State) {
		t.Fatal("rejected typed submits changed engine revision or state")
	}
}
