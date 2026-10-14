package worker

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

// ResultSchema contains the role's semantic body and a closed inability
// alternative. Issued identities and completed transport outcome are not model fields.
func ResultSchema(input protocol.WorkerInput) (map[string]any, error) {
	role, err := semanticRole(input)
	if err != nil {
		return nil, err
	}
	body, err := completedSchema(input, role)
	if err != nil {
		return nil, err
	}
	interruption := fixedObject(map[string]any{"interruptionReason": map[string]any{"type": "string", "minLength": 1}}, []string{"interruptionReason"})
	if _, comparison := role.(*ExecutionJudgments); comparison {
		fields := interruption["properties"].(map[string]any)
		fields["records"] = body["properties"].(map[string]any)["records"]
		fields["findings"] = body["properties"].(map[string]any)["findings"]
	}
	alternatives := []any{body, interruption}
	if _, preparing := role.(*DevelopmentContinuation); !preparing && input.Validation != nil && (input.Handler == "engine.development.batch.v6" || input.Handler == "engine.repair.v7") {
		partial, err := resultTypeSchema(reflect.TypeOf(DevelopmentContinuation{}))
		if err != nil {
			return nil, err
		}
		for _, field := range []string{"summary", "remainingWork"} {
			partial["properties"].(map[string]any)[field] = map[string]any{"type": "string", "minLength": 1}
		}
		if commands := body["properties"].(map[string]any)["commands"]; commands != nil {
			partial["properties"].(map[string]any)["commands"] = commands
		} else {
			delete(partial["properties"].(map[string]any), "commands")
		}
		alternatives = append(alternatives, partial)
	}
	if input.Handler == "engine.repair.v7" && completionBatch(input) == nil && body["properties"].(map[string]any)["completion"] != nil {
		fields := body["properties"].(map[string]any)
		completed := maps.Clone(body)
		completedFields := maps.Clone(fields)
		delete(completedFields, "requirements")
		completed["properties"] = completedFields
		delete(completed, "anyOf")
		completed["required"] = append(slices.Clone(body["required"].([]any)), "granularityReview", "completion", "summary")
		completedFields["summary"] = map[string]any{"type": "string", "minLength": 1}
		completion := maps.Clone(completedFields["completion"].(map[string]any))
		completionFields := maps.Clone(completion["properties"].(map[string]any))
		completionFields["requirements"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "description": "Actual completion evidence for the proposed single batch's selected requirement points in their published order."}
		completion["properties"] = completionFields
		if len(input.RequirementPoints) > 0 {
			completion["required"] = append(slices.Clone(completion["required"].([]any)), "requirements")
		}
		completedFields["completion"] = completion
		plan := maps.Clone(fields["granularityReview"].(map[string]any))
		planFields := maps.Clone(plan["properties"].(map[string]any))
		batches := maps.Clone(planFields["batches"].(map[string]any))
		batches["minItems"], batches["maxItems"] = 1, 1
		batch := maps.Clone(batches["items"].(map[string]any))
		if alternatives, ok := batch["anyOf"].([]any); ok && batch["properties"] == nil {
			batch = maps.Clone(alternatives[0].(map[string]any))
		}
		batchFields := maps.Clone(batch["properties"].(map[string]any))
		delete(batchFields, "artifacts")
		delete(batchFields, "newArtifacts")
		batch["properties"] = batchFields
		delete(batch, "anyOf")
		batches["items"] = batch
		planFields["batches"], plan["properties"] = batches, planFields
		completedFields["granularityReview"] = plan
		delete(fields, "completion")
		alternatives = append(alternatives, completed)
	}
	return map[string]any{"type": "object", "anyOf": alternatives}, nil
}

// StableResultSchema publishes the current role's field and type contract across
// its native continuations. ResultSchema retains each invocation's exact choices.
func StableResultSchema(input protocol.WorkerInput) (map[string]any, error) {
	role, err := semanticRole(input)
	if err != nil {
		return nil, err
	}
	roles := []any{role}
	switch input.Handler {
	case "engine.qa.execution.v4":
		roles = []any{&CommandBindings{}, &ExecutionJudgments{}}
	case "engine.qa.design.v6":
		roles = []any{&Design{}, &CaseRevisions{}}
	case "engine.development.batch.v6":
		roles = []any{&Development{}, &DevelopmentContinuation{}}
	case "engine.repair.v7":
		roles = []any{&Repair{}, &DevelopmentContinuation{}}
	}
	alternatives := []any{}
	for _, variant := range roles {
		body, err := stableBodySchema(input, variant)
		if err != nil {
			return nil, err
		}
		alternatives = append(alternatives, body)
	}
	interruption := any(&Interruption{})
	if input.Handler == "engine.qa.execution.v4" {
		interruption = &ExecutionInterruption{}
	}
	body, err := stableBodySchema(input, interruption)
	if err != nil {
		return nil, err
	}
	alternatives = append(alternatives, body)
	return map[string]any{"type": "object", "anyOf": alternatives}, nil
}

func stableBodySchema(input protocol.WorkerInput, role any) (map[string]any, error) {
	body, err := resultTypeSchema(reflect.TypeOf(role), true)
	if err != nil {
		return nil, err
	}
	fields := body["properties"].(map[string]any)
	if plan, ok := fields["granularityReview"].(map[string]any); ok {
		properties := plan["properties"].(map[string]any)
		created := properties["batches"].(map[string]any)["items"].(map[string]any)
		delete(created["properties"].(map[string]any), "reuse")
		delete(created["properties"].(map[string]any), "remove")
		changed, err := resultTypeSchema(reflect.TypeOf(BatchChange{}), true)
		if err != nil {
			return nil, err
		}
		changed["properties"].(map[string]any)["reuse"] = map[string]any{"type": "integer", "minimum": 1}
		changed["properties"].(map[string]any)["remove"] = map[string]any{"type": "boolean"}
		changed["required"] = []string{"reuse"}
		for _, batch := range []map[string]any{created, changed} {
			properties := batch["properties"].(map[string]any)
			properties["route"].(map[string]any)["properties"].(map[string]any)["mode"] = map[string]any{"type": "string", "enum": []string{"full", "custom"}}
			location := LocationSchema(false)
			delete(location["properties"].(map[string]any), "line")
			properties["artifacts"] = map[string]any{"type": "array", "items": location}
			check := properties["checks"].(map[string]any)["items"].(map[string]any)
			check["properties"].(map[string]any)["purpose"] = map[string]any{"type": "string", "enum": append(protocol.DevelopmentCheckPurposes(), "")}
			if proposesVerificationGoals(input) {
				choices := []any{fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "minimum": 1}}, []string{"reuse"})}
				choices = append(choices, fixedObject(map[string]any{"goal": map[string]any{"type": "string", "minLength": 1}, "purpose": map[string]any{"type": "string", "enum": protocol.DevelopmentCheckPurposes()}}, []string{"goal"}))
				properties["checks"] = map[string]any{"type": "array", "items": map[string]any{"anyOf": choices}}
			} else {
				delete(check["properties"].(map[string]any), "goal")
			}
			for _, name := range []string{"subtasks", "dod"} {
				array := properties[name].(map[string]any)
				item := array["items"].(map[string]any)
				delete(item["properties"].(map[string]any), "reuse")
				delete(item["properties"].(map[string]any), "remove")
				original := fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "minimum": 1}, "description": map[string]any{"type": "string"}, "remove": map[string]any{"type": "boolean"}}, []string{"reuse"})
				array["items"] = map[string]any{"anyOf": []any{item, original}}
			}
		}
		properties["batches"].(map[string]any)["items"] = map[string]any{"anyOf": []any{created, changed}}
		plan["required"] = []string{}
	}
	if input.Review != nil && (input.Review.Action != review.StartReadiness || input.DevelopmentDisabled) {
		delete(fields, "granularityReview")
	}
	if input.Handler == "engine.qa.design.v6" && fields["cases"] != nil {
		item := fields["cases"].(map[string]any)["items"].(map[string]any)
		items := []any{item}
		if _, design := role.(*Design); design {
			change, err := resultTypeSchema(reflect.TypeOf(CaseChange{}), true)
			if err != nil {
				return nil, err
			}
			change["properties"].(map[string]any)["case"] = map[string]any{"type": "integer", "minimum": 1}
			change["required"] = []string{"case"}
			items = append(items, change)
			shared := fields["sharedSteps"].(map[string]any)["items"].(map[string]any)
			step, err := resultTypeSchema(reflect.TypeOf(SharedStepChange{}), true)
			if err != nil {
				return nil, err
			}
			step["properties"].(map[string]any)["step"] = map[string]any{"type": "integer", "minimum": 1}
			step["required"] = []string{"step"}
			fields["sharedSteps"].(map[string]any)["items"] = map[string]any{"anyOf": []any{shared, step}}
		}
		for _, variant := range items {
			properties := variant.(map[string]any)["properties"].(map[string]any)
			if input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				for _, field := range []string{"publicEntry", "preconditions", "steps"} {
					delete(properties, field)
				}
			} else {
				delete(properties, "test")
			}
		}
		fields["cases"].(map[string]any)["items"] = map[string]any{"anyOf": items}
		if rows, ok := fields["coverage"].(map[string]any); ok {
			rows["items"] = map[string]any{"anyOf": []any{rows["items"], map[string]any{"type": "null"}}}
		}
	}
	if records, ok := fields["records"].(map[string]any); ok {
		records["items"] = map[string]any{"anyOf": []any{records["items"], map[string]any{"type": "null"}}}
	}
	if _, continuation := role.(*DevelopmentContinuation); continuation {
		for _, field := range []string{"summary", "remainingWork"} {
			fields[field] = map[string]any{"type": "string", "minLength": 1}
		}
	}
	if fields["interruptionReason"] != nil {
		fields["interruptionReason"] = map[string]any{"type": "string", "minLength": 1}
	}
	return body, nil
}

func completedSchema(input protocol.WorkerInput, target any) (map[string]any, error) {
	body, err := resultTypeSchema(reflect.TypeOf(target))
	if err != nil {
		return nil, err
	}
	fields := body["properties"].(map[string]any)
	if input.Handler == "engine.repair.v7" {
		if completionBatch(input) == nil {
			if points := standaloneRepairPoints(input); len(points) > 0 {
				fields["requirements"] = requirementEvidenceSchema(points)
				body["anyOf"] = []any{map[string]any{"required": []string{"granularityReview"}, "not": map[string]any{"required": []string{"requirements"}}}, map[string]any{"required": []string{"summary", "requirements"}, "not": map[string]any{"required": []string{"granularityReview"}}}}
			} else {
				delete(fields, "requirements")
			}
		} else {
			delete(fields, "requirements")
		}
		if cases := repairCaseIDs(input.Validation); len(cases) > 0 {
			fields["affectedCases"].(map[string]any)["items"] = indexSchema(cases)
		} else {
			delete(fields, "affectedCases")
		}
	}
	if plan, ok := fields["granularityReview"].(map[string]any); ok {
		batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
		properties := batch["properties"].(map[string]any)
		if len(input.RequirementPoints) > 0 {
			ids := []string{}
			for _, point := range input.RequirementPoints {
				ids = append(ids, point.ID)
			}
			properties["requirements"] = map[string]any{"type": "array", "items": indexSchema(ids), "uniqueItems": true, "description": "Select confirmed points assigned to this batch by their published positions."}
			batch["required"] = append(batch["required"].([]any), "requirements")
		} else {
			delete(properties, "requirements")
		}
		location := LocationSchema(false)
		delete(location["properties"].(map[string]any), "line")
		location["description"] = "Select one original read number or listing/entry numbers for the file. The program supplies its path."
		properties["artifacts"] = map[string]any{"type": "array", "items": location, "minItems": 1}
		command := map[string]any{"type": "string", "minLength": 1}
		purpose := map[string]any{"type": "string", "enum": protocol.DevelopmentCheckPurposes()}
		alternatives := []any{}
		if len(input.ApprovedChecks) > 0 {
			ids, commands := []string{}, []string{}
			for _, check := range input.ApprovedChecks {
				ids, commands = append(ids, check.ID), append(commands, check.Command)
			}
			alternatives = append(alternatives, fixedObject(map[string]any{"reuse": indexSchema(ids)}, []string{"reuse"}))
			command["not"] = map[string]any{"enum": commands}
			if input.Handler == "engine.repair.v7" {
				purpose["enum"] = []string{""}
			}
		}
		if proposesVerificationGoals(input) {
			alternatives = append(alternatives, fixedObject(map[string]any{"goal": map[string]any{"type": "string", "minLength": 1}, "purpose": map[string]any{"type": "string", "enum": protocol.DevelopmentCheckPurposes()}}, []string{"goal"}))
		} else {
			alternatives = append(alternatives, fixedObject(map[string]any{"command": command, "purpose": purpose}, []string{"command"}))
		}
		check := map[string]any{"anyOf": alternatives, "description": "Reuse an original approved check or author a genuinely new local invocation."}
		if proposesVerificationGoals(input) {
			check["description"] = "Reuse an original approved check or declare a missing verification goal for the developer to prepare."
		}
		properties["checks"] = map[string]any{"type": "array", "items": check}
		delete(properties, "route")
		if len(input.AvailableRouteCandidates) > 0 {
			properties["route"] = map[string]any{"anyOf": []any{
				fixedObject(map[string]any{"mode": map[string]any{"type": "string", "enum": []string{"full"}}}, []string{"mode"}),
				fixedObject(map[string]any{
					"mode":     map[string]any{"type": "string", "enum": []string{"custom"}},
					"selected": map[string]any{"type": "array", "items": indexSchema(input.AvailableRouteCandidates), "minItems": 1, "uniqueItems": true},
				}, []string{"mode", "selected"}),
			}}
		}
		for _, name := range []string{"subtasks", "dod"} {
			delete(properties[name].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any), "reuse")
			delete(properties[name].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any), "remove")
		}
		delete(properties, "remove")
		delete(properties, "reuse")
		if input.ApprovedPlan != nil && len(input.ApprovedPlan.Batches) > 0 {
			changed := maps.Clone(batch)
			changed["properties"] = maps.Clone(properties)
			changed["properties"].(map[string]any)["reuse"] = map[string]any{"type": "integer", "minimum": 1, "maximum": len(input.ApprovedPlan.Batches)}
			for _, name := range []string{"subtasks", "dod"} {
				array := maps.Clone(properties[name].(map[string]any))
				changed["properties"].(map[string]any)[name] = array
				item := array["items"].(map[string]any)
				original := fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "minimum": 1}, "description": map[string]any{"type": "string", "minLength": 1}, "remove": map[string]any{"type": "boolean"}}, []string{"reuse"})
				array["items"] = map[string]any{"anyOf": []any{item, original}}
			}
			changed["properties"].(map[string]any)["remove"] = map[string]any{"type": "boolean"}
			plan["required"] = []any{}
			plan["description"] = "Change only new semantics of the published approvedPlan; omitted fields and original batches are retained. Explicit batches [] clears the list; reuse with remove:true removes an original batch."
			changed["required"] = []any{"reuse"}
			changed["description"] = "Select an original confirmed batch and supply only changed semantics. Omitted content, identities, dependencies, checks and route are carried by the program."
			plan["properties"].(map[string]any)["batches"].(map[string]any)["items"] = map[string]any{"anyOf": []any{batch, changed}}
		}
		for _, name := range []string{"subtasks", "dod"} {
			array := maps.Clone(properties[name].(map[string]any))
			array["minItems"] = 1
			properties[name] = array
		}
		if input.Handler != "engine.repair.v7" || !slices.ContainsFunc(input.ApprovedChecks, func(check protocol.DevelopmentCheck) bool {
			return check.Purpose == protocol.CheckFullBuild || check.Purpose == protocol.CheckFullTests || check.Purpose == protocol.CheckEdit
		}) {
			checks := maps.Clone(properties["checks"].(map[string]any))
			checks["minItems"] = 1
			properties["checks"] = checks
			batch["required"] = append(batch["required"].([]any), "checks")
		}
		artifacts := maps.Clone(properties["artifacts"].(map[string]any))
		delete(artifacts, "minItems")
		properties["artifacts"] = artifacts
		batch["anyOf"] = []any{
			map[string]any{"required": []string{"artifacts"}, "properties": map[string]any{"artifacts": map[string]any{"minItems": 1}}},
			map[string]any{"required": []string{"newArtifacts"}, "properties": map[string]any{"newArtifacts": map[string]any{"minItems": 1}}},
		}
	}
	if input.Handler == "engine.development.batch.v6" {
		pending, positions, commands := pendingDevelopmentCommands(input)
		duties := []any{}
		for _, check := range pending {
			item := map[string]any{"type": "string", "minLength": 1, "description": check.Goal}
			duties = append(duties, item)
		}
		if len(duties) > 0 {
			fields["commands"] = orderedSchema(duties)
			_, continuation := target.(*DevelopmentContinuation)
			if !continuation && slices.ContainsFunc(positions, func(position int) bool { return commands[position] == "" }) {
				body["required"] = append(body["required"].([]any), "commands")
			}
		} else {
			delete(fields, "commands")
		}
	}
	if input.Handler == "engine.gate.v3" {
		items := []any{}
		for _, point := range input.RequirementPoints {
			item := fixedObject(map[string]any{
				"status":   map[string]any{"type": "string", "enum": []string{protocol.OutcomePass, protocol.OutcomeFail}},
				"evidence": map[string]any{"type": "string", "minLength": 1},
				"findings": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}, "uniqueItems": true},
			}, []string{"status", "evidence"})
			item["description"] = requirementDescription(point)
			items = append(items, item)
		}
		fields["requirements"] = orderedSchema(items)
		if len(items) > 0 {
			body["required"] = append(body["required"].([]any), "requirements")
		} else {
			delete(fields, "requirements")
		}
	}
	if input.Review != nil {
		if input.Review.Action != review.StartReadiness || input.DevelopmentDisabled {
			delete(fields, "granularityReview")
		}
		if input.Review.Action == review.StartReadiness {
			finding := fields["findings"].(map[string]any)["items"].(map[string]any)
			finding["required"] = append(finding["required"].([]any), "category")
			choices := []any{}
			for _, category := range review.TechnicalCategories() {
				severities := []string{}
				for _, severity := range review.Severities() {
					if review.ValidateFindingClassification(input.Review.Action, category, severity) == nil {
						severities = append(severities, severity)
					}
				}
				choices = append(choices, map[string]any{"properties": map[string]any{"category": map[string]any{"const": category}, "severity": map[string]any{"enum": severities}}})
			}
			finding["anyOf"] = choices
		}
		return body, nil
	}
	if input.Handler == "engine.validation.observation.v2" {
		if input.Validation.Observation == nil {
			return nil, fmt.Errorf("semantic worker lacks current issued duties")
		}
		findings, scopes, impacts, suggestions := []any{}, []any{}, []any{}, []any{}
		observation := input.Validation.Observation
		candidateDescriptions := []string{}
		for index, source := range observation.Problems {
			candidateDescriptions = append(candidateDescriptions, fmt.Sprintf("%d = existing problem input.validation.observation.problems[%d] (%s); judge its findings, including their original root-cause history, target, evidence and failedCases", index+1, index, source.ID))
		}
		for index, source := range observation.Findings {
			candidateDescriptions = append(candidateDescriptions, fmt.Sprintf("%d = issued finding input.validation.observation.findings[%d] (%s); judge its finding, target, evidence and failedCases", len(observation.Problems)+index+1, index, source.Finding.ID))
		}
		for index, source := range observation.Findings {
			item, err := resultTypeSchema(reflect.TypeOf(FindingJudgment{}))
			if err != nil {
				return nil, err
			}
			item["description"] = "Judge only this issued finding: " + source.Finding.ID
			properties := item["properties"].(map[string]any)
			properties["decision"] = map[string]any{"type": "string", "enum": []string{"NEW", "SAME_AS", "DISMISSED"}}
			candidates := []int{}
			for candidate := 1; candidate <= len(observation.Problems)+len(observation.Findings); candidate++ {
				if candidate != len(observation.Problems)+index+1 {
					candidates = append(candidates, candidate)
				}
			}
			if len(candidates) == 0 {
				delete(properties, "sameAs")
				properties["decision"] = map[string]any{"type": "string", "enum": []string{"NEW", "DISMISSED"}}
			} else {
				properties["sameAs"] = map[string]any{"type": "integer", "enum": candidates, "description": "Select only a candidate number; the program supplies its actual problem ID. A selected issued finding must be judged NEW in this result. Omit unless decision is SAME_AS.\n" + strings.Join(candidateDescriptions, "\n")}
			}
			if source.Finding.ExecutionFailure != nil {
				delete(properties, "severity")
				delete(properties, "severityRationale")
				delete(properties["verification"].(map[string]any)["properties"].(map[string]any), "severity")
			} else {
				properties["severity"] = map[string]any{"type": "string", "minLength": 1, "enum": review.Severities()}
				properties["severityRationale"] = map[string]any{"type": "string", "minLength": 1}
				item["required"] = append(item["required"].([]any), "severity", "severityRationale")
				verification := properties["verification"].(map[string]any)
				verification["properties"].(map[string]any)["severity"] = map[string]any{"type": "string", "minLength": 1}
				verification["required"] = append(verification["required"].([]any), "severity")
			}
			findings = append(findings, item)
		}
		for _, source := range input.Validation.Observation.Scopes {
			item, err := resultTypeSchema(reflect.TypeOf(ScopeJudgment{}))
			if err != nil {
				return nil, err
			}
			item["description"] = "Judge only this issued QA scope: " + string(source.Mode)
			item["properties"].(map[string]any)["cases"].(map[string]any)["items"] = indexSchema(source.ApprovedCases)
			scopes = append(scopes, item)
		}
		for _, source := range input.Validation.Observation.GateImpacts {
			item, err := resultTypeSchema(reflect.TypeOf(ImpactJudgment{}))
			if err != nil {
				return nil, err
			}
			item["description"] = "Judge only this issued gate impact: " + source.Gate
			item["properties"].(map[string]any)["decision"] = map[string]any{"type": "string", "enum": []string{"INHERIT", "RERUN"}}
			impacts = append(impacts, item)
		}
		for _, source := range input.Validation.Observation.Suggestions {
			item, err := resultTypeSchema(reflect.TypeOf(SuggestionJudgment{}))
			if err != nil {
				return nil, err
			}
			item["description"] = "Judge only this issued optional QA suggestion: " + source.Suggestion.ID
			properties := item["properties"].(map[string]any)
			properties["decision"] = map[string]any{"type": "string", "enum": []string{"ACCEPT", "DEFER", "DECLINE"}}
			cases := []string{}
			for _, entry := range source.Manifest.Cases {
				cases = append(cases, entry.CaseID)
			}
			properties["affectedCases"].(map[string]any)["items"] = indexSchema(cases)
			suggestions = append(suggestions, item)
		}
		if len(suggestions) == 0 {
			delete(fields, "suggestions")
		} else {
			body["required"] = append(body["required"].([]any), "suggestions")
		}
		for name, items := range map[string][]any{"findings": findings, "scopes": scopes, "gateImpacts": impacts, "suggestions": suggestions} {
			if name == "suggestions" && len(items) == 0 {
				continue
			}
			fields[name] = orderedSchema(items)
		}
		return body, nil
	}

	if batch := completionBatch(input); batch != nil && fields["completion"] != nil {
		if input.Handler == "engine.repair.v7" {
			fields["summary"] = map[string]any{"type": "string", "minLength": 1}
			body["required"] = append(body["required"].([]any), "summary")
		}
		completion := fields["completion"].(map[string]any)["properties"].(map[string]any)
		items := []any{}
		for _, criterion := range batch.DoD {
			items = append(items, map[string]any{"type": "string", "minLength": 1, "description": criterion.Description})
		}
		completion["dod"] = orderedSchema(items)
		completion["requirements"] = requirementEvidenceSchema(input.RequirementPoints)
		if len(batch.RequirementPoints) > 0 {
			object := fields["completion"].(map[string]any)
			object["required"] = append(object["required"].([]any), "requirements")
		}
	} else if !(input.Handler == "engine.repair.v7" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Repair.FinalCandidate == nil) {
		delete(fields, "completion")
	}
	if input.Handler == "engine.repair.verify.v2" {
		item, err := resultTypeSchema(reflect.TypeOf(Observation{}))
		if err != nil {
			return nil, err
		}
		fields["records"] = comparisonSchema(input.Validation.Repair.VerificationCases, item, false)
	}
	if input.Handler == "engine.qa.design.v6" {
		whitebox := input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox
		if len(input.Validation.AffectedCases) > 0 {
			cases := []any{}
			for _, id := range input.Validation.AffectedCases {
				item, err := resultTypeSchema(reflect.TypeOf(CaseChange{}))
				if err != nil {
					return nil, err
				}
				item["description"] = "Change only this issued case: " + id
				for _, original := range input.Validation.Manifest.Cases {
					if original.CaseID == id {
						item["properties"].(map[string]any)["operations"], err = operationRevisionSchema(original.Operations)
						if err != nil {
							return nil, err
						}
					}
				}
				if whitebox {
					for _, field := range []string{"publicEntry", "preconditions", "steps"} {
						delete(item["properties"].(map[string]any), field)
					}
				} else {
					delete(item["properties"].(map[string]any), "test")
				}
				cases = append(cases, item)
			}
			fields["cases"] = orderedSchema(cases)
			shared := []any{}
			for _, step := range revisionSharedSteps(input.Validation) {
				item, err := resultTypeSchema(reflect.TypeOf(SharedStepChange{}))
				if err != nil {
					return nil, err
				}
				item["description"] = "Change only this issued shared instruction: " + step.ID
				item["properties"].(map[string]any)["artifacts"] = stringRevisionSchema(step.Artifacts)
				item["properties"].(map[string]any)["operations"], err = operationRevisionSchema(step.Operations)
				if err != nil {
					return nil, err
				}
				shared = append(shared, item)
			}
			fields["sharedSteps"] = orderedSchema(shared)
			return body, nil
		}
		cases, shared := [][]coverage.Operation{}, [][]coverage.Operation{}
		for _, item := range input.Validation.Manifest.Cases {
			cases = append(cases, item.Operations)
		}
		for _, item := range input.Validation.Manifest.SharedSteps {
			shared = append(shared, item.Operations)
		}
		for _, family := range []struct {
			field, selection string
			change           reflect.Type
			operations       [][]coverage.Operation
		}{
			{"cases", "case", reflect.TypeOf(CaseChange{}), cases},
			{"sharedSteps", "step", reflect.TypeOf(SharedStepChange{}), shared},
		} {
			if len(family.operations) == 0 {
				continue
			}
			array := fields[family.field].(map[string]any)
			original := array["items"].(map[string]any)
			delete(original["properties"].(map[string]any), family.selection)
			alternatives := []any{original}
			for index, operations := range family.operations {
				change, err := resultTypeSchema(family.change)
				if err != nil {
					return nil, err
				}
				properties := change["properties"].(map[string]any)
				delete(properties, "remove")
				properties[family.selection] = map[string]any{"type": "integer", "enum": []int{index + 1}}
				if family.field == "sharedSteps" {
					properties["artifacts"] = stringRevisionSchema(input.Validation.Manifest.SharedSteps[index].Artifacts)
				}
				properties["operations"], err = operationRevisionSchema(operations)
				if err != nil {
					return nil, err
				}
				change["required"] = []any{family.selection}
				change["description"] = "Select this existing item and supply only changed semantic fields; omitted values retain original content."
				if family.field == "cases" {
					if whitebox {
						for _, field := range []string{"publicEntry", "preconditions", "steps"} {
							delete(properties, field)
						}
					} else {
						delete(properties, "test")
					}
				}
				alternatives = append(alternatives, change)
			}
			array["items"] = map[string]any{"anyOf": alternatives}
		}
		item := fields["cases"].(map[string]any)["items"].(map[string]any)
		if branches, ok := item["anyOf"].([]any); ok {
			item = branches[0].(map[string]any)
		}
		if whitebox {
			for _, field := range []string{"publicEntry", "preconditions", "steps"} {
				delete(item["properties"].(map[string]any), field)
			}
			item["properties"].(map[string]any)["operations"].(map[string]any)["minItems"] = 1
			item["oneOf"] = []any{
				map[string]any{"required": []string{"test"}, "not": map[string]any{"required": []string{"operations"}}},
				map[string]any{"required": []string{"operations"}, "not": map[string]any{"required": []string{"test"}}},
			}
		} else {
			delete(item["properties"].(map[string]any), "test")
		}

		items := []any{}
		for _, point := range input.Validation.Manifest.Points {
			items = append(items, map[string]any{"anyOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}, "description": "Choose cases covering: " + point.ObservableBehavior}}})
		}
		fields["coverage"] = orderedSchema(items)
	}
	if input.Handler == "engine.qa.review.rationale.v2" {
		sources, points, cases, shared := slices.Clone(input.Validation.Manifest.Sources), []string{}, []string{}, []string{}
		for _, item := range input.Validation.Manifest.Points {
			points = append(points, item.PointID)
		}
		for _, item := range input.Validation.Manifest.Cases {
			cases = append(cases, item.CaseID)
		}
		for _, item := range input.Validation.Manifest.SharedSteps {
			shared = append(shared, item.ID)
		}
		decision, err := resultTypeSchema(reflect.TypeOf(Decision{}))
		if err != nil {
			return nil, err
		}
		fields["sourceDecisions"] = decisionSchema(sources, nil, decision)
		fields["pointDecisions"] = decisionSchema(points, nil, decision)
		fields["caseDecisions"] = decisionSchema(cases, input.Validation.InheritableReviews, decision)
		fields["sharedStepDecisions"] = decisionSchema(shared, input.Validation.InheritableSharedReviews, decision)
	}
	if input.Handler == "engine.qa.execution.v4" && len(input.Validation.CommandCases) > 0 {
		operation, err := newOperationSchema()
		if err != nil {
			return nil, err
		}
		item := map[string]any{"type": "array", "items": operation}
		items := []any{}
		for index, id := range input.Validation.CommandCases {
			entry := maps.Clone(item)
			entry["description"] = "Supply only the invocation for approved case " + id
			if len(input.Validation.CommandSources) > 0 && strings.HasPrefix(input.Validation.CommandSources[index], "shared:") {
				entry["description"] = "Supply only the invocation for approved shared preparation " + strings.TrimPrefix(input.Validation.CommandSources[index], "shared:") + " referenced by case " + id
			}
			entry["minItems"] = 1
			items = append(items, entry)
		}
		fields["commands"] = orderedSchema(items)
		return body, nil
	}
	if input.Handler == "engine.qa.execution.v4" {
		item, err := resultTypeSchema(reflect.TypeOf(Observation{}))
		if err != nil {
			return nil, err
		}
		fields["records"] = comparisonSchema(executionCaseIDs(input.Validation), item, true)
	}

	return body, nil
}

func resultTypeSchema(value reflect.Type, stable ...bool) (map[string]any, error) {
	if value == reflect.TypeOf(Location{}) {
		return LocationSchema(true), nil
	}
	if value == reflect.TypeOf(StringChange{}) {
		if len(stable) > 0 && stable[0] {
			return stringSourceSchema(fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "minimum": 1}}, []string{"reuse"})), nil
		}
		return stringSourceSchema(), nil
	}
	if value == reflect.TypeOf(ArgumentChange{}) {
		if len(stable) > 0 && stable[0] {
			return map[string]any{"anyOf": []any{map[string]any{"type": "string"}, fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "minimum": 1}}, []string{"reuse"})}}, nil
		}
		return map[string]any{"type": "string"}, nil
	}
	if value == reflect.TypeOf(coverage.Operation{}) {
		return newOperationSchema(stable...)
	}
	var literals []string
	switch value {
	case reflect.TypeOf(coverage.OperationKind("")):
		literals = coverage.OperationKinds()
	case reflect.TypeOf(coverage.ReviewKind("")):
		literals = coverage.ReviewKinds()
	case reflect.TypeOf(coverage.DecisionStatus("")):
		literals = coverage.DecisionStatuses()
	case reflect.TypeOf(coverage.ExecutionStatus("")):
		literals = coverage.ExecutionStatuses()
	}
	if literals != nil {
		return map[string]any{"type": "string", "enum": literals}, nil
	}
	if value.Kind() == reflect.Pointer {
		return resultTypeSchema(value.Elem(), stable...)
	}
	switch value.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Slice, reflect.Array:
		item, err := resultTypeSchema(value.Elem(), stable...)
		return map[string]any{"type": "array", "items": item}, err
	case reflect.Map:
		if value.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("worker result map keys must be strings")
		}
		item, err := resultTypeSchema(value.Elem(), stable...)
		return map[string]any{"type": "object", "additionalProperties": item}, err
	case reflect.Struct:
		properties := map[string]any{}
		required := []any{}
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)

			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			child, err := resultTypeSchema(field.Type, stable...)
			if value == reflect.TypeOf(Check{}) && field.Name == "Purpose" {
				child = map[string]any{"type": "string", "enum": protocol.DevelopmentCheckPurposes()}
			}
			if field.Name == "Decision" {
				switch value {
				case reflect.TypeOf(FindingJudgment{}):
					child["enum"] = []string{"NEW", "SAME_AS", "DISMISSED"}
				case reflect.TypeOf(ImpactJudgment{}):
					child["enum"] = []string{"INHERIT", "RERUN"}
				case reflect.TypeOf(SuggestionJudgment{}):
					child["enum"] = []string{"ACCEPT", "DEFER", "DECLINE"}
				}
			}
			if err != nil {
				return nil, err
			}
			if field.Anonymous && name == "" {
				for key, item := range child["properties"].(map[string]any) {
					properties[key] = item
				}
				required = append(required, child["required"].([]any)...)
				continue
			}
			if name == "" {
				name = field.Name
			}
			if (value == reflect.TypeOf(Case{}) || value == reflect.TypeOf(DesignedCase{}) || value == reflect.TypeOf(CaseChange{})) && field.Name == "Scenario" {
				child["enum"] = coverage.Scenarios()
			}
			if value == reflect.TypeOf(Finding{}) && field.Name == "Severity" {
				child["enum"] = review.Severities()
			}
			if value == reflect.TypeOf(Suggestion{}) && field.Name == "Severity" {
				child["enum"] = []string{"P2", "P3"}
			}
			if value == reflect.TypeOf(DesignedSharedStep{}) && field.Name == "Artifacts" {
				child = stringRevisionSchema(nil)
			}
			if field.Name == "Files" {
				location := LocationSchema(false)
				delete(location["properties"].(map[string]any), "line")
				location["description"] = "Select original read or listing/entry numbers for related test files. Omit files to retain an issued relation; an empty array clears it. The program supplies paths."
				child["items"] = location
			}
			if value == reflect.TypeOf(TestSelection{}) {
				child["minimum"] = 1
			}
			if field.Name == "Test" {
				child["description"] = "Select the original read and its published Go symbol number. The program supplies the exact reference and includes its file; omit to retain an issued reference."
			}

			properties[name] = child
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
		}
		if value == reflect.TypeOf(Observation{}) {
			properties["result"] = map[string]any{"type": "string", "enum": []string{string(coverage.ExecutedPass), string(coverage.ExecutedFail)}}
			properties["comparison"] = map[string]any{"type": "string", "minLength": 1}
		}
		if value == reflect.TypeOf(Granularity{}) {
			tiers := []any{}
			for _, tier := range protocol.GranularityVerificationTiers() {
				tiers = append(tiers, tier)
			}
			properties["verificationTier"] = map[string]any{"type": "string", "enum": tiers}
			return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required, "description": "Propose semantic batch boundaries; omit IDs. Dependencies and checkpointGroups select 1-based batch positions; owner is 0 for master merge or a positive slice number. FINAL/CHECKPOINT, approved purposes and complete-project checks retain their issued meaning."}, nil
		}

		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}, nil
	}
	return nil, fmt.Errorf("worker result type %s has no supported native representation", value)
}

// Completion keys come only from the issued member batch. Final-candidate
// verification and repair planning have different evidence duties.
func completionBatch(input protocol.WorkerInput) *protocol.DevelopmentBatch {
	if input.Validation == nil || input.Validation.Development == nil || input.Validation.Development.Batch.ID == "" || (input.Handler != "engine.development.batch.v6" && input.Handler != "engine.repair.v7") {
		return nil
	}
	if input.Validation.Repair != nil && input.Validation.Repair.FinalCandidate != nil {
		return nil
	}
	return &input.Validation.Development.Batch
}

func fixedObject(properties map[string]any, required []string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
func decisionSchema(ids []string, inherited []coverage.ReviewDecision, decision map[string]any) map[string]any {
	items := []any{}
	for _, id := range ids {
		if !slices.ContainsFunc(inherited, func(item coverage.ReviewDecision) bool { return item.ID == id }) {
			items = append(items, decision)
		}
	}
	return orderedSchema(items)
}
func orderedSchema(items []any) map[string]any {
	schema := map[string]any{"type": "array", "items": false, "minItems": len(items), "maxItems": len(items)}
	if len(items) > 0 {
		schema["prefixItems"] = items
	}
	return schema
}
func requirementDescription(point requirement.Point) string {
	return "Confirmed requirement: " + point.Requirement + "\nPoint: " + point.Text + "\nOracle: " + point.Oracle + "\nSource: " + point.Source
}
func requirementEvidenceSchema(points []requirement.Point) map[string]any {
	items := []any{}
	for _, point := range points {
		items = append(items, map[string]any{"type": "string", "minLength": 1, "description": requirementDescription(point)})
	}
	return orderedSchema(items)
}
func standaloneRepairPoints(input protocol.WorkerInput) []requirement.Point {
	if input.Validation == nil || input.Validation.Repair == nil {
		return nil
	}
	return input.Validation.Repair.RequiredPoints
}
func comparisonSchema(ids []string, item map[string]any, partial bool) map[string]any {
	items := []any{}
	for _, id := range ids {
		value := maps.Clone(item)
		value["description"] = "Compare the issued case: " + id
		if partial {
			value = map[string]any{"anyOf": []any{value, map[string]any{"type": "null"}}}
		}
		items = append(items, value)
	}
	return orderedSchema(items)
}
func indexSchema(ids []string) map[string]any {
	indices := []int{}
	for index := range ids {
		indices = append(indices, index+1)
	}
	return map[string]any{"type": "integer", "enum": indices, "description": "Select the 1-based issued item number; the program supplies its actual ID."}
}

func stringRevisionSchema(original []string) map[string]any {
	source := stringSourceSchema()
	if len(original) > 0 {
		positions := []int{}
		for index := range original {
			positions = append(positions, index+1)
		}
		source = stringSourceSchema(fixedObject(map[string]any{"reuse": map[string]any{"type": "integer", "enum": positions}}, []string{"reuse"}))
	}
	return map[string]any{"type": "array", "items": source, "description": "Omit this field to retain the original array. An explicit array defines its complete final order: select original element positions, original read bytes or new text; [] clears the array."}
}

func operationRevisionSchema(original []coverage.Operation) (map[string]any, error) {
	created, err := newOperationSchema()
	if err != nil {
		return nil, err
	}
	items := []any{created}
	for index, operation := range original {
		changed, err := resultTypeSchema(reflect.TypeOf(OperationChange{}))
		if err != nil {
			return nil, err
		}
		properties := changed["properties"].(map[string]any)
		properties["reuse"] = map[string]any{"type": "integer", "enum": []int{index + 1}, "description": "Select this 1-based original operation; the program carries its original argv, command and stdin."}
		arguments := []any{map[string]any{"type": "string"}}
		if len(operation.Argv) > 0 {
			arguments = append(arguments, fixedObject(map[string]any{"reuse": indexSchema(operation.Argv)}, []string{"reuse"}))
		}
		properties["argv"] = map[string]any{"type": "array", "items": map[string]any{"anyOf": arguments}, "description": "Retain original arguments by their published positions or author new argument values; omitted argv retains the complete original array."}
		changed["required"] = []any{"reuse"}
		items = append(items, changed)
	}
	return map[string]any{"type": "array", "items": map[string]any{"anyOf": items}, "description": "Omit operations to retain the original sequence. An explicit array defines the complete final order: reuse original positions and supply only changed fields, add complete new operations, or omit original positions to delete them; [] clears the sequence. Changing kind clears incompatible original argv or command values; supply the new invocation."}, nil
}

func stringSourceSchema(reused ...map[string]any) map[string]any {
	position := map[string]any{"type": "integer", "minimum": 1}
	source := fixedObject(map[string]any{"read": position, "from": position, "to": position}, []string{"read", "from", "to"})
	columns := fixedObject(map[string]any{"read": position, "from": position, "to": position, "fromColumn": position, "toColumn": position}, []string{"read", "from", "to", "fromColumn", "toColumn"})
	columns["description"] = "Paired 1-based Unicode columns on the first/last selected line, inclusive; CR/LF count as original characters."
	created := map[string]any{"type": "string"}
	parts := fixedObject(map[string]any{"parts": map[string]any{"type": "array", "items": map[string]any{"anyOf": []any{created, source, columns}}}}, []string{"parts"})
	parts["description"] = "Join these original selections and newly authored separators in order, without trimming or rewriting any selected bytes."
	items := []any{created, source, columns, parts}
	if len(reused) > 0 {
		parts["properties"].(map[string]any)["parts"].(map[string]any)["items"].(map[string]any)["anyOf"] = append([]any{created, source, columns}, reused[0])
		items = append(items, reused[0])
	}
	return map[string]any{"anyOf": items, "description": "Author new text, select original read/from/to lines with optional paired fromColumn/toColumn, or join parts. Selected bytes retain Unicode, line endings and missing final newlines; no shell parsing or trimming."}
}
func newOperationSchema(stable ...bool) (map[string]any, error) {
	result, err := resultTypeSchema(reflect.TypeOf(OperationChange{}), stable...)
	if err != nil {
		return nil, err
	}
	delete(result["properties"].(map[string]any), "reuse")
	result["required"] = []any{"kind"}
	return result, nil
}
