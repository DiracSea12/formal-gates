package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubRunner 按 argv 前缀返回预置输出（测试替身；断言真实命令面）。
type stubRunner struct {
	responses map[string]string // 子命令空格连接的关键字 → 输出
	failures  map[string]string // 关键字 → 错误文本
	commands  [][]string        // 依序记录全部调用
}

func (s *stubRunner) Run(dir, command string, args ...string) (string, error) {
	argv := append([]string{command}, args...)
	key := strings.Join(argv, " ")
	s.commands = append(s.commands, argv)
	for prefix, msg := range s.failures {
		if strings.HasPrefix(key, prefix) {
			return "", &stubError{msg}
		}
	}
	for prefix, out := range s.responses {
		if strings.HasPrefix(key, prefix) {
			return out, nil
		}
	}
	return "", &stubError{"stub: unexpected command " + key}
}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

const headIdentity = "0123456789abcdef0123456789abcdef01234567"

func newStub(responses, failures map[string]string) (*Provider, *stubRunner) {
	runner := &stubRunner{responses: responses, failures: failures}
	return New("/repo", runner), runner
}

func TestResolveValidatesIdentityShape(t *testing.T) {
	px := map[string]string{
		"valid 40 hex":  headIdentity,
		"valid 64 hex":  headIdentity + "0123456789abcdef01234567",
		"not hex":       "zz456789abcdef0123456789abcdef01234567",
		"short":         "abc",
		"empty":         "",
		"mixed numeric": "12345",
	}
	for name, identity := range px {
		t.Run(name, func(t *testing.T) {
			p, runner := newStub(map[string]string{
				"git rev-parse --show-toplevel": "/repo",
				"git rev-parse HEAD":            identity,
			}, nil)
			got, err := p.Resolve()
			if valid := name == "valid 40 hex" || name == "valid 64 hex"; valid {
				if err != nil || got != strings.ToLower(identity) {
					t.Fatalf("resolve = %q, %v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "invalid immutable identity") {
				t.Fatalf("resolve accepted %q: %v", identity, err)
			}
			if len(runner.commands) == 0 {
				t.Fatal("stub runner saw no commands")
			}
		})
	}
}

func TestVerifyRejectsInvalidAndUnreproducibleIdentities(t *testing.T) {
	t.Run("invalid identity shape", func(t *testing.T) {
		p, _ := newStub(nil, nil)
		if err := p.Verify("abc"); err == nil || !strings.Contains(err.Error(), "invalid git snapshot identity") {
			t.Fatalf("verify accepted malformed identity: %v", err)
		}
	})
	t.Run("rev-parse fails", func(t *testing.T) {
		p, _ := newStub(map[string]string{"git rev-parse --show-toplevel": "/repo"},
			map[string]string{"git rev-parse --verify": "not an object"})
		if err := p.Verify(headIdentity); err == nil || !strings.Contains(err.Error(), "cannot verify git snapshot") {
			t.Fatalf("verify error = %v", err)
		}
	})
	t.Run("resolved identity differs", func(t *testing.T) {
		p, _ := newStub(map[string]string{
			"git rev-parse --show-toplevel": "/repo",
			"git rev-parse --verify":        "fedcba9876543210fedcba9876543210fedcba98",
		}, nil)
		if err := p.Verify(headIdentity); err == nil || !strings.Contains(err.Error(), "cannot reproduce snapshot") {
			t.Fatalf("verify error = %v", err)
		}
	})
	t.Run("reproducible identity passes case-insensitively", func(t *testing.T) {
		p, _ := newStub(map[string]string{
			"git rev-parse --show-toplevel": "/repo",
			"git rev-parse --verify":        strings.ToUpper(headIdentity),
		}, nil)
		if err := p.Verify(headIdentity); err != nil {
			t.Fatal(err)
		}
	})
}

func TestVerifyRootRejectsForeignRepositoryRoot(t *testing.T) {
	p, _ := newStub(map[string]string{"git rev-parse --show-toplevel": "/elsewhere"}, nil)
	if _, err := p.Resolve(); err == nil || !strings.Contains(err.Error(), "does not match repository root") {
		t.Fatalf("resolve error = %v", err)
	}
	p2, _ := newStub(nil, map[string]string{"git rev-parse --show-toplevel": "boom"})
	if _, err := p2.Resolve(); err == nil || !strings.Contains(err.Error(), "cannot resolve git working root") {
		t.Fatalf("resolve error = %v", err)
	}
}

func TestIsAncestorOrEqualComparesMergeBaseResult(t *testing.T) {
	t.Run("ancestor resolves to itself", func(t *testing.T) {
		p, _ := newStub(map[string]string{"git merge-base": headIdentity}, nil)
		if err := p.IsAncestorOrEqual(headIdentity, "head"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("divergent base", func(t *testing.T) {
		p, _ := newStub(map[string]string{"git merge-base": "fedcba9876543210fedcba9876543210fedcba98"}, nil)
		if err := p.IsAncestorOrEqual(headIdentity, "head"); err == nil || !strings.Contains(err.Error(), "is not an ancestor of the current snapshot") {
			t.Fatalf("ancestry error = %v", err)
		}
	})
	t.Run("merge-base failure", func(t *testing.T) {
		p, _ := newStub(nil, map[string]string{"git merge-base": "no such commit"})
		if err := p.IsAncestorOrEqual(headIdentity, "head"); err == nil || !strings.Contains(err.Error(), "cannot resolve git ancestry") {
			t.Fatalf("ancestry error = %v", err)
		}
	})
}

func TestCommitCountParsesRevListOutput(t *testing.T) {
	p, _ := newStub(map[string]string{"git rev-list --count": "2"}, nil)
	if n, err := p.CommitCount("base", "head"); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	p2, _ := newStub(map[string]string{"git rev-list --count": "two"}, nil)
	if _, err := p2.CommitCount("base", "head"); err == nil || !strings.Contains(err.Error(), "non-numeric count") {
		t.Fatalf("count error = %v", err)
	}
}

func TestStatusExcludesOnlyRuntimeUntrackedEntries(t *testing.T) {
	p, _ := newStub(map[string]string{"git status": strings.Join([]string{
		" M tracked.go",
		"?? .gates/engine/run-1/state.json",
		"?? .gates/tmp/run-1/state.json",
		"?? .gates/run-locks/run-1.lock",
		"?? .gates/qa-isolation/case/x.md",
		"?? .gates/slices/a.json",
		"?? .gates/results/a.json",
		"?? deliverable.go",
	}, "\n")}, nil)
	got, err := p.Status()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{" M tracked.go", "?? deliverable.go"}, "\n")
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if err := p.VerifyReady(); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("ready accepted pending changes: %v", err)
	}
	// 运行期路径被跟踪后出现修改（非 "?? " 行）仍照常拦截——只豁免未跟踪
	// 的运行期产物。
	p2, _ := newStub(map[string]string{"git status": " M .gates/engine/run-1/state.json"}, nil)
	if err := p2.VerifyReady(); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("tracked runtime modification escaped readiness: %v", err)
	}
}

func TestStagedTreeDigestResolvesWriteTree(t *testing.T) {
	t.Run("resolves and normalizes the staged identity", func(t *testing.T) {
		p, runner := newStub(map[string]string{
			"git rev-parse --show-toplevel": "/repo",
			"git write-tree":                strings.ToUpper(headIdentity),
		}, nil)
		got, err := p.StagedTreeDigest()
		if err != nil || got != headIdentity {
			t.Fatalf("staged digest = %q, %v", got, err)
		}
		if len(runner.commands) == 0 {
			t.Fatal("stub runner saw no commands")
		}
	})
	t.Run("rejects non-identity output", func(t *testing.T) {
		p, _ := newStub(map[string]string{
			"git rev-parse --show-toplevel": "/repo",
			"git write-tree":                "not a tree",
		}, nil)
		if _, err := p.StagedTreeDigest(); err == nil || !strings.Contains(err.Error(), "invalid tree identity") {
			t.Fatalf("staged digest error = %v", err)
		}
	})
	t.Run("write-tree failure wraps", func(t *testing.T) {
		p, _ := newStub(map[string]string{"git rev-parse --show-toplevel": "/repo"},
			map[string]string{"git write-tree": "index unusable"})
		if _, err := p.StagedTreeDigest(); err == nil || !strings.Contains(err.Error(), "cannot resolve the staged tree") {
			t.Fatalf("staged digest failure = %v", err)
		}
	})
}

// TestStagedOnlyClassifiesPorcelainLines：纯暂存形态判定＝porcelain 主体非空
// 且每行都是已暂存差（index 侧非空、工作区侧为空）；未暂存（含居首行）、未
// 跟踪、混合与空主体（含仅剩运行期未跟踪豁免行）都不是。
func TestStagedOnlyClassifiesPorcelainLines(t *testing.T) {
	cases := map[string]struct {
		status string
		want   bool
	}{
		"pure staged":            {"## master\nM  a.go\nA  b.go\nR  old -> new", true},
		"unstaged first line":    {"## master\n M a.go", false},
		"unstaged later line":    {"## master\nM  a.go\n M b.go", false},
		"staged and unstaged":    {"## master\nMM a.go", false},
		"staged plus untracked":  {"## master\nM  a.go\n?? new.go", false},
		"untracked only":         {"## master\n?? new.go", false},
		"header only":            {"## master", false},
		"runtime untracked only": {"## master\n?? .gates/engine/run-1/promotion.lock", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := newStub(map[string]string{"git status": tc.status}, nil)
			got, err := p.StagedOnly()
			if err != nil || got != tc.want {
				t.Fatalf("staged-only = %t, %v; want %t", got, err, tc.want)
			}
		})
	}
}

// realRepo 建立真 git 仓（exec 冒烟；无模型调用）。
func realRepo(t *testing.T) *Provider {
	t.Helper()
	root := t.TempDir()
	p := New(root, nil)
	run := func(args ...string) {
		t.Helper()
		if _, err := p.runner.Run(root, "git", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init")
	run("config", "user.email", "engine-vcs@example.invalid")
	run("config", "user.name", "Engine VCS Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".gates/tmp/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--all")
	run("commit", "-m", "base")
	return p
}

func TestRealRepositorySmokeCoversProviderOperations(t *testing.T) {
	p := realRepo(t)
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(base); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyReady(); err != nil {
		t.Fatalf("clean repository not ready: %v", err)
	}
	// 运行期未跟踪产物不阻断就绪；交付文件（未跟踪或被跟踪修改）阻断。
	if err := os.MkdirAll(filepath.Join(p.Root(), ".gates/tmp/run-1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root(), ".gates/tmp/run-1/state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyReady(); err != nil {
		t.Fatalf("runtime untracked artifact blocked readiness: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.Root(), "deliverable.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyReady(); err == nil {
		t.Fatal("untracked deliverable did not block readiness")
	}
	paths, err := p.ChangedPaths()
	if err != nil || len(paths) != 1 || paths[0] != "deliverable.go" {
		t.Fatalf("changed paths = %v, %v", paths, err)
	}
	if err := p.Track(paths); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("candidate work"); err != nil {
		t.Fatal(err)
	}
	head, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.IsAncestorOrEqual(base, head); err != nil {
		t.Fatal(err)
	}
	if n, err := p.CommitCount(base, head); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	changes, err := p.Changes(base, head)
	if err != nil || len(changes) != 1 || changes[0].Path != "deliverable.go" {
		t.Fatalf("native changes = %v, %v", changes, err)
	}
	// 追加第二个提交后区间为 2；squash 压回 1 且最终树不变（差异面为空）。
	if err := os.WriteFile(filepath.Join(p.Root(), "deliverable.go"), []byte("package main // touched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err = p.ChangedPaths()
	if err != nil || len(paths) != 1 || paths[0] != "deliverable.go" {
		t.Fatalf("changed paths = %v, %v", paths, err)
	}
	if err := p.Track(paths); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("second commit"); err != nil {
		t.Fatal(err)
	}
	head, err = p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.CommitCount(base, head); err != nil || n != 2 {
		t.Fatalf("count after second commit = %d, %v", n, err)
	}
	if err := p.SquashToBase(base, "organized candidate"); err != nil {
		t.Fatal(err)
	}
	squashed, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.CommitCount(base, squashed); err != nil || n != 1 {
		t.Fatalf("count after squash = %d, %v", n, err)
	}
	if err := p.IsAncestorOrEqual(base, squashed); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyReady(); err != nil {
		t.Fatalf("squash left pending changes: %v", err)
	}
	if squashed == head {
		t.Fatal("squash produced the pre-squash identity")
	}
	// 暂存树只读探针：干净 index 的暂存树＝HEAD 提交的树。
	staged, err := p.StagedTreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	squashTree, err := p.TreeDigest(squashed)
	if err != nil {
		t.Fatal(err)
	}
	if staged != squashTree {
		t.Fatalf("clean staged tree = %s, want the HEAD tree %s", staged, squashTree)
	}
	// 纯暂存形态：reset --soft 回 base 后 commit 未执行的组织中断中间态。
	if _, err := p.runner.Run(p.Root(), "git", "reset", "-q", "--soft", base); err != nil {
		t.Fatal(err)
	}
	stagedOnly, err := p.StagedOnly()
	if err != nil || !stagedOnly {
		t.Fatalf("soft-reset midstate staged-only = %t (%v)", stagedOnly, err)
	}
	// 叠加未暂存修改后不再属于该形态。
	if err := os.WriteFile(filepath.Join(p.Root(), "app.go"), []byte("package main // user edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stagedOnly, err := p.StagedOnly(); err != nil || stagedOnly {
		t.Fatalf("unstaged modification classified staged-only = %t (%v)", stagedOnly, err)
	}
}

func (s *stubRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	out, err := s.Run(dir, command, args...)
	return []byte(out), err
}
