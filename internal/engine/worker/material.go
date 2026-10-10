// Package worker renders issued engine inputs and decodes worker evidence.
// It does not schedule work, record completion or decide user/Operator choices.
package worker

import (
	"encoding/json"
	"fmt"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"slices"
	"strings"
)

type Material struct {
	Input        protocol.WorkerInput `json:"input"`
	ReadOnly     bool                 `json:"readOnly"`
	Prompt       string               `json:"prompt"`
	ResultSchema map[string]any       `json:"resultSchema"`
}

func Render(input protocol.WorkerInput, catalog prompts.PromptCatalog) (Material, error) {
	if input.Pending.ActionID == "" || input.Pending.AttemptID == "" || input.Provider == "" || input.RequirementDocument.Content == "" {
		return Material{}, fmt.Errorf("worker material requires issued identity and confirmed requirement context")
	}
	if input.Review != nil && input.Review.Action == review.StartReadiness || input.Handler == "engine.repair" {
		input.AvailableRouteCandidates = catalog.RouteCandidates()
		input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
	}
	value := Material{Input: input, ReadOnly: true}
	instructions := ""
	if input.Review != nil {
		if input.Review.Action == review.ProductReview {
			instructions = "Report actual document defects, not known baseline gaps explicitly assigned to authorized repair."
		} else if input.Review.Action == review.StartReadiness {
			instructions = "granularityReview omits IDs. Dependencies/checkpointGroups select 1-based batch numbers; owner is 0 for master merge or a positive slice number."
		} else {
			return Material{}, fmt.Errorf("unsupported review role %s", input.Review.Action)
		}
		if input.Review.Action == review.StartReadiness {
			instructions += " FINAL omits checkpoints; CHECKPOINT groups cover every batch position once in dependency order. Checks declare their approved FULL_BUILD/FULL_TESTS meaning."
		}
		instructions += " verification follows the issued check order with non-empty actual evidence; do not copy keys. Findings carry independent evidence and severity rationale."

	} else if input.Validation != nil {
		switch input.Handler {
		case "engine.development.batch":
			instructions = "Report summary, ordered DoD evidence and handoff for input.validation.development.batch. If work remains, return summary and actual remainingWork instead of completion. Use development.result and exact failed-check evidence as unfinished confirmed obligations."
			value.ReadOnly = false
		case "engine.repair.verify":
			instructions = "Report only each comparison and honest EXECUTED_PASS/EXECUTED_FAIL against the approved affected case oracle and issued native facts."
			value.ReadOnly = true
		case "engine.repair":
			instructions = "completion is only for an issued development batch or a proposed single batch in granularityReview. Without either, omit completion and report summary. If work remains, return summary and actual remainingWork instead of completion or granularityReview. Use repair.result as unfinished confirmed obligations."
			if len(repairCaseIDs(input.Validation)) > 0 {
				instructions += " affectedCases selects the affected cases from contract manifests in their original order, with duplicates removed; use 1-based positions."
			}
			value.ReadOnly = false
		case "engine.merge.resolve":
			instructions = "Report only the semantic resolution summary. The program supplies the exact original conflict paths."
			value.ReadOnly = false
		case "engine.qa.design":
			instructions = "Return coverage in issued point order, selecting 1-based case positions: retained cases first, then new cases. [] clears relations; null preserves original relations for remaining cases. retainCases/retainSharedSteps select original list positions; cases/sharedSteps contain only new authored entries, and absent entries are deleted. Case sharedSteps selects original shared instructions followed by new ones. BLACKBOX/MERGE require publicEntry/preconditions/steps and omit testRef; WHITEBOX requires a unique testRef and associates their test material using case files or referenced shared-step files, omitting public-entry fields."
			if len(input.Validation.AffectedCases) > 0 {
				instructions = "Return one change per affectedCases item in its input order, containing only changed semantic fields. revisionInstruction supplies the original accepted reason/category; older suggestions cannot replace it. Optional sharedSteps changes follow the original referenced instructions in manifest order; omit when unchanged. The program preserves IDs, untouched content and original coverage."
			}
			value.ReadOnly = input.Validation.Manifest.Binding.ReviewKind != coverage.ReviewWhitebox
		case "engine.qa.review.rationale":
			instructions = "Return real status and reason in issued source/point order and non-inherited case/shared order, without inherited placeholders. Suggestions contain only P2/P3, message and reason."
		case "engine.qa.execution":
			instructions = "Compare the issued complete native execution facts with each approved case oracle. Return records in issued pending required fact order, with comparison and EXECUTED_PASS/EXECUTED_FAIL, or null for unfinished comparison. The program derives checkpoint and overall status. Completed FAIL judgments are complete; when no new judgments remain, return records []. Missing native facts never establish PASS."
			value.ReadOnly = true
		case "engine.validation.observation":
			instructions = "Return findings/scopes/gateImpacts/suggestions arrays exactly their issued input order; preserve real P0/P1. sameAs selects input.validation.observation.problems first, then findings, numbered continuously from 1. A selected finding must be another pending item judged NEW; existing problem history is context. Scope cases select approvedCases; the program includes mustExecute. Accepted suggestion impact selects manifest case numbers, otherwise affectedCases is empty."
		case "engine.gate":
			instructions = "Report concrete findings with independent evidence, normal-use reproduction and severity rationale."
		default:
			return Material{}, fmt.Errorf("unsupported worker role %s", input.Handler)
		}
	} else {
		return Material{}, fmt.Errorf("worker material has no typed role input")
	}
	if !value.ReadOnly && input.Handler != "engine.qa.execution" && input.Handler != "engine.qa.design" && input.Handler != "engine.merge.resolve" {
		instructions += " Deliver only actual authorized artifacts; the program captures registered VCS differences. Temporary build and test outputs are not delivery artifacts; remove your own temporary validation outputs from the workspace before reporting completion, or produce them outside the workspace."
	}
	if input.Handler == "engine.merge.resolve" {
		instructions += " Remove your temporary build and test outputs before reporting completion, or produce them outside the workspace."
	}
	if input.Handler == "engine.qa.design" && !value.ReadOnly {
		instructions += " Associate only actual test material files with cases or their referenced shared steps. Remove temporary build and test outputs from the workspace before reporting completion, or produce them outside the workspace. Do not return a separate development path declaration."
	}
	if completionBatch(input) != nil {
		instructions += " completion.dod is an ordered array matching the issued batch criteria, each with non-empty semantic evidence; do not copy IDs. Approved command execution and its evidence belong to the program."
	}
	schema, err := ResultSchema(input)
	if err != nil {
		return Material{}, err
	}
	value.ResultSchema = schema
	actionID := ""
	switch input.Handler {
	case "engine.review.worker":
		actionID = "product-review"
	case "engine.readiness.plan":
		actionID = "start-readiness"
	case "engine.development.batch", "engine.repair", "engine.repair.verify", "engine.merge.resolve":
		actionID = "development-worker"
	case "engine.qa.design":
		actionID = "qa-design"
	case "engine.qa.review.rationale":
		actionID = "qa-review"
	case "engine.qa.execution":
		actionID = "qa-execution"
	case "engine.validation.observation":
		actionID = "validation-observation"
	}
	blocks := []string{}
	if value.ReadOnly {
		blocks = append(blocks, "[Shared reviewer contract]\n"+catalog.Base)
	}
	if input.Handler == "engine.gate" {
		if input.Validation.Gate == nil {
			return Material{}, fmt.Errorf("gate material lacks exact gate duty")
		}
		gate, ok := catalog.Gate(input.Validation.Gate.Gate)
		if !ok {
			return Material{}, fmt.Errorf("gate %s has no bound prompt", input.Validation.Gate.Gate)
		}
		blocks = append(blocks, "[Gate]\n"+gate.Content)
	} else {
		action, ok := catalog.Action(actionID)
		if !ok {
			return Material{}, fmt.Errorf("worker action %s has no bound prompt", actionID)
		}
		blocks = append(blocks, "[Action]\n"+action.Content)
	}
	document := input.RequirementDocument
	document.Content = ""
	contextValue := map[string]any{"requirementDocument": document}
	if input.Scope != nil {
		contextValue["scope"] = input.Scope
		instructions += " scope supplies the confirmed responsibility of this child. Judge its assigned obligations and regressions caused by its changes; another owner's unfinished work is not a defect of this child."
	}
	planRole := input.Review != nil && input.Review.Action == review.StartReadiness || input.Handler == "engine.repair" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Development == nil
	if planRole {
		contextValue["availableRouteCandidates"] = input.AvailableRouteCandidates
		if len(input.ApprovedChecks) > 0 {
			contextValue["approvedChecks"] = input.ApprovedChecks
		}
	}
	if input.Validation != nil {
		raw, err := json.Marshal(input.Validation)
		if err != nil {
			return Material{}, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return Material{}, err
		}
		var names []string
		switch input.Handler {
		case "engine.development.batch":
			names = []string{"development"}
		case "engine.repair":
			names = []string{"repair", "development", "contract"}
		case "engine.merge.resolve":
			names = []string{"conflict"}
		case "engine.qa.design":
			names = []string{"manifest", "contract", "sourcePoints", "revisionInstruction", "acceptedSuggestions", "affectedCases"}
		case "engine.qa.review.rationale":
			names = []string{"manifest", "contract", "sourcePoints", "revisionInstruction", "acceptedSuggestions", "affectedCases", "reviewScope", "reviewBase", "reviewArtifact", "inheritableReviews", "inheritableSharedReviews"}
		case "engine.qa.execution":
			names = []string{"manifest", "executionFacts", "requiredCases", "pendingCases", "completed", "inherited"}
		case "engine.repair.verify":
			names = []string{"repair", "contract", "executionFacts"}
		case "engine.validation.observation":
			names = []string{"observation"}
		case "engine.gate":
			names = []string{"gate"}
		}
		validation := map[string]json.RawMessage{}
		for _, name := range append(names, "interruption") {
			if field, ok := fields[name]; ok {
				validation[name] = field
			}
		}
		if input.Validation.Workspace != nil {
			validation["workspace"], err = json.Marshal(map[string]string{"path": input.Validation.Workspace.Path})
			if err != nil {
				return Material{}, err
			}
		}
		contextValue["validation"] = validation
	}

	context, err := json.MarshalIndent(contextValue, "", "  ")
	if err != nil {
		return Material{}, err
	}
	blocks = append(blocks, "[Current requirement]\n"+input.RequirementDocument.Content, "[Action input]\n"+string(context), fmt.Sprintf("[Dispatch]\nrole=%s vcs=%s readOnly=%t", input.Handler, input.VCS, value.ReadOnly))

	if planRole {
		instructions += " Proposal artifacts select original read/listing entries; newArtifacts names only new authored files. A completed single-batch repair omits both artifact fields; the program supplies its native paths. When approvedChecks is present, its FULL checks are supplied automatically; optional checks reuse a number or author a new local command."
		instructions += " Available route choices are declared in Action input; select by 1-based number, and the program supplies the full gate catalog and actual IDs; per-batch routes, when proposed, use full/custom ordinary gates (not QA modes or reserved merge gates), with a non-empty selected subset for custom; omit unneeded batch routes."
	}
	blocks = append(blocks, "[Result contract]\n"+instructions+"\nReturn exactly one JSON object matching the supplied semantic schema, using only its direct body or allowed inability/continuation alternative. inability uses an actual non-empty interruptionReason. Do not return status, paths, transport outcome or role wrappers. Issued identities, bindings, native facts and inherited content are assembled by the program. Preserve honest FAIL judgments and actual semantic evidence. Do not execute any VCS write operation; all VCS writes belong to engine adapters. Read-only VCS inspection remains permitted.\n")
	value.Prompt = strings.Join(blocks, "\n\n") + "\n"
	return value, nil
}
