package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func installedScriptRuntimeFixture(t *testing.T) string {
	t.Helper()
	source := copyPackageFixture(t)
	entries, err := installPackageFiles(source, true)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := copyInstallPackageFiles(source, target, entries); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestInstalledScriptRuntimeAllowsDeclaredHostAssets(t *testing.T) {
	root := installedScriptRuntimeFixture(t)
	entries, err := installPackageFiles(root, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"internal/harness/native_dsh.mjs", "internal/harness/zcode_credential.mjs"} {
		declared := false
		for _, entry := range entries {
			if entry.Path == relative {
				declared = true
				break
			}
		}
		if !declared {
			t.Fatalf("host asset %s is not selected by the manifest owner", relative)
		}
	}
	hook := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(hook, []byte(`{"command":"formal-gates hook decide"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := InstallReport{Targets: []InstallTargetReport{{TargetPath: root, HookConfig: hook}}}
	if detail := installedScriptRuntimeDetail(report); detail != "" {
		t.Fatalf("declared native host assets were rejected: %s", detail)
	}
}

func TestInstalledScriptRuntimeRejectsUndeclaredScripts(t *testing.T) {
	paths := []string{
		"native_dsh.mjs",
		"runtime/native_dsh.mjs",
		"internal/harness/nested/zcode_credential.mjs",
		"internal/harness/Native_dsh.mjs",
	}
	for _, ext := range []string{".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs"} {
		paths = append(paths, "internal/harness/other"+ext, "references/other"+strings.ToUpper(ext))
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			root := installedScriptRuntimeFixture(t)
			path := filepath.Join(root, filepath.FromSlash(rel))
			if rel == "internal/harness/Native_dsh.mjs" {
				if err := os.Remove(filepath.Join(root, "internal/harness/native_dsh.mjs")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(rel+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			detail := installedScriptRuntimeDetail(InstallReport{Targets: []InstallTargetReport{{TargetPath: root}}})
			if detail != "installed script runtime files: "+filepath.ToSlash(path) {
				t.Fatalf("undeclared script was not identified: %q", detail)
			}
		})
	}
}

func TestInstalledScriptRuntimeRejectsScriptHookCommands(t *testing.T) {
	for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash"} {
		t.Run(marker, func(t *testing.T) {
			root := installedScriptRuntimeFixture(t)
			hook := filepath.Join(t.TempDir(), "hooks.json")
			if err := os.WriteFile(hook, []byte(`{"command":"`+strings.ToUpper(marker)+` helper"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			detail := installedScriptRuntimeDetail(InstallReport{Targets: []InstallTargetReport{{TargetPath: root, HookConfig: hook}}})
			if detail != "hook config contains script runtime marker "+marker {
				t.Fatalf("script hook marker was not identified: %q", detail)
			}
		})
	}
}

func TestCodexHookProbeRecordsWithoutMakingHookDecision(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	payload := []byte(fmt.Sprintf(` {"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":%q},"session_id":"owner","transcript_path":"/owner.jsonl"}`+"\n", "formal-gates workflow start --root "+fmt.Sprintf("%q", root)+" --run-id hook-probe"))

	probe, result := CodexHookProbe(CodexHookProbeOptions{
		PayloadDir: dir,
		Payload:    payload,
	})
	if !result.OK() {
		t.Fatalf("expected hook probe to pass, got %#v", result.Failures)
	}
	if probe.ExitCode != 0 {
		t.Fatalf("expected passive recorder to exit successfully, got %#v", probe)
	}
	data, err := os.ReadFile(filepath.FromSlash(probe.PayloadPath))
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("original payload changed: %q, %v", data, err)
	}
	if _, err := os.Stat(ownerSidecarPath(root, "hook-probe")); !os.IsNotExist(err) {
		t.Fatalf("passive probe must not capture a start owner: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("passive probe must only write raw input: %v %v", entries, err)
	}
}

func TestPortableComparisonUsesIssuedNativeCaseEvidence(t *testing.T) {
	cases := []struct {
		item       coverage.AcceptanceCase
		evidence   protocol.CheckEvidence
		comparison string
	}{
		{coverage.AcceptanceCase{CaseID: "delivery-blackbox", Mode: coverage.CaseBlackbox}, protocol.CheckEvidence{StdoutText: canaryDeliveryContents}, fmt.Sprintf("expected stdout=%q and exit=0; actual stdout=%q exit=0; equal=true", canaryDeliveryContents, canaryDeliveryContents)},
		{coverage.AcceptanceCase{CaseID: "missing-blackbox", Mode: coverage.CaseBlackbox, Steps: "Read absent-marker.txt from the delivered HEAD"}, protocol.CheckEvidence{ExitCode: 128, StderrText: "fatal: path 'absent-marker.txt' does not exist in 'HEAD'"}, "expected Git to reject absent-marker.txt with no stdout; actual exit=128 stdout=\"\" stderr=\"fatal: path 'absent-marker.txt' does not exist in 'HEAD'\""},
		{coverage.AcceptanceCase{CaseID: "delivery-whitebox", Mode: coverage.CaseWhitebox, TestRef: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, protocol.CheckEvidence{GoTestName: "TestWhiteboxDirectBehavior", GoResult: "pass"}, "expected exact Go test TestWhiteboxDirectBehavior to pass; actual test=TestWhiteboxDirectBehavior result=pass exit=0"},
	}
	for _, item := range cases {
		value, err := portableComparison(item.item, protocol.ExecutionFact{CaseID: item.item.CaseID, Operations: []protocol.ExecutionOperationFact{{Evidence: item.evidence}}})
		if err != nil || value.Result != coverage.ExecutedPass || value.Comparison != item.comparison {
			t.Fatalf("lost actual native comparison: %+v %v", value, err)
		}
	}
}

func TestPortableComparisonRetainsActualExecutionFailures(t *testing.T) {
	for _, item := range []struct {
		name   string
		c      coverage.AcceptanceCase
		e      protocol.CheckEvidence
		actual string
	}{
		{"missing-delivery", coverage.AcceptanceCase{CaseID: "delivery-blackbox", Mode: coverage.CaseBlackbox}, protocol.CheckEvidence{ExitCode: 128}, "exit=128"},
		{"mismatched-delivery", coverage.AcceptanceCase{CaseID: "delivery-blackbox", Mode: coverage.CaseBlackbox}, protocol.CheckEvidence{StdoutText: "unexpected delivery\n"}, "unexpected delivery"},
		{"present-absent-artifact", coverage.AcceptanceCase{CaseID: "missing-blackbox", Mode: coverage.CaseBlackbox, Steps: "Read absent-marker.txt from the delivered HEAD"}, protocol.CheckEvidence{StdoutText: "artifact exists"}, "artifact exists"},
		{"wrong-whitebox-test", coverage.AcceptanceCase{Mode: coverage.CaseWhitebox, TestRef: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, protocol.CheckEvidence{GoResult: "pass", GoTestName: "AnotherTest"}, "actual test=AnotherTest"},
		{"whitebox-fail", coverage.AcceptanceCase{Mode: coverage.CaseWhitebox, TestRef: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, protocol.CheckEvidence{ExitCode: 1, GoResult: "fail", GoTestName: "TestWhiteboxDirectBehavior"}, "result=fail exit=1"},
		{"whitebox-unmatched", coverage.AcceptanceCase{Mode: coverage.CaseWhitebox, TestRef: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, protocol.CheckEvidence{GoResult: "not-executed"}, "result=not-executed"},
		{"whitebox-skip", coverage.AcceptanceCase{Mode: coverage.CaseWhitebox, TestRef: "whitebox_delivered_test.go::TestWhiteboxDirectBehavior"}, protocol.CheckEvidence{GoResult: "skip", GoTestName: "TestWhiteboxDirectBehavior"}, "result=skip"},
	} {
		t.Run(item.name, func(t *testing.T) {
			value, err := portableComparison(item.c, protocol.ExecutionFact{Operations: []protocol.ExecutionOperationFact{{Evidence: item.e}}})
			if err != nil || value.Result != coverage.ExecutedFail || !strings.Contains(value.Comparison, item.actual) {
				t.Fatalf("native failure became PASS or lost actual evidence: %+v %v", value, err)
			}
		})
	}
}

func TestPortableWhiteboxCasesObserveArtifacts(t *testing.T) {
	workspace := t.TempDir()
	input := protocol.WorkerInput{Validation: &protocol.ValidationInput{Handler: "engine.qa.design.v5", Workspace: &protocol.NativeResource{Path: workspace}}}
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	var facts []worker.LocationFact
	value, err := portableWorkerResult(input, map[string]bool{}, &facts)
	if err != nil {
		t.Fatal(err)
	}
	design := value.(*worker.Design)
	if len(design.Cases) != 2 || len(facts) != 1 {
		t.Fatalf("whitebox fixture requires two cases and their original read: %+v %+v", design, facts)
	}
	actual, err := os.ReadFile(filepath.Join(workspace, "whitebox_delivered_test.go"))
	if err != nil || !bytes.Equal(actual, facts[0].Content) || string(actual) != whiteboxDeliveredTestCode {
		t.Fatalf("delivered source differs from its original read: %s %v", actual, err)
	}
	symbols, err := coverage.GoTestSymbols(facts[0].Paths[0], facts[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestWhiteboxDirectBehavior", "TestWhiteboxMissingArtifact"}
	if len(symbols) != len(want) {
		t.Fatalf("fixture has unrelated test symbols: %v", symbols)
	}
	for index, item := range design.Cases {
		selection := item.Test
		if selection == nil || selection.Read != 1 || selection.Symbol != index+1 || symbols[index] != want[index] {
			t.Fatalf("case %d lost its original source/symbol binding: %+v %v", index, selection, symbols)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module portablefixture\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, delivery string
		marker         bool
		selected       []string
		result         string
	}{
		{"valid-artifacts", "delivery\n", false, symbols, "pass"},
		{"wrong-delivery", "wrong\n", false, symbols[:1], "fail"},
		{"present-missing-artifact", "delivery\n", true, symbols[1:], "fail"},
	} {
		t.Run(item.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(workspace, "delivery.txt"), []byte(item.delivery), 0600); err != nil {
				t.Fatal(err)
			}
			if item.marker {
				if err := os.WriteFile(filepath.Join(workspace, "absent-marker.txt"), []byte("existing artifact\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			argv := []string{"go", "test", "-timeout=0", "-count=1", "-run", "^(" + strings.Join(item.selected, "|") + ")$", "-json", "."}
			command := exec.Command(argv[0], argv[1:]...)
			command.Dir = workspace
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			t.Logf("argv=%q cwd=%q exit=%d stdout=%s stderr=%s", argv, workspace, code, &stdout, &stderr)
			if (code == 0) != (item.result == "pass") {
				t.Fatalf("selected %v execution result differs: exit=%d", item.selected, code)
			}
			observed := map[string]string{}
			decoder := json.NewDecoder(&stdout)
			for decoder.More() {
				var event struct{ Action, Test string }
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event.Test != "" && (event.Action == "pass" || event.Action == "fail") {
					observed[event.Test] = event.Action
				}
			}
			for _, name := range item.selected {
				if observed[name] != item.result {
					t.Fatalf("selected test %s did not report %s: %v", name, item.result, observed)
				}
			}
		})
	}
}
