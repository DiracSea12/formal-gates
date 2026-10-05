package protocol

import (
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// Integration parameters bind one fixed-order child and its original prefix.
// Completed native receipts, rather than an extra cursor, own that prefix.
type IntegrationParameters struct {
	SliceID            string `json:"sliceId"`
	ChildRunID         string `json:"childRunId"`
	ChildReceiptDigest string `json:"childReceiptDigest"`
	TopologyDigest     string `json:"topologyDigest"`
	MapDigest          string `json:"mapDigest"`
	Incoming           string `json:"incoming"`
	IncomingTree       string `json:"incomingTree"`
	StartAction        string `json:"startAction,omitempty"`
}

type MergeConflict struct {
	Path    string           `json:"path"`
	Entries []vcs.IndexEntry `json:"entries"`
	Content []byte           `json:"content,omitempty"`
	Mode    uint32           `json:"mode,omitempty"`
	Text    bool             `json:"text"`
}
type IntegrationEvidence struct {
	Incoming  string           `json:"incoming"`
	Prefix    string           `json:"prefix"`
	MergeHead string           `json:"mergeHead,omitempty"`
	Index     []vcs.IndexEntry `json:"index"`
	Conflicts []MergeConflict  `json:"conflicts,omitempty"`
}
type ConflictResolution struct {
	Paths   []string `json:"paths"`
	Summary string   `json:"summary"`
}
type ConflictInput struct {
	StartAction string                `json:"startAction"`
	Parameters  IntegrationParameters `json:"parameters"`
	Evidence    IntegrationEvidence   `json:"evidence"`
}

// ChildValidationView is produced by the native candidate adapter. Unknown
// equality is represented by false only for the obligation whose input is absent.
type ChildValidationView struct {
	BaseSnapshot  string                   `json:"baseSnapshot"`
	ReceiptDigest string                   `json:"receiptDigest"`
	OwnedChanges  []vcs.Change             `json:"ownedChanges"`
	ContentEqual  bool                     `json:"contentEqual"`
	Gates         map[string]ChildGateView `json:"gates"`
}

type ChildGateView struct {
	SourceCandidate string `json:"sourceCandidate"`
	SourceAction    string `json:"sourceAction"`
	SourceDigest    string `json:"sourceDigest"`
	CandidateDigest string `json:"candidateDigest"`
	Equal           bool   `json:"equal"`
}

func (s *State) mergeMaster() bool {
	return s.SliceChild == nil && s.RouteMode == routeModeMerge && s.Topology != nil && s.Topology.Decision == topologySplit
}
func (s *State) integrationResource() GitResource {
	if !s.mergeMaster() || s.ChildCreation == nil || s.Validation == nil {
		return GitResource{}
	}
	r := s.resource("merge", s.ChildCreation.Source, digestOfCanonical(s.Topology)+":"+strconv.FormatUint(s.Validation.Generation, 10))
	if existing, ok := s.GitResources[r.ID]; ok {
		return existing
	}
	return r
}

func (s *State) integrationReceipt(stage, sliceID string) (HostActionIntent, CandidateEvidence, bool) {
	var found HostActionIntent
	var proof CandidateEvidence
	for action, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" || receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled {
			continue
		}
		intent, exists := s.hostIntent(action)
		p, err := candidateParameters(intent)
		if !exists || err != nil || p.Stage != stage || p.Integration == nil || p.Integration.SliceID != sliceID || !s.candidateContextCurrent(p) {
			continue
		}
		child := s.SliceReceipts[p.Integration.ChildRunID]
		if p.Integration.ChildReceiptDigest != child.Digest || p.Integration.MapDigest != digestOfCanonical(s.SliceChildren) || p.Integration.TopologyDigest != digestOfCanonical(s.Topology) {
			continue
		}
		v, err := decodeCandidateEvidence(receipt)
		if err == nil && v.Integration != nil && intent.Revision >= found.Revision {
			found, proof = intent, v
		}
	}
	return found, proof, found.ActionID != ""
}

func (s *State) integrationParameters(cd *compiler.CompiledDefinition) (CandidateParameters, bool) {
	if !s.mergeMaster() || s.Validation == nil || s.ChildCreation == nil || s.Validation.DevelopmentSnapshot != "" || s.currentValidationWave() != nil || s.abortRequested() {
		return CandidateParameters{}, false
	}
	ready, expected := s.sliceReceiptProgress()
	if ready != expected || expected == 0 {
		return CandidateParameters{}, false
	}
	prefix := s.ChildCreation.Source
	for _, sliceID := range s.Topology.Slices {
		if _, proof, complete := s.integrationReceipt("MERGE_FINISH", sliceID); complete {
			prefix = proof.Identity
			continue
		}
		receipt := s.SliceReceipts[s.SliceChildren[sliceID]]
		integration := &IntegrationParameters{SliceID: sliceID, ChildRunID: receipt.Receipt.ChildRunID, ChildReceiptDigest: receipt.Digest, TopologyDigest: digestOfCanonical(s.Topology), MapDigest: digestOfCanonical(s.SliceChildren), Incoming: receipt.Receipt.Snapshot, IncomingTree: receipt.Receipt.Tree}
		stage := "MERGE_START"
		if start, _, complete := s.integrationReceipt("MERGE_START", sliceID); complete {
			stage = "MERGE_FINISH"
			integration.StartAction = start.ActionID
		}
		r := s.integrationResource()
		return CandidateParameters{Stage: stage, Base: prefix, WorkspaceID: r.ID, Integration: integration, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest()}, true
	}
	return CandidateParameters{}, false
}

func (s *State) integrationResolution(action string) *ConflictResolution {
	for _, result := range s.Results {
		if result.Validation != nil && result.Validation.Binding.Task.Scope == action && result.Validation.Resolution != nil && result.Validation.Status == "PASS" && result.Outcome == OutcomePass {
			return result.Validation.Resolution
		}
	}
	return nil
}
func (s *State) conflictInput(action string) *ConflictInput {
	intent, exists := s.hostIntent(action)
	p, err := candidateParameters(intent)
	receipt, accepted := s.HostActionReceipts[action]
	proof, proofErr := decodeCandidateEvidence(receipt)
	if !exists || err != nil || p.Integration == nil || !accepted || proofErr != nil || proof.Integration == nil {
		return nil
	}
	return &ConflictInput{StartAction: action, Parameters: *p.Integration, Evidence: *proof.Integration}
}

func (s *State) integratedSources() ([]runtime.TaskKey, []string, string, bool) {
	if !s.mergeMaster() || s.ChildCreation == nil {
		return nil, nil, "", false
	}
	var paths []string
	prefix := s.ChildCreation.Source
	for _, sliceID := range s.Topology.Slices {
		_, evidence, ok := s.integrationReceipt("MERGE_FINISH", sliceID)
		if !ok {
			return nil, nil, "", false
		}
		prefix = evidence.Identity
		for _, change := range evidence.Changes {
			paths = append(paths, change.Path)
		}
	}
	paths = append(paths, ".gates/results/"+s.RunID+".merge-cases.md")
	var tasks []runtime.TaskKey
	for _, work := range s.repairWork() {
		if work.result == nil {
			continue
		}
		tasks = append(tasks, work.task)
		paths = append(paths, work.result.Validation.Development.Paths...)
		prefix = work.result.Validation.Binding.Candidate
	}
	slices.Sort(paths)
	slices.SortFunc(tasks, func(a, b runtime.TaskKey) int { return strings.Compare(a.String(), b.String()) })
	return tasks, slices.Compact(paths), prefix, true
}

func sValidateIntegrationReceipt(p CandidateParameters, v CandidateEvidence) error {
	if p.Integration == nil || v.Integration == nil || v.Integration.Incoming != p.Integration.Incoming || v.Integration.Prefix != p.Base {
		return validationError("integration receipt differs from its signed child and prefix")
	}
	if p.Stage == "MERGE_START" && (v.Identity != p.Base || v.Integration.MergeHead != "" && v.Integration.MergeHead != p.Integration.Incoming) {
		return validationError("merge start has a different native prefix")
	}
	if p.Stage == "MERGE_FINISH" && (v.Integration.MergeHead != "" || len(v.Integration.Conflicts) != 0 || slices.ContainsFunc(v.Integration.Index, func(e vcs.IndexEntry) bool { return e.Stage != 0 })) {
		return validationError("merge finish has unresolved native conflicts")
	}
	return nil
}

func (s *State) candidateChildViews() map[string]ChildValidationView {
	if s.Validation == nil || s.Validation.Candidate == nil {
		return nil
	}
	for _, receipt := range s.HostActionReceipts {
		v, err := decodeCandidateEvidence(receipt)
		if err == nil && v.Identity == s.Validation.Candidate.Identity && v.Digest == s.Validation.Candidate.Digest && v.ChildViews != nil {
			return v.ChildViews
		}
	}
	return nil
}

func (e *Engine) advanceIntegration(s *State, revision uint64) (bool, error) {
	p, ready, err := s.currentCandidateParameters(e.cfg.Definition)
	if err != nil || !ready {
		return false, err
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && intent.Correlation == candidateCorrelation(p) {
			return false, nil
		}
	}
	r := s.integrationResource()
	if !s.resourceUsable(r.ID) || r.Status != "READY" {
		return e.ensureWorkspace(s, r, revision)
	}
	intent, err := e.prepareAdapterIntent(s, revision, "op.candidate", p, "")
	if err != nil {
		return false, err
	}
	s.PendingHostActions[intent.ActionID] = intent
	return true, nil
}
