package decision

import (
	"fmt"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
)

// RegularView is the small deterministic decision projection of the authoritative
// protocol state. Results, corrections and authorizations remain in that state.
// The selected review TaskKey is derived from a confirmed document and a finite
// review series; external callers cannot add tasks through this view.
type RegularView struct {
	PreparationAsk       string             `json:"preparationAsk,omitempty"`
	VerificationOperator bool               `json:"verificationOperator"`
	PreparationTasks     []ReadyTask        `json:"preparationTasks,omitempty"`
	PreparationWait      string             `json:"preparationWait,omitempty"`
	ReviewReady          bool               `json:"reviewReady"`
	HostSteps            []authoring.StepID `json:"hostSteps,omitempty"`
	Prechecked           bool               `json:"prechecked"`
	Confirmed            bool               `json:"confirmed"`
	Mode                 string             `json:"mode"`
	ProductStatus        string             `json:"productStatus"`
	TechnicalStatus      string             `json:"technicalStatus"`
	ProductTask          runtime.TaskKey    `json:"productTask"`
	TechnicalTask        runtime.TaskKey    `json:"technicalTask"`
	TopologyConfirmed    bool               `json:"topologyConfirmed"`
	RouteConfirmed       bool               `json:"routeConfirmed"`
	Control              string             `json:"control,omitempty"`
	GuardReason          string             `json:"guardReason,omitempty"`
	Terminal             bool               `json:"terminal"`
}

func decideRegular(s *State, obs Observation, cd *compiler.CompiledDefinition) (*Plan, error) {
	v := s.Regular
	var next NextResult
	step := ""
	ask := func(id string) {
		step = id
		next = NextResult{Kind: KindAsk, Ask: &AskPayload{Steps: []authoring.StepID{authoring.StepID(id)}}}
	}
	wait := func(reason string) { next = NextResult{Kind: KindWait, Wait: &WaitPayload{Reason: WaitReason(reason)}} }
	review := func(action, status string, key runtime.TaskKey) {
		step = action
		switch status {
		case "OPERATOR":
			next = NextResult{Kind: KindOperator, Operator: &OperatorPayload{Facts: []Fact{}}}
		case "FINDINGS", "RETRY_EXHAUSTED", "ROUND_EXHAUSTED":
			ask(action + "." + status)
		case "RUNNING":
			wait("TASKS_IN_FLIGHT")
		default:
			next = NextResult{Kind: KindReady, Ready: &ReadyPayload{Tasks: []ReadyTask{{Task: key, Step: authoring.StepID(action)}}}}
		}
	}
	switch {
	case v.Terminal:
		next = NextResult{Kind: KindComplete, Complete: &CompletePayload{}}
	case len(v.HostSteps) > 0:
		next = NextResult{Kind: KindHostAction, HostAction: &HostActionPayload{Steps: v.HostSteps}}
	case v.Control != "":
		ask(v.Control)
	case !v.Prechecked:
		step = "intake.precheck"
		wait("ENGINE_INTERNAL")
	case !v.Confirmed:
		ask("intake.confirm")
	case v.Mode == "":
		ask("intake.mode")
	case v.Mode == "lightweight":
		next = NextResult{Kind: KindComplete, Complete: &CompletePayload{}}
	case v.GuardReason == "COST_PENDING":
		wait("COST_PENDING")
	case v.GuardReason != "":
		ask(v.GuardReason)
	case v.ProductStatus != "PASS" && v.ProductStatus != "SKIPPED":
		review("product-review", v.ProductStatus, v.ProductTask)
	case v.TechnicalStatus != "PASS" && v.TechnicalStatus != "SKIPPED":
		review("start-readiness", v.TechnicalStatus, v.TechnicalTask)
	case !v.TopologyConfirmed:
		ask("topology.confirm")
	case !v.RouteConfirmed:
		ask("route.confirm")
	case v.PreparationAsk != "":
		ask(v.PreparationAsk)
	case v.VerificationOperator:
		next = NextResult{Kind: KindOperator, Operator: &OperatorPayload{Facts: []Fact{}}}
	case v.ReviewReady:
		ask("review.ready")
	case len(v.PreparationTasks) > 0:
		next = NextResult{Kind: KindReady, Ready: &ReadyPayload{Tasks: v.PreparationTasks}}
	case v.PreparationWait != "":
		wait(v.PreparationWait)
	default:
		ask("development.ready")
	}
	var frontier []FrontierEntry
	if step != "" {
		for _, cs := range cd.Steps {
			if string(cs.Header.ID) == step {
				frontier = append(frontier, FrontierEntry{Step: cs.Header.ID, Node: cs.Header.NodeID, Kind: cs.Header.Kind, Ordinal: cs.Header.Ordinal})
			}
		}
	}
	if next.Kind == KindReady && len(v.PreparationTasks) > 0 {
		for _, task := range v.PreparationTasks {
			for _, cs := range cd.Steps {
				if cs.Header.ID == task.Step {
					frontier = append(frontier, FrontierEntry{Step: cs.Header.ID, Node: cs.Header.NodeID, Kind: cs.Header.Kind, Ordinal: cs.Header.Ordinal})
				}
			}
		}
	}
	if next.Kind == KindReady && len(frontier) != len(next.Ready.Tasks) {
		return nil, fmt.Errorf("regular: selected step %q is not in the bound definition", step)
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		return nil, err
	}
	sd, err := s.Digest()
	if err != nil {
		return nil, err
	}
	od, err := obs.Digest()
	if err != nil {
		return nil, err
	}
	return &Plan{DefinitionDigest: encoder.Digest(data), StateDigest: sd, ObservationDigest: od, Frontier: frontier, Next: next}, nil
}
