import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, realpathSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import * as nativePlugin from './runtime_tools_dsh.mjs'
import { apply } from './runtime_tools_dsh.mjs'

test('DSH native tool binds the role before ACP readiness', async () => {
  let tool, filter, guard, dispose
  const listeners = new Map()
  const calls = []
  const ctx = {
    tools: {
      register(value) { tool = value; calls.push('register'); return () => calls.push('unregister') },
      restrict() { throw new Error('global restrict is not supported by DSH') },
      schemas() { return [{name: 'formal_gates_operation'}, {name: 'Read'}, {name: 'Bash'}] },
      guard(value) { guard = value; calls.push('guard'); return () => calls.push('unguard') },
    },
    on(name, listener) { listeners.set(name, listener) },
    effect(value) { dispose = value() },
    provide(name, value) { assert.equal(name, 'formalGatesNativeReady'); assert.equal(value, true); calls.push('ready') },
  }
  apply(ctx, { executable: '/engine', root: '/work', requestDirectory: '/requests', sourceInfoPath: '/native-source.json', runId: 'run', actionId: 'action',
    deniedTools: ['Bash', 'subagent'], tool: { name: 'formal_gates_operation', description: 'Bound operation', inputSchema: { type: 'object' } } })
  assert.deepEqual(calls, ['register', 'guard', 'ready'])
  listeners.get('agent/created')({agent: {ctx: {tools: {restrict(value) { filter = value }}}}})
  assert.deepEqual(filter, { deny: ['Bash'] })
  assert.equal(guard({name: 'WebSearch'}), undefined)
  assert.equal(guard({name: 'Read'}), undefined)
  assert.equal(guard({name: 'formal_gates_operation'}), undefined)
  assert.equal(typeof guard({name: 'subagent'}), 'string')
  assert.equal(typeof guard({name: 'Bash'}), 'string')
  await assert.rejects(tool.execute({requestId: 'stable'}, {}), /native identity/)
  await assert.rejects(tool.execute({}, {agent: {session: {id: 'native'}}}), /stable requestId/)
  dispose()
  assert.deepEqual(calls.slice(-2), ['unguard', 'unregister'])
})

// Optional installed-SDK check. It creates no native model session and is not a
// real regular-scenario PASS. The package path selects the SDK already installed
// on the test host; no dependencies, credentials or profiles are copied.
test('DSH installed SDK applies role restrictions before prompt assembly', {
  skip: !process.env.FORMAL_GATES_DSH_SDK_PACKAGE,
}, async () => {
  const requireSDK = createRequire(realpathSync(process.env.FORMAL_GATES_DSH_SDK_PACKAGE))
  const requireTools = createRequire(requireSDK.resolve('@deepseek-ai/dsh-tools'))
  const { Context } = await import(pathToFileURL(requireSDK.resolve('@deepseek-ai/cordis')))
  const { ToolRuntime } = await import(pathToFileURL(requireSDK.resolve('@deepseek-ai/dsh-tools')))
  const { createScope } = await import(pathToFileURL(requireTools.resolve('@deepseek-ai/dsh-scope')))
  const directory = mkdtempSync(join(tmpdir(), 'formal-gates-dsh-sdk-'))
  const ctx = new Context()
  let scope
  try {
    ctx.provide('systemPrompt', { tools() { return () => {} } })
    const tools = ctx.plugin(ToolRuntime)
    await tools.inertia
    const bridge = ctx.plugin(nativePlugin, {
      executable: '/engine', root: directory, requestDirectory: directory,
      sourceInfoPath: join(directory, 'native-source.json'), runId: 'sdk-fixture', actionId: 'sdk-fixture',
      exposeOperation: false, deniedTools: [], disableTools: true,
      tool: { name: 'formal_gates_operation', description: 'SDK fixture only', inputSchema: { type: 'object' } },
    })
    await bridge.inertia
    const agent = { session: { id: 'native', header: { id: 'native' } } }
    scope = createScope(bridge.ctx, agent)
    agent.ctx = scope.ctx
    ctx.emit('agent/created', { agent })
    await scope.ctx.fiber.inertia
    // The SDK emits inbox/claimed synchronously before systemPrompt.assemble.
    ctx.emit('agent/inbox/claimed', { agent, message: { id: 'input-1' }, turn: 1 })
    assert.deepEqual(bridge.ctx.tools.schemas(agent), [])
    assert.equal(JSON.parse(readFileSync(join(directory, 'native-source.json'), 'utf8')).sessionId, 'native')
    ctx.emit('agent/inbox/claimed', { agent, message: { id: 'input-2' }, turn: 2 })
    assert.deepEqual(bridge.ctx.tools.schemas(agent), [])
    assert.equal(JSON.parse(readFileSync(join(directory, 'native-source.json'), 'utf8')).inputId, 'input-2')
  } finally {
    await scope?.dispose()
    await ctx.fiber.dispose()
    rmSync(directory, { recursive: true, force: true })
  }
})
