package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

func TestDevinNativeExecutionResolves(t *testing.T) {
	impl, err := nativeImplementationFor("devin")
	if err != nil {
		t.Fatalf("devin must resolve as a native execution platform: %v", err)
	}
	if impl.resolvePaths == nil || impl.captureConfigured == nil || impl.verifySource == nil || impl.verifyStart == nil || impl.reconcile == nil || impl.execute == nil {
		t.Fatal("devin native implementation is incomplete")
	}
}

// devinInstallFixture builds a fake desktop bundle: the PATH anchor
// (devin-desktop) names the install root; the only executable the adapter may
// bind is the standalone agent inside resources. The Electron shell decoys
// are present to prove they are never picked.
func devinInstallFixture(t *testing.T) (installRoot, standalone string) {
	t.Helper()
	installRoot = resolveNativePath(t.TempDir())
	bin := filepath.Join(installRoot, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"devin-desktop", "devin-desktop.cmd"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("echo desktop\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	standalone = filepath.Join(installRoot, "resources", "app", "extensions", "windsurf", "devin", "bin", "devin.exe")
	if err := os.MkdirAll(filepath.Dir(standalone), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(standalone, []byte("agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The Electron shell: present in the bundle, never a candidate.
	if err := os.WriteFile(filepath.Join(installRoot, "Devin.exe"), []byte("electron"), 0o755); err != nil {
		t.Fatal(err)
	}
	return installRoot, standalone
}

// The PATH resolution binds the standalone ACP binary inside the desktop
// bundle; the Electron shell and the devin-desktop launcher are anchors, never
// candidates.
func TestDevinResolvePathsBindsTheStandaloneBinary(t *testing.T) {
	installRoot, standalone := devinInstallFixture(t)
	t.Setenv("PATH", filepath.Join(installRoot, "bin"))
	native, sources := map[string]string{}, map[string]string{}
	if err := devinResolvePaths(native, sources); err != nil {
		t.Fatalf("devinResolvePaths: %v", err)
	}
	if native["devinExecutable"] != resolveNativePath(standalone) {
		t.Fatalf("devinExecutable = %q, want the standalone agent %q", native["devinExecutable"], resolveNativePath(standalone))
	}
	if sources["devinExecutable"] != "desktop bundle" {
		t.Fatalf("devinExecutable source = %q", sources["devinExecutable"])
	}
}

// With neither the agent nor the desktop launcher on PATH the answer is the
// honest unavailable refusal, never a fabricated binding.
func TestDevinResolvePathsReportsUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	native, sources := map[string]string{}, map[string]string{}
	err := devinResolvePaths(native, sources)
	if blockedText(t, err) != "DEVIN_UNAVAILABLE: devin is not on PATH" {
		t.Fatalf("err = %v", err)
	}
	if native["devinExecutable"] != "" {
		t.Fatalf("a refused resolution must bind nothing: %v", native)
	}
}

// A configured executable path that does not exist fails at spawn with the
// real error - ACP was never spoken, so no identity or source may land.
func TestDevinExecuteMissingExecutableFailsAtSpawn(t *testing.T) {
	directory, workspace := t.TempDir(), t.TempDir()
	missing := filepath.Join(t.TempDir(), "devin.exe")
	request := map[string]any{
		"provider":         "devin",
		"binding":          map[string]any{"provider": "devin", "launch": map[string]any{"executable": missing}, "files": map[string]any{}},
		"workspace":        workspace,
		"read_only":        true,
		"identity":         "",
		"model":            "",
		"reasoning_effort": "",
		"prompt":           "Review the requirement.",
	}
	_, err := devinExecute(request, directory, false)
	if err == nil || !strings.Contains(err.Error(), "cannot start") {
		t.Fatalf("missing executable must fail at spawn, got %v", err)
	}
	for _, name := range []string{"identity.json", "source.json", "answer.json"} {
		if _, statErr := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(statErr) {
			t.Fatalf("a failed spawn must not land %s", name)
		}
	}
}

// The host-permission answer is ACP's own selected outcome over the host's
// offered allow option; a request without one is cancelled.
func TestDevinPermissionReplyShape(t *testing.T) {
	reply, err := devinRespondPermission("session/request_permission", map[string]any{
		"sessionId": "d-1", "toolCall": map[string]any{"toolCallId": "call-9"},
		"options": []any{
			map[string]any{"optionId": "reject-1", "name": "Reject", "kind": "reject_once"},
			map[string]any{"optionId": "allow-1", "name": "Allow once", "kind": "allow_once"},
		},
	})
	if err != nil || reply == nil {
		t.Fatalf("reply = %v, err = %v", reply, err)
	}
	if !valuesEqual(reply.payload, map[string]any{"result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow-1"}}}) {
		t.Fatalf("payload = %v", reply.payload)
	}
	if reply.status != "permission-selected" {
		t.Fatalf("status = %q", reply.status)
	}
	// kind wins over optionId text: allow_always is still an allow.
	reply, err = devinRespondPermission("session/request_permission", map[string]any{
		"options": []any{map[string]any{"optionId": "later", "kind": "allow_always"}},
	})
	if err != nil || !valuesEqual(getOf(getOf(getOf(reply.payload, "result"), "outcome"), "optionId"), "later") {
		t.Fatalf("allow_always must be picked: %v %v", reply, err)
	}
	reply, err = devinRespondPermission("session/request_permission", map[string]any{
		"options": []any{map[string]any{"optionId": "reject-1", "kind": "reject_once"}},
	})
	if err != nil || !valuesEqual(reply.payload, map[string]any{"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}}) ||
		reply.status != "permission-cancelled" {
		t.Fatalf("cancel payload = %v status = %q", reply.payload, reply.status)
	}
	if reply, err = devinRespondPermission("session/other", map[string]any{}); err != nil || reply != nil {
		t.Fatalf("unrelated methods fall through: %v %v", reply, err)
	}
}

// Only the host's own session_not_found answer (or a load method it does not
// implement) classifies the carried identity as destroyed.
func TestDevinClassifySessionUnavailable(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		errBody any
		want    string
	}{
		{"load not found", "session/load", map[string]any{"code": -32016, "message": "Session not found: d-old"}, "DEVIN_SESSION_UNAVAILABLE: Session not found: d-old"},
		{"load by kind", "session/load", map[string]any{"code": -32016, "message": "gone", "data": map[string]any{"cognition.ai/errorKind": "session_not_found"}}, "DEVIN_SESSION_UNAVAILABLE: gone"},
		{"load unimplemented", "session/load", map[string]any{"code": -32601, "message": "Method not found"}, "DEVIN_SESSION_UNAVAILABLE: Method not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := devinClassifySessionUnavailable(tc.method, tc.errBody, map[string]any{"sessionId": "d-old"})
			unavailable, ok := err.(*sessionUnavailableError)
			if !ok {
				t.Fatalf("expected sessionUnavailableError, got %T: %v", err, err)
			}
			if unavailable.message != tc.want || textOf(unavailable.identity) != "d-old" || unavailable.method != tc.method {
				t.Fatalf("unavailable = %+v", unavailable)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		method string
		err    any
	}{
		{"prompt errors are not sessions", "session/prompt", map[string]any{"code": -32016, "message": "Session not found"}},
		{"unrelated messages stay errors", "session/load", map[string]any{"code": -32016, "message": "quota exceeded"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := devinClassifySessionUnavailable(tc.method, tc.err, map[string]any{"sessionId": "d-old"}); err != nil {
				t.Fatalf("must stay a plain request error: %v", err)
			}
		})
	}
}

// The frozen binding carries the resolved executable and the account's home;
// no invented keys.
func TestDevinConfiguredBinding(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "devin.exe")
	if err := os.WriteFile(executable, []byte("agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := settings.Values{Native: map[string]string{"devinExecutable": executable}}
	binding, err := devinConfiguredBinding(config)
	if err != nil {
		t.Fatal(err)
	}
	if textOf(getOf(getOf(binding, "launch"), "executable")) != executable {
		t.Fatalf("launch = %v", getOf(binding, "launch"))
	}
	environment := objectOrEmpty(getOf(binding, "environment"))
	if environment["HOME"] == "" {
		t.Fatalf("environment = %v", environment)
	}
	if _, err := devinConfiguredBinding(settings.Values{Native: map[string]string{}}); blockedText(t, err) != "DEVIN_CONFIG: frozen native paths required" {
		t.Fatalf("frozen-paths refusal = %v", err)
	}
}

func TestAdapterLessHostStaysUnsupported(t *testing.T) {
	_, err := nativeImplementationFor("default")
	if err == nil || !strings.HasPrefix(err.Error(), "UNSUPPORTED_NATIVE_HOST: default; execution adapters: ") {
		t.Fatalf("adapter-less host must not resolve as an execution platform: %v", err)
	}
}
