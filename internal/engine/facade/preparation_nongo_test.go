package facade

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"strings"
)

func TestWhiteboxWorkspaceReplaysOnlyItsBoundOperation(t *testing.T) {
	f := facadePreparation(t, nil)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	p := run.Regular.Preparation
	p.QA["whitebox"] = &protocol.PreparedQA{}
	first := protocol.GitPreparation{Kind: "whitebox-workspace", Before: p.DevelopmentSnapshot, ID: "git:sha256:first"}
	a, err := f.performPreparationGit(run, first)
	if err != nil {
		t.Fatal(err)
	}
	workspace := a.Resources["whitebox"]
	writePreparationFile(t, workspace, "probe", "retained test work")
	replayed, err := f.performPreparationGit(run, first)
	if err != nil || replayed.Resources["whitebox"] != workspace {
		t.Fatalf("same operation did not reuse workspace: %+v %v", replayed, err)
	}
	// The previous whitebox author committed its tests. A later preparation
	// still has the same plan/rework number but targets new development input.
	old, err := gitprep.Commit(workspace, first.Before, "test-artifacts", []string{"probe"})
	if err != nil {
		t.Fatal(err)
	}
	dev := p.Resources["development"]
	writePreparationFile(t, dev, "implementation", "new development input")
	updated, err := gitprep.Commit(dev, first.Before, "updated-development", []string{"implementation"})
	if err != nil {
		t.Fatal(err)
	}
	next := protocol.GitPreparation{Kind: "whitebox-workspace", Before: updated.After, ID: "git:sha256:second"}
	b, err := f.performPreparationGit(run, next)
	if err != nil {
		t.Fatal(err)
	}
	if b.Resources["whitebox"] == workspace {
		t.Fatal("new operation reused committed old whitebox workspace")
	}
	if head, err := gitprep.Head(workspace); err != nil || head != old.After {
		t.Fatal("old whitebox evidence changed", err)
	}
	if head, err := gitprep.Head(b.Resources["whitebox"]); err != nil || head != updated.After {
		t.Fatal("new whitebox does not match development", err)
	}
	if _, err := os.Stat(filepath.Join(b.Resources["whitebox"], "probe")); !os.IsNotExist(err) {
		t.Fatal("new workspace inherited unapproved old test state", err)
	}
	// Model interruption after creation but before receipt persistence. Normal
	// terminal cleanup must also find this operation-owned workspace.
	p.Resources["whitebox"] = workspace
	p.Operation = &next
	if err := f.cleanupPreparationResources(run); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{workspace, b.Resources["whitebox"]} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("terminal cleanup left a whitebox workspace", path, err)
		}
	}
}

// Exercise the shared Git preparation entry used by Drive, in a real external
// repository. Role results are fixtures; the shell command and Git work are real.
func TestNonGoPreparationReuseAndUnclassifiedChanges(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := gitprep.Git(root, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "fixture")
	writePreparationFile(t, root, "entry", "printf old")
	// Deliberately use opaque names: the engine must not infer test ownership.
	writePreparationFile(t, root, "probe", "sh entry")
	git("add", ".")
	git("commit", "-qm", "baseline")
	base := git("rev-parse", "HEAD")
	f := &Facade{root: root, stateDir: t.TempDir()}
	p := &protocol.Preparation{BindingDigest: "non-go", DevelopmentSnapshot: base, Resources: map[string]string{}, QA: map[string]*protocol.PreparedQA{"blackbox": {}, "whitebox": {}}}
	run := Run{RunID: "external-non-go", Regular: &protocol.RegularState{BaseSnapshot: base, Candidate: base, Preparation: p, Requirement: protocol.RequirementDocument{Path: "requirement.md", Text: "sh entry: stdout hello, stderr empty, exit 0"}, Route: &protocol.RegularRoute{QA: []string{"blackbox", "whitebox"}}}}
	perform := func(op protocol.GitPreparation) protocol.GitPreparationReceipt {
		t.Helper()
		receipt, err := f.performPreparationGit(run, op)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range receipt.Resources {
			p.Resources[k] = v
		}
		return receipt
	}
	perform(protocol.GitPreparation{Kind: "workspaces", Before: base, ID: "workspaces"})
	dev := p.Resources["development"]
	writePreparationFile(t, dev, "entry", "printf hello")
	receipt := perform(protocol.GitPreparation{Kind: "batch-commit", Before: base, ID: "development", Paths: []string{"entry"}})
	p.DevelopmentSnapshot = receipt.Git.After
	perform(protocol.GitPreparation{Kind: "whitebox-workspace", Before: p.DevelopmentSnapshot, ID: "whitebox"})
	wb := p.Resources["whitebox"]
	baseline, err := os.ReadFile(filepath.Join(p.Resources["blackbox"], "entry"))
	if err != nil || string(baseline) != "printf old" {
		t.Fatalf("blackbox not baseline: %q %v", baseline, err)
	}
	cmd := exec.Command("sh", "probe")
	cmd.Dir = wb
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.String() != "hello" || stderr.Len() != 0 {
		t.Fatalf("shell oracle: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
	for _, invalid := range []string{"missing", "."} {
		p.QA["whitebox"].Design = &protocol.QADesign{TestFiles: []string{invalid}}
		_, err := f.performPreparationGit(run, protocol.GitPreparation{Kind: "whitebox-commit", Before: p.DevelopmentSnapshot, ID: "invalid-file"})
		if err == nil || !strings.Contains(err.Error(), "not a project file") {
			t.Fatalf("non-file declaration %q accepted: %v", invalid, err)
		}
	}
	p.QA["whitebox"].Design = &protocol.QADesign{TestFiles: []string{}}
	receipt = perform(protocol.GitPreparation{Kind: "whitebox-commit", Before: p.DevelopmentSnapshot, ID: "reuse"})
	if receipt.Ownership == nil || receipt.Ownership.Status != "TEST_ONLY" || len(receipt.Ownership.Paths) != 0 {
		t.Fatalf("unchanged tree rejected: %+v", receipt.Ownership)
	}
	p.QA["whitebox"].Snapshot = receipt.Git.After
	p.QA["whitebox"].Design = &protocol.QADesign{TestFiles: []string{}}
	receipt = perform(protocol.GitPreparation{Kind: "finalize", Before: receipt.Git.After, ID: "finalize"})
	if receipt.Git.After == "" {
		t.Fatal("no candidate")
	}
	if len(receipt.TestOwned) != 0 {
		t.Fatal("guessed ownership")
	}
	if _, err := os.Stat(filepath.Join(p.Resources["candidate"], "go.mod")); !os.IsNotExist(err) {
		t.Fatal("unexpected Go configuration")
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		if _, err := gitprep.Git(p.Resources["candidate"], "cat-file", "-e", receipt.Git.After+":"+protocol.PreparationArtifact(run.RunID, mode)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("external shell reuse: stdout=%q stderr=%q exit=0; baseline isolated; whitebox unchanged TEST_ONLY; candidate materialized without go.mod", stdout.String(), stderr.String())
	// An accidental production edit may be snapshotted for inspection, but
	// cannot enter a final candidate without an independent review.
	writePreparationFile(t, wb, "entry", "printf altered")
	receipt = perform(protocol.GitPreparation{Kind: "whitebox-commit", Before: p.DevelopmentSnapshot, ID: "accidental", Paths: []string{"entry"}})
	if receipt.Ownership.Status != "REVIEW_PENDING" {
		t.Fatal(receipt.Ownership)
	}
	q := p.QA["whitebox"]
	q.Snapshot = receipt.Git.After
	for _, disposition := range []string{"pending", "failed", "waived"} {
		q.Frozen = disposition == "waived"
		if disposition == "waived" {
			q.ReviewWaiver = &protocol.QAReviewWaiver{}
		}
		_, err := f.performPreparationGit(run, protocol.GitPreparation{Kind: "finalize", Before: q.Snapshot, ID: disposition})
		if err == nil || !strings.Contains(err.Error(), "WHITEBOX_REVIEW_REQUIRED") {
			t.Fatalf("%s promoted: %v", disposition, err)
		}
		q.ReviewWaiver = nil
	}
	// Correct the accidental edit and add a real test. The full review diff
	// must include all whitebox revisions, not only the last manifest.
	writePreparationFile(t, wb, "entry", "printf hello")
	writePreparationFile(t, wb, "assertion", "import subprocess\nr = subprocess.run(['sh', 'entry'], capture_output=True)\nassert (r.returncode, r.stdout, r.stderr) == (0, b'hello', b'')\n")
	cmd = exec.Command("python3", "assertion")
	cmd.Dir = wb
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual new test: %s %v", out, err)
	}
	receipt = perform(protocol.GitPreparation{Kind: "whitebox-commit", Before: q.Snapshot, ID: "corrected", Paths: []string{"entry", "assertion"}})
	q.Snapshot = receipt.Git.After
	q.Design = &protocol.QADesign{TestFiles: []string{"assertion"}}
	q.Review = &coverage.ReviewResult{}
	q.Whitelist = &coverage.ApprovedWhitelist{}
	q.Frozen = true // explicit independent review fixture; not a real reviewer PASS
	claim := protocol.ExecutionClaim{Candidate: q.Snapshot}
	claim.Task.Step = "whitebox-review"
	payload, _, err := preparationInput(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	args := payload["testChanges"].(map[string]any)["diffArgs"].([]string)
	diff, err := gitprep.Git(wb, args...)
	if err != nil || !strings.Contains(diff, "assertion") || strings.Contains(diff, "printf altered") {
		t.Fatalf("wrong complete diff: %s %v", diff, err)
	}
	receipt = perform(protocol.GitPreparation{Kind: "finalize", Before: q.Snapshot, ID: "reviewed"})
	if len(receipt.TestOwned) != 1 || receipt.TestOwned[0] != "assertion" {
		t.Fatalf("not reviewed delta: %v", receipt.TestOwned)
	}
	t.Log("non-Go new test executed; complete corrected diff reviewed by fixture; pending/failed/waived review blocked final candidate; only reviewed assertion delta promoted")
}

type nonGoCoverageTransport struct {
	savedCoverageTransport
	t         *testing.T
	f         *Facade
	workspace string
	turns     int
}

func (a *nonGoCoverageTransport) turn(turn nativeTurn) (string, error) {
	a.t.Helper()
	a.turns++
	if turn.Workspace != a.workspace {
		a.t.Fatalf("turn %d lost ordinary workspace: %s", a.turns, turn.Workspace)
	}
	run, err := a.f.Show()
	if err != nil {
		return "", err
	}
	ready := protocol.WhiteboxCoverageReady(run.Executions[turn.Claim.ActionID])
	if a.turns == 1 {
		if ready || !turn.NoTools {
			a.t.Fatal("coverage did not precede test authoring")
		}
		if _, err := os.ReadFile(filepath.Join(turn.Workspace, "entry")); err != nil {
			return "", err
		}
		answer := []byte(`{"coverageJudgment":"independent command contract","behaviorOracles":[{"behavior":"command output","oracle":"stdout hello, stderr empty, exit 0"}]}`)
		if err := os.WriteFile(filepath.Join(turn.Job, "answer.json"), answer, 0600); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(turn.Job, "stdout.jsonl"), []byte("fixture coverage evidence"), 0600); err != nil {
			return "", err
		}
	} else {
		if !ready || turn.NoTools {
			a.t.Fatal("test authoring started without recorded coverage")
		}
		if _, err := os.ReadFile(filepath.Join(turn.Workspace, "probe")); err != nil {
			return "", err
		}
	}
	return "native-non-go", nil
}

func TestNonGoNativeCoveragePrecedesTestInspection(t *testing.T) {
	document := strings.ReplaceAll(b1Document, "命令输出字符串 hello。", "在项目根目录执行 sh entry，stdout 恰好为 hello，stderr 为空，退出码为 0。")
	document = strings.ReplaceAll(document, "运行后输出恰好是 hello。", "执行 sh entry 后 stdout 恰好为 hello，stderr 为空，退出码为 0。")
	f := facadePreparationTask(t, []string{"whitebox"}, 1, "Implement the shell entry and probe with no Go module", document)
	run, _ := f.Show()
	dev := run.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "entry", "printf hello")
	writePreparationFile(t, dev, "probe", "sh entry")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"entry", "probe"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "prepare external shell fixture", Observation: "entry and probe written", Result: "PASS"}}, Handoff: "fixture"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "whitebox-design")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-non-go", Status: "RUNNING"}})
	run, _ = f.Show()
	claim := run.Executions[x.Claim.ActionID].Claim
	claim.Identity = "native-non-go"
	job := t.TempDir()
	workspace := run.Regular.Preparation.Resources["whitebox"]
	coverage, err := prepareNativeCoverage(run, claim, workspace, nil, job)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &nonGoCoverageTransport{t: t, f: f, workspace: workspace}
	plan := nativeActionInput{coverage: coverage, material: nativePromptMaterial{workspace: workspace}}
	if _, err := f.runNativeBridgeTurns(run, claim, true, job, "fixture test inspection", plan, adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.turns != 2 {
		t.Fatalf("turns=%d", adapter.turns)
	}
	t.Log("shared native bridge: non-Go candidate read in coverage turn; test inspection turn observes persisted independent judgment; transport is a fixture")
}
