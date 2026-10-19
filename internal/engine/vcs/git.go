// Package vcs owns native repository operations and their observed content identities.
package vcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// Runner executes a native command in its workspace and returns stdout.
// An empty directory selects the calling process working directory.
type Runner interface {
	Run(dir, command string, args ...string) (string, error)
	RunRaw(dir, command string, input []byte, args ...string) ([]byte, error)
}

// ExecRunner 是生产 runner：直接 exec，stderr 作错误详情。
type ExecRunner struct{}

func (r ExecRunner) Run(dir, command string, args ...string) (string, error) {
	body, err := r.RunRaw(dir, command, nil, args...)
	return strings.TrimSpace(string(body)), err
}

func (ExecRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	if runtime.GOOS == "windows" && command == "git" {
		args = gitWindowsArgs(dir, args)
	}
	if command == "p4" {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("p4 working directory: %w", err)
		}
		args = append([]string{"-d", absolute}, args...)
	}
	cmd := exec.Command(command, args...)
	if cmd.Err != nil {
		return nil, fmt.Errorf("%s: %w", command, cmd.Err)
	}
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("%s: cwd=%q argv=%q: %s", command, cmd.Dir, cmd.Args, detail)
	}
	return stdout.Bytes(), nil
}

func gitWindowsArgs(dir string, args []string) []string {
	options := []string{"-c", "core.longpaths=true"}
	if body, err := os.ReadFile(filepath.Join(dir, ".git")); err == nil {
		if gitdir, ok := strings.CutPrefix(string(body), "gitdir: "); ok {
			gitdir = strings.TrimRight(gitdir, "\r\n")
			// Repository discovery precedes core.longpaths configuration.
			options = append(options, "--git-dir="+gitdir, "--work-tree=.")
		}
	}
	return append(options, args...)
}

// Provider 绑定一个工作区根并提供该工作区的快照操作。零值不可用；New 构造。
type Provider struct {
	root         string
	configRoot   string
	name         string
	handle       WorkspaceHandle
	runner       Runner
	runtimeFiles RuntimeFiles
}

// New 返回绑定 root 的 Git provider；runner 为 nil 时用 exec。
func New(root string, runner Runner) *Provider {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Provider{root: root, configRoot: root, name: "git", runner: runner}
}

// Root 返回绑定的仓库根（登记制的隔离工作区各自构造 Provider）。
func (p *Provider) Root() string { return p.root }

var gitIdentityPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)

// runtimeStatusDirs 是 formal-gates 的保留运行期目录（git status 路径形
// 式）：由 CLI 自身创建/维护，不是交付文件；宿主仓库未必预先忽略它
// 们。清单包含引擎自身必建的
// .gates/engine/ run 目录。清单单源持有，逐路径豁免读这一份（.gates/
// token-cap.json 是宿主维护的项目级配置，引擎只读不建，不属本清单）。
var runtimeStatusDirs = []string{
	".gates/engine/", ".gates/tmp/", ".gates/run-locks/", ".gates/qa-isolation/", ".gates/slices/", ".gates/results/",
}

// Status 返回排除运行期未跟踪产物后的 porcelain 状态（git status
// --porcelain=v1 --untracked-files=all）。只剔运行期路径的 "?? " 行；这些
// 路径被跟踪后出现修改（非 "??" 行）仍照常作为未提交变更报告。
func (p *Provider) Status() (string, error) {
	if p.name != "git" {
		return p.nativeStatus()
	}
	pending, err := p.runner.Run(p.root, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("cannot inspect unsubmitted git changes: %w", err)
	}
	return p.excludeRuntimeUntracked(pending), nil
}

// VerifyReady 要求工作区无未提交内容（含未跟踪交付文件）；非空即拒绝。
func (p *Provider) VerifyReady() error {
	pending, err := p.Status()
	if err != nil {
		return err
	}
	if strings.TrimSpace(pending) != "" {
		return fmt.Errorf("unsubmitted %s changes must be committed before recording a snapshot (including any untracked files that must be added)", p.name)
	}
	return nil
}

// StagedOnly 报告未提交内容是否恰为纯暂存形态：porcelain 主体非空且每行都
// 是已暂存差异（index 侧非空、工作区侧为空、无未跟踪）。squash 的
// reset --soft 已完成而 commit 未执行的组织中断中间态正是这一形状（index
// 持候选树、工作区与 index 一致）；未暂存或未跟踪的用户修改不属于它。读
// --branch 变体：首行 "## " 头保证 XY 状态列不位于输出开头——共享 runner 会
// 裁剪整个 stdout 的首尾空白，纯 " M" 行一旦居首会丢列。
func (p *Provider) StagedOnly() (bool, error) {
	if p.name != "git" {
		return p.nativeTracked()
	}
	pending, err := p.runner.Run(p.root, "git", "status", "--porcelain=v1", "--branch", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("cannot inspect unsubmitted git changes: %w", err)
	}
	_, staged := gitStatusState(p.excludeRuntimeUntracked(pending))
	return staged, nil
}

func gitStatusState(status string) (clean, staged bool) {
	clean = true
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "## ") {
			continue
		}
		clean = false
		if len(line) < 2 || line[0] == ' ' || line[0] == '?' || line[1] != ' ' {
			return false, false
		}
		staged = true
	}
	return clean, staged
}

// Resolve 返回当前 HEAD 的提交身份（40/64 位 hex，小写规范化）。
func (p *Provider) Resolve() (string, error) {
	if p.name != "git" {
		return p.nativeResolve("")
	}
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
	if p.name != "git" {
		return p.nativeVerify(identity)
	}
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
	if p.name != "git" {
		return p.nativeDigest(rev)
	}
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

// StagedTreeDigest 返回暂存区的树身份（git write-tree，只读）。干净 index 的
// 暂存树＝HEAD 提交的树；reset --soft 后 index 保留截链前内容——晋升入口以
// 此分辨「reset 已完成、commit 未执行」的组织中断中间态。
func (p *Provider) StagedTreeDigest() (string, error) {
	if p.name != "git" {
		return "", fmt.Errorf("%s has no immutable index tree", p.name)
	}
	if err := p.verifyRoot(); err != nil {
		return "", err
	}
	digest, err := p.runner.Run(p.root, "git", "write-tree")
	if err != nil {
		return "", fmt.Errorf("cannot resolve the staged tree: %w", err)
	}
	digest = strings.TrimSpace(digest)
	if !gitIdentityPattern.MatchString(digest) {
		return "", fmt.Errorf("git returned an invalid tree identity %q", digest)
	}
	return strings.ToLower(digest), nil
}

// IsAncestorOrEqual 要求 ancestor 是 descendant 的祖先或与之相等。
func (p *Provider) IsAncestorOrEqual(ancestor, descendant string) error {
	if p.name != "git" {
		return p.nativeAncestor(ancestor, descendant)
	}
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
	if p.name != "git" {
		return p.nativeCount(base, head)
	}
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
	if p.name != "git" {
		return fmt.Errorf("%s snapshots are not squashed", p.name)
	}
	if _, err := p.runner.Run(p.root, "git", "reset", "--soft", base); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	if _, err := p.runner.Run(p.root, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	return nil
}

// Commit records the exact staged tree with its bound operation message.
func (p *Provider) Commit(message string) error {
	if p.name != "git" {
		return p.nativeCommit(message)
	}
	if _, err := p.runner.Run(p.root, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("cannot commit: %w", err)
	}
	return nil
}

func (p *Provider) verifyRoot() error {
	if p.name != "git" {
		return p.nativeRoot()
	}
	resolved, err := p.runner.Run(p.root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("cannot resolve git working root (the directory may not be a git working copy): %w", err)
	}
	if !SamePath(resolved, p.root) {
		return fmt.Errorf("git working root %q does not match repository root %q", resolved, p.root)
	}
	return nil
}

// excludeRuntimeUntracked 移除 porcelain 输出中属于运行期路径的
// "?? <path>" 行；其余行（含运行期路径被跟踪后的修改）原样保留。
func (p *Provider) excludeRuntimeUntracked(status string) string {
	lines := strings.Split(status, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "?? ") {
			path := normalizeStatusPath(strings.TrimSpace(line[3:]))
			if p.isRuntimeStatusPath(path) {
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

// RuntimePath reports tool-managed project settings, their lock, and runtime
// directories and descendants. Status callers exclude only untracked paths.
func RuntimePath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == ".gates/config.json" || path == ".gates/config.json.lock" {
		return true
	}
	for _, dir := range runtimeStatusDirs {
		if path == strings.TrimSuffix(dir, "/") || strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}

// RuntimeFiles carries the verified installed inventory and installer-created
// empty resource directories. It grants no exclusion to their other descendants.
type RuntimeFiles struct {
	Files            []string `json:"files,omitempty"`
	EmptyDirectories []string `json:"emptyDirectories,omitempty"`
}

// ContainsFile compares only verified installed leaves, preserving user files
// beside them and through their canonical native path identity.
func (files RuntimeFiles) ContainsFile(path string) bool {
	return slices.Contains(files.Files, canonicalPath(path))
}

func (p *Provider) WithRuntimeFiles(files RuntimeFiles) *Provider {
	p.runtimeFiles = files
	return p
}

func (p *Provider) isRuntimeStatusPath(path string) bool {
	return RuntimePath(path) || p.runtimeFiles.ContainsFile(materialPath(p.root, path))
}

func (p *Provider) isEmptyRuntimeDirectory(path string) bool {
	return slices.Contains(p.runtimeFiles.EmptyDirectories, canonicalPath(materialPath(p.root, path)))
}

// SamePath compares native directory identity, including a missing leaf whose
// existing parent resolves through a symlink. Cleanup uses it after deletion.
func SamePath(a, b string) bool {
	actual, actualErr := os.Stat(a)
	bound, boundErr := os.Stat(b)
	if actualErr == nil && boundErr == nil && os.SameFile(actual, bound) {
		return true
	}
	a, b = canonicalPath(a), canonicalPath(b)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func canonicalPath(path string) string {
	path = absPath(path)
	parent := path
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			suffix, _ := filepath.Rel(parent, path)
			return filepath.Join(resolved, suffix)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return path
		}
		parent = next
	}
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
