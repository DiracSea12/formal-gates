package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

func frozenVerificationFixture(t *testing.T, wrong bool) *Facade {
	return frozenVerificationCommandFixture(t, wrong, nil)
}
func frozenVerificationCommandFixture(t *testing.T, wrong bool, command *coverage.Command, configure ...func(*coverage.AcceptanceManifest)) *Facade {
	t.Helper()
	f := facadePreparation(t, []string{"blackbox"})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	value := "hello"
	if wrong {
		value = "wrong"
	}
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", fmt.Sprintf("package main\nimport \"fmt\"\nfunc main(){fmt.Print(%q)}\n", value))
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "construct developer result fixture", Observation: "ready for actual independent command oracle", Result: "PASS"}}, Handoff: "No unresolved decisions"})
	var err error
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	d := facadeDesign(r, "blackbox")
	if command != nil {
		d.Manifest.Cases[0].Command = command
	}
	for _, configureManifest := range configure {
		configureManifest(&d.Manifest)
	}
	facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &d})
	r, _ = f.Show()
	review := facadeReview(r, "blackbox")
	for _, c := range d.Manifest.Cases[1:] {
		review.CaseDecisions = append(review.CaseDecisions, coverage.ReviewDecision{ID: c.CaseID, Status: coverage.StatusPass})
	}
	for _, step := range d.Manifest.SharedSteps {
		review.SharedStepDecisions = append(review.SharedStepDecisions, coverage.ReviewDecision{ID: step.ID, Status: coverage.StatusPass})
	}
	facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: review})
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	return f
}
func nativeFixtureClaim(t *testing.T, f *Facade, x protocol.RegularExecution) protocol.ExecutionSource {
	t.Helper()
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	src := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "deterministic-protocol-fixture:" + x.Claim.ActionID, Status: "RUNNING"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
	if x.Claim.Task.Step == "whitebox-design" {
		facadeCoverage(t, f, x.Claim.ActionID)
	}
	return src
}
func nativeFixtureStop(t *testing.T, f *Facade, src protocol.ExecutionSource) {
	src.Status = "UNAVAILABLE"
	src.Reason = "Protocol result fixture; actual project commands are recorded separately, no native model session"
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
}
func executeVerificationFixture(t *testing.T, f *Facade, step string) protocol.VerificationResult {
	t.Helper()
	x := facadeExecution(t, f, step)
	src := nativeFixtureClaim(t, f, x)
	r, _ := f.Show()
	w := verificationWave(r)
	result := protocol.VerificationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: "PASS", Message: "Protocol fixture with actual command evidence"}
	if step == "blackbox-execution" {
		var receipt protocol.TaskOperationReceipt
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			q := operationRequest(t, f, x.Claim.ActionID, kind, kind)
			var err error
			receipt, err = f.ExecuteTaskOperation(q, false)
			if err != nil || receipt.Status != "COMPLETED" {
				t.Fatalf("%s: %+v %v", kind, receipt, err)
			}
		}
		root := receipt.Workspace
		out := []byte(receipt.Output)
		var err error
		if receipt.ExitCode != 0 {
			err = fmt.Errorf("target exit %d", receipt.ExitCode)
		}
		status := "PASS"
		if err != nil || string(out) != "hello" {
			status = "FAIL"
			result.Status = "FAIL"
		}
		result.Cases = []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: status, Procedure: "go run .", Output: string(out), Comparison: fmt.Sprintf("stdout == hello: %v; exit error: %v", string(out) == "hello", err), Environment: "current Go host", Tool: "go", Workspace: root}}
		if status == "FAIL" {
			result.Findings = []protocol.ReviewFinding{{ID: "hello-output", Severity: "P1", Category: "normal-use", Requirement: "BLACKBOX:C1", Problem: "documented stdout differs", NormalPath: "go run .", Consequence: "user receives wrong result", SeverityRationale: "required public behavior is broken", Remedy: "print exactly hello"}}
		}
	}
	a, _, err := f.submitAdapterRegular("test-result:"+w.ID+step, protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result})
	if err != nil || a.Status != "ACCEPTED" {
		t.Fatalf("verification submit: %+v %v", a, err)
	}
	nativeFixtureStop(t, f, src)
	return result
}
func TestVerificationActualFailRepairAffectedPromotionSeal(t *testing.T) {
	f := frozenVerificationFixture(t, true)
	r, _ := f.Show()
	initial := r.Regular.Preparation.Candidate.Candidate.Identity
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	r, _ = f.Show()
	w := verificationWave(r)
	if !w.Complete {
		t.Fatal("wave not complete")
	}
	a := protocol.WaveAdjudication{WaveID: w.ID, Target: "blackbox", ResultDigest: w.Results["blackbox"].Digest, Decisions: []protocol.WaveFindingDecision{{FindingID: "hello-output", Decision: "NEW", ProblemID: "wrong-hello", Severity: "P1", Reason: "documented public output is incorrect", Evidence: w.Results["blackbox"].Cases[0].Output, AffectedCases: []string{"BLACKBOX:C1"}}}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &a})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	repair := verificationRepair(r)
	root := r.Regular.Preparation.Resources[repair.ID]
	x := facadeExecution(t, f, "repair")
	src := nativeFixtureClaim(t, f, x)
	// Host-limited developer continuations reuse the repair action and identity.
	for i := 0; i < 4; i++ {
		pending := protocol.RepairResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, PlanID: repair.ID, Status: "CONTINUE", Message: fmt.Sprintf("fixture host turn boundary %d", i)}
		accepted, current, err := f.submitAdapterRegular(fmt.Sprintf("repair-continue-%d", i), protocol.RegularEvent{Operation: "REPAIR_RESULT", RepairResult: &pending})
		if err != nil || accepted.Status != "CONTINUE" {
			t.Fatal(accepted, err)
		}
		live := current.Executions[x.Claim.ActionID]
		if live.Finished || live.Number != x.Number || !checkpointCanResume(current, live) || verificationRepair(current).Result != nil || len(current.Regular.RepairErrors) != 0 {
			t.Fatal("continuation spent an attempt or completed repair")
		}
		material, err := prepareVerificationPrompt(current, live.Claim)
		if err != nil || material.input["continuation"] == nil {
			t.Fatal("repair handoff missing", err)
		}
		nativeFixtureStop(t, f, src)
		claim := live.Claim
		claim.Operation = "resume"
		claim.Identity = src.Identity
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &claim})
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
	}
	writePreparationFile(t, root, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
	check := func(args ...string) protocol.BatchValidation {
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(out), err)
		}
		return protocol.BatchValidation{Procedure: "go " + strings.Join(args, " "), Observation: "exit 0: " + string(out), Result: "PASS"}
	}
	build := check("build", "-o", t.TempDir()+"/hello", ".")
	tests := check("test", "./...")
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "hello" {
		t.Fatal(string(out), err)
	}
	result := protocol.RepairResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, PlanID: repair.ID, Status: "PASS", Message: "corrected required output", Manifest: []string{"main.go"}, Resolved: repair.Problems, Build: build, ProjectTests: tests, Cases: []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "PASS", Procedure: "go run .", Output: string(out), Comparison: "matches exact required stdout"}}}
	accepted, _, err := f.submitAdapterRegular("repair-result", protocol.RegularEvent{Operation: "REPAIR_RESULT", RepairResult: &result})
	if err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal(accepted, err)
	}
	nativeFixtureStop(t, f, src)
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	if r.Regular.Preparation.Candidate.Candidate.Identity == initial {
		t.Fatal("repair did not create new candidate")
	}
	if !r.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("ordinary code repair thawed QA")
	}
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	r, _ = f.Show()
	if len(r.Regular.Verification.Waves) != 2 || verificationWave(r).Targets[0].Scope != coverage.ScopeAffected {
		t.Fatal("repair should finish with AFFECTED, without an extra FULL wave")
	}
	for i := 0; i < 3; i++ {
		r, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == "COMPLETE" {
			break
		}
	}
	if r.Regular.Terminal != "SEALED" || r.Regular.Verification.Seal == nil {
		t.Fatalf("missing seal %+v", r.Next)
	}
	head, _ := gitprep.Head(f.root)
	if head != r.Regular.Verification.Seal.Candidate {
		t.Fatal("seal does not bind actual promoted HEAD")
	}
	worktrees, _ := gitprep.Git(f.root, "worktree", "list", "--porcelain")
	if strings.Contains(worktrees, "resources") {
		t.Fatal("terminal cleanup leaked worktrees")
	}
	replay, err := f.Drive()
	if err != nil || replay.Regular.Verification.Seal.Candidate != head {
		t.Fatal("terminal replay changed identity", err)
	}
}

func TestVerificationRequirementKeepPromotesNewDocument(t *testing.T) {
	f := frozenVerificationFixture(t, false)
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	r, _ := f.Show()
	old := r.Regular.Preparation.Candidate.Candidate.Identity
	originalBatches := r.Regular.Preparation.Batches
	originalCommits := r.Regular.Preparation.Commits
	impact := map[string]string{"product-review": "affected", "start-readiness": "unknown", "development": "unaffected", "blackbox-review": "affected", "blackbox": "affected", "implementation-quality-gate": "unknown"}
	// facadeChoice explicitly delegates every impact item. Completed batches
	// are separate scopes; classify each one instead of leaving it unknown.
	for id := range originalCommits {
		impact["batch:"+id] = "unaffected"
	}

	facadeChoice(t, f, protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "user adds a clarification and retains existing passes"})
	doc := protocol.Document(r.Regular.Requirement.Path, []byte(r.Regular.Requirement.Text+"\n用户说明：现有 hello 行为保持。\n"))
	writePreparationFile(t, f.root, r.Regular.Requirement.Path, doc.Text)
	facadeChoice(t, f, protocol.RegularRequest{Action: "apply-requirement", Origin: "USER_INITIATED_CHANGE", Meaning: "changed", Document: &doc, Keep: []string{"product-review", "start-readiness", "blackbox-review", "blackbox", "implementation-quality-gate"}, Impact: impact, Reason: "explicitly retain all existing PASS for this clarification"})
	r, _ = f.Show()
	for _, target := range []string{"blackbox", "implementation-quality-gate"} {
		ev, ok := r.Regular.Verification.Effective[target]
		if !ok || ev.Candidate != old || ev.RequirementRevision != doc.Revision {
			t.Fatalf("confirmation did not atomically bind retained %s", target)
		}
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "same confirmed batches and no split"})
	r, _ = f.Show()
	if !reflect.DeepEqual(r.Regular.Preparation.Batches, originalBatches) ||
		!reflect.DeepEqual(r.Regular.Preparation.Commits, originalCommits) {
		t.Fatal("explicitly retained completed batches or commit receipts changed")
	}
	for _, x := range r.Executions {
		if x.Claim.Task.Step == "development-worker" && !x.Finished && x.SupersededBy == "" {
			t.Fatalf("retained development was reissued: %s", x.Claim.ActionID)
		}
	}

	var err error
	for i := 0; i < 5; i++ {
		r, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == "COMPLETE" {
			break
		}
	}
	if r.Regular.Terminal != "SEALED" {
		t.Fatalf("kept PASS did not allow progress: next=%+v wave=%+v effective=%+v", r.Next, verificationWave(r), r.Regular.Verification.Effective)
	}
	if r.Regular.Verification.Seal.Candidate == old || r.Regular.Verification.Seal.RequirementRevision != doc.Revision {
		t.Fatal("retention sealed obsolete document")
	}
	kept, assembled := map[string]bool{}, map[string]bool{}
	for _, in := range r.Regular.Verification.Inheritance {
		if in.Kind == "USER_KEEP" {
			if in.Source.Candidate != old || in.Candidate != old || in.RequirementRevision != doc.Revision || in.StateRevision == 0 {
				t.Fatal("incomplete original authorization", in)
			}
			kept[in.Source.Target] = true
		}
		if in.Kind == "IMPACT_CARRY" && strings.HasPrefix(in.Reason, "requirement artifact assembly: ") {
			if in.Source.Candidate != old || in.Candidate != r.Regular.Verification.Seal.Candidate || in.RequirementRevision != doc.Revision || in.Source.RequirementRevision != doc.Revision {
				t.Fatal("invalid actual assembly binding", in)
			}
			assembled[in.Source.Target] = true
		}
	}
	for _, target := range []string{"blackbox", "implementation-quality-gate"} {
		if !kept[target] || !assembled[target] {
			t.Fatalf("missing complete authorization/assembly chain for %s", target)
		}
	}
}

// Preparation and manual operator adjudication are explicit fixtures. All wave
// workers, the repair, source accounting, Git promotion and final case execution
// below use the real native adapter during this opt-in run.
func TestVerificationNativeFailRepairToSeal(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in: real Codex verification and repair workers")
	}
	f := frozenVerificationFixture(t, true)
	t.Cleanup(func() {
		if path := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); path != "" {
			r, err := f.Show()
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := json.MarshalIndent(r, "", "  ")
			if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
				err = os.WriteFile(path, data, 0600)
			}
			if err != nil {
				t.Error(err)
			}
		}
	})
	initial, _ := f.Show()
	first := initial.Regular.Preparation.Candidate.Candidate.Identity
	sawFail := false
	sawRepair := false
	for turns := 0; turns < 18; turns++ {
		r, err := f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		for _, repair := range r.Regular.Verification.Repairs {
			if repair.Committed != "" {
				sawRepair = true
			}
		}
		if r.Regular.Terminal == "SEALED" {
			if !sawFail || !sawRepair || r.Regular.Verification.Seal.Candidate == first {
				t.Fatal("native path skipped actual FAIL/repair/new candidate")
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = f.root
			out, err := cmd.Output()
			if err != nil || string(out) != "hello" {
				t.Fatalf("promoted native result: %q %v", out, err)
			}
			t.Logf("native FAIL→repair→FULL→Seal; original=%s promoted=%s executions=%d", first, r.Regular.Verification.Seal.Candidate, len(r.Executions))
			return
		}
		if r.Regular.Ask != nil {
			t.Fatalf("native path needs explicit further disposition: %+v", r.Regular.Ask)
		}
		var work []protocol.RegularExecution
		for _, x := range r.Executions {
			if verificationAction(string(x.Claim.Task.Step)) && !x.Finished && !x.Claimed {
				work = append(work, x)
			}
		}
		if len(work) > 0 {
			var wg sync.WaitGroup
			errs := make(chan error, len(work))
			for _, x := range work {
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
			continue
		}
		r, _ = f.Show()
		w := verificationWave(r)
		if w == nil {
			t.Fatal("missing wave")
		}
		for target, result := range w.Results {
			if target == "blackbox" && result.Status == "FAIL" {
				sawFail = true
			}
			already := false
			for _, a := range r.Regular.Verification.Adjudications {
				if a.WaveID == w.ID && a.Target == target && a.ResultDigest == result.Digest {
					already = true
				}
			}
			if already || len(result.Findings) == 0 {
				continue
			}
			ds := []protocol.WaveFindingDecision{}
			for _, finding := range result.Findings {
				ds = append(ds, protocol.WaveFindingDecision{FindingID: finding.ID, Decision: "NEW", ProblemID: target + ":" + finding.ID, Severity: finding.Severity, Reason: "Explicit integration-fixture adjudication of the returned normal-path finding", Evidence: finding.NormalPath + "; " + finding.Consequence, AffectedCases: []string{"BLACKBOX:C1"}})
			}
			preparationSubmit(t, f, protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &protocol.WaveAdjudication{WaveID: w.ID, Target: target, ResultDigest: result.Digest, Decisions: ds}})
		}
		for _, repair := range r.Regular.Verification.Repairs {
			if repair.Committed != "" {
				sawRepair = true
			}
		}
	}
	r, _ := f.Show()
	t.Fatalf("native path did not converge: %+v errors=%+v repairs=%+v", r.Next, r.Regular.VerificationErrors, r.Regular.RepairErrors)
}

// verificationPrompt renders the production material for prompt assertions in this package.
func verificationPrompt(run Run, claim protocol.ExecutionClaim) (string, string, error) {
	material, err := prepareVerificationPrompt(run, claim)
	if err != nil {
		return "", "", err
	}
	prompt, err := material.render(claim)
	return prompt, material.workspace, err
}
