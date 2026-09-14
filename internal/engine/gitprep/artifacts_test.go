package gitprep

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRetainedTestArtifactsFollowNewDevelopmentSnapshot(t *testing.T) {
	root := repository(t)
	head, _ := Head(root)
	write(t, root, "obsolete_test.go", "package main\n// previous test\n")
	base, err := Commit(root, head, "test-artifact-base", []string{"obsolete_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	qa := filepath.Join(t.TempDir(), "qa")
	if err := Workspace(root, qa, base.After); err != nil {
		t.Fatal(err)
	}
	write(t, qa, "current_test.go", "package main\n// retained independent test\n")
	if err := os.Remove(filepath.Join(qa, "obsolete_test.go")); err != nil {
		t.Fatal(err)
	}
	manifest := []string{"current_test.go", "obsolete_test.go"}
	tests, err := Commit(qa, base.After, "test-artifacts", manifest)
	if err != nil {
		t.Fatal(err)
	}
	production := "package main\nfunc main() { println(\"new implementation\") }\n"
	write(t, root, "main.go", production)
	increment, err := Commit(root, base.After, "development-increment", []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "new-whitebox")
	if err := Workspace(root, target, increment.After); err != nil {
		t.Fatal(err)
	}
	for replay := 0; replay < 2; replay++ {
		if err := RestoreArtifacts(target, base.After, tests.After, manifest); err != nil {
			t.Fatalf("replay %d: %v", replay, err)
		}
		got, err := os.ReadFile(filepath.Join(target, "main.go"))
		if err != nil || string(got) != production {
			t.Fatalf("retained QA restored the old production tree: %q %v", got, err)
		}
		gotHead, _ := Head(target)
		if gotHead != increment.After {
			t.Fatal("artifact carry changed the new development snapshot")
		}
		changes, err := Changes(target, increment.After)
		if err != nil || !reflect.DeepEqual(changes, manifest) {
			t.Fatalf("artifact manifest differs: %v %v", changes, err)
		}
	}
	write(t, target, "current_test.go", "package main\n// a newer developer test\n")
	before, err := WorkingView(target, increment.After)
	if err != nil {
		t.Fatal(err)
	}
	if err := RestoreArtifacts(target, base.After, tests.After, manifest); err == nil || !strings.Contains(err.Error(), "ARTIFACT_CONFLICT") {
		t.Fatalf("divergent test edits were not reported: %v", err)
	}
	after, err := WorkingView(target, increment.After)
	if err != nil || before.Digest != after.Digest {
		t.Fatal("conflict modified the workspace instead of requesting a QA revision")
	}
}
