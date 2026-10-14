package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/lifecycle"
)

func meteredNativeJob(t *testing.T, root, provider, turn, events, transcript string, configured ...map[string]any) (facade.WorkerMaterial, string) {
	t.Helper()
	stateDir := filepath.Join(root, facade.EngineNamespace, "run")
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", RunID: "run", Provider: provider, Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	value := worker.Material{ReadOnly: true, Prompt: "Exact issued context " + turn, Input: input, ResultSchema: schema}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := facade.WorkerMaterial{ActionID: "action", AttemptID: "attempt", ReadOnly: true, Digest: "sha256:" + hex.EncodeToString(sum[:]), Path: filepath.Join(stateDir, "materials", hex.EncodeToString(sum[:])+".json")}
	if err := os.MkdirAll(filepath.Dir(ref.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.Path, body, 0600); err != nil {
		t.Fatal(err)
	}
	directory := facade.WorkerExecutionDirectory(ref, "")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path string, value any) {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := map[string]any{"provider": provider, "workspace": root, "read_only": true, "schema": schema, "engine": map[string]any{"root": root, "runId": "run", "workerMaterial": ref, "resumeActionId": ""}}
	if len(configured) > 0 {
		for key, value := range configured[0] {
			request[key] = value
		}
	}
	write(filepath.Join(directory, "native-request.json"), request)
	if transcript == "" {
		transcript = filepath.Join(directory, "native-events.jsonl")
	}
	if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(events+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(directory, "native-source.json"), map[string]any{"sessionId": "session", "turnId": turn})
	peerSubmitSemantics(directory, map[string]any{"findings": []any{}})
	if err := writeSubmittedAnswer(directory); err != nil {
		t.Fatal(err)
	}
	source, err := evidenceSource(directory, provider, "session", map[string]any{"native_turn": turn, "transcript": transcript})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(directory, "source.json"), source)
	return ref, directory
}

func TestManagedCostSourcesUseOriginalVerifiedNativeStreams(t *testing.T) {
	root := t.TempDir()
	first := `{"sessionId":"session","turnId":"one","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":100,"cacheReadTokens":40,"outputTokens":10}}}`
	second := `{"sessionId":"session","turnId":"two","type":"turn.failed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":80,"cacheReadTokens":20,"outputTokens":5}}}`
	meteredNativeJob(t, root, "zcode", "one", first, "")
	_, two := meteredNativeJob(t, root, "zcode", "two", second, "")
	if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "session", "zcode"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeCompletion(root, "run", "action", "zcode", "session"); err != nil {
		t.Fatal(err)
	}
	ref := protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: "zcode", Correlation: "session"}
	bridge := facade.CostSourceBridge(root, "run", NativeResultEvidence)
	for range 2 {
		resolved := bridge(ref)
		want := cost.Usage{InputCacheHitTokens: 60, InputCacheMissTokens: 120, OutputTokens: 15}
		if resolved.State != protocol.CostSourceReady || resolved.Usage != want {
			t.Fatalf("verified original native streams: %#v want %#v", resolved, want)
		}
	}
	if err := os.WriteFile(filepath.Join(two, "native-events.jsonl"), []byte(second+" changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := bridge(ref)
	if changed.State != protocol.CostSourceUnavailable || changed.Reason == "" {
		t.Fatalf("changed raw proof became a measured source: %#v", changed)
	}
}

func TestManagedCostSourceRejectsForeignBindingsAndMissingVerifier(t *testing.T) {
	for _, scenario := range []string{"missing-verifier", "foreign-attempt", "foreign-run", "foreign-provider", "foreign-identity", "material-changed", "unfinished"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			events := `{"sessionId":"session","turnId":"one","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":100,"cacheReadTokens":40,"outputTokens":10}}}`
			material, directory := meteredNativeJob(t, root, "zcode", "one", events, "")
			if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "session", "zcode"); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.ObserveNativeCompletion(root, "run", "action", "zcode", "session"); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "foreign-attempt", "foreign-run", "foreign-provider":
				body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				engine := value["engine"].(map[string]any)
				if scenario == "foreign-run" {
					engine["runId"] = "another"
				}
				if scenario == "foreign-provider" {
					value["provider"] = "claude"
				}
				if scenario == "foreign-attempt" {
					engine["workerMaterial"].(map[string]any)["attemptId"] = "another"
				}
				body, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "native-request.json"), body, 0600); err != nil {
					t.Fatal(err)
				}
			case "foreign-identity":
				body, err := os.ReadFile(filepath.Join(directory, "source.json"))
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				value["identity"] = "another"
				body, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "source.json"), body, 0600); err != nil {
					t.Fatal(err)
				}
			case "material-changed":
				if err := os.WriteFile(material.Path, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "unfinished":
				if err := os.Remove(filepath.Join(directory, "source.json")); err != nil {
					t.Fatal(err)
				}
			}
			verifier := NativeResultEvidence
			if scenario == "missing-verifier" {
				verifier = nil
			}
			result := facade.CostSourceBridge(root, "run", verifier)(protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: "zcode", Correlation: "session"})
			if result.State != protocol.CostSourceUnavailable || result.Reason == "" {
				t.Fatalf("%s source falsely qualified: %#v", scenario, result)
			}
		})
	}
}

func TestManagedCostSourcesRetainKnownUsageAcrossUnavailableInvocations(t *testing.T) {
	for _, unavailableTurn := range []string{"one", "two"} {
		for _, missing := range []string{"source", "usage"} {
			t.Run(unavailableTurn+"-"+missing, func(t *testing.T) {
				root := t.TempDir()
				known := `{"sessionId":"session","turnId":"known","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":2,"inputTokens":35830,"cacheReadTokens":15104,"outputTokens":10780}}}`
				_, unavailable := meteredNativeJob(t, root, "zcode", unavailableTurn, `{"sessionId":"session","turnId":"failed","type":"turn.failed","payload":{}}`, "")
				if missing == "source" {
					if err := os.Remove(filepath.Join(unavailable, "source.json")); err != nil {
						t.Fatal(err)
					}
				}
				knownTurn := "one"
				if unavailableTurn == knownTurn {
					knownTurn = "two"
				}
				meteredNativeJob(t, root, "zcode", knownTurn, known, "")
				meteredNativeJob(t, root, "zcode", "replayed", known, "")
				if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "session", "zcode"); err != nil {
					t.Fatal(err)
				}
				if err := lifecycle.ObserveNativeCompletion(root, "run", "action", "zcode", "session"); err != nil {
					t.Fatal(err)
				}
				bridge := facade.CostSourceBridge(root, "run", NativeResultEvidence)
				for range 2 {
					result := bridge(protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: "zcode"})
					want := cost.Usage{InputCacheHitTokens: 15104, InputCacheMissTokens: 20726, OutputTokens: 10780}
					if result.State != protocol.CostSourceUnavailable || result.Reason == "" || result.Usage != want {
						t.Fatalf("unavailable invocation erased or doubled the known original usage: %#v; want %#v", result, want)
					}
				}
			})
		}
	}
}

func workbuddyMeteredNativeJob(t *testing.T, root, invocation, id string, usage, rawUsage map[string]any) string {
	t.Helper()
	initialization := map[string]any{"type": "system", "subtype": "init", "session_id": "session", "uuid": "init", "cwd": root, "model": "selected-model"}
	printUsage := map[string]any{"input_tokens": usage["inputTokens"], "output_tokens": usage["outputTokens"]}
	if rawUsage != nil {
		printUsage["cache_read_input_tokens"] = rawUsage["cache_read_input_tokens"]
		printUsage["cache_creation_input_tokens"] = rawUsage["cache_creation_input_tokens"]
	} else {
		var cached int
		for _, detail := range listOf(usage["inputTokensDetails"]) {
			cached += intOf(getOf(detail, "cached_tokens"))
		}
		printUsage["cache_read_input_tokens"] = cached
	}
	events := []any{initialization, map[string]any{"type": "assistant", "uuid": id, "session_id": "session", "message": map[string]any{"usage": printUsage}}, map[string]any{"type": "result", "subtype": "success", "uuid": "result-" + id, "session_id": "session", "is_error": false}}
	var stream []byte
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, append(body, '\n')...)
	}
	configRoot := filepath.Join(root, "native-config")
	_, directory := meteredNativeJob(t, root, "workbuddy", invocation, string(bytes.TrimSuffix(stream, []byte{10})), "", map[string]any{"binding": map[string]any{"environment": map[string]any{"CODEBUDDY_CONFIG_DIR": configRoot}}})
	store := filepath.Join(configRoot, "projects", "original-project")
	if err := os.MkdirAll(store, 0700); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"usage": usage}
	if rawUsage != nil {
		data["rawUsage"] = rawUsage
	}
	file, err := os.OpenFile(filepath.Join(store, "session.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(map[string]any{"id": id, "type": "message", "role": "assistant", "providerData": data, "content": []any{map[string]any{"type": "text", "text": "unrelated SDK context"}}}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), map[string]any{"sessionId": "session", "workspace": root, "nativeWorkspace": root, "selection": streamSelection(initialization), "process": map[string]any{"exit_code": 0, "error": ""}}); err != nil {
		t.Fatal(err)
	}
	source, err := streamObserve(workbuddyStream, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestManagedWorkBuddyCostSourcesUseInvocationSDKUsageAndSharedRecovery(t *testing.T) {
	root := t.TempDir()
	normalized := map[string]any{"inputTokens": 100, "outputTokens": 7, "inputTokensDetails": []any{map[string]any{"cached_tokens": 40}}}
	first := workbuddyMeteredNativeJob(t, root, "one", "native-first", normalized, nil)
	second := workbuddyMeteredNativeJob(t, root, "two", "native-next", map[string]any{"inputTokens": 4, "outputTokens": 5, "inputTokensDetails": []any{}}, map[string]any{"input_tokens": 4, "cache_read_input_tokens": 1, "cache_creation_input_tokens": 3})
	workbuddyMeteredNativeJob(t, root, "replay", "native-first", normalized, nil)
	if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "session", "workbuddy"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeCompletion(root, "run", "action", "workbuddy", "session"); err != nil {
		t.Fatal(err)
	}
	bridge := facade.CostSourceBridge(root, "run", NativeResultEvidence)
	ref := protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: "workbuddy", Correlation: "session"}
	for range 2 {
		got := bridge(ref)
		want := cost.Usage{InputCacheMissTokens: 64, InputCacheHitTokens: 41, OutputTokens: 12}
		if got.State != protocol.CostSourceReady || got.Usage != want {
			t.Fatalf("invocation SDK source=%#v want=%+v", got, want)
		}
	}
	projection, err := os.ReadFile(filepath.Join(first, "native-usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(projection, []byte("native-next")) || bytes.Contains(projection, []byte("unrelated SDK context")) {
		t.Fatalf("usage projection copied other invocation/body: %s", projection)
	}
	receipt, err := os.ReadFile(filepath.Join(second, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(second, "source.json")); err != nil {
		t.Fatal(err)
	}
	recovered, err := workbuddyReconcile(second, map[string]any{"exit_code": 0, "error": ""})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := canonicalBody(recovered)
	if err != nil || !bytes.Equal(encoded, receipt) {
		t.Fatalf("recovery changed source bytes: %s != %s (%v)", encoded, receipt, err)
	}
	// The selected original evidence remains valid after the mutable session is gone.
	if err := os.RemoveAll(filepath.Join(root, "native-config")); err != nil {
		t.Fatal(err)
	}
	got := bridge(ref)
	if got.State != protocol.CostSourceReady || got.Usage != (cost.Usage{InputCacheMissTokens: 64, InputCacheHitTokens: 41, OutputTokens: 12}) {
		t.Fatalf("original usage evidence lost source qualification: %#v", got)
	}
}

func TestWorkBuddyMissingSDKUsageRetainsSourceReason(t *testing.T) {
	root := t.TempDir()
	directory := workbuddyMeteredNativeJob(t, root, "one", "native-first", map[string]any{"inputTokens": 100, "outputTokens": 7, "inputTokensDetails": []any{}}, nil)
	if err := os.Remove(filepath.Join(directory, "native-usage.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "native-config")); err != nil {
		t.Fatal(err)
	}
	source, err := streamObserve(workbuddyStream, directory)
	if err != nil {
		t.Fatal(err)
	}
	reason := getOf(getOf(source, "usage_evidence"), "unavailableReason")
	if source["transcript"] != nil || textOf(reason) == "" {
		t.Fatalf("missing SDK source fabricated usage or lost reason: %#v", source)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "session", "workbuddy"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeCompletion(root, "run", "action", "workbuddy", "session"); err != nil {
		t.Fatal(err)
	}
	resolved := facade.CostSourceBridge(root, "run", NativeResultEvidence)(protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: "workbuddy", Correlation: "session"})
	if resolved.State != protocol.CostSourceUnavailable || !strings.Contains(resolved.Reason, textOf(reason)) {
		t.Fatalf("missing SDK reason did not reach original cost owner: %#v", resolved)
	}
}
