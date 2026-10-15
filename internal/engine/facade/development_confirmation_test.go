package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
)

// Requirement replacement must prepare its current plan before asking the user
// to confirm topology or route, while retaining original requests and evidence.
func TestRequirementChangeRetiresPendingPlanConfirmation(t *testing.T) {
	for _, control := range []protocol.ControlKind{protocol.ControlConfirmTopology, protocol.ControlSelectRoute} {
		t.Run(string(control), func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{
				RunID: "development-confirmation", Route: "regular", BaseSnapshot: base,
				FlowSelection: &protocol.FlowSelection{Disabled: []string{"product-review", "start-readiness", "qa", "gate", "repair"}}, IntakeConfirmationReceipt: intake.IntakeCandidate,
			}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
			if err != nil {
				t.Fatal(err)
			}
			d := &validationDriver{t: t, f: f, run: run}
			d.drive()
			originalAction, originalInput := d.action("engine.development.plan.v1")
			d.business(originalAction, protocol.ValidationResult{Binding: originalInput.Binding, Status: "PASS", GranularityReview: commandPlan()})
			confirm := func(choice string) ControlCandidate {
				t.Helper()
				candidate, err := d.f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: choice, Reason: "Confirm the displayed current plan"})
				if err != nil {
					t.Fatal(err)
				}
				_, d.run, err = d.f.SubmitControl(candidate)
				if err != nil {
					t.Fatal(err)
				}
				return candidate
			}
			if requested, err := d.f.RequestWorkflowChoice(); err != nil || !requested {
				t.Fatal("prepared plan did not offer topology", err)
			}
			d.run, err = d.f.Show()
			if err != nil {
				t.Fatal(err)
			}
			if control == protocol.ControlSelectRoute {
				confirm("no-split")
				if requested, err := d.f.RequestWorkflowChoice(); err != nil || !requested {
					t.Fatal("confirmed topology did not offer route", err)
				}
				d.run, err = d.f.Show()
				if err != nil {
					t.Fatal(err)
				}
			}
			originalAsk := d.run.PendingAsks[askPosition(t, d.run, control)-1]
			originalState := driverState(t, d)
			storedAsk, storedResult := originalState.PendingAsks[originalAsk.RequestID], originalState.Results[originalAction]

			request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(request)
			if err != nil {
				t.Fatal(err)
			}
			document := strings.Replace(validationDocument, "正常输入成功", "正常输入返回 HI", 1)
			if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			choices := make([]WorkChoice, len(d.run.RequirementWork))
			for i := range choices {
				choices[i] = WorkChoice{Disposition: protocol.RequirementWorkRedo, Reason: "The changed public output requires a current implementation plan"}
			}
			candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{
				Classification: protocol.RequirementClassificationChanged, Redo: []string{"route"}, Work: choices,
				Impact: protocol.RequirementImpact{ChangedItems: []string{"public command output"}, TransitiveImpacts: []string{"development plan"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(candidate)
			if err != nil {
				t.Fatal(err)
			}
			confirmation, err := d.f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm the complete changed output requirement and redo dispositions"})
			if err != nil {
				t.Fatal(err)
			}
			accepted, changed, err := d.f.SubmitControl(confirmation)
			if err != nil {
				t.Fatal(err)
			}
			d.run = changed
			if len(d.run.PendingAsks) != 0 || d.run.Next.Kind == decision.KindAsk {
				t.Fatalf("superseded %s Ask blocks current development preparation: asks=%+v next=%+v", control, d.run.PendingAsks, d.run.Next)
			}
			storedAsk.Resolved = true
			s := driverState(t, d)
			if !reflect.DeepEqual(s.PendingAsks[originalAsk.RequestID], storedAsk) {
				t.Fatalf("retirement changed original request: got=%+v want=%+v", s.PendingAsks[originalAsk.RequestID], storedAsk)
			}
			if !reflect.DeepEqual(s.ObsoleteResults[originalAction], protocol.WorkerResultHistory{storedResult}) {
				t.Fatalf("retirement changed worker evidence: got=%+v want=%+v", s.ObsoleteResults[originalAction], storedResult)
			}
			if _, decided := s.Decisions[originalAsk.RequestID]; decided {
				t.Fatal("retirement fabricated a user decision")
			}
			if _, err := d.f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "no-split"}); err == nil {
				t.Fatal("superseded confirmation was prepared before current plan completion")
			}
			if requested, err := d.f.RequestWorkflowChoice(); err != nil || requested {
				t.Fatal("confirmation opened before current plan completion", err)
			}
			freshAction, freshInput := d.action("engine.development.plan.v1")
			if freshAction == originalAction || freshInput.Binding.Task == originalInput.Binding.Task || freshInput.Binding.Generation <= originalInput.Binding.Generation || freshInput.Binding.RequirementRevision != confirmation.Event.Decide.Requirement.CandidateRevision {
				t.Fatal("replacement preparation lost its new confirmation binding", freshInput.Binding)
			}
			material := d.run.WorkerMaterials[freshAction]
			d.reopen()
			if replay, replayed, err := d.f.SubmitControl(confirmation); err != nil || !reflect.DeepEqual(replay, accepted) || !reflect.DeepEqual(replayed.WorkerMaterials, d.run.WorkerMaterials) {
				t.Fatal("repeated confirmed change replayed preparation", err)
			}
			d.drive()
			if d.run.WorkerMaterials[freshAction] != material || len(d.run.WorkerMaterials) != 1 || len(d.run.PendingAsks) != 0 || d.run.Validation.Document.Content != document {
				t.Fatal("same-run recovery changed current preparation or exposed superseded confirmation")
			}
			plan := commandPlan()
			plan.Batches[0].Deliverable = "Command returns HI for valid input and rejects invalid input"
			d.business(freshAction, protocol.ValidationResult{Binding: freshInput.Binding, Status: "PASS", GranularityReview: plan})
			if requested, err := d.f.RequestWorkflowChoice(); err != nil || !requested {
				t.Fatal("current completed plan did not offer a fresh confirmation", err)
			}
			d.run, err = d.f.Show()
			if err != nil {
				t.Fatal(err)
			}
			if ask := d.run.PendingAsks[0]; ask.Control != control || ask.RequestID == originalAsk.RequestID || !driverState(t, d).PendingAsks[originalAsk.RequestID].Resolved {
				t.Fatal("completed preparation reused its superseded confirmation", ask)
			}
			if control == protocol.ControlConfirmTopology {
				confirm("no-split")
				if requested, err := d.f.RequestWorkflowChoice(); err != nil || !requested {
					t.Fatal("fresh topology did not offer the current route", err)
				}
			}
			current := confirm("custom")
			if current.DevelopmentPlan == nil || current.DevelopmentPlan.Confirmation.ReviewActionID != freshAction || !reflect.DeepEqual(current.DevelopmentPlan.Proposal, *plan) {
				t.Fatal("route confirmation did not bind the exact current preparation")
			}
		})
	}
}
