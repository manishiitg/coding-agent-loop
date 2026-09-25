#!/usr/bin/env python3
"""Validate Sales v1 artifacts and lead-to-research-to-follow-up references.

The checks block malformed or out-of-scope handoffs. They do not prove
source truth or authorization to contact a person.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import datetime
from pathlib import Path


class InvalidArtifact(ValueError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise InvalidArtifact(message)


def obj(value: object, label: str) -> dict:
    require(isinstance(value, dict), f"{label} must be an object")
    return value


def text(value: object, label: str) -> str:
    require(isinstance(value, str) and bool(value.strip()), f"{label} must be non-empty text")
    return value.strip()


def items(value: object, label: str, *, allow_empty: bool = False) -> list:
    require(isinstance(value, list) and (allow_empty or bool(value)), f"{label} must be a {'list' if allow_empty else 'non-empty list'}")
    return value


def timestamp(value: object, label: str) -> None:
    try:
        parsed = datetime.fromisoformat(text(value, label).replace("Z", "+00:00"))
    except ValueError as exc:
        raise InvalidArtifact(f"{label} must be an ISO timestamp") from exc
    require(parsed.tzinfo is not None, f"{label} needs a timezone")


def base(raw: object, kind: str) -> dict:
    doc = obj(raw, kind)
    require(doc.get("schema_version") == 1, f"{kind}.schema_version must be 1")
    require(doc.get("artifact_type") == f"{kind}/v1", f"{kind}.artifact_type is wrong")
    timestamp(doc.get("created_at"), f"{kind}.created_at")
    return doc


def references(raw: object, label: str) -> set[str]:
    ids: set[str] = set()
    for index, value in enumerate(items(raw, label)):
        ref = obj(value, f"{label}[{index}]")
        ref_id = text(ref.get("id"), f"{label}[{index}].id")
        require(ref_id not in ids, f"duplicate reference {ref_id}")
        ids.add(ref_id)
        text(ref.get("uri"), f"{label}[{index}].uri")
        timestamp(ref.get("observed_at"), f"{label}[{index}].observed_at")
    return ids


def known_refs(raw: object, available: set[str], label: str) -> None:
    for index, value in enumerate(items(raw, label)):
        require(text(value, f"{label}[{index}]") in available, f"{label} cites an unknown source")


def validate_qualification(raw: object) -> dict:
    brief = base(raw, "lead-qualification-brief")
    for field in ("brief_id", "lead_id", "entity", "period", "contact_ref", "inbound_request", "owner", "next_action"):
        text(brief.get(field), field)
    require("@" not in brief["contact_ref"], "contact_ref must identify an authorized source record, not contain an email address")
    domain = brief.get("company_domain")
    require(domain is None or (isinstance(domain, str) and bool(re.fullmatch(r"[a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)+", domain))), "company_domain must be a domain or null")
    require(brief.get("fit_status") in {"qualified", "needs_review", "not_fit"}, "fit_status is invalid")
    require(brief.get("duplicate_state") in {"none", "possible", "known"}, "duplicate_state is invalid")
    require(brief.get("contact_policy_status") in {"review_required", "approved", "blocked"}, "contact_policy_status is invalid")
    available = references(brief.get("source_refs"), "source_refs")
    known_refs(brief.get("evidence_refs"), available, "evidence_refs")
    criteria_ids: set[str] = set()
    for index, value in enumerate(items(brief.get("criteria"), "criteria")):
        label = f"criteria[{index}]"
        criterion = obj(value, label)
        criterion_id = text(criterion.get("id"), f"{label}.id")
        require(criterion_id not in criteria_ids, f"duplicate criterion {criterion_id}")
        criteria_ids.add(criterion_id)
        require(criterion.get("result") in {"match", "no_match", "unknown"}, f"{label}.result is invalid")
        text(criterion.get("reason"), f"{label}.reason")
        known_refs(criterion.get("evidence_refs"), available, f"{label}.evidence_refs")
    items(brief.get("missing_info"), "missing_info", allow_empty=True)
    items(brief.get("limitations"), "limitations", allow_empty=True)
    return brief


def require_ready_for_handoff(brief: dict) -> None:
    require(brief["fit_status"] == "qualified", "handoff requires a qualified lead")
    require(brief["duplicate_state"] == "none", "handoff blocked by duplicate lead state")
    require(brief["contact_policy_status"] != "blocked", "handoff blocked by contact policy")


def validate_research(raw: object, qualification_raw: object) -> dict:
    research = base(raw, "account-research-brief")
    qualification = validate_qualification(qualification_raw)
    require_ready_for_handoff(qualification)
    text(research.get("research_id"), "research_id")
    require(research.get("source_brief_id") == qualification["brief_id"], "research cites a different qualification brief")
    require(qualification["company_domain"] is not None, "research requires a resolved company domain")
    require(research.get("company_domain") == qualification["company_domain"], "research company_domain differs from lead")
    available = references(research.get("source_refs"), "source_refs") | references(qualification.get("source_refs"), "qualification.source_refs")
    finding_ids: set[str] = set()
    for index, value in enumerate(items(research.get("findings"), "findings")):
        label = f"findings[{index}]"
        finding = obj(value, label)
        finding_id = text(finding.get("id"), f"{label}.id")
        require(finding_id not in finding_ids, f"duplicate finding {finding_id}")
        finding_ids.add(finding_id)
        text(finding.get("finding"), f"{label}.finding")
        require(finding.get("evidence_kind") in {"observed", "owner_provided", "hypothesis"}, f"{label}.evidence_kind is invalid")
        known_refs(finding.get("evidence_refs"), available, f"{label}.evidence_refs")
    items(research.get("questions"), "questions", allow_empty=True)
    items(research.get("limitations"), "limitations", allow_empty=True)
    return research


def validate_followup(raw: object, qualification_raw: object, research_raw: object | None) -> None:
    draft = base(raw, "sales-followup-draft")
    qualification = validate_qualification(qualification_raw)
    require_ready_for_handoff(qualification)
    require(draft.get("source_brief_id") == qualification["brief_id"], "follow-up cites a different qualification brief")
    require(draft.get("lead_id") == qualification["lead_id"], "follow-up cites a different lead")
    require(draft.get("recipient_ref") == qualification["contact_ref"], "follow-up recipient differs from lead contact")
    require(draft.get("status") == "draft" and draft.get("send_state") == "not_sent", "follow-up must remain an unsent draft")
    require(draft.get("approval_required") is True, "follow-up must require owner approval")
    research_id = draft.get("source_research_id")
    if research_raw is None:
        require(research_id is None, "follow-up cites research that was not validated")
        research_refs: set[str] = set()
    else:
        research = validate_research(research_raw, qualification_raw)
        require(research_id == research["research_id"], "follow-up cites a different research brief")
        research_refs = references(research.get("source_refs"), "research.source_refs")
    for field in ("draft_id", "owner", "channel", "subject", "body", "next_action"):
        text(draft.get(field), field)
    timestamp(draft.get("next_check_at"), "next_check_at")
    available = references(draft.get("source_refs"), "source_refs") | references(qualification.get("source_refs"), "qualification.source_refs") | research_refs
    known_refs(draft.get("claim_refs"), available, "claim_refs")
    items(draft.get("limitations"), "limitations", allow_empty=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("qualification", "research", "followup"))
    parser.add_argument("artifact", type=Path)
    parser.add_argument("--qualification", type=Path)
    parser.add_argument("--research", type=Path)
    parser.add_argument("--require-ready", action="store_true", help="block a qualification handoff unless fit, duplicate, and contact states are ready")
    args = parser.parse_args()
    if args.kind in {"research", "followup"} and args.qualification is None:
        parser.error(f"{args.kind} validation requires --qualification")
    if args.kind == "qualification" and (args.qualification or args.research):
        parser.error("qualification validation accepts no handoff files")
    if args.kind == "research" and args.research:
        parser.error("--research is only for followup validation")
    if args.kind != "qualification" and args.require_ready:
        parser.error("--require-ready is only for qualification validation")
    try:
        artifact = json.loads(args.artifact.read_text())
        qualification = json.loads(args.qualification.read_text()) if args.qualification else None
        research = json.loads(args.research.read_text()) if args.research else None
        if args.kind == "qualification":
            brief = validate_qualification(artifact)
            if args.require_ready:
                require_ready_for_handoff(brief)
        elif args.kind == "research":
            validate_research(artifact, qualification)
        else:
            validate_followup(artifact, qualification, research)
    except (OSError, json.JSONDecodeError, InvalidArtifact) as exc:
        print(f"INVALID {args.kind}: {exc}", file=sys.stderr)
        return 1
    print(f"VALID {args.kind}: {args.artifact}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
