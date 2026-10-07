# 一个正式流程示例

需求：“给 tdo 增加归档命令，把已完成任务移入归档，并能查看归档列表。”
本示例展示角色和次序；具体字段与调用形式只见 [正式流程公共合同](formal-flow.md)。

1. 普通修改请求先非阻塞提醒。用户明确要求 formal-gates 后，主代理读取仓库事实，
   只澄清仍影响范围、公开行为、验收或主要架构的选择。无待澄清问题时明确说明。
2. 把完整需求、方案和有限 REQ/AC/来源写入唯一正式文件。先结构预检，再展示全文
   与精确 revision，单独等待用户确认。修改时仍更新该文件并重新预检与确认。
3. 确认后如 mode 尚未明确，再选择 regular/lightweight。用 typed intake receipt 启动
   run；首次 drive 自动登记同一确认，不重新发起全文确认。
4. regular 先独立产品审，再独立技术审。Operator 核实 raw finding，完整展示有效
   finding 后由用户逐项处置。技术审形成 Batch→Subtask 与 split/no-split 计划。
5. 用户确认计划、拓扑与路线。engine 签发当前 Ready 集合，开发与基线隔离区中的黑盒
   设计/审查并发；开发者仅实现已确认内容，审查者不接收开发解释。
6. adapter 接纳开发 manifest，跟踪、提交并固定不可变快照。黑盒屏障完成后白盒独立
   核对覆盖、编写/复用测试；全部 FROZEN case-set 纳入候选身份。
7. engine 冻结完整验证 wave，按容量并行推进 QA 与门。所有目标终结并裁决后，失败
   只归入一份统一 repair；未受影响证据经精确 receipt 继承。
8. 必需义务全部通过后，promotion 固定同一候选并对账；engine 清理登记资源并核验
   无残留，才完成 Seal。报告引用真实 receipt、snapshot 和原始证据。

lightweight 在第 3 步选择，不进入后续开发/QA/门流程；只留下明确 unverified 记录。
中断恢复使用原 run 的 drive；UNKNOWN 先对账。需求变更、reset、abort、路线变更和
合法节点跳转从当前 availableActions 发起 typed Ask，经 submit 记录实际用户决定。
