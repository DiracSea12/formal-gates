package facade

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// A confirmed requirement and its observed document jointly qualify child work;
// accepted physical identity and carried gate evidence survive the waiting window.
func TestChildWaitsForCurrentParentDocumentAcrossRecovery(t *testing.T) {
	for _, reviewPending := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent-review-pending-%t", reviewPending), func(t *testing.T) {
			master, child, _, ready := deliveredChildCheckpoint(t, validationDocument)
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
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:confirmed-document", ImpactDigest: "sha256:current-child-material", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: reviewPending, Children: children, Impact: &protocol.RequirementImpact{ChangedItems: []string{"confirmed document"}, TransitiveImpacts: []string{"current child candidate and receipt"}, CarryReasons: []string{"unchanged child development and gate inputs"}}}
			for _, work := range master.run.RequirementWork {
				pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master inputs"})
			}
			request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm current child material"})
			if err != nil {
				t.Fatal(err)
			}
			master.submit(request)
			ask := master.run.PendingRequirementAsk
			master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, Children: children, Impact: pkg.Impact, RedoStartReadiness: reviewPending, Reason: "redo current child material and carry the unchanged gate inputs"}}})
			for range 2 {
				child.reopen()
				child.drive()
				waiting := driverState(t, child)
				if child.run.Next.Kind != decision.KindOperator && child.run.Next.Kind != decision.KindWait || waiting.FinalValidationComplete(child.f.definition) || waiting.DeliveryComplete(child.f.definition) || waiting.SliceReceipt != nil || waiting.RequirementRevision() != revision || waiting.Validation.RequirementRevision == revision {
					t.Fatal("child crossed the unobserved current document boundary")
				}
				if waiting.AcceptedNativeSnapshot() != physical.AcceptedNativeSnapshot() || !reflect.DeepEqual(waiting.CandidatePromotion, physical.CandidatePromotion) || waiting.SliceChild.RequirementRevision != physical.SliceChild.RequirementRevision || !reflect.DeepEqual(waiting.IntakeConfirmationReceipt, physical.IntakeConfirmationReceipt) {
					t.Fatal("waiting changed accepted physical identity or child origin")
				}
			}
			master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			if reviewPending {
				child.reopen()
				child.drive()
				if !driverState(t, child).SliceChild.ParentReviewPending || child.run.Status == runtime.PhaseSliceReady {
					t.Fatal("current document bypassed pending parent review")
				}
				passPreDevelopmentReviews(t, master, commandPlan())
			}
			child.reopen()
			child.drive()
			current := driverState(t, child)
			if child.run.Status != runtime.PhaseSliceReady || current.SliceReceipt.Receipt.RequirementRevision != revision || current.SliceReceipt.Receipt.Snapshot != ready.Receipt.Snapshot || current.Validation.Document.Content != document || len(current.SpawnReceipts) != len(physical.SpawnReceipts) || !reflect.DeepEqual(current.Results, physical.Results) {
				t.Fatal("current document did not rebuild delivery with the original carried gate evidence")
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
