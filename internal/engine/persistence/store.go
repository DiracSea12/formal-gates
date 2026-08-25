package persistence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 状态目录内的协议文件名。temp 文件名形如 ".state.json.<random>.tmp"，
// 前后缀用于识别崩溃遗留的暂存文件，Recover 对账时清扫不被 intent
// 引用的残留。
const (
	stateFileName  = "state.json"
	intentFileName = "state.json.intent"
	lockFileName   = "write.lock"
	tempPrefix     = ".state.json."
	tempSuffix     = ".tmp"
)

// Config 是 Store 的身份配置。PackageDigest 是 owning runtime 的安装
// 身份摘要：本批（阶段 2 批 1a）只交付信封校验侧，其实际计算与安装
// 绑定随阶段 3+ 安装事务交付（master-requirements §9），这里由调用方
// 注入期望值并参与逐字段精确校验。
type Config struct {
	PackageDigest string
	FaultInjector func(FaultPoint) error
}

// Snapshot 是一次成功 Load 的只读结果：revision、内容摘要与内容字节。
// 信封字段已经严格校验等于当前 engine 身份，不再单独暴露。
type Snapshot struct {
	Revision      uint64
	ContentDigest string
	Content       json.RawMessage
}

// Transaction 是一次写事务的输入（draft §3.3 的写事务形态在本包的
// 落地）：读 state → Observe → Decide 之后，调用方带着读到的 revision
// 与观察到的外部事实指纹进入 Save。
type Transaction struct {
	// ExpectedRevision 是调用方读 state 时看到的 revision；Save 在锁内
	// 重读后不匹配即 CAS 冲突拒绝。
	ExpectedRevision uint64
	// ExpectedFingerprint 是调用方 Observe 得到的外部事实指纹（例如
	// decision.Observation.Digest 的结果）；空值拒绝。
	ExpectedFingerprint string
	// CollectFingerprint 在锁内重验指纹时调用（写前、写后各一次），
	// 必须重新收集外部事实而不是返回缓存值。
	CollectFingerprint func() (string, error)
	// Content 是新状态内容；nil 拒绝。内容结构对本包不透明。
	Content any
}

// SaveResult 是提交完结后的最小结果。
type SaveResult struct {
	Revision uint64
}

// Store 管理一个隔离状态目录的 engine state 读取、写入与崩溃恢复。
type Store struct {
	dir string
	cfg Config
}

func (s *Store) inject(point FaultPoint) error {
	if s.cfg.FaultInjector == nil {
		return nil
	}
	return s.cfg.FaultInjector(point)
}

// NewStore 构造状态目录的 Store。PackageDigest 必填：没有安装身份就
// 不存在合法信封，任何读写都会被信封校验拒绝，与其在每次操作晚失败，
// 不如在构造期直接拒绝。
func NewStore(dir string, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.PackageDigest) == "" {
		return nil, fmt.Errorf("persistence: config PackageDigest is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Store{dir: abs, cfg: cfg}, nil
}

func (s *Store) statePath() string { return filepath.Join(s.dir, stateFileName) }
func (s *Store) intentPath() string {
	return filepath.Join(s.dir, intentFileName)
}
func (s *Store) lockPath() string { return filepath.Join(s.dir, lockFileName) }

// Load 读取并校验当前 state。文件不存在时返回包装的 fs.ErrNotExist；
// 信封缺失或不精确匹配返回 UnsupportedRunVersionError，摘要不符返回
// IntegrityMismatchError。Load 不加锁：最终文件的替换是原子 rename，
// 读到的永远是某一次完整提交。
func (s *Store) Load() (*Snapshot, error) {
	doc, err := s.readDocument()
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("persistence: load %s: %w", s.statePath(), os.ErrNotExist)
	}
	return &Snapshot{Revision: doc.Revision, ContentDigest: doc.ContentDigest, Content: doc.Content}, nil
}

// readDocument 读取最终状态文件并完成信封与完整性摘要校验；文件不
// 存在返回 (nil, nil)（全新目录，revision 视为 0）。
func (s *Store) readDocument() (*document, error) {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("persistence: decode %s: %w", s.statePath(), err)
	}
	if err := validateEnvelope(doc.envelope(), s.cfg); err != nil {
		return nil, err
	}
	compact, err := compactContent(doc.Content)
	if err != nil {
		return nil, err
	}
	if observed := contentDigestOf(compact); observed != doc.ContentDigest {
		return nil, &IntegrityMismatchError{Path: s.statePath(), Expected: doc.ContentDigest, Observed: observed}
	}
	return &doc, nil
}

// Save 执行一次完整写事务（draft §3.3/§9.2 的持久化基座形态）：
//
//	加锁重读 → 信封/摘要校验 → revision CAS → 写前指纹重验 →
//	persist intent → execute（原子 rename）→ observe/reconcile →
//	commit result（写后指纹重验 + 清除 intent）
//
// 一切拒绝都发生在 persist intent 之前：被拒路径不写状态文件，也不留
// intent/temp 协议字节（锁文件的创建与释放是目录互斥机制，不属于状态
// 写入）。
func (s *Store) Save(tx Transaction) (SaveResult, error) {
	if tx.Content == nil {
		return SaveResult{}, fmt.Errorf("persistence: save: content is required")
	}
	if tx.CollectFingerprint == nil {
		return SaveResult{}, fmt.Errorf("persistence: save: CollectFingerprint is required")
	}
	if strings.TrimSpace(tx.ExpectedFingerprint) == "" {
		return SaveResult{}, fmt.Errorf("persistence: save: ExpectedFingerprint is required")
	}
	processLock := transactionLock(s.dir)
	processLock.Lock()
	defer processLock.Unlock()
	unlock, err := s.acquireLock()
	if err != nil {
		return SaveResult{}, err
	}
	defer unlock()

	// 加锁重读：后续所有校验针对锁内现状，不依赖加锁前的旧投影。
	// 信封或摘要不通过（UNSUPPORTED_RUN_VERSION / 篡改）在这里直接
	// 拒绝，绝不写状态。
	current, err := s.readDocument()
	if err != nil {
		return SaveResult{}, err
	}
	var observedRevision uint64
	if current != nil {
		observedRevision = current.Revision
	}
	if observedRevision != tx.ExpectedRevision {
		return SaveResult{}, &RevisionConflictError{
			Path: s.statePath(), Expected: tx.ExpectedRevision, Observed: observedRevision,
		}
	}

	// 写前指纹重验：外部事实已变则直接拒绝（draft §3.3：外部事实变化
	// 释放锁重算，不依据过期 HEAD 或需求摘要推进）。
	fingerprint, err := tx.CollectFingerprint()
	if err != nil {
		return SaveResult{}, fmt.Errorf("persistence: save: fingerprint before write: %w", err)
	}
	if fingerprint != tx.ExpectedFingerprint {
		return SaveResult{}, &FingerprintChangedError{
			Phase: FingerprintPhaseBefore, Expected: tx.ExpectedFingerprint, Observed: fingerprint,
		}
	}

	// ---- 1. persist intent：完整新文档先原子落盘到暂存文件并 fsync，
	// 再写 intent 记录。intent 存在即代表一次未完结提交，此后崩溃由
	// Recover 对账。revision 单调 +1，仅用于 CAS。
	content, err := json.Marshal(tx.Content)
	if err != nil {
		return SaveResult{}, fmt.Errorf("persistence: save: encode content: %w", err)
	}
	doc := newDocument(s.cfg, tx.ExpectedRevision+1, contentDigestOf(content), content)
	tempName, err := s.writeTempDocument(doc)
	if err != nil {
		return SaveResult{}, err
	}
	if err := s.inject(FaultIntentBefore); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeTemp(tempName)
		return SaveResult{}, err
	}
	intent := intentRecord{
		Target:           stateFileName,
		Temp:             tempName,
		ExpectedRevision: tx.ExpectedRevision,
		NextRevision:     doc.Revision,
		ContentDigest:    doc.ContentDigest,
	}
	if err := s.writeIntent(intent); err != nil {
		s.removeTemp(tempName)
		return SaveResult{}, err
	}
	if err := s.inject(FaultIntentAfter); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		s.removeTemp(tempName)
		return SaveResult{}, err
	}

	// ---- 2. execute：暂存文件原子替换最终状态文件并 fsync 目录。进程
	// 内失败时自清理本轮 intent/temp；真正崩溃则留给 Recover 对账。
	if err := s.inject(FaultReplaceBefore); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		s.removeTemp(tempName)
		return SaveResult{}, err
	}
	if err := s.inject(FaultExecuteBefore); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		s.removeTemp(tempName)
		return SaveResult{}, err
	}
	if err := s.executeCommit(tempName); err != nil {
		s.removeIntent()
		s.removeTemp(tempName)
		return SaveResult{}, err
	}
	if err := s.inject(FaultReplaceAfter); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		return SaveResult{}, err
	}
	if err := s.inject(FaultExecuteAfter); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		return SaveResult{}, err
	}

	// ---- 3. observe/reconcile：重读最终文件，核对信封、摘要与 revision
	// 恰为 intent 声明的终态。
	if err := s.inject(FaultObserveBefore); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		return SaveResult{}, err
	}
	if err := s.inject(FaultReconcileBefore); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		s.removeIntent()
		return SaveResult{}, err
	}
	reread, err := s.readDocument()
	if err != nil {
		s.removeIntent()
		return SaveResult{}, fmt.Errorf("persistence: save: observe after commit: %w", err)
	}
	if reread == nil || reread.Revision != intent.NextRevision || reread.ContentDigest != intent.ContentDigest {
		s.removeIntent()
		return SaveResult{}, fmt.Errorf(
			"persistence: save: observe after commit: %s is not at intent revision %d (digest %s)",
			s.statePath(), intent.NextRevision, intent.ContentDigest)
	}

	// ---- 4. commit result：写后指纹重验，然后清除 intent，提交完结。
	// 写后变化不回滚（提交已原子完整且内部自洽），以 Committed=true
	// 报告对账路径：调用方必须重新观察外部事实并重算。
	fingerprintAfter, ferr := tx.CollectFingerprint()
	if ferr != nil {
		_ = s.removeIntent()
		return SaveResult{Revision: doc.Revision}, fmt.Errorf(
			"persistence: save: committed revision %d but fingerprint re-verification failed: %w", doc.Revision, ferr)
	}
	if fingerprintAfter != tx.ExpectedFingerprint {
		_ = s.removeIntent()
		return SaveResult{Revision: doc.Revision}, &FingerprintChangedError{
			Phase: FingerprintPhaseAfter, Committed: true,
			Expected: tx.ExpectedFingerprint, Observed: fingerprintAfter,
		}
	}
	if err := s.removeIntent(); err != nil {
		return SaveResult{Revision: doc.Revision}, err
	}
	if err := s.inject(FaultCommitResponseLost); err != nil {
		if errors.Is(err, ErrInjectedCrash) {
			return SaveResult{}, err
		}
		return SaveResult{Revision: doc.Revision}, &CommitResponseLostError{Revision: doc.Revision}
	}
	return SaveResult{Revision: doc.Revision}, nil
}

// newDocument 由当前身份构造带信封的完整文档。
func newDocument(cfg Config, revision uint64, contentDigest string, content []byte) *document {
	env := expectedEnvelope(cfg)
	return &document{
		Writer:                    env.Writer,
		StateSchemaVersion:        env.StateSchemaVersion,
		WorkflowDefinitionVersion: env.WorkflowDefinitionVersion,
		DefinitionDigest:          env.DefinitionDigest,
		PackageDigest:             env.PackageDigest,
		Revision:                  revision,
		ContentDigest:             contentDigest,
		Content:                   append(json.RawMessage(nil), content...),
	}
}

// writeTempDocument 把完整新文档写入目录内暂存文件，fsync 后返回文件
// 名（目录内相对名）。暂存文件是 intent 的执行载体：内容与最终文件
// 完全一致，execute 段只做原子 rename。
func (s *Store) writeTempDocument(doc *document) (string, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	data, err := canonicalJSON(doc)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.dir, tempPrefix+"*"+tempSuffix)
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := s.inject(FaultTempSyncBefore); err != nil {
		_ = tmp.Close()
		if !errors.Is(err, ErrInjectedCrash) {
			_ = os.Remove(name)
		}
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := s.inject(FaultTempSyncAfter); err != nil {
		_ = tmp.Close()
		if !errors.Is(err, ErrInjectedCrash) {
			_ = os.Remove(name)
		}
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return filepath.Base(name), nil
}

// writeIntent 落盘 intent 记录（O_TRUNC 直写 + fsync）。intent 是对账
// 锚点：写它的时候 rename 尚未发生，部分写入的坏 intent 只会被对账
// 判为残留并清除，不会破坏现状，因此不需要自身的两级原子写。
func (s *Store) writeIntent(intent intentRecord) error {
	data, err := canonicalJSON(intent)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.intentPath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// executeCommit 是 execute 段单点：暂存文件原子替换最终状态文件，
// 随后 fsync 目录使 rename 持久。Windows 平台分叉按阶段决策延期，
// 未来分层只替换本函数内的 rename 实现。
func (s *Store) executeCommit(tempName string) error {
	if err := os.Rename(filepath.Join(s.dir, tempName), s.statePath()); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

// removeIntent 清除 intent 记录（提交完结或判定残留）；不存在视为成功。
func (s *Store) removeIntent() error {
	if err := os.Remove(s.intentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeTemp 清除暂存文件，失败只忽略（Recover 的残留清扫兜底）。
func (s *Store) removeTemp(name string) {
	_ = os.Remove(filepath.Join(s.dir, name))
}

// canonicalJSON 是本包统一的 canonical 编码，与 engine 各包同形态：
// JSON、2 空格缩进、不转义 HTML、恰一个尾随换行。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("persistence: canonical encode: %w", err)
	}
	return buf.Bytes(), nil
}
