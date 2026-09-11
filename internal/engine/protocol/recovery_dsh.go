package protocol

import "encoding/json"

func dshRecoveryOutcome(r DeveloperRecovery) string {
	if r.NativeMethod != "session/resume" {
		return "UNKNOWN"
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(r.NativeResponse, &response) != nil || response.JSONRPC != "2.0" || response.ID != 3 || len(response.Error) > 0 && string(response.Error) != "null" {
		return "UNKNOWN"
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(response.Result, &result) != nil || result == nil {
		return "UNKNOWN"
	}
	return "RESUMABLE"
}
