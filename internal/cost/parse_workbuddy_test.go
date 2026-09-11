package cost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

// These are SDK-schema fixtures, not captured native execution evidence.
const workBuddyMessage = `{"type":"assistant","uuid":"m1","session_id":"s1","message":{"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":5}}}` + "\n"

func TestWorkBuddySDKUsageAndOwnerResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.jsonl")
	initial := workBuddyMessage + workBuddyMessage + `{"type":"result","usage":{"input_tokens":12,"output_tokens":3}}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"workbuddy", "codebuddy", "cbc"} {
		u, err := ParseTranscript(provider, path)
		if err != nil || u != (Usage{InputCacheMissTokens: 12, OutputTokens: 3, InputCacheHitTokens: 5}) {
			t.Fatalf("%s: %+v %v", provider, u, err)
		}
	}
	baseline, err := CaptureBaseline("workbuddy", path)
	if err != nil {
		t.Fatal(err)
	}
	next := `{"type":"assistant","uuid":"m2","session_id":"s1","message":{"usage":{"input_tokens":7,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(initial+next), 0600); err != nil {
		t.Fatal(err)
	}
	delta, err := MeasureOwner("workbuddy", path, baseline)
	if err != nil || delta != (Usage{InputCacheMissTokens: 7, OutputTokens: 2}) {
		t.Fatalf("delta %+v: %v", delta, err)
	}
	d, err := host.Lookup("workbuddy")
	if err != nil || !d.Installable || d.CostProvider != host.WorkBuddy {
		t.Fatalf("WorkBuddy capability registration incomplete: %+v %v", d, err)
	}
}

func TestWorkBuddyUnavailableUsage(t *testing.T) {
	for _, input := range []string{
		`{"type":"result","usage":{"input_tokens":12,"output_tokens":3}}`,
		`{"type":"system","subtype":"task_notification","usage":{"total_tokens":15}}`,
		`{"type":"assistant","uuid":"x","session_id":"s1","message":{"usage":{"input_tokens":1}}}`,
		strings.Replace(workBuddyMessage, `"input_tokens":12`, `"input_tokens":-1`, 1),
		workBuddyMessage + strings.Replace(workBuddyMessage, `"output_tokens":3`, `"output_tokens":4`, 1),
		workBuddyMessage + strings.Replace(workBuddyMessage, `"session_id":"s1"`, `"session_id":"s2"`, 1),
	} {
		if u, err := parseTranscript("workbuddy", strings.NewReader(input)); err == nil || u != (Usage{}) {
			t.Fatalf("incomplete/conflicting input measured: %+v %v", u, err)
		}
	}
}
