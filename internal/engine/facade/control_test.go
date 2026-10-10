package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func askPosition(t *testing.T, run Run, control protocol.ControlKind) int {
	t.Helper()
	for i, a := range run.PendingAsks {
		if a.Control == control {
			return i + 1
		}
	}
	t.Fatalf("missing %s Ask", control)
	return 0
}
func TestControlCandidateUsesOriginalAskAndPreservesTwoStages(t *testing.T) {
	d := startValidationDriver(t, nil)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("request preview changed authoritative state", err)
	}
	if request.Event.ID == "" || request.Event.Kind != protocol.KindRequestControl || request.Event.Decide != nil {
		t.Fatal("preview did not preserve request-only control")
	}
	d.submit(request.Event)
	n := askPosition(t, d.run, protocol.ControlAbort)
	pending := d.run.PendingAsks[n-1]
	decision, err := d.f.PrepareControl(ControlInput{Ask: n, Choice: "abort", Reason: "stop the confirmed work"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Event.Decide.Request != protocol.RequestID(pending.RequestID) || decision.Event.Decide.Token != pending.Token || decision.Event.ID == request.Event.ID {
		t.Fatal("decision lost its original Ask binding")
	}
	unchanged, err := d.f.Show()
	if err != nil || unchanged.Revision != d.run.Revision {
		t.Fatal("decision preview authorized the operation", err)
	}
	// Exact saved candidate bytes are the event consumed after user confirmation.
	raw, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	var restored ControlCandidate
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	accepted, updated, err := d.f.SubmitControl(restored)
	if err != nil {
		t.Fatal(err)
	}
	d.run = updated
	replay, _, err := d.f.SubmitControl(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accepted, replay) {
		t.Fatal("candidate replay changed the accepted decision")
	}
}
func TestControlCandidateBindsCurrentResetAndRejectsStaleDecision(t *testing.T) {
	d := plannedValidationDriver(t, nil, commandPlan(), "quality")
	request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlReset})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request.Event)
	candidate, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReset), Choice: "proceed", Reason: "reconsider current validation"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Reset == nil || candidate.Event.Decide.Reset.PreviewDigest != candidate.Reset.CanonicalDigest() {
		t.Fatal("reset preview digest was not program-bound")
	}
	another, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(another.Event)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.Submit(candidate.Event); err == nil {
		t.Fatal("stale saved confirmation accepted changed state")
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("stale decision changed state", err)
	}
}
func TestRequirementControlBindsOriginalWorkAndCurrentDocument(t *testing.T) {
	d := plannedValidationDriver(t, nil, commandPlan(), "quality")
	req, err := d.f.request()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.f.root, req.IntakeConfirmationReceipt.RequirementSource)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	choices := make([]WorkChoice, len(d.run.RequirementWork))
	for i := range choices {
		choices[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "formatting preserves the exact obligation"}
	}
	candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"formatting"}, CarryReasons: []string{"all semantic content is unchanged"}}}})
	if err != nil {
		t.Fatal(err)
	}
	pkg := candidate.Event.Request.Package
	if pkg == nil || candidate.Intake == nil || pkg.CandidateRevision == d.revision || len(pkg.WorkDecisions) != len(choices) {
		t.Fatal("missing complete program-bound requirement package")
	}
	for i, w := range pkg.WorkDecisions {
		if !reflect.DeepEqual(w.Work, d.run.RequirementWork[i]) {
			t.Fatal("original work identity changed")
		}
	}
	d.submit(candidate.Event)
	confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "confirm the displayed complete formatting change"})
	if err != nil {
		t.Fatal(err)
	}
	if confirm.Event.Decide.Requirement.BaseRevision != d.revision || confirm.Event.Decide.Requirement.DeltaDigest != pkg.DeltaDigest || confirm.Intake == nil {
		t.Fatal("confirmation copied or lost original requirement facts")
	}
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	accepted, updated, err := d.f.SubmitControl(confirm)
	if err != nil {
		t.Fatal(err)
	}
	d.run = updated
	if d.run.Review.RequirementRevision != pkg.CandidateRevision {
		t.Fatal("requirement confirmation did not reach original acceptance owner")
	}
	after, err := d.f.engine.Load()
	if err != nil || after.Revision != before.Revision+1 || !reflect.DeepEqual(after.State.Validation.Document, *confirm.Intake.RequirementDocument) || after.State.RequirementGuarantee != nil && after.State.RequirementGuarantee.ManifestDigest == "" || !reflect.DeepEqual(after.State.IntakeConfirmationReceipt, before.State.IntakeConfirmationReceipt) {
		t.Fatal("one decision transaction did not consume the exact full requirement and preserve intake", err)
	}
	reopened, err := Open(d.f.root, d.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := reopened.SubmitControl(confirm)
	if err != nil || !reflect.DeepEqual(accepted, replay) {
		t.Fatal("original confirmation replay changed acceptance", err)
	}
	repeated, err := reopened.engine.Load()
	if err != nil || !reflect.DeepEqual(after, repeated) {
		t.Fatal("reopened confirmation replay changed state or document", err)
	}
}

func TestControlCandidateRejectsAnotherProjectOrRunWithoutWriting(t *testing.T) {
	d := startValidationDriver(t, nil)
	candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{"project", "run"} {
		wrong := candidate
		if boundary == "project" {
			wrong.Root = t.TempDir()
		} else {
			wrong.Run.RunID = "other-run"
		}
		before, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.f.SubmitControl(wrong); err == nil {
			t.Fatalf("accepted another %s candidate", boundary)
		}
		after, err := d.f.engine.Load()
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("wrong candidate changed the target run", err)
		}
	}
}

func TestConfirmedRequirementContentRejectsChangedSourceAndRecoversAtomically(t *testing.T) {
	for _, scenario := range []string{"source-changed", string(persistence.FaultIntentBefore), string(persistence.FaultCommitResponseLost)} {
		t.Run(scenario, func(t *testing.T) {
			d := plannedValidationDriver(t, nil, commandPlan(), "quality")
			freeze, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(freeze)
			if err != nil {
				t.Fatal(err)
			}
			req, err := d.f.request()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(d.f.root, req.IntakeConfirmationReceipt.RequirementSource)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(original, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			work := make([]WorkChoice, len(d.run.RequirementWork))
			for i := range work {
				work[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "Formatting leaves this original duty unchanged"}
			}
			update, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{"trailing LF"}, CarryReasons: []string{"same semantic duties"}}}})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(update)
			if err != nil {
				t.Fatal(err)
			}
			confirmed, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm the complete displayed original content"})
			if err != nil {
				t.Fatal(err)
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "source-changed" {
				if err := os.WriteFile(path, append(original, '\n', '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				if _, _, err := d.f.SubmitControl(confirmed); err == nil {
					t.Fatal("confirmation accepted source bytes changed after the preview")
				}
				after, err := d.f.engine.Load()
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("changed source wrote confirmation or document", err)
				}
				return
			}
			once := false
			store, err := persistence.NewStore(d.f.stateDir, persistence.Config{PackageDigest: d.f.envelope.PackageDigest, DefinitionSource: d.f.envelope.DefinitionSource, InstalledTargetIdentity: d.f.envelope.InstalledTargetIdentity, AcquireAdmission: d.f.admission.acquire, FaultInjector: func(point persistence.FaultPoint) error {
				if string(point) == scenario && !once {
					once = true
					return fmt.Errorf("confirmed document commit interrupted")
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			engine, err := protocol.New(store, protocol.Config{NativeRoot: d.f.root, Definition: d.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(d.f.root, d.f.runID), ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			d.f.store, d.f.engine = store, engine
			if _, _, err := d.f.SubmitControl(confirmed); err == nil {
				t.Fatal("original commit failure did not surface")
			}
			reopened, err := Open(d.f.root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			accepted, _, err := reopened.SubmitControl(confirmed)
			if err != nil {
				t.Fatal(err)
			}
			after, err := reopened.engine.Load()
			if err != nil || after.Revision != before.Revision+1 || !reflect.DeepEqual(after.State.Validation.Document, *confirmed.Intake.RequirementDocument) || !reflect.DeepEqual(after.State.IntakeConfirmationReceipt, before.State.IntakeConfirmationReceipt) {
				t.Fatal("recovery separated original document from its confirmation", err)
			}
			if _, err := after.State.Events[string(confirmed.Event.ID)].Replay(confirmed.Event); err != nil {
				t.Fatal(err)
			}
			replay, _, err := reopened.SubmitControl(confirmed)
			if err != nil || !reflect.DeepEqual(accepted, replay) {
				t.Fatal("same original confirmation was recorded twice", err)
			}
		})
	}
}

func TestRouteControlUsesOfferedCatalogAndOriginalPlan(t *testing.T) {
	for _, mode := range []string{"full", "custom"} {
		t.Run(mode, func(t *testing.T) {
			d := startValidationDriver(t, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
			offered := []string{"blackbox", "whitebox", "quality"}
			request, err := protocol.NewRouteRequestEvent(d.eventID(), offered, protocol.AskOption{ID: protocol.AskOptionID(mode), Label: mode})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			input := ControlInput{Ask: askPosition(t, d.run, protocol.ControlSelectRoute), Choice: mode}
			if mode == "custom" {
				input.Gates = []int{3}
			}
			candidate, err := d.f.PrepareControl(input)
			if err != nil {
				t.Fatal(err)
			}
			route := candidate.Event.Decide.Review.Route
			if !reflect.DeepEqual(route.Gates, offered) || candidate.DevelopmentPlan == nil || route.DevelopmentPlan == nil || *route.DevelopmentPlan != candidate.DevelopmentPlan.Confirmation {
				t.Fatal("route lost the original complete catalog or exact plan binding")
			}
			if mode == "custom" && !reflect.DeepEqual(route.Selected, []string{"quality"}) {
				t.Fatal("selected gate position did not resolve its original offered identity")
			}
			if _, _, err := d.f.SubmitControl(candidate); err != nil {
				t.Fatal("program route did not reach original typed acceptance", err)
			}
		})
	}
}
