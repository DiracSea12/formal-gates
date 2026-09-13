package facade

import (
	"fmt"
	"strings"

	"formal-gates/internal/engine/protocol"
)

type QABatchReceipt struct {
	Receipts []protocol.TaskOperationReceipt `json:"receipts"`
	Pending  protocol.QAPending              `json:"pending"`
}

// RunTaskOperation is the common CLI/native entry. qa.run is a consumption
// convenience: every side effect still has its original individual claim and
// receipt. It never writes a PASS or creates another worker.
func (f *Facade) RunTaskOperation(q protocol.TaskOperationRequest, resume bool) (any, error) {
	if q.Command != nil && q.Kind != "qa.target" {
		return nil, fmt.Errorf("SCOPE_MISMATCH: command belongs only to qa.target")
	}
	if q.Kind == "qa.checkpoint" {
		return f.checkpointQA(q)
	}
	if q.Kind == "qa.run" {
		if resume {
			snapshot, err := f.engine.Load()
			if err != nil {
				return nil, err
			}
			q.ExpectedRevision = snapshot.Revision
		}
		return f.ConsumeQA(q)
	}
	return f.ExecuteTaskOperation(q, resume)
}

func (f *Facade) ConsumeQA(q protocol.TaskOperationRequest) (QABatchReceipt, error) {
	result := QABatchReceipt{}
	if q.Prompt != "" || q.RequestID == "" {
		return result, fmt.Errorf("qa.run requires requestId and empty prompt")
	}
	initial, err := f.engine.Load()
	if err != nil {
		return result, err
	}
	if q.RunID != initial.State.RunID || q.ExpectedRevision != initial.Revision {
		return result, fmt.Errorf("STALE_FRESHNESS: batch request binding or revision differs")
	}
	initialPlan, err := protocol.PendingQA(initial.State, q.Binding, q.CaseID)
	if err != nil {
		return result, err
	}
	type completed struct {
		id, caseID string
		receipt    protocol.TaskOperationReceipt
		err        error
	}
	done := make(chan completed, len(initialPlan.Ready)+len(initialPlan.Running)+1)
	active := map[string]bool{}
	paused := map[string]string{}
	for {
		snapshot, err := f.engine.Load()
		if err != nil {
			return result, err
		}
		pending, err := protocol.PendingQA(snapshot.State, q.Binding, q.CaseID)
		if err != nil {
			return result, err
		}
		result.Pending = pending
		start := func(request protocol.TaskOperationRequest, resume bool) {
			id := protocol.TaskOperationID(request)
			if active[id] || paused[request.CaseID] != "" {
				return
			}
			request.ExpectedRevision = snapshot.Revision
			// Recovery must retain the original request, including its old revision.
			if resume {
				request = snapshot.State.RegularExecutions[q.Binding.ActionID].Operations[id].Request
			}
			active[id] = true
			go func() { r, e := f.ExecuteTaskOperation(request, resume); done <- completed{id, request.CaseID, r, e} }()
		}
		for _, request := range pending.Running {
			start(request, true)
		}
		for _, request := range pending.Ready {
			start(request, false)
		}
		if len(active) == 0 {
			for id, reason := range paused {
				result.Pending.Blocked[id] = reason
			}
			return result, nil
		}
		finished := <-done
		delete(active, finished.id)
		if finished.err != nil {
			// Admission refusals and missing recovery observations are handed back as
			// pending relationships, not fabricated case failures or retry permission.
			paused[finished.caseID] = finished.err.Error()
			if strings.Contains(finished.err.Error(), "STALE_FRESHNESS") && len(active) == 0 {
				// The writer's CAS may have lost to another action. Recompute once from
				// current state; further queueing is represented by that state's claims.
				latest, e := f.engine.Load()
				if e == nil && latest.Revision != snapshot.Revision {
					delete(paused, finished.caseID)
				}
			}
			continue
		}
		result.Receipts = append(result.Receipts, finished.receipt)
		paused = map[string]string{}
	}
}
