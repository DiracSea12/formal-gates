package panel

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	_ "formal-gates/internal/harness" // native platform capabilities, as the CLI binary registers them
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

const panelRequirement = "# Delivery\n\n## 需求点\n\n### REQ-001：命令\n\n#### 要求\n命令验证输入。\n\n#### 验收条件\n- AC-001：正常输入成功，错误输入拒绝。\n\n#### 来源\n用户要求。\n"

func panelHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func userConfigPath(home string) string {
	return filepath.Join(home, ".config", "formal-gates", "config.json")
}

func projectConfigPath(root string) string {
	return filepath.Join(root, ".gates", "config.json")
}

func putJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func overlayBytes(t *testing.T, root, home string) (user, project []byte) {
	t.Helper()
	user, _ = os.ReadFile(userConfigPath(home))
	project, _ = os.ReadFile(projectConfigPath(root))
	return user, project
}

func panelRevisions(t *testing.T, b Backend) map[string]string {
	t.Helper()
	revisions, err := settings.Revisions(b.Root, b.Home)
	if err != nil {
		t.Fatal(err)
	}
	return revisions
}

var panelRuntime struct {
	once sync.Once
	data []byte
	err  error
}

func panelFixtureRuntime(t *testing.T) []byte {
	t.Helper()
	panelRuntime.once.Do(func() {
		source, err := filepath.Abs("../..")
		if err != nil {
			panelRuntime.err = err
			return
		}
		name := "formal-gates"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(os.TempDir(), "panel-fixture-"+name)
		command := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
		command.Dir = source
		if output, err := command.CombinedOutput(); err != nil {
			panelRuntime.err = fmt.Errorf("build fixture runtime: %w: %s", err, output)
			return
		}
		panelRuntime.data, panelRuntime.err = os.ReadFile(binary)
	})
	if panelRuntime.err != nil {
		t.Fatal(panelRuntime.err)
	}
	return panelRuntime.data
}

// panelAdmission installs a registered fixture runtime: the testkit registry
// record plus the real prompt/gate package and a compiled runtime binary.
func panelAdmission(t *testing.T, root, recordID, provider string) (*facade.Admission, string) {
	t.Helper()
	registryPath, target, generation, lease, token := testkit.Installation(t, root, recordID, "sha256:package", provider)
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"prompts", "gates"} {
		copyTree(t, filepath.Join(source, directory), filepath.Join(target, directory))
	}
	name := "formal-gates"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(filepath.Join(target, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "bin", name), panelFixtureRuntime(t), 0o700); err != nil {
		t.Fatal(err)
	}
	quality, err := os.ReadFile(filepath.Join(source, "gates", "implementation-quality-gate.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"quality", "security", "performance"} {
		if err := os.WriteFile(filepath.Join(target, "gates", gate+".md"), quality, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &facade.Admission{RegistryPath: registryPath, Target: target, Root: root,
		Provider: provider, PackageDigest: "sha256:package", InstalledTargetIdentity: recordID,
		Generation: generation, Lease: lease, Token: token}, registryPath
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(source, path)
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func panelReceipt(root, requirementPath string) facade.IntakeConfirmationReceipt {
	data, err := os.ReadFile(filepath.Join(root, requirementPath))
	if err != nil {
		return facade.IntakeConfirmationReceipt{}
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(data))
	return facade.IntakeConfirmationReceipt{
		Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport,
		RequirementSource: requirementPath, RequirementRevision: revision,
		Artifacts:        []facade.IntakeArtifact{{Path: requirementPath, Revision: revision}},
		SolutionRevision: revision, SolutionDigest: "sha256:" + revision,
	}
}

func panelGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// panelRun starts a real bound run: a saved project configuration, a git
// baseline, the native admission fixture, and a drive through facade.Start.
func panelRun(t *testing.T, home string) (string, *facade.Facade, facade.Run) {
	t.Helper()
	root := t.TempDir()
	testkit.ConfigureNativePaths(t, root, "zcode")
	putJSON(t, filepath.Join(root, "requirements.md"), panelRequirement)
	putJSON(t, filepath.Join(root, "app.go"), "package main\nfunc main() {}\n")
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	panelGit(t, root, "init", "-q")
	panelGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "add", ".")
	panelGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Initial command")
	base := panelGit(t, root, "rev-parse", "HEAD")
	admission, _ := panelAdmission(t, root, "panel-target", "zcode")
	f, run, err := facade.Start(facade.StartOptions{Root: root,
		Request: facade.StartRequest{RunID: "panel-run", Provider: "zcode", VCS: "git",
			BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource,
			DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: panelReceipt(root, "requirements.md")},
		Admission: admission, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	return root, f, run
}

// panelBoundRun starts an additional run in an existing panelRun root, bound
// to a freshly saved configuration of the given name through the same
// facade.Start path.
func panelBoundRun(t *testing.T, root, home, runID, configName string) facade.Run {
	t.Helper()
	if _, err := settings.SaveConfiguration(root, home, "project", configName, "", nil); err != nil {
		t.Fatal(err)
	}
	base := panelGit(t, root, "rev-parse", "HEAD")
	admission, _ := panelAdmission(t, root, runID+"-target", "zcode")
	_, run, err := facade.Start(facade.StartOptions{Root: root, Configuration: configName,
		Request: facade.StartRequest{RunID: runID, Provider: "zcode", VCS: "git",
			BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource,
			DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: panelReceipt(root, "requirements.md")},
		Admission: admission, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// claimFirstMaterial drives the run and marks its first issued worker claimed
// exactly as the supervisor leaves it on disk: the augmented native request
// beside the owner marker inside the owned job directory.
func claimFirstMaterial(t *testing.T, f *facade.Facade) {
	t.Helper()
	driven, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	for _, material := range driven.WorkerMaterials {
		request, supervised, directory, _, err := f.WorkerExecutionRequest(material, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, payload := range map[string]map[string]any{"native-request.json": request, "request.json": supervised} {
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, name), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(directory, "owner"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("drive issued no worker materials")
}

func nodeState(states []protocol.ExecutionNodeState, node string) protocol.ExecutionNodeState {
	for _, state := range states {
		if state.Node == node {
			return state
		}
	}
	return protocol.ExecutionNodeState{Node: node}
}

func TestRunSummaryRequirementUsesIntakeSourceBasename(t *testing.T) {
	got := runSummary(facade.Run{RunID: "r1", IntakeConfirmationReceipt: &facade.IntakeConfirmationReceipt{RequirementSource: "docs/需求说明.md"}})
	if got.Requirement != "需求说明.md" {
		t.Fatalf("nested requirement source = %q", got.Requirement)
	}
	if got := runSummary(facade.Run{RunID: "r1"}); got.Requirement != "" {
		t.Fatalf("missing receipt must leave requirement empty: %q", got.Requirement)
	}
	if got := runSummary(facade.Run{RunID: "r1", IntakeConfirmationReceipt: &facade.IntakeConfirmationReceipt{}}); got.Requirement != "" {
		t.Fatalf("empty requirement source must leave requirement empty: %q", got.Requirement)
	}
}

func TestPanelStateReadsMergedLibraryBindingsAndRuns(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, userConfigPath(home), `{"configurations":{"用户层":{"qaModes":["blackbox"]}}}`)
	if _, err := settings.SaveConfiguration(root, home, "project", "项目层", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "项目层", nil); err != nil {
		t.Fatal(err)
	}
	b := Backend{Root: root, Home: home}
	state, err := b.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Active != "项目层" || len(state.Configurations) != 4 || len(state.Runs) != 0 || len(state.Bindings) != 0 {
		t.Fatalf("empty library projection = %+v", state)
	}
	sources := map[string]string{}
	for _, option := range state.Configurations {
		sources[option.Name] = option.Source
	}
	if sources["默认"] != "default" || sources["用户层"] != userConfigPath(home) || sources["项目层"] != projectConfigPath(root) {
		t.Fatalf("merged sources = %v", sources)
	}
	if state.Revisions["user"] == "" || state.Revisions["project"] == "" {
		t.Fatalf("revisions = %v", state.Revisions)
	}

	root, _, run := panelRun(t, home)
	b = Backend{Root: root, Home: home}
	state, err = b.State()
	if err != nil {
		t.Fatal(err)
	}
	if run.Configuration.Name != "工作" || len(state.Runs) != 1 || state.Runs[0].RunID != run.RunID || state.Runs[0].Status != "ACTIVE" || state.Runs[0].Configuration != "工作" {
		t.Fatalf("run projection = %+v", state.Runs)
	}
	if state.Runs[0].Requirement != "requirements.md" {
		t.Fatalf("run requirement label = %q", state.Runs[0].Requirement)
	}
	if len(state.Runs[0].Nodes) == 0 || !nodeState(state.Runs[0].Nodes, "product-review").Owed {
		t.Fatalf("run node states = %+v", state.Runs[0].Nodes)
	}
	if !slices.Contains(state.Bindings["工作"], run.RunID) {
		t.Fatalf("bindings = %v", state.Bindings)
	}
}

func TestFullRouteBuiltinBindsByNameAcrossActivationAndReopen(t *testing.T) {
	home := panelHome(t)
	root, _, original := panelRun(t, home)
	base := panelGit(t, root, "rev-parse", "HEAD")
	admission, _ := panelAdmission(t, root, "full-route-target", "zcode")
	_, run, err := facade.Start(facade.StartOptions{Root: root, Configuration: settings.FullConfigurationName,
		Request: facade.StartRequest{RunID: "full-route-run", Provider: "zcode", VCS: "git", BaseSnapshot: base,
			DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: panelReceipt(root, "requirements.md")},
		Admission: admission, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if run.Configuration.Name != settings.FullConfigurationName || !run.Configuration.Configuration.QAModeEnabled("blackbox") || !run.Configuration.Configuration.QAModeEnabled("whitebox") || run.Configuration.Configuration.Gates != nil {
		t.Fatalf("bound full route = %+v", run.Configuration)
	}
	if _, err := settings.UseConfiguration(root, home, "project", settings.DefaultConfigurationName, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := facade.Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := reopened.Show()
	if err != nil || shown.Configuration.Name != settings.FullConfigurationName || !shown.Configuration.Configuration.QAModeEnabled("blackbox") || !shown.Configuration.Configuration.QAModeEnabled("whitebox") {
		t.Fatalf("bound name changed with active pointer = %+v %v", shown.Configuration, err)
	}
	state, err := (Backend{Root: root, Home: home}).State()
	if err != nil || !slices.Contains(state.Bindings[settings.FullConfigurationName], run.RunID) || !slices.Contains(state.Bindings["工作"], original.RunID) {
		t.Fatalf("configuration bindings = %+v %v", state.Bindings, err)
	}
}

func TestPanelStateReportsEmptyProjectAndDanglingPointer(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	state, err := b.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Error != "" || state.Active != "默认" || len(state.Configurations) != 2 || len(state.Runs) != 0 {
		t.Fatalf("empty projection = %+v", state)
	}
	putJSON(t, projectConfigPath(root), `{"configuration":"不存在","configurations":{"甲":{}}}`)
	state, err = b.State()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Error, `configuration "不存在" does not exist`) {
		t.Fatalf("dangling pointer error = %q", state.Error)
	}
	if len(state.Configurations) != 3 {
		t.Fatalf("dangling projection fell back: %+v", state.Configurations)
	}
}

func TestPanelSaveWritesCompleteDraftAndRespectsOwners(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	putJSON(t, userConfigPath(home), `{"configurations":{"用户层":{"qaModes":["blackbox"]}},"configuration":"用户层"}`)
	beforeUser, beforeProject := overlayBytes(t, root, home)
	revisions := panelRevisions(t, b)
	draft := settings.Configuration{
		FlowSelection: &settings.FlowSelection{Disabled: []string{"repair"}},
		QAModes:       []string{"blackbox", "whitebox"},
		Execution:     map[string]settings.ExecutionChoice{"development": {Provider: "zcode", Model: "m"}},
		RoundLimits:   map[string]int{"product-review": 1, "tech-review": 2, "qa-blackbox": 3, "qa-whitebox": 4, "repair": 5},
	}
	if _, err := b.Save("project", "完整", draft, false, revisions, ""); err != nil {
		t.Fatal(err)
	}
	_, options, err := settings.LoadConfigurations(root, home)
	if err != nil {
		t.Fatal(err)
	}
	var saved *settings.ConfigurationOption
	for i := range options {
		if options[i].Name == "完整" {
			saved = &options[i]
		}
	}
	if saved == nil || saved.Source != projectConfigPath(root) {
		t.Fatalf("saved option = %+v", saved)
	}
	if !slices.Equal(saved.Configuration.FlowSelection.Disabled, []string{"repair"}) ||
		!slices.Equal(saved.Configuration.QAModes, []string{"blackbox", "whitebox"}) ||
		saved.Configuration.Execution["development"].Model != "m" ||
		saved.Configuration.Gates != nil ||
		saved.Configuration.RoundLimits["repair"] != 5 {
		t.Fatalf("saved value = %+v", saved.Configuration)
	}
	effective, _, err := settings.LoadConfigurations(root, home)
	if err != nil || effective.ConfigurationName != "用户层" {
		t.Fatalf("active pointer moved: %v %v", effective.ConfigurationName, err)
	}

	// Overwrite keeps the scope owner; other layers are untouched.
	draft.QAModes = []string{"whitebox"}
	if _, err := b.Save("project", "完整", draft, true, panelRevisions(t, b), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Save("user", "完整", draft, true, panelRevisions(t, b), ""); err == nil {
		t.Fatal("wrong-scope overwrite wrote through")
	}
	if _, err := b.Save("project", "默认", draft, true, panelRevisions(t, b), ""); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("default overwrite = %v", err)
	}
	if _, err := b.Save("project", "   ", draft, false, panelRevisions(t, b), ""); err == nil {
		t.Fatal("whitespace name wrote")
	}
	afterUser, _ := overlayBytes(t, root, home)
	if string(afterUser) != string(beforeUser) || len(beforeProject) != 0 {
		t.Fatal("rejections touched overlay bytes")
	}

	// Copy uses the locked automatic names; Use writes the project pointer.
	created, err := b.Copy("project", "完整", panelRevisions(t, b))
	if err != nil || created == "完整" {
		t.Fatalf("copy = %q %v", created, err)
	}
	if _, err := b.Use("用户层", panelRevisions(t, b)); err != nil {
		t.Fatal(err)
	}
	effective, _, err = settings.LoadConfigurations(root, home)
	if err != nil || effective.ConfigurationName != "用户层" || effective.ConfigurationSource != userConfigPath(home) {
		t.Fatalf("use = %q %q %v", effective.ConfigurationName, effective.ConfigurationSource, err)
	}
}

func TestPanelLibraryRejectsBoundRenameAndDelete(t *testing.T) {
	home := panelHome(t)
	root, _, run := panelRun(t, home)
	b := Backend{Root: root, Home: home}
	beforeUser, beforeProject := overlayBytes(t, root, home)
	if _, err := b.Rename("project", "工作", "新名", panelRevisions(t, b)); err == nil {
		t.Fatal("bound rename wrote")
	}
	if _, err := b.Delete("project", "工作", panelRevisions(t, b)); err == nil {
		t.Fatal("bound delete wrote")
	}
	if _, err := b.Rename("project", "缺失", "新名", panelRevisions(t, b)); err == nil {
		t.Fatal("missing rename wrote")
	}
	afterUser, afterProject := overlayBytes(t, root, home)
	if string(beforeUser) != string(afterUser) || string(beforeProject) != string(afterProject) {
		t.Fatal("bound rejections changed overlay bytes")
	}
	if run.Configuration.Name != "工作" {
		t.Fatalf("fixture binding = %q", run.Configuration.Name)
	}
}

func TestPanelSaveGuardsBoundRunResponsibilities(t *testing.T) {
	home := panelHome(t)
	root, f, run := panelRun(t, home)
	b := Backend{Root: root, Home: home}
	beforeUser, beforeProject := overlayBytes(t, root, home)
	current := run.Configuration.Configuration

	// Completed responsibilities are locked: repair is enabled yet never owed
	// work in this run, so its fields cannot change.
	locked := settings.CloneConfiguration(current)
	locked.Execution = map[string]settings.ExecutionChoice{"repair": {Provider: "zcode", Model: "late"}}
	if _, err := b.Save("project", "工作", locked, true, panelRevisions(t, b), run.RunID); err == nil {
		t.Fatal("completed responsibility change wrote")
	}
	afterUser, afterProject := overlayBytes(t, root, home)
	if string(beforeUser) != string(afterUser) || string(beforeProject) != string(afterProject) {
		t.Fatal("locked rejection changed overlay bytes")
	}

	// A disabled node is a configuration state, not a completed verdict:
	// enabling blackbox QA writes and takes effect at the next dispatch.
	enabledQA := settings.CloneConfiguration(current)
	enabledQA.QAModes = []string{"blackbox"}
	if _, err := b.Save("project", "工作", enabledQA, true, panelRevisions(t, b), run.RunID); err != nil {
		t.Fatalf("disabled-node enable rejected: %v", err)
	}
	_, savedProject := overlayBytes(t, root, home)

	// The gate owes future work, so development cannot be closed under it.
	tail := settings.CloneConfiguration(current)
	tail.FlowSelection = &settings.FlowSelection{Disabled: []string{"development"}}
	if _, err := b.Save("project", "工作", tail, true, panelRevisions(t, b), run.RunID); err == nil {
		t.Fatal("closed development under an owed gate wrote")
	}

	// Disabling every owed enabled node leaves the run nothing to dispatch.
	stuck := settings.CloneConfiguration(current)
	stuck.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development", "gate"}}
	if _, err := b.Save("project", "工作", stuck, true, panelRevisions(t, b), run.RunID); err == nil {
		t.Fatal("zero-dispatchable candidate wrote")
	}
	_, afterProject = overlayBytes(t, root, home)
	if string(savedProject) != string(afterProject) {
		t.Fatal("stuck rejection changed overlay bytes")
	}

	// A still-editable responsibility saves.
	editable := settings.CloneConfiguration(current)
	editable.Execution = map[string]settings.ExecutionChoice{"product-review": {Provider: "zcode", Model: "fresh"}}
	if _, err := b.Save("project", "工作", editable, true, panelRevisions(t, b), run.RunID); err != nil {
		t.Fatalf("editable save rejected: %v", err)
	}
	effective, options, err := settings.LoadConfigurations(root, home)
	if err != nil {
		t.Fatal(err)
	}
	var saved *settings.ConfigurationOption
	for i := range options {
		if options[i].Name == "工作" {
			saved = &options[i]
		}
	}
	if saved == nil || saved.Configuration.Execution["product-review"].Model != "fresh" || effective.ConfigurationName != "工作" {
		t.Fatalf("saved value = %+v active=%q", saved, effective.ConfigurationName)
	}

	// A non-bound save under a different name does not touch the binding.
	if _, err := b.Save("project", "旁路", editable, false, panelRevisions(t, b), run.RunID); err != nil {
		t.Fatalf("nonbound save rejected: %v", err)
	}
	bindings, err := facade.BoundConfigurations(root)
	if err != nil || !slices.Equal(bindings["工作"], []string{run.RunID}) || len(bindings["旁路"]) != 0 {
		t.Fatalf("nonbound save rebound: %v", bindings)
	}

	// Once an owed node is claimed its fields lock; a candidate that leaves
	// the locked responsibilities unchanged still saves.
	claimFirstMaterial(t, f)
	claimed, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	running := ""
	for _, state := range claimed.NodeStates {
		if state.Running {
			running = state.Node
		}
	}
	if running == "" {
		t.Fatalf("claim did not mark any node running: %+v", claimed.NodeStates)
	}
	taken := settings.CloneConfiguration(current)
	taken.Execution = map[string]settings.ExecutionChoice{running: {Provider: "zcode", Model: "taken"}}
	if _, err := b.Save("project", "工作", taken, true, panelRevisions(t, b), run.RunID); err == nil {
		t.Fatal("claimed responsibility change wrote")
	}

	// An unchanged candidate still saves over the claimed obligation: the
	// claimed owed node still counts as dispatchable work.
	_, options, err = settings.LoadConfigurations(root, home)
	if err != nil {
		t.Fatal(err)
	}
	var latest settings.Configuration
	for _, option := range options {
		if option.Name == "工作" {
			latest = option.Configuration
		}
	}
	if latest.Execution["product-review"].Model != "fresh" {
		t.Fatalf("library value before claimed save = %+v", latest.Execution)
	}
	if _, err := b.Save("project", "工作", latest, true, panelRevisions(t, b), run.RunID); err != nil {
		t.Fatalf("unchanged candidate over a claimed obligation rejected: %v", err)
	}
	_, options, err = settings.LoadConfigurations(root, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		if option.Name == "工作" && option.Configuration.Execution["product-review"].Model != "fresh" {
			t.Fatal("claimed write leaked")
		}
	}
}

func TestGuardCandidateStatesRejectsClosedDevelopmentWithOwedTail(t *testing.T) {
	states := []protocol.ExecutionNodeState{
		{Node: "development", Enabled: false},
		{Node: "qa-blackbox", Enabled: true, Owed: true, Dispatchable: true},
	}
	if err := guardCandidateStates(states); err == nil || !strings.Contains(err.Error(), "development") {
		t.Fatalf("owed QA with closed development = %v", err)
	}
	states[0].Enabled = true
	if err := guardCandidateStates(states); err != nil {
		t.Fatalf("owed QA with open development rejected: %v", err)
	}
	states[1].Dispatchable = false
	if err := guardCandidateStates(states); err == nil {
		t.Fatal("no dispatchable node accepted")
	}
}

func TestPanelIntakeCheckReportsSupersededAndSelection(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, filepath.Join(root, "requirements.md"), panelRequirement)
	b := Backend{Root: root, Home: home}
	candidate := filepath.Join(root, "intake-candidate.json")
	writeReceipt := func(receipt facade.IntakeConfirmationReceipt) {
		data, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		putJSON(t, candidate, string(data))
	}
	writeReceipt(panelReceipt(root, "requirements.md"))

	check, err := b.CheckIntake(candidate, "默认")
	if err != nil || check.Superseded || check.Preview == nil || check.Selection == nil {
		t.Fatalf("valid check = %+v %v", check, err)
	}
	if check.Selection.Configuration != "默认" || len(check.Selection.Disabled) != 0 {
		t.Fatalf("selection = %+v", check.Selection)
	}

	// A candidate already consumed by another run stays valid: the check owns
	// artifact freshness, not run history.
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	testkit.ConfigureNativePaths(t, root, "zcode")
	putJSON(t, filepath.Join(root, "app.go"), "package main\nfunc main() {}\n")
	panelGit(t, root, "init", "-q")
	panelGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "add", ".")
	panelGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Initial")
	base := panelGit(t, root, "rev-parse", "HEAD")
	admission, _ := panelAdmission(t, root, "panel-intake", "zcode")
	if _, _, err := facade.Start(facade.StartOptions{Root: root,
		Request: facade.StartRequest{RunID: "intake-consumer", Provider: "zcode", VCS: "git",
			BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource,
			DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: panelReceipt(root, "requirements.md")},
		Admission: admission}); err != nil {
		t.Fatal(err)
	}
	check, err = b.CheckIntake(candidate, "默认")
	if err != nil || check.Superseded {
		t.Fatalf("used candidate = %+v %v", check, err)
	}

	putJSON(t, filepath.Join(root, "requirements.md"), "# changed\n\n"+panelRequirement)
	check, err = b.CheckIntake(candidate, "默认")
	if err != nil || !check.Superseded || check.Reason == "" {
		t.Fatalf("changed artifact = %+v %v", check, err)
	}
	check, err = b.CheckIntake(filepath.Join(root, "missing.json"), "默认")
	if err != nil || !check.Superseded {
		t.Fatalf("missing candidate = %+v %v", check, err)
	}

	// A malformed requirement fails the structured check even when its
	// bindings are fresh.
	putJSON(t, filepath.Join(root, "broken.md"), "# nothing here\n")
	writeReceipt(panelReceipt(root, "broken.md"))
	receipt := panelReceipt(root, "broken.md")
	receipt.RequirementSource = "broken.md"
	data, _ := json.Marshal(receipt)
	putJSON(t, candidate, string(data))
	check, err = b.CheckIntake(candidate, "默认")
	if err == nil || check.Superseded {
		t.Fatalf("malformed candidate = %+v %v", check, err)
	}
}

func TestPanelIntakeCheckRejectsAllDisabledConfiguration(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, filepath.Join(root, "requirements.md"), "# plain notes, no requirement structure\n")
	b := Backend{Root: root, Home: home}
	disabled := settings.Configuration{FlowSelection: &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development", "qa", "gate", "repair"}}}
	if _, err := settings.WriteConfiguration(root, home, "project", "全禁", disabled, false, nil); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "intake-candidate.json")
	data, _ := json.Marshal(panelReceipt(root, "requirements.md"))
	putJSON(t, candidate, string(data))
	check, err := b.CheckIntake(candidate, "全禁")
	if err == nil || !strings.Contains(err.Error(), "at least one enabled") {
		t.Fatalf("all-disabled check = %+v %v", check, err)
	}
}

func TestPanelEditSettingRestrictsToPanelFlags(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	putJSON(t, projectConfigPath(root), `{"panelIdleTimeout":120}`)
	revisions := panelRevisions(t, b)
	if _, err := b.EditPanelSetting("panelIdleTimeout", "600", revisions); err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil || effective.Values.PanelIdleTimeout != 600 {
		t.Fatalf("idle timeout = %v %v", effective.Values.PanelIdleTimeout, err)
	}
	if _, err := b.EditPanelSetting("panelSelection", "false", panelRevisions(t, b)); err != nil {
		t.Fatal(err)
	}
	effective, err = settings.Load(root, home)
	if err != nil || effective.Values.PanelSelection {
		t.Fatalf("selection flag = %v %v", effective.Values.PanelSelection, err)
	}
	if _, err := b.EditPanelSetting("capacity", "9", panelRevisions(t, b)); err == nil {
		t.Fatal("non-panel flag wrote")
	}
	if _, err := b.EditPanelSetting("panelIdleTimeout", "bogus", panelRevisions(t, b)); err == nil {
		t.Fatal("invalid integer wrote")
	}
}

func TestPanelExecutionOptionsAndGatesNeedRegisteredRuntime(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	if _, err := b.Gates(); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("gates without runtime = %v", err)
	}
	if _, err := b.ExecutionOptions("默认", facade.ExecutionSelectionInput{Node: "development"}); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("options without runtime = %v", err)
	}
	state, err := b.State()
	if err != nil || state.Error != "" {
		t.Fatalf("library state failed with missing runtime: %+v %v", state, err)
	}

	// A registered runtime resolves candidates minus the QA gates. The record
	// needs a real installed package identity and the launcher's invocation
	// path, exactly as a project-scope install would leave them.
	admission, registryPath := panelAdmission(t, root, "panel-gates", "zcode")
	putJSON(t, filepath.Join(admission.Target, "gates", "quality.md"), "---\r\n简介: |\r\n  安装目录的简介\r\n  第二行\r\n---\r\nQUALITY_PROMPT_BODY")
	putJSON(t, filepath.Join(admission.Target, "gates", "security.md"), "# 安全门\n简介: 正文不是简介字段")
	putJSON(t, filepath.Join(admission.Target, "gates", "performance.md"), "---\ndescription: Installed performance description\n---\nPERFORMANCE_PROMPT_BODY")
	putJSON(t, filepath.Join(admission.Target, "formal-gates.manifest.json"), `{}`)
	receipt, err := validate.PackageReceipt(admission.Target)
	if err != nil {
		t.Fatal(err)
	}
	var launcher string
	for _, name := range []string{"registry.json", "registry.json.bootstrap.json"} {
		path := filepath.Join(filepath.Dir(registryPath), name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		for _, item := range document["records"].([]any) {
			record := item.(map[string]any)
			record["packageDigest"], record["installedDigest"] = receipt.Digest, receipt.Digest
			launcher, _ = record["launcherPath"].(string)
		}
		data, err = json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	registryHome := filepath.Join(home, ".formal-gates")
	if err := os.MkdirAll(registryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"registry.json", "registry.json.bootstrap.json"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(registryPath), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(registryHome, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	original := os.Args[0]
	os.Args[0] = launcher
	t.Cleanup(func() { os.Args[0] = original })
	gates, err := b.Gates()
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(gates, func(gate GateOption) bool {
		return gate.ID == "blackbox" || gate.ID == "whitebox" || gate.ID == "merge-gate"
	}) || !slices.Contains(gates, GateOption{ID: "quality", Description: "安装目录的简介\n第二行"}) || !slices.Contains(gates, GateOption{ID: "security"}) || !slices.Contains(gates, GateOption{ID: "performance", Description: "Installed performance description"}) {
		t.Fatalf("gates = %v", gates)
	}
	recorder := httptest.NewRecorder()
	NewHandler(b, "127.0.0.1:1234", "token").ServeHTTP(recorder, httptest.NewRequest("GET", "http://127.0.0.1:1234/api/gates", nil))
	if recorder.Code != 200 {
		t.Fatalf("gate API: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Gates []GateOption `json:"gates"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || !slices.Equal(response.Gates, gates) {
		t.Fatalf("gate API metadata differs: %+v %v", response, err)
	}
	for _, unwanted := range []string{"Content", "content", "QUALITY_PROMPT_BODY", "PERFORMANCE_PROMPT_BODY", `"description":""`} {
		if strings.Contains(recorder.Body.String(), unwanted) {
			t.Fatalf("gate API exposed content or empty metadata: %s", recorder.Body.String())
		}
	}
}
