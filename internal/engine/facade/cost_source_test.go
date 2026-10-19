package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

func digestOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// TestCostSourceBridgeReportsUnsupportedHostUnavailable：宿主不暴露可用
// lifecycle 事件（LifecycleRequired=false 的宽松 provider）不得被当成等待中的
// 成本来源——解析结果必须是 UNAVAILABLE，否则结果会永久停在 COST_PENDING。
func TestCostSourceBridgeReportsUnsupportedHostUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runID, action := "cost-run", "act:cost-1"
	if err := lifecycle.BindDispatchWithProvider(root, runID, action, "agent-1", "default"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeStart(root, runID, action, "default", "agent-1"); err != nil {
		t.Fatal(err)
	}
	resolved := CostSourceBridge(root, runID)(protocol.CostSourceRef{RunID: runID, ActionID: action, Provider: "default"})
	if resolved.State != protocol.CostSourceUnavailable || resolved.Reason != "host lifecycle evidence is unavailable" {
		t.Fatalf("lenient host resolution = %v, want UNAVAILABLE", resolved.State)
	}
}

// TestCostSourceBridgeHoldsObservableHostPending：强 lifecycle 宿主上 stop 证据
// 未到（start 在账）仍是 PENDING——回执边界等待语义只对可观测宿主成立。
func TestCostSourceBridgeHoldsObservableHostPending(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runID, action := "cost-run", "act:cost-2"
	if err := lifecycle.BindDispatchWithProvider(root, runID, action, "agent-2", host.Claude); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeStart(root, runID, action, host.Claude, "agent-2"); err != nil {
		t.Fatal(err)
	}
	resolved := CostSourceBridge(root, runID)(protocol.CostSourceRef{RunID: runID, ActionID: action, Provider: host.Claude})
	if resolved.State != protocol.CostSourcePending {
		t.Fatalf("observable host without stop = %v, want PENDING", resolved.State)
	}
}

// TestCostSourceBridgeReportsMissingBindingUnavailable：无 lifecycle 绑定的引用
// 是明确缺源（UNAVAILABLE），不是等待。
func TestCostSourceBridgeReportsMissingBindingUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resolved := CostSourceBridge(root, "cost-run")(protocol.CostSourceRef{RunID: "cost-run", ActionID: "act:missing", Provider: "claude"})
	if resolved.State != protocol.CostSourceUnavailable || resolved.Reason != "dispatch has no lifecycle binding" {
		t.Fatalf("missing binding resolution = %v, want UNAVAILABLE", resolved.State)
	}
}

func TestCostSourceBridgeResolvesBoundNativeUsage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider, content string
		want              cost.Usage
	}{
		{"deepseek-harness", `{"sessionId":"session","event":{"seq":22,"type":"assistant/message","data":{"usage":{"inputTokens":3728,"cacheReadTokens":8832,"outputTokens":100}}}}`, cost.Usage{InputCacheMissTokens: 3728, InputCacheHitTokens: 8832, OutputTokens: 100}},
		{"zcode", `{"sessionId":"session","turnId":"turn","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":8,"inputTokens":304400,"cacheReadTokens":270848,"outputTokens":23243}}}`, cost.Usage{InputCacheMissTokens: 33552, InputCacheHitTokens: 270848, OutputTokens: 23243}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "native-events.jsonl")
			if err := os.WriteFile(path, []byte(tc.content+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "identity", tc.provider); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.ObserveNativeCompletion(root, "run", "action", tc.provider, "identity"); err != nil {
				t.Fatal(err)
			}
			writeManagedCostSource(t, root, "run", "action", tc.provider, "identity", path)
			bridge := CostSourceBridge(root, "run")
			ref := protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: tc.provider}
			for range 2 {
				resolved := bridge(ref)
				if resolved.State != protocol.CostSourceReady || resolved.Usage != tc.want || resolved.Reason != "" {
					t.Fatalf("bound native source=%+v want=%+v", resolved, tc.want)
				}
			}
		})
	}
}

func TestCostSourceBridgeRetainsMissingSourceReasons(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, provider, refProvider, content, reason string
		withPath                                     bool
	}{
		{"provider mismatch", host.Claude, "zcode", "", "cost source provider does not match dispatch provider", false},
		{"path missing", host.Claude, host.Claude, "", "native dispatch has no completed metering source", false},
		{"usage missing", "zcode", "zcode", `{"sessionId":"session","turnId":"turn","type":"turn.completed","payload":{}}`, "ZCode terminal provider usage is unavailable (including no-backend zero fallback)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := lifecycle.BindDispatchWithProvider(root, "run", "action", "identity", tc.provider); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.ObserveNativeCompletion(root, "run", "action", tc.provider, "identity"); err != nil {
				t.Fatal(err)
			}
			if tc.withPath {
				path := filepath.Join(root, "native-events.jsonl")
				if err := os.WriteFile(path, []byte(tc.content+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				writeManagedCostSource(t, root, "run", "action", tc.provider, "identity", path)
			}
			resolved := CostSourceBridge(root, "run")(protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: tc.refProvider})
			if resolved.State != protocol.CostSourceUnavailable || !strings.Contains(resolved.Reason, tc.reason) || resolved.Usage != (cost.Usage{}) {
				t.Fatalf("missing source=%+v want reason=%q", resolved, tc.reason)
			}
		})
	}
}

func TestExecutionSelectionCostUsesOnlyClaimedInvocations(t *testing.T) {
	f, root := workerMaterialFacade(t)
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var ready WorkerMaterial
	for _, ref := range run.WorkerMaterials {
		ready = ref
	}
	oldRequest, oldSupervisor, oldDirectory, _, err := f.WorkerExecutionRequest(ready, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(oldDirectory, "native-request.json"), oldRequest); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(oldDirectory, "request.json"), oldSupervisor); err != nil {
		t.Fatal(err)
	}
	platform := selectionPlatformFixture(t, "selected", true)
	descriptor, err := host.Lookup("zcode")
	if err != nil {
		t.Fatal(err)
	}
	platform.descriptor.ReadUsage = descriptor.ReadUsage
	platform.descriptor.LifecycleRequired = true
	saveExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "Careful"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	release, err := AdmitExecution(oldDirectory, oldSupervisor)
	if release != nil {
		release()
	}
	if err == nil {
		t.Fatal("pre-selection material unexpectedly admitted")
	}
	if _, err := os.Stat(filepath.Join(oldDirectory, "owner")); !os.IsNotExist(err) {
		t.Fatal("rejected preparation acquired ownership", err)
	}
	request, _, directory, selected, err := f.WorkerExecutionRequest(ready, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.ActionID != ready.ActionID || directory == oldDirectory {
		t.Fatal("selection did not refresh material for the same action")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	release, err = AdmitExecution(directory, request)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "owner"), nil, 0600)
	release()
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(directory, "native-events.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"sessionId":"session","turnId":"selected","type":"turn.completed","payload":{"usage":{"source":"provider","modelRequestCount":1,"inputTokens":100,"cacheReadTokens":40,"outputTokens":10}}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(directory, "source.json"), map[string]any{"transcript": transcript}); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BindDispatchWithProvider(root, f.runID, selected.ActionID, "session", platform.descriptor.ID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeCompletion(root, f.runID, selected.ActionID, platform.descriptor.ID, "session"); err != nil {
		t.Fatal(err)
	}
	verifier := func(provider, observed string, source map[string]any) ([]byte, string, string, error) {
		if provider != platform.descriptor.ID || observed != directory || source["transcript"] != transcript {
			t.Fatal("cost verification lost its original provider/invocation/transcript", provider, observed, source)
		}
		return []byte("answer"), "sha256:answer", "session", nil
	}
	resolve := CostSourceBridge(root, f.runID, verifier)
	ref := protocol.CostSourceRef{RunID: f.runID, ActionID: selected.ActionID, Provider: platform.descriptor.ID, Correlation: "session"}
	want := cost.Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 60, OutputTokens: 10}
	for range 2 {
		if resolved := resolve(ref); resolved.State != protocol.CostSourceReady || resolved.Usage != want || resolved.Reason != "" {
			t.Fatalf("unclaimed preparation affected measured execution: %+v", resolved)
		}
	}
	if err := os.WriteFile(filepath.Join(oldDirectory, "owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if resolved := resolve(ref); resolved.State != protocol.CostSourceUnavailable || resolved.Usage != want || !strings.Contains(resolved.Reason, "original run/action/provider/material") {
		t.Fatalf("claimed foreign execution lost its unavailable evidence: %+v", resolved)
	}
}
