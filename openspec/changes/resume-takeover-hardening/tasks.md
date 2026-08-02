# Tasks

- [ ] A1: 放开 `workflow start --base-snapshot` 为 HEAD 祖先；测试祖先/相等/非祖先拒绝。
- [ ] A2: SKILL.md + references/formal-flow.md 增加接手协议；resume 标注为备用路径。
- [ ] A2(补充): SKILL.md 正式第 2 步只负责登记已对齐需求；澄清问答/呈现/确认/持久化全在受理阶段完成。
- [ ] (Phase 2 后续 run) 修 `prompts/actions/requirements-clarification.md`：Q&A 位置、持久化、确认粒度。
- [ ] B1: `RunState` 记录逐门/逐 action prompt 哈希；兼容无该字段的旧状态。
- [ ] B2: 中途任意文件修改 → 主代理检视改动范围逐结果判定继承或重跑（不限于 prompt）；`requireCurrentCatalog` 改按门 delta 分类，不得自动全量失效。
- [ ] B3: 扩展 `workflow resume` 承载"采纳外部改动"，重绑 CurrentSnapshot + 记录 origin/reason（需用户确认）；`requireNativeCurrent` 放行已重绑状态。
- [ ] B4: 删除开发后 meaning-preserved 硬禁止；改为主代理认定 + 用户确认，保留 PASS。
- [ ] B5: 把 carry 原语推广到任何重绑定时刻；主代理继承需理由，否则独立判定/重跑。
- [ ] B6: SKILL.md 增加判定手册（rubric），修改既有章节。
- [ ] P1: runner.go 重跑门提示词显式声明完整 base→current 范围。
- [ ] P2: 门审结果契约新增 `compared` 快照对；不匹配即丢弃。
- [ ] P3: vcs-snapshots.md / SKILL.md 更新重跑范围与报告要求。
- [ ] 最低层单元测试 + CHANGELOG 更新。
- [ ] (Phase 3 后续 run) 中断子代理续跑优先：宿主支持续用时恢复原代理继续，否则才标 STALE 重开；适配生命周期校验与 identity 占用规则。
