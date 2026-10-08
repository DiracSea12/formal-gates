package worker

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// ResultSchema renders the finite semantic result types; it does not validate
// business values or interpret arbitrary schemas. Core Submit owns admission.
func ResultSchema(input protocol.WorkerInput) (map[string]any, error) {
	failures := []any{}
	for _, class := range authoring.FailureClasses() {
		failures = append(failures, string(class))
	}
	properties := map[string]any{
		"outcome":      map[string]any{"type": "string", "enum": []any{protocol.OutcomePass, protocol.OutcomeFail, protocol.OutcomeRuntimeError}},
		"failureClass": map[string]any{"type": "string", "enum": failures}, "interruptionReason": map[string]any{"type": "string"},
	}
	if input.Review != nil {
		body, err := resultTypeSchema(reflect.TypeOf(Review{}))
		if err != nil {
			return nil, err
		}
		if input.Review.Action != review.StartReadiness {
			delete(body["properties"].(map[string]any), "granularityReview")
		}
		verification := map[string]any{}
		for _, key := range review.RequiredVerificationChecks() {
			verification[key] = map[string]any{"type": "string", "minLength": 1}
		}
		body["properties"].(map[string]any)["verification"] = fixedObject(verification, review.RequiredVerificationChecks())
		properties["review"] = body
	} else if input.Validation != nil {
		body := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}, "required": []any{"status"}}
		fields := body["properties"].(map[string]any)
		allowed := map[string]bool{"Status": true}
		var statuses []string
		switch input.Handler {
		case "engine.development.batch":
			allowed["Development"] = true
			statuses = []string{protocol.OutcomePass}
		case "engine.repair":
			allowed["Development"], allowed["GranularityReview"] = true, true
			statuses = []string{protocol.OutcomePass}
		case "engine.merge.resolve":
			allowed["Resolution"] = true
			statuses = []string{protocol.OutcomePass}
		case "engine.qa.design":
			statuses = []string{protocol.OutcomePass}
			for _, name := range []string{"Manifest", "Development", "Checkpoint"} {
				allowed[name] = true
			}
		case "engine.qa.review.rationale":
			allowed["Review"], allowed["Suggestions"] = true, true
		case "engine.qa.execution":
			for _, name := range []string{"Execution", "Checkpoint", "Findings"} {
				allowed[name] = true
			}
		case "engine.gate":
			statuses = []string{protocol.OutcomePass, protocol.OutcomeFail}
			allowed["Findings"] = true
		default:
			return nil, fmt.Errorf("worker validation schema has no role %s", input.Handler)
		}
		typ := reflect.TypeOf(Validation{})
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !allowed[field.Name] {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			child, err := resultTypeSchema(field.Type)
			if err != nil {
				return nil, err
			}
			fields[name] = child
		}
		if statuses != nil {
			fields["status"] = map[string]any{"type": "string", "enum": statuses}
		}
		if batch := completionBatch(input); batch != nil {
			completion := fields["development"].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)
			dod := map[string]any{}
			dodIDs := []string{}
			for _, criterion := range batch.DoD {
				dod[criterion.ID] = map[string]any{"type": "string", "minLength": 1}
				dodIDs = append(dodIDs, criterion.ID)
			}
			completion["dod"] = map[string]any{"type": "object", "additionalProperties": false, "properties": dod, "required": dodIDs}
		} else if development, present := fields["development"].(map[string]any); present && !(input.Handler == "engine.repair" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Repair.FinalCandidate == nil) {
			delete(development["properties"].(map[string]any), "completion")
		}

		if input.Handler == "engine.qa.design" {
			coverageKeys := map[string]any{}
			for _, point := range input.Validation.Manifest.Points {
				coverageKeys[point.PointID] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
			}
			fields["manifest"].(map[string]any)["properties"].(map[string]any)["coverage"] = fixedObject(coverageKeys, nil)
		}
		if input.Handler == "engine.qa.review.rationale" {
			items := fields["review"].(map[string]any)["properties"].(map[string]any)
			sources, points, cases, shared := []string{}, []string{}, []string{}, []string{}
			sources = append(sources, input.Validation.Manifest.Sources...)
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
			items["sourceDecisions"] = decisionSchema(sources, nil, decision)
			items["pointDecisions"] = decisionSchema(points, nil, decision)
			items["caseDecisions"] = decisionSchema(cases, input.Validation.InheritableReviews, decision)
			items["sharedStepDecisions"] = decisionSchema(shared, input.Validation.InheritableSharedReviews, decision)
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
			fields["execution"].(map[string]any)["properties"].(map[string]any)["records"] = fixedObject(keys, nil)
		}
		properties["validation"] = body
	} else {
		return nil, fmt.Errorf("worker result schema lacks typed role")
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": []any{"outcome"}}
	return schema, nil
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
