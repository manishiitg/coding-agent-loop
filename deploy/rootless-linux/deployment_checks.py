"""Fail-closed rootless-Linux deployment checks; never print environment contents.

Generalized from deploy/cf/deployment_checks.py (confida). Product-specific
values come from PRODUCT and EXPECTED_PUBLIC_URL in the environment, both
already exported by build-and-activate.sh before this runs.
"""
import argparse
import os
from pathlib import Path
import platform
import pwd
import re
import subprocess
import sys

PRODUCT = os.environ.get("PRODUCT", "")
EXPECTED_PUBLIC_URL = os.environ.get("EXPECTED_PUBLIC_URL", "")
ENV_FILE = Path(f"/srv/{PRODUCT}/.env")
UNIT = f"{PRODUCT}-agent"


def check_environment_file(path=ENV_FILE):
    if not EXPECTED_PUBLIC_URL:
        return
    values = []
    for line in path.read_text().splitlines():
        match = re.match(r"^\s*PUBLIC_URL\s*=(.*)$", line)
        if match:
            value = match.group(1).strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            values.append(value)
    if values != [EXPECTED_PUBLIC_URL]:
        raise ValueError(f"{path} must contain exactly one PUBLIC_URL={EXPECTED_PUBLIC_URL}")


def check_process_environment(raw):
    if not EXPECTED_PUBLIC_URL:
        return
    values = [entry for entry in raw.split(b"\0") if entry.startswith(b"PUBLIC_URL=")]
    if values != [f"PUBLIC_URL={EXPECTED_PUBLIC_URL}".encode()]:
        raise ValueError(f"running {UNIT} has missing, duplicate, or incorrect PUBLIC_URL")


def systemctl_value(property_name):
    return subprocess.check_output(
        ["systemctl", "--user", "show", UNIT, f"--property={property_name}", "--value"],
        text=True, timeout=10,
    ).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["preflight", "running"])
    args = parser.parse_args()
    if not PRODUCT:
        raise ValueError("PRODUCT must be set in the environment before running this check")
    if platform.system() != "Linux" or pwd.getpwuid(os.getuid()).pw_name != PRODUCT:
        raise ValueError(f"deployment checks must run as the {PRODUCT} account on Linux")
    check_environment_file()
    if EXPECTED_PUBLIC_URL and str(ENV_FILE) not in systemctl_value("EnvironmentFiles").split():
        raise ValueError(f"{UNIT} must load {ENV_FILE} through EnvironmentFile")
    if args.phase == "running":
        pid = int(systemctl_value("MainPID"))
        if pid <= 0:
            raise ValueError(f"{UNIT} has no running main process")
        check_process_environment(Path(f"/proc/{pid}/environ").read_bytes())
    print(f"PASS {args.phase}: {PRODUCT} deployment configuration")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
