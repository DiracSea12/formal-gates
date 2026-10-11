package harness

import (
	"encoding/json"
	"formal-gates/internal/host"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNativeAdapterEntriesImplementWholeBoundary(t *testing.T) {
	seen := make(map[string]bool)
	for _, entry := range host.RegisteredAdapters() {
		capability, ok := entry.(nativeCapability)
		if !ok {
			continue
		}
		provider := capability.HostDescriptor().InstallName
		if seen[provider] {
			t.Fatalf("duplicate provider %s", provider)
		}
		seen[provider] = true
		adapter := capability.nativeBoundary()
		if adapter.capture == nil || adapter.captureConfigured == nil || adapter.validateBinding == nil || adapter.verifySource == nil || adapter.verifyStart == nil || adapter.reconcile == nil || adapter.execute == nil {
			t.Fatalf("incomplete provider %s", provider)
		}
	}

}

func TestNativeAdapterRegistrationRoutesExecutionAndRecovery(t *testing.T) {
	if os.Getenv("FORMAL_GATES_NATIVE_PEER") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativeAdapterRegistrationRoutesExecutionAndRecovery$", "-test.timeout=0", "-test.v")
		cmd.Env = append(os.Environ(), "FORMAL_GATES_NATIVE_PEER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("registered platform: %s %v", out, err)
		}
		return
	}
	directory := t.TempDir()
	calls := []string{}
	entry := nativePlatform{Descriptor: &host.Descriptor{ID: "native-peer", InstallName: "native-peer", Aliases: []string{"native-peer-alias"}}}
	binding := map[string]any{"identity": "binary-identity"}
	source := map[string]any{"session": "session-identity"}
	process := map[string]any{"attempt": "attempt-identity"}
	entry.implementation = nativeImplementation{
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
	if err := registerNativePlatform(entry.Descriptor, entry.implementation); err != nil {
		t.Fatal(err)
	}
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
