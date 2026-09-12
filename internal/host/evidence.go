package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Usage is native evidence normalized to the existing ledger categories.
type Usage struct {
	InputCacheHitTokens  int64
	InputCacheMissTokens int64
	OutputTokens         int64
}

// TaskTiming includes native tool waits, but not idle time between turns.
type TaskTiming struct {
	IncompleteTurns int    `json:"incompleteTurns,omitempty"`
	StartedAt       string `json:"startedAt"`
	CompletedAt     string `json:"completedAt"`
	WallTimeMillis  int64  `json:"wallTimeMillis"`
}

// RecoveryObservation contains only native evidence, not workflow state.
type RecoveryObservation struct {
	Identity       string
	NativeMethod   string
	NativeResponse json.RawMessage
	TransportError string
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

// acpRecoveryOutcome validates the common ACP response, not platform methods.
func acpRecoveryOutcome(raw json.RawMessage) string {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil || response.JSONRPC != "2.0" || response.ID != 3 || len(response.Error) > 0 && string(response.Error) != "null" {
		return "UNKNOWN"
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(response.Result, &result) != nil || result == nil {
		return "UNKNOWN"
	}
	return "RESUMABLE"
}
