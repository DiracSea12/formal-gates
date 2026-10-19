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

	"formal-gates/internal/host"
)

const controlServerName = "formal-gates"

const controlTOMLStart = "# formal-gates:control:start"
const controlTOMLEnd = "# formal-gates:control:end"

func controlConfigPath(target installTarget) string {
	base := filepath.Dir(filepath.Dir(target.targetPath))
	switch target.host {
	case "devin":
		return filepath.Join(base, "mcp_config.json")
	case "codex":
		return filepath.Join(base, "config.toml")
	case "zcode":
		if target.hookConfig != "" {
			return target.hookConfig
		}
		return filepath.Join(base, "config.json")
	}
	return ""
}

func configureControlServer(target installTarget, remove bool) (bool, error) {
	path := controlConfigPath(target)
	if path == "" {
		return false, nil
	}
	if target.host == "zcode" {
		return configureZCodeControl(target, path, remove)
	}
	if target.host == "devin" {
		return configureDevinControl(target, path, remove)
	}
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
	if !remove {
		conflict := regexp.MustCompile(`(?m)^\s*\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*(?:formal-gates|"formal-gates"|'formal-gates')\s*(?:\]|\.)`)
		if conflict.MatchString(text) {
			return false, fmt.Errorf("MCP server %q already exists outside the installer-owned block in %s", controlServerName, path)
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += controlTOMLStart + "\n[mcp_servers.formal-gates]\ncommand = " + strconv.Quote(filepath.ToSlash(targetLauncherPath(target))) + "\nargs = [\"workflow\", \"serve\"]\n" + controlTOMLEnd + "\n"
	}
	return writeControlConfig(path, before, []byte(text))
}

func configureZCodeControl(target installTarget, path string, remove bool) (bool, error) {
	return configureJSONControl(target, path, remove, []string{"mcp", "servers"}, "stdio")
}

func configureDevinControl(target installTarget, path string, remove bool) (bool, error) {
	return configureJSONControl(target, path, remove, []string{"mcpServers"}, "")
}

func configureJSONControl(target installTarget, path string, remove bool, serverPath []string, serverType string) (bool, error) {
	descriptor, err := host.Lookup(target.host)
	if err != nil {
		return false, err
	}
	name := descriptor.ManifestName
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	config := map[string]any{}
	if len(before) != 0 {
		if err := json.Unmarshal(before, &config); err != nil || config == nil {
			return false, fmt.Errorf("existing %s MCP configuration must be a JSON object: %s", name, path)
		}
	}
	parents := make([]map[string]any, 0, len(serverPath))
	servers := config
	for _, key := range serverPath {
		parents = append(parents, servers)
		child, ok := servers[key].(map[string]any)
		if servers[key] != nil && !ok {
			return false, fmt.Errorf("existing %s MCP configuration has malformed %s object: %s", name, key, path)
		}
		if child == nil {
			child = map[string]any{}
		}
		servers = child
	}
	if _, present := servers[controlServerName]; remove && !present {
		return false, nil
	}
	desired := map[string]any{"command": filepath.ToSlash(targetLauncherPath(target)), "args": []any{"workflow", "serve"}}
	if serverType != "" {
		desired["type"] = serverType
	}
	if existing, present := servers[controlServerName]; present {
		entry, ok := existing.(map[string]any)
		if !ok || entry["command"] != desired["command"] || !reflect.DeepEqual(entry["args"], desired["args"]) {
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
	for i := len(serverPath) - 1; i >= 0; i-- {
		parent, key := parents[i], serverPath[i]
		if len(servers) == 0 {
			delete(parent, key)
		} else {
			parent[key] = servers
		}
		servers = parent
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
