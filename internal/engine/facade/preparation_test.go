package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func TestBothQADesignSchemasRequireCaseDependencies(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox", "whitebox"})
	x := facadeExecution(t, f, "blackbox-design")
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		claim := x.Claim
		claim.Task.Step = authoring.StepID(mode + "-design")
		r.Regular.Preparation.Resources[mode] = t.TempDir()
		_, _, err := preparationPrompt(r, claim)
		if err != nil {
			t.Fatal(err)
		}
		definition := preparationSchema(mode+"-design", r, claim)
		schema, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		design := definition["properties"].(map[string]any)["design"].(map[string]any)
		properties := design["properties"].(map[string]any)
		manifest := properties["manifest"].(map[string]any)["properties"].(map[string]any)
		caseDefinition := manifest["cases"].(map[string]any)["items"].(map[string]any)
		dependencies := caseDefinition["properties"].(map[string]any)["dependencies"].(map[string]any)
		if dependencies["type"] != "array" || dependencies["items"].(map[string]any)["type"] != "string" || !strings.Contains(strings.Join(caseDefinition["required"].([]string), ","), "dependencies") {
			t.Fatalf("%s case dependencies are not a required string array: %#v", mode, dependencies)
		}
		if mode == "whitebox" {
			if bytes.Contains(schema, []byte("uniqueItems")) {
				t.Fatal("native result schema contains unsupported uniqueItems")
			}
			if !strings.Contains(strings.Join(design["required"].([]string), ","), "testFiles") || properties["testFiles"].(map[string]any)["type"] != "array" || properties["testFiles"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatal("actual whitebox result schema does not require explicit testFiles")
			}
		}
	}
}

func preparationSubmit(t *testing.T, f *Facade, v protocol.RegularEvent) Run {
	t.Helper()
	a, r, err := f.submitAdapterRegular(fmt.Sprintf("fixture-%s", v.Operation), v)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status == "REJECTED_RESULT" {
		t.Fatalf("rejected %+v: %+v", v, r.Regular.PreparationErrors)
	}
	return r
}
func facadePreparation(t *testing.T, modes []string) *Facade {
	t.Helper()
	return facadePreparationBatches(t, modes, 1)
}

func facadePreparationBatches(t *testing.T, modes []string, batches int) *Facade {
	return facadePreparationTask(t, modes, batches, "")
}
func facadePreparationTask(t *testing.T, modes []string, batches int, task string, documents ...string) *Facade {
	return facadePreparationPlan(t, modes, batches, task, nil, documents...)
}

func facadePreparationPlan(t *testing.T, modes []string, batches int, task string, edit func(*protocol.DevelopmentPlan), documents ...string) *Facade {
	t.Helper()
	document := strings.ReplaceAll(b1Document, "命令输出字符串 hello。", "在 Go module 根目录执行公开命令 go run .，退出码为 0，stdout 恰好为五个 UTF-8 字节 hello，无换行，stderr 为空。")
	document = strings.ReplaceAll(document, "运行后输出恰好是 hello。", "执行 go run . 后退出码为 0，stdout 恰好为 hello（无换行），stderr 为空。")
	if len(documents) > 0 {
		document = documents[0]
	}
	f := newRegularFacadeDocument(t, document)
	facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	for _, step := range []string{"product-review", "start-readiness"} {
		r, _ := f.Show()
		var x protocol.RegularExecution
		for _, execution := range r.Executions {
			if string(execution.Claim.Task.Step) == step && !execution.Finished {
				x = execution
			}
		}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
		source := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "fixture-" + step, Status: "RUNNING"}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
		raw := protocol.RawReviewCandidate{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: source.Identity, Action: step, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Complete: true, Message: "Every stated normal path and acceptance entry inspected", Findings: []protocol.ReviewFinding{}}
		if step == "start-readiness" {
			raw.GranularityReview = &protocol.DevelopmentPlan{RequirementRevision: x.Claim.RequirementRevision, Rationale: "one output capability", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "Implement a Go hello command", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "Create go.mod and main.go printing exactly hello; validate by running the Go command"}}, Interfaces: "stdout hello", StateBoundary: "command output", Risks: "wrong output", DefinitionOfDone: "go run . prints hello", Validation: "go run . and go test ./...", Recovery: "revert batch commit", Handoff: "only unresolved decisions", ContextCost: "single executable"}}}
			if batches == 2 {
				raw.GranularityReview.Batches = append(raw.GranularityReview.Batches, protocol.DevelopmentBatch{ID: "B2", Task: "Add a regression test for the committed hello command", Subtasks: []protocol.DevelopmentSubtask{{ID: "S2", Task: "Add main_test.go with a subprocess test of go run . asserting exit 0, stdout exactly hello with no newline, and empty stderr. Run go test ./...."}}, Dependencies: []string{"B1"}, Interfaces: "committed command from B1", StateBoundary: "test only", Risks: "assertion may omit stream or newline", DefinitionOfDone: "subprocess regression passes", Validation: "go test ./...", Recovery: "revert the test commit", Handoff: "only unresolved decisions", ContextCost: "independently rerunnable regression"})
			}
			if edit != nil {
				edit(raw.GranularityReview)
			}
			if task != "" {
				raw.GranularityReview.Batches[0].Task = task
			}
		}
		r = preparationSubmit(t, f, protocol.RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
		source.Status = "UNAVAILABLE"
		source.Reason = "Fixture has no provider transcript"
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
		ad := protocol.ReviewAdjudication{Action: step, ResultDigest: r.Regular.Reviews[step].CurrentDigest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "Every requirement and normal acceptance path inspected"}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &ad})
	}
	r, _ := f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "single repository"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "custom", QA: modes, Gates: []string{"implementation-quality-gate"}}})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	return f
}
func facadeExecution(t *testing.T, f *Facade, step string) protocol.RegularExecution {
	t.Helper()
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Executions {
		if string(x.Claim.Task.Step) == step && !x.Finished {
			return x
		}
	}
	t.Fatalf("missing %s; next=%+v", step, r.Next)
	return protocol.RegularExecution{}
}
func facadeResult(t *testing.T, f *Facade, x protocol.RegularExecution, result protocol.PreparationResult) Run {
	t.Helper()
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	src := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "native-" + x.Claim.ActionID, Status: "RUNNING"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
	if x.Claim.Task.Step == "whitebox-design" {
		facadeCoverage(t, f, x.Claim.ActionID)
	}
	result.ActionID = x.Claim.ActionID
	result.AttemptID = x.Claim.AttemptID
	result.Provider = "codex"
	result.Identity = src.Identity
	result.RequirementRevision = x.Claim.RequirementRevision
	result.Candidate = x.Claim.Candidate
	result.Status = "PASS"
	if strings.HasSuffix(string(x.Claim.Task.Step), "-design") {
		result.Status = "DRAFT"
	}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	src.Status = "UNAVAILABLE"
	src.Reason = "fixture source is unavailable"
	return preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
}
func facadeDesign(r Run, mode string) protocol.QADesign {
	prefix := strings.ToUpper(mode)
	m := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: r.Regular.RequiredSources.Binding, ReviewKind: coverage.ReviewKind(prefix), RouteScope: gitprep.Digest(r.Regular.Route), TopologyScope: r.Regular.Topology}, Sources: []string{r.Regular.Requirement.Path}, Points: []coverage.AcceptancePoint{{PointID: prefix + ":AC-001", ObservableBehavior: "command output", Oracle: "hello"}}, Cases: []coverage.AcceptanceCase{{CaseID: prefix + ":C1", Command: &coverage.Command{Executable: "go", Args: []string{"run", "."}}, Mode: coverage.CaseMode(prefix), PublicEntry: "go run .", Preconditions: "Go installed", Steps: "execute command", Oracle: "hello"}}, Edges: []coverage.CoverageEdge{{SourceID: r.Regular.Requirement.Path, PointID: prefix + ":AC-001", CaseID: prefix + ":C1"}}}
	if mode == "whitebox" {
		m.Cases[0] = coverage.AcceptanceCase{CaseID: prefix + ":C1", Command: &coverage.Command{Executable: "go", Args: []string{"test", "./..."}}, Mode: coverage.CaseWhitebox, TestRef: "main_test.go::TestHello", Oracle: "hello"}
	}
	d := protocol.QADesign{Manifest: m, CoverageJudgment: "Independently compared the exact hello requirement to the case oracle"}
	if mode == "whitebox" {
		d.TestFiles = []string{"main_test.go"}
	}
	return d
}
func facadeReview(r Run, mode string) *coverage.ReviewResult {
	m := r.Regular.Preparation.QA[mode].Design.Manifest
	return &coverage.ReviewResult{Binding: m.Binding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: r.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: m.Points[0].PointID, Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: m.Cases[0].CaseID, Status: coverage.StatusPass}}}
}
func writePreparationFile(t *testing.T, root, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestPreparationRealGitCandidates(t *testing.T) {
	for _, whitebox := range []bool{false, true} {
		t.Run(fmt.Sprint(whitebox), func(t *testing.T) {
			modes := []string{"blackbox"}
			if whitebox {
				modes = append(modes, "whitebox")
			}
			f := facadePreparation(t, modes)
			r, _ := f.Show()
			dev := r.Regular.Preparation.Resources["development"]
			base := r.Regular.BaseSnapshot
			writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
			writePreparationFile(t, dev, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
			x := facadeExecution(t, f, "development-worker")
			facadeResult(t, f, x, protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "go test ./...", Observation: "Go fixture prepared", Result: "PASS"}}, Handoff: "No unresolved decisions"})
			// Commit, restart from the stored projection, then let blackbox finish.
			var err error
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			f, err = Open(f.root, "regular")
			if err != nil {
				t.Fatal(err)
			}
			if r.Regular.Preparation.DevelopmentSnapshot == base {
				t.Fatal("development not committed")
			}
			data, err := os.ReadFile(filepath.Join(r.Regular.Preparation.Resources["blackbox"], "main.go"))
			if !os.IsNotExist(err) {
				t.Fatalf("baseline contaminated: %s %v", data, err)
			}
			d := facadeDesign(r, "blackbox")
			facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &d})
			r, _ = f.Show()
			facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
			if whitebox {
				r, err = f.Drive()
				if err != nil {
					t.Fatal(err)
				}
				wb := r.Regular.Preparation.Resources["whitebox"]
				writePreparationFile(t, wb, "main_test.go", "package main\nimport \"testing\"\nfunc TestHello(t *testing.T){ main() }\n")
				d = facadeDesign(r, "whitebox")
				facadeResult(t, f, facadeExecution(t, f, "whitebox-design"), protocol.PreparationResult{Manifest: []string{"main_test.go"}, Design: &d, Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "test calls command", Procedure: "go test -run TestHello", Observation: "PASS", Comparison: "hello output", Result: "EXECUTED_PASS"}}})
				r, err = f.Drive()
				if err != nil {
					t.Fatal(err)
				}
				facadeResult(t, f, facadeExecution(t, f, "whitebox-review"), protocol.PreparationResult{Review: facadeReview(r, "whitebox")})
			}
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			c := r.Regular.Preparation.Candidate
			if c == nil || !c.ReviewReady {
				t.Fatalf("no candidate: %+v", r.Regular.Preparation)
			}
			head, _ := gitprep.Head(f.root)
			if head != base {
				t.Fatal("candidate changed primary workspace")
			}
			for _, mode := range modes {
				if _, err = gitprep.Git(f.root, "cat-file", "-e", c.Candidate.Identity+":"+protocol.PreparationArtifact(r.RunID, mode)); err != nil {
					t.Fatal(err)
				}
			}
			if whitebox && !strings.Contains(strings.Join(c.TestOwned, "\n"), "main_test.go") {
				t.Fatal("developer/QA test scope absent")
			}
			count, _ := gitprep.Git(f.root, "rev-list", "--count", base+".."+c.Candidate.Identity)
			if count != "1" {
				t.Fatal("final candidate not consolidated", count)
			}
			facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "fixture complete"})
			if _, err = f.Drive(); err != nil {
				t.Fatal(err)
			}
			worktrees, _ := gitprep.Git(f.root, "worktree", "list", "--porcelain")
			if strings.Contains(worktrees, "resources") {
				t.Fatal("registered worktree leaked")
			}
		})
	}
}
func TestPreparationNativeConcurrent(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in: two real native sessions and actual concurrent baseline QA/development")
	}
	f := facadePreparationBatches(t, []string{"blackbox"}, 2)
	// Save the observed run even when the native provider or result contract fails.
	// This is evidence from this invocation, never a substitute for its assertions.
	t.Cleanup(func() {
		if path := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); path != "" {
			r, err := f.Show()
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := json.MarshalIndent(r, "", "  ")
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Error(err)
				return
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Error(err)
			}
		}
	})
	dev := facadeExecution(t, f, "development-worker")
	black := facadeExecution(t, f, "blackbox-design")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, x := range []protocol.RegularExecution{dev, black} {
		wg.Add(1)
		go func(x protocol.RegularExecution) {
			defer wg.Done()
			_, err := f.ExecuteNative(x.Claim.ActionID, false)
			errs <- err
		}(x)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	a, b := r.Executions[dev.Claim.ActionID], r.Executions[black.Claim.ActionID]
	if a.Identity == "" || b.Identity == "" || a.Identity == b.Identity || !a.Finished || !b.Finished || r.Regular.Preparation.QA["blackbox"].Design == nil || len(r.Regular.Preparation.Batches) != 1 {
		t.Fatalf("native preparation incomplete: %+v", r.Regular)
	}
	assertOverlap := func(left, right string) {
		t.Helper()
		first, second := r.Cost.Dispatches[left].Timing, r.Cost.Dispatches[right].Timing
		if first == nil || second == nil {
			t.Fatal("native timing missing")
		}
		start1, err1 := time.Parse(time.RFC3339Nano, first.StartedAt)
		end1, err2 := time.Parse(time.RFC3339Nano, first.CompletedAt)
		start2, err3 := time.Parse(time.RFC3339Nano, second.StartedAt)
		end2, err4 := time.Parse(time.RFC3339Nano, second.CompletedAt)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || !start1.Before(end2) || !start2.Before(end1) {
			t.Fatal("native task intervals did not overlap")
		}
	}
	assertOverlap(dev.Claim.ActionID, black.Claim.ActionID)
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	second := facadeExecution(t, f, "development-worker")
	review := facadeExecution(t, f, "blackbox-review")
	errs = make(chan error, 2)
	for _, x := range []protocol.RegularExecution{second, review} {
		wg.Add(1)
		go func(x protocol.RegularExecution) {
			defer wg.Done()
			_, err := f.ExecuteNative(x.Claim.ActionID, false)
			errs <- err
		}(x)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	assertOverlap(second.Claim.ActionID, review.Claim.ActionID)
	if r.Executions[second.Claim.ActionID].Identity == a.Identity || !r.Regular.Preparation.QA["blackbox"].Frozen || len(r.Regular.Preparation.Batches) != 2 {
		t.Fatalf("second batch/review failed: %+v", r.Regular)
	}
	for _, x := range r.Executions {
		if strings.Contains(string(x.Claim.Task.Step), "gate") {
			t.Fatal("intermediate gate issued")
		}
	}
	// Commit the second batch, then consolidate the fully approved candidate.
	for i := 0; i < 2; i++ {
		r, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.Regular.Preparation.Candidate == nil || !r.Regular.Preparation.Candidate.ReviewReady {
		t.Fatalf("native candidate incomplete: %+v", r.Regular.Preparation)
	}
	workspace := r.Regular.Preparation.Resources["candidate"]
	var stderr bytes.Buffer
	cmd := exec.Command("go", "run", ".")
	cmd.Dir, cmd.Stderr = workspace, &stderr
	stdout, err := cmd.Output()
	if err != nil || string(stdout) != "hello" || stderr.Len() != 0 {
		t.Fatalf("go run .: stdout=%q stderr=%q err=%v", stdout, stderr.String(), err)
	}
	cmd = exec.Command("go", "test", "./...")
	cmd.Dir = workspace
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("candidate test: %s %v", output, err)
	}
	t.Logf("real development=%s second batch=%s blackbox design=%s review=%s candidate=%s", a.Identity, r.Executions[second.Claim.ActionID].Identity, b.Identity, r.Executions[review.Claim.ActionID].Identity, r.Regular.Preparation.Candidate.Candidate.Identity)
}

// A real subprocess dies after Git has committed and snapshotted the delivery,
// before either the adapter observation or the engine receipt is persisted.
func TestPreparationGitWindowChild(t *testing.T) {
	if os.Getenv("FORMAL_GATES_GIT_WINDOW_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	f, err := Open(os.Getenv("FORMAL_GATES_GIT_WINDOW_ROOT"), "regular")
	if err != nil {
		t.Fatal(err)
	}
	f.fault = func(p persistence.FaultPoint) error {
		if p == persistence.FaultExecuteAfter {
			os.Exit(73)
		}
		return nil
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("actual Git success window not reached")
}
func TestPreparationGitCommitSuccessBeforeReceipt(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	before, _ := gitprep.Head(dev)
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nfunc main(){print(\"hello\")}\n")
	x := facadeExecution(t, f, "development-worker")
	facadeResult(t, f, x, protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "fixture creates declared delivery", Observation: "both paths exist", Result: "PASS"}}, Handoff: "No unresolved decisions"})
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPreparationGitWindowChild$", "-test.v")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_GIT_WINDOW_CHILD=1", "FORMAL_GATES_GIT_WINDOW_ROOT="+f.root)
	output, err := cmd.CombinedOutput()
	if err == nil || cmd.ProcessState.ExitCode() != 73 {
		t.Fatalf("child %v %s", err, output)
	}
	r, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	op := r.Regular.Preparation.Operation
	if op == nil || op.Kind != "batch-commit" {
		t.Fatalf("missing claimed commit operation: %+v", op)
	}
	original := *op
	after, _ := gitprep.Head(dev)
	count := func() string {
		n, e := gitprep.Git(dev, "rev-list", "--count", before+"..HEAD")
		if e != nil {
			t.Fatal(e)
		}
		return n
	}
	if after == before || count() != "1" {
		t.Fatalf("native commit count: %s -> %s = %s", before, after, count())
	}
	observation := filepath.Join(f.stateDir, "git-operations", strings.TrimPrefix(original.ID, "git:sha256:")+".json")
	if _, err = os.Stat(observation); !os.IsNotExist(err) {
		t.Fatalf("observation already exists: %v", err)
	}
	r, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if r.Regular.Preparation.Operation == nil || !r.Regular.Preparation.Operation.Claimed || r.Regular.Preparation.DevelopmentSnapshot == after {
		t.Fatal("formal receipt was already persisted")
	}
	trailer, _ := gitprep.Git(dev, "show", "-s", "--format=%B", after)
	if !strings.Contains(trailer, original.ID) {
		t.Fatalf("actual commit omitted operation binding: %s", trailer)
	}
	f, err = Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Regular.Preparation.DevelopmentSnapshot != after || count() != "1" {
		t.Fatal("recovery duplicated or lost native commit")
	}
	for _, kind := range []string{"batch-commit", "finalize", "whitebox-commit"} {
		q := original
		q.Kind = kind
		ev, _ := protocol.NewRegularEvent(protocol.EventID("stale-git-"+kind), protocol.RegularEvent{Operation: "GIT_CLAIM", ExpectedRevision: recovered.Revision, GitClaim: &q})
		if _, _, err = f.Submit(ev); err == nil {
			t.Fatal("out-of-order Git request accepted")
		}
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	if count() != "1" {
		t.Fatal("rejected/replayed operation committed again")
	}
	writeOperationEvidence(t, "engine-git", map[string]any{"childExitCode": 73, "operation": original, "nativeCommit": after, "base": before, "trailer": trailer, "receiptAbsentAtInterruption": true, "independentCommitCountBeforeRecovery": 1, "independentCommitCountAfterRecoveryAndRefusals": count(), "recoveredDevelopmentSnapshot": recovered.Regular.Preparation.DevelopmentSnapshot})
}

func TestWhiteboxAuthoringTrialsBeforeFrozenQA(t *testing.T) {
	for _, actual := range []string{"hello", "BAD"} {
		t.Run(actual, func(t *testing.T) {
			failed := actual == "BAD"
			f := facadePreparation(t, []string{"blackbox", "whitebox"})
			r, _ := f.Show()
			dev := r.Regular.Preparation.Resources["development"]
			writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
			writePreparationFile(t, dev, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\""+actual+"\")}\n")
			facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "construct isolated predecessor fixture", Observation: "ready for actual independent command test", Result: "PASS"}}, Handoff: "No unresolved decisions"})
			if _, err := f.Drive(); err != nil {
				t.Fatal(err)
			}
			r, _ = f.Show()
			design := facadeDesign(r, "blackbox")
			facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &design})
			r, _ = f.Show()
			facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
			if _, err := f.Drive(); err != nil {
				t.Fatal(err)
			}
			x := facadeExecution(t, f, "whitebox-design")
			src := nativeFixtureClaim(t, f, x)
			r, _ = f.Show()
			_, workspace, err := preparationPrompt(r, x.Claim)
			if err != nil || !filepath.IsAbs(workspace) || workspace != r.Regular.Preparation.Resources["whitebox"] {
				t.Fatalf("authoring workspace: %q %v", workspace, err)
			}

			writePreparationFile(t, workspace, "main_test.go", `package main
import("bytes";"testing";"os/exec")
func TestHello(t *testing.T){var out, stderr bytes.Buffer;cmd:=exec.Command("go","run",".");cmd.Stdout=&out;cmd.Stderr=&stderr;if err:=cmd.Run();err!=nil||out.String()!="hello"||stderr.Len()!=0{t.Fatalf("stdout=%q stderr=%q error=%v",out.String(),stderr.String(),err)}}
`)
			cmd := exec.Command("go", "test", "./...", "-count=1")
			cmd.Dir = workspace
			out, err := cmd.CombinedOutput()
			if (err != nil) != failed {
				t.Fatalf("actual authoring test: %s %v", out, err)
			}
			for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
				q := operationRequest(t, f, x.Claim.ActionID, kind, "authoring-refusal-"+kind)
				q.CaseID = "WHITEBOX:C1"
				if _, err = f.ExecuteTaskOperation(q, false); err == nil || !strings.Contains(err.Error(), "no current QA wave") {
					t.Fatalf("pre-freeze %s: %v", kind, err)
				}
				if _, err = os.Stat(f.taskOperationDir(protocol.TaskOperationID(q))); !os.IsNotExist(err) {
					t.Fatal("refused QA operation created invocation files")
				}
			}
			r, _ = f.Show()
			for _, op := range r.Executions[x.Claim.ActionID].Operations {
				if strings.HasPrefix(op.Request.Kind, "qa.") {
					t.Fatal("authoring trial or refused QA allocated formal QA operations")
				}
			}
			if !protocol.WhiteboxCoverageReady(r.Executions[x.Claim.ActionID]) {
				t.Fatal("authoring lost its coverage-before-tests prerequisite")
			}
			design = facadeDesign(r, "whitebox")
			v := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "DRAFT", Manifest: []string{"main_test.go"}, Design: &design, Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "asserts exit and both exact output streams", Procedure: "go test ./... -count=1 in authoring workspace", Observation: string(out), Comparison: "subprocess exit 0, stdout hello, empty stderr", Result: "EXECUTED_PASS"}}}
			if failed {
				v.Status = "FAIL"
				v.Message = "Production stdout is BAD; the valid independent test requires hello."
				v.Reuse[0].Result = "EXECUTED_FAIL"
			}
			preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
			nativeFixtureStop(t, f, src)
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			if r.Regular.Preparation.QA["whitebox"].Frozen || verificationWave(r) != nil {
				t.Fatal("authoring trial bypassed independent review/freeze")
			}
			if failed {
				if r.Regular.Preparation.Blocked != "PRODUCTION_FAILURE" || len(r.Regular.PreparationErrors) != 0 || r.Regular.Preparation.QA["whitebox"].Reuse[0].Observation != string(out) {
					t.Fatalf("failed trial was not accepted intact: %+v", r.Regular.Preparation)
				}
				facadeChoice(t, f, protocol.RegularRequest{Action: "development-rework", Reason: "Repair the observed production output"})
				if _, err = f.Drive(); err != nil {
					t.Fatal(err)
				}
				r, _ = f.Show()
				if r.Regular.Preparation.Blocked != "" || r.Regular.Preparation.Rework != 1 || !strings.Contains(r.Regular.Preparation.ReworkReason, "BAD") {
					t.Fatalf("controlled rework lost failure: %+v", r.Regular.Preparation)
				}
				facadeExecution(t, f, "development-worker")
				return
			}
			facadeResult(t, f, facadeExecution(t, f, "whitebox-review"), protocol.PreparationResult{Review: facadeReview(r, "whitebox")})
			r, err = f.Drive()
			if err != nil || !r.Regular.Preparation.QA["whitebox"].Frozen || verificationWave(r) == nil {
				t.Fatalf("ordinary review/freeze did not establish wave: %v", err)
			}
			t.Logf("actual authoring go test exit 0: %s; direct premature QA calls=0; independent review/freeze remained required", out)
		})
	}
}

func TestNativeOperationToolMatchesRoleAdmission(t *testing.T) {
	instructions, err := loadActionInstructions(copyPromptPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"product-review", "start-readiness", "development-worker", "blackbox-design", "blackbox-review", "whitebox-design", "whitebox-review", "complexity-gate", "implementation-quality-gate", "blackbox-execution", "whitebox-execution", "repair"} {
		description, err := nativeOperationInstructions(Run{Regular: &protocol.RegularState{ActionInstructions: instructions}}, step)
		if err != nil {
			t.Fatal(err)
		}
		tool := nativeOperationTool(step, description)
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		kinds := props["kind"].(map[string]any)["enum"].([]string)
		qa := step == "blackbox-execution" || step == "whitebox-execution" || step == "repair"
		if (!qa && len(kinds) != 0) || (qa && len(kinds) != 5) {
			t.Fatalf("%s operation kinds: %v", step, kinds)
		}
		if !qa {
			cases := props["caseId"].(map[string]any)["enum"].([]string)
			if len(cases) != 1 || cases[0] != "" {
				t.Fatalf("%s unavailable operation caseId: %v", step, cases)
			}
		}
		if strings.Contains(description, "fixture development-worker section 5") != qa {
			t.Fatalf("%s received the wrong tool section: %s", step, description)
		}
	}
}

// preparationPrompt renders the production material for prompt assertions in this package.
func preparationPrompt(run Run, claim protocol.ExecutionClaim) (string, string, error) {
	material, err := preparePreparationPrompt(run, claim)
	if err != nil {
		return "", "", err
	}
	prompt, err := material.render(claim)
	return prompt, material.workspace, err
}
