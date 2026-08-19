package lifecycle

// Independent whitebox QA for host identity recovery after the stage-0
// launcher was unified to one fixed path. Provider selection must come from
// the admitted registry record, with the most specific project root winning.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWhiteboxPhase0Round7ProviderSelectionUsesSpecificRegistryBinding(t *testing.T) {
	home := t.TempDir()
	globalRoot := filepath.Join(home, "workspace")
	projectRoot := filepath.Join(globalRoot, "project")
	outsideRoot := filepath.Join(globalRoot, "other")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", "formal-gates")
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	canonical := func(path string) string {
		value, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Clean(value)
	}
	type record struct {
		LauncherPath string            `json:"launcherPath"`
		Host         string            `json:"host"`
		ProjectRoot  string            `json:"projectRoot"`
		Canonical    map[string]string `json:"canonicalPaths"`
		Status       string            `json:"status"`
	}
	document := struct {
		Records []record `json:"records"`
	}{Records: []record{
		{LauncherPath: launcher, Host: "codex", ProjectRoot: globalRoot, Status: "active", Canonical: map[string]string{"launcher": launcher, "projectRoot": globalRoot}},
		{LauncherPath: launcher, Host: "claude", ProjectRoot: projectRoot, Status: "active", Canonical: map[string]string{"launcher": launcher, "projectRoot": projectRoot}},
		// A disabled, more-specific record must never override an active one.
		{LauncherPath: launcher, Host: "cursor", ProjectRoot: filepath.Join(projectRoot, "nested"), Status: "disabled", Canonical: map[string]string{"launcher": launcher, "projectRoot": filepath.Join(projectRoot, "nested")}},
	}}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(registry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	previousExecutable := executablePath
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		executablePath = previousExecutable
		_ = os.Chdir(previousDirectory)
	})
	executablePath = func() (string, error) { return launcher, nil }
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Registry binding wins over a conflicting host environment marker.
	t.Setenv("AI_AGENT", "codex-cli")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	provider, err := currentProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderClaude {
		t.Fatalf("most-specific active registry binding did not win: got %q", provider)
	}
	if canonical(launcher) != canonicalProviderPath(launcher) {
		t.Fatal("fixture launcher did not have a stable canonical identity")
	}

	if err := os.Chdir(outsideRoot); err != nil {
		t.Fatal(err)
	}
	provider, err = currentProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderCodex {
		t.Fatalf("global registry binding was not selected outside the nested project: got %q", provider)
	}
}
