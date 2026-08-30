//go:build phase0whitebox

package validate

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

import (
	"encoding/json"
	"os"
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

// Case: the start-time --route surface is a lightweight-only declaration, not a
// generic route picker. Valid routeModes values (full/custom) and garbage are
// rejected at start with the lightweight-or-empty error; "lightweight" is
// accepted case-insensitively and pins RouteMode lightweight. Leaving --route
// empty keeps the regular intake, which still demands the explicit split
// declaration.
func TestStartRouteDeclarationAcceptsOnlyLightweightOrEmpty(t *testing.T) {
	root, pkg := startFixture(t)
	for _, route := range []string{"full", "custom", "bogus"} {
		_, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "r-" + route, Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: route})
		if err == nil || !strings.Contains(err.Error(), "--route must be lightweight or empty") {
			t.Fatalf("route %q: expected lightweight-or-empty rejection, got %v", route, err)
		}
	}
	// 大小写不敏感接受：route 归一化后 LIGHTWEIGHT 等价于 lightweight。
	for index, route := range []string{"lightweight", "LIGHTWEIGHT"} {
		state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-route-" + strings.ToLower(route) + "-" + string(rune('a'+index)), Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: route})
		if err != nil {
			t.Fatalf("route %q: %v", route, err)
		}
		if state.RouteMode != "lightweight" {
			t.Fatalf("route %q: expected RouteMode lightweight, got %q", route, state.RouteMode)
		}
	}
	// 留空走常规受理：仍要求显式拆分声明，声明后才启动。
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "regular-empty", Flow: "formal", RequirementSource: "requirements.md", VCS: "git"}); err == nil || !strings.Contains(err.Error(), "requires an explicit --split") {
		t.Fatalf("empty route must still require the split declaration, got %v", err)
	}
	regular, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "regular-no", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if regular.RouteMode != "" {
		t.Fatalf("regular intake must not pin a route at start, got %q", regular.RouteMode)
	}
	if regular.SplitDeclaration != "no" {
		t.Fatalf("regular intake must record the split declaration, got %q", regular.SplitDeclaration)
	}
}

// Case: a lightweight start refuses every split intent because the lightweight
// route does not split — --split yes, --retained-overall and --master are all
// rejected. An explicit --split no is accepted but the declaration is cleared
// from the state: a lightweight run never records a split declaration.
func TestStartLightweightRejectsSplitDeclarations(t *testing.T) {
	root, pkg := startFixture(t)
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-split-yes", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight", Split: "yes"}); err == nil || !strings.Contains(err.Error(), "does not split") {
		t.Fatalf("lightweight + --split yes accepted: %v", err)
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-retained", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight", RetainedOverall: true, Split: "no"}); err == nil || !strings.Contains(err.Error(), "does not retain overall") {
		t.Fatalf("lightweight + --retained-overall accepted: %v", err)
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-master", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight", MasterRunID: "some-master"}); err == nil || !strings.Contains(err.Error(), "--master are not valid with --route lightweight") {
		t.Fatalf("lightweight + --master accepted: %v", err)
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-split-no", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if state.RouteMode != "lightweight" || state.SplitDeclaration != "" {
		t.Fatalf("lightweight --split no: route=%q declaration=%q", state.RouteMode, state.SplitDeclaration)
	}
}

// Case: SetRoute accepts mode "lightweight" and selects zero gates — RouteMode
// lightweight, no selected gates, every route candidate marked ROUTE/UNSELECTED.
// The lightweight branch rejects any --gate selection with the zero-gates error.
func TestSetRouteLightweightSelectsZeroGates(t *testing.T) {
	root, pkg := startFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "lw-setroute"), "lightweight", nil)
	if state.RouteMode != "lightweight" {
		t.Fatalf("expected RouteMode lightweight, got %q", state.RouteMode)
	}
	if len(state.SelectedGates) != 0 {
		t.Fatalf("lightweight route must select zero gates, got %v", state.SelectedGates)
	}
	for _, id := range []string{blackboxQAID, whiteboxQAID, "quality", "architecture"} {
		auth, ok := state.SkipAuthorizations[id]
		if !ok || auth.Origin != "ROUTE" || auth.Status != "UNSELECTED" {
			t.Fatalf("candidate %q: want ROUTE/UNSELECTED skip, got %#v ok=%v", id, auth, ok)
		}
	}
	// 轻量路线选中零门：携带任何 --gate 都被拒。
	neg := mustStart(t, root, pkg, "lw-setroute-neg")
	neg = confirmRequirement(t, root, pkg, neg)
	neg = recordProductReview(t, root, pkg, neg)
	neg = recordReadiness(t, root, pkg, neg)
	neg = recordSlicing(t, root, pkg, neg, "no-split")
	if _, err := SetRoute(root, pkg, neg.RunID, "lightweight", []string{"quality"}); err == nil || !strings.Contains(err.Error(), "lightweight route selects zero gates without --gate") {
		t.Fatalf("lightweight + --gate accepted: %v", err)
	}
}

// Case: the route-mode set is restored to {lightweight, full, custom}, and the
// invalid-mode error message enumerates all three legal modes.
func TestRouteModesRestoreAndInvalidModeMessage(t *testing.T) {
	for _, mode := range []string{"lightweight", "full", "custom"} {
		if !routeModes[mode] {
			t.Fatalf("routeModes must accept %q", mode)
		}
	}
	if len(routeModes) != 3 {
		t.Fatalf("routeModes must contain exactly lightweight/full/custom, got %v", routeModes)
	}
	root, pkg := startFixture(t)
	state := mustStart(t, root, pkg, "route-mode-invalid")
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	if _, err := SetRoute(root, pkg, state.RunID, "bogus", nil); err == nil || !strings.Contains(err.Error(), "route mode must be lightweight, full, or custom") {
		t.Fatalf("invalid route mode accepted: %v", err)
	}
}

// Case: the seal migration gate is waived for lightweight runs — a lightweight
// run reaches Seal with no development snapshot, no Product Review / Start
// Readiness PASS and no selected results resolved — while the identical missing
// state on a regular run is rejected for the missing development snapshot. The
// isLightweight predicate recognizes only the lightweight route mode.
func TestLightweightRunWaivesSealTransitionGates(t *testing.T) {
	root, pkg := startFixture(t)
	lw, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-seal-gate", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	lw = confirmRequirement(t, root, pkg, lw)
	if !isLightweight(lw) {
		t.Fatal("lightweight run must satisfy isLightweight")
	}
	if err := requireSealTransition(lw); err != nil {
		t.Fatalf("lightweight seal transition must be waived: %v", err)
	}
	if err := requireTransition(lw, "seal", ""); err != nil {
		t.Fatalf("lightweight seal entry must be waived: %v", err)
	}

	// 常规 run 在同样缺失状态下被 seal 门拒绝：要求开发快照。
	regular := RunState{RouteMode: "full", RequirementConfirmed: true, Actions: map[string]ActionResult{"development-worker": {Status: "PENDING"}}}
	if isLightweight(regular) {
		t.Fatal("regular run must not satisfy isLightweight")
	}
	if err := requireSealTransition(regular); err == nil || !strings.Contains(err.Error(), "immutable development snapshot is required before Seal") {
		t.Fatalf("regular run seal gate must demand a development snapshot, got %v", err)
	}

	// isLightweight 判定只认 lightweight 一种路线模式。
	for _, mode := range []string{"", "full", "custom", "merge"} {
		if isLightweight(RunState{RouteMode: mode}) {
			t.Fatalf("isLightweight(%q) must be false", mode)
		}
	}
}

// Case: runSummary annotates a lightweight run's seal summary with
// 「本 run 未经任何验证」, and a verified (full-route) run's summary omits the
// field entirely (JSON omitempty).
func TestRunSummaryMarksLightweightUnverifiedOnly(t *testing.T) {
	lightweight := runSummary(RunState{RunID: "lw", Flow: "formal", Status: "SEALED", RequirementRevision: "r1", VCS: "git", BaseSnapshot: "b", CurrentSnapshot: "c", RouteMode: "lightweight", SelectedGates: []string{}, SkipAuthorizations: map[string]SkipAuthorization{}, Gates: map[string]GateResult{}})
	if lightweight.Unverified != "本 run 未经任何验证" {
		t.Fatalf("lightweight summary must carry the unverified annotation, got %q", lightweight.Unverified)
	}
	lwJSON, err := json.Marshal(lightweight)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(lwJSON), `"unverified":"本 run 未经任何验证"`)

	regular := runSummary(RunState{RunID: "full", Flow: "formal", Status: "SEALED", RequirementRevision: "r1", VCS: "git", BaseSnapshot: "b", CurrentSnapshot: "c", RouteMode: "full", SelectedGates: []string{"quality"}, SkipAuthorizations: map[string]SkipAuthorization{}, Gates: map[string]GateResult{}})
	if regular.Unverified != "" {
		t.Fatalf("regular summary must not carry the unverified annotation, got %q", regular.Unverified)
	}
	fullJSON, err := json.Marshal(regular)
	if err != nil {
		t.Fatal(err)
	}
	mustLack(t, string(fullJSON), `"unverified"`)
}

// Case: a lightweight run's persisted seal record — the .gates/results/<runId>.
// json file written by Seal — stays SEALED, carries routeMode lightweight and
// the explicit 「本 run 未经任何验证」 annotation, so the record itself marks the
// run as unverified, distinguishing it from a verified seal ledger.
func TestSealPersistsLightweightUnverifiedRecord(t *testing.T) {
	root, pkg := startFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-persist", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	state = confirmRequirement(t, root, pkg, state)
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.RouteMode != "lightweight" || summary.Unverified != "本 run 未经任何验证" {
		t.Fatalf("seal summary: route=%q unverified=%q", summary.RouteMode, summary.Unverified)
	}
	record, err := os.ReadFile(RunSummaryPath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(record), `"status": "SEALED"`)
	mustContain(t, string(record), `"routeMode": "lightweight"`)
	mustContain(t, string(record), `"unverified": "本 run 未经任何验证"`)
}

func TestLightweightRequirementCanConfirmWithoutClarificationAction(t *testing.T) {
	root, pkg := startFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "lw-direct-confirm", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil)
	if err != nil {
		t.Fatalf("lightweight requirement confirmation: %v", err)
	}
	if !state.RequirementConfirmed {
		t.Fatal("lightweight requirement was not confirmed")
	}
	if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err != nil {
		t.Fatalf("lightweight seal after direct confirmation: %v", err)
	}
}

// Case: the renamed quick-e2e canary (was "lightweight-workflow") still runs the
// complete verified formal route — start → requirement registration → Product
// Review / Start Readiness → slicing → full-route confirmation → QA design /
// review / execution → development snapshot → every discovered gate review →
// Seal — and returns nil, so the rename preserved the full-verification canary
// behavior instead of silently turning it into the record-only lightweight route.
func TestQuickE2ECanaryRunsFullVerifiedRoute(t *testing.T) {
	packageRoot := repoRootValidateTest(t)
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Gates) == 0 {
		t.Skip("no gates discovered in the package catalog")
	}
	if err := runQuickE2ECanary(packageRoot, catalog); err != nil {
		t.Fatalf("quick-e2e canary must complete the full verified route to Seal: %v", err)
	}
}
