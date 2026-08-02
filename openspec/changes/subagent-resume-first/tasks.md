# Tasks

- [x] 派发恢复路径：被中断且已认领（CLAIMED）、未产出结果的派发优先续用原代理（宿主机制可用时），否则才 stale 重开；不新增续用标记字段/命令（RQ-3-4 由既有 claim→identity→一次结果不变量满足）。
- [x] 生命周期适配：续用完成的派发 start→stop 成对可认证（既有生命周期模块）。
- [x] SKILL 独立派发章节改为续用优先（保留宿主能力条件；生命周期不可认证的宿主按 RQ-3-2 回退）。
- [x] 最低层测试：续用派发的 start→stop 配对由既有 `TestLifecycleCaptureAndVerification` 覆盖、回退（stale + 重开）由既有 `TestReviewDispatchClaimsAreFreshBoundAndReserved` 覆盖，无需新增；续用优先决策是编排层行为，无 Go 代码归属。
- [x] CHANGELOG。
