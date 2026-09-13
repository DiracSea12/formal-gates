package facade

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

// Configuration capture can succeed while a registered platform rejects the
// required runtime capability. No native process is needed to test admission.
type handoffCapabilityFixture struct {
	*fixtureRuntime
	unavailable bool
	validations int
}

func (a *handoffCapabilityFixture) capture(settings.Values) (*protocol.HostRuntimeContext, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return captureStreamContext(a.id, executable, map[string]string{})
}

func (a *handoffCapabilityFixture) validate(binding *protocol.HostRuntimeContext) error {
	a.validations++
	if a.unavailable {
		return fmt.Errorf("FIXTURE_CAPABILITY: required native resume is unavailable")
	}
	return a.fixtureRuntime.validate(binding)
}

func TestExecutorHandoffValidatesRegisteredTargetBeforeDecision(t *testing.T) {
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	adapter := &handoffCapabilityFixture{fixtureRuntime: &fixtureRuntime{
		id: "handoff-capability-" + suffix, executableOption: "handoffExecutable" + suffix,
		stateOption: "handoffState" + suffix, serverVersion: 1, adapterVersion: 1,
	}, unavailable: true}
	if err := registerNativePlatform(adapter.descriptor(), adapter); err != nil {
		t.Fatal(err)
	}
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	before, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.RegularRequest{Action: "executor-handoff", Target: x.Claim.ActionID,
		Reason:  "fixture user selects a registered native target",
		Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "native", Provider: adapter.id, Label: "local capability fixture"}}}
	event, err := protocol.NewRegularEvent("target-refuses-before-ask", protocol.RegularEvent{
		Operation: "REQUEST", ExpectedRevision: before.Revision, Request: &request})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(event); err == nil || !strings.Contains(err.Error(), "FIXTURE_CAPABILITY") {
		t.Fatalf("registered target was accepted despite its capability refusal: %v", err)
	}
	rejected, _ := f.Show()
	if rejected.Revision != before.Revision || rejected.Regular.Ask != nil || !reflect.DeepEqual(rejected.Executions, before.Executions) {
		t.Fatal("failed target admission changed the action or created an Ask")
	}
	adapter.unavailable = false
	event.ID = "target-available-before-ask"
	_, preview, err := f.Submit(event)
	if err != nil || preview.Regular.Ask == nil {
		t.Fatal("available target did not produce a typed Ask", err)
	}
	ask := preview.Regular.Ask
	adapter.unavailable = true
	decision, err := protocol.NewRegularEvent("target-refuses-before-confirm", protocol.RegularEvent{
		Operation: "DECIDE", ExpectedRevision: preview.Revision,
		Decision: &protocol.RegularDecision{RequestID: ask.ID, Token: ask.Token, Digest: ask.Digest, Choice: "confirm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(decision); err == nil || !strings.Contains(err.Error(), "FIXTURE_CAPABILITY") {
		t.Fatalf("target capability was not rechecked before handoff: %v", err)
	}
	after, _ := f.Show()
	if after.Revision != preview.Revision || !reflect.DeepEqual(after.Executions, preview.Executions) || len(after.Regular.ExecutorHandoffs) != 0 {
		t.Fatal("failed capability recheck issued a replacement action")
	}
	if adapter.validations != 3 {
		t.Fatalf("expected request/refreshed-request/decision capability checks, got %d", adapter.validations)
	}
	t.Log("registered target validation is required before both the Ask and its decision; refusal preserves the old action and no native call is launched")
}
