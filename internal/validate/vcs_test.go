package validate

import (
	"errors"
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
		{name: "git", identity: gitID, want: [][]string{{"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "HEAD"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "--verify", gitID + "^{commit}"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "status", "--porcelain=v1", "--untracked-files=all"}}},
		{name: "svn", identity: "123", want: [][]string{{"svn", "info", "--show-item", "wc-root", "/repo"}, {"svnversion", "/repo"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "info", "--show-item", "revision", "-r", "123", "/repo"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "status", "--quiet", "/repo"}}},
		{name: "p4", identity: "456", want: [][]string{{"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "sync", "-n", "...@456"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...@456"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "opened", "..."}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			outputs := []string{"/repo", test.identity, "/repo", test.identity, "/repo", ""}
			if test.name == "p4" {
				outputs = []string{"/repo", test.identity, "", "/repo", test.identity, "/repo", ""}
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
			if err := resolver.Verify("/repo", identity); err != nil {
				t.Fatal(err)
			}
			if err := resolver.VerifyReady("/repo"); err != nil {
				t.Fatal(err)
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

func TestNativeVCSResolverRejectsUnsubmittedChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		pending string
	}{
		{name: "git", pending: " M delivery.txt"},
		{name: "svn", pending: "M       delivery.txt"},
		{name: "p4", pending: "//depot/project/delivery.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: []string{"/repo", test.pending}}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "unsubmitted "+test.name+" changes") {
				t.Fatalf("pending changes were accepted: %v", err)
			}
		})
	}
}

// TestNativeVCSResolverRejectsUntrackedFiles verifies 缺陷 4：快照脏检查必须检测
// 未跟踪且未忽略的新交付文件（漏了 git add），存在时明确报错，不让新交付文件静默丢失。
func TestNativeVCSResolverRejectsUntrackedFiles(t *testing.T) {
	// git status --porcelain 对未跟踪文件输出 "?? <path>"；脏检查必须把它当作未提交变更拒绝。
	runner := &scriptedNativeRunner{outputs: []string{"/repo", "?? delivery-new.txt"}}
	resolver, err := resolverForVCS("git", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-ignored file was accepted by the snapshot dirty check: %v", err)
	}
	// 干净工作区（无输出）仍放行。
	clean := &scriptedNativeRunner{outputs: []string{"/repo", ""}}
	resolver, err = resolverForVCS("git", clean)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("clean worktree was rejected: %v", err)
	}
}

// TestNativeVCSResolverIgnoresUntrackedRuntimeDirectories verifies the dirty check
// still catches delivery files while not treating formal-gates' own runtime
// directories as uncommitted delivery: a host repo may not pre-gitignore
// .gates/tmp or the QA isolation worktree, yet a snapshot must not be blocked by
// the CLI's own untracked working state.
func TestNativeVCSResolverIgnoresUntrackedRuntimeDirectories(t *testing.T) {
	runtimeOnly := &scriptedNativeRunner{outputs: []string{"/repo", "?? .gates/tmp/run-1/state.json\n?? .gates/qa-isolation/run-1/\n?? .gates/slices/run-1/\n?? .gates/results/old-run.json"}}
	resolver, err := resolverForVCS("git", runtimeOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("untracked formal-gates runtime directories blocked a snapshot: %v", err)
	}

	mixed := &scriptedNativeRunner{outputs: []string{"/repo", "?? .gates/tmp/run-1/state.json\n?? delivery-new.txt"}}
	resolver, err = resolverForVCS("git", mixed)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked delivery file was accepted alongside runtime directories: %v", err)
	}

	trackedRuntime := &scriptedNativeRunner{outputs: []string{"/repo", " M .gates/tmp/run-1/state.json"}}
	resolver, err = resolverForVCS("git", trackedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err == nil {
		t.Fatalf("tracked modification under a runtime directory was accepted: %v", err)
	}
}

// TestNativeVCSResolverExemptsRuntimePathsPerUntrackedEntry verifies the
// --untracked-files=all semantics: git itself expands every untracked and
// non-ignored file into its own "?? <path>" line (folded directory entries no
// longer exist), so the dirty check's exemption is a pure per-path judgment
// against the runtime directory manifest — untracked runtime paths are
// exempted, an untracked non-runtime file is intercepted, and no directory
// content is ever re-enumerated or re-judged against git's ignore rules.
func TestNativeVCSResolverExemptsRuntimePathsPerUntrackedEntry(t *testing.T) {
	// Untracked runtime paths, one path per line exactly as -uall lists them:
	// exempted (the host has not pre-gitignored any of them).
	runtimeOnly := &scriptedNativeRunner{outputs: []string{"/repo",
		"?? .gates/tmp/run-1/state.json\n?? .gates/run-locks/run-1.lock\n?? .gates/qa-isolation/run-1/\n?? .gates/slices/run-1/\n?? .gates/results/old-run.json"}}
	resolver, err := resolverForVCS("git", runtimeOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("untracked formal-gates runtime paths blocked a snapshot: %v", err)
	}

	// An untracked non-runtime file beside the runtime paths is still
	// intercepted by the per-path rule.
	mixed := &scriptedNativeRunner{outputs: []string{"/repo",
		"?? .gates/tmp/run-1/state.json\n?? .gates/run-locks/run-1.lock\n?? delivery-new.txt"}}
	resolver, err = resolverForVCS("git", mixed)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err == nil ||
		!strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-runtime file beside runtime paths was exempted: %v", err)
	}

	// A non-runtime path directly under .gates is not exempted either: the
	// exemption never widens past the runtime manifest.
	under := &scriptedNativeRunner{outputs: []string{"/repo", "?? .gates/delivery-note.txt"}}
	resolver, err = resolverForVCS("git", under)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err == nil {
		t.Fatalf("untracked non-runtime child under .gates was exempted: %v", err)
	}
}

// TestSnapshotDirtyCheckFollowsGitIgnoreRules runs the snapshot readiness
// check against a real git repository whose ignore policy covers part of
// .gates: an ignored local note under .gates is not git-reported uncommitted
// content and must not block a clean snapshot, while the runtime lock path
// the host did NOT ignore is exempted per-path; an untracked non-runtime file
// the host did not ignore is still intercepted.
func TestSnapshotDirtyCheckFollowsGitIgnoreRules(t *testing.T) {
	root, pkg := workflowFixture(t)
	// The host ignores the runtime tmp/results directories and a local note
	// file, but NOT the persistent run-lock directory: VerifyReady explicitly
	// supports runtime directories the host has not pre-ignored.
	writeTestFile(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n.gates/results/\n.gates/local-note.md\n")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "base\n")
	commitAll(t, root, "base delivery and ignore policy")

	runIDs := []string{"ignored-policy-runtime-only", "ignored-policy-with-note"}
	for i, note := range []bool{false, true} {
		name := "runtime_lock_only"
		if note {
			name = "runtime_lock_and_ignored_local_note"
		}
		t.Run(name, func(t *testing.T) {
			if note {
				writeTestFile(t, filepath.Join(root, ".gates", "local-note.md"), "local ignored notes\n")
			}
			state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, runIDs[i]), "custom", []string{"quality"})
			dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
			writeTestFile(t, filepath.Join(root, "delivery.txt"), "new delivery\n")
			commitAll(t, root, "new delivery")
			// The framework's own persistent run lock under the NOT-ignored
			// .gates/run-locks/ is the untracked runtime path in this tree.
			if _, err := AdvanceSnapshot(root, pkg, state.RunID, dispatch, false, ""); err != nil {
				t.Fatalf("ignored local note or unignored runtime lock blocked a clean snapshot: %v", err)
			}
		})
	}

	// An untracked, non-ignored delivery file beside the same runtime lock is
	// still intercepted — the exemption never widens past the runtime manifest.
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "ignored-policy-intercept"), "custom", []string{"quality"})
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "another delivery\n")
	commitAll(t, root, "another delivery")
	writeTestFile(t, filepath.Join(root, "delivery-new.txt"), "uncommitted delivery\n")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, dispatch, false, ""); err == nil ||
		!strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-runtime file was accepted beside the runtime lock: %v", err)
	}
}

func TestP4ResolverAcceptsNoOpenedFiles(t *testing.T) {
	runner := &scriptedNativeRunner{outputs: []string{"/repo"}, errors: []error{nil, errors.New("p4: File(s) not opened on this client.")}}
	resolver, err := resolverForVCS("p4", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("standard clean P4 result was rejected: %v", err)
	}
}

func TestNativeVCSResolverAncestorOrEqual(t *testing.T) {
	gitID := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	runner := &scriptedNativeRunner{outputs: []string{gitID}}
	resolver, err := resolverForVCS("git", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.IsAncestorOrEqual("/repo", gitID, head); err != nil {
		t.Fatalf("git ancestor was rejected: %v", err)
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], []string{"git", "merge-base", gitID, head}) {
		t.Fatalf("git ancestry command shape=%v", runner.calls)
	}
	nonAncestor := &scriptedNativeRunner{outputs: []string{strings.Repeat("c", 40)}}
	resolver, _ = resolverForVCS("git", nonAncestor)
	if err := resolver.IsAncestorOrEqual("/repo", gitID, head); err == nil || !strings.Contains(err.Error(), "not an ancestor") {
		t.Fatalf("git non-ancestor was accepted: %v", err)
	}
	for _, vcs := range []string{"svn", "p4"} {
		resolver, err := resolverForVCS(vcs, &scriptedNativeRunner{})
		if err != nil {
			t.Fatal(err)
		}
		if err := resolver.IsAncestorOrEqual("/repo", "123", "456"); err != nil {
			t.Fatalf("%s earlier revision was rejected as an ancestor: %v", vcs, err)
		}
		if err := resolver.IsAncestorOrEqual("/repo", "456", "123"); err == nil || !strings.Contains(err.Error(), "not an ancestor") {
			t.Fatalf("%s later revision was accepted as an ancestor: %v", vcs, err)
		}
	}
}

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
