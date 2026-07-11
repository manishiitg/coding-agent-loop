#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..');
const docsRoot = path.join(repoRoot, 'docs');
const markdownFiles = [];

function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const fullPath = path.join(directory, entry.name);
    if (entry.isDirectory()) walk(fullPath);
    if (entry.isFile() && entry.name.endsWith('.md')) markdownFiles.push(fullPath);
  }
}

function lineNumber(content, index) {
  return content.slice(0, index).split('\n').length;
}

walk(docsRoot);

const failures = [];
let linksChecked = 0;
const markdownLink = /!?\[[^\]]*\]\(([^)]+)\)/g;

for (const file of markdownFiles) {
  const content = fs.readFileSync(file, 'utf8');
  let match;

  if (!file.endsWith('.local.md') && /(?:file:\/\/\/)?\/Users\/mipl\//.test(content)) {
    failures.push({ file, line: 1, target: '/Users/mipl/...', reason: 'machine-local path in public documentation' });
  }

  while ((match = markdownLink.exec(content)) !== null) {
    let target = match[1].trim();
    const line = lineNumber(content, match.index);

    if (target.startsWith('<') && target.endsWith('>')) target = target.slice(1, -1);
    target = target.split(/\s+["']/)[0];
    linksChecked += 1;

    if (/^(https?:|mailto:|#)/.test(target)) continue;
    if (/^file:\/\/\//.test(target) || target.startsWith('/Users/')) {
      failures.push({ file, line, target, reason: 'machine-local path' });
      continue;
    }

    const withoutFragment = target.split('#')[0].split('?')[0];
    if (!withoutFragment) continue;

    let decodedTarget = withoutFragment;
    try {
      decodedTarget = decodeURIComponent(withoutFragment);
    } catch {
      failures.push({ file, line, target, reason: 'invalid URL encoding' });
      continue;
    }

    const resolved = path.resolve(path.dirname(file), decodedTarget);
    if (!fs.existsSync(resolved)) {
      failures.push({ file, line, target, reason: 'target does not exist' });
    }
  }
}

if (failures.length > 0) {
  console.error(`Documentation link check failed: ${failures.length} invalid link(s).`);
  for (const failure of failures) {
    console.error(`${path.relative(repoRoot, failure.file)}:${failure.line} ${failure.reason}: ${failure.target}`);
  }
  process.exit(1);
}

console.log(`Documentation link check passed: ${linksChecked} links across ${markdownFiles.length} files.`);
