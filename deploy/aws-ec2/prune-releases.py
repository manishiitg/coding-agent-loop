#!/usr/bin/env python3
"""Compatibility entry point for the shared Linux release cleanup command."""
from pathlib import Path
import runpy

if __name__ == '__main__':
    runpy.run_path(str(Path(__file__).resolve().parents[1] / 'common/prune-releases.py'), run_name='__main__')
