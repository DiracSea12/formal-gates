//go:build qastructure

package validate

// These tests pin launcher-driven admission discovery: a workflow start from
// the fixed stable launcher discovers its registry binding, while the same
// invocation from a candidate copy of the executable is fenced before any run
// state exists. They use fresh fixtures independent of the shared helpers.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func launcherWriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func launcherRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func launcherCopyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	launcherWriteFile(t, destination, string(data), 0o700)
}

func launcherRepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func launcherChildEnvironment(overrides map[string]string) []string {
	skip := map[string]bool{}
	for key := range overrides {
		skip[key] = true
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		if !skip[key] {
			environment = append(environment, item)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func TestStableLauncherDiscoveryFencesCandidateWrites(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	testkit.ConfigureNativePaths(t, root, "codex")
	launcherWriteFile(t, filepath.Join(root, "requirements.md"), "## 需求点\n\n### REQ-001：Launcher discovery\n\n#### 要求\n\nThe stable launcher binds its registered admission.\n\n#### 验收条件\n\n- AC-001：The confirmed intake starts under the stable launcher's record.\n\n#### 来源\n\nTest fixture.\n", 0600)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Launcher Test"}, {"config", "user.email", "launcher@example.invalid"}, {"add", "requirements.md"}, {"commit", "-qm", "base"}} {
		launcherRun(t, root, "git", args...)
	}
	home := t.TempDir()
	source := copyPackageFixture(t)
	binaryPath := filepath.Join(source, "bin", nativeBinaryName())
	if err := os.Remove(binaryPath); err != nil {
		t.Fatal(err)
	}
	launcherRun(t, launcherRepoRoot(t), "go", "build", "-o", binaryPath, "./cmd/formal-gates")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	candidate := filepath.Join(home, "candidate", nativeBinaryName())
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	options := InstallOptions{Source: source, Host: "codex", Scope: "project", Project: root, RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	record := admissionRecordByID(t, document, report.Targets[0].RegistryRecordID)
	packageRoot := record.Target
	launcherCopyExecutable(t, launcher, candidate)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	intake, err := facade.SaveOriginalCandidate(filepath.Join(root, ".gates", "tmp", "intake"), preview.IntakeCandidate)
	if err != nil {
		t.Fatal(err)
	}
	baseEnvironment := map[string]string{
		"HOME": home, "USERPROFILE": home,
		"LAUNCHER_DISCOVERY_ROOT":    root,
		"LAUNCHER_DISCOVERY_PACKAGE": packageRoot,
	}
	for _, invocation := range []struct {
		mode       string
		executable string
	}{
		{mode: "stable", executable: launcher},
		{mode: "candidate", executable: candidate},
	} {
		t.Run(invocation.mode, func(t *testing.T) {
			environment := make(map[string]string, len(baseEnvironment)+1)
			for key, value := range baseEnvironment {
				environment[key] = value
			}
			before, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(invocation.executable, "workflow", "serve")
			command.Dir = root
			command.Env = testkit.HostFilteredEnvironment(launcherChildEnvironment(environment))
			output, childErr := launcherStartIntake(t, command, root, intake)
			if invocation.mode == "stable" {
				if childErr != nil {
					t.Fatalf("stable launcher failed: %v\n%s", childErr, output)
				}
				var started struct {
					RunID string `json:"runId"`
				}
				if err := json.Unmarshal(output, &started); err != nil || started.RunID == "" {
					t.Fatalf("start response lost its generated run identity: %s %v", output, err)
				}
				persisted := admissionCurrentRequest(t, root, started.RunID)
				if persisted.AdmissionRegistryPath != registry || persisted.InstalledTargetIdentity != record.ID || persisted.AdmissionRoot != root || persisted.AdmissionTarget != packageRoot || persisted.AdmissionGeneration != record.Generation || persisted.AdmissionLease != record.Lease || persisted.AdmissionToken != record.Token {
					t.Fatalf("persisted exact launcher binding lost: %+v", persisted)
				}
				if persisted.IntakeConfirmationReceipt.Confirmation != "conversation" || persisted.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest {
					t.Fatal("launcher startup lost intake or conversation provenance", persisted)
				}
				materials, err := facade.OriginalPayloadPaths(filepath.Join(root, facade.EngineNamespace, started.RunID, "payloads", "intake"))
				if err != nil || len(materials) != 1 {
					t.Fatal("launcher startup lost original materials", materials, err)
				}
				body, err := os.ReadFile(materials[0])
				var stored facade.IntakePreview
				if err != nil || json.Unmarshal(body, &stored) != nil || stored.Content != preview.Content || stored.Revision != preview.Revision {
					t.Fatal("launcher startup changed original material content", stored, err)
				}
				after, err := LoadRegistry(registry)
				if err != nil || after.Epoch != document.Epoch {
					t.Fatalf("launcher start changed registry epoch: %+v %v", after, err)
				}
			} else {
				if childErr == nil || !strings.Contains(string(output), "UNREGISTERED_INSTALL:") || !strings.Contains(string(output), "no active registry record matches candidate package root") {
					t.Fatalf("candidate not fenced by actual launcher owner: %v\n%s", childErr, output)
				}
				after, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
				if err != nil || strings.Join(before, "\n") != strings.Join(after, "\n") {
					t.Fatalf("candidate rejection created authority: before=%v after=%v err=%v", before, after, err)
				}
			}
		})
	}
}

// launcherStartIntake submits a conversation-authorized startup over the
// installed service and rejects any unexpected native confirmation request.
func launcherStartIntake(t *testing.T, command *exec.Cmd, root string, intake int) ([]byte, error) {
	t.Helper()
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer command.Wait()
	defer command.Process.Kill()
	defer input.Close()
	encode, decode := json.NewEncoder(input), json.NewDecoder(output)
	if err := encode.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}}}); err != nil {
		return nil, err
	}
	var initialized struct {
		ID     int `json:"id"`
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := decode.Decode(&initialized); err != nil {
		return nil, err
	}
	if initialized.ID != 1 || initialized.Result.ServerInfo.Name != "formal-gates" {
		return nil, fmt.Errorf("unexpected initialize response: %+v", initialized)
	}
	if err := encode.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "confirm_start", "arguments": map[string]any{"root": root, "intake": intake}}}); err != nil {
		return nil, err
	}
	var frame struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := decode.Decode(&frame); err != nil {
		return nil, fmt.Errorf("%w; stderr=%s", err, stderr.String())
	}
	if frame.ID != 2 || frame.Method != "" {
		return nil, fmt.Errorf("unexpected startup response: %+v", frame)
	}
	var body []byte
	for _, content := range frame.Result.Content {
		if content.Type == "text" {
			body = append(body, []byte(content.Text)...)
		}
	}
	if len(frame.Error) != 0 {
		return body, fmt.Errorf("%s", frame.Error)
	}
	if frame.Result.IsError {
		return body, fmt.Errorf("%s", body)
	}
	return body, nil
}
