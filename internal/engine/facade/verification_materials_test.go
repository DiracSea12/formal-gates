package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func largeVerificationMaterialFixture(t *testing.T) *Facade {
	t.Helper()
	f := facadePreparation(t, []string{"blackbox"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "route-add", Reason: "exercise both bound gate readers", Route: &protocol.RegularRoute{Kind: "custom", QA: []string{"blackbox"}, Gates: []string{"complexity-gate"}}})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n/*\n"+strings.Repeat("production material bound to this candidate\n", 32768)+"*/\n")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "fixture", Observation: "prepared", Result: "PASS"}}, Handoff: "fixture"})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	design := facadeDesign(r, "blackbox")
	design.CoverageJudgment = strings.Repeat("UNNEEDED-DESIGN-RATIONALE\n", 16384)
	design.Manifest.SharedSteps = []coverage.SharedStep{{ID: "needed", Procedure: "Use the bound QA preparation"}, {ID: "unused", Procedure: strings.Repeat("UNNEEDED-SHARED-STEP\n", 8192)}}
	design.Manifest.Cases[0].SharedSteps = []string{"needed"}
	facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &design})
	r, _ = f.Show()
	review := facadeReview(r, "blackbox")
	for _, s := range design.Manifest.SharedSteps {
		review.SharedStepDecisions = append(review.SharedStepDecisions, coverage.ReviewDecision{ID: s.ID, Status: coverage.StatusPass})
	}
	facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: review})
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	return f
}

// Render the prior production assembly against the SAME immutable inputs and
// actual final dispatch binding for a comparable character/byte measurement.
func priorVerificationMessage(t *testing.T, run Run, claim protocol.ExecutionClaim) string {
	t.Helper()
	m, err := prepareVerificationPrompt(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	w := verificationWave(run)
	p := map[string]any{"binding": claim, "requirement": run.Regular.Requirement, "baseSnapshot": run.Regular.BaseSnapshot, "candidate": claim.Candidate}
	step := string(claim.Task.Step)
	if step == "repair" {
		p["repairPlan"] = verificationRepair(run)
		p["problems"] = run.Regular.Verification.Problems
		p["sourceResults"] = w.Results
		p["frozenQA"] = run.Regular.Preparation.QA
	} else if strings.HasSuffix(step, "-execution") {
		mode := strings.TrimSuffix(step, "-execution")
		p["design"] = run.Regular.Preparation.QA[mode].Design
		for _, target := range w.Targets {
			if target.Target == mode {
				p["target"] = target
			}
		}
		p["checkpoint"] = w.Checkpoints[mode]
	} else {
		args := []string{"-C", m.workspace, "diff", "--binary", run.Regular.BaseSnapshot, claim.Candidate, "--", "."}
		for _, path := range w.Candidate.TestOwned {
			args = append(args, ":(exclude)"+path)
		}
		for path := range w.Candidate.Artifacts {
			if path != run.Regular.Requirement.Path {
				args = append(args, ":(exclude)"+path)
			}
		}
		diff, err := exec.Command("git", args...).Output()
		if err != nil {
			t.Fatal(err)
		}
		p["fullImplementationDiff"] = string(diff)
		p["reviewerInstructions"] = run.Regular.ReviewerInstructions
		p["gateInstructions"] = run.Regular.CatalogGateInstructions[step]
		p["reviewScope"] = "FULL"
	}
	text, err := assemblePrompt(m.instructions, p)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

type verificationMaterialTransport struct {
	codexRuntimeAdapter
	t    *testing.T
	f    *Facade
	seen bool
	step string
}

func (a *verificationMaterialTransport) turn(q nativeTurn) (string, error) {
	a.seen = true
	run, err := a.f.Show()
	if err != nil {
		a.t.Fatal(err)
	}
	old := priorVerificationMessage(a.t, run, q.Claim)
	a.t.Logf("SENT %s old=%d chars/%d bytes new=%d chars/%d bytes", a.step, utf8.RuneCountInString(old), len(old), utf8.RuneCountInString(q.Prompt), len(q.Prompt))
	for _, bad := range []string{"\"fullImplementationDiff\":", "UNNEEDED-DESIGN-RATIONALE", "UNNEEDED-SHARED-STEP", "UNRELATED-PROBLEM", "\"frozenQA\"", "\"sourceResults\""} {
		if strings.Contains(q.Prompt, bad) {
			a.t.Fatalf("dispatch contains %s", bad)
		}
	}
	material, err := prepareVerificationPrompt(run, q.Claim)
	if err != nil {
		a.t.Fatal(err)
	}
	if err = material.bindFiles(q.Job, run.Regular.Requirement, a.f.root); err != nil {
		a.t.Fatal(err)
	}
	for name, want := range material.files {
		path := filepath.Join(q.Job, name)
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			a.t.Fatalf("ordinary file read %s: %v", name, err)
		}
		if !strings.Contains(q.Prompt, path) {
			a.t.Fatalf("unreachable file reference %s", name)
		}
	}
	if strings.HasSuffix(a.step, "-gate") {
		implementation := material.input["implementation"].(map[string]any)
		args := append([]string{"-C", q.Workspace}, implementation["diffArgs"].([]string)...)
		diff, err := exec.Command("git", args...).Output()
		if err != nil || !strings.Contains(string(diff), "production material") {
			a.t.Fatal("ordinary native git diff cannot read candidate", err)
		}
		if nativeRoleAccess(a.step) != nativeReadOnly {
			a.t.Fatal("gate lost normal read-only tool policy")
		}
	}
	if !reflect.DeepEqual(q.Schema, verificationSchema(q.Claim)) {
		a.t.Fatal("result contract changed")
	}
	return "", fmt.Errorf("fixture stops after actual dispatch read checks")
}
func TestVerificationMaterialActualDispatch(t *testing.T) {
	for _, step := range []string{"complexity-gate", "implementation-quality-gate", "blackbox-execution"} {
		t.Run(step, func(t *testing.T) {
			f := largeVerificationMaterialFixture(t)
			x := facadeExecution(t, f, step)
			spy := &verificationMaterialTransport{t: t, f: f, step: step}
			_, _ = f.executeNativeTransport(x.Claim.ActionID, false, spy)
			if !spy.seen {
				t.Fatal("actual native dispatch not reached")
			}
		})
	}
}
func TestVerificationRepairSelectsOnlyPlanSources(t *testing.T) {
	f := frozenVerificationFixture(t, true)
	executeVerificationFixture(t, f, "blackbox-execution")
	executeVerificationFixture(t, f, "implementation-quality-gate")
	run, _ := f.Show()
	w := verificationWave(run)
	decision := protocol.WaveAdjudication{WaveID: w.ID, Target: "blackbox", ResultDigest: w.Results["blackbox"].Digest, Decisions: []protocol.WaveFindingDecision{{FindingID: "hello-output", Decision: "NEW", ProblemID: "wrong-hello", Severity: "P1", Reason: "required stdout", Evidence: w.Results["blackbox"].Cases[0].Output, AffectedCases: []string{"BLACKBOX:C1"}}}}
	if _, _, err := f.submitAdapterRegular("material-adjudicate", protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &decision}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "repair")
	run, _ = f.Show()
	// Unrelated data must never enter either prompt or selected action files.
	run.Regular.Verification.Problems["unrelated"] = protocol.CanonicalProblem{ID: "unrelated", Evidence: strings.Repeat("UNRELATED-PROBLEM", 8192)}
	m, err := prepareVerificationPrompt(run, x.Claim)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range m.files {
		if strings.Contains(string(data), "UNRELATED-PROBLEM") {
			t.Fatalf("unrelated evidence in %s", name)
		}
	}
	var problems map[string]protocol.CanonicalProblem
	if err = json.Unmarshal(m.files["repair-problems.json"], &problems); err != nil || len(problems) != 1 {
		t.Fatal("repair selection", err)
	}
	if !strings.Contains(string(m.files["repair-qa.json"]), "BLACKBOX:C1") {
		t.Fatal("repair lost its frozen acceptance oracle")
	}
	if len(m.input["resultFiles"].([]any)) != 1 {
		t.Fatal("repair lost related result source")
	}
	spy := &verificationMaterialTransport{t: t, f: f, step: "repair"}
	_, _ = f.executeNativeTransport(x.Claim.ActionID, false, spy)
	if !spy.seen {
		t.Fatal("repair actual dispatch not reached")
	}
}

func TestVerificationExecutionDisclosesOnlyRemainingCases(t *testing.T) {
	f := largeVerificationMaterialFixture(t)
	run, _ := f.Show()
	w := verificationWave(run)
	q := run.Regular.Preparation.QA["blackbox"]
	next := q.Design.Manifest.Cases[0]
	next.CaseID = "BLACKBOX:C2"
	next.Dependencies = []string{"BLACKBOX:C1"}
	q.Design.Manifest.Cases = append(q.Design.Manifest.Cases, next)
	unrelated := next
	unrelated.CaseID = "BLACKBOX:UNRELATED"
	unrelated.Oracle = "OTHER-CASE-ORACLE"
	q.Design.Manifest.Cases = append(q.Design.Manifest.Cases, unrelated)
	for i := range w.Targets {
		if w.Targets[i].Target == "blackbox" {
			w.Targets[i].Expected = []string{"BLACKBOX:C1", "BLACKBOX:C2"}
		}
	}
	w.Checkpoints["blackbox"] = protocol.VerificationResult{Cases: []protocol.CaseExecution{{CaseID: "BLACKBOX:C1", Status: "PASS", Output: strings.Repeat("RETAINED-OUTPUT", 8192)}}, Remaining: []string{"BLACKBOX:C2"}}
	input := map[string]any{}
	files := map[string][]byte{}
	if err := verificationExecutionInputs(run, w, "blackbox", input, files); err != nil {
		t.Fatal(err)
	}
	var scoped struct {
		Execute []string                  `json:"execute"`
		Cases   []coverage.AcceptanceCase `json:"cases"`
		Shared  []coverage.SharedStep     `json:"sharedSteps"`
	}
	if err := json.Unmarshal(files["qa-target.json"], &scoped); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scoped.Execute, []string{"BLACKBOX:C2"}) || len(scoped.Cases) != 2 || scoped.Cases[0].CaseID != "BLACKBOX:C1" || scoped.Cases[1].CaseID != "BLACKBOX:C2" {
		t.Fatal("current case or necessary dependency lost")
	}
	if !reflect.DeepEqual(scoped.Cases[0], q.Design.Manifest.Cases[0]) || !reflect.DeepEqual(scoped.Cases[1], q.Design.Manifest.Cases[1]) {
		t.Fatal("frozen case contract changed")
	}
	if len(scoped.Shared) != 1 || scoped.Shared[0].ID != "needed" {
		t.Fatal("disclosed unused shared preparation")
	}
	data, _ := json.Marshal(input)
	if strings.Contains(string(data), "RETAINED-OUTPUT") || strings.Contains(string(data), "OTHER-CASE-ORACLE") {
		t.Fatal("unneeded evidence in launch input")
	}
	for name, data := range files {
		if strings.Contains(string(data), "OTHER-CASE-ORACLE") || strings.Contains(string(data), "UNNEEDED-DESIGN-RATIONALE") || strings.Contains(string(data), "UNNEEDED-SHARED-STEP") {
			t.Fatalf("unrelated material copied to %s", name)
		}
	}
	if _, ok := files["design.json"]; ok {
		t.Fatal("whole design copied for single-case execution")
	}
	var evidence struct {
		Checkpoint protocol.VerificationResult `json:"checkpoint"`
	}
	if err := json.Unmarshal(files["checkpoint.json"], &evidence); err != nil || !reflect.DeepEqual(evidence.Checkpoint.Cases, w.Checkpoints["blackbox"].Cases) {
		t.Fatal("retained case evidence changed", err)
	}
	q.Design.Manifest.Cases[1].Dependencies = []string{"missing"}
	if _, _, err := verificationCaseScope(q.Design.Manifest, []string{"BLACKBOX:C2"}); err == nil {
		t.Fatal("missing dependency accepted")
	}
}

func TestVerificationReceivesBoundReviewerInstructions(t *testing.T) {
	f := frozenVerificationFixture(t, false)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	text := run.Regular.ReviewerInstructions

	x := facadeExecution(t, f, "implementation-quality-gate")
	m, err := prepareVerificationPrompt(run, x.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if m.input["reviewerInstructions"] != text {
		t.Fatal("verification dispatch did not receive bound reviewer contract")
	}

}

func TestVerificationCaseScopeTransitiveDependencies(t *testing.T) {
	manifest := coverage.AcceptanceManifest{
		Cases:       []coverage.AcceptanceCase{{CaseID: "prerequisite", Oracle: "first", SharedSteps: []string{"setup"}}, {CaseID: "dependency", Oracle: "second", Dependencies: []string{"prerequisite"}}, {CaseID: "current", Oracle: "third", Dependencies: []string{"dependency"}}, {CaseID: "unrelated", Oracle: "not allowed"}},
		SharedSteps: []coverage.SharedStep{{ID: "setup", Procedure: "exact required setup"}, {ID: "other", Procedure: "unrelated"}},
	}
	cases, steps, err := verificationCaseScope(manifest, []string{"current"})
	if err != nil || !reflect.DeepEqual(cases, manifest.Cases[:3]) || !reflect.DeepEqual(steps, manifest.SharedSteps[:1]) {
		t.Fatal("transitive prerequisite contract differs", err)
	}
}
