package vcs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSVNDirectoryPropertyApplyPrefixesRemainResumable(t *testing.T) {
	for _, directory := range []string{".", "empty"} {
		t.Run(directory, func(t *testing.T) {
			p, runner := svnFixture(t)
			leaf := filepath.ToSlash(filepath.Join(directory, "body.txt"))
			const property = "custom:directory"
			if err := os.WriteFile(materialPath(p.root, leaf), []byte("base body\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := p.Track([]string{leaf}); err != nil {
				t.Fatal(err)
			}
			nativeCommand(t, runner, p.root, "svn", "propset", property, "base property", svnLiteral(directory))
			if err := p.Commit("directory property source"); err != nil {
				t.Fatal(err)
			}
			base := nativeIdentityAt(t, p)
			candidate, handle := nativeWorkspaceAt(t, p, base, "directory-properties")
			if err := os.WriteFile(materialPath(candidate.root, leaf), []byte("candidate body\n"), 0644); err != nil {
				t.Fatal(err)
			}
			nativeCommand(t, runner, candidate.root, "svn", "propset", property, "candidate property", svnLiteral(directory))
			if err := candidate.Commit("directory property candidate"); err != nil {
				t.Fatal(err)
			}
			source := nativeIdentityAt(t, candidate)
			for _, input := range []struct{ identity, property, content string }{
				{base, "base property", "base body\n"}, {source, "candidate property", "candidate body\n"},
			} {
				entries, err := p.TreeEntries(input.identity)
				if err != nil {
					t.Fatal(err)
				}
				var container, body nativeEntry
				if err := json.Unmarshal([]byte(entries[directory]), &container); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(entries[leaf]), &body); err != nil {
					t.Fatal(err)
				}
				if string(container.Properties[property]) != input.property || string(body.Content) != input.content {
					t.Fatalf("immutable input values differ: %s %q %q", input.identity, container.Properties[property], body.Content)
				}
			}
			if err := candidate.IsAncestorOrEqual(base, source); err != nil {
				t.Fatal(err)
			}
			changes, err := p.Changes(base, source)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, change := range changes {
				paths = append(paths, change.Path)
			}
			runner.lost, runner.didLose = property, false
			if err := p.ApplyArtifact(source, paths); err == nil || !runner.didLose {
				t.Fatalf("actual successful property write response was not lost: %v", err)
			}
			runner.lost = ""
			actual, err := os.ReadFile(materialPath(p.root, leaf))
			if err != nil || string(actual) != "base body\n" {
				t.Fatalf("unapplied descendant differs: %q %v", actual, err)
			}
			if value := nativeCommand(t, runner, p.root, "svn", "propget", property, svnLiteral(directory)); value != "candidate property" {
				t.Fatalf("successful own-node property did not land: %q", value)
			}
			pending, err := p.Pending(base, source)
			if err != nil || pending.Clean || !pending.Tracked || pending.Equal || !pending.Prefix {
				t.Fatalf("exact own-node property prefix cannot resume: %+v %v", pending, err)
			}
			nativeCommand(t, runner, p.root, "svn", "propset", property, "unrelated property", svnLiteral(directory))
			negative, err := p.Pending(base, source)
			if err != nil || negative.Prefix {
				t.Fatalf("unrelated property accepted: %+v %v", negative, err)
			}
			nativeCommand(t, runner, p.root, "svn", "propset", property, "candidate property", svnLiteral(directory))
			if err := p.ApplyArtifact(source, paths); err != nil {
				t.Fatal(err)
			}
			pending, err = p.Pending(base, source)
			if err != nil || !pending.Tracked || !pending.Equal || !pending.Prefix {
				t.Fatalf("normal resumed apply did not match complete source: %+v %v", pending, err)
			}
			if err := p.Commit("directory property promotion"); err != nil {
				t.Fatal(err)
			}
			final := nativeIdentityAt(t, p)
			if err := p.IsAncestorOrEqual(base, final); err != nil {
				t.Fatal(err)
			}
			if nativeDigestAt(t, p, final) != nativeDigestAt(t, candidate, source) {
				t.Fatal("final source properties and child bytes differ")
			}
			if err := p.VerifyReady(); err != nil {
				t.Fatal(err)
			}
			if err := p.CleanupWorkspace(candidate.root, "directory property cleanup", handle); err != nil {
				t.Fatal(err)
			}
		})
	}
}
