package cost

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
)

// scanTranscriptLines opens the transcript file and invokes fn for every
// non-blank line. The callback reports whether the line carried usable
// usage; the shared structural failure is returned when no line did. A
// scanner error (oversized or truncated line) is returned as-is so partial
// reads never silently produce a partial total.
func scanTranscriptLines(path string, fn func(line []byte) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	parsed := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if fn(line) {
			parsed = true
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !parsed {
		return fmt.Errorf("transcript %s contains no parseable token usage", path)
	}
	return nil
}
