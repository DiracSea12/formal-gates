//go:build !windows

package cli

import (
	"context"
	"encoding/json"
	"errors"
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
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
)

// The controlled supervisor records actual execution ownership and a native
// init acknowledgement without issuing any model turn.
func TestWorkflowSupervisorProcess(t *testing.T) {
	if os.Getenv("WORKFLOW_SUPERVISOR_PROCESS") != "1" {
		return
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
}

func workflowStartFixture(t *testing.T) (*facade.Facade, string, facade.WorkerMaterial, string) {
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
	registry, target, generation, lease, token := testkit.Installation(t, root, "consumer-runtime", "sha256:package", "workbuddy")
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
	if err := os.MkdirAll(filepath.Join(target, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "bin", "formal-gates"), []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	admission := &facade.Admission{RegistryPath: registry, Target: target, Root: root, Provider: "workbuddy", PackageDigest: "sha256:package", InstalledTargetIdentity: "consumer-runtime", Generation: generation, Lease: lease, Token: token}
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
	f, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, VCSRunner: runner, Admission: admission, Request: facade.StartRequest{RunID: "consumer", Route: "regular", BaseSnapshot: strings.TrimSpace(base), IntakeConfirmationReceipt: receipt}})
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
		candidate, err := child.PrepareControl(facade.ControlInput{Ask: 1, Choice: "full", Reason: "Validate the complete original child responsibility"})
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

func TestWorkflowPendingAskStartsOriginalResumeAndReusesOwnership(t *testing.T) {
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
	directory := facade.WorkerExecutionDirectory(ref, resume.ActionID)
	cleanupWorkflowSupervisor(t, directory)
	candidate, err = f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
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
	calls, err := os.ReadFile(filepath.Join(root, "supervisor-calls"))
	want := "harness|supervise|" + originalDirectory + "\nharness|supervise|" + directory + "\n"
	if err != nil || string(calls) != want {
		t.Fatal("actual original and resume starts differ", string(calls), err)
	}
}
