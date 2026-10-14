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
	root := t.TempDir()
	resolved := CostSourceBridge(root, "cost-run")(protocol.CostSourceRef{RunID: "cost-run", ActionID: "act:missing", Provider: "claude"})
	if resolved.State != protocol.CostSourceUnavailable || resolved.Reason != "dispatch has no lifecycle binding" {
		t.Fatalf("missing binding resolution = %v, want UNAVAILABLE", resolved.State)
	}
}

func TestCostSourceBridgeResolvesBoundNativeUsage(t *testing.T) {
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
