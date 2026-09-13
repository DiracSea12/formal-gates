package protocol

import (
	"fmt"
	"reflect"
	"testing"
)

func handoffFixture(t *testing.T) (*Engine, RegularExecution) {
	t.Helper()
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	return e, currentExecution(t, e, "development-worker")
}
func handoffRequest(x RegularExecution, kind, provider string) RegularRequest {
	to := ExecutorTarget{Kind: kind, Provider: provider, Label: "explicit local fixture executor"}
	if kind == "native" {
		to.Context = &HostRuntimeContext{Provider: provider, Executable: "/local/fixture-runtime", Environment: map[string]string{}}
	}
	return RegularRequest{Action: "executor-handoff", Target: x.Claim.ActionID, Reason: "user explicitly selected another executor", Handoff: &ExecutorHandoff{To: to, WorkspaceView: "sha256:local-working-view"}}
}
func rejectHandoffEvent(t *testing.T, e *Engine, v RegularEvent) {
	t.Helper()
	before, _ := e.Load()
	v.ExpectedRevision = before.Revision
	ev, err := NewRegularEvent(EventID(fmt.Sprintf("rejected-handoff-%d", before.Revision)), v)
	if err == nil {
		_, err = e.Submit(ev, fingerprintOf(t, e))
	}
	if err == nil {
		t.Fatal("invalid event accepted")
	}
	after, _ := e.Load()
	if after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejection changed persisted state")
	}
}
func TestExecutorHandoffRequiresUserDecisionAndPreservesHistory(t *testing.T) {
	e, old := handoffFixture(t)
	before, _ := e.Load()
	q := handoffRequest(old, "external", "chatgpt")
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &q})
	preview, _ := e.Load()
	a := preview.State.Regular.Ask
	if a == nil || a.Kind != "executor-handoff" || a.Preview == nil || preview.State.RegularExecutions[old.Claim.ActionID].Finished {
		t.Fatal("request applied before user decision")
	}
	rejectHandoffEvent(t, e, RegularEvent{Operation: "CLAIM", Claim: &old.Claim})
	rejectHandoffEvent(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: "stale", Digest: a.Digest, Choice: "confirm"}})
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: RegularFreshness(preview.Revision, a.ID), Digest: a.Digest, Choice: "confirm"}})
	after, _ := e.Load()
	r := after.State.Regular
	oldAfter := after.State.RegularExecutions[old.Claim.ActionID]
	if !oldAfter.Finished || oldAfter.SupersededBy != a.ID || oldAfter.PreparationResult != nil || oldAfter.Claimed {
		t.Fatal("retired unclaimed execution fabricated work/failure")
	}
	if !reflect.DeepEqual(before.State.Cost, after.State.Cost) || !reflect.DeepEqual(before.State.Regular.Reviews, r.Reviews) || !reflect.DeepEqual(before.State.Regular.Preparation.Commits, r.Preparation.Commits) {
		t.Fatal("handoff changed unrelated evidence")
	}
	fresh := currentExecution(t, e, "development-worker")
	if fresh.Claim.Task != old.Claim.Task || fresh.Claim.Provider != "chatgpt" || fresh.Claim.Operation != "external" || fresh.Claim.Context != nil || fresh.Identity != "" || fresh.Number != old.Number+1 {
		t.Fatalf("new binding: %+v", fresh)
	}
	if len(r.ExecutorHandoffs) != 1 || r.ExecutorHandoffs[0].FromAction != old.Claim.ActionID {
		t.Fatal("missing lineage")
	}
	if from := r.ExecutorHandoffs[0].FromExecutor; from == nil || from.Kind != "native" || from.Provider != old.Claim.Provider {
		t.Fatal("handoff receipt omitted original executor")
	}
	if preparationMax(after.State, old.Claim.Task) != preparationMax(before.State, old.Claim.Task)+1 {
		t.Fatal("handoff did not authorize exactly one execution")
	}
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &fresh.Claim})
	claimed, _ := e.Load()
	x := claimed.State.RegularExecutions[fresh.Claim.ActionID]
	if x.Identity == "" || x.Source == nil || x.Source.Status != "UNAVAILABLE" || x.Source.Provider != "chatgpt" {
		t.Fatal("external identity/source was not explicit")
	}
	if _, ok := claimed.State.SpawnReceipts[x.Claim.ActionID]; ok {
		t.Fatal("fabricated a native spawn")
	}
	rejectHandoffEvent(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: x.Identity, Status: "RUNNING"}})
	result := preparationResult(x)
	result.Identity = x.Identity
	result.Manifest = []string{"B1.go"}
	result.Handoff = "fixture author is external"
	result.Validation = []BatchValidation{{SubtaskID: "B1-S1", Procedure: "local fixture", Observation: "actual protocol contract exercise", Result: "PASS"}}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	ended, _ := e.Load()
	if ended.State.Regular.Preparation.Batches["B1"].Provider != "chatgpt" || ended.State.Regular.Preparation.Batches["B1"].Identity != x.Identity {
		t.Fatal("result attribution changed")
	}
	if ended.State.Regular.Guard.Status == "COST_PENDING" {
		t.Fatal("external unavailable usage blocked settlement")
	}
	gitReceipt(t, e, "B1-commit")
	next := currentExecution(t, e, "development-worker")
	if next.Claim.Task == x.Claim.Task || next.Executor != nil || next.Claim.Provider != "codex" {
		t.Fatal("handoff leaked into another batch")
	}
}
func TestExecutorHandoffCrossProviderStartsNewNativeIdentity(t *testing.T) {
	e, old := handoffFixture(t)
	claimAndAttach(t, e, old)
	rejectHandoffEvent(t, e, RegularEvent{Operation: "REQUEST", Request: ptrHandoff(handoffRequest(old, "native", "zcode"))})
	v := preparationResult(old)
	v.Status = "CONTINUE"
	v.Message = "stopped local fixture with reusable work"
	v.Manifest = []string{}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
	sourceUnavailable(t, e, old)
	pending := currentExecution(t, e, "development-worker")
	before, _ := e.Load()
	oldEvidence := before.State.RegularExecutions[old.Claim.ActionID]
	regularChoice(t, e, handoffRequest(pending, "native", "zcode"), "confirm")
	next := currentExecution(t, e, "development-worker")
	if next.Claim.Provider != "zcode" || next.Claim.Operation != "spawn" || next.Identity != "" || next.Claim.Identity != "" || next.Claim.Context.Provider != "zcode" {
		t.Fatalf("cross-provider resume leaked: %+v", next)
	}
	after, _ := e.Load()
	if !reflect.DeepEqual(oldEvidence, after.State.RegularExecutions[old.Claim.ActionID]) {
		t.Fatal("old native result/source altered")
	}
	bad := next.Claim
	bad.Provider = "codex"
	rejectHandoffEvent(t, e, RegularEvent{Operation: "CLAIM", Claim: &bad})
	rejectHandoffEvent(t, e, RegularEvent{Operation: "CLAIM", Claim: &pending.Claim})
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &next.Claim})
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: next.Claim.ActionID, AttemptID: next.Claim.AttemptID, Provider: "zcode", Identity: "new-zcode-session", Status: "RUNNING"}})
}
func ptrHandoff(q RegularRequest) *RegularRequest { return &q }
func TestExecutorHandoffCancelAndRoleBoundary(t *testing.T) {
	e, x := handoffFixture(t)
	before, _ := e.Load()
	regularChoice(t, e, handoffRequest(x, "external", "chatgpt"), "cancel")
	after, _ := e.Load()
	if after.State.RegularExecutions[x.Claim.ActionID].Finished || len(after.State.Regular.ExecutorHandoffs) != 0 || !reflect.DeepEqual(before.State.Regular.Preparation, after.State.Regular.Preparation) {
		t.Fatal("cancel transferred work")
	}
	black := currentExecution(t, e, "blackbox-design")
	rejectHandoffEvent(t, e, RegularEvent{Operation: "REQUEST", Request: ptrHandoff(handoffRequest(black, "external", "chatgpt"))})
	bad := handoffRequest(x, "native", "zcode")
	bad.Handoff.To.Context = nil
	rejectHandoffEvent(t, e, RegularEvent{Operation: "REQUEST", Request: &bad})
	bad = handoffRequest(x, "external", "chatgpt")
	bad.Handoff.To.Context = &HostRuntimeContext{Provider: "codex", Executable: "/cannot-use"}
	rejectHandoffEvent(t, e, RegularEvent{Operation: "REQUEST", Request: &bad})
}

func TestExternalExecutorCannotLaunchNativeOrSurviveAbort(t *testing.T) {
	e, old := handoffFixture(t)
	regularChoice(t, e, handoffRequest(old, "external", "codex"), "confirm")
	x := currentExecution(t, e, "development-worker")
	bad := x.Claim
	bad.Operation = "spawn"
	rejectHandoffEvent(t, e, RegularEvent{Operation: "CLAIM", Claim: &bad})
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	regularChoice(t, e, RegularRequest{Action: "abort", Reason: "fixture user stops external work"}, "confirm")
	s, _ := e.Load()
	if len(s.State.PendingHostActions) != 0 || !s.State.RegularExecutions[x.Claim.ActionID].Finished || s.State.Regular.Terminal != "ABORTED" {
		t.Fatal("external abort fabricated or waited for a native process")
	}
}
