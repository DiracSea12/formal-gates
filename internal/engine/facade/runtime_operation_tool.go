package facade

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
	return map[string]any{"type": "function", "name": "formal_gates_operation", "description": description, "inputSchema": schema}
}

// Native file/command grants are workflow policy, not a platform decision.
// Adapters only translate this access into their own tool names and options.
type nativeAccess uint8

const (
	nativeTextOnly nativeAccess = iota
	nativeOperationOnly
	nativeReadOnly
	nativeWorkspaceWrite
)

func nativeRoleAccess(step string) nativeAccess {
	switch step {
	case "product-review", "start-readiness":
		return nativeTextOnly
	case "blackbox-execution", "whitebox-execution":
		return nativeOperationOnly
	case "development-worker", "repair-worker", "whitebox-design":
		return nativeWorkspaceWrite
	default:
		return nativeReadOnly
	}
}

func nativeTextOnlyRole(step string) bool {
	return nativeRoleAccess(step) == nativeTextOnly
}
