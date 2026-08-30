package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
)

// opTransport 是 checked-in 定义注册的唯一 host adapter operation。
const opTransport = "op.fan.transport"

// hostReceipt 构造与 intent 精确匹配的 EXECUTED 回执事件。
func hostReceipt(t *testing.T, id EventID, intent HostActionIntent) Event {
	t.Helper()
	if intent.Adapter == nil {
		t.Fatal("hostReceipt requires adapter intent")
	}
	ev, err := NewHostActionReceiptEvent(id, intent.ActionID, intent.Adapter.Operation,
		testProvider, "corr-1", intent.PayloadDigest, HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("host action receipt event: %v", err)
	}
	return ev
}

func unknownHostReceipt(t *testing.T, id EventID, intent HostActionIntent) Event {
	t.Helper()
	if intent.Adapter == nil {
		t.Fatal("unknownHostReceipt requires adapter intent")
	}
	ev, err := NewHostActionReceiptEvent(id, intent.ActionID, intent.Adapter.Operation,
		testProvider, "corr-unknown", intent.PayloadDigest, HostActionStatusUnknown)
	if err != nil {
		t.Fatalf("unknown host action receipt event: %v", err)
	}
	return ev
}

// TestHostActionIntentPersistedBeforeExecute：intent 先持久化（pending
// 落账、含参数 digest），回执接纳后 intent 清账、回执留档。
func TestHostActionIntentPersistedBeforeExecute(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	params := map[string]any{"target": "fan.slice", "retries": float64(2)}
	intent, revision, err := engine.ExecuteHostAction(opTransport, params, fp)
	if err != nil {
		t.Fatalf("execute host action: %v", err)
	}
	if revision != 2 || intent.ActionID != "hact:"+string(HostActionExecuteAdapterOperation)+":2" {
		t.Fatalf("intent = %+v revision = %d", intent, revision)
	}
	if intent.Operation != HostActionExecuteAdapterOperation || intent.Adapter == nil || intent.PayloadDigest == "" || !reflect.DeepEqual(intent.Adapter.Params, params) {
		t.Fatalf("intent params/digest = %+v", intent)
	}
	snap := engine.LoadMustSucceed(t)
	if _, ok := snap.State.PendingHostActions[intent.ActionID]; !ok {
		t.Fatal("intent not persisted before host execution")
	}

	acceptance, err := engine.Submit(hostReceipt(t, "evt-hr-1", intent), fp)
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || acceptance.ActionID != intent.ActionID {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	snap2 := engine.LoadMustSucceed(t)
	if _, settled := snap2.State.PendingHostActions[intent.ActionID]; settled {
		t.Fatal("intent not cleared after receipt admission")
	}
	recorded, ok := snap2.State.HostActionReceipts[intent.ActionID]
	if !ok || recorded.Operation != HostActionExecuteAdapterOperation || recorded.AdapterOperation != opTransport || recorded.Status != HostActionStatusExecuted {
		t.Fatalf("recorded receipt = %+v ok=%v", recorded, ok)
	}

	// 逐字节重发（新事件 ID）：不重复 receipt 效果，但占用新事件 ID。
	dup, err := engine.Submit(hostReceipt(t, "evt-hr-1bis", intent), fp)
	if err != nil {
		t.Fatalf("duplicate receipt: %v", err)
	}
	if dup.Status != "DUPLICATE" {
		t.Fatalf("duplicate acceptance = %+v", dup)
	}
	snap3 := engine.LoadMustSucceed(t)
	if snap3.Revision != snap2.Revision+1 || len(snap3.State.HostActionReceipts) != len(snap2.State.HostActionReceipts) || len(snap3.State.Events) != len(snap2.State.Events)+1 {
		t.Fatal("duplicate receipt did not produce exactly one event-ledger commit")
	}
}

func TestHostActionReconcileRequiresDurableUnknownReceipt(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	before := engine.LoadMustSucceed(t)
	if _, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, fp); err == nil {
		t.Fatal("reconcile succeeded before the UNKNOWN receipt was durable")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("missing UNKNOWN receipt code = %q", code)
	}
	afterRejected := engine.LoadMustSucceed(t)
	if afterRejected.Revision != before.Revision || !reflect.DeepEqual(afterRejected.State, before.State) {
		t.Fatal("reconcile without UNKNOWN receipt changed state")
	}
	if _, err := engine.Submit(unknownHostReceipt(t, "evt-host-unknown-before-reconcile", intent), fp); err != nil {
		t.Fatalf("UNKNOWN receipt: %v", err)
	}
	plan, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, fp)
	if err != nil || plan.Action != RecoveryReconcile {
		t.Fatalf("reconcile after UNKNOWN receipt = %+v err=%v", plan, err)
	}
}

// TestHostActionUnregisteredOperationAndFreeCommand：未注册 operation
// 与自由命令形态在持久化之前拒绝——宿主零执行（无 intent 落账）。
func TestHostActionUnregisteredOperationAndFreeCommand(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	before := engine.LoadMustSucceed(t)

	if _, _, err := engine.ExecuteHostAction("shell.exec", map[string]any{"cmd": "rm -rf /"}, fp); err == nil {
		t.Fatal("unregistered operation accepted")
	} else if code := rejectionCode(t, err); code != CodeOperationNotRegistered {
		t.Fatalf("unregistered code = %q", code)
	}
	if _, _, err := engine.ExecuteHostAction("", map[string]any{"cmd": "anything"}, fp); err == nil {
		t.Fatal("empty operation accepted")
	} else if code := rejectionCode(t, err); code != CodeFreeCommandForm {
		t.Fatalf("empty operation code = %q", code)
	}
	if _, _, err := engine.ExecuteHostAction(opTransport, "rm -rf / --no-preserve-root", fp); err == nil {
		t.Fatal("string command params accepted")
	} else if code := rejectionCode(t, err); code != CodeFreeCommandForm {
		t.Fatalf("free command code = %q", code)
	}
	if _, _, err := engine.ExecuteHostAction(opTransport, []string{"ls", "-la"}, fp); err == nil {
		t.Fatal("array command params accepted")
	}
	after := engine.LoadMustSucceed(t)
	if after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected host actions changed state（宿主零执行应有零落账）")
	}

	// 合法 operation + nil 参数（空对象）可持久化 intent。
	if _, _, err := engine.ExecuteHostAction(opTransport, nil, fp); err != nil {
		t.Fatalf("nil params: %v", err)
	}
}

// TestHostActionReceiptNegatives：未知 intent、provider mismatch、
// operation/参数 digest 与 intent 不一致、同 actionID 异字节回执。
func TestHostActionReceiptNegatives(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "v"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	unknown, err := NewHostActionReceiptEvent("evt-hr-u", "hact:ghost:1", opTransport,
		testProvider, "c", digestOfCanonical(map[string]any{"k": "v"}), HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(unknown, fp); err == nil {
		t.Fatal("unknown intent accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("unknown intent code = %q", code)
	}

	mismatchProvider, err := NewHostActionReceiptEvent("evt-hr-p", intent.ActionID, opTransport,
		"another-host", "c", intent.PayloadDigest, HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatchProvider, fp); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("provider code = %q", code)
	}

	wrongOperation, err := NewHostActionReceiptEvent("evt-hr-o", intent.ActionID, "op.other",
		testProvider, "c", intent.PayloadDigest, HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(wrongOperation, fp); err == nil {
		t.Fatal("wrong operation accepted")
	} else if code := rejectionCode(t, err); code != CodeIntentMismatch {
		t.Fatalf("operation code = %q", code)
	}

	wrongDigest, err := NewHostActionReceiptEvent("evt-hr-d", intent.ActionID, opTransport,
		testProvider, "c", "sha256:tampered-params", HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(wrongDigest, fp); err == nil {
		t.Fatal("wrong params digest accepted")
	} else if code := rejectionCode(t, err); code != CodeIntentMismatch {
		t.Fatalf("digest code = %q", code)
	}

	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected receipts changed state")
	}

	// 正确回执接纳后，同 actionID 异字节回执硬拒绝。
	if _, err := engine.Submit(hostReceipt(t, "evt-hr-ok", intent), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	conflicting, err := NewHostActionReceiptEvent("evt-hr-x", intent.ActionID, opTransport,
		testProvider, "c2", intent.PayloadDigest, HostActionStatusExecuted)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(conflicting, fp); err == nil {
		t.Fatal("conflicting receipt accepted")
	} else if code := rejectionCode(t, err); code != CodeReceiptConflict {
		t.Fatalf("conflict code = %q", code)
	}
	// intent 已清账：再发正确回执也是 duplicate（幂等）而非 unknown。
	dup, err := engine.Submit(hostReceipt(t, "evt-hr-ok2", intent), fp)
	if err != nil || dup.Status != "DUPLICATE" {
		t.Fatalf("post-settlement duplicate = %+v err = %v", dup, err)
	}
}

// TestHostActionReconciliationReplay：UNKNOWN side effect 完成对账后，即使
// 调用方没有收到首次响应，重试也返回已提交事实，不重新要求不存在的 intent。
func TestHostActionReconciliationReplay(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "v"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if _, err := engine.Submit(unknownHostReceipt(t, "evt-hr-unknown", intent), fp); err != nil {
		t.Fatalf("unknown receipt: %v", err)
	}
	first, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, fp)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	second, revision, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, fp)
	if err != nil {
		t.Fatalf("reconcile replay: %v", err)
	}
	if second.Action != first.Action || second.Class != first.Class || revision != 4 {
		t.Fatalf("reconcile replay = %+v revision=%d, first=%+v", second, revision, first)
	}
}

// TestHostActionSchemaNegatives：回执 payload 的 schema 负向
// （空 operation 即自由命令形态、空 provider、非法 status）。
func TestHostActionSchemaNegatives(t *testing.T) {
	cases := []struct {
		name   string
		action string
		op     authoring.OperationID
		status string
	}{
		{"empty operation", "a", "", HostActionStatusExecuted},
		{"bogus status", "a", opTransport, "OK"},
	}
	for _, tc := range cases {
		if _, err := NewHostActionReceiptEvent("evt-x", tc.action, tc.op, testProvider, "c", "sha256:d", tc.status); err == nil {
			t.Fatalf("%s accepted", tc.name)
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q", tc.name, code)
		}
	}
	if _, err := NewHostActionReceiptEvent("evt-x", "a", opTransport, "", "c", "sha256:d", HostActionStatusExecuted); err == nil {
		t.Fatal("empty provider accepted")
	}
}

// TestAdapterOperationSchemaRejectsUndeclaredData verifies that the adapter
// branch cannot persist arbitrary map fields in either intent parameters or
// receipt evidence.
func TestAdapterOperationSchemaRejectsUndeclaredData(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	before := engine.LoadMustSucceed(t)
	for _, params := range []map[string]any{
		{"command": "free-form"},
		{"target": true},
	} {
		if _, _, err := engine.ExecuteHostAction(opTransport, params, fp); err == nil {
			t.Fatalf("undeclared/ill-typed parameters accepted: %+v", params)
		} else if code := rejectionCode(t, err); code != CodeOperationSchemaInvalid {
			t.Fatalf("parameter rejection code = %q", code)
		}
	}
	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("rejected adapter parameters changed state")
	}

	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp)
	if err != nil {
		t.Fatalf("valid adapter intent: %v", err)
	}
	receipt, err := NewAdapterHostActionReceiptEvent(
		"evt-host-evidence-invalid", intent.ActionID, opTransport, testProvider, "corr",
		intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{"unregistered": "value"},
	)
	if err != nil {
		t.Fatalf("construct structurally valid receipt: %v", err)
	}
	beforeReceipt := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(receipt, fp); err == nil {
		t.Fatal("undeclared adapter evidence accepted")
	} else if code := rejectionCode(t, err); code != CodeOperationSchemaInvalid {
		t.Fatalf("evidence rejection code = %q", code)
	}
	if after := engine.LoadMustSucceed(t); after.Revision != beforeReceipt.Revision || !reflect.DeepEqual(after.State, beforeReceipt.State) {
		t.Fatal("rejected adapter evidence changed state")
	}
}

// TestFailedHostActionReceiptRoutesAndAllowsRetry verifies that FAILED does not
// masquerade as EXECUTED and clear a retryable pending intent.
func TestFailedHostActionReceiptRoutesAndAllowsRetry(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	failed, err := NewAdapterHostActionReceiptEvent(
		"evt-host-failed", intent.ActionID, opTransport, testProvider, "corr-failed",
		intent.PayloadDigest, HostActionStatusFailed, authoring.FailureTransientEngine, map[string]any{},
	)
	if err != nil {
		t.Fatalf("failed receipt event: %v", err)
	}
	acceptance, err := engine.Submit(failed, fp)
	if err != nil || acceptance.RecoveryAction != string(RecoveryResumeAttempt) {
		t.Fatalf("failed receipt acceptance = %+v err=%v", acceptance, err)
	}
	afterFailure := engine.LoadMustSucceed(t)
	if len(afterFailure.State.HostActionFailures[intent.ActionID]) != 1 || len(afterFailure.State.RecoveryRecords) != 1 {
		t.Fatalf("host failure/recovery ledger = %+v / %+v", afterFailure.State.HostActionFailures, afterFailure.State.RecoveryRecords)
	}
	if _, pending := afterFailure.State.PendingHostActions[intent.ActionID]; !pending {
		t.Fatal("retryable FAILED receipt cleared pending intent")
	}
	if _, settled := afterFailure.State.HostActionReceipts[intent.ActionID]; settled {
		t.Fatal("FAILED receipt occupied settled receipt slot")
	}
	if retried, err := engine.Submit(hostReceipt(t, "evt-host-retried", intent), fp); err != nil || retried.Status != "ACCEPTED" {
		t.Fatalf("EXECUTED retry = %+v err=%v", retried, err)
	}
}

// TestAgentHostActionVariantsRequireDurableLifecycleEvidence exercises both
// non-adapter branches of the closed union and verifies their typed payloads.
func TestAgentHostActionVariantsRequireDurableLifecycleEvidence(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-agent-spawn", reviewAction, "agent-review-1"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	start, err := NewLifecycleEventEvent("evt-agent-start", testProvider, "agent-review-1", LifecycleStart)
	if err != nil {
		t.Fatalf("start event: %v", err)
	}
	if _, err := engine.Submit(start, fp); err != nil {
		t.Fatalf("start: %v", err)
	}
	resume, _, err := engine.ResumeAgent(reviewAction, "agent-review-1", fp)
	if err != nil {
		t.Fatalf("resume intent: %v", err)
	}
	if resume.Operation != HostActionResumeAgent || resume.Resume == nil || resume.Terminate != nil || resume.Adapter != nil {
		t.Fatalf("resume union = %+v", resume)
	}
	resumeReceipt, err := NewAgentHostActionReceiptEvent(
		"evt-agent-resumed", resume.ActionID, HostActionResumeAgent, testProvider, "corr-resume",
		resume.PayloadDigest, HostActionStatusExecuted, "", LifecycleEvidence{Identity: "agent-review-1", Event: LifecycleStart},
	)
	if err != nil {
		t.Fatalf("resume receipt: %v", err)
	}
	if _, err := engine.Submit(resumeReceipt, fp); err != nil {
		t.Fatalf("submit resume receipt: %v", err)
	}

	stop, err := NewLifecycleEventEvent("evt-agent-stop", testProvider, "agent-review-1", LifecycleStop)
	if err != nil {
		t.Fatalf("stop event: %v", err)
	}
	if _, err := engine.Submit(stop, fp); err != nil {
		t.Fatalf("stop: %v", err)
	}
	terminate, _, err := engine.TerminateAgent(reviewAction, "agent-review-1", "replace stale worker", fp)
	if err != nil {
		t.Fatalf("terminate intent: %v", err)
	}
	if terminate.Operation != HostActionTerminateAgent || terminate.Terminate == nil || terminate.Resume != nil || terminate.Adapter != nil {
		t.Fatalf("terminate union = %+v", terminate)
	}
	terminateReceipt, err := NewAgentHostActionReceiptEvent(
		"evt-agent-terminated", terminate.ActionID, HostActionTerminateAgent, testProvider, "corr-terminate",
		terminate.PayloadDigest, HostActionStatusExecuted, "", LifecycleEvidence{Identity: "agent-review-1", Event: LifecycleStop},
	)
	if err != nil {
		t.Fatalf("terminate receipt: %v", err)
	}
	if _, err := engine.Submit(terminateReceipt, fp); err != nil {
		t.Fatalf("submit terminate receipt: %v", err)
	}
	final := engine.LoadMustSucceed(t)
	if final.State.HostActionReceipts[resume.ActionID].Operation != HostActionResumeAgent || final.State.HostActionReceipts[terminate.ActionID].Operation != HostActionTerminateAgent {
		t.Fatalf("agent HostAction receipts = %+v", final.State.HostActionReceipts)
	}
}
