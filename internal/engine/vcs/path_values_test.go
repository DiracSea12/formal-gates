package vcs

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Native path lists and staging preserve filename bytes independently of Git's
// display quoting, and reads leave the index unchanged.
func TestChangedPathsPreserveFilenameBytes(t *testing.T) {
	for _, path := range []string{" artifact.txt", "artifact.txt ", "\tartifact.txt", "line\nartifact.txt", `"artifact".txt`, "说明.txt"} {
		t.Run(path, func(t *testing.T) {
			p := realRepo(t)
			base, err := p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.Root(), path), []byte("ordinary artifact\n"), 0600); err != nil {
				t.Fatal(err)
			}
			raw, err := p.runner.RunRaw(p.Root(), "git", nil, "ls-files", "--others", "--exclude-standard", "-z")
			if err != nil || !bytes.Equal(raw, []byte(path+"\x00")) {
				t.Fatalf("native path bytes=%q err=%v", raw, err)
			}
			assertPaths := func(working []string) {
				t.Helper()
				index, err := p.StagedTreeDigest()
				if err != nil {
					t.Fatal(err)
				}
				for _, current := range []*Provider{p, New(p.Root(), nil)} {
					for _, read := range []func() ([]string, error){current.ChangedPaths, func() ([]string, error) { return current.ChangedPathsFrom(base) }} {
						got, err := read()
						if err != nil || !slices.Equal(got, []string{path}) {
							t.Fatalf("path list=%q err=%v want=%q", got, err, path)
						}
					}
					if got, err := current.WorkingChanges(); err != nil || !slices.Equal(got, working) {
						t.Fatalf("working paths=%q err=%v want=%q", got, err, working)
					}
				}
				if got, err := p.StagedTreeDigest(); err != nil || got != index {
					t.Fatalf("reading paths changed index: %s %v want=%s", got, err, index)
				}
			}
			assertPaths([]string{path})
			if err := p.Track([]string{path}); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("tracked artifact"); err != nil {
				t.Fatal(err)
			}
			base, err = p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.Root(), path), []byte("modified artifact\n"), 0600); err != nil {
				t.Fatal(err)
			}
			assertPaths([]string{path})
			if err := p.Track([]string{path}); err != nil {
				t.Fatal(err)
			}
			assertPaths(nil)
			if err := os.Remove(filepath.Join(p.Root(), path)); err != nil {
				t.Fatal(err)
			}
			assertPaths([]string{path})
		})
	}
}

func TestTrackPreservesFilenameBytes(t *testing.T) {
	for _, path := range []string{" artifact.txt", "artifact.txt ", "\tartifact.txt", "line\nartifact.txt", `"artifact".txt`, "说明.txt"} {
		t.Run(path, func(t *testing.T) {
			p := realRepo(t)
			if err := os.WriteFile(filepath.Join(p.Root(), "unrelated.txt"), []byte("unrelated\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, body := range []string{"new artifact\n", "modified artifact\n"} {
				if err := os.WriteFile(filepath.Join(p.Root(), path), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := p.Track([]string{path}); err != nil {
					t.Fatalf("exact native path failed staging: %v", err)
				}
				index, err := p.StagedTreeDigest()
				if err != nil {
					t.Fatal(err)
				}
				resumed := New(p.Root(), nil)
				if err := resumed.Track([]string{path}); err != nil {
					t.Fatal(err)
				}
				if got, err := resumed.StagedTreeDigest(); err != nil || got != index {
					t.Fatalf("repeated staging changed index: %s %v want=%s", got, err, index)
				}
				entries, err := resumed.IndexEntries()
				if err != nil || !slices.ContainsFunc(entries, func(entry IndexEntry) bool { return entry.Path == path && entry.Stage == 0 }) || slices.ContainsFunc(entries, func(entry IndexEntry) bool { return entry.Path == "unrelated.txt" }) {
					t.Fatalf("index entries=%+v err=%v", entries, err)
				}
				got, err := resumed.runner.RunRaw(p.Root(), "git", nil, "show", ":"+path)
				if err != nil || !bytes.Equal(got, []byte(body)) {
					t.Fatalf("staged original content=%q err=%v want=%q", got, err, body)
				}
				if err := p.Commit("artifact content"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(p.Root(), path)); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := New(p.Root(), nil).Track([]string{path}); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := p.runner.RunRaw(p.Root(), "git", nil, "diff", "--cached", "--name-only", "-z")
			if err != nil || !bytes.Equal(raw, []byte(path+"\x00")) {
				t.Fatalf("staged removal=%q err=%v want=%q", raw, err, path)
			}
			entries, err := p.IndexEntries()
			if err != nil || slices.ContainsFunc(entries, func(entry IndexEntry) bool { return entry.Path == path || entry.Path == "unrelated.txt" }) {
				t.Fatalf("removal or declaration boundary changed: %+v %v", entries, err)
			}
		})
	}
}

func TestChangesPreserveOriginalPathsWithDefaultGitQuoting(t *testing.T) {
	p := realRepo(t)
	if _, err := p.runner.Run(p.Root(), "git", "config", "core.quotePath", "true"); err != nil {
		t.Fatal(err)
	}
	removed := " removed.txt"
	if err := os.WriteFile(filepath.Join(p.Root(), removed), []byte("removed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{removed}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("removable artifact"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{" artifact.txt", "artifact.txt ", "\tartifact.txt", "line\nartifact.txt", `"artifact".txt`, "说明.txt", removed}
	for _, path := range wanted[:len(wanted)-1] {
		if err := os.WriteFile(filepath.Join(p.Root(), path), []byte("ordinary artifact\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(p.Root(), removed)); err != nil {
		t.Fatal(err)
	}
	if err := p.Track(wanted); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("artifact transitions"); err != nil {
		t.Fatal(err)
	}
	head, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(wanted)
	changes, err := p.Changes(base, head)
	if err != nil || len(changes) != len(wanted) {
		t.Fatalf("native changes=%+v err=%v", changes, err)
	}
	for i, change := range changes {
		if change.Path != wanted[i] || change.Path == removed && change.Kind != "D" || change.Path != removed && change.Kind != "A" {
			t.Fatalf("original entry transition=%+v want=%q", change, wanted[i])
		}
	}
	raw, err := p.runner.RunRaw(p.Root(), "git", nil, "diff", "--name-only", "-z", base+".."+head)
	if err != nil || !bytes.Equal(raw, []byte(strings.Join(wanted, "\x00")+"\x00")) {
		t.Fatalf("native output=%q err=%v want=%q", raw, err, wanted)
	}
}

// Artifact application resumes an observed staged prefix through the same
// native operation while preserving additions, edits and removals.
func TestApplyArtifactPreservesOriginalPathsAcrossStagedPrefix(t *testing.T) {
	p := realRepo(t)
	removed, modified, added := " removed.txt", "\tmodified.txt", "说明\n.txt "
	for _, path := range []string{removed, modified} {
		if err := os.WriteFile(filepath.Join(p.Root(), path), []byte("base\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Track([]string{removed, modified}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("artifact base"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.Root(), removed)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{modified, added} {
		if err := os.WriteFile(filepath.Join(p.Root(), path), []byte("artifact\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{removed, modified, added}
	if err := p.Track(paths); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("artifact content"); err != nil {
		t.Fatal(err)
	}
	artifact, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.runner.Run(p.Root(), "git", "reset", "--hard", base); err != nil {
		t.Fatal(err)
	}
	if err := p.ApplyArtifact(artifact, paths[:1]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		resumed := New(p.Root(), nil)
		if err := resumed.ApplyArtifact(artifact, paths); err != nil {
			t.Fatal(err)
		}
		if err := resumed.Track(paths); err != nil {
			t.Fatal(err)
		}
		wanted, err := resumed.TreeDigest(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := resumed.StagedTreeDigest(); err != nil || got != wanted {
			t.Fatalf("artifact index identity=%s err=%v want=%s", got, err, wanted)
		}
		entries, err := resumed.TreeEntries(artifact)
		if err != nil || entries[modified] == "" || entries[added] == "" || entries[removed] != "" {
			t.Fatalf("artifact tree entries=%v err=%v", entries, err)
		}
		for _, path := range []string{modified, added} {
			if body, err := os.ReadFile(filepath.Join(p.Root(), path)); err != nil || !bytes.Equal(body, []byte("artifact\n")) {
				t.Fatalf("artifact working bytes at %q=%q err=%v", path, body, err)
			}
		}
		if _, err := os.Stat(filepath.Join(p.Root(), removed)); !os.IsNotExist(err) {
			t.Fatalf("artifact removal was not applied: %v", err)
		}
	}
}
