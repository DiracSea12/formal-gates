package harness

import (
	"fmt"
	"formal-gates/internal/settings"
	"strings"
)

// nativeImplementation is the execution boundary for one native provider.
type nativeImplementation struct {
	cleanup           func(string) error
	captureConfigured func(settings.Values) (map[string]any, error)
	capture           func() (map[string]any, error)
	validateBinding   func(binding any) error
	verifySource      func(directory string, source any) error
	verifyStart       func(request, acknowledgement any, identity string) error
	reconcile         func(directory string, process any) (map[string]any, error)
	execute           func(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error)
}

// Ordered entries own both routing and unsupported-host diagnostics.
var nativeImplementations = []struct {
	provider string
	nativeImplementation
}{
	{"dsh", nativeImplementation{capture: dshCaptureBinding, captureConfigured: dshConfiguredBinding, validateBinding: dshValidateBinding, verifySource: dshVerifySource, verifyStart: dshVerifyStart, reconcile: dshReconcile, execute: dshExecute}},
	{"zcode", nativeImplementation{capture: zcodeCaptureBinding, captureConfigured: zcodeConfiguredBinding, validateBinding: zcodeValidateBinding, verifySource: zcodeVerifySource, verifyStart: zcodeVerifyStart, reconcile: zcodeReconcile, execute: zcodeExecute}},
	{"claude", nativeImplementation{capture: claudeCaptureBinding, captureConfigured: claudeConfiguredBinding, validateBinding: claudeValidateBinding, verifySource: claudeVerifySource, verifyStart: nativeInitAcknowledgement, reconcile: claudeReconcile, execute: claudeExecute}},
	{"codex", nativeImplementation{capture: codexCaptureBinding, captureConfigured: codexConfiguredBinding, validateBinding: codexValidateBinding, verifySource: codexVerifySource, verifyStart: codexCheckConfiguration, reconcile: codexReconcile, execute: codexExecute}},
	{"workbuddy", nativeImplementation{cleanup: workbuddyInvocationCleanup, capture: workbuddyCaptureBinding, captureConfigured: workbuddyConfiguredBinding, validateBinding: workbuddyValidateBinding, verifySource: workbuddyVerifySource, verifyStart: nativeInitAcknowledgement, reconcile: workbuddyReconcile, execute: workbuddyExecute}},
	{"cursor", nativeImplementation{capture: cursorCaptureBinding, captureConfigured: cursorConfiguredBinding, validateBinding: cursorValidateBinding, verifySource: cursorVerifySource, verifyStart: nativeInitAcknowledgement, reconcile: cursorReconcile, execute: cursorExecute}},
}

func nativeImplementationFor(provider string) (nativeImplementation, error) {
	names := make([]string, 0, len(nativeImplementations))
	for _, entry := range nativeImplementations {
		if entry.provider == provider {
			return entry.nativeImplementation, nil
		}
		names = append(names, entry.provider)
	}
	return nativeImplementation{}, &blockedError{message: fmt.Sprintf(
		"UNSUPPORTED_NATIVE_HOST: %s; execution adapters: %s", provider, strings.Join(names, ", "))}
}

func nativeInitAcknowledgement(_ any, acknowledgement any, identity string) error {
	if getOf(acknowledgement, "type") != "system" || getOf(acknowledgement, "subtype") != "init" || getOf(acknowledgement, "session_id") != identity {
		return fmt.Errorf("native init acknowledgement differs")
	}
	return nil
}
