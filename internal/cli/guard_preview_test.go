package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestGuardPreviewBinary(t *testing.T) {
	// Execute the formal build in an unrelated working directory. Assertions only
	// inspect the documented input/output and filesystem; no internal calculator is
	// imported or used as a black-box oracle.
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Windows cannot exec a PE file without the .exe suffix; the package's
	// shared helper already names the platform-correct binary.
	binary := filepath.Join(t.TempDir(), installTestBinaryName())
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fixture, err := os.ReadFile(filepath.Join(repo, "references", "guard-preview-basis.json"))
	if err != nil {
		t.Fatal(err)
	}
	inputDir, work := t.TempDir(), t.TempDir()
	run := func(args ...string) ([]byte, string, error) {
		cmd := exec.Command(binary, append([]string{"workflow", "guard-preview"}, args...)...)
		cmd.Dir = work
		cmd.Env = hostFilteredEnv(nil)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.String(), err
	}
	inputPath := filepath.Join(inputDir, "basis.json")
	mustWriteCLI(t, inputPath, string(fixture))
	first, stderr, err := run("--input", inputPath)
	if err != nil {
		t.Fatalf("preview: %v %s", err, stderr)
	}
	var result struct {
		RequestAllowance, RetryAllowance, TotalAllowance, DistinctExternalTaskKeys int
		RuntimeIntegration, BudgetBasisDigest                                      string
		Basis                                                                      json.RawMessage
	}
	if err := json.Unmarshal(first, &result); err != nil {
		t.Fatal(err)
	}
	if result.RequestAllowance != 3 || result.RetryAllowance != 4 || result.TotalAllowance != 7 || result.DistinctExternalTaskKeys != 3 || result.RuntimeIntegration != protocol.GuardRuntimeIntegration || len(result.Basis) == 0 || !strings.HasPrefix(result.BudgetBasisDigest, "sha256:") {
		t.Fatalf("result: %s", first)
	}
	if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
		t.Fatalf("preview created files: %v %v", entries, err)
	}
	if result.BudgetBasisDigest != "sha256:692f7b9bb402a8762c0f1ccffdfbaab6c7e9f48964e2f4ca54b2236e45e1bcef" {
		t.Fatalf("documented digest: %s", result.BudgetBasisDigest)
	}
	for _, tc := range []struct {
		name                  string
		change                func(map[string]any)
		request, retry, total int
	}{
		{"zero", func(b map[string]any) { b["externalTasks"] = []any{} }, 0, 0, 0},
		{"duplicate and order", func(b map[string]any) {
			tasks := b["externalTasks"].([]any)
			b["externalTasks"] = []any{tasks[2], tasks[1], tasks[0], tasks[1]}
		}, 3, 4, 7},
		{"extra attempt", func(b map[string]any) {
			b["externalTasks"].([]any)[0].(map[string]any)["maxAttempts"] = 2
		}, 3, 5, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b map[string]any
			if err := json.Unmarshal(fixture, &b); err != nil {
				t.Fatal(err)
			}
			tc.change(b)
			data, _ := json.Marshal(b)
			mustWriteCLI(t, inputPath, string(data))
			out, stderr, err := run("--input", inputPath)
			if err != nil {
				t.Fatalf("preview: %v %s", err, stderr)
			}
			var got struct {
				RequestAllowance, RetryAllowance, TotalAllowance int
				BudgetBasisDigest                                string
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if got.RequestAllowance != tc.request || got.RetryAllowance != tc.retry || got.TotalAllowance != tc.total {
				t.Fatalf("%s: %s", tc.name, out)
			}
			if tc.name == "duplicate and order" && !bytes.Equal(first, out) {
				t.Fatal("duplicates/order changed canonical output")
			}
			if tc.name == "extra attempt" && got.BudgetBasisDigest == result.BudgetBasisDigest {
				t.Fatal("retry policy change did not change digest")
			}
		})
	}

	// A pre-existing run is untouched on both success and ordinary input errors.
	statePath := filepath.Join(work, ".gates", "tmp", "existing", "state.json")
	mustWriteCLI(t, statePath, `{"status":"ACTIVE"}`)
	before, _ := os.Stat(statePath)
	for _, tc := range []struct {
		name, message string
		change        func(map[string]any)
	}{
		{"repeat", "", func(b map[string]any) {}},
		{"schema", "schemaVersion", func(b map[string]any) { b["schemaVersion"] = 2 }},
		{"unknown field", "unknown field", func(b map[string]any) { b["allowance"] = 100 }},
		{"confirmation", "confirmedRevision", func(b map[string]any) { delete(b["batch"].(map[string]any), "confirmedRevision") }},
		{"missing identity", "definition.id", func(b map[string]any) { delete(b["definition"].(map[string]any), "id") }},
		{"retry", "maxAttempts", func(b map[string]any) { b["externalTasks"].([]any)[0].(map[string]any)["maxAttempts"] = 0 }},
		{"fractional retry", "maxAttempts", func(b map[string]any) { b["externalTasks"].([]any)[0].(map[string]any)["maxAttempts"] = 1.5 }},
		{"task identity", "taskKey", func(b map[string]any) { delete(b["externalTasks"].([]any)[0].(map[string]any), "taskKey") }},
		{"retry conflict", "conflicting declarations", func(b map[string]any) {
			tasks := b["externalTasks"].([]any)
			extra := map[string]any{}
			for key, value := range tasks[0].(map[string]any) {
				extra[key] = value
			}
			extra["maxAttempts"] = 3
			b["externalTasks"] = append(tasks, extra)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b map[string]any
			if err := json.Unmarshal(fixture, &b); err != nil {
				t.Fatal(err)
			}
			tc.change(b)
			data, _ := json.Marshal(b)
			mustWriteCLI(t, inputPath, string(data))
			out, stderr, err := run("--input", inputPath)
			if tc.message == "" {
				if err != nil || !bytes.Equal(out, first) {
					t.Fatalf("unstable preview: %s %s %v", out, stderr, err)
				}
			} else if err == nil || !strings.Contains(stderr, tc.message) || !strings.Contains(stderr, inputPath) || len(out) != 0 {
				t.Fatalf("want %s, got %s %s %v", tc.message, out, stderr, err)
			}
		})
	}
	for _, invalid := range []string{`{`, `null`, string(fixture) + ` {}`} {
		mustWriteCLI(t, inputPath, invalid)
		if _, stderr, err := run("--input", inputPath); err == nil || !strings.Contains(stderr, inputPath) {
			t.Fatalf("invalid JSON accepted: %s %v", stderr, err)
		}
	}
	if _, stderr, err := run("--input", filepath.Join(inputDir, "missing.json")); err == nil || !strings.Contains(stderr, "missing.json") {
		t.Fatalf("missing file: %s %v", stderr, err)
	}
	if _, _, err := run(); err == nil {
		t.Fatal("missing --input accepted")
	}
	if _, _, err := run("--help"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(statePath)
	contents, _ := os.ReadFile(statePath)
	if string(contents) != `{"status":"ACTIVE"}` || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("existing run modified")
	}
	var files []string
	if err := filepath.Walk(work, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(work, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{".gates/tmp/existing/state.json"}) {
		t.Fatalf("unexpected files: %v", files)
	}
}
