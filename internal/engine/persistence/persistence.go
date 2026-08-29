// Package persistence 是 engine 的持久化基座（阶段 2 批 1a：
// orchestration-pipeline-engine-phase-2/master-requirements.md §1/§2、
// final-implementation-draft §3.3/§9.2/§10）。
//
// 本包只在调用方给定的隔离状态目录内读写，不触碰 legacy 状态文件、
// 不改变任何公开 CLI 行为。交付六件基座能力：
//
//   - 版本信封：state 文档携带 writer/stateSchemaVersion/
//     workflowDefinitionVersion/packageDigest 四字段信封，外加 draft §3.3
//     要求与 packageDigest 双重绑定的 definitionDigest。读取侧严格校验：
//     缺失或任一字段不精确匹配一律 UNSUPPORTED_RUN_VERSION 写前拒绝、
//     绝不写状态。
//   - 原子保存：persist intent → execute → observe/reconcile → commit
//     四段协议；temp+rename 原子落盘；崩溃后重启由 Recover 从 intent
//     对账：补做提交、确认已提交或判定残留。
//   - 完整性摘要：contentDigest 对状态内容计算，读取与写入路径都与
//     信封一起校验，篡改可检测。
//   - 文件锁：状态目录级独占写锁（O_CREATE|O_EXCL 锁文件），同目录
//     并发写互斥；持有者崩溃后按陈旧判定可抢占。
//   - revision/CAS：每次写入携带期望 revision，锁内重读不匹配即冲突
//     拒绝。
//   - external fingerprint 重验：写入前后各校验一次外部事实指纹；写前
//     变化直接拒绝（什么都不写），写后变化提交仍原子完整，以
//     Committed=true 报告进入对账路径。
//
// 常量同源与依赖方向：
//
//   - workflowDefinitionVersion 与 definitionDigest 直接引用
//     internal/engine/definition 的生成身份常量（identity_gen.go，与
//     definitions/workflow.json 由同一生成动作产出）；
//   - stateSchemaVersion 复用 encoder 的引擎侧常量 "1"，语义对齐
//     internal/validate/phase0.go 的冻结常量 CurrentStateSchemaVersion，
//     由测试钉死相等；
//   - 本包不导入 internal/validate：阶段 5 将删除 legacy 实现，engine
//     包不得反向依赖（与 shadow 包相同的结构性约束）。
//
// Windows 平台分叉按阶段决策维持延期：execute 段的原子 rename 收敛在
// Store.executeCommit 单点，未来平台分层只需替换该调用。
package persistence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

const (
	// Writer 是版本信封 writer 字段的唯一合法值：state 只能由 engine 写入。
	Writer = "engine"

	// UnsupportedRunVersionCode 与 phase0 冻结错误码同值（测试钉死）：
	// 版本身份缺失或不精确匹配的统一拒绝码。
	UnsupportedRunVersionCode = "UNSUPPORTED_RUN_VERSION"
	// StateIntegrityCode 是完整性摘要不符（内容被篡改或损坏）的拒绝码。
	StateIntegrityCode = "STATE_INTEGRITY_MISMATCH"
	// RevisionConflictCode 是 CAS 冲突（期望 revision 与锁内现状不符）的拒绝码。
	RevisionConflictCode = "REVISION_CONFLICT"
	// FingerprintChangedCode 是外部事实指纹在写事务期间变化的拒绝/对账码。
	FingerprintChangedCode = "FINGERPRINT_CHANGED"
)

// FaultPoint names deterministic crash windows in the four-stage persistence
// protocol. Tests and isolated harnesses may inject a crash at one point; the
// production default is nil and has no fault behavior.
type FaultPoint string

const (
	FaultIntentBefore       FaultPoint = "intent_before"
	FaultIntentAfter        FaultPoint = "intent_after"
	FaultTempSyncBefore     FaultPoint = "temp_sync_before"
	FaultTempSyncAfter      FaultPoint = "temp_sync_after"
	FaultExecuteBefore      FaultPoint = "execute_before"
	FaultExecuteAfter       FaultPoint = "execute_after"
	FaultObserveBefore      FaultPoint = "observe_before"
	FaultReconcileBefore    FaultPoint = "reconcile_before"
	FaultReplaceBefore      FaultPoint = "replace_before"
	FaultReplaceAfter       FaultPoint = "replace_after"
	FaultCommitResponseLost FaultPoint = "commit_response_lost"
	// The remaining points are consumed by the test-only protocol adapters in
	// internal/engine/testkit. Keeping their names beside the persistence
	// windows gives every deterministic failure a single vocabulary.
	FaultSpawnAfterAttach    FaultPoint = "spawn_after_attach"
	FaultResultBeforeReceipt FaultPoint = "result_before_receipt"
	FaultSubmitResponseLost  FaultPoint = "submit_response_lost"
	FaultHostObserveBefore   FaultPoint = "host_observe_before"
	FaultHostReconcileBefore FaultPoint = "host_reconcile_before"
	FaultHostCommitBefore    FaultPoint = "host_commit_before"
	FaultHostCommitAfter     FaultPoint = "host_commit_after"
)

// ErrInjectedCrash identifies a simulated process loss. Save deliberately
// leaves the durable intent/temp files at these points so Store.Recover can
// exercise the same restart path as a real crash.
var ErrInjectedCrash = errors.New("persistence: injected crash")

type InjectedCrashError struct {
	Point FaultPoint
}

func (e *InjectedCrashError) Error() string {
	return fmt.Sprintf("%v at %s", ErrInjectedCrash, e.Point)
}
func (e *InjectedCrashError) Unwrap() error { return ErrInjectedCrash }

// CommitResponseLostError means the commit completed but the caller did not
// receive a normal response. Retrying with the same logical event must use the
// caller's idempotency protocol rather than executing a second side effect.
type CommitResponseLostError struct {
	Revision uint64
}

func (e *CommitResponseLostError) Error() string {
	return fmt.Sprintf("persistence: commit response lost after revision %d", e.Revision)
}

// Envelope 是 state 文档的版本信封。master-requirements §1 列举的四个
// 身份字段之外，definitionDigest 是 draft §3.3 的双重绑定字段：
// definitionDigest 绑定拓扑与语义身份（同源 definition 生成常量），
// packageDigest 绑定 owning runtime 的实现字节（安装事务在阶段 3+
// 计算并注入）。五个字段都在读取侧逐字段精确校验。
type Envelope struct {
	Writer                    string `json:"writer"`
	StateSchemaVersion        string `json:"stateSchemaVersion"`
	WorkflowDefinitionVersion string `json:"workflowDefinitionVersion"`
	DefinitionSource          string `json:"definitionSource,omitempty"`
	DefinitionDigest          string `json:"definitionDigest"`
	PackageDigest             string `json:"packageDigest"`
	InstalledTargetIdentity   string `json:"installedTargetIdentity,omitempty"`
}

// expectedEnvelope 返回当前 engine 身份下的唯一合法信封。
func expectedEnvelope(cfg Config) Envelope {
	return Envelope{
		Writer:                    Writer,
		StateSchemaVersion:        encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion,
		DefinitionSource:          cfg.DefinitionSource,
		DefinitionDigest:          definition.WorkflowDefinitionDigest,
		PackageDigest:             cfg.PackageDigest,
		InstalledTargetIdentity:   cfg.InstalledTargetIdentity,
	}
}

// ExpectedEnvelope returns the exact engine envelope for an owning package.
// It is used by the isolated harness to construct positive and negative
// envelope fixtures without opening a state file.
func ExpectedEnvelope(packageDigest string) (Envelope, error) {
	if strings.TrimSpace(packageDigest) == "" {
		return Envelope{}, fmt.Errorf("persistence: package digest is required")
	}
	return expectedEnvelope(Config{PackageDigest: packageDigest}), nil
}

// ValidateEnvelope applies the same exact write barrier used by Store reads.
// Validation is side-effect free, so a rejected candidate cannot create or
// truncate its intended target.
func ValidateEnvelope(observed Envelope, packageDigest string) error {
	if strings.TrimSpace(packageDigest) == "" {
		return fmt.Errorf("persistence: package digest is required")
	}
	return validateEnvelope(observed, Config{PackageDigest: packageDigest})
}

// ValidateEnvelopeWithIdentity is the candidate-facing write barrier.  It
// keeps the phase-2 package-only helper above intact while allowing a façade to
// require the full phase-3 definition-source and installed-target identity.
func ValidateEnvelopeWithIdentity(observed Envelope, packageDigest, definitionSource, installedTargetIdentity string) error {
	if strings.TrimSpace(packageDigest) == "" {
		return fmt.Errorf("persistence: package digest is required")
	}
	if strings.TrimSpace(definitionSource) == "" {
		return &UnsupportedRunVersionError{Field: "definitionSource", Expected: definitionSource, Observed: observed.DefinitionSource}
	}
	if strings.TrimSpace(installedTargetIdentity) == "" {
		return &UnsupportedRunVersionError{Field: "installedTargetIdentity", Expected: installedTargetIdentity, Observed: observed.InstalledTargetIdentity}
	}
	return validateEnvelope(observed, Config{
		PackageDigest: packageDigest, DefinitionSource: definitionSource,
		InstalledTargetIdentity: installedTargetIdentity,
	})
}

// validateEnvelope 逐字段精确比对信封。缺失字段即零值，同样落入不精确
// 匹配；调用方据此在写入前拒绝，本包不做迁移、修复或兼容读取。
func validateEnvelope(observed Envelope, cfg Config) error {
	expected := expectedEnvelope(cfg)
	checks := []struct {
		field, got, want string
	}{
		{"writer", observed.Writer, expected.Writer},
		{"stateSchemaVersion", observed.StateSchemaVersion, expected.StateSchemaVersion},
		{"workflowDefinitionVersion", observed.WorkflowDefinitionVersion, expected.WorkflowDefinitionVersion},
		{"definitionDigest", observed.DefinitionDigest, expected.DefinitionDigest},
		{"packageDigest", observed.PackageDigest, expected.PackageDigest},
	}
	if expected.DefinitionSource != "" {
		checks = append(checks, struct{ field, got, want string }{"definitionSource", observed.DefinitionSource, expected.DefinitionSource})
	}
	if expected.InstalledTargetIdentity != "" {
		checks = append(checks, struct{ field, got, want string }{"installedTargetIdentity", observed.InstalledTargetIdentity, expected.InstalledTargetIdentity})
	}
	for _, check := range checks {
		if check.got != check.want {
			return &UnsupportedRunVersionError{Field: check.field, Expected: check.want, Observed: check.got}
		}
	}
	return nil
}

// UnsupportedRunVersionError 表示 state 版本身份缺失或不精确匹配
// （draft §10）。携带该错误的读取/写入路径绝不写状态。
type UnsupportedRunVersionError struct {
	Field    string
	Expected string
	Observed string
}

func (e *UnsupportedRunVersionError) Error() string {
	return fmt.Sprintf("%s: %s expected %q, got %q", UnsupportedRunVersionCode, e.Field, e.Expected, e.Observed)
}

// IntegrityMismatchError 表示 contentDigest 与实际内容不符：状态文件被
// 篡改或损坏。携带该错误的路径绝不写状态。
type IntegrityMismatchError struct {
	Path     string
	Expected string
	Observed string
}

func (e *IntegrityMismatchError) Error() string {
	return fmt.Sprintf("%s: %s content digest expected %q, got %q", StateIntegrityCode, e.Path, e.Expected, e.Observed)
}

// RevisionConflictError 表示期望 revision 与锁内重读到的实际 revision
// 不一致（CAS 失败）：期间已有其他写入推进，本次写入拒绝。
type RevisionConflictError struct {
	Path     string
	Expected uint64
	Observed uint64
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("%s: %s revision expected %d, got %d", RevisionConflictCode, e.Path, e.Expected, e.Observed)
}

// 指纹重验发生的两个阶段。
const (
	// FingerprintPhaseBefore：persist intent 之前的重验。外部事实已变则
	// 直接拒绝，状态目录内不落任何协议字节。
	FingerprintPhaseBefore = "before write"
	// FingerprintPhaseAfter：execute/observe 之后的重验。此时提交已原子
	// 完整、不回滚；变化以 Committed=true 报告，调用方走对账路径
	// （重读、重新观察、重新决策），不得把过期事实当作已验证结果继续用。
	FingerprintPhaseAfter = "after write"
)

// FingerprintChangedError 表示外部事实指纹在写事务期间发生变化。
type FingerprintChangedError struct {
	Phase     string
	Committed bool
	Expected  string
	Observed  string
}

func (e *FingerprintChangedError) Error() string {
	if e.Committed {
		return fmt.Sprintf("%s (%s): expected %q, got %q; revision committed, re-observe and re-drive", FingerprintChangedCode, e.Phase, e.Expected, e.Observed)
	}
	return fmt.Sprintf("%s (%s): expected %q, got %q; nothing written", FingerprintChangedCode, e.Phase, e.Expected, e.Observed)
}

// LockHeldError 表示状态目录写锁被其他持有者占用（同目录并发写互斥）。
type LockHeldError struct {
	Path string
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("LOCK_HELD: state directory write lock is held: %s", e.Path)
}

// document 是 state 文件的完整 wire 形态：信封字段 + 单调 revision +
// 完整性摘要 + 内容。Content 以 RawMessage 原样承载调用方内容，本包
// 不理解其内部结构。
type document struct {
	Writer                    string          `json:"writer"`
	StateSchemaVersion        string          `json:"stateSchemaVersion"`
	WorkflowDefinitionVersion string          `json:"workflowDefinitionVersion"`
	DefinitionSource          string          `json:"definitionSource,omitempty"`
	DefinitionDigest          string          `json:"definitionDigest"`
	PackageDigest             string          `json:"packageDigest"`
	InstalledTargetIdentity   string          `json:"installedTargetIdentity,omitempty"`
	Revision                  uint64          `json:"revision"`
	ContentDigest             string          `json:"contentDigest"`
	Content                   json.RawMessage `json:"content"`
}

func (d *document) envelope() Envelope {
	return Envelope{
		Writer:                    d.Writer,
		StateSchemaVersion:        d.StateSchemaVersion,
		WorkflowDefinitionVersion: d.WorkflowDefinitionVersion,
		DefinitionSource:          d.DefinitionSource,
		DefinitionDigest:          d.DefinitionDigest,
		PackageDigest:             d.PackageDigest,
		InstalledTargetIdentity:   d.InstalledTargetIdentity,
	}
}

// contentDigestOf 计算 content 紧凑 JSON 字节的完整性摘要（复用 encoder
// 的 sha256: 前缀格式）。摘要两侧都取紧凑形态（Save 侧 json.Marshal、
// Load 侧 json.Compact）：外层文档以缩进形态书写时内嵌内容会被重新
// 缩进，摘要不因展示形态漂移，而内容的任何语义变化都会改变摘要。
func contentDigestOf(compactContent []byte) string {
	return encoder.Digest(compactContent)
}

// compactContent 把 content 归一为紧凑 JSON 字节。
func compactContent(raw json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, fmt.Errorf("persistence: compact content: %w", err)
	}
	return buf.Bytes(), nil
}
