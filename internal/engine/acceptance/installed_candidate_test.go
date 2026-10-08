package acceptance_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"sync"
)

const candidateDocument = "# Delivery\n\n## 需求点\n\n### REQ-001：Command\n\n#### 要求\nThe command validates its input.\n\n#### 验收条件\n- AC-001：Valid input succeeds and invalid input is rejected.\n\n#### 来源\nUser contract.\n"

// candidateDriver only supplies worker semantics. Every native workflow write
// is executed by a separately launched installed binary; Git calls below are
// project setup or independent reads of that binary's result.
type candidateDriver struct {
	p                                 installedStartProject
	t                                 *testing.T
	run                               facade.Run
	seq                               int
	mode                              string
	plan                              *protocol.GranularityReview
	pauseOnInterrupt                  bool
	implementation                    string
	unknownReceipt                    bool
	unknownAction                     string
	candidate, authoring              string
	candidateTree                     string
	env                               []string
	breakMarker, breakLog, breakStage string
	accepted                          []struct {
		event   protocol.Event
		receipt protocol.Acceptance
	}
}

func newCandidateDriver(t *testing.T, mode string, requirementPath ...string) *candidateDriver {
	p := newInstalledStartProject(t)
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(p.project, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.project, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if len(requirementPath) > 0 {
		p.intake.RequirementSource = requirementPath[0]
		p.intake.Artifacts[0].Path = requirementPath[0]
	}
	write(p.intake.RequirementSource, candidateDocument)
	write("go.mod", "module candidate-app\n\ngo 1.22\n")
	write("old_test.go", "package main\nimport \"testing\"\nfunc TestOld(t *testing.T) {}\n")
	write("remove_test.go", "package main\nimport \"testing\"\nfunc TestRemove(t *testing.T) {}\n")
	write("mode_test.go", "package main\nimport \"testing\"\nfunc TestMode(t *testing.T) {}\n")
	p.git(t, "add", "--all")
	p.git(t, "commit", "-qm", "Candidate project inputs")
	p.base = p.git(t, "rev-parse", "HEAD")
	sum := sha256.Sum256([]byte(candidateDocument))
	p.intake.RequirementRevision = hex.EncodeToString(sum[:])
	p.intake.Artifacts[0].Revision = p.intake.RequirementRevision
	writeInstalledJSON(t, p.intakePath, p.intake)
	p.install(t, "codex", "global")
	p.call(t, nil, p.startArgs("candidate", "regular")...)
	d := &candidateDriver{p: p, t: t, mode: mode}
	d.drive()
	return d
}
func (d *candidateDriver) id() protocol.EventID {
	d.seq++
	return protocol.EventID(fmt.Sprintf("semantic-%d", d.seq))
}
func (d *candidateDriver) submit(e protocol.Event) {
	d.t.Helper()
	if e.ID == "" {
		e.ID = d.id()
	}
	out := d.call(installedJSON(d.t, e), "workflow", "submit", "--root", d.p.project, "--run-id", "candidate")
	var response struct {
		Run        facade.Run          `json:"run"`
		Acceptance protocol.Acceptance `json:"acceptance"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		d.t.Fatal(err)
	}
	if strings.Contains(response.Acceptance.Status, "REJECTED") || response.Acceptance.Status == "OBSOLETE_RESULT" {
		d.t.Fatalf("semantic event %s was not accepted: %+v", e.Kind, response.Acceptance)
	}
	d.run = response.Run
	d.accepted = append(d.accepted, struct {
		event   protocol.Event
		receipt protocol.Acceptance
	}{e, response.Acceptance})
}
func (d *candidateDriver) call(input []byte, args ...string) []byte {
	d.t.Helper()
	cmd := d.p.command(input, args...)
	cmd.Env = append(cmd.Env, d.env...)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	d.t.Logf("installed argv=%v exit=%v elapsed=%s", cmd.Args, err, time.Since(started))
	if err != nil {
		d.t.Fatalf("installed %v: %v\n%s", args, err, output)
	}
	return output
}
func (d *candidateDriver) drive() {
	d.t.Helper()
	d.driveOnce()
	deadline, bounded := d.t.Deadline()
	for d.run.Next.Kind == decision.KindHostAction && d.run.Next.HostAction != nil {
		pending := false
		for _, intent := range d.run.AdapterIntents {
			if intent.Adapter != nil && intent.Adapter.Operation == "op.check" && slices.Contains(d.run.Next.HostAction.Steps, intent.Step) {
				pending = true
				break
			}
		}
		if !pending {
			return
		}
		if bounded && !time.Now().Before(deadline) {
			d.t.Fatalf("installed check did not settle within test deadline: %+v", d.run.Next)
		}
		time.Sleep(100 * time.Millisecond)
		d.driveOnce()
	}
}

func (d *candidateDriver) driveOnce() {
	d.t.Helper()
	args := []string{"workflow", "drive", "--root", d.p.project, "--run-id", "candidate"}
	cmd := d.p.command(nil, args...)
	cmd.Env = append(cmd.Env, d.env...)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	d.t.Logf("installed argv=%v exit=%v elapsed=%s", cmd.Args, err, time.Since(started))
	if err != nil {
		if d.breakMarker == "" {
			d.t.Fatalf("installed drive: %v\n%s", err, output)
		}
		if _, statErr := os.Stat(d.breakMarker); statErr != nil {
			d.t.Fatalf("unplanned drive failure: %v\n%s", err, output)
		}
		d.t.Logf("native %s process interruption: %v %s", d.breakStage, err, output)
		if d.breakStage == "cleanup" {
			d.assertAcceptedReplay(false)
		}
		if d.unknownReceipt {
			d.unknownReceipt = false
			var intent protocol.HostActionIntent
			for _, pending := range d.run.AdapterIntents {
				if pending.Adapter != nil && pending.Adapter.Operation == "op.workspace" {
					intent = pending
				}
			}
			if intent.ActionID == "" {
				d.t.Fatal("lost-response window has no original workspace intent")
			}
			d.unknownAction = intent.ActionID
			event, eventErr := protocol.NewAdapterHostActionReceiptEvent(d.id(), intent.ActionID, intent.Adapter.Operation, "codex", intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
			if eventErr != nil {
				d.t.Fatal(eventErr)
			}
			d.submit(event)
			var stored struct {
				Content protocol.State `json:"content"`
			}
			raw := readInstalledFile(d.t, filepath.Join(d.p.project, facade.EngineNamespace, "candidate", "state.json"))
			if err := json.Unmarshal(raw, &stored); err != nil {
				d.t.Fatal(err)
			}
			if stored.Content.HostActionReceipts[intent.ActionID].Status != protocol.HostActionStatusUnknown {
				d.t.Fatal("unknown response was not durably accepted")
			}
			d.t.Logf("durable uncertain responsibility: action=%s state=%s", intent.ActionID, raw)
		}
		if d.pauseOnInterrupt {
			output = d.call(nil, "workflow", "status", "--root", d.p.project, "--run-id", "candidate")
			if err := json.Unmarshal(output, &d.run); err != nil {
				d.t.Fatal(err)
			}
			return
		}
		cmd = d.p.command(nil, args...)
		cmd.Env = append(cmd.Env, d.env...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			d.t.Fatalf("native resume: %v\n%s", err, output)
		}
	}
	if err := json.Unmarshal(output, &d.run); err != nil {
		d.t.Fatal(err)
	}
}

func candidatePlan() *protocol.GranularityReview {
	return &protocol.GranularityReview{Reason: "One command is a cohesive deliverable.", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{{ID: "command", Subtasks: []protocol.DevelopmentSubtask{{ID: "input", Description: "Validate input"}}, Deliverable: "Command validates input", PublicEntry: "app", Artifacts: []string{"app.go"}, Interfaces: "Command arguments", StateBoundary: "No persistent command state", Risk: "Invalid input", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "Command contract holds"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}}, Rollback: "Revert app.go", Recovery: "Run command again", Handoff: "Command ready", ContextCost: "One small package"}}}
}
func (d *candidateDriver) reviews() {
	for _, kind := range []review.Action{review.ProductReview, review.StartReadiness} {
		var id string
		for action, pending := range d.run.PendingActions {
			if pending.ReviewSeries == kind {
				id = action
			}
		}
		if id == "" {
			d.t.Fatalf("missing review %s", kind)
		}
		event, err := protocol.NewSpawnReceiptEvent(d.id(), id, "codex", id, protocol.SpawnStatusSpawned)
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
		d.captureLifecycle(id)
		result := protocol.ReviewResultEvidence{Action: kind, DispatchID: id, RequirementRevision: d.p.intake.RequirementRevision, Status: "PASS", Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "The confirmed command requirement has a runnable public input contract."}}
		if kind == review.StartReadiness {
			result.GranularityReview = candidatePlan()
			if d.plan != nil {
				result.GranularityReview = d.plan
			}
		}
		event, err = protocol.NewReviewWorkerResultEvent(d.id(), id, "codex", string(d.id()), result)
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
		d.drive()
	}
	request, err := protocol.NewRequestEvent(d.id(), protocol.ControlConfirmTopology, protocol.AskOption{ID: "no-split", Label: "No split"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(request)
	event, err := protocol.NewReviewDecideEvent(d.id(), protocol.RequestID(request.ID), d.token(string(request.ID)), "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command project"}})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	gates := []string{"blackbox", "whitebox", "quality"}
	route := protocol.RouteDecision{Mode: "custom", Gates: gates, Selected: []string{"quality"}}
	switch d.mode {
	case "none":
		route.Mode = "default"
		route.Selected = nil
	case "blackbox":
		route.Selected = append(route.Selected, "blackbox")
	case "whitebox":
		route.Selected = append(route.Selected, "whitebox")
	case "both":
		route.Mode = "full"
		route.Selected = nil
	}
	request, err = protocol.NewRouteRequestEvent(d.id(), gates, protocol.AskOption{ID: protocol.AskOptionID(route.Mode), Label: route.Mode})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(request)
	// This existing semantic preview is read-only. It supplies the user's exact
	// plan confirmation; the installed CLI alone executes native effects.
	opened, err := facade.Open(d.p.project, "candidate")
	if err != nil {
		d.t.Fatal(err)
	}
	preview, err := opened.DevelopmentPlan(route)
	if err != nil {
		d.t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	event, err = protocol.NewReviewDecideEvent(d.id(), protocol.RequestID(request.ID), d.token(string(request.ID)), protocol.AskOptionID(route.Mode), protocol.ReviewDecisionPayload{Route: &route})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.p.intake.RequirementRevision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.p.intake.RequirementRevision, Document: candidateDocument}}})
	d.drive()
}
func (d *candidateDriver) token(id string) string {
	for _, ask := range d.run.PendingAsks {
		if ask.RequestID == id {
			return ask.Token
		}
	}
	d.t.Fatalf("missing Ask %s", id)
	return ""
}
func (d *candidateDriver) business(id string, result protocol.ValidationResult) {
	d.t.Helper()
	input, ok := d.run.ValidationInputs[id]
	if !ok || input.Binding != result.Binding || input.Workspace == nil || input.Workspace.ID != input.Binding.WorkspaceID || input.Workspace.Path == d.p.project {
		d.t.Fatalf("worker input lacks its exact isolated binding: %+v", input)
	}
	if input.Workspace.Status != "READY" || input.Workspace.Owner != "candidate" {
		d.t.Fatal("worker resource owner is not admitted")
	}
	expected := input.Workspace.Identity
	if input.Handler == "engine.qa.execution" || input.Handler == "engine.gate" {
		expected = input.Binding.Candidate
		if input.Workspace.Source != expected {
			d.t.Fatal("execution source is not Qn")
		}
	}
	if input.Handler == "engine.qa.design" && input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox && input.Workspace.Source != input.Binding.Candidate {
		d.t.Fatal("whitebox authoring source is not Dn")
	}
	if head := d.p.git(d.t, "-C", input.Workspace.Path, "rev-parse", "HEAD"); head != expected {
		d.t.Fatalf("worker consumed native identity %s, expected %s", head, expected)
	}
	event, err := protocol.NewSpawnReceiptEvent(d.id(), id, "codex", id, protocol.SpawnStatusSpawned)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	d.captureLifecycle(id)
	event = protocol.Event{ID: d.id(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: "codex", Outcome: "PASS", PayloadDigest: string(d.id()), Validation: &result}}
	d.submit(event)
	d.submit(event)
}
func (d *candidateDriver) captureLifecycle(action string) {
	payload := installedJSON(d.t, map[string]string{"agent_id": action, "cwd": d.p.project})
	for _, event := range []string{"SubagentStart", "SubagentStop"} {
		d.call(payload, "lifecycle", "capture", "--root", d.p.project, "--provider", "codex", "--event", event)
	}
}
func (d *candidateDriver) write(root, path, body string, mode os.FileMode) {
	d.t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(body), mode); err != nil {
		d.t.Fatal(err)
	}
}
func (d *candidateDriver) develop(id string, input protocol.ValidationInput, extraPaths ...string) {
	if input.Handler != "engine.development.batch" || input.Development == nil {
		d.t.Fatal("wrong development method")
	}
	body := d.implementation
	if body == "" {
		body = "package main\nimport \"os\"\nfunc main() {if len(os.Args)!=2 || !valid(os.Args[1]) {os.Exit(2)}}\nfunc valid(s string) bool {return s!=\"\"}\n"
	}
	d.write(input.Workspace.Path, "app.go", body, 0644)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: append([]string{"app.go"}, extraPaths...), Summary: "Implemented input validation", Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"behavior": "Input contract holds"}, Handoff: "Command ready"}}})
}
func (d *candidateDriver) designResult(id string, input protocol.ValidationInput) protocol.ValidationResult {
	manifest := input.Manifest
	manifest.Cases = nil
	manifest.Edges = nil
	mode := manifest.Binding.ReviewKind
	result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
	if mode == coverage.ReviewWhitebox {
		d.authoring = input.Workspace.Path
		d.write(d.authoring, "candidate_test.go", "package main\nimport \"testing\"\nfunc TestValid(t *testing.T) {if !valid(\"ok\"){t.Fatal(\"valid input\")}}\nfunc TestInvalid(t *testing.T) {if valid(\"\"){t.Fatal(\"invalid input\")}}\n", 0644)
		if err := os.Remove(filepath.Join(d.authoring, "remove_test.go")); err != nil {
			d.t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(d.authoring, "old_test.go"), filepath.Join(d.authoring, "renamed_test.go")); err != nil {
			d.t.Fatal(err)
		}
		d.write(d.authoring, "mode_test.go", "package main\nimport \"testing\"\nfunc TestMode(t *testing.T) {t.Log(\"native mode\")}\n", 0755)
		if err := os.Chmod(filepath.Join(d.authoring, "mode_test.go"), 0755); err != nil {
			d.t.Fatal(err)
		}
		result.Development = &protocol.DevelopmentEvidence{Paths: []string{"candidate_test.go", "remove_test.go", "old_test.go", "renamed_test.go", "mode_test.go"}, Summary: "Structural input cases and owned test changes"}
	}
	for i, scenario := range []string{"SUCCESS", "FAILURE"} {
		item := coverage.AcceptanceCase{CaseID: fmt.Sprintf("%s-%d", mode, i), Scenario: scenario, Oracle: "Input contract holds"}
		if mode == coverage.ReviewWhitebox {
			item.Mode = coverage.CaseWhitebox
			item.TestRef = "candidate_test.go::" + []string{"TestValid", "TestInvalid"}[i]
		} else {
			item.Mode = coverage.CaseBlackbox
			item.PublicEntry = "app"
			item.Preconditions = "Command built"
			item.Steps = "Invoke command with input"
			item.Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"go", "build", "-o", ".gates-command", "."}}, {Kind: coverage.OperationArgv, Argv: []string{"./.gates-command", []string{"ok", ""}[i]}}}
			item.Oracle = []string{"Valid input exits zero", "Empty input returns a nonzero exit"}[i]
		}
		manifest.Cases = append(manifest.Cases, item)
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: item.CaseID})
	}
	result.Manifest = &manifest
	return result
}
func (d *candidateDriver) design(id string, input protocol.ValidationInput) {
	d.business(id, d.designResult(id, input))
}
func (d *candidateDriver) caseReview(id string, input protocol.ValidationInput) {
	result := coverage.ReviewResult{Binding: input.Manifest.Binding, Scope: coverage.ScopeFull}
	for _, source := range input.Manifest.Sources {
		result.SourceDecisions = append(result.SourceDecisions, coverage.ReviewDecision{ID: source, Status: coverage.StatusPass, Reason: "Cases cover the command requirement"})
	}
	for _, point := range input.Manifest.Points {
		result.PointDecisions = append(result.PointDecisions, coverage.ReviewDecision{ID: point.PointID, Status: coverage.StatusPass, Reason: "Positive and negative input cases cover the point"})
	}
	for _, item := range input.Manifest.Cases {
		result.CaseDecisions = append(result.CaseDecisions, coverage.ReviewDecision{ID: item.CaseID, Status: coverage.StatusPass, Reason: "Concrete command or Go test oracle"})
	}
	if input.Manifest.Binding.ReviewKind == coverage.ReviewBlackbox {
		expected := protocol.BlackboxCasesMarkdown("candidate", input.Manifest)
		if body := readInstalledFile(d.t, filepath.Join(input.Workspace.Path, ".gates", "cases", "blackbox.md")); string(body) != expected {
			d.t.Fatal("isolated blackbox mirror does not match review manifest")
		}
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &result})
	if input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
		// After the reviewed artifact is immutable, opposite mutable bytes must not
		// influence the assembled candidate. Runtime residue is not another input.
		d.write(d.authoring, "mode_test.go", "package main\n", 0644)
		if err := os.Chmod(filepath.Join(d.authoring, "mode_test.go"), 0644); err != nil {
			d.t.Fatal(err)
		}
		d.write(d.authoring, "remove_test.go", "package main\n", 0644)
	}
}
func (d *candidateDriver) assertTargetUnchanged() {
	d.t.Helper()
	if head := d.p.git(d.t, "rev-parse", "HEAD"); head != d.p.base {
		d.t.Fatalf("validation moved target HEAD: %s", head)
	}
	if d.p.git(d.t, "write-tree") != d.p.git(d.t, "rev-parse", d.p.base+"^{tree}") {
		d.t.Fatal("validation changed the target index")
	}
	if out := d.p.git(d.t, "diff", "HEAD", "--"); out != "" {
		d.t.Fatalf("validation changed target/index: %s", out)
	}
}
func (d *candidateDriver) untilValidation() {
	for n := 0; n < 30; n++ {
		if d.run.Validation != nil && d.run.Validation.Candidate != nil {
			d.candidate = d.run.Validation.Candidate.Identity
			d.candidateTree = d.run.Validation.Candidate.Digest
			d.assertTargetUnchanged()
			return
		}
		var ids []string
		for id := range d.run.ValidationInputs {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		if len(ids) == 0 {
			d.t.Fatalf("candidate progression has no semantic worker: %+v", d.run.Next)
		}
		for _, id := range ids {
			input := d.run.ValidationInputs[id]
			switch input.Handler {
			case "engine.development.batch":
				d.develop(id, input)
			case "engine.qa.design":
				d.design(id, input)
			case "engine.qa.review.rationale":
				d.caseReview(id, input)
			default:
				d.t.Fatalf("unexpected worker %s", input.Handler)
			}
		}
		d.drive()
		d.assertTargetUnchanged()
	}
	d.t.Fatal("candidate did not freeze")
}
func (d *candidateDriver) execute() {
	for n := 0; n < 12 && d.run.Status != "COMPLETE"; n++ {
		d.executeWorkers()
		d.drive()
	}
	if d.run.Status != "COMPLETE" {
		d.t.Fatalf("run failed to complete: %s %+v", d.run.Status, d.run.Next)
	}
}
func (d *candidateDriver) executeWorkers() {
	var ids []string
	for id := range d.run.ValidationInputs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		input := d.run.ValidationInputs[id]
		if input.Binding.Candidate != d.candidate {
			d.t.Fatal("execution is not bound to exact Qn")
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if input.Handler == "engine.qa.execution" {
			if input.Execution == nil || input.Execution.Candidate.Identity != d.candidate || input.Whitelist == nil {
				d.t.Fatal("wrong execution method or input identity")
			}
			report := coverage.ExecutionReport{Binding: *input.Execution}
			for _, fact := range input.ExecutionFacts {
				if !slices.Contains(input.PendingCases, fact.CaseID) {
					continue
				}
				item := slices.IndexFunc(input.Manifest.Cases, func(item coverage.AcceptanceCase) bool { return item.CaseID == fact.CaseID })
				if item < 0 || len(fact.Operations) == 0 {
					d.t.Fatal("native facts have no approved case")
				}
				last := fact.Operations[len(fact.Operations)-1]
				if input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
					if last.Evidence.GoResult != "pass" || last.Evidence.ExitCode != 0 {
						d.t.Fatalf("Qn precise Go case failed: %+v", last.Evidence)
					}
				} else {
					accepted := last.Evidence.ExitCode == 0
					if input.Manifest.Cases[item].Scenario == "FAILURE" {
						accepted = last.Evidence.ExitCode != 0
					}
					if len(fact.Operations) != 2 || fact.Operations[0].Evidence.ExitCode != 0 || !accepted {
						d.t.Fatalf("Qn public input case failed: %+v", fact)
					}
				}
				procedure, observation, err := fact.Render()
				if err != nil {
					d.t.Fatal(err)
				}
				for _, edge := range input.Whitelist.Entries {
					if edge.CaseID == fact.CaseID {
						report.Records = append(report.Records, coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID, Procedure: procedure, Observation: observation, Comparison: "Actual command exit or exact Go run/pass matches the approved input oracle", Provenance: coverage.ProvenanceExecuted, Result: coverage.ExecutedPass})
					}
				}
				report.Binding.ActualCaseIDs = append(report.Binding.ActualCaseIDs, fact.CaseID)
			}
			if len(report.Records) != len(input.Whitelist.Entries) {
				d.t.Fatal("approved execution edges lack complete native facts")
			}
			result.Execution = &report
			result.Tool, result.Environment = protocol.ExecutionIdentities(input.ExecutionFacts)
		} else if input.Handler == "engine.gate" {
			cmd := exec.Command("go", "test", "./...")
			cmd.Dir = input.Workspace.Path
			if out, err := cmd.CombinedOutput(); err != nil {
				d.t.Fatalf("Qn gate Go checks: %v %s", err, out)
			}
		} else {
			d.t.Fatalf("unexpected final worker %s", input.Handler)
		}
		d.business(id, result)
	}
}
func (d *candidateDriver) assertClean() {
	d.t.Helper()
	head := d.p.git(d.t, "rev-parse", "HEAD")
	if tree := d.p.git(d.t, "rev-parse", head+"^{tree}"); tree != d.candidateTree {
		d.t.Fatalf("final native tree %s != validated %s", tree, d.candidateTree)
	}
	if count := d.p.git(d.t, "rev-list", "--count", d.p.base+".."+head); count != "1" {
		d.t.Fatalf("delivery commit count %s", count)
	}
	state := d.run.Termination
	if state == nil || !state.ResourcesCleaned() {
		d.t.Fatal("terminal summary lacks resource cleanup proof")
	}
	for _, resource := range state.NativeResources {
		if resource.Status != "CLEANED" {
			d.t.Fatalf("unclean resource %+v", resource)
		}
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			d.t.Fatalf("resource directory remains: %v", err)
		}
		if ref := d.p.git(d.t, "for-each-ref", "--format=%(objectname)", resource.Ref); ref != "" {
			d.t.Fatalf("resource ref remains %s", ref)
		}
	}
	trees := d.p.git(d.t, "worktree", "list", "--porcelain")
	d.t.Logf("native cleanup worktrees=%s refs=%s Qn entries=\n%s\nF entries=\n%s", trees, d.p.git(d.t, "for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/"), d.p.git(d.t, "ls-tree", "-r", "--full-tree", d.candidate), d.p.git(d.t, "ls-tree", "-r", "--full-tree", head))
	if strings.Count(trees, "worktree ") != 1 {
		d.t.Fatalf("native worktree residuals: %s", trees)
	}
	var cleanup int
	for _, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation == "op.cleanup" && receipt.Status == protocol.HostActionStatusExecuted {
			cleanup++
		}
	}
	if cleanup != 1 {
		d.t.Fatalf("cleanup typed receipts %d", cleanup)
	}
	if d.mode == "whitebox" || d.mode == "both" {
		for _, path := range []string{"remove_test.go", "old_test.go"} {
			if out := d.p.git(d.t, "ls-tree", head, "--", path); out != "" {
				d.t.Fatalf("deleted artifact path survived: %s", out)
			}
		}
		for _, path := range []string{"candidate_test.go", "renamed_test.go"} {
			if out := d.p.git(d.t, "ls-tree", head, "--", path); out == "" {
				d.t.Fatalf("artifact path missing: %s", path)
			}
		}
		if out := d.p.git(d.t, "ls-tree", head, "--", "mode_test.go"); !strings.HasPrefix(out, "100755 ") {
			d.t.Fatalf("native executable mode lost: %s", out)
		}
	}
	if d.mode == "blackbox" || d.mode == "both" {
		manifest := state.Validation.Sets[coverage.ReviewBlackbox].Manifest
		expected := protocol.BlackboxCasesMarkdown(state.RunID, manifest)
		if body := string(readInstalledFile(d.t, filepath.Join(d.p.project, ".gates", "results", state.RunID+".blackbox-cases.md"))); body != expected {
			d.t.Fatalf("tracked blackbox artifact differs from approved manifest")
		}
	}
	raw, _ := json.Marshal(state)
	d.t.Logf("terminal native proof: base=%s Qn=%s tree=%s F=%s resources/state/receipts=%s", d.p.base, d.candidate, d.candidateTree, head, raw)
	summaryPath := d.run.SummaryPath
	before := readInstalledFile(d.t, summaryPath)
	d.drive()
	after := readInstalledFile(d.t, summaryPath)
	if string(before) != string(after) {
		d.t.Fatal("terminal replay changed summary")
	}
	d.assertAcceptedReplay(true)
}

// Replaying accepted business facts must preserve every receipt and durable
// byte, including events accepted before the final gate or abort decision.
func (d *candidateDriver) assertAcceptedReplay(terminal bool) {
	d.t.Helper()
	dir := filepath.Join(d.p.project, facade.EngineNamespace, "candidate")
	before := map[string][]byte{}
	for _, name := range []string{"state.json", "terminal-summary.intent.json", "terminal-summary.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			d.t.Fatal(err)
		}
		before[name] = data
	}
	head, index := d.p.git(d.t, "rev-parse", "HEAD"), d.p.git(d.t, "write-tree")
	var trace []byte
	if d.breakLog != "" {
		trace = readInstalledFile(d.t, d.breakLog)
	}
	for _, original := range d.accepted {
		out := d.call(installedJSON(d.t, original.event), "workflow", "submit", "--root", d.p.project, "--run-id", "candidate")
		var response struct {
			Acceptance protocol.Acceptance `json:"acceptance"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			d.t.Fatal(err)
		}
		if !reflect.DeepEqual(response.Acceptance, original.receipt) {
			d.t.Fatalf("accepted %s receipt changed: %+v != %+v", original.event.ID, response.Acceptance, original.receipt)
		}
		if terminal {
			digest, err := original.event.Digest()
			if err != nil {
				d.t.Fatal(err)
			}
			record := d.run.Termination.Events[string(original.event.ID)]
			if record.Digest != digest || !reflect.DeepEqual(record.Acceptance, original.receipt) {
				d.t.Fatalf("terminal event record changed: %+v", record)
			}
		}
	}
	if terminal && len(d.accepted) > 0 {
		var summary facade.TerminalSummary
		if err := json.Unmarshal(before["terminal-summary.json"], &summary); err != nil {
			d.t.Fatal(err)
		}
		last := d.accepted[len(d.accepted)-1]
		if summary.LastEvent == nil || !reflect.DeepEqual(summary.LastEvent.Acceptance, last.receipt) {
			d.t.Fatal("native cleanup replaced the frozen terminal business receipt")
		}
		conflict, err := protocol.NewRequestEvent(last.event.ID, protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop"})
		if err != nil {
			d.t.Fatal(err)
		}
		unknown := last.event
		unknown.ID = d.id()
		for _, item := range []struct {
			event protocol.Event
			code  string
		}{{conflict, string(protocol.CodeDuplicateEventMismatch)}, {unknown, string(protocol.CodeNotInitialized)}} {
			out, err := d.p.command(installedJSON(d.t, item.event), "workflow", "submit", "--root", d.p.project, "--run-id", "candidate").CombinedOutput()
			if err == nil || !strings.Contains(string(out), item.code) {
				d.t.Fatalf("terminal rejection expected %s: %v %s", item.code, err, out)
			}
		}
	}
	for name, data := range before {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			d.t.Fatal(err)
		}
		if !reflect.DeepEqual(data, after) {
			d.t.Fatalf("replay changed %s", name)
		}
	}
	if d.p.git(d.t, "rev-parse", "HEAD") != head || d.p.git(d.t, "write-tree") != index {
		d.t.Fatal("event replay mutated native content")
	}
	if d.breakLog != "" && string(trace) != string(readInstalledFile(d.t, d.breakLog)) {
		d.t.Fatal("event replay performed a native command")
	}
}
func TestInstalledCandidateLifecycle(t *testing.T) {
	for _, mode := range []string{"none", "blackbox", "whitebox", "both"} {
		t.Run(mode, func(t *testing.T) {
			d := newCandidateDriver(t, mode)
			d.reviews()
			d.untilValidation()
			d.execute()
			d.assertClean()
		})
	}
}

// breakNative wraps the actual Git command in the installed process. It kills
// that process only after the selected native write has returned successfully.
func (d *candidateDriver) breakNative(stage string) {
	d.t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		d.t.Fatal(err)
	}
	dir := d.t.TempDir()
	d.breakMarker = filepath.Join(dir, "interrupted")
	d.breakLog = filepath.Join(dir, "native.log")
	d.breakStage = stage
	script := `#!/bin/sh
"$CANDIDATE_NATIVE_GIT" "$@"
result=$?
printf '%s' "$PWD" >> "$CANDIDATE_NATIVE_LOG"
printf '\t%s' "$@" >> "$CANDIDATE_NATIVE_LOG"
printf '\n' >> "$CANDIDATE_NATIVE_LOG"
match=false
case "$CANDIDATE_NATIVE_STAGE:$1:$2" in
 workspace:worktree:add|cleanup:worktree:remove|promotion:merge:--ff-only|squash:reset:--soft|track:add:--all|pin:update-ref:*|artifact:restore:--source=*) match=true ;;
 freeze:commit:-m) case "$3" in "formal-gates candidate "*) match=true ;; esac ;;
 delivery:commit:-m) case "$3" in "formal-gates delivery "*) match=true ;; esac ;;
esac
if [ "$result" -eq 0 ] && [ "$match" = true ] && [ ! -f "$CANDIDATE_NATIVE_MARKER" ]; then
 : > "$CANDIDATE_NATIVE_MARKER"
 kill -KILL "$PPID"
fi
exit "$result"
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755); err != nil {
		d.t.Fatal(err)
	}
	d.env = []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"), "CANDIDATE_NATIVE_GIT=" + realGit, "CANDIDATE_NATIVE_LOG=" + d.breakLog, "CANDIDATE_NATIVE_STAGE=" + stage, "CANDIDATE_NATIVE_MARKER=" + d.breakMarker}
}
func TestInstalledCandidateResumesNativeWrites(t *testing.T) {
	for _, stage := range []string{"workspace", "track", "artifact", "pin", "unknown", "freeze", "promotion", "squash", "delivery", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			d := newCandidateDriver(t, "both")
			if stage == "unknown" {
				d.breakNative("pin")
				d.unknownReceipt = true
			} else {
				d.breakNative(stage)
			}
			d.reviews()
			d.untilValidation()
			d.execute()
			d.assertClean()
			if stage == "unknown" && (d.unknownAction == "" || d.run.Termination.HostActionReceipts[d.unknownAction].Status != protocol.HostActionStatusExecuted) {
				t.Fatal("unknown native responsibility did not reconcile through its original receipt")
			}
			if _, err := os.Stat(d.breakMarker); err != nil {
				t.Fatalf("native boundary %s was not exercised: %v", stage, err)
			}
			native := string(readInstalledFile(t, d.breakLog))
			t.Logf("raw native command trace:\n%s", native)
			// Successful recovery must not repeat promotion or either squash prefix.
			for _, command := range []string{"\tmerge\t--ff-only\t", "\treset\t--soft\t"} {
				if strings.Count(native, command) != 1 {
					t.Fatalf("native prefix count for %q: %d", command, strings.Count(native, command))
				}
			}
		})
	}
}
func TestInstalledCandidateConcurrentDrive(t *testing.T) {
	d := newCandidateDriver(t, "both")
	d.reviews()
	for id, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch" {
			d.develop(id, input)
		} else if input.Handler == "engine.qa.design" {
			d.design(id, input)
		}
	}
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			cmd := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", "candidate")
			out, err := cmd.CombinedOutput()
			if err != nil && !strings.Contains(string(out), "REVISION_CONFLICT") && !strings.Contains(string(out), "LOCK_HELD") {
				t.Errorf("concurrent public drive: %v %s", err, out)
			}
		}()
	}
	group.Wait()
	d.drive()
	d.untilValidation()
	d.execute()
	d.assertClean()
}

func TestInstalledAcceptedEventsReplayDuringTerminalCleanup(t *testing.T) {
	d := newCandidateDriver(t, "none")
	d.reviews()
	d.untilValidation()
	d.executeWorkers()
	if len(d.accepted) < 2 {
		t.Fatal("missing accepted business facts")
	}
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		out, err := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", "candidate").CombinedOutput()
		if err != nil {
			t.Errorf("parallel terminal drive: %v %s", err, out)
		}
	}()
	for _, original := range []struct {
		event   protocol.Event
		receipt protocol.Acceptance
	}{d.accepted[0], d.accepted[len(d.accepted)-1]} {
		group.Add(1)
		go func(event protocol.Event, receipt protocol.Acceptance) {
			defer group.Done()
			raw, err := d.p.command(installedJSON(t, event), "workflow", "submit", "--root", d.p.project, "--run-id", "candidate").CombinedOutput()
			if err != nil {
				t.Errorf("parallel accepted replay: %v %s", err, raw)
				return
			}
			var response struct {
				Acceptance protocol.Acceptance `json:"acceptance"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(installedJSON(t, receipt), installedJSON(t, response.Acceptance)) {
				t.Errorf("parallel event %s receipt changed", event.ID)
			}
		}(original.event, original.receipt)
	}
	group.Wait()
	d.drive()
	if d.run.Status != "COMPLETE" {
		t.Fatalf("terminal cleanup did not complete: %s", d.run.Status)
	}
	d.assertClean()
}
func TestInstalledCandidateAbortCleansRegisteredResources(t *testing.T) {
	d := newCandidateDriver(t, "both")
	d.reviews()
	// A transport can truthfully report that an unsigned worker never started.
	for id := range d.run.PendingActions {
		event, err := protocol.NewSpawnReceiptWithFailureClass(d.id(), id, "codex", id, protocol.SpawnStatusFailed, authoring.FailureBusinessReject)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(event)
	}
	request, err := protocol.NewRequestEvent(d.id(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	event, err := protocol.NewDecideEvent(d.id(), protocol.RequestID(request.ID), d.token(string(request.ID)), "abort")
	if err != nil {
		t.Fatal(err)
	}
	d.submit(event)
	d.drive()
	if d.run.Status != "ABORTED" || d.run.Termination == nil || !d.run.Termination.ResourcesCleaned() {
		t.Fatalf("abort did not clean native resources: %+v", d.run)
	}
	d.assertTargetUnchanged()
	for _, r := range d.run.Termination.NativeResources {
		if _, err := os.Stat(r.Path); !os.IsNotExist(err) {
			t.Fatalf("aborted resource remains: %s %v", r.Path, err)
		}
		if ref := d.p.git(t, "for-each-ref", "--format=%(objectname)", r.Ref); ref != "" {
			t.Fatalf("aborted ref remains: %s", ref)
		}
	}
	t.Logf("abort typed resource proof: %+v", d.run.Termination)
	d.assertAcceptedReplay(true)
}

func (d *candidateDriver) expectNativeError(fragment string) {
	d.t.Helper()
	cmd := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", "candidate")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), fragment) {
		d.t.Fatalf("native boundary accepted invalid content: %v %s", err, out)
	}
	d.t.Logf("native rejection: argv=%v exit=%v raw=%s", cmd.Args, err, out)
	d.assertTargetUnchanged()
}
func TestInstalledCandidateRejectsUndeclaredContent(t *testing.T) {
	d := newCandidateDriver(t, "none")
	d.reviews()
	for id, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch" {
			d.write(input.Workspace.Path, "notes.txt", "unrelated user content\n", 0644)
			d.develop(id, input)
			d.expectNativeError("undeclared change: notes.txt")
			if head := d.p.git(t, "-C", input.Workspace.Path, "rev-parse", "HEAD"); head != d.p.base {
				t.Fatal("invalid freeze moved its source HEAD")
			}
			if body := readInstalledFile(t, filepath.Join(input.Workspace.Path, "notes.txt")); string(body) != "unrelated user content\n" {
				t.Fatal("invalid freeze cleaned undeclared content")
			}
			return
		}
	}
	t.Fatal("missing development worker")
}
func TestInstalledWhiteboxRejectsProductionInput(t *testing.T) {
	d := newCandidateDriver(t, "whitebox")
	d.reviews()
	for n := 0; n < 6; n++ {
		for id, input := range d.run.ValidationInputs {
			if input.Handler == "engine.development.batch" {
				d.develop(id, input)
				continue
			}
			if input.Handler == "engine.qa.design" && input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				result := d.designResult(id, input)
				d.write(input.Workspace.Path, "app.go", "package main\nfunc main() {}\nfunc valid(s string) bool {return true}\n", 0644)
				result.Development.Paths = append(result.Development.Paths, "app.go")
				d.business(id, result)
				d.expectNativeError("whitebox change is not test-owned: app.go")
				if head := d.p.git(t, "-C", input.Workspace.Path, "rev-parse", "HEAD"); head != input.Workspace.Source {
					t.Fatal("invalid whitebox freeze moved Dn")
				}
				return
			}
		}
		d.drive()
	}
	t.Fatal("missing whitebox design worker")
}

func TestInstalledPromotionRejectsTargetChanges(t *testing.T) {
	for _, change := range []string{"dirty", "drift"} {
		t.Run(change, func(t *testing.T) {
			d := newCandidateDriver(t, "none")
			d.reviews()
			d.untilValidation()
			d.executeWorkers()
			d.write(d.p.project, "app.go", "package main\nfunc main() {println(\"ordinary user change\")}\n", 0644)
			expectedError := "unsubmitted git changes"
			if change == "drift" {
				d.p.git(t, "add", "--", "app.go")
				d.p.git(t, "commit", "-qm", "Concurrent user change")
				expectedError = "target drifted"
			}
			head := d.p.git(t, "rev-parse", "HEAD")
			index := d.p.git(t, "write-tree")
			body := readInstalledFile(t, filepath.Join(d.p.project, "app.go"))
			cmd := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", "candidate")
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), expectedError) {
				t.Fatalf("target boundary accepted %s: %v %s", change, err, out)
			}
			if d.p.git(t, "rev-parse", "HEAD") != head || d.p.git(t, "write-tree") != index || string(readInstalledFile(t, filepath.Join(d.p.project, "app.go"))) != string(body) {
				t.Fatal("rejected promotion changed user HEAD/index/content")
			}
			t.Logf("target %s rejection: argv=%v exit=%v raw=%s head=%s index=%s body=%s", change, cmd.Args, err, out, head, index, body)
		})
	}
}

func TestInstalledCandidatePreservesUndeclaredIndexEdit(t *testing.T) {
	d := newCandidateDriver(t, "none")
	d.reviews()
	for id, input := range d.run.ValidationInputs {
		if input.Handler != "engine.development.batch" {
			continue
		}
		root := input.Workspace.Path
		original := readInstalledFile(t, filepath.Join(root, "mode_test.go"))
		d.write(root, "mode_test.go", "package main\n// ordinary staged user edit\n", 0644)
		d.p.git(t, "-C", root, "add", "--", "mode_test.go")
		d.write(root, "mode_test.go", string(original), 0644)
		if net := d.p.git(t, "-C", root, "diff", "HEAD", "--", "mode_test.go"); net != "" {
			t.Fatal("fixture working bytes are not restored")
		}
		head, index := d.p.git(t, "-C", root, "rev-parse", "HEAD"), d.p.git(t, "-C", root, "write-tree")
		d.develop(id, input)
		d.expectNativeError("undeclared change: mode_test.go")
		if d.p.git(t, "-C", root, "rev-parse", "HEAD") != head || d.p.git(t, "-C", root, "write-tree") != index || string(readInstalledFile(t, filepath.Join(root, "mode_test.go"))) != string(original) {
			t.Fatal("rejected declaration changed native user state")
		}
		return
	}
	t.Fatal("missing development dispatch")
}

func TestInstalledGateRetainsChangedProductionEmbed(t *testing.T) {
	d := newCandidateDriver(t, "none")
	d.reviews()
	d.implementation = "package main\nimport (\"os\"; _ \"embed\")\n//go:embed old_test.go\nvar content string\nfunc main(){if len(os.Args)!=2 || !valid(os.Args[1]){os.Exit(2)}}\nfunc valid(s string)bool{return s!=\"\"}\n"
	for id, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch" {
			d.write(input.Workspace.Path, "old_test.go", "package main\nimport \"testing\"\nfunc TestOld(t *testing.T) {t.Log(\"production embed\")}\n", 0644)
			d.develop(id, input, "old_test.go")
		}
	}
	d.drive()
	d.untilValidation()
	found := false
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.gate" {
			found = true
			if input.Gate == nil || slices.Contains(input.Gate.TestOwnedPaths, "old_test.go") {
				t.Fatalf("production build input removed from gate range: %+v", input.Gate)
			}
		}
	}
	if !found {
		t.Fatal("no selected gate input")
	}
	d.execute()
	d.assertClean()
}

func TestInstalledRequirementBarrierPreventsCandidateWrites(t *testing.T) {
	d := newCandidateDriver(t, "none")
	d.reviews()
	for id, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch" {
			d.develop(id, input)
		}
	}
	if len(d.run.AdapterIntents) == 0 {
		t.Fatal("no issued candidate responsibility")
	}
	request, err := protocol.NewRequestEvent(d.id(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	before := d.p.git(t, "worktree", "list", "--porcelain")
	d.drive()
	if d.run.PendingRequirementAsk == nil {
		t.Fatal("requirement barrier not visible")
	}
	if d.p.git(t, "worktree", "list", "--porcelain") != before {
		t.Fatal("barrier executed candidate write")
	}
	d.assertTargetUnchanged()
}
