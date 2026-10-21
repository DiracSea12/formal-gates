package facade

import (
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/requirement"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These positive fixtures produce separate semantic evidence for each issued
// point. Negative payloads go directly through submit without these builders.
func fixtureImplementedPoints(points []requirement.Point, behavior string) []requirement.PointResult {
	results := make([]requirement.PointResult, 0, len(points))
	for _, point := range points {
		results = append(results, requirement.PointResult{PointID: point.ID, Status: "PASS", Evidence: point.ID + ": " + point.Text + "; implementation fixture: " + behavior})
	}
	return results
}
func fixtureGatePointDecisions(points []requirement.Point, candidate string, findings []review.Finding) []protocol.RequirementDecision {
	decisions := make([]protocol.RequirementDecision, 0, len(points))
	for _, point := range points {
		decision := protocol.RequirementDecision{PointID: point.ID, Status: "PASS", Evidence: point.ID + ": independently compared " + point.Text + " with the command behavior in fixture candidate " + candidate}
		for index, finding := range findings {
			if finding.Requirement == point.ID {
				decision.Status = "FAIL"
				decision.Evidence = point.ID + ": gate fixture compared " + point.Text + " with " + finding.ActualError + "; " + finding.Message
				decision.FindingIndexes = append(decision.FindingIndexes, index)
			}
		}
		decisions = append(decisions, decision)
	}
	return decisions
}
func fixturePlanRequirements(plan *protocol.GranularityReview, points []requirement.Point) {
	for index := range plan.Batches {
		if plan.Batches[index].RequirementPoints == nil {
			plan.Batches[index].RequirementPoints = requirement.PointIDs(points)
		}
	}
}

// Checkpoint review covers completed contributions; final review covers the
// confirmed project even when no QA gates are selected.
func TestCheckpointGateUsesCompletedRequirementPoints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	snapshotRepoCommits(t, root)
	document := strings.Replace(validationDocument, "- AC-001：正常输入成功，错误输入拒绝。", "- AC-001：正常输入成功。\n- AC-002：错误输入拒绝。", 1)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	runner := vcs.ExecRunner{}
	for _, args := range [][]string{{"add", "requirements.md"}, {"commit", "-qm", "Separate input outcomes"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	base, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	ensureEditableConfiguration(t, root)
	f, run, err := Start(StartOptions{Root: root, Capacity: 16, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider), Request: StartRequest{RunID: "validation", Provider: DefaultProvider, BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: preview.Revision, provider: f.admission.Provider, git: f.vcs}
	retainNativeEvidence(t, f)
	d.drive()
	plan := commandPlan()
	first := plan.Batches[0]
	first.ID, first.RequirementPoints = "accept", []string{"AC-001"}
	first.Deliverable = "Accept valid command input"
	first.Subtasks = []protocol.DevelopmentSubtask{{ID: "accept-input", Description: first.Deliverable}}
	second := plan.Batches[0]
	second.ID, second.RequirementPoints = "reject", []string{"AC-002"}
	second.Deliverable = "Reject invalid command input"
	second.Subtasks = []protocol.DevelopmentSubtask{{ID: "reject-input", Description: second.Deliverable}}
	second.DependsOn = []string{"accept"}
	plan.Batches = []protocol.DevelopmentBatch{first, second}
	plan.VerificationTier, plan.CheckpointReason = "CHECKPOINT", "Valid input is reviewable before adding rejection"
	plan.CheckpointGroups = [][]string{{"accept"}, {"reject"}}
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command with two input contributions"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	action, input := planBatchAction(d, "accept")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() {}\n// Valid input returns successfully.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.materialize()
	d.materialize()
	gate, gin := d.gateAction("quality")
	wi, ok := driverState(t, d).WorkerInput(gate, d.f.definition)
	if !ok {
		t.Fatal("checkpoint has no signed input")
	}
	t.Logf("checkpoint issued points: %+v; original gate: %+v", wi.RequirementPoints, gin.Gate)
	if !reflect.DeepEqual(gin.Gate.Batches, []protocol.DevelopmentBatch{first}) {
		t.Fatalf("checkpoint lost original completed batch contract: %+v", gin.Gate.Batches)
	}
	if !reflect.DeepEqual(requirement.PointIDs(wi.RequirementPoints), []string{"AC-001"}) {
		t.Fatalf("checkpoint demanded unimplemented contribution: %+v", wi.RequirementPoints)
	}
	d.reopen()
	reopened, ok := driverState(t, d).WorkerInput(gate, d.f.definition)
	if !ok || !reflect.DeepEqual(wi, reopened) {
		t.Fatal("checkpoint scope changed on reopen")
	}
	d.business(gate, protocol.ValidationResult{Binding: gin.Binding, Status: "PASS"})
	d.drive()
	action, input = planBatchAction(d, "reject")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nimport \"os\"\nfunc main() {if len(os.Args)>1 && os.Args[len(os.Args)-1]==\"invalid\" {os.Exit(2)}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.materialize()
	d.materialize()
	gate, gin = d.gateAction("quality")
	wi, ok = driverState(t, d).WorkerInput(gate, d.f.definition)
	if !ok || !reflect.DeepEqual(requirement.PointIDs(wi.RequirementPoints), []string{"AC-001", "AC-002"}) {
		t.Fatalf("final review lost confirmed project scope: %+v", wi.RequirementPoints)
	}
	if len(gin.Gate.Batches) != 0 {
		t.Fatal("final gate retained intermediate scope", gin.Gate.Batches)
	}
	d.business(gate, protocol.ValidationResult{Binding: gin.Binding, Status: "PASS"})
	d.drive()
	if wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; wave.Status != "COMPLETE" || len(wave.CheckpointTasks) != 0 {
		t.Fatal("final point review did not complete original gate", wave)
	}
}

// A verification-only contribution preserves the candidate while requiring a
// final review of all responsibilities beyond the preceding checkpoint.
func TestFinalGateReviewsCompleteScopeForUnchangedCandidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	p := commandPlan()
	first := p.Batches[0]
	first.ID = "implementation"
	first.Checks[0].Command = fmt.Sprintf("go build -o %q ./...", filepath.Join(t.TempDir(), "app"))
	second := first
	second.ID = "verification"
	second.Subtasks = []protocol.DevelopmentSubtask{{ID: "verify-command", Description: "Verify the completed public command"}}
	second.DependsOn = []string{first.ID}
	second.Checks = append([]protocol.DevelopmentCheck(nil), first.Checks...)
	second.Checks[0].Command = ""
	second.Checks[0].Goal = "Build the complete delivered command"
	p.Batches = []protocol.DevelopmentBatch{first, second}
	p.VerificationTier = "CHECKPOINT"
	p.CheckpointGroups = [][]string{{first.ID}, {second.ID}}
	p.CheckpointReason = "Implementation precedes final complete verification"
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, p, "quality")
	id, in := planBatchAction(d, first.ID)
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app.go"), []byte("package main // implemented\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	d.drive()
	gid, gin := d.gateAction("quality")
	if len(gin.Gate.Batches) != 1 {
		t.Fatal("checkpoint did not freeze original contribution")
	}
	d.business(gid, protocol.ValidationResult{Binding: gin.Binding, Status: "PASS"})
	d.drive()
	prior := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	id, in = planBatchAction(d, second.ID)
	evidence := batchEvidence(in)
	evidence.Paths = []string{}
	evidence.Commands = []string{first.Checks[0].Command}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: evidence})
	d.drive()
	final := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	t.Logf("checkpoint=%s candidate=%+v tasks=%d; final=%s candidate=%+v tasks=%d status=%s", prior.ID, prior.Candidate, len(prior.CheckpointTasks), final.ID, final.Candidate, len(final.CheckpointTasks), final.Status)
	for _, target := range final.Targets {
		t.Logf("gate=%s status=%s resultAction=%s checkpointAction=%s", target.Gate, target.Status, target.ResultAction, gid)
	}
	if final.Candidate != prior.Candidate {
		t.Fatal("verification-only contribution changed the candidate")
	}
	if len(final.CheckpointTasks) != 0 {
		t.Fatal("final gate retained checkpoint task scope")
	}
	for _, target := range final.Targets {
		if target.ResultAction == gid && target.Status == "PASS" {
			t.Fatal("final full responsibility reused partial checkpoint result without final review")
		}
	}

	fresh, finalInput := d.gateAction("quality")
	if fresh == gid || len(finalInput.Gate.Batches) != 0 {
		t.Fatal("complete final responsibility was not freshly issued", fresh, finalInput.Gate)
	}
	original, issued := driverState(t, d).WorkerInput(fresh, d.f.definition)
	if !issued {
		t.Fatal("final gate has no original signed input")
	}
	d.reopen()
	recovered, issued := driverState(t, d).WorkerInput(fresh, d.f.definition)
	if !issued || !reflect.DeepEqual(original, recovered) {
		t.Fatal("final responsibility changed across reopen")
	}
	d.business(fresh, protocol.ValidationResult{Binding: finalInput.Binding, Status: "PASS"})
	d.drive()
	completed := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if completed.Status != "COMPLETE" {
		t.Fatal("fresh final responsibility did not complete", completed)
	}
	for _, target := range completed.Targets {
		if target.ResultAction != fresh {
			t.Fatal("final gate retained checkpoint evidence", target)
		}
	}
}
