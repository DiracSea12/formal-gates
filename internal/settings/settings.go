// Package settings reads the editable configuration once at run creation.
// A restart uses the persisted effective values and never resets cost or grants.
package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Values struct {
	QACapacity          int    `json:"qaCapacity"`
	TokenCap            *int64 `json:"tokenCap,omitempty"`
	Capacity            int    `json:"capacity"`
	CodexExecutable     string `json:"codexExecutable"`
	Model               string `json:"model,omitempty"`
	ReasoningEffort     string `json:"reasoningEffort,omitempty"`
	WorkBuddyExecutable string `json:"workbuddyExecutable"`
	ZCodeExecutable     string `json:"zcodeExecutable"`
	ZCodeScript         string `json:"zcodeScript"`
	DSHExecutable       string `json:"dshExecutable"`
}
type Effective struct {
	Values  Values            `json:"values"`
	Sources map[string]string `json:"sources"`
	Files   []string          `json:"files"`
}
type input struct {
	QACapacity          *int    `json:"qaCapacity,omitempty"`
	TokenCap            *int64  `json:"tokenCap,omitempty"`
	Capacity            *int    `json:"capacity,omitempty"`
	CodexExecutable     *string `json:"codexExecutable,omitempty"`
	Model               *string `json:"model,omitempty"`
	ReasoningEffort     *string `json:"reasoningEffort,omitempty"`
	WorkBuddyExecutable *string `json:"workbuddyExecutable,omitempty"`
	ZCodeExecutable     *string `json:"zcodeExecutable,omitempty"`
	ZCodeScript         *string `json:"zcodeScript,omitempty"`
	DSHExecutable       *string `json:"dshExecutable,omitempty"`
}

// Load overlays project values over installation/user values field by field.
// Unknown fields and null are errors: a typo must never silently remove a cap.
func Load(root, home string) (Effective, error) {
	e := Effective{Values: Values{QACapacity: 4, Capacity: 4, CodexExecutable: "codex", WorkBuddyExecutable: "codebuddy", ZCodeExecutable: "node", DSHExecutable: "dsh"}, Sources: map[string]string{}, Files: []string{filepath.Join(home, ".config", "formal-gates", "config.json"), filepath.Join(root, ".gates", "config.json")}}
	for _, field := range []string{"qaCapacity", "tokenCap", "capacity", "codexExecutable", "model", "reasoningEffort", "workbuddyExecutable", "zcodeExecutable", "zcodeScript", "dshExecutable"} {
		e.Sources[field] = "default"
	}
	for _, path := range e.Files {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return e, err
		}
		var raw map[string]json.RawMessage
		if err = json.Unmarshal(data, &raw); err != nil || raw == nil {
			return e, fmt.Errorf("%s: expected a JSON object: %v", path, err)
		}
		for field, value := range raw {
			if _, ok := e.Sources[field]; !ok {
				return e, fmt.Errorf("%s: unknown configuration field %q; see references/user-configuration.md", path, field)
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return e, fmt.Errorf("%s: %s: null is invalid; remove the field to inherit its default", path, field)
			}
		}
		var in input
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&in); err != nil {
			return e, fmt.Errorf("%s: %w; see references/user-configuration.md for supported fields", path, err)
		}
		if err = dec.Decode(new(any)); err != io.EOF {
			return e, fmt.Errorf("%s: trailing JSON data", path)
		}
		if in.TokenCap != nil {
			if *in.TokenCap <= 0 {
				return e, fmt.Errorf("%s: tokenCap: use a positive integer in tokens", path)
			}
			e.Values.TokenCap = in.TokenCap
		}
		if in.QACapacity != nil {
			if *in.QACapacity < 1 || *in.QACapacity > 64 {
				return e, fmt.Errorf("%s: qaCapacity must be 1..64", path)
			}
			e.Values.QACapacity = *in.QACapacity
		}

		if in.Capacity != nil {
			if *in.Capacity < 1 || *in.Capacity > 64 {
				return e, fmt.Errorf("%s: capacity: use an integer from 1 to 64", path)
			}
			e.Values.Capacity = *in.Capacity
		}
		if in.CodexExecutable != nil {
			if strings.TrimSpace(*in.CodexExecutable) == "" {
				return e, fmt.Errorf("%s: codexExecutable: use a command name or executable path", path)
			}
			e.Values.CodexExecutable = *in.CodexExecutable
		}

		for _, field := range []struct {
			name   string
			input  *string
			target *string
		}{
			{"workbuddyExecutable", in.WorkBuddyExecutable, &e.Values.WorkBuddyExecutable},
			{"zcodeExecutable", in.ZCodeExecutable, &e.Values.ZCodeExecutable},
			{"zcodeScript", in.ZCodeScript, &e.Values.ZCodeScript},
			{"dshExecutable", in.DSHExecutable, &e.Values.DSHExecutable},
		} {
			if field.input == nil {
				continue
			}
			if strings.TrimSpace(*field.input) == "" {
				return e, fmt.Errorf("%s: %s: use a nonempty command/path or remove the field", path, field.name)
			}
			*field.target = *field.input
		}
		if in.Model != nil {
			if strings.TrimSpace(*in.Model) == "" {
				return e, fmt.Errorf("%s: model: use a nonempty model identifier or remove the field", path)
			}
			e.Values.Model = *in.Model
		}
		if in.ReasoningEffort != nil {
			switch *in.ReasoningEffort {
			case "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
			default:
				return e, fmt.Errorf("%s: reasoningEffort: expected minimal, low, medium, high, xhigh, max, or ultra", path)
			}
			e.Values.ReasoningEffort = *in.ReasoningEffort
		}
		for field := range raw {
			e.Sources[field] = path
		}
	}
	return e, nil
}
