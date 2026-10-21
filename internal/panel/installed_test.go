package panel

import (
	"os"
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

// nativeFixtureValues binds one platform's own native configuration fields so
// its resolver sees a complete path-only configuration.
func nativeFixtureValues(t *testing.T, provider string, fixture host.NativeFixture) map[string]string {
	t.Helper()
	adapter, err := host.LookupAdapter(provider)
	if err != nil {
		t.Fatal(err)
	}
	binder, ok := adapter.(host.NativeFixtureProvider)
	if !ok {
		t.Fatalf("native path fixture unavailable for %q", provider)
	}
	values, err := binder.FixtureNativePaths(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestInstalledExecutablesFollowPATHVerdicts(t *testing.T) {
	platforms := executionPlatformNames()
	if len(platforms) == 0 {
		t.Fatal("no execution platforms registered")
	}
	// A configured absolute executable path is its own installed proof; a
	// configured name missing from PATH is not.
	native := nativeFixtureValues(t, "devin", host.NativeFixture{Executable: os.Args[0]})
	for field, value := range nativeFixtureValues(t, "codex", host.NativeFixture{Executable: "formal-gates-definitely-missing-executable-name"}) {
		native[field] = value
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
	// The platform materializes the script its dispatch resolution requires,
	// and an absolute stand-in executable keeps the test independent of the
	// machine's own node install.
	scriptDir := t.TempDir()
	native := nativeFixtureValues(t, "zcode", host.NativeFixture{Executable: os.Args[0], ScriptDir: scriptDir})
	if installed := installedExecutables(native, platforms); !installed["zcode"] {
		t.Fatalf("zcode dispatch resolution was ignored: %+v", installed)
	}
	// The same bound values without the platform's dispatch script: its own
	// resolver reports the platform unavailable.
	if err := os.RemoveAll(scriptDir); err != nil {
		t.Fatal(err)
	}
	if installed := installedExecutables(native, platforms); installed["zcode"] {
		t.Fatal("zcode reported installed while its dispatch script is absent")
	}
}
