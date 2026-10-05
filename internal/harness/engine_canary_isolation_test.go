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
	"slices"
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
			overlay, err := zcodeAccountProviderConfiguration(c.hostPaths()["catalogue"], wanted)
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

func TestCanaryProviderReleaseKeepsPrivateActiveAndNativeBundle(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	consumer := canaryNativeReleaseConsumer(t)
	_, file, _, _ := runtime.Caller(0)
	sourceRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	for _, layout := range []string{"script-directory", "source-tree", "resources"} {
		for _, existing := range []bool{false, true} {
			name := layout + "/missing-active"
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
				release := objectOrEmpty(decodeTestJSON(t, readTestFile(t, zcodeTestCatalogue(t, "7"))))
				config := objectOrEmpty(release["config"])
				config["providers"] = map[string]any{"fixture": "bundled"}
				config["providerTemplates"], config["modelConfigRules"] = map[string]any{}, []any{}
				bundleContent := mustMarshalCanary(t, release)
				if err := os.WriteFile(bundled, []byte(bundleContent), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", script)
				c := &engineCanary{t: t}
				if err := c.prepareHome(candidate, t.TempDir(), sourceRoot); err != nil {
					t.Fatal(err)
				}
				if existing {
					release["revision"] = 9
					config["providers"] = map[string]any{"fixture": "private-active"}
					active := c.hostPaths()["catalogue"]
					if err := os.MkdirAll(filepath.Dir(active), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(active, []byte(mustMarshalCanary(t, release)), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for repeat := 0; repeat < 2; repeat++ {
					if err := c.captureBinding(); err != nil {
						t.Fatal(err)
					}
					environment := zcodeLaunchEnvironment(c.binding)
					if environment[zcodeBuiltinCatalogueEnv] != c.hostPaths()["catalogue"] || environment["ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE"] != canaryCanonicalPath(bundled) || zcodePinnedCatalogue(c.binding) != environment[zcodeBuiltinCatalogueEnv] {
						t.Fatalf("active/bundled binding identities: %#v", environment)
					}
					cmd := exec.Command("node", consumer)
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("native read: %v %s", err, output)
					}
					native := objectOrEmpty(decodeTestJSON(t, string(output)))
					revision, err := zcodeBuiltinProviderRevision(environment[zcodeBuiltinCatalogueEnv])
					if err != nil || native["revision"] != revision || native["activeFilePath"] != environment[zcodeBuiltinCatalogueEnv] || native["bundledFilePath"] != canaryCanonicalPath(bundled) || !valuesEqual(native["providers"], config["providers"]) || !valuesEqual(decodeTestJSON(t, readTestFile(t, c.hostPaths()["catalogue"])), release) {
						t.Fatalf("native persistent release/revision identity: native=%#v error=%v", native, err)
					}
					overlay, err := zcodeAccountProviderConfiguration(zcodePinnedCatalogue(c.binding), nil)
					if err != nil || getOf(overlay, "based_on") != native["revision"] || getOf(overlay, "current") != "p2" || getOf(overlay, "current_source") != "provisioning-order" || getOf(getOf(overlay, "configuration"), "basedOnZCodeBuiltinRevision") != native["revision"] {
						t.Fatalf("native account revision identity: %#v %v", overlay, err)
					}
					if readTestFile(t, bundled) != bundleContent {
						t.Fatal("native active read changed the shared bundled release")
					}
					if layout != "resources" {
						lookup, err := exec.Command("node", consumer, "lookup", script).CombinedOutput()
						if err != nil || strings.TrimSpace(string(lookup)) != canaryCanonicalPath(bundled) {
							t.Fatalf("original native lookup: %v %s", err, lookup)
						}
					}
					t.Logf("native release layout=%s existing_active=%t capture=%d observation=%s account_revision=%s", layout, existing, repeat+1, strings.TrimSpace(string(output)), textOf(getOf(overlay, "based_on")))
				}
			})
		}
	}
}

// The consumer runs the native file read, release selection, active write and
// revision path hash. Schema parsing and locking are local fixture substitutes;
// it starts no app-server, credential helper or model request.
func canaryNativeReleaseConsumer(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(zcodeDefaultScript)
	if os.IsNotExist(err) {
		t.Skip("native ZCode release consumer is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	_, functions, found := strings.Cut(source, "async function MWt(")
	functions, ended := strings.Cut(functions, "var Snr,")
	_, class, classFound := strings.Cut(source, "UO=class")
	class, classEnded := strings.Cut(class, ";r(MWt,")
	_, lookup, lookupFound := strings.Cut(source, "async function fQi(")
	lookup, lookupEnded := strings.Cut(lookup, "function mQi(")
	if !found || !ended || !classFound || !classEnded || !lookupFound || !lookupEnded {
		t.Fatal("native release consumer source markers are missing")
	}
	prelude := `const Snr=require('node:fs'),wnr=require('node:crypto'),rZe=require('node:fs/promises'),bee=require('node:path'),Zj=bee,bmr=Snr;
const r=()=>{},Spe=x=>x,bV=x=>JSON.stringify(x),OWt=x=>x,lw=async(p,fn)=>fn(),qL=(p,data)=>rZe.writeFile(p,data);
`
	driver := `
async function consume() {
  if (process.argv[2] === 'lookup') { console.log(await fQi({entrypoint:process.argv[3]})); return; }
  const bundledFilePath=process.env.ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE;
  const activeFilePath=process.env.ZCODE_BUILTIN_PROVIDER_CONFIG_FILE;
  const consumer=new UO({bundledFilePath,activeFilePath,watch:false});
  try { console.log(JSON.stringify({...await consumer.read(),activeFilePath:consumer.activeFilePath,bundledFilePath})); }
  finally { consumer.dispose(); }
}
consume().catch(error=>{console.error(error);process.exitCode=1});
`
	path := filepath.Join(t.TempDir(), "native-release-consumer.cjs")
	if err := os.WriteFile(path, []byte(prelude+"async function MWt("+functions+"const UO=class"+class+";async function fQi("+lookup+driver), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("native release consumer source=%s sha256=%x", zcodeDefaultScript, sha256.Sum256(data))
	return path
}

func TestCanaryInstalledHostUsesPrivateRoot(t *testing.T) {
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
	reqSum := sha256.Sum256([]byte(canaryRequirementDocument))
	frobSum := sha256.Sum256([]byte(canaryFrobSource))
	reqRevision, frobRevision := hex.EncodeToString(reqSum[:]), hex.EncodeToString(frobSum[:])
	intake := facade.IntakeConfirmationReceipt{
		Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: reqRevision,
		Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: reqRevision}, {Path: "frob.go", Revision: frobRevision}},
		SolutionRevision: frobRevision, SolutionDigest: "sha256:" + frobRevision,
	}
	c.install(base, intake)
	target := c.installed.Targets[0]
	if c.home != canaryCanonicalPath(candidate) || zcodeHostDataDirectory() != c.home || os.Getenv("HOME") != c.home || os.Getenv("USERPROFILE") != c.home {
		t.Fatal("installation and native host did not share the prepared root")
	}
	for _, path := range []string{target.TargetPath, target.LauncherPath, target.HookConfig, target.ManagedRulePath, c.installed.Registry, zcodeCredentialStore()} {
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
	if getOf(proof["separateRoots"], "source") != canaryCanonicalPath(sourceRoot) || !valuesEqual(proof["installReport"], decodeTestJSON(t, mustMarshalCanary(t, c.installed))) || getOf(proof["observed"], "projectState") != true || getOf(proof["observed"], "log") != false {
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
const nativeRuntime = {bundledCatalogue:fs.realpathSync(process.env.ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE),bundledRevision:JSON.parse(fs.readFileSync(process.env.ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE)).revision};
const activeRevision = JSON.parse(fs.readFileSync(process.env.ZCODE_BUILTIN_PROVIDER_CONFIG_FILE)).revision;
readline.createInterface({input: process.stdin}).on('line', line => {
  const request = JSON.parse(line);
  let result;
  if (request.method === 'session/create') {
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
