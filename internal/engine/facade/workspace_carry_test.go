package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Route confirmation retains the native creation responsibility; normal
// completion and a lost response admit the same original workspace proof.
func TestQueuedDevelopmentWorkspaceSurvivesRouteSelection(t *testing.T) {
	t.Parallel()
	for _, lostResponse := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost-response=%t", lostResponse), func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := nativeMasterAt(t, root, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
			catalog, err := prompts.PackageRouteCandidates(d.f.admission.Target)
			if err != nil || len(catalog) < 4 {
				t.Fatal("fixture needs two installed ordinary gates", catalog, err)
			}
			d.confirmRoute(routeDecisionForGates([]string{catalog[2]}))
			state := driverState(t, d)
			if len(state.PendingHostActions) != 1 || len(state.PendingActions) != 0 {
				t.Fatal("fixture requires an unobserved workspace before development")
			}
			var original protocol.HostActionIntent
			for _, intent := range state.PendingHostActions {
				original = intent
			}
			var parameters protocol.WorkspaceParameters
			if err := decodeNativeParams(original, &parameters); err != nil || parameters.Task == nil || parameters.Resource.Role != "development" {
				t.Fatal("fixture has no original development workspace", err)
			}
			var executed nativeResult
			if lostResponse {
				var complete bool
				executed, complete, err = d.f.nativeEvidence(state, original, true)
				if err != nil || !complete || executed.Workspace == nil {
					t.Fatal("native execution did not establish its actual effect", complete, err)
				}
				unknown, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), original.ActionID, "op.workspace", d.host(), original.Correlation, original.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(unknown)
			}
			configuration := protocol.RouteDecision{Selected: catalog[3:4]}
			d.configureRoute(&configuration)
			if !reflect.DeepEqual(d.run.Review.SelectedGates, catalog[3:4]) {
				t.Fatal("queued workspace blocked a saved library change")
			}
			d.reopen()
			state = driverState(t, d)
			if len(state.PendingHostActions) != 1 || !reflect.DeepEqual(state.PendingHostActions[original.ActionID], original) || state.NativeResources[parameters.Resource.ID] != parameters.Resource || !state.NativeActionCurrent(original, d.f.definition) || state.NativeActionWritable(original, d.f.definition) == lostResponse {
				t.Fatalf("workspace after library change: count=%d original=%t resource=%t current=%t writable=%t lost=%t", len(state.PendingHostActions), reflect.DeepEqual(state.PendingHostActions[original.ActionID], original), state.NativeResources[parameters.Resource.ID] == parameters.Resource, state.NativeActionCurrent(original, d.f.definition), state.NativeActionWritable(original, d.f.definition), lostResponse)
			}
			if progress, err := d.f.completeNativeIntent(state, original, !lostResponse); err != nil || !progress {
				t.Fatal("original completion did not reach typed receipt admission", progress, err)
			}
			state = driverState(t, d)
			receipt := state.HostActionReceipts[original.ActionID]
			if receipt.AdapterEvidence == nil {
				t.Fatal("original completion lacks typed workspace evidence")
			}
			var proof protocol.WorkspaceEvidence
			raw, err := json.Marshal(receipt.AdapterEvidence.Values)
			if err != nil || json.Unmarshal(raw, &proof) != nil {
				t.Fatal("original receipt lacks typed resource proof", err)
			}
			expected := parameters.Resource
			expected.Status, expected.Identity = "READY", expected.Source
			digest, err := d.f.vcs.TreeDigest(base)
			if err != nil || proof.Resource != expected || proof.ParametersDigest != original.PayloadDigest || proof.Digest != digest || proof.SourceDigest != digest || receipt.Status != protocol.HostActionStatusExecuted || receipt.Correlation != original.Correlation || receipt.PayloadDigest != original.PayloadDigest {
				t.Fatal("original receipt differs from its actual registered source", proof, err)
			}
			if lostResponse && !reflect.DeepEqual(proof, *executed.Workspace) {
				t.Fatal("observation changed the lost-response proof")
			}
			if effect := state.ReconciledEffects[original.ActionID]; effect.Intent == nil || !reflect.DeepEqual(*effect.Intent, original) || lostResponse && effect.UnknownReceipt == nil {
				t.Fatal("receipt settlement discarded the original intent or UNKNOWN evidence")
			}
			body, err := os.ReadFile(filepath.Join(expected.Path, "requirements.md"))
			if err != nil || string(body) != validationDocument {
				t.Fatal("workspace material differs from its confirmed source", err)
			}
			before := state
			event, err := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+receipt.ActionID), receipt.ActionID, receipt.AdapterOperation, receipt.Provider, receipt.Correlation, receipt.PayloadDigest, receipt.Status, receipt.FailureClass, receipt.AdapterEvidence.Values)
			if err != nil {
				t.Fatal(err)
			}
			if accepted := d.submit(event); !reflect.DeepEqual(accepted, before.Events[string(event.ID)].Acceptance) {
				t.Fatal("original receipt event replay changed acceptance", accepted)
			}
			d.reopen()
			if progress, err := d.f.completeNativeIntent(driverState(t, d), original, false); err != nil || progress || !reflect.DeepEqual(before, driverState(t, d)) {
				t.Fatal("reopen or duplicate receipt repeated the workspace effect", progress, err)
			}
		})
	}
}

func TestCarriedPendingWorkspaceKeepsItsOriginalReceipt(t *testing.T) {
	t.Parallel()
	pendingWorkspaceRequirementChange(t, false)
}

func TestReplacedPendingWorkspaceUsesItsNewTask(t *testing.T) {
	t.Parallel()
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
			qa, qi := d.action("engine.qa.execution.v4")
			gate, gi := d.action("engine.gate.v3")
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
			intakeReceipt := driverState(t, d).CurrentIntakeConfirmation()
			if err := os.WriteFile(filepath.Join(d.f.root, intakeReceipt.RequirementSource), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			intake, err := prepareConfirmedIntake(d.f.root, intakeReceipt, true)
			if err != nil {
				t.Fatal(err)
			}
			pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlRequestRequirementChange, Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Package: &pkg}})
			if d.run.PendingRequirementAsk.RequestID != ask.RequestID || d.run.PendingRequirementAsk.Package == nil {
				t.Fatal("package update replaced or failed to bind the existing Ask")
			}
			t.Logf("package bound to same ask=%s", ask.RequestID)
			decision := &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Choice: choice}
			if choice == "proceed" {
				decision.Requirement = &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "Confirmed unchanged ordering requirement"}
			}
			confirmation := protocol.Event{Kind: protocol.KindDecide, Decide: decision}
			if choice == "proceed" {
				confirmation = confirmation.WithRequirementDocument(document)
			}
			d.submit(confirmation)
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
				_, repair := d.action("engine.repair.v7")
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
			_, repair := d.action("engine.repair.v7")
			if repair.Binding.RequirementRevision != expected {
				t.Fatal("repair resumed against the wrong confirmed requirement")
			}
			t.Logf("resolved %s; current requirement=%s; repair resumed", choice, expected)
		})
	}
}

// A confirmed route addition binds a new mirror while retained calls keep
// their original evidence and observe their native prefix before new writes.
func TestPendingBlackboxMirrorRouteAdditionUsesCurrentManifest(t *testing.T) {
	t.Parallel()
	for _, response := range []string{"pending", "unknown-absent", "unknown-executed", "late-executed"} {
		t.Run(response, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := nativeMasterAt(t, root, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
			catalog, err := prompts.PackageRouteCandidates(d.f.admission.Target)
			if err != nil || len(catalog) < 4 {
				t.Fatal("fixture needs two installed ordinary gates", catalog, err)
			}
			d.confirmRoute(routeDecisionForGates([]string{"blackbox", catalog[2]}))
			d.drive()
			design, input := d.action("engine.qa.design.v6")
			manifest := simpleManifest(input)
			d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
			state := driverState(t, d)
			var original protocol.HostActionIntent
			var parameters protocol.WorkspaceParameters
			for _, intent := range state.PendingHostActions {
				var p protocol.WorkspaceParameters
				if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" && decodeNativeParams(intent, &p) == nil && p.Manifest != nil {
					original, parameters = intent, p
				}
			}
			if original.ActionID == "" || parameters.Resource.Status != "READY" {
				t.Fatal("normal design did not queue a mirror on its created workspace")
			}
			var executed nativeResult
			if response == "unknown-executed" || response == "late-executed" {
				var complete bool
				executed, complete, err = d.f.nativeEvidence(state, original, true)
				if err != nil || !complete || executed.Workspace == nil {
					t.Fatal("original mirror did not establish its exact effect", complete, err)
				}
			}
			var unknown protocol.Event
			if response != "pending" {
				unknown, err = protocol.NewAdapterHostActionReceiptEvent(d.eventID(), original.ActionID, "op.workspace", d.host(), original.Correlation, original.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(unknown)
			}
			configuration := protocol.RouteDecision{Selected: []string{catalog[0], catalog[2], catalog[3]}}
			d.configureRoute(&configuration)
			d.reopen()
			state = driverState(t, d)
			if !reflect.DeepEqual(state.PendingHostActions[original.ActionID], original) || state.NativeActionCurrent(original, d.f.definition) || state.NativeActionWritable(original, d.f.definition) || !state.WorkspaceMirrorSuperseded(original, d.f.definition) {
				t.Fatalf("mirror after library change: original=%t current=%t writable=%t superseded=%t", reflect.DeepEqual(state.PendingHostActions[original.ActionID], original), state.NativeActionCurrent(original, d.f.definition), state.NativeActionWritable(original, d.f.definition), state.WorkspaceMirrorSuperseded(original, d.f.definition))
			}
			target := filepath.Join(parameters.Resource.Path, ".gates", "cases", "blackbox.md")
			prior, priorErr := os.ReadFile(target)
			if priorErr != nil && !os.IsNotExist(priorErr) {
				t.Fatal(priorErr)
			}
			if _, _, err := d.f.nativeEvidence(state, original, true); err != nil {
				t.Fatal("stale observation failed to use original physical inputs", err)
			}
			if actual, err := os.ReadFile(target); err != nil && !os.IsNotExist(err) || os.IsNotExist(err) != os.IsNotExist(priorErr) || string(actual) != string(prior) {
				t.Fatal("stale intent changed its native mirror", err)
			}
			if response == "late-executed" {
				var current protocol.HostActionIntent
				for _, intent := range state.PendingHostActions {
					if intent.ActionID != original.ActionID && intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" && state.NativeActionCurrent(intent, d.f.definition) {
						current = intent
					}
				}
				if current.ActionID == "" {
					t.Fatal("route addition lacks its signed current mirror")
				}
				if progress, err := d.f.completeNativeIntent(state, current, true); err != nil || !progress {
					t.Fatal("current mirror did not produce its original receipt", progress, err)
				}
				state = driverState(t, d)
				if !state.WorkspaceMirrorSuperseded(original, d.f.definition) {
					t.Fatal("completed current mirror lost supersession evidence")
				}
			}
			d.drive()
			state = driverState(t, d)
			if _, pending := state.PendingHostActions[original.ActionID]; pending {
				t.Fatal("current mirror left original native responsibility unresolved")
			}
			effect := state.ReconciledEffects[original.ActionID]
			if effect.Intent == nil || !reflect.DeepEqual(*effect.Intent, original) {
				t.Fatal("mirror settlement changed original signed parameters")
			}
			if response == "unknown-executed" {
				receipt := state.HostActionReceipts[original.ActionID]
				var proof protocol.WorkspaceEvidence
				raw, err := json.Marshal(receipt.AdapterEvidence.Values)
				if err != nil || json.Unmarshal(raw, &proof) != nil || !reflect.DeepEqual(proof, *executed.Workspace) || receipt.PayloadDigest != original.PayloadDigest || effect.Status != "FULFILLED" || effect.UnknownReceipt == nil {
					t.Fatal("lost-response completion changed its original workspace proof", proof, err)
				}
			} else {
				if effect.Status != "WITHDRAWN" || effect.Resource == nil || effect.Resource.ParametersDigest != original.PayloadDigest || effect.Resource.Resource != parameters.Resource || !effect.Resource.WorktreeExists || effect.Resource.Prefix == nil || effect.ObservationDigest == "" {
					t.Fatal("unfinished mirror was not withdrawn from its exact observed resource")
				}
				if receipt, exists := state.HostActionReceipts[original.ActionID]; exists && receipt.Status == protocol.HostActionStatusExecuted {
					t.Fatal("unfinished mirror manufactured an EXECUTED receipt")
				}
				if response != "pending" && effect.UnknownReceipt == nil {
					t.Fatal("withdrawal discarded the original UNKNOWN receipt")
				}
			}
			currentManifest := state.Validation.Sets[coverage.ReviewBlackbox].Manifest
			body, err := os.ReadFile(target)
			if err != nil || string(body) != protocol.BlackboxCasesMarkdown(state.RunID, currentManifest) || state.NativeResources[parameters.Resource.ID].MirrorDigest != protocol.BlackboxCasesDigest(state.RunID, currentManifest) {
				t.Fatal("materialized mirror differs from the entire current manifest", err)
			}
			_, reviewInput := d.action("engine.qa.review.rationale.v2")
			if reviewInput.Binding.WorkspaceID != parameters.Resource.ID {
				t.Fatal("current QA review borrowed another physical workspace")
			}
			if response == "late-executed" {
				before := state.NativeResources[parameters.Resource.ID]
				values, err := executed.fields()
				if err != nil {
					t.Fatal(err)
				}
				late, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), original.ActionID, "op.workspace", d.host(), original.Correlation, original.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(late)
				state = driverState(t, d)
				if state.NativeResources[parameters.Resource.ID] != before || state.ReconciledEffects[original.ActionID].Status != "WITHDRAWN" || state.HostActionReceipts[original.ActionID].Status != protocol.HostActionStatusExecuted {
					t.Fatal("late original completion rolled back current native facts")
				}
				if !reflect.DeepEqual(d.submit(late), state.Events[string(late.ID)].Acceptance) {
					t.Fatal("original receipt replay changed acceptance")
				}
			}
			if response != "pending" {
				d.submit(unknown)
			}
			before := driverState(t, d)
			d.reopen()
			d.drive()
			after := driverState(t, d)
			if !reflect.DeepEqual(before.NativeResources, after.NativeResources) || !reflect.DeepEqual(before.HostActionReceipts, after.HostActionReceipts) || !reflect.DeepEqual(before.ReconciledEffects, after.ReconciledEffects) {
				t.Fatal("reopen or duplicate observation changed settled native evidence")
			}
			if actual, err := os.ReadFile(target); err != nil || string(actual) != string(body) {
				t.Fatal("reopen or replay changed current mirror bytes", err)
			}
		})
	}
}
