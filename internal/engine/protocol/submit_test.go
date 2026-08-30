package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
)

// resetRequest 是两阶段测试的标准受限请求事件构造器。
func resetRequest(t *testing.T, id EventID) Event {
	t.Helper()
	ev, err := NewRequestEvent(id, ControlReset,
		AskOption{ID: "proceed", Label: "确认重置"}, AskOption{ID: "cancel", Label: "取消"})
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
	fp := fingerprintOf(t, engine)
	acceptance, err := engine.Submit(resetRequest(t, "req-reset-1"), fp)
	if err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if acceptance.EventID != "req-reset-1" || acceptance.Kind != string(KindRequestControl) ||
		acceptance.Status != "ACCEPTED" || acceptance.RequestID != "req-reset-1" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	if acceptance.Revision != 2 {
		t.Fatalf("acceptance revision = %d, want 2", acceptance.Revision)
	}
	token, err := engine.Freshness("req-reset-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if acceptance.FreshnessToken != token {
		t.Fatalf("acceptance token %q != current freshness %q", acceptance.FreshnessToken, token)
	}
	snap := engine.LoadMustSucceed(t)
	ask, ok := snap.State.PendingAsks["req-reset-1"]
	if !ok || ask.Resolved || ask.Control != ControlReset || len(ask.Options) != 2 {
		t.Fatalf("pending ask = %+v ok=%v", ask, ok)
	}
	if ask.Options[0].ID != "proceed" || ask.Options[1].ID != "cancel" {
		t.Fatalf("options not recorded verbatim: %+v", ask.Options)
	}
}

// TestRequestIdempotentReplay：同 event ID 同 payload digest 重放返回
// 原样 acceptance（含 token），不重复创建、revision 不再 +1。
func TestRequestIdempotentReplay(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	first, err := engine.Submit(resetRequest(t, "req-reset-1"), fp)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := engine.Submit(resetRequest(t, "req-reset-1"), fp)
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
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(resetRequest(t, "req-reset-1"), fp); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	// 同 ID、不同选项集 → 不同 digest。
	conflicting, err := NewRequestEvent("req-reset-1", ControlReset,
		AskOption{ID: "proceed", Label: "另一种选项"})
	if err != nil {
		t.Fatalf("conflicting event: %v", err)
	}
	before := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(conflicting, fp); err == nil {
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
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(resetRequest(t, "req-reset-1"), fp); err != nil {
		t.Fatalf("request: %v", err)
	}
	token, err := engine.Freshness("req-reset-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	decide, err := NewDecideEvent("evt-decide-1", "req-reset-1", token, "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	acceptance, err := engine.Submit(decide, fp)
	if err != nil {
		t.Fatalf("submit decide: %v", err)
	}
	if acceptance.RequestID != "req-reset-1" || acceptance.Revision != 3 {
		t.Fatalf("decide acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if ask, pending := snap.State.PendingAsks["req-reset-1"]; !pending || !ask.Resolved {
		t.Fatalf("resolved ask = %+v pending=%v", ask, pending)
	}
	decision := snap.State.Decisions["req-reset-1"]
	if decision.Choice != "proceed" || decision.Control != ControlReset ||
		decision.EventID != "evt-decide-1" || decision.Revision != 3 {
		t.Fatalf("recorded decision = %+v", decision)
	}

	// 决定事件重放：稳定 acceptance、revision 不变、不二次落账。
	replay, err := engine.Submit(decide, fp)
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

// TestFreshnessStaleRejectedZeroChange：token 被「新签发」（任何后续
// 提交）取代后旧 token 拒绝且零状态变化；重新获取的当前 token 放行。
func TestFreshnessStaleRejectedZeroChange(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(resetRequest(t, "req-reset-1"), fp); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	staleToken, err := engine.Freshness("req-reset-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	// 另一个受限请求提交成功：revision 前进，旧 token 被取代。
	if _, err := engine.Submit(resetRequest(t, "req-reset-2"), fp); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	decide, err := NewDecideEvent("evt-decide-stale", "req-reset-1", staleToken, "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(decide, fp); err == nil {
		t.Fatal("stale token accepted")
	} else if code := rejectionCode(t, err); code != CodeStaleFreshness {
		t.Fatalf("stale code = %q", code)
	}
	after := engine.LoadMustSucceed(t)
	if before.Revision != after.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("stale freshness rejection changed state")
	}

	// 当前 token 放行。
	currentToken, err := engine.Freshness("req-reset-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if currentToken == staleToken {
		t.Fatal("token not rotated after later commit")
	}
	decideFresh, err := NewDecideEvent("evt-decide-fresh", "req-reset-1", currentToken, "cancel")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(decideFresh, fp); err != nil {
		t.Fatalf("current token rejected: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if ask, pending := snap.State.PendingAsks["req-reset-1"]; !pending || !ask.Resolved || snap.State.Decisions["req-reset-1"].Choice != "cancel" {
		t.Fatalf("decision not recorded: %+v", snap.State.Decisions)
	}
}

// TestDecideNegatives：未知 request、已决 request、选项不在落账选项集。
func TestDecideNegatives(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(resetRequest(t, "req-reset-1"), fp); err != nil {
		t.Fatalf("request: %v", err)
	}
	token, err := engine.Freshness("req-reset-1")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}

	unknown, err := NewDecideEvent("evt-d-unknown", "req-ghost", token, "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(unknown, fp); err == nil {
		t.Fatal("unknown request accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownRequest {
		t.Fatalf("unknown request code = %q", code)
	}

	badChoice, err := NewDecideEvent("evt-d-choice", "req-reset-1", token, "nuke-everything")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(badChoice, fp); err == nil {
		t.Fatal("invalid choice accepted")
	} else if code := rejectionCode(t, err); code != CodeInvalidChoice {
		t.Fatalf("invalid choice code = %q", code)
	}

	ok, err := NewDecideEvent("evt-d-ok", "req-reset-1", token, "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(ok, fp); err != nil {
		t.Fatalf("valid decide: %v", err)
	}
	newToken, err := engine.Freshness("req-reset-1")
	if err == nil || newToken != "" {
		t.Fatalf("freshness on resolved request should fail, got %q %v", newToken, err)
	}
	again, err := NewDecideEvent("evt-d-again", "req-reset-1", "any-token", "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(again, fp); err == nil {
		t.Fatal("re-decide accepted")
	} else if code := rejectionCode(t, err); code != CodeRequestResolved {
		t.Fatalf("re-decide code = %q", code)
	}
}

// TestReplayAcrossEngineRestart：台账持久——新引擎实例对同一事件重放
// 仍返回原样 acceptance 且零状态变化（崩溃/重启后的幂等）。
func TestReplayAcrossEngineRestart(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	request := resetRequest(t, "req-reset-1")
	acceptance, err := engine.Submit(request, fp)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	restarted := reopenEngine(t, dir)
	replay, err := restarted.Submit(request, fp)
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
	engine, err := New(mustStore(t, dir), Config{
		Definition: compiledWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	}, nil)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	return engine
}
