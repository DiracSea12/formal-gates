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
