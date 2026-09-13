package host

import (
	"encoding/json"
	"testing"
)

func TestWorkBuddyRecoveryUsesBoundSessionStoreReceipt(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"sessionId": "native-session", "present": true})
	if got := workBuddyRecoveryOutcome(RecoveryObservation{NativeMethod: "session/store", NativeResponse: raw, Identity: "native-session"}); got != "RESUMABLE" {
		t.Fatalf("valid native store receipt=%s", got)
	}
	if got := workBuddyRecoveryOutcome(RecoveryObservation{NativeMethod: "session/store", NativeResponse: raw, Identity: "other-session"}); got != "UNKNOWN" {
		t.Fatalf("mismatched native identity=%s", got)
	}
}
