package cost

import (
	"fmt"
	"strings"
)

// ParseTranscript parses exact token usage from a host transcript file,
// selecting the provider adapter by host name. Unsupported providers,
// unreadable files, and transcripts with no parseable usage return an error
// so the caller marks the dispatch UNAVAILABLE rather than fabricating a
// number. Adapters never fail on unknown line shapes or missing fields;
// those lines are skipped.
func ParseTranscript(provider, path string) (Usage, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code", "claude", "claude code":
		return parseClaudeTranscript(path)
	case "codex":
		return parseCodexTranscript(path)
	default:
		return Usage{}, fmt.Errorf("unsupported transcript provider %q", provider)
	}
}
