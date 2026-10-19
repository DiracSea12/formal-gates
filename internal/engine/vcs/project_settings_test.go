package vcs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

func TestProjectSettingsRuntimePathsAreExact(t *testing.T) {
	for _, path := range []string{".gates/config.json", ".gates/config.json.lock"} {
		if !RuntimePath(filepath.FromSlash(path)) {
			t.Fatal("tool-managed settings path not recognized", path)
		}
	}
	for _, path := range []string{".gates", ".gates/token-cap.json", ".gates/other-tool.json", ".gates/config.json.backup", ".gates/config.json.lock.backup", "config.json", "other/.gates/config.json", ".gates/config.json/child"} {
		if RuntimePath(filepath.FromSlash(path)) {
			t.Fatal("non-settings path classified as runtime", path)
		}
	}
}

func TestProjectSettingsPreserveSnapshotReadiness(t *testing.T) {
	for _, name := range []string{"git", "svn"} {
		t.Run(name, func(t *testing.T) {
			var p *Provider
			if name == "git" {
				p = realRepo(t)
			} else {
				p, _ = nativeFixture(t, name)
				writeRuntimeStatusFile(t, p.root, ".gates/keep.txt", "project material\n")
				if err := p.Track([]string{".gates/keep.txt"}); err != nil {
					t.Fatal(err)
				}
				nativeCommand(t, p.runner, p.root, "svn", "propset", "svn:ignore", "*.json\n*.lock\nconfig.json.backup", ".gates")
				if err := p.Commit("project ignore rules"); err != nil {
					t.Fatal(err)
				}
			}
			home := t.TempDir()
			if _, err := settings.SaveConfiguration(p.root, home, "project", "personal", settings.DefaultConfigurationName, nil); err != nil {
				t.Fatal(err)
			}
			leaves := []string{".gates/config.json", ".gates/config.json.lock"}
			for _, leaf := range leaves {
				if _, err := os.Stat(filepath.Join(p.root, leaf)); err != nil {
					t.Fatal(err)
				}
			}
			if name == "svn" {
				t.Log(nativeCommand(t, p.runner, p.root, "svn", "status", "--xml", "--no-ignore"))
			} else {
				t.Log(nativeCommand(t, p.runner, p.root, "git", "status", "--porcelain=v1", "--untracked-files=all"))
			}
			base := nativeIdentityAt(t, p)
			for _, current := range []*Provider{p, p.At(p.root), NewNative(p.root, name, p.runner)} {
				if err := current.VerifyReady(); err != nil {
					t.Errorf("saved settings blocked readiness: %v", err)
				}
				if paths, err := current.ChangedPaths(); err != nil || len(paths) != 0 {
					t.Errorf("settings changed paths=%v error=%v", paths, err)
				}
				if pending, err := current.Pending(base, base); err != nil || !pending.Clean || !pending.Equal || len(pending.Changes) != 0 {
					t.Errorf("settings changed pending snapshot: %+v error=%v", pending, err)
				}
			}
			if t.Failed() {
				return
			}
			for i := 0; i < 2; i++ {
				if _, err := settings.UseConfiguration(p.root, home, "project", "personal", nil); err != nil {
					t.Fatal(err)
				}
				if err := p.VerifyReady(); err != nil {
					t.Fatal("repeated settings save blocked readiness", err)
				}
			}
			before := map[string][]byte{}
			for _, leaf := range leaves {
				body, err := os.ReadFile(filepath.Join(p.root, leaf))
				if err != nil {
					t.Fatal(err)
				}
				before[leaf] = body
			}
			writeRuntimeStatusFile(t, p.root, "delivery.txt", "delivery bytes\n")
			if _, _, err := p.ValidateDeclaredChanges(base, []string{"delivery.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := p.Track([]string{"delivery.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("declared delivery"); err != nil {
				t.Fatal(err)
			}
			if err := p.VerifyReady(); err != nil {
				t.Fatal(err)
			}
			head := nativeIdentityAt(t, p)
			if rows, err := p.CompareSnapshots(base, head); err != nil || len(rows) != 1 || rows[0].Path != "delivery.txt" || string(rows[0].Content) != "delivery bytes\n" {
				t.Fatalf("snapshot includes non-delivery changes: %+v %v", rows, err)
			}
			for _, leaf := range leaves {
				if entry, err := p.Entry(head, leaf); err != nil || entry != "" {
					t.Fatal("settings entered delivery snapshot", leaf, entry, err)
				}
				if after, err := os.ReadFile(filepath.Join(p.root, leaf)); err != nil || !bytes.Equal(before[leaf], after) {
					t.Fatal("snapshot changed saved settings", leaf, err)
				}
			}
			users := []string{".gates/token-cap.json", ".gates/other-tool.json", ".gates/config.json.backup", ".gates/config.json.lock.backup", "config.json"}
			slices.Sort(users)
			for _, path := range users {
				writeRuntimeStatusFile(t, p.root, path, "user material\n")
			}
			if changes, err := p.ChangedPaths(); err != nil || !slices.Equal(changes, users) {
				t.Fatalf("user neighbours hidden: %v %v", changes, err)
			}
			if p.VerifyReady() == nil {
				t.Fatal("user material did not block readiness")
			}
			for _, path := range users {
				if err := os.Remove(filepath.Join(p.root, path)); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.Track(leaves); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("versioned project settings"); err != nil {
				t.Fatal(err)
			}
			for _, leaf := range leaves {
				writeRuntimeStatusFile(t, p.root, leaf, "tracked edit\n")
			}
			if changes, err := p.ChangedPaths(); err != nil || !slices.Equal(changes, leaves) {
				t.Fatalf("tracked settings hidden: %v %v", changes, err)
			}
			if p.VerifyReady() == nil {
				t.Fatal("tracked settings edits did not block readiness")
			}
		})
	}
}

func TestSVNUnversionedProjectSettingsDirectory(t *testing.T) {
	p, _ := nativeFixture(t, "svn")
	if _, err := settings.SaveConfiguration(p.root, t.TempDir(), "project", "personal", settings.DefaultConfigurationName, nil); err != nil {
		t.Fatal(err)
	}
	t.Log(nativeCommand(t, p.runner, p.root, "svn", "status", "--xml", "--no-ignore"))
	if err := p.VerifyReady(); err != nil {
		t.Fatal(err)
	}
	if paths, err := p.ChangedPaths(); err != nil || len(paths) != 0 {
		t.Fatalf("unversioned settings paths=%v error=%v", paths, err)
	}
	base := nativeIdentityAt(t, p)
	if pending, err := p.Pending(base, base); err != nil || !pending.Clean || !pending.Equal || len(pending.Changes) != 0 {
		t.Fatalf("unversioned settings changed pending snapshot: %+v error=%v", pending, err)
	}
	writeRuntimeStatusFile(t, p.root, ".gates/token-cap.json", "user cap\n")
	if paths, err := p.ChangedPaths(); err != nil || !slices.Equal(paths, []string{".gates/token-cap.json"}) {
		t.Fatalf("unversioned neighbour hidden: %v error=%v", paths, err)
	}
	if p.VerifyReady() == nil {
		t.Fatal("unversioned neighbour did not block readiness")
	}
}

func TestP4ProjectSettingsStatusPreservesOpenedChanges(t *testing.T) {
	const reconcile = "p4 -c main -ztag -Mj reconcile -n //main/..."
	const opened = "p4 -c main -ztag -Mj opened //main/..."
	leaves := []string{".gates/config.json", ".gates/config.json.lock"}
	for _, test := range []struct {
		name, change string
		users        []string
		want         []string
	}{
		{name: "untracked"},
		{name: "neighbours", users: []string{".gates/token-cap.json", ".gates/other-tool.json", ".gates/config.json.backup"}, want: []string{".gates/config.json.backup", ".gates/other-tool.json", ".gates/token-cap.json"}},
		{name: "opened", change: "default", want: leaves},
	} {
		t.Run(test.name, func(t *testing.T) {
			var pending, versioned strings.Builder
			for _, path := range append(slices.Clone(leaves), test.users...) {
				record := map[string]string{"clientFile": "//main/" + path, "action": "add"}
				if test.change != "" {
					record["action"], record["change"] = "edit", test.change
					if err := json.NewEncoder(&versioned).Encode(record); err != nil {
						t.Fatal(err)
					}
				} else if err := json.NewEncoder(&pending).Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			runner := &stubRunner{responses: map[string]string{reconcile: pending.String(), opened: versioned.String()}}
			p := NewNative(t.TempDir(), "p4", runner)
			p.handle.Client = "main"
			if paths, err := p.ChangedPaths(); err != nil || !slices.Equal(paths, test.want) {
				t.Fatalf("P4 paths=%v want=%v error=%v", paths, test.want, err)
			}
			if err := p.VerifyReady(); (err == nil) != (len(test.want) == 0) {
				t.Fatalf("P4 readiness error=%v want paths=%v", err, test.want)
			}
			for i, command := range runner.commands {
				want := reconcile
				if i%2 == 1 {
					want = opened
				}
				if strings.Join(command, " ") != want {
					t.Fatalf("unexpected P4 invocation: %q", command)
				}
			}
			if len(runner.commands) != 4 {
				t.Fatalf("P4 status did not inspect reconcile and opened: %v", runner.commands)
			}
		})
	}
}
