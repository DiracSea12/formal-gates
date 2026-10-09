package acceptance_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/validate"
)

// installedStartProject uses a complete package and a real Git repository.
// Hooks are skipped: these tests exercise the public process boundary and
// submitted protocol observations without starting a host model.
type installedStartProject struct {
	binary, home, source, project, intakePath, base string
	intake                                          facade.IntakeConfirmationReceipt
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
	write := func(name, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(p.project, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		return hex.EncodeToString(sum[:])
	}
	requirement := write("requirements.md", "# Application\nREQ-001: Review the application through the installed native workflow.\nAC-001: Keep the admitted host and native base.\n")
	solution := write("design.md", "# Design\nUse the admitted installation and the native Git provider.\n")
	write("app.go", "package main\nfunc main() {}\n")
	write(".gitignore", ".gates/\n.formal-gates-resources/\n.zcode/\n.codex/\nAGENTS.md\n")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Installed workflow"}, {"config", "user.email", "workflow@example.invalid"}, {"add", "."}, {"commit", "-qm", "Application"}} {
		p.git(t, args...)
	}
	p.base = p.git(t, "rev-parse", "HEAD")
	p.intake = facade.IntakeConfirmationReceipt{
		Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: requirement,
		Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: requirement}, {Path: "design.md", Revision: solution}},
		SolutionRevision: solution, SolutionDigest: "sha256:" + solution,
	}
	p.intakePath = filepath.Join(t.TempDir(), "intake.json")
	writeInstalledJSON(t, p.intakePath, p.intake)
	return p
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
	if len(args) > 1 && args[0] == "workflow" && (args[1] == "drive" || args[1] == "submit") {
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

func (p installedStartProject) startArgs(runID, route string) []string {
	args := []string{"workflow", "start", "--root", p.project, "--run-id", runID, "--intake-receipt", p.intakePath}
	if route != "" {
		args = append(args, "--route", route)
	}
	if route == "regular" {
		args = append(args, "--base-snapshot", p.base)
	}
	return args
}

func (p installedStartProject) run(t *testing.T, command, runID string) facade.Run {
	t.Helper()
	output := p.call(t, nil, "workflow", command, "--root", p.project, "--run-id", runID)
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
	p.call(t, nil, p.startArgs("native-review", "regular")...)
	request, _ := p.bindings(t, "native-review", "zcode")
	if request.BaseSnapshot != p.base || request.Route != "regular" {
		t.Fatalf("native request=%+v", request)
	}
	statePath := filepath.Join(p.project, facade.EngineNamespace, "native-review", "state.json")
	before := readInstalledFile(t, statePath)
	for _, command := range []string{"show", "status", "next", "diagnose"} {
		p.call(t, nil, "workflow", command, "--root", p.project, "--run-id", "native-review")
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
	driven := p.run(t, "drive", "native-review")
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
	wrongData := installedJSON(t, wrong)
	before = readInstalledFile(t, statePath)
	output, err := p.command(wrongData, "workflow", "submit", "--root", p.project, "--run-id", "native-review").CombinedOutput()
	if err == nil || !strings.Contains(string(output), string(protocol.CodeProviderMismatch)) {
		t.Fatalf("wrong host receipt accepted: err=%v output=%s", err, output)
	}
	after = readInstalledFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("wrong host receipt changed state")
	}
	data := installedJSON(t, event)
	p.call(t, data, "workflow", "submit", "--root", p.project, "--run-id", "native-review")
	_, state := p.bindings(t, "native-review", "zcode")
	if receipt := state.SpawnReceipts[actions[0]]; receipt.Provider != "zcode" || receipt.Correlation != "declared-worker" || receipt.Status != protocol.SpawnStatusSpawned {
		t.Fatalf("submitted receipt lost its identity: %+v", receipt)
	}
	// A second installed host shares the launcher. Reopening selects the run's
	// exact record; a sibling's host never replaces its persisted binding.
	p.install(t, "codex", "project")
	p.call(t, nil, p.startArgs("project-review", "regular")...)
	projectRequest, _ := p.bindings(t, "project-review", "codex")
	if projectRequest.InstalledTargetIdentity == request.InstalledTargetIdentity {
		t.Fatal("global and project installations shared a record identity")
	}
	for _, item := range []struct{ run, provider string }{{"native-review", "zcode"}, {"project-review", "codex"}} {
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
	for _, command := range []string{"show", "drive", "submit"} {
		output, err := p.command(data, "workflow", command, "--root", p.project, "--run-id", "native-review").CombinedOutput()
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
	assertRejected := func(name string, args []string, want string) {
		t.Helper()
		output, err := p.command(nil, args...).CombinedOutput()
		if err == nil || !strings.Contains(string(output), want) {
			t.Fatalf("%s: err=%v output=%s, want %s", name, err, output, want)
		}
		for _, namespace := range []string{facade.EngineNamespace, ".gates/tmp"} {
			if _, err := os.Stat(filepath.Join(p.project, namespace, name)); !os.IsNotExist(err) {
				t.Fatalf("%s created state in %s: %v", name, namespace, err)
			}
		}
	}
	assertRejected("uninstalled", p.startArgs("uninstalled", "regular"), "UNREGISTERED_INSTALL")
	p.install(t, "zcode", "global")
	missingBase := p.startArgs("missing-base", "regular")
	assertRejected("missing-base", missingBase[:len(missingBase)-2], "requires an immutable native base snapshot")
	wrongBase := p.startArgs("wrong-base", "regular")
	wrongBase[len(wrongBase)-1] = strings.Repeat("f", 40)
	assertRejected("wrong-base", wrongBase, "snapshot")
	for _, route := range []string{"default", "full", "custom"} {
		assertRejected(route, p.startArgs(route, route), "workflow start supports --route regular or lightweight")
	}
	for _, flag := range []string{"--split=no", "--master=parent", "--retained-overall=false", "--from-sealed-run=sealed"} {
		name := strings.Split(strings.TrimPrefix(flag, "--"), "=")[0]
		assertRejected(name, append(p.startArgs(name, "regular"), flag), "flag provided but not defined")
	}
	if err := os.WriteFile(filepath.Join(p.project, "design.md"), []byte("# Revised solution\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertRejected("stale-intake", p.startArgs("stale-intake", "regular"), facade.InvalidIntakeConfirmation)
	if head := p.git(t, "rev-parse", "HEAD"); head != p.base {
		t.Fatalf("rejected starts changed native history: %s", head)
	}
}

func TestInstalledLightweightAndRegularStartRecovery(t *testing.T) {
	p := newInstalledStartProject(t)
	p.install(t, "zcode", "global")
	// Lightweight keeps its bootstrap default and needs no native repository.
	if err := os.RemoveAll(filepath.Join(p.project, ".git")); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"", "lightweight"} {
		runID := "record-only"
		if route != "" {
			runID = "explicit-record"
		}
		p.call(t, nil, append(p.startArgs(runID, route), "--base-snapshot", "not-a-native-snapshot")...)
		request, _ := p.bindings(t, runID, "zcode")
		if request.Route != "lightweight" || request.BaseSnapshot != "" {
			t.Fatalf("lightweight acquired native semantics: %+v", request)
		}
		if run := p.run(t, "drive", runID); run.Next.Kind != decision.KindComplete || !run.Unverified {
			t.Fatalf("lightweight completion=%+v", run)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Installed workflow"}, {"config", "user.email", "workflow@example.invalid"}, {"add", "."}, {"commit", "-qm", "Application"}} {
		p.git(t, args...)
	}
	p.base = p.git(t, "rev-parse", "HEAD")
	p.call(t, nil, p.startArgs("native-reference", "regular")...)
	referenceRequest, referenceState := p.bindings(t, "native-reference", "zcode")
	// Construct the durable prefix of Start's request-before-state window.
	// Recovery removes that prefix and uses the same public Start owner.
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
	p.call(t, nil, p.startArgs(runID, "regular")...)
	recoveredRequest, recoveredState := p.bindings(t, runID, "zcode")
	if _, err := os.Stat(dir + ".start.intent.json"); !os.IsNotExist(err) {
		t.Fatalf("recovery left start intent: %v", err)
	}
	referenceRequest.RunID = runID
	referenceState.RunID = runID
	if !reflect.DeepEqual(referenceRequest, recoveredRequest) || !reflect.DeepEqual(referenceState, recoveredState) {
		t.Fatal("normal and recovered starts produced different request/protocol bindings")
	}
	// Concurrent same-id starts are serialized. The rejected invocation cannot
	// remove the established run or alter its admitted identity.
	args := p.startArgs("native-concurrent", "regular")
	commands := []*exec.Cmd{p.command(nil, args...), p.command(nil, args...)}
	var outputs [2]bytes.Buffer
	for i, cmd := range commands {
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	successes := 0
	for i, cmd := range commands {
		if err := cmd.Wait(); err == nil {
			successes++
		} else if !strings.Contains(outputs[i].String(), "already exists") && !strings.Contains(outputs[i].String(), "LOCK_HELD") {
			t.Fatalf("concurrent start: %v\n%s", err, outputs[i].String())
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent start successes=%d", successes)
	}
	p.bindings(t, "native-concurrent", "zcode")
	statePath := filepath.Join(p.project, facade.EngineNamespace, "native-concurrent", "state.json")
	before := readInstalledFile(t, statePath)
	if output, err := p.command(nil, args...).CombinedOutput(); err == nil || !strings.Contains(string(output), "already exists") {
		t.Fatalf("repeated start: err=%v output=%s", err, output)
	}
	after := readInstalledFile(t, statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("repeated start changed established state")
	}
}

// Submit reads its typed event only after the installed CLI has opened the run.
// Filling that input pipe with JSON whitespace proves the process crossed that
// boundary while leaving the business event pending until normal reinstall.
func TestInstalledSubmitRejectsTokenChangedAfterOpen(t *testing.T) {
	p := newInstalledStartProject(t)
	p.install(t, "zcode", "global")
	runID := "delayed-submit"
	p.call(t, nil, p.startArgs(runID, "regular")...)
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
	event, err := protocol.NewRequestEvent("delayed-abort", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(input).Encode(event); err != nil {
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
	p.call(t, nil, p.startArgs("current-installation", "regular")...)
	p.bindings(t, "current-installation", "zcode")
}
