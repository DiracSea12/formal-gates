package facade

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"time"
)

func probeACPRecovery(command *exec.Cmd, job, actionID, workspace, identity, method string) (json.RawMessage, error) {
	// This is a bounded native control query, not a model turn or task budget.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var response json.RawMessage
	_, err := runNativeStreamProcess(ctx, command, job, actionID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
		client := newACPClient(in, out)
		if err := client.initialize(); err != nil {
			return nativeProcessResult{}, err
		}
		raw, _, err := client.request(3, method, map[string]any{"sessionId": identity, "cwd": workspace, "mcpServers": []any{}})
		response = raw
		return nativeProcessResult{Identity: identity, Answer: json.RawMessage(`{}`)}, err
	})
	return response, err
}
