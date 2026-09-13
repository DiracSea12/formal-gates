package facade

import (
	"formal-gates/internal/engine/protocol"
	"path/filepath"
	"reflect"
)

type nativePromptMaterial struct {
	initialMessage     string
	resumeInstructions string
	instructions       string
	input              map[string]any
	workspace          string
	files              map[string][]byte
}

// The role payload and mandatory instructions are prepared once, before CLAIM.
// Bind the final operation after native recovery (which can authorize replace).
func (m nativePromptMaterial) render(claim protocol.ExecutionClaim) (string, error) {
	m.input["binding"] = claim
	instructions := m.instructions
	if claim.Operation == "resume" && claim.Identity != "" && m.initialMessage != "" {
		instructions = m.resumeInstructions
		m.input["previousMessage"] = m.initialMessage
	}
	return assemblePrompt(instructions, m.input)
}

type nativeActionInput struct {
	prompt          string
	promptClaim     protocol.ExecutionClaim
	material        nativePromptMaterial
	schema          map[string]any
	sources         []SubagentSource
	toolDescription string
	coverage        *nativeCoverageInput
}

// This is shared by every native platform. It performs all deterministic input
// checks without consuming an attempt, creating a job, or invoking an adapter.
func (f *Facade) prepareNativeActionInput(run Run, claim protocol.ExecutionClaim, execution protocol.RegularExecution, job string) (nativeActionInput, error) {
	var p nativeActionInput
	var err error
	if !execution.Claimed {
		p.sources, err = f.planActionSources(run, claim, nil)
	} else {
		p.sources, err = f.capturedActionSources(claim.ActionID)
	}
	if err != nil {
		return p, err
	}
	if err = validateCapturedSources(p.sources, string(claim.Task.Step)); err != nil {
		return p, err
	}
	switch {
	case preparationAction(string(claim.Task.Step)):
		p.material, err = preparePreparationPrompt(run, claim)
		p.schema = preparationSchema(string(claim.Task.Step), run, claim)
	case verificationAction(string(claim.Task.Step)):
		p.material, err = prepareVerificationPrompt(run, claim)
		p.schema = verificationSchema(claim)
	default:
		p.material, err = prepareReviewPrompt(run, claim)
		p.material.workspace = job
		p.schema = reviewSchema(claim)
	}
	if err != nil {
		return p, err
	}
	p.toolDescription, err = nativeOperationInstructions(run, string(claim.Task.Step))
	if err != nil {
		return p, err
	}
	p.coverage, err = prepareNativeCoverage(run, claim, p.material.workspace, p.sources, job)
	if err != nil {
		return p, err
	}
	if claim.Operation == "resume" && checkpointCanResume(run, execution) {
		p.material.bindVerificationResumeFiles(execution.ResumeCount + 1)
	}
	if err = p.material.bindFiles(job, run.Regular.Requirement); err != nil {
		return p, err
	}
	p.material.bindPublicSources(job, p.sources)
	if execution.Claimed {
		p.material.initialMessage = filepath.Join(job, "prompt.txt")
	} else if execution.PreviousAction != "" {
		p.material.initialMessage = filepath.Join(f.codexJob(execution.PreviousAction), "prompt.txt")
	}

	if p.material.initialMessage != "" {
		p.material.resumeInstructions, err = actionInstructions(run, "actions/development-worker.md#续做约定")
		if err != nil {
			return p, err
		}
	}

	return p, nil
}

// Protocol limits belong to adapters; shared planning invokes the same check
// before CLAIM that the transport invokes before sending.
func validateNativeInput(adapter nativeExecutionTransport, prompt string) error {
	if bounded, ok := adapter.(interface{ validatePrompt(string) error }); ok {
		return bounded.validatePrompt(prompt)
	}
	return nil
}

func (p *nativeActionInput) preflight(claim protocol.ExecutionClaim, adapter nativeExecutionTransport) error {
	var err error
	p.prompt, err = p.material.render(claim)
	if err != nil {
		return err
	}
	p.promptClaim = claim
	if err = validateNativeInput(adapter, p.prompt); err != nil {
		return err
	}
	if p.coverage != nil {
		if err := validateNativeInput(adapter, p.coverage.prompt); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (p *nativeActionInput) render(claim protocol.ExecutionClaim) (string, error) {
	if reflect.DeepEqual(claim, p.promptClaim) {
		return p.prompt, nil
	}
	// Only recovery-authorized binding changes need a new serialization.
	return p.material.render(claim)
}
