package facade

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runWorkBuddyBridge composes the platform command, MCP config, process journal
// and observed identity callback. The public driver must claim first; this
// transport neither schedules nor registers the resulting action verdict.

func workBuddyCarrierWorkspace(identity, workspace string) (string, func(), error) {
	if identity == "" || !filepath.IsAbs(workspace) {
		return "", nil, fmt.Errorf("WORKBUDDY_CONFIG: bound identity and absolute workspace are required for native cwd")
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return "", nil, fmt.Errorf("WORKBUDDY_CONFIG: authorized workspace unavailable: %s", workspace)
	}
	base := filepath.Join(os.TempDir(), "fgw")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256([]byte(identity))
	carrier := filepath.Join(base, fmt.Sprintf("%x", sum[:8]))
	// The action lock prevents two native owners for one identity. Recreate the
	// deterministic carrier so a crashed prior turn cannot leak local files into
	// the resumed turn while WorkBuddy still finds its session by the same short cwd.
	if err := os.RemoveAll(carrier); err != nil {
		return "", nil, err
	}
	if err := os.Mkdir(carrier, 0700); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(carrier)
		_ = os.Remove(base)
	}
	return carrier, cleanup, nil
}

func workBuddyAuthorizeWorkspace(cmd *exec.Cmd, step, workspace, prompt string) (string, error) {
	access := nativeRoleAccess(step)
	if access != nativeWorkspaceWrite && access != nativeReadOnly {
		return prompt, nil
	}
	if !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("WORKBUDDY_CONFIG: authorized workspace must be absolute")
	}
	cmd.Args = append(cmd.Args, "--add-dir", workspace)
	return fmt.Sprintf("Formal-Gates authorized workspace: %s\nThe native process cwd is only a short transport directory used to keep WorkBuddy session storage within filesystem limits. Use the authorized workspace above for every file or shell operation; do not create product files in the transport directory.\n\n%s", workspace, prompt), nil
}

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
	if q.NoTools {
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
	carrier, cleanupCarrier, err := workBuddyCarrierWorkspace(identity, workspace)
	if err != nil {
		return "", err
	}
	defer cleanupCarrier()
	cmd, err := workBuddySessionCommand(claim.Context.Executable, carrier, q.Settings.Model, q.Settings.ReasoningEffort, identity, resume, encodedSchema, configPath)
	if err != nil {
		return "", err
	}
	if !q.NoTools {
		workBuddyRoleTools(cmd, string(claim.Task.Step))
	} else {
		for i := 1; i+1 < len(cmd.Args); i++ {
			if cmd.Args[i] == "--tools" {
				cmd.Args[i+1] = "Read,Glob,Grep,StructuredOutput"
			}
		}
	}
	prompt, err = workBuddyAuthorizeWorkspace(cmd, string(claim.Task.Step), workspace, prompt)
	if err != nil {
		return "", err
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
