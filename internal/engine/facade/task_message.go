package facade

import (
	"formal-gates/internal/engine/protocol"
	"sort"
)

// The action owner claims delivery before invoking the host. An interrupted
// SENDING message is never silently sent again: the host may have received it.
func (f *Facade) pendingTaskMessage(action, identity string) (*protocol.TaskMessage, error) {
	snapshot, err := f.engine.Load()
	if err != nil {
		return nil, err
	}
	executions := snapshot.State.RegularExecutions
	x := executions[action]
	for {
		ids := []string{}
		for id, m := range x.Messages {
			if m.Status == "PENDING" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(ids) > 0 {
			m := x.Messages[ids[0]]
			m.Status, m.Identity = "SENDING", identity
			if err := f.recordTaskMessage(m); err != nil {
				return nil, err
			}
			return &m, nil
		}
		// CONTINUE reuses the native identity through the existing task lineage.
		// Consume the original record in place, so a successor never copies a
		// stale PENDING entry or loses the original action/attempt provenance.
		previous, ok := executions[x.PreviousAction]
		if !ok || previous.Claim.Task != x.Claim.Task || previous.Identity != identity {
			return nil, nil
		}
		x = previous
	}
}

func (f *Facade) recordTaskMessage(m protocol.TaskMessage) error {
	_, _, err := f.submitAdapterRegular("message-"+m.ActionID+"-"+m.ID+"-"+m.Status, protocol.RegularEvent{Operation: "MESSAGE_RECEIPT", Message: &m})
	return err
}
