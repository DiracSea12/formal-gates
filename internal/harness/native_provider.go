package harness

import (
	"fmt"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"strings"
)

// nativeImplementation is the execution boundary for one native provider.
type nativeImplementation struct {
	resolvePaths      func(map[string]string, map[string]string) error
	fixturePaths      func(host.NativeFixture) (map[string]string, error)
	cleanup           func(string) error
	captureConfigured func(settings.Values) (map[string]any, error)
	capture           func() (map[string]any, error)
	validateBinding   func(binding any) error
	verifySource      func(directory string, source any) error
	verifyStart       func(request, acknowledgement any, identity string) error
	inheritInvocation func(directory string, request map[string]any, source any) error
	reconcile         func(directory string, process any, services ...ExecutionServices) (map[string]any, error)
	execute           func(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error)
}

// Builtin composition extends the original host objects with native transport.
func init() {
	platforms := []struct {
		provider string
		nativeImplementation
	}{
		{"dsh", nativeImplementation{resolvePaths: dshResolvePaths, fixturePaths: dshFixturePaths, captureConfigured: dshConfiguredBinding, verifySource: dshVerifySource, verifyStart: acpVerifyStart, reconcile: dshReconcile, execute: dshExecute}},
		{"zcode", nativeImplementation{resolvePaths: zcodeResolvePaths, fixturePaths: zcodeFixturePaths, captureConfigured: zcodeConfiguredBinding, verifySource: zcodeVerifySource, verifyStart: zcodeVerifyStart, reconcile: zcodeReconcile, execute: zcodeExecute}},
		{"claude", nativeImplementation{resolvePaths: claudeResolvePaths, fixturePaths: claudeFixturePaths, captureConfigured: claudeConfiguredBinding, verifySource: claudeVerifySource, verifyStart: nativeInitAcknowledgement, reconcile: claudeReconcile, execute: claudeExecute}},
		{"codex", nativeImplementation{resolvePaths: codexResolvePaths, fixturePaths: codexFixturePaths, captureConfigured: codexConfiguredBinding, verifySource: codexVerifySource, verifyStart: codexCheckConfiguration, reconcile: codexReconcile, execute: codexExecute}},
		{"workbuddy", nativeImplementation{resolvePaths: workbuddyResolvePaths, fixturePaths: workbuddyFixturePaths, cleanup: workbuddyInvocationCleanup, captureConfigured: workbuddyConfiguredBinding, verifySource: workbuddyVerifySource, verifyStart: nativeInitAcknowledgement, reconcile: workbuddyReconcile, execute: workbuddyExecute}},
		{"cursor", nativeImplementation{resolvePaths: cursorResolvePaths, fixturePaths: cursorFixturePaths, captureConfigured: cursorConfiguredBinding, verifySource: cursorVerifySource, verifyStart: nativeInitAcknowledgement, inheritInvocation: cursorInheritInvocation, reconcile: cursorReconcile, execute: cursorExecute}},
		{"devin", nativeImplementation{resolvePaths: devinResolvePaths, fixturePaths: devinFixturePaths, captureConfigured: devinConfiguredBinding, verifySource: devinVerifySource, verifyStart: acpVerifyStart, reconcile: devinReconcile, execute: devinExecute}},
	}

	for _, entry := range platforms {
		implementation := entry.nativeImplementation
		implementation.capture = nativeCapture(implementation.resolvePaths, implementation.captureConfigured)
		implementation.validateBinding = func(binding any) error {
			return validateNativeBinding(binding, entry.provider)
		}
		base, err := host.LookupAdapter(entry.provider)
		if err != nil {
			panic(err)
		}
		if err := registerNativePlatform(base.HostDescriptor(), implementation); err != nil {
			panic(err)
		}
	}
}

// nativeCapture resolves live native paths and freezes them into a binding.
func nativeCapture(resolvePaths func(map[string]string, map[string]string) error, configured func(settings.Values) (map[string]any, error)) func() (map[string]any, error) {
	return func() (map[string]any, error) {
		native, sources := map[string]string{}, map[string]string{}
		if err := resolvePaths(native, sources); err != nil {
			return nil, err
		}
		return configured(settings.Values{Native: native})
	}
}

type nativePlatform struct {
	*host.Descriptor
	implementation nativeImplementation
}

func (p *nativePlatform) nativeBoundary() nativeImplementation { return p.implementation }

func (p *nativePlatform) ResolveNativePaths(native, sources map[string]string) error {
	if p.implementation.resolvePaths == nil {
		return fmt.Errorf("native path resolver unavailable for %s", p.ID)
	}
	return p.implementation.resolvePaths(native, sources)
}

func (p *nativePlatform) FixtureNativePaths(fixture host.NativeFixture) (map[string]string, error) {
	if p.implementation.fixturePaths == nil {
		return nil, fmt.Errorf("native fixture binding unavailable for %s", p.ID)
	}
	return p.implementation.fixturePaths(fixture)
}

type nativeCapability interface {
	host.Adapter
	nativeBoundary() nativeImplementation
}

func registerNativePlatform(descriptor *host.Descriptor, implementation nativeImplementation) error {
	return host.Register(&nativePlatform{descriptor, implementation})
}

func nativeImplementationFor(provider string) (nativeImplementation, error) {
	if entry, err := host.LookupAdapter(provider); err == nil {
		if capability, ok := entry.(nativeCapability); ok && capability.HostDescriptor().InstallName == provider {
			return capability.nativeBoundary(), nil
		}
	}
	names := []string{}
	for _, entry := range host.RegisteredAdapters() {
		if capability, ok := entry.(nativeCapability); ok {
			names = append(names, capability.HostDescriptor().InstallName)
		}
	}
	return nativeImplementation{}, &blockedError{message: fmt.Sprintf("UNSUPPORTED_NATIVE_HOST: %s; execution adapters: %s", provider, strings.Join(names, ", "))}
}
func nativeInitAcknowledgement(_ any, acknowledgement any, identity string) error {
	if getOf(acknowledgement, "type") != "system" || getOf(acknowledgement, "subtype") != "init" || getOf(acknowledgement, "session_id") != identity {
		return fmt.Errorf("native init acknowledgement differs")
	}
	return nil
}
