package facade

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// A native workspace keeps its copied source and its accepted editing prefix
// as separate facts. These fixtures use real private repositories and clients.
func nativeCandidateWorkspace(t *testing.T, name string) (*Facade, *protocol.State, *vcs.Provider, *nativeProviderRunner) {
	t.Helper()
	root, source, runner := nativeProviderFixture(t, name)
	native := vcs.NewNative(root, name, runner)
	resource := protocol.NativeResource{ID: "editing", Owner: "candidate", Role: "development", Path: filepath.Join(nativeFixtureDirectory(t), "editing"), Source: source, Status: "READY"}
	resource.Handle = vcs.PlanWorkspace(name, source, resource.Owner, resource.ID)
	if err := native.CreateWorkspace(resource.Path, source, "formal-gates workspace candidate editing workspace-input", resource.Handle); err != nil {
		t.Fatal(err)
	}
	editing := native.At(resource.Path, resource.Handle)
	var err error
	resource.Identity, err = editing.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest, err := native.TreeDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := editing.TreeDigest(resource.Identity)
	if err != nil || digest != sourceDigest {
		t.Fatalf("workspace did not preserve actual native source: %s %s %v", digest, sourceDigest, err)
	}
	proof := protocol.WorkspaceEvidence{ParametersDigest: "workspace-input", Resource: resource, Digest: digest, SourceDigest: sourceDigest}
	values, err := evidenceFields(proof)
	if err != nil {
		t.Fatal(err)
	}
	state := &protocol.State{RunID: resource.Owner, VCSName: name, Validation: &protocol.Validation{Sets: map[coverage.ReviewKind]*protocol.CaseSet{}}, NativeResources: map[string]protocol.NativeResource{resource.ID: resource}, HostActionReceipts: map[string]protocol.HostActionReceipt{
		"editing": {ActionID: "editing", AdapterOperation: "op.workspace", Status: protocol.HostActionStatusExecuted, AdapterEvidence: &protocol.AdapterEvidence{Values: values}},
	}}
	f := &Facade{root: root, vcs: native, admission: *testAdmission(t, root, resource.Owner, "sha256:candidate-package", DefaultProvider)}
	return f, state, editing, runner
}

func nativeCandidateIntent(t *testing.T, stage, base string, paths ...string) protocol.HostActionIntent {
	t.Helper()
	params, err := evidenceFields(protocol.CandidateParameters{Stage: stage, WorkspaceID: "editing", Base: base, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.HostActionIntent{ActionID: stage, PayloadDigest: "candidate-input-" + stage, Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: params}}
}

func TestNativeCandidateFreezeKeepsAcceptedSourcePrefix(t *testing.T) {
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			f, state, native, runner := nativeCandidateWorkspace(t, name)
			resource := state.NativeResources["editing"]
			start := resource.Identity
			if err := os.WriteFile(filepath.Join(resource.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) > 1 }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			intent := nativeCandidateIntent(t, "CHECK_SOURCE", start, "app.go")
			result, complete, err := f.candidateEvidence(state, intent, true, new(bool))
			if err != nil || !complete || result.Candidate == nil {
				t.Fatalf("freeze editing source: %+v %t %v", result, complete, err)
			}
			count, err := native.CommitCount(start, result.Candidate.Identity)
			if err != nil || count != 1 {
				t.Fatalf("source freeze does not have one native commit: %d %v", count, err)
			}
			resource.Identity = result.Candidate.Identity
			state.NativeResources[resource.ID] = resource
			intent = nativeCandidateIntent(t, "DEVELOPMENT", resource.Identity, "app.go")
			head, err := native.Resolve()
			if err != nil || head != resource.Identity || state.ResourceStartIdentity(resource) == head {
				t.Fatalf("accepted source prefix fixture is not exact: head=%s base=%s start=%s %v", head, resource.Identity, state.ResourceStartIdentity(resource), err)
			}
			invoked := false
			result, complete, err = f.candidateEvidence(state, intent, true, &invoked)
			if err != nil || !complete || result.Candidate == nil || result.Candidate.Identity != head || len(result.Candidate.Changes) != 0 {
				t.Fatalf("HEAD equals issued source, but development freeze failed: %+v complete=%t invoked=%t error=%v", result, complete, invoked, err)
			}
			// A reopened observer uses the original parameters even while the
			// completion receipt remains UNKNOWN; it creates no second commit.
			state.HostActionReceipts[intent.ActionID] = protocol.HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.candidate", Status: protocol.HostActionStatusUnknown}
			reopened := &Facade{root: f.root, vcs: vcs.NewNative(f.root, name, runner), admission: f.admission}
			invoked = false
			observed, complete, err := reopened.candidateEvidence(state, intent, false, &invoked)
			if err != nil || !complete || invoked || !reflect.DeepEqual(observed, result) {
				t.Fatalf("source-prefix observation changed native evidence: %+v %t %t %v", observed, complete, invoked, err)
			}
			t.Logf("HEAD==issued Base; source freeze has one commit; development and reopened UNKNOWN observation preserve exact identity/digest/changes without another commit; observation writes nothing")
		})
	}
}

func TestNativeCandidateFreezeMapsCopiedSourceAndRejectsForeignCommits(t *testing.T) {
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			f, state, native, runner := nativeCandidateWorkspace(t, name)
			resource := state.NativeResources["editing"]
			intent := nativeCandidateIntent(t, "AUTHORING", resource.Source, "app_test.go")
			result, complete, err := f.candidateEvidence(state, intent, true, new(bool))
			if err != nil || !complete || result.Candidate == nil || result.Candidate.Identity != resource.Identity {
				t.Fatalf("copied source authoring failed: %+v %t %v", result, complete, err)
			}
			if err := os.WriteFile(filepath.Join(resource.Path, "app_test.go"), []byte("package command\nimport \"testing\"\nfunc TestValid(t *testing.T) { if !Valid(\"yes\") { t.Fatal(\"valid input\") } }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			intent = nativeCandidateIntent(t, "QA_ARTIFACT", resource.Source, "app_test.go")
			result, complete, err = f.candidateEvidence(state, intent, true, new(bool))
			if err != nil || !complete || result.Candidate == nil {
				t.Fatalf("copied source artifact failed: %+v %t %v", result, complete, err)
			}
			changes, err := native.Changes(resource.Source, result.Candidate.Identity)
			if err != nil || !reflect.DeepEqual(changes, result.Candidate.Changes) {
				t.Fatalf("artifact native values differ: %+v %v", result.Candidate, err)
			}
			invoked := false
			observed, complete, err := f.candidateEvidence(state, intent, false, &invoked)
			if err != nil || !complete || invoked || !reflect.DeepEqual(observed, result) {
				t.Fatalf("artifact commit reobservation differs: %+v %t %v", observed, complete, err)
			}
			t.Run("review-ready", func(t *testing.T) {
				reviewer, reviewing, reviewNative, _ := nativeCandidateWorkspace(t, name)
				copy := reviewing.NativeResources["editing"]
				copy.Role = "candidate"
				reviewing.NativeResources[copy.ID] = copy
				ready := nativeCandidateIntent(t, "REVIEW_READY", copy.Source)
				result, complete, err := reviewer.candidateEvidence(reviewing, ready, true, new(bool))
				if err != nil || !complete || result.Candidate == nil || result.Candidate.Identity != copy.Identity || len(result.Candidate.Changes) != 0 {
					t.Fatalf("copied review source failed: %+v %t %v", result, complete, err)
				}
				invoked := false
				observed, complete, err := reviewer.candidateEvidence(reviewing, ready, false, &invoked)
				if err != nil || !complete || invoked || !reflect.DeepEqual(observed, result) {
					t.Fatalf("copied review source observation differs: %+v %t %v", observed, complete, err)
				}
				head, err := reviewNative.Resolve()
				if err != nil || head != copy.Identity {
					t.Fatalf("unchanged review freeze created a native commit: %s %v", head, err)
				}
			})
			base := result.Candidate.Identity
			for i, scenario := range []string{"foreign-message", "multiple-commits"} {
				intent = nativeCandidateIntent(t, "CHECK_SOURCE", base, "app.go")
				message := "foreign native commit"
				if i != 0 {
					message = "formal-gates candidate " + state.RunID + " " + intent.ActionID + " " + intent.PayloadDigest
				}
				if err := os.WriteFile(filepath.Join(resource.Path, "app.go"), []byte(fmt.Sprintf("package command\nfunc Valid(s string) bool { return len(s) > %d }\n", i+2)), 0644); err != nil {
					t.Fatal(err)
				}
				if err := native.Track([]string{"app.go"}); err != nil {
					t.Fatal(err)
				}
				if err := native.Commit(message); err != nil {
					t.Fatal(err)
				}
				before := runner.synchronizations
				invoked = false
				bad, complete, err := f.candidateEvidence(state, intent, false, &invoked)
				if err == nil || complete || bad.Candidate != nil || invoked || runner.synchronizations != before {
					t.Fatalf("%s accepted or mutated: %+v %t %t %v", scenario, bad, complete, invoked, err)
				}
				t.Logf("%s rejected with native message/range proof: %v", scenario, err)
			}
		})
	}
}
