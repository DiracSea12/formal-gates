package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 主代理与审查类代理写阻断（PreToolUse hook）。
//
// 正式流程进入开发阶段后，主代理（主线程，payload 无 agent_id/agent_type）与全部审查类
// 代理不得写代码或直接改 run 状态；run 状态只能经 CLI 写入，代码与执行文件只能经
// development-worker 派发写入。判定按调用者身份（agent_type 匹配）、不按文件路径
// （无静态文件白名单，千项目通用）。存在活动正式 run 时生效；无活动 run 放行，不干扰
// 普通项目。
//
// 阻断：主代理与审查类代理对代码与 run 状态的直接写入（Edit/Write/MultiEdit、
// git commit、写文件 Bash）。
// 放行：formal-gates CLI 命令（run 状态唯一合法写入者）与只读命令；development-worker
// （写代码）；qa-design（白盒设计者写测试代码、黑盒设计者写用例文档）；主代理对已登记
// 需求/设计文档的编辑（按活动 run 的 RequirementArtifacts 动态识别豁免）。

// reviewerAgentTypes 是审查类代理的 agent_type：product-review、start-readiness、
// qa-review、qa-execution、carry 继承判定、各门审查。这些代理不得写代码或 run 状态。
var reviewerAgentTypes = map[string]bool{
	"product-review":   true,
	"start-readiness":  true,
	"qa-review":        true,
	"qa-execution":     true,
	"carry":            true,
	"gate-review":      true,
	"merge-review":     true,
	"complexity-gate":  true,
	"implementation-quality-gate": true,
}

// writeAllowedAgentTypes 是允许写代码/测试/用例文档的 agent_type：development-worker
// （写代码）、qa-design（白盒写测试、黑盒写用例文档）。
var writeAllowedAgentTypes = map[string]bool{
	"development-worker": true,
	"qa-design":          true,
}

// writeBlockPayload 是 hook 判定所需的最小结构化输入，由 decideWriteBlockValue 从
// PreToolUse 载荷提取。agentType 为空串表示主线程（主代理）。
type writeBlockPayload struct {
	filePath     string   // Edit/Write/MultiEdit 的目标路径
	agentType    string   // 调用者 agent_type；"" = 主线程
	hasActiveRun bool     // cwd（或其祖先）下存在活动正式 run
	artifacts    []string // 活动 run 的 RequirementArtifacts 路径（相对 repo 根）
	repoRoot     string   // 检测到活动 run 的仓库根
}

// hookToolName 从 payload 提取 PreToolUse 的 tool_name。Codex / Claude Code /
// Cursor 的 PreToolUse 载荷都以 tool_name 标识工具。
func hookToolName(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range []string{"tool_name", "toolName", "tool"} {
			if scalar := scalarHookString(object[name]); scalar != "" {
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookToolName(nested)
		}
	}
	return ""
}

// hookToolInputFilePath 从 tool_input 提取被编辑的文件路径。
func hookToolInputFilePath(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, container := range []string{"tool_input", "toolInput", "input", "params"} {
			if input, ok := object[container].(map[string]any); ok {
				for _, name := range []string{"file_path", "filePath", "path", "filename"} {
					if scalar := scalarHookString(input[name]); scalar != "" {
						return scalar
					}
				}
			}
		}
	}
	return ""
}

// hookAgentType 从 payload 提取调用者的 agent_type（subagent 身份）。主线程（无
// agent 身份字段）返回空串。
func hookAgentType(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range []string{"agent_type", "agentType", "agent_id", "agentId", "agent_name", "agentName"} {
			if scalar := scalarHookString(object[name]); scalar != "" {
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookAgentType(nested)
		}
	}
	return ""
}

// hookPayloadCwd 从 payload 提取工作目录（cwd），用于定位活动正式 run。
func hookPayloadCwd(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range []string{"cwd", "projectDir", "project_dir", "workspace_root"} {
			if scalar := scalarHookString(object[name]); scalar != "" {
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookPayloadCwd(nested)
		}
	}
	return ""
}

func scalarHookString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64, bool:
		return strings.TrimSpace(toString(typed))
	default:
		return ""
	}
}

func toString(value any) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(jsonString(value), `"`, ""), `\`, ""))
}

func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// isWriteTool reports whether a PreToolUse tool is a code/run-state write the
// guard must adjudicate: file-edit tools and Bash that commits or writes
// files. Read-only tools and non-write tools are not adjudicated here.
func isWriteTool(toolName, command string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write", "edit", "multiedit", "notebookedit", "multi_edit", "notebook_edit":
		return true
	case "bash", "shell", "cmd", "powershell", "pwsh":
		return commandWritesFiles(command)
	}
	return false
}

// commandWritesFiles reports whether a Bash/shell command writes to the VCS or to
// files (git commit 与写文件 Bash），从而纳入阻断判定。只读命令不命中：输出
// 重定向到 /dev/null（丢弃输出）与 2>&1（stderr 合并）等只读惯用法不视为写文件，由
// redirectWriteTargets 正确解析重定向目标、不靠朴素子串匹配（"2>&1 命中 2>"、
// "> /dev/null 命中 > " 的误判在此被修正）。
func commandWritesFiles(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	lower := strings.ToLower(command)
	// VCS 写入：提交 / 推送 / 合并 / 重置 变更仓库状态。
	for _, marker := range []string{"git commit", "git push", "git merge", "git rebase", "git reset --hard", "git checkout --", "git clean"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// run 状态直接写入：任何 .gates 下的写入路径。
	if strings.Contains(lower, ".gates") {
		return true
	}
	// 文件写入工具与文件系统变更命令（非重定向的写文件写法）。
	for _, marker := range []string{"tee ", "sed -i", "install ", "git add", "touch ", "mkdir ", "rm ", "mv ", "cp "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// 输出重定向到真实文件（写文件）：> file、>> file、2> file、&> file 等。只读惯用法
	// （> /dev/null、2>&1 等）由 redirectWriteTargets 排除、不命中。
	return len(redirectWriteTargets(command)) != 0
}

// redirectWriteTargets 解析命令中的输出重定向，返回真正写入文件的目标列表。只读惯用法
// 不命中：> /dev/null、> NUL（丢弃输出）、2>&1、>&2（stdout/stderr 合并）、2>&-（关闭
// fd）。解析跳过单/双引号字符串与反斜杠转义，避免把字符串参数里的 '>' 误判为重定向。
func redirectWriteTargets(command string) []string {
	var targets []string
	for i := 0; i < len(command); i++ {
		switch command[i] {
		case '\'':
			// 单引号字符串：跳到闭引号（循环的 i++ 落到闭引号之后）。
			if end := strings.IndexByte(command[i+1:], '\''); end < 0 {
				return targets
			} else {
				i += end + 1
			}
		case '"':
			// 双引号字符串：跳到未转义闭引号（循环的 i++ 落到闭引号之后）。
			i++
			for i < len(command) {
				if command[i] == '\\' && i+1 < len(command) {
					i += 2
					continue
				}
				if command[i] == '"' {
					break
				}
				i++
			}
		case '>':
			if target, next := redirectTarget(command, i); target != "" {
				if !redirectTargetReadOnly(target) {
					targets = append(targets, target)
				}
				i = next - 1 // 循环的 i++ 落到目标之后
			}
		}
	}
	return targets
}

// redirectTarget 从 command[i]（必须是 '>'）解析一个输出重定向，返回目标字符串与目标
// 之后的扫描位置。支持 >、>>、>|、2>、2>>、&>、&>> 等输出重定向；目标为 '>' 后到空白
// 或 shell 元字符（< > | & ; ( )）之间的连续字符，两端匹配的引号被剥掉。无真实目标
// （命令以 '>' 结尾、目标为空、后随 & 起头的 fd 合并 2>&1 / >&2 等）时返回 ("", i)。
func redirectTarget(command string, i int) (string, int) {
	if command[i] != '>' {
		return "", i
	}
	j := i + 1
	// 追加重定向 >> 与 noclobber >|。
	for j < len(command) && (command[j] == '>' || command[j] == '|') {
		j++
	}
	// 跳过目标前的空白。
	for j < len(command) && isShellSpace(command[j]) {
		j++
	}
	start := j
	for j < len(command) && !isShellSpace(command[j]) && !isShellMeta(command[j]) {
		j++
	}
	dest := strings.TrimSpace(command[start:j])
	// 剥掉两端匹配的引号（> "my file.txt"、> 'out.log'）。
	if len(dest) >= 2 && (dest[0] == '"' || dest[0] == '\'') && dest[len(dest)-1] == dest[0] {
		dest = dest[1 : len(dest)-1]
	}
	if dest == "" {
		return "", i
	}
	return dest, j
}

// redirectTargetReadOnly 报告重定向目标是否为只读惯用法（不写真实文件）：/dev/null 与
// Windows 的 NUL（丢弃输出）。&<fd> / &-（fd 合并 / 关闭）由 redirectTarget 直接判为无
// 目标（& 是 shell 元字符，目标读到它即终止），不在此重复处理。
func redirectTargetReadOnly(dest string) bool {
	lower := strings.ToLower(strings.TrimSpace(dest))
	return lower == "/dev/null" || lower == "nul"
}

// isShellSpace 报告字节是否为 shell 空白分隔符。
func isShellSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isShellMeta 报告字节是否为 shell 元字符：重定向 / 管道 / 后台 / 分号 / 括号。重定向
// 目标读到这些字符即终止（2>&1 的 & 因此不会进入目标）。
func isShellMeta(c byte) bool {
	switch c {
	case '<', '>', '|', '&', ';', '(', ')':
		return true
	}
	return false
}

// isFormalGatesCLICommand reports whether the command is a formal-gates CLI
// invocation（run 状态唯一合法写入者），此类命令放行。
func isFormalGatesCLICommand(command string) bool {
	tokens := splitCommand(command)
	for i := 0; i < len(tokens); i++ {
		if isFormalGatesExecutableToken(tokens[i]) {
			return true
		}
	}
	return false
}

// activeFormalRunArtifacts 在 cwd（或其祖先）下查找活动正式 run，返回其登记的需求/
// 设计文档路径（RequirementArtifacts，相对 repo 根）与仓库根。无活动 run 返回 ok=false。
func activeFormalRunArtifacts(cwd string) (repoRoot string, artifacts []string, ok bool) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil, false
	}
	root := cwd
	for depth := 0; depth < 16; depth++ {
		paths, err := filepath.Glob(filepath.Join(root, ".gates", "tmp", "*", "state.json"))
		if err == nil {
			for _, statePath := range paths {
				st, err := readHookRunStatus(statePath)
				if err != nil || st != "ACTIVE" {
					continue
				}
				paths := registeredHookArtifacts(statePath)
				return root, paths, true
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return "", nil, false
}

// readHookRunStatus 读取 run 状态文件的 status 字段（不触发完整性校验——hook 只做
// 粗粒度存在性判断，不替代 CLI 的严格加载）。
func readHookRunStatus(statePath string) (string, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return "", err
	}
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", err
	}
	return probe.Status, nil
}

// registeredHookArtifacts 读取 run 状态文件的 RequirementArtifacts 路径。
func registeredHookArtifacts(statePath string) []string {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var probe struct {
		RequirementArtifacts []RequirementArtifact `json:"requirementArtifacts"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil
	}
	paths := make([]string, 0, len(probe.RequirementArtifacts))
	for _, artifact := range probe.RequirementArtifacts {
		paths = append(paths, strings.TrimSpace(artifact.Path))
	}
	return paths
}

// decideWriteBlockValue 对 PreToolUse 载荷执行写阻断判定。返回
// (decision, true) 表示本载荷属于需要判定的代码/run 状态写入；返回
// (HookDecision{}, false) 表示非写入（只读）、formal-gates CLI 命令或不属于本判定范围，
// 交由既有 hook 逻辑处理。从载荷 cwd 定位活动 run；找不到活动 run 时放行（不干扰普通项目）。
func decideWriteBlockValue(decoded any) (HookDecision, bool) {
	toolName := hookToolName(decoded)
	if toolName == "" {
		return HookDecision{}, false
	}
	command := hookCommand(decoded)
	// formal-gates CLI 命令是 run 状态唯一合法写入者：既有的命令级校验（legacy 脚本、
	// PASS 绑定）继续负责，不在此阻断。
	if strings.TrimSpace(command) != "" && isFormalGatesCLICommand(command) {
		return HookDecision{}, false
	}
	if !isWriteTool(toolName, command) {
		return HookDecision{}, false
	}
	input := writeBlockPayload{
		filePath:  hookToolInputFilePath(decoded),
		agentType: hookAgentType(decoded),
	}
	repoRoot, artifacts, ok := activeFormalRunArtifacts(hookPayloadCwd(decoded))
	input.hasActiveRun = ok
	input.artifacts = artifacts
	input.repoRoot = repoRoot

	// 无活动正式 run：放行，不干扰普通项目。
	if !input.hasActiveRun {
		return allowHook("no active formal run; write allowed"), true
	}

	// 写代码/测试/用例文档的代理：development-worker、qa-design 放行。
	if writeAllowedAgentTypes[input.agentType] {
		return allowHook(input.agentType + " is allowed to write code and test/design documents"), true
	}

	// 主代理（无 agent 身份）：阻断范围是"代码与 run 状态"（用户 2026-08-09
	// 明确）。已登记需求/设计文档的编辑（需求更改流程的一部分）放行；非代码、非 run 状态
	// 文件（P2-BACKLOG.md 等文档）的写入放行；对代码与 run 状态的直接写入阻断。
	if input.agentType == "" {
		if input.filePath != "" && isRegisteredArtifact(input.filePath, input.repoRoot, input.artifacts) {
			return allowHook("main agent editing a registered requirement/design document"), true
		}
		if !mainAgentWriteBlocked(input, command) {
			return allowHook("main agent writing a non-code, non-run-state file"), true
		}
		return denyWrite("the main agent (main thread) must not write code or run state directly; dispatch development-worker, or edit a registered requirement/design document through the requirement change flow"), true
	}

	// 审查类代理：一律阻断直接写入。
	if reviewerAgentTypes[input.agentType] {
		return denyWrite(input.agentType + " is a reviewer-class agent and must not write code or run state directly"), true
	}

	// 其余代理不阻断。
	return allowHook(input.agentType + " is not a reviewer-class agent; write allowed"), true
}

func denyWrite(reason string) HookDecision {
	return HookDecision{
		Decision:                 "block",
		Reason:                   reason,
		Permission:               "deny",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}
}

// isRegisteredArtifact reports whether the edited file path (absolute, or
// relative to the repo root / cwd) matches one of the active run's registered
// RequirementArtifacts paths (relative to the repo root).
func isRegisteredArtifact(filePath, repoRoot string, artifacts []string) bool {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return false
	}
	filePath = filepath.Clean(filePath)
	// 把目标路径解析为相对 repo 根的形式：绝对路径转相对；相对路径若以 repo 根为 cwd
	// 则原样；否则按 repo 根连接后再转相对。
	if repoRoot != "" {
		if filepath.IsAbs(filePath) {
			if rel, err := filepath.Rel(repoRoot, filePath); err == nil {
				filePath = rel
			}
		} else if !isPathWithinRoot(filePath, repoRoot) {
			if abs := filepath.Join(repoRoot, filePath); filepath.IsAbs(abs) {
				filePath = filepath.Clean(abs)
			}
		}
	}
	filePath = filepath.ToSlash(filePath)
	for _, artifact := range artifacts {
		if filepath.ToSlash(strings.TrimSpace(artifact)) == filePath {
			return true
		}
	}
	return false
}

// isPathWithinRoot reports whether the path looks already repo-root-relative
// (does not escape upward with a leading ../ segment), used to avoid double-joining
// a path that is already relative to the repo root.
func isPathWithinRoot(path, repoRoot string) bool {
	cleaned := filepath.Clean(path)
	return cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// mainAgentWriteBlocked reports whether the main thread's write targets code or run
// state — the only writes blocks for the main agent（P2-2 处置，收窄阻断范围）。
// Edit/Write 按目标路径判定（isCodeOrRunStatePath）；Bash 按命令判定
// （bashWriteTargetsCodeOrState）。
func mainAgentWriteBlocked(input writeBlockPayload, command string) bool {
	if input.filePath != "" {
		return isCodeOrRunStatePath(input.filePath)
	}
	return bashWriteTargetsCodeOrState(command)
}

// codeFileExtensions 是常见代码 / 脚本 / 构建清单文件扩展名（小写）。 对主线程的
// 写阻断只覆盖"代码与 run 状态"：非代码、非状态文件（P2-BACKLOG.md、README 等文档）
// 放行。此判定是扩展名级通用启发式（千项目通用），不做项目静态路径白名单。
var codeFileExtensions = map[string]bool{
	// 源码
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".java": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".cs": true, ".rs": true, ".rb": true, ".php": true, ".swift": true, ".kt": true,
	".kts": true, ".scala": true, ".dart": true, ".lua": true, ".sql": true,
	".proto": true, ".m": true, ".mm": true, ".vue": true, ".svelte": true,
	// 脚本
	".sh": true, ".bash": true, ".zsh": true, ".ps1": true, ".bat": true, ".cmd": true,
	// 构建 / 清单（影响编译与产物）
	".mod": true, ".sum": true, ".toml": true, ".yaml": true, ".yml": true,
	".json": true, ".xml": true, ".lock": true, ".gradle": true, ".bzl": true,
	".mk": true, ".hcl": true, ".tf": true,
}

// isCodeOrRunStatePath reports whether a file path is code or run state — the only
// things blocks for the main thread. 非代码、非状态文件（文档 / 数据 / 配置）不
// 命中。run 状态指任何 .gates 路径；代码按扩展名识别。
func isCodeOrRunStatePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if lower == "" {
		return false
	}
	// run 状态：任何 .gates 路径。
	if strings.Contains(lower, ".gates") {
		return true
	}
	return codeFileExtensions[strings.ToLower(filepath.Ext(lower))]
}

// bashWriteTargetsCodeOrState reports whether a Bash write command targets code or
// run state — the only writes blocks for the main agent. git / VCS 状态写入
// （commit / push / merge / reset / checkout -- / clean / add）与 .gates（run 状态）一律
// 命中；输出重定向按目标路径判定（> notes.md 不命中、> main.go 命中）；其余文件变更
// 工具（tee / touch / mkdir / rm / mv / cp / sed -i / install）目标难以可靠解析，保守命中
// ——主线程在活动 run 下不应以 Bash 进行无法归类的文件变更。
func bashWriteTargetsCodeOrState(command string) bool {
	lower := strings.ToLower(command)
	// run 状态：任何 .gates 路径。
	if strings.Contains(lower, ".gates") {
		return true
	}
	// VCS 状态写入。
	for _, marker := range []string{"git commit", "git push", "git merge", "git rebase", "git reset --hard", "git checkout --", "git clean", "git add"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// 输出重定向到代码 / run 状态文件。
	for _, target := range redirectWriteTargets(command) {
		if isCodeOrRunStatePath(target) {
			return true
		}
	}
	// 其余文件变更工具：目标难以可靠解析，保守命中。
	for _, marker := range []string{"tee ", "sed -i", "install ", "touch ", "mkdir ", "rm ", "mv ", "cp "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
