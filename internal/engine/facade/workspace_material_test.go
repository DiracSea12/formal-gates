package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Issued material is observed by its own identity after later confirmation.
// A committed product artifact retains its bytes; current prose travels in input.
func TestWorkspaceMaterialUsesIssuedIdentityAndPreservesNativeContent(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "untracked"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			runner := vcs.ExecRunner{}
			if !committed {
				if _, err := runner.Run(root, "git", "rm", "--cached", "requirements.md"); err != nil {
					t.Fatal(err)
				}
				if _, err := runner.Run(root, "git", "commit", "-m", "project without committed material"); err != nil {
					t.Fatal(err)
				}
			}
			d := nativeMasterAt(t, root, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
			source, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(t.TempDir(), "authoring")
			if err := d.f.vcs.AddWorktree(workspace, source); err != nil {
				t.Fatal(err)
			}
			original := protocol.WorkspaceParameters{RequirementRevision: d.revision, Resource: protocol.GitResource{Path: workspace, Source: source, Role: "development"}}
			invoked := false
			complete, err := d.f.injectRequirements(driverState(t, d), original, true, &invoked)
			if err != nil || !complete || invoked == committed {
				t.Fatal("initial material was not established honestly", complete, invoked, err)
			}
			currentDocument := validationDocument + "\n"
			if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(strings.ReplaceAll(currentDocument, "\n", "\r\n")), 0600); err != nil {
				t.Fatal(err)
			}
			confirmValidationDocument(d, "", nil, currentDocument)
			d.revision = driverState(t, d).RequirementRevision()
			d.reopen()
			state := driverState(t, d)
			if state.Validation.Document.Content != currentDocument || state.Validation.Document.ContentDigest != strings.TrimPrefix(d.revision, "sha256:") {
				t.Fatal("reopen lost normalized full confirmation")
			}
			invoked = false
			complete, err = d.f.injectRequirements(state, original, false, &invoked)
			if err != nil || !complete || invoked {
				t.Fatal("lost response observation reused later material", complete, invoked, err)
			}
			body, err := os.ReadFile(filepath.Join(workspace, "requirements.md"))
			if err != nil || string(body) != validationDocument {
				t.Fatal("old action changed its native material", err)
			}
			fresh := filepath.Join(t.TempDir(), "current")
			if err := d.f.vcs.AddWorktree(fresh, source); err != nil {
				t.Fatal(err)
			}
			current := protocol.WorkspaceParameters{RequirementRevision: d.revision, Generation: state.Validation.Generation, Resource: protocol.GitResource{Path: fresh, Source: source, Role: "gate"}}
			invoked = false
			complete, err = d.f.injectRequirements(state, current, false, &invoked)
			if err != nil || complete != committed || invoked {
				t.Fatal("observation wrote missing material or lost committed content", complete, invoked, err)
			}
			complete, err = d.f.injectRequirements(state, current, true, &invoked)
			if err != nil || !complete || invoked == committed {
				t.Fatal("current producer lost its signed material or native protection", complete, invoked, err)
			}
			want := currentDocument
			if committed {
				want = validationDocument
			}
			body, err = os.ReadFile(filepath.Join(fresh, "requirements.md"))
			if err != nil || string(body) != want {
				t.Fatal("current/native material bytes differ", err)
			}
			invoked = false
			complete, err = d.f.injectRequirements(state, current, false, &invoked)
			if err != nil || !complete || invoked {
				t.Fatal("completed producer was replayed", complete, invoked, err)
			}
			if !committed {
				if err := os.Remove(filepath.Join(workspace, "requirements.md")); err != nil {
					t.Fatal(err)
				}
				complete, err = d.f.injectRequirements(state, original, false, &invoked)
				if err != nil || complete || invoked {
					t.Fatal("stale incomplete action borrowed current material", complete, invoked, err)
				}
				if _, err := os.Stat(filepath.Join(workspace, "requirements.md")); !os.IsNotExist(err) {
					t.Fatal("stale observation recreated material", err)
				}
			}
		})
	}
}

// A base worktree registers the current confirmed material without changing
// its native source or borrowing the immutable intake's earlier identity.
func TestQAWorktreeRegistrationUsesConfirmedMaterial(t *testing.T) {
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, commandPlan())
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(d.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	confirmValidationDocument(d, "", nil, document)
	d.revision = driverState(t, d).RequirementRevision()
	path := filepath.Join(t.TempDir(), "baseline")
	if err := d.f.vcs.AddWorktree(path, d.run.BaseSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.f.RegisterQAWorktree(path); err != nil {
		t.Fatal("current confirmed QA material was rejected", err)
	}
	d.reopen()
	if state := driverState(t, d); state.QAWorktree != path || state.RequirementRevision() != d.revision {
		t.Fatal("registered material lost current identity on reopen")
	}
	before := installationFiles(t, d.f.stateDir)
	if err := d.f.RegisterQAWorktree(path); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, installationFiles(t, d.f.stateDir)) {
		t.Fatal("registration replay rewrote the current material binding")
	}
}
