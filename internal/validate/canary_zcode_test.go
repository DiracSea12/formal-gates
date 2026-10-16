package validate

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
	"formal-gates/internal/prompts"
)

// Local synthetic worker evidence drives the installed provider; QA operations
// execute the real committed fixture artifacts without a native model session.
func TestPortableZCodeInstalledWorkflowCompletes(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	project := t.TempDir()
	options := InstallOptions{Source: portablePackageFixture(t), Host: host.ZCode, Scope: "project", Project: project, RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "bin", nativeBinaryName()), Force: true, SkipHooks: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0]
	restore := fixtureRegisteredInvocation(report.Registry, target.TargetPath, target.RegistryRecordID)
	defer restore()
	admission, err := ResolveEngineAdmissionByRecord(target.RegistryRecordID, report.Registry)
	if err != nil || admission.Provider != host.ZCode {
		t.Fatalf("installed provider identity: %+v %v", admission, err)
	}
	binary := filepath.Join(target.TargetPath, "bin", nativeBinaryName())
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	info, err := exec.Command("go", "version", "-m", binary).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installed package=%s binary sha256=%x source identity:\n%s", report.PackageDigest, sha256.Sum256(body), info)
	catalog, err := prompts.LoadPromptCatalog(target.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runQuickE2ECanaryAt(target.TargetPath, catalog, project, report.Registry, target.RegistryRecordID); err != nil {
		t.Fatal(err)
	}
	f, err := facade.Open(project, "canary")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" || run.Unverified || run.PromotedSnapshot == "" || run.Envelope.InstalledTargetIdentity != target.RegistryRecordID {
		t.Fatalf("installed ZCode workflow lost its verified terminal: %+v", run)
	}
}
