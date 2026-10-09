package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
)

func digestOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// writeLifecycleSidecar 在 lifecycle sidecar 里落一条派发绑定与若干事件记录
// （与 internal/lifecycle 的 journal 文件布局一致）。
func writeLifecycleSidecar(t *testing.T, root, runID, action, provider, identity string, events []string) {
	t.Helper()
	lc := filepath.Join(root, ".gates", "tmp", runID, "lifecycle")
	if err := os.MkdirAll(lc, 0o700); err != nil {
		t.Fatal(err)
	}
	binding, err := json.Marshal(map[string]string{"runId": runID, "dispatchId": action, "provider": provider, "identity": identity})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lc, digestOf(action)+".json"), append(binding, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	evDir := filepath.Join(lc, "events", digestOf(action))
	if len(events) > 0 {
		if err := os.MkdirAll(evDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range events {
		record, err := json.Marshal(map[string]string{"provider": provider, "event": event, "identity": identity})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evDir, event+".json"), append(record, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCostSourceBridgeReportsUnsupportedHostUnavailable：宿主不暴露可用
// lifecycle 事件（LifecycleRequired=false 的宽松 provider）不得被当成等待中的
// 成本来源——解析结果必须是 UNAVAILABLE，否则结果会永久停在 COST_PENDING。
func TestCostSourceBridgeReportsUnsupportedHostUnavailable(t *testing.T) {
	root := t.TempDir()
	runID, action := "cost-run", "act:cost-1"
	writeLifecycleSidecar(t, root, runID, action, "default", "agent-1", []string{"subagent_start"})
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
	writeLifecycleSidecar(t, root, runID, action, "claude", "agent-2", []string{"subagent_start"})
	resolved := CostSourceBridge(root, runID)(protocol.CostSourceRef{RunID: runID, ActionID: action, Provider: "claude"})
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
			writeLifecycleSidecar(t, root, "run", "action", tc.provider, "identity", []string{"subagent_start", "subagent_stop"})
			stop, err := json.Marshal(map[string]string{"provider": tc.provider, "event": "subagent_stop", "identity": "identity", "transcriptPath": path})
			if err != nil {
				t.Fatal(err)
			}
			stopPath := filepath.Join(root, ".gates", "tmp", "run", "lifecycle", "events", digestOf("action"), "subagent_stop.json")
			if err := os.WriteFile(stopPath, stop, 0o600); err != nil {
				t.Fatal(err)
			}
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
		{"provider mismatch", "claude", "zcode", "", "cost source provider does not match dispatch provider", false},
		{"path missing", "claude", "claude", "", "dispatch stop has no transcript path", false},
		{"usage missing", "zcode", "zcode", `{"sessionId":"session","turnId":"turn","type":"turn.completed","payload":{}}`, "ZCode terminal provider usage is unavailable (including no-backend zero fallback)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLifecycleSidecar(t, root, "run", "action", tc.provider, "identity", []string{"subagent_start", "subagent_stop"})
			if tc.withPath {
				path := filepath.Join(root, "native-events.jsonl")
				if err := os.WriteFile(path, []byte(tc.content+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				stop, err := json.Marshal(map[string]string{"provider": tc.provider, "event": "subagent_stop", "identity": "identity", "transcriptPath": path})
				if err != nil {
					t.Fatal(err)
				}
				stopPath := filepath.Join(root, ".gates", "tmp", "run", "lifecycle", "events", digestOf("action"), "subagent_stop.json")
				if err := os.WriteFile(stopPath, stop, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			resolved := CostSourceBridge(root, "run")(protocol.CostSourceRef{RunID: "run", ActionID: "action", Provider: tc.refProvider})
			if resolved.State != protocol.CostSourceUnavailable || resolved.Reason != tc.reason || resolved.Usage != (cost.Usage{}) {
				t.Fatalf("missing source=%+v want reason=%q", resolved, tc.reason)
			}
		})
	}
}
