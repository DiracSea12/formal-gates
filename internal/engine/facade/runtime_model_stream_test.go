package facade

import (
	"errors"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamModelSourceUnavailableAndFailedUsage(t *testing.T) {
	for _, usage := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			job := t.TempDir()
			binding := &protocol.HostRuntimeContext{Provider: "workbuddy", Environment: map[string]string{nativeSourceRoot: filepath.Join(job, "sources")}}
			op := protocol.TaskOperation{ID: "model", Request: protocol.TaskOperationRequest{Binding: protocol.ExecutionClaim{Provider: "workbuddy", Context: binding}}}
			process := map[string]any{"actionId": "model", "identity": "native", "stopped": true, "exitCode": 0}
			if failed {
				process["exitCode"] = 7
			}
			if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(job, "answer.json"), []byte(`{"output":"answer"}`), 0600); err != nil {
				t.Fatal(err)
			}
			stream := `{"type":"system","session_id":"native","subtype":"init"}` + "\n"
			if usage {
				stream += `{"type":"assistant","uuid":"message","session_id":"native","message":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n"
			}
			if err := os.WriteFile(filepath.Join(job, "stdout.jsonl"), []byte(stream), 0600); err != nil {
				t.Fatal(err)
			}
			var executionErr error
			if failed {
				executionErr = errors.New("native exit 7")
			}
			got := completeStreamModel(op, job, "stdout.jsonl", protocol.TaskOperationReceipt{}, executionErr)
			if (got.Status == "FAILED") != failed || got.Identity != "native" {
				t.Fatalf("%+v", got)
			}
			if (got.TranscriptPath != "") != usage {
				t.Fatalf("usage=%v got=%+v", usage, got)
			}
			if !usage && !strings.Contains(got.Message, "Usage source unavailable") {
				t.Fatal("missing source diagnosis")
			}
			if !failed && got.Output != "answer" {
				t.Fatal("source gap discarded completed answer")
			}
		}
	}
}
