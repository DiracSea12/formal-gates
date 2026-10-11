package host

import (
	"os"
)

// deepseekAdapter adapts DeepSeek Harness (DSH). DSH 本身没有原生 shell-hook
// 配置，formal-gates 安装器会写一个最小 Cordis 插件，把 PreToolUse 判定和
// subagent/start、subagent/end 生命周期事件转发给原生二进制。插件使用 Codex
// 形状的 identity 字段（agent_id/subagent_id），所以 start/stop 可以按同一个
// identity 配对。

func deepseekAdapter() *Lifecycle {
	return &Lifecycle{
		Name:            DeepSeek,
		EnvironmentKeys: []string{"DSH_HOME", "DSH_PROJECT_DIR"},
		NormalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(DeepSeek, eventName, "SubagentStart", "SubagentStop")
		},
		Identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"agent_id", "agentId", "subagent_id", "subagentId"})
		},
		Correlation: func(_ string, _ any) string { return "" },
		ProjectRoots: func(payload any) []string {
			roots := AppendUniqueStrings(nil, os.Getenv("DSH_PROJECT_DIR"))
			roots = AppendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
			return AppendUniqueStrings(roots, payloadProjectRoots(payload)...)
		},
		TranscriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"transcript_path", "transcriptPath"}, 0)
		},
		Reason: payloadReason,
	}
}
