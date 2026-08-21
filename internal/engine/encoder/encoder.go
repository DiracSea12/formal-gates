// Package encoder 是 ADR-001 决策 4/5 的单一 canonical encoder：唯一编码入口
// Encode 只认识 compiler.CompiledDefinition，不为各 authoring 变体分别实现
// MarshalCanonical（避免默认值处理、字段排序与新增变体漏编漂移）。
//
// canonical 形态（spike 结论，全仓统一）：JSON、2 空格缩进、不转义 HTML、
// 恰一个尾随换行；wire 结构独占形态定义（IR 结构不带 json tag）；duration
// 一律 int64 纳秒；无 map、无 float、无 time.Time、无函数/路径/时间——字节
// 输出只是 IR 的函数，同 IR 必得同字节。
//
// 变体判别只存在于本文件的 payload type switch（encode/decode 各一处）：
// 新增变体实例不触碰本包；新增变体种类才加 case。
//
// 身份：DefinitionDigest = 制品字节 SHA-256（Digest 函数，sha256: 前缀）。
// handler 实现变化不进入制品字节，因此只改实现不改 ID/定义时 definition
// digest 不变（digest 分离的制品级基础）；PackageDigest 由安装事务另行计算，
// 属后续批次。
package encoder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// 制品信封常量。信封沿阶段 0 冻结的 definitions/workflow.json 字段（spike
// 结论第 7 条：统一到同一 encoder 产物，否则 freshness 验收被空白差异击穿），
// $schema 随 workflowDefinitionVersion "1"->"2" 同步演进；stateSchemaVersion
// 保持 "1"（状态 schema 未随本批变化，与 internal/validate 同值）。
// LoadFutureDefinition（internal/validate/future.go）解析本制品时读取
// stateSchemaVersion 与 version 两个字段，信封必须保留它们。
const (
	artifactSchema           = "https://formal-gates.dev/schema/workflow-definition-v2.json"
	ArtifactID               = "formal-gates.workflow"
	StateSchemaVersion       = "1"
	artifactWriter           = "engine"
	artifactTransactionOwner = "validate"
)

// artifactEntrypoints 是制品信封的公共入口声明（沿阶段 0 冻结值）。
var artifactEntrypoints = []string{"workflow start", "workflow resume", "workflow seal"}

// definitionWire 是制品顶层 wire 结构。字段声明序即 JSON 键序（encoding/json
// 对 struct 字段按声明序输出）；信封字段在前，拓扑字段（entryNode/steps）在后。
type definitionWire struct {
	Schema             string     `json:"$schema"`
	ID                 string     `json:"id"`
	Version            string     `json:"version"`
	StateSchemaVersion string     `json:"stateSchemaVersion"`
	Writer             string     `json:"writer"`
	TransactionOwner   string     `json:"transactionOwner"`
	Entrypoints        []string   `json:"entrypoints"`
	EntryNode          string     `json:"entryNode"`
	Steps              []stepWire `json:"steps"`
}

// stepWire 是单步 wire 结构：公共头（含 compiler 派生的 ordinal 与物化的
// authority/runner）+ 可选共享 IO 段 + 变体 payload。零值归一化：可选策略
// 指针化（retry）、空集合省略（dependencies/preconditions/postconditions/inputs/
// io）、默认枚举位省略（negated）。Payload 用 RawMessage 承载：encode 侧由
// payload wire 结构内层编码（同一 HTML 转义策略），decode 侧按 kind 严格解析。
type stepWire struct {
	ID                string          `json:"id"`
	NodeID            string          `json:"nodeId"`
	Ordinal           int             `json:"ordinal"`
	Kind              string          `json:"kind"`
	DefinitionVersion string          `json:"definitionVersion"`
	Dependencies      []string        `json:"dependencies,omitempty"`
	Authority         string          `json:"authority"`
	Runner            string          `json:"runner"`
	IO                *ioWire         `json:"io,omitempty"`
	Payload           json.RawMessage `json:"payload"`
}

// ioWire 是共享 IO 段（仅 local/durable/host_action/agent 四个可执行变体）：
// human 的 typed I/O 是 payload 内 schema，parallel 是纯调度语义，二者制品中
// 不携带 io 键。
type ioWire struct {
	InputCodec     string          `json:"inputCodec"`
	OutputCodec    string          `json:"outputCodec"`
	Preconditions  []predicateWire `json:"preconditions,omitempty"`
	Postconditions []predicateWire `json:"postconditions,omitempty"`
	Inputs         []bindingWire   `json:"inputs,omitempty"`
}

type predicateWire struct {
	ID      string `json:"id"`
	Negated bool   `json:"negated,omitempty"`
}

type bindingWire struct {
	From   string `json:"from"`
	Output string `json:"output"`
	To     string `json:"to"`
}

// 变体 payload wire：每变体确切字段，与 IR payload 一一对应；duration 一律
// int64 纳秒。local 的 timeout 可选（0 省略）、retry 可选（指针）；durable
// 的幂等/reconcile/timeout/retry 必填。
type localPayloadWire struct {
	Handler   string     `json:"handler"`
	TimeoutNs int64      `json:"timeoutNs,omitempty"`
	Retry     *retryWire `json:"retry,omitempty"`
}

type durablePayloadWire struct {
	Handler     string     `json:"handler"`
	Idempotency string     `json:"idempotency"`
	Reconcile   string     `json:"reconcileId"`
	TimeoutNs   int64      `json:"timeoutNs"`
	Retry       retryWire  `json:"retry"`
}

type hostPayloadWire struct {
	Handler   string `json:"handler"`
	Boundary  string `json:"boundary"`
	Operation string `json:"operation"`
	TimeoutNs int64  `json:"timeoutNs"`
}

type agentPayloadWire struct {
	Handler   string     `json:"handler"`
	Reason    string     `json:"nonProgrammableReason"`
	TimeoutNs int64      `json:"timeoutNs"`
	Retry     *retryWire `json:"retry,omitempty"`
}

type humanPayloadWire struct {
	AskKind        string `json:"askKind"`
	RequestSchema  string `json:"requestSchema"`
	ResponseSchema string `json:"responseSchema"`
	FreshnessTtlNs int64  `json:"freshnessTtlNs"`
}

type parallelPayloadWire struct {
	Children []string    `json:"children"`
	Join     joinWire    `json:"join"`
	Failure  failureWire `json:"failure"`
}

type retryWire struct {
	MaxAttempts int   `json:"maxAttempts"`
	BackoffNs   int64 `json:"backoffNs,omitempty"`
}

type joinWire struct {
	JoinStep string `json:"joinStep"`
	Mode     string `json:"mode"`
}

type failureWire struct {
	Mode     string `json:"mode"`
	Escalate string `json:"escalate"`
}

// kindProfile 是变体物化档案：kind 对应的 payload IR 类型名、派生
// authority/runner。篡改 authority/kind 的 mutation 由本表机械拒绝。
type kindProfile struct {
	payloadType string
	authority   authoring.DecisionAuthority
	runner      authoring.RunnerKind
}

// kindProfiles 是六变体的封闭物化档案（与 compiler.materializeStep 的派生
// 一致）。payloadType 是 IR payload 类型名，用于 kind↔payload 判别。
var kindProfiles = map[compiler.StepKind]kindProfile{
	compiler.KindLocal:      {payloadType: payloadTypeName(compiler.CompiledLocalStep{}), authority: authoring.AuthorityEngine, runner: authoring.RunnerEngineLocal},
	compiler.KindDurable:    {payloadType: payloadTypeName(compiler.CompiledDurableStep{}), authority: authoring.AuthorityEngine, runner: authoring.RunnerDurableActivity},
	compiler.KindHostAction: {payloadType: payloadTypeName(compiler.CompiledHostActionStep{}), authority: authoring.AuthorityEngine, runner: authoring.RunnerHostAdapter},
	compiler.KindAgent:      {payloadType: payloadTypeName(compiler.CompiledAgentStep{}), authority: authoring.AuthorityAgent, runner: authoring.RunnerAgentWorker},
	compiler.KindHumanAsk:   {payloadType: payloadTypeName(compiler.CompiledHumanAskStep{}), authority: authoring.AuthorityHuman, runner: authoring.RunnerHostAdapter},
	compiler.KindParallel:   {payloadType: payloadTypeName(compiler.CompiledParallelStep{}), authority: authoring.AuthorityEngine, runner: authoring.RunnerEngineLocal},
}

// Encode 把 CompiledDefinition 编码为 canonical 制品字节。输入除 compiler
// 输出外还可能是 Decode 的产物（round-trip）；两条路径都经 checkCoherence
// 拒绝 kind/payload/物化维度不一致的 IR，携带 MISSING_ENGINE_ADAPTER marker
// 的 diagnostic-only 定义不得物化为 canonical 制品身份。
func Encode(cd *compiler.CompiledDefinition) ([]byte, error) {
	if err := checkCoherence(cd); err != nil {
		return nil, err
	}
	w, err := definitionToWire(cd)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(w); err != nil {
		return nil, fmt.Errorf("encoder: %w", err)
	}
	// json.Encoder.Encode 自带恰一个尾随换行，即 canonical 空白形态。
	return buf.Bytes(), nil
}

// Decode 把 canonical 制品字节解析回 CompiledDefinition。两级严格
// （外层与 payload 各自 DisallowUnknownFields，closed world 不被未知字段
// 穿透）；信封字段与常量精确比对（防 decode→encode 静默"归一化"改写身份）；
// kind/payload/authority/runner 一致性经 checkCoherence 复核。
func Decode(data []byte) (*compiler.CompiledDefinition, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w definitionWire
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("encoder: decode definition: %w", err)
	}
	// 制品是单一 JSON 文档；尾随内容（常见操作失误）不得被静默丢弃。
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return nil, errors.New("encoder: decode definition: trailing content after JSON document")
	}
	if err := checkEnvelope(&w); err != nil {
		return nil, err
	}
	cd := &compiler.CompiledDefinition{Version: authoring.DefinitionVersion(w.Version), EntryNode: authoring.NodeID(w.EntryNode)}
	cd.Steps = make([]compiler.CompiledStep, 0, len(w.Steps))
	for i := range w.Steps {
		cs, err := stepFromWire(w.Version, &w.Steps[i])
		if err != nil {
			return nil, err
		}
		cd.Steps = append(cd.Steps, *cs)
	}
	if err := checkCoherence(cd); err != nil {
		return nil, err
	}
	return cd, nil
}

// Digest 返回制品字节的 SHA-256 摘要（sha256: 前缀，与仓库现行格式一致）。
// DefinitionDigest 即本函数对 canonical 制品字节的结果。
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// checkEnvelope 校验制品信封与常量精确一致：信封身份字段（$schema/id/
// stateSchemaVersion/writer/transactionOwner/entrypoints）是 canonical 形态
// 的一部分，不得由解码方静默改写。
func checkEnvelope(w *definitionWire) error {
	checks := []struct {
		field, observed, expected string
	}{
		{"$schema", w.Schema, artifactSchema},
		{"id", w.ID, ArtifactID},
		{"stateSchemaVersion", w.StateSchemaVersion, StateSchemaVersion},
		{"writer", w.Writer, artifactWriter},
		{"transactionOwner", w.TransactionOwner, artifactTransactionOwner},
	}
	for _, c := range checks {
		if c.observed != c.expected {
			return fmt.Errorf("encoder: envelope %s = %q, want %q", c.field, c.observed, c.expected)
		}
	}
	if len(w.Entrypoints) != len(artifactEntrypoints) {
		return fmt.Errorf("encoder: envelope entrypoints = %v, want %v", w.Entrypoints, artifactEntrypoints)
	}
	for i, ep := range artifactEntrypoints {
		if w.Entrypoints[i] != ep {
			return fmt.Errorf("encoder: envelope entrypoints = %v, want %v", w.Entrypoints, artifactEntrypoints)
		}
	}
	if w.Version == "" || w.EntryNode == "" || len(w.Steps) == 0 {
		return fmt.Errorf("encoder: envelope requires version, entryNode and steps")
	}
	return nil
}

// checkCoherence 拒绝物化不一致的 IR：marker 定义、空信封、kind 与 payload
// 变体不符、authority/runner 与变体派生物不符、步骤版本未与信封绑定。
// compiler 正常输出天然满足；本检查是 encode/decode 共用的二次防线。
func checkCoherence(cd *compiler.CompiledDefinition) error {
	if cd == nil {
		return errors.New("encoder: nil definition")
	}
	if cd.MissingEngineAdapter {
		return errors.New("encoder: definition carries MISSING_ENGINE_ADAPTER marker; diagnostic-only definitions must not become the canonical artifact")
	}
	if cd.Version == "" || cd.EntryNode == "" || len(cd.Steps) == 0 {
		return errors.New("encoder: definition requires version, entryNode and steps")
	}
	for i := range cd.Steps {
		cs := &cd.Steps[i]
		profile, ok := kindProfiles[cs.Header.Kind]
		if !ok {
			return fmt.Errorf("encoder: step %q: unknown kind %q", cs.Header.ID, cs.Header.Kind)
		}
		if payloadTypeName(cs.Payload) != profile.payloadType {
			return fmt.Errorf("encoder: step %q: payload %s does not match kind %s", cs.Header.ID, payloadTypeName(cs.Payload), cs.Header.Kind)
		}
		if cs.Header.Authority != profile.authority || cs.Header.Runner != profile.runner {
			return fmt.Errorf("encoder: step %q: authority/runner = %s/%s, want %s/%s for kind %s",
				cs.Header.ID, cs.Header.Authority, cs.Header.Runner, profile.authority, profile.runner, cs.Header.Kind)
		}
		if cs.Header.DefinitionVersion != cd.Version {
			return fmt.Errorf("encoder: step %q: definitionVersion %q != definition %q", cs.Header.ID, cs.Header.DefinitionVersion, cd.Version)
		}
	}
	return nil
}

// payloadTypeName 返回 payload 的 IR 类型名，用于 kind↔payload 判别。
func payloadTypeName(p compiler.Payload) string {
	return fmt.Sprintf("%T", p)
}

// definitionToWire 把 IR 转为 wire 结构（变体判别唯一 encode 侧 switch）。
func definitionToWire(cd *compiler.CompiledDefinition) (*definitionWire, error) {
	w := &definitionWire{
		Schema:             artifactSchema,
		ID:                 ArtifactID,
		Version:            string(cd.Version),
		StateSchemaVersion: StateSchemaVersion,
		Writer:             artifactWriter,
		TransactionOwner:   artifactTransactionOwner,
		Entrypoints:        artifactEntrypoints,
		EntryNode:          string(cd.EntryNode),
		Steps:              make([]stepWire, 0, len(cd.Steps)),
	}
	for i := range cd.Steps {
		cs := &cd.Steps[i]
		sw := stepWire{
			ID:                string(cs.Header.ID),
			NodeID:            string(cs.Header.NodeID),
			Ordinal:           cs.Header.Ordinal,
			Kind:              string(cs.Header.Kind),
			DefinitionVersion: string(cs.Header.DefinitionVersion),
			Authority:         string(cs.Header.Authority),
			Runner:            string(cs.Header.Runner),
		}
		if deps := idStrings(cs.Header.Dependencies); len(deps) > 0 {
			sw.Dependencies = deps
		}
		// human/parallel 不物化 IO（compiler 保证零值）；可执行变体必带。
		if cs.IO.InputCodec != "" || cs.IO.OutputCodec != "" || len(cs.IO.Preconditions) > 0 ||
			len(cs.IO.Postconditions) > 0 || len(cs.IO.Inputs) > 0 {
			sw.IO = ioToWire(&cs.IO)
		}
		payload, err := payloadToWire(cs)
		if err != nil {
			return nil, err
		}
		raw, err := marshalPayload(payload)
		if err != nil {
			return nil, err
		}
		sw.Payload = raw
		w.Steps = append(w.Steps, sw)
	}
	return w, nil
}

func ioToWire(io *authoring.IO) *ioWire {
	out := &ioWire{InputCodec: string(io.InputCodec), OutputCodec: string(io.OutputCodec)}
	if len(io.Preconditions) > 0 {
		out.Preconditions = predicateWires(io.Preconditions)
	}
	if len(io.Postconditions) > 0 {
		out.Postconditions = predicateWires(io.Postconditions)
	}
	if len(io.Inputs) > 0 {
		out.Inputs = make([]bindingWire, 0, len(io.Inputs))
		for _, b := range io.Inputs {
			out.Inputs = append(out.Inputs, bindingWire{From: string(b.From), Output: b.OutputField, To: b.ToField})
		}
	}
	return out
}

func predicateWires(refs []authoring.PredicateRef) []predicateWire {
	out := make([]predicateWire, 0, len(refs))
	for _, ref := range refs {
		out = append(out, predicateWire{ID: string(ref.ID), Negated: ref.Negated})
	}
	return out
}

func retryToWire(r *authoring.RetryPolicy) *retryWire {
	if r == nil {
		return nil
	}
	return &retryWire{MaxAttempts: r.MaxAttempts, BackoffNs: int64(r.Backoff)}
}

func idStrings(ids []authoring.StepID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// payloadToWire 是 encode 侧唯一变体感知点：IR payload 类型 -> payload wire。
func payloadToWire(cs *compiler.CompiledStep) (any, error) {
	switch p := cs.Payload.(type) {
	case compiler.CompiledLocalStep:
		return localPayloadWire{Handler: string(p.Handler), TimeoutNs: int64(p.Timeout), Retry: retryToWire(p.Retry)}, nil
	case compiler.CompiledDurableStep:
		retry := retryWire{MaxAttempts: p.Retry.MaxAttempts, BackoffNs: int64(p.Retry.Backoff)}
		return durablePayloadWire{Handler: string(p.Handler), Idempotency: string(p.Idempotency),
			Reconcile: string(p.Reconcile), TimeoutNs: int64(p.Timeout), Retry: retry}, nil
	case compiler.CompiledHostActionStep:
		return hostPayloadWire{Handler: string(p.Handler), Boundary: string(p.Boundary),
			Operation: string(p.Operation), TimeoutNs: int64(p.Timeout)}, nil
	case compiler.CompiledAgentStep:
		return agentPayloadWire{Handler: string(p.Handler), Reason: string(p.Reason),
			TimeoutNs: int64(p.Timeout), Retry: retryToWire(p.Retry)}, nil
	case compiler.CompiledHumanAskStep:
		return humanPayloadWire{AskKind: p.AskKind, RequestSchema: string(p.RequestSchema),
			ResponseSchema: string(p.ResponseSchema), FreshnessTtlNs: int64(p.FreshnessTTL)}, nil
	case compiler.CompiledParallelStep:
		join := joinWire{JoinStep: string(p.Join.JoinStep), Mode: string(p.Join.Mode)}
		failure := failureWire{Mode: string(p.Failure.Mode), Escalate: string(p.Failure.Escalate)}
		return parallelPayloadWire{Children: idStrings(p.Children), Join: join, Failure: failure}, nil
	default:
		return nil, fmt.Errorf("encoder: step %q: unknown payload variant %T", cs.Header.ID, cs.Payload)
	}
}

// marshalPayload 把 payload wire 内层编码为 RawMessage。内层同样关闭 HTML
// 转义，与外层 canonical 形态一致；Encoder 自带的尾换行在外层嵌入时被压缩
// 丢弃，不影响制品字节。
func marshalPayload(payload any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, fmt.Errorf("encoder: marshal payload: %w", err)
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}
