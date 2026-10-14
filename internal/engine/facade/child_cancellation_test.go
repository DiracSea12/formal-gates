package facade

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestParentAbortCancelsCurrentAndReplacedChildren(t *testing.T) {
	master, child, _, _ := deliveredChildCheckpoint(t, validationDocument)
	before := driverState(t, master)
	choices := requirementChildChoices(t, master)
	for i := range choices {
		if choices[i].ChildRunID != child.f.runID {
			choices[i].RouteDisposition = protocol.RequirementWorkRedo
			choices[i].Reason = "replace the affected slice route"
			for j := range choices[i].WorkDecisions {
				choices[i].WorkDecisions[j].Disposition = protocol.RequirementWorkRedo
			}
		}
	}
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	intake, err := prepareConfirmedIntake(master.f.root, master.run.IntakeConfirmationReceipt, true)
	if err != nil {
		t.Fatal(err)
	}
	pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:topology", ImpactDigest: "sha256:affected-slice", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, RedoTopology: true, Children: choices, Impact: &protocol.RequirementImpact{ChangedItems: []string{"affected slice boundary"}, TransitiveImpacts: []string{"replacement route"}, CarryReasons: []string{"core evidence and ownership unchanged"}}}
	for _, work := range master.run.RequirementWork {
		pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master obligation"})
	}
	request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm complete topology impact"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	ask := master.run.PendingRequirementAsk
	master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, RedoStartReadiness: pkg.RedoStartReadiness, RedoTopology: true, Children: choices, Impact: pkg.Impact, Reason: "preserve exact core and replace affected route and its development plan"}}}.WithRequirementDocument(document))
	master.revision = revision
	master.drive()
	passPreDevelopmentReviews(t, master, splitCommandPlan("lane-core", "lane-io"), review.StartReadiness)
	current := driverState(t, master)
	if !reflect.DeepEqual(current.ReviewSeries.ProductReview, before.ReviewSeries.ProductReview) || current.ReviewSeries.StartReadiness.Completed != before.ReviewSeries.StartReadiness.Completed+1 {
		t.Fatal("owner plan revision reran product review or omitted the explicitly authorized fresh technical review")
	}
	topology := splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})
	topology.Slices[1] = "lane-io"
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology})
	current = driverState(t, master)
	digest, err := master.f.vcs.TreeDigest(current.AcceptedNativeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := master.f.engine.PrepareChildren(digest, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	current = driverState(t, master)
	if !current.RebuildingChildren() || current.ChildCreation.Previous == nil {
		t.Fatal("original typed topology rebuild was not admitted")
	}
	for id := range master.run.PendingActions {
		master.receipt(id)
	}

	abortRun(t, master)
	master.reopen()
	master.drive()
	master.completeWorkerTerminations()
	master.drive()
	if master.run.Status != "ABORTED" || master.run.Termination == nil || !master.run.Termination.ResourcesCleaned() {
		t.Fatal("parent abort did not clean the frozen resource scope", master.run.Status)
	}
	terminal := master.run.Termination
	for _, prior := range terminal.ChildCreation.Previous.Children {
		if slices.ContainsFunc(terminal.ChildCreation.Children, func(c protocol.ChildPreparation) bool { return c.RunID == prior.RunID }) {
			continue
		}
		old, err := Open(master.f.root, prior.RunID, master.f.runtimeFiles)
		if err != nil {
			t.Fatal(err)
		}
		run, err := old.Show()
		if err != nil || run.Status != "ABORTED" || run.Termination == nil || !run.Termination.ResourcesCleaned() {
			t.Fatal("previous-only child remained active", run.Status, err)
		}
		authority := run.Termination.SliceChild.ParentAbort
		if authority == nil || authority.Requirement == nil || authority.Decision.RequestID != terminal.ChildCreation.RequestID || !reflect.DeepEqual(authority.Event, terminal.Events[authority.Decision.EventID]) {
			t.Fatal("replaced child lost the original confirmed topology cancellation authority")
		}
		if _, err := os.Stat(filepath.Join(old.stateDir, "state.json")); !os.IsNotExist(err) {
			t.Fatal("canceled old child retained active workflow state", err)
		}
		if _, err := os.Stat(terminal.NativeResources[prior.ResourceID].Path); !os.IsNotExist(err) {
			t.Fatal("old child target was not cleaned", err)
		}
	}
	master.reopen()
	master.drive()
	if master.run.Status != "ABORTED" || !reflect.DeepEqual(master.run.Termination.ReconciledEffects, terminal.ReconciledEffects) || !reflect.DeepEqual(master.run.Termination.Cost, terminal.Cost) {
		t.Fatal("recovery repeated cancellation or changed cost")
	}
}
