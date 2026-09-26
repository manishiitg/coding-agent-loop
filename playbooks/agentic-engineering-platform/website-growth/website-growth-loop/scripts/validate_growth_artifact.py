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
    nonempty(brief.get("artifact_id"), "brief.artifact_id")
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
    nonempty(search.get("artifact_id"), "search.artifact_id")
    nonempty(search.get("source_brief_artifact_id"), "search.source_brief_artifact_id")
    site = url(search.get("site_url"), "site_url")
    timestamp(search.get("created_at"), "created_at")
    inspected = string_list(search.get("inspected_urls"), "inspected_urls")
    for index, page in enumerate(inspected):
        same_site(url(page, f"inspected_urls[{index}]"), site, f"inspected_urls[{index}]")
    brief_ids: set[str] | None = None
    if brief_raw is not None:
        brief_ids = validate_brief(brief_raw)
        require(site == obj(brief_raw, "brief")["site_url"], "search map site_url does not match brief")
        require(search["source_brief_artifact_id"] == brief_raw["artifact_id"], "search map cites a different strategist artifact")
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


def validate_content(raw: object, search_raw: object, brief_raw: object) -> dict[str, dict]:
    validate_search(search_raw, brief_raw)
    content = obj(raw, "content brief")
    search = obj(search_raw, "search map")
    brief = obj(brief_raw, "growth brief")
    require(content.get("artifact_type") == "content-brief/v1", "content artifact_type is wrong")
    nonempty(content.get("artifact_id"), "content.artifact_id")
    require(content.get("source_search_artifact_id") == search["artifact_id"], "content cites a different search artifact")
    site = url(content.get("site_url"), "content.site_url")
    require(site == search["site_url"], "content site_url does not match search map")
    timestamp(content.get("created_at"), "content.created_at")
    question_id = nonempty(content.get("source_question_id"), "content.source_question_id")
    questions = {question["id"]: question for question in search["buyer_questions"]}
    require(question_id in questions, "content cites an unknown buyer question")
    question = questions[question_id]
    require(content.get("buyer_question") == question["question"], "content changed the buyer question")
    require(question["confidence"] != "low" and "hypothesis" not in question["source_ref"].lower(), "content needs a sourced, reviewed buyer question")
    approval = obj(content.get("approved_opportunity"), "approved_opportunity")
    require(approval.get("decision") == "approved", "content opportunity lacks owner approval")
    nonempty(approval.get("owner_id"), "approved_opportunity.owner_id")
    timestamp(approval.get("approved_at"), "approved_opportunity.approved_at")
    require(content.get("visitor_action") == brief["visitor_action"], "content changed the visitor action")
    target = url(content.get("target_page_url"), "content.target_page_url")
    same_site(target, site, "content.target_page_url")
    mode = content.get("page_mode")
    require(mode in {"update_existing", "new_page"}, "content.page_mode is invalid")
    if mode == "update_existing":
        require(target == question["existing_page_url"], "content update target is not the inspected existing page")
    else:
        require(question["answer_status"] == "gap" and question["existing_page_url"] is None, "new page needs a validated content gap")
        require(target not in search["inspected_urls"], "new page target already exists in inspected scope")
    nonempty(content.get("angle"), "content.angle")
    string_list(content.get("outline"), "content.outline")
    claims: dict[str, dict] = {}
    for index, value in enumerate(items(content.get("claims"), "content.claims")):
        claim = obj(value, f"content.claims[{index}]")
        claim_id = nonempty(claim.get("id"), f"content.claims[{index}].id")
        require(claim_id not in claims, f"duplicate content claim ID {claim_id}")
        nonempty(claim.get("text"), f"content.claims[{index}].text")
        require(claim.get("status") in {"verified", "needs_verification"}, f"content.claims[{index}].status is invalid")
        if claim["status"] == "verified":
            nonempty(claim.get("source_ref"), f"content.claims[{index}].source_ref")
        else:
            require(claim.get("source_ref") is None, f"content.claims[{index}] must not imply a verified source")
        claims[claim_id] = claim
    for index, link in enumerate(string_list(content.get("internal_links"), "content.internal_links", allow_empty=True)):
        same_site(url(link, f"content.internal_links[{index}]"), site, f"content.internal_links[{index}]")
        require(link in search["inspected_urls"], "content cites an uninspected internal link")
    open_questions = string_list(content.get("open_questions"), "content.open_questions", allow_empty=True)
    if any(claim["status"] == "needs_verification" for claim in claims.values()):
        require(bool(open_questions), "unverified content claims need an open question")
    nonempty(content.get("success_signal"), "content.success_signal")
    return claims


def validate_page(raw: object, content_raw: object, search_raw: object, brief_raw: object) -> None:
    claims = validate_content(content_raw, search_raw, brief_raw)
    page = obj(raw, "page draft")
    content = obj(content_raw, "content brief")
    require(page.get("artifact_type") == "reviewable-page-draft/v1", "page artifact_type is wrong")
    nonempty(page.get("artifact_id"), "page.artifact_id")
    require(page.get("source_content_artifact_id") == content["artifact_id"], "page cites a different content brief")
    require(page.get("source_question_id") == content["source_question_id"], "page changed the buyer question")
    require(page.get("site_url") == content["site_url"], "page site_url does not match content brief")
    require(page.get("target_page_url") == content["target_page_url"], "page target does not match approved content brief")
    timestamp(page.get("created_at"), "page.created_at")
    nonempty(page.get("draft_ref"), "page.draft_ref")
    require(page.get("draft_state") == "reviewable" and page.get("review_state") in {"pending", "approved"}, "page is not a reviewable draft")
    require(page.get("publication_state") == "not_published" and page.get("publication_receipt") is None, "page draft falsely claims publication")
    used_claims: set[str] = set()
    for index, value in enumerate(items(page.get("sections"), "page.sections")):
        section = obj(value, f"page.sections[{index}]")
        nonempty(section.get("heading"), f"page.sections[{index}].heading")
        nonempty(section.get("copy"), f"page.sections[{index}].copy")
        for claim_id in string_list(section.get("claim_ids"), f"page.sections[{index}].claim_ids", allow_empty=True):
            require(claim_id in claims, f"page cites unknown claim {claim_id}")
            require(claims[claim_id]["status"] == "verified", f"page uses unverified claim {claim_id}")
            used_claims.add(claim_id)
    unresolved = set(string_list(page.get("unverified_claim_ids"), "page.unverified_claim_ids", allow_empty=True))
    expected_unresolved = {claim_id for claim_id, claim in claims.items() if claim["status"] == "needs_verification"}
    require(unresolved == expected_unresolved and not used_claims.intersection(unresolved), "page omitted or used an unverified content claim")
    links = string_list(page.get("internal_links"), "page.internal_links", allow_empty=True)
    require(all(link in content["internal_links"] for link in links), "page added an unreviewed internal link")
    string_list(page.get("open_questions"), "page.open_questions", allow_empty=True)
    nonempty(page.get("next_action"), "page.next_action")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("brief", "search", "content", "page"))
    parser.add_argument("artifact", type=Path)
    parser.add_argument("--brief", type=Path, help="validated strategist artifact required for the Automation search handoff")
    parser.add_argument("--search", type=Path, help="validated search map required for content and page handoffs")
    parser.add_argument("--content", type=Path, help="approved content brief required for the page handoff")
    args = parser.parse_args()
    if args.kind in {"search", "content", "page"} and args.brief is None:
        parser.error(f"{args.kind} validation requires --brief to check the handoff")
    if args.kind in {"content", "page"} and args.search is None:
        parser.error(f"{args.kind} validation requires --search to check the handoff")
    if args.kind == "page" and args.content is None:
        parser.error("page validation requires --content to check the handoff")
    unexpected = (
        (args.kind == "brief" and any((args.brief, args.search, args.content)))
        or (args.kind == "search" and any((args.search, args.content)))
        or (args.kind == "content" and args.content is not None)
    )
    if unexpected:
        parser.error("unexpected upstream artifact argument for this validation kind")
    try:
        artifact = json.loads(args.artifact.read_text())
        brief = json.loads(args.brief.read_text()) if args.brief else None
        search = json.loads(args.search.read_text()) if args.search else None
        content = json.loads(args.content.read_text()) if args.content else None
        if args.kind == "brief":
            validate_brief(artifact)
        elif args.kind == "search":
            validate_search(artifact, brief)
        elif args.kind == "content":
            validate_content(artifact, search, brief)
        else:
            validate_page(artifact, content, search, brief)
    except (OSError, json.JSONDecodeError, InvalidArtifact) as exc:
        print(f"INVALID {args.kind}: {exc}", file=sys.stderr)
        return 1
    print(f"VALID {args.kind}: {args.artifact}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
