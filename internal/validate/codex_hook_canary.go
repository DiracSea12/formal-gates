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
)

type CodexHookCanaryOptions struct {
	Worktree       string
	OutputDir      string
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
	FormalHookOutput       string `json:"formalHookOutput"`
	Summary                string `json:"summary"`
	ExpectedPassCondition  string `json:"expectedPassCondition"`
}

type CodexHookProbeOptions struct {
	PayloadDir       string
	FormalHookOutput string
	Payload          []byte
}

type CodexHookProbeResult struct {
	EventName   string        `json:"eventName"`
	ToolName    string        `json:"toolName"`
	PayloadPath string        `json:"payloadPath"`
	Decision    *HookDecision `json:"decision,omitempty"`
	ExitCode    int           `json:"exitCode"`
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
	outputRoot := strings.TrimSpace(options.OutputDir)
	if outputRoot == "" {
		outputRoot = filepath.Join(worktree, ".artifacts", "ai", "formal-gates-hook-client-tests")
	}
	outputRoot = absPath(outputRoot)
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		result.add("codex-hook-canary", err.Error())
		return CodexHookCanarySummary{Status: "FAIL"}, result
	}

	caseName := "codex-hook-client-canary-" + time.Now().UTC().Format("20060102-150405")
	caseDir := filepath.Join(outputRoot, caseName)
	payloadDir := filepath.Join(caseDir, "payloads")
	for _, dir := range []string{caseDir, payloadDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			result.add("codex-hook-canary", err.Error())
			return CodexHookCanarySummary{Status: "FAIL"}, result
		}
	}

	stdoutPath := filepath.Join(caseDir, "codex.stdout.jsonl")
	stderrPath := filepath.Join(caseDir, "codex.stderr.txt")
	finalPath := filepath.Join(caseDir, "codex.final.txt")
	promptPath := filepath.Join(caseDir, "prompt.txt")
	markerPath := filepath.Join(caseDir, "marker.txt")
	formalHookOutputPath := filepath.Join(caseDir, "formal-hook-output.txt")
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
		FormalHookOutput:      slash(formalHookOutputPath),
		Summary:               slash(summaryPath),
		ExpectedPassCondition: "At least one PreToolUse hook payload exists, native formal-gates hook decide denies the invalid formal PASS command, and marker.txt was not created.",
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
	profileName := "formal-gates-hook-canary-" + time.Now().UTC().Format("20060102-150405")
	profilePath := filepath.Join(codexHome, profileName+".config.toml")
	defer os.Remove(profilePath)
	if err := writeCodexCanaryProfile(profilePath, binary, payloadDir, formalHookOutputPath); err != nil {
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
	if runErr != nil {
		appendText(stderrPath, runErr.Error()+"\n")
	}

	summary.MarkerExists = isFile(markerPath)
	summary.HookPayloadCount, summary.PreToolUsePayloadCount = countCodexHookPayloads(payloadDir)
	formalHookOutput := ""
	if isFile(formalHookOutputPath) {
		formalHookOutput, _ = readText(formalHookOutputPath)
	}
	formalHookBlocked := strings.Contains(formalHookOutput, `"permissionDecision":"deny"`) ||
		strings.Contains(formalHookOutput, `"decision":"deny"`) ||
		strings.Contains(formalHookOutput, "formal gate PASS recording requires an artifact")
	if timedOut {
		summary.Status = "TIMED_OUT"
	} else if summary.PreToolUsePayloadCount > 0 && !summary.MarkerExists && formalHookBlocked {
		summary.Status = "PASS"
	}

	if !options.KeepTemp && summary.Status == "PASS" {
		_ = os.RemoveAll(caseDir)
	}
	finishCodexHookCanary(summaryPath, summary, &result)
	return summary, result
}

func CodexHookCanaryJSON(summary CodexHookCanarySummary) ([]byte, error) {
	return json.MarshalIndent(summary, "", "  ")
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
		EventName:   eventName,
		ToolName:    toolName,
		PayloadPath: slash(payloadPath),
		ExitCode:    0,
	}
	if eventName != "PreToolUse" {
		return probe, result
	}
	decision, err := Hook(options.Payload)
	if err != nil {
		result.add("codex-hook-probe", err.Error())
		probe.ExitCode = 1
		return probe, result
	}
	probe.Decision = &decision
	if options.FormalHookOutput != "" {
		lines := []string{"exit=0"}
		if decision.Decision == "deny" {
			lines[0] = "exit=2"
		}
		encoded, _ := json.Marshal(decision)
		lines = append(lines, string(encoded))
		appendText(options.FormalHookOutput, strings.Join(lines, "\n")+"\n")
	}
	if decision.Decision == "deny" {
		probe.ExitCode = 2
	}
	return probe, result
}

func finishCodexHookCanary(path string, summary CodexHookCanarySummary, result *Result) {
	_ = writeJSON(path, summary)
	if summary.Status != "PASS" {
		result.add("codex-hook-canary", "Codex hook canary status="+summary.Status)
	}
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

func writeCodexCanaryProfile(path, binary, payloadDir, formalHookOutput string) error {
	hookCommand := strings.Join([]string{
		quoteCommandArg(binary),
		"canary",
		"codex-hook-probe",
		"--payload-dir",
		quoteCommandArg(payloadDir),
		"--formal-hook-output",
		quoteCommandArg(formalHookOutput),
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
matcher = "*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = %s
timeout = 30
statusMessage = "formal-gates Codex hook canary pre tool"

[[hooks.PostToolUse]]
matcher = "*"
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
`, tomlString(hookCommand), tomlString(hookCommand), tomlString(hookCommand), tomlString(hookCommand))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func tomlString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}

func codexHookPrompt(binary, caseDir, markerPath string) string {
	record := strings.Join([]string{
		quoteCommandArg(binary),
		"workflow",
		"record-stage",
		"--worktree",
		quoteCommandArg(caseDir),
		"--gate",
		"complexity-gate",
		"--verdict",
		"PASS",
		"--mode",
		"formal",
		"--workflow-id",
		"hook-canary",
		"--change-snapshot",
		"hook-snapshot",
	}, " ")
	if runtime.GOOS == "windows" {
		return "Run exactly this shell command once, then stop: & " + record + "; Set-Content -LiteralPath " + quoteCommandArg(markerPath) + " -Value HIT"
	}
	return "Run exactly this shell command once, then stop: " + record + "; printf HIT > " + quoteCommandArg(markerPath)
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
		if value := scalarString(decoded["hook_event_name"]); value != "" {
			eventName = value
		}
		if value := scalarString(decoded["tool_name"]); value != "" {
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
