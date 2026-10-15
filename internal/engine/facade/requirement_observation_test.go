package facade

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

func TestResetRequirementConfirmationRechecksLiveFileBeforeRegistration(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			d := nativeMasterDriver(t)
			intake, err := captureIntake(d.f.root, d.run.IntakeConfirmationReceipt)
			if err != nil {
				t.Fatal(err)
			}
			if err := saveIntake(d.f.stateDir, intake); err != nil {
				t.Fatal(err)
			}
			request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlReset})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(t, d.f, request)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReset), Choice: "proceed", Reason: "Reset unfinished work while retaining the document"})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(t, d.f, decision)
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
				if saves == 1 {
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
			_, _, observedErr := submitControlWithHost(t, d.f, confirm)
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if saves != 1 || registrationBase.State == nil {
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

func TestControlMaterialObservationRechecksSelectedSolution(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"reset-request", "reset-decision", "requirement-package", "requirement-confirmation"} {
		for _, changed := range []bool{false, true} {
			t.Run(boundary+"/"+map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
				root := t.TempDir()
				snapshotRepoCommits(t, root)
				path := filepath.Join(root, "design.md")
				if err := os.WriteFile(path, []byte("# Selected solution\nUse the approved command.\n"), 0600); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"add", "design.md"}, {"commit", "-m", "Selected solution"}} {
					if _, err := (vcs.ExecRunner{}).Run(root, "git", args...); err != nil {
						t.Fatal(err)
					}
				}
				base, err := vcs.New(root, nil).Resolve()
				if err != nil {
					t.Fatal(err)
				}
				d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, "design.md")
				var candidate ControlCandidate
				if boundary == "reset-request" || boundary == "reset-decision" {
					candidate, err = d.f.PrepareControl(ControlInput{Request: protocol.ControlReset})
					if err != nil {
						t.Fatal(err)
					}
					if boundary == "reset-decision" {
						_, d.run, err = submitControlWithHost(t, d.f, candidate)
						if err != nil {
							t.Fatal(err)
						}
						candidate, err = d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReset), Choice: "proceed", Reason: "Retain the selected solution while resetting workflow facts"})
					}
				} else {
					original, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, append(original, '\n'), 0600); err != nil {
						t.Fatal(err)
					}
					work := make([]WorkChoice, len(d.run.RequirementWork))
					for i := range work {
						work[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "Solution formatting preserves this original duty"}
					}
					candidate, err = d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{"selected solution formatting"}, CarryReasons: []string{"original command behavior remains unchanged"}}}})
					if err != nil {
						t.Fatal(err)
					}
					if boundary == "requirement-confirmation" {
						_, d.run, err = submitControlWithHost(t, d.f, candidate)
						if err != nil {
							t.Fatal(err)
						}
						candidate, err = d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm the complete selected materials"})
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				saves := 0
				store, err := persistence.NewStore(d.f.stateDir, persistence.Config{PackageDigest: d.f.envelope.PackageDigest, DefinitionSource: d.f.envelope.DefinitionSource, InstalledTargetIdentity: d.f.envelope.InstalledTargetIdentity, AcquireAdmission: func() (func(), error) {
					release, err := d.f.admission.acquire()
					if err != nil {
						return nil, err
					}
					saves++
					if saves == 1 && changed {
						if err := os.WriteFile(path, append(append([]byte(nil), original...), []byte("\nChanged before material acceptance.\n")...), 0600); err != nil {
							release()
							return nil, err
						}
					}
					return release, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				s := before.State
				engine, err := protocol.New(store, protocol.Config{Settings: s.Settings, RequirementGuarantee: s.RequirementGuarantee, VCSName: s.VCSName, NativeHandle: s.NativeHandle, NativeRoot: s.NativeRoot, Definition: d.f.definition, Registry: definition.Registry(), Capacity: d.f.capacity, CostSource: CostSourceBridge(root, d.f.runID), ClaimDispatch: dispatchClaimBridge(root, d.f.runID, d.f.admission), WorkerExecution: workerExecutionBridge(root, d.f.runID)})
				if err != nil {
					t.Fatal(err)
				}
				d.f.store, d.f.engine = store, engine
				saved, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				accepted, _, observedErr := submitControlWithHost(t, d.f, candidate)
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if saves != 1 {
					t.Fatalf("material control did not reach its single save: saves=%d err=%v", saves, observedErr)
				}
				if changed {
					var drift *persistence.FingerprintChangedError
					if !errors.As(observedErr, &drift) || drift.Committed || !reflect.DeepEqual(before, after) {
						t.Fatalf("changed selected solution wrote control effects: err=%v before=%d after=%d", observedErr, before.Revision, after.Revision)
					}
					return
				}
				if observedErr != nil || after.Revision != before.Revision+1 {
					t.Fatalf("unchanged selected solution was not accepted once: %v", observedErr)
				}
				switch boundary {
				case "reset-request":
					preview := after.State.PendingAsks[accepted.RequestID].Reset
					if preview == nil || preview.Intake.SolutionRevision != requirementContentRevision(original) {
						t.Fatal("reset request lost its original selected solution")
					}
				case "reset-decision":
					decision := after.State.Decisions[string(candidate.Event.Decide.Request)].Reset
					if decision == nil || decision.PreviewDigest != candidate.Reset.Digest || !decision.Applied {
						t.Fatal("reset decision lost its original material preview")
					}
				case "requirement-package":
					pkg := after.State.PendingAsks[accepted.RequestID].Package
					if pkg == nil || !reflect.DeepEqual(pkg.Intake, &candidate.Intake.IntakeCandidate) || !reflect.DeepEqual(after.State.Validation.Document, before.State.Validation.Document) {
						t.Fatal("requirement package lost its original material or registered it before confirmation")
					}
				case "requirement-confirmation":
					if !reflect.DeepEqual(after.State.CurrentIntakeConfirmation(), &candidate.Intake.IntakeCandidate) || !reflect.DeepEqual(after.State.Validation.Document, *candidate.Intake.RequirementDocument) {
						t.Fatal("requirement confirmation lost its exact full material")
					}
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				reopened, err := Open(root, d.run.RunID)
				if err != nil {
					t.Fatal(err)
				}
				var originalCandidate ControlCandidate
				if err := json.Unmarshal(saved, &originalCandidate); err != nil {
					t.Fatal(err)
				}
				for _, current := range []*Facade{d.f, reopened} {
					replay, _, err := submitControlWithHost(t, current, originalCandidate)
					if err != nil || !reflect.DeepEqual(accepted, replay) {
						t.Fatal("accepted original control replay read changed material or lost acceptance", err)
					}
					repeated, err := current.engine.Load()
					if err != nil || !reflect.DeepEqual(after, repeated) {
						t.Fatal("accepted original control replay changed durable effects", err)
					}
				}
			})
		}
	}
}
