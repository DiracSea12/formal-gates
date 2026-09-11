//go:build phase0whitebox

package validate

import

// This file is the independently authored whitebox QA delivery for the
// current incremental round. Each exported TestWhiteboxPhase0Round16*
// function is bound to exactly one QA case and exercises the implementation
// at the lowest responsibility that owns the behavior: the first-start
// bootstrap receipt boundary (Start), the pre-write candidate admission gate
// (mutateRun prepare flows and workflow cleanup --run), the manifest
// host-target registration check (Install), the global project-local sibling
// identity across upgrade and bootstrap (Install/bootstrap), the installed
// digest admission check (AdmitRegistry), and the Codex hook canary
// text-only-session retry. The tests build their own fixtures and do not
// call or name any development-worker test.
(
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func round16Write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func round16Read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func round16Run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func round16GitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	round16Write(t, filepath.Join(root, "requirements.md"), "round sixteen requirement\n", 0o600)
	round16Run(t, root, "git", "init")
	round16Run(t, root, "git", "config", "user.email", "round16@example.invalid")
	round16Run(t, root, "git", "config", "user.name", "Round16 Whitebox QA")
	round16Run(t, root, "git", "add", "requirements.md")
	round16Run(t, root, "git", "commit", "-m", "baseline")
	return root
}

// round16Manifest registers every installable host target so install only
// rejects a host when a scenario explicitly mutates the registration.
const round16Manifest = `{"name":"formal-gates","hosts":[
  {"name": "Claude Code", "support": "host-target"},
  {"name": "Codex", "support": "host-target"},
  {"name": "Cursor", "support": "host-target"},
  {"name": "DeepSeek Harness", "support": "host-target"}
]}
`

// round16Source builds a complete runtime install fixture: the managed-rule
// block, the full prompt catalog required by workflow start, and a shebang
// placeholder binary accepted by the installed-binary smoke.
func round16Source(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" +
		hostInstructionsStartMarker + "\nround sixteen managed rule\n" + hostInstructionsEndMarker + "\n"
	round16Write(t, filepath.Join(root, "SKILL.md"), skill, 0o600)
	round16Write(t, filepath.Join(root, "README.md"), "round sixteen runtime\n", 0o600)
	round16Write(t, filepath.Join(root, "README_EN.md"), "round sixteen runtime en\n", 0o600)
	round16Write(t, filepath.Join(root, "formal-gates.manifest.json"), round16Manifest, 0o600)
	round16Write(t, filepath.Join(root, "bin", nativeBinaryName()), "#!/bin/sh\nexit 0\n", 0o755)
	round16Write(t, filepath.Join(root, "prompts", "reviewer-base.md"), "round sixteen reviewer base\n", 0o600)
	for _, action := range []string{
		"carry", "development-worker", "product-review", "qa-design",
		"qa-execution", "qa-review", "requirements-clarification", "start-readiness",
	} {
		round16Write(t, filepath.Join(root, "prompts", "actions", action+".md"), action+" prompt\n", 0o600)
	}
	round16Write(t, filepath.Join(root, "gates", "round16-gate.md"), "# round sixteen gate\n", 0o600)
	round16Write(t, filepath.Join(root, "agents", "agent.md"), "agent\n", 0o600)
	round16Write(t, filepath.Join(root, "references", "reference.md"), "reference\n", 0o600)
	return root
}

// round16InstallProject performs the documented native install plus the
// documented install --bootstrap maintenance entry for one project-scope
// claude target, returning the admission identity workflow start needs.
func round16InstallProject(t *testing.T, source, project, registry, launcher string) (string, string) {
	t.Helper()
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("round16 native install failed: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("round16 install reported %d targets: %+v", len(report.Targets), report)
	}
	if _, statErr := os.Stat(registry + ".bootstrap.json"); !os.IsNotExist(statErr) {
		t.Fatalf("plain install committed a bootstrap receipt: %v", statErr)
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("round16 documented bootstrap entry failed: %v", err)
	}
	return report.Targets[0].TargetPath, report.Targets[0].RegistryRecordID
}

// round16SetRecordStatus flips one record's status through the one semantic
// registry transaction owner while holding the shared registry lock.
func round16SetRecordStatus(t *testing.T, registry, recordID, status string) {
	t.Helper()
	unlock, err := acquireRegistryLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	document, err := loadRegistryForCommit(registry)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range document.Records {
		if document.Records[index].ID == recordID {
			document.Records[index].Status = status
			found = true
		}
	}
	if !found {
		t.Fatalf("registry record %q was not found for status flip", recordID)
	}
	if _, err := commitRegistryRecordsUnlocked(registry, document, document.Records); err != nil {
		t.Fatal(err)
	}
}

func round16RecordByID(t *testing.T, document RegistryDocument, id string) RegistryRecord {
	t.Helper()
	for _, record := range document.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("registry record %q was not found", id)
	return RegistryRecord{}
}

// TestWhiteboxPhase0Round16InstallRejectsManifestUnregisteredHostTarget pins
// the payload-manifest host-target registration check: an install into a
// host the payload manifest does not register as an installable host-target
// (downgraded support or a missing entry) must be rejected before any
// target, launcher, or registry is created, while the registered manifest
// installs normally.
func TestWhiteboxPhase0Round16InstallRejectsManifestUnregisteredHostTarget(t *testing.T) {
	const claudeEntry = `{"name": "Claude Code", "support": "host-target"}`
	scenarios := []struct {
		name     string
		manifest string
		rejects  bool
	}{
		{name: "registered-control", manifest: round16Manifest, rejects: false},
		{
			name: "downgraded-support",
			manifest: strings.Replace(round16Manifest, claudeEntry,
				`{"name": "Claude Code", "support": "explanation-level"}`, 1),
			rejects: true,
		},
		{
			name:     "missing-entry",
			manifest: strings.Replace(round16Manifest, claudeEntry+",\n  ", "", 1),
			rejects:  true,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			source := round16Source(t)
			round16Write(t, filepath.Join(source, "formal-gates.manifest.json"), scenario.manifest, 0o600)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
			report, err := Install(InstallOptions{
				Source: source, Host: "claude", Scope: "project", Project: project,
				RegistryPath: registry, BinaryTarget: launcher, Force: true,
			})
			if scenario.rejects {
				if err == nil || !strings.Contains(err.Error(), "does not register") || !strings.Contains(err.Error(), "Claude Code") {
					t.Fatalf("install accepted a manifest-unregistered host target: %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install left a target behind: %v", statErr)
				}
				if _, statErr := os.Stat(launcher); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install left a stable launcher behind: %v", statErr)
				}
				if _, statErr := os.Stat(registry); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install created a registry: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(project, ".gates")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install created workflow state roots: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("registered manifest install failed: %v", err)
			}
			if len(report.Targets) != 1 || !isFile(registry) {
				t.Fatalf("registered manifest install did not commit its target and registry: %+v", report)
			}
		})
	}
}

// round16FakeCodex writes a stub codex command used through the documented
// CodexCommand option. It answers --version and "exec --help" (exposing
// --profile) and drives the canary session according to FAKE_CODEX_MODE:
// "retry-proof" emits no PreToolUse payload on the first exec run and one on
// the second, "never" never emits a payload, and "marker-proof" emits a
// payload together with the forbidden marker file on the first run.
func round16FakeCodex(t *testing.T, stateFile string) string {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo "fake-codex 0.0.0"
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  echo "usage: codex exec --profile NAME"
  exit 0
fi
runs=0
if [ -f %q ]; then
  runs=$(cat %q)
fi
runs=$((runs+1))
printf '%%s\n' "$runs" > %q
mode="${FAKE_CODEX_MODE:-never}"
if [ "$mode" = "marker-proof" ]; then
  for dir in "$PWD"/.gates/tmp/codex-hook-canary/*/payloads; do
    [ -d "$dir" ] || continue
    printf '{"hook_event_name":"PreToolUse","tool_name":"shell"}\n' > "$dir/hook-PreToolUse-shell-fake-codex.json"
    printf 'HIT\n' > "$dir/../marker.txt"
  done
  exit 0
fi
if [ "$mode" = "retry-proof" ] && [ "$runs" -ge 2 ]; then
  for dir in "$PWD"/.gates/tmp/codex-hook-canary/*/payloads; do
    [ -d "$dir" ] || continue
    printf '{"hook_event_name":"PreToolUse","tool_name":"shell"}\n' > "$dir/hook-PreToolUse-shell-fake-codex.json"
  done
fi
exit 0
`, stateFile, stateFile, stateFile)
	path := filepath.Join(t.TempDir(), "fake-codex")
	round16Write(t, path, script, 0o755)
	return path
}

// TestWhiteboxPhase0Round16CodexHookCanaryRetriesTextOnlySession pins the
// documented live-proof retry behavior of the Codex hook canary: a driven
// session that answers the prompt in text (no PreToolUse payload, no marker)
// is retried exactly once and passes when the retry produces the payload
// proof; a session that never emits a payload stays FAIL after the single
// retry, and a session whose proof includes the forbidden marker fails
// without consuming a retry.
func TestWhiteboxPhase0Round16CodexHookCanaryRetriesTextOnlySession(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		wantStatus  string
		wantAttempt int
		wantReason  string
	}{
		{name: "text-only-then-retry-proof", mode: "retry-proof", wantStatus: "PASS", wantAttempt: 2},
		{name: "never-emits-payload", mode: "never", wantStatus: "FAIL", wantAttempt: 2, wantReason: "no PreToolUse"},
		{name: "marker-proof-no-retry", mode: "marker-proof", wantStatus: "FAIL", wantAttempt: 1, wantReason: "marker file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worktree := t.TempDir()
			t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
			t.Setenv("FAKE_CODEX_MODE", tc.mode)
			stateFile := filepath.Join(t.TempDir(), "runs")
			stub := round16FakeCodex(t, stateFile)
			summary, result := CodexHookCanary(CodexHookCanaryOptions{
				Worktree: worktree, CodexCommand: stub, TimeoutSeconds: 60, KeepTemp: true,
			})
			if summary.Status != tc.wantStatus {
				t.Fatalf("canary status=%q want %q (summary=%+v)", summary.Status, tc.wantStatus, summary)
			}
			if summary.Attempts != tc.wantAttempt {
				t.Fatalf("canary attempts=%d want %d (summary=%+v)", summary.Attempts, tc.wantAttempt, summary)
			}
			if tc.wantStatus == "PASS" {
				if !result.OK() {
					t.Fatalf("passing canary reported failures: %#v", result.Failures)
				}
				if summary.PreToolUsePayloadCount == 0 || summary.MarkerExists {
					t.Fatalf("passing canary lost its proof conditions: %+v", summary)
				}
				if !strings.Contains(summary.Diagnostic, "retry") {
					t.Fatalf("retry-produced proof was not diagnosed: %+v", summary)
				}
				if !isFile(filepath.Join(summary.ArtifactDir, "payloads", "hook-PreToolUse-shell-fake-codex.json")) {
					t.Fatalf("retry proof payload was not kept as evidence: %+v", summary)
				}
				return
			}
			if result.OK() {
				t.Fatalf("failing canary reported no failures: %#v", result.Failures)
			}
			if !strings.Contains(summary.FailureReason, tc.wantReason) {
				t.Fatalf("failure reason %q does not contain %q: %+v", summary.FailureReason, tc.wantReason, summary)
			}
			if tc.mode == "marker-proof" && !summary.MarkerExists {
				t.Fatalf("marker-proof session did not record the forbidden marker: %+v", summary)
			}
		})
	}
}
