package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderFromExecutableDistinguishesGlobalZCodeInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	global := filepath.Join(home, ".zcode", "skills", "formal-gates", "bin", nativeName())
	if got := providerFromExecutable(global); got != ProviderZCode {
		t.Fatalf("global ZCode binary resolved provider %q, want %q", got, ProviderZCode)
	}
	project := filepath.Join(t.TempDir(), "repo", ".zcode", "skills", "formal-gates", "bin", nativeName())
	if got := providerFromExecutable(project); got != ProviderDefault {
		t.Fatalf("project-local ZCode binary resolved provider %q, want %q", got, ProviderDefault)
	}
}

func TestCurrentProviderKeepsProjectZCodeLenient(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AI_AGENT", "zcode")
	for _, key := range []string{"ZCODE_PLUGIN_ROOT", "ZCODE_PLUGIN_ID", "ZCODE_PLUGIN_NAME"} {
		t.Setenv(key, "")
	}
	project := filepath.Join(t.TempDir(), "repo", ".zcode", "skills", "formal-gates", "bin", nativeName())
	prior := executablePath
	executablePath = func() (string, error) { return project, nil }
	t.Cleanup(func() { executablePath = prior })
	provider, err := currentProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderDefault {
		t.Fatalf("project ZCode binary resolved provider %q, want %q", provider, ProviderDefault)
	}
}

func TestCurrentProviderKeepsProjectZCodeStableLauncherLenient(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", nativeName())
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	document := map[string]any{"records": []any{
		map[string]any{
			"launcherPath": launcher,
			"host":         ProviderCodex,
			"scope":        "global",
			"projectRoot":  home,
			"status":       "active",
			"canonicalPaths": map[string]string{
				"launcher": launcher, "projectRoot": home,
			},
		},
		map[string]any{
			"launcherPath": launcher,
			"host":         ProviderZCode,
			"scope":        "project",
			"projectRoot":  project,
			"status":       "active",
			"canonicalPaths": map[string]string{
				"launcher": launcher, "projectRoot": project,
			},
		},
	}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(registry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AI_AGENT", "zcode")
	for _, key := range []string{"ZCODE_PLUGIN_ROOT", "ZCODE_PLUGIN_ID", "ZCODE_PLUGIN_NAME"} {
		t.Setenv(key, "")
	}
	previousExecutable := executablePath
	previousDirectory, err := os.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		executablePath = previousExecutable
		_ = previousDirectory.Chdir()
		_ = previousDirectory.Close()
	})
	executablePath = func() (string, error) { return launcher, nil }
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	provider, err := currentProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderDefault {
		t.Fatalf("project ZCode stable launcher resolved provider %q, want %q", provider, ProviderDefault)
	}
}
