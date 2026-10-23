package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	codexChildID  = "01900000-0000-7000-8000-000000000001"
	codexParentID = "01900000-0000-7000-8000-000000000002"
)

func codexRecord(kind string, payload any) string {
	raw, err := json.Marshal(map[string]any{"type": kind, "payload": payload})
	if err != nil {
		panic(err)
	}
	return string(raw) + "\n"
}

func codexIdentity(id, parent string) string {
	var source any = "vscode"
	if parent != "" {
		source = map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "agent_path": "/root/worker"}}}
	}
	return codexRecord("session_meta", map[string]any{"id": id, "session_id": codexParentID, "model_provider": "openai", "originator": "Codex Desktop", "cli_version": "0.163.0", "source": source})
}

func codexStarted(id string) string {
	return codexRecord("event_msg", map[string]any{"type": "task_started", "turn_id": id})
}

func codexContext(id, model, effort string) string {
	return codexRecord("turn_context", map[string]any{"turn_id": id, "model": model, "effort": effort, "approval_policy": "on-request", "approvals_reviewer": "auto_review", "sandbox_policy": map[string]any{"type": "workspace-write"}})
}

func codexResponseUsage(thread, turn, response string, input, cached, output int) string {
	return codexRecord("token_usage_record", map[string]any{"thread_id": thread, "turn_id": turn, "response_id": response, "usage": map[string]int{"input_tokens": input, "cached_input_tokens": cached, "output_tokens": output}, "thread_token_usage": map[string]int{"input_tokens": 99999, "output_tokens": 99999}})
}

func codexTool(kind, call, turn string, fields map[string]any) string {
	fields["type"], fields["call_id"] = kind, call
	fields["internal_chat_message_metadata_passthrough"] = map[string]string{"turn_id": turn}
	return codexRecord("response_item", fields)
}

func TestCodexThreadExcludesInheritedIdentityContextAndUsage(t *testing.T) {
	usage := codexResponseUsage(codexChildID, "child-turn", "child-response", 70, 40, 9)
	raw := codexIdentity(codexChildID, codexParentID) + codexIdentity(codexParentID, "") +
		codexContext("parent-turn", "parent-model", "low") + codexResponseUsage(codexParentID, "parent-turn", "parent-response", 500, 100, 80) +
		codexStarted("child-turn") + codexContext("child-turn", "child-model", "high") + usage + usage +
		codexRecord("event_msg", map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": map[string]int{"input_tokens": 88888}}}) +
		codexRecord("event_msg", map[string]string{"type": "task_complete", "turn_id": "child-turn"}) +
		codexStarted("followup-turn") + codexContext("followup-turn", "followup-model", "medium") +
		codexResponseUsage(codexChildID, "followup-turn", "followup-response", 32, 5, 3)
	thread, err := parseCodexThread([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.ValidateIdentity(codexChildID, codexParentID); err != nil {
		t.Fatal(err)
	}
	if !thread.SourceSubagent || thread.AgentPath != "/root/worker" || thread.ModelProvider != "openai" || thread.Originator != "Codex Desktop" || thread.CLIVersion != "0.163.0" {
		t.Fatalf("native identity lost: %+v", thread)
	}
	if len(thread.Turns) != 2 {
		t.Fatal(thread.Turns)
	}
	first, current := thread.Turns[0], thread.Turns[1]
	if first.State != "completed" || first.Model != "child-model" || first.Effort != "high" || first.ApprovalPolicy != "on-request" || first.ApprovalsReviewer != "auto_review" || string(first.SandboxPolicy) != `{"type":"workspace-write"}` || first.Usage != (Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 30, OutputTokens: 9}) || !first.UsageAvailable {
		t.Fatalf("first local turn: %+v", first)
	}
	if current.ID != "followup-turn" || current.State != "running" || current.Model != "followup-model" || current.Effort != "medium" || current.Usage != (Usage{InputCacheHitTokens: 5, InputCacheMissTokens: 27, OutputTokens: 3}) {
		t.Fatalf("current local turn: %+v", current)
	}
	repeated, err := parseCodexThread([]byte(raw))
	if err != nil || !reflect.DeepEqual(thread, repeated) {
		t.Fatal("repeated observation changed native facts", err)
	}
	if err := thread.ValidateIdentity(codexChildID, "01900000-0000-7000-8000-000000000003"); err == nil {
		t.Fatal("accepted wrong parent")
	}
	if err := thread.ValidateIdentity(codexParentID, ""); err == nil {
		t.Fatal("accepted inherited parent as entity")
	}
}

func TestCodexThreadPreservesNativeToolFactsWithoutInferringSuccess(t *testing.T) {
	raw := codexIdentity(codexChildID, codexParentID) + codexStarted("turn") + codexContext("turn", "model", "high") +
		codexTool("function_call", "message", "turn", map[string]any{"namespace": "collaboration", "name": "send_message", "arguments": `{"target":"/root","message":"status"}`}) +
		codexTool("function_call_output", "message", "turn", map[string]any{"output": ""}) +
		codexTool("custom_tool_call", "wrapper", "turn", map[string]any{"name": "exec", "input": `await tools.mcp__formal_gates__submit_semantics({semantic:{status:"PASS"}})`}) +
		codexTool("custom_tool_call_output", "wrapper", "turn", map[string]any{"output": []any{map[string]any{"type": "input_text", "text": `{"isError":true,"error":"rejected"}`}}}) +
		codexTool("function_call", "pending", "turn", map[string]any{"namespace": "collaboration", "name": "send_message", "arguments": `{}`})
	thread, err := parseCodexThread([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	tools := thread.Turns[0].Tools
	if len(tools) != 3 || tools[0].Name != "send_message" || tools[0].Namespace != "collaboration" || string(tools[0].Output) != `""` || tools[1].Name != "exec" || tools[1].Type != "custom_tool_call" || tools[1].Input == "" || !strings.Contains(string(tools[1].Output), "rejected") || tools[2].Output != nil {
		t.Fatalf("native calls/results changed: %+v", tools)
	}
	if thread.Turns[0].UsageAvailable || thread.Turns[0].State != "running" {
		t.Fatal("unfinished tool or missing metering was treated as completion")
	}
}

func TestCodexThreadInterruptedTurnAndFollowupShareOriginalIdentity(t *testing.T) {
	raw := codexIdentity(codexChildID, codexParentID) + codexStarted("interrupted-turn") + codexContext("interrupted-turn", "model", "high") +
		codexResponseUsage(codexChildID, "interrupted-turn", "interrupted-response", 9, 3, 2) +
		codexRecord("event_msg", map[string]string{"type": "turn_aborted", "turn_id": "interrupted-turn", "reason": "interrupted"}) +
		codexStarted("followup-turn") + codexContext("followup-turn", "model", "high") +
		codexRecord("event_msg", map[string]string{"type": "turn_aborted", "turn_id": "followup-turn", "reason": "other"})
	thread, err := parseCodexThread([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.ValidateIdentity(codexChildID, codexParentID); err != nil {
		t.Fatal(err)
	}
	if len(thread.Turns) != 2 || thread.Turns[0].State != "interrupted" || thread.Turns[0].Reason != "interrupted" || thread.Turns[0].Usage.OutputTokens != 2 || thread.Turns[1].State != "aborted" || thread.Turns[1].UsageAvailable {
		t.Fatal("interruption or followup replaced native evidence", thread.Turns)
	}
}

func TestCodexTranscriptMetersLocalSubagentResponsesThroughOriginalParser(t *testing.T) {
	parentUsage := codexResponseUsage(codexParentID, "parent-turn", "parent-response", 900, 400, 100)
	localUsage := codexResponseUsage(codexChildID, "local-turn", "local-response", 14, 8, 3)
	prefix := codexIdentity(codexChildID, codexParentID) + codexIdentity(codexParentID, "") + parentUsage
	raw := prefix + codexStarted("local-turn") + codexContext("local-turn", "model", "high") + localUsage + localUsage
	usage, err := parseCodexTranscript(strings.NewReader(raw))
	if err != nil || usage != (Usage{InputCacheHitTokens: 8, InputCacheMissTokens: 6, OutputTokens: 3}) {
		t.Fatal("original parser included inherited or repeated response usage", usage, err)
	}
	if _, err := parseCodexTranscript(strings.NewReader(prefix)); err == nil {
		t.Fatal("inherited usage supplied unavailable subagent metering")
	}
	for name, source := range map[string]string{
		"root":             codexIdentity(codexParentID, "") + parentUsage,
		"without metadata": parentUsage,
		"blank prefix":     "\n\n" + codexIdentity(codexParentID, "") + parentUsage,
	} {
		t.Run(name, func(t *testing.T) {
			usage, err := parseCodexTranscript(strings.NewReader(source))
			if err != nil || usage != (Usage{InputCacheHitTokens: 400, InputCacheMissTokens: 500, OutputTokens: 100}) {
				t.Fatal("standalone source changed", usage, err)
			}
		})
	}
}

func TestCodexThreadRejectsUnboundTurnFacts(t *testing.T) {
	prefix := codexIdentity(codexChildID, codexParentID) + codexStarted("local") + codexContext("local", "model", "high")
	for name, suffix := range map[string]string{
		"parent context":       codexContext("parent", "parent-model", "low"),
		"changed model":        codexContext("local", "other-model", "high"),
		"changed effort":       codexContext("local", "model", "low"),
		"parent usage":         codexResponseUsage(codexParentID, "local", "response", 1, 0, 1),
		"different turn usage": codexResponseUsage(codexChildID, "other", "response", 1, 0, 1),
		"unbound output":       codexTool("function_call_output", "absent", "local", map[string]any{"output": "ok"}),
		"parent tool":          codexTool("function_call", "parent-call", "parent", map[string]any{"name": "submit_semantics", "arguments": `{}`}),
		"wrong completion":     codexRecord("event_msg", map[string]string{"type": "task_complete", "turn_id": "other"}),
		"overlapping start":    codexStarted("other"),
		"partial append":       `{"type":"token_usage_record","payload":`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCodexThread([]byte(prefix + suffix)); err == nil {
				t.Fatal("accepted facts without the original thread/turn boundary")
			}
		})
	}
	if _, err := parseCodexThread([]byte(codexContext("parent", "model", "high") + codexIdentity(codexChildID, codexParentID))); err == nil {
		t.Fatal("scanned past first record to find identity")
	}
}

func TestFindCodexThreadVerifiesFirstEntityAndRetainsOriginalPath(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(home, "sessions", "2026", "10", "22")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "rollout-2026-10-22T00-00-00-"+codexChildID+".jsonl")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(codexIdentity(codexChildID, codexParentID) + codexStarted("pending"))
	thread, err := FindCodexThread(home, codexChildID)
	if err != nil || thread.Path != path || len(thread.Turns) != 1 || thread.Turns[0].State != "running" || thread.Turns[0].Model != "" {
		t.Fatal(thread, err)
	}
	write(codexIdentity(codexParentID, "") + codexIdentity(codexChildID, codexParentID))
	if _, err := FindCodexThread(home, codexChildID); err == nil {
		t.Fatal("accepted filename identity over first metadata")
	}
	for _, id := range []string{"", "../thread", "01900000-0000-7000-8000-000000000099"} {
		if _, err := FindCodexThread(home, id); err == nil {
			t.Fatal("accepted unavailable thread", id)
		}
	}
}
