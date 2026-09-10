package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex-specific flags, session storage and JSONL event names belong here.
// All callers use the same binding and tool boundary for fresh or resumed work.
type codexRuntimeAdapter struct{}

func (codexRuntimeAdapter) execute(f *Facade, actionID string, resume bool) (Run, error) {
	return f.executeCodex(actionID, resume)
}

func (codexRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.CodexExecutable
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	nativeHome := os.Getenv("CODEX_HOME")
	if nativeHome == "" {
		nativeHome = filepath.Join(home, ".codex")
	}
	nativeHome, err = filepath.Abs(nativeHome)
	if err != nil {
		return nil, err
	}
	return &protocol.HostRuntimeContext{Provider: "codex", Executable: resolved, Environment: map[string]string{"HOME": home, "CODEX_HOME": nativeHome, "XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"), "PATH": os.Getenv("PATH")}}, nil
}

func (codexRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if binding == nil || binding.Provider != "codex" || !filepath.IsAbs(binding.Executable) || binding.Environment["CODEX_HOME"] == "" {
		return fmt.Errorf("RUNTIME_CONTEXT_MISSING: native execution needs its original adapter binding")
	}
	if info, err := os.Stat(binding.Executable); err != nil || info.IsDir() {
		return fmt.Errorf("RUNTIME_CONTEXT_UNAVAILABLE: original executable %s is unavailable", binding.Executable)
	}
	return nil
}
func (codexRuntimeAdapter) transcript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	if binding == nil || binding.Provider != "codex" {
		return "", fmt.Errorf("RUNTIME_CONTEXT_MISSING: transcript lookup needs the original runtime binding")
	}
	matches, err := filepath.Glob(filepath.Join(binding.Environment["CODEX_HOME"], "sessions", "*", "*", "*", "*"+identity+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("native session %s has %d matching transcript files in its bound context", identity, len(matches))
	}
	return matches[0], nil
}

// Codex merges table overrides. Enumerating the resolved configuration includes
// both user and workspace settings, and disabling named entries keeps personal
// configuration editable without making it a workflow tool grant.
func (codexRuntimeAdapter) toolConfig(raw json.RawMessage) (map[string]any, error) {
	var result struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Config == nil {
		return nil, fmt.Errorf("BRIDGE_UNAVAILABLE: effective tool configuration unavailable")
	}
	// Some native models still register v1 delegation with both multi-agent
	// feature flags off. Zero native depth removes that route from the actual
	// tool registry; descendants must use the engine's bounded model.run tool.
	config := map[string]any{"web_search": "disabled", "features.enable_mcp_apps": false, "features.multi_agent": false, "features.multi_agent_v2": false, "agents": map[string]any{"max_depth": 0}}
	for _, category := range []string{"mcp_servers", "plugins"} {
		entries := map[string]json.RawMessage{}
		if raw, ok := result.Config[category]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return nil, err
			}
		}
		disabled := map[string]any{}
		for name, rawEntry := range entries {
			var entry map[string]any
			if err := json.Unmarshal(rawEntry, &entry); err != nil || entry == nil {
				return nil, fmt.Errorf("BRIDGE_UNAVAILABLE: invalid %s entry", category)
			}
			// The native JSON-to-TOML bridge turns null into an empty string.
			// Omit unset optional fields while retaining the actual transport.
			for key, value := range entry {
				if value == nil {
					delete(entry, key)
				}
			}
			entry["enabled"] = false
			disabled[name] = entry
		}
		config[category] = disabled
	}
	return config, nil
}

func (codexRuntimeAdapter) runModel(ctx context.Context, run Run, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	q := op.Request
	fail := func(err error) protocol.TaskOperationReceipt {
		result.Status = "FAILED"
		result.Message = err.Error()
		return result
	}
	config := run.Regular.Settings.Values
	answer := filepath.Join(job, "answer.txt")
	args := []string{"exec", "--ignore-user-config", "-c", "agents.max_depth=0", "--disable", "shell_tool", "--disable", "multi_agent", "--disable", "multi_agent_v2", "--disable", "enable_mcp_apps", "-c", `web_search="disabled"`, "--skip-git-repo-check", "--json", "--color", "never", "-s", "read-only", "-C", job, "-o", answer}
	if config.Model != "" {
		args = append(args, "-m", config.Model)
	}
	if config.ReasoningEffort != "" {
		raw, _ := json.Marshal(config.ReasoningEffort)
		args = append(args, "-c", "model_reasoning_effort="+string(raw))
	}
	cmd := exec.CommandContext(ctx, config.CodexExecutable, append(args, "-")...)
	if err := runtimeCommandContext(cmd, q.Binding.Context); err != nil {
		return fail(err)
	}
	cmd.Stdin = strings.NewReader("Answer only from the supplied prompt text. Do not search for files, tools, or external context. If necessary input is absent, state what is missing; do not invent workflow results.\n\n" + q.Prompt)
	code, err := runOperationProcess(cmd, job, &result)
	result.ExitCode = code
	if err != nil {
		return fail(err)
	}
	if code != 0 {
		return fail(fmt.Errorf("native model exited %d", code))
	}
	data, err := os.ReadFile(answer)
	if err != nil {
		return fail(err)
	}
	result.Output = string(data)
	journal, err := os.ReadFile(filepath.Join(job, "stdout.log"))
	if err != nil {
		return fail(err)
	}
	for _, line := range strings.Split(string(journal), "\n") {
		var v struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &v) == nil && v.Type == "thread.started" {
			result.Identity = v.ThreadID
		}
	}
	if result.Identity == "" {
		return fail(fmt.Errorf("UNKNOWN: model completed without observed native identity"))
	}
	result.TranscriptPath, err = findBoundTranscript(result.Identity, q.Binding.Context)
	if err != nil {
		return fail(err)
	}
	result.Status = "COMPLETED"
	return result
}

func (codexRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, r protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	unknown := func(err error) (protocol.TaskOperationReceipt, error) {
		return r, fmt.Errorf("UNKNOWN: %s; no underlying call repeated: %w", op.ID, err)
	}
	journal, err := os.ReadFile(filepath.Join(job, "stdout.log"))
	if err != nil {
		return unknown(err)
	}
	complete := false
	for _, line := range strings.Split(string(journal), "\n") {
		var v struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &v) == nil {
			if v.Type == "thread.started" {
				r.Identity = v.ThreadID
			}
			if v.Type == "turn.completed" {
				complete = true
			}
		}
	}
	if !complete || r.Identity == "" || r.ExitCode != 0 {
		return unknown(fmt.Errorf("native model success is not observed"))
	}
	r.TranscriptPath, err = findBoundTranscript(r.Identity, op.Request.Binding.Context)
	if err != nil {
		return unknown(err)
	}
	answer, err := os.ReadFile(filepath.Join(job, "answer.txt"))
	if err != nil {
		return unknown(err)
	}
	r.Output = string(answer)
	return r, nil
}
