//go:build !windows

package harness

// Live acceptance (plan §13.2: "K5 及发布门再使用真实宿主验收") — the
// composition's one real-host pass.  Skipped unless
// FORMAL_GATES_LIVE_ACCEPTANCE=1, because every worker turn is a real host
// call with real spend: the gate exists so no ordinary test run ever spends
// one.  The chain routes the product review through the real DSH binding and
// every other worker stage through the real ZCode binding, so both accepted
// adapters carry live work in one run.
//
// The drive answers process confirmations with their allowing choice, allows
// at most ONE repair round (a second ask, or any restart ask, aborts — the
// pass is bounded spend, not an open wallet), answers a held host interaction
// with its first offered option (the fixture requirement asks for no tools;
// the machine-answerable credential interactions the hosts raise are answered
// inside the adapters themselves), and asserts the run reaches
// HARNESS_COMPLETE with a walkable archive plan.  Every turn, provider and
// session identity the run spent is printed for the record.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestComposeLiveAcceptanceGreenChain(t *testing.T) {
	if os.Getenv("FORMAL_GATES_LIVE_ACCEPTANCE") != "1" {
		t.Skip("live acceptance: set FORMAL_GATES_LIVE_ACCEPTANCE=1 (real host calls, real spend)")
	}
	bin := supervisorBinary(t)
	// The run directory survives the test (FORMAL_GATES_LIVE_DIR or a
	// stamped /tmp path): a failed pass keeps its job artifacts — receipts,
	// native errors, outcome records — because that is the only place the
	// failure's cause lives.
	dir := os.Getenv("FORMAL_GATES_LIVE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("formal-gates-live-%d", time.Now().UnixNano()))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("live run directory: %s", dir)
	binding := func(call NativeCall) (map[string]any, error) {
		if call.TaskID == stageProductReview {
			return dshCaptureBinding()
		}
		return zcodeCaptureBinding()
	}
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:    dir,
		RunID:  "k5-live",
		Binary: bin,
		Requirement: "Acceptance fixture (live): the delivered state is the initial empty " +
			"workspace and no changes are required. Judge the empty state as adequate and " +
			"finish in one turn with status PASS and no findings.",
		Binding:     binding,
		RepairLimit: 1,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	repairs := 0
	for {
		state := c.Drive(ctx)
		switch state.Status {
		case StatusWaitingDecision:
			for _, question := range state.Pending {
				choice := ""
				for _, option := range question.Options {
					switch option {
					case "approve", "continue-repair", "restart-session":
						choice = option
					}
				}
				switch choice {
				case "":
					t.Fatalf("live ask without a known allowing choice: %+v", question)
				case "continue-repair":
					if repairs > 0 {
						choice = "abort" // bounded spend: one repair round at most
					} else {
						repairs++
					}
				case "restart-session":
					choice = "abort" // bounded spend: no replacement sessions
				}
				t.Logf("answering %s (%s) with %q", question.DecisionID, question.TaskID, choice)
				if _, err := c.Answer(ctx, question.DecisionID, choice); err != nil {
					t.Fatalf("answer %s: %v", question.DecisionID, err)
				}
			}
		case StatusWaitingInteraction:
			for _, held := range state.Held {
				option := firstHeldOption(held.Options)
				args := []string{"harness", "native-respond", "--job", held.JobDir, "--request", held.RequestID}
				if option != "" {
					args = append(args, "--option", option)
				} else {
					args = append(args, "--cancel")
				}
				t.Logf("answering held %s %s with %q", held.Method, held.RequestID, option)
				if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
					t.Fatalf("native-respond %s: %v: %s", held.RequestID, err, out)
				}
			}
		case StatusComplete:
			if state.Terminal != statusHarnessComplete {
				t.Fatalf("terminal = %q, want HARNESS_COMPLETE", state.Terminal)
			}
			// The destination must sit OUTSIDE the run directory — the
			// retention destination judgement refuses one inside it (the
			// live run proved that refusal on real state).
			plan, err := c.Archive(filepath.Join(os.TempDir(),
				fmt.Sprintf("formal-gates-live-archive-%d", time.Now().UnixNano())))
			if err != nil {
				t.Fatalf("archive plan: %v", err)
			}
			reportLiveSpend(t, dir)
			t.Logf("live chain complete: terminal=%s archive-status=%s files=%d repairs-allowed=%d",
				state.Terminal, plan.StatusName, plan.FileCount, repairs)
			return
		default:
			reportLiveSpend(t, dir)
			t.Fatalf("live chain stopped: status=%s err=%v repairs-allowed=%d", state.Status, state.Err, repairs)
		}
	}
}

// firstHeldOption picks the first option id a held request offers (the
// allowing one in every permission shape the hosts raise); "" when the host
// offered none, which the caller turns into a cancel.
func firstHeldOption(options any) string {
	for _, option := range listOf(options) {
		if id := getOf(option, "optionId"); id != nil {
			return textOf(id)
		}
		if id := getOf(option, "value"); id != nil {
			return textOf(id)
		}
	}
	return ""
}

// reportLiveSpend walks the job tree and prints every real turn this run
// spent — task, turn, provider, session identity, the supervisor's exit
// receipt, any native error record — plus the outcome records, so a failed
// pass carries its own diagnosis and the acceptance record carries its
// evidence of what the authorization paid for.
func reportLiveSpend(t *testing.T, dir string) {
	t.Helper()
	jobRoot := filepath.Join(dir, "jobs")
	var lines []string
	_ = filepath.WalkDir(jobRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || !strings.HasPrefix(entry.Name(), "turn-") {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		provider, identity := "", ""
		if source, rerr := readJSONFile(filepath.Join(path, "source.json")); rerr == nil {
			provider = textOf(getOf(source, "provider"))
			identity = textOf(getOf(source, "identity"))
		}
		line := fmt.Sprintf("%s provider=%s identity=%s", rel, provider, identity)
		if receipt, rerr := readJSONFile(filepath.Join(path, "exit.json")); rerr == nil {
			line += fmt.Sprintf(" exit=%v", getOf(receipt, "exit_code"))
			if e := textOf(getOf(receipt, "error")); e != "" {
				line += fmt.Sprintf(" receipt-error=%q", e)
			}
		} else {
			line += " exit=(no receipt)"
		}
		if e, rerr := readJSONFile(filepath.Join(path, "native-error.json")); rerr == nil {
			line += fmt.Sprintf(" native-error-kind=%s native-error=%q",
				textOf(getOf(e, "error_kind")), textOf(getOf(e, "error")))
		}
		lines = append(lines, line)
		return nil
	})
	sort.Strings(lines)
	t.Logf("live spend: %d supervised turns", len(lines))
	for _, line := range lines {
		t.Logf("  %s", line)
	}
	var outcomes []string
	_ = filepath.WalkDir(filepath.Join(dir, "outcomes"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var record outcomeRecord
		if json.Unmarshal(body, &record) != nil {
			return nil
		}
		status, summary := "", ""
		if record.Worker != nil {
			status, summary = record.Worker.Status, record.Worker.Summary
		}
		outcomes = append(outcomes, fmt.Sprintf("%s action=%s status=%s summary=%q",
			entry.Name(), record.Action, status, summary))
		return nil
	})
	sort.Strings(outcomes)
	t.Logf("live outcomes: %d", len(outcomes))
	for _, line := range outcomes {
		t.Logf("  %s", line)
	}
}
