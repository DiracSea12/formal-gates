package validate

import (
	"path/filepath"
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

func TestCodexHookCanaryUsesUniqueCaseIDs(t *testing.T) {
	worktree := t.TempDir()
	missingCodex := filepath.Join(worktree, "missing-codex")
	options := CodexHookCanaryOptions{
		Worktree:     worktree,
		CodexCommand: missingCodex,
		KeepTemp:     true,
	}

	first, _ := CodexHookCanary(options)
	second, _ := CodexHookCanary(options)
	if first.Case == "" || second.Case == "" || first.Case == second.Case {
		t.Fatalf("canary case IDs must be unique: first=%q second=%q", first.Case, second.Case)
	}
	if first.ArtifactDir == second.ArtifactDir || first.Summary == second.Summary {
		t.Fatalf("canary outputs must be isolated: first=%#v second=%#v", first, second)
	}
}
