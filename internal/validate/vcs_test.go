package validate

import (
	engineVCS "formal-gates/internal/engine/vcs"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReadyExemptsEngineRuntimeFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "mixed-generation@example.invalid")
	runGit(t, root, "config", "user.name", "Mixed Generation Test")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "base\n")
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "base delivery")
	writeTestFile(t, filepath.Join(root, ".gates", "engine", "mixed-run", "state.json"), "{}\n")
	if err := engineVCS.New(root, nil).VerifyReady(); err != nil {
		t.Fatalf("engine runtime directory blocked the readiness check: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "delivery-new.txt"), "uncommitted delivery\n")
	if err := engineVCS.New(root, nil).VerifyReady(); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("untracked delivery file was accepted beside engine runtime files: %v", err)
	}
}
