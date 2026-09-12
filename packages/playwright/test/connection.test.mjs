import { test } from 'node:test'
import assert from 'node:assert/strict'
import { attachLiveBrowser } from '../index.mjs'

test('rejects credentials in a URL before opening a browser connection', async () => {
  await assert.rejects(attachLiveBrowser({}, { apiURL: 'https://user:secret@example.test/s/run', token: 'test' }), /without embedded credentials/)
})
test('requires a run binding rather than a caller-selected workspace', async () => {
  const old = process.env.MCP_SESSION_ID
  delete process.env.MCP_SESSION_ID
  try { await assert.rejects(attachLiveBrowser({}, { apiURL: 'http://localhost:12345', token: 'test' }), /session-scoped/) }
  finally { if (old !== undefined) process.env.MCP_SESSION_ID = old }
})

test('unattended explicit attach never touches the browser or opens a connection', async () => {
  const previous = process.env.AGENTWORKS_EXECUTION_CONTEXT
  try {
    for (const origin of ['schedule', 'webhook', 'bot', 'pulse', 'notification']) {
      process.env.AGENTWORKS_EXECUTION_CONTEXT = origin
      const browser = new Proxy({}, { get() { throw new Error('browser touched') } })
      const live = await attachLiveBrowser(browser, { apiURL: 'invalid URL', token: 'invalid' })
      assert.equal(live.sessionID, '')
      await live.stop()
      await live.stop()
    }
    process.env.AGENTWORKS_EXECUTION_CONTEXT = 'builder'
    await assert.rejects(attachLiveBrowser({}, { apiURL: 'https://user:secret@example.test/s/run', token: 'test' }), /without embedded credentials/)
  } finally {
    if (previous === undefined) delete process.env.AGENTWORKS_EXECUTION_CONTEXT
    else process.env.AGENTWORKS_EXECUTION_CONTEXT = previous
  }
})
