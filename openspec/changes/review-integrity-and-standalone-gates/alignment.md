# Requirements Alignment

Date: 2026-08-06
Status: confirmed

## RQ-001 - 审查子代理第一步污染检查

审查子代理收到任务后的第一步 SHALL 核对任务文本完整性：任务只应包含 CLI 组装的块
（`[Shared reviewer contract]` / `[Action]` / `[Gate]` / `[Current requirement]` /
`[Current change]` / `[Action input]` / `[Dispatch]` / `[Result contract]`）。任务块结构
之外出现的任何附加文本 SHALL 视为污染；发现污染 SHALL 立刻拒绝、不进入审查。

## RQ-002 - 污染定义与拒绝信号

污染 SHALL 定义为：块结构之外、由主代理擅自夹带的、暴露"之前做了什么 / 修了什么"等有锚
定效应、破坏零上下文环境的信息。发现污染 SHALL 返回 `RUNTIME_ERROR`，其消息 SHALL 传达
拒绝原因（检测到何种污染），使主代理可见并处置；`RUNTIME_ERROR` 不计入审查轮次上限。

## RQ-003 - 合法输入例外

`[Action input]` 内的既有用例（qa-design 增量修改所需）与已拍板问题（settled findings）
SHALL 为合法输入、不算污染。

## RQ-004 - 污染检查范围

污染检查 SHALL 覆盖门审、qa-design、qa-review、product-review、start-readiness、
requirements-clarification；SHALL NOT 覆盖开发代理（非零上下文）与 qa-execution（执行
者）。

## RQ-005 - CLI 强制继承操作处理完毕才能重跑

任一项继承判定未处理完毕时（需求产物变更未重新分类、或存在旧快照 PASS 结果待 Carry 决
策、或检测到的中途修改未判定），CLI SHALL 拒绝继续 / 重跑的入口（prepare / claim /
record / snapshot / seal / qa-* / authorize-repair 等），直到处理完。

## RQ-006 - 处置命令豁免

`carry` / `requirement` / `settle-findings` SHALL 为继承处置命令、豁免 RQ-005 硬闸。

## RQ-007 - CLI 强制恢复中断子代理

同一职责（同门 / 同 action 目标）+ 快照不变（派发源快照 == run 当前快照）+ 任务不变
（当前组装提示词 hash == 派发 PromptHash）+ 无用户主动中断或授权时，被中断且未出结果的
已认领子代理 SHALL 必须恢复原代理（同一身份），CLI SHALL 拒绝 prepare 同目标新派发并指
示恢复原派发 / 原代理。

## RQ-008 - 续用例外

快照已变、或不同职责、或任务已变、或用户经 `--user-requested` 显式授权重开时 SHALL 不受
RQ-007 限制、可开新派发。用户有权主动中断 / 重开；主代理 SHALL 无破例权。

## RQ-009 - 续用范围与审计

RQ-007 SHALL 覆盖开发 / 修复派发与全部审查派发。续用完成的派发 SHALL 保持同一身份、同
一结果映射，可经生命周期模块 start / stop 配对认证（既有 `VerifyDispatch`）。

## RQ-010 - 门单跑 / 选几个跑（完全脱离流程）

SHALL 提供新能力：一个或多个门完全脱离 run 运行；审查对象为工作树 vs HEAD（未提交改
动）；无需需求文档；使用轻量零上下文审查者（不做 claim / 生命周期绑定）；结果仅展示、
不持久化。

## RQ-011 - 单跑复用共享契约

单跑门 SHALL 复用共享审查者契约（`prompts/reviewer-base.md`），因此 RQ-001 污染检查在单
跑中自动生效；RQ-007 续用不适用于单跑（无持久化派发，中断即重跑）。

## RQ-012 - 边界

本变更 SHALL NOT 引入对抗性加固、权限 / 不可变文件故障注入、不受支持平台的兼容路径或框
架层，除非已确认需求明确要求。提示词与文档改动 SHALL 优先修改既有文件、不新增，新增文
本精简克制。
