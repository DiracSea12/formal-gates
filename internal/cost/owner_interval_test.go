package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func ownerUsageEvent(stamp string, input, cached, output int64) string {
	raw, _ := json.Marshal(map[string]any{"timestamp": stamp, "type": "event_msg", "payload": map[string]any{"type": "token_count", "model": "qa-model", "info": map[string]any{"last_token_usage": map[string]int64{"input_tokens": input, "cached_input_tokens": cached, "output_tokens": output, "reasoning_output_tokens": output / 2}}}})
	return string(raw) + "\n"
}

func TestOwnerIntervalExcludesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	first := ownerUsageEvent("one", 100, 40, 20)
	if err := os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	baseline, err := CaptureBaseline("codex", path)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Usage != (Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 60, OutputTokens: 20}) || baseline.Size != int64(len(first)) || baseline.Digest == "" {
		t.Fatalf("baseline %+v", baseline)
	}
	if err := os.WriteFile(path, []byte(first+ownerUsageEvent("two", 35, 15, 8)), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := MeasureOwner("codex", path, baseline)
	if err != nil || got != (Usage{InputCacheHitTokens: 15, InputCacheMissTokens: 20, OutputTokens: 8}) {
		t.Fatalf("got %+v err %v", got, err)
	}
	got2, err := MeasureOwner("codex", path, baseline)
	if err != nil || got2 != got {
		t.Fatalf("repeat %+v %v", got2, err)
	}
}

func TestOwnerIntervalRequiresSupportedProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	if err := os.WriteFile(path, []byte(ownerUsageEvent("one", 100, 40, 20)), 0600); err != nil {
		t.Fatal(err)
	}
	baseline, err := CaptureBaseline("codex", path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MeasureOwner("cursor", path, baseline)
	if err == nil || got != (Usage{}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestOwnerAndDispatchRecordedOnce(t *testing.T) {
	var run RunCost
	dispatch := DispatchCost{Target: "gate", Kind: "gate", TotalInputTokens: 10, InputCacheHitTokens: 3, InputCacheMissTokens: 7, OutputTokens: 4, Source: SourceTranscript}
	owner := OwnerCost{TotalInputTokens: 35, InputCacheHitTokens: 15, InputCacheMissTokens: 20, OutputTokens: 8, Source: SourceTranscript}
	Record(&run, "d1", dispatch)
	RecordOwner(&run, owner)
	before, _ := json.Marshal(run)
	RecordOwner(&run, owner)
	Record(&run, "d1", dispatch)
	after, _ := json.Marshal(run)
	if string(before) != string(after) || run.TotalInputTokens != 45 || run.InputCacheHitTokens != 18 || run.InputCacheMissTokens != 27 || run.OutputTokens != 12 || len(run.Dispatches) != 1 || !reflect.DeepEqual(run.Dispatches["d1"], dispatch) {
		t.Fatalf("double count/changed dispatch: %+v", run)
	}
}

func TestChildMergeKeepsOwnerCostsSeparate(t *testing.T) {
	var run RunCost
	if err := json.Unmarshal([]byte(`{"totalInputTokens":0,"dispatches":{}}`), &run); err != nil || run.Owner != nil {
		t.Fatalf("run cost: %+v %v", run, err)
	}
	RecordOwner(&run, OwnerCost{Source: SourceUnavailable})
	child := RunCost{}
	Record(&child, "d", DispatchCost{TotalInputTokens: 7, OutputTokens: 3, Source: SourceTranscript})
	RecordOwner(&child, OwnerCost{TotalInputTokens: 100, OutputTokens: 50, Source: SourceTranscript})
	Merge(&run, "child", &child)
	Merge(&run, "child", &child)
	if run.TotalInputTokens != 7 || run.OutputTokens != 3 || len(run.Dispatches) != 1 || run.Owner.Source != SourceUnavailable {
		t.Fatalf("child owner aggregated or dispatch doubled: %+v", run)
	}
}

func TestDispatchParserDeduplicatesUsage(t *testing.T) {
	line := ownerUsageEvent("one", 100, 40, 20)
	got, err := parseTranscript("codex", strings.NewReader("{}\n"+line+line))
	if err != nil || got != (Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 60, OutputTokens: 20}) {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err = parseTranscript("codex", strings.NewReader("{}\n")); err == nil || !strings.Contains(err.Error(), "no parseable token usage") {
		t.Fatalf("missing usage: %v", err)
	}
}
