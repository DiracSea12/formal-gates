package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Worktree is the native registration, rather than a directory-existence claim.
type Worktree struct{ Path, Identity string }

func (p *Provider) At(root string, handles ...WorkspaceHandle) *Provider {
	child := *p
	child.root = root
	if len(handles) != 0 {
		child.handle = handles[0]
	}
	return &child
}

func (p *Provider) Worktrees() ([]Worktree, error) {
	out, err := p.runner.Run(p.root, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var result []Worktree
	var item Worktree
	for _, line := range strings.Split(out+"\n\n", "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			item.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			item.Identity = strings.TrimPrefix(line, "HEAD ")
		case line == "" && item.Path != "":
			result = append(result, item)
			item = Worktree{}
		}
	}
	return result, nil
}
func (p *Provider) AddWorktree(path, base string) error {
	_, err := p.runner.Run(p.root, "git", "worktree", "add", "--detach", path, base)
	return err
}
func (p *Provider) RemoveWorktree(path string) error {
	_, err := p.runner.Run(p.root, "git", "worktree", "remove", "--force", path)
	return err
}
func (p *Provider) Pin(ref, identity string) error {
	_, err := p.runner.Run(p.root, "git", "update-ref", ref, identity)
	return err
}
func (p *Provider) RemoveRef(ref string) error {
	_, err := p.runner.Run(p.root, "git", "update-ref", "-d", ref)
	return err
}
func (p *Provider) Ref(ref string) (string, error) {
	out, err := p.runner.Run(p.root, "git", "for-each-ref", "--format=%(objectname)", ref)
	return strings.TrimSpace(out), err
}
func (p *Provider) FastForward(identity string) error {
	_, err := p.runner.Run(p.root, "git", "merge", "--ff-only", identity)
	return err
}
func (p *Provider) Message(identity string) (string, error) {
	if p.name != "git" {
		return p.nativeMessage(identity)
	}
	out, err := p.runner.Run(p.root, "git", "show", "-s", "--format=%B", identity)
	return strings.TrimSpace(out), err
}
func nulPaths(out string) []string {
	var paths []string
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
func (p *Provider) ChangedPaths() ([]string, error) {
	if p.name != "git" {
		return p.nativeChangedPaths()
	}
	tracked, err := p.runner.Run(p.root, "git", "diff", "--name-only", "--no-renames", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	staged, err := p.runner.Run(p.root, "git", "diff", "--cached", "--name-only", "--no-renames", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := p.runner.Run(p.root, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	paths := append(nulPaths(tracked), nulPaths(staged)...)
	for _, path := range nulPaths(untracked) {
		if !p.isRuntimeStatusPath(path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// ChangedPathsFrom compares the actual editing workspace with its complete
// immutable source, including materials already committed in that workspace.
func (p *Provider) ChangedPathsFrom(base string) ([]string, error) {
	if p.name == "git" {
		out, err := p.runner.Run(p.root, "git", "diff", "--name-only", "--no-renames", "-z", base, "--")
		if err != nil {
			return nil, err
		}
		untracked, err := p.runner.Run(p.root, "git", "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		paths := nulPaths(out)
		for _, path := range nulPaths(untracked) {
			if !p.isRuntimeStatusPath(path) {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		return slices.Compact(paths), nil
	}
	source, err := p.TreeEntries(base)
	if err != nil {
		return nil, err
	}
	head, err := p.Resolve()
	if err != nil {
		return nil, err
	}
	current, err := p.TreeEntries(head)
	if err != nil {
		return nil, err
	}
	entries, err := p.IndexEntries()
	if err != nil {
		return nil, err
	}
	working := map[string]string{}
	for _, entry := range entries {
		working[entry.Path] = entry.Metadata
	}
	pending, err := p.ChangedPaths()
	if err != nil {
		return nil, err
	}
	for _, path := range pending {
		target := materialPath(p.root, path)
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			delete(current, path)
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			current[path] = working[path]
			continue
		}
		value := nativeEntry{Kind: "file"}
		if working[path] != "" {
			if err := json.Unmarshal([]byte(working[path]), &value); err != nil {
				return nil, err
			}
		}
		body, err := workingBytes(target)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 && value.Properties["svn:special"] != nil {
			body = append([]byte("link "), body...)
		}
		value.Content, value.Action, value.Resolve = body, "", nil
		current[path] = entryValue(value)
	}
	var paths []string
	for _, change := range entryChanges(source, current) {
		paths = append(paths, change.Path)
	}
	return paths, nil
}

// ValidateDeclaredChanges admits only declared workspace changes and native
// directory containers required to hold declared descendants.
func (p *Provider) ValidateDeclaredChanges(base string, paths []string) ([]string, Pending, error) {
	changes, err := p.ChangedPaths()
	if err != nil {
		return nil, Pending{}, err
	}
	pending, err := p.Pending(base, "")
	if err != nil {
		return nil, Pending{}, err
	}
	for _, path := range changes {
		if !slices.Contains(paths, path) && !slices.ContainsFunc(pending.Changes, func(c Change) bool { return c.Path == path && c.IsDirectoryContainer(paths) }) {
			return nil, Pending{}, fmt.Errorf("candidate contains undeclared change: %s", path)
		}
	}
	return changes, pending, nil
}

// Track stages only declared differences between the working files and index.
// An artifact's already staged removal or addition is an observed prefix.
func (p *Provider) Track(paths []string) error {
	if p.name != "git" {
		return p.nativeTrack(paths)
	}
	if len(paths) == 0 {
		return nil
	}
	changed, err := p.runner.Run(p.root, "git", append([]string{"diff", "--name-only", "--no-renames", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	untracked, err := p.runner.Run(p.root, "git", append([]string{"ls-files", "--others", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	remaining := append(nulPaths(changed), nulPaths(untracked)...)
	slices.Sort(remaining)
	remaining = slices.Compact(remaining)
	if len(remaining) == 0 {
		return nil
	}
	_, err = p.runner.Run(p.root, "git", append([]string{"add", "--all", "--force", "--"}, remaining...)...)
	return err
}

// ApplyArtifact uses Git's tree checkout, including executable and symlink modes.
// Missing entries are removals; both endpoints of a rename are declared paths.
func (p *Provider) ApplyArtifact(identity string, paths []string) error {
	if p.name != "git" {
		return p.nativeApply(identity, paths)
	}
	for _, path := range paths {
		working, err := p.runner.Run(p.root, "git", "diff", "--name-only", "--no-renames", "-z", identity, "--", path)
		if err != nil {
			return err
		}
		staged, err := p.runner.Run(p.root, "git", "diff", "--cached", "--name-only", "--no-renames", "-z", identity, "--", path)
		if err != nil {
			return err
		}
		if working == "" && staged == "" {
			continue
		}
		entry, err := p.Entry(identity, path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(entry) == "" {
			if _, err = p.runner.Run(p.root, "git", "rm", "--force", "--ignore-unmatch", "--", path); err != nil {
				return err
			}
		} else if _, err = p.runner.Run(p.root, "git", "restore", "--source="+identity, "--staged", "--worktree", "--", path); err != nil {
			return err
		}
	}
	return nil
}

// InputOwnership uses explicitly configured Go module/workspace input graphs.
// It returns nil,nil when this root has no resolver capability or
// usable graph; a nonnil map (including an empty map) is a successful graph.
// Errors report actual query or decoding failures. Test inputs are true,
// production inputs false, and absent keys UNKNOWN; production takes precedence.
func (p *Provider) InputOwnership(testReferences ...string) (map[string]bool, error) {
	configured := false
	for directory := canonicalPath(p.root); ; directory = filepath.Dir(directory) {
		for _, name := range []string{"go.mod", "go.work"} {
			_, err := os.Stat(filepath.Join(directory, name))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			configured = configured || err == nil
		}
		if configured || filepath.Dir(directory) == directory {
			break
		}
	}
	for _, reference := range testReferences {
		file, symbol, qualified := strings.Cut(reference, "::")
		configured = configured || qualified && symbol != "" && strings.HasSuffix(file, "_test.go")
	}
	if !configured {
		return nil, nil
	}
	configuration, err := p.runner.Run(p.root, "go", "env", "-json", "GOMOD", "GOWORK")
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env struct{ GOMOD, GOWORK string }
	if err := json.Unmarshal([]byte(configuration), &env); err != nil {
		return nil, err
	}
	if env.GOMOD == os.DevNull && (env.GOWORK == "" || env.GOWORK == "off") {
		return nil, nil
	}
	// Ownership reads build inputs, without VCS stamping of nested workspaces.
	out, err := p.runner.Run(p.root, "go", "list", "-buildvcs=false", "-test", "-json", "./...")
	if err != nil {
		return nil, fmt.Errorf("test ownership is unavailable: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(out))
	owned := map[string]bool{}
	production := map[string]bool{}
	for {
		var pkg struct {
			Dir                                                                                                                                                                             string
			ForTest                                                                                                                                                                         string
			Match                                                                                                                                                                           []string
			GoFiles, CgoFiles, CFiles, CXXFiles, MFiles, HFiles, FFiles, SFiles, SwigFiles, SwigCXXFiles, SysoFiles, EmbedFiles, TestGoFiles, XTestGoFiles, TestEmbedFiles, XTestEmbedFiles []string
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		// Test variants mix production and test fields; generated test mains
		// have no command-line match. The matched original packages describe
		// both input sets, including embed files resolved by -test.
		if pkg.ForTest != "" || len(pkg.Match) == 0 {
			continue
		}
		relative, err := filepath.Rel(canonicalPath(p.root), canonicalPath(pkg.Dir))
		if err != nil {
			return nil, err
		}
		add := func(target map[string]bool, lists ...[]string) {
			for _, list := range lists {
				for _, name := range list {
					target[filepath.ToSlash(filepath.Join(relative, name))] = true
				}
			}
		}
		add(owned, pkg.TestGoFiles, pkg.XTestGoFiles, pkg.TestEmbedFiles, pkg.XTestEmbedFiles)
		add(production, pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles, pkg.EmbedFiles)
	}
	for path := range production {
		owned[path] = false
	}
	return owned, nil
}

func (p *Provider) Entry(identity, path string) (string, error) {
	if p.name != "git" {
		entries, err := p.TreeEntries(identity)
		return entries[path], err
	}
	return p.runner.Run(p.root, "git", "ls-tree", identity, "--", path)
}

// Change is the complete native tree entry transition. Rename endpoints are
// represented as a deletion and addition so neither path can be omitted.
type Change struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	BaseMode     string `json:"baseMode"`
	Mode         string `json:"mode"`
	BaseObject   string `json:"baseObject"`
	Object       string `json:"object"`
	BaseMetadata string `json:"baseMetadata,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
}

func (p *Provider) Changes(base, identity string) ([]Change, error) {
	if p.name != "git" {
		return p.nativeChanges(base, identity)
	}
	out, err := p.runner.Run(p.root, "git", "diff", "--raw", "--no-abbrev", "--no-renames", "-z", base, identity, "--")
	if err != nil {
		return nil, err
	}
	fields := nulPaths(out)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("native change inventory is incomplete")
	}
	var changes []Change
	for i := 0; i < len(fields); i += 2 {
		metadata := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(metadata) != 5 {
			return nil, fmt.Errorf("native change metadata is incomplete")
		}
		changes = append(changes, Change{Path: fields[i+1], Kind: metadata[4], BaseMode: metadata[0], Mode: metadata[1], BaseObject: metadata[2], Object: metadata[3]})
	}
	return changes, nil
}
