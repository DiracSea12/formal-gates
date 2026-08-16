package validate

import (
	"os"
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
		"permissionDecision",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("dsh_plugin.mjs missing required behavior %q", required)
		}
	}
}
