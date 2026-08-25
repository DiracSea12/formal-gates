package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// reviewAction 是 wire 测试使用的标准 actionID。
const reviewAction = "act:review/review.worker"

// richState 构造一个各台账全字段非空的 State（wire round trip 输入）。
func richState(t *testing.T) *State {
	t.Helper()
	view, err := decision.NewState("2", runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	state := NewState(*view)
	// 单元测试没有 compiled definition 在手，完成集直接登记（CompleteStep
	// 的定义内校验由 decision 包自己的测试覆盖）。
	state.Completed = append(state.Completed, authoring.StepID("entry.parse"))
	key := runtime.TaskKey{Node: "review", Step: "review.worker"}
	state.Expected = []runtime.TaskKey{key}
	state.Attempts[key] = Attempt{
		IssuedAction: decision.IssuedAction{ActionID: "act:review/review.worker", Task: key, Step: "review.worker"},
		ID:           "att:review/review.worker:2",
		Bindings: AttemptBindings{
			Task: key, Snapshot: "sha256:snapshot", Responsibility: "fake-host",
		},
		Plan: PlanIdentity{
			PlanDigest: "sha256:plan", DefinitionDigest: "sha256:definition",
			StateDigest: "sha256:state", ObservationDigest: "sha256:observation",
		},
	}
	state.PendingActions["act:review/review.worker"] = PendingAction{
		ActionID: "act:review/review.worker", Task: key, Step: "review.worker", AttemptID: "att:review/review.worker:2",
	}
	state.Tasks[key] = runtime.TaskIssued
	state.PendingAsks["req-1"] = PendingAsk{
		RequestID: "req-1", Control: ControlReset,
		Options: []AskOption{{ID: "proceed", Label: "确认重置"}, {ID: "cancel", Label: "取消"}},
	}
	state.Decisions["req-0"] = RecordedDecision{
		RequestID: "req-0", Control: ControlAbort, Choice: "abort", EventID: "evt-0", Revision: 3,
	}
	state.Events["evt-0"] = EventRecord{Digest: "sha256:abc", Acceptance: Acceptance{
		EventID: "evt-0", Kind: "REQUEST_CONTROL", Status: "ACCEPTED", Revision: 3, RequestID: "req-0",
	}}
	// 批 1c 台账：provider 绑定、回执/暂存结果/结果、Operator 观察、
	// HostAction intent/回执、lifecycle buffer 与验证配对。
	state.RunProvider = "fake-host"
	state.SpawnReceipts[reviewAction] = SpawnReceipt{
		ActionID: reviewAction, Provider: "fake-host", Correlation: "agent-1",
		Status: SpawnStatusSpawned, Digest: "sha256:spawn",
	}
	state.StagedResults["act:early"] = WorkerResult{
		ActionID: "act:early", Provider: "fake-host", Outcome: OutcomePass,
		PayloadDigest: "sha256:out", Digest: "sha256:res",
	}
	state.Results[reviewAction] = WorkerResult{
		ActionID: reviewAction, Provider: "fake-host", Outcome: OutcomeRuntimeError,
		PayloadDigest: "sha256:err", Digest: "sha256:res2",
	}
	state.OperatorObservations = []OperatorObservation{{
		Subject: reviewAction,
		Facts:   []decision.Fact{{Source: decision.SourceReceipt, Key: "k", Value: "v"}},
		EventID: "evt-obs", Revision: 4,
	}}
	state.PendingHostActions["hact:op:5"] = HostActionIntent{
		ActionID: "hact:op:5", Operation: HostActionExecuteAdapterOperation,
		Adapter:       &AdapterHostAction{Operation: "op.fan.transport", Params: map[string]any{"target": "b"}},
		PayloadDigest: "sha256:params", Revision: 5,
	}
	state.HostActionReceipts["hact:op:4"] = HostActionReceipt{
		ActionID: "hact:op:4", Operation: HostActionExecuteAdapterOperation, AdapterOperation: "op.fan.transport", Provider: "fake-host",
		Correlation: "corr", PayloadDigest: "sha256:params", Status: HostActionStatusExecuted, Digest: "sha256:hr",
		AdapterEvidence: &AdapterEvidence{Values: map[string]any{}},
	}
	state.LifecycleEvents = []LifecycleEventRecord{
		{Provider: "fake-host", Correlation: "corr-1", Identity: "agent-1", Event: LifecycleStart, Digest: "sha256:ls"},
		{Provider: "fake-host", Correlation: "corr-1", Identity: "agent-1", Event: LifecycleStop, Digest: "sha256:lp"},
	}
	state.LifecycleVerified["agent-1"] = LifecycleVerification{Correlation: "corr-1", Identity: "agent-1", Provider: "fake-host", Revision: 6}
	return state
}

// TestStateJSONRoundTrip：全字段非空的状态经领域 JSON 往返不丢
// 信息（TaskKey 结构键、四张台账 map、决策视图全部复原）。
func TestStateJSONRoundTrip(t *testing.T) {
	state := richState(t)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(state, &decoded) {
		t.Fatalf("round trip mismatch:\n got  %#v\n want %#v", &decoded, state)
	}
}

// TestStateMarshalDeterministic：两次编码同字节；等价状态（构造路径不同）
// 同字节——map 遍历序不得泄漏进权威投影。
func TestStateMarshalDeterministic(t *testing.T) {
	first := richState(t)
	second := richState(t)
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("same logical state marshals differently:\n%q\n%q", a, b)
	}
	again, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(a) != string(again) {
		t.Fatalf("re-marshal of identical state differs")
	}
}

// TestStateJSONEmptyCollections：空台账编码为稳定空集合而非 null，空状态可往返。
func TestStateJSONEmptyCollections(t *testing.T) {
	view, err := decision.NewState("2", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	// canonicalJSON 是实际落盘形态（2 空格缩进）；空集编码为 [] 而非 null。
	data, err := canonicalJSON(NewState(*view))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, fragment := range []string{`"tasks": {}`, `"expected": []`, `"attempts": {}`, `"pendingActions": {}`, `"pendingAsks": {}`, `"decisions": {}`, `"events": {}`, `"spawnReceipts": {}`, `"spawnFailures": {}`, `"stagedResults": {}`, `"results": {}`, `"recoverableResults": {}`, `"operatorObservations": []`, `"pendingHostActions": {}`, `"hostActionReceipts": {}`, `"hostActionFailures": {}`, `"lifecycleEvents": []`, `"lifecycleVerified": {}`, `"runProvider": ""`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("empty collection not encoded as []: missing %q in %s", fragment, data)
		}
	}
	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Attempts == nil || decoded.Events == nil {
		t.Fatal("decoded maps must be non-nil")
	}
	if decoded.SpawnReceipts == nil || decoded.SpawnFailures == nil || decoded.StagedResults == nil || decoded.Results == nil || decoded.RecoverableResults == nil ||
		decoded.PendingHostActions == nil || decoded.HostActionReceipts == nil || decoded.HostActionFailures == nil || decoded.LifecycleVerified == nil {
		t.Fatal("decoded 1c maps must be non-nil")
	}
}

// TestTaskKeyTextCodec：规范键形态两段/三段可解析，段数与空段拒绝。
func TestTaskKeyTextCodec(t *testing.T) {
	var key runtime.TaskKey
	err := key.UnmarshalText([]byte("review/review.worker"))
	if err != nil || key.Node != "review" || key.Step != "review.worker" || key.Scope != "" {
		t.Fatalf("two-segment parse: %v %+v", err, key)
	}
	err = key.UnmarshalText([]byte("fan/fan.slice/case-1"))
	if err != nil || key.Scope != "case-1" {
		t.Fatalf("three-segment parse: %v %+v", err, key)
	}
	encoded, err := key.MarshalText()
	if err != nil || string(encoded) != "fan/fan.slice/case-1" {
		t.Fatalf("marshal text = %q err=%v", encoded, err)
	}
	for _, bad := range []string{"review", "a/b/c/d", "/step", "node/", "node/step/"} {
		if err := key.UnmarshalText([]byte(bad)); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", bad)
		}
	}
}

// TestStateDecodeRejectsInvalidTaskIdentity：领域 codec 拒绝无法寻址的
// TaskKey map key。
func TestStateDecodeRejectsInvalidTaskIdentity(t *testing.T) {
	invalid := `{
  "definitionVersion": "2",
  "phase": "INTAKE_REGISTERED",
  "completed": [],
  "tasks": {"not-a-task": "ISSUED"},
  "expected": [],
  "attempts": {},
  "pendingActions": {},
  "pendingAsks": {},
  "decisions": {},
  "events": {}
}`
	var state State
	if err := json.Unmarshal([]byte(invalid), &state); err == nil {
		t.Fatal("invalid task identity accepted")
	}
}
