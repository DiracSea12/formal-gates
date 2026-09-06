package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func boundedRepairQA(t *testing.T, id, mode, requirement string) (string, string, validate.RunState) {
	t.Helper()
	root, pkg := cliWorkflowFixture(t)
	if requirement != "" {
		mustWriteCLI(t, filepath.Join(root, "requirements.md"), requirement)
		cliGit(t, root, "add", "requirements.md")
		cliGit(t, root, "commit", "-m", "bounded repair requirement fixture")
	}
	s := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	s = cliRecordAction(t, root, pkg, s, "requirements-clarification", "PASS")
	s = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", id, "--confirmed"))
	s = cliRecordAction(t, root, pkg, s, "product-review", "PASS")
	s = cliRecordAction(t, root, pkg, s, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", id, "--decision", "no-split", "--note", "single bounded fixture")
	s = decodeSemanticState(t, runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", id, "--mode", "custom", "--gate", mode, "--gate", "quality"))
	if mode == "blackbox" {
		tree := filepath.Join(t.TempDir(), "qa")
		cliGit(t, root, "worktree", "add", "--detach", tree, s.BaseSnapshot)
		runCLI(t, "workflow", "qa-worktree", "--root", root, "--package-root", pkg, "--run-id", id, "--worktree", tree)
	} else {
		d := cliPrepareAction(t, root, pkg, id, "development-worker")
		mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
		cliGit(t, root, "add", "delivery.txt")
		cliGit(t, root, "commit", "-m", "S1 development")
		s = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", id, "--dispatch", d))
	}
	return root, pkg, s
}

func boundedStateBytes(t *testing.T, root, id string) string {
	t.Helper()
	data, err := os.ReadFile(validate.RunStatePath(root, id))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBoundedRepairCoverageFailureDoesNotInventFinding(t *testing.T) {
	for _, decision := range []string{"FAIL", "PENDING"} {
		t.Run(decision, func(t *testing.T) {
			requirement := cliStructuredRequirement("First independent operation.") + "\n### REQ-002：Second operation\n\n#### 要求\n\nSecond independent operation.\n\n#### 验收条件\n\n- AC-002：Second operation works.\n\n#### 来源\n\nIsolated regression.\n"
			root, pkg, s := boundedRepairQA(t, "coverage-no-invention", "blackbox", requirement)
			d := repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-design", "blackbox")
			s = decodeSemanticState(t, runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--case", "first operation", "--mode", "blackbox", "--procedure", "execute first", "--oracle", "first result", "--ac", "AC-001", "--case", "second operation", "--mode", "blackbox", "--procedure", "execute second", "--oracle", "second result", "--ac", "AC-002"))
			d = repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-review", "blackbox")
			args := []string{"workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--source-decision", "REQ-001=PASS", "--source-decision", "REQ-002=" + decision, "--point-decision", "AC-001=PASS", "--point-decision", "AC-002=" + decision}
			for _, c := range s.QACasesByMode["blackbox"] {
				args = append(args, "--case", c.ID, "--outcome", "PASS", "--case-decision", c.ID+"=PASS")
			}
			s = decodeSemanticState(t, runCLI(t, args...))
			review := s.QAReviewByMode["blackbox"]
			coverage := s.RequirementGuarantee.ReviewsByMode["blackbox"]
			if review.Status != "FAIL" || len(review.Findings) != 0 || coverage.Whitelist != nil || coverage.Review.SetStatus != "FAIL" || s.QACaseSets["blackbox"].Status == "FROZEN" || s.QADesignByMode["blackbox"].Status != "PENDING" {
				t.Fatalf("coverage failure fabricated evidence or approved the set: review=%+v coverage=%+v", review, coverage)
			}
			for _, item := range coverage.Review.SourceDecisions {
				want := "PASS"
				if item.ID == "REQ-002" {
					want = decision
				}
				if string(item.Status) != want {
					t.Fatalf("source decision changed: %+v", item)
				}
			}
			before := boundedStateBytes(t, root, s.RunID)
			if message := failingCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "qa-review", "--mode", "blackbox"); !strings.Contains(message, "QA Design") || boundedStateBytes(t, root, s.RunID) != before {
				t.Fatalf("coverage failure lost its design repair boundary: %s", message)
			}
		})
	}
}

func TestBoundedRepairWhiteboxRequiresCommittedRegisteredSnapshot(t *testing.T) {
	root, pkg, s := boundedRepairQA(t, "whitebox-snapshot-boundary", "whitebox", "")
	s1 := s.CurrentSnapshot
	d := repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-design", "whitebox")
	testPath := filepath.Join(root, "new_test.go")
	const testCode = "package fixture\n\nfunc TestNew() {}\n"
	mustWriteCLI(t, testPath, testCode)
	s = decodeSemanticState(t, runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--case", "direct structure", "--mode", "whitebox", "--procedure", "run TestNew", "--oracle", "expected structure", "--test", "new_test.go::TestNew", "--ac", "AC-001"))
	prepare := []string{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "qa-review", "--mode", "whitebox"}
	before := boundedStateBytes(t, root, s.RunID)
	if message := failingCLI(t, prepare...); !strings.Contains(message, "commit the tests") || !strings.Contains(message, "workflow snapshot") || before != boundedStateBytes(t, root, s.RunID) {
		t.Fatalf("uncommitted test code did not block prepare without writes: %s", message)
	}
	cliGit(t, root, "add", "new_test.go")
	cliGit(t, root, "commit", "-m", "S2 whitebox tests")
	if message := failingCLI(t, prepare...); !strings.Contains(message, "registered with workflow snapshot") || before != boundedStateBytes(t, root, s.RunID) {
		t.Fatalf("unregistered S2 did not block prepare without writes: %s", message)
	}
	s = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", s.RunID))
	if s.CurrentSnapshot == s1 {
		t.Fatal("test commit did not advance the registered snapshot")
	}
	d = repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-review", "whitebox")
	cid := s.QACasesByMode["whitebox"][0].ID
	record := []string{"workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--case", cid, "--outcome", "PASS", "--source-decision", "REQ-001=PASS", "--point-decision", "AC-001=PASS", "--case-decision", cid + "=PASS"}
	mustWriteCLI(t, testPath, testCode+"// changed after prepare\n")
	before = boundedStateBytes(t, root, s.RunID)
	if message := failingCLI(t, record...); !strings.Contains(message, "committed test code") || before != boundedStateBytes(t, root, s.RunID) {
		t.Fatalf("test change after prepare was frozen: %s", message)
	}
	mustWriteCLI(t, testPath, testCode)
	s = decodeSemanticState(t, runCLI(t, record...))
	if s.QAReviewByMode["whitebox"].Status != "PASS" || s.QACaseSets["whitebox"].Status != "FROZEN" || s.QACaseSets["whitebox"].BoundSnapshot != s.CurrentSnapshot {
		t.Fatal("committed and registered tests failed to freeze at S2")
	}
	mustWriteCLI(t, testPath, testCode+"// changed after review\n")
	before = boundedStateBytes(t, root, s.RunID)
	wave := []string{"workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", s.RunID}
	if message := failingCLI(t, wave...); !strings.Contains(message, "committed test code") || before != boundedStateBytes(t, root, s.RunID) {
		t.Fatalf("test change after review opened a wave: %s", message)
	}
	mustWriteCLI(t, testPath, testCode)
	runCLI(t, wave...)
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	for _, target := range s.ReviewWaves[s.ActiveReviewWaveID].Expected {
		if s.Dispatches[target.DispatchID].SourceSnapshot != s.CurrentSnapshot {
			t.Fatalf("wave target was not bound to S2: %+v", target)
		}
	}
}

func TestBoundedRepairCASE029CompletedReviewAfterCleanup(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "case029-completed-review", true)
	if s.QAWorktree != "" || s.QAReviewByMode["blackbox"].Status != "PASS" || s.QACaseSets["blackbox"].Status != "FROZEN" {
		t.Fatal("fixture did not complete review and normal isolation cleanup")
	}
	before := boundedStateBytes(t, root, s.RunID)
	for range 2 {
		message := failingCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "qa-review", "--mode", "blackbox")
		if !strings.Contains(message, "authoritative PASS") || !strings.Contains(message, "review is complete") || strings.Contains(message, "qa-worktree") || before != boundedStateBytes(t, root, s.RunID) {
			t.Fatalf("completed review retry returned wrong guidance or consumed a dispatch/attempt: %s", message)
		}
	}
}
