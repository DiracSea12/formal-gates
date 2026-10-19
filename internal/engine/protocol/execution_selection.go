package protocol

import (
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

type ExecutionChoice = settings.ExecutionChoice

func ExecutionNodeIDs() []string {
	nodes := definition.ExecutionNodes()
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

// EffectiveExecutionChoice applies the latest applied configuration to the
// run defaults.
func (s *State) EffectiveExecutionChoice(node string) ExecutionChoice {
	choice := ExecutionChoice{Provider: s.RunProvider}
	if s.Settings != nil {
		choice.Model, choice.ReasoningEffort = s.Settings.Values.Model, s.Settings.Values.ReasoningEffort
	}
	if !slices.Contains(ExecutionNodeIDs(), node) {
		return choice
	}
	return s.AppliedConfiguration().EffectiveExecutionChoice(node, choice)
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
				case coverage.ReviewMerge:
					if slices.Contains(s.AppliedConfiguration().QAModes, "whitebox") {
						return "qa-whitebox"
					}
					return "qa-blackbox"
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
	if receipt, ok := s.SpawnReceipts[actionID]; ok && receipt.Execution != nil {
		return *receipt.Execution, true
	}
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

func (e *Engine) rebindUnclaimedExecution(s *State, node string, choice ExecutionChoice) error {
	var unclaimed []PendingAction
	for id, pending := range s.PendingActions {
		if s.ExecutionNode(pending.Task, e.cfg.Definition) != node {
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
		attempt.Bindings.Responsibility, attempt.Bindings.Model, attempt.Bindings.ReasoningEffort = choice.Provider, &choice.Model, &choice.ReasoningEffort
		s.Attempts[pending.Task] = attempt
	}
	return nil
}
