package facade

import (
	"bytes"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Control tests use actual engine-created children and the normal facade entry
// points. Worker payloads bind the exact signed workspace and requirement.
func controlChildren(t *testing.T) (*validationDriver, *validationDriver) {
	t.Helper()
	master := splitMaster(t)
	master.drive()
	s := driverState(t, master)
	f, err := Open(master.f.root, s.SliceChildren["lane-core"])
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: f.vcs}
	child.drive()
	return master, child
}
func resetControl(t *testing.T, d *validationDriver) protocol.Event {
	t.Helper()
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlReset, protocol.AskOption{ID: "proceed", Label: "Reset with preview"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	state := driverState(t, d)
	ask := state.PendingAsks[string(request.ID)]
	if ask.Reset == nil || ask.Reset.Digest == "" || len(ask.Reset.Preserve) == 0 || len(ask.Reset.Clear) == 0 {
		t.Fatal("reset omitted preservation/cleanup preview")
	}
	token, err := d.f.Freshness(ask.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{ID: protocol.EventID(d.eventID()), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: token, Choice: "proceed", Reset: &protocol.ResetDecision{PreviewDigest: ask.Reset.Digest, Reason: "explicitly reset the flow and preserve development"}}}
	d.submit(event)
	return event
}
func confirmResetControl(t *testing.T, d *validationDriver) {
	t.Helper()
	d.drive()
	ask := d.run.PendingRequirementAsk
	if ask == nil {
		t.Fatal("reset omitted complete document re-confirmation")
	}
	pkg := ask.Package
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Coverage: pkg.Coverage, BaseRevision: ask.BaseRevision, CandidateRevision: pkg.CandidateRevision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, RedoProductReview: pkg.RedoProductReview, RedoStartReadiness: pkg.RedoStartReadiness, ResetRequestID: pkg.ResetRequestID, Reason: "confirm the complete registered requirement and solution"}}})
}
func TestResetPreservesNativeAndScopedAuthority(t *testing.T) {
	master, child := controlChildren(t)
	before := driverState(t, child)
	parentBefore := driverState(t, master)
	raw, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	reset := resetControl(t, child)
	confirmResetControl(t, child)
	state := driverState(t, child)
	after, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) || state.BaseSnapshot != before.BaseSnapshot || state.InitialSnapshot != before.InitialSnapshot || !reflect.DeepEqual(state.Topology, before.Topology) || state.SliceChild.RequirementRevision != before.SliceChild.RequirementRevision {
		t.Fatal("reset changed native content or child origin")
	}
	if !reflect.DeepEqual(driverState(t, master).ReviewSeries, parentBefore.ReviewSeries) || len(state.ReviewSeries.ProductReview.RawCandidates) != 0 {
		t.Fatal("child reset changed parent review authority")
	}
	if _, _, err = child.f.Submit(reset); err != nil {
		t.Fatal("reset replay was not stable", err)
	}
}
func TestMasterResetCascadesOneConfirmation(t *testing.T) {
	master, child := controlChildren(t)
	resetControl(t, master)
	child.drive()
	confirmResetControl(t, master)
	child.drive()
	state := driverState(t, child)
	if state.ResetPending() || state.SliceChild.ParentReset == nil || !state.SliceChild.ParentReset.Decision.Reset.Confirmed {
		t.Fatal("master reset required an independent child decision")
	}
	if driverState(t, master).ReviewSeries.ProductReview.Completed != 0 || driverState(t, master).ReviewSeries.StartReadiness.Completed != 0 {
		t.Fatal("explicit reset did not rebuild review quotas")
	}
}
func TestMasterRequirementBarrierProtectsChildWrites(t *testing.T) {
	master, child := controlChildren(t)
	request, err := protocol.NewRequestEvent(master.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	route := protocol.RouteDecision{Mode: "default", Gates: []string{"quality"}, DevelopmentPlan: &protocol.DevelopmentPlanConfirmation{Proposal: sliceLanePlan()}}
	preview, err := child.f.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	if err = controlError(t, child, protocol.ControlSelectRoute, "default", protocol.ReviewDecisionPayload{Route: &route}); err == nil {
		t.Fatal("master barrier admitted child route")
	}
	if _, err = child.f.PrepareCandidate(""); err == nil {
		t.Fatal("master barrier admitted promotion")
	}
	state := driverState(t, child)
	if !state.RequirementBarrierActive() || state.SliceChild.ParentChange == nil {
		t.Fatal("child omitted exact parent authority")
	}
}
func TestMasterRequirementConfirmationCarriesChildOrigin(t *testing.T) {
	master, child := controlChildren(t)
	origin := driverState(t, child).SliceChild.RequirementRevision
	intake := driverState(t, child).IntakeConfirmationReceipt
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	confirmValidationDocument(master, "", nil, document)
	master.drive()
	child.drive()
	state := driverState(t, child)
	if state.RequirementRevision() != driverState(t, master).RequirementRevision() || state.SliceChild.RequirementRevision != origin || !reflect.DeepEqual(state.IntakeConfirmationReceipt, intake) || state.SliceChild.ParentChange == nil || state.SliceChild.ParentChange.DecisionDigest == "" {
		t.Fatal("cascade changed origin or lost confirmation authority")
	}
	request, err := protocol.NewRequestEvent(child.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Independent child revision"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = child.f.Submit(request); err == nil {
		t.Fatal("child accepted independent requirement authority")
	}
}
func TestAbortedSliceSupersedesReadyReceipt(t *testing.T) {
	master, child, _, ready := deliveredChildCheckpoint(t, validationDocument)
	abortRun(t, child)
	child.drive()
	master.drive()
	s := driverState(t, master)
	accepted := s.SliceReceipts[child.f.runID]
	if accepted.Receipt.Status != "ABORTED" || accepted.Receipt.Abort == nil || accepted.Digest == ready.Digest {
		t.Fatal("abort did not replace successful receipt")
	}
	if master.run.Next.Kind != decision.KindAsk {
		t.Fatal("aborted member became a tasks-in-flight wait")
	}
	before := s.Cost
	if err := master.f.acceptSliceReceipt(accepted); err != nil {
		t.Fatal(err)
	}
	if err := master.f.acceptSliceReceipt(ready); err != nil {
		t.Fatal(err)
	}
	current := driverState(t, master)
	if current.SliceReceipts[child.f.runID].Digest != accepted.Digest || !reflect.DeepEqual(current.Cost, before) {
		t.Fatal("repeated/late receipt changed abort or cost")
	}
	child.reopen()
	master.reopen()
	master.drive()
	if driverState(t, master).SliceReceipts[child.f.runID].Digest != accepted.Digest {
		t.Fatal("terminal discovery lost aborted evidence")
	}
}
