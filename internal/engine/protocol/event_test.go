package protocol

import (
	"errors"
	"reflect"
	"testing"

	"formal-gates/internal/engine/runtime"
)

func rejectionCode(t *testing.T, err error) string {
	t.Helper()
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error %v is %T, want *RejectedError", err, err)
	}
	return rejected.Code
}

// TestEventConstructors：三类事件的合法构造通过校验，digest 稳定且随
// payload 变化。
func TestEventConstructors(t *testing.T) {
	request, err := NewRequestEvent("evt-req-1", ControlReset,
		AskOption{ID: "proceed", Label: "确认重置"}, AskOption{ID: "cancel", Label: "取消"})
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	decide, err := NewDecideEvent("evt-decide-1", "evt-req-1", "sha256:token", "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	key := runtime.TaskKey{Node: "review", Step: "review.worker"}
	progress, err := NewTaskEvent("evt-run-1", key, "att:review/review.worker:2", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}

	// digest 稳定：同事件恒同 digest；payload 变化必变 digest。
	d1, err := request.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	d2, err := request.Digest()
	if err != nil || d1 != d2 {
		t.Fatalf("digest unstable: %v %v %v", d1, d2, err)
	}
	other, err := NewRequestEvent("evt-req-1", ControlReset,
		AskOption{ID: "proceed", Label: "另一种选项"})
	if err != nil {
		t.Fatalf("other request: %v", err)
	}
	d3, err := other.Digest()
	if err != nil {
		t.Fatalf("digest other: %v", err)
	}
	if d1 == d3 {
		t.Fatal("different payload produced identical digest")
	}
	if _, err := decide.Digest(); err != nil {
		t.Fatalf("decide digest: %v", err)
	}
	if _, err := progress.Digest(); err != nil {
		t.Fatalf("progress digest: %v", err)
	}
}

// TestEventZeroValueAndUnknownKind：零值事件与未知 kind（含一切自由
// USER_* 直写形态）可区分拒绝——封闭 kind 集合里不存在用户自由写入。
func TestEventZeroValueAndUnknownKind(t *testing.T) {
	if err := (Event{}).Validate(); err == nil {
		t.Fatal("zero event accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownEventKind {
		t.Fatalf("zero event code = %q, want %q", code, CodeUnknownEventKind)
	}
	for _, kind := range []EventKind{"USER_RESET", "USER_FREE_WRITE", "REQUEST_USER_ANYTHING", "SPAWN", ""} {
		ev := Event{ID: "evt-x", Kind: kind}
		if err := ev.Validate(); err == nil {
			t.Fatalf("kind %q accepted", kind)
		} else if code := rejectionCode(t, err); code != CodeUnknownEventKind {
			t.Fatalf("kind %q code = %q, want %q", kind, code, CodeUnknownEventKind)
		}
	}
	if EventKind("USER_RESET").Valid() {
		t.Fatal("USER_* kind must not be valid")
	}
}

// TestEventUnionInvariant：kind 与 payload 不配对（缺 payload 或多挂
// payload）按 schema 拒绝。
func TestEventUnionInvariant(t *testing.T) {
	missing := Event{ID: "evt-x", Kind: KindRequestControl}
	if err := missing.Validate(); err == nil {
		t.Fatal("missing payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("missing payload code = %q, want %q", code, CodeEventSchemaInvalid)
	}
	cross := Event{
		ID:      "evt-x",
		Kind:    KindRequestControl,
		Request: &RequestPayload{Control: ControlReset, Options: []AskOption{{ID: "a", Label: "A"}}},
		Task:    &TaskPayload{Task: runtime.TaskKey{Node: "n", Step: "s"}, Attempt: "att", Status: runtime.TaskRunning},
	}
	if err := cross.Validate(); err == nil {
		t.Fatal("cross-kind payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("cross payload code = %q, want %q", code, CodeEventSchemaInvalid)
	}
	if _, err := cross.CanonicalBytes(); err == nil {
		t.Fatal("canonical bytes of invalid event accepted")
	}
}

// TestRequestEventSchemaNegatives：REQUEST payload 的 schema 负向全集。
func TestRequestEventSchemaNegatives(t *testing.T) {
	options := []AskOption{{ID: "proceed", Label: "确认"}, {ID: "cancel", Label: "取消"}}
	cases := []struct {
		name    string
		id      EventID
		control ControlKind
		options []AskOption
	}{
		{"empty id", "", ControlReset, options},
		{"unknown control", "e", ControlKind("REQUIREMENT_CHANGE"), options},
		{"no options", "e", ControlReset, nil},
		{"duplicate option ids", "e", ControlReset, []AskOption{{ID: "a", Label: "1"}, {ID: "a", Label: "2"}}},
		{"empty option id", "e", ControlReset, []AskOption{{ID: "", Label: "1"}}},
		{"empty option label", "e", ControlReset, []AskOption{{ID: "a", Label: ""}}},
	}
	for _, tc := range cases {
		_, err := NewRequestEvent(tc.id, tc.control, tc.options...)
		if err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
		if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q, want %q", tc.name, code, CodeEventSchemaInvalid)
		}
	}
}

// TestDecideEventSchemaNegatives：DECIDE payload 的 schema 负向全集。
func TestDecideEventSchemaNegatives(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		request string
		token   string
		choice  string
	}{
		{"empty id", "", "r", "t", "c"},
		{"empty request", "e", "", "t", "c"},
		{"empty token", "e", "r", "", "c"},
		{"empty choice", "e", "r", "t", ""},
	}
	for _, tc := range cases {
		_, err := NewDecideEvent(EventID(tc.id), RequestID(tc.request), tc.token, AskOptionID(tc.choice))
		if err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
		if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q, want %q", tc.name, code, CodeEventSchemaInvalid)
		}
	}
}

// TestTaskEventSchemaNegatives：TASK_PROGRESS payload 的 schema 负向
// 全集（非法键、空 attempt、不可报告状态）。
func TestTaskEventSchemaNegatives(t *testing.T) {
	key := runtime.TaskKey{Node: "review", Step: "review.worker"}
	cases := []struct {
		name   string
		task   runtime.TaskKey
		status runtime.TaskStatus
	}{
		{"empty node", runtime.TaskKey{Step: "s"}, runtime.TaskRunning},
		{"empty step", runtime.TaskKey{Node: "n"}, runtime.TaskRunning},
		{"queued not reportable", key, runtime.TaskQueued},
		{"issued not reportable", key, runtime.TaskIssued},
		{"bogus status", key, runtime.TaskStatus("BOGUS")},
		{"empty status", key, runtime.TaskStatus("")},
	}
	for _, tc := range cases {
		_, err := NewTaskEvent("evt-x", tc.task, "att:1", tc.status)
		if err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
		if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q, want %q", tc.name, code, CodeEventSchemaInvalid)
		}
	}
	if _, err := NewTaskEvent("evt-x", key, "", runtime.TaskRunning); err == nil {
		t.Fatal("empty attempt accepted")
	}
}

// TestFreshnessTokenDeterministic：token 是 (revision, requestID) 的确定
// 函数——同输入同值、任一输入变化即变化。
func TestFreshnessTokenDeterministic(t *testing.T) {
	a := freshnessToken(2, "req-1")
	b := freshnessToken(2, "req-1")
	if a != b || a == "" {
		t.Fatalf("token unstable: %q vs %q", a, b)
	}
	if freshnessToken(3, "req-1") == a {
		t.Fatal("revision change must change token")
	}
	if freshnessToken(2, "req-2") == a {
		t.Fatal("request change must change token")
	}
}

// TestControlKindClosedSet：控制类型封闭集合当前恰为 RESET/ABORT（业务
// 控制随阶段 4 扩充枚举，此处钉死现状防漂移）。
func TestControlKindClosedSet(t *testing.T) {
	valid := []ControlKind{ControlReset, ControlAbort}
	invalid := []ControlKind{"", "RESET ", "REQUIREMENT_CHANGE", "abort"}
	for _, k := range valid {
		if !k.Valid() {
			t.Fatalf("control %q should be valid", k)
		}
	}
	for _, k := range invalid {
		if k.Valid() {
			t.Fatalf("control %q should be invalid", k)
		}
	}
	if !reflect.DeepEqual(valid, []ControlKind{ControlReset, ControlAbort}) {
		t.Fatal("closed set drifted")
	}
}
