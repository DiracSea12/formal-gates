package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/harness"
)

type workflowWorkerInput struct {
	Root      string          `json:"root"`
	Run       int             `json:"run,omitempty"`
	Child     int             `json:"child,omitempty"`
	Worker    int             `json:"worker,omitempty"`
	Operation string          `json:"operation"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Material  string          `json:"material,omitempty"`
}

func runWorkflowWorker(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow worker", streams)
	input := workflowWorkerInput{}
	fs.StringVar(&input.Root, "root", "", "project directory; original --material derives its scope")
	fs.StringVar(&input.Material, "material", "", "original published material path; stable across parallel work")
	fs.IntVar(&input.Run, "run", 0, "original run position")
	fs.IntVar(&input.Child, "child", 0, "original child position")
	fs.IntVar(&input.Worker, "worker", 0, "original worker position for claim or observation")
	fs.StringVar(&input.Operation, "operation", "", "claim, workspace, submit or observe")
	path := fs.String("input", "", "original operation arguments JSON; - reads stdin")
	inline := fs.String("arguments", "", "original operation arguments as one JSON argument")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *path != "" && *inline != "" {
		return 1, fmt.Errorf("select --input or --arguments")
	}
	if *inline != "" {
		input.Arguments = json.RawMessage(*inline)
	}
	if *path != "" {
		var arguments json.RawMessage
		if err := readWorkflowInput(*path, streams.Stdin, &arguments); err != nil {
			return 1, err
		}
		input.Arguments = arguments
	}
	value, err := callWorkflowWorker(input, streams)
	return printValue(streams.Stdout, value, err)
}

func callWorkflowWorker(input workflowWorkerInput, streams IO) (any, error) {
	var runID string
	var f *facade.Facade
	var stable facade.WorkerMaterial
	var err error
	if input.Material != "" {
		if input.Root != "" || input.Run != 0 || input.Child != 0 || input.Worker != 0 {
			return nil, fmt.Errorf("original material derives its scope; omit root/run/child/worker positions")
		}
		f, stable, err = facade.OpenWorkerMaterial(input.Material)
		if err != nil {
			return nil, err
		}
	} else {
		if input.Root == "" {
			input.Root = "."
		}
		var selected bool
		f, selected, err = selectWorkflowRun(input.Root, &runID, input.Run, streams.Stdout)
		if err != nil {
			return nil, err
		}
		if !selected || f == nil {
			return nil, fmt.Errorf("select an original workflow run")
		}
	}
	if input.Child != 0 {
		runs, err := f.ControlRuns()
		if err != nil {
			return nil, err
		}
		if input.Child < 1 || input.Child >= len(runs) {
			return nil, fmt.Errorf("select an original child position")
		}
		f, err = facade.Open(input.Root, runs[input.Child].RunID)
		if err != nil {
			return nil, err
		}
	}
	run, err := f.Show()
	if err != nil {
		return nil, err
	}
	session, nativeHome, err := workflowWorkerCaller(streams, run)
	if err != nil {
		return nil, err
	}
	var ref facade.WorkerMaterial
	if stable.Path != "" {
		ref = stable
	} else if input.Operation == "claim" || input.Operation == "observe" {
		ids := make([]string, 0, len(run.WorkerMaterials))
		for id := range run.WorkerMaterials {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if input.Worker < 1 || input.Worker > len(ids) {
			return nil, fmt.Errorf("select an original issued worker position")
		}
		ref = run.WorkerMaterials[ids[input.Worker-1]]
	} else {
		ref, err = f.HostWorkerMaterial(session)
		if err != nil {
			return nil, err
		}
	}
	if input.Operation == "claim" {
		var resume string
		for _, intent := range run.AdapterIntents {
			if intent.Resume != nil && intent.Resume.WorkerActionID == ref.ActionID {
				resume = intent.ActionID
			}
		}
		directory, err := f.ClaimHostWorker(ref, resume, session, nativeHome)
		if err != nil {
			return nil, err
		}
		schemas, err := harness.HostWorkerToolSchemas(directory)
		if err != nil {
			return nil, err
		}
		return map[string]any{"claimed": true, "material": ref, "tools": schemas, "instruction": "The original material must have been read completely before claim. Use this exact material path for every call. Call worker operation=workspace with original workspace arguments or operation=submit with direct submit_semantics. CLI: workflow worker --material <original path> --operation workspace|submit --arguments '<JSON>'. Direct native reads, edits and commands are not permitted."}, nil
	}
	directory, turn, err := f.HostWorkerExecution(ref, session, input.Operation == "observe")
	if err != nil {
		return nil, err
	}
	if input.Operation == "observe" {
		if turn.State != "completed" {
			return map[string]any{"state": turn.State, "model": turn.Model, "reasoningEffort": turn.Effort}, nil
		}
		if err := acceptWorkflowHostWorker(f, ref, directory); err != nil {
			return nil, err
		}
		return map[string]any{"state": "completed", "accepted": true}, nil
	}
	if input.Operation != "workspace" && input.Operation != "submit" {
		return nil, fmt.Errorf("unknown worker operation %s", input.Operation)
	}
	if len(input.Arguments) == 0 {
		return nil, fmt.Errorf("worker operation requires its original arguments")
	}
	return harness.InvokeHostWorkerTool(directory, input.Operation, session, input.Arguments, harness.ExecutionServices{Admit: facade.AdmitExecution})
}

func workflowWorkerCaller(streams IO, run facade.Run) (string, string, error) {
	session := os.Getenv("CODEX_THREAD_ID")
	if streams.owner != nil && streams.owner.Session != "" {
		session = streams.owner.Session
	}
	if session == "" {
		return "", "", fmt.Errorf("HOST_CONTEXT_UNAVAILABLE: native calling conversation is unavailable")
	}
	nativeHome := ""
	if run.Settings != nil {
		nativeHome = run.Settings.Values.Native["codexConfigRoot"]
	}
	if nativeHome == "" {
		nativeHome = os.Getenv("CODEX_HOME")
	}
	if nativeHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		nativeHome = filepath.Join(home, ".codex")
	}
	return session, nativeHome, nil
}

func acceptWorkflowHostWorker(f *facade.Facade, ref facade.WorkerMaterial, directory string) error {
	if err := f.ObserveWorkerStart(ref, directory, f.HostWorkerStartEvidence); err != nil {
		return err
	}
	var binding struct {
		Engine struct {
			Resume string `json:"resumeActionId"`
		} `json:"engine"`
	}
	body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &binding); err != nil {
		return err
	}
	source, err := harness.HostWorkerResultSource(directory)
	if err != nil {
		if errors.Is(err, harness.ErrSemanticSubmissionMissing) {
			var request map[string]any
			if err := json.Unmarshal(body, &request); err != nil {
				return err
			}
			return acceptWorkflowFailure(f, ref, protocol.HostActionIntent{ActionID: binding.Engine.Resume}, directory, request)
		}
		return err
	}
	_, _, err = f.SubmitWorkerResult(facade.WorkerResultEventID(ref, binding.Engine.Resume), ref, directory, source, harness.NativeWorkerResultEvidence)
	return err
}
