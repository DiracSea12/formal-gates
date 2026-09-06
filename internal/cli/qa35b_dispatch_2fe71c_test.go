package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/guard"
)

const qa35bPreviewInput = `{"schemaVersion":1,"requirement":{"id":"r","revision":"1","confirmedRevision":"1"},"batch":{"id":"b","revision":"1","confirmedRevision":"1"},"route":{"id":"route","revision":"1","confirmedRevision":"1"},"topology":{"id":"topology","revision":"1","confirmedRevision":"1"},"definition":{"id":"d","revision":"1","externalSteps":["review/agent"]},"externalTasks":[{"taskKey":"review/agent/a","provenance":{"kind":"DEFINITION","id":"d","revision":"1"}},{"taskKey":"review/agent/b","provenance":{"kind":"DEFINITION","id":"d","revision":"1"},"maxAttempts":4}]}`

func TestQA35BPreviewDelegatesAndReadsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "basis.json")
	if err := os.WriteFile(path, []byte(qa35bPreviewInput), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "guard-preview", "--input", path}, IO{Stdout: &out, Stderr: &stderr})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
	var got struct {
		guard.Result
		RuntimeIntegration string `json:"runtimeIntegration"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var b guard.Basis
	if err := json.Unmarshal([]byte(qa35bPreviewInput), &b); err != nil {
		t.Fatal(err)
	}
	production, err := guard.Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Result, production) || got.RequestAllowance != 2 || got.RetryAllowance != 4 || got.TotalAllowance != 6 || got.RuntimeIntegration != "NOT_INSTALLED" {
		t.Fatalf("output %+v", got)
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) || string(data) != qa35bPreviewInput {
		t.Fatal("preview changed input directory")
	}
}

func TestQA35BPreviewCommonInputFailures(t *testing.T) {
	for _, tt := range []struct{ name, input, fragment string }{
		{"syntax", "{", "EOF"}, {"schema", strings.Replace(qa35bPreviewInput, `"schemaVersion":1`, `"schemaVersion":2`, 1), "schemaVersion"},
		{"unknown-field", `{"surprise":1}`, "unknown field"}, {"two-objects", qa35bPreviewInput + " {}", "exactly one JSON object"},
		{"missing-identity", strings.Replace(qa35bPreviewInput, `"id":"r"`, `"id":""`, 1), "requirement.id"},
		{"bad-retry", strings.Replace(qa35bPreviewInput, `"maxAttempts":4`, `"maxAttempts":0`, 1), "maxAttempts"},
		{"conflicting-key", strings.Replace(qa35bPreviewInput, "review/agent/b", "review/agent/a", 1), "conflicting declarations"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "basis.json")
			if err := os.WriteFile(path, []byte(tt.input), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := Run("formal-gates", []string{"workflow", "guard-preview", "--input", path}, IO{Stdout: &out, Stderr: &stderr})
			if code == 0 || !strings.Contains(stderr.String(), tt.fragment) || out.Len() != 0 {
				t.Fatalf("code %d stderr %s out %s", code, stderr.String(), out.String())
			}
			data, e := os.ReadFile(path)
			if e != nil || string(data) != tt.input {
				t.Fatal("input changed")
			}
			files, e := os.ReadDir(dir)
			if e != nil || len(files) != 1 {
				t.Fatal("preview created state")
			}
		})
	}
}

func TestQA35BPreviewMissingArgumentsAndFile(t *testing.T) {
	for _, args := range [][]string{nil, {"--input", filepath.Join(t.TempDir(), "missing.json")}, {"--input", "a", "extra"}} {
		var out, stderr bytes.Buffer
		code, err := runWorkflowGuardPreview(args, IO{Stdout: &out, Stderr: &stderr})
		if code == 0 || err == nil || out.Len() != 0 {
			t.Fatalf("args=%v code=%d err=%v", args, code, err)
		}
	}
}
