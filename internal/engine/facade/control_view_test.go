package facade

import (
	"errors"
	"reflect"
	"testing"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/protocol"
)

func TestChildControlReadWindowUsesActualMasterAndChild(t *testing.T) {
	master := splitMaster(t)
	master.drive()
	child, err := Open(master.f.root, master.run.SliceChildren["lane-core"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Drive(); err != nil {
		t.Fatal(err)
	}
	if requested, err := child.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("child route Ask was not issued", err)
	}
	unlock, err := child.acquireControlReadWindow()
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*Facade{master.f, child} {
		release, err := coordination.AcquirePath(owner.promotionLockPath(), "control read assertion")
		if err == nil {
			release()
			unlock()
			t.Fatal("actual master or child window was not held")
		}
		var held *coordination.LockHeldError
		if !errors.As(err, &held) {
			unlock()
			t.Fatal(err)
		}
	}
	unlock()
	current, err := child.ControlRuns()
	if err != nil || len(current) != 1 || current[0].MasterRunID != master.run.RunID {
		t.Fatal("direct child projection lost its actual master", err)
	}
	input := ControlInput{Ask: askPosition(t, current[0], protocol.ControlSelectRoute), Choice: "full"}
	before := installationFiles(t, child.stateDir)
	local, err := child.PrepareControl(input)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := child.PrepareCurrentControl(input)
	if err != nil || !reflect.DeepEqual(local, semantic) || !reflect.DeepEqual(before, installationFiles(t, child.stateDir)) {
		t.Fatal("child preparation changed local semantics or wrote state", err)
	}
}
