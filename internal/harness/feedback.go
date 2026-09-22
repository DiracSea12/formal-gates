package harness

import (
	"encoding/json"
	"fmt"
)

// taskInputInput is the payload for the task-input bridge operation. The
// wrapper shape and the canonical digest live here; the Python side keeps
// file I/O and the callers. The wrapper's sha256 field is the canonical
// digest of the feedback value (CanonicalDigest).
type taskInputInput struct {
	Role      string          `json:"role"`
	Revision  string          `json:"revision"`
	Base      string          `json:"base"`
	Candidate string          `json:"candidate"`
	Checks    json.RawMessage `json:"checks"`
	Feedback  json.RawMessage `json:"feedback"`
}

// evidenceFeedbackInput is the payload for the evidence-feedback bridge
// operation. `revision` is the run's revision (from store.manifest); `evidence`
// is the already-resolved artifact value Python obtained via verify_evidence.
// Python keeps verify_evidence (artifact addressing, verify-files/process/
// source delegation); Go owns the wrapper shape and the binding judgement.
type evidenceFeedbackInput struct {
	Ref      string          `json:"ref"`
	Purpose  string          `json:"purpose"`
	Revision string          `json:"revision"`
	Evidence json.RawMessage `json:"evidence"`
}

// planTaskInput builds the task.json content: the harness-v1 envelope with the
// untrusted-feedback wrapper. It computes the canonical digest itself and
// never merges any field of
// feedback into the top-level role/revision/base/candidate/checks/control.
//
// feedback and checks are decoded with number fidelity (decodeValue uses
// UseNumber) so an integer literal stays an integer and the produced canonical
// bytes match the Python side's digest(feedback) byte for byte. A missing or
// null feedback yields feedback: null (not an omitted key, not an empty object).
func planTaskInput(q taskInputInput) (map[string]any, error) {
	var feedback any
	if len(q.Feedback) > 0 {
		decoded, err := decodeValue(q.Feedback)
		if err != nil {
			return nil, fmt.Errorf("task-input: malformed feedback: %w", err)
		}
		feedback = decoded
	}
	var checks any
	if len(q.Checks) > 0 {
		decoded, err := decodeValue(q.Checks)
		if err != nil {
			return nil, fmt.Errorf("task-input: malformed checks: %w", err)
		}
		checks = decoded
	}

	var wrapped any
	if feedback != nil {
		digest, err := CanonicalDigest(feedback)
		if err != nil {
			return nil, err
		}
		wrapped = map[string]any{
			"kind":      "untrusted-feedback/v1",
			"authority": "none",
			"sha256":    digest,
			"content":   feedback,
		}
	}

	task := map[string]any{
		"profile":   "harness-v1",
		"role":      q.Role,
		"revision":  q.Revision,
		"base":      q.Base,
		"candidate": q.Candidate,
		"checks":    checks,
		"feedback":  wrapped,
		"control": map[string]any{
			"can_approve":           false,
			"can_choose_next_stage": false,
			"can_edit_state":        false,
			"result_schema_only":    true,
		},
	}
	return task, nil
}

// planEvidenceFeedback builds the evidence-data wrapper, or returns a typed
// refusal when the evidence is bound to a different requirement. The binding
// rule: the default is the run's
// revision, so evidence that omits "revision" passes, and evidence whose
// "revision" differs from the run's revision is refused with HarnessError
// wording. The refusal travels inside the value so Python's
// refuse() can raise the HarnessError family.
func planEvidenceFeedback(q evidenceFeedbackInput) (map[string]any, error) {
	var evidence any
	if len(q.Evidence) > 0 && string(q.Evidence) != "null" {
		decoded, err := decodeValue(q.Evidence)
		if err != nil {
			return nil, fmt.Errorf("evidence-feedback: malformed evidence: %w", err)
		}
		evidence = decoded
	}

	content := evidence
	// Python's value.get('role', 'verification') returns the default only when
	// the key is absent; when present it returns the raw value verbatim,
	// whatever its type (null, number, ...). Keep the default for the absent
	// case but pass the present value through unchanged.
	var role any = "verification"
	var evMap map[string]any
	if m, ok := evidence.(map[string]any); ok {
		evMap = m
		if r, ok := m["role"]; ok {
			role = r
		}
		if res, ok := m["result"]; ok {
			content = res
		}
	}

	// Binding judgement. The default is the run's revision (key absent => pass).
	// When the key is present the reference is `value.get('revision', revision)
	// != revision`, where revision is always a str, so a non-string value can
	// never match and must refuse; only a JSON string equal to the run
	// revision passes. A number, null, bool or missing-key default therefore
	// all refuse (a numeric revision like 7 is never equal to the str "7").
	if evMap != nil {
		if rv, ok := evMap["revision"]; ok {
			s, isStr := rv.(string)
			if !isStr || s != q.Revision {
				return map[string]any{
					"ok":      false,
					"kind":    "binding",
					"refusal": "FEEDBACK_BINDING: evidence is from a different requirement",
				}, nil
			}
		}
	}

	wrapper := map[string]any{
		"kind":      "evidence-data/v1",
		"authority": "none",
		"purpose":   q.Purpose,
		"source": map[string]any{
			"artifact": q.Ref,
			"role":     role,
			"revision": q.Revision,
		},
		"content": content,
	}
	return map[string]any{"ok": true, "wrapper": wrapper}, nil
}
