package cost

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestActiveDispatchCumulativeUsagePreservesOtherCosts(t *testing.T) {
	run := &RunCost{}
	other := DispatchCost{Target: "other", Kind: "gate", Source: SourceTranscript, TotalInputTokens: 10, InputCacheHitTokens: 3, InputCacheMissTokens: 7, OutputTokens: 2}
	Record(run, "other", other)
	owner := OwnerCost{Source: SourceTranscript, TotalInputTokens: 5, InputCacheMissTokens: 5, OutputTokens: 1}
	RecordOwner(run, owner)
	first := DispatchCost{Target: "task", Kind: "action", Source: SourceTranscript, TotalInputTokens: 20, InputCacheHitTokens: 6, InputCacheMissTokens: 14, OutputTokens: 4}
	RecordCumulative(run, "active", first)
	second := first
	second.TotalInputTokens = 40
	second.InputCacheHitTokens = 12
	second.InputCacheMissTokens = 28
	second.OutputTokens = 8
	RecordCumulative(run, "active", second)
	RecordCumulative(run, "active", second)
	if len(run.Dispatches) != 2 || run.TotalInputTokens != 55 || run.InputCacheHitTokens != 15 || run.InputCacheMissTokens != 40 || run.OutputTokens != 11 || !reflect.DeepEqual(run.Dispatches["other"], other) || !reflect.DeepEqual(*run.Owner, owner) {
		t.Fatalf("cumulative replacement changed unrelated or repeated costs: %#v", run)
	}
	frozen, _ := json.Marshal(run)
	Record(run, "active", first)
	after, _ := json.Marshal(run)
	if string(frozen) != string(after) {
		t.Fatal("ordinary final Record stopped being immutable")
	}
}

func TestUnavailableContinuationRetainsExactMeasuredPrefix(t *testing.T) {
	run := &RunCost{}
	measured := DispatchCost{Target: "task", Kind: "action", Source: SourceTranscript, TotalInputTokens: 20, InputCacheHitTokens: 6, InputCacheMissTokens: 14, OutputTokens: 4, TranscriptPath: "original-source"}
	RecordCumulative(run, "active", measured)
	unavailable := DispatchCost{Target: measured.Target, Kind: measured.Kind, Source: SourceUnavailable, Reason: "continuation source has no complete usage record"}
	RecordCumulative(run, "active", unavailable)
	entry := run.Dispatches["active"]
	if entry.Source != SourceUnavailable || entry.Reason != unavailable.Reason || entry.TotalInputTokens != 20 || entry.InputCacheHitTokens != 6 || entry.InputCacheMissTokens != 14 || entry.OutputTokens != 4 || entry.TranscriptPath != measured.TranscriptPath || run.TotalInputTokens != 20 || run.OutputTokens != 4 {
		t.Fatalf("unavailable continuation erased exact usage/source qualification: %#v", run)
	}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var reopened RunCost
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*run, reopened) {
		t.Fatal("persisted unavailable prefix lost original exact numbers")
	}
	recovered := measured
	recovered.TotalInputTokens = 40
	recovered.InputCacheHitTokens = 12
	recovered.InputCacheMissTokens = 28
	recovered.OutputTokens = 8
	RecordCumulative(&reopened, "active", recovered)
	if reopened.TotalInputTokens != 40 || reopened.OutputTokens != 8 || reopened.Dispatches["active"].Source != SourceTranscript {
		t.Fatal("genuine cumulative source did not restore exact accounting")
	}
}

func TestUnavailableCumulativeUsageRecordsKnownContinuationOnce(t *testing.T) {
	run := &RunCost{}
	entry := DispatchCost{Target: "task", Kind: "action", Source: SourceUnavailable, Reason: "original invocation has no provider usage"}
	RecordCumulative(run, "active", entry)
	entry.TotalInputTokens, entry.InputCacheHitTokens, entry.InputCacheMissTokens, entry.OutputTokens = 35830, 15104, 20726, 10780
	RecordCumulative(run, "active", entry)
	RecordCumulative(run, "active", entry)
	if run.Dispatches["active"] != entry || run.TotalInputTokens != 35830 || run.InputCacheHitTokens != 15104 || run.InputCacheMissTokens != 20726 || run.OutputTokens != 10780 {
		t.Fatalf("known incomplete cumulative usage was lost or duplicated: %#v", run)
	}
	unproven := entry
	unproven.TotalInputTokens, unproven.InputCacheHitTokens, unproven.InputCacheMissTokens, unproven.OutputTokens = 0, 0, 0, 0
	RecordCumulative(run, "active", unproven)
	if run.Dispatches["active"] != entry || run.TotalInputTokens != 35830 || run.OutputTokens != 10780 {
		t.Fatal("an unavailable source erased its exact measured prefix")
	}
	regressed := entry
	regressed.TotalInputTokens, regressed.InputCacheHitTokens, regressed.InputCacheMissTokens, regressed.OutputTokens = 40000, 10000, 30000, 12000
	RecordCumulative(run, "active", regressed)
	if run.Dispatches["active"] != entry {
		t.Fatal("a regressed source fabricated a tuple from different measurements")
	}
}
