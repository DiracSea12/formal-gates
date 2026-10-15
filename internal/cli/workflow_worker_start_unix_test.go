//go:build !windows

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

// The controlled supervisor records actual execution ownership and a native
// init acknowledgement without issuing any model turn.
func TestWorkflowSupervisorProcess(t *testing.T) {
	if os.Getenv("WORKFLOW_SUPERVISOR_PROCESS") != "1" {
		return
	}
	if id := os.Getenv("WORKFLOW_SELECTION_PROVIDER_ID"); id != "" {
		if err := host.Register(workflowSelectionPlatform{descriptor: host.Descriptor{ID: id, InstallName: os.Getenv("WORKFLOW_SELECTION_INSTALL_NAME"), Installable: true}}); err != nil {
			t.Fatal(err)
		}
	}
	args := os.Args[len(os.Args)-3:]
	if args[0] != "harness" || args[1] != "supervise" {
		t.Fatalf("unexpected supervisor method: %v", args)
	}
	log, err := os.OpenFile(os.Getenv("WORKFLOW_SUPERVISOR_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.WriteString(strings.Join(args, "|") + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	directory := args[2]
	var request, native map[string]any
	if err := readJSONFileInto(filepath.Join(directory, "request.json"), &request); err != nil {
		t.Fatal(err)
	}
	if err := readJSONFileInto(filepath.Join(directory, "native-request.json"), &native); err != nil {
		t.Fatal(err)
	}
	release, err := facade.AdmitExecution(directory, request)
	if err != nil || release == nil {
		t.Fatal("original invocation was not admitted", err)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(stop)
	owner, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	identity := "controlled-session"
	if previous, ok := native["identity"].(string); ok && previous != "" {
		identity = previous
	}
	if err := writeInvocationFile(filepath.Join(directory, "identity.json"), map[string]any{"identity": identity, "workspace": native["workspace"], "acknowledgement": map[string]any{"type": "system", "subtype": "init", "session_id": identity}}); err != nil {
		t.Fatal(err)
	}
	digest, err := harness.CanonicalDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeInvocationFile(filepath.Join(directory, "process.json"), map[string]any{"pid": os.Getpid(), "pgid": syscall.Getpgrp(), "started_at": float64(time.Now().UnixNano()) / 1e9, "request_digest": digest}); err != nil {
		t.Fatal(err)
	}
	release()
	release = nil
	<-stop
	if err := writeInvocationFile(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0, "request_digest": digest, "error": ""}); err != nil {
		t.Fatal(err)
	}
}

func workflowStartFixture(t *testing.T, selections ...*protocol.FlowSelection) (*facade.Facade, string, facade.WorkerMaterial, string) {
	t.Helper()
	return workflowStartPlatformFixture(t, "workbuddy", nil, selections...)
}

func workflowStartPlatformFixture(t *testing.T, provider string, native map[string]string, selections ...*protocol.FlowSelection) (*facade.Facade, string, facade.WorkerMaterial, string) {
	t.Helper()
	root, home, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", filepath.Join(home, ".workbuddy"))
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WORKFLOW_SUPERVISOR_PROCESS", "1")
	t.Setenv("WORKFLOW_SUPERVISOR_CALLS", filepath.Join(root, "supervisor-calls"))
	mustWriteCLI(t, filepath.Join(home, ".workbuddy", "cache", "acc-product-config-v3.json"), `{}`)
	if err := os.WriteFile(filepath.Join(bin, "codebuddy"), []byte("#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --help ] || exit 91\nprintf '%s\\n' '--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	receipt := writerIntakeReceipt(t, root)
	registry, target, generation, lease, token := testkit.Installation(t, root, "consumer-runtime", "sha256:package", provider)
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
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launcher := "#!/bin/sh\nexec " + "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'" + " -test.run '^TestWorkflowSupervisorProcess$' -- \"$@\"\n"
	if len(selections) > 0 {
		launcher = "#!/bin/sh\n[ \"$#\" = 3 ] && [ \"$1\" = harness ] && [ \"$2\" = supervise ] || exit 92\nprintf '%s\\n' \"$3\" >> \"$WORKFLOW_SUPERVISOR_CALLS\"\nexit 93\n"
	}
	if err := os.MkdirAll(filepath.Join(target, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "bin", "formal-gates"), []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	admission := &facade.Admission{RegistryPath: registry, Target: target, Root: root, Provider: provider, PackageDigest: "sha256:package", InstalledTargetIdentity: "consumer-runtime", Generation: generation, Lease: lease, Token: token}
	if native != nil {
		body, err := json.Marshal(native)
		if err != nil {
			t.Fatal(err)
		}
		mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), string(body))
	}
	mustWriteCLI(t, filepath.Join(root, "README.md"), "independent command components\n")
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.formal-gates-resources/\n")
	runner := vcs.ExecRunner{}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Consumer Test", "-c", "user.email=consumer@example.invalid", "commit", "-qm", "command inputs"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	base, err := runner.Run(root, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var selection *protocol.FlowSelection
	if len(selections) > 0 {
		selection = selections[0]
	}
	f, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, VCSRunner: runner, Admission: admission, Request: facade.StartRequest{RunID: "consumer", Route: "regular", BaseSnapshot: strings.TrimSpace(base), IntakeConfirmationReceipt: receipt, FlowSelection: selection}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("original worker was not issued", err, run)
	}
	var ref facade.WorkerMaterial
	for _, material := range run.WorkerMaterials {
		ref = material
	}
	directory := facade.WorkerExecutionDirectory(ref, "")
	cleanupWorkflowSupervisor(t, directory)
	return f, root, ref, directory
}

func TestWorkflowPendingAskStartsOriginalWorkerAndReusesOwnership(t *testing.T) {
	f, root, ref, directory := workflowStartFixture(t)
	candidate, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Content protocol.State `json:"content"`
	}
	state, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(state, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Content.RequirementBarrierActive() {
		t.Fatal("pending Ask introduced a requirement barrier")
	}
	if _, authorized := stored.Content.AbortDecision(); authorized {
		t.Fatal("pending Ask authorized abort")
	}
	var originalProcess []byte
	for iteration := range 2 {
		result, err := consumeWorkflow(IO{}, f, root, "consumer")
		if err != nil || result.Boundary != "ASK" || len(result.Run.PendingAsks) != 1 || len(result.Blocked) != 0 {
			t.Fatal("pending Ask lost the independent worker or its boundary", result, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "owner")); err != nil {
			t.Fatal(err)
		}
		var request struct {
			Engine struct {
				Root           string                `json:"root"`
				RunID          string                `json:"runId"`
				WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
			} `json:"engine"`
		}
		body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request.Engine.Root != root || request.Engine.RunID != "consumer" || request.Engine.WorkerMaterial != ref {
			t.Fatal("launch changed the signed material", request)
		}
		identity, err := harness.NativeStartEvidence("workbuddy", directory)
		if err != nil || identity != "controlled-session" {
			t.Fatal("actual native init differs", identity, err)
		}
		if !harness.NativeJobAlive(directory) {
			t.Fatal("owned supervisor is not alive")
		}
		process, err := os.ReadFile(filepath.Join(directory, "process.json"))
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 0 {
			originalProcess = process
		} else if !reflect.DeepEqual(process, originalProcess) {
			t.Fatal("repeated consumption replaced original process", string(process))
		}
	}
	if calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls")); err != nil || string(calls) != "harness|supervise|"+directory+"\n" {
		t.Fatal("consumer repeated or changed the actual native start", string(calls), err)
	}
}

type workflowSelectionPlatform struct{ descriptor host.Descriptor }

func (p workflowSelectionPlatform) HostDescriptor() *host.Descriptor { return &p.descriptor }
func (p workflowSelectionPlatform) SelectionOptions(string, string, map[string]string, string) (host.SelectionCatalogue, error) {
	return host.SelectionCatalogue{Models: []host.ModelOption{{Value: "target-model", Efforts: []string{"high"}}}, Source: "controlled native catalogue"}, nil
}

func TestWorkflowSelectedPlatformFreezesNewBindingAndRetainsClaimedBinding(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "cursor", "dsh", "zcode", "workbuddy"} {
		for _, invocation := range []string{"cross-platform", "unavailable", "controller"} {
			t.Run(provider+"/"+invocation, func(t *testing.T) {
				missing := invocation == "unavailable"
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				executable, home := filepath.Join(root, "native-cli"), filepath.Join(root, "native-home")
				mustWriteCLI(t, executable, "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --help ] || exit 91\nprintf '%s\\n' '--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk'\n")
				if err := os.Chmod(executable, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(home, 0700); err != nil {
					t.Fatal(err)
				}
				native := map[string]string{provider + "Executable": executable}
				if provider != "dsh" {
					native[provider+"Home"] = home
				}
				if provider == "zcode" {
					native["zcodeScript"] = filepath.Join(root, "server.cjs")
					mustWriteCLI(t, native["zcodeScript"], "controlled native server")
				}
				if provider == "workbuddy" {
					native["workbuddyProductConfig"] = filepath.Join(home, ".workbuddy", "cache", "acc-product-config-v3.json")
					mustWriteCLI(t, native["workbuddyProductConfig"], `{}`)
				}
				controller := "workbuddy"
				if provider == "workbuddy" {
					controller = "codex"
					native["codexExecutable"] = executable
				}
				if invocation == "controller" {
					controller = provider
				}
				f, _, ready, _ := workflowStartPlatformFixture(t, controller, native)
				before, err := f.Show()
				if err != nil {
					t.Fatal(err)
				}
				if invocation != "controller" && before.Settings.Values.Native[provider+"ConfigRoot"] != "" {
					t.Fatal("target platform was already frozen by the controller")
				}
				if provider == "dsh" {
					home, err = filepath.EvalSymlinks(os.Getenv("HOME"))
					if err != nil {
						t.Fatal(err)
					}
				}
				var controllerBindingJSON []byte
				if invocation == "controller" {
					binding, err := harness.NativeBinding(provider, before.Settings.Values)
					if err != nil {
						t.Fatal(err)
					}
					controllerBindingJSON, err = json.Marshal(binding)
					if err != nil {
						t.Fatal(err)
					}
					t.Setenv("HOME", t.TempDir())
					t.Setenv("FORMAL_GATES_ZCODE_CREDENTIALS", filepath.Join(t.TempDir(), "changed-credentials.json"))
				}
				platform := workflowSelectionPlatform{descriptor: host.Descriptor{ID: strings.ToLower(t.Name()), InstallName: provider, Installable: true}}
				if err := host.Register(platform); err != nil {
					t.Fatal(err)
				}
				t.Setenv("WORKFLOW_SELECTION_PROVIDER_ID", platform.descriptor.ID)
				t.Setenv("WORKFLOW_SELECTION_INSTALL_NAME", provider)
				candidate, err := f.PrepareControl(facade.ControlInput{Execution: &facade.ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "target-model", Effort: "high"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := f.SubmitControl(candidate); err != nil {
					t.Fatal(err)
				}
				issued, _, directory, selected, err := f.WorkerExecutionRequest(ready, "")
				if err != nil || issued["provider"] != provider || selected.ActionID != ready.ActionID {
					t.Fatal("selection lost its native provider or original action", issued, selected, err)
				}
				cleanupWorkflowSupervisor(t, directory)
				if missing {
					if err := os.Remove(executable); err != nil {
						t.Fatal(err)
					}
				}
				request, observed, original, _, err := startWorkflowWorker(context.Background(), f, ready, protocol.HostActionIntent{})
				if missing {
					if err == nil || !strings.Contains(err.Error(), "UNAVAILABLE") || strings.Contains(err.Error(), "frozen native paths required") {
						t.Fatalf("missing selected executable did not fail at its native resolver: %v", err)
					}
					for _, name := range []string{"owner", "process.json"} {
						if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
							t.Fatal("unavailable selection started an execution", name, err)
						}
					}
					return
				}
				if err != nil || observed != directory || original != selected || request["model"] != "target-model" || request["reasoning_effort"] != "high" {
					t.Fatal("new native invocation lost its issued selection", request, observed, original, err)
				}
				binding := request["binding"].(map[string]any)
				launch := binding["launch"].(map[string]any)
				environment := binding["environment"].(map[string]any)
				if launch["executable"] != executable || environment["HOME"] != home {
					t.Fatal("new invocation did not resolve the selected platform's paths", binding)
				}
				var saved map[string]any
				bindingJSON, err := json.Marshal(binding)
				if err != nil {
					t.Fatal(err)
				}
				if invocation == "controller" && string(bindingJSON) != string(controllerBindingJSON) {
					t.Fatal("controller execution followed changed native environment", binding)
				}
				if err := readJSONFileInto(filepath.Join(directory, "native-request.json"), &saved); err != nil {
					t.Fatal(err)
				}
				if body, err := json.Marshal(saved["binding"]); err != nil || string(body) != string(bindingJSON) {
					t.Fatal("original request did not persist its resolved binding", saved["binding"], err)
				}
				process, err := os.ReadFile(filepath.Join(directory, "process.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(executable); err != nil {
					t.Fatal(err)
				}
				reused, _, _, _, err := startWorkflowWorker(context.Background(), f, ready, protocol.HostActionIntent{})
				if err != nil {
					t.Fatal(err)
				}
				if body, err := json.Marshal(reused["binding"]); err != nil || string(body) != string(bindingJSON) {
					t.Fatal("claimed invocation recaptured native configuration", reused["binding"], err)
				}
				currentProcess, err := os.ReadFile(filepath.Join(directory, "process.json"))
				if err != nil || !reflect.DeepEqual(process, currentProcess) {
					t.Fatal("claimed invocation restarted its original process", err)
				}
				after, err := f.Show()
				if err != nil || !reflect.DeepEqual(before.Settings, after.Settings) {
					t.Fatal("temporary platform binding changed frozen run defaults", err)
				}
			})
		}
	}
}

func TestWorkflowWorkerStartHonorsActualCancellationAndRequirementBarrier(t *testing.T) {
	for _, cause := range []string{"cancel", "requirement-barrier", "authorized-abort"} {
		t.Run(cause, func(t *testing.T) {
			f, root, ref, directory := workflowStartFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cause == "cancel" {
				cancel()
				consumed, err := consumeWorkflow(IO{Context: ctx}, f, root, "consumer")
				if !errors.Is(err, context.Canceled) || consumed.Boundary != "INTERRUPTED" {
					t.Fatal("foreground cancellation was not retained", consumed, err)
				}
			} else {
				request := protocol.ControlRequestRequirementChange
				if cause == "authorized-abort" {
					request = protocol.ControlAbort
				}
				candidate, err := f.PrepareControl(facade.ControlInput{Request: request})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := f.SubmitControl(candidate); err != nil {
					t.Fatal(err)
				}
				if cause == "authorized-abort" {
					candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "abort"})
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := f.SubmitControl(candidate); err != nil {
						t.Fatal(err)
					}
					var stored struct {
						Content protocol.State `json:"content"`
					}
					state, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json"))
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(state, &stored); err != nil {
						t.Fatal(err)
					}
					if _, authorized := stored.Content.AbortDecision(); !authorized {
						t.Fatal("original abort decision was not accepted")
					}
				} else {
					var stored struct {
						Content protocol.State `json:"content"`
					}
					state, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json"))
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(state, &stored); err != nil {
						t.Fatal(err)
					}
					if !stored.Content.RequirementBarrierActive() {
						t.Fatal("requirement request did not establish its barrier")
					}
				}
			}
			err := executeWorkflowWorker(ctx, ctx, f, ref, protocol.HostActionIntent{})
			if err == nil || cause == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("real start boundary was ignored", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "owner")); !os.IsNotExist(err) {
				t.Fatal("blocked start claimed a job", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "native-request.json")); !os.IsNotExist(err) {
				t.Fatal("blocked start published an invocation", err)
			}
			shown, err := f.Show()
			if err != nil || !reflect.DeepEqual(shown.WorkerMaterials[ref.ActionID], ref) && cause == "cancel" {
				t.Fatal("cancellation changed original issuance", root, shown, err)
			}
		})
	}
}

// The native consumer must reach the current preparation's original supervisor
// after requirement replacement, rather than stop at its superseded topology Ask.
func TestWorkflowConsumerStartsRequirementReplacementPreparation(t *testing.T) {
	f, root, original, originalDirectory := workflowStartFixture(t, &protocol.FlowSelection{Disabled: []string{"product-review", "start-readiness", "qa", "gate", "repair"}})
	var material worker.Material
	if err := readJSONFileInto(original.Path, &material); err != nil {
		t.Fatal(err)
	}
	input := material.Input
	if input.Handler != "engine.development.plan.v1" || input.Validation == nil || input.Provider != "workbuddy" || input.Pending.ActionID != original.ActionID {
		t.Fatal("preparation fixture has a different original protocol input", input)
	}
	plan := &protocol.GranularityReview{Reason: "Public engine entries share one run-binding contract", Batches: []protocol.DevelopmentBatch{{
		ID: "entries", RequirementPoints: requirement.PointIDs(input.RequirementPoints), Subtasks: []protocol.DevelopmentSubtask{{ID: "run-binding", Description: "Implement the confirmed engine entry binding"}},
		Deliverable: "Engine entries retain the authoritative run and reject legacy writes", PublicEntry: "workflow", Artifacts: []string{"app.go"}, Interfaces: "Workflow commands", StateBoundary: "Authoritative run state", Risk: "Incorrect run selection",
		DoD: []protocol.DevelopmentCriterion{{ID: "binding", Description: "Public engine entries preserve the confirmed run binding"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Goal: "Build the project"}, {ID: "tests", Purpose: protocol.CheckFullTests, Goal: "Run the public-entry suite"}},
		Rollback: "Revert the entry change", Recovery: "Continue from the confirmed run inputs", Handoff: "Document entry behavior and checks", ContextCost: "One entry contract fits one worker",
	}}}
	identity := "controlled-preparation"
	spawn, err := protocol.NewSpawnReceiptEvent("preparation-spawn", original.ActionID, input.Provider, identity, protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, _, err := f.Submit(spawn); err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("original controlled preparation receipt was rejected", accepted, err)
	}
	usage := `{"type":"assistant","uuid":"controlled-preparation","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}` + "\n"
	transcript := filepath.Join(t.TempDir(), "usage.jsonl")
	mustWriteCLI(t, transcript, usage)
	if err := os.MkdirAll(originalDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"native-request.json": map[string]any{"provider": "workbuddy", "engine": map[string]any{"root": root, "runId": "consumer", "workerMaterial": original, "resumeActionId": ""}},
		"source.json":         map[string]any{"provider": "workbuddy", "identity": identity, "transcript": transcript, "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(usage)))},
	} {
		if err := writeInvocationFile(filepath.Join(originalDirectory, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.ObserveNativeCompletion(root, "consumer", original.ActionID, input.Provider, identity); err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{ID: "prepared-plan", Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: original.ActionID, Provider: input.Provider, Outcome: protocol.OutcomePass, PayloadDigest: "controlled-plan-result", Validation: &protocol.ValidationResult{Binding: input.Validation.Binding, Status: protocol.OutcomePass, GranularityReview: plan}}}
	if accepted, _, err := f.Submit(event); err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("original controlled plan was rejected", accepted, err)
	}
	if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("prepared plan did not publish topology", err)
	}
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlRequestRequirementChange})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(root, input.RequirementDocument.Path), strings.Replace(input.RequirementDocument.Content, "authoritative run binding", "current confirmed run binding", 1))
	choices := make([]facade.WorkChoice, len(run.RequirementWork))
	for i := range choices {
		choices[i] = facade.WorkChoice{Disposition: protocol.RequirementWorkRedo, Reason: "The changed run-binding requirement requires a current plan"}
	}
	askPosition := 0
	for i, ask := range run.PendingAsks {
		if ask.Control == protocol.ControlRequestRequirementChange {
			askPosition = i + 1
		}
	}
	request, err = f.PrepareControl(facade.ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition, Requirement: &facade.RequirementChangeInput{Classification: protocol.RequirementClassificationChanged, Redo: []string{"route"}, Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"current run binding"}, TransitiveImpacts: []string{"development plan"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(request); err != nil {
		t.Fatal(err)
	}
	confirmation, err := f.PrepareControl(facade.ControlInput{Ask: askPosition, Choice: "proceed", Reason: "Confirm the changed run binding and all original redo dispositions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(confirmation); err != nil {
		t.Fatal(err)
	}
	f, err = facade.Open(root, "consumer")
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.Show()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("replacement preparation was not issued", err)
	}
	var current facade.WorkerMaterial
	for _, ref := range run.WorkerMaterials {
		current = ref
	}
	if current.ActionID == original.ActionID {
		t.Fatal("replacement reused obsolete preparation")
	}
	// The real consumer publishes the signed invocation and starts its supervisor;
	// this local fixture records the method and refuses before any model call.
	consumed, err := consumeWorkflow(IO{}, f, root, "consumer")
	if err != nil || consumed.Boundary != "BLOCKED" || !strings.Contains(consumed.Blocked[current.ActionID], "native supervisor ended without an exit receipt") {
		t.Fatal("obsolete topology stopped the current native preparation", consumed.Boundary, consumed.Blocked, err)
	}
	directory := facade.WorkerExecutionDirectory(current, "")
	calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
	if err != nil || string(calls) != directory+"\n" {
		t.Fatal("consumer did not start exactly the current original supervisor", string(calls), err)
	}
	var native struct {
		Engine struct {
			WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
		} `json:"engine"`
	}
	body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil || json.Unmarshal(body, &native) != nil || native.Engine.WorkerMaterial != current {
		t.Fatal("consumer changed the original replacement material", native, err)
	}
}

func cleanupWorkflowSupervisor(t *testing.T, directory string) {
	t.Helper()
	t.Cleanup(func() {
		var process map[string]any
		if readJSONFileInto(filepath.Join(directory, "process.json"), &process) == nil {
			if child, err := os.FindProcess(int(process["pid"].(float64))); err == nil {
				_ = child.Kill()
			}
		}
	})
}

func TestWorkflowPendingAskStartsSignedChildrenAfterReopen(t *testing.T) {
	f, root, _, _ := workflowStartFixture(t)
	workflowSplitFixture(t, f, root)
	run, err := f.Show()
	if err != nil || len(run.SliceChildren) != 2 {
		t.Fatal("normal split did not retain its two children", run, err)
	}
	children := map[string]facade.WorkerMaterial{}
	for _, id := range run.SliceChildren {
		child, err := facade.Open(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if requested, err := child.RequestWorkflowChoice(); err != nil || !requested {
			t.Fatal("normal child has no original route choice", requested, err)
		}
		candidate, err := child.PrepareControl(facade.ControlInput{Ask: 1, Choice: "approve", Reason: "Validate the complete original child responsibility"})
		if err != nil {
			t.Fatal(err)
		}
		if accepted, _, err := child.SubmitControl(candidate); err != nil || accepted.Status != "ACCEPTED" {
			t.Fatal("normal child route was not accepted", accepted, err)
		}
		issued, err := child.Drive()
		if err != nil || len(issued.WorkerMaterials) != 1 {
			t.Fatal("normal child did not issue its original worker", issued, err)
		}
		for _, ref := range issued.WorkerMaterials {
			children[id] = ref
			cleanupWorkflowSupervisor(t, facade.WorkerExecutionDirectory(ref, ""))
		}
	}
	run, err = f.Show()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("normal parent did not retain its signed integration worker", run, err)
	}
	for _, ref := range run.WorkerMaterials {
		children[run.RunID] = ref
		cleanupWorkflowSupervisor(t, facade.WorkerExecutionDirectory(ref, ""))
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	processes := map[string]string{}
	for iteration := range 2 {
		f, err = facade.Open(root, "consumer")
		if err != nil {
			t.Fatal(err)
		}
		consumed, err := consumeWorkflow(IO{}, f, root, "consumer")
		if err != nil || consumed.Boundary != "ASK" || len(consumed.Blocked) != 0 {
			t.Fatal("parent Ask blocked signed child startup", consumed, err)
		}
		for id, ref := range children {
			if len(consumed.Children[id].Blocked) != 0 {
				t.Fatal("child launch did not preserve its admission", consumed.Children[id])
			}
			directory := facade.WorkerExecutionDirectory(ref, "")
			var request struct {
				Engine struct {
					Root           string                `json:"root"`
					RunID          string                `json:"runId"`
					WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
				} `json:"engine"`
			}
			body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
			if err != nil || json.Unmarshal(body, &request) != nil || request.Engine.Root != root || request.Engine.RunID != id || request.Engine.WorkerMaterial != ref || !harness.NativeJobAlive(directory) {
				t.Fatal("child did not start its original signed material", request, err)
			}
			process, err := os.ReadFile(filepath.Join(directory, "process.json"))
			if err != nil {
				t.Fatal(err)
			}
			if iteration == 0 {
				processes[id] = string(process)
			} else if processes[id] != string(process) {
				t.Fatal("reopened parent repeated a child native process", id)
			}
		}
	}
	calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
	if err != nil || len(strings.Split(strings.TrimSpace(string(calls)), "\n")) != len(children) {
		t.Fatal("actual child starts differ from the original signed children", string(calls), err)
	}
	for _, ref := range children {
		if strings.Count(string(calls), "harness|supervise|"+facade.WorkerExecutionDirectory(ref, "")+"\n") != 1 {
			t.Fatal("signed material did not have exactly one actual native start", ref)
		}
	}
}

func workflowResumeFixture(t *testing.T) (*facade.Facade, string, facade.WorkerMaterial, protocol.HostActionIntent, string) {
	t.Helper()
	f, root, ref, originalDirectory := workflowStartFixture(t)
	_, _, original, finished, err := startWorkflowWorker(context.Background(), f, ref, protocol.HostActionIntent{})
	if err != nil || original != ref || finished == nil {
		t.Fatal("original native execution was not launched", original, err)
	}
	if err := f.ObserveWorkerStart(ref, originalDirectory, harness.NativeStartEvidence); err != nil {
		t.Fatal(err)
	}
	var process map[string]any
	if err := readJSONFileInto(filepath.Join(originalDirectory, "process.json"), &process); err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(int(process["pid"].(float64)))
	if err != nil || child.Signal(syscall.SIGTERM) != nil {
		t.Fatal("controlled original process could not stop", err)
	}
	if err := <-finished; err != nil {
		t.Fatal("controlled original process did not stop normally", err)
	}
	if err := f.ObserveWorkerTerminal(ref, "controlled-session"); err != nil {
		t.Fatal(err)
	}
	stop, err := protocol.NewLifecycleEventEvent("controlled-stop", "workbuddy", "controlled-session", protocol.LifecycleStop)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, _, err := f.Submit(stop); err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("original controlled stop was rejected", accepted, err)
	}
	// The fixture supplies the observed host outcome through the public protocol.
	failure, err := protocol.NewWorkerResultEvent(facade.WorkerFailureEventID(ref, ""), ref.ActionID, "workbuddy", protocol.OutcomeRuntimeError, "controlled-original-process-ended", "USER_ACTION_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	failure.Result.InterruptionReason = "The controlled native process ended without completing its original review."
	if accepted, _, err := f.Submit(failure); err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("observed original failure was rejected", accepted, err)
	}
	run, err := f.Drive()
	if err != nil || len(run.PendingAsks) != 1 || run.PendingAsks[0].Control != protocol.ControlRecovery {
		t.Fatal("original incomplete execution has no recovery choice", run, err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "resume", Reason: "Continue the original controlled native session"})
	if err != nil {
		t.Fatal(err)
	}
	if accepted, _, err := f.SubmitControl(candidate); err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("original resume choice was rejected", accepted, err)
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var resume protocol.HostActionIntent
	for _, intent := range run.AdapterIntents {
		if intent.Resume != nil && intent.Resume.WorkerActionID == ref.ActionID {
			resume = intent
		}
	}
	if resume.Resume == nil || resume.Resume.AttemptID != ref.AttemptID || resume.Resume.Identity != "controlled-session" {
		t.Fatal("resume changed the original action, attempt or native identity", resume)
	}
	return f, root, ref, resume, originalDirectory
}

func TestWorkflowPendingAskStartsOriginalResumeAndReusesOwnership(t *testing.T) {
	f, root, ref, resume, originalDirectory := workflowResumeFixture(t)
	previous := facade.NativeStartVerifier
	facade.NativeStartVerifier = harness.NativeStartEvidence
	t.Cleanup(func() { facade.NativeStartVerifier = previous })
	directory := facade.WorkerExecutionDirectory(ref, resume.ActionID)
	cleanupWorkflowSupervisor(t, directory)
	candidate, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	var originalProcess string
	for iteration := range 2 {
		f, err = facade.Open(root, "consumer")
		if err != nil {
			t.Fatal(err)
		}
		result, err := consumeWorkflow(IO{}, f, root, "consumer")
		if err != nil || result.Boundary != "ASK" || len(result.Blocked) != 0 {
			t.Fatal("pending Ask lost the signed original resume", result, err)
		}
		var native struct {
			Identity string `json:"identity"`
			Engine   struct {
				WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
				ResumeActionID string                `json:"resumeActionId"`
			} `json:"engine"`
		}
		body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil || json.Unmarshal(body, &native) != nil || native.Identity != resume.Resume.Identity || native.Engine.WorkerMaterial.ActionID != ref.ActionID || native.Engine.WorkerMaterial.AttemptID != ref.AttemptID || native.Engine.ResumeActionID != resume.ActionID || !harness.NativeJobAlive(directory) {
			t.Fatal("actual resume lost its original signed identity", native, err)
		}
		process, err := os.ReadFile(filepath.Join(directory, "process.json"))
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 0 {
			originalProcess = string(process)
		} else if originalProcess != string(process) {
			t.Fatal("reopened consumption repeated the original resume")
		}
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Content protocol.State `json:"content"`
	}
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	state := document.Content
	receipt := state.HostActionReceipts[resume.ActionID]
	if receipt.Status != protocol.HostActionStatusExecuted || receipt.PayloadDigest != resume.PayloadDigest || receipt.LifecycleEvidence == nil || receipt.LifecycleEvidence.Identity != resume.Resume.Identity || run.AdapterIntents[resume.ActionID].Resume != nil {
		t.Fatal("native start did not settle only its original resume execution", receipt)
	}
	for _, attempt := range run.Attempts {
		if attempt.ActionID == ref.ActionID && (attempt.ID != ref.AttemptID || attempt.Attempts != 1) {
			t.Fatal("native resume replay changed its original attempt count", attempt)
		}
	}
	calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
	want := "harness|supervise|" + originalDirectory + "\nharness|supervise|" + directory + "\n"
	if err != nil || string(calls) != want {
		t.Fatal("actual original and resume starts differ", string(calls), err)
	}
}

// Original claimed supervisor facts survive a cancelled collector and remain
// independent of whether the native invocation supplied a semantic result.
func TestWorkflowAbortSettlesOriginalResume(t *testing.T) {
	for _, execution := range []string{"unclaimed", "running", "exited"} {
		t.Run(execution, func(t *testing.T) {
			f, root, ref, resume, originalDirectory := workflowResumeFixture(t)
			previous := facade.NativeStartVerifier
			facade.NativeStartVerifier = harness.NativeStartEvidence
			t.Cleanup(func() { facade.NativeStartVerifier = previous })
			directory := facade.WorkerExecutionDirectory(ref, resume.ActionID)
			cleanupWorkflowSupervisor(t, directory)
			var finished <-chan error
			if execution != "unclaimed" {
				request, observedDirectory, original, completed, err := startWorkflowWorker(context.Background(), f, ref, resume)
				if err != nil || observedDirectory != directory || original != ref || request["identity"] != resume.Resume.Identity {
					t.Fatal("original resume execution changed its signed invocation", original, request, err)
				}
				finished = completed
				if execution == "exited" {
					var process map[string]any
					if err := readJSONFileInto(filepath.Join(directory, "process.json"), &process); err != nil {
						t.Fatal(err)
					}
					child, err := os.FindProcess(int(process["pid"].(float64)))
					if err != nil || child.Signal(syscall.SIGTERM) != nil {
						t.Fatal("controlled original resume did not stop", err)
					}
					if err := <-finished; err != nil {
						t.Fatal(err)
					}
				}
			}
			request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.SubmitControl(request); err != nil {
				t.Fatal(err)
			}
			confirmation, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "abort", Reason: "Stop the original signed native resume"})
			if err != nil {
				t.Fatal(err)
			}
			accepted, run, err := f.SubmitControl(confirmation)
			if err != nil || accepted.Status != "ACCEPTED" || len(run.PendingActions) != 1 {
				t.Fatal("control dropped its original resume before observing its native execution", accepted, run.PendingActions, err)
			}
			for range 2 {
				f, err = facade.Open(root, "consumer")
				if err != nil {
					t.Fatal(err)
				}
				result, err := consumeWorkflow(IO{}, f, root, "consumer")
				if err != nil || result.Boundary != "ABORTED" || len(result.Blocked) != 0 {
					t.Fatal("original native resume prevented typed abort", result, err)
				}
				run = result.Run
			}
			if harness.NativeJobAlive(directory) || run.Termination == nil || len(run.Termination.PendingActions) != 0 || len(run.Termination.PendingHostActions) != 0 {
				t.Fatal("abort left original resumed execution or signed responsibility", run.Termination)
			}
			if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json")); !os.IsNotExist(err) {
				t.Fatal("abort retained active state", err)
			}
			if replay, _, err := f.SubmitControl(confirmation); err != nil || !reflect.DeepEqual(replay, accepted) {
				t.Fatal("original typed abort did not replay", replay, err)
			}
			receipt, executed := run.Termination.HostActionReceipts[resume.ActionID]
			if execution == "unclaimed" {
				if executed || run.Termination.ReconciledEffects[resume.ActionID].Status != "WITHDRAWN" {
					t.Fatal("unclaimed resume fabricated an execution", receipt)
				}
			} else {
				if !executed || receipt.Status != protocol.HostActionStatusExecuted || receipt.PayloadDigest != resume.PayloadDigest || receipt.LifecycleEvidence == nil || receipt.LifecycleEvidence.Identity != resume.Resume.Identity {
					t.Fatal("observed resume lost its exact original execution receipt", receipt)
				}
				attempt := run.Termination.ObsoleteActions[ref.ActionID]
				if attempt.AttemptID != ref.AttemptID {
					t.Fatal("abort lost original action/attempt", attempt)
				}
				if effect := run.Termination.ReconciledEffects[resume.ActionID]; effect.Status != "FULFILLED" || effect.Intent == nil || !reflect.DeepEqual(*effect.Intent, resume) {
					t.Fatal("resume lost its original signed responsibility", effect)
				}
				start, stop := false, false
				for _, event := range run.Termination.LifecycleEvents {
					if event.Correlation == resume.ActionID && event.Identity == resume.Resume.Identity {
						start = start || event.Event == protocol.LifecycleStart
						stop = stop || event.Event == protocol.LifecycleStop
					}
				}
				if !start || !stop {
					t.Fatal("resume was retired without its own observed start/stop")
				}
			}
			calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
			want := "harness|supervise|" + originalDirectory + "\n"
			if execution != "unclaimed" {
				want += "harness|supervise|" + directory + "\n"
			}
			if err != nil || string(calls) != want {
				t.Fatal("abort or recovery repeated a native start", string(calls), err)
			}
		})
	}
}

func TestWorkflowWorkerWaitsForOriginalExitAfterResult(t *testing.T) {
	previous := facade.NativeSourceVerifier
	facade.NativeSourceVerifier = harness.NativeResultEvidence
	t.Cleanup(func() { facade.NativeSourceVerifier = previous })
	f, root, ref, directory := workflowStartFixture(t)
	_, _, _, finished, err := startWorkflowWorker(context.Background(), f, ref, protocol.HostActionIntent{})
	if err != nil {
		t.Fatal(err)
	}
	workflowCompletedSource(t, directory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- executeWorkflowWorker(ctx, ctx, f, ref, protocol.HostActionIntent{}) }()
	select {
	case err := <-done:
		t.Fatalf("worker returned before its original exit receipt: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	stopWorkflowSupervisor(t, directory, finished)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertWorkflowResultAndCost(t, f, root, ref, directory)
}

func TestWorkflowWorkerRecoversOriginalCostPendingResult(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprint("reopen=", reopen), func(t *testing.T) {
			previousStart, previousSource := facade.NativeStartVerifier, facade.NativeSourceVerifier
			facade.NativeStartVerifier, facade.NativeSourceVerifier = harness.NativeStartEvidence, harness.NativeResultEvidence
			t.Cleanup(func() { facade.NativeStartVerifier, facade.NativeSourceVerifier = previousStart, previousSource })
			f, root, ref, directory := workflowStartFixture(t)
			_, _, _, finished, err := startWorkflowWorker(context.Background(), f, ref, protocol.HostActionIntent{})
			if err != nil {
				t.Fatal(err)
			}
			source := workflowCompletedSource(t, directory)
			if err := acceptWorkflowWorker(f, ref, protocol.HostActionIntent{}, directory, source); err != nil {
				t.Fatal(err)
			}
			stored := workflowStoredState(t, root)
			if stored.Events[string(facade.WorkerResultEventID(ref, ""))].Acceptance.Status != protocol.StatusCostPending || stored.StagedResults[ref.ActionID].Outcome != protocol.OutcomePass || stored.Results[ref.ActionID].Outcome != "" || stored.Cost == nil || len(stored.Cost.Dispatches) != 0 {
				t.Fatal("early original result was not held without charging or completing", stored.StagedResults, stored.Cost)
			}
			if _, _, _, _, err := f.WorkerExecutionRequest(ref, ""); err == nil {
				t.Fatal("delivered pending result permitted another execution request")
			}
			stopWorkflowSupervisor(t, directory, finished)
			if reopen {
				f, err = facade.Open(root, "consumer")
				if err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if _, err := f.Drive(); err != nil {
					t.Fatal(err)
				}
				assertWorkflowResultAndCost(t, f, root, ref, directory)
			}
		})
	}
}

// The fixture supplies a native terminal stream and submits the actual semantic
// tool call while the original supervisor remains alive without its exit receipt.
func workflowCompletedSource(t *testing.T, directory string) map[string]any {
	t.Helper()
	var request map[string]any
	if err := readJSONFileInto(filepath.Join(directory, "native-request.json"), &request); err != nil {
		t.Fatal(err)
	}
	var identity map[string]any
	if err := readJSONFileInto(filepath.Join(directory, "identity.json"), &identity); err != nil {
		t.Fatal(err)
	}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"submit_semantics","arguments":{"findings":[]}}}`
	var out, errout bytes.Buffer
	if code := harness.NativeTools(directory, strings.NewReader(call), &out, &errout, harness.ExecutionServices{Admit: facade.AdmitExecution}); code != 0 {
		t.Fatal("native semantic tool failed", code, errout.String())
	}
	var reply struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.Result.IsError {
		t.Fatal("original semantic call was rejected", out.String(), err)
	}
	selection := map[string]any{"model": nil, "reasoning_effort": nil}
	terminal := map[string]any{"type": "result", "subtype": "success", "uuid": "controlled-result", "session_id": identity["identity"], "is_error": false}
	events := []any{map[string]any{"type": "system", "subtype": "init", "session_id": identity["identity"], "cwd": request["workspace"]}, terminal}
	var stream bytes.Buffer
	for _, event := range events {
		if err := json.NewEncoder(&stream).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteCLI(t, filepath.Join(directory, "native-events.jsonl"), stream.String())
	mustWriteCLI(t, filepath.Join(directory, "answer.json"), `{"findings":[]}`)
	mustWriteCLI(t, filepath.Join(directory, "native-usage.json"), `[{"id":"controlled-response","session_id":"controlled-session","providerData":{"usage":{"inputTokens":120,"outputTokens":30,"inputTokensDetails":[{"cached_tokens":20}]}}}]`)
	if err := writeInvocationFile(filepath.Join(directory, "native-source.json"), map[string]any{"sessionId": identity["identity"], "workspace": request["workspace"], "selection": selection, "process": map[string]any{"exit_code": 0, "error": ""}}); err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	for _, name := range []string{"native-source.json", "native-events.jsonl", "answer.json", "native-usage.json", "native-tool-calls.jsonl", "native-tool-context.json"} {
		body, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = map[string]any{"sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "size": len(body)}
	}
	source := map[string]any{"provider": request["provider"], "identity": identity["identity"], "selection": selection, "native_result": terminal, "usage_evidence": nil, "transcript": filepath.Join(directory, "native-usage.json"), "files": files}
	if err := writeInvocationFile(filepath.Join(directory, "source.json"), source); err != nil {
		t.Fatal(err)
	}
	verified, err := harness.NativeJobSource(request["provider"].(string), directory)
	if err != nil {
		t.Fatal("original source did not pass its real provider verifier", err)
	}
	return verified
}

func stopWorkflowSupervisor(t *testing.T, directory string, finished <-chan error) {
	t.Helper()
	var process map[string]any
	if err := readJSONFileInto(filepath.Join(directory, "process.json"), &process); err != nil {
		t.Fatal(err)
	}
	owner, err := os.FindProcess(int(process["pid"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func assertWorkflowResultAndCost(t *testing.T, f *facade.Facade, root string, ref facade.WorkerMaterial, directory string) {
	t.Helper()
	s := workflowStoredState(t, root)
	entry := s.Cost.Dispatches[ref.ActionID]
	if s.Results[ref.ActionID].Outcome != protocol.OutcomePass || s.StagedResults[ref.ActionID].Outcome != "" || s.Events["native-stopped:"+ref.Digest+":"].Acceptance.Status != "ACCEPTED" || len(s.Cost.Dispatches) != 1 || entry.TotalInputTokens != 120 || entry.InputCacheHitTokens != 20 || entry.InputCacheMissTokens != 100 || entry.OutputTokens != 30 || s.Cost.TotalInputTokens != 120 || s.Cost.OutputTokens != 30 {
		t.Fatal("original result/stop/exact cost did not close once", s.Results[ref.ActionID], s.StagedResults[ref.ActionID], entry, s.Cost)
	}
	calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
	if err != nil || string(calls) != "harness|supervise|"+directory+"\n" {
		t.Fatal("original invocation was repeated", string(calls), err)
	}
	if _, _, _, _, err := f.WorkerExecutionRequest(ref, ""); err == nil {
		t.Fatal("completed result permitted another execution request")
	}
}

func workflowStoredState(t *testing.T, root string) protocol.State {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "consumer", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Content protocol.State `json:"content"`
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	return stored.Content
}
