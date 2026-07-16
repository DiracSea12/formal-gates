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
	if !strings.HasPrefix(event.EventArtifact, ".claude/gates/runs/wf/proofs/events/") {
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
	artifact := ".claude/gates/runs/active-run/review.json"
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	event, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)})
	if !result.OK() {
		t.Fatalf("run-local capture failed: %#v", result.Failures)
	}
	if !strings.HasPrefix(event.EventArtifact, ".claude/gates/runs/active-run/proofs/events/") {
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
	dispatch, registered := registerReceiptFixture(t, ReceiptRegisterOptions{Worktree: dir, RunDir: runA, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: ".claude/gates/runs/run-a/review.json"})
	if !registered.OK() {
		t.Fatal(registered.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, RunDir: runB, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)}); result.OK() || !strings.Contains(resultSummary(result), "conflicts with dispatch correlation") {
		t.Fatalf("explicit run conflict was accepted: %#v", result.Failures)
	}
	if events, err := filepath.Glob(filepath.Join(runB, "proofs", "events", "*.json")); err != nil || len(events) != 0 {
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
	if !strings.HasPrefix(dispatch.DispatchRegistrationArtifact, ".claude/gates/runs/wf/proofs/dispatch/") {
		t.Fatalf("omitted --run-dir registered outside the default run: %#v", dispatch)
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "status",
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
	if !strings.HasPrefix(receipt.ReceiptArtifact, ".claude/gates/runs/wf/proofs/") {
		t.Fatalf("omitted --run-dir finalized outside the default run: %#v", receipt)
	}
	if isDir(filepath.Join(dir, ".claude", "gates", "proofs")) {
		t.Fatal("omitted --run-dir wrote repository-level receipt proofs")
	}
	assertTypedWireRoundTrip(t, resolvePath(dir, dispatch.DispatchRegistrationArtifact), &dispatchRegistration{}, []string{
		"proofVersion", "dispatchId", "provider", "workflowId", "changeSnapshot", "gate", "stage", "reviewArtifact", "receiptArtifact", "status",
	})
	assertTypedWireRoundTrip(t, resolvePath(dir, receipt.ReceiptArtifact), &reviewerProofReceipt{}, []string{
		"proofVersion", "provider", "workflowId", "changeSnapshot", "gate", "stage", "dispatchId", "dispatchRegistrationArtifact", "dispatchRegistrationSha256", "subagentId", "normalizedEvents", "startEventArtifact", "startEventSha256", "stopEventArtifact", "stopEventSha256", "reviewArtifact", "reviewArtifactSha256",
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
	receiptData, err := os.ReadFile(receiptPath)
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
	if _, result := ReceiptRegisterDispatch(options); result.OK() || !strings.Contains(result.Failures[0].Message, "already reserved") {
		t.Fatalf("expected duplicate open reservation rejection, got %#v", result.Failures)
	}
}

func TestReceiptRegisterValidatesContextBundleBeforeReservation(t *testing.T) {
	for _, kind := range []string{"valid", "workflow", "snapshot", "empty", "duplicate", "missing", "tampered", "escape"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			inputPath := filepath.Join(runDir, "input.txt")
			mustWrite(t, inputPath, "input\n")
			ref := EvidenceRef{Path: "input.txt", SHA256: sha256File(inputPath)}
			bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{ref}}
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
				bundle.Inputs[0] = EvidenceRef{Path: "missing.txt", SHA256: strings.Repeat("0", 64)}
			case "tampered":
				mustWrite(t, inputPath, "tampered\n")
			case "escape":
				bundle.Inputs[0].Path = "../outside.txt"
			}
			writeJSONTest(t, filepath.Join(runDir, "bundle.json"), bundle)
			artifact := relativePath(dir, filepath.Join(runDir, "review.json"))
			_, result := ReceiptRegisterDispatch(ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact, ContextBundle: "bundle.json"})
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
		artifact := ".claude/gates/runs/run/shared.json"
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
		".claude/gates/runs/run/first.json",
		".claude/gates/runs/run/second.json",
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
	proofs := filepath.Join(runDir, "proofs")
	mustWrite(t, proofs, "blocks dispatch directory creation\n")
	options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Execution", Artifact: ".claude/gates/runs/run/review.json"})
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
			if err := os.MkdirAll(filepath.Join(runDir, "outputs"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("outputs", filepath.Join(runDir, "alias")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			options := withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: relativePath(dir, filepath.Join(runDir, filepath.FromSlash(test.first)))})
			if _, result := ReceiptRegisterDispatch(options); !result.OK() {
				t.Fatalf("first reservation failed: %#v", result.Failures)
			}
			options.Artifact = relativePath(dir, filepath.Join(runDir, filepath.FromSlash(test.second)))
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

func TestReceiptFinalizeRejectsCompletedArtifactSnapshotMismatch(t *testing.T) {
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
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: []byte(payload)}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", Gate: "complexity-gate", Artifact: artifact}); result.OK() || !strings.Contains(result.Failures[0].Message, "does not match dispatch binding") {
		t.Fatalf("expected completed artifact mismatch rejection, got %#v", result.Failures)
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
	if report.Status != "UNSUPPORTED_HOST_RECEIPT" {
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
	path := filepath.Join(runDir, filepath.FromSlash(name))
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
	if options.ContextBundle != "" {
		return options
	}
	runDir, err := resolveWorkflowRunDir(options.Worktree, options.WorkflowID, options.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(runDir, "receipt-context.txt")
	mustWrite(t, inputPath, "context\n")
	options.ContextBundle = "receipt-context-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, options.ContextBundle), ContextBundle{
		BundleVersion: 1, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
		Inputs: []EvidenceRef{{Path: "receipt-context.txt", SHA256: sha256File(inputPath)}},
	})
	return options
}

func writeReceiptArtifactFixture(t *testing.T, dir, artifact, snapshot string) {
	t.Helper()
	writeEnvelopeTest(t, filepath.Join(dir, artifact), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "COMPLEXITY_REVIEW", WorkflowID: "wf", ChangeSnapshot: snapshot, Gate: "complexity-gate", Stage: "", Verdict: "PASS"}, ReviewerPayload{Checks: []ReviewCheck{}})
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
		ContextBundle:  "receipt-context-bundle.json",
	})
	if result.OK() {
		os.Exit(0)
	}
	if strings.Contains(resultSummary(result), "already reserved") {
		os.Exit(10)
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
