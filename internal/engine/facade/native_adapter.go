package facade

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"formal-gates/internal/engine/authoring"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// driveNative observes and completes this run's finite native intents inside
// the native window shared by Drive, promotion and revoking decisions. Submit
// uses the same typed admission after ordinary execution
// and observation of an operation whose process response was lost.
func (f *Facade) driveNative() (bool, error) {
	if f.vcs == nil {
		return false, nil
	}
	snap, err := f.engine.Load()
	if err != nil {
		return false, err
	}
	if snap.State.NativeRoot == "" {
		return false, nil
	}
	ids := make([]string, 0, len(snap.State.PendingHostActions))
	for id := range snap.State.PendingHostActions {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Compare(snap.State.PendingHostActions[a].Revision, snap.State.PendingHostActions[b].Revision)
	})
	// Observe every retained call before a writer can change its native prefix.
	for _, execute := range []bool{false, true} {
		for _, id := range ids {
			intent := snap.State.PendingHostActions[id]
			if intent.Adapter == nil {
				continue
			}
			operation := intent.Adapter.Operation
			if operation != "op.workspace" && operation != "op.candidate" && operation != "op.cleanup" {
				continue
			}
			var progressed bool
			var err error
			if intent.Step == "children.workspace" {
				progressed, err = f.driveChildWorkspace(intent, execute)
			} else {
				progressed, err = f.completeNativeIntent(snap.State, intent, execute)
			}
			if err != nil || progressed {
				return progressed, err
			}

		}
	}
	return false, nil
}

// completeNativeIntent uses the caller's existing native window and original
// typed receipt admission for both execution and lost-response observation.
func (f *Facade) completeNativeIntent(state *protocol.State, intent protocol.HostActionIntent, execute bool) (bool, error) {
	operation, id := intent.Adapter.Operation, intent.ActionID
	var params protocol.CandidateParameters
	if operation == "op.candidate" {
		if err := decodeNativeParams(intent, &params); err != nil {
			return false, err
		}
	}
	if receipt, known := state.HostActionReceipts[id]; known && receipt.Status == protocol.HostActionStatusExecuted {
		return false, nil
	}
	if execute {
		if !state.NativeActionWritable(intent, f.definition) {
			return false, nil
		}
	}
	evidence, complete, err := f.nativeEvidence(state, intent, execute)
	if err != nil {
		return false, err
	}
	if !complete {
		_, aborted := state.AbortDecision()
		if aborted && operation != "op.cleanup" {
			var observed protocol.HostActionObservation
			if params.Promotion != nil {
				prefix, err := f.promotionPrefix(*params.Promotion)
				if err != nil {
					return false, err
				}
				observed.Promotion = &prefix
			} else {
				resource, err := f.observeResource(state, intent)
				if err != nil {
					return false, err
				}
				observed.Resource = &resource
			}
			_, _, err := f.engine.ReconcileHostAction(id, observed.Digest(), false, false, emptyFingerprint(), observed)
			return err == nil, err
		}
		return false, nil
	}
	values, err := evidence.fields()
	if err != nil {
		return false, err
	}
	event, err := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+id), id, operation, state.RunProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
	if err != nil {
		return false, err
	}
	_, err = f.engine.Submit(event, emptyFingerprint())
	return err == nil, err
}

func (f *Facade) observeResource(s *protocol.State, intent protocol.HostActionIntent) (protocol.ResourceObservation, error) {
	var resource protocol.GitResource
	var allowed []string
	var integration *protocol.IntegrationParameters
	var integrationBase string
	if intent.Adapter.Operation == "op.workspace" {
		var p protocol.WorkspaceParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return protocol.ResourceObservation{}, err
		}
		resource = p.Resource
	} else {
		var p protocol.CandidateParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return protocol.ResourceObservation{}, err
		}
		resource, allowed = s.GitResources[p.WorkspaceID], p.Paths
		integration, integrationBase = p.Integration, p.Base
	}
	if resource.ID == "" || s.GitResources[resource.ID] != resource {
		return protocol.ResourceObservation{}, fmt.Errorf("native observation has no original registered resource")
	}
	o := protocol.ResourceObservation{ParametersDigest: intent.PayloadDigest, Resource: resource}
	trees, err := f.vcs.Worktrees()
	if err != nil {
		return o, err
	}
	o.WorktreeExists = slices.ContainsFunc(trees, func(t vcs.Worktree) bool { return vcs.SamePath(t.Path, resource.Path) })
	o.RefIdentity, err = f.vcs.Ref(resource.Ref)
	if err != nil {
		return o, err
	}
	if o.WorktreeExists {
		native := f.vcs.At(resource.Path)
		var prefix protocol.PromotionObservation
		var err error
		if integration != nil {
			prefix.Head, err = native.Resolve()
			if err != nil {
				return o, err
			}
			prefix.Tree, err = native.TreeDigest(prefix.Head)
			if err != nil {
				return o, err
			}
			entries, err := native.IndexEntries()
			if err != nil {
				return o, err
			}
			mergeHead, err := native.MergeHead()
			if err != nil {
				return o, err
			}
			o.Merge = &protocol.IntegrationEvidence{Prefix: integrationBase, Incoming: integration.Incoming, MergeHead: mergeHead, Index: entries}
		} else {
			prefix, err = observeNativePrefix(native)
		}
		if err != nil {
			return o, err
		}
		if err := native.IsAncestorOrEqual(resource.Source, prefix.Head); err != nil {
			return o, err
		}
		if intent.Adapter.Operation == "op.candidate" && integration == nil {
			paths, err := native.ChangedPaths()
			if err != nil {
				return o, err
			}
			for _, path := range paths {
				if !slices.Contains(allowed, path) {
					return o, fmt.Errorf("candidate prefix has an undeclared path: %s", path)
				}
			}
		}
		o.Prefix = &prefix
	}
	return o, nil
}

type nativeResult struct {
	Workspace *protocol.WorkspaceEvidence
	Candidate *protocol.CandidateEvidence
	Cleanup   *protocol.WorkspaceCleanupReceipt
}

func (r nativeResult) fields() (map[string]any, error) {
	switch {
	case r.Workspace != nil:
		return evidenceFields(r.Workspace)
	case r.Candidate != nil:
		return evidenceFields(r.Candidate)
	case r.Cleanup != nil:
		return evidenceFields(r.Cleanup)
	default:
		return nil, fmt.Errorf("completed native operation has no evidence")
	}
}

func evidenceFields(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}
func decodeNativeParams(intent protocol.HostActionIntent, out any) error {
	data, err := json.Marshal(intent.Adapter.Params)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func (f *Facade) nativeEvidence(s *protocol.State, intent protocol.HostActionIntent, mayWrite bool) (result nativeResult, complete bool, err error) {
	invoked := false
	defer func() {
		if err == nil || !invoked {
			return
		}
		executionErr := err
		observed, done, observationErr := f.nativeEvidence(s, intent, false)
		if observationErr == nil && done {
			result, complete, err = observed, true, nil
			return
		}
		status, class := protocol.HostActionStatusFailed, authoring.FailureUserActionRequired
		if observationErr != nil {
			status, class = protocol.HostActionStatusUnknown, ""
		} else {
			var errno syscall.Errno
			if errors.As(executionErr, &errno) && errno.Temporary() {
				class = authoring.FailureTransientEngine
			}
		}
		event, receiptErr := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+status+":"+intent.ActionID), intent.ActionID, intent.Adapter.Operation, s.RunProvider, intent.Correlation, intent.PayloadDigest, status, class, map[string]any{})
		if receiptErr == nil {
			_, receiptErr = f.engine.Submit(event, emptyFingerprint())
		}
		err = errors.Join(executionErr, observationErr, receiptErr)
	}()
	mayWrite = mayWrite && s.NativeActionWritable(intent, f.definition)
	switch intent.Adapter.Operation {
	case "op.workspace":
		var p protocol.WorkspaceParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return nativeResult{}, false, err
		}
		r, ok := s.GitResources[p.Resource.ID]
		if !ok || r != p.Resource {
			return nativeResult{}, false, fmt.Errorf("workspace intent is not the registered resource")
		}
		exists, err := f.observeWorktree(r.Path, r.Source)
		if err != nil {
			return nativeResult{}, false, err
		}
		if !exists {
			if !mayWrite {
				return nativeResult{}, false, nil
			}
			if err := f.admission.write(func() error {
				invoked = true
				return f.vcs.AddWorktree(r.Path, r.Source)
			}); err != nil {
				return nativeResult{}, false, err
			}
		}
		if complete, err := f.injectRequirements(s, p, mayWrite, &invoked); err != nil || !complete {
			return nativeResult{}, false, err
		}
		if complete, err := f.ensureRef(r.Ref, r.Source, mayWrite, &invoked); err != nil || !complete {
			return nativeResult{}, false, err
		}
		if p.Manifest != nil {
			body := protocol.BlackboxCasesMarkdown(s.RunID, *p.Manifest)
			target := filepath.Join(r.Path, ".gates", "cases", "blackbox.md")
			existing, readErr := os.ReadFile(target)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nativeResult{}, false, readErr
			}
			if string(existing) != body {
				if !mayWrite {
					return nativeResult{}, false, nil
				}
				if err := f.admission.write(func() error {
					invoked = true

					if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
						return err
					}
					return writeFileAtomic(target, []byte(body), 0644)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			r.MirrorDigest = protocol.BlackboxCasesDigest(s.RunID, *p.Manifest)
		}
		r.Identity = r.Source
		r.Status = "READY"
		digest, err := f.vcs.TreeDigest(r.Identity)
		if err != nil {
			return nativeResult{}, false, err
		}
		return nativeResult{Workspace: &protocol.WorkspaceEvidence{ParametersDigest: intent.PayloadDigest, Resource: r, Digest: digest}}, true, nil
	case "op.candidate":
		return f.candidateEvidence(s, intent, mayWrite, &invoked)
	case "op.cleanup":
		if mayWrite {
			if err := f.freezeCleanupSummary(s); err != nil {
				return nativeResult{}, false, err
			}
		}
		var p protocol.CleanupParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return nativeResult{}, false, err
		}
		for i, r := range p.Resources {
			registered, ok := s.GitResources[r.ID]
			if !ok || registered != r {
				return nativeResult{}, false, fmt.Errorf("cleanup intent does not match its registered resource")
			}
			trees, err := f.vcs.Worktrees()
			if err != nil {
				return nativeResult{}, false, err
			}
			exists := slices.ContainsFunc(trees, func(t vcs.Worktree) bool { return vcs.SamePath(t.Path, r.Path) })
			if exists {
				if !mayWrite {
					return nativeResult{}, false, nil
				}
				if err := f.admission.write(func() error {
					invoked = true
					return f.vcs.RemoveWorktree(r.Path)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			ref, err := f.vcs.Ref(r.Ref)
			if err != nil {
				return nativeResult{}, false, err
			}
			if ref != "" {
				if !mayWrite {
					return nativeResult{}, false, nil
				}
				if err := f.admission.write(func() error {
					invoked = true
					return f.vcs.RemoveRef(r.Ref)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			trees, err = f.vcs.Worktrees()
			if err != nil {
				return nativeResult{}, false, err
			}
			ref, err = f.vcs.Ref(r.Ref)
			if err != nil {
				return nativeResult{}, false, err
			}
			if ref != "" || slices.ContainsFunc(trees, func(t vcs.Worktree) bool { return vcs.SamePath(t.Path, r.Path) }) {
				return nativeResult{}, false, fmt.Errorf("native resource remains after cleanup: %s", r.Path)
			}
			if _, err := os.Stat(r.Path); err == nil {
				if !mayWrite {
					return nativeResult{}, false, nil
				}
				if err := f.admission.write(func() error {
					invoked = true
					return os.RemoveAll(r.Path)
				}); err != nil {
					return nativeResult{}, false, err
				}
			} else if !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			r.Status = "CLEANED"
			p.Resources[i] = r
		}
		return nativeResult{Cleanup: &protocol.WorkspaceCleanupReceipt{ParametersDigest: intent.PayloadDigest, Resources: p.Resources}}, true, nil
	}
	return nativeResult{}, false, nil
}
func (f *Facade) observeWorktree(path, identity string) (bool, error) {
	trees, err := f.vcs.Worktrees()
	if err != nil {
		return false, err
	}
	for _, tree := range trees {
		if vcs.SamePath(tree.Path, path) {
			if tree.Identity != identity {
				return false, fmt.Errorf("workspace %s has identity %s, expected %s", path, tree.Identity, identity)
			}
			return true, nil
		}
	}
	if _, err := os.Stat(path); err == nil {
		return false, fmt.Errorf("unregistered directory occupies workspace %s", path)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}
func (f *Facade) ensureRef(ref, identity string, mayWrite bool, invoked *bool) (bool, error) {
	actual, err := f.vcs.Ref(ref)
	if err != nil {
		return false, err
	}
	if actual == identity {
		return true, nil
	}
	if actual != "" {
		return false, fmt.Errorf("resource ref %s points to %s, expected %s", ref, actual, identity)
	}
	if !mayWrite {
		return false, nil
	}
	err = f.admission.write(func() error {
		*invoked = true
		return f.vcs.Pin(ref, identity)
	})
	return err == nil, err
}
func (f *Facade) injectRequirements(s *protocol.State, p protocol.WorkspaceParameters, mayWrite bool, invoked *bool) (bool, error) {
	if s.IntakeConfirmationReceipt == nil {
		return false, fmt.Errorf("workspace requires intake artifacts")
	}
	requirementPath := filepath.Clean(s.IntakeConfirmationReceipt.RequirementSource)
	if filepath.IsAbs(requirementPath) {
		var err error
		requirementPath, err = filepath.Rel(f.root, requirementPath)
		if err != nil {
			return false, err
		}
	}
	for _, artifact := range s.IntakeConfirmationReceipt.Artifacts {
		isRequirement := filepath.Clean(filepath.FromSlash(artifact.Path)) == requirementPath
		revision := artifact.Revision
		if isRequirement {
			revision = strings.TrimPrefix(p.RequirementRevision, "sha256:")
		}
		target := filepath.Join(p.Resource.Path, filepath.FromSlash(artifact.Path))
		existing, err := os.ReadFile(target)
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if err == nil && requirementContentRevision(existing) == revision {
			continue
		}
		if err == nil {
			// Committed candidate content remains authoritative when development also
			// owns an intake artifact such as a source file.
			entry, entryErr := f.vcs.At(p.Resource.Path).Entry("HEAD", artifact.Path)
			if entryErr != nil {
				return false, entryErr
			}
			if entry != "" {
				continue
			}
		}
		if !mayWrite {
			return false, nil
		}
		var data []byte
		if isRequirement {
			if s.Validation == nil || s.Validation.RequirementRevision != p.RequirementRevision {
				return false, fmt.Errorf("workspace requires its issued confirmed requirement material")
			}
			data = []byte(s.Validation.Document.Content)
		} else {
			data, err = os.ReadFile(filepath.Join(f.root, filepath.FromSlash(artifact.Path)))
			if err != nil {
				return false, err
			}
		}
		if requirementContentRevision(data) != revision {
			return false, fmt.Errorf("requirement artifact %s differs from its confirmed revision", artifact.Path)
		}
		if err := f.admission.write(func() error {
			*invoked = true

			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			return writeFileAtomic(target, data, 0644)
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}
func (f *Facade) candidateEvidence(s *protocol.State, intent protocol.HostActionIntent, mayWrite bool, invoked *bool) (nativeResult, bool, error) {
	var p protocol.CandidateParameters
	if err := decodeNativeParams(intent, &p); err != nil {
		return nativeResult{}, false, err
	}
	if p.Integration != nil {
		return f.integrationEvidence(s, intent, p, mayWrite, invoked)
	}
	if p.Stage == "PROMOTION" {
		return f.promotionEvidence(s, intent, mayWrite, invoked)
	}
	r, ok := s.GitResources[p.WorkspaceID]
	if !ok || r.Status != "READY" {
		return nativeResult{}, false, fmt.Errorf("candidate has no admitted native workspace")
	}
	native := f.vcs.At(r.Path)
	head, err := native.Resolve()
	if err != nil {
		return nativeResult{}, false, err
	}
	message := "formal-gates candidate " + s.RunID + " " + intent.ActionID + " " + intent.PayloadDigest
	completed := p.Stage == "AUTHORING" && head == p.Base
	if head != p.Base {
		observed, err := native.Message(head)
		if err != nil {
			return nativeResult{}, false, err
		}
		if observed != message {
			for _, later := range s.PendingHostActions {
				if later.Revision > intent.Revision && later.Correlation == intent.Correlation && later.PayloadDigest == intent.PayloadDigest && observed == "formal-gates candidate "+s.RunID+" "+later.ActionID+" "+later.PayloadDigest {
					return nativeResult{}, false, nil
				}
			}
			return nativeResult{}, false, fmt.Errorf("candidate workspace HEAD %s is not the issued freeze at %s", head, p.Base)
		}
		if err := native.IsAncestorOrEqual(p.Base, head); err != nil {
			return nativeResult{}, false, err
		}
		count, err := native.CommitCount(p.Base, head)
		if err != nil {
			return nativeResult{}, false, err
		}
		if count != 1 {
			return nativeResult{}, false, fmt.Errorf("candidate freeze has an unexpected commit range")
		}
		completed = true
	}
	if !completed && !mayWrite {
		// A freeze with no tree change has no commit message to identify it.
		// Its clean tree must already contain every assembled input; staged or
		// missing inputs are an incomplete prefix, never a completion receipt.
		changes, err := native.ChangedPaths()
		if err != nil || len(changes) != 0 {
			return nativeResult{}, false, err
		}
		for _, artifact := range p.QAArtifacts {
			for _, path := range artifact.Paths {
				want, err := native.Entry(artifact.Identity, path)
				if err != nil {
					return nativeResult{}, false, err
				}
				actual, err := native.Entry(head, path)
				if err != nil || actual != want {
					return nativeResult{}, false, err
				}
			}
		}
		if p.MergeManifest != nil {
			body, err := os.ReadFile(filepath.Join(r.Path, ".gates", "results", s.RunID+".merge-cases.md"))
			if err != nil && !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			if string(body) != protocol.MergeCasesMarkdown(s.RunID, *p.MergeManifest) {
				return nativeResult{}, false, nil
			}
		}
		if p.BlackboxManifest != nil {
			body, err := os.ReadFile(filepath.Join(r.Path, ".gates", "results", s.RunID+".blackbox-cases.md"))
			if err != nil && !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			if string(body) != protocol.BlackboxCasesMarkdown(s.RunID, *p.BlackboxManifest) {
				return nativeResult{}, false, nil
			}
		}
		completed = true
	}
	paths := slices.Clone(p.Paths)
	if !completed {
		changes, err := native.ChangedPaths()
		if err != nil {
			return nativeResult{}, false, err
		}
		for _, path := range changes {
			if !slices.Contains(paths, path) {
				return nativeResult{}, false, fmt.Errorf("candidate contains undeclared change: %s", path)
			}
		}
		if p.MergeManifest != nil {
			target := filepath.Join(r.Path, ".gates", "results", s.RunID+".merge-cases.md")
			body := protocol.MergeCasesMarkdown(s.RunID, *p.MergeManifest)
			actual, err := os.ReadFile(target)
			if err != nil && !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			if string(actual) != body {
				if err := f.admission.write(func() error {
					*invoked = true
					if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
						return err
					}
					return writeFileAtomic(target, []byte(body), 0644)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
		}
		if p.Stage == "REVIEW_READY" {

			kinds := make([]coverage.ReviewKind, 0, len(p.QAArtifacts))
			for kind := range p.QAArtifacts {
				kinds = append(kinds, coverage.ReviewKind(kind))
			}
			slices.Sort(kinds)
			for _, kind := range kinds {
				artifact := p.QAArtifacts[kind]
				if err := f.admission.write(func() error {
					*invoked = true
					return native.ApplyArtifact(artifact.Identity, artifact.Paths)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			if p.BlackboxManifest != nil {
				target := filepath.Join(r.Path, ".gates", "results", s.RunID+".blackbox-cases.md")
				body := protocol.BlackboxCasesMarkdown(s.RunID, *p.BlackboxManifest)
				if err := f.admission.write(func() error {
					*invoked = true

					if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
						return err
					}
					return writeFileAtomic(target, []byte(body), 0644)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
		}
		if p.Stage == "QA_ARTIFACT" {
			owned, err := native.InputOwnership()
			if err != nil {
				return nativeResult{}, false, err
			}
			baseOwned, err := f.ownershipAt(s, p.Base)
			if err != nil {
				return nativeResult{}, false, err
			}
			for _, path := range changes {
				entry, err := native.Entry(p.Base, path)
				if err != nil {
					return nativeResult{}, false, err
				}
				_, statErr := os.Lstat(filepath.Join(r.Path, filepath.FromSlash(path)))
				if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
					return nativeResult{}, false, statErr
				}
				if !testOwnedTransition(path, entry != "", statErr == nil, baseOwned, owned) {
					return nativeResult{}, false, fmt.Errorf("whitebox change is not test-owned: %s", path)
				}
			}
		}
		if err := f.admission.write(func() error {
			*invoked = true
			return native.Track(paths)
		}); err != nil {
			return nativeResult{}, false, err
		}
		tree, err := native.StagedTreeDigest()
		if err != nil {
			return nativeResult{}, false, err
		}
		baseTree, err := native.TreeDigest(p.Base)
		if err != nil {
			return nativeResult{}, false, err
		}
		if tree != baseTree {
			if err := f.admission.write(func() error {
				*invoked = true
				return native.Commit(message)
			}); err != nil {
				return nativeResult{}, false, err
			}
			head, err = native.Resolve()
			if err != nil {
				return nativeResult{}, false, err
			}
		}
	}
	if p.Stage != "AUTHORING" {
		if err := native.VerifyReady(); err != nil {
			return nativeResult{}, false, err
		}
	}
	digest, err := native.TreeDigest(head)
	if err != nil {
		return nativeResult{}, false, err
	}
	ref, err := f.vcs.Ref(r.Ref)
	if err != nil {
		return nativeResult{}, false, err
	}
	if ref != head {
		if ref != "" && ref != p.Base {
			return nativeResult{}, false, fmt.Errorf("candidate ref %s points to %s, expected its base %s or freeze %s", r.Ref, ref, p.Base, head)
		}
		if !mayWrite {
			return nativeResult{}, false, nil
		}
		if err := f.admission.write(func() error {
			*invoked = true
			return f.vcs.Pin(r.Ref, head)
		}); err != nil {
			return nativeResult{}, false, err
		}
	}
	changes, err := native.Changes(p.Base, head)
	if err != nil {
		return nativeResult{}, false, err
	}
	for _, change := range changes {
		if !slices.Contains(p.Paths, change.Path) {
			return nativeResult{}, false, fmt.Errorf("candidate has an undeclared native change: %s", change.Path)
		}
	}
	evidence := protocol.CandidateEvidence{Changes: changes, ParametersDigest: intent.PayloadDigest, WorkspaceID: p.WorkspaceID, Identity: head, Digest: digest, Paths: p.Paths}
	if p.Stage == "QA_ARTIFACT" {
		owned, err := native.InputOwnership()
		if err != nil {
			return nativeResult{}, false, err
		}
		// Removed test inputs are proven by their original package graph.
		baseOwned, err := f.ownershipAt(s, p.Base)
		if err != nil {
			return nativeResult{}, false, err
		}
		for _, change := range changes {
			path := change.Path
			if !slices.Contains(p.Paths, path) || !testOwnedTransition(path, change.BaseMode != "000000", change.Mode != "000000", baseOwned, owned) {
				return nativeResult{}, false, fmt.Errorf("whitebox change is not test-owned: %s", path)
			}
		}
		evidence.TestOnly = true
		evidence.ProductionViewEqual = true
	}
	if p.Stage == "REVIEW_READY" {
		changes, err := native.Changes(p.ReviewBase, head)
		if err != nil {
			return nativeResult{}, false, err
		}
		owned, err := native.InputOwnership()
		if err != nil {
			return nativeResult{}, false, err
		}
		if p.MergeManifest != nil {
			if owned == nil {
				owned = map[string]bool{}
			}
			owned[".gates/results/"+s.RunID+".merge-cases.md"] = true
		}
		baseOwned, err := f.ownershipAt(s, p.ReviewBase)
		if err != nil {
			return nativeResult{}, false, err
		}
		view := &protocol.GateReviewView{TestOwnedPaths: []string{}}
		for _, change := range changes {
			if testOwnedTransition(change.Path, change.BaseMode != "000000", change.Mode != "000000", baseOwned, owned) {
				view.TestOwnedPaths = append(view.TestOwnedPaths, change.Path)
			}
		}
		evidence.Changes, evidence.GateView = changes, view
		if s.RouteMode == "merge" {
			evidence.ChildViews, err = f.childValidationViews(s, head, owned)
			if err != nil {
				return nativeResult{}, false, err
			}
		}
	}
	return nativeResult{Candidate: &evidence}, true, nil
}

func testOwnedTransition(path string, baseExists, candidateExists bool, base, candidate map[string]bool) bool {
	b, bok := base[path]
	c, cok := candidate[path]
	return (baseExists || candidateExists) && (!baseExists || bok && b) && (!candidateExists || cok && c)
}

func (f *Facade) ownershipAt(s *protocol.State, identity string) (map[string]bool, error) {
	// A graph represents an immutable commit only when HEAD and all delivery
	// bytes match it. Mutable authoring workspaces cannot prove base ownership.
	providers := []*vcs.Provider{f.vcs}
	ids := make([]string, 0, len(s.GitResources))
	for id := range s.GitResources {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r := s.GitResources[id]
		if r.Status == "READY" && !s.ResourceCleanupPending(r.ID) {
			providers = append(providers, f.vcs.At(r.Path))
		}
	}
	for _, native := range providers {
		head, err := native.Resolve()
		if err != nil {
			return nil, err
		}
		if head != identity {
			continue
		}
		status, err := native.Status()
		if err != nil {
			return nil, err
		}
		if status != "" {
			continue
		}
		owned, err := native.InputOwnership()
		if err != nil {
			return nil, err
		}
		for _, receipt := range s.HostActionReceipts {
			var evidence protocol.CandidateEvidence
			if decodeReceiptEvidence(receipt, &evidence) != nil || evidence.Identity != identity || evidence.GateView == nil {
				continue
			}
			if owned == nil {
				owned = map[string]bool{}
			}
			for _, path := range evidence.GateView.TestOwnedPaths {
				if _, known := owned[path]; !known {
					owned[path] = true
				}
			}
		}
		return owned, nil
	}
	return nil, nil
}
