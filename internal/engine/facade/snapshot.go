package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// terminalSnapshotGuard 是 regular COMPLETE 终态发布段的身份恒等边界：发布
// 前 resolve 记录原生身份，返回的 guard 在终态发布（摘要落盘）后调用，核对
// 发布段内身份未移动（镜像 legacy Seal 发布窗口的前后双 resolve）。交付内容
// 与历史形状的核验统一由 sealDelivered 单点持有，guard 不持第二套判据。无
// provider＝明确报错，不静默跳过校验。
func (f *Facade) terminalSnapshotGuard(run Run) (func() error, error) {
	if run.Route != "regular" || run.Status != "COMPLETE" {
		return func() error { return nil }, nil
	}
	if f.vcs == nil {
		return nil, fmt.Errorf("engine terminal: regular run %q has no git provider for snapshot verification", f.runID)
	}
	head, err := f.vcs.Resolve()
	if err != nil {
		return nil, err
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

// PrepareCandidate 组织终态候选（镜像 legacy PrepareCandidate：区间单提交＝确
// 认当前 HEAD；多提交需 squash 消息，squash 后重解析绑定；晋升后区间再度累
// 积提交时可重复组织）。晋升走完整副作用协议 intent→execute→observe→
// commit：内容前置门先核对净内容（暂存树——干净工作区即 HEAD 树）等于已验
// 证最终候选 digest，squash 与续行 commit 只组织已验证内容，漂移零 Git 副作
// 用拒绝；OpenPromotion 先过接纳（活动/无屏障/无 abort/wave COMPLETE），被拒
// 时零 Git 副作用；意图存活期间重入＝续行而非重开，reset 已完成、commit 未
// 执行的中断由暂存树识别并以 Commit 完成组织；RecordCandidatePromotion 消费
// 意图原子提交，接纳被屏障/终态撤销且本次调用改写过历史时 soft reset 回意图
// 目标补偿（净零历史改写）。已在位且 HEAD==PromotedSnapshot 时幂等直返。工
// 作区必须就绪（未提交内容拒绝——与 legacy verifySnapshotReady 同语义；
// track/commit 是候选 freeze 的宿主侧动作，不在此自动执行）。
func (f *Facade) PrepareCandidate(squashMessage string) (string, error) {
	if f.vcs == nil {
		return "", fmt.Errorf("candidate promotion requires a regular run with a git provider")
	}
	snap, err := f.engine.Load()
	if err != nil {
		return "", err
	}
	head, err := f.vcs.Resolve()
	if err != nil {
		return "", err
	}
	if snap.State.PromotedSnapshot == head {
		return head, nil
	}
	// 内容前置门（先于任何 Git 写）：净内容（暂存树——干净工作区即 HEAD
	// 树）必须等于当前终 wave 候选 digest；不等＝内容漂移，错误上抛、零 Git
	// 副作用（恢复＝恢复内容后重入）。
	staged, err := f.vcs.StagedTreeDigest()
	if err != nil {
		return "", err
	}
	var verified string
	if snap.State.Validation != nil && len(snap.State.Validation.Waves) > 0 {
		verified = snap.State.Validation.Waves[len(snap.State.Validation.Waves)-1].Candidate.Digest
	}
	if staged != verified {
		return "", fmt.Errorf("candidate promotion content %s does not match the verified final candidate %s", staged, verified)
	}
	// 未决意图存活期间重入＝续行而非重开（重试不得覆盖未决目标）；为空才
	// OpenPromotion——准入在先，被拒时零 Git 副作用。
	intent := snap.State.PendingPromotion
	if intent == "" {
		if err := f.engine.OpenPromotion(head, emptyFingerprint()); err != nil {
			return "", err
		}
		intent = head
	}
	wrote := false
	if err := f.vcs.VerifyReady(); err != nil {
		// 意图目标之后的脏树且暂存树＝已验证候选（前置门已核）＝本意图组织
		// 的中断中间态：reset 已完成、commit 未执行（soft reset 保树保
		// index），以 Commit 完成组织并绑定原意图。head 仍在意图目标上的脏树
		// 不是本操作的中间态，原样返回就绪错误。
		if head == intent {
			return "", err
		}
		message := strings.TrimSpace(squashMessage)
		if message == "" {
			return "", fmt.Errorf("candidate promotion requires a squash message for multiple commits")
		}
		if err := f.vcs.Commit(message); err != nil {
			return "", err
		}
		wrote = true
		if head, err = f.vcs.Resolve(); err != nil {
			return "", err
		}
	}
	if err := f.vcs.IsAncestorOrEqual(snap.State.BaseSnapshot, head); err != nil {
		return "", err
	}
	count, err := f.vcs.CommitCount(snap.State.BaseSnapshot, head)
	if err != nil {
		return "", err
	}
	if count > 1 {
		message := strings.TrimSpace(squashMessage)
		if message == "" {
			return "", fmt.Errorf("candidate promotion requires a squash message for multiple commits")
		}
		if err := f.vcs.SquashToBase(snap.State.BaseSnapshot, message); err != nil {
			return "", err
		}
		wrote = true
		if head, err = f.vcs.Resolve(); err != nil {
			return "", err
		}
	}
	digest, err := f.vcs.TreeDigest(head)
	if err != nil {
		return "", err
	}
	if err := f.engine.RecordCandidatePromotion(head, digest, emptyFingerprint()); err != nil {
		// 接纳被撤销（屏障/活动期）且本次调用改写过历史：soft reset 回意图
		// 目标补偿——组织保树，index==目标树＝工作区干净，净零历史改写。
		// digest 不匹配与 schema 拒绝不补偿（内容错≠接纳撤销）。
		var rejected *protocol.RejectedError
		if wrote && errors.As(err, &rejected) && (rejected.Code == protocol.CodeChangeBarrierActive || rejected.Code == protocol.CodeEventNotCurrent) {
			if resetErr := f.vcs.SoftReset(intent); resetErr != nil {
				return "", errors.Join(err, fmt.Errorf("restoring the promotion head %s failed: %w", intent, resetErr))
			}
			return "", fmt.Errorf("%w; the organized history was restored to the promotion head", err)
		}
		return "", err
	}
	return head, nil
}

// RegisterQAWorktree 登记 QA 隔离工作区（镜像 legacy RegisterQAWorktree：工作
// 区原生身份必须解析为 run 的 BaseSnapshot，隔离区内需求产物哈希必须等于登记
// 修订——防 host 遗忘刷新注入）。物理建区归宿主；登记经引擎事务落账。
func (f *Facade) RegisterQAWorktree(worktree string) error {
	if f.vcs == nil {
		return fmt.Errorf("QA worktree registration requires a regular run with a git provider")
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
	resolved, err := vcs.New(worktree, f.vcsRunner).Resolve()
	if err != nil {
		return fmt.Errorf("cannot resolve QA isolation worktree: %w", err)
	}
	if !strings.EqualFold(resolved, snap.State.BaseSnapshot) {
		return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, snap.State.BaseSnapshot)
	}
	receipt := snap.State.IntakeConfirmationReceipt
	if receipt == nil {
		return fmt.Errorf("QA worktree registration requires the run's intake artifacts")
	}
	for _, artifact := range receipt.Artifacts {
		path := filepath.Join(worktree, filepath.FromSlash(artifact.Path))
		revision, err := requirementArtifactRevision(path)
		if err != nil {
			return fmt.Errorf("QA isolation worktree requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return fmt.Errorf("QA isolation worktree requirement artifact %s does not match the run revision %s", artifact.Path, artifact.Revision)
		}
	}
	return f.engine.RecordQAWorktree(worktree, emptyFingerprint())
}

// requirementArtifactRevision 计算需求工件的内容哈希（裸 hex，镜像 legacy
// RequirementRevision，与 intake 工件绑定校验同一修订语言；行尾先规范化
// CRLF→LF——Windows checkout 不改变工件身份）。
func requirementArtifactRevision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}
