# 阶段 1 封板后审计：黑盒 52 条全量执行结果（终局轮）

- 执行时 HEAD=76da5ab（执行中推进到 c0ca137 仅涉文档，Go 源与制品零变化，见异常观察 1）
- 执行契约：prompts/actions/qa-execution.md（修订版）；用例集：blackbox-audit-cases.md（52 条）
- 证据：/tmp/fg-audit-bb3/（易失临时目录——b01-b52 逐条日志、tamper//tamper51/ 篡改副本、b38/b43 前后指纹；本表为逐条判定的持久化记录）
- 总结：52/52 PASS，零回归；历史唯一 FAIL（QA-B13）经 B13 与 B52 双确认修复有效

| 用例 | 判定 | 一行证据 |
|---|---|---|
| QA-B01 | PASS | 六变体 Authority/RunnerKind 恰为 ENGINE/ENGINE_LOCAL、ENGINE/DURABLE_ACTIVITY、ENGINE/HOST_ADAPTER、AGENT/AGENT_WORKER、HUMAN/HOST_ADAPTER、ENGINE/ENGINE_LOCAL |
| QA-B02 | PASS | ①Join②Retry/.IO③Handler 复合字面量 `go build` 均报 `unknown field ... in struct literal`（exit=1），对照驱动编译成功（exit=0） |
| QA-B03 | PASS | local/durable 九项非法入参全部非 nil 且点名义务（handler id required / idempotency key strategy required / reconcile id required / positive timeout required / maxAttempts must be >= 1 等），两对照 err==nil |
| QA-B04 | PASS | host/agent/human/parallel 十五项非法组合全拒，消息含枚举合法值提示、`at least 2 children`、`must not be a child`，四对照通过 |
| QA-B05 | PASS | 空 ID/自引用/空引用各报点名 error；`[b,a,b]` 归一化为 `[a b]` |
| QA-B06 | PASS | 零值 IO 字面量 → `input codec id required (untyped IO)`、Class=INVARIANT_VIOLATION；`&step` → `unknown step variant *authoring.LocalStep` |
| QA-B07 | PASS | 六个信封失误各自可区分报错（nil definition/nil registry/version/entry node/no steps/duplicate step id），无 panic |
| QA-B08 | PASS | `definitionVersion "1" != definition "2" (unbound definition version)` |
| QA-B09 | PASS | 依赖缺失报 `dependency "nope" not found`（非 cycle）；A↔B 报 `dependency cycle among steps [a b]` |
| QA-B10 | PASS | 孤立 z → `unreachable steps [z]`；入口无根 → `entry node "entry" has no dependency-free step` |
| QA-B11 | PASS | 四变异分别报 fan-out anchor dependency required / does not depend on child (fan-out coverage) / depends on "o" outside children / child "c1" has dependent "o2" other than join |
| QA-B12 | PASS | `input binding source "z" is not a dependency` / `dependency "a" has no typed input binding` |
| QA-B13 | PASS | 六类未注册 ID 正常 Compile 均 Class=BLOCKED_BUG，消息含 MISSING_ENGINE_ADAPTER、not registered (closed world)、use diagnostic compile |
| QA-B14 | PASS | ①②duplicate id、③invalid runner、④empty id、⑤registered as handler, want codec、⑥`runner ENGINE_LOCAL != variant runner AGENT_WORKER`（INVARIANT_VIOLATION） |
| QA-B15 | PASS | 乱序+重复（deps）/混排 Negated/乱序 Inputs 与全排序两份制品 sha256 相同（f24b4f0d…，bytes.Equal=true） |
| QA-B16 | PASS | 连跑两次+跨副本 /tmp/wsB 三方两文件 sha256 全一致；制品无 $WS 绝对路径、无日期串（0 命中） |
| QA-B17 | PASS | `git status --porcelain -- definitions internal/engine/definition/identity_gen.go` 输出为空 |
| QA-B18 | PASS | 外部 shasum=e342a5f4…==WorkflowDefinitionDigest；Version="2"；Generate("/tmp/genroot") 双文件与 checked-in 逐字节相等 |
| QA-B19 | PASS | d1/d2（Steps 倒序+旋转）× R1/R2（逆序注册）四组合字节全等且等于 checked-in 制品（5981B） |
| QA-B20 | PASS | decode→encode bytes.Equal=true（5981B，sha256 同 e342a5f4…） |
| QA-B21 | PASS | batch.go 追加注释后重生成，两交付物 sha256 均不变；workflow.json 中 internal/、func 、.go 均 0 命中 |
| QA-B22 | PASS | 六维（依赖/retry/reason/requestSchema/handler/joinMode）改动 digest 两两不同；对照编译两次相同 |
| QA-B23 | PASS | 七种篡改全拒：trailing content / unknown field（外层、内层 payload）/ envelope writer / payload must be a LOCAL object, got null / must not carry an io block / requires an io block / HUMAN_ASK 形态不符（⑦b 直证 `decode HUMAN_ASK payload: unknown field "handler"`） |
| QA-B24 | PASS | 三合法边 nil、四非法边含 illegal phase transition；终止闭包完备、TERMINAL 无出边；改写表副本后权威查询不变 |
| QA-B25 | PASS | `"n/s"`、`"n/s/sc"`；空段报 `requires node and step`；String() 稳定 |
| QA-B26 | PASS | 七条合法边全过；TERMINAL 出边/回退/跳步/全部自环均报 illegal task transition |
| QA-B27 | PASS | NewState 两拒、版本不符、not in definition、already completed、dependency not completed 四类可区分；合法完成 Completed=[entry.parse] |
| QA-B28 | PASS | phase/task 合法迁移实际更新字段；非法迁移报错且字段不被改写；无效键报 invalid |
| QA-B29 | PASS | ①–⑥（BOGUS/来源不符/重复/空 Key/nil/error）各报含来源名 error；两相反顺序 canonical 字节与 digest 相同；空观察 `"facts": []` |
| QA-B30 | PASS | nil/版本/不在定义/duplicated/before dependency 各可区分，无空 Plan |
| QA-B31 | PASS | 初始 Frontier=[{entry.parse,entry,0,LOCAL}]；完成 entry.parse 后=[{entry.persist,entry,1,DURABLE} {fan.split,fan,3,PARALLEL}] |
| QA-B32 | PASS | 五段阶梯逐段吻合：READY(tasks=[review/review.worker])→ASK([ask.decide])→WAIT/TASKS_IN_FLIGHT→HOST_ACTION([fan.transport])→COMPLETE 且 Validate nil |
| QA-B33 | PASS | 重复调用字节/摘要一致；map 相反插入序 State 字节一致；两进程 PlanDigest 相同（sha256:dfacde0b…） |
| QA-B34 | PASS | 四种 tagged-union 误用均报 payload presence 不符；五类合法单 payload 组合 nil |
| QA-B35 | PASS | capacity 0→空集且 PersistIssued 仍调用 1 次；1→[act:n/b]；5→[act:n/b act:n/c]；负容量/非 READY/nil store/persist error 各报对应 error；actionID 两次相同 |
| QA-B36 | PASS | false/false/true/false；空成员批 Complete(nil)=true |
| QA-B37 | PASS | projectedPhase=START_READINESS、completed=[]、facts 恰 3 VCS+4 FILE 按 (Source,Key) 排序、unavailable 恰 HOST/LIFECYCLE/RECEIPT/CAPACITY、frontier=[{entry.parse,entry,0,LOCAL}]、WAIT/ENGINE_INTERNAL、digest==常量、observedStateSha256==外部 shasum、报告落 /tmp/qa-out |
| QA-B38 | PASS | 同/不同 OutputDir 三次报告字节与 digest 相同；state.json 字节+mtime（1787390016）与 .gates/tmp 树零变化；默认目录落 `.gates/shadow/r1.shadow.json`；报告无绝对 root 路径 |
| QA-B39 | PASS | f1/f2/f3 投影 INTAKE_REGISTERED/SNAPSHOT_READY/POST_REVIEW 且 verdict=INCOMPARABLE；f4 TERMINAL+COMPLETE+seal→MISMATCH；f5 SEALED→TERMINAL MATCH；三类判定各有正确归属 |
| QA-B40 | PASS | a/b、`..\x`、空 RunID、空 Root、不存在目录、非法 JSON、runId 不符、PAUSED 分别命中对应文案；输出目录 0 文件 |
| QA-B41 | PASS | CompileDiagnostic 成功、marker=true、诊断={s, codec.missing, codec}；Encode 与 Decide 均报 `MISSING_ENGINE_ADAPTER marker; diagnostic-only...`，plan=nil |
| QA-B42 | PASS | 两文件 `grep -c MISSING_ENGINE_ADAPTER`=0；`go run ./cmd/gen-definition` 退出码 0 |
| QA-B43 | PASS | 真实 HOME 下执行 gen-definition + 四个 go build 驱动二进制（编译/编码/决策/shadow，root 指 /tmp）后，~/.formal-gates 与 ~/.local/bin/formal-gates 全树清单+逐文件 sha256 前后 diff 为空 |
| QA-B44 | PASS | LoadFutureDefinition err=nil（非 UnsupportedRunVersionError）；WorkflowVersion="2"、SchemaVersion="1"、Digest==definition 常量 |
| QA-B45 | PASS | 追加 report.audit 纯公开 API 全链 Compile/Encode/Decode 成功，制品含新步骤且 digest 变化（→a97f836c…）；compiler/encoder/authoring `git status --porcelain` 为空 |
| QA-B46 | PASS | ①constructor：`join step "p" must be outside the parallel group`；②绕过构造的结构体字面量被 compiler 同文案二次拒绝 |
| QA-B47 | PASS | 共享 children 与共享 join 两形态均报 `parallel group ownership is exclusive` |
| QA-B48 | PASS | ①NewTaskKey 段内 / 与 \ 四例全拒；②authoring StepID/NodeID 三例全拒（两层文案一致拒 `/'\`）；③绕过构造的 `/` ID 经 Compile 后在 Decide 确定性报 `runtime: task key step "a/b" must not contain ...` |
| QA-B49 | PASS | 20 轮并发 Generate + 读者循环 43 次读取 0 撕裂（JSON 可解析、Go 源完整）；零 .gen-definition-* 残留；两文件 0644 且与 checked-in 逐字节一致 |
| QA-B50 | PASS | `RunID=".."` 报 `must not be a parent-directory reference`；输出目录未创建；被观测 state.json mtime/size 未触碰 |
| QA-B51 | PASS | 删 retry 键与 retry null 均报 `durable payload requires a retry object`；重复条目 `duplicate step id`、`share ordinal 0`、悬空 `dependency "nope" not found` |
| QA-B52 | PASS | 六槽位（codec/handler/predicate/schema/operation/reconciler）Class 均 BLOCKED_BUG，三段提示语（MISSING_ENGINE_ADAPTER / not registered (closed world) / use diagnostic compile）全部齐全 |
