import { stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Run against the packaged directory, including on the target host before
// switching releases. Merely finding the bundle in the source tree is insufficient.
export async function checkReleaseAssets(root) {
  for (const name of ['index.html', 'report-preview.js']) {
    const asset = path.join(root, name)
    let info
    try { info = await stat(asset) } catch { /* diagnosed below */ }
    if (!info?.isFile() || info.size === 0) {
      throw new Error(`Release rejected: missing or empty ${asset}. Run npm run build in frontend/ and package the entire dist directory. The agent STATIC_DIR must point to this directory.`)
    }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = path.resolve(process.argv[2] || 'dist')
  try {
    await checkReleaseAssets(root)
    console.log(`Release assets verified: ${root}`)
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
