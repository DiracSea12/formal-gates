package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// CheckParameters selects an approved development check or frozen QA operation.
// Its binding and registered workspace identify the immutable input.
type CheckParameters struct {
	Repair         *RepairCheckParameters `json:"repair,omitempty"`
	QA             *QACheckParameters     `json:"qa,omitempty"`
	SourceActionID string                 `json:"sourceActionId"`
	Binding        WorkBinding            `json:"binding"`
	Check          DevelopmentCheck       `json:"check"`
	SourceIdentity string                 `json:"sourceIdentity"`
	SourceDigest   string                 `json:"sourceDigest"`
	WorkspaceID    string                 `json:"workspaceId"`
}

type RepairCheckParameters struct {
	Task          runtime.TaskKey `json:"task"`
	SourceActions []string        `json:"sourceActions"`
}

type CheckStreamProof struct {
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

type CheckEvidence struct {
	PreparationInput        string            `json:"preparationInput,omitempty"`
	PreparationArtifacts    map[string]string `json:"preparationArtifacts,omitempty"`
	PreparationSourceAction string            `json:"preparationSourceAction,omitempty"`
	ParametersDigest        string            `json:"parametersDigest"`
	SourceActionID          string            `json:"sourceActionId"`
	SourceIdentity          string            `json:"sourceIdentity"`
	SourceDigest            string            `json:"sourceDigest"`
	WorkspaceID             string            `json:"workspaceId"`
	RequestDigest           string            `json:"requestDigest"`
	Directory               string            `json:"directory"`
	ExitCode                int               `json:"exitCode"`
	Error                   string            `json:"error"`
	Stdout                  CheckStreamProof  `json:"stdout"`
	Stderr                  CheckStreamProof  `json:"stderr"`
	StdoutText              string            `json:"stdoutText,omitempty"`
	StderrText              string            `json:"stderrText,omitempty"`
	Tool                    string            `json:"tool,omitempty"`
	Environment             string            `json:"environment,omitempty"`
	Invocation              string            `json:"invocation,omitempty"`
	WorkingDirectory        string            `json:"workingDirectory,omitempty"`
	GoTestName              string            `json:"goTestName,omitempty"`
	GoResult                string            `json:"goResult,omitempty"`
}

func checkParameters(intent HostActionIntent) (CheckParameters, error) {
	var p CheckParameters
	raw, err := json.Marshal(intent.Adapter.Params)
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	return p, err
}

func (s *State) editResultCurrent(r WorkerResult) bool {
	if r.Validation == nil || r.Validation.Development == nil || r.Outcome != OutcomePass || r.Validation.Binding.VerificationSource != "" {
		return false
	}
	if _, obsolete := s.ObsoleteActions[r.ActionID]; obsolete {
		return false
	}
	b, ok := s.DispatchBindings[r.ActionID]
	if !ok || b != r.Validation.Binding {
		return false
	}
	current, _ := s.requirementWorkReplacement(b.Task)
	return current == b.Task && (s.Validation != nil && b.Generation == s.Validation.Generation && b.RequirementRevision == s.Validation.RequirementRevision || s.validationWorkCarryAuthorized(r.ActionID, b.Task))
}

func (s *State) editBatch(r WorkerResult) (DevelopmentBatch, bool) {
	b := r.Validation.Binding
	if p := r.Validation.GranularityReview; p != nil && len(p.Batches) == 1 {
		return p.Batches[0], true
	}
	// Plans remain in their confirmed decision or original repair proposal.
	for request, d := range s.Decisions {
		if p := s.decisionPlanProposal(d); p != nil {
			if b.PlanRequest != "" && b.PlanRequest != request {
				continue
			}
			for _, batch := range p.Batches {
				if batch.ID == b.BatchID {
					return batch, true
				}
			}
		}
	}
	if proposal, ok := s.Results[b.PlanRequest]; ok && proposal.Validation != nil && proposal.Validation.GranularityReview != nil {
		for _, batch := range proposal.Validation.GranularityReview.Batches {
			if batch.ID == b.BatchID {
				return batch, true
			}
		}
	}
	return DevelopmentBatch{}, false
}

func (s *State) editSourceProof(action string) (CandidateEvidence, bool) {
	r, exists := s.Results[action]
	if !exists || r.Validation == nil {
		return CandidateEvidence{}, false
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.Status != HostActionStatusExecuted || receipt.AdapterOperation != "op.candidate" {
			continue
		}
		intent, ok := s.hostIntent(receipt.ActionID)
		if !ok || intent.Adapter == nil {
			continue
		}
		if stage, known := intent.Adapter.Params["stage"].(string); known && stage != "CHECK_SOURCE" {
			continue
		}
		p, err := candidateParameters(intent)
		if err != nil || p.Stage != "CHECK_SOURCE" || !slices.Contains(p.SourceTasks, r.Validation.Binding.Task) {
			continue
		}
		if p.SourceResults[action] != r.Digest {
			continue
		}
		_, proof, err := validateCandidateReceipt(s.NativeVCS(), intent, receipt)
		if err == nil {
			return proof, true
		}
	}
	return CandidateEvidence{}, false
}

func (s *State) editSourceCurrent(p CandidateParameters) bool {
	if p.Stage != "CHECK_SOURCE" || !s.candidateContextCurrent(p) || !s.resourceUsable(p.WorkspaceID) {
		return false
	}
	var paths []string
	for _, task := range p.SourceTasks {
		r := s.developmentEditResult(task)
		if r == nil || !s.editResultCurrent(*r) || r.Validation.Binding.WorkspaceID != p.WorkspaceID {
			return false
		}
		if p.SourceResults[r.ActionID] != r.Digest {
			return false
		}
		paths = append(paths, r.Validation.Development.Paths...)
	}
	return len(p.SourceTasks) > 0 && slices.Equal(s.candidateSourcePaths(paths), p.Paths)
}

// A shared editing frontier freezes only after every writer has settled.
// Each completed edit keeps its own checks against that immutable source.
func (s *State) nextEditSource(cd *compiler.CompiledDefinition) (CandidateParameters, bool) {
	ids := make([]string, 0, len(s.Results))
	for id := range s.Results {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r := s.Results[id]
		if !s.editResultCurrent(r) {
			continue
		}
		if len(s.editChecks(r)) == 0 {
			continue
		}
		if _, frozen := s.editSourceProof(id); frozen {
			continue
		}
		workspace := s.NativeResources[r.Validation.Binding.WorkspaceID]
		if workspace.Status != "READY" || !s.resourceUsable(workspace.ID) || !s.editWorkspaceQuiescent(workspace.ID, cd) {
			continue
		}
		p := CandidateParameters{Stage: "CHECK_SOURCE", Base: workspace.Identity, WorkspaceID: workspace.ID, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest(), SourceResults: map[string]string{}}
		for _, sourceID := range ids {
			source := s.Results[sourceID]
			if !s.editResultCurrent(source) || source.Validation.Binding.WorkspaceID != workspace.ID {
				continue
			}
			if _, frozen := s.editSourceProof(sourceID); frozen {
				continue
			}
			p.SourceTasks = append(p.SourceTasks, source.Validation.Binding.Task)
			p.SourceResults[sourceID] = source.Digest
			p.Paths = append(p.Paths, source.Validation.Development.Paths...)
		}
		p.Paths = s.candidateSourcePaths(p.Paths)
		slices.SortFunc(p.SourceTasks, func(a, b runtime.TaskKey) int { return strings.Compare(a.String(), b.String()) })
		return p, true
	}
	return CandidateParameters{}, false
}

func (s *State) editWorkspaceQuiescent(workspace string, cd *compiler.CompiledDefinition) bool {
	for action := range s.PendingActions {
		if s.DispatchBindings[action].WorkspaceID == workspace {
			if result, ok := s.Results[action]; !ok || !s.editResultCurrent(result) || s.developmentContinuation(action, cd) {
				return false
			}
		}
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult && attempt.Work != nil && attempt.Work.WorkspaceID == workspace {
			return false
		}
	}
	view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
	s.projectDevelopmentTasks(&view, cd)
	for _, tasks := range view.RequiredTasks {
		for _, required := range tasks {
			if required.BlockedBy == "" && s.taskResource(required.Task, cd).ID == workspace && s.developmentEditResult(required.Task) == nil && s.TaskStatusOf(required.Task) != runtime.TaskTerminal {
				return false
			}
		}
	}
	return true
}

func (s *State) checkResource(action string, source CandidateEvidence) NativeResource {
	r := s.resource("check", source.Identity, action)
	if existing, ok := s.NativeResources[r.ID]; ok {
		return existing
	}
	return r
}

func validateCheckReceipt(intent HostActionIntent, p CheckParameters, e CheckEvidence) error {
	if p.QA != nil {
		operation, goName := p.QA.Operation, ""
		if p.QA.GoTestRef != "" {
			resolved, err := coverage.GoTestOperation(p.QA.GoTestRef)
			if err != nil {
				return err
			}
			operation, goName = resolved.Operation, resolved.GoTestName
		}
		argv := operation.Argv
		if operation.Kind == coverage.OperationShell {
			argv = coverage.ShellCommand(operation.Command)
		}
		invocation, err := json.Marshal(argv)
		if err != nil {
			return err
		}
		if e.Invocation != string(invocation) || e.WorkingDirectory != filepath.Join(p.QA.WorkspaceRoot, p.QA.Directory) || e.GoTestName != goName || (goName != "" && e.GoResult != coverage.GoTestExecutionResult([]byte(e.StdoutText), goName)) {
			return validationError("QA receipt invocation or exact Go events differ from approved inputs")
		}
		if e.Error == "" && (e.Tool == "" || e.Environment == "") || !checkTextProof(e.StdoutText, e.Stdout) || !checkTextProof(e.StderrText, e.Stderr) {
			return validationError("QA receipt lacks authentic tool, environment and stream facts")
		}
	}
	if e.ParametersDigest != intent.PayloadDigest || e.SourceActionID != p.SourceActionID || e.SourceIdentity != p.SourceIdentity || e.SourceDigest != p.SourceDigest || e.WorkspaceID != p.WorkspaceID || e.RequestDigest == "" || e.Directory == "" || e.Stdout.SHA256 == "" || e.Stderr.SHA256 == "" {
		return validationError("check receipt lacks its exact source, request and stream evidence")
	}
	return nil
}

func (s *State) checkReceipt(action, check string, source *CandidateEvidence, approvedChecks ...DevelopmentCheck) (*CheckEvidence, bool) {
	if source == nil {
		if proof, frozen := s.editSourceProof(action); frozen {
			source = &proof
		}
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		intent, ok := s.hostIntent(receipt.ActionID)
		if !ok {
			continue
		}
		if sourceAction, known := intent.Adapter.Params["sourceActionId"].(string); known && sourceAction != action {
			continue
		}
		if rawCheck, known := intent.Adapter.Params["check"].(map[string]any); known {
			if id, known := rawCheck["id"].(string); known && id != check {
				continue
			}
		}
		p, err := checkParameters(intent)
		if err != nil || p.SourceActionID != action || p.Check.ID != check {
			continue
		}
		if source != nil && (p.SourceIdentity != source.Identity || p.SourceDigest != source.Digest) {
			continue
		}
		evidence, err := s.validateNativeCheckReceipt(intent, p, receipt)
		if err != nil {
			continue
		}
		if approvedChecks == nil {
			approvedChecks = s.editChecks(s.Results[action])
		}
		if !slices.ContainsFunc(approvedChecks, func(current DevelopmentCheck) bool { return current == p.Check }) {
			continue
		}
		return &evidence, true
	}
	return nil, false
}

func (s *State) developmentChecksPassed(r WorkerResult) bool {
	if r.Validation == nil || r.Validation.Status != "PASS" {
		return false
	}
	checks := s.editChecks(r)
	if len(checks) == 0 {
		return false
	}
	source, frozen := s.editSourceProof(r.ActionID)
	if !frozen {
		return false
	}
	approvedChecks := s.editChecks(s.Results[r.ActionID])
	for _, check := range checks {
		e, done := s.checkReceipt(r.ActionID, check.ID, &source, approvedChecks...)
		if !done || e.ExitCode != 0 || e.Error != "" || e.SourceIdentity != source.Identity || e.SourceDigest != source.Digest {
			return false
		}
	}
	return true
}

func (s *State) developmentChecksFailed(r WorkerResult) bool {
	source, frozen := s.editSourceProof(r.ActionID)
	if !frozen {
		return false
	}
	for _, check := range s.editChecks(r) {
		if evidence, done := s.checkReceipt(r.ActionID, check.ID, &source); done && (evidence.ExitCode != 0 || evidence.Error != "") {
			return true
		}
	}
	return false
}

// Unfinished development retains its original dispatch. Approved check
// failures permit another edit against the same obligation, never a check replay.
func (s *State) developmentContinuation(action string, cd *compiler.CompiledDefinition) bool {
	result, ok := s.Results[action]
	pending, live := s.PendingActions[action]
	if !ok || !live || !s.editResultCurrent(result) {
		return false
	}
	role := s.validationRole(pending.Task, cd)
	return (role == "engine.development.batch.v6" || role == "engine.repair.v7") && (result.Validation.Status == "CONTINUE" || s.developmentChecksFailed(result))
}

func (s *State) nextCheckParameters(action string) (CheckParameters, bool) {
	r, ok := s.Results[action]
	if !ok || !s.editResultCurrent(r) || r.Validation.Status != "PASS" {
		return CheckParameters{}, false
	}
	checks := s.editChecks(r)
	source, frozen := s.editSourceProof(action)
	if len(checks) == 0 || !frozen {
		return CheckParameters{}, false
	}
	workspace := s.checkResource(action, source)
	if workspace.Status != "READY" || !s.resourceUsable(workspace.ID) {
		return CheckParameters{}, false
	}
	for _, check := range checks {
		if e, done := s.checkReceipt(action, check.ID, &source); done {
			if e.ExitCode != 0 || e.Error != "" {
				return CheckParameters{}, false
			}
			continue
		}
		return CheckParameters{SourceActionID: action, Binding: r.Validation.Binding, Check: check, SourceIdentity: source.Identity, SourceDigest: source.Digest, WorkspaceID: workspace.ID}, true
	}
	return CheckParameters{}, false
}

func (s *State) checkInputsCurrent(intent HostActionIntent, definitions ...*compiler.CompiledDefinition) bool {
	p, err := checkParameters(intent)
	if err != nil {
		return false
	}
	if p.Repair != nil {
		return len(definitions) > 0 && s.repairCheckInputsCurrent(p, definitions[0])
	}
	if p.QA != nil {
		return s.qaCheckInputsCurrent(p)
	}
	r, ok := s.Results[p.SourceActionID]
	if !ok || !s.editResultCurrent(r) || p.Binding != r.Validation.Binding {
		return false
	}
	checks := s.editChecks(r)
	if len(checks) == 0 {
		return false
	}
	source, ok := s.editSourceProof(p.SourceActionID)
	if !ok || source.Identity != p.SourceIdentity || source.Digest != p.SourceDigest || s.checkResource(p.SourceActionID, source).ID != p.WorkspaceID {
		return false
	}
	for _, check := range checks {
		if check.ID == p.Check.ID {
			return check == p.Check
		}
	}
	return false
}

func (e *Engine) advanceDevelopmentChecks(s *State, revision uint64) (bool, error) {
	ids := make([]string, 0, len(s.Results))
	for id := range s.Results {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, action := range ids {
		r := s.Results[action]
		if !s.editResultCurrent(r) {
			continue
		}
		if len(s.editChecks(r)) == 0 {
			continue
		}
		if role := s.validationRole(r.Validation.Binding.Task, e.cfg.Definition); s.developmentChecksPassed(r) && (role == "engine.development.batch.v6" || role == "engine.repair.v7") {
			if _, outstanding := s.PendingActions[action]; outstanding {
				if err := s.terminalizeTask(r.Validation.Binding.Task); err != nil {
					return false, err
				}
				return true, nil
			}
		}
		source, frozen := s.editSourceProof(action)
		if !frozen {
			continue
		}
		workspace := s.checkResource(action, source)
		if workspace.Status != "READY" {
			return e.ensureWorkspace(s, workspace, revision)
		}
		p, ready := s.nextCheckParameters(action)
		if !ready {
			continue
		}
		correlation := "check:" + digestOfCanonical(p)
		if s.latestNativeIntent(correlation).ActionID != "" {
			continue
		}
		intent, err := e.prepareAdapterIntent(s, revision, "op.check", p, "")
		if err != nil {
			return false, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	return false, nil
}

func (s *State) developmentCheckFailure(definitions ...*compiler.CompiledDefinition) *decision.Fact {
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		intent, ok := s.hostIntent(receipt.ActionID)
		if !ok {
			continue
		}
		p, err := checkParameters(intent)
		if err != nil || p.QA != nil || p.Repair != nil {
			continue
		}
		var e CheckEvidence
		if decodeAdapterValues(receipt, &e) != nil || e.ExitCode == 0 && e.Error == "" || !s.checkInputsCurrent(intent, definitions...) {
			continue
		}
		if len(definitions) > 0 && s.developmentContinuation(p.SourceActionID, definitions[0]) {
			continue
		}
		return &decision.Fact{Source: decision.SourceReceipt, Key: receipt.ActionID, Value: "approved development check failed: " + strings.TrimSpace(e.Error)}
	}
	return nil
}

func checkTextProof(text string, proof CheckStreamProof) bool {
	sum := sha256.Sum256([]byte(text))
	return proof.Size == len(text) && proof.SHA256 == hex.EncodeToString(sum[:])
}

func (s *State) editChecks(r WorkerResult) []DevelopmentCheck {
	var checks []DevelopmentCheck
	if batch, ok := s.editBatch(r); ok {
		commands := []string(nil)
		if r.Validation.Development != nil {
			commands = r.Validation.Development.Commands
		}
		checks, _ = BindDevelopmentChecks(batch, commands)
	}
	if r.Validation.Binding.Task.Step == "development.repair" {
		for _, check := range s.fullProjectChecks() {
			if !slices.ContainsFunc(checks, func(existing DevelopmentCheck) bool {
				return existing.Purpose == check.Purpose && existing.Command == check.Command
			}) {
				checks = append(checks, check)
			}
		}
	}
	return checks
}

// Reused preparation retains the original execution values. The new receipt
// names that original action; it never rewrites its request or stream identity.
func (s *State) validateNativeCheckReceipt(intent HostActionIntent, p CheckParameters, receipt HostActionReceipt) (CheckEvidence, error) {
	var evidence CheckEvidence
	if err := decodeAdapterValues(receipt, &evidence); err != nil {
		return evidence, err
	}
	if evidence.PreparationSourceAction == "" {
		return evidence, validateCheckReceipt(intent, p, evidence)
	}
	original, ok := s.hostIntent(evidence.PreparationSourceAction)
	if !ok || original.ActionID == intent.ActionID {
		return evidence, validationError("shared preparation has no distinct original execution")
	}
	recorded, ok := s.HostActionReceipts[original.ActionID]
	if !ok || recorded.Status != HostActionStatusExecuted {
		return evidence, validationError("shared preparation original action has no execution receipt")
	}
	var actual CheckEvidence
	if err := decodeAdapterValues(recorded, &actual); err != nil {
		return evidence, err
	}
	if actual.PreparationSourceAction != "" || actual.PreparationInput == "" || actual.Error != "" {
		return evidence, validationError("shared preparation source is not an original complete execution")
	}
	prior, err := checkParameters(original)
	if err != nil {
		return evidence, err
	}
	if err := validateCheckReceipt(original, prior, actual); err != nil {
		return evidence, err
	}
	if p.QA == nil || prior.QA == nil || p.QA.Preparation == nil || prior.QA.Preparation == nil || p.QA.Preparation.Independent || prior.QA.Preparation.Independent {
		return evidence, validationError("only approved common preparation can reuse an execution")
	}
	if !commonQAPreparation(p, prior) || p.QA.PreparationOrdinal != prior.QA.PreparationOrdinal || digestOfCanonical(p.QA.Operation) != digestOfCanonical(prior.QA.Operation) {
		return evidence, validationError("shared preparation original inputs differ")
	}
	shared := evidence
	shared.PreparationSourceAction = ""
	if digestOfCanonical(shared) != digestOfCanonical(actual) {
		return evidence, validationError("shared preparation altered original execution or stream facts")
	}
	if _, obsolete := s.ObsoleteActions[original.ActionID]; obsolete {
		return evidence, validationError("shared preparation original action is obsolete")
	}
	return evidence, nil
}
