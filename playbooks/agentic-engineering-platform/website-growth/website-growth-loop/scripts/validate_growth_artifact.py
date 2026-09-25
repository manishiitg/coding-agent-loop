#!/usr/bin/env python3
"""Validate Website Growth Loop v1 handoff structure and local references.

This does not authenticate sources or prove that a cited observation is true.
Run as a blocking Workflow script step after each Crew producer.
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime
from pathlib import Path
from urllib.parse import urlparse


class InvalidArtifact(ValueError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise InvalidArtifact(message)


def obj(value: object, label: str) -> dict:
    require(isinstance(value, dict), f"{label} must be an object")
    return value


def nonempty(value: object, label: str) -> str:
    require(isinstance(value, str) and bool(value.strip()), f"{label} must be non-empty text")
    return value.strip()


def items(value: object, label: str, *, allow_empty: bool = False) -> list:
    require(isinstance(value, list) and (allow_empty or len(value) > 0), f"{label} must be a non-empty list")
    return value


def url(value: object, label: str) -> str:
    result = nonempty(value, label)
    parsed = urlparse(result)
    require(parsed.scheme in {"http", "https"} and bool(parsed.netloc), f"{label} must be an absolute HTTP URL")
    return result


def timestamp(value: object, label: str) -> None:
    raw = nonempty(value, label)
    try:
        datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError as exc:
        raise InvalidArtifact(f"{label} must be an ISO timestamp") from exc


def string_list(value: object, label: str, *, allow_empty: bool = False) -> list[str]:
    result = items(value, label, allow_empty=allow_empty)
    for index, entry in enumerate(result):
        nonempty(entry, f"{label}[{index}]")
    return result


def same_site(page_url: str, site_url: str, label: str) -> None:
    require(urlparse(page_url).hostname == urlparse(site_url).hostname, f"{label} belongs to a different site")


def validate_brief(raw: object) -> set[str]:
    brief = obj(raw, "brief")
    require(brief.get("artifact_type") == "growth-priority-brief/v1", "brief artifact_type is wrong")
    site = url(brief.get("site_url"), "site_url")
    for field in ("offer", "audience", "visitor_action"):
        nonempty(brief.get(field), field)
    timestamp(brief.get("created_at"), "created_at")
    inspected = string_list(brief.get("inspected_urls"), "inspected_urls")
    for index, page in enumerate(inspected):
        same_site(url(page, f"inspected_urls[{index}]"), site, f"inspected_urls[{index}]")
    baseline = obj(brief.get("baseline"), "baseline")
    require(baseline.get("state") in {"available", "unavailable"}, "baseline.state must be available or unavailable")
    for field in ("metric", "start_date", "limitation"):
        nonempty(baseline.get(field), f"baseline.{field}")
    findings = items(brief.get("findings"), "findings")
    finding_ids: set[str] = set()
    for index, value in enumerate(findings):
        label = f"findings[{index}]"
        finding = obj(value, label)
        finding_id = nonempty(finding.get("id"), f"{label}.id")
        require(finding_id not in finding_ids, f"duplicate finding ID {finding_id}")
        finding_ids.add(finding_id)
        page = url(finding.get("page_url"), f"{label}.page_url")
        same_site(page, site, f"{label}.page_url")
        require(page in inspected, f"{label}.page_url was not inspected")
        for field in ("observation", "source_ref"):
            nonempty(finding.get(field), f"{label}.{field}")
        timestamp(finding.get("observed_at"), f"{label}.observed_at")
        require(finding.get("evidence_kind") in {"observed", "owner_reported", "hypothesis"}, f"{label}.evidence_kind is invalid")
    priorities = items(brief.get("priorities"), "priorities")
    require(len(priorities) <= 5, "priorities should contain at most five reviewed actions")
    action_ids: set[str] = set()
    for index, value in enumerate(priorities):
        label = f"priorities[{index}]"
        priority = obj(value, label)
        action_id = nonempty(priority.get("id"), f"{label}.id")
        require(action_id not in action_ids, f"duplicate action ID {action_id}")
        action_ids.add(action_id)
        for field in ("action", "owner", "success_signal", "rank_reason"):
            nonempty(priority.get(field), f"{label}.{field}")
        require(priority.get("effort") in {"small", "medium", "large"}, f"{label}.effort is invalid")
        page = url(priority.get("page_url"), f"{label}.page_url")
        same_site(page, site, f"{label}.page_url")
        refs = string_list(priority.get("evidence_refs"), f"{label}.evidence_refs")
        require(all(ref in finding_ids for ref in refs), f"{label} cites an unknown finding ID")
    string_list(brief.get("unknowns"), "unknowns", allow_empty=True)
    string_list(brief.get("limitations"), "limitations", allow_empty=True)
    return finding_ids


def validate_search(raw: object, brief_raw: object | None) -> None:
    search = obj(raw, "search map")
    require(search.get("artifact_type") == "search-opportunity-list/v1", "search artifact_type is wrong")
    site = url(search.get("site_url"), "site_url")
    timestamp(search.get("created_at"), "created_at")
    inspected = string_list(search.get("inspected_urls"), "inspected_urls")
    for index, page in enumerate(inspected):
        same_site(url(page, f"inspected_urls[{index}]"), site, f"inspected_urls[{index}]")
    brief_ids: set[str] | None = None
    if brief_raw is not None:
        brief_ids = validate_brief(brief_raw)
        require(site == obj(brief_raw, "brief")["site_url"], "search map site_url does not match brief")
    question_ids: set[str] = set()
    for index, value in enumerate(items(search.get("buyer_questions"), "buyer_questions")):
        label = f"buyer_questions[{index}]"
        question = obj(value, label)
        question_id = nonempty(question.get("id"), f"{label}.id")
        require(question_id not in question_ids, f"duplicate question ID {question_id}")
        question_ids.add(question_id)
        for field in ("question", "source_ref", "intent", "next_action", "rank_reason"):
            nonempty(question.get(field), f"{label}.{field}")
        require(question.get("answer_status") in {"answered", "weak_answer", "gap"}, f"{label}.answer_status is invalid")
        require(question.get("confidence") in {"low", "medium", "high"}, f"{label}.confidence is invalid")
        demand = nonempty(question.get("search_demand"), f"{label}.search_demand")
        if demand != "unknown":
            nonempty(question.get("demand_source"), f"{label}.demand_source")
        refs = string_list(question.get("evidence_refs"), f"{label}.evidence_refs")
        for ref in refs:
            same_site(url(ref, f"{label}.evidence_refs"), site, f"{label}.evidence_refs")
            require(ref in inspected, f"{label} cites a page outside its inspected scope")
        existing = question.get("existing_page_url")
        if question["answer_status"] == "gap":
            require(existing is None, f"{label}.existing_page_url must be null for a gap")
        else:
            page = url(existing, f"{label}.existing_page_url")
            require(page in inspected, f"{label}.existing_page_url was not inspected")
        if question.get("source_finding_id") is not None:
            finding_id = nonempty(question["source_finding_id"], f"{label}.source_finding_id")
            if brief_ids is not None:
                require(finding_id in brief_ids, f"{label} cites an unknown strategist finding")
    string_list(search.get("limitations"), "limitations", allow_empty=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("brief", "search"))
    parser.add_argument("artifact", type=Path)
    parser.add_argument("--brief", type=Path, help="validated strategist artifact required for the Automation search handoff")
    args = parser.parse_args()
    if args.kind == "search" and args.brief is None:
        parser.error("search validation requires --brief to check the handoff")
    if args.kind == "brief" and args.brief is not None:
        parser.error("--brief is only for search validation")
    try:
        artifact = json.loads(args.artifact.read_text())
        brief = json.loads(args.brief.read_text()) if args.brief else None
        if args.kind == "brief":
            validate_brief(artifact)
        else:
            validate_search(artifact, brief)
    except (OSError, json.JSONDecodeError, InvalidArtifact) as exc:
        print(f"INVALID {args.kind}: {exc}", file=sys.stderr)
        return 1
    print(f"VALID {args.kind}: {args.artifact}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
