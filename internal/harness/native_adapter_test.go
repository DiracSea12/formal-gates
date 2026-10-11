package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNativeAdapterEntriesImplementWholeBoundary(t *testing.T) {
	seen := make(map[string]bool)
	for _, entry := range nativeImplementations {
		if seen[entry.provider] {
			t.Fatalf("duplicate provider %s", entry.provider)
		}
		seen[entry.provider] = true
		adapter := entry.nativeImplementation
		if adapter.capture == nil || adapter.validateBinding == nil || adapter.verifySource == nil || adapter.verifyStart == nil || adapter.reconcile == nil || adapter.execute == nil {
			t.Fatalf("incomplete provider %s", entry.provider)
		}
	}
}

func TestNativeAdapterRegistrationRoutesExecutionAndRecovery(t *testing.T) {
	original := nativeImplementations
	t.Cleanup(func() { nativeImplementations = original })
	directory := t.TempDir()
	calls := []string{}
	entry := nativeImplementations[0]
	entry.provider = "native-peer"
	binding := map[string]any{"identity": "binary-identity"}
	source := map[string]any{"session": "session-identity"}
	process := map[string]any{"attempt": "attempt-identity"}
	entry.nativeImplementation = nativeImplementation{
		capture: func() (map[string]any, error) { calls = append(calls, "capture"); return binding, nil },
		validateBinding: func(got any) error {
			if !reflect.DeepEqual(got, binding) {
				t.Fatalf("binding = %#v", got)
			}
			calls = append(calls, "validate")
			return nil
		},
		verifySource: func(dir string, got any) error {
			if dir != directory || !reflect.DeepEqual(got, source) {
				t.Fatalf("source = %s %#v", dir, got)
			}
			calls = append(calls, "verify")
			return nil
		},
		verifyStart: func(request, acknowledgement any, identity string) error {
			if getOf(request, "prompt") != "exact prompt" || acknowledgement != "native-ack" || identity != "session-identity" {
				t.Fatalf("start = %#v %#v %s", request, acknowledgement, identity)
			}
			calls = append(calls, "start")
			return nil
		},
		reconcile: func(dir string, got any) (map[string]any, error) {
			if dir != directory || !reflect.DeepEqual(got, process) {
				t.Fatalf("process = %s %#v", dir, got)
			}
			calls = append(calls, "reconcile")
			return source, nil
		},
		execute: func(got any, dir string, probe bool, services ...ExecutionServices) (map[string]any, error) {
			if dir != directory || !probe || getOf(got, "prompt") != "exact prompt" {
				t.Fatalf("execute input = %#v %s %v", got, dir, probe)
			}
			calls = append(calls, "execute")
			return source, nil
		},
	}
	nativeImplementations = append(append(original[:0:0], original...), entry)
	adapter, err := nativeImplementationFor("native-peer")
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.capture()
	if err != nil || !reflect.DeepEqual(got, binding) {
		t.Fatalf("capture = %#v %v", got, err)
	}
	raw := func(v any) json.RawMessage {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if err := adapter.validateBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err := adapter.verifySource(directory, source); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": "native-peer", "binding": binding, "prompt": "exact prompt", "probe": true}
	if err := adapter.verifyStart(request, "native-ack", "session-identity"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), raw(request), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = runNativeJob(directory)
	if err != nil || !reflect.DeepEqual(got, source) {
		t.Fatalf("execute = %#v %v", got, err)
	}
	got, err = adapter.reconcile(directory, process)
	if err != nil || !reflect.DeepEqual(got, source) {
		t.Fatalf("reconcile = %#v %v", got, err)
	}
	if !reflect.DeepEqual(calls, []string{"capture", "validate", "verify", "start", "validate", "execute", "reconcile"}) {
		t.Fatalf("boundary calls = %v", calls)
	}
}
