# B6 累计核查增量交付

绑定候选：`1fb82ea24ea33f369743d717b166f036c2a4cdf0`。本次增量依据为
`full-audit-b6-increment-1029`；累计报告比较 `7239018..7445463`，本次没有
改写 B1–B8 的历史结果、attempt、成本或冻结验收材料。

## 结构核对与纠正边界

累计报告第 3 节将跨文件传播分为：持续执行/结果接纳的一次性公共抽取、
host 描述到配置/身份/用量的集中注册、QA 增量与资源的公共职责，以及真实原生协议差异。
这些归属仍成立，不能用文件数下降或测试通过证明整个架构合格。本次按报告所指
生产入口回看并处理具体残留；没有重新声称逐行独立审查了全部历史文件。

- R1：恢复原来假定 transcript 已存在。中断开发在领取 resume 前调用现有
  `source(identity, actualJob, context)`；采集失败保持未领取，采集成功结算原调用。
  开发恢复也通过同一 source 接口，并按真实 ResumeCount 定位调用目录。
- R2：复用 `ChangedQA` 的 case/shared-step 比较，只使改变步骤的引用者失效。
  文件改动用已提交 case 增量表达影响，省略 case 保留证据；独立白盒 review 核对
  文件 diff 和影响声明，不恢复 Go 解析器或从自由 testRef 猜文件路径。
- R3：共同准备按已完成执行前缀复用现有目录、锁和回执；不同尾部和尾部独立步骤
  从前缀复制实际准备树，包含被忽略的中间产物，保留自身 Git 绑定及前缀删除。
  纯文字说明不改变执行输入。共享资源仍沿现有 QA 终态扫描回收。
- R4：公共层只分配 WORK/TMP/OUTPUT/CACHE；默认继承外部项目工具缓存。
- R5：平台仍决定短 cwd，路径在分配前写入既有 session-request 的 resources。
  取消及终态只读已记录资源路径，不枚举 WorkBuddy 或猜临时路径；正常回合
  清理错误会返回调用者。平台会话历史不是这些临时资源。
- R6：Running 需要真实进程回执和存活进程组；claim、prompt、缺失来源不会作为
  原生运行证明。已停止调用显示结果处理或待对账，未启动单独显示。
- R7：WorkBuddy/ZCode 在平台 validate 中执行无模型 CLI 能力检查；DSH 还执行
  ACP initialize，要求 v1 及 resume/close 能力。无 session/prompt，不消费模型调用。
  这些检查证明所探测的协议/CLI 能力，实际身份、结果和用量仍由调用后的既有
  解析与 writer 核实，不能据此宣称真实宿主 regular/Seal 已验证。
- R8：候选 `1fb82ea` 已包含修正及 `TestContinuousConsumesConsecutiveGitPreparations`。
  本次实际运行通过，因此保留该修正，没有重复改写 continuous。

通用 source/result/claim/cost 权威保持原归属。安装器历史上的特殊路径/hook 布局
仍是已有合同边界；未为假想新平台扩展安装框架。本次没有以修复 R1–R8 代替
“所有平台任何升级都绝不改变公共合同”的证明。

## 新增与升级的低成本验证

现有统一合同及唯一注册方式见 `references/user-configuration.md`。
实际执行 `TestNativePlatformSingleRegistrationAndProtocolUpgrade`：本地假平台
只调用一次 registerNativePlatform，配置、安装生命周期描述、启动/续用、身份、结果、
来源重放和用量共用现有消费者；v2 改结果/身份/用量字段和私有文件布局，旧解析拒绝，
仅切换该假平台解析版本后原身份续用成功。不修改其他平台或公共核心、不重复注册。
测试文件为 `internal/engine/facade/runtime_platform_test.go`；该场景本次无需修改文件。
这是真实执行本地子进程的架构测试，不是实机宿主支持或正式 QA 证据。

## 回归入口

- `go test ./internal/engine/protocol -run 'TestWhiteboxIndependentSharedEvidence9528|TestWhiteboxFileIncrement' -count=1`
- `go test ./internal/engine/facade -run 'TestInterruptedDeveloperCollects|TestSharedPreparation|TestQAResources|TestWorkBuddyCarrier|TestProgressCoverageLifecycle|TestNativeCLICapability|TestDSHCapability|TestNativePlatformSingleRegistration|TestContinuousConsumesConsecutiveGit' -count=1`
- 全仓必要验证：`go test ./...`、`go vet ./...`、构建 CLI 与 `package validate --root .`。

白盒 `whitebox_shared_evidence_9528_test.go` 从绑定的独立白盒工作区原样复制，
保留其真实断言；原工作区及原失败记录未修改。首次本地 go test 因默认 Go 缓存
写入被沙箱拒绝，后续仅对自测命令指定 `/private/tmp/formal-gates-b6-go-cache`，
没有把这个测试环境选择写成产品缓存策略。最终各项实跑结果由当前 action validation 记录。
