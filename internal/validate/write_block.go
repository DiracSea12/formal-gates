package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 主代理与审查类代理写阻断（PreToolUse hook）。
//
// 正式流程进入开发阶段后，主代理（主线程，payload 无 agent_id/agent_type）与全部审查类
// 代理不得写代码或直接改 run 状态；run 状态只能经 CLI 写入，代码与执行文件只能经
// development-worker 派发写入。判定按调用者身份（agent_type 匹配）、不按文件路径
// （无静态文件白名单，千项目通用）。只有活动正式 run 的 development-worker 状态离开
// PENDING（即进入 PREPARED / REPAIR_PREPARED / PASS / VERIFIED）后才生效；开发前与无活动
// run 均放行，不妨碍产品审、技术审及其文档修订。Seal / Abort 把 run 置为非 ACTIVE 后
// 立即解除，即使终态临时文件因收尾重试而暂时保留，也不会造成永久阻断。
//
// 阻断：主代理与审查类代理对代码与 run 状态的直接写入（Edit/Write/MultiEdit、
// git commit、写文件 Bash）。
// 放行：formal-gates CLI 命令（run 状态唯一合法写入者）与只读命令；development-worker
// （写代码）；qa-design（白盒设计者写测试代码、黑盒设计者写用例文档）；主代理对已登记
// 需求/设计文档的编辑（按活动 run 的 RequirementArtifacts 动态识别豁免）。

// reviewerAgentTypes 是审查类代理的 agent_type：product-review、start-readiness、
// qa-review、qa-execution、carry 继承判定、各门审查。这些代理不得写代码或 run 状态。
var reviewerAgentTypes = map[string]bool{
	"product-review":              true,
	"start-readiness":             true,
	"qa-review":                   true,
	"qa-execution":                true,
	"carry":                       true,
	"gate-review":                 true,
	"merge-review":                true,
	"complexity-gate":             true,
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
	filePath     string        // Edit/Write/MultiEdit 的目标路径
	agentType    string        // 调用者 agent_type；"" = 主线程
	hasActiveRun bool          // cwd（或其祖先）下存在活动正式 run
	artifacts    []string      // 活动 run 的 RequirementArtifacts 路径（相对 repo 根）
	repoRoot     string        // 检测到活动 run 的仓库根
	cwd          string        // host 窗口工作目录，用于解析相对写目标
	owners       []ownerIdentity // 所有活动 run 的 owner（启动对话 transcript/session）
	toolName     string        // PreToolUse 工具名，用于 apply_patch 这类无目标写工具的保守拦截
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
	case "write", "edit", "multiedit", "notebookedit", "multi_edit", "notebook_edit",
		"applypatch", "apply_patch":
		return true
	}
	// 其余工具（bash/shell/exec 或任何未知工具名）：只要载荷里能提取到 command，就按
	// shell 命令判定写目标，避免漏掉未知工具名导致写墙失效。
	if strings.TrimSpace(command) != "" {
		return commandWritesFiles(command)
	}
	return false
}

// vcsWriteMarkers 是写墙要拦截的 VCS 状态写入命令关键词：git / svn / p4 三套后端
// 的提交、推送、合并、重置、丢弃、暂存、删除/移动/复制等会变更仓库或工作区状态的
// 命令。formal-gates 声明支持这三种 VCS，写墙必须对它们一视同仁，不能只拦 git 而
// 漏掉 svn / p4。
var vcsWriteMarkers = []string{
	// git
	"git commit", "git push", "git merge", "git rebase", "git reset --hard",
	"git checkout --", "git clean", "git add",
	// svn
	"svn commit", "svn ci", "svn add", "svn delete", "svn del", "svn remove",
	"svn rm", "svn move", "svn mv", "svn rename", "svn copy", "svn cp",
	"svn merge", "svn import", "svn revert", "svn propset", "svn propedit",
	"svn propdel",
	// p4
	"p4 submit", "p4 add", "p4 edit", "p4 delete", "p4 del", "p4 move",
	"p4 mv", "p4 copy", "p4 integrate", "p4 integ", "p4 revert", "p4 shelve",
	"p4 reconcile",
}

// isVCSWriteCommand reports whether a command carries a VCS state-write marker
// (git / svn / p4 的提交类或变更工作区状态的命令)。
func isVCSWriteCommand(command string) bool {
	lower := strings.ToLower(command)
	for _, marker := range vcsWriteMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
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
	// VCS 状态写入（git / svn / p4）：提交 / 推送 / 合并 / 重置 / 丢弃 / 暂存等。
	if isVCSWriteCommand(command) {
		return true
	}
	// 按真实写目标判定（改动 3）：不再以命令文本是否含 .gates 子串判写入——只读命令
	// （grep/ls/cat/find/python3 读、只读 git 查询等）即使提到 .gates 也放行；真写 .gates
	// 由下面的文件变更工具与输出重定向按目标路径判定命中。
	// 文件写入工具与文件系统变更命令（非重定向的写文件写法）。
	for _, marker := range []string{"tee ", "sed -i", "install ", "touch ", "mkdir ", "rm ", "mv ", "cp "} {
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

// activeDevelopmentRunArtifacts 在 cwd（或其祖先）下查找已经进入开发阶段的活动正式
// run，返回这些 run 登记的需求/设计文档路径（RequirementArtifacts，相对 repo 根）、仓库
// 根与 owner 身份。只有 ACTIVE 且 development-worker 已离开 PENDING 的 run 才启用写阻断；
// 产品审、技术审等开发前阶段返回 ok=false。
func activeDevelopmentRunArtifacts(cwd string) (repoRoot string, artifacts []string, owners []ownerIdentity, ok bool) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil, nil, false
	}
	root := cwd
	for depth := 0; depth < 16; depth++ {
		paths, err := filepath.Glob(filepath.Join(root, ".gates", "tmp", "*", "state.json"))
		if err == nil {
			for _, statePath := range paths {
				active, started, err := readHookRunWritePhase(statePath)
				if err != nil || !active || !started {
					continue
				}
				artifacts = append(artifacts, registeredHookArtifacts(statePath)...)
				t, s := readRunOwner(statePath)
				owners = append(owners, ownerIdentity{transcript: t, session: s})
				ok = true
			}
			if ok {
				return root, artifacts, owners, true
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return "", nil, nil, false
}

// readHookRunWritePhase 读取 hook 判定所需的最小阶段字段（不触发完整性校验——hook 只做
// 粗粒度阶段判断，不替代 CLI 的严格加载）。development-worker 的 PREPARED 是开发开始
// 边界，与 workflow_transition.go 的 developmentStarted 保持一致。
func readHookRunWritePhase(statePath string) (active bool, started bool, err error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return false, false, err
	}
	var probe struct {
		Status  string                  `json:"status"`
		Actions map[string]ActionResult `json:"actions"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, false, err
	}
	developmentStatus := strings.TrimSpace(probe.Actions["development-worker"].Status)
	return probe.Status == "ACTIVE", developmentStatus != "" && developmentStatus != developmentPending, nil
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
// 交由既有 hook 逻辑处理。从载荷 cwd 定位已进入开发阶段的活动 run；找不到时放行（包括
// 产品审、技术审等开发前阶段与无活动 run，不干扰文档修订或普通项目）。
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
		cwd:       hookPayloadCwd(decoded),
		toolName:  toolName,
	}
	repoRoot, artifacts, owners, ok := activeDevelopmentRunArtifacts(input.cwd)
	input.hasActiveRun = ok
	input.artifacts = artifacts
	input.repoRoot = repoRoot
	input.owners = owners

	// 尚未进入开发阶段或无活动正式 run：放行，不干扰开发前文档修订或普通项目。
	if !input.hasActiveRun {
		return allowHook("no active formal run has entered development; write allowed"), true
	}
	// 写墙只保护承载当前活动 run 的仓库根。一个窗口即使 cwd 位于该仓库内，也不得让
	// 这里的活动 run 扩张成全局文件锁：Edit/Write 的明确仓库外目标，以及能完整解析且
	// 全部位于仓库外的简单 Bash 写目标，直接放行。无法可靠解析的复合 Bash 写入仍按原
	// 边界保守处理，避免把同时修改仓库内外的命令误判为仓库外写入。
	if writeTargetsOutsideActiveRoot(input, command) {
		return allowHook("write target is outside the active formal run root"), true
	}

	// 写代码/测试/用例文档的代理：development-worker、qa-design 放行。
	if writeAllowedAgentTypes[input.agentType] {
		return allowHook(input.agentType + " is allowed to write code and test/design documents"), true
	}

	// 主代理（无 agent 身份）：阻断范围是"代码与 run 状态"（用户 2026-08-09
	// 明确）。已登记需求/设计文档的编辑（需求更改流程的一部分）放行；非代码、非 run 状态
	// 文件（P2-BACKLOG.md 等文档）的写入放行；对代码与 run 状态的直接写入阻断。
	if input.agentType == "" {
		// owner 作用域收窄：只有启动本 run 的对话才拦，其它对话直接放行。owner 未知时
		// known=false，保守走下面的既有拦截。
		payloadTranscript, payloadSession := hookOwnerIdentity(decoded)
		if known, same := sameOwnerIdentity(input.owners, payloadTranscript, payloadSession); known && !same {
			return allowHook("different conversation from the run owner; write allowed"), true
		}
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

// writeTargetsOutsideActiveRoot reports whether every confidently resolved write
// target is outside the repository root that owns the active run. It returns true
// only when scope is unambiguous; unknown/compound writes remain in the normal
// role decision path.
func writeTargetsOutsideActiveRoot(input writeBlockPayload, command string) bool {
	if input.repoRoot == "" {
		return false
	}
	if input.filePath != "" {
		inside, known := writePathWithinRoot(input.filePath, input.cwd, input.repoRoot)
		return known && !inside
	}
	targets, complete := explicitBashWriteTargets(command)
	if !complete || len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		inside, known := writePathWithinRoot(target, input.cwd, input.repoRoot)
		if !known || inside {
			return false
		}
	}
	return true
}

// normalizeHostPath 在 Windows 上把 POSIX 风格绝对路径（/c/Users/...）归一化成盘符
// 路径（C:/Users/...）：filepath.IsAbs 在 Windows 上不认 /c/ 前缀，会把它当相对路径
// 误判成仓库内。非 Windows 或不是 POSIX 盘符形态则原样返回。
func normalizeHostPath(path string) string {
	if runtime.GOOS != "windows" || len(path) < 3 || path[0] != '/' || path[2] != '/' {
		return path
	}
	if c := path[1]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return string(c) + ":" + path[2:]
	}
	return path
}

// writePathWithinRoot resolves a tool/shell path against the host cwd and checks
// containment in repoRoot. Dynamic shell paths are deliberately unknown rather
// than guessed; the project boundary only promises common normal operations.
func writePathWithinRoot(path, cwd, repoRoot string) (inside bool, known bool) {
	path = strings.Trim(strings.TrimSpace(path), `"'`)
	if path == "" || strings.ContainsAny(path, "$`*?[]{}") {
		return false, false
	}
	path = normalizeHostPath(path)
	rootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return false, false
	}
	target := path
	if !filepath.IsAbs(target) {
		base := strings.TrimSpace(cwd)
		if base == "" {
			base = rootAbs
		}
		target = filepath.Join(base, target)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, false
	}
	// Windows 跨卷路径（例如 C:\repo 下写 D:\outside）无法用 filepath.Rel 比较，
	// 但卷名不同即可确定目标在活动仓库根之外；不要把明确的仓库外写入保守成阻断。
	rootVolume, targetVolume := filepath.VolumeName(rootAbs), filepath.VolumeName(targetAbs)
	if rootVolume != "" && targetVolume != "" && !strings.EqualFold(rootVolume, targetVolume) {
		return false, true
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(targetAbs))
	if err != nil {
		return false, false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), true
}

// redirectOperatorStart returns the index of the '>' that begins a shell output
// redirection in a token, or -1 if the token is not a redirection. It skips an
// optional fd prefix (digits or &), so ">", ">>", "2>", "2>/dev/null", "&>file"
// are detected, while ordinary path tokens like "out.go" are not.
func redirectOperatorStart(token string) int {
	i := 0
	if i < len(token) && token[i] == '&' {
		i++
	} else {
		for i < len(token) && token[i] >= '0' && token[i] <= '9' {
			i++
		}
	}
	if i < len(token) && token[i] == '>' {
		return i
	}
	return -1
}

// explicitBashWriteTargets extracts targets only for the simple, common shell
// writes whose destination semantics are unambiguous. VCS writes, sed -i, shell
// pipelines/conditionals and other compound forms return complete=false and keep
// the existing conservative block behavior.
func explicitBashWriteTargets(command string) ([]string, bool) {
	lower := strings.ToLower(command)
	// VCS 状态写入（git / svn / p4）与 sed -i 目标无法可靠解析，保留保守行为。
	if isVCSWriteCommand(command) || strings.Contains(lower, "sed -i") {
		return nil, false
	}
	if strings.Contains(command, "&&") || strings.Contains(command, "||") || strings.ContainsAny(command, ";|") {
		return nil, false
	}
	targets := redirectWriteTargets(command)
	tokens := splitCommand(command)
	for index, token := range tokens {
		name := strings.ToLower(filepath.Base(token))
		if name != "tee" && name != "touch" && name != "mkdir" && name != "rm" && name != "mv" && name != "cp" && name != "install" {
			continue
		}
		var args []string
		skipNextTarget := false
		for _, candidate := range tokens[index+1:] {
			candidate = strings.TrimSpace(candidate)
			if skipNextTarget {
				skipNextTarget = false
				continue
			}
			if candidate == "" || strings.HasPrefix(candidate, "-") {
				continue
			}
			if op := redirectOperatorStart(candidate); op >= 0 {
				// 跳过重定向算符；目标粘连在本 token（2>/dev/null、>file）时整个 token 跳过，
				// 纯算符（> / >> / 2>）时还要跳过下一个 token（分离的重定向目标）。
				j := op
				for j < len(candidate) && candidate[j] == '>' {
					j++
				}
				if j >= len(candidate) {
					skipNextTarget = true
				}
				continue
			}
			args = append(args, candidate)
		}
		if len(args) == 0 {
			return nil, false
		}
		switch name {
		case "cp", "install":
			targets = append(targets, args[len(args)-1])
		case "mv":
			// mv removes its sources as well as writing its destination, so every
			// operand is a write target for containment purposes.
			targets = append(targets, args...)
		default:
			targets = append(targets, args...)
		}
		return targets, true
	}
	return targets, len(targets) != 0
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
	// apply_patch 这类补丁写工具：写代码但目标文件不可从载荷提取（无 file_path/command），
	// 保守拦截，避免被误判成「非代码文件」放行。
	if input.toolName == "apply_patch" || input.toolName == "applypatch" {
		return true
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
// run state — the only writes blocks for the main agent. git / svn / p4 的 VCS 状态
// 写入（提交 / 推送 / 合并 / 重置 / 丢弃 / 暂存等）一律命中；输出重定向按目标路径
// 判定（> notes.md 不命中、> main.go 命中）；文件变更工具（tee / touch / mkdir /
// rm / mv / cp / sed -i / install）按位置参数的真实目标判定，只在目标指向代码或
// run 状态（.gates / 代码扩展名）时命中——npm install -g（写到项目外）、
// touch notes.md（非代码）不再被保守误拦。
func bashWriteTargetsCodeOrState(command string) bool {
	// VCS 状态写入（git / svn / p4）。
	if isVCSWriteCommand(command) {
		return true
	}
	// 输出重定向到代码 / run 状态文件。
	for _, target := range redirectWriteTargets(command) {
		if isCodeOrRunStatePath(target) {
			return true
		}
	}
	// 文件变更工具按真实目标判定。
	return fileChangeTargetsCodeOrRunState(command)
}

// fileChangeTargetsCodeOrRunState reports whether a file-change command (tee /
// touch / mkdir / rm / mv / cp / sed -i / install) carries a positional path
// argument naming a code or run-state path. 只检查非 flag 的位置参数（跳过 -x /
// --x），按 isCodeOrRunStatePath 判定；命令首 token 不是这些工具时返回 false——npm /
// pip / go install 等首 token 是包管理器，不是文件变更工具，直接放行。
func fileChangeTargetsCodeOrRunState(command string) bool {
	tokens := splitCommand(command)
	for index, token := range tokens {
		// 用 filepath.Base 归一，兼容全路径（/usr/bin/cp）与带前缀（sudo/env/command cp）
		// 的写法，避免只认裸首 token 而漏拦。
		switch strings.ToLower(filepath.Base(token)) {
		case "tee", "touch", "mkdir", "rm", "mv", "cp", "install", "sed":
		default:
			continue
		}
		for _, arg := range tokens[index+1:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if isCodeOrRunStatePath(arg) {
				return true
			}
		}
		return false
	}
	return false
}
