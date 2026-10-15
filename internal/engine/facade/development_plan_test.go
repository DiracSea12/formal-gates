package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/requirement"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func commandPlan() *protocol.GranularityReview {
	return &protocol.GranularityReview{Reason: "The command has one cohesive input-validation contract and no independent deliverables.", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{{ID: "command", Subtasks: []protocol.DevelopmentSubtask{{ID: "validate-input", Description: "Implement the public command input contract"}}, Deliverable: "Command accepts valid input and rejects invalid input", PublicEntry: "app run", Artifacts: []string{"app.go"}, Interfaces: "Existing command-line parameters", StateBoundary: "No persistent command state", Risk: "Invalid input must not be accepted", DoD: []protocol.DevelopmentCriterion{{ID: "input-contract", Description: "Valid and invalid inputs satisfy their specified outcomes"}}, Checks: []protocol.DevelopmentCheck{{ID: "project-build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "command-regression", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 -count=1 ./..."}}, Rollback: "Revert the command implementation", Recovery: "Re-run the command from a clean project", Handoff: "Record the implementation paths, build and regression outcomes", ContextCost: "One command and its public contract fit the worker context"}}}
}
func commandCompletion() *protocol.DevelopmentCompletion {
	return &protocol.DevelopmentCompletion{DoD: map[string]string{"input-contract": "Implemented valid and invalid public command outcomes"}, Handoff: "Implementation paths and build/test evidence are attached in this result"}
}

func splitCommandPlan(owners ...string) *protocol.GranularityReview {
	if len(owners) == 0 {
		owners = []string{"lane-core", "lane-tooling"}
	}
	plan := commandPlan()
	plan.Batches[0].Owner = owners[0]
	for _, owner := range append(owners[1:], "master-merge") {
		batch := commandPlan().Batches[0]
		batch.Owner, batch.ID = owner, owner
		batch.Subtasks = []protocol.DevelopmentSubtask{{ID: owner, Description: "Verify the documented command contract in this ownership scope"}}
		plan.Batches = append(plan.Batches, batch)
	}
	return plan
}
func (d *validationDriver) configureRoute(route *protocol.RouteDecision) {
	d.t.Helper()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	if route.Configuration == nil && snapshot.State.RouteConfigurationRequired() {
		configuration := snapshot.State.EffectiveConfiguration()
		configuration.SetSelectedGates(route.Selected)
		route.Configuration = &configuration
	}
}

func (d *validationDriver) confirmRoute(route protocol.RouteDecision) {
	d.t.Helper()
	d.configureRoute(&route)
	preview, err := d.f.DevelopmentPlan(route)
	if err != nil {
		d.t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	d.control(protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route})
}

func batchEvidence(input protocol.ValidationInput) *protocol.DevelopmentEvidence {
	b := input.Development.Batch
	c := &protocol.DevelopmentCompletion{DoD: map[string]string{}, Handoff: b.ID + ": completed implementation and verification, ready for its dependent batches"}
	for _, point := range b.RequirementPoints {
		c.RequirementResults = append(c.RequirementResults, requirement.PointResult{PointID: point, Status: "PASS", Evidence: b.ID + ": " + b.Deliverable + " implements " + point})
	}
	for _, d := range b.DoD {
		c.DoD[d.ID] = b.ID + ": " + d.Description + " verified"
	}
	return &protocol.DevelopmentEvidence{Completion: c, Paths: b.Artifacts, Summary: "implemented " + b.Deliverable}
}
func planBatchAction(d *validationDriver, batch string) (string, protocol.ValidationInput) {
	d.t.Helper()
	for id, input := range d.run.ValidationInputs {
		if input.Development != nil && input.Development.Batch.ID == batch {
			return id, input
		}
	}
	d.t.Fatalf("missing batch %s: %+v", batch, d.run.ValidationInputs)
	return "", protocol.ValidationInput{}
}
func TestConfirmedBatchesPreserveDependenciesAndCandidateSources(t *testing.T) {
	for _, tier := range []string{"FINAL", "CHECKPOINT"} {
		for _, reopen := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reopen=%t", tier, reopen), func(t *testing.T) {
				p := commandPlan()
				base := p.Batches[0]
				p.Batches = nil
				for _, id := range []string{"parser", "command", "independent"} {
					b := base
					b.ID = id
					b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "implement " + id}}
					b.Artifacts = []string{id + ".go"}
					if id == "command" {
						b.DependsOn = []string{"parser"}
					}
					p.Batches = append(p.Batches, b)
				}
				p.VerificationTier = tier
				if tier == "CHECKPOINT" {
					p.CheckpointGroups = [][]string{{"parser", "independent"}, {"command"}}
					p.CheckpointReason = "The command consumes a separately verifiable parser and independent utility boundary"
				}
				d := nativeMasterDriver(t)
				passPreDevelopmentReviews(t, d, p)
				d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Command consumes parser while utility is independent"}})
				d.confirmRoute(routeDecisionForGates([]string{"quality"}))
				d.drive()
				id, a := planBatchAction(d, "parser")
				other, b := planBatchAction(d, "independent")
				contents := map[string]string{}
				for _, input := range []protocol.ValidationInput{a, b} {
					for _, path := range input.Development.Batch.Artifacts {
						contents[path] = "package main\n// " + input.Development.Batch.Deliverable + "\n"
						if err := os.WriteFile(filepath.Join(input.Workspace.Path, path), []byte(contents[path]), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				for _, input := range d.run.ValidationInputs {
					if input.Development != nil && input.Development.Batch.ID == "command" {
						t.Fatal("dependent batch issued before predecessor evidence")
					}
				}
				if len(d.run.RequirementWork) != 3 {
					t.Fatalf("future batch omitted from work: %+v", d.run.RequirementWork)
				}
				d.business(other, protocol.ValidationResult{Binding: b.Binding, Status: "PASS", Development: batchEvidence(b)})
				for _, intent := range d.run.AdapterIntents {
					if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && intent.Adapter.Params["stage"] == "DEVELOPMENT" {
						t.Fatal("partial group materialized")
					}
				}
				d.drive()
				d.business(id, protocol.ValidationResult{Binding: a.Binding, Status: "PASS", Development: batchEvidence(a)})
				d.drive()
				if reopen {
					d.reopen()
					d.drive()
				}
				if tier == "CHECKPOINT" {
					d.materialize()
					d.materialize()
					gid, gin := d.gateAction("quality")
					d.business(gid, protocol.ValidationResult{Binding: gin.Binding, Status: "PASS"})
					d.drive()
					wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
					if wave.Counted || len(wave.CheckpointTasks) != 2 {
						t.Fatalf("group check changed repair quota: %+v", wave)
					}
				}
				id, in := planBatchAction(d, "command")
				if !reflect.DeepEqual(in.Development.Dependencies["parser"].Validation.Development, batchEvidence(a)) || in.Development.Dependencies["parser"].Validation.Binding != a.Binding {
					t.Fatal("dependency handoff lost original evidence")
				}
				for _, path := range in.Development.Batch.Artifacts {
					contents[path] = "package main\n// " + in.Development.Batch.Deliverable + "\n"
					if err := os.WriteFile(filepath.Join(in.Workspace.Path, path), []byte(contents[path]), 0600); err != nil {
						t.Fatal(err)
					}
				}
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
				d.drive()
				snapshot := driverState(t, d)
				contents[snapshot.RequirementArtifactPath()] = validationDocument
				paths := make([]string, 0, len(contents))
				for path := range contents {
					paths = append(paths, path)
				}
				slices.Sort(paths)
				candidateWave := snapshot.Validation.Waves[len(snapshot.Validation.Waves)-1]
				if len(candidateWave.SourceTasks) != 3 || !slices.Contains(candidateWave.SourceTasks, a.Binding.Task) || !slices.Contains(candidateWave.SourceTasks, b.Binding.Task) || !slices.Contains(candidateWave.SourceTasks, in.Binding.Task) {
					t.Fatalf("candidate omitted batch sources: %+v", candidateWave.SourceTasks)
				}
				found := false
				for _, receipt := range snapshot.HostActionReceipts {
					if receipt.AdapterOperation != "op.candidate" || receipt.AdapterEvidence == nil {
						continue
					}
					var evidence protocol.CandidateEvidence
					if err := decodeReceiptEvidence(receipt, &evidence); err != nil {
						t.Fatal(err)
					}
					if evidence.Identity == candidateWave.Candidate.Identity && slices.Equal(evidence.Paths, paths) {
						found = true
					}
				}
				if !found {
					t.Fatal("completed plan has no candidate")
				}
				d.materialize()
				d.materialize()
				gid, gin := d.gateAction("quality")
				d.business(gid, protocol.ValidationResult{Binding: gin.Binding, Status: "PASS"})
				d.drive()
				if reopen {
					d.reopen()
				}
				wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				for path, want := range contents {
					body, err := (vcs.ExecRunner{}).RunRaw(d.f.root, "git", nil, "show", wave.Candidate.Identity+":"+path)
					if err != nil || string(body) != want {
						t.Fatalf("frozen delivery %s = %q, want %q: %v", path, body, want, err)
					}
				}
				if wave.Status != "COMPLETE" || len(wave.CheckpointTasks) != 0 {
					t.Fatal("final complete plan did not receive final validation")
				}
				before := d.run.Revision
				d.drive()
				if before != d.run.Revision {
					t.Fatal("repeat drive added work")
				}
			})
		}
	}
}

func TestChangedBatchContractUsesExplicitDispositionAndRetainsCarriedResult(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("reopen=%t", reopen), func(t *testing.T) {
			p := commandPlan()
			first := p.Batches[0]
			first.ID = "parser"
			first.Subtasks = []protocol.DevelopmentSubtask{{ID: "parser-body", Description: "Implement parser"}}
			first.Artifacts = []string{"parser.go"}
			p.Batches[0].DependsOn = []string{"parser"}
			p.Batches = append([]protocol.DevelopmentBatch{first}, p.Batches...)
			d := plannedValidationDriver(t, nil, p, "quality")
			parser, pi := planBatchAction(d, "parser")
			d.business(parser, protocol.ValidationResult{Binding: pi.Binding, Status: "PASS", Development: batchEvidence(pi)})
			old, ci := planBatchAction(d, "command")
			before, _ := d.f.engine.Load()
			raw, _ := json.Marshal(before.State.Results[parser])
			document := validationDocument + "\nImplementation must retain parser output while adding the command's diagnostic contract.\n"
			receipt := before.State.CurrentIntakeConfirmation()
			if err := os.WriteFile(filepath.Join(d.f.root, receipt.RequirementSource), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			intake, err := prepareConfirmedIntake(d.f.root, receipt, true)
			if err != nil {
				t.Fatal(err)
			}
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			var choices []protocol.RequirementWorkDecision
			for _, w := range d.run.RequirementWork {
				disposition := protocol.RequirementWorkCarry
				if w.ActionID == old {
					disposition = protocol.RequirementWorkRedo
				}
				choices = append(choices, protocol.RequirementWorkDecision{Work: w, Disposition: disposition, Reason: "Parser unchanged; command contract explicitly replaced"})
			}
			// 本场景同时勾选重走路线：解除绑定后由下面的重新选择确认新计
			// 划（重选通道常开，直接重选同样可达——见需求侧同名场景）。
			pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:diagnostic-contract", ImpactDigest: "sha256:command-only", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationChanged, RedoStartReadiness: true, RedoRoute: true, WorkDecisions: choices}
			request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			ask := d.run.PendingRequirementAsk
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, RedoStartReadiness: true, RedoRoute: true, WorkDecisions: choices, Reason: "Confirm changed batch contract"}}}.WithRequirementDocument(document))
			if reopen {
				d.reopen()
			}
			d.drive()
			nextPlan := *p
			nextPlan.Batches = append([]protocol.DevelopmentBatch(nil), p.Batches...)
			nextPlan.Batches[1].DoD = append(append([]protocol.DevelopmentCriterion(nil), p.Batches[1].DoD...), protocol.DevelopmentCriterion{ID: "diagnostic", Description: "Invalid input reports its specific diagnostic"})
			var ready string
			for id, pending := range d.run.PendingActions {
				if pending.ReviewSeries == review.StartReadiness {
					ready = id
				}
			}
			if ready == "" {
				t.Fatal("plan change omitted fresh readiness")
			}
			d.receipt(ready)
			worker, issued := driverState(t, d).WorkerInput(ready, d.f.definition)
			if !issued {
				t.Fatal("readiness fixture lacks its original issued input")
			}
			fixturePlanRequirements(&nextPlan, worker.RequirementPoints)
			ev, err := protocol.NewReviewWorkerResultEvent(d.eventID(), ready, DefaultProvider, "new plan", protocol.ReviewResultEvidence{Action: review.StartReadiness, DispatchID: ready, RequirementRevision: revision, Status: "PASS", GranularityReview: &nextPlan, Findings: []review.Finding{}})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(ev)
			verifyReviewFixture(t, d, ev)
			for _, input := range d.run.ValidationInputs {
				if input.Handler == "engine.development.batch.v6" {
					t.Fatal("new contract executed before plan confirmation")
				}
			}
			// 重走解除了路线绑定：重新选择即新计划的确认通道（绑定→重走
			// 解除→重新选择）。
			d.confirmRoute(protocol.RouteDecision{Gates: []string{"quality", "blackbox"}, Selected: []string{"quality"}})
			d.drive()
			id, in := planBatchAction(d, "command")
			if id == old || in.Binding.Task == ci.Binding.Task || len(in.Development.Batch.DoD) != 2 {
				t.Fatal("replacement did not bind new plan")
			}
			for _, input := range d.run.ValidationInputs {
				if input.Binding.Task == pi.Binding.Task {
					t.Fatal("changed overall plan digest reran carried parser")
				}
			}
			if in.Development.Dependencies["parser"].ActionID != parser {
				t.Fatal("carried dependency lost source action")
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
			d.materialize()
			d.materialize()
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			d.drive()
			d.reopen()
			after, _ := d.f.engine.Load()
			retained, _ := json.Marshal(after.State.Results[parser])
			if string(raw) != string(retained) {
				t.Fatal("plan replacement rewrote original carried result")
			}
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("new plan did not complete")
			}
		})
	}
}

func TestCheckpointRepairsConsumeTheSharedFiniteAllowance(t *testing.T) {
	p := commandPlan()
	second := p.Batches[0]
	second.ID = "dependent"
	second.Subtasks = []protocol.DevelopmentSubtask{{ID: "dependent-body", Description: "Consume command contract"}}
	second.DependsOn = []string{"command"}
	second.Artifacts = []string{"consumer.go"}
	p.Batches = append(p.Batches, second)
	p.VerificationTier = "CHECKPOINT"
	p.CheckpointGroups = [][]string{{"command"}, {"dependent"}}
	p.CheckpointReason = "Verify command before a dependent integration"
	d := plannedValidationDriver(t, nil, p, "quality")
	id, in := planBatchAction(d, "command")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	d.materialize()
	d.materialize()
	problem := ""
	for n := 0; n <= 3; n++ {
		gate, gi := d.gateAction("quality")
		d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P1", SeverityRationale: "Supported success path is unusable", Requirement: "AC-001", NormalPath: "app run valid", ActualError: "valid input exits nonzero", DeferralImpact: "command cannot be used", Locations: []string{"app.go:1"}, Message: fmt.Sprintf("Command still rejects supported input in candidate %d", n)}}})
		wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
		var finding string
		for id, f := range d.run.Validation.Findings {
			if f.WaveID == wave.ID {
				finding = id
			}
		}
		resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "command", RootCause: "valid input rejected", Consequence: "public success path unusable", Severity: "P1", SeverityRationale: "Supported command cannot complete", Verification: review.FindingVerification{Scope: "confirmed command contract", Severity: "main path unusable", Causality: "input dispatch branch", Reproduction: "valid input rejected", Classification: "implementation"}, Reason: "Reproduced actual public input rejection"}
		if problem != "" {
			resolution.Decision = "SAME_AS"
			resolution.ProblemID = problem
		}
		d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
		d.drive()
		for key := range d.run.Validation.Problems {
			problem = key
		}
		if n == 3 {
			ask := d.validationAsk("REPAIR")
			if ask.Validation.Target != wave.ID {
				t.Fatal("repair allowance detached from checkpoint obligation")
			}
			for _, input := range d.run.ValidationInputs {
				if input.Handler == "engine.repair.v7" || input.Development != nil {
					t.Fatal("checkpoint bypassed finite repair decision")
				}
			}
			break
		}
		repair, ri := d.action("engine.repair.v7")
		d.business(repair, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Correct input dispatch branch"}})
		d.materialize()
		d.materialize()
		d.reopen()
	}
	counted := 0
	for _, w := range d.run.Validation.Waves {
		if w.Counted {
			counted++
		}
	}
	if counted != 3 || d.run.Validation.Waves[0].Counted {
		t.Fatalf("checkpoint altered shared repair count: %d", counted)
	}
	ask := d.validationAsk("REPAIR")
	decideValidation(d, ask, "approve")
	d.reopen()
	d.drive()
	repair, ri := d.action("engine.repair.v7")
	d.business(repair, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Correct the confirmed command rejection"}})
	d.materialize()
	d.materialize()
	gate, gi := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	_, dependent := planBatchAction(d, "dependent")
	if dependent.Development == nil {
		t.Fatal("authorized correction did not unlock the real successor")
	}
}
func TestSpikeConclusionUnlocksDevelopmentWithoutEnteringCandidate(t *testing.T) {
	for _, tier := range []string{"FINAL", "CHECKPOINT"} {
		t.Run(tier, func(t *testing.T) {
			p := commandPlan()
			spike := p.Batches[0]
			spike.ID = "spike"
			spike.Spike = true
			spike.Subtasks = []protocol.DevelopmentSubtask{{ID: "probe", Description: "Resolve command boundary uncertainty"}}
			spike.Artifacts = []string{"probe.go"}
			p.Batches[0].DependsOn = []string{"spike"}
			p.Batches = append([]protocol.DevelopmentBatch{spike}, p.Batches...)
			parser := p.Batches[1]
			parser.ID = "parser"
			parser.Subtasks = []protocol.DevelopmentSubtask{{ID: "parse", Description: "Apply the spike conclusion in a parser"}}
			parser.Artifacts = []string{"parser.go"}
			p.Batches[1].DependsOn = []string{"parser"}
			p.Batches = append(p.Batches[:1], parser, p.Batches[1])
			if tier == "CHECKPOINT" {
				p.VerificationTier = tier
				p.CheckpointGroups = [][]string{{"spike"}, {"parser", "command"}}
				p.CheckpointReason = "Record the investigation before production development"
			}
			d := plannedValidationDriver(t, nil, p, "quality")
			id, in := planBatchAction(d, "spike")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
			if len(d.run.AdapterIntents) != 0 {
				t.Fatal("spike code entered a candidate")
			}
			parserID, parserInput := planBatchAction(d, "parser")
			if parserInput.Development.Dependencies["spike"].ActionID != id {
				t.Fatal("spike conclusion was not handed off")
			}
			d.business(parserID, protocol.ValidationResult{Binding: parserInput.Binding, Status: "PASS", Development: batchEvidence(parserInput)})
			dev, di := planBatchAction(d, "command")
			if di.Development.Dependencies["parser"].ActionID != parserID {
				t.Fatal("transitive predecessor evidence missing")
			}
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
			for _, intent := range d.run.AdapterIntents {
				data, _ := json.Marshal(intent.Adapter.Params)
				var params protocol.CandidateParameters
				_ = json.Unmarshal(data, &params)
				if len(params.SourceTasks) != 2 || !slices.Contains(params.SourceTasks, di.Binding.Task) || !slices.Contains(params.SourceTasks, parserInput.Binding.Task) || slices.Contains(params.Paths, "probe.go") {
					t.Fatalf("spike artifact was delivered: %+v", params)
				}
			}
			d.materialize()
			d.materialize()
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("spike plan did not finish")
			}
		})
	}
}

func TestPlanAdmissionRequiresCompleteProposalAndExactConfirmation(t *testing.T) {
	finish := func(d *validationDriver, action review.Action, plan *protocol.GranularityReview) (string, protocol.Event) {
		d.t.Helper()
		var id string
		for key, p := range d.run.PendingActions {
			if p.ReviewSeries == action {
				id = key
			}
		}
		if id == "" {
			d.t.Fatal("missing review")
		}
		d.receipt(id)
		if action == review.StartReadiness && plan != nil {
			input, ok := driverState(d.t, d).WorkerInput(id, d.f.definition)
			if !ok {
				d.t.Fatal("plan fixture lacks signed input")
			}
			fixturePlanRequirements(plan, input.RequirementPoints)
		}
		event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, DefaultProvider, string(d.eventID()), protocol.ReviewResultEvidence{Action: action, DispatchID: id, RequirementRevision: d.revision, Status: "PASS", GranularityReview: plan, Findings: []review.Finding{}})
		if err != nil {
			d.t.Fatal(err)
		}
		return id, event
	}
	for _, kind := range []string{"missing", "duplicate-batch", "cycle", "duplicate-subtask", "missing-check", "missing-full-tests", "missing-handoff", "checkpoint-omission"} {
		t.Run(kind, func(t *testing.T) {
			d := startValidationDriver(t, nil)
			_, product := finish(d, review.ProductReview, nil)
			d.submit(product)
			verifyReviewFixture(t, d, product)
			d.drive()
			p := commandPlan()
			switch kind {
			case "missing":
				p = nil
			case "duplicate-batch":
				p.Batches = append(p.Batches, p.Batches[0])
			case "cycle":
				p.Batches[0].DependsOn = []string{p.Batches[0].ID}
			case "duplicate-subtask":
				p.Batches[0].Subtasks = append(p.Batches[0].Subtasks, p.Batches[0].Subtasks[0])
			case "missing-check":
				p.Batches[0].Checks = nil
			case "missing-full-tests":
				p.Batches[0].Checks[1].Purpose = ""
			case "missing-handoff":
				p.Batches[0].Handoff = ""
			case "checkpoint-omission":
				p.VerificationTier = "CHECKPOINT"
				p.CheckpointGroups = [][]string{{"command"}, {"unknown"}}
				p.CheckpointReason = "structural grouping"
			}
			id, event := finish(d, review.StartReadiness, p)
			task := d.run.PendingActions[id].Task
			accepted := d.submit(event)
			if accepted.Status != "REJECTED_INVALID_RESULT" {
				t.Fatalf("invalid proposal became effective: %s", kind)
			}
			d.reopen()
			d.drive()
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			rejected := snap.State.RejectedResults[id]
			if rejected.Status != "REJECTED_INVALID_RESULT" || rejected.Reason == "" || !reflect.DeepEqual(rejected.Payload.Review, event.Result.Review) {
				t.Fatalf("invalid proposal lost original evidence: %+v", rejected)
			}
			if kind == "missing-full-tests" && !strings.Contains(rejected.Reason, protocol.CheckFullTests) {
				t.Fatal("missing project-test purpose was rejected for another reason", rejected.Reason)
			}
			if snap.State.ReviewSeries.StartReadiness.Completed != 0 || snap.State.ReviewSeries.StartReadiness.FirstPass || snap.State.RouteConfirmed || len(snap.State.SliceChildren) != 0 {
				t.Fatal("invalid worker content unlocked route or counted a semantic round")
			}
			var retry string
			for action, pending := range d.run.PendingActions {
				if pending.Task == task {
					retry = action
				}
			}
			if retry == "" || retry == id || snap.State.Attempts[task].Attempts != 1 || snap.State.Attempts[task].MaxAttempts != 2 {
				t.Fatal("invalid proposal escaped original finite task")
			}
			before, _ := json.Marshal(snap)
			if got := d.submit(event); !reflect.DeepEqual(got, accepted) {
				t.Fatal("repeated event changed acceptance")
			}
			after, _ := d.f.engine.Load()
			raw, _ := json.Marshal(after)
			if string(raw) != string(before) {
				t.Fatal("duplicate invalid event wrote state")
			}
			_, valid := finish(d, review.StartReadiness, commandPlan())
			d.submit(valid)
			verifyReviewFixture(t, d, valid)
			completed, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if completed.State.ReviewSeries.StartReadiness.Completed != 1 {
				t.Fatal("valid replacement did not complete original review")
			}
		})
	}
	d := startValidationDriver(t, nil)
	_, product := finish(d, review.ProductReview, nil)
	d.submit(product)
	verifyReviewFixture(t, d, product)
	d.drive()
	_, ready := finish(d, review.StartReadiness, commandPlan())
	d.submit(ready)
	verifyReviewFixture(t, d, ready)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	route := routeDecisionForGates([]string{"blackbox", "quality"})
	req, err := protocol.NewRouteRequestEvent(d.eventID(), route.Gates, protocol.AskOption{ID: "approve", Label: "Confirm"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(req)
	token, err := d.f.Freshness(string(req.ID))
	if err != nil {
		t.Fatal(err)
	}
	event, err := protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(req.ID), token, "approve", protocol.ReviewDecisionPayload{Route: &route})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := d.f.engine.Load()
	raw, _ := json.Marshal(before)
	if _, _, err := d.f.Submit(event); err == nil {
		t.Fatal("route without exact plan confirmation was admitted")
	}
	after, _ := d.f.engine.Load()
	again, _ := json.Marshal(after)
	if string(raw) != string(again) {
		t.Fatal("missing confirmation changed state")
	}
	preview, err := d.f.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	event, err = protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(req.ID), token, "approve", protocol.ReviewDecisionPayload{Route: &route})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(event)
	d.drive()
	id, in := d.action("engine.development.batch.v6")
	d.receipt(id)
	evidence := batchEvidence(in)
	delete(evidence.Completion.DoD, "input-contract")
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "missing actual DoD", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: evidence}}})
	if a.Status != "REJECTED_INVALID_RESULT" {
		t.Fatal("partial DoD completed batch")
	}
	retry, ri := d.action("engine.development.batch.v6")
	if ri.Binding.Task != in.Binding.Task || d.run.Attempts[in.Binding.Task].Attempts != 1 {
		t.Fatal("DoD rejection escaped original finite duty")
	}
	d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: batchEvidence(ri)})
	snap, _ := d.f.engine.Load()
	if snap.State.Results[retry].Validation.Development.Completion.Handoff == "" || snap.State.RejectedResults[id].Payload.Validation.Development.Completion.DoD["input-contract"] != "" {
		t.Fatal("raw complete/rejected evidence was not retained")
	}
}

func TestSplitTopologyConfirmsExactPlanAndReopens(t *testing.T) {
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, splitCommandPlan())
	topology := splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlConfirmTopology, protocol.AskOption{ID: "split", Label: "split"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	var token string
	for _, ask := range d.run.PendingAsks {
		if ask.RequestID == string(request.ID) {
			token = ask.Token
		}
	}
	path := filepath.Join(d.f.stateDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := d.f.DevelopmentPlan(protocol.RouteDecision{}, *topology)
	if err != nil {
		t.Fatal(err)
	}
	for _, confirmation := range []*protocol.DevelopmentPlanConfirmation{nil, {ReviewActionID: preview.Confirmation.ReviewActionID, Digest: "wrong"}} {
		topology.DevelopmentPlan = confirmation
		event, err := protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(request.ID), token, "split", protocol.ReviewDecisionPayload{Topology: topology})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = d.f.Submit(event); err == nil {
			t.Fatal("missing or incorrect split plan confirmation accepted")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("rejected confirmation changed state", err)
		}
	}
	topology.DevelopmentPlan = &preview.Confirmation
	event, err := protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(request.ID), token, "split", protocol.ReviewDecisionPayload{Topology: topology})
	if err != nil {
		t.Fatal(err)
	}
	accepted := d.submit(event)
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	decision := snapshot.State.Decisions[string(request.ID)]
	if decision.Review == nil || !reflect.DeepEqual(decision.Review.Topology.DevelopmentPlan, &preview.Confirmation) || !snapshot.State.RouteConfirmed {
		t.Fatal("split lost its original plan confirmation")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d.reopen()
	if replay := d.submit(event); !reflect.DeepEqual(replay, accepted) {
		t.Fatal("reopen changed duplicate acceptance")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(saved) != string(after) {
		t.Fatal("duplicate split confirmation rewrote state", err)
	}
}

func TestReleasedSlicePlanPreviewUsesConfirmedOwnership(t *testing.T) {
	master := splitMaster(t)
	master.drive()
	parent := driverState(t, master)
	child, err := Open(master.f.root, parent.SliceChildren["lane-core"])
	if err != nil {
		t.Fatal(err)
	}
	run, err := child.Show()
	if err != nil {
		t.Fatal(err)
	}
	driver := &validationDriver{t: t, f: child, run: run, revision: master.revision, provider: master.host(), git: child.vcs}
	driver.drive()
	route := protocol.RouteDecision{Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"}}
	expected, err := parent.SliceDevelopmentPlan("lane-core")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(child.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := child.DevelopmentPlan(route)
	if err != nil || len(preview.Proposal.Batches) != 1 || !reflect.DeepEqual(preview.Proposal, *expected) {
		t.Fatal("released child did not project its confirmed creator contract", preview, err)
	}
	after, err := os.ReadFile(filepath.Join(child.stateDir, "state.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only automatic slice preview changed authority", err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	driver.control(protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route})
	driver.drive()
	driver.reopen()
	id, input := planBatchAction(driver, "command")
	if id == "" || input.Development == nil || !reflect.DeepEqual(input.Development.Batch, preview.Proposal.Batches[0]) {
		t.Fatal("normal execution and reopen lost the exact automatically projected slice plan")
	}
}

func TestCheckpointMasterConfirmsFinalSliceAndReopensItsOriginalPlan(t *testing.T) {
	master := nativeMasterDriver(t)
	plan := splitCommandPlan()
	plan.VerificationTier = "CHECKPOINT"
	plan.CheckpointReason = "Independent child delivery precedes master integration"
	plan.CheckpointGroups = [][]string{{plan.Batches[0].ID, plan.Batches[1].ID}, {plan.Batches[2].ID}}
	passPreDevelopmentReviews(t, master, plan)
	original, _ := json.Marshal(driverState(t, master).ReadinessProposal())
	request, err := protocol.NewRequestEvent(master.eventID(), protocol.ControlConfirmTopology, protocol.AskOption{ID: "split", Label: "split"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	candidate, err := master.f.PrepareControl(ControlInput{Ask: askPosition(t, master.run, protocol.ControlConfirmTopology), Choice: "split", Owners: []int{1}, Parallel: "2"})
	if err != nil {
		t.Fatal("public prospective split rejected valid checkpoint master", err)
	}
	if candidate.DevelopmentPlan == nil {
		t.Fatal("public candidate omitted original plan confirmation")
	}
	if _, run, err := master.f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	} else {
		master.run = run
	}
	master.drive()
	parent := driverState(t, master)
	child, err := Open(master.f.root, parent.SliceChildren["lane-core"])
	if err != nil {
		t.Fatal(err)
	}
	run, err := child.Show()
	if err != nil {
		t.Fatal(err)
	}
	driver := &validationDriver{t: t, f: child, run: run, revision: master.revision, provider: master.host(), git: child.vcs}
	driver.drive()
	route := protocol.RouteDecision{Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"}}
	preview, err := child.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Proposal.VerificationTier != "FINAL" || preview.Proposal.CheckpointGroups != nil || preview.Proposal.CheckpointReason != "" || !reflect.DeepEqual(preview.Proposal.Batches, []protocol.DevelopmentBatch{plan.Batches[0]}) {
		t.Fatal("child preview lost derived tier or original obligations")
	}
	route.DevelopmentPlan = &preview.Confirmation
	driver.control(protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route})
	driver.drive()
	driver.reopen()
	_, input := planBatchAction(driver, plan.Batches[0].ID)
	if !reflect.DeepEqual(input.Development.Batch, plan.Batches[0]) {
		t.Fatal("confirmed child/reopen changed original FULL checks, interface or DoD")
	}
	source := driverState(t, master).ReadinessProposal()
	after, _ := json.Marshal(source)
	if !bytes.Equal(original, after) {
		t.Fatal("public split projection rewrote master checkpoint plan")
	}
}
