package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/validate"
)

// frozenGitRunner 把 Git 命令面固定为「单提交（base==HEAD）、无未提交内容」
// 的冻结仓应答：按命令与参数精确区分身份——rev-parse HEAD 是提交身份，
// <rev>^{tree} 是树身份，merge-base 返回祖先实参，base==HEAD 的区间计数如实
// 为 0。regular 路线启动快照校验据此通过（真实 git 行为由
// internal/engine/vcs 的真仓冒烟覆盖）。
const frozenSnapshot = "0123456789abcdef0123456789abcdef01234567"
const frozenTree = "0123456789abcdef0123456789abcdef01234568"

type frozenGitRunner struct{}

func (frozenGitRunner) Run(dir, command string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	if command != "git" {
		return "", fmt.Errorf("frozen git fixture: unexpected command %s %s", command, joined)
	}
	switch {
	case joined == "rev-parse --show-toplevel":
		return dir, nil
	case joined == "rev-parse HEAD":
		return frozenSnapshot, nil
	case len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && strings.HasSuffix(args[2], "^{commit}"):
		return strings.TrimSuffix(args[2], "^{commit}"), nil
	case len(args) == 2 && args[0] == "rev-parse" && strings.HasSuffix(args[1], "^{tree}"):
		return frozenTree, nil
	case len(args) == 3 && args[0] == "merge-base":
		return args[1], nil
	case joined == "status --porcelain=v1 --untracked-files=all":
		return "", nil
	case len(args) == 3 && args[0] == "rev-list" && args[1] == "--count":
		if base, head, ok := strings.Cut(args[2], ".."); ok && base == head {
			return "0", nil
		}
		return "", fmt.Errorf("frozen git fixture: unexpected range %s", args[2])
	}
	return "", fmt.Errorf("frozen git fixture: unexpected command git %s", joined)
}

// startEngineRunForWriterTest 以 façade 建立真实 engine run（regular 路线，
// 独立临时 root），返回 façade 与其权威 state.json 路径。
func startEngineRunForWriterTest(t *testing.T) (*facade.Facade, string, string) {
	t.Helper()
	root := t.TempDir()
	receipt := facade.IntakeConfirmationReceipt{
		Source:            facade.DefaultIntakeSource,
		Authority:         facade.DefaultIntakeAuthority,
		Transport:         facade.DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: "rev-1",
		Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: "rev-1"}},
		SolutionRevision: "solution-rev", SolutionDigest: "sha256:solution",
	}
	f, _, err := facade.Start(facade.StartOptions{Root: root, VCSRunner: frozenGitRunner{}, Request: facade.StartRequest{
		RunID: "writer-exclusive", Route: "regular", BaseSnapshot: frozenSnapshot,
		DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt,
	}, Admission: engineWriterAdmission(t, root)})
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	return f, root, statePath
}

// TestLegacyWritersCannotReachEngineRunState：engine run 存在时，legacy 写
// 入口（requirement impact 的 UpdateRequirement writer 与 authorize-repair）
// 不可达：直接调用 legacy writer 因命名空间分离无法寻址 engine 权威状态，
// CLI 入口对 engine run 一律非零退出且不产生任何 legacy 写入——legacy 入口
// 不是 engine state 的第二 writer。
func TestLegacyWritersCannotReachEngineRunState(t *testing.T) {
	f, root, statePath := startEngineRunForWriterTest(t)
	if _, err := f.Drive(); err != nil {
		t.Fatalf("drive: %v", err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	// 直接调用 legacy requirement writer：engine run 无 legacy state，写入被
	// 拒绝（命名空间分离——engine 权威状态不可被 legacy writer 寻址）。
	if _, err := validate.UpdateRequirement(root, "", "writer-exclusive", "requirements.md", true, "meaning-preserved", nil); err == nil {
		t.Fatal("legacy UpdateRequirement accepted an engine run")
	}
	// CLI legacy 写入口：engine run 存在即非零退出，不创建 legacy state。
	for _, command := range [][]string{
		{"workflow", "requirement", "--run-id", "writer-exclusive", "--revision", "rev-2", "--root", root},
		{"workflow", "authorize-repair", "--run-id", "writer-exclusive", "--root", root, "--package-root", "."},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run("formal-gates", command, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
			t.Fatalf("%v unexpectedly succeeded: %s", command, stdout.String())
		}
		if _, err := os.Stat(filepath.Join(root, ".gates", "tmp", "writer-exclusive", "state.json")); !os.IsNotExist(err) {
			t.Fatalf("%v created legacy state: %v", command, err)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("legacy entry mutated the engine authoritative state")
	}
}

// TestRepairPreviewProjectsEngineRun：repair-preview 对 engine run 展示引擎
// 权威投影（pending REPAIR Ask＋cost/guard 投影＋三值可用性），legacy run 行
// 为不变（无 legacy state 时按 legacy 错误报告）。
func TestRepairPreviewProjectsEngineRun(t *testing.T) {
	f, root, _ := startEngineRunForWriterTest(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	face := newEngineRepairPreview(run)
	if face.RunID != "writer-exclusive" || face.Runtime != facade.RuntimeEngine {
		t.Fatalf("face = %+v", face)
	}
	if face.EngineAllowance != "UNAVAILABLE" || face.OwnerCost != "UNAVAILABLE" || face.TokenCap != "UNAVAILABLE" {
		t.Fatalf("availability = %q %q %q", face.EngineAllowance, face.OwnerCost, face.TokenCap)
	}
	if len(face.RepairAsks) != 0 {
		t.Fatalf("unexpected repair asks: %+v", face.RepairAsks)
	}
	// 序列化稳定（JSON 面向 CLI 消费者）。
	data, err := json.Marshal(face)
	if err != nil || !strings.Contains(string(data), "\"runtime\":\"engine\"") {
		t.Fatalf("marshal = %s err=%v", data, err)
	}
	// legacy run：无 legacy state，既有行为是可定位错误。
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "repair-preview", "--run-id", "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 || stderr.Len() == 0 {
		t.Fatalf("engine run without registered admission resolved the legacy preview: code=%d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("engine-run legacy preview wrote stdout: %q", stdout.String())
	}
}

// startAdmittedEngineRun 建立可经公开 CLI 打开的 engine run：真实 install 流程
// 产出稳定注册表与回执，Start 的 Admission 身份取自解析出的注册表记录（公开
// CLI 打开 engine run 的唯一合法通道）。
func startAdmittedEngineRun(t *testing.T, root string) *facade.Facade {
	t.Helper()
	source := writeInstallSource(t, "source")
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"install", "--source", source, "--host", "zcode", "--scope", "global", "--force"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("install failed (rc=%d): %s%s", code, stdout.String(), stderr.String())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(home, ".formal-gates", "registry.json")
	doc, loadErr := validate.LoadRegistry(registryPath)
	if loadErr != nil || len(doc.Records) == 0 {
		t.Fatalf("load registry: %v (%d records)", loadErr, len(doc.Records))
	}
	record := doc.Records[0]
	receiptData, err := json.Marshal(validate.BootstrapReceipt{Accepted: true, PackageDigest: record.PackageDigest, Records: []validate.RegistryRecord{record}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath+".bootstrap.json", receiptData, 0o600); err != nil {
		t.Fatal(err)
	}
	admission, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		t.Fatalf("resolve admission: %v", err)
	}
	f, _, err := facade.Start(facade.StartOptions{Root: root, VCSRunner: frozenGitRunner{}, Request: facade.StartRequest{
		RunID: "writer-exclusive", Route: "regular", BaseSnapshot: frozenSnapshot, Provider: admission.Provider,
		DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: facade.IntakeConfirmationReceipt{
			Source:            facade.DefaultIntakeSource,
			Authority:         facade.DefaultIntakeAuthority,
			Transport:         facade.DefaultIntakeTransport,
			RequirementSource: "requirements.md", RequirementRevision: "rev-1",
			Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: "rev-1"}},
			SolutionRevision: "solution-rev", SolutionDigest: "sha256:solution",
		},
	}, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// snapshotEngineRunTree 固定 engine run 目录全部文件字节（相对路径→内容）。
func snapshotEngineRunTree(t *testing.T, root, runID string) map[string]string {
	t.Helper()
	files := map[string]string{}
	dir := filepath.Join(root, facade.EngineNamespace, runID)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestLegacyWriteCommandsRejectEngineRunPerCommand：route matrix 逐命令负测——
// cli.go 落闸注册表（workflowLegacyWriteEntry）里的每个 legacy 公开写命令对
// engine run 一律 UNSUPPORTED_ENGINE_ENTRY 快速拒绝（闸在 flag 解析前），engine
// 权威 state 字节不变、不产生任何 legacy 写入。注册表增删命令时本表自动跟随
// （遍历注册表本身），计划↔矩阵的结构锁定由 route_matrix_test 持有。
func TestLegacyWriteCommandsRejectEngineRunPerCommand(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for command := range workflowLegacyWriteEntry {
		runFlag := "--run-id"
		if command == "cleanup" {
			runFlag = "--run"
		}
		var stdout, stderr bytes.Buffer
		code := Run("formal-gates", []string{"workflow", command, runFlag, "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr})
		combined := stdout.String() + stderr.String()
		if code == 0 {
			t.Errorf("workflow %s reached a legacy writer for an engine run", command)
		}
		if !strings.Contains(combined, facade.UnsupportedEngineEntry) {
			t.Errorf("workflow %s rejected without %s: %s", command, facade.UnsupportedEngineEntry, combined)
		}
		if _, err := os.Stat(filepath.Join(root, ".gates", "tmp", "writer-exclusive", "state.json")); !os.IsNotExist(err) {
			t.Errorf("workflow %s created legacy state", command)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("fenced legacy entries mutated the engine authoritative state")
	}
}

// TestPublicCleanupCannotOperateOnActiveEngineRun：公开 cleanup 不是 engine 的
// 第二清理通道——对活动 engine run 显式拒绝且 run 目录文件字节不变。
func TestPublicCleanupCannotOperateOnActiveEngineRun(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "cleanup", "--run", "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr})
	combined := stdout.String() + stderr.String()
	if code == 0 || !strings.Contains(combined, facade.UnsupportedEngineEntry) {
		t.Fatalf("public cleanup operated on an active engine run (rc=%d): %s", code, combined)
	}
	if tree := snapshotEngineRunTree(t, root, "writer-exclusive"); !reflect.DeepEqual(before, tree) {
		t.Fatal("rejected cleanup mutated the engine run directory")
	}
}

// TestAlternatingLegacyAndEngineEntriesKeepBindingLocked：同一 run 交替调用旧
// 入口与 engine 入口——legacy 写入口拒绝、engine 入口照常服务，writer/schema/
// definition 绑定（request.json＋state.json 字节）不可改。
func TestAlternatingLegacyAndEngineEntriesKeepBindingLocked(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	engineDir := filepath.Join(root, facade.EngineNamespace, "writer-exclusive")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	requestBefore, err := os.ReadFile(filepath.Join(engineDir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 交替：legacy requirement（拒）→ engine status（通）→ engine submit 无效
	// 事件（engine 侧拒绝，非落闸）→ legacy abort（拒）→ engine status（通）。
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "requirement", "--run-id", "writer-exclusive", "--root", root, "--revision", "r2", "--meaning", "meaning-preserved"}, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("legacy requirement entry accepted an engine run")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "status", "--run-id", "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("engine status failed after a rejected legacy entry: %s%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "writer-exclusive") {
		t.Fatalf("engine status lost the run identity: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "submit", "--run-id", "writer-exclusive", "--root", root}, IO{Stdin: strings.NewReader(`{"id":"probe-1","kind":"NOT_A_KIND"}`), Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("engine submit accepted an unknown event kind")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "abort", "--run-id", "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("legacy abort entry accepted an engine run")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "status", "--run-id", "writer-exclusive", "--root", root}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("engine status failed after alternating entries: %s%s", stdout.String(), stderr.String())
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	requestAfter, err := os.ReadFile(filepath.Join(engineDir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(requestBefore, requestAfter) {
		t.Fatal("alternating entries changed the writer/schema/definition binding bytes")
	}
}

func engineWriterAdmission(t *testing.T, root string) *facade.Admission {
	t.Helper()
	registryPath, target, generation, lease, token := testkit.Installation(t, root, "target-1", "sha256:package", "engine")
	a := &facade.Admission{RegistryPath: registryPath, Target: target, Root: root, Provider: "engine",
		PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1", Generation: generation, Lease: lease, Token: token}
	return a
}
