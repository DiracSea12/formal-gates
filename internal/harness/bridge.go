package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type envelope struct {
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
}

// orderedProofDTO carries one recorded proof in document order.
type orderedProofDTO struct {
	Name     string          `json:"name"`
	Expected json.RawMessage `json:"expected"`
}

func decodeProofs(entries []orderedProofDTO) ([]Proof, error) {
	proofs := make([]Proof, 0, len(entries))
	for _, entry := range entries {
		expected, err := decodeObject(entry.Expected)
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, Proof{Name: entry.Name, Expected: expected})
	}
	return proofs, nil
}

// decodeObject is decodeValue restricted to a JSON object.
func decodeObject(raw json.RawMessage) (map[string]any, error) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return object, nil
}

// decodeValue parses a payload value while preserving number literals, so the
// canonical encoder can decide between integer and float the way Python does.
func decodeValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func Bridge(in io.Reader, out io.Writer) int {
	b, err := io.ReadAll(io.LimitReader(in, 8*1024*1024+1))
	if err == nil && len(b) > 8*1024*1024 {
		err = fmt.Errorf("bridge input exceeds 8 MiB")
	}
	var e envelope
	if err == nil {
		err = StrictDecode(b, &e)
	}
	var result any
	if err == nil {
		result, err = dispatch(e)
	}
	if err != nil {
		_ = json.NewEncoder(out).Encode(map[string]any{"ok": false, "code": "INVALID_HARNESS_OPERATION", "message": err.Error()})
		return 2
	}
	if err = json.NewEncoder(out).Encode(map[string]any{"ok": true, "value": result}); err != nil {
		return 1
	}
	return 0
}
func dispatch(e envelope) (any, error) {
	switch e.Operation {
	case "supervisor-process":
		var q struct {
			Directory     string `json:"directory"`
			RequestDigest string `json:"requestDigest"`
			Cancel        bool   `json:"cancel"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		alive, err := observeSupervisor(q.Directory, q.RequestDigest, q.Cancel)
		if err != nil {
			return nil, err
		}
		return map[string]any{"alive": alive}, nil
	case "verify-process":
		var q struct {
			Directory string            `json:"directory"`
			Process   json.RawMessage   `json:"process"`
			Receipt   json.RawMessage   `json:"receipt"`
			Request   json.RawMessage   `json:"request"`
			Proofs    []orderedProofDTO `json:"proofs"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		process, err := decodeObject(q.Process)
		if err != nil {
			return nil, err
		}
		receipt, err := decodeObject(q.Receipt)
		if err != nil {
			return nil, err
		}
		request, err := decodeObject(q.Request)
		if err != nil {
			return nil, err
		}
		proofs, err := decodeProofs(q.Proofs)
		if err != nil {
			return nil, err
		}
		kind, refusal := VerifyProcess(q.Directory, process, receipt, request, proofs)
		if refusal != "" {
			return map[string]any{"ok": false, "kind": kind, "refusal": refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "digest":
		// The canonical encoding is the content-addressing format, so it is
		// owned here rather than in the caller. Numbers are decoded with
		// UseNumber so integer literals keep arbitrary precision and float
		// literals can be re-rendered with Python's rules.
		var q struct {
			Value json.RawMessage `json:"value"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		value, err := decodeValue(q.Value)
		if err != nil {
			return nil, err
		}
		digest, err := CanonicalDigest(value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"digest": digest}, nil
	default:
		return nil, fmt.Errorf("unknown bridge operation: %s", e.Operation)
	}
}
