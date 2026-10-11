package protocol

import (
	"fmt"
	"strings"
)

// RequestDevelopmentHandoff enters the existing recovery decision path without
// recording a failure. The run keeps its host, work binding and logical task.
func (e *Engine) RequestDevelopmentHandoff(action, reason string, revision uint64, fingerprint string) (string, error) {
	current, state, err := e.load()
	if err != nil {
		return "", err
	}
	if state == nil || current != revision {
		return "", fmt.Errorf("HANDOFF_BINDING: current run revision required")
	}
	pending, active := state.PendingActions[action]
	spawn, started := state.SpawnReceipts[action]
	if _, issued := state.WorkerInput(action, e.cfg.Definition); !issued || !active || !started || spawn.Status != SpawnStatusSpawned || spawn.agentIdentity() == "" || strings.TrimSpace(reason) == "" || state.validationRole(pending.Task, e.cfg.Definition) != "engine.development.batch.v2" {
		return "", fmt.Errorf("HANDOFF_BINDING: current unfinished developer and actual user reason required")
	}
	if recovery, exists := state.pendingAttemptRecovery(action, pending.AttemptID); exists {
		return recovery.RequestID, nil
	}
	record := RecoveryRecord{Handoff: true, ActionID: action, Task: pending.Task, AttemptID: pending.AttemptID, Action: RecoveryAsk, Detail: reason, Revision: current + 1}
	if !state.registerRecoveryAsk(&record, current+1) {
		return "", fmt.Errorf("HANDOFF_BINDING: current recovery must settle first")
	}
	ask := state.PendingAsks[record.RequestID]
	ask.Options = []AskOption{{ID: "fresh", Label: "Transfer unfinished work to a new developer on the same host"}, {ID: "abort", Label: "Abort the run"}}
	state.PendingAsks[record.RequestID] = ask
	state.RecoveryRecords = append(state.RecoveryRecords, record)
	_, err = e.commit(state, current, fingerprint)
	return record.RequestID, err
}
