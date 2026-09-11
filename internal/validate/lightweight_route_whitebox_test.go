//go:build phase0whitebox

package validate

import

// White-box structure tests for the trigger-model V2 change
// (TRIGGER-MODEL-V2-REQUIREMENT.md), independently delivered by the whitebox QA
// designer. Each test function below implements exactly one whitebox case and is
// bound to it via a "--test <file>::<function>" reference; the function name is
// the opaque locator for that case's test.
//
// These cases cover the structural behavior of the lightweight route and its
// boundary with the regular intake, at the layer that owns each rule:
//
//   - the start-time --route declaration surface accepts only lightweight/empty
//     and folds case, while the regular intake still demands the split
//     declaration;
//   - a lightweight start refuses every split intent (--split yes,
//     --retained-overall, --master) and never records a split declaration;
//   - SetRoute restores lightweight into the route-mode set and selects zero
//     gates (rejecting any --gate selection);
//   - the invalid-mode error message enumerates lightweight/full/custom;
//   - the seal transition is waived for lightweight runs while the identical
//     missing state on a regular run is rejected;
//   - the seal summary annotates lightweight runs with 「本 run 未经任何验证」
//     and omits it (JSON omitempty) for verified runs;
//   - the persisted seal record carries the unverified annotation;
//   - the renamed quick-e2e canary still runs the complete verified route.
(
	"path/filepath"
	"strings"
	"testing"
)

// mustContain fails the test unless text contains want.
func mustContain(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("expected text %q to be present, got %q", want, text)
	}
}

// mustLack fails the test when text contains forbidden.
func mustLack(t *testing.T, text, forbidden string) {
	t.Helper()
	if strings.Contains(text, forbidden) {
		t.Fatalf("forbidden text %q is present: %q", forbidden, text)
	}
}

// startFixture builds a fresh git root carrying the requirement/design documents
// and a prompt package with two gates, so start / route / seal operations run
// against a real catalog. It is a local fixture for this file; it does not
// depend on any other test file's fixtures.
func startFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "requirements.md"), "requirement\n")
	writeTestFile(t, filepath.Join(root, "design.md"), "design\n")
	writeTestFile(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n")
	initializeGit(t, root)
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks"})
}
