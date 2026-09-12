package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// runWorkBuddyBridge composes the platform command, MCP config, process journal
// and observed identity callback. The public driver must claim first; this
// transport neither schedules nor registers the resulting action verdict.
func (workBuddyRuntimeAdapter) turn(q nativeTurn) (string, error) {
	ctx := context.Background()
	claim, resume, job, workspace, prompt, schema := q.Claim, q.Resume, q.Job, q.Workspace, q.Prompt, q.Schema
	var err error
	if claim.Provider != "workbuddy" || claim.Context == nil || claim.Context.Provider != "workbuddy" {
		return "", fmt.Errorf("INTENT_MISMATCH: WorkBuddy transport requires its bound runtime")
	}
	identity := claim.Identity
	if resume {
		if identity == "" {
			return "", fmt.Errorf("DEVELOPMENT_CONTINUITY: WorkBuddy resume requires observed identity")
		}
	} else {
		if identity != "" {
			return "", fmt.Errorf("DEVELOPMENT_CONTINUITY: attached WorkBuddy action cannot start a new session")
		}
		identity, err = requestedNativeID()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(job, 0700); err != nil {
		return "", err
	}
	// Preserve the requested identity before process creation; it is not SOURCE.
	requested, err := os.OpenFile(filepath.Join(job, "session-request.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	encodeErr := json.NewEncoder(requested).Encode(map[string]any{"actionId": claim.ActionID, "attemptId": claim.AttemptID, "identity": identity, "resume": resume})
	syncErr := requested.Sync()
	closeErr := requested.Close()
	if encodeErr != nil {
		return "", encodeErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	engineExecutable, err := os.Executable()
	if err != nil {
		return "", err
	}
	config, err := workBuddyMCPConfig(engineExecutable, q.Root, q.RunID, claim.ActionID, identity)
	if err != nil {
		return "", err
	}
	if q.NoTools || nativeTextOnlyRole(string(claim.Task.Step)) {
		config = map[string]any{"mcpServers": map[string]any{}}
	}
	configPath := filepath.Join(job, "mcp.json")
	if err := writeJSONAtomic(configPath, config); err != nil {
		return "", err
	}
	encodedSchema, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	cmd, err := workBuddySessionCommand(claim.Context.Executable, workspace, q.Settings.Model, q.Settings.ReasoningEffort, identity, resume, encodedSchema, configPath)
	if err != nil {
		return "", err
	}
	if !q.NoTools {
		workBuddyRoleTools(cmd, string(claim.Task.Step))
	}
	// Restore the captured native environment.
	cmd.Env = os.Environ()
	for name, value := range claim.Context.Environment {
		cmd.Env = replaceEnvironment(cmd.Env, name, value)
	}
	attachedIdentity := ""
	_, err = runWorkBuddyProcess(ctx, cmd, job, claim.ActionID, identity, prompt, func(observed string) error {
		if err := q.Attached(observed); err != nil {
			return err
		}
		attachedIdentity = observed
		return nil
	})
	if err == nil && attachedIdentity == "" {
		err = fmt.Errorf("UNKNOWN: WorkBuddy initialization identity was not observed")
	}
	return attachedIdentity, err
}
