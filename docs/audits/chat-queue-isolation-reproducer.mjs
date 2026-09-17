// Run from the repository root: node docs/audits/chat-queue-isolation-reproducer.mjs
// Executes the current queue effect with controlled timers and submit callbacks.
// This is a narrow logic harness, not a mounted React/browser integration test.
import fs from 'node:fs'
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'
const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const ts = require('typescript')
const source = fs.readFileSync(new URL('../../frontend/src/components/ChatArea.tsx', import.meta.url), 'utf8')
const start = source.indexOf('    const queuedMessages = queuedTabMessages || []')
const end = source.indexOf('  }, [addToast, queuedTabId, queuedTabIsProcessing, queuedTabIsStreaming, queuedTabMessages])', start)
if (start < 0 || end <= start) {
  console.log('The vulnerable view-owned queue effect has been replaced. Run: npm --prefix frontend test -- src/components/ChatArea.queueOwnership.test.ts')
  process.exit(0)
}
const body = ts.transpileModule(source.slice(start, end), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None },
}).outputText

async function run(accepted) {
  const config = { queuedMessages: ['Private draft for tab A'], isQueueProcessing: false }
  const callbacks = []
  const deliveries = []
  let selectedTab = 'A'
  const store = {
    getTabConfig: () => config,
    setTabConfig: (_tab, patch) => Object.assign(config, patch),
  }
  const env = {
    queuedTabMessages: [...config.queuedMessages], queuedTabId: 'A',
    queuedTabIsProcessing: false, queuedTabIsStreaming: false,
    useChatStore: { getState: () => store }, window: {},
    AUTO_NOTIFICATION_PREFIX: '[AUTO]', isStaleQueuedAutoNotification: () => false,
    addToast: () => {}, logger: { error: () => {} },
    setTimeout: (fn) => { callbacks.push(fn); return callbacks.length },
    submitQueryWithQueryRef: { current: async (message, _execution, options) => {
      deliveries.push({ tab: options?.sourceTabId ?? selectedTab, message })
      return accepted
    } },
  }
  new Function(...Object.keys(env), body)(...Object.values(env))
  selectedTab = 'B'
  await callbacks.shift()()
  return { deliveries, config }
}

let failures = 0
for (const [name, check] of [
  ['queue retains tab A after selection changes to B', async () => {
    assert.equal((await run(true)).deliveries[0].tab, 'A')
  }],
  ['false acceptance restores the queued message', async () => {
    assert.deepEqual((await run(false)).config.queuedMessages, ['Private draft for tab A'])
  }],
]) {
  try { await check(); console.log(`PASS: ${name}`) }
  catch (error) { failures++; console.log(`FAIL: ${name}\n${error.message}`) }
}
process.exitCode = failures ? 1 : 0
