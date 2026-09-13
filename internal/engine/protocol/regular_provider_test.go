package protocol

import "testing"

// Native transports are admitted by the facade; the shared engine must keep
// their bindings through confirmation, result acceptance and disk recovery.
func TestRegularProviderBindingAcrossRecovery(t *testing.T) {
	for _, provider := range []string{"codex", "workbuddy", "zcode", "deepseek-harness"} {
		t.Run(provider, func(t *testing.T) {
			e := regularEngineForProvider(t, nil, t.TempDir(), provider)
			regularStart(t, e)
			x := currentExecution(t, e, "product-review")
			if x.Claim.Provider != provider {
				t.Fatalf("claim provider = %q, want %q", x.Claim.Provider, provider)
			}
			submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
			identity := "native-" + x.Claim.ActionID
			submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: provider, Identity: identity, Status: "RUNNING"}})
			recovered, err := New(e.store, e.cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			v := resultFor(x)
			v.Provider = provider
			submitRegular(t, recovered, RegularEvent{Operation: "REVIEW_RESULT", Review: &v})
			s, err := recovered.Load()
			if err != nil {
				t.Fatal(err)
			}
			if s.State.RunProvider != provider || s.State.Regular.Reviews["product-review"].Completed != 1 || s.State.Regular.Guard.Status != "COST_PENDING" {
				t.Fatalf("provider/result/cost binding lost: provider=%q regular=%+v", s.State.RunProvider, s.State.Regular)
			}
		})
	}
}
