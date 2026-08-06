# Proposal

对 formal-gates 的审查完整性与 CLI 强制做四项改进：审查子代理第一步污染自检、继承操作
处理完毕才可重跑、中断子代理必须续用（含审查子代理）、以及门脱离流程的单跑/选跑。

1. **审查子代理第一步污染检查。** 审查子代理收到任务后的**第一步**核对任务文本完整性：
   任务只应包含 CLI 组装的块（`[Shared reviewer contract]` / `[Action]` / `[Gate]` /
   `[Current requirement]` / `[Current change]` / `[Action input]` / `[Dispatch]` /
   `[Result contract]`）。块结构之外由主代理擅自夹带、暴露"之前做了什么 / 修了什么"等有
   锚定效应、破坏零上下文环境的信息即污染，发现即**立刻拒绝**（返回 `RUNTIME_ERROR` 附
   拒绝原因，主代理可见并处置）。`[Action input]` 内的既有用例（qa-design 增量修改所需）
   与已拍板问题（settled findings）是**合法输入**、不算污染。范围：门审、qa-design、
   qa-review、product-review、start-readiness、requirements-clarification；不含开发代理
   （非零上下文）与 qa-execution（执行者）。

2. **CLI 强制继承操作处理完毕才能重跑。** 全部继承判定（需求修订分类 `--meaning
   preserved|changed`、Carry INHERIT/RERUN、检测到的中途修改）任一项未处理完，CLI 拒绝
   继续 / 重跑的入口（prepare / claim / record / snapshot / seal / qa-* /
   authorize-repair 等），直到处理完；`carry` / `requirement` / `settle-findings` 是处
   置命令、豁免。

3. **CLI 强制恢复中断子代理（含开发 + 审查）。** 同一职责（同门 / 同 action 目标）+ 快
   照不变（派发源快照 == run 当前快照）+ 任务不变（当前组装提示词 hash == 派发
   PromptHash）+ 无用户主动中断或授权时，被中断且未出结果的已认领子代理**必须恢复原代
   理**（同一身份），CLI 拒绝 prepare 同目标新派发。用户有权主动中断 / 重开，经既有
   `--user-requested` 显式授权放行（只有用户可破例、来源记入 ReviewOverrides）；主代理无
   破例权。宿主确认无法恢复也走同一用户授权路径。

4. **门单跑 / 选几个跑（完全脱离流程）。** 一个或多个门完全脱离 run 运行；审**工作树 vs
   HEAD**（未提交改动）；**无需需求文档**；**轻量零上下文**审查者（新零上下文代理、不做
   claim / 生命周期绑定）；**结果仅展示、不持久化**。复用共享审查者契约，故需求 1 的污
   染检查自动生效；需求 3 的续用不适用（无持久化派发，中断即重跑）。
