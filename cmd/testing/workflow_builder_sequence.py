#!/usr/bin/env python3
"""Send queued user messages to a Workflow Builder session.

This is a repo-level Agent Builder harness. It talks to the running Agent API
instead of importing agent_go internals, so it stays outside agent_go/cmd/testing.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from typing import Any


TERMINAL_STATUSES = {"completed", "error", "stopped", "inactive"}


def http_json(method: str, url: str, payload: dict[str, Any] | None = None, headers: dict[str, str] | None = None) -> dict[str, Any]:
    data = None
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers=request_headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {url} failed with HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {url} failed: {exc.reason}") from exc


def load_messages(path: str) -> list[str]:
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    if isinstance(data, list):
        messages = data
    elif isinstance(data, dict) and isinstance(data.get("messages"), list):
        messages = data["messages"]
    else:
        raise ValueError("messages file must be a JSON array or an object with a messages array")
    cleaned = [str(msg).strip() for msg in messages if str(msg).strip()]
    if not cleaned:
        raise ValueError("messages file did not contain any non-empty messages")
    return cleaned


def wait_for_session(base_url: str, session_id: str, timeout: float, poll_interval: float) -> str:
    deadline = time.time() + timeout
    status = "unknown"
    while time.time() < deadline:
        encoded_session = urllib.parse.quote(session_id, safe="")
        resp = http_json("GET", f"{base_url}/api/sessions/{encoded_session}/status")
        status = str(resp.get("status", "unknown"))
        if status in TERMINAL_STATUSES:
            return status
        time.sleep(poll_interval)
    raise TimeoutError(f"session {session_id} did not finish within {timeout:.0f}s; last status={status}")


def build_request(args: argparse.Namespace, message: str) -> dict[str, Any]:
    execution_options: dict[str, Any] = {
        "run_mode": "use_same_run",
        "selected_run_folder": args.run_folder,
        "execution_strategy": "run_single_step",
        "workshop_mode": args.workshop_mode,
    }
    if args.group_name:
        execution_options["enabled_group_names"] = args.group_name

    return {
        "query": message,
        "agent_mode": "workflow_phase",
        "phase_id": "workflow-builder",
        "preset_query_id": args.preset_query_id or args.workspace_path,
        "selected_folder": args.workspace_path,
        "execution_options": execution_options,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Send queued messages to a Workflow Builder session.")
    parser.add_argument("--base-url", default="http://localhost:18743", help="Agent API base URL")
    parser.add_argument("--workspace-path", required=True, help="Workflow workspace path, for example Workflows/My Workflow")
    parser.add_argument("--messages-file", required=True, help="JSON array or {messages: [...]} file")
    parser.add_argument("--session-id", default="", help="Existing Builder session id to resume")
    parser.add_argument("--preset-query-id", default="", help="Preset id; defaults to workspace path")
    parser.add_argument("--group-name", action="append", default=[], help="Enabled variable group name; repeat for multiple groups")
    parser.add_argument("--run-folder", default="iteration-0", help="Run folder used by Builder execution tools")
    parser.add_argument("--workshop-mode", default="run", choices=["builder", "optimizer", "run", "reporting"])
    parser.add_argument("--timeout", type=float, default=1800, help="Seconds to wait for each message")
    parser.add_argument("--poll-interval", type=float, default=2, help="Seconds between status polls")
    parser.add_argument("--dry-run", action="store_true", help="Print requests without sending")
    args = parser.parse_args()

    base_url = args.base_url.rstrip("/")
    session_id = args.session_id or f"agent-builder-test-{uuid.uuid4().hex[:12]}"
    messages = load_messages(args.messages_file)

    print(f"session_id={session_id}")
    for index, message in enumerate(messages, start=1):
        payload = build_request(args, message)
        print(f"\n[{index}/{len(messages)}] {message}")

        if args.dry_run:
            print(json.dumps(payload, indent=2))
            continue

        response = http_json("POST", f"{base_url}/api/query", payload, {"X-Session-ID": session_id})
        print(f"query_id={response.get('query_id')} status={response.get('status')}")

        final_status = wait_for_session(base_url, session_id, args.timeout, args.poll_interval)
        print(f"final_status={final_status}")
        if final_status != "completed":
            return 1

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
