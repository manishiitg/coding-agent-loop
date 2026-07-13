import { gzipSync } from 'node:zlib'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const ENTRY_GZIP_BUDGET_BYTES = 1_000_000
const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distRoot = path.join(frontendRoot, 'dist')
const indexHtml = await readFile(path.join(distRoot, 'index.html'), 'utf8')
const entryMatch = indexHtml.match(/<script[^>]+type="module"[^>]+src="([^"]+\.js)"/)

if (!entryMatch) {
  throw new Error('Unable to find the production entry script in dist/index.html')
}

const entryPath = path.join(distRoot, entryMatch[1].replace(/^\//, ''))
const entrySource = await readFile(entryPath)
const gzipBytes = gzipSync(entrySource, { level: 9 }).byteLength
const formattedSize = (gzipBytes / 1000).toFixed(2)

if (gzipBytes > ENTRY_GZIP_BUDGET_BYTES) {
  throw new Error(
    `Production entry bundle is ${formattedSize} kB gzip; budget is ${ENTRY_GZIP_BUDGET_BYTES / 1000} kB`,
  )
}

console.log(`Bundle budget passed: ${formattedSize} kB gzip / ${ENTRY_GZIP_BUDGET_BYTES / 1000} kB`)
