#!/usr/bin/env python3
"""Remove inactive deployment copies; dry-run unless --apply is supplied."""
import argparse
import fcntl
import os
from pathlib import Path
import re
import shutil

RELEASE_NAME = re.compile(r"[0-9a-f]{7,40}-[0-9]{14}\Z")


def active_releases(releases, proc=Path('/proc')):
    referenced = set()
    pattern = re.compile(re.escape(str(releases)) + r'/([0-9a-f]{7,40}-[0-9]{14})(?:/|\s|\x00|$)')
    for process in proc.iterdir():
        if not process.name.isdigit():
            continue
        for name in ('exe', 'cwd', 'cmdline', 'maps'):
            try:
                value = os.readlink(process / name) if name in ('exe', 'cwd') else (process / name).read_text(errors='replace')
            except (OSError, UnicodeError):
                continue  # Processes can exit during the scan; other users may be inaccessible.
            referenced.update(pattern.findall(value))
            # A process may use an alias such as /var -> /private/var or an
            # app symlink. Compare canonical absolute paths as well.
            for path in re.findall(r'''/[^\s\x00"']+''', value):
                referenced.update(pattern.findall(os.path.realpath(path)))
    return referenced


def prune(app, apply=False, proc=Path('/proc')):
    app = app.resolve(strict=True)
    releases = (app / 'releases').resolve(strict=True)
    current = (app / 'current').resolve(strict=True)
    if current.parent != releases or not current.is_dir():
        raise RuntimeError('current must point to a directory directly inside releases')
    removed = []
    with (app / '.release-cleanup.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        protected = active_releases(releases, proc) | {current.name}
        for release in sorted(releases.iterdir()):
            if release.is_symlink() or not release.is_dir() or not RELEASE_NAME.fullmatch(release.name):
                continue
            if release.name in protected or (release / '.deploying').exists():
                print('keep', release.name)
                continue
            # Recheck the symlink before removal in case another deployment swapped it.
            if (app / 'current').resolve(strict=True) == release:
                continue
            print('remove' if apply else 'would remove', release.name)
            if apply:
                shutil.rmtree(release)
            removed.append(release.name)
    return removed


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('app', type=Path)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    print('release copies removed:', len(prune(args.app, args.apply)))
