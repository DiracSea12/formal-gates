package validate

import (
	"fmt"

	"path/filepath"

	"strconv"
	"strings"

	"testing"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleStub struct {
	verification       lifecycle.Verification
	binds              [][4]string
	transcript         string
	interruptionReason string
}

func (*workflowLifecycleStub) Begin(string, string) error { return nil }

func (stub *workflowLifecycleStub) Bind(root, runID, dispatchID, identity string) error {
	stub.binds = append(stub.binds, [4]string{root, runID, dispatchID, identity})
	return nil
}

func (stub *workflowLifecycleStub) Verify(_, _, dispatchID string) (lifecycle.Verification, error) {
	result := stub.verification
	result.DispatchID = dispatchID
	return result, nil
}

func (stub *workflowLifecycleStub) TranscriptPath(_, _, _ string) (string, string, error) {
	if stub.transcript != "" {
		return lifecycle.ProviderCodex, stub.transcript, nil
	}
	return "", "", nil
}

func (stub *workflowLifecycleStub) ResolveClaimIdentity(_, _, preferred string) (string, error) {
	if strings.TrimSpace(preferred) != "" {
		return strings.TrimSpace(preferred), nil
	}
	return "", fmt.Errorf("reviewer identity is required when no subagent start observation exists")
}

func (stub *workflowLifecycleStub) InterruptionReason(_, _, _ string) (string, error) {
	return stub.interruptionReason, nil
}

// TestMergeGateRegisteredButExcludedFromNormalRouteCandidates verifies the merge
// gate is registered in the catalog but never appears in the normal route
// candidates; the normal candidates expose blackbox and whitebox QA modes.
func TestMergeGateRegisteredButExcludedFromNormalRouteCandidates(t *testing.T) {
	pkg := promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks", "merge-gate": "merge checks"})
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Gate(mergeGateID); !ok {
		t.Fatal("merge gate is not registered in the catalog")
	}
	candidates, err := PackageRouteCandidates(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range candidates {
		if id == mergeGateID || id == mergeQAID {
			t.Fatalf("merge verification appeared in normal route candidates: %v", candidates)
		}
	}
	if !containsString(candidates, blackboxQAID) || !containsString(candidates, whiteboxQAID) {
		t.Fatalf("normal route candidates missing QA modes: %v", candidates)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func workflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Requirement."))
	writeTestFile(t, filepath.Join(root, "design.md"), "design\n")
	// 测试仓库与实际仓库一致地忽略运行期临时状态：否则 .gates/tmp/ 会被误跟踪进
	// "基线到当前"交付 diff，认领后等状态写入会让工作树变脏。.gates/results 是 seal
	// 运行期产物（物化用例 / 封板 ledger），在测试里不提交，忽略它以免缺陷 4 落地后
	// 快照就绪脏检查（检测未跟踪文件）把上一次 seal 的产物误判为未提交变更。
	writeTestFile(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n.gates/results\n")
	// 白盒设计者交付的结构测试代码不在 baseline 里：它由白盒设计者在开发后主工作区
	// 编写、host 提交后经白盒测试代码推进路径进入快照（见 whiteboxDeliveredTestCode
	// 与测试 TestWhiteboxQADesignsAndReviewsAfterDevelopment /
	// TestFullRouteDesignsWhiteboxAfterDevelopment）。把测试文件写进 baseline 会掩盖
	// 真实写入路径、让白盒核心交付物静默丢失的缺陷通过测试。
	initializeGit(t, root)
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks", "merge-gate": "merge checks"})
}

func initializeGit(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "tests@example.invalid")
	runGit(t, root, "config", "user.name", "Formal Gates Tests")
	commitAll(t, root, "base")
}

func commitAll(t *testing.T, root, message string) {
	t.Helper()
	runGit(t, root, "add", "--all")
	// Test repositories must not inherit runner-level commit hooks or signing
	// prompts; neither is part of the snapshot behavior these fixtures verify.
	runGit(t, root, "commit", "--no-verify", "--no-gpg-sign", "-m", message)
}

func gitHead(t *testing.T, root string) string {
	t.Helper()
	return runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	value, err := (execNativeCommandRunner{}).Run(root, "git", args...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func commitCount(t *testing.T, root, base, head string) int {
	t.Helper()
	value := runGit(t, root, "rev-list", "--count", base+".."+head)
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// Fix 15：install 缺 source 明确报"source 必填"。
func TestInstallRequiresSource(t *testing.T) {
	if _, err := Install(InstallOptions{Host: "claude", Scope: "project", Project: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("install without source was not rejected clearly: %v", err)
	}
}

// Fix 17：svn {U+XXXX} unicode 转义转成可读文字。
func TestDecodeVCSMessageUnescapesUnicode(t *testing.T) {
	in := `svn: svn: E155007: {U+201C}/tmp/repo{U+201D}{U+4E0D}{U+662F}{U+5DE5}{U+4F5C}{U+526F}{U+672C}`
	out := decodeVCSMessage(in)
	if strings.Contains(out, "{U+") {
		t.Fatalf("unicode escapes were not decoded: %s", out)
	}
	if !strings.Contains(out, "不是工作副本") {
		t.Fatalf("decoded message lost readable text: %s", out)
	}
	if strings.Contains(out, "svn: svn:") {
		t.Fatalf("duplicated svn prefix not collapsed: %s", out)
	}
}

func TestDecodeVCSMessagePreservesRepeatedBodyWords(t *testing.T) {
	in := "svn: svn: warning: retry retry E155007: E155007: path path"
	want := "svn: warning: retry retry E155007: E155007: path path"
	if got := decodeVCSMessage(in); got != want {
		t.Fatalf("decodeVCSMessage() = %q, want %q", got, want)
	}
}
