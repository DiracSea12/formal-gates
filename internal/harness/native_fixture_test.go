package harness

import (
	"os"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

// Path-only fixtures are the contract the portable canary relies on: each
// installable platform binds its own configuration fields, and that binding
// resolves without an installed host.
func TestNativeFixturePaths(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("fixture executable is not a regular file: %v", err)
	}
	for _, installName := range host.InstallableNames() {
		adapter, err := host.LookupAdapter(installName)
		if err != nil {
			t.Fatal(err)
		}
		fixture, ok := adapter.(host.NativeFixtureProvider)
		if !ok {
			t.Fatalf("installable platform %q has no native fixture binding", installName)
		}
		descriptor := adapter.HostDescriptor()
		t.Run(installName, func(t *testing.T) {
			fields := map[string]bool{}
			for _, field := range descriptor.Configuration {
				fields[field.Name] = true
			}
			values, err := fixture.FixtureNativePaths(host.NativeFixture{Executable: executable, ScriptDir: t.TempDir(), Home: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			for field := range values {
				if !fields[field] {
					t.Fatalf("fixture bound undeclared configuration field %q: %v", field, values)
				}
			}
			native, sources := map[string]string{}, map[string]string{}
			for field, value := range values {
				native[field], sources[field] = value, "fixture"
			}
			if err := adapter.(host.NativePathResolver).ResolveNativePaths(native, sources); err != nil {
				t.Fatalf("fixture %v did not resolve: %v", values, err)
			}
			pathOnly, err := fixture.FixtureNativePaths(host.NativeFixture{Executable: executable})
			if err != nil {
				t.Fatal(err)
			}
			if len(pathOnly) != 1 {
				t.Fatalf("path-only fixture bound %v", pathOnly)
			}
			for field, value := range pathOnly {
				if !fields[field] || !strings.HasSuffix(field, "Executable") || value != executable {
					t.Fatalf("path-only fixture bound %q=%q", field, value)
				}
			}
		})
	}
}
