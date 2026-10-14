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
// The account whose credential is spent is named the host's own way: the
// provider's identity entry a login wrote, or -- when a desktop OAuth login
// left no identity entry -- that login's current records, the encrypted
// oauth:active_provider family plus the same family's user_info id. The
// accountType argument carries the provider's family (the catalogue's
// access.accountType, or the request's own accountAccess value) and is spent
// only on that second source; the id addresses one exact retained key, so
// several retained accounts are normal and neither the first nor the last one
// is ever taken.
//
// usage: node zcode_credential.mjs <providerId> [storeFile] [accountType]
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
const OAUTH_ACTIVE_PROVIDER_KEY = 'oauth:active_provider';
const oauthUserInfoKey = (family) => `oauth:${family}:user_info`;

const providerId = (process.argv[2] || '').trim();
if (!providerId) {
  process.stderr.write('usage: zcode_credential.mjs <providerId> [storeFile] [accountType]\n');
  process.exit(2);
}
const accountType = (process.argv[4] || '').trim();
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

try {
  let identity = decrypt(store[`account-provider:${providerId}:identity`] || '').trim();
  if (!identity && accountType) {
    // A desktop OAuth login keeps no identity entry; its current account is
    // the active family plus that family's user_info id.
    const family = decrypt(store[OAUTH_ACTIVE_PROVIDER_KEY] || '').trim();
    if (family === accountType) {
      const userInfo = decrypt(store[oauthUserInfoKey(accountType)] || '').trim();
      if (userInfo) {
        const id = JSON.parse(userInfo).id;
        if (id != null) identity = String(id).trim();
      }
    }
  }
  if (!identity) process.exit(3);
  const wanted = ACCOUNT_KEY_PREFIX + providerId + IDENTITY_SEPARATOR
    + encodeURIComponent(identity) + ACCOUNT_KEY_SUFFIX;
  const plain = decrypt(store[wanted] || '').trim();
  if (!plain) process.exit(3);
  process.stdout.write(plain);
  process.exit(0);
} catch (error) {
  process.stderr.write(`credential for ${providerId} could not be read: ${error.message}\n`);
  process.exit(4);
}
