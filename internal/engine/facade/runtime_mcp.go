package facade

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// serveNativeOperationMCP translates MCP stdio to the existing task operation
// boundary. The caller owns process lifetime and binds dispatch to one action.
// Native transport request IDs are connection-local; the explicit requestId
// argument is the stable engine operation key across reconnects.
func serveNativeOperationMCP(in io.Reader, out io.Writer, step, description string, dispatch func(string, json.RawMessage) (any, error)) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scan.Bytes(), &request); err != nil {
			return fmt.Errorf("MCP_STREAM: %w", err)
		}
		if request.JSONRPC != "2.0" {
			return fmt.Errorf("MCP_STREAM: invalid protocol version")
		}
		if len(request.ID) == 0 { // Notifications cannot execute operations.
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		rpcError := func(code int, message string) { response["error"] = map[string]any{"code": code, "message": message} }
		switch request.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				rpcError(-32602, "invalid initialize parameters")
				break
			}
			// This server implements the stdio tools subset common to these revisions.
			version := params.ProtocolVersion
			switch version {
			case "2024-11-05", "2025-03-26", "2025-06-18":
			default:
				version = "2024-11-05"
			}
			response["result"] = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "formal-gates", "version": "1"}}
		case "ping":
			response["result"] = map[string]any{}
		case "tools/list":
			tool := nativeOperationTool(step, description)
			delete(tool, "type")
			schema := tool["inputSchema"].(map[string]any)
			schema["required"] = append(schema["required"].([]string), "requestId")
			schema["properties"].(map[string]any)["requestId"] = map[string]any{"type": "string", "minLength": 1, "description": "Stable operation ID; preserve across recovery."}
			response["result"] = map[string]any{"tools": []any{tool}}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Name != "formal_gates_operation" {
				rpcError(-32602, "unknown tool or invalid parameters")
				break
			}
			var args struct {
				RequestID string `json:"requestId"`
			}
			if json.Unmarshal(params.Arguments, &args) != nil || args.RequestID == "" {
				rpcError(-32602, "stable requestId is required")
				break
			}
			value, err := dispatch(args.RequestID, params.Arguments)
			if err != nil {
				value = map[string]string{"error": err.Error()}
			}
			data, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				return marshalErr
			}
			response["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(data)}}, "isError": err != nil}
		default:
			rpcError(-32601, "method not found")
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
	return scan.Err()
}

// ServeNativeOperationMCP exposes only an already claimed action's task
// operations. It cannot claim, schedule or replace a worker. Platform launchers
// may initialize the server with a requested session ID before native attach;
// dispatch still requires that identity to have been observed by the engine.
func (f *Facade) ServeNativeOperationMCP(actionID, identity string, in io.Reader, out io.Writer) error {
	run, err := f.Show()
	if err != nil {
		return err
	}
	execution, ok := run.Executions[actionID]
	if !ok || !execution.Claimed || execution.Finished || identity == "" || (execution.Identity != "" && execution.Identity != identity) {
		return fmt.Errorf("INTENT_MISMATCH: MCP requires a claimed live action and a matching native identity")
	}
	description, err := actionInstructions(run, "actions/development-worker.md#工具说明")
	if err != nil {
		return err
	}
	return serveNativeOperationMCP(in, out, string(execution.Claim.Task.Step), description, func(requestID string, arguments json.RawMessage) (any, error) {
		return f.dispatchNativeOperation(actionID, identity, requestID, arguments)
	})
}
