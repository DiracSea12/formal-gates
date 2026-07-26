package validate

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
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
}

type commandVCSResolver struct {
	name   string
	runner nativeCommandRunner
}

var gitIdentityPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
var numericIdentityPattern = regexp.MustCompile(`^[0-9]+$`)

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
		pending, err = r.runner.Run(root, "git", "status", "--porcelain=v1", "--untracked-files=no")
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
		return fmt.Errorf("unsubmitted %s changes must be committed before recording a snapshot", r.name)
	}
	return nil
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
		return fmt.Errorf("cannot resolve %s working root: %w", r.name, err)
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
