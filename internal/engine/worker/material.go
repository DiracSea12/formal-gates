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
	if input.Review != nil && input.Review.Action == review.StartReadiness && !input.DevelopmentDisabled || input.Handler == "engine.development.plan.v1" || input.Handler == "engine.repair.v7" {
		input.AvailableRouteCandidates = catalog.RouteCandidates()
		input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
	}
	value := Material{Input: input, ReadOnly: true}
	instructions := ""
	if input.Review != nil {
		if input.Review.Action == review.ProductReview {
			instructions = "Report actual document defects, not known baseline gaps explicitly assigned to authorized repair."
		} else if input.Review.Action == review.StartReadiness {
			if !input.DevelopmentDisabled {
				instructions = "granularityReview omits IDs. Dependencies/checkpointGroups select 1-based batch numbers; owner is 0 for master merge or a positive slice number."
			} else {
				instructions = "Development is disabled for this run. Complete the technical review without a development proposal."
			}
		} else {
			return Material{}, fmt.Errorf("unsupported review role %s", input.Review.Action)
		}
		if input.Review.Action == review.StartReadiness {
			instructions += " Every finding requires a technical category from the schema; implementation-detail has severity P2 or P3."
			if !input.DevelopmentDisabled {
				instructions += " FINAL omits checkpoints; CHECKPOINT groups cover every batch position once in dependency order. Approved EDIT checks run after each workspace mutation; ordinary checks and FULL_BUILD/FULL_TESTS run at the batch boundary. When approvedPlan is issued, batch reuse selects its original batch position and only changed semantic fields are supplied; omitted original batch values are preserved."
			}
		}
		instructions += " Findings carry observed evidence and severity rationale. Independent Operator admission is a separate responsibility."

	} else if input.Validation != nil {
		switch input.Handler {
		case "engine.development.plan.v1":
			instructions = "Prepare the development execution proposal from the confirmed requirement and solution. Return only granularityReview, without review findings or verdict. It omits IDs; dependencies/checkpointGroups select 1-based batch numbers, owner is 0 for master merge or a positive slice number. FINAL omits checkpoints; CHECKPOINT groups cover every batch position once in dependency order. The proposal requires user confirmation before implementation."
		case "engine.development.batch.v6":
			instructions = "Report summary, ordered DoD evidence and handoff for input.validation.development.batch. Supply newly prepared command strings only for its missing verification goals in their issued order. Accepted calls are retained by the program. If work remains, return summary, actual remainingWork and any prepared commands instead of completion. Use development.result and exact failed-check evidence as unfinished confirmed obligations."
			value.ReadOnly = missingEditCalls(input)
			if value.ReadOnly {
				instructions = "Prepare the declared EDIT calls before changing production files. The workspace is read-only. Return summary, actual remainingWork and prepared command strings for the issued missing verification goals; the program retains these values and continues the same action. Do not claim implementation completion."
			}
		case "engine.repair.verify.v2":
			instructions = "Report only each comparison and honest EXECUTED_PASS/EXECUTED_FAIL against the approved affected case oracle and issued native facts."
			value.ReadOnly = true
		case "engine.repair.v7":
			instructions = "completion is only for an issued development batch or a proposed single batch in granularityReview. Without either, omit completion and report summary. If work remains, return summary and actual remainingWork instead of completion or granularityReview. Use repair.result as unfinished confirmed obligations."
			if len(repairCaseIDs(input.Validation)) > 0 {
				instructions += " affectedCases selects the affected cases from contract manifests in their original order, with duplicates removed; use 1-based positions."
			}
			value.ReadOnly = false
		case "engine.merge.resolve.v3":
			instructions = "Report only the semantic resolution summary. The program supplies the exact original conflict paths."
			value.ReadOnly = false
		case "engine.qa.design.v6":
			instructions = "Return only the necessary case/shared-step increment. Omitted entries remain unchanged. case/step selects an existing entry by its 1-based original position; omit it for a new entry whose stable ID is supplied by the program. removeCases/removeSharedSteps explicitly select original positions to delete; replace explicitly replaces the whole set. Coverage follows issued point order and selects original cases followed by new cases; [] clears relations and null preserves them. Case sharedSteps selects original shared instructions followed by new ones."
			if len(input.Validation.AffectedCases) > 0 {
				instructions = "Return one change per affectedCases item in its input order, containing only changed semantic fields, or remove:true to explicitly remove that issued item. revisionInstruction supplies the original accepted reason/category; older suggestions cannot replace it. Optional sharedSteps changes follow the original referenced instructions in manifest order; omit when unchanged. The program preserves IDs, untouched content and original coverage."
			}
			if input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				instructions += " Associate test material through original file selections. Select Go tests with test.read and test.symbol from a published full read; the program includes their file. Other languages use approved operations and oracle for the actual test entry. Create new names in code, then read before selection. Omit public-entry fields."
			} else {
				instructions += " Supply publicEntry/preconditions/steps for the public behavior, without test file or symbol selections."
			}
			value.ReadOnly = input.Validation.Manifest.Binding.ReviewKind != coverage.ReviewWhitebox
		case "engine.qa.review.rationale.v2":
			instructions = "Return real status and reason in issued source/point order and non-inherited case/shared order, without inherited placeholders. Suggestions contain only P2/P3, message and reason."
		case "engine.qa.execution.v4":
			instructions = "Compare the issued complete native execution facts with each approved case oracle. Return records in issued pending required fact order, with comparison and EXECUTED_PASS/EXECUTED_FAIL, or null for unfinished comparison. The program derives checkpoint and overall status. Completed FAIL judgments are complete; when no new judgments remain, return records []. Missing native facts never establish PASS."
			if len(input.Validation.CommandCases) > 0 {
				instructions = "Return commands in commandCases/commandSources order, each an ordered nonempty ARGV/SHELL operation list. commandSources identifies the missing case invocation or shared preparation invocation at each position. Supply only these missing invocation values; preserve the approved shared procedures, case steps, oracle and all declared operations. The program supplies all identities, binds the candidate and workspace, executes the operations and collects real stdout/stderr/exit. Do not run shell yourself or judge PASS before the original facts return."
			}
			value.ReadOnly = true
		case "engine.validation.observation.v2":
			instructions = "Return findings/scopes/gateImpacts/suggestions arrays exactly their issued input order; preserve real P0/P1. sameAs selects input.validation.observation.problems first, then findings, numbered continuously from 1. A selected finding must be another pending item judged NEW; existing problem history is context. Scope cases select approvedCases; the program includes mustExecute. Accepted suggestion impact selects manifest case numbers, otherwise affectedCases is empty."
		case "engine.gate.v3":
			instructions = "Report concrete findings with independent evidence, normal-use reproduction and severity rationale."
		default:
			return Material{}, fmt.Errorf("unsupported worker role %s", input.Handler)
		}
	} else {
		return Material{}, fmt.Errorf("worker material has no typed role input")
	}
	if !value.ReadOnly && input.Handler != "engine.qa.execution.v4" && input.Handler != "engine.qa.design.v6" && input.Handler != "engine.merge.resolve.v3" {
		instructions += " Deliver only actual authorized artifacts; the program captures registered VCS differences. Temporary build and test outputs are not delivery artifacts; remove your own temporary validation outputs from the workspace before reporting completion, or produce them outside the workspace."
	}
	if input.Handler == "engine.merge.resolve.v3" {
		instructions += " Remove your temporary build and test outputs before reporting completion, or produce them outside the workspace."
	}
	if input.Handler == "engine.qa.design.v6" && !value.ReadOnly {
		instructions += " Associate only actual test material files with cases or their referenced shared steps. Remove temporary build and test outputs from the workspace before reporting completion, or produce them outside the workspace. Do not return a separate development path declaration."
	}
	if completionBatch(input) != nil && !value.ReadOnly {
		instructions += " completion.dod is an ordered array matching the issued batch criteria, each with non-empty semantic evidence; do not copy IDs. Approved command execution and its evidence belong to the program."
	}
	schema, err := ResultSchema(input)
	if err != nil {
		return Material{}, err
	}
	value.ResultSchema = schema
	actionID := ""
	switch input.Handler {
	case "engine.review.worker.v3":
		actionID = "product-review"
	case "engine.readiness.plan.v6":
		actionID = "start-readiness"
	case "engine.development.plan.v1":
		actionID = "development-plan"
	case "engine.development.batch.v6", "engine.repair.v7":
		actionID = "development-worker"
	case "engine.repair.verify.v2":
		actionID = "qa-execution"
	case "engine.qa.design.v6":
		actionID = "qa-design"
	case "engine.qa.review.rationale.v2":
		actionID = "qa-review"
	case "engine.qa.execution.v4":
		actionID = "qa-execution"
	case "engine.validation.observation.v2":
		actionID = "validation-observation"
	}
	blocks := []string{}
	if value.ReadOnly && input.Handler != "engine.development.plan.v1" {
		blocks = append(blocks, "[Shared reviewer contract]\n"+catalog.Base)
	}
	if input.Handler == "engine.gate.v3" {
		if input.Validation.Gate == nil {
			return Material{}, fmt.Errorf("gate material lacks exact gate duty")
		}
		gate, ok := catalog.Gate(input.Validation.Gate.Gate)
		if !ok {
			return Material{}, fmt.Errorf("gate %s has no bound prompt", input.Validation.Gate.Gate)
		}
		blocks = append(blocks, "[Gate]\n"+gate.Content)
	} else if actionID != "" {
		action, ok := catalog.Action(actionID)
		if !ok {
			return Material{}, fmt.Errorf("worker action %s has no bound prompt", actionID)
		}
		blocks = append(blocks, "[Action]\n"+action.Content)
		if input.Review != nil && input.Review.Action == review.StartReadiness && !input.DevelopmentDisabled {
			plan, ok := catalog.Action("development-plan")
			if !ok {
				return Material{}, fmt.Errorf("worker action development-plan has no bound prompt")
			}
			blocks = append(blocks, "[Development plan contract]\n"+plan.Content)
		}
	}
	var ruleIDs []string
	switch input.Handler {
	case "engine.gate.v3":
		ruleIDs = []string{"gate"}
	case "engine.repair.verify.v2":
		for _, caseID := range input.Validation.Repair.VerificationCases {
			for _, manifest := range input.Validation.Contract.Manifests {
				if !slices.ContainsFunc(manifest.Cases, func(item coverage.AcceptanceCase) bool { return item.CaseID == caseID }) {
					continue
				}
				mode := "blackbox"
				if manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
					mode = "whitebox"
				}
				id := "qa-execution-" + mode
				if !slices.Contains(ruleIDs, id) {
					ruleIDs = append(ruleIDs, id)
				}
			}
		}
	case "engine.qa.design.v6", "engine.qa.review.rationale.v2", "engine.qa.execution.v4":
		mode := "blackbox"
		if input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
			mode = "whitebox"
		}
		ruleIDs = []string{"qa", actionID + "-" + mode}
	}
	for _, id := range ruleIDs {
		rules, ok := catalog.RoleRule(id)
		if !ok {
			return Material{}, fmt.Errorf("worker role rules %s have no bound prompt", id)
		}
		blocks = append(blocks, "[Role rules]\n"+rules.Content)
	}
	document := input.RequirementDocument
	document.Content = ""
	document.Requirements = nil
	contextValue := map[string]any{"requirementDocument": document}
	if len(input.RequirementPoints) > 0 {
		contextValue["requirementPoints"] = input.RequirementPoints
		instructions += " requirementPoints supplies the complete confirmed parent requirement, point, oracle and source in issued order. Judge all assigned points; never copy their IDs, original text, revision or binding into the result. Point coverage supplements the original responsibilities: development retains correctness, readability, simplicity, coupling, maintainability, redundancy, overengineering, defensive programming, abstraction and hardcoding; each reviewer retains its own gate dimensions and findings outside individual points. It does not establish QA execution."
		if input.Review != nil && input.Review.Action == review.StartReadiness && !input.DevelopmentDisabled || input.Handler == "engine.development.plan.v1" {
			instructions += " Each proposed batch requirements selects assigned point positions from requirementPoints; the whole plan covers every issued point. Use the original batch responsibilities to explain the assignment."
		}
		if input.Handler == "engine.repair.v7" && completionBatch(input) == nil {
			instructions += " requirementPoints is the complete confirmed choice inventory for repair planning; repair.requiredPoints contains only actual duties bound to the original confirmed findings and execution facts. Complete a small repair directly with summary and ordered requirements evidence for repair.requiredPoints, without selecting points or creating a plan. A necessary plan selects only actually affected contributions from the confirmed inventory and completion supplies evidence for the proposed single batch's selected points; omit outer requirements. An issued batch uses only completion.requirements. A quality repair without requiredPoints does not claim requirement points completed. Original quality and readability obligations remain effective without reimplementing the whole project."
		}
		if completionBatch(input) != nil {
			instructions += " completion.requirements supplies actual evidence that this batch contribution is complete, in issued batch requirement-point order. DoD and subtasks define that contribution; another batch contribution to the same point can remain unfinished. Unfinished assigned work uses remainingWork."
		}
		if input.Handler == "engine.gate.v3" {
			instructions += " requirements gives one independent PASS/FAIL and evidence per issued point within this gate responsibility. A FAIL selects its original findings by 1-based positions; P2/P3 findings can accompany a PASS. Finding severity and the existing gate contract determine overall status; a point FAIL alone does not establish P0/P1."
			if len(input.Validation.Gate.Batches) > 0 {
				instructions += " gate.batches defines the confirmed completed contributions for this boundary. Judge each issued point against those batches' subtasks and DoD, without requiring unfinished contributions of other batches. Retain the gate's original quality checks and change regressions."
			}
		}
	}
	if input.Review != nil && len(input.Review.SettledFindings) > 0 {
		settled := make([]any, 0, len(input.Review.SettledFindings))
		for _, item := range input.Review.SettledFindings {
			finding := item.Finding
			settled = append(settled, map[string]any{
				"action": item.Action, "requirementRevision": item.RequirementRevision,
				"finding":  map[string]any{"id": finding.ID, "severity": finding.Severity, "severityRationale": finding.SeverityRationale, "category": finding.Category, "requirement": finding.Requirement, "normalPath": finding.NormalPath, "actualError": finding.ActualError, "deferralImpact": finding.DeferralImpact, "verification": finding.Verification, "message": finding.Message, "locations": finding.Locations},
				"decision": item.Decision, "effectiveSeverity": item.EffectiveSeverity, "reason": item.Reason,
			})
		}
		contextValue["settledFindings"] = settled
		instructions += " settledFindings supplies original facts and current user dispositions, not proof of completed repair. Review only your current duty; do not repeat a settled finding unchanged while its premise remains unchanged. If its premise actually changes, report the specific new basis. Do not return or regenerate the historical disposition list."
	}
	if len(input.Materials) > 0 {
		selected := make([]map[string]any, len(input.Materials))
		for i, material := range input.Materials {
			role := "reference"
			if material.Path == input.RequirementDocument.Path {
				role = "requirement"
			}
			if input.Intake != nil && (material.Path == input.Intake.SolutionSource || input.Intake.SolutionSource == "" && material.Path == input.Intake.RequirementSource) {
				if role == "requirement" {
					role = "requirement and solution"
				} else {
					role = "solution"
				}
			}
			selected[i] = map[string]any{"material": i + 1, "role": role}
		}
		contextValue["materials"] = selected
		instructions += " Read confirmed material texts with workspace requirement and its material number; omitting material selects the complete formal requirement. These frozen texts supply semantic context only, not physical file selections."
	}
	if input.Scope != nil {
		contextValue["scope"] = input.Scope
		instructions += " scope supplies the confirmed responsibility of this child. Judge its assigned obligations and regressions caused by its changes; another owner's unfinished work is not a defect of this child."
	}
	planRole := input.Review != nil && input.Review.Action == review.StartReadiness && !input.DevelopmentDisabled || input.Handler == "engine.development.plan.v1" || input.Handler == "engine.repair.v7" && input.Validation.Repair != nil && input.Validation.Repair.Plan == nil && input.Validation.Development == nil
	if planRole {
		if input.ApprovedPlan != nil {
			contextValue["approvedPlan"] = input.ApprovedPlan
			instructions += " approvedPlan publishes the complete original contract. Batch, subtask and DoD reuse select their one-based positions. Omitted plan fields and unmentioned batches retain original values; remove:true explicitly removes a selected original batch. New checkpointGroups select resulting batch positions."
		}
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
		case "engine.development.batch.v6":
			names = []string{"development"}
		case "engine.repair.v7":
			names = []string{"repair", "development", "contract"}
		case "engine.merge.resolve.v3":
			names = []string{"conflict"}
		case "engine.qa.design.v6":
			names = []string{"manifest", "contract", "sourcePoints", "revisionInstruction", "acceptedSuggestions", "affectedCases"}
		case "engine.qa.review.rationale.v2":
			names = []string{"manifest", "contract", "sourcePoints", "revisionInstruction", "acceptedSuggestions", "affectedCases", "reviewScope", "reviewBase", "reviewArtifact", "inheritableReviews", "inheritableSharedReviews"}
		case "engine.qa.execution.v4":
			names = []string{"manifest", "commandCases", "commandSources", "executionFacts", "requiredCases", "pendingCases"}
		case "engine.repair.verify.v2":
			names = []string{"repair", "contract", "executionFacts"}
		case "engine.validation.observation.v2":
			names = []string{"observation"}
		case "engine.gate.v3":
			names = []string{"gate"}
		}
		validation := map[string]json.RawMessage{}
		for _, name := range append(names, "interruption") {
			if field, ok := fields[name]; ok {
				validation[name] = field
			}
		}
		if input.Handler == "engine.qa.review.rationale.v2" {
			for _, field := range []struct {
				name    string
				reviews []coverage.ReviewDecision
			}{{"inheritableReviews", input.Validation.InheritableReviews}, {"inheritableSharedReviews", input.Validation.InheritableSharedReviews}} {
				if len(field.reviews) == 0 {
					continue
				}
				ids := make([]string, len(field.reviews))
				for i, item := range field.reviews {
					ids[i] = item.ID
				}
				validation[field.name], err = json.Marshal(ids)
				if err != nil {
					return Material{}, err
				}
			}
		}
		if recovery := input.Validation.Interruption; recovery != nil {
			validation["interruption"], err = json.Marshal(map[string]any{"class": recovery.Class, "interruptionReason": recovery.InterruptionReason, "detail": recovery.Detail})
			if err != nil {
				return Material{}, err
			}
		}
		if input.Validation.Development != nil && validation["development"] != nil {
			development := input.Validation.Development
			dependencies := make(map[string]any, len(development.Dependencies))
			for key, result := range development.Dependencies {
				dependencies[key], err = promptWorkerResult(&result)
				if err != nil {
					return Material{}, err
				}
			}
			result, err := promptWorkerResult(development.Result)
			if err != nil {
				return Material{}, err
			}
			validation["development"], err = json.Marshal(map[string]any{"batch": development.Batch, "dependencies": dependencies, "result": result, "checks": promptChecks(development.Checks)})
			if err != nil {
				return Material{}, err
			}
		}
		var currentCases []string
		if input.Handler == "engine.qa.execution.v4" {
			currentCases = executionCaseIDs(input.Validation)
			if len(input.Validation.CommandCases) > 0 {
				currentCases = input.Validation.CommandCases
				delete(validation, "executionFacts")
			}
			for _, field := range []struct {
				name string
				ids  []string
			}{{"requiredCases", input.Validation.RequiredCases}, {"pendingCases", input.Validation.PendingCases}} {
				ids := []string{}
				for _, id := range field.ids {
					if slices.Contains(currentCases, id) {
						ids = append(ids, id)
					}
				}
				validation[field.name], err = json.Marshal(ids)
				if err != nil {
					return Material{}, err
				}
			}
			validation["manifest"], err = json.Marshal(promptCaseManifest(input.Validation.Manifest, currentCases))
			if err != nil {
				return Material{}, err
			}
		} else if input.Handler == "engine.repair.verify.v2" {
			currentCases = input.Validation.Repair.VerificationCases
			manifests := []coverage.AcceptanceManifest{}
			for _, manifest := range input.Validation.Contract.Manifests {
				selected := promptCaseManifest(manifest, currentCases)
				if len(selected.Cases) > 0 {
					manifests = append(manifests, selected)
				}
			}
			validation["contract"], err = json.Marshal(map[string]any{"manifests": manifests})
			if err != nil {
				return Material{}, err
			}
			validation["repair"], err = json.Marshal(map[string]any{"verificationCases": currentCases})
			if err != nil {
				return Material{}, err
			}
		}
		if validation["executionFacts"] != nil {
			facts := make([]any, 0, len(input.Validation.ExecutionFacts))
			for _, fact := range input.Validation.ExecutionFacts {
				if !slices.Contains(currentCases, fact.CaseID) {
					continue
				}
				var operations []any
				for _, operation := range fact.Operations {
					qa := operation.Parameters.QA
					operations = append(operations, map[string]any{"source": qa.Source, "ordinal": qa.Ordinal, "operation": qa.Operation, "goTestRef": qa.GoTestRef, "directory": qa.Directory, "evidence": promptCheck(operation.Evidence)})
				}
				facts = append(facts, map[string]any{"caseId": fact.CaseID, "operations": operations})
			}
			validation["executionFacts"], err = json.Marshal(facts)
			if err != nil {
				return Material{}, err
			}
		}
		if input.Handler == "engine.repair.v7" && input.Validation.Repair != nil && validation["repair"] != nil {
			var repair map[string]json.RawMessage
			if err := json.Unmarshal(validation["repair"], &repair); err != nil {
				return Material{}, err
			}
			if repair["checks"] != nil {
				repair["checks"], err = json.Marshal(promptChecks(input.Validation.Repair.Checks))
				if err != nil {
					return Material{}, err
				}
			}
			if repair["result"] != nil {
				result, err := promptWorkerResult(input.Validation.Repair.Result)
				if err != nil {
					return Material{}, err
				}
				repair["result"], err = json.Marshal(result)
				if err != nil {
					return Material{}, err
				}
			}
			if repair["memberResults"] != nil {
				members := make([]any, 0, len(input.Validation.Repair.MemberResults))
				for _, member := range input.Validation.Repair.MemberResults {
					result, err := promptWorkerResult(&member)
					if err != nil {
						return Material{}, err
					}
					members = append(members, result)
				}
				repair["memberResults"], err = json.Marshal(members)
				if err != nil {
					return Material{}, err
				}
			}
			failures := make([]any, 0, len(input.Validation.Repair.ExecutionFailures))
			for _, failure := range input.Validation.Repair.ExecutionFailures {
				failures = append(failures, map[string]any{"findingId": failure.FindingID, "records": promptRecords(failure.Records, true)})
			}
			if repair["executionFailures"] != nil {
				repair["executionFailures"], err = json.Marshal(failures)
				if err != nil {
					return Material{}, err
				}
			}
			validation["repair"], err = json.Marshal(repair)
			if err != nil {
				return Material{}, err
			}
		}
		if validation["observation"] != nil {
			validation["observation"], err = promptObservation(validation["observation"])
			if err != nil {
				return Material{}, err
			}
		}
		if input.Validation.Workspace != nil {
			instructions += " The workspace tool is bound to this role's registered workspace; select files through its listings and reads."
		}
		contextValue["validation"] = validation
	}

	context, err := json.MarshalIndent(contextValue, "", "  ")
	if err != nil {
		return Material{}, err
	}
	blocks = append(blocks, "[Action input]\n"+string(context), fmt.Sprintf("[Dispatch]\nrole=%s vcs=%s readOnly=%t", input.Handler, input.VCS, value.ReadOnly))

	if planRole || input.Handler == "engine.development.batch.v6" || input.Handler == "engine.repair.v7" || input.Handler == "engine.qa.design.v6" {
		instructions += " Approved commands run in the registered workspace. Refer to workspace content by relative paths; SHELL may use \"$FORMAL_GATES_WORKSPACE\" for its root. The program supplies that variable; do not copy a physical directory into a command."
	}
	if planRole {
		instructions += " Proposal artifacts select original read/listing entries; newArtifacts names only new authored files."
		if proposesVerificationGoals(input) {
			instructions += " Verification calls reuse approved check numbers. Missing calls declare only goal and purpose for the developer to prepare. Declared EDIT calls are prepared before the first production edit. Do not author commands or select command text by read, line or column coordinates."
		} else {
			instructions += " A completed single-batch repair omits both artifact fields; the program supplies its native paths. When approvedChecks is present, its FULL and EDIT checks are supplied automatically; optional repair checks reuse a number or author a genuinely new local invocation."
		}
		instructions += " Available route choices are declared in Action input; select by 1-based number, and the program supplies the full gate catalog and actual IDs; per-batch routes, when proposed, explicitly select a non-empty subset of ordinary gates (not QA modes or reserved merge gates); omit unneeded batch routes."
	}
	instructions += " The workspace requirement operation reads the complete original confirmed document (optional from/to line range); its path and contentDigest are in Action input. Its published read number selects original value bytes without physical file identity. Keep the full requirement and acceptance scope; read the relevant original sections as your role requires."
	blocks = append(blocks, "[Result contract]\n"+instructions+"\nCall submit_semantics with the direct semantic body or allowed inability/continuation alternative from its declared tool schema. The program serializes the result. Finish with an ordinary completion message. inability uses an actual non-empty interruptionReason. Do not return status, paths, transport outcome or role wrappers. Issued identities, bindings, native facts and inherited content are assembled by the program. Preserve honest FAIL judgments and actual semantic evidence. Do not execute any VCS write operation; all VCS writes belong to engine adapters.\n")
	value.Prompt = strings.Join(blocks, "\n\n") + "\n"
	return value, nil
}

// promptCaseManifest supplies current case oracles and their referenced shared
// instructions. The complete coverage contract remains in Material.Input.
func promptCaseManifest(original coverage.AcceptanceManifest, ids []string) coverage.AcceptanceManifest {
	manifest := coverage.AcceptanceManifest{Binding: original.Binding}
	shared := map[string]bool{}
	for _, item := range original.Cases {
		if slices.Contains(ids, item.CaseID) {
			manifest.Cases = append(manifest.Cases, item)
			for _, id := range item.SharedSteps {
				shared[id] = true
			}
		}
	}
	for _, step := range original.SharedSteps {
		if shared[step.ID] {
			manifest.SharedSteps = append(manifest.SharedSteps, step)
		}
	}
	return manifest
}

func promptWorkerResult(result *protocol.WorkerResult) (any, error) {
	if result == nil {
		return nil, nil
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for _, field := range []string{"sourceResult", "actionId", "provider", "payloadDigest", "digest"} {
		delete(fields, field)
	}
	if result.Validation != nil {
		var validation map[string]json.RawMessage
		if err := json.Unmarshal(fields["validation"], &validation); err != nil {
			return nil, err
		}
		delete(validation, "binding")
		fields["validation"], err = json.Marshal(validation)
		if err != nil {
			return nil, err
		}
	}
	return fields, nil
}

// promptCheck keeps actual execution observations; request and receipt bindings
// remain in Material.Input for program admission and recovery.
func promptCheck(e protocol.CheckEvidence) map[string]any {
	return map[string]any{"exitCode": e.ExitCode, "error": e.Error, "stdout": e.Stdout, "stderr": e.Stderr, "stdoutText": e.StdoutText, "stderrText": e.StderrText, "tool": e.Tool, "environment": e.Environment, "invocation": e.Invocation, "goTestName": e.GoTestName, "goResult": e.GoResult}
}

func promptChecks(checks []protocol.CheckEvidence) []any {
	result := make([]any, 0, len(checks))
	for _, check := range checks {
		result = append(result, promptCheck(check))
	}
	return result
}

func promptRecords(records []coverage.ExecutionRecord, execution bool) []any {
	result := make([]any, 0, len(records))
	for _, record := range records {
		value := map[string]any{"reviewKind": record.ReviewKind, "sourceId": record.SourceID, "pointId": record.PointID, "caseId": record.CaseID, "result": record.Result, "comparison": record.Comparison}
		if execution {
			// ExecutionFact.Render owns this newline-separated JSON representation.
			// Plain semantic text is retained without interpreting its contents.
			for _, field := range []struct{ name, text string }{{"procedure", record.Procedure}, {"observation", record.Observation}} {
				rows := []any{}
				structured := field.text != ""
				for _, line := range strings.Split(field.text, "\n") {
					var original map[string]json.RawMessage
					if json.Unmarshal([]byte(line), &original) != nil || original == nil {
						structured = false
						break
					}
					if field.name == "procedure" && original["operation"] == nil || field.name == "observation" && (original["actionId"] == nil || original["parametersDigest"] == nil) {
						structured = false
						break
					}
					projected := map[string]json.RawMessage{}
					names := []string{"source", "ordinal", "operation", "goTestRef", "invocation"}
					if field.name == "observation" {
						names = []string{"exitCode", "error", "stdout", "stderr", "stdoutText", "stderrText", "tool", "environment", "invocation", "goTestName", "goResult"}
					}
					for _, name := range names {
						if original[name] != nil {
							projected[name] = original[name]
						}
					}
					rows = append(rows, projected)
				}
				if structured {
					value[field.name] = rows
				} else {
					value[field.name] = field.text
				}
			}
		}
		result = append(result, value)
	}
	return result
}

// promptExecutionContext projects the execution report held by a target or
// worker result. Their raw protocol values remain in Material.Input.
func promptExecutionContext(raw json.RawMessage, actual bool) (json.RawMessage, error) {
	var context map[string]json.RawMessage
	if err := json.Unmarshal(raw, &context); err != nil {
		return nil, err
	}
	if context["validation"] != nil && string(context["validation"]) != "null" {
		var err error
		context["validation"], err = promptExecutionContext(context["validation"], actual)
		if err != nil {
			return nil, err
		}
	}
	if context["execution"] != nil && string(context["execution"]) != "null" {
		var report map[string]json.RawMessage
		if err := json.Unmarshal(context["execution"], &report); err != nil {
			return nil, err
		}
		var records []coverage.ExecutionRecord
		if err := json.Unmarshal(report["records"], &records); err != nil {
			return nil, err
		}
		var err error
		report["records"], err = json.Marshal(promptRecords(records, actual))
		if err != nil {
			return nil, err
		}
		context["execution"], err = json.Marshal(report)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(context)
}

func promptObservation(raw json.RawMessage) (json.RawMessage, error) {
	var observation map[string]json.RawMessage
	if err := json.Unmarshal(raw, &observation); err != nil {
		return nil, err
	}
	for _, group := range []string{"findings", "problems", "scopes", "gateImpacts", "suggestions"} {
		if observation[group] == nil {
			continue
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(observation[group], &items); err != nil {
			return nil, err
		}
		for _, item := range items {
			var err error
			switch group {
			case "findings":
				err = promptFinding(item)
			case "problems":
				var findings []map[string]json.RawMessage
				if err = json.Unmarshal(item["findings"], &findings); err != nil {
					return nil, err
				}
				for _, finding := range findings {
					if err = promptFinding(finding); err != nil {
						return nil, err
					}
				}
				item["findings"], err = json.Marshal(findings)
			case "scopes":
				if item["baseline"] != nil {
					item["baseline"], err = promptExecutionContext(item["baseline"], false)
				}
			case "gateImpacts":
				item["evidence"], err = promptExecutionContext(item["evidence"], false)
			case "suggestions":
				delete(item, "workspace")
			}
			if err != nil {
				return nil, err
			}
		}
		var err error
		observation[group], err = json.Marshal(items)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(observation)
}

func promptFinding(item map[string]json.RawMessage) error {
	for _, field := range []string{"target", "evidence"} {
		var err error
		item[field], err = promptExecutionContext(item[field], true)
		if err != nil {
			return err
		}
	}
	if item["failedCases"] != nil {
		var records []coverage.ExecutionRecord
		if err := json.Unmarshal(item["failedCases"], &records); err != nil {
			return err
		}
		var err error
		item["failedCases"], err = json.Marshal(promptRecords(records, true))
		if err != nil {
			return err
		}
	}
	return nil
}
