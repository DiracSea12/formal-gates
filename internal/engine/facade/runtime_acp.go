package facade

import (
	"encoding/json"
	"fmt"
	"io"
)

// acpClient only exchanges native ACP messages. Callers own the process, native
// session and engine binding; it makes no workflow or permission decisions.
type acpClient struct {
	decoder *json.Decoder
	encoder *json.Encoder
}

func newACPClient(in io.Reader, out io.Writer) *acpClient {
	return &acpClient{json.NewDecoder(in), json.NewEncoder(out)}
}
func (c *acpClient) request(id int, method string, params any) (json.RawMessage, json.RawMessage, error) {
	if err := c.encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, nil, err
	}
	for {
		var raw json.RawMessage
		if err := c.decoder.Decode(&raw); err != nil {
			return nil, nil, err
		}
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int            `json:"id"`
			Method  string          `json:"method"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			return raw, nil, err
		}
		if msg.JSONRPC != "2.0" {
			return raw, nil, fmt.Errorf("ACP_PROTOCOL: invalid envelope")
		}
		if msg.Method != "" {
			if msg.ID != nil {
				return raw, nil, fmt.Errorf("ACP_INTERACTION: unexpected native request %s", msg.Method)
			}
			continue
		}
		if msg.ID == nil || *msg.ID != id {
			return raw, nil, fmt.Errorf("ACP_PROTOCOL: response ID differs")
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return raw, nil, fmt.Errorf("ACP_NATIVE: %s", msg.Error)
		}
		if len(msg.Result) == 0 {
			return raw, nil, fmt.Errorf("ACP_PROTOCOL: missing result")
		}
		return raw, msg.Result, nil
	}
}
func (c *acpClient) initialize() error {
	_, raw, err := c.request(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]any{"name": "formal-gates", "version": "1"}})
	if err != nil {
		return err
	}
	var result struct {
		Version int `json:"protocolVersion"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Version != 1 {
		return fmt.Errorf("ACP_PROTOCOL: unsupported protocol version")
	}
	return nil
}
