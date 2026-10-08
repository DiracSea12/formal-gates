//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirement"
)

func TestCanaryRequirementSeparatesBehaviorAndImplementation(t *testing.T) {
	doc, err := requirement.ParseRequirementProjection("requirements.md", []byte(canaryRequirementDocument))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != canaryRequirementDocument || len(doc.Requirements) != 1 {
		t.Fatal("confirmed complete document changed")
	}
	req := doc.Requirements[0]
	if req.ID != "REQ-001" || len(req.AcceptanceConditions) != 2 || req.AcceptanceConditions[0].ID != "AC-001" || req.AcceptanceConditions[1].ID != "AC-002" {
		t.Fatalf("REQ/AC inventory changed: %+v", req)
	}
	for _, implementation := range []string{"analytics.go", "go build", "调用接线", "本开发批次"} {
		if strings.Contains(req.Requirement, implementation) {
			t.Fatalf("implementation constraint remains a QA requirement: %s", implementation)
		}
		if !strings.Contains(strings.Split(doc.Content, "## 方案\n")[1], implementation) {
			t.Fatalf("solution lost implementation constraint: %s", implementation)
		}
	}
	for _, behavior := range []string{"UTF-8", "空字符串", "额外普通参数", "以 2 退出", "usage: frob <word>", "已知的既有缺口", "不得放宽"} {
		if !strings.Contains(req.Requirement, behavior) {
			t.Fatalf("requirement lost behavior: %s", behavior)
		}
	}
	for _, boundary := range []string{"本开发批次不实现、修复或放宽 AC-002", "frob.go 的 flag 处理", "已授权的返修范围"} {
		if !strings.Contains(doc.Content, boundary) {
			t.Fatalf("authorized development/repair boundary lost: %s", boundary)
		}
	}
}

func TestCanaryPublicEntryExecutesLiteralArguments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "frob.go"), []byte(canaryFrobSource), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module frob\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "frob")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("seed build: %v %s", err, out)
	}
	entries := map[string]bool{}
	for _, args := range [][]string{nil, {""}, {"two words"}, {"first", "ignored"}, {"can't expand $HOME"}} {
		entry := canaryPublicEntry(args)
		if entries[entry] {
			t.Fatalf("argv identities collapsed to %q", entry)
		}
		entries[entry] = true
		direct := exec.Command(binary, args...)
		var wantOut, wantErr bytes.Buffer
		direct.Stdout, direct.Stderr = &wantOut, &wantErr
		directErr := direct.Run()
		shell := exec.Command("/bin/sh", "-c", entry)
		shell.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
		var gotOut, gotErr bytes.Buffer
		shell.Stdout, shell.Stderr = &gotOut, &gotErr
		shellErr := shell.Run()
		code := func(err error) int {
			if err == nil {
				return 0
			}
			if failure, ok := err.(*exec.ExitError); ok {
				return failure.ExitCode()
			}
			t.Fatal(err)
			return -1
		}
		if code(directErr) != code(shellErr) || wantOut.String() != gotOut.String() || wantErr.String() != gotErr.String() {
			t.Fatalf("literal entry %q differs from argv %q: out=%q/%q err=%q/%q", entry, args, gotOut.String(), wantOut.String(), gotErr.String(), wantErr.String())
		}
		if len(args) == 0 && (code(directErr) != 2 || wantOut.Len() != 0 || wantErr.String() != "usage: frob <word>\n") {
			t.Fatal("no-word baseline claim is false")
		}
		if len(args) > 0 && (code(directErr) != 0 || wantOut.String() != "frob: "+args[0]+"\n") {
			t.Fatal("existing first-word behavior was changed")
		}
	}
}

func TestCanaryCaseProducerAndReviewKeepBehaviorFailures(t *testing.T) {
	home := resolveNativePath(t.TempDir())
	workspace := filepath.Join(home, ".formal-gates-resources", "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	approvalRead, approvalWrite := io.Pipe()
	defer approvalRead.Close()
	defer approvalWrite.Close()
	c := &engineCanary{t: t, home: home, project: workspace, jobs: t.TempDir(), worker: true, deadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(approvalRead), approvalOutput: canaryLocalApproval{approvalWrite}}
	doc, err := requirement.ParseRequirementProjection("requirements.md", []byte(canaryRequirementDocument))
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.ValidationInput{Handler: "engine.qa.design", RequirementDocument: doc, Binding: protocol.WorkBinding{WorkspaceID: "qa"}, Workspace: &protocol.NativeResource{ID: "qa", Path: workspace, Status: "READY"}, Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"REQ-001"}, Points: []coverage.AcceptancePoint{{PointID: "AC-001"}, {PointID: "AC-002"}}}, SourcePoints: []protocol.SourcePoint{{SourceID: "REQ-001", PointID: "AC-001"}, {SourceID: "REQ-001", PointID: "AC-002"}}}
	cases := []any{}
	for i, args := range [][]string{{}, {""}, {"two words"}, {"--unknown-flag"}} {
		point := "AC-001"
		if i == 3 {
			point = "AC-002"
		}
		cases = append(cases, map[string]any{"caseId": []string{"missing-word", "empty-word", "spaced-word", "flag"}[i], "pointId": point, "scenario": "BOUNDARY", "args": args, "preconditions": "CLI available as frob", "steps": "invoke literal argv", "oracle": []string{"exit=2 stdout empty stderr=usage: frob <word>", "exit=0 stdout=frob: followed by usage: runs=1 words=0", "exit=0 stdout=frob: two words followed by usage: runs=1 words=9", "nonzero exit with no frob: output"}[i]})
	}
	failReason := "The source also requires the first word to remain unchanged when an extra ordinary argument is present; this case set does not cover that behavior."
	calls := 0
	c.execute = func(request any, directory string, _ bool) (map[string]any, error) {
		calls++
		if getOf(request, "workspace") != workspace || getOf(request, "read_only") != true {
			t.Fatal("case consumer did not receive exact public workspace/read-only contract")
		}
		answer := map[string]any{"cases": cases}
		if calls == 2 {
			prompt := textOf(getOf(request, "prompt"))
			for _, required := range []string{"behavior obligations not yet mapped", "incomplete mapping or missing behavior coverage requires FAIL", "technical review, diff/code review and quality gates", "must not inspect production source"} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("review boundary missing %q", required)
				}
			}
			answer = map[string]any{"sourceDecisions": []any{map[string]any{"id": "REQ-001", "status": "FAIL", "reason": failReason}}, "pointDecisions": []any{map[string]any{"id": "AC-001", "status": "PASS", "reason": "listed point has executable cases"}, map[string]any{"id": "AC-002", "status": "PASS", "reason": "flag has executable case"}}, "caseDecisions": []any{}}
			for _, raw := range cases {
				answer["caseDecisions"] = append(answer["caseDecisions"].([]any), map[string]any{"id": getOf(raw, "caseId"), "status": "PASS", "reason": "case invokes literal public args"})
			}
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(answer)
		if err != nil {
			return nil, err
		}
		if err := writeNativeAnswer(directory, string(raw), "PEER"); err != nil {
			return nil, err
		}
		return map[string]any{"provider": "zcode", "identity": "peer", "usage_evidence": map[string]any{"model_calls": 0}}, nil
	}
	designed := c.designResult("design", input)
	if len(designed.Manifest.Sources) != 1 || len(designed.Manifest.Points) != 2 || len(designed.Manifest.Cases) != 4 {
		t.Fatal("producer lost required source or AC inventory")
	}
	for i, row := range designed.Manifest.Cases {
		expected := getOf(cases[i], "args").([]string)
		if !reflect.DeepEqual(row.Operations, canaryCaseOperations(expected)) || row.PublicEntry != canaryPublicEntry(expected) {
			t.Fatalf("producer rewrote exact args for %s", row.CaseID)
		}
	}
	input.Handler = "engine.qa.review.rationale"
	input.Manifest = *designed.Manifest
	reviewed := c.caseReviewResult("review", input)
	if calls != 2 || reviewed.Status != protocol.OutcomeFail || reviewed.Review == nil || reviewed.Review.SourceDecisions[0].Reason != failReason || reviewed.Review.SourceDecisions[0].Status != coverage.StatusFail {
		t.Fatalf("missing behavior FAIL was rewritten: %+v", reviewed)
	}
}

func TestCanaryApprovedBlackboxOperationsPreserveProcessOutcome(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module fixture\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "frob.go"), []byte(canaryFrobSource), 0600); err != nil {
		t.Fatal(err)
	}
	direct := filepath.Join(t.TempDir(), "frob")
	build := exec.Command("go", "build", "-o", direct, ".")
	build.Dir = workspace
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build direct entry: %v %s", err, out)
	}
	run := func(argv []string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = workspace
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		return code, stdout.String(), stderr.String()
	}
	for _, args := range [][]string{nil, {""}, {"two words"}, {"--unknown-flag"}} {
		operations := canaryCaseOperations(args)
		if len(operations) != 2 || operations[0].Kind != coverage.OperationShell || !reflect.DeepEqual(operations[1].Argv, append([]string{"./frob-candidate"}, args...)) {
			t.Fatalf("approved recipe lost argv: %+v", operations)
		}
		code, out, errOut := run([]string{"/bin/sh", "-c", operations[0].Command})
		if code != 0 {
			t.Fatalf("approved build operation failed: %d %s %s", code, out, errOut)
		}
		wantCode, wantOut, wantErr := run(append([]string{direct}, args...))
		gotCode, gotOut, gotErr := run(operations[1].Argv)
		if gotCode != wantCode || gotOut != wantOut || gotErr != wantErr {
			t.Fatalf("case %q wrapper changed values: got %d %q %q want %d %q %q", args, gotCode, gotOut, gotErr, wantCode, wantOut, wantErr)
		}
		if len(args) == 0 && gotCode != 2 {
			t.Fatalf("missing argument no longer exits 2: %d", gotCode)
		}
	}
}
