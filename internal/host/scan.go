package host

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// scanTranscriptLines reads the transcript and invokes fn for every
// non-blank line. The callback reports whether the line carried usable
// usage; the shared structural failure is returned when no line did. A
// scanner error (oversized or truncated line) is returned as-is so partial
// reads never silently produce a partial total.
func scanTranscriptLines(reader io.Reader, fn func(line []byte) bool) error {
	scanner := bufio.NewScanner(reader)
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
		return fmt.Errorf("transcript contains no parseable token usage")
	}
	return nil
}
