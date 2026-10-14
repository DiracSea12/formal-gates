package authoring

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// 本文件验证 constructor 校验与六变体派生的 Authority/RunnerKind。

// validHeader 返回合法公共头。依赖故意乱序且含重复，用于同时验证归一化。
func validHeader() Header {
	return Header{
		ID:                "node.s1",
		NodeID:            "node",
		Dependencies:      []StepID{"node.b", "node.a", "node.b"},
		DefinitionVersion: "wf-v1",
	}
}

// validIO 返回合法共享 IO 段（含 pre/postcondition 引用与 typed codec）。
func validIO() IO {
	return IO{
		InputCodec:     "codec.node.in",
		OutputCodec:    "codec.node.out",
		Preconditions:  []PredicateRef{{ID: "pred.node.pre"}},
		Postconditions: []PredicateRef{{ID: "pred.node.post"}},
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
	mutate func(h *Header)
	want   string
} {
	return []struct {
		mutate func(h *Header)
		want   string
	}{
		{func(h *Header) { h.ID = "" }, "step id is empty"},
		{func(h *Header) { h.NodeID = "" }, "node id is empty"},
		{func(h *Header) { h.DefinitionVersion = "" }, "definition version is empty"},
		{func(h *Header) { h.Dependencies = []StepID{"node.a", ""} }, "empty dependency id"},
		{func(h *Header) { h.Dependencies = []StepID{"node.s1"} }, "references the step itself"},
		// ID/NodeID 段内 "/" 或 "\" 会使 canonical task key
		// 坍缩（{n,a/b} 与 {n/a,b} 同为 n/a/b），构造层直接拒绝；段内禁用
		// 字符集与 runtime.NewTaskKey 两层对齐。
		{func(h *Header) { h.ID = "node/s1" }, "must not contain '/' or '\\'"},
		{func(h *Header) { h.NodeID = "node/sub" }, "must not contain '/' or '\\'"},
		{func(h *Header) { h.ID = "node\\s1" }, "must not contain '/' or '\\'"},
		{func(h *Header) { h.NodeID = "node\\sub" }, "must not contain '/' or '\\'"},
	}
}

// ioRejectRows 是共享 IO 段拒绝分支的通用表（仅四个可执行变体使用）。
func ioRejectRows() []struct {
	mutate func(io *IO)
	want   string
} {
	return []struct {
		mutate func(io *IO)
		want   string
	}{
		{func(io *IO) { io.InputCodec = "" }, "input codec id required"},
		{func(io *IO) { io.OutputCodec = "" }, "output codec id required"},
		{func(io *IO) { io.Preconditions = []PredicateRef{{ID: ""}} }, "precondition predicate id required"},
		{func(io *IO) { io.Postconditions = []PredicateRef{{ID: ""}} }, "postcondition predicate id required"},
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
	if want := []StepID{"node.b", "node.a", "node.b"}; !slices.Equal(h.Dependencies, want) {
		t.Fatalf("caller dependencies mutated: %v", h.Dependencies)
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
		spec LocalSpec
		want string
	}{
		{LocalSpec{}, "handler id required"},
		{LocalSpec{Handler: "h", Timeout: -time.Second}, "timeout must be >= 0"},
		{LocalSpec{Handler: "h", Retry: &RetryPolicy{MaxAttempts: 0}}, "retry maxAttempts must be >= 1"},
		{LocalSpec{Handler: "h", Retry: &RetryPolicy{MaxAttempts: 2, Backoff: -time.Second}}, "retry backoff must be >= 0"},
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
		spec DurableSpec
		want string
	}{
		{DurableSpec{}, "handler id required"},
		{DurableSpec{Handler: "h"}, "idempotency key strategy required"},
		{DurableSpec{Handler: "h", Idempotency: "实现麻烦"}, "idempotency key strategy required"},
		{DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped}, "reconcile id required"},
		{DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r"}, "retry maxAttempts must be >= 1"},
		{DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 2, Backoff: -1}}, "retry backoff must be >= 0"},
		{DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 1}}, "positive timeout required"},
		{DurableSpec{Handler: "h", Idempotency: IdempotencyTaskKeyScoped, Reconcile: "r", Retry: RetryPolicy{MaxAttempts: 1}, Timeout: -time.Second}, "positive timeout required"},
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
		Schema:    "schema.node.transport",
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
		spec HostActionSpec
		want string
	}{
		{HostActionSpec{}, "handler id required"},
		{HostActionSpec{Handler: "h"}, "hostBoundaryReason required"},
		// MISSING_ENGINE_ADAPTER 是 diagnostic-only marker，不是 runner reason，
		// 不得混入 hostBoundaryReason。
		{HostActionSpec{Handler: "h", Boundary: "MISSING_ENGINE_ADAPTER"}, "hostBoundaryReason required"},
		{HostActionSpec{Handler: "h", Boundary: BoundaryExternalCapability}, "registered operation id required"},
		{HostActionSpec{Handler: "h", Boundary: BoundaryAgentDispatchAPI, Operation: "op", Timeout: -time.Second}, "timeout must be >= 0"},
	}
	for _, row := range rejects {
		_, err := NewHostActionStep(validHeader(), validIO(), row.spec)
		wantErr(t, err, row.want)
	}
}

func TestNewAgentStep(t *testing.T) {
	spec := AgentSpec{
		Handler:      "engine.node.review",
		Reason:       ReasonIndependentReview,
		ReviewSeries: ReviewSeriesProductReview,
		Timeout:      time.Minute,
	}
	got, err := NewAgentStep(validHeader(), validIO(), spec)
	if err != nil {
		t.Fatalf("valid agent step rejected: %v", err)
	}
	if got.Reason != ReasonIndependentReview || got.Timeout != time.Minute || got.ReviewSeries != ReviewSeriesProductReview {
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
		h    Header
		io   IO
		spec AgentSpec
		want string
	}{
		{validHeader(), validIO(), AgentSpec{Reason: ReasonCreativeImplementation}, "handler id required"},
		{validHeader(), validIO(), AgentSpec{Handler: "engine.node.review"}, "nonProgrammableReason required"},
		{validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: "实现麻烦"}, "nonProgrammableReason required"},
		{validHeader(), noPost, spec, "postcondition predicate reference required"},
		{validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: ReasonSemanticJudgment, Retry: &RetryPolicy{}}, "retry maxAttempts must be >= 1"},
		{validHeader(), validIO(), AgentSpec{Handler: "engine.node.review", Reason: ReasonSemanticJudgment, Timeout: -time.Second}, "timeout must be >= 0"},
	}
	for _, row := range rejects {
		_, err := NewAgentStep(row.h, row.io, row.spec)
		wantErr(t, err, row.want)
	}

	// 系列标记：封闭三值、INDEPENDENT_REVIEW 步必填、只随
	// INDEPENDENT_REVIEW 携带。
	unmarked := spec
	unmarked.ReviewSeries = ""
	_, err = NewAgentStep(validHeader(), validIO(), unmarked)
	wantErr(t, err, "must declare its review series")
	other := spec
	other.ReviewSeries = ReviewSeriesUnaffiliated
	if got, err = NewAgentStep(validHeader(), validIO(), other); err != nil || got.ReviewSeries != ReviewSeriesUnaffiliated {
		t.Fatalf("an unaffiliated independent-review step rejected: %v %+v", err, got)
	}
	nonReview := spec
	nonReview.Reason = ReasonSemanticJudgment
	nonReview.ReviewSeries = ReviewSeriesStartReadiness
	_, err = NewAgentStep(validHeader(), validIO(), nonReview)
	wantErr(t, err, "requires the INDEPENDENT_REVIEW reason")
	bogus := spec
	bogus.ReviewSeries = "qa-review"
	_, err = NewAgentStep(validHeader(), validIO(), bogus)
	wantErr(t, err, "review series")
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
		spec HumanAskSpec
		want string
	}{
		{HumanAskSpec{}, "ask kind required"},
		{HumanAskSpec{AskKind: "decision"}, "request schema id required"},
		{HumanAskSpec{AskKind: "decision", RequestSchema: "s"}, "response schema id required"},
		{HumanAskSpec{AskKind: "decision", RequestSchema: "s", ResponseSchema: "s"}, "positive freshness ttl required"},
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
	if want := []StepID{"node.c2", "node.c1", "node.c2"}; !slices.Equal(spec.Children, want) {
		t.Fatalf("caller children mutated: %v", spec.Children)
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
		spec ParallelSpec
		want string
	}{
		{ParallelSpec{Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{ParallelSpec{Children: []StepID{"node.c1"}, Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.c1"}, Join: spec.Join, Failure: spec.Failure}, "at least 2 children required"},
		{ParallelSpec{Children: []StepID{"node.c1", ""}, Join: spec.Join, Failure: spec.Failure}, "empty child id"},
		{ParallelSpec{Children: []StepID{"node.s1", "node.c1"}, Join: spec.Join, Failure: spec.Failure}, "references the step itself"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{Mode: JoinAny}, Failure: spec.Failure}, "join step id required"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{JoinStep: "node.join"}, Failure: spec.Failure}, "join mode required"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: spec.Join}, "failure mode required"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: spec.Join, Failure: FailurePolicy{Mode: WaitAll}}, "failure escalate class required"},
		{ParallelSpec{Children: []StepID{"node.c1", "node.join"}, Join: spec.Join, Failure: spec.Failure}, "must not be a child"},
		// join 步 == 并行步自身使 join 依赖集合与 children
		// 自指重合，会在 compiler 层绕过 fan-out 覆盖检查，构造层直接拒绝。
		{ParallelSpec{Children: []StepID{"node.c1", "node.c2"}, Join: JoinPolicy{JoinStep: "node.s1", Mode: JoinAll}, Failure: spec.Failure}, "must be outside the parallel group"},
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
		Operation: "op.node.spawn", Schema: "schema.node.spawn", Timeout: time.Second,
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
