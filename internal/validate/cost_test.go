package validate

import (
	"os"
	"path/filepath"

	"testing"
)

// costCodexFixture mirrors the current real Codex rollout token_count shape
// with incremental last_token_usage; the third event is a duplicate used to
// prove event dedupe at the ledger level.
const costCodexFixture = `{"timestamp":"2026-07-19T23:17:10.000Z","type":"session_meta","payload":{"session_id":"s-1"}}
{"timestamp":"2026-07-19T23:17:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"model_context_window":353400}}}
{"timestamp":"2026-07-19T23:17:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":250,"cached_input_tokens":25,"cache_write_input_tokens":3,"output_tokens":80,"reasoning_output_tokens":12,"total_tokens":330},"last_token_usage":{"input_tokens":150,"cached_input_tokens":15,"cache_write_input_tokens":3,"output_tokens":50,"reasoning_output_tokens":7,"total_tokens":200}}}}
{"timestamp":"2026-07-19T23:17:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":150,"cached_input_tokens":15,"cache_write_input_tokens":3,"output_tokens":50,"reasoning_output_tokens":7,"total_tokens":200}}}}
`

func writeCostFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(costCodexFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
