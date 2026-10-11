package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

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
func confirmValidationDocument(d *validationDriver, redo string, inventory *protocol.CoverageInventory, document string) {
	d.t.Helper()
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
	pkg := protocol.RequirementDecisionPackage{Children: children, Impact: impact, Coverage: inventory, CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:work-selection", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices}
	event, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Children: children, Impact: impact, Coverage: inventory, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Reason: "confirmed each work decision"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
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
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:readiness-only", ImpactDigest: "sha256:command-contract", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, WorkDecisions: choices}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: protocol.RequirementClassificationPreserved, RedoStartReadiness: true, WorkDecisions: choices, Reason: "readiness only; the route stays bound"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	d.drive()
	if d.run.Review.RouteMode != "custom" {
		t.Fatalf("readiness-only redo changed the route binding: %q", d.run.Review.RouteMode)
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
	d.receipt(ready)
	ev, err := protocol.NewReviewWorkerResultEvent(d.eventID(), ready, DefaultProvider, "new plan", protocol.ReviewResultEvidence{Action: review.StartReadiness, DispatchID: ready, RequirementRevision: revision, Status: "PASS", GranularityReview: &nextPlan, Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "Compared the extended command DoD"}})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(ev)
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.development.batch.v2" {
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

// unlockRouteByRequirementRedo 走需求变化勾选重走路线：PRESERVED 全携解
// 除当前路线绑定并重新观察需求文档；已派发工作与波次证据不动，随后的重
// 新选择由调用方完成（直接重选通道同样可达，两种通道等价）。
func unlockRouteByRequirementRedo(d *validationDriver) {
	d.t.Helper()
	d.routeRedos++
	document := validationDocument + strings.Repeat("\n", d.routeRedos+1)
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	var choices []protocol.RequirementWorkDecision
	for _, item := range d.run.RequirementWork {
		choices = append(choices, protocol.RequirementWorkDecision{Work: item, Disposition: protocol.RequirementWorkCarry, Reason: "route redo carries every confirmed duty"})
	}
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:route-redo", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoRoute: true, WorkDecisions: choices}
	event, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, RedoRoute: true, WorkDecisions: choices, Reason: "redo the route selection boundary"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
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
			qa, qi := d.action("engine.qa.execution.v2")
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
					qa, qi = d.action("engine.qa.execution.v2")
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
	id, in := d.action("engine.repair.v2")
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
	id, in = d.action("engine.qa.execution.v2")
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

// Explicit carry creates a current FULL obligation without changing the
// original worker results, native delivery proof or completed-wave history.
func TestDeliveredCandidateRequalifiesAfterExplicitCarry(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "implementation", "review input")
	d := completePromotionPipeline(t, nil, root, identities[0])
	final, err := d.f.PrepareCandidate("confirmed delivery")
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	d.run = run
	old, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	original := old.State.CandidatePromotion
	waves := len(old.State.Validation.Waves)
	confirmValidationWork(d, "")
	// Inspect the requalified projection before terminal resource cleanup.
	if _, _, err := d.f.engine.Drive(emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	current, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(current.State.Validation.Waves) != waves+1 || current.State.Validation.Waves[0].Status != "COMPLETE" || !current.State.FinalValidationComplete(d.f.definition) {
		t.Fatalf("current FULL/history lost after carry: %+v", current.State.Validation)
	}
	if !reflect.DeepEqual(current.State.CandidatePromotion, original) || current.State.PromotedSnapshot != final {
		t.Fatal("carry changed an executed native delivery fact")
	}
	if count, err := d.f.vcs.CommitCount(identities[0], final); err != nil || count != 1 {
		t.Fatalf("carry reorganized delivery: %d %v", count, err)
	}
	if current.State.Validation.Waves[waves].Generation == old.State.Validation.Waves[0].Generation {
		t.Fatal("carry reused the historical wave identity")
	}
}
