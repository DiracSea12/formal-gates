import { spawn } from 'node:child_process'
import { join } from 'node:path'
import { homedir } from 'node:os'
import { createInterface } from 'node:readline'

export const name = 'formal-gates-dsh'

const DEFAULT_TIMEOUT_MS = 30000

function headerOf(agent) {
  return agent && agent.session && agent.session.header ? agent.session.header : undefined
}

function transcriptPath(ctx, agent) {
  const header = headerOf(agent)
  if (!header) return ''
  try {
    const persistence = ctx && typeof ctx.get === 'function' ? ctx.get('sessionPersistence') : undefined
    const located = persistence && typeof persistence.locate === 'function' ? persistence.locate(header) : undefined
    return located && typeof located.path === 'string' ? located.path : ''
  } catch (error) {
    return ''
  }
}

function normalizeArguments(args) {
  if (typeof args !== 'string') return args ?? {}
  try {
    const parsed = JSON.parse(args)
    return parsed && typeof parsed === 'object' ? parsed : { command: args }
  } catch (error) {
    return { command: args }
  }
}

function payloadFor(ctx, exec) {
  const agent = exec && exec.agent
  const header = headerOf(agent)
  return {
    session_id: header && typeof header.id === 'string' ? header.id : '',
    transcript_path: transcriptPath(ctx, agent),
    cwd: header && typeof header.cwd === 'string' ? header.cwd : process.cwd(),
    hook_event_name: 'PreToolUse',
    tool_name: exec.name,
    // DSH 的 tools/pre-execute 可能把 arguments 作为 JSON 字符串传入；
    // formal-gates hook 决策器只从对象字段提取 command/file_path，因此先归一化。
    tool_input: normalizeArguments(exec.arguments),
    tool_use_id: exec.callId,
    ...(agent && header && (header.origin === 'subagent' || (typeof header.delegationDepth === 'number' && header.delegationDepth > 0)) ? { agent_id: header.id } : {}),
  }
}

function denyDecision(decision) {
  if (!decision || typeof decision !== 'object') return false
  return decision.permissionDecision === 'deny'
}

function denyReason(decision) {
  if (!decision || typeof decision !== 'object') return ''
  return decision.permissionDecisionReason ||
    decision.reason ||
    'blocked by formal-gates hook'
}

function runBinary(binary, args, payload, signal, timeoutMs, dshHome) {
  return new Promise((resolve) => {
    let settled = false
    let stdout = ''
    let stderr = ''
    const child = spawn(binary, args, {
      windowsHide: true,
      stdio: ['pipe', 'pipe', 'pipe'],
      env: typeof dshHome === 'string' && dshHome !== '' ? { ...process.env, DSH_HOME: dshHome } : process.env,
    })
    const finish = (result) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      resolve(result)
    }
    const timer = setTimeout(() => {
      child.kill()
    }, Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : DEFAULT_TIMEOUT_MS)
    child.stdout.on('data', (chunk) => { stdout += chunk })
    child.stderr.on('data', (chunk) => { stderr += chunk })
    child.on('error', (error) => finish({ error: String(error) }))
    child.on('close', (code) => finish({ code, stdout, stderr }))
    child.stdin.on('error', () => {})
    child.stdin.end(JSON.stringify(payload))
    if (signal && typeof signal.addEventListener === 'function') {
      signal.addEventListener('abort', () => child.kill(), { once: true })
    }
  })
}

function parseDecision(result) {
  if (result.error || result.stdout === undefined) return undefined
  try {
    const text = String(result.stdout).trim()
    if (!text) return undefined
    return JSON.parse(text)
  } catch (error) {
    return undefined
  }
}

function stableLauncherPath() {
  const home = process.env.HOME || process.env.USERPROFILE || homedir()
  if (process.platform === 'win32') {
    const localAppData = process.env.LOCALAPPDATA || join(home, 'AppData', 'Local')
    return join(localAppData, 'formal-gates', 'bin', 'formal-gates.exe')
  }
  return join(home, '.local', 'bin', 'formal-gates')
}

function controlClient(binary, dshHome, ask) {
  const child = spawn(binary, ['workflow', 'serve'], {
    windowsHide: true,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: dshHome ? { ...process.env, DSH_HOME: dshHome } : process.env,
  })
  const pending = new Map()
  let counter = 0
  let closed = false
  let stderr = ''
  const fail = (error) => {
    for (const waiter of pending.values()) waiter.reject(error)
    pending.clear()
  }
  const send = (message) => {
    if (!closed) child.stdin.write(JSON.stringify({ jsonrpc: '2.0', ...message }) + '\n')
  }
  child.stderr.on('data', (chunk) => { stderr += chunk })
  child.on('error', fail)
  child.on('close', () => fail(new Error(stderr || 'formal-gates control service closed')))
  child.stdin.on('error', fail)
  const lines = createInterface({ input: child.stdout })
  lines.on('line', async (line) => {
    try {
      const message = JSON.parse(line)
      if (message.method) {
        if (message.method !== 'elicitation/create' || !ask) {
          if (message.id !== undefined) send({ id: message.id, error: { code: -32601, message: 'No user interaction handler' } })
          return
        }
        try {
          const response = await ask(message.params)
          send({ id: message.id, result: response })
        } catch (error) {
          send({ id: message.id, error: { code: -32603, message: String(error) } })
        }
        return
      }
      const waiter = pending.get(message.id)
      if (!waiter) return
      pending.delete(message.id)
      if (message.error) waiter.reject(new Error(message.error.message))
      else waiter.resolve(message.result)
    } catch (error) {
      fail(error)
    }
  })
  return {
    request(method, params) {
      return new Promise((resolve, reject) => {
        const id = ++counter
        pending.set(id, { resolve, reject })
        send({ id, method, params })
      })
    },
    notify(method) { send({ method }) },
    close() { closed = true; fail(new Error('formal-gates confirmation cancelled')); lines.close(); child.kill() },
  }
}

async function initializeControl(client) {
  await client.request('initialize', { protocolVersion: '2025-06-18', capabilities: { elicitation: { form: {} } }, clientInfo: { name: 'formal-gates-dsh', version: '1' } })
  client.notify('notifications/initialized')
}

async function registerControlTools(ctx, binary, dshHome, provider) {
  const discovery = controlClient(binary, dshHome)
  let tools
  try {
    await initializeControl(discovery)
    tools = (await discovery.request('tools/list', {})).tools
  } finally {
    discovery.close()
  }
  for (const tool of tools) {
    const remove = ctx.tools.register({
      name: tool.name,
      description: tool.description,
      parameters: tool.inputSchema,
      output: { schema: { type: 'object' }, render(_args, result) { return result.content } },
      async execute(args, exec) {
        if (!exec.agent) throw new Error('formal-gates confirmation requires the live root agent')
        const client = controlClient(binary, dshHome, async (request) => {
          if (request.mode !== 'form') throw new Error('formal-gates confirmation requires a native form')
          const answer = await ctx.userQuestions.ask({
            agent: exec.agent,
            signal: exec.signal,
            questions: [{ id: 'confirmation', header: 'formal-gates', question: request.message, options: [{ label: '确认并执行' }, { label: '取消' }] }],
          })
          const choice = answer.answers.find((item) => item.id === 'confirmation')
          return choice?.selected.length === 1 && choice.selected[0] === '确认并执行'
            ? { action: 'accept', content: { confirmOperation: true } }
            : { action: 'decline' }
        })
        const cancel = () => client.close()
        exec.signal?.addEventListener('abort', cancel, { once: true })
        try {
          exec.signal?.throwIfAborted()
          await initializeControl(client)
          const root = headerOf(exec.agent)?.cwd
          const identity = payloadFor(ctx, exec)
          const owner = { session: identity.session_id, transcript: identity.transcript_path, provider: identity.transcript_path ? provider : '' }
          return await client.request('tools/call', { name: tool.name, arguments: { ...(root ? { root } : {}), ...args }, _meta: { 'formal-gates/owner': owner } })
        } finally {
          exec.signal?.removeEventListener('abort', cancel)
          client.close()
        }
      },
    })
    ctx.effect(() => remove)
  }
}

export function apply(ctx, config = {}) {
  const warn = typeof ctx.logger && typeof ctx.logger.warn === 'function'
    ? (message) => ctx.logger.warn(message)
    : () => {}
  // DSH resolves the plugin beside its home patch. Hook writes use the
  // registry-admitted stable launcher throughout upgrades and rollbacks.
  const binary = stableLauncherPath()
  const timeoutMs = Number(config.timeoutMs) || DEFAULT_TIMEOUT_MS
  const dshHome = typeof config.dshHome === 'string' && config.dshHome.trim() !== '' ? config.dshHome : undefined

  ctx.on('tools/pre-execute', async (exec, next) => {
    try {
      const result = await runBinary(binary, ['hook', 'decide', '--provider', config.provider], payloadFor(ctx, exec), exec.signal, timeoutMs, dshHome)
      const decision = parseDecision(result)
      if (denyDecision(decision)) return { kind: 'deny', reason: denyReason(decision) }
      if (result.error || (typeof result.code === 'number' && result.code !== 0)) {
        warn('formal-gates-dsh: PreToolUse hook failed: ' + (result.error || result.stderr || result.code))
      }
    } catch (error) {
      warn('formal-gates-dsh: PreToolUse hook failed: ' + String(error))
    }
    return next()
  })
  ctx.inject(['tools', 'userQuestions'], (control) => registerControlTools(control, binary, dshHome, config.provider))
}
