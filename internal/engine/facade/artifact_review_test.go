package facade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// An existing external test requires an independent semantic judgment before
// its isolated artifact can affect the final candidate or production view.
func TestExistingTestArtifactRequiresBoundIndependentReview(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, judgment := range []string{"test", "production", "unknown"} {
		t.Run(judgment, func(t *testing.T) {
			root := t.TempDir()
			git := func(dir string, args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=External Project", "GIT_AUTHOR_EMAIL=external@example.invalid", "GIT_COMMITTER_NAME=External Project", "GIT_COMMITTER_EMAIL=external@example.invalid")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(dir, path, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			git(root, "init", "-q")
			git(root, "config", "user.name", "External Project")
			git(root, "config", "user.email", "external@example.invalid")
			write(root, ".gitignore", ".gates/\n__pycache__/\n")
			write(root, "requirements.md", validationDocument)
			write(root, "app.py", "def valid(value):\n    return bool(value)\n")
			originalTest := "import unittest\nfrom app import valid\nclass TestApp(unittest.TestCase):\n    def test_valid(self):\n        self.assertTrue(valid('ok'))\n"
			write(root, "test_app.py", originalTest)
			write(root, "runtime.json", "enabled")
			git(root, "add", "--all")
			git(root, "commit", "-qm", "External source")
			base := git(root, "rev-parse", "HEAD")
			plan := commandPlan()
			plan.Batches[0].Artifacts = []string{"app.py"}
			plan.Batches[0].Checks[0].Command = strconv.Quote(python) + " -m compileall -q ."
			plan.Batches[0].Checks[1].Command = strconv.Quote(python) + " -m unittest discover -v"
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, plan, "whitebox", "quality")
			dev, di := d.action("engine.development.batch.v4")
			if di.Workspace == nil {
				t.Fatal("development lacks signed workspace")
			}
			write(di.Workspace.Path, "app.py", "def valid(value):\n    return isinstance(value, str) and bool(value)\n")
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.py"}, Summary: "validate strings"}})
			d.drive()
			design, in := d.action("engine.qa.design.v3")
			if in.Workspace == nil || in.Workspace.Role != "whitebox-authoring" {
				t.Fatalf("wrong authoring input: %+v", in.Workspace)
			}
			m := in.Manifest
			for _, item := range []struct{ id, scenario string }{{"valid", "SUCCESS"}, {"invalid", "FAILURE"}} {
				m.Cases = append(m.Cases, coverage.AcceptanceCase{CaseID: item.id, Mode: coverage.CaseWhitebox, Scenario: item.scenario, TestRef: "test_app.py::TestApp.test_" + item.id, Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{python, "-m", "unittest", "test_app.TestApp.test_" + item.id}}}, Oracle: "actual public predicate matches " + item.id})
				m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: item.id})
			}
			reviewedTest := originalTest + "    def test_invalid(self):\n        self.assertFalse(valid(''))\n"
			write(in.Workspace.Path, "test_app.py", reviewedTest)
			paths := []string{"test_app.py"}
			if judgment != "test" {
				material := "runtime.json"
				body := "disabled"
				if judgment == "production" {
					material = "app.py"
					body = "def valid(value):\n    return False\n"
				}
				write(in.Workspace.Path, material, body)
				paths = append(paths, material)
				m.Cases[0].Files = []string{material}
			}
			d.business(design, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: paths}})
			d.drive()
			reviewID, ri := d.action("engine.qa.review.rationale.v2")
			if ri.ReviewArtifact == nil || ri.ReviewArtifact.Identity != ri.Binding.Candidate || ri.ReviewBase != d.run.Validation.DevelopmentSnapshot || ri.ReviewArtifact.TestOnly || ri.ReviewArtifact.ProductionViewEqual || ri.ReviewArtifact.GateView != nil {
				t.Fatalf("raw review material asserted approval or lost source: %+v", ri)
			}
			actual, err := d.f.vcs.Changes(ri.ReviewBase, ri.Binding.Candidate)
			if err != nil || !reflect.DeepEqual(actual, ri.ReviewArtifact.Changes) || len(actual) != len(paths) {
				t.Fatalf("raw exact diff lost: %+v %v", ri.ReviewArtifact, err)
			}
			if git(root, "rev-parse", "HEAD") != base {
				t.Fatal("raw artifact changed mainline")
			}
			body, err := os.ReadFile(filepath.Join(root, "test_app.py"))
			if err != nil || string(body) != originalTest {
				t.Fatal("raw artifact changed mainline test", err)
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			blocked := protocol.CandidateParameters{Stage: "REVIEW_READY", Base: ri.ReviewBase, ReviewBase: base, QAArtifacts: map[coverage.ReviewKind]protocol.CandidateArtifact{coverage.ReviewWhitebox: {Identity: ri.Binding.Candidate, Paths: paths}}}
			if reviewedArtifact(snap.State, blocked) == nil {
				t.Fatal("unreviewed artifact authorized final assembly")
			}
			owned, err := d.f.ownershipAt(snap.State, ri.Binding.Candidate)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				if owned[path] {
					t.Fatalf("raw artifact donated test ownership: %s", path)
				}
			}
			d.reopen()
			_, reopened := d.action("engine.qa.review.rationale.v2")
			if !reflect.DeepEqual(reopened.ReviewArtifact, ri.ReviewArtifact) || reopened.ReviewBase != ri.ReviewBase {
				t.Fatal("reopen changed original review source/diff")
			}
			r := approvedReview(ri.Manifest)
			for i := range r.CaseDecisions {
				r.CaseDecisions[i].Reason = "Read exact test source, production predicate and configuration: this associated unittest changes only the test oracle; public entry, setup, execution and assertion satisfy all six criteria."
			}
			status := "PASS"
			if judgment != "test" {
				status = "FAIL"
				r.SetStatus = coverage.StatusFail
				r.CaseDecisions[0].Status = coverage.StatusFail
				r.CaseDecisions[0].Reason = "Associated material is " + judgment + " ownership; the independent review cannot classify it as test-only."
				r.PointDecisions[0].Status = coverage.StatusFail
				r.SourceDecisions[0].Status = coverage.StatusFail
			}
			d.business(reviewID, protocol.ValidationResult{Binding: ri.Binding, Status: status, Review: &r})
			set := d.run.Validation.Sets[coverage.ReviewWhitebox]
			if len(set.ReviewHistory) != 1 || set.ReviewHistory[0].Candidate != ri.Binding.Candidate || !reflect.DeepEqual(set.ReviewHistory[0].Review, r) {
				t.Fatal("independent result did not bind exact artifact/history")
			}
			if judgment != "test" {
				if set.Status != "DRAFT" || d.run.Validation.Candidate != nil {
					t.Fatalf("failed classification authorized validation: %+v", set)
				}
				snap, err = d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if reviewedArtifact(snap.State, blocked) == nil {
					t.Fatal("failed classification authorized final assembly")
				}
				if git(root, "rev-parse", "HEAD") != base {
					t.Fatal("failed review changed mainline")
				}
				return
			}
			if set.Status != "FROZEN" {
				t.Fatalf("accepted semantic judgment not frozen: %+v", set)
			}
			d.drive()
			if d.run.Validation.Candidate == nil {
				t.Fatal("reviewed existing Python test did not produce final candidate")
			}
			var final protocol.CandidateEvidence
			var finalParams protocol.CandidateParameters
			var finalIntent protocol.HostActionIntent
			var finalReceipt protocol.HostActionReceipt
			snap, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			for id, receipt := range snap.State.HostActionReceipts {
				if receipt.AdapterOperation != "op.candidate" {
					continue
				}
				for _, effect := range snap.State.ReconciledEffects {
					if effect.Intent == nil {
						continue
					}
					intent := *effect.Intent
					if intent.ActionID != id || intent.Adapter == nil || intent.Adapter.Params["stage"] != "REVIEW_READY" {
						continue
					}
					if err := decodeReceiptEvidence(receipt, &final); err != nil {
						t.Fatal(err)
					}
					if err := decodeNativeParams(intent, &finalParams); err != nil {
						t.Fatal(err)
					}
					finalIntent = intent
					finalReceipt = receipt
				}
			}
			if final.Identity != d.run.Validation.Candidate.Identity || !final.TestOnly || !final.ProductionViewEqual || final.GateView == nil || !slices.Equal(final.GateView.TestOwnedPaths, []string{"test_app.py"}) {
				t.Fatalf("final exact ownership lost: %+v", final)
			}
			actual, err = d.f.vcs.Changes(base, final.Identity)
			if err != nil || !reflect.DeepEqual(final.Changes, actual) || len(actual) != 2 {
				t.Fatalf("whole production/test range lost: %+v %v", final, err)
			}
			if git(root, "rev-parse", "HEAD") != base {
				t.Fatal("final validation promoted mainline")
			}
			originalHistory := slices.Clone(snap.State.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory)
			for _, reopen := range []bool{false, true} {
				if reopen {
					d.reopen()
					snap, err = d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
				}
				observed, complete, err := d.f.candidateEvidence(snap.State, finalIntent, false, new(bool))
				if err != nil || !complete || observed.Candidate == nil || !reflect.DeepEqual(*observed.Candidate, final) {
					t.Fatalf("final receipt reobservation lost its reviewed artifact: complete=%v err=%v actual=%+v expected=%+v", complete, err, observed.Candidate, final)
				}
				set := snap.State.Validation.Sets[coverage.ReviewWhitebox]
				if set.Candidate != ri.Binding.Candidate || !reflect.DeepEqual(set.ReviewHistory, originalHistory) || snap.State.NativeActionCurrent(finalIntent, d.f.definition) {
					t.Fatal("final receipt changed or invalidated its original artifact/review source")
				}
				_, execution := d.action("engine.qa.execution.v2")
				if execution.Binding.Candidate != final.Identity || execution.Execution == nil || execution.Execution.Candidate.Identity != final.Identity {
					t.Fatal("execution responsibility lost the combined final candidate")
				}
				replay, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), finalIntent.ActionID, "op.candidate", d.host(), finalIntent.Correlation, finalIntent.PayloadDigest, finalReceipt.Status, "", finalReceipt.AdapterEvidence.Values)
				if err != nil {
					t.Fatal(err)
				}
				if acceptance := d.submit(replay); acceptance.Status != "DUPLICATE" {
					t.Fatalf("original final receipt repeated its business admission: %+v", acceptance)
				}
			}
			// A changed immutable test entry cannot reuse the accepted judgment.
			changed := filepath.Join(t.TempDir(), "changed")
			git(root, "worktree", "add", "--detach", changed, final.Identity)
			write(changed, "test_app.py", reviewedTest+"# altered oracle\n")
			git(changed, "add", "--all")
			git(changed, "commit", "-qm", "Changed test input")
			before, after := map[string]bool{}, map[string]bool{}
			if err := d.f.reviewedArtifactOwnership(snap.State, finalParams, git(changed, "rev-parse", "HEAD"), before, after); err == nil || !strings.Contains(err.Error(), "entry changed") {
				t.Fatalf("changed source reused review: %v", err)
			}
			mismatched := finalParams
			mismatched.QAArtifacts = map[coverage.ReviewKind]protocol.CandidateArtifact{coverage.ReviewWhitebox: {Identity: git(changed, "rev-parse", "HEAD"), Paths: paths}}
			if reviewedArtifact(snap.State, mismatched) == nil {
				t.Fatal("changed artifact identity reused accepted review")
			}
			git(changed, "reset", "--hard", final.Identity)
			write(changed, "app.py", "def valid(value):\n    return False\n")
			git(changed, "add", "--all")
			git(changed, "commit", "-qm", "Changed production input")
			if err := d.f.reviewedArtifactOwnership(snap.State, finalParams, git(changed, "rev-parse", "HEAD"), before, after); err == nil || !strings.Contains(err.Error(), "outside its reviewed artifacts") {
				t.Fatalf("extra production change asserted equality: %v", err)
			}
			currentSet := snap.State.Validation.Sets[coverage.ReviewWhitebox]
			currentSet.Manifest.Cases[0].Files = []string{"runtime.json"}
			if reviewedArtifact(snap.State, finalParams) == nil {
				t.Fatal("changed material relation reused accepted review")
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Category: "CASE_EXECUTABILITY", Mode: coverage.ReviewWhitebox, AffectedCases: []string{"valid"}}, "")
			d.reopen()
			snap, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			set = snap.State.Validation.Sets[coverage.ReviewWhitebox]
			if set.Status != "THAWED" || set.Review != nil || set.Candidate != "" || !reflect.DeepEqual(set.ReviewHistory, originalHistory) || snap.State.Validation.Candidate != nil {
				t.Fatal("thaw did not revoke current approval while retaining original reviewed source")
			}
			if observed, complete, err := d.f.candidateEvidence(snap.State, finalIntent, false, new(bool)); err == nil || complete || observed.Candidate != nil {
				t.Fatalf("thawed artifact retained final ownership authority: complete=%v err=%v actual=%+v", complete, err, observed.Candidate)
			}
			t.Logf("existing Python artifact %s independently reviewed; final %s retained %d native changes and excluded only test_app.py", ri.Binding.Candidate, final.Identity, len(actual))
		})
	}
}
