package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"

	"formal-gates/internal/atomicfile"
)

// CodexThread contains original rollout facts, not workflow execution state.
type CodexThread struct {
	Path             string
	ID               string
	ParentThreadID   string
	AgentPath        string
	ModelProvider    string
	Originator       string
	CLIVersion       string
	Source           json.RawMessage
	SourceSubagent   bool
	ForkedFromID     string
	InheritedHistory bool
	Turns            []CodexTurn
}

type CodexTurn struct {
	ID                string
	State             string
	Reason            string
	Model             string
	Effort            string
	ApprovalPolicy    string
	ApprovalsReviewer string
	SandboxPolicy     json.RawMessage
	Usage             Usage
	UsageAvailable    bool
	Tools             []CodexToolCall
}

// CodexToolCall preserves native call/output bytes. Output presence does not
// establish tool success, and an exec wrapper does not establish nested calls.
type CodexToolCall struct {
	Type      string
	CallID    string
	Namespace string
	Name      string
	Arguments string
	Input     string
	Output    json.RawMessage
}

var codexThreadID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (thread CodexThread) ValidateIdentity(id, parent string) error {
	if !codexThreadID.MatchString(id) || thread.ID != id {
		return fmt.Errorf("Codex rollout entity %q does not match thread %q", thread.ID, id)
	}
	if thread.ParentThreadID != parent || parent != "" && !thread.SourceSubagent {
		return fmt.Errorf("Codex thread %s parent %q does not match %q", id, thread.ParentThreadID, parent)
	}
	return nil
}

// FindCodexThread uses the native filename only to locate candidates; first
// session_meta identity must match before a candidate can supply evidence.
func FindCodexThread(home, id string) (CodexThread, error) {
	if home == "" || !codexThreadID.MatchString(id) {
		return CodexThread{}, fmt.Errorf("Codex home and a native UUID thread identity are required")
	}
	var found *CodexThread
	for _, pattern := range []string{
		filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl"),
		filepath.Join(home, "archived_sessions", "rollout-*-"+id+".jsonl"),
	} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return CodexThread{}, err
		}
		for _, path := range paths {
			thread, err := ReadCodexThread(path)
			if err != nil {
				return CodexThread{}, err
			}
			if thread.ID != id {
				return CodexThread{}, fmt.Errorf("Codex rollout %s entity %q does not match thread %q", path, thread.ID, id)
			}
			if found != nil {
				return CodexThread{}, fmt.Errorf("multiple Codex rollouts identify thread %s: %s and %s", id, found.Path, path)
			}
			found = &thread
		}
	}
	if found == nil {
		return CodexThread{}, fmt.Errorf("Codex rollout for thread %s is unavailable under %s", id, home)
	}
	return *found, nil
}

// ReadCodexThread permits concurrent native rollout writes/replacement. Only
// task_started intervals contribute turn context, tool facts and response usage.
func ReadCodexThread(path string) (CodexThread, error) {
	raw, err := atomicfile.ReadFile(path)
	if err != nil {
		return CodexThread{}, err
	}
	thread, err := parseCodexThread(raw)
	if err != nil {
		return CodexThread{}, fmt.Errorf("Codex rollout %s: %w", path, err)
	}
	thread.Path = path
	return thread, nil
}

func parseCodexThread(raw []byte) (CodexThread, error) {
	thread := CodexThread{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	lineNumber, current := 0, -1
	var usage bytes.Buffer
	finishUsage := func() error {
		if usage.Len() == 0 {
			return nil
		}
		value, err := parseCodexUsageRecords(&usage)
		if err != nil {
			return err
		}
		thread.Turns[current].Usage = value
		thread.Turns[current].UsageAvailable = true
		usage.Reset()
		return nil
	}
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return thread, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if thread.ID == "" {
			if record.Type != "session_meta" {
				return thread, fmt.Errorf("first record is not session_meta")
			}
			if err := readCodexThreadIdentity(record.Payload, &thread); err != nil {
				return thread, err
			}
			continue
		}
		var payload struct {
			Type              string          `json:"type"`
			Reason            string          `json:"reason"`
			TurnID            string          `json:"turn_id"`
			ThreadID          string          `json:"thread_id"`
			Model             string          `json:"model"`
			Effort            string          `json:"effort"`
			ApprovalPolicy    string          `json:"approval_policy"`
			ApprovalsReviewer string          `json:"approvals_reviewer"`
			SandboxPolicy     json.RawMessage `json:"sandbox_policy"`
			ResponseID        string          `json:"response_id"`
			CallID            string          `json:"call_id"`
			Namespace         string          `json:"namespace"`
			Name              string          `json:"name"`
			Arguments         string          `json:"arguments"`
			Input             string          `json:"input"`
			Output            json.RawMessage `json:"output"`
			Metadata          struct {
				TurnID string `json:"turn_id"`
			} `json:"internal_chat_message_metadata_passthrough"`
		}
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return thread, fmt.Errorf("line %d payload: %w", lineNumber, err)
		}
		if record.Type == "event_msg" && payload.Type == "task_started" {
			if payload.TurnID == "" {
				return thread, fmt.Errorf("task_started has no turn_id")
			}
			if current >= 0 && thread.Turns[current].State == "running" {
				return thread, fmt.Errorf("task_started overlaps unfinished turn %s", thread.Turns[current].ID)
			}
			if err := finishUsage(); err != nil {
				return thread, err
			}
			for _, turn := range thread.Turns {
				if turn.ID == payload.TurnID {
					return thread, fmt.Errorf("duplicate task_started turn %s", payload.TurnID)
				}
			}
			thread.Turns = append(thread.Turns, CodexTurn{ID: payload.TurnID, State: "running"})
			current = len(thread.Turns) - 1
			continue
		}
		if current < 0 {
			if record.Type == "session_meta" || record.Type == "turn_context" || record.Type == "compacted" {
				thread.InheritedHistory = true
			}
			continue
		}
		turn := &thread.Turns[current]
		switch record.Type {
		case "turn_context":
			if payload.TurnID != turn.ID || turn.State != "running" || payload.Model == "" || payload.Effort == "" {
				return thread, fmt.Errorf("turn_context does not identify active turn %s and model/effort", turn.ID)
			}
			if turn.Model != "" && (turn.Model != payload.Model || turn.Effort != payload.Effort) {
				return thread, fmt.Errorf("turn_context changes model/effort within turn %s", turn.ID)
			}
			turn.Model, turn.Effort = payload.Model, payload.Effort
			turn.ApprovalPolicy, turn.ApprovalsReviewer, turn.SandboxPolicy = payload.ApprovalPolicy, payload.ApprovalsReviewer, payload.SandboxPolicy
		case "token_usage_record":
			if payload.TurnID != turn.ID || payload.ThreadID != thread.ID || payload.ResponseID == "" {
				return thread, fmt.Errorf("response usage does not identify thread %s turn %s", thread.ID, turn.ID)
			}
			usage.Write(line)
			usage.WriteByte('\n')
		case "event_msg":
			state := ""
			switch payload.Type {
			case "task_complete":
				state = "completed"
			case "turn_aborted":
				state = "aborted"
				if payload.Reason == "interrupted" {
					state = "interrupted"
				}
			}
			if state != "" {
				if payload.TurnID != turn.ID || turn.State != "running" {
					return thread, fmt.Errorf("%s does not identify active turn %s", payload.Type, turn.ID)
				}
				turn.State, turn.Reason = state, payload.Reason
			}
		case "response_item":
			if payload.Type != "function_call" && payload.Type != "function_call_output" && payload.Type != "custom_tool_call" && payload.Type != "custom_tool_call_output" {
				continue
			}
			if payload.Metadata.TurnID != turn.ID || payload.CallID == "" || turn.State != "running" {
				return thread, fmt.Errorf("tool fact does not identify active turn %s", turn.ID)
			}
			if payload.Type == "function_call" || payload.Type == "custom_tool_call" {
				if payload.Name == "" {
					return thread, fmt.Errorf("tool call %s has no name", payload.CallID)
				}
				turn.Tools = append(turn.Tools, CodexToolCall{Type: payload.Type, CallID: payload.CallID, Namespace: payload.Namespace, Name: payload.Name, Arguments: payload.Arguments, Input: payload.Input})
				continue
			}
			matched := false
			for index := range turn.Tools {
				call := &turn.Tools[index]
				if call.CallID == payload.CallID && payload.Type == call.Type+"_output" {
					call.Output, matched = payload.Output, true
					break
				}
			}
			if !matched {
				return thread, fmt.Errorf("tool output %s has no matching call in turn %s", payload.CallID, turn.ID)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return thread, err
	}
	if thread.ID == "" {
		return thread, fmt.Errorf("session_meta is unavailable")
	}
	if err := finishUsage(); err != nil {
		return thread, err
	}
	return thread, nil
}

func readCodexThreadIdentity(raw json.RawMessage, thread *CodexThread) error {
	var metadata struct {
		ID            string          `json:"id"`
		ModelProvider string          `json:"model_provider"`
		Originator    string          `json:"originator"`
		CLIVersion    string          `json:"cli_version"`
		Source        json.RawMessage `json:"source"`
		ForkedFromID  string          `json:"forked_from_id"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return err
	}
	if !codexThreadID.MatchString(metadata.ID) {
		return fmt.Errorf("first session_meta has no native UUID entity identity")
	}
	thread.ID, thread.ModelProvider = metadata.ID, metadata.ModelProvider
	thread.Originator, thread.CLIVersion, thread.Source = metadata.Originator, metadata.CLIVersion, metadata.Source
	thread.ForkedFromID = metadata.ForkedFromID
	var source struct {
		Subagent *struct {
			ThreadSpawn *struct {
				ParentThreadID string `json:"parent_thread_id"`
				AgentPath      string `json:"agent_path"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	var sourceName string
	if json.Unmarshal(metadata.Source, &sourceName) == nil {
		return nil
	}
	if err := json.Unmarshal(metadata.Source, &source); err != nil {
		return fmt.Errorf("session_meta source: %w", err)
	}
	if source.Subagent != nil && source.Subagent.ThreadSpawn != nil {
		spawn := source.Subagent.ThreadSpawn
		if !codexThreadID.MatchString(spawn.ParentThreadID) || spawn.ParentThreadID == thread.ID {
			return fmt.Errorf("session_meta thread_spawn has no distinct native parent identity")
		}
		thread.ParentThreadID, thread.AgentPath, thread.SourceSubagent = spawn.ParentThreadID, spawn.AgentPath, true
	}
	return nil
}
