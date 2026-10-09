package cost

import "encoding/json"

// Absent measurements serialize as null. Ledger arithmetic and persisted
// nonzero values retain their integer representation and source qualification.
func unavailableJSON(value any, source string, tokens [4]int64) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || source != SourceUnavailable || tokens != [4]int64{} {
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
	return unavailableJSON(plain(v), v.Source, [4]int64{v.TotalInputTokens, v.InputCacheHitTokens, v.InputCacheMissTokens, v.OutputTokens})
}

func (v OwnerCost) MarshalJSON() ([]byte, error) {
	type plain OwnerCost
	return unavailableJSON(plain(v), v.Source, [4]int64{v.TotalInputTokens, v.InputCacheHitTokens, v.InputCacheMissTokens, v.OutputTokens})
}
