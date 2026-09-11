package facade

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"formal-gates/internal/engine/protocol"
)

// runWorkBuddyBridge composes the platform command, MCP config, process journal
// and observed identity callback. The public driver must claim first; this
// transport neither schedules nor registers the resulting action verdict.
func (f *Facade) runWorkBuddyBridge(ctx context.Context, run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, coverageOnly ...bool) (string, error) {
	if claim.Provider != "workbuddy" || claim.Context == nil || claim.Context.Provider != "workbuddy" {
		return "", fmt.Errorf("INTENT_MISMATCH: WorkBuddy transport requires its bound runtime")
	}
	current, err := f.Show()
	if err != nil {
		return "", err
	}
	execution, ok := current.Executions[claim.ActionID]
	if !ok || !execution.Claimed || execution.Finished || execution.Claim.AttemptID != claim.AttemptID || execution.Claim.Provider != claim.Provider || !reflect.DeepEqual(execution.Claim.Context, claim.Context) || current.RunID != run.RunID || run.Regular == nil {
		return "", fmt.Errorf("INTENT_MISMATCH: WorkBuddy transport requires a claimed live attempt")
	}
	identity := claim.Identity
	if resume {
		if identity == "" || execution.Identity != identity {
			return "", fmt.Errorf("DEVELOPMENT_CONTINUITY: WorkBuddy resume requires observed identity")
		}
	} else {
		if identity != "" || execution.Identity != "" {
			return "", fmt.Errorf("DEVELOPMENT_CONTINUITY: attached WorkBuddy action cannot start a new session")
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		random[6] = (random[6] & 0x0f) | 0x40
		random[8] = (random[8] & 0x3f) | 0x80
		identity = fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
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
	config, err := workBuddyMCPConfig(engineExecutable, f.root, run.RunID, claim.ActionID, identity)
	if err != nil {
		return "", err
	}
	if (len(coverageOnly) > 0 && coverageOnly[0]) || nativeTextOnlyRole(string(claim.Task.Step)) {
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
	cmd, err := workBuddySessionCommand(claim.Context.Executable, workspace, run.Regular.Settings.Values.Model, run.Regular.Settings.Values.ReasoningEffort, identity, resume, encodedSchema, configPath)
	if err != nil {
		return "", err
	}
	if len(coverageOnly) == 0 || !coverageOnly[0] {
		workBuddyRoleTools(cmd, string(claim.Task.Step))
	}
	// Restore the captured native environment.
	cmd.Env = os.Environ()
	for name, value := range claim.Context.Environment {
		cmd.Env = replaceEnvironment(cmd.Env, name, value)
	}
	attachedIdentity := ""
	_, err = runWorkBuddyProcess(ctx, cmd, job, claim.ActionID, identity, prompt, func(observed string) error {
		if err := f.submitSource(protocol.ExecutionSource{ActionID: claim.ActionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: observed, Status: "RUNNING"}); err != nil {
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
