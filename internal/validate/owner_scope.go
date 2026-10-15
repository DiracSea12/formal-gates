package validate

import (
	"path/filepath"
	"strings"
)

// The engine State owns persisted transcript/session identity. This module
// compares write callers against that authority.

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
