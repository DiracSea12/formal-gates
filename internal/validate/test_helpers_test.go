package validate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/lifecycle"
)

func TestMain(m *testing.M) {
	// Isolate workflow and canary fixtures from the driving host identity environment.
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		os.Setenv(key, "")
	}
	os.Exit(m.Run())
}

func repoRootForCanaryTest(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the test helper source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func useTestWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	original, err := os.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		_ = original.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := original.Chdir(); err != nil {
			t.Errorf("restore test working directory: %v", err)
		}
		if err := original.Close(); err != nil {
			t.Errorf("close saved test working directory: %v", err)
		}
	})
}

func testRequirementDocument(requirement string) string {
	return "## 需求点\n\n### REQ-001：Test requirement\n\n#### 要求\n\n" + requirement + "\n\n#### 验收条件\n\n- AC-001：The test requirement is satisfied.\n\n#### 来源\n\nTest fixture.\n"
}

// stubLifecycle replaces the global workflowLifecycle verifier with an isolated
// stub for the duration of the test and restores the prior verifier on cleanup.
// It is the single converged stub for the workflow's lifecycle seam; tests that
// stub the lifecycle swap global state, so they must stay serial (no t.Parallel)
// — the suite currently holds that, keep it.
func engineAdmissionFixture(t *testing.T, root, packageDigest, recordID string) *facade.Admission {
	t.Helper()
	registryPath, target, generation, lease, token := testkit.Installation(t, root, recordID, packageDigest, facade.DefaultProvider)
	return &facade.Admission{RegistryPath: registryPath, Target: target, Root: root, Provider: facade.DefaultProvider,
		PackageDigest: packageDigest, InstalledTargetIdentity: recordID, Generation: generation, Lease: lease, Token: token}
}

// Unit tests select the same invocation identity accepted by the production
// launcher owner. Native subprocess coverage supplies its actual executable.
func fixtureLauncherInvocation(t testing.TB, path string) {
	t.Helper()
	original := os.Args[0]
	os.Args[0] = path
	t.Cleanup(func() { os.Args[0] = original })
}

// Workflow fixtures invoke the launcher named by their committed installation.
// Missing or mismatched records remain visible to the normal admission checks.
func fixtureRegisteredInvocation(registry, target, recordID string) func() {
	original := os.Args[0]
	if document, err := LoadRegistry(registry); err == nil {
		for _, record := range document.Records {
			if recordID != "" && record.ID != recordID || target != "" && canonicalRegistryPath(record.Target) != canonicalRegistryPath(target) {
				continue
			}
			os.Args[0] = record.LauncherPath
			break
		}
	}
	return func() { os.Args[0] = original }
}

func prepareInstalledRuntimeFixture(t *testing.T, source, target string) {
	t.Helper()
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	entries, err := installPackageFiles(source, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackageFiles(source, target, entries); err != nil {
		t.Fatal(err)
	}
}
