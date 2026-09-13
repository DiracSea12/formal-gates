package facade

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// zcodeCommand starts the platform's own app-server. Its NDJSON protocol has
// id/method/params envelopes, not ACP or JSON-RPC 2.0 framing.
func zcodeCommand(executable, script, workspace string) (*exec.Cmd, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(script) || !filepath.IsAbs(workspace) {
		return nil, fmt.Errorf("ZCODE_CONFIG: executable, script and workspace must be absolute paths")
	}
	// Current ZCode parses --cwd as a global option and app-server as the
	// positional command. Native user configuration is discovered from HOME;
	// do not pass help-only/unsupported settings flags.
	args := []string{script, "--cwd", workspace, "app-server"}
	cmd := exec.Command(executable, args...)
	cmd.Dir = workspace
	return cmd, nil
}

type zcodeRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// Native creation owns the session ID. Supplying sessionId for ordinary create
// is rejected by ZCode, even though its common schema marks that field optional.
func zcodeSessionRequest(requestID, workspace, identity, mode string, tools []string) (zcodeRequest, error) {
	if requestID == "" || !filepath.IsAbs(workspace) || tools == nil {
		return zcodeRequest{}, fmt.Errorf("ZCODE_BINDING: request identity, absolute workspace and explicit tool policy required")
	}
	params := map[string]any{
		"workspace":     map[string]any{"workspacePath": workspace, "workspaceKey": workspace},
		"toolAllowlist": append([]string{}, tools...),
	}
	method := "session/create"
	if identity != "" {
		method = "session/resume"
		params["sessionId"] = identity
	} else {
		params["persistence"] = "immediate"
		params["mode"] = mode
		params["titleGenerationEnabled"] = false
	}
	return zcodeRequest{ID: requestID, Method: method, Params: params}, nil
}
