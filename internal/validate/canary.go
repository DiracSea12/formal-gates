package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
)

type PortableCanaryOptions struct{ Root string }
type CanaryCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type PortableCanaryReport struct {
	SchemaVersion int           `json:"schemaVersion"`
	Root          string        `json:"root"`
	Checks        []CanaryCheck `json:"checks"`
}

// hostProviderEnvKeys are the environment variables the lifecycle host
// provider reads to detect the driving host. The portable canary is a
// host-agnostic check: it must observe the lenient default provider even when
// invoked from inside a real host shell (e.g. `canary portable` from a Claude
// Code session), so the host environment is neutralized for its duration.
var hostProviderEnvKeys = []string{"AI_AGENT", "CLAUDE_CODE_ENTRYPOINT", "CODEX_HOME", "CODEX_CLI_PATH", "CURSOR_TRACE_ID", "CURSOR_RUNTIME", "DSH_HOME", "DSH_PROJECT_DIR"}

// withoutHostEnv clears the host lifecycle environment and returns a restore
// function. Empty is treated as unset by providerFromEnvironment, so clearing
// and restoring with empty values is behavior-preserving.
func withoutHostEnv() func() {
	prior := map[string]string{}
	for _, key := range hostProviderEnvKeys {
		prior[key] = os.Getenv(key)
		os.Setenv(key, "")
	}
	return func() {
		for key, value := range prior {
			os.Setenv(key, value)
		}
	}
}

func PortableCanary(options PortableCanaryOptions) (PortableCanaryReport, Result) {
	restore := withoutHostEnv()
	defer restore()
	root := lifecycle.CleanRoot(options.Root)
	report := PortableCanaryReport{SchemaVersion: 1, Root: slash(absPath(root))}
	var result Result
	add := func(name string, err error) {
		status, detail := "PASS", ""
		if err != nil {
			status, detail = "FAIL", err.Error()
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	packageResult := Package(root)
	if packageResult.OK() {
		add("package-validate", nil)
	} else {
		add("package-validate", fmt.Errorf("%s", resultSummary(packageResult)))
	}
	catalog, err := LoadPromptCatalog(root)
	add("prompt-catalog", err)
	decision, err := Hook([]byte(`{"command":"pwsh -File ./scripts/gate-workflow.ps1"}`))
	if err == nil && decision.PermissionDecision != "deny" {
		err = fmt.Errorf("legacy command was allowed")
	}
	add("hook-blocks-legacy-command", err)
	if len(catalog.Gates) > 0 {
		add("quick-e2e-workflow", runQuickE2ECanary(root, catalog))
	}
	tempRoot, err := os.MkdirTemp("", "formal-gates-install-canary-")
	if err != nil {
		add("install", err)
	} else {
		defer os.RemoveAll(tempRoot)
		addInstallChecks(root, tempRoot, func(name string, ok bool, detail string) {
			if ok {
				add(name, nil)
			} else {
				add(name, fmt.Errorf("%s", detail))
			}
		})
	}
	// 写阻断 canary：在有活动正式 run 的仓库根上，验证主线程阻断、development-worker
	// 放行、审查类代理阻断、登记文档豁免、无 run 放行的判定矩阵。
	writeBlockRoot, err := os.MkdirTemp("", "formal-gates-writeblock-canary-")
	if err != nil {
		add("write-block-hook", err)
	} else {
		defer os.RemoveAll(writeBlockRoot)
		add("write-block-hook", runWriteBlockCanary(writeBlockRoot))
	}
	return report, result
}

// runQuickE2ECanary exercises the quick end-to-end formal workflow (full route)
// against a temp git repo, from start through requirement registration, slicing,
// route confirmation, QA design/review/execution, snapshot, gate review and Seal.
func runQuickE2ECanary(packageRoot string, catalog PromptCatalog) error {
	root, err := os.MkdirTemp("", "formal-gates-workflow-canary-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	requirement := filepath.Join(root, "requirement.md")
	if err := os.WriteFile(requirement, []byte("confirmed behavior\n"), 0o600); err != nil {
		return err
	}
	if err := initializeCanaryGit(root); err != nil {
		return err
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "canary", Flow: "formal", RequirementSource: "requirement.md", VCS: "git", Split: "no"})
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "requirements-clarification", "", false, ""); err != nil {
		return err
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		return err
	}
	state, err = RecordAction(root, packageRoot, state.RunID, "requirements-clarification", openDispatchID(state, "action", "requirements-clarification"), "PASS", "", nil, false, "")
	if err != nil {
		return err
	}
	state, err = UpdateRequirement(root, packageRoot, state.RunID, "", true, "", nil)
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "product-review", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID := openDispatchID(state, "action", "product-review")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-product-review")
	if err != nil {
		return err
	}
	state, err = RecordAction(root, packageRoot, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "start-readiness", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "start-readiness")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-start-readiness")
	if err != nil {
		return err
	}
	state, err = RecordAction(root, packageRoot, state.RunID, "start-readiness", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		return err
	}
	// 拆分决定在 Part 2（start-readiness）完成后记录；此处走快速路径（不拆），
	// 记录"建议不拆（原因）"后确认路线。
	state, err = RecordSlicing(root, packageRoot, state.RunID, "no-split", 0, nil, "", "single coherent bounded unit; no split needed", "")
	if err != nil {
		return err
	}
	state, err = SetRoute(root, packageRoot, state.RunID, "full", nil)
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-design", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-design")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-qa-design")
	if err != nil {
		return err
	}
	// 白盒设计者交付的结构测试代码——canary 仓库带一个测试文件，作为白盒用例测试
	// 引用的定位目标（<文件>::<函数>）；CLI 记录时只校验引用非空/1:1，存在性由 qa-review
	// 读代码核对、qa-execution 实际运行验证。
	if err := os.WriteFile(filepath.Join(root, "whitebox_delivered_test.go"), []byte(whiteboxDeliveredTestCode), 0o600); err != nil {
		return err
	}
	state, err = RecordQADesign(root, packageRoot, state.RunID, dispatchID, []QACaseInput{{Mode: "whitebox", Description: "direct behavior", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, {Mode: "blackbox", Description: "confirmed behavior", Procedure: "exercise the public command", Oracle: "the behavior is observed"}}, "")
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-review", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-review")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-qa-reviewer")
	if err != nil {
		return err
	}
	state, err = RecordQAReview(root, packageRoot, state.RunID, dispatchID, []QAReviewInput{{CaseID: "CASE-001", Outcome: "PASS"}, {CaseID: "CASE-002", Outcome: "PASS"}}, "", nil)
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "development-worker", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "development-worker")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-development")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "delivery.txt"), []byte("delivery\n"), 0o600); err != nil {
		return err
	}
	if err := commitCanaryGit(root, "delivery"); err != nil {
		return err
	}
	state, err = AdvanceSnapshot(root, packageRoot, state.RunID, dispatchID, false, "")
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-execution", "", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-execution")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-qa-execution")
	if err != nil {
		return err
	}
	state, err = RecordQAExecution(root, packageRoot, state.RunID, dispatchID, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "ran direct check", Observation: "passed", OracleResult: "matched"}, {CaseID: "CASE-002", Outcome: "PASS", Procedure: "exercised public command", Observation: "observed", OracleResult: "matched"}}, "")
	if err != nil {
		return err
	}
	for index, gate := range catalog.Gates {
		// 合并门是条件自动门：只在分片 >= 2 的保留总任务实例自动附加，不进入正常
		// 路线的门审循环。
		if gate.ID == mergeGateID {
			continue
		}
		if _, err := PrepareGate(root, packageRoot, state.RunID, gate.ID, false, ""); err != nil {
			return err
		}
		state, _ = LoadRunState(root, state.RunID)
		dispatchID = openDispatchID(state, "gate", gate.ID)
		state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, fmt.Sprintf("canary-gate-reviewer-%d", index+1))
		if err != nil {
			return err
		}
		state, err = RecordGate(root, packageRoot, state.RunID, gate.ID, dispatchID, "PASS", "", comparedRange(state), nil)
		if err != nil {
			return err
		}
	}
	_, err = Seal(root, packageRoot, state.RunID, nil, false, "")
	return err
}

func initializeCanaryGit(root string) error {
	runner := execNativeCommandRunner{}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "formal-gates@example.invalid"}, {"config", "user.name", "Formal Gates Canary"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			return err
		}
	}
	// 与实际仓库 / 测试 fixture 一致地忽略运行期临时状态：否则 .gates/tmp/ 会被快照
	// 就绪脏检查（检测未跟踪且未忽略文件）拦下。
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".gates/tmp/\n"), 0o600); err != nil {
		return err
	}
	return commitCanaryGit(root, "base")
}

func commitCanaryGit(root, message string) error {
	runner := execNativeCommandRunner{}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		return err
	}
	_, err := runner.Run(root, "git", "commit", "-m", message)
	return err
}

func openDispatchID(state RunState, kind, target string) string {
	// prepare 不再作废旧派发，同功能旧 CLAIMED 派发可能仍在途，且同功能可能同时
	// 存在多张 OPEN 空票；取新派发必须取 Attempt 最大的 OPEN 票（最新准备），无 OPEN 票时
	// 才回退到 CLAIMED（在途旧票）。
	bestID := ""
	bestAttempt := 0
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != kind || dispatch.Target != target || dispatch.Status != "OPEN" {
			continue
		}
		if dispatch.Attempt >= bestAttempt {
			bestID, bestAttempt = id, dispatch.Attempt
		}
	}
	if bestID != "" {
		return bestID
	}
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == kind && dispatch.Target == target && dispatch.Status == "CLAIMED" {
			return id
		}
	}
	return ""
}

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) {
	for _, tc := range []struct{ name, host string }{{"install-claude-codex-native-runtime", "both"}, {"install-cursor-native-runtime", "cursor"}, {"install-dsh-project-runtime", "dsh"}} {
		project := filepath.Join(tempRoot, tc.name)
		if err := os.MkdirAll(project, 0o700); err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		report, err := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, Force: true})
		if err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		for _, target := range report.Targets {
			installedBinary := filepath.Join(target.TargetPath, "bin", nativeBinaryName())
			if output, smokeErr := exec.Command(installedBinary, "--version").CombinedOutput(); smokeErr != nil {
				addCheck(tc.name+"-installed-binary-smoke", false, fmt.Sprintf("%s: %v (%s)", installedBinary, smokeErr, strings.TrimSpace(string(output))))
				continue
			}
		}
		if report.Registry == "" || !isFile(filepath.FromSlash(report.Registry)) {
			addCheck(tc.name+"-registry-admission", false, "install did not persist a registry admission bridge receipt")
			continue
		}
		if detail := installedScriptRuntimeDetail(report); detail != "" {
			addCheck(tc.name, false, detail)
			continue
		}
		addCheck(tc.name, true, "installed runtime uses native commands")
		rule, err := LoadManagedRule(root)
		if err != nil {
			addCheck(tc.name+"-host-instructions", false, err.Error())
			continue
		}
		if detail := installedManagedRuleDetail(report, rule); detail != "" {
			addCheck(tc.name+"-host-instructions", false, detail)
			continue
		}
		uninstalled, err := Uninstall(UninstallOptions{Host: tc.host, Scope: "project", Project: project})
		if err != nil {
			addCheck(tc.name+"-uninstall", false, err.Error())
			continue
		}
		if detail := uninstalledInstallDetail(uninstalled); detail != "" {
			addCheck(tc.name+"-uninstall", false, detail)
			continue
		}
		addCheck(tc.name+"-uninstall", true, "runtime, hooks, and host instruction blocks cleaned")
	}
}

func installedScriptRuntimeDetail(report InstallReport) string {
	for _, target := range report.Targets {
		var found []string
		err := filepath.WalkDir(target.TargetPath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if isScriptRuntimeExtension(entry.Name()) {
				found = append(found, slash(path))
			}
			return nil
		})
		if err != nil {
			return err.Error()
		}
		if len(found) > 0 {
			return "installed script runtime files: " + strings.Join(found, ", ")
		}
		if strings.TrimSpace(target.HookConfig) != "" {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			lower := strings.ToLower(text)
			for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash"} {
				if strings.Contains(lower, marker) {
					return "hook config contains script runtime marker " + marker
				}
			}
		}
	}
	return ""
}

func installedManagedRuleDetail(report InstallReport, latest string) string {
	for _, target := range report.Targets {
		if strings.TrimSpace(target.ManagedRulePath) == "" {
			continue
		}
		text, err := readText(target.ManagedRulePath)
		if err != nil {
			return err.Error()
		}
		if strings.Count(text, latest) != 1 {
			return fmt.Sprintf("host instruction rule count for %s is %d", target.ManagedRulePath, strings.Count(text, latest))
		}
		if strings.Count(text, hostInstructionsStartMarker) != 1 || strings.Count(text, hostInstructionsEndMarker) != 1 {
			return "host instruction markers did not converge in " + target.ManagedRulePath
		}
	}
	return ""
}

func uninstalledInstallDetail(report UninstallReport) string {
	for _, target := range report.Targets {
		if exists(target.TargetPath) {
			return "formal-gates runtime remains at " + target.TargetPath
		}
		if target.ManagedRulePath != "" && isFile(target.ManagedRulePath) {
			text, err := readText(target.ManagedRulePath)
			if err != nil {
				return err.Error()
			}
			if strings.Contains(text, hostInstructionsStartMarker) || strings.Contains(text, hostInstructionsEndMarker) {
				return "host instruction markers remain in " + target.ManagedRulePath
			}
		}
		if target.HookConfig != "" && isFile(target.HookConfig) {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			if strings.Contains(strings.ToLower(text), "formal-gates") {
				return "installer-owned hook remains in " + target.HookConfig
			}
		}
	}
	return ""
}
func isScriptRuntimeExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}
func resultSummary(result Result) string {
	messages := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		messages = append(messages, failure.Path+": "+failure.Message)
	}
	return strings.Join(messages, "; ")
}

// runWriteBlockCanary verifies the write-block hook decision matrix against
// a temp repo that carries an active formal run in development: the main thread (no agent
// identity) and reviewer-class agents are blocked from direct code/run-state
// writes; development-worker and qa-design are allowed; the main agent editing a
// registered requirement/design document is allowed; and with no active run the
// same write is allowed. It drives the real Hook entry so the canary exercises
// the same decision the host would receive.
func runWriteBlockCanary(root string) error {
	// 一个活动 run 的状态文件：status ACTIVE、登记 requirements.md 与 design.md。
	runDir := filepath.Join(root, ".gates", "tmp", "write-block-canary")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	state := map[string]any{
		"status": "ACTIVE",
		"runId":  "write-block-canary",
		"flow":   "formal",
		"actions": map[string]any{
			"development-worker": map[string]any{"status": developmentPrepared},
		},
		"gates":              map[string]any{},
		"carry":              map[string]any{},
		"dispatches":         map[string]any{},
		"skipAuthorizations": map[string]any{},
		"selectedGates":      []string{},
		"requirementArtifacts": []map[string]string{
			{"path": "requirements.md", "revision": "r1"},
			{"path": "design.md", "revision": "r2"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	cwdPayload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.Join(root, "internal", "code.go"))
	// 1. 主线程（无 agent 身份）写代码 → 阻断。
	if decision, err := Hook([]byte(cwdPayload)); err != nil || decision.PermissionDecision != "deny" {
		return fmt.Errorf("main-thread code write was not blocked: %#v %v", decision, err)
	}
	// 2. development-worker 写代码 → 放行。
	devPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code.go")) + `"},"agent_type":"development-worker"}`
	if decision, err := Hook([]byte(devPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("development-worker write was not allowed: %#v %v", decision, err)
	}
	// 3. qa-design 写测试代码 → 放行。
	qaDesignPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code_test.go")) + `"},"agent_type":"qa-design"}`
	if decision, err := Hook([]byte(qaDesignPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("qa-design write was not allowed: %#v %v", decision, err)
	}
	// 4. 审查类代理（product-review）写代码 → 阻断。
	reviewerPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code.go")) + `"},"agent_type":"product-review"}`
	if decision, err := Hook([]byte(reviewerPayload)); err != nil || decision.PermissionDecision != "deny" {
		return fmt.Errorf("reviewer-class write was not blocked: %#v %v", decision, err)
	}
	// 5. 主线程编辑已登记需求文档 → 放行（需求更改流程）。
	docPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "requirements.md")) + `"}}`
	if decision, err := Hook([]byte(docPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("main-agent registered-doc edit was not allowed: %#v %v", decision, err)
	}
	// 6. 无活动 run 的普通仓库：主线程写代码 → 放行（不干扰普通项目）。普通仓库必须放在
	// 活动 run 仓库根之外，否则向上查找会命中 root 下的活动 run。
	plainRoot, err := os.MkdirTemp("", "formal-gates-plain-repo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(plainRoot)
	plainPayload := `{"cwd":` + string(mustJSONString(plainRoot)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(plainRoot, "main.go")) + `"}}`
	if decision, err := Hook([]byte(plainPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("write without an active run was blocked: %#v %v", decision, err)
	}
	// 7. cwd 仍在活动 run 仓库内，但目标明确位于仓库外：当前 run 的写墙不得扩张成
	// 全局文件锁，另一个目录/窗口的写入应放行。
	outsidePath := filepath.Join(plainRoot, "outside.go")
	outsidePayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":` + string(mustJSONString(outsidePath)) + `}}`
	if decision, err := Hook([]byte(outsidePayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("write outside the active run root was blocked: %#v %v", decision, err)
	}
	return nil
}

func mustJSONString(value string) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
