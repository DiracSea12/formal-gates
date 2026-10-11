//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/requirement"
	"formal-gates/internal/validate"
)

// canaryRequirementDocument confirms one complete requirement and solution.
// Product obligations belong to REQ/AC; implementation and development/repair
// boundaries remain in the solution for technical review and quality gates.
const canaryRequirementDocument = `# 交付

## 需求点

### REQ-001：usage 统计与公开 flag 契约

#### 要求
frob 是一个单命令工具。新增 usage 统计功能：在正常 frob 输出之后附加一行统计输出，格式精确为 usage: runs=1 words=N，其中 N 是本次所处理词的 UTF-8 字节数（空词为 0）。保持其他已存在的公开行为：首个词参数是本次所处理的词，额外普通参数不改变处理结果；空字符串是一个有效词参数；未提供词参数时以 2 退出，标准错误输出 usage: frob <word>，不产生 frob 输出或 usage 统计输出。公开 flag 契约（AC-002）属于本工具的需求边界：当前基线尚未满足该条款——frob 现接受任意首参数（含未知 flag）并以 0 退出，这是已知的既有缺口，不是已交付行为。最终交付必须满足该条款，不得放宽其拒绝和输出要求。
#### 验收条件
- AC-001：运行 frob word 后，精确输出 frob: word 行，随后是精确的 usage: runs=1 words=N 行，N 为该词的 UTF-8 字节数；空字符串参数为有效词，N 为 0。额外普通参数不改变首个词的结果。未提供词参数时保持退出码 2 和标准错误 usage: frob <word>，不产生 frob 输出或 usage 统计输出。
- AC-002：传入未知 flag（例如 --unknown-flag）时 frob 拒绝执行：以非零退出码退出，且不输出 frob: 行。
#### 来源
用户要求。

## 方案
使用现有 frob.go 命令入口。本次开发批次只交付 usage 统计功能：统计实现放在新文件 analytics.go，只计算输入词的 UTF-8 字节数并输出 usage 统计，由 frob.go 在正常 frob 行之后调用；空词仍作为一个参数处理。除 analytics.go 及其在 frob.go 中的调用接线外，本批次不改动其他行为。开发验证命令为 go build ./...。本开发批次不实现、修复或放宽 AC-002 的未知 flag 条款；其满足经本 run 的验证与返修闭环完成：验证判定其不满足时，修复 frob.go 的 flag 处理属本 run 已授权的返修范围。实现位置、调用接线及开发/返修改动边界由技术审、diff/代码审查与质量门核验，不要求黑盒 QA 检查源码；公开统计、参数和 flag 行为仍由 QA 验证。
`

// canaryFrobSource is the seeded frob command: it accepts any first argument,
// including unknown flags, and keeps running — the public-contract defect the
// first validation wave must really catch.
const canaryFrobSource = `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: frob <word>")
		os.Exit(2)
	}
	fmt.Printf("frob: %s\n", os.Args[1])
}
`

const canaryProvider = "zcode"

// engineCanary holds the private native workflow fixture and its original evidence.
type engineCanary struct {
	t              *testing.T
	binary, home   string
	nativeHost     string
	localPeer      bool
	run            facade.Run
	project        string
	vcs            *vcs.Provider
	promotion      *canaryPromotionRecovery
	jobs           string
	binding        map[string]any
	installed      validate.InstallReport
	separateRoots  map[string]string
	sourceBuildEnv []string
	split          bool

	revision string // confirmed requirement revision (bare hex)
	doc      []byte
}

// buildFixtureProject creates the native fixture: one baseline commit with
// the requirement document, the seeded frob command and no test files, so
// nothing exposes the seeded defect before validation runs.
func (c *engineCanary) buildFixtureProject() string {
	c.t.Helper()
	c.initFixtureRepository()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(c.project, name), []byte(content), 0o600); err != nil {
			c.t.Fatal(err)
		}
	}
	write(".gitignore", ".gates/\n")
	write("requirements.md", canaryRequirementDocument)
	write("frob.go", canaryFrobSource)
	write("go.mod", "module frob\n\ngo 1.22\n")
	return c.commitFixture([]string{".gitignore", "requirements.md", "frob.go", "go.mod"}, "frob baseline with requirement")
}

func (c *engineCanary) fail(format string, args ...any) {
	c.t.Helper()
	c.t.Fatalf(format, args...)
}

// canaryFailureEvidence requires a real failed QA case and its collected
// findings; an independent gate verdict does not prescribe the QA outcome.
func canaryFailureEvidence(wave *protocol.ValidationWave, findings map[string]*protocol.ValidationFinding) error {
	qaFailed := false
	for _, target := range wave.Targets {
		if target.Kind == "QA" && target.Status == protocol.OutcomeFail && target.Execution != nil {
			for _, record := range target.Execution.Records {
				if record.Result == coverage.ExecutedFail {
					qaFailed = true
				}
			}
		}
	}
	var rawFindings, executionFailures int
	for _, finding := range findings {
		if finding.WaveID != wave.ID {
			continue
		}
		if finding.Raw != nil {
			rawFindings++
		}
		if finding.ExecutionFailure != nil {
			executionFailures++
		}
	}
	if !qaFailed || rawFindings == 0 || executionFailures == 0 {
		return fmt.Errorf("first wave did not really fail: qaFailed=%t rawFindings=%d executionFailures=%d", qaFailed, rawFindings, executionFailures)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// workspace is a read-only use of the engine's issued native resource pointer.
func (c *engineCanary) workspace(input protocol.ValidationInput) string {
	c.t.Helper()
	if input.Workspace == nil || input.Workspace.Status != "READY" || input.Binding.WorkspaceID != input.Workspace.ID {
		c.fail("dispatch lacks its exact ready workspace")
	}
	return input.Workspace.Path
}

func (c *engineCanary) cli(input []byte, args ...string) []byte {
	if len(args) > 1 && args[0] == "workflow" && (args[1] == "drive" || args[1] == "submit") {
		args = append(args, "--single-step")
	}
	cmd := exec.Command(c.binary, args...)
	cmd.Dir = c.project
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = os.Environ()
	for key, value := range c.hostEnvironment() {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	started := time.Now()
	output, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		exit = 1
		if failure, ok := err.(*exec.ExitError); ok {
			exit = failure.ExitCode()
		}
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	record, _ := json.Marshal(map[string]any{"pid": pid, "argv": append([]string{c.binary}, args...), "cwd": cmd.Dir, "startedUTC": started.UTC().Format(time.RFC3339Nano), "elapsedSeconds": time.Since(started).Seconds(), "exit": exit, "input": string(input), "output": string(output)})
	journal, writeErr := os.OpenFile(filepath.Join(c.jobs, "installed-commands.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if writeErr != nil {
		c.fail("command evidence: %v", writeErr)
	}
	_, writeErr = journal.Write(append(record, '\n'))
	closeErr := journal.Close()
	if writeErr != nil || closeErr != nil {
		c.fail("command evidence write: %v %v", writeErr, closeErr)
	}
	if err != nil {
		if c.promotion != nil && c.promotion.recover(c, args, err) {
			return c.cli(input, args...)
		}
		c.fail("installed %v: %v %s", args, err, output)
	}
	return output
}
func (c *engineCanary) readRun(command string) facade.Run {
	return c.readRunID(command, c.currentRunID())
}
func (c *engineCanary) readRunID(command, runID string) facade.Run {
	var run facade.Run
	if err := json.Unmarshal(c.cli(nil, "workflow", command, "--root", c.project, "--run-id", runID), &run); err != nil {
		c.fail("decode %s: %v", command, err)
	}
	return run
}

// Candidate builds use the source environment captured before native fixture isolation.
func (c *engineCanary) buildInstalledCandidate(root string) {
	build := exec.Command("go", "build", "-o", c.binary, "./cmd/formal-gates")
	build.Dir = root
	build.Env = c.sourceBuildEnv
	if build.Env != nil {
		build.Env = append(slices.Clone(build.Env), "PWD="+root)
	}
	if output, err := build.CombinedOutput(); err != nil {
		c.fail("build installed candidate: %v %s", err, output)
	}
}

func (c *engineCanary) install(base string, intake facade.IntakeConfirmationReceipt) {
	root := c.separateRoots["source"]
	c.binary = filepath.Join(c.home, ".local", "bin", "formal-gates")
	if err := os.MkdirAll(filepath.Dir(c.binary), 0700); err != nil {
		c.fail("binary directory: %v", err)
	}
	c.buildInstalledCandidate(root)
	receipt, err := validate.PackageReceipt(root)
	if err != nil {
		c.fail("package receipt: %v", err)
	}
	source := c.t.TempDir()
	copyFile := func(from, to string, mode os.FileMode) {
		data, err := os.ReadFile(from)
		if err != nil {
			c.fail("read package: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			c.fail("package directory: %v", err)
		}
		if err := os.WriteFile(to, data, mode); err != nil {
			c.fail("package copy: %v", err)
		}
	}
	for _, entry := range receipt.Entries {
		rel := filepath.FromSlash(entry.Path)
		copyFile(filepath.Join(root, rel), filepath.Join(source, rel), os.FileMode(entry.Mode))
	}
	copyFile(c.binary, filepath.Join(source, "bin", "formal-gates"), 0755)
	args := []string{"install", "--source", source, "--host", c.nativeName(), "--scope", "global", "--binary-target", c.binary, "--force"}
	output := c.cli(nil, args...)
	begin := bytes.Index(output, []byte("\n{")) + 1
	if begin == 0 || json.Unmarshal(output[begin:], &c.installed) != nil || len(c.installed.Targets) != 1 {
		c.fail("install returned no single target report: %s", output)
	}
	for name, data := range map[string][]byte{
		"install.json":   output,
		"bootstrap.json": c.cli(nil, append(args, "--bootstrap")...),
	} {
		if err := os.WriteFile(filepath.Join(c.jobs, name), data, 0o600); err != nil {
			c.fail("save %s: %v", name, err)
		}
	}
	if c.installed.Targets[0].Host != c.nativeName() || c.installed.PackageDigest == "" || c.installed.Targets[0].InstalledDigest == "" {
		c.fail("installed host/package identity lost: %+v", c.installed)
	}
	if c.provider() == host.DeepSeek {
		patch, err := os.ReadFile(c.installed.Targets[0].HookConfig)
		plugin, pluginErr := os.ReadFile(filepath.Join(c.installed.Targets[0].TargetPath, "plugin", "formal-gates.mjs"))
		if err != nil || pluginErr != nil || !bytes.Contains(patch, []byte("./skills/formal-gates/plugin/formal-gates.mjs")) || !bytes.Contains(plugin, []byte("lifecycle")) {
			c.fail("candidate Cordis bridge readback: %v %v", err, pluginErr)
		}
	}
	if err := c.captureBinding(); err != nil {
		c.fail("capture private native binding: %v", err)
	}
	projection, precheckErr := requirement.ParseRequirementProjection("requirements.md", c.doc)
	if precheckErr != nil || projection.ContentDigest != intake.RequirementRevision || len(intake.Artifacts) != 1 || intake.Artifacts[0].Path != "requirements.md" || intake.SolutionRevision != intake.RequirementRevision {
		c.fail("confirmed canonical intake lost its single-document precheck: %v", precheckErr)
	}
	c.t.Logf("authorized canary user stand-in confirms this complete requirement/solution file before start: requirements.md revision=%s\n%s", intake.RequirementRevision, string(c.doc))
	path := filepath.Join(c.jobs, "intake.json")
	data, _ := json.Marshal(intake)
	if err := os.WriteFile(path, data, 0600); err != nil {
		c.fail("intake: %v", err)
	}
	c.promotion = installCanaryPromotionRecovery(c)
	c.cli(nil, "workflow", "start", "--root", c.project, "--run-id", "engine-canary", "--route", "regular", "--vcs", c.vcs.Name(), "--base-snapshot", base, "--intake-receipt", path)
	if err := c.recordIsolation(); err != nil {
		c.fail("candidate isolation proof: %v", err)
	}
}

func (c *engineCanary) nativeName() string {
	if c.nativeHost == "" {
		return canaryProvider
	}
	return c.nativeHost
}

func (c *engineCanary) provider() string {
	descriptor, err := host.Lookup(c.nativeName())
	if err != nil {
		c.fail("native host: %v", err)
	}
	return descriptor.ID
}

// prepareHome checks the prepared root before installation or capture. Host
// paths come from the native protocol and descriptor; account contents stay
// with the native owner.
func (c *engineCanary) prepareHome(candidate, stableHome, sourceRoot string) error {
	if strings.TrimSpace(candidate) == "" {
		if c.nativeName() == canaryProvider {
			return fmt.Errorf("canary requires a prepared ZCODE_DATA_BASE_DIR")
		}
		return fmt.Errorf("canary requires a prepared FORMAL_GATES_ENGINE_CANARY_HOME")
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("canary host root must be an existing directory: %s", candidate)
	}
	home, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return err
	}
	home = resolveNativePath(home)
	stable := *c
	stable.home = stableHome
	stablePaths := stable.hostPaths()
	separate := map[string]string{
		"source":      sourceRoot,
		"stable home": stableHome, "stable host": stablePaths["host"],
		"stable config":    stablePaths["config"],
		"stable install":   stablePaths["install"],
		"stable hook":      stablePaths["hook"],
		"stable rules":     stablePaths["rules"],
		"stable launcher":  filepath.Join(stableHome, ".local", "bin", "formal-gates"),
		"stable registry":  filepath.Join(stableHome, ".formal-gates", "registry.json"),
		"stable state":     filepath.Join(stableHome, ".gates"),
		"stable resources": filepath.Join(stableHome, ".formal-gates-resources"),
	}
	for _, key := range []string{"USERPROFILE", "ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_SESSION_DB_PATH", "ZCODE_SESSION_DB", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, zcodePersonalConfigEnv, zcodeCredentialHelperEnv} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			separate["stable "+key] = path
		}
	}
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CURSOR_CONFIG_DIR", "CURSOR_DATA_DIR", "NODE_COMPILE_CACHE", "DSH_HOME"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			separate["stable "+key] = path
		}
	}
	for name, path := range separate {
		separate[name] = canaryCanonicalPath(path)
		if canaryPathWithin(home, path) || canaryPathWithin(path, home) {
			return fmt.Errorf("canary host root overlaps %s: %s", name, separate[name])
		}
	}
	c.home = home
	for name, path := range c.hostPaths() {
		if !canaryPathWithin(home, path) {
			return fmt.Errorf("canary %s leaves the prepared host root: %s", name, resolveNativePath(path))
		}
	}
	c.separateRoots = separate
	return nil
}

// canaryPathWithin compares canonical paths using path components. Existing
// paths resolve symlinks; prospective paths inherit their nearest real parent.
func canaryPathWithin(root, path string) bool {
	relative, err := filepath.Rel(canaryCanonicalPath(root), canaryCanonicalPath(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func canaryCanonicalPath(path string) string {
	path = resolveNativePath(path)
	if _, err := os.Lstat(path); os.IsNotExist(err) && filepath.Dir(path) != path {
		return filepath.Join(canaryCanonicalPath(filepath.Dir(path)), filepath.Base(path))
	}
	return resolveNativePath(path)
}

func (c *engineCanary) hostPaths() map[string]string {
	descriptor, _ := host.Lookup(c.nativeName())
	paths := map[string]string{
		"home":      c.home,
		"hook":      filepath.Join(c.home, descriptor.Paths.GlobalHookConfig),
		"rules":     filepath.Join(c.home, descriptor.Paths.GlobalManaged),
		"install":   filepath.Join(c.home, descriptor.Paths.GlobalBase, "formal-gates"),
		"launcher":  filepath.Join(c.home, ".local", "bin", "formal-gates"),
		"registry":  filepath.Join(c.home, ".formal-gates", "registry.json"),
		"state":     filepath.Join(c.home, ".gates"),
		"resources": filepath.Join(c.home, ".formal-gates-resources"),
	}
	switch descriptor.ID {
	case host.Claude:
		paths["host"], paths["config"] = filepath.Join(c.home, ".claude"), filepath.Join(c.home, ".claude")
	case host.Codex:
		paths["host"], paths["config"] = filepath.Join(c.home, ".codex"), filepath.Join(c.home, ".codex")
	case host.Cursor:
		paths["host"], paths["config"] = filepath.Join(c.home, ".cursor"), filepath.Join(c.home, ".cursor")
		paths["data"], paths["cache"] = filepath.Join(c.home, ".cursor", "data"), filepath.Join(c.home, ".cursor", "cache")
	case host.DeepSeek:
		paths["host"], paths["config"] = filepath.Join(c.home, ".dsh"), filepath.Join(c.home, ".dsh")
		paths["install"] = filepath.Join(paths["host"], "skills", "formal-gates")
		paths["hook"], paths["rules"] = filepath.Join(paths["host"], "cordis.patch.yml"), filepath.Join(paths["host"], "AGENTS.md")
	case host.ZCode:
		hostRoot := filepath.Join(c.home, ".zcode")
		config := filepath.Join(hostRoot, "v2")
		for key, value := range map[string]string{
			"host": hostRoot, "config": config,
			"personal": filepath.Join(config, zcodePersonalConfig), "setting": filepath.Join(config, zcodeSettingFile),
			"credentials": filepath.Join(config, zcodeCredentialsFile), "runtime": filepath.Join(config, "runtime"),
			"catalogue": filepath.Join(config, "runtime", "provider", "zcode-builtin.json"),
			"log":       filepath.Join(hostRoot, "log"), "mailbox": filepath.Join(hostRoot, "mailbox"),
			"sessionDatabase": filepath.Join(hostRoot, "cli", "db", "db.sqlite"),
		} {
			paths[key] = value
		}
	}
	return paths
}

// hostEnvironment pins private writable paths for the installed CLI and
// native binding. Host detection keys come from the lifecycle registry.
// ZCode's script layout owns its read-only bundled release; the installed
// target supplies the credential helper and the active catalogue stays private.
func (c *engineCanary) hostEnvironment() map[string]string {
	paths := c.hostPaths()
	if c.nativeName() != canaryProvider {
		environment := map[string]string{"HOME": c.home, "USERPROFILE": c.home,
			"PATH": filepath.Dir(paths["launcher"]) + string(os.PathListSeparator) + os.Getenv("PATH")}
		for _, key := range lifecycle.ProviderEnvironmentKeys() {
			environment[key] = ""
		}
		switch c.provider() {
		case host.Claude:
			environment["CLAUDE_CONFIG_DIR"] = paths["config"]
		case host.Codex:
			environment["CODEX_HOME"] = paths["config"]
		case host.Cursor:
			environment["CURSOR_CONFIG_DIR"], environment["CURSOR_DATA_DIR"], environment["NODE_COMPILE_CACHE"] = paths["config"], paths["data"], paths["cache"]
		case host.DeepSeek:
			environment["DSH_HOME"], environment["DSH_PROJECT_DIR"] = paths["host"], c.project
		}
		return environment
	}
	script, _ := getOf(getOf(c.binding, "launch"), "script").(string)
	if script == "" {
		var configured bool
		script, configured = os.LookupEnv("FORMAL_GATES_ZCODE_SCRIPT")
		if !configured {
			script = zcodeDefaultScript
		}
	}
	script = resolveNativePath(zcodeExpandUser(script))
	bundled := zcodeBundledProviderCatalogue(script)
	directory := filepath.Dir(script)
	for _, path := range []string{
		filepath.Join(directory, "provider", "zcode-builtin.json"),
		filepath.Join(directory, "..", "..", "..", "..", "..", "config", "provider", "zcode-builtin.json"),
	} {
		if zcodeIsRegularFile(path) {
			bundled = resolveNativePath(path)
			break
		}
	}
	searchPath := os.Getenv("PATH")
	binPrefix := filepath.Dir(paths["launcher"]) + string(os.PathListSeparator)
	if !strings.HasPrefix(searchPath, binPrefix) {
		searchPath = binPrefix + searchPath
	}
	environment := map[string]string{
		"HOME": c.home, "USERPROFILE": c.home, "ZCODE_DATA_BASE_DIR": c.home,
		"ZCODE_HOME": paths["host"], "ZCODE_STORAGE_DIR": paths["host"],
		"ZCODE_SESSION_DB_PATH": paths["sessionDatabase"], "ZCODE_SESSION_DB": paths["sessionDatabase"],
		"ZCODE_LOG_DIR": paths["log"], "ZCODE_MAILBOX_ROOT": paths["mailbox"],
		"FORMAL_GATES_ZCODE_SCRIPT":                  script,
		zcodeBuiltinCatalogueEnv:                     paths["catalogue"],
		"ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE": bundled,
		zcodePersonalConfigEnv:                       paths["personal"], zcodeCredentialHelperEnv: paths["credentials"],
		"PATH":     searchPath,
		"AI_AGENT": "", "CODEX_HOME": "", "DSH_HOME": "", "DSH_PROJECT_DIR": "",
	}
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		environment[key] = ""
	}
	return environment
}

func (c *engineCanary) captureBinding() error {
	for key, value := range c.hostEnvironment() {
		c.t.Setenv(key, value)
	}
	impl, err := nativeImplementationFor(c.nativeName())
	if err != nil {
		return err
	}
	binding, err := impl.capture()
	if err != nil {
		return err
	}
	environment := objectOrEmpty(binding["environment"])
	for key, value := range c.hostEnvironment() {
		environment[key] = value
	}
	binding["environment"] = environment
	c.binding = binding
	return nil
}

// recordIsolation is path evidence in the existing jobs directory. The
// install report remains the authority for the target and package identity.
// Missing runtime paths are labelled prospective, never claimed observed.
func (c *engineCanary) recordIsolation() error {
	paths := c.hostPaths()
	paths["project"] = c.project
	paths["projectState"] = filepath.Join(c.project, ".gates")
	paths["jobs"] = c.jobs
	target := c.installed.Targets[0]
	paths["install"], paths["launcher"], paths["hook"], paths["rules"] = target.TargetPath, target.LauncherPath, target.HookConfig, target.ManagedRulePath
	for name, path := range paths {
		if path == "" {
			delete(paths, name)
		}
	}
	observed := map[string]bool{}
	for name, path := range paths {
		paths[name] = canaryCanonicalPath(path)
		_, err := os.Stat(path)
		observed[name] = err == nil
		for separateName, separateRoot := range c.separateRoots {
			if canaryPathWithin(separateRoot, path) || canaryPathWithin(path, separateRoot) {
				return fmt.Errorf("candidate %s overlaps %s", name, separateName)
			}
		}
	}
	bundled := zcodeLaunchEnvironment(c.binding)["ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE"]
	if bundled != "" {
		bundled = canaryCanonicalPath(bundled)
	}
	return writeAtomicJSON(filepath.Join(c.jobs, "host-isolation.json"), map[string]any{
		"provider": c.provider(), "nativeHost": c.nativeName(), "separateRoots": c.separateRoots, "candidate": paths,
		"observed": observed, "installReport": c.installed, "binding": c.binding["launch"],
		"nativeRuntime": map[string]any{"bundledCatalogue": bundled, "bundledCatalogueObserved": zcodeIsRegularFile(bundled)},
	})
}

func (c *engineCanary) currentRunID() string { return firstNonEmpty(c.run.RunID, "engine-canary") }

func canaryValuesEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
