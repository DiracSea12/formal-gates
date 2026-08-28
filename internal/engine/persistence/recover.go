package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RecoveryOutcome 是 Recover 对账的四种结论。
type RecoveryOutcome string

const (
	// RecoveryClean：无 intent，不存在未完结提交（顺带清扫崩溃遗留的
	// 孤儿 temp）。
	RecoveryClean RecoveryOutcome = "clean"
	// RecoveryCommitted：最终文件已处于 intent 声明的终态（崩溃发生在
	// execute 之后、清除 intent 之前），对账只需清除协议残留。
	RecoveryCommitted RecoveryOutcome = "committed"
	// RecoveryRecovered：intent 与暂存文件完整、基线 revision 吻合
	// （崩溃发生在 execute 之前），对账补做原子提交。
	RecoveryRecovered RecoveryOutcome = "recovered"
	// RecoveryResidual：intent 无法完成（暂存丢失或损坏、基线已被超越、
	// intent 不可解析等），判定残留：清除 intent 与 temp，状态停留在
	// 最后一个完整提交，未完结的那次写入作废。
	RecoveryResidual RecoveryOutcome = "residual"
)

// RecoveryReport 是一次对账的结果。
type RecoveryReport struct {
	Outcome  RecoveryOutcome `json:"outcome"`
	Revision uint64          `json:"revision"`
}

// intentRecord 是 persist intent 阶段落盘的对账记录：它声明一次未完结
// 提交的完整终态（目标文件、暂存文件、基线/目标 revision 与内容摘要）。
type intentRecord struct {
	Target           string `json:"target"`
	Temp             string `json:"temp"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	NextRevision     uint64 `json:"nextRevision"`
	ContentDigest    string `json:"contentDigest"`
}

// Recover 对状态目录做崩溃对账（master-requirements §2：崩溃后重启可
// 从 intent 对账并恢复或判定残留）。持锁执行，判定顺序：
//
//   - 当前状态文件无法通过信封/摘要校验 → 返回错误且不动任何协议
//     文件：篡改或外来状态不属于崩溃对账可自愈的范围；
//   - 无 intent → 清扫孤儿 temp，clean；
//   - 最终文件已达 intent 终态（revision + 内容摘要吻合）→ 清除协议
//     残留，committed；
//   - 暂存文件通过完整结构校验（信封、摘要、revision、内容与 intent
//     声明一致）且基线 revision 吻合 → 补做 execute（原子 rename +
//     fsync 目录）并复核，recovered；
//   - 其余 → 判定残留，清除 intent 与 temp，residual。
func (s *Store) Recover() (RecoveryReport, error) {
	processLock := transactionLock(s.dir)
	processLock.Lock()
	defer processLock.Unlock()
	unlock, err := s.acquireLock()
	if err != nil {
		return RecoveryReport{}, err
	}
	defer unlock()

	current, err := s.readDocument()
	if err != nil {
		return RecoveryReport{}, err
	}
	var currentRevision uint64
	if current != nil {
		currentRevision = current.Revision
	}

	intentData, err := os.ReadFile(s.intentPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.sweepOrphanTemps()
			return RecoveryReport{Outcome: RecoveryClean, Revision: currentRevision}, nil
		}
		return RecoveryReport{}, err
	}
	var intent intentRecord
	intentErr := json.Unmarshal(intentData, &intent)

	// 崩溃发生在 execute 之后：最终文件已经处于 intent 声明的终态
	// （readDocument 已校验信封与摘要，这里核对 revision 与内容摘要）。
	if intentErr == nil && intent.Target == stateFileName && current != nil &&
		current.Revision == intent.NextRevision && current.ContentDigest == intent.ContentDigest {
		_ = s.removeIntent()
		s.sweepOrphanTemps()
		return RecoveryReport{Outcome: RecoveryCommitted, Revision: current.Revision}, nil
	}

	// 崩溃发生在 execute 之前且基线吻合：校验暂存文件完整结构后补做
	// 原子提交。先校验再 rename，绝不把未通过校验的暂存文件盖到完好
	// 状态上。
	if intentErr == nil && intent.Target == stateFileName && currentRevision == intent.ExpectedRevision {
		staged, stagedErr := s.readStagedDocument(intent.Temp)
		if stagedErr == nil && staged != nil &&
			staged.Revision == intent.NextRevision && staged.ContentDigest == intent.ContentDigest {
			if execErr := s.executeCommit(intent.Temp); execErr != nil {
				return RecoveryReport{}, fmt.Errorf("persistence: recover: redo commit: %w", execErr)
			}
			reread, readErr := s.readDocument()
			if readErr != nil {
				return RecoveryReport{}, fmt.Errorf("persistence: recover: verify after redo: %w", readErr)
			}
			if reread == nil || reread.Revision != intent.NextRevision || reread.ContentDigest != intent.ContentDigest {
				return RecoveryReport{}, fmt.Errorf(
					"persistence: recover: verify after redo: %s is not at intent revision %d",
					s.statePath(), intent.NextRevision)
			}
			_ = s.removeIntent()
			s.sweepOrphanTemps()
			return RecoveryReport{Outcome: RecoveryRecovered, Revision: reread.Revision}, nil
		}
	}

	// 判定残留：intent 无法完成，清除协议残留（intent 引用的 temp 也是
	// 垃圾——intent 已删除，无人再认领它）；状态停留在最后完整提交。
	_ = s.removeIntent()
	s.sweepOrphanTemps()
	return RecoveryReport{Outcome: RecoveryResidual, Revision: currentRevision}, nil
}

// readStagedDocument 读取并完整校验暂存文件：能通过信封、摘要校验且
// 内容可解码，才允许作为补做提交的载体。
func (s *Store) readStagedDocument(name string) (*document, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("persistence: decode staged %s: %w", name, err)
	}
	if err := validateEnvelope(doc.envelope(), s.cfg); err != nil {
		return nil, err
	}
	compact, err := compactContent(doc.Content)
	if err != nil {
		return nil, err
	}
	if observed := contentDigestOf(compact); observed != doc.ContentDigest {
		return nil, &IntegrityMismatchError{Path: filepath.Join(s.dir, name), Expected: doc.ContentDigest, Observed: observed}
	}
	return &doc, nil
}

// sweepOrphanTemps 清扫目录内全部崩溃遗留暂存文件（写 temp 成功但尚未
// 写 intent 即崩溃的产物，以及 intent 已被判残留后无人认领的 temp）。
// commit/recovered 路径里 intent 引用的 temp 已被 rename 消费，剩余匹配
// 命名模式的一律是孤儿。
func (s *Store) sweepOrphanTemps() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(name, tempPrefix) && strings.HasSuffix(name, tempSuffix) {
			_ = os.Remove(filepath.Join(s.dir, name))
		}
	}
}
