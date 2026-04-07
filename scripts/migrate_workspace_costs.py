#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


MODEL_INT_FIELDS = [
    "input_tokens",
    "output_tokens",
    "cache_tokens",
    "cache_read_tokens",
    "cache_write_tokens",
    "reasoning_tokens",
    "llm_call_count",
    "context_window_usage",
    "model_context_window",
]

MODEL_FLOAT_FIELDS = [
    "input_cost_usd",
    "output_cost_usd",
    "reasoning_cost_usd",
    "cache_cost_usd",
    "cache_read_cost_usd",
    "cache_write_cost_usd",
    "total_cost_usd",
    "context_usage_percent",
]

MODEL_FORMATTED_FIELDS = [
    ("input_tokens", "input_tokens_m"),
    ("output_tokens", "output_tokens_m"),
    ("cache_tokens", "cache_tokens_m"),
    ("cache_read_tokens", "cache_read_tokens_m"),
    ("cache_write_tokens", "cache_write_tokens_m"),
    ("reasoning_tokens", "reasoning_tokens_m"),
]


@dataclass
class MigrationStats:
    workflows_seen: int = 0
    workflows_changed: int = 0
    execution_files: int = 0
    evaluation_files: int = 0
    phase_files: int = 0

    @property
    def total_files(self) -> int:
        return self.execution_files + self.evaluation_files + self.phase_files


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Migrate legacy workflow token usage files into costs/ ledgers."
    )
    parser.add_argument(
        "--workflow-root",
        default=None,
        help="Path to workspace-docs/Workflow (defaults to repo-local workspace-docs/Workflow).",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Report what would change without writing files.",
    )
    return parser.parse_args()


def default_workflow_root() -> Path:
    return Path(__file__).resolve().parents[1] / "workspace-docs" / "Workflow"


def parse_timestamp(value: Any) -> datetime | None:
    if not value or not isinstance(value, str):
        return None
    candidate = value.strip()
    if not candidate:
        return None
    if candidate.endswith("Z"):
        candidate = candidate[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(candidate)
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


def isoformat_z(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def format_tokens_m(tokens: int) -> str:
    if tokens == 0:
        return "0.000M"
    return f"{tokens / 1_000_000.0:.3f}M"


def extract_group_folder(run_folder: str) -> str:
    parts = [part for part in run_folder.strip("/").split("/") if part]
    if len(parts) >= 2 and parts[1].strip():
        return parts[1].strip()
    return "__ungrouped__"


def read_json(path: Path) -> dict[str, Any] | None:
    try:
        return json.loads(path.read_text())
    except FileNotFoundError:
        return None
    except json.JSONDecodeError:
        return None


def split_conflicted_text(text: str) -> tuple[str, str] | None:
    if "<<<<<<< " not in text or "=======" not in text or ">>>>>>> " not in text:
        return None

    left: list[str] = []
    right: list[str] = []
    mode = "both"

    for line in text.splitlines(keepends=True):
        if line.startswith("<<<<<<< "):
            mode = "left"
            continue
        if line.startswith("=======") and mode == "left":
            mode = "right"
            continue
        if line.startswith(">>>>>>> ") and mode == "right":
            mode = "both"
            continue

        if mode in {"both", "left"}:
            left.append(line)
        if mode in {"both", "right"}:
            right.append(line)

    return "".join(left), "".join(right)


def read_json_with_conflict_merge(
    path: Path, merge_fn
) -> dict[str, Any] | None:
    try:
        text = path.read_text()
    except FileNotFoundError:
        return None

    try:
        return json.loads(text)
    except json.JSONDecodeError:
        split = split_conflicted_text(text)
        if split is None:
            return None
        left_text, right_text = split
        try:
            left = json.loads(left_text)
            right = json.loads(right_text)
        except json.JSONDecodeError:
            return None
        return merge_fn(left, right)


def write_json(path: Path, data: dict[str, Any], dry_run: bool) -> None:
    if dry_run:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def delete_file(path: Path, dry_run: bool) -> None:
    if dry_run:
        return
    path.unlink(missing_ok=True)


def ensure_token_file_shape(data: dict[str, Any] | None) -> dict[str, Any]:
    token_file = dict(data or {})
    token_file.setdefault("by_model", {})
    token_file.setdefault("by_step_and_model", {})
    return token_file


def ensure_phase_file_shape(data: dict[str, Any] | None) -> dict[str, Any]:
    token_file = dict(data or {})
    token_file.setdefault("by_model", {})
    token_file.setdefault("by_phase_and_model", {})
    return token_file


def merge_model_usage(dst: dict[str, Any] | None, src: dict[str, Any] | None) -> dict[str, Any] | None:
    if src is None:
        return dst
    if dst is None:
        merged = dict(src)
        for raw_field, formatted_field in MODEL_FORMATTED_FIELDS:
            if raw_field in merged:
                merged[formatted_field] = format_tokens_m(int(merged.get(raw_field, 0) or 0))
        return merged

    merged = dict(dst)

    for field in MODEL_INT_FIELDS:
        merged[field] = int(merged.get(field, 0) or 0) + int(src.get(field, 0) or 0)

    for field in MODEL_FLOAT_FIELDS:
        if field == "context_usage_percent":
            merged[field] = max(float(merged.get(field, 0.0) or 0.0), float(src.get(field, 0.0) or 0.0))
        else:
            merged[field] = float(merged.get(field, 0.0) or 0.0) + float(src.get(field, 0.0) or 0.0)

    for raw_field, formatted_field in MODEL_FORMATTED_FIELDS:
        merged[formatted_field] = format_tokens_m(int(merged.get(raw_field, 0) or 0))

    provider = src.get("provider")
    if provider:
        merged["provider"] = provider
    if int(src.get("model_context_window", 0) or 0) > 0:
        merged["model_context_window"] = int(src["model_context_window"])
    if int(src.get("context_window_usage", 0) or 0) > 0:
        merged["context_window_usage"] = int(src["context_window_usage"])

    return merged


def merge_token_usage_files(dst: dict[str, Any] | None, src: dict[str, Any] | None) -> dict[str, Any]:
    if src is None:
        return ensure_token_file_shape(dst)
    if dst is None:
        return ensure_token_file_shape(src)

    merged = ensure_token_file_shape(dst)
    src = ensure_token_file_shape(src)

    dst_created = parse_timestamp(merged.get("created_at"))
    src_created = parse_timestamp(src.get("created_at"))
    if dst_created is None or (src_created is not None and src_created < dst_created):
        if src_created is not None:
            merged["created_at"] = isoformat_z(src_created)

    dst_updated = parse_timestamp(merged.get("updated_at"))
    src_updated = parse_timestamp(src.get("updated_at"))
    if src_updated is not None and (dst_updated is None or src_updated > dst_updated):
        merged["updated_at"] = isoformat_z(src_updated)

    by_model = dict(merged.get("by_model", {}))
    for model_id, usage in src.get("by_model", {}).items():
        by_model[model_id] = merge_model_usage(by_model.get(model_id), usage)
    merged["by_model"] = by_model

    by_step_and_model = {
        step_key: dict(model_map)
        for step_key, model_map in merged.get("by_step_and_model", {}).items()
    }
    for step_key, model_map in src.get("by_step_and_model", {}).items():
        existing_map = by_step_and_model.setdefault(step_key, {})
        for model_id, usage in model_map.items():
            existing_map[model_id] = merge_model_usage(existing_map.get(model_id), usage)
    merged["by_step_and_model"] = by_step_and_model

    return merged


def merge_phase_usage_files(dst: dict[str, Any] | None, src: dict[str, Any] | None) -> dict[str, Any]:
    if src is None:
        return ensure_phase_file_shape(dst)
    if dst is None:
        return ensure_phase_file_shape(src)

    merged = ensure_phase_file_shape(dst)
    src = ensure_phase_file_shape(src)

    dst_created = parse_timestamp(merged.get("created_at"))
    src_created = parse_timestamp(src.get("created_at"))
    if dst_created is None or (src_created is not None and src_created < dst_created):
        if src_created is not None:
            merged["created_at"] = isoformat_z(src_created)

    dst_updated = parse_timestamp(merged.get("updated_at"))
    src_updated = parse_timestamp(src.get("updated_at"))
    if src_updated is not None and (dst_updated is None or src_updated > dst_updated):
        merged["updated_at"] = isoformat_z(src_updated)

    by_model = dict(merged.get("by_model", {}))
    for model_id, usage in src.get("by_model", {}).items():
        by_model[model_id] = merge_model_usage(by_model.get(model_id), usage)
    merged["by_model"] = by_model

    by_phase_and_model = {
        phase_key: dict(model_map)
        for phase_key, model_map in merged.get("by_phase_and_model", {}).items()
    }
    for phase_key, model_map in src.get("by_phase_and_model", {}).items():
        existing_map = by_phase_and_model.setdefault(phase_key, {})
        for model_id, usage in model_map.items():
            existing_map[model_id] = merge_model_usage(existing_map.get(model_id), usage)
    merged["by_phase_and_model"] = by_phase_and_model

    return merged


def migrate_scoped_run_files(
    workspace_dir: Path,
    legacy_root: Path,
    scope: str,
    stats: MigrationStats,
    dry_run: bool,
) -> int:
    if not legacy_root.exists():
        return 0

    migrated = 0
    for legacy_path in legacy_root.rglob("token_usage.json"):
        if not legacy_path.is_file():
            continue

        run_folder = legacy_path.parent.relative_to(legacy_root).as_posix()
        if run_folder in {"", "."}:
            continue

        token_file = read_json_with_conflict_merge(legacy_path, merge_token_usage_files)
        if token_file is None:
            continue

        timestamp = (
            parse_timestamp(token_file.get("created_at"))
            or parse_timestamp(token_file.get("updated_at"))
            or datetime.fromtimestamp(legacy_path.stat().st_mtime, tz=timezone.utc)
        )
        date_key = timestamp.astimezone(timezone.utc).strftime("%Y-%m-%d")
        group_folder = extract_group_folder(run_folder)
        target_path = workspace_dir / "costs" / scope / group_folder / f"{date_key}.json"

        daily_file = read_json(target_path) or {}
        run_folders = dict(daily_file.get("run_folders", {}))
        run_folders[run_folder] = merge_token_usage_files(run_folders.get(run_folder), token_file)

        daily_file["date"] = date_key
        daily_file["group_folder"] = group_folder
        daily_file["updated_at"] = isoformat_z(now_utc())
        daily_file["run_folders"] = run_folders

        write_json(target_path, daily_file, dry_run)
        delete_file(legacy_path, dry_run)
        migrated += 1

    if scope == "evaluation":
        stats.evaluation_files += migrated
    else:
        stats.execution_files += migrated
    return migrated


def migrate_phase_file(workspace_dir: Path, stats: MigrationStats, dry_run: bool) -> int:
    legacy_path = workspace_dir / "token_usage.json"
    if not legacy_path.exists():
        return 0

    legacy_file = read_json_with_conflict_merge(legacy_path, merge_phase_usage_files)
    if legacy_file is None:
        return 0

    target_path = workspace_dir / "costs" / "phase" / "token_usage.json"
    merged = merge_phase_usage_files(read_json(target_path), legacy_file)
    write_json(target_path, merged, dry_run)
    delete_file(legacy_path, dry_run)
    stats.phase_files += 1
    return 1


def migrate_workflow(workspace_dir: Path, stats: MigrationStats, dry_run: bool) -> int:
    changed = 0
    changed += migrate_scoped_run_files(workspace_dir, workspace_dir / "runs", "execution", stats, dry_run)
    changed += migrate_scoped_run_files(
        workspace_dir, workspace_dir / "evaluation" / "runs", "evaluation", stats, dry_run
    )
    changed += migrate_phase_file(workspace_dir, stats, dry_run)
    return changed


def main() -> int:
    args = parse_args()
    workflow_root = Path(args.workflow_root).resolve() if args.workflow_root else default_workflow_root()
    if not workflow_root.is_dir():
        raise SystemExit(f"workflow root does not exist: {workflow_root}")

    stats = MigrationStats()

    for workspace_dir in sorted(path for path in workflow_root.iterdir() if path.is_dir()):
        stats.workflows_seen += 1
        changed = migrate_workflow(workspace_dir, stats, args.dry_run)
        if changed > 0:
            stats.workflows_changed += 1
            print(f"{workspace_dir.name}: migrated {changed} file(s)")

    mode = "dry-run" if args.dry_run else "completed"
    print(
        f"{mode}: workflows_seen={stats.workflows_seen} "
        f"workflows_changed={stats.workflows_changed} "
        f"execution_files={stats.execution_files} "
        f"evaluation_files={stats.evaluation_files} "
        f"phase_files={stats.phase_files} "
        f"total_files={stats.total_files}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
