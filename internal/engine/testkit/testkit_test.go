package testkit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func fixtureWithAction(t *testing.T) (*ProtocolFixture, string) {
	t.Helper()
	root := t.TempDir()
	fixture, err := NewProtocolFixture(root)
	if err != nil {
		t.Fatalf("new protocol fixture: %v", err)
	}
	issued, err := fixture.PrepareReady()
	if err != nil {
		t.Fatalf("prepare ready: %v", err)
	}
	if len(issued) != 1 {
		t.Fatalf("issued actions = %+v, want one action", issued)
	}
	return fixture, issued[0].ActionID
}

func TestFaultPlanIsNamedAndSingleTrigger(t *testing.T) {
	plan := NewFaultPlan()
	plan.Arm(persistence.FaultIntentAfter)
	if err := plan.Inject(persistence.FaultIntentAfter); !errors.Is(err, ErrInjected) {
		t.Fatalf("first injection = %v, want ErrInjected", err)
	}
	if err := plan.Inject(persistence.FaultIntentAfter); err != nil {
		t.Fatalf("second injection = %v, want nil", err)
	}
	if plan.Count(persistence.FaultIntentAfter) != 1 {
		t.Fatalf("consumed count = %d, want 1", plan.Count(persistence.FaultIntentAfter))
	}
	if calls := plan.Calls(); !reflect.DeepEqual(calls, []persistence.FaultPoint{persistence.FaultIntentAfter, persistence.FaultIntentAfter}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestFakeHostSpawnCrashDoesNotRepeatSideEffect(t *testing.T) {
	fixture, actionID := fixtureWithAction(t)
	fixture.Faults.Arm(persistence.FaultSpawnAfterAttach)
	if result, err := fixture.Host.Spawn(actionID, "agent-1", protocol.SpawnStatusSpawned); !errors.Is(err, ErrInjected) || result.Calls != 1 {
		t.Fatalf("crashed spawn result=%+v err=%v", result, err)
	}
	summary, err := Summarize(fixture.Engine)
	if err != nil {
		t.Fatalf("summary after crashed spawn: %v", err)
	}
	if summary.SpawnReceipts != 0 || summary.PendingActions != 1 {
		t.Fatalf("crashed spawn summary = %+v", summary)
	}
	result, err := fixture.Host.Spawn(actionID, "agent-1", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("attach retry: %v", err)
	}
	if result.Calls != 1 || fixture.Host.SpawnCalls(actionID) != 1 {
		t.Fatalf("spawn was repeated: result=%+v calls=%d", result, fixture.Host.SpawnCalls(actionID))
	}
	if result.Acceptance.Status != "ACCEPTED" {
		t.Fatalf("attach acceptance = %+v", result.Acceptance)
	}
}

func TestFakeWorkerResultBeforeReceiptReplaysAfterRestart(t *testing.T) {
	fixture, actionID := fixtureWithAction(t)
	acceptance, err := fixture.Worker.ResultBeforeReceipt(actionID, protocol.OutcomePass, "sha256:result", "")
	if err != nil {
		t.Fatalf("result before receipt: %v", err)
	}
	if acceptance.Status != "STAGED" {
		t.Fatalf("staged acceptance = %+v", acceptance)
	}
	summary, err := Summarize(fixture.Engine)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.PendingResults != 1 || summary.Results != 0 || summary.PendingActions != 1 {
		t.Fatalf("staged summary = %+v", summary)
	}
	restarted, report, err := fixture.Restart()
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if report.Outcome != persistence.RecoveryClean {
		t.Fatalf("restart recovery = %+v, want clean", report)
	}
	if _, err := restarted.Host.Spawn(actionID, "agent-1", protocol.SpawnStatusSpawned); err != nil {
		t.Fatalf("receipt after restart: %v", err)
	}
	summary, err = Summarize(restarted.Engine)
	if err != nil {
		t.Fatalf("summary after receipt: %v", err)
	}
	if summary.PendingResults != 0 || summary.Results != 1 || summary.PendingActions != 0 {
		t.Fatalf("settled summary = %+v", summary)
	}
}

func TestSubmitResponseLossIsIdempotent(t *testing.T) {
	fixture, actionID := fixtureWithAction(t)
	if _, err := fixture.Host.Spawn(actionID, "agent-1", protocol.SpawnStatusSpawned); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	fixture.Faults.Arm(persistence.FaultSubmitResponseLost)
	acceptance, err := fixture.Worker.Result(actionID, protocol.OutcomePass, "sha256:result", "")
	if !errors.Is(err, ErrInjected) || acceptance.Status != "ACCEPTED" {
		t.Fatalf("response-loss result=%+v err=%v", acceptance, err)
	}
	before, err := Summarize(fixture.Engine)
	if err != nil {
		t.Fatalf("summary before retry: %v", err)
	}
	retry, err := fixture.Worker.Result(actionID, protocol.OutcomePass, "sha256:result", "")
	if err != nil {
		t.Fatalf("result retry: %v", err)
	}
	if retry.Status != "DUPLICATE" {
		t.Fatalf("retry acceptance = %+v", retry)
	}
	after, err := Summarize(fixture.Engine)
	if err != nil {
		t.Fatalf("summary after retry: %v", err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("retry event ID was not durably consumed: before=%+v after=%+v", before, after)
	}
	before.Revision = 0
	after.Revision = 0
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("retry repeated protocol effects: before=%+v after=%+v", before, after)
	}
}

func TestHostActionUnknownReconcileDoesNotExecuteAgain(t *testing.T) {
	fixture, _ := fixtureWithAction(t)
	fixture.Faults.Arm(persistence.FaultHostCommitBefore)
	if _, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "blocked"}); !errors.Is(err, ErrInjected) {
		t.Fatalf("host commit-before error = %v", err)
	}
	fixture.Faults.Arm(persistence.FaultHostCommitAfter)
	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "fan.slice"})
	if !errors.Is(err, ErrInjected) || intent.ActionID == "" {
		t.Fatalf("host action response loss intent=%+v err=%v", intent, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("side effect count = %d, want 1", fixture.Host.ActionCalls(intent.ActionID))
	}
	acceptance, err := fixture.Host.Receipt(intent, protocol.HostActionStatusUnknown)
	if err != nil || acceptance.RecoveryAction != string(protocol.RecoveryReconcile) {
		t.Fatalf("unknown receipt acceptance=%+v err=%v", acceptance, err)
	}
	plan, err := fixture.Host.Reconcile(intent.ActionID, "sha256:observed", true, false)
	if err != nil || plan.Action != protocol.RecoveryReconcile {
		t.Fatalf("reconcile plan=%+v err=%v", plan, err)
	}
	if fixture.Host.ActionCalls(intent.ActionID) != 1 {
		t.Fatalf("reconcile re-executed side effect: %d", fixture.Host.ActionCalls(intent.ActionID))
	}
	fixture.Faults.Arm(persistence.FaultHostObserveBefore)
	if _, err := fixture.Host.Observe(intent.ActionID, "sha256:observed"); !errors.Is(err, ErrInjected) {
		t.Fatalf("host observe-before error = %v", err)
	}
	second, err := fixture.Host.Reconcile(intent.ActionID, "sha256:observed", true, false)
	if err != nil || second.Action != protocol.RecoveryReconcile {
		t.Fatalf("reconcile replay plan=%+v err=%v", second, err)
	}

	fixture2, _ := fixtureWithAction(t)
	intent2, err := fixture2.Host.Execute("op.fan.transport", map[string]any{"target": "fan.slice"})
	if err != nil {
		t.Fatalf("duplicate receipt intent: %v", err)
	}
	if _, err := fixture2.Host.ReceiptWithCorrelation(intent2, protocol.HostActionStatusExecuted, "same-correlation"); err != nil {
		t.Fatalf("first executed receipt: %v", err)
	}
	duplicate, err := fixture2.Host.ReceiptWithCorrelation(intent2, protocol.HostActionStatusExecuted, "same-correlation")
	if err != nil || duplicate.Status != "DUPLICATE" {
		t.Fatalf("duplicate receipt = %+v err=%v", duplicate, err)
	}
}

func TestConcurrentSubmitLeavesOneCanonicalState(t *testing.T) {
	fixture, _ := fixtureWithAction(t)
	requestA, err := protocol.NewRequestEvent("request-a", protocol.ControlReset, protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("request A: %v", err)
	}
	requestB, err := protocol.NewRequestEvent("request-b", protocol.ControlAbort, protocol.AskOption{ID: "yes", Label: "yes"})
	if err != nil {
		t.Fatalf("request B: %v", err)
	}
	var acceptances [2]protocol.Acceptance
	var mu sync.Mutex
	results := SubmitConcurrently(
		func() (protocol.Acceptance, error) {
			fp, err := fixture.Engine.ObserveFingerprint()
			if err != nil {
				return protocol.Acceptance{}, err
			}
			acceptance, err := fixture.Engine.Submit(requestA, fp)
			mu.Lock()
			acceptances[0] = acceptance
			mu.Unlock()
			return acceptance, err
		},
		func() (protocol.Acceptance, error) {
			fp, err := fixture.Engine.ObserveFingerprint()
			if err != nil {
				return protocol.Acceptance{}, err
			}
			acceptance, err := fixture.Engine.Submit(requestB, fp)
			mu.Lock()
			acceptances[1] = acceptance
			mu.Unlock()
			return acceptance, err
		},
	)
	for i, err := range results {
		if err == nil {
			continue
		}
		if !errors.As(err, new(*persistence.LockHeldError)) && !errors.As(err, new(*persistence.RevisionConflictError)) {
			t.Fatalf("concurrent submit %d: %v", i, err)
		}
		request := requestA
		if i == 1 {
			request = requestB
		}
		fp, fingerprintErr := fixture.Engine.ObserveFingerprint()
		if fingerprintErr != nil {
			t.Fatalf("retry fingerprint %d: %v", i, fingerprintErr)
		}
		acceptance, retryErr := fixture.Engine.Submit(request, fp)
		if retryErr != nil {
			t.Fatalf("retry concurrent submit %d: %v", i, retryErr)
		}
		mu.Lock()
		acceptances[i] = acceptance
		mu.Unlock()
	}
	if acceptances[0].Status != "ACCEPTED" || acceptances[1].Status != "ACCEPTED" {
		t.Fatalf("concurrent acceptances = %+v", acceptances)
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("load after concurrent submit: %v", err)
	}
	if snapshot.Revision != 4 || len(snapshot.State.PendingAsks) != 2 || len(snapshot.State.Events) != 2 {
		t.Fatalf("concurrent state = revision %d asks %d events %d", snapshot.Revision, len(snapshot.State.PendingAsks), len(snapshot.State.Events))
	}
}

func TestLateAttemptResultIsRecordedObsolete(t *testing.T) {
	fixture, actionID := fixtureWithAction(t)
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{
		Class: authoring.FailureTransientEngine, CauseKnown: true,
	}, fingerprint)
	if err != nil {
		t.Fatalf("recover attempt: %v", err)
	}
	if plan.Action != protocol.RecoveryNewAttempt {
		t.Fatalf("recovery plan = %+v", plan)
	}
	late, err := protocol.NewWorkerResultEvent("late-result", actionID, "fake-host", protocol.OutcomePass, "sha256:late", "")
	if err != nil {
		t.Fatalf("late result: %v", err)
	}
	fingerprint, err = fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("fingerprint for late result: %v", err)
	}
	acceptance, err := fixture.Engine.Submit(late, fingerprint)
	if err != nil {
		t.Fatalf("submit late result: %v", err)
	}
	if acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late result acceptance = %+v", acceptance)
	}
	summary, err := Summarize(fixture.Engine)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.PendingActions != 1 || summary.Attempts != 1 {
		t.Fatalf("replacement summary = %+v", summary)
	}
}

func TestExternalFingerprintRecheckBeforeAndAfterWrite(t *testing.T) {
	fixture, _ := fixtureWithAction(t)
	old, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("observe original fingerprint: %v", err)
	}
	if err := fixture.VCS.Write("facts/changed-before.txt", []byte("changed")); err != nil {
		t.Fatalf("change fake VCS: %v", err)
	}
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("load before fingerprint rejection: %v", err)
	}
	if _, _, err := fixture.Engine.ExecuteHostAction("op.fan.transport", map[string]any{"target": "fan.slice"}, old); err == nil {
		t.Fatal("changed-before fingerprint was accepted")
	} else {
		var changed *persistence.FingerprintChangedError
		if !errors.As(err, &changed) || changed.Committed {
			t.Fatalf("before fingerprint error = %v", err)
		}
	}
	after, err := fixture.Engine.Load()
	if err != nil {
		t.Fatalf("load after fingerprint rejection: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("before fingerprint rejection changed state")
	}

	calls := 0
	_, err = fixture.Store.Save(persistence.Transaction{
		ExpectedRevision:    after.Revision,
		ExpectedFingerprint: "sha256:changed-before",
		CollectFingerprint: func() (string, error) {
			calls++
			if calls == 1 {
				return "sha256:changed-before", nil
			}
			return "sha256:changed-after", nil
		},
		Content: map[string]any{"kind": "after-fingerprint"},
	})
	if err == nil {
		t.Fatal("changed-after fingerprint was accepted")
	}
	var changed *persistence.FingerprintChangedError
	if !errors.As(err, &changed) || !changed.Committed {
		t.Fatalf("after fingerprint error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("fingerprint collector calls = %d, want 2", calls)
	}
}

func TestFakeVCSTracksAndSnapshotsOnlyExplicitProjectPaths(t *testing.T) {
	project, err := NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatalf("new isolated project: %v", err)
	}
	vcs, err := NewFakeVCS(project.Root)
	if err != nil {
		t.Fatalf("new fake VCS: %v", err)
	}
	before, err := vcs.Snapshot()
	if err != nil {
		t.Fatalf("before snapshot: %v", err)
	}
	if err := vcs.Write("state/engine.json", []byte(`{"revision":1}`)); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := vcs.Track("state/engine.json"); err != nil {
		t.Fatalf("track state: %v", err)
	}
	if err := vcs.Track(filepath.Join(project.Root, "stable", "missing")); err == nil {
		t.Fatal("missing delivery path was tracked")
	}
	if err := vcs.Write(".gates/ignored", []byte("ignored")); err != nil {
		t.Fatalf("write ignored: %v", err)
	}
	after, err := vcs.Snapshot()
	if err != nil {
		t.Fatalf("after snapshot: %v", err)
	}
	changes := vcs.Diff(before, after)
	if len(changes) != 1 || changes[0].Path != "state/engine.json" {
		t.Fatalf("snapshot changes = %+v", changes)
	}
	if got := vcs.Tracked(); !reflect.DeepEqual(got, []string{"state/engine.json"}) {
		t.Fatalf("tracked paths = %v", got)
	}
	first, err := vcs.Commit("harness")
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	second, err := vcs.Commit("harness")
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if first != second {
		t.Fatalf("same snapshot commit is not deterministic: first=%+v second=%+v", first, second)
	}
	if _, err := vcs.Fingerprint(); err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
}

func TestIsolatedProjectInstalledEnvironmentIsSeparate(t *testing.T) {
	project, err := NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatalf("new project: %v", err)
	}
	before, err := project.Snapshot()
	if err != nil {
		t.Fatalf("before snapshot: %v", err)
	}
	marker := filepath.Join(project.Root, "state", "harness-marker")
	if err := os.WriteFile(marker, []byte("engine"), 0o600); err != nil {
		t.Fatalf("write isolated marker: %v", err)
	}
	after, err := project.Snapshot()
	if err != nil {
		t.Fatalf("after snapshot: %v", err)
	}
	if changes := (&FakeVCS{Root: project.Root}).Diff(before, after); len(changes) != 1 || changes[0].Path != "state/harness-marker" {
		t.Fatalf("isolated diff = %+v", changes)
	}
	for _, stablePath := range []string{project.StableState, project.StableRun} {
		entries, err := os.ReadDir(stablePath)
		if err != nil {
			t.Fatalf("read stable path %s: %v", stablePath, err)
		}
		if len(entries) != 0 {
			t.Fatalf("stable path %s was written: %v", stablePath, entries)
		}
	}
}

func TestSubmitConcurrentlyHelperDoesNotRaceItsResults(t *testing.T) {
	var mu sync.Mutex
	var order []string
	results := SubmitConcurrently(
		func() (protocol.Acceptance, error) {
			mu.Lock()
			order = append(order, "a")
			mu.Unlock()
			return protocol.Acceptance{Status: "ACCEPTED"}, nil
		},
		func() (protocol.Acceptance, error) {
			mu.Lock()
			order = append(order, "b")
			mu.Unlock()
			return protocol.Acceptance{Status: "ACCEPTED"}, nil
		},
	)
	if results[0] != nil || results[1] != nil || len(order) != 2 {
		t.Fatalf("concurrent helper results=%v order=%v", results, order)
	}
	if fmt.Sprint(order) == "[]" {
		t.Fatal("concurrent helper did not invoke callbacks")
	}
}
