package facade

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeOperationDispatchUsesEngineBinding(t *testing.T) {
	f, action := operationFixture(t)
	before, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	identity := before.Executions[action].Identity
	for _, tc := range []struct{ identity, id, arguments, want string }{
		{"", "call-1", `{}`, "unbound native tool"},
		{"other-session", "call-1", `{}`, "INTENT_MISMATCH"},
		{identity, "", `{}`, "unbound native tool"},
		{identity, "call-1", `{`, "unexpected end"},
		{identity, "missing-original", `{"kind":"qa.prepare","caseId":"BLACKBOX:C1","prompt":"","resume":true}`, "INTENT_MISMATCH"},
		{identity, "call-1", `{"kind":"qa.target","caseId":"BLACKBOX:C1","prompt":""}`, "PREREQUISITE_FAILED"},
	} {
		_, err := f.dispatchNativeOperation(action, tc.identity, tc.id, json.RawMessage(tc.arguments))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("input=%+v err=%v", tc, err)
		}
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Executions[action].Operations) != len(before.Executions[action].Operations) {
		t.Fatal("rejected transport input claimed an operation")
	}
}

func TestNativeOperationExplicitResumeRetainsReceipt(t *testing.T) {
	f, action := operationFixture(t)
	before, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	identity := before.Executions[action].Identity
	args := json.RawMessage(`{"requestId":"prepare-native","kind":"qa.prepare","caseId":"BLACKBOX:C1","prompt":""}`)
	first, err := f.RunNativeOperation(action, identity, args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.RunNativeOperation(action, identity, args); err == nil {
		t.Fatal("duplicate operation was accepted without explicit recovery")
	}
	resumed, err := f.RunNativeOperation(action, identity, json.RawMessage(`{"requestId":"prepare-native","kind":"qa.prepare","caseId":"BLACKBOX:C1","prompt":"","resume":true}`))
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	resumedJSON, _ := json.Marshal(resumed)
	if string(firstJSON) != string(resumedJSON) {
		t.Fatal("recovery changed original receipt")
	}
	if _, err := f.RunNativeOperation(action, identity, json.RawMessage(`{"requestId":"prepare-native","kind":"qa.prepare","caseId":"BLACKBOX:C1","prompt":"changed","resume":true}`)); err == nil {
		t.Fatal("recovery accepted changed original request")
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Executions[action].Operations) != len(before.Executions[action].Operations)+1 {
		t.Fatal("recovery created a second operation")
	}
}
