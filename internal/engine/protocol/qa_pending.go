package protocol

import (
	"fmt"
	"sort"
)

// QAPending is derived from the existing operation and checkpoint records. It
// does not own claims, attempts, receipts, or execution state.
type QAPending struct {
	Ready    []TaskOperationRequest `json:"ready"`
	Running  []TaskOperationRequest `json:"running"`
	Input    []string               `json:"input"`
	Judgment []string               `json:"judgment"`
	Blocked  map[string]string      `json:"blocked"`
}

func PendingQA(s *State, binding ExecutionClaim, caseID string) (QAPending, error) {
	p := QAPending{Blocked: map[string]string{}}
	probe := TaskOperationRequest{RunID: s.RunID, Binding: binding, Kind: "qa.prepare"}
	if err := CurrentTaskOperationBinding(s, probe); err != nil {
		return p, err
	}
	w := currentWave(s.Regular)
	if w == nil {
		return p, fmt.Errorf("EVENT_NOT_CURRENT: no current QA wave")
	}
	var ids []string
	if binding.Task.Step == "repair-worker" {
		repair := currentRepair(s.Regular)
		if repair == nil {
			return p, fmt.Errorf("EVENT_NOT_CURRENT: no repair plan")
		}
		ids = append(ids, repair.AffectedCases...)
	} else {
		for _, target := range w.Targets {
			if target.Task == binding.Task {
				ids = append(ids, target.Expected...)
			}
		}
	}
	if caseID != "" {
		found := false
		for _, id := range ids {
			if id == caseID {
				found = true
			}
		}
		if !found {
			return p, fmt.Errorf("SCOPE_MISMATCH: case %s is outside issued scope", caseID)
		}
		ids = []string{caseID}
	}
	if len(ids) == 0 {
		return p, fmt.Errorf("SCOPE_MISMATCH: no issued executable QA cases")
	}
	sort.Strings(ids)
	x := s.RegularExecutions[binding.ActionID]
	checkpoint := w.Checkpoints[targetName(binding.Task.Step)]
	done := map[string]string{}
	for _, row := range checkpoint.Cases {
		done[row.CaseID] = row.Status
	}
	target := qaVerificationTarget(s.Regular, binding)
	if target != nil {
		for id, row := range target.Inherited {
			done[id] = row.Status
		}
	}
	for _, id := range ids {
		if done[id] != "" {
			continue
		}
		q := probe
		q.CaseID = id
		c, err := taskCase(s, q)
		if err != nil {
			return p, err
		}
		prepare := findTaskOperation(x, "qa.prepare", id)
		invalidated := prepare != nil && qaInvalidatedSince(w, targetName(binding.Task.Step), id, prepare.CheckpointDigest)
		if prepare != nil && invalidated && prepare.CheckpointDigest != checkpoint.Digest {
			prepare = nil
		}
		if prepare == nil {
			blocked := false
			for _, dep := range c.Dependencies {
				if done[dep] != "PASS" {
					p.Blocked[id] = fmt.Sprintf("dependency %s requires accepted PASS (current %s)", dep, done[dep])
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
		}
		previous := uint64(0)
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			op := findTaskOperation(x, kind, id)
			if (kind == "qa.prepare" && prepare == nil) || (kind != "qa.prepare" && (op == nil || op.ClaimedRevision < previous)) {
				op = nil
			}
			if op == nil {
				if kind == "qa.target" && c.Command == nil {
					p.Input = append(p.Input, id)
					break
				}
				q.Kind = kind
				q.RequestID = "qa:" + id + ":" + kind + ":" + checkpoint.Digest
				p.Ready = append(p.Ready, q)
				break
			}
			if op.Receipt == nil {
				p.Running = append(p.Running, op.Request)
				break
			}
			if op.Receipt.Status != "COMPLETED" {
				p.Blocked[id] = fmt.Sprintf("operation %s failed: %s", op.ID, op.Receipt.Message)
				break
			}
			previous = op.ClaimedRevision
			if kind == "qa.collect" {
				p.Judgment = append(p.Judgment, id)
			}
		}
	}
	return p, nil
}

// Later incremental checkpoints may omit an unconsumed invalidation. Its
// authorization remains in the existing history until this case's new prepare
// records a checkpoint at or after it. This never authorizes a second rerun.
func qaInvalidatedSince(w *VerificationWave, target, caseID, after string) bool {
	checkpoints := append([]VerificationResult(nil), w.CheckpointHistory[target]...)
	checkpoints = append(checkpoints, w.Checkpoints[target])
	for i := len(checkpoints) - 1; i >= 0; i-- {
		checkpoint := checkpoints[i]
		if checkpoint.Digest == after {
			return false
		}
		for _, change := range checkpoint.Invalidated {
			if change.CaseID == caseID {
				return true
			}
		}
	}
	return false
}
