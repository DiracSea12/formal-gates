package worker

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestInterruptionPromptKeepsOnlySemanticFeedback(t *testing.T) {
	input := semanticInput()
	input.Pending.AttemptID = "current-attempt"
	input.RequirementDocument.Content = "Confirmed requirement"
	input.Validation.Interruption = &protocol.RecoveryRecord{ActionID: "private-action", AttemptID: "private-attempt", RequestID: "private-request", Revision: 93, Action: protocol.RecoveryAgent, Detail: "The current point has no boundary case", InterruptionReason: "The source tool exited before returning evidence"}
	before, _ := json.Marshal(input)
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(material.Prompt, input.Validation.Interruption.Detail) || !strings.Contains(material.Prompt, input.Validation.Interruption.InterruptionReason) {
		t.Fatal("semantic interruption evidence was omitted")
	}
	for _, identity := range []string{"private-action", "private-attempt", "private-request", `"revision": 93`, `"action": "AGENT"`} {
		if strings.Contains(material.Prompt, identity) {
			t.Fatal("prompt exposed recovery mechanics", identity)
		}
	}
	after, _ := json.Marshal(material.Input)
	if !bytes.Equal(before, after) {
		t.Fatal("prompt projection changed original input")
	}
}

func TestActionMaterialUsesSemanticResultFields(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.development.batch.v4", "engine.repair.v5", "engine.repair.verify.v2", "engine.qa.design.v5", "engine.readiness.plan.v4"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Pending.AttemptID = "attempt"
			if handler == "engine.repair.verify.v2" {
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "issued-candidate"}, VerificationCases: []string{"new"}}
			}
			if handler == "engine.readiness.plan.v4" {
				input.Validation = nil
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"报告实际修改的精确路径", "提交原 `DevelopmentEvidence`", "保留 affectedCases 及逐项 caseRuns", "返回语义字段（mode", "`test` =", "返回的 `manifest.sharedSteps`", "`granularity_review`"} {
				if strings.Contains(material.Prompt, forbidden) {
					t.Fatalf("action requests a removed result field: %s", forbidden)
				}
			}
			if !strings.Contains(material.Prompt, "Issued identities, bindings") || !strings.Contains(material.Prompt, "Do not return status, paths") {
				t.Fatal("action material lost program ownership of mechanical facts")
			}
		})
	}
}

func TestChildMaterialsCarryConfirmedResponsibilityWithoutNarrowingRequirement(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.development.batch.v4", "engine.gate.v2", "engine.validation.observation.v2", "engine.repair.v5", "engine.repair.verify.v2"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Complete confirmed requirement, including sibling and merge obligations."
			input.Scope = &protocol.WorkerScope{Owner: "slice-2", AcceptanceOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge"}}
			if handler == "engine.gate.v2" {
				input.Validation.Gate = &protocol.GateInput{Gate: "implementation-quality-gate"}
			}
			if handler == "engine.validation.observation.v2" {
				input.Validation.Observation = &protocol.ObservationInput{}
			}
			if handler == "engine.repair.v5" || handler == "engine.repair.verify.v2" {
				input.Validation.Repair = &protocol.RepairInput{}
			}
			if handler == "engine.repair.v5" {
				input.AvailableRouteCandidates = catalog.RouteCandidates()
				input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(material.Prompt, input.RequirementDocument.Content) || !strings.Contains(material.Prompt, "workspace requirement operation reads the complete original confirmed document") || !strings.Contains(material.Prompt, "another owner's unfinished work is not a defect of this child") {
				t.Fatal("child lost the whole requirement or its review boundary")
			}
			actionInput := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var projection protocol.WorkerInput
			if err := json.Unmarshal([]byte(actionInput), &projection); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(projection.Scope, input.Scope) || projection.RequirementDocument.Content != "" || projection.RequirementDocument.Path != input.RequirementDocument.Path || projection.RequirementDocument.ContentDigest != input.RequirementDocument.ContentDigest || !reflect.DeepEqual(material.Input, input) {
				t.Fatal("material changed responsibility facts or issued input")
			}
			stored, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(stored, &recovered); err != nil {
				t.Fatal(err)
			}
			replay, err := Render(recovered, catalog)
			if err != nil || !reflect.DeepEqual(material, replay) {
				t.Fatalf("recovery changed role materials: %v", err)
			}
		})
	}
}

func TestMaterialProvidesBoundRequirementReaderWithoutChangingIssuedInput(t *testing.T) {
	document := requirement.FrozenRequirementProjection{Path: "requirements.md", ContentDigest: "confirmed-digest", Content: "Complete confirmed requirement body.", Requirements: []requirement.FrozenRequirement{{ID: "REQ-001", Title: "behavior", Requirement: "required behavior"}}}
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.review.worker.v2", "engine.qa.design.v5"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: document}
			if handler == "engine.review.worker.v2" {
				input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
			} else {
				input.Validation = &protocol.ValidationInput{RequirementDocument: document}
			}
			original, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(material.Prompt, document.Content) || !strings.Contains(material.Prompt, "workspace requirement operation") {
				t.Fatal("initial message must retain the complete requirement reader and omit its repeated body")
			}
			actionInput := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var projection protocol.WorkerInput
			if err := json.Unmarshal([]byte(actionInput), &projection); err != nil {
				t.Fatal(err)
			}
			wantProjection := document
			wantProjection.Content = ""
			wantProjection.Requirements = nil
			if !reflect.DeepEqual(projection.RequirementDocument, wantProjection) || projection.Validation != nil && projection.Validation.RequirementDocument.Content != "" {
				t.Fatal("model action context must retain requirement identity and coverage without repeating its body")
			}
			for _, value := range []protocol.WorkerInput{input, material.Input} {
				body, err := json.Marshal(value)
				if err != nil || !bytes.Equal(body, original) {
					t.Fatalf("render changed original issued input: %s %v", body, err)
				}
			}
		})
	}
}

func TestQADesignMaterialSelectsOnlyCurrentModeRules(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		t.Run(string(mode), func(t *testing.T) {
			input := semanticInput()
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed scope"
			input.Validation.Manifest.Binding.ReviewKind = mode
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if mode != coverage.ReviewWhitebox {
				for _, absent := range []string{"test.read", "test.symbol", "白盒测试文件", "Omit public-entry fields"} {
					if strings.Contains(material.Prompt, absent) {
						t.Fatal("blackbox received unrelated whitebox instructions", absent)
					}
				}
				if !strings.Contains(material.Prompt, "Supply publicEntry/preconditions/steps") {
					t.Fatal("blackbox public operation duty was lost")
				}
			} else if !strings.Contains(material.Prompt, "test.read") || !strings.Contains(material.Prompt, "test.symbol") || !strings.Contains(material.Prompt, "Omit public-entry fields") {
				t.Fatal("whitebox original file/symbol selection duty was lost")
			}
		})
	}
}

func TestReadinessUsesOneOrdinaryGateChoiceOrder(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v4", Provider: "host", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}}, Gates: []prompts.PromptDefinition{{ID: "quality"}}}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
	var context protocol.WorkerInput
	if err := json.Unmarshal([]byte(raw), &context); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(context.AvailableRouteCandidates, material.Input.AvailableRouteCandidates) || !reflect.DeepEqual(context.AvailableRouteCandidates, []string{"quality"}) {
		t.Fatal("model choice order differs from original decoder input")
	}
}

func TestMaterialKeepsRoleEvidenceWithoutExposingOtherDuties(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.gate.v2", "engine.qa.execution.v3"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Content = "Confirmed semantic content"
			input.RunID, input.Provider = "program-only-run", "program-only-provider"
			input.Pending.ActionID, input.Pending.AttemptID = "program-only-action", "program-only-attempt"
			input.AvailableRouteCandidates = []string{"unrelated-route-choice"}
			v := input.Validation
			v.Workspace = &protocol.NativeResource{Path: "issued-workspace", Owner: "program-only-resource-owner", Ref: "program-only-ref", Status: "program-only-status"}
			v.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{Deliverable: "unrelated-development-duty"}}
			v.Gate = &protocol.GateInput{Gate: catalog.Gates[0].ID, BaseSnapshot: "issued-base", Candidate: coverage.ValidationCandidate{Identity: "issued-candidate"}}
			v.RequiredCases, v.PendingCases = []string{"new", "prior"}, []string{"prior", "new"}
			v.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "prior"}, {CaseID: "new"}}
			v.Manifest.Cases = []coverage.AcceptanceCase{{CaseID: "prior", Oracle: "prior oracle"}, {CaseID: "new", Oracle: "new oracle"}, {CaseID: "done", Oracle: "unrelated completed oracle"}}
			v.Completed = []coverage.ExecutionRecord{{CaseID: "done", Comparison: "accepted comparison", Result: coverage.ExecutedFail}}
			v.Inherited = []coverage.ExecutionRecord{{CaseID: "inherited", Comparison: "accepted inherited comparison", Result: coverage.ExecutedPass}}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"program-only-", "issued-workspace", "unrelated-development-duty", "unrelated-route-choice", "Available route choices", "Design retainCases"} {
				if strings.Contains(material.Prompt, forbidden) {
					t.Fatalf("role received unrelated control facts or duties: %s", forbidden)
				}
			}
			raw := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var context protocol.WorkerInput
			if err := json.Unmarshal([]byte(raw), &context); err != nil {
				t.Fatal(err)
			}
			if context.Validation.Workspace != nil || context.Validation.Binding.Task != (protocol.WorkBinding{}).Task {
				t.Fatal("role received a physical workspace or dispatch binding")
			}
			if handler == "engine.gate.v2" {
				if !reflect.DeepEqual(context.Validation.Gate, v.Gate) || len(context.Validation.Manifest.Cases) != 0 || len(context.Validation.ExecutionFacts) != 0 {
					t.Fatal("gate lost its native range or received QA content")
				}
			} else if context.Validation.Gate != nil || !reflect.DeepEqual(context.Validation.Manifest.Cases, v.Manifest.Cases[:2]) || !reflect.DeepEqual(context.Validation.ExecutionFacts, v.ExecutionFacts) || !reflect.DeepEqual(context.Validation.RequiredCases, v.RequiredCases) || !reflect.DeepEqual(context.Validation.PendingCases, v.PendingCases) || len(context.Validation.Completed) != 0 || len(context.Validation.Inherited) != 0 {
				t.Fatal("comparison lost pending oracle/original fact order, repeated retained results, or received a gate duty")
			}
			after, _ := json.Marshal(input)
			internal, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) || !bytes.Equal(before, internal) {
				t.Fatal("prompt projection changed program admission/recovery facts")
			}
		})
	}
}

func TestMaterialKeepsDevelopmentFeedbackWithoutTransportBindings(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	result := protocol.WorkerResult{SourceResult: "opaque-source", ActionID: "opaque-action", Provider: "opaque-provider", PayloadDigest: "opaque-payload", Digest: "opaque-digest", Outcome: "FAILED", InterruptionReason: "original execution interruption", InvalidReason: "original result rejection", Validation: &protocol.ValidationResult{Binding: protocol.WorkBinding{WaveID: "opaque-wave"}, Status: "CONTINUE", Development: &protocol.DevelopmentEvidence{Summary: "author text contains actionId and opaque-user-body", RemainingWork: "finish the original behavior", Paths: []string{"app.go"}, Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"criterion": "original evidence"}, Handoff: "original handoff"}}}}
	for _, handler := range []string{"engine.development.batch.v4", "engine.repair.v5"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Validation.Development = &protocol.DevelopmentInput{Result: &result, Dependencies: map[string]protocol.WorkerResult{"batch": result}}
			if handler == "engine.repair.v5" {
				input.Validation.Repair = &protocol.RepairInput{Result: &result, MemberResults: []protocol.WorkerResult{result}}
				input.AvailableRouteCandidates = catalog.RouteCandidates()
				input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
			}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, transport := range []string{"opaque-source", "opaque-action", "opaque-provider", "opaque-payload", "opaque-digest", "opaque-wave"} {
				if strings.Contains(material.Prompt, transport) {
					t.Fatalf("role prompt leaked result transport %s", transport)
				}
			}
			for _, feedback := range []string{"original execution interruption", "original result rejection", "FAILED", "CONTINUE", "author text contains actionId and opaque-user-body", "finish the original behavior", "app.go", "original evidence", "original handoff"} {
				if !strings.Contains(material.Prompt, feedback) {
					t.Fatalf("role prompt lost business feedback %s", feedback)
				}
			}
			after, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) {
				t.Fatal("prompt projection changed original issued API input")
			}
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(before, &recovered); err != nil {
				t.Fatal(err)
			}
			replayed, err := Render(recovered, catalog)
			if err != nil || !reflect.DeepEqual(material, replayed) {
				t.Fatalf("recovery changed role prompt or original input: %v", err)
			}
		})
	}
}

func TestMaterialRestrictsComparisonAndConflictResponsibilities(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.repair.verify.v2", "engine.merge.resolve.v3"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Validation.Development = nil
			if handler == "engine.repair.verify.v2" {
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "candidate"}, VerificationCases: []string{"case"}}
			} else {
				input.Validation.Conflict = &protocol.ConflictInput{}
			}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, instruction := range []string{"# 开发工作者", "需分块时提出完整", "逐项 DoD 证据与 handoff"} {
				if strings.Contains(material.Prompt, instruction) {
					t.Fatalf("role %s received development duties", handler)
				}
			}
			if handler == "engine.repair.verify.v2" && (!material.ReadOnly || !strings.Contains(material.Prompt, "# QA 判据比较")) {
				t.Fatal("repair comparison lost its existing comparison contract")
			}
			if handler == "engine.merge.resolve.v3" && (material.ReadOnly || !strings.Contains(material.Prompt, "Report only the semantic resolution summary")) {
				t.Fatal("conflict resolver lost its exact current duty")
			}
			after, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) {
				t.Fatal("role instructions changed original issued input")
			}
		})
	}
}

func TestSharedReviewerMaterialIncludesGateRulesOnlyForGate(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.review.worker.v2", "engine.readiness.plan.v4", "engine.qa.review.rationale.v2", "engine.qa.execution.v3", "engine.validation.observation.v2", "engine.gate.v2"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Content = "Confirmed complete requirement"
			if handler == "engine.review.worker.v2" || handler == "engine.readiness.plan.v4" {
				input.Validation = nil
				action := review.ProductReview
				if handler == "engine.readiness.plan.v4" {
					action = review.StartReadiness
				}
				input.Review = &protocol.WorkerReviewInput{Action: action}
			}
			if handler == "engine.validation.observation.v2" {
				input.Validation.Observation = &protocol.ObservationInput{}
			}
			if handler == "engine.gate.v2" {
				input.Validation.Gate = &protocol.GateInput{Gate: "implementation-quality-gate"}
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(material.Prompt, "## 门审材料") != (handler == "engine.gate.v2") || strings.Contains(material.Prompt, "独立 QA 的用例正文、执行证据及新编结构测试不作门判据") != (handler == "engine.gate.v2") {
				t.Fatal("gate-only contract was missing or leaked into another action", handler)
			}
			for _, shared := range []string{"## 范围与实证", "## 严重度与发现证据", "正常操作或常见失误复现", "不存在范围内 P0/P1 时结论为 PASS"} {
				if !strings.Contains(material.Prompt, shared) {
					t.Fatal("shared single-owner evidence rule was lost", shared)
				}
			}
			if strings.Contains(material.Prompt, "门审必填") || strings.Contains(material.Prompt, "产品审、技术审只审确认文档；QA") {
				t.Fatal("other-role instructions remained")
			}
		})
	}
}

func TestQAExecutionMaterialSelectsCurrentModeAndKeepsSharedEvidenceRules(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		t.Run(string(mode), func(t *testing.T) {
			input := semanticInput()
			input.Handler = "engine.qa.execution.v3"
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed scope"
			input.Validation.Manifest.Binding.ReviewKind = mode
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			current, other := "黑盒依据真实公开入口观察。", "白盒依据本轮指定结构测试。"
			if mode == coverage.ReviewWhitebox {
				current, other = other, current
			}
			if !strings.Contains(material.Prompt, current) || strings.Contains(material.Prompt, other) {
				t.Fatalf("execution mode duties differ from issued mode %s", mode)
			}
			for _, rule := range []string{
				"自测主张、模拟输出或代码检视不能代替执行事实。",
				"逐条保留真实缺陷；一个失败不覆盖其他完整结果，授权/skip 也不能改写为执行 PASS。",
				"读取原命令、输入、退出码、stdout/stderr 和证据引用",
				"普通操作非零退出可能是负例预期，零退出不等于 oracle 通过。",
				"无法完成说明原因，不把缺证据写成 PASS",
			} {
				if !strings.Contains(material.Prompt, rule) {
					t.Fatal("common execution rule lost", rule)
				}
			}
		})
	}
}

func TestQAReviewMaterialKeepsCommonPreparationAndCurrentModeDuties(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		t.Run(string(mode), func(t *testing.T) {
			input := semanticInput()
			input.Handler = "engine.qa.review.rationale.v2"
			input.RequirementDocument.Content = "Confirmed scope"
			input.Validation.Manifest.Binding.ReviewKind = mode
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, common := range []string{
				"逐条独立判断尚未批准的 case/shared-step",
				"测试准备可构造规则见 QA Design，构造正常前置不要求重跑前序流程。",
				"## 每条用例六项", "任一项不成立该 case FAIL",
				"逐验收点核正向、失败/不得误报、边界/中间态及相关正常回归",
				"共同说明只独立核一次，变化时核全部引用者",
				"QA 正常可达前置的构造按 QA Design",
			} {
				if !strings.Contains(material.Prompt, common) {
					t.Fatal("common review duty lost", common)
				}
			}
			black := []string{"黑盒在基线隔离区审当前需求与公开步骤", "黑盒不能用已有套件替代公开入口实跑。"}
			white := []string{"白盒读实际实现与候选测试。", "白盒先独立列行为、分支、oracle", "白盒逐项读 reviewBase、reviewArtifact 原差异及代码/配置", "声明为测试不等于归属证明"}
			current, other := black, white
			if mode == coverage.ReviewWhitebox {
				current, other = white, black
			}
			for _, rule := range current {
				if !strings.Contains(material.Prompt, rule) {
					t.Fatal("current mode duty lost", rule)
				}
			}
			for _, rule := range other {
				if strings.Contains(material.Prompt, rule) {
					t.Fatal("other mode duty leaked", rule)
				}
			}
			stored, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(stored, &recovered); err != nil {
				t.Fatal(err)
			}
			replay, err := Render(recovered, catalog)
			if err != nil || !reflect.DeepEqual(material, replay) {
				t.Fatalf("serialized recovery changed role material: %v", err)
			}
		})
	}
}

func TestRoleMaterialRejectsMissingSelectedRules(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	input := semanticInput()
	input.RequirementDocument.Content = "Confirmed scope"
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewBlackbox
	catalog.RoleRules = slices.DeleteFunc(catalog.RoleRules, func(rules prompts.PromptDefinition) bool { return rules.ID == "qa-design-blackbox" })
	if _, err := Render(input, catalog); err == nil || !strings.Contains(err.Error(), "qa-design-blackbox") {
		t.Fatalf("missing current role body was accepted: %v", err)
	}
}

func TestRepairComparisonMaterialPreservesBothApprovedModes(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	input := semanticInput()
	input.Handler = "engine.repair.verify.v2"
	input.RequirementDocument.Content = "Confirmed scope"
	input.Validation.Repair = &protocol.RepairInput{}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"黑盒依据真实公开入口观察。", "白盒依据本轮指定结构测试。", "自测主张、模拟输出或代码检视不能代替执行事实。"} {
		if !strings.Contains(material.Prompt, rule) {
			t.Fatal("approved affected-case comparison duty lost", rule)
		}
	}
}

func TestRepairComparisonReceivesOnlyIssuedCaseRules(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, cases := range [][]string{{"black"}, {"white"}, {"black", "white"}} {
		input := semanticInput()
		input.Handler = "engine.repair.verify.v2"
		input.RequirementDocument.Content = "Confirmed requirement"
		input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "issued-candidate"}, VerificationCases: cases}
		input.Validation.Contract.Manifests = []coverage.AcceptanceManifest{
			{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Cases: []coverage.AcceptanceCase{{CaseID: "black"}}},
			{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}, Cases: []coverage.AcceptanceCase{{CaseID: "white"}}},
		}
		material, err := Render(input, catalog)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"black", "white"} {
			rule, ok := catalog.RoleRule("qa-execution-" + mode + "box")
			if !ok {
				t.Fatal("missing rule", mode)
			}
			if strings.Contains(material.Prompt, rule.Content) != slices.Contains(cases, mode) {
				t.Fatalf("case duties %v received wrong %s rules", cases, mode)
			}
		}
	}
}
