package protocol

import "testing"

// observedStoppedWorker supplies the native identity and termination facts
// required before a fresh execution can replace the original dispatch.
func observedStoppedWorker(t *testing.T, e *Engine, action string) {
	t.Helper()
	s := e.LoadMustSucceed(t).State
	receipt, known := s.SpawnReceipts[action]
	if !known {
		submitSpawnReceipt(t, e, "spawn-stopped-"+action, action)
		receipt = e.LoadMustSucceed(t).State.SpawnReceipts[action]
	}
	identity := receipt.agentIdentity()
	if identity == "" {
		identity = "stopped-" + action
	}
	for _, kind := range []string{LifecycleStart, LifecycleStop} {
		ev := mustLifecycleEvent(t, "observe-"+kind+"-"+action, receipt.Correlation, identity, kind)
		if _, err := e.Submit(ev, fingerprintOf(t, e)); err != nil {
			t.Fatal(err)
		}
	}
	if receipt.Status == SpawnStatusUnknown {
		if _, _, err := e.ReconcileUnknownReceipt(action, fingerprintOf(t, e)); err != nil {
			t.Fatal(err)
		}
	}
}
