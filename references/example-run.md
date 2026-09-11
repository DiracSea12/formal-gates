# 当前 regular 走查

可直接编码的完整前态和事件示例见
[事件字段参考与隔离前态示例](engine-regular.md#事件字段参考与隔离前态示例)。
这里按用户操作顺序说明一个 hello CLI 任务。普通请求采用默认提醒分支：“若需走 formal-gates
流程，可直接提出”；普通请求不进入本示例的正式流程分支。复杂需求在需求澄清与方案确认完毕、
准备开做之际再额外强调一次：“检测到复杂需求，建议走 formal-gates 流程”。

1. 用户要求正式流程，主代理澄清 stdout、退出码、参数和范围，把完整方案写入单一
   requirements.md，Git 提交基线；start 绑定，drive 预检。展示全文和 revision，单独确认。
2. submit REQUEST(confirm-requirement)，用返回 Ask 绑定 DECIDE；同样选择 mode regular。
3. drive --action-id 执行已签发产品审；Operator 逐项 ADJUDICATE，必要 finding 由用户处置。
   产品审通过后才执行技术审，完整 granularity_review 形成 Batch/子步骤与 DoD。
4. 用户确认 topology 的实际 planDigest 和 no-split 理由，再确认 full/custom。
5. drive 准备隔离资源。开发者写 hello 实现，黑盒在基线副本按公开需求设计 command 和
   oracle，两者并行；黑盒失败继续自身重试/恢复，不等待开发收尾。
6. 开发完成后 adapter 校验 manifest、track、commit 和 snapshot；白盒独立补测试，
   所有已选 case-set FROZEN 后装配最终候选。最后必需验证开始前整理 Git 多提交。
7. drive 返回唯一 wave 的并行 QA/门动作。QA 每例使用 prepare、target、collect 的原生
   动态工具；门只审完整实现 diff。每项回传真实结果/source，独立目标继续。
8. 若 hello 输出错误，QA FAIL 给完整 finding；wave 收齐和裁决后唯一 repair 修正。新
   候选按 AFFECTED 重验，保证最终再 FULL。有限额度耗尽时由当前 Ask 给用户处置。
9. drive 执行 promotion 的实际等价核对、Seal 和资源清理；show 返回终态记录。发布由
   独立 Seal/集成/切换流程执行，不能把开发快照或隔离安装成功当作全局发布。

中断时先 show/diagnose。已签发原动作通过 drive --action-id --resume 恢复；内部操作用
原 operation-request 文件加 --resume；UNKNOWN 不能改 ID 重新调用。具体观察文件和
真实成功但 receipt 未写入窗口的取证方法见公开合同末节。
