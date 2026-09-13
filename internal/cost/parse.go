package cost

import (
	"fmt"
	"io"
	"os"

	"formal-gates/internal/host"
)

// ParseTranscript parses exact token usage from a host transcript file,
// selecting the provider adapter by host name. Unsupported providers,
// unreadable files, and transcripts with no parseable usage return an error
// so the caller marks the dispatch UNAVAILABLE rather than fabricating a
// number. Unrelated native events may be skipped; missing or inconsistent
// mandatory usage evidence remains an error, not a measured zero.
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
	parser := descriptor.ReadUsage
	if parser == nil {
		return Usage{}, fmt.Errorf("unsupported transcript provider %q", provider)
	}
	return parser(reader)
}
