package validate

import (
	"strings"
	"testing"
)

func TestCodexHookCanaryFailureReason(t *testing.T) {
	timedOut := CodexHookCanarySummary{TimedOut: true, TimeoutSeconds: 7}
	if got := codexHookCanaryFailureReason(timedOut, false); !strings.Contains(got, "7 seconds") {
		t.Fatalf("expected timeout reason, got %q", got)
	}

	noPayload := CodexHookCanarySummary{}
	if got := codexHookCanaryFailureReason(noPayload, false); !strings.Contains(got, "no PreToolUse") {
		t.Fatalf("expected payload reason, got %q", got)
	}

	marker := CodexHookCanarySummary{PreToolUsePayloadCount: 1, MarkerExists: true}
	if got := codexHookCanaryFailureReason(marker, true); !strings.Contains(got, "marker file") {
		t.Fatalf("expected marker reason, got %q", got)
	}

	noDeny := CodexHookCanarySummary{PreToolUsePayloadCount: 1}
	if got := codexHookCanaryFailureReason(noDeny, false); !strings.Contains(got, "deny decision") {
		t.Fatalf("expected deny reason, got %q", got)
	}
}
