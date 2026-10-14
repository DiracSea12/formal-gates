package vcs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// WorkingTreeDigest binds the immutable tree, native metadata and actual working
// bytes and modes, with the provider's exact runtime exclusions.
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
	current := make(map[string]workingEntry, len(entries)+len(changes))
	for path, metadata := range entries {
		var mode string
		if p.name == "git" {
			mode, _, _ = strings.Cut(metadata, " ")
			metadata = ""
		} else {
			mode = entryMode(metadata)
		}
		current[path] = workingEntry{Path: path, Mode: mode, Metadata: metadata}
	}
	for _, change := range changes {
		current[change.Path] = workingEntry{Path: change.Path, Mode: change.Mode, Metadata: change.Metadata}
	}
	working := make([]workingEntry, 0, len(current))
	for path, entry := range current {
		// Root metadata and Git links describe native identities, not file bytes.
		if path != "." && entry.Mode != "160000" {
			file := materialPath(p.root, path)
			info, err := os.Lstat(file)
			if os.IsNotExist(err) {
				entry.Mode = "000000"
			} else if err != nil {
				return "", err
			} else if info.IsDir() {
				entry.Mode = "040000"
			} else {
				entry.Mode = "100644"
				switch {
				case info.Mode()&os.ModeSymlink != 0:
					entry.Mode = "120000"
				case info.Mode()&0111 != 0:
					entry.Mode = "100755"
				}
				entry.Content, err = workingBytes(file)
				if err != nil {
					return "", err
				}
			}
		}
		working = append(working, entry)
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
