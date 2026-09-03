package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

type PortableCanaryOptions struct{ Root string }
type CanaryCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type PortableCanaryReport struct {
	SchemaVersion  int                     `json:"schemaVersion"`
	Root           string                  `json:"root"`
	VCSIdentity    string                  `json:"vcsIdentity,omitempty"`
	PackageDigest  string                  `json:"packageDigest,omitempty"`
	CanonicalPaths map[string]string       `json:"canonicalPaths,omitempty"`
	DigestBinding  map[string]string       `json:"digestBinding,omitempty"`
	Installed      []CanaryInstallIdentity `json:"installed,omitempty"`
	Checks         []CanaryCheck           `json:"checks"`
}

type CanaryInstallIdentity struct {
	Host             string            `json:"host"`
	Target           string            `json:"target"`
	ReleaseRoot      string            `json:"releaseRoot,omitempty"`
	VCSIdentity      string            `json:"vcsIdentity"`
	PackageDigest    string            `json:"packageDigest"`
	InstalledDigest  string            `json:"installedDigest"`
	CanonicalPaths   map[string]string `json:"canonicalPaths"`
	Disjoint         map[string]string `json:"disjoint"`
	Registry         string            `json:"registry,omitempty"`
	RegistryRecordID string            `json:"registryRecordId,omitempty"`
	RegistryEpoch    uint64            `json:"registryEpoch,omitempty"`
}

type InstallFaultMatrixOptions struct {
	Root    string
	Fixture string
}

type InstallFaultMatrixReport = PortableCanaryReport

// InstallFaultMatrix is the public, deterministic fixture for the native
// install transaction. Each named boundary injects one failure through the
// same process-wide hook used by the real installer, then verifies that the
// target, release root, launcher and transaction journal have no partial
// committed state. The fixture is read-only with respect to the source root.
func InstallFaultMatrix(options InstallFaultMatrixOptions) (InstallFaultMatrixReport, Result) {
	root := lifecycle.CleanRoot(options.Root)
	report := InstallFaultMatrixReport{SchemaVersion: 1, Root: slash(absPath(root))}
	if receipt, receiptErr := PackageReceipt(root); receiptErr == nil {
		report.PackageDigest = receipt.Digest
		report.VCSIdentity = sourceVCSIdentity(root, receipt.Digest)
		report.CanonicalPaths = map[string]string{"root": canonicalRegistryPath(root)}
		report.DigestBinding = map[string]string{"package": receipt.Digest}
	}
	var result Result
	add := func(name string, err error) {
		status, detail := "PASS", ""
		if err != nil {
			status, detail = "FAIL", err.Error()
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	phases := []string{"journal-boundary", "intent", "registry", "copy-component:runtime", "prepared", "switched", "post-switch-smoke", "pointer", "hook", "managed-rule", "registry-commit", "copy-component:prompts", "copy-component:gates", "verify-stage:installed-target", "verify-stage:manifest", "verify-stage:realpath", "verify-stage:digest"}
	if fixture := strings.TrimSpace(options.Fixture); fixture != "" {
		phases = []string{canonicalInstallFaultPhase(fixture)}
	}
	for _, phase := range phases {
		phase := phase
		fixture, err := os.MkdirTemp("", "formal-gates-fault-matrix-")
		if err != nil {
			add("fault-"+phase, err)
			continue
		}
		project := filepath.Join(fixture, "project")
		release := filepath.Join(fixture, "release")
		launcher := filepath.Join(fixture, "stable", nativeBinaryName())
		registry := filepath.Join(fixture, "registry.json")
		faultPrior, hadFault := os.LookupEnv("FORMAL_GATES_INSTALL_FAULT")
		_ = os.Setenv("FORMAL_GATES_INSTALL_FAULT", phase)
		_, installErr := Install(InstallOptions{Source: root, Host: "codex", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true})
		if hadFault {
			_ = os.Setenv("FORMAL_GATES_INSTALL_FAULT", faultPrior)
		} else {
			_ = os.Unsetenv("FORMAL_GATES_INSTALL_FAULT")
		}
		checkErr := installErr
		if checkErr == nil {
			checkErr = fmt.Errorf("fault %s unexpectedly succeeded", phase)
		} else if !strings.Contains(checkErr.Error(), "deterministic install fault injected") {
			// A fixture that fails before the requested boundary is still a
			// failure of the matrix: the named injection must be observable.
			checkErr = fmt.Errorf("fault %s did not reach its injection boundary: %w", phase, checkErr)
		} else {
			checkErr = nil
			target := filepath.Join(project, ".codex", "skills", "formal-gates")
			if exists(target) || exists(release) || exists(launcher) {
				checkErr = fmt.Errorf("fault %s left committed target/release/launcher state", phase)
			} else if matches, globErr := filepath.Glob(registry + ".transaction.json*"); globErr != nil {
				checkErr = globErr
			} else if len(matches) == 0 {
				checkErr = fmt.Errorf("fault %s did not leave an independent recovery receipt", phase)
			} else if strings.HasPrefix(phase, "copy-component:") {
				data, readErr := os.ReadFile(registry + ".transaction.json.failure.json")
				var receipt installRecoveryReceipt
				if readErr != nil {
					checkErr = readErr
				} else if json.Unmarshal(data, &receipt) != nil {
					checkErr = fmt.Errorf("fault %s left an unreadable recovery receipt", phase)
				} else if !receipt.Prepared || !receipt.PartialCopy || len(receipt.CopiedComponents) == 0 {
					checkErr = fmt.Errorf("fault %s did not record prepared partial-copy evidence: %+v", phase, receipt)
				}
			}
		}
		add("fault-"+phase, checkErr)
		_ = os.RemoveAll(fixture)
	}
	return report, result
}

// withoutHostEnv clears the host lifecycle environment and returns a restore
// function. Empty is treated as unset by providerFromEnvironment, so clearing
// and restoring with empty values is behavior-preserving.
func withoutHostEnv() func() {
	prior := map[string]string{}
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
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
	if receipt, receiptErr := PackageReceipt(root); receiptErr == nil {
		report.PackageDigest = receipt.Digest
		report.DigestBinding = map[string]string{"package": receipt.Digest}
	}
	report.VCSIdentity = sourceVCSIdentity(root, report.PackageDigest)
	report.CanonicalPaths = map[string]string{"root": canonicalRegistryPath(root)}
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
	// An installed candidate invokes this command with a private marker.  It
	// must exercise the same regression path without recursively starting a
	// second install-canary from inside itself.
	if os.Getenv("FORMAL_GATES_INSTALLED_CANARY") == "1" {
		if err == nil {
			packageRoot := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_PACKAGE_ROOT"))
			workflowRoot := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_WORKFLOW_ROOT"))
			registry := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_REGISTRY"))
			recordID := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_RECORD"))
			if packageRoot == "" || workflowRoot == "" || registry == "" || recordID == "" {
				add("installed-binary-regression", fmt.Errorf("installed canary is missing its package/workflow/registry binding"))
			} else if installedCatalog, loadErr := LoadPromptCatalog(packageRoot); loadErr != nil {
				add("installed-binary-regression", loadErr)
			} else {
				add("installed-binary-regression", runQuickE2ECanaryAt(packageRoot, installedCatalog, workflowRoot, registry, recordID))
			}
		}
		return report, result
	}
	// A source/candidate binary is read-only with respect to workflow state.
	// Unit/integration test executables may exercise the legacy in-process path;
	// shipped canaries run the same workflow later through each installed stable
	// launcher with an exact registry binding in addInstallChecks.
	executable, _ := os.Executable()
	base := filepath.Base(filepath.Clean(executable))
	if len(catalog.Gates) > 0 && (strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe")) {
		add("quick-e2e-workflow", runQuickE2ECanary(root, catalog))
	}
	tempRoot, err := os.MkdirTemp("", "formal-gates-install-canary-")
	if err != nil {
		add("install", err)
	} else {
		defer os.RemoveAll(tempRoot)
		identities := addInstallChecks(root, tempRoot, func(name string, ok bool, detail string) {
			if ok {
				add(name, nil)
			} else {
				add(name, fmt.Errorf("%s", detail))
			}
		})
		report.Installed = append(report.Installed, identities...)
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
	return runQuickE2ECanaryAt(packageRoot, catalog, "", "", "")
}

// runQuickE2ECanaryAt accepts an installed target's exact admission binding.
// Source-tree canaries leave workflowRoot empty and retain the legacy phase-0
// path; installed-binary evidence must supply all three registry coordinates.
func runQuickE2ECanaryAt(packageRoot string, catalog PromptCatalog, workflowRoot, registry, recordID string) error {
	root := strings.TrimSpace(workflowRoot)
	removeRoot := false
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "formal-gates-workflow-canary-")
		if err != nil {
			return err
		}
		removeRoot = true
	}
	if removeRoot {
		defer os.RemoveAll(root)
	}
	requirement := filepath.Join(root, "requirement.md")
	if err := os.WriteFile(requirement, []byte("# Canary requirement\n\n## 需求点\n\n### REQ-001：Confirmed behavior\n\n#### 要求\n\nThe installed workflow must complete its verified route.\n\n#### 验收条件\n\n- AC-001：The installed workflow reaches Seal through the public runtime.\n\n#### 来源\n\nPortable canary contract.\n"), 0o600); err != nil {
		return err
	}
	if err := initializeCanaryGit(root); err != nil {
		return err
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "canary", Flow: "formal", RequirementSource: "requirement.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: recordID})
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
	state, err = UpdateRequirement(root, packageRoot, state.RunID, "", true, "", nil, RequirementUpdateOptions{ActivateGuarantee: true})
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
	state, err = RecordAction(root, packageRoot, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "", ReviewItemInput{Key: guaranteeProductReviewKey("REQ-001"), Status: "PASS"})
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
	// The full route owns separate blackbox and whitebox review authorities. Keep
	// the canary on those public per-mode paths so an explicitly active guarantee
	// proves that both review kinds contribute approved REQ/AC evidence.
	qaWorktree, err := os.MkdirTemp("", "formal-gates-canary-qa-")
	if err != nil {
		return err
	}
	if err := os.Remove(qaWorktree); err != nil {
		return err
	}
	runner := execNativeCommandRunner{}
	if _, err := runner.Run(root, "git", "worktree", "add", "--detach", qaWorktree, state.BaseSnapshot); err != nil {
		return err
	}
	defer func() { _, _ = runner.Run(root, "git", "worktree", "remove", "--force", qaWorktree) }()
	state, err = RegisterQAWorktree(root, packageRoot, state.RunID, qaWorktree)
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-design", "blackbox", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-design")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-blackbox-design")
	if err != nil {
		return err
	}
	state, err = RecordQADesign(root, packageRoot, state.RunID, dispatchID, []QACaseInput{{Mode: "blackbox", Description: "confirmed behavior", Procedure: "exercise the public command", Oracle: "the behavior is observed", AcceptanceCriteria: []string{"AC-001"}}}, "")
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-review", "blackbox", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-review")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-blackbox-reviewer")
	if err != nil {
		return err
	}
	state, err = RecordQAReview(root, packageRoot, state.RunID, dispatchID, []QAReviewInput{{CaseID: "CASE-001", Outcome: "PASS"}}, "", nil, QAReviewRecordOptions{SourceDecisions: []string{"REQ-001=PASS"}, PointDecisions: []string{"AC-001=PASS"}, CaseDecisions: []string{"CASE-001=PASS"}})
	if err != nil {
		return err
	}

	// The whitebox designer contributes a test locator in the delivery snapshot;
	// review and execution later bind that case to the same confirmed AC.
	if err := os.WriteFile(filepath.Join(root, "whitebox_delivered_test.go"), []byte(whiteboxDeliveredTestCode), 0o600); err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-design", "whitebox", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-design")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-whitebox-design")
	if err != nil {
		return err
	}
	state, err = RecordQADesign(root, packageRoot, state.RunID, dispatchID, []QACaseInput{{Mode: "whitebox", Description: "direct behavior", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior", AcceptanceCriteria: []string{"AC-001"}}}, "")
	if err != nil {
		return err
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-review", "whitebox", false, ""); err != nil {
		return err
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "qa-review")
	state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-whitebox-reviewer")
	if err != nil {
		return err
	}
	state, err = RecordQAReview(root, packageRoot, state.RunID, dispatchID, []QAReviewInput{{CaseID: "CASE-002", Outcome: "PASS"}}, "", nil, QAReviewRecordOptions{SourceDecisions: []string{"REQ-001=PASS"}, PointDecisions: []string{"AC-001=PASS"}, CaseDecisions: []string{"CASE-002=PASS"}})
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
		status, _ := (execNativeCommandRunner{}).Run(root, "git", "status", "--porcelain=v1", "--untracked-files=all")
		return fmt.Errorf("%w (git status: %s)", err, strings.TrimSpace(status))
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		if _, err := PrepareAction(root, packageRoot, state.RunID, "qa-execution", mode, false, ""); err != nil {
			return err
		}
		state, _ = LoadRunState(root, state.RunID)
		dispatchID = openDispatchID(state, "action", "qa-execution")
		state, err = ClaimDispatch(root, packageRoot, state.RunID, dispatchID, "canary-"+mode+"-execution")
		if err != nil {
			return err
		}
		results := make([]QAResultInput, 0, len(state.qaModeCases(mode)))
		for _, testCase := range state.qaModeCases(mode) {
			results = append(results, QAResultInput{CaseID: testCase.ID, Outcome: "PASS", Procedure: "ran the approved " + mode + " check", Observation: "passed", OracleResult: "matched"})
		}
		state, err = RecordQAExecution(root, packageRoot, state.RunID, dispatchID, results, "")
		if err != nil {
			return err
		}
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

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) []CanaryInstallIdentity {
	var identities []CanaryInstallIdentity
	canaryHome := filepath.Join(tempRoot, "stable-home")
	canaryLocalAppData := filepath.Join(tempRoot, "stable-localappdata")
	registry := filepath.Join(canaryHome, ".formal-gates", "registry.json")
	launcher := filepath.Join(canaryHome, ".local", "bin", nativeBinaryName())
	if runtime.GOOS == "windows" {
		launcher = filepath.Join(canaryLocalAppData, "formal-gates", "bin", nativeBinaryName())
	}
	stableEnv := canaryEnvironment(canaryHome, canaryLocalAppData)
	for _, hostName := range host.InstallableNames() {
		tc := struct{ name, host string }{name: "install-" + hostName + "-native-runtime", host: hostName}
		project := filepath.Join(tempRoot, tc.name)
		if err := os.MkdirAll(project, 0o700); err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		releaseRoot := filepath.Join(project, "stable-release")
		report, err := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, ReleaseRoot: releaseRoot, BinaryTarget: launcher, RegistryPath: registry, Force: true})
		if err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		for _, target := range report.Targets {
			identities = append(identities, CanaryInstallIdentity{Host: target.Host, Target: target.TargetPath, ReleaseRoot: target.ReleaseRoot, VCSIdentity: target.VCSIdentity, PackageDigest: target.PackageDigest, InstalledDigest: target.InstalledDigest, CanonicalPaths: target.CanonicalPaths, Disjoint: target.Disjoint, Registry: report.Registry, RegistryRecordID: target.RegistryRecordID, RegistryEpoch: report.RegistryEpoch})
			installedBinary := filepath.Join(target.TargetPath, "bin", nativeBinaryName())
			if output, smokeErr := exec.Command(installedBinary, "--version").CombinedOutput(); smokeErr != nil {
				addCheck(tc.name+"-installed-binary-smoke", false, fmt.Sprintf("%s: %v (%s)", installedBinary, smokeErr, strings.TrimSpace(string(output))))
				continue
			}
		}
		// The source checkout is only the install input.  Re-run the package
		// regression and portable canary through the executable copied to the
		// stable launcher path so a passing in-process source test cannot stand
		// in for candidate evidence.
		installedTarget := filepath.FromSlash(report.Targets[0].TargetPath)
		regressionCommand := exec.Command(launcher, "package", "validate", "--root", installedTarget)
		regressionCommand.Env = stableEnv
		if output, regressionErr := regressionCommand.CombinedOutput(); regressionErr != nil {
			addCheck(tc.name+"-installed-binary-regression", false, fmt.Sprintf("%v (%s)", regressionErr, strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-regression", true, "stable launcher validated the installed target package")
		// The documented first-start boundary: a normal install commits registry
		// records without a bootstrap receipt, and workflow start rejects a
		// registry that was never bootstrapped. Bootstrap the same registry from
		// the same source before the installed-binary canary starts its run.
		if _, bootstrapErr := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, ReleaseRoot: releaseRoot, BinaryTarget: launcher, RegistryPath: registry, Bootstrap: true, Force: true}); bootstrapErr != nil {
			addCheck(tc.name+"-bootstrap", false, bootstrapErr.Error())
			continue
		}
		if !isFile(registry + ".bootstrap.json") {
			addCheck(tc.name+"-bootstrap", false, "bootstrap did not persist the registry bootstrap receipt")
			continue
		}
		addCheck(tc.name+"-bootstrap", true, "registry bootstrap receipt committed for the installed target")
		registryDoc, registryErr := LoadRegistry(registry)
		if registryErr != nil {
			addCheck(tc.name+"-installed-binary-canary", false, registryErr.Error())
			continue
		}
		canaryRecord := ""
		if len(report.Targets) > 0 {
			for _, record := range registryDoc.Records {
				if canonicalRegistryPath(record.Target) == canonicalRegistryPath(report.Targets[0].TargetPath) {
					canaryRecord = record.ID
					break
				}
			}
		}
		if canaryRecord == "" {
			addCheck(tc.name+"-installed-binary-canary", false, "installed target has no exact registry record")
			continue
		}
		canaryCommand := exec.Command(launcher, "canary", "portable", "--root", installedTarget, "--format", "json")
		canaryCommand.Env = append(stableEnv, "FORMAL_GATES_INSTALLED_CANARY=1", "FORMAL_GATES_CANARY_PACKAGE_ROOT="+report.Targets[0].TargetPath, "FORMAL_GATES_CANARY_WORKFLOW_ROOT="+project, "FORMAL_GATES_CANARY_REGISTRY="+registry, "FORMAL_GATES_CANARY_RECORD="+canaryRecord)
		if output, canaryErr := canaryCommand.CombinedOutput(); canaryErr != nil {
			addCheck(tc.name+"-installed-binary-canary", false, fmt.Sprintf("%v (%s)", canaryErr, strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-canary", true, "stable launcher completed the installed target canary")
		candidateExecutable, executableErr := os.Executable()
		candidateBase := filepath.Base(filepath.Clean(candidateExecutable))
		if executableErr == nil && candidateBase == nativeBinaryName() && canonicalRegistryPath(candidateExecutable) != canonicalRegistryPath(launcher) {
			candidateProject := filepath.Join(project, "candidate-must-not-install")
			candidateInstall := exec.Command(candidateExecutable, "install", "--source", releaseRoot, "--host", "claude", "--scope", "project", "--project", candidateProject, "--binary-target", candidateExecutable, "--force")
			candidateInstall.Env = stableEnv
			if output, candidateErr := candidateInstall.CombinedOutput(); candidateErr == nil {
				addCheck(tc.name+"-candidate-install-fenced", false, "candidate binary unexpectedly installed runtime")
				continue
			} else if exists(filepath.Join(candidateProject, ".claude", "skills", "formal-gates")) {
				addCheck(tc.name+"-candidate-install-fenced", false, fmt.Sprintf("candidate rejection left a target: %s", strings.TrimSpace(string(output))))
				continue
			}
			addCheck(tc.name+"-candidate-install-fenced", true, "candidate binary could not claim the stable launcher")

			candidateRunID := "candidate-must-not-start"
			candidateStart := exec.Command(candidateExecutable, "workflow", "start", "--root", project, "--package-root", report.Targets[0].TargetPath, "--run-id", candidateRunID, "--requirement", "requirement.md", "--vcs", "git", "--route", "lightweight")
			candidateStart.Env = stableEnv
			if output, candidateErr := candidateStart.CombinedOutput(); candidateErr == nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, "candidate binary unexpectedly created workflow state")
				continue
			} else if exists(RunDir(project, candidateRunID)) {
				addCheck(tc.name+"-candidate-workflow-fenced", false, fmt.Sprintf("candidate rejection left a run directory: %s", strings.TrimSpace(string(output))))
				continue
			}
			addCheck(tc.name+"-candidate-workflow-fenced", true, "candidate binary could not drive the registered target")
		}
		faultProject := filepath.Join(project, "fault-recovery")
		faultCommand := exec.Command(launcher, "install", "--source", releaseRoot, "--host", "claude", "--scope", "project", "--project", faultProject, "--binary-target", launcher, "--force")
		faultCommand.Env = append(stableEnv, "FORMAL_GATES_INSTALL_FAULT=post-switch-smoke")
		if output, faultErr := faultCommand.CombinedOutput(); faultErr == nil {
			addCheck(tc.name+"-installed-binary-fault-recovery", false, "post-switch smoke fault unexpectedly succeeded")
			continue
		} else if exists(filepath.Join(faultProject, ".claude", "skills", "formal-gates")) {
			addCheck(tc.name+"-installed-binary-fault-recovery", false, fmt.Sprintf("fault left a candidate target: %s", strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-fault-recovery", true, "installed binary recovered the stable namespace")
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
		uninstalled, err := Uninstall(UninstallOptions{Host: tc.host, Scope: "project", Project: project, RegistryPath: registry})
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
	return identities
}

func canaryEnvironment(home, localAppData string) []string {
	keys := map[string]bool{"HOME": true, "USERPROFILE": true, "LOCALAPPDATA": true}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		name := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			name = item[:index]
		}
		if !keys[name] {
			environment = append(environment, item)
		}
	}
	environment = append(environment, "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+localAppData)
	return environment
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
