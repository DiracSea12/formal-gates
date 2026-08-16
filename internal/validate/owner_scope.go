package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 本文件是「写墙 owner 作用域」的完整实现：把写墙收窄到「启动 formal-gates 的那个
// 对话」，其它对话放行（子代理仍按 agent_type 维持现有逻辑，与 owner 无关）。三部分
// 集中在此，不散落到其它文件：
//   1. 捕获 —— 主线程执行 workflow start 的 PreToolUse 时，hook 把发起对话身份写入
//      sidecar（<root>/.gates/tmp/start-owner.json）。
//   2. 持久化 —— Start 读 sidecar 记进 run state 的 ownerTranscript/ownerSession。
//   3. 比对 —— 写墙主线程分支按 payload 身份与 owner 是否匹配收窄。
// 身份主键用 transcript_path（Claude Code / Codex 稳定、内嵌会话 UUID），session_id
// 兜底（Cursor 用 conversation_id 并入 session 兜底）；三键都读 snake/camel 多候选名。
// owner 未知时保守拦截，保证这是纯收窄、不回退保护。

const startOwnerSidecarName = "start-owner.json"

// ownerSidecarPath 返回 hook 写、Start 读的桥接文件路径。
func ownerSidecarPath(root string) string {
	return filepath.Join(root, ".gates", "tmp", startOwnerSidecarName)
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

// isWorkflowStartCommand 识别「formal-gates workflow start」命令（复用
// isFormalGatesExecutableToken 的命令结构解析）。
func isWorkflowStartCommand(command string) bool {
	tokens := splitCommand(command)
	for i, token := range tokens {
		if !isFormalGatesExecutableToken(token) || i+2 >= len(tokens) {
			continue
		}
		if strings.ToLower(tokens[i+1]) == "workflow" && strings.ToLower(tokens[i+2]) == "start" {
			return true
		}
	}
	return false
}

// captureStartOwner 在「主线程 + workflow start」时把发起对话身份写入 sidecar。任何一步
// 不满足（非主线程、非 start、无身份字段、无 --root）都静默跳过——宁可丢失 owner 也不
// 污染 sidecar 或干扰判定。
func captureStartOwner(decoded any) {
	if hookAgentType(decoded) != "" {
		return
	}
	command := hookCommand(decoded)
	if !isWorkflowStartCommand(command) {
		return
	}
	transcript, session := hookOwnerIdentity(decoded)
	if transcript == "" && session == "" {
		return
	}
	root := workflowStartRoot(command)
	if root == "" {
		return
	}
	dir := filepath.Dir(ownerSidecarPath(root))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"transcript": transcript, "session": session})
	_ = os.WriteFile(ownerSidecarPath(root), data, 0o600)
}

// workflowStartRoot 从 workflow start 命令解析 --root（支持 --root X 与 --root=X）。
func workflowStartRoot(command string) string {
	for _, v := range switchValues(splitCommand(command), "root") {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// consumeStartOwner 由 Start 调用：读 sidecar 并立即删除（成不成都删，避免污染下次
// start），返回其中记录的 owner 身份。无 sidecar 或解析失败返回空串。
func consumeStartOwner(root string) (transcript, session string) {
	path := ownerSidecarPath(root)
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return "", ""
	}
	var probe struct {
		Transcript string `json:"transcript"`
		Session    string `json:"session"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", ""
	}
	return strings.TrimSpace(probe.Transcript), strings.TrimSpace(probe.Session)
}

// readRunOwner 读取 run 状态文件里的 owner 字段（旧 run 缺失时返回空串）。
func readRunOwner(statePath string) (transcript, session string) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return "", ""
	}
	var probe struct {
		OwnerTranscript string `json:"ownerTranscript"`
		OwnerSession    string `json:"ownerSession"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", ""
	}
	return strings.TrimSpace(probe.OwnerTranscript), strings.TrimSpace(probe.OwnerSession)
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
