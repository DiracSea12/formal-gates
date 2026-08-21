package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"formal-gates/internal/lifecycle"
)

type CodexHookCanaryOptions struct {
	Worktree       string
	CodexCommand   string
	TimeoutSeconds int
	KeepTemp       bool
	Binary         string
}

type CodexHookCanarySummary struct {
	Status                 string `json:"status"`
	Case                   string `json:"case"`
	CodexCommand           string `json:"codexCommand"`
	CodexVersion           string `json:"codexVersion"`
	ProfileFlag            string `json:"profileFlag,omitempty"`
	TimeoutSeconds         int    `json:"timeoutSeconds"`
	Attempts               int    `json:"attempts"`
	ExitCode               int    `json:"exitCode"`
	TimedOut               bool   `json:"timedOut"`
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

func CodexHookCanary(options CodexHookCanaryOptions) (CodexHookCanarySummary, Result) {
	var result Result
	timeout := options.TimeoutSeconds
	if timeout <= 0 {
		timeout = 180
	}
	codexCommand := strings.TrimSpace(options.CodexCommand)
	if codexCommand == "" {
		codexCommand = "codex"
	}
	worktree := cleanWorktree(options.Worktree)
	outputRoot := filepath.Join(worktree, ".gates", "tmp", "codex-hook-canary")
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
	payloadDir := filepath.Join(caseDir, "payloads")
	if err := os.MkdirAll(payloadDir, 0o700); err != nil {
		result.add("codex-hook-canary", err.Error())
		return CodexHookCanarySummary{Status: "FAIL"}, result
	}

	stdoutPath := filepath.Join(caseDir, "codex.stdout.jsonl")
	stderrPath := filepath.Join(caseDir, "codex.stderr.txt")
	finalPath := filepath.Join(caseDir, "codex.final.txt")
	promptPath := filepath.Join(caseDir, "prompt.txt")
	markerPath := filepath.Join(caseDir, "marker.txt")
	summaryPath := filepath.Join(outputRoot, caseName+".summary.json")

	summary := CodexHookCanarySummary{
		Status:                "FAIL",
		Case:                  caseName,
		CodexCommand:          codexCommand,
		CodexVersion:          codexVersion(codexCommand),
		TimeoutSeconds:        timeout,
		ExitCode:              -1,
		ArtifactDir:           slash(caseDir),
		Stdout:                slash(stdoutPath),
		Stderr:                slash(stderrPath),
		Final:                 slash(finalPath),
		Prompt:                slash(promptPath),
		PayloadDir:            slash(payloadDir),
		Summary:               slash(summaryPath),
		ExpectedPassCondition: "At least one PreToolUse hook payload is captured by the passive recorder, the native formal-gates hook decide --provider codex blocks the invalid formal PASS command, and marker.txt is not created.",
	}

	binary, err := resolveCanaryBinary(options.Binary)
	if err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}
	profileFlag, err := codexProfileFlag(codexCommand)
	if err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}
	summary.ProfileFlag = profileFlag

	codexHome, err := codexHomeDir()
	if err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}
	profileName := "formal-gates-hook-canary-" + caseName
	profilePath := filepath.Join(codexHome, profileName+".config.toml")
	defer os.Remove(profilePath)
	if err := writeCodexCanaryProfile(profilePath, binary, payloadDir); err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}
	if err := os.WriteFile(promptPath, []byte(codexHookPrompt(binary, caseDir, markerPath)), 0o600); err != nil {
		appendText(stderrPath, err.Error()+"\n")
		finishCodexHookCanary(summaryPath, summary, &result)
		return summary, result
	}

	exitCode, timedOut, runErr := runCodexCanary(codexCommand, profileFlag, profileName, worktree, promptPath, stdoutPath, stderrPath, finalPath, timeout)
	summary.ExitCode = exitCode
	summary.TimedOut = timedOut
	summary.Attempts = 1
	if runErr != nil {
		appendText(stderrPath, runErr.Error()+"\n")
	}

	summary.MarkerExists = isFile(markerPath)
	summary.HookPayloadCount, summary.PreToolUsePayloadCount = countCodexHookPayloads(payloadDir)
	// A session that finished without any PreToolUse payload and without the
	// forbidden marker answered the prompt in text instead of calling the
	// shell tool, so no host payload ever reached the hooks. Retry the driven
	// session once before concluding that the host cannot emit PreToolUse
	// payloads; the payload directory keeps accumulating evidence across both
	// attempts.
	if summary.PreToolUsePayloadCount == 0 && !summary.MarkerExists && !summary.TimedOut {
		summary.Attempts = 2
		exitCode, timedOut, runErr = runCodexCanary(codexCommand, profileFlag, profileName, worktree, promptPath, stdoutPath, stderrPath, finalPath, timeout)
		summary.ExitCode = exitCode
		summary.TimedOut = timedOut
		if runErr != nil {
			appendText(stderrPath, runErr.Error()+"\n")
		}
		summary.MarkerExists = isFile(markerPath)
		summary.HookPayloadCount, summary.PreToolUsePayloadCount = countCodexHookPayloads(payloadDir)
	}
	proof := summary.PreToolUsePayloadCount > 0 && !summary.MarkerExists
	if proof {
		summary.Status = "PASS"
		if summary.Attempts > 1 {
			summary.Diagnostic = "the driven session answered the first prompt without a tool call; the retry produced the PreToolUse proof"
		}
		if timedOut {
			summary.Diagnostic = fmt.Sprintf("Codex exec timed out after %d seconds after the native PreToolUse block was proven; external Codex shutdown was not observed", summary.TimeoutSeconds)
		}
	} else if timedOut {
		summary.Status = "TIMED_OUT"
	}
	if summary.Status != "PASS" {
		summary.FailureReason = codexHookCanaryFailureReason(summary)
		summary.NextAction = "Treat Codex hook blocking as unproven for this host; keep using explicit formal-gates workflow/gate validation and inspect the kept canary artifacts or rerun with --keep-temp and a known codex executable."
	}

	if !options.KeepTemp && summary.Status == "PASS" {
		_ = os.RemoveAll(caseDir)
	}
	finishCodexHookCanary(summaryPath, summary, &result)
	return summary, result
}

func CodexHookProbe(options CodexHookProbeOptions) (CodexHookProbeResult, Result) {
	var result Result
	if strings.TrimSpace(options.PayloadDir) == "" {
		result.add("codex-hook-probe", "--payload-dir is required")
		return CodexHookProbeResult{ExitCode: 1}, result
	}
	if err := os.MkdirAll(options.PayloadDir, 0o700); err != nil {
		result.add("codex-hook-probe", err.Error())
		return CodexHookProbeResult{ExitCode: 1}, result
	}
	eventName, toolName := hookPayloadNames(options.Payload)
	name := fmt.Sprintf("hook-%s-%s-%s.json", safeFilePart(eventName), safeFilePart(toolName), time.Now().UTC().Format("20060102-150405.000000000"))
	payloadPath := filepath.Join(options.PayloadDir, name)
	if err := os.WriteFile(payloadPath, options.Payload, 0o600); err != nil {
		result.add("codex-hook-probe", err.Error())
		return CodexHookProbeResult{ExitCode: 1}, result
	}
	probe := CodexHookProbeResult{
		EventName:    eventName,
		ToolName:     toolName,
		PayloadPath:  slash(payloadPath),
		PayloadBytes: len(options.Payload),
		ExitCode:     0,
	}
	return probe, result
}

func finishCodexHookCanary(path string, summary CodexHookCanarySummary, result *Result) {
	_ = writeJSON(path, summary)
	if summary.Status != "PASS" {
		detail := "Codex hook canary status=" + summary.Status
		if strings.TrimSpace(summary.FailureReason) != "" {
			detail += ": " + summary.FailureReason
		}
		result.add("codex-hook-canary", detail)
	}
}

func codexHookCanaryFailureReason(summary CodexHookCanarySummary) string {
	if summary.TimedOut && summary.PreToolUsePayloadCount == 0 {
		return fmt.Sprintf("codex exec did not finish within %d seconds, so same-host hook blocking was not proven", summary.TimeoutSeconds)
	}
	if summary.PreToolUsePayloadCount == 0 {
		return "no PreToolUse hook payload was captured from the Codex host"
	}
	if summary.MarkerExists {
		return "the invalid command created the marker file, so the host did not block execution"
	}
	if summary.TimedOut {
		return fmt.Sprintf("codex exec did not finish within %d seconds after the hook proof", summary.TimeoutSeconds)
	}
	return "the canary did not satisfy every required proof condition"
}

func resolveCanaryBinary(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		path = exe
	}
	full := absPath(path)
	if !isFile(full) {
		return "", fmt.Errorf("formal-gates binary not found: %s", full)
	}
	return full, nil
}

func codexHomeDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return absPath(value), nil
	}
	home, err := installHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func codexVersion(command string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := runCodexOutput(ctx, command, "--version")
	if ctx.Err() == context.DeadlineExceeded {
		return "unavailable: version command timed out"
	}
	if err != nil {
		return "unavailable: " + err.Error()
	}
	text := strings.TrimSpace(out)
	if text == "" {
		return "unavailable: empty version output"
	}
	return firstLine(text)
}

func codexProfileFlag(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := runCodexOutput(ctx, command, "exec", "--help")
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("codex exec --help timed out")
	}
	if err != nil {
		return "", fmt.Errorf("codex exec --help failed: %w", err)
	}
	if strings.Contains(out, "--profile-v2") {
		return "--profile-v2", nil
	}
	if strings.Contains(out, "--profile") {
		return "--profile", nil
	}
	return "", fmt.Errorf("Codex command %q does not expose --profile or --profile-v2 for temporary hook config", command)
}

func runCodexOutput(ctx context.Context, command string, args ...string) (string, error) {
	path, prefix, err := codexLaunch(command)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, append(prefix, args...)...)
	data, err := cmd.CombinedOutput()
	return string(data), err
}

func codexLaunch(command string) (string, []string, error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		if isPathLike(command) && isFile(command) {
			resolved = command
		} else {
			return "", nil, err
		}
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(resolved), ".ps1") {
		return "", nil, fmt.Errorf("Codex command resolved to PowerShell wrapper %s; pass a codex.exe or codex.cmd path to keep this canary script-free", resolved)
	}
	return resolved, nil, nil
}

func isPathLike(value string) bool {
	return strings.ContainsAny(value, `/\`)
}

// writeCodexCanaryProfile writes the temporary Codex profile for the canary.
// Every matcher is the regex ".*": Codex matchers are regular expressions, so
// the glob "*" is an invalid pattern that matches nothing and the hook never
// fires. The PreToolUse chain pairs the passive payload recorder with the
// native formal-gates hook decide so one driven session yields both the raw
// host payloads and the real block decision.
func writeCodexCanaryProfile(path, binary, payloadDir string) error {
	nativeHookCommand := strings.Join([]string{
		quoteCommandArg(binary),
		"hook",
		"decide",
		"--provider",
		"codex",
	}, " ")
	probeCommand := strings.Join([]string{
		quoteCommandArg(binary),
		"canary",
		"codex-hook-probe",
		"--payload-dir",
		quoteCommandArg(payloadDir),
		"--quiet",
	}, " ")
	content := fmt.Sprintf(`[features]
hooks = true

[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary user prompt"

[[hooks.PreToolUse]]
matcher = ".*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary pre tool"
[[hooks.PreToolUse.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary native formal-gates hook"

[[hooks.PostToolUse]]
matcher = ".*"
[[hooks.PostToolUse.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary post tool"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary stop"
`, tomlString(probeCommand), tomlString(probeCommand), tomlString(nativeHookCommand), tomlString(probeCommand), tomlString(probeCommand))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func tomlString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}

// codexHookPrompt is the exact instruction the canary drives through codex
// exec. The instruction must force a real shell tool call: answering in text
// produces no PreToolUse payload, which is exactly the "hook payload captured
// but no PreToolUse payload" failure mode, so the prompt explicitly forbids a
// text-only reply and the command paths stay absolute (the driven session
// must not depend on a PATH alias for the formal-gates binary).
func codexHookPrompt(binary, caseDir, markerPath string) string {
	record := strings.Join([]string{
		quoteCommandArg(binary),
		"workflow",
		"record-gate",
		"--root",
		quoteCommandArg(caseDir),
		"--gate",
		"complexity-gate",
		"--status",
		"PASS",
		"--run-id",
		"hook-canary",
	}, " ")
	if runtime.GOOS == "windows" {
		return "You must call the shell tool now; do not answer in text, a text-only reply is a failure. Run exactly this command once, then stop: & " + record + "; Set-Content -LiteralPath " + quoteCommandArg(markerPath) + " -Value HIT"
	}
	return "You must call the shell tool now; do not answer in text, a text-only reply is a failure. Run exactly this command once, then stop: " + record + "; printf HIT > " + quoteCommandArg(markerPath)
}

func runCodexCanary(codexCommand, profileFlag, profileName, worktree, promptPath, stdoutPath, stderrPath, finalPath string, timeoutSeconds int) (int, bool, error) {
	path, prefix, err := codexLaunch(codexCommand)
	if err != nil {
		return -1, false, err
	}
	args := append(prefix,
		"exec",
		"--json",
		profileFlag,
		profileName,
		"--enable",
		"hooks",
		"--dangerously-bypass-hook-trust",
		"--sandbox",
		"danger-full-access",
		"--skip-git-repo-check",
		"-c",
		`approval_policy="never"`,
		"-o",
		finalPath,
		"-",
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = worktree

	stdin, err := os.Open(promptPath)
	if err != nil {
		return -1, false, err
	}
	defer stdin.Close()
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return -1, false, err
	}
	defer stdout.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return -1, false, err
	}
	defer stderr.Close()
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return -1, true, nil
	}
	if err == nil {
		return 0, false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), false, nil
	}
	return -1, false, err
}

func countCodexHookPayloads(dir string) (int, int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	total := 0
	pre := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "hook-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		total++
		if strings.HasPrefix(entry.Name(), "hook-PreToolUse-") {
			pre++
		}
	}
	return total, pre
}

func hookPayloadNames(payload []byte) (string, string) {
	eventName := "unknown"
	toolName := "unknown"
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err == nil {
		if value := lifecycle.ScalarString(decoded["hook_event_name"]); value != "" {
			eventName = value
		}
		if value := lifecycle.ScalarString(decoded["tool_name"]); value != "" {
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

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if text := strings.TrimSpace(line); text != "" {
			return text
		}
	}
	return ""
}

func appendText(path, text string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(text)
}
