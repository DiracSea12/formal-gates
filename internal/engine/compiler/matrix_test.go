package compiler

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
)

// 本文件覆盖八类非法定义拒绝的 compiler 层（enforcement matrix，
// final-implementation-draft §3.2）与 MISSING_ENGINE_ADAPTER 双路由：
//
// | 非法定义                     | compiler 层拦截                          |
// | 不可达/循环                  | 图校验主责（graph_test.go）             |
// | 无类型 I/O                   | 二次防线（本文件：codec 必填）           |
// | 自然语言-only pre/postcond.  | 结构性消除 + 二次防线（空 predicate ID） |
// | 副作用无幂等/reconcile       | 二次防线（本文件）                       |
// | human 无 request/schema      | 二次防线（本文件）                       |
// | 并行组无 join/failure policy | 图校验主责（覆盖/锚点）+ 二次防线       |
// | AGENT/HOST 缺合法 reason     | 二次防线（本文件）                       |
// | 未绑定 definition version    | compiler 主责（graph_test.go 版本行）   |
// | registry ID 不完备           | compiler 主责（本文件路由测试）         |
//
// 二次防线行全部用绕过 constructor 的原始结构体构造：正常 authoring API 下
// 这些非法状态不可构造，compiler 对零值/非法枚举一律拒绝。

func rawHeader(id string) authoring.Header {
	return authoring.Header{ID: authoring.StepID(id), NodeID: goldenEntry, DefinitionVersion: goldenVersion}
}

// TestCompilerSecondLineRejects：constructor 主拦项的 compiled IR 二次防线。
func TestCompilerSecondLineRejects(t *testing.T) {
	rows := []struct {
		name  string
		steps []authoring.Step
		want  string
	}{
		{"untyped IO: no input codec", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"), IO: authoring.IO{OutputCodec: "codec.any.out"},
			Handler: "engine.entry.parse"}}, "input codec id required (untyped IO)"},
		{"untyped IO: no output codec", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"), IO: authoring.IO{InputCodec: "codec.any.in"},
			Handler: "engine.entry.parse"}}, "output codec id required (untyped IO)"},
		{"predicate ref without id", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"),
			IO: authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
				Preconditions: []authoring.PredicateRef{{ID: ""}}},
			Handler: "engine.entry.parse"}}, "precondition predicate id required"},
		{"local without handler", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"), IO: ioWith()}}, "local step \"entry.s\": handler id required"},
		{"local negative timeout", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.entry.parse", Timeout: -time.Second}},
			"timeout must be >= 0"},
		{"local invalid retry", []authoring.Step{authoring.LocalStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.entry.parse",
			Retry: &authoring.RetryPolicy{MaxAttempts: 0, Backoff: -time.Second}}},
			"retry maxAttempts must be >= 1"},
		{"durable without idempotency", []authoring.Step{authoring.DurableStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.entry.persist",
			Reconcile: "reconcile.entry.persist", Timeout: time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 1}}},
			"idempotency key strategy required"},
		{"durable without reconcile", []authoring.Step{authoring.DurableStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.entry.persist",
			Idempotency: authoring.IdempotencyTaskKeyScoped, Timeout: time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 1}}},
			"durable step \"entry.s\": reconcile id required"},
		{"durable zero retry", []authoring.Step{authoring.DurableStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.entry.persist",
			Idempotency: authoring.IdempotencyTaskKeyScoped, Reconcile: "reconcile.entry.persist",
			Timeout: time.Second}},
			"retry maxAttempts must be >= 1"},
		{"host without boundary", []authoring.Step{authoring.HostActionStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.fan.transport",
			Operation: "op.fan.transport", Timeout: time.Second}},
			"hostBoundaryReason required"},
		{"host without operation", []authoring.Step{authoring.HostActionStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.fan.transport",
			Boundary: authoring.BoundaryExternalCapability, Timeout: time.Second}},
			"registered operation id required"},
		{"agent without reason", []authoring.Step{authoring.AgentStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.review.worker",
			Timeout: time.Second}},
			"nonProgrammableReason required"},
		{"agent zero timeout", []authoring.Step{authoring.AgentStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.review.worker",
			Reason: authoring.ReasonSemanticJudgment}},
			"positive timeout required"},
		// worker result 合同的二次防线：绕过 constructor 的原始结构体缺
		// postconditions 必拒（constructor 主拦项复核，封板后补漏）。
		{"agent without postconditions", []authoring.Step{authoring.AgentStep{
			Header: rawHeader("entry.s"), IO: ioWith(), Handler: "engine.review.worker",
			Reason: authoring.ReasonSemanticJudgment, Timeout: time.Second}},
			"postcondition predicate reference required (worker result contract)"},
		{"human without request schema", []authoring.Step{authoring.HumanAskStep{
			Header: rawHeader("entry.s"), AskKind: "decision",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: time.Minute}},
			"request schema id required"},
		{"human without response schema", []authoring.Step{authoring.HumanAskStep{
			Header: rawHeader("entry.s"), AskKind: "decision",
			RequestSchema: "schema.ask.decision.request", FreshnessTTL: time.Minute}},
			"response schema id required"},
		{"human zero freshness", []authoring.Step{authoring.HumanAskStep{
			Header: rawHeader("entry.s"), AskKind: "decision",
			RequestSchema: "schema.ask.decision.request", ResponseSchema: "schema.ask.decision.response"}},
			"positive freshness ttl required"},
		// 并行组 join/failure 二次防线：基座步骤见 parallelRows。
		{"parallel invalid join mode", parallelRows(func(j *authoring.JoinPolicy, f *authoring.FailurePolicy, joinDeps, children *[]string) {
			j.Mode = ""
		}), "join policy (joinStep + mode ALL|ANY) required"},
		{"parallel invalid failure policy", parallelRows(func(j *authoring.JoinPolicy, f *authoring.FailurePolicy, joinDeps, children *[]string) {
			f.Mode, f.Escalate = "", ""
		}), "failure policy (mode FAIL_FAST|WAIT_ALL + escalate class) required"},
		{"parallel single child", parallelRows(func(j *authoring.JoinPolicy, f *authoring.FailurePolicy, joinDeps, children *[]string) {
			// join 覆盖与 children 保持一致（覆盖检查通过），命中 children<2。
			*children = []string{"fan.c1"}
			*joinDeps = []string{"fan.c1"}
		}), "at least 2 children required"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			_, err := Compile(defWith(row.steps...), goldenRegistry(t))
			wantErr(t, err, row.want)
			var ce *Error
			if !errors.As(err, &ce) || ce.Class != authoring.FailureInvariantViolation {
				t.Fatalf("second-line reject must classify INVARIANT_VIOLATION: %v", err)
			}
		})
	}
}

// parallelRows 构建并行二次防线基座：root → split(children) → join(joinDeps)。
// mutate 调整 join/failure 策略、join 依赖集合与 children 以命中不同分支。
func parallelRows(mutate func(j *authoring.JoinPolicy, f *authoring.FailurePolicy, joinDeps, children *[]string)) []authoring.Step {
	root := mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse")
	join := authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll}
	failure := authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation}
	joinDeps := []string{"fan.c1", "fan.c2"}
	children := []string{"fan.c1", "fan.c2"}
	mutate(&join, &failure, &joinDeps, &children)
	ids := make([]authoring.StepID, 0, len(children))
	kids := make([]authoring.Step, 0, len(children))
	for _, c := range children {
		ids = append(ids, authoring.StepID(c))
		kids = append(kids, mustLocal(header(c, "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice"))
	}
	split := authoring.ParallelStep{
		Header:   header("fan.split", "fan", "entry.root"),
		Children: ids,
		Join:     join,
		Failure:  failure,
	}
	joinStep := mustLocal(header("fan.join", "fan", joinDeps...), ioWith(joinDeps...), "engine.fan.join")
	steps := append([]authoring.Step{root, split}, kids...)
	return append(steps, joinStep)
}

// TestMissingEngineAdapterRouting：同一未完整实现的定义（operation 未注册）
// 在两种模式下的路由——正常 compile 以 BLOCKED_BUG 拒绝签发；diagnostic-only
// 可加载并输出诊断，产物带 marker（不得进入 executable plan）。
func TestMissingEngineAdapterRouting(t *testing.T) {
	reg := goldenRegistry(t, "op.fan.transport") // 故意不注册该 operation

	cd, err := Compile(goldenDefinition(), reg)
	if cd != nil {
		t.Fatal("normal compile must not issue an artifact for incomplete definitions")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("compile error is not *compiler.Error: %T", err)
	}
	if ce.Class != authoring.FailureBlockedBug {
		t.Fatalf("class = %s, want BLOCKED_BUG", ce.Class)
	}
	if !strings.Contains(err.Error(), "MISSING_ENGINE_ADAPTER") {
		t.Fatalf("error must name the MISSING_ENGINE_ADAPTER marker: %q", err.Error())
	}
	// 封板后审计 QA-B13：正常模式的拒绝消息必须携带 diagnostic compile
	// 提示（resolveID 直返路径可达，不再是死代码拼接）。
	if !strings.Contains(err.Error(), "use diagnostic compile") {
		t.Fatalf("normal-mode BLOCKED_BUG error must carry the diagnostic-compile hint: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "not registered (closed world)") {
		t.Fatalf("normal-mode BLOCKED_BUG error must keep the closed-world wording: %q", err.Error())
	}

	dr, err := CompileDiagnostic(goldenDefinition(), reg)
	if err != nil {
		t.Fatalf("diagnostic compile must load incomplete definitions: %v", err)
	}
	if !dr.Definition.MissingEngineAdapter {
		t.Fatal("diagnostic artifact must carry the MISSING_ENGINE_ADAPTER marker")
	}
	want := Diagnostic{Step: "fan.transport", Ref: "op.fan.transport", Want: KindOperation}
	if len(dr.Diagnostics) != 1 || dr.Diagnostics[0] != want {
		t.Fatalf("diagnostics = %+v, want [%+v]", dr.Diagnostics, want)
	}

	// 多处未注册：每条引用逐一记为诊断（缺失 handler + operation）。
	reg2 := goldenRegistry(t, "op.fan.transport", "engine.fan.transport")
	dr2, err := CompileDiagnostic(goldenDefinition(), reg2)
	if err != nil {
		t.Fatalf("diagnostic compile: %v", err)
	}
	if len(dr2.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want 2 entries", dr2.Diagnostics)
	}

	// 完整 registry：正常与 diagnostic 产物一致且无 marker。
	full := goldenRegistry(t)
	cdFull, err := Compile(goldenDefinition(), full)
	if err != nil {
		t.Fatalf("full compile: %v", err)
	}
	drFull, err := CompileDiagnostic(goldenDefinition(), full)
	if err != nil {
		t.Fatalf("full diagnostic compile: %v", err)
	}
	if cdFull.MissingEngineAdapter || drFull.Definition.MissingEngineAdapter || len(drFull.Diagnostics) != 0 {
		t.Fatalf("fully registered definitions must not carry markers: %+v", drFull)
	}
	if !reflect.DeepEqual(cdFull, drFull.Definition) {
		t.Fatal("diagnostic compile of a complete definition must equal normal compile")
	}
}

// TestKindAndRunnerMismatchBothModes：ID 存在但槽位错用（kind 不匹配）或
// handler 错绑执行边界（runner 不匹配）是定义错误而非"未实现"，两种模式下
// 都硬拒绝，不得降级为 MISSING_ENGINE_ADAPTER 诊断。
func TestKindAndRunnerMismatchBothModes(t *testing.T) {
	for _, row := range []struct {
		name string
		reg  func(t *testing.T) *Registry
		want string
	}{
		{"kind mismatch", func(t *testing.T) *Registry {
			reg := goldenRegistry(t, "op.fan.transport")
			if err := reg.RegisterSchema("op.fan.transport"); err != nil {
				t.Fatalf("register schema: %v", err)
			}
			return reg
		}, `id "op.fan.transport" registered as schema, want operation`},
		{"runner mismatch", func(t *testing.T) *Registry {
			reg := goldenRegistry(t, "engine.review.worker")
			if err := reg.RegisterHandler("engine.review.worker", authoring.RunnerEngineLocal); err != nil {
				t.Fatalf("register handler: %v", err)
			}
			return reg
		}, `handler "engine.review.worker" runner ENGINE_LOCAL != variant runner AGENT_WORKER`},
		{"ask kind mismatch", func(t *testing.T) *Registry {
			// ask 类型 ID 错注册进 schema 槽：human 步解析 askKind 时报 kind
			// 错用，而非含糊的 not found（单一命名空间的收益）。
			reg := goldenRegistry(t, "decision")
			if err := reg.RegisterSchema("decision"); err != nil {
				t.Fatalf("register schema: %v", err)
			}
			return reg
		}, `id "decision" registered as schema, want askKind`},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, err := Compile(goldenDefinition(), row.reg(t))
			wantErr(t, err, row.want)
			_, err = CompileDiagnostic(goldenDefinition(), row.reg(t))
			wantErr(t, err, row.want)
			var ce *Error
			if !errors.As(err, &ce) || ce.Class != authoring.FailureInvariantViolation {
				t.Fatalf("mismatch must classify INVARIANT_VIOLATION in both modes: %v", err)
			}
		})
	}
}
