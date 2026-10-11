package harness

import "testing"

func TestStrictDecodeRejectsAmbiguousOrExtraFields(t *testing.T) {
	for _, body := range []string{`{"value":"x","extra":1}`, `{"value":"x","value":"y"}`, `{"value":"x"} {}`} {
		var result struct {
			Value string `json:"value"`
		}
		if err := StrictDecode([]byte(body), &result); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
