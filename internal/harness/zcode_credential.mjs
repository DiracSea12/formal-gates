// Read one account provider's request credential out of the host's own store.
//
// The ZCode app-server does not resolve account credentials by itself. In host
// mode its provider runtime headers port (bundle: createProviderRuntimeHeadersPort)
// always reports shouldRefreshBeforeModelRequest() === true, so before every model
// request the host asks its *client* for the request auth and throws when the
// reply carries none:
//
//     interaction/requestProviderRuntimeHeaders (requestId, sessionId, turnId,
//       workspace, modelSelection, providerId, accountAccess?, reason)
//     -> { headersApplied: true, requestAuth: { apiKey?, headers? } }
//
// A launcher that starts the app-server is that client, so it has to answer. The
// credential is the one the host already provisioned for this account; this file
// only reads it, through the host's own cipher, which is entirely local:
//
//     key = sha256(ZCODE_CREDENTIAL_SECRET
//                  || "zcode-credential-fallback:<platform>:<homedir>:<user>")
//     aes-256-gcm, text form "enc:v1:<iv b64url>.<tag b64url>.<ct b64url>"
//
// No keychain and no user interaction are involved, and a value the host stored in
// the clear is returned as it is.
//
// usage: node zcode_credential.mjs <providerId> [storeFile]
//
// stdout: the credential, or nothing. Never a diagnostic, so a caller cannot
// mistake a message for a secret. Exit 3 means "this host has no such provider".
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const PREFIX = 'enc:v1:';
const ACCOUNT_KEY_PREFIX = 'account-provider:coding-plan:';
const ACCOUNT_KEY_SUFFIX = ':api-key';
const IDENTITY_SEPARATOR = ':account:';

const providerId = (process.argv[2] || '').trim();
if (!providerId) {
  process.stderr.write('usage: zcode_credential.mjs <providerId> [storeFile]\n');
  process.exit(2);
}
const storeFile = process.argv[3] || process.env.FORMAL_GATES_ZCODE_CREDENTIALS
  || path.join(os.homedir(), '.zcode', 'v2', 'credentials.json');

function credentialSecret() {
  const configured = (process.env.ZCODE_CREDENTIAL_SECRET || '').trim();
  if (configured) return configured;
  let user = 'unknown';
  try { user = os.userInfo().username; } catch { /* the host falls back too */ }
  return ['zcode-credential-fallback', os.platform(), os.homedir(), user].join(':');
}

function decrypt(value) {
  if (!value.startsWith(PREFIX)) return value;
  const [iv, tag, ciphertext] = value.slice(PREFIX.length).split('.');
  if (!iv || !tag || !ciphertext) throw new Error('invalid ciphertext format');
  const key = crypto.createHash('sha256').update(credentialSecret()).digest();
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, Buffer.from(iv, 'base64url'));
  decipher.setAuthTag(Buffer.from(tag, 'base64url'));
  return Buffer.concat([decipher.update(Buffer.from(ciphertext, 'base64url')), decipher.final()])
    .toString('utf8');
}

let store;
try {
  store = JSON.parse(fs.readFileSync(storeFile, 'utf8'));
} catch {
  process.exit(3);
}

const wanted = ACCOUNT_KEY_PREFIX + providerId + IDENTITY_SEPARATOR;
for (const [name, value] of Object.entries(store)) {
  if (!name.startsWith(wanted) || !name.endsWith(ACCOUNT_KEY_SUFFIX)) continue;
  try {
    const plain = decrypt(value).trim();
    if (!plain) continue;
    process.stdout.write(plain);
    process.exit(0);
  } catch (error) {
    process.stderr.write(`credential for ${providerId} could not be read: ${error.message}\n`);
    process.exit(4);
  }
}
process.exit(3);
