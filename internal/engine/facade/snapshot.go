package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/vcs"
)

// terminalSnapshotGuard 是 regular COMPLETE 终态发布段的身份边界，单一规则：
// sealDelivered 已验证的交付身份（head==PromotedSnapshot）贯穿 seal 窗口与发布
// 窗口——发布前 resolve 必须仍等于该晋升身份（窗口内漂移即错、摘要不落盘），
// 返回的 guard 在终态发布（摘要落盘）后调用，核对发布段内身份未移动（镜像
// legacy Seal 发布窗口的前后双 resolve）。guard 不持第二套判据：交付内容与历
// 史形状的核验统一由 sealDelivered 单点持有。无 provider＝明确报错，不静默跳
// 过校验。
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
// 就绪检查）。窗口＝PrepareCandidate 的准入→执行→观察→提交；需求变化接纳在
// 同一把锁上等待至窗口关闭（R4e owner 区间串行先例的同款原语）。
func (f *Facade) promotionLockPath() string {
	return filepath.Join(f.stateDir, "promotion.lock")
}

// PrepareCandidate 组织终态候选（镜像 legacy PrepareCandidate：区间单提交＝确
// 认当前 HEAD；多提交需 squash 消息，squash 后重解析绑定；晋升后区间再度累
// 积提交时可重复组织）。晋升走完整副作用协议 intent→execute→observe→
// commit，整个窗口（准入→执行→观察→提交）持 run 级晋升锁与需求变化接纳互
// 斥：屏障要么先于窗口落账（准入处拒绝、零 Git 副作用）、要么等待至窗口关闭
// 后作用于已提交事实——接纳之后不再有窗口内的新 Git 写。恢复只按持久事实
// （引擎状态＋观察到的 Git 状态）对账，零内存恢复事实跨调用：内容前置门核对
// 暂存树等于已验证最终候选 digest，squash 与续行 commit 只组织已验证内容；
// 意图存活期间重入＝续行而非重开，squash 的 reset 已完成、commit 未执行的中
// 断由「未提交内容恰为纯暂存形态」识别并以 Commit 完成组织，区间已是单提交
// 且树＝候选时只提交结果（不重复 squash）；工作区含未暂存/未跟踪的修改不是
// 本操作的中断中间态——拒绝并指明先处理，绝不把用户脏区提交成晋升。已在位
// 且 HEAD==PromotedSnapshot 时幂等直返。track/commit 是候选 freeze 的宿主侧
// 动作，不在此自动执行。
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
	unlock, err := coordination.AcquirePathWaiting(f.promotionLockPath(), promotionLockDescription)
	if err != nil {
		return "", err
	}
	defer unlock()
	// 窗口可能等待过：以窗口内事实重新对账（引擎状态与 Git 状态都可能已变）。
	if snap, err = f.engine.Load(); err != nil {
		return "", err
	}
	if head, err = f.vcs.Resolve(); err != nil {
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
	if err := f.vcs.VerifyReady(); err != nil {
		if head == intent {
			return "", err
		}
		// head 已离开意图目标且工作区不净：本意图组织的精确中断中间态＝
		// 「未提交内容恰为纯暂存形态」（squash 的 reset --soft 已完成、
		// commit 未执行——保树保 index，暂存树＝候选已由前置门核对），以
		// Commit 完成组织并绑定原意图。其余形状（未暂存/未跟踪的用户修
		// 改）不是本操作的中间态：拒绝并指明先处理工作区。
		stagedOnly, classErr := f.vcs.StagedOnly()
		if classErr != nil {
			return "", classErr
		}
		if !stagedOnly {
			return "", fmt.Errorf("candidate promotion cannot resume over workspace changes it did not stage (unstaged or untracked); commit, stage or clean them and re-enter")
		}
		message := strings.TrimSpace(squashMessage)
		if message == "" {
			return "", fmt.Errorf("candidate promotion requires a squash message for multiple commits")
		}
		if err := f.vcs.Commit(message); err != nil {
			return "", err
		}
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
		if head, err = f.vcs.Resolve(); err != nil {
			return "", err
		}
	}
	digest, err := f.vcs.TreeDigest(head)
	if err != nil {
		return "", err
	}
	if err := f.engine.RecordCandidatePromotion(head, digest, emptyFingerprint()); err != nil {
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
