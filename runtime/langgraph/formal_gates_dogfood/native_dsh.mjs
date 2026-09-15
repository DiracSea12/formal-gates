// DSH-local transport policy and native evidence, not a scheduler or new tool API.
import { appendFileSync, writeFileSync, renameSync } from 'node:fs'
import { join, isAbsolute } from 'node:path'
export const name = 'formal-gates-dogfood-native'
export const inject = ['tools']
export function apply(ctx, config) {
  if (!isAbsolute(config.directory) || !['read-only', 'workspace-write'].includes(config.access)) throw new Error('Invalid dogfood native configuration')
  const readable = ['Read','read','read_file','Glob','glob','Grep','grep','Bash','bash','shell','Search','search','List','list','list_directory','read_image','job_output','job_list']
  const writable = ['Write','write','write_file','Edit','edit','edit_file','apply_patch']
  const allowed = new Set([...readable, ...(config.access === 'workspace-write' ? writable : [])])
  const unguard = ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'Current role does not grant this native tool')
  ctx.effect(() => () => unguard())
  ctx.on('agent/created', ({ agent }) => {
    const tools = ctx.tools.schemas(agent).map(tool => tool.name)
    agent.ctx.tools.restrict({ deny: tools.filter(name => !allowed.has(name)) })
    writeFileSync(join(config.directory, 'tool-policy.json'), JSON.stringify({access:config.access, available:tools, allowed:tools.filter(name=>allowed.has(name))}), {mode:0o600})
  })
  let sessionId, turnId
  ctx.on('agent/inbox/claimed', ({agent,message,turn}) => {
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
  ctx.provide('dogfoodNativeReady',true)
}
