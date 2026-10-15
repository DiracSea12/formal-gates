package facade

import (
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A preparation interrupted before native owner creation can precede an
// explicitly carried requirement revision and the current owned invocation.
func TestAgentExecutionExcludesUnclaimedCarriedMaterial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox")
	action, _ := d.action("engine.development.batch.v6")
	old := d.run.WorkerMaterials[action]
	request, _, abandoned, _, err := d.f.WorkerExecutionRequest(old, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(abandoned, 0700); err != nil {
		t.Fatal(err)
	}
	if err = writeJSONAtomic(filepath.Join(abandoned, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	// These are the normal preparation writes in executeWorkflowWorker. No
	// owner was obtained when the host stopped before starting its supervisor.
	confirmValidationWork(d, "")
	current := d.run.WorkerMaterials[action]
	if current.ActionID != old.ActionID || current.AttemptID != old.AttemptID || current.Digest == old.Digest {
		t.Fatal("carry did not preserve action/attempt with new material")
	}
	request, _, directory, _, err := d.f.WorkerExecutionRequest(current, "")
	if err != nil {
		t.Fatal(err)
	}
	if directory == abandoned {
		t.Fatal("new material did not select its own directory")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err = writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	release, err := AdmitExecution(directory, request)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = owner.Close()
	}
	release()
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSONAtomic(filepath.Join(directory, "identity.json"), map[string]any{"identity": action}); err != nil {
		t.Fatal(err)
	}
	d.receipt(action)
	start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlAbort), Choice: "abort", Reason: "stop the original carried work"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	var intent protocol.HostActionIntent
	for _, item := range d.run.AdapterIntents {
		if item.Terminate != nil && item.Terminate.WorkerActionID == action {
			intent = item
		}
	}
	if intent.ActionID == "" {
		t.Fatal("abort did not issue exact termination")
	}
	_, directories, release, err := d.f.AgentExecution(intent)
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, item := range directories {
		if _, err := os.ReadFile(filepath.Join(item, "identity.json")); err != nil {
			t.Fatalf("termination consumer cannot read native identity: %v; unclaimed=%v", err, item == abandoned)
		}
	}
	if len(directories) != 1 || directories[0] != directory {
		t.Fatal("termination must consume only its original owned invocation")
	}
}

// All claimed invocations of the signed action/attempt retain their evidence
// when the host resumes the original native identity.
func TestAgentExecutionRetainsOriginalAndResumedOwners(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v6")
	claim := func(ref WorkerMaterial, resume string) string {
		t.Helper()
		request, _, directory, _, err := d.f.WorkerExecutionRequest(ref, resume)
		if err != nil {
			t.Fatal(err)
		}
		if resume == "" {
			request["binding"] = map[string]any{"identity": "original-native-configuration"}
			request["permission_mode"] = "original-permission"
		} else if !jsonValuesEqual(request["binding"], map[string]any{"identity": "original-native-configuration"}) || request["permission_mode"] != "original-permission" {
			t.Fatal("resume did not inherit the original claimed native configuration", request)
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
			t.Fatal(err)
		}
		release, err := AdmitExecution(directory, request)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = owner.Close()
		}
		release()
		if err != nil {
			t.Fatal(err)
		}
		return directory
	}
	original := claim(d.run.WorkerMaterials[action], "")
	d.receipt(action)
	start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	recovery, _, err := d.f.engine.RecoverAttempt(action, protocol.Interruption{InterruptionReason: "Native invocation interrupted before completion"}, protocol.Observation{})
	if err != nil || recovery.Action != protocol.RecoveryAsk {
		t.Fatal("original interruption did not create its recovery Ask", recovery, err)
	}
	token, err := d.f.Freshness(recovery.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(recovery.RequestID), token, "resume")
	if err != nil {
		t.Fatal(err)
	}
	d.submit(decision)
	var intent protocol.HostActionIntent
	for _, item := range d.run.AdapterIntents {
		if item.Resume != nil && item.Resume.WorkerActionID == action {
			intent = item
		}
	}
	if intent.ActionID == "" {
		t.Fatal("recovery did not sign the original resume")
	}
	state, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	refs, err := d.f.workerMaterials(state.State, true)
	if err != nil {
		t.Fatal(err)
	}
	resumed := claim(refs[action], intent.ActionID)
	_, directories, release, err := d.f.AgentExecution(intent)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(directories) != 2 || !slices.Contains(directories, original) || !slices.Contains(directories, resumed) {
		t.Fatal("signed resume lost an original claimed owner", directories, original, resumed)
	}
}
