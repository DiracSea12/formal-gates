package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func TestSubagentTranscriptRequiresParentInputAndCompletion(t *testing.T) {
	meta := `{"type":"session_meta","payload":{"id":"child","parent_thread_id":"parent","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}}}`
	started := `{"type":"event_msg","payload":{"type":"task_started"}}`
	input := `{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"the-action"}]}}`
	done := `{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"{\"status\":\"PASS\"}"}}`
	for _, tc := range []struct {
		name, body, parent, marker, want string
		complete                         bool
	}{
		{"completed", meta + "\n" + started + "\n" + input + "\n" + done, "parent", "the-action", "", true},
		{"running-bind", meta + "\n" + started + "\n" + input, "parent", "the-action", "", false},
		{"running-collect", meta + "\n" + started + "\n" + input, "parent", "the-action", "WAIT", true},
		{"empty-final-is-complete", meta + "\n" + started + "\n" + input + "\n" + `{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":""}}`, "parent", "the-action", "", true},
		{"wrong-parent", meta + "\n" + started + "\n" + input + "\n" + done, "other", "the-action", "INTENT_MISMATCH", true},
		{"old-result", meta + "\n" + started + "\n" + input + "\n" + done, "parent", "next-action", "INTENT_MISMATCH", true},
		{"later-turn-running", meta + "\n" + started + "\n" + input + "\n" + done + "\n" + started, "parent", "the-action", "WAIT", true},
		{"unrelated-turn", meta + "\n" + started + "\n" + input + "\n" + done + "\n" + started + "\n" + done, "parent", "the-action", "WAIT", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "native.jsonl")
			if err := os.WriteFile(p, []byte(tc.body+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			o, err := readSubagentTranscript(file, p, "child", tc.parent, tc.marker, tc.complete)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			if tc.name == "completed" && o.Answer != `{"status":"PASS"}` {
				t.Fatalf("raw answer changed: %q", o.Answer)
			}
		})
	}
}

func TestSubagentPrepareClaimsOnceAndDoesNotSpawn(t *testing.T) {
	f := readyRegularFacade(t)
	r, _ := f.Show()
	var action string
	for id := range r.Executions {
		action = id
	}
	before := r.Revision
	if _, err := f.PrepareSubagent("unissued", "parent", false); err == nil {
		t.Fatal("unissued action accepted")
	}
	if _, err := f.PrepareSubagent(action, "", false); err == nil {
		t.Fatal("missing parent accepted")
	}
	r, _ = f.Show()
	if r.Revision != before {
		t.Fatal("invalid dispatch mutated state")
	}
	r, err := f.PrepareSubagent(action, "parent", false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subagent == nil || r.Subagent.Operation != "spawn" || !r.Executions[action].Claimed || r.Executions[action].Identity != "" {
		t.Fatalf("bad handoff: %+v", r.Subagent)
	}
	if _, err := os.Stat(filepath.Join(f.codexJob(action), "process.json")); !os.IsNotExist(err) {
		t.Fatal("handoff started legacy process")
	}
	if !strings.Contains(r.Subagent.Prompt, r.Subagent.Marker) {
		t.Fatal("missing native input binding")
	}
	revision := r.Revision
	r, err = f.PrepareSubagent(action, "parent", false)
	if err != nil || r.Subagent.Operation != "observe" || r.Revision != revision {
		t.Fatalf("replay authorized second spawn: %v", err)
	}
	if _, err = f.PrepareSubagent(action, "other-parent", false); err == nil {
		t.Fatal("parent replacement accepted")
	}
	if _, err = f.CollectSubagent(action); err == nil {
		t.Fatal("unbound result accepted")
	}
}

func TestSubagentBlackboxPreservesAuthorizedWorkspaceReads(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	x := facadeExecution(t, f, "blackbox-design")
	r, err := f.PrepareSubagent(x.Claim.ActionID, "fixture-parent", false)
	if err != nil {
		t.Fatal(err)
	}
	prompt := r.Subagent.Prompt
	if strings.Contains(prompt, "Review roles must use only their supplied text") ||
		!strings.Contains(prompt, "QA access to its isolated workspace") ||
		!strings.Contains(prompt, "blackbox must not read development code or development evidence") {
		t.Fatal("native transport changed QA input permissions")
	}
}

func TestSubagentContinuationKeepsIdentityAndRejectsOldTurn(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	r, err := f.PrepareSubagent(x.Claim.ActionID, "fixture-parent", false)
	if err != nil {
		t.Fatal(err)
	}
	d := *r.Subagent
	dir := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "01", "01")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-fixture-child.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	write := func(kind string, payload any) {
		t.Helper()
		if err := enc.Encode(map[string]any{"type": kind, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	write("session_meta", map[string]any{"id": "fixture-child", "parent_thread_id": "fixture-parent", "source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": "fixture-parent"}}}})
	write("event_msg", map[string]any{"type": "task_started"})
	write("response_item", map[string]any{"role": "user", "content": []any{map[string]string{"text": d.Marker}}})
	write("event_msg", map[string]any{"type": "token_count", "info": map[string]any{"last_token_usage": map[string]int{"input_tokens": 10, "output_tokens": 2}}})
	result := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "NATIVE_IDENTITY", RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "CONTINUE", Message: "fixture unfinished implementation"}
	answer, _ := json.Marshal(result)
	write("event_msg", map[string]any{"type": "task_complete", "last_agent_message": string(answer)})
	if _, err = f.BindSubagent(x.Claim.ActionID, "fixture-child"); err != nil {
		t.Fatal(err)
	}
	r, err = f.CollectSubagent(x.Claim.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Executions[x.Claim.ActionID].PreparationResult.Status != "CONTINUE" {
		t.Fatal("native result changed")
	}
	revision := r.Revision
	r, err = f.CollectSubagent(x.Claim.ActionID)
	if err != nil || r.Revision != revision {
		t.Fatalf("duplicate collection mutated state: %v", err)
	}
	next := facadeExecution(t, f, "development-worker")
	if next.Identity != "fixture-child" || next.Claim.Operation != "resume" {
		t.Fatal("engine did not retain native identity")
	}
	if _, err = f.PrepareSubagent(next.Claim.ActionID, "fixture-parent", false); err == nil {
		t.Fatal("continuation spawned fresh child")
	}
	r, err = f.PrepareSubagent(next.Claim.ActionID, "fixture-parent", true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subagent.Operation != "resume" || r.Subagent.Identity != "fixture-child" {
		t.Fatalf("invalid continuation: %+v", r.Subagent)
	}
	if _, err = f.CollectSubagent(next.Claim.ActionID); err == nil {
		t.Fatal("previous turn satisfied new action")
	}
	write("event_msg", map[string]any{"type": "task_started"})
	write("response_item", map[string]any{"role": "user", "content": []any{map[string]string{"text": r.Subagent.Marker}}})
	if _, err = f.BindSubagent(next.Claim.ActionID, "fixture-child"); err != nil {
		t.Fatal(err)
	}
	write("event_msg", map[string]any{"type": "turn_aborted"})
	r, err = f.CollectSubagent(next.Claim.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Executions[next.Claim.ActionID].PreparationResult.Status != "RUNTIME_ERROR" {
		t.Fatal("interruption became a PASS or retained old result")
	}
}

// One opt-in small real review. The parent native tool executes packet.prompt,
// writes only the returned child identity, and this adapter reads the real
// result and usage. No parent-supplied PASS or fabricated transcript is used.
func TestSubagentLiveReview(t *testing.T) {
	dir := os.Getenv("FORMAL_GATES_SUBAGENT_LIVE")
	if dir == "" {
		t.Skip("opt-in desktop parent orchestration")
	}
	parent := os.Getenv("CODEX_THREAD_ID")
	f := readyRegularFacade(t)
	r, _ := f.Show()
	var action string
	for id := range r.Executions {
		action = id
	}
	r, err := f.PrepareSubagent(action, parent, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSONAtomic(filepath.Join(dir, "dispatch.json"), r.Subagent); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		identity, err := os.ReadFile(filepath.Join(dir, "identity.txt"))
		if os.IsNotExist(err) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.BindSubagent(action, strings.TrimSpace(string(identity)))
		if err != nil && strings.Contains(err.Error(), "INTENT_MISMATCH") {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		r, err = f.CollectSubagent(action)
		if err != nil && strings.Contains(err.Error(), "WAIT:") {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		x := r.Executions[action]
		if !x.Finished || x.Source == nil || x.Source.Status != "STOPPED" || r.Cost.TotalInputTokens == 0 {
			t.Fatal("missing real completion/usage")
		}
		raw, _ := json.Marshal(map[string]any{"identity": x.Identity, "source": x.Source, "inputTokens": r.Cost.TotalInputTokens, "outputTokens": r.Cost.OutputTokens, "review": r.Regular.Reviews["product-review"]})
		if err = os.WriteFile(filepath.Join(dir, "result.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("native child did not complete; no substitute result accepted")
}
