package persistence

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"testing"
)

// saveOne 提交一次内容并返回结果快照。
func saveOne(t *testing.T, store *Store, content map[string]any) *Snapshot {
	t.Helper()
	snap, err := store.Load()
	expected := uint64(0)
	if snap != nil {
		expected = snap.Revision
	}
	if _, err := store.Save(Transaction{
		ExpectedRevision:    expected,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  staticFingerprint("fp"),
		Content:             content,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return loaded
}

// TestRecoverClean：无 intent 时无事可做；崩溃遗留的孤儿 temp 被清扫。
func TestRecoverClean(t *testing.T) {
	store := newTestStore(t)
	snap := saveOne(t, store, map[string]any{"v": 1})

	// 孤儿 temp：写 temp 成功但尚未写 intent 即崩溃的产物。
	orphan := store.dir + string(os.PathSeparator) + tempPrefix + "12345" + tempSuffix
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatalf("write orphan temp: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryClean {
		t.Fatalf("outcome = %q, want %q", report.Outcome, RecoveryClean)
	}
	if report.Revision != snap.Revision {
		t.Fatalf("revision = %d, want %d", report.Revision, snap.Revision)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("orphan temp not swept: %v", err)
	}
	if _, err := os.Stat(store.statePath()); err != nil {
		t.Fatalf("state lost after clean recover: %v", err)
	}
}

// TestRecoverCommittedAfterExecute：崩溃发生在 execute 之后、清除 intent
// 之前（最终文件已达 intent 终态）——对账确认已提交并清除残留。
func TestRecoverCommittedAfterExecute(t *testing.T) {
	store := newTestStore(t)
	snap := saveOne(t, store, map[string]any{"v": 1})

	// 手工重建“execute 已完成但 intent 未清除”的崩溃现场。
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempPrefix + "consumed" + tempSuffix,
		ExpectedRevision: snap.Revision - 1, NextRevision: snap.Revision,
		ContentDigest: snap.ContentDigest,
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryCommitted {
		t.Fatalf("outcome = %q, want %q", report.Outcome, RecoveryCommitted)
	}
	if report.Revision != snap.Revision {
		t.Fatalf("revision = %d, want %d", report.Revision, snap.Revision)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("intent not cleared: %v", err)
	}
	again, err := store.Load()
	if err != nil {
		t.Fatalf("load after recover: %v", err)
	}
	if again.Revision != snap.Revision || again.ContentDigest != snap.ContentDigest {
		t.Fatalf("state drifted during committed recovery")
	}
}

// TestRecoverRedoesInterruptedCommit：崩溃发生在 persist intent 之后、
// execute 之前（暂存完整、基线吻合）——对账补做原子提交。
func TestRecoverRedoesInterruptedCommit(t *testing.T) {
	store := newTestStore(t)
	content := map[string]any{"phase": "DEV", "v": 7}
	contentBytes, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	doc := newDocument(store.cfg, 1, contentDigestOf(contentBytes), contentBytes)
	tempName, err := store.writeTempDocument(doc)
	if err != nil {
		t.Fatalf("write staged temp: %v", err)
	}
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempName,
		ExpectedRevision: 0, NextRevision: 1, ContentDigest: doc.ContentDigest,
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	// 最终文件尚不存在：第一次写入在 execute 前崩溃。
	if _, err := os.Stat(store.statePath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final state unexpectedly exists before recovery: %v", err)
	}

	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryRecovered {
		t.Fatalf("outcome = %q, want %q", report.Outcome, RecoveryRecovered)
	}
	if report.Revision != 1 {
		t.Fatalf("revision = %d, want 1", report.Revision)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load after redo: %v", err)
	}
	var loaded map[string]any
	if err := json.Unmarshal(snap.Content, &loaded); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if loaded["v"] != float64(7) {
		t.Fatalf("recovered content mismatch: %v", loaded)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("intent not cleared after redo: %v", err)
	}
	// 补做提交消费的是暂存文件本身，目录回到只剩 state.json。
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != stateFileName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("state dir after redo = %v, want [%s]", names, stateFileName)
	}
}

// TestRecoverRedoesOnTopOfExistingState：已有 revision 1 的状态下，崩溃
// 在第二次提交的 execute 前——补做后 revision 推进到 2。
func TestRecoverRedoesOnTopOfExistingState(t *testing.T) {
	store := newTestStore(t)
	saveOne(t, store, map[string]any{"v": 1})

	contentBytes, err := json.Marshal(map[string]any{"v": 2})
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	doc := newDocument(store.cfg, 2, contentDigestOf(contentBytes), contentBytes)
	tempName, err := store.writeTempDocument(doc)
	if err != nil {
		t.Fatalf("write staged temp: %v", err)
	}
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempName,
		ExpectedRevision: 1, NextRevision: 2, ContentDigest: doc.ContentDigest,
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryRecovered || report.Revision != 2 {
		t.Fatalf("outcome=%s revision=%d, want recovered/2", report.Outcome, report.Revision)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var loaded map[string]any
	if err := json.Unmarshal(snap.Content, &loaded); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if loaded["v"] != float64(2) {
		t.Fatalf("recovered content mismatch: %v", loaded)
	}
}

// TestRecoverResidualWhenTempLost：intent 在、暂存丢失——判定残留，
// 未完结写入作废，状态停留在最后完整提交。
func TestRecoverResidualWhenTempLost(t *testing.T) {
	store := newTestStore(t)
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempPrefix + "gone" + tempSuffix,
		ExpectedRevision: 0, NextRevision: 1, ContentDigest: "sha256:whatever",
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryResidual || report.Revision != 0 {
		t.Fatalf("outcome=%s revision=%d, want residual/0", report.Outcome, report.Revision)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("residual intent not cleared: %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("residual recovery must not fabricate state: %v", err)
	}
}

// TestRecoverResidualWhenTempCorrupt：暂存被写坏（内容与 intent 摘要
// 不符）——不把坏文件盖到状态上，判残留。
func TestRecoverResidualWhenTempCorrupt(t *testing.T) {
	store := newTestStore(t)
	first := saveOne(t, store, map[string]any{"v": 1})

	corruptName := tempPrefix + "corrupt" + tempSuffix
	if err := os.WriteFile(store.dir+string(os.PathSeparator)+corruptName, []byte("{\"writer\":\"engine\"}"), 0o600); err != nil {
		t.Fatalf("write corrupt temp: %v", err)
	}
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: corruptName,
		ExpectedRevision: 1, NextRevision: 2, ContentDigest: "sha256:not-this",
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryResidual {
		t.Fatalf("outcome = %q, want %q", report.Outcome, RecoveryResidual)
	}
	// 判残留后 intent 引用的坏 temp 也必须被清走：intent 已删，无人认领。
	if _, err := os.Stat(store.dir + string(os.PathSeparator) + corruptName); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corrupt temp not swept after residual recovery: %v", err)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load after residual: %v", err)
	}
	if snap.Revision != first.Revision || snap.ContentDigest != first.ContentDigest {
		t.Fatalf("state changed during residual recovery")
	}
}

// TestRecoverResidualWhenBaselineAdvanced：intent 声明的基线已被超越
// （后续写入已完成更高 revision）——旧 intent 判残留，现状不动。
func TestRecoverResidualWhenBaselineAdvanced(t *testing.T) {
	store := newTestStore(t)
	saveOne(t, store, map[string]any{"v": 1})
	second := saveOne(t, store, map[string]any{"v": 2})

	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempPrefix + "stale" + tempSuffix,
		ExpectedRevision: 0, NextRevision: 1, ContentDigest: "sha256:stale",
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryResidual || report.Revision != second.Revision {
		t.Fatalf("outcome=%s revision=%d, want residual/%d", report.Outcome, report.Revision, second.Revision)
	}
}

// TestRecoverResidualWhenIntentUnparsable：intent 本身损坏（写 intent 时
// 崩溃）——判残留并清除，状态不受影响。
func TestRecoverResidualWhenIntentUnparsable(t *testing.T) {
	store := newTestStore(t)
	snap := saveOne(t, store, map[string]any{"v": 1})
	if err := os.WriteFile(store.intentPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write bad intent: %v", err)
	}
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryResidual || report.Revision != snap.Revision {
		t.Fatalf("outcome=%s revision=%d, want residual/%d", report.Outcome, report.Revision, snap.Revision)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("bad intent not cleared: %v", err)
	}
}

// TestRecoverSurfacesTamperedState：当前状态文件被篡改时不做对账、不动
// 协议文件，完整性错误如实上抛（篡改不是崩溃对账可自愈的范围）。
func TestRecoverSurfacesTamperedState(t *testing.T) {
	store := newTestStore(t)
	saveOne(t, store, map[string]any{"v": 1})

	raw, err := os.ReadFile(store.statePath())
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	doc["content"] = map[string]any{"forged": true}
	patched, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal patched: %v", err)
	}
	if err := os.WriteFile(store.statePath(), append(patched, '\n'), 0o600); err != nil {
		t.Fatalf("write patched: %v", err)
	}
	if err := store.writeIntent(intentRecord{
		Target: stateFileName, Temp: tempPrefix + "x" + tempSuffix,
		ExpectedRevision: 0, NextRevision: 2, ContentDigest: "sha256:x",
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}

	_, recoverErr := store.Recover()
	var integrity *IntegrityMismatchError
	if !errors.As(recoverErr, &integrity) {
		t.Fatalf("recover error is %v, want *IntegrityMismatchError", recoverErr)
	}
	// 协议文件保持原样：对账没有清理或改写任何东西。
	if _, err := os.Stat(store.intentPath()); err != nil {
		t.Fatalf("intent must stay in place on validation failure: %v", err)
	}
}

// TestRecoverAfterSaveIsClean：正常保存路径结束后 Recover 报 clean——
// Save 的 commit 段已经把 intent 清掉。
func TestRecoverAfterSaveIsClean(t *testing.T) {
	store := newTestStore(t)
	snap := saveOne(t, store, map[string]any{"v": 1})
	report, err := store.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.Outcome != RecoveryClean || report.Revision != snap.Revision {
		t.Fatalf("outcome=%s revision=%d, want clean/%d", report.Outcome, report.Revision, snap.Revision)
	}
}
