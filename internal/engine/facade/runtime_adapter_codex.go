package facade

import (
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

func (codexRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.NativeString("codexExecutable")
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
	// tool registry; workflow roles may not delegate additional model calls.
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

func init() { registerBuiltinRuntime("codex", codexRuntimeAdapter{}) }

func (a codexRuntimeAdapter) source(identity, job string, binding *protocol.HostRuntimeContext) (string, error) {
	return a.transcript(identity, binding)
}
func (codexRuntimeAdapter) readIdentity(job, actionID string) (string, error) {
	identity := ""
	data, err := os.ReadFile(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
			identity = event.ThreadID
		}
		var native struct {
			Method string `json:"method"`
			Params struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			} `json:"params"`
			Result struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &native) == nil {
			if native.Method == "thread/started" && native.Params.Thread.ID != "" {
				identity = native.Params.Thread.ID
			}
			if native.Result.Thread.ID != "" {
				identity = native.Result.Thread.ID
			}
		}
	}
	if identity == "" {
		return "", fmt.Errorf("UNKNOWN: native thread identity unavailable")
	}
	return identity, nil
}
func (codexRuntimeAdapter) probe(q nativeProbe) (string, json.RawMessage, error) {
	raw, err := codexThreadRequest(q.Context.Executable, "thread/resume", q.Identity, q.Context, q.Workspace)
	return "thread/resume", raw, err
}
