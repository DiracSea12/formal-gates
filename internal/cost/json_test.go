package cost

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCostJSONDistinguishesAbsentMeasurementFromMeasuredZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		absent bool
	}{
		{"unavailable dispatch", DispatchCost{Source: SourceUnavailable, Reason: "dispatch stop has no transcript path"}, true},
		{"unavailable owner", OwnerCost{Source: SourceUnavailable, Reason: "owner baseline is unavailable"}, true},
		{"measured dispatch", DispatchCost{Source: SourceTranscript}, false},
		{"measured owner", OwnerCost{Source: SourceTranscript}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			want := "0"
			if tc.absent {
				want = "null"
				if len(fields["reason"]) == 0 {
					t.Fatalf("unavailable reason was lost: %s", data)
				}
			}
			for _, key := range []string{"totalInputTokens", "inputCacheHitTokens", "inputCacheMissTokens", "outputTokens"} {
				if string(fields[key]) != want {
					t.Fatalf("%s=%s want %s: %s", key, fields[key], want, data)
				}
			}
		})
	}
}

func TestCostJSONRetainsNumbersWithTheirRecordedSource(t *testing.T) {
	for _, source := range []string{SourceTranscript, SourceUnavailable, "unknown"} {
		for _, value := range []any{
			DispatchCost{Source: source, TotalInputTokens: 10, InputCacheHitTokens: 0, InputCacheMissTokens: 10, OutputTokens: 5},
			OwnerCost{Source: source, TotalInputTokens: 10, InputCacheHitTokens: 0, InputCacheMissTokens: 10, OutputTokens: 5},
		} {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["source"]) != `"`+source+`"` || string(fields["totalInputTokens"]) != "10" || string(fields["inputCacheHitTokens"]) != "0" || string(fields["inputCacheMissTokens"]) != "10" || string(fields["outputTokens"]) != "5" {
				t.Fatalf("recorded numbers or source changed: %s", data)
			}
		}
	}
}

func TestUnavailableJSONReplaysThroughTheExistingLedger(t *testing.T) {
	var child RunCost
	measured := DispatchCost{Source: SourceTranscript, TotalInputTokens: 10, InputCacheHitTokens: 3, InputCacheMissTokens: 7, OutputTokens: 4}
	missing := DispatchCost{Source: SourceUnavailable, Reason: "ZCode terminal provider usage is unavailable"}
	Record(&child, "measured", measured)
	Record(&child, "missing", missing)
	RecordOwner(&child, OwnerCost{Source: SourceUnavailable, Reason: "owner baseline is unavailable"})
	data, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	var restored RunCost
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	Record(&restored, "measured", measured)
	Record(&restored, "missing", missing)
	RecordOwner(&restored, OwnerCost{Source: SourceTranscript, TotalInputTokens: 99})
	if !reflect.DeepEqual(child, restored) {
		t.Fatalf("restore/replay changed ledger: %+v want %+v", restored, child)
	}
	var parent RunCost
	Merge(&parent, "child", &restored)
	Merge(&parent, "child", &restored)
	AddChildOwner(&parent, restored.Owner)
	if parent.TotalInputTokens != 10 || parent.InputCacheHitTokens != 3 || parent.InputCacheMissTokens != 7 || parent.OutputTokens != 4 || len(parent.Dispatches) != 2 || parent.Dispatches["child/missing"].Reason != missing.Reason {
		t.Fatalf("child merge duplicated or lost cost facts: %+v", parent)
	}
}
