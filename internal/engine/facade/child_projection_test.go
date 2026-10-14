package facade

import (
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestChildProjectionDistinguishesPlannedAndEstablishedRuns(t *testing.T) {
	master := splitMaster(t)
	state := driverState(t, master)
	digest, err := master.f.vcs.TreeDigest(state.AcceptedNativeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := master.f.engine.PrepareChildren(digest, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	before, err := master.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	run, err := master.f.Show()
	if err != nil || len(run.RequirementChildren) != 0 {
		t.Fatal("planned child requires an uncreated intake", err)
	}
	after, err := master.f.engine.Load()
	if err != nil || after.Revision != before.Revision {
		t.Fatal("child projection wrote preparation state", err)
	}
	for _, plan := range before.State.ChildCreation.Children {
		established, err := runDirectoryEstablished(filepath.Join(master.f.root, EngineNamespace, plan.RunID))
		if err != nil || established {
			t.Fatal("read-only projection bootstrapped planned child", err)
		}
	}
	master.drive()
	current := driverState(t, master)
	if len(current.SliceChildren) != len(current.ChildCreation.Children) {
		t.Fatal("normal drive did not establish original planned children")
	}
	child := current.ChildCreation.Children[0]
	path := filepath.Join(master.f.root, EngineNamespace, child.RunID, "request.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := master.f.Show(); err == nil {
		t.Fatal("established child lost intake without a projection error")
	}
}

func TestAbortBeforeChildBootstrapKeepsOriginalCleanupAuthority(t *testing.T) {
	master := splitMaster(t)
	state := driverState(t, master)
	digest, err := master.f.vcs.TreeDigest(state.AcceptedNativeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := master.f.engine.PrepareChildren(digest, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	abortRun(t, master)
	master.reopen()
	master.drive()
	if master.run.Status != "ABORTED" || master.run.Termination == nil || !master.run.Termination.ResourcesCleaned() {
		t.Fatal("planned child blocked original public abort", master.run.Status)
	}
	for _, decision := range master.run.Termination.Decisions {
		if decision.Control == protocol.ControlAbort && decision.Choice == "abort" {
			return
		}
	}
	t.Fatal("cleanup lost original parent abort decision")
}
