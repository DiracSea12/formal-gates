# Blackbox QA cases (run: phase-2-persistence-protocol)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: 通过阶段 2 登记的独立 engine-harness smoke 入口创建并提交 engine state，四字段 envelope、独立 definitionDigest 与 testkit package binding 持久化到隔离状态文档（范围 1 正向）。
procedure: 前置=候选源树 $CAND_ROOT、独立输出目录 $BIN、全新隔离项目 $PROJECT；按 refactor-plan/stage-records.md 阶段 2 节执行 mkdir -p "$BIN" "$PROJECT/host-config" "$PROJECT/state" "$PROJECT/resources" "$PROJECT/stable/state" "$PROJECT/stable/run"，将 BIN 和 PROJECT 规范化为绝对路径；在候选源树执行 go build -o "$BIN/formal-gates-candidate" ./cmd/formal-gates 与 go build -o "$BIN/engine-harness" ./internal/engine/testkit/cmd/harness；设置 FORMAL_GATES_TEST_PROJECT="$PROJECT"、FORMAL_GATES_HOST_CONFIG="$PROJECT/host-config"、FORMAL_GATES_ENGINE_STATE="$PROJECT/state"、FORMAL_GATES_ENGINE_RESOURCES="$PROJECT/resources"；在 PROJECT 中依次执行 "$BIN/formal-gates-candidate" --help 与 "$BIN/engine-harness" --scenario smoke 一次，使用 smoke 的合法提交输出创建 engine state；随后执行 find "$PROJECT/engine-state" -type f | sort，读取 "$PROJECT/engine-state/state.json"。留存 smoke stdout/rc、状态文档原始字节、shasum -a 256 "$CAND_ROOT/definitions/workflow.json" 输出，以及 refactor-plan/route-matrix.md 阶段 2 表登记的 fixture package binding D=sha256:testkit；不使用未登记的 harness run 命令。
oracle: "$PROJECT/engine-state/state.json" 必须是合法 JSON；其版本 envelope 恰含四个契约字段且逐项为 writer=="engine"、stateSchemaVersion==1、workflowDefinitionVersion==2、packageDigest=="sha256:testkit"，四者均非空；definitionDigest 是 envelope 之外的独立非空字段，且等于 "sha256:" 加上对候选源树 definitions/workflow.json 原始字节计算的 SHA-256；状态字节和 find 清单显示该提交实际写入 engine-state/state.json，且无 state.json.intent、随机 .tmp 或 write.lock 残留。definitionSource 不是当前四字段 envelope 契约，不得作为通过条件。失败信号=smoke 未创建状态、JSON 不可读、四字段缺失/为空/不精确、packageDigest 不等于 sha256:testkit、definitionDigest 与复算值不等，或出现部分写入/残留文件。
review status: PASS

CASE-013
mode: blackbox
description: envelope 四字段（writer、stateSchemaVersion、workflowDefinitionVersion、packageDigest）逐项缺失时一律 `UNSUPPORTED_RUN_VERSION` 写前拒绝、绝不写状态（范围 1/8 失败路径）。
procedure: 前置=同上候选安装与 `$STATE`；入口=harness（登记入口与负向场景开关，route-matrix.md 阶段 2 绑定列/stage-records.md 阶段 2 节登记）；操作=以 harness 登记的 envelope 构造开关分别构造四个缺失形态（分别去 writer、去 stateSchemaVersion、去 workflowDefinitionVersion、去 packageDigest）触发引擎写/加载-再写，每个形态指向一个全新目标路径，另对一个已有合法状态的既有目标重放一次；对每个形态记录退出码/错误输出与目标路径字节数、既有目标的 `shasum -a 256` 前后值。留存=四组输出与摘要对照表。
oracle: 每个形态均被拒绝：错误文本含 `UNSUPPORTED_RUN_VERSION` 且点名所缺字段；全新目标 0 bytes（未创建内容），既有目标 sha256 前后一致（未被截断/改写）。失败信号=任一缺失形态被放行、写出任何字节、或错误未含 `UNSUPPORTED_RUN_VERSION`。
review status: PASS

CASE-014
mode: blackbox
description: envelope 不精确匹配（writer 非 engine、schema/definition 版本改一位、definitionSource/Digest 篡改）逐形态写前拒绝 `UNSUPPORTED_RUN_VERSION`（范围 1 失败路径）。
procedure: 前置=同上；入口=harness（同上解析）；操作=以合法 envelope 为基线构造四个篡改副本：`writer` 改为 "validate"、`stateSchemaVersion` 改为其他值（如加一）、`workflowDefinitionVersion` 改为其他值、`definitionDigest` 改为 `sha256:`+64 个 0；逐个触发引擎写，目标为全新路径与既有合法状态各一轮；记录退出码、错误、字节数与 sha256。留存=四形态对照表。
oracle: 每个篡改形态拒绝且错误含 `UNSUPPORTED_RUN_VERSION` 与被改字段名（expected/got 两侧值可辨）；全新目标 0 bytes；既有目标字节不变。失败信号=任一不精确匹配被接受或发生写入。
review status: PASS

CASE-015
mode: blackbox
description: envelope packageDigest 执行绑定（范围 9 本阶段校验侧）：envelope 的 packageDigest 与实际执行 runtime 安装 identity 不一致时写前 `UNSUPPORTED_RUN_VERSION` 拒绝；一致时放行（对照）。PackageDigest 计算与「只改实现 digest 分离」的另一半属阶段 3+，不作判据。
procedure: 前置=同上，并从 stage-records.md 阶段 2 节读取登记的候选 installed/package digest 记作 D；入口=harness（同上解析）；操作=构造仅 packageDigest 不同的两份 envelope：一份取 D、一份取 `sha256:`+64 个 0（其余六字段与合法基线完全相同），分别触发引擎写（全新目标各一）；记录退出码、错误、字节数。留存=两轮对照与 D 出处。
oracle: 取 D 的写入成功（目标为合法 JSON 且 packageDigest==D）；取伪 digest 的拒绝，错误含 `UNSUPPORTED_RUN_VERSION` 且指向 packageDigest（expected==D、got==伪值可辨）、目标 0 bytes。失败信号=伪 digest 被接受、或合法 D 被拒绝、或错误未指向 packageDigest。
review status: PASS

CASE-024
mode: blackbox
description: 旧版本 envelope 写入被拒：以低于当前制品声明版本的合法历史版本组合写 engine 状态 → `UNSUPPORTED_RUN_VERSION`、绝不写（范围 8 失败路径）。
procedure: 前置=同上；先读候选树 `definitions/workflow.json` 的 `version`/`stateSchemaVersion` 当前值；入口=harness（同上解析）；操作=构造 envelope 取旧版本组合（当前 definition version 减一档、如当前为 2 则取 1；definitionDigest 同步取该旧版本历史值，可从 git 历史 `git -C $CAND show <历史提交>:definitions/workflow.json` 复算；无历史值时以「版本号旧一档+任意 digest」形态替代），definitionSource 不变，触发引擎写到全新目标；记录退出码、错误、字节数。留存=当前版本值、历史复算命令与输出、拒绝输出。
oracle: 拒绝，错误含 `UNSUPPORTED_RUN_VERSION` 且 expected 侧为当前版本值、got 侧为所构造旧版本值；目标 0 bytes。失败信号=旧版本 envelope 被接受写入（出现旧版本 run）。
review status: PASS

CASE-029
mode: blackbox
description: envelope 字段空白边界：空串与纯空格填充的 envelope 字段按缺失处理，写前拒绝（范围 1 边界）。
procedure: 前置=同上；入口=harness（同上解析）；操作=对四个 envelope 字段分别构造空串 "" 与纯空格 " " 两形态（共 8 个），definitionSource/Digest 取合法值，逐个触发引擎写到全新目标；记录每轮退出码、错误、字节数。留存=8 形态结果表。
oracle: 8 个形态全部拒绝，错误均含 `UNSUPPORTED_RUN_VERSION` 且对应字段 observed 为空（trim 后视为缺失）；目标全部 0 bytes。失败信号=任一空白形态被当作合法值放行（如纯空格被静默 trim 后接受并写出）。
review status: PASS

CASE-030
mode: blackbox
description: diagnose 对独立构造的不支持版本 JSON 只读报告，不依赖拒绝写入产物，不修改输入或目录。
procedure: 前置=候选 $CAND_BIN、独立 $TP 和 $FIXTURE；用 printf 或等价临时驱动直接构造合法 JSON fixture，写入 writer=engine、stateSchemaVersion=999、workflowDefinitionVersion=999、packageDigest=sha256:qa-unsupported，不执行拒绝写入场景；入口=$CAND_BIN workflow diagnose --path $FIXTURE；操作=前后采集 fixture sha256/mode/mtime 与 $TP find -type f | sort，记录 rc/stdout。留存=fixture、摘要、元数据、目录清单和报告 JSON。
oracle: rc==0；jsonReadable==true；detectedVersions 四字段逐项等于 fixture；supported 含当前四字段 envelope；integrity/建议含 UNSUPPORTED_RUN_VERSION 与 rebuild 指引；fixture 元数据和目录清单不变且无新文件。失败信号=引用拒绝路径产物、正常 loader 拒绝诊断、报告漏字段/改字段或发生写入。
review status: PASS

CASE-031
mode: blackbox
description: `diagnose` 常见操作失误三形态（不存在的路径、malformed JSON、把目录当 path）稳定失败且零写入（范围 8 失败路径/常见误操作）。
procedure: 前置=`$CAND_BIN`、`$TP` 内临时 fixture：不存在的文件路径、内容为截断 JSON 的文件、一个目录路径；入口=`$CAND_BIN workflow diagnose --path <各 fixture>`；操作=逐个执行记录 rc 与输出；前后 `find $TP | sort` 与各 fixture sha256 比对。留存=三组输出与清单。
oracle: 不存在路径→非 0 退出且错误含读取失败语义（无 panic）；malformed JSON→报告 `jsonReadable`==false 且建议含 rebuild 语义、进程正常返回；目录路径→非 0 或明确错误（不 panic、不把目录当状态读）。三种形态均零文件写入（清单与摘要不变）。失败信号=panic、静默成功、或任何文件被写出。
review status: PASS

CASE-063
mode: blackbox
description: engine full 终态清理完成后只保留带版本 terminal summary；关闭进程后由独立 query-terminal 与 terminal-replay 从该 summary 回落并返回 Complete，查询与重放不恢复活动状态也不写入（范围 1 终态/边界正向）。
procedure: 前置=候选源树 $CAND_ROOT、独立输出目录 $BIN、全新隔离项目 $PROJECT；按 refactor-plan/stage-records.md 阶段 2 节执行 mkdir -p "$BIN" "$PROJECT/host-config" "$PROJECT/state" "$PROJECT/resources" "$PROJECT/stable/state" "$PROJECT/stable/run"，将 BIN 和 PROJECT 规范化为绝对路径，并在候选源树执行 go build -o "$BIN/engine-harness" ./internal/engine/testkit/cmd/harness；设置 FORMAL_GATES_TEST_PROJECT="$PROJECT"、FORMAL_GATES_HOST_CONFIG="$PROJECT/host-config"、FORMAL_GATES_ENGINE_STATE="$PROJECT/state"、FORMAL_GATES_ENGINE_RESOURCES="$PROJECT/resources"；在 PROJECT 中执行 "$BIN/engine-harness" --scenario full --project-root "$PROJECT"，把 stdout/stderr/rc 保存到 PROJECT 之外；full 进程结束后保存 find "$PROJECT" -type f | sort、terminal-summary.json 字节/sha256、engine-state/state.json 是否存在及 state.json.intent/.tmp/write.lock 是否存在；再分别启动新的独立进程执行 "$BIN/engine-harness" --scenario query-terminal --project-root "$PROJECT" 与 "$BIN/engine-harness" --scenario terminal-replay --project-root "$PROJECT"，仍将报告保存到 PROJECT 之外，并复采同一目录清单与摘要。
oracle: full 必须成功；full 后 "$PROJECT/terminal-summary.json" 存在且为合法 JSON，包含 writer、stateSchemaVersion、workflowDefinitionVersion、packageDigest 四字段、最后 request/event acceptance 和 canonical digest；活动 "$PROJECT/engine-state/state.json" 不存在，state.json.intent、随机 .tmp、write.lock 也不存在。query-terminal 和 terminal-replay 必须均成功，均从同一带四字段版本绑定的 terminal-summary 回落并报告 NextResult.Kind==Complete/等价 Complete 语义；两次报告中的终态身份、revision、最后 receipt/acceptance 与 summary 一致。query-terminal 和 terminal-replay 前后 terminal-summary 字节、sha256、mode/mtime 与项目文件清单完全不变，且不重新创建活动 state。失败信号=没有清理活动 state、summary 缺版本绑定或最后 acceptance、任一独立查询不返回 Complete、查询改写 summary/目录、创建活动 state/intent/tmp/lock，或用未登记的 cleanup fault point、WorkspaceCleanupReceipt/cleanup registry 作为执行前提。
review status: PASS

CASE-064
mode: blackbox
description: 无版本 terminal summary 在独立只读查询和读取后推进入口中均拒绝，不当作当前状态且零写入。
procedure: 前置=候选 $CAND_BIN 与全新 $VALID_PROJECT、$INVALID_PROJECT；入口=登记 harness 的 full、query-terminal、terminal-replay；操作=在 VALID 执行 $HARNESS --scenario full --project-root $VALID_PROJECT，复制 terminal-summary.json 到 INVALID 并用 jq 删除 writer/stateSchemaVersion/workflowDefinitionVersion/packageDigest；记录摘要；先执行 query-terminal，再执行 terminal-replay，前后采集 summary 字节、sha256、mode/mtime 和项目清单。留存=构造命令、两轮 rc/stdout、前后快照。
oracle: query-terminal 与 terminal-replay 均含 UNSUPPORTED_RUN_VERSION，不返回 Complete；可读报告给出 rebuild 建议；副本摘要、元数据和目录清单完全不变，无 state/intent/temp/lock 新文件。失败信号=任一入口接受/改写无版本 summary、或省略读取后推进验证。
review status: PASS

CASE-065
mode: blackbox
description: 阶段 0 future envelope 契约面回归：`workflow future generate` 仍产出合法 envelope，非法 envelope 的 `future write` 仍写前拒绝且目标 0 bytes（回归面）。
procedure: 前置=`$CAND_BIN` 与候选树 `$CAND`；操作=`$CAND_BIN workflow future generate --root $CAND`（rc=0、记录 envelope JSON）；从合法 envelope 复制构造四份篡改文件（writer→"validate"、stateSchemaVersion→改值、workflowDefinitionVersion→改值、definitionDigest→`sha256:`+64 个 0），逐个 `$CAND_BIN workflow future write --root $CAND --path /tmp/qa-f<nn> --envelope <篡改文件>`；`ls -l` 各目标字节数；最后以未篡改 envelope 写一个对照目标。留存=五轮命令与输出。
oracle: generate rc=0 且 envelope 六字段与候选制品声明一致；四个篡改形态各 rc=1、错误含 `UNSUPPORTED_RUN_VERSION`、目标 0 bytes；对照写入 rc=0 且文档含完整 envelope。失败信号=阶段 2 改动破坏既有 future 契约（非法 envelope 被写、或合法 generate 失败）。
review status: PASS

CASE-066
mode: blackbox
description: legacy run 与 engine 并存互不干扰：legacy run 按当前格式正常创建/读写（无 envelope 不被拒绝），engine 全场景运行后 legacy run 字节零变化，`workflow diagnose` 对 legacy run 的基线报告不变（回归面）。
procedure: 前置=`$TP`；操作=①`$CAND_BIN` 按 stable 文档创建一个 legacy run（bootstrap→`workflow start`）并 `workflow show` 确认可用，采集其 state sha256/mtime；②对它执行 `$CAND_BIN workflow diagnose --run-id <run>`；③运行 harness 全场景（engine 侧）；④复采 legacy run state 的 sha256/mtime、`find $TP/.gates | sort` 清单、再次 show。留存=四步对照。
oracle: ①legacy run 创建成功（无 envelope 写入不被拒——阶段 0 承诺不变）；②diagnose rc=0、`integrity`=="unsupported" 语义、建议含 `UNSUPPORTED_RUN_VERSION` 与 no version envelope 语义、state 字节/mtime 不变；③④legacy run state 的 sha256/mtime 逐字节不变（engine 未迁移/未回写）、清单中 legacy run 文件集不变、show 仍 rc=0。失败信号=legacy 写入被新增的 envelope 校验拒绝、engine 改写 legacy 状态、或 diagnose 基线报告变化。
review status: PASS

CASE-067
mode: blackbox
description: 原子保存与完整性摘要：一次含本地副作用的提交后状态文档完整、摘要可按声明算法复算相等、无临时文件残留、新进程重载与磁盘字节一致（范围 2 正向）。
procedure: 前置=harness（同上解析）与 `$STATE`；操作=执行一次含 fake VCS 副作用操作的提交场景；`find $STATE -type f | sort` 对照交付声明的文件布局（无 `.tmp`/partial/备份残留）；读取状态文档，按交付在 stage-records 阶段 2 节声明的完整性摘要字段与算法对外部字节复算比对；再以独立进程用 harness 的只读加载场景重载该状态。留存=目录清单、状态字节、复算命令与值、重载输出。
oracle: 状态文档为合法 JSON；声明的完整性摘要字段值==按声明算法对相应字节复算的值；目录清单与声明布局精确相等（多余/缺失即 FAIL）；重载报告的 revision/身份与磁盘文档字段一致。失败信号=摘要复算不等、临时/残留文件存在、或重载与磁盘内容不一致。
review status: PASS

CASE-068
mode: blackbox
description: 单调 revision/CAS 基线：连续合法提交使状态 revision 严格 +1 递增、无跳变无回退（范围 2 正向）。
procedure: 前置=harness（同上解析）；操作=连续执行 5 次互不相同的合法提交（每轮后读状态文档 revision 字段并记录），得到序列 r0..r5；留存=六次读数与命令日志。
oracle: 序列严格单调且每次增量恰为 1（r_{i+1}==r_i+1），最终值==首次+5；每轮状态均为完整合法 JSON。失败信号=任一轮 revision 不变、+2、跳变或回退。
review status: PASS

CASE-069
mode: blackbox
description: 原子保存崩溃窗（临时文件写入后、sync/replace 前）：原状态字节不变、未半提交，重启对账后可继续（范围 2 边界）。
procedure: 前置=harness（同上解析）与其登记的确定性注入开关；操作=执行一次提交并在「临时文件写入后、replace 前」注入点终止 harness 进程；立即采集 `$STATE` 全树 `find | sort`、状态文件 sha256、临时文件存在性；重启 harness 执行对账/继续提交；复采同三项并核对 revision。留存=崩溃前后两轮快照与重启输出。
oracle: 崩溃后原状态文件 sha256 与提交前一致（新内容未半提交）、状态仍可被加载（revision==旧值）；重启对账后临时文件清零（清单与声明布局精确相等）、继续提交成功且 revision==旧值+1。失败信号=崩溃后状态文件被部分替换/撕裂、重启后残留临时文件、或 revision 回退。
review status: PASS

CASE-070
mode: blackbox
description: 原子保存崩溃窗（replace 后、intent/commit 清理前）：重启后 observe/reconcile 确认新 revision 已生效且对应副作用不重复执行（范围 2 边界）。
procedure: 前置=harness（同上解析）与 fake VCS 操作计数输出；操作=执行一次含 fake VCS 副作用的提交，在「replace 完成、intent/commit 清理前」注入点终止；记录 fake VCS 该操作计数（应已为 1）；重启 harness 对账；复采状态 revision、pending intent 清单、fake VCS 计数。留存=两轮计数与状态对照。
oracle: 重启后状态为新 revision（replace 已生效、不被回滚）；fake VCS 该操作计数保持 1（对账不重放副作用）；pending intent 清单为空（清账完成）；后续提交可正常推进。失败信号=对账重复执行副作用（计数变 2）、新 revision 被回滚、或 intent 残留。
review status: PASS

CASE-071
mode: blackbox
description: 独立合法 engine state 的完整性摘要篡改被检测，recover 拒绝且不修复/迁移。
procedure: 前置=候选 $CAND_BIN 与全新 $BASE_PROJECT、$TAMPER_PROJECT；入口=登记 harness smoke/recover；操作=在 BASE 执行 $HARNESS --scenario smoke --project-root $BASE_PROJECT 生成 engine-state/state.json，复制到 TAMPER；只改 payload 一个字符，保持 JSON、四字段 envelope、definitionDigest 原样，记录 sha256；在 TAMPER 执行 recover，另在未篡改 BASE 执行 recover 对照，留存输出、摘要、mode/mtime 和目录。
oracle: 篡改副本 recover 非零且错误含 STATE_INTEGRITY_MISMATCH 或明确完整性摘要不匹配 token；篡改字节和元数据不变，无修复/迁移/temp 产物；未篡改对照成功。失败信号=接受篡改、自动改写、错误不可区分或对照失败。
review status: PASS

CASE-072
mode: blackbox
description: 跨进程文件锁并发提交串行化：两进程对同一 run 并发提交不交错、不死锁，终态一致（范围 2 正向+失败路径）。
procedure: 前置=harness（同上解析）；操作=记录起始 revision R；同时（`&` 并发启动、60 秒上限）两个 harness 进程对同一 run 提交两个不同合法事件；结束后读状态 revision 与两事件是否均在账、`find $STATE -type f | sort`（无锁残留文件）；若交付声明 LOCK_HELD/busy 拒绝语义，则被拒方按声明重试一次。留存=两进程输出、终态字节、耗时。
oracle: 60 秒内两进程均返回（无死锁/崩溃）；终态二选一且一致：两事件均在账且 revision==R+2，或后到方得到文档化 lock/busy 错误且重试后在账、revision==R+2；状态文件为完整合法 JSON（无交错半提交）；无锁文件残留。失败信号=死锁超时、状态撕裂、revision 异常、或锁文件残留。
review status: PASS

CASE-073
mode: blackbox
description: 持锁进程崩溃后锁可恢复不死锁：提交中段杀死持有锁的进程，下一提交可在有限时间内继续并保持状态一致（范围 2 边界/中断恢复）。
procedure: 前置=harness（同上解析）；操作=启动一次较长的提交场景并在其临界区中段以 `kill -9` 终止（或用登记的「锁持有中崩溃」注入点）；采集 `$STATE` 清单与状态 sha256；立即启动下一次合法提交（限时 60 秒）；结束后复采三项并核对 revision 单调。留存=两轮快照、第二次提交输出。
oracle: 第二次提交在限时内成功（锁被释放/恢复，不永久阻塞）；状态为完整 JSON 且 revision 较崩溃前不回退；无锁残留。失败信号=后续提交永久阻塞、或恢复后状态损坏/revision 回退。
review status: PASS

CASE-074
mode: blackbox
description: 过期 revision 的 CAS 拒绝：以已被超越的 revision 提交被拒且不改写现行状态；以当前 revision 重试成功（范围 2 失败+正向）。
procedure: 前置=harness（同上解析）；操作=进程 A 读取 revision R 后挂起（按 harness 提供的场景控制）；另一提交使 revision→R+1；A 以 R 触发提交→记录错误与状态 sha256（应为 R+1 内容）；A 重新读取 R+1 后重试提交→记录结果与最终 revision。留存=三轮输出与摘要。
oracle: 以 R 提交被拒（错误含 revision/CAS 语义，expected R+1 got R 可辨）、状态字节保持 R+1 内容（未被旧提交覆盖）；重试成功且 revision==R+2。失败信号=过期 revision 覆盖新状态（丢失更新）、或 CAS 拒绝后状态被改写。
review status: PASS

CASE-075
mode: blackbox
description: 并发 CAS 恰一胜者：两进程同读 revision R 并发提交不同事件，恰一个成功；失败方零写入、重观察后重试成功，两事件终局都在账（范围 2 边界）。
procedure: 前置=harness（同上解析）；操作=两进程以相同起始状态快照（revision R）同时提交不同合法事件；记录两进程结果；失败方重新加载状态后重试其事件；终局核对 revision 与两事件落账、状态完整性。留存=全程输出与终态字节。
oracle: 首轮恰一进程成功（revision==R+1，内容为胜者事件）、另一进程 CAS 失败且其事件未部分落盘；重试后 revision==R+2 且两事件均在账、状态为完整 JSON。失败信号=两进程都成功于同 revision（CAS 失效）、失败方留下部分写入、或终局丢事件。
review status: PASS

CASE-076
mode: blackbox
description: external fingerprint 重验：锁内重验发现外部事实已漂移时释放锁重算，不依据过期事实提交；落账状态记录的外部 fingerprint 等于新事实（范围 2 失败路径）。
procedure: 前置=harness（同上解析）与 fake VCS 的 HEAD/fingerprint 漂移场景开关；操作=对照轮：无漂移执行一次依赖外部事实的提交，记录落账 fingerprint；漂移轮：在引擎观察后、锁内重验前让 fake VCS 推进 HEAD（用登记开关/时序控制），观察引擎行为，终局读取状态记录的外部 fingerprint/identity 与 fake VCS 实际 HEAD、以及基于旧 HEAD 的副作用操作计数。留存=两轮对照输出。
oracle: 漂移轮引擎不提交基于旧 HEAD 的结果（该副作用操作计数==0），重算后落账的外部 fingerprint==fake VCS 新 HEAD；对照轮正常提交且 fingerprint==当时 HEAD。失败信号=基于过期 HEAD 的结果被提交、或落账 fingerprint 与实际 HEAD 不符。
review status: PASS

CASE-077
mode: blackbox
description: expectedTasks/Attempt/pendingActions 落账：推进到签发一个 agent 任务后，状态文档外部可读出完整 expected 任务清单、该 TaskKey 的当前 Attempt 与 pendingActions[actionID]（范围 3 正向）。
procedure: 前置=harness（同上解析）；操作=执行「创建 run→推进至签发一个 agent 任务」的场景；读取状态文档，逐项列出：expectedTasks 键清单（与场景声明的任务集对照）、目标 TaskKey 的当前 Attempt 标识与状态、`pendingActions` 的键集合与对应 actionID 的已签发参数。留存=状态字节摘录与场景声明清单。
oracle: 三类结构齐备且相互一致：expectedTasks 清单内容与场景声明逐一相等；目标 TaskKey 恰有一个当前 Attempt（标识非空、状态为声明签发态）；pendingActions 含该 actionID 键且参数与签发输出一致。失败信号=任一结构缺失、任务清单与声明不符、或 actionID 与签发参数错配。
review status: PASS

CASE-078
mode: blackbox
description: 幂等 submit（同 ID 同 digest）：重复提交返回稳定 acceptance/status，不重新签发已 ISSUED 的 SpawnRequest，revision 不重复 +1（范围 3 正向）。
procedure: 前置=harness（同上解析）与 fake host spawn 计数输出；操作=提交一个外部事件 E（记录首次响应字节与 spawn 计数、revision）；以完全相同的 event ID 与 payload 连续重Submit 两次；每次记录响应、spawn 计数、revision。留存=三次响应与计数表。
oracle: 两次重复提交的响应与首次 acceptance 语义等价（稳定返回，不报错不换结果）；fake host 对该任务的 spawn 调用计数保持首次提交后的值（不再+1）；revision 较首次提交后不因重复提交递增（或按声明的幂等 no-op 语义精确不变）。失败信号=重复提交报错、重复签发 SpawnRequest（spawn 计数增加）、或 revision 被幂等重放推高。
review status: PASS

CASE-079
mode: blackbox
description: 同 ID 不同 digest 硬拒绝：同一 event ID 携不同 payload 提交被硬拒绝且零状态变化（范围 3 失败路径）。
procedure: 前置=harness（同上解析）；操作=先合法提交事件 E（ID=x，payload P1）成功；构造同 ID=x、payload P2（digest 不同）的提交；记录错误、状态 sha256、revision 前后对照。留存=两轮输出与摘要。
oracle: 第二次提交硬拒绝（错误含 digest/mismatch 语义并给出两侧 digest 可辨）；状态 sha256 与 revision 与第一次提交后完全一致。失败信号=同 ID 不同 payload 被当作幂等重放接受、或任何状态变化。
review status: PASS

CASE-080
mode: blackbox
description: 非法事件三形态可区分拒绝：未知 event kind、payload 不合 schema、非当前节点的合法事件各自被拒且错误可区分、零写入（范围 3 失败路径）。
procedure: 前置=harness（同上解析）；操作=构造三份提交：①kind 为声明枚举外的字符串；②合法 kind 但 payload 缺必填字段/类型错误；③结构与 schema 合法但归属其他节点/阶段的事件；逐个提交并记录错误文本；每轮后采集状态 sha256。留存=三组输出与摘要。
oracle: 三形态均拒绝且错误互可区分（分别点名 kind/schema/节点归属语义）；三次状态 sha256 与提交前一致（零写入）。失败信号=任一形态被接受、或三形态报同一种含糊错误无法区分。
review status: PASS

CASE-081
mode: blackbox
description: freshness 校验：被新签发取代的旧 freshness token 提交被拒且零状态变化；当前 token 放行（范围 3 失败+正向）。
procedure: 前置=harness（同上解析）；操作=获取某 availableAction/request 的 freshness token T1；触发重新签发使 token 更新为 T2（按场景声明方式）；以 T1 提交→记录错误与状态 sha256；以 T2 提交同决定→记录结果。留存=两轮输出与摘要。
oracle: T1 提交被拒（错误含 stale/freshness 语义）且状态字节不变；T2 提交接纳成功、决定落账。失败信号=旧 token 仍有效（freshness 未强制）、或 T1 拒绝时产生状态变化。
review status: PASS

CASE-082
mode: blackbox
description: 独立 interruption 场景建立新 Attempt 后，旧 Attempt 迟到 worker result 以 OBSOLETE_RESULT 拒绝；同 ID 重放不改变新 Attempt。
procedure: 前置=全新 $PROJECT 与 fake worker；入口=登记 interruption/worker-result harness；操作=执行 $HARNESS --scenario interruption --interruption nontransient --project-root $PROJECT，保存报告的 TASK_ID、OLD_ATTEMPT_ID、NEW_ATTEMPT_ID、OLD_RESULT_ID 和 revision；用 worker-result 入口以旧 task/attempt/result ID 提交 PASS，再用相同 ID/digest 重放；每次读取 Attempt、expectedTasks、ledger、revision。留存=两轮输出和状态摘录。
oracle: 第一次非零且错误精确含 OBSOLETE_RESULT；当前 Attempt 仍为 NEW_ATTEMPT_ID，旧结果不完成/覆盖/进入当前 expectedTasks；同 ID/digest 重放保持 OBSOLETE_RESULT 语义且 revision 不增。失败信号=依赖未解析场景、旧结果接纳/双计、重放不可见或改写状态。
review status: PASS

CASE-083
mode: blackbox
description: 两阶段主动控制：受限 `REQUEST_*` 事件先创建 pending Ask，用户决定经 submit 以 request ID 完成；全程无自由 USER_* 直写（范围 3/总需求 §5.14 正向）。
procedure: 前置=harness（同上解析）；操作=经登记的 availableActions/REQUEST_* 场景发起一个主动控制请求；读取状态确认 pending Ask（request ID、选项集）已落账；以该 request ID submit 用户决定；读取状态确认 Ask 关闭、决定生效、后续 NextResult 反映该决定。留存=三步状态摘录与输出。
oracle: 第一步后状态含该 request ID 的 pending Ask 且选项集与声明一致；submit 后同一 request ID 的 pending 条目关闭/消失、决定值可读出、推进路径变化可观察（如对应动作解除阻塞）；无任何绕过 request/freshness 的直接用户事件生效。失败信号=未创建 pending Ask 即生效、决定未落账、或自由 USER_* 事件被接受。
review status: PASS

CASE-084
mode: blackbox
description: SpawnReceipt 统一接纳：fake host 回 SpawnReceipt 后任务进入声明签发态并落账 receipt；同 receipt 重发幂等（范围 4 正向）。
procedure: 前置=harness（同上解析）与 fake host；操作=推进至 Ready 签发 agent 任务；fake host 回 SpawnReceipt（含 actionID/correlation）；读取任务状态与 receipt 落账；逐字节重发同一 receipt 一次；复采任务状态与 receipt 记录数。留存=两轮状态摘录。
oracle: 首次接纳后任务状态为声明签发态（ISSUED/RUNNING 语义）且 receipt 以 actionID 关联落账；重发后任务状态不变、receipt 记录数不增（幂等）。失败信号=receipt 未被接纳（任务滞留）、重复 receipt 产生重复记录或二次状态迁移。
review status: PASS

CASE-085
mode: blackbox
description: capacity=1 时两个 eligible worker task 按 typed result 串行接纳：初始 expectedTasks 保留完整 eligible frontier 但只签发第一项，第一项完成后才自动补位第二项，第二项完成后才进入 Complete（范围 4 正向）。
procedure: 前置=候选源树 $CAND_ROOT、全新隔离项目 $PROJECT、项目外证据目录 $EVIDENCE；执行 CAND_ROOT="${CAND_ROOT:?absolute candidate source root}"、BIN="$(mktemp -d /tmp/fg-case-bin-XXXXXX)"、PROJECT="$(mktemp -d /tmp/fg-case-project-XXXXXX)"、EVIDENCE="$(mktemp -d /tmp/fg-case-evidence-XXXXXX)"，mkdir -p "$BIN" "$PROJECT" "$EVIDENCE"，并将 CAND_ROOT/BIN/PROJECT/EVIDENCE 规范化为绝对路径；在候选源树执行 (cd "$CAND_ROOT" && go build -o "$BIN/engine-harness" ./internal/engine/testkit/cmd/harness)，设 HARNESS="$BIN/engine-harness"。先执行 "$HARNESS" --scenario capacity-refill --capacity 1 --project-root "$PROJECT" > "$EVIDENCE/initial.json"，留存 stdout/rc、"$PROJECT/engine-state/state.json" 原始字节与 shasum；用 jq -e 核对 initial 报告的 scenario=="capacity-refill"、status=="PASS"、NextResult/next 为单个 READY，且 expectedTasks 包含场景声明的完整 eligible frontier（至少含 TASK_1、TASK_2）；同时核对 issued/Ready/pending 与 current Attempt 结构中只有 TASK_1/ATTEMPT_1，TASK_2 在首个 result 前不得进入这些结构，也不得在本轮两个显式 submit-worker 提交前有已接纳的 WORKER_RESULT。不要从 expectedTasks 为 TASK_2 猜测或制造 ATTEMPT_2；从初始报告真实 pendingActions 读取 ACTION_1、TASK_1 和 ATTEMPT_1（read -r ACTION_1 TASK_1 ATTEMPT_1 < <(jq -er '.snapshot.State.PendingActions | to_entries | if length == 1 then .[0].value | [.actionId, .task, .attemptId] | @tsv else error("expected exactly one pending action") end' "$EVIDENCE/initial.json")，attemptId 必须等于 TASK_1 的当前 Attempt 标识），不凭猜测填写任何 ID。对第一项先执行 "$HARNESS" --scenario submit-spawn --event-id capacity-spawn-1 --action-id "$ACTION_1" --provider fake-host --correlation "$ATTEMPT_1" --status SPAWNED --project-root "$PROJECT" > "$EVIDENCE/spawn-1.json"，命令返回后立刻执行 cp "$PROJECT/engine-state/state.json" "$EVIDENCE/spawn-1-state.json"，再执行 jq -e . "$EVIDENCE/spawn-1-state.json" >/dev/null 读取该独立副本，并执行 shasum -a 256 "$EVIDENCE/spawn-1-state.json" | tee "$EVIDENCE/spawn-1-state.sha256"；在任何 submit-worker 前，从 spawn-1-state.json 的完整 expected frontier 只读取 TASK_2 的任务身份，不读取或制造其 action/Attempt（read -r TASK_2 < <(jq -er --arg task "$TASK_1" '.content.expected | map(tostring) | if length == 2 and (index($task) != null) then .[] | select(. != $task) else error("expected exactly two eligible tasks containing TASK_1") end' "$EVIDENCE/spawn-1-state.json")；执行 jq -e --arg action "$ACTION_1" --arg task "$TASK_1" '.acceptance.eventId == "capacity-spawn-1" and .acceptance.kind == "SPAWN_RECEIPT" and .acceptance.status == "ACCEPTED" and .acceptance.actionId == $action and ([.actions[]?.actionID | tostring] | sort) == [$action]' "$EVIDENCE/spawn-1.json"，再执行 jq -e --arg task "$TASK_1" --arg task2 "$TASK_2" '([.next[]? | select(.kind == "READY") | .payload[]?.task | tostring] | sort) == [$task] and ([.next[]? | select(.kind == "READY") | .payload[]?.task | tostring] | index($task2)) == null' "$EVIDENCE/initial.json"，再执行 jq -e --arg action "$ACTION_1" --arg task "$TASK_1" --arg task2 "$TASK_2" '(.content.events["capacity-spawn-1"].acceptance.eventId == "capacity-spawn-1" and .content.events["capacity-spawn-1"].acceptance.kind == "SPAWN_RECEIPT" and .content.events["capacity-spawn-1"].acceptance.status == "ACCEPTED" and .content.events["capacity-spawn-1"].acceptance.actionId == $action) and (.content.spawnReceipts[$action].actionId == $action and .content.spawnReceipts[$action].provider == "fake-host" and .content.spawnReceipts[$action].status == "SPAWNED") and (.content.attempts[$task].actionId == $action and (.content.attempts[$task].bindings.task | tostring) == $task) and ((.content.expected | map(tostring) | index($task)) != null and (.content.expected | map(tostring) | index($task2)) != null) and (([.content.tasks | to_entries[] | select(.value == "ISSUED" or .value == "RUNNING" or .value == "VALIDATING") | .key] | sort) == [$task]) and (([.content.tasks | to_entries[] | select(.value == "ISSUED" or .value == "RUNNING" or .value == "VALIDATING") | .key] | index($task2)) == null) and (([.content.pendingActions | to_entries[] | .value.task | tostring] | sort) == [$task]) and (([.content.pendingActions | to_entries[] | .value.task | tostring] | index($task2)) == null) and ((.content.attempts | keys | map(tostring) | sort) == [$task]) and ((.content.attempts | keys | map(tostring) | index($task2)) == null) and ((.content.results | length) == 0) and ((.content.stagedResults | length) == 0) and (([.content.events | to_entries[] | select(.value.acceptance.kind == "WORKER_RESULT")] | length) == 0)' "$EVIDENCE/spawn-1-state.json"；只有上述 spawn-1.json、spawn-1-state.json、sha256 和全部 jq -e 中间态核对成功后，再执行 "$HARNESS" --scenario submit-worker --event-id capacity-worker-result-1 --action-id "$ACTION_1" --provider fake-host --outcome PASS --payload-digest sha256:capacity-worker-result-1 --project-root "$PROJECT" > "$EVIDENCE/result-1.json"；该命令由 harness 构造并提交 typed payload {actionID:$ACTION_1, provider:"fake-host", outcome:"PASS", payloadDigest:"sha256:capacity-worker-result-1", failureClass:""}，PASS 不传 failure-class。留存两条 stdout/rc、提交后 state.json 字节/sha256、expectedTasks、frontier 和 NextResult，并用 jq -e 核对 result-1 的 acceptance.eventId=="capacity-worker-result-1"、kind=="WORKER_RESULT"、status=="ACCEPTED"、actionId==$ACTION_1、payload digest 为 sha256:capacity-worker-result-1 且 refill 恰有一项；再用独立 jq -e --arg action "$ACTION_1" '.snapshot.State.Results[$action].provider == "fake-host" and .snapshot.State.Results[$action].outcome == "PASS" and .snapshot.State.Results[$action].payloadDigest == "sha256:capacity-worker-result-1"' "$EVIDENCE/result-1.json" 机械核对第一条 typed result 的 provider/outcome/payloadDigest，再用独立 jq -e --arg task "$TASK_2" '([.next[]? | select(.kind == "READY")] | length) == 1 and ([.next[]? | select(.kind == "READY") | .payload[]?.task | tostring] | sort) == [$task] and ([.next[]? | select(.kind == "Complete")] | length) == 0' "$EVIDENCE/result-1.json"；从 result-1 的真实 pendingActions/refill 读取新补位 ACTION_2、TASK_2、ATTEMPT_2（read -r ACTION_2 TASK_2 ATTEMPT_2 < <(jq -er '.snapshot.State.PendingActions | to_entries | if length == 1 then .[0].value | [.actionId, .task, .attemptId] | @tsv else error("expected exactly one refill pending action") end' "$EVIDENCE/result-1.json")），确认 TASK_1 已 TERMINAL，且 TASK_2 的 current Attempt/pending/issued 与 ATTEMPT_2 才在此处首次出现。对第二项先执行 "$HARNESS" --scenario submit-spawn --event-id capacity-spawn-2 --action-id "$ACTION_2" --provider fake-host --correlation "$ATTEMPT_2" --status SPAWNED --project-root "$PROJECT" > "$EVIDENCE/spawn-2.json"，命令返回后立刻执行 cp "$PROJECT/engine-state/state.json" "$EVIDENCE/spawn-2-state.json"，再执行 jq -e . "$EVIDENCE/spawn-2-state.json" >/dev/null 读取该独立副本，并执行 shasum -a 256 "$EVIDENCE/spawn-2-state.json" | tee "$EVIDENCE/spawn-2-state.sha256"；随后执行 jq -e --arg action "$ACTION_2" '.acceptance.eventId == "capacity-spawn-2" and .acceptance.kind == "SPAWN_RECEIPT" and .acceptance.status == "ACCEPTED" and .acceptance.actionId == $action and ([.actions[]?.actionID | tostring] | sort) == [$action]' "$EVIDENCE/spawn-2.json"，以及 jq -e --arg action "$ACTION_2" --arg task "$TASK_2" --arg attempt "$ATTEMPT_2" --arg first "$ACTION_1" '(.content.events["capacity-spawn-2"].acceptance.eventId == "capacity-spawn-2" and .content.events["capacity-spawn-2"].acceptance.kind == "SPAWN_RECEIPT" and .content.events["capacity-spawn-2"].acceptance.status == "ACCEPTED" and .content.events["capacity-spawn-2"].acceptance.actionId == $action) and (.content.spawnReceipts[$action].actionId == $action and .content.spawnReceipts[$action].provider == "fake-host" and .content.spawnReceipts[$action].correlation == $attempt and .content.spawnReceipts[$action].status == "SPAWNED") and (.content.attempts[$task].id == $attempt and .content.attempts[$task].actionId == $action and (.content.attempts[$task].bindings.task | tostring) == $task) and (.content.pendingActions[$action].actionId == $action and .content.pendingActions[$action].attemptId == $attempt and (.content.pendingActions[$action].task | tostring) == $task) and ((.content.results | keys | sort) == [$first]) and (.content.tasks[$task] != "TERMINAL") and (([.content.events | to_entries[] | select(.value.acceptance.kind == "WORKER_RESULT")] | length) == 1)' "$EVIDENCE/spawn-2-state.json"；只有 spawn-2.json、spawn-2-state.json、sha256 和上述全部 jq -e 中间态核对成功后，再执行 "$HARNESS" --scenario submit-worker --event-id capacity-worker-result-2 --action-id "$ACTION_2" --provider fake-host --outcome PASS --payload-digest sha256:capacity-worker-result-2 --project-root "$PROJECT" > "$EVIDENCE/result-2.json"；第二条 typed payload 为 {actionID:$ACTION_2, provider:"fake-host", outcome:"PASS", payloadDigest:"sha256:capacity-worker-result-2", failureClass:""}。留存最终 stdout/rc、state.json 原始字节/sha256、expectedTasks、frontier、NextResult 和完成账，并以 jq -e 核对 result-2 acceptance.eventId/kind/status/actionId 分别为 capacity-worker-result-2/WORKER_RESULT/ACCEPTED/$ACTION_2、两个 TaskKey 均为 TERMINAL、两个 Attempt/result 载荷均各出现一次、expected/Ready/pending 清单为空、NextResult.Kind=="Complete"；所有报告写在 EVIDENCE（PROJECT 外），不使用 go test 或交付内测试文件作为证据。
oracle: initial 阶段必须可机械核对为 capacity=1 且只签发一个 READY/issued task：expectedTasks 必须保留场景声明的完整 eligible frontier，并明确包含 TASK_1 与 TASK_2；但存在且仅存在 TASK_1/ATTEMPT_1 的 current pending action，TASK_2 不得在首个 result 前出现在 issued、current Attempt、pending 或初始 NextResult 中，且在两条显式 submit-worker 命令前没有 WORKER_RESULT 被接纳。第一条 submit-spawn 成功落账后，第一条 submit-worker 必须 rc==0，报告 acceptance.eventId=="capacity-worker-result-1"、kind=="WORKER_RESULT"、status=="ACCEPTED"、actionId==ACTION_1，typed payload 的 provider=="fake-host"、outcome=="PASS"、payloadDigest=="sha256:capacity-worker-result-1"；TASK_1 的 current Attempt 必须进入 TERMINAL 且只完成一次，并且同一 acceptance/refill 恰补位一个新的 ACTION_2/TASK_2/ATTEMPT_2，TASK_1 不得再次签发，TASK_2 的 current Attempt/pending/issued 不得早于第一条 result。第二条 submit-spawn 与 submit-worker 必须同样成功，第二个 acceptance.eventId/kind/status/actionId 必须为 capacity-worker-result-2/WORKER_RESULT/ACCEPTED/ACTION_2，typed payload 的 provider/outcome/payloadDigest 必须为 fake-host/PASS/sha256:capacity-worker-result-2；最终两个 TaskKey 与两个 Attempt 均为 TERMINAL，各结果载荷各出现一次，expectedTasks 中的完整 frontier 已全部完成，expected/Ready/pending 清单为空且 NextResult.Kind==Complete。失败信号=capacity-refill 初始阶段遗漏 TASK_2、初始多于一个 READY/issued、TASK_2 在首个 result 前进入 issued/current Attempt/pending/NextResult、初始阶段已偷偷接纳 worker result、任一命令缺少 action/provider/outcome/payloadDigest 或不能按真实报告绑定当前 Attempt、任一 typed result 未被 ACCEPTED、不补位、重复 TASK_1、第二个 result 前宣布 Complete、结果/Attempt 被重复计数、或最终 NextResult 不是 Complete。 独立中间态附加验收：第一条 submit-spawn 的 stdout 必须写入 EVIDENCE/spawn-1.json；命令返回后必须立刻执行 cp，把 PROJECT/engine-state/state.json 复制到 EVIDENCE/spawn-1-state.json，并保存该副本的 sha256。全部中间态 jq -e 核对必须独立证明 spawn-1 acceptance.eventId/kind/status/actionId 为 capacity-spawn-1/SPAWN_RECEIPT/ACCEPTED/ACTION_1，TASK_1 的 spawn/receipt 记录按真实 action/attempt 关联，expectedTasks 仍包含 TASK_1/TASK_2，issued/Ready/pending/current Attempt 仍只含 TASK_1，TASK_2 不进入这些集合且没有 WORKER_RESULT；第二步 submit-worker 必须明确在独立 state 副本、sha256 和全部中间态 jq -e 核对成功之后才执行。独立中间态失败信号=缺少 spawn-1.json、spawn-1-state.json 或 sha256，acceptance 四字段或 TASK_1 spawn/receipt 记录不匹配，expectedTasks/frontier/issued/Ready/pending/current Attempt 任一集合违反容量=1 约束，TASK_2 提前进入受限集合，中间态已有 WORKER_RESULT，任一 jq -e 核对失败，或第二步 submit-worker 在中间态核对完成前执行。 第二条 submit-spawn 必须先将 stdout 写入 EVIDENCE/spawn-2.json，并在第二条 submit-worker 前独立复制 PROJECT/engine-state/state.json 到 EVIDENCE/spawn-2-state.json、保存 sha256；其 acceptance.eventId/kind/status/actionId 必须通过 jq 机械核对为 capacity-spawn-2/SPAWN_RECEIPT/ACCEPTED/ACTION_2，state 副本中的 spawn receipt 必须绑定真实 ACTION_2、provider=fake-host、correlation=ATTEMPT_2、status=SPAWNED，且 spawn/receipt 与当前 Attempt、pending action 的 action/attempt/task 绑定一致。第一条 result 后 NextResult 必须仍为单个 READY 且仅指向 TASK_2，第二条 result 前不得 Complete；两条 typed worker result 的 provider/outcome/payloadDigest 都必须分别通过独立 jq 机械核对为 fake-host/PASS/sha256:capacity-worker-result-1 与 fake-host/PASS/sha256:capacity-worker-result-2。失败信号=第二条 spawn 缺少独立报告、状态副本或 sha256，spawn receipt 字段或 Attempt 绑定不匹配，第一条 result 后未保持 READY、第二条 result 前提前 Complete，或任一 typed result 的三字段判据缺失/不匹配。
review status: PASS

CASE-086
mode: blackbox
description: Ask 决定事件与 Operator typed observation 的统一接纳落账：Ask 关闭且决定生效；Operator observation 记录且对应对账项进入声明处置（范围 4 正向）。
procedure: 前置=harness（同上解析）；操作=①pending Ask 上 submit 用户决定：核对 pending 条目关闭、决定值落账、frontier 变化；②对登记的 Operator 场景（如需核实的对账事实）submit typed observation：核对 observation 记录入账（清单+1 且内容摘要一致）且对应 pending 对账项按声明转移（关闭/进入下一步）。留存=两段状态前后摘录。
oracle: ①Ask request ID 从 pending 集合消失、决定值与 frontier 变化可读；②observation 记录恰新增一条且绑定原对账项 ID、处置转移与声明一致。失败信号=Ask 决定未关账、observation 丢失或未绑定来源、处置路径与声明不符。
review status: PASS

CASE-087
mode: blackbox
description: HostAction 先持久化 pending intent 再执行、receipt 统一接纳清账；EXECUTE_ADAPTER_OPERATION 引用未注册 operation/自由命令形态被拒且宿主零执行（范围 4 正向+失败路径）。
procedure: 前置=harness（同上解析）与 fake host；操作=①触发一个声明的 HostAction（如 TERMINATE_AGENT）：执行前读状态确认 pending intent（actionID/operation/参数）已持久化，fake host 回 receipt（actionID/operation/payload digest/provider/correlation/status），核对 intent 清账与动作终态；②构造 EXECUTE_ADAPTER_OPERATION 引用未注册 operation id 与自由文本命令两形态提交，核对错误、fake host 执行日志、状态 sha256。留存=两段输出与状态摘录。
oracle: ①执行前 pending intent 存在（先持久化后执行）、receipt 接纳后 pendingActions 该条目清除、动作状态终结且 receipt 公共字段齐备；②两形态均拒绝（错误点名 operation/参数非法语义）、fake host 该操作执行计数==0、状态字节不变。失败信号=未持久化 intent 即执行、receipt 未清账、自由 shell 通道被放行或宿主执行了非法参数。
review status: PASS

CASE-088
mode: blackbox
description: lifecycle 事件统一接纳：成对 start/stop 配对接纳为已验证状态、逐字节重放幂等、未配对/未认领 identity 不验证且不直接改 workflow state（范围 4 正向+失败路径）。
procedure: 前置=harness（同上解析）与 fake host lifecycle 事件注入；操作=①以已绑定 identity 发送成对 start/stop，核对配对/验证状态与观测记录数；②逐字节重放同对事件一次，复采记录数；③发送未配对 stop 与未认领 identity 的事件各一，核对验证状态、workflow state sha256、观测清单。留存=三段输出与摘要。
oracle: ①成对事件接纳且标记配对验证（VERIFIED 等价语义）、观测恰新增对应记录；②重放后记录数不变（duplicate 幂等）；③两形态不验证（REJECTED/未配对语义）、workflow state 字节不变（lifecycle 只写 observation buffer，不直接改 state）。失败信号=未配对/未认领被验证、重放新增记录、或 lifecycle 事件直接改写 workflow state。
review status: PASS

CASE-089
mode: blackbox
description: 越权自由用户事件拒绝：无 pending request/freshness 的 USER_* 形态事件被 submit 拒绝、不产生用户决定效果（范围 4 失败路径/不得误报）。
procedure: 前置=harness（同上解析）；操作=构造不经过 availableActions/REQUEST_* 的自由 USER_* 事件（如 USER_ABORT/USER_DECIDE 形态字符串与 payload）提交两轮；记录错误、状态 sha256、后续 NextResult（应与提交前同型）。留存=两轮输出与摘要。
oracle: 两轮均拒绝（错误含 request/freshness 必需语义）；状态字节不变；NextResult 不出现任何用户决定已生效的形态。失败信号=自由 USER_* 事件被当作有效决定接纳。
review status: PASS

CASE-090
mode: blackbox
description: provider mismatch 硬拒绝不降级 default：fake host 以与 run 绑定不同的 provider identity 回事件/receipt 被硬拒绝，provider 字段保持绑定值（范围 4/总需求 §5.16 失败路径）。
procedure: 前置=harness（同上解析）；操作=读取 run 绑定的 provider identity 值；fake host 以不同 provider identity 发送 SpawnReceipt/lifecycle 事件各一轮；记录错误、provider 字段现值、receipt/事件接纳状态。留存=两轮输出与字段摘录。
oracle: 两轮均硬拒绝（错误含 provider mismatch 语义）；状态中 provider identity 字段保持原绑定值（未降级为 default/未切换）；无任何接纳记录。失败信号=mismatch 被容忍、provider 被静默改写为 default 或来件值。
review status: PASS

CASE-091
mode: blackbox
description: 中断分派①客观瞬态且 bindings 未变：自动 resume 原 Attempt——Attempt 标识不变、无新 Attempt、不重复 spawn、任务最终完成（范围 5 正向）。
procedure: 前置=harness（同上解析）与登记的「客观瞬态」注入开关；操作=任务执行中注入瞬态故障（bindings 保持不变）；观察恢复行为：读取 Attempt 标识/数量、fake host spawn 计数；继续场景至该任务完成。留存=注入前后状态摘录与计数。
oracle: 恢复后该 TaskKey 的当前 Attempt 标识与注入前相同（原 Attempt resume）、Attempt 总数不增、spawn 计数不变（无重复派发）、任务最终达完成态。失败信号=瞬态被升级为新 Attempt、重复 spawn、或任务悬挂不恢复。
review status: PASS

CASE-092
mode: blackbox
description: 中断分派②已知非瞬态/责任变化：自动新 Attempt 且旧 Attempt terminate/stale——旧 Attempt 迟到结果以 `OBSOLETE_RESULT` 拒绝（范围 5 正向）。
procedure: 前置=harness（同上解析）与登记的「已知非瞬态」/「任务责任变化」注入开关；操作=注入后读取该 TaskKey 的 Attempt 列表（新旧标识、旧 Attempt 状态）；以旧 Attempt 身份提交迟到 worker result；记录错误与 Attempt 状态。留存=状态摘录与输出。
oracle: 新 Attempt 建立且成为当前（标识不同）；旧 Attempt 状态转为声明 stale/terminated 值；迟到结果拒绝且错误含 `OBSOLETE_RESULT`。失败信号=旧 Attempt 仍可接纳结果、未建新 Attempt、或旧 Attempt 状态未 stale。
review status: PASS

CASE-093
mode: blackbox
description: 中断分派③bindings 未变但原因未知：产出 Ask 且选项恰为 resume/fresh/abort 语义集，不自动 respawn、不新建 Attempt；选择后按所选推进（范围 5 正向）。
procedure: 前置=harness（同上解析）与登记的「原因未知」注入开关；操作=注入后观察：pending Ask 的出现与选项集、spawn 计数、Attempt 数量；submit resume 选择后核对推进。留存=状态摘录与两段输出。
oracle: 出现 pending Ask 且选项集与 resume/fresh/abort 语义一一对应（无多余选项）；spawn 计数与 Attempt 数量较注入前不变（不盲目动作）；submit 后按 resume 语义恢复推进。失败信号=无 Ask 而自动 respawn/新建 Attempt（猜测）、选项集不符、或选择后未推进。
review status: PASS

CASE-094
mode: blackbox
description: receipt UNKNOWN 对账·唯一匹配：先查 lifecycle，唯一匹配自动 attach——receipt 对账成功、spawn 计数不变、不重复派发（范围 5 正向）。
procedure: 前置=harness（同上解析）与「receipt UNKNOWN+lifecycle 唯一匹配」场景开关；操作=制造 HostAction/spawn receipt 丢失但 lifecycle 存在唯一配对记录的形态；触发引擎对账；核对 receipt 最终状态（attached）、fake host spawn 计数、任务推进。留存=对账前后状态摘录与计数。
oracle: 对账后该 receipt 标记为已对账/attached 且绑定 lifecycle 唯一记录；spawn 计数不变（未重新派发）；任务正常推进。失败信号=唯一匹配仍不 attach（悬挂）、或重复 spawn"保险起见"。
review status: PASS

CASE-095
mode: blackbox
description: receipt UNKNOWN 的多重匹配和无匹配在两个独立 run 中都直接进入 Operator，不 attach 且不 respawn。
procedure: 前置=全新 $MULTI_PROJECT、$NONE_PROJECT、fake host/lifecycle ledger；入口=登记 unknown-receipt 场景；操作=分别执行 $HARNESS --scenario unknown-receipt --lifecycle-matches 2 --project-root $MULTI_PROJECT 与 --lifecycle-matches 0 --project-root $NONE_PROJECT，均记录 UNKNOWN receipt、候选事实、spawn 计数、NextResult.Kind 和 Operator 载荷，不共享状态。
oracle: 两个 run 的 NextResult.Kind 均直接为 Operator；多重载荷列出两个候选且不 attach，无匹配载荷明确为空且不 attach；spawn/respawn 计数均保持 UNKNOWN 前基线，Operator request 可见，不进入 Wait。失败信号=Wait、挑选 attach、respawn 或无 Operator 处置。
review status: PASS

CASE-096
mode: blackbox
description: 副作用 UNKNOWN 的预期事实和冲突事实用两个独立 run：前者只提交结果，后者进入 Operator，均不重复执行。
procedure: 前置=全新 $EXPECTED_PROJECT、$CONFLICT_PROJECT、fake VCS/host、fixture-contract.md adapter baseline；入口=登记 fault、receipt-file、reconcile-host-action；操作 A=在 EXPECTED 用 receipt-file --prepare adapter 创建 pending action，执行 $HARNESS --scenario fault --fault host_commit_after --project-root $EXPECTED，确认副作用计数1；设外部事实为精确预期，执行 reconcile-host-action --fact expected。操作 B=在全新 CONFLICT 重复 prepare/fault，计数从1起；设外部事实为冲突终态，执行 reconcile-host-action --fact conflict。分别留存 intent、receipt、计数、revision、NextResult。
oracle: EXPECTED 只新增一次结果提交，计数仍1、intent 清除、receipt 绑定原 action；CONFLICT NextResult.Kind==Operator，冲突事实进入载荷，计数仍1，不重试/执行/自动提交，intent 按声明保留；两 run ID/action/revision/字节不混用。失败信号=在已清理 intent 上继续冲突、重复副作用、静默提交或缺 Operator。
review status: PASS

CASE-097
mode: blackbox
description: TRANSIENT_ENGINE_ERROR 声明式重试耗尽：重试次数恰等于定义声明的 maxAttempts，耗尽后进入 Wait 或显式失败，不无限重试、零 agent 派发（范围 5/失败分类表边界）。
procedure: 前置=harness（同上解析）与「瞬态引擎错误」连续注入开关、场景声明的 retryPolicy maxAttempts 值 M；操作=连续注入瞬态错误 M+2 次；读取重试计数输出、终态 Kind（Wait/显式失败）、fake host spawn 计数、错误/诊断输出。留存=计数序列与终态输出。
oracle: 重试执行恰 M 次（计数序列长度==M，不超不欠）、之后不再重试；终态为声明二选一（Wait 语义或显式失败+诊断指引）；全程 spawn 计数无新增。失败信号=重试次数≠M、无限重试、或降级为 agent 派发。
review status: PASS

CASE-098
mode: blackbox
description: engine 故障只显式失败并指向 diagnose；仅独立且明文声明 AGENT_RECOVERABLE_SEMANTIC_ERROR 的 run 可按声明产生一个 agent 修复，未声明语义错误不得动态派发。
procedure: 前置=三个全新 $ENGINE_PROJECT、$DECLARED_PROJECT、$UNDECLARED_PROJECT 与 fake host/worker；入口=登记 failure-routing 场景及定义 fixture；操作 A=ENGINE 先留存正常基线 spawn/pending/NextResult/revision，再注入 failure-routing --failure-class INVARIANT_VIOLATION。操作 B=DECLARED 装载明文声明 AGENT_RECOVERABLE_SEMANTIC_ERROR 的定义 fixture，留存基线后注入该语义错误。操作 C=UNDECLARED 装载不声明该 class 的同构 fixture，留存相同基线后注入同一错误。三段均从全新 run 开始。
oracle: ENGINE 错误明确为 INVARIANT_VIOLATION/显式失败并含 diagnose，spawn/Ready、pending、revision 不新增 agent；DECLARED 新增数量精确为1且载荷绑定声明，不得超额；UNDECLARED 不新增 agent/Ready，走显式失败或声明边界错误且不能提示 agent 决定。失败信号=engine 故障降级 agent、未声明派发 agent、声明超额派发或跨 run 代偿基线。
review status: PASS

CASE-099
mode: blackbox
description: result-before-receipt：worker result 先于 SpawnReceipt 到达时暂存不丢弃；lifecycle 对账成功后接纳并推进；全程不重复 spawn（范围 5 边界）。
procedure: 前置=harness（同上解析）与「result 先于 receipt」场景开关；操作=按序：result 到达→读状态（暂存语义、任务未误标完成/未丢弃）、spawn 计数；补齐 SpawnReceipt/lifecycle 配对→读状态（result 接纳、frontier 推进）、spawn 计数。留存=两段状态摘录。
oracle: 暂存阶段 result 可见但不推进 frontier、无错误丢失；配对后 result 被接纳、expectedTasks 完成态落账、frontier 推进；spawn 计数全程==1。失败信号=result 被直接丢弃、未对账即接纳、或补 receipt 时重复 spawn。
review status: PASS

CASE-100
mode: blackbox
description: submit commit 后响应丢失：注入「提交已落账、响应未返回」崩溃，客户端重试同事件 ID 得到等价幂等 acceptance，无重复副作用、revision 不再 +1（范围 5 边界）。
procedure: 前置=harness（同上解析）与该注入点开关；操作=提交事件 E 并在响应返回前终止；记录此时状态（E 已落账的 revision R）；重启后重试同 ID 同 digest 提交；核对响应、副作用计数、revision。留存=两轮输出与计数。
oracle: 重试返回与正常 acceptance 等价的稳定响应（不报错、不重复执行）；副作用计数不变；revision 保持 R（不因重试 +1）。失败信号=重试被当新事件重复执行副作用、或 revision 被推高。
review status: PASS

CASE-101
mode: blackbox
description: 端到端中断恢复：run 推进至中段注入崩溃，重启后恢复推进至终态；已完成前缀的每个副作用恰执行一次、终态 summary 完整（范围 5 边界收口）。
procedure: 前置=harness（同上解析）；操作=驱动 run 至中段（≥2 个任务已完成）；以登记崩溃点（或临界区 kill）终止；重启 harness 恢复；推进至终态；终局核对：fake host/VCS 每操作计数表（逐项==1）、终态 summary 身份键与最后 receipt、`$STATE` 无 intent/锁/临时残留。留存=崩溃点状态、恢复日志、计数表、summary 字节。
oracle: run 达终态且 next 等价面返回 Complete；计数表逐项恰 1（已完成前缀不重放）；summary 含四字段 envelope 与最后 request/event receipt；目录清单与声明布局精确相等。失败信号=恢复后任一副作用计数 2+（重放已完成前缀）、终态悬挂、或残留未清。
review status: PASS

CASE-102
mode: blackbox
description: fake host/worker/VCS 三件套可用性与确定性：无真实宿主/真实 VCS 写即可驱动完整协议序列至终态；同输入两次干净重跑的最终状态与 harness 报告字节一致（范围 6 正向）。
procedure: 前置=harness（同上解析）；操作=清空 `$STATE` 后运行完整协议场景一遍，采集最终状态文件 sha256 与报告输出；再清空重跑一遍；比对两轮全部摘要；确认全程无真实宿主调用与真实 VCS 写（fake 三件套声明即约束，另以场景输出中的 provider/VCS 标识核对为 fake 值）。留存=两轮摘要对照。
oracle: 两轮均成功至终态；对应文件 sha256 两两相等、报告字节相等（确定性）；provider/VCS 标识均为声明 fake 值（无真实宿主/VCS 依赖）。失败信号=第二-run 字节漂移（不确定性）、或场景要求真实宿主/真实 VCS 才能推进。
review status: PASS

CASE-103
mode: blackbox
description: 写路径持久边界注入矩阵：对 {intent 持久化前、intent 后 execute 前、execute 后 observe/reconcile 前、临时文件写后 replace 前、replace 后 commit 前} 逐点注入终止，恢复后状态一致、副作用至多一次、无残留（范围 6 边界）。
procedure: 前置=harness（同上解析）与 stage-records 阶段 2 节登记的注入点清单；操作=对上述五个边界点（以登记清单实际名称为准逐点执行，缺席的登记点记为覆盖缺陷）各执行一轮「注入终止→重启对账→继续提交」；每轮记录：恢复后状态为完整 JSON 且 revision 为旧或新其一、对应副作用操作计数（≤1 且与所提交 revision 一致）、`$STATE` 清单（无 intent/锁/临时残留）。留存=五行逐点结果表。
oracle: 五点全部满足：状态完整无撕裂、revision 二值其一、副作用计数与终局一致（不重放）、清单恢复声明布局。失败信号=任一点恢复后状态撕裂、副作用重复（计数 2+）、或残留未清。
review status: PASS

CASE-104
mode: blackbox
description: 五个边界逐点使用对应操作和 oracle：spawn_after_attach、result_before_receipt、submit_response_lost、并发 submit、旧 Attempt 迟到，不把 duplicate submit acceptance 套到不发生重复提交的点。
procedure: 前置=五个全新项目 P1..P5、候选 harness、fake host/worker/VCS、stage-records 注入点；逐点留存 stdout/状态摘要/revision/Attempt/receipt/副作用：①P1 fault spawn_after_attach 后 recover；②P2 先 worker-result typed result 再 receipt-file SpawnReceipt；③P3 fault submit-response-lost 后以同 event ID/digest 重试一次；④P4 concurrent-submit workers=2 提交两个不同 event，仅 LOCK_HELD/CAS 失败方重试；⑤P5 interruption --interruption nontransient 建立新 Attempt 后提交旧 result。
oracle: ①恢复后 spawn/receipt 各恰一次，不要求重复 submit；②receipt 前 result 在 staged 台账，receipt 后恰结算一次并清 staged，不要求重复 submit；③同 ID/digest 重试稳定 acceptance/status、revision不增、无副作用增加；④失败只能 LOCK_HELD/RevisionConflict，重试后两事件在账且 revision 恰增2，不判幂等 acceptance；⑤旧结果 OBSOLETE_RESULT、不落账、不推进当前 Attempt。任一点重复执行/派发、撕裂、错误混边界即失败。
review status: PASS

CASE-105
mode: blackbox
description: engine 写入只进入隔离状态目录：harness 全场景运行前后，固定稳定环境（用户级 registry、stable 安装树、stable 项目状态）逐字节不变，engine 产物全部位于声明的隔离 namespace（范围 7 正向+失败信号）。
procedure: 前置=harness（同上解析）与候选安装声明的隔离 canonical paths（stage-records 阶段 2 节）；操作=运行前快照：`~/.formal-gates`（含 registry.json）全树 `find -type f | sort`+逐文件 sha256、stable 安装树同采、stable 项目 `.gates` 同采、`$TP` 与 `$STATE` 同采；运行 harness 全场景；复采四处快照比对；列出两轮 `$TP`/`$STATE` 差集并逐项归入声明的隔离 canonical path。留存=四组前后快照与差集归属表。
oracle: 稳定环境三处前后清单与逐文件 sha256 完全一致；engine 新增产物全部落在声明的隔离 canonical paths 集合内（差集逐项可归属）。失败信号=任何 engine 产物落入 stable registry/安装树/稳定项目、或稳定侧任一字节变化。
review status: PASS

CASE-106
mode: blackbox
description: 候选 binary 对稳定 namespace 的写入尝试全部写前拒绝：从 `$CAND_BIN` 对 stable 项目 root 与 stable run 依次尝试 `workflow start`/`workflow resume --run-id <stable run>`/`workflow seal`，全部拒绝且 stable 侧零新文件（阶段 0 fencing 回归）。
procedure: 前置=`$CAND_BIN`、stable 项目根与 stable run id（本 run 环境）；操作=对 stable 项目 root 快照后依次执行上述三个写命令（参数按 stable 文档），记录各 rc 与错误；复采 stable root `find | sort` 与逐文件 sha256 比对；随后仅在 `$TP` 用 `$CAND_BIN` 正常执行一处写操作作对照（应成功且落 `$TP`）。留存=三轮输出与前后快照。
oracle: 三个写命令均非 0 退出且在写入前结束（错误含 admission/namespace/登记语义）；stable root 前后清单与摘要完全一致；对照写成功且仅落 `$TP`。失败信号=候选在 stable 项目创建 `.gates/tmp`/状态/registry 写入、或对照写失败。
review status: PASS

CASE-107
mode: blackbox
description: 候选 namespace disjoint proof：安装清单 canonical paths（host home/config、state root、resource registry、runtime sibling）与固定稳定环境、开发 worktree 零重叠，Lstat/realpath 无 symlink 回指（范围 7 正向）。
procedure: 前置=阶段 2 收口段的候选隔离安装；入口=`$CAND_BIN package validate --root <候选安装根>`（与收口段 baseline receipt 若有则一并执行 `package baseline`）；操作=执行 validate/baseline 记录 manifest（canonical paths、digest、disjoint proof 字段）；对候选安装树执行 `find -type l`（应为空）与 realpath 抽查（不解析到开发 worktree/stable 区）。留存=receipt/manifest、symlink 扫描输出。
oracle: validate rc=0；manifest 覆盖声明 canonical paths 且 disjoint proof 成立（与 stable 环境、开发 worktree 路径零交集）；`find -type l` 输出为空；realpath 无回指。失败信号=任一 canonical path 与 stable/开发区重叠、存在 symlink、或 validate 失败。
review status: PASS

CASE-108
mode: blackbox
description: 候选 installed binary 的 legacy 正常路径回归：lightweight 闭环、二段确认 abort、package validate、canary portable 均按 stable 文档工作且产物只落候选 namespace（范围 7 回归面）。
procedure: 前置=`$CAND_BIN`、`$TP`；操作=①隔离 bootstrap 后 `workflow start`（lightweight 路线参数按 stable 文档）→`workflow show`→`workflow requirement --confirmed`→`workflow seal`，记录各 rc 与产物路径；②另起一 run 验证 abort 二段确认：缺确认参数应拒、带 `--user-confirm` 终止并产出 `.gates/results/<run>.json` 且 `.gates/tmp/<run>` 清理；③`$CAND_BIN package validate --root <候选安装根>`；④`$CAND_BIN canary portable`。留存=各步 rc、产物清单、canary 报告。
oracle: ①各步 rc=0、seal 后 `.gates/results/<run>.json` 存在且 tmp 清理；②缺确认拒绝、确认后 ABORTED summary 落盘、tmp 清理；③rc=0；④rc=0 且 0 FAIL；全部产物仅落 `$TP`/候选 namespace（对照用例 51 快照）。失败信号=任一 legacy 入口行为偏离 stable 文档、产物越出候选 namespace。
review status: PASS

CASE-109
mode: blackbox
description: 固定 stable driver 不受影响：本 run 期间 stable installed binary 只读 smoke 通过、安装树无指向开发 worktree 的 symlink、SKILL.md 指纹跨采集一致（范围 7 回归面）。
procedure: 前置=阶段 1 记录的冻结 stable 安装路径；入口=stable installed binary 只读命令；操作=①`<stable>/formal-gates workflow show --root <主工作区> --run-id phase-2-persistence-protocol`（本 run 由 stable 驱动的直接证据）；②stable 安装树 `find -type l`（应为空）；③`shasum -a 256 <stable>/SKILL.md` 两次采集（间隔执行若干其他用例后）比对。留存=smoke 输出、扫描输出、两次指纹。
oracle: ①rc=0 且输出本 run 状态；②符号链接清单为空；③两次 SKILL.md sha256 相等。失败信号=stable 入口失败、安装树出现 symlink 指向开发 worktree、或指纹漂移（stable 被开发内容污染）。
review status: PASS

CASE-110
mode: blackbox
description: 公开 `workflow drive`/`submit` 仍显式 unsupported：`$CAND_BIN` 上两命令均 rc=1 且 `unknown workflow subcommand: drive|submit`，拒绝路径零写入（范围 10 回归+失败路径）。
procedure: 前置=`$CAND_BIN`、`$TP`；操作=快照 `$TP` 后执行 `$CAND_BIN workflow drive --root $TP` 与 `$CAND_BIN workflow submit --root $TP`（各附最小参数），记录 rc、stderr；复采 `$TP` 快照比对。留存=两轮输出与快照。
oracle: 两命令各 rc=1、stderr 分别含 `unknown workflow subcommand: drive` 与 `unknown workflow subcommand: submit`；`$TP` 前后清单与摘要零变化（拒绝路径不写任何状态）。失败信号=任一命令被路由执行（阶段 3 面提前泄漏）、或拒绝路径产生文件。
review status: PASS

CASE-111
mode: blackbox
description: 阶段 1 canonical 制品 freshness 回归：候选树上 `go run ./cmd/gen-definition` 重生成 checked-in 制品与身份常量零 diff（阶段 1 交付面不被阶段 2 改动破坏）。
procedure: 前置=候选树 `$CAND`（主工作区当前快照）；入口=`go run ./cmd/gen-definition`（文档化生成器用法，在 `$CAND` 根执行）；操作=执行后 `git -C $CAND status --porcelain -- definitions internal/engine/definition/identity_gen.go`。留存=命令输出与 status 输出。
oracle: 生成器 rc=0；status 输出为空（checked-in `definitions/workflow.json` 与 `identity_gen.go` 重生成逐字节一致）。失败信号=出现 M/A 状态行（阶段 2 改动造成制品漂移或人工双写）。
review status: PASS

CASE-112
mode: blackbox
description: NextResult 六类阶梯与 Operator 分支均由 harness 直接观察，Operator 不以交叉引用代偿。
procedure: 前置=全新 $SEQUENCE_PROJECT、$OPERATOR_PROJECT、候选 harness；入口=登记 next-sequence 和 unknown-receipt；操作=在 SEQUENCE 执行 $HARNESS --scenario next-sequence --project-root $SEQUENCE_PROJECT，逐边界保存 Kind、payload 键和 revision；在独立 OPERATOR 执行 $HARNESS --scenario unknown-receipt --lifecycle-matches 2 --project-root $OPERATOR，直接读取 NextResult.Kind 与 Operator payload，不接受状态 request 交叉引用。
oracle: SEQUENCE 按登记场景直接输出 Ready、Ask、Wait、HostAction、Complete，若该场景包含 Operator 也必须直接输出 Operator；每个 Plan 恰一个 Kind，只有对应 payload 非空；OPERATOR NextResult.Kind 必须直接等于 Operator，payload 含两个候选且无其他 Kind 混载。失败信号=Operator 只在别处出现、缺 Kind/顺序错、混载或未知 Kind。
review status: PASS

CASE-113
mode: blackbox
description: 唯一写入者（legacy→engine 方向）：`$CAND_BIN` 的公开 legacy 写命令指向 engine run 所在隔离 namespace 时不改写 engine 状态文件（engine run 不由 legacy 命令续跑/改写）。
procedure: 前置=`$CAND_BIN`、`$STATE` 中一个活动 engine run（harness 产物）；操作=采集 engine 状态文件 sha256/mtime；依次以 `--root` 指向 `$STATE` 所在项目执行 `workflow requirement --confirmed --run-id <engine run>`、`workflow reset --run-id <engine run>`、`workflow abort --run-id <engine run> --user-confirm` 三个 legacy 写命令；记录各 rc/错误；复采 sha256/mtime。留存=三轮输出与前后摘要。
oracle: 三命令均不成功改写（拒绝/无可操作 run 的错误可见），engine 状态文件 sha256 与 mtime 前后一致。失败信号=任一 legacy 命令改写/重建 engine 状态文件（跨 runtime 双写/续跑）。
review status: PASS

CASE-114
mode: blackbox
description: route-matrix.md 阶段 2 绑定列活文档义务：workflow 面与维护面两表的「阶段 2 绑定」列逐行非空且取值合法，drive 行 unsupported、submit 行公开 unsupported 并如实标注 harness 内部协议面（范围 10 机械核对）。
procedure: 前置=候选交付已更新 `refactor-plan/route-matrix.md`；操作=读取两表，逐行提取「阶段 2 绑定」列值并列表；核对 drive/submit 两行的阶段 2 取值与 harness 标注；与实际公开面交叉：用例 56 的拒绝结果、用例 54 的 legacy 行为、harness 入口登记（本组解析源）作为矩阵-实际一致性对照。留存=逐行取值清单与三处交叉引用。
oracle: §2.3 枚举的全部 workflow 行与维护面行的「阶段 2 绑定」列均非空且取值属声明枚举（legacy/install-bootstrap/Shadow 诊断/unsupported/harness 内部协议面标注）；drive 行==unsupported、submit 行==公开 unsupported+harness 内部协议面如实标注；三处交叉与矩阵声明一致。失败信号=列存在空缺/非法取值、drive/submit 被写成已实现公开面、或矩阵与实际公开面矛盾。
review status: PASS

CASE-115
mode: blackbox
description: stage-records.md 阶段 2 节同构追加义务：存在「阶段 2」章节且七项结构齐备（编号/run ID 与 sealed commit、身份与摘要、stable 与候选摘要、矩阵与唯一 writer、证据、cleanup、下一阶段 base），并登记 harness 调用方式与注入点清单（本组用例的解析源）。
procedure: 前置=候选交付已更新 `refactor-plan/stage-records.md`；操作=读取该文件，逐项核对阶段 2 节含上述七项子结构（与阶段 0/1 节同构）；确认其中登记了 protocol/recovery harness 的调用方式与确定性注入点/场景开关清单（本组用例解析所引用）；「不可考」项必须显式标注原因。留存=章节结构清单。
oracle: 阶段 2 节存在且七项子标题/内容齐备；harness 调用方式与注入点清单在其中可解析（非空、具体到可直接照做）；无未标注原因的缺项。失败信号=章节缺失、七项缺一、harness/注入点清单缺席（导致本组解析无从进行）。
review status: PASS

CASE-116
mode: blackbox
description: 成比例性影响评估文档逐项可追溯，并证明触及该文档的提交本身是纯文档，不假设新增提交，也不以混合批次回归代偿。
procedure: 前置=候选 $CAND、登记 BASE_SNAPSHOT/CANDIDATE_SNAPSHOT；操作=在 refactor-plan 定位含受限 submit、availableActions freshness token、新 run 三主题的评估文档 $DOC，逐项读取小额修复走正门的影响结论；执行 git -C $CAND diff --name-status $BASE_SNAPSHOT..$CANDIDATE_SNAPSHOT -- $DOC 和 git -C $CAND log --follow --format=%H $BASE_SNAPSHOT..$CANDIDATE_SNAPSHOT -- $DOC（不使用 --diff-filter=A）；对范围内每个触及提交执行 git show --format= --name-status $COMMIT 读取完整路径集合，并执行 git diff --name-only $BASE_SNAPSHOT..$CANDIDATE_SNAPSHOT -- internal/engine cmd 对照。留存文档、三项摘录、diff/follow、每个提交全路径和 engine 对照。
oracle: 文档非空，三项各有明确成本/比例结论；可由修改或历史跟踪定位，不要求新增提交；每个触及文档的提交完整路径集合只含 .md 文档路径，不得含 internal/engine、cmd、测试或其他生产行为路径；任一混合提交直接 FAIL，不用其他回归用例补偿。失败信号=文档缺失/仅列名、错误假设新增、路径不全或文档提交夹带引擎行为。
review status: PASS

CASE-117
mode: blackbox
description: 失败分类表 BUSINESS_REJECT（声明业务边）可观察：提交违反定义声明业务规则的事件被分类拒绝且错误可区分、零状态变化（范围5 失败分类表）
procedure: 前置=harness（同组解析）与登记的声明业务边场景开关；操作=按场景声明构造一个违反定义声明业务规则的事件形态（payload 结构合法但业务规则拒绝）提交；记录错误文本、状态 sha256 前后对照；另以 TRANSIENT 形态（或既有瞬态用例输出）作分类对照。留存=两轮输出与摘要。
oracle: 业务边形态拒绝且错误含 BUSINESS_REJECT 语义、可与 SCHEMA/TRANSIENT 类错误区分（错误文本分类可辨）；状态 sha256 前后一致（零写入）。失败信号=业务边事件被接纳、或错误分类不可区分。
review status: PASS

