import { spawn } from 'node:child_process'

export const name = 'formal-gates-dsh'

const DEFAULT_TIMEOUT_MS = 30000
const PROVIDER = 'deepseek-harness'

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

function lifecyclePayload(ctx, event, info, child) {
  const header = headerOf(child)
  return {
    session_id: header && typeof header.id === 'string' ? header.id : '',
    transcript_path: transcriptPath(ctx, child),
    cwd: header && typeof header.cwd === 'string' ? header.cwd : process.cwd(),
    hook_event_name: event,
    agent_id: String(info.id),
    ...(event === 'SubagentStop' ? { stop_reason: String(info.stopReason || '') } : {}),
  }
}

function denyDecision(decision) {
  if (!decision || typeof decision !== 'object') return false
  return decision.permissionDecision === 'deny' ||
    decision.PermissionDecision === 'deny' ||
    decision.decision === 'block' ||
    decision.Decision === 'block'
}

function denyReason(decision) {
  if (!decision || typeof decision !== 'object') return ''
  return decision.permissionDecisionReason ||
    decision.PermissionDecisionReason ||
    decision.reason ||
    decision.Reason ||
    'blocked by formal-gates hook'
}

function runBinary(ctx, binary, args, payload, signal, timeoutMs, dshHome) {
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

export function apply(ctx, config = {}) {
  const warn = typeof ctx.logger && typeof ctx.logger.warn === 'function'
    ? (message) => ctx.logger.warn(message)
    : () => {}
  const binary = typeof config.binary === 'string' && config.binary.trim() !== '' ? config.binary : ''
  const timeoutMs = Number(config.timeoutMs) || DEFAULT_TIMEOUT_MS
  const dshHome = typeof config.dshHome === 'string' && config.dshHome.trim() !== '' ? config.dshHome : undefined
  if (!binary) {
    warn('formal-gates-dsh: config.binary is missing; hooks are disabled')
    return
  }

  const inflight = new Set()
  const controller = new AbortController()
  const subagentChildren = new Map()
  const track = (run) => {
    inflight.add(run)
    void run.then(() => inflight.delete(run), () => inflight.delete(run))
  }
  const resolveChild = (id) => {
    const agents = ctx && typeof ctx.get === 'function' ? ctx.get('agents') : undefined
    return agents && typeof agents.get === 'function' ? agents.get(id) : undefined
  }
  const capture = (event, info) => {
    // Stop 事件发生时 child 可能已从 agent registry 注销；保留 start 时拿到的
    // child，使 stop payload 仍能推导项目 cwd/session/transcript。
    if (event === 'SubagentStart') {
      const child = resolveChild(info.id)
      if (child) subagentChildren.set(info.runId, child)
    }
    const child = event === 'SubagentStop'
      ? (subagentChildren.get(info.runId) || resolveChild(info.id))
      : resolveChild(info.id)
    if (event === 'SubagentStop') subagentChildren.delete(info.runId)
    track(runBinary(ctx, binary, ['lifecycle', 'capture', '--provider', PROVIDER, '--event', event], lifecyclePayload(ctx, event, info, child), controller.signal, timeoutMs, dshHome)
      .then((result) => {
        if (result.error || (typeof result.code === 'number' && result.code !== 0)) {
          warn('formal-gates-dsh: lifecycle ' + event + ' hook failed: ' + (result.error || result.stderr || result.code))
        }
      }))
  }
  ctx.effect(() => () => {
    controller.abort()
    subagentChildren.clear()
    return Promise.allSettled([...inflight])
  }, 'formal-gates-dsh: drain lifecycle hooks')

  ctx.on('tools/pre-execute', async (exec, next) => {
    try {
      const result = await runBinary(ctx, binary, ['hook', 'decide'], payloadFor(ctx, exec), exec.signal, timeoutMs, dshHome)
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

  ctx.on('subagent/start', (info) => capture('SubagentStart', info))
  ctx.on('subagent/end', (info) => capture('SubagentStop', info))
}
