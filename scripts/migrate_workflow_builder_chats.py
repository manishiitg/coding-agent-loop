#!/usr/bin/env python3
"""Partition legacy Workflow Builder chats by owner.

Dry-run is the default. Use --apply after reviewing the plan. Ambiguous chats
are moved to conversation/system; provide --owner-map to repair known owners.

Owner map format:
  {"<session-id>": {"user_id": "<id>", "username": "<display name>"}}
The value may also be a user-id string when no display name is available.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile
from pathlib import Path
from typing import Any

SAFE_USER_ID = re.compile(r"^[A-Za-z0-9_-]{1,128}$")
SESSION_FILE = re.compile(r"^session-(.+)-conversation\.json$")


def load_owner_map(path: Path | None) -> dict[str, dict[str, str]]:
    if path is None:
        return {}
    raw = json.loads(path.read_text(encoding="utf-8"))
    result: dict[str, dict[str, str]] = {}
    for session_id, value in raw.items():
        if isinstance(value, str):
            result[str(session_id)] = {"user_id": value, "username": ""}
        elif isinstance(value, dict):
            result[str(session_id)] = {
                "user_id": str(value.get("user_id", "")),
                "username": str(value.get("username", "")),
            }
        else:
            raise ValueError(f"invalid owner map value for {session_id!r}")
    return result


def atomic_write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(value, handle, indent=2, ensure_ascii=False)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_name, path)
    finally:
        if os.path.exists(temp_name):
            os.unlink(temp_name)


def index_owner(index: dict[str, Any], relative_path: str, session_id: str) -> tuple[str, str]:
    entries = index.get("entries", {}) if isinstance(index, dict) else {}
    candidates = []
    direct = entries.get(relative_path)
    if isinstance(direct, dict):
        candidates.append(direct)
    for entry in entries.values():
        if not isinstance(entry, dict):
            continue
        session = entry.get("session", {})
        if isinstance(session, dict) and str(session.get("session_id", "")) == session_id:
            candidates.append(entry)
    for entry in candidates:
        session = entry.get("session", {})
        if not isinstance(session, dict):
            continue
        user_id = str(session.get("user_id", "")).strip()
        if user_id and user_id != "default":
            return user_id, str(session.get("username", "")).strip()
    return "", ""


def migrate(workspace_root: Path, owner_map: dict[str, dict[str, str]], apply: bool) -> tuple[int, int]:
    planned = changed = 0
    workflow_root = workspace_root / "Workflow"
    if not workflow_root.is_dir():
        raise FileNotFoundError(f"Workflow root not found: {workflow_root}")

    for conversation_root in sorted(workflow_root.glob("*/builder/conversation")):
        index_path = conversation_root / "chat-index.json"
        try:
            index = json.loads(index_path.read_text(encoding="utf-8")) if index_path.exists() else {}
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"invalid index {index_path}: {exc}") from exc
        index_changed = False

        for date_dir in sorted(conversation_root.iterdir()):
            if not date_dir.is_dir() or date_dir.name in {"users", "system"}:
                continue
            for source in sorted(date_dir.glob("session-*-conversation.json")):
                entry_changed = False
                match = SESSION_FILE.match(source.name)
                if not match:
                    continue
                session_id = match.group(1)
                record = json.loads(source.read_text(encoding="utf-8"))
                workflow_rel = conversation_root.parents[1].relative_to(workspace_root).as_posix()
                source_rel = source.relative_to(workspace_root).as_posix()

                override = owner_map.get(session_id, {})
                user_id = str(override.get("user_id", "")).strip()
                username = str(override.get("username", "")).strip()
                if not user_id:
                    embedded = str(record.get("user_id", "")).strip()
                    if embedded and embedded != "default":
                        user_id = embedded
                        username = str(record.get("username", "")).strip()
                if not user_id:
                    user_id, index_username = index_owner(index, source_rel, session_id)
                    username = username or index_username

                if user_id and user_id != "default" and SAFE_USER_ID.fullmatch(user_id):
                    destination = conversation_root / "users" / user_id / date_dir.name / source.name
                    record["user_id"] = user_id
                    if username:
                        record["username"] = username
                else:
                    destination = conversation_root / "system" / date_dir.name / source.name
                    record["user_id"] = "default"
                    record["username"] = "System / legacy"
                    user_id = "default"
                    username = "System / legacy"

                destination_rel = destination.relative_to(workspace_root).as_posix()
                planned += 1
                print(f"{'MOVE' if apply else 'WOULD MOVE'} {source_rel} -> {destination_rel}")

                entries = index.get("entries", {}) if isinstance(index, dict) else {}
                matching_keys = [
                    key for key, entry in entries.items()
                    if key == source_rel or (
                        isinstance(entry, dict)
                        and isinstance(entry.get("session"), dict)
                        and str(entry["session"].get("session_id", "")) == session_id
                    )
                ]
                for old_key in matching_keys:
                    entry = entries.pop(old_key)
                    session = entry.setdefault("session", {})
                    session["user_id"] = user_id
                    session["username"] = username or session.get("username", "Former user")
                    session["conversation_path"] = destination_rel
                    session["workspace_path"] = workflow_rel
                    entry["attribution_verified"] = True
                    entries[destination_rel] = entry
                    index_changed = True
                    entry_changed = True

                if not apply:
                    continue
                encoded = json.dumps(record, indent=2, ensure_ascii=False) + "\n"
                if destination.exists():
                    if destination.read_text(encoding="utf-8") != encoded:
                        raise FileExistsError(f"destination differs: {destination}")
                else:
                    atomic_write_json(destination, record)
                if entry_changed:
                    # Commit the destination index before removing the legacy file.
                    # If deployment is interrupted at any point, a rerun sees the
                    # still-present source and safely completes the same move.
                    atomic_write_json(index_path, index)
                source.unlink()
                changed += 1

        if apply and index_changed:
            atomic_write_json(index_path, index)

    return planned, changed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workspace-root", type=Path, default=Path(os.environ.get("WORKSPACE_DOCS_PATH", "workspace-docs")))
    parser.add_argument("--owner-map", type=Path)
    parser.add_argument("--apply", action="store_true", help="perform the migration (default is dry-run)")
    args = parser.parse_args()

    try:
        planned, changed = migrate(args.workspace_root.resolve(), load_owner_map(args.owner_map), args.apply)
    except Exception as exc:  # keep operator output concise and non-destructive
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    print(f"{'Applied' if args.apply else 'Planned'} {changed if args.apply else planned} chat migration(s).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
