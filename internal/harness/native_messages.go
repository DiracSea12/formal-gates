package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
)

func queueNativeMessages(directory string, request map[string]any, services []ExecutionServices) (map[string]any, []protocol.TaskMessage, error) {
	identity, _ := request["identity"].(string)
	if identity == "" || request["engine"] == nil {
		return request, nil, nil
	}
	queued := []protocol.TaskMessage{}
	for begin := true; ; begin = false {
		message, err := services[0].ClaimMessage(directory, identity, begin)
		if err != nil {
			return nil, nil, err
		}
		if message == nil {
			break
		}
		queued = append(queued, *message)
	}
	if len(queued) == 0 {
		return request, nil, nil
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-messages.json"), queued); err != nil {
		return nil, nil, err
	}
	value, err := requestWithNativeMessages(directory, request)
	return value, queued, err
}
func requestWithNativeMessages(directory string, request any) (map[string]any, error) {
	original := objectOrEmpty(request)
	copy := map[string]any{}
	for key, value := range original {
		copy[key] = value
	}
	raw, err := os.ReadFile(filepath.Join(directory, "native-messages.json"))
	if os.IsNotExist(err) {
		return copy, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []protocol.TaskMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	prompt, _ := copy["prompt"].(string)
	ref := getOf(getOf(copy, "engine"), "workerMaterial")
	for _, message := range messages {
		if message.ActionID != textOf(getOf(ref, "actionId")) || message.AttemptID != textOf(getOf(ref, "attemptId")) || message.Identity != textOf(copy["identity"]) || message.Status != "SENDING" {
			return nil, fmt.Errorf("native supplementary text differs from original issued action/identity")
		}
		prompt += "\n\n[Supplementary user message]\n" + message.Text
	}
	copy["prompt"] = prompt
	return copy, nil
}
func settleQueuedMessages(directory string, messages []protocol.TaskMessage, turnErr error, services []ExecutionServices) error {
	for _, message := range messages {
		message.Status = "ACCEPTED"
		if turnErr != nil {
			message.Status = "FAILED"
			message.Reason = turnErr.Error()
		}
		if err := services[0].ReceiptMessage(directory, message); err != nil {
			return err
		}
	}
	return nil
}
