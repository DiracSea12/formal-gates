# G3b 终态资源管理（轻装模式记录，2026-09-20）

方案 §13.1 G3b：归档、独立核验、预览与回收迁入 Go；不重写活动检查点——LangGraph 数据库只经
Python 的只读 checkpointer 与 SQLite backup 接口使用，Go 不解释 checkpoint/writes 表结构。
按机主既定轻装口径执行（差分＋全量回归＋独立复核＋单提交，不跑 acceptance/admission 仪式）。
实施由子代理完成，主代理定合同、亲读全部 diff、亲跑全部验证并加独立探针。

## 十项桥接操作（retention.py @ f287033 → internal/harness/retention.go）

| 操作 | 内容（原位置） |
| --- | --- |
| `archive-inventory` | 成员准入：SKIP 集先于 symlink 拒绝、dir 跳过、非正则拒（inventory） |
| `archive-destination` | 归档目的地须在 run 外，按路径**分量**语义（archive_run:139） |
| `archive-settled` | 终态集判定＋作业结清（_terminal:123 与 ensure_jobs_settled 合并，序保持：终态先、结清后，probe 事实由 Python 的 os.kill 产生） |
| `archive-index-plan` | ARCHIVE_EXISTS 拒绝＋archive-manifest 索引组装（格式 formal-gates-archive-v1，字段与序照迁移前字面量） |
| `archive-verify` 两阶段 | 独立核验：先重复成员判定，manifest 就绪后按序判 format/终态、成员集合、unsafe path、逐成员内容（checked_archive） |
| `archive-stale` | 源漂移：新文件→缺文件（allow_missing 时放行）→逐名比对，checkpoints.sqlite 走逻辑哈希特例（_compare_live，archive 与 prune 共用） |
| `archive-projection` 两阶段 | 终态可查：无 index 的预检让 ARCHIVE_CHANGED 先于任何 zip 打开；带 index 时组装投影并判 tombstone 三元绑定（archive_status/archived_status） |
| `prune-admission` | 回收资格：仅本安装 runs 目录＋归档在 run 外（prune_run:191-194） |
| `prune-bind` | 索引三元绑定：source_directory/run_id/revision（prune_run:199-201） |
| `prune-plan` | marker 绑定（真值门与 is-None 语义分开保真）→漂移→KEEP 过滤＋删除序（深度降序、同深名字序）→PRUNE_PREVIEW 报告→marker-v1 计划（prune_run:203-215） |

留在 Python 的：锁、SQLite backup/逻辑哈希、zip 读写、流式哈希、硬链接发布与 fsync、
unlink/rmdir、进程探测、原子写 marker、`_terminal` 中的 cli/graph 只读终态读取与证据核验、
ARCHIVED/PRUNED 结果 dict 的最终组装、marker 存在性检查与其常量消息（RUN_ARCHIVED 等）。
公开名与签名全部保持（test_retention.py 直接 import 的六个函数原样）。

## 等价证据

- `reference_retention.py`：十项判断按 HEAD f287033 逐字复制抬到 op 载荷形状。
- `test_retention_differential.py`（26 用例）：canonical 整返回值对比，语料覆盖 SKIP 先于 symlink、
  双成员先于 manifest、set/路径/内容三向拒绝、结清四分支、KEEP＋删除序、marker 三拒、
  投影字段、allow_missing 两态、db 逻辑特例（逻辑变拒/字节变过）、分量语义（`/a/bc` 不算 `/a/b` 内）。
- `internal/harness/retention_test.go`（27 测试）同批边界。
- 既有功能测试 `test_retention.py`（12 用例：真实 FS/SQLite/zip 全流程、崩溃恢复标记、
  错归档/中断/源数据变化不误删）**逐字未改、原样全绿**——本单元的能力级验收。

## 主代理亲验（2026-09-20，最终树上）

- `gofmt -l internal/harness/` 干净；`go vet ./...` 零输出；重建二进制 OK。
- `go test ./...` 全 ok（internal/harness 80.255s、internal/validate 376.308s、internal/cli 90.510s，均非缓存）。
- Python 全套：**327 项 OK**（G3a 后 301＋新增 26）；差分子集 `test_*differential*.py`：**180 项 OK**；
  `test_retention*.py`：**36 项 OK**。
- 独立探针：未知载荷字段 → `INVALID_HARNESS_OPERATION` 退出码 2；`archive-destination`
  分量语义四例（`/a/bc` 外、相等内、子路径内、外部路径过）全部符合。
- 基线 f287033 双套件全绿（G3a 收口时亲跑）；本单元模型调用 **0** 次，未做实机原生运行
  （纯计算＋本地文件系统单元，无宿主协议/进程/交互改动）。

## 有界差异（子代理披露 7 项均复核认可；另 2 项主代理复核时补充）

子代理披露：
1. bool-pid：JSON `true` 作 pid 旧代码被 CPython `isinstance(True,int)` 放行，Go 按非整数拒——机主"不做机械仿真"裁定下的有意分歧。
2. op6/op10 增补 `live_database_logical_sha256` 载荷字段：db 逻辑特例要求活库哈希过桥（sqlite 读取留 Python）；仅 current 含 checkpoints.sqlite 时计算。
3. `archive-projection` 做成两阶段：既有测试要求 ARCHIVE_CHANGED 先于任何 zip 打开，预检阶段只判 changed——与 archive-verify 的两阶段同构。
4. ARCHIVE_EXISTS 判定时机后移到索引组装（载荷需备份后 proof）：目的地已存在**且**清单含 symlink/非正则时新代码先报 ARCHIVE_UNSUPPORTED；备份在临时目录中被丢弃，无可观察副作用；RUN_NOT_TERMINAL/UNSETTLED 仍先于 EXISTS。
5. checked_archive 包装序：status/files 键预访问与成员流式哈希先于 Go 判定——"错 format 且缺 status 键"这类双重故障角点上报包裹 KeyError 而非格式拒绝文案（单故障输入行为不变）。
6. jobs 事实（含 os.kill 探测）先于 Go 的 RUN_NOT_TERMINAL 判定收集；signal 0 无副作用。
7. 琐碎：inventory 对 SKIP 条目也做分类 stat（无可观察差异）；sumSizes 对非数字 size 跳过（proof 生成的 size 恒为整数，不可达）。

主代理补充：
8. 腐坏 marker/status 缺键角点：旧代码在 `marker['run_id']`、`result['run_id']`、`index['status']` 非对象等处抛裸 KeyError/TypeError；新代码经宽松取值报类型化 ARCHIVE_* 拒绝。两类都是错误路径，仅类型/文案不同；仅手工改写状态可达（项目边界内为 P3 建议）。
9. bool↔int 相等性：Python `True == 1` 为真、Go pythonEqual 为假——仅影响"篡改清单把 size 写成 true/1 互换"这类对抗性输入，与 C4 时代已登记的 `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON` 同类（P3 先例）。

另注：桥接返回经 Go map 序（键按字母序）编回 Python，故 CLI 展示的 JSON 键序与迁移前的插入序不同
（值等价、JSON 对象语义不变）；Python 侧组装的 ARCHIVED/PRUNED 结果 dict 键序保持原插入序。

## 未做与后续

- 未跑 acceptance.py/admission.py 仪式（轻装裁定）。
- `retention.py` 的 `stream_proof/proof/db_proof` 仍持有 hashlib/sqlite——它们是 I/O 事实产生器
  （流/逻辑库哈希），非判断；按"Go 持规则与格式、Python 持进程与文件系统"边界有意留在 Python。
- 后续单元：G4（Go 内核 K1–K5，独立 Go run 中分项建立调度/流程确认/持久化/恢复）→ G5（切换发布）。
  G4 起前置为 G0–G3b 全链组件组合，属新形态工作，派发前需与机主对齐。
