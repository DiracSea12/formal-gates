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
		capability, ok := hookCapabilities[descriptor.Hook.Kind]
		if !ok || capability.configure == nil || capability.remove == nil {
			t.Fatalf("installable host %q has incomplete hook capability for kind %q", descriptor.ID, descriptor.Hook.Kind)
		}
	}
}
