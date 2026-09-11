package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"os"
	"time"
)

type nativeSourceAdapter struct {
	identity func(string, string) error
	timing   func(string, time.Time) (cost.TaskTiming, error)
}

var nativeSources = map[string]nativeSourceAdapter{
	"codex":            {identity: checkTranscriptIdentity, timing: cost.CodexTaskTimingAfter},
	"workbuddy":        {identity: workBuddyTranscriptIdentity},
	"zcode":            {identity: zcodeTranscriptIdentity},
	"deepseek-harness": {identity: dshTranscriptIdentity},
}

func checkNativeTranscriptIdentity(provider, path, identity string) error {
	a, ok := nativeSources[provider]
	if !ok {
		return fmt.Errorf("COST_PENDING: no native source adapter for %s", provider)
	}
	return a.identity(path, identity)
}
func nativeTranscriptTiming(provider, path string, after time.Time) (cost.TaskTiming, error) {
	a := nativeSources[provider]
	if a.timing == nil {
		return cost.TaskTiming{}, fmt.Errorf("native transcript timing unavailable for %s", provider)
	}
	return a.timing(path, after)
}
func checkStreamTranscriptIdentity(path, identity string, extract func(json.RawMessage) string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("COST_PENDING: %w", err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	seen := false
	for scan.Scan() {
		id := extract(scan.Bytes())
		if id == "" {
			continue
		}
		if identity == "" || id != identity {
			return fmt.Errorf("INTENT_MISMATCH: native transcript identifies another session")
		}
		seen = true
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if !seen {
		return fmt.Errorf("INTENT_MISMATCH: native transcript identity unavailable")
	}
	return nil
}
