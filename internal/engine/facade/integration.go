package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// integrationEvidence observes the original prefix before writing. Both an
// ordinary return and a lost-response observation admit this same typed proof.
func (f *Facade) integrationEvidence(s *protocol.State, intent protocol.HostActionIntent, p protocol.CandidateParameters, mayWrite bool, invoked *bool) (nativeResult, bool, error) {
	r, exists := s.NativeResources[p.WorkspaceID]
	if !exists || r.Status != "READY" || p.Integration == nil {
		return nativeResult{}, false, fmt.Errorf("merge requires its admitted workspace and child")
	}
	native := f.vcs.At(r.Path, r.Handle)
	incomingTree, err := native.TreeDigest(p.Integration.Incoming)
	if err != nil {
		return nativeResult{}, false, err
	}
	if incomingTree != p.Integration.IncomingTree {
		return nativeResult{}, false, fmt.Errorf("incoming child tree differs from its frozen receipt")
	}
	head, err := native.Resolve()
	if err != nil {
		return nativeResult{}, false, err
	}
	observation, err := native.ObserveIntegration(p.Base, p.Integration.Incoming)
	mergeHead := ""
	if native.Name() == "git" {
		mergeHead = observation.Lineage
	}
	if err != nil {
		return nativeResult{}, false, err
	}
	message := "formal-gates merge " + s.RunID + " " + intent.ActionID + " " + intent.PayloadDigest
	if p.Stage == "MERGE_FINISH" && native.Name() != "git" {
		observed, found, err := native.ObservedCommit(p.Base, message)
		if err != nil {
			return nativeResult{}, false, err
		}
		if found && observed != head {
			if err := native.VerifyIntegration(p.Base, p.Integration.Incoming, observed, message); err != nil {
				return nativeResult{}, false, err
			}
			if !mayWrite {
				return nativeResult{observedEffect: true}, false, nil
			}
			if err := f.admission.write(func() error { *invoked = true; return native.Synchronize(observed) }); err != nil {
				return nativeResult{}, false, err
			}
			return f.integrationEvidence(s, intent, p, mayWrite, invoked)
		}
	}
	if p.Stage == "MERGE_START" {
		if head != p.Base || mergeHead != "" && mergeHead != p.Integration.Incoming {
			return nativeResult{}, false, fmt.Errorf("merge start has a different native prefix")
		}
		included := native.IsAncestorOrEqual(p.Integration.Incoming, head) == nil
		if !observation.Started && !included {
			if !mayWrite {
				return nativeResult{}, false, nil
			}
			if err := native.VerifyReady(); err != nil {
				return nativeResult{}, false, err
			}
			if err := f.admission.write(func() error { *invoked = true; return native.Merge(p.Integration.Incoming) }); err != nil {
				return nativeResult{}, false, err
			}
			observation, err = native.ObserveIntegration(p.Base, p.Integration.Incoming)
			if native.Name() == "git" {
				mergeHead = observation.Lineage
			}
			if err != nil {
				return nativeResult{}, false, err
			}
		}
		proof := &protocol.IntegrationEvidence{Prefix: p.Base, Incoming: p.Integration.Incoming, MergeHead: mergeHead, NativeLineage: observation.Lineage, NativeStarted: observation.Started, Index: observation.Entries}
		for _, entry := range observation.Entries {
			if entry.Stage == 0 {
				continue
			}
			position := slices.IndexFunc(proof.Conflicts, func(c protocol.MergeConflict) bool { return c.Path == entry.Path })
			if position < 0 {
				path := filepath.Join(r.Path, filepath.FromSlash(entry.Path))
				body, err := os.ReadFile(path)
				if err != nil && !os.IsNotExist(err) {
					return nativeResult{}, false, err
				}
				var mode uint32
				if info, err := os.Lstat(path); err == nil {
					mode = uint32(info.Mode())
				} else if !os.IsNotExist(err) {
					return nativeResult{}, false, err
				}
				text := !bytes.Contains(body, []byte{0}) && bytes.Contains(body, []byte("<<<<<<< ")) && bytes.Contains(body, []byte("=======\n"))
				proof.Conflicts = append(proof.Conflicts, protocol.MergeConflict{Path: entry.Path, Content: body, Mode: mode, Text: text})
				position = len(proof.Conflicts) - 1
			}
			proof.Conflicts[position].Entries = append(proof.Conflicts[position].Entries, entry)
		}
		return mergeNativeResult(native, p, intent, head, proof)
	}
	var start protocol.CandidateEvidence
	if err := decodeReceiptEvidence(s.HostActionReceipts[p.Integration.StartAction], &start); err != nil || start.Integration == nil {
		return nativeResult{}, false, fmt.Errorf("merge completion has no admitted start evidence: %w", err)
	}
	if head != p.Base {
		observed, err := native.Message(head)
		if err != nil {
			return nativeResult{}, false, err
		}

		if err := native.VerifyIntegration(p.Base, p.Integration.Incoming, head, message); err != nil {
			return nativeResult{}, false, err
		}
		if observed != message || mergeHead != "" {
			return nativeResult{}, false, fmt.Errorf("merge commit is not the exact issued operation")
		}

	} else if start.Integration.NativeStarted || start.Integration.MergeHead != "" {
		if native.Name() == "git" && mergeHead != p.Integration.Incoming {
			return nativeResult{}, false, fmt.Errorf("merge completion lost its original incoming identity")
		}
		if !mayWrite {
			return nativeResult{}, false, nil
		}
		if err := validateConflictEdits(native, r.Path, *start.Integration); err != nil {
			return nativeResult{}, false, err
		}
		paths := []string{}
		for _, conflict := range start.Integration.Conflicts {
			paths = append(paths, conflict.Path)
		}
		if err := f.admission.write(func() error { *invoked = true; return native.ResolveConflicts(paths) }); err != nil {
			return nativeResult{}, false, err
		}
		entries, err := native.IndexEntries()
		if err != nil {
			return nativeResult{}, false, err
		}
		if slices.ContainsFunc(entries, func(e vcs.IndexEntry) bool { return e.Stage != 0 }) {
			return nativeResult{}, false, fmt.Errorf("merge resolution remains unmerged")
		}
		if err := f.admission.write(func() error { *invoked = true; return native.Commit(message) }); err != nil {
			return nativeResult{}, false, err
		}
		head, err = native.Resolve()
		if err != nil {
			return nativeResult{}, false, err
		}
	} else if native.IsAncestorOrEqual(p.Integration.Incoming, head) != nil {
		return nativeResult{}, false, fmt.Errorf("no-change merge does not include its child")
	}
	if err := native.VerifyReady(); err != nil {
		return nativeResult{}, false, err
	}

	if r.Ref != "" {
		ref, err := f.vcs.Ref(r.Ref)
		if err != nil {
			return nativeResult{}, false, err
		}
		if ref != head {
			if ref != p.Base {
				return nativeResult{}, false, fmt.Errorf("merge ref differs from its original prefix")
			}
			if !mayWrite {
				return nativeResult{observedEffect: true}, false, nil
			}
			if err := f.admission.write(func() error { *invoked = true; return f.vcs.Pin(r.Ref, head) }); err != nil {
				return nativeResult{}, false, err
			}
		}
	}
	entries, err := native.IndexEntries()
	if err != nil {
		return nativeResult{}, false, err
	}
	proof := &protocol.IntegrationEvidence{Prefix: p.Base, Incoming: p.Integration.Incoming, NativeLineage: observation.Lineage, Index: entries}
	return mergeNativeResult(native, p, intent, head, proof)
}

func decodeReceiptEvidence(receipt protocol.HostActionReceipt, target any) error {
	if receipt.AdapterEvidence == nil {
		return fmt.Errorf("native receipt has no typed evidence")
	}
	data, err := json.Marshal(receipt.AdapterEvidence.Values)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func mergeNativeResult(native *vcs.Provider, p protocol.CandidateParameters, intent protocol.HostActionIntent, head string, proof *protocol.IntegrationEvidence) (nativeResult, bool, error) {
	digest, err := native.TreeDigest(head)
	if err != nil {
		return nativeResult{}, false, err
	}
	changes, err := native.Changes(p.Base, head)
	if err != nil {
		return nativeResult{}, false, err
	}
	return nativeResult{Candidate: &protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: p.WorkspaceID, Identity: head, Digest: digest, Paths: p.Paths, Changes: changes, Integration: proof}}, true, nil
}

// Automatically merged index entries and conflict-file context are immutable.
// Binary and delete/modify conflicts permit a semantic choice for the whole file.
func validateConflictEdits(native *vcs.Provider, root string, proof protocol.IntegrationEvidence) error {
	conflicts := map[string]protocol.MergeConflict{}
	for _, conflict := range proof.Conflicts {
		conflicts[conflict.Path] = conflict
	}
	working, err := native.WorkingChanges()
	if err != nil {
		return err
	}
	for _, path := range working {
		if native.Name() != "git" && slices.ContainsFunc(proof.Index, func(e vcs.IndexEntry) bool { return e.Path == path }) {
			continue
		}
		if _, allowed := conflicts[path]; !allowed {
			return fmt.Errorf("resolution edited non-conflict path: %s", path)
		}
	}
	entries, err := native.IndexEntries()
	if err != nil {
		return err
	}
	unchanged := func(entries []vcs.IndexEntry) []vcs.IndexEntry {
		result := []vcs.IndexEntry{}
		for _, entry := range entries {
			if _, conflict := conflicts[entry.Path]; !conflict {
				result = append(result, entry)
			}
		}
		return result
	}
	if !slices.Equal(unchanged(entries), unchanged(proof.Index)) {
		return fmt.Errorf("resolution changed automatically merged index content")
	}
	for _, conflict := range proof.Conflicts {
		if !conflict.Text {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(conflict.Path)))
		if err != nil {
			return err
		}
		if err := preserveConflictContext(conflict.Content, body); err != nil {
			return fmt.Errorf("resolution outside conflict content in %s: %w", conflict.Path, err)
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(conflict.Path)))
		if err != nil {
			return err
		}
		if uint32(info.Mode()) != conflict.Mode {
			return fmt.Errorf("text resolution changed file mode: %s", conflict.Path)
		}
	}
	return nil
}

func preserveConflictContext(original, resolved []byte) error {
	var fixed []string
	var part strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(string(original), "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") {
			fixed = append(fixed, part.String())
			part.Reset()
			inside = true
			continue
		}
		if inside && strings.HasPrefix(line, ">>>>>>> ") {
			inside = false
			continue
		}
		if !inside {
			part.WriteString(line)
		}
	}
	if inside || len(fixed) == 0 {
		return fmt.Errorf("native conflict markers are incomplete")
	}
	fixed = append(fixed, part.String())
	text := string(resolved)
	if !strings.HasPrefix(text, fixed[0]) || !strings.HasSuffix(text, fixed[len(fixed)-1]) {
		return fmt.Errorf("non-conflict prefix or suffix changed")
	}
	text = strings.TrimPrefix(text, fixed[0])
	text = strings.TrimSuffix(text, fixed[len(fixed)-1])
	for _, context := range fixed[1 : len(fixed)-1] {
		replacement, after, found := strings.Cut(text, context)
		if !found {
			return fmt.Errorf("non-conflict context changed")
		}
		if err := resolvedConflictContent(replacement); err != nil {
			return err
		}
		text = after
	}
	return resolvedConflictContent(text)
}

// The fixed contexts may contain ordinary separator text. Within the edited
// spans, opening/base/closing delimiters still identify unresolved native hunks.
func resolvedConflictContent(text string) error {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, "||||||| ") || strings.HasPrefix(line, ">>>>>>> ") {
			return fmt.Errorf("conflict markers remain")
		}
	}
	return nil
}
