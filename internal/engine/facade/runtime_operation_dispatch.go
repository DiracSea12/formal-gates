package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// dispatchNativeOperation is the single adapter-to-engine operation boundary.
// The transport supplies only its observed identity, stable call ID and input;
// all claim fields and the current revision come from the engine's own record.
func (f *Facade) dispatchNativeOperation(actionID, identity, requestID string, arguments json.RawMessage) (any, error) {
	if identity == "" || requestID == "" {
		return nil, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: unbound native tool")
	}
	var input struct {
		Command *coverage.Command `json:"command"`
		Resume  bool              `json:"resume"`
		Kind    string            `json:"kind"`
		CaseID  string            `json:"caseId"`
		Prompt  string            `json:"prompt"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return nil, err
	}
	current, err := f.Show()
	if err != nil {
		return nil, err
	}
	execution, ok := current.Executions[actionID]
	if !ok || execution.Identity != identity {
		return nil, fmt.Errorf("INTENT_MISMATCH: native tool identity differs from execution")
	}
	if !nativeAllowsOperation(string(execution.Claim.Task.Step)) {
		return nil, fmt.Errorf("ACTION_INPUT_SCOPE: independent review cannot execute task operations")
	}
	if execution.Claim.Task.Step == "whitebox-design" && !protocol.WhiteboxCoverageReady(execution) {
		return nil, fmt.Errorf("ACTION_INPUT_SCOPE: coverage cannot execute task operations")
	}
	bound := execution.Claim
	bound.Identity = identity
	q := protocol.TaskOperationRequest{RunID: current.RunID, ExpectedRevision: current.Revision, Binding: bound, RequestID: requestID, Kind: input.Kind, CaseID: input.CaseID, Prompt: input.Prompt, Command: input.Command}
	if input.Resume {
		if original, ok := execution.Operations[protocol.TaskOperationID(q)]; ok {
			// The operation digest includes its admission revision. Recovery
			// must preserve that revision, not replace it with today's state.
			q.ExpectedRevision = original.Request.ExpectedRevision
		}
	}
	// Reconciliation is explicit at the existing operation boundary; this helper
	// never retries an interrupted operation with a different request ID.
	return f.RunTaskOperation(q, input.Resume)
}

// RunNativeOperation is the file/CLI transport for hosts that register a native
// tool directly. It shares the same engine binding checks as the MCP transport.
func (f *Facade) RunNativeOperation(actionID, identity string, arguments json.RawMessage) (any, error) {
	var request struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil {
		return nil, err
	}
	return f.dispatchNativeOperation(actionID, identity, request.RequestID, arguments)
}
