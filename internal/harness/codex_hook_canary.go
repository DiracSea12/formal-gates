package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	engineRuntime "formal-gates/internal/engine/runtime"
)

// The maintenance tool owns its patch; the model selects only the registered tool.
func CodexHookTarget(directory string, in io.Reader, out, errout io.Writer) int {
	request, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	schema := map[string]any{"name": "apply_patch", "description": "Apply the program-owned hook canary patch. Call once with {}.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}
	return serveNativeTools(in, out, errout, []any{schema}, func(name string, raw json.RawMessage) (any, error) {
		var arguments map[string]any
		if err := json.Unmarshal(raw, &arguments); err != nil || arguments == nil || len(arguments) != 0 || name != "apply_patch" {
			return nil, fmt.Errorf("apply_patch requires an empty object")
		}
		cmd := exec.Command("git", "apply", "--", filepath.Join(directory, "marker.patch"))
		cmd.Dir = textOf(getOf(request, "workspace"))
		// Patch application is rooted in the isolated workspace.
		cmd.Env = nativeChildEnvironment(map[string]string{"GIT_CEILING_DIRECTORIES": filepath.Dir(cmd.Dir)})
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("apply patch: %w: %s", err, output)
		}
		return map[string]any{"applied": true}, nil
	}, func(raw json.RawMessage, result any) error {
		return writeAtomicJSON(filepath.Join(directory, "target-call.json"), map[string]any{"request": raw, "result": result})
	})
}

// CodexHookClient uses the native transport and host-owned thread/turn identities.
// The isolated state binds the ordinary write wall to that original owner session.
func CodexHookClient(directory string) (resultErr error) {
	request, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		return err
	}
	workspace, binary := textOf(getOf(request, "workspace")), textOf(getOf(request, "binary"))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	hook := quote(binary) + " hook decide --provider codex --record-dir " + quote(filepath.Join(directory, "payloads"))
	args := []string{textOf(getOf(request, "codex")), "app-server", "--stdio", "-c", `hooks.PreToolUse=[{matcher=".*",hooks=[{type="command",command=` + strconv.Quote(hook) + `}]}]`, "-c", `mcp_servers.hook_canary={command=` + strconv.Quote(binary) + `,args=["harness","codex-hook-target",` + strconv.Quote(directory) + `]}`}
	events, err := os.Create(filepath.Join(directory, "native-events.jsonl"))
	if err != nil {
		return err
	}
	defer events.Close()
	var recordErr error
	var terminal any
	client, err := newNativeClient(nativeHooks{transportPrefix: "CODEX_NATIVE", requestPrefix: "CODEX_RPC", identify: func(c *nativeClient) any { return c.nextCounter() }, received: func(message any) {
		if recordErr == nil {
			recordErr = json.NewEncoder(events).Encode(message)
		}
		if getOf(message, "method") == "turn/completed" {
			terminal = getOf(message, "params")
		}
	}}, args, workspace, directory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, client.close()) }()
	if _, err = client.request("initialize", map[string]any{"clientInfo": map[string]any{"name": "formal-gates", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, nativeUnbounded); err != nil {
		return err
	}
	if err = client.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	opened, err := client.request("thread/start", map[string]any{"cwd": workspace, "ephemeral": true, "approvalPolicy": "never", "sandbox": "danger-full-access", "config": map[string]any{"bypass_hook_trust": true, "features.shell_tool": false, "features.unified_exec": false, "tools.view_image": false, "web_search": "disabled"}}, nativeUnbounded)
	if err != nil {
		return err
	}
	thread, _ := getOf(getOf(opened, "thread"), "id").(string)
	if thread == "" {
		return fmt.Errorf("Codex did not return a native thread identity")
	}
	if err = bindCodexHookOwner(workspace, thread); err != nil {
		return err
	}
	started, err := client.request("turn/start", map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": getOf(request, "prompt"), "text_elements": []any{}}}}, nativeUnbounded)
	if err != nil {
		return err
	}
	turn, _ := getOf(getOf(started, "turn"), "id").(string)
	if turn == "" {
		return fmt.Errorf("Codex did not return a native turn identity")
	}
	if err = writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"threadId": thread, "turnId": turn, "thread": opened, "turn": started}); err != nil {
		return err
	}
	for getOf(terminal, "threadId") != thread || getOf(getOf(terminal, "turn"), "id") != turn {
		if _, err = client.receive(nil); err != nil {
			return err
		}
	}
	if recordErr != nil {
		return recordErr
	}
	if getOf(getOf(terminal, "turn"), "status") != "completed" {
		return fmt.Errorf("Codex turn did not complete successfully")
	}
	return writeAtomicJSON(filepath.Join(directory, "terminal.json"), terminal)
}

func bindCodexHookOwner(workspace, thread string) error {
	view, err := decision.NewState(authoring.DefinitionVersion(definition.WorkflowDefinitionVersion), engineRuntime.PhaseDevelopmentParallel)
	if err != nil {
		return err
	}
	state := protocol.NewState(*view)
	state.RunID, state.Route, state.OwnerSession = "hook-canary", "regular", thread
	state.RequirementRevisions = []protocol.ConfirmedRevision{{Revision: "canary"}}
	state.IntakeConfirmationReceipt = &protocol.IntakeConfirmationReceipt{RequirementRevision: "canary", Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: "canary"}}}
	state.DispatchBindings["development"] = protocol.WorkBinding{RequirementRevision: "canary", PlanRequest: "confirmed-plan", BatchID: "owned-work"}
	store, err := persistence.NewStore(filepath.Join(workspace, ".gates", "engine", state.RunID), persistence.Config{PackageDigest: "sha256:hook-canary", DefinitionSource: "definitions/workflow.json", InstalledTargetIdentity: "hook-canary"})
	if err != nil {
		return err
	}
	_, err = store.Save(persistence.Transaction{ExpectedFingerprint: "canary", CollectFingerprint: func() (string, error) { return "canary", nil }, Content: state})
	return err
}
