package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
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
}

type commandVCSResolver struct {
	name   string
	runner nativeCommandRunner
}

var gitIdentityPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
var numericIdentityPattern = regexp.MustCompile(`^[0-9]+$`)
var p4IndexedViewPattern = regexp.MustCompile(`^(View|ChangeView)([0-9]+)$`)

type svnSnapshotIdentity struct {
	Revision string `json:"revision"`
	URL      string `json:"url"`
}

type p4SnapshotIdentity struct {
	Change         string   `json:"change"`
	Client         string   `json:"client"`
	Stream         string   `json:"stream,omitempty"`
	StreamAtChange string   `json:"streamAtChange,omitempty"`
	View           []string `json:"view,omitempty"`
	ChangeView     []string `json:"changeView,omitempty"`
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

func nativeSnapshotsEqual(vcs, first, second string) bool {
	if strings.EqualFold(strings.TrimSpace(vcs), "git") {
		return strings.EqualFold(first, second)
	}
	return first == second
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
	switch r.name {
	case "git":
		identity, err := r.runner.Run(root, "git", "rev-parse", "HEAD")
		if err != nil {
			return "", fmt.Errorf("cannot resolve git snapshot: %w", err)
		}
		identity = strings.TrimSpace(identity)
		if !gitIdentityPattern.MatchString(identity) {
			return "", fmt.Errorf("git returned an invalid immutable identity %q", identity)
		}
		return strings.ToLower(identity), nil
	case "svn":
		revision, err := r.runner.Run(root, "svnversion", root)
		if err != nil {
			return "", fmt.Errorf("cannot resolve svn snapshot: %w", err)
		}
		revision = strings.TrimSpace(revision)
		if !numericIdentityPattern.MatchString(revision) {
			return "", fmt.Errorf("svn workspace is not at one uniform revision: %q", revision)
		}
		repositoryURL, err := r.runner.Run(root, "svn", "info", "--show-item", "url", root)
		if err != nil {
			return "", fmt.Errorf("cannot resolve svn repository URL: %w", err)
		}
		return marshalSnapshotIdentity(svnSnapshotIdentity{Revision: revision, URL: strings.TrimSpace(repositoryURL)})
	case "p4":
		change, err := r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have")
		if err != nil {
			return "", fmt.Errorf("cannot resolve p4 snapshot: %w", err)
		}
		change = strings.TrimSpace(change)
		if !numericIdentityPattern.MatchString(change) {
			return "", fmt.Errorf("p4 returned an invalid immutable identity %q", change)
		}
		clientSpec, err := r.runner.Run(root, "p4", "-d", root, "-ztag", "client", "-o")
		if err != nil {
			return "", fmt.Errorf("cannot resolve p4 client view: %w", err)
		}
		identity, err := parseP4ClientSnapshot(change, clientSpec)
		if err != nil {
			return "", err
		}
		pending, err := r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%depotFile%", "sync", "-n", "...@"+change)
		if err != nil {
			return "", fmt.Errorf("cannot inspect p4 workspace at changelist %s: %w", change, err)
		}
		if strings.TrimSpace(pending) != "" {
			return "", fmt.Errorf("p4 workspace is not uniformly synced to changelist %s", change)
		}
		return marshalSnapshotIdentity(identity)
	}
	return "", fmt.Errorf("unsupported VCS %q", r.name)
}

func (r commandVCSResolver) Verify(root, identity string) error {
	identity = strings.TrimSpace(identity)
	if !r.validIdentity(identity) {
		return fmt.Errorf("invalid %s snapshot identity %q", r.name, identity)
	}
	if err := r.verifyRoot(root); err != nil {
		return err
	}
	expected := identity
	var resolved string
	var err error
	switch r.name {
	case "git":
		resolved, err = r.runner.Run(root, "git", "rev-parse", "--verify", identity+"^{commit}")
	case "svn":
		snapshot, _ := parseSVNSnapshotIdentity(identity)
		resolved, err = r.runner.Run(root, "svn", "info", "--show-item", "revision", "-r", snapshot.Revision, snapshot.URL)
		expected = snapshot.Revision
	case "p4":
		snapshot, _ := parseP4SnapshotIdentity(identity)
		resolved, err = r.runner.Run(root, "p4", "-d", root, "-ztag", "-F", "%Change%", "change", "-o", snapshot.Change)
		expected = snapshot.Change
	}
	if err != nil {
		return fmt.Errorf("cannot verify %s snapshot %s: %w", r.name, identity, err)
	}
	if !strings.EqualFold(strings.TrimSpace(resolved), expected) {
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
	switch r.name {
	case "git":
		return gitIdentityPattern.MatchString(identity)
	case "svn":
		_, err := parseSVNSnapshotIdentity(identity)
		return err == nil
	case "p4":
		_, err := parseP4SnapshotIdentity(identity)
		return err == nil
	}
	return false
}

func marshalSnapshotIdentity(identity any) (string, error) {
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("cannot encode native snapshot identity: %w", err)
	}
	return string(encoded), nil
}

func parseSVNSnapshotIdentity(identity string) (svnSnapshotIdentity, error) {
	var snapshot svnSnapshotIdentity
	if err := json.Unmarshal([]byte(identity), &snapshot); err != nil ||
		!numericIdentityPattern.MatchString(snapshot.Revision) ||
		strings.TrimSpace(snapshot.URL) == "" || snapshot.URL != strings.TrimSpace(snapshot.URL) {
		return svnSnapshotIdentity{}, fmt.Errorf("invalid svn snapshot identity %q", identity)
	}
	return snapshot, nil
}

func parseP4SnapshotIdentity(identity string) (p4SnapshotIdentity, error) {
	var snapshot p4SnapshotIdentity
	if err := json.Unmarshal([]byte(identity), &snapshot); err != nil ||
		!numericIdentityPattern.MatchString(snapshot.Change) ||
		strings.TrimSpace(snapshot.Client) == "" ||
		(len(snapshot.View) == 0 && strings.TrimSpace(snapshot.Stream) == "") {
		return p4SnapshotIdentity{}, fmt.Errorf("invalid p4 snapshot identity %q", identity)
	}
	return snapshot, nil
}

func parseP4ClientSnapshot(change, output string) (p4SnapshotIdentity, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "... ") {
			continue
		}
		name, value, found := strings.Cut(strings.TrimPrefix(line, "... "), " ")
		if found {
			fields[name] = strings.TrimSpace(value)
		}
	}
	identity := p4SnapshotIdentity{
		Change:         change,
		Client:         fields["Client"],
		Stream:         fields["Stream"],
		StreamAtChange: fields["StreamAtChange"],
	}
	type indexedView struct {
		kind  string
		index int
		value string
	}
	var indexed []indexedView
	for name, value := range fields {
		match := p4IndexedViewPattern.FindStringSubmatch(name)
		if match == nil || strings.TrimSpace(value) == "" {
			continue
		}
		index, err := strconv.Atoi(match[2])
		if err != nil {
			return p4SnapshotIdentity{}, fmt.Errorf("p4 returned an invalid client view index %q", name)
		}
		indexed = append(indexed, indexedView{kind: match[1], index: index, value: value})
	}
	sort.Slice(indexed, func(i, j int) bool {
		if indexed[i].kind != indexed[j].kind {
			return indexed[i].kind < indexed[j].kind
		}
		return indexed[i].index < indexed[j].index
	})
	for _, entry := range indexed {
		if entry.kind == "View" {
			identity.View = append(identity.View, entry.value)
		} else {
			identity.ChangeView = append(identity.ChangeView, entry.value)
		}
	}
	if strings.TrimSpace(identity.Client) == "" || (len(identity.View) == 0 && strings.TrimSpace(identity.Stream) == "") {
		return p4SnapshotIdentity{}, fmt.Errorf("p4 returned an incomplete client view")
	}
	return identity, nil
}
