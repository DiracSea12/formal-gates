package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
	definition "formal-gates/internal/engine/testkit/workflow"
)

// cancelAbortRequest 是两阶段测试的标准受限请求事件构造器。
func cancelAbortRequest(t *testing.T, id EventID) Event {
	t.Helper()
	ev, err := NewRequestEvent(id, ControlAbort,
		AskOption{ID: "cancel", Label: "取消终止"}, AskOption{ID: "abort", Label: "终止"})
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	return ev
}

// TestRequestCreatesPendingAsk：两阶段第一段——受限 REQUEST_* 创建
// pending Ask（request ID、控制类型、选项集落账），回执携带当前
// freshness token，与 Freshness() 求值一致。
func TestRequestCreatesPendingAsk(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	acceptance, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput)
	if err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if acceptance.EventID != "req-abort-1" || acceptance.Kind != string(KindRequestControl) ||
		acceptance.Status != "ACCEPTED" || acceptance.RequestID != "req-abort-1" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	if acceptance.Revision != 2 {
		t.Fatalf("acceptance revision = %d, want 2", acceptance.Revision)
	}
	token, err := engine.Freshness("req-abort-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if acceptance.FreshnessToken != token {
		t.Fatalf("acceptance token %q != current freshness %q", acceptance.FreshnessToken, token)
	}
	snap := engine.LoadMustSucceed(t)
	ask, ok := snap.State.PendingAsks["req-abort-1"]
	if !ok || ask.Resolved || ask.Control != ControlAbort || len(ask.Options) != 2 {
		t.Fatalf("pending ask = %+v ok=%v", ask, ok)
	}
	if ask.Options[0].ID != "cancel" || ask.Options[1].ID != "abort" {
		t.Fatalf("options not recorded verbatim: %+v", ask.Options)
	}
}

// TestRequestIdempotentReplay：同 event ID 同 payload digest 重放返回
// 原样 acceptance（含 token），不重复创建、revision 不再 +1。
func TestRequestIdempotentReplay(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	first, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput)
	if err != nil {
		t.Fatalf("replay submit: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay acceptance drifted:\nfirst  %+v\nsecond %+v", first, second)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.Revision != 2 {
		t.Fatalf("revision after replay = %d, want 2（幂等重放不得再 +1）", snap.Revision)
	}
	if len(snap.State.PendingAsks) != 1 || len(snap.State.Events) != 1 {
		t.Fatalf("replay duplicated bookkeeping: asks=%d events=%d", len(snap.State.PendingAsks), len(snap.State.Events))
	}
}

// TestSameIDDifferentDigestHardReject：同 event ID 不同 payload digest
// 硬拒绝，零状态变化（台账、Ask、revision 均不动）。
func TestSameIDDifferentDigestHardReject(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	// 同 ID、不同选项集 → 不同 digest。
	conflicting, err := NewRequestEvent("req-abort-1", ControlAbort,
		AskOption{ID: "cancel", Label: "另一种选项"})
	if err != nil {
		t.Fatalf("conflicting event: %v", err)
	}
	before := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(conflicting, observedInput); err == nil {
		t.Fatal("same-id different-digest accepted")
	} else if code := rejectionCode(t, err); code != CodeDuplicateEventMismatch {
		t.Fatalf("mismatch code = %q", code)
	}
	after := engine.LoadMustSucceed(t)
	if before.Revision != after.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("hard rejection changed state")
	}
}

// TestDecideTwoPhaseHappyPath：第二段——以 request ID + 当前 token 提交
// 决定；Ask 保留并置 resolved、决定落账（控制类型、选项、事件与 revision）；
// 重放返回稳定 acceptance 且 revision 不再 +1。
func TestDecideTwoPhaseHappyPath(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput); err != nil {
		t.Fatalf("request: %v", err)
	}
	token, err := engine.Freshness("req-abort-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	decide, err := NewDecideEvent("evt-decide-1", "req-abort-1", token, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	acceptance, err := engine.Submit(decide, observedInput)
	if err != nil {
		t.Fatalf("submit decide: %v", err)
	}
	if acceptance.RequestID != "req-abort-1" || acceptance.Revision != 3 {
		t.Fatalf("decide acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if ask, pending := snap.State.PendingAsks["req-abort-1"]; !pending || !ask.Resolved {
		t.Fatalf("resolved ask = %+v pending=%v", ask, pending)
	}
	decision := snap.State.Decisions["req-abort-1"]
	if decision.Choice != "cancel" || decision.Control != ControlAbort ||
		decision.EventID != "evt-decide-1" || decision.Revision != 3 {
		t.Fatalf("recorded decision = %+v", decision)
	}
	wantRequestDigest, err := cancelAbortRequest(t, "req-abort-1").Digest()
	if err != nil || snap.State.requestDigest("req-abort-1") != wantRequestDigest {
		t.Fatalf("external request lost its canonical event digest: %s, %v", snap.State.requestDigest("req-abort-1"), err)
	}

	// 决定事件重放：稳定 acceptance、revision 不变、不二次落账。
	replay, err := engine.Submit(decide, observedInput)
	if err != nil {
		t.Fatalf("decide replay: %v", err)
	}
	if !reflect.DeepEqual(acceptance, replay) {
		t.Fatalf("decide replay drifted:\nfirst  %+v\nreplay %+v", acceptance, replay)
	}
	snap2 := engine.LoadMustSucceed(t)
	if snap2.Revision != 3 || len(snap2.State.Events) != 2 || len(snap2.State.Decisions) != 1 {
		t.Fatalf("replay changed durable state: rev=%d events=%d decisions=%d",
			snap2.Revision, len(snap2.State.Events), len(snap2.State.Decisions))
	}
}

func TestRecoveryRequestEvidenceSurvivesDecisionAndReopen(t *testing.T) {
	engine, dir, _, _ := preparedEngine(t)
	action := issueReady(t, engine)[0]
	plan, _, err := engine.RecoverAttempt(action, Interruption{}, Observation{})
	if err != nil || plan.Action != RecoveryAsk {
		t.Fatalf("recovery request = %+v, %v", plan, err)
	}
	state := engine.LoadMustSucceed(t).State
	ask := state.PendingAsks[plan.RequestID]
	if ask.Control != ControlRecovery || ask.RequestID != plan.RequestID || ask.Resolved {
		t.Fatalf("recovery offer = %+v", ask)
	}
	want := digestOfCanonical(ask)
	if _, external := state.Events[plan.RequestID]; external || state.requestDigest(plan.RequestID) != want {
		t.Fatal("generated recovery request lost its offer identity")
	}
	token, err := engine.Freshness(RequestID(plan.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewDecideEvent("resume-original", RequestID(plan.RequestID), token, "resume")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := engine.Submit(event, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	engine, err = New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := engine.Submit(event, Observation{})
	if err != nil || !reflect.DeepEqual(replay, accepted) || !reflect.DeepEqual(before, stateBytesOf(t, dir)) {
		t.Fatal("replayed recovery decision changed its durable evidence")
	}
	state = engine.LoadMustSucceed(t).State
	if !state.PendingAsks[plan.RequestID].Resolved || state.requestDigest(plan.RequestID) != want || state.Decisions[plan.RequestID].EventID != string(event.ID) {
		t.Fatal("settlement or reopen changed the original recovery request evidence")
	}
}

// TestFreshnessStaleRejectedZeroChange：token 被「新签发」（任何后续
// 提交）取代后旧 token 拒绝且零状态变化；重新获取的当前 token 放行。
func TestFreshnessStaleRejectedZeroChange(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	staleToken, err := engine.Freshness("req-abort-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	// 另一个受限请求提交成功：revision 前进，旧 token 被取代。
	if _, err := engine.Submit(cancelAbortRequest(t, "req-abort-2"), observedInput); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	decide, err := NewDecideEvent("evt-decide-stale", "req-abort-1", staleToken, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(decide, observedInput); err == nil {
		t.Fatal("stale token accepted")
	} else if code := rejectionCode(t, err); code != CodeStaleFreshness {
		t.Fatalf("stale code = %q", code)
	}
	after := engine.LoadMustSucceed(t)
	if before.Revision != after.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("stale freshness rejection changed state")
	}

	// 当前 token 放行。
	currentToken, err := engine.Freshness("req-abort-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if currentToken == staleToken {
		t.Fatal("token not rotated after later commit")
	}
	decideFresh, err := NewDecideEvent("evt-decide-fresh", "req-abort-1", currentToken, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(decideFresh, observedInput); err != nil {
		t.Fatalf("current token rejected: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if ask, pending := snap.State.PendingAsks["req-abort-1"]; !pending || !ask.Resolved || snap.State.Decisions["req-abort-1"].Choice != "cancel" {
		t.Fatalf("decision not recorded: %+v", snap.State.Decisions)
	}
}

// TestDecideNegatives：未知 request、已决 request、选项不在落账选项集。
func TestDecideNegatives(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, err := engine.Submit(cancelAbortRequest(t, "req-abort-1"), observedInput); err != nil {
		t.Fatalf("request: %v", err)
	}
	token, err := engine.Freshness("req-abort-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}

	unknown, err := NewDecideEvent("evt-d-unknown", "req-ghost", token, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(unknown, observedInput); err == nil {
		t.Fatal("unknown request accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownRequest {
		t.Fatalf("unknown request code = %q", code)
	}

	badChoice, err := NewDecideEvent("evt-d-choice", "req-abort-1", token, "nuke-everything")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(badChoice, observedInput); err == nil {
		t.Fatal("invalid choice accepted")
	} else if code := rejectionCode(t, err); code != CodeInvalidChoice {
		t.Fatalf("invalid choice code = %q", code)
	}

	ok, err := NewDecideEvent("evt-d-ok", "req-abort-1", token, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(ok, observedInput); err != nil {
		t.Fatalf("valid decide: %v", err)
	}
	newToken, err := engine.Freshness("req-abort-1")
	if err == nil || newToken != "" {
		t.Fatalf("freshness on resolved request should fail, got %q %v", newToken, err)
	}
	again, err := NewDecideEvent("evt-d-again", "req-abort-1", "any-token", "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(again, observedInput); err == nil {
		t.Fatal("re-decide accepted")
	} else if code := rejectionCode(t, err); code != CodeRequestResolved {
		t.Fatalf("re-decide code = %q", code)
	}
}

// TestReplayAcrossEngineRestart：台账持久——新引擎实例对同一事件重放
// 仍返回原样 acceptance 且零状态变化（崩溃/重启后的幂等）。
func TestReplayAcrossEngineRestart(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
		t.Fatalf("issue: %v", err)
	}
	request := cancelAbortRequest(t, "req-abort-1")
	acceptance, err := engine.Submit(request, observedInput)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	restarted := reopenEngine(t, dir)
	replay, err := restarted.Submit(request, observedInput)
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if !reflect.DeepEqual(acceptance, replay) {
		t.Fatalf("replay after restart drifted:\nfirst  %+v\nreplay %+v", acceptance, replay)
	}
	snap := restarted.LoadMustSucceed(t)
	if snap.Revision != 3 || len(snap.State.PendingAsks) != 1 {
		t.Fatalf("replay after restart changed state: rev=%d asks=%d", snap.Revision, len(snap.State.PendingAsks))
	}
}

// reopenEngine 用同一状态目录重开引擎（重启语义）。
func reopenEngine(t *testing.T, dir string) *Engine {
	t.Helper()
	engine, err := New(mustStore(t, dir), Config{InternalSteps: fixtureInternalSteps(),
		Definition: compiledWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	return engine
}

// Snapshot token queries remain read-only and retain the supplied revision
// even when a later request commits through the same engine.
func TestSnapshotFreshnessRetainsItsRevision(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	accepted, err := engine.Submit(cancelAbortRequest(t, "original-request"), observedInput)
	if err != nil {
		t.Fatal(err)
	}
	old := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(cancelAbortRequest(t, "parallel-request"), observedInput); err != nil {
		t.Fatal(err)
	}
	before := engine.LoadMustSucceed(t)
	token, err := old.Freshness("original-request")
	if err != nil || token != accepted.FreshnessToken {
		t.Fatalf("snapshot token = %q, %v", token, err)
	}
	current, err := engine.Freshness("original-request")
	if err != nil || current == token {
		t.Fatal("current token did not advance")
	}
	for _, request := range []RequestID{"original-request", "unknown"} {
		fromSnapshot, snapshotErr := before.Freshness(request)
		fromEngine, engineErr := engine.Freshness(request)
		if fromSnapshot != fromEngine || !reflect.DeepEqual(snapshotErr, engineErr) {
			t.Fatal("engine and snapshot freshness semantics differ")
		}
	}
	if _, err := (Snapshot{}).Freshness("original-request"); rejectionCode(t, err) != CodeNotInitialized {
		t.Fatal("empty snapshot lost not-initialized error")
	}
	decision, err := NewDecideEvent("accepted-decision", "original-request", current, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(decision, observedInput); err != nil {
		t.Fatal(err)
	}
	resolved := engine.LoadMustSucceed(t)
	if _, err := resolved.Freshness("original-request"); rejectionCode(t, err) != CodeRequestResolved {
		t.Fatal("resolved snapshot retained a token")
	}
	if token, err := old.Freshness("original-request"); err != nil || token != accepted.FreshnessToken {
		t.Fatal("decision changed the earlier snapshot")
	}
	after := engine.LoadMustSucceed(t)
	if resolved.Revision != after.Revision || !reflect.DeepEqual(resolved.State, after.State) {
		t.Fatal("snapshot query changed durable state")
	}
}
