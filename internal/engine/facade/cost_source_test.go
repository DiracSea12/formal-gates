package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	writeLifecycleSidecar(t, root, runID, action, "default", "agent-1", []string{"start"})
	resolved := CostSourceBridge(root, runID)(protocol.CostSourceRef{RunID: runID, ActionID: action, Provider: "default"})
	if resolved.State != protocol.CostSourceUnavailable {
		t.Fatalf("lenient host resolution = %v, want UNAVAILABLE", resolved.State)
	}
}

// TestCostSourceBridgeHoldsObservableHostPending：强 lifecycle 宿主上 stop 证据
// 未到（start 在账）仍是 PENDING——回执边界等待语义只对可观测宿主成立。
func TestCostSourceBridgeHoldsObservableHostPending(t *testing.T) {
	root := t.TempDir()
	runID, action := "cost-run", "act:cost-2"
	writeLifecycleSidecar(t, root, runID, action, "claude", "agent-2", []string{"start"})
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
	if resolved.State != protocol.CostSourceUnavailable {
		t.Fatalf("missing binding resolution = %v, want UNAVAILABLE", resolved.State)
	}
}
