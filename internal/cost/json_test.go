package cost

import (
	"encoding/json"
	"testing"
)

func TestUnavailableJSONDoesNotInventZeroUsage(t *testing.T) {
	for _, value := range []any{DispatchCost{Source: SourceUnavailable, SourceReason: "missing native source"}, OwnerCost{Source: SourceUnavailable, SourceReason: "overlapping owner interval"}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"totalInputTokens", "inputCacheHitTokens", "inputCacheMissTokens", "outputTokens"} {
			if v, ok := fields[key]; !ok || v != nil {
				t.Fatalf("missing usage must be null: %s", data)
			}
		}
		if fields["sourceReason"] == "" {
			t.Fatalf("missing source reason: %s", data)
		}
		var restored DispatchCost
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.Source != SourceUnavailable || restored.TotalInputTokens != 0 {
			t.Fatalf("cannot replay: %+v", restored)
		}
	}
	data, err := json.Marshal(DispatchCost{Source: SourceTranscript, TotalInputTokens: 0})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	if fields["totalInputTokens"] != float64(0) {
		t.Fatalf("measured zero was lost: %s", data)
	}
}
