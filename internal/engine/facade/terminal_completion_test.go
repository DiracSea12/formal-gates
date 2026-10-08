package facade

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// 本文件验证 regular run 的终态完成机制：终态收口以「终 wave COMPLETE＋候选
// 晋升绑定（内容与已验证最终候选相等）」为共同事实，验证收集期晋升被意图准
// 入拒绝（零 Git 副作用）；两事实齐备后 Drive 核验交付并相变到终态；终态后的
// 重入 Drive 只读。

// concludePromoted 把 regular run 推进到 COMPLETE：验证收集期晋升被拒（wave
// 未 COMPLETE——准入在先，零 Git 副作用、无意图落账），补齐 gate 结果使 wave
// COMPLETE 后晋升收敛，最终 Drive 核验交付到达终态。返回到达终态的驱动。
func concludePromoted(t *testing.T) *validationDriver {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root, "implementation", "reviewed changes")[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	if _, err := d.f.PrepareCandidate(""); err == nil || !strings.Contains(err.Error(), "final validation wave to be COMPLETE") {
		t.Fatalf("promotion during collection: %v", err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != "" {
		t.Fatalf("rejected promotion left an intent behind: %q", pendingPromotionSource(snap.State))
	}
	qa, qi := d.action("engine.qa.execution")
	gate, gi := d.action("engine.gate")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COMPLETE" {
		t.Fatalf("wave status = %s, want COMPLETE after the final gate result", wave.Status)
	}
	promoted, err := d.f.PrepareCandidate("organized final candidate")
	if err != nil {
		t.Fatalf("promotion after wave conclusion: %v", err)
	}
	if promoted == frozenSnapshot {
		t.Fatalf("multi-commit promotion kept the base identity: %s", promoted)
	}
	d.drive()
	if d.run.Status != "COMPLETE" {
		t.Fatalf("run status = %s, want COMPLETE (next = %+v)", d.run.Status, d.run.Next)
	}
	if d.run.PromotedSnapshot != promoted {
		t.Fatalf("promoted snapshot = %s, want %s", d.run.PromotedSnapshot, promoted)
	}
	return d
}

// TestTerminalCompletionConvergesAfterWaveConclusionAndPromotion：收集期晋升被
// 拒后，wave 完成与晋升绑定两个事实齐备时，Drive 把 run 收口到 COMPLETE 且引
// 擎 phase 进入 TERMINAL。
func TestTerminalCompletionConvergesAfterWaveConclusionAndPromotion(t *testing.T) {
	d := concludePromoted(t)
	summary, err := d.f.readSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Termination == nil || summary.Termination.Phase != runtime.PhaseTerminal {
		t.Fatalf("engine phase = %s, want TERMINAL", summary.Termination.Phase)
	}
}

// TestTerminalCompletionRequiresWaveConclusionAndPromotion：收口的两个事实各
// 缺其一都不达终态——wave 未完成时晋升被拒（不得出现「先绑定后收集」的交
// 错）；wave 完成而未晋升时晋升等待 Operator fact 仍在。
func TestTerminalCompletionRequiresWaveConclusionAndPromotion(t *testing.T) {
	t.Run("promotion rejected before wave conclusion", func(t *testing.T) {
		root := t.TempDir()
		base := snapshotRepoCommits(t, root, "implementation", "reviewed changes")[0]
		d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
		d.reviewReady()
		if _, err := d.f.PrepareCandidate(""); err == nil || !strings.Contains(err.Error(), "final validation wave to be COMPLETE") {
			t.Fatalf("promotion with the wave unresolved: %v", err)
		}
		d.drive()
		if d.run.Status == "COMPLETE" {
			t.Fatal("terminal reached with the wave unresolved")
		}
		wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
		if wave.Status == "COMPLETE" {
			t.Fatal("premise lost: the wave already concluded")
		}
		snap, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if snap.State.Phase == runtime.PhaseTerminal {
			t.Fatal("engine phase reached TERMINAL with the wave unresolved")
		}
		if snap.State.PromotedSnapshot != "" || pendingPromotionSource(snap.State) != "" {
			t.Fatalf("rejected promotion bound state: promoted=%q pending=%q", snap.State.PromotedSnapshot, pendingPromotionSource(snap.State))
		}
	})
	t.Run("wave conclusion without promotion", func(t *testing.T) {
		root := t.TempDir()
		base := snapshotRepoCommits(t, root, "implementation", "reviewed changes")[0]
		d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
		d.reviewReady()
		qa, qi := d.action("engine.qa.execution")
		gate, gi := d.action("engine.gate")
		report := executionFor(qi)
		tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
		d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
		d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
		shown, err := d.f.Show()
		d.run = shown
		if err != nil {
			t.Fatal(err)
		}
		if d.run.Status == "COMPLETE" {
			t.Fatal("terminal reached without candidate promotion")
		}
		promotionWait := false
		if d.run.Next.Operator != nil {
			for _, fact := range d.run.Next.Operator.Facts {
				if fact.Key == "validation" && strings.Contains(fact.Value, "candidate promotion") {
					promotionWait = true
				}
			}
		}
		if !promotionWait {
			t.Fatalf("terminal boundary = %+v, want promotion wait", d.run.Next)
		}
	})
}

// TestTerminalDriveReplayIsReadOnly：终态后的重入 Drive 只读——不 bump
// revision、不改写权威状态字节，也不改写已落盘的终态摘要字节。
func TestTerminalDriveReplayIsReadOnly(t *testing.T) {
	d := concludePromoted(t)
	revision := d.run.Revision
	statePath := filepath.Join(d.f.stateDir, "state.json")
	summaryPath := filepath.Join(d.f.stateDir, "terminal-summary.json")
	stateBefore, err := os.ReadFile(statePath)
	if !os.IsNotExist(err) {
		t.Fatalf("terminal active state: %v", err)
	}
	summaryBefore, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	d.drive()
	if d.run.Status != "COMPLETE" {
		t.Fatalf("terminal replay status = %s, want COMPLETE", d.run.Status)
	}
	if d.run.Revision != revision {
		t.Fatalf("terminal replay bumped revision %d -> %d", revision, d.run.Revision)
	}
	stateAfter, err := os.ReadFile(statePath)
	if !os.IsNotExist(err) {
		t.Fatalf("replayed active state: %v", err)
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("terminal replay rewrote the authoritative state bytes")
	}
	summaryAfter, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(summaryBefore, summaryAfter) {
		t.Fatal("terminal replay rewrote the immutable terminal summary bytes")
	}
}
