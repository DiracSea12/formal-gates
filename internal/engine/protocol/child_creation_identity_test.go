package protocol

import (
	"testing"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/settings"
)

func TestPreparedChildBindsOriginAndConfigurationName(t *testing.T) {
	confirmation := reviewConfirmation()
	topology := TopologyBinding{Decision: topologySplit, SplitCount: 2, Slices: []string{"core", "tools"}, Parallel: "independent slices"}
	plan := ChildPreparation{SliceID: "core", RunID: "child", ResourceID: "workspace"}
	resource := NativeResource{ID: plan.ResourceID, Path: "child-root", Handle: vcs.WorkspaceHandle{Source: "native-origin"}}
	parent := &State{
		RunID: "master", VCSName: "git", BaseSnapshot: "base", Topology: &topology,
		Settings:                  &settings.Effective{ConfigurationName: "shared"},
		NativeResources:           map[string]NativeResource{resource.ID: resource},
		IntakeConfirmationReceipt: confirmation,
		ChildCreation:             &ChildCreation{RequirementRevision: "requirement", Intake: confirmation, Coverage: &CoverageInventory{}, Topology: topology, Children: []ChildPreparation{plan}},
	}
	start := parent.ChildStart(plan)
	actual := &State{
		RunID: plan.RunID, Route: "regular", VCSName: "git", BaseSnapshot: parent.BaseSnapshot,
		NativeRoot: resource.Path, NativeHandle: resource.Handle,
		SliceChild: &start.Binding, Topology: &start.Topology, IntakeConfirmationReceipt: confirmation,
		Settings:      &settings.Effective{ConfigurationName: "shared"},
		FlowSelection: &FlowSelection{Disabled: []string{"repair"}},
	}
	if !parent.preparedChild(plan, actual) {
		t.Fatal("a changed application value invalidated an otherwise exact child origin")
	}
	for _, test := range []struct {
		name   string
		change func(*State)
	}{
		{"missing-child", func(s *State) { s.SliceChild = nil }},
		{"run", func(s *State) { s.RunID = "other-child" }},
		{"route", func(s *State) { s.Route = "lightweight" }},
		{"native-vcs", func(s *State) { s.VCSName = "svn" }},
		{"native-resource-handle", func(s *State) { s.NativeHandle.Source = "other-resource" }},
		{"native-resource-root", func(s *State) { s.NativeRoot = "other-root" }},
		{"base-source", func(s *State) { s.BaseSnapshot = "other-source" }},
		{"master", func(s *State) { s.SliceChild.MasterRunID = "other-master" }},
		{"slice", func(s *State) { s.SliceChild.SliceID = "tools" }},
		{"requirement", func(s *State) { s.SliceChild.RequirementRevision = "other-requirement" }},
		{"inherited-intake", func(s *State) { s.SliceChild.InheritedIntake.RequirementRevision = "other-intake" }},
		{"coverage", func(s *State) { s.SliceChild.Coverage = nil }},
		{"topology", func(s *State) { s.Topology.Slices = []string{"other-slice"} }},
		{"initial-intake", func(s *State) { s.IntakeConfirmationReceipt.RequirementRevision = "other-initial-intake" }},
		{"missing-initial-intake", func(s *State) { s.IntakeConfirmationReceipt = nil }},
		{"configuration-name", func(s *State) { s.Settings.ConfigurationName = "other-binding" }},
		{"missing-named-settings", func(s *State) { s.Settings = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			wrong, err := cloneState(actual)
			if err != nil {
				t.Fatal(err)
			}
			test.change(wrong)
			if parent.preparedChild(plan, wrong) {
				t.Fatal("a different immutable child origin was accepted")
			}
		})
	}
	parent.Settings, actual.Settings = nil, nil
	if !parent.preparedChild(plan, actual) {
		t.Fatal("standalone default-bound child initialization was rejected")
	}
}
