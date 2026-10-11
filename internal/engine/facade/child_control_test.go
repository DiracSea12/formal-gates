package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Coverage: pkg.Coverage, BaseRevision: ask.BaseRevision, CandidateRevision: pkg.CandidateRevision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, RedoProductReview: pkg.RedoProductReview, RedoStartReadiness: pkg.RedoStartReadiness, ResetRequestID: pkg.ResetRequestID, Reason: "confirm the complete registered requirement and solution"}}})
	state := driverState(t, d)
	if state.Validation != nil {
		generation := state.RequirementRevisions[len(state.RequirementRevisions)-1].StateRevision
		if state.Validation.Generation != generation {
			t.Fatal("reset inventory does not belong to its accepted local confirmation")
		}
		for id, intent := range state.PendingHostActions {
			if intent.Adapter == nil || intent.Adapter.Operation != "op.workspace" && intent.Adapter.Operation != "op.candidate" {
				continue
			}
			raw, err := json.Marshal(intent.Adapter.Params)
			if err != nil {
				t.Fatal(err)
			}
			var params struct {
				Generation uint64 `json:"generation"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				t.Fatal(err)
			}
			if params.Generation != generation {
				t.Fatalf("confirmed reset issued stale native work: action=%s intent=%d confirmation=%d", id, params.Generation, generation)
			}
		}
	}
	retainResetState(t, d, "confirmed")
}

// Preserve exact confirmation-boundary states as well as the final scenario.
// The existing test evidence sink has no effect on production behavior.
func retainResetState(t *testing.T, d *validationDriver, authority string) {
	t.Helper()
	destination := os.Getenv("FORMAL_GATES_TEST_EVIDENCE")
	if destination == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination, t.Name(), "reset-authority", authority+"-state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResetPreservesNativeAndScopedAuthority(t *testing.T) {
	master, child, _, ready := deliveredChildCheckpoint(t, validationDocument)
	before := driverState(t, child)
	parentBefore := driverState(t, master)
	raw, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	reset := resetControl(t, child)
	child.drive()
	pending := driverState(t, child)
	if !pending.ResetPending() || !pending.Decisions[string(reset.Decide.Request)].Reset.Applied {
		t.Fatal("reset did not preserve its pending confirmation authority")
	}
	retainResetState(t, child, "unconfirmed")
	unchanged, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := child.f.engine.SyncParentRequirement(driverState(t, master), emptyFingerprint()); err != nil {
			t.Fatal(err)
		}
	}
	observation := protocol.Event{ID: protocol.EventID(child.eventID()), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: pending.RequirementRevision(), Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: pending.RequirementRevision(), Document: string(raw)}}}
	if _, _, err := child.f.Submit(observation); err == nil {
		t.Fatal("ordinary requirement observation bypassed reset re-confirmation")
	} else {
		var rejected *protocol.RejectedError
		if !errors.As(err, &rejected) || rejected.Code != protocol.CodeValidationEvidenceRejected {
			t.Fatal("pending reset did not reject through the requirement inventory owner", err)
		}
	}
	afterObservation, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if err != nil || !bytes.Equal(unchanged, afterObservation) {
		t.Fatal("parent sync or rejected observation changed pending reset authority", err)
	}
	child.reopen()
	confirmResetControl(t, child)
	state := driverState(t, child)
	assertResetResourceOwnership(t, child, before, reset)
	after, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) || state.BaseSnapshot != before.BaseSnapshot || state.InitialSnapshot != before.InitialSnapshot || state.Validation.DevelopmentSnapshot != before.Validation.DevelopmentSnapshot || state.AcceptedNativeSnapshot() != before.AcceptedNativeSnapshot() || state.RouteMode != before.RouteMode || !reflect.DeepEqual(state.SelectedGates, before.SelectedGates) || !reflect.DeepEqual(state.Topology, before.Topology) || state.SliceChild.RequirementRevision != before.SliceChild.RequirementRevision {
		t.Fatal("reset changed native content or child origin")
	}
	if !reflect.DeepEqual(driverState(t, master).ReviewSeries, parentBefore.ReviewSeries) || len(state.ReviewSeries.ProductReview.RawCandidates) != 0 {
		t.Fatal("child reset changed parent review authority")
	}
	parent := driverState(t, master)
	if parent.SliceReceipts[child.f.runID].Digest != ready.Digest || parent.SliceReceiptCurrent(ready) || parent.SliceResetAuthorities[child.f.runID].Decision.RequestID != string(reset.Decide.Request) {
		t.Fatal("child reset changed immutable evidence or retained its successful qualification")
	}
	tool, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "tool"))
	mode, modeErr := os.Stat(filepath.Join(child.f.vcs.Root(), "tool"))
	if err != nil || modeErr != nil || string(tool) != "#!/bin/sh\nexit 0\n" || mode.Mode().Perm() != 0755 {
		t.Fatal("reset changed accepted executable content or mode", err, modeErr)
	}
	if _, _, err = child.f.Submit(reset); err != nil {
		t.Fatal("reset replay was not stable", err)
	}
	child.reopen()
	child.drive()
	fresh := driverState(t, child)
	if fresh.Validation.Generation <= before.Validation.Generation || len(fresh.Validation.Waves) != 1 || fresh.Validation.Waves[0].Status != "COLLECTING" {
		t.Fatal("reset did not rebuild the current validation obligations")
	}
	for _, target := range fresh.Validation.Waves[0].Targets {
		for _, old := range before.Validation.Waves {
			if old.Targets[target.Task.String()] != nil {
				t.Fatal("reset reused a retired validation task")
			}
		}
	}
	completeSliceGates(t, child)
	child.drive()
	current := driverState(t, master)
	receipt := current.SliceReceipts[child.f.runID]
	if receipt.Digest == ready.Digest || !current.SliceReceiptCurrent(receipt) || len(current.SliceReceiptHistory) != 1 || current.SliceReceiptHistory[0].Digest != ready.Digest || receipt.Receipt.ResetRequestID != string(reset.Decide.Request) {
		t.Fatal("reset did not replace the retired child qualification exactly once")
	}
	acceptedCost := current.Cost
	child.reopen()
	child.drive()
	master.reopen()
	master.drive()
	current = driverState(t, master)
	if current.SliceReceipts[child.f.runID].Digest != receipt.Digest || len(current.SliceReceiptHistory) != 1 || !reflect.DeepEqual(current.Cost, acceptedCost) {
		t.Fatal("reset receipt replay changed accepted evidence or cost")
	}
}

// Native cleanup proves ownership by registered resource identity. Every
// scratch path and ref in the accepted preview must disappear, while the
// actual development workspace and native target retain their original bytes.
func assertResetResourceOwnership(t *testing.T, d *validationDriver, before *protocol.State, reset protocol.Event) {
	t.Helper()
	state := driverState(t, d)
	preview := state.PendingAsks[string(reset.Decide.Request)].Reset
	var cleaned []protocol.NativeResource
	roles := map[string]bool{}
	for id, resource := range before.NativeResources {
		if resource.Status == "CLEANED" {
			continue
		}
		included := slices.ContainsFunc(preview.Resources, func(r protocol.NativeResource) bool { return r.ID == id })
		if resource.Role == "gate" || resource.Role == "candidate" {
			roles[resource.Role] = true
			if resource.Identity != before.Validation.DevelopmentSnapshot || !included {
				t.Fatal("distinct equal-snapshot scratch was excluded from reset cleanup", resource)
			}
		}
		if included {
			want := resource
			want.Status = "CLEANED"
			if state.NativeResources[id] != want {
				t.Fatal("reset did not accept exact cleanup resource evidence", resource)
			}
			if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
				t.Fatal("reset left a registered scratch path", resource.Path, err)
			}
			if _, err := (vcs.ExecRunner{}).Run(d.f.vcs.Root(), "git", "show-ref", "--verify", resource.Ref); err == nil {
				t.Fatal("reset left a registered scratch ref", resource.Ref)
			}
		} else if resource.Role == "development" {
			if state.NativeResources[id] != resource {
				t.Fatal("reset changed retained development registration")
			}
			for _, root := range []string{resource.Path, d.f.vcs.Root()} {
				tree, err := vcs.New(root, nil).TreeDigest(before.Validation.DevelopmentSnapshot)
				if err != nil || tree != before.Validation.Candidate.Digest {
					t.Fatal("retained development/native target lost its complete candidate", root, tree, err)
				}
			}
		}
	}
	if !roles["gate"] || !roles["candidate"] {
		t.Fatal("reset fixture did not create both distinct equal-snapshot scratch roles")
	}
	for _, resource := range preview.Resources {
		resource.Status = "CLEANED"
		cleaned = append(cleaned, resource)
	}
	for _, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation != "op.cleanup" || receipt.Status != protocol.HostActionStatusExecuted {
			continue
		}
		var evidence protocol.WorkspaceCleanupReceipt
		raw, err := json.Marshal(receipt.AdapterEvidence.Values)
		if err != nil || json.Unmarshal(raw, &evidence) != nil || !reflect.DeepEqual(evidence.Resources, cleaned) || evidence.ParametersDigest != receipt.PayloadDigest {
			t.Fatal("durable reset cleanup receipt differs from the exact approved scope")
		}
		return
	}
	t.Fatal("reset omitted its durable cleanup receipt")
}
func TestMasterResetCascadesOneConfirmation(t *testing.T) {
	master, child := controlChildren(t)
	for id := range master.run.PendingActions {
		master.receipt(id)
	}
	resetControl(t, master)
	settleResetWorkerStops(t, master)
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

func settleResetWorkerStops(t *testing.T, d *validationDriver) {
	t.Helper()
	for {
		var action string
		var intent protocol.HostActionIntent
		for id, pending := range d.run.AdapterIntents {
			if pending.Terminate != nil {
				action, intent = id, pending
				break
			}
		}
		if action == "" {
			return
		}
		for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
			event, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), intent.Terminate.Identity, edge)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(event)
		}
		event, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), action, intent.Operation, d.host(), action, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(event)
	}
}

func TestResetRebuildsStandaloneValidationFromRetainedDevelopment(t *testing.T) {
	for _, gates := range [][]string{{"quality", "security"}, {"blackbox", "security"}} {
		t.Run(fmt.Sprint(gates), func(t *testing.T) {
			d := nativeMasterDriver(t)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
			d.confirmRoute(routeDecisionForGates(gates))
			d.drive()
			id, input := planBatchAction(d, "command")
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app_test.go"), []byte("package main\n"), 0600); err != nil {
				t.Fatal(err)
			}
			evidence := batchEvidence(input)
			evidence.Paths = append(evidence.Paths, "app_test.go")
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: evidence})
			d.drive()
			freezeCases := func() {
				if !slices.Contains(gates, "blackbox") {
					return
				}
				id, input := d.action("engine.qa.design.v4")
				manifest := simpleManifest(input)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
				d.drive()
				id, input = d.action("engine.qa.review.rationale.v2")
				review := approvedReview(manifest)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &review})
				d.drive()
			}
			freezeCases()
			before := driverState(t, d)
			for id := range d.run.PendingActions {
				d.receipt(id)
			}
			reset := resetControl(t, d)
			settleResetWorkerStops(t, d)
			d.reopen()
			confirmResetControl(t, d)
			d.drive()
			passPreDevelopmentReviews(t, d, commandPlan())
			freezeCases()
			fresh := driverState(t, d)
			if fresh.Validation.DevelopmentSnapshot != before.Validation.DevelopmentSnapshot || fresh.BaseSnapshot != before.BaseSnapshot || fresh.AcceptedNativeSnapshot() != before.AcceptedNativeSnapshot() || len(fresh.Validation.Waves) != 1 {
				t.Fatal("standalone reset changed retained development or failed to rebuild validation")
			}
			for _, target := range fresh.Validation.Waves[0].Targets {
				for _, old := range before.Validation.Waves {
					if old.Targets[target.Task.String()] != nil {
						t.Fatal("standalone reset reused a retired validation duty")
					}
				}
			}
			if slices.Contains(gates, "blackbox") {
				id, input := d.action("engine.qa.execution.v3")
				report := executionFor(input)
				tool, environment := protocol.ExecutionIdentities(input.ExecutionFacts)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
			}
			for _, target := range fresh.Validation.Waves[0].Targets {
				if target.Kind == "GATE" {
					id, input := d.gateAction(target.Gate)
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
				}
			}
			d.drive()
			if d.run.Status != "COMPLETE" {
				t.Fatal("current standalone validation did not reach native delivery")
			}
			if _, _, err := d.f.Submit(reset); err != nil {
				t.Fatal("completed reset replay lost its original acceptance", err)
			}
		})
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
	owner := filepath.Join(t.TempDir(), "owner.jsonl")
	writeTranscript(t, owner, []string{intervalBaseLine})
	master := receiptMaster(t, OwnerBinding{Transcript: owner, Provider: meteredProvider})
	child := receiptChild(t, master, "core", "lane-core", "custom")
	promoteReceiptChild(t, child)
	writeTranscript(t, owner, []string{intervalBaseLine, intervalDuringLine})
	ready := publishReceiptChild(t, child)
	child.drive()
	if ready.Receipt.Cost.Owner == nil || *ready.Receipt.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 17, InputCacheHitTokens: 2, InputCacheMissTokens: 15, OutputTokens: 8}) {
		t.Fatal("successful receipt lacks its actual nonzero owner interval")
	}
	readyCost := driverState(t, master).Cost
	abortRun(t, child)
	child.drive()
	master.drive()
	s := driverState(t, master)
	accepted := s.SliceReceipts[child.f.runID]
	if accepted.Receipt.Status != "ABORTED" || accepted.Receipt.Abort == nil || accepted.Digest == ready.Digest {
		t.Fatal("abort did not replace successful receipt")
	}
	if !reflect.DeepEqual(accepted.Receipt.Cost.Owner, ready.Receipt.Cost.Owner) || !reflect.DeepEqual(s.Cost, readyCost) || len(s.SliceReceiptHistory) != 1 || s.SliceReceiptHistory[0].Digest != ready.Digest {
		t.Fatal("receipt replacement duplicated nonzero owner usage or lost original evidence")
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

func requirementChildChoices(t *testing.T, d *validationDriver) []protocol.ChildRequirementDecision {
	t.Helper()
	// A child receipt can update the master independently of its last Drive.
	// Build the confirmation package from the current public projection.
	var err error
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	choices, err := d.f.requirementChildren()
	if err != nil {
		t.Fatal(err)
	}
	for i := range choices {
		c := &choices[i]
		c.ReceiptDisposition, c.CandidateDisposition, c.RouteDisposition = protocol.RequirementWorkCarry, protocol.RequirementWorkCarry, protocol.RequirementWorkCarry
		c.Reason = "explicitly preserve unaffected child responsibilities"
		for j := range c.WorkDecisions {
			c.WorkDecisions[j].Disposition = protocol.RequirementWorkCarry
			c.WorkDecisions[j].Reason = c.Reason
		}
	}
	return choices
}

func TestResetRetainsResponsibilityUntilWorkerStop(t *testing.T) {
	_, child := controlChildren(t)
	plan := sliceLanePlan()
	dependent := plan.Batches[0]
	dependent.ID = "output"
	dependent.Subtasks = []protocol.DevelopmentSubtask{{ID: "format-output", Description: "Render the validated command outcome"}}
	dependent.Artifacts = []string{"output.go"}
	dependent.DependsOn = []string{"command"}
	plan.Batches = append(plan.Batches, dependent)
	child.confirmSliceRoute(protocol.RouteDecision{Mode: "default", Gates: []string{"quality"}}, plan)

	child.drive()
	id, input := planBatchAction(child, "command")
	if len(child.run.RequirementWork) != 2 {
		t.Fatal("reset fixture omitted the queued dependent development duty")
	}
	child.receipt(id)
	reset := resetControl(t, child)
	state := driverState(t, child)
	if _, pending := state.PendingActions[id]; !pending {
		t.Fatal("reset dropped the active worker responsibility")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app_test.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)}
	child.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: child.host(), Outcome: protocol.OutcomePass, PayloadDigest: "completed-before-stop", Validation: &result}})
	child.reopen()
	child.drive()
	var terminated bool
	for action, intent := range child.run.AdapterIntents {
		if intent.Terminate == nil || intent.Terminate.WorkerActionID != id {
			continue
		}
		terminated = true
		for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
			event, err := protocol.NewLifecycleEventEvent(child.eventID(), child.host(), intent.Terminate.Identity, edge)
			if err != nil {
				t.Fatal(err)
			}
			child.submit(event)
		}
		event, err := protocol.NewAgentHostActionReceiptEvent(child.eventID(), action, intent.Operation, child.host(), action, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
		if err != nil {
			t.Fatal(err)
		}
		child.submit(event)
	}
	if !terminated {
		t.Fatal("reset omitted original termination intent")
	}
	child.drive()
	if ask := child.run.PendingRequirementAsk; ask == nil || len(ask.Package.WorkDecisions) != 2 || !slices.ContainsFunc(ask.Package.WorkDecisions, func(c protocol.RequirementWorkDecision) bool {
		return c.Work.ActionID == id && c.Work.Task == input.Binding.Task && c.Disposition == protocol.RequirementWorkRedo
	}) || !slices.ContainsFunc(ask.Package.WorkDecisions, func(c protocol.RequirementWorkDecision) bool {
		return c.Work.IssuedRevision == 0 && c.Disposition == protocol.RequirementWorkRedo
	}) {
		t.Fatal("reset lost the original stopped or queued development obligation")
	}
	confirmResetControl(t, child)
	child.drive()
	state = driverState(t, child)
	if _, accepted := state.Results[id]; accepted {
		t.Fatal("pre-reset result requalified after reset")
	}
	if state.ResetPending() || len(state.PendingHostActions) != 0 {
		t.Fatal("reset did not settle the original responsibilities")
	}
	_, replacement := planBatchAction(child, "command")
	if replacement.Binding.Task == input.Binding.Task || replacement.Binding.Generation <= input.Binding.Generation || len(child.run.RequirementWork) != 2 {
		t.Fatal("reset did not rebuild the stopped and queued development responsibilities")
	}
	if _, _, err := child.f.Submit(reset); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalMaterialsFinishBeforeSeal(t *testing.T) {
	master, _ := mainlineChildren(t, "")
	for len(master.run.ValidationInputs) > 0 {
		submitMainlineTarget(t, master)
		if master.run.Validation.Waves[len(master.run.Validation.Waves)-1].Status == "COMPLETE" {
			break
		}
		master.drive()
	}
	beforeHandler := driverState(t, master)
	if beforeHandler.Phase == runtime.PhaseTerminal || !beforeHandler.FinalValidationComplete(master.f.definition) {
		t.Fatal("terminal fixture did not stop at the completed validation boundary")
	}
	path := filepath.Join(master.f.stateDir, "terminal-summary.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := master.f.Drive(); err == nil {
		t.Fatal("terminal material failure was hidden")
	}
	snap, err := master.f.engine.Load()
	if err != nil || snap.State.Phase == runtime.PhaseTerminal {
		t.Fatal("failed terminal material left SEALED", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	master.reopen()
	master.drive()
	if master.run.Status != "COMPLETE" {
		t.Fatal("same terminal handler did not recover")
	}
	summary, err := master.f.readSummary()
	if err != nil || summary.SliceGuarantee == nil {
		t.Fatal("active guarantee was omitted", err)
	}
	data, err := os.ReadFile(summary.SliceGuarantee.Path)
	if err != nil || !bytes.Contains(data, []byte("activation")) || !bytes.Contains(data, []byte("receipts")) || !bytes.Contains(data, []byte("validation")) {
		t.Fatal("guarantee sidecar omitted required content", err)
	}
	var material struct {
		RunID      string                        `json:"runId"`
		Activation *protocol.GuaranteeActivation `json:"activation"`
		Receipts   []protocol.SliceReceipt       `json:"receipts"`
		Validation *protocol.Validation          `json:"validation"`
	}
	if err := json.Unmarshal(data, &material); err != nil {
		t.Fatal(err)
	}
	var receipts []protocol.SliceReceipt
	for _, slice := range summary.Termination.Topology.Slices {
		receipts = append(receipts, summary.Termination.SliceReceipts[summary.Termination.SliceChildren[slice]])
	}
	digest := sha256.Sum256(data)
	if material.RunID != master.f.runID || !reflect.DeepEqual(material.Activation, summary.Termination.RequirementGuarantee) || !reflect.DeepEqual(material.Receipts, receipts) || !reflect.DeepEqual(material.Validation, summary.Termination.Validation) || summary.SliceGuarantee.Digest != hex.EncodeToString(digest[:]) {
		t.Fatal("guarantee material differs from the concluded activation, claims or final FULL evidence")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	master.reopen()
	master.drive()
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("repeated terminal drive changed immutable summary bytes", err)
	}
}

func TestTopologyRebuildPreservesCarriedChildOrigin(t *testing.T) {
	master, child, _, ready := deliveredChildCheckpoint(t, validationDocument)
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
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:topology", ImpactDigest: "sha256:affected-slice", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, RedoTopology: true, Children: choices, Impact: &protocol.RequirementImpact{ChangedItems: []string{"affected slice boundary"}, TransitiveImpacts: []string{"replacement route"}, CarryReasons: []string{"core evidence and ownership unchanged"}}}
	for _, work := range master.run.RequirementWork {
		pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master obligation"})
	}
	request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm complete topology impact"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	ask := master.run.PendingRequirementAsk
	master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, RedoStartReadiness: pkg.RedoStartReadiness, RedoTopology: true, Children: choices, Impact: pkg.Impact, Reason: "preserve exact core and replace affected route and its development plan"}}})
	master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
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
	master.reopen()
	master.drive()
	state := driverState(t, master)
	if state.SliceChildren["lane-core"] != child.f.runID || state.SliceReceipts[child.f.runID].Digest != ready.Digest || state.SliceChildren["lane-io"] == before.SliceChildren["lane-tooling"] || state.SliceChildren["lane-io"] == "" || state.SliceChildren["lane-tooling"] != "" || state.ChildCreation.Previous == nil {
		t.Fatal("topology rebuild discarded carried origin or reused the affected child")
	}
	for _, old := range before.ChildCreation.Children {
		resource := state.NativeResources[old.ResourceID]
		if old.RunID == child.f.runID {
			if resource != before.NativeResources[old.ResourceID] {
				t.Fatal("topology cleanup changed the carried target registry")
			}
			if _, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "show-ref", "--verify", resource.Ref); err != nil {
				t.Fatal("topology cleanup removed the carried target ref", err)
			}
		} else {
			if resource.Status != "CLEANED" {
				t.Fatal("topology cleanup retained retired target debt")
			}
			if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
				t.Fatal("topology cleanup retained the retired worktree", err)
			}
			if _, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "show-ref", "--verify", resource.Ref); err == nil {
				t.Fatal("topology cleanup retained the retired target ref")
			}
		}
	}
	tool, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "tool"))
	mode, modeErr := os.Stat(filepath.Join(child.f.vcs.Root(), "tool"))
	if err != nil || modeErr != nil || string(tool) != "#!/bin/sh\nexit 0\n" || mode.Mode().Perm() != 0755 {
		t.Fatal("topology cleanup changed carried executable bytes or mode", err, modeErr)
	}
	carried := driverState(t, child)
	if carried.SliceChild.RequirementRevision != ready.Receipt.RequirementRevision || carried.SliceReceipt.Digest != ready.Digest {
		t.Fatal("topology carry rewrote frozen child evidence")
	}
}

func TestMasterRequirementCarryRetainsOriginalNativePrefix(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	runner := &mergeInterruption{Runner: vcs.ExecRunner{}, point: "later-commit"}
	master.f.vcsRunner, master.f.vcs = runner, vcs.New(master.f.root, runner)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("native commit response loss was not reached")
			}
		}()
		_, _ = master.f.Drive()
	}()
	original := driverState(t, master)
	var workspace string
	for _, intent := range original.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			var p protocol.CandidateParameters
			if err := decodeNativeParams(intent, &p); err != nil {
				t.Fatal(err)
			}
			if p.Integration != nil {
				workspace = p.WorkspaceID
			}
		}
	}
	if workspace == "" {
		t.Fatal("fixture omitted the original merge responsibility")
	}
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	master.run, _ = master.f.Show()
	confirmValidationDocument(master, "", nil, document)
	master.reopen()
	master.f.vcsRunner, master.f.vcs = runner, vcs.New(master.f.root, runner)
	master.drive()
	state := driverState(t, master)
	if runner.merges != 3 || runner.commits != 3 || state.Validation.Candidate == nil {
		t.Fatal("requirement CARRY repeated or lost the native prefix", runner.merges, runner.commits)
	}
	for _, intent := range state.ReconciledEffects {
		if intent.Intent == nil || intent.Intent.Adapter == nil || intent.Intent.Adapter.Operation != "op.candidate" {
			continue
		}
		var p protocol.CandidateParameters
		if err := decodeNativeParams(*intent.Intent, &p); err != nil {
			t.Fatal(err)
		}
		if p.Integration != nil && p.WorkspaceID != workspace {
			t.Fatal("carried native prefix changed its registered workspace")
		}
	}
	for _, child := range children {
		if readSliceReceipt(t, child).Digest != original.SliceReceipts[child.f.runID].Digest {
			t.Fatal("master carry rewrote child checkpoint bytes")
		}
	}
}

func TestParentRequirementRedoRequalifiesChildReceipt(t *testing.T) {
	master, child, _, ready := deliveredChildCheckpoint(t, validationDocument)
	physical := driverState(t, child)
	origin := physical.SliceChild.RequirementRevision
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	children := requirementChildChoices(t, master)
	for i := range children {
		if children[i].ChildRunID != child.f.runID {
			continue
		}
		children[i].ReceiptDisposition, children[i].CandidateDisposition = protocol.RequirementWorkRedo, protocol.RequirementWorkRedo
		for j := range children[i].WorkDecisions {
			if children[i].WorkDecisions[j].Work.Task.Step == "validation.gate" {
				children[i].WorkDecisions[j].Disposition = protocol.RequirementWorkRedo
			}
		}
	}
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:confirmed-document", ImpactDigest: "sha256:child-revalidation", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, Children: children, Impact: &protocol.RequirementImpact{ChangedItems: []string{"confirmed document"}, TransitiveImpacts: []string{"fresh child gates"}, CarryReasons: []string{"development and route remain unchanged"}}}
	for _, work := range master.run.RequirementWork {
		pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master inputs"})
	}
	request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm child revalidation"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	ask := master.run.PendingRequirementAsk
	master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, Children: children, Impact: pkg.Impact, Reason: "confirm the complete changed and carried inventory"}}})
	master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	if driverState(t, master).SliceReceiptQualified(ready) || driverState(t, master).SliceReceipts[child.f.runID].Digest != ready.Digest {
		t.Fatal("redo lost frozen evidence or retained its successful qualification")
	}
	redo := driverState(t, child)
	if redo.AcceptedNativeSnapshot() != physical.AcceptedNativeSnapshot() || !reflect.DeepEqual(redo.CandidatePromotion, physical.CandidatePromotion) || redo.PromotedSnapshot != physical.PromotedSnapshot || redo.PromotedDigest != physical.PromotedDigest || redo.FinalValidationComplete(child.f.definition) || redo.DeliveryComplete(child.f.definition) || redo.SliceReceipt != nil {
		t.Fatal("child redo confused accepted physical facts with current validation qualification")
	}
	if redo.ResetPreview().Current != physical.AcceptedNativeSnapshot() {
		t.Fatal("child redo reset preview lost its accepted target")
	}
	if observed, err := child.f.vcs.Resolve(); err != nil || observed != physical.AcceptedNativeSnapshot() {
		t.Fatal("child redo changed accepted native identity", observed, err)
	}
	tool, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), "tool"))
	mode, modeErr := os.Stat(filepath.Join(child.f.vcs.Root(), "tool"))
	if err != nil || modeErr != nil || string(tool) != "#!/bin/sh\nexit 0\n" || mode.Mode().Perm() != 0755 {
		t.Fatal("child redo changed accepted executable bytes or mode", err, modeErr)
	}
	child.reopen()
	child.drive()
	completeSliceGates(t, child)
	child.drive()
	current := driverState(t, master)
	receipt := current.SliceReceipts[child.f.runID]
	if receipt.Digest == ready.Digest || receipt.Receipt.RequirementRevision != revision || receipt.Receipt.OriginRequirementRevision != origin || !current.SliceReceiptCurrent(receipt) || len(current.SliceReceiptHistory) != 1 || current.SliceReceiptHistory[0].Digest != ready.Digest {
		t.Fatal("parent redo failed same-owner receipt replacement with the original child origin")
	}
	before, err := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	child.reopen()
	child.drive()
	master.reopen()
	master.drive()
	after, err := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("receipt replacement replay changed accepted evidence or cost", err)
	}
}

func TestParentAbortAfterUnconfirmedResetCleansBothChildren(t *testing.T) {
	master, _ := controlChildren(t)
	parent := driverState(t, master)
	if len(parent.ChildCreation.Children) != 2 {
		t.Fatal("fixture must have two original children")
	}
	var children []*validationDriver
	for _, plan := range parent.ChildCreation.Children {
		f, err := Open(master.f.root, plan.RunID, master.f.runtimeFiles)
		if err != nil {
			t.Fatal(err)
		}
		run, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: f.vcs}
		development := sliceLanePlan()
		development.Batches[0].Artifacts = []string{plan.SliceID + ".go"}
		child.confirmSliceRoute(protocol.RouteDecision{Mode: "default", Gates: []string{"quality"}}, development)
		child.drive()
		id, input := planBatchAction(child, "command")
		if err := os.WriteFile(filepath.Join(input.Workspace.Path, development.Batches[0].Artifacts[0]), []byte("package main\n"), 0600); err != nil {
			t.Fatal(err)
		}
		child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
		child.drive()
		for id := range child.run.PendingActions {
			child.receipt(id)
		}
		children = append(children, child)
	}
	for id := range master.run.PendingActions {
		master.receipt(id)
	}
	reset := resetControl(t, master)
	settleResetWorkerStops(t, master)
	master.drive()
	for _, child := range children {
		child.drive()
		settleResetWorkerStops(t, child)
		child.drive()
	}
	master.drive()
	before := driverState(t, master)
	if !before.ResetPending() || !before.Decisions[string(reset.Decide.Request)].Reset.Applied {
		t.Fatal("reset was not applied with confirmation still pending")
	}
	for _, child := range children {
		state := driverState(t, child)
		if state.SliceChild.ParentReset == nil || !state.ResetPending() || state.SliceChild.ParentChange == nil || !state.SliceChild.ParentChange.Pending {
			t.Fatal("original child did not retain its unconfirmed parent reset")
		}
		retainedDevelopment := false
		for _, resource := range state.NativeResources {
			retainedDevelopment = retainedDevelopment || resource.Role == "development" && resource.Status == "READY"
		}
		if !retainedDevelopment {
			t.Fatal("reset fixture has no retained original development resource")
		}
	}
	abortRun(t, master)
	master.reopen()
	master.drive()
	if master.run.Status != "ABORTED" || master.run.Termination == nil || !master.run.Termination.ResourcesCleaned() {
		t.Fatal("unconfirmed reset prevented parent abort cleanup", master.run.Status)
	}
	terminal := master.run.Termination
	if terminal.Decisions[string(reset.Decide.Request)].Reset == nil || terminal.Decisions[string(reset.Decide.Request)].Reset.Confirmed {
		t.Fatal("abort invented a reset confirmation or discarded original audit")
	}
	for _, child := range children {
		run, err := child.f.Show()
		if err != nil || run.Status != "ABORTED" || run.Termination == nil || !run.Termination.ResourcesCleaned() {
			t.Fatal("parent abort did not finish original child cleanup", run.Status, err)
		}
		state := run.Termination
		if state.SliceChild.ParentAbort == nil || !reflect.DeepEqual(state.SliceChild.ParentAbort.Decision, terminal.Decisions[state.SliceChild.ParentAbort.Decision.RequestID]) {
			t.Fatal("child abort lost original parent authority")
		}
		for _, decision := range state.Decisions {
			if decision.Control == protocol.ControlAbort {
				t.Fatal("cascade invented a child user abort")
			}
		}
		for _, resource := range state.NativeResources {
			if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
				t.Fatal("abort retained child resource", resource.ID, err)
			}
		}
	}
	if _, _, err := master.f.Submit(reset); err != nil {
		t.Fatal("original reset confirmation could not replay after abort", err)
	}
	master.reopen()
	master.drive()
	if master.run.Status != "ABORTED" || !reflect.DeepEqual(master.run.Termination.ReconciledEffects, terminal.ReconciledEffects) || !reflect.DeepEqual(master.run.Termination.Cost, terminal.Cost) {
		t.Fatal("repeat recovery changed abort cleanup or cumulative costs")
	}
}
