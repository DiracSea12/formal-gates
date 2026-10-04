package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/lifecycle"
)

// reviewReadySlice drives a real Git child through development and candidate freeze.
func reviewReadySlice(t *testing.T, mode string, owner OwnerBinding) *validationDriver {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	master := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
	passPreDevelopmentReviews(t, master, commandPlan())
	master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: master.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: master.revision, Document: validationDocument}}})
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	options := sliceChildStart(master, "child", "lane-core", sliceChildReceipt(master.revision))
	options.Request.BaseSnapshot = base
	options.Owner = owner
	f, run, err := semanticValidationFacade(t, options)
	if err != nil {
		t.Fatal(err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: master.git}
	route := protocol.RouteDecision{Mode: mode, Gates: []string{"quality", "security"}}
	if mode == "custom" {
		route.Selected = []string{"security"}
	}
	child.confirmSliceRoute(route, sliceLanePlan())
	child.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: child.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: child.revision, Document: validationDocument}}})
	child.drive()
	id, input := planBatchAction(child, "command")
	if err := os.WriteFile(filepath.Join(root, "app_test.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotCommitOne(t, root, "implement command")
	evidence := batchEvidence(input)
	evidence.Paths = append(evidence.Paths, "app_test.go")
	child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: evidence})
	for {
		child.drive()
		if len(child.run.AdapterIntents) == 0 {
			break
		}
		child.materialize()
	}
	return child
}

func completeSliceGates(t *testing.T, child *validationDriver) {
	t.Helper()
	for _, gate := range driverState(t, child).SelectedGates {
		id, input := child.gateAction(gate)
		child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	}
}

// promotedSlice drives selected gates and binds the verified final native delivery.
func promotedSlice(t *testing.T, mode string, owner OwnerBinding) *validationDriver {
	t.Helper()
	child := reviewReadySlice(t, mode, owner)
	completeSliceGates(t, child)
	if _, err := child.f.PrepareCandidate("verified child delivery"); err != nil {
		t.Fatal(err)
	}
	return child
}

func promotedSliceWithAuthorizedRetry(t *testing.T, mode string, owner OwnerBinding) (*validationDriver, protocol.PendingAsk, protocol.Event) {
	t.Helper()
	child := reviewReadySlice(t, mode, owner)
	for _, gate := range driverState(t, child).SelectedGates {
		if gate != "security" {
			id, input := child.gateAction(gate)
			child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		id, _ := child.gateAction("security")
		child.receipt(id)
		child.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, PayloadDigest: id + "-unavailable", FailureClass: "TRANSIENT_ENGINE_ERROR"}})
		child.drive()
	}
	face := child.validationAsk("RETRY")
	offer := driverState(t, child).PendingAsks[face.RequestID]
	if offer.Resolved || offer.Validation == nil || offer.Validation.Digest == "" {
		t.Fatal("retry does not own a current validation offer")
	}
	event := decideValidation(child, face, "approve")
	child.drive()
	id, input := child.gateAction("security")
	child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	if _, err := child.f.PrepareCandidate(""); err != nil {
		t.Fatal(err)
	}
	return child, offer, event
}

func assertSliceRequestEvidence(t *testing.T, child *validationDriver, receipt protocol.SliceReceipt, offer protocol.PendingAsk, event protocol.Event) {
	t.Helper()
	var canonical bytes.Buffer
	enc := json.NewEncoder(&canonical)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(offer); err != nil {
		t.Fatal(err)
	}
	decisionDigest, err := event.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := driverState(t, child)
	if _, external := state.Events[offer.RequestID]; external {
		t.Fatal("generated request was manufactured as an external event")
	}
	want := &protocol.SliceRequestIdentity{RequestID: offer.RequestID, EventID: string(event.ID), RequestDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(canonical.Bytes())), DecisionDigest: decisionDigest, Revision: state.Decisions[offer.RequestID].Revision}
	if !reflect.DeepEqual(receipt.Receipt.LastRequest, want) {
		t.Fatalf("frozen request evidence = %+v, want %+v", receipt.Receipt.LastRequest, want)
	}
}

func TestSliceReceiptBindsTheAuthorizedGeneratedRequest(t *testing.T) {
	child, offer, event := promotedSliceWithAuthorizedRetry(t, "default", OwnerBinding{})
	child.drive()
	assertSliceRequestEvidence(t, child, readSliceReceipt(t, child), offer, event)
}

func assertNoSuccessfulSliceReceipt(t *testing.T, child *validationDriver) {
	t.Helper()
	if state := driverState(t, child); state.SliceReceipt != nil || state.Phase == runtime.PhaseSliceReady {
		t.Fatal("waiting/rejected child froze a successful checkpoint")
	}
	if _, err := os.Stat(filepath.Join(child.f.stateDir, "slice-receipt.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("waiting/rejected child published receipt: %v", err)
	}
}

func TestSliceCheckpointRequiresAllDeliveryFacts(t *testing.T) {
	t.Run("selected gate missing", func(t *testing.T) {
		child := reviewReadySlice(t, "full", OwnerBinding{})
		id, input := child.gateAction("quality")
		child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		if _, err := child.f.PrepareCandidate(""); err == nil {
			t.Fatal("promotion admitted while security gate is missing")
		}
		state := driverState(t, child)
		if err := child.f.engine.CheckpointSlice(state.Validation.Candidate.Identity, state.Validation.Candidate.Digest, nil, "", emptyFingerprint()); err == nil {
			t.Fatal("checkpoint admitted missing gate")
		}
		child.drive()
		assertNoSuccessfulSliceReceipt(t, child)
	})
	t.Run("promotion missing", func(t *testing.T) {
		child := reviewReadySlice(t, "custom", OwnerBinding{})
		completeSliceGates(t, child)
		state := driverState(t, child)
		if err := child.f.engine.CheckpointSlice(state.Validation.Candidate.Identity, state.Validation.Candidate.Digest, nil, "", emptyFingerprint()); err == nil {
			t.Fatal("checkpoint admitted unbound promotion")
		}
		child.drive()
		assertNoSuccessfulSliceReceipt(t, child)
	})
	for _, kind := range []string{"dirty", "drift", "tree", "barrier", "abort", "independent-seal"} {
		t.Run(kind, func(t *testing.T) {
			child := promotedSlice(t, "default", OwnerBinding{})
			state := driverState(t, child)
			var err error
			switch kind {
			case "dirty":
				if err := os.WriteFile(filepath.Join(child.f.root, "unsubmitted.go"), []byte("package main\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err = child.f.Drive()
			case "drift":
				snapshotCommitOne(t, child.f.root, "different delivery")
				_, err = child.f.Drive()
			case "tree":
				err = child.f.engine.CheckpointSlice(state.PromotedSnapshot, strings.Repeat("1", 40), nil, "", emptyFingerprint())
			case "barrier":
				request, e := protocol.NewRequestEvent(child.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
				if e != nil {
					t.Fatal(e)
				}
				child.submit(request)
				err = child.f.engine.CheckpointSlice(state.PromotedSnapshot, state.PromotedDigest, nil, "", emptyFingerprint())
				child.drive()
			case "abort":
				abortRun(t, child)
				err = child.f.engine.CheckpointSlice(state.PromotedSnapshot, state.PromotedDigest, nil, "", emptyFingerprint())
			case "independent-seal":
				err = child.f.engine.SealTerminal(state.PromotedSnapshot, emptyFingerprint())
			}
			if err == nil {
				t.Fatalf("%s was accepted", kind)
			}
			assertNoSuccessfulSliceReceipt(t, child)
		})
	}
}

func TestSliceCheckpointPreservesUnavailableOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")
	child := promotedSlice(t, "custom", OwnerBinding{Transcript: path, Provider: meteredProvider})
	child.drive()
	r := readSliceReceipt(t, child).Receipt
	if r.Cost.Owner == nil || *r.Cost.Owner != (cost.OwnerCost{Source: cost.SourceUnavailable}) || r.OwnerTranscript != path || r.OwnerStartedAt == "" || r.OwnerEndedAt == "" {
		t.Fatalf("unavailable owner was fabricated or dropped: %+v", r)
	}
}

func TestConcurrentSliceCheckpointFreezesOwnerOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	writeTranscript(t, path, []string{intervalBaseLine})
	child, offer, event := promotedSliceWithAuthorizedRetry(t, "default", OwnerBinding{Transcript: path, Provider: meteredProvider})
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
	if _, _, err := child.f.engine.Drive(emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	before, err := child.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	var writers []*Facade
	for i := 0; i < 3; i++ {
		f, err := Open(child.f.root, child.f.runID)
		if err != nil {
			t.Fatal(err)
		}
		writers = append(writers, f)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(writers))
	for _, writer := range writers {
		wg.Add(1)
		go func(f *Facade) { defer wg.Done(); _, err := f.Drive(); errs <- err }(writer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := child.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || after.State.Cost.Owner == nil || after.State.Cost.TotalInputTokens != before.State.Cost.TotalInputTokens+17 || after.State.Cost.OutputTokens != before.State.Cost.OutputTokens+8 {
		t.Fatalf("concurrent checkpoint did not freeze exactly once: before=%d after=%d cost=%+v", before.Revision, after.Revision, after.State.Cost)
	}
	assertSliceRequestEvidence(t, child, readSliceReceipt(t, child), offer, event)
}

func TestSliceChildPromotionReachesRecoverableCheckpoint(t *testing.T) {
	for _, mode := range []string{"default", "full", "custom"} {
		t.Run(mode, func(t *testing.T) {
			child := promotedSlice(t, mode, OwnerBinding{})
			before := driverState(t, child)
			wave := before.Validation.Waves[len(before.Validation.Waves)-1]
			var lastID string
			var lastRevision uint64
			for id, event := range before.Events {
				if event.Acceptance.Revision > lastRevision {
					lastID, lastRevision = id, event.Acceptance.Revision
				}
			}
			child.drive()
			state := driverState(t, child)
			if state.Phase != runtime.PhaseSliceReady || child.run.Status != "SLICE_READY" {
				t.Fatalf("child completed independently: phase=%s status=%s", state.Phase, child.run.Status)
			}
			if child.run.Next.Kind != decision.KindWait || child.run.Next.Wait.Reason != decision.WaitDependencies || child.run.SummaryPath != "" {
				t.Fatalf("checkpoint boundary = %+v", child.run)
			}
			receipt := readSliceReceipt(t, child)
			r := receipt.Receipt
			tree, err := child.git.TreeDigest(before.PromotedSnapshot)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != runtime.PhaseSliceReady || r.ChildRunID != "child" || r.MasterRunID != "validation" || r.SliceID != "lane-core" || r.Envelope != child.f.envelope || r.Snapshot != before.PromotedSnapshot || r.Tree != tree || r.Digest != tree || r.RequirementRevision != child.revision || !reflect.DeepEqual(r.Topology, *before.Topology) || r.RouteMode != mode || !reflect.DeepEqual(r.SelectedGates, before.SelectedGates) {
				t.Fatalf("receipt bindings = %+v", r)
			}
			if content, err := (vcs.ExecRunner{}).Run(child.f.root, "git", "show", r.Snapshot+":app_test.go"); err != nil || content != "package main" {
				t.Fatalf("final native identity lost developer tests: content=%q error=%v", content, err)
			}
			v := r.Validation
			if v.WaveID != wave.ID || v.Status != "COMPLETE" || v.RequirementRevision != child.revision || v.Generation != wave.Generation || v.RouteDigest != wave.RouteDigest || v.ExpectedDigest != wave.ExpectedDigest || v.Candidate != wave.Candidate || v.Scope != wave.Scope || len(v.Results) != len(before.SelectedGates) {
				t.Fatalf("final wave receipt = %+v", v)
			}
			for _, result := range v.Results {
				if result.Target.Kind != "GATE" || result.Target.Status != "PASS" || !reflect.DeepEqual(result.Target, *wave.Targets[result.Target.Task.String()]) || !reflect.DeepEqual(result.Result, before.Results[result.Target.ResultAction]) || result.Result.Validation == nil || result.Result.Validation.Binding.WaveID != wave.ID {
					t.Fatalf("gate evidence does not bind the final wave: %+v", result)
				}
			}
			if r.SelectedQA == nil || len(r.SelectedQA) != 0 || r.ApprovedCases == nil || len(r.ApprovedCases) != 0 || !reflect.DeepEqual(r.Cost, before.Cost) || r.OwnerTranscript != "" || r.OwnerStartedAt != "" || r.OwnerEndedAt != "" {
				t.Fatalf("QA-free and cost facts = %+v", r)
			}
			if r.LastEvent == nil || r.LastEvent.EventID != lastID || r.LastEvent.Digest != before.Events[lastID].Digest || r.LastEvent.Revision != before.Events[lastID].Acceptance.Revision || r.LastRequest == nil || r.LastRequest.RequestID != "event-1" || r.LastRequest.EventID != "event-2" || r.LastRequest.RequestDigest != before.Events["event-1"].Digest || r.LastRequest.DecisionDigest != before.Events["event-2"].Digest || r.LastRequest.Revision != before.Decisions["event-1"].Revision {
				t.Fatalf("last request/event identity = %+v / %+v", r.LastRequest, r.LastEvent)
			}
			if state.IntakeReceipt == nil || state.Validation == nil || len(state.Results) == 0 || len(state.DispatchBindings) == 0 || state.SliceReceipt == nil || !reflect.DeepEqual(child.run.SliceReceipt, state.SliceReceipt) {
				t.Fatal("checkpoint discarded recovery evidence or query receipt")
			}
			for _, name := range []string{"terminal-summary.json", terminalIntentName} {
				if _, err := os.Stat(filepath.Join(child.f.stateDir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("child has terminal artifact %s: %v", name, err)
				}
			}
			active, err := lifecycle.ActiveRunIDs(child.f.root)
			if err != nil || !slices.Contains(active, child.f.runID) {
				t.Fatalf("child lifecycle was ended: active=%v error=%v", active, err)
			}
		})
	}
}

func readSliceReceipt(t *testing.T, child *validationDriver) protocol.SliceReceipt {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(child.f.stateDir, "slice-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt protocol.SliceReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Digest != receipt.Receipt.CanonicalDigest() || !reflect.DeepEqual(&receipt, driverState(t, child).SliceReceipt) {
		t.Fatalf("sidecar is not the state-owned receipt: %s", data)
	}
	return receipt
}

func TestSliceReceiptRemainsFrozenAcrossReplayAndLaterCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	writeTranscript(t, path, []string{intervalBaseLine})
	child, offer, event := promotedSliceWithAuthorizedRetry(t, "default", OwnerBinding{Transcript: path, Provider: meteredProvider})
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
	child.drive()
	receipt := readSliceReceipt(t, child)
	assertSliceRequestEvidence(t, child, receipt, offer, event)
	if receipt.Receipt.Cost.Owner == nil || *receipt.Receipt.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 17, InputCacheHitTokens: 2, InputCacheMissTokens: 15, OutputTokens: 8}) || receipt.Receipt.OwnerTranscript != path || receipt.Receipt.OwnerStartedAt == "" || receipt.Receipt.OwnerEndedAt == "" {
		t.Fatalf("owner receipt = %+v", receipt.Receipt)
	}
	stateBytes, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	bytesBefore, err := os.ReadFile(filepath.Join(child.f.stateDir, "slice-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshotCommitOne(t, child.f.root, "later authorized work")
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine, intervalLaterLine})
	child.reopen()
	for i := 0; i < 2; i++ {
		child.drive()
	}
	if _, err := child.f.PrepareCandidate("another candidate"); err == nil {
		t.Fatal("checkpoint allowed a second candidate promotion")
	}
	bytesAfter, _ := os.ReadFile(filepath.Join(child.f.stateDir, "slice-receipt.json"))
	stateAfter, _ := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if !bytes.Equal(bytesBefore, bytesAfter) || !bytes.Equal(stateBytes, stateAfter) || !reflect.DeepEqual(readSliceReceipt(t, child), receipt) {
		t.Fatal("replay changed frozen identity, cost, endpoint or state")
	}
	for _, query := range []func() (Run, error){child.f.Show, child.f.Status} {
		run, err := query()
		if err != nil || run.Status != "SLICE_READY" || !reflect.DeepEqual(run.SliceReceipt, &receipt) {
			t.Fatalf("checkpoint query = %+v, %v", run, err)
		}
	}
	next, err := child.f.Next()
	if err != nil || next.Kind != decision.KindWait {
		t.Fatalf("checkpoint next = %+v, %v", next, err)
	}
	// The state-owned endpoint is already closed before sidecar discovery.
	if err := os.Remove(filepath.Join(child.f.stateDir, "slice-receipt.json")); err != nil {
		t.Fatal(err)
	}
	later := startIntervalRun(t, child.f.root, "later-owner", path)
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine, intervalLaterLine, `{"type":"assistant","uuid":"last","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":1,"output_tokens":2}}}`})
	laterRun, err := later.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if laterRun.Cost.Owner == nil || *laterRun.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 6, InputCacheHitTokens: 1, InputCacheMissTokens: 5, OutputTokens: 2}) {
		t.Fatalf("closed child interval overlaps later owner: %+v", laterRun.Cost.Owner)
	}
	child.drive()
	if !reflect.DeepEqual(readSliceReceipt(t, child), receipt) {
		t.Fatal("missing sidecar caused remeasurement")
	}
}

func TestSliceCheckpointRecoveryUsesTheSameFrozenReceipt(t *testing.T) {
	for _, point := range []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		t.Run(string(point), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.jsonl")
			writeTranscript(t, path, []string{intervalBaseLine})
			child, offer, event := promotedSliceWithAuthorizedRetry(t, "custom", OwnerBinding{Transcript: path, Provider: meteredProvider})
			writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
			// Settle the engine's internal frontier before injecting the checkpoint transaction.
			if _, _, err := child.f.engine.Drive(emptyFingerprint()); err != nil {
				t.Fatal(err)
			}
			store, err := persistence.NewStore(child.f.stateDir, persistence.Config{PackageDigest: child.f.envelope.PackageDigest, DefinitionSource: child.f.envelope.DefinitionSource, InstalledTargetIdentity: child.f.envelope.InstalledTargetIdentity, AcquireAdmission: child.f.admission.acquire, FaultInjector: func(at persistence.FaultPoint) error {
				if at == point {
					return &persistence.InjectedCrashError{Point: at}
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			eng, err := protocol.New(store, protocol.Config{Definition: child.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(child.f.root, child.f.runID), ClaimDispatch: dispatchClaimBridge(child.f.root, child.f.runID, child.f.admission)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			child.f.store, child.f.engine = store, eng
			if _, err := child.f.Drive(); err == nil {
				t.Fatal("checkpoint fault did not interrupt")
			}
			if _, err := os.Stat(filepath.Join(child.f.stateDir, "slice-receipt.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("sidecar published across interruption: %v", err)
			}
			var frozen *protocol.SliceReceipt
			if point != persistence.FaultIntentBefore {
				frozen = driverState(t, child).SliceReceipt
				if frozen == nil {
					t.Fatal("post-commit interruption lost checkpoint")
				}
				if frozen.Receipt.Cost.Owner == nil || frozen.Receipt.Cost.Owner.TotalInputTokens != 17 || frozen.Receipt.OwnerEndedAt == "" {
					t.Fatal("post-commit interruption split receipt, owner cost or endpoint")
				}
				writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine, intervalLaterLine})
				snapshotCommitOne(t, child.f.root, "later work after checkpoint commit")
			} else if driverState(t, child).Cost.Owner != nil {
				t.Fatal("failed checkpoint committed owner separately")
			}
			child.reopen()
			child.drive()
			receipt := readSliceReceipt(t, child)
			assertSliceRequestEvidence(t, child, receipt, offer, event)
			if frozen != nil && !reflect.DeepEqual(&receipt, frozen) {
				t.Fatal("recovery replaced the committed receipt")
			}
			if child.run.Status != "SLICE_READY" || receipt.Receipt.Cost.Owner == nil || receipt.Receipt.Cost.Owner.TotalInputTokens != 17 {
				t.Fatalf("recovered checkpoint = %+v", child.run)
			}
			child.drive()
		})
	}
}
