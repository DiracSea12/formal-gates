package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/vcs"
)

// terminalSnapshotGuard 是 regular COMPLETE 终态发布段的身份边界，单一规则：
// sealDelivered 已验证的交付身份（head==PromotedSnapshot）贯穿 seal 窗口与发布
// 窗口——发布前 resolve 必须仍等于该晋升身份（窗口内漂移即错、摘要不落盘），
// 返回的 guard 在终态发布（摘要落盘）后调用，核对发布段内身份未移动。
// guard 不持第二套判据：交付内容与历
// 史形状的核验统一由 verifyDelivered 单点持有。无 provider＝明确报错，不静默跳
// 过校验。
func (f *Facade) terminalSnapshotGuard(run Run) (func() error, error) {
	if run.Route != "regular" || run.Status != "COMPLETE" {
		return func() error { return nil }, nil
	}
	if f.vcs == nil {
		return nil, fmt.Errorf("engine terminal: regular run %q has no native VCS provider for snapshot verification", f.runID)
	}
	head, err := f.vcs.Resolve()
	if err != nil {
		return nil, err
	}
	if head != run.PromotedSnapshot {
		return nil, fmt.Errorf("engine terminal: native VCS identity %s does not match the promoted snapshot %s", head, run.PromotedSnapshot)
	}
	return func() error {
		after, err := f.vcs.Resolve()
		if err != nil {
			return err
		}
		if after != head {
			return fmt.Errorf("native VCS identity moved during terminal publication")
		}
		return nil
	}, nil
}

// promotionLockDescription 是晋升执行窗互斥锁的用途描述（锁诊断用）。
const promotionLockDescription = "candidate promotion window"

// promotionLockPath 是晋升执行窗互斥锁（等待式文件锁）的路径：放在 run 状态
// 目录内（.gates/engine/ 属 porcelain 的运行期未跟踪排除清单，锁文件不污染
// 就绪检查）。Drive、PrepareCandidate 的原生执行与观察共用此窗口；公开新事件的
// 接纳在同一把锁上等待至窗口关闭（等待式文件锁本
// 体：持锁冲突时阻塞、由内核在持锁释放时唤醒的等待者，与 owner 区间串行使
// 用同款原语）。
func (f *Facade) promotionLockPath() string {
	return filepath.Join(f.stateDir, "promotion.lock")
}

// acquireNativeWindow observes the original installation binding before waiting.
// Every actual write rechecks it under the registry lock in Admission.acquire.
func (f *Facade) acquireNativeWindow() (func(), error) {
	if err := verifyAdmissionSnapshot(f.admission); err != nil {
		return nil, err
	}
	return coordination.AcquireExistingPathWaiting(f.promotionLockPath(), promotionLockDescription)
}

// PrepareCandidate completes the signed native effect for the current FULL
// candidate. Observation and execution use the same handler as Drive recovery.
func (f *Facade) PrepareCandidate(message string) (string, error) {
	if f.vcs == nil {
		return "", fmt.Errorf("candidate promotion requires a regular run with a native VCS provider")
	}
	unlock, err := f.acquireNativeWindow()
	if err != nil {
		return "", err
	}
	defer unlock()
	return f.prepareCandidate(message)
}
func (f *Facade) prepareCandidate(message string) (string, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return "", err
	}
	s := snap.State
	if err := s.AdmitPromotion(f.definition); err != nil {
		return "", err
	}
	if s.DeliveryComplete(f.definition) {
		tree, err := f.vcs.TreeDigest("HEAD")
		if err != nil {
			return "", err
		}
		if tree != s.PromotedDigest {
			return "", fmt.Errorf("candidate content does not match the verified final candidate")
		}
		head, _, err := f.verifyDelivered()
		return head, err
	}
	source := s.Validation.Waves[len(s.Validation.Waves)-1].Candidate
	intent, pending := s.PromotionIntent()
	if pending {
		var old protocol.CandidateParameters
		if err := decodeNativeParams(intent, &old); err != nil {
			return "", err
		}
		if old.Promotion.Source != source.Identity || old.Promotion.SourceDigest != source.Digest || len(s.HostActionFailures[intent.ActionID]) != 0 {
			result, complete, err := f.nativeEvidence(s, intent, false)
			if err != nil {
				return "", err
			}
			if complete {
				if err := f.engine.RecordCandidatePromotion(intent.ActionID, *result.Candidate.Promotion, emptyFingerprint()); err != nil {
					return "", err
				}
				return f.prepareCandidate(message)
			}
			if _, err := f.promotionPrefix(*old.Promotion); err != nil {
				return "", err
			}
			pending = false
		}
	}
	if !pending {
		observed, err := f.observePromotion()
		if err != nil {
			return "", err
		}
		p := protocol.PromotionParameters{Source: source.Identity, SourceDigest: source.Digest, TargetRoot: f.vcs.Root(), TargetBase: s.BaseSnapshot, TargetStart: observed, Message: strings.TrimSpace(message)}
		if s.NativeRoot != "" {
			p.Message = "formal-gates delivery " + f.runID + " " + s.BaseSnapshot + " " + source.Identity
		}
		if p.Message == "" {
			count, err := f.vcs.CommitCount(s.BaseSnapshot, observed.Head)
			if err != nil {
				return "", err
			}
			if s.SliceChild == nil && count > 1 {
				return "", fmt.Errorf("candidate promotion requires a squash message for multiple commits")
			}
			p.Message = "formal-gates delivery " + f.runID + " " + s.BaseSnapshot + " " + source.Identity
		}
		if intent.ActionID != "" {
			p.SupersedesActionID = intent.ActionID
		} else {
			expected := s.AcceptedNativeSnapshot()
			if observed.Head != expected {
				return "", fmt.Errorf("promotion target drifted from its accepted delivery %s to %s", expected, observed.Head)
			}
			if !observed.Clean {
				return "", fmt.Errorf("promotion target has unsubmitted native changes")
			}
		}
		for _, r := range s.NativeResources {
			if r.Role == "candidate" && r.Identity == source.Identity && r.Status == "READY" && !s.ResourceCleanupPending(r.ID) {
				p.SourceWorkspace = r.ID
				break
			}
		}
		if s.NativeRoot != "" && p.SourceWorkspace == "" {
			return "", fmt.Errorf("candidate promotion requires its registered source workspace")
		}
		if err := f.engine.OpenPromotion(p, emptyFingerprint()); err != nil {
			return "", err
		}
		snap, err = f.engine.Load()
		if err != nil {
			return "", err
		}
		s = snap.State
		intent, pending = s.PromotionIntent()
	}
	if !pending {
		return "", fmt.Errorf("candidate promotion has no signed intent")
	}
	if !s.NativeActionCurrent(intent, f.definition) {
		return "", fmt.Errorf("candidate promotion is not current")
	}
	result, complete, err := f.nativeEvidence(s, intent, true)
	if err != nil {
		return "", err
	}
	if !complete {
		return "", fmt.Errorf("candidate promotion remains incomplete")
	}
	proof := *result.Candidate.Promotion
	if err := f.engine.RecordCandidatePromotion(intent.ActionID, proof, emptyFingerprint()); err != nil {
		return "", err
	}
	return proof.Final, nil
}

func (f *Facade) observePromotion() (protocol.PromotionObservation, error) {
	return observeNativePrefix(f.vcs)
}

func observeNativePrefix(native *vcs.Provider) (protocol.PromotionObservation, error) {
	var o protocol.PromotionObservation
	head, err := native.Resolve()
	if err != nil {
		return o, err
	}
	o.Head = head
	if o.Tree, err = native.TreeDigest(head); err != nil {
		return o, err
	}

	pending, err := native.Pending(head, "")
	if err != nil {
		return o, err
	}
	if native.Name() == "git" {
		o.IndexTree = pending.Fingerprint
	} else {
		o.PendingFingerprint = pending.Fingerprint
	}
	o.Changes, o.Clean, o.StagedOnly = pending.Changes, pending.Clean, pending.Tracked

	return o, err
}

// promotionPrefix accepts only the signed initial state or exact native
// prefixes whose entries and organization are explained by this responsibility.
func (f *Facade) promotionPrefix(p protocol.PromotionParameters) (protocol.PromotionObservation, error) {
	if f.vcs.Name() != "git" {
		o, err := f.observePromotion()
		if err != nil {
			return o, err
		}
		digest, err := f.vcs.TreeDigest(p.Source)
		if err != nil {
			return o, err
		}
		if digest != p.SourceDigest {
			return o, fmt.Errorf("promotion source values differ from verified candidate")
		}
		if err = f.vcs.IsAncestorOrEqual(p.TargetBase, o.Head); err != nil {
			return o, err
		}
		pending, err := f.vcs.Pending(p.TargetStart.Head, p.Source)
		if err != nil {
			return o, err
		}
		if o.Head == p.TargetStart.Head {
			expected, err := f.vcs.Changes(p.TargetStart.Head, p.Source)
			if err != nil {
				return o, err
			}
			for _, change := range pending.Changes {
				if !slices.ContainsFunc(expected, func(e vcs.Change) bool { return e.Path == change.Path }) {
					return o, fmt.Errorf("promotion has an unexplained native change: %s", change.Path)
				}
			}
			if !pending.Clean && (!pending.Tracked || !pending.Prefix) {
				return o, fmt.Errorf("promotion has unopened or untracked changes")
			}
			return o, nil
		}
		message, err := f.vcs.Message(o.Head)
		if err != nil {
			return o, err
		}
		if message != p.Message || !o.Clean || o.Tree != p.SourceDigest {
			return o, fmt.Errorf("native promotion does not prove the issued operation")
		}
		return o, nil
	}

	o, err := f.observePromotion()
	if err != nil {
		return o, err
	}
	tree, err := f.vcs.TreeDigest(p.Source)
	if err != nil {
		return o, err
	}
	if tree != p.SourceDigest {
		return o, fmt.Errorf("candidate promotion source does not match the verified final candidate")
	}
	if err := f.vcs.IsAncestorOrEqual(p.TargetBase, o.Head); err != nil {
		return o, err
	}
	if !o.Clean && !o.StagedOnly {
		return o, fmt.Errorf("candidate promotion cannot resume over changes it did not stage (unstaged or untracked)")
	}
	if o.Head == p.TargetStart.Head {
		expected, err := f.vcs.Changes(p.TargetStart.IndexTree, p.Source)
		if err != nil {
			return o, err
		}
		actual, err := f.vcs.Changes(p.TargetStart.IndexTree, o.IndexTree)
		if err != nil {
			return o, err
		}
		for _, change := range actual {
			if !slices.Contains(expected, change) {
				return o, fmt.Errorf("promotion contains an unexplained staged change: %s", change.Path)
			}
		}
		return o, nil
	}
	if o.IndexTree != p.SourceDigest || o.Clean && o.Tree != p.SourceDigest {
		return o, fmt.Errorf("promotion target does not match the verified final candidate")
	}
	if o.Head == p.TargetBase || o.Head == p.Source {
		return o, nil
	}
	message, err := f.vcs.Message(o.Head)
	if err != nil {
		return o, err
	}
	count, err := f.vcs.CommitCount(p.TargetBase, o.Head)
	if err != nil {
		return o, err
	}
	if message != p.Message {
		return o, fmt.Errorf("promotion target has no signed organization message")
	}
	if count != 1 {
		if err := f.vcs.IsAncestorOrEqual(p.TargetStart.Head, o.Head); err != nil {
			return o, err
		}
		delta, err := f.vcs.CommitCount(p.TargetStart.Head, o.Head)
		if err != nil {
			return o, err
		}
		if delta != 1 {
			return o, fmt.Errorf("promotion has an unexpected organization range")
		}
	}
	return o, nil
}

func (f *Facade) promotionEvidence(s *protocol.State, intent protocol.HostActionIntent, mayWrite bool, invoked *bool) (nativeResult, bool, error) {
	var params protocol.CandidateParameters
	if err := decodeNativeParams(intent, &params); err != nil {
		return nativeResult{}, false, err
	}
	if params.Promotion == nil {
		return nativeResult{}, false, fmt.Errorf("promotion has no typed parameters")
	}
	p := *params.Promotion
	if f.vcs.Name() != "git" {
		return f.nativePromotionEvidence(s, intent, params, mayWrite, invoked)
	}

	o, err := f.promotionPrefix(p)
	if err != nil {
		if latest, exists := s.PromotionIntent(); exists && latest.ActionID != intent.ActionID {
			chain, chainErr := s.PromotionChain(latest)
			if chainErr == nil && slices.ContainsFunc(chain, func(original protocol.HostActionIntent) bool { return original.ActionID == intent.ActionID }) {
				var current protocol.CandidateParameters
				if decodeErr := decodeNativeParams(latest, &current); decodeErr != nil {
					return nativeResult{}, false, decodeErr
				}
				if _, prefixErr := f.promotionPrefix(*current.Promotion); prefixErr == nil {
					return nativeResult{}, false, nil
				}
			}
		}
		return nativeResult{}, false, err
	}
	count, err := f.vcs.CommitCount(p.TargetBase, o.Head)
	if err != nil {
		return nativeResult{}, false, err
	}
	complete := o.Clean && o.Tree == p.SourceDigest && o.IndexTree == p.SourceDigest && (s.SliceChild != nil || count <= 1)
	if !complete {
		if !mayWrite {
			return nativeResult{}, false, nil
		}
		if !s.NativeActionWritable(intent, f.definition) {
			return nativeResult{}, false, fmt.Errorf("native responsibility has no current execution allowance")
		}
		if p.SourceWorkspace != "" {
			r, ok := s.NativeResources[p.SourceWorkspace]
			if !ok || r.Identity != p.Source || r.Status != "READY" || s.ResourceCleanupPending(r.ID) {
				return nativeResult{}, false, fmt.Errorf("promotion source workspace is not available")
			}
			native := f.vcs.At(r.Path, r.Handle)
			if err := native.VerifyReady(); err != nil {
				return nativeResult{}, false, err
			}
			head, err := native.Resolve()
			if err != nil {
				return nativeResult{}, false, err
			}
			if head != p.Source {
				return nativeResult{}, false, fmt.Errorf("promotion source identity drifted")
			}
		}
		if o.IndexTree != p.SourceDigest {
			if o.Clean && o.Head == p.TargetBase && p.SupersedesActionID == "" && p.TargetStart.Head == p.TargetBase {
				if err := f.admission.write(func() error {
					*invoked = true
					return f.vcs.FastForward(p.Source)
				}); err != nil {
					return nativeResult{}, false, err
				}
			} else {
				changes, err := f.vcs.Changes(o.IndexTree, p.Source)
				if err != nil {
					return nativeResult{}, false, err
				}
				paths := make([]string, 0, len(changes))
				for _, change := range changes {
					paths = append(paths, change.Path)
				}
				if err := f.admission.write(func() error {
					*invoked = true
					return f.vcs.ApplyArtifact(p.Source, paths)
				}); err != nil {
					return nativeResult{}, false, err
				}
			}
			o, err = f.observePromotion()
			if err != nil {
				return nativeResult{}, false, err
			}
		}
		if o.IndexTree != p.SourceDigest {
			return nativeResult{}, false, fmt.Errorf("promotion index does not match the verified final candidate")
		}
		if !o.Clean {
			if !o.StagedOnly {
				return nativeResult{}, false, fmt.Errorf("promotion has unstaged or untracked changes")
			}
			if err := f.admission.write(func() error {
				*invoked = true
				return f.vcs.Commit(p.Message)
			}); err != nil {
				return nativeResult{}, false, err
			}
			o, err = f.observePromotion()
			if err != nil {
				return nativeResult{}, false, err
			}
		}
		count, err = f.vcs.CommitCount(p.TargetBase, o.Head)
		if err != nil {
			return nativeResult{}, false, err
		}
		if s.SliceChild == nil && count > 1 {
			if err := f.admission.write(func() error {
				*invoked = true
				return f.vcs.SquashToBase(p.TargetBase, p.Message)
			}); err != nil {
				return nativeResult{}, false, err
			}
			o, err = f.observePromotion()
			if err != nil {
				return nativeResult{}, false, err
			}
		}
		count, err = f.vcs.CommitCount(p.TargetBase, o.Head)
		if err != nil {
			return nativeResult{}, false, err
		}
		if !o.Clean || o.Tree != p.SourceDigest || o.IndexTree != p.SourceDigest || s.SliceChild == nil && count > 1 {
			return nativeResult{}, false, fmt.Errorf("promotion final native tree is incomplete")
		}
	}
	proof := &protocol.CandidatePromotionReceipt{OperationID: intent.ActionID, Provider: f.vcs.Name(), SourceWorkspace: p.SourceWorkspace, Source: p.Source, SourceDigest: p.SourceDigest, TargetRoot: p.TargetRoot, TargetBase: p.TargetBase, TargetStart: p.TargetStart, SupersedesActionID: p.SupersedesActionID, Final: o.Head, FinalDigest: o.Tree}
	return nativeResult{Candidate: &protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: params.WorkspaceID, Identity: o.Head, Digest: o.Tree, Promotion: proof}}, true, nil
}

// RegisterQAWorktree validates a base workspace and its confirmed current
// artifacts. Native workflow resources are created and owned by Drive.
func (f *Facade) RegisterQAWorktree(worktree string) error {
	if f.vcs == nil {
		return fmt.Errorf("QA worktree registration requires a regular run with a native VCS provider")
	}
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return fmt.Errorf("QA isolation worktree path is required")
	}
	worktree, err = filepath.Abs(worktree)
	if err != nil {
		return err
	}
	resolved, err := f.vcs.At(worktree).Resolve()
	if err != nil {
		return fmt.Errorf("cannot resolve QA isolation worktree: %w", err)
	}
	if resolved != snap.State.BaseSnapshot {
		return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, snap.State.BaseSnapshot)
	}
	receipt := snap.State.IntakeConfirmationReceipt
	if receipt == nil {
		return fmt.Errorf("QA worktree registration requires the run's intake artifacts")
	}
	requirementPath := filepath.Clean(receipt.RequirementSource)
	if filepath.IsAbs(requirementPath) {
		requirementPath, err = filepath.Rel(f.root, requirementPath)
		if err != nil {
			return err
		}
	}
	for _, artifact := range receipt.Artifacts {
		expected := artifact.Revision
		if filepath.Clean(filepath.FromSlash(artifact.Path)) == requirementPath {
			expected = strings.TrimPrefix(snap.State.RequirementRevision(), "sha256:")
		}
		path := filepath.Join(worktree, filepath.FromSlash(artifact.Path))
		revision, err := requirementArtifactRevision(path)
		if err != nil {
			return fmt.Errorf("QA isolation worktree requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != expected {
			return fmt.Errorf("QA isolation worktree requirement artifact %s does not match the run revision %s", artifact.Path, expected)
		}
	}
	return f.engine.RecordQAWorktree(worktree, emptyFingerprint())
}

// requirementArtifactRevision uses the intake artifact hash language: bare
// hexadecimal SHA256 with CRLF normalized to LF.
func requirementArtifactRevision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return requirementContentRevision(data), nil
}

func requirementContentRevision(data []byte) string {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// Native promotion materializes the verified full value through the same
// candidate operation and receipt admission. Immutable values are read after
// submit, including a submit whose process response was lost.
func (f *Facade) nativePromotionEvidence(s *protocol.State, intent protocol.HostActionIntent, params protocol.CandidateParameters, mayWrite bool, invoked *bool) (nativeResult, bool, error) {
	p := *params.Promotion
	head, err := f.vcs.Resolve()
	if err != nil {
		return nativeResult{}, false, err
	}
	observed, found, err := f.vcs.ObservedCommit(p.TargetStart.Head, p.Message)
	if err != nil {
		return nativeResult{}, false, err
	}
	if found {
		digest, err := f.vcs.TreeDigest(observed)
		if err != nil {
			return nativeResult{}, false, err
		}
		if digest != p.SourceDigest {
			return nativeResult{}, false, fmt.Errorf("observed native promotion values differ from candidate")
		}
	}
	if found && observed != head {
		if !mayWrite {
			return nativeResult{observedEffect: true}, false, nil
		}
		if _, err := f.promotionPrefix(p); err != nil {
			return nativeResult{}, false, err
		}
		if err = f.admission.write(func() error { *invoked = true; return f.vcs.Synchronize(observed) }); err != nil {
			return nativeResult{}, false, err
		}
	}
	o, err := f.promotionPrefix(p)
	if err != nil {
		return nativeResult{}, false, err
	}
	complete := o.Clean && o.Tree == p.SourceDigest
	if !complete {
		if !mayWrite {
			return nativeResult{}, false, nil
		}
		changes, err := f.vcs.Changes(p.TargetStart.Head, p.Source)
		if err != nil {
			return nativeResult{}, false, err
		}
		paths := make([]string, 0, len(changes))
		for _, change := range changes {
			paths = append(paths, change.Path)
		}
		if err = f.admission.write(func() error { *invoked = true; return f.vcs.ApplyArtifact(p.Source, paths) }); err != nil {
			return nativeResult{}, false, err
		}
		pending, err := f.vcs.Pending(p.TargetStart.Head, p.Source)
		if err != nil {
			return nativeResult{}, false, err
		}
		if !pending.Equal {
			return nativeResult{}, false, fmt.Errorf("native promotion pending values differ from candidate")
		}
		if !pending.Clean {
			if err = f.admission.write(func() error { *invoked = true; return f.vcs.Commit(p.Message) }); err != nil {
				return nativeResult{}, false, err
			}
		}
		o, err = f.observePromotion()
		if err != nil {
			return nativeResult{}, false, err
		}
		if !o.Clean || o.Tree != p.SourceDigest {
			return nativeResult{}, false, fmt.Errorf("native promotion final values differ from candidate")
		}
	}
	proof := &protocol.CandidatePromotionReceipt{OperationID: intent.ActionID, Provider: f.vcs.Name(), SourceWorkspace: p.SourceWorkspace, Source: p.Source, SourceDigest: p.SourceDigest, TargetRoot: p.TargetRoot, TargetBase: p.TargetBase, TargetStart: p.TargetStart, SupersedesActionID: p.SupersedesActionID, Final: o.Head, FinalDigest: o.Tree}
	return nativeResult{Candidate: &protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: params.WorkspaceID, Identity: o.Head, Digest: o.Tree, Promotion: proof}}, true, nil
}
