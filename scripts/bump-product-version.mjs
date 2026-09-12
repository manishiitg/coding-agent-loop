import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync, lstatSync } from 'node:fs'
import path from 'node:path'

const versionPath = 'frontend/src/version.ts'
const git = (...args) => execFileSync('git', args, { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] })
const declaration = /^(export\s+const\s+APP_VERSION\s*=\s*)(['"])(\d+)\.(\d+)\.(\d+)\2/m
function parse(content) {
  const match = content.match(declaration)
  if (!match) throw new Error(`${versionPath} must export a numeric major.minor.patch APP_VERSION`)
  return match.slice(3, 6).map(BigInt)
}
function compare(a, b) {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] > b[i] ? 1 : -1
  }
  return 0
}
function replaceVersion(content, version) {
  return content.replace(declaration, (_all, prefix, quote) => `${prefix}${quote}${version.join('.')}${quote}`)
}

try {
  // Do not turn an otherwise empty `git commit` into a version-only commit.
  if (!git('diff', '--cached', '--name-only').trim()) process.exit(0)
  const root = git('rev-parse', '--show-toplevel').trim()
  const entry = git('ls-files', '--stage', '--', versionPath).trim()
  const match = entry.match(/^(100644|100755) [a-f0-9]+ 0\t/)
  if (!match) throw new Error(`${versionPath} must be a staged, regular file without conflicts`)
  const staged = git('show', `:${versionPath}`)
  const stagedVersion = parse(staged)
  let previous
  try { previous = git('show', `HEAD:${versionPath}`) } catch { /* First introduction of the version file. */ }
  const base = previous === undefined ? stagedVersion : parse(previous)
  // A retry already has HEAD+1 staged; a deliberate major/minor bump also wins.
  const next = previous === undefined || compare(stagedVersion, base) > 0 ? stagedVersion : [base[0], base[1], base[2] + 1n]
  const stagedNext = replaceVersion(staged, next)
  const absolute = path.join(root, versionPath)
  if (!lstatSync(absolute).isFile()) throw new Error(`${versionPath} must be a regular working-tree file`)
  const working = readFileSync(absolute, 'utf8')
  const workingVersion = parse(working)

  // Update only this staged blob. `git add` would also stage unrelated local
  // edits when version.ts is partially staged. Respect Git's temporary index
  // for `git commit -a` and `git commit --only` as well.
  if (stagedNext !== staged) {
    const blob = execFileSync('git', ['hash-object', '-w', '--stdin'], { input: stagedNext, encoding: 'utf8' }).trim()
    git('update-index', '--cacheinfo', match[1], blob, versionPath)
  }
  if (compare(workingVersion, stagedVersion) === 0) {
    const workingNext = replaceVersion(working, next)
    if (workingNext !== working) writeFileSync(absolute, workingNext)
  }
  // Path-only commits use a temporary index, then restore the user's real
  // index. Record exact blobs so post-commit can reconcile only our version
  // change without touching any other staged content.
  const committedBlob = git('rev-parse', `:${versionPath}`).trim()
  const oldBlob = execFileSync('git', ['hash-object', '--stdin'], { input: staged, encoding: 'utf8' }).trim()
  const stateFile = git('rev-parse', '--git-path', 'product-version-hook.json').trim()
  writeFileSync(stateFile, JSON.stringify({ oldBlob, committedBlob, mode: match[1] }))
  console.log(`Product version: ${next.join('.')} (staged)`)
} catch (error) {
  console.error(`Product version bump failed: ${error.message}`)
  process.exitCode = 1
}
