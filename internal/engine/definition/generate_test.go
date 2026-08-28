package definition

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/validate"
)

// repoRoot 返回仓库根（测试 CWD 是本包目录）。go.mod 存在性校验防止在错误
// 目录下静默通过。
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return root
}

func readCheckedIn(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read checked-in %s: %v", rel, err)
	}
	return data
}

// compileCanonical 编译定义表得到 canonical IR 与制品字节。
func compileCanonical(t *testing.T) (*compiler.CompiledDefinition, []byte) {
	t.Helper()
	cd, err := compiler.Compile(Workflow(), Registry())
	if err != nil {
		t.Fatalf("compile workflow: %v", err)
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode workflow: %v", err)
	}
	return cd, data
}

// TestGenerateFreshness 是 ADR-001 验收 1（generated-artifact freshness，
// 独立于 round-trip 的 CI 检查）：调用生成入口后，两个 checked-in 交付物
// 必须零 diff。
func TestGenerateFreshness(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, rel := range []string{artifactPath, identityPath} {
		generated, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read generated %s: %v", rel, err)
		}
		checkedIn := readCheckedIn(t, rel)
		if !bytes.Equal(generated, checkedIn) {
			t.Errorf("checked-in %s is stale; regenerate with: go run ./cmd/gen-definition\n--- generated ---\n%s\n--- checked-in ---\n%s",
				rel, generated, checkedIn)
		}
	}
}

// TestGenerateDeterministic：同一生成动作重复执行产出逐字节相同的结果
// （无时间/路径/随机性进入交付物）。
func TestGenerateDeterministic(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := Generate(first); err != nil {
		t.Fatalf("generate first: %v", err)
	}
	if err := Generate(second); err != nil {
		t.Fatalf("generate second: %v", err)
	}
	for _, rel := range []string{artifactPath, identityPath} {
		a, err := os.ReadFile(filepath.Join(first, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(second, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between repeated generation runs", rel)
		}
	}
}

// TestWriteGeneratedAtomic（封板后审计 H5）：writeGenerated 必须以
// temp+rename 原子替换——并发读者只能观察到完整的旧字节或完整的新字节，
// 不存在撕裂中间态；不得残留临时文件，权限保持 0644。字节内容与直写
// 逐字节相同由 TestGenerateFreshness 对 checked-in 交付物的零 diff 保证。
func TestWriteGeneratedAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "artifact.json")
	stale := []byte("{\"stale\":true}\n")
	fresh := []byte("{\"fresh\":true,\"payload\":\"0123456789abcdef0123456789abcdef\"}\n")
	if err := writeGenerated(path, stale); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	stop := make(chan struct{})
	torn := make(chan string, 1)
	var reader sync.WaitGroup
	reader.Add(1)
	go func() {
		defer reader.Done()
		for {
			b, err := os.ReadFile(path)
			if err != nil {
				select {
				case <-stop:
					return
				default:
					continue // 目标文件尚未首次落位；rename 原子性下不存在 ENOENT 窗口
				}
			}
			if !bytes.Equal(b, stale) && !bytes.Equal(b, fresh) {
				select {
				case torn <- fmt.Sprintf("torn read observed partial bytes: %q", b):
				default:
				}
				return
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	for i := 0; i < 300; i++ {
		if err := writeGenerated(path, fresh); err != nil {
			t.Fatalf("write fresh: %v", err)
		}
		if err := writeGenerated(path, stale); err != nil {
			t.Fatalf("write stale: %v", err)
		}
	}
	close(stop)
	reader.Wait()
	select {
	case msg := <-torn:
		t.Fatal(msg)
	default:
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".gen-definition-") {
			t.Fatalf("temporary file %q left behind after writeGenerated", e.Name())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows 的 NTFS 不保留 POSIX 权限位（可写文件 Stat 呈现为 0666），断言
	// 退化为"未被置只读"；POSIX 上仍钉死 0644。
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Fatalf("artifact mode = %v, want owner-writable", info.Mode().Perm())
		}
	} else if info.Mode().Perm() != 0o644 {
		t.Fatalf("artifact mode = %v, want 0644", info.Mode().Perm())
	}
}

// TestArtifactIndependentOfAssemblyAndRegistrationOrder 是 ADR-001 验收 2：
// 步骤 assembly 顺序与 registry 注册顺序都不进入制品——不同顺序编译产生
// 相同字节与相同 digest。
func TestArtifactIndependentOfAssemblyAndRegistrationOrder(t *testing.T) {
	_, canonical := compileCanonical(t)
	canonicalDigest := encoder.Digest(canonical)

	// 反向 assembly：同一批步骤逆序输入编译器。
	reversed := Workflow()
	for i, j := 0, len(reversed.Steps)-1; i < j; i, j = i+1, j-1 {
		reversed.Steps[i], reversed.Steps[j] = reversed.Steps[j], reversed.Steps[i]
	}
	revCD, err := compiler.Compile(reversed, Registry())
	if err != nil {
		t.Fatalf("compile reversed assembly: %v", err)
	}
	revData, err := encoder.Encode(revCD)
	if err != nil {
		t.Fatalf("encode reversed assembly: %v", err)
	}
	if !bytes.Equal(revData, canonical) {
		t.Error("artifact bytes differ between assembly orders")
	}
	if encoder.Digest(revData) != canonicalDigest {
		t.Error("artifact digest differs between assembly orders")
	}

	// 反向注册：同一批 registry 条目逆序注册（内部测试可见包内注册表）。
	reg := compiler.NewRegistry()
	for i := len(workflowAskKinds) - 1; i >= 0; i-- {
		if err := reg.RegisterAskKind(workflowAskKinds[i]); err != nil {
			t.Fatalf("register ask kind: %v", err)
		}
	}
	for i := len(workflowOperations) - 1; i >= 0; i-- {
		if err := reg.RegisterOperation(workflowOperations[i]); err != nil {
			t.Fatalf("register operation: %v", err)
		}
	}
	for i := len(workflowSchemas) - 1; i >= 0; i-- {
		if err := reg.RegisterSchema(workflowSchemas[i]); err != nil {
			t.Fatalf("register schema: %v", err)
		}
	}
	for i := len(workflowReconciles) - 1; i >= 0; i-- {
		if err := reg.RegisterReconciler(workflowReconciles[i]); err != nil {
			t.Fatalf("register reconciler: %v", err)
		}
	}
	for i := len(workflowPredicates) - 1; i >= 0; i-- {
		if err := reg.RegisterPredicate(workflowPredicates[i]); err != nil {
			t.Fatalf("register predicate: %v", err)
		}
	}
	for i := len(workflowCodecs) - 1; i >= 0; i-- {
		if err := reg.RegisterCodec(workflowCodecs[i]); err != nil {
			t.Fatalf("register codec: %v", err)
		}
	}
	for i := len(workflowHandlers) - 1; i >= 0; i-- {
		h := workflowHandlers[i]
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler %q: %v", h.id, err)
		}
	}
	revRegCD, err := compiler.Compile(Workflow(), reg)
	if err != nil {
		t.Fatalf("compile reversed registry: %v", err)
	}
	revRegData, err := encoder.Encode(revRegCD)
	if err != nil {
		t.Fatalf("encode reversed registry: %v", err)
	}
	if !bytes.Equal(revRegData, canonical) {
		t.Error("artifact bytes differ between registration orders")
	}
	if encoder.Digest(revRegData) != canonicalDigest {
		t.Error("artifact digest differs between registration orders")
	}
}

// TestCheckedInArtifactRoundTrip：checked-in 制品 decode→encode 字节不变
// （ADR-001 验收 3 在真实制品上的执行），且 IR 与直接编译结果一致。
func TestCheckedInArtifactRoundTrip(t *testing.T) {
	checkedIn := readCheckedIn(t, artifactPath)
	decoded, err := encoder.Decode(checkedIn)
	if err != nil {
		t.Fatalf("decode checked-in artifact: %v", err)
	}
	reencoded, err := encoder.Encode(decoded)
	if err != nil {
		t.Fatalf("re-encode decoded artifact: %v", err)
	}
	if !bytes.Equal(reencoded, checkedIn) {
		t.Errorf("round-trip changed artifact bytes:\n--- re-encoded ---\n%s\n--- checked-in ---\n%s", reencoded, checkedIn)
	}
	if encoder.Digest(checkedIn) != WorkflowDefinitionDigest {
		t.Errorf("checked-in digest = %s, identity constant = %s", encoder.Digest(checkedIn), WorkflowDefinitionDigest)
	}
	canonical, err := compiler.Compile(Workflow(), Registry())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !reflect.DeepEqual(canonical, decoded) {
		t.Error("decoded IR differs from freshly compiled IR")
	}
}

// TestCheckedInArtifactIsFutureDefinition：衔接 internal/validate/future.go
// 的 LoadFutureDefinition——版本 bump 到 "2" 后，新制品解析路径（读
// stateSchemaVersion/version 字段）仍然成立：作为新 future 候选被接受，
// 不触发 "changed bytes without bump" 拒绝，且 digest 与同源常量一致。
func TestCheckedInArtifactIsFutureDefinition(t *testing.T) {
	future, err := validate.LoadFutureDefinition(repoRoot(t))
	if err != nil {
		t.Fatalf("LoadFutureDefinition rejected the v2 artifact: %v", err)
	}
	if future.SchemaVersion != encoder.StateSchemaVersion {
		t.Errorf("stateSchemaVersion = %q, want %q", future.SchemaVersion, encoder.StateSchemaVersion)
	}
	if future.WorkflowVersion != WorkflowDefinitionVersion {
		t.Errorf("workflowDefinitionVersion = %q, want %q", future.WorkflowVersion, WorkflowDefinitionVersion)
	}
	if future.Digest != WorkflowDefinitionDigest {
		t.Errorf("future digest = %q, identity constant = %q", future.Digest, WorkflowDefinitionDigest)
	}
	if future.Source != validate.CurrentWorkflowDefinitionSource {
		t.Errorf("definition source = %q, want %q", future.Source, validate.CurrentWorkflowDefinitionSource)
	}
}

// TestWorkflowStepVersionsBound：定义表每个步骤的版本与信封绑定（编译器
// 强制；此处固化表本身的约定，防止后续编辑定义表时漂移）。
func TestWorkflowStepVersionsBound(t *testing.T) {
	cd, err := compiler.Compile(Workflow(), Registry())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, cs := range cd.Steps {
		if cs.Header.DefinitionVersion != Version {
			t.Errorf("step %q definitionVersion = %q, want %q", cs.Header.ID, cs.Header.DefinitionVersion, Version)
		}
	}
	if cd.Version != Version {
		t.Errorf("definition version = %q, want %q", cd.Version, Version)
	}
	if cd.EntryNode != entryNode {
		t.Errorf("entry node = %q, want %q", cd.EntryNode, entryNode)
	}
}
