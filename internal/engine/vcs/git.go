// Package vcs 是 engine 侧的 Git 快照与提交操作面：值级镜像 internal/validate
// vcs.go 的 git 分支语义（快照身份、就绪检查、祖先、区间计数、squash），并补
// 候选 freeze/promotion 所需的 track/commit/diff（legacy 无对应实现，宿主侧
// 唯一先例是 internal/validate canary.go 的 git add --all＋commit）。protocol
// 层零 exec：本包经 Runner 注入（生产 exec、测试替身），engine 只消费结果。
// SVN/P4 不在本阶段。
package vcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Runner 执行一条工作区命令并返回 stdout（与 legacy vcs.go 的
// nativeCommandRunner 同形状）。dir 允许为空表示当前目录。
type Runner interface {
	Run(dir, command string, args ...string) (string, error)
}

// ExecRunner 是生产 runner：直接 exec，stderr 作错误详情。
type ExecRunner struct{}

func (ExecRunner) Run(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s: %s", command, detail)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Provider 绑定一个工作区根并提供该工作区的快照操作。零值不可用；New 构造。
type Provider struct {
	root   string
	runner Runner
}

// New 返回绑定 root 的 Git provider；runner 为 nil 时用 exec。
func New(root string, runner Runner) *Provider {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Provider{root: root, runner: runner}
}

// Root 返回绑定的仓库根（登记制的隔离工作区各自构造 Provider）。
func (p *Provider) Root() string { return p.root }

var gitIdentityPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)

// runtimeGitStatusDirs 是 formal-gates 的保留运行期目录（git status 路径形
// 式）：由 CLI 自身创建/维护，不是交付文件；宿主仓库未必预先 gitignore 它
// 们。判据是「谁创建」而非逐字镜像 legacy 清单——含引擎自身必建的
// .gates/engine/ run 目录。清单单源持有，逐路径豁免读这一份（.gates/
// token-cap.json 是宿主维护的项目级配置，引擎只读不建，不属本清单）。
var runtimeGitStatusDirs = []string{
	".gates/engine/", ".gates/tmp/", ".gates/run-locks/", ".gates/qa-isolation/", ".gates/slices/", ".gates/results/",
}

// Status 返回排除运行期未跟踪产物后的 porcelain 状态（git status
// --porcelain=v1 --untracked-files=all）。只剔运行期目录下的 "?? " 行；这些
// 路径被跟踪后出现修改（非 "??" 行）仍照常作为未提交变更报告。
func (p *Provider) Status() (string, error) {
	pending, err := p.runner.Run(p.root, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("cannot inspect unsubmitted git changes: %w", err)
	}
	return excludeRuntimeUntracked(pending), nil
}

// VerifyReady 要求工作区无未提交内容（含未跟踪交付文件）；非空即拒绝。
func (p *Provider) VerifyReady() error {
	pending, err := p.Status()
	if err != nil {
		return err
	}
	if strings.TrimSpace(pending) != "" {
		return fmt.Errorf("unsubmitted git changes must be committed before recording a snapshot (including any untracked files that must be added)")
	}
	return nil
}

// Resolve 返回当前 HEAD 的提交身份（40/64 位 hex，小写规范化）。
func (p *Provider) Resolve() (string, error) {
	if err := p.verifyRoot(); err != nil {
		return "", err
	}
	identity, err := p.runner.Run(p.root, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("cannot resolve git snapshot: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if !gitIdentityPattern.MatchString(identity) {
		return "", fmt.Errorf("git returned an invalid immutable identity %q", identity)
	}
	return strings.ToLower(identity), nil
}

// Verify 校验 identity 是可复现的提交对象（rev-parse --verify <id>^{commit}
// 且解析结果与 identity 相等）。
func (p *Provider) Verify(identity string) error {
	identity = strings.TrimSpace(identity)
	if !gitIdentityPattern.MatchString(identity) {
		return fmt.Errorf("invalid git snapshot identity %q", identity)
	}
	if err := p.verifyRoot(); err != nil {
		return err
	}
	resolved, err := p.runner.Run(p.root, "git", "rev-parse", "--verify", identity+"^{commit}")
	if err != nil {
		return fmt.Errorf("cannot verify git snapshot %s: %w", identity, err)
	}
	if !strings.EqualFold(strings.TrimSpace(resolved), identity) {
		return fmt.Errorf("git cannot reproduce snapshot %s", identity)
	}
	return nil
}

// TreeDigest 返回 rev 指向提交的树身份（git rev-parse <rev>^{tree}）：候选与
// 晋升共用的内容摘要原生形态——同一棵树经不同提交历史组织（squash）内容仍
// 相等。
func (p *Provider) TreeDigest(rev string) (string, error) {
	if err := p.verifyRoot(); err != nil {
		return "", err
	}
	digest, err := p.runner.Run(p.root, "git", "rev-parse", rev+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("cannot resolve git tree of %s: %w", rev, err)
	}
	digest = strings.TrimSpace(digest)
	if !gitIdentityPattern.MatchString(digest) {
		return "", fmt.Errorf("git returned an invalid tree identity %q", digest)
	}
	return strings.ToLower(digest), nil
}

// IsAncestorOrEqual 要求 ancestor 是 descendant 的祖先或与之相等。
func (p *Provider) IsAncestorOrEqual(ancestor, descendant string) error {
	resolved, err := p.runner.Run(p.root, "git", "merge-base", ancestor, descendant)
	if err != nil {
		return fmt.Errorf("cannot resolve git ancestry of %s: %w", ancestor, err)
	}
	if !strings.EqualFold(strings.TrimSpace(resolved), ancestor) {
		return fmt.Errorf("git snapshot %s is not an ancestor of the current snapshot", ancestor)
	}
	return nil
}

// CommitCount 统计 base..head 区间内的提交数（不含 base、含 head）；超过一个
// 提交的区间才需要 squash。
func (p *Provider) CommitCount(base, head string) (int, error) {
	output, err := p.runner.Run(p.root, "git", "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("git rev-list returned a non-numeric count %q", output)
	}
	return count, nil
}

// SquashToBase 把 base..HEAD 压成单个提交并保持最终树不变：reset --soft
// <base> 把整段差异压入暂存区，再以 message 重新提交；中间提交悬空（审计证据
// 是恒等的最终树＋run 摘要）。
func (p *Provider) SquashToBase(base, message string) error {
	if _, err := p.runner.Run(p.root, "git", "reset", "--soft", base); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	if _, err := p.runner.Run(p.root, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	return nil
}

// TrackAll 把工作区当前变更全部纳入暂存（git add --all）：候选 freeze 的收口
// 动作，语义＝把工作区当前变更全部纳入候选提交。
func (p *Provider) TrackAll() error {
	if _, err := p.runner.Run(p.root, "git", "add", "--all"); err != nil {
		return fmt.Errorf("cannot stage working tree changes: %w", err)
	}
	return nil
}

// Commit 以 message 提交暂存区（与 TrackAll 配对使用）。
func (p *Provider) Commit(message string) error {
	if _, err := p.runner.Run(p.root, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("cannot commit: %w", err)
	}
	return nil
}

// DiffNames 列出 base..head 的变更路径（每行一个，按 git 输出原序）：候选
// freeze/promotion 据此描述候选包含的交付面。
func (p *Provider) DiffNames(base, head string) ([]string, error) {
	output, err := p.runner.Run(p.root, "git", "diff", "--name-only", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("cannot list changed paths %s..%s: %w", base, head, err)
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

func (p *Provider) verifyRoot() error {
	resolved, err := p.runner.Run(p.root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("cannot resolve git working root (the directory may not be a git working copy): %w", err)
	}
	if !samePath(resolved, p.root) {
		return fmt.Errorf("git working root %q does not match repository root %q", resolved, p.root)
	}
	return nil
}

// excludeRuntimeUntracked 移除 porcelain 输出中位于保留运行期目录下的
// "?? <path>" 行；其余行（含运行期路径被跟踪后的修改）原样保留。
func excludeRuntimeUntracked(status string) string {
	lines := strings.Split(status, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "?? ") {
			path := normalizeStatusPath(strings.TrimSpace(line[3:]))
			if isRuntimeStatusPath(path) {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// normalizeStatusPath 剥 porcelain 的可选引号并统一分隔符，使 "?? <path>"
// 的路径可与保留目录清单比较。
func normalizeStatusPath(path string) string {
	path = strings.Trim(path, `"`)
	return strings.ReplaceAll(path, `\`, "/")
}

func isRuntimeStatusPath(path string) bool {
	for _, dir := range runtimeGitStatusDirs {
		if path == strings.TrimSuffix(dir, "/") || strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}

// samePath 比较两个路径的绝对规范化形态（符号链接解析后；Windows 大小写不
// 敏感）。
func samePath(a, b string) bool {
	a, b = absPath(a), absPath(b)
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func absPath(path string) string {
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}
