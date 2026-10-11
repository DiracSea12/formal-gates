package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
