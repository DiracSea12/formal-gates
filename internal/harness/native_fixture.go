package harness

import (
	"os"
	"path/filepath"

	"formal-gates/internal/host"
)

// Path-only native fixtures: each platform binds the fixture values into its
// own configuration fields so path resolution succeeds without an installed
// host. A platform whose launch carries a script operand materializes its own
// fixture script; a platform whose configured binding requires a product
// configuration file materializes that file.

// fixtureBindings binds the fixture executable and, when the caller supplies
// one, its isolated home into one platform's own configuration fields.
func fixtureBindings(fixture host.NativeFixture, executableField, homeField string) map[string]string {
	values := map[string]string{executableField: fixture.Executable}
	if fixture.Home != "" && homeField != "" {
		values[homeField] = fixture.Home
	}
	return values
}

func dshFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	return fixtureBindings(fixture, "dshExecutable", ""), nil
}

func zcodeFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	values := fixtureBindings(fixture, "zcodeExecutable", "zcodeHome")
	if fixture.ScriptDir == "" {
		return values, nil
	}
	script := filepath.Join(fixture.ScriptDir, "native-fixture.cjs")
	if err := os.MkdirAll(fixture.ScriptDir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(script, []byte(`throw new Error("path-only fixture cannot start a model host");`), 0600); err != nil {
		return nil, err
	}
	values["zcodeScript"] = script
	return values, nil
}

func claudeFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	return fixtureBindings(fixture, "claudeExecutable", "claudeHome"), nil
}

func codexFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	return fixtureBindings(fixture, "codexExecutable", "codexHome"), nil
}

func workbuddyFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	values := fixtureBindings(fixture, "workbuddyExecutable", "workbuddyHome")
	if fixture.Home == "" {
		return values, nil
	}
	product := filepath.Join(fixture.Home, ".workbuddy", "cache", "acc-product-config-v3.json")
	if err := os.MkdirAll(filepath.Dir(product), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(product, []byte("{}"), 0600); err != nil {
		return nil, err
	}
	values["workbuddyProductConfig"] = product
	return values, nil
}

func cursorFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	return fixtureBindings(fixture, "cursorExecutable", "cursorHome"), nil
}

func devinFixturePaths(fixture host.NativeFixture) (map[string]string, error) {
	return fixtureBindings(fixture, "devinExecutable", ""), nil
}
