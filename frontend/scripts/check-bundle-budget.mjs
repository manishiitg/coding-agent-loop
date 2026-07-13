import { gzipSync } from 'node:zlib'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const EAGER_GZIP_WARNING_BYTES = 950_000
const EAGER_GZIP_BUDGET_BYTES = 1_000_000
const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distRoot = path.join(frontendRoot, 'dist')
const indexHtml = await readFile(path.join(distRoot, 'index.html'), 'utf8')

const parseAttributes = (tag) => {
  const attributes = new Map()
  const source = tag.replace(/^<\/?[a-z][^\s>]*/i, '').replace(/\/?\s*>$/, '')
  const attributePattern = /([^\s=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g
  for (const match of source.matchAll(attributePattern)) {
    attributes.set(match[1].toLowerCase(), match[2] ?? match[3] ?? match[4] ?? '')
  }
  return attributes
}

const eagerScripts = new Set()
for (const match of indexHtml.matchAll(/<(script|link)\b[^>]*>/gi)) {
  const tagName = match[1].toLowerCase()
  const attributes = parseAttributes(match[0])
  if (tagName === 'script' && attributes.get('type') === 'module') {
    const src = attributes.get('src')
    if (src?.endsWith('.js')) eagerScripts.add(src)
  }
  if (tagName === 'link' && (attributes.get('rel') || '').split(/\s+/).includes('modulepreload')) {
    const href = attributes.get('href')
    if (href?.endsWith('.js')) eagerScripts.add(href)
  }
}

if (eagerScripts.size === 0) {
  throw new Error('Unable to find the production entry script in dist/index.html')
}

let gzipBytes = 0
for (const asset of eagerScripts) {
  const source = await readFile(path.join(distRoot, asset.replace(/^\//, '')))
  gzipBytes += gzipSync(source, { level: 9 }).byteLength
}
const formattedSize = (gzipBytes / 1000).toFixed(2)

if (gzipBytes > EAGER_GZIP_BUDGET_BYTES) {
  throw new Error(
    `Production eager JavaScript is ${formattedSize} kB gzip; budget is ${EAGER_GZIP_BUDGET_BYTES / 1000} kB`,
  )
}

if (gzipBytes > EAGER_GZIP_WARNING_BYTES) {
  console.warn(
    `Bundle budget warning: ${formattedSize} kB gzip exceeds the ${EAGER_GZIP_WARNING_BYTES / 1000} kB warning threshold`,
  )
}

console.log(
  `Bundle budget passed: ${formattedSize} kB gzip across ${eagerScripts.size} eager script(s) / ${EAGER_GZIP_BUDGET_BYTES / 1000} kB`,
)
