package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

// Consumption reports the original engine projection and foreground boundary.
// Active calls below are local waits; original jobs own all execution facts.
type workflowConsumption struct {
	Run         facade.Run                     `json:"run"`
	Boundary    string                         `json:"boundary"`
	Blocked     map[string]string              `json:"blocked,omitempty"`
	Acceptance  *protocol.Acceptance           `json:"acceptance,omitempty"`
	Children    map[string]workflowConsumption `json:"children,omitempty"`
	HostWorkers map[string]workflowHostWorker  `json:"hostWorkers,omitempty"`
	HostActions []protocol.HostActionIntent    `json:"hostActions,omitempty"`
	Progress    struct {
		Issued    int `json:"issued"`
		Running   int `json:"running"`
		Decisions int `json:"decisions"`
	} `json:"progress"`
}

// workflowHostWorker projects original issued work for the controlling host.
// The host creates its real subagent; this value does not claim execution.
type workflowHostWorker struct {
	Material        facade.WorkerMaterial `json:"material"`
	Provider        string                `json:"provider"`
	Workspace       string                `json:"workspace"`
	Model           string                `json:"model,omitempty"`
	ReasoningEffort string                `json:"reasoningEffort,omitempty"`
	ResumeActionID  string                `json:"resumeActionId,omitempty"`
	Identity        string                `json:"identity,omitempty"`
	State           string                `json:"state,omitempty"`
	Worker          int                   `json:"worker"`
}

// Only foreground controls consume subsequent work. Native worker evidence
// uses the same Submit owner while this consumer holds its original lease.
func workflowControlEvent(event protocol.Event) bool {
	return event.Kind == protocol.KindRequestControl || event.Kind == protocol.KindDecide || event.Kind == protocol.KindOperatorObservation
}

func consumeWorkflow(streams IO, f *facade.Facade, root, runID string) (result workflowConsumption, err error) {
	parent := streams.Context
	if parent == nil {
		parent = context.Background()
	}
	executionContext, stop := signal.NotifyContext(parent, os.Interrupt)
	defer stop()
	streams.Context = executionContext
	return consumeWorkflowContext(streams, f, root, runID, executionContext)
}

func consumeWorkflowContext(streams IO, f *facade.Facade, root, runID string, executionContext context.Context) (result workflowConsumption, err error) {
	unlease, err := coordination.AcquirePath(filepath.Join(root, facade.EngineNamespace, runID, "executor.lock"), "workflow consumer")
	if err != nil {
		return result, err
	}
	defer unlease()
	ctx, stop := context.WithCancel(streams.Context)
	result.Blocked = map[string]string{}
	type completion struct {
		key, action string
		err         error
		child       *workflowConsumption
	}
	done := make(chan completion, 16)
	var waits sync.WaitGroup
	active := map[string]bool{}
	advance := true
	recordCompletion := func(finished completion) {
		delete(active, finished.key)
		advance = true
		if finished.child != nil {
			result.Children[finished.action] = *finished.child
		}
		if finished.err != nil && !errors.Is(finished.err, context.Canceled) {
			result.Blocked[finished.action] = finished.err.Error()
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		stop()
		for len(active) != 0 {
			recordCompletion(<-done)
		}
		waits.Wait()
		if latest, showErr := f.Show(); showErr == nil {
			result.Run = latest
		}
		for id, child := range result.Children {
			owner, showErr := facade.Open(root, id)
			if showErr == nil {
				child.Run, showErr = owner.Show()
			}
			if showErr != nil {
				if err == nil {
					err = showErr
				}
				continue
			}
			if len(child.Run.PendingAsks) != 0 {
				child.Boundary = string(decision.KindAsk)
			}
			result.Children[id] = child
		}
		result.Progress.Issued = len(result.Run.PendingActions)
		result.Progress.Running = len(active)
		result.Progress.Decisions = len(result.Run.PendingAsks)
		if err != nil && result.Boundary == "" {
			result.Boundary = "BLOCKED"
		}
	}()
	for {
		var run facade.Run
		var driveErr error
		if advance {
			run, driveErr = f.Drive()
			advance = false
		} else {
			run, driveErr = f.Show()
		}
		if driveErr != nil {
			return result, driveErr
		}
		result.Run = run
		result.HostWorkers = nil
		result.HostActions = nil
		if run.Status == "COMPLETE" || run.Status == "ABORTED" || run.Status == "SLICE_READY" {
			if len(active) == 0 {
				result.Boundary = run.Status
				return result, nil
			}
		} else {
			childIDs := make([]string, 0, len(run.SliceChildren))
			for _, id := range run.SliceChildren {
				childIDs = append(childIDs, id)
			}
			sort.Strings(childIDs)
			for _, id := range childIDs {
				child, err := facade.Open(root, id)
				if err != nil {
					return result, err
				}
				current, err := child.Show()
				if err != nil {
					return result, err
				}
				if result.Children == nil {
					result.Children = map[string]workflowConsumption{}
				}
				observed := result.Children[id]
				if observed.Run.Revision != current.Revision && (observed.Boundary == "HOST_WORKER" || observed.Boundary == "HOST_ACTION") {
					observed.Boundary = ""
					observed.HostWorkers = nil
					observed.HostActions = nil
				}
				observed.Run = current
				if len(current.PendingAsks) != 0 {
					observed.Boundary = string(decision.KindAsk)
				}
				result.Children[id] = observed
			}
			if len(run.PendingAsks) != 0 || run.Next.Kind == decision.KindAsk {
				stop()
			}
			for _, child := range result.Children {
				if len(child.Run.PendingAsks) != 0 {
					stop()
				}
			}
			for _, id := range childIDs {
				key := "child:" + id
				observed := result.Children[id]
				current := observed.Run
				if active[key] || current.Status == "COMPLETE" || current.Status == "ABORTED" || current.Status == "SLICE_READY" || len(current.PendingAsks) != 0 && len(current.WorkerMaterials) == 0 || observed.Boundary == "BLOCKED" || observed.Boundary == "HOST_WORKER" || observed.Boundary == "HOST_ACTION" {
					continue
				}
				active[key] = true
				waits.Add(1)
				go func() {
					defer waits.Done()
					childStreams := streams
					childStreams.Context = ctx
					child, err := facade.Open(root, id)
					var consumed workflowConsumption
					if err == nil {
						consumed, err = consumeWorkflowContext(childStreams, child, root, id, executionContext)
					}
					done <- completion{key: key, action: id, err: err, child: &consumed}
				}()
			}
			resumes := map[string]protocol.HostActionIntent{}
			checksPending := false
			for _, intent := range run.AdapterIntents {
				checksPending = checksPending || intent.Adapter != nil && intent.Adapter.Operation == "op.check"
				if intent.Resume != nil {
					resumes[intent.Resume.WorkerActionID] = intent
				}
				if intent.Terminate != nil && result.Blocked[intent.ActionID] == "" {
					if err := terminateWorkflowWorker(ctx, f, intent); err != nil {
						var duty *hostWorkerInterrupt
						if errors.As(err, &duty) {
							result.HostActions = append(result.HostActions, duty.intent)
						} else {
							result.Blocked[intent.ActionID] = err.Error()
						}
					}
				}
			}
			ids := make([]string, 0, len(run.WorkerMaterials))
			for id := range run.WorkerMaterials {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for index, id := range ids {
				ref, resume := run.WorkerMaterials[id], resumes[id]
				waiting := false
				for _, record := range run.RecoveryRecords {
					if record.ActionID == id && record.AttemptID == ref.AttemptID {
						for _, ask := range run.PendingAsks {
							if ask.RequestID == record.RequestID {
								waiting = true
							}
						}
					}
				}
				if waiting {
					continue
				}
				key := ref.ActionID + ":" + ref.AttemptID
				if active[key] || result.Blocked[id] != "" {
					continue
				}
				hostWorker, err := workflowWorkerForHost(f, streams, ref, resume)
				if err != nil {
					result.Blocked[id] = err.Error()
					continue
				}
				if hostWorker != nil {
					hostWorker.Worker = index + 1
					if hostWorker.State == "completed" {
						directory, _, err := f.HostWorkerExecution(hostWorker.Material, "", true)
						if err == nil {
							err = acceptWorkflowHostWorker(f, hostWorker.Material, directory)
						}
						if err != nil {
							result.Blocked[id] = err.Error()
						} else {
							advance = true
						}
						continue
					}
					if result.HostWorkers == nil {
						result.HostWorkers = map[string]workflowHostWorker{}
					}
					result.HostWorkers[id] = *hostWorker
					continue
				}
				active[key] = true
				waits.Add(1)
				go func() {
					defer waits.Done()
					executionErr := executeWorkflowWorker(ctx, executionContext, f, ref, resume)
					done <- completion{key: key, action: id, err: executionErr}
				}()
			}
			if advance {
				continue
			}
			if len(run.PendingAsks) != 0 || run.Next.Kind == decision.KindAsk {
				result.Boundary = string(decision.KindAsk)
				return result, nil
			}
			for _, child := range result.Children {
				if len(child.Run.PendingAsks) != 0 {
					result.Boundary = string(decision.KindAsk)
					return result, nil
				}
			}
			if len(active) == 0 {
				if len(result.HostActions) != 0 {
					result.Boundary = "HOST_ACTION"
					return result, nil
				}
				if len(result.HostWorkers) != 0 {
					result.Boundary = "HOST_WORKER"
					return result, nil
				}
				for _, child := range result.Children {
					if child.Boundary == "HOST_WORKER" || child.Boundary == "HOST_ACTION" {
						result.Boundary = child.Boundary
						return result, nil
					}
				}
				if len(result.Blocked) > 0 {
					result.Boundary = "BLOCKED"
					return result, nil
				}
				for _, child := range result.Children {
					if child.Boundary == "BLOCKED" {
						result.Boundary = "BLOCKED"
						return result, nil
					}
				}
				if requested, err := f.RequestWorkflowChoice(); err != nil {
					return result, err
				} else if requested {
					advance = true
					continue
				}
				if run.Next.Kind == decision.KindOperator && !checksPending {
					result.Boundary = "BLOCKED"
					return result, nil
				}
				if len(run.AdapterIntents) == 0 {
					result.Boundary = string(run.Next.Kind)
					return result, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			result.Boundary = "INTERRUPTED"
			return result, ctx.Err()
		case <-ticker.C:
			latest, showErr := f.Show()
			if showErr != nil {
				return result, showErr
			}
			advance = latest.Revision != run.Revision
			for _, intent := range run.AdapterIntents {
				if intent.Adapter != nil && intent.Adapter.Operation == "op.check" {
					directory := filepath.Join(root, facade.EngineNamespace, runID, "checks", strings.ReplaceAll(intent.ActionID, ":", "-"))
					if _, err := os.Stat(filepath.Join(directory, "exit.json")); err == nil || !harness.NativeJobAlive(directory) {
						advance = true
					}
				}
			}
		case finished := <-done:
			recordCompletion(finished)
		}
	}
}

func executeWorkflowWorker(ctx, executionContext context.Context, f *facade.Facade, ref facade.WorkerMaterial, resume protocol.HostActionIntent) error {
	request, directory, ref, finished, err := startWorkflowWorker(executionContext, f, ref, resume)
	if err != nil {
		return err
	}
	provider := request["provider"].(string)
	resume.ActionID = request["engine"].(map[string]any)["resumeActionId"].(string)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	started := false
	services := harness.ExecutionServices{ReceiptMessage: facade.RecordNativeMessage}
	for {
		if !started {
			if _, err := os.Stat(filepath.Join(directory, "identity.json")); err == nil {
				if err := f.ObserveWorkerStart(ref, directory, harness.NativeStartEvidence); err != nil {
					return err
				}
				started = true
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		var receipt map[string]any
		receiptErr := readJSONFileInto(filepath.Join(directory, "exit.json"), &receipt)
		if receiptErr == nil {
			source, sourceErr := harness.NativeJobSource(provider, directory, services)
			if sourceErr == nil {
				return acceptWorkflowWorker(f, ref, resume, directory, source)
			}
			return acceptWorkflowFailure(f, ref, resume, directory, request)
		}
		if _, err := os.Stat(filepath.Join(directory, "owner")); err == nil && finished == nil && !harness.NativeJobAlive(directory) {
			// Completion can land between the receipt read and process observation.
			if err := readJSONFileInto(filepath.Join(directory, "exit.json"), &receipt); err == nil {
				continue
			}
			return fmt.Errorf("native invocation is UNKNOWN; original job %s needs observation: %w", directory, receiptErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case executionErr := <-finished:
			finished = nil
			if _, err := os.Stat(filepath.Join(directory, "exit.json")); os.IsNotExist(err) {
				journal, _ := os.ReadFile(filepath.Join(directory, "supervisor.log"))
				return fmt.Errorf("native supervisor ended without an exit receipt: %v %s", executionErr, journal)
			}
		case <-ticker.C:
		}
	}
}

func workflowWorkerForHost(f *facade.Facade, streams IO, ref facade.WorkerMaterial, resume protocol.HostActionIntent) (*workflowHostWorker, error) {
	// Claimed independent invocations retain their original executor on reentry.
	claimed, err := f.HasNativeWorkerExecution(ref)
	if err != nil {
		return nil, err
	}
	if claimed {
		return nil, nil
	}
	request, _, _, original, err := f.WorkerExecutionRequest(ref, resume.ActionID)
	if err != nil {
		return nil, err
	}
	var start facade.StartRequest
	if err := readJSONFileInto(filepath.Join(filepath.Dir(filepath.Dir(ref.Path)), "request.json"), &start); err != nil {
		return nil, err
	}
	controller, err := host.Lookup(start.Provider)
	if err != nil {
		return nil, err
	}
	provider, err := host.Lookup(request["provider"].(string))
	if err != nil {
		return nil, err
	}
	if provider.ID != controller.ID {
		return nil, nil
	}
	if provider.ID != host.Codex {
		return nil, fmt.Errorf("HOST_SUBAGENT_UNAVAILABLE: verified native host worker integration is unavailable for %s", provider.InstallName)
	}
	if request["host"] == nil && provider.ID == host.Codex {
		run, err := f.Show()
		if err != nil {
			return nil, err
		}
		session, nativeHome, err := workflowWorkerCaller(streams, run)
		if err != nil {
			return nil, err
		}
		if err := f.PrepareHostWorker(original, resume.ActionID, session, nativeHome); err != nil {
			return nil, err
		}
	}
	model, _ := request["model"].(string)
	effort, _ := request["reasoning_effort"].(string)
	projected := &workflowHostWorker{Material: original, Provider: provider.InstallName, Workspace: request["workspace"].(string), Model: model, ReasoningEffort: effort, ResumeActionID: resume.ActionID}
	if request["host"] == nil && resume.ActionID == "" {
		projected.Identity, err = f.HostWorkerContinuation(original)
		if err != nil {
			return nil, err
		}
	}
	if request["host"] != nil {
		var source host.CodexInvocation
		body, err := json.Marshal(request["host"])
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &source); err != nil {
			return nil, err
		}
		directory, turn, err := f.HostWorkerExecution(original, "", true)
		if err != nil {
			return nil, err
		}
		if err := f.ObserveWorkerStart(original, directory, f.HostWorkerStartEvidence); err != nil {
			return nil, err
		}
		projected.Identity, projected.State = source.Session, turn.State
	}
	return projected, nil
}

func startWorkflowWorker(ctx context.Context, f *facade.Facade, ref facade.WorkerMaterial, resume protocol.HostActionIntent) (map[string]any, string, facade.WorkerMaterial, <-chan error, error) {
	request, supervised, directory, original, err := f.WorkerExecutionRequest(ref, resume.ActionID)
	if err != nil {
		return nil, "", facade.WorkerMaterial{}, nil, err
	}
	ref = original
	provider := request["provider"].(string)
	var finished <-chan error
	if _, err := os.Stat(filepath.Join(directory, "owner")); os.IsNotExist(err) {
		if err := ctx.Err(); err != nil {
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		if _, bound := request["binding"]; !bound {
			run, err := f.Show()
			if err != nil {
				return nil, "", facade.WorkerMaterial{}, nil, err
			}
			var configured settings.Effective
			if run.Settings != nil {
				configured = *run.Settings
			}
			configured.Values.Model, _ = request["model"].(string)
			configured.Values.ReasoningEffort, _ = request["reasoning_effort"].(string)
			var start facade.StartRequest
			if err := readJSONFileInto(filepath.Join(filepath.Dir(filepath.Dir(ref.Path)), "request.json"), &start); err != nil {
				return nil, "", facade.WorkerMaterial{}, nil, err
			}
			controller, err := host.Lookup(start.Provider)
			if err != nil {
				return nil, "", facade.WorkerMaterial{}, nil, err
			}
			if provider != controller.InstallName {
				configured, err = settings.Freeze(provider, configured)
				if err != nil {
					return nil, "", facade.WorkerMaterial{}, nil, err
				}
			}
			binding, err := harness.NativeBinding(provider, configured.Values)
			if err != nil {
				return nil, "", facade.WorkerMaterial{}, nil, err
			}
			request["binding"] = binding
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		if err := writeInvocationFile(filepath.Join(directory, "native-request.json"), request); err != nil {
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		if err := writeInvocationFile(filepath.Join(directory, "request.json"), supervised); err != nil {
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		argv := supervised["argv"].([]string)
		cmd := exec.Command(argv[0], "harness", "supervise", directory)
		harness.DetachSupervisor(cmd)
		journal, err := os.OpenFile(filepath.Join(directory, "supervisor.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		cmd.Stderr = journal
		if err := ctx.Err(); err != nil {
			journal.Close()
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		if err := cmd.Start(); err != nil {
			journal.Close()
			return nil, "", facade.WorkerMaterial{}, nil, err
		}
		completed := make(chan error, 1)
		finished = completed
		go func() { err := cmd.Wait(); journal.Close(); completed <- err }()
	} else if err != nil {
		return nil, "", facade.WorkerMaterial{}, nil, err
	}
	if finished != nil {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, ownerErr := os.Stat(filepath.Join(directory, "owner"))
			_, processErr := os.Stat(filepath.Join(directory, "process.json"))
			if ownerErr == nil && processErr == nil {
				break
			}
			if ownerErr != nil && !os.IsNotExist(ownerErr) {
				return nil, "", facade.WorkerMaterial{}, nil, ownerErr
			}
			if processErr != nil && !os.IsNotExist(processErr) {
				return nil, "", facade.WorkerMaterial{}, nil, processErr
			}
			select {
			case <-ctx.Done():
				return nil, "", facade.WorkerMaterial{}, nil, ctx.Err()
			case executionErr := <-finished:
				if _, err := os.Stat(filepath.Join(directory, "process.json")); err != nil {
					journal, _ := os.ReadFile(filepath.Join(directory, "supervisor.log"))
					return nil, "", facade.WorkerMaterial{}, nil, fmt.Errorf("native supervisor ended without an exit receipt: %v %s", executionErr, journal)
				}
				return request, directory, ref, nil, nil
			case <-ticker.C:
			}
		}
	}
	return request, directory, ref, finished, nil
}

func acceptWorkflowWorker(f *facade.Facade, ref facade.WorkerMaterial, resume protocol.HostActionIntent, directory string, source map[string]any) error {
	var original worker.Material
	if err := readJSONFileInto(ref.Path, &original); err != nil {
		return err
	}
	provider := original.Input.Provider
	nativeProvider := provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		nativeProvider = descriptor.InstallName
	}
	_, _, _, err := harness.NativeResultEvidence(nativeProvider, directory, source)
	if err != nil {
		return err
	}
	if err := f.ObserveWorkerStart(ref, directory, harness.NativeStartEvidence); err != nil {
		return err
	}

	_, _, err = f.SubmitWorkerResult(facade.WorkerResultEventID(ref, resume.ActionID), ref, directory, source, harness.NativeWorkerResultEvidence)
	return err
}

func acceptWorkflowFailure(f *facade.Facade, ref facade.WorkerMaterial, resume protocol.HostActionIntent, directory string, request map[string]any) error {
	body, digest, nativeIdentity, err := harness.NativeFailureEvidence(request["provider"].(string), directory)
	if err != nil {
		return fmt.Errorf("native invocation cannot prove a completed failure: %w", err)
	}
	var failure struct {
		Class authoring.FailureClass `json:"failure_class"`
		Error string                 `json:"error"`
	}
	if err := json.Unmarshal(body, &failure); err != nil {
		return err
	}
	var original worker.Material
	if err := readJSONFileInto(ref.Path, &original); err != nil {
		return err
	}
	provider := original.Input.Provider
	if nativeIdentity == "" {
		event, err := protocol.NewSpawnReceiptWithFailureClass(facade.WorkerSpawnFailureEventID(ref), ref.ActionID, provider, directory, protocol.SpawnStatusFailed, failure.Class)
		if err != nil {
			return err
		}
		_, _, err = f.Submit(event)
		return err
	}
	if err := f.ObserveWorkerStart(ref, directory, harness.NativeStartEvidence); err != nil {
		return err
	}
	event, err := protocol.NewWorkerResultEvent(facade.WorkerFailureEventID(ref, resume.ActionID), ref.ActionID, provider, protocol.OutcomeRuntimeError, digest, failure.Class)
	if err != nil {
		return err
	}
	event.Result.InterruptionReason = failure.Error
	_, _, err = f.Submit(event)
	return err
}

type hostWorkerInterrupt struct{ intent protocol.HostActionIntent }

func (e *hostWorkerInterrupt) Error() string {
	return "HOST_INTERRUPT_REQUIRED: interrupt the original native child " + e.intent.Terminate.Identity + " using the host subagent tool, then drive to observe its actual terminal turn"
}

func terminateWorkflowWorker(ctx context.Context, f *facade.Facade, intent protocol.HostActionIntent) error {
	if handled, stopped, err := f.HostWorkerTermination(intent); err != nil {
		return err
	} else if handled {
		if !stopped {
			return &hostWorkerInterrupt{intent: intent}
		}
		return nil
	}
	provider, directories, release, err := f.AgentExecution(intent)
	if err != nil {
		return err
	}
	if len(directories) == 0 {
		release()
		return fmt.Errorf("native termination has no original invocation evidence")
	}
	for _, directory := range directories {
		var identity map[string]any
		if err := readJSONFileInto(filepath.Join(directory, "identity.json"), &identity); err != nil {
			release()
			return err
		}
		if identity["identity"] != intent.Terminate.Identity {
			release()
			return fmt.Errorf("native termination identity differs from its original invocation")
		}
		if harness.NativeJobAlive(directory) {
			if err := harness.StopNativeJob(directory); err != nil {
				release()
				return err
			}
		}
	}
	release()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for _, directory := range directories {
		for harness.NativeJobAlive(directory) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		var receipt map[string]any
		if err := readJSONFileInto(filepath.Join(directory, "exit.json"), &receipt); err != nil {
			return fmt.Errorf("native termination is UNKNOWN without its exit receipt: %w", err)
		}
		var request struct {
			Engine struct {
				WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
			} `json:"engine"`
		}
		body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &request); err != nil {
			return err
		}
		if err := f.ObserveWorkerStart(request.Engine.WorkerMaterial, directory, harness.NativeStartEvidence); err != nil {
			return err
		}
	}
	event, err := protocol.NewAgentHostActionReceiptEvent(protocol.EventID("native-termination:"+intent.ActionID), intent.ActionID, protocol.HostActionTerminateAgent, provider, nativeIntentCorrelation(intent), intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
	if err != nil {
		return err
	}
	_, _, err = f.Submit(event)
	return err
}

func nativeIntentCorrelation(intent protocol.HostActionIntent) string {
	if intent.Correlation != "" {
		return intent.Correlation
	}
	return intent.ActionID
}

func writeInvocationFile(path string, value any) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0600)
}

func printWorkflowConsumption(streams IO, value any, executionErr error) (int, error) {
	if code, err := printJSON(streams.Stdout, value); err != nil {
		return code, err
	}
	if executionErr != nil {
		fmt.Fprintln(streams.Stderr, executionErr)
		if errors.Is(executionErr, context.Canceled) {
			return 130, nil
		}
		return 1, nil
	}
	return 0, nil
}
