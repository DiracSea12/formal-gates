package vcs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRegisteredRuntimeFilesPreserveUserAndTrackedChanges(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"git", "svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			var p *Provider
			if name == "git" {
				p = realRepo(t)
			} else {
				p, _ = nativeFixture(t, name)
			}
			tool := ".zcode/skills/formal-gates/prompts/action.md"
			resources := ".formal-gates-resources"
			writeRuntimeStatusFile(t, p.root, tool, "installed tool\n")
			if err := os.MkdirAll(filepath.Join(p.root, resources), 0700); err != nil {
				t.Fatal(err)
			}
			if err := p.VerifyReady(); err == nil {
				t.Fatal("unbound names were treated as tool ownership")
			}
			inventory := RuntimeFiles{Files: []string{canonicalPath(filepath.Join(p.root, tool))}, EmptyDirectories: []string{canonicalPath(filepath.Join(p.root, resources))}}
			p.WithRuntimeFiles(inventory)
			for _, current := range []*Provider{p, p.At(p.root)} {
				if err := current.VerifyReady(); err != nil {
					t.Fatal("verified tool inventory blocked readiness", err)
				}
				if paths, err := current.ChangedPaths(); err != nil || len(paths) != 0 {
					t.Fatalf("tool changed paths=%v error=%v", paths, err)
				}
			}
			users := []string{".zcode/skills/formal-gates/prompts/user.md", ".zcode/another-skill/user.md", ".gates/token-cap.json", resources + "/user.md"}
			for _, path := range users {
				writeRuntimeStatusFile(t, p.root, path, "user bytes: "+path+"\n")
			}
			changes, err := p.ChangedPaths()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range users {
				if !slices.Contains(changes, path) {
					t.Fatalf("user leaf hidden: %s changes=%v", path, changes)
				}
			}
			if slices.Contains(changes, tool) || p.VerifyReady() == nil {
				t.Fatal("ownership changed delivery readiness")
			}
			if err := p.Track(users); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("declared user delivery"); err != nil {
				t.Fatal(err)
			}
			head, err := p.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range users {
				var body []byte
				if name == "git" {
					body, err = p.runner.RunRaw(p.root, "git", nil, "show", head+":"+path)
				} else {
					entry, entryErr := p.Entry(head, path)
					if entryErr != nil {
						t.Fatal(entryErr)
					}
					var value nativeEntry
					err = json.Unmarshal([]byte(entry), &value)
					body = value.Content
				}
				if err != nil || string(body) != "user bytes: "+path+"\n" {
					t.Fatal("delivery lost exact user content", path, err)
				}
			}
			if entry, err := p.Entry(head, tool); err != nil || entry != "" {
				t.Fatal("tool file entered the product snapshot", entry, err)
			}
			if err := p.At(p.root).VerifyReady(); err != nil {
				t.Fatal("delivered work retained unsubmitted tool files", err)
			}
			// A tracked installation file remains a versioned change after modification.
			if err := p.Track([]string{tool}); err != nil {
				t.Fatal(err)
			}
			if err := p.Commit("tracked installation fixture"); err != nil {
				t.Fatal(err)
			}
			if name == "p4" {
				if _, err := p.p4Raw(nil, "edit", tool); err != nil {
					t.Fatal(err)
				}
			}
			writeRuntimeStatusFile(t, p.root, tool, "tracked edit\n")
			if name == "p4" {
				if err := p.Track([]string{tool}); err != nil {
					t.Fatal(err)
				}
			}
			if changes, err := p.ChangedPaths(); err != nil || !slices.Contains(changes, tool) {
				t.Fatalf("tracked tool modification hidden: %v %v", changes, err)
			}
			if p.VerifyReady() == nil {
				t.Fatal("tracked tool modification did not block readiness")
			}
			// Reusing an isolated root does not transfer ownership to the same relative name.
			isolated := t.TempDir()
			writeRuntimeStatusFile(t, isolated, tool, "independent user\n")
			leaves, err := p.At(isolated).untrackedPaths(".zcode")
			if err != nil || !slices.Contains(leaves, tool) {
				t.Fatalf("same name in independent workspace hidden: %v %v", leaves, err)
			}
		})
	}
}

func writeRuntimeStatusFile(t *testing.T, root, path, body string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
