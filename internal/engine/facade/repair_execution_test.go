package facade

import (
	"fmt"
	"formal-gates/internal/engine/vcs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func repairBatchPlan(tier string) *protocol.GranularityReview {
	p := commandPlan()
	base := p.Batches[0]
	p.Batches = nil
	for _, id := range []string{"parser", "logging", "command"} {
		b := base
		b.ID = id
		b.Artifacts = []string{id + ".go"}
		b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-contract", Description: "implement " + id}}
		b.Route = &protocol.RouteDecision{Mode: "custom", Gates: []string{"quality", "security"}, Selected: []string{"quality"}}
		if id == "command" {
			b.DependsOn = []string{"parser"}
		}
		p.Batches = append(p.Batches, b)
	}
	p.VerificationTier = tier
	if tier == "CHECKPOINT" {
		p.CheckpointGroups = [][]string{{"parser", "logging"}, {"command"}}
		p.CheckpointReason = "command consumes completed parser and logging boundaries"
	}
	return p
}

func nativeRepairReady(t *testing.T, severity string) (*validationDriver, protocol.ValidationInput, string) {
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one public command"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.drive()
	id, in := d.action("engine.qa.design")
	manifest := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.drive()
	id, in = d.action("engine.development.batch")
	for path, body := range map[string]string{"go.mod": "module command\n\ngo 1.22\n", "app.go": "package main\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(in.Workspace.Path, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	e := batchEvidence(in)
	e.Paths = []string{"app.go", "go.mod"}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: e})
	d.drive()
	id, in = d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "fixture command", Environment: "native workspace"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: severity, Message: "command must compose its documented parser and logging behavior", SeverityRationale: "documented command cannot compose required output", Requirement: "AC-001", NormalPath: "app run valid-input", ActualError: "composition absent", DeferralImpact: "documented output unavailable", Locations: []string{"app.go:1"}}}})
	d.drive()
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if w.Status != "ADJUDICATING" {
		t.Fatalf("native setup not collected: wave=%+v targets=%+v inputs=%+v", w, w.Targets, d.run.ValidationInputs)
	}
	var finding string
	for id, f := range d.run.Validation.Findings {
		if f.WaveID == w.ID {
			finding = id
		}
	}
	r := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid-input", Module: "command", RootCause: "composition", Consequence: "documented result missing", Severity: severity, SeverityRationale: "documented command behavior", Verification: review.FindingVerification{Scope: "command", Severity: "documented severity", Causality: "composition", Reproduction: "normal command invocation", Classification: "implementation"}, Reason: "native output comparison"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: w.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
	d.drive()
	id, in = d.action("engine.repair")
	return d, in, id
}

func nativeMemberCompletion(t *testing.T, d *validationDriver, id string, in protocol.ValidationInput) {
	t.Helper()
	batch := in.Development.Batch.ID
	body := map[string]string{"parser": "package main\nfunc parse() string { return \"parsed\" }\n", "logging": "package main\nfunc logMessage() string { return \"logged\" }\n", "command": "package main\nfunc command() string { return parse() }\n"}[batch]
	if batch == "command" {
		b, err := os.ReadFile(filepath.Join(in.Workspace.Path, "parser.go"))
		if err != nil || string(b) != "package main\nfunc parse() string { return \"parsed\" }\n" {
			t.Fatalf("dependency content absent at execution prefix: %q %v", b, err)
		}
	}
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, batch+".go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cases := nativeRepairSelfRun(t, in.Workspace.Path)
	e := batchEvidence(in)
	e.AffectedCases = []string{"normal"}
	e.CaseRuns = cases
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: e})
	d.drive()
}

func TestRepairNativeDependenciesAndFinalSelfRun(t *testing.T) {
	for _, severity := range []string{"P1", "P2"} {
		for _, tier := range []string{"FINAL", "CHECKPOINT"} {
			t.Run(fmt.Sprintf("%s/%s", severity, tier), func(t *testing.T) {
				d, initial, id := nativeRepairReady(t, severity)
				original := driverState(t, d)
				candidate := original.Validation.Waves[0].Candidate
				frozen := original.Validation.Sets[coverage.ReviewBlackbox].Version
				d.business(id, protocol.ValidationResult{Binding: initial.Binding, Status: "PASS", GranularityReview: repairBatchPlan(tier)})
				d.drive()
				parser, pi := planBatchAction(d, "parser")
				logging, li := planBatchAction(d, "logging")
				if parser == logging || pi.Binding.WorkspaceID == li.Binding.WorkspaceID || len(d.run.RequirementWork) < 3 {
					t.Fatal("repair frontier or control inventory collapsed")
				}
				for _, input := range d.run.ValidationInputs {
					if input.Development != nil && input.Development.Batch.ID == "command" {
						t.Fatal("dependent command issued before parser")
					}
				}
				nativeMemberCompletion(t, d, logging, li)
				if d.run.Validation.DevelopmentSnapshot != "" {
					t.Fatal("partial repair became Dn")
				}
				d.reopen()
				d.drive()
				nativeMemberCompletion(t, d, parser, pi)
				finishGates := func() {
					seen := map[string]bool{}
					for {
						var action string
						var input protocol.ValidationInput
						for id, in := range d.run.ValidationInputs {
							if in.Handler == "engine.gate" && in.Binding.Candidate != candidate.Identity {
								action, input = id, in
								break
							}
						}
						if action == "" {
							return
						}
						if seen[action] {
							t.Fatalf("same gate repeated after completion: %s target=%+v rejected=%+v", action, driverState(t, d).Validation.Waves[0].Repair.Targets[input.Binding.Task.String()], driverState(t, d).RejectedResults[action])
						}
						seen[action] = true
						if input.Gate == nil || input.Gate.Gate != "quality" {
							t.Fatalf("custom slice gate inputs: %+v", input)
						}
						d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
						d.drive()
					}
				}
				finishGates()
				command, ci := planBatchAction(d, "command")
				if ci.Development.Dependencies["parser"].Validation.Binding != pi.Binding {
					t.Fatal("dependency handoff identity changed")
				}
				nativeMemberCompletion(t, d, command, ci)
				finishGates()
				d.reopen()
				d.drive()
				if severity == "P2" {
					verify, vi := d.action("engine.repair")
					if vi.Repair.FinalCandidate == nil || vi.Binding.Candidate != vi.Repair.FinalCandidate.Identity || len(vi.Repair.MemberResults) != 3 {
						t.Fatalf("final integrated self-run binding: %+v", vi)
					}
					for _, name := range []string{"parser.go", "logging.go", "command.go"} {
						if _, err := os.ReadFile(filepath.Join(vi.Workspace.Path, name)); err != nil {
							t.Fatal(err)
						}
					}
					cases := nativeRepairSelfRun(t, vi.Workspace.Path)
					e := gateRepairResult(vi).Development
					e.Paths = []string{"parser.go", "logging.go", "command.go"}
					e.CaseRuns = cases
					d.business(verify, protocol.ValidationResult{Binding: vi.Binding, Status: "PASS", Development: e})
					d.drive()
				}
				if d.run.Validation.Sets[coverage.ReviewBlackbox].Version != frozen {
					t.Fatal("repair changed frozen QA")
				}
				state := driverState(t, d)
				wave := state.Validation.Waves[len(state.Validation.Waves)-1]
				for _, task := range []protocol.WorkBinding{pi.Binding, li.Binding, ci.Binding} {
					if !slices.Contains(wave.SourceTasks, task.Task) {
						t.Fatalf("final source set omitted member %s: %v", task.Task, wave.SourceTasks)
					}
				}
				for path, expected := range map[string]string{"parser.go": "package main\nfunc parse() string { return \"parsed\" }\n", "logging.go": "package main\nfunc logMessage() string { return \"logged\" }\n", "command.go": "package main\nfunc command() string { return parse() }\n"} {
					actual, err := (vcs.ExecRunner{}).Run(d.f.root, "git", "show", state.Validation.DevelopmentSnapshot+":"+path)
					if err != nil || actual != strings.TrimSpace(expected) {
						t.Fatalf("final native content %s: %q %v", path, actual, err)
					}
				}
				if len(state.SliceChildren) != 0 || state.Topology.Decision != "no-split" {
					t.Fatal("repair created child lifecycle or changed topology")
				}
				if severity == "P2" {
					for _, in := range d.run.ValidationInputs {
						if in.Handler == "engine.gate" {
							t.Fatal("pure P2 repeated a gate review")
						}
					}
				}
				if !slices.Contains(d.run.Validation.Waves[0].Repair.Problems, initial.Repair.Problems[0].ID) {
					t.Fatal("original repair problem changed")
				}
			})
		}
	}
}

func TestRepairNativeGateFailureKeepsFrozenSource(t *testing.T) {
	d, initial, id := nativeRepairReady(t, "P1")
	p := repairBatchPlan("FINAL")
	p.Batches = p.Batches[:2]
	d.business(id, protocol.ValidationResult{Binding: initial.Binding, Status: "PASS", GranularityReview: p})
	d.drive()
	parser, pi := planBatchAction(d, "parser")
	logging, li := planBatchAction(d, "logging")
	nativeMemberCompletion(t, d, logging, li)
	nativeMemberCompletion(t, d, parser, pi)
	var failed string
	for _, batch := range []string{"parser", "logging"} {
		var action string
		var input protocol.ValidationInput
		for id, in := range d.run.ValidationInputs {
			if in.Handler == "engine.gate" && in.Binding.BatchID == batch {
				action, input = id, in
				break
			}
		}
		if action == "" {
			t.Fatalf("missing %s gate", batch)
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if batch == "logging" {
			failed = action
			result.Status = "FAIL"
			result.Findings = []review.Finding{{Severity: "P2", Message: "normal output log text differs from the documented text"}}
		}
		d.business(action, result)
		d.drive()
	}
	wave := d.run.Validation.Waves[0]
	var finding string
	for id, f := range d.run.Validation.Findings {
		if f.Target == d.run.Validation.Waves[0].Repair.Targets[driverState(t, d).Results[failed].Validation.Binding.Task.String()].Task.String() {
			finding = id
		}
	}
	resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid-input", Module: "logging", RootCause: "normal text", Consequence: "documented text differs", Severity: "P2", SeverityRationale: "normal command remains usable", Verification: review.FindingVerification{Scope: "normal command", Severity: "limited text defect", Causality: "logging text", Reproduction: "normal command output", Classification: "implementation"}, Reason: "verified source-specific text mismatch"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
	d.reopen()
	d.drive()
	correction, ci := planBatchAction(d, "logging")
	if ci.Binding.Task == li.Binding.Task || ci.Repair.Obligation.Task != initial.Repair.Obligation.Task {
		t.Fatal("correction lost its source generation or original obligation")
	}
	prior, err := os.ReadFile(filepath.Join(ci.Workspace.Path, "logging.go"))
	if err != nil || string(prior) != "package main\nfunc logMessage() string { return \"logged\" }\n" {
		t.Fatalf("correction did not start at the exact completed source: %q %v", prior, err)
	}
	changed := "package main\nfunc logMessage() string { return \"documented log\" }\n"
	if err := os.WriteFile(filepath.Join(ci.Workspace.Path, "logging.go"), []byte(changed), 0644); err != nil {
		t.Fatal(err)
	}
	cases := nativeRepairSelfRun(t, ci.Workspace.Path)
	e := batchEvidence(ci)
	e.AffectedCases = []string{"normal"}
	e.CaseRuns = cases
	d.business(correction, protocol.ValidationResult{Binding: ci.Binding, Status: "PASS", Development: e})
	d.drive()
	d.reopen()
	d.drive()
	for _, in := range d.run.ValidationInputs {
		if in.Handler == "engine.gate" && in.Binding.BatchID == "logging" {
			t.Fatal("P2 member correction repeated gate review")
		}
	}
	verify, vi := d.action("engine.repair")
	if vi.Repair.FinalCandidate == nil || len(vi.Repair.MemberResults) != 2 {
		t.Fatal("P2 member correction omitted final integrated self-run")
	}
	actual, err := os.ReadFile(filepath.Join(vi.Workspace.Path, "logging.go"))
	if err != nil || string(actual) != changed {
		t.Fatalf("integrated corrected source differs: %q %v", actual, err)
	}
	final := gateRepairResult(vi).Development
	final.Paths = []string{"parser.go", "logging.go"}
	final.CaseRuns = nativeRepairSelfRun(t, vi.Workspace.Path)
	d.business(verify, protocol.ValidationResult{Binding: vi.Binding, Status: "PASS", Development: final})
	d.drive()
	state := driverState(t, d)
	if state.Results[failed].Validation.Status != "FAIL" || state.Results[failed].Validation.Findings[0].Message != "normal output log text differs from the documented text" {
		t.Fatal("repair rewrote original gate evidence")
	}
}

func nativeRepairSelfRun(t *testing.T, workspace string) []protocol.RepairCaseEvidence {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "command")
	for _, args := range [][]string{{"build", "-o", binary, "./..."}, {"test", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = workspace
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("self-run %v: %s %v", args, output, err)
		}
	}
	cmd := exec.Command(binary, "run", "valid-input")
	cmd.Dir = workspace
	if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("normal public invocation: %s %v", output, err)
	}
	return []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "normal", Result: coverage.ExecutedPass, Procedure: "execute the built command with run valid-input in the bound workspace", Observation: "exit zero with empty output", Comparison: "matches the approved normal-case exits-zero oracle"}}
}
