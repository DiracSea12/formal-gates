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
			id, in := d.action("engine.qa.design")
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
			id, in = d.action("engine.qa.review.rationale")
			approved := approvedReview(m)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			d.drive()
			id, in = d.action("engine.development.batch")
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
			id, in = wait("engine.qa.execution")
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
			id, in = d.action("engine.repair")
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
				if d.run.Next.Operator == nil {
					t.Fatal("failed full tests silently waited")
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
			verification, input := wait("engine.repair.verify")
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
