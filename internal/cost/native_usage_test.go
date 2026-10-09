package cost

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const dshMessageUsage = `{"sessionId":"session","event":{"seq":15,"type":"assistant/message","data":{"usage":{"inputTokens":7875,"cacheReadTokens":768,"outputTokens":205,"reasoningTokens":9,"totalTokens":8848}}}}` + "\n"
const dshCachedMessageUsage = `{"sessionId":"session","event":{"seq":22,"type":"assistant/message","data":{"usage":{"inputTokens":3728,"cacheReadTokens":8832,"outputTokens":100,"reasoningTokens":6,"totalTokens":12660}}}}` + "\n"
const zcodeTurnUsage = `{"sessionId":"session","turnId":"turn","seq":1658,"type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":8,"inputTokens":304400,"cacheReadTokens":270848,"outputTokens":23243,"reasoningTokens":0,"totalTokens":327643}}}` + "\n"

func TestNativeUsagePreservesHostTokenCategoriesAndReplay(t *testing.T) {
	for _, tc := range []struct {
		provider string
		content  string
		want     Usage
	}{
		{"dsh", dshMessageUsage + dshCachedMessageUsage + dshMessageUsage, Usage{InputCacheHitTokens: 9600, InputCacheMissTokens: 11603, OutputTokens: 305}},
		{"zcode", zcodeTurnUsage + zcodeTurnUsage, Usage{InputCacheHitTokens: 270848, InputCacheMissTokens: 33552, OutputTokens: 23243}},
		{"zcode", zcodeTurnUsage + strings.ReplaceAll(zcodeTurnUsage, `"turnId":"turn"`, `"turnId":"another"`), Usage{InputCacheHitTokens: 541696, InputCacheMissTokens: 67104, OutputTokens: 46486}},
		{"zcode", strings.ReplaceAll(zcodeTurnUsage, "turn.completed", "turn.failed"), Usage{InputCacheHitTokens: 270848, InputCacheMissTokens: 33552, OutputTokens: 23243}},
		{"dsh", `{"sessionId":"session","event":{"seq":23,"type":"compaction/summary","data":{"llmStreamCall":true,"usage":{"inputTokens":7,"cacheReadTokens":11,"outputTokens":5}}}}` + "\n", Usage{InputCacheHitTokens: 11, InputCacheMissTokens: 7, OutputTokens: 5}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			got, err := ParseTranscript(tc.provider, writeFixture(t, tc.content))
			if err != nil || got != tc.want {
				t.Fatalf("usage=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}

func TestNativeUsageKeepsIncompleteSourcesUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, provider, content string
	}{
		{"DSH missing cache", "dsh", strings.ReplaceAll(dshMessageUsage, `"cacheReadTokens":768,`, "")},
		{"DSH missing identity", "dsh", strings.ReplaceAll(dshMessageUsage, `"seq":15,`, "")},
		{"DSH interrupted attempt", "dsh", dshMessageUsage + `{"sessionId":"session","event":{"type":"assistant/attempt","seq":16,"data":{"usage":{"inputTokens":3}}}}`},
		{"DSH conflicting replay", "dsh", dshMessageUsage + strings.ReplaceAll(dshMessageUsage, `"inputTokens":7875`, `"inputTokens":7876`)},
		{"DSH mixed sessions", "dsh", dshMessageUsage + strings.ReplaceAll(dshCachedMessageUsage, `"sessionId":"session"`, `"sessionId":"other"`)},
		{"ZCode missing usage", "zcode", zcodeTurnUsage + `{"sessionId":"session","turnId":"another","type":"turn.completed","payload":{}}`},
		{"ZCode missing input", "zcode", strings.ReplaceAll(zcodeTurnUsage, `"inputTokens":304400,`, "")},
		{"ZCode no backend", "zcode", strings.ReplaceAll(zcodeTurnUsage, `"modelRequestCount":8`, `"modelRequestCount":0`)},
		{"ZCode absent provider", "zcode", strings.ReplaceAll(zcodeTurnUsage, `"source":"provider"`, `"source":"unavailable"`)},
		{"ZCode cache beyond input", "zcode", strings.ReplaceAll(zcodeTurnUsage, `"cacheReadTokens":270848`, `"cacheReadTokens":304401`)},
		{"ZCode conflicting replay", "zcode", zcodeTurnUsage + strings.ReplaceAll(zcodeTurnUsage, `"outputTokens":23243`, `"outputTokens":23244`)},
		{"ZCode mixed sessions", "zcode", zcodeTurnUsage + strings.ReplaceAll(zcodeTurnUsage, `"sessionId":"session"`, `"sessionId":"other"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTranscript(tc.provider, writeFixture(t, tc.content))
			if err == nil || got != (Usage{}) {
				t.Fatalf("unavailable source produced usage=%+v err=%v", got, err)
			}
		})
	}
}

func TestNativeOwnerIntervalUsesPersistedUsageBaseline(t *testing.T) {
	for _, tc := range []struct {
		provider, start, continuation string
		want                          Usage
	}{
		{"dsh", dshMessageUsage, dshCachedMessageUsage, Usage{InputCacheHitTokens: 8832, InputCacheMissTokens: 3728, OutputTokens: 100}},
		{"zcode", zcodeTurnUsage, strings.ReplaceAll(zcodeTurnUsage, `"turnId":"turn"`, `"turnId":"another"`), Usage{InputCacheHitTokens: 270848, InputCacheMissTokens: 33552, OutputTokens: 23243}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			path := writeFixture(t, tc.start)
			baseline, err := CaptureBaseline(tc.provider, path)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var restored TranscriptSnapshot
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.start+tc.continuation), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := MeasureOwner(tc.provider, path, &restored)
			if err != nil || got != tc.want {
				t.Fatalf("owner interval=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}
