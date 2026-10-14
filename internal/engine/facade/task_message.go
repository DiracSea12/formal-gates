package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"formal-gates/internal/engine/protocol"
)

func (f *Facade) QueueWorkerMessage(worker int, text string) (protocol.TaskMessage, error) {
	release, err := f.acquireWriteWindow()
	if err != nil {
		return protocol.TaskMessage{}, err
	}
	defer release()
	snap, err := f.engine.Load()
	if err != nil {
		return protocol.TaskMessage{}, err
	}
	ids := workerActionOrder(snap.State.WorkerInputs(f.definition))
	if worker < 1 || worker > len(ids) {
		return protocol.TaskMessage{}, fmt.Errorf("message worker is not one of the current workers")
	}
	return f.engine.QueueTaskMessage(ids[worker-1], text, snap.Revision, protocol.Observation{})
}

// Message delivery resolves the supervisor's original issued material. It never
// accepts a caller-selected task, attempt or native session.
func nativeMessageOwner(directory, identity string) (*Facade, WorkerMaterial, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, WorkerMaterial{}, err
	}
	var request struct {
		Engine struct {
			Root     string         `json:"root"`
			RunID    string         `json:"runId"`
			Material WorkerMaterial `json:"workerMaterial"`
			Resume   string         `json:"resumeActionId"`
		} `json:"engine"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, WorkerMaterial{}, err
	}
	ref := request.Engine.Material
	if ref.Path == "" {
		return nil, WorkerMaterial{}, nil
	}
	f, err := Open(request.Engine.Root, request.Engine.RunID)
	if err != nil {
		return nil, WorkerMaterial{}, err
	}
	if !sameExecutionPath(directory, WorkerExecutionDirectory(ref, request.Engine.Resume)) {
		return nil, WorkerMaterial{}, fmt.Errorf("MESSAGE_BINDING: original native job required")
	}
	if _, err := readWorkerMaterial(ref); err != nil {
		return nil, WorkerMaterial{}, err
	}
	return f, ref, nil
}
func PendingNativeMessage(directory, identity string, begin bool) (*protocol.TaskMessage, error) {
	f, ref, err := nativeMessageOwner(directory, identity)
	if err != nil || f == nil {
		return nil, err
	}
	release, err := f.acquireWriteWindow()
	if err != nil {
		return nil, err
	}
	defer release()
	return f.engine.ClaimTaskMessage(ref.ActionID, ref.AttemptID, identity, protocol.Observation{}, begin)
}
func RecordNativeMessage(directory string, message protocol.TaskMessage) error {
	f, ref, err := nativeMessageOwner(directory, message.Identity)
	if err != nil {
		return err
	}
	if f == nil || ref.ActionID != message.ActionID || ref.AttemptID != message.AttemptID {
		return fmt.Errorf("MESSAGE_BINDING: delivery owner differs")
	}
	release, err := f.acquireWriteWindow()
	if err != nil {
		return err
	}
	defer release()
	return f.engine.RecordTaskMessage(message, protocol.Observation{})
}

func workerActionOrder(inputs map[string]protocol.WorkerInput) []string {
	ids := make([]string, 0, len(inputs))
	for id := range inputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
