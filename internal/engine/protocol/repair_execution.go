package protocol

import (
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// A repair plan belongs to the original worker result. Tasks and dependency
// frontiers are projections; a single unplanned repair has one member.
func (s *State) repairMembers(w *ValidationWave) []repairWork {
	var members []repairWork
	for _, execution := range s.repairExecutions(w) {
		prior := slices.IndexFunc(members, func(member repairWork) bool {
			return member.batch != nil && execution.batch != nil && member.batch.ID == execution.batch.ID
		})
		if prior < 0 {
			members = append(members, execution)
		} else {
			members[prior] = execution
		}
	}
	return members
}

// Executions project the same member chain, including its exact predecessor
// source. No execution history is stored outside the result and issue owners.
func (s *State) repairExecutions(w *ValidationWave) []repairWork {
	task := s.replacementFold(w.Repair.Task)
	var proposal *WorkerResult
	for _, r := range s.Results {
		if r.Validation != nil && r.Validation.GranularityReview != nil && r.Validation.Binding.Task == task && r.Outcome == OutcomePass {
			copy := r
			proposal = &copy
			break
		}
	}
	if proposal == nil {
		return []repairWork{{wave: w, task: task}}
	}
	p := proposal.Validation.GranularityReview
	contract := ""
	if len(p.Batches) > 1 || proposal.Validation.Development == nil {
		contract = strings.TrimPrefix(planContractDigest(p), "sha256:")
	}
	var executions []repairWork
	for i := range p.Batches {
		key := task
		if len(p.Batches) > 1 || proposal.Validation.Development == nil {
			key.Scope += ":" + contract + ":" + p.Batches[i].ID
		}
		member := repairWork{wave: w, task: s.replacementFold(key), plan: p, batch: &p.Batches[i], planAction: proposal.ActionID, businessRepairs: s.repairBusinessRounds(w, p.Batches[i].ID)}
		for {
			completion := s.developmentResultRecord(member.task)
			executions = append(executions, member)
			if completion == nil {
				break
			}
			failed, pureP2 := s.repairGateCorrection(member)
			if failed == "" {
				break
			}
			member.previous = completion
			member.task.Scope += ":correction:" + failed
			member.pureP2 = pureP2

			member.task = s.replacementFold(member.task)
		}
	}
	return executions
}

func (s *State) repairMember(task runtime.TaskKey) *repairWork {
	for _, work := range s.repairWork() {
		if work.task == task {
			return &work
		}
	}
	return nil
}

func (s *State) repairRoute(work repairWork) RouteDecision {
	if work.batch != nil && work.batch.Route != nil {
		return *work.batch.Route
	}
	gates := slices.DeleteFunc(slices.Clone(s.SelectedGates), func(g string) bool {
		return g == "blackbox" || g == "whitebox" || slices.Contains(reservedRouteGates, g)
	})
	return RouteDecision{Mode: routeModeFull, Gates: gates}
}

func (s *State) repairGates(work repairWork) []string {
	if work.verification || work.wave.Repair.PureP2 || work.pureP2 || work.plan == nil || len(work.plan.Batches) == 1 {
		return nil
	}
	route := s.repairRoute(work)
	if route.Mode == routeModeCustom {
		return route.Selected
	}
	return route.Gates
}

// ValidateRepairPlan checks the shared plan contract and the repair-specific
// slice routes, including whether this proposal may also complete its work.
func ValidateRepairPlan(p *GranularityReview, completion bool) error {
	if err := ValidateGranularity(p); err != nil {
		return err
	}

	for _, batch := range p.Batches {
		if batch.Spike {
			return validationError("repair batches must deliver the original obligation")
		}
		if batch.Route != nil {
			if batch.Route.DevelopmentPlan != nil || batch.Route.Mode != routeModeFull && batch.Route.Mode != routeModeCustom {
				return validationError("repair slice route must be full or custom without another development plan")
			}
			if err := validateRouteDecision(batch.Route); err != nil {
				return err
			}
			for _, gate := range batch.Route.Gates {
				if gate == "blackbox" || gate == "whitebox" {
					return validationError("repair slices do not select ordinary QA")
				}
			}
		}
	}
	if len(p.Batches) > 1 && completion {
		return validationError("a multi-batch plan must precede its isolated member completions")
	}
	return nil
}

func (s *State) acceptRepairResult(record WorkerResult) error {
	p := record.Validation
	work := s.repairMember(p.Binding.Task)
	if work == nil || work.wave.ID != p.Binding.WaveID || work.result != nil || p.Status != "PASS" {
		return validationError("repair result does not bind an unfinished authorized member")
	}
	if p.GranularityReview != nil {
		if work.plan != nil || work.task != s.replacementFold(work.wave.Repair.Task) {
			return validationError("only the original repair worker can freeze its plan")
		}
		if err := ValidateRepairPlan(p.GranularityReview, p.Development != nil); err != nil {
			return err
		}
		if p.Development == nil {
			if p.Manifest != nil || p.Review != nil || p.Execution != nil || p.Resolution != nil {
				return validationError("repair plan carries no other semantic result")
			}
			return nil
		}
		work.batch = &p.GranularityReview.Batches[0]
	}
	if p.Development == nil || p.Manifest != nil || p.Review != nil || p.Execution != nil || p.Resolution != nil || len(p.Development.Paths) == 0 || strings.TrimSpace(p.Development.Summary) == "" {
		return validationError("repair completion requires declared paths and summary")
	}
	if work.verification {
		if err := s.validateFinalRepairEvidence(*work, *p.Development); err != nil {
			return err
		}
		return nil
	}
	if work.batch != nil {
		if err := validateDevelopmentCompletion(*work.batch, p.Development); err != nil {
			return err
		}
	}
	if err := s.validateRepairEvidence(*p.Development, work.wave.Repair.PureP2 || work.pureP2); err != nil {
		return err
	}
	s.Validation.DevelopmentSnapshot, s.Validation.AuthoringWorkspace = "", ""
	s.Validation.Candidate = nil
	return nil
}

func (s *State) repairDevelopmentInput(work repairWork) *DevelopmentInput {
	if work.batch == nil {
		return nil
	}
	input := &DevelopmentInput{Batch: *work.batch, Dependencies: map[string]WorkerResult{}}
	for _, member := range s.repairMembers(work.wave) {
		if slices.Contains(work.batch.DependsOn, member.batch.ID) {
			if result := s.developmentResultRecord(member.task); result != nil {
				input.Dependencies[member.batch.ID] = *result
			}
		}
	}
	return input
}

func (s *State) repairDependenciesReady(work repairWork) bool {
	if work.verification || work.batch == nil {
		return true
	}
	if work.previous != nil {
		return true
	}
	if work.plan.VerificationTier == "CHECKPOINT" {
		for _, group := range work.plan.CheckpointGroups {
			if slices.Contains(group, work.batch.ID) {
				break
			}
			for _, member := range s.repairMembers(work.wave) {
				if slices.Contains(group, member.batch.ID) && (s.developmentResultRecord(member.task) == nil || !s.repairGatesComplete(member)) {
					return false
				}
			}
		}
	}
	for _, member := range s.repairMembers(work.wave) {
		if slices.Contains(work.batch.DependsOn, member.batch.ID) {
			if s.developmentResultRecord(member.task) == nil || !s.repairGatesComplete(member) {
				return false
			}
			if s.NativeRoot != "" {
				if _, _, ok := s.repairIntegrationReceipt("MERGE_FINISH", member.task, &work.task); !ok {
					return false
				}
			}
		}
	}
	return true
}

// Frozen source identities are native receipts, never a mutable workspace HEAD.
func (s *State) repairSource(task runtime.TaskKey) (HostActionIntent, CandidateEvidence, bool) {
	var found HostActionIntent
	var proof CandidateEvidence
	for action, receipt := range s.HostActionReceipts {
		intent, exists := s.hostIntent(action)
		p, err := candidateParameters(intent)
		if !exists || err != nil || p.RepairTask == nil || *p.RepairTask != task || p.Integration != nil || !s.candidateContextCurrent(p) || receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled {
			continue
		}
		v, err := decodeCandidateEvidence(receipt)
		if err == nil && intent.Revision > found.Revision {
			found, proof = intent, v
		}
	}
	return found, proof, found.ActionID != ""
}

func (s *State) repairIntegrationReceipt(stage string, source runtime.TaskKey, target *runtime.TaskKey) (HostActionIntent, CandidateEvidence, bool) {
	var found HostActionIntent
	var proof CandidateEvidence
	for action, receipt := range s.HostActionReceipts {
		intent, exists := s.hostIntent(action)
		p, err := candidateParameters(intent)
		if !exists || err != nil || p.Stage != stage || p.Integration == nil || p.Integration.RepairTask == nil || *p.Integration.RepairTask != source || !sameTaskPointer(p.Integration.TargetTask, target) || !s.candidateContextCurrent(p) || receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled {
			continue
		}
		if !s.repairIntegrationCurrent(p) {
			continue
		}
		v, err := decodeCandidateEvidence(receipt)
		if err == nil && intent.Revision > found.Revision {
			found, proof = intent, v
		}
	}
	return found, proof, found.ActionID != ""
}

func sameTaskPointer(a, b *runtime.TaskKey) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (s *State) repairIntegrationCurrent(p CandidateParameters) bool {
	i := p.Integration
	if i == nil || i.RepairTask == nil {
		return false
	}
	var work *repairWork
	if s.Validation != nil {
		for index := range s.Validation.Waves {
			w := &s.Validation.Waves[index]
			if w.Repair == nil {
				continue
			}
			for _, member := range s.repairMembers(w) {
				if member.task == *i.RepairTask {
					member.result = s.developmentResultRecord(member.task)
					work = &member
					break
				}
			}
		}
	}
	if work == nil || work.result == nil {
		return false
	}
	source, proof, ok := s.repairSource(work.task)
	if !ok || source.ActionID != i.SourceAction || proof.Identity != i.Incoming || proof.Digest != i.IncomingTree {
		return false
	}
	if i.TargetTask != nil {
		var target *repairWork
		for _, member := range s.repairMembers(work.wave) {
			if member.task == *i.TargetTask {
				target = &member
				break
			}
		}
		return target != nil && target.wave.ID == work.wave.ID && target.batch != nil && slices.Contains(target.batch.DependsOn, work.batch.ID)
	}
	return p.WorkspaceID == s.repairAssemblyResource(work.wave).ID
}

func (s *State) repairAssemblyResource(w *ValidationWave) NativeResource {
	var members []runtime.TaskKey
	for _, member := range s.repairMembers(w) {
		members = append(members, member.task)
	}
	r := s.resource("repair-integration", w.Candidate.Identity, w.ID+":"+digestOfCanonical(members))
	if existing, ok := s.NativeResources[r.ID]; ok {
		return existing
	}
	return r
}

func (s *State) repairMergeParameters(work repairWork, target *runtime.TaskKey, prefix string, resource NativeResource) (CandidateParameters, bool) {
	source, proof, frozen := s.repairSource(work.task)
	if !frozen {
		return CandidateParameters{}, false
	}
	i := &IntegrationParameters{RepairTask: &work.task, SourceAction: source.ActionID, TargetTask: target, Incoming: proof.Identity, IncomingTree: proof.Digest}
	stage := "MERGE_START"
	if start, _, ok := s.repairIntegrationReceipt(stage, work.task, target); ok {
		stage, i.StartAction = "MERGE_FINISH", start.ActionID
	}
	return CandidateParameters{Stage: stage, Base: prefix, WorkspaceID: resource.ID, Integration: i, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest()}, true
}

func (s *State) repairCandidateParameters(cd *compiler.CompiledDefinition) (CandidateParameters, bool) {
	if s.NativeRoot == "" || s.abortRequested() {
		return CandidateParameters{}, false
	}
	work := s.repairWork()
	for _, member := range work {
		if member.verification {
			continue
		}
		if member.result != nil {
			if _, _, frozen := s.repairSource(member.task); !frozen {
				r := s.taskResource(member.task, cd)
				return CandidateParameters{Stage: "DEVELOPMENT", RepairTask: &member.task, SourceTasks: []runtime.TaskKey{member.task}, Paths: slices.Clone(member.result.Validation.Development.Paths), Base: r.Identity, WorkspaceID: r.ID, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest()}, true
			}
		}
	}
	for _, member := range work {
		if member.verification || member.result != nil || member.batch == nil || member.previous != nil {
			continue
		}
		r := s.taskResource(member.task, cd)
		prefix := s.ResourceStartIdentity(r)
		for _, dep := range s.repairMembers(member.wave) {
			if !slices.Contains(member.batch.DependsOn, dep.batch.ID) {
				continue
			}
			if _, proof, ok := s.repairIntegrationReceipt("MERGE_FINISH", dep.task, &member.task); ok {
				prefix = proof.Identity
				continue
			}
			if s.developmentResultRecord(dep.task) != nil && s.repairGatesComplete(dep) {
				return s.repairMergeParameters(dep, &member.task, prefix, r)
			}
			break
		}
	}
	for _, member := range work {
		if !member.verification && member.result == nil {
			return CandidateParameters{}, false
		}
	}
	for i := range s.Validation.Waves {
		w := &s.Validation.Waves[i]
		if !s.repairDutyCurrent(w) || len(s.repairMembers(w)) == 1 {
			continue
		}
		r := s.repairAssemblyResource(w)
		prefix := s.ResourceStartIdentity(r)
		for _, member := range s.repairMembers(w) {
			if _, proof, ok := s.repairIntegrationReceipt("MERGE_FINISH", member.task, nil); ok {
				prefix = proof.Identity
				continue
			}
			return s.repairMergeParameters(member, nil, prefix, r)
		}
	}
	return CandidateParameters{}, false
}

func (s *State) repairCompleted(w *ValidationWave) bool {
	for _, member := range s.repairMembers(w) {
		if s.developmentResultRecord(member.task) == nil {
			return false
		}
		if s.NativeRoot != "" {
			if _, _, integrated := s.repairFinalSource(member); !integrated {
				return false
			}
		}
		if !s.repairGatesComplete(member) {
			return false
		}
	}
	if verification := s.repairVerificationWork(w); verification != nil {
		return verification.result != nil
	}
	return true
}

func (s *State) repairGateTarget(task runtime.TaskKey) (*ValidationWave, *ValidationTarget) {
	if s.Validation == nil {
		return nil, nil
	}
	for i := range s.Validation.Waves {
		w := &s.Validation.Waves[i]
		if w.Repair != nil {
			if target := w.Repair.Targets[task.String()]; target != nil {
				return w, target
			}
		}
	}
	return nil, nil
}

func (s *State) repairGatesComplete(work repairWork) bool {
	for _, gate := range s.repairGates(work) {
		_, target := s.memberGateTarget(work, gate)
		if target == nil || target.Status != "PASS" && target.Status != "DISPOSED" && target.Status != "AUTHORIZED_SKIP" && !s.repairGateDismissed(work.wave, target) {
			return false
		}
	}
	return true
}

func (s *State) memberGateTarget(work repairWork, gate string) (runtime.TaskKey, *ValidationTarget) {
	var source string
	if s.NativeRoot != "" {
		intent, _, frozen := s.repairSource(work.task)
		if !frozen {
			return runtime.TaskKey{}, nil
		}
		source = intent.ActionID
	} else if result := s.developmentResultRecord(work.task); result != nil {
		source = result.ActionID
	} else {
		return runtime.TaskKey{}, nil
	}
	for _, target := range work.wave.Repair.Targets {
		if target.Task.Scope == repairGateScope(source, gate) {
			key := s.replacementFold(target.Task)
			return key, work.wave.target(key.String())
		}
	}
	return runtime.TaskKey{}, nil
}

func (s *State) ensureRepairGates(cd *compiler.CompiledDefinition) bool {
	changed := false
	for _, work := range s.repairWork() {
		if work.verification || work.result == nil {
			continue
		}
		source := work.result.ActionID
		if s.NativeRoot != "" {
			intent, _, frozen := s.repairSource(work.task)
			if !frozen {
				continue
			}
			source = intent.ActionID
		}
		for _, gate := range s.repairGates(work) {
			task := s.replacementFold(pipelineTask(cd, "engine.gate", repairGateScope(source, gate)))
			if work.wave.Repair.Targets == nil {
				work.wave.Repair.Targets = map[string]*ValidationTarget{}
			}
			if work.wave.Repair.Targets[task.String()] == nil {
				work.wave.Repair.Targets[task.String()] = &ValidationTarget{Task: task, Gate: gate, Kind: "GATE", Status: "PENDING"}
				changed = true
			}
		}
	}
	return changed
}

func (s *State) repairGateBinding(task runtime.TaskKey, works []repairWork) (WorkBinding, bool) {
	w, target := s.repairGateTarget(task)
	if target == nil {
		return WorkBinding{}, false
	}
	for _, work := range works {
		if work.wave.ID != w.ID {
			continue
		}
		key, _ := s.memberGateTarget(work, target.Gate)
		if key != task {
			continue
		}
		b := WorkBinding{Task: task, WaveID: w.ID, Generation: s.Validation.Generation, RequirementRevision: s.Validation.RequirementRevision, RouteScope: s.validationRouteDigest(), PlanRequest: work.planAction}
		if work.batch != nil {
			b.BatchID = work.batch.ID
		}
		if s.NativeRoot != "" {
			_, source, _ := s.repairSource(work.task)
			b.Candidate = source.Identity
		} else {
			b.Candidate = work.result.Validation.Binding.Candidate
		}
		return b, true
	}
	return WorkBinding{}, false
}

func (s *State) projectRepairGates(view *decision.State, cd *compiler.CompiledDefinition) {
	var work []repairWork
	loaded := false
	for i := range s.Validation.Waves {
		w := &s.Validation.Waves[i]
		if w.Repair == nil {
			continue
		}
		for _, target := range w.Repair.Targets {
			if target.Status != "PENDING" && target.Status != "RUNTIME_ERROR" && target.Status != "RETRY_EXHAUSTED" {
				continue
			}
			if !loaded {
				work = s.repairWork()
				loaded = true
			}
			if _, current := s.repairGateBinding(target.Task, work); !current {
				continue
			}
			reason := decision.WaitReason("")
			if !s.workspaceReady(target.Task, cd) {
				reason = decision.WaitDependencies
			}
			view.RequiredTasks[target.Task.Step] = append(view.RequiredTasks[target.Task.Step], decision.RequiredTask{Task: target.Task, BlockedBy: reason})
		}
	}
}

func (s *State) repairGateInput(b WorkBinding) *GateInput {
	w, target := s.repairGateTarget(b.Task)
	if target == nil {
		return nil
	}
	for _, work := range s.repairWork() {
		if work.wave.ID != w.ID {
			continue
		}
		key, _ := s.memberGateTarget(work, target.Gate)
		if key != b.Task {
			continue
		}
		candidate := coverage.ValidationCandidate{Identity: b.Candidate}
		var owned []string
		if s.NativeRoot != "" {
			_, proof, ok := s.repairSource(work.task)
			if !ok || proof.GateView == nil {
				return nil
			}
			candidate.Digest = proof.Digest
			owned = slices.Clone(proof.GateView.TestOwnedPaths)
		}
		return &GateInput{Gate: target.Gate, BaseSnapshot: w.Candidate.Identity, Candidate: candidate, TestOwnedPaths: owned}
	}
	return nil
}

func (s *State) repairFinalSource(work repairWork) (HostActionIntent, CandidateEvidence, bool) {
	members := s.repairMembers(work.wave)
	if work.verification {
		work = members[len(members)-1]
	}
	if len(members) == 1 {
		return s.repairSource(work.task)
	}
	return s.repairIntegrationReceipt("MERGE_FINISH", work.task, nil)
}

// Pure P2 split repairs self-run the exact integrated content once. This is
// development verification under the same obligation, not another review wave.
func (s *State) repairVerificationWork(w *ValidationWave) *repairWork {
	if !s.repairDutyCurrent(w) {
		return nil
	}
	members := s.repairMembers(w)
	needsSelfRun := w.Repair.PureP2
	for _, member := range members {
		needsSelfRun = needsSelfRun || member.pureP2
	}
	if len(members) < 2 || !needsSelfRun {
		return nil
	}
	var final CandidateEvidence
	for _, member := range members {
		if s.developmentResultRecord(member.task) == nil || !s.repairGatesComplete(member) {
			return nil
		}
		if s.NativeRoot != "" {
			_, proof, ok := s.repairFinalSource(member)
			if !ok {
				return nil
			}
			final = proof
		}
	}
	if s.NativeRoot == "" {
		final.Identity = w.Candidate.Identity
		var actions []string
		for _, member := range members {
			actions = append(actions, s.developmentResultRecord(member.task).ActionID)
		}
		final.Digest = digestOfCanonical(actions)
	}
	task := w.Repair.Task
	task.Scope += ":verification:" + strings.TrimPrefix(digestOfCanonical(struct{ Identity, Digest string }{final.Identity, final.Digest}), "sha256:")
	work := &repairWork{wave: w, task: s.replacementFold(task), verification: true, plan: members[0].plan, planAction: members[0].planAction}
	work.result = s.developmentResultRecord(work.task)
	return work
}

func (s *State) validateFinalRepairEvidence(work repairWork, e DevelopmentEvidence) error {
	var members []DevelopmentEvidence
	for _, member := range s.repairMembers(work.wave) {
		r := s.developmentResultRecord(member.task)
		if r == nil {
			return validationError("final verification lacks a completed repair member")
		}
		members = append(members, *r.Validation.Development)
	}
	if err := ValidateFinalRepairEvidence(members, e); err != nil {
		return err
	}
	return s.validateRepairEvidence(e, true)
}

// ValidateFinalRepairEvidence checks the exact paths and affected-case union
// of the completed members; per-case execution uses ValidateRepairEvidence.
func ValidateFinalRepairEvidence(members []DevelopmentEvidence, e DevelopmentEvidence) error {
	var paths, affected []string
	for _, member := range members {
		paths = append(paths, member.Paths...)
		affected = append(affected, member.AffectedCases...)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	slices.Sort(affected)
	affected = slices.Compact(affected)
	actual := slices.Clone(e.Paths)
	slices.Sort(actual)
	cases := slices.Clone(e.AffectedCases)
	slices.Sort(cases)
	if !slices.Equal(paths, actual) || !slices.Equal(affected, cases) {
		return validationError("final self-run must cover the complete integrated member paths and affected cases")
	}
	return nil
}

// Gate findings retain their source target; an accepted repair disposition
// projects the next member Task without rewriting its completed source result.
func (s *State) repairGateCorrection(work repairWork) (string, bool) {
	result := s.developmentResultRecord(work.task)
	if result == nil {
		return "", false
	}
	for _, gate := range s.repairGates(work) {
		_, target := s.memberGateTarget(work, gate)
		if target == nil || target.Status != "PASS" && target.Status != "FAIL" && target.Status != "DISPOSED" && target.Status != "AUTHORIZED_SKIP" {
			return "", false
		}
	}

	var actions []string
	pureP2 := true
	for _, gate := range s.repairGates(work) {
		_, target := s.memberGateTarget(work, gate)
		if target == nil || target.Status != "FAIL" {
			continue
		}
		found, required := false, false
		for _, finding := range s.Validation.Findings {
			if finding.WaveID != work.wave.ID || finding.Target != target.Task.String() {
				continue
			}
			found = true
			resolution, ok := findingResolutionAt(finding, 0)
			if !ok {
				return "", false
			}
			if resolution.Decision == "DISMISSED" || resolution.Severity == "P3" || s.validationObligationWaived(work.wave, target, resolution.ProblemID) {
				continue
			}
			required = true
			pureP2 = pureP2 && resolution.Severity == "P2"
		}
		if found && required {
			actions = append(actions, target.ResultAction)
		}
	}
	if len(actions) == 0 {
		return "", false
	}
	slices.Sort(actions)
	return strings.TrimPrefix(digestOfCanonical(actions), "sha256:"), pureP2
}

func (s *State) repairGateDismissed(w *ValidationWave, target *ValidationTarget) bool {
	if target == nil || target.Status != "FAIL" {
		return false
	}
	found := false
	for _, finding := range s.Validation.Findings {
		if finding.WaveID != w.ID || finding.Target != target.Task.String() {
			continue
		}
		found = true
		resolution, ok := findingResolutionAt(finding, 0)
		if !ok || resolution.Decision != "DISMISSED" && resolution.Severity != "P3" && !s.validationObligationWaived(w, target, resolution.ProblemID) {
			return false
		}
	}
	return found
}

func repairGateScope(source, gate string) string {
	return "repair:" + strings.TrimPrefix(digestOfCanonical(source), "sha256:") + ":" + gate
}

// Gate task scopes identify an issued source. Different gates on that source
// consume one business round; parallel batches share the greatest depth.
func (s *State) repairBusinessRounds(w *ValidationWave, batch string) int {
	if w.Repair == nil {
		return 0
	}
	sources := map[string]map[string]bool{}
	for _, target := range w.Repair.Targets {
		binding, known := s.DispatchBindings[target.ResultAction]
		if !known || batch != "" && binding.BatchID != batch {
			continue
		}
		failed := false
		for _, finding := range s.Validation.Findings {
			if finding.WaveID != w.ID || finding.Target != target.Task.String() {
				continue
			}
			for _, resolution := range finding.History {
				if resolution.Decision != "DISMISSED" && (resolution.Severity == "P0" || resolution.Severity == "P1") {
					failed = true
				}
			}
		}
		if !failed {
			continue
		}
		if sources[binding.BatchID] == nil {
			sources[binding.BatchID] = map[string]bool{}
		}
		sources[binding.BatchID][strings.TrimSuffix(target.Task.Scope, ":"+target.Gate)] = true
	}
	depth := 0
	for _, observed := range sources {
		if len(observed) > depth {
			depth = len(observed)
		}
	}
	return depth
}
