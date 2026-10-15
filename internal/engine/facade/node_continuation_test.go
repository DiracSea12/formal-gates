package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
)

func businessNodeNumber(t *testing.T, run Run, id string) int {
	t.Helper()
	for i, n := range run.BusinessNodes {
		if n.ID == id {
			return i + 1
		}
	}
	t.Fatalf("missing business node %s: %+v", id, run.BusinessNodes)
	return 0
}

func continueBusinessNode(d *validationDriver, id string) ControlCandidate {
	d.t.Helper()
	path := filepath.Join(d.f.stateDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		d.t.Fatal(err)
	}
	request, err := d.f.PrepareCurrentControl(ControlInput{Request: protocol.ControlContinueNode, Node: businessNodeNumber(d.t, d.run, id)})
	if err != nil {
		d.t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		d.t.Fatal("node suggestion wrote state", err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, request)
	if err != nil {
		d.t.Fatal(err)
	}
	if !reflect.DeepEqual(driverState(d.t, d).PendingAsks[string(request.Event.ID)].Continuation, request.Continuation) {
		d.t.Fatal("saved node request differs from its complete original preview")
	}
	before, err = os.ReadFile(path)
	if err != nil {
		d.t.Fatal(err)
	}
	confirmation, err := d.f.PrepareCurrentControl(ControlInput{Ask: askPosition(d.t, d.run, protocol.ControlContinueNode), Choice: "approve", Reason: "user selects this existing responsibility"})
	if err != nil {
		d.t.Fatal(err)
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		d.t.Fatal("node confirmation preview wrote state", err)
	}
	raw, err := json.Marshal(confirmation)
	if err != nil {
		d.t.Fatal(err)
	}
	var saved ControlCandidate
	if err = json.Unmarshal(raw, &saved); err != nil {
		d.t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, saved)
	if err != nil {
		d.t.Fatal(err)
	}
	decision := driverState(d.t, d).Decisions[string(saved.Event.Decide.Request)]
	if decision.Continuation == nil || decision.RequestID != string(saved.Event.Decide.Request) || decision.EventID != string(saved.Event.ID) {
		d.t.Fatal("saved node authorization lost its original decision identity")
	}
	authorization := *decision.Continuation
	if authorization.AppliedRevision != 0 && authorization.AppliedRevision != decision.Revision {
		d.t.Fatal("new node authorization was applied outside its original decision transaction")
	}
	authorization.AppliedRevision = 0
	if !reflect.DeepEqual(&authorization, saved.Event.Decide.Continuation) {
		d.t.Fatal("saved node authorization differs from its complete submitted content")
	}
	return saved
}

// Existing tasks, original material and the public two-stage control are the
// source of node continuation; no phase assignment constructs the precondition.
func TestNodeContinuationEntersExistingReviewAndRouteOwners(t *testing.T) {
	for _, target := range []string{"product-review", "start-readiness", "topology", "route", "development:command", "qa-design:BLACKBOX"} {
		t.Run(target, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			original := driverState(t, d)
			for action := range d.run.PendingActions {
				d.receipt(action)
			}
			decision := continueBusinessNode(d, target)
			d.completeWorkerTerminations()
			d.drive()
			state := driverState(t, d)
			if state.RequirementRevision() != original.RequirementRevision() || !reflect.DeepEqual(state.RequirementRevisions, original.RequirementRevisions) || !reflect.DeepEqual(state.IntakeReceipt, original.IntakeReceipt) {
				t.Fatal("node choice manufactured a requirement confirmation")
			}
			switch target {
			case "product-review", "start-readiness":
				found := false
				for _, pending := range d.run.PendingActions {
					found = found || string(pending.ReviewSeries) == target
				}
				if !found {
					t.Fatalf("node did not enter original review owner: %+v", d.run.Next)
				}
			case "route":
				askPosition(t, d.run, protocol.ControlSelectRoute)
				if state.RouteConfirmed || !reflect.DeepEqual(state.SelectedGates, original.SelectedGates) {
					t.Fatal("route continuation lost its obligations or bypassed confirmation")
				}
			case "topology":
				if !reflect.DeepEqual(state.Topology, original.Topology) {
					t.Fatal("node choice re-cut topology")
				}
				for _, ask := range d.run.PendingAsks {
					if ask.Control == protocol.ControlConfirmTopology {
						t.Fatal("retained topology required a redundant confirmation")
					}
				}
				if _, input := d.action("engine.development.batch.v6"); input.Development == nil {
					t.Fatal("retained topology did not continue into its original development owner")
				}
			case "development:command":
				_, input := d.action("engine.development.batch.v6")
				if input.Development == nil || input.Development.Batch.ID != "command" {
					t.Fatal("selected development responsibility missing")
				}
			case "qa-design:BLACKBOX":
				_, input := d.action("engine.qa.design.v6")
				if input.Manifest.Binding.ReviewKind != "BLACKBOX" {
					t.Fatal("selected design owner missing")
				}
			}
			before, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			d.reopen()
			_, d.run, err = submitControlWithHost(d.t, d.f, decision)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("reopen replay repeated node effects", err)
			}
		})
	}
}

func TestNodeContinuationPreparesMissingPlanThroughOriginalOwners(t *testing.T) {
	d := nativeMasterDriver(t)
	decision := continueBusinessNode(d, "development")
	state := driverState(t, d)
	if len(decision.Continuation.To.Missing) == 0 || state.Decisions[string(decision.Event.Decide.Request)].Continuation.AppliedRevision != 0 {
		t.Fatal("future development choice manufactured its missing plan")
	}
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one public command"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	_, input := planBatchAction(d, "command")
	if input.Development == nil || driverState(t, d).Decisions[string(decision.Event.Decide.Request)].Continuation.AppliedRevision == 0 {
		t.Fatal("future target did not enter the actual confirmed developer responsibility")
	}
}

func TestNodeContinuationWaitsForExactOldExecutionAndRejectsLateResult(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	old, input := d.action("engine.development.batch.v6")
	d.receipt(old)
	decision := continueBusinessNode(d, "development:command")
	state := driverState(t, d)
	if state.Decisions[string(decision.Event.Decide.Request)].Continuation.AppliedRevision != 0 {
		t.Fatal("node entered before original live execution was stopped")
	}
	var stop protocol.HostActionIntent
	for _, intent := range d.run.AdapterIntents {
		if intent.Terminate != nil && intent.Terminate.WorkerActionID == old {
			stop = intent
		}
	}
	if stop.Terminate == nil || stop.Terminate.AttemptID != state.ObsoleteActions[old].AttemptID || stop.Terminate.Identity != state.SpawnReceipts[old].Correlation {
		t.Fatal("termination lost the original exact execution")
	}
	d.completeWorkerTerminations()
	d.drive()
	acceptance := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: old, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "late-original", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "original execution finished late"}}}})
	if acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late result advanced selected node: %+v", acceptance)
	}
	if _, accepted := driverState(t, d).Results[old]; accepted {
		t.Fatal("old execution became current")
	}
	_, fresh := d.action("engine.development.batch.v6")
	if fresh.Binding.Task == input.Binding.Task {
		t.Fatal("selected owner reused retired execution identity")
	}
}

func TestNodeContinuationRepeatedChoiceRetainsAuthorizedReplacement(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "quality")
			old, input := d.action("engine.development.batch.v6")
			d.receipt(old)
			first := continueBusinessNode(d, "development:command")
			continueBusinessNode(d, "candidate")
			if reopen {
				d.reopen()
			}
			d.completeWorkerTerminations()
			d.drive()
			fresh, binding := d.action("engine.development.batch.v6")
			if fresh == old || binding.Binding.RequirementRevision != input.Binding.RequirementRevision {
				t.Fatal("later target discarded an already authorized development replacement")
			}
			state := driverState(t, d)
			if state.Decisions[string(first.Event.Decide.Request)].Continuation.AppliedRevision == 0 {
				t.Fatal("earlier work authorization was not reconciled")
			}
			path := filepath.Join(d.f.stateDir, "state.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, d.run, err = submitControlWithHost(d.t, d.f, first); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("replayed earlier authority applied its work twice", err)
			}
		})
	}
}

func TestNodeContinuationConcurrentAuthorizationsKeepDistinctBatchIdentity(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			plan := commandPlan()
			second := commandPlan().Batches[0]
			second.ID, second.Artifacts = "tooling", []string{"tooling.go"}
			second.Subtasks = []protocol.DevelopmentSubtask{{ID: "tooling-output", Description: "Implement the independent public tooling command"}}
			plan.Batches = append(plan.Batches, second)
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, plan, "quality")
			for _, id := range []string{"command", "tooling"} {
				action, _ := planBatchAction(d, id)
				d.receipt(action)
			}
			first := continueBusinessNode(d, "development:command")
			secondChoice := continueBusinessNode(d, "development:tooling")
			if first.Event.Decide.Continuation.Work[0].Disposition != protocol.RequirementWorkRedo || secondChoice.Event.Decide.Continuation.Work[0].Disposition != protocol.RequirementWorkRedo {
				t.Fatal("normal two-authority producer did not have the same local work position")
			}
			// The later batch stops first, so both grants become applicable when
			// the earlier batch's exact STOP arrives in one ordinary transaction.
			later := secondChoice.Event.Decide.Continuation.Work[0].Work.ActionID
			for id, intent := range d.run.AdapterIntents {
				if intent.Terminate != nil && intent.Terminate.Identity == later {
					for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
						event, err := protocol.NewLifecycleEventEvent(protocol.EventID("lifecycle:"+id+":"+edge), d.host(), intent.Terminate.Identity, edge)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(event)
					}
				}
			}
			if reopen {
				d.reopen()
			}
			d.completeWorkerTerminations()
			d.drive()
			state := driverState(t, d)
			firstAuthority := state.Decisions[string(first.Event.Decide.Request)]
			secondAuthority := state.Decisions[string(secondChoice.Event.Decide.Request)]
			if firstAuthority.Continuation.AppliedRevision != secondAuthority.Continuation.AppliedRevision || firstAuthority.Revision == secondAuthority.Revision {
				t.Fatal("normal source did not exercise distinct authorizations in one application transaction")
			}
			command, commandInput := planBatchAction(d, "command")
			tooling, toolingInput := planBatchAction(d, "tooling")
			if command == tooling || commandInput.Binding.Task == toolingInput.Binding.Task {
				t.Fatal("same application transaction merged distinct authorized duties")
			}
		})
	}
}

func TestNodeContinuationSavedConfirmationRejectsStaleFreshnessWithoutWriting(t *testing.T) {
	d := newValidationDriver(t, "quality")
	request, err := d.f.PrepareCurrentControl(ControlInput{Request: protocol.ControlContinueNode, Node: businessNodeNumber(t, d.run, "product-review")})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, request)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := d.f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlContinueNode), Choice: "approve", Reason: "user requests review"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.f.PrepareCurrentControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, other)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.f.stateDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = submitControlWithHost(d.t, d.f, confirmation)
	if err == nil || !strings.Contains(err.Error(), protocol.CodeStaleFreshness) {
		t.Fatal("stale node authorization was accepted", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("stale node authorization wrote state", err)
	}
}

func TestNodeContinuationEntersValidationOwners(t *testing.T) {
	for _, target := range []string{"qa-review:BLACKBOX", "qa-execution:BLACKBOX", "gate:quality", "candidate", "post-review", "intake"} {
		t.Run(target, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			if target == "post-review" {
				qa, input := d.action("engine.qa.execution.v4")
				report := executionFor(input)
				d.business(qa, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
				gate, gi := d.gateAction("quality")
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			}
			original := driverState(t, d)
			for action := range d.run.PendingActions {
				d.receipt(action)
			}
			continueBusinessNode(d, target)
			d.completeWorkerTerminations()
			d.drive()
			if target == "qa-execution:BLACKBOX" || target == "gate:quality" {
				materializeUnchangedCandidate(d, *original.Validation.Candidate)
				d.reopen()
				d.drive()
			}
			switch target {
			case "qa-review:BLACKBOX":
				d.action("engine.qa.review.rationale.v2")
			case "qa-execution:BLACKBOX":
				d.action("engine.qa.execution.v4")
			case "gate:quality":
				d.gateAction("quality")
			case "candidate":
				state := driverState(t, d)
				if !reflect.DeepEqual(original.Validation.Candidate, state.Validation.Candidate) || !reflect.DeepEqual(original.HostActionReceipts, state.HostActionReceipts) {
					t.Fatal("candidate continuation replayed a completed native prefix")
				}
				d.action("engine.qa.execution.v4")
			case "post-review":
				state := driverState(t, d)
				if state.Validation.Waves[len(state.Validation.Waves)-1].Status != "COMPLETE" || !state.FinalValidationComplete(d.f.definition) {
					t.Fatal("post-review continuation lost original joined qualification")
				}
			case "intake":
				askPosition(t, d.run, protocol.ControlRequestRequirementChange)
			}
		})
	}
}

func TestNodeContinuationPreservesReviewLimitAndSingleExtraGrant(t *testing.T) {
	d := newValidationDriver(t, "quality")
	for completed := 1; completed < 3; completed++ {
		continueBusinessNode(d, "product-review")
		d.drive()
		passPreDevelopmentReviews(t, d, commandPlan(), review.ProductReview)
	}
	continueBusinessNode(d, "product-review")
	d.drive()
	state := driverState(t, d)
	if state.ReviewSeries.ProductReview.Completed != 3 || state.ReviewSeries.ProductReview.AutomaticLimit != 3 {
		t.Fatal("node continuation changed completed review allowance")
	}
	for _, p := range d.run.PendingActions {
		if p.ReviewSeries == review.ProductReview {
			t.Fatal("exhausted review was dispatched without its original grant")
		}
	}
	confirmation, err := d.f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReviewRoundGrant), Choice: "grant", Reason: "user authorizes exactly one original product review round"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	d.drive()
	state = driverState(t, d)
	if len(state.ReviewSeries.ProductReview.Authorizations) != 1 {
		t.Fatal("grant created more than one review authorization")
	}
	count := 0
	for _, p := range d.run.PendingActions {
		if p.ReviewSeries == review.ProductReview {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("single grant issued %d product reviews", count)
	}
}

func TestNodeContinuationReopensRealChildCheckpointAndPreservesParentAuthority(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	child := children[0]
	old := readSliceReceipt(t, child)
	runs, err := master.f.ControlRuns()
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	for _, run := range runs {
		if run.RunID == child.f.runID {
			break
		}
		offset += len(run.BusinessNodes)
	}
	local := ControlInput{Node: businessNodeNumber(t, child.run, "slice-ready")}
	fromChild, err := child.f.PrepareCurrentControl(local)
	if err != nil {
		t.Fatal(err)
	}
	global := local
	global.Node += offset
	fromParent, err := master.f.PrepareCurrentControl(global)
	if err != nil {
		t.Fatal(err)
	}
	fromParent.Input = fromChild.Input
	if !reflect.DeepEqual(fromParent, fromChild) {
		t.Fatal("parent node number did not preserve exact original child preparation")
	}
	continueBusinessNode(child, "slice-ready")
	child.drive()
	if fresh := readSliceReceipt(t, child); fresh.Digest != old.Digest || !reflect.DeepEqual(fresh.Receipt, old.Receipt) {
		t.Fatal("slice-ready target rewrote completed checkpoint facts")
	}
	gate := driverState(t, child).SelectedGates[0]
	decision := continueBusinessNode(child, "gate:"+gate)
	child.drive()
	state := driverState(t, child)
	if state.SliceReceipt != nil || len(state.SliceReceiptHistory) == 0 || state.SliceReceiptHistory[len(state.SliceReceiptHistory)-1].Digest != old.Digest {
		t.Fatal("child continuation lost its original frozen checkpoint")
	}
	parent := driverState(t, master)
	known := parent.SliceReceipts[child.f.runID]
	if known.Continuation == nil || known.Continuation.Decision.RequestID != string(decision.Event.Decide.Request) || parent.SliceReceiptQualified(known) {
		t.Fatal("parent kept superseded checkpoint eligible")
	}
	if err := master.f.engine.AcceptSliceReceipt(old, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	parent = driverState(t, master)
	if parent.SliceReceipts[child.f.runID].Continuation == nil || parent.SliceReceiptQualified(parent.SliceReceipts[child.f.runID]) {
		t.Fatal("late original sidecar cleared new child authority")
	}
	id, in := child.gateAction(gate)
	child.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	child.drive()
	fresh := readSliceReceipt(t, child)
	if fresh.Receipt.LastRequest == nil || fresh.Receipt.LastRequest.RequestID != string(decision.Event.Decide.Request) || fresh.Digest == old.Digest {
		t.Fatal("replacement checkpoint lost exact user authority")
	}
	parent = driverState(t, master)
	if !parent.SliceReceiptQualified(parent.SliceReceipts[child.f.runID]) {
		t.Fatal("new checkpoint failed original parent qualification")
	}
}

func TestNodeContinuationRetainsMainlinePrefixAndRejectsOrdinaryReturnToChild(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	continueBusinessNode(master, "mainline-integration")
	master.drive()
	state := driverState(t, master)
	if state.Validation.Candidate == nil || !state.SliceEnteredMainline(children[0].f.runID) {
		t.Fatal("integration target did not run its original native owner")
	}
	original := state.Validation.Candidate.Identity
	continueBusinessNode(master, "mainline-integration")
	master.drive()
	if driverState(t, master).Validation.Candidate.Identity != original {
		t.Fatal("integration continuation replayed completed native prefix")
	}
	child := children[0]
	child.run, _ = child.f.Show()
	request, err := child.f.PrepareCurrentControl(ControlInput{Request: protocol.ControlContinueNode, Node: businessNodeNumber(t, child.run, "development:command")})
	if err != nil {
		t.Fatal(err)
	}
	if !request.Continuation.EnteredMainline || len(request.Continuation.To.Missing) == 0 {
		t.Fatal("child preview missed original entered-mainline fact")
	}
	_, child.run, err = submitControlWithHost(t, child.f, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, ask := range child.run.PendingAsks {
		if ask.Control == protocol.ControlContinueNode {
			for _, option := range ask.Options {
				if option.ID == "approve" {
					t.Fatal("unavailable child target offered confirmation")
				}
			}
		}
	}
	path := filepath.Join(child.f.stateDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = child.f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, child.run, protocol.ControlContinueNode), Choice: "approve", Reason: "attempt ordinary return to child"})
	if err == nil {
		t.Fatal("ordinary child return bypassed original mainline ownership")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected child target changed current", err)
	}
}

func TestNodeContinuationChildGrantRecoversThroughOriginalParentObservation(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	child := children[0]
	old := readSliceReceipt(t, child)
	gate := driverState(t, child).SelectedGates[0]
	request, err := child.f.PrepareCurrentControl(ControlInput{Node: businessNodeNumber(t, child.run, "gate:"+gate)})
	if err != nil {
		t.Fatal(err)
	}
	_, child.run, err = submitControlWithHost(t, child.f, request)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := child.f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, child.run, protocol.ControlContinueNode), Choice: "approve", Reason: "user selects fresh child validation"})
	if err != nil {
		t.Fatal(err)
	}
	once := false
	store, err := persistence.NewStore(child.f.stateDir, persistence.Config{PackageDigest: child.f.envelope.PackageDigest, DefinitionSource: child.f.envelope.DefinitionSource, InstalledTargetIdentity: child.f.envelope.InstalledTargetIdentity, AcquireAdmission: child.f.admission.acquire, FaultInjector: func(point persistence.FaultPoint) error {
		if point == persistence.FaultCommitResponseLost && !once {
			once = true
			return errors.New("child confirmation response lost before parent observation")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := protocol.New(store, protocol.Config{NativeRoot: driverState(t, child).NativeRoot, Definition: child.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(child.f.root, child.f.runID), ClaimDispatch: dispatchClaimBridge(child.f.root, child.f.runID, child.f.admission)})
	if err != nil {
		t.Fatal(err)
	}
	child.f.store, child.f.engine = store, engine
	if _, _, err = submitControlWithHost(t, child.f, confirmation); err == nil {
		t.Fatal("normal child commit-response loss did not interrupt parent observation")
	}
	parent := driverState(t, master)
	if parent.SliceReceipts[child.f.runID].Continuation != nil {
		t.Fatal("parent was already synchronized before the injected interruption")
	}
	master.reopen()
	master.drive()
	parent = driverState(t, master)
	known := parent.SliceReceipts[child.f.runID]
	if known.Continuation == nil || known.Continuation.Decision.RequestID != string(confirmation.Event.Decide.Request) || parent.SliceReceiptQualified(known) || known.Digest != old.Digest || !reflect.DeepEqual(known.Receipt, old.Receipt) {
		t.Fatal("parent recovery did not observe exact committed child authority before integration")
	}
	if parent.SliceEnteredMainline(child.f.runID) {
		t.Fatal("parent integrated stale checkpoint before recovery observation")
	}
}

func TestNodeContinuationShowsOriginalSignedIntegrationClaimBeforeChildChoice(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	interruption := &mergeInterruption{Runner: vcs.ExecRunner{}, point: "merge"}
	master.f.vcsRunner = interruption
	// Use the existing native response-loss producer and its exact signed input.
	master.f.vcs = vcs.New(master.f.root, interruption)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("signed native merge interruption was not reached")
			}
		}()
		_, _ = master.f.Drive()
	}()
	parent := driverState(t, master)
	var child *validationDriver
	for _, candidate := range children {
		if len(parent.SliceIntegrationClaims(candidate.f.runID)) != 0 {
			child = candidate
			break
		}
	}
	if child == nil {
		t.Fatal("original signed child integration claim missing")
	}
	claim := parent.PendingHostActions[parent.SliceIntegrationClaims(child.f.runID)[0]]
	unknown, err := protocol.NewAdapterHostActionReceiptEvent(master.eventID(), claim.ActionID, claim.Adapter.Operation, master.host(), claim.Correlation, claim.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(unknown)
	if driverState(t, master).HostActionReceipts[claim.ActionID].Status != protocol.HostActionStatusUnknown {
		t.Fatal("response loss did not retain the original UNKNOWN native claim")
	}
	child.run, _ = child.f.Show()
	request, err := child.f.PrepareCurrentControl(ControlInput{Node: businessNodeNumber(t, child.run, "development:command")})
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Continuation.ParentClaims) == 0 || request.Continuation.To.Unavailable == "" {
		t.Fatal("preview missed exact pending mainline input")
	}
	_, child.run, err = submitControlWithHost(t, child.f, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, ask := range child.run.PendingAsks {
		if ask.Control == protocol.ControlContinueNode {
			for _, option := range ask.Options {
				if option.ID == "approve" {
					t.Fatal("pending integration claim offered ordinary child mutation")
				}
			}
		}
	}
	master.reopen()
	master.drive()
	if !driverState(t, master).SliceEnteredMainline(child.f.runID) {
		t.Fatal("original mainline owner did not reconcile its executed native prefix")
	}
}

func TestNodeContinuationKeepsIndependentDesignRunningDuringDeveloperStop(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
			dev, _ := d.action("engine.development.batch.v6")
			design, input := d.action("engine.qa.design.v6")
			d.receipt(dev)
			d.receipt(design)
			original := driverState(t, d).DispatchBindings[design]
			continueBusinessNode(d, "development:command")
			if _, exists := d.run.PendingActions[design]; !exists || driverState(t, d).DispatchBindings[design] != original {
				t.Fatal("selected developer stopped independent design")
			}
			manifest := simpleManifest(input)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: design, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "independent-design-completed", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest}}})
			if _, accepted := driverState(t, d).Results[design]; !accepted {
				t.Fatal("independent result was quarantined by node continuation")
			}
			var signed protocol.WorkspaceParameters
			for _, intent := range driverState(t, d).PendingHostActions {
				if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" {
					data, _ := json.Marshal(intent.Adapter.Params)
					var params protocol.WorkspaceParameters
					if json.Unmarshal(data, &params) == nil && params.Manifest != nil && params.Task != nil {
						signed = params
					}
				}
			}
			if signed.Task == nil {
				t.Fatal("original design did not produce its signed review input")
			}
			if reopen {
				d.reopen()
			}
			d.completeWorkerTerminations()
			var err error
			d.run, err = d.f.Drive()
			if err != nil {
				state := driverState(t, d)
				original, _ := json.Marshal(struct {
					Pending  map[string]protocol.HostActionIntent
					Receipts map[string]protocol.HostActionReceipt
				}{state.PendingHostActions, state.HostActionReceipts})
				t.Fatalf("independent owner continuation: %v; exact native facts=%s", err, original)
			}
			d.action("engine.development.batch.v6")
			reviewer, reviewInput := d.action("engine.qa.review.rationale.v2")
			if reviewInput.Binding.Task != *signed.Task || reviewInput.Binding.WorkspaceID != signed.Resource.ID || reviewInput.Binding.Generation != signed.Generation || reviewInput.Binding.RequirementRevision != signed.RequirementRevision || !reflect.DeepEqual(reviewInput.Manifest, *signed.Manifest) {
				t.Fatal("unrelated developer epoch replaced independent review source")
			}
			approved := approvedReview(reviewInput.Manifest)
			d.business(reviewer, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &approved})
			d.drive()
			if driverState(t, d).Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
				t.Fatal("carried source review did not freeze through original result owner")
			}
		})
	}
}

func TestNodeContinuationCarriedReviewSurvivesReplayAndHonorsRedo(t *testing.T) {
	for _, target := range []string{"qa-review:BLACKBOX", "qa-design:BLACKBOX", "requirement-review", "requirement-design"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
			dev, _ := d.action("engine.development.batch.v6")
			design, input := d.action("engine.qa.design.v6")
			d.receipt(dev)
			d.receipt(design)
			continueBusinessNode(d, "development:command")
			manifest := simpleManifest(input)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: design, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "independent-design-completed", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest}}})
			d.completeWorkerTerminations()
			d.drive()
			reviewer, reviewInput := d.action("engine.qa.review.rationale.v2")
			d.receipt(reviewer)
			before := driverState(t, d)
			d.drive()
			d.reopen()
			d.drive()
			replayed, replayInput := d.action("engine.qa.review.rationale.v2")
			if replayed != reviewer || !reflect.DeepEqual(replayInput, reviewInput) || !reflect.DeepEqual(driverState(t, d).HostActionReceipts, before.HostActionReceipts) {
				t.Fatal("replay changed the carried review or repeated its native workspace")
			}
			nextHandler := "engine.qa.review.rationale.v2"
			switch target {
			case "requirement-review":
				confirmValidationWork(d, reviewer)
				nextHandler = "engine.qa.design.v6"
			case "requirement-design":
				confirmValidationWork(d, design)
				nextHandler = "engine.qa.design.v6"
			default:
				continueBusinessNode(d, target)
				if target == "qa-design:BLACKBOX" {
					nextHandler = "engine.qa.design.v6"
				}
			}
			d.completeWorkerTerminations()
			d.drive()
			fresh, freshInput := d.action(nextHandler)
			if fresh == reviewer || freshInput.Binding.Task == reviewInput.Binding.Task {
				t.Fatal("explicit redo retained the carried review responsibility")
			}
			approved := approvedReview(reviewInput.Manifest)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: reviewer, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "superseded-review-completed", Validation: &protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &approved}}})
			state := driverState(t, d)
			if _, accepted := state.Results[reviewer]; accepted || state.Validation.Sets[coverage.ReviewBlackbox].Status == "FROZEN" {
				t.Fatal("superseded carried review froze the replacement source")
			}
		})
	}
}

func TestNodeContinuationEntersOriginalRepairResponsibility(t *testing.T) {
	d, original, action := nativeRepairReady(t, "P2")
	d.receipt(action)
	var target string
	for _, n := range d.run.BusinessNodes {
		if n.Kind == "repair" && n.Task != nil && *n.Task == original.Binding.Task {
			target = n.ID
		}
	}
	if target == "" {
		t.Fatal("original repair obligation is absent from business choices")
	}
	continueBusinessNode(d, target)
	d.completeWorkerTerminations()
	d.drive()
	fresh, input := d.action("engine.repair.v7")
	if fresh == action || input.Binding.RequirementRevision != original.Binding.RequirementRevision || input.Binding.WaveID != original.Binding.WaveID {
		t.Fatal("repair target did not enter the original signed obligation")
	}
}

func TestNodeContinuationAbortUsesOriginalNativeCleanup(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "quality")
	for id := range d.run.PendingActions {
		d.receipt(id)
	}
	continueBusinessNode(d, "abort")
	d.completeWorkerTerminations()
	d.drive()
	if d.run.Status != "ABORTED" || d.run.Termination == nil || !d.run.Termination.ResourcesCleaned() {
		t.Fatal("node abort bypassed original cleanup completion")
	}
	if len(d.run.BusinessNodes) != 0 {
		t.Fatal("terminal offered new node continuation")
	}
}

func TestNodeContinuationCleanupWaitsForOriginalDeliveryAndCompletes(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root, "implementation")[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	report := executionFor(qi)
	d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report}))
	action, input := d.gateAction("quality")
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	decision := continueBusinessNode(d, "cleanup")
	state := driverState(t, d)
	if state.Decisions[string(decision.Event.Decide.Request)].Continuation.AppliedRevision != 0 || d.run.Status != "ACTIVE" {
		t.Fatal("cleanup target supplied a missing promotion or cleanup fact")
	}
	if _, err := d.f.PrepareCandidate("deliver original verified candidate"); err != nil {
		t.Fatal(err)
	}
	d.drive()
	if d.run.Status != "COMPLETE" || d.run.Termination == nil || !d.run.Termination.ResourcesCleaned() {
		t.Fatal("cleanup target did not converge through original delivery and cleanup")
	}
}
