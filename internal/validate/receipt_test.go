package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptCaptureSavesLifecycleEvent(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "capture.json")
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	event, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "codex",
		Event:    "SubagentStart",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `"}`),
	})
	if !result.OK() {
		t.Fatalf("expected capture to pass, got %#v", result.Failures)
	}
	if event.NormalizedEvent != "subagent_start" {
		t.Fatalf("unexpected normalized event: %#v", event)
	}
	if !strings.HasPrefix(event.EventArtifact, ".claude/gates/runs/wf/restricted/proofs/events/") {
		t.Fatalf("unexpected event artifact path: %q", event.EventArtifact)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(event.EventArtifact))); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptCaptureAutoSelectsRunLocalDispatch(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "active-run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := ".claude/gates/runs/active-run/restricted/review.json"
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	event, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)})
	if !result.OK() {
		t.Fatalf("run-local capture failed: %#v", result.Failures)
	}
	if !strings.HasPrefix(event.EventArtifact, ".claude/gates/runs/active-run/restricted/proofs/events/") {
		t.Fatalf("capture escaped correlated run: %q", event.EventArtifact)
	}
}

func TestReceiptCaptureRejectsExplicitRunConflict(t *testing.T) {
	dir := t.TempDir()
	runA := filepath.Join(dir, ".claude", "gates", "runs", "run-a")
	runB := filepath.Join(dir, ".claude", "gates", "runs", "run-b")
	for _, runDir := range []string{runA, runB} {
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runA, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: ".claude/gates/runs/run-a/restricted/review.json"})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, RunDir: runB, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)}); result.OK() || !strings.Contains(resultSummary(result), "conflicts with dispatch correlation") {
		t.Fatalf("explicit run conflict was accepted: %#v", result.Failures)
	}
	if events, err := filepath.Glob(filepath.Join(runB, "restricted", "proofs", "events", "*.json")); err != nil || len(events) != 0 {
		t.Fatalf("rejected conflict wrote an event: events=%v err=%v", events, err)
	}
}

func TestReceiptCaptureDispatchCorrelationMatrix(t *testing.T) {
	tests := []struct {
		name       string
		idSource   string
		pathSource string
		accept     bool
	}{
		{name: "correct both", idSource: "a", pathSource: "a", accept: true},
		{name: "id only", idSource: "a", accept: true},
		{name: "path only", pathSource: "a", accept: true},
		{name: "wrong id correct path", idSource: "missing", pathSource: "a"},
		{name: "correct id wrong path", idSource: "a", pathSource: "missing"},
		{name: "nonexistent id", idSource: "missing"},
		{name: "nonexistent path", pathSource: "missing"},
		{name: "crossed parallel dispatches", idSource: "a", pathSource: "b"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			aArtifact := defaultReceiptArtifact(t, dir, "wf", "a.json")
			bArtifact := defaultReceiptArtifact(t, dir, "wf", "b.json")
			a, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: aArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			b, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "architecture-health-gate", Artifact: bArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			ids := map[string]string{"a": a.DispatchID, "b": b.DispatchID, "missing": "missing-dispatch"}
			paths := map[string]string{
				"a":       a.DispatchRegistrationArtifact,
				"b":       b.DispatchRegistrationArtifact,
				"missing": filepath.ToSlash(filepath.Join(filepath.Dir(a.DispatchRegistrationArtifact), "missing.json")),
			}
			payload := map[string]any{
				"workflowId": "wf",
				"gate":       "complexity-gate",
				"subagentId": "subagent-1",
			}
			if id := ids[test.idSource]; id != "" {
				payload["dispatchId"] = id
			}
			if path := paths[test.pathSource]; path != "" {
				payload["dispatchRegistrationArtifact"] = path
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			event, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: raw})
			if !test.accept {
				if result.OK() {
					t.Fatalf("expected correlation rejection, event=%#v", event)
				}
				return
			}
			if !result.OK() {
				t.Fatalf("expected capture to pass, got %#v", result.Failures)
			}
			record, ok := decodeReceiptEvent(resolvePath(dir, event.EventArtifact))
			if !ok {
				t.Fatal("captured event could not be decoded")
			}
			if record.DispatchID != a.DispatchID || record.DispatchRegistrationArtifact != a.DispatchRegistrationArtifact {
				t.Fatalf("event did not store canonical dispatch correlation: %#v", record)
			}
		})
	}
}

func TestReceiptFinalizeRejectsMismatchedPreexistingDispatchEvents(t *testing.T) {
	tests := []struct {
		name      string
		startID   string
		startPath string
		stopID    string
		stopPath  string
	}{
		{name: "start wrong id", startID: "b", startPath: "a", stopID: "a", stopPath: "a"},
		{name: "start wrong path", startID: "a", startPath: "b", stopID: "a", stopPath: "a"},
		{name: "stop wrong id", startID: "a", startPath: "a", stopID: "b", stopPath: "a"},
		{name: "stop wrong path", startID: "a", startPath: "a", stopID: "a", stopPath: "b"},
		{name: "stop noncanonical path", startID: "a", startPath: "a", stopID: "a", stopPath: "alias"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			aArtifact := defaultReceiptArtifact(t, dir, "wf", "a.json")
			bArtifact := defaultReceiptArtifact(t, dir, "wf", "b.json")
			a, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: aArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			b, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "architecture-health-gate", Artifact: bArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			writeReceiptArtifactFixture(t, dir, aArtifact, "snap")
			ids := map[string]string{"a": a.DispatchID, "b": b.DispatchID}
			paths := map[string]string{"a": a.DispatchRegistrationArtifact, "b": b.DispatchRegistrationArtifact, "alias": "./" + a.DispatchRegistrationArtifact}
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			eventDir := receiptProofDir(dir, runDir, "events")
			for name, event := range map[string]receiptEventRecord{
				"start.json": {Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", NormalizedEvent: "subagent_start", RawEventName: "SubagentStart", SubagentID: "subagent-1", DispatchID: ids[test.startID], DispatchRegistrationArtifact: paths[test.startPath]},
				"stop.json":  {Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", NormalizedEvent: "subagent_stop", RawEventName: "SubagentStop", SubagentID: "subagent-1", DispatchID: ids[test.stopID], DispatchRegistrationArtifact: paths[test.stopPath]},
			} {
				if err := writeJSON(filepath.Join(eventDir, name), event); err != nil {
					t.Fatal(err)
				}
			}
			if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: aArtifact}); result.OK() || !strings.Contains(result.Failures[0].Message, "matching subagent_start") {
				t.Fatalf("mismatched lifecycle event was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestReceiptFinalizeRejectsStopCapturedBeforeStart(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "strictly after") {
		t.Fatalf("stop-before-start lifecycle was accepted: %#v", result.Failures)
	}
}

func TestReceiptCaptureRejectsUnknownProviderAndEvent(t *testing.T) {
	dir := t.TempDir()
	if _, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "unknown",
		Event:    "SubagentStart",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"dispatch-1"}`),
	}); result.OK() {
		t.Fatal("expected unknown provider to fail")
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "codex",
		Event:    "TaskStarted",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"dispatch-1"}`),
	}); result.OK() {
		t.Fatal("expected unknown event to fail")
	}
}

func TestReceiptFinalizeAndValidate(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "complexity.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree:       dir,
		Provider:       "codex",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
		Gate:           "complexity-gate",
		Artifact:       artifact,
	})
	if !result.OK() {
		t.Fatalf("expected dispatch registration to pass, got %#v", result.Failures)
	}
	if !strings.HasPrefix(dispatch.DispatchRegistrationArtifact, ".claude/gates/runs/wf/restricted/proofs/dispatch/") {
		t.Fatalf("omitted --run-dir registered outside the default run: %#v", dispatch)
	}
	registeredDispatch, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if !ok {
		t.Fatal("cannot decode registered dispatch")
	}
	promptData, err := os.ReadFile(resolvePath(dir, registeredDispatch.PromptArtifact))
	if err != nil {
		t.Fatal(err)
	}
	var markerResult Result
	if !validateDispatchStaticMarker(string(promptData), &markerResult, "prompt") {
		t.Fatalf("receipt register did not write a valid static PASS binding: %#v", markerResult.Failures)
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "contextBundle", "contextSha256", "promptArtifact", "promptSha256", "status",
	})
	capturePayload := `{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(capturePayload)}); !result.OK() {
		t.Fatalf("expected start capture to pass, got %#v", result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: []byte(capturePayload)}); !result.OK() {
		t.Fatalf("expected stop capture to pass, got %#v", result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if !result.OK() {
		t.Fatalf("expected receipt finalize to pass, got %#v", result.Failures)
	}
	if !strings.HasPrefix(receipt.ReceiptArtifact, ".claude/gates/runs/wf/restricted/proofs/") {
		t.Fatalf("omitted --run-dir finalized outside the default run: %#v", receipt)
	}
	if isDir(filepath.Join(dir, ".claude", "gates", "proofs")) {
		t.Fatal("omitted --run-dir wrote repository-level receipt proofs")
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "contextBundle", "contextSha256", "promptArtifact", "promptSha256", "receiptArtifact", "status",
	})
	assertTypedWireRoundTrip(t, resolvePath(dir, receipt.ReceiptArtifact), &reviewerProofReceipt{}, []string{
		"proofVersion", "provider", "workflowId", "changeSnapshot", "gate", "stage", "dispatchId", "dispatchRegistrationArtifact", "dispatchRegistrationSha256", "subagentId", "normalizedEvents", "startEventArtifact", "startEventSha256", "stopEventArtifact", "stopEventSha256", "reviewArtifact", "reviewArtifactSha256", "promptArtifact", "promptSha256",
	})
	result = ReceiptValidate(ReceiptValidateOptions{
		Worktree:       dir,
		Receipt:        receipt.ReceiptArtifact,
		Artifact:       artifact,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected receipt validation to pass, got %#v", result.Failures)
	}
	receiptData, err := os.ReadFile(resolvePath(dir, receipt.ReceiptArtifact))
	if err != nil {
		t.Fatal(err)
	}
	var bound reviewerProofReceipt
	if err := strictContractJSON(receiptData, &bound); err != nil {
		t.Fatal(err)
	}
	promptPath := resolvePath(dir, bound.PromptArtifact)
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, promptPath, string(promptBytes)+"Directed focus: repaired path\n")
	if invalid := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"}); invalid.OK() || !strings.Contains(resultSummary(invalid), "final-send prompt") {
		t.Fatalf("changed final-send prompt passed receipt validation: %#v", invalid.Failures)
	}
	mustWrite(t, promptPath, string(promptBytes))
	result = ReceiptValidate(ReceiptValidateOptions{
		Worktree:       dir,
		Receipt:        receipt.ReceiptArtifact,
		Artifact:       artifact,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "other-snapshot",
	})
	if result.OK() {
		t.Fatal("expected snapshot mismatch to fail")
	}
	receiptPath := resolvePath(dir, receipt.ReceiptArtifact)
	receiptData, err = os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receiptWire reviewerProofReceipt
	if err := strictContractJSON(receiptData, &receiptWire); err != nil {
		t.Fatal(err)
	}
	receiptWire.SubagentID = "different-subagent"
	if err := writeJSON(receiptPath, receiptWire); err != nil {
		t.Fatal(err)
	}
	result = ReceiptValidate(ReceiptValidateOptions{
		Worktree:       dir,
		Receipt:        receipt.ReceiptArtifact,
		Artifact:       artifact,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() || !strings.Contains(resultSummary(result), "subagent IDs do not match") {
		t.Fatalf("typed receipt subagent binding drift was accepted: %#v", result.Failures)
	}
}

func TestReceiptFinalizeAndValidateQADesignDocument(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "qa-cases.md")
	options := ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact}
	dispatch, result := registerReceiptFixture(t, options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"design-snap","gate":"qa-test-gate","stage":"Design","subagentId":"designer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	mustWrite(t, resolvePath(dir, artifact), "# Cases\n\nCase ID: P2-001\n\nOracle: expected behavior\n")
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact})
	if !result.OK() {
		t.Fatalf("Design document finalization failed: %#v", result.Failures)
	}
	result = ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "qa-test-gate", Stage: "Design", WorkflowID: "wf", ChangeSnapshot: "design-snap"})
	if !result.OK() {
		t.Fatalf("Design document receipt validation failed: %#v", result.Failures)
	}
	dispatchData, err := os.ReadFile(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if err != nil {
		t.Fatal(err)
	}
	var registration dispatchRegistration
	if err := strictContractJSON(dispatchData, &registration); err != nil {
		t.Fatal(err)
	}
	if registration.PromptArtifact != "" || registration.PromptSha256 != "" {
		t.Fatalf("QA Design was incorrectly forced to bind a reviewer prompt: %#v", registration)
	}
}

func TestReceiptRegisterDoesNotRequirePromptForQAExecutionLifecycle(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "qa-execution.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Execution", Artifact: artifact})
	if options.Prompt != "" {
		t.Fatal("QA Execution fixture was given a reviewer prompt")
	}
	if _, result := ReceiptRegisterDispatch(options); !result.OK() {
		t.Fatalf("QA Execution lifecycle was incorrectly forced to bind a prompt: %#v", result.Failures)
	}
}

func TestReceiptFinalizeRejectsPromptChangedAfterRegistration(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	registration, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + registration.DispatchID + `","dispatchRegistrationArtifact":"` + registration.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	promptPath, err := safeEvidencePath(filepath.Join(dir, ".claude", "gates", "runs", "wf"), options.Prompt)
	if err != nil {
		t.Fatal(err)
	}
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, promptPath, string(promptBytes)+"Directed focus: repaired path\n")
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "final-send prompt") {
		t.Fatalf("changed prompt passed receipt finalization: %#v", result.Failures)
	}
}

func TestReceiptFinalizeMissingLifecycleIsUnproven(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "complexity.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree:       dir,
		Provider:       "codex",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
		Gate:           "complexity-gate",
		Artifact:       artifact,
	})
	if !result.OK() {
		t.Fatalf("expected dispatch registration to pass, got %#v", result.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)}); !result.OK() {
		t.Fatalf("expected start capture to pass, got %#v", result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	_, result = ReceiptFinalize(ReceiptFinalizeOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if result.OK() {
		t.Fatal("expected missing stop event to be unproven")
	}
	if len(result.Failures) == 0 || !strings.Contains(result.Failures[0].Message, "UNPROVEN") {
		t.Fatalf("expected UNPROVEN failure, got %#v", result.Failures)
	}
}

func TestReceiptRegisterReservesAbsentArtifactPath(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	dispatch, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatalf("expected absent output registration to pass, got %#v", result.Failures)
	}
	if _, err := os.Stat(resolvePath(dir, artifact)); !os.IsNotExist(err) {
		t.Fatalf("registration must not create reviewer output, err=%v", err)
	}
	if dispatch.DispatchRegistrationStatusText != "open" {
		t.Fatalf("unexpected registration: %#v", dispatch)
	}
	beforeDispatch, _ := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if duplicate, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(result.Failures[0].Message, "already reserved") {
		afterDispatch, _ := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
		t.Fatalf("expected duplicate open reservation rejection, got registration=%#v failures=%#v before=%#v after=%#v", duplicate, result.Failures, beforeDispatch, afterDispatch)
	}
}

func TestReceiptRegisterRebindsUnstartedDispatchToChangedPrompt(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	first, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(runDir, filepath.FromSlash(options.Prompt))
	changedPrompt := strings.Replace(finalSendPromptForOptions(runDir, options), "closed schema-version-2", "closed  schema-version-2", 1)
	mustWrite(t, promptPath, resealDispatchPrompt(changedPrompt))
	rebound, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatalf("expected unstarted dispatch to be rebound, got %#v", result.Failures)
	}
	if rebound.DispatchRegistrationStatusText != "rebound" || rebound.DispatchID == first.DispatchID || rebound.DispatchRegistrationArtifact != first.DispatchRegistrationArtifact {
		t.Fatalf("unexpected rebound registration: first=%#v rebound=%#v", first, rebound)
	}
}

func TestReceiptRegisterRejectsRebindAfterLifecycleStart(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	dispatch, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "has started") {
		t.Fatalf("started dispatch was rebound: %#v", result.Failures)
	}
}

func TestReceiptRegisterRejectsPromptForDifferentGate(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.Prompt)), finalSendPromptFixture(dir, "architecture-health-gate", artifact))
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "does not match the registered gate") {
		t.Fatalf("prompt for a different gate was accepted: %#v", result.Failures)
	}
}

func TestReceiptCarryUsesArbiterDispatchRole(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact})
	if !result.OK() {
		t.Fatalf("Carry registration rejected its documented dispatch role: %#v", result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"target","gate":"qa-test-gate","stage":"Carry","subagentId":"arbiter","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	carry := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	writeEnvelopeTest(t, resolvePath(dir, artifact), carry.Envelope, carry.Payload)
	artifactBytes, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictJSON(artifactBytes, &envelope); err != nil {
		t.Fatalf("Carry fixture is not a valid evidence envelope: %v", err)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact}); !result.OK() {
		t.Fatalf("Carry finalization rejected its documented dispatch role: %#v", result.Failures)
	}
}

func TestReceiptCarryRejectsWrongDispatchRole(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact})
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.Prompt)), finalSendPromptFixture(dir, "qa-test-gate", artifact))
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "registered gate and stage role") {
		t.Fatalf("Carry prompt with the wrong dispatch role was accepted: %#v", result.Failures)
	}
}

func TestReceiptRegisterValidatesContextBundleBeforeReservation(t *testing.T) {
	for _, kind := range []string{"valid", "workflow", "snapshot", "empty", "duplicate", "missing", "tampered", "escape", "outside-input", "outside-bundle"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			inputPath := filepath.Join(runDir, "restricted", "input.txt")
			mustWrite(t, inputPath, "input\n")
			ref := EvidenceRef{Path: "restricted/input.txt", SHA256: sha256File(inputPath)}
			bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{ref}}
			bundleName := "restricted/bundle.json"
			switch kind {
			case "workflow":
				bundle.WorkflowID = "other"
			case "snapshot":
				bundle.ChangeSnapshot = "other"
			case "empty":
				bundle.Inputs = []EvidenceRef{}
			case "duplicate":
				bundle.Inputs = append(bundle.Inputs, ref)
			case "missing":
				bundle.Inputs[0] = EvidenceRef{Path: "restricted/missing.txt", SHA256: strings.Repeat("0", 64)}
			case "tampered":
				mustWrite(t, inputPath, "tampered\n")
			case "escape":
				bundle.Inputs[0].Path = "../outside.txt"
			case "outside-input":
				mustWrite(t, filepath.Join(runDir, "outside.txt"), "{}")
				bundle.Inputs[0] = testRef(t, runDir, "outside.txt")
			case "outside-bundle":
				bundleName = "bundle.json"
			}
			writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(bundleName)), bundle)
			artifact := relativePath(dir, filepath.Join(runDir, "restricted", "review.json"))
			_, result := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact, ContextBundle: bundleName}))
			if result.OK() != (kind == "valid") {
				t.Fatalf("unexpected result: %#v", result.Failures)
			}
			if _, err := os.Stat(resolvePath(dir, artifact)); !os.IsNotExist(err) {
				t.Fatalf("registration created reviewer output: %v", err)
			}
			entries, err := os.ReadDir(receiptProofDir(dir, runDir, "dispatch"))
			if kind == "valid" {
				if err != nil || len(entries) != 1 {
					t.Fatalf("valid bundle did not register one dispatch: entries=%v err=%v", entries, err)
				}
			} else if err == nil && len(entries) != 0 {
				t.Fatalf("invalid bundle registered a dispatch: %v", entries)
			}
		})
	}
}

func TestReceiptRegisterLeavesRestrictedContextAvailableToCarryArbitration(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	mustWrite(t, filepath.Join(runDir, "restricted", "repair-chain.json"), "{}\n")
	bundleName := "restricted/carry-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(bundleName)), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "target", Inputs: []EvidenceRef{testRef(t, runDir, "restricted/repair-chain.json")}})
	artifact := relativePath(dir, filepath.Join(runDir, "restricted", "carry-review.json"))
	_, result := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact, ContextBundle: bundleName}))
	if !result.OK() {
		t.Fatalf("Carry arbitration lost access to its restricted repair chain: %#v", result.Failures)
	}
}

func TestReceiptRegisterRejectsInvalidComplexityContextBeforeDispatch(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	statistics := filepath.Join(runDir, "restricted", "statistics.json")
	writeJSONTest(t, statistics, ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}})
	bundleName := "restricted/complexity-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(bundleName)), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, runDir, "restricted/statistics.json")}})
	artifact := relativePath(dir, filepath.Join(runDir, "restricted", "review.json"))
	_, result := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact, ContextBundle: bundleName}))
	if result.OK() || !strings.Contains(resultSummary(result), "must not include development-time budget or statistics schema fields") {
		t.Fatalf("invalid complexity dispatch context was accepted: %#v", result.Failures)
	}
	entries, err := os.ReadDir(receiptProofDir(dir, runDir, "dispatch"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("failed static validation still reserved a dispatch: %v", entries)
	}
}

func TestReceiptRegisterChecksEveryDispatchRouteField(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string, ReceiptRegisterOptions) string
		message string
	}{
		{name: "worktree", mutate: func(prompt string, options ReceiptRegisterOptions) string {
			return strings.Replace(prompt, "Worktree: "+options.Worktree, "Worktree: "+filepath.Join(options.Worktree, "other"), 1)
		}, message: "Worktree does not match"},
		{name: "snapshot", mutate: func(prompt string, _ ReceiptRegisterOptions) string {
			return strings.Replace(prompt, "Base commit or snapshot: snap", "Base commit or snapshot: other", 1)
		}, message: "does not match --change-snapshot"},
		{name: "output", mutate: func(prompt string, _ ReceiptRegisterOptions) string {
			return strings.Replace(prompt, "Output path: ", "Output path: other-", 1)
		}, message: "Output path does not match"},
		{name: "role", mutate: func(prompt string, _ ReceiptRegisterOptions) string {
			return strings.Replace(prompt, "COMPLEXITY_REVIEW", "ARCHITECTURE_REVIEW", 1)
		}, message: "Output format does not match"},
		{name: "context", mutate: func(prompt string, _ ReceiptRegisterOptions) string {
			return strings.Replace(prompt, "contextBundle=restricted/receipt-context-bundle.json", "contextBundle=restricted/other.json", 1)
		}, message: "contextBundle binding does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
			options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			promptPath := filepath.Join(runDir, filepath.FromSlash(options.Prompt))
			data, _ := os.ReadFile(promptPath)
			mustWrite(t, promptPath, resealDispatchPrompt(test.mutate(string(data), options)))
			if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), test.message) {
				t.Fatalf("dispatch route mismatch was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestReceiptFinalizeValidatesReviewerArtifactBeforeLocking(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "invalid-review.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeEnvelopeTest(t, resolvePath(dir, artifact), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "COMPLEXITY_REVIEW", WorkflowID: "wrong", ChangeSnapshot: "wrong", Gate: "architecture-health-gate", Verdict: "PASS"}, ReviewerPayload{ReviewPolicyID: "", Checks: []ReviewCheck{}})
	before, _ := os.ReadFile(resolvePath(dir, artifact))
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "reviewPolicyId") {
		t.Fatalf("invalid reviewer artifact was finalized: %#v", result.Failures)
	}
	after, _ := os.ReadFile(resolvePath(dir, artifact))
	if !bytes.Equal(before, after) {
		t.Fatal("failed finalization rewrote the reviewer artifact")
	}
	dispatchPath := resolvePath(dir, dispatch.DispatchRegistrationArtifact)
	registered, ok := decodeDispatch(dispatchPath)
	if !ok || registered.Status != "open" || registered.ReceiptArtifact != "" {
		t.Fatalf("failed finalization locked the dispatch: %#v", registered)
	}
}

func TestReceiptRegisterBlocksFourthFinalizedReviewWithoutUserAuthorization(t *testing.T) {
	dir := t.TempDir()
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	dispatchDir := receiptProofDir(dir, runDir, "dispatch")
	for i := 1; i <= 3; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", i)), dispatchRegistration{
			ProofVersion: 1, DispatchID: fmt.Sprintf("dispatch-%d", i), Provider: "codex",
			WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", i), Gate: "complexity-gate",
			ReviewArtifact:  fmt.Sprintf(".claude/gates/runs/wf/restricted/review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".claude/gates/runs/wf/restricted/proofs/receipt-%d.json", i), Status: "finalized",
		})
	}
	writeJSONTest(t, filepath.Join(dispatchDir, "failed-open.json"), dispatchRegistration{
		ProofVersion: 1, DispatchID: "failed", Provider: "codex", WorkflowID: "wf",
		ChangeSnapshot: "snap-failed", Gate: "complexity-gate",
		ReviewArtifact: ".claude/gates/runs/wf/restricted/review-failed.json", Status: "open",
	})

	options := ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-4",
		Gate: "complexity-gate", Artifact: ".claude/gates/runs/wf/restricted/review-4.json",
	}
	if _, result := registerReceiptFixture(t, options); result.OK() || !strings.Contains(resultSummary(result), "review limit reached") {
		t.Fatalf("fourth finalized review was not blocked: %#v", result.Failures)
	}
	options.UserAuthorizedExtraReview = true
	if _, result := registerReceiptFixture(t, options); !result.OK() {
		t.Fatalf("explicitly authorized extra review was rejected: %#v", result.Failures)
	}
}

func TestReceiptRegisterOpenReservationUsesRemainingReviewCapacity(t *testing.T) {
	dir := t.TempDir()
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	dispatchDir := receiptProofDir(dir, runDir, "dispatch")
	for i := 1; i <= 2; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", i)), dispatchRegistration{
			ProofVersion: 1, DispatchID: fmt.Sprintf("dispatch-%d", i), Provider: "codex",
			WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", i), Gate: "complexity-gate",
			ReviewArtifact:  fmt.Sprintf(".claude/gates/runs/wf/restricted/review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".claude/gates/runs/wf/restricted/proofs/receipt-%d.json", i), Status: "finalized",
		})
	}
	first := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-3",
		Gate: "complexity-gate", Artifact: ".claude/gates/runs/wf/restricted/review-3.json",
	})
	if _, result := ReceiptRegisterDispatch(first); !result.OK() {
		t.Fatalf("remaining review slot was not reserved: %#v", result.Failures)
	}
	promptPath := filepath.Join(runDir, filepath.FromSlash(first.Prompt))
	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, promptPath, resealDispatchPrompt(strings.Replace(string(promptData), "closed schema-version-2", "closed  schema-version-2", 1)))
	if rebound, result := ReceiptRegisterDispatch(first); !result.OK() || rebound.DispatchRegistrationStatusText != "rebound" {
		t.Fatalf("unstarted attempt could not reuse its reserved slot: registration=%#v failures=%#v", rebound, result.Failures)
	}
	second := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-4",
		Gate: "complexity-gate", Artifact: ".claude/gates/runs/wf/restricted/review-4.json",
	})
	if _, result := ReceiptRegisterDispatch(second); result.OK() || !strings.Contains(resultSummary(result), "review capacity reserved") {
		t.Fatalf("open reservation did not hold the remaining review slot: %#v", result.Failures)
	}
	completed, open, err := reviewReservationCounts(dir, runDir, "wf", "complexity-gate", "")
	if err != nil || completed != 2 || open != 1 {
		t.Fatalf("open attempt changed completed-cycle accounting: completed=%d open=%d err=%v", completed, open, err)
	}
	independent := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-4",
		Gate: "architecture-health-gate", Artifact: ".claude/gates/runs/wf/restricted/architecture-review.json",
	})
	if _, result := ReceiptRegisterDispatch(independent); !result.OK() {
		t.Fatalf("a different gate incorrectly shared review capacity: %#v", result.Failures)
	}
	for i := 1; i <= 3; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("qa-execution-%d.json", i)), dispatchRegistration{
			ProofVersion: 1, DispatchID: fmt.Sprintf("qa-dispatch-%d", i), Provider: "codex",
			WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("qa-snap-%d", i), Gate: "qa-test-gate", Stage: "Execution",
			ReviewArtifact:  fmt.Sprintf(".claude/gates/runs/wf/restricted/qa-review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".claude/gates/runs/wf/restricted/proofs/qa-receipt-%d.json", i), Status: "finalized",
		})
	}
	independentStage := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: ".claude/gates/runs/wf/restricted/carry-review.json",
	})
	if _, result := ReceiptRegisterDispatch(independentStage); !result.OK() {
		t.Fatalf("a different stage incorrectly shared review capacity: %#v", result.Failures)
	}
}

func TestReceiptRegisterConcurrentOpenReservationsShareCapacityAtomically(t *testing.T) {
	for i := 0; i < 25; i++ {
		worktree := t.TempDir()
		runDir := filepath.Join(worktree, ".claude", "gates", "runs", "run")
		dispatchDir := receiptProofDir(worktree, runDir, "dispatch")
		for completed := 1; completed <= 2; completed++ {
			writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", completed)), dispatchRegistration{
				ProofVersion: 1, DispatchID: fmt.Sprintf("dispatch-%d", completed), Provider: "codex",
				WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", completed), Gate: "qa-test-gate", Stage: "Execution",
				ReviewArtifact:  fmt.Sprintf(".claude/gates/runs/run/restricted/review-%d.json", completed),
				ReceiptArtifact: fmt.Sprintf(".claude/gates/runs/run/restricted/proofs/receipt-%d.json", completed), Status: "finalized",
			})
		}
		codes, output := runSimultaneousReceiptRegistrations(t, worktree, runDir, [2]string{
			".claude/gates/runs/run/restricted/third-a.json",
			".claude/gates/runs/run/restricted/third-b.json",
		})
		if !((codes[0] == 0 && codes[1] == 11) || (codes[0] == 11 && codes[1] == 0)) {
			t.Fatalf("pair %d did not reserve exactly one remaining review slot: codes=%v output=%q", i+1, codes, output)
		}
		completed, open, err := reviewReservationCounts(worktree, runDir, "wf", "qa-test-gate", "Execution")
		if err != nil || completed != 2 || open != 1 {
			t.Fatalf("pair %d committed the wrong capacity state: completed=%d open=%d err=%v", i+1, completed, open, err)
		}
	}
}

func TestStoppedUnfinalizedReviewReleasesDispatchCapacityButCannotCommitFourth(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	dispatchDir := receiptProofDir(dir, runDir, "dispatch")
	for i := 1; i <= 2; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", i)), dispatchRegistration{ProofVersion: 1, DispatchID: fmt.Sprintf("done-%d", i), Provider: "codex", WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", i), Gate: "complexity-gate", ReviewArtifact: fmt.Sprintf(".claude/gates/runs/wf/restricted/done-%d.json", i), ReceiptArtifact: fmt.Sprintf(".claude/gates/runs/wf/restricted/proofs/done-%d.json", i), Status: "finalized"})
	}
	artifact := defaultReceiptArtifact(t, dir, "wf", "attempt.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-3", Gate: "complexity-gate", Artifact: artifact})
	dispatch, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap-3","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap-3")
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	other := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-other", Gate: "complexity-gate", Artifact: defaultReceiptArtifact(t, dir, "wf", "other.json")})
	if _, result := ReceiptRegisterDispatch(other); !result.OK() {
		t.Fatalf("stopped failed attempt did not release dispatch capacity: %#v", result.Failures)
	}
	writeJSONTest(t, filepath.Join(dispatchDir, "completed-3.json"), dispatchRegistration{ProofVersion: 1, DispatchID: "done-3", Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-final", Gate: "complexity-gate", ReviewArtifact: ".claude/gates/runs/wf/restricted/done-3.json", ReceiptArtifact: ".claude/gates/runs/wf/restricted/proofs/done-3.json", Status: "finalized"})
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "review limit reached") {
		t.Fatalf("stopped attempt committed a fourth unauthorized review: %#v", result.Failures)
	}
}

func TestReceiptRegisterWithoutRunDirRejectsRepositoryArtifact(t *testing.T) {
	dir := t.TempDir()
	if _, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: "review.json"}); result.OK() || !strings.Contains(resultSummary(result), "outside active run") {
		t.Fatalf("repository-level review output was accepted: %#v", result.Failures)
	}
	if isDir(filepath.Join(dir, ".claude", "gates", "proofs")) {
		t.Fatal("rejected registration wrote repository-level proofs")
	}
}

func TestReceiptRegisterConcurrentReservationProcesses(t *testing.T) {
	for i := 0; i < 100; i++ {
		worktree := t.TempDir()
		runDir := filepath.Join(worktree, ".claude", "gates", "runs", "run")
		artifact := ".claude/gates/runs/run/restricted/shared.json"
		codes, output := runSimultaneousReceiptRegistrations(t, worktree, runDir, [2]string{artifact, artifact})
		if !((codes[0] == 0 && codes[1] == 10) || (codes[0] == 10 && codes[1] == 0)) {
			t.Fatalf("pair %d did not produce exactly one success and one reservation rejection: codes=%v output=%q", i+1, codes, output)
		}
		entries, err := os.ReadDir(receiptProofDir(worktree, runDir, "dispatch"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("pair %d committed %d registrations: err=%v entries=%v", i+1, len(entries), err, entries)
		}
	}

	worktree := t.TempDir()
	runDir := filepath.Join(worktree, ".claude", "gates", "runs", "run")
	codes, output := runSimultaneousReceiptRegistrations(t, worktree, runDir, [2]string{
		".claude/gates/runs/run/restricted/first.json",
		".claude/gates/runs/run/restricted/second.json",
	})
	if codes != [2]int{0, 0} {
		t.Fatalf("distinct simultaneous outputs did not both succeed: codes=%v output=%q", codes, output)
	}
	entries, err := os.ReadDir(receiptProofDir(worktree, runDir, "dispatch"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("distinct simultaneous outputs committed %d registrations: err=%v entries=%v", len(entries), err, entries)
	}
}

func TestReceiptRegisterWriteFailureIsRetryable(t *testing.T) {
	worktree := t.TempDir()
	runDir := filepath.Join(worktree, ".claude", "gates", "runs", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	proofs := filepath.Join(runDir, "restricted", "proofs")
	mustWrite(t, proofs, "blocks dispatch directory creation\n")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Execution", Artifact: ".claude/gates/runs/run/restricted/review.json"})
	if _, result := ReceiptRegisterDispatch(options); result.OK() {
		t.Fatal("expected registration write failure")
	}
	if err := os.Remove(proofs); err != nil {
		t.Fatal(err)
	}
	if _, result := ReceiptRegisterDispatch(options); !result.OK() {
		t.Fatalf("registration write failure was not retryable: %#v", result.Failures)
	}
}

func TestReceiptRegisterTreatsSymlinkOutputAliasesAsOneReservation(t *testing.T) {
	for _, test := range []struct {
		first  string
		second string
	}{
		{first: "outputs/review.json", second: "alias/review.json"},
		{first: "alias/review.json", second: "outputs/review.json"},
	} {
		t.Run(test.first, func(t *testing.T) {
			dir := t.TempDir()
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			if err := os.MkdirAll(filepath.Join(runDir, "restricted", "outputs"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("outputs", filepath.Join(runDir, "restricted", "alias")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: relativePath(dir, filepath.Join(runDir, "restricted", filepath.FromSlash(test.first)))})
			if _, result := ReceiptRegisterDispatch(options); !result.OK() {
				t.Fatalf("first reservation failed: %#v", result.Failures)
			}
			options.Artifact = relativePath(dir, filepath.Join(runDir, "restricted", filepath.FromSlash(test.second)))
			if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "already reserved") {
				t.Fatalf("symlink alias reserved the same output twice: %#v", result.Failures)
			}
		})
	}
}

func TestReceiptRegisterRejectsExistingArtifactAndInvalidSnapshot(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	mustWrite(t, resolvePath(dir, artifact), "stale output\n")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(result.Failures[0].Message, "already exists") {
		t.Fatalf("expected stale output rejection without parsing, got %#v", result.Failures)
	}
	os.Remove(resolvePath(dir, artifact))
	for _, snapshot := range []string{"", "placeholder"} {
		options.ChangeSnapshot = snapshot
		if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(result.Failures[0].Message, "--change-snapshot") {
			t.Fatalf("expected invalid snapshot %q to fail, got %#v", snapshot, result.Failures)
		}
	}
}

func TestReceiptFinalizeWritesDispatchOwnedRouteFields(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := `{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "other-snapshot")
	mutateJSONObject(t, resolvePath(dir, artifact), func(root map[string]any) {
		root["schemaVersion"] = 1
		root["artifactRole"] = "ARCHITECTURE_REVIEW"
		root["workflowId"] = "other-workflow"
		root["gate"] = "architecture-health-gate"
		root["stage"] = "Carry"
	})
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: []byte(payload)}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); !result.OK() {
		t.Fatalf("machine-owned route fields were not corrected: %#v", result.Failures)
	}
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 2 || envelope.ArtifactRole != "COMPLEXITY_REVIEW" || envelope.WorkflowID != "wf" || envelope.ChangeSnapshot != "snap" || envelope.Gate != "complexity-gate" || envelope.Stage != "" {
		t.Fatalf("dispatch route was not written into reviewer output: %#v", envelope)
	}
	var reviewer ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &reviewer); err != nil {
		t.Fatal(err)
	}
	if reviewer.ContextBundle.Path != "restricted/receipt-context-bundle.json" || reviewer.ContextBundle.SHA256 == "" {
		t.Fatalf("dispatch context bundle was not written into reviewer output: %#v", reviewer.ContextBundle)
	}
}

func TestReceiptPreflightReadsNativeHookConfig(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(dir, ".codex", "hooks.json")
	mustWrite(t, config, `{
  "hooks": {
    "SubagentStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"formal-gates\" receipt capture --provider codex --event SubagentStart --worktree ."
          }
        ]
      }
    ],
    "SubagentStop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"formal-gates\" receipt capture --provider codex --event SubagentStop --worktree ."
          }
        ]
      }
    ]
  }
}`)
	report, result := ReceiptPreflight(ReceiptPreflightOptions{Host: "codex", Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected preflight diagnostic to pass, got %#v", result.Failures)
	}
	if report.Status != "HOST_AUTO_CAPTURE_UNPROVEN" {
		t.Fatalf("unexpected status: %#v", report)
	}
	if report.ConfigPath == "" || !strings.Contains(report.ConfigPath, ".codex/hooks.json") {
		t.Fatalf("expected config path, got %#v", report)
	}
	if len(report.ConfiguredLifecycleHooks["SubagentStart"]) != 1 || len(report.ConfiguredLifecycleHooks["SubagentStop"]) != 1 {
		t.Fatalf("expected lifecycle hook commands, got %#v", report.ConfiguredLifecycleHooks)
	}
	for _, missing := range report.Missing {
		if strings.Contains(missing, "receipt capture hook") {
			t.Fatalf("did not expect hook-missing diagnostic when config contains hooks: %#v", report.Missing)
		}
	}
}

func TestReceiptPreflightReportsMissingHookConfig(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	report, result := ReceiptPreflight(ReceiptPreflightOptions{Host: "cursor", Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected preflight diagnostic to pass, got %#v", result.Failures)
	}
	if report.ConfigPath != "" {
		t.Fatalf("expected no config path, got %#v", report)
	}
	if len(report.Missing) == 0 || !strings.Contains(strings.Join(report.Missing, "\n"), "Cursor hooks.json") {
		t.Fatalf("expected missing Cursor config diagnostic, got %#v", report.Missing)
	}
}

func defaultReceiptArtifact(t *testing.T, worktree, workflowID, name string) string {
	t.Helper()
	runDir, err := resolveWorkflowRunDir(worktree, workflowID, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "restricted", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return relativePath(worktree, path)
}

func registerReceiptFixture(t *testing.T, options ReceiptRegisterOptions) (ReceiptRegistration, Result) {
	t.Helper()
	return ReceiptRegisterDispatch(withReceiptBundle(t, options))
}

func withReceiptBundle(t *testing.T, options ReceiptRegisterOptions) ReceiptRegisterOptions {
	t.Helper()
	runDir, err := resolveWorkflowRunDir(options.Worktree, options.WorkflowID, options.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONTest(t, filepath.Join(options.Worktree, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	if options.ContextBundle == "" {
		inputPath := filepath.Join(runDir, "restricted", "receipt-context.txt")
		mustWrite(t, inputPath, "context\n")
		options.ContextBundle = "restricted/receipt-context-bundle.json"
		writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(options.ContextBundle)), ContextBundle{
			BundleVersion: 1, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
			Inputs: []EvidenceRef{{Path: "restricted/receipt-context.txt", SHA256: sha256File(inputPath)}},
		})
	}
	if reviewJudgmentLifecycle(options.Gate, options.Stage) && options.Prompt == "" {
		name := "receipt-final-send-" + sha256Bytes([]byte(options.Artifact))[:12] + ".txt"
		options.Prompt = filepath.ToSlash(filepath.Join("restricted", name))
		mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.Prompt)), finalSendPromptForOptions(runDir, options))
	}
	return options
}

func finalSendPromptForOptions(runDir string, options ReceiptRegisterOptions) string {
	role, policies := dispatchOutputContracts(options.Gate, options.Stage)
	policyID := ""
	if len(policies) > 0 {
		policyID = policies[0]
	}
	bundlePath := filepath.Join(runDir, filepath.FromSlash(options.ContextBundle))
	format := "closed schema-version-2 " + role + " JSON for " + policyID + "; routing-only bindings contextBundle=" + filepath.ToSlash(options.ContextBundle) + " sha256=" + sha256File(bundlePath) + "; do not read these files"
	prompt := "formal_gate_dispatch: " + expectedDispatchRole(options.Gate, options.Stage) + "\n" +
		"Current requirement: requirements/current.md\n" +
		"Current diff or proposed change: git diff base --\n" +
		"Worktree: " + options.Worktree + "\n" +
		"Base commit or snapshot: " + options.ChangeSnapshot + "\n" +
		"Output path: " + options.Artifact + "\n" +
		"Output format: " + format + "\n"
	return prompt
}

func finalSendPromptFixture(worktree, gate, artifact string) string {
	return "formal_gate_dispatch: " + gate + "\n" +
		"Current requirement: requirements/current.md\n" +
		"Current diff or proposed change: git diff base --\n" +
		"Worktree: " + worktree + "\n" +
		"Base commit or snapshot: base..snapshot\n" +
		"Output path: " + artifact + "\n" +
		"Output format: schema-version-2 JSON\n"
}

func resealDispatchPrompt(prompt string) string {
	prompt = dispatchStaticValidationPattern.ReplaceAllString(prompt, dispatchStaticValidationPrefix+strings.Repeat("0", 64))
	return sealDispatchStaticValidation(prompt)
}

func writeReceiptArtifactFixture(t *testing.T, dir, artifact, snapshot string) {
	t.Helper()
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := policyByID("complexity.post-development.v2")
	envelope, payload := reviewerPolicyFixture(t, runDir, policy)
	envelope.ChangeSnapshot = snapshot
	writeEnvelopeTest(t, filepath.Join(dir, artifact), envelope, payload)
}

func assertTypedWireRoundTrip(t *testing.T, path string, target any, expectedFields []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := strictContractJSON(data, target); err != nil {
		t.Fatalf("typed wire decode failed for %s: %v", path, err)
	}
	encoded, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, append(encoded, '\n')) {
		t.Fatalf("typed wire round trip changed %s\nwritten=%s\nencoded=%s", path, data, encoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != len(expectedFields) {
		t.Fatalf("wire field count drifted for %s: got=%v want=%v", path, fields, expectedFields)
	}
	for _, field := range expectedFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("wire field %s is missing from %s", field, path)
		}
	}
}

func TestReceiptRegisterProcessHelper(t *testing.T) {
	if os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_HELPER") != "1" {
		return
	}
	ready := os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_READY")
	release := os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_RELEASE")
	if err := os.WriteFile(ready, []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(release); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for simultaneous release")
		}
		time.Sleep(time.Millisecond)
	}
	_, result := ReceiptRegisterDispatch(ReceiptRegisterOptions{
		Worktree:       os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_WORKTREE"),
		RunDir:         os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_RUN_DIR"),
		Provider:       "codex",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
		Gate:           "qa-test-gate",
		Stage:          "Execution",
		Artifact:       os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_ARTIFACT"),
		ContextBundle:  "restricted/receipt-context-bundle.json",
		Prompt:         "restricted/receipt-final-send.txt",
	})
	if result.OK() {
		os.Exit(0)
	}
	if strings.Contains(resultSummary(result), "already reserved") {
		os.Exit(10)
	}
	if strings.Contains(resultSummary(result), "review capacity reserved") {
		os.Exit(11)
	}
	t.Fatalf("unexpected registration result: %#v", result.Failures)
}

func runSimultaneousReceiptRegistrations(t *testing.T, worktree, runDir string, artifacts [2]string) ([2]int, [2]string) {
	t.Helper()
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	withReceiptBundle(t, ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "snap"})
	mustWrite(t, filepath.Join(runDir, "restricted", "receipt-final-send.txt"), finalSendPromptFixture(worktree, "qa-test-gate", artifacts[0]))
	release := filepath.Join(worktree, "release")
	var commands [2]*exec.Cmd
	var buffers [2]bytes.Buffer
	var ready [2]string
	for i := range commands {
		ready[i] = filepath.Join(worktree, fmt.Sprintf("ready-%d", i))
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestReceiptRegisterProcessHelper$")
		commands[i].Env = append(os.Environ(),
			"FORMAL_GATES_RECEIPT_REGISTER_HELPER=1",
			"FORMAL_GATES_RECEIPT_REGISTER_WORKTREE="+worktree,
			"FORMAL_GATES_RECEIPT_REGISTER_RUN_DIR="+runDir,
			"FORMAL_GATES_RECEIPT_REGISTER_ARTIFACT="+artifacts[i],
			"FORMAL_GATES_RECEIPT_REGISTER_READY="+ready[i],
			"FORMAL_GATES_RECEIPT_REGISTER_RELEASE="+release,
		)
		commands[i].Stdout = &buffers[i]
		commands[i].Stderr = &buffers[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, firstErr := os.Stat(ready[0])
		_, secondErr := os.Stat(ready[1])
		if firstErr == nil && secondErr == nil {
			break
		}
		if time.Now().After(deadline) {
			for _, command := range commands {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
			t.Fatalf("timed out waiting for registration processes: first=%v second=%v output=%q", firstErr, secondErr, [2]string{buffers[0].String(), buffers[1].String()})
		}
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(release, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var codes [2]int
	var output [2]string
	for i, command := range commands {
		err := command.Wait()
		if err == nil {
			codes[i] = 0
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			codes[i] = exitErr.ExitCode()
		} else {
			t.Fatalf("registration process wait failed: %v", err)
		}
		output[i] = buffers[i].String()
	}
	return codes, output
}
