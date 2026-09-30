package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/validate"
)

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
	f, _, err := facade.Start(facade.StartOptions{Root: root, Request: facade.StartRequest{
		RunID: "writer-exclusive", Route: "regular", BaseSnapshot: "native-base",
		DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt,
	}, Admission: &facade.Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
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
