import { execFileSync } from 'node:child_process'
import { readFileSync, unlinkSync, existsSync } from 'node:fs'

const git = (...args) => execFileSync('git', args, { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim()
const versionPath = 'frontend/src/version.ts'
try {
  const stateFile = git('rev-parse', '--git-path', 'product-version-hook.json')
  if (existsSync(stateFile)) {
    const state = JSON.parse(readFileSync(stateFile, 'utf8'))
    const committed = git('rev-parse', `HEAD:${versionPath}`)
    if (committed === state.committedBlob) {
      // Never replace a different staged edit. Only remove the stale version
      // blob Git restored after a path-only commit used its temporary index.
      if (git('rev-parse', `:${versionPath}`) === state.oldBlob) {
        git('update-index', '--cacheinfo', state.mode, committed, versionPath)
      }
      unlinkSync(stateFile)
    }
  }
} catch (error) {
  console.error(`Could not synchronize product version index: ${error.message}`)
  process.exitCode = 1
}
