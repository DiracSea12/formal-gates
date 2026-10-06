package protocol

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"slices"
)

// repairIssue freezes a logical repair's wave and adjudication input at its
// first actual issue. Retries and explicitly replaced attempts retain it.
func (s *State) repairIssue(task runtime.TaskKey) (WorkBinding, uint64) {
	task = s.sourceValidationTask(task)
	var binding WorkBinding
	var first uint64
	for id, b := range s.DispatchBindings {
		if s.sourceValidationTask(b.Task) == task {
			if r := s.DispatchIssueRevisions[id]; r != 0 && (first == 0 || r < first) {
				binding = b
				first = r
			}
		}
	}
	return binding, first
}

func findingResolutionAt(f *ValidationFinding, revision uint64) (FindingResolution, bool) {
	for i := len(f.History) - 1; i >= 0; i-- {
		a := f.History[i]
		if revision == 0 || a.AcceptedRevision <= revision {
			return a, true
		}
	}
	return FindingResolution{}, false
}

// validationFindingResponsibility includes corrections deferred from a signed
// repair only after that repair is present in the actual candidate sources.
func (s *State) validationFindingResponsibility(w *ValidationWave, f *ValidationFinding) *ValidationTarget {
	if target := s.validationFindingTarget(w, f); target != nil {
		return target
	}
	if w == nil {
		return nil
	}
	a, ok := findingResolutionAt(f, 0)
	if !ok {
		return nil
	}
	for _, work := range s.repairWork() {
		if work.result == nil || !slices.Contains(w.SourceTasks, work.task) || slices.Contains(work.wave.Repair.Problems, a.ProblemID) {
			continue
		}
		old := s.validationFindingTarget(work.wave, f)
		if old == nil {
			continue
		}
		for _, target := range w.Targets {
			if sameTarget(target, old) {
				return target
			}
		}
	}
	return nil
}

// resolveValidationProblems records completion on the problem itself. The
// referenced wave owns the actual candidate and immutable source/result facts.
func (s *State) resolveValidationProblems(wave *ValidationWave) bool {
	if wave == nil || wave.Status == "INVALIDATED" {
		return false
	}
	changed := false
	complete := func(id string) {
		if p := s.Validation.Problems[id]; p != nil && p.ResolvedWave == "" {
			p.ResolvedWave = wave.ID
			changed = true
		}
	}
	work := s.repairWork()
	for _, repair := range work {
		if repair.result != nil && slices.Contains(wave.SourceTasks, repair.task) && repair.wave.Repair.PureP2 && s.repairSourcesComplete(wave, repair.wave) {
			for _, id := range s.repairCompletedProblems(repair) {
				if s.repairProblemValidationComplete(wave, repair, id, true) {
					complete(id)
				}
			}
		}
	}
	if !s.validationFindingsAdjudicated(wave) {
		return changed
	}
	for _, target := range wave.Targets {
		if target.Status == "PASS" {
			continue
		}
		if target.Status != "FAIL" || target.Kind == "QA" {
			return changed
		}
		found := false
		for _, f := range s.Validation.Findings {
			if s.validationFindingTarget(wave, f) != target {
				continue
			}
			found = true
			a, ok := findingResolutionAt(f, 0)
			if !ok || a.Decision != "DISMISSED" && !s.validationProblemResolved(a.ProblemID) && (f.ExecutionFailure != nil || a.Severity == "P0" || a.Severity == "P1") {
				return changed
			}
		}
		if !found {
			return changed
		}
	}
	for _, repair := range work {
		if repair.result == nil || !slices.Contains(wave.SourceTasks, repair.task) || !s.repairSourcesComplete(wave, repair.wave) {
			continue
		}
		for _, id := range s.repairCompletedProblems(repair) {
			if s.repairProblemValidationComplete(wave, repair, id, false) {
				complete(id)
			}
		}
	}
	return changed
}

func (s *State) validationProblemResolved(id string) bool {
	p := s.Validation.Problems[id]
	return p != nil && p.ResolvedWave != ""
}

type validationObligation struct {
	id     string
	target *ValidationTarget
	pureP2 bool
}

// validationObligationDetails is the common current repair/disposition query.
// A failed QA report without a confirmed problem still needs explicit disposal.
func (s *State) validationObligationDetails(w *ValidationWave) []validationObligation {
	if w == nil {
		return nil
	}
	var obligations []validationObligation
	coveredCases := map[string]map[string]bool{}
	for _, f := range s.Validation.Findings {
		target := s.validationFindingResponsibility(w, f)
		if target == nil || target.Status == "DISPOSED" || target.Status == "AUTHORIZED_SKIP" {
			continue
		}
		a, ok := findingResolutionAt(f, 0)
		if !ok {
			continue
		}
		waived := s.validationObligationWaived(w, target, a.ProblemID)
		open := mandatoryValidationFinding(target, f, a) && !s.validationProblemResolved(a.ProblemID)
		if open && !waived {
			obligations = append(obligations, validationObligation{a.ProblemID, target, target.Kind == "GATE" && a.Severity == "P2"})
		}
		// Only this execution's own case decision can account for its failure.
		// A resolved problem prevents duplicate repair, but does not waive QA.
		if ref := f.ExecutionFailure; ref != nil && ref.ResultAction == target.ResultAction && (open || waived) {
			if coveredCases[target.Task.String()] == nil {
				coveredCases[target.Task.String()] = map[string]bool{}
			}
			coveredCases[target.Task.String()][ref.CaseID] = true
		}
	}
	for _, target := range w.Targets {
		if target.Kind != "QA" || target.Status != "FAIL" || s.validationObligationWaived(w, target, target.Task.String()) {
			continue
		}
		for _, row := range target.Execution.Records {
			if row.Result == coverage.ExecutedFail && !coveredCases[target.Task.String()][row.CaseID] {
				obligations = append(obligations, validationObligation{target.Task.String(), target, false})
				break
			}
		}
	}
	return obligations
}

func (s *State) repairSourcesComplete(wave, original *ValidationWave) bool {
	if !s.repairCompleted(original) {
		return false
	}
	for _, member := range s.repairMembers(original) {
		if !slices.Contains(wave.SourceTasks, member.task) {
			return false
		}
	}
	return true
}

// Completion covers the signed input of each actual correction source. The
// issue cutoff excludes later adjudications on the same original finding.
func (s *State) repairProblemCoverage(work repairWork) []FindingResolution {
	var covered []FindingResolution
	_, cutoff := s.repairIssue(work.wave.Repair.Task)
	for _, finding := range s.Validation.Findings {
		resolution, known := findingResolutionAt(finding, cutoff)
		if cutoff != 0 && known && slices.Contains(work.wave.Repair.Problems, resolution.ProblemID) && s.validationFindingTarget(work.wave, finding) != nil {
			covered = append(covered, resolution)
		}
	}
	executions := s.repairExecutions(work.wave)
	for _, execution := range executions {
		if execution.previous == nil || execution.batch == nil || s.developmentResultRecord(execution.task) == nil {
			continue
		}
		_, cutoff := s.repairIssue(execution.task)
		if cutoff == 0 {
			continue
		}
		index := slices.IndexFunc(executions, func(source repairWork) bool { return source.task == execution.previous.Validation.Binding.Task })
		if index < 0 {
			continue
		}
		source := executions[index]
		for _, gate := range s.repairGates(source) {
			_, target := s.memberGateTarget(source, gate)
			if target == nil {
				continue
			}
			for _, finding := range s.Validation.Findings {
				if finding.WaveID != work.wave.ID || finding.Target != target.Task.String() {
					continue
				}
				resolution, known := findingResolutionAt(finding, cutoff)
				if known && mandatoryValidationFinding(target, finding, resolution) {
					covered = append(covered, resolution)
				}
			}
		}
	}
	return covered
}

func (s *State) repairCompletedProblems(work repairWork) []string {
	problems := slices.Clone(work.wave.Repair.Problems)
	for _, covered := range s.repairProblemCoverage(work) {
		if !slices.Contains(problems, covered.ProblemID) {
			problems = append(problems, covered.ProblemID)
		}
	}
	return problems
}

func (s *State) repairProblemValidationComplete(wave *ValidationWave, work repairWork, id string, carry bool) bool {
	covered := s.repairProblemCoverage(work)
	for _, finding := range s.Validation.Findings {
		resolution, known := findingResolutionAt(finding, 0)
		if !known || resolution.ProblemID != id {
			continue
		}
		target := s.validationFindingResponsibility(wave, finding)
		if target == nil {
			continue
		}
		if !slices.ContainsFunc(covered, func(original FindingResolution) bool {
			return original.FindingID == finding.ID && original.AcceptedRevision == resolution.AcceptedRevision && original.ProblemID == id
		}) {
			return false
		}
		if !carry && target.Status != "PASS" && target.Status != "DISPOSED" && target.Status != "AUTHORIZED_SKIP" {
			return false
		}
	}
	return true
}
