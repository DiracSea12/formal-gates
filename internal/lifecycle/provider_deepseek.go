package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

// deepseekAdapter adapts DeepSeek Harness (DSH). DSH 本身没有原生 shell-hook
// 配置，formal-gates 安装器会写一个最小 Cordis 插件，把 PreToolUse 判定和
// subagent/start、subagent/end 生命周期事件转发给原生二进制。插件使用 Codex
// 形状的 identity 字段（agent_id/subagent_id），所以 start/stop 可以按同一个
// identity 配对。

// deepseekGlobalInstallPrefix is the normalized absolute prefix of a global DSH
// install ($DSH_HOME/skills/formal-gates/bin/). Project-local .dsh skill
// directories intentionally do not match: DSH auto-loads no project-level hook
// patch, so those binaries keep the lenient default lifecycle provider.
func deepseekGlobalInstallPrefix() string {
	home := strings.TrimSpace(os.Getenv("DSH_HOME"))
	if home == "" {
		// DSH 默认 home 是 ~/.dsh，与 $DSH_HOME 显式给出的目录语义不同：
		// 显式值本身已经是 home 根目录，不再追加 .dsh。
		userHome, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(userHome, ".dsh")
	} else {
		absolute, err := filepath.Abs(home)
		if err != nil {
			return ""
		}
		home = absolute
	}
	prefix := filepath.ToSlash(filepath.Join(home, "skills", "formal-gates", "bin")) + "/"
	return strings.ToLower(prefix)
}

// isProjectDshInstall reports whether path is a project-local DSH skill binary
// rather than a global DSH-home install.
func isProjectDshInstall(path string) bool {
	path = strings.TrimSpace(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if !strings.Contains(normalized, "/.dsh/skills/formal-gates/bin/") {
		return false
	}
	prefix := deepseekGlobalInstallPrefix()
	return prefix == "" || !strings.HasPrefix(normalized, prefix)
}

func deepseekAdapter() providerAdapter {
	return providerAdapter{
		name:       ProviderDeepSeek,
		required:   true,
		hookEvents: []string{"SubagentStart", "SubagentStop"},
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderDeepSeek, eventName, "SubagentStart", "SubagentStop")
		},
		identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"agent_id", "agentId", "subagent_id", "subagentId"})
		},
		correlation: func(_ string, _ any) string { return "" },
		projectRoots: func(payload any) []string {
			roots := appendUniqueStrings(nil, os.Getenv("DSH_PROJECT_DIR"))
			roots = appendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
			return appendUniqueStrings(roots, payloadProjectRoots(payload)...)
		},
		transcriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"transcript_path", "transcriptPath"}, 0)
		},
		reason: payloadReason,
	}
}
