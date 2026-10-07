package validate

import (
	"errors"
	engineVCS "formal-gates/internal/engine/vcs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeVCSResolvers(t *testing.T) {
	for _, vcs := range []string{"git", "svn", "p4"} {
		if _, err := resolverForVCS(vcs, nil); err != nil {
			t.Fatalf("resolver %s: %v", vcs, err)
		}
	}
	if _, err := resolverForVCS("other", nil); err == nil {
		t.Fatal("unsupported resolver was accepted")
	}
}

func TestNativeVCSResolverCommandShapes(t *testing.T) {
	gitID := strings.Repeat("a", 40)
	for _, test := range []struct {
		name     string
		identity string
		want     [][]string
	}{
		{name: "git", identity: gitID, want: [][]string{{"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "HEAD"}}},
		{name: "svn", identity: "123", want: [][]string{{"svn", "info", "--show-item", "wc-root", "/repo"}, {"svnversion", "/repo"}}},
		{name: "p4", identity: "456", want: [][]string{{"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "sync", "-n", "...@456"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			outputs := []string{"/repo", test.identity}
			if test.name == "p4" {
				outputs = []string{"/repo", test.identity, ""}
			}
			runner := &scriptedNativeRunner{outputs: outputs}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := resolver.Resolve("/repo")
			if err != nil || identity != test.identity {
				t.Fatalf("resolve identity=%q err=%v", identity, err)
			}
			if !reflect.DeepEqual(runner.calls, test.want) {
				t.Fatalf("calls=%v want=%v", runner.calls, test.want)
			}
		})
	}
}

func TestNativeVCSResolverRejectsNonUniformWorkspace(t *testing.T) {
	for _, test := range []struct {
		name    string
		outputs []string
		want    string
	}{
		{name: "svn", outputs: []string{"/repo", "2:3"}, want: "not at one uniform revision"},
		{name: "p4", outputs: []string{"/repo", "456", "//depot/project/older.txt"}, want: "not uniformly synced"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: test.outputs}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.Resolve("/repo"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("non-uniform workspace was accepted: %v", err)
			}
		})
	}
}

func TestVerifyReadyExemptsEngineRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "mixed-generation@example.invalid")
	runGit(t, root, "config", "user.name", "Mixed Generation Test")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "base\n")
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "base delivery")
	writeTestFile(t, filepath.Join(root, ".gates", "engine", "mixed-run", "state.json"), "{}\n")
	if err := engineVCS.New(root, nil).VerifyReady(); err != nil {
		t.Fatalf("engine runtime directory blocked the legacy readiness check: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "delivery-new.txt"), "uncommitted delivery\n")
	if err := engineVCS.New(root, nil).VerifyReady(); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("untracked delivery file was accepted beside engine runtime files: %v", err)
	}
}

// commitDeliveryPaths commits only the explicitly named delivery paths with
// the same commit hygiene commitAll applies (no hooks, no signing). The shared
// commitAll helper sweeps git add --all, which would also commit the
// persistent run lock a live run has already created, emptying git status
// before the readiness check and voiding the untracked-runtime-lock scenarios
// below. Other consumers depend on that --all semantics, so this test commits
// explicit paths instead of changing the shared helper.
func commitDeliveryPaths(t *testing.T, root, message string, paths ...string) {
	t.Helper()
	runGit(t, root, append([]string{"add", "--"}, paths...)...)
	runGit(t, root, "commit", "--no-verify", "--no-gpg-sign", "-m", message)
}

// requireRunLockUntracked asserts the precondition these scenarios exist to
// exercise: right before the snapshot readiness check runs, git still reports
// the framework's persistent run lock (never deleted, and not pre-ignored by
// this host's .gitignore) as untracked content.
func requireRunLockUntracked(t *testing.T, root, runID string) {
	t.Helper()
	lockPath := ".gates/run-locks/" + runID + ".lock"
	status := runGit(t, root, "status", "--porcelain=v1", "--untracked-files=all")
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "?? "+lockPath {
			return
		}
	}
	t.Fatalf("precondition failed: git does not report the runtime lock %s as untracked; git status:\n%s", lockPath, status)
}

// TestSnapshotDirtyCheckFollowsGitIgnoreRules runs the snapshot readiness
// check against a real git repository whose ignore policy covers part of
// .gates: an ignored local note under .gates is not git-reported uncommitted
// content and must not block a clean snapshot, while the runtime lock path
// the host did NOT ignore is exempted per-path; an untracked non-runtime file
// the host did not ignore is still intercepted.
func TestSVNResolveIgnoresBaseStatusSuffix(t *testing.T) {
	// 隔离工作区注入当前需求文档后工作树被修改，svnversion 返回 "123M"；身份校验取其
	// BASE 版本级（123），M 后缀不影响。
	for _, output := range []string{"123M", "123MS", "123"} {
		runner := &scriptedNativeRunner{outputs: []string{"/repo", output}}
		resolver, err := resolverForVCS("svn", runner)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := resolver.Resolve("/repo")
		if err != nil || identity != "123" {
			t.Fatalf("svnversion %q: identity=%q err=%v", output, identity, err)
		}
	}
	// 混合版本范围仍是非均匀工作区，即便带 M 后缀也被拒绝。
	runner := &scriptedNativeRunner{outputs: []string{"/repo", "2:3M"}}
	resolver, err := resolverForVCS("svn", runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("/repo"); err == nil || !strings.Contains(err.Error(), "not at one uniform revision") {
		t.Fatalf("mixed svn version range with M suffix was accepted: %v", err)
	}
}

func TestNativeVCSResolverPropagatesCommandFailure(t *testing.T) {
	runner := &scriptedNativeRunner{errors: []error{errors.New("not a working copy")}}
	resolver, err := resolverForVCS("svn", runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("/repo"); err == nil || !strings.Contains(err.Error(), "not a working copy") {
		t.Fatalf("native error was lost: %v", err)
	}
}

type scriptedNativeRunner struct {
	outputs []string
	errors  []error
	calls   [][]string
}

func (r *scriptedNativeRunner) Run(_ string, command string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	index := len(r.calls) - 1
	if index < len(r.errors) && r.errors[index] != nil {
		return "", r.errors[index]
	}
	if index >= len(r.outputs) {
		return "", nil
	}
	return r.outputs[index], nil
}
