package validate

import (
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"os"
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
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":"formal-gates workflow record-gate --gate complexity-gate --status PASS --run-id wf"}}`)

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
	if _, err := os.Stat(filepath.FromSlash(probe.PayloadPath)); err != nil {
		t.Fatalf("expected payload artifact: %v", err)
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
