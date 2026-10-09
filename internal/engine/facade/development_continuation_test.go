package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

func TestDevelopmentFailedCheckRetainsIssuedObligation(t *testing.T) {
	d := nativeMasterDriver(t)
	plan := commandPlan()
	plan.Batches[0].Artifacts = []string{"app.go", "handoff.txt"}
	plan.Batches[0].Checks = []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "printf 'build: actual source check\\n'; grep -q '^// ready$' app.go"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "printf 'tests: actual source check\\n'; grep -q '^// ready$' app.go"}}
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one declared artifact"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	id, input := d.action("engine.development.batch")
	issued := driverState(t, d).Attempts[input.Binding.Task]
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// unfinished\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "handoff.txt"), []byte("retained delivery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.receipt(id)
	originalMaterial := d.run.WorkerMaterials[id]
	completion := batchEvidence(input).Completion
	original := submitDevelopmentSemanticFixture(t, d, originalMaterial, worker.Development{Paths: input.Development.Batch.Artifacts, Summary: "delivered edit; approved source checks still own completion", Completion: completion}, "fixture-native-session")
	d.drive()
	failed := driverState(t, d)
	var failedReceipt protocol.HostActionReceipt
	for _, receipt := range failed.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.AdapterEvidence == nil {
			continue
		}
		body, _ := json.Marshal(receipt.AdapterEvidence.Values)
		var check protocol.CheckEvidence
		if json.Unmarshal(body, &check) == nil && check.SourceActionID == id && check.ExitCode == 1 {
			failedReceipt = receipt
		}
	}
	if failedReceipt.ActionID == "" {
		t.Fatal("fixture did not observe a real failing approved check")
	}
	if failed.TaskStatusOf(input.Binding.Task) == runtime.TaskTerminal || failed.PendingActions[id].AttemptID != issued.ID || failed.Attempts[input.Binding.Task].ID != issued.ID {
		t.Fatalf("unfinished batch lost its original action/attempt: task=%s pending=%+v attempt=%+v", failed.TaskStatusOf(input.Binding.Task), failed.PendingActions[id], failed.Attempts[input.Binding.Task])
	}
	if _, ok := d.run.WorkerMaterials[id]; !ok {
		t.Fatal("original developer has no completion material after its actual check failure")
	}
	result := failed.Results[id]
	failedBytes, _ := json.Marshal(failedReceipt)
	spawnBytes, _ := json.Marshal(failed.SpawnReceipts[id])
	before, _ := json.Marshal(failed)
	d.submit(original)
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(before, after) {
		t.Fatal("duplicate edit changed failed source or original dispatch")
	}
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() { defer group.Done(); _, err := d.f.Drive(); errors <- err }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	d.reopen()
	d.drive()
	continuation := d.run.WorkerMaterials[id]
	if continuation.AttemptID != originalMaterial.AttemptID || continuation.Digest == originalMaterial.Digest {
		t.Fatal("continuation did not retain original attempt with new immutable feedback material")
	}
	body, err := os.ReadFile(continuation.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	feedback := material.Input.Validation.Development
	if !reflect.DeepEqual(feedback.Batch, input.Development.Batch) || feedback.Result == nil || feedback.Result.Digest != result.Digest || len(feedback.Checks) != 1 || feedback.Checks[0].ExitCode != 1 || feedback.Checks[0].Directory == "" {
		t.Fatalf("continuation lacks exact confirmed batch and authentic failed-check feedback: %+v", feedback)
	}
	if material.Input.Validation.Binding != input.Binding || material.Input.Pending.ActionID != id || material.Input.Pending.AttemptID != issued.ID {
		t.Fatal("continuation changed issued work identity")
	}
	stdout, err := os.ReadFile(filepath.Join(feedback.Checks[0].Directory, "stdout.log"))
	if err != nil || !strings.Contains(string(stdout), "build: actual source check") {
		t.Fatalf("original failed stdout: %s %v", stdout, err)
	}
	if !strings.Contains(material.Prompt, "original workspace and native session") {
		t.Fatal("material does not keep batch continuation in original session")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// incomplete change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := submitDevelopmentSemanticFixture(t, d, continuation, worker.Development{Paths: []string{"app.go"}, Summary: "changed original implementation; actual checks still own completion", Completion: completion}, "fixture-native-session")
	d.drive()
	second := driverState(t, d)
	if second.Results[id].Digest == result.Digest || second.PendingActions[id].AttemptID != issued.ID {
		t.Fatal("next failed edit changed dispatch or did not advance source")
	}
	stale := next
	stale.ID = d.eventID()
	payload := *next.Result
	stale.Result = &payload
	stale.Result.PayloadDigest = "late payload based on obsolete feedback"
	priorState, _ := json.Marshal(second)
	if _, _, err := d.f.Submit(stale); err == nil || !strings.Contains(err.Error(), "RECEIPT_CONFLICT") {
		t.Fatalf("late old-feedback result not rejected: %v", err)
	}
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(priorState, after) {
		t.Fatal("old-feedback result changed current source or completed duty")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	final := submitDevelopmentSemanticFixture(t, d, d.run.WorkerMaterials[id], worker.Development{Paths: []string{"app.go"}, Summary: "completed original authorized implementation", Completion: completion}, "fixture-native-session")
	d.drive()
	completed := driverState(t, d)
	if completed.TaskStatusOf(input.Binding.Task) != runtime.TaskTerminal || completed.PendingActions[id].ActionID != "" || len(completed.RecoverableResults[id]) != 2 || completed.RecoverableResults[id][0].Digest != result.Digest {
		t.Fatal("actual checks did not settle original batch with retained edit evidence")
	}
	if completed.Results[id].Digest == result.Digest || !slices.Equal(completed.Results[id].Validation.Development.Paths, []string{"app.go", "handoff.txt"}) {
		t.Fatal("new result lost its source identity or programmatically retained delivery paths")
	}
	if receiptBytes, _ := json.Marshal(completed.HostActionReceipts[failedReceipt.ActionID]); !bytes.Equal(receiptBytes, failedBytes) {
		t.Fatal("old failure receipt was overwritten")
	}
	if receiptBytes, _ := json.Marshal(completed.SpawnReceipts[id]); !bytes.Equal(receiptBytes, spawnBytes) {
		t.Fatal("batch continuation respawned or changed native identity")
	}
	passed := 0
	for _, receipt := range completed.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.AdapterEvidence == nil {
			continue
		}
		body, _ := json.Marshal(receipt.AdapterEvidence.Values)
		var check protocol.CheckEvidence
		if json.Unmarshal(body, &check) == nil && check.SourceActionID == id && check.ExitCode == 0 {
			if check.SourceIdentity == feedback.Checks[0].SourceIdentity || check.SourceDigest == feedback.Checks[0].SourceDigest {
				t.Fatal("new checks reused failed immutable source")
			}
			passed++
		}
	}
	if passed != 2 {
		t.Fatalf("approved full checks not executed on new source: %d", passed)
	}
	if issued.Attempts != failed.Attempts[input.Binding.Task].Attempts || failed.Attempts[input.Binding.Task].RetryExhausted {
		t.Fatal("business continuation consumed runtime retry allowance")
	}
	settled, _ := json.Marshal(completed)
	final.ID = d.eventID()
	if acceptance := d.submit(final); acceptance.Status != "DUPLICATE" {
		t.Fatalf("final edit replay: %+v", acceptance)
	}
	next.ID = d.eventID()
	if acceptance := d.submit(next); acceptance.Status != "DUPLICATE" {
		t.Fatalf("new edit replay: %+v", acceptance)
	}
	original.ID = d.eventID()
	if acceptance := d.submit(original); acceptance.Status != "DUPLICATE" {
		t.Fatalf("old edit replay: %+v", acceptance)
	}
	// Fresh event IDs have their own immutable event receipts; business facts
	// and native effects remain identical.
	completed.Events = driverState(t, d).Events
	settled, _ = json.Marshal(completed)
	d.submit(final)
	d.submit(next)
	d.submit(original)
	d.reopen()
	d.drive()
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(settled, after) {
		t.Fatal("duplicate/reopen changed completed batch or repeated native checks")
	}
}

// Semantic worker answers and the native verifier are local test fixtures;
// candidate freezing and approved checks use the normal physical adapters.
func submitDevelopmentSemanticFixture(t *testing.T, d *validationDriver, ref WorkerMaterial, development worker.Development, session string) protocol.Event {
	t.Helper()
	body, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	answer, err := json.Marshal(worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: "PASS", Development: &development}})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	request, _ := json.Marshal(map[string]any{"prompt": material.Prompt, "workspace": material.Input.Validation.Workspace.Path, "read_only": material.ReadOnly})
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), request, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(answer)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	eventID := d.eventID()
	verify := func(provider, path string, source map[string]any) ([]byte, string, string, error) {
		descriptor, err := host.Lookup(d.host())
		if err != nil || provider != descriptor.InstallName || path != directory || source["session"] != "fixture-native-session" || session != source["session"] {
			t.Fatal("semantic fixture changed native session/provider/source")
		}
		return answer, digest, session, nil
	}
	acceptance, run, err := d.f.SubmitWorkerResult(eventID, ref, directory, map[string]any{"session": session}, verify)
	if err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("semantic result admission: %+v %v", acceptance, err)
	}
	d.run = run
	event, err := worker.DecodeResult(material.Input, eventID, answer, digest)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
