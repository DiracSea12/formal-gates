package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"formal-gates/internal/atomicfile"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	engineRuntime "formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

// The auxiliary write wall reads current engine issuance, owner identity and
// confirmed document paths. It does not admit events or decide workflow state.
// Development/QA authors may edit their work; owners and reviewers remain read-only.

// reviewerAgentTypes 是审查类代理的 agent_type：product-review、start-readiness、
// qa-review、qa-execution、各门审查。这些代理不得写代码或 run 状态。
var reviewerAgentTypes = map[string]bool{
	"product-review":              true,
	"start-readiness":             true,
	"qa-review":                   true,
	"qa-execution":                true,
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
	filePath     string          // Edit/Write/MultiEdit 的目标路径
	agentType    string          // 调用者 agent_type；"" = 主线程
	hasActiveRun bool            // cwd（或其祖先）下存在活动正式 run
	artifacts    []string        // 活动 run 的 IntakeConfirmationReceipt.Artifacts 路径
	repoRoot     string          // 检测到活动 run 的仓库根
	cwd          string          // host 窗口工作目录，用于解析相对写目标
	owners       []ownerIdentity // 所有活动 run 的 owner（启动对话 transcript/session）
	toolName     string          // PreToolUse 工具名，用于 apply_patch 这类无目标写工具的保守拦截
}

// hookToolName 从 payload 提取 PreToolUse 的 tool_name。Codex / Claude Code /
// Cursor 的 PreToolUse 载荷都以 tool_name 标识工具。
func hookToolName(value any, providers ...string) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range []string{"tool_name", "toolName", "tool"} {
			if scalar := scalarHookString(object[name]); scalar != "" {
				if len(providers) > 0 && strings.HasPrefix(scalar, "mcp__") {
					if descriptor, err := host.Lookup(providers[0]); err == nil && descriptor.Hook.Protocol == host.ProtocolCodex {
						if _, tool, ok := strings.Cut(strings.TrimPrefix(scalar, "mcp__"), "__"); ok {
							return tool
						}
					}
				}
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookToolName(nested, providers...)
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

// hookPayloadCwd 优先使用 payload 目录；缺省时读取平台声明的项目目录环境变量。
func hookPayloadCwd(value any, providers ...string) string {
	if object, ok := value.(map[string]any); ok {
		for _, name := range []string{"cwd", "projectDir", "project_dir", "workspace_root"} {
			if scalar := scalarHookString(object[name]); scalar != "" {
				return scalar
			}
		}
		if nested, ok := object["value"].(map[string]any); ok {
			return hookPayloadCwd(nested, providers...)
		}
	}
	if len(providers) > 0 {
		if descriptor, err := host.Lookup(providers[0]); err == nil && descriptor.Hook.ProjectDirEnv != "" {
			return strings.TrimSpace(os.Getenv(descriptor.Hook.ProjectDirEnv))
		}
	}
	return ""
}

func scalarHookString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64, bool:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(data)
	default:
		return ""
	}
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
	for _, segment := range commandSegments(command) {
		tokens := executableCommandTokens(segment)
		if len(tokens) == 0 {
			continue
		}
		tokens[0] = filepath.Base(tokens[0])
		for _, marker := range vcsWriteMarkers {
			words := strings.Fields(marker)
			if len(tokens) >= len(words) && slices.EqualFunc(tokens[:len(words)], words, strings.EqualFold) {
				return true
			}
		}
	}
	return false
}

// commandWritesFiles reports writes to the VCS or files. Discarded output and
// descriptor redirection do not count as file writes.
func commandWritesFiles(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	// VCS 状态写入（git / svn / p4）：提交 / 推送 / 合并 / 重置 / 丢弃 / 暂存等。
	if isVCSWriteCommand(command) {
		return true
	}
	// 文件写入工具与文件系统变更命令（非重定向的写文件写法）。
	for _, segment := range commandSegments(command) {
		tokens := executableCommandTokens(segment)
		if len(tokens) == 0 {
			continue
		}
		switch strings.ToLower(filepath.Base(tokens[0])) {
		case "tee", "install", "touch", "mkdir", "rm", "mv", "cp":
			return true
		case "sed":
			if len(tokens) > 1 && strings.HasPrefix(tokens[1], "-i") {
				return true
			}
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
// 之后的扫描位置。支持 >、>>、>|、2>、2>>、&>、&>> 等输出重定向；目标到未被引号包围
// 的空白或 shell 元字符（< > | & ; ( )）为止，引号内空白保留，由共享 splitCommand 解码。无真实目标
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
	var quote byte
	for j < len(command) {
		if quote != 0 {
			if command[j] == quote {
				quote = 0
			}
		} else if command[j] == '\'' || command[j] == '"' {
			quote = command[j]
		} else if isShellSpace(command[j]) || isShellMeta(command[j]) {
			break
		}
		j++
	}
	dest := strings.Join(splitCommand(command[start:j]), "")
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

// activeDevelopmentRunArtifacts projects active engine work at cwd or an ancestor.
// Development scope comes from the current confirmation's issued batch bindings.
func activeDevelopmentRunArtifacts(cwd string) (repoRoot string, artifacts []string, owners []ownerIdentity, ok bool) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil, nil, false
	}
	root := cwd
	for {
		paths, err := filepath.Glob(filepath.Join(root, ".gates", "engine", "*", "state.json"))
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

// hookBoundAgentType resolves a subagent id to its formal-gates dispatch target
// using the lifecycle claim bindings under an active, development-started run.
// DSH (and any host that cannot stamp agent_type into PreToolUse) uses this to
// preserve the write wall's role decision for reviewer/development workers.
func hookBoundAgentType(repoRoot, identity string) string {
	identity = strings.TrimSpace(identity)
	if repoRoot == "" || identity == "" {
		return ""
	}
	statePaths, _ := filepath.Glob(filepath.Join(repoRoot, ".gates", "engine", "*", "state.json"))
	for _, statePath := range statePaths {
		active, started, err := readHookRunWritePhase(statePath)
		if err != nil || !active || !started {
			continue
		}
		state, err := readHookEngineState(statePath)
		if err != nil {
			continue
		}
		for action, receipt := range state.SpawnReceipts {
			if receipt.Correlation == identity || receipt.LifecycleIdentity == identity {
				if target := hookDispatchTarget(statePath, action); target != "" {
					return target
				}
			}
		}
		bindingPaths, _ := filepath.Glob(filepath.Join(repoRoot, ".gates", "tmp", state.RunID, "lifecycle", "*.json"))
		for _, bindingPath := range bindingPaths {
			data, err := os.ReadFile(bindingPath)
			if err != nil {
				continue
			}
			var binding struct {
				DispatchID string `json:"dispatchId"`
				Identity   string `json:"identity"`
			}
			if err := json.Unmarshal(data, &binding); err != nil {
				continue
			}
			if strings.TrimSpace(binding.Identity) != identity {
				continue
			}
			if target := hookDispatchTarget(statePath, binding.DispatchID); target != "" {
				return target
			}
		}
	}
	return ""
}

// readHookEngineState reads the current engine envelope for auxiliary scope.
// Core loaders and Submit retain full integrity and business admission authority.
func readHookEngineState(statePath string) (*protocol.State, error) {
	data, err := atomicfile.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Writer                    string         `json:"writer"`
		StateSchemaVersion        string         `json:"stateSchemaVersion"`
		WorkflowDefinitionVersion string         `json:"workflowDefinitionVersion"`
		DefinitionSource          string         `json:"definitionSource"`
		DefinitionDigest          string         `json:"definitionDigest"`
		Content                   protocol.State `json:"content"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Writer != "engine" || doc.StateSchemaVersion != encoder.StateSchemaVersion || doc.WorkflowDefinitionVersion != definition.WorkflowDefinitionVersion || doc.DefinitionSource != "definitions/workflow.json" || doc.DefinitionDigest != definition.WorkflowDefinitionDigest {
		return nil, fmt.Errorf("UNSUPPORTED_RUN_VERSION: hook scope requires current engine state")
	}
	return &doc.Content, nil
}

func hookDispatchTarget(statePath, dispatchID string) string {
	state, err := readHookEngineState(statePath)
	if err != nil {
		return ""
	}
	receipt, known := state.SpawnReceipts[dispatchID]
	if !known {
		return ""
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return ""
	}
	for _, step := range cd.Steps {
		if step.Header.ID != receipt.Task.Step {
			continue
		}
		agent, ok := step.Payload.(compiler.CompiledAgentStep)
		if !ok {
			return ""
		}
		switch string(agent.Handler) {
		case "engine.development.batch.v6", "engine.repair.v7", "engine.merge.resolve.v3":
			return "development-worker"
		case "engine.qa.design.v6":
			return "qa-design"
		default:
			return "gate-review"
		}
	}
	return ""
}

func readHookRunWritePhase(statePath string) (active bool, started bool, err error) {
	state, err := readHookEngineState(statePath)
	if err != nil {
		return false, false, err
	}
	return state.Phase != engineRuntime.PhaseTerminal, state.DevelopmentStarted(), nil
}

func registeredHookArtifacts(statePath string) []string {
	state, err := readHookEngineState(statePath)
	if err != nil || state.IntakeConfirmationReceipt == nil {
		return nil
	}
	paths := []string{}
	for _, artifact := range state.IntakeConfirmationReceipt.Artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

// decideWriteBlockValue 对 PreToolUse 载荷执行写阻断判定。返回
// (decision, true) 表示本载荷属于需要判定的代码/run 状态写入；返回
// (HookDecision{}, false) 表示非 shell/文件写入或不属于本判定范围，
// 交由既有 hook 逻辑处理。从宿主工作目录定位已进入开发阶段的活动 run；找不到时放行（包括
// 产品审、技术审等开发前阶段与无活动 run，不干扰文档修订或普通项目）。
func decideWriteBlockValue(decoded any, providers ...string) (HookDecision, bool) {
	toolName := hookToolName(decoded, providers...)
	if toolName == "" {
		return HookDecision{}, false
	}
	if boundHostWorker(decoded, providers...) {
		if managedHostWorkerTool(decoded) {
			return allowHook("issued host worker uses its managed tools"), true
		}
		return denyWrite("issued host workers must use formal-gates worker tools; native tools bypass the issued workspace and edit checks"), true
	}
	command := hookCommand(decoded)
	if !isWriteTool(toolName, command) {
		return HookDecision{}, false
	}
	input := writeBlockPayload{
		filePath:  hookToolInputFilePath(decoded),
		agentType: hookAgentType(decoded),
		cwd:       hookPayloadCwd(decoded, providers...),
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

	// DeepSeek Harness 的子代理载荷只有 agent id，没有 agent_type；host 也无法在
	// subagent 事件中注入类型。这里用 lifecycle claim 绑定（identity → dispatch id）
	// 反查派发目标，把 unknown id 归一化成 development-worker/qa-design/各审查类型。
	// 已经自带 agent_type 的宿主（Claude Code/Codex/Cursor）也会命中同一绑定并得到
	// 相同目标，不会改变既有判定。
	if input.agentType != "" {
		if bound := hookBoundAgentType(input.repoRoot, input.agentType); bound != "" {
			input.agentType = bound
		}
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

	// 主代理只阻断代码与 run 状态写入；已登记需求/设计文档及其他文档放行。
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

// The registry reference only locates original evidence across host and task
// working directories. The native turn, issued material and engine receipt
// establish whether this caller is a managed worker.
func boundHostWorker(value any, providers ...string) bool {
	if len(providers) == 0 {
		return false
	}
	descriptor, err := host.Lookup(providers[0])
	if err != nil || descriptor.ID != host.Codex {
		return false
	}
	transcript, _ := hookOwnerIdentity(value)
	turnID := hookPayloadField(value, "turn_id", "turnId")
	if transcript == "" || turnID == "" {
		return false
	}
	thread, err := host.ReadCodexThread(transcript)
	if err != nil || !thread.SourceSubagent {
		return false
	}
	registry, err := LoadRegistry(installRegistryPath(InstallOptions{}))
	if err != nil {
		return false
	}
	for _, record := range registry.Records {
		provider, err := host.Lookup(record.Host)
		if err != nil || provider.ID != descriptor.ID || record.Status != "active" || record.StateRoot == "" {
			continue
		}
		var pointer struct {
			Job string `json:"job"`
		}
		body, err := os.ReadFile(filepath.Join(record.StateRoot, "host-workers", thread.ID+".json"))
		if err != nil || json.Unmarshal(body, &pointer) != nil || !filepath.IsAbs(pointer.Job) {
			continue
		}
		if hostWorkerEvidenceMatches(pointer.Job, thread, turnID) {
			return true
		}
	}
	return false
}

func hostWorkerEvidenceMatches(job string, thread host.CodexThread, turnID string) bool {
	var request struct {
		Provider   string                `json:"provider"`
		Host       host.CodexInvocation  `json:"host"`
		Controller *host.CodexInvocation `json:"controller"`
		Engine     struct {
			Root           string                `json:"root"`
			RunID          string                `json:"runId"`
			WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
			ResumeActionID string                `json:"resumeActionId"`
		} `json:"engine"`
	}
	body, err := os.ReadFile(filepath.Join(job, "native-request.json"))
	if err != nil || json.Unmarshal(body, &request) != nil || request.Provider != "codex" {
		return false
	}
	ref := request.Host
	if ref.Session != thread.ID || ref.Parent != thread.ParentThreadID || ref.Turn != turnID || canonicalRegistryPath(ref.Transcript) != canonicalRegistryPath(thread.Path) {
		return false
	}
	_, turn, err := ref.Observe()
	if err != nil || turn.State != "running" {
		return false
	}
	materialRef := request.Engine.WorkerMaterial
	if canonicalRegistryPath(facade.WorkerExecutionDirectory(materialRef, request.Engine.ResumeActionID)) != canonicalRegistryPath(job) {
		return false
	}
	statePath := filepath.Join(request.Engine.Root, ".gates", "engine", request.Engine.RunID, "state.json")
	if canonicalRegistryPath(filepath.Dir(filepath.Dir(materialRef.Path))) != canonicalRegistryPath(filepath.Dir(statePath)) {
		return false
	}
	state, err := readHookEngineState(statePath)
	if err != nil || state.Phase == engineRuntime.PhaseTerminal || state.RunID != request.Engine.RunID {
		return false
	}
	parent := state.OwnerSession
	if request.Controller != nil {
		if _, _, err := request.Controller.Observe(); err != nil {
			return false
		}
		parent = request.Controller.Session
	}
	if parent == "" || parent != ref.Parent {
		return false
	}
	receipt, issued := state.SpawnReceipts[materialRef.ActionID]
	pending, active := state.PendingActions[materialRef.ActionID]
	if !issued || !active || pending.AttemptID != materialRef.AttemptID || receipt.ActionID != materialRef.ActionID || receipt.Task != pending.Task || receipt.Correlation != ref.Session || receipt.Provider != request.Provider || receipt.Status != protocol.SpawnStatusSpawned && receipt.Status != protocol.SpawnStatusAttached {
		return false
	}
	body, err = os.ReadFile(materialRef.Path)
	if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(body)) != materialRef.Digest {
		return false
	}
	var material worker.Material
	return json.Unmarshal(body, &material) == nil && material.Input.RunID == state.RunID && material.Input.Pending == pending
}

func managedHostWorkerTool(value any) bool {
	name := hookToolName(value)
	if name == "functions.exec" || name == "functions.wait" {
		return true
	}
	for _, prefix := range []string{"mcp__formal_gates__", "mcp__formal-gates__"} {
		if tool, found := strings.CutPrefix(name, prefix); found {
			return tool == "worker" || tool == "workspace" || tool == "submit_semantics" || tool == "confirm_control" || tool == "confirm_native"
		}
	}
	command := strings.TrimSpace(hookCommand(value))
	if strings.HasPrefix(command, "&") && !strings.HasPrefix(command, "&&") {
		command = strings.TrimSpace(command[1:])
	}
	if len(commandSegments(command)) != 1 || len(redirectWriteTargets(command)) != 0 {
		return false
	}
	tokens := executableCommandTokens(command)
	return len(tokens) >= 3 && isFormalGatesExecutableToken(tokens[0]) && tokens[1] == "workflow" && tokens[2] == "worker"
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
	// VCS 状态写入（git / svn / p4）与 sed -i 目标无法可靠解析，保留保守行为。
	tokens := executableCommandTokens(command)
	if isVCSWriteCommand(command) || len(tokens) > 1 && strings.EqualFold(filepath.Base(tokens[0]), "sed") && strings.HasPrefix(tokens[1], "-i") {
		return nil, false
	}
	if strings.Contains(command, "&&") || strings.Contains(command, "||") || strings.ContainsAny(command, ";|") {
		return nil, false
	}
	targets := redirectWriteTargets(command)
	if len(tokens) == 0 {
		return targets, len(targets) != 0
	}
	name := strings.ToLower(filepath.Base(tokens[0]))
	switch name {
	case "tee", "touch", "mkdir", "rm", "mv", "cp", "install":
	default:
		return targets, len(targets) != 0
	}
	var args []string
	skipNextTarget := false
	for _, candidate := range tokens[1:] {
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
// IntakeConfirmationReceipt.Artifacts paths (relative to the repo root).
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
		} else if !isPathWithinRoot(filePath) {
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
func isPathWithinRoot(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// mainAgentWriteBlocked reports whether the main thread writes code or run state.
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
// 写阻断只覆盖"代码与 run 状态"：非代码、非状态文件（README 等文档）
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
// things blocked for the main thread. Non-code documents do not match; source,
// script, build/config extensions and .gates paths follow the shared heuristic.
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
// touch notes.md（非代码）不命中。
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

// fileChangeTargetsCodeOrRunState checks each actual command's write targets.
// cp/install use destinations; mv includes removed sources. In-place sed keeps
// its positional target check, while read-only transforms are not writes.
func fileChangeTargetsCodeOrRunState(command string) bool {
	for _, segment := range commandSegments(command) {
		targets, _ := explicitBashWriteTargets(segment)
		tokens := executableCommandTokens(segment)
		if len(tokens) > 1 && strings.EqualFold(filepath.Base(tokens[0]), "sed") && strings.HasPrefix(tokens[1], "-i") {
			targets = tokens[2:]
		}
		for _, target := range targets {
			if isCodeOrRunStatePath(target) {
				return true
			}
		}
	}
	return false
}

// commandSegments keeps quoted arguments intact while separating native shell
// command boundaries. Each segment retains the original command bytes.
func commandSegments(command string) []string {
	segments := []string{}
	start := 0
	var quote byte
	for index := 0; index < len(command); index++ {
		char := command[index]
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
		} else if char == ';' || char == '|' || char == '&' || char == '\n' {
			segments = append(segments, command[start:index])
			start = index + 1
		}
	}
	return append(segments, command[start:])
}

// executableCommandTokens uses the shared quote parser and the existing native
// execution prefixes; arguments after the executable remain arguments.
func executableCommandTokens(command string) []string {
	tokens := splitCommand(command)
	for len(tokens) != 0 {
		prefix := strings.ToLower(filepath.Base(tokens[0]))
		switch prefix {
		case "sudo", "env", "command":
			tokens = tokens[1:]
		default:
			return tokens
		}
		for len(tokens) != 0 && (strings.HasPrefix(tokens[0], "-") || strings.Contains(tokens[0], "=")) {
			option := tokens[0]
			tokens = tokens[1:]
			if prefix == "sudo" && option == "-u" && len(tokens) != 0 {
				tokens = tokens[1:]
			}
		}
	}
	return tokens
}
