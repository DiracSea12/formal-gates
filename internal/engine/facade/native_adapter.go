package facade

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
			if operation != "op.workspace" && operation != "op.candidate" && operation != "op.cleanup" && operation != "op.check" {
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
		if operation == "op.check" {
			directory := filepath.Join(f.stateDir, "checks", strings.ReplaceAll(id, ":", "-"))
			if _, owned := os.Stat(filepath.Join(directory, "owner")); owned == nil {
				event, eventErr := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:UNKNOWN:"+id), id, operation, state.RunProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
				if eventErr == nil {
					_, eventErr = f.engine.Submit(event, emptyFingerprint())
				}
				return false, errors.Join(err, eventErr)
			}
		}
		return false, err
	}
	if !complete {
		_, aborted := state.AbortDecision()
		if operation == "op.check" && evidence.observedEffect {
			return false, nil
		}
		if (aborted || state.ResetPending()) && operation != "op.cleanup" {
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
	var resource protocol.NativeResource
	var allowed []string
	var integration *protocol.IntegrationParameters
	var integrationBase string
	if intent.Adapter.Operation == "op.check" {
		var p protocol.CheckParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return protocol.ResourceObservation{}, err
		}
		resource = s.NativeResources[p.WorkspaceID]
	} else if intent.Adapter.Operation == "op.workspace" {
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
		resource, allowed = s.NativeResources[p.WorkspaceID], p.Paths
		integration, integrationBase = p.Integration, p.Base
	}
	if resource.ID == "" || s.NativeResources[resource.ID] != resource {
		return protocol.ResourceObservation{}, fmt.Errorf("native observation has no original registered resource")
	}
	o := protocol.ResourceObservation{ParametersDigest: intent.PayloadDigest, Resource: resource}

	var err error
	o.WorktreeExists, err = f.vcs.WorkspaceExists(resource.Path, resource.Handle)
	if err != nil {
		return o, err
	}
	if resource.Ref != "" {
		o.RefIdentity, err = f.vcs.Ref(resource.Ref)
		if err != nil {
			return o, err
		}
	}

	if o.WorktreeExists {
		native := f.vcs.At(resource.Path, resource.Handle)
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

			observation, err := native.ObserveIntegration(integrationBase, integration.Incoming)
			if err != nil {
				return o, err
			}
			mergeHead := ""
			if native.Name() == "git" {
				mergeHead = observation.Lineage
			}
			o.Merge = &protocol.IntegrationEvidence{Prefix: integrationBase, Incoming: integration.Incoming, MergeHead: mergeHead, NativeLineage: observation.Lineage, NativeStarted: observation.Started, Index: entries}

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
			pending, err := native.Pending(integrationBase, "")
			if err != nil {
				return o, err
			}
			for _, path := range paths {
				if !slices.Contains(allowed, path) && !slices.ContainsFunc(pending.Changes, func(c vcs.Change) bool { return c.Path == path && c.IsDirectoryContainer(allowed) }) {
					return o, fmt.Errorf("candidate prefix has an undeclared path: %s", path)
				}
			}
		}
		o.Prefix = &prefix
	}
	return o, nil
}

type nativeResult struct {
	// A precise native effect can precede its local ref or workspace finish.
	// This observation never admits a receipt or authorizes another writer.
	observedEffect bool
	Workspace      *protocol.WorkspaceEvidence
	Candidate      *protocol.CandidateEvidence
	Cleanup        *protocol.WorkspaceCleanupReceipt
	Check          *protocol.CheckEvidence
}

func (r nativeResult) fields() (map[string]any, error) {
	switch {
	case r.Check != nil:
		return evidenceFields(r.Check)
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
		if observationErr == nil && observed.observedEffect {
			// Keep the original error and responsibility until its authorized
			// local continuation produces the complete typed evidence.
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
	case "op.check":
		return f.checkEvidence(s, intent, mayWrite, &invoked)
	case "op.workspace":
		var p protocol.WorkspaceParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return nativeResult{}, false, err
		}
		r, ok := s.NativeResources[p.Resource.ID]
		if !ok || r != p.Resource {
			return nativeResult{}, false, fmt.Errorf("workspace intent is not the registered resource")
		}
		exists, err := f.observeWorktree(r.Path, r.Source, r.Handle)
		if err != nil {
			return nativeResult{}, false, err
		}
		if !exists {
			if !mayWrite {
				return f.incompleteWorkspaceEvidence(s, intent, r)
			}
			if err := f.admission.write(func() error {
				invoked = true
				return f.vcs.CreateWorkspace(r.Path, r.Source, "formal-gates workspace "+s.RunID+" "+intent.ActionID+" "+intent.PayloadDigest, r.Handle)
			}); err != nil {
				return nativeResult{}, false, err
			}
		}
		if complete, err := f.injectRequirements(s, p, mayWrite, &invoked); err != nil || !complete {
			if err != nil {
				return nativeResult{}, false, err
			}
			return f.incompleteWorkspaceEvidence(s, intent, r)
		}
		if complete, err := f.ensureRef(r.Ref, r.Source, mayWrite, &invoked); err != nil || !complete {
			if err != nil {
				return nativeResult{}, false, err
			}
			return f.incompleteWorkspaceEvidence(s, intent, r)
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
					return f.incompleteWorkspaceEvidence(s, intent, r)
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
		r.Identity, err = f.vcs.At(r.Path, r.Handle).Resolve()
		if err != nil {
			return nativeResult{}, false, err
		}
		r.Status = "READY"
		digest, err := f.vcs.TreeDigest(r.Identity)
		if err != nil {
			return nativeResult{}, false, err
		}
		sourceDigest, err := f.vcs.TreeDigest(r.Source)
		if err != nil {
			return nativeResult{}, false, err
		}
		if sourceDigest != digest {
			return nativeResult{}, false, fmt.Errorf("native workspace does not preserve complete source values")
		}
		return nativeResult{Workspace: &protocol.WorkspaceEvidence{ParametersDigest: intent.PayloadDigest, Resource: r, Digest: digest, SourceDigest: sourceDigest}}, true, nil
	case "op.candidate":
		return f.candidateEvidence(s, intent, mayWrite, &invoked)
	case "op.cleanup":
		var p protocol.CleanupParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			return nativeResult{}, false, err
		}
		if mayWrite && p.Checkpoint == nil && !s.ResetPending() && !s.RebuildingChildren() {
			if err := f.freezeCleanupSummary(s); err != nil {
				return nativeResult{}, false, err
			}
		}
		observedEffect := false
		for i, r := range p.Resources {
			registered, ok := s.NativeResources[r.ID]
			if !ok || registered != r {
				return nativeResult{}, false, fmt.Errorf("cleanup intent does not match its registered resource")
			}

			absent, err := f.vcs.WorkspaceAbsent(r.Path, r.Handle)
			if err != nil {
				return nativeResult{}, false, err
			}
			observedEffect = observedEffect || absent
			if !absent {
				if !mayWrite {
					if f.vcs.Name() != "git" {
						removed, err := f.vcs.ObservedWorkspaceCleanup(r.Source, "formal-gates cleanup "+s.RunID+" "+intent.ActionID+" "+intent.PayloadDigest, r.Handle)
						if err != nil {
							return nativeResult{}, false, err
						}
						observedEffect = observedEffect || removed
					}
					return nativeResult{observedEffect: observedEffect}, false, nil
				}
				if err := f.admission.write(func() error {
					invoked = true
					return f.vcs.CleanupWorkspace(r.Path, "formal-gates cleanup "+s.RunID+" "+intent.ActionID+" "+intent.PayloadDigest, r.Handle)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			if r.Ref != "" {
				ref, err := f.vcs.Ref(r.Ref)
				if err != nil {
					return nativeResult{}, false, err
				}
				if ref != "" {
					if !mayWrite {
						return nativeResult{observedEffect: observedEffect}, false, nil
					}
					if err = f.admission.write(func() error { invoked = true; return f.vcs.RemoveRef(r.Ref) }); err != nil {
						return nativeResult{}, false, err
					}
				}
			}
			if _, err := os.Stat(r.Path); err == nil {
				if !mayWrite {
					return nativeResult{observedEffect: observedEffect}, false, nil
				}
				if err = f.admission.write(func() error { invoked = true; return os.RemoveAll(r.Path) }); err != nil {
					return nativeResult{}, false, err
				}
			} else if !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			absent, err = f.vcs.WorkspaceAbsent(r.Path, r.Handle)
			if err != nil {
				return nativeResult{}, false, err
			}
			if !absent {
				return nativeResult{}, false, fmt.Errorf("native resource remains after cleanup")
			}

			observedEffect = true
			r.Status = "CLEANED"
			p.Resources[i] = r
		}
		return nativeResult{Cleanup: &protocol.WorkspaceCleanupReceipt{ParametersDigest: intent.PayloadDigest, Resources: p.Resources}}, true, nil
	}
	return nativeResult{}, false, nil
}
func (f *Facade) incompleteWorkspaceEvidence(s *protocol.State, intent protocol.HostActionIntent, r protocol.NativeResource) (nativeResult, bool, error) {
	if r.Status == "READY" {
		return nativeResult{}, false, nil
	}
	message := "formal-gates workspace " + s.RunID + " " + intent.ActionID + " " + intent.PayloadDigest
	identity, found, err := f.vcs.ObservedWorkspace(r.Path, r.Source, message, r.Handle)
	if err != nil || !found {
		return nativeResult{}, false, err
	}
	actual, err := f.vcs.TreeDigest(identity)
	if err != nil {
		return nativeResult{}, false, err
	}
	expected, err := f.vcs.TreeDigest(r.Source)
	if err != nil {
		return nativeResult{}, false, err
	}
	if actual != expected {
		return nativeResult{}, false, fmt.Errorf("observed native workspace does not preserve complete source values")
	}
	return nativeResult{observedEffect: true}, false, nil
}

func (f *Facade) observeWorktree(path, identity string, handles ...vcs.WorkspaceHandle) (bool, error) {
	h := vcs.WorkspaceHandle{}
	if len(handles) != 0 {
		h = handles[0]
	}
	exists, err := f.vcs.WorkspaceExists(path, h)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	native := f.vcs.At(path, h)
	actual, err := native.Resolve()
	if err != nil {
		return false, err
	}
	if f.vcs.Name() == "git" {
		if actual != identity {
			return false, fmt.Errorf("workspace identity differs from original source")
		}
	} else {
		a, err := native.TreeDigest(actual)
		if err != nil {
			return false, err
		}
		b, err := native.TreeDigest(identity)
		if err != nil {
			return false, err
		}
		if a != b {
			return false, fmt.Errorf("native workspace values differ from original source")
		}
	}
	return true, nil
}
func (f *Facade) ensureRef(ref, identity string, mayWrite bool, invoked *bool) (bool, error) {
	if ref == "" {
		return true, nil
	}
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
			entry, entryErr := f.vcs.At(p.Resource.Path, p.Resource.Handle).Entry("HEAD", artifact.Path)
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
	r, ok := s.NativeResources[p.WorkspaceID]
	if !ok || r.Status != "READY" {
		return nativeResult{}, false, fmt.Errorf("candidate has no admitted native workspace")
	}
	native := f.vcs.At(r.Path, r.Handle)
	head, err := native.Resolve()
	if err != nil {
		return nativeResult{}, false, err
	}
	workspaceBase := p.Base
	// A copied source maps to its workspace baseline. An issued editing
	// prefix already belongs to the native workspace and remains authoritative.
	if native.Name() != "git" && p.Base == r.Source {
		workspaceBase = s.ResourceStartIdentity(r)
	}
	message := "formal-gates candidate " + s.RunID + " " + intent.ActionID + " " + intent.PayloadDigest
	if native.Name() != "git" {
		observed, found, err := native.ObservedCommit(workspaceBase, message)
		if err != nil {
			return nativeResult{}, false, err
		}
		status, err := native.Status()
		if err != nil {
			return nativeResult{}, false, err
		}
		if found && (observed != head || status != "") {
			if !mayWrite {
				return nativeResult{observedEffect: true}, false, nil
			}
			pending, err := native.Pending(workspaceBase, "")
			if err != nil {
				return nativeResult{}, false, err
			}
			if !pending.Clean {
				return nativeResult{}, false, native.VerifyReady()
			}
			if err = f.admission.write(func() error { *invoked = true; return native.Synchronize(observed) }); err != nil {
				return nativeResult{}, false, err
			}
			head, err = native.Resolve()
			if err != nil {
				return nativeResult{}, false, err
			}
		}
	}

	completed := p.Stage == "AUTHORING" && head == workspaceBase
	if head != workspaceBase {
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
			return nativeResult{}, false, fmt.Errorf("candidate workspace HEAD %s is not the issued freeze at %s", head, workspaceBase)
		}
		if err := native.IsAncestorOrEqual(workspaceBase, head); err != nil {
			return nativeResult{}, false, err
		}
		count, err := native.CommitCount(workspaceBase, head)
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
	if p.Stage == "REVIEW_READY" {
		if err := reviewedArtifact(s, p); err != nil {
			return nativeResult{}, false, err
		}
	}
	if !completed {
		changes, pending, err := native.ValidateDeclaredChanges(p.Base, paths)
		if err != nil {
			return nativeResult{}, false, err
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
			baseOwned, owned, err := f.testOwnership(s, native, p.Base, p.ReviewCases, p.TestFiles)
			if err != nil {
				return nativeResult{}, false, err
			}
			for _, path := range changes {
				if slices.ContainsFunc(pending.Changes, func(c vcs.Change) bool { return c.Path == path && c.IsDirectoryContainer(paths) }) {
					continue
				}
				entry, err := native.Entry(p.Base, path)
				if err != nil {
					return nativeResult{}, false, err
				}
				_, statErr := os.Lstat(filepath.Join(r.Path, filepath.FromSlash(path)))
				if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
					return nativeResult{}, false, statErr
				}
				if !testOwnedTransition(path, entry != "", statErr == nil, baseOwned, owned) && !reviewableArtifactPath(path, p.ReviewCases, p.TestFiles, baseOwned, owned) {
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

		pending, err = native.Pending(p.Base, "")
		if err != nil {
			return nativeResult{}, false, err
		}
		if !pending.Clean {
			if err := f.admission.write(func() error { *invoked = true; return native.Commit(message) }); err != nil {
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

	if r.Ref != "" {
		ref, err := f.vcs.Ref(r.Ref)
		if err != nil {
			return nativeResult{}, false, err
		}
		if ref != head {
			if ref != "" && ref != p.Base {
				return nativeResult{}, false, fmt.Errorf("candidate ref %s points to %s, expected its base %s or freeze %s", r.Ref, ref, p.Base, head)
			}
			if !mayWrite {
				return nativeResult{observedEffect: true}, false, nil
			}
			if err := f.admission.write(func() error {
				*invoked = true
				return f.vcs.Pin(r.Ref, head)
			}); err != nil {
				return nativeResult{}, false, err
			}
		}
	}
	changes, err := native.Changes(p.Base, head)
	if err != nil {
		return nativeResult{}, false, err
	}
	for _, change := range changes {
		if !slices.Contains(p.Paths, change.Path) && !change.IsDirectoryContainer(p.Paths) {
			return nativeResult{}, false, fmt.Errorf("candidate has an undeclared native change: %s", change.Path)
		}
	}
	evidence := protocol.CandidateEvidence{Changes: changes, ParametersDigest: intent.PayloadDigest, WorkspaceID: p.WorkspaceID, Identity: head, Digest: digest, Paths: p.Paths}
	if p.Stage == "QA_ARTIFACT" {
		baseOwned, owned, err := f.testOwnership(s, native, p.Base, p.ReviewCases, p.TestFiles)
		if err != nil {
			return nativeResult{}, false, err
		}
		for _, change := range changes {
			if !testOwnedChange(change, changes, baseOwned, owned) && !reviewableArtifactPath(change.Path, p.ReviewCases, p.TestFiles, baseOwned, owned) {
				return nativeResult{}, false, fmt.Errorf("whitebox change is not declared test material: %s", change.Path)
			}
		}
		// This isolated snapshot is review input; semantic test-only approval
		// belongs to the accepted independent case-set review.
	}
	if p.Stage == "REVIEW_READY" || p.RepairTask != nil {
		reviewBase := p.ReviewBase
		if reviewBase == "" {
			reviewBase = p.Base
		}
		changes, err := native.Changes(reviewBase, head)
		if err != nil {
			return nativeResult{}, false, err
		}
		baseOwned, owned, err := f.testOwnership(s, native, reviewBase, p.ReviewCases, p.TestFiles)
		if err != nil {
			return nativeResult{}, false, err
		}
		if p.Stage == "REVIEW_READY" {
			if err := f.reviewedArtifactOwnership(s, p, head, baseOwned, owned); err != nil {
				return nativeResult{}, false, err
			}
			evidence.TestOnly = true
			evidence.ProductionViewEqual = true
		}
		if p.MergeManifest != nil {
			if owned == nil {
				owned = map[string]bool{}
			}
			owned[".gates/results/"+s.RunID+".merge-cases.md"] = true
		}
		view := &protocol.GateReviewView{TestOwnedPaths: []string{}}
		for _, change := range changes {
			if testOwnedChange(change, changes, baseOwned, owned) {
				view.TestOwnedPaths = append(view.TestOwnedPaths, change.Path)
			}
		}
		evidence.Changes, evidence.GateView = changes, view
		if p.Stage == "REVIEW_READY" && s.RouteMode == "merge" {
			evidence.ChildViews, err = f.childValidationViews(s, head, owned)
			if err != nil {
				return nativeResult{}, false, err
			}
		}
	}
	return nativeResult{Candidate: &evidence}, true, nil
}

func testOwnedChange(change vcs.Change, changes []vcs.Change, base, candidate map[string]bool) bool {
	owned := func(c vcs.Change) bool {
		return testOwnedTransition(c.Path, c.BaseMode != "000000", c.Mode != "000000", base, candidate)
	}
	if owned(change) {
		return true
	}
	var paths []string
	for path, test := range candidate {
		if test {
			paths = append(paths, path)
		}
	}
	if !change.IsDirectoryContainer(paths) {
		return false
	}
	return !slices.ContainsFunc(changes, func(c vcs.Change) bool {
		return strings.HasPrefix(c.Path, change.Path+"/") && !owned(c) && !c.IsDirectoryContainer(paths)
	})
}

func testOwnedTransition(path string, baseExists, candidateExists bool, base, candidate map[string]bool) bool {
	b, bok := base[path]
	c, cok := candidate[path]
	return (baseExists || candidateExists) && (!baseExists || bok && b) && (!candidateExists || cok && c)
}

func (f *Facade) ownershipAt(s *protocol.State, identity string, testReferences ...string) (map[string]bool, error) {
	// A graph represents an immutable commit only when HEAD and all delivery
	// bytes match it. Mutable authoring workspaces cannot prove base ownership.
	var owned map[string]bool
	providers := []*vcs.Provider{f.vcs}
	ids := make([]string, 0, len(s.NativeResources))
	for id := range s.NativeResources {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r := s.NativeResources[id]
		if r.Status == "READY" && !s.ResourceCleanupPending(r.ID) {
			providers = append(providers, f.vcs.At(r.Path, r.Handle))
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
		graph, err := native.InputOwnership(testReferences...)
		if err != nil {
			return nil, err
		}
		if graph != nil {
			owned = graph
		}
		break
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" || receipt.Status != protocol.HostActionStatusExecuted && receipt.Status != protocol.HostActionStatusReconciled {
			continue
		}
		var evidence protocol.CandidateEvidence
		if decodeReceiptEvidence(receipt, &evidence) != nil || evidence.ParametersDigest != receipt.PayloadDigest {
			continue
		}
		paths := []string{}
		if evidence.GateView != nil {
			paths = append(paths, evidence.GateView.TestOwnedPaths...)
		}
		for _, path := range paths {
			if _, known := owned[path]; known {
				continue
			}
			original, err := f.vcs.Entry(evidence.Identity, path)
			if err != nil {
				return nil, err
			}
			current, err := f.vcs.Entry(identity, path)
			if err != nil {
				return nil, err
			}
			if original != "" && original == current {
				if owned == nil {
					owned = map[string]bool{}
				}
				owned[path] = true
			}
		}
	}
	return owned, nil
}

// Signed file references supplement native graph proof only for new files or
// existing endpoints already proved test-owned at the original base.
func (f *Facade) testOwnership(s *protocol.State, native *vcs.Provider, base string, references map[string]string, files []string) (map[string]bool, map[string]bool, error) {
	refs := []string{}
	for _, ref := range references {
		refs = append(refs, ref)
	}
	before, err := f.ownershipAt(s, base, refs...)
	if before == nil {
		before = map[string]bool{}
	}
	if err != nil {
		return nil, nil, err
	}
	after, err := native.InputOwnership(refs...)
	if err != nil {
		return nil, nil, err
	}
	if after == nil {
		after = map[string]bool{}
	}
	paths := artifactReferences(references, files)
	for _, file := range paths {
		if _, known := after[file]; known {
			continue
		}
		entry, err := native.Entry(base, file)
		if err != nil {
			return nil, nil, err
		}
		if entry != "" && !before[file] {
			continue
		}
		info, err := os.Lstat(filepath.Join(native.Root(), filepath.FromSlash(file)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if info.Mode().IsRegular() {
			after[file] = true
		}
	}
	return before, after, nil
}

func artifactReferences(references map[string]string, files []string) []string {
	paths := slices.Clone(files)
	for _, ref := range references {
		file, symbol, qualified := strings.Cut(ref, "::")
		if qualified && strings.TrimSpace(symbol) != "" && filepath.IsLocal(filepath.FromSlash(file)) {
			paths = append(paths, filepath.ToSlash(filepath.Clean(filepath.FromSlash(file))))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func reviewableArtifactPath(path string, references map[string]string, files []string, before, after map[string]bool) bool {
	if value, known := before[path]; known && !value {
		return false
	}
	if value, known := after[path]; known && !value {
		return false
	}
	return slices.Contains(artifactReferences(references, files), path)
}

// The original QA review owns semantic file classification. Native identities
// and exact artifact transitions are still verified by the candidate adapter.
func reviewedArtifact(s *protocol.State, p protocol.CandidateParameters) error {
	set := s.Validation.Sets[coverage.ReviewWhitebox]
	if set == nil || len(set.Paths) == 0 {
		return nil
	}
	artifact, issued := p.QAArtifacts[coverage.ReviewWhitebox]
	if !issued || artifact.Identity != set.Candidate || !slices.Equal(artifact.Paths, set.Paths) || set.Status != "FROZEN" || set.Review == nil || set.Review.Verdict() != coverage.StatusPass {
		return fmt.Errorf("test artifact has no accepted independent review for its exact candidate")
	}
	if !slices.ContainsFunc(set.ReviewHistory, func(prior protocol.CaseReviewEvidence) bool {
		return prior.Candidate == artifact.Identity && reflect.DeepEqual(prior.Review, *set.Review) && reflect.DeepEqual(prior.Manifest, set.Manifest)
	}) {
		return fmt.Errorf("test artifact review does not bind its actual source and material relations")
	}
	return nil
}

func (f *Facade) reviewedArtifactOwnership(s *protocol.State, p protocol.CandidateParameters, identity string, before, after map[string]bool) error {
	paths := []string{}
	for _, artifact := range p.QAArtifacts {
		paths = append(paths, artifact.Paths...)
	}
	if p.BlackboxManifest != nil {
		paths = append(paths, ".gates/results/"+s.RunID+".blackbox-cases.md")
	}
	if p.MergeManifest != nil {
		paths = append(paths, ".gates/results/"+s.RunID+".merge-cases.md")
	}
	changes, err := f.vcs.Changes(p.Base, identity)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if !slices.Contains(paths, change.Path) && !change.IsDirectoryContainer(paths) {
			return fmt.Errorf("final candidate changed production outside its reviewed artifacts: %s", change.Path)
		}
	}
	artifact, issued := p.QAArtifacts[coverage.ReviewWhitebox]
	if !issued {
		return nil
	}
	for _, path := range artifact.Paths {
		reviewed, err := f.vcs.Entry(artifact.Identity, path)
		if err != nil {
			return err
		}
		current, err := f.vcs.Entry(identity, path)
		if err != nil {
			return err
		}
		if reviewed != current {
			return fmt.Errorf("reviewed test artifact entry changed: %s", path)
		}
		base, err := f.vcs.Entry(p.ReviewBase, path)
		if err != nil {
			return err
		}
		if !reviewableArtifactPath(path, p.ReviewCases, p.TestFiles, before, after) && !testOwnedTransition(path, base != "", current != "", before, after) {
			return fmt.Errorf("reviewed artifact is not test material or is a known production input: %s", path)
		}
		if base != "" {
			before[path] = true
		}
		if current != "" {
			after[path] = true
		}
	}
	return nil
}
