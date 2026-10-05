package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
)

// These trajectories use the public drive/submit surface and actual Git
// branches, conflicts, modes, artifacts, validation targets and cleanup.
func mainlineReady(t *testing.T, conflict string, identical bool) (*validationDriver, []*validationDriver) {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module command\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runner := vcs.ExecRunner{}
	if conflict == "binary" {
		if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte{0, 0}, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-m", "command module"); err != nil {
		t.Fatal(err)
	}
	master := nativeMasterAt(t, root, base, nil)
	passPreDevelopmentReviews(t, master, commandPlan())
	master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: master.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: master.revision, Document: validationDocument}}})
	topology := &protocol.TopologyDecision{Decision: "split", Slices: []string{"parser", "command", "tooling"}, ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "master-merge"}}, SplitCount: 3, Parallel: "three independent slices"}
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology})
	master.drive()
	design, input := master.action("engine.qa.design")
	if input.Manifest.Binding.ReviewKind != coverage.ReviewMerge {
		t.Fatal("retained master did not start merge QA design")
	}
	manifest := simpleManifest(input)
	for i := range manifest.Cases {
		manifest.Cases[i].Mode = coverage.CaseMerge
	}
	master.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	master.drive()
	reviewer, reviewInput := master.action("engine.qa.review.rationale")
	review := approvedReview(manifest)
	master.business(reviewer, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &review})
	master.drive()
	if driverState(t, master).Validation.Sets[coverage.ReviewMerge].Status != "FROZEN" {
		t.Fatal("merge cases did not freeze before child development")
	}
	var children []*validationDriver
	for index, sliceID := range topology.Slices {
		state := driverState(t, master)
		childFacade, err := Open(master.f.root, state.SliceChildren[sliceID])
		if err != nil {
			t.Fatal(err)
		}
		run, err := childFacade.Show()
		if err != nil {
			t.Fatal(err)
		}
		child := &validationDriver{t: t, f: childFacade, run: run, revision: master.revision, git: childFacade.vcs}
		route := protocol.RouteDecision{Mode: "full", Gates: []string{"quality", "security"}}
		if index == 1 {
			route.Mode = "custom"
			route.Selected = []string{"security"}
		}
		plan := sliceLanePlan()
		plan.Batches[0].Artifacts = []string{sliceID + ".go"}
		if identical {
			plan.Batches[0].Artifacts = []string{"app.go"}
		}
		child.confirmSliceRoute(route, plan)
		child.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: child.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: child.revision, Document: validationDocument}}})
		child.drive()
		id, in := planBatchAction(child, "command")
		path := sliceID + ".go"
		body := []byte("package main\n// " + sliceID + "\n")
		if identical {
			path = "app.go"
			body = []byte("package main\n// shared command behavior\n")
		}
		if conflict != "" && index < 2 {
			path = "app.go"
			body = []byte("package main\n// " + sliceID + " chooses command behavior\n")
		}
		if conflict == "binary" && index < 2 {
			path, body = "asset.bin", []byte{0, byte(index + 1)}
		}
		if conflict == "delete" && index == 0 {
			if err := os.Remove(filepath.Join(in.Workspace.Path, path)); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(filepath.Join(in.Workspace.Path, path), body, 0644); err != nil {
			t.Fatal(err)
		}
		if !identical {
			if err := os.WriteFile(filepath.Join(in.Workspace.Path, sliceID+".go"), []byte("package main\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		evidence := batchEvidence(in)
		if !slices.Contains(evidence.Paths, path) {
			evidence.Paths = append(evidence.Paths, path)
		}
		if index == 2 && !identical {
			if err := os.WriteFile(filepath.Join(in.Workspace.Path, "tool"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			evidence.Paths = append(evidence.Paths, "tool")
		}
		child.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: evidence})
		child.drive()
		completeSliceGates(t, child)
		if _, err := child.f.PrepareCandidate("child validated delivery"); err != nil {
			t.Fatal(err)
		}
		child.drive()
		children = append(children, child)
	}
	return master, children
}

func mainlineChildren(t *testing.T, conflict string) (*validationDriver, []*validationDriver) {
	master, children := mainlineReady(t, conflict, false)
	master.drive()
	return master, children

}

type mergeInterruption struct {
	vcs.Runner
	point           string
	interrupted     bool
	merges, commits int
}

func (r *mergeInterruption) Run(dir, command string, args ...string) (string, error) {
	out, err := r.Runner.Run(dir, command, args...)
	merge := command == "git" && slices.Contains(args, "--no-commit")
	commit := command == "git" && len(args) > 2 && args[0] == "commit" && strings.HasPrefix(args[2], "formal-gates merge ")
	candidate := command == "git" && len(args) > 2 && args[0] == "commit" && strings.HasPrefix(args[2], "formal-gates candidate ")
	cases := command == "git" && len(args) > 0 && args[0] == "add" && slices.Contains(args, ".gates/results/validation.merge-cases.md")
	promotion := command == "git" && len(args) > 1 && args[0] == "merge" && args[1] == "--ff-only"
	resolution := command == "git" && len(args) > 0 && args[0] == "add" && slices.Contains(args, "app.go")
	cleanup := command == "git" && len(args) > 1 && args[0] == "worktree" && args[1] == "remove"
	if merge {
		r.merges++
	}
	if commit {
		r.commits++
	}
	if err == nil && !r.interrupted && (r.point == "merge" && merge || r.point == "commit" && commit || r.point == "later-commit" && commit && r.commits == 2 || r.point == "resolution" && resolution || r.point == "candidate" && candidate || r.point == "cases" && cases || r.point == "promotion" && promotion || r.point == "cleanup" && cleanup) {
		r.interrupted = true
		panic("native response lost")
	}
	return out, err
}

func TestMainlineReopenAdmitsOriginalNativePrefixes(t *testing.T) {
	for _, point := range []string{"merge", "commit", "cases", "candidate"} {
		t.Run(point, func(t *testing.T) {
			r := &mergeInterruption{Runner: vcs.ExecRunner{}, point: point}
			master, _ := mainlineReady(t, "", false)
			root := master.f.root
			master.f.vcsRunner = r
			master.f.vcs = vcs.New(root, r)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("native interruption was not reached")
					}
				}()
				_, _ = master.f.Drive()
			}()
			master.reopen()
			master.f.vcsRunner = r
			master.f.vcs = vcs.New(root, r)
			master.drive()
			if driverState(t, master).Validation.Candidate == nil || r.merges != 3 || r.commits != 3 {
				t.Fatal("native prefix replayed or candidate omitted", r.merges, r.commits)
			}
			before := driverState(t, master).Validation.Candidate.Identity
			master.reopen()
			master.drive()
			if driverState(t, master).Validation.Candidate.Identity != before {
				t.Fatal("reopen changed frozen candidate")
			}
		})
	}
}

func passMainlineTargets(t *testing.T, master *validationDriver) {
	t.Helper()
	for len(master.run.ValidationInputs) > 0 {
		var id string
		var input protocol.ValidationInput
		for action, in := range master.run.ValidationInputs {
			if in.Handler == "engine.qa.execution" || in.Handler == "engine.gate" {
				id, input = action, in
				break
			}
		}
		if id == "" {
			t.Fatalf("unexpected mainline task: %+v", master.run.ValidationInputs)
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if input.Handler == "engine.qa.execution" {
			report := executionFor(input)
			result.Execution = &report
			result.Tool = "actual command contract"
			result.Environment = "native isolated candidate"
		}
		master.business(id, result)
		master.drive()
	}
}

func TestMainlineIntegratesFixedOrderWithFrozenCasesAndFullChildGates(t *testing.T) {
	master, children := mainlineChildren(t, "")
	state := driverState(t, master)
	if state.Validation.Candidate == nil || len(state.Validation.Waves) != 1 {
		t.Fatal("mainline did not freeze one final candidate")
	}
	wave := state.Validation.Waves[0]
	if len(wave.Targets) != 7 || len(master.run.ValidationInputs) != 7 {
		t.Fatalf("merge QA/gate plus five selected child gates must issue together: %d/%d", len(wave.Targets), len(master.run.ValidationInputs))
	}
	for _, input := range master.run.ValidationInputs {
		if input.Binding.Candidate != wave.Candidate.Identity {
			t.Fatal("mixed candidate validation")
		}
		if input.Handler == "engine.gate" && (input.Gate == nil || input.Gate.BaseSnapshot != state.BaseSnapshot || input.Gate.Candidate != wave.Candidate) {
			t.Fatal("gate lost original FULL range")
		}
	}
	var receipts []protocol.CandidateParameters
	for _, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" {
			continue
		}
		for _, effect := range state.ReconciledEffects {
			if effect.Intent == nil {
				continue
			}
			intent := *effect.Intent
			if intent.ActionID == receipt.ActionID {
				raw, _ := json.Marshal(intent.Adapter.Params)
				var p protocol.CandidateParameters
				_ = json.Unmarshal(raw, &p)
				if p.Stage == "MERGE_FINISH" {
					receipts = append(receipts, p)
				}
			}
		}
	}
	if len(receipts) != 3 {
		t.Fatalf("fixed sequence has %d completion receipts", len(receipts))
	}
	for _, child := range children {
		receipt := readSliceReceipt(t, child)
		if err := master.f.vcs.IsAncestorOrEqual(state.ChildCreation.Source, receipt.Receipt.Snapshot); err != nil {
			t.Fatal("child checkpoint lost its creation source ancestry", err)
		}
		if err := master.f.vcs.IsAncestorOrEqual(receipt.Receipt.Snapshot, wave.Candidate.Identity); err != nil {
			t.Fatal("candidate lacks child history", err)
		}
	}
	body, err := vcs.ExecRunner{}.Run(master.f.root, "git", "show", state.Validation.DevelopmentSnapshot+":.gates/results/"+state.RunID+".merge-cases.md")
	if err != nil || body != strings.TrimSpace(protocol.MergeCasesMarkdown(state.RunID, state.Validation.Sets[coverage.ReviewMerge].Manifest)) {
		t.Fatal("approved complete merge cases missing before development snapshot", err)
	}
	mode, err := master.f.vcs.Entry(wave.Candidate.Identity, "tool")
	if err != nil || !strings.HasPrefix(mode, "100755 ") {
		t.Fatal("integration lost executable mode", err)
	}
	passMainlineTargets(t, master)
	if _, err := master.f.PrepareCandidate("validated mainline"); err != nil {
		t.Fatal(err)
	}
	master.drive()
	if master.run.Status != "COMPLETE" {
		t.Fatalf("mainline did not close after child checkpoint cleanup: %s", master.run.Status)
	}
	for _, child := range children {
		if _, err := os.Stat(filepath.Join(child.f.stateDir, "slice-receipt.json")); !os.IsNotExist(err) {
			t.Fatal("child checkpoint sidecar remains", err)
		}
		for _, path := range []string{filepath.Join(child.f.stateDir, "state.json"), filepath.Join(child.f.root, ".gates", "tmp", child.f.runID, "lifecycle", "active")} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("child checkpoint active state remains", path, err)
			}
		}
	}
	trees, err := master.f.vcs.Worktrees()
	if err != nil || len(trees) != 1 {
		t.Fatal("temporary worktree remains", trees, err)
	}
}

func TestMainlineConflictWorkerHasExactContentAuthority(t *testing.T) {
	master, _ := mainlineChildren(t, "text")
	id, input := master.action("engine.merge.resolve")
	if input.Conflict == nil || len(input.Conflict.Evidence.Conflicts) != 1 || input.Conflict.Evidence.Conflicts[0].Path != "app.go" {
		t.Fatal("conflict did not carry exact native stages")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// integrated command behavior\n"), 0644); err != nil {
		t.Fatal(err)
	}
	master.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Resolution: &protocol.ConflictResolution{Paths: []string{"app.go"}, Summary: "choose the unified command behavior"}})
	r := &mergeInterruption{Runner: vcs.ExecRunner{}, point: "resolution"}
	master.f.vcsRunner, master.f.vcs = r, vcs.New(master.f.root, r)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("resolution staging response loss was not reached")
			}
		}()
		_, _ = master.f.Drive()
	}()
	master.reopen()
	master.f.vcsRunner, master.f.vcs = r, vcs.New(master.f.root, r)
	master.drive()
	if driverState(t, master).Validation.Candidate == nil {
		t.Fatal("resolved prefix did not freeze")
	}
	passMainlineTargets(t, master)
	if _, err := master.f.PrepareCandidate("resolved validated mainline"); err != nil {
		t.Fatal(err)
	}
	master.drive()
	if master.run.Status != "COMPLETE" {
		t.Fatal("conflict trajectory failed terminal cleanup", master.run.Status)
	}
}

func TestConflictContextRejectsNonConflictEdits(t *testing.T) {
	original := []byte("keep before\n<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> incoming\nkeep after\n")
	for name, body := range map[string]string{"valid": "keep before\nchosen\nkeep after\n", "outside": "edited before\nchosen\nkeep after\n", "markers": string(original)} {
		t.Run(name, func(t *testing.T) {
			err := preserveConflictContext(original, []byte(body))
			if (err == nil) != (name == "valid") {
				t.Fatal(fmt.Sprintf("wrong content boundary: %v", err))
			}
		})
	}
}

func mainlineResult(input protocol.ValidationInput) protocol.ValidationResult {
	result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
	if input.Handler == "engine.qa.execution" {
		report := executionFor(input)
		result.Execution, result.Tool, result.Environment = &report, "command", "isolated candidate"
	}
	return result
}

func TestMainlineCapacityRefillAndRepairUseOneFinalCandidate(t *testing.T) {
	master, children := mainlineReady(t, "", false)
	engine, err := protocol.New(master.f.store, protocol.Config{NativeRoot: master.f.root, Definition: master.f.definition, Registry: definition.Registry(), Capacity: 3, CostSource: CostSourceBridge(master.f.root, master.f.runID), ClaimDispatch: dispatchClaimBridge(master.f.root, master.f.runID, master.f.admission)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	master.f.engine, master.f.capacity = engine, 3
	master.drive()
	original := master.run.Validation.Waves[0].Candidate
	if len(master.run.ValidationInputs) != 3 {
		t.Fatal("capacity did not issue min(N,C)", len(master.run.ValidationInputs))
	}
	completed := 0
	var failedCase string
	for len(master.run.ValidationInputs) > 0 {
		var id string
		var input protocol.ValidationInput
		for action, in := range master.run.ValidationInputs {
			id, input = action, in
			break
		}
		result := mainlineResult(input)
		target := master.run.Validation.Waves[0].Targets[input.Binding.Task.String()]
		if target.SliceID == "" && input.Handler == "engine.gate" {
			result.Status = "FAIL"
			result.Findings = []review.Finding{{Severity: "P2", Message: "Combined command output differs from the confirmed oracle"}}
		}
		if input.Handler == "engine.qa.execution" {
			result.Status = "FAIL"
			result.Execution.Records[0].Result = coverage.ExecutedFail
			failedCase = result.Execution.Records[0].CaseID
		}
		master.business(id, result)
		completed++
		master.drive()
		if completed < 7 {
			if master.run.Validation.Waves[0].Status != "COLLECTING" || master.run.Validation.Waves[0].Repair != nil {
				t.Fatal("partial failure bypassed collecting")
			}
			want := min(3, 7-completed)
			if len(master.run.ValidationInputs) != want {
				t.Fatalf("refill issued %d, expected %d", len(master.run.ValidationInputs), want)
			}
		}
	}
	if completed != 7 {
		t.Fatal("slice identity collapsed gate obligations", completed)
	}
	wave := master.run.Validation.Waves[0]
	var gateFinding string
	for id, finding := range master.run.Validation.Findings {
		if finding.Raw != nil {
			gateFinding = id
		}
	}
	resolution := protocol.FindingResolution{FindingID: gateFinding, Decision: "NEW", Requirement: "AC-001", NormalPath: "command accepts valid input", Module: "command", RootCause: "combined output formatting", Consequence: "specified output differs", Severity: "P2", SeverityRationale: "limited output mismatch", Verification: review.FindingVerification{Scope: "confirmed command", Severity: "limited output mismatch", Causality: "combined output formatting", Reproduction: "invoke command with valid input", Classification: "implementation"}, Reason: "observed the exact output mismatch"}
	master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	for id, finding := range master.run.Validation.Findings {
		if finding.ExecutionFailure == nil {
			continue
		}
		item := resolution
		item.FindingID, item.Decision, item.ProblemID = id, "SAME_AS", master.run.Validation.Findings[gateFinding].History[0].ProblemID
		item.Severity, item.SeverityRationale, item.Verification.Severity = "", "", ""
		master.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{item}}}})
	}
	master.drive()
	repair, input := master.action("engine.repair")
	if input.Repair == nil || len(input.Repair.Problems) != 1 || len(input.Repair.ExecutionFailures) != 1 {
		t.Fatal("simultaneous merge failures did not join one repair")
	}
	if input.Binding.Candidate != original.Identity {
		t.Fatal("repair lost its actual source candidate")
	}
	path := "integration_test.go"
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, path), []byte("package main\nimport \"testing\"\nfunc TestCombinedCommand(t *testing.T) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	master.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{path}, Summary: "verified integrated command output", Build: []string{"candidate builds"}, Tests: []string{"combined command succeeds"}, AffectedCases: []string{failedCase}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewMerge, CaseID: failedCase, Result: coverage.ExecutedPass, Procedure: "invoke the combined command", Observation: "specified output returned", Comparison: "exact match to approved oracle"}}}})
	master.drive()
	state := driverState(t, master)
	current := state.Validation.Waves[len(state.Validation.Waves)-1]
	if current.Candidate == original || current.Scope != coverage.ScopeFull || state.Validation.Sets[coverage.ReviewMerge].Status != "FROZEN" {
		t.Fatal("repair did not produce a current FULL candidate with frozen cases")
	}
	if len(master.run.ValidationInputs) != 2 || len(state.AffectedChildSet()) != 0 {
		t.Fatal("unchanged FULL child views were reexecuted", state.AffectedChildSet(), len(master.run.ValidationInputs))
	}
	for _, target := range current.Targets {
		if target.SliceID != "" && (target.Status != "PASS" || target.Reuse == nil || target.Reuse.SourceCandidate != original.Identity) {
			t.Fatal("prior mainline PASS lacked exact native reuse proof")
		}
	}
	// Independent results race through the public store admission and replay
	// their original event identities without changing the native candidate.
	var events []protocol.Event
	for id, in := range master.run.ValidationInputs {
		master.receipt(id)
		result := mainlineResult(in)
		events = append(events, protocol.Event{ID: master.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: master.host(), Outcome: protocol.OutcomePass, PayloadDigest: id, Validation: &result}})
	}
	errors := make(chan error, len(events))
	for _, event := range events {
		go func(event protocol.Event) { _, _, err := master.f.Submit(event); errors <- err }(event)
	}
	for range events {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range events {
		master.submit(event)
	}
	master.drive()
	if master.run.Validation.Waves[len(master.run.Validation.Waves)-1].Status != "COMPLETE" {
		t.Fatal("parallel results did not join")
	}
	for _, child := range children {
		if readSliceReceipt(t, child).Receipt.Snapshot == current.Candidate.Identity {
			t.Fatal("mainline repair changed child checkpoint")
		}
	}
	if _, err := master.f.PrepareCandidate("validated repaired mainline"); err != nil {
		t.Fatal(err)
	}
	master.drive()
	if master.run.Status != "COMPLETE" {
		t.Fatal("repaired mainline did not complete", master.run.Status)
	}
	assertNoMainlineResources(t, master)
}

func TestMainlineReusesOnlyEqualOriginalFullGateInputs(t *testing.T) {
	master, children := mainlineReady(t, "", true)
	master.drive()
	state := driverState(t, master)
	if state.BaseSnapshot == state.ChildCreation.Source {
		t.Fatal("fixture omitted the formal base ancestor")
	}
	if len(master.run.ValidationInputs) != 2 || len(state.AffectedChildSet()) != 0 {
		t.Fatal("identical complete FULL inputs did not reuse", state.AffectedChildSet(), len(master.run.ValidationInputs))
	}
	for _, target := range state.Validation.Waves[0].Targets {
		if target.SliceID != "" && (target.Reuse == nil || target.Reuse.ChildReceiptDigest != state.SliceReceipts[state.SliceChildren[target.SliceID]].Digest) {
			t.Fatal("reuse lacks frozen child identity")
		}
	}
	for _, child := range children {
		receipt := readSliceReceipt(t, child)
		if receipt.Receipt.Validation.Candidate.Identity == state.ChildCreation.Source {
			t.Fatal("fixture omitted child content")
		}
	}
	passMainlineTargets(t, master)
}

func assertNoMainlineResources(t *testing.T, master *validationDriver) {
	t.Helper()
	trees, err := master.f.vcs.Worktrees()
	if err != nil || len(trees) != 1 {
		t.Fatal("registered temporary worktrees remain", trees, err)
	}
	refs, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "for-each-ref", "--format=%(refname)", "refs/formal-gates/")
	if err != nil || refs != "" {
		t.Fatal("registered temporary refs remain", refs, err)
	}
}

func TestMainlineNativeChoicesPreserveNonConflictContent(t *testing.T) {
	for _, kind := range []string{"binary", "delete"} {
		t.Run(kind, func(t *testing.T) {
			master, _ := mainlineChildren(t, kind)
			id, input := master.action("engine.merge.resolve")
			if input.Conflict == nil || len(input.Conflict.Evidence.Conflicts) != 1 {
				t.Fatal("native conflict inventory is incomplete")
			}
			conflict := input.Conflict.Evidence.Conflicts[0]
			if conflict.Text || len(conflict.Entries) < 2 {
				t.Fatal("nontext conflict was converted to a text guess")
			}
			if kind == "delete" {
				if err := os.Remove(filepath.Join(input.Workspace.Path, conflict.Path)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(input.Workspace.Path, conflict.Path), []byte{0, 2}, 0644); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(input.Workspace.Path, "parser.go")
			if err := os.WriteFile(outside, []byte("package main\n// unauthorized content\n"), 0644); err != nil {
				t.Fatal(err)
			}
			native := master.f.vcs.At(input.Workspace.Path)
			if err := validateConflictEdits(native, input.Workspace.Path, input.Conflict.Evidence); err == nil {
				t.Fatal("worker edit outside conflict authority was admitted")
			}
			if err := os.WriteFile(outside, []byte("package main\n"), 0644); err != nil {
				t.Fatal(err)
			}
			master.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Resolution: &protocol.ConflictResolution{Paths: []string{conflict.Path}, Summary: "choose the documented native content outcome"}})
			master.reopen()
			master.drive()
			candidate := master.run.Validation.Candidate.Identity
			if kind == "delete" {
				entry, err := master.f.vcs.Entry(candidate, conflict.Path)
				if err != nil || entry != "" {
					t.Fatal("delete choice was not preserved", entry, err)
				}
			} else {
				body, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "show", candidate+":"+conflict.Path)
				if err != nil || body != string([]byte{0, 2}) {
					t.Fatal("binary choice changed bytes", err)
				}
			}
			passMainlineTargets(t, master)
			if _, err := master.f.PrepareCandidate("native choices validated"); err != nil {
				t.Fatal(err)
			}
			master.drive()
			if master.run.Status != "COMPLETE" {
				t.Fatal("native choice delivery did not complete", master.run.Status)
			}
			assertNoMainlineResources(t, master)
		})
	}
}

func TestMainlineAbortReconcilesPrefixAndStopsConflictWorker(t *testing.T) {
	for _, point := range []string{"prefix", "later-commit", "worker"} {
		t.Run(point, func(t *testing.T) {
			kind := ""
			if point == "worker" {
				kind = "text"
			}
			master, _ := mainlineReady(t, kind, false)
			var workerWorkspace string
			if point != "worker" {
				nativePoint := "merge"
				if point == "later-commit" {
					nativePoint = point
				}
				r := &mergeInterruption{Runner: vcs.ExecRunner{}, point: nativePoint}
				master.f.vcsRunner, master.f.vcs = r, vcs.New(master.f.root, r)
				func() {
					defer func() {
						if recover() == nil {
							t.Fatal("merge response loss not reached")
						}
					}()
					_, _ = master.f.Drive()
				}()
				request := holdRequirement(t, master)
				master.drive()
				expectedMerges := 1
				if point == "later-commit" {
					expectedMerges = 2
				}
				if r.merges != expectedMerges || master.run.Validation.Candidate != nil || master.run.Validation.DevelopmentSnapshot != "" {
					t.Fatal("requirement barrier admitted another native prefix")
				}
				releaseRequirement(t, master, request)
			} else {
				master.drive()
				id, in := master.action("engine.merge.resolve")
				master.receipt(id)
				workerWorkspace = in.Workspace.Path
				if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app.go"), []byte("package main\n// resolution in flight\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			abortRun(t, master)
			master.drive()
			if point == "worker" {
				if master.run.Status != "ABORTING" {
					t.Fatal("active worker bypassed termination")
				}
				for id, intent := range master.run.AdapterIntents {
					if intent.Terminate == nil {
						continue
					}
					if _, err := os.Stat(workerWorkspace); err != nil {
						t.Fatal(err)
					}
					for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
						event, err := protocol.NewLifecycleEventEvent(master.eventID(), master.host(), intent.Terminate.Identity, edge)
						if err != nil {
							t.Fatal(err)
						}
						master.submit(event)
					}
					event, err := protocol.NewAgentHostActionReceiptEvent(master.eventID(), id, intent.Operation, master.host(), id, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
					if err != nil {
						t.Fatal(err)
					}
					master.submit(event)
				}
				master.drive()
			}
			if master.run.Status != "ABORTED" || master.run.Termination.Validation.Candidate != nil || len(master.run.Termination.PendingHostActions) != 0 {
				t.Fatal("abort left a candidate or unresolved native debt", master.run.Status)
			}
			assertNoMainlineResources(t, master)
		})
	}
}

func TestMainlineReopensDeliveryAndPartialCleanup(t *testing.T) {
	for _, point := range []string{"promotion", "cleanup"} {
		t.Run(point, func(t *testing.T) {
			master, _ := mainlineChildren(t, "")
			passMainlineTargets(t, master)
			r := &mergeInterruption{Runner: vcs.ExecRunner{}, point: point}
			master.f.vcsRunner, master.f.vcs = r, vcs.New(master.f.root, r)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("delivery interruption not reached")
					}
				}()
				if _, err := master.f.PrepareCandidate("validated integration"); err != nil {
					t.Fatal(err)
				}
				_, _ = master.f.Drive()
			}()
			master.reopen()
			master.f.vcsRunner, master.f.vcs = r, vcs.New(master.f.root, r)
			master.drive()
			if master.run.Status != "COMPLETE" {
				t.Fatal("delivery prefix did not complete", master.run.Status)
			}
			assertNoMainlineResources(t, master)
			before, err := os.ReadFile(filepath.Join(master.f.stateDir, "terminal-summary.json"))
			if err != nil {
				t.Fatal(err)
			}
			master.reopen()
			master.drive()
			after, err := os.ReadFile(filepath.Join(master.f.stateDir, "terminal-summary.json"))
			if err != nil || string(after) != string(before) {
				t.Fatal("terminal replay changed evidence", err)
			}
		})
	}
}
