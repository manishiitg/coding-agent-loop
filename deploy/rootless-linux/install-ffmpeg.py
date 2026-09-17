#!/usr/bin/env python3
"""Install the pinned rootless ffmpeg used by voice-note transcription."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import tempfile
import urllib.request
import zipfile


WHEEL_URL = "https://files.pythonhosted.org/packages/a0/2d/43c8522a2038e9d0e7dbdf3a61195ecc31ca576fb1527a528c877e87d973/imageio_ffmpeg-0.6.0-py3-none-manylinux2014_x86_64.whl"
WHEEL_SHA256 = "c7e46fcec401dd990405049d2e2f475e2b397779df2519b544b8aab515195282"
WHEEL_MEMBER = "imageio_ffmpeg/binaries/ffmpeg-linux-x86_64-v7.0.2"


def main() -> None:
    remote_app = Path(os.environ["REMOTE_APP"])
    destination = remote_app / "tools" / "bin" / "ffmpeg"
    destination.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(dir=remote_app / "tools") as stage_name:
        stage = Path(stage_name)
        wheel = stage / "imageio_ffmpeg.whl"
        with urllib.request.urlopen(WHEEL_URL, timeout=120) as response:
            wheel.write_bytes(response.read())
        digest = hashlib.sha256(wheel.read_bytes()).hexdigest()
        if digest != WHEEL_SHA256:
            raise RuntimeError(f"ffmpeg wheel checksum mismatch: {digest}")

        with zipfile.ZipFile(wheel) as archive:
            binary = archive.read(WHEEL_MEMBER)
        candidate = stage / "ffmpeg"
        candidate.write_bytes(binary)
        candidate.chmod(0o755)
        os.replace(candidate, destination)

    print(f"installed {destination}")


if __name__ == "__main__":
    main()
