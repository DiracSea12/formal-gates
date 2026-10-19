package validate

import (
	"testing"

	"formal-gates/internal/host"
)

func TestHookCapabilityRegistryCoversEveryInstallableHost(t *testing.T) {
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		adapter, ok := platformAdapters[descriptor.ID]
		if !ok || adapter.hook.configure == nil || adapter.hook.remove == nil {
			t.Fatalf("installable host %q has incomplete hook capability", descriptor.ID)
		}
		if _, err := platformAdapterFor(descriptor.ID); err != nil {
			t.Fatalf("installable host %q has no platform adapter: %v", descriptor.ID, err)
		}
	}
}
