package cost

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDispatchCostPreservesRecordedTokenPresence(t *testing.T) {
	for _, raw := range []string{
		`{"target":"review","kind":"action","source":"unavailable","totalInputTokens":0,"inputCacheHitTokens":0,"inputCacheMissTokens":0,"outputTokens":0}`,
		`{"target":"review","kind":"action","source":"unavailable","totalInputTokens":null,"inputCacheHitTokens":null,"inputCacheMissTokens":null,"outputTokens":null}`,
		`{"target":"review","kind":"action","source":"unavailable","totalInputTokens":0,"inputCacheHitTokens":null,"inputCacheMissTokens":0,"outputTokens":null}`,
	} {
		var v DispatchCost
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		json.Unmarshal([]byte(raw), &a)
		json.Unmarshal(after, &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("existing observation rewritten: %s -> %s", raw, after)
		}
	}
	b, err := json.Marshal(DispatchCost{Target: "developer", Kind: "action", Source: SourceUnavailable})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	json.Unmarshal(b, &fields)
	for _, key := range tokenJSONFields {
		if fields[key] != nil {
			t.Fatalf("new missing usage filled with zero: %s", b)
		}
	}
}
