package acceptance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// Development starts from the accepted native HEAD; base continues to own the
// review and squash range. Frozen and delivered content includes existing inputs.
func TestNativeDeliveryPreservesStartupContent(t *testing.T) {
	for _, ancestor := range []bool{true, false} {
		name := "startup-base"
		if ancestor {
			name = "ancestor-base"
		}
		t.Run(name, func(t *testing.T) {
			p := newInstalledStartProject(t)
			files := map[string]string{"requirements.md": candidateDocument, "go.mod": "module candidate-app\n\ngo 1.22\n", "old_test.go": "package main\nimport \"testing\"\nfunc TestOld(t *testing.T) {}\n", "remove_test.go": "package main\nimport \"testing\"\nfunc TestRemove(t *testing.T) {}\n", "mode_test.go": "package main\nimport \"testing\"\nfunc TestMode(t *testing.T) {}\n"}
			for path, body := range files {
				if err := os.WriteFile(filepath.Join(p.project, path), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			p.git(t, "add", "--all")
			p.git(t, "commit", "-qm", "Confirmed application inputs")
			p.base = p.git(t, "rev-parse", "HEAD")
			existing := map[string]struct {
				body string
				mode os.FileMode
			}{
				"notes.txt":       {"Existing project note\n", 0644},
				"assets/data.bin": {"\x00\x01existing\xff", 0644},
				"tools/check.sh":  {"#!/bin/sh\nexit 0\n", 0755},
			}
			for path, f := range existing {
				file := filepath.Join(p.project, path)
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(f.body), f.mode); err != nil {
					t.Fatal(err)
				}
			}
			p.git(t, "add", "--all")
			p.git(t, "commit", "-qm", "Existing project content")
			head := p.git(t, "rev-parse", "HEAD")
			if !ancestor {
				p.base = head
			}
			sum := sha256.Sum256([]byte(candidateDocument))
			p.intake.RequirementRevision = hex.EncodeToString(sum[:])
			p.intake.Artifacts[0].Revision = p.intake.RequirementRevision
			writeInstalledJSON(t, p.intakePath, p.intake)
			p.install(t, "codex", "global")
			p.call(t, nil, p.startArgs("candidate", "regular")...)
			d := &candidateDriver{p: p, t: t, mode: "none"}
			d.drive()
			d.reviews()
			_, state := p.bindings(t, "candidate", "codex")
			if state.InitialSnapshot != head || state.BaseSnapshot != p.base {
				t.Fatalf("startup identities: initial=%s base=%s, want %s %s", state.InitialSnapshot, state.BaseSnapshot, head, p.base)
			}
			for n := 0; n < 30; n++ {
				if d.run.Validation != nil && d.run.Validation.Candidate != nil {
					d.candidate = d.run.Validation.Candidate.Identity
					d.candidateTree = d.run.Validation.Candidate.Digest
					break
				}
				var ids []string
				for id := range d.run.ValidationInputs {
					ids = append(ids, id)
				}
				slices.Sort(ids)
				if len(ids) == 0 {
					t.Fatal("no issued development or QA work")
				}
				for _, id := range ids {
					in := d.run.ValidationInputs[id]
					switch in.Handler {
					case "engine.development.batch.v4":
						if in.Workspace == nil || in.Workspace.Source != head || in.Binding.WorkspaceID != in.Workspace.ID {
							t.Fatalf("wrong development source: %+v", in)
						}
						d.develop(id, in)
					case "engine.qa.design.v5":
						d.design(id, in)
					case "engine.qa.review.rationale.v2":
						d.caseReview(id, in)
					default:
						t.Fatalf("unexpected worker %s", in.Handler)
					}
				}
				d.drive()
				if got := p.git(t, "rev-parse", "HEAD"); got != head {
					t.Fatalf("target changed before validation: %s != %s", got, head)
				}
			}
			if d.candidate == "" {
				t.Fatal("candidate missing")
			}
			checkContent := func(identity string) {
				t.Helper()
				for path, f := range existing {
					out, err := exec.Command("git", "-C", p.project, "show", identity+":"+path).CombinedOutput()
					if err != nil || string(out) != f.body {
						t.Fatalf("%s:%s content=%q err=%v", identity, path, out, err)
					}
					mode := "100644"
					if f.mode&0111 != 0 {
						mode = "100755"
					}
					entry := p.git(t, "ls-tree", identity, "--", path)
					if len(entry) < 6 || entry[:6] != mode {
						t.Fatalf("%s:%s mode=%q want %s", identity, path, entry, mode)
					}
				}
			}
			checkContent(d.candidate)
			d.executeWorkers()
			d.drive()
			final := p.git(t, "rev-parse", "HEAD")
			if p.git(t, "rev-parse", final+"^{tree}") != d.candidateTree {
				t.Fatal("promotion changed the verified candidate tree")
			}
			checkContent(final)
			if d.run.Status != "COMPLETE" || d.run.PromotedSnapshot != final {
				t.Fatalf("delivery did not close: %s %s", d.run.Status, d.run.PromotedSnapshot)
			}
			if d.run.Termination == nil || d.run.Termination.InitialSnapshot != head || d.run.Termination.BaseSnapshot != p.base {
				t.Fatal("terminal evidence lost initial/base identities")
			}
			d.assertAcceptedReplay(true)
			t.Logf("base=%s initial=%s Qn=%s tree=%s Final=%s", p.base, head, d.candidate, d.candidateTree, final)
		})
	}
}
