package vcs

import "encoding/json"

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
				*side.content, err = p.runner.RunRaw(p.root, "git", nil, "show", side.identity+":"+change.Path)
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
