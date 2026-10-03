package cost

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const ownerCodexStart = `{"timestamp":"start","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10}}}}
`
const ownerCodexEnd = `{"timestamp":"end","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":25,"output_tokens":14}}}}
`

func TestOwnerIntervalUsesBothExistingParsers(t *testing.T) {
	for _, tc := range []struct{ provider, start, end string }{
		{"codex", ownerCodexStart, ownerCodexEnd},
		{"claude", `{"type":"assistant","uuid":"before","message":{"usage":{"input_tokens":80,"cache_read_input_tokens":20,"output_tokens":10}}}` + "\n",
			`{"type":"assistant","uuid":"after","message":{"usage":{"input_tokens":55,"cache_read_input_tokens":5,"output_tokens":4}}}` + "\n"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			path := writeFixture(t, tc.start)
			baseline, err := CaptureBaseline(tc.provider, path)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate process restart: only the small JSON baseline survives.
			data, err := json.Marshal(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var restored TranscriptSnapshot
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.start+tc.end), 0o600); err != nil {
				t.Fatal(err)
			}
			usage, err := MeasureOwner(tc.provider, path, &restored)
			want := Usage{InputCacheHitTokens: 5, InputCacheMissTokens: 55, OutputTokens: 4}
			if err != nil || usage != want {
				t.Fatalf("delta=%+v err=%v, want %+v", usage, err, want)
			}
		})
	}
}

func TestOwnerBaselineAndEndMustBeVerifiable(t *testing.T) {
	for _, tc := range []struct{ name, provider, content string }{
		{"missing provider", "", ownerCodexStart},
		{"unsupported provider", "cursor", ownerCodexStart},
		{"missing usage", "codex", "{}\n"},
		{"unparseable", "codex", "not json\n"},
		{"partial tail", "codex", ownerCodexStart + `{"type":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CaptureBaseline(tc.provider, writeFixture(t, tc.content)); err == nil {
				t.Fatal("unverifiable baseline was accepted")
			}
		})
	}
	for _, name := range []string{"missing baseline", "missing source", "truncated", "rewritten", "longer rewrite", "partial tail", "unparseable end"} {
		t.Run(name, func(t *testing.T) {
			path := writeFixture(t, ownerCodexStart)
			baseline, err := CaptureBaseline("codex", path)
			if err != nil {
				t.Fatal(err)
			}
			content := ownerCodexStart + ownerCodexEnd
			switch name {
			case "missing baseline":
				baseline = nil
			case "missing source":
				path += ".missing"
			case "truncated":
				content = "{}\n"
			case "rewritten":
				content = strings.ReplaceAll(ownerCodexStart, "100", "200")
			case "longer rewrite":
				content = strings.ReplaceAll(content, "100", "200") + "{}\n"
			case "partial tail":
				content += `{"type":`
			case "unparseable end":
				content = "not json\n"
			}
			if name != "missing source" {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			usage, err := MeasureOwner("codex", path, baseline)
			if err == nil || usage != (Usage{}) {
				t.Fatalf("unverifiable interval produced usage=%+v err=%v", usage, err)
			}
		})
	}
}

func TestOwnerUnchangedTranscriptHasExactZeroUsage(t *testing.T) {
	path := writeFixture(t, ownerCodexStart)
	baseline, err := CaptureBaseline("codex", path)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := MeasureOwner("codex", path, baseline)
	if err != nil || usage != (Usage{}) {
		t.Fatalf("unchanged source usage=%+v err=%v", usage, err)
	}
}

func TestOwnerAndDispatchTotalsAreIndependentAndIdempotent(t *testing.T) {
	var run, child RunCost
	owner := OwnerCost{Source: SourceTranscript, TotalInputTokens: 60, InputCacheHitTokens: 5, InputCacheMissTokens: 55, OutputTokens: 4}
	dispatch := DispatchCost{Source: SourceTranscript, TotalInputTokens: 100, InputCacheHitTokens: 20, InputCacheMissTokens: 80, OutputTokens: 10}
	Record(&run, "dispatch", dispatch)
	RecordOwner(&run, owner)
	RecordOwner(&run, OwnerCost{Source: SourceUnavailable})
	Record(&run, "dispatch", dispatch)
	RecordOwner(&child, OwnerCost{Source: SourceTranscript, TotalInputTokens: 900, InputCacheMissTokens: 900})
	Record(&child, "dispatch", dispatch)
	Merge(&run, "child", &child)
	Merge(&run, "child", &child)
	if *run.Owner != owner || run.TotalInputTokens != 260 || run.InputCacheHitTokens != 45 || run.InputCacheMissTokens != 215 || run.OutputTokens != 24 || len(run.Dispatches) != 2 {
		t.Fatalf("cost projection double counted or merged child owner: %+v", run)
	}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var restored RunCost
	if err := json.Unmarshal(data, &restored); err != nil || !reflect.DeepEqual(run, restored) {
		t.Fatalf("projection did not round-trip: %+v, %v", restored, err)
	}
	RecordOwner(&restored, owner)
	if !reflect.DeepEqual(run, restored) {
		t.Fatal("restored projection re-added owner cost")
	}
}

func TestChildOwnerContributesWithoutReplacingThisRunsOwner(t *testing.T) {
	var run RunCost
	owner := OwnerCost{Source: SourceTranscript, TotalInputTokens: 60, InputCacheHitTokens: 5, InputCacheMissTokens: 55, OutputTokens: 4}
	child := OwnerCost{Source: SourceTranscript, TotalInputTokens: 17, InputCacheHitTokens: 2, InputCacheMissTokens: 15, OutputTokens: 8}
	RecordOwner(&run, owner)
	AddChildOwner(&run, &child)
	AddChildOwner(&run, &OwnerCost{Source: SourceUnavailable})
	AddChildOwner(&run, nil)
	if *run.Owner != owner || run.TotalInputTokens != 77 || run.InputCacheHitTokens != 7 || run.InputCacheMissTokens != 70 || run.OutputTokens != 12 || run.Dispatches != nil {
		t.Fatalf("child owner replaced this run's owner or dispatches: %+v", run)
	}
}
