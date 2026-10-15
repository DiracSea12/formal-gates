package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	_ "formal-gates/internal/harness" // Register native adapters for trusted program Start recovery.
	"formal-gates/internal/validate"
)

// installedStartProject uses a complete package and a real Git repository.
// Hooks are skipped: controls and native effects cross the installed process
// boundary; simulated host observations use the typed facade without a model.
type installedStartProject struct {
	binary, home, source, project, base string
	intakePosition                      int
	preview                             facade.IntakePreview
	intake                              facade.IntakeConfirmationReceipt
}

func newInstalledStartProject(t *testing.T) installedStartProject {
	t.Helper()
	repository, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	p := installedStartProject{home: t.TempDir(), project: t.TempDir()}
	p.binary = executablePath(filepath.Join(p.home, ".local", "bin", "formal-gates"))
	buildInstalledBinaryAt(t, repository, "./cmd/formal-gates", p.binary)
	p.source = isolatedInstallSource(t, repository, p.binary)
	quality, err := os.ReadFile(filepath.Join(p.source, "gates", "implementation-quality-gate.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.source, "gates", "quality.md"), quality, 0600); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(p.project, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("requirements.md", "# Application\n\n## 需求点\n\n### REQ-001：Installed native workflow\n\n#### 要求\nReview the application through the installed native workflow.\n\n#### 验收条件\n- AC-001：Keep the admitted host and native base.\n\n#### 来源\nUser contract.\n")
	write("design.md", "# Design\nUse the admitted installation and the native Git provider.\n")
	write("app.go", "package main\nfunc main() {}\n")
	write(".gitignore", ".gates/\n.formal-gates-resources/\n.zcode/\n.codex/\nAGENTS.md\n")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Installed workflow"}, {"config", "user.email", "workflow@example.invalid"}, {"add", "."}, {"commit", "-qm", "Application"}} {
		p.git(t, args...)
	}
	p.base = p.git(t, "rev-parse", "HEAD")
	p.prepareIntake(t, "requirements.md")
	return p
}

func (p *installedStartProject) prepareIntake(t *testing.T, requirementPath string) {
	t.Helper()
	output := p.call(t, nil, "workflow", "start", "--machine", "--precheck", "--root", p.project, "--requirement", requirementPath, "--solution", "design.md")
	var response struct {
		facade.IntakePreview
		Position int `json:"intake"`
	}
	if err := json.Unmarshal(output, &response); err != nil || response.Position < 1 {
		t.Fatalf("prepare original intake: %v %s", err, output)
	}
	p.preview, p.intake, p.intakePosition = response.IntakePreview, response.IntakeCandidate, response.Position
}

func (p installedStartProject) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = p.project
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func (p installedStartProject) command(input []byte, args ...string) *exec.Cmd {
	if len(args) > 1 && args[0] == "workflow" && (args[1] == "drive" || args[1] == "submit") && !slices.Contains(args, "--single-step=false") {
		args = append(args, "--single-step")
	}
	cmd := exec.Command(p.binary, args...)
	cmd.Dir = p.project
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = append(os.Environ(), "HOME="+p.home, "USERPROFILE="+p.home, "AI_AGENT=", "CLAUDE_CODE_ENTRYPOINT=", "CODEX_HOME=", "CODEX_CLI_PATH=", "CURSOR_TRACE_ID=", "CURSOR_RUNTIME=", "DSH_HOME=", "DSH_PROJECT_DIR=")
	return cmd
}

func (p installedStartProject) call(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	output, err := p.command(input, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("installed %v: %v\n%s", args, err, output)
	}
	return output
}

func (p installedStartProject) install(t *testing.T, host, scope string) {
	t.Helper()
	args := []string{"install", "--source", p.source, "--host", host, "--scope", scope, "--binary-target", p.binary, "--force", "--skip-hooks"}
	if scope == "project" {
		args = append(args, "--project", p.project)
	}
	p.call(t, nil, args...)
	p.call(t, nil, append(args, "--bootstrap")...)
	path := filepath.Join(p.home, ".formal-gates", "registry.json")
	data, err := os.ReadFile(path + ".bootstrap.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt validate.BootstrapReceipt
	if err := json.Unmarshal(data, &receipt); err != nil || !receipt.Accepted || receipt.StateCreated || len(receipt.Records) == 0 || receipt.PackageDigest == "" {
		t.Fatalf("actual bootstrap receipt: %+v err=%v", receipt, err)
	}
}

// confirmation simulates the host's native form reply over the installed MCP
// process. Its observations prove protocol identity, not a human UI interaction.
func (p installedStartProject) confirmation(t *testing.T, tool string, arguments any, check func(map[string]any), environment ...string) ([]byte, error) {
	t.Helper()
	cmd := p.command(nil, "workflow", "serve")
	cmd.Env = append(cmd.Env, environment...)
	cmd.Stdin = nil
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer cmd.Process.Kill()
	body, exchangeErr := exchangeInstalledConfirmation(input, output, tool, arguments, check)
	_ = input.Close()
	waitErr := cmd.Wait()
	if exchangeErr != nil {
		return body, fmt.Errorf("%w; service stderr=%s", exchangeErr, &stderr)
	}
	if waitErr != nil {
		return body, fmt.Errorf("%w; service stderr=%s", waitErr, &stderr)
	}
	return body, nil
}

func exchangeInstalledConfirmation(input io.Writer, output io.Reader, tool string, arguments any, check func(map[string]any)) ([]byte, error) {
	encoder, decoder := json.NewEncoder(input), json.NewDecoder(output)
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		return nil, err
	}
	var initialized struct {
		ID     int `json:"id"`
		Result struct {
			ServerInfo any `json:"serverInfo"`
		} `json:"result"`
	}
	if err := decoder.Decode(&initialized); err != nil {
		return nil, err
	}
	if initialized.ID != 1 || initialized.Result.ServerInfo == nil {
		return nil, fmt.Errorf("service initialization lost its original identity: %+v", initialized)
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": arguments}}); err != nil {
		return nil, err
	}
	for {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Mode    string `json:"mode"`
				Message string `json:"message"`
				Schema  struct {
					Required   []string `json:"required"`
					Properties map[string]struct {
						Type    string `json:"type"`
						Default any    `json:"default"`
					} `json:"properties"`
				} `json:"requestedSchema"`
			} `json:"params"`
			Error  any `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := decoder.Decode(&frame); err != nil {
			return nil, err
		}
		if frame.Method != "" {
			field := frame.Params.Schema.Properties["confirmOperation"]
			if frame.Method != "elicitation/create" || len(frame.ID) == 0 || frame.Params.Mode != "form" || !reflect.DeepEqual(frame.Params.Schema.Required, []string{"confirmOperation"}) || field.Type != "boolean" || field.Default != nil {
				return nil, fmt.Errorf("service omitted the original explicit confirmation form: %+v", frame)
			}
			var original map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(frame.Params.Message, "Confirm this exact operation:\n")), &original); err != nil {
				return nil, err
			}
			if check != nil {
				check(original)
			}
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": map[string]any{"action": "accept", "content": map[string]any{"confirmOperation": true}}}); err != nil {
				return nil, err
			}
			continue
		}
		if string(frame.ID) != "2" {
			return nil, fmt.Errorf("response lost original tool call identity: %s", frame.ID)
		}
		if frame.Error != nil {
			return nil, fmt.Errorf("%v", frame.Error)
		}
		var body []byte
		for _, content := range frame.Result.Content {
			if content.Type == "text" {
				body = append(body, content.Text...)
			}
		}
		if frame.Result.IsError {
			return body, fmt.Errorf("%s", body)
		}
		return body, nil
	}
}

func (p installedStartProject) start(t *testing.T, route string, base ...string) string {
	t.Helper()
	displayedID := ""
	expectedBase := p.base
	if route == "lightweight" {
		expectedBase = ""
	} else if len(base) != 0 && base[0] != "" {
		expectedBase = base[0]
	}
	arguments := map[string]any{"root": p.project, "intake": p.intakePosition, "route": route, "baseSnapshot": p.base}
	if len(base) != 0 {
		arguments["baseSnapshot"] = base[0]
	}
	body, err := p.confirmation(t, "confirm_start", arguments, func(original map[string]any) {
		var materials facade.IntakePreview
		data := installedJSON(t, original["materials"])
		if err := json.Unmarshal(data, &materials); err != nil || materials.Content != p.preview.Content || materials.Revision != p.preview.Revision || !reflect.DeepEqual(materials.Materials, p.preview.Materials) || !reflect.DeepEqual(materials.IntakeCandidate, p.preview.IntakeCandidate) || original["root"] != p.project || original["route"] != route || original["baseSnapshot"] != expectedBase {
			t.Fatalf("start form changed its original source, revision, project or route: %v %s", err, data)
		}
		displayedID, _ = original["runId"].(string)
		if displayedID == "" {
			t.Fatal("start form omitted its generated run identity")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.RunID == "" || response.RunID != displayedID {
		t.Fatalf("start response changed its displayed run identity: %v %s", err, body)
	}
	return response.RunID
}

func (p installedStartProject) run(t *testing.T, command, runID string) facade.Run {
	t.Helper()
	output := p.call(t, nil, "workflow", command, "--machine", "--root", p.project, "--run-id", runID)
	var run facade.Run
	if err := json.Unmarshal(output, &run); err != nil {
		t.Fatalf("decode %s: %v\n%s", command, err, output)
	}
	return run
}

func (p installedStartProject) bindings(t *testing.T, runID, provider string) (facade.StartRequest, protocol.State) {
	t.Helper()
	dir := filepath.Join(p.project, facade.EngineNamespace, runID)
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	document, err := validate.LoadRegistry(filepath.Join(p.home, ".formal-gates", "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record validate.RegistryRecord
	for _, candidate := range document.Records {
		if candidate.ID == request.InstalledTargetIdentity {
			record = candidate
		}
	}
	if request.Provider != provider || record.Host != provider || request.PackageDigest != record.PackageDigest || request.AdmissionGeneration != record.Generation || request.AdmissionLease != record.Lease || request.AdmissionToken != record.Token {
		t.Fatalf("request lost exact admitted identity: request=%+v record=%+v", request, record)
	}
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: record.PackageDigest, InstalledTargetIdentity: record.ID, DefinitionSource: request.DefinitionSource})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var state protocol.State
	if err := json.Unmarshal(snapshot.Content, &state); err != nil {
		t.Fatal(err)
	}
	if state.RunProvider != provider || state.BaseSnapshot != request.BaseSnapshot || state.Route != request.Route || !reflect.DeepEqual(state.IntakeConfirmationReceipt, &request.IntakeConfirmationReceipt) {
		t.Fatalf("protocol lost request bindings: provider=%s base=%s route=%s intake=%+v", state.RunProvider, state.BaseSnapshot, state.Route, state.IntakeConfirmationReceipt)
	}
	if digest, err := facade.IntakeDigest(*state.IntakeConfirmationReceipt); err != nil || digest != state.IntakeConfirmationReceipt.Digest {
		t.Fatalf("intake content digest=%s err=%v", digest, err)
	}
	return request, state
}

func writeInstalledJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.WriteFile(path, installedJSON(t, value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installedJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readInstalledFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstalledRegularStartCarriesHostThroughPublicOperations(t *testing.T) {
	p := newInstalledStartProject(t)
	p.install(t, "zcode", "global")
	runID := p.start(t, "regular")
	request, _ := p.bindings(t, runID, "zcode")
	if request.BaseSnapshot != p.base || request.Route != "regular" {
		t.Fatalf("native request=%+v", request)
	}
	statePath := filepath.Join(p.project, facade.EngineNamespace, runID, "state.json")
	before := readInstalledFile(t, statePath)
	for _, command := range []string{"show", "status", "next", "diagnose"} {
		p.call(t, nil, "workflow", command, "--root", p.project, "--run-id", runID)
	}
	repository, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	p.call(t, nil, "workflow", "guard-preview", "--input", filepath.Join(repository, "references", "guard-preview-basis.json"))
	after := readInstalledFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("public queries changed the authoritative state")
	}
	driven := p.run(t, "drive", runID)
	if driven.Next.Kind != decision.KindWait || driven.Next.Wait.Reason != decision.WaitTasksInFlight || len(driven.PendingActions) == 0 || driven.IntakeReceipt == nil || driven.IntakeReceipt.IntakeDigest != request.IntakeConfirmationReceipt.Digest {
		t.Fatalf("regular drive did not reach the external worker boundary: %+v", driven)
	}
	actions := make([]string, 0, len(driven.PendingActions))
	for action := range driven.PendingActions {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	event, err := protocol.NewSpawnReceiptEvent("spawn-observation", actions[0], "zcode", "declared-worker", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	wrong := event
	wrong.Spawn = &protocol.SpawnReceiptPayload{ActionID: actions[0], Provider: "codex", Correlation: "declared-worker", Status: protocol.SpawnStatusSpawned}
	opened, err := facade.Open(p.project, runID)
	if err != nil {
		t.Fatal(err)
	}
	before = readInstalledFile(t, statePath)
	_, _, err = opened.Submit(wrong)
	if err == nil || !strings.Contains(err.Error(), string(protocol.CodeProviderMismatch)) {
		t.Fatalf("wrong host receipt accepted: %v", err)
	}
	after = readInstalledFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("wrong host receipt changed state")
	}
	if _, _, err = opened.Submit(event); err != nil {
		t.Fatal(err)
	}
	_, state := p.bindings(t, runID, "zcode")
	if receipt := state.SpawnReceipts[actions[0]]; receipt.Provider != "zcode" || receipt.Correlation != "declared-worker" || receipt.Status != protocol.SpawnStatusSpawned {
		t.Fatalf("submitted receipt lost its identity: %+v", receipt)
	}
	// A second installed host shares the launcher. Reopening selects the run's
	// exact record; a sibling's host never replaces its persisted binding.
	p.install(t, "codex", "project")
	projectRunID := p.start(t, "regular")
	projectRequest, _ := p.bindings(t, projectRunID, "codex")
	if projectRequest.InstalledTargetIdentity == request.InstalledTargetIdentity {
		t.Fatal("global and project installations shared a record identity")
	}
	for _, item := range []struct{ run, provider string }{{runID, "zcode"}, {projectRunID, "codex"}} {
		p.run(t, "show", item.run)
		p.run(t, "drive", item.run)
		p.bindings(t, item.run, item.provider)
	}
	if head := p.git(t, "rev-parse", "HEAD"); head != p.base {
		t.Fatalf("start/queries/drive/submit changed native history: %s != %s", head, p.base)
	}
	// Reinstallation advances the registry fence. Old installed requests cannot
	// use either the common read entry or the common writers to mutate the run.
	before = readInstalledFile(t, statePath)
	p.install(t, "zcode", "global")
	data := installedJSON(t, facade.ControlInput{Request: protocol.ControlAbort})
	for _, command := range []string{"show", "drive", "submit"} {
		output, err := p.command(data, "workflow", command, "--root", p.project, "--run-id", runID).CombinedOutput()
		if err == nil || !strings.Contains(string(output), "UNREGISTERED_INSTALL") {
			t.Fatalf("old fence %s: err=%v output=%s", command, err, output)
		}
	}
	if after = readInstalledFile(t, statePath); !bytes.Equal(before, after) {
		t.Fatal("old installation fence changed state")
	}
}

func TestInstalledStartRejectsInvalidInputsBeforeState(t *testing.T) {
	p := newInstalledStartProject(t)
	namespaces := func() map[string][]string {
		t.Helper()
		names := map[string][]string{}
		for _, namespace := range []string{facade.EngineNamespace, ".gates/tmp"} {
			entries, err := os.ReadDir(filepath.Join(p.project, namespace))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, entry := range entries {
				names[namespace] = append(names[namespace], entry.Name())
			}
		}
		return names
	}
	assertRejected := func(name string, args map[string]any, want string) {
		t.Helper()
		before := namespaces()
		output, err := p.confirmation(t, "confirm_start", args, func(original map[string]any) { t.Fatal("invalid start reached host confirmation", original) })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err=%v output=%s, want %s", name, err, output, want)
		}
		if !reflect.DeepEqual(before, namespaces()) {
			t.Fatalf("%s created a run or state namespace", name)
		}
	}
	arguments := func(route string) map[string]any {
		return map[string]any{"root": p.project, "intake": p.intakePosition, "route": route}
	}
	assertRejected("uninstalled", arguments("regular"), "UNREGISTERED_INSTALL")
	p.install(t, "zcode", "global")
	runID := p.start(t, "regular", "")
	request, _ := p.bindings(t, runID, "zcode")
	if request.BaseSnapshot != p.base {
		t.Fatalf("program resolved base=%q want native HEAD %q", request.BaseSnapshot, p.base)
	}
	t.Logf("isolated home=%s project=%s resolved original native HEAD=%s", p.home, p.project, request.BaseSnapshot)
	wrongBase := arguments("regular")
	wrongBase["baseSnapshot"] = strings.Repeat("f", 40)
	assertRejected("wrong-base", wrongBase, "snapshot")
	assertRejected("missing-route", arguments(""), "workflow start requires --route regular or lightweight")
	assertRejected("unknown-route", arguments("unsupported"), "workflow start supports --route regular or lightweight")
	for _, field := range []string{"split", "master", "retainedOverall", "fromSealedRun", "runId"} {
		args := arguments("regular")
		args[field] = "agent-authored"
		assertRejected(field, args, "unknown field")
	}
	if output, err := p.command(nil, "workflow", "start", "--root", p.project, "--run-id", "agent-authored", "--intake", fmt.Sprint(p.intakePosition), "--route", "regular").CombinedOutput(); err == nil || !strings.Contains(string(output), "NATIVE_CONFIRMATION_UNAVAILABLE") {
		t.Fatalf("agent-authored start bypassed the native channel: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(p.project, "design.md"), []byte("# Revised solution\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertRejected("stale-intake", arguments("regular"), facade.InvalidIntakeConfirmation)
	if head := p.git(t, "rev-parse", "HEAD"); head != p.base {
		t.Fatalf("rejected starts changed native history: %s", head)
	}
}

func TestInstalledLightweightAndRegularStartRecovery(t *testing.T) {
	p := newInstalledStartProject(t)
	p.install(t, "zcode", "global")
	// Lightweight is an explicit start choice and needs no native repository.
	if err := os.RemoveAll(filepath.Join(p.project, ".git")); err != nil {
		t.Fatal(err)
	}
	lightweightID := p.start(t, "lightweight", "not-a-native-snapshot")
	request, _ := p.bindings(t, lightweightID, "zcode")
	if request.Route != "lightweight" || request.BaseSnapshot != "" {
		t.Fatalf("lightweight acquired native semantics: %+v", request)
	}
	if run := p.run(t, "drive", lightweightID); run.Next.Kind != decision.KindComplete || !run.Unverified {
		t.Fatalf("lightweight completion=%+v", run)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Installed workflow"}, {"config", "user.email", "workflow@example.invalid"}, {"add", "."}, {"commit", "-qm", "Application"}} {
		p.git(t, args...)
	}
	p.base = p.git(t, "rev-parse", "HEAD")
	referenceID := p.start(t, "regular")
	referenceRequest, referenceState := p.bindings(t, referenceID, "zcode")
	// Construct the durable prefix of Start's request-before-state window.
	// Recovery uses the trusted program Start API for that exact issued identity.
	runID := "native-recovered"
	dir := filepath.Join(p.project, facade.EngineNamespace, runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	partial := referenceRequest
	partial.RunID = runID
	writeInstalledJSON(t, filepath.Join(dir, "request.json"), partial)
	writeInstalledJSON(t, dir+".start.intent.json", map[string]any{"runId": runID, "admission": facade.Admission{
		RegistryPath: partial.AdmissionRegistryPath, Target: partial.AdmissionTarget, Root: partial.AdmissionRoot, Provider: partial.Provider,
		PackageDigest: partial.PackageDigest, InstalledTargetIdentity: partial.InstalledTargetIdentity,
		Generation: partial.AdmissionGeneration, Lease: partial.AdmissionLease, Token: partial.AdmissionToken,
	}, "definitionSource": partial.DefinitionSource, "definitionDigest": partial.DefinitionDigest})
	admission := facade.Admission{
		RegistryPath: partial.AdmissionRegistryPath, Target: partial.AdmissionTarget, Root: partial.AdmissionRoot, Provider: partial.Provider,
		PackageDigest: partial.PackageDigest, InstalledTargetIdentity: partial.InstalledTargetIdentity,
		Generation: partial.AdmissionGeneration, Lease: partial.AdmissionLease, Token: partial.AdmissionToken,
	}
	options := facade.StartOptions{Root: p.project, ArtifactRoot: p.project, Request: partial, Admission: &admission, Settings: referenceState.Settings}
	if partial.RuntimeFiles != nil {
		options.RuntimeFiles = *partial.RuntimeFiles
	}
	if _, _, err := facade.Start(options); err != nil {
		t.Fatal(err)
	}
	recoveredRequest, recoveredState := p.bindings(t, runID, "zcode")
	if _, err := os.Stat(dir + ".start.intent.json"); !os.IsNotExist(err) {
		t.Fatalf("recovery left start intent: %v", err)
	}
	referenceTime, err := time.Parse(time.RFC3339Nano, referenceState.StartedAt)
	if err != nil {
		t.Fatal("normal start lacks its actual start time", err)
	}
	recoveredTime, err := time.Parse(time.RFC3339Nano, recoveredState.StartedAt)
	if err != nil || recoveredTime.Before(referenceTime) {
		t.Fatal("recovered initialization lacks its actual start time", recoveredState.StartedAt, err)
	}
	p.run(t, "show", runID)
	_, reopenedState := p.bindings(t, runID, "zcode")
	if reopenedState.StartedAt != recoveredState.StartedAt {
		t.Fatal("reopen changed the original recovered start time")
	}
	// Independent initializations have separate clocks; all binding values match.
	referenceState.StartedAt = recoveredState.StartedAt
	referenceRequest.RunID = runID
	referenceState.RunID = runID
	if !reflect.DeepEqual(referenceRequest, recoveredRequest) || !reflect.DeepEqual(referenceState, recoveredState) {
		t.Fatal("normal and recovered starts produced different request/protocol bindings")
	}
	// Program callers can retry an issued identity; serialization preserves it.
	options.Request.RunID = "native-concurrent"
	var results [2]error
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(i int) { defer workers.Done(); _, _, results[i] = facade.Start(options) }(i)
	}
	workers.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "LOCK_HELD") {
			t.Fatal("concurrent program start", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent program start successes=%d", successes)
	}
	p.bindings(t, options.Request.RunID, "zcode")
	statePath := filepath.Join(p.project, facade.EngineNamespace, options.Request.RunID, "state.json")
	before := readInstalledFile(t, statePath)
	if _, _, err := facade.Start(options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal("repeated program start", err)
	}
	if after := readInstalledFile(t, statePath); !bytes.Equal(before, after) {
		t.Fatal("repeated program start changed established state")
	}
	// Independent public starts each receive their own program-generated identity.
	var publicIDs [2]string
	for i := range publicIDs {
		workers.Add(1)
		go func(i int) { defer workers.Done(); publicIDs[i] = p.start(t, "regular") }(i)
	}
	workers.Wait()
	if publicIDs[0] == "" || publicIDs[1] == "" || publicIDs[0] == publicIDs[1] {
		t.Fatalf("public start identities=%v", publicIDs)
	}
	for _, id := range publicIDs {
		p.bindings(t, id, "zcode")
	}
}

// Submit reads its typed event only after the installed CLI has opened the run.
// Filling that input pipe with JSON whitespace proves the process crossed that
// boundary while leaving the business event pending until normal reinstall.
func TestInstalledSubmitRejectsTokenChangedAfterOpen(t *testing.T) {
	p := newInstalledStartProject(t)
	p.install(t, "zcode", "global")
	runID := p.start(t, "regular")
	p.run(t, "drive", runID)
	request, _ := p.bindings(t, runID, "zcode")
	files := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		for _, dir := range []string{filepath.Join(p.project, facade.EngineNamespace, runID), filepath.Join(p.project, ".gates", "tmp", runID)} {
			err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					result[path] = string(readInstalledFile(t, path))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	before := files()
	cmd := p.command(nil, "workflow", "submit", "--root", p.project, "--run-id", runID)
	cmd.Stdin = nil
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	readPending := make(chan error, 1)
	go func() {
		_, err := input.Write(bytes.Repeat([]byte(" "), 2*1024*1024))
		readPending <- err
	}()
	select {
	case err := <-readPending:
		if err != nil {
			t.Fatalf("CLI did not reach event input after Open: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CLI did not consume event input after Open")
	}
	p.install(t, "zcode", "global")
	document, err := validate.LoadRegistry(request.AdmissionRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	fenced := false
	for _, record := range document.Records {
		if record.ID == request.InstalledTargetIdentity {
			fenced = record.Generation != request.AdmissionGeneration && record.Token != request.AdmissionToken
		}
	}
	if !fenced {
		t.Fatal("normal reinstall did not advance the owning token")
	}
	if err := json.NewEncoder(input).Encode(facade.ControlInput{Request: protocol.ControlAbort}); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil || !strings.Contains(output.String(), "STALE_FENCING_TOKEN") {
		t.Fatalf("retained CLI token: err=%v output=%s", err, &output)
	}
	if !reflect.DeepEqual(before, files()) {
		t.Fatal("stale CLI changed owning state, intent, receipt or sidecar bytes")
	}
	currentID := p.start(t, "regular")
	p.bindings(t, currentID, "zcode")
}
