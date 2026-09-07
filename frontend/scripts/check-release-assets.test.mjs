import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { checkReleaseAssets } from './check-release-assets.mjs'

test('reject missing/empty preview bundle and accept complete release', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'release-assets-'))
  try {
    await assert.rejects(checkReleaseAssets(root), /index.html/)
    await writeFile(path.join(root, 'index.html'), '<html></html>')
    await assert.rejects(checkReleaseAssets(root), /report-preview.js/)
    await writeFile(path.join(root, 'report-preview.js'), '')
    await assert.rejects(checkReleaseAssets(root), /report-preview.js/)
    await writeFile(path.join(root, 'report-preview.js'), 'console.log("preview")')
    await checkReleaseAssets(root)
  } finally { await rm(root, { recursive: true, force: true }) }
})
