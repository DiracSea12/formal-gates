package protocol

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/guard"
	"formal-gates/internal/engine/runtime"
)

// Verification keeps evidence and effective obligations in the same State as
// the existing Attempt lifecycle. A wave's expected set is immutable: adding a
// target or changing input invalidates the wave and creates another binding.
type WaveWaiver struct {
	WaveID              string   `json:"waveId"`
	Candidate           string   `json:"candidate"`
	RequirementRevision string   `json:"requirementRevision"`
	ProblemIDs          []string `json:"problemIds"`
	Reason              string   `json:"reason"`
	Revision            uint64   `json:"revision"`
}
type Verification struct {
	Waivers       []WaveWaiver                  `json:"waivers"`
	Waves         []VerificationWave            `json:"waves"`
	Current       string                        `json:"current"`
	Completed     int                           `json:"completed"`
	Limit         int                           `json:"limit"`
	ExtraAttempts map[string]int                `json:"extraAttempts"`
	Problems      map[string]CanonicalProblem   `json:"problems"`
	Adjudications []WaveAdjudication            `json:"adjudications"`
	Repairs       []RepairPlan                  `json:"repairs"`
	Effective     map[string]ValidationEvidence `json:"effective"`
	Inheritance   []ValidationInheritance       `json:"inheritance"`
	Promotion     *gitprep.PromotionReceipt     `json:"promotion,omitempty"`
	Seal          *SealBinding                  `json:"seal,omitempty"`
}
type CheckpointInvalidation struct {
	CaseID              string `json:"caseId"`
	PreviousTool        string `json:"previousTool"`
	PreviousEnvironment string `json:"previousEnvironment"`
	Tool                string `json:"tool"`
	Environment         string `json:"environment"`
	Reason              string `json:"reason"`
}
type VerificationWave struct {
	CheckpointHistory   map[string][]VerificationResult `json:"checkpointHistory,omitempty"`
	Counted             bool                            `json:"counted"`
	ID                  string                          `json:"id"`
	Candidate           PreparedCandidate               `json:"candidate"`
	RequirementRevision string                          `json:"requirementRevision"`
	Targets             []VerificationTarget            `json:"targets"`
	ExpectedDigest      string                          `json:"expectedDigest"`
	Results             map[string]VerificationResult   `json:"results"`
	Checkpoints         map[string]VerificationResult   `json:"checkpoints"`
	Invalidated         string                          `json:"invalidated,omitempty"`
	Complete            bool                            `json:"complete"`
}
type VerificationTarget struct {
	Target        string                   `json:"target"`
	Task          runtime.TaskKey          `json:"task"`
	ReviewScope   coverage.ScopeMode       `json:"reviewScope"`
	Scope         coverage.ScopeMode       `json:"scope"`
	Expected      []string                 `json:"expected"`
	Inherited     map[string]CaseExecution `json:"inherited"`
	BindingDigest string                   `json:"bindingDigest"`
}
type CaseExecution struct {
	OperationID       string            `json:"operationId,omitempty"`
	Command           *coverage.Command `json:"command,omitempty"`
	ExitCode          *int              `json:"exitCode,omitempty"`
	StdoutPath        string            `json:"stdoutPath,omitempty"`
	StderrPath        string            `json:"stderrPath,omitempty"`
	CaseID            string            `json:"caseId"`
	Status            string            `json:"status"`
	Procedure         string            `json:"procedure"`
	Output            string            `json:"output"`
	Comparison        string            `json:"comparison"`
	Environment       string            `json:"environment"`
	Tool              string            `json:"tool"`
	Workspace         string            `json:"workspace"`
	Dependencies      []string          `json:"dependencies"`
	ReusedPreparation string            `json:"reusedPreparation,omitempty"`
}
type VerificationResult struct {
	Invalidated         []CheckpointInvalidation `json:"invalidated,omitempty"`
	ActionID            string                   `json:"actionId"`
	AttemptID           string                   `json:"attemptId"`
	Provider            string                   `json:"provider"`
	Identity            string                   `json:"identity"`
	RequirementRevision string                   `json:"requirementRevision"`
	Candidate           string                   `json:"candidate"`
	BindingDigest       string                   `json:"bindingDigest"`
	Status              string                   `json:"status"`
	Message             string                   `json:"message"`
	Cases               []CaseExecution          `json:"cases"`
	Remaining           []string                 `json:"remaining"`
	Findings            []ReviewFinding          `json:"findings"`
	Digest              string                   `json:"digest"`
	Quarantined         bool                     `json:"quarantined"`
}
type WaveFindingDecision struct {
	FindingID     string   `json:"findingId"`
	Decision      string   `json:"decision"`
	ProblemID     string   `json:"problemId,omitempty"`
	Severity      string   `json:"severity"`
	Reason        string   `json:"reason"`
	Evidence      string   `json:"evidence"`
	AffectedCases []string `json:"affectedCases"`
}
type WaveAdjudication struct {
	WaveID           string                `json:"waveId"`
	Target           string                `json:"target"`
	ResultDigest     string                `json:"resultDigest"`
	Decisions        []WaveFindingDecision `json:"decisions"`
	Revision         uint64                `json:"revision"`
	ReplacesRevision uint64                `json:"replacesRevision,omitempty"`
}
type CanonicalProblem struct {
	ID            string   `json:"id"`
	Severity      string   `json:"severity"`
	Status        string   `json:"status"`
	Findings      []string `json:"findings"`
	AffectedCases []string `json:"affectedCases"`
	Evidence      string   `json:"evidence"`
}
type RepairPlan struct {
	Issued        bool            `json:"issued"`
	ID            string          `json:"id"`
	WaveID        string          `json:"waveId"`
	Candidate     string          `json:"candidate"`
	Problems      []string        `json:"problems"`
	AffectedCases []string        `json:"affectedCases"`
	PureP2        bool            `json:"pureP2"`
	Task          runtime.TaskKey `json:"task"`
	Result        *RepairResult   `json:"result,omitempty"`
	Committed     string          `json:"committed,omitempty"`
}
type RepairResult struct {
	ActionID            string          `json:"actionId"`
	AttemptID           string          `json:"attemptId"`
	Provider            string          `json:"provider"`
	Identity            string          `json:"identity"`
	RequirementRevision string          `json:"requirementRevision"`
	Candidate           string          `json:"candidate"`
	PlanID              string          `json:"planId"`
	Status              string          `json:"status"`
	Message             string          `json:"message"`
	Manifest            []string        `json:"manifest"`
	Resolved            []string        `json:"resolved"`
	Build               BatchValidation `json:"build"`
	ProjectTests        BatchValidation `json:"projectTests"`
	Cases               []CaseExecution `json:"cases"`
}
type ValidationEvidence struct {
	Target              string             `json:"target"`
	ResultDigest        string             `json:"resultDigest"`
	WaveID              string             `json:"waveId"`
	Candidate           string             `json:"candidate"`
	RequirementRevision string             `json:"requirementRevision"`
	Scope               coverage.ScopeMode `json:"scope"`
	Cases               []CaseExecution    `json:"cases"`
}
type ValidationInheritance struct {
	Kind                string             `json:"kind"`
	Source              ValidationEvidence `json:"source"`
	Candidate           string             `json:"candidate"`
	RequirementRevision string             `json:"requirementRevision"`
	StateRevision       uint64             `json:"stateRevision"`
	Reason              string             `json:"reason"`
}
type SealBinding struct {
	Waivers             []WaveWaiver                  `json:"waivers,omitempty"`
	Candidate           string                        `json:"candidate"`
	ValidatedDigest     string                        `json:"validatedDigest"`
	RequirementRevision string                        `json:"requirementRevision"`
	WaveID              string                        `json:"waveId"`
	Evidence            map[string]ValidationEvidence `json:"evidence"`
	ExternalArtifacts   map[string]string             `json:"externalArtifacts"`
	Advisory            []CanonicalProblem            `json:"advisory"`
}

var verificationSteps = []string{"blackbox-execution", "whitebox-execution", "complexity-gate", "implementation-quality-gate", "repair-worker"}

func verificationStep(step authoring.StepID) bool {
	for _, id := range verificationSteps {
		if string(step) == id {
			return true
		}
	}
	return false
}
func currentWave(r *RegularState) *VerificationWave {
	if r.Verification == nil {
		return nil
	}
	for i := range r.Verification.Waves {
		if r.Verification.Waves[i].ID == r.Verification.Current {
			return &r.Verification.Waves[i]
		}
	}
	return nil
}
func currentRepair(r *RegularState) *RepairPlan {
	w := currentWave(r)
	if w == nil {
		return nil
	}
	for i := range r.Verification.Repairs {
		if r.Verification.Repairs[i].WaveID == w.ID {
			return &r.Verification.Repairs[i]
		}
	}
	return nil
}
func waveTarget(w *VerificationWave, target string) *VerificationTarget {
	if w == nil {
		return nil
	}
	for i := range w.Targets {
		if w.Targets[i].Target == target {
			return &w.Targets[i]
		}
	}
	return nil
}
func targetName(step authoring.StepID) string { return strings.TrimSuffix(string(step), "-execution") }
func equalStrings(a, b []string) bool {
	return strings.Join(sortedStrings(a), "\x00") == strings.Join(sortedStrings(b), "\x00")
}
func allCases(p *PreparedCandidate, target string) []string {
	out := []string{}
	for _, m := range p.Coverage.Manifests {
		if strings.EqualFold(string(m.Binding.ReviewKind), target) {
			for _, c := range m.Cases {
				out = append(out, c.CaseID)
			}
		}
	}
	return sortedStrings(out)
}

// freezeWave is called by the controller only after the candidate assembly has
// checked every selected design and VCS artifact. It never edits an old target
// set, even when route changes make part of its evidence reusable.
func freezeWave(r *RegularState, candidate *PreparedCandidate, affected []string, qaOnly ...bool) {
	v := r.Verification
	if v == nil {
		v = &Verification{Limit: 3, ExtraAttempts: map[string]int{}, Problems: map[string]CanonicalProblem{}, Effective: map[string]ValidationEvidence{}}
		r.Verification = v
	}
	w := VerificationWave{Candidate: *candidate, RequirementRevision: r.Requirement.Revision, Results: map[string]VerificationResult{}, Checkpoints: map[string]VerificationResult{}}
	names := append([]string{}, r.Route.QA...)
	if len(qaOnly) == 0 || !qaOnly[0] {
		names = append(names, r.Route.Gates...)
	}
	sort.Strings(names)
	for _, name := range names {
		if ev, ok := v.Effective[name]; ok && ev.Candidate == candidate.Candidate.Identity && ev.RequirementRevision == r.Requirement.Revision {
			continue
		}
		step := name
		if name == "blackbox" || name == "whitebox" {
			step += "-execution"
		}
		t := VerificationTarget{Target: name, ReviewScope: coverage.ScopeFull, Scope: coverage.ScopeFull, Expected: allCases(candidate, name), Inherited: map[string]CaseExecution{}}
		if affected != nil && (name == "blackbox" || name == "whitebox") {
			t.Scope = coverage.ScopeAffected
			old := v.Effective[name]
			// A failed target can still contain genuine PASS cases. Read its last
			// adjudicated wave without promoting that whole target to effective PASS.
			if previous := currentWave(r); previous != nil {
				if result, ok := previous.Results[name]; ok {
					old.Cases = append([]CaseExecution{}, result.Cases...)
					if target := waveTarget(previous, name); target != nil {
						for _, row := range target.Inherited {
							old.Cases = append(old.Cases, row)
						}
					}
				}
			}
			for _, c := range old.Cases {
				hit := false
				for _, id := range affected {
					if id == c.CaseID {
						hit = true
					}
				}
				if !hit && c.Status == "PASS" {
					t.Inherited[c.CaseID] = c
				}
			}
			expected := []string{}
			for _, id := range t.Expected {
				if _, ok := t.Inherited[id]; !ok {
					expected = append(expected, id)
				}
			}
			t.Expected = expected
		}
		t.BindingDigest = digestOfCanonical(struct {
			Candidate PreparedCandidate
			Req       string
			Target    VerificationTarget
		}{*candidate, r.Requirement.Revision, t})
		t.Task = runtime.TaskKey{Node: "entry", Step: authoring.StepID(step), Scope: fmt.Sprintf("wave-%d:%s", len(v.Waves)+1, strings.TrimPrefix(t.BindingDigest, "sha256:"))}
		w.Targets = append(w.Targets, t)
	}
	w.ExpectedDigest = digestOfCanonical(w.Targets)
	w.ID = "wave:" + digestOfCanonical(struct {
		Candidate string
		Req       string
		Targets   string
		Ordinal   int
	}{candidate.Candidate.Identity, r.Requirement.Revision, w.ExpectedDigest, len(v.Waves) + 1})
	v.Waves = append(v.Waves, w)
	v.Current = w.ID
}
func refreshVerification(s *State) {
	r := s.Regular
	p := r.Preparation
	if p == nil || p.BindingDigest != preparationBinding(r) || r.Topology != "no-split" || p.Candidate == nil || !p.Candidate.ReviewReady || r.ChangePending || r.Ask != nil || r.Terminal != "" {
		return
	}
	if r.Verification == nil || currentWave(r) == nil || currentWave(r).Invalidated != "" || currentWave(r).Candidate.Candidate.Identity != p.Candidate.Candidate.Identity && (currentRepair(r) == nil || !currentRepair(r).PureP2) {
		bindVerificationInheritance(r, p.Candidate)
		freezeWave(r, p.Candidate, nil)
	}
	w := currentWave(r)
	if w == nil || w.Invalidated != "" {
		return
	}
	if !w.Complete {
		complete := len(w.Results) == len(w.Targets)
		for _, t := range w.Targets {
			result, ok := w.Results[t.Target]
			if !ok {
				complete = false
				continue
			}
			if _, ok = s.Cost.Dispatches[result.ActionID]; !ok {
				complete = false
			}
		}
		if complete {
			w.Complete = true

		}
	}
	if !w.Complete || !waveAdjudicated(r, w) {
		return
	}
	if !w.Counted {
		w.Counted = true
		r.Verification.Completed++
	}
	if p := currentRepair(r); p == nil || !p.PureP2 || p.Committed == "" {
		updateEffective(r, w)
	}
	if repair := currentRepair(r); repair != nil {
		return
	}
	problems := []string{}
	affected := []string{}
	pure := true
	for id, p := range r.Verification.Problems {
		if p.Status == "OPEN" && p.Severity != "P3" {
			problems = append(problems, id)
			affected = uniqueAppend(affected, p.AffectedCases...)
			if p.Severity != "P2" {
				pure = false
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		for _, t := range w.Targets {
			if result, ok := w.Results[t.Target]; ok && result.Status == "FAIL" {
				pure = false
			}
		}
		if r.Verification.Completed >= r.Verification.Limit && !pure {
			return
		}
		plan := RepairPlan{WaveID: w.ID, Candidate: w.Candidate.Candidate.Identity, Problems: problems, AffectedCases: sortedStrings(affected), PureP2: pure}
		plan.ID = "repair:" + digestOfCanonical(plan)
		plan.Task = runtime.TaskKey{Node: "entry", Step: "repair-worker", Scope: plan.ID}
		r.Verification.Repairs = append(r.Verification.Repairs, plan)
		return
	}

}
func waveAdjudicated(r *RegularState, w *VerificationWave) bool {
	for _, t := range w.Targets {
		result, ok := w.Results[t.Target]
		if !ok {
			return false
		}
		if len(result.Findings) == 0 {
			continue
		}
		a := lastWaveAdjudication(r, w.ID, t.Target)
		if a == nil || a.ResultDigest != result.Digest {
			return false
		}
	}
	return true
}
func lastWaveAdjudication(r *RegularState, wave, target string) *WaveAdjudication {
	for i := len(r.Verification.Adjudications) - 1; i >= 0; i-- {
		a := &r.Verification.Adjudications[i]
		if a.WaveID == wave && a.Target == target {
			return a
		}
	}
	return nil
}
func updateEffective(r *RegularState, w *VerificationWave) {
	for _, t := range w.Targets {
		result := w.Results[t.Target]
		pass := result.Status == "PASS" || len(t.Expected) == 0
		if a := lastWaveAdjudication(r, w.ID, t.Target); a != nil {
			for _, d := range a.Decisions {
				if d.Decision != "DISMISSED" && (d.Severity == "P0" || d.Severity == "P1") {
					pass = false
				}
			}
		}
		if !pass {
			continue
		}
		cases := append([]CaseExecution{}, result.Cases...)
		for _, id := range sortedCaseKeys(t.Inherited) {
			cases = append(cases, t.Inherited[id])
		}
		r.Verification.Effective[t.Target] = ValidationEvidence{Target: t.Target, ResultDigest: result.Digest, WaveID: w.ID, Candidate: w.Candidate.Candidate.Identity, RequirementRevision: w.RequirementRevision, Scope: t.Scope, Cases: cases}
	}
}
func sortedCaseKeys(m map[string]CaseExecution) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func verificationView(s *State, v *decision.RegularView) {
	r := s.Regular
	w := currentWave(r)
	if w == nil || r.Preparation == nil || r.Preparation.Candidate == nil {
		return
	}
	v.ReviewReady = false
	v.PreparationTasks = nil
	v.PreparationWait = ""
	if w.Invalidated != "" {
		v.PreparationWait = "VERIFICATION_INVALIDATED"
		return
	}
	add := func(k runtime.TaskKey) {
		if at, ok := s.Attempts[k]; ok {
			if at.RetryExhausted {
				v.PreparationAsk = "RETRY_EXHAUSTED"
				return
			}
			if s.Tasks[k] != runtime.TaskTerminal {
				return
			}
		}
		v.PreparationTasks = append(v.PreparationTasks, decision.ReadyTask{Task: k, Step: k.Step})
	}
	if !w.Complete {
		for _, t := range w.Targets {
			if _, ok := w.Results[t.Target]; !ok {
				add(t.Task)
			}
		}
		if len(v.PreparationTasks) == 0 && v.PreparationWait == "" {
			v.PreparationWait = "TASKS_IN_FLIGHT"
		}
		return
	}
	if !waveAdjudicated(r, w) {
		v.VerificationOperator = true
		return
	}
	if repair := currentRepair(r); repair != nil {
		if repair.Committed != "" {
			v.PreparationWait = "VERIFICATION_REBIND"
			return
		}
		if r.Preparation.Operation != nil || r.Preparation.Resources[repair.ID] == "" {
			v.PreparationWait = "GIT_PREPARATION"
			return
		}
		if repair.Result == nil {
			add(repair.Task)
		} else {
			v.PreparationWait = "GIT_PREPARATION"
		}
		return
	}
	for _, p := range r.Verification.Problems {
		if p.Status == "OPEN" && p.Severity != "P3" {
			v.Control = "authorize-wave"
			return
		}
	}
	v.PreparationWait = "PROMOTION"
}
func verificationClaim(r *RegularState, key runtime.TaskKey) (string, string) {
	w := currentWave(r)
	if w == nil {
		return "", ""
	}
	if key.Step == "repair-worker" {
		if p := currentRepair(r); p != nil && p.Task == key {
			return p.Candidate, p.ID
		}
	}
	for _, t := range w.Targets {
		if t.Task == key {
			return w.Candidate.Candidate.Identity, t.BindingDigest
		}
	}
	return "", ""
}
func currentVerificationClaim(r *RegularState, c ExecutionClaim) bool {
	w := currentWave(r)
	if w == nil || w.Invalidated != "" {
		return false
	}
	candidate, binding := verificationClaim(r, c.Task)
	return candidate == c.Candidate && binding == c.PrerequisiteDigest
}
func verificationAllowance(s *State, b *guard.Basis) {
	for _, id := range verificationSteps {
		b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, runtime.TaskKey{Node: "entry", Step: authoring.StepID(id)})
	}
	keys := map[runtime.TaskKey]bool{}
	for _, x := range s.RegularExecutions {
		if verificationStep(x.Claim.Task.Step) {
			keys[x.Claim.Task] = true
		}
	}
	v := s.Regular.Verification
	if v != nil {
		for _, w := range v.Waves {
			for _, t := range w.Targets {
				keys[t.Task] = true
			}
		}
		for _, p := range v.Repairs {
			keys[p.Task] = true
		}
	}
	for key := range keys {
		max := 2
		if v != nil {
			max += v.ExtraAttempts[key.String()]
		}
		b.ExternalTasks = append(b.ExternalTasks, guard.ExternalTask{TaskKey: key, Provenance: guard.Provenance{Kind: guard.DefinitionSource, Identity: b.Definition.Identity}, MaxAttempts: &max})
	}
}

func (e *Engine) acceptVerification(s *State, result VerificationResult, revision uint64) (string, error) {
	r := s.Regular
	x, ok := s.RegularExecutions[result.ActionID]
	if !ok || !x.Claimed || x.Finished || !verificationStep(x.Claim.Task.Step) || (x.Claim.Task.Step == "repair-worker" && result.Status != "CHECKPOINT") {
		return "", fmt.Errorf("UNKNOWN_ACTION: verification requires a live claimed task")
	}
	if result.AttemptID != x.Claim.AttemptID || result.Identity != x.Identity {
		return "", fmt.Errorf("INTENT_MISMATCH: verification native attempt")
	}

	if w := currentWave(r); w != nil {
		var err error
		result, err = mergeVerificationDelta(w.Checkpoints[targetName(x.Claim.Task.Step)], result)
		if err != nil {
			return "", err
		}
	}
	var checkpointErr error
	if result.Status == "CHECKPOINT" && currentVerificationClaim(r, x.Claim) && !r.ChangePending {
		checkpointErr = validateVerificationResult(r, x, result, true)
		if checkpointErr == nil {
			w := currentWave(r)
			target := targetName(x.Claim.Task.Step)
			preserveCheckpoint(w, target)
			result.Digest = ""
			result.Digest = digestOfCanonical(result)
			w.Checkpoints[target] = result
			x.ResumeClaimed = false
			s.RegularExecutions[result.ActionID] = x
			return "CHECKPOINT", nil
		}
	}
	x.Finished = true
	delete(s.PendingActions, result.ActionID)
	s.Tasks[x.Claim.Task] = runtime.TaskTerminal
	result.Quarantined = r.ChangePending || !currentVerificationClaim(r, x.Claim)
	result.Digest = ""
	result.Digest = digestOfCanonical(result)
	x.VerificationResult = &result
	s.RegularExecutions[result.ActionID] = x
	reject := func(reason string) (string, error) {
		at := s.Attempts[x.Claim.Task]
		at.RetryExhausted = x.Number >= preparationMax(r, x.Claim.Task)
		s.Attempts[x.Claim.Task] = at
		r.VerificationErrors = append(r.VerificationErrors, VerificationError{Result: result, Reason: reason, Revision: revision + 1})
		return "REJECTED_RESULT", nil
	}
	if result.Quarantined {
		return reject("QUARANTINED: stale candidate, requirement or wave")
	}
	if checkpointErr != nil {
		return reject(checkpointErr.Error())
	}
	if err := validateVerificationResult(r, x, result, false); err != nil {
		return reject(err.Error())
	}
	w := currentWave(r)
	if len(result.Invalidated) > 0 {
		preserveCheckpoint(w, targetName(x.Claim.Task.Step))
	}
	w.Results[targetName(x.Claim.Task.Step)] = result
	s.Results[result.ActionID] = WorkerResult{ActionID: result.ActionID, Provider: result.Provider, Outcome: result.Status, Digest: result.Digest}
	return "ACCEPTED", nil
}

type VerificationError struct {
	Result   VerificationResult `json:"result"`
	Reason   string             `json:"reason"`
	Revision uint64             `json:"revision"`
}

func validateVerificationResult(r *RegularState, x RegularExecution, result VerificationResult, checkpoint bool) error {
	w := currentWave(r)
	t := qaVerificationTarget(r, x.Claim)
	if t == nil || result.Provider != x.Claim.Provider || result.RequirementRevision != x.Claim.RequirementRevision || result.Candidate != x.Claim.Candidate || result.BindingDigest != t.BindingDigest {
		return fmt.Errorf("verification result binding differs")
	}
	if (checkpoint && result.Status != "CHECKPOINT") || (!checkpoint && result.Status != "PASS" && result.Status != "FAIL") {
		return fmt.Errorf("RUNTIME_ERROR: %s", result.Message)
	}
	seen := map[string]CaseExecution{}
	for _, c := range result.Cases {
		if _, ok := seen[c.CaseID]; ok {
			return fmt.Errorf("duplicate case %s", c.CaseID)
		}
		if c.Status != "PASS" && c.Status != "FAIL" && c.Status != "BLOCKED" {
			return fmt.Errorf("incomplete case %s", c.CaseID)
		}
		if c.Procedure == "" || c.Output == "" || c.Comparison == "" || c.Tool == "" || c.Environment == "" || (c.Workspace == "" && c.Status != "BLOCKED") {
			return fmt.Errorf("case %s needs actual procedure/output/oracle, tool/environment and isolated workspace", c.CaseID)
		}
		if c.Status != "BLOCKED" && (strings.TrimSpace(c.Output) == "符合预期" || strings.TrimSpace(c.Comparison) == "符合预期") {
			return fmt.Errorf("case %s contains template evidence instead of actual observation and comparison", c.CaseID)
		}
		seen[c.CaseID] = c
	}
	actual := sortedCaseKeys(seen)
	expected := append([]string{}, actual...)
	expected = append(expected, result.Remaining...)
	if !equalStrings(expected, t.Expected) {
		return fmt.Errorf("expected/actual case set mismatch")
	}
	if !checkpoint && len(result.Remaining) > 0 {
		return fmt.Errorf("final result has unfinished cases")
	}
	if checkpoint && result.Message == "" {
		return fmt.Errorf("checkpoint needs interruption reason")
	}
	// Previously checkpointed rows retain their exact observations. New executable
	// rows require the current action's real prepare/target/collect receipts;
	// a model's prose cannot convert an invocation error into a product failure.
	checkpointRows := w.Checkpoints[t.Target].Cases
	for _, c := range result.Cases {
		retained := false
		for _, old := range checkpointRows {
			if digestOfCanonical(old) == digestOfCanonical(c) {
				retained = true
			}
		}
		if retained || c.Status == "BLOCKED" {
			continue
		}
		for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
			op := findTaskOperation(x, kind, c.CaseID)
			if op == nil || op.Receipt == nil || op.Receipt.Status != "COMPLETED" || op.Receipt.ExitCode < 0 {
				return fmt.Errorf("RUNTIME_ERROR: case %s requires completed %s operation", c.CaseID, kind)
			}

			if kind == "qa.target" && c.Status == "PASS" && op.Command != nil && op.Receipt.ExitCode != op.Command.ExpectedExitCode {
				return fmt.Errorf("case %s PASS conflicts with frozen expected exit %d (actual %d)", c.CaseID, op.Command.ExpectedExitCode, op.Receipt.ExitCode)
			}
			if op.Receipt.Workspace != c.Workspace {
				return fmt.Errorf("case %s workspace differs from actual operation", c.CaseID)
			}
		}
	}
	for _, c := range result.Cases {
		for _, dep := range c.Dependencies {
			d, ok := seen[dep]
			if !ok {
				d, ok = t.Inherited[dep]
			}
			if !ok {
				return fmt.Errorf("unknown dependency %s", dep)
			}
			if d.Status != "PASS" && c.Status != "BLOCKED" {
				return fmt.Errorf("failed dependency must block its dependent case")
			}
		}
		if c.Status == "BLOCKED" {
			blocked := false
			for _, dep := range c.Dependencies {
				if d, ok := seen[dep]; ok && d.Status != "PASS" {
					blocked = true
				}
			}
			if !blocked {
				return fmt.Errorf("blocked case needs a failed dependency")
			}
		}
	}
	old, hasCheckpoint := w.Checkpoints[t.Target]
	invalid := map[string]CheckpointInvalidation{}
	oldCases := map[string]CaseExecution{}
	for _, c := range old.Cases {
		oldCases[c.CaseID] = c
	}
	for _, change := range result.Invalidated {
		prior, ok := oldCases[change.CaseID]
		if !hasCheckpoint || !ok || change.Reason == "" || change.Tool == "" || change.Environment == "" || change.PreviousTool != prior.Tool || change.PreviousEnvironment != prior.Environment || (change.Tool == prior.Tool && change.Environment == prior.Environment) {
			return fmt.Errorf("checkpoint invalidation requires the exact prior case, changed tool/environment and reason")
		}
		if _, duplicate := invalid[change.CaseID]; duplicate {
			return fmt.Errorf("duplicate checkpoint invalidation")
		}
		if now, ok := seen[change.CaseID]; ok && (now.Tool != change.Tool || now.Environment != change.Environment) {
			return fmt.Errorf("rerun differs from checkpoint invalidation tool/environment")
		}
		invalid[change.CaseID] = change
	}
	for _, c := range old.Cases {
		if _, changed := invalid[c.CaseID]; changed {
			continue
		}
		now, ok := seen[c.CaseID]
		if !ok || digestOfCanonical(c) != digestOfCanonical(now) {
			return fmt.Errorf("checkpoint completed evidence cannot be dropped or changed without a precise invalidation")
		}
		for _, dep := range c.Dependencies {
			if _, changed := invalid[dep]; changed {
				return fmt.Errorf("checkpoint dependent %s must be invalidated with changed prerequisite %s", c.CaseID, dep)
			}
		}
	}
	failures := map[string]bool{}
	for _, c := range result.Cases {
		if c.Status == "FAIL" {
			failures[c.CaseID] = true
		}
	}
	findings := map[string]bool{}
	for _, f := range result.Findings {
		if f.ID == "" || findings[f.ID] || !validSeverity(f.Severity) || f.Problem == "" || f.NormalPath == "" || f.Consequence == "" || f.SeverityRationale == "" || f.Remedy == "" {
			return fmt.Errorf("finding needs unique ID and complete normal-use evidence")
		}
		findings[f.ID] = true
		delete(failures, f.Requirement)
	}
	if !checkpoint {
		if len(failures) > 0 {
			return fmt.Errorf("each failed case needs its own raw finding with requirement=caseId")
		}
		if result.Status == "PASS" {
			for _, c := range result.Cases {
				if c.Status != "PASS" {
					return fmt.Errorf("PASS contains a failed or blocked case")
				}
			}
		}
		if result.Status == "FAIL" && len(result.Findings) == 0 {
			return fmt.Errorf("FAIL requires findings")
		}
	}
	return nil
}

func adjudicateWave(r *RegularState, a WaveAdjudication, revision uint64) error {
	w := currentWave(r)
	if w == nil || !w.Complete || w.Invalidated != "" || a.WaveID != w.ID {
		return fmt.Errorf("wave must be complete and current before adjudication")
	}
	result, ok := w.Results[a.Target]
	if !ok || result.Digest != a.ResultDigest {
		return fmt.Errorf("adjudication requires exact raw result digest")
	}
	previous := lastWaveAdjudication(r, w.ID, a.Target)
	if previous != nil && a.ReplacesRevision != previous.Revision {
		return fmt.Errorf("correction must bind the preceding adjudication revision")
	}
	if previous == nil && a.ReplacesRevision != 0 {
		return fmt.Errorf("no adjudication to correct")
	}
	known := map[string]bool{}
	for _, f := range result.Findings {
		known[f.ID] = true
	}
	if len(a.Decisions) != len(known) {
		return fmt.Errorf("adjudication must cover every raw finding exactly once")
	}
	for _, d := range a.Decisions {
		if !known[d.FindingID] || !validSeverity(d.Severity) || d.Reason == "" || d.Evidence == "" {
			return fmt.Errorf("finding needs an individual severity rationale and verified normal-use evidence")
		}
		delete(known, d.FindingID)
		if d.Decision != "NEW" && d.Decision != "SAME_AS" && d.Decision != "DISMISSED" {
			return fmt.Errorf("decision must be NEW, SAME_AS or DISMISSED")
		}
		if d.Decision != "DISMISSED" {
			if d.ProblemID == "" {
				return fmt.Errorf("accepted finding requires stable problem ID")
			}
			if d.Decision == "SAME_AS" {
				if _, ok := r.Verification.Problems[d.ProblemID]; !ok {
					return fmt.Errorf("SAME_AS must reference an existing canonical problem")
				}
			}
			for _, id := range d.AffectedCases {
				found := false
				for _, mode := range r.Route.QA {
					for _, c := range allCases(&w.Candidate, mode) {
						if c == id {
							found = true
						}
					}
				}
				if !found {
					return fmt.Errorf("unknown affected case %s", id)
				}
			}
		}
		for _, f := range result.Findings {
			if f.ID == d.FindingID && f.Category == "out-of-scope" && d.Severity != "P3" {
				return fmt.Errorf("out-of-scope finding is advisory P3")
			}
		}
	}
	a.Revision = revision + 1
	r.Verification.Adjudications = append(r.Verification.Adjudications, a)
	// Recompute membership from latest decisions, keeping stable problem records.
	// Resolved problems remain resolved; a changed merge never silently reopens
	// them or erases the previous decision's evidence.
	for id, p := range r.Verification.Problems {
		p.Findings = nil
		p.AffectedCases = nil
		r.Verification.Problems[id] = p
	}
	latest := map[string]WaveAdjudication{}
	for _, d := range r.Verification.Adjudications {
		latest[d.WaveID+":"+d.Target] = d
	}
	for _, d := range latest {
		for _, f := range d.Decisions {
			if f.Decision == "DISMISSED" {
				continue
			}
			p, exists := r.Verification.Problems[f.ProblemID]
			if !exists {
				p = CanonicalProblem{ID: f.ProblemID, Severity: f.Severity, Status: "OPEN", Evidence: f.Evidence}
			}
			// Membership was cleared above, so the first effective finding resets
			// severity too. Historical severity must not survive a corrected decision.
			if len(p.Findings) == 0 || f.Severity < p.Severity {
				p.Severity = f.Severity
			}
			p.Findings = uniqueAppend(p.Findings, d.WaveID+":"+d.Target+":"+f.FindingID)
			p.AffectedCases = uniqueAppend(p.AffectedCases, f.AffectedCases...)
			r.Verification.Problems[p.ID] = p
		}
	}
	for id, p := range r.Verification.Problems {
		p.Findings = sortedStrings(p.Findings)
		p.AffectedCases = sortedStrings(p.AffectedCases)
		r.Verification.Problems[id] = p
		if len(p.Findings) == 0 && p.Status == "OPEN" {
			p.Status = "DISMISSED"
			r.Verification.Problems[id] = p
		}
	}
	if p := currentRepair(r); p != nil && p.Result == nil {
		dispatched := false // If already issued, new obligations belong to its successor.
		// The immutable plan is also the TaskKey binding. Never change it after a
		// worker could have observed it; the caller supplies that fact below.
		dispatched = p.Issued
		if !dispatched {
			members := []string{}
			cases := []string{}
			pure := true
			for id, problem := range r.Verification.Problems {
				if problem.Status == "OPEN" && problem.Severity != "P3" {
					members = append(members, id)
					cases = uniqueAppend(cases, problem.AffectedCases...)
					pure = pure && problem.Severity == "P2"
				}
			}
			p.Problems = sortedStrings(members)
			p.AffectedCases = sortedStrings(cases)
			p.PureP2 = pure
		}
	}
	return nil
}

func (e *Engine) acceptRepair(s *State, result RepairResult, revision uint64) (string, error) {
	r := s.Regular
	x, ok := s.RegularExecutions[result.ActionID]
	if !ok || !x.Claimed || x.Finished || x.Claim.Task.Step != "repair-worker" {
		return "", fmt.Errorf("UNKNOWN_ACTION: repair needs a live claimed attempt")
	}
	if result.AttemptID != x.Claim.AttemptID || result.Identity != x.Identity {
		return "", fmt.Errorf("INTENT_MISMATCH: repair native attempt")
	}
	if w := currentWave(r); w != nil {
		merged, err := mergeVerificationDelta(w.Checkpoints["repair-worker"], VerificationResult{Cases: result.Cases})
		if err != nil {
			return "", err
		}
		result.Cases = merged.Cases
	}
	x.Finished = true
	x.RepairResult = &result
	s.RegularExecutions[result.ActionID] = x
	delete(s.PendingActions, result.ActionID)
	s.Tasks[x.Claim.Task] = runtime.TaskTerminal
	reject := func(reason string) (string, error) {
		at := s.Attempts[x.Claim.Task]
		at.RetryExhausted = x.Number >= preparationMax(r, x.Claim.Task)
		s.Attempts[x.Claim.Task] = at
		r.RepairErrors = append(r.RepairErrors, RepairError{Result: result, Reason: reason, Revision: revision + 1})
		return "REJECTED_RESULT", nil
	}
	p := currentRepair(r)
	if p == nil || r.ChangePending || !currentVerificationClaim(r, x.Claim) {
		return reject("QUARANTINED: obsolete repair")
	}
	if result.Provider != x.Claim.Provider || result.RequirementRevision != x.Claim.RequirementRevision || result.Candidate != p.Candidate || result.PlanID != p.ID || result.Status != "PASS" {
		return reject("repair failed or returned an invalid binding: " + result.Message)
	}
	if !equalStrings(result.Resolved, p.Problems) {
		return reject("repair must report every planned canonical problem")
	}
	if _, err := gitprep.Paths(result.Manifest); err != nil {
		return reject(err.Error())
	}
	for _, check := range []BatchValidation{result.Build, result.ProjectTests} {
		if check.Result != "PASS" || check.Procedure == "" || check.Observation == "" {
			return reject("repair needs actual successful build and full project tests")
		}
	}
	actual := []string{}
	for _, c := range result.Cases {
		if c.Status != "PASS" || c.Procedure == "" || c.Output == "" || c.Comparison == "" {
			return reject("affected self-test needs complete PASS evidence")
		}
		actual = append(actual, c.CaseID)
	}
	if !equalStrings(actual, p.AffectedCases) {
		return reject("repair must self-run each fixed affected case")
	}
	// Frozen test/case artifacts are changed only through the existing explicit
	// QA revision route, which invalidates the candidate and schedules fresh review.
	w := currentWave(r)
	for _, path := range result.Manifest {
		for artifact := range w.Candidate.Artifacts {
			if path == artifact {
				return reject("QA_ARTIFACT_REPAIR: request an explicit QA revision before changing frozen artifacts")
			}
		}
	}
	p.Result = &result
	s.Results[result.ActionID] = WorkerResult{ActionID: result.ActionID, Provider: result.Provider, Outcome: "PASS", Digest: digestOfCanonical(result)}
	return "ACCEPTED", nil
}

type RepairError struct {
	Result   RepairResult `json:"result"`
	Reason   string       `json:"reason"`
	Revision uint64       `json:"revision"`
}

func verificationReadyToPromote(r *RegularState) bool {
	w := currentWave(r)
	if w == nil || !w.Complete || w.Invalidated != "" || !waveAdjudicated(r, w) || r.ChangePending || r.Ask != nil {
		return false
	}
	if p := currentRepair(r); p != nil && p.Committed == "" {
		return false
	}
	for _, p := range r.Verification.Problems {
		if p.Status == "OPEN" && p.Severity != "P3" {
			return false
		}
	}
	candidate := r.Preparation.Candidate.Candidate.Identity
	for _, name := range append(append([]string{}, r.Route.QA...), r.Route.Gates...) {
		ev, ok := r.Verification.Effective[name]
		if !ok || ev.Candidate != candidate || ev.RequirementRevision != r.Requirement.Revision {
			if waivedVerificationTarget(r, name, candidate) {
				continue
			}
			return false
		}
	}
	return true
}
func desiredVerificationGit(r *RegularState) *GitPreparation {
	w := currentWave(r)
	if w == nil || w.Invalidated != "" {
		return nil
	}
	p := r.Preparation
	op := GitPreparation{BindingDigest: p.BindingDigest, Before: w.Candidate.Candidate.Identity}
	if repair := currentRepair(r); repair != nil && repair.Committed == "" {
		op.Target = repair.ID
		if p.Resources[repair.ID] == "" {
			op.Kind = "repair-workspace"
		} else if repair.Result != nil {
			op.Kind = "repair-commit"
			op.Paths = repair.Result.Manifest
		}
	}
	if op.Kind == "" && verificationReadyToPromote(r) {
		op.Kind = "promote"
		op.Before = p.Candidate.Candidate.Identity
		op.Target = r.Candidate
	}
	if op.Kind == "" {
		return nil
	}
	op.ID = "git:" + digestOfCanonical(op)
	return &op
}
func acceptVerificationGit(s *State, receipt GitPreparationReceipt) error {
	r := s.Regular
	p := r.Preparation
	v := r.Verification
	op := receipt.Operation
	w := currentWave(r)
	switch op.Kind {
	case "repair-workspace":
		if p.Resources[op.Target] == "" {
			return fmt.Errorf("repair workspace receipt is missing")
		}
	case "repair-commit":
		repair := currentRepair(r)
		if repair == nil || repair.ID != op.Target || repair.Result == nil {
			return fmt.Errorf("repair commit has no completed plan")
		}
		next := *p.Candidate
		next.Candidate = coverage.ValidationCandidate{Identity: receipt.Git.After, Digest: receipt.Git.View.Digest}
		next.View = receipt.Git.View
		next.Coverage.Candidate = &next.Candidate
		p.Candidate = &next
		repair.Committed = receipt.Git.After
		for _, id := range repair.Problems {
			problem := v.Problems[id]
			problem.Status = "RESOLVED"
			v.Problems[id] = problem
		}
		if repair.PureP2 {
			for name, ev := range v.Effective {
				v.Inheritance = append(v.Inheritance, ValidationInheritance{Kind: "P2_CARRY", Source: ev, Candidate: next.Candidate.Identity, RequirementRevision: r.Requirement.Revision, StateRevision: op.Revision, Reason: repair.ID})
				ev.Candidate = next.Candidate.Identity
				v.Effective[name] = ev
			}
			// A pure P2 carry is not a new review/wave. Keep the original comparison
			// and evidence, while promotion checks the new independently committed tree.
		} else {
			freezeWave(r, &next, repair.AffectedCases)
		}
	case "promote":
		if !verificationReadyToPromote(r) || receipt.Promotion == nil {
			return fmt.Errorf("promotion requires complete current evidence and receipt")
		}
		if err := gitprep.ValidatePromotion(*receipt.Promotion, p.Candidate.View, op.Target, op.ID); err != nil {
			return err
		}
		v.Promotion = receipt.Promotion
		r.Candidate = receipt.Promotion.Target.Identity
		seal := &SealBinding{Waivers: append([]WaveWaiver{}, v.Waivers...), Candidate: r.Candidate, ValidatedDigest: p.Candidate.View.Digest, RequirementRevision: r.Requirement.Revision, WaveID: w.ID, Evidence: map[string]ValidationEvidence{}, ExternalArtifacts: p.Candidate.ExternalArtifacts}
		for name, ev := range v.Effective {
			seal.Evidence[name] = ev
		}
		for _, problem := range v.Problems {
			if problem.Status == "OPEN" && problem.Severity == "P3" {
				seal.Advisory = append(seal.Advisory, problem)
			}
		}
		sort.Slice(seal.Advisory, func(i, j int) bool { return seal.Advisory[i].ID < seal.Advisory[j].ID })
		v.Seal = seal
		r.Terminal = "SEALED"
	}
	return nil
}
func verificationRequest(s *State, q RegularRequest) error {
	r := s.Regular
	v := r.Verification
	if v == nil {
		return fmt.Errorf("no verification obligations")
	}
	switch q.Action {
	case "verification-retry":
		for key, at := range s.Attempts {
			if key.String() == q.Target && verificationStep(key.Step) && at.RetryExhausted {
				return nil
			}
		}
		return fmt.Errorf("retry must target an exhausted verification TaskKey")
	case "authorize-wave":
		w := currentWave(r)
		if w == nil || !w.Complete || !waveAdjudicated(r, w) || v.Completed < v.Limit {
			return fmt.Errorf("extra wave requires settled findings and exhausted semantic allowance")
		}
	default:
		return fmt.Errorf("unknown verification control")
	}
	return nil
}
func verificationDecision(s *State, q RegularRequest) {
	v := s.Regular.Verification
	if q.Action == "authorize-wave" {
		v.Limit++
	} else if q.Action == "verification-retry" {
		v.ExtraAttempts[q.Target]++
		for key, at := range s.Attempts {
			if key.String() == q.Target {
				at.RetryExhausted = false
				s.Attempts[key] = at
			}
		}
	}
}
func invalidateVerification(s *State, reason string) {
	r := s.Regular
	if w := currentWave(r); w != nil && w.Invalidated == "" {
		w.Invalidated = reason
	}
	if r.Preparation != nil && r.Preparation.Operation != nil {
		op := r.Preparation.Operation
		if op.Kind != "" {
			delete(s.PendingHostActions, op.ID)
			r.Preparation.Operation = nil
		}
	}
}

// Verification impact is a projection of explicit classifications. It never
// turns a failed or unfinished run into PASS. The source evidence remains at
// its actual requirement/candidate; only this authorization layer is rebound.
func verificationKeep(r *RegularState, name string) bool {
	if strings.HasSuffix(name, "-review") {
		mode := strings.TrimSuffix(name, "-review")
		return r.Preparation != nil && r.Preparation.QA[mode] != nil && r.Preparation.QA[mode].Frozen && r.Preparation.QA[mode].ReviewWaiver == nil
	}
	if r.Verification == nil {
		return false
	}
	_, ok := r.Verification.Effective[name]
	return ok
}
func classifyVerificationChange(r *RegularState, q RegularRequest, revision uint64) {
	v := r.Verification
	if v == nil {
		return
	}
	targetReq := r.Requirement.Revision
	if q.Document != nil {
		targetReq = q.Document.Revision
	}
	selected := map[string]bool{}
	for _, name := range q.Keep {
		selected[name] = true
	}
	for name, ev := range v.Effective {
		if selected[name] || q.Impact[name] == "unaffected" {
			kind := "IMPACT_CARRY"
			if selected[name] {
				kind = "USER_KEEP"
			}
			v.Inheritance = append(v.Inheritance, ValidationInheritance{Kind: kind, Source: ev, Candidate: ev.Candidate, RequirementRevision: targetReq, StateRevision: revision, Reason: q.Reason})
			ev.RequirementRevision = targetReq
			if selected[name] {
				ev.Scope = coverage.ScopeFull
			}
			v.Effective[name] = ev
		} else {
			delete(v.Effective, name)
		}
	}
}
func bindVerificationInheritance(r *RegularState, candidate *PreparedCandidate) {
	v := r.Verification
	if v == nil {
		return
	}
	for i := range v.Inheritance {
		in := &v.Inheritance[i]
		if in.RequirementRevision != r.Requirement.Revision {
			continue
		}
		if in.Candidate != candidate.Candidate.Identity {
			current, ok := v.Effective[in.Source.Target]
			if !ok || current.Candidate != in.Candidate || current.RequirementRevision != in.RequirementRevision {
				continue
			}
			receipt := requirementAssemblyReceipt(r, *in, candidate)
			if receipt == nil {
				continue
			}
			// The original authorization remains immutable and complete. The
			// separately observed artifact-only assembly gets its own exact-Q
			// carry receipt, never a blank target filled after confirmation.
			carry := ValidationInheritance{Kind: "IMPACT_CARRY", Source: current, Candidate: candidate.Candidate.Identity, RequirementRevision: in.RequirementRevision, StateRevision: receipt.Operation.Revision, Reason: "requirement artifact assembly: " + receipt.Operation.ID}
			current.Candidate = candidate.Candidate.Identity
			v.Effective[current.Target] = current
			v.Inheritance = append(v.Inheritance, carry)
			continue
		}
		// Existing evidence covers only its own finite cases. A newly introduced
		// obligation with no original PASS still goes through design and execution.
		expected := allCases(candidate, in.Source.Target)
		actual := []string{}
		for _, c := range in.Source.Cases {
			actual = append(actual, c.CaseID)
		}
		if !equalStrings(expected, actual) {
			continue
		}
		ev := in.Source
		ev.Candidate = in.Candidate
		ev.RequirementRevision = in.RequirementRevision
		if in.Kind == "USER_KEEP" {
			ev.Scope = coverage.ScopeFull
		}
		v.Effective[ev.Target] = ev
	}
}

// Only the first artifact assembly of this exact confirmed requirement may
// carry its retained PASS. Native tree evidence and unchanged case semantics are
// required; a later repair or unrelated candidate is not this authorization.
func requirementAssemblyReceipt(r *RegularState, in ValidationInheritance, candidate *PreparedCandidate) *GitPreparationReceipt {
	if len(r.Changes) == 0 || r.Preparation == nil {
		return nil
	}
	change := r.Changes[len(r.Changes)-1]
	if change.StateRevision != in.StateRevision || change.ToRevision != in.RequirementRevision {
		return nil
	}
	var original *PreparedCandidate
	for i := range r.Verification.Waves {
		w := &r.Verification.Waves[i]
		if w.Candidate.Candidate.Identity == in.Candidate {
			original = &w.Candidate
			break
		}
	}
	if original == nil {
		return nil
	}
	ignored := map[string]bool{r.Requirement.Path: true}
	for path := range original.Artifacts {
		ignored[path] = true
	}
	for path := range candidate.Artifacts {
		ignored[path] = true
	}
	implementation := func(view gitprep.View) []gitprep.Entry {
		entries := []gitprep.Entry{}
		for _, entry := range view.Entries {
			if !ignored[entry.Path] {
				entries = append(entries, entry)
			}
		}
		return entries
	}
	if !reflect.DeepEqual(implementation(original.View), implementation(candidate.View)) {
		return nil
	}
	oldManifests := append([]coverage.AcceptanceManifest(nil), original.Coverage.Manifests...)
	newManifests := append([]coverage.AcceptanceManifest(nil), candidate.Coverage.Manifests...)
	for i := range oldManifests {
		oldManifests[i].Binding = coverage.ManifestBinding{}
	}
	for i := range newManifests {
		newManifests[i].Binding = coverage.ManifestBinding{}
	}
	if !reflect.DeepEqual(oldManifests, newManifests) {
		return nil
	}
	for i := range r.Preparation.Receipts {
		receipt := &r.Preparation.Receipts[i]
		if receipt.Operation.Kind == "finalize" && receipt.Git.After == candidate.Candidate.Identity && receipt.Git.View.Digest == candidate.View.Digest {
			return receipt
		}
	}
	return nil
}

func developmentHasStarted(s *State) bool {
	for _, x := range s.RegularExecutions {
		if x.Claim.Task.Step == "development-worker" {
			return true
		}
	}
	return false
}
func validateRouteChangeAfterDevelopment(s *State, q RegularRequest) error {
	if !developmentHasStarted(s) {
		return nil
	}
	if q.Action != "route-add" || q.Route == nil || q.Route.Kind != "custom" {
		return fmt.Errorf("after development begins only adding ordinary gates is supported; existing QA/gate obligations cannot be removed")
	}
	for _, mode := range q.Route.QA {
		if s.Regular.Route == nil || !slices.Contains(s.Regular.Route.QA, mode) {
			return fmt.Errorf("after development begins adding QA is unsupported")
		}
	}
	return nil
}
func rebindAddedGates(s *State, oldBinding string) {
	r := s.Regular
	p := r.Preparation
	if p == nil || !developmentHasStarted(s) {
		return
	}
	if w := currentWave(r); w != nil && w.Invalidated == "" {
		w.Invalidated = "route targets added"
	}
	p.BindingDigest = preparationBinding(r)
	if p.Operation != nil {
		delete(s.PendingHostActions, p.Operation.ID)
		p.Operation = nil
	}
	if p.Candidate != nil {
		p.Candidate.BindingDigest = p.BindingDigest
	}
	// Already finished execution evidence remains traceable and reusable. Live
	// old claims are stale, and their late results are quarantined by binding.
	for key, x := range s.RegularExecutions {
		if preparationStep(x.Claim.Task.Step) && !x.Finished && !x.Claimed && x.Claim.PrerequisiteDigest == oldBinding {
			x.Finished = true
			s.RegularExecutions[key] = x
			delete(s.PendingActions, key)
			s.Tasks[x.Claim.Task] = runtime.TaskTerminal
		}
	}
}

func reconcileDismissedQA(s *State, a WaveAdjudication) {
	r := s.Regular
	w := currentWave(r)
	t := waveTarget(w, a.Target)
	result := w.Results[a.Target]
	if t == nil || len(t.Expected) == 0 || result.Status != "FAIL" {
		return
	}
	for _, d := range a.Decisions {
		if d.Decision != "DISMISSED" {
			return
		}
	}
	// A rejected execution diagnosis is not execution PASS. Retry the same finite
	// TaskKey to obtain a corrected run; keep the rejected raw evidence in the
	// original RegularExecution and preserve the Operator's correction history.
	delete(w.Results, a.Target)
	delete(w.Checkpoints, a.Target)
	if w.Counted {
		r.Verification.Completed--
		w.Counted = false
	}
	w.Complete = false
	x := s.RegularExecutions[result.ActionID]
	at := s.Attempts[t.Task]
	at.RetryExhausted = x.Number >= preparationMax(r, t.Task)
	s.Attempts[t.Task] = at
}

// Missing/failed execution evidence can be waived only by an explicit disposition
// of every concrete blocking finding for this target, requirement and candidate.
// No ValidationEvidence is fabricated; Seal carries the waiver separately.
func waivedVerificationTarget(r *RegularState, target, candidate string) bool {
	waived := map[string]bool{}
	for _, waiver := range r.Verification.Waivers {
		if waiver.Candidate == candidate && waiver.RequirementRevision == r.Requirement.Revision {
			for _, id := range waiver.ProblemIDs {
				waived[id] = true
			}
		}
	}
	for i := len(r.Verification.Waves) - 1; i >= 0; i-- {
		w := &r.Verification.Waves[i]
		if w.Candidate.Candidate.Identity != candidate || w.RequirementRevision != r.Requirement.Revision {
			continue
		}
		result, ok := w.Results[target]
		if !ok {
			continue
		}
		a := lastWaveAdjudication(r, w.ID, target)
		if a == nil || len(result.Findings) == 0 {
			return false
		}
		found := false
		for _, d := range a.Decisions {
			if d.Decision == "DISMISSED" {
				continue
			}
			if !waived[d.ProblemID] {
				return false
			}
			found = true
		}
		return found
	}
	return false
}
func openWaveProblems(r *RegularState) []string {
	ids := []string{}
	for id, p := range r.Verification.Problems {
		if p.Status == "OPEN" && p.Severity != "P3" {
			ids = append(ids, id)
		}
	}
	return sortedStrings(ids)
}
func decideWaveRecovery(s *State, a *RegularAsk, d RegularDecision, revision uint64) error {
	r := s.Regular
	v := r.Verification
	w := currentWave(r)
	if w == nil || w.ID != a.Request.Target || !equalStrings(a.Request.Keep, openWaveProblems(r)) || len(a.Request.Keep) == 0 {
		return fmt.Errorf("STALE_FRESHNESS: exact wave obligations changed")
	}
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("wave disposition requires a current nonempty reason")
	}
	switch d.Choice {
	case "additional-wave":
		v.Limit++
	case "retry-qa":
		if len(r.Route.QA) == 0 {
			return fmt.Errorf("no selected QA to retry")
		}
		v.Limit++
		for _, mode := range r.Route.QA {
			delete(v.Effective, mode)
		}
		freezeWave(r, r.Preparation.Candidate, nil, true)
	case "waiver":
		waiver := WaveWaiver{WaveID: w.ID, Candidate: w.Candidate.Candidate.Identity, RequirementRevision: r.Requirement.Revision, ProblemIDs: append([]string{}, a.Request.Keep...), Reason: d.Reason, Revision: revision}
		v.Waivers = append(v.Waivers, waiver)
		for _, id := range waiver.ProblemIDs {
			p := v.Problems[id]
			p.Status = "WAIVED"
			v.Problems[id] = p
		}
		r.Guarantee.Status = "WAIVED"
		r.Guarantee.WaiverReason = d.Reason
	case "change-requirement":
		r.ChangePending = true
		invalidateVerification(s, "wave recovery requirement change")
	default:
		return fmt.Errorf("INVALID_CHOICE")
	}
	return nil
}

func preserveCheckpoint(w *VerificationWave, target string) {
	if old, ok := w.Checkpoints[target]; ok {
		if w.CheckpointHistory == nil {
			w.CheckpointHistory = map[string][]VerificationResult{}
		}
		history := w.CheckpointHistory[target]
		if len(history) == 0 || history[len(history)-1].Digest != old.Digest {
			w.CheckpointHistory[target] = append(history, old)
		}
	}
}
