package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
)

// newTestStore 在隔离临时目录构造 Store（不触碰真实用户路径）。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir(), Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

// staticFingerprint 返回恒定指纹的收集函数。
func staticFingerprint(fp string) func() (string, error) {
	return func() (string, error) { return fp, nil }
}

// flipFingerprint 第一次调用返回 expected、之后返回 changed，模拟写后
// 外部事实变化。
func flipFingerprint(expected, changed string) func() (string, error) {
	first := true
	return func() (string, error) {
		if first {
			first = false
			return expected, nil
		}
		return changed, nil
	}
}

// stateBytes 读取当前 state.json 原始字节（断言“绝不写状态”用）。
func stateBytes(t *testing.T, store *Store) []byte {
	t.Helper()
	data, err := os.ReadFile(store.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read state: %v", err)
	}
	return data
}

// assertNothingWritten 断言被拒路径既没写状态文件也没留 intent。
func assertNothingWritten(t *testing.T, store *Store, before []byte) {
	t.Helper()
	if after := stateBytes(t, store); !reflect.DeepEqual(before, after) {
		t.Fatalf("state file changed on rejected path:\nbefore %q\nafter  %q", before, after)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("intent record exists after rejection: %v", err)
	}
}

// writeRawDocument 直接手写一份以当前身份为基线的 state.json，mutate
// 可改写或删除字段，用于构造缺失/篡改/外来文件。
func writeRawDocument(t *testing.T, store *Store, mutate func(map[string]any)) {
	t.Helper()
	doc := map[string]any{
		"writer":                    Writer,
		"stateSchemaVersion":        expectedEnvelope(store.cfg).StateSchemaVersion,
		"workflowDefinitionVersion": definition.WorkflowDefinitionVersion,
		"definitionDigest":          definition.WorkflowDefinitionDigest,
		"packageDigest":             store.cfg.PackageDigest,
		"revision":                  1,
		"contentDigest":             "sha256:placeholder",
		"content":                   map[string]any{"phase": "INTAKE"},
	}
	if mutate != nil {
		mutate(doc)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal raw document: %v", err)
	}
	if err := os.WriteFile(store.statePath(), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write raw document: %v", err)
	}
}

// TestSaveLoadRoundTrip：正向闭环——保存后可读取，信封字段精确、revision
// 单调推进、协议残留（intent/temp）清除干净。
func TestSaveLoadRoundTrip(t *testing.T) {
	store := newTestStore(t)
	content := map[string]any{"phase": "INTAKE", "tasks": []any{}}
	res, err := store.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp-1",
		CollectFingerprint:  staticFingerprint("fp-1"),
		Content:             content,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.Revision != 1 {
		t.Fatalf("first save revision = %d, want 1", res.Revision)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snap.Revision != 1 {
		t.Fatalf("snapshot revision = %d, want 1", snap.Revision)
	}
	var loaded map[string]any
	if err := json.Unmarshal(snap.Content, &loaded); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if !reflect.DeepEqual(loaded, content) {
		t.Fatalf("content round trip mismatch:\n got  %#v\n want %#v", loaded, content)
	}

	// 顺序写入：revision 每次单调 +1，内容与摘要随之更新。
	res, err = store.Save(Transaction{
		ExpectedRevision:    snap.Revision,
		ExpectedFingerprint: "fp-2",
		CollectFingerprint:  staticFingerprint("fp-2"),
		Content:             map[string]any{"phase": "DEV", "done": []string{"intake"}},
	})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if res.Revision != 2 {
		t.Fatalf("second save revision = %d, want 2", res.Revision)
	}
	snap2, err := store.Load()
	if err != nil {
		t.Fatalf("load after second save: %v", err)
	}
	if snap2.Revision != 2 || snap2.ContentDigest == snap.ContentDigest {
		t.Fatalf("revision/digest did not advance: rev=%d digest-equal=%v", snap2.Revision, snap2.ContentDigest == snap.ContentDigest)
	}

	// 磁盘形态：五个信封字段 + revision + contentDigest 精确在档；协议
	// 残留清除干净（目录里只剩 state.json）。
	raw, err := os.ReadFile(store.statePath())
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	env := expectedEnvelope(store.cfg)
	for field, want := range map[string]any{
		"writer":                    env.Writer,
		"stateSchemaVersion":        env.StateSchemaVersion,
		"workflowDefinitionVersion": env.WorkflowDefinitionVersion,
		"definitionDigest":          env.DefinitionDigest,
		"packageDigest":             env.PackageDigest,
		"revision":                  float64(2),
		"contentDigest":             snap2.ContentDigest,
	} {
		if got := onDisk[field]; got != want {
			t.Fatalf("state file %s = %v, want %v", field, got, want)
		}
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != stateFileName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("state dir should only contain %s after commit, got %v", stateFileName, names)
	}
}

// TestLoadMissingState：全新目录读取返回 fs.ErrNotExist。
func TestLoadMissingState(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("load on empty dir err = %v, want fs.ErrNotExist", err)
	}
}

// TestNewStoreRequiresPackageDigest：没有安装身份就没有合法信封，构造
// 期直接拒绝。
func TestNewStoreRequiresPackageDigest(t *testing.T) {
	if _, err := NewStore(t.TempDir(), Config{}); err == nil {
		t.Fatal("empty PackageDigest accepted")
	}
	if _, err := NewStore(t.TempDir(), Config{PackageDigest: " "}); err == nil {
		t.Fatal("blank PackageDigest accepted")
	}
}

// TestSaveRejectsBadTransactions：事务参数缺失（内容、收集函数、期望
// 指纹）在加锁前拒绝，目录里什么都不产生。
func TestSaveRejectsBadTransactions(t *testing.T) {
	store := newTestStore(t)
	cases := []struct {
		name string
		tx   Transaction
	}{
		{"nil content", Transaction{ExpectedFingerprint: "fp", CollectFingerprint: staticFingerprint("fp")}},
		{"nil collector", Transaction{ExpectedFingerprint: "fp", Content: map[string]any{}}},
		{"empty fingerprint", Transaction{CollectFingerprint: staticFingerprint(""), Content: map[string]any{}}},
	}
	for _, tc := range cases {
		if _, err := store.Save(tc.tx); err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
		if entries, err := os.ReadDir(store.dir); err == nil && len(entries) != 0 {
			t.Fatalf("%s left files in state dir", tc.name)
		}
	}
}

// TestSaveNeverWritesOnUnsupportedEnvelope：磁盘信封缺失字段或值不精确
// 时，Load 与 Save 都以 UNSUPPORTED_RUN_VERSION 拒绝，state.json 原字节
// 保持不变（master-requirements §1：绝不写状态）。
func TestSaveNeverWritesOnUnsupportedEnvelope(t *testing.T) {
	missing := []string{
		"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest", "packageDigest",
	}
	wrong := []struct {
		name  string
		field string
		value any
	}{
		{"writer legacy", "writer", "legacy"},
		{"schema 2", "stateSchemaVersion", "2"},
		{"definition version 1", "workflowDefinitionVersion", "1"},
		{"definition digest other", "definitionDigest", "sha256:0123"},
		{"package digest stale", "packageDigest", "sha256:stale"},
	}
	for _, field := range missing {
		store := newTestStore(t)
		writeRawDocument(t, store, func(doc map[string]any) { delete(doc, field) })
		before := stateBytes(t, store)
		verifyUnsupportedAndUnwritten(t, store, before, field)
	}
	for _, tc := range wrong {
		store := newTestStore(t)
		writeRawDocument(t, store, func(doc map[string]any) { doc[tc.field] = tc.value })
		before := stateBytes(t, store)
		verifyUnsupportedAndUnwritten(t, store, before, tc.field)
	}
}

func verifyUnsupportedAndUnwritten(t *testing.T, store *Store, before []byte, field string) {
	t.Helper()
	_, loadErr := store.Load()
	if loadErr == nil {
		t.Fatalf("load accepted invalid %s", field)
	}
	var unsupported *UnsupportedRunVersionError
	if !errors.As(loadErr, &unsupported) {
		t.Fatalf("load error for %s is %T, want *UnsupportedRunVersionError", field, loadErr)
	}
	_, saveErr := store.Save(Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  staticFingerprint("fp"),
		Content:             map[string]any{"phase": "DEV"},
	})
	if !errors.As(saveErr, &unsupported) {
		t.Fatalf("save error for %s is %v, want *UnsupportedRunVersionError", field, saveErr)
	}
	assertNothingWritten(t, store, before)
}

// TestLoadRejectsTamperedContent：内容被改动而 contentDigest 未更新时，
// 读取与写入都以完整性错误拒绝，原字节不变（篡改可检测）。
func TestLoadRejectsTamperedContent(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Save(Transaction{
		ExpectedFingerprint: "fp", CollectFingerprint: staticFingerprint("fp"),
		Content: map[string]any{"phase": "INTAKE"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 只改内容字段，保留信封与摘要——模拟手工篡改。
	raw, err := os.ReadFile(store.statePath())
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	doc["content"] = map[string]any{"phase": "SEALED", "forged": true}
	patched, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal patched: %v", err)
	}
	if err := os.WriteFile(store.statePath(), append(patched, '\n'), 0o600); err != nil {
		t.Fatalf("write patched: %v", err)
	}
	before := stateBytes(t, store)

	_, loadErr := store.Load()
	var integrity *IntegrityMismatchError
	if !errors.As(loadErr, &integrity) {
		t.Fatalf("load error is %v, want *IntegrityMismatchError", loadErr)
	}
	_, saveErr := store.Save(Transaction{
		ExpectedRevision:    1,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  staticFingerprint("fp"),
		Content:             map[string]any{"phase": "DEV"},
	})
	if !errors.As(saveErr, &integrity) {
		t.Fatalf("save on tampered state error is %v, want *IntegrityMismatchError", saveErr)
	}
	assertNothingWritten(t, store, before)
}

// TestRevisionCASConflict：两个事务携带同一期望 revision，后到者在锁内
// 重读发现 revision 已推进，冲突拒绝且不覆盖第一次提交。
func TestRevisionCASConflict(t *testing.T) {
	store := newTestStore(t)
	first, err := store.Save(Transaction{
		ExpectedRevision: 0, ExpectedFingerprint: "fp", CollectFingerprint: staticFingerprint("fp"),
		Content: map[string]any{"v": 1},
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	stale, err := store.Save(Transaction{
		ExpectedRevision: 0, ExpectedFingerprint: "fp", CollectFingerprint: staticFingerprint("fp"),
		Content: map[string]any{"v": 2},
	})
	if err == nil {
		t.Fatalf("stale save accepted with revision %d", stale.Revision)
	}
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale save error is %v, want *RevisionConflictError", err)
	}
	if conflict.Expected != 0 || conflict.Observed != first.Revision {
		t.Fatalf("conflict pair = (%d, %d), want (0, %d)", conflict.Expected, conflict.Observed, first.Revision)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var content map[string]any
	if err := json.Unmarshal(snap.Content, &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if content["v"] != float64(1) {
		t.Fatalf("CAS conflict overwrote committed content: %v", content)
	}
	assertNothingWritten(t, store, stateBytes(t, store))
}

// TestFingerprintBeforeWriteRejects：写前重验发现外部事实已变，直接拒绝，
// 目录内不落任何协议字节。
func TestFingerprintBeforeWriteRejects(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp-observed",
		CollectFingerprint:  staticFingerprint("fp-changed"),
		Content:             map[string]any{"v": 1},
	})
	var changed *FingerprintChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error is %v, want *FingerprintChangedError", err)
	}
	if changed.Committed {
		t.Fatal("before-write rejection must not be committed")
	}
	if changed.Phase != FingerprintPhaseBefore {
		t.Fatalf("phase = %q, want %q", changed.Phase, FingerprintPhaseBefore)
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("rejected path left files: %v", names)
	}
}

// TestFingerprintAfterWriteCommitsAndReports：写后重验发现外部事实已变，
// 提交仍原子完整（revision 推进、内容落盘、intent 清除），以
// Committed=true 报告对账路径。
func TestFingerprintAfterWriteCommitsAndReports(t *testing.T) {
	store := newTestStore(t)
	res, err := store.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp-observed",
		CollectFingerprint:  flipFingerprint("fp-observed", "fp-drifted"),
		Content:             map[string]any{"v": 1},
	})
	var changed *FingerprintChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error is %v, want *FingerprintChangedError", err)
	}
	if !changed.Committed {
		t.Fatal("after-write change must report committed")
	}
	if changed.Phase != FingerprintPhaseAfter {
		t.Fatalf("phase = %q, want %q", changed.Phase, FingerprintPhaseAfter)
	}
	if res.Revision != 1 {
		t.Fatalf("committed revision = %d, want 1", res.Revision)
	}
	// 对账路径的实态：状态在档、可读、内容正确、无协议残留。
	snap, err := store.Load()
	if err != nil {
		t.Fatalf("load after committed fingerprint change: %v", err)
	}
	if snap.Revision != 1 {
		t.Fatalf("snapshot revision = %d, want 1", snap.Revision)
	}
	if _, err := os.Stat(store.intentPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("intent remains after committed fingerprint change: %v", err)
	}
}

// TestFingerprintCollectErrorAbortsBeforeWrite：写前收集失败按可重试错误
// 上抛，不写状态。
func TestFingerprintCollectErrorAbortsBeforeWrite(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  func() (string, error) { return "", fmt.Errorf("collector down") },
		Content:             map[string]any{"v": 1},
	})
	var changed *FingerprintChangedError
	if err == nil || errors.As(err, &changed) {
		t.Fatalf("collect error mishandled: %v", err)
	}
	entries, _ := os.ReadDir(store.dir)
	if len(entries) != 0 {
		t.Fatalf("aborted path left files in state dir")
	}
}

// TestSaveAtomicReplaceLeavesNoPartialState：最终文件要么是旧 revision、
// 要么是新 revision 的完整文档——保存后重读必须自洽（信封+摘要+revision
// 一次通过），不存在半新半旧的中间形态。
func TestSaveAtomicReplaceLeavesNoPartialState(t *testing.T) {
	store := newTestStore(t)
	for revision := uint64(0); revision < 3; revision++ {
		res, err := store.Save(Transaction{
			ExpectedRevision:    revision,
			ExpectedFingerprint: "fp",
			CollectFingerprint:  staticFingerprint("fp"),
			Content:             map[string]any{"round": revision},
		})
		if err != nil {
			t.Fatalf("save round %d: %v", revision, err)
		}
		if res.Revision != revision+1 {
			t.Fatalf("round %d revision = %d", revision, res.Revision)
		}
		if _, err := store.Load(); err != nil {
			t.Fatalf("load after round %d: %v", revision, err)
		}
	}
	// 目录里只有最终文件：没有遗留 temp，也没有 intent。
	matches, err := filepath.Glob(filepath.Join(store.dir, tempPrefix+"*"+tempSuffix))
	if err != nil {
		t.Fatalf("glob temps: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}

// TestSaveFaultBoundariesRecoverDeterministically：每个持久边界注入一次
// 进程丢失，重启后的 Recover 必须把状态归入可证明的 clean、recovered
// 或 committed 结论，不能留下不可读的半写状态。
func TestSaveFaultBoundariesRecoverDeterministically(t *testing.T) {
	cases := []struct {
		point    FaultPoint
		outcome  RecoveryOutcome
		revision uint64
	}{
		{FaultIntentBefore, RecoveryClean, 0},
		{FaultTempSyncBefore, RecoveryClean, 0},
		{FaultTempSyncAfter, RecoveryClean, 0},
		{FaultIntentAfter, RecoveryRecovered, 1},
		{FaultReplaceBefore, RecoveryRecovered, 1},
		{FaultExecuteBefore, RecoveryRecovered, 1},
		{FaultReplaceAfter, RecoveryCommitted, 1},
		{FaultExecuteAfter, RecoveryCommitted, 1},
		{FaultObserveBefore, RecoveryCommitted, 1},
		{FaultReconcileBefore, RecoveryCommitted, 1},
		{FaultCommitResponseLost, RecoveryClean, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.point), func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore(dir, Config{
				PackageDigest: testPackageDigest,
				FaultInjector: func(point FaultPoint) error {
					if point == tc.point {
						return &InjectedCrashError{Point: point}
					}
					return nil
				},
			})
			if err != nil {
				t.Fatalf("new faulting store: %v", err)
			}
			_, saveErr := store.Save(Transaction{
				ExpectedRevision:    0,
				ExpectedFingerprint: "fp",
				CollectFingerprint:  staticFingerprint("fp"),
				Content:             map[string]any{"point": tc.point},
			})
			if !errors.Is(saveErr, ErrInjectedCrash) {
				t.Fatalf("save error = %v, want injected crash", saveErr)
			}

			restarted, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
			if err != nil {
				t.Fatalf("new restarted store: %v", err)
			}
			report, err := restarted.Recover()
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if report.Outcome != tc.outcome || report.Revision != tc.revision {
				t.Fatalf("recovery report = %+v, want outcome=%s revision=%d", report, tc.outcome, tc.revision)
			}
			if tc.revision == 0 {
				if _, err := restarted.Load(); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("load after discarded intent = %v, want fs.ErrNotExist", err)
				}
				return
			}
			snapshot, err := restarted.Load()
			if err != nil {
				t.Fatalf("load recovered state: %v", err)
			}
			if snapshot.Revision != tc.revision {
				t.Fatalf("snapshot revision = %d, want %d", snapshot.Revision, tc.revision)
			}
		})
	}
}
