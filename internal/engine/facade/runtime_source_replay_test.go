package facade

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func sourceReplayJob(t *testing.T, content []byte) string {
	t.Helper()
	job := t.TempDir()
	if err := os.WriteFile(filepath.Join(job, "stdout.jsonl"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(job, "process.json"), map[string]any{"actionId": filepath.Base(job)}); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestNativeSourceAppendRecoversObservedCopy(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "copy-finished-before-marker"
		if partial {
			name = "copy-interrupted-midstream"
		}
		t.Run(name, func(t *testing.T) {
			binding := &protocol.HostRuntimeContext{Provider: "fixture", Environment: map[string]string{nativeSourceRoot: t.TempDir()}}
			prefix := []byte("{\"observation\":\"previous turn\"}\n")
			current := []byte("{\"observation\":\"current completed turn\"}\n")
			if _, err := appendStreamTranscript(sourceReplayJob(t, prefix), "fixture-session", "stdout.jsonl", binding); err != nil {
				t.Fatal(err)
			}
			job := sourceReplayJob(t, current)
			path, err := appendStreamTranscript(job, "fixture-session", "stdout.jsonl", binding)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate the persisted boundary after the source copy but before
			// its completion marker. This is an isolated native journal fixture,
			// not a workflow state edit or another native model invocation.
			raw, err := os.ReadFile(filepath.Join(job, "process.json"))
			if err != nil {
				t.Fatal(err)
			}
			var process map[string]any
			if err := json.Unmarshal(raw, &process); err != nil {
				t.Fatal(err)
			}
			delete(process, "transcriptPath")
			if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
				t.Fatal(err)
			}
			if partial {
				if err := os.Truncate(path, int64(len(prefix)+len(current)/2)); err != nil {
					t.Fatal(err)
				}
			}
			for replay := 0; replay < 2; replay++ {
				if _, err := appendStreamTranscript(job, "fixture-session", "stdout.jsonl", binding); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, append(append([]byte(nil), prefix...), current...)) {
					t.Fatalf("source replay duplicated or corrupted observations: %q (%v)", got, err)
				}
			}
		})
	}
}

func TestNativeSourceDistinctIdenticalTurnsRemainDistinct(t *testing.T) {
	binding := &protocol.HostRuntimeContext{Provider: "fixture", Environment: map[string]string{nativeSourceRoot: t.TempDir()}}
	content := []byte("{\"observation\":\"same native response\"}\n")
	var path string
	for call := 0; call < 2; call++ {
		var err error
		path, err = appendStreamTranscript(sourceReplayJob(t, content), "fixture-session", "stdout.jsonl", binding)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, bytes.Repeat(content, 2)) {
		t.Fatal("different native invocations were deduplicated by content instead of their journals", err)
	}
}
