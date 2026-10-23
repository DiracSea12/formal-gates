// Drives the DSH plugin's control-client path against a fake MCP control
// service (the Go test helper installed at the stable launcher path). Proves
// that aborting one configure call sends notifications/cancelled for that
// exact request ID, detaches only that promise, keeps the cached client alive
// for the next call, and that pings remain independent of held control calls.
// Cancellation discards late RPC success; teardown closes every service.
// Usage: node dsh_plugin_client.test.mjs <log-file>
import { readFileSync } from 'node:fs'
import { apply } from '../dsh_plugin.mjs'

const [, , logFile] = process.argv
let failures = 0
const ok = (name, cond) => { console.log((cond ? 'PASS ' : 'FAIL ') + name); if (!cond) failures++ }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const waitFor = async (what, fn, timeoutMs = 10000) => {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (fn()) { ok(what, true); return true }
    await sleep(25)
  }
  ok(what, false)
  return false
}
const logNow = () => { try { return readFileSync(logFile, 'utf8') } catch { return '' } }
const pidAlive = (pid) => { try { process.kill(pid, 0); return true } catch { return false } }

const registered = []
const effects = []
let askCount = 0
const ctx = {
  logger: { warn() {} },
  on() {},
  inject(_deps, fn) {
    fn({
      tools: { register(spec) { registered.push(spec); return () => {} } },
      userQuestions: { ask: () => { askCount++; throw new Error('extra confirmation question') } },
      effect(fn) { effects.push(fn) },
    })
  },
  effect(fn) { effects.push(fn) },
}

apply(ctx, {})
// Registration completes only after the discovery round-trip; wait for the
// actual tool list, not a fixed delay.
if (!await waitFor('tools registered after discovery', () => registered.length === 2)) {
  throw new Error('control discovery did not finish:\n' + logNow())
}

const configure = registered.find((t) => t.name === 'configure')
const confirmControl = registered.find((t) => t.name === 'confirm_control')
ok('configure registered alongside confirmation tools', !!configure && !!confirmControl)
ok('discovery ping completed without user input', /PING_OK ping-discovery/.test(logNow()) && askCount === 0)

const execFor = () => {
  const controller = new AbortController()
  return {
    name: 'configure', callId: 'call-1', arguments: {},
    agent: { session: { header: { id: 'session-1', cwd: '/project' } } },
    signal: controller.signal,
    abort: () => controller.abort(),
  }
}

// First configure call: abort mid-flight; the client must send the cancel
// notification for that exact request id and reject only this promise.
const first = execFor()
const firstResult = configure.execute({ force: true, hold: true }, first).then(
  () => ({ resolved: true }),
  (error) => ({ error: String(error) }),
)
// The cached client's initialize is request id 1, so its first tools/call is
// id 2; wait until the service actually holds it before aborting.
await waitFor('first call reached the service', () => /CALL 2\b/.test(logNow()))
await waitFor('cached configure answers ping without user input', () => /PING_OK ping-held-configure/.test(logNow()))
ok('configure did not invoke user input', askCount === 0)
first.abort()
const outcome = await firstResult
ok('aborted configure rejects without running', !!outcome.error && /cancelled/.test(outcome.error))
await waitFor('cancel notification reached the service', () => /CANCELLED 2\b/.test(logNow()))
await waitFor('service replied late to the cancelled RPC', () => /LATE_REPLY 2\b/.test(logNow()))
ok('late successful RPC does not resolve the cancelled promise', !!outcome.error && !outcome.resolved)

// The same cached client must serve the next call: a second spawn or a dead
// client would both fail this. Request id 3 is the second tools/call.
const secondResult = await configure.execute({ force: true }, execFor())
let reply = {}
try { reply = JSON.parse(secondResult?.content?.[0]?.text ?? '') } catch {}
ok('second configure reply is exact', reply.status === 'selected' && reply.name === 'configure' && reply.root === '/project' && reply.requestId === 3)
ok('reused configure client answers ping', /PING_OK ping-configure-3/.test(logNow()))

const confirmation = execFor()
const confirmationResult = confirmControl.execute({ candidate: 3 }, confirmation).then(
  () => ({ resolved: true }),
  (error) => ({ error: String(error) }),
)
await waitFor('original control RPC is held with its identity', () => /HELD_CONTROL 2 candidate=3 session=session-1/.test(logNow()))
ok('granted control does not ask for confirmation again', askCount === 0)
await waitFor('held control answers ping independently', () => /PING_OK ping-held-control/.test(logNow()))
await waitFor('unknown request stays rejected', () => /UNKNOWN_REPLY -32601/.test(logNow()))
const confirmationPID = Number([...logNow().matchAll(/SPAWN (\d+)/g)].at(-1)?.[1])
confirmation.abort()
const cancelledConfirmation = await confirmationResult
ok('cancelled confirmation rejects', !!cancelledConfirmation.error && /cancelled/.test(cancelledConfirmation.error))
await waitFor('cancelled confirmation service exited', () => !pidAlive(confirmationPID))
await new Promise((resolve) => setImmediate(resolve))
ok('cancelled control cannot resolve its held RPC', !cancelledConfirmation.resolved && askCount === 0)

// Teardown through ctx.effect closes the cached connection; every spawned
// service process must be dead afterwards.
const spawns = (logNow().match(/SPAWN (\d+)/g) || []).map((m) => Number(m.split(' ')[1]))
// Discovery, cached configure, and the separate control call each own
// one service; cancellation of configure must not create another service.
ok('single cached service process reused', spawns.length === 3)
for (const fn of effects) {
  // effect callbacks return the actual teardown; invoke both.
  const cleanup = fn()
  if (typeof cleanup === 'function') cleanup()
}
await waitFor('cached service process exited', () => spawns.every((pid) => !pidAlive(pid)), 5000)
ok('all spawned service processes are dead', spawns.length > 0 && spawns.every((pid) => !pidAlive(pid)))

const log = logNow()
ok('exact request id cancelled', /CANCELLED 2\b/.test(log))
ok('second call reached the service', /CALL 3 name=configure root=\/project/.test(log))
ok('all operations avoided extra questions', askCount === 0)

console.log(failures === 0 ? 'ALL OK' : failures + ' FAILURES')
process.exit(failures === 0 ? 0 : 1)
