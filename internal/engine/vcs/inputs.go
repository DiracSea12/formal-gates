package vcs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// WorkingTreeDigest binds the complete immutable tree and its current native
// content changes, with the provider's exact runtime exclusions.
func (p *Provider) WorkingTreeDigest(base string) (string, error) {
	entries, err := p.TreeEntries(base)
	if err != nil {
		return "", err
	}
	changes, err := p.CompareWorkingTree(base)
	if err != nil {
		return "", err
	}
	type workingEntry struct {
		Path     string
		Mode     string
		Metadata string
		Content  []byte
	}
	working := make([]workingEntry, 0, len(changes))
	for _, change := range changes {
		working = append(working, workingEntry{change.Path, change.Mode, change.Metadata, change.Content})
	}
	slices.SortFunc(working, func(a, b workingEntry) int { return strings.Compare(a.Path, b.Path) })
	body, err := json.Marshal(struct {
		Entries map[string]string
		Working []workingEntry
	}{entries, working})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(body)), nil
}

func (p *Provider) TreeEntries(identity string) (map[string]string, error) {
	if p.name != "git" {
		return p.nativeEntries(identity)
	}
	out, err := p.runner.RunRaw(p.root, "git", nil, "ls-tree", "-r", "-z", identity)
	if err != nil {
		return nil, err
	}
	entries := map[string]string{}
	for _, entry := range nulPaths(string(out)) {
		metadata, path, found := strings.Cut(entry, "\t")
		if !found {
			return nil, fmt.Errorf("native tree entry is incomplete")
		}
		entries[path] = metadata
	}
	return entries, nil
}
