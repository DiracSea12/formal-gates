package protocol

import "testing"

func TestTaskMessageDeliveryBindingAndReplay(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	x := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, x)
	m := TaskMessage{ID: "note-1", ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Text: "Keep the amber output."}
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE", Message: &m})
	snap, _ := e.Load()
	if err := acceptTaskMessage(snap.State, m, false); err == nil {
		t.Fatal("duplicate accepted")
	}
	m.Status, m.Identity = "SENDING", "native-"+x.Claim.ActionID
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE_RECEIPT", Message: &m})
	snap, _ = e.Load()
	if err := acceptTaskMessage(snap.State, m, true); err == nil {
		t.Fatal("interrupted sending automatically repeated")
	}
	m.Status = "ACCEPTED"
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE_RECEIPT", Message: &m})
	snap, _ = e.Load()
	if snap.State.RegularExecutions[x.Claim.ActionID].Finished {
		t.Fatal("delivery counted as execution")
	}
	if err := acceptTaskMessage(snap.State, m, true); err == nil {
		t.Fatal("ack replay accepted")
	}
	m.ID = "note-2"
	m.Status = ""
	m.Identity = ""
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE", Message: &m})
	m.Status, m.Identity = "SENDING", "wrong-native"
	snap, _ = e.Load()
	if err := acceptTaskMessage(snap.State, m, true); err == nil {
		t.Fatal("wrong identity accepted")
	}
	m.Identity = "native-" + x.Claim.ActionID
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE_RECEIPT", Message: &m})
	m.Status, m.Reason = "FAILED", "host rejected delivery"
	submitRegular(t, e, RegularEvent{Operation: "MESSAGE_RECEIPT", Message: &m})
}
