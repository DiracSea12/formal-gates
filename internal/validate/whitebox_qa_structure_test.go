//go:build phase0whitebox

package validate

import (
	"strings"
	"testing"
)

// whiteboxScriptedNativeRunner 是自包含的原生命令 runner，用于驱动 VerifyReady
// 的 git 脏检查。它独立于 development-worker 的 scriptedNativeRunner：每次 Run 返回
// 下一个脚本化输出，模仿 VerifyReady 驱动的 git 子进程。
type whiteboxScriptedNativeRunner struct {
	outputs []string
	calls   [][]string
}

func (r *whiteboxScriptedNativeRunner) Run(_ string, command string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	idx := len(r.calls) - 1
	if idx >= len(r.outputs) {
		return "", nil
	}
	return r.outputs[idx], nil
}

func whiteboxGitResolver(t *testing.T, outputs ...string) nativeVCSResolver {
	t.Helper()
	resolver, err := resolverForVCS("git", &whiteboxScriptedNativeRunner{outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func TestWhiteboxVerifyReadyRejectsUntrackedDeliveryFile(t *testing.T) {
	resolver := whiteboxGitResolver(t, "/repo", "?? delivery-new.txt")
	err := resolver.VerifyReady("/repo")
	if err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-ignored delivery file was accepted by the snapshot dirty check: %v", err)
	}
}

func TestWhiteboxVerifyReadyAcceptsCleanWorktree(t *testing.T) {
	resolver := whiteboxGitResolver(t, "/repo", "")
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("clean worktree was rejected: %v", err)
	}
}

func TestWhiteboxVerifyReadyIgnoresUntrackedRuntimeDirectories(t *testing.T) {
	runtimeOnly := "?? .gates/tmp/run-1/state.json\n?? .gates/qa-isolation/run-1/\n?? .gates/slices/run-1/\n?? .gates/results/old-run.json"
	resolver := whiteboxGitResolver(t, "/repo", runtimeOnly)
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("untracked formal-gates runtime directories blocked a snapshot: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsMixedUntracked(t *testing.T) {
	mixed := "?? .gates/tmp/run-1/state.json\n?? delivery-new.txt"
	resolver := whiteboxGitResolver(t, "/repo", mixed)
	if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked delivery file was accepted alongside runtime directories: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsTrackedRuntimeModification(t *testing.T) {
	// 豁免只针对「未跟踪」的运行期产物；运行期目录下被跟踪文件的修改（" M " 行）仍须拦截。
	resolver := whiteboxGitResolver(t, "/repo", " M .gates/tmp/run-1/state.json")
	if err := resolver.VerifyReady("/repo"); err == nil {
		t.Fatalf("tracked modification under a runtime directory was accepted: %v", err)
	}
}

func TestWhiteboxIsRuntimeGitStatusPath(t *testing.T) {
	for _, path := range []string{".gates/tmp", ".gates/tmp/run-1/state.json", ".gates/qa-isolation/run-1/", ".gates/slices/run-1/", ".gates/results/old-run.json"} {
		if !isRuntimeGitStatusPath(path) {
			t.Fatalf("runtime path %q not recognized as a reserved runtime directory", path)
		}
	}
	for _, path := range []string{"delivery-new.txt", ".gates/other/run.json", "internal/code.go", ".gates/tmp-notes.md"} {
		if isRuntimeGitStatusPath(path) {
			t.Fatalf("non-runtime path %q misclassified as a reserved runtime directory", path)
		}
	}
}
