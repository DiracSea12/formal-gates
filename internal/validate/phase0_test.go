package validate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVersionEnvelopeUsesExactWriteBarrier(t *testing.T) {
	valid := VersionEnvelope{
		Writer: "engine", StateSchemaVersion: CurrentStateSchemaVersion,
		WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
		DefinitionSource:          "fixture", DefinitionDigest: "sha256:fixture",
	}
	if err := ValidateVersionEnvelope(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*VersionEnvelope)
	}{
		{"missing schema", func(v *VersionEnvelope) { v.StateSchemaVersion = "" }},
		{"future definition", func(v *VersionEnvelope) { v.WorkflowDefinitionVersion = "2" }},
		{"legacy writer", func(v *VersionEnvelope) { v.Writer = "legacy" }},
		{"missing digest", func(v *VersionEnvelope) { v.DefinitionDigest = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			var unsupported *UnsupportedRunVersionError
			if err := ValidateVersionEnvelope(candidate); !errors.As(err, &unsupported) || !strings.Contains(err.Error(), UnsupportedRunVersionCode) {
				t.Fatalf("expected unsupported-version error, got %v", err)
			}
		})
	}
}

func TestPackageReceiptRejectsSymlinkAndOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "mutable")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "payload"), []byte("worktree"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "payload"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	cleanRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(cleanRoot, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageReceipt(cleanRoot, cleanRoot); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected disjoint proof rejection, got %v", err)
	}
}

func TestDiagnoseStateIsReadOnlyAndKeepsTerminalSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"status":"SEALED","runId":"run-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := DiagnoseState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.JSONReadable || report.Summary["status"] != "SEALED" {
		t.Fatalf("diagnose summary=%#v readable=%v", report.Summary, report.JSONReadable)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"status":"SEALED","runId":"run-1"}` {
		t.Fatal("diagnose modified state")
	}
}

func TestAdmitRegistryLeavesMachineReadableUnregisteredReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	receipt, err := AdmitRegistry(path, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" {
		t.Fatalf("receipt=%+v", receipt)
	}
	if _, err := os.Stat(path + ".admission.json"); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBaselineReceiptBindsCanonicalPathsAndDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := BuildBaselineReceipt("git:abc123", root, "", map[string]string{"source": root})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.VCSIdentity != "git:abc123" || receipt.PackageDigest == "" || receipt.CanonicalPaths["source"] == "" {
		t.Fatalf("unexpected baseline receipt: %+v", receipt)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaselineReceipt(path, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
