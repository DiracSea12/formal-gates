package settings

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/coordination"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type EditableField struct {
	Position int             `json:"position"`
	Name     string          `json:"name"`
	Value    json.RawMessage `json:"value"`
	Source   string          `json:"source"`
}

// EditableFields uses the loader's published sources for both common fields and
// adapter-owned fields. Positions refer to this sorted view.
func EditableFields(e Effective) ([]EditableField, error) {
	data, err := json.Marshal(e.Values)
	if err != nil {
		return nil, err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(e.Sources))
	for name := range e.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]EditableField, 0, len(names))
	for i, name := range names {
		value := values[name]
		if value == nil {
			value = json.RawMessage("null")
		}
		fields = append(fields, EditableField{Position: i + 1, Name: name, Value: value, Source: e.Sources[name]})
	}
	return fields, nil
}

// EditField changes only the selected field in its original overlay file. The
// same Load rules validate the complete candidate before it replaces that file.
func EditField(root, home, scope string, position int, value string, remove bool) (Effective, error) {
	files := overlayPaths(root, home)
	var path string
	switch scope {
	case "project":
		path = files[1]
	case "user":
		path = files[0]
	default:
		return Effective{}, fmt.Errorf("configuration scope must be project or user")
	}
	release, err := coordination.AcquirePathWaiting(path+".lock", "settings edit")
	if err != nil {
		return Effective{}, err
	}
	defer release()
	effective, err := Load(root, home)
	if err != nil {
		return Effective{}, err
	}
	fields, err := EditableFields(effective)
	if err != nil {
		return Effective{}, err
	}
	if position < 1 || position > len(fields) {
		return Effective{}, fmt.Errorf("field position must be 1..%d", len(fields))
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Effective{}, err
	}
	raw := map[string]json.RawMessage{}
	if err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return Effective{}, err
		}
	}
	name := fields[position-1].Name
	if remove {
		delete(raw, name)
	} else {
		var encoded []byte
		switch name {
		case "capacity", "qaCapacity", "tokenCap":
			number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return Effective{}, fmt.Errorf("%s: expected an integer: %w", name, err)
			}
			encoded, err = json.Marshal(number)
			if err != nil {
				return Effective{}, err
			}
		default:
			encoded, err = json.Marshal(value)
			if err != nil {
				return Effective{}, err
			}
		}
		raw[name] = encoded
	}
	candidate, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return Effective{}, err
	}
	candidate = append(candidate, '\n')
	if err := applyOverlay(&effective, path, candidate, path == effective.Files[0] && effective.Sources["tokenCap"] == filepath.Join(root, ".gates", "token-cap.json")); err != nil {
		return Effective{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return Effective{}, err
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(candidate)
	closeErr := temp.Close()
	if writeErr != nil {
		return Effective{}, writeErr
	}
	if closeErr != nil {
		return Effective{}, closeErr
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return Effective{}, err
	}
	return Load(root, home)
}
