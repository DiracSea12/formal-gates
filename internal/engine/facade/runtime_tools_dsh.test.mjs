import { test } from 'node:test'
import assert from 'node:assert/strict'
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
    allowedTools: ['Read'], tool: { name: 'formal_gates_operation', description: 'Bound operation', inputSchema: { type: 'object' } } })
  assert.deepEqual(calls, ['register', 'guard', 'ready'])
  listeners.get('agent/created')({agent: {ctx: {tools: {restrict(value) { filter = value }}}}})
  assert.deepEqual(filter.allow, ['formal_gates_operation', 'Read'])
  assert.equal(guard({name: 'Read'}), undefined)
  assert.equal(guard({name: 'formal_gates_operation'}), undefined)
  assert.equal(typeof guard({name: 'subagent'}), 'string')
  assert.equal(typeof guard({name: 'Bash'}), 'string')
  await assert.rejects(tool.execute({requestId: 'stable'}, {}), /native identity/)
  await assert.rejects(tool.execute({}, {agent: {session: {id: 'native'}}}), /stable requestId/)
  dispose()
  assert.deepEqual(calls.slice(-2), ['unguard', 'unregister'])
})
