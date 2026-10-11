package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func TestQACommandBindingKeepsApprovalAndOriginalRecipe(t *testing.T) {
	s, cd, target := qaOperationState(t)
	manifest := &s.currentValidationWave().Coverage.Manifests[0]
	manifest.Cases[0].Operations = nil
	before, _ := json.Marshal(manifest.Cases[0])
	if !s.qaComparisonReady(target, cd) || !reflect.DeepEqual(s.qaCommandCases(target, cd), []string{"case"}) {
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
	if len(s.qaCommandCases(target, cd)) != 0 {
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
}
