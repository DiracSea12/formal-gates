package cost

import "encoding/json"

// Unavailable is an absent measurement, including when serialized. Keep the
// integer representation inside the existing ledger for arithmetic and replay.
func unavailableJSON(value any, source string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || source != SourceUnavailable {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"totalInputTokens", "inputCacheHitTokens", "inputCacheMissTokens", "outputTokens"} {
		fields[key] = json.RawMessage("null")
	}
	return json.Marshal(fields)
}

func (v DispatchCost) MarshalJSON() ([]byte, error) {
	type plain DispatchCost
	return unavailableJSON(plain(v), v.Source)
}

func (v OwnerCost) MarshalJSON() ([]byte, error) {
	type plain OwnerCost
	return unavailableJSON(plain(v), v.Source)
}
