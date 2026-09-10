#!/usr/bin/env python3
"""Build dependency-free distribution archives; never include caches or test data."""
import io
import json
from pathlib import Path
import sys
import tarfile
import zipfile

root = Path(__file__).resolve().parents[1]
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
node = root / 'packages/playwright'
with tarfile.open(out / 'agentworks-playwright.tgz', 'w:gz') as archive:
    for name in ['package.json', 'index.cjs', 'index.mjs', 'index.d.ts', 'README.md']:
        data = (node / name).read_bytes()
        entry = tarfile.TarInfo('package/' + name)
        entry.size, entry.mode, entry.mtime = len(data), 0o644, 0
        archive.addfile(entry, io.BytesIO(data))
python = root / 'packages/playwright-python'
with zipfile.ZipFile(out / 'agentworks-playwright-python.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for file in [python / 'pyproject.toml', python / 'README.md', *sorted((python / 'agentworks_playwright').glob('*.py'))]:
        archive.writestr('agentworks-playwright-python/' + str(file.relative_to(python)), file.read_bytes())
print(json.dumps({'packages': sorted(file.name for file in out.iterdir())}))
