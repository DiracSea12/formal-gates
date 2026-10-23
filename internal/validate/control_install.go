package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const controlServerName = "formal-gates"

const controlTOMLStart = "# formal-gates:control:start"
const controlTOMLEnd = "# formal-gates:control:end"

func controlConfigPath(target installTarget) string {
	adapter, err := platformAdapterFor(target.host)
	if err != nil || adapter.controlPath == nil {
		return ""
	}
	return adapter.controlPath(target)
}

func configureControlServer(target installTarget, remove bool) (bool, error) {
	path := controlConfigPath(target)
	if path == "" {
		return false, nil
	}
	adapter, _ := platformAdapterFor(target.host)
	return adapter.configureControl(target, path, remove)
}

func configureCodexControl(target installTarget, path string, remove bool) (bool, error) {
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	text := string(before)
	start, end := strings.Index(text, controlTOMLStart), strings.Index(text, controlTOMLEnd)
	if (start < 0) != (end < 0) || start >= 0 && end < start {
		return false, fmt.Errorf("incomplete formal-gates MCP configuration in %s", path)
	}
	if start >= 0 {
		end += len(controlTOMLEnd)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
	conflict := regexp.MustCompile(`(?m)^\s*\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*(?:formal-gates|"formal-gates"|'formal-gates')\s*(?:\]|\.)`)
	var config struct {
		Servers map[string]map[string]any `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(text), &config); err != nil {
		return false, fmt.Errorf("existing Codex MCP configuration is invalid in %s: %w", path, err)
	}
	if entry, present := config.Servers[controlServerName]; present {
		desired := []any{"workflow", "serve", "--package-root", filepath.ToSlash(target.targetPath)}
		if entry["command"] != filepath.ToSlash(targetLauncherPath(target)) || !ownControlArgs(entry["args"], desired) {
			if remove {
				return false, nil
			}
			return false, fmt.Errorf("MCP server %q in %s is not owned by this installation", controlServerName, path)
		}
		if !remove {
			return false, nil
		}
		// Native TOML decoding proves ownership; table spans preserve other settings.
		candidates := regexp.MustCompile(`(?m)^[ \t]*\[[^\r\n]*\][ \t]*(?:#[^\r\n]*)?(?:\r?\n|$)`).FindAllStringIndex(text, -1)
		var headers [][]int
		for _, span := range candidates {
			// A table starts after a complete document, not inside a TOML value.
			var prefix map[string]any
			if toml.Unmarshal([]byte(text[:span[0]]), &prefix) == nil {
				headers = append(headers, span)
			}
		}

		for i := len(headers) - 1; i >= 0; i-- {
			span := headers[i]
			if !conflict.MatchString(text[span[0]:span[1]]) {
				continue
			}
			end := len(text)
			if i+1 < len(headers) {
				end = headers[i+1][0]
			}
			text = text[:span[0]] + text[end:]
		}
	}
	if !remove {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += controlTOMLStart + "\n[mcp_servers.formal-gates]\ncommand = " + strconv.Quote(filepath.ToSlash(targetLauncherPath(target))) + "\nargs = [\"workflow\", \"serve\", \"--package-root\", " + strconv.Quote(filepath.ToSlash(target.targetPath)) + "]\n" + controlTOMLEnd + "\n"
	}
	return writeControlConfig(path, before, []byte(text))
}

// jsonServerControl configures the installer-owned MCP server entry inside a
// JSON document. serversPath locates the server map (e.g. ["mcp","servers"]
// or ["mcpServers"]) and entryType is the entry's transport type, empty when
// the host does not declare one.
func jsonServerControl(label string, serversPath []string, entryType string) func(installTarget, string, bool) (bool, error) {
	return func(target installTarget, path string, remove bool) (bool, error) {
		before, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		config := map[string]any{}
		if len(before) != 0 {
			if err := json.Unmarshal(before, &config); err != nil || config == nil {
				return false, fmt.Errorf("existing %s MCP configuration must be a JSON object: %s", label, path)
			}
		}
		node := config
		parents := []map[string]any{config}
		for _, key := range serversPath {
			child, ok := node[key].(map[string]any)
			if node[key] != nil && !ok {
				return false, fmt.Errorf("existing %s MCP configuration has malformed %s object: %s", label, key, path)
			}
			if child == nil {
				child = map[string]any{}
			}
			node[key] = child
			parents = append(parents, child)
			node = child
		}
		servers := node
		if _, present := servers[controlServerName]; remove && !present {
			return false, nil
		}
		desired := map[string]any{"command": filepath.ToSlash(targetLauncherPath(target)), "args": []any{"workflow", "serve", "--package-root", filepath.ToSlash(target.targetPath)}}
		if entryType != "" {
			desired["type"] = entryType
		}
		if existing, present := servers[controlServerName]; present {
			entry, ok := existing.(map[string]any)
			if !ok || entry["command"] != desired["command"] || !ownControlArgs(entry["args"], desired["args"]) {
				if remove {
					return false, nil
				}
				return false, fmt.Errorf("MCP server %q in %s is not owned by this installation", controlServerName, path)
			}
			for key, value := range entry {
				if _, owned := desired[key]; !owned {
					desired[key] = value
				}
			}
		}
		if remove {
			delete(servers, controlServerName)
		} else {
			servers[controlServerName] = desired
		}
		for i := len(serversPath); i > 0; i-- {
			if len(parents[i]) == 0 {
				delete(parents[i-1], serversPath[i-1])
			}
		}
		if len(config) == 0 {
			return writeControlConfig(path, before, nil)
		}
		desiredBytes, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return false, err
		}
		return writeControlConfig(path, before, append(desiredBytes, '\n'))
	}
}

// ownControlArgs reports whether args matches an argument list this installer
// accepts as its own control-server entry: bare "workflow serve" or the
// desired slice.
func ownControlArgs(args, desired any) bool {
	return reflect.DeepEqual(args, desired) || reflect.DeepEqual(args, []any{"workflow", "serve"})
}

func writeControlConfig(path string, before, desired []byte) (bool, error) {
	if string(before) == string(desired) {
		return false, nil
	}
	if len(desired) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			err = nil
		}
		return true, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, desired, 0600)
}
