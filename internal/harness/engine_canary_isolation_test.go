//go:build !windows

package harness

// These tests run the canary's existing preparation/install boundary and a
// local ZCode protocol peer. They never enable the live canary or spend a
// model call. Native configuration files contain synthetic fixture values.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/validate"
)

func writeCanaryHostFixture(t *testing.T, root, provider string) {
	t.Helper()
	files := map[string]string{
		"provider_config.json":                `{"fixtureProvider":"` + provider + `"}`,
		"setting.json":                        strings.ReplaceAll(zcodeSettings, "p1", provider),
		"credentials.json":                    `{}`,
		"runtime/provider/zcode-builtin.json": `{"revision":1,"config":{"providerConfigRules":{"providerRules":[]}}}`,
	}
	for name, content := range files {
		path := filepath.Join(root, ".zcode", "v2", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanaryPreparedHomeRejectsStableAndMissingRoots(t *testing.T) {
	stable := t.TempDir()
	writeCanaryHostFixture(t, stable, "stable-provider")
	t.Setenv("USERPROFILE", stable)
	for _, key := range []string{"ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", zcodePersonalConfigEnv, zcodeCredentialHelperEnv} {
		t.Setenv(key, "")
	}
	alias := filepath.Join(t.TempDir(), "stable-alias")
	if err := os.Symlink(stable, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, root, message string }{
		{"unset", "", "prepared ZCODE_DATA_BASE_DIR"},
		{"missing", filepath.Join(t.TempDir(), "missing"), "existing directory"},
		{"stable-home", stable, "overlaps stable"},
		{"stable-config", filepath.Join(stable, ".zcode", "v2"), "overlaps stable"},
		{"stable-alias", alias, "overlaps stable"},
		{"stable-parent", filepath.Dir(stable), "overlaps stable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &engineCanary{t: t}
			if err := c.prepareHome(tc.root, stable); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("prepare %q: %v", tc.root, err)
			}
			if c.binary != "" || c.binding != nil || len(c.installed.Targets) != 0 {
				t.Fatal("refused preparation reached installation or capture")
			}
		})
	}
	for _, name := range []string{zcodePersonalConfig, zcodeSettingFile, zcodeCredentialsFile, "runtime/provider/zcode-builtin.json"} {
		t.Run("config-alias-"+filepath.Base(name), func(t *testing.T) {
			candidate := t.TempDir()
			writeCanaryHostFixture(t, candidate, "candidate-provider")
			path := filepath.Join(candidate, ".zcode", "v2", name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(stable, ".zcode", "v2", name), path); err != nil {
				t.Fatal(err)
			}
			c := &engineCanary{t: t}
			if err := c.prepareHome(candidate, stable); err == nil || !strings.Contains(err.Error(), "leaves the prepared host root") {
				t.Fatalf("external native %s: %v", name, err)
			}
		})
	}
}

func TestCanaryInstalledHostUsesPrivateRoot(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	stable, candidate := t.TempDir(), t.TempDir()
	writeCanaryHostFixture(t, stable, "stable-provider")
	writeCanaryHostFixture(t, candidate, "candidate-provider")
	stableFiles := map[string]string{
		".zcode/cli/config.json":           `{"stableHook":"untouched"}`,
		".zcode/AGENTS.md":                 "stable rule sentinel\n",
		".local/bin/formal-gates":          "stable launcher sentinel\n",
		".formal-gates/registry.json":      "stable registry sentinel\n",
		".gates/sentinel":                  "stable state sentinel\n",
		".formal-gates-resources/sentinel": "stable resource sentinel\n",
	}
	for name, data := range stableFiles {
		path := filepath.Join(stable, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"provider_config.json", "setting.json", "credentials.json", "runtime/provider/zcode-builtin.json"} {
		path := filepath.Join(".zcode", "v2", name)
		stableFiles[path] = readTestFile(t, filepath.Join(stable, path))
	}
	t.Setenv("USERPROFILE", stable)
	t.Setenv("ZCODE_DATA_BASE_DIR", candidate)
	t.Setenv("ZCODE_STORAGE_DIR", filepath.Join(stable, ".zcode"))
	t.Setenv("ZCODE_LOG_DIR", filepath.Join(stable, ".zcode", "log"))
	t.Setenv("ZCODE_MAILBOX_ROOT", filepath.Join(stable, ".zcode", "mailbox"))
	t.Setenv("ZCODE_HOME", filepath.Join(stable, ".zcode"))
	t.Setenv(zcodePersonalConfigEnv, filepath.Join(stable, ".zcode", "v2", zcodePersonalConfig))
	t.Setenv(zcodeBuiltinCatalogueEnv, filepath.Join(stable, ".zcode", "v2", "runtime", "provider", "zcode-builtin.json"))
	t.Setenv("ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", os.Getenv(zcodeBuiltinCatalogueEnv))
	t.Setenv(zcodeCredentialHelperEnv, filepath.Join(stable, ".zcode", "v2", zcodeCredentialsFile))
	peer := filepath.Join(t.TempDir(), "zcode.cjs")
	if err := os.WriteFile(peer, []byte(canaryNativePathPeer), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", peer)
	c := &engineCanary{t: t}
	if err := c.prepareHome(os.Getenv("ZCODE_DATA_BASE_DIR"), stable); err != nil {
		t.Fatal(err)
	}
	c.project = filepath.Join(c.home, ".formal-gates-resources", "project")
	c.jobs = filepath.Join(c.home, "canary-evidence", "jobs")
	for _, path := range []string{c.project, c.jobs} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	base := c.buildFixtureProject()
	reqSum := sha256.Sum256([]byte(canaryRequirementDocument))
	frobSum := sha256.Sum256([]byte(canaryFrobSource))
	reqRevision, frobRevision := hex.EncodeToString(reqSum[:]), hex.EncodeToString(frobSum[:])
	intake := facade.IntakeConfirmationReceipt{
		Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: reqRevision,
		Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: reqRevision}, {Path: "frob.go", Revision: frobRevision}},
		SolutionRevision: frobRevision, SolutionDigest: "sha256:" + frobRevision,
	}
	_, file, _, _ := runtime.Caller(0)
	c.install(filepath.Dir(filepath.Dir(filepath.Dir(file))), base, intake)
	target := c.installed.Targets[0]
	if c.home != canaryCanonicalPath(candidate) || zcodeHostDataDirectory() != c.home || os.Getenv("HOME") != c.home || os.Getenv("USERPROFILE") != c.home {
		t.Fatal("installation and native host did not share the prepared root")
	}
	for _, path := range []string{target.TargetPath, target.LauncherPath, target.HookConfig, target.ManagedRulePath, c.installed.Registry, zcodeCredentialStore()} {
		if !canaryPathWithin(c.home, path) {
			t.Fatalf("installed or native path leaves the candidate: %s", path)
		}
	}
	if c.installed.PackageDigest == "" || target.InstalledDigest != c.installed.PackageDigest || target.HookAction != "CONFIGURED" || target.ManagedRuleAction != "APPLIED" {
		t.Fatalf("candidate install identity/hooks/rule: %#v", target)
	}
	hook := readTestFile(t, target.HookConfig)
	if !strings.Contains(hook, target.LauncherPath) || strings.Contains(hook, filepath.Join(stable, ".local", "bin", "formal-gates")) {
		t.Fatal("native hook does not call the installed candidate launcher")
	}
	rule, err := validate.LoadManagedRule(target.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if actual := readTestFile(t, target.ManagedRulePath); actual != "<formal-gates:host-instructions:start>\n"+rule+"\n<formal-gates:host-instructions:end>\n" {
		t.Fatal("managed rule differs from the installed package's canonical block")
	}
	helper, err := zcodeCredentialHelperPath()
	if err != nil || helper != filepath.Join(target.TargetPath, "runtime", "langgraph", "formal_gates_harness", zcodeCredentialHelper) || !zcodeIsRegularFile(helper) {
		t.Fatalf("installed helper = %s, %v", helper, err)
	}
	if os.Getenv("FORMAL_GATES_HARNESS_ROOT") != target.TargetPath {
		t.Fatal("helper root did not come from the actual install target")
	}

	identities := map[string]bool{}
	for _, name := range []string{"project", "issued-workspace"} {
		workspace := filepath.Join(c.home, ".formal-gates-resources", name)
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		request := map[string]any{"provider": canaryProvider, "binding": c.binding, "workspace": workspace, "read_only": true, "identity": ""}
		source, err := zcodeExecute(request, filepath.Join(c.jobs, name), true)
		if err != nil {
			t.Fatal(err)
		}
		identity := textOf(source["identity"])
		if identity != "fixture-"+canaryCanonicalPath(workspace) || identities[identity] {
			t.Fatalf("native input identity=%q source=%#v", identity, source)
		}
		identities[identity] = true
		observation := objectOrEmpty(decodeTestJSON(t, readTestFile(t, filepath.Join(workspace, "native-observation.json"))))
		if observation["workspace"] != canaryCanonicalPath(workspace) || observation["argvWorkspace"] != canaryCanonicalPath(workspace) || observation["provider"] != "candidate-provider" {
			t.Fatalf("native peer resolved the wrong input/config: %#v", observation)
		}
		for key, want := range c.hostEnvironment() {
			if key == "PATH" {
				continue
			}
			if got := objectOrEmpty(observation["environment"])[key]; got != want {
				t.Fatalf("native %s=%v, want %s", key, got, want)
			}
		}
		for key, got := range objectOrEmpty(observation["paths"]) {
			if !canaryPathWithin(c.home, textOf(got)) {
				t.Fatalf("native observed %s outside candidate: %s", key, got)
			}
		}
		t.Logf("local native observation: %s", readTestFile(t, filepath.Join(workspace, "native-observation.json")))
	}
	proof := objectOrEmpty(decodeTestJSON(t, readTestFile(t, filepath.Join(c.jobs, "host-isolation.json"))))
	if !valuesEqual(proof["installReport"], decodeTestJSON(t, mustMarshalCanary(t, c.installed))) || getOf(proof["observed"], "projectState") != true || getOf(proof["observed"], "log") != false {
		t.Fatalf("proof lost actual installation or observed/prospective distinction: %#v", proof)
	}
	t.Logf("local canonical isolation proof: %s", readTestFile(t, filepath.Join(c.jobs, "host-isolation.json")))
	for name, want := range stableFiles {
		if got := readTestFile(t, filepath.Join(stable, name)); got != want {
			t.Fatalf("stable fixture changed: %s", name)
		}
	}
}

func mustMarshalCanary(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The peer observes real child environment, cwd, argv and configuration files.
// Its session identity depends on the request workspace; it refuses unexpected
// methods and never sends a model request or a credential request.
const canaryNativePathPeer = `
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const keys = ['HOME','USERPROFILE','ZCODE_DATA_BASE_DIR','ZCODE_HOME','ZCODE_STORAGE_DIR','ZCODE_LOG_DIR','ZCODE_MAILBOX_ROOT','ZCODE_BUILTIN_PROVIDER_CONFIG_FILE','ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE','ZCODE_PERSONAL_PROVIDER_CONFIG_FILE','FORMAL_GATES_ZCODE_CREDENTIALS','FORMAL_GATES_HARNESS_ROOT','AI_AGENT','CODEX_HOME','DSH_HOME','DSH_PROJECT_DIR'];
const environment = Object.fromEntries(keys.map(key => [key, process.env[key] ?? '']));
const config = path.join(process.env.ZCODE_DATA_BASE_DIR, '.zcode', 'v2');
const paths = Object.fromEntries(['ZCODE_BUILTIN_PROVIDER_CONFIG_FILE','ZCODE_PERSONAL_PROVIDER_CONFIG_FILE','FORMAL_GATES_ZCODE_CREDENTIALS','FORMAL_GATES_HARNESS_ROOT'].map(key => [key, fs.realpathSync(process.env[key])]));
const settings = JSON.parse(fs.readFileSync(path.join(config, 'setting.json')));
const provider = JSON.parse(fs.readFileSync(process.env.ZCODE_PERSONAL_PROVIDER_CONFIG_FILE)).fixtureProvider;
readline.createInterface({input: process.stdin}).on('line', line => {
  const request = JSON.parse(line);
  let result;
  if (request.method === 'session/create') {
    const workspace = fs.realpathSync(request.params.workspace.workspacePath);
    const argvWorkspace = fs.realpathSync(process.argv[process.argv.indexOf('--cwd')+1]);
    if (request.params.persistence !== 'immediate' || request.params.titleGenerationEnabled !== false || workspace !== fs.realpathSync(process.cwd()) || workspace !== argvWorkspace) process.exit(4);
    fs.writeFileSync(path.join(workspace,'native-observation.json'), JSON.stringify({environment,paths,workspace,argvWorkspace,provider}));
    result = {protocol:{name:'ZCode Protocol',version:1},session:{sessionId:'fixture-'+workspace},settings};
  } else if (request.method === 'session/close') {
    result = {};
  } else {
    process.exit(5);
  }
  process.stdout.write(JSON.stringify({id:request.id,result})+'\n');
});
`
