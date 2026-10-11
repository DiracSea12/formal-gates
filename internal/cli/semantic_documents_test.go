package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/settings"
)

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
	if code, err = runRequirementWithSource([]string{"edit", "--root", root, "--item", "1", "--title", "保留"}, streams, choices[0].Path); code != 0 || err != nil {
		t.Fatalf("bound source edit: %d %v", code, err)
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
