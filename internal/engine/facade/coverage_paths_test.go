package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoveragePathsScopeAndBinding(t *testing.T) {
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
	writePreparationFile(t, root, "checks", "SECRET_TEST")
	writePreparationFile(t, root, "unrelated", strings.Repeat("UNRELATED_LARGE", 100000))
	git("add", ".")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	writePreparationFile(t, root, "entry", "printf hello")
	git("add", ".")
	git("commit", "-qm", "candidate")
	candidate := git("rev-parse", "HEAD")
	run := Run{Regular: &protocol.RegularState{BaseSnapshot: base}}
	instructions, err := loadActionInstructions(copyPromptPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	run.Regular.ActionInstructions = instructions
	run.Regular.Preparation = &protocol.Preparation{QA: map[string]*protocol.PreparedQA{"whitebox": {DesignGap: "Use the amber fixture; keep its original output."}}}
	claim := protocol.ExecutionClaim{Candidate: candidate}
	claim.Task.Step = "whitebox-design"
	job := t.TempDir()
	plan, err := prepareNativeCoverage(run, claim, root, nil, job, root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.workspace != root {
		t.Fatal("candidate workspace lost")
	}
	var input struct {
		SourcePaths []coverageSourcePath `json:"sourcePaths"`
		DesignGap   string               `json:"designGap"`
	}
	start := strings.LastIndex(plan.prompt, "\n")
	if err := json.Unmarshal([]byte(plan.prompt[start+1:]), &input); err != nil {
		t.Fatal(err)
	}
	if input.DesignGap != run.Regular.Preparation.QA["whitebox"].DesignGap {
		t.Fatal("current instructions missing from coverage")
	}
	if len(input.SourcePaths) != 1 || input.SourcePaths[0].Path != root {
		t.Fatalf("not ordinary workspace: %+v", input)
	}
	if err := plan.writeFiles(filepath.Join(job, "coverage"), claim); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(job, "coverage", "production")); !os.IsNotExist(err) {
		t.Fatal("source projection created")
	}
	if strings.Contains(plan.prompt, "SECRET_TEST") || strings.Contains(plan.prompt, "UNRELATED_LARGE") || strings.Contains(plan.prompt, "printf hello") {
		t.Fatal("project bodies inlined")
	}
	if _, err := os.ReadFile(filepath.Join(input.SourcePaths[0].Path, "entry")); err != nil {
		t.Fatal(err)
	}
	writePreparationFile(t, root, "entry", "printf changed")
	if _, err := coverageSourcePaths(run, claim, root); err == nil || !strings.Contains(err.Error(), "COVERAGE_SOURCE_BINDING") {
		t.Fatalf("dirty binding accepted: %v", err)
	}
	git("checkout", "--", "entry")
	claim.Candidate = base
	if _, err := coverageSourcePaths(run, claim, root); err == nil {
		t.Fatal("wrong HEAD accepted")
	}
	// The normal incremental path restores bound, previously reviewed tests
	// before the independent judgment; that is not arbitrary workspace drift.
	claim.Candidate = candidate
	writePreparationFile(t, root, "checks", "retained assertion")
	git("add", "checks")
	git("commit", "-qm", "reviewed tests")
	retained := git("rev-parse", "HEAD")
	git("checkout", "--detach", candidate)
	git("restore", "--source="+retained, "--staged", "--worktree", "--", "checks")
	run.Regular.Preparation = &protocol.Preparation{QA: map[string]*protocol.PreparedQA{"whitebox": {TestArtifactsFrom: retained, Manifest: []string{"checks"}}}}
	if _, err := coverageSourcePaths(run, claim, root); err != nil {
		t.Fatalf("bound retained tests rejected: %v", err)
	}
	writePreparationFile(t, root, "checks", "unbound test edit")
	if _, err := coverageSourcePaths(run, claim, root); err == nil {
		t.Fatal("changed retained artifact accepted")
	}
	t.Log("non-Go candidate: ordinary workspace, no module required, no source copies or inlined test bodies; dirty/wrong HEAD rejected")
}

func TestCoverageInputSizeBeforeClaimAndSend(t *testing.T) {
	f := newRegularFacade(t)
	facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	x := facadeExecution(t, f, "product-review")
	removeFixtureInstructions(t, f, func(r *protocol.RegularState) {
		r.ActionInstructions["actions/product-review.md"] = strings.Repeat("x", codexInputCharacters+1)
	})
	before, _ := f.engine.Load()
	spy := &inputTurnSpy{}
	for i := 0; i < 3; i++ {
		_, err := f.executeNativeTransport(x.Claim.ActionID, false, spy)
		if err == nil || !strings.Contains(err.Error(), "INPUT_TOO_LARGE") {
			t.Fatalf("oversize: %v", err)
		}
	}
	after, _ := f.engine.Load()
	if before.Revision != after.Revision || spy.turns != 0 || spy.probes != 0 {
		t.Fatal("oversize invoked adapter")
	}
	if _, err := (codexRuntimeAdapter{}).turn(nativeTurn{Prompt: strings.Repeat("x", codexInputCharacters+1)}); err == nil || !strings.Contains(err.Error(), "INPUT_TOO_LARGE") {
		t.Fatalf("adapter send check: %v", err)
	}
}
