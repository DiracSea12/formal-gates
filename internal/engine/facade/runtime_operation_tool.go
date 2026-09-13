package facade

import "strings"

// Use the same bound Markdown sections for direct native tools and MCP.
func nativeOperationInstructions(run Run, step string) (string, error) {
	paths := []string{"actions/development-worker.md#工具说明"}
	// Older runs retain their captured tool text; do not require a section that
	// did not exist when they started or replace it with current package rules.
	if (step == "blackbox-execution" || step == "whitebox-execution" || step == "repair-worker") &&
		strings.Contains(run.Regular.ActionInstructions["actions/development-worker.md"], "\n# QA 操作说明\n") {
		paths = append(paths, "actions/development-worker.md#QA 操作说明")
	}
	return actionInstructions(run, paths...)
}

// Advertise only operations this role can actually request. Authoring tests run
// in the existing writable authoring workspace before any frozen QA wave exists;
// a design trial must never be presented as formal QA execution.
func nativeOperationTool(step, description string) map[string]any {
	kinds := []string{"model.run"}
	caseID := map[string]any{"type": "string", "enum": []string{""}}
	if step == "blackbox-execution" || step == "whitebox-execution" || step == "repair-worker" {
		kinds = append(kinds, "qa.prepare", "qa.target", "qa.collect", "qa.run", "qa.checkpoint")
		delete(caseID, "enum")
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "caseId", "prompt"}, "properties": map[string]any{"resume": map[string]any{"type": "boolean", "description": "Default false. Set true only to reconcile an interrupted operation with its original requestId and unchanged kind, caseId and prompt; never rerun under a new ID."}, "kind": map[string]any{"type": "string", "enum": kinds}, "caseId": caseID, "prompt": map[string]any{"type": "string"}}}
	if step == "blackbox-execution" || step == "whitebox-execution" || step == "repair-worker" {
		schema["properties"].(map[string]any)["command"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"executable", "args", "expectedExitCode"}, "properties": map[string]any{
			"executable": map[string]any{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "expectedExitCode": map[string]any{"type": "integer"},
		}, "description": "qa.target only: execute the approved procedure in the engine prepared workspace when the case has no command. Cannot replace an approved command or change its oracle."}
	}
	return map[string]any{"type": "function", "name": "formal_gates_operation", "description": description, "inputSchema": schema}
}

// Native file/command grants are workflow policy, not a platform decision.
// Adapters only translate this access into their own tool names and options.
type nativeAccess uint8

const (
	nativeOperationOnly nativeAccess = iota
	nativeReadOnly
	nativeWorkspaceWrite
)

func nativeRoleAccess(step string) nativeAccess {
	switch step {
	case "product-review", "start-readiness":
		return nativeReadOnly
	case "blackbox-execution", "whitebox-execution":
		return nativeOperationOnly
	case "development-worker", "repair-worker", "whitebox-design":
		return nativeWorkspaceWrite
	default:
		return nativeReadOnly
	}
}
