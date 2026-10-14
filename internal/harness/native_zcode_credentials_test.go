package harness

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

// zcodeTestEncrypt seals a value with the host's local cipher (see
// zcode_credential.mjs) under the given secret.
func zcodeTestEncrypt(t *testing.T, secret, value string) string {
	t.Helper()
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	sealed := aead.Seal(nil, nonce, []byte(value), nil)
	tag, body := sealed[len(sealed)-aead.Overhead():], sealed[:len(sealed)-aead.Overhead()]
	encode := base64.RawURLEncoding.EncodeToString
	return "enc:v1:" + encode(nonce) + "." + encode(tag) + "." + encode(body)
}

func TestZcodeCredentialsKeepBoundEnvironmentAndActiveAccount(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	original, err := os.ReadFile(zcodeCredentialHelper)
	if err != nil {
		t.Fatal(err)
	}
	if runNativePackageTest(t) {
		return
	}
	root, err := nativePackageRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "internal", "harness", zcodeCredentialHelper)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	engineHome, nativeHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", engineHome)
	t.Setenv("USERPROFILE", engineHome)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "")
	t.Setenv(zcodeCredentialHelperEnv, "")
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	encrypt := func(secret, value string) string { return zcodeTestEncrypt(t, secret, value) }
	store := filepath.Join(nativeHome, ".zcode", "v2", zcodeCredentialsFile)
	if err := os.MkdirAll(filepath.Dir(store), 0755); err != nil {
		t.Fatal(err)
	}
	for _, configuredSecret := range []string{"", "bound-fixture-secret"} {
		secret := configuredSecret
		if secret == "" {
			secret = strings.Join([]string{"zcode-credential-fallback", runtime.GOOS, nativeHome, account.Username}, ":")
		}
		value := map[string]string{
			"account-provider:p1:identity":                            encrypt(secret, "fixture"),
			"account-provider:p2:identity":                            encrypt(secret, "fixture"),
			"account-provider:coding-plan:p1:account:fixture:api-key": encrypt(secret, "local-native-credential"),
			"account-provider:coding-plan:p2:account:fixture:api-key": encrypt(secret, "other-native-credential"),
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store, raw, 0600); err != nil {
			t.Fatal(err)
		}
		unbound := map[string]string{"ZCODE_DATA_BASE_DIR": nativeHome, zcodeCredentialHelperEnv: store}
		if zcodeRequestAuth(node, "p1", "", unbound) != "" {
			t.Fatal("parent environment silently decrypted another native environment")
		}
		environment := map[string]string{"HOME": nativeHome, "USERPROFILE": nativeHome, "ZCODE_DATA_BASE_DIR": nativeHome, "ZCODE_CREDENTIAL_SECRET": configuredSecret, zcodeCredentialHelperEnv: store}
		directory := t.TempDir()
		client := &zcodeClient{executable: node, hostData: nativeHome, environment: environment, directory: directory}
		for _, name := range []string{"protocol.jsonl", "host.stderr", "interaction-status.json", "native-events.jsonl"} {
			if err := os.WriteFile(filepath.Join(directory, name), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for provider, wanted := range map[string]string{"p1": "local-native-credential", "p2": "other-native-credential"} {
			reply, err := client.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": provider})
			if err != nil {
				t.Fatal(err)
			}
			result := getOf(reply.payload, "result")
			if reply.status != "provider-runtime-headers-applied" || getOf(getOf(result, "requestAuth"), "apiKey") != wanted {
				t.Fatal("bound environment or selected account was lost")
			}
		}
		reopened := &zcodeClient{executable: node, hostData: nativeHome, environment: environment, directory: directory}
		reply, err := reopened.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "p1"})
		if err != nil || getOf(getOf(getOf(reply.payload, "result"), "requestAuth"), "apiKey") != "local-native-credential" {
			t.Fatal("recreated client changed its native environment")
		}
		value["account-provider:p1:identity"] = encrypt(secret, "new@example.test")
		value["account-provider:coding-plan:p1:account:new%40example.test:api-key"] = encrypt(secret, "current-native-credential")
		raw, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store, raw, 0600); err != nil {
			t.Fatal(err)
		}
		reply, err = reopened.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "p1"})
		if err != nil || getOf(getOf(getOf(reply.payload, "result"), "requestAuth"), "apiKey") != "current-native-credential" {
			t.Fatal("native account switch reused a historical credential", err)
		}
		delete(value, "account-provider:p1:identity")
		delete(value, "account-provider:coding-plan:p1:account:new%40example.test:api-key")
		raw, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store, raw, 0600); err != nil {
			t.Fatal(err)
		}
		reply, err = reopened.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "p1"})
		if err != nil || reply.status != "provider-runtime-headers-unavailable" {
			t.Fatal("native logout reused a historical credential", err)
		}
		reply, err = client.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "unprovisioned"})
		if err != nil || reply.status != "provider-runtime-headers-unavailable" {
			t.Fatal("missing native account was fabricated")
		}
		for _, name := range []string{"protocol.jsonl", "host.stderr", "interaction-status.json", "native-events.jsonl"} {
			body, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "native-credential") {
				t.Fatal("account material leaked into evidence", name)
			}
		}
		if os.Getenv("HOME") != engineHome || os.Getenv("USERPROFILE") != engineHome {
			t.Fatal("native binding changed parent process environment")
		}
	}
}

func TestZcodeConfiguredExecutableOwnsCredentialHelper(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	node = resolveNativePath(node)
	original, err := os.ReadFile(zcodeCredentialHelper)
	if err != nil {
		t.Fatal(err)
	}
	if runNativePackageTest(t) {
		return
	}
	root, err := nativePackageRoot()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "internal", "harness", zcodeCredentialHelper)
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, original, 0600); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "zcode.cjs")
	if err := os.WriteFile(script, []byte("process.stdin.resume(); process.stdin.on('end', () => process.exit(0));\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(directory, "credentials.json")
	if err := os.WriteFile(store, []byte(`{"account-provider:provider:identity":"fixture","account-provider:coding-plan:provider:account:fixture:api-key":"offline-dummy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zcodeCredentialHelperEnv, store)
	t.Setenv("PATH", t.TempDir())
	native := map[string]string{"zcodeExecutable": node, "zcodeScript": script}
	if err := zcodeResolvePaths(native, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	binding, err := zcodeConfiguredBinding(settings.Values{Native: native})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newZcodeClient(objectOrEmpty(binding["launch"]), directory, directory, zcodeLaunchEnvironment(binding))
	if err != nil {
		t.Fatal(err)
	}
	defer client.client.close()
	otherStore := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(otherStore, []byte(`{"account-provider:provider:identity":"fixture","account-provider:coding-plan:provider:account:fixture:api-key":"other-dummy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zcodeCredentialHelperEnv, otherStore)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZCODE_DATA_BASE_DIR", t.TempDir())
	for range 2 {
		reply, err := client.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "provider"})
		if err != nil || reply.status != "provider-runtime-headers-applied" || getOf(getOf(getOf(reply.payload, "result"), "requestAuth"), "apiKey") != "offline-dummy" {
			t.Fatal("configured executable or provider lost in credential helper", err, reply)
		}
		t.Log("PATH has no node; original configured launch and credential helper both succeeded")
	}
}

func TestZcodePathFreezeKeepsIndependentPathsAndFreshProofs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	type paths struct{ home, data, executable, script, catalogue, credentials, marker string }
	makePaths := func() paths {
		t.Helper()
		root := resolveNativePath(t.TempDir())
		p := paths{home: filepath.Join(root, "home"), data: filepath.Join(root, "data"), executable: filepath.Join(root, "bin", "node"), credentials: filepath.Join(root, "account", "credentials.json"), marker: filepath.Join(root, "executed")}
		p.script = filepath.Join(p.home, "app", "glm", "zcode.cjs")
		p.catalogue = filepath.Join(p.home, "app", "config", "provider", "zcode-builtin.json")
		for _, path := range []string{p.executable, p.script, p.catalogue, p.credentials} {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			body := "not a protocol or credentials document"
			if path == p.executable {
				body = "#!/bin/sh\nprintf executed > '" + p.marker + "'\nexit 91\n"
			}
			if err := os.WriteFile(path, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	a, b := makePaths(), makePaths()
	setPaths := func(p paths) {
		t.Helper()
		t.Setenv("HOME", p.home)
		t.Setenv("USERPROFILE", p.home)
		t.Setenv("PATH", filepath.Dir(p.executable))
		t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", "~/app/glm/zcode.cjs")
		t.Setenv("ZCODE_DATA_BASE_DIR", p.data)
		t.Setenv(zcodeCredentialHelperEnv, p.credentials)
	}
	setPaths(a)
	frozen := settings.Effective{Values: settings.Values{Model: "exact-native-model", ReasoningEffort: "high", Capacity: 3, QACapacity: 2, Native: map[string]string{}}, Sources: map[string]string{"zcodeExecutable": "default", "zcodeScript": "default", "zcodeHome": "default"}, Files: []string{"original-settings.json"}}
	if err := zcodeResolvePaths(frozen.Values.Native, frozen.Sources); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"zcodeExecutable": a.executable, "zcodeScript": a.script, "zcodeHome": a.home, "zcodeDataBaseDir": a.data, "zcodeBuiltinCatalogue": a.catalogue, "zcodePersonalConfig": filepath.Join(a.data, ".zcode", "v2", zcodePersonalConfig), "zcodeCredentialStore": a.credentials, "zcodeCataloguePinned": "true"}
	if !reflect.DeepEqual(frozen.Values.Native, want) {
		t.Fatalf("actual resolved paths = %v, want %v", frozen.Values.Native, want)
	}
	wantSources := map[string]string{"zcodeExecutable": "PATH", "zcodeScript": "FORMAL_GATES_ZCODE_SCRIPT", "zcodeHome": "HOME", "zcodeDataBaseDir": "ZCODE_DATA_BASE_DIR", "zcodeBuiltinCatalogue": "zcodeScript", "zcodePersonalConfig": "zcodeDataBaseDir", "zcodeCredentialStore": zcodeCredentialHelperEnv, "zcodeCataloguePinned": "zcodeScript"}
	if !reflect.DeepEqual(frozen.Sources, wantSources) {
		t.Fatalf("resolved sources = %v, want %v", frozen.Sources, wantSources)
	}
	body, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	var reopened settings.Effective
	if err := json.Unmarshal(body, &reopened); err != nil || !reflect.DeepEqual(reopened, frozen) {
		t.Fatalf("actual settings JSON readback differs: %s %v", body, err)
	}
	setPaths(b)
	// Host discovery candidates can change while the pinned catalogue remains fixed.
	candidate := filepath.Join(filepath.Dir(a.script), "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(candidate), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("host candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	for range 2 {
		binding, err := zcodeConfiguredBinding(reopened.Values)
		if err != nil {
			t.Fatal(err)
		}
		if err := zcodeValidateBinding(binding); err != nil {
			t.Fatal(err)
		}
		if getOf(getOf(binding, "launch"), "executable") != a.executable || getOf(getOf(binding, "launch"), "script") != a.script {
			t.Fatal("invocation selected current parent paths", binding)
		}
		env := zcodeLaunchEnvironment(binding)
		if env["HOME"] != a.home || env["ZCODE_DATA_BASE_DIR"] != a.data || env[zcodeCredentialHelperEnv] != a.credentials || env[zcodeBuiltinCatalogueEnv] != a.catalogue || env[zcodePersonalConfigEnv] != want["zcodePersonalConfig"] {
			t.Fatal("invocation environment changed frozen independent paths", env)
		}
		for _, path := range []string{a.executable, a.script} {
			proof, err := fileProofOf(path)
			if err != nil || !valuesEqual(getOf(getOf(binding, "files"), path), proof) {
				t.Fatal("invocation file proof differs from actual selected file", path, err)
			}
		}
		original = binding
	}
	if err := os.WriteFile(a.script, []byte("changed script"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := zcodeValidateBinding(original); err == nil {
		t.Fatal("original invocation proof accepted a changed script")
	}
	current, err := zcodeConfiguredBinding(reopened.Values)
	if err != nil || valuesEqual(current["files"], original["files"]) {
		t.Fatal("new invocation reused old file proof", err)
	}
	fresh, sources := map[string]string{}, map[string]string{}
	if err := zcodeResolvePaths(fresh, sources); err != nil || fresh["zcodeExecutable"] != b.executable || fresh["zcodeHome"] != b.home || fresh["zcodeDataBaseDir"] != b.data || fresh["zcodeCredentialStore"] != b.credentials || fresh["zcodeBuiltinCatalogue"] != b.catalogue {
		t.Fatal("new start did not resolve independent B input", fresh, err)
	}
	if os.Getenv("HOME") != b.home || os.Getenv(zcodeCredentialHelperEnv) != b.credentials {
		t.Fatal("binding mutated parent environment")
	}
	for _, marker := range []string{a.marker, b.marker} {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("path mapping or binding executed the native fixture", marker, err)
		}
	}
	if _, err := zcodeConfiguredBinding(settings.Values{Native: map[string]string{"zcodeExecutable": a.executable, "zcodeScript": a.script, "zcodeHome": a.home}}); err == nil {
		t.Fatal("missing frozen payload triggered discovery")
	}
}

func TestZcodePathFreezePreservesExplicitHomeAndHostCatalogueResolution(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	parent, explicit, data := resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir())
	script := filepath.Join(parent, "glm", "zcode.cjs")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("not executable code"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", parent)
	t.Setenv("USERPROFILE", parent)
	t.Setenv("ZCODE_DATA_BASE_DIR", data)
	t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", "/missing/env-script")
	t.Setenv(zcodeCredentialHelperEnv, "")
	t.Setenv(zcodeBuiltinCatalogueEnv, "")
	t.Setenv(zcodePersonalConfigEnv, "")
	for _, home := range []string{"", explicit} {
		name := "inherited-home"
		if home != "" {
			name = "explicit-home"
		}
		t.Run(name, func(t *testing.T) {
			native := map[string]string{"zcodeExecutable": node, "zcodeScript": script, "zcodeHome": home}
			sources := map[string]string{"zcodeExecutable": "settings.json", "zcodeScript": "settings.json"}
			if home != "" {
				sources["zcodeHome"] = "settings.json"
			}
			if err := zcodeResolvePaths(native, sources); err != nil {
				t.Fatal(err)
			}
			wantHome, wantData := parent, data
			if home != "" {
				wantHome, wantData = explicit, explicit
			}
			if native["zcodeScript"] != script || native["zcodeHome"] != wantHome || native["zcodeDataBaseDir"] != wantData || native["zcodeCredentialStore"] != filepath.Join(wantData, ".zcode", "v2", zcodeCredentialsFile) {
				t.Fatal("original explicit home/script precedence changed", native)
			}
			if sources["zcodeExecutable"] != "settings.json" || sources["zcodeScript"] != "settings.json" || home != "" && sources["zcodeHome"] != "settings.json" {
				t.Fatal("explicit settings source was discarded", sources)
			}
			for _, name := range []string{"zcodeBuiltinCatalogue", "zcodePersonalConfig"} {
				value, exists := native[name]
				if !exists || value != "" || sources[name] != "host resolves catalogue" {
					t.Fatal("no catalogue was represented as an adopted file", name, native, sources)
				}
			}
			binding, err := zcodeConfiguredBinding(settings.Values{Native: native})
			if err != nil || zcodePinnedCatalogue(binding) != "" {
				t.Fatal("host catalogue resolution became a launcher pin", err, binding)
			}
			missing := map[string]string{"zcodeExecutable": node, "zcodeScript": "/missing/script"}
			if err := zcodeResolvePaths(missing, map[string]string{}); err == nil {
				t.Fatal("unavailable selected script was frozen")
			}
		})
	}
}

func TestZcodePathFreezeKeepsInheritedCatalogueValues(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	root := resolveNativePath(t.TempDir())
	script := filepath.Join(root, "glm", "zcode.cjs")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("not a protocol script"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	for _, pair := range [][2]string{{" relative-catalogue.json ", " relative-personal.json "}, {"", "relative-personal.json"}, {"relative-catalogue.json", ""}, {"", ""}} {
		t.Setenv(zcodeBuiltinCatalogueEnv, pair[0])
		t.Setenv(zcodePersonalConfigEnv, pair[1])
		native, sources := map[string]string{"zcodeExecutable": node, "zcodeScript": script}, map[string]string{}
		if err := zcodeResolvePaths(native, sources); err != nil {
			t.Fatal(err)
		}
		wantBuiltin, wantPersonal := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
		if native["zcodeBuiltinCatalogue"] != wantBuiltin || native["zcodePersonalConfig"] != wantPersonal {
			t.Fatal("inherited catalogue values changed host trim/relative path semantics", native)
		}
		t.Setenv(zcodeBuiltinCatalogueEnv, "other-catalogue.json")
		t.Setenv(zcodePersonalConfigEnv, "other-personal.json")
		binding, err := zcodeConfiguredBinding(settings.Values{Native: native})
		if err != nil {
			t.Fatal(err)
		}
		environment := zcodeLaunchEnvironment(binding)
		if environment[zcodeBuiltinCatalogueEnv] != wantBuiltin || environment[zcodePersonalConfigEnv] != wantPersonal || zcodePinnedCatalogue(binding) != "" {
			t.Fatal("invocation inherited different catalogue paths", environment)
		}
		// Each variable remains independent: a one-sided original setting is
		// left for the host's own protocol validation rather than completed here.
		if wantBuiltin != "" && sources["zcodeBuiltinCatalogue"] != zcodeBuiltinCatalogueEnv || wantPersonal != "" && sources["zcodePersonalConfig"] != zcodePersonalConfigEnv {
			t.Fatal("inherited catalogue source was lost", sources)
		}
	}
	// A launcher fallback takes precedence over inherited catalogue variables.
	fallback := filepath.Join(root, "config", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(fallback), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("not a catalogue document"), 0600); err != nil {
		t.Fatal(err)
	}
	native, sources := map[string]string{"zcodeExecutable": node, "zcodeScript": script}, map[string]string{}
	if err := zcodeResolvePaths(native, sources); err != nil || native["zcodeBuiltinCatalogue"] != fallback || sources["zcodeBuiltinCatalogue"] != "zcodeScript" || native["zcodeCataloguePinned"] != "true" {
		t.Fatal("launcher fallback did not keep original precedence", native, sources, err)
	}
}
