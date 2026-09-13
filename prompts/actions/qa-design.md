# QA 设计

写清前态与后态、步骤顺序和取证入口。静态检查只解析，不导入或 source 会启动任务的脚本。

交付可执行用例和共享准备的草稿增量，status 为 DRAFT；DRAFT 只表示登记设计，不是覆盖 PASS。
增量仅定义与已有设计的合并方式，不缩小当前 action 的工作范围。首次设计面向完整已确认需求；返修须处理全部未解决审查意见和覆盖缺口。
用例充分性、需求/AC 映射与全套覆盖由独立 QA 审查判断，不由设计者自行返回覆盖 PASS/FAIL。
读取 previousDesign 中的当前完整用例集、reviewGaps、designGap 及 existingTestEvidence，
只提交必要增删改；未提到的用例、共享步骤和有效证据由引擎保留。
新增 caseId 使用本次唯一的局部标签，引用该标签填写 edges、dependencies 和 reuse；引擎分配正式 ID。
修改已有用例必须引用 previousDesign 的正式 ID；removeCases 和 removeSharedSteps 列明要删除的正式 ID。
默认 replace=false；仅需明确整体替换时设为 true，并交付新的完整集合，不能靠遗漏条目隐式删除。
返修必须修改实际用例或准备内容；只列缺失 AC、改说明或重交相同材料不算修复。
真实读取/执行障碍走原有运行时错误路径，并保留已完成工作；不能因为基线没有新功能就判设计失败。
previousResultRejection 是当前动作的拒收原因，按原因修正提交。

从已确认需求出发，为当前 action 指定的 QA 模式设计用例集。

本次新增或修改的产品入口在开发前按已确认接口契约设计步骤，不要求基线帮助已包含
该入口；测试准备可构造的边界见下文，被测功能与执行证据仍须在快照后真实运行取得。

**用例集是否充分由 QA 审查（review）判断，不设机械化质量下限**——被选中模式零用例是
覆盖缺失，由 review 的 set-level 覆盖判定承担。

**测试准备可构造（黑盒与白盒共用）。** 低成本时优先真实链路；前置真实链路需要较高的
时间、token 或重复派发成本时，可以构造或手工准备。按具体用例判断，不设硬成本阈值或新
配置，不要求用户逐例授权，也不要求一律模拟或一律真实。可构造的材料包括测试环境、
fixture、输入、已有前置状态，以及模拟审查结果、授权、生命周期等测试数据。在用例既有
步骤中明确构造部分为隔离测试材料，写明构造方法及所代表的前置；测试准备不要求必须有公开
生成入口。选择构造时，无需为造数据真实跑前序产品审、技术审或开发流程、等待模型输出
特定等级，也无需重新要求用户确认虚拟测试项目。

- 构造前置后，被测操作或逻辑仍须实际运行并观察结果：黑盒通过文档化产品入口，白盒运行
  绑定的结构测试。不得把被测判断或输出预先写成预期值后声称通过。若验证的就是宿主真实
  集成、真实派发或身份捕获，必须取得相应真实观测，不能用构造输入或模拟身份冒充。
- 手工构造正常可达的前置状态不等于破坏状态，不能因此自动降级或否定发现；将不受支持的
  状态破坏当作产品应保证的新场景，仍受项目边界限制。输入与失败路径同样以已确认需求、
  文档化正常使用和常见操作失误为准。
- 构造仅用于隔离测试环境，不得篡改当前项目的正式 run，或将模拟材料冒充正式审查、授权
  或正式 PASS。测试材料与实际运行所得证据须明确区分。

设计结果使用当前 schema 的 design（replace、removeCases、removeSharedSteps、manifest、alternatives，白盒另含 testFiles）；manifest 中
包含 binding、sources、points、cases、edges、sharedSteps。所选模式合起来须覆盖全部 AC，
每个已覆盖 AC 的 pointId 为 REVIEWKIND:AC-ID，例如 BLACKBOX:AC-001；case ID 同样带
REVIEWKIND 前缀，并以 edge 连接需求来源和验收点。每个 case 有独立 oracle。

把行为主张写进描述；把文档化的入口、必要的前置条件、用户操作和需要留存的证据写进
步骤；把可观察的预期结果和失败信号写进预期判据。代码检视、模拟输出或开发者自测主
张都不能替代黑盒的实际使用执行。共同准备和操作在 manifest.sharedSteps 保存一份，case.sharedSteps 按 ID 引用；逐例保留特殊前提、操作差异、证据与 oracle。

覆盖文档化的正常使用和常见操作失误，并逐验收点设计：正向生效、失败路径或不得
误报、边界/中间态、以及本次改动可能破坏的既有文档化正常使用行为的回归面——凡对应
不同可观察行为、或需要不同公开步骤才能验证的角度，都是独立用例，不得合并成一条顺带
带过，也不得只留正向路径。在直接拥有该行为的最低层测试每个行为，且要测得深、不止测得到：预期判据必须落到
可机械比对的具体观察——精确的消息片段、字节/摘要、列表内容与顺序、数值与边界——不
得用「行为正确/符合预期/正常工作」级别的空判据；失败信号必须能在真实执行中把对错区
分开。步骤必须具体到他人无需追问即可照做，不得以类别名或「相应测试/相应入口」的方
式含糊引用。每个验收点从典型输入、边界/空/极端、错误输入三个层面考虑；已有强判据用
例覆盖的层面不必重复。当既有
的直接自动化覆盖已经验证同一条确定性规则时，不要在更高层重复覆盖，除非更高层引入
了不同的正常使用行为。除非已确认需求明确要求，不要发明对抗性、改写内部状态、权限、
不可变文件或不受支持平台的用例；这里的改写内部状态指制造不受支持的状态破坏，不包括
上述隔离测试前置的构造。

QA Review 可选建议由 engine 按 suggestion ID 接受用户处置，不在本动作调用旧推进命令。
采纳改变验收含义时由 engine 修订并重审受影响部分；普通代码返修不解冻既有 case-set。

共同说明只设计、审查一份，但每条未批准用例仍须逐条判断；共同说明修改后所有引用者
重新检查。对 source、验收点、case 的双向映射完整对账，不抽样或按组背书。
一次简单检查直接执行，重复步骤才写短脚本。脚本限于准备、真实调用、取证和断言；核对
它与已批准用例一致且不会漏报失败。缺陷回归须能发现原错误；关键成功/失败判断用已知
错误样例校验，不递归为全部测试写测试；同版本未改脚本无需重新完整设计审批。

`FROZEN` case-set 不接受普通 QA Design。只有需求含义、对外行为、成功/失败判据或原用例的
稳定可执行性变化经编排层合法 THAW 后，才设计受影响用例；普通代码返修和一次性环境不可用
都不属于 THAW 理由。

## 设计与执行分工

设计交付完整可照做的步骤、前置构造方法、取证方式和判据，不要求在设计阶段编写驱动脚本。
case.command 可以为 null；已有适用的可执行命令可直接声明 executable、args、expectedExitCode。
执行者按批准步骤在 qa.target 提交实际命令，仍经同一引擎绑定、执行和留证，不得改变用例或 oracle。
若设计已声明 command，执行者不得替换它。dependencies 只引用此前已有或本增量中更早的 case ID，
独立用例为 []；共用准备引用 sharedSteps，环境可用性写入前置，不作 case 依赖。

## 可执行共用准备

sharedSteps 的 command 使用与目标相同的 executable/args/expectedExitCode 声明；
文字 procedure 不自动执行，没有可执行步骤时 command 为 null。artifacts 列出需要复用
检查的产物相对文件路径。independent 为 true 表示准备本身是独立验收目标，必须实际
执行；否则相同有效输入共用完成的准备。case.sharedSteps 的引用顺序就是执行顺序。
普通只使用共用产物的用例选择 workspaceMode=isolated，只分配独立可变目录，
通过 FORMAL_GATES_QA_SHARED 读取共用源及产物；确实需要独立源码或 Git 工作树的
构建、安装、返修自测等选择 candidate。工具与依赖缓存沿用运行环境，不复制 HOME。
构建入口与前提以当前任务已绑定的材料为准。


# 黑盒设计

- **黑盒**：真实 QA。不关心代码是什么；只从「产品是什么、怎么用、需求是什么」出发，
  通过实际使用产品（文档化的正常使用方式操作产品、覆盖常见操作失误）验证需求是否完成
  并通过。黑盒设计在独立的基线工作区中进行，不包含本次开发变更；基线是否已有可运行
  产品及公开文档，应按项目实际情况核对。当前确认需求决定本次新增或变更的行为；未变
  接口沿用已有公开文档。公开文档与当前确认需求冲突时，按当前确认契约判断，并指出
  具体冲突，不能无条件认定基线文档过时。声称接口或公开说明缺失前，先用正常读取和
  搜索工具核对当前可读的公开文档；只读取需求或列出文件不能证明文档缺失。设计不读取
  本次开发实现或白盒材料；正式执行时使用引擎绑定的候选产品，保留实际运行证据。

黑盒仍必须通过文档化产品入口实际使用产物，不能以既有测试套件为主体或主要证据；
独立的短驱动脚本可调用公共入口、采集真实输出并断言。

**每一条黑盒用例都必须写出真实、完整、可照做的执行步骤。** 步骤必须给出实际使用
的公开入口、命令或调用、必要参数和输入、环境与 fixture 前置、操作顺序，以及需要留
存的输出或产物；不能只写场景名称、检查项、类别名或“执行相应测试”。设计阶段不能
运行产品，不等于可以省略执行细节；这些步骤必须能在快照后的真实产品上直接照做。

# 白盒设计

- **白盒**：开发后由独立代理读实现设计面向实现结构的测试（单元、系统、集成等职责式定
  义，不限定具体类型），在指定的白盒隔离工作区进行。结构测试充分性依赖实现，白盒设计阶段读
  取实现；开发前无实现可读，白盒设计只在开发后进行。

白盒用例先独立确定应验证的行为、分支和 oracle，再检查已有测试，复用真正有效的覆盖。
每条白盒用例用 testRef 定位实际测试（可引用独立子测试）；引用可用项目适用的测试标识，
例如 `<文件路径>::<函数名>`，不规定语言或解析格式。CLI 校验引用非空且唯一，QA Review
检查输入、断言和对应性，QA Execution 独立运行并保留输出。引擎不从 testRef 推断文件路径。

**白盒先独立判断覆盖，再复用已有测试。** 先列出应验证的行为、关键分支和 oracle，再读取
已有测试的输入、真实断言与覆盖对应。有效测试显式绑定并独立运行；仅缺口或错误才新增、
修正。普通逻辑优先单测，跨模块用集成测试，公开路径由黑盒承担；交叉验证须有不同验证
目标。不能仅凭测试名、覆盖率或开发者声明放行。没有测试变动时不增加文件或推进快照；
有变动则提交并纳入最终含测试快照。

白盒 design.testFiles 必须明确列出全部测试专用文件及测试专用 fixture 的项目相对路径，
包括开发者编写、直接复用而没有改动的测试，以及本轮新增、修改、删除的测试文件。
只列精确文件路径，不列目录或 glob；生产文件、生产配置及生产共用输入即使被测试引用，
也不能列入。允许在混合文件内新增、修正测试内容，但不得修改生产行为、生产配置或共用输入；
混合文件列入实际变更 manifest，不列入 testFiles，仍保留在实现门 diff 中。没有项目内测试专用文件时
显式返回 []。这是待独立白盒 review 核对的声明，不能单凭作者声明排除文件。
顶层 manifest 只列实际新增、修改、删除的路径；零改动复用时 manifest 为 []，testFiles
仍列出复用的测试文件。两者不得混用，不为标记测试归属修改已有文件。

**whitebox-design：** Inspect and independently run existing tests before adding only missing coverage. Use the available native shell tool in this assigned whitebox authoring workspace for test discovery and trial execution, including running newly written tests before reporting reuse. The qa.prepare/qa.target/qa.collect operations are for later frozen-case execution or repair and are unavailable during authoring. These trial results are preparation evidence, not formal QA results. Edit only test content or test-only fixtures. Tests may live inside production files: preserve all production behavior and shared inputs, list the mixed file in the change manifest, and never list it in design.testFiles. Return only actual changed file paths in the top-level manifest; declare all test-only file paths separately in design.testFiles, including unchanged reused developer tests. Return reuse records for each added or changed case with caseId/testRef/coverageJudgment/procedure/observation/comparison/result EXECUTED_PASS or EXECUTED_FAIL. If a valid test reveals a production failure, return status FAIL with the actual failure evidence and a concrete message; do not change the test to force a pass. Production fixes return to development. The isolated intermediate commit is for independent review, not approval to promote; the reviewer checks all declared testFiles and the complete whitebox diff before final candidate creation.

# 白盒覆盖判断

Independently enumerate required behavior and exact oracles from the bound requirement,
public contracts and candidate implementation. The engine provides the bound ordinary
candidate workspace for normal reading and searching; source bodies are not inlined or
copied into a filtered projection. Locate relevant implementation as needed without
assuming any language or directory layout. Before recording this independent judgment,
do not inspect existing tests, modify files, run tests or bind existing tests. This is
role sequencing, not a per-file read whitelist. Return the coverage JSON only. The engine
records it before the subsequent test inspection and gap-filling invocation.
