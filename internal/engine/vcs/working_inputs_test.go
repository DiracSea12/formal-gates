package vcs

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkingTreeDigestBindsCleanConvertedBytes(t *testing.T) {
	p := realRepo(t)
	path := filepath.Join(p.root, "material.txt")
	original := []byte("first line\nsecond line\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"material.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("text input"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.WorkingTreeDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.root, ".git", "info", "attributes"), []byte("material.txt text eol=crlf\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, p.runner, p.root, "git", "checkout", "--", "material.txt")
	converted, err := os.ReadFile(path)
	if err != nil || bytes.Equal(original, converted) || !bytes.Contains(converted, []byte("\r\n")) {
		t.Fatalf("checkout did not convert actual input bytes: %q %v", converted, err)
	}
	if status, err := p.Status(); err != nil || status != "" {
		t.Fatalf("converted checkout is not native clean: %q %v", status, err)
	}
	if rows, err := p.CompareWorkingTree(base); err != nil || len(rows) != 0 {
		t.Fatalf("clean converted checkout changed native comparison: %+v %v", rows, err)
	}
	after, err := p.WorkingTreeDigest(base)
	if err != nil || after == before {
		t.Fatalf("different actual input bytes retained source digest: %s %s %v", before, after, err)
	}
}

func TestWorkingTreeDigestBindsSymlinkAndCleanExecutableMode(t *testing.T) {
	p := realRepo(t)
	for _, path := range []string{"first.txt", "second.txt"} {
		writeRuntimeStatusFile(t, p.root, path, "input\n")
	}
	link := filepath.Join(p.root, "alias")
	if err := os.Symlink("first.txt", link); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"first.txt", "second.txt", "alias"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("linked inputs"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.WorkingTreeDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, p.runner, p.root, "git", "config", "core.filemode", "false")
	if err := os.Chmod(filepath.Join(p.root, "first.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	if status, err := p.Status(); err != nil || status != "" {
		t.Fatalf("executable input is not native clean: %q %v", status, err)
	}
	executable, err := p.WorkingTreeDigest(base)
	if err != nil || executable == before {
		t.Fatalf("actual executable mode did not change input digest: %s %v", executable, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second.txt", link); err != nil {
		t.Fatal(err)
	}
	retargeted, err := p.WorkingTreeDigest(base)
	if err != nil || retargeted == executable {
		t.Fatalf("symlink target bytes did not change input digest: %s %v", retargeted, err)
	}
}

func TestWorkingTreeDigestBindsAllContentAndNativeModes(t *testing.T) {
	p := realRepo(t)
	for _, path := range []string{"first.txt", "second.txt", ".gates/tmp/tracked.txt"} {
		writeRuntimeStatusFile(t, p.root, path, "original\n")
	}
	if err := p.Track([]string{"first.txt", "second.txt", ".gates/tmp/tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("input contents"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	previous, err := p.WorkingTreeDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"first.txt", "second.txt", "new.txt", ".gates/tmp/tracked.txt"} {
		if err := os.WriteFile(filepath.Join(p.root, path), []byte{0xff, 0x00, '\n'}, 0644); err != nil {
			t.Fatal(err)
		}
		current, err := p.WorkingTreeDigest(base)
		if err != nil || current == previous {
			t.Fatalf("changed input %s did not change digest: %s %v", path, current, err)
		}
		previous = current
	}
	if err := os.Chmod(filepath.Join(p.root, "first.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	mode, err := p.WorkingTreeDigest(base)
	if err != nil || mode == previous {
		t.Fatalf("native mode did not change digest: %s %v", mode, err)
	}
	if err := os.Remove(filepath.Join(p.root, "second.txt")); err != nil {
		t.Fatal(err)
	}
	deleted, err := p.WorkingTreeDigest(base)
	if err != nil || deleted == mode {
		t.Fatalf("deleted input did not change digest: %s %v", deleted, err)
	}
	index, err := p.IndexEntries()
	if err != nil {
		t.Fatal(err)
	}
	readOnly := New(p.root, comparisonReadRunner{Runner: p.runner})
	if got, err := readOnly.WorkingTreeDigest(base); err != nil || got != deleted {
		t.Fatalf("read-only source capture differs: %s %v", got, err)
	}
	after, err := p.IndexEntries()
	if err != nil || !reflect.DeepEqual(index, after) {
		t.Fatalf("source capture changed native index: %v", err)
	}
}

func TestWorkingTreeDigestUsesExactRuntimeInventory(t *testing.T) {
	p := realRepo(t)
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	tool := ".zcode/skills/formal-gates/prompts/action.md"
	p.WithRuntimeFiles(RuntimeFiles{Files: []string{canonicalPath(filepath.Join(p.root, tool))}})
	original, err := p.WorkingTreeDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".gates/tmp/receipt.json", tool} {
		writeRuntimeStatusFile(t, p.root, path, "runtime\n")
	}
	if got, err := p.WorkingTreeDigest(base); err != nil || got != original {
		t.Fatalf("verified runtime leaves changed inputs: %s %v", got, err)
	}
	for _, path := range []string{".zcode/skills/formal-gates/prompts/user.md", ".gates/token-cap.json"} {
		writeRuntimeStatusFile(t, p.root, path, "user bytes\n")
		got, err := p.WorkingTreeDigest(base)
		if err != nil || got == original {
			t.Fatalf("user input beside runtime leaf disappeared: %s %s %v", path, got, err)
		}
		original = got
	}
	if got, err := p.WorkingTreeDigest("missing-source"); err == nil || !strings.Contains(err.Error(), "missing-source") || got != "" {
		t.Fatalf("unavailable native source accepted: %s %v", got, err)
	}
}

func TestWorkingTreeDigestPreservesNativeProviderInputs(t *testing.T) {
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			p, runner := nativeFixture(t, name)
			base, err := p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			before, err := p.WorkingTreeDigest(base)
			if err != nil {
				t.Fatal(err)
			}
			if name == "p4" {
				nativeCommand(t, runner, p.root, "p4", "edit", "body.txt")
			}
			if err := os.WriteFile(filepath.Join(p.root, "body.txt"), []byte{0xff, 0xfe, '\n'}, 0644); err != nil {
				t.Fatal(err)
			}
			changed, err := p.WorkingTreeDigest(base)
			if err != nil || changed == before {
				t.Fatalf("native input bytes unchanged: %s %v", changed, err)
			}
			writeRuntimeStatusFile(t, p.root, "new.txt", "new user input\n")
			untracked, err := p.WorkingTreeDigest(base)
			if err != nil || untracked == changed {
				t.Fatalf("native untracked input disappeared: %s %v", untracked, err)
			}
			status, err := p.Status()
			if err != nil {
				t.Fatal(err)
			}
			if again, err := p.WorkingTreeDigest(base); err != nil || again != untracked {
				t.Fatalf("repeated native input differs: %s %v", again, err)
			}
			if after, err := p.Status(); err != nil || after != status {
				t.Fatalf("capture changed native workspace: %s %s %v", after, status, err)
			}
			if after, err := p.Resolve(); err != nil || after != base {
				t.Fatalf("capture changed source identity: %s %s %v", after, base, err)
			}
		})
	}
}
