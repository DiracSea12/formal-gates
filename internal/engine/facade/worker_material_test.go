package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

func workerMaterialFacade(t *testing.T) (*Facade, string) {
	t.Helper()
	root := t.TempDir()
	write := func(path string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("requirements.md", []byte(validationDocument))
	write("app.go", []byte("package main\nfunc main() {}\n"))
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "add", ".")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Initial command")
	base := run("rev-parse", "HEAD")
	sum := sha256.Sum256([]byte(validationDocument))
	revision := hex.EncodeToString(sum[:])
	receipt := testReceipt()
	receipt.RequirementRevision = revision
	receipt.Artifacts[0].Revision = revision
	receipt.SolutionRevision = revision
	receipt.SolutionDigest = "sha256:" + revision
	admission := testAdmission(t, root, "worker-target", "sha256:worker-package", "zcode")
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "worker-material", Route: "regular", Provider: "zcode", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, Admission: admission, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	return f, root
}

func TestWorkerMaterialDriveAndReadOnlyRecovery(t *testing.T) {
	f, root := workerMaterialFacade(t)
	first, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.WorkerMaterials) != 1 || first.Validation == nil || first.Validation.Document.Content != validationDocument {
		t.Fatal("first drive lost confirmed document or issued material")
	}
	var ref WorkerMaterial
	for _, value := range first.WorkerMaterials {
		ref = value
	}
	if filepath.Dir(ref.Path) != filepath.Join(f.stateDir, "materials") || filepath.Base(ref.Path) != strings.TrimPrefix(ref.Digest, "sha256:")+".json" {
		t.Fatal("material path differs from its existing content identity")
	}
	body, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	if material.Input.Pending.ActionID != ref.ActionID || material.Input.Pending.AttemptID != ref.AttemptID || material.Input.Review.Action != review.ProductReview || material.Input.Review.RequirementRevision != first.Review.RequirementRevision || !strings.Contains(material.Prompt, "[Shared reviewer contract]") || !strings.Contains(material.Prompt, "独立审查者契约") {
		t.Fatal("material lost actual binding or shared source contract")
	}
	state := readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))
	if _, err := f.Show(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Status(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Next(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(state, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
		t.Fatal("read-only entry wrote state")
	}
	second, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision || len(second.WorkerMaterials) != 1 || !bytes.Equal(body, readWorkerFile(t, ref.Path)) {
		t.Fatal("repeated drive changed requirement fact/material")
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != first.Revision || !bytes.Equal(body, readWorkerFile(t, ref.Path)) {
		t.Fatal("reopened drive changed issued context")
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument+"\nchanged on disk"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err = reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if again.Validation.Document.Content != validationDocument || !bytes.Equal(body, readWorkerFile(t, ref.Path)) {
		t.Fatal("later disk content replaced confirmed material")
	}
}

func TestWorkerMaterialRejectsUnconfirmedFirstDriveContent(t *testing.T) {
	f, root := workerMaterialFacade(t)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument+"\nchanged before drive"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err == nil {
		t.Fatal("different file bytes became confirmed requirement")
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State.Validation != nil {
		t.Fatal("failed registration froze different document")
	}
}

func readWorkerFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestWorkerNativeResultUsesOriginalDigestAndReplay(t *testing.T) {
	for _, boundary := range []string{"normal", "terminal", "terminal-reopen", "replaced"} {
		t.Run(boundary, func(t *testing.T) {
			f, _ := workerMaterialFacade(t)
			run, err := f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			var ref WorkerMaterial
			for _, value := range run.WorkerMaterials {
				ref = value
			}
			var material worker.Material
			if err := json.Unmarshal(readWorkerFile(t, ref.Path), &material); err != nil {
				t.Fatal(err)
			}
			correlation := "dispatch-lifecycle-key"
			spawn, err := protocol.NewSpawnReceiptEvent("spawn", ref.ActionID, "zcode", correlation, protocol.SpawnStatusSpawned)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.Submit(spawn); err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			session := "native-session-different-from-dispatch"
			payload := worker.Review{Findings: []worker.Finding{}, Verification: []string{}}
			for _, key := range review.RequiredVerificationChecks() {
				payload.Verification = append(payload.Verification, "verified full current requirement: "+key)
			}
			parsed, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			raw := append([]byte("\n"), parsed...)
			write := func(name string, value any) {
				t.Helper()
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("native-request.json", map[string]any{"prompt": material.Prompt, "workspace": f.root, "read_only": material.ReadOnly})
			write("native-source.json", map[string]any{"sessionId": session})
			if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "answer.raw.txt"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "answer.json"), parsed, 0600); err != nil {
				t.Fatal(err)
			}
			files := map[string]any{}
			for _, name := range []string{"native-source.json", "native-events.jsonl", "answer.raw.txt", "answer.json"} {
				proof, refusal := harness.FileProof(filepath.Join(directory, name))
				if refusal != "" {
					t.Fatal(refusal)
				}
				files[name] = map[string]any{"sha256": proof.SHA256, "size": proof.Size}
			}
			source := map[string]any{"provider": "zcode", "identity": session, "files": files}
			descriptor, _ := host.Lookup("zcode")
			startPayload, _ := json.Marshal(map[string]any{"tool_use_id": correlation})
			if _, err := lifecycle.Capture(f.root, descriptor.ID, descriptor.LifecycleEvents[0], startPayload); err != nil {
				t.Fatal(err)
			}
			stopPayload, _ := json.Marshal(map[string]any{"tool_use_id": correlation, "transcript_path": filepath.Join(directory, "native-events.jsonl")})
			if _, err := lifecycle.Capture(f.root, descriptor.ID, descriptor.LifecycleEvents[1], stopPayload); err != nil {
				t.Fatal(err)
			}

			if boundary == "replaced" {
				interruption, err := protocol.NewWorkerResultEvent("worker-interrupted", ref.ActionID, "zcode", protocol.OutcomeRuntimeError, "sha256:interruption", authoring.FailureTransientEngine)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := f.Submit(interruption); err != nil {
					t.Fatal(err)
				}
				snap, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if snap.State.ObsoleteActions[ref.ActionID].ReplacedBy == "" {
					t.Fatal("normal runtime interruption did not replace the issued attempt")
				}
			} else if boundary != "normal" {
				progress, err := protocol.NewTaskEvent("task-terminal", material.Input.Pending.Task, material.Input.Pending.AttemptID, runtime.TaskTerminal)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := f.Submit(progress); err != nil {
					t.Fatal(err)
				}
				snapshot, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, present := snapshot.State.PendingActions[ref.ActionID]; present || !snapshot.State.EndedAttempts[material.Input.Pending.Task].AwaitingResult {
					t.Fatal("terminal-before-result precondition did not retain the actual ended attempt")
				}
				if boundary == "terminal-reopen" {
					f, err = Open(f.root, f.runID)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			accepted, _, err := f.SubmitWorkerResult("worker-result", ref, directory, source, harness.NativeWorkerResultEvidence)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(raw)
			want := "sha256:" + hex.EncodeToString(hash[:])
			if boundary == "replaced" {
				if accepted.Status != "OBSOLETE_RESULT" {
					t.Fatalf("original attempt result bypassed core obsolete admission: %+v", accepted)
				}
				found := false
				for _, result := range snapshot.State.ObsoleteResults[ref.ActionID] {
					found = found || result.PayloadDigest == want
				}
				if !found || len(snapshot.State.ReviewSeries.ProductReview.RawCandidates) != 0 {
					t.Fatal("obsolete evidence was lost or advanced review authority")
				}
				return
			}
			result := snapshot.State.Results[ref.ActionID]
			t.Logf("acceptance=%+v result=%+v staged=%+v receipt=%+v", accepted, result, snapshot.State.StagedResults[ref.ActionID], snapshot.State.SpawnReceipts[ref.ActionID])
			if result.PayloadDigest != want || snapshot.State.SpawnReceipts[ref.ActionID].Correlation != correlation {
				t.Fatal("raw evidence digest or lifecycle correlation was rewritten")
			}
			before := readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))
			repeated, _, err := f.SubmitWorkerResult("worker-result", ref, directory, source, harness.NativeWorkerResultEvidence)
			if err != nil || !reflect.DeepEqual(repeated, accepted) || !bytes.Equal(before, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
				t.Fatalf("normal repeated native result bypassed core idempotence: %v", err)
			}
			reopened, err := Open(f.root, f.runID)
			if err != nil {
				t.Fatal(err)
			}
			repeated, _, err = reopened.SubmitWorkerResult("worker-result", ref, directory, source, harness.NativeWorkerResultEvidence)
			if err != nil || !reflect.DeepEqual(repeated, accepted) || !bytes.Equal(before, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
				t.Fatalf("lost-response/reopen native result changed state: %v", err)
			}
			duplicate, _, err := reopened.SubmitWorkerResult("worker-result-other-id", ref, directory, source, harness.NativeWorkerResultEvidence)
			if err != nil || duplicate.Status != "DUPLICATE" {
				t.Fatalf("same native payload with a different event ID bypassed core duplicate admission: %+v %v", duplicate, err)
			}
			duplicateState := readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))
			replay, _, err := reopened.SubmitWorkerResult("worker-result-other-id", ref, directory, source, harness.NativeWorkerResultEvidence)
			if err != nil || !reflect.DeepEqual(replay, duplicate) || !bytes.Equal(duplicateState, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
				t.Fatalf("duplicate event replay did not retain its core record: %+v %v", replay, err)
			}
			snap, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.Results[ref.ActionID].PayloadDigest != want || len(snap.State.ReviewSeries.ProductReview.RawCandidates) != 1 {
				t.Fatal("duplicate native answer created a second completion or candidate")
			}
		})
	}

}

func TestWorkerIntakeWaitsForResetConfirmation(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	event, err := protocol.NewRequestEvent("request-reset", protocol.ControlReset, protocol.AskOption{ID: "proceed", Label: "Reset with preview"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.Submit(event)
	if err != nil {
		t.Fatal(err)
	}
	ask := run.PendingAsks[0]
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := snap.State.PendingAsks[ask.RequestID]
	token, err := f.Freshness(ask.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	decide := protocol.Event{ID: "decide-reset", Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: token, Choice: "proceed", Reset: &protocol.ResetDecision{PreviewDigest: pending.Reset.Digest, Reason: "reset before first drive"}}}
	if _, _, err := f.Submit(decide); err != nil {
		t.Fatal(err)
	}
	shown, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if shown.Next.Kind != "ASK" {
		t.Fatal("reset confirmation ask was not projected")
	}
	first, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Next, shown.Next) || len(first.WorkerMaterials) != 0 || first.Validation != nil {
		t.Fatal("automatic intake crossed the reset confirmation boundary")
	}
	before := readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
		t.Fatal("waiting drive created a second confirmation fact")
	}
	reopened, err := Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Drive(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readWorkerFile(t, filepath.Join(f.stateDir, "state.json"))) {
		t.Fatal("reopened intake crossed pending reset confirmation")
	}
}

func TestRequirementChangeBeforeFirstDrivePreservesConfirmationBarrier(t *testing.T) {
	f, root := workerMaterialFacade(t)
	document := validationDocument + "\n"
	sum := sha256.Sum256([]byte(document))
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: hex.EncodeToString(sum[:]), DeltaDigest: "sha256:document-formatting", ImpactDigest: "sha256:unchanged-command", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved}
	event, err := protocol.NewRequirementRequestEvent("document-confirmation", pkg, protocol.AskOption{ID: "proceed", Label: "Confirm complete document"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(event); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.PendingRequirementAsk == nil || run.PendingRequirementAsk.RequestID != string(event.ID) || run.Next.Kind != "ASK" {
		t.Fatalf("unconfirmed document lost its human boundary: %+v", run.Next)
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.State.RequirementBarrierActive() || snapshot.State.Validation != nil || len(snapshot.State.PendingActions) != 0 {
		t.Fatal("unconfirmed document acquired inventory or dispatch authority")
	}
	for _, record := range snapshot.State.Events {
		if record.Acceptance.Kind == string(protocol.KindOperatorObservation) {
			t.Fatal("automatic requirement observation crossed the confirmation barrier")
		}
	}
}
