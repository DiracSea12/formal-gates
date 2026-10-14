package facade

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func TestResetRequirementObservationRechecksLiveFileBeforeRegistration(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			d := nativeMasterDriver(t)
			request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlReset})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(request)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReset), Choice: "proceed", Reason: "Reset unfinished work while retaining the document"})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(decision)
			if err != nil {
				t.Fatal(err)
			}
			d.run, err = d.f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm the complete preserved requirement"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(d.f.root, confirm.Intake.IntakeCandidate.RequirementSource)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			saves := 0
			var registrationBase protocol.Snapshot
			store, err := persistence.NewStore(d.f.stateDir, persistence.Config{PackageDigest: d.f.envelope.PackageDigest, DefinitionSource: d.f.envelope.DefinitionSource, InstalledTargetIdentity: d.f.envelope.InstalledTargetIdentity, AcquireAdmission: func() (func(), error) {
				release, err := d.f.admission.acquire()
				if err != nil {
					return nil, err
				}
				saves++
				if saves == 2 {
					registrationBase, err = d.f.engine.Load()
					if err != nil {
						release()
						return nil, err
					}
					if changed {
						err = os.WriteFile(path, append(append([]byte(nil), original...), []byte("\nChanged after reset registration observation.\n")...), 0600)
						if err != nil {
							release()
							return nil, err
						}
					}
				}
				return release, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			s := before.State
			engine, err := protocol.New(store, protocol.Config{Settings: s.Settings, RequirementGuarantee: s.RequirementGuarantee, VCSName: s.VCSName, NativeHandle: s.NativeHandle, NativeRoot: s.NativeRoot, Definition: d.f.definition, Registry: definition.Registry(), Capacity: d.f.capacity, CostSource: CostSourceBridge(d.f.root, d.f.runID), ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission), WorkerExecution: workerExecutionBridge(d.f.root, d.f.runID)})
			if err != nil {
				t.Fatal(err)
			}
			d.f.store, d.f.engine = store, engine
			// The original Submit accepts the typed confirmation. Its live reset file
			// registration is a separate transaction after that confirmation commits.
			_, _, observedErr := d.f.Submit(confirm.Event)
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if saves != 2 || registrationBase.State == nil {
				t.Fatalf("original live registration was not reached: saves=%d err=%v", saves, observedErr)
			}
			if changed {
				var drift *persistence.FingerprintChangedError
				if !errors.As(observedErr, &drift) || drift.Committed || after.Revision != registrationBase.Revision || !reflect.DeepEqual(after.State, registrationBase.State) {
					t.Fatalf("reset registration saved a changed live document: err=%v base=%d after=%d", observedErr, registrationBase.Revision, after.Revision)
				}
				return
			}
			if observedErr != nil || after.Revision != registrationBase.Revision+1 || after.State.Validation.Document.Content != string(original) {
				t.Fatalf("unchanged reset registration did not preserve exact document: %v", observedErr)
			}
		})
	}
}
