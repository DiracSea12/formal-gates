// determinism_acceptance_test.go 覆盖批次 3 验收的跨进程确定性：真实生成
// 入口（cmd/gen-definition，`go run ./cmd/gen-definition` 的同一 main）以两个
// 独立子进程各执行一次，产出逐字节相同的两个交付物，且与 checked-in 制品
// 逐字节一致。
//
// 不直接断言 `go run ./cmd/gen-definition` 本身：该命令只能在仓库根解析包
// 路径，且 Generate 以 CWD 为写盘根，会在测试中改写 checked-in 工作树文件。
// 等价做法是先把同一入口构建成二进制，再在两个隔离的临时工作目录各作为
// 独立进程运行——被测的生成代码路径完全相同，且两次执行分属不同 OS 进程
// （不同堆、不同地址空间），时间/路径/随机性不得进入制品字节。
package acceptance_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

// runGenerator 运行生成器二进制（CWD=workdir，即 Generate 的写盘根）。
func runGenerator(t *testing.T, bin, workdir string) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Dir = workdir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generator run in %s: %v\n%s", workdir, err, out)
	}
}

// readRun 读取一次生成运行产出的两个交付物。
func readRun(t *testing.T, workdir string) (artifact, identity []byte) {
	t.Helper()
	artifact, err := os.ReadFile(filepath.Join(workdir, "definitions", "workflow.json"))
	if err != nil {
		t.Fatalf("read generated artifact: %v", err)
	}
	identity, err = os.ReadFile(filepath.Join(workdir, "internal", "engine", "definition", "identity_gen.go"))
	if err != nil {
		t.Fatalf("read generated identity: %v", err)
	}
	return artifact, identity
}

// TestAcceptanceCrossProcessGenerationDeterministic：同一生成入口的两个独立
// 子进程，制品与身份常量逐字节一致，并与 checked-in 交付物一致。
func TestAcceptanceCrossProcessGenerationDeterministic(t *testing.T) {
	root := repoRoot(t)

	bin := filepath.Join(t.TempDir(), "gen-definition")
	build := exec.Command("go", "build", "-o", bin, "./cmd/gen-definition")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, out)
	}

	dirA, dirB := t.TempDir(), t.TempDir()
	runGenerator(t, bin, dirA)
	runGenerator(t, bin, dirB)

	artifactA, identityA := readRun(t, dirA)
	artifactB, identityB := readRun(t, dirB)
	if !bytes.Equal(artifactA, artifactB) {
		t.Error("definitions/workflow.json differs between two generator processes")
	}
	if !bytes.Equal(identityA, identityB) {
		t.Error("identity_gen.go differs between two generator processes")
	}

	// 与 checked-in 交付物逐字节一致；digest 与同源身份常量一致。
	checkedInArtifact, err := os.ReadFile(filepath.Join(root, "definitions", "workflow.json"))
	if err != nil {
		t.Fatalf("read checked-in artifact: %v", err)
	}
	checkedInIdentity, err := os.ReadFile(filepath.Join(root, "internal", "engine", "definition", "identity_gen.go"))
	if err != nil {
		t.Fatalf("read checked-in identity: %v", err)
	}
	if !bytes.Equal(artifactA, checkedInArtifact) {
		t.Errorf("generated artifact differs from checked-in definitions/workflow.json:\n--- generated ---\n%s\n--- checked-in ---\n%s", artifactA, checkedInArtifact)
	}
	if !bytes.Equal(identityA, checkedInIdentity) {
		t.Errorf("generated identity differs from checked-in identity_gen.go:\n--- generated ---\n%s\n--- checked-in ---\n%s", identityA, checkedInIdentity)
	}
	if digest := encoder.Digest(artifactA); digest != definition.WorkflowDefinitionDigest {
		t.Errorf("generated digest = %s, want identity constant %s", digest, definition.WorkflowDefinitionDigest)
	}
}
