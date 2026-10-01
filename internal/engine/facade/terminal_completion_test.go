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

// 本文件验证 regular run 的终态完成机制：验证期步骤的收口事实（终 wave
// COMPLETE＋候选晋升绑定）无论先后落在哪个事务，下一次 Drive 都完成收口；
// 两个事实各缺其一都不达终态；终态后的重入 Drive 只读。

// concludeInterleaved 以「晋升先于终 wave 完成」的交错序把 regular run 推进
// 到 COMPLETE：reviewReady 冻结首 wave 后先绑定晋升，最后补齐 gate 结果（wave
// COMPLETE 落在结果 Submit 事务内），下一次 Drive 收口到终态。返回到达终态
// 的驱动。
func concludeInterleaved(t *testing.T) *validationDriver {
	t.Helper()
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	promoted, err := d.f.PrepareCandidate("")
	if err != nil {
		t.Fatalf("promotion before wave conclusion: %v", err)
	}
	if promoted != frozenSnapshot {
		t.Fatalf("promoted snapshot = %s, want %s", promoted, frozenSnapshot)
	}
	qa, qi := d.action("engine.qa.execution")
	gate, gi := d.action("engine.gate")
	report := executionFor(qi)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COMPLETE" {
		t.Fatalf("wave status = %s, want COMPLETE after the final gate result", wave.Status)
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

// TestTerminalCompletionConvergesWhenPromotionPrecedesWaveConclusion：晋升绑
// 定与终 wave 完成的交错序——两个事实分属不同事务时，下一次 Drive 把 run 收
// 口到 COMPLETE 且引擎 phase 进入 TERMINAL。
func TestTerminalCompletionConvergesWhenPromotionPrecedesWaveConclusion(t *testing.T) {
	d := concludeInterleaved(t)
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Phase != runtime.PhaseTerminal {
		t.Fatalf("engine phase = %s, want TERMINAL", snap.State.Phase)
	}
}

// TestTerminalCompletionRequiresWaveConclusionAndPromotion：收口的两个事实各
// 缺其一都不达终态——仅绑定晋升而 wave 未完成不得终态；wave 完成而未晋升时
// 晋升等待 Operator fact 仍在。
func TestTerminalCompletionRequiresWaveConclusionAndPromotion(t *testing.T) {
	t.Run("promotion without wave conclusion", func(t *testing.T) {
		d := newValidationDriver(t, "blackbox", "quality")
		d.reviewReady()
		if _, err := d.f.PrepareCandidate(""); err != nil {
			t.Fatal(err)
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
	})
	t.Run("wave conclusion without promotion", func(t *testing.T) {
		d := newValidationDriver(t, "blackbox", "quality")
		d.reviewReady()
		qa, qi := d.action("engine.qa.execution")
		gate, gi := d.action("engine.gate")
		report := executionFor(qi)
		d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
		d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
		d.drive()
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
	d := concludeInterleaved(t)
	revision := d.run.Revision
	statePath := filepath.Join(d.f.stateDir, "state.json")
	summaryPath := filepath.Join(d.f.stateDir, "terminal-summary.json")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatal(err)
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
