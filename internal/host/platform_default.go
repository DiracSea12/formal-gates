package host

var defaultDescriptor = &Descriptor{
	Lifecycle:       defaultAdapter(),
	ID:              Default,
	InstallName:     "",
	ManifestName:    "",
	Aliases:         []string{"default"},
	Installable:     false,
	Hook:            HookSpec{Kind: HookNested, Protocol: ProtocolGeneric},
	LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: false,
}
