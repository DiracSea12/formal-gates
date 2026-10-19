package facade

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/vcs"
)

// Complete confirmation preserves carried child evidence across recovery;
// a pending parent review still prevents delivery.
func TestChildConfirmedParentDocumentPreservesRecoveryAndReviewBoundary(t *testing.T) {
	t.Parallel()
	for _, reviewPending := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent-review-pending-%t", reviewPending), func(t *testing.T) {
			master, child, _, ready := deliveredChildCheckpointAt(t, testkit.ShortRoot(t), validationDocument)
			physical := driverState(t, child)
			document := validationDocument + "\n\n"
			if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			children := requirementChildChoices(t, master)
			for i := range children {
				if children[i].ChildRunID == child.f.runID {
					children[i].ReceiptDisposition, children[i].CandidateDisposition = protocol.RequirementWorkRedo, protocol.RequirementWorkRedo
				}
			}
			intake, err := prepareConfirmedIntake(master.f.root, master.run.IntakeConfirmationReceipt, true)
			if err != nil {
				t.Fatal(err)
			}
			pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:confirmed-document", ImpactDigest: "sha256:current-child-material", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: reviewPending, Children: children, Impact: &protocol.RequirementImpact{ChangedItems: []string{"confirmed document"}, TransitiveImpacts: []string{"current child candidate and receipt"}, CarryReasons: []string{"unchanged child development and gate inputs"}}}
			for _, work := range master.run.RequirementWork {
				pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master inputs"})
			}
			request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm current child material"})
			if err != nil {
				t.Fatal(err)
			}
			master.submit(request)
			ask := master.run.PendingRequirementAsk
			master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, Children: children, Impact: pkg.Impact, RedoStartReadiness: reviewPending, Reason: "redo current child material and carry the unchanged gate inputs"}}}.WithRequirementDocument(document))
			for range 2 {
				child.reopen()
				child.drive()
				current := driverState(t, child)
				if current.RequirementRevision() != revision || current.Validation.RequirementRevision != revision || current.Validation.Document.Content != document {
					t.Fatal("recovery lost the complete confirmed parent document")
				}
				if current.SliceChild.RequirementRevision != physical.SliceChild.RequirementRevision || !reflect.DeepEqual(current.IntakeConfirmationReceipt, physical.IntakeConfirmationReceipt) || !reflect.DeepEqual(current.SpawnReceipts, physical.SpawnReceipts) || !reflect.DeepEqual(current.Results, physical.Results) {
					t.Fatal("confirmation and recovery changed child origin or carried execution facts")
				}
				if reviewPending && (current.FinalValidationComplete(child.f.definition) || current.DeliveryComplete(child.f.definition) || current.SliceReceipt != nil || current.AcceptedNativeSnapshot() != physical.AcceptedNativeSnapshot() || !reflect.DeepEqual(current.CandidatePromotion, physical.CandidatePromotion)) {
					t.Fatal("child crossed its actual pending parent review boundary")
				}
			}
			if reviewPending {
				master.drive()
				child.reopen()
				child.drive()
				if !driverState(t, child).SliceChild.ParentReviewPending || child.run.Status == string(runtime.PhaseSliceReady) {
					t.Fatal("current document bypassed pending parent review")
				}
				id := ""
				for key, pending := range master.run.PendingActions {
					if pending.ReviewSeries == review.ProductReview {
						t.Fatal("unchanged product review was reopened")
					}
					if pending.ReviewSeries == review.StartReadiness {
						id = key
					}
				}
				if id == "" {
					t.Fatal("missing current start-readiness dispatch")
				}
				master.receipt(id)
				input, issued := driverState(t, master).WorkerInput(id, master.f.definition)
				if !issued {
					t.Fatal("parent review lacks its current signed input")
				}
				plan := splitCommandPlan()
				fixturePlanRequirements(plan, input.RequirementPoints)
				result, err := protocol.NewReviewWorkerResultEvent(master.eventID(), id, master.host(), string(master.eventID()), protocol.ReviewResultEvidence{GranularityReview: plan, Action: review.StartReadiness, DispatchID: id, RequirementRevision: revision, Status: "PASS", Findings: []review.Finding{}})
				if err != nil {
					t.Fatal(err)
				}
				master.submit(result)
				verifyReviewFixture(t, master, result)
				master.drive()
			}
			child.reopen()
			child.drive()
			current := driverState(t, child)
			if child.run.Status != string(runtime.PhaseSliceReady) || current.SliceReceipt.Receipt.RequirementRevision != revision || current.Validation.Document.Content != document || len(current.SpawnReceipts) != len(physical.SpawnReceipts) || !reflect.DeepEqual(current.Results, physical.Results) {
				t.Fatal("current document did not rebuild delivery with the original carried gate evidence")
			}
			if current.CandidatePromotion == nil || current.SliceReceipt.Receipt.Snapshot != current.AcceptedNativeSnapshot() || current.SliceReceipt.Receipt.Snapshot != current.CandidatePromotion.Final || current.SliceReceipt.Receipt.Digest != current.CandidatePromotion.FinalDigest || !reflect.DeepEqual(current.SpawnReceipts, physical.SpawnReceipts) {
				t.Fatal("current child receipt lost its actual promotion or original dispatches")
			}
			requirementPath := current.RequirementArtifactPath()
			changes, err := child.f.vcs.Changes(ready.Receipt.Snapshot, current.SliceReceipt.Receipt.Snapshot)
			if err != nil || len(changes) != 1 || changes[0].Path != requirementPath || changes[0].Kind != "M" || changes[0].BaseMode != changes[0].Mode {
				t.Fatalf("document delivery changed original production values: %+v %v", changes, err)
			}
			body, err := (vcs.ExecRunner{}).RunRaw(child.f.vcs.Root(), "git", nil, "show", current.SliceReceipt.Receipt.Snapshot+":"+requirementPath)
			if err != nil || string(body) != document {
				t.Fatalf("delivered child requirement differs from confirmation: %q %v", body, err)
			}
			before, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			child.reopen()
			child.drive()
			after, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("current child document required repeated observation or changed on replay", err)
			}
		})
	}
}
