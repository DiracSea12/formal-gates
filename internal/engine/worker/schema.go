package worker

import (
	"fmt"
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
	if input.Handler == "engine.qa.execution" {
		fields := interruption["properties"].(map[string]any)
		fields["records"] = body["properties"].(map[string]any)["records"]
		fields["findings"] = body["properties"].(map[string]any)["findings"]
	}
	return map[string]any{"anyOf": []any{body, interruption}}, nil
}

func completedSchema(input protocol.WorkerInput) (map[string]any, error) {
	var typ reflect.Type
	if input.Review != nil {
		typ = reflect.TypeOf(Review{})
	} else if input.Validation != nil {
		switch input.Handler {
		case "engine.development.batch":
			typ = reflect.TypeOf(Development{})
		case "engine.repair":
			typ = reflect.TypeOf(Repair{})
		case "engine.merge.resolve":
			typ = reflect.TypeOf(ConflictResolution{})
		case "engine.qa.design":
			typ = reflect.TypeOf(Design{})
		case "engine.qa.review.rationale":
			typ = reflect.TypeOf(CaseReview{})
		case "engine.qa.execution":
			typ = reflect.TypeOf(ExecutionJudgments{})
		case "engine.repair.verify":
			typ = reflect.TypeOf(Execution{})
		case "engine.gate":
			typ = reflect.TypeOf(Gate{})
		default:
			return nil, fmt.Errorf("worker result schema has no role %s", input.Handler)
		}
	} else {
		return nil, fmt.Errorf("worker result schema lacks typed role")
	}
	body, err := resultTypeSchema(typ)
	if err != nil {
		return nil, err
	}
	fields := body["properties"].(map[string]any)
	if input.Review != nil {
		if input.Review.Action != review.StartReadiness {
			delete(fields, "granularityReview")
		}
		fields["status"] = map[string]any{"type": "string", "enum": []string{protocol.OutcomePass, protocol.OutcomeFail}}
		verification := map[string]any{}
		for _, key := range review.RequiredVerificationChecks() {
			verification[key] = map[string]any{"type": "string", "minLength": 1}
		}
		fields["verification"] = fixedObject(verification, review.RequiredVerificationChecks())
		return body, nil
	}
	if input.Handler == "engine.gate" {
		fields["status"] = map[string]any{"type": "string", "enum": []string{protocol.OutcomePass, protocol.OutcomeFail}}
	}
	if batch := completionBatch(input); batch != nil {
		completion := fields["completion"].(map[string]any)["properties"].(map[string]any)
		dod, ids := map[string]any{}, []string{}
		for _, criterion := range batch.DoD {
			dod[criterion.ID] = map[string]any{"type": "string", "minLength": 1}
			ids = append(ids, criterion.ID)
		}
		completion["dod"] = fixedObject(dod, ids)
	} else if !(input.Handler == "engine.repair" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Repair.FinalCandidate == nil) {
		delete(fields, "completion")
	}
	if input.Handler == "engine.repair.verify" {
		records := map[string]any{}
		for _, fact := range input.Validation.ExecutionFacts {
			item, err := resultTypeSchema(reflect.TypeOf(Observation{}))
			if err != nil {
				return nil, err
			}
			records[fact.CaseID] = item
		}
		fields["records"] = fixedObject(records, input.Validation.Repair.VerificationCases)
	}
	if input.Handler == "engine.qa.design" {
		keys := map[string]any{}
		for _, point := range input.Validation.Manifest.Points {
			keys[point.PointID] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}
		fields["coverage"] = fixedObject(keys, nil)
	}
	if input.Handler == "engine.qa.review.rationale" {
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
	if input.Handler == "engine.qa.execution" {
		keys := map[string]any{}
		observation, err := resultTypeSchema(reflect.TypeOf(Observation{}))
		if err != nil {
			return nil, err
		}
		for _, fact := range input.Validation.ExecutionFacts {
			if slices.Contains(input.Validation.RequiredCases, fact.CaseID) && slices.Contains(input.Validation.PendingCases, fact.CaseID) {
				keys[fact.CaseID] = observation
			}
		}
		fields["records"] = fixedObject(keys, nil)
	}
	return body, nil
}

func resultTypeSchema(value reflect.Type) (map[string]any, error) {
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
			if value == reflect.TypeOf(protocol.DevelopmentBatch{}) && field.Name == "Route" {
				continue
			}
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			child, err := resultTypeSchema(field.Type)
			if value == reflect.TypeOf(protocol.DevelopmentCheck{}) && field.Name == "Purpose" {
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
			if value == reflect.TypeOf(Case{}) && field.Name == "Scenario" {
				child["enum"] = coverage.Scenarios()
			}
			if value == reflect.TypeOf(Suggestion{}) && field.Name == "Severity" {
				child["enum"] = []string{"P2", "P3"}
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
			return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required, "description": protocol.GranularityResultContract()}, nil
		}

		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}, nil
	}
	return nil, fmt.Errorf("worker result type %s has no supported native representation", value)
}

// Completion keys come only from the issued member batch. Final-candidate
// verification and repair planning have different evidence duties.
func completionBatch(input protocol.WorkerInput) *protocol.DevelopmentBatch {
	if input.Validation == nil || input.Validation.Development == nil || input.Validation.Development.Batch.ID == "" || (input.Handler != "engine.development.batch" && input.Handler != "engine.repair") {
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
	properties := map[string]any{}
	required := []string{}
	for _, id := range ids {
		properties[id] = decision
		if !slices.ContainsFunc(inherited, func(item coverage.ReviewDecision) bool { return item.ID == id }) {
			required = append(required, id)
		}
	}
	return fixedObject(properties, required)
}
