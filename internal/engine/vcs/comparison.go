package vcs

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
)

// SnapshotComparison carries the original native entry transition and bytes.
// Non-Git providers already include those bytes in Changes' native metadata.
type SnapshotComparison struct {
	Change
	BaseContent []byte `json:"baseContent"`
	Content     []byte `json:"content"`
}

func (p *Provider) CompareSnapshots(base, head string) ([]SnapshotComparison, error) {
	changes, err := p.Changes(base, head)
	if err != nil {
		return nil, err
	}
	return p.compareChanges(changes, base, head)
}

// CompareWorkingTree reads native changes and original working bytes without
// staging, committing or changing the registered workspace.
func (p *Provider) CompareWorkingTree(base string) ([]SnapshotComparison, error) {
	var changes []Change
	if p.name != "git" {
		original, err := p.TreeEntries(base)
		if err != nil {
			return nil, err
		}
		current, err := p.workingTreeEntries()
		if err != nil {
			return nil, err
		}
		changes = entryChanges(original, current)
	} else {
		var err error
		changes, err = p.Changes(base, "")
		if err != nil {
			return nil, err
		}
		untracked, err := p.runner.RunRaw(p.root, "git", nil, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		paths := nulPaths(string(untracked))
		slices.Sort(paths)
		paths = slices.Compact(paths)
		known := map[string]bool{}
		for _, change := range changes {
			known[change.Path] = true
		}
		for _, path := range paths {
			if !known[path] && !p.isRuntimeStatusPath(path) {
				changes = append(changes, Change{Path: path, Kind: "A", BaseMode: "000000"})
			}
		}
		for i := range changes {
			change := &changes[i]
			info, err := os.Lstat(materialPath(p.root, change.Path))
			if os.IsNotExist(err) {
				change.Mode, change.Kind = "000000", "D"
				continue
			}
			if err != nil {
				return nil, err
			}
			change.Mode = "100644"
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				change.Mode = "120000"
			case info.Mode()&0111 != 0:
				change.Mode = "100755"
			}
			change.Kind = "M"
			if change.BaseMode == "000000" {
				change.Kind = "A"
			}
		}
	}
	rows, err := p.compareChanges(changes, base, "")
	if err != nil {
		return nil, err
	}
	result := make([]SnapshotComparison, 0, len(rows))
	for _, row := range rows {
		if row.BaseMode != row.Mode || row.BaseMetadata != row.Metadata || !bytes.Equal(row.BaseContent, row.Content) {
			result = append(result, row)
		}
	}
	return result, nil
}

func (p *Provider) compareChanges(changes []Change, base, head string) ([]SnapshotComparison, error) {
	var err error
	result := make([]SnapshotComparison, 0, len(changes))
	for _, change := range changes {
		row := SnapshotComparison{Change: change}
		for _, side := range []struct {
			identity, mode, metadata string
			content                  *[]byte
		}{{base, change.BaseMode, change.BaseMetadata, &row.BaseContent}, {head, change.Mode, change.Metadata, &row.Content}} {
			if side.mode == "000000" || side.mode == "040000" {
				continue
			}
			if p.name == "git" {
				if side.identity == "" {
					*side.content, err = workingBytes(materialPath(p.root, change.Path))
				} else {
					*side.content, err = p.runner.RunRaw(p.root, "git", nil, "show", side.identity+":"+change.Path)
				}
			} else {
				var entry nativeEntry
				err = json.Unmarshal([]byte(side.metadata), &entry)
				*side.content = entry.Content
			}
			if err != nil {
				return nil, err
			}
		}
		result = append(result, row)
	}
	return result, nil
}
