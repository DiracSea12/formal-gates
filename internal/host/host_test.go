package host

import "testing"

func TestRegistryExposesFiniteInstallableHostCapabilities(t *testing.T) {
	for _, want := range []struct {
		value       string
		id          string
		installName string
	}{
		{"claude", Claude, "claude"},
		{"codex", Codex, "codex"},
		{"cursor", Cursor, "cursor"},
		{"dsh", DeepSeek, "dsh"},
		{"z-code", ZCode, "zcode"},
	} {
		descriptor, err := Lookup(want.value)
		if err != nil {
			t.Fatal(err)
		}
		if descriptor.ID != want.id || descriptor.InstallName != want.installName {
			t.Fatalf("Lookup(%q)=%+v", want.value, descriptor)
		}
		if !descriptor.Installable {
			t.Fatalf("%q is not installable", want.id)
		}
	}
}

func TestZCodeDescriptorCapturesGlobalOnlyHookAndToolLifecycle(t *testing.T) {
	descriptor, err := Lookup(ZCode)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Paths.GlobalHookConfig != ".zcode/cli/config.json" || descriptor.Paths.ProjectHookConfig != "" {
		t.Fatalf("unexpected ZCode hook paths: %+v", descriptor.Paths)
	}
	if descriptor.Hook.Kind != HookZCode || descriptor.Hook.Protocol != ProtocolZCode || descriptor.Hook.GateEvent != "PreToolUse" || descriptor.Hook.GateMatcher != ".*" || descriptor.Hook.LifecycleMatcher != "Agent|Task" {
		t.Fatalf("unexpected ZCode hook descriptor: %+v", descriptor.Hook)
	}
	if len(descriptor.LifecycleEvents) != 3 || descriptor.LifecycleEvents[0] != "PreToolUse" || descriptor.LifecycleEvents[2] != "PostToolUseFailure" {
		t.Fatalf("unexpected ZCode lifecycle events: %v", descriptor.LifecycleEvents)
	}
}

func TestInstallableListsComeFromRegistry(t *testing.T) {
	if got, want := InstallableNames(), []string{"claude", "codex", "cursor", "dsh", "zcode"}; !equalStrings(got, want) {
		t.Fatalf("InstallableNames()=%v, want %v", got, want)
	}
	if got, want := InstallableIDs(), []string{Claude, Codex, Cursor, DeepSeek, ZCode}; !equalStrings(got, want) {
		t.Fatalf("InstallableIDs()=%v, want %v", got, want)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
