package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTopLevelUsageListsSettingsRename(t *testing.T) {
	var out bytes.Buffer
	printUsage(&out, "formal-gates")
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "settings ") {
			if !strings.Contains(line, "|rename|") {
				t.Fatal("top-level settings usage omitted rename", line)
			}
			return
		}
	}
	t.Fatal("top-level usage omitted settings")
}
