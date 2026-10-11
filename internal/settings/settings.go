// Package settings reads the editable configuration once at run creation.
// A restart uses the persisted effective values and never resets cost or grants.
package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/host"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Values struct {
	Native     map[string]string `json:"-"`
	QACapacity int               `json:"qaCapacity"`
	TokenCap   *int64            `json:"tokenCap,omitempty"`
	Capacity   int               `json:"capacity"`

	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}
type Effective struct {
	Values  Values            `json:"values"`
	Sources map[string]string `json:"sources"`
	Files   []string          `json:"files"`
}
type input struct {
	QACapacity *int   `json:"qaCapacity,omitempty"`
	TokenCap   *int64 `json:"tokenCap,omitempty"`
	Capacity   *int   `json:"capacity,omitempty"`

	Model           *string `json:"model,omitempty"`
	ReasoningEffort *string `json:"reasoningEffort,omitempty"`
}

// Load overlays project values over installation/user values field by field.
// Unknown fields and null are errors: a typo must never silently remove a cap.
func Load(root, home string) (Effective, error) {
	nativeFields := host.Configuration()
	e := Effective{Values: Values{QACapacity: 4, Capacity: 16, Native: map[string]string{}}, Sources: map[string]string{}, Files: []string{filepath.Join(home, ".config", "formal-gates", "config.json"), filepath.Join(root, ".gates", "config.json")}}
	for _, name := range []string{"qaCapacity", "tokenCap", "capacity", "model", "reasoningEffort"} {
		e.Sources[name] = "default"
	}
	for name, field := range nativeFields {
		if _, exists := e.Sources[name]; exists {
			return e, fmt.Errorf("platform configuration shadows common field %q", name)
		}
		e.Values.Native[name] = field.Default
		e.Sources[name] = "default"
	}
	// The supported project cap input participates in this same overlay.
	legacy := filepath.Join(root, ".gates", "token-cap.json")
	if data, err := os.ReadFile(legacy); err == nil {
		var value struct {
			Cap *int64 `json:"engineDispatchMaxTotalTokens"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&value); err != nil {
			return e, fmt.Errorf("token cap config %s is invalid: %w", legacy, err)
		}
		if value.Cap == nil || *value.Cap < 0 {
			return e, fmt.Errorf("token cap config %s is invalid: engineDispatchMaxTotalTokens must be non-negative", legacy)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return e, fmt.Errorf("token cap config %s has trailing JSON data", legacy)
		}
		e.Values.TokenCap, e.Sources["tokenCap"] = value.Cap, legacy
	} else if !os.IsNotExist(err) {
		return e, err
	}
	for _, path := range e.Files {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return e, err
		}
		if err := applyOverlay(&e, path, data, path == e.Files[0] && e.Sources["tokenCap"] == legacy); err != nil {
			return e, err
		}
	}
	return e, nil
}

// applyOverlay is the single-file parser and validator for persisted and edited
// configuration. A project cap can retain precedence over the user cap field.
func applyOverlay(e *Effective, path string, data []byte, preserveTokenCap bool) error {
	nativeFields := host.Configuration()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return fmt.Errorf("%s: expected a JSON object: %v", path, err)
	}
	for field, value := range raw {
		if _, ok := e.Sources[field]; !ok {
			return fmt.Errorf("%s: unknown configuration field %q; see references/user-configuration.md", path, field)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s: %s: null is invalid; remove the field to inherit its default", path, field)
		}
	}
	var in input
	dec := json.NewDecoder(bytes.NewReader(data))
	// Exact names (including platform fields) were checked against the one registry above.
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("%s: %w; see references/user-configuration.md for supported fields", path, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s: trailing JSON data", path)
	}
	if in.TokenCap != nil {
		if *in.TokenCap < 0 {
			return fmt.Errorf("%s: tokenCap: use a non-negative integer in tokens", path)
		}
		if !preserveTokenCap {
			e.Values.TokenCap = in.TokenCap
		}
	}
	if in.QACapacity != nil {
		if *in.QACapacity < 1 || *in.QACapacity > 64 {
			return fmt.Errorf("%s: qaCapacity must be 1..64", path)
		}
		e.Values.QACapacity = *in.QACapacity
	}

	if in.Capacity != nil {
		if *in.Capacity < 1 || *in.Capacity > 64 {
			return fmt.Errorf("%s: capacity: use an integer from 1 to 64", path)
		}
		e.Values.Capacity = *in.Capacity
	}
	for name, field := range nativeFields {
		value, present := raw[name]
		if !present {
			continue
		}
		var decoded string
		if err := json.Unmarshal(value, &decoded); err != nil {
			return fmt.Errorf("%s: %s: %w", path, name, err)
		}
		if field.Validate != nil {
			if err := field.Validate(decoded); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		e.Values.Native[name] = decoded
	}
	if in.Model != nil {
		if strings.TrimSpace(*in.Model) == "" {
			return fmt.Errorf("%s: model: use a nonempty model identifier or remove the field", path)
		}
		e.Values.Model = *in.Model
	}
	if in.ReasoningEffort != nil {
		switch *in.ReasoningEffort {
		case "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		default:
			return fmt.Errorf("%s: reasoningEffort: expected minimal, low, medium, high, xhigh, max, or ultra", path)
		}
		e.Values.ReasoningEffort = *in.ReasoningEffort
	}
	for field := range raw {
		if field != "tokenCap" || !preserveTokenCap {
			e.Sources[field] = path
		}
	}
	return nil
}

// NativeString reads an adapter-owned option. Load freezes all configured defaults
// into Native; the descriptor default also supports existing zero-value callers.
func (v Values) NativeString(name string) string {
	if value, ok := v.Native[name]; ok {
		return value
	}
	return host.Configuration()[name].Default
}

// Preserve the existing flat JSON wire format. Adding a native option changes
// only its descriptor, never this struct, encoder, decoder or overlay loop.
func (v Values) MarshalJSON() ([]byte, error) {
	type common Values
	data, err := json.Marshal(common(v))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for name, value := range v.Native {
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("native option shadows common field %q", name)
		}
		fields[name], err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

func (v *Values) UnmarshalJSON(data []byte) error {
	type common Values
	var base common
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	native := map[string]string{}
	for name := range host.Configuration() {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		native[name] = value
	}
	*v = Values(base)
	v.Native = native
	return nil
}
