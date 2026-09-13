package host

var cursorDescriptor = &Descriptor{
	Lifecycle:    cursorAdapter(),
	ID:           Cursor,
	InstallName:  "cursor",
	ManifestName: "Cursor",
	Aliases:      []string{"cursor"},
	Installable:  true,
	Paths: PathLayout{
		GlobalBase: ".cursor", GlobalHookConfig: ".cursor/hooks.json",
		ProjectBase: ".cursor", ProjectHookConfig: ".cursor/hooks.json", ProjectManaged: ".cursor/rules/formal-gates.mdc", RemoveManagedWhenEmpty: true,
	},
	Hook:            HookSpec{Kind: HookFlat, Protocol: ProtocolGeneric, GateEvent: "preToolUse", GateMatcher: "*"},
	LifecycleEvents: []string{"subagentStart", "subagentStop"}, LifecycleRequired: true,
}
