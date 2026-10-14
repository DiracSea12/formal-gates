package harness

// DSH adapter tests that need no host process: selection parsing, the patch
// shape, observe's evidence contract, recovery eligibility, the lifecycle
// operations (capture/validate/verify-source) and the narrow entry command's
// failure shapes. The ACP execute sequence over a scripted peer lives in
// native_dsh_unix_test.go.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeDSHLookupTarget(t *testing.T, directory string) string {
	t.Helper()
	name := "dsh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonNumber(literal string) any { return json.Number(literal) }

// TestDSHDispatch pins the registered providers and the unsupported-host text.
func TestDSHDispatch(t *testing.T) {
	for _, provider := range []string{"dsh", "zcode", "claude", "codex", "cursor"} {
		if _, err := nativeImplementationFor(provider); err != nil {
			t.Fatalf("%s must dispatch: %v", provider, err)
		}
	}
	_, err := nativeImplementationFor("ghost")
	if err == nil || err.Error() != "UNSUPPORTED_NATIVE_HOST: ghost; execution adapters: dsh, zcode, claude, codex, workbuddy, cursor" {
		t.Fatalf("unsupported text = %v", err)
	}
}

// TestDSHSelectionParsing pins selection: configOptions map to {id:
// currentValue}, a missing currentValue becoming null (合同 Go 单测：
// selection 解析含 currentValue 缺失).
func TestDSHSelectionParsing(t *testing.T) {
	raw := decodeTestJSON(t, `{"configOptions": [
		{"id": "model", "currentValue": "m-1"},
		{"id": "reasoning_effort"},
		{"id": "model", "currentValue": null}
	]}`)
	selection, err := dshSelection(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, selection); got != `{"model":null,"reasoning_effort":null}` {
		t.Fatalf("selection = %s", got)
	}

	if _, err := dshSelection(decodeTestJSON(t, `{"configOptions": [{"currentValue": "x"}]}`)); err == nil ||
		err.Error() != `missing JSON field "id"` {
		t.Fatalf("missing id: %v", err)
	}
	// A non-object configOptions list is simply empty.
	selection, err = dshSelection(decodeTestJSON(t, `{"configOptions": null}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(selection) != 0 {
		t.Fatalf("selection = %v", selection)
	}
}

// TestMakeDSHPatch pins the patch shape (合同 Go 单测：make_patch 形状)： the
// harness-native plugin with tools plus sandboxPolicy only when read_only, the
// ACP overlay, and the probe's session-title-llm disable; nothing else - no
// agent-loop/sandbox-policy/permission/approval overrides (the tool surface is
// the unchanged mjs plugin's business). Atomic canonical bytes.
func TestMakeDSHPatch(t *testing.T) {
	root, err := nativePackageRoot()
	if err != nil {
		t.Fatal(err)
	}
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})

	path, err := makeDSHPatch(directory, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(directory, "profile.json") {
		t.Fatalf("patch path = %q", path)
	}
	plugin := filepath.Join(root, "internal", "harness", "native_dsh.mjs")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := mustCanonical(t, []any{map[string]any{"insert": []any{map[string]any{"config": map[string]any{"directory": directory, "read_only": true, "executable": executable, "tools": []any{nativeWorkspaceToolSchema(true), map[string]any{"name": nativeSemanticTool, "description": "Submit this invocation's semantic result once. The program binds the role, identities and receipt; final messages only finish or explain.", "inputSchema": map[string]any{"type": "object"}}}, "managed_tools": true}, "id": "harness-native", "inject": []any{"tools", "sandboxPolicy"}, "name": plugin}}}, map[string]any{"id": "acp", "inject": []any{"acpAppStartup", "harnessNativeReady"}}})
	if got := readTestFileBytes(t, path); string(got) != want {
		t.Fatalf("patch:\n got %s\nwant %s", got, want)
	}

	// read_only false drops sandboxPolicy; the probe adds the disable overlay.
	_, err = makeDSHPatch(directory, false, true)
	if err != nil {
		t.Fatal(err)
	}
	body := readTestFileBytes(t, path)
	if !strings.Contains(string(body), `"inject":["tools"]`) {
		t.Fatalf("read_only=false must drop sandboxPolicy: %s", body)
	}
	if !strings.HasSuffix(string(body), `,{"disabled":true,"id":"session-title-llm"}]`) {
		t.Fatalf("probe must append the session-title-llm disable: %s", body)
	}
	for _, forbidden := range []string{"agent-loop", "sandbox-policy", "permission", "approval"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("patch must not override %s: %s", forbidden, body)
		}
	}
}

func canonicalString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func readTestFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestObserveNative pins the evidence contract of observe: session/turn
// binding, compaction and other-turn skipping, semantic submission, usage
// evidence from message and attempt events, turn/end completion and the
// receipt shape (合同 observe 全表).
func TestObserveNative(t *testing.T) {
	messageContent := []any{
		map[string]any{"type": "text", "text": "Review completed. "},
		map[string]any{"type": "tool_use", "id": "t-1"},
		map[string]any{"type": "text", "text": "Semantic result submitted."},
		map[string]any{"type": "text", "text": " Finished."},
	}
	events := []string{
		peerEvent("s-1", "compaction/summary", map[string]any{"turn": 1}),
		peerEvent("s-1", "assistant/message", map[string]any{"turn": 2,
			"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "other turn"}}}}),
		peerEvent("s-1", "assistant/message", map[string]any{"turn": 1, "step": 0,
			"usage":   map[string]any{"inputTokens": 12},
			"message": map[string]any{"content": messageContent}}),
		peerEvent("s-1", "assistant/attempt", map[string]any{"turn": 1, "step": 1, "usage": map[string]any{"inputTokens": 4}}),
		peerEvent("s-1", "assistant/attempt", map[string]any{"turn": 1, "step": 2}),
		peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}),
	}

	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	value := map[string]any{"status": "PASS"}
	peerSubmitSemantics(directory, value)
	writeEvidence(t, directory,
		`{"sessionId": "s-1", "turn": 1, "transcriptPath": "/t/root.jsonl"}`, events)
	receipt, err := observeNative(directory, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if answer := readTestFileBytes(t, filepath.Join(directory, "answer.json")); string(answer) != `{"status":"PASS"}` {
		t.Fatalf("answer.json = %s", answer)
	}
	if _, err := os.Stat(filepath.Join(directory, "answer.raw.txt")); !os.IsNotExist(err) {
		t.Fatal("terminal prose became semantic evidence")
	}
	// usage evidence: the message usage (an object, empty or not) and the
	// attempt whose usage object has entries; the usage-less attempt records
	// nothing.
	usage := receipt["usage_evidence"]
	want := `[{"event":"assistant/message","step":0,"turn":1,"usage":{"inputTokens":12}},` +
		`{"event":"assistant/attempt","step":1,"turn":1,"usage":{"inputTokens":4}}]`
	if got := mustCanonical(t, usage); got != want {
		t.Fatalf("usage_evidence:\n got %s\nwant %s", got, want)
	}
	if transcript := textOf(receipt["transcript"]); transcript != filepath.Join(directory, "native-events.jsonl") {
		t.Fatalf("transcript = %v", transcript)
	}
	if turn := textOf(receipt["native_turn"]); turn != "1" {
		t.Fatalf("native_turn = %v", turn)
	}
	if !valuesEqual(receipt["files"], proofsOf(t, directory, append(append([]string{}, nativeSourceFiles...), nativeToolCallsFile, nativeToolContextFile))) {
		t.Fatalf("files = %v", receipt["files"])
	}
}

// peerEvent renders one native-events.jsonl line.
func peerEvent(sessionID, eventType string, data map[string]any) string {
	encoded, err := CanonicalJSON(map[string]any{
		"sessionId": sessionID,
		"event":     map[string]any{"type": eventType, "data": data},
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func writeEvidence(t *testing.T, directory, source string, events []string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "native-source.json"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(strings.Join(events, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestObserveNativeRefusals pins the refusal texts of the evidence scan. A
// turn-end reason renders as canonical JSON.
func TestObserveNativeRefusals(t *testing.T) {
	event := func(sessionID, eventType string, data string) string {
		return `{"sessionId": "` + sessionID + `", "event": {"type": "` + eventType + `", "data": ` + data + `}}`
	}
	end := event("s-1", "turn/end", `{"turn": 1, "reason": {"kind": "completed"}}`)
	message := event("s-1", "assistant/message", `{"turn": 1, "message": {"content": [{"type": "text", "text": "x"}]}}`)

	cases := []struct {
		name    string
		source  string
		events  []string
		observe string
		want    string
	}{
		{"session mismatch", `{"sessionId": "s-2", "turn": 1}`, nil, "s-1",
			"DSH_BINDING: native source does not match the session"},
		{"turn not an int", `{"sessionId": "s-1", "turn": "1"}`, nil, "s-1",
			"DSH_BINDING: native source does not match the session"},
		{"message after end", `{"sessionId": "s-1", "turn": 1}`, []string{end, message}, "s-1",
			"DSH_RESULT: message after turn ended"},
		{"turn not completed", `{"sessionId": "s-1", "turn": 1}`,
			[]string{event("s-1", "turn/end", `{"turn": 1, "reason": {"kind": "errored"}}`)}, "s-1",
			`DSH_RESULT: turn was not completed: {"kind":"errored"}`},
		{"duplicate end", `{"sessionId": "s-1", "turn": 1}`, []string{end, end}, "s-1",
			`DSH_RESULT: turn was not completed: {"kind":"completed"}`},
		{"no end", `{"sessionId": "s-1", "turn": 1}`, []string{message}, "s-1",
			"DSH_RESULT: committed turn/end evidence is missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			writeEvidence(t, directory, tc.source, tc.events)
			_, err := observeNative(directory, tc.observe)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestObserveNativeRejectsChangedSavedSemantics checks the saved semantic payload
// against the original successful submission.
func TestObserveNativeRejectsChangedSavedSemantics(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	peerSubmitSemantics(directory, map[string]any{"status": "PASS"})
	writeEvidence(t, directory,
		`{"sessionId": "s-1", "turn": 1, "transcriptPath": ""}`,
		[]string{peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}})})
	if err := os.WriteFile(filepath.Join(directory, "answer.json"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := observeNative(directory, "s-1")
	if err == nil || err.Error() != "NATIVE_RESULT: saved semantics differ from original tool submission" {
		t.Fatalf("err = %v", err)
	}
}

// TestDSHReconcile retains original successful process, session, workspace,
// selection and semantic tool evidence without replaying a model call.
func TestDSHReconcile(t *testing.T) {
	fixture := func(t *testing.T, workspaceOverride, configuredModel string) string {
		t.Helper()
		directory := t.TempDir()
		workspace := filepath.Join(directory, "ws")
		identityWorkspace := workspace
		if workspaceOverride != "" {
			identityWorkspace = workspaceOverride
		}
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
			"identity": "s-1", "workspace": identityWorkspace,
			"selection": map[string]any{"model": configuredModel, "reasoning_effort": "high"}}); err != nil {
			t.Fatal(err)
		}
		if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{
			"provider": "dsh", "workspace": workspace, "read_only": true,
			"model": "test-model", "reasoning_effort": "high", "schema": map[string]any{"type": "object"}}); err != nil {
			t.Fatal(err)
		}
		peerSubmitSemantics(directory, map[string]any{"status": "PASS", "summary": "ok"})
		writeEvidence(t, directory,
			`{"sessionId": "s-1", "turn": 1, "transcriptPath": "retained-native-log"}`,
			[]string{
				peerEvent("s-1", "assistant/message", map[string]any{"turn": 1, "step": 0, "usage": map[string]any{"inputTokens": 12}, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Semantics submitted."}}}}),
				peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}),
			})
		return directory
	}
	directory := fixture(t, "", "test-model")
	names := []string{"native-source.json", "native-events.jsonl", nativeToolCallsFile, nativeToolContextFile}
	originalProofs := proofsOf(t, directory, names)
	source, err := dshReconcile(directory, map[string]any{"exit_code": 0, "error": ""})
	if err != nil || source["identity"] != "s-1" || !valuesEqual(source["selection"], map[string]any{"model": "test-model", "reasoning_effort": "high"}) {
		t.Fatalf("successful original process: %v %v", source, err)
	}
	if source["transcript"] != filepath.Join(directory, "native-events.jsonl") {
		t.Fatalf("recovered metering source = %v", source["transcript"])
	}
	if !valuesEqual(originalProofs, proofsOf(t, directory, names)) || string(readTestFileBytes(t, filepath.Join(directory, "answer.json"))) != `{"status":"PASS","summary":"ok"}` {
		t.Fatal("recovery changed original native or semantic evidence")
	}
	stored, err := readJSONFile(filepath.Join(directory, "source.json"))
	if err != nil || !valuesEqual(stored, source) {
		t.Fatal("recovered receipt differs from original source", err)
	}
	for _, process := range []map[string]any{{"exit_code": 1, "error": ""}, {"exit_code": 0, "error": "interrupted"}} {
		other := fixture(t, "", "test-model")
		_, err := dshReconcile(other, process)
		if err == nil || err.Error() != "DSH_RECONCILE: original native process did not complete successfully" {
			t.Fatalf("failed original process: %v", err)
		}
		if _, err := os.Stat(filepath.Join(other, "source.json")); !os.IsNotExist(err) {
			t.Fatal("failed original process became a recovered success")
		}
	}
	for _, item := range []struct{ workspace, model, want string }{
		{filepath.Join(t.TempDir(), "elsewhere"), "test-model", "DSH_RECONCILE: workspace changed"},
		{"", "other-model", "DSH_RECONCILE: original model selection differs"},
	} {
		other := fixture(t, item.workspace, item.model)
		_, err := dshReconcile(other, map[string]any{"exit_code": 0, "error": ""})
		if err == nil || err.Error() != item.want {
			t.Fatalf("changed original binding: %v", err)
		}
		if _, err := os.Stat(filepath.Join(other, "source.json")); !os.IsNotExist(err) {
			t.Fatal("changed original binding became a recovered success")
		}
	}
}

// jsonString is shared with artifact_test.go (same package): json.Marshal of
// one string, for embedding paths into hand-built JSON fixtures.

// TestDSHCaptureBinding pins capture's PATH semantics (合同 Go 单测): the
// resolved executable with its file proof, and the refusal when dsh is absent.
func TestDSHCaptureBinding(t *testing.T) {
	root := t.TempDir()
	bindir := filepath.Join(root, "bin")
	if err := os.Mkdir(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := writeDSHLookupTarget(t, bindir)
	t.Setenv("PATH", bindir)
	binding, err := dshCaptureBinding()
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolveNativePath(fake)
	if got := textOf(objectOrEmpty(binding["launch"])["executable"]); got != resolved {
		t.Fatalf("executable = %v, want %v", got, resolved)
	}
	files := objectOrEmpty(binding["files"])
	proof, present := files[resolved]
	if !present {
		t.Fatalf("files must proof the resolved executable: %v", files)
	}
	bound, err := fileProofOf(fake)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(proof, bound) {
		t.Fatalf("proof = %v", proof)
	}

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	_, err = dshCaptureBinding()
	if err == nil || err.Error() != "DSH_UNAVAILABLE: dsh is not on PATH" {
		t.Fatalf("absent dsh: %v", err)
	}
}

// TestDSHValidateBinding pins validate_binding: HOST_BINDING for a foreign
// provider (the HarnessError family), HOST_CHANGED per differing file proof,
// and acceptance for a matching binding.
func TestDSHValidateBinding(t *testing.T) {
	root := t.TempDir()
	bound := filepath.Join(root, "dsh")
	if err := os.WriteFile(bound, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proof, err := fileProofOf(bound)
	if err != nil {
		t.Fatal(err)
	}

	err = dshValidateBinding(decodeTestJSON(t, `{"provider": "zcode", "files": {}}`))
	if _, ok := err.(*bindingError); !ok || err.Error() != "HOST_BINDING: provider differs" {
		t.Fatalf("provider differs: %v (%T)", err, err)
	}

	changed := map[string]any{"provider": "dsh", "files": map[string]any{
		bound: map[string]any{"sha256": strings.Repeat("0", 64), "size": 1}}}
	err = dshValidateBinding(changed)
	if err == nil || err.Error() != "HOST_CHANGED: bound native executable/script changed: "+bound {
		t.Fatalf("changed proof: %v", err)
	}

	valid := map[string]any{"provider": "dsh", "files": map[string]any{bound: proof}}
	if err := dshValidateBinding(valid); err != nil {
		t.Fatalf("valid binding: %v", err)
	}
	// An extra key in the proof is a difference (value comparison).
	extraProof := map[string]any{}
	for key, value := range proof {
		extraProof[key] = value
	}
	extraProof["mtime"] = jsonNumber("1")
	extra := map[string]any{"provider": "dsh", "files": map[string]any{bound: extraProof}}
	if err := dshValidateBinding(extra); err == nil || err.Error() != "HOST_CHANGED: bound native executable/script changed: "+bound {
		t.Fatalf("extra key: %v", err)
	}
}

// TestDSHVerifySource pins verify_source's refusal ladder: NATIVE_BINDING,
// EVIDENCE_MISSING for the complete original native and tool proof set,
// EVIDENCE_CHANGED per file, and acceptance.
func TestDSHVerifySource(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	peerSubmitSemantics(directory, map[string]any{"status": "PASS"})
	writeEvidence(t, directory, `{"sessionId":"s-1","turn":1}`, []string{peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}})})
	receipt, err := observeNative(directory, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	proofs := objectOrEmpty(receipt["files"])

	source := map[string]any{"provider": "zcode", "identity": "s-1", "files": proofs}
	err = dshVerifySource(directory, source)
	if err == nil || err.Error() != "NATIVE_BINDING: missing or different provider/identity" {
		t.Fatalf("foreign provider: %v", err)
	}
	source = map[string]any{"provider": "dsh", "identity": "", "files": proofs}
	if err = dshVerifySource(directory, source); err == nil || err.Error() != "NATIVE_BINDING: missing or different provider/identity" {
		t.Fatalf("empty identity: %v", err)
	}

	short := map[string]any{}
	for _, name := range nativeSourceFiles[:3] {
		short[name] = proofs[name]
	}
	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": short}
	if err = dshVerifySource(directory, source); err == nil || err.Error() != "EVIDENCE_MISSING: native source proofs absent" {
		t.Fatalf("missing proof: %v", err)
	}

	changed := map[string]any{}
	for name, proof := range proofs {
		changed[name] = proof
	}
	changed[nativeToolCallsFile] = map[string]any{"sha256": strings.Repeat("0", 64), "size": 1}
	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": changed}
	if err = dshVerifySource(directory, source); err == nil || err.Error() != "EVIDENCE_CHANGED: "+nativeToolCallsFile {
		t.Fatalf("changed proof: %v", err)
	}

	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": proofs}
	if err = dshVerifySource(directory, source); err != nil {
		t.Fatalf("valid source: %v", err)
	}
}

// TestRunNativeEntryFailureShapes pins the entry's Go-style error texts and
// exit code 1 for every pre-spawn failure of native-request.json (合同接口：
// 缺失/畸形/provider 未知).
func TestRunNativeEntryFailureShapes(t *testing.T) {
	cases := []struct {
		name    string
		request string
		want    string
	}{
		{"malformed", "not json",
			"invalid JSON in "},
		{"array request", "[1]",
			"native-request.json must contain a JSON object"},
		{"missing provider", "{}",
			`missing JSON field "provider"`},
		{"numeric provider", `{"provider": 5}`,
			"provider must be a string"},
		{"unsupported provider", `{"provider": "ghost"}`,
			"RuntimeBlocked: UNSUPPORTED_NATIVE_HOST: ghost; execution adapters: dsh, zcode, claude, codex, workbuddy, cursor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "native-request.json"), []byte(tc.request), 0o644); err != nil {
				t.Fatal(err)
			}
			var errout bytes.Buffer
			code := 0
			stdout := captureNativeStdout(t, func() { code = RunNative(directory, &errout) })
			if code != 1 {
				t.Fatalf("code = %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			got := nativeRecordError(t, directory)
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("error = %q, want prefix %q", got, tc.want)
			}
			if errout.String() != got+"\n" {
				t.Fatalf("stderr = %q, want %q", errout.String(), got+"\n")
			}
		})
	}

	t.Run("missing request file", func(t *testing.T) {
		directory := t.TempDir()
		requestPath := filepath.Join(resolveNativePath(directory), "native-request.json")
		_, wantErr := os.ReadFile(requestPath)
		if wantErr == nil {
			t.Fatal("missing request unexpectedly exists")
		}
		var errout bytes.Buffer
		code := 0
		stdout := captureNativeStdout(t, func() { code = RunNative(directory, &errout) })
		if code != 1 || stdout != "" {
			t.Fatalf("code = %d stdout = %q", code, stdout)
		}
		want := wantErr.Error()
		if got := nativeRecordError(t, directory); got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	})

	t.Run("nothing spawned for an unknown provider", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "native-request.json"), []byte(`{"provider": "ghost"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var errout bytes.Buffer
		captureNativeStdout(t, func() { RunNative(directory, &errout) })
		if _, err := os.Stat(filepath.Join(directory, "native-process.json")); !os.IsNotExist(err) {
			t.Fatal("no process record may exist")
		}
	})
}

func nativeRecordError(t *testing.T, directory string) string {
	t.Helper()
	record, err := readJSONFile(filepath.Join(directory, "native-error.json"))
	if err != nil {
		t.Fatal(err)
	}
	return textOf(objectOrEmpty(record)["error"])
}

func captureNativeStdout(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	done := make(chan string, 1)
	go func() {
		body, _ := io.ReadAll(read)
		done <- string(body)
	}()
	fn()
	write.Close()
	os.Stdout = saved
	return <-done
}

// TestRunNativeSessionUnavailableRecord pins the five-key SessionUnavailable
// error record: error, error_kind, identity, method and the host's original
// native_code (null included), in canonical bytes.
func TestRunNativeSessionUnavailableRecord(t *testing.T) {
	unavailable := &sessionUnavailableError{
		blockedError: blockedError{message: "DSH_SESSION_UNAVAILABLE: Session not found: s-1"},
		identity:     "s-1",
		method:       "session/resume",
		code:         nil,
	}
	directory := t.TempDir()
	var errout bytes.Buffer
	writeNativeError(directory, unavailable, &errout)
	record, err := readJSONFile(filepath.Join(directory, "native-error.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"error":"SessionUnavailable: DSH_SESSION_UNAVAILABLE: Session not found: s-1",` +
		`"error_kind":"SESSION_UNAVAILABLE","identity":"s-1","method":"session/resume","native_code":null}`
	if got := mustCanonical(t, record); got != want {
		t.Fatalf("record = %s", got)
	}
	if errout.String() != "SessionUnavailable: DSH_SESSION_UNAVAILABLE: Session not found: s-1\n" {
		t.Fatalf("stderr = %q", errout.String())
	}
}

// TestDSHObservesDelivered pins the attribution rule's truth table: a
// session/update confirms a delivered hold only when its tool-card update
// (sessionUpdate tool_call or tool_call_update, toolCallId at the update's
// top level, content an array - the host's real shapes) names the held
// request's toolCallId; metadata updates (available commands, message chunks)
// of the same session never do; a plain response is the in-flight prompt
// answering and always does.
func TestDSHObservesDelivered(t *testing.T) {
	hold := &nativePendingHold{params: map[string]any{
		"sessionId": "s-1", "toolCall": map[string]any{"toolCallId": "call-9"}}}
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{"tool card names the held call", `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "tool_call", "toolCallId": "call-9", "title": "Write marker", "kind": "other", "status": "in_progress", "rawInput": {"path": "marker.txt"}}}}`, true},
		{"tool card names another call", `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "tool_call_update", "toolCallId": "call-10", "status": "completed", "content": []}}}`, false},
		{"available commands update", `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "available_commands_update", "availableCommands": []}}}`, false},
		{"message chunk of the same session", `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "agent_message_chunk", "content": "text chunk"}}}`, false},
		{"tool card of another session", `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-2", "update": {"sessionUpdate": "tool_call", "toolCallId": "call-9"}}}`, false},
		{"other notification", `{"jsonrpc": "2.0", "method": "session/status", "params": {"sessionId": "s-1"}}`, false},
		{"plain response answers the prompt", `{"jsonrpc": "2.0", "id": 3, "result": {"stopReason": "end_turn"}}`, true},
	}
	for _, test := range cases {
		if got := dshObservesDelivered(hold, decodeTestJSON(t, test.message).(map[string]any)); got != test.want {
			t.Errorf("%s: got %v", test.name, got)
		}
	}
}
