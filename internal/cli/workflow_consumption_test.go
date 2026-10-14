package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
)

func TestWorkflowWorkerSourceUsesOriginalProvider(t *testing.T) {
	for _, provider := range []string{"zcode", "deepseek", "claude-code", "unregistered-host"} {
		t.Run(provider, func(t *testing.T) {
			directory := t.TempDir()
			original := worker.Material{Input: protocol.WorkerInput{Provider: provider}}
			path := filepath.Join(directory, "material.json")
			body, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			source := map[string]any{"provider": "codex", "identity": "different-provider-session"}
			err = acceptWorkflowWorker(nil, facade.WorkerMaterial{Path: path}, protocol.HostActionIntent{}, directory, source)
			want := "NATIVE_BINDING: missing or different provider/identity"
			if provider == "unregistered-host" {
				want = "UNSUPPORTED_NATIVE_HOST: " + provider
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("original material provider was not used by the native verifier: %v", err)
			}
		})
	}
}

// A terminal projection may retain the slice registry after active checkpoints
// have retired. The foreground boundary must consume terminal authority first.
func TestWorkflowConsumerReturnsTerminalBeforeChildLookup(t *testing.T) {
	for _, status := range []string{"COMPLETE", "ABORTED"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			f, _, err := facade.Start(facade.StartOptions{Root: root, Request: facade.StartRequest{RunID: "terminal-consumer", Route: "lightweight", DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: writerIntakeReceipt(t, root)}, Admission: engineWriterAdmission(t, root)})
			if err != nil {
				t.Fatal(err)
			}
			if status == "ABORTED" {
				event, err := protocol.NewRequestEvent("abort-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(event); err != nil {
					t.Fatal(err)
				}
				token, err := f.Freshness(string(event.ID))
				if err != nil {
					t.Fatal(err)
				}
				decide, err := protocol.NewDecideEvent("abort-decision", protocol.RequestID(event.ID), token, "abort")
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(decide); err != nil {
					t.Fatal(err)
				}
			}
			directory := filepath.Join(root, facade.EngineNamespace, "terminal-consumer")
			retainRetiredChildReference(t, root, "terminal-consumer")
			result, err := consumeWorkflow(IO{}, f, root, "terminal-consumer")
			if err != nil {
				t.Fatal(err)
			}
			if result.Boundary != status || result.Progress.Running != 0 || len(result.Children) != 0 {
				t.Fatalf("terminal boundary=%+v", result)
			}
			controls, err := f.ControlRuns()
			if err != nil || len(controls) != 1 || controls[0].Status != status {
				t.Fatal("terminal control projection looked up a retired child", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "state.json")); !os.IsNotExist(err) {
				t.Fatal("terminal transition retained active state", err)
			}
			summaryPath := filepath.Join(directory, "terminal-summary.json")
			before, err := os.ReadFile(summaryPath)
			if err != nil {
				t.Fatal(err)
			}
			revision := result.Run.Revision
			result, err = consumeWorkflow(IO{}, f, root, "terminal-consumer")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(summaryPath)
			if err != nil {
				t.Fatal(err)
			}
			if result.Boundary != status || result.Run.Revision != revision || string(before) != string(after) {
				t.Fatal("terminal replay changed evidence or boundary")
			}
		})
	}
}

func TestWorkflowConsumerPreservesNonterminalChildError(t *testing.T) {
	f, root, statePath := startEngineRunForWriterTest(t)
	retainRetiredChildReference(t, root, "writer-exclusive")
	result, err := consumeWorkflow(IO{}, f, root, "writer-exclusive")
	if err == nil || result.Run.Status == "COMPLETE" || result.Run.Status == "ABORTED" {
		t.Fatal("nonterminal child error was hidden", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal("nonterminal error removed active state", err)
	}
	if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "terminal-summary.json")); !os.IsNotExist(err) {
		t.Fatal("nonterminal error published terminal", err)
	}
}

// The boundary fixture retains the parent's slice reference and child's intake
// metadata after the child's active checkpoint has retired.
func retainRetiredChildReference(t *testing.T, root, runID string) {
	t.Helper()
	directory := filepath.Join(root, facade.EngineNamespace, runID)
	requestRaw, err := os.ReadFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	childDirectory := filepath.Join(root, facade.EngineNamespace, "retired-child")
	if err := os.MkdirAll(childDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err = json.Unmarshal(requestRaw, &request); err != nil {
		t.Fatal(err)
	}
	childRequest := request
	childRequest.RunID = "retired-child"
	childRaw, err := json.Marshal(childRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDirectory, "request.json"), childRaw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := persistence.NewStore(directory, persistence.Config{PackageDigest: request.PackageDigest, DefinitionSource: request.DefinitionSource, InstalledTargetIdentity: request.InstalledTargetIdentity})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var state protocol.State
	if err = json.Unmarshal(snapshot.Content, &state); err != nil {
		t.Fatal(err)
	}
	state.SliceChildren = map[string]string{"slice": "retired-child"}
	if _, err = store.Save(persistence.Transaction{ExpectedRevision: snapshot.Revision, ExpectedFingerprint: "fixture", CollectFingerprint: func() (string, error) { return "fixture", nil }, Content: &state}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowColdWorkerConsumesStartFrozenNativePaths(t *testing.T) {
	root := t.TempDir()
	receipt := writerIntakeReceipt(t, root)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "requirements.md"}, {"commit", "-qm", "Confirmed input"}} {
		cliGit(t, root, args...)
	}
	base := strings.TrimSpace(cliGit(t, root, "rev-parse", "HEAD"))
	registry, target, generation, lease, token := testkit.Installation(t, root, "native-worker-runtime", "sha256:package", "workbuddy")
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(target, "prompts", "reviewer-base.md"), catalog.Base)
	for _, action := range catalog.Actions {
		mustWriteCLI(t, filepath.Join(target, "prompts", "actions", action.ID+".md"), action.Content)
	}
	for _, rules := range catalog.RoleRules {
		mustWriteCLI(t, filepath.Join(target, "prompts", "roles", rules.ID+".md"), rules.Content)
	}
	for _, gate := range catalog.Gates {
		mustWriteCLI(t, filepath.Join(target, "gates", gate.ID+".md"), gate.Content)
	}
	admission := &facade.Admission{RegistryPath: registry, Target: target, Root: root, Provider: "workbuddy", PackageDigest: "sha256:package", InstalledTargetIdentity: "native-worker-runtime", Generation: generation, Lease: lease, Token: token}
	if err := os.MkdirAll(filepath.Join(target, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	// The consumer reaches its real request publication and supervisor boundary.
	// The local supervisor fixture refuses execution before any native model call.
	supervisor := "#!/bin/sh\n[ \"$#\" = 3 ] && [ \"$1\" = harness ] && [ \"$2\" = supervise ] || exit 92\nprintf '%s\\n' \"$3\" >> \"$WORKFLOW_SUPERVISOR_CALLS\"\nexit 93\n"
	if err := os.WriteFile(filepath.Join(target, "bin", "formal-gates"), []byte(supervisor), 0700); err != nil {
		t.Fatal(err)
	}
	homeA, homeB, configA, configB, binA, binB := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	for _, path := range []*string{&homeA, &homeB, &configA, &configB, &binA, &binB} {
		*path, err = filepath.EvalSymlinks(*path)
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(t.TempDir(), "help-calls")
	supervisorCalls := filepath.Join(t.TempDir(), "supervisor-calls")
	peer := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --help ] || exit 91\nprintf '%s|%s|%s|%s\\n' \"$HOME\" \"$CODEBUDDY_CONFIG_DIR\" \"$WORKBUDDY_CONFIG_DIR\" \"$ACC_PRODUCT_CONFIG_PATH\" >> \"$WORKFLOW_NATIVE_CALLS\"\nprintf '%s\\n' '--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk'\n"
	for _, bin := range []string{binA, binB} {
		if err := os.WriteFile(filepath.Join(bin, "codebuddy"), []byte(peer), 0700); err != nil {
			t.Fatal(err)
		}
	}
	product := filepath.Join(configA, "cache", "acc-product-config-v3.json")
	mustWriteCLI(t, product, `{}`)
	originalPath := os.Getenv("PATH")
	t.Setenv("HOME", homeA)
	t.Setenv("PATH", binA+string(os.PathListSeparator)+originalPath)
	t.Setenv("WORKBUDDY_CONFIG_DIR", configA)
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	t.Setenv("WORKFLOW_NATIVE_CALLS", calls)
	t.Setenv("WORKFLOW_SUPERVISOR_CALLS", supervisorCalls)
	mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), `{"capacity":2,"qaCapacity":1,"model":"selected-model","reasoningEffort":"high","tokenCap":77}`)
	f, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, Admission: admission, Request: facade.StartRequest{RunID: "native-consumer", Route: "regular", VCS: "git", BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("Start performed a native probe", err)
	}
	run, err := f.Drive()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("normal Start did not issue original worker", err)
	}
	var material facade.WorkerMaterial
	for _, ref := range run.WorkerMaterials {
		material = ref
	}
	t.Setenv("HOME", homeB)
	t.Setenv("PATH", binB+string(os.PathListSeparator)+originalPath)
	t.Setenv("WORKBUDDY_CONFIG_DIR", configB)
	mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), `invalid edit ignored by cold worker`)
	f, err = facade.Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	directory := facade.WorkerExecutionDirectory(material, "")
	for range 2 {
		err := executeWorkflowWorker(context.Background(), context.Background(), f, material, protocol.HostActionIntent{})
		if err == nil || !strings.Contains(err.Error(), "native supervisor ended without an exit receipt") {
			t.Fatalf("consumer did not reach the local supervisor refusal: %v", err)
		}
		var request map[string]any
		if err := readJSONFileInto(filepath.Join(directory, "native-request.json"), &request); err != nil {
			t.Fatal(err)
		}
		binding := request["binding"].(map[string]any)
		environment := binding["environment"].(map[string]any)
		launch := binding["launch"].(map[string]any)
		if request["provider"] != "workbuddy" || request["model"] != "selected-model" || request["reasoning_effort"] != "high" || launch["executable"] != filepath.Join(binA, "codebuddy") || environment["HOME"] != homeA || environment["CODEBUDDY_CONFIG_DIR"] != configA || environment["WORKBUDDY_CONFIG_DIR"] != configA || environment["ACC_PRODUCT_CONFIG_PATH"] != product {
			t.Fatalf("consumer discovered B instead of persisted A: %v", request)
		}
	}
	body, err := os.ReadFile(calls)
	line := homeA + "|" + configA + "|" + configA + "|" + product + "\n"
	if err != nil || string(body) != line+line {
		t.Fatalf("actual help invocation environment: %q %v", body, err)
	}
	body, err = os.ReadFile(supervisorCalls)
	if err != nil || string(body) != directory+"\n"+directory+"\n" {
		t.Fatalf("actual supervisor argv lost original directory: %q %v", body, err)
	}
	shown, err := f.Show()
	if err != nil || !reflect.DeepEqual(shown.Settings, run.Settings) || !reflect.DeepEqual(shown.Cost, run.Cost) || !reflect.DeepEqual(shown.GuardBudget, run.GuardBudget) {
		t.Fatal("cold consumer changed settings, cap, cost or grants", err)
	}
	if os.Getenv("HOME") != homeB || os.Getenv("WORKBUDDY_CONFIG_DIR") != configB {
		t.Fatal("consumer changed global environment")
	}
}
