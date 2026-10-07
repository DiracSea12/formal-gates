package worker

import (
	"fmt"
	"reflect"
	"strings"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// ResultSchema renders the finite core result types; it does not validate
// business values or interpret arbitrary schemas. Core Submit owns admission.
func ResultSchema(input protocol.WorkerInput) (map[string]any, error) {
	properties := map[string]any{
		"actionId":     map[string]any{"type": "string", "enum": []any{input.Pending.ActionID}},
		"provider":     map[string]any{"type": "string", "enum": []any{input.Provider}},
		"outcome":      map[string]any{"type": "string", "enum": []any{protocol.OutcomePass, protocol.OutcomeFail, protocol.OutcomeRuntimeError}},
		"failureClass": map[string]any{"type": "string"}, "interruptionReason": map[string]any{"type": "string"},
	}
	if input.Review != nil {
		body, err := resultTypeSchema(reflect.TypeOf(protocol.ReviewResultEvidence{}))
		if err != nil {
			return nil, err
		}
		if input.Review.Action != review.StartReadiness {
			delete(body["properties"].(map[string]any), "granularityReview")
		}
		properties["review"] = body
	} else if input.Validation != nil {
		body := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}, "required": []any{"binding", "status"}}
		fields := body["properties"].(map[string]any)
		allowed := map[string]bool{"Binding": true, "Status": true}
		switch input.Handler {
		case "engine.development.batch":
			allowed["Development"] = true
		case "engine.repair":
			allowed["Development"], allowed["GranularityReview"] = true, true
		case "engine.merge.resolve":
			allowed["Resolution"] = true
		case "engine.qa.design":
			for _, name := range []string{"Manifest", "Development", "Checkpoint"} {
				allowed[name] = true
			}
		case "engine.qa.review.rationale":
			allowed["Review"], allowed["Suggestions"] = true, true
		case "engine.qa.execution":
			for _, name := range []string{"Execution", "Tool", "Environment", "Checkpoint", "Findings"} {
				allowed[name] = true
			}
		case "engine.gate":
			allowed["Findings"] = true
		default:
			return nil, fmt.Errorf("worker validation schema has no role %s", input.Handler)
		}
		typ := reflect.TypeOf(protocol.ValidationResult{})
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
		properties["validation"] = body
	} else {
		return nil, fmt.Errorf("worker result schema lacks typed role")
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": []any{"actionId", "provider", "outcome"}}
	return schema, nil
}

func resultTypeSchema(value reflect.Type) (map[string]any, error) {
	// TaskKey's actual encoding is its text identity, not its Go fields.
	if value == reflect.TypeOf(runtime.TaskKey{}) {
		return map[string]any{"type": "string"}, nil
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
			// A worker's batch route is the full/custom proposal. The nested user
			// DevelopmentPlan confirmation belongs to SELECT_ROUTE, not worker output.
			if value == reflect.TypeOf(protocol.RouteDecision{}) && field.Name == "DevelopmentPlan" {
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
			properties[name] = child
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}, nil
	}
	return nil, fmt.Errorf("worker result type %s has no supported native representation", value)
}
