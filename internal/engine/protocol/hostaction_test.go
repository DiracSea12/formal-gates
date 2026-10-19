package protocol

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	definition "formal-gates/internal/engine/testkit/workflow"
)

// opTransport 是 checked-in 定义注册的唯一 host adapter operation。
const opTransport = "op.fan.transport"

// prepareHostIntentForReceipt persists adapter protocol input for receipt tests.
func prepareHostIntentForReceipt(t *testing.T, engine *Engine, operation authoring.OperationID, params any, correlation string, input Observation) (HostActionIntent, uint64, error) {
	t.Helper()
	revision, state, err := engine.load()
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	intent, err := engine.prepareAdapterIntent(state, revision+1, operation, params, correlation)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	state.PendingHostActions[intent.ActionID] = intent
	committed, err := engine.commit(state, revision, input)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	return intent, committed, nil
}

// hostReceipt 构造与 intent 精确匹配的 EXECUTED 回执事件。
func hostReceipt(t *testing.T, id EventID, intent HostActionIntent) Event {
	t.Helper()
	if intent.Adapter == nil {
		t.Fatal("hostReceipt requires adapter intent")
	}
	ev, err := NewAdapterHostActionReceiptEvent(id, intent.ActionID, intent.Adapter.Operation,
		testProvider, "corr-1", intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
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
	ev, err := NewAdapterHostActionReceiptEvent(id, intent.ActionID, intent.Adapter.Operation,
		testProvider, "corr-unknown", intent.PayloadDigest, HostActionStatusUnknown, "", map[string]any{})
	if err != nil {
		t.Fatalf("unknown host action receipt event: %v", err)
	}
	return ev
}

// TestHostActionIntentPersistedBeforeExecute：intent 先持久化（pending
// 落账、含参数 digest），回执接纳后 intent 清账、回执留档。
func TestHostActionIntentPersistedBeforeExecute(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	params := map[string]any{"stage": "fan.slice", "generation": float64(2)}
	intent, revision, err := prepareHostIntentForReceipt(t, engine, opTransport, params, "", observedInput)
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

	acceptance, err := engine.Submit(hostReceipt(t, "evt-hr-1", intent), observedInput)
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
	dup, err := engine.Submit(hostReceipt(t, "evt-hr-1bis", intent), observedInput)
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
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "fan.slice"}, "", observedInput)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	before := engine.LoadMustSucceed(t)
	if _, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, observedInput); err == nil {
		t.Fatal("reconcile succeeded before the UNKNOWN receipt was durable")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("missing UNKNOWN receipt code = %q", code)
	}
	afterRejected := engine.LoadMustSucceed(t)
	if afterRejected.Revision != before.Revision || !reflect.DeepEqual(afterRejected.State, before.State) {
		t.Fatal("reconcile without UNKNOWN receipt changed state")
	}
	if _, err := engine.Submit(unknownHostReceipt(t, "evt-host-unknown-before-reconcile", intent), observedInput); err != nil {
		t.Fatalf("UNKNOWN receipt: %v", err)
	}
	plan, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, observedInput)
	if err != nil || plan.Action != RecoveryReconcile {
		t.Fatalf("reconcile after UNKNOWN receipt = %+v err=%v", plan, err)
	}
}

// TestAdapterIntentRejectsUnknownOperationAndCommandParameters verifies that
// unknown operations and command-shaped parameters cannot become durable intents.
func TestAdapterIntentRejectsUnknownOperationAndCommandParameters(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	before := engine.LoadMustSucceed(t)

	if _, _, err := prepareHostIntentForReceipt(t, engine, "shell.exec", map[string]any{"cmd": "rm -rf /"}, "", observedInput); err == nil {
		t.Fatal("unregistered operation accepted")
	} else if code := rejectionCode(t, err); code != CodeOperationNotRegistered {
		t.Fatalf("unregistered code = %q", code)
	}
	if _, _, err := prepareHostIntentForReceipt(t, engine, "", map[string]any{"cmd": "anything"}, "", observedInput); err == nil {
		t.Fatal("empty operation accepted")
	} else if code := rejectionCode(t, err); code != CodeOperationNotRegistered {
		t.Fatalf("empty operation code = %q", code)
	}
	if _, _, err := prepareHostIntentForReceipt(t, engine, opTransport, "rm -rf / --no-preserve-root", "", observedInput); err == nil {
		t.Fatal("string command params accepted")
	} else if code := rejectionCode(t, err); code != CodeFreeCommandForm {
		t.Fatalf("free command code = %q", code)
	}
	if _, _, err := prepareHostIntentForReceipt(t, engine, opTransport, []string{"ls", "-la"}, "", observedInput); err == nil {
		t.Fatal("array command params accepted")
	}
	after := engine.LoadMustSucceed(t)
	if after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected host actions changed state（宿主零执行应有零落账）")
	}

	// 合法 operation + nil 参数（空对象）可持久化 intent。
	if _, _, err := prepareHostIntentForReceipt(t, engine, opTransport, nil, "", observedInput); err != nil {
		t.Fatalf("nil params: %v", err)
	}
}

// TestHostActionReceiptNegatives：未知 intent、provider mismatch、
// operation/参数 digest 与 intent 不一致、同 actionID 异字节回执。
func TestHostActionReceiptNegatives(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "v"}, "", observedInput)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	unknown, err := NewAdapterHostActionReceiptEvent("evt-hr-u", "hact:ghost:1", opTransport,
		testProvider, "c", digestOfCanonical(map[string]any{"k": "v"}), HostActionStatusExecuted, "", map[string]any{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(unknown, observedInput); err == nil {
		t.Fatal("unknown intent accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("unknown intent code = %q", code)
	}

	mismatchProvider, err := NewAdapterHostActionReceiptEvent("evt-hr-p", intent.ActionID, opTransport,
		"another-host", "c", intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatchProvider, observedInput); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("provider code = %q", code)
	}

	wrongOperation, err := NewAdapterHostActionReceiptEvent("evt-hr-o", intent.ActionID, "op.other",
		testProvider, "c", intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(wrongOperation, observedInput); err == nil {
		t.Fatal("wrong operation accepted")
	} else if code := rejectionCode(t, err); code != CodeIntentMismatch {
		t.Fatalf("operation code = %q", code)
	}

	wrongDigest, err := NewAdapterHostActionReceiptEvent("evt-hr-d", intent.ActionID, opTransport,
		testProvider, "c", "sha256:tampered-params", HostActionStatusExecuted, "", map[string]any{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(wrongDigest, observedInput); err == nil {
		t.Fatal("wrong params digest accepted")
	} else if code := rejectionCode(t, err); code != CodeIntentMismatch {
		t.Fatalf("digest code = %q", code)
	}

	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected receipts changed state")
	}

	// 正确回执接纳后，同 actionID 异字节回执硬拒绝。
	if _, err := engine.Submit(hostReceipt(t, "evt-hr-ok", intent), observedInput); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	conflicting, err := NewAdapterHostActionReceiptEvent("evt-hr-x", intent.ActionID, opTransport,
		testProvider, "c2", intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(conflicting, observedInput); err == nil {
		t.Fatal("conflicting receipt accepted")
	} else if code := rejectionCode(t, err); code != CodeReceiptConflict {
		t.Fatalf("conflict code = %q", code)
	}
	// intent 已清账：再发正确回执也是 duplicate（幂等）而非 unknown。
	dup, err := engine.Submit(hostReceipt(t, "evt-hr-ok2", intent), observedInput)
	if err != nil || dup.Status != "DUPLICATE" {
		t.Fatalf("post-settlement duplicate = %+v err = %v", dup, err)
	}
	t.Run("protocol host receipt provider", func(t *testing.T) {
		engine, dir := newTestEngine(t)
		collectCalls := 0

		view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
		if err != nil {
			t.Fatalf("view: %v", err)
		}
		for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
			if err := view.CompleteStep(step, engine.cfg.Definition); err != nil {
				t.Fatalf("complete %s: %v", step, err)
			}
		}

		confirmation := &IntakeConfirmationReceipt{
			Source: "fake-adapter", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
			RequirementSource: "requirement.md", RequirementRevision: "rev-1",
			Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
			SolutionRevision: "s1", SolutionDigest: "sha256:solution",
		}
		if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", confirmation, "", "", nil); err != nil {
			t.Fatalf("init: %v", err)
		}
		if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:testkit-intake"}, Observation{}); err != nil {
			t.Fatalf("intake: %v", err)
		}

		plan, _, err := engine.Plan(Observation{})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, Observation{})
		if err != nil || len(issued) != 1 {
			t.Fatalf("issue: %+v err=%v", issued, err)
		}

		observedInput := Observation{}
		observedInput.Read = func() (decision.Observation, error) { collectCalls++; return decision.Observation{}, nil }
		intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "protocol"}, "", observedInput)
		if err != nil {
			t.Fatalf("intent: %v", err)
		}
		event, err := NewAdapterHostActionReceiptEvent("protocol-wrong-provider-host-receipt", intent.ActionID, intent.Adapter.Operation, "wrong-provider", "host-correlation", intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		before := engine.LoadMustSucceed(t)
		beforeBytes := stateBytesOf(t, dir)
		beforeCalls := collectCalls
		if _, err := engine.Submit(event, observedInput); err == nil {
			t.Fatal("wrong provider accepted")
		} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
			t.Fatalf("provider code=%s", code)
		}
		after := engine.LoadMustSucceed(t)
		if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) || !reflect.DeepEqual(stateBytesOf(t, dir), beforeBytes) || collectCalls != beforeCalls {
			t.Fatal("provider rejection changed state or collected external facts")
		}
	})

}

// TestHostActionReconciliationReplay：UNKNOWN side effect 完成对账后，即使
// 调用方没有收到首次响应，重试也返回已提交事实，不重新要求不存在的 intent。
func TestHostActionReconciliationReplay(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "v"}, "", observedInput)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if _, err := engine.Submit(unknownHostReceipt(t, "evt-hr-unknown", intent), observedInput); err != nil {
		t.Fatalf("unknown receipt: %v", err)
	}
	first, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, observedInput)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	second, revision, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed", true, false, observedInput)
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
	t.Parallel()
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
		if _, err := NewAdapterHostActionReceiptEvent("evt-x", tc.action, tc.op, testProvider, "c", "sha256:d", tc.status, "", map[string]any{}); err == nil {
			t.Fatalf("%s accepted", tc.name)
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q", tc.name, code)
		}
	}
	if _, err := NewAdapterHostActionReceiptEvent("evt-x", "a", opTransport, "", "c", "sha256:d", HostActionStatusExecuted, "", map[string]any{}); err == nil {
		t.Fatal("empty provider accepted")
	}
}

// TestAdapterOperationSchemaRejectsUndeclaredData verifies that the adapter
// branch cannot persist arbitrary map fields in either intent parameters or
// receipt evidence.
func TestAdapterOperationSchemaRejectsUndeclaredData(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	before := engine.LoadMustSucceed(t)
	for _, params := range []map[string]any{
		{"command": "free-form"},
		{"stage": true},
	} {
		if _, _, err := prepareHostIntentForReceipt(t, engine, opTransport, params, "", observedInput); err == nil {
			t.Fatalf("undeclared/ill-typed parameters accepted: %+v", params)
		} else if code := rejectionCode(t, err); code != CodeOperationSchemaInvalid {
			t.Fatalf("parameter rejection code = %q", code)
		}
	}
	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
		t.Fatal("rejected adapter parameters changed state")
	}

	intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "fan.slice"}, "", observedInput)
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
	if _, err := engine.Submit(receipt, observedInput); err == nil {
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
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	observedInput := Observation{}
	intent, _, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "fan.slice"}, "", observedInput)
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
	acceptance, err := engine.Submit(failed, observedInput)
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
	if retried, err := engine.Submit(hostReceipt(t, "evt-host-retried", intent), observedInput); err != nil || retried.Status != "ACCEPTED" {
		t.Fatalf("EXECUTED retry = %+v err=%v", retried, err)
	}
}

// TestAgentHostActionVariantsRequireDurableLifecycleEvidence exercises both
// non-adapter branches of the closed union and verifies their typed payloads.
func TestAgentHostActionVariantsRequireDurableLifecycleEvidence(t *testing.T) {
	t.Parallel()
	engine, _, plan, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-agent-spawn", reviewAction, "agent-review-1"), observedInput); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	start, err := NewLifecycleEventEvent("evt-agent-start", testProvider, "agent-review-1", LifecycleStart)
	if err != nil {
		t.Fatalf("start event: %v", err)
	}
	if _, err := engine.Submit(start, observedInput); err != nil {
		t.Fatalf("start: %v", err)
	}
	revision, state, err := engine.load()
	if err != nil {
		t.Fatalf("load resume inputs: %v", err)
	}
	resume, err := engine.agentHostIntent(state, HostActionResumeAgent, reviewAction, "agent-review-1", "", "", revision+1)
	if err != nil {
		t.Fatalf("resume intent: %v", err)
	}
	state.PendingHostActions[resume.ActionID] = resume
	if _, err := engine.commit(state, revision, observedInput); err != nil {
		t.Fatalf("persist resume intent: %v", err)
	}
	if resume.Operation != HostActionResumeAgent || resume.Resume == nil || resume.Terminate != nil || resume.Adapter != nil {
		t.Fatalf("resume union = %+v", resume)
	}
	resumedStart, err := NewCorrelatedLifecycleEvent("evt-resume-start", testProvider, resume.ActionID, "agent-review-1", LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(resumedStart, observedInput); err != nil {
		t.Fatal(err)
	}
	resumeReceipt, err := NewAgentHostActionReceiptEvent(
		"evt-agent-resumed", resume.ActionID, HostActionResumeAgent, testProvider, "corr-resume",
		resume.PayloadDigest, HostActionStatusExecuted, "", LifecycleEvidence{Identity: "agent-review-1", Event: LifecycleStart},
	)
	if err != nil {
		t.Fatalf("resume receipt: %v", err)
	}
	if _, err := engine.Submit(resumeReceipt, observedInput); err != nil {
		t.Fatalf("submit resume receipt: %v", err)
	}

	stop, err := NewCorrelatedLifecycleEvent("evt-agent-stop", testProvider, resume.ActionID, "agent-review-1", LifecycleStop)
	if err != nil {
		t.Fatalf("stop event: %v", err)
	}
	if _, err := engine.Submit(stop, observedInput); err != nil {
		t.Fatalf("stop: %v", err)
	}
	revision, state, err = engine.load()
	if err != nil {
		t.Fatalf("load terminate inputs: %v", err)
	}
	terminate, err := engine.agentHostIntent(state, HostActionTerminateAgent, reviewAction, "agent-review-1", "replace stale worker", "", revision+1)
	if err != nil {
		t.Fatalf("terminate intent: %v", err)
	}
	state.PendingHostActions[terminate.ActionID] = terminate
	if _, err := engine.commit(state, revision, observedInput); err != nil {
		t.Fatalf("persist terminate intent: %v", err)
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
	if _, err := engine.Submit(terminateReceipt, observedInput); err != nil {
		t.Fatalf("submit terminate receipt: %v", err)
	}
	final := engine.LoadMustSucceed(t)
	if final.State.HostActionReceipts[resume.ActionID].Operation != HostActionResumeAgent || final.State.HostActionReceipts[terminate.ActionID].Operation != HostActionTerminateAgent {
		t.Fatalf("agent HostAction receipts = %+v", final.State.HostActionReceipts)
	}
}

func TestAttachedWorkerLifecycleUsesOriginalTransportCorrelation(t *testing.T) {
	t.Parallel()
	for _, correlation := range []string{"native-worker", "original-transport"} {
		t.Run(correlation, func(t *testing.T) {
			e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v6")
			receipt := s.SpawnReceipts[pending.ActionID]
			receipt.Status, receipt.Correlation, receipt.LifecycleIdentity = SpawnStatusAttached, "original-transport", "native-worker"
			s.SpawnReceipts[pending.ActionID] = receipt
			s.LifecycleEvents = []LifecycleEventRecord{{Provider: receipt.Provider, Correlation: correlation, Identity: receipt.agentIdentity(), Event: LifecycleStart}, {Provider: receipt.Provider, Correlation: correlation, Identity: receipt.agentIdentity(), Event: LifecycleStop}}
			terminate, err := e.agentHostIntent(s, HostActionTerminateAgent, pending.ActionID, receipt.agentIdentity(), "Stop original work", "", 3)
			if err != nil {
				t.Fatal(err)
			}
			proof := &HostActionReceiptPayload{ActionID: terminate.ActionID, Operation: HostActionTerminateAgent, Provider: receipt.Provider, PayloadDigest: terminate.PayloadDigest, Status: HostActionStatusExecuted, LifecycleEvidence: &LifecycleEvidence{Identity: receipt.agentIdentity(), Event: LifecycleStop}}
			exact := correlation == receipt.Correlation
			if err := e.validateHostActionEvidence(s, terminate, proof); (err == nil) != exact {
				t.Fatal("termination evidence did not retain the original correlation and identity", err)
			}
			if _, err := e.stopControlledWorkers(s, "stop-original", "Stop ", 4, true); err != nil {
				t.Fatal(err)
			}
			_, retired := s.ObsoleteActions[pending.ActionID]
			if retired != exact {
				t.Fatal("worker retirement used a different invocation's stop", retired, exact)
			}
			if !exact && (s.PendingActions[pending.ActionID] != pending || len(s.PendingHostActions) != 1) {
				t.Fatal("unmatched lifecycle evidence lost the original termination duty")
			}
		})
	}
}

// UNKNOWN observations retain the pending receipt until fulfillment; replay
// returns the durable reconciliation without another transaction.
func TestHostActionUnknownObservationRoutesAndReplay(t *testing.T) {
	t.Parallel()
	engine, dir := newTestEngine(t)

	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
		if err := view.CompleteStep(step, engine.cfg.Definition); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}

	confirmation := &IntakeConfirmationReceipt{
		Source: "fake-adapter", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
		RequirementSource: "requirement.md", RequirementRevision: "rev-1",
		Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
		SolutionRevision: "s1", SolutionDigest: "sha256:solution",
	}
	if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", confirmation, "", "", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:testkit-intake"}, Observation{}); err != nil {
		t.Fatalf("intake: %v", err)
	}

	plan, _, err := engine.Plan(Observation{})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, Observation{})
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue: %+v err=%v", issued, err)
	}

	observedInput := Observation{}
	baseline := engine.LoadMustSucceed(t)
	intent, revision, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": "protocol-host-action"}, "", observedInput)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	afterIntent := engine.LoadMustSucceed(t)
	if intent.ActionID == "" || revision != baseline.Revision+1 || afterIntent.Revision != revision {
		t.Fatalf("intent revision=%d snapshot=%d baseline=%d", revision, afterIntent.Revision, baseline.Revision)
	}
	if _, pending := afterIntent.State.PendingHostActions[intent.ActionID]; !pending {
		t.Fatal("intent not durable before receipt")
	}
	unknown, err := NewAdapterHostActionReceiptEvent("fake-host-receipt-1", intent.ActionID, intent.Adapter.Operation, testProvider, "fake-correlation-1", intent.PayloadDigest, HostActionStatusUnknown, "", map[string]any{})
	if err != nil {
		t.Fatalf("UNKNOWN event: %v", err)
	}
	acceptance, err := engine.Submit(unknown, observedInput)
	if err != nil || acceptance.RecoveryAction != string(RecoveryReconcile) {
		t.Fatalf("UNKNOWN acceptance=%+v err=%v", acceptance, err)
	}
	before := engine.LoadMustSucceed(t)
	if acceptance.Revision != afterIntent.Revision+1 || before.Revision != acceptance.Revision {
		t.Fatalf("UNKNOWN revision=%d snapshot=%d", acceptance.Revision, before.Revision)
	}
	for _, tc := range []struct {
		digest   string
		conflict bool
		action   RecoveryAction
	}{
		{"sha256:not-yet", false, RecoveryWait}, {"sha256:conflict", true, RecoveryOperator},
	} {
		plan, revision, err := engine.ReconcileHostAction(intent.ActionID, tc.digest, false, tc.conflict, observedInput)
		if err != nil || plan.Action != tc.action {
			t.Fatalf("observation %s: %+v err=%v", tc.digest, plan, err)
		}
		after := engine.LoadMustSucceed(t)
		if revision != before.Revision+1 || after.Revision != revision {
			t.Fatalf("observation revision=%d snapshot=%d before=%d", revision, after.Revision, before.Revision)
		}
		if !reflect.DeepEqual(after.State.PendingHostActions[intent.ActionID], before.State.PendingHostActions[intent.ActionID]) || !reflect.DeepEqual(after.State.HostActionReceipts[intent.ActionID], before.State.HostActionReceipts[intent.ActionID]) || after.State.HostActionReceipts[intent.ActionID].Status != HostActionStatusUnknown {
			t.Fatal("unfulfilled observation changed pending intent or UNKNOWN receipt")
		}
		if _, reconciled := after.State.ReconciledEffects[intent.ActionID]; reconciled {
			t.Fatal("unfulfilled observation recorded fulfillment")
		}
		before = after
	}
	fulfilledPlan, revision, err := engine.ReconcileHostAction(intent.ActionID, "sha256:fulfilled", true, false, observedInput)
	if err != nil || fulfilledPlan.Action != RecoveryReconcile {
		t.Fatalf("fulfilled: %+v err=%v", fulfilledPlan, err)
	}
	settled := engine.LoadMustSucceed(t)
	if revision != before.Revision+1 || settled.Revision != revision {
		t.Fatalf("fulfilled revision=%d snapshot=%d", revision, settled.Revision)
	}
	if _, pending := settled.State.PendingHostActions[intent.ActionID]; pending {
		t.Fatal("fulfilled intent remains pending")
	}
	effect, ok := settled.State.ReconciledEffects[intent.ActionID]
	if !ok || effect.Status != "FULFILLED" || effect.ObservationDigest != "sha256:fulfilled" {
		t.Fatalf("effect=%+v present=%v", effect, ok)
	}
	if receipt := settled.State.HostActionReceipts[intent.ActionID]; receipt.Status != HostActionStatusReconciled {
		t.Fatalf("receipt=%+v", receipt)
	}
	beforeReplay := stateBytesOf(t, dir)
	replayed, revision, err := engine.ReconcileHostAction(intent.ActionID, "sha256:fulfilled", true, false, observedInput)
	if err != nil || replayed.Action != RecoveryReconcile || revision != settled.Revision {
		t.Fatalf("replay=%+v revision=%d err=%v", replayed, revision, err)
	}
	afterReplay := engine.LoadMustSucceed(t)
	if afterReplay.Revision != settled.Revision || !reflect.DeepEqual(afterReplay.State, settled.State) || !reflect.DeepEqual(stateBytesOf(t, dir), beforeReplay) {
		t.Fatal("replay changed durable state")
	}
}

// Reconciliation completes its concrete frontier and refills the next worker.
func TestReconciledHostActionSettlesFrontierAndRefills(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ stage, observation string }{
		{"reconciled", "sha256:fulfilled"}, {"qa-reconciled", "sha256:qa-observation"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			engine, _ := newTestEngine(t)
			view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatalf("view: %v", err)
			}
			for _, step := range []authoring.StepID{"entry.parse", "entry.persist", "fan.split"} {
				if err := view.CompleteStep(step, engine.cfg.Definition); err != nil {
					t.Fatalf("complete %s: %v", step, err)
				}
			}

			confirmation := &IntakeConfirmationReceipt{
				Source: "fake-adapter", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
				RequirementSource: "requirement.md", RequirementRevision: "rev-1",
				Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
				SolutionRevision: "s1", SolutionDigest: "sha256:solution",
			}
			if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", confirmation, "", "", nil); err != nil {
				t.Fatalf("init: %v", err)
			}
			if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:testkit-intake"}, Observation{}); err != nil {
				t.Fatalf("intake: %v", err)
			}

			observedInput := Observation{}
			baseline := engine.LoadMustSucceed(t)
			intent, revision, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": tc.stage}, "", observedInput)
			if err != nil || intent.Step != "fan.transport" || revision != baseline.Revision+1 {
				t.Fatalf("intent=%+v revision=%d err=%v", intent, revision, err)
			}
			unknown, err := NewAdapterHostActionReceiptEvent("fake-host-receipt-1", intent.ActionID, intent.Adapter.Operation, testProvider, "fake-correlation-1", intent.PayloadDigest, HostActionStatusUnknown, "", map[string]any{})
			if err != nil {
				t.Fatalf("UNKNOWN event: %v", err)
			}
			acceptance, err := engine.Submit(unknown, observedInput)
			if err != nil || acceptance.RecoveryAction != string(RecoveryReconcile) || acceptance.Revision != revision+1 {
				t.Fatalf("UNKNOWN acceptance=%+v err=%v", acceptance, err)
			}
			plan, revision, err := engine.ReconcileHostAction(intent.ActionID, tc.observation, true, false, observedInput)
			if err != nil || plan.Action != RecoveryReconcile || revision != acceptance.Revision+1 {
				t.Fatalf("reconcile=%+v revision=%d err=%v", plan, revision, err)
			}
			snapshot := engine.LoadMustSucceed(t)
			if snapshot.Revision != revision {
				t.Fatalf("snapshot revision=%d want=%d", snapshot.Revision, revision)
			}
			for _, step := range []authoring.StepID{"fan.transport", "fan.slice", "fan.join"} {
				if !slices.Contains(snapshot.State.Completed, step) {
					t.Fatalf("missing completed %s: %v", step, snapshot.State.Completed)
				}
			}
			if _, pending := snapshot.State.PendingHostActions[intent.ActionID]; pending {
				t.Fatal("reconciled intent remains pending")
			}
			receipt, ok := snapshot.State.HostActionReceipts[intent.ActionID]
			if !ok || receipt.Step != "fan.transport" || receipt.Status != HostActionStatusReconciled {
				t.Fatalf("receipt=%+v present=%v", receipt, ok)
			}
			if _, refilled := snapshot.State.PendingActions["act:review/review.worker"]; !refilled {
				t.Fatalf("missing exact refill: %+v", snapshot.State.PendingActions)
			}
			var refilled []PendingAction
			for _, pending := range snapshot.State.PendingActions {
				if pending.Step == "review.worker" {
					refilled = append(refilled, pending)
				}
			}
			if len(refilled) != 1 || refilled[0].AttemptID == "" {
				t.Fatalf("refill=%+v", snapshot.State.PendingActions)
			}
		})
	}
}

// Identical operations settle only the Step carried by their pending intent.
func TestHostActionRepeatedOperationSettlesBoundStep(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		node, parse, askOne, askTwo, hostOne, hostTwo string
		requestOne, requestTwo, decideOne, decideTwo  EventID
	}{
		{"binding", "entry.parse", "ask.one", "ask.two", "host.one", "host.two", "binding-request-one", "binding-request-two", "binding-decision-one", "binding-decision-two"},
		{"repair", "repair.parse", "repair.ask.one", "repair.ask.two", "repair.host.one", "repair.host.two", "qa-repair-request-one", "qa-repair-request-two", "qa-repair-decision-one", "qa-repair-decision-two"},
	} {
		t.Run(tc.node, func(t *testing.T) {
			header := func(id string, deps ...authoring.StepID) authoring.Header {
				return authoring.Header{ID: authoring.StepID(id), NodeID: authoring.NodeID(tc.node), Dependencies: deps, DefinitionVersion: definition.Version}
			}
			io := authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out"}
			parse, err := authoring.NewLocalStep(header(tc.parse), io, authoring.LocalSpec{Handler: "engine.entry.parse"})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			steps := []authoring.Step{parse}
			for _, id := range []string{tc.askOne, tc.askTwo} {
				ask, err := authoring.NewHumanAskStep(header(id, authoring.StepID(tc.parse)), authoring.HumanAskSpec{AskKind: "decision", RequestSchema: "schema.ask.decision.request", ResponseSchema: "schema.ask.decision.response", FreshnessTTL: time.Minute})
				if err != nil {
					t.Fatalf("ask %s: %v", id, err)
				}
				steps = append(steps, ask)
			}
			for _, id := range []string{tc.hostOne, tc.hostTwo} {
				host, err := authoring.NewHostActionStep(header(id, authoring.StepID(tc.parse)), io, authoring.HostActionSpec{Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI, Operation: opTransport, Schema: "schema.candidate.review-range", Timeout: time.Minute})
				if err != nil {
					t.Fatalf("host %s: %v", id, err)
				}
				steps = append(steps, host)
			}
			compiled, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: authoring.NodeID(tc.node), Steps: steps}, definition.Registry())
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			engine, _ := newTestEngine(t)
			engine, err = New(engine.store, Config{Definition: compiled, Registry: definition.Registry(), Capacity: 4, InternalSteps: fixtureInternalSteps()})
			if err != nil {
				t.Fatalf("engine: %v", err)
			}
			view, err := decision.NewState(compiled.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatalf("view: %v", err)
			}
			if err := view.CompleteStep(authoring.StepID(tc.parse), compiled); err != nil {
				t.Fatalf("complete parse: %v", err)
			}

			confirmation := &IntakeConfirmationReceipt{
				Source: "fake-adapter", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
				RequirementSource: "requirement.md", RequirementRevision: "rev-1",
				Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
				SolutionRevision: "s1", SolutionDigest: "sha256:solution",
			}
			if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", confirmation, "", "", nil); err != nil {
				t.Fatalf("init: %v", err)
			}
			if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:testkit-intake"}, Observation{}); err != nil {
				t.Fatalf("intake: %v", err)
			}

			observedInput := Observation{}
			for _, pair := range []struct{ request, decide EventID }{{tc.requestOne, tc.decideOne}, {tc.requestTwo, tc.decideTwo}} {
				request, err := NewRequestEvent(pair.request, ControlAbort, AskOption{ID: "cancel", Label: "Keep running"})
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				if _, err := engine.Submit(request, observedInput); err != nil {
					t.Fatalf("submit request: %v", err)
				}
			}
			for _, pair := range []struct{ request, decide EventID }{{tc.requestOne, tc.decideOne}, {tc.requestTwo, tc.decideTwo}} {
				token, err := engine.Freshness(RequestID(pair.request))
				if err != nil {
					t.Fatalf("freshness: %v", err)
				}
				decide, err := NewDecideEvent(pair.decide, RequestID(pair.request), token, "cancel")
				if err != nil {
					t.Fatalf("decide: %v", err)
				}
				if _, err := engine.Submit(decide, observedInput); err != nil {
					t.Fatalf("submit decide: %v", err)
				}
			}
			var intents []HostActionIntent
			for index, input := range []struct {
				stage, step string
				id          EventID
				correlation string
			}{
				{"first", tc.hostOne, "fake-host-receipt-1", "fake-correlation-1"}, {"second", tc.hostTwo, "fake-host-receipt-2", "fake-correlation-2"},
			} {
				before := engine.LoadMustSucceed(t)
				intent, revision, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": input.stage}, "", observedInput)
				if err != nil || intent.Step != authoring.StepID(input.step) || revision != before.Revision+1 {
					t.Fatalf("intent=%+v revision=%d err=%v", intent, revision, err)
				}
				event, err := NewAdapterHostActionReceiptEvent(input.id, intent.ActionID, intent.Adapter.Operation, testProvider, input.correlation, intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{})
				if err != nil {
					t.Fatalf("receipt: %v", err)
				}
				accepted, err := engine.Submit(event, observedInput)
				if err != nil || accepted.Status != "ACCEPTED" || accepted.Revision != revision+1 {
					t.Fatalf("acceptance=%+v err=%v", accepted, err)
				}
				snapshot := engine.LoadMustSucceed(t)
				if snapshot.Revision != accepted.Revision {
					t.Fatalf("snapshot revision=%d acceptance=%d", snapshot.Revision, accepted.Revision)
				}
				receipt, ok := snapshot.State.HostActionReceipts[intent.ActionID]
				if !ok || receipt.Step != authoring.StepID(input.step) || receipt.Status != HostActionStatusExecuted || !slices.Contains(snapshot.State.Completed, authoring.StepID(input.step)) {
					t.Fatalf("receipt=%+v completed=%v", receipt, snapshot.State.Completed)
				}
				if index == 0 && slices.Contains(snapshot.State.Completed, authoring.StepID(tc.hostTwo)) {
					t.Fatal("first receipt completed the second step")
				}
				intents = append(intents, intent)
			}
			snapshot := engine.LoadMustSucceed(t)
			for index, step := range []string{tc.hostOne, tc.hostTwo} {
				if !slices.Contains(snapshot.State.Completed, authoring.StepID(step)) || snapshot.State.HostActionReceipts[intents[index].ActionID].Step != authoring.StepID(step) {
					t.Fatalf("final receipts=%+v completed=%v", snapshot.State.HostActionReceipts, snapshot.State.Completed)
				}
			}
		})
	}
}

// Registered schemas preserve normalized parameters and complete receipt fields.
func TestAdapterReceiptPreservesRegisteredSchemaFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stage                                                 string
		invalidID, validID                                    EventID
		invalidCorrelation, validCorrelation, invalidIdentity string
		evidence                                              map[string]any
	}{
		{"qa-schema", "qa-invalid-host-receipt", "qa-valid-host-receipt", "qa-correlation", "qa-correlation", "qa-host", map[string]any{"identity": "qa-receipt", "digest": "sha256:receipt"}},
		{"protocol-fixture", "protocol-host-schema-invalid", "protocol-host-schema-valid", "protocol-host-correlation", "receipt-correlation-exact", "protocol-host", map[string]any{"identity": "receipt-evidence-identity", "digest": "sha256:receipt-evidence"}},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			engine, dir := newTestEngine(t)
			collectCalls := 0

			view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatalf("view: %v", err)
			}
			for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
				if err := view.CompleteStep(step, engine.cfg.Definition); err != nil {
					t.Fatalf("complete %s: %v", step, err)
				}
			}

			confirmation := &IntakeConfirmationReceipt{
				Source: "fake-adapter", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
				RequirementSource: "requirement.md", RequirementRevision: "rev-1",
				Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
				SolutionRevision: "s1", SolutionDigest: "sha256:solution",
			}
			if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", confirmation, "", "", nil); err != nil {
				t.Fatalf("init: %v", err)
			}
			if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:testkit-intake"}, Observation{}); err != nil {
				t.Fatalf("intake: %v", err)
			}

			plan, _, err := engine.Plan(Observation{})
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, Observation{})
			if err != nil || len(issued) != 1 {
				t.Fatalf("issue: %+v err=%v", issued, err)
			}

			observedInput := Observation{}
			observedInput.Read = func() (decision.Observation, error) { collectCalls++; return decision.Observation{}, nil }
			baseline := engine.LoadMustSucceed(t)
			beforeBytes := stateBytesOf(t, dir)
			for _, invalid := range []struct {
				name      string
				operation authoring.OperationID
				params    any
				code      string
			}{
				{"unregistered operation", "op.not.registered", map[string]any{}, CodeOperationNotRegistered},
				{"undeclared parameter", opTransport, map[string]any{"command": "echo bypass"}, CodeOperationSchemaInvalid},
				{"wrong parameter type", opTransport, map[string]any{"stage": 42}, CodeOperationSchemaInvalid},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					beforeCalls := collectCalls
					if _, _, err := prepareHostIntentForReceipt(t, engine, invalid.operation, invalid.params, "", observedInput); err == nil {
						t.Fatal("invalid intent accepted")
					} else if code := rejectionCode(t, err); code != invalid.code {
						t.Fatalf("code=%s want=%s", code, invalid.code)
					}
					after := engine.LoadMustSucceed(t)
					if after.Revision != baseline.Revision || !reflect.DeepEqual(after.State, baseline.State) || !reflect.DeepEqual(stateBytesOf(t, dir), beforeBytes) || collectCalls != beforeCalls {
						t.Fatal("invalid intent changed state or collected external facts")
					}
				})
			}
			intent, revision, err := prepareHostIntentForReceipt(t, engine, opTransport, map[string]any{"stage": tc.stage, "generation": 2}, "", observedInput)
			if err != nil || revision != baseline.Revision+1 || intent.Operation != HostActionExecuteAdapterOperation || intent.Adapter == nil || intent.Resume != nil || intent.Terminate != nil || intent.PayloadDigest == "" {
				t.Fatalf("intent=%+v revision=%d err=%v", intent, revision, err)
			}
			afterIntent := engine.LoadMustSucceed(t)
			pending, ok := afterIntent.State.PendingHostActions[intent.ActionID]
			wantParams := map[string]any{"stage": tc.stage, "generation": float64(2)}
			if afterIntent.Revision != revision || !ok || pending.ActionID != intent.ActionID || pending.Operation != intent.Operation || pending.PayloadDigest != intent.PayloadDigest || pending.Resume != nil || pending.Terminate != nil || pending.Adapter == nil || pending.Adapter.Operation != intent.Adapter.Operation || !reflect.DeepEqual(pending.Adapter.Params, wantParams) {
				t.Fatalf("pending=%+v present=%v", pending, ok)
			}
			invalid, err := NewAdapterHostActionReceiptEvent(tc.invalidID, intent.ActionID, intent.Adapter.Operation, testProvider, tc.invalidCorrelation, intent.PayloadDigest, HostActionStatusExecuted, "", map[string]any{"identity": tc.invalidIdentity, "digest": "sha256:observed", "undeclared": true})
			if err != nil {
				t.Fatalf("invalid evidence constructor: %v", err)
			}
			beforeBytes = stateBytesOf(t, dir)
			beforeCalls := collectCalls
			if _, err := engine.Submit(invalid, observedInput); err == nil {
				t.Fatal("invalid evidence accepted")
			} else if code := rejectionCode(t, err); code != CodeOperationSchemaInvalid {
				t.Fatalf("evidence code=%s", code)
			}
			afterRejected := engine.LoadMustSucceed(t)
			if afterRejected.Revision != afterIntent.Revision || !reflect.DeepEqual(afterRejected.State, afterIntent.State) || !reflect.DeepEqual(stateBytesOf(t, dir), beforeBytes) || collectCalls != beforeCalls {
				t.Fatal("invalid evidence changed state or collected external facts")
			}
			valid, err := NewAdapterHostActionReceiptEvent(tc.validID, intent.ActionID, intent.Adapter.Operation, testProvider, tc.validCorrelation, intent.PayloadDigest, HostActionStatusExecuted, "", tc.evidence)
			if err != nil {
				t.Fatalf("valid evidence: %v", err)
			}
			acceptance, err := engine.Submit(valid, Observation{})
			if err != nil || acceptance.Status != "ACCEPTED" || acceptance.Revision != afterIntent.Revision+1 {
				t.Fatalf("acceptance=%+v err=%v", acceptance, err)
			}
			settled := engine.LoadMustSucceed(t)
			if settled.Revision != acceptance.Revision {
				t.Fatalf("settled revision=%d acceptance=%d", settled.Revision, acceptance.Revision)
			}
			if _, pending := settled.State.PendingHostActions[intent.ActionID]; pending {
				t.Fatal("executed intent remains pending")
			}
			receipt, ok := settled.State.HostActionReceipts[intent.ActionID]
			if !ok || len(settled.State.HostActionReceipts) != 1 {
				t.Fatalf("receipts=%+v present=%v", settled.State.HostActionReceipts, ok)
			}
			if receipt.ActionID != intent.ActionID || receipt.Operation != HostActionExecuteAdapterOperation || receipt.AdapterOperation != intent.Adapter.Operation || receipt.Step != pending.Step || receipt.Provider != testProvider || receipt.Correlation != tc.validCorrelation || receipt.PayloadDigest != intent.PayloadDigest || receipt.Status != HostActionStatusExecuted || receipt.FailureClass != "" || receipt.LifecycleEvidence != nil || receipt.AdapterEvidence == nil || !reflect.DeepEqual(receipt.AdapterEvidence.Values, tc.evidence) || receipt.Digest == "" {
				t.Fatalf("receipt=%+v", receipt)
			}
		})
	}
}
