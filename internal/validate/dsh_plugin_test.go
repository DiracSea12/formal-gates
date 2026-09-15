package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDshPluginSourceOwnsRequiredInterceptionSurface(t *testing.T) {
	data, err := os.ReadFile("dsh_plugin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"export const name = 'formal-gates-dsh'",
		"ctx.on('tools/pre-execute'",
		"ctx.on('subagent/start'",
		"ctx.on('subagent/end'",
		"['hook', 'decide']",
		"['lifecycle', 'capture', '--provider', PROVIDER, '--event', event]",
		"normalizeArguments",
		"config.skillRoot",
		"process.platform",
		"permissionDecision",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("dsh_plugin.mjs missing required behavior %q", required)
		}
	}
}

// DSH anchors inserted plugin names beside the patch, not beside the profile.
func TestDshPluginResolvesBesideHomePatch(t *testing.T) {
	home := filepath.Join(t.TempDir(), "custom dsh home")
	target := installTarget{
		targetPath: filepath.Join(home, "skills", "formal-gates"),
		hookConfig: filepath.Join(home, dshPatchFileName),
	}
	if err := configureDshHook(target); err != nil {
		t.Fatal(err)
	}
	doc, err := readDshPatch(target.hookConfig)
	if err != nil {
		t.Fatal(err)
	}
	row := dshMappingNode(doc.Content[0].Content[0], "insert").Content[0]
	_, name := dshPluginRowIdentity(row)
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target.hookConfig), filepath.FromSlash(name)))
	if resolved != dshPluginPath(target) {
		t.Fatalf("DSH resolves plugin to %q, installed at %q", resolved, dshPluginPath(target))
	}
	if _, err := os.Stat(resolved); err != nil {
		t.Fatal(err)
	}
}
