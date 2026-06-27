package validate

import (
	"bytes"
	"encoding/json"
	"strings"
)

type HookDecision struct {
	Decision                 string `json:"decision"`
	Reason                   string `json:"reason"`
	Permission               string `json:"permission"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

func Hook(payload []byte) (HookDecision, error) {
	var decoded any
	if err := json.Unmarshal(trimJSONBOM(payload), &decoded); err != nil {
		return HookDecision{}, err
	}

	command := hookCommand(decoded)
	if strings.TrimSpace(command) == "" {
		return allowHook("no command-like field found"), nil
	}
	if mentionsLegacyFormalGatesCommand(command) {
		return HookDecision{
			Decision:                 "block",
			Reason:                   "legacy PowerShell formal-gates commands are not supported; use native formal-gates commands",
			Permission:               "deny",
			PermissionDecision:       "deny",
			PermissionDecisionReason: "legacy PowerShell formal-gates commands are not supported; use native formal-gates commands",
		}, nil
	}
	if deniesFormalGatePassWithoutArtifact(command) {
		return HookDecision{
			Decision:                 "block",
			Reason:                   "formal gate PASS recording requires an artifact",
			Permission:               "deny",
			PermissionDecision:       "deny",
			PermissionDecisionReason: "formal gate PASS recording requires an artifact",
		}, nil
	}
	return allowHook("command allowed"), nil
}

func trimJSONBOM(payload []byte) []byte {
	return bytes.TrimPrefix(payload, []byte{0xef, 0xbb, 0xbf})
}

func allowHook(reason string) HookDecision {
	return HookDecision{
		Decision:                 "approve",
		Reason:                   reason,
		Permission:               "allow",
		PermissionDecision:       "allow",
		PermissionDecisionReason: reason,
	}
}

func deniesFormalGatePassWithoutArtifact(command string) bool {
	tokens := splitCommand(command)
	if !isFormalGatePassRecordCommand(command, tokens) {
		return false
	}
	if !hasSwitchValue(tokens, "Verdict", "PASS") {
		return false
	}
	if !hasNonEmptySwitchValue(tokens, "Artifact") {
		return true
	}
	return false
}

func isFormalGatePassRecordCommand(command string, tokens []string) bool {
	if mentionsNativeRecord(tokens) {
		return true
	}
	return false
}

func mentionsLegacyFormalGatesCommand(command string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(command, "\\", "/"))
	return strings.Contains(normalized, "gate-workflow.ps1") ||
		strings.Contains(normalized, "gate-state.ps1") ||
		strings.Contains(normalized, "gate-artifact-validation.ps1") ||
		strings.Contains(normalized, "gate-proof-receipt.ps1") ||
		strings.Contains(normalized, "install-formal-gates.ps1") ||
		strings.Contains(normalized, "run-complexity-gate.ps1") ||
		strings.Contains(normalized, "capture-subagent-receipt.ps1") ||
		strings.Contains(normalized, "enforce-gate-sequence.ps1")
}

func mentionsNativeRecord(tokens []string) bool {
	for i, token := range tokens {
		if !isFormalGatesExecutableToken(token) || i+2 >= len(tokens) {
			continue
		}
		group := strings.ToLower(tokens[i+1])
		action := strings.ToLower(tokens[i+2])
		if group == "workflow" && action == "record-stage" {
			return true
		}
		if group == "gate" && action == "record" {
			return true
		}
	}
	return false
}

func isFormalGatesExecutableToken(token string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(token, "\\", "/"))
	normalized = strings.Trim(normalized, `"'`)
	parts := strings.Split(normalized, "/")
	base := parts[len(parts)-1]
	return base == "formal-gates" ||
		base == "formal-gates.exe" ||
		base == "formal-gates-validate" ||
		base == "formal-gates-validate.exe"
}

func hasSwitchValue(tokens []string, name, expected string) bool {
	for _, value := range switchValues(tokens, name) {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

func hasNonEmptySwitchValue(tokens []string, name string) bool {
	for _, value := range switchValues(tokens, name) {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func switchValues(tokens []string, name string) []string {
	wants := []string{"-" + strings.ToLower(name), "--" + strings.ToLower(name)}
	var values []string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		lowerToken := strings.ToLower(token)
		for _, want := range wants {
			if value, ok := switchInlineValue(token, lowerToken, want); ok {
				values = append(values, value)
				continue
			}
			if lowerToken == want {
				if i == len(tokens)-1 || strings.HasPrefix(tokens[i+1], "-") {
					values = append(values, "")
					continue
				}
				values = append(values, tokens[i+1])
			}
		}
	}
	return values
}

func switchInlineValue(token, lowerToken, want string) (string, bool) {
	for _, separator := range []string{":", "="} {
		prefix := want + separator
		if strings.HasPrefix(lowerToken, prefix) {
			return token[len(prefix):], true
		}
	}
	return "", false
}

func hookCommand(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if command, ok := typed["command"].(string); ok {
			return command
		}
		if command, ok := typed["cmd"].(string); ok {
			return command
		}
		if input, ok := typed["tool_input"]; ok {
			if command := hookCommand(input); command != "" {
				return command
			}
		}
		if input, ok := typed["input"]; ok {
			if command := hookCommand(input); command != "" {
				return command
			}
		}
		if params, ok := typed["params"]; ok {
			if command := hookCommand(params); command != "" {
				return command
			}
		}
		if args, ok := typed["arguments"]; ok {
			if command := hookCommand(args); command != "" {
				return command
			}
		}
	case []any:
		for _, item := range typed {
			if command := hookCommand(item); command != "" {
				return command
			}
		}
	}
	return ""
}

func splitCommand(command string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune

	for _, ch := range command {
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			current.WriteRune(ch)
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
