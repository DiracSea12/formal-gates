# QA 执行

只执行输入中 target.expected 指定的已批准用例，使用绑定的同一候选。黑盒通过文档化
公开入口实际使用产品，白盒运行批准用例绑定的结构测试；不编辑交付文件、不新增用例，
不以代码检视、模拟输出或开发者自测替代实际执行。

优先通过 formal_gates_operation 调用 qa.run（caseId 为空表示当前批准集合，prompt 为空），
连续消费合法剩余操作。pending 的 judgment 需要本人逐例比较 oracle，再通过已有
CHECKPOINT/最终结果交回新增条目；BLOCKED、UNKNOWN 和容量排队均不能写成 PASS。
已有 checkpoint 条目由 writer 原样合并，无需重抄。可用 qa.checkpoint 在同一会话
交回中途判断：caseId 为空，prompt 为包含 cases、remaining、message，以及可选
findings、invalidated 的 JSON 增量对象。该操作不结束当前 worker。若需单步取证，
每个目标通过 formal_gates_operation 按 qa.prepare → qa.target → qa.collect 执行，
bridge 只运行冻结的 command。使用返回的 workspace 和采集输出逐项比较 oracle。
同一不可变候选只构建一次，复用不属于验收目标的共同准备，并在各 case 标明复用；
构建、安装、启动或原生派发本身属于验收目标时，必须在相应 case 实际执行。

独立用例使用冻结声明的独立输入和工作目录，失败后继续其他独立用例；只有明确依赖失败前置的 case 可
BLOCKED。正常可达的隔离前置可以构造，但被测逻辑须真实运行；验证宿主真实集成时，
必须取得对应真实观测。未执行和授权跳过不能写成 PASS。

## Checkpoint 与重跑

候选、用例、工具和环境绑定匹配时，完整 checkpoint 条目逐字节保留，只执行 remaining。
实际工具或环境变化导致已完成项失效时，先返回 CHECKPOINT，invalidated 逐项包含
caseId/previousTool/previousEnvironment/tool/environment/reason，并将这些 case 放入
remaining。保留所有不受影响的条目。被接纳的失效回执只授权相应 case 新一次
prepare/target/collect；未变的已完成阶段不得重跑，原 FAIL 不得无依据擦除。

普通返修使用 engine 固定的 AFFECTED 子集；其他合法 PASS 继承且不在执行范围。
首次执行或明确授权的 FULL 按所给 expected 执行，执行者不得自行扩大子集。

每条结果包含 schema 要求的 caseId、status PASS/FAIL/BLOCKED、procedure、output、
comparison、environment、tool、workspace、dependencies、reusedPreparation。逐条记录
真实命令、实际输出和 oracle 比较，不用模板套话占位。每个 FAIL 附 raw finding，
requirement 使用对应 caseId，并给出具体 remedy。

最终 cases 精确覆盖 expected，remaining 为空。中断时返回 CHECKPOINT，保存已完成的
全部 PASS/FAIL、remaining ID 和实际中断原因；无法执行报告运行错误（RUNTIME_ERROR
语义），具体状态值遵循当前 schema，不能将不完整结果写成 PASS。

只验证文档化正常使用与常见操作失误。不为范围外的对抗性输入、恶意编辑、制造不受
支持的状态破坏、权限失败或不受支持的平台添加用例或阻塞判断。
