package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/runtime"
)

// DeveloperRecovery is an adapter observation attached to the already claimed
// execution. It does not create another lifecycle or grant another attempt.
// NativeResponse is retained verbatim: a host-supplied reason never proves that
// the original thread is irrecoverable.
type DeveloperRecovery struct {
	Claim          ExecutionClaim  `json:"claim"`
	Identity       string          `json:"identity"`
	StoppedAction  string          `json:"stoppedAction"`
	ProcessID      int             `json:"processId"`
	WorkspaceView  string          `json:"workspaceView"`
	NativeMethod   string          `json:"nativeMethod"`
	NativeResponse json.RawMessage `json:"nativeResponse"`
	TransportError string          `json:"transportError,omitempty"`
}

func (r DeveloperRecovery) Outcome() string {
	if r.Identity == "" || r.TransportError != "" {
		return "UNKNOWN"
	}
	observe := recoveryObservers[r.Claim.Provider]
	if observe == nil {
		return "UNKNOWN"
	}
	return observe(r)
}

func latestDeveloper(s *State, task runtime.TaskKey) (RegularExecution, bool) {
	var latest RegularExecution
	found := false
	for _, x := range s.RegularExecutions {
		if x.Claim.Task == task && x.Claimed && x.Identity != "" && (!found || x.Number > latest.Number) {
			latest, found = x, true
		}
	}
	return latest, found
}

func acceptDeveloperRecovery(s *State, receipt DeveloperRecovery) error {
	x, ok := s.RegularExecutions[receipt.Claim.ActionID]
	if !ok || x.Claim.Task.Step != "development-worker" || !x.Claimed || x.Finished || x.Recovery != nil || !reflect.DeepEqual(receipt.Claim, x.Claim) || receipt.Identity != x.Identity {
		return fmt.Errorf("DEVELOPMENT_CONTINUITY: recovery needs the exact live claimed developer")
	}
	if !currentPreparationClaim(s.Regular, x.Claim) || s.Regular.ChangePending || s.Regular.Ask != nil || s.Regular.Terminal != "" {
		return fmt.Errorf("EVENT_NOT_CURRENT: developer recovery binding expired")
	}
	previous := x.Claim.ActionID
	if x.PreviousAction != "" && !x.ResumeClaimed {
		previous = x.PreviousAction
	}
	old, ok := s.RegularExecutions[previous]
	for ok && previous != receipt.StoppedAction && old.Recovery != nil && old.Recovery.Outcome() != "RESUMABLE" && old.PreviousAction != "" {
		previous = old.PreviousAction
		old, ok = s.RegularExecutions[previous]
	}
	if !ok || receipt.StoppedAction != previous || old.Identity != receipt.Identity || old.Source == nil || old.Source.Status == "RUNNING" || receipt.ProcessID <= 0 || receipt.WorkspaceView == "" || s.Regular.Preparation.Operation != nil {
		return fmt.Errorf("DEVELOPMENT_CONTINUITY: previous execution or Git side effects are not reconciled")
	}
	x.Recovery = &receipt
	s.RegularExecutions[x.Claim.ActionID] = x
	if receipt.Outcome() == "UNRECOVERABLE" {
		resumeID := "hact:" + x.Claim.ActionID + ":resume"
		if intent, ok := s.PendingHostActions[resumeID]; ok {
			failed := HostActionReceipt{ActionID: resumeID, Operation: intent.Operation, Step: x.Claim.Task.Step, Provider: x.Claim.Provider, Correlation: receipt.Identity, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusFailed, FailureClass: authoring.FailureBusinessReject}
			failed.Digest = digestOfCanonical(failed)
			s.HostActionFailures[resumeID] = append(s.HostActionFailures[resumeID], failed)
			delete(s.PendingHostActions, resumeID)
		}
		id := "hact:" + x.Claim.ActionID + ":replace"
		s.PendingHostActions[id] = HostActionIntent{ActionID: id, Operation: HostActionExecuteAdapterOperation, Step: x.Claim.Task.Step, Adapter: &AdapterHostAction{Operation: "developer.replace", Schema: "schema.developer.recovery", Params: map[string]any{"workerActionId": x.Claim.ActionID, "identity": receipt.Identity}}, PayloadDigest: digestOfCanonical(receipt), Correlation: receipt.Identity}
	}
	return nil
}

func claimDeveloperReplacement(s *State, x *RegularExecution, c ExecutionClaim) error {
	intent, ok := s.PendingHostActions["hact:"+c.ActionID+":replace"]
	if !ok || !x.Claimed || x.Finished || x.ReplacementClaimed || x.Recovery == nil || x.Recovery.Outcome() != "UNRECOVERABLE" || c.Identity != x.Recovery.Identity || intent.PayloadDigest != digestOfCanonical(*x.Recovery) || s.Regular.Preparation.Operation != nil {
		return fmt.Errorf("DEVELOPMENT_CONTINUITY: replacement needs unconsumed native irrecoverability evidence and reconciled side effects")
	}
	x.ReplacementClaimed = true
	x.Identity = ""
	return nil
}
