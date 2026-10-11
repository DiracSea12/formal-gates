package harness

import (
	"encoding/json"
	"fmt"
)

// Turn identity and host selection come from the issued native request.
func codexTurnParameters(request any, thread string) (map[string]any, error) {
	prompt, ok := getOf(request, "prompt").(string)
	if !ok {
		return nil, fmt.Errorf("prompt must be a string")
	}
	if schema := objectOrEmpty(getOf(request, "schema")); len(schema) > 0 {
		body, err := json.Marshal(schema)
		if err != nil {
			return nil, err
		}
		prompt += "\n[Current submit_semantics input schema]\n" + string(body)
	}
	params := map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": prompt}}}

	for _, pair := range [][2]string{{"model", "model"}, {"reasoning_effort", "effort"}} {
		if value, _ := getOf(request, pair[0]).(string); value != "" {
			params[pair[1]] = value
		}
	}
	return params, nil
}
