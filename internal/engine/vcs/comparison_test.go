package vcs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
			beforeStatus, err := p.Status()
			if err != nil {
				t.Fatal(err)
			}
			workingRows, err := p.CompareWorkingTree(base)
			if err != nil {
				t.Fatal(err)
			}
			var working *SnapshotComparison
			for i := range workingRows {
				if workingRows[i].Path == "body.txt" {
					working = &workingRows[i]
				}
			}
			if working == nil || !bytes.Equal(working.BaseContent, original) || !bytes.Equal(working.Content, updated) {
				t.Fatalf("original working comparison bytes: %+v", workingRows)
			}
			if status, err := p.Status(); err != nil || status != beforeStatus {
				t.Fatalf("working comparison changed native status: %q %q %v", beforeStatus, status, err)
			}
			if identity, err := p.Resolve(); err != nil || identity != base {
				t.Fatalf("working comparison changed native source: %s %s %v", base, identity, err)
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

// Only native read operations can obtain a working comparison; errors remain errors.
func TestWorkingTreeComparisonUsesReadOnlyNativeCommands(t *testing.T) {
	root := t.TempDir()
	p := New(root, nil)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, err := p.runner.Run(root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "original.txt"), []byte(" original bytes \n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"original.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("Comparison source"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "original.txt"), []byte(" working bytes \n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("untracked\n"), 0644); err != nil {
		t.Fatal(err)
	}
	guard := comparisonReadRunner{Runner: p.runner}
	readonly := New(root, guard)
	rows, err := readonly.CompareWorkingTree(base)
	if err != nil || len(rows) != 2 {
		t.Fatalf("readonly original comparison: %+v %v", rows, err)
	}
	guard.failOriginal = true
	if _, err := New(root, guard).CompareWorkingTree(base); err == nil || !strings.Contains(err.Error(), "original native bytes unavailable") {
		t.Fatalf("unavailable original bytes must fail capture: %v", err)
	}
}

type comparisonReadRunner struct {
	Runner
	failOriginal bool
}

func (r comparisonReadRunner) Run(dir, command string, args ...string) (string, error) {
	body, err := r.RunRaw(dir, command, nil, args...)
	return strings.TrimSpace(string(body)), err
}

func (r comparisonReadRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	if command != "git" || len(args) == 0 || !slices.Contains([]string{"rev-parse", "ls-tree", "diff", "ls-files", "show"}, args[0]) || len(input) != 0 {
		return nil, fmt.Errorf("comparison attempted a native mutation: %s %q", command, args)
	}
	if r.failOriginal && args[0] == "show" {
		return nil, fmt.Errorf("original native bytes unavailable")
	}
	return r.Runner.RunRaw(dir, command, input, args...)
}
