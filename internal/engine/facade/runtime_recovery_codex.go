package facade

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"io"
	"os/exec"
	"time"
)

// codexThreadRequest loads only the specified native thread, without starting a
// model turn. The native missing-rollout response is distinct from a timeout or
// transport error; the engine decides whether that evidence permits replacement.
func codexThreadRequest(executable, method, identity string, binding *protocol.HostRuntimeContext, workspace string) (raw json.RawMessage, resultErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "app-server", "--stdio", "--disable", "enable_mcp_apps", "--disable", "multi_agent", "--disable", "multi_agent_v2")

	if err := runtimeCommandContext(cmd, binding); err != nil {
		return nil, err
	}
	cmd.Dir = workspace
	isolateCodexProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, finishCodexProcess(cmd, input, func() error {
			_, err := io.Copy(io.Discard, output)
			return err
		}))
	}()
	enc := json.NewEncoder(input)
	scan := bufio.NewScanner(output)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	request := func(id int, method string, params any) (json.RawMessage, error) {
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for scan.Scan() {
			var response struct {
				ID int `json:"id"`
			}
			if json.Unmarshal(scan.Bytes(), &response) == nil && response.ID == id {
				return append(json.RawMessage(nil), scan.Bytes()...), nil
			}
		}
		return nil, fmt.Errorf("native %s response unavailable: scan=%v context=%v", method, scan.Err(), ctx.Err())
	}
	initialized, err := request(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "formal-gates", "version": "4"}})
	if err != nil {
		return nil, err
	}
	var initResponse struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(initialized, &initResponse) != nil || initResponse.Error != nil {
		return nil, fmt.Errorf("native initialization failed: %s", initialized)
	}
	configResponse, err := request(2, "config/read", map[string]any{"includeLayers": false})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(configResponse, &envelope) != nil {
		return nil, fmt.Errorf("native configuration response invalid")
	}
	config, err := (codexRuntimeAdapter{}).toolConfig(envelope.Result)
	if err != nil {
		return nil, err
	}
	return request(3, method, map[string]any{"threadId": identity, "config": config})
}

// EOF lets app-server release its thread writer. Reap only after draining its
// output; killing immediately after a turn or resume response skips shutdown.
// The deadline bounds an unresponsive shutdown, not normal turn handoff.
func finishCodexProcess(cmd *exec.Cmd, input io.Closer, drain func() error) error {
	closeErr := input.Close()
	done := make(chan error, 1)
	go func() {
		drainErr := drain()
		done <- errors.Join(drainErr, cmd.Wait())
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		return errors.Join(closeErr, err)
	case <-timer.C:
		stopErr := stopCodexProcess(cmd.Process.Pid)
		return errors.Join(closeErr, fmt.Errorf("BRIDGE_UNAVAILABLE: native shutdown timed out"), stopErr, <-done)
	}
}
