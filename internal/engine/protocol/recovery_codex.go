package protocol

import "encoding/json"

func codexRecoveryOutcome(r DeveloperRecovery) string {
	if r.NativeMethod != "thread/resume" || r.Identity == "" || r.TransportError != "" {
		return "UNKNOWN"
	}
	var response struct {
		ID     int `json:"id"`
		Result *struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(r.NativeResponse, &response) != nil || response.ID != 3 {
		return "UNKNOWN"
	}
	if response.Error == nil && response.Result != nil && response.Result.Thread.ID == r.Identity {
		return "RESUMABLE"
	}
	if response.Error != nil && response.Error.Code == -32600 && response.Error.Message == "no rollout found for thread id "+r.Identity {
		return "UNRECOVERABLE"
	}
	return "UNKNOWN"
}
