//go:build qastructure

package validate

import (
	"strings"
	"testing"
)

// 结构测试，覆盖「修复现有实现缺陷」run 的三项已确认需求：缺陷 1（白盒
// 测试代码进快照）、缺陷 4（未跟踪交付文件检测）、缺陷 3（删除 legacy "qa"
// 兼容）。

// structureScriptedNativeRunner 是自包含的原生命令 runner，用于驱动 VerifyReady
// 的 git 脏检查。每次 Run 返回下一个脚本化输出，模仿 VerifyReady 驱动的 git
// 子进程（包内另有 vcs_test.go 的 scriptedNativeRunner，本文件自带一份）。
type structureScriptedNativeRunner struct {
	outputs []string
	calls   [][]string
}

func (r *structureScriptedNativeRunner) Run(_ string, command string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	idx := len(r.calls) - 1
	if idx >= len(r.outputs) {
		return "", nil
	}
	return r.outputs[idx], nil
}

func structureGitResolver(t *testing.T, outputs ...string) nativeVCSResolver {
	t.Helper()
	resolver, err := resolverForVCS("git", &structureScriptedNativeRunner{outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

// 缺陷 4：快照脏检查以 porcelain --untracked-files=all 逐路径检测未跟踪且未忽略文
// 件，新增交付文件漏 git add 必须在快照前明确报错。

func TestWhiteboxVerifyReadyRejectsUntrackedDeliveryFile(t *testing.T) {
	resolver := structureGitResolver(t, "/repo", "?? delivery-new.txt")
	err := resolver.VerifyReady("/repo")
	if err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-ignored delivery file was accepted by the snapshot dirty check: %v", err)
	}
}

func TestWhiteboxVerifyReadyAcceptsCleanWorktree(t *testing.T) {
	resolver := structureGitResolver(t, "/repo", "")
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("clean worktree was rejected: %v", err)
	}
}

func TestWhiteboxVerifyReadyIgnoresUntrackedRuntimeDirectories(t *testing.T) {
	runtimeOnly := "?? .gates/tmp/run-1/state.json\n?? .gates/qa-isolation/run-1/\n?? .gates/slices/run-1/\n?? .gates/results/old-run.json"
	resolver := structureGitResolver(t, "/repo", runtimeOnly)
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("untracked formal-gates runtime directories blocked a snapshot: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsMixedUntracked(t *testing.T) {
	mixed := "?? .gates/tmp/run-1/state.json\n?? delivery-new.txt"
	resolver := structureGitResolver(t, "/repo", mixed)
	if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked delivery file was accepted alongside runtime directories: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsTrackedRuntimeModification(t *testing.T) {
	// 豁免只针对「未跟踪」的运行期产物；运行期目录下被跟踪文件的修改（" M " 行）仍须拦截。
	resolver := structureGitResolver(t, "/repo", " M .gates/tmp/run-1/state.json")
	if err := resolver.VerifyReady("/repo"); err == nil {
		t.Fatalf("tracked modification under a runtime directory was accepted: %v", err)
	}
}

func TestWhiteboxIsRuntimeGitStatusPath(t *testing.T) {
	for _, path := range []string{".gates/tmp", ".gates/tmp/run-1/state.json", ".gates/run-locks", ".gates/run-locks/run-1.lock", ".gates/qa-isolation/run-1/", ".gates/slices/run-1/", ".gates/results/old-run.json"} {
		if !isRuntimeGitStatusPath(path) {
			t.Fatalf("runtime path %q not recognized as a reserved runtime directory", path)
		}
	}
	// 折叠条目 ".gates"/".gates/" 在路径层永远判非运行期：状态以
	// --untracked-files=all 获取，git 已把未跟踪文件逐路径展开（不存在折叠条目；
	// 见 vcs_test.go 的 TestNativeVCSResolverExemptsRuntimePathsPerUntrackedEntry）。
	for _, path := range []string{"delivery-new.txt", ".gates", ".gates/", ".gates/other/run.json", "internal/code.go", ".gates/tmp-notes.md"} {
		if isRuntimeGitStatusPath(path) {
			t.Fatalf("non-runtime path %q misclassified as a reserved runtime directory", path)
		}
	}
}

// 缺陷 1：白盒测试代码推进路径（方案 A）。whiteboxTestCodeAdvancement 仅在开发完成、
// 白盒已选中、白盒 per-mode 设计已记录且派发绑当前快照时为真；推进后一次性关闭。
