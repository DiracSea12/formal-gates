//go:build phase0whitebox

package validate

import

// 白盒结构测试（QA-INCREMENTAL-ISOLATION 白盒用例实现）。
//
// 本文件是白盒设计者独立交付的结构测试代码，绑定本 run 的每一条白盒用例
// （--test "<文件路径>::<函数名>"，一测试实现一用例）。按三处改动的职责归属分层：
//   - 改动 1（黑盒用例隔离 + seal 合回）：mirror 派生视图、mirror 写入门、物化过滤、
//     seal 合回路径。
//   - 改动 2（QA 用例真增量）：默认增量保留、--case-id 修改、未知 id 拒绝、语义重复拒绝、
//     --remove-case / --replace-all、按 mode 隔离、跨 mode 全局唯一 id、返工约束、
//     --test 绑定、审查提示词增量上下文。
//   - 改动 3（write-block 收窄）：只按真实写目标判定，只读命令（即使提到 .gates）放行、
//     真写仍拦；重定向解析；代码/run 状态路径判定；hook 级放行/拦截。
//
// 测试在开发后主工作区（绑定当前快照）运行，与 development-worker 交付的既有测试相互独立。
(
	"fmt"
	"os"

	"testing"
)

// assertBlackboxMirrorContent asserts the file at path holds exactly the given
// derived-view bytes (single source: mirror / seal materialization are both
// blackboxCasesMarkdown of the run-state cases).
func assertBlackboxMirrorContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("file is not the run-state derived view:\n--- got ---\n%s\n--- want ---\n%s", data, want)
	}
}

// TestWhiteboxCommandWritesFilesJudgesRealTargetsNotGatesMention verifies 改动 3 的最低层
// commandWritesFiles：命令文本提到 .gates 但只是只读查询不再判写入；真实写目标
// （git/tee/sed -i/重定向到 .gates/代码）仍判写入。
func TestWhiteboxCommandWritesFilesJudgesRealTargetsNotGatesMention(t *testing.T) {
	readOnly := []string{
		"rg --files .gates/",
		"grep -rn reviewStatus .gates/cases/",
		"cat .gates/cases/blackbox.md",
		"git log --oneline -- .gates/results/",
		"git diff --stat HEAD -- .gates/results/",
		"git show HEAD:.gates/tmp/x/state.json",
		"python3 read.py .gates/cases/blackbox.md",
	}
	for _, cmd := range readOnly {
		if commandWritesFiles(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a write: %q", cmd)
		}
	}
	writing := []string{
		"git add .gates/results/x.json",
		"git commit -m x",
		"echo x > .gates/cases/blackbox.md",
		"echo x >> .gates/tmp/state.json",
		"tee .gates/results/x.md",
		"sed -i 's/a/b/' internal/code.go",
	}
	for _, cmd := range writing {
		if !commandWritesFiles(cmd) {
			t.Fatalf("real write to .gates/code not detected: %q", cmd)
		}
	}
}

// TestWhiteboxBashWriteTargetsJudgesRealTargetsNotGatesMention verifies 改动 3 的主线程
// Bash 判定 bashWriteTargetsCodeOrState：只读命令（含 .gates 字样）放行；真写 .gates/代码
// 仍拦；重定向到非代码、非 run 状态文档不命中。
func TestWhiteboxBashWriteTargetsJudgesRealTargetsNotGatesMention(t *testing.T) {
	readOnly := []string{
		"rg --files .gates/",
		"grep -rn reviewStatus .gates/cases/",
		"cat .gates/cases/blackbox.md",
		"git log --oneline -- .gates/results/",
		"git status --short .gates/",
		"git show HEAD:.gates/tmp/x/state.json",
		"python3 read.py .gates/cases/blackbox.md",
	}
	for _, cmd := range readOnly {
		if bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a code/state write: %q", cmd)
		}
	}
	writing := []string{
		"git add .gates/results/x.json",
		"git commit -m x",
		"echo x > .gates/cases/blackbox.md",
		"tee .gates/results/x.md",
		"echo x > internal/code.go",
	}
	for _, cmd := range writing {
		if !bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("real write to .gates/code not detected by the main-thread judge: %q", cmd)
		}
	}
	for _, cmd := range []string{"echo note >> P2-BACKLOG.md", "echo x > notes.md"} {
		if bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("redirect to a non-code doc misjudged as a code/state write: %q", cmd)
		}
	}
}

// TestWhiteboxRedirectWriteTargetParsing verifies 改动 3 的重定向目标解析：引号目标、追加重
// 定向、stderr 重定向、noclobber 都能解析；只读惯用法（2>&1、> /dev/null、2>/dev/null）不
// 命中。
func TestWhiteboxRedirectWriteTargetParsing(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{command: `echo x > "quoted.log"`, want: []string{"quoted.log"}},
		{command: "echo x >> log.txt", want: []string{"log.txt"}},
		{command: "cmd 2> err.log", want: []string{"err.log"}},
		{command: "echo a >| out.txt", want: []string{"out.txt"}},
		{command: "echo x > main.go", want: []string{"main.go"}},
		{command: "go test ./... 2>&1 | head", want: nil},
		{command: "echo hi > /dev/null", want: nil},
		{command: "go test ./... 2>/dev/null", want: nil},
		{command: "echo hi > /dev/null 2>&1", want: nil},
	} {
		got := redirectWriteTargets(tc.command)
		if len(got) != len(tc.want) {
			t.Fatalf("redirectWriteTargets(%q) = %#v, want %#v", tc.command, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("redirectWriteTargets(%q) = %#v, want %#v", tc.command, got, tc.want)
			}
		}
	}
}

// TestWhiteboxIsCodeOrRunStatePathClassification verifies 改动 3 的目标路径判定：run 状态
// （任何 .gates 路径）与代码扩展名命中，非代码、非状态文档不命中。
func TestWhiteboxIsCodeOrRunStatePathClassification(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: ".gates/cases/blackbox.md", want: true},
		{path: "internal/main.go", want: true},
		{path: "scripts/deploy.sh", want: true},
		{path: "P2-BACKLOG.md", want: false},
		{path: "notes.md", want: false},
		{path: "README.md", want: false},
		{path: "", want: false},
		{path: ".gates", want: true},
	} {
		if got := isCodeOrRunStatePath(tc.path); got != tc.want {
			t.Fatalf("isCodeOrRunStatePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestWhiteboxHookAllowsGatesMentionReadsAndDeniesRealWrites verifies 改动 3 的 hook 级
// 判定：活动 run 下主线程/审查类代理的只读 Bash（含 .gates 字样）放行；真写 .gates/代码
// 仍被拦。
func TestWhiteboxHookAllowsGatesMentionReadsAndDeniesRealWrites(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	for _, tc := range []struct {
		name     string
		command  string
		agent    string
		wantDeny bool
	}{
		{name: "main thread rg reads gates", command: "rg --files .gates/"},
		{name: "reviewer grep reads gates", command: "grep -rn reviewStatus .gates/cases/", agent: "qa-review"},
		{name: "main thread python3 reads gates", command: "python3 read.py .gates/cases/blackbox.md"},
		{name: "main thread git diff over gates path", command: "git diff --stat HEAD -- .gates/results/"},
		{name: "main thread redirect writes gates", command: "echo x > .gates/cases/blackbox.md", wantDeny: true},
		{name: "reviewer tee writes code", command: "tee internal/code.go", agent: "qa-review", wantDeny: true},
		{name: "main thread git add gates", command: "git add .gates/results/x.json", wantDeny: true},
	} {
		agentPart := ""
		if tc.agent != "" {
			agentPart = fmt.Sprintf(`,"agent_type":%q`, tc.agent)
		}
		payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}%s}`, root, tc.command, agentPart)
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.wantDeny && decision.PermissionDecision != "deny" {
			t.Fatalf("%s: expected deny, got %#v", tc.name, decision)
		}
		if !tc.wantDeny && decision.PermissionDecision != "allow" {
			t.Fatalf("%s: expected allow, got %#v", tc.name, decision)
		}
	}
}
