package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// confirmValidationWork submits the same semantic document with a formatting
// revision and the caller's explicit per-dispatch decisions.
func confirmValidationWork(d *validationDriver, redo string) {
	confirmValidationCoverage(d, redo, nil)
}
func confirmValidationCoverage(d *validationDriver, redo string, inventory *protocol.CoverageInventory) {
	d.t.Helper()
	confirmValidationDocument(d, redo, inventory, validationDocument+"\n")
}
func confirmValidationDocument(d *validationDriver, redo string, inventory *protocol.CoverageInventory, document string, redoReadiness ...bool) {
	d.t.Helper()
	redoStartReadiness := len(redoReadiness) > 0 && redoReadiness[0]
	receipt := driverState(d.t, d).CurrentIntakeConfirmation()
	if err := os.WriteFile(filepath.Join(d.f.root, receipt.RequirementSource), []byte(document), 0600); err != nil {
		d.t.Fatal(err)
	}
	intake, err := prepareConfirmedIntake(d.f.root, receipt, true)
	if err != nil {
		d.t.Fatal(err)
	}
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	var choices []protocol.RequirementWorkDecision
	for _, item := range d.run.RequirementWork {
		disposition := protocol.RequirementWorkCarry
		if item.ActionID == redo {
			disposition = protocol.RequirementWorkRedo
		}
		choices = append(choices, protocol.RequirementWorkDecision{Work: item, Disposition: disposition, Reason: "confirmed unchanged inputs except the selected fresh review"})
	}
	children := d.run.RequirementChildren
	for i := range children {
		children[i].RouteDisposition = protocol.RequirementWorkCarry
		children[i].ReceiptDisposition = protocol.RequirementWorkCarry
		children[i].CandidateDisposition = protocol.RequirementWorkCarry
		children[i].Reason = "formatting preserves the exact child inputs"
		for j := range children[i].WorkDecisions {
			children[i].WorkDecisions[j].Disposition = protocol.RequirementWorkCarry
			children[i].WorkDecisions[j].Reason = "unchanged child inputs"
		}
	}
	impact := &protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, TransitiveImpacts: []string{}, CarryReasons: []string{"all bound inputs preserve meaning"}}
	pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, Children: children, Impact: impact, Coverage: inventory, CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:work-selection", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: redoStartReadiness, WorkDecisions: choices}
	event, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, Children: children, Impact: impact, Coverage: inventory, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, RedoStartReadiness: pkg.RedoStartReadiness, WorkDecisions: choices, Reason: "confirmed each work decision"}}}.WithRequirementDocument(document))
}

// TestReadinessRedoOnBoundRouteConfirmsNewPlanByReselection：重走
// start-readiness 而不重走路线时绑定保持，新计划经一次直接路线重选（同
// 集＋新计划确认）即可确认并替换执行契约——重选通道常开，无死等。
func TestReadinessRedoOnBoundRouteConfirmsNewPlanByReselection(t *testing.T) {
	d := newValidationDriver(t, "quality")
	old, oldInput := planBatchAction(d, "command")
	var choices []protocol.RequirementWorkDecision
	for _, item := range d.run.RequirementWork {
		disposition := protocol.RequirementWorkCarry
		if item.ActionID == old {
			disposition = protocol.RequirementWorkRedo
		}
		choices = append(choices, protocol.RequirementWorkDecision{Work: item, Disposition: disposition, Reason: "the changed command contract replaces its dispatch; other duties carry"})
	}
	d.routeRedos++
	document := validationDocument + strings.Repeat("\n", d.routeRedos+1)
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	receipt := driverState(t, d).CurrentIntakeConfirmation()
	if err := os.WriteFile(filepath.Join(d.f.root, receipt.RequirementSource), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := prepareConfirmedIntake(d.f.root, receipt, true)
	if err != nil {
		t.Fatal(err)
	}
	pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:readiness-only", ImpactDigest: "sha256:command-contract", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, WorkDecisions: choices}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, WorkDecisions: choices, Reason: "readiness only; the route stays bound"}}}.WithRequirementDocument(document))
	d.drive()
	if !d.run.Review.RouteConfirmed {
		t.Fatalf("readiness-only redo changed the route binding: %t", d.run.Review.RouteConfirmed)
	}
	// 新计划的重走 readiness 轮（DoD 扩一项）。
	nextPlan := *commandPlan()
	nextPlan.Batches[0].DoD = append(nextPlan.Batches[0].DoD, protocol.DevelopmentCriterion{ID: "diagnostic", Description: "Invalid input reports its specific diagnostic"})
	var ready string
	for id, pending := range d.run.PendingActions {
		if pending.ReviewSeries == review.StartReadiness {
			ready = id
		}
	}
	if ready == "" {
		t.Fatal("readiness redo omitted the fresh round")
	}
	worker, issued := driverState(t, d).WorkerInput(ready, d.f.definition)
	if !issued {
		t.Fatal("new readiness has no original signed requirement points")
	}
	fixturePlanRequirements(&nextPlan, worker.RequirementPoints)
	d.receipt(ready)
	ev, err := protocol.NewReviewWorkerResultEvent(d.eventID(), ready, DefaultProvider, "new plan", protocol.ReviewResultEvidence{Action: review.StartReadiness, DispatchID: ready, RequirementRevision: revision, Status: "PASS", GranularityReview: &nextPlan, Findings: []review.Finding{}})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(ev)
	verifyReviewFixture(t, d, ev)
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch.v6" {
			t.Fatal("new contract executed before plan confirmation")
		}
	}
	// 直接同集重选即新计划的确认通道（路线未解除绑定）。
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	id, in := planBatchAction(d, "command")
	if id == old || in.Binding.Task == oldInput.Binding.Task || len(in.Development.Batch.DoD) != 2 {
		t.Fatal("direct reselection did not bind the new plan")
	}
}

// unlockRouteByRequirementRedo confirms the current source's formatting change
// and carries every duty while reopening the route selection boundary.
func unlockRouteByRequirementRedo(d *validationDriver) {
	d.t.Helper()
	source := d.run.IntakeConfirmationReceipt.RequirementSource
	path := filepath.Join(d.f.root, source)
	document, err := os.ReadFile(path)
	if err != nil {
		d.t.Fatal(err)
	}
	document = append(document, '\n')
	if err := os.WriteFile(path, document, 0600); err != nil {
		d.t.Fatal(err)
	}
	choices := make([]WorkChoice, len(d.run.RequirementWork))
	for i := range choices {
		choices[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "route redo carries every confirmed duty"}
	}
	request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Redo: []string{"route"}, Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, CarryReasons: []string{"all bound inputs preserve meaning"}}}})
	if err != nil {
		d.t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(d.t, d.f, request)
	if err != nil {
		d.t.Fatal(err)
	}
	confirmation, err := d.f.PrepareControl(ControlInput{Ask: askPosition(d.t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "redo the route selection boundary"})
	if err != nil {
		d.t.Fatal(err)
	}
	intake := confirmation.Intake
	if intake.Content != string(document) || intake.IntakeCandidate.RequirementSource != source || intake.RequirementDocument.Path != source || intake.Revision != intake.RequirementDocument.ContentDigest || intake.IntakeCandidate.RequirementRevision != intake.Revision {
		d.t.Fatal("route redo confirmation differs from its actual requirement source")
	}
	before := d.run.Revision
	_, d.run, err = submitControlWithHost(d.t, d.f, confirmation)
	if err != nil {
		d.t.Fatal(err)
	}
	state := driverState(d.t, d)
	if d.run.Revision != before+1 || d.run.Review.RequirementRevision != confirmation.Event.Decide.Requirement.CandidateRevision || !reflect.DeepEqual(state.CurrentIntakeConfirmation(), &intake.IntakeCandidate) || !reflect.DeepEqual(state.Validation.Document, *intake.RequirementDocument) {
		d.t.Fatal("route redo did not bind its complete source and projection in one confirmation")
	}
}

func materializeUnchangedCandidate(d *validationDriver, candidate coverage.ValidationCandidate) {
	d.t.Helper()
	d.drive()
	if len(d.run.AdapterIntents) != 1 {
		d.t.Fatalf("expected one final candidate intent: %+v", d.run.AdapterIntents)
	}
	for id, intent := range d.run.AdapterIntents {
		if intent.Adapter.Operation != "op.candidate" {
			d.t.Fatal("unexpected adapter method")
		}
		var params protocol.CandidateParameters
		data, _ := json.Marshal(intent.Adapter.Params)
		if err := json.Unmarshal(data, &params); err != nil {
			d.t.Fatal(err)
		}
		if params.Stage != "REVIEW_READY" || params.Base != d.run.Validation.DevelopmentSnapshot || !params.Finalize {
			d.t.Fatalf("unexpected materialization inputs: %+v", params)
		}
		// The development tree and approved artifacts were not edited. A native
		// immutable candidate therefore retains its exact identity and digest.
		evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: params.WorkspaceID, Identity: candidate.Identity, Digest: candidate.Digest, Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
		evidence.GateView = gateReviewView(params)
		data, _ = json.Marshal(evidence)
		var values map[string]any
		_ = json.Unmarshal(data, &values)
		event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), id, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, "EXECUTED", "", values)
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
	}
}

func TestRequirementGateRedoPreservesCarriedQAExecution(t *testing.T) {
	for _, delivery := range []string{"completed", "in-flight", "staged", "retry", "terminal-first"} {
		t.Run(delivery, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution.v4")
			gate, gi := d.gateAction("quality")
			report := executionFor(qi)
			result := nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report})
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "executed-input-contract", Validation: &result}}
			if delivery == "completed" {
				d.receipt(qa)
				d.submit(event)
			}
			if delivery == "staged" {
				d.submit(event)
			}
			if delivery == "terminal-first" {
				attempt := d.run.PendingActions[qa].AttemptID
				d.receipt(qa)
				for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					progress, err := protocol.NewTaskEvent(d.eventID(), qi.Binding.Task, attempt, status)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(progress)
				}
			}
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			candidate := *d.run.Validation.Candidate
			confirmValidationWork(d, gate)
			materializeUnchangedCandidate(d, candidate)
			d.reopen()
			d.drive()
			if delivery != "completed" {
				if delivery != "terminal-first" {
					d.receipt(qa)
				}
				if delivery == "retry" {
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "environment-interrupted"}})
					d.drive()
					qa, qi = d.action("engine.qa.execution.v4")
					report = executionFor(qi)
					result = nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report})
					event = protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "executed-input-contract", Validation: &result}}
					d.receipt(qa)
				}
				if delivery != "staged" {
					if a := d.submit(event); a.Status != "ACCEPTED" {
						t.Fatalf("carried evidence rejected: %+v %+v", a, d.run.RejectedResults)
					}
				}
			}
			d.reopen()
			d.drive()
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			for _, target := range wave.Targets {
				if target.Kind == "QA" && (target.Status != "PASS" || target.ResultAction != qa || target.Execution == nil || len(target.Execution.Records) != len(report.Records) || target.Execution.Records[0].Observation != report.Records[0].Observation) {
					t.Fatalf("carried QA lost evidence: %+v", target)
				}
				if target.Kind == "GATE" && (target.Status != "PENDING" || target.ResultAction != "" || target.Task == gi.Binding.Task) {
					t.Fatalf("redo inherited old gate: %+v", target)
				}
			}
			if wave.Status == "COMPLETE" {
				t.Fatal("fresh gate requirement was bypassed")
			}
			fresh, input := d.gateAction("quality")
			d.business(fresh, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("carried QA and fresh gate failed to join")
			}
			if delivery != "completed" && d.run.Validation.Waves[0].Status != "INVALIDATED" {
				t.Fatal("old wave revived")
			}
		})
	}
}

func TestRequirementCarryPreservesRequiredAffectedExecution(t *testing.T) {
	d, finding, wave := validationWithGateFinding(t)
	original := d.run.Validation.Waves[0].Candidate.Identity
	resolveGateFinding(d, finding, wave)
	id, in := d.action("engine.repair.v7")
	repair := gateRepairResult(in)
	repair.Development.AffectedCases = []string{"invalid"}
	repair.Development.CaseRuns[0].CaseID = "invalid"
	d.business(id, repair)
	d.verifyRepair(repair)
	d.materialize()
	d.materialize()
	d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
	d.drive()
	unlockRouteByRequirementRedo(d)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	id, in = d.action("engine.qa.execution.v4")
	if in.Execution.Scope != coverage.ScopeAffected {
		t.Fatal("setup needs AFFECTED")
	}
	report := executionFor(in)
	for i := range report.Records {
		if report.Records[i].CaseID == "normal" {
			report.Records[i].Provenance = coverage.ProvenanceInherited
			report.Records[i].InheritedFromCandidate = original
			report.Records[i].ReuseEvidence = "unchanged normal input verified against original candidate"
			report.Records[i].Procedure = ""
			report.Records[i].Observation = ""
			report.Records[i].Comparison = ""
		}
	}
	carriedAction := id
	interruptedWave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1].ID
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
	gate, _ := d.gateAction("security")
	candidate := *d.run.Validation.Candidate
	confirmValidationWork(d, gate)
	materializeUnchangedCandidate(d, candidate)
	d.reopen()
	d.drive()
	carriedWave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if carriedWave.Scope != coverage.ScopeAffected {
		t.Fatal("requirement carry changed execution scope")
	}
	for _, target := range carriedWave.Targets {
		if target.Kind == "QA" && (target.ResultAction != carriedAction || target.Execution == nil || target.Execution.Binding.Scope != coverage.ScopeAffected || !reflect.DeepEqual(target.Execution.Records, report.Records)) {
			t.Fatal("current projection lost original affected evidence")
		}
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.State.Results[carriedAction].Validation.Execution, &report) {
		t.Fatal("raw carried execution was rewritten")
	}
	for _, prior := range d.run.Validation.Waves {
		if prior.ID == interruptedWave && (prior.Status != "INVALIDATED" || prior.Counted) {
			t.Fatal("interrupted wave was revived or counted")
		}
	}
	id, in = d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	d.reopen()
	d.assertRequiredValidationComplete(coverage.ScopeAffected)
	current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if current.Candidate != candidate {
		t.Fatal("required validation changed the carried candidate")
	}
	for _, target := range current.Targets {
		if target.Kind == "QA" && (target.ResultAction != carriedAction || target.Execution == nil || !reflect.DeepEqual(target.Execution.Records, report.Records)) {
			t.Fatal("completion did not retain original approved affected execution")
		}
	}
	counted := 0
	for _, completed := range d.run.Validation.Waves {
		if completed.Counted {
			counted++
		}
	}
	if counted != 1 {
		t.Fatalf("pure P2 repair consumed extra business waves: %d", counted)
	}
}

// Explicit carry retains original delivery facts while the confirmed document
// is assembled into its own candidate through the native receipt owner.
func TestDeliveredCandidateRequalifiesAfterExplicitCarry(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "implementation", "review input")
	d := completePromotionPipeline(t, nil, root, identities[0])
	final, err := d.f.PrepareCandidate("confirmed delivery")
	if err != nil {
		t.Fatal(err)
	}
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	old, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	original := old.State.CandidatePromotion
	confirmValidationWork(d, "")
	d.reopen()
	for i := 0; i < 12; i++ {
		if _, _, err := d.f.engine.Drive(protocol.Observation{}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.f.driveNative(); err != nil {
			t.Fatal(err)
		}
		current, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if current.State.FinalValidationComplete(d.f.definition) {
			break
		}
	}
	current, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.State.Validation.Waves[0], old.State.Validation.Waves[0]) || !reflect.DeepEqual(current.State.Results, old.State.Results) || !reflect.DeepEqual(current.State.DispatchBindings, old.State.DispatchBindings) {
		t.Fatal("original wave or raw worker identity changed")
	}
	for action, receipt := range old.State.HostActionReceipts {
		if !reflect.DeepEqual(current.State.HostActionReceipts[action], receipt) {
			t.Fatal("original native receipt changed")
		}
	}
	if !reflect.DeepEqual(current.State.CandidatePromotion, original) || current.State.PromotedSnapshot != final {
		t.Fatal("assembly changed an executed native delivery fact")
	}
	if count, err := d.f.vcs.CommitCount(identities[0], final); err != nil || count != 1 {
		t.Fatalf("original delivery interval = %d %v", count, err)
	}
	wave := current.State.Validation.Waves[len(current.State.Validation.Waves)-1]
	if !current.State.FinalValidationComplete(d.f.definition) || wave.Candidate == old.State.Validation.Waves[0].Candidate || wave.Scope != coverage.ScopeAffected || wave.Generation == old.State.Validation.Waves[0].Generation || current.State.RequirementAssemblyReceipt(&current.State.Validation.Waves[0], &wave) == "" {
		t.Fatal("new confirmed document lacks exact authorized candidate inheritance")
	}
	for _, target := range wave.Targets {
		if target.Execution != nil {
			for _, row := range target.Execution.Records {
				if row.Provenance != coverage.ProvenanceInherited {
					t.Fatal("assembly claims a new execution")
				}
			}
		}
	}
	delivered, err := d.f.PrepareCandidate("confirmed requirement delivery")
	if err != nil {
		t.Fatal(err)
	}
	if delivered == final {
		t.Fatal("confirmed document did not produce a new delivery")
	}
	if count, err := d.f.vcs.CommitCount(identities[0], delivered); err != nil || count != 1 {
		t.Fatalf("new delivery interval = %d %v", count, err)
	}
	current, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for action, receipt := range old.State.HostActionReceipts {
		if !reflect.DeepEqual(current.State.HostActionReceipts[action], receipt) {
			t.Fatal("new promotion rewrote old receipt")
		}
	}
	body, err := os.ReadFile(filepath.Join(root, "requirements.md"))
	if err != nil || string(body) != validationDocument+"\n" {
		t.Fatalf("confirmed document was not delivered: %v", err)
	}
}

func TestRequirementRedoStopsOriginalExecutionBeforeReplacement(t *testing.T) {
	for _, status := range []string{"spawned", "awaiting-result", "unknown", "attached", "claimed", "failed"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox")
			action, _ := d.action("engine.development.batch.v6")
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			original := before.State.PendingActions[action]
			attempt := before.State.Attempts[original.Task]
			correlation, identity := action, action
			if status == "spawned" || status == "awaiting-result" {
				d.receipt(action)
				start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), correlation, identity, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(start)
			}
			if status == "awaiting-result" {
				for _, progress := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					event, err := protocol.NewTaskEvent(d.eventID(), original.Task, attempt.ID, progress)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(event)
				}
			}
			if status == "unknown" || status == "attached" {
				correlation, identity = action+"/transport", action+"/native"
				event, err := protocol.NewSpawnReceiptEvent(d.eventID(), action, d.host(), correlation, protocol.SpawnStatusUnknown)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(event)
				if status == "attached" {
					for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
						event, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), correlation, identity, edge)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(event)
					}
				}
			}
			if status == "claimed" {
				refs, err := d.f.workerMaterials(before.State, true)
				if err != nil {
					t.Fatal(err)
				}
				ref := refs[action]
				request, _, directory, _, err := d.f.WorkerExecutionRequest(ref, "")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err = writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
					t.Fatal(err)
				}
				release, err := AdmitExecution(directory, request)
				if err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					err = file.Close()
				}
				release()
				if err != nil {
					t.Fatal(err)
				}
				correlation, identity = directory, action+"/native"
			}
			path := filepath.Join(root, "requirements.md")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			choices := make([]WorkChoice, len(d.run.RequirementWork))
			for i, w := range d.run.RequirementWork {
				choice := protocol.RequirementWorkCarry
				if w.ActionID == action {
					choice = protocol.RequirementWorkRedo
				}
				choices[i] = WorkChoice{Disposition: choice, Reason: "Explicitly replace unfinished implementation and carry independent duty"}
			}
			candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"requirement formatting and implementation replay"}, CarryReasons: []string{"independent QA design remains applicable"}}}})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(d.t, d.f, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if status == "failed" {
				failure, err := protocol.NewSpawnReceiptWithFailureClass(d.eventID(), action, d.host(), action, protocol.SpawnStatusFailed, authoring.FailureTransientEngine)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(failure)
			}
			confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm exact work dispositions"})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(d.t, d.f, confirm)
			if err != nil {
				t.Fatal(err)
			}
			d.reopen()
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			obsolete := after.State.ObsoleteActions[action]
			if obsolete.AttemptID != attempt.ID || !reflect.DeepEqual(obsolete.Bindings, attempt.Bindings) || !reflect.DeepEqual(obsolete.Plan, attempt.Plan) {
				t.Fatal("lost original complete carrier", obsolete, attempt)
			}
			if _, material := d.run.WorkerMaterials[action]; material {
				t.Fatal("obsolete execution still has launch material")
			}
			if _, worker := after.State.WorkerInput(action, d.f.definition); worker {
				t.Fatal("obsolete execution still admitted")
			}
			if _, replacement := after.State.PendingActions[obsolete.ReplacedBy]; replacement && status != "failed" && status != "attached" {
				t.Fatal("replacement issued before stop", obsolete)
			}
			assertStopped := func(withLate bool) {
				d.drive()
				d.reopen()
				final, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, pending := final.State.PendingActions[action]; pending {
					t.Fatal("stopped original remains pending")
				}
				if _, ended := final.State.EndedAttempts[original.Task]; ended {
					t.Fatal("stopped original remains awaiting result")
				}
				if _, replacement := final.State.PendingActions[obsolete.ReplacedBy]; !replacement {
					t.Fatal("exact stop did not release replacement", d.run.Next, final.State.Tasks)
				}
				if !reflect.DeepEqual(final.State.ObsoleteActions[action], obsolete) {
					t.Fatal("stop replaced original identity evidence", final.State.ObsoleteActions[action], obsolete)
				}
				if withLate && (len(final.State.ObsoleteResults[action]) != 1 || final.State.ObsoleteResults[action][0].PayloadDigest != "original-late-result") {
					t.Fatal("late original raw evidence lost")
				}
			}
			if status == "failed" || status == "attached" {
				assertStopped(false)
				return
			}
			late, err := protocol.NewWorkerResultEvent(d.eventID(), action, d.host(), protocol.OutcomeRuntimeError, "original-late-result", authoring.FailureTransientEngine)
			if err != nil {
				t.Fatal(err)
			}
			if acceptance := d.submit(late); acceptance.Status != "OBSOLETE_RESULT" {
				t.Fatal("late result advanced original duty", acceptance)
			}
			if status == "unknown" || status == "claimed" {
				unknown, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if unknown.State.SpawnReceipts[action].Status != protocol.SpawnStatusUnknown {
					t.Fatal("unknown original lost", unknown.State.SpawnReceipts[action])
				}
				for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
					event, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), correlation, identity, edge)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(event)
				}
				assertStopped(true)
				return
			}
			after, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			var terminate protocol.HostActionIntent
			for _, intent := range after.State.PendingHostActions {
				if intent.Terminate != nil && intent.Terminate.WorkerActionID == action {
					terminate = intent
				}
			}
			if terminate.ActionID == "" || terminate.Terminate.AttemptID != attempt.ID || terminate.Terminate.Identity != identity {
				t.Fatal("missing exact termination", terminate, identity)
			}
			after, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, replacement := after.State.PendingActions[obsolete.ReplacedBy]; replacement {
				t.Fatal("late result released replacement without stop")
			}
			wrong, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), terminate.ActionID, protocol.HostActionTerminateAgent, d.host(), terminate.ActionID, terminate.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: identity + "/wrong", Event: protocol.LifecycleStop})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = d.f.Submit(wrong); err == nil {
				t.Fatal("wrong stop identity admitted")
			}
			unchanged, err := d.f.engine.Load()
			if err != nil || !reflect.DeepEqual(after, unchanged) {
				t.Fatal("wrong stop changed state", err)
			}
			stop, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), correlation, identity, protocol.LifecycleStop)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(stop)
			receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), terminate.ActionID, protocol.HostActionTerminateAgent, d.host(), terminate.ActionID, terminate.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: identity, Event: protocol.LifecycleStop})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(receipt)
			assertStopped(true)
		})
	}
}

func TestRequirementRedoReviewSeriesWaitForOriginalStop(t *testing.T) {
	for _, kind := range []review.Action{review.ProductReview, review.StartReadiness} {
		for _, terminal := range []bool{false, true} {
			name := string(kind)
			if terminal {
				name += "/awaiting-result"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				base := snapshotRepoCommits(t, root)[0]
				d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
				if kind == review.StartReadiness {
					passPreDevelopmentReviews(t, d, commandPlan(), review.ProductReview)
				}
				var action string
				for id, pending := range d.run.PendingActions {
					if pending.ReviewSeries == kind {
						action = id
					}
				}
				if action == "" {
					t.Fatal("missing original series")
				}
				d.receipt(action)
				initial, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				original := initial.State.PendingActions[action]
				attempt := initial.State.Attempts[original.Task]
				start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(start)
				if terminal {
					for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
						event, err := protocol.NewTaskEvent(d.eventID(), original.Task, attempt.ID, status)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(event)
					}
				}
				path := filepath.Join(root, "requirements.md")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Redo: []string{string(kind)}, Impact: protocol.RequirementImpact{ChangedItems: []string{"formatting with explicit fresh review"}, CarryReasons: []string{"unchanged other series"}}}})
				if err != nil {
					t.Fatal(err)
				}
				_, d.run, err = submitControlWithHost(d.t, d.f, candidate)
				if err != nil {
					t.Fatal(err)
				}
				confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Redo the selected original review"})
				if err != nil {
					t.Fatal(err)
				}
				_, d.run, err = submitControlWithHost(d.t, d.f, confirm)
				if err != nil {
					t.Fatal(err)
				}
				d.reopen()
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				obsolete := after.State.ObsoleteActions[action]
				if obsolete.AttemptID != attempt.ID || !obsolete.Bindings.Equal(attempt.Bindings) || obsolete.Plan != attempt.Plan {
					t.Fatal("review carrier lost", obsolete, attempt)
				}
				for id, pending := range after.State.PendingActions {
					if id != action && pending.ReviewSeries == kind {
						t.Fatal("fresh review issued before original stop")
					}
				}
				var termination protocol.HostActionIntent
				for _, intent := range after.State.PendingHostActions {
					if intent.Terminate != nil && intent.Terminate.WorkerActionID == action {
						termination = intent
					}
				}
				if termination.Terminate == nil || termination.Terminate.AttemptID != attempt.ID {
					t.Fatal("missing review termination", termination)
				}
				d.completeWorkerTerminations()
				d.reopen()
				d.drive()
				after, err = d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				fresh := false
				for id, pending := range after.State.PendingActions {
					if id != action && pending.ReviewSeries == kind {
						fresh = true
					}
				}
				if !fresh {
					t.Fatal("stopped review did not unlock original fresh obligation", d.run.Next)
				}
				if !reflect.DeepEqual(after.State.ObsoleteActions[action], obsolete) {
					t.Fatal("review stop lost carrier")
				}
			})
		}
	}
}
func TestRequirementRedoTracksLatestReplacementAndPreservesIndependentWorker(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox")
	action, _ := d.action("engine.development.batch.v6")
	qa, _ := d.action("engine.qa.design.v6")
	d.receipt(action)
	initial, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	original := initial.State.PendingActions[action]
	independent := initial.State.PendingActions[qa]
	independentAttempt := initial.State.Attempts[independent.Task]
	start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	var latest string
	for iteration := 0; iteration < 2; iteration++ {
		path := filepath.Join(root, "requirements.md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		choices := make([]WorkChoice, len(d.run.RequirementWork))
		for i, w := range d.run.RequirementWork {
			choice := protocol.RequirementWorkCarry
			if w.Task.Step == original.Task.Step {
				choice = protocol.RequirementWorkRedo
			}
			choices[i] = WorkChoice{Disposition: choice, Reason: "Replace the current implementation obligation only"}
		}
		candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"implementation replay"}, CarryReasons: []string{"independent QA design unchanged"}}}})
		if err != nil {
			t.Fatal(err)
		}
		_, d.run, err = submitControlWithHost(d.t, d.f, candidate)
		if err != nil {
			t.Fatal(err)
		}
		confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm current redo and independent carry"})
		if err != nil {
			t.Fatal(err)
		}
		_, d.run, err = submitControlWithHost(d.t, d.f, confirm)
		if err != nil {
			t.Fatal(err)
		}
		d.reopen()
		current, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if current.State.PendingActions[qa] != independent || !reflect.DeepEqual(current.State.Attempts[independent.Task], independentAttempt) {
			t.Fatal("redo changed independent original execution")
		}
		for id, pending := range current.State.PendingActions {
			if id != action && pending.Task.Step == original.Task.Step {
				t.Fatal("replacement issued before original stopped")
			}
		}
		for _, w := range d.run.RequirementWork {
			if w.Task.Step == original.Task.Step {
				latest = w.ActionID
			}
		}
		if latest == "" || latest == action {
			t.Fatal("missing current queued replacement")
		}
	}
	d.completeWorkerTerminations()
	d.drive()
	d.reopen()
	final, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, issued := final.State.PendingActions[latest]; !issued {
		t.Fatal("stopping original failed to release latest confirmed replacement", latest, d.run.Next)
	}
	if final.State.PendingActions[qa] != independent || !reflect.DeepEqual(final.State.Attempts[independent.Task], independentAttempt) {
		t.Fatal("stop changed independent carried work")
	}
}
