package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
)

func queueNativeMessages(directory string, request map[string]any, services []ExecutionServices) (map[string]any, error) {
	identity, _ := request["identity"].(string)
	if identity == "" || request["engine"] == nil {
		return request, nil
	}
	queued := []protocol.TaskMessage{}
	for begin := true; ; begin = false {
		message, err := services[0].ClaimMessage(directory, identity, begin)
		if err != nil {
			return nil, err
		}
		if message == nil {
			break
		}
		queued = append(queued, *message)
	}
	if len(queued) == 0 {
		return request, nil
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-messages.json"), queued); err != nil {
		return nil, err
	}
	return requestWithNativeMessages(directory, request)
}
func nativeMessages(directory string, request any) ([]protocol.TaskMessage, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "native-messages.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []protocol.TaskMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	ref := getOf(getOf(request, "engine"), "workerMaterial")
	for _, message := range messages {
		if message.ActionID != textOf(getOf(ref, "actionId")) || message.AttemptID != textOf(getOf(ref, "attemptId")) || message.Identity != textOf(getOf(request, "identity")) || message.Status != "SENDING" {
			return nil, fmt.Errorf("native supplementary text differs from original issued action/identity")
		}
	}
	return messages, nil
}
func requestWithNativeMessages(directory string, request any) (map[string]any, error) {
	messages, err := nativeMessages(directory, request)
	if err != nil {
		return nil, err
	}
	copy := map[string]any{}
	for key, value := range objectOrEmpty(request) {
		copy[key] = value
	}
	if len(messages) == 0 {
		return copy, nil
	}
	prompt, _ := copy["prompt"].(string)
	for _, message := range messages {
		prompt += "\n\n[Supplementary user message]\n" + message.Text
	}
	copy["prompt"] = prompt
	return copy, nil
}
func settleQueuedMessages(directory string, request any, turnErr error, services []ExecutionServices) error {
	messages, err := nativeMessages(directory, request)
	if err != nil {
		return err
	}
	for _, message := range messages {
		message.Status = "ACCEPTED"
		if turnErr != nil {
			message.Status = "FAILED"
			message.Reason = "native invocation ended without a delivery acknowledgement; delivery outcome unknown; not automatically resent: " + turnErr.Error()
		}
		if err := services[0].ReceiptMessage(directory, message); err != nil {
			return err
		}
	}
	return nil
}
