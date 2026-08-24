package whitebox_qa

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

// 用例：包内 authoring 定义表编译-编码后与 checked-in definitions/workflow.json
// 逐字节一致，digest 与 identity_gen.go 的同源常量一致，版本信封为 "2"——
// 制品身份的唯一来源是生成动作，不允许人工双写漂移。
func TestDefinitionArtifactIdentityMatchesCheckedInBytes(t *testing.T) {
	checkedIn := readCheckedInArtifact(t)

	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile definition.Workflow: %v", err)
	}
	if cd.MissingEngineAdapter {
		t.Fatal("shipped definition must not carry MISSING_ENGINE_ADAPTER")
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(data, checkedIn) {
		t.Fatal("compiled artifact differs from checked-in definitions/workflow.json")
	}
	if got, want := encoder.Digest(data), definition.WorkflowDefinitionDigest; got != want {
		t.Fatalf("artifact digest = %s, identity constant = %s", got, want)
	}
	if got, want := string(cd.Version), definition.WorkflowDefinitionVersion; got != want || got != "2" {
		t.Fatalf("definition version = %s, identity constant = %s, want \"2\"", got, want)
	}
	// checked-in 制品必须可解码（信封常量与物化一致性双向成立）。
	if _, err := encoder.Decode(checkedIn); err != nil {
		t.Fatalf("checked-in artifact must decode: %v", err)
	}
}

// 用例：Generate 的同一生成动作在临时根目录复现两个 checked-in 交付物，
// 逐字节零 diff（generated-artifact freshness，独立于 round-trip）。
func TestGenerateReproducesCheckedInArtifactsZeroDiff(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	if err := definition.Generate(tmp); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	artifactWant := readCheckedInArtifact(t)
	artifactGot, err := os.ReadFile(filepath.Join(tmp, "definitions", "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(artifactGot, artifactWant) {
		t.Fatal("generated definitions/workflow.json differs from checked-in artifact (stale artifact)")
	}

	identityWant, err := os.ReadFile(filepath.Join(root, "internal", "engine", "definition", "identity_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	identityGot, err := os.ReadFile(filepath.Join(tmp, "internal", "engine", "definition", "identity_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(identityGot, identityWant) {
		t.Fatal("generated identity_gen.go differs from checked-in identity constants (manual double-write drift)")
	}
}

// 用例：gen-definition 命令端到端——从快照源码构建生成器二进制，在两个独立
// 进程/工作目录分别执行，产物逐字节相同且等于 checked-in 制品与身份常量
// （跨进程、重复构建确定性）。
func TestGenDefinitionCommandDeterministicAcrossProcesses(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	// Windows 上可执行文件必须带 .exe 后缀（CreateProcess 不认无后缀名）。
	binName := "gen-definition"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(tmp, binName)
	build := exec.Command("go", "build", "-o", bin, "./cmd/gen-definition")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/gen-definition: %v\n%s", err, out)
	}

	runs := make([][]byte, 2)
	for i := range runs {
		workdir := t.TempDir()
		cmd := exec.Command(bin)
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %d: gen-definition: %v\n%s", i, err, out)
		}
		data, err := os.ReadFile(filepath.Join(workdir, "definitions", "workflow.json"))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		runs[i] = data
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Fatal("two gen-definition processes produced different artifact bytes")
	}
	if !bytes.Equal(runs[0], readCheckedInArtifact(t)) {
		t.Fatal("gen-definition artifact differs from checked-in definitions/workflow.json")
	}
	if got, want := encoder.Digest(runs[0]), definition.WorkflowDefinitionDigest; got != want {
		t.Fatalf("gen-definition digest = %s, identity constant = %s", got, want)
	}
}
