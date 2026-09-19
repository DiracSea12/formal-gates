package harness

// DSH adapter tests that need no host process: selection parsing, the patch
// shape, observe's evidence contract, reconcile's three ways, the lifecycle
// operations (capture/validate/verify-source) and the narrow entry command's
// failure shapes. The ACP execute sequence over a scripted peer lives in
// native_dsh_unix_test.go.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonNumber(literal string) any { return json.Number(literal) }

// TestDSHDispatch pins the provider dispatch: only dsh, and the exact
// unsupported-host refusal text.
func TestDSHDispatch(t *testing.T) {
	if err := dshDispatch("dsh"); err != nil {
		t.Fatalf("dsh must dispatch: %v", err)
	}
	err := dshDispatch("zcode")
	if err == nil || err.Error() != "UNSUPPORTED_NATIVE_HOST: zcode; execution adapters: dsh" {
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
	root := t.TempDir()
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", root)
	directory := t.TempDir()

	path, err := makeDSHPatch(directory, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(directory, "profile.json") {
		t.Fatalf("patch path = %q", path)
	}
	plugin := filepath.Join(root, "runtime", "langgraph", "formal_gates_harness", "native_dsh.mjs")
	want := `[{"insert":[{"config":{"directory":` + canonicalString(t, directory) +
		`,"read_only":true},"id":"harness-native","inject":["tools","sandboxPolicy"],"name":` +
		canonicalString(t, plugin) + `}]},{"id":"acp","inject":["acpAppStartup","harnessNativeReady"]}]`
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
// binding, compaction and other-turn skipping, the answer text join, usage
// evidence from message and attempt events, turn/end completion and the
// receipt shape (合同 observe 全表).
func TestObserveNative(t *testing.T) {
	messageContent := []any{
		map[string]any{"type": "text", "text": "```json\n"},
		map[string]any{"type": "tool_use", "id": "t-1"},
		map[string]any{"type": "text", "text": `{"status": "PASS"}`},
		map[string]any{"type": "text", "text": "\n```"},
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

	directory := t.TempDir()
	writeEvidence(t, directory,
		`{"sessionId": "s-1", "turn": 1, "transcriptPath": "/t/root.jsonl"}`, events)
	receipt, err := observeNative(directory, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if body := readTestFileBytes(t, filepath.Join(directory, "answer.raw.txt")); string(body) != "```json\n{\"status\": \"PASS\"}\n```" {
		t.Fatalf("answer.raw.txt = %q", body)
	}
	if answer := readTestFileBytes(t, filepath.Join(directory, "answer.json")); string(answer) != `{"status":"PASS"}` {
		t.Fatalf("answer.json = %s", answer)
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
	if transcript := textOf(receipt["transcript"]); transcript != "/t/root.jsonl" {
		t.Fatalf("transcript = %v", transcript)
	}
	if turn := textOf(receipt["native_turn"]); turn != "1" {
		t.Fatalf("native_turn = %v", turn)
	}
	if !valuesEqual(receipt["files"], proofsOf(t, directory, nativeSourceFiles)) {
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
// turn-end reason renders as canonical JSON (the registered Go-style
// difference from the reference's Python dict repr).
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

// TestObserveNativeSavedOutputDiffers pins the pre-existing answer boundary:
// different bytes are refused, identical bytes are kept (write_answer 幂等).
func TestObserveNativeSavedOutputDiffers(t *testing.T) {
	directory := t.TempDir()
	writeEvidence(t, directory,
		`{"sessionId": "s-1", "turn": 1, "transcriptPath": ""}`,
		[]string{`{"sessionId": "s-1", "event": {"type": "turn/end", "data": {"turn": 1, "reason": {"kind": "completed"}}}}`})
	// No assistant/message: the answer is the empty string; a stale saved
	// output with other bytes must be refused.
	if err := os.WriteFile(filepath.Join(directory, "answer.raw.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := observeNative(directory, "s-1")
	if err == nil || err.Error() != "DSH_RESULT: saved output differs" {
		t.Fatalf("err = %v", err)
	}
}

// TestDSHReconcile pins reconcile's three ways: eligible legacy and
// NATIVE_RESULT-prefixed failures are recovered with the reconciliation block,
// ineligible failures are refused, and workspace/model selection changes are
// refused (合同 reconcile 全表).
func TestDSHReconcile(t *testing.T) {
	const legacy = "RuntimeBlocked: DSH_RESULT: final message is not a JSON result object"
	const resultPrefixed = "RuntimeBlocked: NATIVE_RESULT: multiple candidate objects"
	raw := "Already implemented.\n\n```json\n{\"status\": \"PASS\", \"summary\": \"ok\"}\n```"

	fixture := func(t *testing.T, originalError, workspaceOverride, configuredModel string) string {
		t.Helper()
		directory := t.TempDir()
		workspace := filepath.Join(directory, "ws")
		// The identity records what the original run used; the request keeps
		// the current workspace, so an override makes the two differ.
		identityWorkspace := workspace
		if workspaceOverride != "" {
			identityWorkspace = workspaceOverride
		}
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			t.Fatal(err)
		}
		writeAtomicJSON(filepath.Join(directory, "native-error.json"), map[string]any{"error": originalError})
		writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
			"identity": "s-1", "workspace": identityWorkspace,
			"selection": map[string]any{"model": configuredModel, "reasoning_effort": "high"}})
		writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{
			"provider": "dsh", "workspace": workspace,
			"model": "test-model", "reasoning_effort": "high"})
		if err := os.WriteFile(filepath.Join(directory, "native-source.json"),
			[]byte(`{"sessionId": "s-1", "turn": 1, "transcriptPath": "retained-native-log"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(strings.Join([]string{
			`{"sessionId": "s-1", "event": {"type": "assistant/message", "data": {"turn": 1, "step": 0, "usage": {"inputTokens": 12}, "message": {"content": [{"type": "text", "text": ` + jsonString(t, raw) + `}]}}}}`,
			`{"sessionId": "s-1", "event": {"type": "turn/end", "data": {"turn": 1, "reason": {"kind": "completed"}}}}`,
		}, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "answer.raw.txt"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		return directory
	}

	for _, original := range []string{legacy, resultPrefixed} {
		directory := fixture(t, original, "", "test-model")
		source, err := dshReconcile(directory, map[string]any{"exit_code": 1, "error": ""})
		if err != nil {
			t.Fatalf("eligible %q: %v", original, err)
		}
		if !valuesEqual(source["reconciliation"], map[string]any{
			"kind":               "completed-output-format",
			"original_exit_code": 1,
			"original_error":     original,
			"new_model_calls":    0,
		}) {
			t.Fatalf("reconciliation = %v", source["reconciliation"])
		}
		if body := readTestFileBytes(t, filepath.Join(directory, "source.json")); !bytes.Contains(body, []byte(`"reconciliation":`)) {
			t.Fatalf("source.json missing the reconciliation block: %s", body)
		}
		// The original error record is never rewritten.
		if record := readTestFileBytes(t, filepath.Join(directory, "native-error.json")); !bytes.Contains(record, []byte(jsonString(t, original))) {
			t.Fatalf("native-error.json rewritten: %s", record)
		}
	}

	directory := fixture(t, "RuntimeBlocked: DSH_TRANSPORT: native ACP process closed its output", "", "test-model")
	_, err := dshReconcile(directory, map[string]any{"exit_code": 1, "error": ""})
	if err == nil || err.Error() != "DSH_RECONCILE: not a completed formatting-only failure" {
		t.Fatalf("ineligible: %v", err)
	}

	directory = fixture(t, legacy, filepath.Join(t.TempDir(), "elsewhere"), "test-model")
	_, err = dshReconcile(directory, map[string]any{"exit_code": 1, "error": ""})
	if err == nil || err.Error() != "DSH_RECONCILE: workspace changed" {
		t.Fatalf("workspace changed: %v", err)
	}

	directory = fixture(t, legacy, "", "other-model")
	_, err = dshReconcile(directory, map[string]any{"exit_code": 1, "error": ""})
	if err == nil || err.Error() != "DSH_RECONCILE: original model selection differs" {
		t.Fatalf("selection changed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "source.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused reconcile must not land source.json")
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
	fake := filepath.Join(bindir, "dsh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
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
// EVIDENCE_MISSING (the proof set must be exactly the four files),
// EVIDENCE_CHANGED per file, and acceptance.
func TestDSHVerifySource(t *testing.T) {
	directory := t.TempDir()
	for _, name := range nativeSourceFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proofs := proofsOf(t, directory, nativeSourceFiles)

	source := map[string]any{"provider": "zcode", "identity": "s-1", "files": proofs}
	err := dshVerifySource(directory, source)
	if err == nil || err.Error() != "NATIVE_BINDING: missing or different provider/identity" {
		t.Fatalf("foreign provider: %v", err)
	}
	source = map[string]any{"provider": "dsh", "identity": "", "files": proofs}
	if err := dshVerifySource(directory, source); err == nil || err.Error() != "NATIVE_BINDING: missing or different provider/identity" {
		t.Fatalf("empty identity: %v", err)
	}

	short := map[string]any{}
	for _, name := range nativeSourceFiles[:3] {
		short[name] = proofs[name]
	}
	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": short}
	if err := dshVerifySource(directory, source); err == nil || err.Error() != "EVIDENCE_MISSING: native source proofs absent" {
		t.Fatalf("missing proof: %v", err)
	}

	changed := map[string]any{}
	for _, name := range nativeSourceFiles {
		changed[name] = proofs[name]
	}
	changed["answer.raw.txt"] = map[string]any{"sha256": strings.Repeat("0", 64), "size": 1}
	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": changed}
	if err := dshVerifySource(directory, source); err == nil || err.Error() != "EVIDENCE_CHANGED: answer.raw.txt" {
		t.Fatalf("changed proof: %v", err)
	}

	source = map[string]any{"provider": "dsh", "identity": "s-1", "files": proofs}
	if err := dshVerifySource(directory, source); err != nil {
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
			"RuntimeBlocked: UNSUPPORTED_NATIVE_HOST: ghost; execution adapters: dsh"},
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
		var errout bytes.Buffer
		code := 0
		stdout := captureNativeStdout(t, func() { code = RunNative(directory, &errout) })
		if code != 1 || stdout != "" {
			t.Fatalf("code = %d stdout = %q", code, stdout)
		}
		want := "open " + filepath.Join(resolveNativePath(directory), "native-request.json") + ": no such file or directory"
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
