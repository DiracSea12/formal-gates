package host

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

var zcodeDescriptor = &Descriptor{
	Lifecycle:    zcodeAdapter(),
	ID:           ZCode,
	InstallName:  "zcode",
	ManifestName: "ZCode",
	Aliases:      []string{"zcode", "z-code"},
	Installable:  true,
	Paths: PathLayout{
		GlobalBase: ".zcode/skills", GlobalHookConfig: ".zcode/cli/config.json", GlobalManaged: ".zcode/AGENTS.md",
		ProjectBase: ".zcode/skills", ProjectManaged: "AGENTS.md",
	},
	Hook:            HookSpec{Response: zcodeHookResponse, Kind: HookZCode, Protocol: ProtocolZCode, GateEvent: "PreToolUse", GateMatcher: ".*", LifecycleMatcher: "Agent|Task"},
	LifecycleEvents: []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"}, LifecycleRequired: true,
	CostProvider:    ZCode,
	Configuration:   []ConfigField{StringConfig("zcodeExecutable", "node"), StringConfig("zcodeScript", ""), StringConfig("zcodeHome", "")},
	ReadUsage:       parseZCodeTranscript,
	CheckIdentity:   zcodeTranscriptIdentity,
	ObserveRecovery: zcodeRecoveryOutcome,
}

// session/usage is cumulative. Its documented no-backend response contains
// zero model requests and must never be reported as actual zero consumption.
func parseZCodeTranscript(reader io.Reader) (Usage, error) {
	latest := Usage{}
	session := ""
	var requests int64
	var invalid error
	seen := false
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var raw struct {
			ID     string `json:"id"`
			Result *struct {
				Session  string `json:"sessionId"`
				Requests *int64 `json:"modelRequestCount"`
				Input    *int64 `json:"inputTokens"`
				Output   *int64 `json:"outputTokens"`
				Cache    *int64 `json:"cacheReadTokens"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &raw) != nil || !strings.HasSuffix(raw.ID, ":usage") {
			return false
		}
		r := raw.Result
		if r == nil || r.Session == "" || r.Requests == nil || *r.Requests <= 0 || r.Input == nil || r.Output == nil || r.Cache == nil || *r.Input < 0 || *r.Output < 0 || *r.Cache < 0 || *r.Cache > *r.Input {
			invalid = fmt.Errorf("ZCode cumulative billing source unavailable (including no-backend zero fallback)")
			return false
		}
		if session != "" && session != r.Session {
			invalid = fmt.Errorf("ZCode transcript contains multiple sessions")
			return false
		}
		session = r.Session
		value := Usage{InputCacheMissTokens: *r.Input - *r.Cache, InputCacheHitTokens: *r.Cache, OutputTokens: *r.Output}
		if seen && (*r.Requests < requests || value.InputCacheMissTokens < latest.InputCacheMissTokens || value.InputCacheHitTokens < latest.InputCacheHitTokens || value.OutputTokens < latest.OutputTokens) {
			invalid = fmt.Errorf("ZCode cumulative usage regressed")
			return false
		}
		latest = value
		requests = *r.Requests
		seen = true
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	if invalid != nil {
		return Usage{}, invalid
	}
	if !seen {
		return Usage{}, fmt.Errorf("ZCode cumulative billing source unavailable")
	}
	return latest, nil
}

func zcodeTranscriptIdentity(path, identity string) error {
	return checkStreamTranscriptIdentity(path, identity, func(raw json.RawMessage) string {
		var row struct {
			ID     string `json:"id"`
			Result struct {
				Session string `json:"sessionId"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &row) != nil || !strings.HasSuffix(row.ID, ":usage") {
			return ""
		}
		return row.Result.Session
	})
}

func zcodeRecoveryOutcome(r RecoveryObservation) string {
	if r.NativeMethod != "session/resume" {
		return "UNKNOWN"
	}
	var response struct {
		ID     string `json:"id"`
		Result struct {
			Session struct {
				ID string `json:"sessionId"`
			} `json:"session"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(r.NativeResponse, &response) != nil || response.ID != "recovery" || len(response.Error) > 0 && string(response.Error) != "null" || response.Result.Session.ID != r.Identity {
		return "UNKNOWN"
	}
	return "RESUMABLE"
}

type ZCodeHookDecision struct {
	HookSpecificOutput ZCodeHookSpecificOutput `json:"hookSpecificOutput"`
}

type ZCodeHookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

func zcodeHookResponse(decision HookDecision) any {
	if decision.PermissionDecision != "deny" {
		return nil
	}
	return ZCodeHookDecision{HookSpecificOutput: ZCodeHookSpecificOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: decision.Reason,
	}}
}
