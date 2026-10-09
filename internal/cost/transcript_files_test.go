package cost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTranscriptFilesUseOriginalProviderDeduplication(t *testing.T) {
	cases := []struct {
		provider string
		streams  []string
		want     Usage
	}{
		{"zcode", []string{`{"sessionId":"session","turnId":"one","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":100,"cacheReadTokens":40,"outputTokens":10}}}`, `{"sessionId":"session","turnId":"two","type":"turn.failed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":80,"cacheReadTokens":20,"outputTokens":5}}}`}, Usage{InputCacheHitTokens: 60, InputCacheMissTokens: 120, OutputTokens: 15}},
		{"deepseek-harness", []string{`{"sessionId":"session","event":{"seq":1,"type":"assistant/message","data":{"usage":{"inputTokens":30,"cacheReadTokens":70,"outputTokens":10}}}}`, `{"sessionId":"session","event":{"seq":2,"type":"assistant/message","data":{"usage":{"inputTokens":20,"cacheReadTokens":60,"outputTokens":5}}}}`}, Usage{InputCacheHitTokens: 130, InputCacheMissTokens: 50, OutputTokens: 15}},
		{"codex", []string{`{"type":"token_usage_record","payload":{"response_id":"response","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10},"thread_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10}}}`}, Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 60, OutputTokens: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			root := t.TempDir()
			paths := []string{}
			for index, content := range tc.streams {
				path := filepath.Join(root, string(rune('a'+index))+".jsonl")
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			paths = append(paths, paths[0])
			usage, err := ParseTranscriptFiles(tc.provider, paths...)
			if err != nil || usage != tc.want {
				t.Fatalf("original multi-stream/dedup parse: %#v %v want %#v", usage, err, tc.want)
			}
		})
	}
}
