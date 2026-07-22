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
	if !strings.HasPrefix(event.EventArtifact, ".gates/runs/wf/restricted/proofs/events/") {
		t.Fatalf("unexpected event artifact path: %q", event.EventArtifact)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(event.EventArtifact))); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptCaptureAutoSelectsRunLocalDispatch(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".gates", "runs", "active-run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := ".gates/runs/active-run/restricted/review.json"
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	event, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)})
	if !result.OK() {
		t.Fatalf("run-local capture failed: %#v", result.Failures)
	}
	if !strings.HasPrefix(event.EventArtifact, ".gates/runs/active-run/restricted/proofs/events/") {
		t.Fatalf("capture escaped correlated run: %q", event.EventArtifact)
	}
}

func TestReceiptCaptureRejectsExplicitRunConflict(t *testing.T) {
	dir := t.TempDir()
	runA := filepath.Join(dir, ".gates", "runs", "run-a")
	runB := filepath.Join(dir, ".gates", "runs", "run-b")
	for _, runDir := range []string{runA, runB} {
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runA, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: ".gates/runs/run-a/restricted/review.json"})
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
			a, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: aArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			b, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "architecture-health-gate", Artifact: bArtifact})
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
			event, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStart", Payload: raw})
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
			a, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: aArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			b, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "architecture-health-gate", Artifact: bArtifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			writeReceiptArtifactFixture(t, dir, aArtifact, "snap")
			ids := map[string]string{"a": a.DispatchID, "b": b.DispatchID}
			paths := map[string]string{"a": a.DispatchRegistrationArtifact, "b": b.DispatchRegistrationArtifact, "alias": "./" + a.DispatchRegistrationArtifact}
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			eventDir := receiptProofDir(dir, runDir, "events")
			for name, event := range map[string]receiptEventRecord{
				"start.json": {Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", NormalizedEvent: "subagent_start", RawEventName: "SubagentStart", SubagentID: "subagent-1", DispatchID: ids[test.startID], DispatchRegistrationArtifact: paths[test.startPath]},
				"stop.json":  {Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", NormalizedEvent: "subagent_stop", RawEventName: "SubagentStop", SubagentID: "subagent-1", DispatchID: ids[test.stopID], DispatchRegistrationArtifact: paths[test.stopPath]},
			} {
				if err := writeJSON(filepath.Join(eventDir, name), event); err != nil {
					t.Fatal(err)
				}
			}
			if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", Gate: "complexity-gate", Artifact: aArtifact}); result.OK() || !strings.Contains(result.Failures[0].Message, "matching subagent_start") {
				t.Fatalf("mismatched lifecycle event was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestReceiptFinalizeRejectsStopCapturedBeforeStart(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "strictly after") {
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
	if !strings.HasPrefix(dispatch.DispatchRegistrationArtifact, ".gates/runs/wf/restricted/proofs/dispatch/") {
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
	if sha256Bytes(promptData) != registeredDispatch.PromptSha256 {
		t.Fatalf("receipt register prompt hash does not match exact prepared bytes")
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "contextBundle", "contextSha256", "reviewPolicyId", "reviewTemplate", "reviewTemplateSha256", "promptArtifact", "promptSha256", "status",
	})
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
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
	if !strings.HasPrefix(receipt.ReceiptArtifact, ".gates/runs/wf/restricted/proofs/") {
		t.Fatalf("omitted --run-dir finalized outside the default run: %#v", receipt)
	}
	if isDir(filepath.Join(dir, ".gates", "proofs")) {
		t.Fatal("omitted --run-dir wrote repository-level receipt proofs")
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "contextBundle", "contextSha256", "reviewPolicyId", "reviewTemplate", "reviewTemplateSha256", "semanticSubmissionSha256", "promptArtifact", "promptSha256", "receiptArtifact", "status",
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
	if bound.SubagentID != "" || len(bound.NormalizedEvents) != 0 || bound.StartEventArtifact != "" || bound.StopEventArtifact != "" {
		t.Fatalf("Codex receipt claimed unavailable lifecycle evidence: %#v", bound)
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
}

func TestReceiptFinalizeAndValidateQADesignDocument(t *testing.T) {
	dir := t.TempDir()
	var artifact string
	var dispatch ReceiptRegistration
	var receipt ReceiptFinalizeOutput
	for i := 1; i <= 4; i++ {
		snapshot := fmt.Sprintf("design-snap-%d", i)
		artifact = defaultReceiptArtifact(t, dir, "wf", fmt.Sprintf("qa-cases-%d.md", i))
		options := ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, QACaseCount: 1}
		var result Result
		dispatch, result = registerReceiptFixture(t, options)
		if !result.OK() {
			t.Fatalf("Design %d registration failed: %#v", i, result.Failures)
		}
		payload := []byte(`{"workflowId":"wf","changeSnapshot":"` + snapshot + `","gate":"qa-test-gate","stage":"Design","subagentId":"designer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
		if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
			t.Fatal(result.Failures)
		}
		if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("confirmed")}}}); !result.OK() {
			t.Fatal(result.Failures)
		}
		if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
			t.Fatal(result.Failures)
		}
		receipt, result = ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact})
		if !result.OK() {
			t.Fatalf("Design %d finalization failed: %#v", i, result.Failures)
		}
	}
	result := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "qa-test-gate", Stage: "Design", WorkflowID: "wf", ChangeSnapshot: "design-snap-4"})
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

func TestReceiptSubmitBuildsCanonicalMultiCaseQADesign(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "qa-cases-multi.md")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, QACaseCount: 2})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	submission, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, DesignCases: []ReceiptSemanticDesignCase{
		{Position: 2, Values: designSemanticValues("second")},
		{Position: 1, Values: designSemanticValues("first")},
	}})
	if !result.OK() {
		t.Fatalf("multi-case Design submission failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n\n")) || bytes.Count(data, []byte("\n")) != 2+2*9 {
		t.Fatalf("CLI did not generate canonical Design line structure: %q", data)
	}
	text := string(data)
	if !strings.Contains(text, "Case ID: CASE-001\nClaim: first claim\n") || !strings.Contains(text, "Case ID: CASE-002\nClaim: second claim\n") || strings.Contains(text, "PENDING") {
		t.Fatalf("CLI did not project case semantics into generated case order: %s", text)
	}
	registered, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if !ok || registered.SemanticSubmissionSHA != submission.ArtifactSha256 {
		t.Fatalf("Design submission hash was not committed to the open dispatch: %#v", registered)
	}
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"design-snap","gate":"qa-test-gate","stage":"Design","subagentId":"designer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact})
	if !result.OK() {
		t.Fatalf("multi-case Design submission was not finalizable: %#v", result.Failures)
	}
	if result := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "qa-test-gate", Stage: "Design", WorkflowID: "wf", ChangeSnapshot: "design-snap"}); !result.OK() {
		t.Fatalf("multi-case Design receipt did not validate: %#v", result.Failures)
	}
}

func TestReceiptSubmitRejectsInvalidQADesignSemanticsWithoutChangingFiles(t *testing.T) {
	valid := func() []ReceiptSemanticDesignCase {
		return []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("first")}, {Position: 2, Values: designSemanticValues("second")}}
	}
	tests := []struct {
		name   string
		mutate func([]ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase
		want   string
	}{
		{name: "missing", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase { return cases[:1] }, want: "cover every generated case"},
		{name: "duplicate", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[1].Position = 1
			return cases
		}, want: "duplicated"},
		{name: "unknown", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[1].Position = 3
			return cases
		}, want: "unknown"},
		{name: "extra", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			return append(cases, ReceiptSemanticDesignCase{Position: 3, Values: designSemanticValues("extra")})
		}, want: "cover every generated case"},
		{name: "missing value", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[0].Values = cases[0].Values[:6]
			return cases
		}, want: "exactly 7"},
		{name: "empty value", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[0].Values[0] = ""
			return cases
		}, want: "value 1 is invalid"},
		{name: "pending value", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[0].Values[1] = " PENDING "
			return cases
		}, want: "value 2 is invalid"},
		{name: "multiline value", mutate: func(cases []ReceiptSemanticDesignCase) []ReceiptSemanticDesignCase {
			cases[0].Values[2] = "first line\nsecond line"
			return cases
		}, want: "value 3 is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			artifact := defaultReceiptArtifact(t, dir, "wf", "invalid-design.md")
			dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, QACaseCount: 2})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			beforeArtifact, err := os.ReadFile(resolvePath(dir, artifact))
			if err != nil {
				t.Fatal(err)
			}
			beforeDispatch, err := os.ReadFile(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
			if err != nil {
				t.Fatal(err)
			}
			if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, DesignCases: test.mutate(valid())}); result.OK() || !strings.Contains(resultSummary(result), test.want) {
				t.Fatalf("invalid QA Design semantics were accepted: %#v", result.Failures)
			}
			afterArtifact, _ := os.ReadFile(resolvePath(dir, artifact))
			afterDispatch, _ := os.ReadFile(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
			if !bytes.Equal(beforeArtifact, afterArtifact) || !bytes.Equal(beforeDispatch, afterDispatch) {
				t.Fatal("rejected QA Design submission changed artifact or dispatch")
			}
		})
	}
}

func TestReceiptSemanticSubmissionRolesRejectEveryForeignField(t *testing.T) {
	designValues := []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("case")}}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "reviewer Carry decisions", call: func() error {
			_, err := composeReviewSemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{CarryDecisions: []ReceiptSemanticCarryDecision{{GatePosition: 1, Decision: "ACCEPT_CARRY", Reason: "reason"}}})
			return err
		}},
		{name: "reviewer Design cases", call: func() error {
			_, err := composeReviewSemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{DesignCases: designValues})
			return err
		}},
		{name: "Carry checks", call: func() error {
			_, err := composeCarrySemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{Checks: []ReceiptSemanticCheck{{Position: 1}}})
			return err
		}},
		{name: "Carry findings", call: func() error {
			_, err := composeCarrySemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{Findings: []ReceiptSemanticFinding{{CheckPosition: 1}}})
			return err
		}},
		{name: "Carry locations", call: func() error {
			_, err := composeCarrySemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{Locations: []ReceiptSemanticLocation{{FindingPosition: 1}}})
			return err
		}},
		{name: "Carry Design cases", call: func() error {
			_, err := composeCarrySemanticSubmission(FormalGateEvidence{}, ReceiptSubmitOptions{DesignCases: designValues})
			return err
		}},
		{name: "Design checks", call: func() error {
			_, err := composeQADesignSemanticSubmission(dispatchRegistration{QACaseCount: 1}, ReceiptSubmitOptions{DesignCases: designValues, Checks: []ReceiptSemanticCheck{{Position: 1}}})
			return err
		}},
		{name: "Design findings", call: func() error {
			_, err := composeQADesignSemanticSubmission(dispatchRegistration{QACaseCount: 1}, ReceiptSubmitOptions{DesignCases: designValues, Findings: []ReceiptSemanticFinding{{CheckPosition: 1}}})
			return err
		}},
		{name: "Design locations", call: func() error {
			_, err := composeQADesignSemanticSubmission(dispatchRegistration{QACaseCount: 1}, ReceiptSubmitOptions{DesignCases: designValues, Locations: []ReceiptSemanticLocation{{FindingPosition: 1}}})
			return err
		}},
		{name: "Design Carry decisions", call: func() error {
			_, err := composeQADesignSemanticSubmission(dispatchRegistration{QACaseCount: 1}, ReceiptSubmitOptions{DesignCases: designValues, CarryDecisions: []ReceiptSemanticCarryDecision{{GatePosition: 1}}})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("foreign semantic field was accepted")
			}
		})
	}
}

func TestReceiptFinalizeRejectsDirectlyEditedQADesignWithSingleEOFTerminator(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "direct-design.md")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, QACaseCount: 1})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	record, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if !ok {
		t.Fatal("cannot decode QA Design dispatch")
	}
	canonical, err := composeQADesignSemanticSubmission(record, ReceiptSubmitOptions{DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("direct")}}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, resolvePath(dir, artifact), strings.TrimSuffix(string(canonical), "\n"))
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"design-snap","gate":"qa-test-gate","stage":"Design","subagentId":"designer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Design", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "requires CLI semantic submission") {
		t.Fatalf("directly edited single-terminator Design document reached finalization: %#v", result.Failures)
	}
}

func TestReceiptRegisterRejectsQAExecutionLifecycle(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "qa-execution.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Execution", Artifact: artifact})
	if options.Prompt != "" {
		t.Fatal("QA Execution fixture was given a reviewer prompt")
	}
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "QA Execution uses CLI composition") {
		t.Fatalf("QA Execution was accepted by the obsolete receipt path: %#v", result.Failures)
	}
}

func TestReceiptRegisterRequiresRecordedRerunForPriorPostDevelopmentPass(t *testing.T) {
	const workflowID, sourceSnapshot, targetSnapshot, gate = "wf-rerun-register", "source", "target", "code-quality-gate"
	tests := []struct {
		name           string
		setup          func(*testing.T, string)
		wantOK         bool
		wantTransition string
	}{
		{name: "first run", wantOK: true},
		{name: "prior other gate", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, "complexity-gate")
		}, wantOK: true},
		{name: "prior Design Review", setup: func(t *testing.T, root string) {
			recordDesignReviewPassFixture(t, root, workflowID, sourceSnapshot)
		}, wantOK: true},
		{name: "missing transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, gate)
		}, wantTransition: "required for new snapshot"},
		{name: "ACCEPT_CARRY transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, gate)
			if result := recordCarryDecisionTransitionFixture(t, root, workflowID, sourceSnapshot, targetSnapshot, gate, "ACCEPT_CARRY"); !result.OK() {
				t.Fatalf("cannot record ACCEPT_CARRY transition: %#v", result.Failures)
			}
		}, wantTransition: "explicit RERUN decision"},
		{name: "BLOCKED transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, gate)
			if result := recordCarryDecisionTransitionFixture(t, root, workflowID, sourceSnapshot, targetSnapshot, gate, "BLOCKED"); result.OK() {
				t.Fatal("BLOCKED Carry artifact was recorded as a terminal transition")
			}
		}, wantTransition: "required for new snapshot"},
		{name: "RERUN_REQUIRED transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, gate)
			if result := recordCarryDecisionTransitionFixture(t, root, workflowID, sourceSnapshot, targetSnapshot, gate, "RERUN_REQUIRED"); !result.OK() {
				t.Fatalf("cannot record RERUN_REQUIRED transition: %#v", result.Failures)
			}
		}, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.setup != nil {
				test.setup(t, root)
			}
			runDir, err := resolveWorkflowRunDir(root, workflowID, "")
			if err != nil {
				t.Fatal(err)
			}
			artifact := filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, "restricted", "target-code-quality.json"))
			options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: root, RunDir: runDir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: targetSnapshot, Gate: gate, Artifact: artifact})
			artifactPath, err := runLocalReviewArtifactPath(root, runDir, artifact)
			if err != nil {
				t.Fatal(err)
			}
			dispatchPath := filepath.Join(receiptProofDir(root, runDir, "dispatch"), sha256Bytes([]byte(artifactPath))+".json")
			registration, result := ReceiptRegisterDispatch(options)
			if result.OK() != test.wantOK {
				t.Fatalf("registration OK=%v, want %v: %#v", result.OK(), test.wantOK, result.Failures)
			}
			if test.wantTransition != "" && !strings.Contains(resultSummary(result), test.wantTransition) {
				t.Fatalf("missing transition failure %q: %#v", test.wantTransition, result.Failures)
			}
			if test.wantOK {
				if !isFile(artifactPath) || !isFile(resolvePath(root, registration.DispatchRegistrationArtifact)) {
					t.Fatal("successful registration did not create artifact and dispatch proof")
				}
				return
			}
			if _, err := os.Lstat(artifactPath); !os.IsNotExist(err) {
				t.Fatalf("rejected registration left reviewer artifact: %v", err)
			}
			if _, err := os.Lstat(dispatchPath); !os.IsNotExist(err) {
				t.Fatalf("rejected registration left dispatch proof: %v", err)
			}
		})
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
	promptPath, err := safeEvidencePath(filepath.Join(dir, ".gates", "runs", "wf"), options.Prompt)
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

func TestReceiptFinalizeMissingLifecycleIsUnprovenForSupportedProvider(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "complexity.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree:       dir,
		Provider:       "cursor",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
		Gate:           "complexity-gate",
		Artifact:       artifact,
	})
	if !result.OK() {
		t.Fatalf("expected dispatch registration to pass, got %#v", result.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStart", Payload: []byte(payload)}); !result.OK() {
		t.Fatalf("expected start capture to pass, got %#v", result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	_, result = ReceiptFinalize(ReceiptFinalizeOptions{
		Worktree:   dir,
		Provider:   "cursor",
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

func TestReceiptFinalizeAcceptsLifecycleForSupportedProvider(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "complexity.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "cursor", WorkflowID: "wf", ChangeSnapshot: "snap",
		Gate: "complexity-gate", Artifact: artifact,
	})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeReceiptArtifactFixture(t, dir, artifact, "snap")
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "cursor", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "cursor", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	if result := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatal(result.Failures)
	}
}

func TestReceiptRegisterGeneratesStaticReviewTemplate(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	dispatch, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		t.Fatalf("expected absent output registration to pass, got %#v", result.Failures)
	}
	if _, err := os.Stat(resolvePath(dir, artifact)); err != nil {
		t.Fatalf("registration did not create reviewer template: %v", err)
	}
	if dispatch.DispatchRegistrationStatusText != "open" {
		t.Fatalf("unexpected registration: %#v", dispatch)
	}
	beforeDispatch, _ := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if duplicate, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(result.Failures[0].Message, "already exists") {
		afterDispatch, _ := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
		t.Fatalf("expected duplicate open reservation rejection, got registration=%#v failures=%#v before=%#v after=%#v", duplicate, result.Failures, beforeDispatch, afterDispatch)
	}
}

func TestReceiptSubmitBuildsFinalizableReviewWithMultipleFindingsAndLocations(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review-submit.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap",
		Gate: "complexity-gate", Artifact: artifact,
	})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	checks := receiptSemanticChecks(t, dir, artifact, "PASS")
	checks[1].Status = "REVIEW"
	checks[1].Message = "The diff shape needs rework."
	submission, result := ReceiptSubmit(ReceiptSubmitOptions{
		Worktree: dir, Artifact: artifact, Checks: checks,
		Findings: []ReceiptSemanticFinding{
			{CheckPosition: 2, Message: "First semantic finding."},
			{CheckPosition: 2, Message: "Second semantic finding."},
		},
		Locations: []ReceiptSemanticLocation{
			{FindingPosition: 1, Path: "internal/validate/receipt.go", StartLine: 10, EndLine: 12},
			{FindingPosition: 1, Path: "internal/cli/cli.go", StartLine: 20, EndLine: 20},
			{FindingPosition: 2, Path: "README.md", StartLine: 30, EndLine: 31},
		},
	})
	if !result.OK() {
		t.Fatalf("semantic submission failed: %#v", result.Failures)
	}
	if submission.ArtifactRole != "COMPLEXITY_REVIEW" || submission.ArtifactSha256 == "" || submission.Status != "submitted" {
		t.Fatalf("unexpected submission result: %#v", submission)
	}
	submittedDispatch, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if !ok || submittedDispatch.Status != "open" || submittedDispatch.SemanticSubmissionSHA != submission.ArtifactSha256 {
		t.Fatalf("open dispatch did not atomically record submitted artifact hash: %#v", submittedDispatch)
	}
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Verdict != "PENDING" {
		t.Fatalf("semantic submission wrote the aggregate verdict: %q", envelope.Verdict)
	}
	var payload ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Checks[1].Findings) != 2 || len(payload.Checks[1].Findings[0].Locations) != 2 || len(payload.Checks[1].Findings[1].Locations) != 1 {
		t.Fatalf("nested findings and locations were not generated correctly: %#v", payload.Checks[1].Findings)
	}
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatalf("submitted artifact was not finalizable: %#v", result.Failures)
	}
	data, err = os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Verdict != "REVIEW" {
		t.Fatalf("finalization did not derive non-PASS verdict: %q", envelope.Verdict)
	}
	if result := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatalf("finalized submitted artifact did not validate: %#v", result.Failures)
	}
}

func TestReceiptSubmitAllowsCompleteReviewResubmissionAndFinalize(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review-resubmit.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap",
		Gate: "complexity-gate", Artifact: artifact,
	})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	firstChecks := receiptSemanticChecks(t, dir, artifact, "PASS")
	firstChecks[0].Message = "Initial complete review."
	first, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: firstChecks})
	if !result.OK() {
		t.Fatalf("initial submission failed: %#v", result.Failures)
	}
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}

	secondChecks := receiptSemanticChecks(t, dir, artifact, "PASS")
	secondChecks[0].Status = "REVIEW"
	secondChecks[0].Message = "Replacement complete review."
	second, result := ReceiptSubmit(ReceiptSubmitOptions{
		Worktree: dir, Artifact: artifact, Checks: secondChecks,
		Findings:  []ReceiptSemanticFinding{{CheckPosition: 1, Message: "Replacement finding."}},
		Locations: []ReceiptSemanticLocation{{FindingPosition: 1, Path: "internal/validate/receipt.go", StartLine: 1, EndLine: 1}},
	})
	if !result.OK() {
		t.Fatalf("complete resubmission failed: %#v", result.Failures)
	}
	if first.ArtifactSha256 == second.ArtifactSha256 {
		t.Fatal("resubmission did not replace the semantic projection")
	}
	registered, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if !ok || registered.SemanticSubmissionSHA != second.ArtifactSha256 {
		t.Fatalf("dispatch does not bind the replacement submission: %#v", registered)
	}

	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatalf("replacement submission was not finalizable: %#v", result.Failures)
	}
	if result := ReceiptValidate(ReceiptValidateOptions{Worktree: dir, Receipt: receipt.ReceiptArtifact, Artifact: artifact, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatalf("replacement submission receipt did not validate: %#v", result.Failures)
	}
	finalArtifact, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(finalArtifact, []byte("Replacement complete review.")) || bytes.Contains(finalArtifact, []byte("Initial complete review.")) {
		t.Fatalf("final artifact was not rebuilt from the static catalog: %s", finalArtifact)
	}
	finalDispatch, err := os.ReadFile(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
	if err != nil {
		t.Fatal(err)
	}
	if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: secondChecks}); result.OK() {
		t.Fatal("finalized dispatch accepted a resubmission")
	}
	assertFileBytes(t, resolvePath(dir, artifact), finalArtifact)
	assertFileBytes(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), finalDispatch)
}

func TestReceiptSubmitRejectsInvalidReviewResubmissionsWithoutChangingFiles(t *testing.T) {
	setup := func(t *testing.T) (string, string, ReceiptRegistration, []ReceiptSemanticCheck) {
		t.Helper()
		dir := t.TempDir()
		artifact := defaultReceiptArtifact(t, dir, "wf", "review-resubmit-invalid.json")
		dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
			Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap",
			Gate: "complexity-gate", Artifact: artifact,
		})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		checks := receiptSemanticChecks(t, dir, artifact, "PASS")
		if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: checks}); !result.OK() {
			t.Fatal(result.Failures)
		}
		return dir, artifact, dispatch, checks
	}

	for _, test := range []struct {
		name    string
		options func(string, string, []ReceiptSemanticCheck) ReceiptSubmitOptions
	}{
		{name: "static validation", options: func(dir, artifact string, checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			checks[0].Status = "REVIEW"
			return ReceiptSubmitOptions{
				Worktree: dir, Artifact: artifact, Checks: checks,
				Findings:  []ReceiptSemanticFinding{{CheckPosition: 1, Message: "Invalid location."}},
				Locations: []ReceiptSemanticLocation{{FindingPosition: 1, Path: ".gates/runs/wf/restricted/review.json", StartLine: 1, EndLine: 1}},
			}
		}},
		{name: "incomplete semantics", options: func(dir, artifact string, checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: checks[:len(checks)-1]}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, artifact, dispatch, checks := setup(t)
			artifactBefore, err := os.ReadFile(resolvePath(dir, artifact))
			if err != nil {
				t.Fatal(err)
			}
			dispatchPath := resolvePath(dir, dispatch.DispatchRegistrationArtifact)
			dispatchBefore, err := os.ReadFile(dispatchPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, result := ReceiptSubmit(test.options(dir, artifact, checks)); result.OK() {
				t.Fatal("invalid resubmission passed")
			}
			assertFileBytes(t, resolvePath(dir, artifact), artifactBefore)
			assertFileBytes(t, dispatchPath, dispatchBefore)
		})
	}

	t.Run("edited assigned artifact", func(t *testing.T) {
		dir, artifact, dispatch, checks := setup(t)
		artifactPath := resolvePath(dir, artifact)
		artifactBefore, err := os.ReadFile(artifactPath)
		if err != nil {
			t.Fatal(err)
		}
		artifactBefore = append(artifactBefore, '\n')
		if err := os.WriteFile(artifactPath, artifactBefore, 0o600); err != nil {
			t.Fatal(err)
		}
		dispatchPath := resolvePath(dir, dispatch.DispatchRegistrationArtifact)
		dispatchBefore, err := os.ReadFile(dispatchPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: checks}); result.OK() {
			t.Fatal("edited assigned artifact accepted a resubmission")
		}
		assertFileBytes(t, artifactPath, artifactBefore)
		assertFileBytes(t, dispatchPath, dispatchBefore)
	})
}

func TestReceiptSubmitResubmissionContractAppliesToCarryAndQADesign(t *testing.T) {
	t.Run("Carry", func(t *testing.T) {
		dir := t.TempDir()
		artifact := defaultReceiptArtifact(t, dir, "wf", "carry-resubmit.json")
		dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
			Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
			Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact,
		})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		first, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, CarryDecisions: []ReceiptSemanticCarryDecision{{GatePosition: 1, Decision: "ACCEPT_CARRY", Reason: "Initially unchanged."}}})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		second, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, CarryDecisions: []ReceiptSemanticCarryDecision{{GatePosition: 1, Decision: "RERUN_REQUIRED", Reason: "Replacement decision."}}})
		if !result.OK() || first.ArtifactSha256 == second.ArtifactSha256 {
			t.Fatalf("Carry resubmission failed: first=%#v second=%#v failures=%#v", first, second, result.Failures)
		}
		registered, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
		if !ok || registered.SemanticSubmissionSHA != second.ArtifactSha256 {
			t.Fatalf("Carry dispatch does not bind replacement: %#v", registered)
		}
	})

	t.Run("QA Design", func(t *testing.T) {
		dir := t.TempDir()
		artifact := defaultReceiptArtifact(t, dir, "wf", "design-resubmit.md")
		dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
			Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap",
			Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, QACaseCount: 1,
		})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		first, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("initial")}}})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		second, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("replacement")}}})
		if !result.OK() || first.ArtifactSha256 == second.ArtifactSha256 {
			t.Fatalf("QA Design resubmission failed: first=%#v second=%#v failures=%#v", first, second, result.Failures)
		}
		registered, ok := decodeDispatch(resolvePath(dir, dispatch.DispatchRegistrationArtifact))
		if !ok || registered.SemanticSubmissionSHA != second.ArtifactSha256 {
			t.Fatalf("QA Design dispatch does not bind replacement: %#v", registered)
		}
	})
}

func TestReceiptSubmitRejectsInvalidReviewSemanticsWithoutChangingArtifact(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]ReceiptSemanticCheck) ReceiptSubmitOptions
		want   string
	}{
		{name: "missing check", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Checks: checks[:len(checks)-1]}
		}, want: "cover every generated check"},
		{name: "duplicate check", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			checks[len(checks)-1].Position = 1
			return ReceiptSubmitOptions{Checks: checks}
		}, want: "duplicated"},
		{name: "unknown check", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			checks[len(checks)-1].Position = len(checks) + 1
			return ReceiptSubmitOptions{Checks: checks}
		}, want: "unknown"},
		{name: "invalid status", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			checks[0].Status = "OK"
			return ReceiptSubmitOptions{Checks: checks}
		}, want: "status is invalid"},
		{name: "missing message", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			checks[0].Message = ""
			return ReceiptSubmitOptions{Checks: checks}
		}, want: "message is missing"},
		{name: "unknown finding check", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Checks: checks, Findings: []ReceiptSemanticFinding{{CheckPosition: len(checks) + 1, Message: "finding"}}}
		}, want: "unknown semantic check"},
		{name: "invalid location", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Checks: checks, Findings: []ReceiptSemanticFinding{{CheckPosition: 1, Message: "finding"}}, Locations: []ReceiptSemanticLocation{{FindingPosition: 1, Path: "/absolute.go", StartLine: 0, EndLine: 0}}}
		}, want: "location 1 is invalid"},
		{name: "workflow evidence location", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Checks: checks, Findings: []ReceiptSemanticFinding{{CheckPosition: 1, Message: "finding"}}, Locations: []ReceiptSemanticLocation{{FindingPosition: 1, Path: ".gates/runs/wf/restricted/review.json", StartLine: 1, EndLine: 1}}}
		}, want: "location 1 is invalid"},
		{name: "QA Design fields", mutate: func(checks []ReceiptSemanticCheck) ReceiptSubmitOptions {
			return ReceiptSubmitOptions{Checks: checks, DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("wrong role")}}}
		}, want: "only checks, findings, and locations"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			artifact := defaultReceiptArtifact(t, dir, "wf", "invalid-submit.json")
			registration, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			before, err := os.ReadFile(resolvePath(dir, artifact))
			if err != nil {
				t.Fatal(err)
			}
			dispatchBefore, err := os.ReadFile(resolvePath(dir, registration.DispatchRegistrationArtifact))
			if err != nil {
				t.Fatal(err)
			}
			options := test.mutate(receiptSemanticChecks(t, dir, artifact, "PASS"))
			options.Worktree, options.Artifact = dir, artifact
			if _, result := ReceiptSubmit(options); result.OK() || !strings.Contains(resultSummary(result), test.want) {
				t.Fatalf("invalid semantics were accepted: %#v", result.Failures)
			}
			after, err := os.ReadFile(resolvePath(dir, artifact))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("rejected submission changed assigned artifact\nbefore=%s\nafter=%s", before, after)
			}
			dispatchAfter, err := os.ReadFile(resolvePath(dir, registration.DispatchRegistrationArtifact))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dispatchBefore, dispatchAfter) {
				t.Fatal("rejected submission changed its open dispatch proof")
			}
		})
	}
}

func TestReceiptFinalizeRejectsDirectlyEditedReviewerJSONWithoutSubmissionProof(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "direct-edit.json")
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	path := resolvePath(dir, artifact)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for index := range payload.Checks {
		payload.Checks[index].Status = "PASS"
		payload.Checks[index].Message = "Directly edited semantic value."
	}
	writeEnvelopeTest(t, path, envelope, payload)
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "requires CLI semantic submission") {
		t.Fatalf("directly edited reviewer JSON reached finalization: %#v", result.Failures)
	}
}

func TestReceiptSubmitBuildsMultiGateCarryAndRejectsUnknownGate(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry-submit.json")
	carry := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:2])
	options := ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact,
		ContextBundle: carry.Payload.ContextBundle.Path, TransitionChain: carry.Payload.TransitionChain.Path,
	}
	for index := len(carry.Payload.Decisions) - 1; index >= 0; index-- {
		options.CarrySourceClosures = append(options.CarrySourceClosures, carry.Payload.Decisions[index].SourceGateEvidence.Path)
	}
	registration, result := registerReceiptFixture(t, options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	templateData, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var templateEnvelope FormalGateEvidence
	if err := strictContractJSON(templateData, &templateEnvelope); err != nil {
		t.Fatal(err)
	}
	var generated CarryPayload
	if err := strictContractJSON(templateEnvelope.Payload, &generated); err != nil {
		t.Fatal(err)
	}
	if len(generated.Decisions) != 2 || generated.Decisions[0].Gate != postDevelopmentGateOrder[0] || generated.Decisions[1].Gate != postDevelopmentGateOrder[1] {
		t.Fatalf("Carry catalog followed caller closure order instead of fixed gate order: %#v", generated.Decisions)
	}
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"target","gate":"qa-test-gate","stage":"Carry","subagentId":"arbiter","dispatchId":"` + registration.DispatchID + `","dispatchRegistrationArtifact":"` + registration.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	before, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	dispatchBefore, err := os.ReadFile(resolvePath(dir, registration.DispatchRegistrationArtifact))
	if err != nil {
		t.Fatal(err)
	}
	mixedRole := ReceiptSubmitOptions{
		Worktree: dir, Artifact: artifact,
		CarryDecisions: []ReceiptSemanticCarryDecision{
			{GatePosition: 1, Decision: "ACCEPT_CARRY", Reason: "Unchanged behavior."},
			{GatePosition: 2, Decision: "RERUN_REQUIRED", Reason: "Affected behavior."},
		},
		DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("wrong role")}},
	}
	if _, result := ReceiptSubmit(mixedRole); result.OK() || !strings.Contains(resultSummary(result), "only per-gate decisions") {
		t.Fatalf("Carry submission accepted QA Design semantics: %#v", result.Failures)
	}
	afterMixedRole, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	dispatchAfterMixedRole, err := os.ReadFile(resolvePath(dir, registration.DispatchRegistrationArtifact))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterMixedRole) || !bytes.Equal(dispatchBefore, dispatchAfterMixedRole) {
		t.Fatal("rejected cross-role Carry submission changed artifact or dispatch")
	}
	invalid := ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, CarryDecisions: []ReceiptSemanticCarryDecision{
		{GatePosition: 1, Decision: "ACCEPT_CARRY", Reason: "Unchanged behavior."},
		{GatePosition: 3, Decision: "RERUN_REQUIRED", Reason: "Affected behavior."},
	}}
	if _, result := ReceiptSubmit(invalid); result.OK() || !strings.Contains(resultSummary(result), "unknown") {
		t.Fatalf("unknown Carry gate was accepted: %#v", result.Failures)
	}
	after, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected Carry submission changed assigned artifact")
	}
	submission, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, CarryDecisions: []ReceiptSemanticCarryDecision{
		{GatePosition: 1, Decision: "ACCEPT_CARRY", Reason: "Unchanged behavior."},
		{GatePosition: 2, Decision: "RERUN_REQUIRED", Reason: "Affected behavior."},
	}})
	if !result.OK() || submission.ArtifactRole != "CARRY_ARBITER" {
		t.Fatalf("multi-gate Carry submission failed: submission=%#v failures=%#v", submission, result.Failures)
	}
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload CarryPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Decisions[0].Gate != postDevelopmentGateOrder[0] || payload.Decisions[0].Decision != "ACCEPT_CARRY" || payload.Decisions[1].Gate != postDevelopmentGateOrder[1] || payload.Decisions[1].Decision != "RERUN_REQUIRED" {
		t.Fatalf("CLI did not preserve generated gates while applying semantic decisions: %#v", payload.Decisions)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact}); !result.OK() {
		t.Fatalf("multi-gate Carry submission was not finalizable: %#v", result.Failures)
	}
}

func TestReceiptRegisterRebindsGeneratedTemplateBeforeLifecycleStart(t *testing.T) {
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
	mustWrite(t, promptPath, changedPrompt)
	rebound, result := ReceiptRegisterDispatch(options)
	if !result.OK() || rebound.DispatchRegistrationStatusText != "rebound" || rebound.DispatchID == first.DispatchID {
		t.Fatalf("generated static template was not rebound: first=%#v rebound=%#v failures=%#v", first, rebound, result.Failures)
	}
	if data, err := os.ReadFile(resolvePath(dir, artifact)); err != nil || strings.Contains(string(data), "PENDING") == false {
		t.Fatalf("rebound generated artifact was not retained as a CLI template: err=%v data=%q", err, data)
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
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "already exists") {
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
	carry := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact, ContextBundle: carry.Payload.ContextBundle.Path, TransitionChain: carry.Payload.TransitionChain.Path, CarrySourceClosures: []string{carry.Payload.Decisions[0].SourceGateEvidence.Path}})
	if !result.OK() {
		t.Fatalf("Carry registration rejected its documented dispatch role: %#v", result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"target","gate":"qa-test-gate","stage":"Carry","subagentId":"arbiter","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeCarrySemanticFixture(t, resolvePath(dir, artifact), carry.Payload)
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

func TestReceiptCarryFinalizationDerivesBlockedVerdict(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry-blocked.json")
	carry := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	carry.Payload.Decisions[0].Decision = "BLOCKED"
	carry.Payload.Decisions[0].Reason = "The repair invalidates this gate's evidence."
	dispatch, result := registerReceiptFixture(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact,
		ContextBundle: carry.Payload.ContextBundle.Path, TransitionChain: carry.Payload.TransitionChain.Path,
		CarrySourceClosures: []string{carry.Payload.Decisions[0].SourceGateEvidence.Path},
	})
	if !result.OK() {
		t.Fatalf("Carry registration failed: %#v", result.Failures)
	}
	payload := []byte(`{"workflowId":"wf","changeSnapshot":"target","gate":"qa-test-gate","stage":"Carry","subagentId":"arbiter","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeCarrySemanticFixture(t, resolvePath(dir, artifact), carry.Payload)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact}); !result.OK() {
		t.Fatalf("Carry BLOCKED finalization failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Verdict != "BLOCKED" {
		t.Fatalf("Carry verdict was not mechanically derived: %q", envelope.Verdict)
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
			if _, err := os.Stat(resolvePath(dir, artifact)); (kind == "valid") != (err == nil) {
				t.Fatalf("template creation did not match registration validity: %v", err)
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

func TestReceiptRegisterRequiresComposedContextBundle(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "review.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap",
		Gate: "complexity-gate", Artifact: artifact,
	})
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	input := "restricted/operator-context.txt"
	mustWrite(t, filepath.Join(runDir, filepath.FromSlash(input)), "context\n")
	options.ContextBundle = "restricted/operator-context-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(options.ContextBundle)), ContextBundle{
		BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap",
		Inputs: []EvidenceRef{testRef(t, runDir, input)},
	})
	options.Prompt = "restricted/operator-context-prompt.txt"
	mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.Prompt)), finalSendPromptForOptions(runDir, options))
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "CLI composition proof is missing") {
		t.Fatalf("caller-authored context bundle was accepted for formal registration: %#v", result.Failures)
	}
}

func TestReceiptCarryRequiresComposedTransitionChain(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry.json")
	options := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact,
	})
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(options.TransitionChain)))
	if err != nil {
		t.Fatal(err)
	}
	options.TransitionChain = "restricted/operator-transition.json"
	if err := writeBytesExclusive(filepath.Join(runDir, filepath.FromSlash(options.TransitionChain)), data); err != nil {
		t.Fatal(err)
	}
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "CLI composition proof is missing") {
		t.Fatalf("caller-authored transition chain was accepted for Carry registration: %#v", result.Failures)
	}
}

func TestReceiptCarryDerivesGateAndRejectsDuplicateSourceClosure(t *testing.T) {
	dir := t.TempDir()
	artifact := defaultReceiptArtifact(t, dir, "wf", "carry-duplicate-source.json")
	carry := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	closure := carry.Payload.Decisions[0].SourceGateEvidence.Path
	options := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact,
		ContextBundle: carry.Payload.ContextBundle.Path, TransitionChain: carry.Payload.TransitionChain.Path,
		CarrySourceClosures: []string{closure, closure},
	})
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "duplicate Carry source closure for gate") {
		t.Fatalf("duplicate derived gate was accepted: %#v", result.Failures)
	}
	if _, err := os.Lstat(resolvePath(dir, artifact)); !os.IsNotExist(err) {
		t.Fatalf("rejected Carry registration wrote artifact: %v", err)
	}
	dispatches, err := filepath.Glob(filepath.Join(carry.RunDir, "restricted", "proofs", "dispatch", "*.json"))
	if err != nil || len(dispatches) != 0 {
		t.Fatalf("rejected Carry registration wrote dispatch proof: paths=%v err=%v", dispatches, err)
	}
}

func TestReceiptRegisterLeavesRestrictedRequirementAvailableToCarryArbitration(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	mustWrite(t, filepath.Join(runDir, "restricted", "requirement.md"), "current requirement\n")
	bundleName := "restricted/carry-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(bundleName)), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "target", Inputs: []EvidenceRef{testRef(t, runDir, "restricted/requirement.md")}})
	artifact := relativePath(dir, filepath.Join(runDir, "restricted", "carry-review.json"))
	_, result := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target", Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact, ContextBundle: bundleName}))
	if !result.OK() {
		t.Fatalf("Carry arbitration lost access to its restricted requirement: %#v", result.Failures)
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
			mustWrite(t, promptPath, test.mutate(string(data), options))
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
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "requires CLI semantic submission") {
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
			ReviewArtifact:  fmt.Sprintf(".gates/runs/wf/restricted/review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".gates/runs/wf/restricted/proofs/receipt-%d.json", i), Status: "finalized",
		})
	}
	writeJSONTest(t, filepath.Join(dispatchDir, "failed-open.json"), dispatchRegistration{
		ProofVersion: 1, DispatchID: "failed", Provider: "codex", WorkflowID: "wf",
		ChangeSnapshot: "snap-failed", Gate: "complexity-gate",
		ReviewArtifact: ".gates/runs/wf/restricted/review-failed.json", Status: "open",
	})

	options := ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-4",
		Gate: "complexity-gate", Artifact: ".gates/runs/wf/restricted/review-4.json",
	}
	if _, result := registerReceiptFixture(t, options); result.OK() || !strings.Contains(resultSummary(result), "review limit reached") {
		t.Fatalf("fourth finalized review was not blocked: %#v", result.Failures)
	}
	options.UserAuthorizedExtraReview = true
	if _, result := registerReceiptFixture(t, options); !result.OK() {
		t.Fatalf("explicitly authorized extra review was rejected: %#v", result.Failures)
	}
}

func TestReceiptRegisterDesignReviewRetainsReviewCapacity(t *testing.T) {
	dir := t.TempDir()
	runDir, err := resolveWorkflowRunDir(dir, "wf", "")
	if err != nil {
		t.Fatal(err)
	}
	caseSet, _, err := writeCanaryDesignReviewClosure(dir, runDir, "wf", "design-snap")
	if err != nil {
		t.Fatal(err)
	}
	designReceipt := testDesignReceiptForCaseSet(t, dir, runDir, caseSet)
	dispatchDir := receiptProofDir(dir, runDir, "dispatch")
	for i := 2; i <= 3; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-design-review-%d.json", i)), dispatchRegistration{
			ProofVersion: 1, DispatchID: fmt.Sprintf("design-review-dispatch-%d", i), Provider: "codex",
			WorkflowID: "wf", ChangeSnapshot: "design-snap", Gate: "qa-test-gate", Stage: "Design Review",
			ReviewArtifact:  fmt.Sprintf(".gates/runs/wf/restricted/design-review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".gates/runs/wf/restricted/proofs/design-review-receipt-%d.json", i), Status: "finalized",
		})
	}
	artifact := ".gates/runs/wf/restricted/design-review-4.json"
	prompt := "restricted/design-review-4-prompt.txt"
	currentDiff := relativePath(dir, filepath.Join(runDir, filepath.FromSlash(caseSet.Path)))
	if err := writeCanaryPreparedPromptForRun(dir, runDir, "wf", "design-snap", "qa-test-gate", "Design Review", artifact, "restricted/design-bundle.json", prompt, currentDiff); err != nil {
		t.Fatal(err)
	}
	options := ReceiptRegisterOptions{
		Worktree: dir, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap",
		Gate: "qa-test-gate", Stage: "Design Review", Artifact: artifact, ContextBundle: "restricted/design-bundle.json", Prompt: prompt,
		QADesignCaseSet: caseSet.Path, QADesignReceipt: designReceipt.Path,
	}
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "review limit reached") {
		t.Fatalf("fourth unauthorized Design Review was not blocked: %#v", result.Failures)
	}
}

func TestReceiptRegisterRejectsExtraReviewAuthorizationForQADesign(t *testing.T) {
	dir := t.TempDir()
	artifact := ".gates/runs/wf/restricted/cases.md"
	_, result := ReceiptRegisterDispatch(ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "design-snap",
		Gate: "qa-test-gate", Stage: "Design", Artifact: artifact, ContextBundle: "restricted/context-bundle.json",
		QACaseCount: 1, UserAuthorizedExtraReview: true,
	})
	if result.OK() || !strings.Contains(resultSummary(result), "--user-authorized-extra-review is accepted only for a review judgment") {
		t.Fatalf("QA Design accepted extra-review authorization: %#v", result.Failures)
	}
	if isDir(filepath.Join(dir, ".gates")) || isFile(resolvePath(dir, artifact)) {
		t.Fatal("rejected QA Design authorization wrote an artifact or proof")
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
			ReviewArtifact:  fmt.Sprintf(".gates/runs/wf/restricted/review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".gates/runs/wf/restricted/proofs/receipt-%d.json", i), Status: "finalized",
		})
	}
	first := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-3",
		Gate: "complexity-gate", Artifact: ".gates/runs/wf/restricted/review-3.json",
	})
	if _, result := ReceiptRegisterDispatch(first); !result.OK() {
		t.Fatalf("remaining review slot was not reserved: %#v", result.Failures)
	}
	promptPath := filepath.Join(runDir, filepath.FromSlash(first.Prompt))
	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, promptPath, strings.Replace(string(promptData), "closed schema-version-2", "closed  schema-version-2", 1))
	if _, result := ReceiptRegisterDispatch(first); !result.OK() {
		t.Fatalf("pre-lifecycle generated template rebind was rejected: %#v", result.Failures)
	}
	second := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-4",
		Gate: "complexity-gate", Artifact: ".gates/runs/wf/restricted/review-4.json",
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
		Gate: "architecture-health-gate", Artifact: ".gates/runs/wf/restricted/architecture-review.json",
	})
	if _, result := ReceiptRegisterDispatch(independent); !result.OK() {
		t.Fatalf("a different gate incorrectly shared review capacity: %#v", result.Failures)
	}
	for i := 1; i <= 3; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("qa-execution-%d.json", i)), dispatchRegistration{
			ProofVersion: 1, DispatchID: fmt.Sprintf("qa-dispatch-%d", i), Provider: "codex",
			WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("qa-snap-%d", i), Gate: "qa-test-gate", Stage: "Execution",
			ReviewArtifact:  fmt.Sprintf(".gates/runs/wf/restricted/qa-review-%d.json", i),
			ReceiptArtifact: fmt.Sprintf(".gates/runs/wf/restricted/proofs/qa-receipt-%d.json", i), Status: "finalized",
		})
	}
	independentStage := withReceiptBundle(t, ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "target",
		Gate: "qa-test-gate", Stage: "Carry", Artifact: ".gates/runs/wf/restricted/carry-review.json",
	})
	if _, result := ReceiptRegisterDispatch(independentStage); !result.OK() {
		t.Fatalf("a different stage incorrectly shared review capacity: %#v", result.Failures)
	}
}

func TestReceiptRegisterConcurrentQADesignReservationsDoNotShareReviewCapacity(t *testing.T) {
	for i := 0; i < 25; i++ {
		worktree := t.TempDir()
		runDir := filepath.Join(worktree, ".gates", "runs", "run")
		dispatchDir := receiptProofDir(worktree, runDir, "dispatch")
		for completed := 1; completed <= 2; completed++ {
			writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", completed)), dispatchRegistration{
				ProofVersion: 1, DispatchID: fmt.Sprintf("dispatch-%d", completed), Provider: "codex",
				WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", completed), Gate: "qa-test-gate", Stage: "Design",
				ReviewArtifact:  fmt.Sprintf(".gates/runs/run/restricted/review-%d.json", completed),
				ReceiptArtifact: fmt.Sprintf(".gates/runs/run/restricted/proofs/receipt-%d.json", completed), Status: "finalized",
			})
		}
		codes, output := runSimultaneousReceiptRegistrations(t, worktree, runDir, [2]string{
			".gates/runs/run/restricted/third-a.json",
			".gates/runs/run/restricted/third-b.json",
		})
		if codes != [2]int{0, 0} {
			t.Fatalf("pair %d incorrectly shared review capacity: codes=%v output=%q", i+1, codes, output)
		}
		completed, open, err := reviewReservationCounts(worktree, runDir, "wf", "qa-test-gate", "Design")
		if err != nil || completed != 2 || open != 2 {
			t.Fatalf("pair %d committed the wrong capacity state: completed=%d open=%d err=%v", i+1, completed, open, err)
		}
	}
}

func TestStoppedUnfinalizedReviewReleasesDispatchCapacityButCannotCommitFourth(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	dispatchDir := receiptProofDir(dir, runDir, "dispatch")
	for i := 1; i <= 2; i++ {
		writeJSONTest(t, filepath.Join(dispatchDir, fmt.Sprintf("completed-%d.json", i)), dispatchRegistration{ProofVersion: 1, DispatchID: fmt.Sprintf("done-%d", i), Provider: "codex", WorkflowID: "wf", ChangeSnapshot: fmt.Sprintf("snap-%d", i), Gate: "complexity-gate", ReviewArtifact: fmt.Sprintf(".gates/runs/wf/restricted/done-%d.json", i), ReceiptArtifact: fmt.Sprintf(".gates/runs/wf/restricted/proofs/done-%d.json", i), Status: "finalized"})
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
	writeJSONTest(t, filepath.Join(dispatchDir, "completed-3.json"), dispatchRegistration{ProofVersion: 1, DispatchID: "done-3", Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap-final", Gate: "complexity-gate", ReviewArtifact: ".gates/runs/wf/restricted/done-3.json", ReceiptArtifact: ".gates/runs/wf/restricted/proofs/done-3.json", Status: "finalized"})
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "review limit reached") {
		t.Fatalf("stopped attempt committed a fourth unauthorized review: %#v", result.Failures)
	}
}

func TestReceiptRegisterWithoutRunDirRejectsRepositoryArtifact(t *testing.T) {
	dir := t.TempDir()
	if _, result := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: "review.json"}); result.OK() || !strings.Contains(resultSummary(result), "outside active run") {
		t.Fatalf("repository-level review output was accepted: %#v", result.Failures)
	}
	if isDir(filepath.Join(dir, ".gates", "proofs")) {
		t.Fatal("rejected registration wrote repository-level proofs")
	}
}

func TestReceiptRegisterConcurrentReservationProcesses(t *testing.T) {
	for i := 0; i < 100; i++ {
		worktree := t.TempDir()
		runDir := filepath.Join(worktree, ".gates", "runs", "run")
		artifact := ".gates/runs/run/restricted/shared.json"
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
	runDir := filepath.Join(worktree, ".gates", "runs", "run")
	codes, output := runSimultaneousReceiptRegistrations(t, worktree, runDir, [2]string{
		".gates/runs/run/restricted/first.json",
		".gates/runs/run/restricted/second.json",
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
	runDir := filepath.Join(worktree, ".gates", "runs", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Design", Artifact: ".gates/runs/run/restricted/review.json", QACaseCount: 1})
	dispatchProofs := receiptProofDir(worktree, runDir, "dispatch")
	mustWrite(t, dispatchProofs, "blocks dispatch directory creation\n")
	if _, result := ReceiptRegisterDispatch(options); result.OK() {
		t.Fatal("expected registration write failure")
	}
	if err := os.Remove(dispatchProofs); err != nil {
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
			if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(resultSummary(result), "already exists") {
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

func TestReceiptFinalizeRejectsAIChangesToScriptOwnedFields(t *testing.T) {
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
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(resultSummary(result), "requires CLI semantic submission") {
		t.Fatalf("AI changes to script-owned fields were accepted: %#v", result.Failures)
	}
}

func TestReceiptPreflightReportsCodexLifecycleUnavailable(t *testing.T) {
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
	if report.Status != "HOST_LIFECYCLE_UNAVAILABLE" {
		t.Fatalf("unexpected status: %#v", report)
	}
	if report.ConfigPath != "" || len(report.RequiredLifecycleEvents) != 0 {
		t.Fatalf("Codex preflight claimed lifecycle configuration: %#v", report)
	}
	if len(report.ConfiguredLifecycleHooks) != 0 || len(report.Missing) != 0 {
		t.Fatalf("Codex preflight treated unavailable lifecycle as missing configuration: %#v", report)
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

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file bytes changed: %s", path)
	}
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
		bundleName := "receipt-context-bundle.json"
		defaultBundlePath := filepath.Join(runDir, "restricted", bundleName)
		if data, readErr := os.ReadFile(defaultBundlePath); readErr == nil {
			var existing ContextBundle
			if strictContractJSON(data, &existing) != nil || existing.WorkflowID != options.WorkflowID || existing.ChangeSnapshot != options.ChangeSnapshot {
				bundleName = "receipt-context-bundle-" + sha256Bytes([]byte(options.WorkflowID + "\n" + options.ChangeSnapshot))[:12] + ".json"
			}
		}
		inputPath := filepath.Join(runDir, "restricted", "receipt-context.txt")
		mustWrite(t, inputPath, "context\n")
		options.ContextBundle = filepath.ToSlash(filepath.Join("restricted", bundleName))
		writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(options.ContextBundle)), ContextBundle{
			BundleVersion: 1, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
			Inputs: []EvidenceRef{{Path: "restricted/receipt-context.txt", SHA256: sha256File(inputPath)}},
		})
	}
	bundlePath := filepath.Join(runDir, filepath.FromSlash(options.ContextBundle))
	bundleRef := EvidenceRef{Path: options.ContextBundle, SHA256: sha256File(bundlePath)}
	if _, err := writeCompositionProof(options.Worktree, runDir, "context-bundle.v1", options.WorkflowID, options.ChangeSnapshot, bundlePath, []EvidenceRef{bundleRef}); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	if qaDesignLifecycle(options.Gate, options.Stage) && options.QACaseCount == 0 {
		options.QACaseCount = 1
	}
	if options.Gate == "qa-test-gate" && normalizeStage(options.Stage) == "Carry" && options.TransitionChain == "" {
		for _, name := range []string{"carry-changed.txt", "carry-verification.txt"} {
			mustWrite(t, filepath.Join(runDir, "restricted", name), name+"\n")
		}
		options.TransitionChain = "restricted/carry-transition.json"
		writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(options.TransitionChain)), TransitionChain{SchemaVersion: 2, WorkflowID: options.WorkflowID, TargetSnapshot: options.ChangeSnapshot, Hops: []TransitionHop{{FromSnapshot: "source", ToSnapshot: options.ChangeSnapshot, ChangedFiles: testRef(t, runDir, "restricted/carry-changed.txt"), Verification: testRef(t, runDir, "restricted/carry-verification.txt")}}})
		closure := "restricted/carry-source-qa.json"
		rootArtifact := "restricted/source.json"
		mustWrite(t, filepath.Join(runDir, filepath.FromSlash(rootArtifact)), "{}\n")
		writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(closure)), EvidenceClosure{SchemaVersion: 2, WorkflowID: options.WorkflowID, ChangeSnapshot: "source", Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS", RootRole: "QA_EXECUTION", RootArtifact: rootArtifact, Entries: []ClosureEntry{{Path: rootArtifact, SHA256: sha256File(filepath.Join(runDir, filepath.FromSlash(rootArtifact))), References: []string{}}}})
		options.CarrySourceClosures = []string{closure}
	}
	if options.Gate == "qa-test-gate" && normalizeStage(options.Stage) == "Carry" && options.TransitionChain != "" {
		chainPath := filepath.Join(runDir, filepath.FromSlash(options.TransitionChain))
		chainRef := EvidenceRef{Path: options.TransitionChain, SHA256: sha256File(chainPath)}
		if _, err := writeCompositionProof(options.Worktree, runDir, "transition-chain.v1", options.WorkflowID, options.ChangeSnapshot, chainPath, []EvidenceRef{chainRef}); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	policyID := receiptTestPolicyID(options)
	if policy, ok := policyByID(policyID); ok {
		if policy.ChangedFilesRequired && options.ChangedFiles == "" {
			options.ChangedFiles = "restricted/receipt-changed.txt"
			mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.ChangedFiles)), "changed\n")
		}
		if policy.VerificationRequired && options.Verification == "" {
			options.Verification = "restricted/receipt-verification.txt"
			mustWrite(t, filepath.Join(runDir, filepath.FromSlash(options.Verification)), "verified\n")
		}
	}
	for logical, composer := range map[string]string{options.ChangedFiles: "changed-files.v1", options.Verification: "verification.v1"} {
		if logical == "" {
			continue
		}
		path := filepath.Join(runDir, filepath.FromSlash(logical))
		ref := EvidenceRef{Path: logical, SHA256: sha256File(path)}
		if _, err := writeCompositionProof(options.Worktree, runDir, composer, options.WorkflowID, options.ChangeSnapshot, path, []EvidenceRef{ref}); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
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
	policyID := receiptTestPolicyID(options)
	if policyID == "" && len(policies) > 0 {
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

func TestReceiptRegisterBindsQADesignReviewPromptToGeneratedCaseSet(t *testing.T) {
	root := t.TempDir()
	workflowID, snapshot := "wf", "snap"
	runDir := filepath.Join(root, ".gates", "runs", workflowID)
	caseSet, _, err := writeCanaryDesignReviewClosure(root, runDir, workflowID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	designReceipt := testDesignReceiptForCaseSet(t, root, runDir, caseSet)
	register := func(name, target, casePath, receiptPath string) Result {
		t.Helper()
		artifact := filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, "restricted", "qa-design", name+".json"))
		prompt := filepath.ToSlash(filepath.Join("restricted", "qa-design", name+".txt"))
		_, result := PrepareDispatchPrompt(PrepareDispatchPromptOptions{Root: root, OutputFile: filepath.Join(runDir, filepath.FromSlash(prompt)), Gate: "qa-test-gate", Stage: "Design Review", CurrentRequirement: "requirements/current.md", CurrentDiff: target, Worktree: root, ChangeSnapshot: snapshot, ReviewArtifact: artifact, PolicyID: "qa.design-review.v2", ContextBundle: filepath.Join(runDir, "restricted", "design-bundle.json")})
		if !result.OK() {
			return result
		}
		_, result = ReceiptRegisterDispatch(ReceiptRegisterOptions{Worktree: root, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design Review", Artifact: artifact, ContextBundle: "restricted/design-bundle.json", Prompt: prompt, QADesignCaseSet: casePath, QADesignReceipt: receiptPath})
		return result
	}
	target := filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, caseSet.Path))
	if result := register("bound", target, caseSet.Path, designReceipt.Path); !result.OK() {
		t.Fatalf("exact generated QA case binding was rejected: %#v", result.Failures)
	}
	for name, paths := range map[string][2]string{
		"missing case set":       {"", designReceipt.Path},
		"missing Design receipt": {caseSet.Path, ""},
	} {
		if result := register(name, target, paths[0], paths[1]); result.OK() {
			t.Fatalf("%s binding was accepted", name)
		}
	}
	otherArtifact := filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, "restricted", "qa-design", "other-cases.md"))
	otherReceipt, err := writeCanaryReceiptBoundOutput(root, runDir, workflowID, snapshot, "Design", otherArtifact, "restricted/design-bundle.json", "other-designer", func() error {
		_, submitResult := ReceiptSubmit(ReceiptSubmitOptions{Worktree: root, Artifact: otherArtifact, DesignCases: []ReceiptSemanticDesignCase{{Position: 1, Values: designSemanticValues("other")}}})
		if !submitResult.OK() {
			return fmt.Errorf("%s", resultSummary(submitResult))
		}
		return nil
	}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	otherTarget := otherArtifact
	if result := register("different-case", otherTarget, caseSet.Path, designReceipt.Path); result.OK() {
		t.Fatal("prompt targeting a different generated case set was accepted")
	}
	if result := register("mismatched-receipt", target, caseSet.Path, otherReceipt.Path); result.OK() {
		t.Fatal("case set paired with another generated Design receipt was accepted")
	}
}

func designSemanticValues(prefix string) []string {
	return []string{
		prefix + " claim", prefix + " source", prefix + " action", prefix + " oracle",
		prefix + " failure signal", prefix + " evidence", prefix + " gap",
	}
}

func testDesignReceiptForCaseSet(t *testing.T, root, runDir string, caseSet EvidenceRef) EvidenceRef {
	t.Helper()
	var found EvidenceRef
	err := filepath.WalkDir(filepath.Join(runDir, "restricted", "proofs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var receipt reviewerProofReceipt
		if strictContractJSON(data, &receipt) == nil && receipt.Gate == "qa-test-gate" && normalizeStage(receipt.Stage) == "Design" && samePath(resolvePath(root, receipt.ReviewArtifact), resolvePath(runDir, caseSet.Path)) {
			found = EvidenceRef{Path: filepath.ToSlash(strings.TrimPrefix(path, runDir+string(filepath.Separator))), SHA256: sha256File(path)}
		}
		return nil
	})
	if err != nil || found.Path == "" {
		t.Fatalf("cannot locate finalized Design receipt: err=%v ref=%#v", err, found)
	}
	return found
}

func receiptTestPolicyID(options ReceiptRegisterOptions) string {
	if options.Gate == "qa-test-gate" && normalizeStage(options.Stage) == "Design Review" {
		return "qa.design-review.v2"
	}
	if options.Gate == "qa-test-gate" && normalizeStage(options.Stage) == "Carry" {
		return "carry.arbiter.v2"
	}
	if options.Gate == "complexity-gate" && options.ChangedFiles == "" && options.Verification == "" {
		return "complexity.start-readiness.v2"
	}
	if options.Gate == "architecture-health-gate" && options.ChangedFiles == "" && options.Verification == "" {
		return "architecture.start-readiness.v2"
	}
	return map[string]string{"complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2"}[options.Gate]
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

func writeReceiptArtifactFixture(t *testing.T, dir, artifact, snapshot string) {
	t.Helper()
	path := resolvePath(dir, artifact)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	checks := make([]ReceiptSemanticCheck, 0, len(payload.Checks))
	for i := range payload.Checks {
		status, message := "PASS", "checked"
		checks = append(checks, ReceiptSemanticCheck{Position: i + 1, Status: status, Message: message})
	}
	if _, result := ReceiptSubmit(ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: checks}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if envelope.ChangeSnapshot != snapshot {
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := strictContractJSON(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if err := strictContractJSON(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		envelope.ChangeSnapshot = snapshot
		writeEnvelopeTest(t, path, envelope, payload)
	}
}

func receiptSemanticChecks(t *testing.T, dir, artifact, status string) []ReceiptSemanticCheck {
	t.Helper()
	data, err := os.ReadFile(resolvePath(dir, artifact))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	checks := make([]ReceiptSemanticCheck, 0, len(payload.Checks))
	for index := range payload.Checks {
		checks = append(checks, ReceiptSemanticCheck{Position: index + 1, Status: status, Message: "Semantic review completed."})
	}
	return checks
}

func writeReviewerSemanticFixture(t *testing.T, path string, source ReviewerPayload) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var generated ReviewerPayload
	if err := strictContractJSON(envelope.Payload, &generated); err != nil {
		t.Fatal(err)
	}
	semantic := map[string]ReviewCheck{}
	for _, check := range source.Checks {
		semantic[check.ID] = check
	}
	options := ReceiptSubmitOptions{}
	for i := range generated.Checks {
		check, ok := semantic[generated.Checks[i].ID]
		if !ok {
			t.Fatalf("missing semantic check %s", generated.Checks[i].ID)
		}
		options.Checks = append(options.Checks, ReceiptSemanticCheck{Position: i + 1, Status: check.Status, Message: check.Message})
		for _, finding := range check.Findings {
			options.Findings = append(options.Findings, ReceiptSemanticFinding{CheckPosition: i + 1, Message: finding.Message})
			findingPosition := len(options.Findings)
			for _, location := range finding.Locations {
				options.Locations = append(options.Locations, ReceiptSemanticLocation{FindingPosition: findingPosition, Path: location.Path, StartLine: location.StartLine, EndLine: location.EndLine})
			}
		}
	}
	options.Worktree, options.Artifact = receiptFixtureArtifactLocation(t, path)
	if _, result := ReceiptSubmit(options); !result.OK() {
		t.Fatal(result.Failures)
	}
}

func writeCarrySemanticFixture(t *testing.T, path string, source CarryPayload) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var generated CarryPayload
	if err := strictContractJSON(envelope.Payload, &generated); err != nil {
		t.Fatal(err)
	}
	semantic := map[string]CarryDecision{}
	for _, decision := range source.Decisions {
		semantic[decision.Gate] = decision
	}
	options := ReceiptSubmitOptions{}
	for i := range generated.Decisions {
		decision, ok := semantic[generated.Decisions[i].Gate]
		if !ok {
			t.Fatalf("missing semantic Carry decision %s", generated.Decisions[i].Gate)
		}
		options.CarryDecisions = append(options.CarryDecisions, ReceiptSemanticCarryDecision{GatePosition: i + 1, Decision: decision.Decision, Reason: decision.Reason})
	}
	options.Worktree, options.Artifact = receiptFixtureArtifactLocation(t, path)
	if _, result := ReceiptSubmit(options); !result.OK() {
		t.Fatal(result.Failures)
	}
}

func receiptFixtureArtifactLocation(t *testing.T, path string) (string, string) {
	t.Helper()
	abs := absPath(path)
	marker := string(filepath.Separator) + filepath.Join(".gates", "runs") + string(filepath.Separator)
	index := strings.Index(abs, marker)
	if index < 0 {
		t.Fatalf("review fixture is outside a workflow run: %s", path)
	}
	root := abs[:index]
	return root, relativePath(root, abs)
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
		Stage:          "Design",
		QACaseCount:    1,
		Artifact:       os.Getenv("FORMAL_GATES_RECEIPT_REGISTER_ARTIFACT"),
		ContextBundle:  "restricted/receipt-context-bundle.json",
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
