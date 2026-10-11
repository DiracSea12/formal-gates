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
)

// ResultSchema contains the role's semantic body and a closed inability
// alternative. Issued identities and completed transport outcome are not model fields.
func ResultSchema(input protocol.WorkerInput) (map[string]any, error) {
	body, err := completedSchema(input)
	if err != nil {
		return nil, err
	}
	interruption := fixedObject(map[string]any{"interruptionReason": map[string]any{"type": "string", "minLength": 1}}, []string{"interruptionReason"})
	if input.Handler == "engine.qa.execution.v2" {
		fields := interruption["properties"].(map[string]any)
		fields["records"] = body["properties"].(map[string]any)["records"]
		fields["findings"] = body["properties"].(map[string]any)["findings"]
	}
	alternatives := []any{body, interruption}
	if input.Validation != nil && (input.Handler == "engine.development.batch.v2" || input.Handler == "engine.repair.v2") {
		partial, err := resultTypeSchema(reflect.TypeOf(DevelopmentContinuation{}))
		if err != nil {
			return nil, err
		}
		for _, field := range []string{"summary", "remainingWork"} {
			partial["properties"].(map[string]any)[field] = map[string]any{"type": "string", "minLength": 1}
		}
		alternatives = append(alternatives, partial)
	}
	if input.Handler == "engine.repair.v2" && completionBatch(input) == nil && body["properties"].(map[string]any)["completion"] != nil {
		fields := body["properties"].(map[string]any)
		completed := maps.Clone(body)
		completedFields := maps.Clone(fields)
		completed["properties"] = completedFields
		completed["required"] = append(slices.Clone(body["required"].([]any)), "granularityReview", "completion", "summary")
		completedFields["summary"] = map[string]any{"type": "string", "minLength": 1}
		plan := maps.Clone(fields["granularityReview"].(map[string]any))
		planFields := maps.Clone(plan["properties"].(map[string]any))
		batches := maps.Clone(planFields["batches"].(map[string]any))
		batches["minItems"], batches["maxItems"] = 1, 1
		batch := maps.Clone(batches["items"].(map[string]any))
		if alternatives, ok := batch["anyOf"].([]any); ok {
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
	return map[string]any{"anyOf": alternatives}, nil
}

func completedSchema(input protocol.WorkerInput) (map[string]any, error) {
	target, err := semanticRole(input)
	if err != nil {
		return nil, err
	}
	body, err := resultTypeSchema(reflect.TypeOf(target))
	if err != nil {
		return nil, err
	}
	fields := body["properties"].(map[string]any)
	if input.Handler == "engine.repair.v2" {
		if cases := repairCaseIDs(input.Validation); len(cases) > 0 {
			fields["affectedCases"].(map[string]any)["items"] = indexSchema(cases)
		} else {
			delete(fields, "affectedCases")
		}
	}
	if plan, ok := fields["granularityReview"].(map[string]any); ok {
		batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
		properties := batch["properties"].(map[string]any)
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
			if input.Handler == "engine.repair.v2" {
				purpose["enum"] = []string{""}
			}
		}
		alternatives = append(alternatives, fixedObject(map[string]any{"command": command, "purpose": purpose}, []string{"command"}))
		check := alternatives[0]
		if len(alternatives) > 1 {
			check = map[string]any{"anyOf": alternatives}
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
	}
	if input.Review != nil {
		if input.Review.Action != review.StartReadiness {
			delete(fields, "granularityReview")
		}
		verification := []any{}
		for _, key := range review.RequiredVerificationChecks() {
			verification = append(verification, map[string]any{"type": "string", "minLength": 1, "description": "Evidence for " + key})
		}
		fields["verification"] = orderedSchema(verification)
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
				properties["severity"] = map[string]any{"type": "string", "minLength": 1, "enum": []string{"P0", "P1", "P2", "P3"}}
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
			fields[name] = map[string]any{"type": "array", "prefixItems": items, "items": false, "minItems": len(items), "maxItems": len(items)}
		}
		return body, nil
	}

	if batch := completionBatch(input); batch != nil {
		if input.Handler == "engine.repair.v2" {
			fields["summary"] = map[string]any{"type": "string", "minLength": 1}
			body["required"] = append(body["required"].([]any), "summary")
		}
		completion := fields["completion"].(map[string]any)["properties"].(map[string]any)
		items := []any{}
		for _, criterion := range batch.DoD {
			items = append(items, map[string]any{"type": "string", "minLength": 1, "description": criterion.Description})
		}
		completion["dod"] = orderedSchema(items)
	} else if !(input.Handler == "engine.repair.v2" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Repair.FinalCandidate == nil) {
		delete(fields, "completion")
	}
	if input.Handler == "engine.repair.verify.v2" {
		item, err := resultTypeSchema(reflect.TypeOf(Observation{}))
		if err != nil {
			return nil, err
		}
		fields["records"] = comparisonSchema(input.Validation.Repair.VerificationCases, item, false)
	}
	if input.Handler == "engine.qa.design.v2" {
		whitebox := input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox
		if len(input.Validation.AffectedCases) > 0 {
			cases := []any{}
			for _, id := range input.Validation.AffectedCases {
				item, err := resultTypeSchema(reflect.TypeOf(CaseChange{}))
				if err != nil {
					return nil, err
				}
				item["description"] = "Change only this issued case: " + id
				for _, family := range []struct {
					field, selection string
					count            int
					change           reflect.Type
				}{
					{"cases", "case", len(input.Validation.Manifest.Cases), reflect.TypeOf(CaseChange{})},
					{"sharedSteps", "step", len(input.Validation.Manifest.SharedSteps), reflect.TypeOf(SharedStepChange{})},
				} {
					if family.count == 0 {
						continue
					}
					array := fields[family.field].(map[string]any)
					original := array["items"].(map[string]any)
					change, err := resultTypeSchema(family.change)
					if err != nil {
						return nil, err
					}
					properties := change["properties"].(map[string]any)
					delete(properties, "remove")
					properties[family.selection] = map[string]any{"type": "integer", "minimum": 1, "maximum": family.count}
					change["required"] = []any{family.selection}
					change["description"] = "Select an existing item and supply only changed semantic fields; omitted values retain original content."
					if !whitebox && family.field == "cases" {
						delete(properties, "testFile")
						delete(properties, "testSymbol")
					}
					array["items"] = map[string]any{"anyOf": []any{original, change}}
				}
				if !whitebox {
					delete(item["properties"].(map[string]any), "testFile")
					delete(item["properties"].(map[string]any), "testSymbol")
				}
				cases = append(cases, item)
			}
			fields["cases"] = map[string]any{"type": "array", "prefixItems": cases, "items": false, "minItems": len(cases), "maxItems": len(cases)}
			shared := []any{}
			for _, step := range revisionSharedSteps(input.Validation) {
				item, err := resultTypeSchema(reflect.TypeOf(SharedStepChange{}))
				if err != nil {
					return nil, err
				}
				item["description"] = "Change only this issued shared instruction: " + step.ID
				shared = append(shared, item)
			}
			fields["sharedSteps"] = map[string]any{"type": "array", "prefixItems": shared, "items": false, "minItems": len(shared), "maxItems": len(shared)}
			return body, nil
		}
		if !whitebox {
			item := fields["cases"].(map[string]any)["items"].(map[string]any)
			if branches, ok := item["anyOf"].([]any); ok {
				item = branches[0].(map[string]any)
			}
			delete(item["properties"].(map[string]any), "testFile")
			delete(item["properties"].(map[string]any), "testSymbol")
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
	if input.Handler == "engine.qa.execution.v2" && len(input.Validation.CommandCases) > 0 {
		item := fields["commands"].(map[string]any)["items"]
		items := []any{}
		for _, id := range input.Validation.CommandCases {
			entry := maps.Clone(item.(map[string]any))
			entry["description"] = "Supply only the invocation for approved case " + id
			entry["minItems"] = 1
			items = append(items, entry)
		}
		fields["commands"] = orderedSchema(items)
		return body, nil
	}
	if input.Handler == "engine.qa.execution.v2" {
		item, err := resultTypeSchema(reflect.TypeOf(Observation{}))
		if err != nil {
			return nil, err
		}
		fields["records"] = comparisonSchema(executionCaseIDs(input.Validation), item, true)
	}

	return body, nil
}

func resultTypeSchema(value reflect.Type) (map[string]any, error) {
	if value == reflect.TypeOf(Location{}) {
		return LocationSchema(true), nil
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
		return resultTypeSchema(value.Elem())
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
		item, err := resultTypeSchema(value.Elem())
		return map[string]any{"type": "array", "items": item}, err
	case reflect.Map:
		if value.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("worker result map keys must be strings")
		}
		item, err := resultTypeSchema(value.Elem())
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
			child, err := resultTypeSchema(field.Type)
			if value == reflect.TypeOf(Check{}) && field.Name == "Purpose" {
				child = map[string]any{"type": "string", "enum": protocol.DevelopmentCheckPurposes()}
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
			if (value == reflect.TypeOf(Case{}) || value == reflect.TypeOf(DesignedCase{})) && field.Name == "Scenario" {
				child["enum"] = coverage.Scenarios()
			}
			if value == reflect.TypeOf(Suggestion{}) && field.Name == "Severity" {
				child["enum"] = []string{"P2", "P3"}
			}
			if field.Name == "Files" {
				location := LocationSchema(false)
				delete(location["properties"].(map[string]any), "line")
				location["description"] = "Select original read or listing/entry numbers for related test files. Omit files to retain an issued relation; an empty array clears it. The program supplies paths."
				child["items"] = location
			}
			if field.Name == "TestFile" {
				child["minimum"] = 1
				child["description"] = "Select the 1-based original read position in this case's files, together with its published testSymbol number. Omit both to retain the issued reference; operations replaces an issued Go test invocation with the authored recipe."
			}
			if field.Name == "TestSymbol" {
				child["minimum"] = 1
				child["description"] = "Select the published testSymbol number from the original full read selected by testFile. New functions must be written and read before selection."
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
	if input.Validation == nil || input.Validation.Development == nil || input.Validation.Development.Batch.ID == "" || (input.Handler != "engine.development.batch.v2" && input.Handler != "engine.repair.v2") {
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
	return map[string]any{"type": "array", "prefixItems": items, "items": false, "minItems": len(items), "maxItems": len(items)}
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
