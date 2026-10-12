//go:build !windows

package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
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

const nativeCommandRequirement = `# 文本转换命令

## 需求点

### REQ-001：文本转换与命令行为
#### 要求
现有 Python 命令 python3 app.py 接受一个词，输出 frob: UPPER bytes=N 后跟 LF；UPPER 是该词的 Python str.upper()，N 是输入词 UTF-8 字节数。空字符串是有效词；额外普通参数不改变首词结果。没有参数时以 2 退出，stderr 为 usage: frob <word> 后跟 LF，stdout 为空。未知 flag（以 -- 开头的首参数，例如 --unknown-flag）必须非零退出且 stdout 为空。upper_word 原样返回，word_size 恒为0，CLI 接线已存在；未知 flag 被当作普通词接受，是明确的基线缺口。
#### 验收条件
- AC-001：upper_word 对 Unicode/ASCII 词执行 str.upper()，空词返回空串。
- AC-002：word_size 返回输入词的 UTF-8 字节数，空词为0。
- AC-003：python3 app.py héLLo 精确输出 frob: HÉLLO bytes=6 后跟 LF，空字符串输出 frob:  bytes=0 后跟 LF，额外普通参数不改变首词结果；无参数的退出码、stdout、stderr符合上述要求。
- AC-004：python3 app.py --unknown-flag 非零退出且 stdout 为空。
#### 来源
当前用户授权的隔离实跑测试输入；确认参与方是预声明测试 driver，不表示主用户逐份确认了模拟项目文件。

## 方案
使用既有 app.py 与 size.py、Python 标准库，不新增产品功能或依赖。保留 CLI 的既有调用/输出接线，开发只实现 upper_word 和 word_size。开发不改 flag 处理；AC-004 的已知缺口由本 run 的真实验证判定和已授权主线返修闭环满足，不改变该验收条件。返修可以修改 app.py 拒绝以 -- 开头的首参数。
%s
完整构建检查为 python3 -m py_compile app.py size.py；完整开发测试为 python3 -c 'from app import upper_word; from size import word_size; assert upper_word("héLLo")=="HÉLLO"; assert upper_word("")==""; assert word_size("héLLo")==6; assert word_size("")==0'。分片批次测试只断言自己函数；完整构建检查相同。没有其他构建依赖或测试义务。DoD 为本批函数满足相应 AC；回退恢复该批函数占位；恢复沿原会话/原批继续；交接报告实际文件和检查结果。最终独立 QA 与测试使用设计、审查批准后冻结的命令/操作；不得执行时临时造配方或用开发自测代替独立 QA。
`

const nativeCommandSource = `import sys
from size import word_size


def upper_word(word):
    return word


def main(args):
    if not args:
        print("usage: frob <word>", file=sys.stderr)
        return 2
    print(f"frob: {upper_word(args[0])} bytes={word_size(args[0])}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
`

// Every role runs through the installed default consumer. The authorized test
// participant supplies only topology and route choices through original Asks.
func TestWorkflowAutomaticNativeCompletesCurrentFlow(t *testing.T) {
	if os.Getenv("FORMAL_GATES_AUTOMATIC_NATIVE") != "1" {
		t.Skip("requires explicitly authorized real native execution")
	}
	packageRoot := os.Getenv("FORMAL_GATES_AUTOMATIC_NATIVE_PACKAGE")
	if packageRoot == "" {
		t.Fatal("a fixed installed-source package with bin/formal-gates is required")
	}
	for _, provider := range []string{"git", "svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			for _, split := range []bool{false, true} {
				name := "no-split"
				if split {
					name = "split"
				}
				t.Run(name, func(t *testing.T) { runAutomaticNativeFlow(t, packageRoot, provider, name, split) })
			}
		})
	}
}

type automaticNativeFlow struct {
	*engineCanary
	sequence int
	intake   facade.IntakeConfirmationReceipt
}

type automaticNativeBoundary struct {
	Run        facade.Run                         `json:"run"`
	Boundary   string                             `json:"boundary"`
	Blocked    map[string]string                  `json:"blocked"`
	Acceptance *protocol.Acceptance               `json:"acceptance"`
	Children   map[string]automaticNativeBoundary `json:"children"`
}

func runAutomaticNativeFlow(t *testing.T, packageRoot, provider, topology string, split bool) {
	c := &engineCanary{t: t, nativeHost: "zcode", sourceBuildEnv: os.Environ(), split: split}
	f := &automaticNativeFlow{engineCanary: c}
	home := filepath.Join(os.Getenv("FORMAL_GATES_AUTOMATIC_NATIVE_HOME"), provider, topology, "home")
	if err := c.prepareHome(home, os.Getenv("HOME"), packageRoot); err != nil {
		t.Fatal(err)
	}
	c.project, c.jobs = filepath.Join(home, ".formal-gates-resources", "project"), filepath.Join(home, "evidence")
	c.binary = filepath.Join(home, ".local", "bin", "formal-gates")
	for _, path := range []string{c.project, c.jobs, filepath.Dir(c.binary)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	c.vcs = vcs.NewNative(c.project, provider, nil)
	c.initFixtureRepository(filepath.Join(home, "repositories"))
	plan := "单一内聚开发批次实现两函数，公开命令是唯一交付物；不拆分。最终由 engine 签发黑盒、白盒和所选质量门。"
	if split {
		plan = "两个独立开发批次分别只修改 app.py 的 upper_word 与 size.py 的 word_size。两个既有函数签名及 CLI 接线不变，彼此无实现依赖；app.py 所在片承担 AC-001，size.py 所在片承担 AC-002，分片标识由原技术准备审提出，master-merge 承担组合 AC-003 和 flag AC-004。各片的原函数检查须明确承担该片完整测试，使用 FULL_TESTS；完整构建检查使用 FULL_BUILD。child 不执行普通黑盒/白盒 QA，合并后由主线集中执行 MERGE QA 和适用门。"
	}
	c.doc = []byte(fmt.Sprintf(nativeCommandRequirement, plan))
	for name, body := range map[string]string{"app.py": nativeCommandSource, "size.py": "def word_size(word):\n    return 0\n", "requirements.md": string(c.doc), ".gitignore": ".gates/\n.zcode/\nAGENTS.md\n__pycache__/\n"} {
		if err := os.WriteFile(filepath.Join(c.project, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := c.commitFixture([]string{"app.py", "size.py", "requirements.md", ".gitignore"}, "Confirmed command test input")
	if err := os.WriteFile(c.binary, readTestFileBytes(t, filepath.Join(packageRoot, "bin", "formal-gates")), 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range c.hostEnvironment() {
		t.Setenv(key, value)
	}
	options := validate.InstallOptions{Source: packageRoot, Host: "zcode", Scope: "project", Project: c.project, BinaryTarget: c.binary, Force: true, SkipHooks: true}
	var err error
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
	intakePath := filepath.Join(c.jobs, "intake.json")
	var preview facade.IntakePreview
	f.decode(f.command(nil, "workflow", "start", "--precheck", "--root", c.project, "--requirement", "requirements.md", "--save-candidate", intakePath), &preview)
	if preview.Content != string(c.doc) || preview.Revision == "" || preview.RequirementDocument == nil {
		t.Fatal("public precheck lost the complete test document")
	}
	f.intake, c.revision = preview.IntakeCandidate, preview.Revision
	f.save("test-participant-confirmation.json", map[string]any{"authority": "authorized isolated test participant", "displayed": preview, "decision": "confirm complete displayed materials and exact revisions"})
	t.Logf("authorized_test_participant_confirmation revision=%s complete_document=%s", preview.Revision, preview.Content)
	if provider == "git" && !split {
		c.promotion = installCanaryPromotionRecovery(c)
	}
	f.command(nil, "workflow", "start", "--root", c.project, "--run-id", "engine-canary", "--route", "regular", "--vcs", provider, "--base-snapshot", base, "--intake-receipt", intakePath)
	if err := c.recordIsolation(); err != nil {
		t.Fatal(err)
	}
	environment := c.hostEnvironment()
	if provider == "p4" {
		for _, key := range []string{"P4PORT", "P4USER", "P4CLIENT", "P4CONFIG", "P4ENVIRO", "P4TICKETS", "P4TRUST"} {
			environment[key] = os.Getenv(key)
		}
	}
	f.save("workflow-environment.json", map[string]any{"environment": environment, "launcher": c.binary, "project": c.project, "runId": "engine-canary", "vcs": provider, "repositories": filepath.Join(home, "repositories")})
	boundary := f.drive("engine-canary")
	if boundary.Run.Envelope.PackageDigest != c.installed.PackageDigest {
		t.Fatal("actual run is not bound to the prepared installed candidate")
	}
	for boundary.Run.Status != "COMPLETE" {
		f.requireUnblocked(boundary)
		var selected facade.Run
		if len(boundary.Run.PendingAsks) != 0 {
			selected = boundary.Run
		} else {
			ids := make([]string, 0, len(boundary.Children))
			for id := range boundary.Children {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				if len(boundary.Children[id].Run.PendingAsks) != 0 {
					selected = boundary.Children[id].Run
					break
				}
			}
		}
		if selected.RunID == "" {
			t.Fatalf("actual automatic boundary needs a decision outside the declared test choices: %s", textOf(boundary))
		}
		f.decide(selected)
		boundary = f.drive("engine-canary")
	}
	f.requireUnblocked(boundary)
	c.run = boundary.Run
	f.assertTerminal(base)
	f.assertNativeSources()
	final := boundary.Run
	replay := f.drive("engine-canary")
	if replay.Run.Revision != final.Revision || replay.Run.Status != final.Status || !canaryValuesEqual(replay.Run.Termination, final.Termination) {
		t.Fatal("terminal public drive replay changed the exact terminal evidence")
	}
}

func (f *automaticNativeFlow) decode(body []byte, value any) {
	f.t.Helper()
	if err := json.Unmarshal(body, value); err != nil {
		f.t.Fatalf("actual installed JSON: %v %s", err, body)
	}
}

func (f *automaticNativeFlow) save(name string, value any) {
	f.t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.jobs, name), append(body, '\n'), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *automaticNativeFlow) command(input []byte, args ...string) []byte {
	f.t.Helper()
	if len(args) > 1 && args[0] == "workflow" && slices.Contains([]string{"start", "show", "status", "next", "drive", "submit"}, args[1]) {
		args = append(args, "--machine")
	}
	cmd := exec.Command(f.binary, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = f.project, append(os.Environ(), "PWD="+f.project), bytes.NewReader(input)
	started := time.Now()
	body, err := cmd.CombinedOutput()
	f.sequence++
	f.save(fmt.Sprintf("installed-command-%d.json", f.sequence), map[string]any{"argv": append([]string{f.binary}, args...), "cwd": f.project, "input": json.RawMessage(input), "output": string(body), "elapsedSeconds": time.Since(started).Seconds(), "error": fmt.Sprint(err)})
	if err != nil {
		if f.promotion != nil && f.promotion.recover(f.engineCanary, args, err) {
			if args[1] == "submit" {
				confirmation := slices.Index(args, "--confirmation")
				if confirmation < 0 || confirmation+1 >= len(args) {
					f.t.Fatal("interrupted decision requires its original program confirmation")
				}
				var candidate facade.ControlCandidate
				f.decode(readTestFileBytes(f.t, args[confirmation+1]), &candidate)
				event := candidate.Event
				var original struct{ Content protocol.State }
				f.decode(readTestFileBytes(f.t, filepath.Join(f.project, facade.EngineNamespace, "engine-canary", "state.json")), &original)
				record, accepted := original.Content.Events[string(event.ID)]
				if !accepted {
					f.t.Fatal("interrupted control submit has no original accepted event")
				}
				acceptance, err := record.Replay(event)
				if err != nil {
					f.t.Fatal(err)
				}
				recovered := f.drive(original.Content.RunID)
				recovered.Acceptance = &acceptance
				f.save("recovered-submit.json", map[string]any{"event": event, "originalEventRecord": record, "actualDrive": recovered})
				body, err := json.Marshal(recovered)
				if err != nil {
					f.t.Fatal(err)
				}
				return body
			}
			return f.command(input, args...)
		}
		f.t.Fatalf("actual installed command failed, original evidence=%s: %v %s", f.jobs, err, body)
	}
	return body
}

func (f *automaticNativeFlow) drive(runID string) automaticNativeBoundary {
	f.t.Helper()
	var boundary automaticNativeBoundary
	f.decode(f.command(nil, "workflow", "drive", "--root", f.project, "--run-id", runID), &boundary)
	return boundary
}

func (f *automaticNativeFlow) requireUnblocked(boundary automaticNativeBoundary) {
	f.t.Helper()
	if len(boundary.Blocked) != 0 || boundary.Boundary == "BLOCKED" {
		f.t.Fatalf("actual automatic execution blocked; no verdict or source substituted: %s", textOf(boundary))
	}
	for _, child := range boundary.Children {
		f.requireUnblocked(child)
	}
}

func (f *automaticNativeFlow) decide(run facade.Run) {
	f.t.Helper()
	ask := run.PendingAsks[0]
	input := facade.ControlInput{Ask: 1}
	switch ask.Control {
	case protocol.ControlConfirmTopology:
		input.Choice, input.Reason = "no-split", "one cohesive command and its two existing functions"
		if f.split {
			input.Choice, input.Parallel = "split", "independent functions and concurrent merge case preparation"
			input.Owners = f.splitOwners(run)
		}
	case protocol.ControlSelectRoute:
		input.Choice = "full"
		if run.MasterRunID != "" {
			master := f.run
			f.decode(f.command(nil, "workflow", "show", "--root", f.project, "--run-id", run.MasterRunID), &master)
			var slice string
			for name, id := range master.SliceChildren {
				if id == run.RunID {
					slice = name
				}
			}
			if master.Review.Topology != nil && slice == master.Review.Topology.ACOwners["AC-002"] {
				input.Choice = "custom"
				gate := slices.Index(ask.RouteGates, "implementation-quality-gate")
				if gate < 0 {
					f.t.Fatal("declared custom gate is absent from original route candidates")
				}
				input.Gates = []int{gate + 1}
			}
		}
	default:
		f.t.Fatalf("actual typed Ask needs Root's semantic disposition beyond declared test choices: %s", textOf(ask))
	}
	draft, err := json.Marshal(input)
	if err != nil {
		f.t.Fatal(err)
	}
	f.sequence++
	name := fmt.Sprintf("control-confirmation-%d.json", f.sequence)
	path := filepath.Join(f.jobs, name)
	var candidate facade.ControlCandidate
	f.decode(f.command(draft, "workflow", "next", "--root", f.project, "--run-id", run.RunID, "--decision", "-", "--save-candidate", path), &candidate)
	if candidate.Run.RunID != run.RunID || !canaryValuesEqual(candidate.Input, input) {
		f.t.Fatal("program candidate differs from declared participant choices")
	}
	f.save(fmt.Sprintf("control-participant-%d.json", f.sequence), map[string]any{"authority": "authorized isolated test participant", "displayed": candidate, "decision": "confirm complete original candidate for these predeclared choices"})
	var response automaticNativeBoundary
	f.decode(f.command(nil, "workflow", "submit", "--root", f.project, "--run-id", run.RunID, "--confirmation", path), &response)
	if response.Acceptance == nil || strings.Contains(response.Acceptance.Status, "REJECTED") {
		f.t.Fatalf("original test-participant decision not accepted: %s", textOf(response))
	}
	f.requireUnblocked(response)
}

func (f *automaticNativeFlow) splitOwners(run facade.Run) []int {
	f.t.Helper()
	plan := run.Review.ReadinessProposal
	if plan == nil {
		f.t.Fatal("original readiness proposal is required for topology confirmation")
	}
	owners := map[string]string{}
	var proposed []string
	for _, batch := range plan.Batches {
		if batch.Owner == "master-merge" {
			continue
		}
		if batch.Owner != "" && !slices.Contains(proposed, batch.Owner) {
			proposed = append(proposed, batch.Owner)
		}
		for _, path := range batch.Artifacts {
			if path == "app.py" || path == "size.py" {
				if batch.Owner == "" || owners[path] != "" && owners[path] != batch.Owner {
					f.t.Fatal("original proposed ownership is missing or ambiguous", path)
				}
				owners[path] = batch.Owner
			}
		}
	}
	if owners["app.py"] == "" || owners["size.py"] == "" || owners["app.py"] == owners["size.py"] {
		f.t.Fatal("original plan does not propose the confirmed two independent function owners")
	}
	if run.Validation == nil {
		f.t.Fatal("original confirmed acceptance conditions are required")
	}
	var selected []int
	for _, requirement := range run.Validation.Document.Requirements {
		for _, condition := range requirement.AcceptanceConditions {
			owner := 0
			if artifact := map[string]string{"AC-001": "app.py", "AC-002": "size.py"}[condition.ID]; artifact != "" {
				owner = slices.Index(proposed, owners[artifact]) + 1
			}
			selected = append(selected, owner)
		}
	}
	return selected
}

func (f *automaticNativeFlow) assertTerminal(base string) {
	f.t.Helper()
	if f.revision != f.intake.RequirementRevision || canaryBytesRevision(f.doc) != f.revision {
		f.t.Fatal("original full intake oracle changed after confirmation")
	}
	terminal := f.run.Termination
	if terminal == nil || !terminal.ResourcesCleaned() || terminal.CandidatePromotion == nil || terminal.Validation == nil || len(terminal.PendingActions) != 0 || len(terminal.PendingHostActions) != 0 {
		f.t.Fatal("actual terminal has incomplete validation, native responsibility or resource cleanup")
	}
	if !terminal.GuaranteeActive() || terminal.IntakeConfirmationReceipt == nil || !canaryValuesEqual(*terminal.IntakeConfirmationReceipt, f.intake) {
		f.t.Fatal("actual terminal lost the original confirmed intake")
	}
	latestGuarantee := f.assertConfirmedTerminalRequirement(terminal)
	if f.promotion != nil {
		f.promotion.assertTerminal(f.engineCanary)
	}
	f.assertCanonicalGuarantee(f.run, latestGuarantee)
	head, err := f.vcs.Resolve()
	if err != nil || head != f.run.PromotedSnapshot {
		f.t.Fatalf("actual final native identity differs: %s %v", head, err)
	}
	proof := terminal.CandidatePromotion
	validated, err := f.vcs.TreeEntries(proof.Source)
	if err != nil {
		f.t.Fatal(err)
	}
	delivered, err := f.vcs.TreeEntries(head)
	if err != nil || !canaryValuesEqual(validated, delivered) || proof.SourceDigest != proof.FinalDigest {
		f.t.Fatal("promotion changed complete native bytes or metadata", err)
	}
	if f.vcs.Name() == "git" {
		count, err := f.vcs.CommitCount(base, head)
		if err != nil || count != 1 {
			f.t.Fatal("Git final delivery is not one organized commit", count, err)
		}
		worktrees, err := f.vcs.Worktrees()
		if err != nil || len(worktrees) != 1 {
			f.t.Fatal("actual Git worktree residuals", err)
		}
	}
	for _, resource := range terminal.NativeResources {
		absent, err := f.vcs.WorkspaceAbsent(resource.Path, resource.Handle)
		if err != nil || !absent {
			f.t.Fatalf("actual native registered resource remains: %+v %v", resource, err)
		}
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			f.t.Fatal("actual registered resource path remains", resource.Path, err)
		}
		if f.vcs.Name() == "git" && resource.Ref != "" {
			if ref := f.fixtureCommand(f.project, "git", nil, "for-each-ref", "--format=%(objectname)", resource.Ref); ref != "" {
				f.t.Fatal("actual Git resource reference remains", resource.Ref)
			}
		}
	}
	if f.split && (len(terminal.SliceChildren) != 2 || len(terminal.SliceReceipts) != 2) {
		f.t.Fatal("actual split omitted original child receipts")
	}
	modes := []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}
	if f.split {
		modes = []coverage.ReviewKind{coverage.ReviewMerge}
	}
	for _, mode := range modes {
		set := terminal.Validation.Sets[mode]
		if set == nil || set.Status != "FROZEN" || len(set.Manifest.Cases) == 0 || set.Review == nil || set.Review.Verdict() != coverage.StatusPass {
			f.t.Fatal("actual required QA mode lacks its frozen approved cases", mode)
		}
	}
	if len(terminal.Validation.Waves) < 2 {
		f.t.Fatal("actual validation did not complete the required failure/repair cycle")
	}
	first, last := terminal.Validation.Waves[0], terminal.Validation.Waves[len(terminal.Validation.Waves)-1]
	if err := canaryFailureEvidence(&first, terminal.Validation.Findings); err != nil {
		f.t.Fatal(err)
	}
	if last.Status != "COMPLETE" || last.Scope != coverage.ScopeAffected || first.Candidate.Identity == last.Candidate.Identity {
		f.t.Fatal("actual repair did not validate a new AFFECTED candidate")
	}
	for _, target := range last.Targets {
		if target.Status != protocol.OutcomePass {
			f.t.Fatal("required final target did not pass through actual execution or legal inheritance", textOf(target))
		}
	}
	f.save("terminal-proof.json", map[string]any{"run": f.run, "actualFinal": head, "nativeTreesEqual": true, "resourcesActuallyAbsent": true, "modelEvidence": "original native jobs and transcripts"})
}

func (f *automaticNativeFlow) assertConfirmedTerminalRequirement(terminal *protocol.State) *protocol.GuaranteeActivation {
	f.t.Helper()
	path := filepath.Join(f.jobs, "carry-proceed-candidate.json")
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if terminal.RequirementRevision() != f.revision {
			f.t.Fatal("terminal differs from the original confirmed requirement without a carry confirmation")
		}
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var candidate facade.ControlCandidate
	f.decode(body, &candidate)
	var participant struct {
		Authority     string                  `json:"authority"`
		Decision      string                  `json:"decision"`
		Displayed     facade.ControlCandidate `json:"displayed"`
		CandidateFile string                  `json:"candidateFile"`
	}
	f.decode(readTestFileBytes(f.t, filepath.Join(f.jobs, "carry-proceed-confirmation.json")), &participant)
	if participant.Authority != "authorized isolated test participant" || participant.Decision == "" || participant.CandidateFile != path || !canaryValuesEqual(participant.Displayed, candidate) {
		f.t.Fatal("latest requirement has no independent complete participant confirmation")
	}
	decision := candidate.Event.Decide
	if candidate.Root != f.project || candidate.Run.RunID != terminal.RunID || decision == nil || decision.Choice != "proceed" || decision.Requirement == nil || decision.Requirement.Classification != protocol.RequirementClassificationPreserved || candidate.Intake == nil || candidate.Intake.RequirementDocument == nil {
		f.t.Fatal("carry candidate does not identify this original preserved requirement confirmation")
	}
	record, found := terminal.Events[string(candidate.Event.ID)]
	acceptance, err := record.Replay(candidate.Event)
	if !found || err != nil || acceptance.Status != "ACCEPTED" || acceptance.RequestID != string(decision.Request) {
		f.t.Fatalf("latest candidate has no original accepted event: %v", err)
	}
	for _, material := range candidate.Intake.Materials {
		actual := readTestFileBytes(f.t, filepath.Join(f.project, filepath.FromSlash(material.Path)))
		content := strings.ReplaceAll(string(actual), "\r\n", "\n")
		if content != material.Content || fmt.Sprintf("%x", sha256.Sum256([]byte(content))) != material.Revision {
			f.t.Fatal("latest confirmed full material or exact revision differs from its original file")
		}
	}
	if candidate.Intake.Revision != decision.Requirement.CandidateRevision || terminal.RequirementRevision() != candidate.Intake.Revision || terminal.Validation == nil || !canaryValuesEqual(terminal.Validation.Document, *candidate.Intake.RequirementDocument) {
		f.t.Fatal("terminal latest requirement differs from the independently confirmed candidate projection")
	}
	projection, err := requirement.ParseRequirementProjection(candidate.Intake.RequirementDocument.Path, []byte(candidate.Intake.Content))
	if err != nil || !canaryValuesEqual(projection, *candidate.Intake.RequirementDocument) {
		f.t.Fatal("confirmed candidate projection differs from its complete original content", err)
	}
	return protocol.NewGuaranteeActivation(candidate.Intake.Revision, f.intake.SolutionRevision, projection)
}

func (f *automaticNativeFlow) assertNativeSources() {
	f.t.Helper()
	jobs, handlers := 0, map[string]bool{}
	usages := map[string]cost.Usage{}
	turns := map[string]int64{}
	unavailable := map[string]bool{}
	type nativeRequest struct {
		Engine struct {
			RunID          string                `json:"runId"`
			WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
		} `json:"engine"`
	}
	if err := filepath.WalkDir(filepath.Join(f.project, facade.EngineNamespace), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if entry.Name() == "native-request.json" {
			directory := filepath.Dir(path)
			if _, err := os.Stat(filepath.Join(directory, "source.json")); os.IsNotExist(err) {
				if _, err := os.Stat(filepath.Join(directory, "native-error.json")); err != nil {
					return err
				}
				var request nativeRequest
				f.decode(readTestFileBytes(f.t, path), &request)
				unavailable[request.Engine.RunID+"/"+request.Engine.WorkerMaterial.ActionID] = true
			}
		}
		if entry.Name() != "source.json" {
			return nil
		}
		directory := filepath.Dir(path)
		if _, err := os.Stat(filepath.Join(directory, "native-request.json")); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		source, err := NativeJobSource("zcode", directory)
		if err != nil {
			return err
		}
		if _, _, _, err := NativeResultEvidence("zcode", directory, source); err != nil {
			return err
		}
		if NativeJobAlive(directory) {
			return fmt.Errorf("native job remains alive at terminal: %s", directory)
		}
		var request nativeRequest
		f.decode(readTestFileBytes(f.t, filepath.Join(directory, "native-request.json")), &request)
		var material worker.Material
		f.decode(readTestFileBytes(f.t, request.Engine.WorkerMaterial.Path), &material)
		if material.Input.Pending.ActionID != request.Engine.WorkerMaterial.ActionID || material.Input.RunID != request.Engine.RunID || material.Input.Provider != "zcode" {
			return fmt.Errorf("original native source lost its issued action/run/provider: %s", directory)
		}
		handlers[material.Input.Handler] = true
		transcript, _ := source["transcript"].(string)
		usage, err := cost.ParseTranscript("zcode", transcript)
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(readTestFileBytes(f.t, transcript), []byte("\n")) {
			var event struct {
				Session string `json:"sessionId"`
				Turn    string `json:"turnId"`
				Type    string `json:"type"`
				Payload struct {
					Usage struct {
						Source   string `json:"source"`
						Requests int64  `json:"modelRequestCount"`
						Input    int64  `json:"inputTokens"`
						Output   int64  `json:"outputTokens"`
						Cache    int64  `json:"cacheReadTokens"`
					} `json:"usage"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &event) != nil || event.Type != "turn.completed" && event.Type != "turn.failed" || event.Payload.Usage.Source != "provider" {
				continue
			}
			key := event.Session + "/" + event.Turn
			if requests, seen := turns[key]; seen {
				if requests != event.Payload.Usage.Requests {
					return fmt.Errorf("original native turn has conflicting request counts: %s", key)
				}
				continue
			}
			turns[key] = event.Payload.Usage.Requests
			dispatch := request.Engine.RunID + "/" + request.Engine.WorkerMaterial.ActionID
			value := usages[dispatch]
			value.InputCacheHitTokens += event.Payload.Usage.Cache
			value.InputCacheMissTokens += event.Payload.Usage.Input - event.Payload.Usage.Cache
			value.OutputTokens += event.Payload.Usage.Output
			usages[dispatch] = value
		}
		jobs++
		f.save(fmt.Sprintf("native-source-%d.json", jobs), map[string]any{"directory": directory, "source": source, "usage": usage, "request": request})
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	for _, handler := range []string{"engine.review.worker.v2", "engine.readiness.plan.v4", "engine.development.batch.v4", "engine.qa.design.v5", "engine.qa.review.rationale.v2", "engine.qa.execution.v3", "engine.gate.v2", "engine.validation.observation.v2", "engine.repair.v5"} {
		if !handlers[handler] {
			f.t.Fatal("default consumer did not preserve a required actual native model responsibility", handler)
		}
	}
	for dispatch, usage := range usages {
		runID, actionID, _ := strings.Cut(dispatch, "/")
		ledger := f.run.Cost
		if runID != f.run.RunID {
			ledger = f.run.Termination.SliceReceipts[runID].Receipt.Cost
		}
		if ledger == nil {
			f.t.Fatal("actual native source has no original cost ledger", dispatch)
		}
		entry := ledger.Dispatches[actionID]
		sourceMatches := entry.Source == cost.SourceTranscript || entry.Source == cost.SourceUnavailable && unavailable[dispatch] && entry.Reason != ""
		if !sourceMatches || entry.InputCacheHitTokens != usage.InputCacheHitTokens || entry.InputCacheMissTokens != usage.InputCacheMissTokens || entry.OutputTokens != usage.OutputTokens {
			f.t.Fatal("original cost differs from deduplicated actual native provider turns", dispatch, entry, usage)
		}
	}
	var requests int64
	for _, count := range turns {
		requests += count
	}
	f.save("native-model-proof.json", map[string]any{"completedNativeJobs": jobs, "verifiedNativeTurns": len(turns), "verifiedBackendRequests": requests, "actualHandlers": handlers, "originalCostMatchesVerifiedSources": true, "unavailableCostDispatches": unavailable, "cost": f.run.Cost})
}
