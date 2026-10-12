package lifecycle

import (
	"strings"

	"formal-gates/internal/host"
)

const (
	eventStart = "subagent_start"
	eventStop  = "subagent_stop"
)

// ProviderEnvironmentKeys returns host identity environment keys from the
// host registry for callers that isolate their environment, such as the
// portable canary.
func ProviderEnvironmentKeys() []string {
	keys := []string{"AI_AGENT"}
	seen := map[string]bool{"AI_AGENT": true}
	for _, descriptor := range host.All() {
		for _, key := range descriptor.EnvironmentKeys {
			if key = strings.TrimSpace(key); key != "" && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}
