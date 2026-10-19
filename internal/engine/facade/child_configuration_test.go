package facade

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func TestChildCreationPreservesIdentityAcrossConfigurationEdits(t *testing.T) {
	for _, window := range []string{"unchanged", "before-plan", "after-plan", "between-children", "restored"} {
		t.Run(window, func(t *testing.T) {
			d := splitMaster(t)
			parent := driverState(t, d)
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			edit := func(enabled bool, model string) {
				t.Helper()
				_, err := settings.EditConfiguration(d.f.root, home, "project", parent.ConfigurationName(), nil, func(c *settings.Configuration) error {
					c.SetFlowNodeEnabled("repair", enabled)
					if model != "" {
						if c.Execution == nil {
							c.Execution = map[string]settings.ExecutionChoice{}
						}
						c.Execution["development"] = settings.ExecutionChoice{Provider: d.host(), Model: model}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if window == "before-plan" {
				edit(false, "")
			}
			digest, err := d.f.vcs.TreeDigest(parent.AcceptedNativeSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			if err := d.f.engine.PrepareChildren(digest, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			parent = driverState(t, d)
			creation := parent.ChildCreation
			bootstrapped := map[string]*protocol.State{}
			prepare := func(plan protocol.ChildPreparation) {
				t.Helper()
				child, err := d.f.prepareChild(parent, plan, OwnerBinding{})
				if err != nil {
					t.Fatal(err)
				}
				if err := d.f.finishChildBootstrap(child); err != nil {
					t.Fatal(err)
				}
				if _, err := child.Drive(); err != nil {
					t.Fatal(err)
				}
				snapshot, err := child.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.State.Phase != runtime.PhasePreparing || len(snapshot.State.PendingActions) != 0 || len(snapshot.State.Decisions) != 0 {
					t.Fatal("an unreleased child dispatched work or fabricated a decision")
				}
				bootstrapped[plan.RunID] = snapshot.State
			}
			if window == "between-children" {
				prepare(creation.Children[0])
			}
			if window == "after-plan" || window == "between-children" || window == "restored" {
				edit(false, "")
			}
			if window == "between-children" {
				prepare(creation.Children[1])
				if bootstrapped[creation.Children[0].RunID].FlowNodeEnabled("repair") == bootstrapped[creation.Children[1].RunID].FlowNodeEnabled("repair") {
					t.Fatal("siblings did not sample different library values")
				}
			}
			if window == "restored" {
				for _, plan := range creation.Children {
					prepare(plan)
				}
				edit(true, "")
			}
			enabled := window == "unchanged" || window == "restored"
			for attempt := 0; attempt < 2; attempt++ {
				d.reopen()
				d.drive()
				current := driverState(t, d)
				if len(current.SliceChildren) != 2 || !reflect.DeepEqual(current.ChildCreation, creation) {
					t.Fatal("recovery changed the creation plan or released a partial member set")
				}
				for _, plan := range creation.Children {
					if current.SliceChildren[plan.SliceID] != plan.RunID {
						t.Fatal("recovery changed a planned child identity")
					}
					child, err := Open(d.f.root, plan.RunID)
					if err != nil {
						t.Fatal(err)
					}
					snapshot, err := child.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					s := snapshot.State
					if s.RunID != plan.RunID || s.ConfigurationName() != parent.ConfigurationName() || s.NativeRoot != current.NativeResources[plan.ResourceID].Path || s.Phase != runtime.PhaseTopologyAndRoute {
						t.Fatal("released child lost its origin or activation boundary")
					}
					if s.FlowNodeEnabled("repair") != enabled || len(s.PendingActions) != 0 || len(s.Decisions) != 0 {
						t.Fatal("activation did not apply the latest library value through the ordinary path")
					}
					if original := bootstrapped[plan.RunID]; original != nil {
						if original.StartedAt != s.StartedAt || original.NativeRoot != s.NativeRoot || original.NativeHandle != s.NativeHandle || !reflect.DeepEqual(original.SliceChild, s.SliceChild) {
							t.Fatal("activation replaced the established child")
						}
					} else {
						bootstrapped[plan.RunID] = s
					}
				}
			}
			edit(enabled, "shared-child-model")
			var children []*validationDriver
			for _, plan := range creation.Children {
				child, err := Open(d.f.root, plan.RunID)
				if err != nil {
					t.Fatal(err)
				}
				run, err := child.Show()
				if err != nil {
					t.Fatal(err)
				}
				driver := &validationDriver{t: t, f: child, run: run, revision: d.revision, git: child.vcs}
				driver.confirmSliceRoute(protocol.RouteDecision{Gates: []string{"quality"}, Selected: []string{"quality"}}, sliceLanePlan())
				driver.drive()
				action, input := planBatchAction(driver, "command")
				state := driverState(t, driver)
				choice, known := state.ActionExecutionChoice(action)
				if !known || choice.Model != "shared-child-model" || input.Binding.Task != state.PendingActions[action].Task {
					t.Fatal("activated child did not dispatch from its current bound library")
				}
				children = append(children, driver)
			}
			edit(enabled, "updated-child-model")
			for _, child := range children {
				before := driverState(t, child)
				action, _ := planBatchAction(child, "command")
				pending := before.PendingActions[action]
				if _, claimed := before.SpawnReceipts[action]; claimed {
					t.Fatal("the rebind fixture already claimed its worker")
				}
				child.reopen()
				child.drive()
				after := driverState(t, child)
				choice, known := after.ActionExecutionChoice(action)
				if !known || choice.Model != "updated-child-model" || after.PendingActions[action] != pending || after.Attempts[pending.Task].ID != before.Attempts[pending.Task].ID {
					t.Fatal("library rebind changed the unclaimed worker's task or attempt identity")
				}
				if !reflect.DeepEqual(before.Decisions, after.Decisions) || !reflect.DeepEqual(before.PendingAsks, after.PendingAsks) || after.ConfigurationName() != parent.ConfigurationName() || after.StartedAt != bootstrapped[child.f.runID].StartedAt {
					t.Fatal("library edit created a control or rebuilt a child")
				}
			}
		})
	}
}

func TestChildCreationRejectsDifferentConfigurationBinding(t *testing.T) {
	d := splitMaster(t)
	parent := driverState(t, d)
	digest, err := d.f.vcs.TreeDigest(parent.AcceptedNativeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.f.engine.PrepareChildren(digest, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	parent = driverState(t, d)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	name, err := settings.SaveConfiguration(d.f.root, home, "project", "other-child-binding", parent.ConfigurationName(), nil)
	if err != nil {
		t.Fatal(err)
	}
	input := *parent
	input.Settings = stateSettings(parent)
	input.Settings.ConfigurationName = name
	plan := parent.ChildCreation.Children[0]
	child, err := d.f.prepareChild(&input, plan, OwnerBinding{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.f.finishChildBootstrap(child); err != nil {
		t.Fatal(err)
	}
	actual, err := child.engine.Load()
	if err != nil || actual.State.ConfigurationName() != name {
		t.Fatal("negative child was not actually created under the other binding", err)
	}
	if _, err := child.Drive(); err == nil || !strings.Contains(err.Error(), "child creation origin differs from its master") {
		t.Fatal("a differently bound child accepted parent authority", err)
	}
	before, err := os.ReadFile(filepath.Join(child.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		d.reopen()
		_, err := d.f.Drive()
		if err == nil || !strings.Contains(err.Error(), "child preparation does not prove the planned child") || len(driverState(t, d).SliceChildren) != 0 {
			t.Fatal("a different child binding was released", err)
		}
	}
	after, err := os.ReadFile(filepath.Join(child.stateDir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected child binding was rebuilt or rewritten", err)
	}
}
