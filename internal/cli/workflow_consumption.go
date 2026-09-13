package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func (streams IO) openWorkflow(root, runID string) (*facade.Facade, bool, error) {
	if streams.workflowOpen != nil {
		return streams.workflowOpen(root, runID)
	}
	return openEngineIfPresent(root, runID)
}

// The response carries the existing progress projection and only the exact
// control payloads needed at the current boundary. Full history is in show.
type workflowConsumption struct {
	Progress   facade.Progress      `json:"progress"`
	Next       decision.NextResult  `json:"next"`
	Ask        *protocol.RegularAsk `json:"ask,omitempty"`
	Acceptance *protocol.Acceptance `json:"acceptance,omitempty"`
	Boundary   string               `json:"boundary"`
	Blocked    map[string]string    `json:"blocked,omitempty"`
	External   []string             `json:"external,omitempty"`
}

// Only foreground control submissions consume subsequent work. Worker receipts
// can arrive while that consumer holds the executor lease.
func workflowControlEvent(event protocol.Event) bool {
	if event.Regular == nil {
		return false
	}
	switch event.Regular.Operation {
	case "REQUEST", "DECIDE", "ADJUDICATE", "WAVE_ADJUDICATE", "CORRECT", "DEVELOPER_RECOVERY":
		return true
	}
	return false
}

func consumeWorkflow(streams IO, f *facade.Facade, afterRevision uint64) (workflowConsumption, error) {
	parent := streams.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := f.DriveContinuous(ctx)
	if err != nil {
		// Preserve the latest readable projection even if interrupted before
		// the first Drive, or after an accepted control input.
		if run, showErr := f.Show(); showErr == nil {
			result.Run = run
		}
		if result.Boundary == "" {
			result.Boundary = "BLOCKED"
		}
	}
	progress, progressErr := f.ProgressSince(afterRevision)
	response := workflowConsumption{Progress: progress, Next: result.Run.Next,
		Boundary: result.Boundary, Blocked: result.Blocked, External: result.External}
	if result.Run.Regular != nil {
		response.Ask = result.Run.Regular.Ask
	}
	// Keep the execution error authoritative, especially cancellation's exit code.
	if err == nil {
		err = progressErr
	}
	return response, err
}

// An accepted event and subsequent consumption share one JSON response. Handle
// execution errors here so Run's lock-error formatter cannot append a second.
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
