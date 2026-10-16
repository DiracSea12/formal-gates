package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func qaOperationState(t *testing.T) (*State, *compiler.CompiledDefinition, *ValidationTarget) {
	t.Helper()
	cd := compiledCanonicalWorkflow(t)
	binding := coverage.SourceBinding{RunID: "run", RequirementRevision: "req", SolutionRevision: "req"}
	manifestBinding := coverage.ManifestBinding{SourceBinding: binding, ReviewKind: coverage.ReviewBlackbox, RouteScope: "route", TopologyScope: "topology"}
	manifest := coverage.AcceptanceManifest{Binding: manifestBinding, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point", ObservableBehavior: "reject invalid input", Oracle: "exit 7"}, {PointID: "other", ObservableBehavior: "same rejection path", Oracle: "exit 7"}}, SharedSteps: []coverage.SharedStep{{ID: "setup", Procedure: "prepare isolated case", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf ready"}}}}, Cases: []coverage.AcceptanceCase{{CaseID: "case", Mode: coverage.CaseBlackbox, Scenario: "FAILURE", PublicEntry: "./app", Preconditions: "ready", Steps: "invalid input", Oracle: "exit 7", SharedSteps: []string{"setup"}, Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"./app", ""}, Stdin: "input"}}}, {CaseID: "complement", Mode: coverage.CaseBlackbox, Scenario: "FAILURE", PublicEntry: "./app other", Preconditions: "ready", Steps: "other input", Oracle: "rejected", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "./app other"}}}}, Edges: []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "source", PointID: "point", CaseID: "case"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "source", PointID: "other", CaseID: "case"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "source", PointID: "point", CaseID: "complement"}}}
	review := coverage.ReviewResult{Binding: manifestBinding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: "source", Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: "point", Status: coverage.StatusPass}, {ID: "other", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "case", Status: coverage.StatusPass}, {ID: "complement", Status: coverage.StatusPass}}, SharedStepDecisions: []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass}}, SetStatus: coverage.StatusPass}
	candidate := coverage.ValidationCandidate{Identity: "frozen-candidate", Digest: "frozen-tree"}
	target := &ValidationTarget{Kind: "QA", Mode: coverage.ReviewBlackbox, Status: "PENDING", RequiredCases: []string{"case"}, Task: pipelineTask(cd, "engine.qa.execution.v4", "wave:BLACKBOX")}
	s := &State{RunID: "run", NativeRoot: t.TempDir(), RouteConfirmed: true, SelectedGates: []string{"blackbox"}, RequirementRevisions: []ConfirmedRevision{{Revision: "req", Source: RequirementSourceIntake, Classification: RequirementClassificationIntake, StateRevision: 1}}, Validation: &Validation{RequirementRevision: "req", Generation: 0, Candidate: &candidate, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {Version: "version", Manifest: manifest}}}, NativeResources: map[string]NativeResource{}, PendingHostActions: map[string]HostActionIntent{}, HostActionReceipts: map[string]HostActionReceipt{}, DispatchBindings: map[string]WorkBinding{}}
	wave := ValidationWave{ID: "wave", Candidate: candidate, RequirementRevision: "req", Generation: 0, RouteDigest: s.validationRouteDigest(), Scope: coverage.ScopeAffected, Status: "COLLECTING", Coverage: coverage.CoverageContract{RequiredSources: coverage.RequiredSources{Binding: binding, Sources: []coverage.RequiredSource{{SourceID: "source", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}}, SelectedKinds: []coverage.ReviewKind{coverage.ReviewBlackbox}, Candidate: &candidate, Manifests: []coverage.AcceptanceManifest{manifest}}, Reviews: []coverage.ReviewResult{review}, Targets: map[string]*ValidationTarget{target.Task.String(): target}}
	s.Validation.Waves = []ValidationWave{wave}
	resource := s.taskResource(target.Task, cd)
	resource.Status, resource.Identity = "READY", candidate.Identity
	s.NativeResources[resource.ID] = resource
	if _, _, err := s.executionContract(target.Mode, s.currentValidationWave()); err != nil {
		t.Fatal(err)
	}
	return s, cd, target
}

func addQAOperationReceipt(t *testing.T, s *State, p CheckParameters, stdout string, exit int) HostActionIntent {
	t.Helper()
	params, _ := normalizeHostActionParams(p)
	id := "qa-operation:" + digestOfCanonical(p)
	intent := s.latestNativeIntent("check:" + digestOfCanonical(p))
	if intent.ActionID != "" {
		id = intent.ActionID
	} else {
		intent = HostActionIntent{ActionID: id, PayloadDigest: digestOfCanonical(p), Adapter: &AdapterHostAction{Operation: "op.check", Params: params}, Correlation: "check:" + digestOfCanonical(p)}
	}
	proof := func(value string) CheckStreamProof {
		sum := sha256.Sum256([]byte(value))
		return CheckStreamProof{SHA256: hex.EncodeToString(sum[:]), Size: len(value)}
	}
	evidence := CheckEvidence{ParametersDigest: intent.PayloadDigest, SourceIdentity: p.SourceIdentity, SourceDigest: p.SourceDigest, WorkspaceID: p.WorkspaceID, RequestDigest: "request:" + id, Directory: "evidence:" + id, ExitCode: exit, Stdout: proof(stdout), Stderr: proof(""), StdoutText: stdout, Tool: "actual-tool", Environment: "actual-environment"}
	argv := p.QA.Operation.Argv
	if p.QA.Operation.Kind == coverage.OperationShell {
		argv = []string{"/bin/sh", "-c", p.QA.Operation.Command}
	}
	invocation, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Invocation = string(invocation)
	evidence.WorkingDirectory = filepath.Join(p.QA.WorkspaceRoot, p.QA.Directory)
	values, _ := normalizeHostActionParams(evidence)
	s.PendingHostActions[id] = intent
	s.HostActionReceipts[id] = HostActionReceipt{ActionID: id, AdapterOperation: "op.check", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
	return intent
}

func TestCommonPreparationWaitsForCompleteConsumerCase(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	s.currentValidationWave().Scope = coverage.ScopeFull
	manifest := &s.currentValidationWave().Coverage.Manifests[0]
	manifest.SharedSteps[0].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf re > prepared"}, {Kind: coverage.OperationShell, Command: "printf ady >> prepared"}}
	manifest.Cases[0].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "cat prepared"}, {Kind: coverage.OperationShell, Command: "cat prepared; exit 7"}}
	manifest.Cases[1].SharedSteps = []string{"setup"}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 7 {
		t.Fatal("approved shared consumer plans", err)
	}
	e := &Engine{cfg: Config{Definition: cd}}
	first, err := e.prepareAdapterIntent(s, 1, "op.check", plans[0], "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[first.ActionID] = first
	second, err := e.prepareAdapterIntent(s, 2, "op.check", plans[4], "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[second.ActionID] = second
	assertBlocked := func(state *State) {
		t.Helper()
		if state.qaCheckExecutionReady(second, cd) || state.NativeActionWritable(second, cd) || state.ExecutableHostActions(cd)[second.ActionID].ActionID != "" {
			t.Fatal("other recipe started before the complete consumer case ended")
		}
	}
	assertBlocked(s)
	for _, plan := range plans[:3] {
		addQAOperationReceipt(t, s, plan, "ready", 0)
		assertBlocked(s)
	}
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	assertBlocked(&restored)
	completed := addQAOperationReceipt(t, &restored, plans[3], "ready", 7)
	if !restored.NativeActionWritable(second, cd) || restored.ExecutableHostActions(cd)[second.ActionID].ActionID != second.ActionID {
		t.Fatal("complete negative-exit consumer retained the common environment")
	}
	restored.ReconciledEffects = map[string]ReconciledEffect{completed.ActionID: {Intent: &completed, Status: "FULFILLED"}}
	delete(restored.PendingHostActions, completed.ActionID)
	if !restored.qaCheckExecutionReady(second, cd) {
		t.Fatal("reconciled consumer receipt did not release the common environment")
	}
	for _, plan := range plans[:4] {
		if _, done := restored.qaCheckReceipt(plan); !done {
			t.Fatal("released original operation lost its exact receipt")
		}
	}
	t.Run("repair", func(t *testing.T) {
		s, cd, target := qaOperationState(t)
		wave := s.currentValidationWave()
		wave.Scope = coverage.ScopeFull
		wave.Coverage.Manifests[0].Cases[1].SharedSteps = []string{"setup"}
		wave.Repair = &ValidationRepair{Task: pipelineTask(cd, "engine.repair.v7", "original-repair")}
		originalBinding := WorkBinding{Task: wave.Repair.Task, WaveID: wave.ID, WorkspaceID: "repair-edit"}
		original := WorkerResult{ActionID: "repair-edit", Outcome: OutcomePass, Validation: &ValidationResult{Binding: originalBinding, Status: "PASS", Development: &DevelopmentEvidence{Paths: []string{"app"}, AffectedCases: []string{"case", "complement"}}}}
		s.Results = map[string]WorkerResult{original.ActionID: original}
		s.DispatchBindings[original.ActionID] = originalBinding
		params, _ := normalizeHostActionParams(CandidateParameters{Stage: "CHECK_SOURCE", WorkspaceID: originalBinding.WorkspaceID, SourceTasks: []runtime.TaskKey{originalBinding.Task}, Paths: original.Validation.Development.Paths})
		freeze := HostActionIntent{ActionID: "repair-source", PayloadDigest: digestOfCanonical(params), Adapter: &AdapterHostAction{Operation: "op.candidate", Params: params}}
		proof := CandidateEvidence{ParametersDigest: freeze.PayloadDigest, WorkspaceID: originalBinding.WorkspaceID, Identity: "repaired-candidate", Digest: "repaired-tree", Paths: original.Validation.Development.Paths}
		values, _ := normalizeHostActionParams(proof)
		s.PendingHostActions[freeze.ActionID] = freeze
		s.HostActionReceipts[freeze.ActionID] = HostActionReceipt{ActionID: freeze.ActionID, AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
		binding := s.validationBinding(target.Task, cd)
		binding.Task = pipelineTask(cd, "engine.repair.verify.v2", "approved-repair-comparison")
		binding.Candidate, binding.VerificationSource, binding.PureP2 = proof.Identity, original.ActionID, true
		resource := s.NativeResources[binding.WorkspaceID]
		resource.Source, resource.Identity = proof.Identity, proof.Identity
		s.NativeResources[resource.ID] = resource
		s.DispatchBindings["repair-comparison"] = binding
		s.RequirementRevisions[0].WorkDecisions = []RequirementWorkDecision{{Work: RequirementWork{Task: binding.Task}, Disposition: RequirementWorkCarry}}
		context := repairComparison{binding: binding, resource: resource, source: proof, wave: wave, originals: []WorkerResult{original}, paths: original.Validation.Development.Paths, affected: original.Validation.Development.AffectedCases}
		plans, err := s.repairComparisonPlans(context, cd)
		if err != nil || len(plans) != 4 {
			t.Fatal("repair approved shared consumer plans", err)
		}
		addQAOperationReceipt(t, s, plans[0], "ready", 0)
		if recovered, found := s.repairComparisonContext(binding.Task, cd); !found || recovered.binding != binding || recovered.source.Identity != proof.Identity {
			t.Fatal("original repair receipt did not reconstruct its precise comparison context")
		}
		e := &Engine{cfg: Config{Definition: cd}}
		if _, err := e.prepareAdapterIntent(s, 1, "op.check", plans[2], ""); err == nil {
			t.Fatal("repair signing bypassed its original consumer receipt")
		}
		consumer, err := e.prepareAdapterIntent(s, 1, "op.check", plans[1], "")
		if err != nil || !s.qaCheckExecutionReady(consumer, cd) {
			t.Fatal("original repair consumer was not executable", err)
		}
		s.PendingHostActions[consumer.ActionID] = consumer
		addQAOperationReceipt(t, s, plans[1], "ready", 7)
		second, err := e.prepareAdapterIntent(s, 2, "op.check", plans[2], "")
		if err != nil {
			t.Fatal("ended repair consumer did not release the signing frontier", err)
		}
		s.PendingHostActions[second.ActionID] = second
		if !s.qaCheckExecutionReady(second, cd) || !s.NativeActionWritable(second, cd) || s.ExecutableHostActions(cd)[second.ActionID].ActionID == "" {
			t.Fatal("repair negative-exit consumer did not release its original common environment")
		}
	})
}

func TestPreparationDependenciesKeepIndependentCasesExecutable(t *testing.T) {
	t.Parallel()
	for _, relation := range []string{"no-preparation", "independent", "different-preparation", "same-content", "private-prefix"} {
		t.Run(relation, func(t *testing.T) {
			s, cd, target := qaOperationState(t)
			s.currentValidationWave().Scope = coverage.ScopeFull
			manifest := &s.currentValidationWave().Coverage.Manifests[0]
			manifest.Cases[1].SharedSteps = []string{"setup"}
			switch relation {
			case "no-preparation":
				manifest.Cases[1].SharedSteps = nil
			case "independent":
				manifest.SharedSteps[0].Independent = true
			case "different-preparation", "same-content", "private-prefix":
				other := manifest.SharedSteps[0]
				other.ID = "other-preparation"
				if relation == "different-preparation" {
					other.Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf other"}}
				}
				if relation == "private-prefix" {
					other.Independent = true
				}
				manifest.SharedSteps = append(manifest.SharedSteps, other)
				manifest.Cases[1].SharedSteps = []string{other.ID}
				if relation == "private-prefix" {
					manifest.Cases[1].SharedSteps = append(manifest.Cases[1].SharedSteps, "setup")
				}
				s.currentValidationWave().Reviews[0].SharedStepDecisions = append(s.currentValidationWave().Reviews[0].SharedStepDecisions, coverage.ReviewDecision{ID: other.ID, Status: coverage.StatusPass})
			}
			plans, err := s.qaCheckPlans(target, cd)
			if err != nil {
				t.Fatal(err)
			}
			e := &Engine{cfg: Config{Definition: cd}}
			var intents []HostActionIntent
			for _, plan := range []CheckParameters{plans[0], plans[2]} {
				intent, err := e.prepareAdapterIntent(s, uint64(len(intents)+1), "op.check", plan, "")
				if err != nil {
					t.Fatal(err)
				}
				intents = append(intents, intent)
				s.PendingHostActions[intent.ActionID] = intent
			}
			want := relation != "same-content"
			if s.qaCheckExecutionReady(intents[1], cd) != want || s.NativeActionWritable(intents[1], cd) != want || (s.ExecutableHostActions(cd)[intents[1].ActionID].ActionID != "") != want {
				t.Fatal("approved preparation relation did not determine launch dependency")
			}
			if relation == "private-prefix" {
				addQAOperationReceipt(t, s, plans[2], "private", 0)
				common, err := e.prepareAdapterIntent(s, 3, "op.check", plans[3], "")
				if err != nil || s.qaCheckExecutionReady(common, cd) {
					t.Fatal("private prefix acquired the later common environment", err)
				}
			}
		})
	}
}

func TestCommonPreparationWaitsForUnknownAndReleasesExecutionError(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	s.currentValidationWave().Scope = coverage.ScopeFull
	s.currentValidationWave().Coverage.Manifests[0].Cases[1].SharedSteps = []string{"setup"}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{cfg: Config{Definition: cd}}
	original, err := e.prepareAdapterIntent(s, 1, "op.check", plans[0], "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[original.ActionID] = original
	second, err := e.prepareAdapterIntent(s, 2, "op.check", plans[2], "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[second.ActionID] = second
	first := addQAOperationReceipt(t, s, plans[0], "", 0)
	receipt := s.HostActionReceipts[first.ActionID]
	receipt.Status = HostActionStatusUnknown
	s.HostActionReceipts[first.ActionID] = receipt
	if s.qaCheckExecutionReady(second, cd) {
		t.Fatal("unknown original execution released its common environment")
	}
	receipt.Status = HostActionStatusExecuted
	var evidence CheckEvidence
	if err := decodeAdapterValues(receipt, &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Error = "original process ended without complete execution"
	receipt.AdapterEvidence.Values, _ = normalizeHostActionParams(evidence)
	s.HostActionReceipts[first.ActionID] = receipt
	if !s.qaCheckExecutionReady(second, cd) || !s.NativeActionWritable(second, cd) {
		t.Fatal("ended execution error blocked the other original case")
	}
	if _, err := e.prepareAdapterIntent(s, 3, "op.check", plans[1], ""); err == nil {
		t.Fatal("failed original case continued its consumer")
	}
}

func TestQAOperationsPreservePartitionOrderAndReceipts(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	if s.qaComparisonReady(target, cd) {
		t.Fatal("comparison ready before any approved operation ran")
	}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 2 || plans[0].QA.Source != "shared:setup" || plans[1].QA.Source != "case:case" || plans[1].QA.Operation.Argv[1] != "" || plans[1].QA.Operation.Stdin != "input" {
		t.Fatalf("case deduplication/shared prefix/partition: %#v %v", plans, err)
	}
	for ordinal, expected := range plans {
		actual, ready := s.nextQACheckParameters(target.Task, cd)
		if !ready || !reflect.DeepEqual(actual, expected) || actual.QA.Ordinal != ordinal {
			t.Fatalf("approved sequential operation: %+v", actual)
		}
		exit := 0
		if ordinal == 1 {
			exit = 7
		}
		intent := addQAOperationReceipt(t, s, actual, "operation-output:"+actual.QA.Source, exit)
		if !s.checkInputsCurrent(intent) {
			t.Fatal("settled approved receipt became noncurrent")
		}
		if ordinal == 0 && s.qaComparisonReady(target, cd) {
			t.Fatal("partial operation prefix completed a case")
		}
	}
	if _, next := s.nextQACheckParameters(target.Task, cd); next || !s.qaComparisonReady(target, cd) {
		t.Fatal("complete nonzero negative case was rerun or withheld from oracle comparison")
	}
	facts := s.qaExecutionFacts(target, cd)
	if len(facts) != 1 || len(facts[0].Operations) != 2 || facts[0].Operations[1].Evidence.ExitCode != 7 {
		t.Fatalf("original receipt projection: %+v", facts)
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if _, next := restored.nextQACheckParameters(target.Task, cd); next || !reflect.DeepEqual(restored.qaExecutionFacts(restored.currentValidationWave().Targets[target.Task.String()], cd), facts) {
		t.Fatal("recovery repeated an executed prefix or changed original facts")
	}
	target.Execution = &coverage.ExecutionReport{Records: []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "source", PointID: "point", CaseID: "case", Result: coverage.ExecutedPass}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "source", PointID: "other", CaseID: "case", Result: coverage.ExecutedPass}}}
	if _, next := s.nextQACheckParameters(target.Task, cd); next || s.qaComparisonReady(target, cd) {
		t.Fatal("completed case was resubmitted or reexecuted")
	}
	target.Execution = nil
	s.currentValidationWave().Coverage.Manifests[0].Cases[0].Operations[0].Stdin = "changed"
	if len(s.qaExecutionFacts(target, cd)) != 0 {
		t.Fatal("changed recipe reused an original receipt")
	}
}

func TestSharedPreparationReceiptsRetainOriginalExecutionFacts(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	s.currentValidationWave().Scope = coverage.ScopeFull
	s.currentValidationWave().Coverage.Manifests[0].Cases[1].SharedSteps = []string{"setup"}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 4 {
		t.Fatal("approved shared preparation plans", err)
	}
	original := addQAOperationReceipt(t, s, plans[0], "ready", 0)
	originalParameters, err := checkParameters(original)
	if err != nil {
		t.Fatal(err)
	}
	recorded := s.HostActionReceipts[original.ActionID]
	var actual CheckEvidence
	if err := decodeAdapterValues(recorded, &actual); err != nil {
		t.Fatal(err)
	}
	actual.PreparationInput = "original-input"
	actual.PreparationArtifacts = map[string]string{"prepared": "original-artifact"}
	recorded.AdapterEvidence.Values, _ = normalizeHostActionParams(actual)
	s.HostActionReceipts[original.ActionID] = recorded
	shared := addQAOperationReceipt(t, s, plans[2], "ready", 0)
	reused := actual
	reused.PreparationSourceAction = original.ActionID
	receipt := s.HostActionReceipts[shared.ActionID]
	receipt.AdapterEvidence.Values, _ = normalizeHostActionParams(reused)
	s.HostActionReceipts[shared.ActionID] = receipt
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	for _, state := range []*State{s, &restored} {
		before, _ := json.Marshal(state.HostActionReceipts)
		e := &Engine{cfg: Config{Definition: cd}}
		if err := e.applyNativeReceipt(state, shared, receipt); err != nil {
			t.Fatal("shared receipt lost its original execution", err)
		}
		fact, done := state.qaCheckReceipt(plans[2])
		if !done || fact.ActionID != original.ActionID || !reflect.DeepEqual(fact.Parameters, originalParameters) || !reflect.DeepEqual(fact.Evidence, reused) {
			t.Fatalf("shared receipt changed original parameters or evidence: %+v", fact)
		}
		changed := reused
		changed.RequestDigest = "another-execution"
		altered := receipt
		values, _ := normalizeHostActionParams(changed)
		altered.AdapterEvidence = &AdapterEvidence{Values: values}
		if err := e.applyNativeReceipt(state, shared, altered); err == nil {
			t.Fatal("shared receipt admitted changed original execution facts")
		}
		after, _ := json.Marshal(state.HostActionReceipts)
		if string(after) != string(before) {
			t.Fatal("receipt validation or lookup mutated recorded execution evidence")
		}
	}
}

func TestQACoreRejectsTextOnlyAndChangedNativeFacts(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	s.currentValidationWave().Scope = coverage.ScopeFull
	// The second case belongs to the same approved full obligation.
	for {
		p, ready := s.nextQACheckParameters(target.Task, cd)
		if !ready {
			break
		}
		addQAOperationReceipt(t, s, p, "actual:"+p.QA.Source, 0)
	}
	facts := s.qaExecutionFacts(target, cd)
	_, whitelist, err := s.executionContract(target.Mode, s.currentValidationWave())
	if err != nil {
		t.Fatal(err)
	}
	report := coverage.ExecutionReport{Binding: facts[0].Operations[0].Parameters.QA.Execution}
	for _, fact := range facts {
		procedure, observation, err := fact.Render()
		if err != nil {
			t.Fatal(err)
		}
		for _, edge := range whitelist.Entries {
			if edge.CaseID == fact.CaseID {
				report.Records = append(report.Records, coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: fact.CaseID, Procedure: procedure, Observation: observation, Comparison: "actual output matched the approved oracle", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted})
			}
		}
		report.Binding.ActualCaseIDs = append(report.Binding.ActualCaseIDs, fact.CaseID)
	}
	tool, environment := ExecutionIdentities(facts)
	changed := report
	changed.Records = append([]coverage.ExecutionRecord(nil), report.Records...)
	changed.Records[0].Observation = "claimed PASS"
	if s.acceptExecution(target, changed, tool, environment, false, cd) == nil {
		t.Fatal("model text replaced original native facts")
	}
	if err := s.acceptExecution(target, report, tool, environment, false, cd); err != nil {
		t.Fatal(err)
	}
	if len(target.Execution.Records) != 3 {
		t.Fatal("once-run case lost a source/point edge")
	}
	if s.acceptExecution(target, report, tool, environment, false, cd) == nil {
		t.Fatal("completed evidence was resubmitted")
	}
}

func TestQACheckPlansNeverReadGoSource(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	manifest := &s.currentValidationWave().Coverage.Manifests[0]
	manifest.Cases[0].Operations = nil
	manifest.Cases[0].Mode = coverage.CaseWhitebox
	// Use the whitebox contract, with an absent frozen-file path. Core planning
	// must carry the approved ref; the native adapter owns file parsing.
	manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	manifest.Cases[0].PublicEntry, manifest.Cases[0].Preconditions, manifest.Cases[0].Steps = "", "", ""
	manifest.Cases[0].TestRef = "missing/app_test.go::TestActual"
	manifest.Cases = manifest.Cases[:1]
	manifest.Edges = manifest.Edges[:2]
	for i := range manifest.Edges {
		manifest.Edges[i].ReviewKind = coverage.ReviewWhitebox
	}
	target.Mode = coverage.ReviewWhitebox
	target.Task = runtime.TaskKey{Node: target.Task.Node, Step: target.Task.Step, Scope: "wave:WHITEBOX"}
	wave := s.currentValidationWave()
	wave.Targets = map[string]*ValidationTarget{target.Task.String(): target}
	wave.Coverage.SelectedKinds = []coverage.ReviewKind{coverage.ReviewWhitebox}
	wave.Reviews[0].Binding = manifest.Binding
	wave.Reviews[0].CaseDecisions = wave.Reviews[0].CaseDecisions[:1]
	resource := s.taskResource(target.Task, cd)
	resource.Status, resource.Identity = "READY", wave.Candidate.Identity
	s.NativeResources[resource.ID] = resource
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 2 || plans[1].QA.GoTestRef != "missing/app_test.go::TestActual" || plans[1].QA.Operation.Kind != "" {
		t.Fatalf("core read or reconstructed Go source: %+v %v", plans, err)
	}
}

func TestQACarriedDutyPreservesNativeRecipesAndReceipts(t *testing.T) {
	t.Parallel()
	s, cd, target := qaOperationState(t)
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		t.Fatal(err)
	}
	binding := plans[0].Binding
	s.Attempts = map[runtime.TaskKey]Attempt{target.Task: {Work: &binding}}
	for _, p := range plans {
		addQAOperationReceipt(t, s, p, "original:"+p.QA.Source, 7)
	}
	facts := s.qaExecutionFacts(target, cd)
	raw, err := json.Marshal(s.currentValidationWave())
	if err != nil {
		t.Fatal(err)
	}
	var current ValidationWave
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	s.Validation.Waves[0].Status = "INVALIDATED"
	current.ID, current.Generation, current.RequirementRevision = "carried-wave", 2, "new-req"
	// Candidate rebuilding suspends execution without discarding the carried
	// exhausted duty that the next frozen wave must retain.
	s.RequirementRevisions = append(s.RequirementRevisions, ConfirmedRevision{Revision: "new-req", StateRevision: 2, WorkDecisions: []RequirementWorkDecision{{Work: RequirementWork{Task: target.Task}, Disposition: RequirementWorkCarry}}})
	s.Validation.Generation, s.Validation.RequirementRevision = 2, "new-req"
	if !s.validationWorkOwed(binding, cd) || len(s.qaExecutionFacts(target, cd)) != 0 {
		t.Fatal("candidate rebuilding lost carried duty or allowed premature execution")
	}
	s.Validation.Waves = append(s.Validation.Waves, current)
	target = s.currentValidationWave().Targets[target.Task.String()]
	assertCarried := func(state *State) {
		t.Helper()
		if !state.validationWorkOwed(binding, cd) {
			t.Fatal("confirmed carried duty is not owed")
		}
		got, err := state.qaCheckPlans(target, cd)
		if err != nil || !reflect.DeepEqual(got, plans) {
			t.Fatalf("carried recipes changed: %+v %v", got, err)
		}
		if !reflect.DeepEqual(state.qaExecutionFacts(target, cd), facts) {
			t.Fatal("carried receipts changed")
		}
		if _, ready := state.nextQACheckParameters(target.Task, cd); ready {
			t.Fatal("carried executed operation was reissued")
		}
		for _, p := range plans {
			if !state.qaCheckInputsCurrent(p) {
				t.Fatal("original native check lost its carried authority")
			}
		}
	}
	assertCarried(s)
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	assertCarried(&reopened)
	for _, change := range []string{"redo", "candidate", "scope", "cases", "ended"} {
		t.Run(change, func(t *testing.T) {
			var changed State
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			wave := changed.currentValidationWave()
			switch change {
			case "redo":
				changed.RequirementRevisions[1].WorkDecisions[0].Disposition = RequirementWorkRedo
			case "candidate":
				wave.Candidate.Digest = "different-tree"
			case "scope":
				wave.Scope = coverage.ScopeFull
			case "cases":
				wave.Versions = map[coverage.ReviewKind]string{coverage.ReviewBlackbox: "different-cases"}
				wave.Coverage.Manifests[0].Cases[0].Operations[0].Stdin = "different-input"
			case "ended":
				wave.Status = "COMPLETE"
			}
			if changed.validationWorkOwed(binding, cd) || len(changed.qaExecutionFacts(target, cd)) != 0 {
				t.Fatal("changed or unconfirmed duty reused native receipts")
			}
			for _, p := range plans {
				if changed.qaCheckInputsCurrent(p) {
					t.Fatal("original native check remained authorized for changed duty")
				}
			}
		})
	}
}

func TestRequirementBarrierPreservesQAReceiptsAndFreezesExecution(t *testing.T) {
	t.Parallel()
	for _, executed := range []bool{false, true} {
		t.Run(map[bool]string{false: "awaiting-operation", true: "completed-operation"}[executed], func(t *testing.T) {
			s, cd, target := qaOperationState(t)
			plans, err := s.qaCheckPlans(target, cd)
			if err != nil {
				t.Fatal(err)
			}
			var intent HostActionIntent
			if executed {
				for _, p := range plans {
					intent = addQAOperationReceipt(t, s, p, "actual output", 0)
				}
			} else {
				p, ready := s.nextQACheckParameters(target.Task, cd)
				if !ready {
					t.Fatal("approved operation was not eligible before the requirement barrier")
				}
				e := &Engine{cfg: Config{Definition: cd}}
				intent, err = e.prepareAdapterIntent(s, 1, "op.check", p, "")
				if err != nil {
					t.Fatal(err)
				}
				s.PendingHostActions[intent.ActionID] = intent
				if !s.NativeActionWritable(intent, cd) {
					t.Fatal("issued QA operation was not writable before the requirement barrier")
				}
			}
			facts := s.qaExecutionFacts(target, cd)
			receipts, err := json.Marshal(s.HostActionReceipts)
			if err != nil {
				t.Fatal(err)
			}
			s.PendingAsks = map[string]PendingAsk{"requirement": {RequestID: "requirement", Control: ControlRequestRequirementChange}}
			got, err := s.qaCheckPlans(target, cd)
			if err != nil || !reflect.DeepEqual(got, plans) {
				t.Fatalf("requirement barrier changed the original frozen recipe: %+v %v", got, err)
			}
			if !reflect.DeepEqual(s.qaExecutionFacts(target, cd), facts) {
				t.Fatal("requirement barrier changed completed execution facts")
			}
			if _, ready := s.nextQACheckParameters(target.Task, cd); ready {
				t.Fatal("requirement barrier allowed a new native QA execution")
			}
			if s.NativeActionWritable(intent, cd) {
				t.Fatal("requirement barrier allowed an issued native QA writer")
			}
			after, err := json.Marshal(s.HostActionReceipts)
			if err != nil || string(after) != string(receipts) {
				t.Fatal("read-only QA projection changed native receipts")
			}
		})
	}
}
