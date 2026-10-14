package protocol

import (
	"fmt"
	"strings"
)

// admitDevelopmentHandoff enters the existing recovery decision path without
// recording a failure. The run keeps its host, work binding and logical task.
func (e *Engine) admitDevelopmentHandoff(state *State, request *DevelopmentHandoffRequest, nextRevision uint64) (string, error) {
	action, reason := request.ActionID, request.Reason
	pending, active := state.PendingActions[action]
	spawn, started := state.SpawnReceipts[action]
	if _, issued := state.WorkerInput(action, e.cfg.Definition); !issued || !active || !started || !inflightWitnessReceipt(spawn.Status) || spawn.agentIdentity() == "" || strings.TrimSpace(reason) == "" || state.validationRole(pending.Task, e.cfg.Definition) != "engine.development.batch.v5" {
		return "", fmt.Errorf("HANDOFF_BINDING: current unfinished developer and actual user reason required")
	}
	if recovery, exists := state.pendingAttemptRecovery(action, pending.AttemptID); exists {
		return recovery.RequestID, nil
	}
	record := RecoveryRecord{Handoff: true, ActionID: action, Task: pending.Task, AttemptID: pending.AttemptID, Action: RecoveryAsk, Detail: reason, Revision: nextRevision}
	if !state.registerRecoveryAsk(&record, nextRevision) {
		return "", fmt.Errorf("HANDOFF_BINDING: current recovery must settle first")
	}
	ask := state.PendingAsks[record.RequestID]
	ask.Options = []AskOption{{ID: "fresh", Label: "Transfer unfinished work to a new developer on the same host"}, {ID: "abort", Label: "Abort the run"}}
	state.PendingAsks[record.RequestID] = ask
	state.RecoveryRecords = append(state.RecoveryRecords, record)
	return record.RequestID, nil
}
