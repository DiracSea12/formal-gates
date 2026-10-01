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

// terminalSnapshotGuard 是 regular COMPLETE 的终态快照边界（镜像 legacy Seal
// 的多 commit 拒绝与前后双 resolve：区间提交已由候选晋升入口组织，这里是最后
// 一道防线）。返回的 guard 在终态发布（摘要落盘）后调用，核对原生身份未在发
// 布段内移动。无 provider＝明确报错，不静默跳过校验。
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
	if err := f.vcs.IsAncestorOrEqual(run.BaseSnapshot, head); err != nil {
		return nil, err
	}
	if count, err := f.vcs.CommitCount(run.BaseSnapshot, head); err != nil {
		return nil, err
	} else if count > 1 {
		return nil, fmt.Errorf("organize the final candidate with engine candidate promotion before terminal completion")
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
// commit：OpenPromotion 先过接纳（活动/无屏障/无 abort/wave COMPLETE），被拒
// 时零 Git 副作用；squash 是唯一写点；组织后重解析并计算内容摘要，经
// RecordCandidatePromotion 消费意图原子提交（内容必须等于已验证最终候选）。
// 已在位且 HEAD==PromotedSnapshot 时幂等直返。工作区必须就绪（未提交内容拒
// 绝——与 legacy verifySnapshotReady 同语义；track/commit 是候选 freeze 的宿主
// 侧动作，不在此自动执行）。
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
	if err := f.engine.OpenPromotion(head, emptyFingerprint()); err != nil {
		return "", err
	}
	if err := f.vcs.VerifyReady(); err != nil {
		return "", err
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
		var rejected *protocol.RejectedError
		if errors.As(err, &rejected) && rejected.Code == protocol.CodeChangeBarrierActive {
			// execute 与 commit 之间屏障到达的窗口：内容已组织、意图保留，
			// 接纳待屏障解除后重入（重入经 OpenPromotion 覆盖 target 收敛）。
			return "", fmt.Errorf("%w; the organized content stays parked until the barrier resolves and promotion is re-entered", err)
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
