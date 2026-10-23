// WorkBuddy's own runtime supplies the desktop credential bootstrap. The CLI
// receives it through its native in-memory channel in this same process.
import { createRequire } from 'node:module'
import { readFileSync, unlinkSync } from 'node:fs'
import { createConnection } from 'node:net'
import { randomUUID } from 'node:crypto'
import { join } from 'node:path'
import { tmpdir } from 'node:os'

const require = createRequire(import.meta.url)
let crypto, bootstrap, connection, timer, socketPath

function dispose() {
  clearTimeout(timer)
  connection?.removeAllListeners('close')
  connection?.destroy()
  if (bootstrap) crypto.i(bootstrap)
  bootstrap = undefined
  if (socketPath && process.platform !== 'win32') {
    try { unlinkSync(socketPath) } catch {}
  }
}

function fail(category) {
  dispose()
  process.stderr.write(`WORKBUDDY_AUTH: desktop credential bootstrap unavailable (${category})\n`)
  process.exit(1)
}

process.once('exit', dispose)

try {
  const archive = process.env.FORMAL_GATES_WORKBUDDY_ARCHIVE
  const cli = process.env.FORMAL_GATES_WORKBUDDY_CLI
  crypto = require(join(archive, 'main', 'dist.js'))
  crypto.s()
  const main = readFileSync(join(archive, 'main', 'index.js'), 'utf8')
  const policy = main.match(/\bbuildPolicy\s*=\s*resolveAtRestEncryptionBuildPolicy\(`(off|fields|files)`\)/)?.[1]
  if (!policy || !crypto.c(policy)) fail('policy')
  const keys = crypto.T(process._linkedBinding('electron_browser_workbuddy_storage').loggerGet())
  bootstrap = { policy }
  if (keys.symmetric.status === 'available') bootstrap.symmetricKey = keys.symmetric.value
  else bootstrap.symmetricUnavailable = keys.symmetric.category
  if (bootstrap.symmetricKey && keys.developerPublicKey.status === 'available') {
    const user = crypto.u({
      keyblobPath: crypto.l(process.env.WORKBUDDY_CONFIG_DIR),
      protectorKey: bootstrap.symmetricKey,
      createIfMissing: false,
    })
    if (user.status === 'available') {
      bootstrap.userKey = user.value
      bootstrap.developerPublicKey = keys.developerPublicKey.value
    } else bootstrap.asymmetricUnavailable = user.category
  } else bootstrap.asymmetricUnavailable = keys.developerPublicKey.category ?? keys.symmetric.category
  if (crypto.t(policy, 'fields') && !bootstrap.symmetricKey ||
      crypto.t(policy, 'files') && (!bootstrap.userKey || !bootstrap.developerPublicKey)) fail('key')

  const sessionId = randomUUID(), token = randomUUID()
  const name = `formal-gates-workbuddy-${randomUUID()}`
  socketPath = process.platform === 'win32' ? `\\\\.\\pipe\\${name}` : join(tmpdir(), `${name}.sock`)
  Object.assign(process.env, {
    WORKBUDDY_AT_REST_ENCRYPTION: policy,
    CODEBUDDY_SIDECAR_READY_SOCKET: `${socketPath}-ready`,
    CODEBUDDY_SIDECAR_READY_SESSION_ID: sessionId,
    CODEBUDDY_SIDECAR_READY_TOKEN: token,
    CODEBUDDY_SIDECAR_CREDENTIAL_BOOTSTRAP_SOCKET: socketPath,
  })
  const deadline = Date.now() + 10000
  function deliver() {
    const client = createConnection(socketPath)
    connection = client
    let received = ''
    client.setTimeout(Math.max(1, deadline - Date.now()), () => fail('timeout'))
    client.once('connect', () => client.write(JSON.stringify({
      version: 1, sessionId, token, pid: process.pid, bootstrap: crypto.a(bootstrap),
    }) + '\n'))
    client.on('data', data => {
      received += data.toString('utf8')
      if (received.length > 4096) fail('acknowledgement')
      const end = received.indexOf('\n')
      if (end < 0) return
      try {
        const acknowledgement = JSON.parse(received.slice(0, end))
        if (acknowledgement.ok !== true || acknowledgement.policy !== policy) fail('acknowledgement')
        dispose()
      } catch { fail('acknowledgement') }
    })
    client.once('error', error => {
      client.removeAllListeners('close')
      client.destroy()
      if (['ENOENT', 'ECONNREFUSED'].includes(error.code) && Date.now() < deadline) timer = setTimeout(deliver, 20)
      else fail('transport')
    })
    client.once('close', () => fail('transport'))
  }
  process.argv[1] = cli
  require(cli)
  deliver()
} catch { fail('initialization') }
