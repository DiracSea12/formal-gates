//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/validate"
)

const nativeTextRequirement = `# 文本交付

## 需求点

### REQ-001：大写固定文本
#### 要求
将现有 app.txt 的 hello 后跟一个换行改为 HELLO 后跟一个换行。只交付该文件内容，不新增程序、接口或其他行为。
#### 验收条件
- AC-001：app.txt 的完整字节为 HELLO 后跟一个 LF 换行。
#### 来源
已授权的隔离实跑测试输入。

## 方案
使用现有 app.txt，在一个开发批次内修改固定文本。无需拆分、迁移或依赖。批次交付物和公开入口均为 app.txt；DoD 为 AC-001。项目检查分别为 test -f app.txt（完整构建资格）与 test "$(cat app.txt)" = HELLO（完整测试资格），无其他构建或测试义务。回退为恢复 hello 换行；恢复沿原任务继续；交接提供文件字节与检查结果。测试仅自动执行确认需求后的产品审查和技术准备审查，到实际 typed Ask 停止，用户仍决定拆分及路线。
`

// This explicit live test spends real ZCode calls on the default installed
// consumer. Ordinary tests skip it; all native account paths are prepared in a
// separate home before execution, and original source evidence remains there.
func TestWorkflowAutomaticNativeReachesUserDecision(t *testing.T) {
	if os.Getenv("FORMAL_GATES_AUTOMATIC_NATIVE") != "1" {
		t.Skip("requires explicitly authorized real native execution")
	}
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	var err error
	c := &engineCanary{t: t, nativeHost: "zcode", sourceBuildEnv: os.Environ(), doc: []byte(nativeTextRequirement)}
	if err := c.prepareHome(os.Getenv("FORMAL_GATES_AUTOMATIC_NATIVE_HOME"), os.Getenv("HOME"), source); err != nil {
		t.Fatal(err)
	}
	c.project = filepath.Join(c.home, ".formal-gates-resources", "automatic-text")
	c.jobs = filepath.Join(c.home, "evidence")
	c.binary = filepath.Join(c.home, ".local", "bin", "formal-gates")
	for _, path := range []string{c.project, c.jobs, filepath.Dir(c.binary)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"requirements.md": nativeTextRequirement, "app.txt": "hello\n", ".gitignore": ".gates/\n.zcode/\nAGENTS.md\n"} {
		if err := os.WriteFile(filepath.Join(c.project, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = c.project
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native fixture Git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Authorized native test")
	git("config", "user.email", "native-test@example.invalid")
	git("add", ".")
	git("commit", "-qm", "Confirmed native test input")
	base := git("rev-parse", "HEAD")
	intake := c.confirmedIntake()
	t.Logf("authorized_test_input_revision=%s complete_document=%s", c.revision, nativeTextRequirement)
	c.buildInstalledCandidate(source)
	inventory, err := validate.PackageReceipt(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(c.home, "candidate")
	for _, entry := range inventory.Entries {
		body, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(candidate, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, os.FileMode(entry.Mode)); err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile(c.binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(candidate, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate, "bin", "formal-gates"), body, 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range c.hostEnvironment() {
		t.Setenv(key, value)
	}
	options := validate.InstallOptions{Source: candidate, Host: "zcode", Scope: "project", Project: c.project, BinaryTarget: c.binary, Force: true, SkipHooks: true}
	c.installed, err = validate.Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err := validate.Install(options); err != nil {
		t.Fatal(err)
	}
	if err := c.captureBinding(); err != nil {
		t.Fatal(err)
	}
	call := func(name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(c.binary, args...)
		cmd.Dir = c.project
		out, err := cmd.CombinedOutput()
		if saveErr := os.WriteFile(filepath.Join(c.jobs, name+".json"), out, 0600); saveErr != nil {
			t.Fatal(saveErr)
		}
		if err != nil {
			t.Fatalf("native installed command: %v %s", err, out)
		}
		return out
	}
	body, err = json.Marshal(intake)
	if err != nil {
		t.Fatal(err)
	}
	intakePath := filepath.Join(c.jobs, "intake.json")
	if err := os.WriteFile(intakePath, body, 0600); err != nil {
		t.Fatal(err)
	}
	call("start", "workflow", "start", "--root", c.project, "--run-id", "automatic-text", "--route", "regular", "--vcs", "git", "--base-snapshot", base, "--intake-receipt", intakePath)
	if err := c.recordIsolation(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Run      facade.Run        `json:"run"`
		Boundary string            `json:"boundary"`
		Blocked  map[string]string `json:"blocked"`
	}
	body = call("automatic", "workflow", "drive", "--root", c.project, "--run-id", "automatic-text")
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual_native_evidence=%s boundary=%s pending_asks=%d", c.home, result.Boundary, len(result.Run.PendingAsks))
	if result.Boundary != "ASK" || len(result.Run.PendingAsks) == 0 || len(result.Blocked) != 0 {
		t.Fatalf("actual automatic boundary: %s", body)
	}
	jobs := 0
	if err := filepath.WalkDir(filepath.Join(c.project, facade.EngineNamespace, "automatic-text", "native"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "source.json" {
			return nil
		}
		directory := filepath.Dir(path)
		source, err := NativeJobSource("zcode", directory)
		if err != nil {
			return err
		}
		if _, _, _, err := NativeResultEvidence("zcode", directory, source); err != nil {
			return err
		}
		transcript, _ := source["transcript"].(string)
		usage, err := cost.ParseTranscript("zcode", transcript)
		if err != nil {
			return err
		}
		var request struct {
			Engine struct {
				WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
			} `json:"engine"`
		}
		raw, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			return err
		}
		id := request.Engine.WorkerMaterial.ActionID
		entryCost := result.Run.Cost.Dispatches[id]
		if entryCost.Source != "transcript" || entryCost.InputCacheHitTokens != usage.InputCacheHitTokens || entryCost.InputCacheMissTokens != usage.InputCacheMissTokens || entryCost.OutputTokens != usage.OutputTokens {
			t.Fatalf("actual native cost differs from original provider stream: action=%s ledger=%+v usage=%+v", id, entryCost, usage)
		}
		jobs++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if jobs == 0 {
		t.Fatal("default drive performed no verified native model responsibility")
	}
	call("observe", "workflow", "drive", "--root", c.project, "--run-id", "automatic-text")
}
