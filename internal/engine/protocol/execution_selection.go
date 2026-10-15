package protocol

import (
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/host"
)

// ExecutionChoice preserves native model and effort values. Empty values leave
// the corresponding native default in force.
type ExecutionChoice struct {
	Provider        string `json:"provider"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// ExecutionSelection is a user choice for one selectable execution group.
// Decisions owns future choices; Attempt bindings own each issued execution.
type ExecutionSelection struct {
	Node   string          `json:"node"`
	Choice ExecutionChoice `json:"choice"`
}

type ExecutionSelectionDecision struct {
	Digest string `json:"digest"`
}

func ExecutionNodeIDs() []string {
	nodes := definition.ExecutionNodes()
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func executionSelectionApplies(selected, node string) bool {
	return selected == node || selected == "qa" && (node == "qa-blackbox" || node == "qa-whitebox")
}

func (s ExecutionSelection) CanonicalDigest() string { return digestOfCanonical(s) }

func (s ExecutionSelection) validate() error {
	// Saved shared-QA events retain their schema for exact replay.
	if s.Node != "qa" && !slices.Contains(ExecutionNodeIDs(), s.Node) {
		return validationError("execution selection requires an existing execution node")
	}
	adapter, err := host.Lookup(s.Choice.Provider)
	if err != nil || adapter.ID != s.Choice.Provider {
		return validationError("execution selection requires a supported canonical provider")
	}
	if s.Choice.Model != "" && strings.TrimSpace(s.Choice.Model) == "" || s.Choice.ReasoningEffort != "" && strings.TrimSpace(s.Choice.ReasoningEffort) == "" {
		return validationError("native model and effort must be nonempty values or omitted")
	}
	return nil
}

// EffectiveExecutionChoice reads the latest accepted choice for this run. Child
// inheritance is a creation snapshot, never an ongoing parent lookup.
func (s *State) EffectiveExecutionChoice(node string) ExecutionChoice {
	choice := ExecutionChoice{Provider: s.RunProvider}
	if s.Settings != nil {
		choice.Model, choice.ReasoningEffort = s.Settings.Values.Model, s.Settings.Values.ReasoningEffort
	}
	if node == "" {
		return choice
	}
	if s.SliceChild != nil {
		for _, inherited := range s.SliceChild.InheritedExecution {
			if inherited.Node == node {
				choice = inherited.Choice
				break
			}
			if executionSelectionApplies(inherited.Node, node) {
				choice = inherited.Choice
			}
		}
	}
	var revision uint64
	for _, decision := range s.Decisions {
		if decision.Control == ControlSelectExecution && decision.Choice == "approve" && decision.Execution != nil && executionSelectionApplies(decision.Execution.Node, node) && decision.Revision > revision {
			choice, revision = decision.Execution.Choice, decision.Revision
		}
	}
	return choice
}

func (s *State) ExecutionSelections() []ExecutionSelection {
	selections := make([]ExecutionSelection, 0, len(ExecutionNodeIDs()))
	for _, node := range ExecutionNodeIDs() {
		selections = append(selections, ExecutionSelection{Node: node, Choice: s.EffectiveExecutionChoice(node)})
	}
	// The shared QA snapshot carries merge QA's effective execution.
	return append(selections, ExecutionSelection{Node: "qa", Choice: s.EffectiveExecutionChoice("qa")})
}

// ExecutionNode uses the main-flow catalogue. Auxiliary responsibilities retain
// the frozen run defaults without creating another selectable node.
func (s *State) ExecutionNode(task runtime.TaskKey, cd *compiler.CompiledDefinition) string {
	for _, step := range cd.Steps {
		if step.Header.ID == task.Step {
			node := flowStepNode(step)
			if node == "qa" {
				switch s.validationMode(task) {
				case coverage.ReviewBlackbox:
					return "qa-blackbox"
				case coverage.ReviewWhitebox:
					return "qa-whitebox"
				}
			}
			return node
		}
	}
	return ""
}

func (s *State) executionChoice(b AttemptBindings) ExecutionChoice {
	choice := s.EffectiveExecutionChoice("")
	choice.Provider = b.Responsibility
	if b.Model != nil {
		choice.Model = *b.Model
	}
	if b.ReasoningEffort != nil {
		choice.ReasoningEffort = *b.ReasoningEffort
	}
	return choice
}

// ActionExecutionChoice finds original execution facts, including retired work.
// It does not resolve a current stage choice for a previously issued action.
func (s *State) ActionExecutionChoice(actionID string) (ExecutionChoice, bool) {
	if _, attempt, current := s.workerActionTarget(actionID); current {
		return s.executionChoice(attempt.Bindings), true
	}
	for _, ended := range s.EndedAttempts {
		if ended.ActionID == actionID {
			return s.executionChoice(ended.Bindings), true
		}
	}
	if obsolete, ok := s.ObsoleteActions[actionID]; ok {
		return s.executionChoice(obsolete.Bindings), true
	}
	if receipt, ok := s.SpawnReceipts[actionID]; ok {
		return ExecutionChoice{Provider: receipt.Provider}, true
	}
	return ExecutionChoice{}, false
}

func (s *State) actionProvider(actionID string) string {
	choice, known := s.ActionExecutionChoice(actionID)
	if known {
		return choice.Provider
	}
	return s.RunProvider
}

func (s *State) hostActionProvider(intent HostActionIntent) string {
	target := intent.Resume
	if target == nil {
		target = intent.Terminate
	}
	if target != nil {
		return s.actionProvider(target.WorkerActionID)
	}
	return s.RunProvider
}

func (s *State) checkHostActionProvider(actionID, provider string) error {
	if intent, known := s.hostIntent(actionID); known {
		return s.checkProvider(provider, s.hostActionProvider(intent))
	}
	if receipt, known := s.HostActionReceipts[actionID]; known {
		return s.checkProvider(provider, receipt.Provider)
	}
	return s.checkProvider(provider, s.RunProvider)
}

func (s *State) checkLifecycleProvider(provider string) error {
	if provider == s.RunProvider {
		return nil
	}
	for action := range s.PendingActions {
		if choice, known := s.ActionExecutionChoice(action); known && choice.Provider == provider {
			return nil
		}
	}
	for _, obsolete := range s.ObsoleteActions {
		if obsolete.Bindings.Responsibility == provider {
			return nil
		}
	}
	for _, ended := range s.EndedAttempts {
		if ended.Bindings.Responsibility == provider {
			return nil
		}
	}
	for _, receipt := range s.SpawnReceipts {
		if receipt.Provider == provider {
			return nil
		}
	}
	return s.checkProvider(provider, s.RunProvider)
}

func (e *Engine) acceptExecutionSelection(s *State, ask PendingAsk, decision *DecidePayload) error {
	if ask.Control != ControlSelectExecution {
		return validationError("this control does not accept an execution decision")
	}
	if decision.Choice == "decline" {
		if decision.Execution != nil {
			return validationError("declining an execution selection takes no payload")
		}
		return nil
	}
	if decision.Execution == nil || ask.Execution == nil || decision.Execution.Digest != ask.Execution.CanonicalDigest() {
		return validationError("execution decision differs from its original selection")
	}
	var unclaimed []PendingAction
	for id, pending := range s.PendingActions {
		if !executionSelectionApplies(ask.Execution.Node, s.ExecutionNode(pending.Task, e.cfg.Definition)) {
			continue
		}
		if _, known := s.SpawnReceipts[id]; known {
			continue
		}
		attempt := s.Attempts[pending.Task]
		if e.cfg.WorkerExecution != nil {
			claimed, err := e.cfg.WorkerExecution(pending, attempt)
			if err != nil {
				return err
			}
			if claimed != nil {
				continue
			}
		}
		unclaimed = append(unclaimed, pending)
	}
	for _, pending := range unclaimed {
		attempt := s.Attempts[pending.Task]
		choice := ask.Execution.Choice
		attempt.Bindings.Responsibility, attempt.Bindings.Model, attempt.Bindings.ReasoningEffort = choice.Provider, &choice.Model, &choice.ReasoningEffort
		s.Attempts[pending.Task] = attempt
	}
	return nil
}
