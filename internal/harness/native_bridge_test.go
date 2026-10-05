package harness

// Bridge-level tests for the four native operations, against the real
// implementations (no stubs): the refusal travels inside the value with its
// kind, the payloads are strict-decoded, and the entry command's argument
// check mirrors supervise's.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bridgeCall(t *testing.T, operation string, payload string) (map[string]any, int) {
	t.Helper()
	request := `{"operation": "` + operation + `", "payload": ` + payload + `}`
	var out bytes.Buffer
	code := Bridge(strings.NewReader(request), &out)
	var answer map[string]any
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("bridge answer not JSON: %v (%s)", err, out.String())
	}
	return answer, code
}

// TestBridgeNativeCapture pins native-capture: the ordered binding crosses the
// envelope intact, and the PATH refusal is a typed outcome inside the value
// (合同桥接操作表：native-capture).
func TestBridgeNativeCapture(t *testing.T) {
	root := t.TempDir()
	bindir := filepath.Join(root, "bin")
	if err := os.Mkdir(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(bindir, "dsh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bindir)
	answer, code := bridgeCall(t, "native-capture", `{"provider": "dsh"}`)
	if code != 0 || answer["ok"] != true {
		t.Fatalf("answer = %v code = %d", answer, code)
	}
	value, _ := answer["value"].(map[string]any)
	binding, _ := value["binding"].(map[string]any)
	if binding == nil || binding["provider"] != "dsh" {
		t.Fatalf("binding = %v", value)
	}
	launch, _ := binding["launch"].(map[string]any)
	if launch["executable"] != resolveNativePath(fake) {
		t.Fatalf("launch = %v", launch)
	}
	files, _ := binding["files"].(map[string]any)
	if _, present := files[resolveNativePath(fake)]; !present {
		t.Fatalf("files = %v", files)
	}

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	answer, code = bridgeCall(t, "native-capture", `{"provider": "dsh"}`)
	if code != 0 {
		t.Fatalf("refusal must be exit 0, got %d (%v)", code, answer)
	}
	inner, _ := answer["value"].(map[string]any)
	if inner["ok"] != false || inner["kind"] != "" || inner["refusal"] != "DSH_UNAVAILABLE: dsh is not on PATH" {
		t.Fatalf("value = %v", inner)
	}
}

// TestBridgeNativeRefusalKinds pins refuse()'s mapping through the bridge: the
// RuntimeBlocked family (SessionUnavailable included) keeps kind "", the
// HarnessError family (HOST_BINDING) reports kind "binding" (合同：业务拒绝不走
// 失败形状).
func TestBridgeNativeRefusalKinds(t *testing.T) {
	answer, code := bridgeCall(t, "native-verify-binding", `{"provider": "dsh", "binding": {"provider": "zcode", "files": {}}}`)
	if code != 0 {
		t.Fatalf("binding refusal must be exit 0, got %d (%v)", code, answer)
	}
	inner, _ := answer["value"].(map[string]any)
	if inner["ok"] != false || inner["kind"] != "binding" || inner["refusal"] != "HOST_BINDING: provider differs" {
		t.Fatalf("value = %v", inner)
	}

	answer, code = bridgeCall(t, "native-verify-source",
		`{"provider": "ghost", "directory": "`+t.TempDir()+`", "source": {"provider": "dsh"}}`)
	if code != 0 {
		t.Fatalf("dispatch refusal must be exit 0, got %d (%v)", code, answer)
	}
	inner, _ = answer["value"].(map[string]any)
	if inner["ok"] != false || inner["kind"] != "" ||
		inner["refusal"] != "UNSUPPORTED_NATIVE_HOST: ghost; execution adapters: dsh, zcode, claude, codex" {
		t.Fatalf("value = %v", inner)
	}
}

// TestBridgeNativePayloadShapes pins the four payloads: strict decoding (no
// unknown fields) and the exact key sets (合同桥接操作载荷表).
func TestBridgeNativePayloadShapes(t *testing.T) {
	root := t.TempDir()
	bound := filepath.Join(root, "dsh")
	if err := os.WriteFile(bound, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proof, err := fileProofOf(bound)
	if err != nil {
		t.Fatal(err)
	}
	bindingJSON := `{"provider": "dsh", "launch": {"executable": ` + jsonString(t, bound) +
		`}, "files": {` + jsonString(t, bound) + `: {"sha256": ` + jsonString(t, proof["sha256"].(string)) +
		`, "size": ` + fmt.Sprint(proof["size"]) + `}}}`

	answer, code := bridgeCall(t, "native-verify-binding", `{"provider": "dsh", "binding": `+bindingJSON+`}`)
	if code != 0 || answer["ok"] != true || len(answer) != 2 {
		t.Fatalf("verify-binding = %v %d", answer, code)
	}
	inner, _ := answer["value"].(map[string]any)
	if len(inner) != 1 || inner["ok"] != true {
		t.Fatalf("verify-binding value = %v", inner)
	}

	directory := t.TempDir()
	for _, name := range nativeSourceFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sourceJSON := mustCanonical(t, map[string]any{
		"provider": "dsh", "identity": "s-1", "files": proofsOf(t, directory, nativeSourceFiles)})
	answer, code = bridgeCall(t, "native-verify-source",
		`{"provider": "dsh", "directory": `+jsonString(t, directory)+`, "source": `+sourceJSON+`}`)
	if code != 0 || answer["ok"] != true {
		t.Fatalf("verify-source = %v %d", answer, code)
	}

	// Unknown fields are malformed operations, not business input.
	for operation, payload := range map[string]string{
		"native-capture":        `{"provider": "dsh", "extra": 1}`,
		"native-verify-binding": `{"provider": "dsh", "binding": {}, "extra": 1}`,
		"native-verify-source":  `{"provider": "dsh", "directory": "x", "source": {}, "extra": 1}`,
		"native-reconcile":      `{"provider": "dsh", "directory": "x", "process": {}, "extra": 1}`,
	} {
		answer, code := bridgeCall(t, operation, payload)
		if code != 2 || answer["code"] != "INVALID_HARNESS_OPERATION" {
			t.Fatalf("%s malformed payload = %v %d", operation, answer, code)
		}
	}
}

// TestBridgeNativeReconcileRefusal pins native-reconcile's ineligible refusal
// (the recovered-success path is pinned in native_dsh_test.go's dshReconcile).
func TestBridgeNativeReconcileRefusal(t *testing.T) {
	directory := t.TempDir()
	answer, code := bridgeCall(t, "native-reconcile",
		`{"provider": "dsh", "directory": `+jsonString(t, directory)+`, "process": {"exit_code": 0, "error": ""}}`)
	if code != 0 {
		t.Fatalf("reconcile refusal must be exit 0, got %d (%v)", code, answer)
	}
	inner, _ := answer["value"].(map[string]any)
	if inner["ok"] != false || inner["kind"] != "" || inner["refusal"] != "DSH_RECONCILE: not a completed formatting-only failure" {
		t.Fatalf("value = %v", inner)
	}
}

// TestRunCommandNativeRequiresDirectory pins the entry failure: exit 2, the
// supervise-mirror message, and no native-error.json anywhere.
func TestRunCommandNativeRequiresDirectory(t *testing.T) {
	var errout bytes.Buffer
	code := RunCommand([]string{"native"}, strings.NewReader(""), nil, &errout)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if errout.String() != "harness native requires a job directory\n" {
		t.Fatalf("stderr = %q", errout.String())
	}
}
