package vcs

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSnapshotComparisonPreservesNativeBytesWithoutWorkspaceWrites(t *testing.T) {
	for _, name := range []string{"git", "svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			var p *Provider
			var runner *nativeTestRunner
			if name == "git" {
				root := t.TempDir()
				for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
					cmd := exec.Command("git", args...)
					cmd.Dir = root
					if body, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("%v: %v %s", args, err, body)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "body.txt"), []byte(" leading\ntrailing\n\n"), 0644); err != nil {
					t.Fatal(err)
				}
				p = New(root, nil)
				if err := p.Track([]string{"body.txt"}); err != nil {
					t.Fatal(err)
				}
				if err := p.Commit("fixture values"); err != nil {
					t.Fatal(err)
				}
			} else {
				p, runner = nativeFixture(t, name)
			}
			base, err := p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(filepath.Join(p.Root(), "body.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if name == "p4" {
				nativeCommand(t, runner, p.Root(), "p4", "edit", "body.txt")
			}
			updated := []byte(" changed\nincluding trailing whitespace \n\n")
			if err := os.WriteFile(filepath.Join(p.Root(), "body.txt"), updated, 0755); err != nil {
				t.Fatal(err)
			}
			if err := p.Track([]string{"body.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("comparison source"); err != nil {
				t.Fatal(err)
			}
			head, err := p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			status, err := p.Status()
			if err != nil {
				t.Fatal(err)
			}
			rows, err := p.CompareSnapshots(base, head)
			if err != nil {
				t.Fatal(err)
			}
			var selected *SnapshotComparison
			for i := range rows {
				if rows[i].Path == "body.txt" {
					selected = &rows[i]
				}
			}
			if selected == nil || !bytes.Equal(selected.BaseContent, original) {
				t.Fatalf("native baseline bytes missing: %+v", rows)
			}
			if !bytes.Equal(selected.Content, updated) {
				t.Fatalf("native candidate bytes differ: %q", selected.Content)
			}
			actual, err := p.Status()
			if err != nil || actual != status {
				t.Fatalf("comparison changed workspace: %q %q %v", status, actual, err)
			}
			actualHead, err := p.Resolve()
			if err != nil || actualHead != head {
				t.Fatalf("comparison changed native head: %s %s %v", head, actualHead, err)
			}
		})
	}
}
