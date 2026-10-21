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
		{"cbc", WorkBuddy, "workbuddy"},
		{"devin", "devin", "devin"},
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

func TestZCodeDescriptorCapturesGlobalOnlyWriteBlockHook(t *testing.T) {
	descriptor, err := Lookup(ZCode)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Paths.GlobalHookConfig != ".zcode/cli/config.json" || descriptor.Paths.ProjectHookConfig != "" {
		t.Fatalf("unexpected ZCode hook paths: %+v", descriptor.Paths)
	}
	if descriptor.Hook.Kind != HookZCode || descriptor.Hook.Protocol != ProtocolZCode || descriptor.Hook.GateEvent != "PreToolUse" || descriptor.Hook.GateMatcher != ".*" {
		t.Fatalf("unexpected ZCode hook descriptor: %+v", descriptor.Hook)
	}

}

func TestInstallableListsComeFromRegistry(t *testing.T) {
	if got, want := InstallableNames(), []string{"claude", "codex", "cursor", "dsh", "zcode", "workbuddy", "devin"}; !equalStrings(got, want) {
		t.Fatalf("InstallableNames()=%v, want %v", got, want)
	}
	if got, want := InstallableIDs(), []string{Claude, Codex, Cursor, DeepSeek, ZCode, WorkBuddy, "devin"}; !equalStrings(got, want) {
		t.Fatalf("InstallableIDs()=%v, want %v", got, want)
	}
}

func TestDevinDescriptorExposesItsExecutableAndHostContract(t *testing.T) {
	d, err := Lookup("devin")
	if err != nil {
		t.Fatal(err)
	}
	if d.ManifestName != "Devin" || d.Paths.ProjectBase != ".devin/skills" || d.Paths.ProjectHookConfig != ".devin/config.json" || d.Paths.ProjectManaged != "AGENTS.md" {
		t.Fatalf("native host paths: %+v", d)
	}
	if d.Hook.Kind != HookNested || d.Hook.Protocol != ProtocolGeneric || d.Hook.GateEvent != "PreToolUse" || d.Hook.GateMatcher != ".*" || !d.Hook.BindProvider || d.Hook.ProjectDirEnv != "DEVIN_PROJECT_DIR" {
		t.Fatalf("native hooks: %+v", d.Hook)
	}
	if d.Confirmation != ConfirmationHostPermission {
		t.Fatalf("devin confirmation channel: %q", d.Confirmation)
	}
	if d.ReadUsage != nil || d.LifecycleRequired {
		t.Fatalf("unsupported capabilities advertised: %+v", d)
	}
	if len(d.Configuration) != 1 || d.Configuration[0].Name != "devinExecutable" || d.Configuration[0].Default != "devin" {
		t.Fatalf("devin executable configuration: %+v", d.Configuration)
	}
	adapter, err := LookupAdapter("devin")
	if err != nil {
		t.Fatal(err)
	}
	// The bare descriptor (no registered native adapter in this binary) keeps
	// the adapter-less contract: never an execution-selection platform.
	if _, ok := adapter.(SelectionAdapter); ok {
		t.Fatal("unverified native model catalogue advertised")
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

func TestWorkBuddyDescriptorProvidesNativeInstallationWithoutGlobalHooks(t *testing.T) {
	descriptor, err := Lookup(WorkBuddy)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Paths.GlobalBase != ".codebuddy/skills" || descriptor.Paths.ProjectManaged != "CODEBUDDY.md" || descriptor.Paths.GlobalHookConfig != "" || descriptor.Paths.ProjectHookConfig != "" || !descriptor.LifecycleRequired {
		t.Fatalf("native WorkBuddy install contract=%+v", descriptor)
	}
}

func TestConversationSessionReadsNativeToolMetadata(t *testing.T) {
	for _, test := range []struct {
		metadata, session string
		wantError         bool
	}{
		{metadata: ""},
		{metadata: `{"com.zcode/request-context":{"session_id":"zcode-session","turn_id":"original-turn"}}`, session: "zcode-session"},
		{metadata: `{"x-codex-turn-metadata":{"thread_id":"codex-session","turn_id":"original-turn"}}`, session: "codex-session"},
		{metadata: `{"x-codex-turn-metadata":"{\"thread_id\":\"codex-string-session\",\"turn_id\":\"original-turn\"}"}`, session: "codex-string-session"},
		{metadata: `{"thread_id":"codex-flat-session","threadId":"codex-flat-session"}`, session: "codex-flat-session"},
		{metadata: `{"threadId":"codex-camel-session"}`, session: "codex-camel-session"},
		{metadata: `{"x-codex-turn-metadata":"invalid-json"}`, wantError: true},
	} {
		session, err := ConversationSession([]byte(test.metadata))
		if session != test.session || (err != nil) != test.wantError {
			t.Fatalf("metadata %s: session=%q err=%v, want session=%q error=%v", test.metadata, session, err, test.session, test.wantError)
		}
	}
}
