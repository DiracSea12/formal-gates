package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func TestRepairCompleteVerificationUsesApprovedCommands(t *testing.T) {
	for _, regression := range []bool{true, false} {
		t.Run(map[bool]string{true: "regression", false: "passed"}[regression], func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			plan := commandPlan()
			d := nativeMasterAt(t, root, base, nil)
			binary := filepath.Join(d.f.admission.Target, "bin", "formal-gates")
			build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
			build.Dir = "../../.."
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("runtime %v %s", err, out)
			}
			passPreDevelopmentReviews(t, d, plan)
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command"}})
			d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
			d.drive()
			id, in := d.action("engine.qa.design.v5")
			m := simpleManifest(in)
			for i := range m.Cases {
				argument := "valid"
				if m.Cases[i].CaseID == "invalid" {
					argument = "invalid"
				}
				m.Cases[i].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "go build -o tested-app .; ./tested-app " + argument}}
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			d.drive()
			id, in = d.action("engine.qa.review.rationale.v2")
			approved := approvedReview(m)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			d.drive()
			id, in = d.action("engine.development.batch.v5")
			app := "package main\nimport \"os\"\nfunc main(){if len(os.Args)>1&&os.Args[1]==\"invalid\"{os.Exit(2)}}\n"
			for name, body := range map[string]string{"go.mod": "module command\n\ngo 1.22\n", "app.go": app} {
				if err := os.WriteFile(filepath.Join(in.Workspace.Path, name), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			edit := batchEvidence(in)
			edit.Paths = []string{"app.go", "go.mod"}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: edit})
			wait := func(handler string) (string, protocol.ValidationInput) {
				for {
					d.drive()
					for action, input := range d.run.ValidationInputs {
						if input.Handler == handler {
							return action, input
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			id, in = wait("engine.qa.execution.v3")
			report := executionFor(in)
			tool, environment := protocol.ExecutionIdentities(in.ExecutionFacts)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
			id, in = d.gateAction("quality")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "normal command diagnostic needs repair"}}})
			d.drive()
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			finding := ""
			for key, f := range d.run.Validation.Findings {
				if f.WaveID == wave.ID {
					finding = key
				}
			}
			resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "command", RootCause: "diagnostic", Consequence: "wrong output", Severity: "P2", SeverityRationale: "normal command usable", Verification: review.FindingVerification{Scope: "command", Severity: "limited output", Causality: "diagnostic", Reproduction: "normal command", Classification: "implementation"}, Reason: "observed command output"}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.drive()
			id, in = d.action("engine.repair.v6")
			if in.Development != nil {
				t.Fatal("small repair unexpectedly requires a batch plan")
			}
			testBody := "package main\nimport \"testing\"\nfunc TestRegression(t *testing.T) {}\n"
			if regression {
				testBody = "package main\nimport \"testing\"\nfunc TestRegression(t *testing.T){t.Fatal(\"normal regression remains\")}\n"
			}
			if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app_test.go"), []byte(testBody), 0644); err != nil {
				t.Fatal(err)
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "Implemented command repair", AffectedCases: []string{"normal"}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "normal", Procedure: "textual claimed run", Observation: "claimed exit zero", Comparison: "claimed match", Result: coverage.ExecutedPass}}}})
			foundTest := false
			var actual protocol.CheckEvidence
			for {
				d.drive()
				state := driverState(t, d)
				for _, receipt := range state.HostActionReceipts {
					if receipt.AdapterOperation != "op.check" || receipt.Status != protocol.HostActionStatusExecuted {
						continue
					}
					var evidence protocol.CheckEvidence
					if decodeReceiptEvidence(receipt, &evidence) != nil || evidence.SourceActionID != id {
						continue
					}
					var request struct {
						Argv []string `json:"argv"`
					}
					if readCheckDocument(filepath.Join(evidence.Directory, "request.json"), &request) == nil && len(request.Argv) == 3 && request.Argv[2] == plan.Batches[0].Checks[1].Command {
						foundTest, actual = true, evidence
					}
				}
				if foundTest {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !foundTest {
				t.Fatalf("repair produced no exact complete-test receipt: next=%+v raw=%+v intents=%+v resources=%+v rejected=%+v", d.run.Next, driverState(t, d).Results[id], driverState(t, d).PendingHostActions, driverState(t, d).NativeResources, driverState(t, d).RejectedResults)
			}
			if regression {
				if actual.ExitCode != 1 {
					t.Fatalf("complete suite actualexit=%d", actual.ExitCode)
				}
				state := driverState(t, d)
				if state.Results[id].Outcome != protocol.OutcomePass {
					t.Fatal("raw edit result was rewritten")
				}
				if state.Validation.Waves[len(state.Validation.Waves)-1].Candidate.Identity != wave.Candidate.Identity {
					t.Fatal("failed full tests promoted repaired source")
				}
				continuation, available := state.WorkerInput(id, d.f.definition)
				if !available || continuation.Pending.ActionID != id || continuation.Validation.Binding != in.Binding {
					t.Fatal("failed full tests lost the original repair continuation")
				}
				out, err := os.ReadFile(filepath.Join(actual.Directory, "stdout.log"))
				if err != nil || !strings.Contains(string(out), "normal regression remains") {
					t.Fatalf("original failed suite output %q %v", out, err)
				}
				return
			}
			if actual.ExitCode != 0 {
				t.Fatalf("passing complete suite failed: %+v", actual)
			}
			verification, input := wait("engine.repair.verify.v2")
			comparisons := []*worker.Observation{}
			for _, caseID := range input.Repair.VerificationCases {
				index := slices.IndexFunc(input.ExecutionFacts, func(fact protocol.ExecutionFact) bool { return fact.CaseID == caseID })
				if index < 0 {
					t.Fatal("repair comparison lacks original native facts for its issued case")
				}
				comparisons = append(comparisons, &worker.Observation{Comparison: "actual exit and output match the approved normal oracle", Result: coverage.ExecutedPass})
			}
			raw, err := json.Marshal(worker.Execution{Records: comparisons})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			workerInput := snapshot.State.WorkerInputs(d.f.definition)[verification]
			event, err := worker.DecodeResult(workerInput, d.eventID(), raw, "sha256:semantic-original")
			if err != nil {
				t.Fatal(err)
			}
			d.receipt(verification)
			d.submit(event)
			d.drive()
			state := driverState(t, d)
			stored := state.Results[verification].Validation.Development
			if stored == nil || len(stored.CaseRuns) != 1 || stored.CaseRuns[0].Observation == "claimed exit zero" {
				t.Fatal("comparison did not retain program-derived actual facts")
			}
			d.reopen()
			d.drive()
			if driverState(t, d).Results[verification].Digest != state.Results[verification].Digest {
				t.Fatal("reopen changed original comparison result")
			}

		})
	}
}

func childWithP2GateRepair(t *testing.T) (*validationDriver, string, protocol.ValidationInput) {
	t.Helper()
	master := receiptMaster(t)
	child := receiptChild(t, master, "core", "lane-core", "default")
	quality, input := child.gateAction("quality")
	child.business(quality, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "documented output order differs"}}})
	security, input := child.gateAction("security")
	child.business(security, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	wave := child.run.Validation.Waves[0]
	var finding string
	for id := range child.run.Validation.Findings {
		finding = id
	}
	resolveGateFinding(child, finding, wave.ID)
	repair, input := child.action("engine.repair.v6")
	return child, repair, input
}

func assertChildRepairHasNoComparisonWorker(t *testing.T, child *validationDriver) {
	t.Helper()
	state := driverState(t, child)
	if len(state.Validation.Sets) != 0 {
		t.Fatal("child unexpectedly selected QA")
	}
	authorized := map[string]bool{}
	for _, binding := range state.DispatchBindings {
		if binding.Task.Step == "repair.verification" {
			t.Fatal("empty oracle comparison created an external worker")
		}
		authorized[binding.Task.String()] = true
	}
	for _, wave := range state.Validation.Waves {
		for _, target := range wave.Targets {
			authorized[target.Task.String()] = true
		}
		if wave.Repair != nil {
			authorized[wave.Repair.Task.String()] = true
			for _, target := range wave.Repair.Targets {
				authorized[target.Task.String()] = true
			}
		}
	}
	if state.Guard == nil || state.Guard.RequestAllowance != len(authorized) {
		t.Fatalf("empty semantic work added a budget slot: guard=%+v actual obligations=%v", state.Guard, authorized)
	}
	for _, result := range state.Results {
		if result.Validation != nil && result.Validation.Binding.Task.Step == "repair.verification" {
			t.Fatal("empty comparison fabricated a worker result")
		}
	}
}

func nativeRepairChecks(t *testing.T, child *validationDriver, sourceActions ...string) map[string]protocol.CheckEvidence {
	t.Helper()
	state := driverState(t, child)
	checks := map[string]protocol.CheckEvidence{}
	for action, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.Status != protocol.HostActionStatusExecuted {
			continue
		}
		intent := state.PendingHostActions[action]
		if intent.Adapter == nil {
			if effect := state.ReconciledEffects[action]; effect.Intent != nil {
				intent = *effect.Intent
			}
		}
		if intent.Adapter == nil {
			t.Fatal("native check lost its original intent")
		}
		raw, err := json.Marshal(intent.Adapter.Params)
		if err != nil {
			t.Fatal(err)
		}
		var params protocol.CheckParameters
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatal(err)
		}
		if len(sourceActions) == 1 {
			if params.Repair != nil || params.SourceActionID != sourceActions[0] {
				continue
			}
		} else {
			if params.Repair == nil {
				continue
			}
			actual, expected := slices.Clone(params.Repair.SourceActions), slices.Clone(sourceActions)
			slices.Sort(actual)
			slices.Sort(expected)
			if !slices.Equal(actual, expected) {
				continue
			}
		}
		if params.Check.Purpose != protocol.CheckFullBuild && params.Check.Purpose != protocol.CheckFullTests {
			continue
		}
		var evidence protocol.CheckEvidence
		if err := decodeReceiptEvidence(receipt, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.SourceIdentity != params.SourceIdentity || evidence.SourceDigest != params.SourceDigest || evidence.ParametersDigest != intent.PayloadDigest || evidence.Error != "" {
			t.Fatal("full check lost its exact original source and parameters")
		}
		command := commandPlan().Batches[0].Checks[0].Command
		if params.Check.Purpose == protocol.CheckFullTests {
			command = commandPlan().Batches[0].Checks[1].Command
		}
		if params.Check.Command != command {
			t.Fatal("full check did not execute the original approved command")
		}
		checks[params.Check.Purpose] = evidence
	}
	return checks
}

func TestChildRepairWithoutQACasesUsesNativeCompletion(t *testing.T) {
	for _, regression := range []bool{false, true} {
		t.Run(map[bool]string{false: "passed", true: "regression"}[regression], func(t *testing.T) {
			child, repair, input := childWithP2GateRepair(t)
			body := "package main\nfunc main() {}\n// documented output order\n"
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			paths := []string{"app.go"}
			if regression {
				if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app_test.go"), []byte("package main\nimport \"testing\"\nfunc TestCommand(t *testing.T) { t.Fatal(\"documented output still fails\") }\n"), 0600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, "app_test.go")
			}
			child.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: paths, Summary: "corrected ordering"}})
			child.drive()
			checks := nativeRepairChecks(t, child, repair)
			if len(checks) != 2 || checks[protocol.CheckFullBuild].ExitCode != 0 || (checks[protocol.CheckFullTests].ExitCode != 0) != regression {
				t.Fatalf("repair skipped or misclassified original full checks: %+v", checks)
			}
			state := driverState(t, child)
			assertChildRepairHasNoComparisonWorker(t, child)
			for _, resource := range state.NativeResources {
				if resource.Role == "repair-verification" {
					t.Fatal("single empty comparison created an unused workspace")
				}
			}
			if state.Results[repair].Validation.Status != "PASS" || state.Results[repair].Validation.Development.Summary != "corrected ordering" {
				t.Fatal("mechanical completion rewrote original repair evidence")
			}
			if regression {
				if state.Validation.Waves[0].Status == "REPAIRED" || state.Validation.DevelopmentSnapshot != "" {
					t.Fatal("failed full tests completed the repair")
				}
				continuation, available := state.WorkerInput(repair, child.f.definition)
				if !available || continuation.Pending.ActionID != repair || continuation.Validation.Binding != input.Binding {
					t.Fatal("failed empty-case repair lost its original continuation")
				}
			} else {
				if state.Validation.Waves[0].Status != "REPAIRED" || state.Validation.DevelopmentSnapshot == "" {
					t.Fatal("native checks did not complete the empty repair and source assembly")
				}
				actual, err := (vcs.ExecRunner{}).Run(child.f.root, "git", "show", state.Validation.DevelopmentSnapshot+":app.go")
				if err != nil || actual != strings.TrimSpace(body) {
					t.Fatalf("assembled source differs from checked implementation: %q %v", actual, err)
				}
			}
			child.reopen()
			child.drive()
			child.drive()
			assertChildRepairHasNoComparisonWorker(t, child)
			if len(driverState(t, child).HostActionReceipts) != len(state.HostActionReceipts) {
				t.Fatal("reopen repeated completed native verification")
			}
		})
	}
}

func TestChildRepairWithoutQACasesChecksIntegratedSource(t *testing.T) {
	for _, regression := range []bool{false, true} {
		t.Run(map[bool]string{false: "passed", true: "regression"}[regression], func(t *testing.T) {
			child, repair, input := childWithP2GateRepair(t)
			plan := repairBatchPlan("FINAL")
			plan.Batches = plan.Batches[:2]
			if regression {
				plan.Batches[1].Artifacts = append(plan.Batches[1].Artifacts, "logging_test.go")
			}
			child.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", GranularityReview: plan})
			child.drive()
			var actions []string
			for _, batch := range []string{"parser", "logging"} {
				action, member := planBatchAction(child, batch)
				body := "package main\nfunc " + batch + "() {}\n"
				if err := os.WriteFile(filepath.Join(member.Workspace.Path, batch+".go"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if regression && batch == "logging" {
					body := "package main\nimport (\"os\"; \"testing\")\nfunc TestComposition(t *testing.T) { if _, err := os.Stat(\"parser.go\"); err == nil { t.Fatal(\"integrated composition fails\") } }\n"
					if err := os.WriteFile(filepath.Join(member.Workspace.Path, "logging_test.go"), []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
				}
				child.business(action, protocol.ValidationResult{Binding: member.Binding, Status: "PASS", Development: batchEvidence(member)})
				child.drive()
				checks := nativeRepairChecks(t, child, action)
				if len(checks) != 2 || checks[protocol.CheckFullBuild].ExitCode != 0 || checks[protocol.CheckFullTests].ExitCode != 0 {
					t.Fatalf("member checks did not pass before integration: %+v", checks)
				}
				actions = append(actions, action)
				child.reopen()
				child.drive()
			}
			checks := nativeRepairChecks(t, child, actions...)
			if len(checks) != 2 || checks[protocol.CheckFullBuild].ExitCode != 0 || (checks[protocol.CheckFullTests].ExitCode != 0) != regression {
				t.Fatalf("integrated source did not run both original full checks: %+v", checks)
			}
			build, tests := checks[protocol.CheckFullBuild], checks[protocol.CheckFullTests]
			if build.SourceIdentity != tests.SourceIdentity || build.SourceDigest != tests.SourceDigest {
				t.Fatal("integrated full checks observed different sources")
			}
			for _, batch := range []string{"parser", "logging"} {
				actual, err := (vcs.ExecRunner{}).Run(child.f.root, "git", "show", build.SourceIdentity+":"+batch+".go")
				if err != nil || actual != "package main\nfunc "+batch+"() {}" {
					t.Fatalf("full checks omitted integrated member %s: %q %v", batch, actual, err)
				}
			}
			state := driverState(t, child)
			assertChildRepairHasNoComparisonWorker(t, child)
			if regression {
				if state.Validation.Waves[0].Status == "REPAIRED" || child.run.Next.Operator == nil || state.Validation.DevelopmentSnapshot != "" {
					t.Fatal("integrated test failure was treated as completed repair")
				}
			} else {
				if state.Validation.Waves[0].Status != "REPAIRED" || state.Validation.DevelopmentSnapshot == "" {
					t.Fatal("native final checks did not release the source assembly owner")
				}
				for _, sourceAction := range actions {
					result := state.Results[sourceAction].Validation.Development
					if result.Completion == nil || len(result.Completion.DoD) == 0 || result.Completion.Handoff == "" {
						t.Fatal("mechanical final completion lost original member DoD and handoff")
					}
				}
			}
			child.reopen()
			child.drive()
			child.drive()
			assertChildRepairHasNoComparisonWorker(t, child)
			if len(driverState(t, child).HostActionReceipts) != len(state.HostActionReceipts) {
				t.Fatal("recovery repeated the integrated native full checks")
			}
		})
	}
}

func TestPureP2RepairWithApprovedCasesRequiresAffectedCases(t *testing.T) {
	d, input, repair := nativeRepairReady(t, "P2")
	if len(input.Contract.Manifests) == 0 || !input.Binding.PureP2 {
		t.Fatal("normal repair lacks the approved QA prerequisite")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() {}\n// documented ordering\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "corrected ordering"}})
	state := driverState(t, d)
	if _, rejected := state.RejectedResults[repair]; !rejected {
		t.Fatal("pure P2 repair accepted an empty affected set despite approved QA cases")
	}
	if result := state.Results[repair]; result.Validation != nil && result.Validation.Development != nil {
		t.Fatal("incomplete repair declaration became valid completion evidence")
	}
	for _, intent := range state.PendingHostActions {
		if intent.Adapter != nil && (intent.Adapter.Operation == "op.check" || intent.Adapter.Operation == "op.candidate") {
			t.Fatal("rejected empty declaration scheduled native verification or source assembly")
		}
	}
}
