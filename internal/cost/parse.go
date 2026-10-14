package cost

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/host"
)

// ParseTranscript parses exact token usage from a host transcript file,
// selecting the provider adapter by host name. Unsupported providers,
// unreadable files, and transcripts with no parseable usage return an error
// so the caller marks the dispatch UNAVAILABLE rather than fabricating a
// number. Unknown line shapes are skipped; recognized usage records must
// satisfy their provider's counter contract.
func ParseTranscript(provider, path string) (Usage, error) {
	return ParseTranscriptFiles(provider, path)

}

// ParseTranscriptFiles reads original streams once through the provider parser.
// One parser invocation retains its native response/turn deduplication across files.
func ParseTranscriptFiles(provider string, paths ...string) (Usage, error) {
	readers := []io.Reader{}
	files := []*os.File{}
	seen := map[string]bool{}
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return Usage{}, err
		}
		seen[path] = true
		files = append(files, file)
		if len(readers) > 0 {
			readers = append(readers, strings.NewReader("\n"))
		}
		readers = append(readers, file)
	}
	return parseTranscript(provider, io.MultiReader(readers...))
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
