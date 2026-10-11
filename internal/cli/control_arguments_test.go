package cli

import (
	"flag"
	"io"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func TestControlArgumentsPreserveAllSemanticFieldsAndSelectedPositions(t *testing.T) {
	fs := flag.NewFlagSet("control", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	a := registerControlArguments(fs)
	args := []string{
		"--request", "REQUEST_REQUIREMENT_CHANGE", "--ask", "2", "--choice", "proceed", "--reason", "保留 \"原文\"\n和空格  ",
		"--gate", "3", "--gate", "1", "--parallel", "parallel", "--owner", "0", "--owner", "2",
		"--review", "4", "--candidate", "3", "--finding", "2", "--severity", "P2", "--severity-rationale", "可规避", "--invalidity", "wrong premise",
		"--classification", "meaning-changed", "--redo", "route", "--redo", "product-review", "--changed-item", "new output", "--changed-item", "new command",
		"--transitive-impact", "dependent caller", "--carry-reason", "unaffected UI", "--requirement-review", "2", "--requirement-candidate", "1", "--requirement-finding", "3",
		"--work", "2", "--work-disposition", "REDO", "--work-reason", "output changed", "--work", "1", "--work-disposition", "CARRY", "--work-reason", "unaffected",
		"--child", "2", "--child-route", "REDO", "--child-reason", "route changed", "--child-candidate", "REDO", "--child-receipt", "REDO",
		"--child-work", "1", "--child-work-disposition", "REDO", "--child-work-reason", "child output changed",
		"--child", "1", "--child-route", "CARRY", "--child-reason", "child unchanged", "--child-candidate", "CARRY", "--child-receipt", "CARRY",
		"--child-work", "2", "--child-work-disposition", "CARRY", "--child-work-reason", "second unchanged",
		"--child-work", "1", "--child-work-disposition", "CARRY", "--child-work-reason", "first unchanged",
		"--validation-kind", "DISPOSE", "--validation-mode", "blackbox", "--validation-category", "QA",
		"--validation-target", "3", "--validation-case", "2", "--validation-case", "1", "--validation-obligation", "4", "--validation-obligation", "2",
		"--validation-remedies", "retry", "--validation-remedies", "skip", "--validation-remedy", "waive",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	actual, provided, err := a.input()
	expected := facade.ControlInput{
		Request: protocol.ControlKind("REQUEST_REQUIREMENT_CHANGE"), Ask: 2, Choice: "proceed", Reason: "保留 \"原文\"\n和空格  ", Gates: []int{3, 1}, Parallel: "parallel", Owners: []int{0, 2}, Review: 4, Candidate: 3, Finding: 2, Severity: "P2", SeverityRationale: "可规避", Invalidity: "wrong premise",
		Requirement: &facade.RequirementChangeInput{
			Classification: "meaning-changed", Redo: []string{"route", "product-review"}, Review: 2, Candidate: 1, Finding: 3,
			Impact: protocol.RequirementImpact{ChangedItems: []string{"new output", "new command"}, TransitiveImpacts: []string{"dependent caller"}, CarryReasons: []string{"unaffected UI"}},
			Work:   []facade.WorkChoice{{Disposition: "CARRY", Reason: "unaffected"}, {Disposition: "REDO", Reason: "output changed"}},
			Children: []facade.ChildChoice{
				{Receipt: "CARRY", Candidate: "CARRY", Route: "CARRY", Reason: "child unchanged", Work: []facade.WorkChoice{{Disposition: "CARRY", Reason: "first unchanged"}, {Disposition: "CARRY", Reason: "second unchanged"}}},
				{Receipt: "REDO", Candidate: "REDO", Route: "REDO", Reason: "route changed", Work: []facade.WorkChoice{{Disposition: "REDO", Reason: "child output changed"}}},
			},
		},
		Validation: &facade.ValidationControlInput{Kind: "DISPOSE", Mode: coverage.ReviewKind("blackbox"), Category: "QA", Target: 3, Cases: []int{2, 1}, Obligations: []int{4, 2}, Remedies: []string{"retry", "skip"}, Remedy: "waive"},
	}
	if err != nil || !provided || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("all semantic fields must retain their values and own selected positions: provided=%v err=%v\nactual=%#v\nexpected=%#v", provided, err, actual, expected)
	}
}

func TestControlArgumentsDistinguishAbsentAndExplicitEmptyFields(t *testing.T) {
	for _, test := range []struct {
		args     []string
		provided bool
		input    facade.ControlInput
	}{
		{args: []string{"--root", "somewhere"}},
		{args: []string{"--reason", ""}, provided: true},
		{args: []string{"--classification", ""}, provided: true, input: facade.ControlInput{Requirement: &facade.RequirementChangeInput{}}},
		{args: []string{"--validation-kind", ""}, provided: true, input: facade.ControlInput{Validation: &facade.ValidationControlInput{}}},
	} {
		fs := flag.NewFlagSet("control", flag.ContinueOnError)
		fs.String("root", "", "root")
		a := registerControlArguments(fs)
		if err := fs.Parse(test.args); err != nil {
			t.Fatal(err)
		}
		input, provided, err := a.input()
		if err != nil || provided != test.provided || !reflect.DeepEqual(input, test.input) {
			t.Fatalf("%v: input=%#v provided=%v err=%v", test.args, input, provided, err)
		}
	}
}

func TestControlArgumentsRejectUnselectedFieldsAndMissingPositions(t *testing.T) {
	for _, args := range [][]string{
		{"--work-disposition", "CARRY"}, {"--work-reason", "unaffected"},
		{"--child-work", "1"}, {"--child-receipt", "CARRY"}, {"--child-candidate", "CARRY"}, {"--child-route", "CARRY"}, {"--child-reason", "unaffected"},
		{"--child-work-disposition", "CARRY"}, {"--child-work-reason", "unaffected"},
		{"--child", "1", "--child-work", "1", "--child", "2", "--child-work-reason", "must select this child's work"},
		{"--work", "2"}, {"--child", "2"}, {"--child", "1", "--child-work", "2"},
		{"--work", "0"}, {"--child", "not-a-number"}, {"--gate", "missing-number"}, {"--validation-case", "missing-number"},
	} {
		fs := flag.NewFlagSet("control", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		a := registerControlArguments(fs)
		err := fs.Parse(args)
		if err == nil {
			_, _, err = a.input()
		}
		if err == nil {
			t.Fatal("unselected field or missing current position accepted", args)
		}
	}
}
