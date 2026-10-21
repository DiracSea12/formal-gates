package panel

import (
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/host"
)

func executionPlatformNames() []string {
	platforms := []string{}
	for _, adapter := range host.RegisteredAdapters() {
		if _, ok := adapter.(host.SelectionAdapter); ok {
			if name := adapter.HostDescriptor().InstallName; name != "" {
				platforms = append(platforms, name)
			}
		}
	}
	return platforms
}

func TestInstalledExecutablesFollowPATHVerdicts(t *testing.T) {
	platforms := executionPlatformNames()
	if len(platforms) == 0 {
		t.Fatal("no execution platforms registered")
	}
	native := map[string]string{
		// A configured absolute executable path is its own installed proof;
		// a configured name missing from PATH is not.
		"devinExecutable": os.Args[0],
		"codexExecutable": "formal-gates-definitely-missing-executable-name",
	}
	installed := installedExecutables(native, platforms)
	if !installed["devin"] {
		t.Fatalf("configured absolute executable not honored: %+v", installed)
	}
	if installed["codex"] {
		t.Fatalf("a PATH-missing executable reported installed: %+v", installed)
	}
	for _, name := range platforms {
		if _, present := installed[name]; !present {
			t.Fatalf("platform %q has no verdict: %+v", name, installed)
		}
	}
}

// The unified rule runs each platform's own resolver; ZCode's reaches beyond
// the node binary to the app-server script, so its verdict still follows its
// dispatch resolution rather than a bare PATH lookup.
func TestZCodeInstallationFollowsItsDispatchResolution(t *testing.T) {
	platforms := executionPlatformNames()
	native := map[string]string{
		// Node resolves through PATH semantics too; an absolute stand-in keeps
		// the test independent of the machine's own node install.
		"zcodeExecutable": os.Args[0],
		"zcodeScript":     filepath.Join(t.TempDir(), "app-server.cjs"),
	}
	if err := os.WriteFile(native["zcodeScript"], []byte("// stand-in"), 0o600); err != nil {
		t.Fatal(err)
	}
	if installed := installedExecutables(native, platforms); !installed["zcode"] {
		t.Fatalf("zcode dispatch resolution was ignored: %+v", installed)
	}
	native["zcodeScript"] = filepath.Join(t.TempDir(), "missing-app-server.cjs")
	if installed := installedExecutables(native, platforms); installed["zcode"] {
		t.Fatal("zcode reported installed while its dispatch script is absent")
	}
}
