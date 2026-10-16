package cli

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
	"formal-gates/internal/validate"
)

func TestInstalledRuntimeInventoryStaysOutsideOriginalProjectMaterials(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	clearHostEnv(t)
	source := writeInstallSource(t, "Installed runtime Submit marker")
	if err := os.RemoveAll(filepath.Join(source, "prompts", "actions")); err != nil {
		t.Fatal(err)
	}
	for _, role := range prompts.RequiredActionIDs {
		mustWriteCLI(t, filepath.Join(source, "prompts", "actions", role+".md"), "Perform only the issued semantic duty.")
	}

	project := t.TempDir()
	testkit.ConfigureNativePaths(t, project, "codex")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", installTestBinaryName())
	previous := os.Args[0]
	os.Args[0] = launcher
	defer func() { os.Args[0] = previous }()
	report, err := validate.Install(validate.InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0].TargetPath
	if _, err := validate.Install(validate.InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, BinaryTarget: launcher, Bootstrap: true}); err != nil {
		t.Fatal(err)
	}
	admission, err := validate.ResolveEngineAdmission(project, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(admission.RuntimeFiles.Files) == 0 {
		t.Fatal("no verified installed leaves")
	}
	mustWriteCLI(t, filepath.Join(project, ".codex", "user-notes.md"), "Submit original project note\n")
	mustWriteCLI(t, filepath.Join(project, "requirements.md"), cliStructuredRequirement("Return accepted output.")+"\n## 方案\n\nUse the existing output owner.\n")
	var output, failures bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--root", project, "--precheck", "--materials"}, IO{Stdout: &output, Stderr: &failures}); code != 0 {
		t.Fatal(code, &failures)
	}
	var choices []requirementDocumentChoice
	if err := json.Unmarshal(output.Bytes(), &choices); err != nil {
		t.Fatal(err)
	}
	note := false
	for _, item := range choices {
		if admission.RuntimeFiles.ContainsFile(filepath.Join(project, filepath.FromSlash(item.Path))) {
			t.Fatal("installed leaf was published", item)
		}
		note = note || item.Path == ".codex/user-notes.md"
	}
	if !note {
		t.Fatal("ordinary host-adjacent user material was hidden")
	}
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "requirements.md", "AGENTS.md", ".codex/hooks.json", ".codex/user-notes.md"}, {"commit", "-m", "Confirmed inputs"}} {
		cliGit(t, project, args...)
	}
	base := strings.TrimSpace(cliGit(t, project, "rev-parse", "HEAD"))
	intake, err := facade.PrepareIntake(project, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	f, run, err := facade.Start(facade.StartOptions{Root: project, ArtifactRoot: project, RuntimeFiles: admission.RuntimeFiles, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}, Request: facade.StartRequest{RunID: "inventory", Route: "regular", VCS: "git", Provider: admission.Provider, BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: intake.IntakeCandidate}})
	if err != nil {
		t.Fatal(err)
	}
	for len(run.WorkerMaterials) == 0 {
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		if run.Next.Operator != nil {
			t.Fatal(run.Next)
		}
	}
	reopened, err := facade.Open(project, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, material := range run.WorkerMaterials {
		request, _, directory, _, err := reopened.WorkerExecutionRequest(material, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(request)
		if err := os.WriteFile(filepath.Join(directory, "native-request.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			var result, errors bytes.Buffer
			query := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"search","query":"Submit"}}}`
			if code := harness.NativeTools(directory, strings.NewReader(query), &result, &errors, harness.ExecutionServices{Admit: facade.AdmitExecution}); code != 0 {
				t.Fatal(code, &errors)
			}
			var reply struct {
				Result struct {
					Content []struct{ Text string }
					IsError bool
				}
			}
			if err := json.Unmarshal(result.Bytes(), &reply); err != nil || reply.Result.IsError {
				t.Fatal(result.String(), err)
			}
			var matches struct{ Matches []struct{ Path string } }
			if err := json.Unmarshal([]byte(reply.Result.Content[0].Text), &matches); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, match := range matches.Matches {
				if admission.RuntimeFiles.ContainsFile(filepath.Join(project, filepath.FromSlash(match.Path))) {
					t.Fatal("original admitted search published installed runtime", match)
				}
				found = found || match.Path == ".codex/user-notes.md"
			}
			if !found {
				t.Fatal("original user content was not read", result.String())
			}
		}
		changed := map[string]any{}
		for key, value := range request {
			changed[key] = value
		}
		changed["runtime_files"] = map[string]any{"files": []string{filepath.Join(project, ".codex/user-notes.md")}}
		if release, err := facade.AdmitExecution(directory, changed); err == nil {
			if release != nil {
				release()
			}
			t.Fatal("request replaced its original installed inventory")
		}
	}
	// A registered damaged target is an error, not an uninstalled discovery fallback.
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte("changed runtime"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := intakeMaterials(project); err == nil {
		t.Fatal("damaged installed inventory was treated as an uninstalled project")
	}
	t.Logf("default registry and original launcher provided %d verified leaves; precheck and two reopened admitted searches preserved user notes and rejected inventory replacement", len(admission.RuntimeFiles.Files))
}
