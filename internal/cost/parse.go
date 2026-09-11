package cost

import (
	"fmt"
	"io"
	"os"

	"formal-gates/internal/host"
)

type transcriptParser func(io.Reader) (Usage, error)

// transcriptParsers is the cost capability registry. Providers that do not
// expose a supported transcript format simply omit an entry and remain
// UNAVAILABLE instead of fabricating usage.
var transcriptParsers = map[string]transcriptParser{
	host.WorkBuddy: parseWorkBuddyTranscript,
	host.DeepSeek:  parseDSHTranscript,
	host.ZCode:     parseZCodeTranscript,
	host.Claude:    parseClaudeTranscript,
	host.Codex:     parseCodexTranscript,
}

// ParseTranscript parses exact token usage from a host transcript file,
// selecting the provider adapter by host name. Unsupported providers,
// unreadable files, and transcripts with no parseable usage return an error
// so the caller marks the dispatch UNAVAILABLE rather than fabricating a
// number. Adapters never fail on unknown line shapes or missing fields;
// those lines are skipped.
func ParseTranscript(provider, path string) (Usage, error) {
	file, err := os.Open(path)
	if err != nil {
		return Usage{}, err
	}
	defer file.Close()
	return parseTranscript(provider, file)
}

// The same adapters parse dispatch files and the fixed bytes of owner snapshots.
func parseTranscript(provider string, reader io.Reader) (Usage, error) {
	descriptor, err := host.Lookup(provider)
	if err != nil {
		return Usage{}, fmt.Errorf("unsupported transcript provider %q", provider)
	}
	parser, ok := transcriptParsers[descriptor.CostProvider]
	if !ok {
		return Usage{}, fmt.Errorf("unsupported transcript provider %q", provider)
	}
	return parser(reader)
}
