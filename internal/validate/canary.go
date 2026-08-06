package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
var hostProviderEnvKeys = []string{"AI_AGENT", "CLAUDE_CODE_ENTRYPOINT", "CODEX_HOME", "CODEX_CLI_PATH", "CURSOR_TRACE_ID", "CURSOR_RUNTIME"}

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
	root := cleanRoot(options.Root)
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
		add("lightweight-workflow", runLightweightCanary(root, catalog))
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
	return report, result
}

func runLightweightCanary(packageRoot string, catalog PromptCatalog) error {
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
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "canary", Flow: "formal", RequirementSource: "requirement.md", VCS: "git"})
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
	state, err = RecordQADesign(root, packageRoot, state.RunID, dispatchID, []QACaseInput{{Mode: "whitebox", Description: "direct behavior", Procedure: "run the direct automated check", Oracle: "the check passes"}, {Mode: "blackbox", Description: "confirmed behavior", Procedure: "exercise the public command", Oracle: "the behavior is observed"}}, "")
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
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == kind && dispatch.Target == target && (dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED") {
			return id
		}
	}
	return ""
}

func PortableCanaryJSON(report PortableCanaryReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) {
	for _, tc := range []struct{ name, host string }{{"install-claude-codex-native-runtime", "both"}, {"install-cursor-native-runtime", "cursor"}} {
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
		if detail := installedScriptRuntimeDetail(report); detail != "" {
			addCheck(tc.name, false, detail)
			continue
		}
		addCheck(tc.name, true, "installed runtime uses native commands")
		rules, err := LoadManagedRules(root)
		if err != nil {
			addCheck(tc.name+"-managed-rule", false, err.Error())
			continue
		}
		if detail := installedManagedRuleDetail(report, rules[len(rules)-1]); detail != "" {
			addCheck(tc.name+"-managed-rule", false, detail)
			continue
		}
		uninstalled, err := Uninstall(UninstallOptions{Host: tc.host, Scope: "project", Project: project})
		if err != nil {
			addCheck(tc.name+"-uninstall", false, err.Error())
			continue
		}
		if detail := uninstalledInstallDetail(uninstalled, rules); detail != "" {
			addCheck(tc.name+"-uninstall", false, detail)
			continue
		}
		addCheck(tc.name+"-uninstall", true, "runtime, hooks, and managed rules cleaned")
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
			return fmt.Sprintf("managed rule count for %s is %d", target.ManagedRulePath, strings.Count(text, latest))
		}
	}
	return ""
}

func uninstalledInstallDetail(report UninstallReport, rules []string) string {
	for _, target := range report.Targets {
		if exists(target.TargetPath) {
			return "formal-gates runtime remains at " + target.TargetPath
		}
		if target.ManagedRulePath != "" && isFile(target.ManagedRulePath) {
			text, err := readText(target.ManagedRulePath)
			if err != nil {
				return err.Error()
			}
			for _, rule := range rules {
				if strings.Contains(text, rule) {
					return "managed rule remains in " + target.ManagedRulePath
				}
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
