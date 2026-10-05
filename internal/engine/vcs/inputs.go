package vcs

import (
	"fmt"
	"strings"
)

func (p *Provider) TreeEntries(identity string) (map[string]string, error) {
	if p.name != "git" {
		return p.nativeEntries(identity)
	}
	out, err := p.runner.Run(p.root, "git", "ls-tree", "-r", "-z", identity)
	if err != nil {
		return nil, err
	}
	entries := map[string]string{}
	for _, entry := range nulPaths(out) {
		metadata, path, found := strings.Cut(entry, "\t")
		if !found {
			return nil, fmt.Errorf("native tree entry is incomplete")
		}
		entries[path] = metadata
	}
	return entries, nil
}
