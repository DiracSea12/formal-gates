package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/lifecycle"
)

// This opt-in run uses the installed public binary for every transition and real
// native workers for every issued action. The test operator makes the declared
// canary edit in the repository and explicitly adopts its Git commit to force an observable failure;
// it never manufactures a worker result, edits state, or patches a frozen tree.
func TestRegularInstalledFullNativeFailRepairToSeal(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_FULL") != "1" {
		t.Skip("opt-in installed full native canary")
	}
	authHome := os.Getenv("CODEX_HOME")
	if authHome == "" {
		authHome = filepath.Join(os.Getenv("HOME"), ".codex")
	}
	root, _ := cliWorkflowFixture(t)
	t.Setenv("CODEX_HOME", authHome)
	binary, installed := installHostCLI(t, lifecycle.ProviderCodex, root)
	cliGit(t, root, "rm", "whitebox_delivered_test.go")
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.codex/\n.formal-gates-resources/\n")
	document := cliStructuredRequirement("Create a Go module with a public command go run . at the repository root. It must exit 0, print exactly hello followed by one newline to stdout, and print nothing to stderr. No flags or other behavior are required. Go is available. A subprocess Go test of this public command is sufficient for the independent whitebox coverage. Keep the implementation and development plan proportional to this single output responsibility.")
	document = strings.Replace(document, "The test requirement is satisfied.", "From the repository root, a subprocess executes go run .; exit code is 0; stdout is byte-for-byte hello followed by exactly one newline; stderr is empty. A Go subprocess test asserting these observations is sufficient.", 1)
	document += "\n## Technical plan\n\nOne development Batch B1 owns go.mod and main.go. Subtask S1 creates the root Go module and minimal main command printing hello plus one newline to stdout with no stderr. Subtask S2 runs go build . and go run . and verifies exit, exact stdout, and empty stderr against REQ-001/AC-001. No dependent batch is needed. Independent blackbox design runs alongside development from the baseline; independent whitebox design owns main_test.go and the subprocess test after development completes. QA authoring and review are workflow responsibilities, not development batches. The only interface is process output; no persistent application state, options or configuration are required. Recovery is revert the one delivery commit and revalidate.\n"
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), document)
	mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), `{"capacity":4,"reasoningEffort":"medium"}`)
	cliGit(t, root, "add", ".gitignore", "AGENTS.md", "requirements.md")
	cliGit(t, root, "commit", "-m", "installed full canary requirement")
	const id = "native-full"
	nativeEnv := []string{"CODEX_HOME=" + authHome}
	var r facade.Run
	var calls []map[string]any
	defer func() {
		if p := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); p != "" {
			b, _ := json.MarshalIndent(map[string]any{"scope": "installed Git no-split full; all action results are real Codex; test operator adjudications and explicit external implementation edit are explicit", "binary": binary, "installed": installed, "root": root, "calls": calls, "run": r}, "", "  ")
			_ = os.WriteFile(p, b, 0600)
		}
	}()
	invoke := func(sub string, tail ...string) facade.Run {
		args := append([]string{"workflow", sub, "--root", root, "--run-id", id}, tail...)
		out := runInstalledCLI(t, binary, root, nativeEnv, "", args...)
		var v facade.Run
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	r = invoke("start", "--requirement", "requirements.md", "--vcs", "git")
	r = invoke("drive")
	submit := func(v protocol.RegularEvent) {
		v.ExpectedRevision = r.Revision
		e, err := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("native-canary-%d", r.Revision)), v)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(e)
		out := runInstalledCLI(t, binary, root, nativeEnv, string(b), "workflow", "submit", "--root", root, "--run-id", id)
		var response struct {
			Run facade.Run `json:"run"`
		}
		if err = json.Unmarshal([]byte(out), &response); err != nil {
			t.Fatal(err)
		}
		r = response.Run
	}
	choose := func(q protocol.RegularRequest) {
		submit(protocol.RegularEvent{Operation: "REQUEST", Request: &q})
		a := r.Regular.Ask
		if a == nil {
			t.Fatal("missing Ask")
		}
		submit(protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"}})
	}
	choose(protocol.RegularRequest{Action: "confirm-requirement"})
	choose(protocol.RegularRequest{Action: "mode", Mode: "regular"})
	// Execute independently issued workers together, retaining actual native start
	// and completion timestamps to verify the developer/blackbox overlap.
	execute := func(work []protocol.RegularExecution) {
		var wg sync.WaitGroup
		type result struct {
			x          protocol.RegularExecution
			out        []byte
			err        error
			start, end time.Time
		}
		done := make(chan result, len(work))
		for _, x := range work {
			wg.Add(1)
			go func(x protocol.RegularExecution) {
				defer wg.Done()
				cmd := exec.Command(binary, "workflow", "drive", "--root", root, "--run-id", id, "--action-id", x.Claim.ActionID)
				cmd.Dir = root
				cmd.Env = hostFilteredEnv(nativeEnv)
				start := time.Now().UTC()
				out, err := cmd.CombinedOutput()
				done <- result{x, out, err, start, time.Now().UTC()}
			}(x)
		}
		wg.Wait()
		close(done)
		for q := range done {
			calls = append(calls, map[string]any{"action": q.x.Claim.ActionID, "step": q.x.Claim.Task.Step, "start": q.start, "end": q.end})
			if q.err != nil {
				t.Fatalf("native %s: %v %s", q.x.Claim.Task.Step, q.err, q.out)
			}
		}
		r = invoke("show")
	}
	for _, step := range []string{"product-review", "start-readiness"} {
		current, err := nativeCanaryReview(step, func() facade.Run { return r }, func(x protocol.RegularExecution) { execute([]protocol.RegularExecution{x}) })
		if err != nil {
			t.Fatal(err)
		}
		submit(protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: step, ResultDigest: current.Digest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "The native review covered the sole observable stdout, exit and stderr requirement and complete one-responsibility plan"}})
	}
	choose(protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "Single repository and one output responsibility"})
	choose(protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "full"}})
	injected, sawFail, sawRepair, concurrent := false, false, false, false
	first := ""
	for turn := 0; turn < 36; turn++ {
		r = invoke("drive")
		if r.Regular.Terminal == "SEALED" {
			if !injected || !sawFail || !sawRepair || !concurrent || r.Regular.Verification.Seal.Candidate == first {
				t.Fatal("full native canary skipped required stages")
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = root
			out, err := cmd.Output()
			if err != nil || string(out) != "hello\n" {
				t.Fatalf("promoted output %q %v", out, err)
			}
			for _, x := range r.Executions {
				if x.Claimed && (!strings.Contains(x.Identity, "-") || strings.Contains(x.Identity, "fixture")) {
					t.Fatalf("non-native execution: %+v", x)
				}
			}
			t.Logf("installed full native Seal=%s actions=%d first=%s", r.Regular.Verification.Seal.Candidate, len(r.Executions), first)
			return
		}
		if r.Regular.Ask != nil {
			t.Fatalf("native canary needs additional user choice: %+v", r.Regular.Ask)
		}
		if !injected && r.Regular.Preparation != nil && r.Regular.Preparation.Candidate != nil {
			first = r.Regular.Preparation.Candidate.Candidate.Identity
			// All independent design/review and reuse work completed on the correct
			// candidate. Adopt a real external operator commit while keeping those
			// unchanged contracts; execution PASS is never retained for the changed code.
			cliGit(t, root, "merge", "--ff-only", first)
			p := filepath.Join(root, "main.go")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "hello") {
				t.Fatalf("cannot apply declared output edit: %s", b)
			}
			if err = os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "hello", "wrong")), 0644); err != nil {
				t.Fatal(err)
			}
			cliGit(t, root, "add", "main.go")
			cliGit(t, root, "commit", "-m", "canary operator changes output before verification")
			head := cliGit(t, root, "rev-parse", "HEAD")
			cmd := exec.Command("git", "diff", "--binary", r.Regular.Candidate, head, "--")
			cmd.Dir = root
			diff, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(diff)
			impact := map[string]string{"product-review": "unaffected", "start-readiness": "unaffected", "development": "unaffected", "blackbox-review": "unaffected", "whitebox-review": "unaffected", "blackbox": "affected", "whitebox": "affected"}
			for _, gate := range r.Regular.Route.Gates {
				impact[gate] = "affected"
			}
			choose(protocol.RegularRequest{Action: "adopt-external", External: &protocol.ExternalObservation{Identity: head, Lineage: r.Regular.BaseSnapshot, DiffDigest: fmt.Sprintf("sha256:%x", sum)}, Impact: impact, Keep: []string{"product-review", "start-readiness", "blackbox-review", "whitebox-review"}, Reason: "Canary adopts an explicit external implementation edit; native development and test contracts remain recorded, all changed candidate executions must rerun"})
			if r.Regular.Topology == "" {
				choose(protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "The same approved no-split plan applies to the external commit"})
			}
			injected = true
			continue
		}

		var work []protocol.RegularExecution
		dev, black := false, false
		for _, x := range r.Executions {
			if !x.Finished && !x.Claimed {
				work = append(work, x)
				dev = dev || x.Claim.Task.Step == "development-worker"
				black = black || x.Claim.Task.Step == "blackbox-design"
			}
		}
		if len(work) > 0 {
			concurrent = concurrent || (dev && black)
			execute(work)
			continue
		}
		if r.Regular.Preparation != nil {
			for mode, q := range r.Regular.Preparation.QA {
				for _, s := range q.Suggestions {
					if s.Decision == "" {
						choose(protocol.RegularRequest{Action: "qa-suggestion", Target: mode, FindingID: s.ID, Meaning: "deferred", Reason: "Optional suggestion deferred in the bounded canary"})
					}
				}
			}
		}
		v := r.Regular.Verification
		if v == nil {
			t.Fatalf("no pending native work: %+v errors=%+v", r.Next, r.Regular.PreparationErrors)
		}
		var w protocol.VerificationWave
		for _, x := range v.Waves {
			if x.ID == v.Current {
				w = x
			}
		}
		if first == "" {
			first = w.Candidate.Candidate.Identity
		}
		for _, repair := range v.Repairs {
			sawRepair = sawRepair || repair.Committed != ""
		}
		for target, result := range w.Results {
			sawFail = sawFail || result.Status == "FAIL"
			already := false
			for _, a := range v.Adjudications {
				if a.WaveID == w.ID && a.Target == target && a.ResultDigest == result.Digest {
					already = true
				}
			}
			if already || len(result.Findings) == 0 {
				continue
			}
			var cases []string
			for _, target := range w.Targets {
				cases = append(cases, target.Expected...)
			}
			var decisions []protocol.WaveFindingDecision
			for _, f := range result.Findings {
				decisions = append(decisions, protocol.WaveFindingDecision{FindingID: f.ID, Decision: "NEW", ProblemID: target + ":" + f.ID, Severity: f.Severity, Reason: "Canary operator compares the returned finding with the real deliberately wrong output", Evidence: f.NormalPath + "; " + f.Consequence, AffectedCases: cases})
			}
			submit(protocol.RegularEvent{Operation: "WAVE_ADJUDICATE", WaveAdjudication: &protocol.WaveAdjudication{WaveID: w.ID, Target: target, ResultDigest: result.Digest, Decisions: decisions}})
		}
	}
	t.Fatalf("full native canary did not converge: %+v", r.Next)
}

// The canary grants no extra attempts and has no independent retry budget.
// Only engine-issued, unclaimed actions may run; an engine Ask stops consumption.
// A rejected answer remains evidence, not a current review candidate.
func nativeCanaryReview(step string, read func() facade.Run, execute func(protocol.RegularExecution)) (protocol.RawReviewCandidate, error) {
	seen := map[string]bool{}
	task := ""
	for {
		r := read()
		if r.Regular.Ask != nil || r.Next.Kind == decision.KindAsk {
			return protocol.RawReviewCandidate{}, fmt.Errorf("native upstream needs additional user choice: ask=%+v next=%+v", r.Regular.Ask, r.Next)
		}
		series := r.Regular.Reviews[step]
		for _, raw := range series.RawCandidates {
			if series.CurrentDigest != "" && raw.Digest == series.CurrentDigest {
				if raw.Status != "PASS" || len(raw.Findings) > 0 {
					return raw, fmt.Errorf("native upstream needs substantive disposition: %+v", raw)
				}
				return raw, nil
			}
		}
		var pending []protocol.RegularExecution
		for _, x := range r.Executions {
			if string(x.Claim.Task.Step) == step && !x.Finished && !x.Claimed {
				pending = append(pending, x)
			}
		}
		if len(pending) != 1 {
			return protocol.RawReviewCandidate{}, fmt.Errorf("native upstream has no single issued attempt: step=%s next=%+v", step, r.Next)
		}
		x := pending[0]
		if seen[x.Claim.ActionID] || (task != "" && task != x.Claim.Task.String()) {
			return protocol.RawReviewCandidate{}, fmt.Errorf("native upstream would repeat an action or change TaskKey")
		}
		seen[x.Claim.ActionID] = true
		task = x.Claim.Task.String()
		execute(x)
	}
}

func TestNativeCanaryReviewConsumesOnlyIssuedFiniteRetry(t *testing.T) {
	for _, scenario := range []string{"invalid-then-valid", "all-invalid", "valid-finding"} {
		t.Run(scenario, func(t *testing.T) {
			root, _ := cliWorkflowFixture(t)
			binary, _ := installHostCLI(t, lifecycle.ProviderCodex, root)
			configureProtocolFixtureRuntime(t, root)
			mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("The public command prints hello."))
			cliGit(t, root, "add", "requirements.md")
			cliGit(t, root, "commit", "-m", "bounded retry fixture")
			call := func(sub string, extra ...string) facade.Run {
				out := runInstalledCLI(t, binary, root, nil, "", append([]string{"workflow", sub, "--root", root, "--run-id", "retry-fixture"}, extra...)...)
				var r facade.Run
				if err := json.Unmarshal([]byte(out), &r); err != nil {
					t.Fatal(err)
				}
				return r
			}
			r := call("start", "--requirement", "requirements.md", "--vcs", "git")
			r = call("drive")
			submit := func(v protocol.RegularEvent) {
				v.ExpectedRevision = r.Revision
				e, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("retry-fixture-%d", r.Revision)), v)
				data, _ := json.Marshal(e)
				out := runInstalledCLI(t, binary, root, nil, string(data), "workflow", "submit", "--root", root, "--run-id", "retry-fixture")
				var reply struct {
					Run facade.Run `json:"run"`
				}
				if err := json.Unmarshal([]byte(out), &reply); err != nil {
					t.Fatal(err)
				}
				r = reply.Run
			}
			for _, q := range []protocol.RegularRequest{{Action: "confirm-requirement"}, {Action: "mode", Mode: "regular"}} {
				submit(protocol.RegularEvent{Operation: "REQUEST", Request: &q})
				a := r.Regular.Ask
				submit(protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"}})
			}
			calls := 0
			firstTask := ""
			answer, err := nativeCanaryReview("product-review", func() facade.Run { return r }, func(x protocol.RegularExecution) {
				calls++
				if firstTask == "" {
					firstTask = x.Claim.Task.String()
				}
				if x.Claim.Task.String() != firstTask {
					t.Fatal("retry changed TaskKey")
				}
				submit(protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
				src := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "fixture-" + x.Claim.ActionID, Status: "RUNNING"}
				submit(protocol.RegularEvent{Operation: "SOURCE", Source: &src})
				raw := protocol.RawReviewCandidate{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, Action: "product-review", RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Complete: true, Message: "Explicit protocol fixture; not a native review"}
				if scenario == "all-invalid" || (scenario == "invalid-then-valid" && calls == 1) {
					raw.RequirementRevision = raw.RequirementRevision[:48] + raw.RequirementRevision[49:]
				}
				if scenario == "valid-finding" {
					raw.Findings = []protocol.ReviewFinding{{ID: "optional", Severity: "P3", Category: "implementation-detail", Requirement: "REQ-001", Problem: "optional detail", NormalPath: "read the simple requirement", Consequence: "no acceptance failure", SeverityRationale: "advisory only", Remedy: "optional clarification"}}
				}
				submit(protocol.RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
				src.Status = "UNAVAILABLE"
				src.Reason = "Protocol fixture; no native session or transcript"
				submit(protocol.RegularEvent{Operation: "SOURCE", Source: &src})
				if scenario == "invalid-then-valid" && calls == 1 && (r.Regular.Reviews["product-review"].CurrentDigest != "" || len(r.Regular.RejectedResults) != 1) {
					t.Fatal("first wrong binding was not preserved and refused")
				}
			})
			switch scenario {
			case "invalid-then-valid":
				if err != nil || calls != 2 || answer.Status != "PASS" || len(r.Regular.RejectedResults) != 1 {
					t.Fatalf("retry: calls=%d answer=%+v error=%v", calls, answer, err)
				}
			case "all-invalid":
				if err == nil || calls != 3 || len(r.Regular.RejectedResults) != 3 || r.Next.Kind != decision.KindAsk {
					t.Fatalf("finite Ask: calls=%d next=%+v error=%v", calls, r.Next, err)
				}
			case "valid-finding":
				if err == nil || calls != 1 {
					t.Fatalf("finding silently disposed: calls=%d error=%v", calls, err)
				}
			}
			t.Logf("scenario=%s issued attempts executed=%d, TaskKey=%s, rejection evidence retained=%d", scenario, calls, firstTask, len(r.Regular.RejectedResults))
		})
	}
}
