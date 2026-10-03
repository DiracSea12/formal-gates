package facade

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
)

// 本文件覆盖 owner 区间协调在同代 engine run 之间的并发与消失交错：并行启动
// 经共同协调锁串行闭合（不因竞争失败），先消失的 peer 通过双边持久标记留痕，
// 先于我启动且已终态的区间不误伤精确计量。

const intervalBaseLine = `{"type":"assistant","uuid":"base","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":3,"output_tokens":4}}}`
const intervalDuringLine = `{"type":"assistant","uuid":"during","message":{"usage":{"input_tokens":15,"cache_read_input_tokens":2,"output_tokens":8}}}`
const intervalLaterLine = `{"type":"assistant","uuid":"later","message":{"usage":{"input_tokens":16,"cache_read_input_tokens":1,"output_tokens":6}}}`

func startIntervalRun(t *testing.T, root, id, transcript string) *Facade {
	t.Helper()
	f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{RunID: id, Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return f
}

// TestConcurrentOwnerStartsSerializeAndReportTrueIntervals：多对并行启动同一
// root——共享 transcript 的 run 全部报 UNAVAILABLE（区间确相交），独立
// transcript 的 run 报精确差值；并行启动不因协调竞争失败。
func TestConcurrentOwnerStartsSerializeAndReportTrueIntervals(t *testing.T) {
	root := t.TempDir()
	type entry struct {
		id         string
		transcript string
		overlaps   bool
	}
	var entries []entry
	for pair := 1; pair <= 4; pair++ {
		shared := filepath.Join(root, fmt.Sprintf("shared-%d.jsonl", pair))
		writeTranscript(t, shared, []string{intervalBaseLine})
		entries = append(entries,
			entry{id: fmt.Sprintf("pair-%d-a", pair), transcript: shared, overlaps: true},
			entry{id: fmt.Sprintf("pair-%d-b", pair), transcript: shared, overlaps: true})
	}
	solo := filepath.Join(root, "solo.jsonl")
	writeTranscript(t, solo, []string{intervalBaseLine})
	entries = append(entries, entry{id: "solo", transcript: solo})

	facades := make([]*Facade, len(entries))
	errs := make([]error, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		go func(i int, e entry) {
			defer wg.Done()
			facades[i], _, errs[i] = Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: e.transcript, Provider: meteredProvider}, Request: StartRequest{RunID: e.id, Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
		}(i, e)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent start %s failed: %v", entries[i].id, err)
		}
	}
	for _, e := range entries {
		writeTranscript(t, e.transcript, []string{intervalBaseLine, intervalDuringLine})
	}
	for i, f := range facades {
		run, err := f.Drive()
		if err != nil {
			t.Fatalf("drive %s: %v", entries[i].id, err)
		}
		owner := run.Cost.Owner
		if owner == nil {
			t.Fatalf("run %s has no owner projection", entries[i].id)
		}
		if entries[i].overlaps {
			if owner.Source != "unavailable" || owner.TotalInputTokens != 0 {
				t.Fatalf("pair run %s owner = %+v, want unavailable zero", entries[i].id, owner)
			}
			continue
		}
		if owner.Source != "transcript" || owner.TotalInputTokens != 17 || owner.InputCacheHitTokens != 2 || owner.OutputTokens != 8 {
			t.Fatalf("solo owner = %+v, want precise 17 total / 2 hit / 8 output", owner)
		}
	}
}

// TestVanishedPeerBeforeTerminalStillMarksBothUnavailable：peer 在我启动观察
// 点之后启动、先于我终态消失（状态目录已清理）——双边标记在 peer 的启动段内
// 持久，我方终态不得报精确假数字。
func TestVanishedPeerBeforeTerminalStillMarksBothUnavailable(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "shared.jsonl")
	writeTranscript(t, transcript, []string{intervalBaseLine})
	first := startIntervalRun(t, root, "first", transcript)
	peer := startIntervalRun(t, root, "peer", transcript)
	writeTranscript(t, transcript, []string{intervalBaseLine, intervalDuringLine})
	for id, f := range map[string]*Facade{"peer": peer, "first": first} {
		run, err := f.Drive()
		if err != nil {
			t.Fatalf("drive %s: %v", id, err)
		}
		if run.Cost.Owner == nil || run.Cost.Owner.Source != "unavailable" || run.Cost.Owner.TotalInputTokens != 0 {
			t.Fatalf("run %s owner = %+v, want unavailable zero after peer vanish", id, run.Cost.Owner)
		}
	}
}

// TestTerminalPeerDoesNotOverlapLaterRun：精确性负控——对端先于我启动且已终态
// （同 transcript），双方都报各自的精确差值，不因留存终态摘要误判重叠。
func TestTerminalPeerDoesNotOverlapLaterRun(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "shared.jsonl")
	writeTranscript(t, transcript, []string{intervalBaseLine})
	earlier := startIntervalRun(t, root, "earlier", transcript)
	writeTranscript(t, transcript, []string{intervalBaseLine, intervalDuringLine})
	earlierRun, err := earlier.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if earlierRun.Cost.Owner == nil || earlierRun.Cost.Owner.Source != "transcript" || earlierRun.Cost.Owner.TotalInputTokens != 17 || earlierRun.Cost.Owner.OutputTokens != 8 {
		t.Fatalf("earlier owner = %+v, want precise 17/8", earlierRun.Cost.Owner)
	}
	later := startIntervalRun(t, root, "later", transcript)
	writeTranscript(t, transcript, []string{intervalBaseLine, intervalDuringLine, intervalLaterLine})
	laterRun, err := later.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if laterRun.Cost.Owner == nil || laterRun.Cost.Owner.Source != "transcript" || laterRun.Cost.Owner.TotalInputTokens != 17 || laterRun.Cost.Owner.InputCacheHitTokens != 1 || laterRun.Cost.Owner.OutputTokens != 6 {
		t.Fatalf("later owner = %+v, want precise 17/1/6 after terminal peer", laterRun.Cost.Owner)
	}
}

// TestTerminalCheckpointRecoveryDoesNotDoubleAttributeInterval：owner 终态落盘
// 与量取端点分离的可恢复边界——first 停在持久终态检查点（TERMINAL 已落盘、
// 摘要未发布＝区间仍开放）后，共享 transcript 的 peer 启动：peer 把 first 当开
// 放区间（UNAVAILABLE、并补写 first 的重叠标记），双方不再各记一笔精确差值；
// first 恢复量取同样 UNAVAILABLE。
func TestTerminalCheckpointRecoveryDoesNotDoubleAttributeInterval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "owner.jsonl")
	writeTranscript(t, path, []string{intervalBaseLine})
	first := startIntervalRun(t, root, "first", path)
	receipt := testReceipt()
	digest, err := IntakeDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: receipt, IntakeDigest: digest}, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	// 持久终态检查点：CompleteAll 落盘 TERMINAL 后、owner 量取前中断。
	if _, err = first.engine.CompleteAll(emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	peer := startIntervalRun(t, root, "peer", path)
	writeTranscript(t, path, []string{intervalBaseLine, intervalDuringLine})
	peerRun, err := peer.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if peerRun.Cost.Owner == nil || peerRun.Cost.Owner.Source != "unavailable" || peerRun.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("peer owner = %+v, want unavailable zero (first's terminal interval was still open)", peerRun.Cost.Owner)
	}
	reopened, err := Open(root, "first")
	if err != nil {
		t.Fatal(err)
	}
	firstRun, err := reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if firstRun.Cost.Owner == nil || firstRun.Cost.Owner.Source != "unavailable" || firstRun.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("first owner = %+v, want unavailable zero after recovery", firstRun.Cost.Owner)
	}
}
