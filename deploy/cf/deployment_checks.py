"""Fail-closed Confida OAuth deployment checks; never print environment contents."""
import argparse
import os
from pathlib import Path
import platform
import pwd
import re
import subprocess
import sys

PUBLIC_URL = "https://confida.agentworkshq.com"
ENV_FILE = Path("/srv/confida/.env")


def check_environment_file(path=ENV_FILE):
    values = []
    for line in path.read_text().splitlines():
        match = re.match(r"^\s*PUBLIC_URL\s*=(.*)$", line)
        if match:
            value = match.group(1).strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            values.append(value)
    if values != [PUBLIC_URL]:
        raise ValueError(f"{path} must contain exactly one PUBLIC_URL={PUBLIC_URL}")


def check_process_environment(raw):
    values = [entry for entry in raw.split(b"\0") if entry.startswith(b"PUBLIC_URL=")]
    if values != [f"PUBLIC_URL={PUBLIC_URL}".encode()]:
        raise ValueError("running confida-agent has missing, duplicate, or incorrect PUBLIC_URL")


def systemctl_value(property_name):
    return subprocess.check_output(
        ["systemctl", "--user", "show", "confida-agent", f"--property={property_name}", "--value"],
        text=True, timeout=10,
    ).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["preflight", "running"])
    args = parser.parse_args()
    if platform.system() != "Linux" or pwd.getpwuid(os.getuid()).pw_name != "confida":
        raise ValueError("deployment checks must run as the Confida account on Linux")
    check_environment_file()
    if str(ENV_FILE) not in systemctl_value("EnvironmentFiles").split():
        raise ValueError("confida-agent must load /srv/confida/.env through EnvironmentFile")
    if args.phase == "running":
        pid = int(systemctl_value("MainPID"))
        if pid <= 0:
            raise ValueError("confida-agent has no running main process")
        check_process_environment(Path(f"/proc/{pid}/environ").read_bytes())
    print(f"PASS {args.phase}: Confida PUBLIC_URL and OAuth callback configuration")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
