package cost

import (
	"testing"

	"formal-gates/internal/host"
)

func TestTranscriptParserRegistryMatchesDeclaredCostProviders(t *testing.T) {
	for _, descriptor := range host.All() {
		if !descriptor.Installable || descriptor.CostProvider == "" {
			continue
		}
		if _, ok := transcriptParsers[descriptor.CostProvider]; !ok {
			t.Fatalf("installable host %q declares unsupported cost provider %q", descriptor.ID, descriptor.CostProvider)
		}
	}
}
