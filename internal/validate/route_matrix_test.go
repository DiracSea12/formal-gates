package validate

// route_matrix_test.go 是"交付义务反向普查"固化测试（阶段 0/1 交付义务补漏 run，
// 2026-08-22）。它机械落实三条防线：
//
//  1. refactor-plan/route-matrix.md（§2.3 双面矩阵）与 refactor-plan/stage-records.md
//     （§5 七项阶段记录）结构完备、必需列齐备、绑定取值在 §5.11 词汇表内；
//  2. 计划与矩阵双向锁定：§2.3 的命令枚举与测试内固定清单集合相等（计划增删命令时
//     测试同步失败，强制双向维护）；矩阵中超出枚举的行必须显式带"计划未枚举"标注；
//  3. 实际公开面 ⊆ 矩阵：从 internal/cli/cli.go 的 workflowSubcommands 注册表与顶层
//     命令 switch 源码解析派生实际入口清单，断言实现落地的每个入口都有矩阵行——
//     "先覆盖后实现"（用户拍板 2026-08-22）。
//
// 本测试只做结构断言，不复制矩阵的内容结论；行内容的真伪由矩阵内的证据指针溯源。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// §2.3 第一段枚举的全部 workflow 子命令（含 drive/submit）。与
// refactor-plan/incremental-seal-plan.md 的枚举双向锁定：任何一侧增删都会使
// TestPlanCommandEnumerationMatchesFixedLists 失败。
var fixedPlanWorkflowSubcommands = []string{
	"start", "show", "status", "next", "diagnose", "resume", "abort", "reset",
	"requirement", "route-candidates", "slicing", "settle-findings", "route",
	"route-add", "qa-worktree", "prepare-gate", "prepare-action",
	"claim-dispatch", "record-action", "record-gate", "qa-design", "qa-review",
	"qa-execution", "qa-execution-scope", "snapshot", "cleanup", "carry",
	"authorize-repair", "seal", "drive", "submit",
}

// §2.3 第二段枚举的维护/registry 面（lifecycle 与 registry 按动作粒度展开），
// 外加 §2.4 明文枚举的 `install --bootstrap` 公开维护动作。
var fixedPlanMaintenanceEntries = []string{
	"hook", "lifecycle capture", "lifecycle verify", "canary", "gate",
	"install", "install --bootstrap", "uninstall", "package",
	"registry admission", "registry register", "registry reconcile",
	"cutover", "rollback",
}

// §5.11 口径的阶段绑定词汇表。计划未枚举的补行只允许 legacy/unsupported。
var allowedStageBindings = map[string]bool{
	"legacy":            true,
	"install-bootstrap": true,
	"Shadow/诊断":         true,
	"unsupported":       true,
}

// stageRecordItems 是 §5"阶段记录至少保存"的七项标题（全列名）。阶段 0/1 两节
// 各自必须逐项出现，缺任何一项即 FAIL。
var stageRecordItems = []string{
	"### 1. 阶段编号、run ID、sealed commit 与主线集成 commit",
	"### 2. 包摘要、installed-target digest、state schema version、workflow definition version、definition digest",
	"### 3. 固定稳定插件摘要和候选安装摘要",
	"### 4. 本阶段公开能力矩阵与唯一 writer",
	"### 5. 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据",
	"### 6. 资源 cleanup receipt",
	"### 7. 下一阶段 worktree 的精确 post-integration canonical base 与关联 receipt",
}

func loadRepoFileForMatrixTest(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("required delivery artifact %s is missing: %v", rel, err)
	}
	return string(data)
}

// planEnumerationSegment 截取 plan 文档中 marker 之后、第一个句号之前的枚举段。
func planEnumerationSegment(t *testing.T, plan, marker string) string {
	t.Helper()
	idx := strings.Index(plan, marker)
	if idx < 0 {
		t.Fatalf("incremental-seal-plan.md no longer contains marker %q", marker)
	}
	segment := plan[idx:]
	if end := strings.Index(segment, "。"); end >= 0 {
		segment = segment[:end]
	}
	return segment
}

// planCommandTokens 抽出枚举段（marker 之后、第一个句号之前）内全部反引号 token。
// 用于把 §2.3/§2.4 的命令枚举与测试内固定清单做集合相等比较。
func planCommandTokens(t *testing.T, plan, marker string) []string {
	t.Helper()
	tokens := []string{}
	for _, raw := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(planEnumerationSegment(t, plan, marker), -1) {
		tokens = append(tokens, raw[1])
	}
	if len(tokens) == 0 {
		t.Fatalf("no backticked command tokens found after marker %q", marker)
	}
	return tokens
}

// normalizePlanEntryTokens 把枚举段的复合 token 展开为矩阵行使用的粒度：
// "lifecycle capture/verify" -> {lifecycle capture, lifecycle verify}；
// "admission/register/reconcile" -> {registry admission, registry register, registry reconcile}
// （registry 前缀来自枚举上下文"以及 registry `admission/...`"）。
func normalizePlanEntryTokens(t *testing.T, tokens []string) []string {
	t.Helper()
	out := []string{}
	for _, token := range tokens {
		if !strings.Contains(token, "/") {
			out = append(out, token)
			continue
		}
		parts := strings.Split(token, "/")
		switch {
		case strings.HasPrefix(token, "lifecycle"):
			// "lifecycle capture/verify" -> {lifecycle capture, lifecycle verify}
			out = append(out, parts[0])
			for _, p := range parts[1:] {
				out = append(out, "lifecycle "+p)
			}
		case parts[0] == "admission" || parts[0] == "register" || parts[0] == "reconcile":
			for _, p := range parts {
				out = append(out, "registry "+p)
			}
		default:
			t.Fatalf("unhandled compound plan token %q; extend the normalizer deliberately", token)
		}
	}
	return out
}

func sortedCopy(items []string) []string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	return sorted
}

func assertSameStringSet(t *testing.T, what string, want, got []string) {
	t.Helper()
	wantSet, gotSet := sortedCopy(want), sortedCopy(got)
	if !reflect.DeepEqual(wantSet, gotSet) {
		onlyWant, onlyGot := []string{}, []string{}
		wantMap := map[string]bool{}
		for _, w := range want {
			wantMap[w] = true
		}
		gotMap := map[string]bool{}
		for _, g := range got {
			gotMap[g] = true
		}
		for _, w := range wantSet {
			if !gotMap[w] {
				onlyWant = append(onlyWant, w)
			}
		}
		for _, g := range gotSet {
			if !wantMap[g] {
				onlyGot = append(onlyGot, g)
			}
		}
		t.Fatalf("%s mismatch: missing-from-got=%v extra-in-got=%v (sets must stay in sync in both the plan and this test)", what, onlyWant, onlyGot)
	}
}

// TestPlanCommandEnumerationMatchesFixedLists 双向锁定：计划文档 §2.3 的命令枚举与
// 本测试的固定清单必须集合相等。计划增删命令、或测试清单失同步，都会失败。
func TestPlanCommandEnumerationMatchesFixedLists(t *testing.T) {
	plan := loadRepoFileForMatrixTest(t, "refactor-plan/incremental-seal-plan.md")

	workflowTokens := planCommandTokens(t, plan, "全部 workflow 子命令：")
	workflowEntries := []string{}
	for _, token := range workflowTokens {
		// "drive/submit" 是 §2.3 对两个最终面命令的合写。
		for _, part := range strings.Split(token, "/") {
			workflowEntries = append(workflowEntries, part)
		}
	}
	assertSameStringSet(t, "§2.3 workflow enumeration vs fixed test list", fixedPlanWorkflowSubcommands, workflowEntries)

	maintenanceTokens := planCommandTokens(t, plan, "top-level 维护/transport 面：")
	maintenanceEntries := normalizePlanEntryTokens(t, maintenanceTokens)
	// §2.3 维护枚举段未含 cutover/rollback（未加反引号），但同句明文点名；§2.4 另明文
	// 枚举 `install --bootstrap` 公开维护动作。二者都属于计划内条目。
	segment := planEnumerationSegment(t, plan, "top-level 维护/transport 面：")
	for _, bare := range []string{"cutover", "rollback"} {
		if !strings.Contains(segment, bare) {
			t.Fatalf("§2.3 maintenance enumeration segment no longer names %q", bare)
		}
	}
	if !strings.Contains(plan, "`install --bootstrap`") {
		t.Fatalf("plan no longer enumerates the `install --bootstrap` public maintenance action")
	}
	assertSameStringSet(t, "§2.3/§2.4 maintenance enumeration vs fixed test list",
		fixedPlanMaintenanceEntries, append(maintenanceEntries, "cutover", "rollback", "install --bootstrap"))
}

// cliWorkflowSubcommandsFromSource 从 internal/cli/cli.go 的 workflowSubcommands 注册表
// 字面量源码解析实际注册的子命令清单（源码解析派生，新增子命令必须与矩阵同批变更）。
func cliWorkflowSubcommandsFromSource(t *testing.T) []string {
	t.Helper()
	src := loadRepoFileForMatrixTest(t, "internal/cli/cli.go")
	start := strings.Index(src, "var workflowSubcommands = map[string]func(")
	if start < 0 {
		t.Fatal("internal/cli/cli.go no longer declares the workflowSubcommands registry; update this reverse-census test deliberately")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}"); end < 0 {
		t.Fatal("could not find the end of the workflowSubcommands registry literal")
	} else {
		body = body[:end]
	}
	names := []string{}
	for _, match := range regexp.MustCompile(`(?m)^\s*"([a-z0-9-]+)":`).FindAllStringSubmatch(body, -1) {
		names = append(names, match[1])
	}
	if len(names) == 0 {
		t.Fatal("parsed an empty workflowSubcommands registry; the source-format assumption is stale")
	}
	return names
}

// cliTopLevelCommandsFromSource 从 run() 的顶层命令 switch 解析实际公开的 top-level
// 命令（package/registry/install/uninstall/workflow/hook/lifecycle/gate/canary）。
func cliTopLevelCommandsFromSource(t *testing.T) []string {
	t.Helper()
	src := loadRepoFileForMatrixTest(t, "internal/cli/cli.go")
	runIdx := strings.Index(src, "func run(program string, args []string, streams IO) (int, error) {")
	if runIdx < 0 {
		t.Fatal("internal/cli/cli.go no longer contains the top-level run dispatcher")
	}
	rest := src[runIdx:]
	switchIdx := strings.Index(rest, "switch args[0] {")
	if switchIdx < 0 {
		t.Fatal("top-level command switch not found in run()")
	}
	body := rest[switchIdx:]
	if end := strings.Index(body, "default:"); end >= 0 {
		body = body[:end]
	}
	names := []string{}
	for _, match := range regexp.MustCompile(`(?m)^\tcase "([a-z]+)":`).FindAllStringSubmatch(body, -1) {
		names = append(names, match[1])
	}
	if len(names) == 0 {
		t.Fatal("parsed an empty top-level command switch")
	}
	return names
}

// mdTable 是从 markdown 解析出的表格：header 为列名，rows 为数据行单元格。
type mdTable struct {
	header []string
	rows   [][]string
}

// parseMarkdownTableInSection 定位 section 标题之后的第一张表格并解析。
func parseMarkdownTableInSection(t *testing.T, content, sectionMarker, artifact string) mdTable {
	t.Helper()
	idx := strings.Index(content, sectionMarker)
	if idx < 0 {
		t.Fatalf("%s is missing section %q", artifact, sectionMarker)
	}
	var tableLines []string
	started := false
	for _, line := range strings.Split(content[idx:], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") {
			started = true
			tableLines = append(tableLines, trimmed)
			continue
		}
		if started {
			break
		}
	}
	if len(tableLines) == 0 {
		t.Fatalf("%s section %q has no markdown table", artifact, sectionMarker)
	}
	splitCells := func(line string) []string {
		cells := strings.Split(line, "|")
		if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
			cells = cells[1:]
		}
		if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
			cells = cells[:len(cells)-1]
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		return cells
	}
	table := mdTable{header: splitCells(tableLines[0])}
	for _, line := range tableLines[1:] {
		cells := splitCells(line)
		separator := len(cells) > 0
		for _, cell := range cells {
			if cell == "" || !strings.HasPrefix(cell, "-") {
				separator = false
				break
			}
		}
		if separator {
			continue
		}
		if len(cells) != len(table.header) {
			t.Fatalf("%s section %q: row %q has %d cells, header has %d", artifact, sectionMarker, line, len(cells), len(table.header))
		}
		table.rows = append(table.rows, cells)
	}
	if len(table.rows) == 0 {
		t.Fatalf("%s section %q table has no data rows", artifact, sectionMarker)
	}
	return table
}

func (table mdTable) column(t *testing.T, name, artifact, section string) int {
	t.Helper()
	for i, header := range table.header {
		if header == name {
			return i
		}
	}
	t.Fatalf("%s section %q table lacks required column %q (full §2.3 column names are mandatory)", artifact, section, name)
	return -1
}

// matrixRowEntry 规范化行入口名：去掉中文括号补充说明后缀。
func matrixRowEntry(cell string) string {
	if idx := strings.Index(cell, "（"); idx >= 0 {
		return strings.TrimSpace(cell[:idx])
	}
	return cell
}

func checkStageBindingCells(t *testing.T, artifact, entry string, stage0, stage1, planEnum string) {
	t.Helper()
	for label, value := range map[string]string{"阶段 0 绑定": stage0, "阶段 1 绑定": stage1} {
		if !allowedStageBindings[value] {
			t.Fatalf("%s row %q has %s=%q outside the §5.11 vocabulary {legacy, install-bootstrap, Shadow/诊断, unsupported}", artifact, entry, label, value)
		}
	}
	if planEnum != "计划内" && planEnum != "计划未枚举" {
		t.Fatalf("%s row %q has 计划枚举=%q; must be exactly 计划内 or 计划未枚举", artifact, entry, planEnum)
	}
	if planEnum == "计划未枚举" {
		for label, value := range map[string]string{"阶段 0 绑定": stage0, "阶段 1 绑定": stage1} {
			if value != "legacy" && value != "unsupported" {
				t.Fatalf("%s row %q is marked 计划未枚举 but %s=%q; supplementary rows only allow legacy/unsupported", artifact, entry, label, value)
			}
		}
	}
}

func requireNonEmptyCells(t *testing.T, artifact, section, entry string, cells map[string]string) {
	t.Helper()
	for column, value := range cells {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s section %q row %q has empty required column %q", artifact, section, entry, column)
		}
	}
}

// TestRouteMatrixWorkflowFace 机械断言 §2.3 第一段口径：矩阵覆盖全部枚举子命令与
// 实际注册的子命令、每行必需列齐备、绑定取值合法、unsupported 行不得实际实现。
func TestRouteMatrixWorkflowFace(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	const section = "## workflow 面"
	content := loadRepoFileForMatrixTest(t, artifact)
	table := parseMarkdownTableInSection(t, content, section, artifact)

	entryCol := table.column(t, "入口", artifact, section)
	runtimeCol := table.column(t, "runtime", artifact, section)
	writerCol := table.column(t, "唯一 writer", artifact, section)
	versionCol := table.column(t, "schema/definition 版本绑定", artifact, section)
	transitionCol := table.column(t, "允许的状态变化", artifact, section)
	errorCol := table.column(t, "错误码", artifact, section)
	readOnlyCol := table.column(t, "是否只读", artifact, section)
	stage0Col := table.column(t, "阶段 0 绑定", artifact, section)
	stage1Col := table.column(t, "阶段 1 绑定", artifact, section)
	planEnumCol := table.column(t, "计划枚举", artifact, section)
	evidenceCol := table.column(t, "机器证据", artifact, section)

	fixed := map[string]bool{}
	for _, name := range fixedPlanWorkflowSubcommands {
		fixed[name] = true
	}
	actual := map[string]bool{}
	for _, name := range cliWorkflowSubcommandsFromSource(t) {
		actual[name] = true
	}

	matrixEntries := map[string]bool{}
	for _, row := range table.rows {
		entry := matrixRowEntry(row[entryCol])
		if matrixEntries[entry] {
			t.Fatalf("%s has duplicate workflow row %q", artifact, entry)
		}
		matrixEntries[entry] = true
		requireNonEmptyCells(t, artifact, section, entry, map[string]string{
			"入口":                     row[entryCol],
			"runtime":                row[runtimeCol],
			"唯一 writer":              row[writerCol],
			"schema/definition 版本绑定": row[versionCol],
			"允许的状态变化":                row[transitionCol],
			"错误码":                    row[errorCol],
			"是否只读":                   row[readOnlyCol],
			"机器证据":                   row[evidenceCol],
		})
		checkStageBindingCells(t, artifact, entry, row[stage0Col], row[stage1Col], row[planEnumCol])
		if fixed[entry] && row[planEnumCol] != "计划内" {
			t.Fatalf("workflow row %q is enumerated by §2.3 but is marked %q", entry, row[planEnumCol])
		}
		if !fixed[entry] && row[planEnumCol] != "计划未枚举" {
			t.Fatalf("workflow row %q is beyond the §2.3 enumeration and must carry the 计划未枚举 marker", entry)
		}
		// §2.3：不支持的入口必须显式拒绝或不存在，不得缺省冒充——unsupported 行不得
		// 出现在实际注册表中；反之实际注册的入口不得绑 unsupported。
		isUnsupported := row[stage0Col] == "unsupported" && row[stage1Col] == "unsupported"
		if isUnsupported && actual[entry] {
			t.Fatalf("workflow row %q is bound unsupported but the subcommand is actually registered in workflowSubcommands", entry)
		}
	}

	// 覆盖义务：§2.3 全部枚举（含 drive/submit）都有矩阵行。
	for _, name := range fixedPlanWorkflowSubcommands {
		if !matrixEntries[name] {
			t.Fatalf("%s is enumerated by §2.3 but has no workflow matrix row", name)
		}
	}
	// 反向普查：实际公开面 ⊆ 矩阵——实现落地的入口不得缺席矩阵。
	for _, name := range cliWorkflowSubcommandsFromSource(t) {
		if !matrixEntries[name] {
			t.Fatalf("workflow subcommand %q is registered in internal/cli/cli.go but missing from %s (new public surfaces must enter the matrix in the same change)", name, artifact)
		}
	}
}

// TestRouteMatrixMaintenanceFace 机械断言 §2.3 第二段口径：维护/registry/cutover/
// rollback 面条目齐备、每行必需列（分类/owner/token/receipt/恢复/权限）齐备、
// top-level 命令都有矩阵归属。
func TestRouteMatrixMaintenanceFace(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	const section = "## 维护/transport 面"
	content := loadRepoFileForMatrixTest(t, artifact)
	table := parseMarkdownTableInSection(t, content, section, artifact)

	entryCol := table.column(t, "入口", artifact, section)
	categoryCol := table.column(t, "分类", artifact, section)
	ownerCol := table.column(t, "owner", artifact, section)
	tokenCol := table.column(t, "generation/token", artifact, section)
	receiptCol := table.column(t, "receipt schema", artifact, section)
	recoveryCol := table.column(t, "恢复入口", artifact, section)
	boundaryCol := table.column(t, "权限边界", artifact, section)
	stage0Col := table.column(t, "阶段 0 绑定", artifact, section)
	stage1Col := table.column(t, "阶段 1 绑定", artifact, section)
	planEnumCol := table.column(t, "计划枚举", artifact, section)
	evidenceCol := table.column(t, "机器证据", artifact, section)

	fixed := map[string]bool{}
	for _, name := range fixedPlanMaintenanceEntries {
		fixed[name] = true
	}

	firstWords := map[string]bool{}
	for _, row := range table.rows {
		entry := matrixRowEntry(row[entryCol])
		if firstWords[entry] {
			t.Fatalf("%s has duplicate maintenance row %q", artifact, entry)
		}
		firstWords[entry] = true
		requireNonEmptyCells(t, artifact, section, entry, map[string]string{
			"入口":               row[entryCol],
			"分类":               row[categoryCol],
			"owner":            row[ownerCol],
			"generation/token": row[tokenCol],
			"receipt schema":   row[receiptCol],
			"恢复入口":             row[recoveryCol],
			"权限边界":             row[boundaryCol],
			"机器证据":             row[evidenceCol],
		})
		checkStageBindingCells(t, artifact, entry, row[stage0Col], row[stage1Col], row[planEnumCol])
		if fixed[entry] && row[planEnumCol] != "计划内" {
			t.Fatalf("maintenance row %q is enumerated by §2.3/§2.4 but is marked %q", entry, row[planEnumCol])
		}
		if !fixed[entry] && row[planEnumCol] != "计划未枚举" {
			t.Fatalf("maintenance row %q is beyond the plan enumeration and must carry the 计划未枚举 marker", entry)
		}
	}

	for _, name := range fixedPlanMaintenanceEntries {
		if !firstWords[name] {
			t.Fatalf("%s is enumerated by the plan but has no maintenance matrix row", name)
		}
	}
	// 反向普查：top-level 命令 switch 中每个实际命令都有矩阵归属（workflow 面由
	// workflow 表承载，其余按行入口首词归属维护面）。
	for _, command := range cliTopLevelCommandsFromSource(t) {
		if command == "workflow" {
			continue
		}
		covered := false
		for entry := range firstWords {
			if strings.HasPrefix(entry, command) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("top-level command %q is dispatched in internal/cli/cli.go but has no row in %s", command, artifact)
		}
	}
}

// TestStageRecordsContainSevenItemsPerStage 机械断言 §5 七项清单在阶段 0/1 两节中
// 逐项齐备（全列名标题）。
func TestStageRecordsContainSevenItemsPerStage(t *testing.T) {
	const artifact = "refactor-plan/stage-records.md"
	content := loadRepoFileForMatrixTest(t, artifact)
	for _, stage := range []string{"## 阶段 0", "## 阶段 1"} {
		idx := strings.Index(content, stage)
		if idx < 0 {
			t.Fatalf("%s is missing stage section %q", artifact, stage)
		}
		section := content[idx+len(stage):]
		if next := strings.Index(section, "\n## "); next >= 0 {
			section = section[:next]
		}
		for _, item := range stageRecordItems {
			if !strings.Contains(section, item) {
				t.Fatalf("%s section %q is missing required §5 item heading %q", artifact, stage, item)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// QA 白盒结构测试（dispatch-c39ac70c0305fec7e7ce8641，2026-08-22）。
//
// 以下测试由 QA 白盒设计轮独立设计并交付，不从上文开发侧反向普查测试出发，
// 而是从已确认需求（RQ-1 命令路由矩阵 / RQ-2 阶段记录汇编 / RQ-3 反向普查固化）
// 与实现事实出发独立规定应覆盖的结构行为与边界：
//
//   - 验收 2 的自证：人为删改矩阵/记录/计划枚举任一必需项时反向普查必须 FAIL；
//   - RQ-1 的 start 行 --split legacy 事实与 cli.go 实现双向绑定；
//   - RQ-1/验收 3 的机器证据可解析性（CASE-nnn / 测试函数 / *_test.go 真实存在，
//     无引用即必须显式标注证据缺口）；
//   - 验收 3 的引用符号真实存在与阶段 0/1 无"委托 engine submit"面；
//   - RQ-1 的 unsupported/内部 owner 面经真实 CLI 公共入口显式拒绝且零写入；
//   - "先覆盖后实现"在公共帮助面上的一致性（usage = 注册表 ⊆ 矩阵）；
//   - RQ-2 的阶段记录溯源指针与 git 历史、封板 JSON、digest 常量逐项对账。
// ---------------------------------------------------------------------------

// qaRunValidateChildTest 在子进程中重跑本包的一个测试并返回 (输出, 是否通过)。
// 用于"删改交付实物后反向普查测试必须失败"的自证：子进程重新从仓库读取被
// 暂时改写的实物文件，与父进程的内存状态无关。
func qaRunValidateChildTest(t *testing.T, testName string) (string, bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^"+testName+"$", "-test.v")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// qaWithMutatedRepoFile 用 mutate 暂时改写仓库内 rel 文件，测试结束后逐字节恢复
// 并核对恢复结果。改写失败（含文件不可写）直接 FAIL，不得静默跳过自证。
func qaWithMutatedRepoFile(t *testing.T, rel string, mutate func(string) (string, bool)) {
	t.Helper()
	path := filepath.Join(repoRootValidateTest(t), filepath.FromSlash(rel))
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s for the mutation fixture: %v", rel, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mutated, ok := mutate(string(orig))
	if !ok {
		t.Fatalf("mutation fixture for %s did not match the delivered content", rel)
	}
	if err := os.WriteFile(path, []byte(mutated), info.Mode()); err != nil {
		t.Fatalf("write mutated %s: %v", rel, err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(path, orig, info.Mode()); err != nil {
			t.Errorf("restore %s after mutation: %v", rel, err)
			return
		}
		now, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(now, orig) {
			t.Errorf("restore of %s is not byte-identical to the original", rel)
		}
	})
}

// qaFindTableRowLine 返回以 rowPrefix 开口的第一条表格行。
func qaFindTableRowLine(content, rowPrefix string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, rowPrefix) {
			return line, true
		}
	}
	return "", false
}

// qaReplaceTableRowCell 替换一条 markdown 表格行中 split-by-"|" 意义下的第
// cellIndex 个单元格（首尾空单元格计数在内：入口=1，runtime=2，唯一 writer=3，
// …，阶段 0 绑定=8）。
func qaReplaceTableRowCell(line string, cellIndex int, value string) string {
	cells := strings.Split(line, "|")
	cells[cellIndex] = " " + value + " "
	return strings.Join(cells, "|")
}

// TestRouteMatrixReverseCensusFailsOnDeliveredArtifactMutations 落实验收标准 2 的
// 自证义务：人为删改矩阵/阶段记录/计划枚举中的任一必需项时，反向普查测试必须
// FAIL。每个子测试在干净子进程中重跑对应的开发侧反向普查测试；先做正向对照
// （未删改时必须通过，防止子进程通道自身损坏造成空洞通过），再逐项删改并断言
// 失败与失败信息指向被删改的必需项。
func TestRouteMatrixReverseCensusFailsOnDeliveredArtifactMutations(t *testing.T) {
	if out, passed := qaRunValidateChildTest(t, "TestRouteMatrixWorkflowFace"); !passed {
		t.Fatalf("positive control: TestRouteMatrixWorkflowFace must pass on the delivered matrix, output:\n%s", out)
	}

	t.Run("workflow face fails when an enumerated row is deleted", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/route-matrix.md", func(content string) (string, bool) {
			line, ok := qaFindTableRowLine(content, "| seal |")
			if !ok {
				return "", false
			}
			return strings.Replace(content, line+"\n", "", 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestRouteMatrixWorkflowFace")
		if passed {
			t.Fatalf("deleting the §2.3-enumerated `seal` row must fail TestRouteMatrixWorkflowFace, output:\n%s", out)
		}
		for _, sentinel := range []string{"seal", "no workflow matrix row"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})

	t.Run("workflow face fails when a required cell is emptied", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/route-matrix.md", func(content string) (string, bool) {
			line, ok := qaFindTableRowLine(content, "| show |")
			if !ok {
				return "", false
			}
			return strings.Replace(content, line, qaReplaceTableRowCell(line, 3, " "), 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestRouteMatrixWorkflowFace")
		if passed {
			t.Fatalf("emptying the 唯一 writer cell of the `show` row must fail TestRouteMatrixWorkflowFace, output:\n%s", out)
		}
		for _, sentinel := range []string{"show", "empty required column"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})

	t.Run("workflow face fails on an out-of-vocabulary stage binding", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/route-matrix.md", func(content string) (string, bool) {
			line, ok := qaFindTableRowLine(content, "| resume |")
			if !ok {
				return "", false
			}
			return strings.Replace(content, line, qaReplaceTableRowCell(line, 8, "engine"), 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestRouteMatrixWorkflowFace")
		if passed {
			t.Fatalf("rewriting the resume row's 阶段 0 绑定 to `engine` must fail TestRouteMatrixWorkflowFace, output:\n%s", out)
		}
		for _, sentinel := range []string{"resume", "§5.11 vocabulary"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})

	t.Run("workflow face fails on an unmarked supplementary row", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/route-matrix.md", func(content string) (string, bool) {
			line, ok := qaFindTableRowLine(content, "| future |")
			if !ok {
				return "", false
			}
			extra := "| probe | legacy | 无 | 无 | 无 | 无 | 否 | legacy | legacy | 计划内 | 反向普查自证 |"
			return strings.Replace(content, line+"\n", line+"\n"+extra+"\n", 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestRouteMatrixWorkflowFace")
		if passed {
			t.Fatalf("a matrix row beyond the §2.3 enumeration without the 计划未枚举 marker must fail TestRouteMatrixWorkflowFace, output:\n%s", out)
		}
		for _, sentinel := range []string{"probe", "beyond the §2.3 enumeration"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})

	t.Run("stage records fail when a seven-item heading is corrupted", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/stage-records.md", func(content string) (string, bool) {
			if !strings.Contains(content, "### 6. 资源 cleanup receipt") {
				return "", false
			}
			return strings.Replace(content, "### 6. 资源 cleanup receipt", "### 6. 资源 cleanup（被删改）", 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestStageRecordsContainSevenItemsPerStage")
		if passed {
			t.Fatalf("corrupting a §5 seven-item heading must fail TestStageRecordsContainSevenItemsPerStage, output:\n%s", out)
		}
		for _, sentinel := range []string{"资源 cleanup receipt", "is missing required §5 item heading"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})

	t.Run("plan enumeration lock fails when the plan drops a command", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/incremental-seal-plan.md", func(content string) (string, bool) {
			if !strings.Contains(content, "、`reset`") {
				return "", false
			}
			return strings.Replace(content, "、`reset`", "", 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestPlanCommandEnumerationMatchesFixedLists")
		if passed {
			t.Fatalf("removing `reset` from the plan's §2.3 enumeration must fail TestPlanCommandEnumerationMatchesFixedLists, output:\n%s", out)
		}
		if !strings.Contains(out, "reset") {
			t.Fatalf("expected failure output to mention the dropped command, output:\n%s", out)
		}
	})

	t.Run("maintenance face fails when a plan-enumerated row is deleted", func(t *testing.T) {
		qaWithMutatedRepoFile(t, "refactor-plan/route-matrix.md", func(content string) (string, bool) {
			line, ok := qaFindTableRowLine(content, "| registry reconcile |")
			if !ok {
				return "", false
			}
			return strings.Replace(content, line+"\n", "", 1), true
		})
		out, passed := qaRunValidateChildTest(t, "TestRouteMatrixMaintenanceFace")
		if passed {
			t.Fatalf("deleting the plan-enumerated `registry reconcile` row must fail TestRouteMatrixMaintenanceFace, output:\n%s", out)
		}
		for _, sentinel := range []string{"registry reconcile", "no maintenance matrix row"} {
			if !strings.Contains(out, sentinel) {
				t.Fatalf("expected failure output to mention %q, output:\n%s", sentinel, out)
			}
		}
	})
}

// TestRouteMatrixStartRowSplitLegacyFactMatchesCLISource 落实 RQ-1 的明文口径：
// 矩阵 start 行必须注明"legacy start 仍带 --split、属 legacy 维持项、阶段 3 迁移
// 时改写"的当前事实，绑定结论节必须登记该维持现状，且该事实与 internal/cli/cli.go
// 的实际实现一致（runWorkflowStart 仍注册 --split 旗标）——文档与实现任何一侧
// 漂移都 FAIL。
func TestRouteMatrixStartRowSplitLegacyFactMatchesCLISource(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	const section = "## workflow 面"
	content := loadRepoFileForMatrixTest(t, artifact)
	table := parseMarkdownTableInSection(t, content, section, artifact)
	entryCol := table.column(t, "入口", artifact, section)
	transitionCol := table.column(t, "允许的状态变化", artifact, section)

	var startCell string
	found := false
	for _, row := range table.rows {
		if matrixRowEntry(row[entryCol]) == "start" {
			startCell = row[transitionCol]
			found = true
		}
	}
	if !found {
		t.Fatalf("%s has no `start` workflow row", artifact)
	}
	for _, fragment := range []string{"--split", "legacy 维持", "阶段 3"} {
		if !strings.Contains(startCell, fragment) {
			t.Fatalf("start row's 允许的状态变化 cell must state the --split legacy fact including %q, got: %s", fragment, startCell)
		}
	}

	conclusionIdx := strings.Index(content, "## 阶段 0/1 绑定结论")
	if conclusionIdx < 0 {
		t.Fatalf("%s is missing the 阶段 0/1 绑定结论 section", artifact)
	}
	conclusion := content[conclusionIdx:]
	if !strings.Contains(conclusion, "--split") || !strings.Contains(conclusion, "维持现状") {
		t.Fatal("the 阶段 0/1 绑定结论 section no longer registers the --split 维持现状 fact")
	}

	cliSrc := loadRepoFileForMatrixTest(t, "internal/cli/cli.go")
	startIdx := strings.Index(cliSrc, "func runWorkflowStart(")
	if startIdx < 0 {
		t.Fatal("internal/cli/cli.go no longer contains runWorkflowStart")
	}
	body := cliSrc[startIdx:]
	if end := strings.Index(body, "\nfunc "); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, `fs.String("split"`) {
		t.Fatal("the matrix start row claims legacy start still carries --split, but runWorkflowStart no longer registers the split flag")
	}
}

// qaPhaseCasesJSON 是封板 run JSON 中 qaExecution 案例清单的最小投影。
type qaPhaseCasesJSON struct {
	QaExecution struct {
		Cases []struct {
			CaseID string `json:"caseId"`
		} `json:"cases"`
	} `json:"qaExecution"`
}

// qaPhaseJSONCaseIDs 汇总两份封板 run JSON 的全部 caseId。
func qaPhaseJSONCaseIDs(t *testing.T) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, rel := range []string{
		".gates/results/phase-0-distribution-002.json",
		".gates/results/phase-1-decision-kernel.json",
	} {
		data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("sealed phase result %s is missing: %v", rel, err)
		}
		var doc qaPhaseCasesJSON
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		if len(doc.QaExecution.Cases) == 0 {
			t.Fatalf("%s has no qaExecution cases", rel)
		}
		for _, c := range doc.QaExecution.Cases {
			ids[c.CaseID] = true
		}
	}
	return ids
}

// qaRepoTestIndex 遍历仓库全部 *_test.go（含带构建标签的测试文件），收集已声明
// 的测试函数名与测试文件基名，作为引用解析的仓库侧索引。
func qaRepoTestIndex(t *testing.T) (funcs map[string]bool, basenames map[string]bool) {
	t.Helper()
	funcs, basenames = map[string]bool{}, map[string]bool{}
	root := repoRootValidateTest(t)
	declRe := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		basenames[d.Name()] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range declRe.FindAllStringSubmatch(string(data), -1) {
			funcs[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo test files: %v", err)
	}
	if len(funcs) == 0 || len(basenames) == 0 {
		t.Fatal("repo test index is empty; the source-format assumption is stale")
	}
	return funcs, basenames
}

// TestRouteMatrixMachineEvidenceCitationsResolveInRepo 落实 RQ-1 的 negative-tests
// 证据口径与验收 3：矩阵与阶段记录引用的每条机器证据都必须在仓库中真实存在——
// CASE-nnn 必须是封板 run JSON 的真实案例、测试函数必须由某个 *_test.go 声明、
// *_test.go 文件引用必须真实存在；机器证据列不得既无可解析引用又无"证据缺口"
// 标注（不得静默留空）。
func TestRouteMatrixMachineEvidenceCitationsResolveInRepo(t *testing.T) {
	matrix := loadRepoFileForMatrixTest(t, "refactor-plan/route-matrix.md")
	records := loadRepoFileForMatrixTest(t, "refactor-plan/stage-records.md")
	docs := map[string]string{
		"refactor-plan/route-matrix.md":  matrix,
		"refactor-plan/stage-records.md": records,
	}

	caseIDs := qaPhaseJSONCaseIDs(t)
	testFuncs, testBasenames := qaRepoTestIndex(t)

	caseRe := regexp.MustCompile(`CASE-\d+`)
	testRe := regexp.MustCompile(`Test[A-Za-z0-9_]+`)
	fileRe := regexp.MustCompile(`[A-Za-z0-9_]+_test\.go`)

	for artifact, content := range docs {
		for _, cited := range caseRe.FindAllString(content, -1) {
			if !caseIDs[cited] {
				t.Errorf("%s cites %s which is not a case in any sealed phase JSON", artifact, cited)
			}
		}
		for _, cited := range testRe.FindAllString(content, -1) {
			if !testFuncs[cited] {
				t.Errorf("%s cites test %q which is not declared by any *_test.go in the repo", artifact, cited)
			}
		}
		for _, loc := range fileRe.FindAllStringIndex(content, -1) {
			// "a/b_test.go" 形式的行内合写（如 round12/15 合写）是文档速记而非
			// 文件引用，跳过；其余 *_test.go token 必须真实存在。
			if loc[0] > 0 && content[loc[0]-1] == '/' {
				continue
			}
			cited := content[loc[0]:loc[1]]
			if !testBasenames[cited] {
				t.Errorf("%s cites test file %q which does not exist in the repo", artifact, cited)
			}
		}
	}

	for _, section := range []string{"## workflow 面", "## 维护/transport 面"} {
		table := parseMarkdownTableInSection(t, matrix, section, "refactor-plan/route-matrix.md")
		for _, row := range table.rows {
			cell := row[len(table.header)-1]
			hasCitation := caseRe.MatchString(cell) || testRe.MatchString(cell) || fileRe.MatchString(cell)
			if !hasCitation && !strings.Contains(cell, "证据缺口") {
				t.Errorf("%s row %q: the 机器证据 cell has no resolvable citation and no 证据缺口 marker: %q", section, matrixRowEntry(row[0]), cell)
			}
		}
	}
}

// TestRouteMatrixCitedWriterSymbolsExistAndNoEngineSubmitFaceInStage01 落实验收 3
// （矩阵内容与仓库事实一致，不得出现与实现相反的行）：矩阵正文引用的每个
// validate./lifecycle. 符号都必须在对应包源码中真实声明；维护面 分类 必须落在
// §2.3 口径（只读 / 只写外部 observation/receipt / 委托 engine submit / 内部
// owner handler / 显式"无（"）内，且阶段 0/1 不得出现任何"委托 engine submit"
// 入口（engine submit 未实现，公开冒充即与实现相反）。
func TestRouteMatrixCitedWriterSymbolsExistAndNoEngineSubmitFaceInStage01(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	content := loadRepoFileForMatrixTest(t, artifact)

	declared := map[string]bool{}
	declRe := regexp.MustCompile(`(?m)^(?:func|type)\s+([A-Za-z][A-Za-z0-9]*)`)
	for _, pkgDir := range []string{"internal/validate", "internal/lifecycle"} {
		entries, err := os.ReadDir(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(pkgDir)))
		if err != nil {
			t.Fatalf("list %s: %v", pkgDir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(pkgDir), name))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range declRe.FindAllStringSubmatch(string(data), -1) {
				declared[m[1]] = true
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("symbol declaration index is empty; the source-format assumption is stale")
	}
	for _, m := range regexp.MustCompile(`(?:validate|lifecycle)\.([A-Z][A-Za-z0-9]*)`).FindAllStringSubmatch(content, -1) {
		if !declared[m[1]] {
			t.Errorf("%s cites %s which is not declared in internal/validate or internal/lifecycle source", artifact, m[0])
		}
	}

	const section = "## 维护/transport 面"
	table := parseMarkdownTableInSection(t, content, section, artifact)
	categoryCol := table.column(t, "分类", artifact, section)
	knownMarkers := []string{"只读", "只写外部", "委托 engine submit", "内部 owner handler"}
	for _, row := range table.rows {
		entry := matrixRowEntry(row[0])
		cell := row[categoryCol]
		if strings.Contains(cell, "委托 engine submit") {
			t.Errorf("maintenance row %q is classified 委托 engine submit, but stage 0/1 must not expose any engine submit face", entry)
		}
		inVocabulary := strings.HasPrefix(cell, "无（")
		for _, marker := range knownMarkers {
			if strings.Contains(cell, marker) {
				inVocabulary = true
			}
		}
		if !inVocabulary {
			t.Errorf("maintenance row %q 分类 cell %q is outside the §2.3 categories", entry, cell)
		}
	}
}

// qaBuildRouteMatrixCLIBinary 从当前源码构建真实 CLI（./cmd/formal-gates）到测试
// 临时目录，作为公共入口探测的载体。
func qaBuildRouteMatrixCLIBinary(t *testing.T) string {
	t.Helper()
	name := "formal-gates-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binPath, "./cmd/formal-gates")
	build.Dir = repoRootValidateTest(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ./cmd/formal-gates: %v\n%s", err, out)
	}
	return binPath
}

// qaRunRouteMatrixCLI 在空临时工作目录 + 隔离 HOME 中运行 CLI，返回合并输出与
// 退出码，并断言工作目录保持为空（拒绝/帮助路径不得有任何写入）。
func qaRunRouteMatrixCLI(t *testing.T, binPath string, args ...string) (string, int) {
	t.Helper()
	workDir := t.TempDir()
	homeDir := t.TempDir()
	var out bytes.Buffer
	cmd := exec.Command(binPath, args...)
	cmd.Dir = workDir
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run CLI %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	entries, readErr := os.ReadDir(workDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("CLI %v must not write anything on this path, found: %v", args, entries)
	}
	return out.String(), code
}

// TestRouteMatrixUnsupportedFacesExplicitlyRejectedByPublicCLI 落实 RQ-1：unsupported
// 面"必须显式拒绝或不存在，不得缺省冒充"，内部 owner handler 面"CLI 无独立入口"。
// 从矩阵双面派生全部 unsupported 行与内部 owner handler 行，经真实 CLI 公共入口
// 逐一探测：显式拒绝消息、rc=1、拒绝路径零写入（空工作目录不出现任何条目）。
func TestRouteMatrixUnsupportedFacesExplicitlyRejectedByPublicCLI(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	content := loadRepoFileForMatrixTest(t, artifact)
	binPath := qaBuildRouteMatrixCLIBinary(t)

	const wfSection = "## workflow 面"
	workflowTable := parseMarkdownTableInSection(t, content, wfSection, artifact)
	entryCol := workflowTable.column(t, "入口", artifact, wfSection)
	stage0Col := workflowTable.column(t, "阶段 0 绑定", artifact, wfSection)
	stage1Col := workflowTable.column(t, "阶段 1 绑定", artifact, wfSection)

	unsupportedWorkflow := []string{}
	for _, row := range workflowTable.rows {
		if row[stage0Col] == "unsupported" && row[stage1Col] == "unsupported" {
			unsupportedWorkflow = append(unsupportedWorkflow, matrixRowEntry(row[entryCol]))
		}
	}
	if len(unsupportedWorkflow) == 0 {
		t.Fatal("no workflow row is bound unsupported; the derivation became vacuous, update this probe deliberately")
	}
	for _, entry := range unsupportedWorkflow {
		out, code := qaRunRouteMatrixCLI(t, binPath, "workflow", entry)
		if code != 1 || !strings.Contains(out, "unknown workflow subcommand: "+entry) {
			t.Errorf("workflow %s is bound unsupported in %s but the public CLI did not explicitly reject it (rc=%d): %s", entry, artifact, code, out)
		}
	}

	const maintSection = "## 维护/transport 面"
	maintTable := parseMarkdownTableInSection(t, content, maintSection, artifact)
	mEntryCol := maintTable.column(t, "入口", artifact, maintSection)
	mCategoryCol := maintTable.column(t, "分类", artifact, maintSection)
	mStage0Col := maintTable.column(t, "阶段 0 绑定", artifact, maintSection)
	mStage1Col := maintTable.column(t, "阶段 1 绑定", artifact, maintSection)

	unsupportedMaintenance := []string{}
	internalOwnerRegistry := []string{}
	internalOwnerTopLevel := []string{}
	for _, row := range maintTable.rows {
		entry := matrixRowEntry(row[mEntryCol])
		if row[mStage0Col] == "unsupported" && row[mStage1Col] == "unsupported" {
			unsupportedMaintenance = append(unsupportedMaintenance, entry)
			if strings.Contains(entry, " ") {
				t.Fatalf("unsupported maintenance entry %q is not a top-level command; extend this probe deliberately", entry)
			}
			out, code := qaRunRouteMatrixCLI(t, binPath, entry)
			if code != 1 || !strings.Contains(out, "unknown command: "+entry) {
				t.Errorf("maintenance entry %s is bound unsupported but the public CLI did not explicitly reject it (rc=%d): %s", entry, code, out)
			}
			continue
		}
		if !strings.Contains(row[mCategoryCol], "内部 owner handler") {
			continue
		}
		if rest, ok := strings.CutPrefix(entry, "registry "); ok {
			internalOwnerRegistry = append(internalOwnerRegistry, rest)
			out, code := qaRunRouteMatrixCLI(t, binPath, "registry", rest)
			if code != 1 || !strings.Contains(out, "unknown registry subcommand: "+rest) {
				t.Errorf("registry %s is documented as a CLI-less internal owner handler but was not rejected (rc=%d): %s", rest, code, out)
			}
			continue
		}
		if strings.Contains(entry, " ") {
			t.Fatalf("internal-owner maintenance entry %q is not a top-level command; extend this probe deliberately", entry)
		}
		internalOwnerTopLevel = append(internalOwnerTopLevel, entry)
		out, code := qaRunRouteMatrixCLI(t, binPath, entry)
		if code != 1 || !strings.Contains(out, "unknown command: "+entry) {
			t.Errorf("%s is documented as a non-public internal owner handler but the CLI did not reject it (rc=%d): %s", entry, code, out)
		}
	}
	if len(unsupportedMaintenance) == 0 {
		t.Fatal("no maintenance row is bound unsupported; the derivation became vacuous, update this probe deliberately")
	}
	if len(internalOwnerRegistry) == 0 {
		t.Fatal("no registry internal-owner handler row was probed; the derivation became vacuous, update this probe deliberately")
	}
	if len(internalOwnerTopLevel) == 0 {
		t.Fatal("no non-registry internal-owner handler row was probed; the derivation became vacuous, update this probe deliberately")
	}
}

// TestRouteMatrixPublicUsageMatchesRegistryAndMatrixRows 落实"先覆盖后实现"与
// "实际入口在矩阵中缺席即缺陷"在公共帮助面上的口径：真实 CLI 的 workflow 帮助
// 清单与 cli.go 注册表集合相等、且每个 token 都有矩阵行；unsupported token 不得
// 出现在公共帮助里（不得缺省冒充）；顶层帮助覆盖全部顶层命令且不含 unsupported
// 维护命令。
func TestRouteMatrixPublicUsageMatchesRegistryAndMatrixRows(t *testing.T) {
	const artifact = "refactor-plan/route-matrix.md"
	content := loadRepoFileForMatrixTest(t, artifact)
	binPath := qaBuildRouteMatrixCLIBinary(t)

	helpOut, code := qaRunRouteMatrixCLI(t, binPath, "workflow", "--help")
	if code != 0 {
		t.Fatalf("workflow --help returned rc=%d:\n%s", code, helpOut)
	}
	m := regexp.MustCompile(`Subcommands:\s*\n\s+([a-z0-9|-]+)`).FindStringSubmatch(helpOut)
	if m == nil {
		t.Fatalf("workflow help output has no subcommand list:\n%s", helpOut)
	}
	usageTokens := strings.Split(m[1], "|")
	if len(usageTokens) < 5 {
		t.Fatalf("parsed an implausibly small public usage list: %v", usageTokens)
	}
	assertSameStringSet(t, "public workflow usage vs cli.go workflowSubcommands registry", usageTokens, cliWorkflowSubcommandsFromSource(t))

	const wfSection = "## workflow 面"
	table := parseMarkdownTableInSection(t, content, wfSection, artifact)
	entryCol := table.column(t, "入口", artifact, wfSection)
	stage0Col := table.column(t, "阶段 0 绑定", artifact, wfSection)
	stage1Col := table.column(t, "阶段 1 绑定", artifact, wfSection)
	matrixEntries := map[string]bool{}
	unsupported := map[string]bool{}
	for _, row := range table.rows {
		entry := matrixRowEntry(row[entryCol])
		matrixEntries[entry] = true
		if row[stage0Col] == "unsupported" && row[stage1Col] == "unsupported" {
			unsupported[entry] = true
		}
	}
	for _, token := range usageTokens {
		if !matrixEntries[token] {
			t.Errorf("workflow subcommand %q is listed in the public help but has no row in %s", token, artifact)
		}
		if unsupported[token] {
			t.Errorf("workflow subcommand %q is bound unsupported in %s but appears in the public help", token, artifact)
		}
	}

	topOut, topCode := qaRunRouteMatrixCLI(t, binPath, "--help")
	if topCode != 0 {
		t.Fatalf("top-level --help returned rc=%d:\n%s", topCode, topOut)
	}
	for _, command := range cliTopLevelCommandsFromSource(t) {
		if !strings.Contains(topOut, "\n  "+command) {
			t.Errorf("top-level command %q is dispatched by cli.go but missing from the top-level help", command)
		}
	}
	const maintSection = "## 维护/transport 面"
	maintTable := parseMarkdownTableInSection(t, content, maintSection, artifact)
	mEntryCol := maintTable.column(t, "入口", artifact, maintSection)
	mStage0Col := maintTable.column(t, "阶段 0 绑定", artifact, maintSection)
	mStage1Col := maintTable.column(t, "阶段 1 绑定", artifact, maintSection)
	for _, row := range maintTable.rows {
		entry := matrixRowEntry(row[mEntryCol])
		if row[mStage0Col] == "unsupported" && row[mStage1Col] == "unsupported" && !strings.Contains(entry, " ") {
			if strings.Contains(topOut, entry) {
				t.Errorf("maintenance command %q is bound unsupported in %s but appears in the top-level help", entry, artifact)
			}
		}
	}
}

// qaPhaseIdentityJSON 是封板 run JSON 身份字段的最小投影。
type qaPhaseIdentityJSON struct {
	RunID           string `json:"runId"`
	Status          string `json:"status"`
	CurrentSnapshot string `json:"currentSnapshot"`
	BaseSnapshot    string `json:"baseSnapshot"`
}

func qaLoadPhaseIdentity(t *testing.T, rel string) qaPhaseIdentityJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("sealed phase result %s is missing: %v", rel, err)
	}
	var doc qaPhaseIdentityJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	if doc.RunID == "" || doc.CurrentSnapshot == "" || doc.BaseSnapshot == "" {
		t.Fatalf("%s lacks runId/currentSnapshot/baseSnapshot identity fields", rel)
	}
	return doc
}

func qaGitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRootValidateTest(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// qaStageSection 截取 stage-records.md 中单个阶段节（到下一个二级标题为止）。
func qaStageSection(t *testing.T, content, marker string) string {
	t.Helper()
	idx := strings.Index(content, marker)
	if idx < 0 {
		t.Fatalf("stage-records.md is missing section %q", marker)
	}
	section := content[idx:]
	if next := strings.Index(section[len(marker):], "\n## "); next >= 0 {
		section = section[:len(marker)+next]
	}
	return section
}

func qaConstString(t *testing.T, src, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]+)"`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("constant %s not found in source", name)
	}
	return m[1]
}

// TestStageRecordsPointersMatchGitHistoryAndPhaseJSON 落实 RQ-2 的可溯源指针口径
// 与验收 3：阶段记录（与矩阵）引用的全部全哈希 git 对象真实存在；runId/SEALED/
// currentSnapshot/baseSnapshot 与封板 JSON 一致并出现在对应阶段节；sealed commit
// 的父提交声明与 git 历史一致、且等于 JSON baseSnapshot；黑盒用例交付物的行数
// 声明与实际一致；阶段 0 契约常量与 phase0.go 一致、阶段 1 definition digest 与
// identity_gen.go 常量及 definitions/workflow.json 复算值一致。
func TestStageRecordsPointersMatchGitHistoryAndPhaseJSON(t *testing.T) {
	records := loadRepoFileForMatrixTest(t, "refactor-plan/stage-records.md")
	matrix := loadRepoFileForMatrixTest(t, "refactor-plan/route-matrix.md")

	hashes := map[string]bool{}
	for _, cited := range regexp.MustCompile(`\b[0-9a-f]{40}\b`).FindAllString(records+" "+matrix, -1) {
		hashes[cited] = true
	}
	if len(hashes) == 0 {
		t.Fatal("the artifacts cite no full git hashes; the delivery changed shape")
	}
	for hash := range hashes {
		cmd := exec.Command("git", "cat-file", "-t", hash)
		cmd.Dir = repoRootValidateTest(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("cited git object %s does not exist in the repository: %v\n%s", hash, err, out)
		}
	}

	parentRe := regexp.MustCompile(`(?s)父提交.{0,200}?([0-9a-f]{40})`)
	phases := []struct {
		jsonRel string
		section string
	}{
		{".gates/results/phase-0-distribution-002.json", "## 阶段 0"},
		{".gates/results/phase-1-decision-kernel.json", "## 阶段 1"},
	}
	for _, phase := range phases {
		doc := qaLoadPhaseIdentity(t, phase.jsonRel)
		if doc.Status != "SEALED" {
			t.Errorf("%s records status %q; stage-records.md registers sealed phases", phase.jsonRel, doc.Status)
		}
		section := qaStageSection(t, records, phase.section)
		for _, needle := range []string{doc.RunID, "SEALED", doc.CurrentSnapshot, doc.BaseSnapshot} {
			if !strings.Contains(section, needle) {
				t.Errorf("%s section of stage-records.md does not register %q from %s", phase.section, needle, phase.jsonRel)
			}
		}
		parent := qaGitOutput(t, "rev-parse", doc.CurrentSnapshot+"^")
		claim := parentRe.FindStringSubmatch(section)
		if claim == nil {
			t.Fatalf("%s section does not register the sealed commit's parent as a full hash", phase.section)
		}
		if claim[1] != parent {
			t.Errorf("%s section claims sealed parent %s but git history says %s", phase.section, claim[1], parent)
		}
		if doc.BaseSnapshot != parent {
			t.Errorf("%s baseSnapshot %s does not match the sealed commit's parent %s", phase.jsonRel, doc.BaseSnapshot, parent)
		}
	}

	countMatches := regexp.MustCompile("`([^`]+\\.blackbox-cases\\.md)`（(\\d+) 行）").FindAllStringSubmatch(records, -1)
	if len(countMatches) != 2 {
		t.Fatalf("stage-records.md must register both phases' blackbox-case deliverables with line counts, found %d", len(countMatches))
	}
	for _, m := range countMatches {
		data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(m[1])))
		if err != nil {
			t.Errorf("blackbox-case deliverable %s is missing: %v", m[1], err)
			continue
		}
		wantLines, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parse claimed line count for %s: %v", m[1], err)
		}
		gotLines := strings.Count(string(data), "\n")
		if gotLines != wantLines {
			t.Errorf("stage-records.md claims %s has %d lines, actual %d", m[1], wantLines, gotLines)
		}
	}

	phase0Src := loadRepoFileForMatrixTest(t, "internal/validate/phase0.go")
	stage0 := qaStageSection(t, records, "## 阶段 0")
	frozenDigest := qaConstString(t, phase0Src, "CurrentWorkflowDefinitionDigest")
	frozenVersion := qaConstString(t, phase0Src, "CurrentWorkflowDefinitionVersion")
	for _, needle := range []string{frozenDigest, `CurrentWorkflowDefinitionVersion = "` + frozenVersion + `"`} {
		if !strings.Contains(stage0, needle) {
			t.Errorf("stage 0 section does not register the frozen contract constant %q", needle)
		}
	}

	genSrc := loadRepoFileForMatrixTest(t, "internal/engine/definition/identity_gen.go")
	engineDigest := qaConstString(t, genSrc, "WorkflowDefinitionDigest")
	stage1 := qaStageSection(t, records, "## 阶段 1")
	if !strings.Contains(stage1, engineDigest) {
		t.Errorf("stage 1 section does not register the engine definition digest %q", engineDigest)
	}
	artifactData, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), "definitions", "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifactData)
	recomputed := "sha256:" + hex.EncodeToString(sum[:])
	if recomputed != engineDigest {
		t.Errorf("definitions/workflow.json sha256 %s does not match the engine digest constant %s (stage-records claims the recomputed value agrees)", recomputed, engineDigest)
	}
}
