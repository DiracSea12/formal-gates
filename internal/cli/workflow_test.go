package cli

import (
	"bytes"
	"encoding/json"

	"os"
	"os/exec"
	"path/filepath"

	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

func TestCLIWorkflowFutureRejectsSubmitAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.json")
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "future", "submit", "--path", path}, IO{Stderr: &stderr}); code == 0 {
		t.Fatalf("workflow future submit unexpectedly succeeded: stderr=%q", stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected submit alias created future state: %v", err)
	}
}

func TestCLIHelpListsCurrentWorkflowCommands(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	for _, want := range []string{"workflow start|drive|submit|show|status|next|diagnose|guard-preview", "lifecycle capture|verify"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help omitted %s: %s", want, stdout.String())
		}
	}
}

func TestCLISubcommandHelpPrintsUsage(t *testing.T) {
	// 二层 --help 粒度统一：每个组的 --help 打印该组自己的 usage，而非顶层清单。
	for _, group := range []string{"workflow", "lifecycle", "canary", "hook"} {
		for _, flag := range []string{"-h", "--help", "help"} {
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", []string{group, flag}, IO{Stdout: &stdout, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("%s %s code=%d err=%s", group, flag, code, stderr.String())
			}
			want := map[string]string{
				"workflow":  "Usage: formal-gates workflow <subcommand>",
				"lifecycle": "Usage: formal-gates lifecycle <subcommand>",
				"canary":    "Usage: formal-gates canary <subcommand>",
				"hook":      "Usage: formal-gates hook decide",
			}[group]
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("%s %s omitted group usage (%s): %s", group, flag, want, stdout.String())
			}
		}
	}
}

func installHostCLI(t *testing.T, provider, project string) (string, string) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the CLI test source as an absolute path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	sourceBinary := filepath.Join(repoRoot, "bin", installTestBinaryName())
	if err := os.MkdirAll(filepath.Dir(sourceBinary), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", sourceBinary, "./cmd/formal-gates")
	cmd.Dir = repoRoot
	cmd.Env = environmentWithOverrides(os.Environ(), map[string]string{
		"HOME":        os.Getenv("FORMAL_GATES_TEST_BUILD_HOME"),
		"USERPROFILE": os.Getenv("FORMAL_GATES_TEST_BUILD_USERPROFILE"),
	})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build installed %s CLI: %v: %s", provider, err, output)
	}
	launcher := filepath.Join(os.Getenv("HOME"), ".local", "bin", installTestBinaryName())
	host := map[string]string{
		lifecycle.ProviderClaude: "claude",
		lifecycle.ProviderCursor: "cursor",
		lifecycle.ProviderCodex:  "codex",
	}[provider]
	if host == "" {
		t.Fatalf("unsupported installed-host fixture provider %q", provider)
	}
	report, err := validate.Install(validate.InstallOptions{Source: repoRoot, Host: host, Scope: "project", Project: project, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatalf("install %s CLI: %v", provider, err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("install %s returned %d targets", provider, len(report.Targets))
	}
	// 文档化首启边界：普通 install 只提交 registry record，第一次 workflow start
	// 前必须先提交 bootstrap receipt（同一 source/launcher/registry）。
	if _, err := validate.Install(validate.InstallOptions{Source: repoRoot, Host: host, Scope: "project", Project: project, BinaryTarget: launcher, Bootstrap: true, Force: true}); err != nil {
		t.Fatalf("bootstrap %s CLI registry: %v", provider, err)
	}
	return launcher, report.Targets[0].TargetPath
}

func runInstalledCLI(t *testing.T, binary, workdir string, environment []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = hostFilteredEnv(environment)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v: %s", binary, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func cliOperatorVerificationArgs() []string {
	checks := []string{"requirement-match", "normal-entry", "evidence", "locations", "scope", "severity", "binding", "completeness"}
	args := make([]string, 0, len(checks)*2+2)
	for _, check := range checks {
		args = append(args, "--operator-check", check)
	}
	return append(args, "--operator-evidence", "verified candidate against the bound requirement, normal entry path, evidence, locations, scope, severity, dispatch binding, and completeness")
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	clearHostEnv(t)
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("%s failed: %s", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

func cliWorkflowFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("FORMAL_GATES_TEST_BUILD_HOME", os.Getenv("HOME"))
	t.Setenv("FORMAL_GATES_TEST_BUILD_USERPROFILE", os.Getenv("USERPROFILE"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := cliGitFixtureRoot(t)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Requirement."))
	mustWriteCLI(t, filepath.Join(root, "design.md"), "design\n")
	// 白盒设计者交付的结构测试代码——测试仓库带一个测试文件，作为白盒用例测试
	// 引用（<文件>::<函数>）的定位目标。与 internal/validate 的 whiteboxDeliveredTestCode
	// 同源。
	mustWriteCLI(t, filepath.Join(root, "whitebox_delivered_test.go"), "package whiteboxfixture\n\nimport \"testing\"\n\nfunc TestWhiteboxDirectRules(t *testing.T) {}\n\nfunc TestWhiteboxStructure(t *testing.T) {}\n\nfunc TestWhiteboxDirectBehavior(t *testing.T) {}\n")
	// 与 internal/validate fixture 一致地忽略运行期临时状态，使快照就绪脏检查
	// （检测未跟踪且未忽略文件）不把 .gates/tmp/ 运行状态与 .gates/results seal 产物
	// 误判为未提交变更。
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n.gates/results\n")
	cliGit(t, root, "init")
	cliGit(t, root, "config", "user.email", "tests@example.invalid")
	cliGit(t, root, "config", "user.name", "Formal Gates CLI Tests")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "base")
	pkg := t.TempDir()
	mustWriteCLI(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "shared contract\n")
	for _, id := range []string{"requirements-clarification", "product-review", "start-readiness", "qa-design", "qa-review", "qa-execution", "carry", "development-worker"} {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	mustWriteCLI(t, filepath.Join(pkg, "gates", "quality.md"), "quality checks\n")
	return root, pkg
}

func cliStructuredRequirement(requirement string) string {
	return "## 需求点\n\n### REQ-001：Test requirement\n\n#### 要求\n\n" + requirement + "\n\n#### 验收条件\n\n- AC-001：The test requirement is satisfied.\n\n#### 来源\n\nTest fixture.\n"
}

func environmentWithOverrides(environment []string, overrides map[string]string) []string {
	result := make([]string, 0, len(environment)+len(overrides))
	for _, item := range environment {
		key := strings.SplitN(item, "=", 2)[0]
		if _, overridden := overrides[key]; !overridden {
			result = append(result, item)
		}
	}
	for key, value := range overrides {
		if strings.TrimSpace(value) != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func cliGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// Fix 4：show 对已终止/不存在的 run 给友好提示（而非裸 open state.json）。
func TestCLIShowMissingRunFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", t.TempDir(), "--run-id", "never-existed"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "no current engine run") {
		t.Fatalf("show missing run did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 5：workflow 无子命令紧跟 flags 时提示子命令必填。
func TestCLIWorkflowFlagsWithoutSubcommandHintsRequired(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "--root", "."}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "workflow subcommand is required") {
		t.Fatalf("workflow --root accepted without a subcommand: code=%d err=%s", code, stderr.String())
	}
}

// Fix 8：缺 run-id 时友好提示（不裸报 state.json.lock）。
func TestCLIShowMissingRunIDFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", t.TempDir()}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "run id is required") {
		t.Fatalf("show without run-id did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 10：gate run 位置参数在 flags 前时提示 flag 前置。
func TestCLIGateRunFlagsAfterIDsHintsFlagFirst(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"gate", "run", "quality", "--scope", "s"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "flags must precede") {
		t.Fatalf("gate run flag-after-id did not hint flag-first: code=%d err=%s", code, stderr.String())
	}
}

// Fix 12：hook decide --provider 非 codex 等非法值报错。
func TestCLIHookDecideRejectsInvalidProvider(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"hook", "decide", "--provider", "banana"}, IO{Stdin: strings.NewReader(`{"command":"x"}`), Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "unsupported hook provider") {
		t.Fatalf("invalid hook provider was accepted: code=%d err=%s", code, stderr.String())
	}
}

// Fix 13：canary portable --format 非 text/json 报错。
func TestCLICanaryPortableRejectsInvalidFormat(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"canary", "portable", "--format", "xml", "--root", t.TempDir()}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "must be text or json") {
		t.Fatalf("invalid portable format was accepted: code=%d err=%s", code, stderr.String())
	}
}

// Fix 14：canary codex-hook-probe 成功时输出载荷文件路径/字节数。
func TestCLICodexHookProbePrintsPayloadInfo(t *testing.T) {
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"canary", "codex-hook-probe", "--payload-dir", t.TempDir()}, IO{Stdin: strings.NewReader(`{"event":"PreToolUse","tool_name":"x"}`), Stdout: &stdout})
	if code != 0 {
		t.Fatalf("codex-hook-probe failed: code=%d out=%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "codex-hook-probe: wrote") || !strings.Contains(stdout.String(), "payload to") {
		t.Fatalf("codex-hook-probe did not print payload info: %q", stdout.String())
	}
}

// Fix 18：hook decide 空 stdin 给友好错误。
func TestCLIHookDecideEmptyStdinFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"hook", "decide"}, IO{Stdin: strings.NewReader(""), Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires a JSON decision payload") {
		t.Fatalf("empty hook stdin did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 18：--version 给出可用提示而非 unknown command。
func TestCLIVersionFlagPrintsVersionHint(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--version"}, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), "development build") {
		t.Fatalf("--version did not print a version hint: code=%d out=%s", code, stdout.String())
	}
}

// Retired public commands must fail before touching either state namespace.
func TestCLIWorkflowRetirementAndRawDiagnosis(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".gates", "tmp", "old", "state.json")
	raw := []byte(`{"runId":"old","status":"PENDING","schemaVersion":1,"opaque":{"untouched":true}}`)
	mustWriteCLI(t, legacy, string(raw))
	for _, name := range []string{"future", "review-delta-preview", "requirement-impact-preview", "repair-preview", "route-candidates", "cleanup", "resume", "abort", "seal", "snapshot", "prepare-action", "prepare-gate", "claim-dispatch", "record-action", "record-gate", "qa-design", "qa-review", "qa-execution", "authorize-repair", "requirement", "requirements", "advance"} {
		t.Run(name, func(t *testing.T) {
			var out, err bytes.Buffer
			code := Run("formal-gates", []string{"workflow", name, "--root", root, "--run-id", "old"}, IO{Stdout: &out, Stderr: &err})
			if code == 0 || !strings.Contains(err.String(), "unknown workflow subcommand") {
				t.Fatalf("retired entry %s: code=%d out=%s err=%s", name, code, out.String(), err.String())
			}
		})
	}
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "diagnose", "--root", root, "--run-id", "old"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatalf("diagnose %d: %s", code, stderr.String())
	}
	var result struct {
		Code     string          `json:"code"`
		ReadOnly bool            `json:"readOnly"`
		Path     string          `json:"path"`
		Raw      json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, result.Raw); err != nil {
		t.Fatal(err)
	}
	if result.Code != "UNSUPPORTED_RUN_VERSION" || !result.ReadOnly || result.Path != legacy || !bytes.Equal(compact.Bytes(), raw) {
		t.Fatalf("raw diagnose changed legacy contents: %s", out.String())
	}
	after, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("legacy state changed: %v %s", err, after)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "engine")); !os.IsNotExist(err) {
		t.Fatalf("diagnosis or retired commands created engine state: %v", err)
	}
}

// Git for Windows still limits some worktree administration paths even when
// core.longpaths is enabled. Keep the fixture root independent of the test name.
func cliGitFixtureRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return t.TempDir()
	}
	root, err := os.MkdirTemp("", "fg-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return root
}
