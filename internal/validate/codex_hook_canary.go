package validate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/host"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CodexHookCanaryOptions struct {
	Worktree     string
	CodexCommand string
	KeepTemp     bool
	Binary       string
}

type CodexHookCanarySummary struct {
	Status                 string `json:"status"`
	Case                   string `json:"case"`
	CodexCommand           string `json:"codexCommand"`
	ThreadID               string `json:"threadId,omitempty"`
	TurnID                 string `json:"turnId,omitempty"`
	ToolUseID              string `json:"toolUseId,omitempty"`
	Attempts               int    `json:"attempts"`
	ExitCode               int    `json:"exitCode"`
	MarkerExists           bool   `json:"markerExists"`
	HookPayloadCount       int    `json:"hookPayloadCount"`
	PreToolUsePayloadCount int    `json:"preToolUsePayloadCount"`
	ArtifactDir            string `json:"artifactDir"`
	Stdout                 string `json:"stdout"`
	Stderr                 string `json:"stderr"`
	Final                  string `json:"final"`
	Prompt                 string `json:"prompt"`
	PayloadDir             string `json:"payloadDir"`
	Summary                string `json:"summary"`
	ExpectedPassCondition  string `json:"expectedPassCondition"`
	FailureReason          string `json:"failureReason,omitempty"`
	Diagnostic             string `json:"diagnostic,omitempty"`
	NextAction             string `json:"nextAction,omitempty"`
}

type CodexHookProbeOptions struct {
	PayloadDir string
	Payload    []byte
}

type CodexHookProbeResult struct {
	EventName    string `json:"eventName"`
	ToolName     string `json:"toolName"`
	PayloadPath  string `json:"payloadPath"`
	PayloadBytes int    `json:"payloadBytes"`
	ExitCode     int    `json:"exitCode"`
}

// HookDecisionRecord holds the local decision emitted by hook decide. It does
// not establish whether the host honored the response.
type HookDecisionRecord struct {
	PayloadPath string       `json:"payloadPath"`
	Provider    string       `json:"provider"`
	Decision    HookDecision `json:"decision"`
	Response    string       `json:"response"`
	ExitCode    int          `json:"exitCode"`
}

func RecordHookDecision(dir string, payload []byte, provider string, decision HookDecision, response []byte, exitCode int) error {
	probe, err := recordHookPayload(dir, payload)
	if err != nil {
		return err
	}
	return writeJSON(probe.PayloadPath+".decision.json", HookDecisionRecord{
		PayloadPath: probe.PayloadPath,
		Provider:    provider,
		Decision:    decision,
		Response:    string(response),
		ExitCode:    exitCode,
	})
}

func CodexHookCanary(options CodexHookCanaryOptions) (CodexHookCanarySummary, Result) {
	var result Result
	codexCommand := strings.TrimSpace(options.CodexCommand)
	if codexCommand == "" {
		codexCommand = "codex"
	}
	outputRoot := filepath.Join(cleanWorktree(options.Worktree), ".gates", "tmp", "codex-hook-canary")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		result.add("codex-hook-canary", err.Error())
		return CodexHookCanarySummary{Status: "FAIL"}, result
	}
	caseDir, err := os.MkdirTemp(outputRoot, "codex-hook-client-canary-")
	if err != nil {
		result.add("codex-hook-canary", err.Error())
		return CodexHookCanarySummary{Status: "FAIL"}, result
	}
	caseName := filepath.Base(caseDir)
	summaryPath := filepath.Join(outputRoot, caseName+".summary.json")
	summary := CodexHookCanarySummary{
		Status:                "FAIL",
		Case:                  caseName,
		CodexCommand:          codexCommand,
		ExitCode:              -1,
		ArtifactDir:           slash(caseDir),
		Summary:               slash(summaryPath),
		ExpectedPassCondition: "One original PreToolUse MCP apply_patch target call has matching deny evidence and native hook/completed blocked with the same thread/turn, source and deny feedback; the tool handler did not execute, the client and turn completed successfully, and no marker exists.",
		NextAction:            "Use explicit formal-gates workflow validation; a native target invocation and host blocking evidence are required to qualify automatic interception.",
	}
	for invocation := 1; invocation <= 2; invocation++ {
		directory := filepath.Join(caseDir, fmt.Sprintf("invocation-%d", invocation))
		if err := os.MkdirAll(directory, 0o700); err != nil {
			summary.FailureReason = err.Error()
			break
		}
		err := runCodexHookCanary(options, directory, &summary)
		if errors.Is(err, errCodexHookNoCall) && invocation == 1 {
			summary.Diagnostic = "The text-only native turn produced no target call; retrying once."
			continue
		}
		if err != nil {
			summary.FailureReason = err.Error()
		} else {
			summary.Status = "PASS"
			summary.NextAction = ""
		}
		break
	}
	finishCodexHookCanary(summaryPath, summary, &result)
	return summary, result
}

func runCodexHookCanary(options CodexHookCanaryOptions, caseDir string, summary *CodexHookCanarySummary) error {
	codex, err := exec.LookPath(summary.CodexCommand)
	if err != nil {
		return err
	}
	binary := options.Binary
	if binary == "" {
		binary, err = os.Executable()
	}
	if err != nil {
		return err
	}
	binary = absPath(binary)
	workspace := filepath.Join(caseDir, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return err
	}
	if !options.KeepTemp {
		defer os.RemoveAll(workspace)
	}
	prompt := "Call the hook_canary MCP tool apply_patch exactly once with {}. Its target and patch are supplied by the program. Do not call any other tool. After the response, finish with a brief explanation."
	summary.Prompt = slash(filepath.Join(caseDir, "prompt.txt"))
	summary.PayloadDir = slash(filepath.Join(caseDir, "payloads"))
	summary.Stdout = slash(filepath.Join(caseDir, "client.stdout"))
	summary.Stderr = slash(filepath.Join(caseDir, "client.stderr"))
	summary.Final = slash(filepath.Join(caseDir, "terminal.json"))
	for path, data := range map[string]string{summary.Prompt: prompt, filepath.Join(caseDir, "marker.patch"): "diff --git a/marker.go b/marker.go\nnew file mode 100644\n--- /dev/null\n+++ b/marker.go\n@@ -0,0 +1 @@\n+package canary\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(caseDir, "request.json"), map[string]any{"codex": absPath(codex), "binary": binary, "workspace": workspace, "prompt": prompt}); err != nil {
		return err
	}
	stdout, err := os.Create(summary.Stdout)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.Create(summary.Stderr)
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd := exec.Command(binary, "harness", "codex-hook-client", caseDir)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	summary.Attempts++
	summary.HookPayloadCount, summary.PreToolUsePayloadCount = 0, 0
	runErr := cmd.Run()
	if cmd.ProcessState != nil {
		summary.ExitCode = cmd.ProcessState.ExitCode()
	}
	summary.MarkerExists = isFile(filepath.Join(workspace, "marker.go"))
	payloads, err := filepath.Glob(filepath.Join(summary.PayloadDir, "hook-*.json"))
	if err != nil {
		return err
	}
	for _, path := range payloads {
		if strings.HasSuffix(path, ".decision.json") {
			continue
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		summary.HookPayloadCount++
		if event, _ := hookPayloadNames(payload); event == "PreToolUse" {
			summary.PreToolUsePayloadCount++
		}
	}
	if runErr != nil {
		return fmt.Errorf("native client failed: %w; see %s", runErr, summary.Stderr)
	}
	return qualifyCodexHookCanary(caseDir, workspace, summary)
}

var errCodexHookNoCall = errors.New("native turn completed without a PreToolUse target call")

// The original PreToolUse payload owns the tool call identity. Its unique
// decision and the native hook run bind interception to the same thread/turn.
func qualifyCodexHookCanary(directory, workspace string, summary *CodexHookCanarySummary) error {
	identity, err := os.ReadFile(filepath.Join(directory, "identity.json"))
	if err != nil {
		return err
	}
	var ids struct {
		Thread string `json:"threadId"`
		Turn   string `json:"turnId"`
	}
	if err := json.Unmarshal(identity, &ids); err != nil || ids.Thread == "" || ids.Turn == "" {
		return fmt.Errorf("missing original native thread/turn identity")
	}
	summary.ThreadID, summary.TurnID = ids.Thread, ids.Turn
	paths, err := filepath.Glob(filepath.Join(directory, "payloads", "*.decision.json"))
	if err != nil {
		return err
	}
	matching, reason := 0, ""
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record HookDecisionRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		payload, err := os.ReadFile(record.PayloadPath)
		if err != nil {
			return err
		}
		var input struct {
			Event     string         `json:"hook_event_name"`
			Tool      string         `json:"tool_name"`
			Session   string         `json:"session_id"`
			Turn      string         `json:"turn_id"`
			Call      string         `json:"tool_use_id"`
			Cwd       string         `json:"cwd"`
			Arguments map[string]any `json:"tool_input"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			return err
		}
		if input.Event != "PreToolUse" {
			continue
		}
		if input.Tool != "mcp__hook_canary__apply_patch" || input.Session != ids.Thread || input.Turn != ids.Turn || input.Call == "" || cleanWorktree(input.Cwd) != cleanWorktree(workspace) || input.Arguments == nil || len(input.Arguments) != 0 || record.Provider != "codex" || record.Decision.Decision != "block" || record.Decision.Reason == "" || record.ExitCode != 0 {
			return fmt.Errorf("original PreToolUse decision differs from the native target call")
		}
		response, err := json.Marshal(HookResponse("codex", record.Decision))
		if err != nil || !bytes.Equal(bytes.TrimSpace([]byte(record.Response)), response) {
			return fmt.Errorf("original hook response is not the recorded deny")
		}
		matching++
		summary.ToolUseID, reason = input.Call, record.Decision.Reason
	}
	events, err := os.Open(filepath.Join(directory, "native-events.jsonl"))
	if err != nil {
		return err
	}
	defer events.Close()
	scan := bufio.NewScanner(events)
	scan.Buffer(make([]byte, 4096), int(^uint(0)>>1))
	blocked, terminal := 0, false
	for scan.Scan() {
		var frame struct {
			Method string `json:"method"`
			Params struct {
				Thread string `json:"threadId"`
				Turn   string `json:"turnId"`
				Run    struct {
					ID, EventName, Status, Source string
					Entries                       []struct{ Kind, Text string }
				}
				Terminal struct{ ID, Status string } `json:"turn"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
			return err
		}
		p := frame.Params
		if p.Thread != ids.Thread {
			continue
		}
		if frame.Method == "turn/completed" {
			terminal = p.Terminal.ID == ids.Turn && p.Terminal.Status == "completed"
			continue
		}
		if p.Turn != ids.Turn || frame.Method != "hook/completed" || p.Run.EventName != "preToolUse" {
			continue
		}
		feedback := false
		for _, entry := range p.Run.Entries {
			if entry.Kind == "feedback" && entry.Text == reason {
				feedback = true
			}
		}
		if p.Run.ID == "" || p.Run.Status != "blocked" || p.Run.Source != "sessionFlags" || !feedback {
			return fmt.Errorf("native PreToolUse does not prove the recorded target denial")
		}
		blocked++
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if matching == 0 && summary.PreToolUsePayloadCount == 0 && blocked == 0 && terminal && !summary.MarkerExists && !isFile(filepath.Join(directory, "target-call.json")) {
		return errCodexHookNoCall
	}
	if matching != 1 || summary.PreToolUsePayloadCount != 1 || blocked != 1 || !terminal || summary.MarkerExists || isFile(filepath.Join(directory, "target-call.json")) {
		return fmt.Errorf("target execution was not prevented by one original hook deny and matching native blocking")
	}
	return nil
}

func CodexHookProbe(options CodexHookProbeOptions) (CodexHookProbeResult, Result) {
	var result Result
	if strings.TrimSpace(options.PayloadDir) == "" {
		result.add("codex-hook-probe", "--payload-dir is required")
		return CodexHookProbeResult{ExitCode: 1}, result
	}
	probe, err := recordHookPayload(options.PayloadDir, options.Payload)
	if err != nil {
		result.add("codex-hook-probe", err.Error())
		return CodexHookProbeResult{ExitCode: 1}, result
	}
	return probe, result
}

func recordHookPayload(dir string, payload []byte) (CodexHookProbeResult, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CodexHookProbeResult{}, err
	}
	eventName, toolName := hookPayloadNames(payload)
	file, err := os.CreateTemp(dir, fmt.Sprintf("hook-%s-%s-*.json", safeFilePart(eventName), safeFilePart(toolName)))
	if err != nil {
		return CodexHookProbeResult{}, err
	}
	payloadPath := file.Name()
	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return CodexHookProbeResult{}, err
	}
	return CodexHookProbeResult{
		EventName:    eventName,
		ToolName:     toolName,
		PayloadPath:  slash(payloadPath),
		PayloadBytes: len(payload),
		ExitCode:     0,
	}, nil
}

func finishCodexHookCanary(path string, summary CodexHookCanarySummary, result *Result) {
	if err := writeJSON(path, summary); err != nil {
		result.add("codex-hook-canary", err.Error())
	}
	if summary.Status == "PASS" {
		return
	}
	detail := "Codex hook canary status=" + summary.Status
	if strings.TrimSpace(summary.FailureReason) != "" {
		detail += ": " + summary.FailureReason
	}
	result.add("codex-hook-canary", detail)
}

func hookPayloadNames(payload []byte) (string, string) {
	eventName := "unknown"
	toolName := "unknown"
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err == nil {
		if value := host.ScalarString(decoded["hook_event_name"]); value != "" {
			eventName = value
		}
		if value := host.ScalarString(decoded["tool_name"]); value != "" {
			toolName = value
		}
	}
	return eventName, toolName
}

func safeFilePart(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	return builder.String()
}
