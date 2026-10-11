// DSH-native workspace tool registration, policy and original source evidence.
import { appendFileSync, writeFileSync, renameSync, readFileSync, existsSync } from 'node:fs'
import { join, isAbsolute } from 'node:path'
import { spawn } from 'node:child_process'
export const name = 'formal-gates-harness-native'
export const inject = ['tools']
export function apply(ctx, config) {
  if (!isAbsolute(config.directory) || typeof config.read_only !== 'boolean') throw new Error('Invalid harness native configuration')
  if (config.managed_tools) {
    if (!isAbsolute(config.executable) || !Array.isArray(config.tools)) throw new Error('Native workspace tool requires its executable and schema')
    let previous = Promise.resolve()
    const unregister = config.tools.map(tool => ctx.tools.register({
      name: tool.name,
      description: tool.description,
      parameters: tool.inputSchema,
      output: { schema: { type: 'object' }, render(_args, result) { return [{ type: 'text', text: JSON.stringify(result) }] } },
      execute(args) {
        const call = previous.then(() => invokeTool(config, tool.name, args))
        previous = call.catch(() => {})
        return call
      },
    }))
    const allowed = new Set([...config.tools.map(tool => tool.name), 'AskUserQuestion', 'ask_user_question'])
    const unguard = ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'Issued action uses the registered workspace tool')
    ctx.effect(() => () => { unguard(); unregister.forEach(remove => remove()) })
    ctx.on('agent/created', ({ agent }) => {
      const tools = ctx.tools.schemas(agent).map(tool => tool.name)
      agent.ctx.tools.restrict({ deny: tools.filter(name => !allowed.has(name)) })
      writeFileSync(join(config.directory, 'tool-policy.json'), JSON.stringify({ read_only: config.read_only, available: tools, allowed: tools.filter(name => allowed.has(name)) }), { mode: 0o600 })
    })
  }
  if (config.read_only) {
    if (!ctx.get('sandboxPolicy')) throw new Error('DSH read-only role requires native sandbox policy')
    ctx.on('agent/created', ({ agent }) => {
      // This is DSH's native session-mode write path; it preserves the user's
      // approval policy and applies equally to restored and child sessions.
      if (agent.ctx.get('sandboxPolicy').overrideOf(agent.session) !== 'read-only') {
        agent.session.append('sandbox/mode', {mode:'read-only'})
      }
    })
  }
  let sessionId, turnId
  ctx.on('agent/inbox/claimed', ({agent,message,turn}) => {
    // identity.json is written from the ACP create/resume response before the
    // prompt. Never infer the root from whichever child emits an event first.
    const identityPath=join(config.directory,'identity.json')
    if (turnId !== undefined || message.source?.kind !== 'user' || !existsSync(identityPath)) return
    const identity=JSON.parse(readFileSync(identityPath,'utf8')).identity
    if (agent.session.header.id !== identity) return
    sessionId=agent.session.header.id; turnId=turn
    const located=ctx.get('sessionPersistence')?.locate(agent.session.header)
    const info={sessionId,inputId:message.id,turn,transcriptPath:typeof located?.path==='string'?located.path:''}
    const path=join(config.directory,'native-source.json')
    writeFileSync(path+'.tmp',JSON.stringify(info),{mode:0o600});renameSync(path+'.tmp',path)
  })
  ctx.on('session/event',(session,event)=>{
    if(session.header.id!==sessionId) return
    if(event.type!=='compaction/summary' && event.data?.turn!==turnId) return
    if(!['assistant/message','assistant/attempt','turn/end','compaction/summary'].includes(event.type)) return
    appendFileSync(join(config.directory,'native-events.jsonl'),JSON.stringify({sessionId,event})+'\n',{mode:0o600})
  })
  ctx.provide('harnessNativeReady',true)
}

function invokeTool(config, name, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(config.executable, ['harness', 'native-tools', config.directory], { stdio: ['pipe', 'pipe', 'pipe'] })
    const output = [], errors = []
    let failed
    child.stdout.on('data', chunk => output.push(chunk))
    child.stderr.on('data', chunk => errors.push(chunk))
    child.on('error', error => { failed = error })
    child.on('close', code => {
      if (failed) { reject(failed); return }
      if (code !== 0) { reject(new Error(Buffer.concat(errors).toString('utf8'))); return }
      try {
        const reply = JSON.parse(Buffer.concat(output).toString('utf8'))
        if (reply.error || reply.result?.isError) throw new Error(reply.error?.message ?? reply.result?.content?.[0]?.text)
        resolve(JSON.parse(reply.result.content[0].text))
      } catch (error) { reject(error) }
    })
    child.stdin.end(JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }) + '\n')
  })
}
