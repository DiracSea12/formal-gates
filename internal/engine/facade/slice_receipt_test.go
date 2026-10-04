package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func receiptMaster(t *testing.T) *validationDriver {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
	meterReceiptDispatches(d, 3, 9, 4)
	passPreDevelopmentReviews(t, d, commandPlan())
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	return d
}

// The metering seam parses one transcript bound to each actual dispatch identity.
func meterReceiptDispatches(d *validationDriver, hit, miss, output int64) {
	d.t.Helper()
	resolver := func(ref protocol.CostSourceRef) protocol.ResolvedCostSource {
		state := driverState(d.t, d)
		_, pending := state.PendingActions[ref.ActionID]
		spawn, spawned := state.SpawnReceipts[ref.ActionID]
		if ref.RunID != d.f.runID || ref.Provider != d.host() || !pending && !spawned || ref.Correlation != ref.ActionID || spawned && spawn.Correlation != ref.Correlation {
			d.t.Fatalf("metering reference lost its actual run/dispatch/provider: %+v", ref)
		}
		path := filepath.Join(d.f.root, ".gates", "tmp", ref.RunID, "metering", digestOf(ref.ActionID)+".jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			d.t.Fatal(err)
		}
		line := fmt.Sprintf(`{"type":"assistant","uuid":%q,"message":{"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`, ref.ActionID, miss, hit, output)
		if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
			d.t.Fatal(err)
		}
		usage, err := cost.ParseTranscript(meteredProvider, path)
		if err != nil {
			d.t.Fatal(err)
		}
		return protocol.ResolvedCostSource{State: protocol.CostSourceReady, Usage: usage}
	}
	eng, err := protocol.New(d.f.store, protocol.Config{Definition: d.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: resolver, ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission)}, nil)
	if err != nil {
		d.t.Fatal(err)
	}
	d.f.engine = eng
}

func receiptChild(t *testing.T, master *validationDriver, childID, sliceID, mode string, owner OwnerBinding, admission ...*Admission) *validationDriver {
	t.Helper()
	options := sliceChildStart(master, childID, sliceID, sliceChildReceipt(master.revision))
	options.Request.BaseSnapshot = master.run.BaseSnapshot
	options.Owner = owner
	if len(admission) != 0 {
		options.Admission = admission[0]
	}
	f, run, err := semanticValidationFacade(t, options)
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: master.git}
	meterReceiptDispatches(d, 1, 4, 2)
	route := protocol.RouteDecision{Mode: mode, Gates: []string{"quality", "security"}}
	if mode == "custom" {
		route.Selected = []string{"security"}
	}
	d.confirmSliceRoute(route, sliceLanePlan())
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.drive()
	id, input := planBatchAction(d, "command")
	path := childID + "_test.go"
	if err := os.WriteFile(filepath.Join(f.root, path), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotCommitOne(t, f.root, "implement "+sliceID)
	evidence := batchEvidence(input)
	evidence.Paths = append(evidence.Paths, path)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: evidence})
	for {
		d.drive()
		if len(d.run.AdapterIntents) == 0 {
			break
		}
		d.materialize()
	}
	return d
}

func promoteReceiptChild(t *testing.T, child *validationDriver) {
	t.Helper()
	completeSliceGates(t, child)
	if _, err := child.f.PrepareCandidate("verified child delivery"); err != nil {
		t.Fatal(err)
	}
}

func publishReceiptChild(t *testing.T, child *validationDriver) protocol.SliceReceipt {
	t.Helper()
	if err := child.f.checkpointDelivered(); err != nil {
		t.Fatal(err)
	}
	return readSliceReceipt(t, child)
}

func assertMasterReceiptProjection(t *testing.T, master *validationDriver, own *protocol.State, receipts ...protocol.SliceReceipt) {
	t.Helper()
	state := driverState(t, master)
	want := &cost.RunCost{}
	if own.Cost != nil {
		data, _ := json.Marshal(own.Cost)
		if err := json.Unmarshal(data, want); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.SliceReceipts) != len(receipts) {
		t.Fatalf("accepted children = %d, want %d", len(state.SliceReceipts), len(receipts))
	}
	for _, receipt := range receipts {
		r := receipt.Receipt
		if !reflect.DeepEqual(state.SliceReceipts[r.ChildRunID], receipt) || len(r.SelectedQA) != 0 || r.SelectedQA == nil || len(r.ApprovedCases) != 0 || r.ApprovedCases == nil {
			t.Fatalf("master lost child identity/cases payload: %+v", state.SliceReceipts)
		}
		for id, entry := range r.Cost.Dispatches {
			key := r.ChildRunID + "/" + id
			if !reflect.DeepEqual(state.Cost.Dispatches[key], entry) {
				t.Fatalf("namespaced source %s changed", key)
			}
			want.TotalInputTokens += entry.TotalInputTokens
			want.InputCacheHitTokens += entry.InputCacheHitTokens
			want.InputCacheMissTokens += entry.InputCacheMissTokens
			want.OutputTokens += entry.OutputTokens
			if want.Dispatches == nil {
				want.Dispatches = map[string]cost.DispatchCost{}
			}
			want.Dispatches[key] = entry
		}
		if r.Cost.Owner != nil {
			want.TotalInputTokens += r.Cost.Owner.TotalInputTokens
			want.InputCacheHitTokens += r.Cost.Owner.InputCacheHitTokens
			want.InputCacheMissTokens += r.Cost.Owner.InputCacheMissTokens
			want.OutputTokens += r.Cost.Owner.OutputTokens
		}
	}
	if !reflect.DeepEqual(state.Cost, want) || !reflect.DeepEqual(state.Attempts, own.Attempts) || !reflect.DeepEqual(state.SpawnReceipts, own.SpawnReceipts) || !reflect.DeepEqual(state.DispatchBindings, own.DispatchBindings) {
		t.Fatalf("master cost/own dispatch facts changed: cost=%+v want=%+v", state.Cost, want)
	}
	data, err := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		persistence.Envelope
		Content protocol.State `json:"content"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.Envelope != master.f.envelope || !reflect.DeepEqual(stored.Content.SliceReceipts, state.SliceReceipts) || !reflect.DeepEqual(stored.Content.Cost, want) {
		t.Fatalf("durable envelope/content differs: error=%v data=%s", err, data)
	}
	if !reflect.DeepEqual(state.Guard, own.Guard) {
		t.Fatal("imported dispatch manufactured master request/attempt allowance")
	}
	for _, query := range []func() (Run, error){master.f.Show, master.f.Status} {
		run, err := query()
		if err != nil || run.Status != "ACTIVE" || run.Next.Kind == decision.KindComplete || !reflect.DeepEqual(run.SliceReceipts, state.SliceReceipts) || !reflect.DeepEqual(run.SliceChildren, state.SliceChildren) {
			t.Fatalf("master query = %+v, %v", run, err)
		}
	}
	if next, err := master.f.Next(); err != nil || next.Kind == decision.KindComplete {
		t.Fatalf("master next crossed the integration boundary: %+v, %v", next, err)
	}
	after, err := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("read-only master queries mutated durable state")
	}
}

func TestMasterReceiptAcceptancePreservesCostsAcrossConcurrencyAndReplay(t *testing.T) {
	master := receiptMaster(t)
	own := driverState(t, master)
	path := filepath.Join(t.TempDir(), "core-owner.jsonl")
	writeTranscript(t, path, []string{intervalBaseLine})
	core := receiptChild(t, master, "core", "lane-core", "custom", OwnerBinding{Transcript: path, Provider: meteredProvider})
	promoteReceiptChild(t, core)
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
	first := publishReceiptChild(t, core)
	if *first.Receipt.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 17, InputCacheHitTokens: 2, InputCacheMissTokens: 15, OutputTokens: 8}) {
		t.Fatal("owner fixture does not have the known transcript interval")
	}
	for _, entry := range first.Receipt.Cost.Dispatches {
		if entry.Source != cost.SourceTranscript || entry.InputCacheHitTokens != 1 || entry.InputCacheMissTokens != 4 || entry.TotalInputTokens != 5 || entry.OutputTokens != 2 {
			t.Fatalf("child dispatch differs from its known transcript: %+v", entry)
		}
	}
	tooling := receiptChild(t, master, "tooling", "lane-tooling", "custom", OwnerBinding{})
	tooling.reopen()
	promoteReceiptChild(t, tooling)
	second := publishReceiptChild(t, tooling)
	unavailable := false
	for _, entry := range second.Receipt.Cost.Dispatches {
		if entry.Source == cost.SourceUnavailable {
			unavailable = true
			if entry.TotalInputTokens != 0 || entry.InputCacheHitTokens != 0 || entry.InputCacheMissTokens != 0 || entry.OutputTokens != 0 {
				t.Fatal("unavailable dispatch received fabricated usage")
			}
		}
	}
	if !unavailable || second.Receipt.Cost.Owner != nil {
		t.Fatal("missing child transcript/owner did not retain its actual availability")
	}
	common := false
	for id := range first.Receipt.Cost.Dispatches {
		if _, exists := second.Receipt.Cost.Dispatches[id]; exists {
			common = true
		}
	}
	if !common {
		t.Fatal("normal child dispatch identities did not exercise the shared namespace key")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, receipt := range []protocol.SliceReceipt{first, first, second} {
		wg.Add(1)
		go func(r protocol.SliceReceipt) { defer wg.Done(); errs <- master.f.acceptSliceReceipt(r) }(receipt)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertMasterReceiptProjection(t, master, own, first, second)
	before, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	errs = make(chan error, 3)
	for _, f := range []*Facade{core.f, tooling.f, master.f} {
		wg.Add(1)
		go func(f *Facade) { defer wg.Done(); _, err := f.Drive(); errs <- err }(f)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		core.reopen()
		core.drive()
		master.reopen()
		master.drive()
	}
	after, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	next, _ := json.Marshal(master.run.Next)
	if !bytes.Equal(before, after) || !strings.Contains(string(next), "waiting for integration") {
		t.Fatalf("replay mutated acceptance or crossed integration: next=%+v", master.run.Next)
	}
}

func TestMasterWaitsForExactClaimsAndRejectsReceiptConflicts(t *testing.T) {
	master := receiptMaster(t)
	master.drive()
	if len(driverState(t, master).SliceReceipts) != 0 || master.run.Status != "ACTIVE" {
		t.Fatal("unclaimed slices did not wait")
	}
	child := receiptChild(t, master, "core", "lane-core", "custom", OwnerBinding{})
	master.drive()
	if len(driverState(t, master).SliceReceipts) != 0 {
		t.Fatal("unfinished child was accepted")
	}
	if _, err := master.f.engine.ClaimSlice("lane-tooling", "waiting", master.revision, master.run.BaseSnapshot, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	master.drive()
	if len(driverState(t, master).SliceReceipts) != 0 {
		t.Fatal("claim without child state fabricated receipt")
	}
	promoteReceiptChild(t, child)
	receipt := publishReceiptChild(t, child)
	for _, field := range []string{"master", "slice", "child", "requirement", "topology", "envelope", "route", "status"} {
		t.Run(field, func(t *testing.T) {
			bad := receipt
			switch field {
			case "master":
				bad.Receipt.MasterRunID = "other-master"
			case "slice":
				bad.Receipt.SliceID = "lane-tooling"
			case "child":
				bad.Receipt.ChildRunID = "other-child"
			case "requirement":
				bad.Receipt.RequirementRevision = "other-revision"
				bad.Receipt.Validation.RequirementRevision = "other-revision"
			case "topology":
				bad.Receipt.Topology.Note = "different confirmed topology"
			case "envelope":
				bad.Receipt.Envelope.PackageDigest = "sha256:other-package"
			case "route":
				bad.Receipt.RouteMode = "default"
			case "status":
				bad.Receipt.Status = runtime.PhaseTerminal
			}
			bad.Digest = bad.Receipt.CanonicalDigest()
			before, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			if err := master.f.acceptSliceReceipt(bad); err == nil {
				t.Fatal("mismatched receipt was accepted")
			}
			after, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("rejected receipt changed costs or acceptance")
			}
		})
	}
	child.drive()
	master.drive()
	if len(driverState(t, master).SliceReceipts) != 1 {
		t.Fatal("child Drive did not directly deliver its receipt")
	}
	before, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	conflict := receipt
	conflict.Receipt.Snapshot = master.run.BaseSnapshot
	conflict.Digest = conflict.Receipt.CanonicalDigest()
	if err := master.f.acceptSliceReceipt(conflict); err == nil {
		t.Fatal("same child different digest was accepted")
	}
	after, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
	next, _ := json.Marshal(master.run.Next)
	if !bytes.Equal(before, after) || !strings.Contains(string(next), "1/2 accepted") || strings.Contains(string(next), "all expected slices") {
		t.Fatal("partial receipts or duplicate child identity became all-ready")
	}
}

func TestMasterReceiptDiscoveryRecoversPublicationAndAcceptance(t *testing.T) {
	for _, point := range []persistence.FaultPoint{"", persistence.FaultIntentBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		t.Run(string(point), func(t *testing.T) {
			master := receiptMaster(t)
			own := driverState(t, master)
			path := filepath.Join(t.TempDir(), "owner.jsonl")
			writeTranscript(t, path, []string{intervalBaseLine})
			child := receiptChild(t, master, "core", "lane-core", "custom", OwnerBinding{Transcript: path, Provider: meteredProvider})
			promoteReceiptChild(t, child)
			writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
			receipt := publishReceiptChild(t, child)
			if err := os.Remove(filepath.Join(child.f.stateDir, "slice-receipt.json")); err != nil {
				t.Fatal(err)
			}
			if point != "" {
				store, err := persistence.NewStore(master.f.stateDir, persistence.Config{PackageDigest: master.f.envelope.PackageDigest, DefinitionSource: master.f.envelope.DefinitionSource, InstalledTargetIdentity: master.f.envelope.InstalledTargetIdentity, AcquireAdmission: master.f.admission.acquire, FaultInjector: func(at persistence.FaultPoint) error {
					if at == point {
						return &persistence.InjectedCrashError{Point: at}
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				eng, err := protocol.New(store, protocol.Config{Definition: master.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(master.f.root, master.f.runID), ClaimDispatch: dispatchClaimBridge(master.f.root, master.f.runID, master.f.admission)}, nil)
				if err != nil {
					t.Fatal(err)
				}
				master.f.store, master.f.engine = store, eng
				if _, err := master.f.Drive(); err == nil {
					t.Fatal("master acceptance fault did not interrupt")
				}
				accepted := driverState(t, master).SliceReceipts
				if point == persistence.FaultIntentBefore && len(accepted) != 0 || point != persistence.FaultIntentBefore && !reflect.DeepEqual(accepted[child.f.runID], receipt) {
					t.Fatal("interruption split receipt and cost acceptance")
				}
			}
			// Later native work and transcript usage cannot redefine frozen child facts.
			snapshotCommitOne(t, child.f.root, "later authorized work")
			writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine, intervalLaterLine})
			master.reopen()
			master.drive()
			assertMasterReceiptProjection(t, master, own, receipt)
			if !reflect.DeepEqual(readSliceReceipt(t, child), receipt) {
				t.Fatal("master discovery rewrote published child evidence")
			}
			before, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			master.drive()
			after, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("recovered acceptance was not idempotent")
			}
		})
	}
}

func TestMasterReceiptAcceptsRepairCarryWithoutRewritingEvidence(t *testing.T) {
	master := receiptMaster(t)
	own := driverState(t, master)
	child := receiptChild(t, master, "core", "lane-core", "default", OwnerBinding{})
	quality, input := child.gateAction("quality")
	child.business(quality, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "documented output order differs"}}})
	security, input := child.gateAction("security")
	child.business(security, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	wave := child.run.Validation.Waves[0]
	var finding string
	for id := range child.run.Validation.Findings {
		finding = id
	}
	resolveGateFinding(child, finding, wave.ID)
	repair, input := child.action("engine.repair")
	snapshotCommitOne(t, child.f.root, "correct documented output ordering")
	child.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "corrected ordering", Build: []string{"build passed"}, Tests: []string{"command regression passed"}}})
	for {
		child.drive()
		if len(child.run.AdapterIntents) == 0 {
			break
		}
		child.materialize()
	}
	current := child.run.Validation.Waves[len(child.run.Validation.Waves)-1]
	child.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: current.ID, Validation: &protocol.ValidationObservation{Kind: "GATE_IMPACT", Gate: "security", Decision: "INHERIT", Reason: "The ordering repair leaves the checked security boundaries unchanged"}}})
	child.drive()
	if _, err := child.f.PrepareCandidate("verified child delivery"); err != nil {
		t.Fatal(err)
	}
	child.drive()
	receipt := readSliceReceipt(t, child)
	for _, result := range receipt.Receipt.Validation.Results {
		want := security
		if result.Target.Gate == "quality" {
			want = repair
		}
		if result.Target.ResultAction != want || result.Result.ActionID != want || result.Target.Status != "PASS" {
			t.Fatalf("repair/carry identity changed: %+v", result)
		}
	}
	if driverState(t, child).Results[quality].Validation.Status != "FAIL" {
		t.Fatal("raw gate FAIL was rewritten")
	}
	assertMasterReceiptProjection(t, master, own, receipt)
}

func TestMasterReceiptHistorySurvivesRequirementScopeChanges(t *testing.T) {
	for _, scope := range []string{"requirement", "topology", "route"} {
		t.Run(scope, func(t *testing.T) {
			master := receiptMaster(t)
			child := receiptChild(t, master, "core", "lane-core", "custom", OwnerBinding{})
			promoteReceiptChild(t, child)
			child.drive()
			receipt := readSliceReceipt(t, child)
			master.run, _ = master.f.Show()
			before := driverState(t, master)
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			var choices []protocol.RequirementWorkDecision
			for _, item := range master.run.RequirementWork {
				choices = append(choices, protocol.RequirementWorkDecision{Work: item, Disposition: protocol.RequirementWorkCarry, Reason: "retain confirmed work outside the selected context scope"})
			}
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:context", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, RedoTopology: scope == "topology", RedoRoute: scope == "route", WorkDecisions: choices}
			event, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm revised context"})
			if err != nil {
				t.Fatal(err)
			}
			master.submit(event)
			master.drive()
			if master.run.Next.Kind != decision.KindAsk || !reflect.DeepEqual(driverState(t, master).SliceReceipts[child.f.runID], receipt) {
				t.Fatal("Phase A discarded or rejected accepted evidence")
			}
			ask := master.run.PendingRequirementAsk
			master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, RedoTopology: pkg.RedoTopology, RedoRoute: pkg.RedoRoute, WorkDecisions: choices, Reason: "confirm the current context scope"}}})
			master.reopen()
			master.drive()
			state := driverState(t, master)
			if !reflect.DeepEqual(state.SliceReceipts[child.f.runID], receipt) || !reflect.DeepEqual(state.Cost, before.Cost) || state.SliceReceiptCurrent(receipt) || scope == "topology" && state.Topology != nil || scope == "route" && state.RouteMode != "" {
				t.Fatal("Phase B history was coupled to the superseded current context")
			}
			next, _ := json.Marshal(master.run.Next)
			if strings.Contains(string(next), "all expected slices are ready") {
				t.Fatal("historical receipt qualified as current ready")
			}
			frozen, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			child.reopen()
			child.drive()
			replayed, _ := os.ReadFile(filepath.Join(master.f.stateDir, "state.json"))
			if !bytes.Equal(frozen, replayed) {
				t.Fatal("historical duplicate changed the current master context")
			}
		})
	}
}

func TestMasterDriveAcceptsThePublishedChildReceipt(t *testing.T) {
	child := promotedSlice(t, "custom", OwnerBinding{})
	child.drive()
	receipt := readSliceReceipt(t, child)
	master, err := Open(child.f.root, receipt.Receipt.MasterRunID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := master.Drive()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		SliceReceipts map[string]protocol.SliceReceipt `json:"sliceReceipts"`
	}
	if err := json.Unmarshal(data, &projection); err != nil {
		t.Fatal(err)
	}
	if got, accepted := projection.SliceReceipts[child.f.runID]; !accepted || !reflect.DeepEqual(got, receipt) {
		t.Fatalf("master did not accept its claimed published child receipt: accepted=%v got=%+v", accepted, got)
	}
}
