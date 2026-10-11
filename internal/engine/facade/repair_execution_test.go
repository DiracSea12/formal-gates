package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestRepairMemberRedoUsesCurrentSourceClosure(t *testing.T) {
	d, initial, id := nativeRepairReady(t, "P2")
	p := repairBatchPlan("FINAL")
	p.Batches = p.Batches[:2]
	d.business(id, protocol.ValidationResult{Binding: initial.Binding, Status: "PASS", GranularityReview: p})
	d.drive()
	parser, pi := planBatchAction(d, "parser")
	logging, li := planBatchAction(d, "logging")
	nativeMemberCompletion(t, d, parser, pi)
	nativeMemberCompletion(t, d, logging, li)
	verify, old := repairComparisonAction(t, d, "")
	if old.Repair.FinalCandidate == nil {
		t.Fatal("initial closure lacks final verification")
	}
	oldSource := old.Repair.FinalCandidate.Identity
	document := validationDocument + "\n"
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	var choices []protocol.RequirementWorkDecision
	for _, item := range d.run.RequirementWork {
		disposition := protocol.RequirementWorkCarry
		if item.Task == pi.Binding.Task || item.Task == li.Binding.Task {
			disposition = protocol.RequirementWorkRedo
		}
		choices = append(choices, protocol.RequirementWorkDecision{Work: item, Disposition: disposition, Reason: "repeat selected implementation under confirmed document"})
	}
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "formatting", ImpactDigest: "member-source", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices}
	event, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(event)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Reason: "confirm member replacement"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	d.reopen()
	d.drive()
	var current []protocol.WorkBinding
	for _, batch := range []string{"logging", "parser"} {
		action, input := planBatchAction(d, batch)
		if input.Binding.Task == pi.Binding.Task || input.Binding.Task == li.Binding.Task {
			t.Fatal("REDO did not replace execution identity")
		}
		if input.Binding.Generation == initial.Binding.Generation {
			t.Fatal("replacement lost current requirement occurrence")
		}
		nativeMemberCompletion(t, d, action, input)
		current = append(current, input.Binding)
		d.reopen()
		d.drive()
	}
	var action string
	var input protocol.ValidationInput
	for id, pending := range d.run.ValidationInputs {
		if pending.Repair != nil && pending.Repair.FinalCandidate != nil && pending.Binding.Task != old.Binding.Task {
			action, input = id, pending
		}
	}
	if input.Repair == nil || input.Repair.FinalCandidate == nil || len(input.Repair.MemberResults) != 2 || input.Repair.FinalCandidate.Identity == oldSource || action == verify {
		t.Fatal("replacement sources reused the old assembly or verification")
	}
	for _, binding := range current {
		found := false
		for _, result := range input.Repair.MemberResults {
			found = found || result.Validation.Binding.Task == binding.Task
		}
		if !found {
			t.Fatal("final closure omitted a current member")
		}
	}
	nativeRepairComparison(t, d, action, input)
	d.reopen()
	d.drive()
	for _, pending := range d.run.ValidationInputs {
		if pending.Repair != nil && pending.Repair.FinalCandidate != nil {
			t.Fatal("accepted final self-run repeated after reopen")
		}
	}
	for _, binding := range current {
		for _, oldBinding := range []protocol.WorkBinding{pi.Binding, li.Binding} {
			if binding.Task == oldBinding.Task {
				t.Fatal("current source retained a retired member identity")
			}
		}
	}
}

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
	d.drive()
	id, in := d.action("engine.qa.design.v4")
	manifest := simpleManifest(in)
	for i := range manifest.Cases {
		argument := "valid"
		if manifest.Cases[i].CaseID == "invalid" {
			argument = "invalid"
		}
		manifest.Cases[i].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "go build -o tested-app . && ./tested-app " + argument}}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, in = d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.drive()
	id, in = d.action("engine.development.batch.v4")
	for path, body := range map[string]string{"go.mod": "module command\n\ngo 1.22\n", "app.go": "package main\nimport \"os\"\nfunc main(){if len(os.Args)>1&&os.Args[1]==\"invalid\"{os.Exit(2)}}\n"} {
		if err := os.WriteFile(filepath.Join(in.Workspace.Path, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	e := batchEvidence(in)
	e.Paths = []string{"app.go", "go.mod"}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: e})
	d.drive()
	id, in = d.action("engine.qa.execution.v3")
	report := executionFor(in)
	tool, environment := protocol.ExecutionIdentities(in.ExecutionFacts)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
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
	id, in = d.action("engine.repair.v5")
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
	e := batchEvidence(in)
	e.AffectedCases = []string{"normal"}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: e})
	d.drive()
	if in.Binding.PureP2 {
		action, comparison := repairComparisonAction(t, d, id)
		if comparison.Binding.VerificationSource != id {
			t.Fatal("member comparison lost original edit")
		}
		nativeRepairComparison(t, d, action, comparison)
		d.drive()
	}
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
							if in.Handler == "engine.gate.v2" && in.Binding.Candidate != candidate.Identity {
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
					verify, vi := repairComparisonAction(t, d, "")
					if vi.Repair.FinalCandidate == nil || vi.Binding.Candidate != vi.Repair.FinalCandidate.Identity || len(vi.Repair.MemberResults) != 3 {
						t.Fatalf("final integrated self-run binding: %+v", vi)
					}
					for _, name := range []string{"parser.go", "logging.go", "command.go"} {
						if _, err := os.ReadFile(filepath.Join(vi.Workspace.Path, name)); err != nil {
							t.Fatal(err)
						}
					}
					nativeRepairComparison(t, d, verify, vi)
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
						if in.Handler == "engine.gate.v2" {
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
			if in.Handler == "engine.gate.v2" && in.Binding.BatchID == batch {
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
	if ci.Binding.Task == li.Binding.Task || ci.Repair.OriginTask != initial.Repair.OriginTask {
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
	e := batchEvidence(ci)
	e.AffectedCases = []string{"normal"}
	d.business(correction, protocol.ValidationResult{Binding: ci.Binding, Status: "PASS", Development: e})
	d.drive()
	comparison, compared := repairComparisonAction(t, d, correction)
	if compared.Binding.VerificationSource != correction {
		t.Fatal("correction comparison lost source edit")
	}
	nativeRepairComparison(t, d, comparison, compared)
	d.drive()
	d.reopen()
	d.drive()
	for _, in := range d.run.ValidationInputs {
		if in.Handler == "engine.gate.v2" && in.Binding.BatchID == "logging" {
			t.Fatal("P2 member correction repeated gate review")
		}
	}
	verify, vi := repairComparisonAction(t, d, "")
	if vi.Repair.FinalCandidate == nil || len(vi.Repair.MemberResults) != 2 {
		t.Fatal("P2 member correction omitted final integrated self-run")
	}
	actual, err := os.ReadFile(filepath.Join(vi.Workspace.Path, "logging.go"))
	if err != nil || string(actual) != changed {
		t.Fatalf("integrated corrected source differs: %q %v", actual, err)
	}
	nativeRepairComparison(t, d, verify, vi)
	d.drive()
	state := driverState(t, d)
	if state.Results[failed].Validation.Status != "FAIL" || state.Results[failed].Validation.Findings[0].Message != "normal output log text differs from the documented text" {
		t.Fatal("repair rewrote original gate evidence")
	}
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	for {
		var action string
		var input protocol.ValidationInput
		for id, in := range d.run.ValidationInputs {
			if in.Handler == "engine.qa.execution.v3" || in.Handler == "engine.gate.v2" {
				action, input = id, in
				break
			}
		}
		if action == "" {
			break
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if input.Handler == "engine.qa.execution.v3" {
			report := executionFor(input)
			result.Execution = &report
			result.Tool, result.Environment = protocol.ExecutionIdentities(input.ExecutionFacts)
		}
		d.business(action, result)
		d.drive()
	}
	d.reopen()
	d.drive()
	state = driverState(t, d)
	last := state.Validation.Waves[len(state.Validation.Waves)-1]
	if last.Status != "COMPLETE" || last.Scope != coverage.ScopeAffected {
		t.Fatalf("required affected validation did not complete: %+v", last)
	}
	problem := state.Validation.Findings[finding].History[0].ProblemID
	if state.Validation.Problems[problem].ResolvedWave == "" {
		t.Fatal("completed member source problem stayed open")
	}
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.repair.v5" {
			t.Fatal("completed source problem created another repair")
		}
	}
	if state.Results[failed].Validation.Status != "FAIL" || state.Results[failed].Validation.Findings[0].Message != "normal output log text differs from the documented text" {
		t.Fatal("final completion rewrote source FAIL")
	}
}

func nativeRepairComparison(t *testing.T, d *validationDriver, action string, input protocol.ValidationInput) {
	t.Helper()
	if input.Handler != "engine.repair.verify.v2" || len(input.ExecutionFacts) != len(input.Repair.VerificationCases) {
		t.Fatal("comparison lacks complete native case facts")
	}
	records := []*worker.Observation{}
	for _, fact := range input.ExecutionFacts {
		if fact.CaseID != "normal" || len(fact.Operations) == 0 {
			t.Fatal("comparison omitted the frozen normal invocation")
		}
		last := fact.Operations[len(fact.Operations)-1]
		if last.Evidence.ExitCode != 0 || last.Evidence.StdoutText != "" || last.Evidence.StderrText != "" {
			t.Fatalf("normal public invocation actual facts: %+v", last.Evidence)
		}

	}
	for _, caseID := range input.Repair.VerificationCases {
		index := slices.IndexFunc(input.ExecutionFacts, func(fact protocol.ExecutionFact) bool { return fact.CaseID == caseID })
		if index < 0 {
			t.Fatal("repair comparison lacks original facts for its selected case")
		}
		records = append(records, &worker.Observation{Comparison: "actual command exit and output match the frozen oracle", Result: coverage.ExecutedPass})
	}
	raw, err := json.Marshal(worker.Execution{Records: records})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResult(snapshot.State.WorkerInputs(d.f.definition)[action], d.eventID(), raw, "sha256:semantic-comparison")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(action)
	d.submit(event)
	stored := driverState(t, d).Results[action].Validation.Development
	if stored == nil || !slices.Equal(stored.Paths, input.Repair.VerificationPaths) || !slices.Equal(stored.AffectedCases, input.Repair.VerificationCases) {
		t.Fatal("native comparison did not preserve program-derived union")
	}
	for _, fact := range input.ExecutionFacts {
		procedure, observation, err := fact.Render()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(stored.CaseRuns, func(row protocol.RepairCaseEvidence) bool { return row.CaseID == fact.CaseID })
		if index < 0 || stored.CaseRuns[index].Procedure != procedure || stored.CaseRuns[index].Observation != observation {
			t.Fatal("comparison changed native facts")
		}
	}
}

func repairComparisonAction(t *testing.T, d *validationDriver, sourceAction string) (string, protocol.ValidationInput) {
	t.Helper()
	for action, input := range d.run.ValidationInputs {
		if input.Handler == "engine.repair.verify.v2" && (sourceAction != "" && input.Binding.VerificationSource == sourceAction || sourceAction == "" && input.Repair != nil && len(input.Repair.MemberResults) > 1) {
			return action, input
		}
	}
	t.Fatalf("missing comparison for exact edit %q; current inputs=%+v next=%+v", sourceAction, d.run.ValidationInputs, d.run.Next)
	return "", protocol.ValidationInput{}
}
