package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func checkTranscriptIdentity(path, identity string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("COST_PENDING: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		var v struct {
			Type    string `json:"type"`
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &v) == nil && v.Type == "session_meta" && v.Payload.ID == identity {
			return nil
		}
	}
	return fmt.Errorf("INTENT_MISMATCH: transcript session_meta does not identify %s", identity)
}
