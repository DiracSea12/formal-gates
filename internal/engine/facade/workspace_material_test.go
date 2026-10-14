package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Issued material is observed by its own identity after later confirmation.
// Formal requirement bytes follow the issued revision; other committed input
// artifacts remain owned by their native source.
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

			source, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(t.TempDir(), "authoring")
			if err := d.f.vcs.AddWorktree(workspace, source); err != nil {
				t.Fatal(err)
			}
			original := protocol.WorkspaceParameters{RequirementRevision: d.revision, Resource: protocol.NativeResource{Path: workspace, Source: source, Role: "development"}}
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
			current := protocol.WorkspaceParameters{RequirementRevision: d.revision, Generation: state.Validation.Generation, Resource: protocol.NativeResource{Path: fresh, Source: source, Role: "gate"}}
			invoked = false
			complete, err = d.f.injectRequirements(state, current, false, &invoked)
			if err != nil || complete || invoked {
				t.Fatal("observation accepted missing issued material or performed a write", complete, invoked, err)
			}
			complete, err = d.f.injectRequirements(state, current, true, &invoked)
			if err != nil || !complete || !invoked {
				t.Fatal("current producer lost its exact issued requirement", complete, invoked, err)
			}
			want := currentDocument
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

// Native workspace receipts retain the confirmed context and protect product bytes.
func TestBlackboxWorkspaceReceiptPreservesConfirmedContext(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "untracked"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			if !committed {
				runner := vcs.ExecRunner{}
				if _, err := runner.Run(root, "git", "rm", "--cached", "requirements.md"); err != nil {
					t.Fatal(err)
				}
				if _, err := runner.Run(root, "git", "commit", "-m", "project without committed material"); err != nil {
					t.Fatal(err)
				}
				var err error
				base, err = vcs.New(root, nil).Resolve()
				if err != nil {
					t.Fatal(err)
				}
			}
			d := nativeMasterAt(t, root, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			document := validationDocument + "\n"
			confirmValidationDocument(d, "", nil, document)
			d.revision = driverState(t, d).RequirementRevision()
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
			d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
			d.drive()
			design, input := d.action("engine.qa.design.v5")
			state := driverState(t, d)
			resource := state.NativeResources[input.Binding.WorkspaceID]
			if resource.ID != input.Binding.WorkspaceID || resource.Owner != state.RunID || resource.Role != "blackbox-design" || resource.Path == "" || resource.Path == root || resource.Source != base || resource.Identity != base || resource.Status != "READY" || resource.Ref == "" || state.QAWorktree != resource.Path {
				t.Fatalf("native design resource: %+v", resource)
			}
			if state.Validation.Document.Content != document || state.Validation.Document.ContentDigest != strings.TrimPrefix(d.revision, "sha256:") || state.RequirementRevision() != d.revision || !reflect.DeepEqual(input.RequirementDocument, state.Validation.Document) {
				t.Fatal("native design lost full current confirmation")
			}
			inputs := state.WorkerInputs(d.f.definition)
			if len(inputs) != len(d.run.PendingActions) || inputs[design].Validation == nil {
				t.Fatal("pending roles lack their original worker inputs")
			}
			for id, worker := range inputs {
				if !reflect.DeepEqual(worker.RequirementDocument, state.Validation.Document) || worker.Validation == nil || !reflect.DeepEqual(worker.Validation.RequirementDocument, state.Validation.Document) || !reflect.DeepEqual(d.run.ValidationInputs[id].RequirementDocument, state.Validation.Document) {
					t.Fatalf("role %s lost current context", worker.Handler)
				}
			}
			var intent protocol.HostActionIntent
			var receipt protocol.HostActionReceipt
			for id, effect := range state.ReconciledEffects {
				if effect.Intent == nil || effect.Intent.Adapter == nil || effect.Intent.Adapter.Operation != "op.workspace" {
					continue
				}
				var params protocol.WorkspaceParameters
				if err := decodeNativeParams(*effect.Intent, &params); err != nil {
					t.Fatal(err)
				}
				if params.Resource.ID != resource.ID {
					continue
				}
				intent, receipt = *effect.Intent, state.HostActionReceipts[id]
				expected := params.Resource
				expected.Identity, expected.Status = expected.Source, "READY"
				if params.Task == nil || *params.Task != state.PendingActions[design].Task || params.RequirementRevision != d.revision || params.Generation != state.Validation.Generation || params.RouteScope == "" || params.Manifest != nil || expected != resource {
					t.Fatalf("signed design input: %+v", params)
				}
			}
			if intent.ActionID == "" || receipt.ActionID != intent.ActionID || receipt.AdapterOperation != "op.workspace" || receipt.Provider != state.RunProvider || receipt.Correlation != intent.Correlation || receipt.PayloadDigest != intent.PayloadDigest || receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterEvidence == nil {
				t.Fatalf("native design receipt: %+v", receipt)
			}
			var evidence protocol.WorkspaceEvidence
			raw, err := json.Marshal(receipt.AdapterEvidence.Values)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &evidence); err != nil {
				t.Fatal(err)
			}
			tree, err := d.f.vcs.TreeDigest(base)
			if err != nil {
				t.Fatal(err)
			}
			if evidence.ParametersDigest != intent.PayloadDigest || evidence.Resource != resource || evidence.SourceDigest != tree || evidence.Digest != tree {
				t.Fatalf("native design proof: %+v", evidence)
			}
			want := document
			body, err := os.ReadFile(filepath.Join(resource.Path, "requirements.md"))
			if err != nil || string(body) != want || requirementContentRevision(body) != requirementContentRevision([]byte(want)) {
				t.Fatal("native design material differs from its exact protected bytes", err)
			}
			if head, err := d.f.vcs.At(resource.Path, resource.Handle).Resolve(); err != nil || head != resource.Source {
				t.Fatalf("native design source: %s %v", head, err)
			}
			d.reopen()
			state = driverState(t, d)
			if state.QAWorktree != resource.Path || state.RequirementRevision() != d.revision || state.NativeResources[resource.ID] != resource || !reflect.DeepEqual(state.HostActionReceipts[receipt.ActionID], receipt) || !reflect.DeepEqual(state.Validation.Document, input.RequirementDocument) || !reflect.DeepEqual(state.WorkerInputs(d.f.definition), inputs) || !reflect.DeepEqual(d.run.ValidationInputs[design], input) {
				t.Fatal("reopen lost receipt, source, projection or complete confirmed inputs")
			}
			event, err := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+receipt.ActionID), receipt.ActionID, receipt.AdapterOperation, receipt.Provider, receipt.Correlation, receipt.PayloadDigest, receipt.Status, receipt.FailureClass, receipt.AdapterEvidence.Values)
			if err != nil {
				t.Fatal(err)
			}
			before := installationFiles(t, d.f.stateDir)
			nativeBefore := installationFiles(t, resource.Path)
			worktrees, err := d.f.vcs.Worktrees()
			if err != nil {
				t.Fatal(err)
			}
			ref, err := d.f.vcs.Ref(resource.Ref)
			if err != nil || ref != resource.Source {
				t.Fatalf("native design ref: %s %v", ref, err)
			}
			if _, _, err := d.f.Submit(event); err != nil {
				t.Fatal(err)
			}
			if progress, err := d.f.completeNativeIntent(driverState(t, d), intent, false); err != nil || progress {
				t.Fatalf("accepted receipt was observed again: %t %v", progress, err)
			}
			afterWorktrees, err := d.f.vcs.Worktrees()
			if err != nil {
				t.Fatal(err)
			}
			afterRef, err := d.f.vcs.Ref(resource.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, installationFiles(t, d.f.stateDir)) || !reflect.DeepEqual(nativeBefore, installationFiles(t, resource.Path)) || !reflect.DeepEqual(worktrees, afterWorktrees) || ref != afterRef {
				t.Fatal("original receipt replay changed state, native bytes, worktrees or ref")
			}
		})
	}
}

// A selected source artifact is a product file rather than the formal requirement.
// Its committed development value is not replaced by the intake's original bytes.
func TestWorkspaceMaterialPreservesCommittedSourceArtifact(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := nativeMasterAt(t, root, base, nil)
	passPreDevelopmentReviews(t, d, commandPlan())
	intake, err := PrepareIntake(root, "requirements.md", "", []string{"app.go"}, true)
	if err != nil {
		t.Fatal(err)
	}
	state := driverState(t, d)
	state.IntakeConfirmationReceipt = &intake.IntakeCandidate
	workspace := filepath.Join(t.TempDir(), "development")
	if err := d.f.vcs.AddWorktree(workspace, base); err != nil {
		t.Fatal(err)
	}
	native := d.f.vcs.At(workspace, vcs.WorkspaceHandle{})
	body := []byte("package main // native implementation\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(workspace, "app.go"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := native.Track([]string{"app.go"}); err != nil {
		t.Fatal(err)
	}
	if err := native.Commit("native implementation"); err != nil {
		t.Fatal(err)
	}
	head, err := native.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkspaceParameters{RequirementRevision: d.revision, Resource: protocol.NativeResource{Path: workspace, Source: base, Role: "development"}}
	invoked := false
	complete, err := d.f.injectRequirements(state, input, false, &invoked)
	if err != nil || !complete || invoked {
		t.Fatalf("source artifact observation: %t %t %v", complete, invoked, err)
	}
	actual, err := os.ReadFile(filepath.Join(workspace, "app.go"))
	if err != nil || string(actual) != string(body) {
		t.Fatalf("committed source artifact was replaced: %q %v", actual, err)
	}
	if after, err := native.Resolve(); err != nil || after != head {
		t.Fatalf("source artifact observation moved its identity: %s %v", after, err)
	}
}
