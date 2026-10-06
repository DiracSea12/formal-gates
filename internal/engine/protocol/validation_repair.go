package protocol

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"slices"
)

// repairWork selects one frozen obligation per Task. WorkDecisions replace
// identity; Results discharge the duty. Issued work retains its original wave;
// unissued work follows its latest projection.
type repairWork struct {
	wave            *ValidationWave
	task            runtime.TaskKey
	result          *WorkerResult
	plan            *GranularityReview
	batch           *DevelopmentBatch
	planAction      string
	verification    bool
	previous        *WorkerResult
	pureP2          bool
	businessRepairs int
}

func (s *State) repairWork() []repairWork {
	if s.Validation == nil {
		return nil
	}
	required := s.requiredWorkTasks()
	var work []repairWork
	for i := range s.Validation.Waves {
		w := &s.Validation.Waves[i]
		if w.Repair == nil {
			continue
		}
		for _, current := range s.repairMembers(w) {
			task := current.task
			binding, issued := s.repairIssue(task)
			if issued != 0 && binding.WaveID != w.ID {
				continue
			}
			current.result = s.developmentResultRecord(task)
			if current.result != nil && current.result.Validation.Binding.Generation != s.Validation.Generation && !s.validationWorkCarryAuthorized(current.result.ActionID, task) {
				current.result = nil
			}
			owed := slices.Contains(required, task) || s.validationRepairPending(w)
			if current.result == nil && s.repairTaskFinished(task) {
				owed = false
			}
			if current.result == nil && !owed {
				continue
			}
			if prior := slices.IndexFunc(work, func(previous repairWork) bool { return previous.task == task }); prior >= 0 {
				work[prior] = current
			} else {
				work = append(work, current)
			}
		}
	}
	for i := range s.Validation.Waves {
		if verification := s.repairVerificationWork(&s.Validation.Waves[i]); verification != nil {
			work = append(work, *verification)
		}
	}
	return work
}

// A terminal Task without a remaining attempt has no executable repair work.
// Its original result, if any, remains a separate completion fact.
func (s *State) repairTaskFinished(task runtime.TaskKey) bool {
	_, active := s.Attempts[task]
	_, ended := s.EndedAttempts[task]
	return s.Tasks[task] == runtime.TaskTerminal && !active && !ended
}

func (s *State) signedCandidateRepair(w *ValidationWave) *ValidationWave {
	for i := range s.Validation.Waves {
		original := &s.Validation.Waves[i]
		if original.Candidate == w.Candidate && original.Repair != nil {
			binding, issued := s.repairIssue(original.Repair.Task)
			if issued != 0 && binding.WaveID == original.ID {
				return original
			}
		}
	}
	return nil
}

func (s *State) candidateRepairPending(w *ValidationWave) bool {
	for _, work := range s.repairWork() {
		if work.wave.Candidate == w.Candidate && work.result == nil {
			return true
		}
	}
	return false
}

func (s *State) nextRepairWork() *repairWork {
	work := s.repairWork()
	// A signed in-flight duty keeps its input before queued replacement work.
	for _, w := range work {
		if w.result == nil {
			if _, ok := s.Attempts[w.task]; ok {
				return &w
			}
			if a, ok := s.EndedAttempts[w.task]; ok && a.AwaitingResult {
				return &w
			}
		}
	}
	for _, w := range work {
		if w.result == nil {
			return &w
		}
	}
	return nil
}
func (s *State) repairReady(w *repairWork) bool {
	return w != nil && s.repairDependenciesReady(*w)
}

// RepairInput carries the original problem evidence, not only lookup IDs.
type RepairInput struct {
	FinalCandidate    *coverage.ValidationCandidate `json:"finalCandidate,omitempty"`
	MemberResults     []WorkerResult                `json:"memberResults,omitempty"`
	Plan              *GranularityReview            `json:"plan,omitempty"`
	Route             RouteDecision                 `json:"route"`
	Obligation        ValidationRepair              `json:"obligation"`
	Findings          []ValidationFinding           `json:"findings"`
	Problems          []ValidationProblem           `json:"problems"`
	ExecutionFailures []RepairExecutionFailure      `json:"executionFailures,omitempty"`
}

// RepairExecutionFailure expands the reference from the immutable source target.
type RepairExecutionFailure struct {
	FindingID string                     `json:"findingId"`
	Binding   WorkBinding                `json:"binding"`
	Records   []coverage.ExecutionRecord `json:"records"`
}

func (s *State) executionFailureRecords(f *ValidationFinding) []coverage.ExecutionRecord {
	if f.ExecutionFailure == nil {
		return nil
	}
	ref := f.ExecutionFailure
	binding, ok := s.DispatchBindings[ref.ResultAction]
	if !ok || ref.CaseID == "" || binding.WaveID != f.WaveID || binding.Task.String() != f.Target {
		return nil
	}
	wave := s.validationWave(f.WaveID)
	if wave == nil {
		return nil
	}
	target := wave.Targets[f.Target]
	if target == nil || target.ResultAction != ref.ResultAction || target.Execution == nil {
		return nil
	}
	var rows []coverage.ExecutionRecord
	for _, row := range target.Execution.Records {
		if row.CaseID == ref.CaseID && row.Result == coverage.ExecutedFail {
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *State) repairInput(task runtime.TaskKey) *RepairInput {
	for _, w := range s.repairWork() {
		if w.task != task {
			continue
		}
		input := &RepairInput{Plan: w.plan, Route: s.repairRoute(w), Obligation: ValidationRepair{Task: w.wave.Repair.Task, Problems: slices.Clone(w.wave.Repair.Problems), PureP2: w.wave.Repair.PureP2}}
		if w.verification {
			_, proof, _ := s.repairFinalSource(w)
			input.FinalCandidate = &coverage.ValidationCandidate{Identity: proof.Identity, Digest: proof.Digest}
			for _, member := range s.repairMembers(w.wave) {
				if r := s.developmentResultRecord(member.task); r != nil {
					input.MemberResults = append(input.MemberResults, *r)
				}
			}
		}
		_, cutoff := s.repairIssue(w.wave.Repair.Task)
		if w.previous != nil {
			_, cutoff = s.repairIssue(task)
		}
		for _, id := range w.wave.Repair.Problems {
			problem := ValidationProblem{ID: id}
			for _, f := range s.Validation.Findings {
				a, ok := findingResolutionAt(f, cutoff)
				if ok && a.ProblemID == id {
					problem.FindingIDs = append(problem.FindingIDs, f.ID)
				}
			}
			slices.Sort(problem.FindingIDs)
			input.Problems = append(input.Problems, problem)
		}
		for _, f := range s.Validation.Findings {
			a, ok := findingResolutionAt(f, cutoff)
			if !ok || !slices.Contains(w.wave.Repair.Problems, a.ProblemID) && s.validationFindingTarget(w.wave, f) == nil {
				continue
			}
			copy := *f
			copy.History = nil
			for _, resolution := range f.History {
				if cutoff == 0 || resolution.AcceptedRevision <= cutoff {
					copy.History = append(copy.History, resolution)
				}
			}
			input.Findings = append(input.Findings, copy)
			if f.ExecutionFailure != nil {
				input.ExecutionFailures = append(input.ExecutionFailures, RepairExecutionFailure{FindingID: f.ID, Binding: s.DispatchBindings[f.ExecutionFailure.ResultAction], Records: s.executionFailureRecords(f)})
			}
		}
		slices.SortFunc(input.Findings, func(a, b ValidationFinding) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
		slices.SortFunc(input.ExecutionFailures, func(a, b RepairExecutionFailure) int {
			if a.FindingID < b.FindingID {
				return -1
			}
			if a.FindingID > b.FindingID {
				return 1
			}
			return 0
		})
		return input
	}
	return nil
}

func (s *State) repairExtraAuthorized(work *repairWork) bool {
	if work.wave.ExtraAuthorized && work.businessRepairs == 0 {
		return true
	}
	for request, decision := range s.Decisions {
		ask := s.PendingAsks[request].Validation
		if ask != nil && ask.Kind == "REPAIR" && ask.Binding.Task == work.task && decision.Choice == "approve" {
			return true
		}
	}
	return false
}

func (s *State) retireRepairWork(work repairWork, reason string) error {
	for action, binding := range s.DispatchBindings {
		if binding.Task == work.task && s.liveDispatch(action) {
			s.retireAction(action, "", reason)
		}
	}
	delete(s.EndedAttempts, work.task)
	for id, ask := range s.PendingAsks {
		if ask.Validation != nil && ask.Validation.Binding.Task == work.task && !ask.Resolved {
			ask.Resolved = true
			s.PendingAsks[id] = ask
		}
	}
	return s.terminalizeTask(work.task)
}

// candidateRepairChanges uses the frozen candidate inputs. A changed Batch or
// unexplained removal cannot acquire repair-only inheritance or P2 treatment.
func (s *State) candidateRepairChanges(prior, current *ValidationWave) ([]repairWork, bool) {
	var old []runtime.TaskKey
	if prior != nil {
		old = prior.SourceTasks
	}
	var added []runtime.TaskKey
	for _, task := range current.SourceTasks {
		if !slices.Contains(old, task) {
			added = append(added, task)
		}
	}
	allRepairs := len(added) > 0
	var repairs []repairWork
	work := s.repairWork()
	for _, task := range added {
		found := false
		for _, r := range work {
			if r.task == task && r.result != nil {
				repairs = append(repairs, r)
				found = true
				break
			}
		}
		allRepairs = allRepairs && found
	}
	for _, removed := range old {
		if slices.Contains(current.SourceTasks, removed) {
			continue
		}
		replaced := false
		for _, task := range added {
			if s.sourceValidationTask(task) == s.sourceValidationTask(removed) {
				replaced = true
				break
			}
		}
		allRepairs = allRepairs && replaced
	}
	return repairs, allRepairs
}
