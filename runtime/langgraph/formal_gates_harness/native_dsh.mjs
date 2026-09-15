// DSH-local transport policy and native evidence, not a scheduler or new tool API.
import { appendFileSync, writeFileSync, renameSync, readFileSync, existsSync } from 'node:fs'
import { join, isAbsolute } from 'node:path'
export const name = 'formal-gates-harness-native'
export const inject = ['tools']
export function apply(ctx, config) {
  if (!isAbsolute(config.directory) || typeof config.read_only !== 'boolean') throw new Error('Invalid harness native configuration')
  if (config.read_only) {
    if (!ctx.get('sandboxPolicy')) throw new Error('DSH read-only role requires native sandbox policy')
    // Native filesystem/shell policy confines these tools. Arbitrary plugin
    // executors have no equivalent read-only contract in this transport.
    // The native in-process delegation tools inherit the session sandbox;
    // this same hook also confines every child. Native web reads remain usable.
    const allowed = new Set(['Read','read','read_file','Glob','glob','Grep','grep','Bash','bash','shell','Search','search','WebSearch','WebFetch','web_search','web_fetch','List','list','list_directory','read_image','job_output','job_list','subagent','subagent_fork','send_message','list_agents','list_subagent_models'])
    const unguard = ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'Read-only role requires a confined native tool')
    ctx.effect(() => () => unguard())
    ctx.on('agent/created', ({ agent }) => {
      // This is DSH's native session-mode write path; it preserves the user's
      // approval policy and applies equally to restored and child sessions.
      if (agent.ctx.get('sandboxPolicy').overrideOf(agent.session) !== 'read-only') {
        agent.session.append('sandbox/mode', {mode:'read-only'})
      }
      const tools = ctx.tools.schemas(agent).map(tool => tool.name)
      agent.ctx.tools.restrict({ deny: tools.filter(name => !allowed.has(name)) })
      writeFileSync(join(config.directory, 'tool-policy.json'), JSON.stringify({read_only:true, available:tools, allowed:tools.filter(name=>allowed.has(name))}), {mode:0o600})
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
