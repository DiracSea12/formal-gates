package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

// codexOutputSchema uses native null as the transport spelling of an omitted
// domain property and uses entry arrays for open dictionaries. The original
// request schema remains the source of requiredness and value constraints.
func codexOutputSchema(schema any) (any, error) {
	body, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	copy, err := decodeNativeJSON(body)
	if err != nil {
		return nil, err
	}
	return codexStrictSchema(copy), nil
}

func codexStrictSchema(value any) any {
	schema, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if additional, ok := codexOpenMap(schema); ok {
		kind := any("array")
		if codexSchemaAllowsNull(schema) {
			kind = []any{"array", "null"}
		}
		return map[string]any{"type": kind, "description": "Dictionary transport: one entry per unique key; each value follows the declared value schema.", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"key", "value"}, "properties": map[string]any{"key": map[string]any{"type": "string"}, "value": codexStrictSchema(additional)}}}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		required := make(map[string]bool)
		for _, field := range listOf(schema["required"]) {
			required[textOf(field)] = true
		}
		names := append([]any{}, listOf(schema["required"])...)
		optional := make([]string, 0, len(properties))
		for name, property := range properties {
			if !required[name] {
				optional = append(optional, name)
			}
			translated := codexStrictSchema(property)
			if !required[name] && !codexSchemaAllowsNull(property) {
				translated = map[string]any{"anyOf": []any{translated, map[string]any{"type": "null"}}}
			}
			properties[name] = translated
		}
		sort.Strings(optional)
		for _, name := range optional {
			names = append(names, name)
		}
		schema["required"] = names
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if child, ok := schema[key].(map[string]any); ok {
			schema[key] = codexStrictSchema(child)
		}
	}
	for i, child := range listOf(schema["anyOf"]) {
		listOf(schema["anyOf"])[i] = codexStrictSchema(child)
	}
	return schema
}

func codexSchemaAllowsNull(value any) bool {
	schema := objectOrEmpty(value)
	if schema["type"] == "null" {
		return true
	}
	for _, kind := range listOf(schema["type"]) {
		if kind == "null" {
			return true
		}
	}
	for _, branch := range listOf(schema["anyOf"]) {
		if codexSchemaAllowsNull(branch) {
			return true
		}
	}
	return false
}

// codexDomainResult only removes the native omission marker from properties
// which the original domain schema declared optional and nonnullable. Missing
// required fields, bad values and unknown fields are retained for domain checks.
func codexDomainResult(schema, value any) (any, error) {
	contract := objectOrEmpty(schema)
	if value == nil && codexSchemaAllowsNull(schema) {
		return nil, nil
	}
	// Nullable unions have one value schema and one null alternative.
	branches := listOf(contract["anyOf"])
	if len(branches) == 2 {
		if getOf(branches[0], "type") == "null" {
			return codexDomainResult(branches[1], value)
		}
		if getOf(branches[1], "type") == "null" {
			return codexDomainResult(branches[0], value)
		}
	}
	if additional, ok := codexOpenMap(contract); ok {
		rows, ok := value.([]any)
		if !ok {
			return nil, &blockedError{message: "CODEX_RESULT: dictionary transport must be an entry array"}
		}
		result := make(map[string]any, len(rows))
		for _, row := range rows {
			entry, ok := row.(map[string]any)
			if !ok || len(entry) != 2 {
				return nil, &blockedError{message: "CODEX_RESULT: dictionary entry must contain key and value only"}
			}
			key, ok := entry["key"].(string)
			item, present := entry["value"]
			if !ok || !present {
				return nil, &blockedError{message: "CODEX_RESULT: dictionary entry key/value is missing or invalid"}
			}
			if _, duplicate := result[key]; duplicate {
				return nil, &blockedError{message: "CODEX_RESULT: duplicate dictionary key " + key}
			}
			decoded, err := codexDomainResult(additional, item)
			if err != nil {
				return nil, err
			}
			result[key] = decoded
		}
		return result, nil
	}
	switch result := value.(type) {
	case map[string]any:
		properties := objectOrEmpty(contract["properties"])
		required := listOf(contract["required"])
		for name, field := range result {
			property, known := properties[name]
			if !known {
				continue
			}
			if field == nil && !slices.Contains(required, any(name)) && !codexSchemaAllowsNull(property) {
				delete(result, name)
				continue
			}
			decoded, err := codexDomainResult(property, field)
			if err != nil {
				return nil, err
			}
			result[name] = decoded
		}
	case []any:
		for i, field := range result {
			decoded, err := codexDomainResult(contract["items"], field)
			if err != nil {
				return nil, err
			}
			result[i] = decoded
		}
	}
	return value, nil
}

func codexOpenMap(schema map[string]any) (map[string]any, bool) {
	additional, ok := schema["additionalProperties"].(map[string]any)
	object := schema["type"] == "object" || slices.Contains(listOf(schema["type"]), any("object"))
	return additional, ok && object && len(objectOrEmpty(schema["properties"])) == 0
}

// The emitted turn request is the sole native schema/prompt projection used by
// execution and evidence verification. Native source records this actual wire.
func codexTurnParameters(request any, thread string) (map[string]any, error) {
	original, err := mustEntry(request, "schema")
	if err != nil {
		return nil, err
	}
	schema, err := codexOutputSchema(original)
	if err != nil {
		return nil, err
	}
	prompt, ok := getOf(request, "prompt").(string)
	if !ok {
		return nil, fmt.Errorf("prompt must be a string")
	}
	originalBytes, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	wireBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(originalBytes, wireBytes) {
		prompt += "\nNative output transport: follow the native output schema exactly. A null in an originally optional nonnullable property means omit that domain property. Dictionary fields use arrays of objects with exactly key and value; keys must be unique. The original job schema and business checks remain the domain contract."
	}
	params := map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": prompt}}, "outputSchema": schema}
	for _, pair := range [][2]string{{"model", "model"}, {"reasoning_effort", "effort"}} {
		if value, _ := getOf(request, pair[0]).(string); value != "" {
			params[pair[1]] = value
		}
	}
	return params, nil
}
