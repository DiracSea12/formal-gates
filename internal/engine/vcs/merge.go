package vcs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func readMergeHead(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return strings.TrimSpace(string(body)), err
}

// IndexEntry preserves native mode/blob/stage identity, including all three
// sides of a conflict. The index owns automatically merged content.
type IndexEntry struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Object   string `json:"object"`
	Stage    int    `json:"stage"`
	Metadata string `json:"metadata,omitempty"`
	Conflict string `json:"conflict,omitempty"`
}

func (p *Provider) IndexEntries() ([]IndexEntry, error) {
	if p.name != "git" {
		return p.nativeIndex()
	}
	out, err := p.runner.RunRaw(p.root, "git", nil, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	for _, row := range nulPaths(string(out)) {
		metadata, path, ok := strings.Cut(row, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("native index entry is incomplete")
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, err
		}
		entries = append(entries, IndexEntry{Path: path, Mode: fields[0], Object: fields[1], Stage: stage})
	}
	return entries, nil
}

func (p *Provider) MergeHead() (string, error) {
	if p.name != "git" {
		return "", fmt.Errorf("%s has native integration facts, not MERGE_HEAD", p.name)
	}
	path, err := p.runner.Run(p.root, "git", "rev-parse", "--git-path", "MERGE_HEAD")
	if err != nil {
		return "", err
	}
	return readMergeHead(p.root, path)
}

// Merge uses Git's three-way operation and stops before commit even when a
// fast-forward is possible. Only an observed unmerged index is content conflict.
func (p *Provider) Merge(incoming string) error {
	if p.name != "git" {
		return p.nativeMerge(incoming)
	}
	_, err := p.runner.Run(p.root, "git", "-c", "merge.conflictStyle=diff3", "merge", "--no-ff", "--no-commit", incoming)
	if err == nil {
		return nil
	}
	entries, inspectErr := p.IndexEntries()
	if inspectErr != nil {
		return inspectErr
	}
	if !slices.ContainsFunc(entries, func(e IndexEntry) bool { return e.Stage != 0 }) {
		return err
	}
	head, inspectErr := p.MergeHead()
	if inspectErr != nil {
		return inspectErr
	}
	if head != incoming {
		return err
	}
	return nil
}

func (p *Provider) Parents(identity string) ([]string, error) {
	if p.name != "git" {
		return nil, fmt.Errorf("%s has native scope lineage, not commit parents", p.name)
	}
	out, err := p.runner.Run(p.root, "git", "show", "-s", "--format=%P", identity)
	return strings.Fields(out), err
}

func (p *Provider) WorkingChanges() ([]string, error) {
	if p.name != "git" {
		return p.nativeChangedPaths()
	}
	out, err := p.runner.RunRaw(p.root, "git", nil, "diff", "--name-only", "--no-renames", "-z", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := p.runner.RunRaw(p.root, "git", nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	paths := nulPaths(string(out))
	for _, path := range nulPaths(string(untracked)) {
		if !p.isRuntimeStatusPath(path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}
