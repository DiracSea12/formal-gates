# Blackbox QA cases (run: phase-1-decision-kernel)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: authoring source 重新生成 checked-in definitions/workflow.json 与期望身份常量后字节零 diff（freshness，独立于 round-trip）；制品与常量由 compiler 同一生成动作产出
procedure: 入口=freshness CI/再生成命令与 go test；前置=阶段1候选已构建；操作=运行再生成入口后 git diff 核对制品与身份常量文件并运行 freshness 验收；留存再生成输出、diff、测试输出
oracle: 再生成后两文件零 diff 且 freshness 验收 PASS；失败信号=出现 diff、再生成入口不存在、freshness 被 round-trip 顶替
review status: PASS

CASE-002
mode: blackbox
description: definitions/workflow.json 为公共头+封闭变体 payload 的 canonical 制品：版本已按 bump 规则提升；不含函数/闭包/绝对路径/当前时间/无序 map、非全字段平铺
procedure: 入口=checked-in definitions/workflow.json 与阶段0版本信封；前置=候选已含该制品；操作=读制品核对结构、版本提升、扫描绝对路径与时间戳、两轮独立解析比对键序；留存 sha256 与结构摘录
oracle: 结构=公共头+封闭变体 payload、版本已提升、无绝对路径/时间/闭包、键序两轮一致；失败信号=全字段平铺、含禁类内容、版本未提升、键序不稳
review status: PASS

CASE-003
mode: blackbox
description: 任意 assembly/注册顺序编译产生相同字节与 digest（ordinal 由 compiler 派生，输入顺序不泄漏进制品）
procedure: 入口=go test ./internal/engine/definition -run TestArtifactIndependentOfAssemblyAndRegistrationOrder -v；操作=运行该验收项；留存测试输出
oracle: PASS：不同注册/装配顺序编译制品字节与 digest 完全相同；失败信号=字节或 digest 随顺序变化、验收项缺失
review status: PASS

CASE-004
mode: blackbox
description: decode→encode round-trip 后制品字节不变（单一 canonical encoder 无默认值/字段排序漂移）
procedure: 入口=go test ./internal/engine/definition -run TestCheckedInArtifactRoundTrip -v 与 ./internal/engine/encoder -run TestEncodeDecodeRoundTrip -v；操作=运行两个验收项；留存输出
oracle: PASS：解码再编码字节逐一相等；失败信号=字节变化、验收项缺失
review status: PASS

CASE-005
mode: blackbox
description: 多进程、重复构建产生相同制品字节（跨进程/重复构建确定性）
procedure: 入口=跨进程确定性验收测试与文档化生成入口；操作=运行验收项并在两个独立进程各执行一次生成入口比对 sha256；留存输出与两次 sha256
oracle: 验收 PASS 且两次独立进程产物 sha256 相同；失败信号=进程间/构建间字节差异
review status: PASS

CASE-006
mode: blackbox
description: 三类身份分离：只改 handler 实现侧（不改 ID 与定义语义）时 DefinitionDigest 不变
procedure: 入口=go test ./internal/engine/acceptance -run TestAcceptanceDefinitionDigestSeparatedFromImplementationSide -v；操作=运行该验收项；留存输出
oracle: PASS：实现侧变化（独立 registry 实例/未引用槽位差异）不改变 DefinitionDigest（PackageDigest 实际计算属后续批次，本用例只验证 definition digest 不变性）；失败信号=DefinitionDigest 随实现侧变化
review status: PASS

CASE-007
mode: blackbox
description: digest 语义敏感性：改变 dependency/policy/reason/schema ID/handler ID/join 语义时 DefinitionDigest 必变
procedure: 入口=go test ./internal/engine/acceptance -run TestAcceptanceDefinitionDigestSensitiveToDefinitionMutations -v；操作=运行该验收项；留存输出
oracle: PASS：每类语义变化均使 DefinitionDigest 改变；失败信号=任一语义变化后 digest 不变
review status: PASS

CASE-008
mode: blackbox
description: registry 完备性：每个 ID 精确解析一次；缺失/重复/kind 不匹配拒绝；完整解析才激活（definition 与 binary 锁步）
procedure: 入口=go test ./internal/engine/acceptance -run TestAcceptanceRegistryCompleteness -v；操作=运行该验收项；留存输出
oracle: PASS：三类非法均被拒且完整解析才可激活；失败信号=任一非法 ID 被接受或未完整解析即激活
review status: PASS

CASE-009
mode: blackbox
description: constructor 非法状态不可构造：六变体只经包内 constructor 构造，必填缺失/非法枚举/children<2 拒绝；authority/runner 只能派生不可手填
procedure: 入口=constructor 非法状态验收测试；操作=运行该验收项；留存输出
oracle: PASS：合法六变体可构造、各非法组合均构造层被拒；失败信号=任一非法组合可经正常 authoring API 构造成功
review status: PASS

CASE-010
mode: blackbox
description: mutation 拒绝与八类非法定义全保留：随机删依赖/join/failure edge/version/reconcile 必拒；八类拒绝按 enforcement matrix 分层拦截；分支目标封闭（并行组成员依赖仅指向 join）显式枚举校验
procedure: 入口=go test ./internal/engine/acceptance -run 'TestAcceptanceMutation' -v（枚举+fuzz 两函数）+ go test ./internal/engine/compiler -run TestGraphRejects -v（分支目标封闭独立枚举行）；操作=运行该组验收；留存输出
oracle: PASS：每类删除/非法定义被对应层拒绝且结果可见，TestGraphRejects 的 branch closure 行独立通过；失败信号=任一 mutation 或非法定义被接受、某类拒绝被静默丢弃、分支封闭行失败
review status: PASS

CASE-011
mode: blackbox
description: 复杂度止损：新增普通业务节点不得要求修改 compiler core；compiler 不理解业务语义
procedure: 入口=止损验收证据（新增业务节点探针）；操作=运行/核对探针只新增节点定义即编译通过且 compiler core 零改动；留存输出与改动范围证明
oracle: PASS：探针仅新增节点即编译成功、core 无 diff；失败信号=新增普通节点需改 compiler core
review status: PASS

CASE-012
mode: blackbox
description: 决策核心确定性：相同 state+observation 产出字节级稳定 canonical Plan；frontier 完整固定顺序；NextResult 仅六类 Kind 且唯一；拒绝乱序/遗漏/重复 step；非终态无空结果
procedure: 入口=go test ./internal/engine/decision -run 'TestDecide|TestObserve' -v 与 ./internal/engine/acceptance -run 'TestAcceptanceLegalCompletionSequenceGolden|TestAcceptanceIllegalCompletionSequencesRejected' -v；操作=运行该组验收；留存输出
oracle: PASS：全部不变量成立、同输入 Plan 字节相同；失败信号=Plan 不稳定、frontier 不完整、第六类外 Kind、乱序/遗漏/重复被接受、非终态空结果
review status: PASS

CASE-013
mode: blackbox
description: MISSING_ENGINE_ADAPTER 仅 diagnostic-only：只产诊断，不得编译为 executable plan 或签发 Ready/HostAction；正常 compile 路由 BLOCKED_BUG
procedure: 入口=go test ./internal/engine/acceptance -run 'TestAcceptanceMarkerScan' -v；操作=运行该组验收；留存输出
oracle: PASS：marker 定义只产诊断、executable 编译稳定以 BLOCKED_BUG 拒绝；失败信号=marker 被编译为可执行计划、签发 Ready/HostAction 或被静默降级
review status: PASS

CASE-014
mode: blackbox
description: 最终候选无技术债 marker：对最终候选 identity 的扫描证明 executable definitions 无 MISSING_ENGINE_ADAPTER
procedure: 前置=最终候选已冻结；入口=收口段登记的候选 marker 扫描；操作=执行或核对扫描证据；留存扫描命令与结果
oracle: 扫描零命中；失败信号=任一 executable definition 含 marker 或扫描证据缺失/未绑定候选 identity
review status: PASS

CASE-015
mode: blackbox
description: phase-0 测试隔离缺陷已修复：普通 shell 直接 go test ./... 不写真实用户级 registry/安装路径；launcher 不再被写成空桩
procedure: 前置=记录真实 stable 安装 sha256 与 registry 记录数；入口=go test ./... 与隔离回归用例；操作=跑全量测试后复记 sha256 与计数并运行回归用例；留存前后对照与测试输出
oracle: 全量测试全绿、前后 launcher sha256 一致、registry 无新增记录、回归用例 PASS；失败信号=launcher 字节变化、registry 新增记录、回归用例缺失
review status: PASS

CASE-016
mode: blackbox
description: Shadow 只读 E2E：从候选 installed binary 在独立测试项目执行 shadow harness——只读 legacy 状态、输出预测与差异报告（预测内容与已知状态 fixture 的期望 frontier 直接比对）、不写权威 state、telemetry 只落候选目录、同输入输出字节稳定
procedure: 前置=隔离安装与独立 namespace 及已知状态 fixture；入口=候选安装文档化的 shadow harness 调用方式；操作=运行前记录被观测状态 sha256/mtime，执行后复记并核对 telemetry 落点与 fixture 期望 frontier 比对，同输入再执行比对；留存报告、状态摘要、telemetry 清单
oracle: 产出预测与差异报告且预测内容与 fixture 期望 frontier 一致、权威状态字节与 mtime 不变、无副作用、telemetry 仅候选目录、两次输出一致；失败信号=状态被写、telemetry 落 stable/开发路径、输出不确定、预测与 fixture 不符
review status: PASS

CASE-017
mode: blackbox
description: 候选隔离安装与 namespace 不重叠：安装清单记录 identity/digest 与 canonical path 不重叠证明；Lstat 无指回开发树或稳定区的 symlink；候选不写 stable registry、不签发权威 Seal
procedure: 入口=收口段隔离安装构建与 namespace disjoint proof；操作=执行构建与验证并核对清单；结束后检查 stable registry 无候选写入；留存清单、proof、registry 前后对照
oracle: 清单完整、canonical path 不重叠、无违规 symlink、stable registry 零候选写入；失败信号=symlink 指回可变区、路径重叠、候选写 stable registry 或出现权威 Seal
review status: PASS

CASE-018
mode: blackbox
description: installed candidate binary 的 legacy 正常路径回归：lightweight 最短闭环、只读命令、package validate、canary portable 均按 stable 文档工作；产物只落候选 namespace
procedure: 前置=隔离安装与独立测试项目；入口=候选安装中的 stable 兼容 CLI；操作=按文档执行 lightweight 闭环与只读命令、对候选树跑 validate/canary；留存输出与产物落点
oracle: 各命令行为与 stable 文档一致、validate/canary PASS、产物仅落候选 namespace；失败信号=legacy 行为变化、验证失败、候选 run 写 stable 状态
review status: PASS

CASE-019
mode: blackbox
description: 固定 stable driver 不受影响：重冻结 stable 正常入口 smoke 通过、SKILL 指纹 46941e99、安装树无指向开发 worktree 的链接；本 run 全程由该驱动写出留痕
procedure: 入口=stable 安装的文档化只读命令与安装树检查；操作=跑 smoke、核对指纹、Lstat 检查链接、核对 run 留痕由该驱动写出；留存 smoke 输出、指纹、Lstat、留痕索引
oracle: smoke 通过、指纹一致、无 dev-worktree 链接、run 留痕完整；失败信号=stable 入口失败或安装被开发内容污染
review status: PASS

