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

// The single confirmed artifact carries both the requirement and its plan.
func writerIntakeReceipt(t *testing.T, root string) facade.IntakeConfirmationReceipt {
	t.Helper()
	path := "requirements.md"
	content := cliStructuredRequirement("Engine entries preserve the authoritative run binding and reject legacy writes.") + "\n## 方案\n\nUse the engine namespace and its public entry points; legacy write commands must reject this run.\n"
	mustWriteCLI(t, filepath.Join(root, path), content)
	preview, err := facade.PrepareIntake(root, path, "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return preview.IntakeCandidate
}

// startEngineRunForWriterTest 以 façade 建立真实 engine run（regular 路线，
// 独立临时 root），返回 façade 与其权威 state.json 路径。
func startEngineRunForWriterTest(t *testing.T) (*facade.Facade, string, string) {
	t.Helper()
	root := t.TempDir()
	receipt := writerIntakeReceipt(t, root)
	f, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, VCSRunner: frozenGitRunner{}, Request: facade.StartRequest{
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

// TestLegacyWritersCannotReachEngineRunState：CLI 拒绝已退役的写命令，
// 不创建旧格式状态文件，也不改写 engine 权威状态。
func TestLegacyWritersCannotReachEngineRunState(t *testing.T) {
	f, root, statePath := startEngineRunForWriterTest(t)
	if _, err := f.Drive(); err != nil {
		t.Fatalf("drive: %v", err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	// 已退役写命令非零退出，不创建旧格式状态文件。
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

// startAdmittedEngineRun 建立可经公开 CLI 打开的 engine run：真实 install 流程
// 产出稳定注册表与回执，Start 的 Admission 身份取自解析出的注册表记录（公开
// CLI 打开 engine run 的唯一合法通道）。
func startAdmittedEngineRun(t *testing.T, root string) *facade.Facade {
	t.Helper()
	source := writeInstallSource(t, "source")
	homePath, homeErr := os.UserHomeDir()
	if homeErr != nil {
		t.Fatal(homeErr)
	}
	originalInvocation := os.Args[0]
	os.Args[0] = filepath.Join(homePath, ".local", "bin", "formal-gates")
	t.Cleanup(func() { os.Args[0] = originalInvocation })
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
	f, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, VCSRunner: frozenGitRunner{}, Request: facade.StartRequest{
		RunID: "writer-exclusive", Route: "regular", BaseSnapshot: frozenSnapshot, Provider: admission.Provider,
		DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: writerIntakeReceipt(t, root),
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

// Removed workflow entries are rejected before loading or writing a run.
func TestLegacyWriteCommandsRejectEngineRunPerCommand(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range retiredWorkflowCommands {
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
		if !strings.Contains(combined, "unknown workflow subcommand") {
			t.Errorf("workflow %s rejected without %s: %s", command, "unknown workflow subcommand", combined)
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
	if code == 0 || !strings.Contains(combined, "unknown workflow subcommand") {
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
	// 语义请求（engine 侧拒绝，非落闸）→ legacy abort（拒）→ engine status（通）。
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
	if code := Run("formal-gates", []string{"workflow", "next", "--run-id", "writer-exclusive", "--root", root, "--decision", "-"}, IO{Stdin: strings.NewReader(`{"request":"NOT_A_KIND"}`), Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("engine control candidate accepted an unknown request kind")
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

func (r frozenGitRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	out, err := r.Run(dir, command, args...)
	return []byte(out), err
}
