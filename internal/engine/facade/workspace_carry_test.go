package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"testing"
)

func TestCarriedPendingWorkspaceKeepsItsOriginalReceipt(t *testing.T) {
	pendingWorkspaceRequirementChange(t, false)
}

func TestReplacedPendingWorkspaceUsesItsNewTask(t *testing.T) {
	pendingWorkspaceRequirementChange(t, true)
}

func pendingWorkspaceRequirementChange(t *testing.T, redo bool) {
	t.Helper()
	for _, choice := range []protocol.AskOptionID{"proceed"} {
		t.Run(string(choice), func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := nativeMasterAt(t, root, base, nil)
			d.reviewPass(commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One native pending workspace"}})
			d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
			d.drive()
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution.v3")
			gate, gi := d.action("engine.gate.v2")
			report := executionFor(qi)
			tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "Output ordering needs correction"}}})
			wave := d.run.Validation.Waves[0]
			var findingID string
			for id := range d.run.Validation.Findings {
				findingID = id
			}
			resolution := protocol.FindingResolution{FindingID: findingID, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "output", RootCause: "ordering", Consequence: "wrong ordering", Severity: "P2", SeverityRationale: "limited output issue", Verification: review.FindingVerification{Scope: "in scope", Severity: "limited", Causality: "verified", Reproduction: "app run", Classification: "implementation"}, Reason: "verified"}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Remedies: []string{"REPAIR", "REQUEST_REQUIREMENT_CHANGE"}}}})
			var selected bool
			for _, ask := range d.run.PendingAsks {
				if ask.Validation != nil && ask.Validation.Kind == "VALIDATION_DETAIL_DISPOSITION" {
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "Clarify the required ordering before further repair", Digest: ask.Validation.Digest, StateRevision: d.run.Revision, Remedy: "REQUEST_REQUIREMENT_CHANGE"}}})
					selected = true
					break
				}
			}
			if !selected || d.run.PendingRequirementAsk == nil {
				t.Fatal("remedy did not create the requirement barrier")
			}
			ask := *d.run.PendingRequirementAsk
			t.Logf("created requirement ask=%s options=%+v package=%+v", ask.RequestID, ask.Options, ask.Package)
			d.drive()
			if len(d.run.PendingActions) != 0 || d.run.PendingRequirementAsk.RequestID != ask.RequestID {
				t.Fatal("requirement barrier dispatched repair or changed Ask identity")
			}
			before := driverState(t, d)
			var original protocol.HostActionIntent
			for _, intent := range before.PendingHostActions {
				if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" {
					original = intent
				}
			}
			if original.ActionID == "" {
				t.Fatal("test requires a pending original workspace")
			}
			originalParams, err := json.Marshal(original.Adapter.Params)
			if err != nil {
				t.Fatal(err)
			}
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			var originalInput protocol.WorkspaceParameters
			if err := json.Unmarshal(originalParams, &originalInput); err != nil || originalInput.Task == nil {
				t.Fatal("original workspace has no signed task")
			}
			work := []protocol.RequirementWorkDecision{}
			for _, w := range d.run.RequirementWork {
				disposition := protocol.RequirementWorkCarry
				if redo && w.Task == *originalInput.Task {
					disposition = protocol.RequirementWorkRedo
				}
				work = append(work, protocol.RequirementWorkDecision{Work: w, Disposition: disposition, Reason: "confirmed disposition of original work"})
			}
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlRequestRequirementChange, Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Package: &pkg}})
			if d.run.PendingRequirementAsk.RequestID != ask.RequestID || d.run.PendingRequirementAsk.Package == nil {
				t.Fatal("package update replaced or failed to bind the existing Ask")
			}
			t.Logf("package bound to same ask=%s", ask.RequestID)
			decision := &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Choice: choice}
			if choice == "proceed" {
				decision.Requirement = &protocol.RequirementDecisionPayload{BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "Confirmed unchanged ordering requirement"}
			}
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: decision})
			if d.run.PendingRequirementAsk != nil {
				t.Fatal("resolved Ask retained the requirement barrier")
			}
			expected := d.revision
			if choice == "proceed" {
				expected = revision
			}
			if d.run.Review.RequirementRevision != expected {
				t.Fatalf("requirement=%s want %s", d.run.Review.RequirementRevision, expected)
			}
			if choice == "proceed" {
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			}
			if redo {
				state := driverState(t, d)
				if state.NativeActionCurrent(original, d.f.definition) || state.NativeActionWritable(original, d.f.definition) {
					t.Fatal("REDO retained execution authority for the original task")
				}
				d.drive()
				state = driverState(t, d)
				effect := state.ReconciledEffects[original.ActionID]
				if effect.Status != "SUPERSEDED" || effect.Intent == nil || effect.Intent.PayloadDigest != original.PayloadDigest {
					t.Fatal("replacement completion did not retain and retire the original responsibility")
				}
				if _, executed := state.HostActionReceipts[original.ActionID]; executed {
					t.Fatal("REDO executed the obsolete workspace")
				}
				_, repair := d.action("engine.repair.v5")
				if repair.Binding.Task == *originalInput.Task || repair.Binding.WorkspaceID == originalInput.Resource.ID {
					t.Fatal("replacement borrowed the original task or resource")
				}
				var replacementReceipt protocol.HostActionReceipt
				for id, receipt := range state.HostActionReceipts {
					intent := state.ReconciledEffects[id].Intent
					if intent == nil || intent.Adapter == nil || intent.Adapter.Operation != "op.workspace" {
						continue
					}
					params, err := json.Marshal(intent.Adapter.Params)
					if err != nil {
						t.Fatal(err)
					}
					var input protocol.WorkspaceParameters
					if err := json.Unmarshal(params, &input); err != nil {
						t.Fatal(err)
					}
					if input.Task != nil && *input.Task == repair.Binding.Task && input.Resource.ID == repair.Binding.WorkspaceID {
						replacementReceipt = receipt
					}
				}
				if replacementReceipt.Status != protocol.HostActionStatusExecuted || effect.ObservationDigest != replacementReceipt.Digest {
					t.Fatal("obsolete responsibility was not discharged by the actual replacement receipt")
				}
				d.reopen()
				d.drive()
				after := driverState(t, d)
				if after.ReconciledEffects[original.ActionID].ObservationDigest != replacementReceipt.Digest || len(after.HostActionReceipts) != len(state.HostActionReceipts) {
					t.Fatal("reopen changed replacement evidence or executed another workspace")
				}
				return
			}
			d.drive()
			beforeExecution := driverState(t, d)
			receipt, exists := beforeExecution.HostActionReceipts[original.ActionID]
			if !exists || receipt.Status != protocol.HostActionStatusExecuted || receipt.PayloadDigest != original.PayloadDigest {
				t.Fatal("carry did not complete the original signed responsibility")
			}
			effect := beforeExecution.ReconciledEffects[original.ActionID]
			if effect.Intent == nil {
				t.Fatal("original intent was not retained in reconciliation evidence")
			}
			retainedParams, err := json.Marshal(effect.Intent.Adapter.Params)
			if err != nil {
				t.Fatal(err)
			}
			if string(retainedParams) != string(originalParams) {
				t.Fatal("carry rewrote original parameters")
			}
			event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), receipt.ActionID, receipt.AdapterOperation, receipt.Provider, receipt.Correlation, receipt.PayloadDigest, receipt.Status, receipt.FailureClass, receipt.AdapterEvidence.Values)
			if err != nil {
				t.Fatal(err)
			}
			if accepted := d.submit(event); accepted.Status != "DUPLICATE" {
				t.Fatalf("original receipt was not idempotent: %+v", accepted)
			}
			d.reopen()
			d.drive()
			after := driverState(t, d)
			if after.HostActionReceipts[original.ActionID].Digest != receipt.Digest || len(after.HostActionReceipts) != len(beforeExecution.HostActionReceipts) {
				t.Fatal("reopen reexecuted the original workspace")
			}
			_, repair := d.action("engine.repair.v5")
			if repair.Binding.RequirementRevision != expected {
				t.Fatal("repair resumed against the wrong confirmed requirement")
			}
			t.Logf("resolved %s; current requirement=%s; repair resumed", choice, expected)
		})
	}
}
