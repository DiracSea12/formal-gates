package cli

import (
	"bytes"
	"encoding/json"

	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/prompts"

	"formal-gates/internal/validate"
)

func TestCLIWorkflowCandidateRejectsSubmitAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.json")
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "candidate", "submit", "--path", path}, IO{Stderr: &stderr}); code == 0 {
		t.Fatalf("workflow candidate submit unexpectedly succeeded: stderr=%q", stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected submit alias created candidate state: %v", err)
	}
}

func TestCLIWorkflowDiagnoseReportsCandidateEngineIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	packageDigest, err := validate.PackageDigest(".")
	if err != nil {
		t.Fatalf("package digest: %v", err)
	}
	request, err := json.Marshal(facade.StartRequest{PackageDigest: packageDigest, InstalledTargetIdentity: "diagnostic-target"})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(filepath.Dir(path), "request.json"), string(request))
	mustWriteCLI(t, path, `{
  "writer": "engine",
  "stateSchemaVersion": "1",
  "workflowDefinitionVersion": "unsupported",
  "definitionSource": "definitions/workflow.json",
  "definitionDigest": "sha256:unsupported",
  "packageDigest": "sha256:testkit",
  "status": "ACTIVE"
}
`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report facade.DiagnosticReport
	if err := json.Unmarshal([]byte(runCLI(t, "workflow", "diagnose", "--path", path)), &report); err != nil {
		t.Fatal(err)
	}
	if !report.JSONReadable || report.DetectedVersions["workflowDefinitionVersion"] != "unsupported" {
		t.Fatalf("detected report = %+v", report)
	}
	if report.Supported.StateSchemaVersion != encoder.StateSchemaVersion ||
		report.Supported.WorkflowDefinitionVersion != definition.WorkflowDefinitionVersion ||
		report.Supported.DefinitionDigest != definition.WorkflowDefinitionDigest ||
		report.Supported.PackageDigest != packageDigest {
		t.Fatalf("supported identity = %+v", report.Supported)
	}
	if !strings.Contains(report.Recommendation, persistence.UnsupportedRunVersionCode) || !strings.Contains(report.Recommendation, "rebuild") {
		t.Fatalf("recommendation = %q", report.Recommendation)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("workflow diagnose changed its input")
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
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Requirement."))
	mustWriteCLI(t, filepath.Join(root, "design.md"), "design\n")
	// 白盒设计者交付的结构测试代码——测试仓库带一个测试文件，作为白盒用例测试
	// 引用（<文件>::<函数>）的定位目标。与 internal/validate 的 whiteboxDeliveredTestCode
	// 同源。
	mustWriteCLI(t, filepath.Join(root, "whitebox_delivered_test.go"), "package whiteboxfixture\n\nimport \"testing\"\n\nfunc TestWhiteboxDirectRules(t *testing.T) {}\n\nfunc TestWhiteboxStructure(t *testing.T) {}\n\nfunc TestWhiteboxDirectBehavior(t *testing.T) {}\n")
	// 与 internal/validate fixture 一致地忽略运行期临时状态，使快照就绪脏检查
	// （检测未跟踪且未忽略文件）不把 .gates/tmp/ 运行状态、.gates/run-locks/
	// 持久锁文件与 .gates/results seal 产物误判为未提交变更。
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n.gates/run-locks/\n.gates/results\n")
	cliGit(t, root, "init")
	cliGit(t, root, "config", "user.email", "tests@example.invalid")
	cliGit(t, root, "config", "user.name", "Formal Gates CLI Tests")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "base")
	pkg := t.TempDir()
	mustWriteCLI(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "shared contract\n")
	for _, id := range prompts.RequiredActionIDs {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, rules := range catalog.RoleRules {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "roles", rules.ID+".md"), rules.Content)
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
