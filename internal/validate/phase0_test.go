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
		DefinitionSource:          CurrentWorkflowDefinitionSource, DefinitionDigest: CurrentWorkflowDefinitionDigest,
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

func TestLoadFutureDefinitionRejectsChangedBytesWithoutVersionBump(t *testing.T) {
	root := t.TempDir()
	definitionDir := filepath.Join(root, "definitions")
	if err := os.MkdirAll(definitionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), "definitions", "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	definition = append(definition, '\n')
	if err := os.WriteFile(filepath.Join(definitionDir, "workflow.json"), definition, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFutureDefinition(root); !IsUnsupportedRunVersion(err) || !strings.Contains(err.Error(), "definitionDigest") {
		t.Fatalf("changed definition without a version bump was accepted: %v", err)
	}
}

func TestWriteVersionedStateOwnsIdentityFieldsAndRejectsBeforeCreate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	valid := VersionEnvelope{
		Writer:                    "engine",
		StateSchemaVersion:        CurrentStateSchemaVersion,
		WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
		DefinitionSource:          CurrentWorkflowDefinitionSource,
		DefinitionDigest:          CurrentWorkflowDefinitionDigest,
		PackageDigest:             "sha256:package",
	}
	if err := WriteVersionedState(path, valid, map[string]any{"writer": "legacy", "status": "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"writer": "engine"`) || strings.Contains(string(data), `"writer": "legacy"`) {
		t.Fatalf("payload overrode the validated writer identity: %s", data)
	}
	invalid := valid
	invalid.DefinitionDigest = "sha256:stale"
	invalidPath := filepath.Join(root, "invalid.json")
	if err := WriteVersionedState(invalidPath, invalid, map[string]any{"status": "ACTIVE"}); err == nil {
		t.Fatal("unsupported version envelope was written")
	}
	if _, err := os.Stat(invalidPath); !os.IsNotExist(err) {
		t.Fatalf("write barrier created an invalid state file: %v", err)
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
	hostConfig := filepath.Join(t.TempDir(), "host", "config.json")
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(hostConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostConfig, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := BuildBaselineReceipt("git:abc123", root, "", map[string]string{"hostConfig": hostConfig})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.VCSIdentity != "git:abc123" || receipt.PackageDigest == "" || receipt.CanonicalPaths["hostConfig"] != canonicalRegistryPath(hostConfig) {
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
