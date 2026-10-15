package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/testkit"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/prompts"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

func TestWorkflowStartUsesParsedPackageRootForAdmission(t *testing.T) {
	clearHostEnv(t)
	source := writeInstallSource(t, "Installed workflow admission")
	if err := os.Remove(filepath.Join(source, "prompts", "actions", "sample-action.md")); err != nil {
		t.Fatal(err)
	}
	for _, id := range prompts.RequiredActionIDs {
		mustWriteCLI(t, filepath.Join(source, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	root := t.TempDir()
	testkit.ConfigureNativePaths(t, root, "zcode", "codex")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", installTestBinaryName())
	previous := os.Args[0]
	os.Args[0] = launcher
	t.Cleanup(func() { os.Args[0] = previous })
	options := []validate.InstallOptions{
		{Source: source, Host: "zcode", Scope: "global", BinaryTarget: launcher, Force: true},
		{Source: source, Host: "codex", Scope: "project", Project: root, BinaryTarget: launcher, Force: true},
	}
	var targets []string
	for _, option := range options {
		report, err := validate.Install(option)
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, report.Targets[0].TargetPath)
	}
	bootstrap := options[1]
	bootstrap.Bootstrap = true
	if _, err := validate.Install(bootstrap); err != nil {
		t.Fatal(err)
	}
	explicit, err := validate.ResolveEngineAdmission(root, targets[0])
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if explicit.RecordID == automatic.RecordID {
		t.Fatal("explicit global and automatic project admissions are indistinguishable")
	}
	candidate := writerIntakeReceipt(t, root)
	candidatePath := filepath.Join(t.TempDir(), "intake.json")
	if err := saveWorkflowCandidate(candidatePath, candidate); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		flags []string
		want  validate.EngineAdmission
	}{
		{"explicit", []string{"--package-root", targets[0]}, explicit},
		{"last nonempty value", []string{"--package-root=", "--package-root", targets[0]}, explicit},
		{"single dash", []string{"-package-root", targets[0]}, explicit},
		{"last empty value", []string{"--package-root", targets[0], "--package-root="}, automatic},
		{"empty value", []string{"--package-root", ""}, automatic},
		{"omitted", nil, automatic},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			runID := strings.ReplaceAll(test.name, " ", "-")
			args := []string{"workflow", "start", "--root", root, "--run-id", runID, "--route", "lightweight", "--intake-receipt", candidatePath, "--machine"}
			args = append(args, test.flags...)
			if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 || stderr.Len() != 0 {
				t.Fatalf("start: %d %s %s", code, &out, &stderr)
			}
			var run facade.Run
			if err := json.Unmarshal(out.Bytes(), &run); err != nil || run.RunID != runID || run.Route != "lightweight" || run.Status != "ACTIVE" {
				t.Fatalf("start output: %+v %v", run, err)
			}
			data, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "request.json"))
			var request facade.StartRequest
			if err != nil || json.Unmarshal(data, &request) != nil {
				t.Fatalf("start request: %s %v", data, err)
			}
			if request.AdmissionTarget != test.want.Target || request.InstalledTargetIdentity != test.want.RecordID || request.PackageDigest != test.want.PackageDigest || request.Provider != test.want.Provider || !reflect.DeepEqual(request.IntakeConfirmationReceipt, candidate) {
				t.Fatalf("parsed package root lost its admission or confirmed input: %+v", request)
			}
		})
	}
}

func TestWorkflowStartRequiresRouteAfterStructuredPrecheck(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "requirements.md"}, {"commit", "-m", "Confirmed input"}} {
		cliGit(t, root, args...)
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	var out, stderr bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &stderr}
	candidatePath := filepath.Join(t.TempDir(), "intake.json")
	if code := runWithHostConfirmation(t, []string{"workflow", "start", "--precheck", "--root", root, "--requirement", "requirements.md", "--save-candidate", candidatePath}, streams); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.RequirementDocument == nil || preview.IntakeCandidate.RequirementRevision != preview.RequirementDocument.ContentDigest {
		t.Fatalf("omitted precheck route lost structured binding: %+v %v", preview, err)
	}
	raw, err := os.ReadFile(candidatePath)
	var candidate facade.IntakeConfirmationReceipt
	if err != nil || json.Unmarshal(raw, &candidate) != nil || !reflect.DeepEqual(candidate, preview.IntakeCandidate) {
		t.Fatal("saved candidate differs from displayed material", err)
	}
	for _, args := range [][]string{
		{"workflow", "start", "--root", root, "--run-id", "missing-route", "--intake-receipt", candidatePath},
		{"workflow", "start", "--root", root, "--run-id", "missing-route", "--intake-receipt", candidatePath, "--route", " "},
	} {
		out.Reset()
		stderr.Reset()
		if code := Run("formal-gates", args, streams); code == 0 || !strings.Contains(stderr.String(), "requires --route regular or lightweight") || out.Len() != 0 {
			t.Fatalf("route was implicitly selected: %d %s %s", code, &out, &stderr)
		}
		if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "missing-route")); !os.IsNotExist(err) {
			t.Fatal("missing route wrote run files", err)
		}
	}
	if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("precheck or rejected start changed existing authority")
	}
	for _, route := range []string{"regular", "lightweight"} {
		out.Reset()
		stderr.Reset()
		args := []string{"workflow", "start", "--root", root, "--run-id", "selected-" + route, "--intake-receipt", candidatePath, "--route", route, "--vcs", "git", "--machine"}
		if code := runWithHostConfirmation(t, args, streams); code != 0 {
			t.Fatalf("explicit %s start: %d %s", route, code, &stderr)
		}
		var run facade.Run
		if err := json.Unmarshal(out.Bytes(), &run); err != nil || run.Route != route || run.Unverified != (route == "lightweight") || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &candidate) {
			t.Fatalf("explicit mode or material changed: %+v %v", run, err)
		}
		body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "request.json"))
		var request facade.StartRequest
		if err != nil || json.Unmarshal(body, &request) != nil || request.Route != route || !reflect.DeepEqual(request.IntakeConfirmationReceipt, candidate) {
			t.Fatal("persisted route or confirmed binding changed", err)
		}
	}
}

func TestRequirementParsesFlagsBeforeSelectingExistingRuns(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.RunID = "another-target"
	a := &facade.Admission{RegistryPath: request.AdmissionRegistryPath, Target: request.AdmissionTarget, Root: request.AdmissionRoot, Provider: request.Provider, PackageDigest: request.PackageDigest, InstalledTargetIdentity: request.InstalledTargetIdentity, Generation: request.AdmissionGeneration, Lease: request.AdmissionLease, Token: request.AdmissionToken}
	if _, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, Request: request, Admission: a, VCSRunner: frozenGitRunner{}}); err != nil {
		t.Fatal(err)
	}
	before := map[string]map[string]string{}
	for _, id := range []string{"writer-exclusive", "another-target"} {
		before[id] = snapshotEngineRunTree(t, root, id)
	}
	var out, stderr bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &stderr}
	for _, command := range []string{"show", "append", "edit", "remove"} {
		for _, help := range []string{"--help", "-h"} {
			out.Reset()
			stderr.Reset()
			if code := Run("formal-gates", []string{"requirement", command, "--root", root, "--run", "not-a-number", help}, streams); code != 0 || !strings.Contains(out.String(), "Usage of requirement "+command) || strings.Contains(out.String(), `"runs"`) || stderr.Len() != 0 {
				t.Fatalf("help attempted run selection: %d %s %s", code, &out, &stderr)
			}
		}
	}
	for _, flags := range [][]string{{"--unknown"}, {"--run", "not-a-number"}, {"--run", "1", "--run-id", "writer-exclusive"}, {"--run", "3"}, {"--file", "new.md"}} {
		out.Reset()
		stderr.Reset()
		args := append([]string{"requirement", "show", "--root", root}, flags...)
		if code := Run("formal-gates", args, streams); code == 0 || out.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("invalid flags reached run choices: %d %s %s", code, &out, &stderr)
		}
	}
	for _, flags := range [][]string{{"--run", "1"}, {"--run-id", "writer-exclusive"}, {"--run", "2", "--document", "1"}} {
		out.Reset()
		stderr.Reset()
		args := append([]string{"requirement", "show", "--root", t.TempDir(), "--root", root}, flags...)
		if code := Run("formal-gates", args, streams); code != 0 {
			t.Fatalf("parsed selection failed: %d %s", code, &stderr)
		}
		var preview facade.IntakePreview
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.IntakeCandidate.RequirementSource != request.IntakeConfirmationReceipt.RequirementSource {
			t.Fatalf("selected run lost its bound source: %+v %v", preview, err)
		}
	}
	out.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"requirement", "show", "--root", root}, streams); code != 0 || !strings.Contains(out.String(), `"runs"`) {
		t.Fatalf("ambiguous source was guessed: %d %s %s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"requirement", "append", "--root", root, "--file", "new.md", "--title", "Output", "--requirement", "Return output", "--acceptance", "Output is returned", "--source", "User request", "--solution", "Use the existing owner"}, streams); code != 0 {
		t.Fatalf("new file required a run selection: %d %s", code, &stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "new.md")); err != nil {
		t.Fatal("append did not create its selected new document", err)
	}
	for id, original := range before {
		if !reflect.DeepEqual(original, snapshotEngineRunTree(t, root, id)) {
			t.Fatal("requirement help, selection or append changed run authority", id)
		}
	}
}

func TestRequirementCommandsCreateAndEditUsingPublishedPositions(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader("")}
	code, err := runRequirement([]string{"append", "--root", root, "--file", "正式需求.md", "--title", "保存", "--requirement", "保存原内容。", "--acceptance", "读取一致。", "--source", "用户请求。", "--solution", "复用现有程序。"}, streams)
	if code != 0 || err != nil {
		t.Fatalf("create: %d %v", code, err)
	}
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Revision == "" || preview.RequirementDocument == nil || preview.Content != preview.Materials[0].Content || preview.IntakeCandidate.RequirementSource != "正式需求.md" {
		t.Fatalf("incomplete confirmation material: %+v", preview)
	}
	before := preview.Content
	out.Reset()
	if code, err = runRequirement([]string{"show", "--root", root}, streams); code != 0 || err != nil {
		t.Fatalf("listing: %d %v", code, err)
	}
	var choices []requirementDocumentChoice
	if err := json.Unmarshal(out.Bytes(), &choices); err != nil || len(choices) != 1 || choices[0].Path != "正式需求.md" {
		t.Fatalf("published documents: %s %v", out.String(), err)
	}
	out.Reset()
	if code, err = runRequirement([]string{"edit", "--root", root, "--document", "1", "--item", "1", "--condition", "1", "--acceptance", "可重复读取。"}, streams); code != 0 || err != nil {
		t.Fatalf("position edit: %d %v", code, err)
	}
	data, err := os.ReadFile(filepath.Join(root, choices[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.Replace(before, "读取一致。", "可重复读取。", 1) {
		t.Fatalf("inherited material changed: %s", data)
	}
	out.Reset()
	if code, err = runRequirement([]string{"edit", "--root", root, "--document", strconv.Itoa(choices[0].Position), "--item", "1", "--title", "保留"}, streams); code != 0 || err != nil {
		t.Fatalf("published document edit: %d %v", code, err)
	}
	data, err = os.ReadFile(filepath.Join(root, choices[0].Path))
	if err != nil || string(data) != strings.Replace(strings.Replace(before, "读取一致。", "可重复读取。", 1), "保存", "保留", 1) {
		t.Fatalf("published title edit changed unrelated bytes: %q %v", data, err)
	}
}

func TestSettingsCommandsEditPublishedFieldFromPlainInput(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	var out bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader("5\n")}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		t.Fatal(err)
	}
	position := ""
	for _, field := range fields {
		if field.Name == "capacity" {
			position = strconv.Itoa(field.Position)
		}
	}
	if code, err := runSettings([]string{"set", "--root", root, "--home", home, "--field", position}, streams); code != 0 || err != nil {
		t.Fatalf("settings set: %d %v", code, err)
	}
	effective, err = settings.Load(root, home)
	if err != nil || effective.Values.Capacity != 5 {
		t.Fatalf("settings plain input: %+v %v", effective, err)
	}
	input := strings.NewReader("9\n")
	streams.Stdin = input
	if code, err := runSettings([]string{"set", "--root", root, "--home", home, "--field", position, "--value", "6"}, streams); code != 0 || err != nil || input.Len() != len("9\n") {
		t.Fatalf("provided value consumed stdin: %d %v", code, err)
	}
	effective, err = settings.Load(root, home)
	if err != nil || effective.Values.Capacity != 6 {
		t.Fatalf("settings provided value: %+v %v", effective, err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := runSettings([]string{"set", "--root", root, "--home", home, "--field", position, "--value", ""}, streams); code == 0 || err == nil || input.Len() != len("9\n") {
		t.Fatalf("empty provided value fell back to stdin: %d %v", code, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected empty value changed configuration: %q %v", after, err)
	}
}

func TestSettingsHelpDoesNotReadOrWriteConfiguration(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", "")
	paths := []string{filepath.Join(root, ".gates", "config.json"), filepath.Join(home, ".config", "formal-gates", "config.json")}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unparsed configuration"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []string{"show", "set", "remove"} {
		for _, help := range []string{"--help", "-h"} {
			var out, stderr bytes.Buffer
			input := strings.NewReader("unused value")
			streams := IO{Stdout: &out, Stderr: &stderr, Stdin: input}
			for _, homeFlags := range [][]string{nil, {"--home", home}} {
				out.Reset()
				stderr.Reset()
				args := append([]string{"settings", command, "--root", root, "--field", "not-a-number", help}, homeFlags...)
				if code := Run("formal-gates", args, streams); code != 0 || !strings.Contains(out.String(), "Usage of settings "+command) || strings.Contains(out.String(), `"effective"`) || stderr.Len() != 0 || input.Len() != len("unused value") {
					t.Fatalf("settings help used configuration or input: %v %d %s %s", args, code, &out, &stderr)
				}
			}
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "unparsed configuration" {
			t.Fatalf("help changed configuration: %s %q %v", path, data, err)
		}
	}
}

func TestRequirementCommandsReviseExistingSectionsWithoutSelectingARequirement(t *testing.T) {
	root := t.TempDir()
	var out, errout bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &errout}
	create := []string{"requirement", "append", "--root", root, "--file", "requirement.md", "--title", "Public output", "--requirement", "Return requested output", "--acceptance", "Requested output is returned", "--source", "User request", "--context", "Confirmed context", "--solution", "Existing solution"}
	if code := Run("formal-gates", create, streams); code != 0 {
		t.Fatalf("create: %d %s", code, errout.String())
	}
	path := filepath.Join(root, "requirement.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var originalPreview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &originalPreview); err != nil {
		t.Fatal(err)
	}
	correctedSolution := strings.Replace(string(original), "Existing solution", "Corrected solution", 1)
	correctedContext := strings.Replace(correctedSolution, "Confirmed context", "Corrected context", 1)
	finalSolution := strings.Replace(correctedContext, "Corrected solution", "Final solution", 1)
	finalSections := strings.Replace(strings.Replace(finalSolution, "Corrected context", "Final context", 1), "Final solution", "Completed solution", 1)
	for _, test := range []struct {
		flags []string
		want  string
	}{
		{[]string{"--item", "1", "--solution", "Corrected solution"}, correctedSolution},
		{[]string{"--context", "Corrected context"}, correctedContext},
		{[]string{"--solution", "Final solution"}, finalSolution},
		{[]string{"--context", "Final context", "--solution", "Completed solution"}, finalSections},
	} {
		out.Reset()
		errout.Reset()
		args := append([]string{"requirement", "edit", "--root", root, "--document", "1"}, test.flags...)
		if code := Run("formal-gates", args, streams); code != 0 {
			t.Fatalf("section edit: %d %s", code, errout.String())
		}
		want := test.want
		current, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(current) != want {
			t.Fatalf("section edit changed unrelated bytes\nwant=%q\ngot=%q", want, current)
		}
		var preview facade.IntakePreview
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		if preview.Content != want || preview.Materials[0].Content != want || preview.Revision == originalPreview.Revision || preview.IntakeCandidate.RequirementSource != "requirement.md" || preview.RequirementDocument.Requirements[0].ID != originalPreview.RequirementDocument.Requirements[0].ID {
			t.Fatalf("confirmation no longer describes the same edited document: %+v", preview)
		}
	}
}

func TestRequirementCommandsAddContextToAnExistingEmptySection(t *testing.T) {
	root := t.TempDir()
	var out, errout bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &errout}
	create := []string{"requirement", "append", "--root", root, "--file", "requirement.md", "--title", "Output", "--requirement", "Return output", "--acceptance", "Output is returned", "--source", "User", "--solution", "Existing solution"}
	if code := Run("formal-gates", create, streams); code != 0 {
		t.Fatalf("create: %d %s", code, errout.String())
	}
	out.Reset()
	errout.Reset()
	if code := Run("formal-gates", []string{"requirement", "edit", "--root", root, "--document", "1", "--context", "New context"}, streams); code != 0 {
		t.Fatalf("empty context edit: %d %s", code, errout.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "requirement.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "New context\n## 需求点") || !strings.Contains(string(data), "Existing solution") {
		t.Fatalf("context section boundary: %s", data)
	}
}

func TestGateDirectoryDocumentsRetainPublishedRequirementAndMaterialPositions(t *testing.T) {
	root := t.TempDir()
	var out, errors bytes.Buffer
	streams := IO{Stdout: &out, Stderr: &errors, Stdin: strings.NewReader("")}
	invoke := func(args ...string) {
		t.Helper()
		out.Reset()
		errors.Reset()
		if code := Run("formal-gates", args, streams); code != 0 {
			t.Fatalf("%v: %d %s", args, code, &errors)
		}
	}
	invoke("requirement", "append", "--root", root, "--file", ".gates/requirements.md", "--title", "Output", "--requirement", "Return original input", "--acceptance", "Original bytes are retained", "--source", "User request", "--solution", "Use the existing owner")
	var created facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	originals := map[string]string{".gates/requirements.md": created.Content, ".gates/方案.txt": "原方案\r\n", ".gates/材料.bin": "原工件\x00\r\n", ".gates/engine/other/retained.md": created.Content, ".gates/results/other.md": created.Content}
	for path, content := range originals {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	invoke("requirement", "show", "--root", root)
	var documents []requirementDocumentChoice
	if err := json.Unmarshal(out.Bytes(), &documents); err != nil || len(documents) != 1 || documents[0].Path != ".gates/requirements.md" {
		t.Fatalf("original documents: %s %v", &out, err)
	}
	materials, err := intakeMaterials(root)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for _, material := range materials {
		positions[material.Path] = material.Position
	}
	if len(positions) != 3 {
		t.Fatalf("runtime materials published: %+v", materials)
	}
	for i := 0; i < 2; i++ {
		invoke("workflow", "start", "--precheck", "--root", root, "--document", "1", "--solution-document", strconv.Itoa(positions[".gates/方案.txt"]), "--artifact-document", strconv.Itoa(positions[".gates/材料.bin"]))
		var preview facade.IntakePreview
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		if preview.Content != created.Content || preview.IntakeCandidate.RequirementSource != ".gates/requirements.md" || len(preview.Materials) != 3 || len(preview.IntakeCandidate.Artifacts) != 3 || preview.IntakeCandidate.SolutionRevision != fmt.Sprintf("%x", sha256.Sum256([]byte("原方案\n"))) {
			t.Fatalf("selected sources changed: %+v", preview)
		}
		for _, material := range preview.Materials {
			if positions[material.Path] == 0 || strings.ReplaceAll(originals[material.Path], "\r\n", "\n") != material.Content || material.Revision != fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(originals[material.Path], "\r\n", "\n")))) {
				t.Fatalf("bound material bytes changed: %+v", material)
			}
		}
	}
	invoke("requirement", "edit", "--root", root, "--document", "1", "--item", "1", "--requirement", "Return valid original input")
	expected := strings.Replace(created.Content, "Return original input", "Return valid original input", 1)
	data, err := os.ReadFile(filepath.Join(root, ".gates/requirements.md"))
	if err != nil || string(data) != expected {
		t.Fatalf("position edit changed unrelated bytes: %q %v", data, err)
	}
	for path, content := range originals {
		if path == ".gates/requirements.md" {
			continue
		}
		actual, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(actual) != content {
			t.Fatalf("unselected material changed: %q %v", actual, err)
		}
	}
}

func TestProjectDocumentsExcludeRepositoryMetadataAndPreserveUserFiles(t *testing.T) {
	const document = "# Requirement and solution\n\n## 需求点\n\n### REQ-001：Output\n\n#### 要求\n\nReturn original input.\n\n#### 验收条件\n\n- AC-001：Original bytes are retained.\n\n#### 来源\n\nUser request.\n\n## 方案\n\nUse the existing owner.\n"
	for _, gitDirectory := range []bool{true, false} {
		name := "linked worktree"
		if gitDirectory {
			name = "git directory"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			originals := map[string]string{
				".codex/user-notes.md":   "用户笔记\r\n",
				".gates/requirements.md": document,
				"gates/user-material.md": "用户材料\x00\r\n",
				"ordinary.txt":           "ordinary\r\n",
				"requirements.md":        document,
				".svn/entries":           "12\n",
				".svn/wc.db":             "SQLite format 3\x00",
				".svn/requirements.md":   document,
			}
			if gitDirectory {
				originals[".git/HEAD"] = "ref: refs/heads/main\n"
				originals[".git/requirements.md"] = document
			} else {
				originals[".git"] = "gitdir: /project/.git/worktrees/work\n"
			}
			for path, content := range originals {
				target := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, errout bytes.Buffer
			streams := IO{Stdout: &out, Stderr: &errout}
			for i := 0; i < 2; i++ {
				for _, listing := range []struct {
					args []string
					want []requirementDocumentChoice
				}{
					{
						[]string{"workflow", "start", "--root", root, "--precheck", "--materials"},
						[]requirementDocumentChoice{{1, ".codex/user-notes.md"}, {2, ".gates/requirements.md"}, {3, "gates/user-material.md"}, {4, "ordinary.txt"}, {5, "requirements.md"}},
					},
					{
						[]string{"requirement", "show", "--root", root},
						[]requirementDocumentChoice{{1, ".gates/requirements.md"}, {2, "requirements.md"}},
					},
				} {
					out.Reset()
					errout.Reset()
					if code := Run("formal-gates", listing.args, streams); code != 0 {
						t.Fatalf("%v: %d %s", listing.args, code, &errout)
					}
					var choices []requirementDocumentChoice
					if err := json.Unmarshal(out.Bytes(), &choices); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(choices, listing.want) {
						t.Fatalf("%v: want %+v, got %+v", listing.args, listing.want, choices)
					}
				}
			}
			for path, content := range originals {
				actual, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(actual) != content {
					t.Fatalf("source %s changed: %q %v", path, actual, err)
				}
			}
		})
	}
}
