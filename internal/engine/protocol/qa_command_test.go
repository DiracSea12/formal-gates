package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func TestQACommandBindingKeepsApprovalAndOriginalRecipe(t *testing.T) {
	s, cd, target := qaOperationState(t)
	manifest := &s.currentValidationWave().Coverage.Manifests[0]
	manifest.Cases[0].Operations = nil
	before, _ := json.Marshal(manifest.Cases[0])
	missing := s.qaCommandPlans(target, cd)
	if !s.qaComparisonReady(target, cd) || len(missing) != 1 || missing[0].QA.CaseID != "case" || missing[0].QA.Source != "case:case" {
		t.Fatal("approved missing invocation was not exposed as input")
	}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{cfg: Config{Definition: cd}}
	commands := ValidationResult{Binding: plans[0].Binding, Checkpoint: true, Status: "PASS", QACommands: []QACaseOperations{{CaseID: "case", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf rejected; exit 7"}, {Kind: coverage.OperationArgv, Argv: []string{"./app", ""}, Stdin: "original input"}}}}}
	intents, err := e.qaCommandIntents(s, commands, 1)
	if err != nil || len(intents) != 1 {
		t.Fatalf("missing invocation binding: %+v %v", intents, err)
	}
	for _, intent := range intents {
		s.PendingHostActions[intent.ActionID] = intent
		if s.NativeActionWritable(intent, cd) {
			t.Fatal("bound command bypassed its shared preparation prefix")
		}
	}
	if _, err := e.qaCommandIntents(s, commands, 2); err == nil {
		t.Fatal("already bound invocation was filled again")
	}
	if len(s.qaCommandPlans(target, cd)) != 0 {
		t.Fatal("original recipe was not retained by its intent")
	}
	plans, err = s.qaCheckPlans(target, cd)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if intent := s.latestNativeIntent("check:" + digestOfCanonical(p)); intent.ActionID != "" && !s.NativeActionWritable(intent, cd) {
			t.Fatal("bound recipe lost its legitimate execution order")
		}
		addQAOperationReceipt(t, s, p, p.QA.Source, 7)
	}
	facts := s.qaExecutionFacts(target, cd)
	if len(facts) != 1 || len(facts[0].Operations) != 3 || facts[0].Operations[1].Parameters.QA.Operation.Command != commands.QACommands[0].Operations[0].Command || facts[0].Operations[2].Parameters.QA.Operation.Argv[1] != "" {
		t.Fatalf("actual bound command/order lost: %+v", facts)
	}
	after, _ := json.Marshal(manifest.Cases[0])
	if string(before) != string(after) {
		t.Fatal("execution binding modified the approved steps or oracle")
	}
	body, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if _, ready := restored.nextQACheckParameters(target.Task, cd); ready || !reflect.DeepEqual(restored.qaExecutionFacts(restored.currentValidationWave().Targets[target.Task.String()], cd), facts) {
		t.Fatal("recovery repeated or changed the bound native recipe")
	}
}

func TestQACommandBindingRejectsUnissuedOrChangedInvocations(t *testing.T) {
	for _, invalid := range []string{"unknown-case", "empty", "declared", "changed-binding"} {
		t.Run(invalid, func(t *testing.T) {
			s, cd, target := qaOperationState(t)
			if invalid != "declared" {
				s.currentValidationWave().Coverage.Manifests[0].Cases[0].Operations = nil
			}
			plans, err := s.qaCheckPlans(target, cd)
			if err != nil {
				t.Fatal(err)
			}
			result := ValidationResult{Binding: plans[0].Binding, QACommands: []QACaseOperations{{CaseID: "case", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf original"}}}}}
			switch invalid {
			case "unknown-case":
				result.QACommands[0].CaseID = "outside-whitelist"
			case "empty":
				result.QACommands[0].Operations = nil
			case "changed-binding":
				result.Binding.Candidate = "other"
			}
			e := &Engine{cfg: Config{Definition: cd}}
			before, _ := json.Marshal(s)
			if _, err := e.qaCommandIntents(s, result, 1); err == nil {
				t.Fatal("invalid invocation binding accepted")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("failed binding changed state")
			}
		})
	}
}

func TestQAIndependentCasesAdvanceWhileOriginalCheckWaits(t *testing.T) {
	s, cd, target := qaOperationState(t)
	s.currentValidationWave().Scope = coverage.ScopeFull
	s.currentValidationWave().Coverage.Manifests[0].Cases[1].SharedSteps = []string{"setup"}
	e := &Engine{cfg: Config{Definition: cd}}
	first, ready := s.nextQACheckParameters(target.Task, cd)
	if !ready {
		t.Fatal("first approved preparation was not ready")
	}
	intent, err := e.prepareAdapterIntent(s, 1, "op.check", first, "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[intent.ActionID] = intent
	second, ready := s.nextQACheckParameters(target.Task, cd)
	if !ready || second.QA.CaseID == first.QA.CaseID || second.QA.Ordinal != 0 {
		t.Fatal("waiting preparation blocked an independent case or skipped its prefix")
	}
	other, err := e.prepareAdapterIntent(s, 2, "op.check", second, "")
	if err != nil {
		t.Fatal(err)
	}
	s.PendingHostActions[other.ActionID] = other
	if _, ready := s.nextQACheckParameters(target.Task, cd); ready {
		t.Fatal("later operation ran before either original preparation receipt")
	}
	if len(s.CommonPreparationIntents(second)) != 2 {
		t.Fatal("parallel common preparation did not share the original native owner set")
	}
	if s.NativeActionWritable(other, cd) || s.ExecutableHostActions(cd)[other.ActionID].ActionID != "" {
		t.Fatal("early common preparation intent bypassed the original consumer case")
	}
}

// Approved shared procedures retain their invocation duties and source identity.
func TestSharedInvocationPlansKeepOrderedRecipesAcrossRecoveryAndRepair(t *testing.T) {
	s, cd, target := qaOperationState(t)
	wave := s.currentValidationWave()
	wave.Scope = coverage.ScopeFull
	manifest := &wave.Coverage.Manifests[0]
	manifest.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "prepare first", Independent: true, Artifacts: []string{"built/input"}}, {ID: "ready", Procedure: "prepare second"}}
	for i := range manifest.Cases {
		manifest.Cases[i].SharedSteps = []string{"setup", "ready"}
		manifest.Cases[i].Operations = nil
	}
	wave.Reviews[0].SharedStepDecisions = append(wave.Reviews[0].SharedStepDecisions, coverage.ReviewDecision{ID: "ready", Status: coverage.StatusPass})
	approved, _ := json.Marshal(manifest)
	missing := s.qaCommandPlans(target, cd)
	wantSources := []string{"shared:setup", "shared:ready", "case:case", "case:complement"}
	if len(missing) != len(wantSources) {
		t.Fatalf("missing duties=%+v", missing)
	}
	for i, p := range missing {
		if p.QA.Source != wantSources[i] {
			t.Fatalf("duty %d=%s", i, p.QA.Source)
		}
	}
	if _, ready := s.nextQACheckParameters(target.Task, cd); ready {
		t.Fatal("case operation bypassed unbound preparation")
	}
	recipes := [][]coverage.Operation{
		{{Kind: coverage.OperationShell, Command: "printf first"}, {Kind: coverage.OperationArgv, Argv: []string{"./build", "one"}}},
		{{Kind: coverage.OperationShell, Command: "printf second"}},
		{{Kind: coverage.OperationShell, Command: "printf case"}, {Kind: coverage.OperationArgv, Argv: []string{"./read", "two"}, Stdin: "original input"}},
		{{Kind: coverage.OperationShell, Command: "printf other"}},
	}
	result := ValidationResult{Binding: missing[0].Binding}
	for i, p := range missing {
		command := QACaseOperations{CaseID: p.QA.CaseID, Operations: recipes[i]}
		if p.QA.Preparation != nil {
			command.Source = p.QA.Source
		}
		result.QACommands = append(result.QACommands, command)
	}
	e := &Engine{cfg: Config{Definition: cd}}
	before, _ := json.Marshal(s)
	intents, err := e.qaCommandIntents(s, result, 7)
	if err != nil || len(intents) != 4 {
		t.Fatalf("signing %+v %v", intents, err)
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("preparation mutated live state before transaction")
	}
	for i, intent := range intents {
		p, err := checkParameters(intent)
		if err != nil {
			t.Fatal(err)
		}
		ord := []int{0, 2, 3, 3}[i]
		if p.QA.Source != wantSources[i] || p.QA.Ordinal != ord || !reflect.DeepEqual(p.QA.Recipe, recipes[i]) {
			t.Fatalf("signed source/ordinal/recipe=%+v", p.QA)
		}
		s.PendingHostActions[intent.ActionID] = intent
	}
	if len(s.qaCommandPlans(target, cd)) != 0 {
		t.Fatal("shared recipe was not reused by other approved case")
	}
	if _, err := e.qaCommandIntents(s, result, 8); err == nil {
		t.Fatal("duplicate invocation filling accepted")
	}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 9 {
		t.Fatalf("expanded plans %d %v", len(plans), err)
	}
	for _, p := range plans {
		if p.QA.Source == "shared:setup" && (!p.QA.Preparation.Independent || !reflect.DeepEqual(p.QA.Preparation.Artifacts, []string{"built/input"}) || !reflect.DeepEqual(p.QA.Preparation.Operations, recipes[0])) {
			t.Fatal("approved preparation attributes lost")
		}
	}
	binding := result.Binding
	s.Attempts = map[runtime.TaskKey]Attempt{target.Task: {Work: &binding}}
	raw, _ := json.Marshal(s)
	var recovered State
	if err := json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	got, err := recovered.qaCheckPlans(recovered.currentValidationWave().Targets[target.Task.String()], cd)
	if err != nil || !reflect.DeepEqual(got, plans) {
		t.Fatal("unexecuted recipes changed after recovery", err)
	}
	for _, p := range plans {
		addQAOperationReceipt(t, s, p, "actual:"+p.QA.Source, 0)
	}
	facts := s.qaExecutionFacts(target, cd)
	if len(facts) != 2 || len(facts[0].Operations) != 5 || len(facts[1].Operations) != 4 {
		t.Fatalf("incomplete original facts=%+v", facts)
	}
	s.ReconciledEffects = map[string]ReconciledEffect{}
	for id, intent := range s.PendingHostActions {
		copy := intent
		s.ReconciledEffects[id] = ReconciledEffect{Intent: &copy, Status: "FULFILLED"}
		delete(s.PendingHostActions, id)
	}
	raw, _ = json.Marshal(s)
	if err := json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.qaExecutionFacts(recovered.currentValidationWave().Targets[target.Task.String()], cd), facts) {
		t.Fatal("reconciled original receipt/recipe identity changed")
	}
	for i, source := range wantSources[:3] {
		if !reflect.DeepEqual(s.qaApprovedRecipe(wave, coverage.ReviewBlackbox, "complement", source), recipes[i]) && i < 2 {
			t.Fatalf("shared recipe cannot be carried into repair: %s", source)
		}
	}
	context := repairComparison{binding: binding, wave: wave, resource: s.NativeResources[binding.WorkspaceID], source: CandidateEvidence{Identity: "repaired-candidate", Digest: "repaired-tree"}, affected: []string{"case", "complement"}}
	repair, err := s.repairComparisonPlans(context, cd)
	if err != nil || len(repair) != len(plans) {
		t.Fatalf("repair plans=%+v %v", repair, err)
	}
	for i, p := range repair {
		if p.QA.Source != plans[i].QA.Source || p.QA.Ordinal != plans[i].QA.Ordinal || !reflect.DeepEqual(p.QA.Recipe, plans[i].QA.Recipe) || !reflect.DeepEqual(p.QA.Operation, plans[i].QA.Operation) || p.SourceIdentity != "repaired-candidate" {
			t.Fatalf("repair changed original recipe identity %d %+v", i, p)
		}
	}

	raw, _ = json.Marshal(s)
	var carried State
	if err := json.Unmarshal(raw, &carried); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(carried.currentValidationWave())
	var current ValidationWave
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	carried.Validation.Waves[0].Status = "INVALIDATED"
	current.ID, current.Generation, current.RequirementRevision = "carried-wave", 2, "new-req"
	carried.RequirementRevisions = append(carried.RequirementRevisions, ConfirmedRevision{Revision: "new-req", StateRevision: 2, WorkDecisions: []RequirementWorkDecision{{Work: RequirementWork{Task: target.Task}, Disposition: RequirementWorkCarry}}})
	carried.Validation.Generation, carried.Validation.RequirementRevision = 2, "new-req"
	carried.Validation.Waves = append(carried.Validation.Waves, current)
	carriedPlans, err := carried.qaCheckPlans(carried.currentValidationWave().Targets[target.Task.String()], cd)
	if err != nil || !reflect.DeepEqual(carriedPlans, plans) || !reflect.DeepEqual(carried.qaExecutionFacts(carried.currentValidationWave().Targets[target.Task.String()], cd), facts) {
		t.Fatal("carried original recipe/receipt changed", err)
	}
	for _, p := range carriedPlans {
		if !carried.qaCheckInputsCurrent(p) {
			t.Fatal("shared recipe lost original carried native authority")
		}
	}
	afterApproval, _ := json.Marshal(manifest)
	if string(approved) != string(afterApproval) {
		t.Fatal("binding rewrote frozen approval")
	}
}

func TestSharedMissingInvocationPreservesDeclaredCaseAndSharedOperations(t *testing.T) {
	s, cd, target := qaOperationState(t)
	manifest := &s.currentValidationWave().Coverage.Manifests[0]
	manifest.SharedSteps[0].Operations = nil
	before, _ := json.Marshal(manifest)
	missing := s.qaCommandPlans(target, cd)
	if len(missing) != 1 || missing[0].QA.Source != "shared:setup" {
		t.Fatal("known case invocation was requested again")
	}
	recipe := []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf ready"}}
	intents, err := (&Engine{cfg: Config{Definition: cd}}).qaCommandIntents(s, ValidationResult{Binding: missing[0].Binding, QACommands: []QACaseOperations{{CaseID: "case", Source: "shared:setup", Operations: recipe}}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range intents {
		s.PendingHostActions[intent.ActionID] = intent
	}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil || len(plans) != 2 || !reflect.DeepEqual(plans[1].QA.Operation, manifest.Cases[0].Operations[0]) || len(plans[1].QA.Recipe) != 0 {
		t.Fatalf("known case changed %+v %v", plans, err)
	}
	after, _ := json.Marshal(manifest)
	if string(before) != string(after) {
		t.Fatal("manifest changed")
	}
}
