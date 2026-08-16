package validate

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type nativeCommandRunner interface {
	Run(dir, command string, args ...string) (string, error)
}

type execNativeCommandRunner struct{}

func (execNativeCommandRunner) Run(dir, command string, args ...string) (string, error) {
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

type nativeVCSResolver interface {
	Resolve(root string) (string, error)
	Verify(root, identity string) error
	VerifyReady(root string) error
	IsAncestorOrEqual(root, ancestor, descendant string) error
}

type commandVCSResolver struct {
	name   string
	runner nativeCommandRunner
}

var gitIdentityPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
var numericIdentityPattern = regexp.MustCompile(`^[0-9]+$`)
var vcsUnicodeEscape = regexp.MustCompile(`\{U\+([0-9A-Fa-f]{1,6})\}`)

// decodeVCSMessage converts {U+XXXX} unicode escapes that some VCS tools (svn)
// emit for non-ASCII characters back into readable runes, and collapses a
// duplicated leading VCS prefix (e.g. "svn: svn:") so the message stays
// legible without removing repeated words from the message body.
func decodeVCSMessage(message string) string {
	decoded := vcsUnicodeEscape.ReplaceAllStringFunc(message, func(match string) string {
		hex := vcsUnicodeEscape.FindStringSubmatch(match)
		if len(hex) == 2 {
			if value, err := strconv.ParseUint(hex[1], 16, 32); err == nil {
				return string(rune(value))
			}
		}
		return match
	})
	fields := strings.Fields(decoded)
	if len(fields) > 1 && isVCSMessagePrefix(fields[0]) {
		firstBodyField := 1
		for firstBodyField < len(fields) && strings.EqualFold(fields[firstBodyField], fields[0]) {
			firstBodyField++
		}
		fields = append(fields[:1], fields[firstBodyField:]...)
	}
	return strings.Join(fields, " ")
}

func isVCSMessagePrefix(field string) bool {
	switch strings.ToLower(field) {
	case "git:", "svn:", "p4:":
		return true
	default:
		return false
	}
}

func resolverForVCS(name string, runner nativeCommandRunner) (nativeVCSResolver, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "git" && name != "svn" && name != "p4" {
		return nil, fmt.Errorf("unsupported VCS %q; supported values are git, svn, and p4", name)
	}
	if runner == nil {
		runner = execNativeCommandRunner{}
	}
	return commandVCSResolver{name: name, runner: runner}, nil
}

func resolveNativeSnapshot(root, vcs string) (string, error) {
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return "", err
	}
	return resolver.Resolve(cleanWorktree(root))
}

func verifyNativeSnapshot(root, vcs, identity string) error {
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return err
	}
	return resolver.Verify(cleanWorktree(root), strings.TrimSpace(identity))
}

func verifySnapshotReady(root, vcs string) error {
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return err
	}
	return resolver.VerifyReady(cleanWorktree(root))
}

func (r commandVCSResolver) VerifyReady(root string) error {
	if err := r.verifyRoot(root); err != nil {
		return err
	}
	var pending string
	var err error
	switch r.name {
	case "git":
		// 默认 --untracked-files=normal：报告未跟踪且未忽略的文件，使「新增交付文件漏了
		// git add」在快照前就暴露、而不是作为幽灵文件静默丢失。formal-gates 自身的
		// 运行期目录（.gates/tmp、QA 隔离工作区等）在宿主仓库未必已写进 .gitignore，
		// 因此只剔除这些保留目录的未跟踪输出，不把它们误判为未提交交付文件；其它未跟踪
		// 文件仍照常拦截。
		pending, err = r.runner.Run(root, "git", "status", "--porcelain=v1")
		if err == nil {
			pending = excludeRuntimeUntrackedGitStatus(pending)
		}
	case "svn":
		pending, err = r.runner.Run(root, "svn", "status", "--quiet", root)
	case "p4":
		pending, err = r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%depotFile%", "opened", "...")
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "file(s) not opened on this client") {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("cannot inspect unsubmitted %s changes: %w", r.name, err)
	}
	if strings.TrimSpace(pending) != "" {
		return fmt.Errorf("unsubmitted %s changes must be committed before recording a snapshot (including any untracked files that must be added)", r.name)
	}
	return nil
}

// excludeRuntimeUntrackedGitStatus removes porcelain "?? <path>" lines that sit
// under formal-gates 的保留运行期目录。这些目录由 CLI 自身创建/维护，不是交付文件；
// 宿主仓库未必预先 gitignore 它们。若这些路径被跟踪后出现修改（非 "?? " 行），仍照常
// 作为未提交变更拦截——只豁免未跟踪的运行期产物。
func excludeRuntimeUntrackedGitStatus(status string) string {
	lines := strings.Split(status, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "?? ") && isRuntimeGitStatusPath(strings.TrimSpace(line[3:])) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func isRuntimeGitStatusPath(path string) bool {
	path = strings.Trim(path, `"`)
	path = strings.ReplaceAll(path, `\`, "/")
	for _, dir := range []string{".gates/tmp/", ".gates/qa-isolation/", ".gates/slices/", ".gates/results/"} {
		if path == strings.TrimSuffix(dir, "/") || strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}

func (r commandVCSResolver) Resolve(root string) (string, error) {
	if err := r.verifyRoot(root); err != nil {
		return "", err
	}
	var identity string
	var err error
	switch r.name {
	case "git":
		identity, err = r.runner.Run(root, "git", "rev-parse", "HEAD")
	case "svn":
		identity, err = r.runner.Run(root, "svnversion", root)
	case "p4":
		identity, err = r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have")
	}
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s snapshot: %w", r.name, err)
	}
	identity = strings.TrimSpace(identity)
	// SVN 工作副本的不可变标识取其 BASE 版本级：隔离工作区内注入的当前需求文档是工作树
	// 状态，svnversion 的 M（已修改）/S（已切换）/P（稀疏）等状态后缀不影响身份校验，
	// 剥离后缀后取单一 BASE 版本号；混合版本范围（如 2:3）仍按非均匀工作区拒绝。
	if r.name == "svn" {
		identity = strings.TrimRight(identity, "MSP")
	}
	if !r.validIdentity(identity) {
		if r.name == "svn" {
			return "", fmt.Errorf("svn workspace is not at one uniform revision: %q", identity)
		}
		return "", fmt.Errorf("%s returned an invalid immutable identity %q", r.name, identity)
	}
	if r.name == "p4" {
		pending, err := r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%depotFile%", "sync", "-n", "...@"+identity)
		if err != nil {
			return "", fmt.Errorf("cannot inspect p4 workspace at changelist %s: %w", identity, err)
		}
		if strings.TrimSpace(pending) != "" {
			return "", fmt.Errorf("p4 workspace is not uniformly synced to changelist %s", identity)
		}
	}
	return strings.ToLower(identity), nil
}

func (r commandVCSResolver) Verify(root, identity string) error {
	identity = strings.TrimSpace(identity)
	if !r.validIdentity(identity) {
		return fmt.Errorf("invalid %s snapshot identity %q", r.name, identity)
	}
	if err := r.verifyRoot(root); err != nil {
		return err
	}
	var resolved string
	var err error
	switch r.name {
	case "git":
		resolved, err = r.runner.Run(root, "git", "rev-parse", "--verify", identity+"^{commit}")
	case "svn":
		resolved, err = r.runner.Run(root, "svn", "info", "--show-item", "revision", "-r", identity, root)
	case "p4":
		resolved, err = r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%change%", "changes", "-m", "1", "...@"+identity)
	}
	if err != nil {
		return fmt.Errorf("cannot verify %s snapshot %s: %w", r.name, identity, err)
	}
	if !strings.EqualFold(strings.TrimSpace(resolved), identity) {
		return fmt.Errorf("%s cannot reproduce snapshot %s", r.name, identity)
	}
	return nil
}

// gitCommitCountInRange counts the commits in base..head (exclusive of base,
// inclusive of head). It is the squash precondition: only ranges with more than
// one commit are squashed.
func gitCommitCountInRange(root, base, head string) (int, error) {
	output, err := (execNativeCommandRunner{}).Run(root, "git", "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("git rev-list returned a non-numeric count %q", output)
	}
	return count, nil
}

// squashGitRangeToBase rewrites base..HEAD into a single commit while preserving
// the final tree: git reset --soft <base> stages the whole range's diff onto the
// base, then a fresh commit with the supplied message reproduces the exact final
// tree. The intermediate commits become dangling (accepted audit impact); the
// durable audit evidence is the identical final tree plus the run summary. Only
// the last VCS operation of seal runs this.
func squashGitRangeToBase(root, base, message string) error {
	if _, err := (execNativeCommandRunner{}).Run(root, "git", "reset", "--soft", base); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	if _, err := (execNativeCommandRunner{}).Run(root, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("cannot squash: %w", err)
	}
	return nil
}

// IsAncestorOrEqual reports whether ancestor is the descendant's ancestor or
// equal to it. Git uses commit lineage; SVN and P4 revisions are server-wide
// monotonically increasing numbers, so an earlier verifiable revision is an
// ancestor-or-equal of a later one on the same workspace line.
func (r commandVCSResolver) IsAncestorOrEqual(root, ancestor, descendant string) error {
	switch r.name {
	case "git":
		resolved, err := r.runner.Run(root, "git", "merge-base", ancestor, descendant)
		if err != nil {
			return fmt.Errorf("cannot resolve %s ancestry of %s: %w", r.name, ancestor, err)
		}
		if !strings.EqualFold(strings.TrimSpace(resolved), ancestor) {
			return fmt.Errorf("%s snapshot %s is not an ancestor of the current snapshot", r.name, ancestor)
		}
		return nil
	default:
		a, errA := strconv.ParseInt(strings.TrimSpace(ancestor), 10, 64)
		d, errD := strconv.ParseInt(strings.TrimSpace(descendant), 10, 64)
		if errA != nil || errD != nil {
			return fmt.Errorf("%s snapshots %s and %s are not comparable revisions", r.name, ancestor, descendant)
		}
		if a > d {
			return fmt.Errorf("%s snapshot %s is not an ancestor of the current snapshot %s", r.name, ancestor, descendant)
		}
		return nil
	}
}

func (r commandVCSResolver) verifyRoot(root string) error {
	var resolved string
	var err error
	switch r.name {
	case "git":
		resolved, err = r.runner.Run(root, "git", "rev-parse", "--show-toplevel")
	case "svn":
		resolved, err = r.runner.Run(root, "svn", "info", "--show-item", "wc-root", root)
	case "p4":
		resolved, err = r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%clientRoot%", "info")
	}
	if err != nil {
		// 外部 VCS 的 stderr 可能携带 {U+XXXX} unicode 转义（如非 UTF-8 环境下的 svn），
		// 转义成可读文字，并提示 vcs 与仓库可能不匹配（最常见于 --vcs 选错）。
		return fmt.Errorf("cannot resolve %s working root (the repository may not be a %s working copy, or --vcs does not match the repository): %s", r.name, r.name, decodeVCSMessage(err.Error()))
	}
	if !samePath(resolved, root) {
		return fmt.Errorf("%s working root %q does not match repository root %q", r.name, resolved, root)
	}
	return nil
}

func (r commandVCSResolver) validIdentity(identity string) bool {
	if r.name == "git" {
		return gitIdentityPattern.MatchString(identity)
	}
	return numericIdentityPattern.MatchString(identity)
}
