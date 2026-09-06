package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

// The pre-development design may use the merged empty mode. Once development
// starts, blackbox review is dispatched explicitly and must retain that design's
// common instructions without asking the designer to register them again.
func TestQAEfficiencyFastDesignSharedStepsFollowBlackboxStorage(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	id := "fast-design-shared"
	base := []string{"--root", root, "--package-root", pkg, "--run-id", id}
	call := func(command string, extra ...string) string {
		return runCLI(t, append(append([]string{"workflow", command}, base...), extra...)...)
	}
	state := decodeSemanticState(t, call("start", "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	call("requirement", "--confirmed")
	for _, action := range []string{"product-review", "start-readiness"} {
		dispatch := prepareSemanticCLI(t, root, pkg, id, action, false, "")
		recordSemanticCLI(t, root, pkg, id, action, dispatch, "PASS", "", "")
	}
	call("slicing", "--decision", "no-split", "--note", "single public workflow")
	call("route", "--mode", "custom", "--gate", "blackbox")
	design := cliPrepareAction(t, root, pkg, id, "qa-design")
	const procedure = "copy clean fixture and capture stdout separately"
	state = decodeSemanticState(t, call("qa-design", "--dispatch", design,
		"--shared-step", "fixture", "--shared-procedure", procedure,
		"--case", "public read", "--mode", "blackbox", "--use-step", "fixture",
		"--procedure", "call workflow show", "--oracle", "run is ACTIVE", "--ac", "AC-001"))
	qaTree := filepath.Join(t.TempDir(), "qa")
	cliGit(t, root, "worktree", "add", "--detach", qaTree, state.BaseSnapshot)
	call("qa-worktree", "--worktree", qaTree)
	development := cliPrepareAction(t, root, pkg, id, "development-worker")
	prompt := call("prepare-action", "--action", "qa-review", "--mode", "blackbox")
	if strings.Count(prompt, procedure) != 1 || !strings.Contains(prompt, "shared steps: fixture") {
		t.Fatal("blackbox review lost the merged design's common procedure or reference")
	}
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", id))
	review := cliOpenDispatch(state, "action", "qa-review")
	call("claim-dispatch", "--dispatch", review, "--reviewer", "fast-design-reviewer")
	decisions := []string{"--dispatch", review, "--case", "CASE-001", "--outcome", "PASS",
		"--source-decision", "REQ-001=PASS", "--point-decision", "AC-001=PASS", "--case-decision", "CASE-001=PASS"}
	missing := failingCLI(t, append(append([]string{"workflow", "qa-review"}, base...), decisions...)...)
	if !strings.Contains(missing, "all 1 pending shared steps") {
		t.Fatalf("missing shared decision was not checked against the merged definition: %s", missing)
	}
	state = decodeSemanticState(t, call("qa-review", append(decisions,
		"--shared-step", "fixture", "--shared-outcome", "PASS", "--shared-reason", "common preparation is executable")...))
	steps := state.QACaseSets[""].SharedSteps
	if len(steps) != 1 || steps[0].ReviewStatus != "PASS" || len(state.QACaseSets["blackbox"].SharedSteps) != 0 ||
		state.QACaseSets["blackbox"].Status != "FROZEN" || state.QACasesByMode[""][0].ReviewStatus != "PASS" {
		t.Fatalf("shared decision and approved case did not retain their original storage: %+v", state.QACaseSets)
	}
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "public workflow delivery\n")
	cliGit(t, root, "add", "delivery.txt")
	cliGit(t, root, "commit", "-m", "fixture delivery")
	call("snapshot", "--dispatch", development)
	prompt = call("prepare-action", "--action", "qa-execution", "--mode", "blackbox")
	if strings.Count(prompt, procedure) != 1 || !strings.Contains(prompt, "shared steps: fixture") {
		t.Fatal("blackbox execution lost the approved common procedure or reference")
	}
}

// This installed-process regression constructs only the expensive accepted
// intake precondition. QA design/review/checkpoint/disposition/candidate/Seal
// operations below are real public calls. Lifecycle events are fixture data,
// never evidence of an actual external reviewer dispatch. Setting the explicitly
// opt-in FORMAL_GATES_QA_CODEX_RUNNER uses real Codex calls for the cost comparison.
func TestQAEfficiencyInstalledPublicOperations(t *testing.T) {
	root, _ := cliWorkflowFixture(t)
	evidence := os.Getenv("FORMAL_GATES_QA_EVIDENCE_DIR")
	if evidence != "" {
		destination := filepath.Join(evidence, "project")
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
		root = destination
		t.Setenv("HOME", filepath.Join(evidence, "home"))
		t.Setenv("USERPROFILE", filepath.Join(evidence, "home"))
	}
	provider, guidance := lifecycle.ProviderClaude, "CLAUDE.md"
	runner := os.Getenv("FORMAL_GATES_QA_CODEX_RUNNER")
	if runner != "" {
		provider, guidance = lifecycle.ProviderCodex, "AGENTS.md"
	}
	binary, pkg := installHostCLI(t, provider, root)
	// The installed project guidance is preparation, not part of the target diff.
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.claude/\n.codex/\n.formal-gates-resources/\n")
	cliGit(t, root, "add", ".gitignore", guidance)
	cliGit(t, root, "commit", "-m", "installed fixture preparation")
	runID := "qa-efficiency-installed"
	log := func(args ...string) string {
		started := time.Now()
		output := runInstalledCLI(t, binary, root, nil, "", args...)
		if evidence != "" {
			data, _ := json.Marshal(map[string]any{"command": append([]string{binary}, args...), "elapsedSeconds": time.Since(started).Seconds(), "output": output})
			file, err := os.OpenFile(filepath.Join(evidence, "commands.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = file.Write(append(data, '\n'))
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		return output
	}
	log("workflow", "start", "--root", root, "--package-root", pkg, "--run-id", runID, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git", "--split", "no")
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	state.RequirementConfirmed = true
	state.RequirementGuarantee = nil
	state.Actions["requirements-clarification"] = validate.ActionResult{Status: "PASS"}
	state.Actions["product-review"] = validate.ActionResult{Status: "PASS"}
	state.Actions["start-readiness"] = validate.ActionResult{Status: "PASS"}
	state.Slicing = &validate.Slicing{Decision: "no-split", Note: "fixture's single public QA scenario"}
	state.RouteMode = "custom"
	state.SelectedGates = []string{"blackbox"}
	if err := validate.SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	base := []string{"--root", root, "--package-root", pkg, "--run-id", runID}
	call := func(command string, extra ...string) string {
		return log(append(append([]string{"workflow", command}, base...), extra...)...)
	}
	prepare := func(action string) string {
		call("prepare-action", "--action", action)
		state = installedWorkflowState(t, binary, root, runID)
		id := cliOpenDispatch(state, "action", action)
		identity := "fixture-" + id
		transcript := ""
		if runner != "" && action != "development-worker" {
			// Opt-in live comparison: the runner returns the actual Codex thread and
			// rollout. Ordinary test runs keep synthetic lifecycle fixtures and never
			// spend model tokens. The model executes the observed public commands.
			cmd := exec.Command("python3", runner, "--action", action, "--dispatch", id, "--root", root, "--binary", binary, "--evidence", evidence)
			cmd.Env = os.Environ()
			output, runErr := cmd.CombinedOutput()
			if runErr != nil {
				t.Fatalf("real Codex %s: %v %s", action, runErr, output)
			}
			var result struct {
				Identity   string `json:"identity"`
				Transcript string `json:"transcript"`
			}
			if err := json.Unmarshal(output, &result); err != nil || result.Identity == "" || result.Transcript == "" {
				t.Fatalf("Codex source: %v %s", err, output)
			}
			identity, transcript = result.Identity, result.Transcript
		}
		payload := fmt.Sprintf(`{"cwd":%q,"agent_id":%q,"agent_transcript_path":%q}`, root, identity, transcript)
		runInstalledCLI(t, binary, root, nil, payload, "lifecycle", "capture", "--provider", provider, "--event", "SubagentStart")
		call("claim-dispatch", "--dispatch", id, "--reviewer", identity)
		if runner == "" || action != "qa-execution" || os.Getenv("FORMAL_GATES_QA_BASELINE") == "1" {
			runInstalledCLI(t, binary, root, nil, payload, "lifecycle", "capture", "--provider", provider, "--event", "SubagentStop")
		}
		return id
	}
	design := prepare("qa-design")
	call("qa-design", "--dispatch", design, "--shared-step", "fixture", "--shared-procedure", "copy pristine input and capture stdout separately", "--case", "public first", "--mode", "blackbox", "--use-step", "fixture", "--procedure", "call first public operation", "--oracle", "first stdout equals expected", "--case", "public second", "--mode", "blackbox", "--use-step", "fixture", "--procedure", "call second public operation", "--oracle", "second stdout equals expected")
	review := prepare("qa-review")
	call("qa-review", "--dispatch", review, "--shared-step", "fixture", "--shared-outcome", "PASS", "--shared-reason", "fixture and capture are executable", "--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS", "--finding", "optional wording improvement", "--severity", "P3")
	state = installedWorkflowState(t, binary, root, runID)
	call("qa-suggestion", "--suggestion-id", state.QACaseSets[""].Suggestions[0].ID, "--decision", "DEFER", "--reason", "the exact oracle and public path are already sufficient")
	dev := prepare("development-worker")
	for _, name := range []string{"first.txt", "second.txt"} {
		mustWriteCLI(t, filepath.Join(root, name), name+"\n")
		cliGit(t, root, "add", name)
		cliGit(t, root, "commit", "-m", name)
	}
	call("snapshot", "--dispatch", dev)
	call("prepare-candidate", "--squash-message", "final fixture candidate")
	final := cliGit(t, root, "rev-parse", "HEAD")
	observe := func(id string) []string {
		// The fixed comparison repeats this real read-only public call when an
		// older runner loses progress. Its oracle checks the returned identity
		// and ACTIVE state independently of the evidence-recording operation.
		output := ""
		if runner == "" {
			output = log("workflow", "show", "--root", root, "--run-id", runID)
		} else {
			data, err := os.ReadFile(filepath.Join(evidence, "model-observations.json"))
			if err != nil {
				t.Fatal(err)
			}
			var observations map[string]json.RawMessage
			if err := json.Unmarshal(data, &observations); err != nil {
				t.Fatal(err)
			}
			output = string(observations[id])
		}
		var observed validate.RunState
		if err := json.Unmarshal([]byte(output), &observed); err != nil || observed.RunID != runID || observed.Status != "ACTIVE" {
			t.Fatalf("public observation %s failed: %v %s", id, err, output)
		}
		return []string{"--case-result", id, "--outcome", "PASS", "--procedure", "workflow show --root " + root + " --run-id " + runID, "--observation", "runId=" + observed.RunID + " status=" + observed.Status, "--oracle-result", "run identity and ACTIVE state match the expected fixture"}
	}
	execution := prepare("qa-execution")
	call("qa-execution", append([]string{"--dispatch", execution, "--checkpoint", "--tool", "known-script-v1", "--environment", "clean-fixture-v1"}, observe("CASE-001")...)...)
	state = installedWorkflowState(t, binary, root, runID)
	if state.QAExecutionByMode[""].Status != "PENDING" || len(state.QAExecutionByMode[""].PendingCases) != 1 {
		t.Fatal("installed checkpoint became complete")
	}
	if runner != "" {
		// Continue the same native Codex thread only after the first checkpoint.
		// Its rollout then contains both real observation turns, with no repeated row.
		cmd := exec.Command("python3", runner, "--action", "qa-execution", "--dispatch", execution, "--root", root, "--binary", binary, "--evidence", evidence, "--continue-action")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Codex continuation: %v %s", err, output)
		}
		var completed struct {
			Identity   string `json:"identity"`
			Transcript string `json:"transcript"`
		}
		if err := json.Unmarshal(output, &completed); err != nil {
			t.Fatal(err)
		}
		payload := fmt.Sprintf(`{"cwd":%q,"thread_id":%q,"transcript_path":%q}`, root, completed.Identity, completed.Transcript)
		runInstalledCLI(t, binary, root, nil, payload, "lifecycle", "capture", "--provider", provider, "--event", "SubagentStop")
	}
	call("qa-execution", append([]string{"--dispatch", execution, "--tool", "known-script-v1", "--environment", "clean-fixture-v1"}, observe("CASE-002")...)...)
	state = installedWorkflowState(t, binary, root, runID)
	if len(state.QAExecutionByMode[""].ReuseReasons) != 1 {
		t.Fatal("installed continuation reran or lost first row")
	}
	result := call("seal")
	if cliGit(t, root, "rev-parse", "HEAD") != final || !strings.Contains(result, final) {
		t.Fatal("installed Seal changed candidate")
	}
	if evidence != "" {
		data, _ := json.MarshalIndent(map[string]string{"project": root, "home": os.Getenv("HOME"), "binary": binary, "package": pkg, "candidate": final, "stateNamespace": filepath.Join(root, ".gates"), "registry": state.AdmissionRegistry}, "", "  ")
		if runner != "" {
			if state.Cost == nil {
				t.Fatal("real Codex costs missing")
			}
			for _, entry := range state.Cost.Dispatches {
				if entry.Target == "development-worker" {
					continue
				}
				if entry.Source != "transcript" || entry.TotalInputTokens <= 0 || entry.OutputTokens <= 0 {
					t.Fatalf("incomplete model cost: %+v", entry)
				}
				// Reconcile the projection against actual host request counters, rather
				// than trusting the production parser's own aggregate as its oracle.
				raw, err := os.ReadFile(entry.TranscriptPath)
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				var input, cached, output int64
				for _, line := range strings.Split(string(raw), "\n") {
					var record struct {
						Type    string `json:"type"`
						Payload struct {
							ResponseID string `json:"response_id"`
							Usage      struct {
								Input  int64 `json:"input_tokens"`
								Cached int64 `json:"cached_input_tokens"`
								Output int64 `json:"output_tokens"`
							} `json:"usage"`
						} `json:"payload"`
					}
					if json.Unmarshal([]byte(line), &record) != nil || record.Type != "token_usage_record" || seen[record.Payload.ResponseID] {
						continue
					}
					seen[record.Payload.ResponseID] = true
					input += record.Payload.Usage.Input
					cached += record.Payload.Usage.Cached
					output += record.Payload.Usage.Output
				}
				if entry.TotalInputTokens != input || entry.InputCacheHitTokens != cached || entry.InputCacheMissTokens != input-cached || entry.OutputTokens != output || entry.Timing == nil {
					t.Fatalf("Codex projection disagrees with native requests: %+v raw=%d/%d/%d", entry, input, cached, output)
				}
			}
		}
		if err := os.WriteFile(filepath.Join(evidence, "installation.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("actual installed binary=%s package=%s project=%s final=%s", binary, pkg, root, final)
}
