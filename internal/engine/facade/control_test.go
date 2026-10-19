package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
)

// Control fixtures exercise the production MCP confirmation callback with an
// explicit simulated host response. They do not count as native UI validation.
func submitControlWithHost(t *testing.T, f *Facade, candidate ControlCandidate) (protocol.Acceptance, Run, error) {
	t.Helper()
	input, inputWriter := io.Pipe()
	output, outputWriter := io.Pipe()
	t.Cleanup(func() { input.Close(); inputWriter.Close(); output.Close(); outputWriter.Close() })
	var accepted protocol.Acceptance
	var run Run
	var submissionErr error
	var errors bytes.Buffer
	done := make(chan int, 1)
	_, recorded, _ := f.recordedEvent(candidate.Event)
	go func() {
		code := harness.ServeTools(input, outputWriter, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			var identity protocol.EventID
			if name != "control" || json.Unmarshal(arguments, &identity) != nil || identity != candidate.Event.ID {
				return nil, fmt.Errorf("unexpected original control call %s %s", name, arguments)
			}
			accepted, run, submissionErr = f.SubmitControl(candidate, confirm)
			return accepted, submissionErr
		}, nil)
		outputWriter.Close()
		done <- code
	}()
	encoder := json.NewEncoder(inputWriter)
	decoder := json.NewDecoder(output)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		t.Fatal(err)
	}
	var initialized any
	if err := decoder.Decode(&initialized); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "control", "arguments": candidate.Event.ID}}); err != nil {
		t.Fatal(err)
	}
	elicited := 0
	for {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Mode    string `json:"mode"`
				Message string `json:"message"`
				Schema  struct {
					Properties map[string]map[string]any `json:"properties"`
					Required   []string                  `json:"required"`
				} `json:"requestedSchema"`
			} `json:"params"`
		}
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Method == "" {
			break
		}
		expected, err := json.MarshalIndent(candidate, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		property := frame.Params.Schema.Properties["confirmOperation"]
		if frame.Method != "elicitation/create" || frame.Params.Mode != "form" || frame.Params.Message != "Confirm this exact operation:\n"+string(expected) || property["type"] != "boolean" || property["default"] != nil || !reflect.DeepEqual(frame.Params.Schema.Required, []string{"confirmOperation"}) {
			t.Fatalf("confirmation differs from its original candidate: %+v", frame)
		}
		elicited++
		if err := encoder.Encode(map[string]any{"id": frame.ID, "result": map[string]any{"action": "accept", "content": map[string]any{"confirmOperation": true}}}); err != nil {
			t.Fatal(err)
		}
	}
	inputWriter.Close()
	if code := <-done; code != 0 {
		t.Fatal(code, errors.String())
	}
	if (recorded || candidate.Event.Decide == nil) && elicited != 0 || !recorded && candidate.Event.Decide != nil && submissionErr == nil && elicited != 1 {
		t.Fatal("confirmation did not follow the original event ledger", recorded, elicited, submissionErr)
	}

	return accepted, run, submissionErr
}

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
	t.Parallel()
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
	accepted, updated, err := submitControlWithHost(t, d.f, restored)
	if err != nil {
		t.Fatal(err)
	}
	d.run = updated
	replay, _, err := submitControlWithHost(t, d.f, restored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accepted, replay) {
		t.Fatal("candidate replay changed the accepted decision")
	}
}
func TestControlCandidateBindsCurrentResetAndRejectsStaleDecision(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	accepted, updated, err := submitControlWithHost(t, d.f, confirm)
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
	replay, _, err := submitControlWithHost(t, reopened, confirm)
	if err != nil || !reflect.DeepEqual(accepted, replay) {
		t.Fatal("original confirmation replay changed acceptance", err)
	}
	repeated, err := reopened.engine.Load()
	if err != nil || !reflect.DeepEqual(after, repeated) {
		t.Fatal("reopened confirmation replay changed state or document", err)
	}
}

func TestControlCandidateRejectsAnotherProjectOrRunWithoutWriting(t *testing.T) {
	t.Parallel()
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
		if _, _, err := submitControlWithHost(t, d.f, wrong); err == nil {
			t.Fatalf("accepted another %s candidate", boundary)
		}
		after, err := d.f.engine.Load()
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("wrong candidate changed the target run", err)
		}
	}
}

func TestConfirmedRequirementContentRejectsChangedSourceAndRecoversAtomically(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"source-changed", string(persistence.FaultIntentBefore), string(persistence.FaultCommitResponseLost)} {
		t.Run(scenario, func(t *testing.T) {
			d := plannedValidationDriver(t, nil, commandPlan(), "quality")
			freeze, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(t, d.f, freeze)
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
			_, d.run, err = submitControlWithHost(t, d.f, update)
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
				if _, _, err := submitControlWithHost(t, d.f, confirmed); err == nil {
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
			engine, err := protocol.New(store, protocol.Config{NativeRoot: d.f.root, Definition: d.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(d.f.root, d.f.runID), ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission)})
			if err != nil {
				t.Fatal(err)
			}
			d.f.store, d.f.engine = store, engine
			if _, _, err := submitControlWithHost(t, d.f, confirmed); err == nil {
				t.Fatal("original commit failure did not surface")
			}
			reopened, err := Open(d.f.root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			accepted, _, err := submitControlWithHost(t, reopened, confirmed)
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
			replay, _, err := submitControlWithHost(t, reopened, confirmed)
			if err != nil || !reflect.DeepEqual(accepted, replay) {
				t.Fatal("same original confirmation was recorded twice", err)
			}
		})
	}
}

func TestInitialRouteUsesLibraryGatesAndOriginalPlan(t *testing.T) {
	t.Parallel()
	for _, gates := range [][]int{{1, 2, 3}, {3}, {}} {
		t.Run(fmt.Sprint(gates), func(t *testing.T) {
			d := startValidationDriver(t, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
			catalog, err := d.f.routeRequestPayload(false)
			if err != nil {
				t.Fatal(err)
			}
			want, err := selectStrings(catalog.RouteGates, gates, "gate")
			if err != nil {
				t.Fatal(err)
			}
			configured := protocol.RouteDecision{Selected: want}
			d.configureRoute(&configured)
			offered := configured.Gates
			request, err := protocol.NewRouteRequestEvent(d.eventID(), offered, protocol.AskOption{ID: "approve", Label: "Confirm"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			input := ControlInput{Ask: askPosition(t, d.run, protocol.ControlSelectRoute), Choice: "approve"}
			candidate, err := d.f.PrepareControl(input)
			if err != nil {
				t.Fatal(err)
			}
			route := candidate.Event.Decide.Review.Route
			if !reflect.DeepEqual(route.Gates, offered) || candidate.DevelopmentPlan == nil || route.DevelopmentPlan == nil || *route.DevelopmentPlan != candidate.DevelopmentPlan.Confirmation {
				t.Fatal("route lost the original complete catalog or exact plan binding")
			}
			if !slices.Equal(route.Selected, want) {
				t.Fatal("initial route did not use the bound library gates")
			}
			if _, _, err := submitControlWithHost(t, d.f, candidate); err != nil {
				t.Fatal("program route did not reach original typed acceptance", err)
			}
		})
	}
}

func TestInitialRouteSharesAutomaticCatalogAndReplaysWithoutReselection(t *testing.T) {
	t.Parallel()
	d := startValidationDriver(t, nil)
	for _, stage := range []string{"unreviewed", "reviewed"} {
		if stage == "reviewed" {
			passPreDevelopmentReviews(t, d, commandPlan())
		}
		before := driverState(t, d)
		if _, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlSelectRoute}); err == nil {
			t.Fatalf("%s run opened route selection before topology", stage)
		}
		if !reflect.DeepEqual(before, driverState(t, d)) {
			t.Fatal("rejected preview changed state")
		}
	}
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
	catalog, err := prompts.PackageRouteCandidates(d.f.admission.Target)
	if err != nil || len(catalog) < 4 {
		t.Fatal("fixture needs both QA modes and two installed ordinary gates", catalog, err)
	}
	if offered, err := d.f.RequestWorkflowChoice(); err != nil || !offered {
		t.Fatal("initial automatic selection was not offered", offered, err)
	}
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	automatic := d.run.PendingAsks[askPosition(t, d.run, protocol.ControlSelectRoute)-1]
	if !reflect.DeepEqual(automatic.RouteGates, catalog) {
		t.Fatal("automatic request differs from the installed catalog")
	}
	initial, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlSelectRoute), Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, run, err := submitControlWithHost(t, d.f, initial)
	if err != nil {
		t.Fatal(err)
	}
	d.run = run
	if offered, err := d.f.RequestWorkflowChoice(); err != nil || offered {
		t.Fatal("bound route created another automatic selection", offered, err)
	}
	before := driverState(t, d)
	if _, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlSelectRoute}); err == nil {
		t.Fatal("runtime route selection remained available")
	}
	if !reflect.DeepEqual(before, driverState(t, d)) {
		t.Fatal("rejected route request changed state")
	}
	body, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	var saved ControlCandidate
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := submitControlWithHost(t, reopened, saved)
	if err != nil || !reflect.DeepEqual(got, accepted) {
		t.Fatal("original route control replay changed acceptance", err)
	}
	replayed, err := reopened.engine.Load()
	if err != nil || !reflect.DeepEqual(before, replayed.State) {
		t.Fatal("route control replay changed authoritative state", err)
	}
}

func TestLibraryGateChangesApplyDuringDevelopmentWithoutAnAsk(t *testing.T) {
	t.Parallel()
	d := startValidationDriver(t, nil)
	catalog, err := prompts.PackageRouteCandidates(d.f.admission.Target)
	if err != nil || len(catalog) < 4 {
		t.Fatal(catalog, err)
	}
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
	d.confirmRoute(routeDecisionForGates([]string{catalog[2]}))
	d.drive()
	action, in := d.action("engine.development.batch.v6")
	for _, gates := range [][]int{{4}, {1, 3}, {2, 3}, {3, 4}, {}} {
		want, err := selectStrings(catalog, gates, "gate")
		if err != nil {
			t.Fatal(err)
		}
		configuration := protocol.RouteDecision{Selected: want}
		d.configureRoute(&configuration)
		if !slices.Equal(d.run.Review.SelectedGates, want) {
			t.Fatal("route change lost the selected obligations", gates, d.run.Review.SelectedGates, err)
		}
		if current, ok := d.run.PendingActions[action]; !ok || current.Task != in.Binding.Task {
			t.Fatal("validation selection replaced the current development attempt")
		}
	}
}

func TestRouteRequestUsesChildCatalogAndKeepsTopologyOwnedMergeDuties(t *testing.T) {
	t.Parallel()
	master, child := controlChildren(t)
	before := driverState(t, master)
	_, err := master.f.PrepareControl(ControlInput{Request: protocol.ControlSelectRoute})
	if err == nil {
		t.Fatal("merge master opened a runtime configuration Ask")
	}
	if !reflect.DeepEqual(before, driverState(t, master)) {
		t.Fatal("configuration preview changed state")
	}
	if offered, err := child.f.RequestWorkflowChoice(); err != nil || !offered {
		t.Fatal("child initial route was not offered", offered, err)
	}
	child.run, err = child.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	initial := child.run.PendingAsks[askPosition(t, child.run, protocol.ControlSelectRoute)-1]
	catalog, err := prompts.PackageRouteCandidates(child.f.admission.Target)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(initial.RouteGates, catalog[2:]) {
		t.Fatal("child catalog lost ordinary gates or retained QA")
	}
	choice, err := child.f.PrepareControl(ControlInput{Ask: askPosition(t, child.run, protocol.ControlSelectRoute), Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if _, child.run, err = submitControlWithHost(t, child.f, choice); err != nil {
		t.Fatal(err)
	}
	if _, err := child.f.PrepareControl(ControlInput{Request: protocol.ControlSelectRoute}); err == nil {
		t.Fatal("child reopened runtime route selection")
	}
}
