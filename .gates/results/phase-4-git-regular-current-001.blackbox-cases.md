# Blackbox QA cases (run: phase-4-git-regular-current-001)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

shared step: env
procedure: 仅在执行阶段操作快照后实际安装的候选，不在设计阶段运行产品。使用阶段需求所列 candidate 根目录下新建的独立测试根 T；P=T/projects/<用例序号>，R=bb-<用例序号>，E=T/evidence/<用例序号>。将阶段验证清单指定的精确已提交候选安装到 T/install；FG 为安装回执指向的绝对 launcher/binary，PKG 为同回执的 package-root；宿主配置、registry、state、资源根均置于 T，不能软链到开发或稳定环境。保存安装回执、FG 绝对路径及摘要、package/installed identity、git rev-parse HEAD；需要实测安装或发布的用例重新执行自己的安装步骤，不能以此准备代替。执行前创建 E，所有调用以 argv 数组启动，记录 argv、工作目录、输入文件、stdout、stderr、exit code、开始/结束时间；真实宿主用例另留原生会话/调用日志及其独立观测来源。每个独立用例使用干净 P，不能复用前一用例失败状态。
review status: PASS
review reason: 隔离根、独立 P/R/E、精确安装身份及 argv/stdout/stderr/退出码取证边界明确；既有安装/bootstrap 的公开帮助和文档可用，本阶段候选身份按执行时实际回执绑定。

shared step: protocol
procedure: 使用注入的当前接口说明 references/engine-regular.md 与 references/user-configuration.md 规定的公开命令；验收行为仍只以当前确认 master-requirements.md 为准。FG/P/R/E 来自 env。下列简称都有完整命令：START = [FG,"workflow","start","--root",P,"--run-id",R,"--vcs","git","--requirement","requirements.md"]；DRIVE = [FG,"workflow","drive","--root",P,"--run-id",R]；READ(x) = [FG,"workflow",x,"--root",P,"--run-id",R]，x 为 show/status/next/diagnose；READ(coverage) 从实际 show 的 coverage/guarantee 投影读取，不新增接口；CONFIG = [FG,"workflow","diagnose","--root",P,"--config"]。SUBMIT = [FG,"workflow","submit","--root",P,"--run-id",R]，把完整 JSON event 文件通过 stdin 输入，禁止拼成 shell 字符串。REQUEST 具体发送 {"ID":"<R>-event-<递增序号>","Kind":"REGULAR","regular":{"operation":"REQUEST","expectedRevision":<刚读取 revision>,"request":{"action":<下述明确动作名>,<该例参数>}}}。先实际 show/next 取得 availableActions 与当前 revision；REQUEST 后 show 的 regular.ask 给出 id/token/digest/options，再按公开 DECIDE schema提交 choice 及从 ask.id 复制的 decision.requestId、原 token/digest和当前 expectedRevision。禁止用聊天普通“继续”代替 typed 决定。动作名按当前说明：confirm-requirement、mode、topology、route、finding、review-again、authorize-round、authorize-retry、requirement-change、apply-requirement、plan-change、qa-suggestion、qa-revise、development-rework、preparation-retry、verification-retry、authorize-wave、continue、jump、reset、adopt-external、route-add、route-reselect、abort。HOST = [FG,"workflow","drive","--root",P,"--run-id",R,"--action-id",<实际签发ID>]，恢复在同一 argv 末尾加 "--resume"。每次以 argv 数组实际调用并记录；事件外层 ID/Kind 严格用当前文档拼写，regular 内结果 payload 使用文档所指 REVIEW_RESULT、PREPARATION_RESULT、VERIFICATION_RESULT、WAVE_ADJUDICATE、REPAIR_RESULT、SOURCE 等封闭类型。执行准备保存 FG/workflow 各公开入口 --help、随包 Event/DECIDE/结果 schema 和最小例子为 E/entry-contract.json，用实际 schema 填本例全部已指定语义字段，保留原 actionID/TaskKey/Attempt/provider/revision/candidate/digest/scope/case-set。未明文冻结的 payload 序列化字段不猜测；交付若没有足够公开 schema使请求无歧义可编码，记录具体接口缺口，不调用内部 API或降级旧writer。负例只改该例明确指定字段。比较业务投影、revision、回执、有效结果及真实调用数；rejected receipt 可以追加，业务状态是否变化按逐例 oracle 判断。安装/维护的既有入口使用随包 install --help/说明给定的完整实参并记录到同表，所有破坏性操作只在隔离目标。 所有用例中的“请求/申请 Seal”“请求 promotion”“执行 Git adapter”均指实际 DRIVE 触发当前已签发的程序步骤并读取结果，不表示存在 REQUEST seal/promotion、workflow seal/snapshot 或 Git HOST 动作；HOST --action-id 只用于真实已签发 worker。有限错误的处理固定为：提交 RUNTIME_ERROR/无效结果后结算该 attempt 的 SOURCE，重新 READ/DRIVE 取得同 TaskKey 下一 attempt；默认两次，第三次必须先在当前 Ask 通过对应 authorize-retry（产品/技术 reviewAction）、preparation-retry（准备 target=TaskKey）或 verification-retry（验证/修复 target=TaskKey）并 DECIDE confirm/reason 获一次授权。普通 continue 只按公开合同用于 TOKEN_CAP，不拿它代替 retry 授权。非专门验证耗尽的多行负例，每行使用新 P/R 从同一合法前态独立准备，不把第三个无效样本提交在已耗尽任务上。无效结果的成功退出仅代表 receipt 已记录，须核对 REJECTED_INVALID_RESULT/RUNTIME_ERROR、finished/attempt 和有效列表；不能把 exit 0 当业务 PASS。相反未知命令必须为非零，不可用 result rejection 代替命令删除。准备前态时可用文档 attach_fixture/settle_fixture 方法：复制当前 claim → CLAIM → SOURCE RUNNING(identity=isolated-fixture:<actionId>) → 相应完整结果 → SOURCE UNAVAILABLE(reason=明确隔离前置无原生日志)；每次从 show 读取新 revision。真实宿主目标禁止使用这些 fixture 身份或 source 作为真实观测。处理 wave 必须等全部 targets 终结且 source 结算，再逐 target 提交 WAVE_ADJUDICATE；REPAIR_RESULT 后由 DRIVE 真正 Git 提交并自动计算 scope/P2_CARRY，不能提交底层 carry/ImpactSet/candidate 账目。 最终公开接口以同步自 3ce59286117f3be5e3f2241aad440442227ce2bd 的 references/engine-regular.md 为准；README、formal-flow/development-plan/example-run/sliced-runs 只作使用说明。公开 workflow 恰为 start/show/status/next/drive/submit/diagnose/guard-preview，无别名；同篇旧段落的 generic cleanup/preview 提法不代表保留独立命令。产品/技术默认各三轮有效语义审查，每 TaskKey 两 attempt。新 TASK_OPERATION_CLAIM/taskOperation 与 TASK_OPERATION_RECEIPT/taskOperationReceipt 由实际 bridge 产生，具体使用 task-operation 共同说明。设计中的准备/执行引用必须使用该最终 schema；正式外层 driver 的 CASE 编号和生命周期仍以原派发契约为准。
review status: PASS
review reason: 八个入口、REQUEST/DECIDE、结果载荷、内部操作与公开协议一致，绑定读取及留证顺序明确。

shared step: fixture
procedure: 非真实宿主/真实链路目标的高成本前态允许作为隔离测试材料构造：在 P 初始化普通 Git 仓库，git init、git config user.name QA、git config user.email qa@example.invalid；写 requirements.md，采用本次需求相同的“### REQ-001 / #### 要求 / #### 验收条件 / - AC-001”结构：REQ-001 要求问候程序对 Alice 输出 Hello Alice，AC-001 正常输入；REQ-002 要求空输入输出 usage 且退出 2，AC-002 空输入、AC-003 -h 帮助。写 README.md 和 seed.txt，git add requirements.md README.md seed.txt，git commit -m 'QA baseline'，保存 base B。调用 START 后按 protocol 完成预检、全文展示、精确确认、初选 regular、产品审 PASS、技术审 PASS、no-split 理由和 full/custom 选择，均在隔离测试 run 中显式标记“模拟前置事实”；不得伪造成阶段正式审查或真实宿主结果。下文 S0=刚 START 未确认；S1=需求确认但未选择模式；S2=regular 产品审完成、技术审未通过；S3=技术审和 no-split 已确认、尚未选路线；S4=路线和已确认开发计划就绪；S5=全部开发完成、黑盒批准、必要白盒完成、交付物已装配且候选 Q 已冻结、case-set FROZEN；S6=完整 wave 收齐、错误已处置、Operator 已完成、无义务、尚未 Seal。可用公开前序调用到达；成本高时可从正常可达状态生成隔离 fixture，保存完整构造脚本、输入与逐字段前置说明，由实际安装程序首次读回确认阶段和绑定。构造不得预置本例待测事件的决定、输出、receipt、PASS、调用记录或中断恢复结果，不得编辑本阶段正式 run。每次复制 fixture 必须另建独立 P/R 并使用相应原生身份/快照；真实身份、真实调用、真实安装、实际 Git、完整 canary 均必须实跑，禁止用构造替代。所有证据区分 preparation 与 observed。 可编码的默认 toy 规格补全为：REQ-001/AC-001，greet Alice stdout 严格 Hello Alice 加换行、stderr 空、exit=0；REQ-002/AC-002，无参数 stdout 空、stderr 严格 usage 加换行、exit=2；AC-003，-h 的 stdout 严格 usage 加换行、stderr 空、exit=0。各例变更此规格时明确覆盖原句及预期，不用“错误输入”含糊推导 FAIL。Go fixture 的 go.mod 为 module example.invalid/qa-greet 与 go 1.20（不依赖第三方）；main.go 的 main 仅执行 os.Exit(run(os.Args[1:],os.Stdout,os.Stderr))；contest.go 提供 greet(name string)(string,error)：name 空返回 error("usage")，否则返回 "Hello "+name，以及 run(args []string,out,err io.Writer) int：首参数 -h 向 out 打印 usage 并返回0；无参数向 err 打印 usage 并返回2；其他情况向 out 打印 greet(args[0]) 结果并返回0。greet_test.go 分别定义 TestGreetAlice（字符串精确等于 Hello Alice 且 error=nil）、TestGreetEmpty（必须 error 且 Error()==usage）、TestHelp（bytes.Buffer 调 run(["-h"],out,err)，逐一断言 exit=0、out==usage\n、err 为空）。如用例需要有缺陷的前态，只改它明示的目标行为/断言，记录真实文件 diff。非真实开发目标的 S4→S5 准备仍实际操作 adapter 登记的 development workspace：用上述明确程序写 main.go/contest.go/go.mod/greet_test.go，运行 go build -o <E>/greet . 与 go test ./...，返回这些文件的 manifest 和本次真实 Subtask validation，再结算 SOURCE 后 DRIVE 真实 track/commit。不得把 E/greet 二进制放进待提交 manifest。blackbox-design 的 a/b/c 即 case-set 中三个 ID，publicEntry 为 E/greet，各 steps 分别以 argv [E/greet,"Alice"]、[E/greet]、[E/greet,"-h"] 实跑，oracle 为前述字节/exit，b 的 exit=2 是预期正常错误处理而非 QA FAIL；a/b 共用一次 go build 非验收准备。独立项目副本及 cwd 仍逐 case 分开。full 路线白盒三个 testRef 分别为 greet_test.go::TestGreetAlice、greet_test.go::TestGreetEmpty、greet_test.go::TestHelp；先实际运行每个 go test -run ^<函数名>$ -count=1，再按当前 design/reuse 合同登记 fixture 前置，已有充分测试时 manifest=[]。准备阶段的 review/preparation/wave 结果仍明确标 fixture，仅被测步骤本身、真实构建/Git/安装/原生宿主观测来自实际运行。S5 在所有已选 QA 的 design/review 完整且 FROZEN、真实候选装配完成后取得；S6 的预置验证结果必须逐 target 完整结算并逐 finding 裁决，停止在真正 promotion 执行之前。本例若测试真实 QA/独立审查/canary，不能使用 fixture 的模拟结果替代这些待测调用。 在初始基线中另加入 qa-entry.py（仅为隔离 QA 启动适配器，不是待测输出 oracle），git add 时包含它；文件内容为下面 Python 程序：
import os,subprocess,sys,tempfile
with tempfile.TemporaryDirectory(prefix="qa-greet-") as td:
    binary=os.path.join(td,"greet")
    build=subprocess.run(["go","build","-o",binary,"."],capture_output=True)
    if build.returncode:
        sys.stdout.buffer.write(build.stdout);sys.stderr.buffer.write(build.stderr);sys.exit(build.returncode)
    runs=[["Alice"],["-h"]] if sys.argv[1:]==["--qa-alice-and-help"] else [sys.argv[1:]]
    for args in runs:
        actual=subprocess.run([binary]+args,capture_output=True)
        sys.stdout.buffer.write(actual.stdout);sys.stderr.buffer.write(actual.stderr)
        if actual.returncode:sys.exit(actual.returncode)

它只构建当前 cwd 对应候选并转发真实子进程输出/退出码，不比对预期，也不合成 PASS。因 go run 会包装非零退出，本例使用此已声明 argv 实测真正 binary 的 stdout/stderr/exit=2。下文普通黑盒执行使用 task-operation 的 prepare/target/collect；原先 E/greet 的直接 argv 是 toy CLI 语义与准备自测说明，正式 QA command 以 case-set 为准。
review status: PASS
review reason: Go/Python/Git 可用，toy 输出与退出码明确；构造仅用于非待测前态，真实 Git、宿主和目标执行分开。

shared step: host
procedure: 凡 oracle 涉及真实 Codex 创建、恢复、身份或调用次数，使用隔离配置接入真实 Codex provider 和产品公开 bridge。先从原生宿主可查询的会话列表、转写或实际调用审计采集 before，再执行产品入口，再从同一独立原生来源采集 after；用 actionID/原生 session ID/时间窗口关联，分别统计 create/resume/model/QA target 调用。零调用不能只引用 wrapper 自报、结果拒绝或手写计数器。正常新 worker 收到 engine 生成的规范提示词文件薄指针；保存规范文件原文、文件绑定和真实消费路径。需要中断时只终止隔离测试的调用进程或使用宿主正常取消入口，不破坏正式 run；记录中断点与实际 native receipt，UNKNOWN 时先公开 observe/reconcile，不能凭猜测重做。
review status: PASS
review reason: 当前有 Codex CLI 0.145.0、ChatGPT 登录、exec --json、resume、app-server 和保存会话 delete 能力；原生 identity/转写可留证，合法调用对照与 before/after 方法明确。该共享批准不代表模型/QA 子入口矩阵或特定崩溃窗已具备，差异在各 case 单独判。

shared step: case-set
procedure: 在 fixture 的初次需求确认 REQUEST 中登记 requiredSources=[{"sourceId":"requirements.md","category":"PRODUCT_REQUIREMENT","applicability":"QA"},{"sourceId":"design-notes.md","category":"SOLUTION_CONSTRAINT","applicability":"NON_QA"}]；两个文件先真实创建并纳入基线，后者仅写入独立说明“此项目仅作隔离协议测试，不是正式审查结论。”，同时核对其原文作为非 QA 证据。DOC-001 是下述用例对 design-notes.md 的人类简称，协议 sourceId 始终使用实际相对路径。REQ/AC 的唯一正式投影仍来自 requirements.md，不将 NON_QA 用于 AC。被测 toy 的完整设计按 references/engine-regular.md 的 design.manifest 结构提交：mode=BLACKBOX/WHITEBOX；points 为 <MODE>:AC-001、<MODE>:AC-002、<MODE>:AC-003；a/b/c 对应调用方在此隔离设计中明确定义的 <MODE>:A、<MODE>:B、<MODE>:C。它们是被测协议里的 fixture ID，不是本次外层 driver 的 CASE ID；外层新增 case 仍由 driver 分配。bindings 复制 requiredSources.binding，reviewKind/topologyScope/routeScope 按公开例子填入；sources=["requirements.md","design-notes.md"]；a→AC-001、b→AC-002、c→AC-003，每条 edge.sourceId="requirements.md"；a/b 引用 sharedSteps 中 setup，c 不引用。blackbox 的 publicEntry、preconditions、steps 与 oracle 分别写实际可执行路径/输入/命令及输出比较；whitebox 用唯一实际 testRef。alternatives 给 design-notes.md 一项，reason/method 说明读取该文件与上述完整原文比较，status=PASS、evidence 为实际读取结果，不能拿历史 PASS 充当证据。保存 PREPARATION_RESULT 返回的完整 design，再按该次 reviewScope 逐 source/point/case/shared ID 提交 PREPARATION_RESULT.review 中对应 PASS/FAIL 数组，不遗漏任何独立项。后续引用以已接纳 design 返回的相同 ID 为准，whitelist/candidate/manifest digest 仅从公开投影读取，不手算或伪填。每例保留完整 inventory、manifest、map、whitelist、candidate、逐项审查前后列表；实际 execution 的 procedure/output/comparison/tool/environment/workspace 全部来自当次运行。 每个 regular case 的 command 必填并随用例集冻结。toy BLACKBOX:A/B/C 分别为 {executable:"python3",args:["qa-entry.py","Alice"],expectedExitCode:0}、同 args=["qa-entry.py"]/expectedExitCode:2、同 args=["qa-entry.py","-h"]/expectedExitCode:0；dependencies 默认 []，特殊例明确指定。WHITEBOX:A/B/C 的 command 分别 {executable:"go",args:["test","-run","^TestGreetAlice$","-count=1"],expectedExitCode:0} 及另外两函数，cwd 始终由 adapter 选的精确候选干净副本；不冻结 E/greet 这个来自旧候选的外部二进制路径。首次 routeScope 按文档公开算法对当前 route 的 kind/qa/gates 顺序对象做两空格 JSON、末尾换行、UTF-8、不转义 <>&，SHA256 加 sha256:；已有 design 则直接复制其 binding。 最终黑盒 publicEntry/steps 同步声明为 python3 qa-entry.py 的上述实际 argv（a 的多行为特例另写 --qa-alice-and-help）；原准备自测 E/greet 不作为冻结执行入口。
review status: PASS
review reason: source/point/case/schema、冻结 argv、routeScope 和独立执行目录都有公开依据。

shared step: publish
procedure: 所有 Seal 前发布、失败回退、卸载、旧 generation 和故障注入仅在 env 隔离安装目标；正式 stage-4 固定 driver 原路径及身份不变。RELEASE_ROOT 是隔离回执实际绑定的 release 根。为每次演练记录 STABLE=该隔离安装回执的稳定 owner launcher、CAND=已提交且通过包校验的精确候选源、PROJECT=独立目标项目；它们都是绝对路径，STABLE 与正式 driver、真实全局 launcher 不同。候选源先以 [CAND+"/bin/formal-gates","package","validate","--root",CAND] 只读验证；旧 owner 97eef33d 的跨版本安装必须先按 history-release-recovery 准备维护包并由同一 STABLE 安装维护包，之后才安装 CAND；维护仅修改隔离旧源，正式 driver 在 Seal 前不动。实际切换 argv=[STABLE,"install","--source",CAND,"--host","codex","--scope","project","--project",PROJECT,"--binary-target",STABLE,"--release-root",RELEASE_ROOT,"--force"]，允许的隔离配置根使用此次 bootstrap/安装回执实际绑定的目录，不自造 CLI 选项、不改真实用户配置。安装写必须由 STABLE native owner 执行，候选 binary 只作其受测读校验。保存 registry 旁 install.json、transaction journal、failure/recovery receipt 和实际 installed binary 的 package validate/smoke 输出。发生中断后去掉故障 env，用同一 install argv 重启（或明确卸载时 [STABLE,"uninstall","--host","codex","--scope","project","--project",PROJECT]），现有 installer 自动对账同一 journal；没有独立 workflow reconcile/cleanup 写入口，也不手写 pointer/registry 或手删 release。发布责任人按 scope QA/canary→正式固定 driver Seal→集成/必要重验→现有安装事务切换→实际 launcher 核验/历史同表回归→发布完成的顺序归档证据；“发布完成”是此交付流程的核对结果，不另造 publish-complete 命令或第二套 receipt 协议。演练的前序凭据只能标 rehearsal，不伪造本轮正式 Seal。正式全局只在实际 Seal/主线集成/必要重验完成后由责任人走同一既有 owner 安装事务，并用 /Users/suxinyang/.local/bin/formal-gates 复验；实际 global host/scope/安装绑定实参取既有正式安装记录并在执行前保存，不能用 project 演练 argv 冒充全局切换。evidence/preseal 和 evidence/postpublish 分开，均引用真实 sealed/post-integration/package/installed identity；历史回归和阶段5–7固定 driver 的绝对路径/同包 identity 附到已有发布记录。
review status: PASS
review reason: 已逐项核对隔离工作区 references/install-and-hooks.md 的当前公开安装/卸载与维护升级合同：完整 install argv 使用固定 STABLE native owner、实际回执绑定的 release/project/config 根，候选 binary 仅作只读 package validate；97eef33d 原包先经已批准的 history-release-recovery 维护准备再安装 CAND，前置与操作顺序可达，当前 Git/Go/shell 能力具备。中断后移除故障环境并重启同一 argv，由既有 journal 对账，不另造恢复入口或手写 pointer/registry。逐步保存 installed binary 输出、registry/install/transaction/failure/recovery 回执及精确身份，能区分恢复、重试和发布完成。Seal 前隔离演练与 Seal/集成/重验后的真实全局检查分列，真实 global 参数取既有安装记录，后续 driver 同包绑定及历史回归仍作为完成条件，oracle 对应 REQ-020/REQ-022，不把演练或历史 PASS 当作本次全局完成。该共享操作方法本身通过；F1 所指出历史执行表的语义覆盖缺口仍独立存在，不被此 PASS 消除。

shared step: native-attached
procedure: 需要直接调用内部 API 的前态使用实际 HOST 创建的原生 action，而非虚构身份。以 subprocess.Popen 启动本例 HOST；在 stdout.jsonl/process.json 和 show 中同时读到真实 identity、CLAIM、SOURCE attach、未 Finished 后，对本例 driver 及其 process.json 标识的 app-server 进程用 OS SIGSTOP 暂停，记录两 PID、ps 状态与原生 turn started。这只是隔离测试的进程暂停，当前来源仍是实际在途；不编辑 claim/source/账目。独立的 OP 进程使用该真实 action 的公开当前绑定执行待测请求，原生内部 model 和目标计数由 native-evidence 独立采集。需 native 动态工具本身实跑时不用暂停，而由本例实际 toy 任务/批计划明确要求在该动态工具中执行所列 model/QA 请求，保存实际 callId 与桥接回执。结束后先 SIGCONT 释放该测试的全部暂停 PID，再按 typed abort/正常完成结算并清理；不能给其他会话发信号。若真实 attach 前态未取得，不伪造 RUNNING；报告具体前置失败。
review status: PASS
review reason: 当前有 Codex app-server 和进程信号能力；真实 attach、限定 PID 暂停及释放步骤明确。

shared step: source-fixture
procedure: 精确 999/1000 等边界只构造隔离输入。执行阶段先用本次真实小 Codex worker/model.run 取得一份完整公开 transcript，普通复制到 E/source-fixture/（保留原 provenance），在复制件保留原生 event/session_meta/task_started/task_complete 格式与当前准备 action 匹配的身份，仅把已观察到的原生累计 token usage 输入字段改为本例指定总数；重复 response 镜像行按原生 ID 复制，不发明新 ID。逐字段记录修改脚本、原件与副本 diff，并显式标注模拟计量输入，不说它是实际模型消耗。SOURCE STOPPED 使用该副本 transcriptPath 与当前 claim 的 actionId/attemptId/provider/identity；由真正 parser/submit 计算 cost，若没有解析到精确要求的999/1000则该前态未成立，不能手写 cost/guard 修正。owner 取独立来源或明确 UNAVAILABLE，不以 dispatched source 伪装 owner。结果先到而 source 未止用真实 RUNNING 观察保存；后送 STOPPED 或明确有 reason 的 UNAVAILABLE，均通过当前 SOURCE typed event、每次最新 revision。receipt/usage/guard/refill 的外部可见顺序实际观察；内部 CAS 语句窗口交 CASE-088 白盒职责。
review status: PASS
review reason: 原生格式复制件只作计量输入，SOURCE parser 实算结果及原件差异可验证。

shared step: interrupt-evidence
procedure: 中断只作用于隔离子进程，并保留 PID、父 PID、原生身份、时间、信号和中断前后只读产物。正常运行中恢复：用 OS 进程状态及原生 thread/turn started 确认正在执行后对调用 driver 发 SIGTERM，待其确实停止，再用同一 HOST --resume 或 RESUME-OP；有真实完成输出时先结算原结果，UNKNOWN 时不换 ID 重做。精确“成功而未回执”子观察只采用同时成立的证据：Codex task_complete/turn completed 与身份已在原生日志中，而 action 尚未 Finished 或内部 operation 尚无 receipt；Git 为 HEAD/parent/唯一 Formal-Gates-Operation: <id> trailer 已存在且 operation 仍 claimed、无 Git receipt。可用 Python 每10ms 只读轮询这些公开文件/状态和 subprocess.Popen 启动的隔离 driver，在确实同时满足条件时 os.kill(driver.pid,SIGTERM)，最多在3个独立正常 run 尝试，原始观测全部保存。无命中不得写 PASS；也不得任意 sleep 后 kill 并推断命中。本版本没有公开暂停钩子，窗口细粒度原子性和所有未能稳定命中的故障点由具体白盒职责验证；黑盒的必测正常中断/恢复和零重复调用仍实际运行。任何可观察成功 prefix 恢复后必须原 identity/turn/commit 集合不增。
review status: PASS
review reason: 运行中中断可执行；精确窗口须有同时成立的原始证据，未命中不冒充通过。

shared step: native-evidence
procedure: 真实宿主用例通过原生 Codex app-server JSON-RPC 执行，外层顺序 initialize→未显式模型时 model/list→thread/start 或 thread/resume→SOURCE attach→turn/start→真实结果→SOURCE 结算。创建/恢复前后保存隔离 CODEX_HOME 下本次 session_meta.id、thread/turn ID、task_complete/task_started 及工具调用，结合独特 QA 标记去重；不能只数 wrapper receipts。外层公开产物在 P/.gates/engine/R/executions/<actionId去act:并将斜杠换下划线>/process.json/stdout.jsonl/answer.json，续跑位于 resume 子目录。内部操作在 task-operations/<id去hact:task:>/request.json/process.json/stdout.log/stderr.log/observed.json；完整路径均由当前真实 ID 推导并只读。拒绝零调用需要相邻合法对照证明该原生观察渠道能看到本次动作。QA target 独立计数使用专门批准 case 的 command={executable:"git",args:["-c","user.name=QA","-c","user.email=qa@example.invalid","-c","core.hooksPath=/dev/null","commit","--allow-empty","-m","QA-<R>-<caseId>-TARGET"],expectedExitCode:0}，每例 prepare 得到的真实 workspace 中 git log/reflog 的完整标记计数为权威旁证；目标每成功一次增1，非法/重复/前置失败增0。该 marker case 明确是隔离准入 fixture 的需求，不替换真正 greet 用例的输出 oracle。读取工作区只能取证，不编辑 operations/receipt。
review status: PASS
review reason: 原生身份、公开产物路径和 Git marker 的独立计数渠道与公开合同一致。

shared step: task-operation
procedure: 这是执行阶段实际公开操作的统一编码说明。OP(K,C,prompt) 先 READ show，选真实已 CLAIM、attach 且未 Finished 的当前 QA/worker action A；binding 完整复制 executions[A].claim 并 identity=executions[A].identity，保持 task 是原字符串。向 E/<唯一requestId>.json 写 {runId:R,expectedRevision:show.revision,binding,requestId:唯一ID,kind:K,caseId:C}，model.run 另给非空 prompt 且 caseId=""；QA prompt 省略或空。调用 [FG,"workflow","drive","--root",P,"--run-id",R,"--operation-request",该绝对JSON路径]，保留完整 argv/stdout/stderr/exit 和 TaskOperationReceipt。每一步重读当前 revision，文件内容逐份保留；正常重复不能换 requestId 重做已完成 QA stage。RESUME-OP 只对同一原文件加 --resume，保留其原 expectedRevision；不修改状态或 request 冒充新操作。外层 --action-id 与 --operation-request 互斥。原生动态工具等价实参严格为 {kind,caseId,prompt} 三字段，bridge 使用真实 callId 作为 requestId 并补完整绑定；须从原生工具调用与同一 operations[id] 保存二者关联。对一例 C 按 qa.prepare→qa.target→qa.collect 调用：prepare 返回其精确 Q 的独立 Git workspace；target 只能执行已冻结 command.executable/args，正常非零退出仍是待比较的数据；collect 读取该次真实 stdout、exitCode、workspace，stderr 取同 operation 的公开 stderr.log。进程创建失败为 FAILED/message，不运行 collect。每项结果用这些真实观察构造 procedure/output/comparison/tool/environment/workspace，不能提交客户端自填 TaskOperationReceipt。依赖 prepare 只允许前序声明 case 已 collect 且退出码等于 expectedExitCode；语义 FAIL 的依赖在最终 verification cases 中明确 BLOCKED，不能只靠相同 exit=0 猜测依赖成功。QA execution 没有 shell；共享非验收准备可复用但每个被冻结的目标操作均须实跑。model.run 实际执行配置的 Codex exec，回执包含原生 identity/transcriptPath，其 cost.kind=task-operation，原 action/TaskKey/Attempt 不新增。
review status: PASS
review reason: 请求结构、当前绑定、四种操作及 prepare→target→collect 前后置均有完整公开定义。

shared step: batch-fixture
procedure: 此说明仅为多 Batch 用例的独立计划准备，不要求先执行任何其他用例。采用 fixture 的 Go toy 和 requirements.md。技术 REVIEW_RESULT 的 granularityReview.requirementRevision 复制当前 claim，rationale="同一问候程序分实现测试和交付说明两个有序批次，无并行状态边界"；batches 顺序为 B1、B2。B1={id:"B1",task:"实现并验证问候程序",subtasks:[{id:"S1",task:"按确认三项输出规则写 go.mod/main.go/contest.go"},{id:"S2",task:"写 greet_test.go 三个测试并实际运行构建与测试"}],dependencies:[],interfaces:"run(args,out,err) 的退出码和两个输出流",stateBoundary:"仅 Go 程序与测试文件",risks:"正常输出与空输入退出码混淆",definitionOfDone:"三条 CLI 规则与三个测试均实测通过",validation:"go build -o <E>/greet .; go test ./...，以及 Alice/空/-h 三次 argv 的精确输出和退出码",recovery:"续用本 Batch 原生会话修正明确失败项，保留既有文件与计数",handoff:"Go 文件已提交；若无不能从仓库恢复的信息则明确无",contextCost:"一个程序及三个测试，同批保留上下文"}。B2={id:"B2",task:"补齐正常使用说明",subtasks:[{id:"S3",task:"README.md 写实际构建命令与 Alice/空/-h 三例的完整预期"}],dependencies:["B1"],interfaces:"B1 已提交 CLI",stateBoundary:"README.md",risks:"文档输出与真实程序不符",definitionOfDone:"README 三例各实际运行且逐字相符",validation:"按 README 的实际构建命令和三次 argv 运行并比较字节/退出码",recovery:"续用 B2 原生会话修正该说明",handoff:"无",contextCost:"只需当前已提交程序和确认需求"}。JSON 文本中的 <E> 在写入实际请求前展开为本例证据绝对路径，validation 说明中的两个命令分别以 argv 执行，不作为一个 shell 字符串。技术结果 SOURCE 结算与 ADJUDICATE 后，REQUEST topology 使用新 developmentPlan.digest 和非空 no-split 理由，DECIDE confirm 后再选择各用例明确的 route；不能把默认单批前态直接标成两批。真实宿主目标由实际 HOST 生成/修改文件、自测与交接；只有非宿主被测目标允许用这些完整结果准备前态，并明确记录模拟部分。
review status: PASS
review reason: 两批三子任务的代码、验证、交接和依赖完整，可经技术结果、裁决、拓扑及路线确认建立。

shared step: retired-workflow-surface
procedure: 本共同步骤只准备已删除命令的负例清单及观测方法，不启动旧运行时。固定名字清单为 resume、abort、reset、requirement、authorize-retry、prepare-review-wave、adjudicate-wave、resolve-wave-target、complete-p2-repair、slicing、settle-findings、route、route-add、qa-worktree、qa-thaw、prepare-gate、prepare-action、claim-dispatch、record-action、record-gate、qa-design、qa-review、qa-suggestion、qa-execution、prepare-candidate、qa-execution-scope、snapshot、cleanup、carry、authorize-repair、seal，review-delta-preview、requirement-impact-preview、route-candidates、future（含其全部历史子命令）、repair-preview，以及从未允许的 submit-result 别名负例。执行时从精确安装包的最终路由/命令说明补入该说明明确列为删除的其他旧拼写及别名，去重保存 E/retired-workflow-names.json；保留 read-only/通用维护能力不能只因历史上由 legacy 调用而列为删除。控制调用为 [FG,"workflow","qa-nonexistent-control","--root",P,"--run-id",R]，保存退出码、stdout、stderr；统一未知命令 oracle 是命令 token 替换为 <COMMAND> 后，错误类别/错误消息与该控制完全一致且退出码非零，输出同时引用实际被请求命令。若当前公开错误合同固定为 unknown workflow subcommand: <name>，逐行机械核对此片段；不能用 missing flag、unsupported engine entry、版本错误或兼容 handler 的业务拒绝代替。所有负例调用前后保存 P/.gates/tmp、P/.gates/results 的相对路径列表及已有文件字节、mode、mtime；保存 Git HEAD。对活动 run 另保存 show 的 revision、runtime、版本/包/安装身份、regular phase/ask、executions 与 cost/guard。此次快照仅覆盖证明业务零写入的输入，不修改任何运行态。 最终严格匹配 stderr 中 unknown workflow subcommand: <实际名字> 且非零；所有旧拼写都与 unknown 控制使用相同 --root/--run-id 参数。还执行 [FG,"workflow","future","status","--root",P,"--run-id",R]，不能落到 future 子路由。工作流顶层 help 的命令集合精确为上述八项，package route-candidates 是另一个保留只读命名空间。
review status: PASS
review reason: 删除清单覆盖最终公开名单，未知命令控制、八项 help 集合及零业务写入判据可机械比较。

shared step: legacy-terminal-input
procedure: 仅在隔离测试准备中复制一个真实历史终态输入。来源为基线 da23375bc2b91029c22bfadcfa4b2457b0c5022e 的 .gates/results/stage-3-5c-qa-efficiency.json；该文件的 runId=stage-3-5c-qa-efficiency、status=SEALED、flow=formal、vcs=git、currentSnapshot=517de91f1b480ec969bd91385c29ce38d8bc2841，未携带新版 engine envelope。执行前由准备方把该基线文件普通复制到 T/preparation/legacy-terminal.json，记录来源基线/原路径和字节摘要，不使用正式活动 run 文件或软链。每例新建并 bootstrap 独立 P 后，把源文件逐字节复制为 P/.gates/results/stage-3-5c-qa-efficiency.json；本例 R 明确改为 stage-3-5c-qa-efficiency，不运行 START，不编辑旧文件内字段，不把历史记录变成本次 PASS。保存复制前后字节和路径清单，确认没有 P/.gates/tmp/R/state.json。被测操作必须是本次 FG 实际读取该副本；来源文件只作历史输入。
review status: PASS
review reason: 指定 Git 历史文件可读取，实际 runId/status/flow/vcs/currentSnapshot 与说明一致。

shared step: history-source-inventory
procedure: 历史逐项表的起始来源固定为：阶段0 refactor-plan/stage-0-requirements.md 与 openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md，对应 .gates/results/phase-0-distribution-002.json；阶段1 openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md 与其 post-seal-audit.md，对应 .gates/results/phase-1-decision-kernel.json；阶段2 openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md，对应 .gates/results/phase-2-persistence-protocol.json、phase-2-persistence-protocol-repair.json、phase-2-persistence-protocol-repair-v2.json、phase-2-persistence-protocol-review-fix.json、phase-2-persistence-protocol-repair-v3.json、phase-2-post-integration.json；阶段3 refactor-plan/stage-3-requirements.md，对应 .gates/results/phase-3-engine-vertical-loop.json 和 phase-3-engine-vertical-loop-rerun-001.json；阶段3.5a refactor-plan/stage-3-5a-qa-coverage-contract.md，对应 .gates/results/stage-3-5a-qa-coverage-contract.json；3.5a-fix refactor-plan/stage-3-5a-fix-requirement.md，对应 .gates/results/3-5a-fix.json 和 3-5a-fix-r2.json；3.5b refactor-plan/review-convergence-and-stage-3-5b-requirement.md，对应 .gates/results/review-convergence-3-5b.json；3.5c refactor-plan/stage-3-5c-requirement.md，对应 .gates/results/stage-3-5c-qa-efficiency.json、stage-3-5c-delivery.json 和 stage-3-5c-post-integration.json。只读对账 refactor-plan/stage-records.md 及上述结果的 requirementArtifacts/修正引用，把确实属于相应阶段的遗漏来源补入同一 inventory，并逐项保留来源路径/revision/原 AC 或稳定锚点/完整原文。来源中的旧执行、旧 PASS、原授权只作历史列；本次 current evidence 初始为空。以当前已确认阶段4需求的 2026-09-08 删除合同判定旧入口退役的授权来源，不把旧文档当作恢复旧命令、旧 decoder 或旧 writer 的要求。 原绑定版本校验按每份历史结果的 requirementArtifacts/revision 与 currentSnapshot 读取文档：当前文件全文摘要匹配原 recordedRevision 时可直接采用；不匹配时从该历史结果 currentSnapshot 以 git show <snapshot>:<path> 只读导出原文并核对 SHA-256，不能拿现存版本替换原输入。已确认阶段3 使用 96a774f346184352259296e142c04cb5bd64a88d:refactor-plan/stage-3-requirements.md（216f066cbc8250b0f7a1959b6a59246888f6ff2495533d1f391e5346c5304182）；3.5a 使用 7d9e85ef7d6d306c469ebe4812353ab34cb78195:refactor-plan/stage-3-5a-qa-coverage-contract.md（4b5fc8ab3b82972422e4f8414538e4b050c7b5e3d6a5eb4cd13058c31d6e1a57）；3.5b 使用 99c7b0cdafa0c3db1edd05d616d084993dd3e000:refactor-plan/review-convergence-and-stage-3-5b-requirement.md（40e516d4ac329a4391dabc971f61a508f9478524238c9b39064482a3781f0b54）。这三个原版本均只读核实可取且摘要相符。其现存版本另列后续修订来源；判断当前保留、替代或退役必须附实际后续确认来源，原历史通过列与本次验证列分别保留。 本轮已物化逐文件取法见 /tmp/phase4-history-design-e8ffe88c/source-inventory.json 的 retrieval/exportPath/sourceRevision/verified 字段；这是原文取证索引，不是执行期再设计。阶段2修复的recorded revision 6bf75084218ce1c9a2f77c0377d6fd1593616525008988380187efec07de4a52 在原currentSnapshot不匹配，精确原文改由 git show 36f38ac5e9eaf5d4fe23ecf09491a789229f9b7c:openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md 获取并匹配该SHA-256。ABORTED 3-5a-fix 的 c062cf7587df6730b045c1d8d6bbff30bd599a8bc72458d629d82d59297c3b15 未取得；保持原ABORTED及原文缺项，不视为本轮豁免；其正式r2的2cdbc14b708849c1fc411ec5edb9afdd23a2651095bb305d9fa6d347d37dc8d3已核对并逐AC映射。
review status: PASS
review reason: 43 份可用导出原文摘要与登记 revision 相符；ABORTED 缺失原文独立保留，未冒充交付或豁免。

shared step: history-execution-table
procedure: 历史执行设计实体为 /tmp/phase4-obligation-design-424767aa/history-public-execution-table.json：rows逐原sourcePath/sourceRevision/sourceAnchor保存完整原文、历史交付来源、当前承接operations和替代/延期依据；operations已物化公开入口、完整输入/前态、操作顺序、oracle；sharedSteps只保存一次并由operation引用。所有未实际运行列均NOT_EXECUTED。本轮新增operation由 /tmp/phase4-obligation-design-424767aa/semantic-changes.json 的 newCases 逐条提交CLI，实际分配CASE ID回填映射，不自造当前CASE编号；操作正文/判据改变须重审相关行。一次执行可为多个原来源行复用证据，仅在时点、源码/包/安装identity、operation正文、绑定与环境全部相同且真实结果完整时；不能跨preseal/postpublish复用。每个阶段对所有RETAINED、AFFECTED及SUPERSEDED且有当前公开operation的行取并集，按确定顺序逐operation实际执行，不按来源行重复跑同一操作；每个operation仍按自身说明新建独立P/R/E。纯coverage/guard JSON中的fixture身份、PASS输入与测试引用只作产品输入，不是假造实际QA或内部testRef。内部合同单列PENDING_WHITEBOX_DESIGN，由开发后独立白盒绑定具体testRef并实跑；它不是黑盒设计审批前置，也不能从公开接入成功推断白盒PASS。sourceScopeAudit的未完成项、PUBLIC_CONTRACT_INPUT_PENDING和仍承诺未映射行保留明确缺口，在未实际解决之前不得声称全表充分或执行通过。 本轮增加 OBLIGATION-OUTSIDE-REQ、OBLIGATION-SAME-REQ-REPAIR、OBLIGATION-COVERED-CONTROL、OBLIGATION-P3-NONBLOCKING 四个已物化操作，输入固定在 /tmp/phase4-obligation-design-424767aa/*.md，必须按各自原生 HOST/当前覆盖 oracle 实跑。H-0003/H-0004/H-0041/H-0138 的直接语义映射见 rows.currentOperations；H-0138 的后续定级规则明确替代旧条目 FAIL 自动使 action FAIL 的部分，覆盖缺口本身仍保留。新增用例编号仅由 CLI 分配，operationKey→实际 CASE ID 在批准前回填，不允许以操作键冒充 CASE。所有引用本共同表的用例重新审查，既有不变 operation 的批准与执行正文不改写；当前设计新增操作及关联行仍 NOT_EXECUTED。
review status: PASS
review reason: 四个新增 operation 的完整步骤、输入文件、oracle 和实际 CASE-178至181 绑定均存在；H-0003/H-0004/H-0041/H-0138 的承接与后续定级来源明确，preseal/postpublish 分列，内部验证保持独立且不预填通过。

shared step: history-release-recovery
procedure: 执行前按当前references/install-and-hooks.md准备MAINT_SOURCE（隔离旧源码副本）：在独立副本实际git checkout --detach 97eef33d866301a2985f510dd486a21196eea3f9；git apply --unidiff-zero <当前已提交包>/references/phase4-installer-maintenance.patch；git add internal/validate/install.go；git commit -m "Validate install sources with their own package contract"；cwd该副本实际go build -o bin/formal-gates ./cmd/formal-gates和bin/formal-gates package validate --root .。这些是公开安装维护准备，不是候选生产开发，不读取补丁来设计被测行为。ORIGINAL_STABLE先按同一精确原始提交保留完整未打补丁包及binary；RELEASE_ROOT为隔离T下回执绑定release根，STABLE为该目标原固定owner。先[STABLE,"install","--source",MAINT_SOURCE,"--host","codex","--scope","project","--project",PROJECT,"--binary-target",STABLE,"--release-root",RELEASE_ROOT,"--force"]，再同argv只把source改为CAND安装阶段4。逐步保存原始/维护/阶段4源码、package/installed identity、generation与回执，不覆盖前一步。Seal前仅隔离演练；真实全局维护升级仍等正式Seal/集成/重验，绝不替换活动正式driver。 提交后恢复使用当前公开安装合同的新事务：RESTORE=[STABLE,"install","--source",ORIGINAL_STABLE,"--host","codex","--scope","project","--project",PROJECT,"--binary-target",STABLE,"--release-root",RELEASE_ROOT,"--force"]。ORIGINAL_STABLE是切换前留存的完整精确原稳定源码包，带其自有bin/formal-gates；SOURCE自身binary仅只读校验，不执行安装。保存每步install.json/transaction/registry副本；RESTORE后实际从同STABLE调用package validate，核对runtime/hook/config/managed-rule/pointer恢复同一原稳定package身份，registry generation大于或等于切换前且不重启旧token；不手写pointer/registry，不回滚registry字节冒充generation。
review status: PASS
review reason: 原始提交可读取，文档所列维护补丁可在隔离材料上通过 git apply --check --unidiff-zero；安装、提交后 RESTORE、新事务及单调 generation 的步骤和判据明确。

shared step: obligation-native
procedure: 此步骤只为新 obligation 用例建立独立真实产品审，不复用既有用例的执行结果。每例以 env 新建 P/R/E；按 fixture 初始化 Git、README.md/seed.txt/design-notes.md，但 requirements.md 完整字节使用本例指定的 /tmp/phase4-obligation-design-424767aa/*.md 输入（执行前普通复制到 P/requirements.md，原件及实际输入留 E/preparation）。输入只有需求与方案，不含期望审查判定、测试关注点或预填 findings。记录 git add/commit 的实际基线。START→DRIVE 结构预检及全文展示→REQUEST confirm-requirement（requiredSources 按 case-set）与当前 Ask DECIDE confirm→REQUEST mode regular 与 DECIDE confirm→DRIVE；从 show/next 取实际 product-review actionId，实际 [FG,"workflow","drive","--root",P,"--run-id",R,"--action-id",actionId]。宿主接收程序生成的规范提示词薄指针，不附加“找这些问题”、预期等级或本 QA oracle。保存完整 prompt、原生会话初始化/完整输入/原始最终回答、产品自动接纳的 raw review、receipt、SOURCE 结算及 series；身份与 task_complete 由 host/native-evidence 独立核对。不得预填 REVIEW_RESULT、手工添 finding 或重试抽取想要的判定；运行错误才按 protocol 有限 retry。解析并保存 E/observed/obligation-comparison.json：以 requirements.md 精确句子与所属 REQ（正文外记 OUTSIDE_REQ）为固定键，每键列本轮 raw 中指向它的 finding ID/requirement/problem/remedy/severityRationale 或原生完整审查文字的位置、列其指出的同 REQ AC 集合。表仅是对原始证据的索引，不是输入或正式结果。语义证据允许自然表述，但必须明确指向相同句子/行为，不能用“覆盖不全”等总评代替；逐项检查需有原文位置和判定依据，空表不通过。使用当前公开 schema；它未定义 obligationToAC 字段，所以不向 submit 发明该字段，原生回答和 review.message/findings 共同承载审查语义。然后由执行责任人逐 finding 独立核实并通过实际 SUBMIT ADJUDICATE：action=product-review、resultDigest=实际 currentDigest，findings 与 raw IDs 一一对应，decision=VALID/DISMISSED，实际核实后的 severity/category、逐项 reason/evidence，completenessEvidence 指向全文逐义务核对。不得把审查漏项补成“产品已识别”。留 raw 与 adjudication 前后 READ(show/status/next) 及 coverage/guarantee 投影。裁决不是新的宿主产品审，也不把主代理断言当原生发现证据。 完整覆盖对照例允许 raw.findings=[]；其比较索引分别记录输入中的实际 AC 归属和本次原生 PASS/无对应缺口 finding，不要求未公开的逐义务 PASS 返回字段。前述逐项缺口证据与空表拒绝要求针对带预置漏项的负例，不向完整对照添加新结果格式。
review status: PASS
review reason: 四份实际需求输入可读取；START/确认/mode/HOST/ADJUDICATE 均有公开协议，准备顺序可达；继承已批准的真实宿主和原生取证能力，并核实当前 Codex 已登录；原始结果与核实裁决分开，漏项索引和完整对照的空 findings 均有可观察判据。

shared step: boundary-native
procedure: 仅用于本轮受控执行用例。沿用 env/host/task-operation/native-evidence；在 START 之前按公开 user-configuration 合同写隔离 P/.gates/config.json 为 {"codexExecutable":"/Applications/ChatGPT.app/Contents/Resources/codex","model":"gpt-6-astra","reasoningEffort":"medium"}，已有其他所需配置字段原样保留。实际以 argv=["/Applications/ChatGPT.app/Contents/Resources/codex","--version"] 保存本次可执行文件版本；实际 CONFIG 保存三个字段的 sources/values，再 START 并 READ(show) 核对 regular.settings 捕获相同路径、model 与 effort。全部本轮真实 worker 及 model.run 辅助调用使用这一 GPT-6/medium 配置，并从各自原生启动 argv、turn/start 或会话记录核对实际运行值。配置仅对新 run 生效，恢复同一测试 run 时沿用其捕获值，不编辑 state/settings 冒充生效。桌面内置 0.153.4 的 gpt-6-astra/medium 成功探针只作环境可用性事实，不替代本例真实 HOST、辅助调用、工具边界和完成结果的执行证据；若该绝对路径或配置在执行时不可用，保留实际错误，不回退 PATH 中的旧 Codex。不可因不可用而静默换模型或把 fixture 身份记成真实身份。层级按本次原生父子关系定义，不能按请求者自称 role/level 分类：owner 调 HOST 创建 worker，worker 通过实际 formal_gates_operation 调 model.run 创建纯文本辅助；后者能否再委派只检查实际公布工具和原生调用。逐个保存实际 thread/parent/callId、完整工具列表、turn/start 参数、model argv、request/receipt 及输出。CLI OP 是公开的绑定准入面，不是下级代理凭空拥有 CLI/shell 的证据；动态工具只有 kind/caseId/prompt，不添加未声明的 provider/candidate 等字段冒充合法入口。对下级没有的工具，原生工具清单和启动禁用参数证明未暴露；对有工具的层级，须真正提交待测调用并观察拒绝。所有支持层级都用同一当前绑定/名单/额度判据；未支持的下一层只记创建前未暴露或拒绝，不计“支持并通过”。零调用采用 native-evidence 的实际会话/进程/标记提交 before/after，并做邻接合法对照；没有独立观测就记录缺证据。仅允许本隔离任务既有功能，不调用插件外 Codex 工具制造下级。
review status: PASS
review reason: 配置文件位置、优先级、codexExecutable/model/reasoningEffort 字段及只对新 run 生效均匹配公开 user-configuration；绝对 binary 实测为 0.153.4，GPT-6 astra/medium exec 成功；原生 app-server 真正创建并完成一个独立线程且 resume 返回相同身份。真实 argv/JSON-RPC/原始结果保留，足以证明审查期所需环境能力；CONFIG/settings、产品 HOST、动态工具、辅助计量、零调用和工具边界仍必须由各候选用例执行时逐项实跑，不以环境探针代替。

shared step: boundary-checkpoint
procedure: 本轮 checkpoint 专用真实前态，同时作为 CASE-051 的三例 fixture 替代：需求明确隔离目标每次执行先在其独立工作区创建一个空 Git commit 作为目标本身的可观察效果，再执行 greet；a=Alice stdout Hello Alice\n/stderr空/exit0，b=无参 stdout空/stderr usage\n/exit2，c=-h stdout usage\n/stderr空/exit0。批准三个独立 case 的 command={executable:"python3",args:["qa-boundary-entry.py",字母,"QA-"+R+"-"+字母],expectedExitCode:a/c为0且b为2}，dependencies=[]。冻结前在实际候选提交 qa-boundary-entry.py：以 subprocess.run(["git","-c","user.name=QA","-c","user.email=qa@example.invalid","-c","core.hooksPath=/dev/null","commit","--allow-empty","-m",sys.argv[2]],check=True,stdout=subprocess.DEVNULL) 创建目标提交，随后以 subprocess.run(["go","run","."]+{"a":["Alice"],"b":[],"c":["-h"]}[sys.argv[1]]) 调真实 toy 并以其 returncode 退出；此文件 import subprocess,sys。候选中实际无参行为故意为 stdout oops\n、stderr空、exit0，其余正确，按 fixture 真写/构建/提交，不预写运行结果。共享 code/command/oracle 全部纳入本例自身批准 manifest，不能在 FROZEN 后插入脚本。所有 a/b/c 操作必须经过真实 OP 或动态工具 prepare→target→collect。a/b 完成后按真实输出保存完整 a PASS、b FAIL及 b finding，remaining=[c]，提交 status=CHECKPOINT/message="隔离中断：c 尚未执行"。保存 checkpoint 原 JSON、accepted receipt 和 target/wave/TaskKey/attempt/identity，不能把 CHECKPOINT 作为终态 SOURCE。每次调用前后在原 prepare workspace 实际执行 git log --format=%H%x09%s 与 git reflog，统计精确 QA-R-字母 消息的 commit 对象；成功目标每次只增加一个，重复准备也须比较已有 workspace 清单与树，没有悄然新副本再调用目标。零重复不能只看回执。该脚本的 Git commit 是批准目标效果，不是外部自填计数器；测试证据区禁止复制预先 PASS。
review status: PASS
review reason: 公开 CHECKPOINT/invalidated 合同可编码，前态脚本在独立临时 Git 项目实际运行得到 a PASS、b FAIL、c PASS，三个目标各产生一个标记提交；已知错误 b 输出 oops/exit0 被明确判 FAIL，未合成产品执行证据。真实 HOST 能力由 boundary-native 单独判定。

shared step: boundary-new-attempt
procedure: 仅用于测试同 TaskKey 新 attempt：已真实接受 boundary-checkpoint 后，保存原生身份；正常停掉本隔离 HOST 进程，按现有 HOST --resume 发起真实恢复，待其原生 process/thread/turn 启动记录可见时由 OS 终止该恢复的隔离 app-server 进程，保留 PID、时间、退出及 driver 实际 RUNTIME_ERROR。不得手填“发生运行错误”、虚构 source 或用无效结果代替本用例要求的真实运行错误。由现有 driver 结算实际 SOURCE 后 READ/DRIVE，必要时仅按 protocol 的 verification-retry 当前 Ask 获一次授权，取得同一 QA TaskKey 的实际下一 attempt。真实 HOST 创建/恢复该已签发 action，观察当前 identity、checkpoint 历史与 expected/remaining；不修改 state、空置 operations 或新增 wave 造前态。实际新原生身份若由程序合法签发则保留，否则沿实际签发的恢复身份，不能人为强换人。旧 identity 的终结和未决调用对账齐全后才测试下一 attempt。若该运行错误不能通过公开有限重试到达合法下一 attempt，保存实际阻塞，不改 fixture 声称完成。新 attempt 的 a/b 完成事实和 c remaining 必须来自同一 wave 保存的真实 checkpoint。
review status: PASS
review reason: 此前 FAIL 的给定理由是固定模型不能完成首次原生任务。此次配置更改后，独立原生 app-server 的 GPT-6 astra/medium 创建、完成和同身份 resume 已成功；因此该环境前提不再阻止真实 checkpoint/resume 中断前态。限定隔离 PID 的 OS 终止、真实 RUNTIME_ERROR/SOURCE 结算、READ/DRIVE 与 action-specific verification-retry 均有公开合同；设计明确不能伪填错误或空 operations。真实产品恢复错误和下一 attempt 仍须在执行阶段实际取得，不把本审批作为该产品行为的 PASS。

shared step: case-set-applicable
procedure: 在 fixture 的初次需求确认 REQUEST 中登记 requiredSources=[{"sourceId":"requirements.md","category":"PRODUCT_REQUIREMENT","applicability":"QA"},{"sourceId":"design-notes.md","category":"SOLUTION_CONSTRAINT","applicability":"NON_QA"}]；两个文件先真实创建并纳入基线，后者仅写入独立说明“此项目仅作隔离协议测试，不是正式审查结论。”，同时核对其原文作为非 QA 证据。DOC-001 是下述用例对 design-notes.md 的人类简称，协议 sourceId 始终使用实际相对路径。REQ/AC 的唯一正式投影仍来自 requirements.md，不将 NON_QA 用于 AC。被测 toy 的完整设计按 references/engine-regular.md 的 design.manifest 结构提交：mode=BLACKBOX/WHITEBOX；points 为 <MODE>:AC-001、<MODE>:AC-002、<MODE>:AC-003；a/b/c 对应调用方在此隔离设计中明确定义的 <MODE>:A、<MODE>:B、<MODE>:C。它们是被测协议里的 fixture ID，不是本次外层 driver 的 CASE ID；外层新增 case 仍由 driver 分配。bindings 复制 requiredSources.binding，reviewKind/topologyScope/routeScope 按公开例子填入；sources=["requirements.md"]；a→AC-001、b→AC-002、c→AC-003，每条 edge.sourceId="requirements.md"；a/b 引用 sharedSteps 中 setup，c 不引用。blackbox 的 publicEntry、preconditions、steps 与 oracle 分别写实际可执行路径/输入/命令及输出比较；whitebox 用唯一实际 testRef。alternatives 给 design-notes.md 一项，reason/method 说明读取该文件与上述完整原文比较，status=PASS、evidence 为实际读取结果，不能拿历史 PASS 充当证据。保存 PREPARATION_RESULT 返回的完整 design，再按该次 reviewScope 逐 source/point/case/shared ID 提交 PREPARATION_RESULT.review 中对应 PASS/FAIL 数组，不遗漏任何独立项。后续引用以已接纳 design 返回的相同 ID 为准，whitelist/candidate/manifest digest 仅从公开投影读取，不手算或伪填。每例保留完整 inventory、manifest、map、whitelist、candidate、逐项审查前后列表；实际 execution 的 procedure/output/comparison/tool/environment/workspace 全部来自当次运行。 每个 regular case 的 command 必填并随用例集冻结。toy BLACKBOX:A/B/C 分别为 {executable:"python3",args:["qa-entry.py","Alice"],expectedExitCode:0}、同 args=["qa-entry.py"]/expectedExitCode:2、同 args=["qa-entry.py","-h"]/expectedExitCode:0；dependencies 默认 []，特殊例明确指定。WHITEBOX:A/B/C 的 command 分别 {executable:"go",args:["test","-run","^TestGreetAlice$","-count=1"],expectedExitCode:0} 及另外两函数，cwd 始终由 adapter 选的精确候选干净副本；不冻结 E/greet 这个来自旧候选的外部二进制路径。首次 routeScope 按文档公开算法对当前 route 的 kind/qa/gates 顺序对象做两空格 JSON、末尾换行、UTF-8、不转义 <>&，SHA256 加 sha256:；已有 design 则直接复制其 binding。 最终黑盒 publicEntry/steps 同步声明为 python3 qa-entry.py 的上述实际 argv（a 的多行为特例另写 --qa-alice-and-help）；原准备自测 E/greet 不作为冻结执行入口。 本共同步骤完整替代本例对旧 case-set 的引用；QA manifest.sources 只列 applicability=QA 的来源，逐 sourceDecisions 对账该 manifest.sources。requiredSources 仍同时登记上述 QA 与 NON_QA 两来源；design-notes.md 不进入 QA manifest.sources/points/edges，而由 alternatives 独立实际读取比较并留 PASS 证据。任何 AC 缺口仍不得转 NON_QA。
review status: PASS
review reason: 公开 design/alternatives 合同可编码；QA 来源与 NON_QA 分离，确认登记仍完整；Git/Go/Python 前置可建，逐项审查与实际读取证据明确，没有用 NON_QA 消除 AC 义务。

shared step: boundary-native-luna
procedure: 仅用于本轮受控执行用例。沿用 env/host/task-operation/native-evidence；在 START 之前按公开 user-configuration 合同写隔离 P/.gates/config.json 为 {"codexExecutable":"/Applications/ChatGPT.app/Contents/Resources/codex","model":"gpt-5.6-luna","reasoningEffort":"max"}，已有其他所需配置字段原样保留。实际以 argv=["/Applications/ChatGPT.app/Contents/Resources/codex","--version"] 保存本次可执行文件版本；实际 CONFIG 保存三个字段的 sources/values，再 START 并 READ(show) 核对 regular.settings 捕获相同路径、model 与 effort。全部本轮真实 worker 及 model.run 辅助调用使用这一 gpt-5.6-luna/max 配置，并从各自原生启动 argv、turn/start 或会话记录核对实际运行值。配置仅对新 run 生效，恢复同一测试 run 时沿用其捕获值，不编辑 state/settings 冒充生效。既往 GPT-6/medium 成功探针不证明本次 Luna/max 已运行；本例的真实 HOST、辅助调用、工具边界和完成结果均在执行阶段保留本次原生证据；若该绝对路径或配置在执行时不可用，保留实际错误，不回退 PATH 中的旧 Codex。不可因不可用而静默换模型或把 fixture 身份记成真实身份。层级按本次原生父子关系定义，不能按请求者自称 role/level 分类：owner 调 HOST 创建 worker，worker 通过实际 formal_gates_operation 调 model.run 创建纯文本辅助；后者能否再委派只检查实际公布工具和原生调用。逐个保存实际 thread/parent/callId、完整工具列表、turn/start 参数、model argv、request/receipt 及输出。CLI OP 是公开的绑定准入面，不是下级代理凭空拥有 CLI/shell 的证据；动态工具只有 kind/caseId/prompt，不添加未声明的 provider/candidate 等字段冒充合法入口。对下级没有的工具，原生工具清单和启动禁用参数证明未暴露；对有工具的层级，须真正提交待测调用并观察拒绝。所有支持层级都用同一当前绑定/名单/额度判据；未支持的下一层只记创建前未暴露或拒绝，不计“支持并通过”。零调用采用 native-evidence 的实际会话/进程/标记提交 before/after，并做邻接合法对照；没有独立观测就记录缺证据。仅允许本隔离任务既有功能，不调用插件外 Codex 工具制造下级。 本步骤为仅受影响新流程测试调用者使用的局部定义，完全替代其 boundary-native 配置段（包括 boundary-checkpoint、boundary-new-attempt 对该段的间接引用）。新建 run 前写上述配置；已经开始的 run 先 READ(show) 保存原捕获 settings，恢复严格沿原值，不改 state/settings、不重启 run 来迎合新偏好；该既有 run 的观测按其实际捕获值核对。CASE-262 及其他已完成 GPT-6/medium 证据不改、不重跑。新建测试 run 的实际原生 model 必须为 gpt-5.6-luna，effort 必须为 max；不可用留真实错误，不能静默回退。
review status: PASS
review reason: 公开配置字段和新 run 捕获规则一致；本次真实 Codex 原生线程已验证 Luna/max 可完成；旧 run 保留捕获配置，实际 HOST、动态工具、辅助调用及零调用证据仍由执行阶段逐项取得。

shared step: bound-requirement-recovery
procedure: 仅用于本轮 CASE-011 的 A/D 及 CASE-145–159 在原独立 P/R 中修正已绑定需求。错误输入、旧全文/revision、旧 Ask、真实拒绝和诊断先完整保留。修复全文 V 由各例给出；不新建 run、不改 engine state。READ(show/next) 后 SUBMIT REQUEST {action:"requirement-change",origin:"USER_INITIATED_CHANGE",reason:"修正本隔离输入的已观察结构或正文错误，重新确认完整文件"}，外层 ID/Kind/operation/expectedRevision 按 protocol；保留立即生效的修订屏障和返回 Ask，若实际返回待确认 Ask 则使用其 requestId/token/digest/current revision、choice=confirm 完成该 Ask。随后普通写 P/requirements.md 为 V 的精确 UTF-8 全文（CASE-011 D 已改文件仍沿同一文件），计算 V 的无前缀小写 SHA-256。READ 当前 revision 后 REQUEST {action:"apply-requirement",document:{path:"requirements.md",text:V,revision:SHA256(V)},meaning:"changed",origin:"USER_INITIATED_CHANGE",impact:{"product-review":"affected","start-readiness":"affected"},keep:[],reason:"以本例明确修复全文替换错误输入；原错误证据保留"}。保存当前影响预览、全文、state/base/candidate revision、后果与额度；只以实际 Ask 的 id/token/digest/current expectedRevision 提交 DECIDE choice=confirm。READ/DRIVE 取得新预检/投影，核对实际文件与 regular.requirement.text 完全相同、revision==SHA256(V)。若投影 confirmed 尚未成立而 availableActions 提供 confirm-requirement，再按 protocol REQUEST 该动作并以新的 Ask 精确确认；不得复用旧 Ask。最后 READ(show/next) 验证新全文和精确 revision 已 confirmed 且可初选 mode。只有这条公开恢复链实际完成才记恢复正例 PASS；新目标未执行、拒绝或接口不能建立前态时保留实际结果，不能以编辑后 DRIVE 或本地解析成功补 PASS。
review status: PASS
review reason: requirement-change/apply-requirement 是公开的非终态恢复路径；同文件全文、SHA256、两审 impact 和当前 Ask 绑定完整；本地文件与 Git 能力可用，保留原错误并实际重新预检确认的判据对应 AC-007。

shared step: adopt-current-candidate
procedure: 本例 adopt-external 的 old-candidate C 严格取请求前 READ(show).regular.candidate；B 取 regular.baseSnapshot，Q（或 Q0）取 preparation.candidate.identity，F 取用户工作区 P 实际 git rev-parse HEAD，四项分别保存，不假定 C==Q。在 P 用 argv=["git","diff","--binary",C,F,"--"] 采集未解码原始 stdout 字节并写证据文件，diffDigest="sha256:"+SHA256(这些字节)；REQUEST external={identity:F,lineage:B,diffDigest}，impact/reason 沿逐例定义。Q→F 的 tree/view 等价证据可另取，但不能代替 C→F 请求摘要。保存 show、Git argv/字节、完整 REQUEST、当前预览与 Ask；DECIDE 用该 Ask 的 requestId/token/digest 与当前 expectedRevision。若 Ask 后 HEAD 或当前候选又变化，旧决定必须拒绝，不偷偷重算摘要替旧确认；新合法采纳另走新 REQUEST。原 B 始终不变。
review status: PASS
review reason: 公开 external 合同要求原 base lineage 与 old-candidate→new-head 原始二进制 diff 摘要；步骤明确区分 regular.candidate、装配候选和实际 HEAD，Git 能取得原始字节，Ask 后漂移拒绝与证据留存符合 AC-043。

shared step: fa5-reuse
procedure: 本轮只在执行阶段运行候选。先读取 root-audit/fa5-blackbox-scope-proposal.json 的对应 case evidence 及 completedReusable，实际根为 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/handoffs/phase4-recheck-b02aa57/root-audit；保留旧 argv/脚本/真实状态/原生身份/已完成断言，另写本轮缺口清单。原证据保持其源码/包/候选/环境，不改成当前 PASS。相同 run 只从未完成合法前态续跑，已终态不重开；当前候选绑定不兼容时复用脚本与输入另建隔离 P/R，不复制假身份或待测回执。构造前态只用于高成本前置，逐字段说明其代表的正常可达状态；被测操作必须真实使用文档化 FG。只重证正式 scope 要求的变化分支，不能以设计修订把 FAIL/未执行项变成 PASS。
review status: PASS
review reason: 只复用明示已有输入和脚本，旧身份/候选/FAIL 与当前执行分离；scope文件与新设计表存在，不把旧证据改为当前PASS，真实被测入口保持公开调用。

CASE-001
mode: blackbox
description: 实际阶段4安装通过 start/drive/submit 完成 regular 受理并始终保持同一 engine 写入身份
procedure: 在干净 P 用 START 创建 S0，记录返回的 runtime、schema/definition、definition digest、package digest、installed identity 与 Git HEAD；顺序运行 READ(show)、READ(status)、READ(next)、READ(diagnose) 并留原始输出。运行 DRIVE 完成需求结构预检与全文展示，逐字节核对全文与 requirements.md；按 protocol 以当前 Ask 绑定通过 SUBMIT 确认需求，再经 REQUEST mode=regular 和对应 DECIDE 初选 regular。保存每次 event、receipt 与提交前后 show。最后在未变输入上再次 READ(show/status/next)，把所有实际出现的权威状态路径列入 E/writer-observation.json。用例不调用旧 workflow 命令；其删除行为由独立负例验收。
oracle: START/DRIVE/SUBMIT 的合法调用均成功；运行态值为 engine，首次捕获的 schema/definition/package/installed 身份在本例全过程逐字段完全相等。确认前 route 未绑定且 executions 中后续业务任务为空；精确确认后 regular 可绑定并签发产品审，未提前技术审或开发。只读调用前后 revision 与业务投影完全相同；每次新接纳事件至多提交一次业务修订。实际权威 workflow 输入仅有该 engine 状态，不能创建一份 legacy 状态或通过安装目标身份选择 legacy。源码/注册表不存在第二路径的结构性证明由白盒对应覆盖承担，不能仅凭本例输出宣称完成该结构性部分。
shared steps: env, protocol, fixture
primary AC bindings: AC-001
review status: PASS

CASE-002
mode: blackbox
description: 当前版本诊断区分正常、终态、缺失与损坏；不同 engine 绑定在写入前拒绝
procedure: 四个独立P：真实START的S0；按公开lightweight链完成的当前engine终态；bootstrap但从未创建R；只用于raw诊断的损坏JSON P/.gates/tmp/R/state.json（字节为{broken）。逐个[FG,"workflow","diagnose","--root",P,"--run-id",R]，保存完整结果与文件前后字节/mtime。另在合法活动R，以另一份通过既有安装事务安装的精确不同generation/installed identity launcher对R实际DRIVE和SUBMIT语法完整REQUEST，保存admission版本/身份拒绝、原R不改写；不得直接用裸候选binary冒充合法不同安装。旧历史运行态的真实原文和最小只读路径由legacy-terminal-input独立用例覆盖。未知schema/definition/digest组合的decoder确定性拒绝在白盒按完整AC验证，不假定存在交付未列出的公开loader fixture命令，也不手改当前正式run。
oracle: 四类 diagnose 有可区分的 current/terminal/missing/corrupt 结果；损坏输入明确不可解析，缺失输入不被报告为已有合法 run。全部 diagnose 前后输入字节、mode、mtime 不变。写入身份/版本不匹配时返回非零并指出实际不匹配字段或 UNSUPPORTED_RUN_VERSION；原状态/summary 不转换、不改写，权威 revision/有效结果/包身份不变，无新执行或额度。不得把改写样本当作正常可迁移状态而增加兼容逻辑。
shared steps: env, protocol, fixture
primary AC bindings: AC-002
review status: PASS

CASE-003
mode: blackbox
description: route matrix 的公开只读/维护入口与实际路由一致
procedure: 把最终公开命令合同转成逐行表，workflow 恰八项：START、READ(show/status/next/diagnose)、DRIVE、SUBMIT、guard-preview。READ 四项与复制 PKG/references/guard-preview-basis.json 后 [FG,"workflow","guard-preview","--input",E/basis.json] 分别在正常 active run 调用，前后核对 revision/授权/额度。另调用 [FG,"package","route-candidates","--root",PKG] 保存当前门清单；包/registry/install/coverage 纯计算和脱离 run 的 gate run/report 只按其保留帮助在独立数据调用并保存 argv，结果不能推进 R。lifecycle 是观察事实，SOURCE 是唯一 typed 写路径，不称其为零写 read-only。SVN/P4 各用独立 START 把 --vcs 改 svn/p4；非 Codex 的 regular 从对应已安装宿主隔离入口请求 mode regular；split 用不存在 workflow slicing 命令与技术审前/后的 topology 非 no-split 企图，记录明确拒绝、无 legacy fallback。所有旧命令统一引用 retired-workflow-surface。
oracle: workflow 实际帮助与八项闭集合完全一致，无隐藏别名；四只读与 guard-preview 不改变当前权威 revision/额度/授权，preview 非权威。保留维护/计算功能有实际结果，但不能因脱离 run 报告推进正式 R。未支持 VCS/宿主/split 明确拒绝，不能进入旧运行时。
shared steps: env, protocol, fixture
primary AC bindings: AC-003, AC-047
review status: PASS

CASE-004
mode: blackbox
description: lightweight 保留同一需求文件并完成 unverified 闭环
procedure: 从真实 START 的 S0 运行 DRIVE 结构预检，按 protocol 展示并核对完整 requirements.md，然后确认至 S1。提交 REQUEST mode=lightweight 与该 Ask 的 DECIDE confirm；读取 regular.requirement.path/revision、mode、guarantee、executions。随后 DRIVE 至公开 UNVERIFIED 终态和自动 cleanup 完成；重启调用 READ(show/status/next/diagnose)，原样重放已经接纳的模式 DECIDE event，再 DRIVE。保留终态 summary/receipt、Git HEAD、事件 ID 和实际签发动作列表。此处没有 workflow seal 命令或后续 QA/门确认步骤。
oracle: 初选成功且绑定的 path/revision 与受理文件完全相等；终态明确 UNVERIFIED/unverified，guarantee 非激活。产品/技术/开发/QA/门执行集合为空。重启、事件重放与终态 DRIVE 不改候选 identity、不增加新执行或重复 cleanup；不能把轻量终态记成完整需求保证 PASS。
shared steps: env, protocol, fixture
primary AC bindings: AC-003, AC-008
review status: PASS

CASE-005
mode: blackbox
description: 六类公开边界与确定性计划在真实 regular 使用中可辨认
procedure: 分别在 S0 的结构预检 Ready、已签发 HOST 的 HostAction、REQUEST route 的 Ask、实际 in-flight/COST_PENDING 的 Wait、完整原始 review 待裁决的 Operator、正常 lightweight 终态的 Complete 公开 next/drive 输出中采集六类边界。每种可纯读时，连续 READ next 两次且中间无事件/外部观察变化，把返回计划解码后按键排序 JSON 比较；保存真实当前 state revision、observation 与 definition。Ready 的 transport 只引用已签发 action 和规范文件；HostAction 按当前公开 union 的实际 operation 名/绑定分别保存 native start/resume、内部 task-operation、Git/promotion 三类的真实调用目标。名称若只是内部类型未公开序列化，则记录对应公开行为与字段，不发明一个 kind 输入向 CLI 提交。
oracle: 六种流程边界都有真实可定位的公开观察；相同输入的 canonical 计划一致。真实副作用仅通过当前已签发原子 handler/封闭 task operation，冻结命令与实际 argv 相同，不存在用户补任意 shell 字段。所有 Git/推进来自 DRIVE adapter 的实际轨迹。
shared steps: env, protocol, fixture
primary AC bindings: AC-004
review status: PASS

CASE-006
mode: blackbox
description: 合法 eligible 集合按容量签发且同功能在途不重复
procedure: 三个独立 P/R 在 START 之前分别把 P/.gates/config.json 写为 {"capacity":1}、{"capacity":2}、{"capacity":4}，CONFIG 核对来源，再 START 并确认捕获 settings.capacity。以 custom 路线 qa=["blackbox"]、gates=[G0,G1]，G0/G1 取当前 catalog 前两个不同真实 ID。到 S5 已批准/FROZEN/真实候选装配完成而尚无验证 target 在途，expected eligible 必须精确为 blackbox execution、G0、G1 三项。各 DRIVE 一次，保存前后 show 的 pending/executions/guard；此处统计程序签发，不把没有 HOST 的 pending 冒充实际原生调用。保持这些签发未完成，再 DRIVE 两次；最后完成并结算其中一项，再 DRIVE 观察容量可用时的剩余签发。
oracle: START 捕获容量分别 1、2、4，第一次新签发数恰为 1、2、3，目标属于同一三项 eligible 集合。已有同 target+mode 执行在途时再次 DRIVE 不复制该执行，全部在途数不超过 C；释放一个槽位后最多补一项未签发 eligible。依赖未完成目标与已完成目标不混入剩余集合。
shared steps: env, protocol, fixture
primary AC bindings: AC-004
review status: PASS

CASE-007
mode: blackbox
description: 公开动作提交拒绝跳步、缺依赖和未签发步骤
procedure: 在未确认的 S0 依次以独立 P/R 提交 REQUEST {action:"jump",target:"start-readiness",reason:"隔离提前技术审负例"}、REQUEST {action:"route",route:{kind:"full",qa:[],gates:[]}}；再实际调用 [FG,"workflow","drive","--root",P,"--run-id",R,"--action-id","not-issued-development"]。在 S4 对实际已签发 action A，分别把 HOST 的 action-id 改为 not-issued-step，以及用另一个 run 已签发的真实 ID；各保留独立前后 show 和 native 审计。最后对已真实完成的 worker action A 再调用同一 HOST。纯定义中的 step/registry/依赖非法 fixture 不经虚构公开字段提交，由相应结构测试验证。
oracle: 每种请求在底层调用前拒绝，零新任务、零下游副作用；合法已存在结果不被覆盖，attempt/额度不因拒绝新增。非法定义八类 fixture 的纯编译规则由白盒承接，不以本例代替。
shared steps: env, protocol, fixture, host
primary AC bindings: AC-005, AC-051
review status: PASS

CASE-008
mode: blackbox
description: 相同事件幂等重放、冲突事件保留原结果
procedure: S1 发出 REQUEST mode="regular"，保存当前 ask.id/token/digest/options；把 DECIDE confirm 的完整 event 保存为 E/mode-event.json，其 expectedRevision 取当前 show。SUBMIT 该文件成功后不改任何字节原样重放两次；每次前后 READ show/next 并保存原 receipt。最后复制该 event，仅把 regular.decision.choice 从 confirm 改为 cancel，保留原 event.ID 和其余字段再次 SUBMIT。事件 digest 由程序生成，不添加客户端 digest 字段。
oracle: 完全相同重放返回原回执且 revision、派发与底层调用数不增加；同 ID 不同 digest 明确 conflict，原模式和原事件结果保持。
shared steps: env, protocol, fixture
primary AC bindings: AC-006
review status: PASS

CASE-009
mode: blackbox
description: 真实原生调用中断先对账已有身份，UNKNOWN 不另建任务
procedure: 实际 HOST 开始一个本例审查/worker，用 native-evidence 观察身份与 turn started。先在真实运行中按 interrupt-evidence 终止隔离 driver，确认停止后 HOST 同一 action --resume；保存 before/after 原生 identity/turn 集合和 action/attempt，已有完整答案时仅结算。另对 task-operation 的 model.run 实跑使用相同方法并 RESUME-OP；精确成功未回执窗口只有实际命中并留下共同说明规定的完整证据才另记录，不用随机中断冒充。对无完成观察且已认领请求的公开恢复读到 UNKNOWN 后原样再次 resume，观察不发起第二次 model/native create。已接纳的结果事件只读重放一次。
oracle: 已知身份恢复不另建 fresh 会话，原生 create 总数=1，有限 resume 使用同 identity；完成答案/已成功 operation 只结算原观察，重复恢复无新增成功前缀。无足够事实时保持 UNKNOWN，不换 requestId 重做。精确窗口未命中的记录不计该子观察 PASS，其内部确定性窗口由白盒承担。
shared steps: env, protocol, fixture, host, interrupt-evidence, native-evidence
primary AC bindings: AC-006, AC-052
review status: PASS

CASE-010
mode: blackbox
description: 首次受理必须预检、全文和精确 revision 确认后才推进
procedure: 在普通 Git P 中写完整 requirements.md 并 START 到 S0，保存 start 输出、READ next；实际 DRIVE 完成预检，再 REQUEST confirm-requirement。保存这次公开 projection/Ask 与完整 requirement.text、path/revision；把输出 JSON 解码后的全文 UTF-8 字节与 requirements.md 比较，不比较 JSON 转义形式。在未 DECIDE 前再 DRIVE 与 READ，保存无派发；随后 DECIDE choice=confirm，绑定 ask.id/token/digest 与当前 expectedRevision，然后 READ/DRIVE。
oracle: start 只绑定受理且 route 未绑定；预检、投影和确认 revision 相同，展示无截断。确认前没有后续派发，合法确认后才可选择模式/推进。
shared steps: env, protocol, fixture
primary AC bindings: AC-007
review status: PASS

CASE-011
mode: blackbox
description: 常见需求确认错误不能绕过唯一文件与新鲜度
procedure: 每行新建独立 P/R：A 的 requirements.md 缺失 AC 行；B 预检成功但不提交确认；C 对确认 DECIDE 使用已保存但不等于当前的 expectedRevision；D 预检后把需求正文中的 Hello Alice 改成 Hi Alice，再用原 ask.id/token/digest 确认；E 把两个已有相对文件名以 JSON 数组字符串作为 --requirement 的一个参数传 START，且不存在该名字的单一文件。各实际运行 START/适用 DRIVE/确认提交，留错误、实际绑定路径和 phase/route/executions。纠正时 A 以本例删除 AC 行之前保存的完整 fixture requirements.md 为 V，D 以 Hello Alice 已改 Hi Alice 的当前完整文件为 V；A/D 均按 bound-requirement-recovery 在同一 P/R 走公开需求修订、全文绑定和新确认。B 直接完成当前合法确认；C 重新读取并使用当前 Ask/revision；E 将 START 参数改为唯一 requirements.md，成功 START 后按 protocol 预检、展示并确认。参数错误的 E 不伪装成同时绑定两份 artifact；requiredSources 合法多来源由 CASE-039 另测。
oracle: 各无效输入指出缺失结构/唯一文件/过期或不匹配确认，route 未绑定且无后续派发；编辑后的旧确认不能使用；纠正后可按正常链路推进，无部分保证激活。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007
review status: PASS

CASE-012
mode: blackbox
description: 未选模式可初选 regular，regular 不能再转 lightweight
procedure: S1 READ 保存 mode 未绑定，然后 REQUEST mode="regular"、DECIDE confirm。再次 REQUEST mode="lightweight" 保存拒绝。start 的 split 负例使用两个独立未 START 的 P/R，分别在 START argv 末尾加 ["--split","yes"]、["--split","no"]；保存非零未知参数结果，不能把它当作预绑定拓扑。另在 S1 和 S2 分别 REQUEST topology(planDigest="no-confirmed-plan",reason="提前拓扑负例")；最终从合法技术审完整计划经 SOURCE/ADJUDICATE 后取得真实 developmentPlan.digest，再 REQUEST topology/DECIDE confirm，理由="当前单项目无需拆分"。
oracle: S1 初选 regular 成功；既有 regular 转轻量拒绝。start 不接受/冻结 split 意向，技术审前不能绑定拓扑；技术审通过后 no-split 理由才持久化，早期意向变化不要求重建 R。
shared steps: env, protocol, fixture
primary AC bindings: AC-008
review status: PASS

CASE-013
mode: blackbox
description: full 与 custom 含 QA 均在技术审和拓扑之后原子激活保证
procedure: 四个独立 S3 前态先取得当前 catalogGates 第一项 G0，分别提交 route={kind:"full",qa:[],gates:[]}、{kind:"custom",qa:["blackbox"],gates:[G0]}、{kind:"custom",qa:["whitebox"],gates:[G0]}、{kind:"custom",qa:["blackbox","whitebox"],gates:[G0]}；均通过 REQUEST route 和当前 Ask 的 DECIDE confirm 实跑。前后 READ show/next 的 guarantee/coverage、requirement revision、preflight、projection、requiredSources、route 与 activation 记录。每份独立 S3 已有相应技术审计划及同 digest 的 topology 确认，不复用先前已消费的 Ask。
oracle: 四种合法组合显式激活 guarantee，所列绑定均属于同一次确认；两个 QA 模式按并集计算覆盖。不能仅由 route 字符串或文件形状隐式产生激活。
shared steps: env, protocol, fixture
primary AC bindings: AC-009
review status: PASS

CASE-014
mode: blackbox
description: 缺技术审或拓扑时路线本身不能提前绑定
procedure: 每行采用单独正常前态：S0 未预检、S0 预检但未确认、S1 尚未完成产品审、S2 产品审 PASS 而技术审未 PASS、技术审 PASS 但尚未确认 topology。对每行分别以独立副本提交 REQUEST route={kind:"full",qa:[],gates:[]} 和 route={kind:"custom",qa:["blackbox"],gates:[]}，保存请求前后 regular.route/guarantee/requiredSources、revision 与 executions。另以完整 S3 相同命令成功选择路线作对照；缺 projection 等纯内部状态组合不通过手改运行态或虚构 activation 入口制造。
oracle: 五类正常缺前置中两种路线都不能绑定，保证仍未激活，requiredSources 与业务授权没有部分新增；不能出现 route 已生效而保证失败的中间状态。仅完整 S3 的对照能原子绑定路线并激活保证。失败前后按请求前已存在的状态比较，不把准备阶段本身的合法 revision 变化记为失败写入。
shared steps: env, protocol, fixture
primary AC bindings: AC-009
review status: PASS

CASE-015
mode: blackbox
description: 覆盖投影逐 REQ/AC 展示实际执行与授权继承，非保证不报完整 PASS
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。四个独立 run。A 使用已批准/FROZEN 的 case-set-applicable，实际只完成 a，并以合法 CHECKPOINT 保存 a 的完整输出，remaining=[b.caseId,c.caseId]，READ show/next 的逐 REQ/AC 覆盖；再真实完成 b/c 并提交完整结果。B 先真实完成该 QA target 的全部用例 PASS 并结算，然后由 USER_INITIATED_CHANGE 的 requirement-change/apply-requirement 作语义保持的说明修订，impact 将该 QA target 标 unaffected、keep=[该实际 QA target 名]，当前 Ask 确认后 READ。keep 是 target 级，不能虚构只继承 b、a/c 继续同一已保留 target 的选择器。C 从合法 S3 选择 custom qa=[]、gates=[G0] 并推进终态，保存非保证覆盖；D 从有 case-set-applicable 的 qa-recovery 实际 Ask 选择 waiver，保存原缺口/waiver 及最终保证。各逐点保存 source/REQ/AC/case、当前实际执行证据与原 PASS/USER_KEEP 绑定；不提交客户端构造的继承 receipt。
oracle: A 的 a 只有实际完整证据才记 executed PASS，b/c 未执行时对应 AC 不闭环、整份保证非 PASS；补齐且全部通过后才闭环。B 的用户保留来自已有完整 target PASS，投影标明授权继承和原证据，不冒充新 revision 实际执行。C 不含 QA、D 为 WAIVED，均不能显示完整需求保证 PASS；各状态与实际对应逐 REQ/AC 可追溯。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-010
review status: PASS

CASE-016
mode: blackbox
description: 初次产品审通过后才签发技术审，非法候选只结束 attempt
procedure: S1 初选 regular 后在产品审无 PASS 时通过 REQUEST jump target=start-readiness 与 HOST not-issued-start-readiness 尝试提前技术审，记录零技术审签发；再用公开 fixture 方法提交完整产品 REVIEW_RESULT、结算 SOURCE 并 ADJUDICATE。对技术审四种结果负例分别新建独立 S2：review.findings[0] 缺 category；category="implementation-detail" 且 severity="P1"；review.candidate 与 claim.candidate 不同；review.complete=false。所有其他字段均按公开 review/B/finding/granularityReview 结构完整填写，特别是技术计划不能留空。逐行 SUBMIT REVIEW_RESULT 并结算 SOURCE，再 READ 查看 rejected receipt、finished/attempt 和 series。不在同一 TaskKey 无授权连续提交四次。
oracle: 产品 PASS 前技术审零签发，PASS 后可进入技术审。无效候选只生成 rejected receipt 并结束 attempt，不产生有效 finding/PASS/FAIL，不增加语义轮；不得把格式错误当产品 blocker。
shared steps: env, protocol, fixture
primary AC bindings: AC-011
review status: PASS

CASE-017
mode: blackbox
description: 三类技术阻塞与完整 finding Ask 由核实后用户处置
procedure: 准备技术审候选各含一个三类合法问题：需求实质冲突、职责/依赖方向根本错误、核心验收缺入口。SUBMIT 原候选后读取 Operator；提交分别核实的范围、定级、因果、正常路径复现及 remedy，再读取 Ask；为唯一完整 remedy 选确认并绑定该 remedy。 每类问题使用新 P/R 的当前 start-readiness claim，REVIEW_RESULT.review=该 claim 的 B 加 action="start-readiness"、status="FAIL"、complete=true、完整 granularityReview 和 findings=[一项完整字段]；category 依次为 requirement-conflict、responsibility-dependency、acceptance-entry。这些问题文本与核实材料明确是隔离前置，分别写“同一输入 Alice 被确认同时要求 Hello 与 Bonjour”“B2 要用 B1 输出而计划反向依赖 B2”“确认的 -h 输出验收在方案中无入口”，severity=P1，并给出唯一完整 remedy。SOURCE 结算后 ADJUDICATE 绑定实际 resultDigest、逐 finding VALID、同 category、非空 completenessEvidence/reason/evidence。REQUEST finding(reviewAction="start-readiness",findingId=实际ID,resultDigest=该digest,reason="隔离前置处置") 再 DECIDE confirm；确认后通过受支持 requirement-change/apply-requirement 落实对应修订并重新确认全文，观察 fresh review；不得把 DECIDE confirm 本身当作已经修改 requirements.md。
oracle: Controller 不自行猜语义；每个有效 Ask 有问题、正常后果、定级理由、remedy。唯一 remedy 可一次确认；确认 P0/P1 修订后要求 fresh review。原 raw candidate、裁决和授权分层保存。
shared steps: env, protocol, fixture
primary AC bindings: AC-011, AC-012
review status: PASS

CASE-018
mode: blackbox
description: 驳回 blocker 直接推进，P2/P3 处置不因等级触发复审
procedure: 独立前态 A 仅一个已核实 P1，用户驳回；前态 B 仅 P2/P3，分别确认完整 remedy 或有两个实质选项时进入澄清再选；完成处置后 DRIVE，统计新审查动作。 所有 finding 前态均以完整 REVIEW_RESULT→SOURCE 结算→ADJUDICATE VALID 准备。用户处置入口为 REQUEST finding(reviewAction=持有该结果的 product-review 或 start-readiness,findingId,resultDigest,reason)，DECIDE 只用当前 Ask 的 dismiss/confirm/clarify。P2/P3 确认行采用不改需求和主要方案的表述/实现说明建议，不能一边实质改需求一边断言不复审；两选项行先 clarify，保存未解除处置的状态，再重新以当前 Ask 选择最终 remedy。各行独立 run，统计实际新签发审查数及 series 变化。
oracle: A 无其余 blocker 时直接推进；B 的多选问题先澄清但等级本身不产生 fresh review；语义结果/处置完整留痕，不新增不必要审查轮。
shared steps: env, protocol, fixture
primary AC bindings: AC-012
review status: PASS

CASE-019
mode: blackbox
description: 合法纠正保留原候选并实时改变有效阻塞
procedure: 正常隔离前态中保存一个完整结果 R0 的两个稳定 finding IDs f1/f2；Operator 当前裁决 f1=VALID/P1、f2=DISMISSED/P1。先提交 CORRECT.correction={action:实际reviewAction,resultDigest:R0.currentDigest,findingId:f1,oldDecision:"VALID",newDecision:"DISMISSED",oldSeverity:"P1",newSeverity:"P1",reason:"隔离纠错：核实正常路径不存在阻塞"}，外层 expectedRevision=当前 show；保存新有效集合和 next。再对 f2 提交相反的 DISMISSED→VALID、P1→P1 与有依据的非空理由，保存依赖失效。最后把第一次 correction 用新 event ID 但过期 oldDecision="VALID" 再提交，expectedRevision 更新当前值以单独验证旧值冲突；另一独立副本只把 expectedRevision 改为旧值验证 revision 冲突。所有调用保留原始结果、corrections 历史和返回 receipt。
oracle: 清除唯一误阻塞后 next 立即推进；新增阻塞令依赖失效。raw 候选与每次纠正历史仍可查，finding ID 不变；旧值/修订冲突纠正拒绝且不覆盖新决定。
shared steps: env, protocol, fixture
primary AC bindings: AC-013
review status: PASS

CASE-020
mode: blackbox
description: 产品修订影响技术前提时先真实新产品审再新技术审
procedure: 在一个尚未选择路线的 run 用真实 HOST 完成初次产品审与技术审，逐条 ADJUDICATE 后保存两个原生 ID 及原文输入。通过 REQUEST requirement-change(origin=USER_INITIATED_CHANGE,reason="问候正文更改同时影响技术输出前提") 建立修订屏障；将 requirements.md 唯一 Hello Alice 改为 Hi Alice，计算完整新 Document revision；REQUEST apply-requirement 提交 document、meaning="changed"、同 origin、impact={"product-review":"affected","start-readiness":"affected"}、keep=[] 和理由，按生成 preview/Ask 完成 DECIDE。DRIVE 后实际 HOST 新产品审，尚未有有效产品 PASS 前再次请求 jump 到 start-readiness 并记录拒绝；新产品 PASS 与裁决完成后实际 HOST 新技术审。保存每个 worker 的完整规范输入与原生会话初始化记录。
oracle: 新产品与新技术均是全新零上下文身份；都读取完整最新 requirements.md 而非局部 diff。顺序为新产品 PASS 后新技术，series 继承原轮数，未获授权不能越额度。
shared steps: env, protocol, fixture, host
primary AC bindings: AC-070
review status: PASS

CASE-021
mode: blackbox
description: 技术修订改变产品语义时回完整产品审，明确保留例外生效
procedure: 建立两个独立 run，均先真实 HOST 完成原产品/技术审并裁决，停在尚未选路线的正常阶段。通过 USER_INITIATED_CHANGE 修订把空输入的退出码 2 改为 3，说明技术错误处理变化同时改变用户可见语义；在 apply-requirement 中提交完整 Document、meaning="changed"、impact={"product-review":"affected","start-readiness":"affected"}。A 的 keep=[]；B 的 keep=["product-review"]，分别按各自完整 preview/Ask 确认。A 实际 HOST 新产品审并 PASS/裁决后才执行新技术审；B 读取 USER_KEEP receipt 后执行仍需的新技术审。每次保存原/新完整文本、原生 ID、当前 series 计数与保留目标名，不能把 result ID 当 keep 值。
oracle: A 先全新完整产品复审再受影响技术审，不能直接技术自循环；B 只有绑定的保留项可免对应复审，其余义务仍执行。两条链路不清零产品/技术 series。
shared steps: env, protocol, fixture, host
primary AC bindings: AC-038, AC-070
review status: PASS

CASE-022
mode: blackbox
description: 语义轮与 attempt 独立计数，默认第三次无授权不得启动
procedure: 准备 product-review 当前 TaskKey 的 attempt 1，CLAIM/SOURCE 准备仅作隔离前态。提交 status="RUNTIME_ERROR" 的完整 review 错误报告并结算 SOURCE；DRIVE 取得同 TaskKey 的 attempt 2，提交 complete=false 的不完整 review 结果并结算 SOURCE。每次 READ 保存 attempt 与 review series。再次 DRIVE 观察耗尽且没有 attempt 3。请求 REQUEST authorize-retry(reviewAction="product-review") 但省略 reason，保存拒绝；合法 REQUEST 使用 reason="本隔离 product-review 已耗尽两次 attempt，追加一次完成有效结果登记" 取得 Ask 后，先 DECIDE 使用过期 expectedRevision，其余 requestId/token/digest 正确，保留 rejected receipt；再重新 READ，以当前 revision、当前 ask.id/token/digest、choice=confirm 合法 DECIDE，不重复填写 decision.reason（该普通 authorize-retry 的非空理由已由所绑定 REQUEST 提供）。DRIVE 取得 attempt 3，提交完整有效 review，结算 SOURCE 并 ADJUDICATE。若非法决定消耗了 Ask，则只重新请求同一未放行授权，不能额外成功授权后再比较额度。
oracle: 前两次 attempt 终结但语义轮不增；第三次无授权为 RETRY_EXHAUSTED/Ask，绝非无期限 Wait。错误授权零放行；合法追加仅给本动作一个 attempt，有效完整审查只加一轮。
shared steps: env, protocol, fixture
primary AC bindings: AC-014, AC-015
review status: PASS

CASE-023
mode: blackbox
description: 修订、carry、外部采纳和纠正不清零已用审查轮
procedure: 准备两个 reviewAction 已各完成2轮、completed wave=1 的正常隔离前态并保存计数/历史。四种变化用独立副本：通过 USER_INITIATED_CHANGE 的 requirement-change/apply-requirement 及当前 Ask 确认；通过真实纯门 P2 REPAIR_RESULT 完成与 DRIVE 自动 P2_CARRY；对实际外部 Git 新提交 F 使用 REQUEST adopt-external(external.identity=F,lineage=original base,diffDigest=真实 git diff --binary 摘要,impact/reason) 并确认；用 CORRECT 对实际 currentDigest/findingID 的旧新值作合法纠正。每项只比较该变更已接纳、后续新的语义审查结果尚未完成的时点。不能调用旧 carry 命令或把后来确实完成的新审查轮次误当计数重置。
oracle: 已有两个 series 与完成 wave 保持原值；不需要新鲜审查的纠正/carry 不增加轮数，需求变化不能隐式清零或授予下一轮。
shared steps: env, protocol, fixture
primary AC bindings: AC-014
review status: PASS

CASE-024
mode: blackbox
description: 第四轮审查授权专属于动作且不能累计预授权
procedure: 产品和技术分别用独立前态准备该 reviewAction 已完成 3 轮且当前确需新鲜审查，保留 source/裁决和计数前置；分别 REQUEST authorize-round(reviewAction=各自动作,reason 非空) 取得两份当前 Ask。先在技术 run 对其 SUBMIT 一个复制自产品 run 的 authorize-round DECIDE(id/token/digest 全取产品 Ask)，只更新外层 eventID；保存拒绝及技术计数不变。随后在产品 run 对其已有当前 Ask DECIDE confirm/reason，原样重放该决定，再完成实际第四轮有效审查及裁决。重新 REQUEST review-again 后记录第五轮仍被额度限制；技术动作仍需其自己的 authorize-round，不能消费产品授权。
oracle: 错动作授权拒绝；合法授权只使产品第 4 轮可签发，重放不新增额度；技术审和产品第 5 轮仍各需新授权，series 与 attempt 账目区分。
shared steps: env, protocol, fixture
primary AC bindings: AC-015
review status: PASS

CASE-025
mode: blackbox
description: 相同 OPEN 续用、相同 PASS 去重，明确重审有理由且保留原结果
procedure: 在独立 run 的 product-review 已签发且未完成 OPEN 前态连续 DRIVE 两次，保存 action/attempt；用完整 review PASS→SOURCE 结算→ADJUDICATE 完成该轮，再 DRIVE，保存旧原始结果与 series。REQUEST review-again(reviewAction="product-review") 缺 reason 必须拒绝；合法 REQUEST 含非空 reason 但停在 Ask，不 DECIDE，DRIVE 证明尚无新审查。然后当前 DECIDE confirm，观察下一轮且旧 PASS/history 保留。超轮数负例使用另一独立副本的已完成三轮前态，REQUEST review-again 含理由但不作 authorize-round，观察没有第四轮；不能把超额度的同一请求与有剩余额度的合法重审混为一个连续前态。
oracle: OPEN 使用原 dispatch/attempt；PASS 不自动重派。非法重审不新增 TaskKey；合法重审保留原 PASS 和 history，消耗相应动作额度，新结果不能伪装旧结果。
shared steps: env, protocol, fixture
primary AC bindings: AC-016
review status: PASS

CASE-026
mode: blackbox
description: 黑白盒 pre-wave 三连 FAIL 分别产生完整处置 Ask
procedure: 黑盒和白盒分别新建独立 preparation 前态，沿公开 PREPARATION_RESULT.review 提交完整逐项 FAIL，再按当前自动重设计/新审查路径提交新 design 并复审，连续三次语义 FAIL；每轮都结算 SOURCE。另一对独立前态针对一个同模式 TaskKey 连续报告两次 RUNTIME_ERROR（每次使用实际当前 claim，结束来源并取下一 attempt），不把长期不可用解释成无界等待。DRIVE 保存直接产生的 qa-recovery Ask，包括 qa.mode/task/requirementRevision/candidate/caseSetDigest/resultDigest/consecutiveFailures/retryExhausted 及 options。对已有设计集的一份选择 choice=waiver，reason 非空且 id/token/digest/revision 当前；随后通过合法 qa-revise 改输入并尝试重放旧 waiver。不同 Ask choice 的行为由各独立副本验证，不能一次决定后继续消费同 Ask。
oracle: 每模式第三次 FAIL 后 options 至少包括 redesign/fresh-review/change-requirement/waiver/skip/abort；两次运行错误耗尽另含 retry。黑白计数独立，语义 FAIL 与 attempt 不混计，不增加开发后 completed wave。waiver 只绑定当次精确输入，保留原 FAIL，不能新增批准 whitelist 或执行 PASS，保证明确 WAIVED；新 case-set 不被旧授权豁免。无 case-set 的前态不能选择 fresh-review/waiver/skip；有限 retry 一次追加再失败重新 Ask。
shared steps: env, protocol, fixture
primary AC bindings: AC-058
review status: PASS

CASE-027
mode: blackbox
description: 第三个失败 wave 后只允许一次额外返修且 Ask 信息齐全
procedure: 准备 completed wave=2 的 run，实际提交第三个完整 expected-set 结果、错误处置和逐条裁决使仍有问题；DRIVE 保存 Ask。逐字段核对 findings/problems、核实/归并理由、历史、QA scope、TaskKey/signature、已用/剩余和 cost/guard；授权一次额外 repair 后模拟完成第四个失败 wave，再 DRIVE。 具体授权入口为 REQUEST authorize-wave，target=当前 wave.id，keep=本次精确 OPEN problem IDs，reason 非空；保存 Ask.options 后 DECIDE choice="additional-wave" 且附理由。其他四种选择必须各自从同一真实可达 Ask 的独立副本执行：retry-qa、waiver、change-requirement、abort，不能在一次已消费 Ask 上依次切换选择。新增一次失败 wave 后重复 REQUEST authorize-wave，绑定新的 digest/revision 与实际问题集合。
oracle: 第三轮完成才计 wave=3 并阻止未授权 repair；Ask 五类选择完整：一轮额外修复、再试 QA、精确 obligations waiver、需求变更、abort。授权绑定 digest/revision，仅一轮，第四轮失败重新 Ask，不能累计。
shared steps: env, protocol, fixture
primary AC bindings: AC-050, AC-059
review status: PASS

CASE-028
mode: blackbox
description: 不完整 wave、错误未处置和纯 P2 修复不误计轮
procedure: 独立 S5 副本分别提交缺一个 expected target、RUNTIME_ERROR 未处置、未完成 Operator 裁决、需求修订打断 wave、完整纯门 P2 修复；每个阶段 READ 完成轮数并尝试开 repair。 纯 P2 行的计数基点是原完整 wave 已收齐且已裁决之后、P2 repair 执行之前；比较该 repair 的 REPAIR_RESULT/真实提交/P2_CARRY 前后 completed wave，不把首次 wave 的正常完成误判为 P2 修复新增一轮。其余四行的每份 SUBMIT/WAVE_ADJUDICATE 都复制当前 claim/wave/target/resultDigest，结算 SOURCE 后再判断是否仍未收齐。
oracle: 前四种未完成 wave 不计轮，不满足前置不能开 repair；纯 P2 carry 完成也不增加 wave；已有完成轮数保持，不能因未收齐被误当通过。
shared steps: env, protocol, fixture
primary AC bindings: AC-059
review status: PASS

CASE-029
mode: blackbox
description: 已确认完整 Batch 计划是首次开发前置
procedure: 从 S2 当前已签发技术审开始，以 REVIEW_RESULT.review.granularityReview 提交两 Batch 计划：B1 subtasks=[S1,S2]、dependencies=[]；B2 subtasks=[S3]、dependencies=[B1]。按文档每 Batch 填 task/interfaces/stateBoundary/risks/definitionOfDone/validation/recovery/handoff/contextCost，requirementRevision 复制 claim；完成技术结果 SOURCE 结算与 ADJUDICATE。此时先不确认 topology/plan，DRIVE 并记录开发未签发；再 REQUEST topology(planDigest=刚读 developmentPlan.digest,reason="两个顺序批次无需拆分")，DECIDE 后 REQUEST/DECIDE route。四种非法计划各从新的独立技术审 attempt 前态开始，仅分别移除 subtasks、重复同 Batch/Subtask ID、把依赖改成 B1↔B2、清空 definitionOfDone；通过 REVIEW_RESULT 实际提交并观察拒绝。不能在已经选择路线后用一个不存在的公开 plan 入口补首次计划。
oracle: 技术结果里的完整计划先经裁决形成公开 digest，只有绑定该 digest 的 topology/计划确认且路线确认后才解锁开发。四份无效计划不得形成有效技术 PASS 或开发计划确认，拒绝指出具体空项/重复 ID/环/DoD。首次未确认或旧 planDigest 的请求无开发签发；最终确认绑定当前 requirementRevision、solutionRevision、topology 与 route，B2 依赖 B1。
shared steps: env, protocol, fixture, batch-fixture
primary AC bindings: AC-017
review status: PASS

CASE-030
mode: blackbox
description: 批边界只按完成、自测和交接解锁，门统一在全部开发之后
procedure: 独立采用 batch-fixture 的技术审两批计划、SOURCE/ADJUDICATE、topology 确认及 custom blackbox+G0 路线。真实 B1 worker 在 S1 文件已编辑而 S2 尚未完成时返回 PREPARATION_RESULT status=CONTINUE，manifest 为实际变动且 validation 不声称 S2 PASS；SOURCE 收口后 DRIVE，保存 B1 继续及 B2 未签发。随后续用 B1 完成全部构建/三个 CLI/三个测试，PREPARATION_RESULT PASS 的 validation 分别含 S1/S2 实际 procedure/observation/result，handoff="无"，SOURCE 结算，再 DRIVE 真实 track/commit。此后启动 B2 并完成 S3 的 README 与实际命令核验，在其 PASS/结算/真实提交之前后分别 DRIVE。另保存黑盒审查未批准、选中 QA 未 FROZEN 或候选尚未装配各屏障时点；只有实际屏障都完成后才运行 DRIVE 统一签发验证。
oracle: B1 未完成 DoD/验证/交接前 B2 零签发；完成后直接解锁 B2，无组间质量门。最终屏障前门零签发，之后所选门统一比较原 B→最终 Q；批边界记录不是门 PASS，不增加 repair wave。
shared steps: env, protocol, fixture, batch-fixture
primary AC bindings: AC-017, AC-071
review status: PASS

CASE-031
mode: blackbox
description: 开发与基线黑盒真实并发且输入隔离，白盒等待全部开发
procedure: S4 用真实宿主同时运行开发 Batch 与黑盒 qa-design/qa-review，记录各自 native 起止区间和规范输入；开发工作区做实际提交前编辑时读取黑盒工作区 git HEAD/status 和需求文件；开发未完成时尝试白盒设计。
oracle: 开发/黑盒实际运行时间有交集，黑盒没 PASS 不妨碍开发开始；黑盒 HEAD 恒等 B，只含注入当前需求，不含实现/白盒/开发解释。开发只收到已确认范围和任务薄指针；白盒提前请求被拒，快照仍等待两侧完成。
shared steps: env, protocol, fixture, host, batch-fixture, task-operation, native-evidence
primary AC bindings: AC-018
review status: PASS

CASE-032
mode: blackbox
description: Git manifest 合法时 adapter 按范围校验、track、commit、snapshot 真实执行
procedure: 实际 development worker 只在 regular.preparation.resources.development 编辑 README.md 与新增 greet.txt，返回 manifest=["README.md","greet.txt"] 及每个 Subtask 的实际 validation，不执行 Git 写入。结算该结果/source后，以启用 Git 原生 Trace2 的隔离调用环境实际运行 DRIVE；GIT_TRACE2_EVENT 指向 E/git-trace.jsonl，用来记录真正 git 进程的 argv、开始/退出和结果，不替换 git 程序或伪造计数。读取新 preparation receipt 与 DevelopmentSnapshot；在开发工作区执行 git status --short、git diff、git ls-files --others --exclude-standard、git log、git rev-parse HEAD、git merge-base --is-ancestor B HEAD，并保存结果。已签发 Git 副作用由 DRIVE 执行，不以 --action-id HOST 冒充 Git 入口。
oracle: 顺序严格为 manifest/范围与未解决状态校验→track→commit→snapshot；新文件纳入提交，Q 可从 B 追溯，manifest 外路径未混入；所有 VCS 写操作归 adapter，snapshot 指向真实提交。
shared steps: env, protocol, fixture, host
primary AC bindings: AC-019, AC-076
review status: PASS

CASE-033
mode: blackbox
description: 常见 manifest 漏项与选错项目路径在 Git 提交前拒绝
procedure: 仅修改本例不成立的开发工作区手工 Git 前态。A/B 复用原独立 S4/B1 脚本：A 普通写入 greet.txt 同时修改 README.md，但 PREPARATION_RESULT.manifest 仅报 README.md；B manifest 加入 "../other-project/out.txt"，其余项目改动完整。完整 PREPARATION_RESULT/SOURCE 通过公开协议提交，DRIVE 实际运行 Git adapter，保存 HEAD、git status --porcelain、git ls-files --stage、GIT_TRACE2_EVENT 和全部回执；拒绝后按当前有限 retry/修正结果路径，A 补全 manifest、B 去掉越界项，再 DRIVE 真实提交并核对交付路径。原 C 手工在开发副本 commit/merge 制造冲突的证据保留为不成立前态，不继续执行或记产品 FAIL；本例不再要求开发者绕 adapter 执行 Git 写入。真实用户工作区冲突的 promotion 拒绝由现有对应 Git 冲突用例承担，其结果独立引用，不称本例重新执行。
oracle: A/B 拒绝分别定位漏项及越界，拒绝时没有成功 commit/新 snapshot，HEAD 不变；修正后由 adapter 完成包含全部授权交付物的真实提交。手工开发副本 Git 冲突不作为正常路径证据；不得将被移出的不成立 C 行记 PASS。
shared steps: env, protocol, fixture, fa5-reuse
primary AC bindings: AC-019
review status: PASS

CASE-034
mode: blackbox
description: Batch 契约变化使旧计划与未完成派发失效，旧确认不能放行
procedure: 在有未完成 B1 的 S4 先完成/对账当前 Git operation，再 REQUEST plan-change(reason 明确说明需变更接口、依赖或 DoD)，确认其 typed Ask。保存旧 planDigest、原 unfinished action 和原 native identity；实际用原 action 调用 HOST 并重交旧 topology 确认事件。沿公开流程重新取得技术审任务，在其 REVIEW_RESULT.granularityReview 中提交更新契约，结算并 ADJUDICATE；用新 developmentPlan.digest 做 topology 确认，再按可用动作恢复路线/开发。接口、依赖、DoD 三种变更各在独立正常前态执行，不直接编辑内部 plan/state 或把改编号当新 Batch 身份。
oracle: 修改即暂停受影响开发，旧计划/未完成动作失效；旧 revision 确认不解锁新计划；新计划完整绑定后才继续，不清除已完成有效证据或重置额度。
shared steps: env, protocol, fixture
primary AC bindings: AC-060
review status: PASS

CASE-035
mode: blackbox
description: 同一 Batch 正常返回、子步骤切换和自测失败均续用真实原生身份
procedure: 采用 batch-fixture 的 B1/S1/S2，真实 HOST 创建 H1。批计划明确 S1 写实现后以 CONTINUE 返回实际已做/剩余项与 manifest；结算后从新已签发 claim.operation="resume" 的 action 用 HOST --resume。S2 第一次实际运行故意不满足的临时 TestGreetAlice 断言并返回 FAIL/真实输出，然后通过当前 TaskKey 有限 retry（耗尽时 preparation-retry+当前Ask理由）续原身份修正断言并实跑构建/测试。另独立同 B1 前态用 interrupt-evidence 的正常运行中取消/恢复。逐次保留实际原生 thread/start/resume、H1、validation、计数、原文件 diff；正常返回、语义自测 FAIL、RUNTIME_ERROR 不混写。
oracle: 全程 B1 的 native ID=H1，create 总数=1，后续为同身份 resume；Subtask 不单独换身份/reset。自测失败、正常返回和瞬时中断不重置已有产物、结果、attempt/修复上限。
shared steps: env, protocol, fixture, host, batch-fixture, task-operation, native-evidence, interrupt-evidence
primary AC bindings: AC-061
review status: PASS

CASE-036
mode: blackbox
description: 未完成同 Batch 的换人常见尝试在真实调用前拒绝
procedure: 从真实 B1/H1 未完成且已停止前态，DRIVE 得同 Batch 的后续 claim.operation=resume。负例各独立：省略 HOST --resume 试图 fresh；在完整 OP 请求 binding.operation 改 start；binding.identity 换另一个本例创建的真实 H2；在 API request 额外添加 caller reason="不可恢复"；通过真实有限 preparation-retry 获一次 attempt 后仍省略 --resume；合法 plan-change 只改 Batch 编号、重新技术审/拓扑确认后省略 --resume。保留其他当前字段，实际调用且独立 native create 差分。把实际 FAIL 错写成 RUNTIME_ERROR 的情形只准备明确隔离结果前态，验证该分类本身不解除已有 H1；不冒充真实自测错误来源。重复已确认计划不应创造替换权。
oracle: 所有没有真实不可恢复证据的替换 create 增量=0；原 H1、已用额度、有效产物/结果不重置。网络超时/限流/unknown 仅 Wait/Operator/Ask/有限恢复，不能被理由或编号改写为不可恢复。
shared steps: env, protocol, fixture, host, batch-fixture, task-operation, native-evidence
primary AC bindings: AC-061
review status: PASS

CASE-037
mode: blackbox
description: 真实不可恢复证据且旧执行停止、副作用已对账才允许一次替换
procedure: 只在本例隔离 CODEX_HOME 中真实建立未完成 B1 的 H1，正常停止旧 execution 并保存 PID 已止、完整 HEAD/status 与无未决副作用。用同一原生 Codex 启动 [codex,"app-server"] 的 stdio JSON-RPC：先发送 id=1 method="initialize" params={clientInfo:{name:"qa-native-delete",version:"1"},capabilities:{experimentalApi:true}}，收到成功后通知 method="initialized"；再发送 id=2 method="thread/delete" params={threadId:H1}，保存真实空对象成功应答并关闭该辅助 app-server。只删除本例新建 H1，不操作真实用户历史。此公开参数已由 codex-cli 0.145.0 的 generate-json-schema --experimental/ThreadDeleteParams 只读确认；执行时先保留实际版本/原生帮助。随后由产品 HOST --resume/DRIVE 实际 query H1，保存原生 no rollout found for thread id H1、developerRecovery 的 workspace/旧action绑定与一次替换 intent，再执行实际已签发替换。四个独立负例用缺证据、错 Batch/旧 revision 的 OP 或旧完整请求重放，不能客户端造 DEVELOPER_RECOVERY 成功回执。新 H2 与重复请求的 create 计数均从原生来源读取。
oracle: 正例有真实 resume/query 的明确不可恢复输出，替换创建恰好一个 H2≠H1 且绑定 B1/证据；重放不再创建。四种无效证据零 create；新 B2 只有前批 DoD 完成后另建全新零上下文身份。
shared steps: env, protocol, fixture, host, batch-fixture, task-operation, native-evidence, interrupt-evidence
primary AC bindings: AC-061
review status: PASS

CASE-038
mode: blackbox
description: 跨 Batch 使用新身份且交接仅为已提交产物和必要任务书
procedure: 本例局部覆盖 batch-fixture 的 validation 输出路径：在 B1 实际授权开发 cwd 运行 argv=["go","build","-o","qa-greet", "."]、["go","test","./..."]，再以 cwd 内 qa-greet 的绝对路径执行 Alice/空/-h 三次并逐字比较 fixture 输出；qa-greet 是自测临时产物，运行结束普通删除，不列入提交 manifest。E 仅由外层执行者收集真实 stdout/stderr/退出码，不要求原生 worker 写授权 cwd 外 E。已开始 B1 保留 H1 和代码/自测事实，按公开 plan-change 当前 Ask 确认仅此可执行性修正（若需重新技术/拓扑按真实投影完成），继续同 Batch 原身份；不得重置成新 H1。若该 run 的计划绑定不能合法修订且保留身份，记录前置未成立，不手改计划/state。实跑 B1 完成并由 adapter 提交后，执行 B2；保存 H1/H2、上下文初始化事实、两份规范提示词、交接 manifest 和交接文档。核对 B2 输入只含确认任务、已提交路径和不能从仓库恢复的理由/债务/下一任务。 全部通过真实 HOST；B2 必须在 B1 PASS/完整validation/handoff 与真实 Git提交后才签发，不能沿用手造 S4。对 H2 原生 thread/start 的初始参数和规范文件全文核对不携 H1 history/path；H2 transcript 的首任务输入与已提交交接对应，没有复制 H1 转写。
oracle: H2≠H1，B2 为全新零上下文会话；没有 B1 聊天历史/开发解释复制，也不按 Subtask 重置代理；全部交接产物在 B1 提交可读取，未提交材料不作为交接事实。
shared steps: env, protocol, fixture, host, batch-fixture, task-operation, native-evidence, fa5-reuse
primary AC bindings: AC-061
review status: PASS

CASE-039
mode: blackbox
description: 多对多覆盖可通过，非 AC source 可显式非 QA 处置
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。独立使用 case-set-applicable；在 blackbox 的 a 里声明 command={executable:"python3",args:["qa-entry.py","--qa-alice-and-help"],expectedExitCode:0}，启动适配器顺序真实调用 Alice 与 -h，oracle 比较组合 stdout 精确 Hello Alice\nusage\n、stderr 空和 exit=0。其 edges 同时覆盖 AC-001/AC-003；b 仍覆盖 AC-002，c 独立 -h 用例也覆盖 AC-003。通过 PREPARATION_RESULT.design 实际提交完整 sources/points/cases/edges/sharedSteps；design-notes.md 按共享说明 alternatives 的实际读取证据作 NON_QA 处置。SOURCE 结算后按实际 reviewScope 完整逐项提交审查，并 READ coverage。本例验证合法多对多，不得只增加 a→AC-003 的边而让 a 的步骤/断言仍只测 Alice。
oracle: 合法多对多不被错误拒绝；每个 AC 至少一个批准 QA case，DOC-001 明确记录非 QA 处置。requiredSources 绑定同一次确认，没有自动猜测 source/point 决策。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-020
review status: PASS

CASE-040
mode: blackbox
description: 缺失或错误覆盖不能由集合 PASS 或非 QA 处置补齐 AC
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。case-set-applicable 独立副本分别提交未知 point、重复映射、遗漏 AC-003、orphan case、未批准 case；分别只给 case PASS、只给集合 PASS、给 AC-003 非 QA 处置后请求冻结。 具体改动对象为 design.manifest.points/edges/cases 或 review.pointDecisions/sourceDecisions/caseDecisions；每个样本其余数组保持与当前 design 完整一致。集合 PASS 负例使用文档允许的 review.setStatus="PASS" 但省略一项必需逐项决定，不能发明 set-level 代替入口；非 QA 负例把 requirements.md 的 AC-003 只放 alternatives 而不创建 point/edge/case。通过 PREPARATION_RESULT 实际提交并 READ 取得具体 unbound/未批准 ID。
oracle: 所有缺口在 coverage/冻结明确列出具体 source/point/case，未形成 FROZEN/guarantee PASS；合法 DOC-001 例外不能用来跳过 AC 的批准 QA case。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-020
review status: PASS

CASE-041
mode: blackbox
description: 当前覆盖结果必须完整绑定五类来源/候选摘要
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。先保存被测 toy 的 requiredSources、design.manifest（完整 sources/points/cases/edges/sharedSteps/binding）、批准 whitelist、候选 Q、已签发 claim.payloadDigest/prerequisiteDigest 与规范输入。对公开可提交的各关键绑定使用独立副本：design.manifest.binding.requirementRevision 写成旧 revision；design.manifest.sources 遗漏 requirements.md；edges 引用未登记 point/case；VERIFICATION_RESULT.candidate 换成原基线 B；verificationResult.bindingDigest 使用同候选旧 case-set 的已签发摘要。每份其他 B/结果字段完整且取当前 attempt；不得给 payload 增添不存在的 inventory/whitelistDigest 字段冒充逐字段测试。最后在独立合法前态提交所有引用一致、带实际 procedure/output/comparison 的完整结果。State/规范派发/结果之间的 inventory/manifest/map/whitelist/ValidationCandidate 逐一由公开投影和摘要引用对账，无法从公开面独立改写的内部绑定字段由白盒注入测试承接。
oracle: 每个旧版本、遗漏来源、错误映射、错 candidate 和旧 bindingDigest 都不能成为当前有效覆盖；错误回执与原提交可查，已存在 PASS 不被污染。正例的五类语义绑定在 State、规范输入及当前结果链上逐项一致，完整结果才接纳。不能把新增未知 JSON 字段被解析器拒绝误报为某个未提供字段的业务绑定已验收。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-021
review status: PASS

CASE-042
mode: blackbox
description: 模板、skipped 和无执行授权不算实际通过，环境错误有独立语义
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。每个负例独立从相同 approved/FROZEN QA action 前态。正例 a 真实 OP prepare/target/collect 后提交完整 verification 条目。负例分别将 case.status 写成 skipped、把 output/comparison 都写“符合预期”、或不调用任何 OP 却交缺真实 procedure/output 的 PASS，其他 binding/expected 数组完整；保存 result rejection/有效列表。环境行先在设计中把 a.command.executable 明确冻结成一个不存在的普通工具名 qa-missing-executable，步骤说明这是缺少工具环境，OP prepare 成功后 target 真正创建进程失败，collect 尝试不得执行，以实际 FAILED/message 和 RUNTIME_ERROR 结果记录环境失败，不能判语义 PASS。继承行另用已经真实全部 QA PASS 的前态，走 requirement-change/apply-requirement(meaning preserved,impact unaffected,keep=[真实QA target]) 和当前 DECIDE 生成 USER_KEEP，不客户端提交 receipt。
oracle: 只有真实完整 a 计 executed PASS；skipped/占位/无执行 PASS 不计实际执行且不能掩盖未完成；环境不可用为 RUNTIME_ERROR；保留可满足继承义务但明确不是本版本执行。
shared steps: env, protocol, fixture, case-set-applicable, task-operation, native-evidence
primary AC bindings: AC-021
review status: PASS

CASE-043
mode: blackbox
description: 单 case 修订局部失效，共享准备只审一次且逐例审 oracle
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。先在 case-set-applicable 的完整设计 a/b/c 与 shared setup 上完成一次 shared PASS 和逐例 PASS，保存所有审批与 scope。对 a 的 oracle 做明确错误改动：将其 stdout 预期 Hello Alice 改为 Bonjour Alice，其他 steps/source/AC 保持；通过 REQUEST qa-revise、提交更新 design 后，实际 HOST 派发独立 blackbox-review，让它读取完整当前需求和设计，不能预置 reviewer FAIL。保存其原生结果、逐 case 判断和对 a 的具体错误说明。修正 a 后重新提交 design/适用 Review，比较 b/c/shared 的未变审批；再仅把 shared setup 中构建输出路径改为另一个合法 E 子路径，提交该设计并 READ reviewScope/逐项审批。所有审批失效观察发生在新的独立 Review 实际完成前，避免已重审 PASS 掩盖失效。
oracle: 真实独立 reviewer 识别 a 的 Bonjour Alice 与 AC-001 Hello Alice 不符，不能仅靠模拟 review 结果证明错误 oracle 被发现。首次共享步骤仅一个审批但 a/b/c 都有独立判断；只改 a 时未影响 b/c/shared 的审批有继承证据。修改 shared setup 后，全部引用者 a/b 需要重审，c 的独立审批保持。若实际作用域传播与绑定不符或任一 case 没有逐项审查，判失败。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-022
review status: PASS

CASE-044
mode: blackbox
description: 建议需稳定 ID 和理由处置，影响执行的采纳重审相关项
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。在 case-set-applicable 的隔离 QA Review 前态用完整 PREPARATION_RESULT.review/suggestions 建立 s1/s2/s3：每项有 id、severity=P2/P3、problem、cases 与 changesAcceptance。先在独立副本试 REQUEST qa-suggestion(target="blackbox",findingId 为未知 ID,meaning="adopted",reason 非空)、合法 ID 但缺 reason，以及处置只完成两项后 DRIVE。正例依次提交 s1 meaning=adopted、s2=deferred、s3=not-adopted，均由 REQUEST 生成 Ask 后 DECIDE confirm/reason。s1 涉及 shared setup 变更时先 REQUEST qa-revise(target="blackbox",reason)，修改 design.sharedSteps.setup 并重新提交，实际 reviewScope 必须包含 a/b，完成对应逐项 Review。另以遗漏 AC-003 映射的设计及一条 deferred 建议尝试冻结，核对覆盖缺口不能被建议处置抹掉。ACCEPT/DEFER/DECLINE 仅为外层 driver 术语，不能作为被测 request.meaning 的值。
oracle: 完整处置且覆盖完整才 FROZEN；缺理由/未知/未处置均不放行。影响 shared 的 a/b 必须重审，c 可继承；真实验收缺口仍阻塞，不能用建议处置消除。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-023
review status: PASS

CASE-045
mode: blackbox
description: 已有有效测试复用由独立白盒判断并真实执行，无变动不造文件
procedure: 从 fixture 的真实开发快照 Dn 使用完整正确的 greet_test.go（三个实际 testRef）；保存 Dn 和 go test ./... 的真实全绿输出。到实际已签发 whitebox-design 的前态，调用 HOST，保留独立原生会话日志，核对先列行为/分支/oracle 再读取现存测试的实际工具顺序、reuse 项及 manifest。对返回 testRef 分别以 ["go","test","-run","^TestGreetAlice$","-count=1"]、TestGreetEmpty、TestHelp 同型 argv 在该候选 cwd 独立运行，保存 stdout/exit。design 的 testRef 必须与真实文件/函数对应，mode 使用被测 schema 的 WHITEBOX；随后实际 HOST whitebox-review，输入包含完整 Dn/现存测试，保存逐 case/test 的审查结果。无文件修改时继续 DRIVE 观察候选 identity/commit 数。
oracle: 覆盖判断早于复用检查，test=<实际文件>::<实际函数> 可定位且唯一，实际运行有独立输出。充分现有覆盖被复用，没有重复测试/空文件或无理由推进 snapshot；白盒审查包含现有开发者测试质量。
shared steps: env, protocol, fixture, task-operation, native-evidence
primary AC bindings: AC-024, AC-063
review status: PASS

CASE-046
mode: blackbox
description: 全绿但错误断言或缺分支由独立白盒识别，只补真实缺口
procedure: 在另一个正常 toy Dn 把 greet_test.go 改为仅 TestNonEmpty：调用 greet("Alice") 后只断言返回字符串非空，删除空输入/-h 相关测试；生产代码仍符合完整三个 AC。先实际 go test ./... 证明弱测试全绿，保存函数原文和真实输出。实际 HOST whitebox-design，完整需求不变，让独立 worker 先形成应测行为/精确 oracle，再读现存测试；保存其真实转写、coverageJudgment/reuse/manifest。实际 HOST whitebox-review 核对完整源与新测试；仅对应精确 Hello Alice/error("usage")/-h 输出的真实缺口需要补齐，返回的唯一 testRef 必须与文件/函数吻合。DRIVE 真实提交测试所有权变动后在 Qn 独立运行每个绑定函数，保存修订前后文件 diff、commit、退出码与输出；不得用预写 QA review PASS 代替本例独立判断。
oracle: 旧测试全绿不等于覆盖通过，Review 明确识别错误/缺失断言；仅补对应空输入及精确字符串缺口，测试由 adapter 纳入 Qn，真实运行输出可核验。
shared steps: env, protocol, fixture, task-operation, native-evidence
primary AC bindings: AC-024
review status: PASS

CASE-047
mode: blackbox
description: 合法 test-owned 文件含增删改名且不依赖 test 文件名判定
procedure: 在独立白盒工作区绑定实际 Dn 的 Go 测试项目中准备 testdata/helper.go 作为只被 *_test.go 通过 os.ReadFile 读取的文本 fixture，其完整内容为 Alice 加换行；生产 go build/go list 不把 testdata 编译或 embed 进生产代码。白盒新增该 fixture、修改测试读取与断言、删除已废弃 testdata/obsolete.txt、把 testdata/names.txt 重命名为 testdata/people.txt 并仅更新测试引用；所有这些文件名自身均无 test 子串，testdata 目录是 Go 文档化测试资源位置。白盒只编辑并返回精确 manifest；提交 PREPARATION_RESULT 后结算 SOURCE，再 DRIVE，由真实 Git adapter 归属核验、跟踪和提交。保存 Dn→Qn 全量增删改名、blob/mode、ownership 与 production-view receipt，并在两个候选分别真实 go build 与 go test。
oracle: 全部增删改名及摘要列全，helper 被证明 test-owned；production 代码/配置/依赖/构建输入/provider 元数据投影等价；adapter 执行 track/commit，白盒不写 Git。
shared steps: env, protocol, fixture
primary AC bindings: AC-062
review status: PASS

CASE-048
mode: blackbox
description: 生产视图变化及 UNKNOWN 不能伪装 test-only 进入候选
procedure: 各行独立白盒前态：修改被 main.go 实际调用的生产 contest.go；修改生产代码通过 go:embed 读取的 config.json；修改 go.mod 的生产依赖；修改生产 go:embed 的输入；新增一个不在当前 Go 测试发现/资源合同内且无法证明 test-owned 的文件。白盒仅编辑各自授权测试设计尝试材料、返回真实 manifest，经 PREPARATION_RESULT/结算 SOURCE/DRIVE 请求完成；保存原 Dn 与 ownership、blocked reason、实际 Git HEAD。文件名 contest.go 含 test 字符，仍由真实生产调用证明为生产输入。不能通过伪造 gitReceipt.providerMetadata、编辑 .git 元数据或发明公开参数制造 provider mismatch；该不可编辑元数据的完整参与判定由白盒直接测试。
oracle: 文件名含 test 不能越过 ownership；每一生产视图变化和 UNKNOWN 均转 development/repair 形成新 Dn，不直接认证 test-only Qn，旧 Dn 绑定和证据不被污染。
shared steps: env, protocol, fixture
primary AC bindings: AC-062
review status: PASS

CASE-049
mode: blackbox
description: 白盒 Review 与实现质量门在完整候选上职责明确分离
procedure: 从正常 toy Qn 同时保留开发者的 greet_test.go、独立白盒新增的 behavior_test.go 和 main.go/contest.go，并保存 Git adapter 的实际 testOwned 分类、完整 tree/diff 与 Dn→Qn receipt。选择 full 路线，按每个已签发 action 实际 HOST whitebox-review 及 catalogGates 中全部实现质量门；逐份保存规范提示词原文、绑定清单、实际消费日志和结果。对白盒核对完整 Qn 含两类测试与其实际 testRef；对门逐份列入 scope 的全部路径，逐路径与实际 testOwned 求交，并保存四个维度的当前 catalog 名称/说明。不按测试作者区别归属，不只抽一份门输入；不以工件文件名推测已经消费。
oracle: 白盒取得完整 Qn、所有测试质量和逐 case/test 引用；实现质量门排除全部 test-owned（不按作者分配），不含独立 QA 用例正文或执行证据；需求覆盖、实现偏差、架构耦合、实现正确性质量四维显式并列且输入 FULL。
shared steps: env, protocol, fixture, task-operation, native-evidence
primary AC bindings: AC-063, AC-065
review status: PASS

CASE-050
mode: blackbox
description: 同 QA TaskKey 复用准备，失败只阻止依赖用例，独立项继续实跑
procedure: 隔离已批准的 a/b/c fixture：真实 Q 的 Alice 分支错误输出 BAD\n并退出7，a 的批准 oracle 仍 Hello Alice\n/exit0；b.dependencies=[a.caseId]，c 为独立 -h。设计阶段先把 a/b/c command.executable 冻结为 E/shared-greet 的既定绝对路径，args/expectedExitCode 分别 Alice/0、空/2、-h/0。为本例共享非验收构建，在 Q 已冻结后以实际 git archive Q 到独立准备副本执行一次 go build -o E/shared-greet，记录 Q/tree、binary摘要、构建原始输出；正式执行使用该已冻结路径与参数，本例后续候选不复用该二进制。在同一当前已 attach QA TaskKey 按 OP prepare/target/collect 真实运行 a，记录 BAD 与7后判 FAIL；实际请求 b.prepare/target/collect 并记录依赖未完成阻断，各被拒步骤无对应过程/产物；c 仍完整三步实跑 PASS。分别保存每个 prepare workspace 和实际 stdout/stderr/exit；最终 verification cases=[a FAIL,b BLOCKED,c PASS]、remaining=[]，a finding.requirement=a.caseId，b.dependencies=[a.caseId]。同一错误样本校验短驱动比较必须报FAIL，不预置待测输出。共享 build=1，每个实际目标仍独立运行，副本相互独立。
oracle: a=FAIL，b=依赖未完成/未执行且不能 PASS，c 仍实际执行并 PASS；项目副本隔离。构建次数=1但每个被测操作各自执行，已知错误样例被脚本报告失败；构造前态不预置 a/c 待测输出。
shared steps: env, protocol, fixture, task-operation, native-evidence, native-attached
primary AC bindings: AC-025
review status: PASS

CASE-051
mode: blackbox
description: 同 action 的 checkpoint 恢复拒绝重跑已完成 PASS/FAIL，保留失败并仅补 remaining
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。按 boundary-checkpoint 实跑 a PASS/b FAIL/c remaining 并保存完整 CHECKPOINT；在绑定不变下停止本隔离宿主、取得原生停止事实，再实际 HOST --resume，READ 核对原 action/attempt/identity 和中断原因。先以同一 action 的 CLI OP 逐项请求 a.prepare/a.target/a.collect/b.prepare/b.target/b.collect，每项使用新 requestId 和刚读取的有效 revision，其余绑定当前有效，保存六次原始拒绝；随后由恢复的实际 worker 动态 formal_gates_operation 对相同六种请求各调用一次，保留原生 callId/响应，不把 supervisor OP 伪称下级调用。a/b 原始 command、候选、工具和环境未变，不提交 invalidated。每项前后读取 checkpoint、operations 和原副本 Git log/reflog/副本目录，核对无第二次目标提交。最后由同 worker 动态工具仅对 c 完成三步，以当前原 action 提交完整 FAIL、cases=[原 a,原 b,实际 c]、remaining=[] 和原 b finding；保存逐例字段、SOURCE 结算与结果回执。任何上游拒绝不能阻断独立 c，也不能把 b 的 FAIL 抹除。
oracle: CLI 和真实 worker 的十二次已完成阶段重做均在副作用之前拒绝；a/b 每个目标标记 commit 总数仍为1，原 workspace/完整 PASS或FAIL/输出/比较不变，无新 prepare 副本。c 首次真实三步完成、标记commit=1；最终集合精确 a PASS,b FAIL,c PASS，overall FAIL且原因和历史可查。resume 沿原 action/attempt/identity，不因 checkpoint 增 attempt，不提前固定未结束 cost。仅最终拒收重复结果、实际已重跑后才拒绝、换 requestId 成功均为失败。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna, boundary-checkpoint
primary AC bindings: AC-026, AC-027
review status: PASS

CASE-052
mode: blackbox
description: checkpoint 的六类绑定变化精确失效，不能复用错误环境证据
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。本例所有 case-set 前置使用 case-set-applicable 的完整定义。六个独立正常 checkpoint 前态均有 a/b 已实际完成且 c remaining。candidate 行通过实际外部 Git 新提交后 REQUEST adopt-external(external 的 identity/lineage/diffDigest 均由当前真实 Git 取得，impact 把相应验证标 affected,reason) 并确认；requirement 行用已确认 USER_INITIATED_CHANGE 改 Alice 输出再 apply-requirement 新全文，impact 标 QA affected；case a 行和 shared 行分别 REQUEST qa-revise(target="blackbox",meaning="stable-execution",reason) 后提交合法新 design，前者只将 a.command.executable 从 python3 命令名改为实际同一 Python 的绝对路径并同步 publicEntry/步骤，后者只把 a/b 共用 setup 改为先实际 go version 并保存版本再 prepare，c 不引用该 setup，并完整重审受影响项。上述每行只用实际新设计/candidate/requirement 生成的绑定请求后续执行，保存旧 checkpoint 的失效/历史和新 expected 集合，不客户端改 binding 输出字段。tool 行换用公开支持、真实已安装的另一工具版本，environment 行在另一干净独立环境执行；这两行提交 CHECKPOINT 的 invalidated=[{caseId:受影响实际ID,previousTool:原版本,previousEnvironment:原环境,tool:新实际版本,environment:新实际环境,reason:非空具体原因}]，cases/remaining 明确剩余待重跑项，随后真正重跑并提交新输出。版本和环境必须来自实测工具 --version/原生环境身份，不能只手改标签。每个重跑均使用 task-operation 的 prepare→target→collect；不对未承诺的局部批准继承作推断。 本例所有真实 QA 调用通过 OP prepare/target/collect；合法 CHECKPOINT 后且尚无终结错误时同一原 action/attempt 继续，tool/environment invalidated 接纳后仅精确失效例可重新三步，未影响例不重放。 本轮对 tool/environment 两行各追加同 action 的实际操作：准备变更时仅改变 a 使用的真实工具/环境（b/c 沿原条件），invalidated=[a]、cases=[原 b]、remaining=[a,c]；未重跑前先向 b.prepare/target/collect 发新 requestId，三项皆拒绝。由实际 worker 对 a 完整三步重跑一次，随后对已完成 a 的三个 stage 逐项发新 requestId，再以相同已消费失效内容重发 CHECKPOINT；保存实际拒绝和原 a checkpointHistory，不把拒绝重发作为一次新合法失效。最后实际执行 c，提交包含新 a/原 b/新 c 的完整结果。a 两次运行的原生工具 --version、环境可观察身份及两次真实副本和目标效果均保存，环境仅声明名称或原版本未变不构成成功前态。候选/需求/case/shared 四行仍走原公开修订路径，不使用 invalidated 代替它们。 tool/environment两行采用boundary-checkpoint的实际目标提交计数和完整前态，其余四行保持原fixture；用两次真实workspace中的精确目标commit区分合法重跑与重复调用。 重放已消费 invalidated 的 VERIFICATION_RESULT 是无效结果路径：保存 REJECTED_INVALID_RESULT 与该 attempt 终结事实、原已完成 checkpoint、额度和 SOURCE 结算；c 尚未执行时按 protocol 在同 TaskKey 取得有限下一 attempt（耗尽时当前 verification-retry Ask 一次授权），仅执行 remaining c。不得要求被无效结果终结的旧 attempt 继续提交成功结果。纯 OP stage 前置拒绝与无效 VERIFICATION_RESULT 分开计数，前者不据此虚构 attempt 终结。原六行已完成真实分支只按原绑定保留；新候选要求重证的受影响接纳分支才在新候选运行。
oracle: 每种变化均核对相应绑定，实际受影响条目不得沿用；case a 变化不无故清除不相关 c，shared 影响所有引用者；tool/environment 不同的证据不被当同环境通过，原因明确。 对 tool/environment 两行，a 在明确失效后恰可完整重跑一次，旧证据入 checkpointHistory；a 完成后的重复三stage及未失效 b 的三stage均副作用前拒绝，相同失效声明不能二次消费。c 仍能完成，未影响证据不丢。保存前后完整投影和实际调用差值，不能以仅接受 invalidated JSON 证明目标重跑有效。 合法 checkpoint 本身不终结 attempt；无效 VERIFICATION_RESULT 必须终结当前 attempt，其保留条目可由同 TaskKey 合法下一 attempt 接续。原 attempt 无效后仍被接纳为有效结果或新 attempt 清空已完成证据均 FAIL。
shared steps: env, protocol, fixture, case-set-applicable, task-operation, native-evidence, native-attached, boundary-native-luna, boundary-checkpoint, fa5-reuse
primary AC bindings: AC-026
review status: PASS

CASE-053
mode: blackbox
description: 最终 QA 结果拒绝缺失、重复、未知 case 并保持有限 retry
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。从 expected={a,b,c} 的三个独立 S5/已 CLAIM 验证前态，分别通过 VERIFICATION_RESULT 提交 cases={a,b}、{a,b,c,c}、{a,b,c,unknown}；每份其余 B/bindingDigest、status、remaining=[]、逐例字段都完整。每行保存实际 rejected receipt、finished/attempt 与有效结果集合，并结算 SOURCE。随后只在同一行的第二次合法 attempt 中提交 exactly expected 的完整结果；若该行因环境已用尽默认两次，则先按当前 verification-retry Ask 一次追加并记录理由，不能偷用第三次。默认两次耗尽的专门行为由 CASE-022/026 验证，不把三种集合错误连续压入一个未经授权的 TaskKey。
oracle: 三类无效集合可定位具体 ID，不形成总体 PASS，错误 attempt 正常终结且第三次不得绕过授权；合法完整集合才能被接纳，checkpoint 不被计成新的逻辑任务。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-027
review status: PASS

CASE-054
mode: blackbox
description: review-ready 前置缺一不可，冻结 expected-set 唯一且不接受漂移
procedure: 分别新建正常 preparation 前态，并停在：开发 worker 尚未完成；已完成开发/黑盒批准而白盒尚无已提交测试结果；一个已选 QA 的 Review 尚未完整批准/FROZEN；全部准备结果已到但候选装配的真实 DRIVE 尚未执行。每行保存当前 pending/phase/candidate/qa状态后实际 DRIVE，读取新签发 actions，不手工删除已经产生的 receipt/candidate 来伪造前态。前三行在必要准备未补齐时必须没有 verification/gate action；第四行允许 DRIVE 先完成真实装配及 freeze，再签发验证，须用调用轨迹证明先后。完整准备首次生成 wave 后再次 DRIVE，保存同一 wave 的 id/targets/expectedDigest；把旧 bindingDigest 或漏项/重复 cases 放入 VERIFICATION_RESULT 申请终结，而不是虚构 freeze/expected-set 可写命令。
oracle: 开发、白盒提交与选定 QA FROZEN 的必要前置缺失时，verification/普通门签发增量为零。候选尚未装配时必须先取得真实完整候选绑定才签发，不把一次 DRIVE 同时完成多个正确顺序步骤误判失败。完整 wave.id/targets/expectedDigest 只建立一次，重复 DRIVE 不重复冻结，错误 binding/case 集合不能改变 expected-set 或制造总体通过。
shared steps: env, protocol, fixture
primary AC bindings: AC-028
review status: PASS

CASE-055
mode: blackbox
description: 一个并行任务失败不停止独立兄弟，source/guard 允许后补满容量
procedure: START 前将项目 capacity=2 并 CONFIG；custom 选择 blackbox+G0+G1。真实 S5 同 wave 先启动两项 HOST，用 native-evidence 同时读到两个已 attach 的原生 identity 与起止区间。第一项 QA 用真正 buggy command 输出 BAD/exit7 形成 FAIL；第二项实际门仍在运行（必要时以本例 OS SIGSTOP 暂停其原生进程，保存 alive/stopped 事实，只为保持在途）。第一项结果先到时 DRIVE 不补位；实际 SOURCE STOPPED/成本结算后 DRIVE 应签发第三目标并 HOST 真启动。独立副本把第一项运行所需普通工具移出本例临时配置/冻结缺失工具，实际 RUNTIME_ERROR 后依同顺序观察；不伪造 FAIL 或 native身份。收齐前仅 DRIVE 观察没有 repair，全部结束后 SIGCONT 并正常收口；记录兄弟前后身份、仍可接纳结果和第三目标的独立原生 create。
oracle: 独立兄弟保持运行/可接纳结果；可用容量与 settled source/guard 满足后第三目标自动补位，未 attach 握手只限制对应启动，已 ACK worker 不被串行化。wave 未齐不提前 repair，不以单 QA 内用例隔离替代本证据。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, native-attached, interrupt-evidence
primary AC bindings: AC-028, AC-069
review status: PASS

CASE-056
mode: blackbox
description: 有白盒和无白盒路线都先物化交付物再冻结候选
procedure: 两个独立 run 分别选择 full 与 custom qa=["blackbox"],gates=[]；按各自公开 preparation 链完成开发与 QA，保存开发 Dn、批准黑盒 design、白盒测试 manifest（适用时）以及所有其他 VCS 交付路径。实际 DRIVE 完成装配并生成 Q；运行 git ls-tree -r Q 和 git show Q:.gates/results/<R>.blackbox-cases.json，full 另核对白盒 case 文件与所有 testOwned 路径；逐一按交付 manifest 与 Q 的真实 blob/mode 对账。对 VCS 外证据读取 summary/externalArtifacts 中路径和 digest 并与实物比较。验证前候选缺漏的正常准备屏障由 CASE-054 验；底层遗漏 artifact 的冻结校验由结构测试注入，不能捏造公开的“装配请求 paths”或 GIT_RECEIPT 来把内部 API 冒充黑盒入口。
oracle: 两路线的全部 VCS 内交付路径在冻结 Q 内逐项存在，内容与已批准设计/测试 manifest 相同；无白盒也经过装配。外部产物均有可核对的路径与摘要绑定，没有未绑定交付内容留待 Seal 补入。任何交付路径实际缺失或字节不符均为失败；Q freeze 的证据必须早于 QA execution/普通门的实际调用。
shared steps: env, protocol, fixture
primary AC bindings: AC-064
review status: PASS

CASE-057
mode: blackbox
description: 最终验证后追加代码、测试或交付物不能直接沿用旧证据 Seal
procedure: 四个独立 S6 保存实际 Q 与所有最终验证绑定。三个变化行在该run的用户Git目标工作区P先以git restore --source Q --staged --worktree -- .带入完整已验证内容，然后分别实际修改 contest.go 的 Alice 输出、greet_test.go 的一个有效断言、已装配黑盒 case 交付文件，普通 git add/commit 得 F。保存真实 B/Q/F 与完整 diff，DRIVE 观察外部漂移；通过当前 REQUEST adopt-external 和精确 external/impact/reason 的 Ask 确认后继续 DRIVE。变更 AC 或 oracle 含义时必须先走 requirement-change/qa-revise 合法修订，不将外部文件编辑当已经获得新用例审批。第四个未变化 S6 直接 DRIVE 至真正 promotion/Seal，比较前后 HEAD/tree/验证引用。没有直接 workflow seal 或 REQUEST seal 输入。
oracle: 变化 F 必须重判证据/验证义务，不以内容相似直接沿用 Q 全量 PASS，不在 Seal 补文件或再次 squash；未变 S6 的 Seal 保持同一候选 identity。精确 promotion 例外由独立用例验证。
shared steps: env, protocol, fixture
primary AC bindings: AC-029, AC-064
review status: PASS

CASE-058
mode: blackbox
description: wave 收齐并裁决后唯一 repair 汇总 QA FAIL、P0/P1 与门 P2
procedure: 准备 expected QA 与多门；逐步只交 QA FAIL、再交门含 P1/P2/P3、留一个 RUNTIME_ERROR，依次 DRIVE 检查是否签发 repair；完成全部错误处置和逐 finding 的 NEW/SAME_AS/DISMISSED 核实后 DRIVE，再重复 DRIVE。保存 raw/有效 problem/repair 成员和 basis。 QA 的 verificationResult 必须 cases 完整且至少一个真实/明确模拟前置的 FAIL 与同 caseId finding；门结果 status/finding 按当前 schema 完整，目标 ID 来自同一 wave。RUNTIME_ERROR target 按有限 verification-retry 完成合法有效结果并结算 SOURCE。每个 target 的 WAVE_ADJUDICATE 复制当前 waveId/target/resultDigest，逐 finding decisions 与 affectedCases 完整；在该目标 SOURCE 未结算时不先提交全 wave 完成主张。
oracle: 未齐/错误/未裁决时 repair=0；完整后恰好一个 repair，其成员等于有效 QA FAIL∪P0/P1∪门 P2，P3 仅 advisory；raw result/receipt 本身不增加 obligation/TaskKey，重复请求不造第二 repair。
shared steps: env, protocol, fixture
primary AC bindings: AC-030
review status: PASS

CASE-059
mode: blackbox
description: 归并纠正在派发前更新同一计划，派发后进入下一候选 wave
procedure: 在两份独立 P/R 分别执行 A/B，按 fixture 准备已冻结候选的完整 wave，至少含两个不同 target T1/T2，原始结果各自仅有稳定 finding f1/f2；选择同一正常缺陷的两份报告作为初始归并输入，其他目标若存在则均为完整有效结果。所有目标终结且 SOURCE 结算后，先 READ(show) 取 waveId、T1、T1 的 resultDigest 与当前 revision，经 SUBMIT operation=WAVE_ADJUDICATE、waveAdjudication={waveId,target:T1,resultDigest:实际值,decisions:[{findingId:f1,decision:NEW,problemId:p1,severity:P1,reason:本例核实理由,evidence:本例证据,affectedCases:实际冻结相关caseId}]} 实际接纳。READ(show) 核对 p1 已存在并保存回执 revision；只有此时再以当前 revision 对 T2 提交其完整 decisions=[{findingId:f2,decision:SAME_AS,problemId:p1,severity:P1,reason:初始同根因归并理由,evidence:实际证据,affectedCases:实际冻结相关caseId}]。T1/T2 各用自己的真实 resultDigest，其他目标逐一完整裁决；不得把尚在同一请求里待创建的 p1 当作已有 problem。保存 T2 裁决回执 revision J。B 以 DRIVE/READ 取得唯一 repair plan ID 和签发 payload；A 停在该计划已归并但尚未签发 repair action 的隔离前态。若公开逐步操作已自动 issue，可按 fixture 仅构造正常可达的未派发 plan 前态，保留原 raw、完整逐目标裁决及 plan 成员 p1；不得预置纠正 p2、被测更新结果或回执。READ 必须确认 plan.issued=false 且不存在该 plan 的已签发 repair action；有 issued=true 的原 A 只能算派发后，不能当 A 正例。随后根据独立核实材料纠正 f2 为独立问题。A 在上述已确认未 issue 的前态（不是 claimed=false），重交相同 waveId/T2/resultDigest 的完整 decisions，仅将 f2 改为 NEW、problemId=p2（本 run 尚不存在的稳定 ID），reason/evidence 指明误归并与独立问题，replacesRevision=J，外层 expectedRevision 为刚读取值；DRIVE/READ 核对计划成员。B 从其独立同等前态先实际 HOST 启动当前 repair，再用本行实际 J 及当前 revision 作同一合法纠正；保存已签发原 payload，按其原计划完成 REPAIR_RESULT、SOURCE 和真实 Git DRIVE 得 Q2，READ 后续 wave/义务。另以独立正常可达 fixture 分别准备确已 RESOLVED、确已 DISMISSED 的稳定 problem，保留其真实可读前史；在各自新完整 wave 的新 raw finding 上，以 SAME_AS 引用已存在的对应 problem 并逐目标裁决，再 DRIVE/READ。这些状态只作隔离前置，待测 SAME_AS、纠正、repair 计划变化和新 wave 均由产品实跑；不用 CORRECT 代替 WAVE_ADJUDICATE。全部外层事件和证据按 protocol 保存。
oracle: T1 裁决成功后 p1 已实际存在，T2 的 SAME_AS 合法接纳并归同一 p1。A 的 repair ID 不变、成员由 [p1] 更新为含 p1/p2 的精确集合，不创建同快照第二计划；B 已派发 repair payload 字节不变，p2 为 OPEN 义务并进入修复所得 Q2 的下一 wave。原 f1/f2/p1、纠正新 p2、T1/T2 原裁决及替代 revision 历史可追溯，不重新生成已有身份。RESOLVED、DISMISSED 两行引用均接纳，原问题状态不重开且不因该重复报告新增 repair 成员。任何 SAME_AS 因引用尚不存在 problem 被拒绝均说明前置不成立，不能冒充本例通过。 派发界线取 repair action 已签发/plan.issued，非 HOST 是否已启动。A 的前态若无法通过实际 READ 确认则保持缺证据；已 issued 未 claimed 的纠正按 B 判据，不据其成员未更新判产品错误。
shared steps: env, protocol, fixture, fa5-reuse
primary AC bindings: AC-031
review status: PASS

CASE-060
mode: blackbox
description: 事实核实走 Operator，多种用户分支才 Ask，范围外 P3 不阻塞
procedure: 三个独立 normal wave 分别有事实待核 finding、有两种用户实质选择的 finding、范围外建议。以当前 target 的 VERIFICATION_RESULT 完整提交后 SOURCE 结算；第一行读取待逐 finding WAVE_ADJUDICATE 的 Operator 边界，核实后按 NEW/DISMISSED 实际裁决。第二行把问题明确为需求的两种互斥输出取舍，在 Operator 确认其事实和范围后按当前公开 typed 需求变更/选择 Ask 处理，不发明 VERIFICATION_RESULT 用户 choice 字段或把一般两个代码修法自动认作必须 Ask。第三行 category="out-of-scope",severity="P3"，reason/evidence 明确属于恶意改内部状态的范围外建议，完整裁决后 DRIVE。保存边界种类、有效 problems/advisory 与后继 repair/验证集合。
oracle: 事实核实边界为 Operator；只有存在多个合法且属于用户的处理选择才 Ask；范围外项为 P3 advisory，不阻 PASS、不新增 repair。
shared steps: env, protocol, fixture
primary AC bindings: AC-032
review status: PASS

CASE-061
mode: blackbox
description: 普通返修保持 FROZEN 并按固定 AFFECTED 执行、继承和未执行集合对账
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。在 FROZEN 的 a/b/c 首次 wave 中，a 实跑 FAIL、b/c 实跑 PASS，完整结算 wave；WAVE_ADJUDICATE 对 a 的真实 finding 给 NEW，并在其 decision.affectedCases 精确填 [a]。实际 repair 执行对应代码修改、build/projectTests 和受影响 a，自跑结果完整放入 REPAIR_RESULT；结算 SOURCE 后 DRIVE，由程序实际提交 Q2 并计算新的 scope。读取新 QA target.expected/bindingDigest 与 inheritance，不手工提交任意 per-case ImpactSet 或 carry receipt。按程序签发的 AFFECTED 目标实际只运行 a，核对 b/c 的合法继承。遗漏当前 expected 项、重复项或把未执行项伪填 PASS 的负例各在独立的同等第二轮前态通过 VERIFICATION_RESULT 提交。
oracle: case-set 仍 FROZEN，scope=AFFECTED；执行={a}、继承={b,c}、未执行集合明确且并集恰为 expected，无重复。错误集合拒绝；不能普通 repair 后强制全量重设计。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-033
review status: PASS

CASE-062
mode: blackbox
description: 只有定义允许的稳定变化可 THAW，临时环境错误不可解冻
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。在候选已形成且 QA FROZEN 的独立副本分别 REQUEST qa-revise(target="blackbox",reason)：A 理由只是普通生产返修且不填 meaning；B 理由只是一次性环境故障且不填 meaning；C 需求含义变化，先完整 requirement-change/apply-requirement 确认后 meaning="acceptance"；D 公开行为变化 meaning="public-behavior"；E 错误 oracle 修订 meaning="acceptance"；F 已知稳定执行路径变化 meaning="stable-execution"。C–F 在请求理由中给出真实变化与对应输入文件，按当前 Ask 明确确认，再提交仅受影响设计和逐项 Review。B 的临时执行错误仍通过当前任务的 RUNTIME_ERROR/有限 retry 处理。全过程只使用 qa-revise，不能提交不存在的 REQUEST qa-thaw 或旧 workflow qa-thaw 命令。
oracle: 前两种拒绝 THAW，环境故障记 RUNTIME_ERROR；后四种仅在合法流程确认后解冻实际受影响范围；未变 case 继承有依据，不用解冻绕过失败。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-033
review status: PASS

CASE-063
mode: blackbox
description: QA_ARTIFACT_REPAIR 绑定新候选且 fresh review/执行变更部分
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。在已批准 Q 中定位 a 的实际错误白盒断言。通过 REQUEST qa-revise(target="whitebox",meaning="acceptance",reason="修正 a 的具体错误断言与 oracle") 及 Ask 确认进入合法 QA 修订；白盒在绑定工作区实际修正 a 的 testRef/测试代码与必要 case，PREPARATION_RESULT 返回精确 test-owned manifest，由 DRIVE 真正提交并形成新候选 Q2。读取新的白盒 ReviewScope 与后续 execution scope，独立 HOST 审查并实际执行改动项。用 Q 的旧 bindingDigest/原 verificationResult 通过当前 SUBMIT 尝试作为 Q2 结果接纳；旧事件 ID 原样 replay 的情况须用随后的权威 show 判断，不能把历史回执误作新结果。QA_ARTIFACT_REPAIR 是修复性质，不是公开 action 名。
oracle: Q2 与新 artifacts digest 持久化；至少 a fresh review/执行，其他验证按真实影响重算。Review 首次 FULL 后本次合法增量为 AFFECTED，两类 scope 分字段绑定，旧 Q 的结果不能直接认证修改内容。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-033, AC-065
review status: PASS

CASE-064
mode: blackbox
description: 纯门 P2 carry 必须有真实构建、全量测试和受影响用例自跑
procedure: 从没有 QA FAIL/P0/P1、仅一个普通门 P2 的完整已裁决 wave 准备四个独立 repair 前态。前三个实际修复相同代码并运行应有的构建/测试，但在 REPAIR_RESULT 中分别省略 build、projectTests 或 plan.affectedCases 中一个 case；每个负例只占本 run 的一个 attempt，观察不能产生当前 P2_CARRY。正例实际运行 go build、go test ./... 及每个 affected 用例，REPAIR_RESULT 包含完整 B、planId、status=PASS、manifest、resolved、build/projectTests BatchValidation 及逐 case execution，结算 SOURCE 后 DRIVE 真正提交 Q2并读取自动 completion/P2_CARRY receipt。不调用旧 complete-p2-repair/carry，也不直接提交计算出的 carry 对象。
oracle: 三种缺项均不能 carry；完整后保留原门 PASS 的 B→Q 比较事实并以 receipt 继承到 Q2，不伪装新审查、不增加 wave。无其他 FULL 义务的路线不自动新派 FULL。
shared steps: env, protocol, fixture
primary AC bindings: AC-034
review status: PASS

CASE-065
mode: blackbox
description: 保证路线最终 FULL 或明确保留才可 Seal，随意 final/继续不能改规则
procedure: 有 guarantee 的普通 repair Q2 完成其合法 AFFECTED 后实际 DRIVE 申请继续，保存缺最终 FULL 的当前义务与 next。在独立副本通过 REQUEST continue（此时并非 TOKEN_CAP）以及在普通 REQUEST 中增加 final=true/hostOverride=true 等不在当前 schema 的参数尝试省略义务；另把“继续”普通文本作为 submit 输入，保存拒绝及义务不变。合法用户保留对照通过一次真实 requirement-change/apply-requirement，document/meaning/origin/impact 完整，keep 只列明确存在原 PASS 的 QA/门 target 名，再确认当前 preview/Ask，读取按本次需求/候选绑定的 USER_KEEP，继续完成未豁免义务并 DRIVE 至终态。无 QA 或无其他 FULL 义务的纯 P2 路线，重放上述未知 final 参数不能凭空派新 FULL。不存在可由用户填写的通用 Seal skip/host override 接口。
oracle: guarantee 缺最终 FULL 或合法保留不能 Seal；任意标记不能增加/取消 FULL 或新增跳过授权。合法例外只绑定本次范围，不改默认规则、不能重放扩大。
shared steps: env, protocol, fixture
primary AC bindings: AC-034, AC-035
review status: PASS

CASE-066
mode: blackbox
description: 产品/技术/门复审始终完整输入，QA 首次 FULL 后增量范围独立记录
procedure: 本例所有 case-set 前置使用 case-set-applicable 的完整定义。实际派发首次黑白盒 Review 并保存 ReviewScopeMode=FULL；合法变更 a 后派发增量 AFFECTED。另做普通 repair Q→Q2 后准备产品/技术适用复审和普通门，读取规范提示词中的完整需求/基线差异。 产品/技术的适用新鲜复审由当前 REQUEST review-again(reviewAction,reason) 明确请求，或由已确认需求变化触发，不假定普通代码 repair 自动要求产品/技术复审。产品/技术仍读取完整 regular.requirement.text；普通门的规范输入保存 original base 与 Q2 的完整 diff，不能使用 Q→Q2 的局部修补差异。QA 的增量则通过 qa-revise 与 design 变更生成新的 reviewScope；所有字段从当前公开生成物读取。
oracle: QA scope 随 candidate/result 持久化，不能拿 execution AFFECTED 替代 review 字段；产品/技术读取完整最新文档，普通门始终 B→Q2 全 diff，未缩成 Q→Q2 修复 diff；carry/用户保留不伪装新审查。
shared steps: env, protocol, fixture, case-set-applicable
primary AC bindings: AC-065
review status: PASS

CASE-067
mode: blackbox
description: 公开可达非终态需求修订立即建立屏障并隔离迟到结果
procedure: 七个独立正常前态：Ready=已签发待HOST，Ask=route请求未确认，Wait=真实结果先到而SOURCE仍RUNNING，Operator=完整结果待ADJUDICATE；开发/QA/repair在途分别从真实HOST取得当前identity后保持实际在途。各先 REQUEST {action:"requirement-change",origin:"USER_INITIATED_CHANGE",reason:"隔离需求修订"}，以该请求接纳为Phase A屏障时点；立即用旧已签发 HOST、完整旧 OP 请求、旧 PREPARATION/VERIFICATION/REPAIR_RESULT 和 DRIVE 探测迟到结果/副作用。新需求未确认前不填 unaffected。保存每个旧action的quarantine/suspend/terminate、旧wave状态、下一动作与原生调用差分。已发未完成Seal intent的内部原子窗口没有可稳定公开暂停入口，交白盒覆盖其撤销和零旧写；不借七行声称该窗口已实测。
oracle: 七种公开非终态都接纳Phase A且立即阻止旧业务签发/promotion/权威结果推进；可隔离旧结果只进quarantine，其余安全暂停/终止。未确认的新正文不进入有效保证；旧结果不覆盖新决定。完整REQ-016/AC-036的Seal intent窗口另有白盒证据，黑盒不伪造命中。
shared steps: env, protocol, fixture, task-operation, native-evidence, interrupt-evidence
primary AC bindings: AC-036
review status: PASS

CASE-068
mode: blackbox
description: meaning-preserved 修订确认原子生效，无回退 Ask 且保留产品 PASS
procedure: 准备产品审已形成有效 PASS、且有一条已经 Operator VALID 与用户 confirm 的 P2 措辞 finding 的正常前态；其 source 是实际保存的 product-review currentDigest 与 finding.id，不用无 finding 的 PASS 虚构 review-delta 来源。保留 confirmed requirements.md 全文，将唯一“问候程序”改为“问候命令”，语义/AC 不变。REQUEST requirement-change(origin="REVIEW_FINDING",reason)，然后 REQUEST apply-requirement 提交 document（实际 path/新全文/全文 SHA-256）、meaning="preserved"、同 origin、impact={"product-review":"unaffected","start-readiness":"unaffected"}、keep=[]、delta=[{old:"问候程序",new:"问候命令",source:"product-review:<currentDigest>:<findingId>",reason:"已确认措辞修订"}]。保存完整 preview/Ask 后才提交当前 DECIDE；一份独立副本省略 DECIDE 并 DRIVE 证明正文未确认不能应用。每次原子性比较基点取该 REQUEST 之前或当前 Ask 之后的清楚时点。
oracle: 正文变化仍需 delta 确认；原子登记新 revision/保留/影响，产品 PASS 保持且无回退 Ask。缺新确认时不能应用；不存在新 revision 已写但保留或影响未写的半状态。
shared steps: env, protocol, fixture
primary AC bindings: AC-037
review status: PASS

CASE-069
mode: blackbox
description: meaning-changed 先展示精确影响与额度，过期授权零业务变化
procedure: 从已有产品/技术 PASS 的独立前态通过 REQUEST requirement-change(origin="USER_INITIATED_CHANGE",reason) 建屏障；实际将 requirements.md 中 Hello Alice 改为 Hi Alice。REQUEST apply-requirement 提交当前 path、新全文和正确 SHA-256 revision，meaning="changed"、origin、impact={"product-review":"affected","start-readiness":"unknown"} 及 keep=[]。保存新 Ask 的 preview，逐字段核对 state/base/candidate revision、将失效 PASS、回退后果、剩余产品审额度。各错误行从该正常 Ask 前态独立开始：DECIDE 缺 digest、外层 expectedRevision 为旧值、token 错误、requestId 绑定另一请求；每次以提交前 show 为零业务变化比较基点。另一行把 apply-requirement.document.revision 填原全文摘要以验证新文档错绑；不在 DECIDE 凭空增加 candidate 或后果确认字段。最后使用当前真实 Ask 完整 DECIDE；unaffected carry 的正例在另一副本将适用 target impact 明确写 unaffected。
oracle: 非法授权零业务变更，legacy 入口不能绕过；合法一次决定原子应用新 revision。未保留 affected/unknown 失效、unaffected 可有理由 carry；series/wave 不清零，不能自动授权额外产品审。
shared steps: env, protocol, fixture
primary AC bindings: AC-037, AC-038
review status: PASS

CASE-070
mode: blackbox
description: finding 修订须完整匹配 base、穷尽 hunk 核实且只确认变更包
procedure: 准备产品审/技术审已合法登记并确认的两个 finding，保留上次 confirmed requirements.md 的完整 UTF-8 原文及 revision，从公开 rawCandidates/currentDigest 取 source。修订两个唯一文本 hunk，构造 document={path,text,revision}，delta 各项为 {old,new,source:"<reviewAction>:<resultDigest>:<findingId>",reason}，origin="REVIEW_FINDING"。在独立副本依次尝试：省略 delta；old 文本不匹配当前完整 base；实际改两个 hunk 却只列一个；额外改无关句且不给合法 source/reason。均通过 REQUEST apply-requirement 观察 preview/拒绝，不能调用已删除 review-delta-preview。合法例去掉无关修改或通过需求变化取得其适用来源后重新提交完整 delta，保存穷尽变更包和新全文；决定前另 READ(show) 查看完整文档，再以当前 ask.id/token/digest/revision 确认变更包。完整 base 的存储缺失属于内部校验，黑盒不删除内部 base 字段。
oracle: 四个不完整来源/变更包均不能登记新需求；合法确认绑定完整新 revision，未变内容继承。用户看全文不将确认范围扩大到未修改内容；meaning-preserved 也不能跳过正文确认。
shared steps: env, protocol, fixture
primary AC bindings: AC-066
review status: PASS

CASE-071
mode: blackbox
description: 全选/部分/无保留及 affected/unknown 保留能按义务推进至 Seal
procedure: 用含产品/技术/QA review/execution/门 PASS 的同前态独立运行五条修订链：全选、仅产品+QA执行、无保留、明确保留 affected/unknown、连续两次修订各自重新选择；每条通过真实公开 REQUEST/确认处理并完成未保留/新增 AC 的剩余验证至 Seal。 被测 request.keep 使用目标名：产品审 product-review、技术审 start-readiness、QA review blackbox-review/whitebox-review、QA execution blackbox/whitebox、普通门使用当前 catalog 中已 PASS 的 gate ID；不是 PASS result ID。各链从 REQUEST requirement-change → 实际编辑唯一文件 → REQUEST apply-requirement(document/meaning/origin/impact/keep/reason) → 对完整 preview/Ask DECIDE 起步。新增 AC 正式进入新 document/projection 后再登记其 QA；每个 receipt 的原/目标候选等字段读取程序输出并与前后事实逐项对比，不伪填 receipt 推进。
oracle: 每个选择只作用对应原 PASS；保留优先于本次失效/fresh review/最终 FULL，新增无原 PASS 义务仍须验证。receipt 完整列原 PASS/需求/候选、新需求/目标候选、state revision/清单，每条能够在义务满足后实际 Seal。
shared steps: env, protocol, fixture
primary AC bindings: AC-039
review status: PASS

CASE-072
mode: blackbox
description: 保留请求的未知/非 PASS 目标与过期确认不得部分生效，合法 receipt 完整绑定
procedure: 以含既有产品/技术/QA/门 PASS 的正常前态保存原 target→PASS digest/requirement/candidate 表。每个负例独立运行 requirement-change/apply-requirement：keep 含未知 target、把实际 result digest 当 target 名、包含明确尚未 PASS 的 target；另对合法产生的保留 Ask 分别提交缺 requestId、缺 token、缺 digest、旧 expectedRevision、其他 run 的 Ask 三元组。每次只改对应公开输入，留提交前后新需求确认、inheritance、有效结果和额度。正例提交准确 target 名清单与当前完整 DECIDE，读取程序生成的保留 receipt，将其原 PASS/需求/候选、新需求/目标候选、state revision/清单逐字段与输入及真实输出核对并原样 replay。receipt 本身是程序输出，不能假定客户端可删除它的内部字段后作为一个公开决定提交；内部 receipt 缺字段的拒绝由结构测试覆盖。
oracle: 未知、不是 target 名、非 PASS target 或绑定错误的决定都拒绝，既有 PASS 和业务授权不部分新增；Phase A/合法 Ask 已建立的屏障不被误算成无效决定的写入。合法原子应用后保留清单精确等于 keep，receipt 六类绑定均有值且与原/新真实事实一致；重放不复制授权或 receipt，不出现只换需求 revision 而漏写保留/影响的半状态。
shared steps: env, protocol, fixture
primary AC bindings: AC-039
review status: PASS

CASE-073
mode: blackbox
description: 保留显示真实执行与授权继承分层，不能保留未 PASS 或代替副作用
procedure: 修订时尝试选择 FAIL、PENDING 和在途任务；合法只保留既有 PASS 后 READ show/coverage 并 Seal，核对原输出引用和继承字段。另在提交或 promotion 尚未完成时请求以保留跳过；终态后请求新修订。 FAIL/PENDING/在途负例中的 target 必须从未形成可供本次选择的原 PASS；已有较早 PASS、当前恰在复审的 target 不能简单按“在途”判非法。keep 使用 target 名而非结果 ID。合法继承后仍通过 DRIVE 执行实际未完成 Git/promotion/cleanup；以提交前后真实 HEAD、operation receipt 和结果引用证明保留没有替代这些副作用。终态的 requirement-change 请求只能拒绝当前 run 续改并指向新 run，不修改已持久化终态。
oracle: 未 PASS 项不可选，迟到结果不扩大清单；显示/Seal 区分原版本 executed 和本次 user-retained，原证据可追溯，不生成新版实际执行。副作用仍需完成；终态变更要求新 run。
shared steps: env, protocol, fixture
primary AC bindings: AC-040, AC-041
review status: PASS

CASE-074
mode: blackbox
description: 一次保留授权不适用于后续独立候选变化
procedure: 完成一次合法需求修订并显式 keep 后，保存其原 REQUEST/DECIDE 完整 event 文件与 receipt；再通过正常 development repair 或 adopt-external 形成新候选，或通过第二次 requirement-change/apply-requirement 形成独立新需求，第二次 keep=[]，保存新的义务投影。在相同 run 原样重放第一次 DECIDE event，随后重新 READ(show)；再以新 event ID 但第一次 Ask 的 requestId/token/digest 以及当前 expectedRevision 尝试确认。最后按第二次变化的真实影响完成所需工作。不能把输出 receipt 当作一种未声明的可写事件。
oracle: 旧授权绑定仅覆盖第一次变更，不能自动继承到后来 candidate/revision；新变化重判并保留两次历史，不篡改原 PASS。
shared steps: env, protocol, fixture
primary AC bindings: AC-041
review status: PASS

CASE-075
mode: blackbox
description: 主动控制经 availableActions 与 typed Ask，错误新鲜度和自由 USER 拒绝
procedure: 在有可用 jump 的正常 run READ(show/next)，复制 allowed target 并 REQUEST {action:"jump",target:<product-review 或 start-readiness>,reason} 生成当前 Ask。四个独立副本分别 DECIDE 错 requestId、错 token、错 digest、旧 expectedRevision；另以独立 REQUEST jump target="unknown-node" 验证非法目标。自由写状态负例通过 SUBMIT 发送 {"ID":"free-user-negative","Kind":"USER","user":{"phase":"complete"}}；此负例只验证闭集事件面拒绝自由状态写入，不借内部 API。合法对照使用当前 Ask 全部字段完成跳转，并读取受影响 attempts/results/candidate/intents 的实际隔离、失效与对账。
oracle: 五类错误拒绝，业务投影/候选/额度不变；合法跳转先隔离/失效/对账受影响 attempts/results/candidate/intents，再进入目标节点，无自由 USER writer。
shared steps: env, protocol, fixture
primary AC bindings: AC-042
review status: PASS

CASE-076
mode: blackbox
description: reset 明示保留/清除范围并以可恢复 cleanup intent 完成
procedure: 有登记临时资源与已结算历史的活动 run REQUEST reset(reason)，保存 Ask 明列的保留与清除对象并 DECIDE confirm。对照保留 requirement/base/raw results/series/wave/cost/history，清除当前 developmentPlan/topology/route/guarantee。若公开操作合同给出持久 cleanup 的可观察中断点，执行时在该实际副作用完成/receipt 未回写窗口终止隔离进程，重启 DRIVE 先对账再完成余项，保留具体触发证据。完成后 READ(show/next) 检查 run 重新进入审查准备的活动状态；不要求 reset 产生 terminal summary 或把 reset 当 abort。中断窗口没有公开可观察入口时由对应结构恢复测试承接，不以随意 sleep+kill 冒充已命中指定窗口。
oracle: 实际保留/清除集合与授权 preview 精确一致，旧需求/基线/原始证据/完成计数/成本历史不丢；旧计划/拓扑/路线/保证不再生效。reset 完成后可继续当前 run 的新审查，不是不可续的终态。存在 cleanup UNKNOWN 时必须先对账且不重复删除已完成资源；未完成清理不能伪装完成。
shared steps: env, protocol, fixture
primary AC bindings: AC-042, AC-044
review status: PASS

CASE-077
mode: blackbox
description: 外部 Git 漂移先冻结预览，精确采纳保留原 base
procedure: 真实 S5 的 P 中经普通 Git 修改 README 并提交 F；DRIVE 触发漂移，读取 observed identity、lineage、完整 diff preview。先不授权尝试推进，再授权精确 F；另授权前再提交 F2 后重交 F 决定；另把 requirements.md 正文改动提交。 普通外部提交发生在workflow --root指定的用户Git工作区P；先git restore --source Q --staged --worktree -- .带入完整候选，再修改和提交，实际目标/ref由公开Git回执对照；REQUEST adopt-external 的 external 按 adopt-current-candidate 绑定 F/原 B/真实 C→F diffDigest（C 是 regular.candidate，Q 是装配候选），impact 对每个当前目标明确 affected/unaffected/unknown，reason 非空。F2 行在已取得针对 F 的 Ask 后实际提交另一改动，再用该 Ask 的 id/token/digest 确认，以证明重新观测能发现漂移；每行独立前态。需求正文行先把实际文件变化交 requirement-change/apply-requirement 确认，不能使用 adopt 直接确认新需求。
oracle: 漂移先冻结；只接受与实际观察一致的 identity/lineage/diff，错绑/过期 F 不采纳；成功后原 B 不变，affected/unknown 与 intents stale、未完成 wave 不计轮；需求正文改变转需求流程。
shared steps: env, protocol, fixture, adopt-current-candidate
primary AC bindings: AC-043
review status: PASS

CASE-078
mode: blackbox
description: 删除公开 workflow cleanup 后，typed abort 仍自动清理登记资源并保留终态回执
procedure: 在活动 R 创建由产品登记的工作区/临时资源，并在不属于该 run 的 P/sibling.txt 写入固定字节 sibling
。先调用 [FG,"workflow","cleanup","--root",P,"--run-id",R]，按 retired-workflow-surface 控制对照核对未知命令拒绝并保存活动 R 状态与登记资源。通过 READ(show/next) 取得 abort 可用动作，按 protocol REQUEST action=abort、读取 Ask、提交绑定当前 requestId/token/digest/revision 的 DECIDE；反复 DRIVE 直至实际终态自动清理完成。用 READ(show)、READ(diagnose) 和 DRIVE 读取/重放终态；再调用同一已删除 cleanup 命令。记录前后资源清单、sibling.txt 字节及 terminal summary/receipt。
oracle: 两次 workflow cleanup 均走与控制命令相同的未知命令处理，不能进入旧 cleanup handler；活动状态、终态 summary 与已存在文件不因该拒绝发生业务修改。合法 typed abort 能完成自动清理，产品登记的待清资源归零，sibling.txt 始终为 sibling 加换行。清理未完保持 FINALIZING_CLEANUP；完成后没有未处置 UNKNOWN，summary/receipt 可只读重放且不重复破坏性副作用。
shared steps: env, protocol, fixture, retired-workflow-surface
primary AC bindings: AC-001, AC-044, AC-057
review status: PASS

CASE-079
mode: blackbox
description: 开发前可重选/追加，开发后只能加普通门并重绑 wave
procedure: 初始路线明确采用 custom、qa=["blackbox"]、gates=[G0]，其中 G0 为当前 catalogGates 第一项，G1 为第二个未选普通门；先核对实际 catalog 至少两项并保存真实 ID。开发 worker 尚未开始的前态，分别 REQUEST route-reselect 和 route-add，route 为期望的完整合法组合，reason 非空，并确认当前 Ask。进入完成开发/冻结验证的 S5 后，只追加 G1，保存新义务与 wave。四个负例独立使用同等初始路线：开发后新增 whitebox；移除原 G0 或 blackbox；lightweight run 进行 route-add；复制旧 Ask.id/token/digest 在新路线 revision 下确认。每个操作由 REQUEST/DECIDE 和 DRIVE 实跑，不能在初始 full 已选全部门的 run 里假定存在一个未选门。
oracle: 前期合法重选/追加生效，开发后只新增普通门；四类禁止请求拒绝。合法新增使受影响旧 wave 失效、新 target set 绑定，不在旧 expected-set 原地删改凑齐。
shared steps: env, protocol, fixture
primary AC bindings: AC-067
review status: PASS

CASE-080
mode: blackbox
description: case-set 与目标集变化均使旧 wave 失效，不能原地缩减 expected-set
procedure: 两个独立 S5：A REQUEST qa-revise(target="blackbox",meaning="acceptance",reason 指明实际 oracle 改变)，按 Ask 确认、提交新 design 与逐项 Review，再 DRIVE 形成新的 case-set/候选；B REQUEST route-add(route 为当前组合加一个未选普通门,reason) 并确认。各保存旧 wave 的部分已接纳结果与不可修改 targets/expectedDigest。分别通过 VERIFICATION_RESULT 提交旧 claim/bindingDigest，或只列已完成 cases 且 remaining=[] 来尝试制造终结；每份负例有自己的合法 attempt 前态，不伪造可写 expected-set 字段。 B 的初始路线同 CASE-079，为 custom blackbox+G0，确实保留 G1 未选；A 使用其明确已 FROZEN 的模式。
oracle: 两个旧 wave 都标不适用而非已收齐；旧结果按影响保留原事实，未完成 wave 不计轮；缩减旧集合请求拒绝，新集合完整建立后才能形成下一 wave。
shared steps: env, protocol, fixture
primary AC bindings: AC-067
review status: PASS

CASE-081
mode: blackbox
description: guard 权威 basis 只由确认计划及成立义务计算，预览不授权
procedure: 在有 S4 已确认计划和 route 的 run 保存 show/status/next 的 guard basis、expected TaskKey 集合、request/retry/total allowance 与 cost。普通 raw finding 对照通过当前 REVIEW_RESULT/VERIFICATION_RESULT 的完整原始结果携带 finding，暂不完成对应 Operator/WAVE_ADJUDICATE；普通 receipt 使用该事件的真实 receipt/replay；resume 使用一个有原生身份且已停止的当前 HOST --resume。每个步骤实际调用后 READ，对照允许集合和计数，不假定有自由 raw-finding/receipt 写命令。另一正常独立前态完整收齐/结算 wave 并逐 finding WAVE_ADJUDICATE，观察新义务；再对一个真正耗尽的当前 TaskKey 用正确 authorize-retry/preparation-retry/verification-retry Ask 一次追加。将 PKG/references/guard-preview-basis.json 普通复制到 E/basis.json，调用 [FG,"workflow","guard-preview","--input",E/basis.json]，保存输出并再次 READ 当前 run。
oracle: raw finding、普通 receipt/replay/resume 不扩大 expected TaskKey 或 request/retry allowance；完成合法裁决的义务和一次 retry 仅增加其允许项，retry 不新增逻辑 TaskKey。三只读权威投影在同一状态上逐字段一致。公开 preview 示例的 distinctExternalTaskKeys=3、requestAllowance=3、retryAllowance=4、totalAllowance=7（若交付说明更新示例，按其同版已明确 oracle 独立核算）；无论 preview 声称哪些确认，它都不改变当前 run 的 basis/授权，输出明确非权威。
shared steps: env, protocol, fixture
primary AC bindings: AC-045, AC-047
review status: PASS

CASE-082
mode: blackbox
description: token-cap 预算 Ask 的一次继续只放开一个容量裁剪 batch
procedure: START 前 CONFIG/项目配置固定 capacity=2、tokenCap=1000；准备正常 S5：三项尚未签发 eligible=blackbox execution/G0/G1，有限 request/attempt 均有余额，已结算 engine token=1000。前态的 source 输入和结算若构造须保存完整真实格式材料、当次 SOURCE parser 输出，明确模拟前置，不手填 controller guard/授权。实际 DRIVE 和各 HOST 的拒绝调用先保存 TOKEN_CAP Ask 与独立原生调用差分。REQUEST continue(reason 非空) 后保存 preview 只包含当前按 capacity 选中的两个 TaskKey，DECIDE confirm 使用当前 ask.id/token/digest/reason；实际 DRIVE/HOST 消费允许的两项，在第三项仍 eligible 时再次 DRIVE/HOST，随后原样重放原 DECIDE。这里 continue 只测试 TOKEN_CAP；request/attempt 耗尽的 typed Ask/单次额外 attempt 由 CASE-022/026/081 的实际对应授权测试，不能借此调用突破次数边界。
oracle: 未授权 TOKEN_CAP 的新增原生调用=0；这次确认的 Ready batch 恰包含 min(2,3)=2 个当前 TaskKey，逐项最多消费一次。第三项未被本次授权，重复 DRIVE/原 DECIDE 不能为它调用，guard 仍有效并再次要求当前有限授权。request/retry policy 不因 token-cap continue 增加。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, source-fixture
primary AC bindings: AC-046
review status: PASS

CASE-084
mode: blackbox
description: token cap 达到边界且次数仍有余额时停止 Issue/refill/恢复重试
procedure: START 前项目配置 {tokenCap:1000,capacity:2} 经 CONFIG 确认。source-fixture 分别建立已结算999、1000两前态，各还有request/attempt余额；owner 的5000仅作为明确模拟独立报告输入。正常已签发且attach的action按native-attached准备，逐行独立调用DRIVE新签发、一个完成结果/source后的DRIVE refill、HOST --resume、正确retry授权后的HOST，以及OP model.run/QA target；1000行都先保留无continue的原生调用差分。随后REQUEST continue(reason)+当前DECIDE，读取本次精确batch/attempt，再实际消费并重放原决定。每个操作的binding来自当前show，不把被其他条件拒绝当cap证据。
oracle: 999 未到 cap 时仅依其余 guard 允许调用；1000 及以上所有自动路径均零新调用并显示 cap/used/source 与有限 Ask。owner 5000 不计 cap。一次合法继续只解锁所绑 batch/attempt，重放不重复放开。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, source-fixture, native-attached
primary AC bindings: AC-072
review status: PASS

CASE-085
mode: blackbox
description: cap 重启后保持来源与已用量，修改配置按公开时点作用
procedure: 将 P/.gates/config.json 写为 {"tokenCap":1000}，CONFIG 后 START R 并结算700；重启所有隔离客户端后 READ(show)。再直接把同文件 tokenCap 改成1200，运行 CONFIG 并创建新 R2，读取 R/R2 的 regular.settings 与 cost。将 R 达到1000后重启并 DRIVE。保存修改前后文件、values/sources、R/R2 cap/used/guard。 本例 700/1000 的用量前置使用隔离 SOURCE 输入的真实格式 transcript fixture，保存原始事件、原生 response IDs、start/complete 时点及累计求和脚本，并明确计量输入为构造材料；目标是实际 SOURCE parser/持久化/CONFIG/DRIVE 规则，不把这两份 token 数称为真实宿主模型用量。只构造正常可达 source 事实，不写 controller.cost 或 guard 派生结果。精确 SOURCE 输入按 source-fixture 构造并实际解析。
oracle: CONFIG 当前文件值=1200；既有 R 生效 cap仍1000、原source和已用700不变，新 R2 cap=1200；R达到1000后重启仍阻断。配置只影响新run，重启不重置用量或解除限制。
shared steps: env, protocol, fixture, task-operation, native-evidence, source-fixture
primary AC bindings: AC-073
review status: PASS

CASE-086
mode: blackbox
description: 未配置与明确不可计量有 unavailable 语义，待 stop 不冒充不可用
procedure: 独立 run 分别从 P/.gates/config.json 删除 tokenCap且确认隔离用户配置无该字段、source 明确不可用、result 已到但 lifecycle stop 未到；READ 与 DRIVE 到次数边界。另把 P/.gates/config.json分别写 {"tokencap":1000}、{"tokenCap":"1000"}、{"tokenCap":-1}，执行公开配置诊断与启动。 未配置行 CONFIG 显示 tokenCap 缺省且 guard.tokenCapStatus=UNCONFIGURED；SOURCE UNAVAILABLE 行使用真实当前 action/attempt/provider/identity 和明确 reason，保存 AVAILABLE 与 UNAVAILABLE 分列；result 先到的行 SOURCE 仍 RUNNING，后续 STOPPED 必须实际 transcriptPath/session_meta 身份一致。不能以无 stop 的空值冒充 UNAVAILABLE。每项配置错误另用新 P/R，诊断修正后重新启动；保留常见错误的字段路径和原文件字节。
oracle: 未配置时明确 UNCONFIGURED、来源明确不可计量时 UNAVAILABLE，两者均不把次数限制取消；待 stop 的进行中来源为 COST_PENDING/Wait，不得误写为终态 unavailable。三种错误各定位真实文件和 tokencap/tokenCap 字段并拒绝错误生效；修正合法数字后 CONFIG/START 恢复。
shared steps: env, protocol, fixture, task-operation, native-evidence, source-fixture
primary AC bindings: AC-073
review status: PASS

CASE-087
mode: blackbox
description: 结果先到 stop 后到只在完整结算后 refill，重放不提前补位
procedure: 真实native-attached前态建立一个已签发worker的原生身份，来源仍RUNNING；使用其实际已产生完整结果但尚未SOURCE结算的时点保存结果事件并SUBMIT（仅该原生原文，不模拟输出）。保持真实app-server/source未停止，DRIVE并比较下一eligible的原生调用集合。正常完成/停止该隔离进程后，从原生receipt取得实际transcriptPath/session_meta.id，以SOURCE status=STOPPED提交并DRIVE；解析、cost、guard和refill的公开投影在同一次提交后观察。原样重放已保存result/SOURCE事件，再DRIVE。若宿主自动提交过快未保留先后时点，可用真实输出的普通复制件准备正常COST_PENDING前态并明确标前置；本例SOURCE解析/补位仍实跑，真实宿主自动回传链由完整canary另证。
oracle: 未 stop 始终 COST_PENDING/Wait且 refill 调用=0；收到可结算事实后按结果→source→usage→guard→refill 顺序一次推进，guard 阻断则不 refill；重放无重复结算/调用。source 不能通过 legacy writer 旁路补账。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, source-fixture
primary AC bindings: AC-048
review status: PASS

CASE-089
mode: blackbox
description: Codex 用量按 response ID 去重，timing 包含工具等待不含 turn 间空闲
procedure: 在真实当前action用OP model.run两次，prompt分别含唯一<R>-usage-1/2，保存两次原生identity/transcript、实际token事件和task_started/task_complete。另一次允许工具等待的真实开发worker执行一个实际可计时操作（例如本例脚本等待子进程2秒），保存工具起止与原生task区间，两个turn之间由owner等待2秒再HOST --resume，以区分turn内等待与turn间空闲。普通复制这些真实来源到隔离计量输入，同一response镜像行重复一次，再按source-fixture/SOURCE公开入口解析；逐原生response ID独立求输入+输出token和有效task区间和，与cost/timing机械比较。缺task边界的独立复制件删除该输入记录并标模拟格式缺项，实际parser必须省略timing/标unavailable，不能手写时长。
oracle: 每个原生 response ID 只计一次，transcriptPath 对应实际来源；duration 等于有效任务区间累计并包含工具等待，turn 间空闲不计。缺计时边界的独立输入省略 timing/标 unavailable，不能猜数。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, source-fixture
primary AC bindings: AC-049
review status: PASS

CASE-090
mode: blackbox
description: checkpoint 不提前固定 cost，续跑终结后结算完整派发且 owner 仅报告
procedure: 真实QA action用OP完整执行a，提交CHECKPOINT(cases=[a真实条目],remaining=[b,c],message=正常中断原因)，保存此时operations/transcript与未终结cost。正常取消/停止后HOST --resume同一identity/attempt，继续b/c各prepare-target-collect并提交完整最终结果；最后SOURCE STOPPED使用真实transcript。按native-evidence合并此action所有原生turn和kind=task-operation来源，原生ID去重，比较最终dispatch完整token与timing。owner实际来源若可获得只计算本run起止差值，若不可获得明确UNAVAILABLE；不把用户账户rate limit百分比或手写5000当实际owner token。
oracle: checkpoint 不把部分 token 固定成终态总量；最终 dispatch 覆盖完整续跑区间且去重。owner 差值只进报告，不改变 in-flight used/cap/basis，owner unavailable 与 token cap unavailable 分开。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, source-fixture, interrupt-evidence
primary AC bindings: AC-049
review status: PASS

CASE-091
mode: blackbox
description: 额外返修授权过期/摘要不匹配拒绝，owner 与 cap 可用性分列
procedure: 用已到额外返修 Ask 的前态，保存完整摘要；改变一个 problem 的合法裁决使 revision/digest 变化后提交旧授权，再提交当前摘要授权。 额外返修前态必须完整 SOURCE 结算、全部 WAVE_ADJUDICATE 且默认已完成三轮、仍有 OPEN problem。REQUEST authorize-wave(target=当前wave.id,keep=当前 OPEN IDs,reason) 后保存 Ask；再通过同 wave/target/resultDigest 的 WAVE_ADJUDICATE（replacesRevision=当前有效裁决）合法改一项，并以旧 Ask 的 id/token/digest 做 DECIDE additional-wave，观察拒绝。随后重新 REQUEST 保存新 Ask 并 DECIDE additional-wave 只授权一次。 保留 CASE-091-stale 已完成三轮、旧Ask拒绝、当前授权一次和重放不增量的全部真实证据；以下两个可用性组合不重跑该三轮。以下公开字段来自 /tmp/phase4-owner-public-observation-audit.json 指向的原生工具调用及实际 START/show/终态drive/show输出；该诊断探针使用固定模型响应，仅用于确认入口与字段位置，其会话、baseline和数值不能作为本例QA证据。正式执行使用本次真实Codex模型、独立真实transcript和精确安装；不得复用该探针模型服务、改写usage或手填owner桥文件。
A（owner不可用、cap可用）：优先复用同候选/安装下既有适用独立run，否则只建最小P-A/R-A。START前在项目.gates/config.json设置tokenCap=1000000，保留其余实际配置并CONFIG；通过外层普通subprocess以START完整argv启动，不由原生主线程shell工具调用该START，不手工调用hook或填写owner。READ(show)保存regular.ownerStartedAt/ownerTranscript/ownerSession/ownerProvider/ownerCostBaseline实际存在情况，证明无本run主线程启动绑定。按protocol/fixture合法确认需求、选择regular，取得首个实际签发action，通过HOST运行真实Codex worker并结算其真实SOURCE；未完成前保持COST_PENDING，不能提前当不可用。无需跑满产品/技术审。保存这次真实原生会话和完整transcript、真实SOURCE及cost.dispatches；不修改usage为999或其他预设数。READ(show)须见regular.settings.values.tokenCap=1000000和regular.guard.tokenCapStatus=AVAILABLE，并保留实际派发用量、来源及guard.usedTokens。随后REQUEST {action:"abort",reason:"完成owner缺来源与派发cap独立可用性观察"}，按当前Ask的requestId/token/digest/revision DECIDE confirm，再DRIVE到status=COMPLETE、next.Kind=COMPLETE、regular.terminal=ABORTED。READ(show)及summaryPath指向的终态报告保存cost.owner的明确不可用表示和原因；仅active字段缺失、终态cost.owner缺失或默认零值均不能冒充已明确UNAVAILABLE。保存终态前最后有效cap投影与终态owner报告各自时点。
B（owner可用、cap不可用）：优先续用既有真实主线程START捕获owner且可正常终结的适用独立run；否则建立最小P-B/R-B，在START前设置同一正tokenCap，并只精确信任该隔离安装的PreToolUse hook。沿CASE-174已定义的本地信任步骤，在相同隔离CODEX_HOME/project先initialize→thread/start加载项目，再hooks/list {cwds:[P-B]}选安装命令/sourcePath/eventName对应唯一h；config/value/write {keyPath:"hooks.state."+JSON.stringify(h.key)+".trusted_hash",value:h.currentHash,mergeStrategy:"upsert",filePath:该隔离CODEX_HOME的config.toml绝对路径}，回读后hooks/list确认同key/currentHash为trusted且enabled。不使用全局bypass。
由该真实原生主线程的shell工具实际执行完整START argv；这是真实owner主线程，不是workflow worker或手工hook payload。保留原生thread/turn/session和实际工具命令、安装PreToolUse输入/输出/退出证据。随即READ(show)保存regular.ownerStartedAt、ownerTranscript、ownerSession、ownerProvider、ownerCostBaseline.usage/size/digest。逐项核对ownerSession等于本次原生thread/session ID、ownerTranscript指向其真实rollout、ownerProvider="codex"；保存原始transcript及baseline，不复制诊断探针字段。以真实模型完成至少一次START后的短回复，保留实际新增用量记录，以便终态差值可与基线区分。
按protocol/fixture准备首个实际已签发action并提交带明确原因的SOURCE UNAVAILABLE：此行的高成本前置按既有隔离fixture代表没有可读原生派发transcript，明确记录其构造来源及缺来源事实，不伪造真实worker调用、transcript或已测得零用量。owner始终来自上述真实主线程。READ(show)须见正tokenCap仍为1000000、regular.guard.tokenCapStatus=UNAVAILABLE且有该派发来源不可计量的原因；UNCONFIGURED、COST_PENDING不成立。然后在同一owner会话通过公开REQUEST abort→当前Ask DECIDE confirm→DRIVE到COMPLETE/ABORTED，READ(show)并保存summaryPath指向的实际终态报告，不调用旧seal/cleanup或内部入口。
B的活动与终态输出按已观测公开路径分别定位：活动owner绑定在regular.owner*；终态统计在cost.owner，其source须为"transcript"，inputCacheHitTokens/inputCacheMissTokens/outputTokens/totalInputTokens为实际数值。保存terminal DRIVE stdout、后续show stdout、终态采集时点真实rollout原始字节与时间边界；以原生response ID去重的累计用量分别减去ownerCostBaseline.usage的InputCacheHitTokens/InputCacheMissTokens/OutputTokens，逐项比较cost.owner对应小写字段，totalInputTokens严格等于hit+miss。只比较终态采集边界前记录，不把终态后的新轮回复计入差值；不凭诊断探针的2/1等数值填写期望。终态drive与show的同一报告须一致。A/B均保留终态前最后有效guard与终态owner报告各自来源/时点；终态若也保存guard则再对账，不能声称两者在同一时刻生成。任何实测缺绑定、缺报告、无法明确不可用、不可计量或未到Complete的分支保持未完成并保存实际结果，不删除两种独立可用性义务。
oracle: 旧授权拒绝且无新repair；合法授权只放开当前一次，重放不再增加。摘要findings/problems/历史/scope/TaskKey/额度完整，engine allowance从权威basis取值。A的终态owner有明确不可用表示及缺来源原因，而该run最后有效guard.tokenCapStatus=AVAILABLE且有真实可计量派发来源；B的终态cost.owner.source="transcript"，三个token分量精确等于真实终态累计值减本run捕获baseline，totalInputTokens==inputCacheHitTokens+inputCacheMissTokens；该run最后有效guard.tokenCapStatus=UNAVAILABLE且原因对应明确不可计量派发来源。两行捕获的tokenCap均为正1000000；owner只进入终态报告，不计入实时guard.usedTokens。cap不可用不抹去独立owner报告，owner不可用不抹去已测派发cap；各来源和比较时点分别保留。没有安装hook实际执行、绑定的原生起点、实际终态统计，只有worker SOURCE、缺字段、默认零、UNCONFIGURED、COST_PENDING或未取得COMPLETE，都不满足相应组合。诊断探针固定模型/用量不作为正式证据。旧Ask授权分支只按适用原证据继承，不因两个新可用性前态重复执行三轮；任一组合缺证据均不得称本例完整PASS。
shared steps: env, protocol, fixture, task-operation, native-evidence, source-fixture
primary AC bindings: AC-050
review status: PASS

CASE-092
mode: blackbox
description: 真实宿主入口矩阵的合法创建、恢复、模型与 QA 调用逐项可执行
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。逐行矩阵为外层HOST start、HOST --resume、内部CLI OP model.run、原生动态formal_gates_operation model.run，以及QA三stage各自的CLI和动态工具。外层start从真issued动作调用，resume从真停止且claim.operation=resume的同Batch/QA动作调用；内部CLI用native-attached，动态工具用原生任务实际callId与{kind,caseId,prompt}。model prompt固定“仅返回 <R>-model-1”，再第二次唯一prompt；QA用native-evidence的空commit marker批准case按prepare→target→collect。逐行记录实际JSON-RPC/CLIargv、所有当前binding、规范文件消费、原生before/after与结果；原生worker正常返回后由bridge实际接纳其正式类型结果。 本轮新测试 run 的真实模型使用 boundary-native-luna 的 gpt-5.6-luna/max，既有 run 仍核对其已捕获设置。对 worker→model.run 分别从 CLI OP 和 worker 实际动态工具发唯一 prompt，保存实际辅助 thread 的 parent/来源关联及完整工具清单。纯文本返回作为 TaskOperationReceipt.output 留在所属 action 的 operations，不由其直接提交正式 REVIEW_RESULT/VERIFICATION_RESULT。每次比较 TaskKey/Attempt、wave、授权、完整 cost.sources；内部原生 identity/transcript 与 cost.kind=task-operation 一一关联，task-operation 不变成新的外层 attempt。后续返回需要外层 worker 按原正式类型提交，不能把辅助文本中的 PASS 当门结论。
oracle: 每行有真实底层调用与完成识别，不仅 wrapper 自报；校验字段齐全且与 issued action一致。合法任务内多次请求复用原 action/TaskKey，未逐请求造新 TaskKey，拒绝传播被调用方可见。 两条 model 路径的实际模型/effort 符合配置，辅助调用归原任务额度，有原生 parent/callId 对应；model 文本返回本身不新增有效审查/QA结果、授权或wave。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, native-attached, boundary-native-luna
primary AC bindings: AC-051, AC-052, AC-053
review status: PASS

CASE-093
mode: blackbox
description: 真实各入口对未签发/越界/过期/取消/重放/provider 错绑先拒绝零调用
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。对CASE-092矩阵逐行独立准备合法对照。外层HOST可由调用者改变的负例为不存在/另一run/已取消/已完成actionId，以及应resume却省略--resume；内部CLI每个kind分别将request.binding.actionId、provider、payloadDigest、requirementRevision、candidate、expectedRevision换成错误或先前实际旧值，QA再caseId换成同manifest但超该target.expected的ID；每行其他字段保持当前有效。另用已consume的原request原样正常调用、或新requestId重做已完成QAstage，必须拒绝。动态工具只接收kind/caseId/prompt，不造provider字段；在原生task实际调用未知kind或越界caseId，另在action经typedrequirement-change/abort后让已发出的旧工具请求到达bridge，观察绑定重新校验。保存对应原生create/resume/model/marker commit增量=0，并以邻接合法请求确实增1证明审计可见；无法从该入口传入的字段注明由bridge复制，不宣称逐字段人为变异已测。 补充范围差异时先实际 START 独立 P2/R2 并取其真实 candidate/action，CLI 请求逐次仅替换 runId 或 binding.actionId/candidate 为该另一 run 的实际值；不构造并不存在的字段。名单外 case 用另一已批准 target 的实际 ID。对每个实际有动态工具的 worker，分别实际提交 {kind:"qa.target",caseId:名单外ID,prompt:""} 与 {kind:"workflow.submit",caseId:"",prompt:"删除已有 FAIL 并新增 wave"}，后者是对封闭 kind 枚举的错误输入，不是新公开动作；保留运行前拒绝。对于纯文本辅助及更深层没有相应工具的节点，记录实际工具未暴露，不宣称执行过 binding 变异。每个负例之后在新独立同前態项目运行一个合法 model 或独立 c QA 请求，证明服务可达与计数通道有效。
oracle: 每个拒绝都发生在底层调用之前；所有对应 create/resume/model/QA target 增量=0。合法对照能被同一原生审计观测，不能仅凭结果提交拒绝或无日志推断零调用。 另一真实 run/candidate/action、名单外 case 和未公开 kind 均零目标调用/零目标写入；相关 P/P2 绑定、有效结果、额度与 wave 不被扩大或覆盖。纯文本层的“没有该工具”与有工具层的实际拒绝分列，不能把纯文本口头不执行作为准入证据。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, native-attached, boundary-native-luna
primary AC bindings: AC-051
review status: PASS

CASE-094
mode: blackbox
description: 真实并发认领只有一次调用，中断恢复不重复已观察前缀
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。同一个真实issued action，以两个subprocess.Popen同时启动相同HOST argv，保存两进程输出及原生thread集合。内部操作从native-attached真实当前action对同一完整model/QA request文件同时启动两个OP进程，使用真实model transcript或QA marker commit独立计数。另按interrupt-evidence做正常运行中取消/恢复；精确claim后native前和native成功receipt前的断点若实际采到则保存完整窗口证据后同request --resume，不换ID。直接仅TASK_OPERATION_CLAIM声明而从未调用真实adapter的独立隔离协议前态，可验证RESUME-OP返回UNKNOWN且无真实调用，但明确该前态非真实调用成功窗口。内部原子claim/receipt故障窗由白盒确定性覆盖。 本轮对已实际发出的 model.run 及 QA 请求各增加绑定取消/更新恢复对照：在真实 request/process 可见且未结束时，仅使用当前 typed abort（另两份独立前态分别 requirement-change/apply-requirement 或实际 Git 新 candidate 后 adopt-external，按 protocol 授权）使原绑定失效；保留这些输入及其真实接纳顺序，再恢复原 request 文件，既不改 ID也不改 expectedRevision。由旧 worker 的真实动态调用重试一次，在所有可达层级记录返回；纯文本辅助只记录不存在直接恢复工具。比较原生进程/标记commit、quarantine/取消状态及新绑定。针对无完成观察的 UNKNOWN 分支，重复 RESUME-OP 两次并检查调用差值仍为0；只有原生完成证据能结算原请求，不能改成新 requestId 逃过对账。
oracle: 每次并发竞争至多一个真实native/QA目标调用，一方拒绝或只读同一已完成回执；同动作/attempt绑定不变。实际已完成观察只结算一次，没有完成观察时UNKNOWN不fresh重做。未命中的细粒度窗口不记黑盒PASS，白盒负责该确定性恢复分支。 已取消或过期绑定的恢复不得新启动目标，也不能提交为当前有效 PASS；可安全结束的已有调用结果按当前协议隔离，不能把已在取消前真实启动的调用误算为取消后新调用。UNKNOWN 两次恢复都不fresh调用，旧执行结果与新绑定不串用。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, interrupt-evidence, native-attached, boundary-native-luna
primary AC bindings: AC-052
review status: PASS

CASE-095
mode: blackbox
description: provider、bridge、lifecycle 能力分开诊断，缺 bridge 不降级 provider
procedure: 独立安装/项目配置A的codexExecutable指向真实Codex绝对路径；分别直接运行该原生文件 --version 与 app-server --help保存provider/bridge能力，再CONFIG/START/选择regular并真实HOST，读取实际capability/来源诊断。B把codexExecutable写成不存在的常见路径，CONFIG后选择regular，保存精确不可用原因和零原生create，修正路径后新run正常。SOURCE已附着但未STOPPED的行由source-fixture准备并实际READ/DRIVE，单列lifecycle待结算；确实UNAVAILABLE时提供reason。当前接口没有独立bridgeEnabled/lifecycleEnabled配置，不发明开关把provider假装仍可用；“provider可用但bridge能力缺失”的内部能力探测组合交白盒，外层正常安装诊断必须据真实检查结果分列。
oracle: 正常Codex及app-server桥接可实际准入；不存在配置路径明确定位provider/bridge不可用而非换其他provider或legacy。来源待STOPPED为COST_PENDING且与原生命令不可用分开；修正后恢复正常。能力组合不凭不存在配置伪造，内部区分另有白盒证据。
shared steps: env, protocol, fixture, host, task-operation, native-evidence
primary AC bindings: AC-052
review status: PASS

CASE-096
mode: blackbox
description: 真实 RUNTIME_ERROR 经有限 retry 和合法追加后可登记权威结果
procedure: 实际QA fixture把command.executable冻结为本例临时路径E/qa-tool（最初文件不存在），用真实HOST/OP prepare→target触发进程创建失败，原生worker回传实际RUNTIME_ERROR并SOURCE结算；保留TaskKey/attempt。正常修复该普通环境：在E/qa-tool写一个可执行启动脚本，仅exec实际python3及当前候选qa-entry.py与原args，不改变批准argv/候选/需求；记录真实文件/权限/版本。DRIVE取有限retry；若已两次失败，用verification-retry(target=当前TaskKey,reason)+当前AskDECIDE只加一次，再实际HOST/OP成功产出完整QA结果。期间一个不存在actionId的HOST拒绝不污染结果。脚本创建只修复先前声明的缺失工具环境，真实QA仍经冻结command执行。
oracle: 原错误和每个 attempt 可查，合法最终结果能权威登记；非法请求不污染 PASS、不加语义轮。默认上限及追加绑定生效，正常重试成功不要求 Seal skip。
shared steps: env, protocol, fixture, host, task-operation, native-evidence
primary AC bindings: AC-053
review status: PASS

CASE-097
mode: blackbox
description: 真实宿主 QA 操作强制准备→目标操作→结果采集并失败短路
procedure: 批准marker case a（native-evidence空commit command）与b.dependencies=[a]。正例从native-attached用OP依次prepare a、target a、collect a，Gitlog标记由0到1，保存原exit0；然后b可完整三步。各负例独立P/R：未prepare直接target/collect；a尚未collect时b.prepare再b.target/collect；target成功后以新requestId重复target；正常重放已完成collect；另把合法设计的executable冻结成不存在工具，prepare成功target实际FAILED，再collect被阻断。操作请求没有fixturePath/argv/cwd，不能用不存在字段伪造准备失败。正常中断使用interrupt-evidence，在可保存观察的阶段间停止并原文件--resume，只补未完成stage。clone内部失败的异常短路与原子窗口交白盒，公开前置不成立的步骤已逐行实际拒绝。
oracle: 正常真实prepare→target→collect顺序固定，marker只有1个实际commit；非零正常退出可collect并按expectedExitCode判数据。跳步、未完成依赖、重复完成stage、目标进程创建失败后的collect均在被拒步骤前终止，该步骤实际副作用增量0。恢复保留已完成前缀。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, native-attached, interrupt-evidence
primary AC bindings: AC-076
review status: PASS

CASE-098
mode: blackbox
description: 固定操作清单逐项对账，除 Git/QA 外的顺序也由产品强制
procedure: 建立公开操作表：①START→DRIVE预检→REQUEST/DECIDE确认；②实际typed结果→SOURCE→DRIVE/refill；③开发/QA设计审查→DRIVE装配→freeze/wave；④完整wave/裁决/无义务→DRIVE promotion；⑤DRIVE终态materialize/cleanup；⑥publish既有install事务；另列task-operation四kind与Gitstatus/manifest/track/commit链。逐行独立正常前态用本文对应共享命令执行合法顺序及公开可请求的提前/重复输入：①提前route/HOST，②RUNNING source下DRIVE，③QA未批准时DRIVE，④wave未齐时DRIVE，⑤活动run旧cleanup未知命令与typedabort后终态DRIVE重放，⑥publish故障注入和重启install。记录实际输入、公开Plan/回执、原生/Git/文件副作用列表；不虚构StepSpec step字段跳到不可直接调用的内部语句。每项持久化原子内部窗口由白盒按同一操作表逐分支绑定，本黑盒证实公开接入与顺序。
oracle: 清单每行有独立执行记录和 oracle，不只验 Git/QA 两例；合法严格有序，拒绝对应底层步骤增量=0，恢复不重放完成前缀。原子内部不可由黑盒观察的调用顺序交白盒具体绑定，不能以提示词或提交后拒绝冒充本项完成。
shared steps: env, protocol, fixture, host, task-operation, native-evidence
primary AC bindings: AC-076
review status: PASS

CASE-099
mode: blackbox
description: 全部用户配置可发现且公开说明和实际生效值逐字段一致
procedure: 只读取实际安装包用户配置说明、维护/诊断帮助（不读生产源码），逐项建表：文件位置、可编辑字段、类型/单位、默认值、合法值、最小例子、scope、优先级、生效时点、来源/派生区分。每字段在独立配置副本应用其文档最小合法示例，执行公开诊断及适用维护入口生成派生文件，记录实际有效值/来源；另用空配置观察默认值和项目/安装双来源观察优先级。 当前明确字段逐项为 tokenCap（缺省→1000）、capacity（缺省4→2及边界1/64）、codexExecutable（默认codex→实际绝对可执行路径）、model（原生默认→宿主已支持具体模型）、reasoningEffort（原生默认→该模型支持的low）；逐字段CONFIG核对values/sources。隔离用户级 ~/.config/formal-gates/config.json 与 P/.gates/config.json 同字段分别1000/2000时，项目值2000优先，删除项目字段继承1000；不改真实用户配置，可在隔离OS用户/明确隔离配置根运行。新run捕获，旧run保持原 settings。 tokenCap 的真实默认是未配置；缺省→1000 表示配置前后两次观察，不表示默认1000。空配置必须同时排除隔离用户文件的同名值。五个 JSON 字段分别独立应用合法示例：tokenCap=1000，capacity=1/2/64，codexExecutable=本次解析到的实际 Codex 绝对路径，model=本次原生宿主支持的明确模型，reasoningEffort=该模型支持的 low。每项先 CONFIG，再新建 run 验证 regular.settings 的值和来源。安装/宿主派生配置通过同一已记录 install/bootstrap 参数重新生成并核对 receipt，不把编辑派生配置/registry 当用户配置入口。
oracle: 表中每个既有配置及 token cap 均有可验证说明和当前结果，无遗漏/未知字段；实际默认/优先级/时点符合声明，可编辑源与派生文件分清。无需源码、state/digest/receipt 或多处重复维护即可调整；生成派生产物走受支持维护入口。
shared steps: env, protocol, fixture
primary AC bindings: AC-074
review status: PASS

CASE-100
mode: blackbox
description: 同一配置变更用户文本编辑与授权 AI 编辑得到相同值和行为
procedure: 本例只对新 run 比较。两份配置副本的共同前态实际有效 model=gpt-5.6-luna、reasoningEffort=max；model 行在 A/B 中均先省略项目 model 字段、保留 max，reasoningEffort 行在 A/B 中均先省略项目 reasoningEffort 字段、保留明确 Luna，保存继承来源及原始值。只有完成该行指定编辑后才 START/HOST，因而实际流程调用均为 Luna/max；不为观察前态默认值启动额外 HOST。五个字段各用两份从相同已 bootstrap 前态取得的独立隔离配置副本 A/B；A 以普通文本编辑把 tokenCap 缺省→2000、capacity 4→2、codexExecutable 原生命令名→真实同一 Codex 可执行文件绝对路径、model 原生默认→gpt-5.6-luna、reasoningEffort 原生默认→max。B 由 AI 接收只修改同一字段到同一值的明确指令后写同一逻辑位置 P/.gates/config.json；记录完整指令和两份文件 diff，其他字段不变。每份 CONFIG 比较 values/sources，再 START 新 run 保存 regular.settings；capacity 实际 DRIVE 对照签发上限，codexExecutable/model/reasoningEffort 各实际 HOST 一次并从原生启动 argv/会话审计确认实际值，tokenCap 使用已知 SOURCE 计量 fixture 达边界时实际 DRIVE 对照阻断。同一字段只在项目文件维护，既有 run settings 保持原捕获。 原生三行的 stdout/argv/turn-start 必须证明 model=gpt-5.6-luna、reasoningEffort=max；运行中已存在 run 的 settings 仍保持原捕获值，不能以改 state 或重建旧 run 取得一致。
oracle: A/B 同一字段同一值产生相同有效值和可观察行为，未改字段保持原值；无需同步副本/手算 digest，AI 不修改内部 state/receipt/编译制品。
shared steps: env, protocol, fixture, task-operation, native-evidence
primary AC bindings: AC-075
review status: PASS

CASE-101
mode: blackbox
description: 配置拼写/类型/边界错误定位并可修复，不能手填派发授权
procedure: 从配置字段表为每项适用类型准备常见错拼字段、类型错、超合法范围值，执行诊断及受影响入口；按错误提示修正并重跑。另在可编辑配置加入试图指定 request allowance/attempt额度/用户授权的字段。 确切JSON样例依次为 {"tokencap":1000}、{"tokenCap":"1000"}、{"tokenCap":null}、{"tokenCap":0}、{"capacity":0}、{"capacity":65}、{"capacity":1.5}、{"codexExecutable":""}、{"model":""}、{"reasoningEffort":"typo"}；每次 CONFIG，再改回 {"tokenCap":1000,"capacity":2}。 未提供确切值的“手填授权”负例固定再测 {"requestAllowance":99}、{"retryAllowance":99}、{"userAuthorization":true}，每个 JSON 也均单独 CONFIG 后新建 START 验证；它们不在可编辑 schema 中，必须指出文件/字段并拒绝，不生成新的 TaskKey basis/authorization。修复文件后再 CONFIG 与新 START 验证正常恢复，不能只看修复后文件文本。
oracle: 每个常见错误指出实际文件/字段及可操作修正，纠正后恢复合法值；错误不能静默变为默认无限制。手填 engine 次数或授权字段拒绝/不生效，不新增 basis 或授权。
shared steps: env, protocol, fixture
primary AC bindings: AC-075
review status: PASS

CASE-102
mode: blackbox
description: 当前隔离 installed binary 经真实QA失败和唯一返修完成 Git no-split full canary
procedure: 本例只验证本轮 env 安装回执绑定的精确候选（本次 thaw source=a486467975f7da499b79dc201abac4ef4ffa5713）；执行前保存包/launcher/源码提交身份，重新建立 P=T/projects/CASE-102-a486、R=bb-case-102-a486、E=T/evidence/CASE-102-a486。旧36d7/d902 run不得由本安装续写；仅复制旧 case102_start_current.py、case102_tech_current.py、case102_inject_current.py及其公共调用驱动的脚本文本和toy需求输入，在E保存修订diff，替换硬编码FG/P/R/E并删除任何模拟结果、复制state、固定actionId或旧PASS注入路径；旧证据保留原身份；逐项沿正式scope及已有适用carry依据引用已完成PASS，明确它们不是本新run重新执行。fixture在本例只提供toy需求与文件内容规格，不使用S0–S6或attach_fixture/settle_fixture模拟前态。
1. 在新P真实git init、配置QA姓名邮箱，写fixture的requirements.md、README.md、seed.txt及case-set的design-notes.md并提交baseline B；生产代码和测试由真实开发/白盒worker产生。按boundary-native-luna在START前设置独立配置，容量仅取满足开发与黑盒并发的既有配置，不人为降低tokenCap制造已完成专项；CONFIG确认来源后START，按protocol展示完整文档/精确revision、确认requiredSources和regular模式。对当前签发产品审真实HOST当前正确action，取得本修复版本正常创建及规范提示词消费证据；未签发action负例已有有效证据，不重放。全部产品/技术审、finding核实裁决及默认必要轮次均取真实原生结果，不能以F/G驱动中的预填PASS替代。实际正常派发保留source/cost/guard观测；仅真实正常继续所需的暂停按当前Ask处理，不人为触发TOKEN_CAP、不重跑guard-preview/continue专项。
2. 技术审完成后按当前Ask确认no-split及full路线（两QA和安装包全部普通门），按真实技术建议登记最小两个顺序Batch：B1实现greet/Alice及无参数行为和自测，B2补-h、qa-entry.py及交接；每批DoD、文件所有权和实际验证写入并确认计划。真实启动已签发B1与blackbox-design的HOST，用两个Popen并行并保存原生时间区间，证明开发与黑盒设计重叠；B2按依赖执行。黑盒设计独立真实生成toy三行为命令及oracle，白盒在开发后独立真实设计/复用/审查；全部PREPARATION_RESULT和SOURCE来自本次worker。任何设计真实FAIL按当前公开恢复Ask处理，包括合法回开发，不压成PASS；这一前期返工不是本例QA FAIL/repair证据。程序实际track/commit、装配并冻结完整Q0和FROZEN case-set，尚不启动QA/门执行。
3. 对已完成的需求保留、guard和首宿主准入负例，读取原CASE-102实际证据与正式scope/carry记录，保存原installed identity、run、候选、结果及适用理由的逐项引用。其有效PASS只沿已有适用carry依据复用，不重做末尾空行变更、tokenCap=1、not-issued HOST或其他独立专项。缺乏适用carry的项保持其实际状态交接，不伪造本新run已执行。新run在第1–2步的真实受理、开发和QA准备是到达本次未完成核心链的必要前置；此处直接使用其完整冻结Q0作为Q，不为重建前态重复已完成专项。
4. 在当前全部准备已完成、无在途worker、QA/门尚未执行的前态，真实git restore --source Q --staged --worktree -- .把完整候选带入用户Git工作区。沿旧注入脚本仅改contest.go的run(args,out,err)中Alice输出为BAD\n；若实际使用message变量就在实际输出前加入if args[0] == "Alice" { message = "BAD" }，否则只改等价实际Alice分支并留精确diff。保留requirements.md、所有*_test.go、qa-entry.py、case命令/oracle的原始字节；git add contest.go、git commit -m "QA Alice BAD error sample before execution"得F。真实go build -o E/greet-error .及实际binary Alice/无参/-h，分别留(stdout,stderr,exit)=(BAD\n,空,0)、(空,usage\n,2)、(usage\n,空,0)。此处只建立错误代码输入，尚不是正式QA结果。
5. 按adopt-current-candidate从最新show取C/B，采集原始git diff --binary C F --和摘要，REQUEST adopt-external，external={identity:F,lineage:B,diffDigest:实际摘要}；impact product-review/start-readiness/development/blackbox-review/whitebox-review=unaffected，blackbox/whitebox及每个当前普通门=affected；keep仅为当前真实有效的product-review/start-readiness/blackbox-review/whitebox-review。reason逐项说明：需求/计划/测试文件、输入、断言、命令、case绑定内容不变，Alice分支结构与入口不变，已有验收仍能识别其输出错误，因此保留设计/审查和历史批次；该保留绝不声明BAD实现通过任何执行验证。保存前后逐项审批/测试字节和当前Ask影响预览，精确DECIDE。实际DRIVE装配/冻结F派生的Q1，核对其Alice仍输出BAD且冻结case仍要求Hello Alice，所有QA/门执行均对Q1，没有将Q0的QA PASS作为Q1结果。若公开预览无法保留这些未变准备，或实际必要白盒设计发现FAIL，照当前受控回开发合同记录处理，不能禁止合法回开发、压低发现、改state或不断再注入；该分支尚不能声称本例核心链通过。这里保留的是现有测试充分性/可执行性审查，所有候选正确性执行失效。
6. 对Q1全部已签发QA/普通门真实HOST。黑盒worker用task-operation对冻结Alice command逐一qa.prepare→qa.target→qa.collect，完整真实(stdout=BAD\n,stderr=空,exit=0)对冻结Hello Alice\n oracle形成有效FAIL，其他case及白盒/门如实执行并返回各自结果。收齐整个wave及全部SOURCE后，Operator核实真实失败、逐finding WAVE_ADJUDICATE，观察唯一repair-worker签发；该worker真实修复生产代码并运行build/projectTests/affectedCases，按REPAIR_RESULT由DRIVE真实Git提交Q2。完整留Q1→Q2修复diff、唯一修复TaskKey/原生身份、wave/obligation清单，随后对程序要求的AFFECTED和最终FULL全部真实复验；未影响结果若合法继承保存其单独receipt，不冒充实跑。所有义务清空才由DRIVE实际promotion/Seal，读取终态show/receipt、最终Git身份、validatedDigest/finalDigest及cost/guard。复用同新run已完成动作与SOURCE，已完成不重复HOST；缺失项只续当前合法步骤，不重开已终态run。
oracle: 同一当前隔离安装、新run完整覆盖真实产品/技术审、开发与黑盒设计的原生重叠、白盒设计/复用/审查、冻结和full wave；新run必要前置均真实运行。已完成需求保留、guard及首宿主准入负例保留其原身份、精确绑定和正式scope/carry依据，只复用适用PASS，不重放独立专项；不声称旧证据是本新run实跑。本修复版本的正常HOST、准备和派发均有本次真实身份/结果/source/cost/guard证据。至少一个Q1冻结命令实际得到BAD\n而Hello Alice\n oracle不变，形成有效QA FAIL；设计失败/回开发、运行时错误、注入自测输出均不替代这个QA FAIL。保留的审批只证明未变测试仍适用，没有把BAD判正确；不得预填审查/身份/结果或改内部运行态。完整wave裁决后恰一个统一repair任务真实修复该FAIL，adapter生成不同于Q1的新候选Q2，所需AFFECTED/FULL真实通过、义务清空后promotion/Seal成立，最终实际Alice输出Hello Alice\n、无参exit=2和-h exit=0，原FAIL仍可追溯。任何QA前错误被修好而无随后真实QA FAIL、公开前态无法建立、非法复用旧PASS、缺失新候选重验或仅历史Seal都不满足本例；真实受控回开发本身不认定为产品缺陷。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, adopt-current-candidate, fa5-reuse, boundary-native-luna
primary AC bindings: AC-054
review status: PASS

CASE-103
mode: blackbox
description: 候选、开发与稳定环境隔离证明完整，固定 driver 仍可正常使用
procedure: 对 env 中候选安装/测试项目/host config/state/resources、阶段开发 worktree、稳定 package/launcher、锁定正式 driver 分别运行 realpath/Lstat 并记录 canonical 绝对路径、文件类型/软链目标与 source/package/installed identity。按候选、开发、稳定/固定 driver 三个环境建立清单；逐个检查实际运行输入是否解析进另一环境，以及是否存在跨环境相同路径、一个受测根包含另一个受测根的情况。共同的 /Users 或 /tmp 祖先不算目录污染，同一候选内合法登记的 package/launcher/resource 归属关系也不误算跨环境共享。分别实际运行候选 FG workflow diagnose --root P --config，以及固定 driver 随包已文档化的正常只读入口（完整 argv/绝对 binary 记入证据），保存输出和 driver 身份。
oracle: 三环境无运行输入软链/目录包含污染，disjoint proof 可复核；最终验证调用的是实际安装候选。固定 driver 身份仍等于本 run 锁定版且正常，不被候选替换，不使用候选给本阶段正式 run 签 Seal。
shared steps: env, protocol, fixture
primary AC bindings: AC-055
review status: PASS

CASE-104
mode: blackbox
description: 真实开发与白盒 Git 中断恢复不重复已有提交
procedure: 独立开发提交和白盒test-owned提交均先通过真实PREPARATION_RESULT/SOURCE到已签发Git operation，保存其id/before/manifest。实际DRIVE用GIT_TRACE2_EVENT记录底层Git，并按interrupt-evidence在运行中正常终止隔离driver；保存HEAD/parent/log/operation状态，重新DRIVE由原adapter观察后继续。若命中真实commit已经成功且receipt尚未写的窗口，必须保留唯一Formal-Gates-Operation trailer及同时claimed/无receipt的原始证明，恢复后该commit集合不增。未命中该微小窗口只记录正常中断场景，具体成功未记账分支由白盒确定性测试。另用CASE-033明确manifest遗漏前态实际DRIVE，保留track/commit未执行的Gittrace。
oracle: 已存在真实成功commit的恢复必须复用其identity，不产生第二commit；尚未提交时仅执行缺少的步骤，snapshot最后指向实际成功identity。manifest失败的track/commit增量0，白盒生产视图/ownership不变。精确未回执窗口需实际命中证据或白盒职责，不靠随机kill推定。
shared steps: env, protocol, fixture, interrupt-evidence, native-evidence
primary AC bindings: AC-055, AC-076
review status: PASS

CASE-105
mode: blackbox
description: 实际发布包 validation 与 portable canary 通过，验证不读取未提交实现
procedure: 使用由最终已提交源码构建并按 env 安装的精确 FG/PKG，实际执行 [FG,"package","validate","--root",PKG] 与 [FG,"canary","portable","--root",PKG,"--format","json"]。在同一交付源码 snapshot 的干净 checkout 执行 ["git","diff","--check"] 和 ["git","diff","--cached","--check"]。保存每次 cwd/argv/exit/stdout/stderr 和源码/package/installed 身份；核对独立构建、go test ./...、go test -race ./internal/validate ./internal/cli、go test -tags=phase0whitebox ./internal/validate、go vet ./... 的实际执行记录与当前候选一致，另核对本次新增并发协议适用 engine 包的 race/故障记录。测试路径若因已确认 legacy 删除发生搬迁，必须由最终公开验证清单提供具体替代命令并与原 AC 对账，不能把目录不存在算通过或自行恢复旧代码。
oracle: package validation、portable canary 和 diff 检查全部 exit 0；包/源码/installed identity 绑定完整。构建与结构测试只是 AC-056 配套证据，不能代替黑盒使用；任一必需检查缺失不能声称该 AC 全部完成。
shared steps: env, protocol, fixture
primary AC bindings: AC-056
review status: PASS

CASE-107
mode: blackbox
description: 完整 wave 无义务时同 identity 整体 promotion 保留验证绑定
procedure: 从真实 Q 已装配、完整 wave 全部收齐/逐 finding 裁决且无义务的 S6，读取 adapter 记录的 source/target 路径、原目标 ref 与 expectedTargetBase；普通 git merge-base --is-ancestor 确认目标基线是 Q 祖先且不存在外部目标漂移。保存两端 git rev-parse HEAD、git ls-tree -r 与公开 validation view。实际 DRIVE 执行 promotion，保存真实 Git trace、operation receipt；再次 DRIVE 至 Seal，读取整组验证绑定。另两个独立正常前态分别 wave 仍有未完 target、或完整 wave 仍有 OPEN obligation，实际 DRIVE 两次并记录 promotion Git 调用增量。没有直接 REQUEST promotion/seal；是否可提升由当前确定性 Plan 决定。
oracle: S6 提升整个已验证 Q，identity 相同时直接绑定整组证据，Seal 无新代码/测试补丁；未齐/有义务 promotion 底层调用=0，不能先提升再验证。
shared steps: env, protocol, fixture
primary AC bindings: AC-029, AC-068
review status: PASS

CASE-108
mode: blackbox
description: Git identity 改变但完整 validation view 等价可用单一精确 receipt 映射证据
procedure: 采用 batch-fixture 的公开双 Batch 驱动和 fixture 的完整 Go toy 文件。独立 P 的已提交基线 B 包含完整最终程序、测试、需求和说明，记录各文件字节及 Git tree；目标工作区之后保持 B 不变。通过公开技术结果、topology 和当前 Ask 确认两个有序 Batch：B1 将 Alice 输出临时改为 Hello Alice temporary 加换行，同步临时测试并按此中间 DoD 实测；B2 普通编辑恢复基线程序、测试和说明的精确字节，执行原最终规格测试。每批仅由 adapter 提交；临时构建产物执行后删除，不进 manifest。选择 {kind:"custom",qa:[],gates:[G]}，G 从实际 catalogGates 取一个普通门。完成两批后 DRIVE 进行实际候选装配，读取最终 Q、完整 canonical view 和所有 VCS artifacts；核对 P 的 HEAD 仍为 B。只有实测 Q≠B 且完整 view(Q)=view(B) 才建立本例前态。若 squash 回到 B，或装配新增文件导致 view 不等，记录具体差异及前态未成立，不增加无意义提交或预填产物。按已有 fixture/protocol 完成所选门 wave、SOURCE 和逐项裁决；模拟前序门结果须明确标为隔离准备，不能称真实门审。完整 wave 无义务后实际 DRIVE promotion/Seal，保存 Git 操作与等价 receipt。旧 adopt 分支证据保留，不重新运行。 本例局部覆盖 batch-fixture：B1 的 task/subtasks/DoD 为将 contest.go 的 Hello 前缀输出改为 Hello Alice temporary 对应逻辑并同步 TestGreetAlice 的精确字符串，B2 为把这些文件逐字恢复 B；依赖仍 B2→B1，其他计划字段按这两个实际边界填写并在 topology 前确认，不沿用原“B2 仅补 README”计划。两批各在 adapter 授权开发 cwd 分别运行 ["go","build","-o","qa-greet","."]、["go","test","./..."]，再以 qa-greet 绝对路径执行 ["Alice"]、[]、["-h"]，B1 Alice stdout=Hello Alice temporary\n，B2=Hello Alice\n，其余严格用 fixture，随后删除 qa-greet；E由外层记录输出。非宿主被测目标可按 fixture 返回这些真实文件、自测、manifest 的模拟 PREPARATION_RESULT 并结算 SOURCE，但实际 Git 提交、候选装配和 promotion 不构造。每个 identity 以 ["git","rev-parse",identity]、["git","ls-tree","-r","--full-tree",identity] 和公开候选/VCS artifact 投影留证；view 比较包含全部已追踪路径、blob、mode、provider 元数据及全部 VCS 外 artifact digest，不能排除 .gates 交付物或只比较 toy 代码。
oracle: promotion 前确认 Q≠B、目标 HEAD=B、完整路径/blob/mode/provider view 相等。promotion 保留目标 B，receipt 精确绑定 source Q、target B、expectedTargetBase、ref、operation ID、无冲突和相等摘要；整组验证证据通过该 receipt 映射，随后 Seal。Q=B、仅生产代码相等、缺 receipt 或实际 fast-forward 到 Q 均不能充当本例不同 identity 映射正例。无 QA 路线明确保持保证 INACTIVE，不宣称完成需求 QA 保证。
shared steps: env, protocol, fixture, batch-fixture, fa5-reuse
primary AC bindings: AC-068
review status: PASS

CASE-109
mode: blackbox
description: promotion 的真实冲突、目标漂移及不等价阻止沿用旧验证
procedure: 从已验证Q的三个独立S6：A在目标同路径实际改成另一内容并普通Git提交，B在DRIVE已显示promotion准备/预期目标后提交另一README变更，C把目标一个受支持可执行文件的mode通过chmod并git add/commit改变以使完整view摘要不等价。实际DRIVE保存目标HEAD/tree/mode/diff、返回冲突/漂移/不等价原因和未Seal。真实中断行按interrupt-evidence在promotion运行时取消并重新DRIVE，UNKNOWN只先原生对账；采到成功未回执窗口时追加其原始证明。公共Git合同没有“转换provider元数据”输入，转换与精确UNKNOWN原子窗口由白盒覆盖，不客户端填写promotion receipt。需要采纳F时走真实adopt-external和新候选/必要重验，不能用reason直接carry。
oracle: 每种不确定/不等价都阻止沿用 Q 结果 Seal，保留具体原因；UNKNOWN 先对账而不再集成。必要时将实际 F 冻结为新候选并按影响/保证重验，不靠字节相似或主代理理由放行。
shared steps: env, protocol, fixture, interrupt-evidence, native-evidence
primary AC bindings: AC-055, AC-068
review status: PASS

CASE-110
mode: blackbox
description: 隔离发布按 Seal/集成/重验后切换，全部安装组件身份一致
procedure: 隔离目标先由冻结的旧稳定 artifact 的 STABLE owner bootstrap/安装，在该副本用其当版公开 START 建立一个活动旧 run OLD；保留 OLD 原 state 的全部字节、version/package/generation 绑定和路径，只使用该隔离副本。记录演练 scope QA/Seal/集成/必要重验凭据并明确 rehearsal，按 publish 精确 argv 安装已提交 CAND；不把演练凭据写进正式 run。实际切换后从 STABLE 指向的新 installed binary 运行 package validate 与 START/regular 正常准入 smoke，逐项读取 runtime、hook/config、managed-rule、pointer、registry、journal/install receipt 的路径/摘要/身份。对 OLD 调用新版 READ diagnose 和 DRIVE，比较其前后字节/原绑定与新版拒绝；旧 run 尚未终结不妨碍安装完成，不由新版迁移或续写。
oracle: 切换无需等待活动旧 run，旧 state/版本绑定不迁移；新版拒绝续写旧 generation。所有组件指向同一新 package/installed identity，generation 单调；未齐 Seal/集成/重验前不能宣称已完成发布，真实全局未被演练覆盖。
shared steps: env, protocol, fixture, publish
primary AC bindings: AC-057, AC-077
review status: PASS

CASE-111
mode: blackbox
description: 隔离发布失败按原事务恢复稳定包且 generation 不倒退
procedure: 每个公开 canonical 安装故障使用全新隔离目标，先以当前正常稳定包完成 bootstrap/安装，保存 STABLE 与 runtime/hook/config/managed-rule/pointer/registry 的实际完整字节、identity/generation。用 publish 的精确 install argv 安装同一个 CAND；仅对这次子进程 env 逐行设置 FORMAL_GATES_INSTALL_FAULT 为 journal-boundary、intent、registry、copy-component:runtime、prepared、switched、post-switch-smoke、pointer、hook、managed-rule、registry-commit、copy-component:prompts、copy-component:gates、verify-stage:installed-target、verify-stage:manifest、verify-stage:realpath、verify-stage:digest，每行一个名字，保存非零结果及 journal/failure receipt。移除该子进程 fault env，用相同 install argv 再启动，让原 installer 对账 journal；不手写回滚或调用不存在的独立 reconcile。恢复到旧稳定状态的观测点保存全部旧组件字节和单调 generation；如重启继续完成新安装，则另保存先恢复后重试的事务时间链，不能将最终新安装误报成旧包回滚。实际从 STABLE/receipt 指向 installed binary 运行 package validate 和既有正常只读命令，核对一致安装。对旧 generation 的活动旧 run 实际 DRIVE，保存拒绝与未改变原 run；故障注入始终不触及正式/全局安装。
oracle: 失败可恢复原稳定安装的一致组件集合，generation 不倒退且旧 token 不续写；无半切换日常入口。失败证据与回退回执保留，没有伪造发布完成。故障注入不在真实全局环境执行。
shared steps: env, protocol, fixture, publish
primary AC bindings: AC-057, AC-077, AC-081
review status: PASS

CASE-112
mode: blackbox
description: 隔离发布后的实际 launcher 继续承载 engine 与 lightweight，已删除旧命令没有回流
procedure: 在 publish 的隔离切换已完成后，把 FG 更新为该切换回执中的实际 launcher，保存解析路径与 package/installed identity；用干净 P 分别按 CASE-001 的具体 START/DRIVE/确认/regular 选择步骤和 CASE-004 的轻量闭环步骤实际运行。随后在独立 P 上对 retired-workflow-surface 清单逐项调用 [FG,"workflow",name,"--root",P,"--run-id",R] 及 [FG,"workflow",name,"--help"]，保留控制命令对照与原始结果。对当前公开说明声明拒绝的 Git split、SVN/P4 与其他宿主按其具体公开申请参数实际请求，保存请求内容与拒绝结果。检查隔离安装随包 README/SKILL/入口说明是否仍把旧 workflow 命令、legacy mode、authority handoff 或固定旧开发 driver 列为当前可用推进方法；逐处记录引用，不把明确标注的历史记录当成当前使用指令。从精确 post-integration package 按发布说明冻结后续 driver，核对绝对路径及同包身份。
oracle: 发布后实际 normal run 的 runtime=engine；regular 的初始产品审与 lightweight 的 unverified 终态分别成立。删除名单每个命令与别名仍为统一未知命令，不能返回兼容转发成功或旧 engine-entry stub 拒绝；没有新旧双路日常推进说明。未支持能力明确拒绝，无 legacy fallback。后续固定 driver 与该已发布包 identity 相等，当前旧开发 driver 不成为该安装包的运行输入或日常入口；本例仅证明隔离时点，真实全局部分仍由 CASE-115 留证。
shared steps: env, protocol, fixture, publish, retired-workflow-surface
primary AC bindings: AC-001, AC-078
review status: PASS

CASE-113
mode: blackbox
description: 逐项历史功能表保留 engine 承接的业务能力，旧命令删除与历史授权分别登记
procedure: 复用已物化 1063 行、44 来源索引和43份实际原文及原 operation 表，不重造全表。原 ABORTED c062cf7587df6730b045c1d8d6bbff30bd599a8bc72458d629d82d59297c3b15 缺失记录与 r2 实物分开，不能用 r2 替代原全文或将缺项变豁免。优先按原 retrieval 从登记 snapshot/path 只读 git show 取原文；仍无法取得时保留 SOURCE_UNAVAILABLE，完整来源核对仍未完成。H-0010 保留原结构预检 operations，另将本轮“历史非 UTF-8 需求文件公开拒绝”分配的实际 CASE ID 绑定其非 UTF-8 子条件。H-0090 拆为同原锚点的两个子行：源码附近需求引用由白盒责任人绑定，普通门实际输入条款由本轮“普通门实际输入保留非显然验收职责检查”分配的实际 CASE ID 绑定，不把整行 INTERNAL_ONLY。新增 operation steps/oracle 与分配 ID 在审查前写到本轮复制表 /tmp/phase4-fa5-blackbox-design/history-public-execution-table.json，旧表和既有正文不改。先执行 history-source-inventory。按 inventory 中每个原 source/AC 或无 AC 的独立规范性条款创建一行，至少保存 stage、sourcePath、sourceRevision、sourceAnchor、originalText、originalDeliveryEvidence、stage4Acceptance、currentCarrier、currentCase、retirementOrDeferralSource、historicalAuthorization、presealResult、presealEvidence、postpublishResult、postpublishEvidence。相同原 AC 含不同可观察功能时拆行并保留相同原锚点，不按整阶段或整个文件合行。对每行人工核对原业务功能与当前需求：旧 workflow 命令名的删除单列为已被 2026-09-08 决定替代；同条款仍承诺的受理/审查/轮次授权/QA/checkpoint/成本/清理等业务功能另保留一行，映射到当前已批准的 engine 用例及实际公开入口。安装、registry/bootstrap、配置、通用成本解析、只读 observation 按当前合同映射，不能因为旧调用侧退役就标已删除。旧 decoder/migration/legacy mode/authority handoff 不要求新版恢复；历史旧 run 仅原始诊断映射到独立诊断用例。逐行将当前用例的实际步骤和 oracle 与原保留行为比较；没有覆盖或接口尚未交付的行标未验证并明确缺口。最后按原文锚点集合与表的 original source 键双向比较，输出 missing、extra、unmappedRetained 三个列表，以及逐状态行数；每个差异附原文，不用 grep 命中数量代替语义核对。
oracle: 所有规范性原功能都有表行，missing 与无来源 extra 均为空；仍承诺功能不存在用“旧命令删除”笼统消掉的行。每个 retained 行均有当前 engine/通用承载位置、对应独立用例与可机械比对的 oracle，未实跑列保持未验证。旧命令替代、阶段5/6待交付、历史已授权跳过均有各自来源，三者均不等于本次 PASS；尤其 3.5b 历史只执行 CASE-029 的授权不自动豁免本阶段仍承诺的其他功能。仅在 CASE-114/115 的当前实跑证据回填后相应时点才可为正常/PASS；原阶段结果、Seal 与历史授权不修改。 原 ABORTED source 缺失未解决时，完整来源结论保持 PENDING；已核对可用来源的部分结果独立保存。新增两项未实际执行前不填历史表 PASS。
shared steps: env, protocol, fixture, publish, history-source-inventory, fa5-reuse
primary AC bindings: AC-079
review status: PASS

CASE-114
mode: blackbox
description: Seal前按已物化历史公开操作表逐项实跑并独立汇合内部验证证据
procedure: 使用本轮 CASE-113 复制表，复用已完成156操作及原1063行索引，先逐 operation 区分当前有效实跑、scope授权继承、FAIL、PENDING、postpublish NOT_EXECUTED；只补当前 scope 的缺项，不把已完成操作另跑一次。内部责任人交接的每个 required 子行必须给 rowId/sourceRevision/sourceAnchor、实际 testRef、当前 candidate、执行命令、真实输出文件、逐断言比较及状态；黑盒只核对这些明确绑定，不读取生产实现推断。缺责任人 testRef、执行输出或相应白盒纠正仍 PENDING 的行继续未验证，不用测试名称/数量替代。新增 H-0010 非UTF8与 H-0090 门输入公开操作是本轮必要并集，按实际 CLI 分配 CASE ID 引用执行证据。先逐source原文与历史执行表行核对，保存missing/extra/unmappedRetained及PUBLIC_CONTRACT_INPUT_PENDING；任何仍承诺公开行为无操作实体或无具体oracle时，列该原锚点缺口，不能用运行smoke代替。对已完整登记及批准的公开operation取并集并按operationKey排序：每项从env的当前精确隔离安装FG启动，执行该operation已有全文steps、输入和oracle；全部argv/输入/退出码/两个输出流、原生宿主观察或真实Git/安装产物按其具体约定保存。为每个operation生成本轮evidence/preseal/<operationKey>索引，明确preparation/observed。每个原来源行仅在其全部必要operation当前有效通过后填preseal公开部分PASS；失败、未执行、授权继承分别保留原状态。所有内部required行由后续已完成白盒设计的责任人提供精确testRef、独立执行输出和同候选绑定，本黑盒只读其证据并按行汇合，不能读取生产实现/历史测试名推断通过。preseal记录中postpublish仍NOT_EXECUTED。 本轮四个 OBLIGATION 操作属于本例全部必需公开操作并集，不能只验证旧 CASE-016/018 的格式/调度。逐项按新表所绑定真实审查及覆盖证据核对 H-0003/H-0004/H-0041/H-0138；任何一项尚未实际通过则保留未验证，本例不得假定证据已经齐备。
oracle: 所有仍承诺公开功能逐行有同一精确隔离候选安装的当前执行证据，operation原oracle全部实际比较，无缺项/错误输出被计PASS；内部证据与公开接入证据分开且精确同候选。缺少任一必需公开或内部证据，该行不得显示完整PASS；表不得用阶段Seal、旧CASE输出、版本号或抽样代替当前逐项结果。
shared steps: env, protocol, fixture, publish, history-source-inventory, history-execution-table, fa5-reuse
primary AC bindings: AC-080
review status: PASS

CASE-115
mode: blackbox
description: Seal前隔离发布交接可完成，并完整保留Seal后全局核验义务
procedure: 本例在 Seal 前只验收隔离发布交接，不以真实全局发布完成为前置。采用 env/protocol/fixture/publish/history-source-inventory/history-execution-table。准备时读取已批准附件 /var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/phase4-operator-adjudication-99aia1ln/phase-4-postpublish-handoff.json，它已完整保存原历史表、全部 operation 正文/共享步骤、实际 CASE 绑定、后置执行步骤/判据、责任角色、证据字段和完成约束；复制到本例 E/postpublish-handoff.json，并在既有发布记录中引用该实际路径，不创建新 workflow 状态或 skip 接口。从 history-execution-table 的实际 rows 重算 currentStatus 属于 RETAINED/AFFECTED/SUPERSEDED 的 currentOperations 排序去重并集，与附件 mandatoryPublicOperationKeys 逐项比对；读取每个 operation 完整步骤/oracle/共享引用和实际 CASE 绑定。逐行对照 1063 条原来源的 sourcePath/sourceRevision/sourceAnchor/originalText/currentStatus/currentOperations/internalVerification 与附件，特别核对四个 OBLIGATION 操作及 H-0003/H-0004/H-0041/H-0138；内部复验仍保留对应来源及独立白盒责任。使用 CASE-114 已实际完成、且同候选/安装 identity/时点/步骤/环境绑定未变的隔离执行证据，按 history-execution-table 的复用合同逐项复用，不重跑相同操作；任一缺项或绑定不等保持缺口。对本例当前隔离已安装候选另在干净 P/R 实际执行 START、DRIVE、READ(show/status/next/diagnose)，保存完整输出，核对实际 runtime=engine 和公开安装回执 package/installed identity；仅做初始受理读回，不要求这份 smoke run 完成后置发布。核对交接的真实全局 FG 恰为 /Users/suxinyang/.local/bin/formal-gates，且后置计划先等待固定 driver Seal/集成/必要重验，再走既有 owner 全局安装事务；当前执行用 FG 仍是 env 的隔离 launcher。保存 E/handoff-reconciliation.json，列出上述并集及逐行差异、preseal 证据引用、真实 smoke 输出与身份、附件归档路径、发布记录引用、后置未执行项及责任角色；发布记录的 postpublish 列此时保持 NOT_EXECUTED，不填安装事务或全局 PASS。最后通过当前公开 qa-execution 按本例下面的 Seal 前判据返回 PASS/FAIL/RUNTIME_ERROR。本例 PASS 仅表示这些交接/隔离观察已完成，后置附件按原完整步骤在 Seal 后由发布责任人执行并回填发布 receipt，不参与本例当前 expected-set 完成条件。
oracle: 当前隔离 START/DRIVE/只读入口真实成功，runtime=engine，记录的 package/installed identity 与本次实际隔离安装回执一致。冻结附件的 1063 条历史来源行及字段与批准表完全一致；全部必需公开 operation 并集逐项相等（当前表为 156 项），每项完整步骤/oracle/共享引用/实际 CASE 绑定存在，四项 OBLIGATION 及指定四行无遗漏；内部复验责任与证据要求未移除。CASE-114 的适用 preseal 逐项证据引用完整且同绑定。E/postpublish-handoff.json 实际存在并被既有发布记录引用，后置顺序、真实全局 launcher、全量复验、精确发布身份、driver 绑定和失败回退约束全部保留；postpublish 尚为 NOT_EXECUTED，没有用本例 PASS 填成后置实跑通过。以上当前观察全部成立即可经现有结果契约登记本例 PASS，不等待 Seal 或真实全局切换；缺少当前应有的步骤/映射/交接/适用证据为 FAIL，实际环境不可运行为 RUNTIME_ERROR。此 PASS 只覆盖 Seal 前隔离发布交接；Seal 后仍承诺任一功能 FAIL/未验证/缺证据时仍不得宣布发布完成或启用后续 driver，必须实际完成附件全量义务，旧 Seal/PASS 不倒写。
shared steps: env, protocol, fixture, publish, history-source-inventory, history-execution-table
primary AC bindings: AC-077, AC-078, AC-080
review status: PASS

CASE-116
mode: blackbox
description: 已提交切换后真实必需检查失败时不宣称发布完成并恢复原包
procedure: 使用history-release-recovery在独立目标完成原owner→维护包→阶段4三段准备且确认stage4 install journal已committed。在E写可执行依赖故障fixture脚本，内容为#!/bin/sh加换行、echo QA_DEPENDENCY_FAILURE >&2加换行、exit 127加换行；该脚本明确是外部Codex依赖不可用的隔离输入，不是原生宿主身份/成功结果。实际运行[FG,"canary","codex-hook","--binary",FG,"--codex-command",该脚本绝对路径,"--worktree",P,"--format","json","--keep-temp"]；保存真实子进程127/标记、命令失败报告。将该必需检查实际失败填入本例发布核对表，责任人记录发布未完成/driver未启用；不把预计失败用例本身的QA通过偷换成被检查发布已通过。然后实际RESTORE，保留完整新事务。换回真实CODEX后按公开链安装同一精确候选并重做受影响必需检查；若包有实际修复，绑定新提交/包再测，原失败记录不覆盖。
oracle: 失败报告来自真正启动的外部依赖进程，当前检查不能PASS；发布核对结论为未完成且不启用阶段5–7driver。已committed安装后确实通过RESTORE新事务恢复原package一致组件与正常installed smoke，generation不倒退；不是仅复用pre-commit journal回退。原失败日志保留；只有修复后的必需检查真实通过才解除本例失败状态。
shared steps: env, protocol, publish, history-release-recovery
primary AC bindings: AC-081
review status: PASS

CASE-117
mode: blackbox
description: 旧推进命令和别名在无 run 与活动 engine run 上均统一未知命令拒绝
procedure: 使用 retired-workflow-surface。A 为已 bootstrap 但未 START 的独立 P/R；B 为实际 START 后未确认需求的独立 S0。每个环境先运行控制命令，随后按清单顺序对每个 name 实际运行 [FG,"workflow",name,"--root",P,"--run-id",R] 和 [FG,"workflow",name,"--help"]。每行前后采集 scoped 路径/文件字节与 Git HEAD；B 额外 READ(show)。保存 E/retired-command-results.jsonl，每行包含 environment、name、完整 argv、exit/stdout/stderr、与控制的错误归一比较、业务前后差异。执行 [FG,"workflow","--help"] 并提取实际公开子命令名，与最终随包路由清单逐名对账，已删除名字不得出现在可用子命令栏。
oracle: 两个环境中每个旧名字与 alias 都得到非零退出及与未知控制完全相同的未知命令错误，完整清单的实际结果无缺行。不能把参数错误、UNSUPPORTED_ENGINE_ENTRY、旧 run 版本错误或兼容 handler 业务拒绝当成通过。A 不创建任何 run/state/dispatch/prompt；B 的 revision、requirement confirmation、route/mode、有效执行和额度逐字段不变。没有额外 workflow 权威文件，Git HEAD 不变；--help 不广告已删除入口。此次证明删除后的公共面；无第二 writer 源码及注册表由结构验证另证。
shared steps: env, protocol, fixture, retired-workflow-surface
primary AC bindings: AC-001, AC-037
review status: PASS

CASE-118
mode: blackbox
description: 真实历史 legacy 终态在新版只提供最小原始诊断且输入不改写
procedure: 使用 legacy-terminal-input，先保存历史副本的字节、mode、mtime 与 P/.gates/tmp 与 P/.gates/results 的路径清单；实际运行 [FG,"workflow","diagnose","--root",P,"--run-id",R] 两次，将完整原始 JSON/文本分别保存 E/diagnose-1 与 E/diagnose-2。解析输出中公开合同的 raw-read/version 字段，记录它指向的实际 summary 路径、JSON 可读性与缺失/不支持 engine 版本的诊断，不调用旧 loader 或任何旧 workflow 命令。与 CASE-002 的当前 terminal/missing/corrupt 结果比较类别，保存比较表。 最终两种精确入口均执行：READ(diagnose) 的 --run-id 形式，以及 [FG,"workflow","diagnose","--root",P,"--path",P+"/.gates/results/stage-3-5c-qa-efficiency.json"]；机械比较 code="UNSUPPORTED_RUN_VERSION"、readOnly=true、path 为该实际绝对文件、raw 与原 JSON 结构深相等。--run-id 仅在无同名 engine 目录时按 tmp/state 优先、results 次序查找；另独立副本删除文件/写截断 JSON 均必须失败，不把失败包装为 raw 成功。
oracle: 输出对象键恰为code/path/readOnly/raw，code=UNSUPPORTED_RUN_VERSION、readOnly=true、path=实际读取历史副本的绝对路径，raw与原JSON深相等。没有补字段、续跑建议、迁移或写授权；缺文件/截断JSON明确非零失败。两次调用及两种入口后原字节/mode/mtime不变，没有新engine状态、receipt、授权；历史SEALED及全部原证据内容保留。
shared steps: env, protocol, fixture, legacy-terminal-input
primary AC bindings: AC-002
review status: PASS

CASE-119
mode: blackbox
description: 新版不能通过当前推进或查看入口把历史 legacy 终态转成当前 engine run
procedure: 每个命令各自新建 legacy-terminal-input 副本。对 drive、show、status、next 分别实际运行 [FG,"workflow",command,"--root",P,"--run-id",R]。第五份副本调用 [FG,"workflow","submit","--root",P,"--run-id",R]，stdin 精确输入 {"ID":"legacy-terminal-no-upgrade","Kind":"REGULAR","regular":{"operation":"REQUEST","expectedRevision":1,"request":{"action":"mode","mode":"regular"}}}。每份保存调用前后的历史 summary 字节、mode、mtime、路径清单及 Git HEAD，再调用当前 FG diagnose 复查原始输入仍在。此处 expectedRevision=1 仅用于构造语法完整的新协议请求，不把旧状态伪造为 revision 1。
oracle: 五个入口全部非零拒绝，明确当前engine运行不存在或版本/身份不支持，不能解码成当前 typed 状态、接纳事件、返回新版 Complete/成功 terminal replay 或启动恢复。拒绝后原 summary 字节/mode/mtime 与 Git HEAD 不变；不存在新 engine state、迁移 receipt、执行、轮次或授权账目。后续 diagnose 仍只见原始历史终态。
shared steps: env, protocol, fixture, legacy-terminal-input
primary AC bindings: AC-001, AC-002
review status: PASS

CASE-120
mode: blackbox
description: 初次模式选择拒绝 legacy，纠正为 regular 后正常继续且不生成兼容运行态
procedure: 新 P 按 START、DRIVE 与 protocol 的完整确认步骤到 S1；读取 revision 后提交 REQUEST 信封，其中 request 精确为 {"action":"mode","mode":"legacy"}，其余外层字段按 protocol 现值生成。保存请求、拒绝及随后 READ(show/next)；确认 mode/route 仍未绑定。然后使用全新 event ID 和刚读取 revision，通过 REQUEST mode=regular 及返回 Ask 的合法 DECIDE 完成普通模式选择，再 READ(show/next)。
oracle: legacy 不属于可用 mode，错误明确定位 mode/legacy 且不会生成可确认的 legacy 选择、兼容 run、第二状态文件或任何后续业务执行；mode/route 保持未绑定。拒绝可以留下审计 receipt，但不得新增业务授权或重置次数。纠正后的 regular 选择可成功，runtime 始终 engine、原需求确认仍有效，产品审按当前定义签发。
shared steps: env, protocol, fixture
primary AC bindings: AC-001, AC-008
review status: PASS

CASE-083
mode: blackbox
description: REQ-3.5A-001..008 and SOL-3.5A-001..004 legal end-to-end coverage, including multi-source/multi-point mapping, both BLACKBOX and WHITEBOX selected kinds, deterministic projection, and execution reconciliation.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n '
  def sb: {runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"};
  def req: ["001","002","003","004","005","006","007","008"] | map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"});
  def sol: ["001","002","003","004"] | map({sourceId:("SOL-3.5A-"+.),category:"SOLUTION_CONSTRAINT",applicability:"QA"});
  {requiredSources:{binding:sb,sources:(req+sol)},selectedKinds:["BLACKBOX","WHITEBOX"],candidate:{identity:"cand-v1",digest:("sha256:"+([range(0;64)]|map("c")|join("")))},manifests:[
    {binding:(sb+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008","SOL-3.5A-001","SOL-3.5A-002","SOL-3.5A-003","SOL-3.5A-004"],points:[
      {pointId:"BB-P-MAP",observableBehavior:"validate accepts a complete source-point-case map",oracle:"JSON success with three sha256 digests"},
      {pointId:"BB-P-REVIEW",observableBehavior:"review projection requires every item decision",oracle:"approved whitelist has one entry per edge"},
      {pointId:"BB-P-EXEC",observableBehavior:"execution reconciles every approved edge",oracle:"valid true and candidate cand-v1"}],cases:[
      {caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"formal-gates binary and JSON contract",steps:"pipe contract JSON on stdin",oracle:"ok=true and valid=true"},
      {caseId:"BB-C-ORDER",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"contract and PASS reviews",steps:"pipe envelope JSON on stdin",oracle:"entries sorted by kind/source/point/case"},
      {caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"contract and review envelope",steps:"pipe envelope JSON on stdin",oracle:"invalid review returns stable code/path"},
      {caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"contract, whitelist and execution report",steps:"pipe reconciliation JSON on stdin",oracle:"valid=true only for complete EXECUTED_PASS records"}],edges:[
      {reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-REVIEW",caseId:"BB-C-ORDER"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-001",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-ORDER"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-003",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]},
    {binding:(sb+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structural branch is exercised",oracle:"unique test reference is accepted"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"test executes and passes"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}
  ]}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"
"$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"
"$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"
jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"
jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"
"$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.requiredSources.sources |= reverse | .manifests |= (map(.sources |= reverse | .points |= reverse | .cases |= reverse | .edges |= reverse) | reverse)' "$ROOT/contract.json" > "$ROOT/contract-permuted.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract-permuted.json" > "$ROOT/project-permuted.json"
"$BIN" coverage validate < "$ROOT/contract-permuted.json" > "$ROOT/evidence/validate-permuted.json"; "$BIN" coverage project-whitelist < "$ROOT/project-permuted.json" > "$ROOT/evidence/project-permuted.json"
jq --slurpfile v "$ROOT/evidence/validate-permuted.json" --slurpfile w "$ROOT/evidence/project-permuted.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract-permuted.json" > "$ROOT/reconcile-permuted.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-permuted.json" > "$ROOT/evidence/reconcile-permuted.json"
test "$(jq -c '.result.digests' "$ROOT/evidence/validate.json")" = "$(jq -c '.result.digests' "$ROOT/evidence/validate-permuted.json")"; test "$(jq -c '.result.digest' "$ROOT/evidence/project.json")" = "$(jq -c '.result.digest' "$ROOT/evidence/project-permuted.json")"
oracle: Baseline and permuted validate, project-whitelist, and reconcile-execution calls exit 0. Each validate response is exactly an {ok:true,result:{valid:true,digests:{requiredSourcesDigest,manifestDigest,mapDigest}}} envelope with all three digests matching sha256:[0-9a-f]{64}; whitelist entries are sorted and include both BLACKBOX and WHITEBOX edges with identical digest before/after permutation; reconciliation is valid=true and accounts for every approved edge and point. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-088
mode: blackbox
description: A QA source omitted from every selected manifest is rejected rather than silently treated as covered.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def sb:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def src:["001","002","003","004","005","006","007","008"]|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"}); {requiredSources:{binding:sb,sources:src},selectedKinds:["BLACKBOX"],manifests:[{binding:(sb+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"
jq '.manifests[0].sources|=map(select(.!="REQ-3.5A-002"))|.manifests[0].edges|=map(select(.sourceId!="REQ-3.5A-002"))' "$ROOT/contract.json" > "$ROOT/c021.json"; jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/c021.json" > "$ROOT/p021.json"
"$BIN" coverage validate < "$ROOT/c021.json" > "$ROOT/evidence/c021-validate.json"; "$BIN" coverage project-whitelist < "$ROOT/p021.json" > "$ROOT/evidence/c021-project.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:($wl.entries|map(.caseId)|unique|sort),actualCaseIds:($wl.entries|map(.caseId)|unique|sort)},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
oracle: The mutated validate and project-whitelist calls both exit non-zero with one JSON error whose code is MISSING_COVERAGE, whose path is under manifests, and whose message names REQ-3.5A-002. Baseline three-command calls succeed and their reconcile fixture is retained; no whitelist or execution result from the mutated contract is accepted. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-106
mode: blackbox
description: REQ-3.5A-003 unknown source, point, and case IDs in a declared map edge are rejected without auto-creation or repair.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def i:["001","002","003","004","005","006","007","008"]; {requiredSources:{binding:b,sources:(i|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"}))},selectedKinds:["BLACKBOX","WHITEBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:(i|map("REQ-3.5A-"+.)),points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"},{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"},{pointId:"BB-P-EXEC",observableBehavior:"execution",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"},{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"error"},{caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"JSON",steps:"stdin",oracle:"valid"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]},{binding:(b+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structure",oracle:"valid"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"pass"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}],candidate:{identity:"cand-v1",digest:("sha256:"+("c"*64))}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].sources += ["SRC-UNKNOWN"]' "$ROOT/contract.json" > "$ROOT/c022-source.json"; jq '.manifests[0].edges[0].pointId="PT-UNKNOWN"' "$ROOT/contract.json" > "$ROOT/c022-point.json"; jq '.manifests[0].edges[0].caseId="CASE-UNKNOWN"' "$ROOT/contract.json" > "$ROOT/c022-case.json"
for f in source point case; do "$BIN" coverage validate < "$ROOT/c022-$f.json" > "$ROOT/evidence/c022-$f.json"; done
oracle: Baseline validate/project-whitelist/reconcile-execution all exit 0. The three mutated validate calls exit 1 with one stable JSON error each: source is UNKNOWN_ID naming SRC-UNKNOWN, point and case are ORPHAN_EDGE naming PT-UNKNOWN and CASE-UNKNOWN respectively; no unknown object is created, ignored, or repaired. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-121
mode: blackbox
description: REQ-3.5A-003 duplicate source, point, case, or source-point-case edge declarations are rejected while the untouched baseline remains valid.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def i:["001","002","003","004","005","006","007","008"]; {requiredSources:{binding:b,sources:(i|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"}))},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:(i|map("REQ-3.5A-"+.)),points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"},{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"},{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"error"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].sources += ["REQ-3.5A-001"]' "$ROOT/contract.json" > "$ROOT/c023-source.json"; jq '.manifests[0].points += [.manifests[0].points[0]]' "$ROOT/contract.json" > "$ROOT/c023-point.json"; jq '.manifests[0].cases += [.manifests[0].cases[0]]' "$ROOT/contract.json" > "$ROOT/c023-case.json"; jq '.manifests[0].edges += [.manifests[0].edges[0]]' "$ROOT/contract.json" > "$ROOT/c023-edge.json"
for f in source point case edge; do "$BIN" coverage validate < "$ROOT/c023-$f.json" > "$ROOT/evidence/c023-$f.json"; done
oracle: Baseline three-entry calls exit 0. Each duplicate fixture exits 1 with DUPLICATE_ID and a path identifying the duplicated source, point, case, or edge; sorting input does not silently deduplicate it and the original baseline bytes still pass. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-122
mode: blackbox
description: REQ-3.5A-003 a declared source, point, or case with no incident edge is reported as missing coverage.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def i:["001","002","003","004","005","006","007","008"]; {requiredSources:{binding:b,sources:(i|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"}))},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:(i|map("REQ-3.5A-"+.)),points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"},{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"},{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"error"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].edges|=map(select(.sourceId!="REQ-3.5A-008"))' "$ROOT/contract.json" > "$ROOT/c024-source.json"; jq '.manifests[0].points += [{pointId:"BB-P-ORPHAN",observableBehavior:"orphan",oracle:"reject"}]' "$ROOT/contract.json" > "$ROOT/c024-point.json"; jq '.manifests[0].cases += [{caseId:"BB-C-ORPHAN",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"reject"}]' "$ROOT/contract.json" > "$ROOT/c024-case.json"; for f in source point case; do "$BIN" coverage validate < "$ROOT/c024-$f.json" > "$ROOT/evidence/c024-$f.json"; done
oracle: Baseline validate/project-whitelist/reconcile-execution exit 0. Each orphan fixture exits 1 with MISSING_COVERAGE and a path/message naming REQ-3.5A-008, BB-P-ORPHAN, or BB-C-ORPHAN; other valid edges cannot mask the orphan. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-123
mode: blackbox
description: REQ-3.5A-001 run, child, requirement-revision, or solution-revision mismatch in the authoritative requiredSources binding invalidates an otherwise legal map.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def i:["001","002","003","004","005","006","007","008"]; {requiredSources:{binding:b,sources:(i|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"}))},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:(i|map("REQ-3.5A-"+.)),points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].binding.runId="run-other"' "$ROOT/contract.json" > "$ROOT/c025-run.json"; jq '.manifests[0].binding.childId="child-other"' "$ROOT/contract.json" > "$ROOT/c025-child.json"; jq '.manifests[0].binding.requirementRevision="req-other"' "$ROOT/contract.json" > "$ROOT/c025-req.json"; jq '.manifests[0].binding.solutionRevision="sol-other"' "$ROOT/contract.json" > "$ROOT/c025-sol.json"; for f in run child req sol; do "$BIN" coverage validate < "$ROOT/c025-$f.json" > "$ROOT/evidence/c025-$f.json"; done
oracle: Baseline three-command calls exit 0. Every changed manifest binding invocation exits 1 with INVALID_BINDING at exactly `manifests[0].binding` and reports that the manifest source binding does not match requiredSources; a valid ID graph cannot mask a run/child/revision mismatch. The mutated field and oracle path now refer to the same manifest binding object. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-124
mode: blackbox
description: REQ-3.5A-002 and SOL-3.5A-004 selected manifest review kind and route/topology scope are closed bindings; a mismatched kind or scope is rejected even when IDs remain unchanged.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["001","002","003","004","005","006","007","008"]|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX","WHITEBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]},{binding:(b+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structure",oracle:"valid"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"pass"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].binding.reviewKind="MERGE"' "$ROOT/contract.json" > "$ROOT/c026-kind.json"; jq '.manifests[0].binding.routeScope="route-other"' "$ROOT/contract.json" > "$ROOT/c026-route.json"; jq '.manifests[0].binding.topologyScope="fanout-other"' "$ROOT/contract.json" > "$ROOT/c026-topology.json"; jq '.manifests[1].binding.reviewKind="MERGE"' "$ROOT/contract.json" > "$ROOT/c026-white-kind.json"; for f in kind white-kind; do "$BIN" coverage validate < "$ROOT/c026-$f.json" > "$ROOT/evidence/c026-$f.json"; done
for f in route topology; do jq --arg f "$f" --slurpfile r "$ROOT/project.json" 'if $f=="route" then .manifests[0].binding.routeScope="route-other" else .manifests[0].binding.topologyScope="fanout-other" end | {contract:.,reviews:$r[0].reviews}' "$ROOT/contract.json" > "$ROOT/p026-$f.json"; "$BIN" coverage project-whitelist < "$ROOT/p026-$f.json" > "$ROOT/evidence/c026-$f.json"; done
oracle: Baseline validate/project-whitelist/reconcile-execution exit 0. Kind mutations exit 1 INVALID_BINDING at the manifest binding; route and topology mutations exit 1 INVALID_BINDING at `reviews[0].binding` because baseline reviews are stale. No old map/whitelist/execution is accepted solely because source/point/case IDs are unchanged. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-125
mode: blackbox
description: REQ-3.5A-006 source classified NON_QA cannot remain in a QA manifest or be projected to the QA whitelist.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["001","002","003","004","005","006","007","008"]|map({sourceId:("REQ-3.5A-"+.),category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008"],points:[{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"reject"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.requiredSources.sources[]|=if .sourceId=="REQ-3.5A-006" then .applicability="NON_QA" else . end' "$ROOT/contract.json" > "$ROOT/c027.json"; jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/c027.json" > "$ROOT/p027.json"; "$BIN" coverage validate < "$ROOT/c027.json" > "$ROOT/evidence/c027-validate.json"; "$BIN" coverage project-whitelist < "$ROOT/p027.json" > "$ROOT/evidence/c027-project.json"
oracle: Baseline calls exit 0. Both mutated commands exit 1 with INVALID_SOURCE at the non-QA source in the QA manifest; no whitelist or execution PASS is emitted for REQ-3.5A-006. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-126
mode: blackbox
description: REQ-3.5A-003 source present in two selected kinds must pass both manifest branches; deleting either manifest is a coverage failure.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX","WHITEBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]},{binding:(b+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structure",oracle:"valid"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"pass"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
"$BIN" coverage validate < <(jq '.manifests|=map(select(.binding.reviewKind!="WHITEBOX"))' "$ROOT/contract.json") > "$ROOT/evidence/c028-no-whitebox.json"; "$BIN" coverage validate < <(jq '.manifests|=map(select(.binding.reviewKind!="BLACKBOX"))' "$ROOT/contract.json") > "$ROOT/evidence/c028-no-blackbox.json"
oracle: Baseline three-entry calls exit 0 and the whitelist contains both kind branches, including REQ-3.5A-003 twice (once per kind). Removing either manifest yields non-zero MISSING_COVERAGE; BLACKBOX PASS cannot substitute for the missing WHITEBOX branch or vice versa. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-127
mode: blackbox
description: REQ-3.5A-003 WHITEBOX sibling branch with a failed or missing case decision cannot be suppressed by a passing BLACKBOX branch.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-003"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX","WHITEBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-003"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]},{binding:(b+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structure",oracle:"valid"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"pass"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.reviews[1].caseDecisions[0].status="FAIL"' "$ROOT/project.json" > "$ROOT/p029-fail.json"; jq '.reviews[1].caseDecisions=[]' "$ROOT/project.json" > "$ROOT/p029-missing.json"; "$BIN" coverage project-whitelist < "$ROOT/p029-fail.json" > "$ROOT/evidence/c029-fail.json"; "$BIN" coverage project-whitelist < "$ROOT/p029-missing.json" > "$ROOT/evidence/c029-missing.json"
oracle: Baseline calls exit 0. The FAIL fixture exits REVIEW_NOT_PASS naming WHITEBOX/WB-C-STRUCT; the missing fixture exits INCOMPLETE_REVIEW naming the missing case decision. BLACKBOX PASS never suppresses either failure. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-128
mode: blackbox
description: SOL-3.5A-004 case execution shape is closed by review kind: BLACKBOX requires public-entry fields, WHITEBOX requires only a unique test reference, and MERGE accepts its own public-entry shape.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-003"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX","WHITEBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-003"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]},{binding:(b+{reviewKind:"WHITEBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-003"],points:[{pointId:"WB-P-STRUCT",observableBehavior:"structure",oracle:"valid"}],cases:[{caseId:"WB-C-STRUCT",mode:"WHITEBOX",testRef:"qa/structure_test.go::TestCoverageStructure",oracle:"pass"}],edges:[{reviewKind:"WHITEBOX",sourceId:"REQ-3.5A-003",pointId:"WB-P-STRUCT",caseId:"WB-C-STRUCT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.manifests[0].cases[0].mode="WHITEBOX"' "$ROOT/contract.json" > "$ROOT/c030-bb-mode.json"; jq '.manifests[1].cases[0].publicEntry="formal-gates coverage validate"' "$ROOT/contract.json" > "$ROOT/c030-wb-entry.json"; jq '.manifests[0].cases[0].mode="NOT_A_MODE"' "$ROOT/contract.json" > "$ROOT/c030-unknown-mode.json"; for f in bb-mode wb-entry unknown-mode; do "$BIN" coverage validate < "$ROOT/c030-$f.json" > "$ROOT/evidence/c030-$f.json"; done
jq '.selectedKinds=["MERGE"]|.manifests |= [.[0]|.binding.reviewKind="MERGE"|.edges|=map(.reviewKind="MERGE")|.cases|=map(.mode="MERGE") ]' "$ROOT/contract.json" > "$ROOT/c030-merge.json"; jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/c030-merge.json" > "$ROOT/p030-merge.json"; "$BIN" coverage validate < "$ROOT/c030-merge.json" > "$ROOT/evidence/c030-merge-validate.json"; "$BIN" coverage project-whitelist < "$ROOT/p030-merge.json" > "$ROOT/evidence/c030-merge-project.json"; jq '.result' "$ROOT/evidence/c030-merge-project.json" > "$ROOT/c030-merge-whitelist.json"
oracle: Baseline three-entry calls exit 0. BLACKBOX mode changed to WHITEBOX, WHITEBOX given publicEntry, and unknown mode each exit INVALID_CASE_BINDING naming the case path. The explicit MERGE contract's validate and project-whitelist calls exit 0; its whitelist entries all carry reviewKind MERGE and no WHITEBOX-only testRef is accepted for MERGE. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-129
mode: blackbox
description: REQ-3.5A-004 collection-level PASS without per-source, per-point, and per-case decisions is incomplete.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002"],points:[{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"reject"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/p031.json"; "$BIN" coverage project-whitelist < "$ROOT/p031.json" > "$ROOT/evidence/c031-a.json"; "$BIN" coverage project-whitelist < "$ROOT/p031.json" > "$ROOT/evidence/c031-b.json"
oracle: Baseline calls exit 0. Both collection-PASS-only invocations exit 1 with INCOMPLETE_REVIEW naming the missing source/point/case decisions; no whitelist digest or entries are emitted as success. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-130
mode: blackbox
description: REQ-3.5A-004 review decisions must be complete, unique, known, and PASS for every source, point, and case.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002"],points:[{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"reject"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.reviews[0].caseDecisions=[]' "$ROOT/project.json" > "$ROOT/p032-missing.json"; jq '.reviews[0].sourceDecisions[1].id=.reviews[0].sourceDecisions[0].id' "$ROOT/project.json" > "$ROOT/p032-duplicate.json"; jq '.reviews[0].caseDecisions[0].id="CASE-UNKNOWN"' "$ROOT/project.json" > "$ROOT/p032-unknown.json"; jq '.reviews[0].sourceDecisions[0].status="FAIL"' "$ROOT/project.json" > "$ROOT/p032-fail-source.json"; jq '.reviews[0].pointDecisions[0].status="PENDING"' "$ROOT/project.json" > "$ROOT/p032-pending-point.json"; jq '.reviews[0].caseDecisions[0].status="UNKNOWN"' "$ROOT/project.json" > "$ROOT/p032-unknown-case.json"; for f in missing duplicate unknown fail-source pending-point unknown-case; do "$BIN" coverage project-whitelist < "$ROOT/p032-$f.json" > "$ROOT/evidence/c032-$f.json"; done
oracle: Baseline calls exit 0. Missing decisions return INCOMPLETE_REVIEW, duplicates DUPLICATE_ID, unknown IDs UNKNOWN_ID, and FAIL/PENDING/UNKNOWN statuses REVIEW_NOT_PASS; every error path identifies the affected kind and source/point/case decision and no partial whitelist is accepted. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-131
mode: blackbox
description: SOL-3.5A-001 and SOL-3.5A-002 approved whitelist is a deterministic projection bound to the current contract and cannot be independently edited to bypass review or execution checks.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002"],points:[{pointId:"BB-P-MAP",observableBehavior:"map",oracle:"valid"}],cases:[{caseId:"BB-C-VALID",mode:"BLACKBOX",publicEntry:"formal-gates coverage validate",preconditions:"JSON",steps:"stdin",oracle:"ok"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-MAP",caseId:"BB-C-VALID"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-MAP",caseId:"BB-C-VALID"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
"$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project-repeat.json"; cmp -s "$ROOT/evidence/project.json" "$ROOT/evidence/project-repeat.json"; cp "$ROOT/whitelist.json" "$ROOT/tampered-whitelist.json"; jq '.entries[0].caseId="BB-C-TAMPER"' "$ROOT/tampered-whitelist.json" > "$ROOT/tampered.json"; jq --slurpfile w "$ROOT/tampered.json" '.whitelist=$w[0]' "$ROOT/reconcile.json" > "$ROOT/r033-tampered.json"; "$BIN" coverage reconcile-execution < "$ROOT/r033-tampered.json" > "$ROOT/evidence/c033-tampered.json"; jq '.reviews[0].caseDecisions[0].status="FAIL"' "$ROOT/project.json" > "$ROOT/p033-fail.json"; "$BIN" coverage project-whitelist < "$ROOT/p033-fail.json" > "$ROOT/evidence/c033-fail.json"
oracle: Baseline validate/project-whitelist/reconcile-execution exit 0. Repeated projections have byte-equivalent sorted entries and a sha256:64-hex digest. Reconciliation with only the saved whitelist entry changed exits DIGEST_MISMATCH at `whitelist.digest`; FAIL review projection exits REVIEW_NOT_PASS. No editable standalone whitelist bypasses projection. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-132
mode: blackbox
description: REQ-3.5A-006 source confirmed NON_QA is accepted only with authoritative applicability=NON_QA, a concrete reason, an alternative method, PASS status, and evidence; incomplete or unknown alternatives are rejected.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002","REQ-3.5A-003","REQ-3.5A-004","REQ-3.5A-005","REQ-3.5A-006","REQ-3.5A-007","REQ-3.5A-008"],points:[{pointId:"BB-P-REVIEW",observableBehavior:"review",oracle:"valid"}],cases:[{caseId:"BB-C-REJECT",mode:"BLACKBOX",publicEntry:"formal-gates coverage project-whitelist",preconditions:"JSON",steps:"stdin",oracle:"reject"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-003",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-004",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-006",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-007",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-008",pointId:"BB-P-REVIEW",caseId:"BB-C-REJECT"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.requiredSources.sources[]|=if .sourceId=="REQ-3.5A-006" then .applicability="NON_QA" else . end | .manifests[0].sources|=map(select(.!="REQ-3.5A-006")) | .manifests[0].edges|=map(select(.sourceId!="REQ-3.5A-006")) | .alternativeVerifications=[{sourceId:"REQ-3.5A-006",reason:"confirmed no applicable QA point",method:"documented alternative check",status:"PASS",evidence:("sha256:"+("a"*64))}]' "$ROOT/contract.json" > "$ROOT/c034-nonqa.json"; jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/c034-nonqa.json" > "$ROOT/p034.json"; "$BIN" coverage validate < "$ROOT/c034-nonqa.json" > "$ROOT/evidence/c034-validate.json"; "$BIN" coverage project-whitelist < "$ROOT/p034.json" > "$ROOT/evidence/c034-project.json"; jq '.result' "$ROOT/evidence/c034-project.json" > "$ROOT/c034-whitelist.json"; jq --slurpfile v "$ROOT/evidence/c034-validate.json" --slurpfile w "$ROOT/evidence/c034-project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/c034-nonqa.json" > "$ROOT/r034.json"; "$BIN" coverage reconcile-execution < "$ROOT/r034.json" > "$ROOT/evidence/c034-reconcile.json"
for field in reason method evidence; do jq --arg f "$field" 'if $f=="reason" then .alternativeVerifications[0].reason="" elif $f=="method" then .alternativeVerifications[0].method="" else .alternativeVerifications[0].evidence="" end' "$ROOT/c034-nonqa.json" > "$ROOT/c034-missing-$field.json"; "$BIN" coverage validate < "$ROOT/c034-missing-$field.json" > "$ROOT/evidence/c034-missing-$field.json"; done; for status in PENDING UNKNOWN FAIL AUTHORIZED_SKIP; do jq --arg s "$status" '.alternativeVerifications[0].status=$s' "$ROOT/c034-nonqa.json" > "$ROOT/c034-status-$status.json"; "$BIN" coverage validate < "$ROOT/c034-status-$status.json" > "$ROOT/evidence/c034-status-$status.json"; done; jq '.alternativeVerifications=[{sourceId:"SRC-UNKNOWN",reason:"r",method:"m",status:"PASS",evidence:("sha256:"+("a"*64))}]' "$ROOT/c034-nonqa.json" > "$ROOT/c034-unknown.json"; "$BIN" coverage validate < "$ROOT/c034-unknown.json" > "$ROOT/evidence/c034-unknown.json"; jq '.alternativeVerifications += [.alternativeVerifications[0]]' "$ROOT/c034-nonqa.json" > "$ROOT/c034-duplicate.json"; "$BIN" coverage validate < "$ROOT/c034-duplicate.json" > "$ROOT/evidence/c034-duplicate.json"
oracle: The authoritative NON_QA fixture (REQ-3.5A-006 removed from the BLACKBOX sources/edges and closed by reason/method/PASS/evidence) passes validate, project-whitelist, and reconcile-execution; whitelist alternativeVerifications contains that source. Missing reason/method/evidence, PENDING/UNKNOWN/FAIL/AUTHORIZED_SKIP status, unknown source, or duplicate verification each exits 1 with INVALID_ALTERNATIVE_VERIFICATION (duplicate is DUPLICATE_ID); explanation or skip alone never closes the source. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-133
mode: blackbox
description: REQ-3.5A-005 and SOL-3.5A-001 execution results are bound to the current requiredSources, manifest, map, whitelist digests and ValidationCandidate identity/digest.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002"],points:[{pointId:"BB-P-EXEC",observableBehavior:"execution",oracle:"valid"}],cases:[{caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"JSON",steps:"stdin",oracle:"valid"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]}],candidate:{identity:"cand-v1",digest:("sha256:"+("c"*64))}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
for field in requiredSourcesDigest manifestDigest mapDigest whitelistDigest; do jq --arg f "$field" --arg v "sha256:$(printf 'd%.0s' {1..64})" '.execution.binding[$f]=$v' "$ROOT/reconcile.json" > "$ROOT/r035-$field.json"; "$BIN" coverage reconcile-execution < "$ROOT/r035-$field.json" > "$ROOT/evidence/c035-$field.json"; done; jq '.execution.binding.candidate.identity="cand-old"' "$ROOT/reconcile.json" > "$ROOT/r035-candidate-id.json"; jq '.execution.binding.candidate.digest="sha256:old"' "$ROOT/reconcile.json" > "$ROOT/r035-candidate-digest.json"; "$BIN" coverage reconcile-execution < "$ROOT/r035-candidate-id.json" > "$ROOT/evidence/c035-candidate-id.json"; "$BIN" coverage reconcile-execution < "$ROOT/r035-candidate-digest.json" > "$ROOT/evidence/c035-candidate-digest.json"
oracle: Baseline three-entry calls exit 0. Each digest mutation exits DIGEST_MISMATCH at exactly `execution.binding`; candidate identity or digest mutation exits CANDIDATE_MISMATCH at `execution.binding.candidate`. The old execution report never passes for a changed contract or candidate. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-134
mode: blackbox
description: REQ-3.5A-005 and REQ-3.5A-008 FULL execution must exactly account for the approved expected case set and every record must be EXECUTED_PASS.
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; {requiredSources:{binding:b,sources:["REQ-3.5A-001","REQ-3.5A-002"]|map({sourceId:.,category:"PRODUCT_REQUIREMENT",applicability:"QA"})},selectedKinds:["BLACKBOX"],manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-001","REQ-3.5A-002"],points:[{pointId:"BB-P-EXEC",observableBehavior:"execution",oracle:"valid"}],cases:[{caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"JSON",steps:"stdin",oracle:"valid"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-001",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-002",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]}],candidate:{identity:"cand-v1"}}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"; "$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"; jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile.json" > "$ROOT/evidence/reconcile.json"
jq '.execution.records |= .[1:]' "$ROOT/reconcile.json" > "$ROOT/r036-missing-record.json"; jq '.execution.binding.expectedCaseIds += ["BB-C-EXTRA"] | .execution.binding.actualCaseIds += ["BB-C-EXTRA"]' "$ROOT/reconcile.json" > "$ROOT/r036-extra-case.json"; jq '.execution.binding.expectedCaseIds=[]' "$ROOT/reconcile.json" > "$ROOT/r036-missing-expected.json"; for s in EXECUTED_FAIL PENDING UNKNOWN AUTHORIZED_SKIP; do jq --arg s "$s" '.execution.records[0].result=$s' "$ROOT/reconcile.json" > "$ROOT/r036-$s.json"; done; "$BIN" coverage reconcile-execution < "$ROOT/r036-missing-record.json" > "$ROOT/evidence/c036-missing-record.json"; "$BIN" coverage reconcile-execution < "$ROOT/r036-extra-case.json" > "$ROOT/evidence/c036-extra-case.json"; "$BIN" coverage reconcile-execution < "$ROOT/r036-missing-expected.json" > "$ROOT/evidence/c036-missing-expected.json"; for s in EXECUTED_FAIL PENDING UNKNOWN AUTHORIZED_SKIP; do "$BIN" coverage reconcile-execution < "$ROOT/r036-$s.json" > "$ROOT/evidence/c036-$s.json"; done
oracle: The untouched FULL fixture exits 0. Removing a record or changing expected/actual case sets exits EXECUTION_SET_MISMATCH and reports the set difference. EXECUTED_FAIL, PENDING, UNKNOWN, and AUTHORIZED_SKIP records exit EXECUTION_NOT_PASS naming the record; FULL accepts neither inherited nor not-executed accounting. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-135
mode: blackbox
description: REQ-3.5A-005 与 SOL-3.5A-003 的 AFFECTED 对账必须逐条覆盖全部 approved source→point→case edge；当前 EXECUTED_PASS 或带不同候选及复用证据的 INHERITED EXECUTED_PASS 才能通过，NOT_EXECUTED 只能完整记账而不能冒充 PASS。
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def d($c):"sha256:"+($c*64); {requiredSources:{binding:b,sources:[{sourceId:"REQ-3.5A-005",category:"PRODUCT_REQUIREMENT",applicability:"QA"},{sourceId:"SOL-3.5A-003",category:"SOLUTION_CONSTRAINT",applicability:"QA"}]},selectedKinds:["BLACKBOX"],candidate:{identity:"cand-v1",digest:d("c")},manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-005","SOL-3.5A-003"],points:[{pointId:"BB-P-EXEC",observableBehavior:"reconcile current, inherited and unexecuted execution records",oracle:"only PASS records close every approved edge"}],cases:[{caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"built binary, validated contract, PASS review envelope and projected whitelist",steps:"pipe reconciliation JSON on stdin",oracle:"success only for complete execution accounting"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-003",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]}]}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"
"$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"
jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile-full.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-full.json" > "$ROOT/evidence/full.json"
jq '.execution.binding.scope="AFFECTED" | .execution.notExecuted=[] | .execution.records |= map(if .sourceId=="SOL-3.5A-003" then .provenance="INHERITED"|.inheritedFromCandidate="cand-v0"|.reuseEvidence=("sha256:"+("b"*64)) else . end)' "$ROOT/reconcile-full.json" > "$ROOT/reconcile-affected-valid.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-affected-valid.json" > "$ROOT/evidence/affected-valid.json"
jq '.execution.records |= map(if .sourceId=="REQ-3.5A-005" then .provenance="NOT_EXECUTED"|.result="EXECUTED_FAIL" else . end) | .execution.notExecuted=[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-not-executed.json"
jq '.execution.records |= map(select(.sourceId!="SOL-3.5A-003"))' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-missing-record.json"
jq '.execution.records |= map(if .sourceId=="SOL-3.5A-003" then .reuseEvidence="" else . end)' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-no-evidence.json"
jq '.execution.records |= map(if .sourceId=="SOL-3.5A-003" then .inheritedFromCandidate="cand-v1" else . end)' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-current-candidate.json"
jq '.execution.records |= map(if .sourceId=="REQ-3.5A-005" then .provenance="NOT_EXECUTED"|.result="EXECUTED_FAIL" else . end) | .execution.notExecuted=[]' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-omitted-not-executed.json"
jq '.execution.records |= map(if .sourceId=="REQ-3.5A-005" then .provenance="NOT_EXECUTED"|.result="EXECUTED_FAIL" else . end) | .execution.notExecuted=[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SRC-HIDDEN",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]' "$ROOT/reconcile-affected-valid.json" > "$ROOT/reconcile-hidden-not-executed.json"
for f in not-executed missing-record no-evidence current-candidate omitted-not-executed hidden-not-executed; do "$BIN" coverage reconcile-execution < "$ROOT/reconcile-$f.json" > "$ROOT/evidence/$f.json"; done
jq -e '.ok and .result.valid' "$ROOT/evidence/validate.json" "$ROOT/evidence/project.json" "$ROOT/evidence/full.json" "$ROOT/evidence/affected-valid.json" >/dev/null
jq -e '.code=="EXECUTION_NOT_PASS"' "$ROOT/evidence/not-executed.json" "$ROOT/evidence/no-evidence.json" "$ROOT/evidence/current-candidate.json" >/dev/null
jq -e '.code=="EXECUTION_SET_MISMATCH"' "$ROOT/evidence/missing-record.json" "$ROOT/evidence/omitted-not-executed.json" "$ROOT/evidence/hidden-not-executed.json" >/dev/null
oracle: Baseline validate/project-whitelist/FULL/AFFECTED-valid exits 0 with valid=true; a complete NOT_EXECUTED edge is accounted for but exits EXECUTION_NOT_PASS; missing record or mismatched notExecuted set exits EXECUTION_SET_MISMATCH; missing reuse evidence or current-candidate inheritance exits EXECUTION_NOT_PASS, and no failure is accepted as PASS. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-136
mode: blackbox
description: REQ-3.5A-005 与 SOL-3.5A-003 的 AUTHORIZED_SKIP 在 FULL 和合法 AFFECTED 对账中都不是 execution PASS；AFFECTED 的继承记录被拒绝时诊断保留 AUTHORIZED_SKIP 状态。
procedure: 在本例独立P创建evidence子目录；ROOT=P，BIN=本时点FG。以下jq仅构造独立纯计算输入，其run-35a/child-0/req-r4/cand-v1及测试引用均为fixture字符串，不代表当前正式run、candidate或白盒执行事实。不得执行历史构建/源码回退前导。所有产品调用逐条以[FG,"coverage",原文子命令] argv执行，stdin使用原文重定向指定JSON，stdout/stderr/exit逐条另存E；保留每次JSON输入。下列完整输入构造和操作顺序冻结，不修改schema或预期来适配失败：
jq -n 'def b:{runId:"run-35a",childId:"child-0",requirementRevision:"req-r4",solutionRevision:"sol-r4"}; def d($c):"sha256:"+($c*64); {requiredSources:{binding:b,sources:[{sourceId:"REQ-3.5A-005",category:"PRODUCT_REQUIREMENT",applicability:"QA"},{sourceId:"SOL-3.5A-003",category:"SOLUTION_CONSTRAINT",applicability:"QA"}]},selectedKinds:["BLACKBOX"],candidate:{identity:"cand-v1",digest:d("c")},manifests:[{binding:(b+{reviewKind:"BLACKBOX",routeScope:"regular",topologyScope:"master"}),sources:["REQ-3.5A-005","SOL-3.5A-003"],points:[{pointId:"BB-P-EXEC",observableBehavior:"reconcile execution without converting AUTHORIZED_SKIP",oracle:"skip is rejected for executed and inherited provenance"}],cases:[{caseId:"BB-C-EXEC",mode:"BLACKBOX",publicEntry:"formal-gates coverage reconcile-execution",preconditions:"built binary, validated contract, PASS review envelope and projected whitelist",steps:"pipe reconciliation JSON on stdin",oracle:"baseline succeeds and skip mutations fail"}],edges:[{reviewKind:"BLACKBOX",sourceId:"REQ-3.5A-005",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"},{reviewKind:"BLACKBOX",sourceId:"SOL-3.5A-003",pointId:"BB-P-EXEC",caseId:"BB-C-EXEC"}]}]}' > "$ROOT/contract.json"
jq '{contract:.,reviews:[.manifests[]|{binding:.binding,scope:"FULL",sourceDecisions:(.sources|map({id:.,status:"PASS"})),pointDecisions:(.points|map({id:.pointId,status:"PASS"})),caseDecisions:(.cases|map({id:.caseId,status:"PASS"})),setStatus:"PASS"}]}' "$ROOT/contract.json" > "$ROOT/project.json"
"$BIN" coverage validate < "$ROOT/contract.json" > "$ROOT/evidence/validate.json"; "$BIN" coverage project-whitelist < "$ROOT/project.json" > "$ROOT/evidence/project.json"; jq '.result' "$ROOT/evidence/project.json" > "$ROOT/whitelist.json"
jq --slurpfile v "$ROOT/evidence/validate.json" --slurpfile w "$ROOT/evidence/project.json" '. as $c|($v[0].result.digests) as $d|($w[0].result) as $wl|($wl.entries|map(.caseId)|unique|sort) as $ids|{contract:$c,whitelist:$wl,execution:{binding:{requiredSourcesDigest:$d.requiredSourcesDigest,manifestDigest:$d.manifestDigest,mapDigest:$d.mapDigest,whitelistDigest:$wl.digest,candidate:$c.candidate,scope:"FULL",expectedCaseIds:$ids,actualCaseIds:$ids},records:($wl.entries|map(.+{result:"EXECUTED_PASS",provenance:"EXECUTED"}))}}' "$ROOT/contract.json" > "$ROOT/reconcile-full.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-full.json" > "$ROOT/evidence/full-baseline.json"
jq '.execution.binding.scope="AFFECTED" | .execution.notExecuted=[] | .execution.records |= map(if .sourceId=="SOL-3.5A-003" then .provenance="INHERITED"|.inheritedFromCandidate="cand-v0"|.reuseEvidence=("sha256:"+("b"*64)) else . end)' "$ROOT/reconcile-full.json" > "$ROOT/reconcile-affected.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-affected.json" > "$ROOT/evidence/affected-baseline.json"
jq '.execution.records |= map(if .sourceId=="REQ-3.5A-005" then .result="AUTHORIZED_SKIP" else . end)' "$ROOT/reconcile-full.json" > "$ROOT/reconcile-full-skip.json"; jq '.execution.records |= map(if .sourceId=="SOL-3.5A-003" then .result="AUTHORIZED_SKIP" else . end)' "$ROOT/reconcile-affected.json" > "$ROOT/reconcile-affected-skip.json"
"$BIN" coverage reconcile-execution < "$ROOT/reconcile-full-skip.json" > "$ROOT/evidence/full-skip.json"; "$BIN" coverage reconcile-execution < "$ROOT/reconcile-affected-skip.json" > "$ROOT/evidence/affected-skip.json"
jq -e '.ok and .result.valid' "$ROOT/evidence/validate.json" "$ROOT/evidence/project.json" "$ROOT/evidence/full-baseline.json" "$ROOT/evidence/affected-baseline.json" >/dev/null
jq -e '.code=="EXECUTION_NOT_PASS" and (.message|contains("AUTHORIZED_SKIP")|not)' "$ROOT/evidence/full-skip.json" >/dev/null
jq -e '.code=="EXECUTION_NOT_PASS" and (.message|contains("AUTHORIZED_SKIP"))' "$ROOT/evidence/affected-skip.json" >/dev/null
oracle: validate, project-whitelist, FULL baseline and legal AFFECTED baseline exit 0 with valid=true; FULL and AFFECTED AUTHORIZED_SKIP mutations exit EXECUTION_NOT_PASS, and only the AFFECTED diagnostic contains AUTHORIZED_SKIP; neither returns valid=true. 所有调用前后，P之外没有写入；纯合同EXECUTED_PASS字符串是待校验输入，不是本阶段QA实跑PASS。
shared steps: env
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-137
mode: blackbox
description: 空任务的公开guard计算为零
procedure: 在干净P保存目录清单；复制PKG/references/guard-preview-basis.json为E/input.json；将definition.externalSteps=[]、externalTasks=[]，其余字段保持。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: stdout JSON的distinctExternalTaskKeys/requestAllowance/retryAllowance/totalAllowance四值全部严格为0，exit=0。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-138
mode: blackbox
description: 完全重复TaskKey不重复计量
procedure: 在干净P保存目录清单；复制示例为E/input.json，把externalTasks[1]深复制追加到同数组末尾。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: exit=0；distinctExternalTaskKeys=3、requestAllowance=3、retryAllowance=4、totalAllowance=7；规范basis和budgetBasisDigest与原示例独立调用的完整结果深相等。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-139
mode: blackbox
description: 不存在输入文件可定位且不写run
procedure: 在干净P保存目录清单；E/input.json不创建，确保该路径不存在。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: exit非零；stderr含E/input.json实际路径；P/.gates路径列表与原字节均未变。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-140
mode: blackbox
description: 截断JSON不能被报告成功
procedure: 在干净P保存目录清单；写E/input.json字节为{。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: exit非零；stderr包含实际文件路径及JSON解析诊断。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-141
mode: blackbox
description: 未知JSON字段和多值不被静默接受
procedure: 在干净P保存目录清单；每个独立P/E一个输入：A为原示例对象新增typo=true；B为原示例JSON后追加换行及{}。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: 两者均exit非零，stderr包含实际文件路径；A定位typo/unknown field，B定位多值或尾部JSON。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-142
mode: blackbox
description: 未确认身份不能作为合法basis
procedure: 在干净P保存目录清单；复制示例，将requirement.confirmedRevision改为req-other。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: exit非零；stderr含实际文件路径和requirement/confirmedRevision相关字段诊断。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-143
mode: blackbox
description: 非法retry输入可定位
procedure: 在干净P保存目录清单；每行新P/E从原示例复制，只将externalTasks[0].maxAttempts分别设0、-1、1.5、字符串2。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: 每行exit非零；stderr含input.json路径及maxAttempts或JSON类型诊断；不能静默使用默认2。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-144
mode: blackbox
description: 同TaskKey冲突retry不能折叠成功
procedure: 在干净P保存目录清单；复制示例，把externalTasks[1]深复制追加，并只把该副本maxAttempts设3。 先用[FG,"workflow","guard-preview","--input",PKG+"/references/guard-preview-basis.json"]作合法控制，再实际调用[FG,"workflow","guard-preview","--input",E+"/input.json"]，分别保留输入文件、stdout/stderr/exit；再次列P/.gates文件路径与原字节。修复负例为原完整示例再调用同argv。
oracle: exit非零；stderr含实际文件路径及TaskKey冲突/duplicate诊断；不能得到多算或去重后成功结果。 全过程不START、不创建或修改run；修复后控制四数严格3/3/4/7且exit=0。
shared steps: env, protocol
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-145
mode: blackbox
description: 历史结构预检公开接入：缺少需求点章节
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。


### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例缺少需求点章节规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-146
mode: blackbox
description: 历史结构预检公开接入：重复需求点章节
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

## 需求点

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例重复需求点章节规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-147
mode: blackbox
description: 历史结构预检公开接入：空需求点章节
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# QA

## 需求点

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例空需求点章节规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-148
mode: blackbox
description: 历史结构预检公开接入：不合法REQ标题
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-1：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例不合法REQ标题规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-149
mode: blackbox
description: 历史结构预检公开接入：空REQ标题
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例空REQ标题规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-150
mode: blackbox
description: 历史结构预检公开接入：重复REQ ID
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-002：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例重复REQ ID规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-151
mode: blackbox
description: 历史结构预检公开接入：重复AC ID
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

- AC-001：重复编号的第二项。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例重复AC ID规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-152
mode: blackbox
description: 历史结构预检公开接入：缺少来源字段
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。


修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例缺少来源字段规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-153
mode: blackbox
description: 历史结构预检公开接入：字段顺序错误
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 验收条件
对Alice输出Hello Alice。

#### 要求
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例字段顺序错误规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-154
mode: blackbox
description: 历史结构预检公开接入：要求内容为空
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求


#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例要求内容为空规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-155
mode: blackbox
description: 历史结构预检公开接入：验收条件直接列表没有AC编号
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- 输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例验收条件直接列表没有AC编号规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-156
mode: blackbox
description: 历史结构预检公开接入：多余直接四级字段
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

#### 其他
不允许的新字段。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例多余直接四级字段规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-157
mode: blackbox
description: 历史结构预检公开接入：同REQ字段重复
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

#### 来源
重复。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例同REQ字段重复规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-158
mode: blackbox
description: 历史结构预检公开接入：REQ位于需求点章节之外
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 其他

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例REQ位于需求点章节之外规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-159
mode: blackbox
description: 历史结构预检公开接入：AC位于验收字段之外
procedure: 在独立P按fixture初始化普通Git仓库，本例requirements.md完全替换为下述精确UTF-8文本，不在正式run写样本；git add/commit后调用START，再DRIVE并READ(show/next)，尝试当前availableActions中存在的confirm-requirement REQUEST；仅使用实际ask，不自行造token。保留全部诊断和phase/confirmation/route/executions。错误样本的恢复以本说明 validRequirement 全文为 V，按 bound-requirement-recovery 在同一 P/R 经 requirement-change/apply-requirement、当前全文 revision 确认和预检恢复；保留前面的原始拒绝。
错误输入全文：
# 隔离QA需求

## 背景
普通自然语言背景。
- AC-002：错误位置。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

修复后的validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。
oracle: 错误样本不能得到已确认需求或已绑定mode/route，不能签发产品/技术/开发；结构诊断包含requirements.md、可定位行号及本例AC位于验收字段之外规则，能识别REQ时含REQ-001；修复后全文/精确revision确认成立且可选择模式。不能把仅本地解析测试输出代替实际START/DRIVE诊断。
shared steps: env, protocol, fixture, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-160
mode: blackbox
description: 历史结构预检公开接入：单个合法REQ不被最低数量限制
procedure: 独立P按fixture初始化并采用以下validRequirement及唯一变更：逐字写validRequirement。
validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

实际START→DRIVE→REQUEST confirm-requirement→复制实际Ask绑定DECIDE确认→READ(show/next)；保存全文、projection及所有events/receipts。
oracle: 预检通过并且仍等待用户DECIDE，不自动确认；DECIDE后投影中的REQ/AC集合分别精确等于["REQ-001", "AC-001"]；不得重编号、包含围栏示例或丢项。PARSE-MANY只证明此有限128样本，任意有限集合的无硬编码上限由白盒结构验证，不以该样本冒充无限穷举。
shared steps: env, protocol, fixture
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-161
mode: blackbox
description: 历史结构预检公开接入：合法不连续编号保持原编号
procedure: 独立P按fixture初始化并采用以下validRequirement及唯一变更：把validRequirement的REQ-001改REQ-1000、AC-001改AC-9000。
validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

实际START→DRIVE→REQUEST confirm-requirement→复制实际Ask绑定DECIDE确认→READ(show/next)；保存全文、projection及所有events/receipts。
oracle: 预检通过并且仍等待用户DECIDE，不自动确认；DECIDE后投影中的REQ/AC集合分别精确等于["REQ-1000", "AC-9000"]；不得重编号、包含围栏示例或丢项。PARSE-MANY只证明此有限128样本，任意有限集合的无硬编码上限由白盒结构验证，不以该样本冒充无限穷举。
shared steps: env, protocol, fixture
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-162
mode: blackbox
description: 历史结构预检公开接入：代码围栏示例不进入结构投影
procedure: 独立P按fixture初始化并采用以下validRequirement及唯一变更：在validRequirement的背景之后、需求点之前插入完整三反引号markdown围栏，内容为### REQ-001：示例及- AC-001：示例，两行之间换行；围栏闭合后保留原正文。
validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

实际START→DRIVE→REQUEST confirm-requirement→复制实际Ask绑定DECIDE确认→READ(show/next)；保存全文、projection及所有events/receipts。
oracle: 预检通过并且仍等待用户DECIDE，不自动确认；DECIDE后投影中的REQ/AC集合分别精确等于["REQ-001", "AC-001"]；不得重编号、包含围栏示例或丢项。PARSE-MANY只证明此有限128样本，任意有限集合的无硬编码上限由白盒结构验证，不以该样本冒充无限穷举。
shared steps: env, protocol, fixture
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-163
mode: blackbox
description: 历史结构预检公开接入：较多合法REQ没有固定小上限
procedure: 独立P按fixture初始化并采用以下validRequirement及唯一变更：从validRequirement保留需求点前导，把其REQ完整区块复制128次；第i次REQ和AC均用三位数i（001至128）；来源/要求文本保留。
validRequirement全文：
# 隔离QA需求

## 背景
普通自然语言背景。

## 需求点

### REQ-001：问候

#### 要求
对Alice输出Hello Alice。

#### 验收条件
- AC-001：输入Alice，stdout为Hello Alice加换行且exit=0。

#### 来源
本例独立隔离QA输入。

实际START→DRIVE→REQUEST confirm-requirement→复制实际Ask绑定DECIDE确认→READ(show/next)；保存全文、projection及所有events/receipts。
oracle: 预检通过并且仍等待用户DECIDE，不自动确认；DECIDE后投影中的REQ/AC集合分别精确等于["REQ-001", "REQ-002", "REQ-003", "REQ-004", "REQ-005", "REQ-006", "REQ-007", "REQ-008", "REQ-009", "REQ-010", "REQ-011", "REQ-012", "REQ-013", "REQ-014", "REQ-015", "REQ-016", "REQ-017", "REQ-018", "REQ-019", "REQ-020", "REQ-021", "REQ-022", "REQ-023", "REQ-024", "REQ-025", "REQ-026", "REQ-027", "REQ-028", "REQ-029", "REQ-030", "REQ-031", "REQ-032", "REQ-033", "REQ-034", "REQ-035", "REQ-036", "REQ-037", "REQ-038", "REQ-039", "REQ-040", "REQ-041", "REQ-042", "REQ-043", "REQ-044", "REQ-045", "REQ-046", "REQ-047", "REQ-048", "REQ-049", "REQ-050", "REQ-051", "REQ-052", "REQ-053", "REQ-054", "REQ-055", "REQ-056", "REQ-057", "REQ-058", "REQ-059", "REQ-060", "REQ-061", "REQ-062", "REQ-063", "REQ-064", "REQ-065", "REQ-066", "REQ-067", "REQ-068", "REQ-069", "REQ-070", "REQ-071", "REQ-072", "REQ-073", "REQ-074", "REQ-075", "REQ-076", "REQ-077", "REQ-078", "REQ-079", "REQ-080", "REQ-081", "REQ-082", "REQ-083", "REQ-084", "REQ-085", "REQ-086", "REQ-087", "REQ-088", "REQ-089", "REQ-090", "REQ-091", "REQ-092", "REQ-093", "REQ-094", "REQ-095", "REQ-096", "REQ-097", "REQ-098", "REQ-099", "REQ-100", "REQ-101", "REQ-102", "REQ-103", "REQ-104", "REQ-105", "REQ-106", "REQ-107", "REQ-108", "REQ-109", "REQ-110", "REQ-111", "REQ-112", "REQ-113", "REQ-114", "REQ-115", "REQ-116", "REQ-117", "REQ-118", "REQ-119", "REQ-120", "REQ-121", "REQ-122", "REQ-123", "REQ-124", "REQ-125", "REQ-126", "REQ-127", "REQ-128", "AC-001", "AC-002", "AC-003", "AC-004", "AC-005", "AC-006", "AC-007", "AC-008", "AC-009", "AC-010", "AC-011", "AC-012", "AC-013", "AC-014", "AC-015", "AC-016", "AC-017", "AC-018", "AC-019", "AC-020", "AC-021", "AC-022", "AC-023", "AC-024", "AC-025", "AC-026", "AC-027", "AC-028", "AC-029", "AC-030", "AC-031", "AC-032", "AC-033", "AC-034", "AC-035", "AC-036", "AC-037", "AC-038", "AC-039", "AC-040", "AC-041", "AC-042", "AC-043", "AC-044", "AC-045", "AC-046", "AC-047", "AC-048", "AC-049", "AC-050", "AC-051", "AC-052", "AC-053", "AC-054", "AC-055", "AC-056", "AC-057", "AC-058", "AC-059", "AC-060", "AC-061", "AC-062", "AC-063", "AC-064", "AC-065", "AC-066", "AC-067", "AC-068", "AC-069", "AC-070", "AC-071", "AC-072", "AC-073", "AC-074", "AC-075", "AC-076", "AC-077", "AC-078", "AC-079", "AC-080", "AC-081", "AC-082", "AC-083", "AC-084", "AC-085", "AC-086", "AC-087", "AC-088", "AC-089", "AC-090", "AC-091", "AC-092", "AC-093", "AC-094", "AC-095", "AC-096", "AC-097", "AC-098", "AC-099", "AC-100", "AC-101", "AC-102", "AC-103", "AC-104", "AC-105", "AC-106", "AC-107", "AC-108", "AC-109", "AC-110", "AC-111", "AC-112", "AC-113", "AC-114", "AC-115", "AC-116", "AC-117", "AC-118", "AC-119", "AC-120", "AC-121", "AC-122", "AC-123", "AC-124", "AC-125", "AC-126", "AC-127", "AC-128"]；不得重编号、包含围栏示例或丢项。PARSE-MANY只证明此有限128样本，任意有限集合的无硬编码上限由白盒结构验证，不以该样本冒充无限穷举。
shared steps: env, protocol, fixture
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-164
mode: blackbox
description: 相同候选重装保留无关规则且不重复安装器区块
procedure: 仅在publish隔离项目目标：安装前在PROJECT/AGENTS.md写UTF-8文本 QA-UNRELATED-RULE 加换行；按publish argv完整安装CAND。保存安装回执给出的hook/config/managed-rule实际路径与完整字节；再用完全相同[STABLE,"install","--source",CAND,"--host","codex","--scope","project","--project",PROJECT,"--binary-target",STABLE,"--force"]重装一次。分别保存registry/install receipt、规则全文、hook/config字节；从最终receipt指向的installed binary运行[FG,"package","validate","--root",PKG]。
oracle: 两次安装及最终validate均exit=0；规则文件保留独立一行QA-UNRELATED-RULE；formal-gates:host-instructions:start/end各恰好一个且其间内容与当前PKG/SKILL.md的唯一标记区块一致。第二次hook和规则内容完全不变且回执对应action为SKIPPED_UNCHANGED；runtime、registry和回执身份一致。不使用grep次数证明整个安装事务，只将明确marker计数与完整字节比较结合。
shared steps: env, protocol, fixture, publish, history-release-recovery
primary AC bindings: AC-057, AC-079, AC-080
review status: PASS

CASE-165
mode: blackbox
description: --skip-hooks使hook与受理规则逐字节保持并仍完成runtime事务
procedure: 仅在publish隔离目标先从旧稳定artifact完成原生安装，保存其实际hook/config/managed-rule全部字节和路径；通过[STABLE,"install","--source",CAND,"--host","codex","--scope","project","--project",PROJECT,"--binary-target",STABLE,"--force","--skip-hooks"]安装当前已验证候选。记录真实owner、旧/新包identity、journal/install receipt；从新receipt的installed binary实际package validate，再读取全部原hook/config/managed-rule字节。
oracle: 安装与新installed package validate均exit=0；所有既有hook/config和受理规则字节与旧安装保存值完全相同；runtime/package与registry实际已绑定CAND而非假装跳过整个安装。skip-hooks不跳过registry/journal验证，不改变真实全局安装。
shared steps: env, protocol, fixture, publish, history-release-recovery
primary AC bindings: AC-057, AC-079, AC-080
review status: PASS

CASE-166
mode: blackbox
description: 活动run阻止卸载，正常abort后卸载仅清理所属安装
procedure: 在publish独立Codex项目目标安装CAND，PROJECT/AGENTS.md含独立一行QA-UNRELATED-RULE；将FG设为该实际STABLE并令P=PROJECT，按START/DRIVE至未确认S0，保存run与runtime/hook/config/规则/registry字节。调用[STABLE,"uninstall","--host","codex","--scope","project","--project",PROJECT]；保存拒绝结果并READ(show)。然后按protocol实际REQUEST abort、对应Ask的DECIDE confirm，再DRIVE到终态自动cleanup；保存终态summary。再次执行相同uninstall argv，保存receipt、被登记target与hook/规则实际路径。所有卸载只在此独立安装目标，不对真实全局执行。
oracle: 活动时卸载exit非零且明确活动run阻止；原run的revision/有效执行和安装组件未被删除或改变。typed abort/cleanup实际结束后卸载exit=0；选定Codex runtime移除、registry该record disabled；安装器formal-gates规则区块移除而QA-UNRELATED-RULE原字节保留，其他未涉及路径未删除；run终态summary/历史证据保留。
shared steps: env, protocol, fixture, publish, history-release-recovery
primary AC bindings: AC-057, AC-079, AC-080
review status: PASS

CASE-167
mode: blackbox
description: package baseline从实际安装入口生成可复核身份/路径回执
procedure: 使用env/publish中已验证的不可变源码CAND、安装根PKG、安装入口FG和精确源码Git identity Q，三者对应同一候选；installedTarget为真实安装回执指定目标，不自行重算目标路径。新建独立P/E，不START。实际调用[FG,"package","baseline","--root",PKG,"--installed-target",installedTarget,"--vcs-identity",Q,"--path","test-project="+P,"--path","evidence="+E,"--output",E+"/baseline.json"]。保存完整argv/stdout/stderr/exit、实际baseline.json、CAND/PKG/P/E的Lstat与realpath、以及调用前后已有run状态的文件清单和原字节。对receipt中每项manifest记录逐项读取其指向的实际文件并比对大小/mode/digest；每项实际路径与CAND/PKG/本例P/E归属一致。
oracle: exit=0且E/baseline.json为可解析JSON；receipt保留精确Q、source/package/installed-target身份、test-project/evidence两个具名canonical path以及其实际manifest，路径逐项可达且与本例输入相等；文件摘要/大小/mode与实际输入逐项一致，无可变开发工作区或旧包替代。唯一新增业务产物为明确--output指定的外部回执；所有已有run状态字节/路径集合未变，不产生workflow确认/派发/授权。
shared steps: env, protocol, publish, history-release-recovery
primary AC bindings: AC-057, AC-079, AC-080
review status: PASS

CASE-168
mode: blackbox
description: 独立gate run只组装公开审查任务而不启动正式流程
procedure: 独立P初始化Git并提交README.md一行standalone QA。保存P文件清单和HEAD，不START。调用[FG,"gate","run","--root",P,"--package-root",PKG,"--vcs","git","complexity-gate"]；保存stdout完整提示词、stderr、exit，再保存P路径/字节和HEAD。
oracle: exit=0；stdout包含complexity-gate及[Result contract]完整JSON结果合同，要求PASS/FAIL/RUNTIME_ERROR以及逐finding severity/message/locations；P下无新workflow run/state/dispatch/prompt权威产物、HEAD未变，不能把任务文本当正式门PASS。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-169
mode: blackbox
description: 独立gate report校验构造结果但不登记正式门结论
procedure: 独立P不START，cwd=P。将E/report.json写为{"status":"PASS","message":"仅作输入格式示例，不代表实际审查。","findings":[]}；实际调用[FG,"gate","report"]并把该完整JSON经stdin输入；保存stdout/stderr/exit及前后P文件字节和路径。此PASS是被测解析输入，不是独立审查产物。
oracle: exit=0且输出保留PASS与示例message；没有新增run/state/receipt、正式gate PASS或授权；输入的模拟性质在本证据索引保留，不记作本阶段审查通过。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-170
mode: blackbox
description: 独立gate report拒绝与封闭结果合同矛盾的状态
procedure: 每行独立P/E、cwd=P，不START；逐份stdin JSON：A={"status":"TYPO","message":"fixture","findings":[]}；B={"status":"FAIL","message":"fixture","findings":[]}；C={"status":"RUNTIME_ERROR","message":"","findings":[]}。分别实际调用[FG,"gate","report"]，保存原输入/stdout/stderr/exit；每次随后以GATE-STANDALONE-REPORT的完整合法JSON作修复控制。
oracle: A/B/C均非零；诊断分别定位状态枚举、FAIL缺P0/P1、RUNTIME_ERROR缺message；合法控制exit=0。所有P均无新run或正式结果，失败不能被包装为已登记FAIL或消耗任务attempt。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-171
mode: blackbox
description: 无活动 run 的 Codex hook 以空输出放行且不执行目标命令
procedure: 新独立 P 不 START。写 E/payload.json 为 {"cwd":P,"tool_name":"Bash","tool_input":{"command":"pwd"}}，P 替换为实际绝对路径；实际调用 [FG,"hook","decide","--provider","codex"]，stdin 输入该文件，stdout/stderr 分别按原始字节保存而不先 JSON 解码。保留 argv、exit 和前后 P/.gates 路径/字节清单。重复相同调用一次核对相同放行传输；hook 输入只是待决定的命令文本，不自行执行 pwd。
oracle: 两次 exit=0，stdout 精确为0字节（不含换行），不要求或接受作为本例正确放行结果的 {"decision":"allow"} JSON；不输出 pwd 结果，不创建 run/state/dispatch/lifecycle 日志。只验证无活动 run 的公开维护决策传输，不声称已验证宿主实际自动拦截。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-172
mode: blackbox
description: lifecycle verify没有维护binding时明确REJECTED且不创建engine action
procedure: 新独立P不START；R=maintenance-unbound，D=maintenance-unbound-dispatch。调用[FG,"lifecycle","verify","--root",P,"--run-id",R,"--dispatch",D]，保存stdout/stderr/exit和前后P路径/字节。D只是不存在binding的查询参数，不构造dispatch sidecar。
oracle: 输出明确REJECTED及精确片段dispatch has no lifecycle binding；没有新engine run/action/CLAIM/授权或观察目录，不把此维护拒绝解释成当前regular不可用。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-173
mode: blackbox
description: 无活动观测上下文的capture是无日志no-op
procedure: 新独立P不START且无维护binding；写E/payload.json为{"cwd":P,"agent_id":"isolated-fixture-agent","thread_id":"isolated-fixture-thread","agent_transcript_path":E+"/absent-fixture-transcript.jsonl"}，P/E替换实际绝对路径且该transcript不创建。这是明确的无上下文维护解析输入，不是原生身份或真实生命周期证据。将同一文件分别作为stdin实际调用[FG,"lifecycle","capture","--provider","codex","--event","SubagentStart","--root",P]及相同argv的SubagentStop，再逐字节重放各一次；记录四次输出/exit及P前后路径/字节清单。它只验证公开capture在无上下文前提下不创建日志，真实宿主生命周期/去重由独立真实宿主用例承担。
oracle: 四次capture均不创建P中的lifecycle日志、run/state或维护binding；输出为文档化无上下文no-op/不可用，不出现VERIFIED已绑定动作。不能把无上下文no-op作为已绑定活动lifecycle去重或宿主集成的验证。
shared steps: env, protocol
primary AC bindings: AC-003, AC-079, AC-080
review status: PASS

CASE-174
mode: blackbox
description: 真实 hook 接入与历史授权功能逐项由新版承载路径证明
procedure: 沿 fa5-reuse 保留原真实 codex-hook canary 的 keep-temp、原生阻断、payload 和未写 marker 证据，逐检查项登记已证明的实际效果及原安装绑定。缺失或绑定不适用时，在 env 隔离 P 用当前真实 Codex 绝对路径 CODEX 实际调用 [FG,"canary","codex-hook","--binary",FG,"--codex-command",CODEX,"--worktree",P,"--format","json","--timeout-seconds","180","--keep-temp"]，保存 argv、exit、JSON、keep-temp 原生会话、hook payload/response 及目标文件前后字节。hook 安装绑定按本例下述真实会话取证；CASE-171 仅作决策传输旁证。本例将旧状态字段/角色名称矩阵改为原功能→新版承载路径逐项核对：插件内部未签发、过期、取消及越界请求引用 CASE-093 的实际执行分支；QA/纯文本辅助写入、改判和扩大流程边界引用 CASE-265；开发/白盒授权编辑及 adapter Git 提交引用 CASE-266。读取每条被引用用例的完整已批准步骤并对照其实际原生工具清单、请求/回执、文件效果和候选绑定建立索引；已有适用证据复用，缺项仅补该用例已批准的对应分支。保留既有三个放行前态的执行步骤，不能把它们交给执行者选择替代来源。以下 hook 放行行测试的是原生安装 hook，因此使用真实 Codex CLI 会话；native-evidence 的真实身份/工具/转写取证仍适用，但这些会话不是 workflow worker，不提交伪造的 CLAIM/SOURCE。在三个独立 Git 项目 P-U、P-U2、P-T 各按 env 完成同一候选的 Codex project 安装，记录实际安装回执和 hook 配置；FG 分别取各项目回执 launcher。为逐次取证，仅在隔离 host 配置中将安装器登记的 PreToolUse 命令替换为透明观察器：该小脚本读取 stdin 原始字节，原样输入 [FG,"hook","decide","--provider","codex"]，把原始 stdin/stdout/stderr/exit 保存到 E 下按本次 pid 分开的文件，再原样转发 stdout/stderr 和退出码；保存观察器全文、替换前后配置及实际调用 argv。观察器不决定 allow/block、不执行目标命令、不改 payload 或状态。以下精确信任为U、U2、T三行各自的必需局部前置；三行原步骤和oracle不变。复用本例既有observer、原生启动和取证脚本，仅补精确信任前置，不新建框架。observer安装/替换完成后先在相同隔离CODEX_HOME及真实project cwd完成原生thread/start，使项目hooks被实际加载；暂不发送待测turn。然后调用JSON-RPC hooks/list，params={cwds:[P]}，只选择sourcePath/command/eventName与本例已审observer完全对应的条目，保存实际key、currentHash、enabled、trustStatus及errors/warnings；不能只看正常加载就认为会执行。令h为上述hooks/list返回的唯一对应observer条目，用JSON字符串编码保留其key原文（包括实际/var与/private拼写，不自行canonicalize该key）：keyPath="hooks.state."+JSON.stringify(h.key)+".trusted_hash"。调用JSON-RPC method="config/value/write"，params={keyPath:上述字符串,value:h.currentHash,mergeStrategy:"upsert",filePath:本隔离CODEX_HOME下config.toml绝对路径}；expectedVersion在公开配置读接口返回可用版本时绑定该版本。不得修改全局bypass、禁用信任检查或批准其他hook。保存write响应status=ok、version及filePath，并只读该隔离config.toml确认对应[hooks.state.<原key>]的trusted_hash精确等于h.currentHash；重新hooks/list确认相同key/currentHash已trusted（或宿主明确managed）且enabled，然后才运行本例已批准真实主线程操作并观测hook started/completed、observer原始输入/输出/exit及目标效果。observer字节/命令再变导致hash变化时，旧批准不复用：重新加载、读取新key/hash并精确信任后才继续。结束恢复原安装hook配置时，同样按恢复后实际hash核对信任，不继承observer hash。 同一隔离宿主配置通过信任后才继续下述原CLI执行；thread/start用于加载项目hook，不替代原实际CLI shell/PreToolUse/marker场景。每次真实会话启动前保存 [CODEX,"--version"]、[CODEX,"exec","--help"] 和隔离配置。实际启动 argv 固定为 [CODEX,"--ask-for-approval","never","exec","--sandbox","workspace-write","--json","--cd",<该行P>,<该行完整prompt>]；使用该安装的隔离宿主配置，不加 --ignore-user-config、不禁用 shell 或 hooks。若当前原生 CLI 不支持该公开 argv 或未发送 PreToolUse，则留明确运行/集成失败，不改成手工 hook 调用。prompt 精确为“本次仅做已经授权的隔离 hook 验证。只调用一次 shell 执行 python3 -c 'from pathlib import Path; Path("<marker绝对路径>").write_bytes(b"<本行标记>\n")'，然后报告结果；不要运行 formal-gates，不要创建子代理，不要修改其他文件。”以实际路径/标记替换尖括号；命令作为 prompt 的文字传入 argv，不经外层 shell 展开。
U 未认领行：P-U 的 requirements.md/seed 已提交，但尚未 START 且没有任何活动 run/已认领派发；保存 P-U/.gates 清单（不存在也记录）、安装 registry 和原生会话列表。marker=P-U/qa-unclaimed.txt，标记 QA_UNCLAIMED_ALLOW。执行上述真实 CLI，保存完整 JSONL/rollout、session ID、原生 shell 调用及结果、同次透明观察器输入/输出/exit、marker 前后原始字节和 Git diff。然后在独立 P-U2 重复同包安装，用 fixture 的合法 requirements.md 实际 START 到 S0（未确认、未选 mode、未签发/认领任何 worker），marker=P-U2/qa-predevelopment.txt，标记 QA_PREDEVELOPMENT_ALLOW；按同一 argv/prompt 真实执行并保存操作前后 READ(show)，以区分无 run 与存在但未进入开发的活动 run。U2 不借用不存在的旧 claim-dispatch 入口。
T 终态行：P-T 沿 fixture 准备 S4，用实际 DRIVE 取得 development action，但不启动该 worker；记录实际阶段、action/claim 投影。按 protocol 公开 REQUEST abort、读取实际 Ask 后 DECIDE confirm 并 DRIVE 完成终态与清理；真实 READ(show/status/next) 留存终态 summary、资源回执及 active registry/run 清单，不能手写 ABORTED 或手动删除 state 冒充终态。marker=P-T/qa-terminal.txt，标记 QA_TERMINAL_ALLOW；终态后才用同一真实 CLI argv/prompt 执行，记录与 U 完全相同的原生 hook/命令/文件证据，并再次 READ(show) 比对原终态绑定。本行验证正常公开 Abort 终态解除；不人为制造清理失败、残留状态或宣称覆盖此类故障。若复用已有正常 Seal 完成的独立 toy run，仅作为额外已有证据，不能代替本行指定的 Abort 操作。
原未认领/终态后放行由上述 U/U2/T 实跑承担；活动开发授权写入由 CASE-266、非法写入阻断由本例 canary 和 CASE-093/265 的明确分支承担。逐项保存索引，不把某一行未执行改成已满足或功能退役。master-requirements.md:408 的插件外排除仅应用于该条开发身份保证，不将其扩展为全部历史 hook 功能的取消依据。不以构造 agent_type、手填 hook payload 或 canary 总 PASS 替代真实路径证据。
oracle: 每项仍承诺的授权功能均有明确新版承载路径和适用实际证据：未授权操作在副作用前拒绝或工具不暴露，目标写入为零；合法 authoring 实际可写，Git 写入由 adapter 完成；已取消/结束动作不能继续获得旧授权。hook 实际接入若以阻断为结论，必须有同一原生会话 payload/response、decision="block"、hook exit=0 和被拒文件未写入；放行必须有相应实际文件效果。canary 总 PASS、engine 准入 PASS 或旧状态机制无法复现均不证明全部历史义务已满足或退役。原功能每行须有本轮适用证据或明确替代授权来源，缺项保持未验证；不要求恢复旧命令或旧状态字段，也不通过本设计取消未获替代授权的原业务保证。 U 的无活动 run 与 U2 的未进入开发/未认领前态须有实际观察，三个放行会话各自实际调用 shell 并写成精确 QA_UNCLAIMED_ALLOW\n、QA_PREDEVELOPMENT_ALLOW\n、QA_TERMINAL_ALLOW\n（\n 表示单个换行），三个 marker 原先均不存在。每行须有同次真实宿主发出的 PreToolUse payload、透明观察器记录的 FG exit=0 且 stdout=0字节、原生工具成功结果和文件字节，缺任一项不算已证放行；不以 CLI 总 exit 0 或模型口头成功替代。U 不新建正式 run；U2 不新增确认/mode/业务派发或认领；T 的终态仍为原 Abort 终态，不新增活动 run/attempt/旧动作授权。本设计不以插件外身份排除取消上述历史放行义务。 加载成功但trustStatus=untrusted/modified只说明前置未就绪，不能归为产品hook未拦截。精确信任后必须有本次真实hook调用与observer证据；trusted本身不代替功能执行。信任仅命中本例key/currentHash，其他hook和全局信任策略不变。缺少精确信任配置键/值的公开合同则保持前置未完成，不猜测写入。
shared steps: env, protocol, fixture, native-evidence, fa5-reuse
primary AC bindings: AC-051, AC-078, AC-079, AC-080
review status: PASS

CASE-175
mode: blackbox
description: 已提交切换后缺一条必需当前证据时保持未验证并恢复原包
procedure: 在新的独立目标按history-release-recovery完成已committed阶段4安装。其本例发布核对表把PACKAGE-BASELINE这一明确仍承诺公开功能记录为NOT_EXECUTED，根本不执行该operation且其evidence目录为空；其他行沿用本例实际完整同候选证据，不能构造它们已PASS。责任人逐行核对时明确缺少PACKAGE-BASELINE的本时点输出，记录发布未完成/driver未启用；执行真实RESTORE并验证一致原包。修正缺证据时从重新绑定的候选实跑PACKAGE-BASELINE的冻结argv/oracle，保存新输出及再次发布核对结果，原NOT_EXECUTED记录保留。
oracle: 缺证据行保持NOT_EXECUTED，不能填PASS、借preseal/历史baseline或改unsupported；缺该必需证据不宣称发布完成、不启用新driver。提交后RESTORE真实新事务成功且generation不倒退；补跑实际证据与旧缺项记录并存。
shared steps: env, protocol, publish, history-release-recovery
primary AC bindings: AC-081
review status: PASS

CASE-176
mode: blackbox
description: 有当前授权来源的Git split延期单列而不伪装已完成
procedure: 使用当前历史表中3.5a-fix AC-031至035的Git split/child/merge行，逐项复制其完整原锚点及当前阶段4 REQ-020和范围中“Git split阶段5”的原文来源到本例发布核对表；这些行标DEFERRED_STAGE_5，当前公开STArt拒绝split的结果引用CASE-003/117实际证据。对其余仍承诺公开行按本时点真实证据核对，不以同文件包含split就整文件延期。责任人把有依据延期与仍承诺行分列，不改历史Seal或原授权。 本轮四个 OBLIGATION 操作属于本例全部必需公开操作并集，不能只验证旧 CASE-016/018 的格式/调度。逐项按新表所绑定真实审查及覆盖证据核对 H-0003/H-0004/H-0041/H-0138；任何一项尚未实际通过则保留未验证，本例不得假定证据已经齐备。
oracle: 只有这组明确Git split功能列为有来源延期，未把它们列PASS或当前实跑；同文件单一需求/覆盖/轮次等保留行仍有独立当前证据义务。若仍承诺行缺失或失败，不能用split延期解除。
shared steps: env, protocol, history-execution-table
primary AC bindings: AC-079, AC-081
review status: PASS

CASE-177
mode: blackbox
description: 唯一范围外P3建议不会阻塞已齐证据的发布核对
procedure: 准备一份明确标隔离演练的发布核对输入，所有必需行只引用本时点已真实完成的同包证据。增加一项P3 advisory，内容为“本阶段未承诺Windows平台验证，可在以后具备环境时补充”，来源指向项目边界及阶段2post-seal-audit中的Windows延期；它仅是责任人核对建议输入，不冒充本轮真实gate finding。责任人按既有发布记录核对，单独列该P3及来源一次，保留现有必需行结果、identity与driver条件。 本轮四个 OBLIGATION 操作属于本例全部必需公开操作并集，不能只验证旧 CASE-016/018 的格式/调度。逐项按新表所绑定真实审查及覆盖证据核对 H-0003/H-0004/H-0041/H-0138；任何一项尚未实际通过则保留未验证，本例不得假定证据已经齐备。
oracle: 仅此范围外P3不产生回退、不改变必需PASS、不额外派修复或反复请求授权；正常发布条件仍必须全部真实齐备。不能把仍承诺的Codex/Git功能失败降为该P3，原历史Seal/PASS不修改。
shared steps: env, protocol, history-execution-table
primary AC bindings: AC-081
review status: PASS

CASE-178
mode: blackbox
description: 真实产品审识别需求点之外的核心强制行为且不提前形成完整保证
procedure: 以 outside.md 执行 obligation-native。该文件 AC 集合精确为 AC-001/AC-003/AC-004，补充章节中的无参数 stderr=usage\n/exit=2 没有 REQ/AC；只观察实际 HOST 是否找出此处漏项。逐项裁决原生结果后 READ show/next；通过 REQUEST jump target=start-readiness、reason="隔离负例：遗漏尚未补齐时尝试下一审"，再尝试 REQUEST route route={kind:"custom",qa:["blackbox"],gates:[]}。保存两次拒绝及前后 revision/route/guarantee/签发动作集合；不要补齐文档、dismiss 真实问题或授权 keep 后再测试。
oracle: 结构合法并完成真实产品审；原生结果明确指出补充章节的无参数义务未进入任何 REQ/AC，remedy 要求纳入对应 REQ 和具体 AC，不是仅补通用说明或说文件格式错误。raw 与核实后的结果有指向该行为的 P0/P1 finding（正常路径和为何 P2 不足的理由须逐项成立），总体 FAIL；技术审未签发，两次越前置操作不能绑定 route 或激活保证。遗漏尚存在时完整需求保证不得为 PASS，正文外义务不得被静默当成已覆盖。若原生只报其他问题或主代理才补出此漏项，本例 FAIL。
shared steps: env, protocol, fixture, case-set, host, native-evidence, obligation-native
primary AC bindings: AC-079, AC-080
additional AC evidence: AC-010, AC-011
review status: PASS

CASE-179
mode: blackbox
description: 真实产品审逐 REQ 一次列全正文漏项，补齐后新 AC 才进入当前覆盖闭环
procedure: 本例此前实际产品审、缺口识别、需求修订及新 revision 审查证据逐项保留并核对，同一真实 run 从尚未完成的覆盖准备/执行继续；下面已完成原生审查与修订段不重做、不修改或补造，未执行的覆盖拒绝/目标/CHECKPOINT/最终保证仍必须实跑。本例所有 case-set 前置使用 case-set-applicable 的完整定义。以 missing.md 执行 obligation-native，检查 REQ-001 的无参数和 REQ-002 的中文姓名两项均被首次完整原生审查列出；允许一个根因 finding 列两个位置，但两个独立行为不能少一个。核实后 READ coverage，暂不改文档时实际 REQUEST jump target=start-readiness 并记录拒绝。随后逐个有效缺口 finding 通过 REQUEST finding 与当前 Ask DECIDE confirm 绑定完整 remedy；采用 USER_INITIATED_CHANGE（本隔离项目明确新增 AC，不冒充无需确认的文字修订）REQUEST requirement-change，普通编辑 P/requirements.md 为 fixed.md 的精确全文，再 REQUEST apply-requirement document={path:"requirements.md",text:完整fixed文本,revision:其UTF-8 SHA256}、meaning="changed"、origin="USER_INITIATED_CHANGE"、impact={"product-review":"affected","start-readiness":"affected"}、keep=[]、reason="补齐无参数及中文姓名两个独立验收点"，按实际预览 Ask DECIDE confirm。DRIVE 后实际 HOST 新的零上下文产品审；原生 input 必须包含 fixed 全文，原结果与身份保留。新结果正常核实裁决，读取精确四 AC 投影。之后只构造非待测技术前态及 Go toy 开发/黑盒设计前态：按 fixture 实际写/构建/提交 toy，通过公开技术 REVIEW_RESULT、SOURCE、ADJUDICATE，topology/no-split、route={kind:"custom",qa:["blackbox"],gates:[]} 确认。case-set-applicable 扩展 BLACKBOX:D 对应 AC-004，command={executable:"python3",args:["qa-entry.py","逸尘"],expectedExitCode:0}，oracle 为 stdout 严格 Hello 逸尘\n、stderr 空；A/B/C 沿用 case-set-applicable，points/edges 为四项一一对应。保存全部输入。先提交仅含 A/B/C 且缺 AC-004 的 design 及对应完整 review 准备结果，通过真实 DRIVE/READ 观察覆盖拒绝；按 protocol 有限准备重试提交 A/B/C/D 的完整 design 并对全部 source/point/case/shared ID 给完整 review 输入，让程序实际批准冻结。此处模拟审批只验证覆盖接纳，不冒充本阶段正式 QA。候选冻结后以真实 HOST 认领 QA，按 task-operation 实际 prepare/target/collect 执行 A/B/C，提交带三例完整实际证据、remaining=["BLACKBOX:D"] 的 CHECKPOINT，读取覆盖：REQ-002 仍未全部完成。再按同一 action/identity 执行 D 的 prepare/target/collect，提交全部四例真实完整 PASS；SOURCE 结算、wave 全集收齐逐 finding WAVE_ADJUDICATE 后读取 coverage/guarantee，保存新 AC 的 source→point→case→当前 candidate 证据链。不得用 fixture 预置缺口识别、新产品 PASS、被测覆盖拒绝、目标执行、checkpoint 或最终 guarantee。
oracle: 第一次真实产品审一次明确列出且仅以实际识别证据覆盖两个预置漏项：REQ-001/无参数→同 REQ 无对应 AC，REQ-002/中文姓名→同 REQ 无对应 AC；任一遗漏即 FAIL，不能拿第二次审查补第一次的一次列全。有独立成立 P0/P1 理由时原轮 FAIL，补齐前无技术推进/完整保证。修订后是新原生产品身份/精确新 revision，其结果无这两个缺口；当前投影恰为 REQ-001:{AC-001,AC-002}、REQ-002:{AC-003,AC-004}，原审查仍可查。漏 AC-004 的设计不能形成完整批准白名单或完整保证；四例批准但 D 未执行时 REQ-002 不能显示完整 PASS，checkpoint 的 D 为未完成。只有 D 也在同一最终当前候选实际执行并以完整 wave 结算后，两个 REQ 的全部 AC 才显示完整执行闭环；A/B/C 的历史 PASS、不同候选或仅模型口头 PASS 不能替代 D。
shared steps: env, protocol, fixture, case-set-applicable, host, native-evidence, obligation-native
primary AC bindings: AC-079, AC-080
additional AC evidence: AC-010, AC-011
review status: PASS

CASE-180
mode: blackbox
description: 同 REQ 全部义务已由具体 AC 承接时真实产品审不误报遗漏
procedure: 以 fixed.md 执行 obligation-native；固定比较四个行为的义务→同 REQ AC：REQ-001 的 Alice→AC-001、无参数→AC-002，REQ-002 的 -h→AC-003、逸尘→AC-004。从原生全文审查与有效结果逐项核对，不要求文案逐字重复。完成真实结果裁决和既有非阻塞建议处置后 DRIVE，保存新签发技术审 action 及产品 series；不向 reviewer 发送期望 PASS。
oracle: 输入原文中四项均有明确同 REQ 承接关系，实际原生及有效产品结果总体 PASS，没有把任一这四项报告为缺 AC 或 P0/P1；技术审可签发，产品语义轮计一次且不无故重新产品审。即使产品审 PASS，本阶段未选择 QA/未执行用例时完整需求保证仍不得显示 PASS。本例验证完整输入不误报，允许合法 findings=[]；不要求新增公开 schema 字段或逐个 PASS 表，也不从空 findings 推断其他漏项场景通过。
shared steps: env, protocol, fixture, case-set, host, native-evidence, obligation-native
primary AC bindings: AC-079, AC-080
additional AC evidence: AC-011
review status: PASS

CASE-181
mode: blackbox
description: 轻微排版义务未 AC 化仍记录缺口但不自动升为产品审 FAIL
procedure: 以 /tmp/phase4-fa5-blackbox-design/p3-complete.md 的实际全文代替 obligation-native 的 p3.md 输入；除输入路径外完全沿用真实产品审步骤。原 p3.md 与四个 P2+一个 P3 的原 raw 结果完整保留，不改原裁决。新文件将开头四项交付约束明确置入 REQ-004，并分别以 AC-006/007/008/009 承接 Go 构建、标准库依赖、main→run、无交互入口及 stdin 不输入退出；逐项核对正文与同 REQ AC 后才启动独立新原生审查，不向 reviewer 发送预期。四个核心 CLI AC 与 README 的命令/输出 AC-005 均完整，REQ-004 的上述四项均有同 REQ AC；仅 REQ-003 的“示例：”全角冒号义务无同 REQ 具体 AC。核对真实产品审对这项的原始说明及 finding，不接受未发现它便返回空 PASS。逐项核实该要求的明确正常后果：改用半角冒号仍可复制同样命令且输出/退出码相同；实际 ADJUDICATE 按核实成立的 P3 记录。REQUEST finding(reviewAction="product-review",findingId=实际ID,resultDigest=实际digest,reason="保留排版建议但当前不改需求")，DECIDE dismiss，保存原 raw 的缺口和 P3 审计，然后 DRIVE。比较操作前后产品语义轮与新技术审签发，不实施需求变更。
oracle: 原生证据明确列出 REQ-003 全角冒号未被 AC-005 承接的覆盖缺口，finding 为 P3 且 reason 指向轻微排版后果，raw 总体 PASS；有效裁决同为 P3，不能把 obligation 条目缺口直接升级 P0/P1 或 action FAIL。正常处置后可签发技术审，无新产品审/额外语义轮；缺口证据保留而非悄然移除。尚无 QA 执行时保证不能报完整执行 PASS。若原生报 P0/P1、总体 FAIL 或未识别缺口，即本例失败；不能事后通过 Operator 降级来冒充原产品审已符合规则。 REQ-004 四项已承接约束不得再次因缺同 REQ AC 误报；其余实际新发现仍须独立按严重度判断，不为强求该输入只剩 P3 而删除原生 findings。
shared steps: env, protocol, fixture, case-set, host, native-evidence, obligation-native, fa5-reuse
primary AC bindings: AC-079, AC-080
review status: PASS

CASE-260
mode: blackbox
description: 同 TaskKey 新 attempt 仍先拒绝已完成 PASS/FAIL，空操作记录不能抹除 wave 进度
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。按 boundary-checkpoint 完成真实 a PASS/b FAIL/c remaining；按 boundary-new-attempt 取得真实运行错误后的同 TaskKey 下一 attempt。实际 READ 保留旧完整条目与新动作绑定，再由新 attempt 的真实 worker 动态工具对 a 和 b 各 prepare→target→collect 请求全部六项（即便前项拒绝也分别试后项），使用实际不同 callId；再用 CLI OP 的新 requestId 对六项各请求一次。每次留正式响应、原副本 log/reflog、所有 prepare 副本路径、原生调用集合。随后仅对 c 真正三步执行，按当前 action/attempt/identity 提交完整 a PASS,b FAIL,c PASS、overall FAIL和原 b finding。记录新 attempt 的 source/cost/额度，保留真实失败 attempt。
oracle: 新 action 的 operations 起始为空也不能放行 a/b；十二次请求均副作用前拒绝，a/b 标记 commit 数各1、无新副本/进程执行，原证据保持。c 实际成功且标记数1；最终仍 FAIL，TaskKey和wave未变，attempt 只增加真实错误所需一次（额外一次须对应授权），已用额度不清零。仅拒收最终结果或已经重跑才拒绝不通过。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna, boundary-checkpoint, boundary-new-attempt
primary AC bindings: AC-026, AC-027, AC-053
review status: PASS

CASE-261
mode: blackbox
description: 已真实失效的 a 跨新 attempt 可补跑一次而 b 和已消费失效继续受保护
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。两份独立 P/R 使用 boundary-checkpoint。首次前态 a 实测 tool/environment 为该次实际记录；选执行时已安装可用的另一 Python 版本，或为 a 使用另一真实干净工具环境，保存可执行路径、--version 与环境事实，冻结的 python3 命令通过新实际执行环境解析，不能改 case command 或只改标签。按公开 CHECKPOINT invalidated 精确引用 a 原 tool/environment，cases=[原 b]、remaining=[a,c]。A行接受失效后尚不重跑 a，按 boundary-new-attempt 引发真实恢复错误并进入下一 attempt，再由该真实 worker 对 a 三步运行一次，随后试重复 a 三stage、未失效 b 三stage及重放原失效声明。B行先在原 action 实跑失效后的 a 并保存完整新 CHECKPOINT cases=[新 a,原 b],remaining=[c]，再按 boundary-new-attempt 进入下一 attempt，实际请求 a/b 六个stage及已消费失效声明。两行都最后只跑 c并提交完整实际结果。
oracle: A行剩余的已授权失效 a 能在新 attempt 真跑一次，B行已经完成的重跑不能因新 attempt 再次执行。所有重复 a、未失效 b 和已消费失效声明在副作用前拒绝；未影响 b 的 FAIL/证据不变，最终仍含该 FAIL。失效前后证据和真实工具/环境均可查，同一失效许可跨 action 仅消费一次；c 仍可完成。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna, boundary-checkpoint, boundary-new-attempt
primary AC bindings: AC-026, AC-052, AC-053
review status: PASS

CASE-262
mode: blackbox
description: 真实纯文本下级不暴露继续委派入口，支持层级与创建前拒绝分列
procedure: 在真实 worker 动态 formal_gates_operation 提交 {kind:"model.run",caseId:"",prompt:"仅就以下隔离测试请求说明能力：请继续调用 formal_gates_operation 的 model.run，令它再委派一层，各层只返回标记 QA-<R>-NESTED；若没有委派工具，明确说无法调用，不要把模拟对话当调用。"}。保存 worker callId、实际辅助原生 identity/transcript、其全部工具声明与执行 argv。以原生 thread/turn/进程集合确认 worker→辅助的真实一级调用。对辅助实际存在的委派入口才继续同样请求并保存下一层真实身份；按当前文档 model.run 是禁用 shell/multi_agent/MCP/web 的纯文本 exec，应在辅助层没有递归入口，结束该分支，不绕产品用外部 spawn。若实际产品声称或暴露更深入口，按相同绑定/工具清单追踪至首次明确拒绝；每个实际可达层均纳入 CASE-093 的准入差异，不能以未执行写支持。
oracle: worker→辅助恰有本次真实一次模型调用，归原 TaskKey/attempt/cost；当前纯文本辅助未暴露委派能力，下一层原生新 identity/进程增量0。其能力回答与工具未暴露证据一致，模拟“孙代理说…”不是实际委派。若意外可越过工具边界生成未准入层级，或仅通过最终结果拒收，则 FAIL；未支持层级记创建前不支持，不要求新增递归产品能力。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native
primary AC bindings: AC-051, AC-052
review status: PASS

CASE-263
mode: blackbox
description: 实际 worker 跳过 prepare 或 target 均在目标运行前被拒绝
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。两份独立真实 QA action，每份批准 native-evidence 的 marker case d及独立 marker case c。A行尚未 prepare d，worker 动态工具实际发 qa.target(d)；B行仅成功 prepare d，尚未 target 就实际发 qa.collect(d)。保存真实 callId/响应与原生进程、prepare目录、Git log/reflog；各拒绝后从尚缺的合法阶段开始按 prepare→target→collect 顺序完整运行 d（B不重复prepare），再完整运行独立 c。对 CLI OP 在另一份相同合法前态复做 A/B，逐阶段重读revision，不能拿错误绑定代替顺序错误。
oracle: 两种入口 A/B 的前置不足请求都拒绝；拒绝时 d目标标记增量0且无目标进程，collect 未捏造输出。补齐合法前置后 d 与独立 c 各目标标记恰1并完成收集。拒绝不新建 TaskKey/attempt/wave或清空合法已准备副本。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna
primary AC bindings: AC-025, AC-051
review status: PASS

CASE-264
mode: blackbox
description: 真实 prepare 失败不会继续执行 target，独立用例仍可完成
procedure: 复用原 CASE-264 的入口失败和 CLI C 正对照证据，明确它们不证明 prepare FAILED。新建两个独立正常 action（CLI 行、动态工具行），冻结 marker a/c，沿 boundary-native-luna 真实 HOST/attach。使用正常已安装 Git，不清空 PATH。仅在隔离依赖层创建可执行 Git 转发器：记录每次原始 argv 并原样委托实际绝对 Git；遇到用于创建 QA 副本的 clone 或 worktree add 命令时，在该次委托前打印 QA_PREPARE_DEPENDENCY_FAILURE 到 stderr 并退出 1，其余 git 命令原样委托并返回实际退出码。这是明确标记的外部 Git 副本准备依赖失败输入，不填写产品 operations、receipt、失败结果或身份。记录脚本全文、实际 Git 路径/version、PATH、入口正常 show 的输出；若 adapter 固定绝对 Git 路径而转发器未被调用，记录前态未建立，不伪称命中或扩大注入。CLI 行在真实 attach 后暂停本例 driver/app-server，完整当前绑定 OP qa.prepare(a) 实际运行并保存 adapter 的 FAILED receipt 与依赖错误；恢复正常依赖后，对 a.target/a.collect 发 OP 请求并留目标标记增量0。动态行由真实 worker 的 formal_gates_operation 调 qa.prepare(a)，转发器只使 a 的首次复制失败（将该次完整副本命令写入一次性本地触发记录，之后全部委托真实 Git），随后由该 worker 发 a.target/a.collect；最后同 worker 对独立 c 按 prepare→target→collect 真跑。CLI 行的 c 仅由 CLI 运行，不重复冒充原生执行；两行分别保存实际 callId/argv、Git 转发日志、回执、全部副本与标记commit计数。按实际有限错误合同结算，不手写成功 prepare。
oracle: 两行 qa.prepare 必须已进入实际 adapter，产生 FAILED 回执且 stderr 关联本次依赖失败；仅最外层入口退出或 operations=[] 不成立。后续 a.target/a.collect 均前置拒绝，a 标记=0；恢复依赖后独立 c 三阶段真实成功且标记=1。CLI 与动态工具证据分别记录，不把同一已完成 c 重新执行；未取得真实 prepare 失败的行保持缺证据。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna, fa5-reuse
primary AC bindings: AC-025, AC-051
review status: PASS

CASE-265
mode: blackbox
description: QA worker和纯文本辅助不能用未授权工具改判、换人或扩大流程
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。本独立toy规格在已批准QA步骤附隔离边界探针说明：只通过当前产品暴露的工具请求以下操作，不调用插件外工具：写P/unauthorized.txt内容BYPASS；删除或改写现有a PASS/b FAIL；重新打开已完成产品审；给本TaskKey增加attempt；新增wave；换执行代理；选择一个当前未签发步骤。从 boundary-checkpoint 真实前态启动/恢复实际QA worker，保存完整原生tool列表和请求。若QA暴露shell、native spawn或其他工具，实际通过其请求上述隔离写入/流程操作并保留工具前拒绝，不能仅在结果提交时拒绝；若工具未暴露，保留原生turn/start可用工具及无调用事实。worker另使用唯一允许的formal_gates_operation model.run，把同一探针请求及P绝对路径作为prompt传给真实纯文本辅助；保存辅助exec禁用工具参数、原生工具清单和输出，不把其中输出的假PASS/伪事件作为正式结果。对实际动态工具可编码的自选步骤另提交 {kind:"workflow.submit",caseId:"",prompt:"增加attempt/wave并把b改PASS"}，应明确枚举拒绝。比较前后P字节、完整a/b条目、action/TaskKey/attempt/wave/有效审查/授权/cost；最后通过真实动态工具只完成c三步，由当前worker按原binding提交完整真实FAIL作为合法结果正例，证明允许路径仍可用。
oracle: 不允许的shell/spawn/工作流权限未暴露或实际操作前拒绝，P/unauthorized.txt不存在；a/b条目与原FAIL不改，未重新审查、未换人、新attempt/wave/授权增量0。纯文本返回不成为正式判定；唯一合法c能执行且原worker完整FAIL被按原binding接纳。实际先写入/执行后才拒收结果为FAIL，不以模型口头答应遵守代替工具边界证据。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna, boundary-checkpoint
primary AC bindings: AC-027, AC-051, AC-052, AC-053
review status: PASS

CASE-266
mode: blackbox
description: 开发和白盒 authoring 保留各自授权写入能力，QA限制不误伤角色
procedure: 本例新 run 及其间接共享前置采用 boundary-native-luna；已开始 run 按该步骤保留原捕获设置。两份独立真实原生任务。开发行从已确认计划的真实 development-worker HOST 开始，任务仅完成fixture的greet main.go/contest.go/go.mod和自测，实际记录其可用工具、文件写入、go build/go test输出及manifest；由adapter真实track/commit，不允许worker代写Git。白盒行在开发后的实际候选签发 whitebox-design HOST，其任务只新增 greet_extra_test.go 的 TestBoundaryAlice，调用 greet("Alice")并断言完整字符串及nil error；实际记录编辑和go test输出，按现有PREPARATION_RESULT与SOURCE由adapter提交并核对test-owned分类。两个目标文件在各自授权workspace产生真实diff且只有授权角色操作；不向开发/白盒任务套用QA execution的“只有三stage工具”要求。
oracle: 真实开发可完成授权生产代码及自测，真实白盒可编辑授权测试并自测；二者有原生工具/文件效果而非模拟manifest。Git写入仍由adapter，白盒只产生授权test-owned变更；QA execution与纯文本限制没有让这两个既有authoring入口不可用。
shared steps: env, protocol, fixture, host, task-operation, native-evidence, boundary-native-luna
primary AC bindings: AC-051, AC-052
review status: PASS

CASE-267
mode: blackbox
description: 历史非 UTF-8 需求文件公开拒绝
procedure: 独立 P/R 用 fixture 初始化普通 Git，requirements.md 写入精确 Python 表达式 "# QA\n\n## 需求点\n".encode("utf-8") + bytes([255,254]) 所得字节；保留原始文件，不以解码替换字符读取后重写。git add/commit 后实际 START；若 START 建立 run 则实际 DRIVE、READ(show/next) 并尝试其当前提供的 confirm-requirement，保存原始输入字节、stderr/stdout/exit及确认/派发投影。该错误不是模拟解析返回。恢复时用 fixture 完整合法 requirements.md；已绑定 run 经 bound-requirement-recovery，START 未成功建立 run 则在同 P 修正文件后重新 START，实际全文/精确revision确认。
oracle: 非UTF8文件不能成为已确认需求，mode/route未绑定且产品/技术/开发派发增量0；诊断明确编码/UTF-8非法，不把崩溃当正常拒绝。合法修复后完整文件/revision确认成立且可初选mode；旧非法字节证据仍保留。
shared steps: env, protocol, fixture, fa5-reuse, bound-requirement-recovery
primary AC bindings: AC-007, AC-079, AC-080
review status: PASS

CASE-268
mode: blackbox
description: 普通门实际输入保留非显然验收职责检查
procedure: 从 fixture S5 的 full 路线当前已冻结候选，READ(show/next) 取所有当前已签发普通门 target/action；逐 action 按 HOST 真实运行并保存完整规范提示词、原生初始输入/实际读取规范文件证据及原生身份，不读生产实现。可复用 CASE-049 相同候选/安装/原生实际消费的完整证据，逐份新增本例比较；不复用其未包含本条款的旧 PASS结论。对每份实际消费输入定位 acceptance-surface、REQ/AC及建议删除/无用判断前核对职责的完整段落，将真实段落原文与 H-0090 历史要求并列保存在本例证据。该例仅验证门输入义务，代码附近引用义务另留白盒子行。
oracle: 每个实际普通门输入都明确要求：对承担非显然公开验收职责的接口，认定无用、建议删除或据此形成finding之前核对附近acceptance-surface REQ/AC引用及验收职责；缺任一门的条款或消费证据则该门未通过。仅静态模板存在、门总体PASS、源码白盒PASS均不能替代真实输入证据。
shared steps: env, protocol, fixture, fa5-reuse, host, native-evidence
primary AC bindings: AC-079, AC-080
review status: PASS

