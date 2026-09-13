package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
)

func nativeModelPrompt(prompt string) string {
	return "Answer only the supplied prompt. Do not use tools, files, external context or delegate. Return one JSON object with an output string containing your answer.\n\n" + prompt
}
func nativeModelSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"output"}, "properties": map[string]any{"output": map[string]any{"type": "string"}}}
}
func completeStreamModel(op protocol.TaskOperation, job, stream string, r protocol.TaskOperationReceipt, executionErr error) protocol.TaskOperationReceipt {
	if executionErr != nil {
		r.Status = "FAILED"
		r.Message = executionErr.Error()
		return streamModelSource(op, job, stream, r)
	}
	observed, err := observeStreamModel(op, job, stream, r)
	if err != nil {
		r.Status = "FAILED"
		r.Message = err.Error()
		return r
	}
	return observed
}
func observeStreamModel(op protocol.TaskOperation, job, stream string, r protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return r, err
	}
	var process struct {
		ActionID string `json:"actionId"`
		Identity string `json:"identity"`
		Stopped  bool   `json:"stopped"`
		ExitCode *int   `json:"exitCode"`
	}
	if json.Unmarshal(data, &process) != nil || process.ActionID != op.ID || process.Identity == "" || !process.Stopped || process.ExitCode == nil || *process.ExitCode != 0 {
		return r, fmt.Errorf("UNKNOWN: no successful native process receipt for %s", op.ID)
	}
	data, err = os.ReadFile(filepath.Join(job, "answer.json"))
	if err != nil {
		return r, err
	}
	var answer struct {
		Output *string `json:"output"`
	}
	if json.Unmarshal(data, &answer) != nil || answer.Output == nil {
		return r, fmt.Errorf("UNKNOWN: native model answer missing")
	}
	r.Status = "COMPLETED"
	r.Identity = process.Identity
	r.Output = *answer.Output
	r.ExitCode = *process.ExitCode
	return streamModelSource(op, job, stream, r), nil
}

// A missing billing source must not turn a completed model answer into an
// uncollectable operation. Preserve the diagnostic through the existing receipt.
func streamModelSource(op protocol.TaskOperation, job, stream string, r protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	if r.Identity == "" {
		raw, _ := os.ReadFile(filepath.Join(job, "process.json"))
		var process struct {
			ActionID string `json:"actionId"`
			Identity string `json:"identity"`
		}
		if json.Unmarshal(raw, &process) == nil && process.ActionID == op.ID {
			r.Identity = process.Identity
		}
	}
	path, err := appendStreamTranscript(job, r.Identity, stream, op.Request.Binding.Context)
	if err == nil {
		_, err = cost.ParseTranscript(op.Request.Binding.Provider, path)
	}
	if err != nil {
		r.TranscriptPath = ""
		if r.Message != "" {
			r.Message += "; "
		}
		r.Message += "Usage source unavailable: " + err.Error()
	} else {
		r.TranscriptPath = path
	}
	return r
}
