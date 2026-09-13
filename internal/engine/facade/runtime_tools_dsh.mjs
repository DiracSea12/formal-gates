import { spawn } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import { writeFileSync, renameSync, appendFileSync } from 'node:fs'
import { join, isAbsolute } from 'node:path'

export const name = 'formal-gates-native-operation'
export const inject = ['tools']

// This is a DSH tool plugin, not a workflow scheduler. One native process owns
// one bound engine action. ACP startup can depend on formalGatesNativeReady.
export function apply(ctx, config) {
  for (const field of ['executable', 'root', 'requestDirectory', 'sourceInfoPath']) {
    if (!isAbsolute(config?.[field] ?? '')) throw new Error(`DSH_CONFIG: ${field} must be absolute`)
  }
  if (!config.runId || !config.actionId || !config.tool || config.tool.name !== 'formal_gates_operation' || !Array.isArray(config.allowedTools)) {
    throw new Error('DSH_CONFIG: complete action and native tool configuration required')
  }
  const allowed = new Set([...(config.exposeOperation === false ? [] : ['formal_gates_operation']), ...config.allowedTools])
  const unregister = ctx.tools.register({
    name: config.tool.name,
    description: config.tool.description,
    parameters: config.tool.inputSchema,
    output: {
      schema: { type: 'object' },
      render(_args, result) { return [{ type: 'text', text: JSON.stringify(result) }] },
    },
    async execute(args, execution) {
      const identity = execution.agent?.session?.id
      if (typeof identity !== 'string' || !identity || typeof args?.requestId !== 'string' || !args.requestId) {
        throw new Error('DSH_BINDING: native identity and stable requestId required')
      }
      const requestPath = join(config.requestDirectory, Buffer.from(args.requestId).toString('base64url') + (args.resume === true ? '-resume.json' : '.json'))
      const payload = JSON.stringify(args)
      try {
        await writeFile(requestPath, payload, { flag: 'wx', mode: 0o600 })
      } catch (error) {
        if (error.code !== 'EEXIST') throw error
        if (await readFile(requestPath, 'utf8') !== payload) throw new Error('DSH_BINDING: stable requestId reused with different input')
      }
      return invokeOperation(config, identity, requestPath, execution.signal)
    },
  })
  ctx.on('agent/created', ({ agent }) => {
    const names = new Set(ctx.tools.schemas(agent).map(tool => tool.name))
    agent.ctx.tools.restrict({ allow: [...allowed].filter(name => names.has(name)) })
  })
  const unguard = ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'Engine action does not grant this native tool')
  ctx.effect(() => () => { unguard(); unregister() })
  let activeSession, activeTurn
  ctx.on('agent/inbox/claimed', ({ agent, message, turn }) => {
    const header = agent.session.header
    activeSession = header.id
    activeTurn = turn
    const persistence = ctx.get('sessionPersistence')
    const located = persistence?.locate(header)
    const info = { sessionId: header.id, inputId: message.id, turn,
      transcriptPath: typeof located?.path === 'string' ? located.path : '' }
    const temporary = config.sourceInfoPath + '.tmp'
    writeFileSync(temporary, JSON.stringify(info), { mode: 0o600 })
    renameSync(temporary, config.sourceInfoPath)
  })
  ctx.on('session/event', (session, event) => {
    if (session.header.id !== activeSession) return
    if (event.type !== 'compaction/summary' && event.data?.turn !== activeTurn) return
    if (!['assistant/message', 'assistant/attempt', 'turn/end', 'compaction/summary'].includes(event.type)) return
    appendFileSync(join(config.requestDirectory, '..', 'native-events.jsonl'),
      JSON.stringify({ sessionId: session.header.id, event }) + '\n', { mode: 0o600 })
  })
  ctx.provide('formalGatesNativeReady', true)
}

function invokeOperation(config, identity, path, signal) {
  return new Promise((resolve, reject) => {
    const child = spawn(config.executable, ['workflow', 'drive', '--root', config.root,
      '--run-id', config.runId, '--action-id', config.actionId,
      '--native-identity', identity, '--operation-native', path],
      { cwd: config.root, stdio: ['ignore', 'pipe', 'pipe'], signal })
    const output = [], errors = []
    let processError
    child.stdout.on('data', chunk => output.push(chunk))
    child.stderr.on('data', chunk => errors.push(chunk))
    child.on('error', error => { processError = error })
    child.on('close', code => {
      if (processError) { reject(processError); return }
      if (code !== 0) { reject(new Error(`Native operation exited ${code}: ${Buffer.concat(errors).toString('utf8')}`)); return }
      try {
        const result = JSON.parse(Buffer.concat(output).toString('utf8'))
        if (!result || Array.isArray(result) || typeof result !== 'object') throw new Error('Native operation did not return an object')
        resolve(result)
      } catch (error) { reject(error) }
    })
  })
}
