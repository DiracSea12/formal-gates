package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/settings"
)

// The local platform binds real subprocess output and resume identity through
// the same registered object used by configuration, lifecycle and metering.
func TestNativePlatformRegistrationCarriesAllPublicCapabilities(t *testing.T) {
	if os.Getenv("FORMAL_GATES_PLATFORM_PEER") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativePlatformRegistrationCarriesAllPublicCapabilities$", "-test.timeout=0", "-test.v")
		cmd.Env = append(os.Environ(), "FORMAL_GATES_PLATFORM_PEER=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("public platform: %s %v", out, err)
		}
		t.Logf("registered public capability facts:\n%s", out)
		return
	}
	root, home := t.TempDir(), t.TempDir()
	binary := filepath.Join(root, "peer")
	version := 1
	valueKey := func() string {
		if version == 2 {
			return "answer"
		}
		return "value"
	}
	writePeer := func() {
		t.Helper()
		body := fmt.Sprintf("#!/bin/sh\nprintf '{\"identity\":\"%%s\",\"%s\":\"%%s\",\"input\":%%s,\"output\":%%s}\\n' \"${2:-peer-session}\" \"$1\" \"${#1}\" %d\nprintf 'stderr-%%s\\n' \"$1\" >&2\n", valueKey(), version)
		if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writePeer()
	capture := func(config settings.Values) (map[string]any, error) {
		return map[string]any{"executable": config.Native["peerExecutable"]}, nil
	}
	decode := func(directory string) (map[string]any, error) {
		value, err := readJSONFile(filepath.Join(directory, "stdout.log"))
		if err != nil {
			return nil, err
		}
		return value.(map[string]any), nil
	}
	descriptor := &host.Descriptor{ID: "native-platform-peer", InstallName: "native-platform-peer", Aliases: []string{"peer-alias"}, Configuration: []host.ConfigField{host.StringConfig("peerExecutable", binary)}, LifecycleRequired: true, LifecycleEvents: []string{"Started", "Finished"}}
	descriptor.ReadUsage = func(reader io.Reader) (host.Usage, error) {
		var value map[string]any
		if err := json.NewDecoder(reader).Decode(&value); err != nil {
			return host.Usage{}, err
		}
		if value[valueKey()] == nil {
			return host.Usage{}, fmt.Errorf("peer output format differs")
		}
		return host.Usage{InputCacheMissTokens: int64(value["input"].(float64)), OutputTokens: int64(value["output"].(float64))}, nil
	}
	descriptor.Lifecycle = &host.Lifecycle{Name: descriptor.ID, NormalizeEvent: func(event string) (string, error) {
		switch event {
		case "Started":
			return "subagent_start", nil
		case "Finished":
			return "subagent_stop", nil
		}
		return "", fmt.Errorf("unknown peer event %q", event)
	}, Identity: func(_ string, payload any) string { return textOf(getOf(payload, "identity")) }, Correlation: func(string, any) string { return "" }, ProjectRoots: func(any) []string { return []string{root} }}
	implementation := nativeImplementation{
		capture: func() (map[string]any, error) {
			return capture(settings.Values{Native: map[string]string{"peerExecutable": binary}})
		}, captureConfigured: capture,
		validateBinding: func(binding any) error {
			if getOf(binding, "executable") != binary {
				return fmt.Errorf("peer executable differs")
			}
			_, err := os.Stat(binary)
			return err
		},
		verifyStart: func(request, ack any, identity string) error {
			if getOf(ack, "identity") != identity || getOf(ack, valueKey()) != getOf(request, "prompt") {
				return fmt.Errorf("peer acknowledgement differs")
			}
			return nil
		},
		verifySource: func(directory string, source any) error {
			actual, err := decode(directory)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(actual, source) {
				return fmt.Errorf("peer source differs from original output")
			}
			return nil
		},
		reconcile: func(directory string, process any) (map[string]any, error) {
			if !valuesEqual(getOf(process, "exit_code"), 0) {
				return nil, fmt.Errorf("peer process failed")
			}
			return decode(directory)
		},
		execute: func(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
			if probe {
				return map[string]any{"protocol": version}, nil
			}
			command := exec.Command(textOf(getOf(getOf(request, "binding"), "executable")), textOf(getOf(request, "prompt")), textOf(getOf(request, "identity")))
			command.Dir = root
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Run(); err != nil {
				return nil, err
			}
			for name, body := range map[string][]byte{"stdout.log": stdout.Bytes(), "stderr.log": stderr.Bytes()} {
				if err := os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
					return nil, err
				}
			}
			if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
				return nil, err
			}
			source, err := decode(directory)
			if err != nil {
				return nil, err
			}
			if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"identity": source["identity"], "workspace": root, "acknowledgement": source}); err != nil {
				return nil, err
			}
			if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
				return nil, err
			}
			return source, nil
		},
	}
	// No other registry or core file needs an entry for this platform.
	if err := registerNativePlatform(descriptor, implementation); err != nil {
		t.Fatal(err)
	}
	original, err := host.LookupAdapter("peer-alias")
	if err != nil || original.HostDescriptor() != descriptor {
		t.Fatal("registration copied descriptor", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(root, ".gates", "config.json"), map[string]any{"peerExecutable": binary}); err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil || effective.Values.Native["peerExecutable"] != binary {
		t.Fatal("public configuration", effective, err)
	}
	binding, err := NativeBinding(descriptor.InstallName, effective.Values)
	if err != nil || binding["executable"] != binary {
		t.Fatal("configured native capture", binding, err)
	}
	run := func(prompt, identity string, probe bool) (string, map[string]any) {
		t.Helper()
		directory := t.TempDir()
		request := map[string]any{"provider": descriptor.InstallName, "binding": binding, "prompt": prompt, "identity": identity, "workspace": root, "probe": probe}
		if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
			t.Fatal(err)
		}
		var errors bytes.Buffer
		if code := RunNative(directory, &errors); code != 0 {
			t.Fatalf("native public run %d: %s", code, &errors)
		}
		if probe {
			return directory, nil
		}
		source, err := NativeJobSource(descriptor.InstallName, directory)
		if err != nil {
			t.Fatal(err)
		}
		actualIdentity, err := NativeStartEvidence(descriptor.InstallName, directory)
		if err != nil || actualIdentity != "peer-session" {
			t.Fatal("original start/resume identity", actualIdentity, err)
		}
		if source[valueKey()] != prompt || source["identity"] != "peer-session" {
			t.Fatal("different request lost its original output", source)
		}
		stderr, err := os.ReadFile(filepath.Join(directory, "stderr.log"))
		if err != nil || string(stderr) != "stderr-"+prompt+"\n" {
			t.Fatal("original stderr", string(stderr), err)
		}
		usage, err := cost.ParseTranscript("peer-alias", filepath.Join(directory, "stdout.log"))
		if err != nil || usage.InputCacheMissTokens != int64(len(prompt)) || usage.OutputTokens != int64(version) {
			t.Fatal("original metering", usage, err)
		}
		if err := os.Remove(filepath.Join(directory, "source.json")); err != nil {
			t.Fatal(err)
		}
		recovered, err := NativeJobSource(descriptor.InstallName, directory)
		if err != nil || !reflect.DeepEqual(source, recovered) {
			t.Fatal("lost response changed source", recovered, err)
		}
		return directory, source
	}
	run("capability-probe", "", true)
	first, one := run("alpha", "", false)
	_, two := run("different-output", "peer-session", false)
	if reflect.DeepEqual(one, two) {
		t.Fatal("fixture hides different native requests")
	}
	if err := lifecycle.BeginRun(root, "peer-run"); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"identity":"peer-session"}`)
	if _, err := lifecycle.Capture(root, "peer-alias", "Started", payload); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BindDispatchWithProvider(root, "peer-run", "peer-dispatch", "peer-session", descriptor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Capture(root, descriptor.ID, "Finished", payload); err != nil {
		t.Fatal(err)
	}
	verified, err := lifecycle.VerifyDispatch(root, "peer-run", "peer-dispatch")
	if err != nil || verified.Outcome != lifecycle.Verified || verified.Identity != "peer-session" {
		t.Fatal("public lifecycle identity", verified, err)
	}
	if _, err := NativeBinding("unknown-platform"); err == nil {
		t.Fatal("unknown execution capability accepted")
	}
	if _, err := cost.ParseTranscript("cursor", filepath.Join(first, "stdout.log")); err == nil {
		t.Fatal("missing usage capability fabricated")
	}
	// A format update modifies this adapter and its native protocol only.
	version = 2
	writePeer()
	_, updated := run("updated-format", "peer-session", false)
	if updated["value"] != nil || updated["answer"] != "updated-format" {
		t.Fatal("updated protocol not used", updated)
	}
	if current, err := host.LookupAdapter(descriptor.ID); err != nil || current != original {
		t.Fatal("update requires another registration", current, err)
	}
}
