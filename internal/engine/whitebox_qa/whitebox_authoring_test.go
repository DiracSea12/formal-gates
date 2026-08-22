package whitebox_qa

import (
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
)

// 用例：六种封闭变体经 constructor 正常构造后，authority/runner 是变体类型
// 的常量派生（作者无填写入口），依赖与 children 被去重排序归一化且不改写
// 调用方入参——assembly 顺序不得泄漏进制品。
func TestVariantsDeriveAuthorityRunnerAndNormalizeRefs(t *testing.T) {
	cases := []struct {
		name      string
		step      authoring.Step
		authority authoring.DecisionAuthority
		runner    authoring.RunnerKind
	}{
		{"local", fxParse(t), authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		{"durable", fxPersist(t), authoring.AuthorityEngine, authoring.RunnerDurableActivity},
		{"host_action", fxDispatch(t), authoring.AuthorityEngine, authoring.RunnerHostAdapter},
		{"agent", fxReview(t), authoring.AuthorityAgent, authoring.RunnerAgentWorker},
		{"human_ask", fxAsk(t), authoring.AuthorityHuman, authoring.RunnerHostAdapter},
		{"parallel", fxFan(t), authoring.AuthorityEngine, authoring.RunnerEngineLocal},
	}
	for _, tc := range cases {
		if got := tc.step.Authority(); got != tc.authority {
			t.Errorf("%s: Authority() = %s, want %s", tc.name, got, tc.authority)
		}
		if got := tc.step.RunnerKind(); got != tc.runner {
			t.Errorf("%s: RunnerKind() = %s, want %s", tc.name, got, tc.runner)
		}
	}

	// 依赖去重 + 排序，调用方切片原样保留。
	deps := []authoring.StepID{"s.b", "s.a", "s.b"}
	local, err := authoring.NewLocalStep(fxHeader("s.x", "n0", "s.b", "s.a", "s.b"), fxIO("s.a", "s.b"),
		authoring.LocalSpec{Handler: "h.s.parse"})
	if err != nil {
		t.Fatalf("NewLocalStep: %v", err)
	}
	if want := []authoring.StepID{"s.a", "s.b"}; !reflect.DeepEqual(local.Header.Dependencies, want) {
		t.Errorf("normalized deps = %v, want %v", local.Header.Dependencies, want)
	}
	if want := []authoring.StepID{"s.b", "s.a", "s.b"}; !reflect.DeepEqual(deps, want) {
		t.Errorf("caller deps slice mutated: %v", deps)
	}

	// parallel children 同样去重 + 排序。
	children := []authoring.StepID{"c.two", "c.one", "c.two"}
	par, err := authoring.NewParallelStep(fxHeader("s.fanx", "n0", "s.parse"),
		authoring.ParallelSpec{
			Children: children,
			Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	if err != nil {
		t.Fatalf("NewParallelStep: %v", err)
	}
	if want := []authoring.StepID{"c.one", "c.two"}; !reflect.DeepEqual(par.Children, want) {
		t.Errorf("normalized children = %v, want %v", par.Children, want)
	}
	if want := []authoring.StepID{"c.two", "c.one", "c.two"}; !reflect.DeepEqual(children, want) {
		t.Errorf("caller children slice mutated: %v", children)
	}
}

// 用例：constructor 对每个变体的必填枚举/payload 义务强制拒绝（八类拒绝中
// 副作用无幂等/reconcile、HOST 缺边界、AGENT 缺理由、人工等待缺 schema、
// 并行组缺 join/failure 的第一拦截层）。
func TestConstructorsRejectMissingVariantObligations(t *testing.T) {
	validIO := fxIO("s.parse")
	postIO := fxIO("s.parse")
	postIO.Postconditions = []authoring.PredicateRef{{ID: "pred.done"}}
	rows := []struct {
		name   string
		build  func() error
		substr string
	}{
		{"local: handler missing", func() error {
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), fxIO(), authoring.LocalSpec{})
			return err
		}, "handler id required"},
		{"local: negative timeout", func() error {
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse", Timeout: -time.Second})
			return err
		}, "timeout must be >= 0"},
		{"local: retry maxAttempts 0", func() error {
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse", Retry: &authoring.RetryPolicy{}})
			return err
		}, "maxAttempts must be >= 1"},
		{"local: negative backoff", func() error {
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse", Retry: &authoring.RetryPolicy{MaxAttempts: 2, Backoff: -1}})
			return err
		}, "backoff must be >= 0"},
		{"durable: idempotency missing", func() error {
			_, err := authoring.NewDurableStep(fxHeader("s.x", "n0", "s.parse"), validIO, authoring.DurableSpec{
				Handler: "h.s.persist", Reconcile: "r.persist", Timeout: time.Second,
				Retry: authoring.RetryPolicy{MaxAttempts: 1},
			})
			return err
		}, "idempotency key strategy required"},
		{"durable: idempotency bogus enum", func() error {
			_, err := authoring.NewDurableStep(fxHeader("s.x", "n0", "s.parse"), validIO, authoring.DurableSpec{
				Handler: "h.s.persist", Idempotency: "WHENEVER", Reconcile: "r.persist",
				Timeout: time.Second, Retry: authoring.RetryPolicy{MaxAttempts: 1},
			})
			return err
		}, "idempotency key strategy required"},
		{"durable: reconcile missing", func() error {
			_, err := authoring.NewDurableStep(fxHeader("s.x", "n0", "s.parse"), validIO, authoring.DurableSpec{
				Handler: "h.s.persist", Idempotency: authoring.IdempotencyDeterministicInput,
				Timeout: time.Second, Retry: authoring.RetryPolicy{MaxAttempts: 1},
			})
			return err
		}, "reconcile id required"},
		{"durable: timeout zero", func() error {
			_, err := authoring.NewDurableStep(fxHeader("s.x", "n0", "s.parse"), validIO, authoring.DurableSpec{
				Handler: "h.s.persist", Idempotency: authoring.IdempotencyDeterministicInput,
				Reconcile: "r.persist", Retry: authoring.RetryPolicy{MaxAttempts: 1},
			})
			return err
		}, "positive timeout required"},
		{"durable: retry zero attempts", func() error {
			_, err := authoring.NewDurableStep(fxHeader("s.x", "n0", "s.parse"), validIO, authoring.DurableSpec{
				Handler: "h.s.persist", Idempotency: authoring.IdempotencyDeterministicInput,
				Reconcile: "r.persist", Timeout: time.Second, Retry: authoring.RetryPolicy{},
			})
			return err
		}, "maxAttempts must be >= 1"},
		{"host: boundary missing", func() error {
			_, err := authoring.NewHostActionStep(fxHeader("s.x", "n1", "s.parse"), validIO, authoring.HostActionSpec{
				Handler: "h.s.dispatch", Operation: "op.x", Timeout: time.Second,
			})
			return err
		}, "hostBoundaryReason required"},
		{"host: boundary bogus enum", func() error {
			_, err := authoring.NewHostActionStep(fxHeader("s.x", "n1", "s.parse"), validIO, authoring.HostActionSpec{
				Handler: "h.s.dispatch", Boundary: "BECAUSE", Operation: "op.x", Timeout: time.Second,
			})
			return err
		}, "hostBoundaryReason required"},
		{"host: operation missing", func() error {
			_, err := authoring.NewHostActionStep(fxHeader("s.x", "n1", "s.parse"), validIO, authoring.HostActionSpec{
				Handler: "h.s.dispatch", Boundary: authoring.BoundaryUserIOTransport, Timeout: time.Second,
			})
			return err
		}, "operation id required"},
		{"host: timeout zero", func() error {
			_, err := authoring.NewHostActionStep(fxHeader("s.x", "n1", "s.parse"), validIO, authoring.HostActionSpec{
				Handler: "h.s.dispatch", Boundary: authoring.BoundaryUserIOTransport, Operation: "op.x",
			})
			return err
		}, "positive timeout required"},
		{"agent: reason missing", func() error {
			_, err := authoring.NewAgentStep(fxHeader("s.x", "n1", "s.parse"), postIO, authoring.AgentSpec{
				Handler: "h.s.review", Timeout: time.Minute,
			})
			return err
		}, "nonProgrammableReason required"},
		{"agent: reason bogus enum", func() error {
			_, err := authoring.NewAgentStep(fxHeader("s.x", "n1", "s.parse"), postIO, authoring.AgentSpec{
				Handler: "h.s.review", Reason: "TOO_LAZY", Timeout: time.Minute,
			})
			return err
		}, "nonProgrammableReason required"},
		{"agent: postcondition missing", func() error {
			_, err := authoring.NewAgentStep(fxHeader("s.x", "n1", "s.parse"), validIO, authoring.AgentSpec{
				Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute,
			})
			return err
		}, "postcondition predicate reference required"},
		{"agent: timeout zero", func() error {
			_, err := authoring.NewAgentStep(fxHeader("s.x", "n1", "s.parse"), postIO, authoring.AgentSpec{
				Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment,
			})
			return err
		}, "positive timeout required"},
		{"human: ask kind missing", func() error {
			_, err := authoring.NewHumanAskStep(fxHeader("s.x", "n1", "s.parse"), authoring.HumanAskSpec{
				RequestSchema: "s.req", ResponseSchema: "s.resp", FreshnessTTL: time.Minute,
			})
			return err
		}, "ask kind required"},
		{"human: request schema missing", func() error {
			_, err := authoring.NewHumanAskStep(fxHeader("s.x", "n1", "s.parse"), authoring.HumanAskSpec{
				AskKind: "confirm", ResponseSchema: "s.resp", FreshnessTTL: time.Minute,
			})
			return err
		}, "request schema id required"},
		{"human: response schema missing", func() error {
			_, err := authoring.NewHumanAskStep(fxHeader("s.x", "n1", "s.parse"), authoring.HumanAskSpec{
				AskKind: "confirm", RequestSchema: "s.req", FreshnessTTL: time.Minute,
			})
			return err
		}, "response schema id required"},
		{"human: ttl zero", func() error {
			_, err := authoring.NewHumanAskStep(fxHeader("s.x", "n1", "s.parse"), authoring.HumanAskSpec{
				AskKind: "confirm", RequestSchema: "s.req", ResponseSchema: "s.resp",
			})
			return err
		}, "positive freshness ttl required"},
		{"parallel: single child after dedup", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.one"},
				Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "at least 2 children required"},
		{"parallel: join step missing", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.two"},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "join step id required"},
		{"parallel: join mode missing", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.two"},
				Join:     authoring.JoinPolicy{JoinStep: "j"},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "join mode required"},
		{"parallel: failure mode missing", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.two"},
				Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "failure mode required"},
		{"parallel: escalate missing", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.two"},
				Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast},
			})
			return err
		}, "escalate class required"},
		{"parallel: join is a child", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n2", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "c.two"},
				Join:     authoring.JoinPolicy{JoinStep: "c.one", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "must not be a child"},
	}
	for _, row := range rows {
		if err := row.build(); err == nil {
			t.Errorf("%s: expected constructor rejection, got nil", row.name)
		} else {
			wantErrContaining(t, err, row.substr)
		}
	}
}

// 用例：公共头与共享 IO 段的必填规则（空 ID/节点/版本、空依赖/自引用、
// 无类型 codec、空 predicate 引用、空 binding 来源）在 constructor 层拒绝
// ——"未绑定 definition version"与"无类型 I/O"的第一拦截层。
func TestConstructorsRejectHeaderAndIOViolations(t *testing.T) {
	rows := []struct {
		name   string
		build  func() error
		substr string
	}{
		{"empty step id", func() error {
			h := fxHeader("", "n0")
			_, err := authoring.NewLocalStep(h, fxIO(), authoring.LocalSpec{Handler: "h"})
			return err
		}, "step id is empty"},
		{"empty node id", func() error {
			h := fxHeader("s.x", "")
			_, err := authoring.NewLocalStep(h, fxIO(), authoring.LocalSpec{Handler: "h"})
			return err
		}, "node id is empty"},
		{"empty definition version", func() error {
			h := fxHeader("s.x", "n0")
			h.DefinitionVersion = ""
			_, err := authoring.NewLocalStep(h, fxIO(), authoring.LocalSpec{Handler: "h"})
			return err
		}, "definition version is empty"},
		{"empty dependency id", func() error {
			h := fxHeader("s.x", "n0", "")
			_, err := authoring.NewLocalStep(h, fxIO(), authoring.LocalSpec{Handler: "h"})
			return err
		}, "empty dependency id"},
		{"self dependency", func() error {
			h := fxHeader("s.x", "n0", "s.x")
			_, err := authoring.NewLocalStep(h, fxIO("s.x"), authoring.LocalSpec{Handler: "h"})
			return err
		}, "references the step itself"},
		{"parallel: empty child id", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n0", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", ""},
				Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "empty child id"},
		{"parallel: child references itself", func() error {
			_, err := authoring.NewParallelStep(fxHeader("s.x", "n0", "s.parse"), authoring.ParallelSpec{
				Children: []authoring.StepID{"c.one", "s.x"},
				Join:     authoring.JoinPolicy{JoinStep: "j", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
			return err
		}, "references the step itself"},
		{"input codec missing", func() error {
			io := fxIO()
			io.InputCodec = ""
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), io, authoring.LocalSpec{Handler: "h"})
			return err
		}, "input codec id required"},
		{"output codec missing", func() error {
			io := fxIO()
			io.OutputCodec = ""
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), io, authoring.LocalSpec{Handler: "h"})
			return err
		}, "output codec id required"},
		{"precondition predicate id missing", func() error {
			io := fxIO()
			io.Preconditions = []authoring.PredicateRef{{ID: ""}}
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), io, authoring.LocalSpec{Handler: "h"})
			return err
		}, "precondition predicate id required"},
		{"postcondition predicate id missing", func() error {
			io := fxIO()
			io.Postconditions = []authoring.PredicateRef{{ID: ""}}
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), io, authoring.LocalSpec{Handler: "h"})
			return err
		}, "postcondition predicate id required"},
		{"input binding source missing", func() error {
			io := fxIO()
			io.Inputs = []authoring.InputBinding{{From: "", OutputField: "o", ToField: "i"}}
			_, err := authoring.NewLocalStep(fxHeader("s.x", "n0"), io, authoring.LocalSpec{Handler: "h"})
			return err
		}, "input binding source step required"},
	}
	for _, row := range rows {
		if err := row.build(); err == nil {
			t.Errorf("%s: expected constructor rejection, got nil", row.name)
		} else {
			wantErrContaining(t, err, row.substr)
		}
	}
}
