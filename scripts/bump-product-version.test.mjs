import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, copyFileSync, chmodSync, rmSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const source = path.dirname(fileURLToPath(import.meta.url))
const versionFile = 'frontend/src/version.ts'
const versionSource = version => `export const APP_VERSION = '${version}'\n`
function fixture(t) {
  const root = mkdtempSync(path.join(tmpdir(), 'product-version-hook-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const env = { ...process.env }
  for (const key of Object.keys(env)) if (key.startsWith('GIT_')) delete env[key]
  const bin = path.join(root, 'test-bin')
  mkdirSync(bin)
  writeFileSync(path.join(bin, 'gitleaks'), '#!/bin/sh\nexit 0\n', { mode: 0o755 })
  env.PATH = `${bin}:${path.dirname(process.execPath)}:${env.PATH}`
  const repo = path.join(root, 'repo'); mkdirSync(repo)
  function git(args, { cwd = repo, extraEnv = {}, fail = false } = {}) {
    const result = spawnSync('git', args, { cwd, env: { ...env, ...extraEnv }, encoding: 'utf8' })
    if (!fail) assert.equal(result.status, 0, `${args.join(' ')}\n${result.stdout}\n${result.stderr}`)
    return result
  }
  git(['init', '-q'])
  git(['config', 'user.email', 'test@example.invalid'])
  git(['config', 'user.name', 'Hook test'])
  git(['config', 'commit.gpgsign', 'false'])
  mkdirSync(path.join(repo, 'frontend/src'), { recursive: true })
  mkdirSync(path.join(repo, 'scripts'))
  writeFileSync(path.join(repo, versionFile), versionSource('0.50.2'))
  writeFileSync(path.join(repo, 'note.txt'), 'initial\n')
  writeFileSync(path.join(repo, 'other.txt'), 'initial\n')
  for (const script of ['bump-product-version.mjs', 'sync-product-version-index.mjs']) {
    copyFileSync(path.join(source, script), path.join(repo, 'scripts', script))
  }
  writeFileSync(path.join(repo, 'scripts/check-schema-drift.sh'), '#!/bin/sh\nexit "${TEST_SCHEMA_EXIT:-0}"\n', { mode: 0o755 })
  git(['add', '.']); git(['-c', 'core.hooksPath=/dev/null', 'commit', '-qm', 'seed'])
  const hooks = path.join(root, 'hooks'); mkdirSync(hooks)
  for (const hook of ['pre-commit', 'post-commit']) {
    copyFileSync(path.join(source, 'hooks', hook), path.join(hooks, hook))
    chmodSync(path.join(hooks, hook), 0o755)
  }
  git(['config', 'core.hooksPath', hooks])
  return { root, repo, env, hooks, git,
    stageNote(text = 'changed\n', cwd = repo) { writeFileSync(path.join(cwd, 'note.txt'), text); git(['add', 'note.txt'], { cwd }) },
    version(ref = 'HEAD', cwd = repo) { return git(['show', `${ref}:${versionFile}`], { cwd }).stdout.match(/APP_VERSION = '([^']+)'/)[1] },
  }
}

test('real commits increment patch and stage only the intended changes', t => {
  const f = fixture(t)
  writeFileSync(path.join(f.repo, 'other.txt'), 'unstaged\n')
  f.stageNote(); f.git(['commit', '-qm', 'first'])
  assert.equal(f.version(), '0.50.3')
  assert.equal(f.git(['show', 'HEAD:other.txt']).stdout, 'initial\n')
  f.stageNote('second\n'); f.git(['commit', '-qm', 'second'])
  assert.equal(f.version(), '0.50.4')
})

test('partially staged version edits keep unstaged content out of the commit', t => {
  const f = fixture(t)
  const target = path.join(f.repo, versionFile)
  writeFileSync(target, `// staged comment\n${versionSource('0.50.2')}`)
  f.git(['add', versionFile])
  writeFileSync(target, readFileSync(target, 'utf8') + '// unstaged comment\n')
  f.git(['commit', '-qm', 'partial'])
  assert.equal(f.version(), '0.50.3')
  assert.doesNotMatch(f.git(['show', `HEAD:${versionFile}`]).stdout, /unstaged comment/)
  assert.match(readFileSync(target, 'utf8'), /0.50.3[\s\S]*unstaged comment/)
})

test('a failed commit retry does not increment twice', t => {
  const f = fixture(t)
  writeFileSync(path.join(f.hooks, 'commit-msg'), '#!/bin/sh\nexit 1\n', { mode: 0o755 })
  f.stageNote()
  assert.notEqual(f.git(['commit', '-qm', 'blocked'], { fail: true }).status, 0)
  assert.equal(f.version(), '0.50.2')
  assert.equal(f.version(''), '0.50.3')
  rmSync(path.join(f.hooks, 'commit-msg'))
  f.git(['commit', '-qm', 'retry'])
  assert.equal(f.version(), '0.50.3')
})

test('manual higher releases are retained and staged downgrades are prevented', t => {
  const f = fixture(t)
  writeFileSync(path.join(f.repo, versionFile), versionSource('0.51.0'))
  f.git(['add', versionFile]); f.git(['commit', '-qm', 'minor release'])
  assert.equal(f.version(), '0.51.0')
  writeFileSync(path.join(f.repo, versionFile), versionSource('0.49.0'))
  f.git(['add', versionFile]); f.git(['commit', '-qm', 'stale branch version'])
  assert.equal(f.version(), '0.51.1')
})

test('existing validation failures happen before the version mutation', t => {
  const f = fixture(t); f.stageNote()
  assert.notEqual(f.git(['commit', '-qm', 'blocked'], { extraEnv: { TEST_SCHEMA_EXIT: '1' }, fail: true }).status, 0)
  assert.equal(f.version(''), '0.50.2')
  assert.match(readFileSync(path.join(f.repo, versionFile), 'utf8'), /0.50.2/)
})

test('an empty commit attempt does not manufacture a version-only commit', t => {
  const f = fixture(t)
  assert.notEqual(f.git(['commit', '-qm', 'nothing'], { fail: true }).status, 0)
  assert.equal(f.version(), '0.50.2')
})

test('linked worktrees bump their own index and working tree', t => {
  const f = fixture(t); const checkout = path.join(f.root, 'linked')
  f.git(['worktree', 'add', '--detach', checkout, 'HEAD'])
  f.stageNote('worktree change\n', checkout)
  f.git(['commit', '-qm', 'worktree'], { cwd: checkout })
  assert.equal(f.version('HEAD', checkout), '0.50.3')
  assert.equal(f.version(), '0.50.2')
})

test('path-only commits include the bump without including other staged changes', t => {
  const f = fixture(t)
  writeFileSync(path.join(f.repo, 'other.txt'), 'staged for later\n'); f.git(['add', 'other.txt'])
  writeFileSync(path.join(f.repo, 'note.txt'), 'selected\n')
  f.git(['commit', '--only', 'note.txt', '-qm', 'selected path'])
  assert.equal(f.version(), '0.50.3')
  assert.equal(f.git(['show', 'HEAD:other.txt']).stdout, 'initial\n')
  assert.equal(f.git(['diff', '--cached', '--', versionFile]).stdout, '')
})


test('unstaged manual version edits remain untouched', t => {
  const f = fixture(t)
  writeFileSync(path.join(f.repo, versionFile), versionSource('0.99.0'))
  f.stageNote(); f.git(['commit', '-qm', 'other work'])
  assert.equal(f.version(), '0.50.3')
  assert.equal(readFileSync(path.join(f.repo, versionFile), 'utf8'), versionSource('0.99.0'))
})
