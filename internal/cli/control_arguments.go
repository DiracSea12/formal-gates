package cli

import (
	"flag"
	"fmt"
	"strconv"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

type controlChildArguments struct {
	choice facade.ChildChoice
	work   map[int]*facade.WorkChoice
}

type controlArguments struct {
	fs                *flag.FlagSet
	groups            map[string]string
	value             facade.ControlInput
	requirement       facade.RequirementChangeInput
	validation        facade.ValidationControlInput
	request           string
	validationMode    string
	gates, owners     stringListFlag
	redo              stringListFlag
	changedItems      stringListFlag
	transitiveImpact  stringListFlag
	carryReasons      stringListFlag
	cases             stringListFlag
	obligations       stringListFlag
	remedies          stringListFlag
	work              map[int]*facade.WorkChoice
	children          map[int]*controlChildArguments
	selectedWork      *facade.WorkChoice
	selectedChild     *controlChildArguments
	selectedChildWork *facade.WorkChoice
}

func registerControlArguments(fs *flag.FlagSet) *controlArguments {
	a := &controlArguments{fs: fs, groups: map[string]string{}, work: map[int]*facade.WorkChoice{}, children: map[int]*controlChildArguments{}}
	text := func(name string, target *string, group, help string) {
		fs.StringVar(target, name, "", help)
		a.groups[name] = group
	}
	number := func(name string, target *int, group, help string) {
		fs.IntVar(target, name, 0, help)
		a.groups[name] = group
	}
	list := func(name string, target *stringListFlag, group, help string) {
		fs.Var(target, name, help+" (repeatable)")
		a.groups[name] = group
	}
	function := func(name, group, help string, set func(string) error) {
		fs.Func(name, help, set)
		a.groups[name] = group
	}
	text("request", &a.request, "control", "semantic control request")
	number("ask", &a.value.Ask, "control", "current Ask number")
	number("node", &a.value.Node, "control", "current business node number to continue from")
	text("choice", &a.value.Choice, "control", "offered semantic choice")
	text("reason", &a.value.Reason, "control", "decision reason")
	list("gate", &a.gates, "control", "current gate number")
	text("parallel", &a.value.Parallel, "control", "parallelization choice")
	list("owner", &a.owners, "control", "current owner number; zero selects master merge")
	number("review", &a.value.Review, "control", "current review number")
	number("candidate", &a.value.Candidate, "control", "current review candidate number")
	number("finding", &a.value.Finding, "control", "current finding number")
	text("severity", &a.value.Severity, "control", "corrected severity")
	text("severity-rationale", &a.value.SeverityRationale, "control", "severity correction rationale")
	text("invalidity", &a.value.Invalidity, "control", "result invalidity explanation")

	text("classification", &a.requirement.Classification, "requirement", "requirement change classification")
	list("redo", &a.redo, "requirement", "review, topology or route to redo")
	list("changed-item", &a.changedItems, "requirement", "changed requirement meaning")
	list("transitive-impact", &a.transitiveImpact, "requirement", "transitive impact explanation")
	list("carry-reason", &a.carryReasons, "requirement", "carry justification")
	number("requirement-review", &a.requirement.Review, "requirement", "review supplying the requirement change finding")
	number("requirement-candidate", &a.requirement.Candidate, "requirement", "candidate supplying the requirement change finding")
	number("requirement-finding", &a.requirement.Finding, "requirement", "finding supplying the requirement change")
	function("work", "requirement", "select current requirement work number for subsequent work fields", func(value string) error {
		n, err := controlPosition(value)
		if err != nil {
			return err
		}
		if a.work[n] == nil {
			a.work[n] = &facade.WorkChoice{}
		}
		a.selectedWork = a.work[n]
		return nil
	})
	function("work-disposition", "requirement", "CARRY or REDO for the selected work", func(value string) error {
		if a.selectedWork == nil {
			return fmt.Errorf("--work-disposition requires a preceding --work")
		}
		a.selectedWork.Disposition = value
		return nil
	})
	function("work-reason", "requirement", "reason for the selected work disposition", func(value string) error {
		if a.selectedWork == nil {
			return fmt.Errorf("--work-reason requires a preceding --work")
		}
		a.selectedWork.Reason = value
		return nil
	})
	function("child", "requirement", "select current child number for subsequent child fields", func(value string) error {
		n, err := controlPosition(value)
		if err != nil {
			return err
		}
		if a.children[n] == nil {
			a.children[n] = &controlChildArguments{work: map[int]*facade.WorkChoice{}}
		}
		a.selectedChild, a.selectedChildWork = a.children[n], nil
		return nil
	})
	function("child-work", "requirement", "select work number in the selected child", func(value string) error {
		if a.selectedChild == nil {
			return fmt.Errorf("--child-work requires a preceding --child")
		}
		n, err := controlPosition(value)
		if err != nil {
			return err
		}
		if a.selectedChild.work[n] == nil {
			a.selectedChild.work[n] = &facade.WorkChoice{}
		}
		a.selectedChildWork = a.selectedChild.work[n]
		return nil
	})
	function("child-work-disposition", "requirement", "CARRY or REDO for the selected child work", func(value string) error {
		if a.selectedChildWork == nil {
			return fmt.Errorf("--child-work-disposition requires a preceding --child-work")
		}
		a.selectedChildWork.Disposition = value
		return nil
	})
	function("child-work-reason", "requirement", "reason for the selected child work disposition", func(value string) error {
		if a.selectedChildWork == nil {
			return fmt.Errorf("--child-work-reason requires a preceding --child-work")
		}
		a.selectedChildWork.Reason = value
		return nil
	})
	function("child-receipt", "requirement", "receipt disposition for the selected child", func(value string) error {
		if a.selectedChild == nil {
			return fmt.Errorf("--child-receipt requires a preceding --child")
		}
		a.selectedChild.choice.Receipt = value
		return nil
	})
	function("child-candidate", "requirement", "candidate disposition for the selected child", func(value string) error {
		if a.selectedChild == nil {
			return fmt.Errorf("--child-candidate requires a preceding --child")
		}
		a.selectedChild.choice.Candidate = value
		return nil
	})
	function("child-route", "requirement", "route disposition for the selected child", func(value string) error {
		if a.selectedChild == nil {
			return fmt.Errorf("--child-route requires a preceding --child")
		}
		a.selectedChild.choice.Route = value
		return nil
	})
	function("child-reason", "requirement", "reason for the selected child dispositions", func(value string) error {
		if a.selectedChild == nil {
			return fmt.Errorf("--child-reason requires a preceding --child")
		}
		a.selectedChild.choice.Reason = value
		return nil
	})

	text("validation-kind", &a.validation.Kind, "validation", "validation action kind")
	text("validation-mode", &a.validationMode, "validation", "QA review kind")
	text("validation-category", &a.validation.Category, "validation", "validation category")
	number("validation-target", &a.validation.Target, "validation", "current validation target number")
	list("validation-case", &a.cases, "validation", "current QA case number")
	list("validation-obligation", &a.obligations, "validation", "current validation obligation number")
	list("validation-remedies", &a.remedies, "validation", "validation remedy choice")
	text("validation-remedy", &a.validation.Remedy, "validation", "validation remedy choice")
	return a
}

func (a *controlArguments) input() (facade.ControlInput, bool, error) {
	provided, requirementProvided, validationProvided := false, false, false
	a.fs.Visit(func(f *flag.Flag) {
		group, ok := a.groups[f.Name]
		provided = provided || ok
		requirementProvided = requirementProvided || group == "requirement"
		validationProvided = validationProvided || group == "validation"
	})
	out := a.value
	out.Request = protocol.ControlKind(a.request)
	if out.Node != 0 && out.Request == "" {
		out.Request = protocol.ControlContinueNode
	}
	var err error
	if out.Gates, err = controlNumbers(a.gates); err != nil {
		return out, provided, fmt.Errorf("--gate: %w", err)
	}
	if out.Owners, err = controlNumbers(a.owners); err != nil {
		return out, provided, fmt.Errorf("--owner: %w", err)
	}
	if requirementProvided {
		r := a.requirement
		r.Redo = []string(a.redo)
		r.Impact = protocol.RequirementImpact{ChangedItems: []string(a.changedItems), TransitiveImpacts: []string(a.transitiveImpact), CarryReasons: []string(a.carryReasons)}
		if r.Work, err = orderedControlWork(a.work); err != nil {
			return out, provided, fmt.Errorf("requirement work: %w", err)
		}
		for n := 1; n <= len(a.children); n++ {
			child := a.children[n]
			if child == nil {
				return out, provided, fmt.Errorf("missing --child %d", n)
			}
			choice := child.choice
			if choice.Work, err = orderedControlWork(child.work); err != nil {
				return out, provided, fmt.Errorf("child %d work: %w", n, err)
			}
			r.Children = append(r.Children, choice)
		}
		out.Requirement = &r
	}
	if validationProvided {
		v := a.validation
		v.Mode, v.Remedies = coverage.ReviewKind(a.validationMode), []string(a.remedies)
		if v.Cases, err = controlNumbers(a.cases); err != nil {
			return out, provided, fmt.Errorf("--validation-case: %w", err)
		}
		if v.Obligations, err = controlNumbers(a.obligations); err != nil {
			return out, provided, fmt.Errorf("--validation-obligation: %w", err)
		}
		out.Validation = &v
	}
	return out, provided, nil
}

func controlPosition(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("expected a positive current position, got %q", value)
	}
	return n, nil
}

func controlNumbers(values []string) ([]int, error) {
	var out []int
	for _, value := range values {
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("expected a current position, got %q", value)
		}
		out = append(out, n)
	}
	return out, nil
}

func orderedControlWork(values map[int]*facade.WorkChoice) ([]facade.WorkChoice, error) {
	var out []facade.WorkChoice
	for n := 1; n <= len(values); n++ {
		choice := values[n]
		if choice == nil {
			return nil, fmt.Errorf("missing work position %d", n)
		}
		out = append(out, *choice)
	}
	return out, nil
}
