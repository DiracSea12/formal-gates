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
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

func TestZcodeCredentialsUseTheBoundNativeEnvironment(t *testing.T) {
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
	encrypt := func(secret, value string) string {
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
	store := filepath.Join(nativeHome, ".zcode", "v2", zcodeCredentialsFile)
	if err := os.MkdirAll(filepath.Dir(store), 0755); err != nil {
		t.Fatal(err)
	}
	for _, configuredSecret := range []string{"", "bound-fixture-secret"} {
		secret := configuredSecret
		if secret == "" {
			secret = strings.Join([]string{"zcode-credential-fallback", runtime.GOOS, nativeHome, account.Username}, ":")
		}
		value := map[string]string{"account-provider:coding-plan:p1:account:fixture:api-key": encrypt(secret, "local-native-credential"), "account-provider:coding-plan:p2:account:fixture:api-key": encrypt(secret, "other-native-credential")}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store, raw, 0600); err != nil {
			t.Fatal(err)
		}
		unbound := map[string]string{"ZCODE_DATA_BASE_DIR": nativeHome}
		if zcodeRequestAuth(node, "p1", unbound) != "" {
			t.Fatal("parent environment silently decrypted another native environment")
		}
		environment := map[string]string{"HOME": nativeHome, "USERPROFILE": nativeHome, "ZCODE_DATA_BASE_DIR": nativeHome, "ZCODE_CREDENTIAL_SECRET": configuredSecret}
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
	if err := os.WriteFile(store, []byte(`{"account-provider:coding-plan:provider:account:fixture:api-key":"offline-dummy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zcodeCredentialHelperEnv, store)
	t.Setenv("PATH", t.TempDir())
	binding, err := zcodeConfiguredBinding(settings.Values{Native: map[string]string{"zcodeExecutable": node, "zcodeScript": script}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newZcodeClient(objectOrEmpty(binding["launch"]), directory, directory, zcodeLaunchEnvironment(binding))
	if err != nil {
		t.Fatal(err)
	}
	defer client.client.close()
	for range 2 {
		reply, err := client.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "provider"})
		if err != nil || reply.status != "provider-runtime-headers-applied" || getOf(getOf(getOf(reply.payload, "result"), "requestAuth"), "apiKey") != "offline-dummy" {
			t.Fatal("configured executable or provider lost in credential helper", err, reply)
		}
		t.Log("PATH has no node; original configured launch and credential helper both succeeded")
	}
}
