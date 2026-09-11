package protocol

import "encoding/json"

func zcodeRecoveryOutcome(r DeveloperRecovery) string {
	if r.NativeMethod != "session/resume" {
		return "UNKNOWN"
	}
	var response struct {
		ID     string `json:"id"`
		Result struct {
			Session struct {
				ID string `json:"sessionId"`
			} `json:"session"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(r.NativeResponse, &response) != nil || response.ID != "recovery" || len(response.Error) > 0 && string(response.Error) != "null" || response.Result.Session.ID != r.Identity {
		return "UNKNOWN"
	}
	return "RESUMABLE"
}
