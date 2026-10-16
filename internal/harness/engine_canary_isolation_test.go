//go:build !windows

package harness

// These tests run the canary's existing preparation/install boundary and a
// local ZCode protocol peer. They never enable the live canary or spend a
// model call. Native configuration files contain synthetic fixture values.

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"

	"formal-gates/internal/engine/testkit"
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
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
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
			if err := c.prepareHome(tc.root, stable, sourceRoot); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("prepare %q: %v", tc.root, err)
			}
			if c.binary != "" || c.binding != nil || len(c.installed.Targets) != 0 {
				t.Fatal("refused preparation reached installation or capture")
			}
		})
	}
	t.Run("named-stable-storage", func(t *testing.T) {
		storage := t.TempDir()
		writeCanaryHostFixture(t, storage, "stable-provider")
		t.Setenv("ZCODE_STORAGE_DIR", storage)
		c := &engineCanary{t: t}
		if err := c.prepareHome(storage, stable, sourceRoot); err == nil || !strings.Contains(err.Error(), "overlaps stable") {
			t.Fatalf("named stable storage root: %v", err)
		}
	})

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
			if err := c.prepareHome(candidate, stable, sourceRoot); err == nil || !strings.Contains(err.Error(), "leaves the prepared host root") {
				t.Fatalf("external native %s: %v", name, err)
			}
		})
	}
}

func TestCanaryPreparedHomeSeparatesSourceAndProfile(t *testing.T) {
	stable := t.TempDir()
	t.Setenv("USERPROFILE", stable)
	for _, key := range []string{"ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", zcodePersonalConfigEnv, zcodeCredentialHelperEnv} {
		t.Setenv(key, "")
	}
	for _, relationship := range []string{"disjoint", "source-contains-home", "home-contains-source", "same", "candidate-alias", "source-alias"} {
		t.Run(relationship, func(t *testing.T) {
			base := t.TempDir()
			source, candidate := filepath.Join(base, "source"), filepath.Join(base, "profile")
			switch relationship {
			case "source-contains-home":
				candidate = filepath.Join(source, "native-home")
			case "home-contains-source", "source-alias":
				source = filepath.Join(candidate, "source")
			case "same", "candidate-alias":
				candidate = source
			}
			if err := os.MkdirAll(source, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "source.txt"), []byte("source sentinel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			writeCanaryHostFixture(t, candidate, "profile-provider")
			actualSource, actualHome := source, candidate
			if relationship == "candidate-alias" || relationship == "source-alias" {
				alias := filepath.Join(base, "alias")
				target := candidate
				if relationship == "source-alias" {
					target = source
				}
				if err := os.Symlink(target, alias); err != nil {
					t.Fatal(err)
				}
				if relationship == "source-alias" {
					source = alias
				} else {
					candidate = alias
				}
			}
			before, err := validate.PackageReceipt(actualSource)
			if err != nil {
				t.Fatal(err)
			}
			profile := readTestFile(t, filepath.Join(actualHome, ".zcode", "v2", zcodePersonalConfig))
			c := &engineCanary{t: t}
			for repeat := 0; repeat < 2; repeat++ {
				err := c.prepareHome(candidate, stable, source)
				if relationship == "disjoint" {
					if err != nil || c.separateRoots["source"] != canaryCanonicalPath(actualSource) {
						t.Fatalf("disjoint source/profile preparation: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "overlaps source") {
					t.Fatalf("overlapping source/profile preparation: %v", err)
				}
				if c.binary != "" || c.binding != nil || len(c.installed.Targets) != 0 {
					t.Fatal("preparation reached installation or capture")
				}
			}
			after, err := validate.PackageReceipt(actualSource)
			if err != nil || before.Digest != after.Digest || !slices.Equal(before.Entries, after.Entries) {
				t.Fatalf("preparation changed actual package input: %v", err)
			}
			if relationship == "disjoint" && (len(after.Entries) != 1 || after.Entries[0].Path != "source.txt") {
				t.Fatalf("profile entered the source package: %+v", after.Entries)
			}
			if readTestFile(t, filepath.Join(actualSource, "source.txt")) != "source sentinel\n" || readTestFile(t, filepath.Join(actualHome, ".zcode", "v2", zcodePersonalConfig)) != profile {
				t.Fatal("preparation changed source or native profile content")
			}
			t.Logf("source/profile relationship=%s package=%s entries=%d binary=%q captured=%t", relationship, after.Digest, len(after.Entries), c.binary, c.binding != nil)
		})
	}
}

func TestCanaryPreparationPreservesNativeSelection(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	stable := t.TempDir()
	t.Setenv("USERPROFILE", stable)
	for _, key := range []string{"ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", zcodePersonalConfigEnv, zcodeCredentialHelperEnv} {
		t.Setenv(key, "")
	}
	preference := `{"providerFamilyDomain":"zhipu","providerFamilyConnectionSelections":{"zhipu":{"kind":"coding-plan"}}}`
	for _, tc := range []struct {
		name, setting, request, provider, selection string
		provisioned, catalogue                      bool
	}{
		{"missing-setting", "", "", "p2", "provisioning-order", true, true},
		{"no-preference", `{}`, "", "p2", "provisioning-order", true, true},
		{"host-preference", preference, "", "p1", "host-selected-plan", true, true},
		{"requested", preference, `{"providerId":"p2","modelId":"m1"}`, "p2", "requested", true, true},
		{"no-provisioning", "", "", "", "", false, false},
		{"provisioned-needs-catalogue", "", "", "", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := t.TempDir()
			if tc.provisioned {
				candidate = zcodeTestStore(t, []string{"account-provider:coding-plan:p2:account:u2:api-key", "account-provider:coding-plan:p1:account:u1:api-key"})
			}
			config := filepath.Join(candidate, ".zcode", "v2")
			catalogue := filepath.Join(config, "runtime", "provider", "zcode-builtin.json")
			if tc.catalogue {
				if err := os.MkdirAll(filepath.Dir(catalogue), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(catalogue, readTestFileBytes(t, zcodeTestCatalogue(t, "7")), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.setting != "" {
				if err := os.WriteFile(filepath.Join(config, zcodeSettingFile), []byte(tc.setting), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c := &engineCanary{t: t}
			if err := c.prepareHome(candidate, stable, sourceRoot); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ZCODE_DATA_BASE_DIR", c.home)
			wanted, err := zcodeReference(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			overlay, err := zcodeAccountProviderConfiguration(c.hostPaths()["catalogue"], wanted, zcodeTestAccountClient(t, c.home))
			if tc.provisioned && !tc.catalogue {
				if err == nil || err.Error() != "ZCODE_ACCOUNT_CONFIG: cannot read provider rules from "+c.hostPaths()["catalogue"] {
					t.Fatalf("native catalogue requirement: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if !tc.provisioned {
				if overlay != nil {
					t.Fatalf("unprovisioned native host = %#v", overlay)
				}
			} else if getOf(overlay, "current") != tc.provider || getOf(overlay, "current_source") != tc.selection || !valuesEqual(getOf(overlay, "providers"), []any{"p2", "p1"}) {
				t.Fatalf("native selection identity = %#v", overlay)
			}
			for name, absent := range map[string]bool{"personal": true, "setting": tc.setting == "", "credentials": !tc.provisioned, "catalogue": !tc.catalogue} {
				if absent {
					if _, err := os.Stat(c.hostPaths()[name]); !os.IsNotExist(err) {
						t.Fatalf("preparation created absent native %s: %v", name, err)
					}
				}
			}
			t.Logf("prepared native selection=%s provider=%v source=%v nativeError=%v", tc.name, getOf(overlay, "current"), getOf(overlay, "current_source"), err)
		})
	}
}

func TestCanaryRuntimeIdentitySurvivesPrivateHome(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	for _, provider := range []string{"git", "svn", "p4"} {
		for _, spelling := range []string{"absolute", "tilde"} {
			t.Run(provider+"/"+spelling, func(t *testing.T) {
				stable, candidate := t.TempDir(), t.TempDir()
				script := filepath.Join(stable, "runtime", "glm", "zcode.cjs")
				if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
					t.Fatal(err)
				}
				content := "// native runtime fixture\n"
				if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", stable)
				t.Setenv("USERPROFILE", stable)
				configured := script
				if spelling == "tilde" {
					configured = "~/runtime/glm/zcode.cjs"
				}
				t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", configured)
				original, err := zcodeCaptureBinding()
				if err != nil {
					t.Fatal(err)
				}
				c := &engineCanary{t: t}
				if err := c.prepareHome(candidate, stable, sourceRoot); err != nil {
					t.Fatal(err)
				}
				c.project = filepath.Join(c.home, ".formal-gates-resources", "project")
				c.jobs = filepath.Join(c.home, "jobs")
				for _, path := range []string{c.project, c.jobs} {
					if err := os.MkdirAll(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				c.vcs = vcs.NewNative(c.project, provider, nil)
				c.initFixtureRepository()
				bootstrapped, err := zcodeCaptureBinding()
				if err != nil || !valuesEqual(bootstrapped["launch"], original["launch"]) || !valuesEqual(bootstrapped["files"], original["files"]) || c.hostEnvironment()["FORMAL_GATES_ZCODE_SCRIPT"] != canaryCanonicalPath(script) {
					t.Fatalf("bootstrap changed runtime identity: binding=%#v error=%v", bootstrapped, err)
				}
				for repeat := 0; repeat < 2; repeat++ {
					if err := c.captureBinding(); err != nil {
						t.Fatal(err)
					}
					if !valuesEqual(c.binding["launch"], original["launch"]) || !valuesEqual(c.binding["files"], original["files"]) || zcodeLaunchEnvironment(c.binding)["FORMAL_GATES_ZCODE_SCRIPT"] != canaryCanonicalPath(script) || os.Getenv("HOME") != c.home {
						t.Fatalf("runtime identity across private HOME: %#v", c.binding)
					}
				}
				if readTestFile(t, script) != content {
					t.Fatal("capture changed the native runtime")
				}
			})
		}
	}
}

// The public app-server initializes the database at the canary's private path.
func TestCanaryNativeDatabaseStaysPrivate(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	binding := canaryNativePublicBinding(t)
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	for _, key := range []string{"default", "ZCODE_SESSION_DB_PATH", "ZCODE_SESSION_DB"} {
		t.Run(key, func(t *testing.T) {
			stable, candidate := t.TempDir(), t.TempDir()
			stableDatabase := filepath.Join(stable, ".zcode", "cli", "db", "db.sqlite")
			if err := os.MkdirAll(filepath.Dir(stableDatabase), 0o700); err != nil {
				t.Fatal(err)
			}
			const sentinel = "stable database sentinel\n"
			if err := os.WriteFile(stableDatabase, []byte(sentinel), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("USERPROFILE", stable)
			for _, alias := range []string{"ZCODE_SESSION_DB_PATH", "ZCODE_SESSION_DB"} {
				t.Setenv(alias, "")
			}
			if key != "default" {
				t.Setenv(key, stableDatabase)
			}
			c := &engineCanary{t: t}
			if err := c.prepareHome(candidate, stable, sourceRoot); err != nil {
				t.Fatal(err)
			}
			environment := c.hostEnvironment()
			privateDatabase := c.hostPaths()["sessionDatabase"]
			if environment["ZCODE_SESSION_DB_PATH"] != privateDatabase || environment["ZCODE_SESSION_DB"] != privateDatabase || !canaryPathWithin(c.home, privateDatabase) {
				t.Fatalf("database environment differs from private path: %#v", environment)
			}
			if key != "default" && c.separateRoots["stable "+key] != canaryCanonicalPath(stableDatabase) {
				t.Fatalf("named stable database omitted: %#v", c.separateRoots)
			}
			active := c.hostPaths()["catalogue"]
			if err := os.MkdirAll(filepath.Dir(active), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(active, []byte(canaryNativeCatalogue(t, 7, "private-active")), 0o600); err != nil {
				t.Fatal(err)
			}
			canaryNativePublicSession(t, binding, environment, c.home, t.TempDir())
			if !bytes.HasPrefix(readTestFileBytes(t, privateDatabase), []byte("SQLite format 3\x00")) || readTestFile(t, stableDatabase) != sentinel {
				t.Fatal("native database escaped private path or changed stable data")
			}
			t.Logf("native database alias=%s private=%s stable_unchanged=true", key, privateDatabase)
		})
	}
}

func TestCanaryAccountOverlayFollowsCatalogueReadiness(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	public := canaryNativePublicBinding(t)
	zcodeTestCredentialHelper(t)
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	for _, activeRevision := range []string{"missing", "5", "9"} {
		t.Run(activeRevision, func(t *testing.T) {
			candidate := zcodeTestStore(t, []string{"account-provider:coding-plan:p1:account:u1:api-key"})
			c := &engineCanary{t: t}
			if err := c.prepareHome(candidate, t.TempDir(), sourceRoot); err != nil {
				t.Fatal(err)
			}
			runtimeRoot := t.TempDir()
			script := filepath.Join(runtimeRoot, "glm", "zcode.cjs")
			bundled := filepath.Join(runtimeRoot, "config", "provider", "zcode-builtin.json")
			for _, path := range []string{script, bundled, c.hostPaths()["catalogue"]} {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			bundleContent := canaryNativeCatalogue(t, 7, "bundled")
			if err := os.WriteFile(bundled, []byte(bundleContent), 0o600); err != nil {
				t.Fatal(err)
			}
			selectedContent := bundleContent
			if activeRevision != "missing" {
				revision := 5
				if activeRevision == "9" {
					revision = 9
				}
				activeContent := canaryNativeCatalogue(t, revision, "private-active")
				if err := os.WriteFile(c.hostPaths()["catalogue"], []byte(activeContent), 0o600); err != nil {
					t.Fatal(err)
				}
				if activeRevision == "9" {
					selectedContent = activeContent
				}
			}
			if err := os.WriteFile(script, []byte("// native startup protocol fixture\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", script)
			if err := c.captureBinding(); err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(runtimeRoot, "startup-peer")
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			wrapperBody := "#!/bin/sh\ncase \"$1\" in\n*/zcode_credential.mjs) exec " + quote(textOf(getOf(public["launch"], "executable"))) + " \"$@\" ;;\n*)\n" + strings.TrimPrefix(canaryPeerWrapper("TestCanaryNativeStartupPeer", "FORMAL_GATES_CANARY_STARTUP_PEER", "1"), "#!/bin/sh\n") + ";;\nesac\n"
			if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o700); err != nil {
				t.Fatal(err)
			}
			binding, err := buildCaptureBinding("zcode", map[string]any{"executable": wrapper, "script": getOf(c.binding["launch"], "script")})
			if err != nil {
				t.Fatal(err)
			}
			binding["environment"], binding["cataloguePinned"] = c.binding["environment"], true
			c.binding = binding
			workspace := filepath.Join(c.home, ".formal-gates-resources", "issued")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			identity := "fixture-" + canaryCanonicalPath(workspace)
			for _, previous := range []string{"", identity} {
				directory := t.TempDir()
				result, err := zcodeExecute(map[string]any{"provider": canaryProvider, "binding": c.binding, "workspace": workspace, "read_only": true, "identity": previous}, directory, true)
				if err != nil {
					t.Fatal(err)
				}
				revision, err := zcodeBuiltinProviderRevision(c.hostPaths()["catalogue"])
				if err != nil {
					t.Fatal(err)
				}
				record := decodeTestJSON(t, readTestFile(t, filepath.Join(directory, "account-provider.json")))
				if !valuesEqual(record, map[string]any{"catalogue": c.hostPaths()["catalogue"], "based_on": revision, "providers": []any{"p1"}, "current": "p1", "current_source": "provisioning-order", "status": "received", "provider_count": json.Number("1"), "source": "host credential index"}) || getOf(result, "model_calls") != 0 || !valuesEqual(getOf(result, "selection"), map[string]any{"model": `{"modelId":"m1","providerId":"p1"}`, "reasoning_effort": "low"}) {
					t.Fatalf("first-attempt native account evidence: record=%#v result=%#v", record, result)
				}
				if getOf(decodeTestJSON(t, readTestFile(t, filepath.Join(directory, "identity.json"))), "identity") != identity || !valuesEqual(decodeTestJSON(t, readTestFile(t, c.hostPaths()["catalogue"])), decodeTestJSON(t, selectedContent)) || readTestFile(t, bundled) != bundleContent {
					t.Fatal("program session identity or persisted fixture release differs")
				}
				t.Logf("program startup active=%s peer_resume=%t revision=%s observation=%s", activeRevision, previous != "", revision, readTestFile(t, filepath.Join(workspace, "native-startup-observation.json")))
			}
			trace := strings.Split(strings.TrimSpace(readTestFile(t, filepath.Join(workspace, "native-startup.jsonl"))), "\n")
			wantMethods := []string{"runtime/capabilities", zcodeAccountConfigMethod, "session/create", "session/close", "runtime/capabilities", zcodeAccountConfigMethod, "session/resume", "session/close"}
			if len(trace) != len(wantMethods) {
				t.Fatalf("unexpected native startup requests: %v", trace)
			}
			for index, line := range trace {
				if getOf(decodeTestJSON(t, line), "method") != wantMethods[index] {
					t.Fatalf("native readiness/overlay/session order: %v", trace)
				}
			}
		})
	}
}

// This local peer simulates release settlement to check the launcher's fixed
// catalogue binding and startup sequence. It does not prove SDK promotion or
// recovery of an empty SDK session.
func TestCanaryNativeStartupPeer(t *testing.T) {
	if os.Getenv("FORMAL_GATES_CANARY_STARTUP_PEER") != "1" {
		return
	}
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace = canaryCanonicalPath(workspace)
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if !slices.Equal(args, []string{os.Getenv("FORMAL_GATES_ZCODE_SCRIPT"), "--cwd", workspace, "app-server"}) {
		t.Fatalf("startup argv differs: %q", args)
	}
	c := &engineCanary{t: t, home: os.Getenv("HOME")}
	environment := c.hostEnvironment()
	trace, err := os.OpenFile(filepath.Join(workspace, "native-startup.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	decoder.UseNumber()
	identity := "fixture-" + workspace
	var revision string
	step := 0
	for {
		var request map[string]any
		if err := decoder.Decode(&request); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		method, params := textOf(request["method"]), objectOrEmpty(request["params"])
		var result any
		switch step {
		case 0:
			if method != "runtime/capabilities" || len(params) != 0 {
				t.Fatal("expected native readiness request")
			}
			selected := decodeTestJSON(t, readTestFile(t, environment["ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE"]))
			bundledRevision, err := getOf(selected, "revision").(json.Number).Int64()
			if err != nil {
				t.Fatal(err)
			}
			active, err := os.ReadFile(environment[zcodeBuiltinCatalogueEnv])
			if err == nil {
				current := decodeTestJSON(t, string(active))
				currentRevision, err := getOf(current, "revision").(json.Number).Int64()
				if err != nil {
					t.Fatal(err)
				}
				if currentRevision >= bundledRevision {
					selected = current
				}
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := writeAtomicJSON(environment[zcodeBuiltinCatalogueEnv], selected); err != nil {
				t.Fatal(err)
			}
			revision, err = zcodeBuiltinProviderRevision(environment[zcodeBuiltinCatalogueEnv])
			if err != nil {
				t.Fatal(err)
			}
			if err := writeAtomicJSON(filepath.Join(workspace, "native-startup-observation.json"), map[string]any{"environment": environment, "workspace": workspace, "argv": args, "activeCatalogueBytes": readTestFile(t, environment[zcodeBuiltinCatalogueEnv]), "settlement": "local protocol fixture"}); err != nil {
				t.Fatal(err)
			}
			result = map[string]any{"independentPlanState": true}
		case 1:
			want := map[string]any{"revision": "account-config:" + revision + ":p1", "basedOnZCodeBuiltinRevision": revision, "providers": map[string]any{"p1": map[string]any{"access": map[string]any{"entitled": true, "type": "zhipu-account"}}}, "states": map[string]any{"p1": map[string]any{"availability": "available", "current": true, "entitled": true}}}
			if method != zcodeAccountConfigMethod || !valuesEqual(params, want) {
				t.Fatalf("wrong account overlay/order: %#v", request)
			}
			result = map[string]any{"status": "received", "providerCount": 1}
		case 2:
			want := map[string]any{"workspace": map[string]any{"workspacePath": workspace, "workspaceKey": workspace}}
			switch method {
			case "session/create":
				want["persistence"], want["titleGenerationEnabled"] = "immediate", false
			case "session/resume":
				want["sessionId"] = identity
			default:
				t.Fatalf("wrong session method: %s", method)
			}
			if !valuesEqual(params, want) {
				t.Fatalf("wrong session workspace/identity: %#v", request)
			}
			result = map[string]any{"protocol": map[string]any{"name": zcodeProtocolName, "version": zcodeProtocolVersion}, "session": map[string]any{"sessionId": identity}, "settings": decodeTestJSON(t, zcodeSettings)}
		case 3:
			if method != "session/close" || !valuesEqual(params, map[string]any{"sessionId": identity}) {
				t.Fatalf("wrong close identity: %#v", request)
			}
			result = map[string]any{}
		default:
			t.Fatalf("unexpected request: %#v", request)
		}
		if err := json.NewEncoder(trace).Encode(request); err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(map[string]any{"id": request["id"], "result": result}); err != nil {
			t.Fatal(err)
		}
		step++
	}
	if step != 4 {
		t.Fatalf("incomplete startup sequence: %d", step)
	}
	if err := trace.Close(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

// Binding capture names the private active catalogue without initializing it.
// The SDK checks use prepared active data; settlement belongs to the local peer.
func TestCanaryProviderCatalogueBindingKeepsActiveAndBundle(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	public := canaryNativePublicBinding(t)
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	for _, layout := range []string{"script-directory", "source-tree", "resources"} {
		for _, existing := range []bool{false, true} {
			name := layout + "/unprepared-active"
			if existing {
				name = layout + "/private-active"
			}
			t.Run(name, func(t *testing.T) {
				for _, key := range []string{"USERPROFILE", "ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", zcodePersonalConfigEnv, zcodeCredentialHelperEnv} {
					t.Setenv(key, "")
				}
				candidate := zcodeTestStore(t, []string{"account-provider:coding-plan:p2:account:u2:api-key", "account-provider:coding-plan:p1:account:u1:api-key"})
				base := t.TempDir()
				runtimeRoot := filepath.Join(base, "a", "b", "c", "app")
				script := filepath.Join(runtimeRoot, "glm", "zcode.cjs")
				bundled := filepath.Join(runtimeRoot, "glm", "provider", "zcode-builtin.json")
				if layout == "source-tree" {
					bundled = filepath.Join(base, "config", "provider", "zcode-builtin.json")
				} else if layout == "resources" {
					bundled = filepath.Join(runtimeRoot, "config", "provider", "zcode-builtin.json")
				}
				for _, path := range []string{script, bundled} {
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(script, []byte("// native runtime fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				bundleContent := canaryNativeCatalogue(t, 7, "bundled")
				if err := os.WriteFile(bundled, []byte(bundleContent), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", script)
				c := &engineCanary{t: t}
				if err := c.prepareHome(candidate, t.TempDir(), sourceRoot); err != nil {
					t.Fatal(err)
				}
				active := c.hostPaths()["catalogue"]
				var release any
				if existing {
					release = decodeTestJSON(t, canaryNativeCatalogue(t, 9, "private-active"))
					if err := os.MkdirAll(filepath.Dir(active), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(active, []byte(mustMarshalCanary(t, release)), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				var persisted []byte
				for repeat := 0; repeat < 2; repeat++ {
					if err := c.captureBinding(); err != nil {
						t.Fatal(err)
					}
					environment := zcodeLaunchEnvironment(c.binding)
					if environment[zcodeBuiltinCatalogueEnv] != active || environment["ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE"] != canaryCanonicalPath(bundled) || zcodePinnedCatalogue(c.binding) != active {
						t.Fatalf("active/bundled binding identities: %#v", environment)
					}
					if !existing {
						if _, err := os.Stat(active); !os.IsNotExist(err) {
							t.Fatalf("binding capture materialized an unprepared catalogue: %v", err)
						}
						if readTestFile(t, bundled) != bundleContent {
							t.Fatal("binding capture changed the shared bundled release")
						}
						t.Logf("provider binding layout=%s capture=%d active=%s bundled=%s active_unprepared=true bundle_bytes=%s", layout, repeat+1, active, canaryCanonicalPath(bundled), bundleContent)
						continue
					}
					canaryNativePublicSession(t, public, environment, c.home, t.TempDir())
					content := readTestFileBytes(t, active)
					if persisted != nil && !bytes.Equal(content, persisted) {
						t.Fatal("native repeated startup changed the persisted active bytes")
					}
					persisted = content
					revision, err := zcodeBuiltinProviderRevision(active)
					if err != nil || !valuesEqual(decodeTestJSON(t, string(content)), release) {
						t.Fatalf("native persistent release/revision identity: error=%v", err)
					}
					overlay, err := zcodeAccountProviderConfiguration(active, nil, zcodeTestAccountClient(t, c.home))
					if err != nil || getOf(overlay, "based_on") != revision || getOf(overlay, "current") != "p2" || getOf(overlay, "current_source") != "provisioning-order" || getOf(getOf(overlay, "configuration"), "basedOnZCodeBuiltinRevision") != revision {
						t.Fatalf("native account revision identity: %#v %v", overlay, err)
					}
					if readTestFile(t, bundled) != bundleContent {
						t.Fatal("native active read changed the shared bundled release")
					}
					t.Logf("native release layout=%s existing_active=%t capture=%d actual_catalogue=%s active_bytes=%s account_revision=%s", layout, existing, repeat+1, active, string(content), revision)
				}
			})
		}
	}
}

// The SDK release schema owns these synthetic provider and model rules.
func canaryNativeCatalogue(t *testing.T, revision int, name string) string {
	t.Helper()
	return mustMarshalCanary(t, map[string]any{"schemaVersion": 1, "revision": revision, "config": map[string]any{
		"providerConfigRules": map[string]any{"templateRules": []any{}, "providerRules": []any{
			map[string]any{"providerId": "p1", "providerName": name, "config": map[string]any{"group": "bigmodel-family", "access": map[string]any{"type": "zhipu-account", "accountType": "bigmodel", "mode": "individual-coding-plan"}}},
			map[string]any{"providerId": "p2", "providerName": name, "config": map[string]any{"group": "bigmodel-family", "access": map[string]any{"type": "zhipu-account", "accountType": "bigmodel", "mode": "highspeed"}}},
		}},
		"modelConfigRules": map[string]any{"modelRules": []any{}, "modelApiRules": []any{}, "providerSiteRules": []any{}, "templateModelRules": []any{}, "builtinProviderModelRules": []any{}},
	}})
}

func canaryNativePublicBinding(t *testing.T) map[string]any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	if _, err := os.Stat(zcodeDefaultScript); os.IsNotExist(err) {
		t.Skip("native ZCode app-server is not available")
	} else if err != nil {
		t.Fatal(err)
	}
	binding, err := buildCaptureBinding("zcode", map[string]any{"executable": resolveNativePath(node), "script": zcodeDefaultScript})
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

// Only public zero-model RPCs establish storage and provider bootstrap facts.
func canaryNativePublicSession(t *testing.T, binding map[string]any, environment map[string]string, workspace, directory string) {
	t.Helper()
	client, err := newZcodeClient(objectOrEmpty(binding["launch"]), workspace, directory, environment)
	if err != nil {
		t.Fatal(err)
	}
	defer client.client.close()
	calls := []any{}
	request := func(method string, params map[string]any) any {
		result, err := client.request(method, params)
		if err != nil {
			t.Fatalf("%v\nargv=%q\nstderr=%s\nprotocol=%s", err, client.client.cmd.Args, readTestFile(t, filepath.Join(directory, "host.stderr")), readTestFile(t, filepath.Join(directory, "protocol.jsonl")))
		}
		calls = append(calls, map[string]any{"method": method, "params": params, "result": result})
		return result
	}
	capabilities := request("runtime/capabilities", map[string]any{})
	if getOf(capabilities, "independentPlanState") != true {
		t.Fatalf("native capabilities differ: %#v", capabilities)
	}
	snapshot := request("session/create", map[string]any{"workspace": map[string]any{"workspacePath": workspace, "workspaceKey": workspace}, "persistence": "immediate", "titleGenerationEnabled": false})
	if _, err := zcodeHandshake(snapshot); err != nil {
		t.Fatal(err)
	}
	identity := textOf(getOf(getOf(snapshot, "session"), "sessionId"))
	if identity == "" || !valuesEqual(getOf(getOf(snapshot, "session"), "workspace"), map[string]any{"workspacePath": workspace, "workspaceKey": workspace}) {
		t.Fatalf("native session identity/workspace differs: %#v", snapshot)
	}
	if getOf(request("session/close", map[string]any{"sessionId": identity}), "closed") != true {
		t.Fatal("native session was not closed")
	}
	if err := client.client.close(); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeBinding(binding, "zcode"); err != nil {
		t.Fatal(err)
	}
	observation := map[string]any{"argv": client.client.cmd.Args, "workspace": workspace, "environment": environment, "calls": calls, "files": binding["files"], "exitCode": client.client.cmd.ProcessState.ExitCode(), "modelCalls": 0, "protocol": readTestFile(t, filepath.Join(directory, "protocol.jsonl")), "stderr": readTestFile(t, filepath.Join(directory, "host.stderr"))}
	if err := writeAtomicJSON(filepath.Join(directory, "public-native-observation.json"), observation); err != nil {
		t.Fatal(err)
	}
	t.Logf("public_native_observation=%s", mustMarshalCanary(t, observation))
}

func TestCanaryInstalledHostUsesPrivateRoot(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	stable, candidate := t.TempDir(), t.TempDir()
	writeCanaryHostFixture(t, stable, "stable-provider")
	writeCanaryHostFixture(t, candidate, "candidate-provider")
	stableFiles := map[string]string{
		".zcode/cli/config.json":           `{"stableHook":"untouched"}`,
		".zcode/cli/db/db.sqlite":          "stable database sentinel\n",
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
	t.Setenv("ZCODE_SESSION_DB_PATH", filepath.Join(stable, ".zcode", "cli", "db", "db.sqlite"))
	t.Setenv("ZCODE_SESSION_DB", os.Getenv("ZCODE_SESSION_DB_PATH"))
	t.Setenv("ZCODE_LOG_DIR", filepath.Join(stable, ".zcode", "log"))
	t.Setenv("ZCODE_MAILBOX_ROOT", filepath.Join(stable, ".zcode", "mailbox"))
	t.Setenv("ZCODE_HOME", filepath.Join(stable, ".zcode"))
	t.Setenv(zcodePersonalConfigEnv, filepath.Join(stable, ".zcode", "v2", zcodePersonalConfig))
	t.Setenv(zcodeBuiltinCatalogueEnv, filepath.Join(stable, ".zcode", "v2", "runtime", "provider", "zcode-builtin.json"))
	t.Setenv("ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", os.Getenv(zcodeBuiltinCatalogueEnv))
	t.Setenv(zcodeCredentialHelperEnv, filepath.Join(stable, ".zcode", "v2", zcodeCredentialsFile))
	runtimeRoot := t.TempDir()
	peer := filepath.Join(runtimeRoot, "glm", "zcode.cjs")
	bundled := filepath.Join(runtimeRoot, "config", "provider", "zcode-builtin.json")
	bundledContent := readTestFileBytes(t, zcodeTestCatalogue(t, "7"))
	for _, path := range []string{peer, bundled} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(bundled, bundledContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peer, []byte(canaryNativePathPeer), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", peer)
	c := &engineCanary{t: t}
	if err := c.prepareHome(os.Getenv("ZCODE_DATA_BASE_DIR"), stable, sourceRoot); err != nil {
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
	c.doc = []byte(canaryRequirementDocument)
	intake := c.confirmedIntake()
	c.install(base, intake)
	target := c.installed.Targets[0]
	if c.home != canaryCanonicalPath(candidate) || zcodeHostDataDirectory() != c.home || os.Getenv("HOME") != c.home || os.Getenv("USERPROFILE") != c.home {
		t.Fatal("installation and native host did not share the prepared root")
	}
	for _, path := range []string{target.TargetPath, target.LauncherPath, target.HookConfig, target.ManagedRulePath, c.installed.Registry, os.Getenv(zcodeCredentialHelperEnv)} {
		if !canaryPathWithin(c.home, path) {
			t.Fatalf("installed or native path leaves the candidate: %s", path)
		}
	}
	sourceReceipt, err := validate.PackageReceipt(target.SourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	installedReceipt, err := validate.PackageReceipt(target.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.installed.PackageDigest != sourceReceipt.Digest || target.SourceDigest != sourceReceipt.Digest || target.InstalledDigest != installedReceipt.Digest || target.HookAction != "CONFIGURED" || target.ManagedRuleAction != "APPLIED" {
		t.Fatalf("candidate receipt binding: source=%s/%s installed=%s/%s hook=%s rule=%s", target.SourceDigest, sourceReceipt.Digest, target.InstalledDigest, installedReceipt.Digest, target.HookAction, target.ManagedRuleAction)
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
	helper := filepath.Join(target.TargetPath, "internal", "harness", zcodeCredentialHelper)
	if !zcodeIsRegularFile(helper) {
		t.Fatalf("installed helper = %s, %v", helper, err)
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
		identityRecord, err := readJSONFile(filepath.Join(c.jobs, name, "identity.json"))
		if err != nil {
			t.Fatal(err)
		}
		identity := textOf(getOf(identityRecord, "identity"))
		if identity != "fixture-"+canaryCanonicalPath(workspace) || identities[identity] || getOf(identityRecord, "workspace") != canaryCanonicalPath(workspace) || getOf(source, "model_calls") != 0 {
			t.Fatalf("native input identity=%q record=%#v source=%#v", identity, identityRecord, source)
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
		if getOf(observation["nativeRuntime"], "bundledCatalogue") != canaryCanonicalPath(bundled) || getOf(observation["nativeRuntime"], "bundledRevision") != json.Number("7") || observation["activeRevision"] != json.Number("1") {
			t.Fatalf("native bundled/active identity: %#v", observation)
		}
		for key, got := range objectOrEmpty(observation["paths"]) {
			if !canaryPathWithin(c.home, textOf(got)) {
				t.Fatalf("native observed %s outside candidate: %s", key, got)
			}
		}
		t.Logf("local native observation: %s", readTestFile(t, filepath.Join(workspace, "native-observation.json")))
	}
	proof := objectOrEmpty(decodeTestJSON(t, readTestFile(t, filepath.Join(c.jobs, "host-isolation.json"))))
	if getOf(proof["separateRoots"], "source") != canaryCanonicalPath(sourceRoot) || !valuesEqual(proof["installReport"], decodeTestJSON(t, mustMarshalCanary(t, c.installed))) || getOf(proof["candidate"], "sessionDatabase") != c.hostPaths()["sessionDatabase"] || getOf(proof["observed"], "sessionDatabase") != false || getOf(proof["observed"], "projectState") != true || getOf(proof["observed"], "log") != false {
		t.Fatalf("proof lost actual installation or observed/prospective distinction: %#v", proof)
	}
	if getOf(proof["nativeRuntime"], "bundledCatalogue") != canaryCanonicalPath(bundled) || getOf(proof["nativeRuntime"], "bundledCatalogueObserved") != true || canaryPathWithin(c.home, bundled) || string(readTestFileBytes(t, bundled)) != string(bundledContent) {
		t.Fatalf("read-only native bundle ownership: %#v", proof["nativeRuntime"])
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
var canaryNativePathPeer = `
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
const keys = ['HOME','USERPROFILE','ZCODE_DATA_BASE_DIR','ZCODE_HOME','ZCODE_STORAGE_DIR','ZCODE_SESSION_DB_PATH','ZCODE_SESSION_DB','ZCODE_LOG_DIR','ZCODE_MAILBOX_ROOT','ZCODE_BUILTIN_PROVIDER_CONFIG_FILE','ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE','ZCODE_PERSONAL_PROVIDER_CONFIG_FILE','FORMAL_GATES_ZCODE_SCRIPT','FORMAL_GATES_ZCODE_CREDENTIALS','` + strings.Join(lifecycle.ProviderEnvironmentKeys(), "','") + `'];
const environment = Object.fromEntries(keys.map(key => [key, process.env[key] ?? '']));
const config = path.join(process.env.ZCODE_DATA_BASE_DIR, '.zcode', 'v2');
const paths = Object.fromEntries(['ZCODE_BUILTIN_PROVIDER_CONFIG_FILE','ZCODE_PERSONAL_PROVIDER_CONFIG_FILE','FORMAL_GATES_ZCODE_CREDENTIALS'].map(key => [key, fs.realpathSync(process.env[key])]));
paths.sessionDatabase = process.env.ZCODE_SESSION_DB_PATH;
if (paths.sessionDatabase !== process.env.ZCODE_SESSION_DB) process.exit(6);
const settings = JSON.parse(fs.readFileSync(path.join(config, 'setting.json')));
const provider = JSON.parse(fs.readFileSync(process.env.ZCODE_PERSONAL_PROVIDER_CONFIG_FILE)).fixtureProvider;
const nativeRuntime = {bundledCatalogue:fs.realpathSync(process.env.ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE),bundledRevision:JSON.parse(fs.readFileSync(process.env.ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE)).revision};
const activeRevision = JSON.parse(fs.readFileSync(process.env.ZCODE_BUILTIN_PROVIDER_CONFIG_FILE)).revision;
readline.createInterface({input: process.stdin}).on('line', line => {
  const request = JSON.parse(line);
  let result;
  if (request.method === 'runtime/capabilities') {
    if (Object.keys(request.params).length) process.exit(7);
    result = {independentPlanState:true};
  } else if (request.method === 'session/create') {
    const workspace = fs.realpathSync(request.params.workspace.workspacePath);
    const argvWorkspace = fs.realpathSync(process.argv[process.argv.indexOf('--cwd')+1]);
    if (request.params.persistence !== 'immediate' || request.params.titleGenerationEnabled !== false || workspace !== fs.realpathSync(process.cwd()) || workspace !== argvWorkspace) process.exit(4);
    fs.writeFileSync(path.join(workspace,'native-observation.json'), JSON.stringify({environment,paths,workspace,argvWorkspace,provider,nativeRuntime,activeRevision}));
    result = {protocol:{name:'ZCode Protocol',version:1},session:{sessionId:'fixture-'+workspace},settings};
  } else if (request.method === 'session/close') {
    result = {};
  } else {
    process.exit(5);
  }
  process.stdout.write(JSON.stringify({id:request.id,result})+'\n');
});
`

func TestCanaryNativeBootstrapPreservesConfiguredCachePaths(t *testing.T) {
	for _, provider := range []string{"git", "svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			base := t.TempDir()
			moduleCache := filepath.Join(base, "module cache")
			buildCache := filepath.Join(base, "build cache")
			t.Setenv("GOMODCACHE", moduleCache)
			t.Setenv("GOCACHE", buildCache)
			c := &engineCanary{t: t, home: filepath.Join(base, "home"), project: filepath.Join(base, "project"), jobs: filepath.Join(base, "jobs")}
			for _, path := range []string{c.home, c.project, c.jobs} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			c.vcs = vcs.NewNative(c.project, provider, nil)
			c.initFixtureRepository()
			if gotModule, gotBuild := os.Getenv("GOMODCACHE"), os.Getenv("GOCACHE"); gotModule != moduleCache || gotBuild != buildCache {
				t.Fatalf("configured cache values changed: module=%q want=%q build=%q want=%q", gotModule, moduleCache, gotBuild, buildCache)
			}
			observed := c.fixtureCommand(c.project, "go", nil, "env", "GOMODCACHE", "GOCACHE")
			if observed != moduleCache+"\n"+buildCache {
				t.Fatalf("Go did not consume the configured cache values: %q", observed)
			}
		})
	}
}

// The installed CLI must keep the immutable source build identity while native
// fixture builds consume their own stamping flag and private host environment.
func TestCanaryCandidateBuildPreservesSourceInputs(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(t.TempDir(), "source")
	clone := exec.Command("git", "clone", "--local", "--no-hardlinks", filepath.Dir(filepath.Dir(filepath.Dir(file))), source)
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone immutable source: %v %s", err, output)
	}
	packageRoot := t.TempDir()
	receipt, err := validate.PackageReceipt(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range receipt.Entries {
		rel := filepath.FromSlash(entry.Path)
		data, err := os.ReadFile(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(packageRoot, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, os.FileMode(entry.Mode)); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(packageRoot, "bin", "formal-gates")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	baseline := &engineCanary{t: t, binary: binary}
	baseline.buildInstalledCandidate(source)
	wantBytes := readTestFileBytes(t, binary)
	wantInfo, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	stamped := false
	for _, setting := range wantInfo.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			stamped = true
		}
	}
	if !stamped {
		t.Fatal("baseline source build has no VCS identity")
	}
	wantPackage, err := validate.PackageReceipt(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	originalFlags := os.Getenv("GOFLAGS")
	for _, provider := range []string{"git", "svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			base := t.TempDir()
			c := &engineCanary{t: t, binary: binary, home: filepath.Join(base, "home"), project: filepath.Join(base, "project"), jobs: filepath.Join(base, "jobs")}
			for _, path := range []string{c.home, c.project, c.jobs} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			c.vcs = vcs.NewNative(c.project, provider, nil)
			c.initFixtureRepository()
			wantFlags := originalFlags
			if provider != "git" {
				wantFlags = strings.TrimSpace(originalFlags + " -buildvcs=false")
			}
			if got := c.fixtureCommand(c.project, "go", nil, "env", "GOFLAGS"); got != wantFlags {
				t.Fatalf("fixture GOFLAGS=%q, want %q", got, wantFlags)
			}
			for attempt := 0; attempt < 2; attempt++ {
				c.buildInstalledCandidate(source)
				gotBytes := readTestFileBytes(t, binary)
				gotInfo, err := buildinfo.ReadFile(binary)
				if err != nil || !bytes.Equal(gotBytes, wantBytes) || gotInfo.String() != wantInfo.String() {
					t.Fatalf("candidate bytes or build info differ from source build: %v\nwant SHA256=%x info=%s\ngot SHA256=%x info=%s", err, sha256.Sum256(wantBytes), wantInfo.String(), sha256.Sum256(gotBytes), gotInfo.String())
				}
				gotPackage, err := validate.PackageReceipt(packageRoot)
				if err != nil || gotPackage.Digest != wantPackage.Digest || !canaryValuesEqual(gotPackage.Entries, wantPackage.Entries) {
					t.Fatalf("candidate package manifest differs from source build: %v", err)
				}
			}
			t.Logf("candidate SHA256=%x package=%s entries=%d fixture GOFLAGS=%q buildinfo=%s", sha256.Sum256(wantBytes), wantPackage.Digest, len(wantPackage.Entries), wantFlags, wantInfo.String())
		})
	}
}
