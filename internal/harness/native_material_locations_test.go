package harness

import (
	"bytes"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalReadBytesSurviveNativeContinuation(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	original := []byte{0xd6, 0xd0, '\n', 'a', '\n'}
	if err := os.WriteFile(filepath.Join(workspace, "source.txt"), original, 0600); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	list := callWorkspace(t, tools, `{"action":"list"}`)
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":1}`, list["listing"]))
	number := read["read"].(int)
	before := tools.facts[number-1].Content
	recovered, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	after := recovered.facts[number-1].Content
	if !bytes.Equal(before, after) {
		t.Fatalf("unchanged original file: before=%x; after=%x", before, after)
	}
	callWorkspace(t, recovered, fmt.Sprintf(`{"action":"copy-range","sourceRead":%d,"sourceFrom":1,"sourceTo":1,"read":%d,"from":3,"to":2}`, number, number))
	expected := append(append([]byte{}, original...), original[:3]...)
	copied, err := os.ReadFile(filepath.Join(workspace, "source.txt"))
	if err != nil || !bytes.Equal(copied, expected) {
		t.Fatalf("copied original bytes %x want %x: %v", copied, expected, err)
	}
	listing := callWorkspace(t, recovered, `{"action":"list"}`)
	current := callWorkspace(t, recovered, fmt.Sprintf(`{"action":"read","listing":%d,"entry":1}`, listing["listing"]))
	callWorkspace(t, recovered, fmt.Sprintf(`{"action":"replace","read":%d,"from":2,"to":2,"content":"new\n"}`, current["read"]))
	expected = append(append(append([]byte{}, original[:3]...), []byte("new\n")...), original[:3]...)
	changed, err := os.ReadFile(filepath.Join(workspace, "source.txt"))
	if err != nil || !bytes.Equal(changed, expected) {
		t.Fatalf("replaced bytes %x want %x: %v", changed, expected, err)
	}
}
func TestNativeComparisonPublishesDeletedSourceLocation(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, b, err)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "Fixture")
	for name, body := range map[string]string{"removed.go": "package app\nfunc Required() {}\n", "keep.go": "package app\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(workspace, "removed.go")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "candidate")
	head := git("rev-parse", "HEAD")
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	tools.material.Input.Handler = "engine.gate.v3"
	tools.material.Input.VCS = "git"
	tools.material.Input.Validation.Gate = &protocol.GateInput{BaseSnapshot: base, Candidate: coverage.ValidationCandidate{Identity: head}}
	comparison := callWorkspace(t, tools, `{"action":"comparison"}`)
	t.Logf("normal frozen comparison: %+v", comparison)
	for _, row := range comparison["changes"].([]any) {
		if getOf(row, "path") != "removed.go" {
			continue
		}
		number := getOf(row, "beforeRead").(int)
		facts := []worker.LocationFact{}
		for _, fact := range tools.facts {
			facts = append(facts, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
		}
		paths, err := worker.LocationPaths([]worker.Location{{Read: number, Line: 2}}, facts, requirement.FrozenRequirementProjection{})
		if err != nil || len(paths) != 1 || paths[0] != "removed.go:2" {
			t.Fatalf("original deleted location: %v %v", paths, err)
		}
		if !bytes.Equal(facts[number-1].Content, []byte("package app\nfunc Required() {}\n")) {
			t.Fatal("deleted original content changed")
		}
		return
	}
	t.Fatal("comparison did not publish deleted original read", comparison)
}
