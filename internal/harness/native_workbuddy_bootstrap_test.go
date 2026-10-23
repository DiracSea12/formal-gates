package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The desktop bridge preserves CLI arguments and delivers the selected native
// key material to the exact process/session/token expected by the CLI receiver.
func TestWorkBuddyDesktopCredentialBootstrap(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable")
	}
	_, file, _, _ := runtime.Caller(0)
	helper := filepath.Join(filepath.Dir(file), "native_workbuddy.mjs")
	for _, policy := range []string{"fields", "files", "missing-policy", "missing-key"} {
		t.Run(policy, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "app.asar")
			write := func(path, body string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			buildPolicy := policy
			if policy == "missing-key" {
				buildPolicy = "fields"
			}
			write(filepath.Join(archive, "main", "index.js"), "var buildPolicy=resolveAtRestEncryptionBuildPolicy(`"+buildPolicy+"`)")
			write(filepath.Join(archive, "main", "dist.js"), `
const assert = require('node:assert/strict');
const keys = [];
process.on('exit', () => assert.ok(keys.every(key => key.every(byte => byte === 0))));
exports.s = () => {};
exports.c = value => ['off', 'fields', 'files'].includes(value);
exports.t = (value, minimum) => ['off','fields','files'].indexOf(value) >= ['off','fields','files'].indexOf(minimum);
exports.T = raw => {
  assert.equal(raw, process.env.BOOTSTRAP_PROFILE);
  if (raw === 'missing-key') return {symmetric:{status:'unavailable',category:'missing-key'},developerPublicKey:{status:'available',value:{id:raw}}};
  const key = Buffer.from(raw); keys.push(key);
  return {symmetric:{status:'available',value:{keyId:raw,key}}, developerPublicKey:{status:'available',value:{id:raw}}};
};
exports.l = root => require('node:path').join(root,'keyblob');
exports.u = value => {
  assert.equal(value.keyblobPath, require('node:path').join(process.env.WORKBUDDY_CONFIG_DIR,'keyblob'));
  assert.equal(value.protectorKey.key.toString(), process.env.BOOTSTRAP_PROFILE);
  assert.equal(value.createIfMissing, false);
  const key = Buffer.from('user-'+process.env.BOOTSTRAP_PROFILE); keys.push(key);
  return {status:'available',value:{keyId:'user',key}};
};
exports.a = value => ({version:1,policy:value.policy,symmetricKey:{keyId:value.symmetricKey.keyId,keyBase64:value.symmetricKey.key.toString('base64')},userKey:{keyId:value.userKey.keyId,keyBase64:value.userKey.key.toString('base64')},developerPublicKey:value.developerPublicKey});
exports.i = value => { value.symmetricKey?.key.fill(0); value.userKey?.key.fill(0); assert.ok(keys.every(key => key.every(byte => byte === 0))); };
`)
			preload := filepath.Join(root, "native.cjs")
			write(preload, `process._linkedBinding = name => { require('node:assert/strict').equal(name,'electron_browser_workbuddy_storage'); return {loggerGet:() => process.env.BOOTSTRAP_PROFILE}; };`)
			cli := filepath.Join(root, "codebuddy.cjs")
			write(cli, `
const assert = require('node:assert/strict'), net = require('node:net');
assert.deepEqual(process.argv.slice(2), ['--print','--model','native-'+process.env.BOOTSTRAP_PROFILE]);
assert.equal(process.argv[1], __filename);
const server = net.createServer(client => {
  let text = '';
  client.on('data', chunk => {
    text += chunk;
    if (!text.includes('\n')) return;
    const payload = JSON.parse(text.trim());
    assert.deepEqual(Object.keys(payload).sort(), ['bootstrap','pid','sessionId','token','version']);
    assert.equal(payload.version, 1);
    assert.equal(payload.pid, process.pid);
    assert.equal(payload.sessionId, process.env.CODEBUDDY_SIDECAR_READY_SESSION_ID);
    assert.equal(payload.token, process.env.CODEBUDDY_SIDECAR_READY_TOKEN);
    assert.ok(payload.token.length > 20);
    assert.equal(payload.bootstrap.policy, process.env.BOOTSTRAP_PROFILE);
    assert.equal(Buffer.from(payload.bootstrap.symmetricKey.keyBase64,'base64').toString(), process.env.BOOTSTRAP_PROFILE);
    assert.equal(Buffer.from(payload.bootstrap.userKey.keyBase64,'base64').toString(), 'user-'+process.env.BOOTSTRAP_PROFILE);
    client.end(JSON.stringify({ok:true,policy:payload.bootstrap.policy})+'\n', () => server.close());
    process.stdout.write(JSON.stringify({pidMatched:true,selection:process.argv.slice(2),bootstrapAccepted:true}));
  });
});
server.listen(process.env.CODEBUDDY_SIDECAR_CREDENTIAL_BOOTSTRAP_SOCKET);
`)
			cmd := exec.Command(node, "--require", preload, helper, "--print", "--model", "native-"+policy)
			cmd.Env = nativeChildEnvironment(map[string]string{
				"FORMAL_GATES_WORKBUDDY_ARCHIVE": archive, "FORMAL_GATES_WORKBUDDY_CLI": cli,
				"WORKBUDDY_CONFIG_DIR": root, "BOOTSTRAP_PROFILE": policy,
			})
			output, err := cmd.CombinedOutput()
			if strings.HasPrefix(policy, "missing-") {
				category := strings.TrimPrefix(policy, "missing-")
				if err == nil || string(output) != "WORKBUDDY_AUTH: desktop credential bootstrap unavailable ("+category+")\n" {
					t.Fatalf("missing native capability: err=%v output=%q", err, output)
				}
				return
			}
			var observed struct {
				PIDMatched        bool     `json:"pidMatched"`
				Selection         []string `json:"selection"`
				BootstrapAccepted bool     `json:"bootstrapAccepted"`
			}
			if err != nil || json.Unmarshal(output, &observed) != nil || !observed.PIDMatched || !observed.BootstrapAccepted {
				t.Fatalf("desktop native bootstrap: err=%v output=%q", err, output)
			}
		})
	}
}
