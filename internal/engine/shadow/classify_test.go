package shadow_test

import (
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/shadow"
)

// TestShadowClassifyVerdicts 逐一钉死三类差异判定的分类规则：终态性比对
// （一致/不一致）、双方表达外部边界时的类型比对（一致/不一致）、以及不可
// 比较的两类来源（实际不可推断；预测 Wait 未表达外部边界）。
func TestShadowClassifyVerdicts(t *testing.T) {
	ready := decision.NextResult{Kind: decision.KindReady, Ready: &decision.ReadyPayload{}}
	host := decision.NextResult{Kind: decision.KindHostAction, HostAction: &decision.HostActionPayload{}}
	ask := decision.NextResult{Kind: decision.KindAsk, Ask: &decision.AskPayload{}}
	wait := decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitTasksInFlight}}
	complete := decision.NextResult{Kind: decision.KindComplete, Complete: &decision.CompletePayload{}}
	for _, n := range []decision.NextResult{ready, host, ask, wait, complete} {
		if err := n.Validate(); err != nil {
			t.Fatalf("fixture next result invalid: %v", err)
		}
	}
	cases := []struct {
		name   string
		next   decision.NextResult
		actual shadow.ActualNext
		want   shadow.Verdict
	}{
		{"both terminal", complete, shadow.ActualNext{Inferable: true, Step: "terminal", Boundary: shadow.BoundaryTerminal}, shadow.VerdictMatch},
		{"predicted terminal but run ongoing", complete, shadow.ActualNext{Inferable: true, Step: "action:product-review", Boundary: shadow.BoundaryAgent}, shadow.VerdictMismatch},
		{"run terminal but prediction ongoing", wait, shadow.ActualNext{Inferable: true, Step: "terminal", Boundary: shadow.BoundaryTerminal}, shadow.VerdictMismatch},
		{"agent boundary agrees", ready, shadow.ActualNext{Inferable: true, Step: "action:product-review", Boundary: shadow.BoundaryAgent}, shadow.VerdictMatch},
		{"host boundary agrees", host, shadow.ActualNext{Inferable: true, Step: "gate:gate.build", Boundary: shadow.BoundaryHost}, shadow.VerdictMatch},
		{"human boundary agrees", ask, shadow.ActualNext{Inferable: true, Step: "seal", Boundary: shadow.BoundaryHuman}, shadow.VerdictMatch},
		{"boundary type differs", ready, shadow.ActualNext{Inferable: true, Step: "gate:gate.build", Boundary: shadow.BoundaryHost}, shadow.VerdictMismatch},
		{"prediction expresses no boundary", wait, shadow.ActualNext{Inferable: true, Step: "action:product-review", Boundary: shadow.BoundaryAgent}, shadow.VerdictIncomparable},
		{"actual not inferable", ready, shadow.ActualNext{}, shadow.VerdictIncomparable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shadow.Classify(tc.next, tc.actual); got != tc.want {
				t.Fatalf("Classify = %q, want %q", got, tc.want)
			}
		})
	}
}
