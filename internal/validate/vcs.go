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
