package validate

import (
	"encoding/json"
	"strings"
)

type HookDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func Hook(payload []byte) (HookDecision, error) {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return HookDecision{}, err
	}

	command := hookCommand(decoded)
	if strings.TrimSpace(command) == "" {
		return allowHook("no command-like field found"), nil
	}
	if deniesGateWorkflowPassWithoutArtifact(command) {
		return HookDecision{
			Decision: "deny",
			Reason:   "gate-workflow record-stage PASS requires -Artifact",
		}, nil
	}
	return allowHook("command allowed"), nil
}

func allowHook(reason string) HookDecision {
	return HookDecision{Decision: "allow", Reason: reason}
}

func deniesGateWorkflowPassWithoutArtifact(command string) bool {
	if !mentionsGateWorkflow(command) {
		return false
	}
	tokens := splitCommand(command)
	if strings.EqualFold(switchValue(tokens, "Action"), "record-stage") &&
		strings.EqualFold(switchValue(tokens, "Verdict"), "PASS") &&
		strings.TrimSpace(switchValue(tokens, "Artifact")) == "" {
		return true
	}
	return false
}

func mentionsGateWorkflow(command string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(command, "\\", "/"))
	return strings.Contains(normalized, "gate-workflow.ps1")
}

func switchValue(tokens []string, name string) string {
	want := "-" + strings.ToLower(name)
	colonPrefix := want + ":"
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		lowerToken := strings.ToLower(token)
		if strings.HasPrefix(lowerToken, colonPrefix) {
			return token[len(colonPrefix):]
		}
		if lowerToken == want {
			if i == len(tokens)-1 {
				return ""
			}
			if strings.HasPrefix(tokens[i+1], "-") {
				return ""
			}
			return tokens[i+1]
		}
	}
	return ""
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
	escaped := false

	for _, ch := range command {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
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
	if escaped {
		current.WriteRune('\\')
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
