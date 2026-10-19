package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/settings"
)

// saveReviewOnlyConfiguration 保存一个仍启用 product-review 的非默认命名配置：
// 它通过启动校验，且带非空 FlowSelection 供确认面断言。
func saveReviewOnlyConfiguration(t *testing.T, root string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "仅评审", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "仅评审", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"start-readiness", "development", "qa", "gate", "repair"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "仅评审", nil); err != nil {
		t.Fatal(err)
	}
}

// cliStartRepo 让 root 成为有一个提交的 git 仓库，使 workflow start 能解析
// 原生 base 快照。
func cliStartRepo(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.test"}, {"config", "user.name", "Test User"}, {"add", "--all"}, {"commit", "-q", "-m", "base"}} {
		cliGit(t, root, args...)
	}
}

func TestWorkflowStartRejectsPerRunNodeParameters(t *testing.T) {
	for _, flags := range [][]string{{"--nodes"}, {"--disable-node", "1"}, {"--precheck", "--disable-node", "1"}} {
		root := t.TempDir()
		var out, stderr bytes.Buffer
		args := append([]string{"workflow", "start", "--root", root}, flags...)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code == 0 {
			t.Fatal("start accepted a per-run node override", flags)
		}
		if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
			t.Fatal("removed parameter wrote state", err)
		}
	}
}

func TestStartConfirmationRejectsDisabledNodesBeforeHostConfirmation(t *testing.T) {
	called := false
	_, err := callConfirmationTool("confirm_start", json.RawMessage(`{"intake":1,"disabledNodes":[1]}`), IO{}, func(any) error { called = true; return nil }, "")
	if err == nil || called {
		t.Fatal("removed disabledNodes reached host confirmation", err)
	}
}
