package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"formal-gates/internal/host"
)

// The hook captures the initiating conversation into a one-use bootstrap payload.
// The current engine State owns its persisted transcript/session identity; this
// module compares the write caller against that authority.

// ownerSidecarPath 返回 hook 写、Start 读的桥接文件路径。
func ownerSidecarPath(root, runID string) string {
	return filepath.Join(root, ".gates", "tmp", runID+".start-owner.json")
}

// hookOwnerIdentity 从 PreToolUse 载荷提取发起对话身份：transcript_path 为主键、
// session_id 兜底（Cursor 的 conversation_id 并入 session 兜底）。
func hookOwnerIdentity(value any) (transcript, session string) {
	transcript = hookPayloadField(value, "transcript_path", "transcriptPath")
	session = hookPayloadField(value, "session_id", "sessionId")
	if session == "" {
		session = hookPayloadField(value, "conversation_id", "conversationId")
	}
	return transcript, session
}

// hookPayloadField 从 payload（含可能的嵌套 value 层）读一个字段，按候选名依次尝试。
func hookPayloadField(value any, names ...string) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range names {
			if scalar := scalarHookString(object[name]); scalar != "" {
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookPayloadField(nested, names...)
		}
	}
	return ""
}

// isWorkflowStartCommand checks the actual executable and start operation.
// The final precheck value follows the CLI boolean flag semantics.
func isWorkflowStartCommand(tokens []string) bool {
	if len(tokens) < 3 || !isFormalGatesExecutableToken(tokens[0]) ||
		!strings.EqualFold(tokens[1], "workflow") || !strings.EqualFold(tokens[2], "start") {
		return false
	}
	precheck := false
	for _, token := range tokens[3:] {
		for _, name := range []string{"-precheck", "--precheck"} {
			if token == name {
				precheck = true
			} else if value, ok := switchInlineValue(token, token, name); ok {
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return false
				}
				precheck = parsed
			}
		}
	}
	return !precheck
}

// captureStartOwner 在「主线程 + workflow start」时把发起对话身份写入 sidecar。任何一步
// 不满足（非主线程、非 start、无身份字段、相对根目录无宿主 cwd）都静默跳过。
func captureStartOwner(decoded any, providers ...string) {
	if hookAgentType(decoded) != "" {
		return
	}
	for _, segment := range commandSegments(hookCommand(decoded)) {
		tokens := executableCommandTokens(segment)
		if !isWorkflowStartCommand(tokens) {
			continue
		}
		runID := strings.TrimSpace(workflowStartSwitchValue(tokens, "run-id", ""))
		if runID == "" {
			continue
		}
		transcript, session := hookOwnerIdentity(decoded)
		if transcript == "" && session == "" {
			continue
		}
		root := workflowStartSwitchValue(tokens, "root", ".")
		if !filepath.IsAbs(root) {
			cwd := hookPayloadCwd(decoded)
			if cwd == "" {
				continue
			}
			root = filepath.Join(cwd, root)
		}
		path := ownerSidecarPath(root, runID)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			continue
		}
		// Use the host already selected by hook decide --provider. Transcript file
		// names and session IDs do not establish which parser owns the format.
		provider := ""
		if len(providers) > 0 && strings.TrimSpace(providers[0]) != "" {
			if descriptor, err := host.Lookup(providers[0]); err == nil {
				provider = descriptor.ID
			}
		}
		data, _ := json.Marshal(map[string]string{"transcript": transcript, "session": session, "provider": provider})
		_ = os.WriteFile(path, data, 0o600)
	}
}

// workflowStartSwitchValue uses the last supplied value, as the CLI flag parser does.
func workflowStartSwitchValue(tokens []string, name, fallback string) string {
	values := switchValues(tokens, name)
	if len(values) > 0 {
		return values[len(values)-1]
	}
	return fallback
}

// ConsumeStartOwner 由 Start 调用：读 sidecar 并立即删除（成不成都删，避免污染下次
// start），返回其中记录的 owner 身份。无 sidecar 或解析失败返回空串。
func ConsumeStartOwner(root, runID string) (transcript, session, provider string) {
	path := ownerSidecarPath(root, strings.TrimSpace(runID))
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return "", "", ""
	}
	var probe struct {
		Transcript string `json:"transcript"`
		Session    string `json:"session"`
		Provider   string `json:"provider"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", "", ""
	}
	return strings.TrimSpace(probe.Transcript), strings.TrimSpace(probe.Session), strings.TrimSpace(probe.Provider)
}

// readRunOwner obtains scope identity from the current engine authority.
// Child work uses the retained master's owner when it has no separate identity.
func readRunOwner(statePath string) (transcript, session string) {
	state, err := readHookEngineState(statePath)
	if err != nil {
		return "", ""
	}
	if state.OwnerTranscript == "" && state.OwnerSession == "" && state.SliceChild != nil {
		parent := filepath.Join(filepath.Dir(filepath.Dir(statePath)), state.SliceChild.MasterRunID, "state.json")
		if master, err := readHookEngineState(parent); err == nil {
			state = master
		}
	}
	return strings.TrimSpace(state.OwnerTranscript), strings.TrimSpace(state.OwnerSession)
}

// ownerIdentity 是某个活动 run 的 owner 身份（启动对话 transcript / session）。
type ownerIdentity struct {
	transcript string
	session    string
}

// sameOwnerIdentity 比对 payload 身份与所有活动 run 的 owner。known=false 表示无法判定
// （所有 owner 关键字段都缺失，应保守拦截）；known=true 且 same=false 表示确属不同对话
// （不匹配任一 owner，可放行）。主键 transcript_path 优先，其次 session。任一 owner 命中
// 即判 same（多 run 并集，避免只取最后一个 run 的 owner 导致其它 run 的 owner 被绕过）。
func sameOwnerIdentity(owners []ownerIdentity, payloadTranscript, payloadSession string) (known, same bool) {
	hasUnknown := false
	for _, owner := range owners {
		// transcript 是主键：两侧都非空就只比 transcript，不比 session。
		if owner.transcript != "" && payloadTranscript != "" {
			if owner.transcript == payloadTranscript {
				return true, true
			}
			continue
		}
		if owner.session != "" && payloadSession != "" {
			if owner.session == payloadSession {
				return true, true
			}
			continue
		}
		hasUnknown = true // 该 owner 无身份，或两侧关键字段不全，无法比对
	}
	if hasUnknown {
		return false, false // 存在未知 owner，无法排除 payload 就是它的 owner → 保守拦截
	}
	return true, false // 所有 owner 都已知且不匹配 → 确属不同对话
}
