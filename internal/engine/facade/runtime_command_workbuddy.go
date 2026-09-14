package facade

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// workBuddyCommand builds the platform invocation from adapter-owned inputs.
// Prompt delivery is through stdin; no shell or native delegation is introduced.
// The caller must claim the engine action before starting this command and use
// observeWorkBuddyResult plus the existing result writer after process exit.
func workBuddyCommand(executable, workspace, model, effort, resumeIdentity string, schema json.RawMessage, mcpConfigPath string) (*exec.Cmd, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(workspace) || !filepath.IsAbs(mcpConfigPath) {
		return nil, fmt.Errorf("WORKBUDDY_CONFIG: executable, workspace and MCP config must be absolute paths")
	}
	switch effort {
	case "", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, fmt.Errorf("WORKBUDDY_CONFIG: unsupported effort %q", effort)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(schema, &object); err != nil || object == nil {
		return nil, fmt.Errorf("WORKBUDDY_CONFIG: result schema must be a JSON object")
	}
	// WorkBuddy implements --json-schema through its StructuredOutput tool.
	// Declare the result-only protocol explicitly even when a role has no
	// workspace or engine-operation tools; do not rely on implicit injection.
	args := []string{"--print", "--verbose", "--output-format", "stream-json", "--input-format", "text",
		"--json-schema", string(schema), "--tools", "StructuredOutput",
		"--strict-mcp-config", "--mcp-config", mcpConfigPath,
		"--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", "bypassPermissions"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if resumeIdentity != "" {
		args = append(args, "--resume", resumeIdentity)
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = workspace
	return cmd, nil
}

// workBuddySessionCommand distinguishes a requested fresh identity from resume.
// The requested identity is not source evidence until system/init confirms it.
func workBuddySessionCommand(executable, workspace, model, effort, identity string, resume bool, schema json.RawMessage, mcpConfigPath string) (*exec.Cmd, error) {
	if identity == "" {
		return nil, fmt.Errorf("WORKBUDDY_CONFIG: session identity is required")
	}
	resumeIdentity := ""
	if resume {
		resumeIdentity = identity
	}
	cmd, err := workBuddyCommand(executable, workspace, model, effort, resumeIdentity, schema, mcpConfigPath)
	if err != nil {
		return nil, err
	}
	if !resume {
		cmd.Args = append(cmd.Args, "--session-id", identity)
	}
	return cmd, nil
}

// workBuddyRoleTools applies the same authoring versus frozen execution boundary
// as the existing native adapter. Preserve default tools and deny role-prohibited
// operations; native permission checks still govern shell commands.
func workBuddyRoleTools(cmd *exec.Cmd, step string, coverage ...bool) {
	readOnlyCoverage := len(coverage) > 0 && coverage[0]
	denied := []string{"Task", "Agent", "Workflow", "EnterWorktree"}
	if readOnlyCoverage || nativeRoleAccess(step) != nativeWorkspaceWrite {
		denied = append(denied, "Write", "Edit", "NotebookEdit", "EnterPlanMode", "ExitPlanMode")
	}
	if !readOnlyCoverage && nativeRoleAccess(step) == nativeOperationOnly {
		denied = append(denied, "Bash")
	}
	if readOnlyCoverage || !nativeAllowsOperation(step) {
		denied = append(denied, "mcp__formal-gates__formal_gates_operation")
	}
	if readOnlyCoverage || nativeRoleAccess(step) == nativeReadOnly {
		// Do not pre-approve Bash: dontAsk must check each command natively.
		for i := 1; i+1 < len(cmd.Args); i++ {
			if cmd.Args[i] == "--permission-mode" {
				cmd.Args[i+1] = "dontAsk"
				break
			}
		}
		allowed := "StructuredOutput,Read,Glob,Grep"
		if !readOnlyCoverage && nativeAllowsOperation(step) {
			allowed += ",mcp__formal-gates__formal_gates_operation"
		}
		cmd.Args = append(cmd.Args, "--allowedTools", allowed)
	}
	cmd.Args = append(cmd.Args, "--disallowedTools", strings.Join(denied, ","))
	for i := 1; i+1 < len(cmd.Args); i++ {
		if cmd.Args[i] == "--tools" {
			cmd.Args[i+1] = "default"
			break
		}
	}
}
