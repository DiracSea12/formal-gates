# 原生 VCS 与候选身份

正式流程由 VCS adapter 持有 Git/SVN/P4 操作，代理只编辑授权内容并返回语义结果；原生差异与登记范围由程序取得，
不要求模型重列精确路径或决定执行顺序。
完整公共调用合同见 [正式流程](formal-flow.md)。

adapter 负责 provider/root 探测、status/diff/untracked、base/current/ancestry、工作区
创建、显式 track/add/open/reopen、resolve 核验、集成、commit/submit、snapshot、Git
压缩、已授权 reset/rollback、cleanup 和崩溃对账。只跟踪明确交付路径，不加入无关文件。

Windows 上的 Git 工作区受原生 Git 路径长度限制；项目路径过深时，请移到更浅的目录。

reviewer/QA 使用 Controller 绑定 provider、base/current、definition 版本与 digest 的
只读 snapshot/diff artifact。质量门重跑始终比较原基线到最终候选完整 diff；继承判断
比较受影响范围并绑定原始证据，不用修复增量代替完整门审。

## Provider 身份

| Provider | 不可变身份与资源 |
| --- | --- |
| Git | commit 与 tree/blob/mode/ref；登记临时 branch、ref 与 worktree。 |
| SVN | 明确 working-copy/repository 身份、revision/property 与候选路径。 |
| P4 | client/view、已提交 changelist、文件摘要/类型与 workspace。 |

候选冻结包含全部交付文件、白盒测试和 FROZEN case-set；外部产物使用独立 digest 与
summary 绑定。Git 基线到当前超过一条提交时，adapter 在最后必需验证之前压缩为同树
最终候选；SVN/P4 不压缩。Seal 只固化已验证身份，不压缩或增加候选内容。

promotion 可以保持原身份或由精确 receipt 证明内容等价；不得猜测提交号不同仍等价。
所有非幂等副作用共用 intent→execute→observe/reconcile→commit。外部已达到精确
预期状态时接纳原执行事实，UNKNOWN 先对账，不重复 squash、submit 或集成。

外部漂移先冻结 observed identity、lineage、只读 diff 和 adoption preview。用户在
当前 typed Ask 授权后，保留原 base，把精确外部 identity 绑定为 current；影响与继承
逐项确认，无法证明未受影响的项按 affected 建议处理。不能静默采纳或切换 provider。

终态清理覆盖 resource registry 中全部 workspace/client/ref/changelist；先可恢复
intent/summary，再逐项 observe，取得无残留 receipt 后才 Complete。异常按当前 retry、
Wait、Operator 或明确失败处理，不动态降级为代理写 VCS。
