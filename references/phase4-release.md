# 阶段 4 安装、发布与证据

阶段 4 源码交付支持 Codex/Git no-split regular 和既有 lightweight。Git split、SVN/P4
和其他宿主的 regular 仍按后续阶段交付；不迁移活动旧 run，不把旧 run 交给新包续写。
普通用户/AI 可编辑来源与派生产物见 [用户配置](user-configuration.md)。

发布由既有安装事务持有，不另建发布 writer。顺序固定：

1. 从精确已提交候选构建隔离安装包；在独立项目、home、host config、registry 与资源
   namespace 执行阶段 QA、regular canary 和历史回归，并演练安装/失败回退。
2. 固定 stable driver 对阶段交付 Seal；候选不得驱动自己的权威 run。
3. Seal 后集成主线，记录 sealed identity 与 post-integration identity；树或包变化时
   按影响重建并重验，保留精确后继基线。
4. 经既有 stable launcher 安装事务执行全局切换。事务统一覆盖 runtime、hook/config、
   managed-rule、pointer 和 registry；使用已验证的精确包。旧稳定安装器需要先按
   [安装与 Hook](install-and-hooks.md) 应用并安装通用维护补丁，再安装阶段 4；两步均
   在 Seal 后使用原 stable owner 的事务完成。
5. 从实际全局入口在独立项目验证身份、Codex/Git regular 路由、执行前认领和历史功能。
   将本次回归表、输出及安装 identity 附到发布 receipt，完成后才启用阶段 5–7 driver。

安装、卸载、故障注入和回退命令沿用 [安装与 Hook](install-and-hooks.md)。破坏性用例
只在隔离安装目标执行；真实全局检查仍需实际入口、身份绑定与正常使用，不调用开发目录
binary 或旧 driver 代替。活动旧 run 不阻塞安装、不迁移状态；旧 generation 的写入由
现有 admission/fencing 拒绝。安装失败由现有 journal/reconcile 恢复稳定包，generation
保持单调，保留失败 receipt。

发布 receipt 应记录 sealed/post-integration/source/package/installed identity、canonical
namespace 与 disjoint proof、安装 transaction/registry receipt、旧版本绑定与回退结果、
全局入口检查，以及历史回归每项的正常/失败/未验证/有依据替代或延期状态。缺少全局
结果、存在仍承诺功能的回归或必需证据缺失时，不记录发布完成、不启用后续 driver。
旧开发 driver 在发布完成后退出日常驱动；旧稳定包只按安装失败回退合同保留。

本文件是发布操作合同，不是已发布证明。正式 QA、Seal、集成及真实全局切换由规范流程
在相应时点登记。源码测试或隔离安装演练不充当真实全局执行记录。
