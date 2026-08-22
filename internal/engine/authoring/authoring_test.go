package authoring

import (
	"strings"
	"testing"
	"time"
)

// 本文件覆盖批次 1a 的两类配套测试：
//  1. constructor 非法状态全覆盖——每个 New*Step 的每个拒绝分支至少一行
//     （公共头/共享 IO 段的共享分支在每个变体上重复出现，防止某变体日后
//     漏接共享校验时测试仍绿）；
//  2. 六变体 × 双维派生正确性——Authority/RunnerKind 是变体类型的常量
//     函数，作者入参中不存在填写入口。

// validHeader 返回合法公共头。依赖故意乱序且含重复，用于同时验证归一化。
func validHeader() Header {
	return Header{
		ID:                "node.s1",
		NodeID:            "node",
		Dependencies:      []StepID{"node.b", "node.a", "node.b"},
		DefinitionVersion: "wf-v1",
	}
}

// validIO 返回合法共享 IO 段（含 pre/postcondition 引用与 typed 输入绑定）。
func validIO() IO {
	return IO{
		InputCodec:     "codec.node.in",
		OutputCodec:    "codec.node.out",
		Preconditions:  []PredicateRef{{ID: "pred.node.pre"}},
		Postconditions: []PredicateRef{{ID: "pred.node.post"}},
		Inputs:         []InputBinding{{From: "node.a", OutputField: "out", ToField: "in"}},
	}
}

// wantErr 匹配错误消息子串：子串取自该分支的 distinguishing 文案，确保
// 命中的是目标分支而不是偶然的非 nil 错误。
func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("want error containing %q, got %q", substr, err.Error())
	}
}

// sharedRejectRows 是公共头拒绝分支的通用表：每个变体的测试都跑一遍，
// 证明共享校验在所有 constructor 上都已接线。
func sharedRejectRows() []struct {
	name   string
	mutate func(h *Header)
	want   string
} {
	return []struct {
		name   string
		mutate func(h *Header)
		want   string
	}{
		{"empty id", func(h *Header) { h.ID = "" }, "step id is empty"},
		{"empty node id", func(h *Header) { h.NodeID = "" }, "node id is empty"},
		{"empty definition version", func(h *Header) { h.DefinitionVersion = "" }, "definition version is empty"},
		{"empty dependency id", func(h *Header) { h.Dependencies = []StepID{"node.a", ""} }, "empty dependency id"},
		{"self dependency", func(h *Header) { h.Dependencies = []StepID{"node.s1"} }, "references the step itself"},
		// 封板后审计 H4：ID/NodeID 段内 "/" 会使 canonical task key 坍缩
		//（{n,a/b} 与 {n/a,b} 同为 n/a/b），构造层直接拒绝。
		{"id with path separator", func(h *Header) { h.ID = "node/s1" }, `must not contain "/"`},
		{"node id with path separator", func(h *Header) { h.NodeID = "node/sub" }, `must not contain "/"`},
	}
}

// ioRejectRows 是共享 IO 段拒绝分支的通用表（仅四个可执行变体使用）。
func ioRejectRows() []struct {
	name   string
	mutate func(io *IO)
	want   string
} {
	return []struct {
		name   string
		mutate func(io *IO)
		want   string
	}{
		{"empty input codec", func(io *IO) { io.InputCodec = "" }, "input codec id required"},
		{"empty output codec", func(io *IO) { io.OutputCodec = "" }, "output codec id required"},
		{"empty precondition id", func(io *IO) { io.Preconditions = []PredicateRef{{ID: ""}} }, "precondition predicate id required"},
		{"empty postcondition id", func(io *IO) { io.Postconditions = []PredicateRef{{ID: ""}} }, "postcondition predicate id required"},
		{"empty input binding source", func(io *IO) { io.Inputs = []InputBinding{{From: "", OutputField: "out", ToField: "in"}} }, "input binding source step required"},
	}
}

func TestNewLocalStep(t *testing.T) {
	h, io := validHeader(), validIO()
	got, err := NewLocalStep(h, io, LocalSpec{
		Handler: "engine.node.transform",
		Timeout: 3 * time.Second,
		Retry:   &RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
	})
	if err != nil {
		t.Fatalf("valid local step rejected: %v", err)
	}
	if got.ID != h.ID || got.NodeID != h.NodeID || got.DefinitionVersion != h.DefinitionVersion {
		t.Fatalf("header not materialized: %+v", got.Header)
	}
	if want := []StepID{"node.a", "node.b"}; len(got.Dependencies) != 2 || got.Dependencies[0] != want[0] || got.Dependencies[1] != want[1] {
		t.Fatalf("dependencies not deduped+sorted: %v", got.Dependencies)
	}
	if got.Handler != "engine.node.transform" || got.Timeout != 3*time.Second ||
		got.Retry == nil || got.Retry.MaxAttempts != 2 {
		t.Fatalf("payload not materialized: %+v", got)
	}
	// timeout 可选：0 表示不设超时，必须被接受。
	if _, err := NewLocalStep(h, io, LocalSpec{Handler: "engine.node.transform"}); err != nil {
		t.Fatalf("zero optional timeout rejected: %v", err)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewLocalStep(hh, validIO(), LocalSpec{Handler: "engine.node.transform"})
		wantErr(t, err, row.want)
	}
	for _, row := range ioRejectRows() {
		ii := validIO()
		row.mutate(&ii)
		_, err := NewLocalStep(validHeader(), ii, LocalSpec{Handler: "engine.node.transform"})
		wantErr(t, err, row.want)
	}
	rejects := []struct {
		name string
		spec LocalSpec
		want string
	}{
		{"empty handler", LocalSpec{}, "handler id required"},
		{"negative timeout", LocalSpec{Handler: "h", Timeout: -time.Second}, "timeout must be >= 0"},
		{"retry maxAttempts < 1", LocalSpec{Handler: "h", Retry: &RetryPolicy{MaxAttempts: 0}}, "retry maxAttempts must be >= 1"},
		{"retry negative backoff", LocalSpec{Handler: "h", Retry: &RetryPolicy{MaxAttempts: 2, Backoff: -time.Second}}, "retry backoff must be >= 0"},
	}
	for _, row := range rejects {
		_, err := NewLocalStep(validHeader(), validIO(), row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewDurableStep(t *testing.T) {
	spec := DurableSpec{
		Handler:     "engine.node.persist",
		Idempotency: IdempotencyDeterministicInput,
		Reconcile:   "reconcile.node.persist",
		Timeout:     30 * time.Second,
		Retry:       RetryPolicy{MaxAttempts: 3, Backoff: 2 * time.Second},
	}
	got, err := NewDurableStep(validHeader(), validIO(), spec)
	if err != nil {
		t.Fatalf("valid durable step rejected: %v", err)
	}
	if got.Idempotency != IdempotencyDeterministicInput || got.Reconcile != "reconcile.node.persist" ||
		got.Timeout != 30*time.Second || got.Retry.MaxAttempts != 3 {
		t.Fatalf("payload not materialized: %+v", got)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewDurableStep(hh, validIO(), spec)
		wantErr(t, err, row.want)
	}
	for _, row := range ioRejectRows() {
		ii := validIO()
		row.mutate(&ii)
		_, err := NewDurableStep(validHeader(), ii, spec)
		wantErr(t, err, row.want)
	}
	rejects := []struct {
		name string
		spec DurableSpec
		want string
	}{
		{"empty handler", DurableSpec{}, "handler id required"},
		{"zero idempotency strategy", DurableSpec{Handler: "h"}, "idempotency key strategy required"},
		{"bogus idempotency strategy", DurableSpec{Handler: "h", Idempotency: "实现麻烦"}, "idempotency key strategy required"},
		{"empty reconcile", DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped}, "reconcile id required"},
		{"retry maxAttempts < 1", DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r"}, "retry maxAttempts must be >= 1"},
		{"retry negative backoff", DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 2, Backoff: -1}}, "retry backoff must be >= 0"},
		{"zero timeout", DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 1}}, "positive timeout required"},
		{"negative timeout", DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 1}, Timeout: -time.Second}, "positive timeout required"},
	}
	for _, row := range rejects {
		_, err := NewDurableStep(validHeader(), validIO(), row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewHostActionStep(t *testing.T) {
	spec := HostActionSpec{
		Handler:   "engine.node.dispatch",
		Boundary:  BoundaryUserIOTransport,
		Operation: "op.node.transport",
		Timeout:   10 * time.Second,
	}
	got, err := NewHostActionStep(validHeader(), validIO(), spec)
	if err != nil {
		t.Fatalf("valid host action step rejected: %v", err)
	}
	if got.Boundary != BoundaryUserIOTransport || got.Operation != "op.node.transport" || got.Timeout != 10*time.Second {
		t.Fatalf("payload not materialized: %+v", got)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewHostActionStep(hh, validIO(), spec)
		wantErr(t, err, row.want)
	}
	for _, row := range ioRejectRows() {
		ii := validIO()
		row.mutate(&ii)
		_, err := NewHostActionStep(validHeader(), ii, spec)
		wantErr(t, err, row.want)
	}
	rejects := []struct {
		name string
		spec HostActionSpec
		want string
	}{
		{"empty handler", HostActionSpec{}, "handler id required"},
		{"zero boundary reason", HostActionSpec{Handler: "h"}, "hostBoundaryReason required"},
		// MISSING_ENGINE_ADAPTER 是 diagnostic-only marker，不是 runner reason，
		// 不得混入 hostBoundaryReason。
		{"diagnostic marker as boundary", HostActionSpec{Handler: "h", Boundary: "MISSING_ENGINE_ADAPTER"}, "hostBoundaryReason required"},
		{"empty operation", HostActionSpec{Handler: "h", Boundary: BoundaryExternalCapability}, "registered operation id required"},
		{"zero timeout", HostActionSpec{Handler: "h", Boundary: BoundaryAgentDispatchAPI, Operation: "op"}, "positive timeout required"},
	}
	for _, row := range rejects {
		_, err := NewHostActionStep(validHeader(), validIO(), row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewAgentStep(t *testing.T) {
	spec := AgentSpec{
		Handler: "engine.node.review",
		Reason:  ReasonIndependentReview,
		Timeout: time.Minute,
	}
	got, err := NewAgentStep(validHeader(), validIO(), spec)
	if err != nil {
		t.Fatalf("valid agent step rejected: %v", err)
	}
	if got.Reason != ReasonIndependentReview || got.Timeout != time.Minute {
		t.Fatalf("payload not materialized: %+v", got)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewAgentStep(hh, validIO(), spec)
		wantErr(t, err, row.want)
	}
	for _, row := range ioRejectRows() {
		ii := validIO()
		row.mutate(&ii)
		_, err := NewAgentStep(validHeader(), ii, spec)
		wantErr(t, err, row.want)
	}
	// "实现麻烦"不是合法理由：零值与任意其他字符串一律拒绝。
	noPost := validIO()
	noPost.Postconditions = nil
	rejects := []struct {
		name string
		h    Header
		io   IO
		spec AgentSpec
		want string
	}{
		{"empty handler", validHeader(), validIO(), AgentSpec{Reason: ReasonCreativeImplementation}, "handler id required"},
		{"zero reason", validHeader(), validIO(), AgentSpec{Handler: "engine.node.review"}, "nonProgrammableReason required"},
		{"bogus reason", validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: "实现麻烦"}, "nonProgrammableReason required"},
		{"no postcondition", validHeader(), noPost, spec, "postcondition predicate reference required"},
		{"retry maxAttempts < 1", validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: ReasonSemanticJudgment, Retry: &RetryPolicy{}}, "retry maxAttempts must be >= 1"},
		{"zero timeout", validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: ReasonSemanticJudgment}, "positive timeout required"},
	}
	for _, row := range rejects {
		_, err := NewAgentStep(row.h, row.io, row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewHumanAskStep(t *testing.T) {
	spec := HumanAskSpec{
		AskKind:        "decision",
		RequestSchema:  "schema.ask.node.request",
		ResponseSchema: "schema.ask.node.response",
		FreshnessTTL:   15 * time.Minute,
	}
	got, err := NewHumanAskStep(validHeader(), spec)
	if err != nil {
		t.Fatalf("valid human ask step rejected: %v", err)
	}
	if got.AskKind != "decision" || got.RequestSchema != "schema.ask.node.request" ||
		got.ResponseSchema != "schema.ask.node.response" || got.FreshnessTTL != 15*time.Minute {
		t.Fatalf("payload not materialized: %+v", got)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewHumanAskStep(hh, spec)
		wantErr(t, err, row.want)
	}
	rejects := []struct {
		name string
		spec HumanAskSpec
		want string
	}{
		{"empty ask kind", HumanAskSpec{}, "ask kind required"},
		{"empty request schema", HumanAskSpec{AskKind: "decision"}, "request schema id required"},
		{"empty response schema", HumanAskSpec{AskKind: "decision", RequestSchema: "s"}, "response schema id required"},
		{"zero freshness ttl", HumanAskSpec{AskKind: "decision", RequestSchema: "s", ResponseSchema: "s"}, "positive freshness ttl required"},
	}
	for _, row := range rejects {
		_, err := NewHumanAskStep(validHeader(), row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewParallelStep(t *testing.T) {
	spec := ParallelSpec{
		Children: []StepID{"node.c2", "node.c1", "node.c2"},
		Join:     JoinPolicy{JoinStep: "node.join", Mode: JoinAll},
		Failure:  FailurePolicy{Mode: FailFast, Escalate: FailureInvariantViolation},
	}
	got, err := NewParallelStep(validHeader(), spec)
	if err != nil {
		t.Fatalf("valid parallel step rejected: %v", err)
	}
	if want := []StepID{"node.c1", "node.c2"}; len(got.Children) != 2 || got.Children[0] != want[0] || got.Children[1] != want[1] {
		t.Fatalf("children not deduped+sorted: %v", got.Children)
	}
	if got.Join.Mode != JoinAll || got.Failure.Mode != FailFast || got.Failure.Escalate != FailureInvariantViolation {
		t.Fatalf("policies not materialized: %+v", got)
	}

	for _, row := range sharedRejectRows() {
		hh := validHeader()
		row.mutate(&hh)
		_, err := NewParallelStep(hh, spec)
		wantErr(t, err, row.want)
	}
	rejects := []struct {
		name string
		spec ParallelSpec
		want string
	}{
		{"no children", ParallelSpec{Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{"one child", ParallelSpec{Children: []StepID{"node.c1"}, Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{"dup children collapse below 2", ParallelSpec{Children: []StepID{"node.c1", "node.c1"}, Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{"empty child id", ParallelSpec{Children: []StepID{"node.c1", ""}, Join: spec.Join, Failure: spec.Failure}, "empty child id"},
		{"self child", ParallelSpec{Children: []StepID{"node.s1", "node.c1"}, Join: spec.Join, Failure: spec.Failure}, "references the step itself"},
		{"empty join step", ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{Mode: JoinAny}, Failure: spec.Failure}, "join step id required"},
		{"invalid join mode", ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{JoinStep: "node.join"}, Failure: spec.Failure}, "join mode required"},
		{"invalid failure mode", ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: spec.Join}, "failure mode required"},
		{"invalid escalate class", ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: spec.Join, Failure: FailurePolicy{Mode: WaitAll}}, "failure escalate class required"},
		{"join step is a child", ParallelSpec{Children: []StepID{"node.c1", "node.join"}, Join: spec.Join, Failure: spec.Failure}, "must not be a child"},
		// 封板后审计 H1：join 步 == 并行步自身使 join 依赖集合与 children
		// 自指重合，会在 compiler 层绕过 fan-out 覆盖检查，构造层直接拒绝。
		{"join step is the parallel step itself", ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{JoinStep: "node.s1", Mode: JoinAll}, Failure: spec.Failure}, "must be outside the parallel group"},
	}
	for _, row := range rejects {
		_, err := NewParallelStep(validHeader(), row.spec)
		wantErr(t, err, row.want)
	}
}

// TestDerivedAuthorityRunner 固化六变体 × 双维派生矩阵（§5.3）。步骤均经
// constructor 构造：派生值是变体类型的常量函数，与作者入参无关。
func TestDerivedAuthorityRunner(t *testing.T) {
	local, err := NewLocalStep(validHeader(), validIO(), LocalSpec{Handler: "engine.node.transform"})
	if err != nil {
		t.Fatalf("local: %v", err)
	}
	durable, err := NewDurableStep(validHeader(), validIO(), DurableSpec{
		Handler: "engine.node.persist", Idempotency: IdempotencyDeterministicInput,
		Reconcile: "reconcile.node.persist", Timeout: time.Second, Retry: RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("durable: %v", err)
	}
	host, err := NewHostActionStep(validHeader(), validIO(), HostActionSpec{
		Handler: "engine.node.dispatch", Boundary: BoundaryAgentDispatchAPI,
		Operation: "op.node.spawn", Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	agent, err := NewAgentStep(validHeader(), validIO(), AgentSpec{
		Handler: "engine.node.review", Reason: ReasonCreativeImplementation, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	ask, err := NewHumanAskStep(validHeader(), HumanAskSpec{
		AskKind: "decision", RequestSchema: "schema.ask.node.request",
		ResponseSchema: "schema.ask.node.response", FreshnessTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	parallel, err := NewParallelStep(validHeader(), ParallelSpec{
		Children: []StepID{"node.c1", "node.c2"},
		Join:     JoinPolicy{JoinStep: "node.join", Mode: JoinAll},
		Failure:  FailurePolicy{Mode: FailFast, Escalate: FailureBlockedBug},
	})
	if err != nil {
		t.Fatalf("parallel: %v", err)
	}

	// 表以 Step 接口值承载：六变体实现封闭接口这一事实由编译器静态检查。
	matrix := []struct {
		step      Step
		authority DecisionAuthority
		runner    RunnerKind
	}{
		{local, AuthorityEngine, RunnerEngineLocal},
		{durable, AuthorityEngine, RunnerDurableActivity},
		{host, AuthorityEngine, RunnerHostAdapter},
		{agent, AuthorityAgent, RunnerAgentWorker},
		{ask, AuthorityHuman, RunnerHostAdapter},
		{parallel, AuthorityEngine, RunnerEngineLocal},
	}
	for _, row := range matrix {
		if got := row.step.Authority(); got != row.authority {
			t.Errorf("%T: Authority() = %q, want %q", row.step, got, row.authority)
		}
		if got := row.step.RunnerKind(); got != row.runner {
			t.Errorf("%T: RunnerKind() = %q, want %q", row.step, got, row.runner)
		}
	}
}
