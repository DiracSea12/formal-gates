package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
)

// lifecycleEvent 便捷构造 lifecycle 事件。
func lifecycleEvent(t *testing.T, id EventID, identity, eventName string) Event {
	t.Helper()
	ev, err := NewLifecycleEventEvent(id, testProvider, identity, eventName)
	if err != nil {
		t.Fatalf("lifecycle event: %v", err)
	}
	return ev
}

// issuedRunWithReceipt 完成「初始化 + 签发 + SPAWNED 回执（correlation
// 认领 identity）」的标准前置，返回引擎与 fp。
func issuedRunWithReceipt(t *testing.T, correlation string) (*Engine, string) {
	t.Helper()
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-receipt-lc", reviewAction, correlation), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	return engine, fp
}

// TestLifecyclePairedStartStopVerified：成对 start/stop 且 identity 被
// SpawnReceipt correlation 认领 → 落账已验证配对；workflow 投影不动。
func TestLifecyclePairedStartStopVerified(t *testing.T) {
	engine, fp := issuedRunWithReceipt(t, "agent-review-1")
	before := engine.LoadMustSucceed(t)

	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), fp); err != nil {
		t.Fatalf("start: %v", err)
	}
	mid := engine.LoadMustSucceed(t)
	if _, verified := mid.State.LifecycleVerified["agent-review-1"]; verified {
		t.Fatal("unpaired start verified")
	}
	acceptance, err := engine.Submit(lifecycleEvent(t, "evt-lc-2", "agent-review-1", LifecycleStop), fp)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if acceptance.Status != "ACCEPTED" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	verification, ok := snap.State.LifecycleVerified["agent-review-1"]
	if !ok || verification.Provider != testProvider || verification.Revision == 0 {
		t.Fatalf("verification = %+v ok=%v", verification, ok)
	}
	// lifecycle 只写 observation buffer：任务/步骤/expected 投影与回执前
	// 完全一致。
	if snap.State.TaskStatusOf(reviewTaskKey()) != before.State.TaskStatusOf(reviewTaskKey()) ||
		!reflect.DeepEqual(snap.State.Expected, before.State.Expected) ||
		!reflect.DeepEqual(snap.State.Completed, before.State.Completed) {
		t.Fatal("lifecycle mutated workflow projection")
	}
	if len(snap.State.LifecycleEvents) != 2 {
		t.Fatalf("buffer = %d records", len(snap.State.LifecycleEvents))
	}
}

// TestLifecyclePairBeforeReceiptVerifiedOnReceiptArrival：start/stop 先于
// SpawnReceipt 到达时，receipt 的 correlation 认领既有配对也必须回填
// LifecycleVerified，不能依赖固定到达顺序。
func TestLifecyclePairBeforeReceiptVerifiedOnReceiptArrival(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-before-start", "agent-review-1", LifecycleStart), fp); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-before-stop", "agent-review-1", LifecycleStop), fp); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if snap := engine.LoadMustSucceed(t); len(snap.State.LifecycleVerified) != 0 {
		t.Fatalf("unclaimed pair verified before receipt: %+v", snap.State.LifecycleVerified)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-lc-after-receipt", reviewAction, "agent-review-1"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	verification, ok := snap.State.LifecycleVerified["agent-review-1"]
	if !ok || verification.Provider != testProvider || verification.Revision == 0 {
		t.Fatalf("pair was not verified on receipt arrival: %+v ok=%v", verification, ok)
	}
}

// TestLifecycleDuplicateReplayIdempotent：逐字节重发不重复 lifecycle
// buffer；换事件 ID 重发时只占用新的事件台账键。
func TestLifecycleDuplicateReplayIdempotent(t *testing.T) {
	engine, fp := issuedRunWithReceipt(t, "agent-review-1")
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), fp); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	same, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), fp)
	if err != nil || same.Status != "ACCEPTED" {
		t.Fatalf("same-id replay = %+v err = %v", same, err)
	}
	cross, err := engine.Submit(lifecycleEvent(t, "evt-lc-1bis", "agent-review-1", LifecycleStart), fp)
	if err != nil {
		t.Fatalf("cross-id replay: %v", err)
	}
	if cross.Status != "DUPLICATE" || cross.Revision != before.Revision+1 {
		t.Fatalf("cross-id replay = %+v", cross)
	}
	after := engine.LoadMustSucceed(t)
	if after.Revision != before.Revision+1 || len(after.State.LifecycleEvents) != len(before.State.LifecycleEvents) || len(after.State.Events) != len(before.State.Events)+1 {
		t.Fatal("duplicate lifecycle replay did not produce exactly one event-ledger commit")
	}
}

// TestLifecycleUnclaimedOrUnpairedNotVerified：未认领 identity（无回执
// correlation 认领）即使配对也不验证；只缓冲。
func TestLifecycleUnclaimedOrUnpairedNotVerified(t *testing.T) {
	engine, fp := issuedRunWithReceipt(t, "agent-review-1")
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-a", "agent-stranger", LifecycleStart), fp); err != nil {
		t.Fatalf("stranger start: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-b", "agent-stranger", LifecycleStop), fp); err != nil {
		t.Fatalf("stranger stop: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if _, verified := snap.State.LifecycleVerified["agent-stranger"]; verified {
		t.Fatal("unclaimed identity verified")
	}
	if len(snap.State.LifecycleEvents) != 2 {
		t.Fatalf("buffer = %d", len(snap.State.LifecycleEvents))
	}

	// 未配对（只有 stop）的已认领 identity 在 admission 层拒绝，不能进入
	// observation buffer。
	beforeLoneStop := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-c", "agent-review-1", LifecycleStop), fp); err == nil {
		t.Fatal("lone stop accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("lone stop rejection code = %q", code)
	}
	snap2 := engine.LoadMustSucceed(t)
	if snap2.Revision != beforeLoneStop.Revision || !reflect.DeepEqual(snap2.State, beforeLoneStop.State) {
		t.Fatal("rejected lone stop changed state")
	}
	if _, verified := snap2.State.LifecycleVerified["agent-review-1"]; verified {
		t.Fatal("unpaired identity verified")
	}
}

// TestLifecycleNegatives：provider mismatch、非法事件名。
func TestLifecycleNegatives(t *testing.T) {
	engine, fp := issuedRunWithReceipt(t, "agent-review-1")
	before := engine.LoadMustSucceed(t)
	mismatch, err := NewLifecycleEventEvent("evt-lc-m", "another-host", "agent-review-1", LifecycleStart)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatch, fp); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("provider code = %q", code)
	}
	if _, err := NewLifecycleEventEvent("evt-lc-bad", testProvider, "agent-x", "subagent_crashed"); err == nil {
		t.Fatal("bogus lifecycle event name accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("bogus event code = %q", code)
	}
	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected lifecycle events changed state")
	}
}

// TestUnknownReceiptUsesDurableLifecycleCardinality proves reconciliation does
// not accept caller-supplied identities and can represent the multiple-match
// branch when two paired identities share one transport correlation.
func TestUnknownReceiptUsesDurableLifecycleCardinality(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, identity := range []string{"agent-a", "agent-b"} {
		for _, eventName := range []string{LifecycleStart, LifecycleStop} {
			ev, err := NewCorrelatedLifecycleEvent(
				EventID("evt-"+identity+"-"+eventName), testProvider, "spawn-correlation", identity, eventName,
			)
			if err != nil {
				t.Fatalf("lifecycle event: %v", err)
			}
			if _, err := engine.Submit(ev, fp); err != nil {
				t.Fatalf("submit lifecycle event: %v", err)
			}
		}
	}
	unknown := unknownSpawnReceipt(t, "evt-multi-unknown", reviewAction, "spawn-correlation")
	acceptance, err := engine.Submit(unknown, fp)
	if err != nil {
		t.Fatalf("UNKNOWN receipt: %v", err)
	}
	if acceptance.RecoveryAction != string(RecoveryOperator) {
		t.Fatalf("initial multiple-match route = %+v", acceptance)
	}
	planResult, _, err := engine.ReconcileUnknownReceipt(reviewAction, fp)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if planResult.Action != RecoveryOperator || planResult.LifecycleMatches != 2 {
		t.Fatalf("multiple-match plan = %+v", planResult)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.SpawnReceipts[reviewAction].Status != SpawnStatusUnknown {
		t.Fatalf("multiple evidence attached receipt: %+v", snap.State.SpawnReceipts[reviewAction])
	}
	if len(snap.State.LifecycleVerified) != 2 {
		t.Fatalf("verified lifecycle identities = %+v", snap.State.LifecycleVerified)
	}
}
