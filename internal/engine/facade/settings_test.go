package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/definition"
)

func TestEffectiveSettingsSurviveReopenAndTerminalWithoutReload(t *testing.T) {
	root := t.TempDir()
	// The admission fixture builds the CLI binary once per process; keep that
	// build on the real module cache before HOME is isolated for the overlay.
	admission := testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(root, ".gates", "config.json")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"capacity":2,"qaCapacity":1,"model":"chosen-model","reasoningEffort":"high","tokenCap":77}`), 0600); err != nil {
		t.Fatal(err)
	}
	request := StartRequest{RunID: "configured-run", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}
	f, run, err := Start(StartOptions{Root: root, Request: request, Admission: admission})
	if err != nil {
		t.Fatal(err)
	}
	if run.Settings == nil || run.Settings.Values.Capacity != 2 || run.Settings.Values.QACapacity != 1 || *run.Settings.Values.TokenCap != 77 || run.Settings.Sources["model"] != config {
		t.Fatalf("effective=%+v", run.Settings)
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Settings.Values.TokenCap != nil || snap.State.TokenCapMaxTokens == nil || *snap.State.TokenCapMaxTokens != 77 {
		t.Fatal("cap must retain its sole cost owner")
	}
	before, _ := json.Marshal(snap.State.Settings)
	if err := os.WriteFile(config, []byte(`invalid edited config`), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.capacity != 2 {
		t.Fatalf("reopen capacity=%d", reopened.capacity)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(after.State.Settings)
	if string(before) != string(got) || *after.State.TokenCapMaxTokens != 77 {
		t.Fatal("reopen reset settings or cap")
	}
	terminal, err := reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Settings == nil || terminal.Settings.Values.Model != "chosen-model" || *terminal.Settings.Values.TokenCap != 77 {
		t.Fatalf("terminal settings=%+v", terminal.Settings)
	}
	shown, err := reopened.Show()
	if err != nil || shown.Settings == nil || shown.Settings.Sources["model"] != config {
		t.Fatalf("terminal show=%+v err=%v", shown.Settings, err)
	}
}
