package cost

import "encoding/json"

// Unavailable is an absent measurement, including when serialized. Keep the
// integer representation inside the existing ledger for arithmetic and replay.
var tokenJSONFields = [...]string{"totalInputTokens", "inputCacheHitTokens", "inputCacheMissTokens", "outputTokens"}

func unavailableJSON(value any, source string, numeric *[4]bool) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || source != SourceUnavailable {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for i, key := range tokenJSONFields {
		if numeric == nil || !numeric[i] {
			fields[key] = json.RawMessage("null")
		}
	}
	return json.Marshal(fields)
}

func (v DispatchCost) MarshalJSON() ([]byte, error) {
	type plain DispatchCost
	return unavailableJSON(plain(v), v.Source, v.numericTokenFields)
}

func (v OwnerCost) MarshalJSON() ([]byte, error) {
	type plain OwnerCost
	return unavailableJSON(plain(v), v.Source, nil)
}

func (v *DispatchCost) UnmarshalJSON(data []byte) error {
	type plain DispatchCost
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = DispatchCost(decoded)
	if v.Source != SourceUnavailable {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var numeric [4]bool
	anyNumeric := false
	for i, key := range tokenJSONFields {
		var value *int64
		if raw, ok := fields[key]; ok {
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
		}
		numeric[i] = value != nil
		anyNumeric = anyNumeric || numeric[i]
	}
	if anyNumeric {
		v.numericTokenFields = &numeric
	}
	return nil
}
