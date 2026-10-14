package facade

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// User edits use the same complete material preview and typed confirmation at
// review, route, development and validation boundaries on a native microproject.
func TestUserRequirementEditsConfirmCompleteSourceAtBusinessBoundaries(t *testing.T) {
	second := "\n### REQ-002：错误输入\n\n#### 要求\n错误输入拒绝。\n\n#### 验收条件\n- AC-002：错误输入退出码非零。\n\n#### 来源\n用户要求。\n"
	original := strings.Replace(validationDocument, "正常输入成功，错误输入拒绝。", "正常输入退出码零。", 1) + second
	for _, boundary := range []string{"product-review", "route-selection", "development", "validation"} {
		for _, edit := range []string{"add", "delete", "modify"} {
			t.Run(boundary+"/"+edit, func(t *testing.T) {
				root := t.TempDir()
				snapshotRepoCommits(t, root)
				path := filepath.Join(root, "requirements.md")
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"add", "requirements.md"}, {"commit", "-m", "Confirmed command requirements"}} {
					if _, err := (vcs.ExecRunner{}).Run(root, "git", args...); err != nil {
						t.Fatal(err)
					}
				}
				base, err := vcs.New(root, nil).Resolve()
				if err != nil {
					t.Fatal(err)
				}
				intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
				if err != nil {
					t.Fatal(err)
				}
				f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Capacity: 16, Admission: testAdmission(t, root, "user-edit-runtime", "sha256:package", DefaultProvider), Request: StartRequest{RunID: "user-edit", Route: "regular", BaseSnapshot: base, IntakeConfirmationReceipt: intake.IntakeCandidate}})
				if err != nil {
					t.Fatal(err)
				}
				d := &validationDriver{t: t, f: f, run: run, revision: intake.Revision, git: vcs.New(root, nil)}
				d.drive()
				if boundary != "product-review" {
					passPreDevelopmentReviews(t, d, commandPlan())
					d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command contract"}})
				}
				if boundary == "development" || boundary == "validation" {
					d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
					d.drive()
				}
				if boundary == "validation" {
					design, input := d.action("engine.qa.design.v5")
					manifest := simpleManifest(input)
					manifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "normal"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "invalid"}, {SourceID: "REQ-002", PointID: "AC-002", CaseID: "normal"}, {SourceID: "REQ-002", PointID: "AC-002", CaseID: "invalid"}}
					dev, di := d.action("engine.development.batch.v5")
					d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
					if len(d.run.RejectedResults) != 0 {
						t.Fatalf("native QA preparation rejected its source manifest: %+v", d.run.RejectedResults)
					}
					d.drive()
					reviewID, ri := d.action("engine.qa.review.rationale.v2")
					r := approvedReview(manifest)
					d.business(reviewID, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
					d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "command implements both confirmed input conditions"}})
					d.drive()
				}
				statePath := filepath.Join(f.stateDir, "state.json")
				readState := func() []byte {
					t.Helper()
					data, err := os.ReadFile(statePath)
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				before := readState()
				old, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				request, err := f.PrepareCurrentControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
				if err != nil || !bytes.Equal(before, readState()) {
					t.Fatal("request preview wrote workflow facts", err)
				}
				if _, d.run, err = f.SubmitControl(request); err != nil {
					t.Fatal(err)
				}
				d.drive()
				frozen, err := f.engine.Load()
				if err != nil || frozen.State.RequirementRevision() != intake.Revision || !reflect.DeepEqual(old.State.PendingActions, frozen.State.PendingActions) || frozen.State.Phase != old.State.Phase {
					t.Fatal("clarification advanced the revision, phase or execution", err)
				}
				document := original
				switch edit {
				case "add":
					document += "\n### REQ-003：重复输入\n\n#### 要求\n重复正常输入仍成功。\n\n#### 验收条件\n- AC-003：连续两次正常输入退出码均为零。\n\n#### 来源\n用户新增要求。\n"
				case "delete":
					document = strings.TrimSuffix(original, second)
				case "modify":
					document = strings.Replace(original, "错误输入退出码非零。", "错误输入退出码为一。", 1)
				}
				if err := os.WriteFile(path, []byte(document), 0600); err != nil {
					t.Fatal(err)
				}
				work := make([]WorkChoice, len(d.run.RequirementWork))
				for i := range work {
					work[i] = WorkChoice{Disposition: protocol.RequirementWorkRedo, Reason: "user explicitly requests fresh work for this changed contract"}
				}
				before = readState()
				prepared, err := f.PrepareCurrentControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationChanged, Redo: []string{"product-review", "start-readiness"}, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{edit}, TransitiveImpacts: []string{"command implementation and acceptance cases"}}}})
				if err != nil || !bytes.Equal(before, readState()) {
					t.Fatal("impact suggestion wrote execution or authority", err)
				}
				if prepared.Intake.Content != document || prepared.Intake.RequirementDocument.ContentDigest != prepared.Intake.Revision {
					t.Fatal("suggestion lost the complete original candidate bytes")
				}
				for i, choice := range prepared.Event.Request.Package.WorkDecisions {
					if !reflect.DeepEqual(choice.Work, d.run.RequirementWork[i]) {
						t.Fatal("suggestion changed original work identity")
					}
				}
				if _, d.run, err = f.SubmitControl(prepared); err != nil {
					t.Fatal(err)
				}
				bound, err := f.engine.Load()
				if err != nil || bound.State.RequirementRevision() != intake.Revision || !reflect.DeepEqual(frozen.State.ReviewSeries, bound.State.ReviewSeries) || !reflect.DeepEqual(frozen.State.PendingActions, bound.State.PendingActions) {
					t.Fatal("package binding executed the suggested redo before confirmation", err)
				}
				before = readState()
				confirmation, err := f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "user confirms the complete changed document and each fresh-work decision"})
				if err != nil || !bytes.Equal(before, readState()) {
					t.Fatal("confirmation preview authorized the suggestion", err)
				}
				if edit == "modify" {
					another, err := f.PrepareCurrentControl(ControlInput{Request: protocol.ControlAbort})
					if err != nil {
						t.Fatal(err)
					}
					if _, d.run, err = f.SubmitControl(another); err != nil {
						t.Fatal(err)
					}
					before = readState()
					if _, _, err = f.SubmitControl(confirmation); err == nil || !strings.Contains(err.Error(), protocol.CodeStaleFreshness) || !bytes.Equal(before, readState()) {
						t.Fatal("stale confirmation changed workflow or source authority", err)
					}
					confirmation, err = f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "user confirms the complete changed document on the current Ask"})
					if err != nil || !bytes.Equal(before, readState()) {
						t.Fatal("fresh confirmation preview changed workflow", err)
					}
					bound, err = f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
				}
				accepted, _, err := f.SubmitControl(confirmation)
				if err != nil {
					t.Fatal(err)
				}
				updated, err := f.engine.Load()
				if err != nil || updated.Revision != bound.Revision+1 || updated.State.RequirementRevision() != confirmation.Intake.Revision || !reflect.DeepEqual(updated.State.Validation.Document, *confirmation.Intake.RequirementDocument) || !reflect.DeepEqual(old.State.IntakeReceipt, updated.State.IntakeReceipt) {
					t.Fatal("confirmation separated source, revision, projection or original intake", err)
				}
				before = readState()
				reopened, err := Open(root, d.run.RunID)
				if err != nil {
					t.Fatal(err)
				}
				replay, _, err := reopened.SubmitControl(confirmation)
				if err != nil || !reflect.DeepEqual(accepted, replay) || !bytes.Equal(before, readState()) {
					t.Fatal("reopened original confirmation replay changed durable facts", err)
				}
			})
		}
	}
}

func TestRequirementChangePreservesSelectedMaterials(t *testing.T) {
	for _, scenario := range []string{"independent", "same-content", "updated-solution", "changed-solution", "changed-reference"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			snapshotRepoCommits(t, root)
			solution := "# Selected solution\nUse the approved entry.\n"
			if scenario == "same-content" {
				data, err := os.ReadFile(filepath.Join(root, "requirements.md"))
				if err != nil {
					t.Fatal(err)
				}
				solution = string(data)
			}
			for path, content := range map[string]string{"design.md": solution, "reference.md": "# Selected reference\nExact source evidence.\n"} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git := vcs.New(root, nil)
			runner := vcs.ExecRunner{}
			for _, args := range [][]string{{"add", "design.md", "reference.md"}, {"commit", "-m", "Selected confirmation material"}} {
				if _, err := runner.Run(root, "git", args...); err != nil {
					t.Fatal(err)
				}
			}
			base, err := git.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			initial, err := PrepareIntake(root, "requirements.md", "design.md", []string{"reference.md"}, true)
			if err != nil {
				t.Fatal(err)
			}
			if initial.IntakeCandidate.SolutionSource != "design.md" {
				t.Fatal("selected solution path was not retained")
			}
			if scenario == "same-content" {
				defaultSelection, err := PrepareIntake(root, "requirements.md", "", []string{"reference.md", "design.md"}, true)
				if err != nil {
					t.Fatal(err)
				}
				if defaultSelection.IntakeCandidate.SolutionRevision != initial.IntakeCandidate.SolutionRevision || defaultSelection.IntakeCandidate.Digest == initial.IntakeCandidate.Digest {
					t.Fatal("equal content erased the distinct selected path")
				}
			}
			f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "selected-material", Route: "regular", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: initial.IntakeCandidate}, Admission: testAdmission(t, root, "selected-material-runtime", "sha256:package", DefaultProvider)})
			if err != nil {
				t.Fatal(err)
			}
			run, err := f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			initialState, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			initialReceipt, _ := json.Marshal(initialState.State.IntakeReceipt)
			begin, err := f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
			if err != nil {
				t.Fatal(err)
			}
			_, run, err = f.SubmitControl(begin)
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(filepath.Join(root, "requirements.md"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "requirements.md"), append(original, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "updated-solution" {
				if err = os.WriteFile(filepath.Join(root, "design.md"), []byte(solution+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			work := make([]WorkChoice, len(run.RequirementWork))
			for i := range work {
				work[i] = WorkChoice{Disposition: protocol.RequirementWorkRedo, Reason: "Recheck the confirmed material revision"}
			}
			change, err := f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, CarryReasons: []string{"selected material keeps its meaning"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(change.Intake.Materials) != 3 || change.Intake.IntakeCandidate.SolutionSource != "design.md" {
				t.Fatal("changed preview dropped selected solution or reference")
			}
			for _, material := range change.Intake.Materials {
				data, err := os.ReadFile(filepath.Join(root, material.Path))
				if err != nil || string(data) != material.Content || requirementContentRevision(data) != material.Revision {
					t.Fatal("preview changed original material bytes", err)
				}
			}
			if scenario != "updated-solution" && change.Intake.IntakeCandidate.SolutionRevision != initial.IntakeCandidate.SolutionRevision {
				t.Fatal("changed requirement replaced the independent solution binding")
			}
			if scenario == "updated-solution" && change.Intake.IntakeCandidate.SolutionRevision == initial.IntakeCandidate.SolutionRevision {
				t.Fatal("updated selected solution kept its old binding")
			}
			_, run, err = f.SubmitControl(change)
			if err != nil {
				t.Fatal(err)
			}
			confirmInput := ControlInput{Ask: askPosition(t, run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm the displayed complete selected material"}
			confirm, err := f.PrepareControl(confirmInput)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(confirm.Intake, change.Intake) || !reflect.DeepEqual(confirm.Event.Decide.Requirement.Intake, &change.Intake.IntakeCandidate) {
				t.Fatal("decision did not bind the original complete package")
			}
			encoded, err := json.Marshal(confirm)
			if err != nil {
				t.Fatal(err)
			}
			var restored ControlCandidate
			if err = json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(f.stateDir, "state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "changed-solution" || scenario == "changed-reference" {
				name := "design.md"
				if scenario == "changed-reference" {
					name = "reference.md"
				}
				if err = os.WriteFile(filepath.Join(root, name), []byte("Changed after prepared confirmation\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = f.PrepareControl(confirmInput); err == nil {
					t.Fatal("pending package accepted changed selected material")
				}
				if _, _, err = f.SubmitControl(restored); err == nil {
					t.Fatal("saved confirmation accepted changed material bytes")
				}
				after, err := os.ReadFile(statePath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("rejected material changed authoritative bytes", err)
				}
				return
			}
			accepted, updated, err := f.SubmitControl(restored)
			if err != nil {
				t.Fatal(err)
			}
			state, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			current := state.State.CurrentIntakeConfirmation()
			if !reflect.DeepEqual(current, &change.Intake.IntakeCandidate) || !reflect.DeepEqual(updated.IntakeConfirmationReceipt, current) {
				t.Fatal("accepted current material bindings were not persisted or projected")
			}
			originalReceipt, _ := json.Marshal(state.State.IntakeReceipt)
			if !bytes.Equal(initialReceipt, originalReceipt) || !reflect.DeepEqual(state.State.IntakeConfirmationReceipt, &initial.IntakeCandidate) {
				t.Fatal("new confirmation rewrote the initial intake")
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(root, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "reference.md"), []byte("Post-confirmation external edit\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, show := range []func() (Run, error){reopened.Show, reopened.Status} {
				shown, err := show()
				if err != nil || !reflect.DeepEqual(shown.WorkerMaterials, updated.WorkerMaterials) || !reflect.DeepEqual(shown.IntakeConfirmationReceipt, current) {
					t.Fatal("read-only view lost original confirmed materials", err)
				}
			}
			next, err := reopened.Next()
			if err != nil || !reflect.DeepEqual(next, updated.Next) {
				t.Fatal("read-only next changed after source edit", err)
			}
			if _, err = reopened.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange}); err != nil {
				t.Fatal("source edit blocked requirement change preparation", err)
			}
			replay, _, err := reopened.SubmitControl(restored)
			if err != nil || !reflect.DeepEqual(accepted, replay) {
				t.Fatal("original confirmation replay lost its accepted identity", err)
			}
			repeated, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(after, repeated) {
				t.Fatal("replay changed persisted confirmation bytes", err)
			}
			durable, err := reopened.engine.Load()
			if err != nil || !reflect.DeepEqual(durable.State.CurrentIntakeConfirmation(), current) {
				t.Fatal("restart lost current selected bindings", err)
			}
		})
	}
}

func TestResetReconfirmsCurrentSelectedMaterials(t *testing.T) {
	root := t.TempDir()
	snapshotRepoCommits(t, root)
	for path, content := range map[string]string{"design.md": "# Selected design\nRetain the approved solution.\n", "reference.md": "# Original reference\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := vcs.ExecRunner{}
	for _, args := range [][]string{{"add", "design.md", "reference.md"}, {"commit", "-m", "Selected reset materials"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	base, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := PrepareIntake(root, "requirements.md", "design.md", []string{"reference.md"}, true)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "selected-reset", Route: "regular", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: initial.IntakeCandidate}, Admission: testAdmission(t, root, "selected-reset-runtime", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	initialState, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	initialReceipt, _ := json.Marshal(initialState.State.IntakeConfirmationReceipt)

	if err = os.WriteFile(filepath.Join(root, "reference.md"), []byte("# Current reference\nChanged before reset; preserve native target bytes.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeReset, err := PrepareIntake(root, "requirements.md", "design.md", []string{"reference.md"}, true)
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.PrepareControl(ControlInput{Request: protocol.ControlReset})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, protocol.ControlReset), Choice: "proceed", Reason: "Reset workflow facts and retain current native materials"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Reset == nil || !reflect.DeepEqual(decision.Reset.Intake, &beforeReset.IntakeCandidate) {
		t.Fatal("reset preview did not bind actual current selected materials")
	}
	_, run, err = f.SubmitControl(decision)
	if err != nil {
		t.Fatal(err)
	}

	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.State.ResetPending() && run.PendingRequirementAsk == nil {
		facts, _ := json.Marshal(map[string]any{"pendingActions": state.State.PendingActions, "pendingHostActions": state.State.PendingHostActions, "spawnReceipts": state.State.SpawnReceipts, "endedAttempts": state.State.EndedAttempts})
		t.Fatalf("reset did not reach its original material reconfirmation: next=%+v facts=%s", run.Next, facts)
	}
	for _, material := range beforeReset.Materials {
		data, err := os.ReadFile(filepath.Join(root, material.Path))
		if err != nil || string(data) != material.Content {
			t.Fatal("reset changed native material outside its declared preservation contract", material.Path, err)
		}
	}
	confirm, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm all three preserved current materials"})
	if err != nil {
		t.Fatal(err)
	}
	if len(confirm.Intake.Materials) != 3 || !reflect.DeepEqual(confirm.Intake.Materials, beforeReset.Materials) {
		t.Fatal("post-reset confirmation did not publish actual complete preserved bytes")
	}
	accepted, _, err := f.SubmitControl(confirm)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.State.ResetPending() || !reflect.DeepEqual(state.State.CurrentIntakeConfirmation(), &beforeReset.IntakeCandidate) {
		t.Fatal("reset confirmation lost current solution/reference bindings")
	}
	originalReceipt, _ := json.Marshal(state.State.IntakeConfirmationReceipt)
	if !bytes.Equal(initialReceipt, originalReceipt) || !reflect.DeepEqual(state.State.IntakeReceipt.Confirmation, initial.IntakeCandidate) {
		t.Fatal("reset rewrote the initial confirmation")
	}
	before, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := reopened.SubmitControl(confirm)
	if err != nil || !reflect.DeepEqual(accepted, replay) {
		t.Fatal("reset reconfirmation replay changed accepted facts", err)
	}
	after, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reset replay changed persisted state bytes", err)
	}
}
