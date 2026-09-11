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
	args := []string{"--print", "--verbose", "--output-format", "stream-json", "--input-format", "text",
		"--json-schema", string(schema), "--tools", "",
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
// as the existing native adapter. The allow list never exposes native delegation.
func workBuddyRoleTools(cmd *exec.Cmd, step string) {
	tools := ""
	switch step {
	case "development-worker", "repair-worker", "whitebox-design":
		tools = "Bash,Edit,Read"
	case "product-review", "start-readiness":
	default:
		if !strings.HasSuffix(step, "-execution") {
			tools = "Read"
		}
	}
	for i := 1; i+1 < len(cmd.Args); i++ {
		if cmd.Args[i] == "--tools" {
			cmd.Args[i+1] = tools
			break
		}
	}
}
