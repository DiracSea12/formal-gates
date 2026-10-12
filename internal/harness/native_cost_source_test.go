package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/lifecycle"
)

func meteredNativeJob(t *testing.T, root, provider, turn, events, transcript string) (facade.WorkerMaterial, string) {
	t.Helper()
	stateDir := filepath.Join(root, facade.EngineNamespace, "run")
	input := protocol.WorkerInput{Handler: "engine.gate.v2", RunID: "run", Provider: provider, Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}
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
