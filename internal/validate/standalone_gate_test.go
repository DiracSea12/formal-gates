package validate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
)

// TestComposeStandaloneGatePrompt covers detached gate prompt: it reuses
// the shared reviewer contract (contamination check applies), the gate block, and
// a working-tree-vs-head change block with no requirement or dispatch binding.
func TestComposeStandaloneGatePrompt(t *testing.T) {
	t.Parallel()
	root, pkg := standaloneGateFixture(t)
	catalog, err := prompts.LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	context, err := CaptureStandaloneGateContext(root, "git", "")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := ComposeStandaloneGatePrompt(catalog, "quality", context)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range []string{"[Shared reviewer contract]", "[Gate: quality]", "[Current change]", "[Result contract]"} {
		if !strings.Contains(prompt, block) {
			t.Fatalf("standalone prompt missing %s: %s", block, prompt)
		}
	}
	if strings.Contains(prompt, "[Current requirement]") || strings.Contains(prompt, "[Dispatch]") {
		t.Fatalf("standalone prompt must not carry requirement or dispatch blocks: %s", prompt)
	}
	// 单跑明示无需求块属设计意图，零上下文审查者不应因此报
	// RUNTIME_ERROR。
	if !strings.Contains(prompt, "no requirement block") || !strings.Contains(prompt, "must not be reported as RUNTIME_ERROR") {
		t.Fatalf("standalone prompt must state the missing requirement block is by design: %s", prompt)
	}
	if !strings.Contains(prompt, "base snapshot: "+standaloneGateHead(t, root)) || !strings.Contains(prompt, "[Native comparison]") {
		t.Fatalf("standalone prompt missing the working-tree comparison: %s", prompt)
	}
	// 复用共享契约块使污染检查自动生效（契约文本本身由
	// TestContaminationCheckCoversReviewersButNotExecutors 对真实包校验）。
	if !strings.Contains(prompt, "[Shared reviewer contract]") {
		t.Fatalf("standalone prompt must reuse the shared reviewer contract: %s", prompt)
	}
	if _, err := ComposeStandaloneGatePrompt(catalog, "missing", context); err == nil {
		t.Fatal("unknown standalone gate was accepted")
	}
	if _, err := CaptureStandaloneGateContext(root, "mercurial", ""); err == nil {
		t.Fatal("unsupported standalone VCS was accepted")
	}
	context.Scope = "internal/cli"
	scoped, err := ComposeStandaloneGatePrompt(catalog, "quality", context)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "Review scope (user-specified): internal/cli") {
		t.Fatalf("standalone scope was not injected: %s", scoped)
	}
}

// TestValidateStandaloneGateResult covers result-contract validation:
// the same semantic rules as a gate result, with no run-state writes.
func TestValidateStandaloneGateResult(t *testing.T) {
	for _, item := range []struct{ name, raw, status, wantErr string }{
		{"empty completed review", `{"findings":[]}`, "PASS", ""},
		{"advisory findings", `{"findings":[{"severity":"P2","message":"suggestion"},{"severity":"P3","message":"minor"}]}`, "PASS", ""},
		{"blocking finding", `{"findings":[{"severity":"P1","message":"blocker"}]}`, "FAIL", ""},
		{"highest severity", `{"findings":[{"severity":"P0","message":"blocking error"}]}`, "FAIL", ""},
		{"actual inability", `{"interruptionReason":"VCS unavailable"}`, "RUNTIME_ERROR", ""},
		{"forbidden status", `{"status":"PASS","findings":[]}`, "", "unknown field"},
		{"inability with findings", `{"interruptionReason":"VCS unavailable","findings":[{"severity":"P2","message":"suggestion"}]}`, "", "cannot include"},
		{"blank inability", `{"interruptionReason":" "}`, "", "actual reason"},
		{"missing judgment", `{}`, "", "requires findings"},
		{"bad json", `{`, "", "not valid JSON"},
		{"trailing object", `{"findings":[]} {}`, "", "one JSON object"},
	} {
		result, err := ValidateStandaloneGateResult([]byte(item.raw), StandaloneGateContext{})
		if item.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), item.wantErr) {
				t.Fatalf("%s: want %q, got %v", item.name, item.wantErr, err)
			}
			continue
		}
		if err != nil || result.Status != item.status {
			t.Fatalf("%s: status %q, want %q: %v", item.name, result.Status, item.status, err)
		}
	}
}

// Typed worker material carries the shared contract for readonly judgments;
// implementation and whitebox authors receive their scoped author contract.
func TestSharedReviewerContractInjectedIntoReviewersOnly(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	roles := []struct {
		handler  string
		action   review.Action
		readOnly bool
	}{
		{"engine.review.worker.v3", review.ProductReview, true}, {"engine.readiness.plan.v6", review.StartReadiness, true}, {"engine.qa.review.rationale.v2", "", true}, {"engine.development.batch.v6", "", false}, {"engine.qa.execution.v4", "", true}, {"engine.repair.verify.v2", "", true}, {"engine.qa.design.v6", "", false},
	}
	for _, role := range roles {
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, RunID: "contract", Provider: "codex", VCS: "git", Handler: role.handler, Pending: protocol.PendingAction{ActionID: "contract:" + role.handler, AttemptID: "attempt"}}
		input.RequirementDocument.Content = "Confirmed requirement"
		if role.action != "" {
			input.Review = &protocol.WorkerReviewInput{Action: role.action, RequirementRevision: "revision"}
		} else {
			input.Validation = &protocol.ValidationInput{Handler: role.handler}
			input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
			if role.handler == "engine.repair.verify.v2" {
				input.Validation.Repair = &protocol.RepairInput{}
				input.Validation.Contract = &coverage.CoverageContract{}
			}
		}
		material, err := worker.Render(input, catalog)
		if err != nil {
			t.Fatalf("%s: %v", role.handler, err)
		}
		injected := strings.HasPrefix(material.Prompt, "[Shared reviewer contract]\nshared contract\n")
		if injected != role.readOnly {
			t.Fatalf("%s shared-contract scope=%t, want %t", role.handler, injected, role.readOnly)
		}
	}
}

func standaloneGateFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	for _, args := range [][]string{{"init"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "user.name", "Gate fixture"}, {"add", "."}, {"commit", "--no-verify", "--no-gpg-sign", "-m", "Delivery"}} {
		runGit(t, root, args...)
	}
	return root, promptPackage(t, map[string]string{"quality": "quality checks"})
}

func standaloneGateHead(t *testing.T, root string) string {
	t.Helper()
	return runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	value, err := (vcs.ExecRunner{}).Run(root, "git", args...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// Captured values remain bound to the review after the live workspace changes.
func TestStandaloneComparisonPublishesOriginalValuesAndLocations(t *testing.T) {
	t.Parallel()
	root, pkg := standaloneGateFixture(t)
	originals := map[string][]byte{
		"removed.txt":   []byte(" deleted original \n\n"),
		"binary.dat":    {0, 255, '\n'},
		"executable.sh": []byte("#!/bin/sh\necho mode\n"),
	}
	for path, body := range originals {
		if err := os.WriteFile(filepath.Join(root, path), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "update-index", "--chmod=+x", "executable.sh")
	runGit(t, root, "commit", "--no-verify", "--no-gpg-sign", "-m", "Native comparison materials")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "index value\n")
	runGit(t, root, "add", "delivery.txt")
	after := map[string][]byte{
		"delivery.txt": []byte(" actual working value \n\n"),
		"binary.dat":   {0, 254, '\n'},
		"new.txt":      []byte(" untracked original \n\n"),
		"empty.txt":    {},
	}
	for path, body := range after {
		if err := os.WriteFile(filepath.Join(root, path), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "update-index", "--chmod=-x", "executable.sh")
	head := standaloneGateHead(t, root)
	status := runGit(t, root, "status", "--porcelain=v1", "--untracked-files=all")
	index, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := CaptureStandaloneGateContext(root, "git", "")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := CaptureStandaloneGateContext(root, "git", "")
	if err != nil || !reflect.DeepEqual(context, repeated) {
		t.Fatalf("repeated original capture changed: %v", err)
	}
	actualIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil || !bytes.Equal(index, actualIndex) || standaloneGateHead(t, root) != head || runGit(t, root, "status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatalf("comparison modified native index/head/workspace: %v", err)
	}
	if len(context.Comparison) != 6 || len(context.LocationFacts) != 6 {
		t.Fatalf("complete comparison inventory: %+v", context)
	}
	for _, row := range context.Comparison {
		original := originals[row.Path]
		if row.Path == "delivery.txt" {
			original = []byte("delivery\n")
		}
		if !bytes.Equal(row.BaseContent, original) {
			t.Fatalf("%s original bytes: %q", row.Path, row.BaseContent)
		}
		current := after[row.Path]
		if row.Path == "executable.sh" {
			current = original
		}
		if !bytes.Equal(row.Content, current) {
			t.Fatalf("%s working bytes: %q", row.Path, row.Content)
		}
		switch row.Path {
		case "removed.txt":
			if row.Kind != "D" || row.Mode != "000000" {
				t.Fatalf("deleted native entry: %+v", row)
			}
		case "new.txt", "empty.txt":
			if row.Kind != "A" || row.BaseMode != "000000" {
				t.Fatalf("untracked native entry: %+v", row)
			}
		case "executable.sh":
			if row.BaseMode != "100755" || row.Mode != "100644" {
				t.Fatalf("mode-only native entry: %+v", row)
			}
		}
	}
	saved, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	var captured StandaloneGateContext
	if err := json.Unmarshal(saved, &captured); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "later unrelated contents\n")
	catalog, err := prompts.LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := ComposeStandaloneGatePrompt(catalog, "quality", captured)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"git status", "git diff", "svn status", "p4 opened", "Original report context", "later unrelated contents"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("review delegates or rediscovers mechanical material: %s", forbidden)
		}
	}
	rawComparison := strings.Split(strings.Split(prompt, "[Native comparison]\n")[1], "\n\n[Source locations]")[0]
	rawComparison = rawComparison[strings.Index(rawComparison, "\n[")+1:]
	var published []map[string]any
	if err := json.Unmarshal([]byte(rawComparison), &published); err != nil {
		t.Fatal(err)
	}
	for _, row := range published {
		path := row["path"].(string)
		if path == "binary.dat" {
			for key, want := range map[string][]byte{"before": originals[path], "after": after[path]} {
				got, err := base64.StdEncoding.DecodeString(row[key].(string))
				if err != nil || row[key+"Encoding"] != "base64" || !bytes.Equal(got, want) {
					t.Fatalf("binary %s original bytes: %+v %v", key, row, err)
				}
			}
		} else if path == "delivery.txt" && row["after"] != string(after[path]) {
			t.Fatalf("rendered after bytes changed: %+v", row)
		}
	}
	for i, fact := range captured.LocationFacts {
		selector := worker.Location{Read: i + 1, Line: 1}
		if fact.Kind == "listing" {
			selector = worker.Location{Listing: i + 1, Entry: 1}
		}
		if fact.Kind == "read" && len(fact.Content) == 0 {
			selector.Line = 0
		}
		wire, err := json.Marshal(map[string]any{"findings": []worker.Finding{{Severity: "P2", Message: "captured source", Locations: []worker.Location{selector}}}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := ValidateStandaloneGateResult(wire, captured)
		want := append([]string(nil), fact.Paths...)
		if selector.Line != 0 {
			want[0] = fmt.Sprintf("%s:%d", want[0], selector.Line)
		}
		if err != nil || len(result.Findings) != 1 || !reflect.DeepEqual(result.Findings[0].Locations, want) {
			t.Fatalf("original source selector %d: %+v %v", i+1, result, err)
		}
	}
	if _, err := CaptureStandaloneGateContext(t.TempDir(), "git", ""); err == nil {
		t.Fatal("native comparison failure was accepted")
	}
}
