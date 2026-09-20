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
- 既有功能测试 `test_retention.py`（**10** 用例：真实 FS/SQLite/zip 全流程、崩溃恢复标记、
  错归档/中断/源数据变化不误删）**逐字未改、原样全绿**——本单元的能力级验收。
  （**更正 2026-09-20 返修时**：本记录初版把该项写成"12 用例"，实际为 10 项；当时 36 项子集
  总数（10＋26）是对的。计数错误由外部评审指出。）

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

---

# 【返修 2026-09-20】外部评审驳回 → v2 重做（本节为准）

## 评审驳回（对象 dde420b，基线 f287033）

- **F1/P1（阻塞）**：清单整包过桥撞 8 MiB 桥接输入上限。评审用真实终态 run＋30,000 个小文件
  复现：基线归档并核验 30,006 文件，v1 的归档/archive-show/prune 三个公开入口全部退出 2
  （`bridge input exceeds 8 MiB`）。**主代理独立复现成立**（同脚本同结果）。
- **F2/P3**：为模拟 Python 语义引入的辅助逻辑（通用递归相等、任意真值、big.Rat 数值比较、
  对象键序保留、big.Int 求和）超出业务类型需要；Python 侧还留无赋值键访问模拟异常顺序。
- **F3/P3**：目的地已存在时先做全量 inventory/哈希/SQLite 备份才拒绝——评审实测 v1 多读
  16,829,698 字节（8 次 proof＋2 次逻辑哈希），基线为 0。**主代理独立复现成立**。
- 另指正本记录 test_retention.py 计数错误（12→10，上文已更正）。
- 评审全文与复现脚本：`/private/tmp/g3b-review/review.md`、`many_files.py`、`existing_destination.py`。

## v2 设计（返修合同定死）

总原则：**清单/证明永不整包过桥**——分块（`CHUNK = 2000` 条/次，Go 侧 `retentionChunk`
同值且超限熔断报错）或小事实（bool/计数/字符串）；唯一保留的大载荷是 `canonical(index)`
写 zip 那一次＝基线容量包络。**删除全部语言兼容辅助**改严格类型化＋StrictDecode，顺序由数组
承载。**ARCHIVE_EXISTS 回到 Python 守卫、基线位置**（_terminal 之后、任何 I/O 之前；事实＋
常量类，与 RUN_ARCHIVED 同类）。

操作形状变化：`archive-index-plan` 改骨架（files 由 Python 并入，不再过桥）；`archive-verify`
改"事实（duplicates/format/status_name/names_match）＋分块 entries（manifest 排序序）"单操作，
成功时 checked_archive 返回 Python 本地解析值；`archive-stale` 改两段（集合事实＋分块内容，
db 行比逻辑哈希）；`archive-projection` 载荷改小事实（status/source_directory/file_count）；
新增 `prune-marker`（marker 绑定，基线 203-204 位置）；`prune-plan` 收 names+size 单份
（≈80B/行）；`archive-settled` 的 pid 改类型化 `{pid, pid_valid, probe}`。

## 返修后亲验（主代理，最终树上）

- 评审脚本回归（主代理亲跑）：**many_files.py**——g3b verify PASS 30,006 文件、archive-show/
  archive/prune 三 CLI 全部 exit 0（preview 30003 项）；**existing_destination.py**——拒绝前
  proof 调用 0、读取 0 字节、逻辑哈希 0（v1 为 8/16.8MB/2）。脚本自带的
  `assert not new.zip.exists()` 是按"CLI 必败"写的收尾守卫，修复后按设计触发，属评审脚本
  前提过时，非产品缺陷（证据行在其之前已全部打印）。
- `test_retention*.py` 44 项 OK（既有 10 原样＋差分 33＋**新增 scale 1**：30k 文件全链
  archive→verify→status→preview→prune→readback，实测 22.5s）；全套与差分见本轮索引。
- `gofmt` 干净、`go vet` 零输出、全仓 go test / Python 全套 / 差分子集见索引当轮记录。
- 模型调用 0。

## v2 有界差异（在 v1 九项之上替换/新增）

- v1 第 4 项（ARCHIVE_EXISTS 时机后移）**已消除**——守卫回基线位置。
- v1 第 5 项（checked_archive 包装序角点）**形式变化**：verify 事实化后，format/status/files
  的自然访问仍在 Go 判定之前，"错 format 且缺 status 键"双重故障角点仍报包裹 KeyError
  （单故障行为与基线一致）。
- v1 第 8 项（腐坏 marker/status 缺键）**大幅收窄**：marker 与 index 的键访问全部回到载荷
  构造处的自然访问，缺键的裸 KeyError 语义与基线一致。唯一残余：**腐坏归档**的 status 投影
  本身缺 run_id/revision 键时，基线在读投影时抛裸 KeyError，v2 由 Go 判 ARCHIVE_BINDING
  拒绝（仅手工构造的畸形归档可达）。
- v1 第 9 项（bool↔int 相等性）**已消除**——类型化比较不再有该歧义（bool pid 由 pid_valid
  事实显式排除）。
- **新增**：verify entries 按 `sorted(files)` 序（基线为 manifest 文档序；本工具产出的 manifest
  经 canonical 排序，二者一致——仅手工构造的非排序 manifest 上 content-differs 的具名顺序
  可能不同）；纯构造损坏输入（float size、多余 proof 键、类型损坏值）走桥接硬错误而非基线
  内容拒绝文案（对抗性输入 P3 范畴）；prune-plan 返回的 marker 计划不含 archive_proof，由
  prune_run 写入前本地并入（落盘 marker 与基线一致，格式归属轻微拆分）。
- 容量包络（如实声明）：与基线一致——最大单次桥接载荷为 `canonical(index)` 一份
  （30k 文件实测 ≈3.5MB；上限约 7 万文件级，基线同样受此约束）；prune-plan 的 names+size
  单份约 80B/行（30k≈2.4MB）。`CHUNK` 上限由 Go 侧 `chunkLimit` 熔断保护，防止未来回归。
