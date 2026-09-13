package facade

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type dshSessionOptions struct {
	Model  string
	Effort string
}

func runDSHSession(in io.Reader, out io.Writer, job, workspace, identity, prompt string, attached func(string) error, options ...dshSessionOptions) (nativeProcessResult, error) {
	result := nativeProcessResult{Identity: identity}
	client := newACPClient(in, out)
	if err := client.initialize(); err != nil {
		return result, err
	}
	method := "session/new"
	params := map[string]any{"cwd": workspace, "mcpServers": []any{}}
	if identity != "" {
		method = "session/resume"
		params["sessionId"] = identity
	}
	_, raw, err := client.request(3, method, params)
	if err != nil {
		return result, err
	}
	if identity == "" {
		var session struct {
			ID string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &session) != nil || session.ID == "" {
			return result, fmt.Errorf("DSH_BINDING: native session ID missing")
		}
		result.Identity = session.ID
	}
	if attached == nil {
		return result, fmt.Errorf("DSH_BINDING: identity attachment required")
	}
	if err := attached(result.Identity); err != nil {
		return result, err
	}
	var option dshSessionOptions
	if len(options) > 0 {
		option = options[0]
	}
	if option.Model != "" {
		var configuration struct {
			Options []struct {
				ID      string `json:"id"`
				Current string `json:"currentValue"`
			} `json:"configOptions"`
		}
		if json.Unmarshal(raw, &configuration) != nil {
			return result, fmt.Errorf("DSH_CONFIG: native model configuration missing")
		}
		provider := ""
		for _, item := range configuration.Options {
			if item.ID == "model" {
				var selection []string
				if json.Unmarshal([]byte(item.Current), &selection) == nil && len(selection) == 2 {
					provider = selection[0]
				}
			}
		}
		if provider == "" {
			return result, fmt.Errorf("DSH_CONFIG: native model provider missing")
		}
		selection, _ := json.Marshal([]string{provider, option.Model})
		if _, _, err := client.request(10, "session/set_config_option", map[string]any{"sessionId": result.Identity, "configId": "model", "value": string(selection)}); err != nil {
			return result, err
		}
	}
	if option.Effort != "" {
		if _, _, err := client.request(11, "session/set_config_option", map[string]any{"sessionId": result.Identity, "configId": "reasoning_effort", "value": option.Effort}); err != nil {
			return result, err
		}
	}
	_, raw, err = client.request(4, "session/prompt", map[string]any{"sessionId": result.Identity, "prompt": []any{map[string]any{"type": "text", "text": prompt}}})
	if err != nil {
		return result, err
	}
	var completion struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &completion) != nil || completion.StopReason != "end_turn" {
		return result, fmt.Errorf("DSH_RESULT: prompt did not end normally (%s)", completion.StopReason)
	}
	if _, _, err := client.request(5, "session/close", map[string]any{"sessionId": result.Identity}); err != nil {
		return result, err
	}
	data, err := os.ReadFile(filepath.Join(job, "native-source.json"))
	if err != nil {
		return result, err
	}
	var source struct {
		ID   string `json:"sessionId"`
		Turn *int   `json:"turn"`
	}
	if json.Unmarshal(data, &source) != nil || source.ID != result.Identity || source.Turn == nil {
		return result, fmt.Errorf("DSH_BINDING: committed input source missing or mismatched")
	}
	events, err := os.Open(filepath.Join(job, "native-events.jsonl"))
	if err != nil {
		return result, err
	}
	defer events.Close()
	result.Answer, err = observeDSHResult(events, result.Identity, *source.Turn)
	return result, err
}
