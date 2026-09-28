package protocol

import (
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
)

// ValidationInput is a read-only projection of the exact issued obligation.
// It supplies contract values rather than asking the host to reconstruct them.
type ValidationInput struct {
	SourcePoints     []SourcePoint               `json:"sourcePoints,omitempty"`
	Binding          WorkBinding                 `json:"binding"`
	Handler          string                      `json:"handler"`
	Manifest         coverage.AcceptanceManifest `json:"manifest"`
	Contract         *coverage.CoverageContract  `json:"contract,omitempty"`
	Whitelist        *coverage.ApprovedWhitelist `json:"whitelist,omitempty"`
	Execution        *coverage.ExecutionBinding  `json:"execution,omitempty"`
	Completed        []coverage.ExecutionRecord  `json:"completed,omitempty"`
	InheritableCases []string                    `json:"inheritableCases,omitempty"`
	PendingCases     []string                    `json:"pendingCases,omitempty"`
}

func (s *State) ValidationInputs(cd *compiler.CompiledDefinition) map[string]ValidationInput {
	values := map[string]ValidationInput{}
	if s.Validation == nil {
		return values
	}
	for id, pending := range s.PendingActions {
		binding, ok := s.DispatchBindings[id]
		if !ok {
			continue
		}
		mode := s.validationMode(pending.Task)
		role := s.validationRole(pending.Task, cd)
		input := ValidationInput{Binding: binding, Handler: role}
		c := s.coverageContract(s.Validation.Candidate)
		switch role {
		case "engine.qa.design", "engine.qa.review":
			contract := modeCoverageContract(c, mode)
			contract.Candidate = nil
			input.Contract = &contract
		case "engine.repair":
			input.Contract = &c
		}
		_, input.SourcePoints = s.coveragePoints()
		input.Manifest.Binding = coverage.ManifestBinding{SourceBinding: c.RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
		for _, src := range c.RequiredSources.Sources {
			if src.Applicability == coverage.QAApplicability {
				input.Manifest.Sources = append(input.Manifest.Sources, src.SourceID)
			}
		}
		input.Manifest.Points, _ = s.coveragePoints()
		if set := s.Validation.Sets[mode]; set != nil {
			input.Manifest = set.Manifest
			input.InheritableCases = s.inheritableCases(set)
		}
		if (role == "engine.qa.design" || role == "engine.qa.review") && binding.RouteScope != "" {
			input.Manifest = manifestAtRoute(input.Manifest, binding.RouteScope)
			for i := range input.Contract.Manifests {
				input.Contract.Manifests[i] = manifestAtRoute(input.Contract.Manifests[i], binding.RouteScope)
			}
		}
		if role == "engine.qa.execution" {
			wave := s.validationWave(binding.WaveID)
			if wave == nil {
				values[id] = input
				continue
			}
			contract, whitelist, err := s.executionContract(mode, wave)
			if err == nil {
				input.Contract = &contract
				input.Whitelist = &whitelist
				input.Execution = &coverage.ExecutionBinding{RequiredSourcesDigest: whitelist.RequiredSourcesDigest, ManifestDigest: whitelist.ManifestDigest, MapDigest: whitelist.MapDigest, WhitelistDigest: whitelist.Digest, Candidate: wave.Candidate, Scope: wave.Scope}
				for _, edge := range whitelist.Entries {
					if !slices.Contains(input.Execution.ExpectedCaseIDs, edge.CaseID) {
						input.Execution.ExpectedCaseIDs = append(input.Execution.ExpectedCaseIDs, edge.CaseID)
					}
				}
				slices.Sort(input.Execution.ExpectedCaseIDs)
				input.PendingCases = slices.Clone(input.Execution.ExpectedCaseIDs)
				if target := wave.Targets[pending.Task.String()]; target != nil && target.Execution != nil {
					input.Completed = target.Execution.Records
					for _, record := range input.Completed {
						input.PendingCases = slices.DeleteFunc(input.PendingCases, func(id string) bool { return id == record.CaseID })
					}
				}
			}
		}
		values[id] = input
	}
	return values
}
