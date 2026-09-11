package protocol

import (
	"encoding/json"
	"testing"
)

func TestNativeRecoveryEvidenceByProvider(t *testing.T) {
	for _, tc := range []struct{ provider, method, raw string }{
		{"workbuddy", "session/load", `{"jsonrpc":"2.0","id":3,"result":{}}`},
		{"deepseek-harness", "session/resume", `{"jsonrpc":"2.0","id":3,"result":{"configOptions":[]}}`},
		{"zcode", "session/resume", `{"id":"recovery","result":{"session":{"sessionId":"native"}}}`},
	} {
		r := DeveloperRecovery{Claim: ExecutionClaim{Provider: tc.provider}, Identity: "native", NativeMethod: tc.method, NativeResponse: json.RawMessage(tc.raw)}
		if got := r.Outcome(); got != "RESUMABLE" {
			t.Fatalf("%s: %s", tc.provider, got)
		}
		r.TransportError = "connection ended"
		if r.Outcome() != "UNKNOWN" {
			t.Fatal("transport uncertainty accepted")
		}
		r.TransportError = ""
		r.NativeMethod = "thread/resume"
		if r.Outcome() != "UNKNOWN" {
			t.Fatal("other host method accepted")
		}
	}
	r := DeveloperRecovery{Claim: ExecutionClaim{Provider: "zcode"}, Identity: "bound", NativeMethod: "session/resume", NativeResponse: json.RawMessage(`{"id":"recovery","result":{"session":{"sessionId":"other"}}}`)}
	if r.Outcome() != "UNKNOWN" {
		t.Fatal("other session accepted")
	}
}
