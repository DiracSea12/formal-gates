package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

// TestCLIGateRunAndReportStandalone covers detached CLI commands: gate
// run assembles the standalone prompt (no requirement or dispatch block) and gate
// report resolves the original numeric source positions without changing run state.
func TestCLIGateRunAndReportStandalone(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	context := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(filepath.Join(root, "example.txt"), []byte("first line\nsecond line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prompt := runCLI(t, "gate", "run", "--root", root, "--package-root", pkg, "--vcs", "git", "--save-context", context, "quality")
	if strings.Contains(prompt, "first line") || strings.Contains(prompt, "second line") || !strings.Contains(prompt, `"read": 1`) || !strings.Contains(prompt, `"path": "example.txt"`) || !strings.Contains(prompt, context) {
		t.Fatal("gate task repeated source content or lost original location/context indexes", prompt)
	}
	data, err := os.ReadFile(context)
	if err != nil {
		t.Fatal(err)
	}
	var original validate.StandaloneGateContext
	if err := json.Unmarshal(data, &original); err != nil || len(original.LocationFacts) != 1 || original.LocationFacts[0].Paths[0] != "example.txt" || original.LocationFacts[0].Content != "first line\nsecond line\n" {
		t.Fatal("gate run did not persist its exact original source context", err)
	}
	if err := os.Remove(filepath.Join(root, "example.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "earlier.txt"), []byte("replacement source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, block := range []string{"[Shared reviewer contract]", "[Gate: quality]", "[Current change]", "[Result contract]"} {
		if !strings.Contains(prompt, block) {
			t.Fatalf("gate run prompt missing %s: %s", block, prompt)
		}
	}
	if strings.Contains(prompt, "[Current requirement]") || strings.Contains(prompt, "[Dispatch]") {
		t.Fatalf("gate run prompt must not carry requirement or dispatch blocks: %s", prompt)
	}

	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"gate", "run", "--root", root, "--package-root", pkg, "missing"}, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("gate run accepted an unknown gate id")
	}

	for _, sample := range []struct{ status, body, message string }{
		{"PASS", `{"findings":[]}`, ""},
		{"FAIL", `{"findings":[{"severity":"P1","message":"blocker","locations":[{"read":1,"line":2}]}]}`, "blocker"},
		{"RUNTIME_ERROR", `{"interruptionReason":"Native VCS unavailable"}`, "Native VCS unavailable"},
	} {
		t.Run(sample.status, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if code := Run("formal-gates", []string{"gate", "report", "--context", context}, IO{Stdin: strings.NewReader(sample.body), Stdout: &stdout, Stderr: &stderr}); code != 0 {
				t.Fatalf("gate report rejected a valid %s result: %s", sample.status, stderr.String())
			}
			for _, want := range []string{"脱离 run 的快速检查", "status: " + sample.status, sample.message} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("gate report display missing %q: %s", want, stdout.String())
				}
			}
			if sample.status == "FAIL" && !strings.Contains(stdout.String(), "example.txt:2") {
				t.Fatal("gate report rediscovered or rebound the original source", stdout.String())
			}
		})
	}
	for _, body := range []string{`{"findings":[{"severity":"P3","message":"position","locations":["example.txt:2"]}]}`, `{"findings":[{"severity":"P3","message":"position","locations":[{"read":2}]}]}`} {
		stdout.Reset()
		stderr.Reset()
		if code := Run("formal-gates", []string{"gate", "report", "--context", context}, IO{Stdin: strings.NewReader(body), Stdout: &stdout, Stderr: &stderr}); code == 0 {
			t.Fatal("gate report accepted a copied or unpublished position")
		}
	}
}

func TestStandaloneGateStartupSelectsPublishedCatalogPositions(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	if err := os.WriteFile(filepath.Join(pkg, "gates", "other.md"), []byte("# Another gate\nCheck the current changes.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "example.txt"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := Run("formal-gates", []string{"gate", "run", "--root", root, "--package-root", pkg}, IO{Stdout: &out, Stderr: &errout}); code != 0 {
		t.Fatal(errout.String())
	}
	var choices []struct {
		Gate int    `json:"gate"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out.Bytes(), &choices); err != nil || len(choices) != 2 {
		t.Fatal("original catalog was not published", out.String(), err)
	}
	for _, choice := range choices {
		out.Reset()
		errout.Reset()
		if code := Run("formal-gates", []string{"gate", "run", "--root", root, "--package-root", pkg, "--gate", strconv.Itoa(choice.Gate)}, IO{Stdout: &out, Stderr: &errout}); code != 0 {
			t.Fatal(errout.String())
		}
		if !strings.Contains(out.String(), "[Gate: "+choice.Name+"]") {
			t.Fatal("selected position rebound catalog", out.String())
		}
	}
	contexts, _ := filepath.Glob(filepath.Join(root, ".gates", "tmp", "gate-review", "review-*.json"))
	if len(contexts) != len(choices) {
		t.Fatal("original comparison payloads not saved")
	}
	out.Reset()
	errout.Reset()
	if code := Run("formal-gates", []string{"gate", "run", "--root", root, "--package-root", pkg, "--gate", "3"}, IO{Stdout: &out, Stderr: &errout}); code == 0 {
		t.Fatal("unpublished gate accepted")
	}
	after, _ := filepath.Glob(filepath.Join(root, ".gates", "tmp", "gate-review", "review-*.json"))
	if len(after) != len(contexts) {
		t.Fatal("invalid selection created a review")
	}
	if body, _ := os.ReadFile(filepath.Join(root, "example.txt")); string(body) != "original\n" {
		t.Fatal("standalone gate changed source")
	}
}
