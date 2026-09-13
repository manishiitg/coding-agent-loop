#!/usr/bin/env python3
"""Validate AgentWorks playbook packages against Playbook Specification v1."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
REQUIRED_SECTIONS = [
    "Outcome",
    "When to use",
    "Required inputs",
    "Plan and AgentWorks tools",
    "Knowledge and persistence",
    "Validation and reporting",
    "Guardrails",
    "Read details when needed",
    "Completion contract",
]
REQUIRED_MANIFEST_FIELDS = [
    "metadata_version",
    "content_schema",
    "id",
    "version",
    "title",
    "description",
    "hierarchy",
    "order",
    "entrypoint",
    "audience",
    "setup_prompt",
    "setup_inputs",
    "required_capabilities",
    "recommended_tools",
    "pulse_focus",
    "outputs",
]
REQUIRED_TOOL_FIELDS = {"id", "name", "type", "purpose", "capability", "optional"}
REQUIRED_PULSE_FOCUS_FIELDS = {"module", "label", "focus_areas", "review_when"}
PULSE_FOCUS_MODULES = {"technical_review", "architecture_review", "strategic_review"}
REQUIRED_SKILL_RECOMMENDATION_FIELDS = {
    "id",
    "name",
    "publisher",
    "source",
    "install_hint",
    "purpose",
    "optional",
}
KEBAB_CASE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
SEMVER = re.compile(r"^\d+\.\d+\.\d+$")
MARKDOWN_LINK = re.compile(r"\[[^\]]+\]\(([^)]+)\)")


def fail(errors: list[str], path: Path, message: str) -> None:
    errors.append(f"{path.relative_to(ROOT.parent)}: {message}")


def frontmatter(text: str) -> dict[str, str]:
    match = re.match(r"\A---\s*\n(.*?)\n---\s*\n", text, re.DOTALL)
    if not match:
        return {}
    values: dict[str, str] = {}
    for line in match.group(1).splitlines():
        if ":" in line:
            key, value = line.split(":", 1)
            values[key.strip()] = value.strip()
    return values


def word_count_without_frontmatter(text: str) -> int:
    body = re.sub(r"\A---\s*\n.*?\n---\s*\n", "", text, count=1, flags=re.DOTALL)
    return len(re.findall(r"\b[\w'-]+\b", body))


def heading_slug(heading: str) -> str:
    value = re.sub(r"[^a-z0-9 _-]", "", heading.strip().lower())
    return re.sub(r"[ _]+", "-", value)


def markdown_anchors(path: Path) -> set[str]:
    return {
        heading_slug(heading)
        for heading in re.findall(r"^#{1,6}\s+(.+?)\s*$", path.read_text(), re.MULTILINE)
    }


def validate_links(path: Path, text: str, errors: list[str]) -> None:
    for raw_target in MARKDOWN_LINK.findall(text):
        target = raw_target.strip()
        if not target or "://" in target:
            continue
        relative, _, anchor = target.partition("#")
        linked_path = path if not relative else path.parent / relative
        if not linked_path.exists():
            fail(errors, path, f"broken local link: {target}")
        elif anchor and linked_path.suffix.lower() == ".md" and anchor not in markdown_anchors(linked_path):
            fail(errors, path, f"broken local anchor: {target}")


def validate_package(package: Path, errors: list[str]) -> dict[str, object] | None:
    manifest_path = package / "playbook.json"
    try:
        manifest = json.loads(manifest_path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        fail(errors, manifest_path, f"invalid JSON: {exc}")
        return None

    for field in REQUIRED_MANIFEST_FIELDS:
        if field not in manifest:
            fail(errors, manifest_path, f"missing required field {field!r}")

    playbook_id = manifest.get("id")
    if not isinstance(playbook_id, str) or not KEBAB_CASE.fullmatch(playbook_id):
        fail(errors, manifest_path, "id must be lowercase kebab-case")
    if package.name != playbook_id:
        fail(errors, manifest_path, "id must match the package directory")
    if manifest.get("metadata_version") != 1:
        fail(errors, manifest_path, "metadata_version must be 1")
    if manifest.get("content_schema") != "agentworks-playbook/v1":
        fail(errors, manifest_path, "content_schema must be agentworks-playbook/v1")
    if not isinstance(manifest.get("version"), str) or not SEMVER.fullmatch(manifest["version"]):
        fail(errors, manifest_path, "version must use MAJOR.MINOR.PATCH")
    changelog = manifest.get("changelog", [])
    if not isinstance(changelog, list):
        fail(errors, manifest_path, "changelog must be a list")
    else:
        for index, entry in enumerate(changelog):
            if not isinstance(entry, dict) or set(entry) != {"version", "summary"}:
                fail(errors, manifest_path, f"changelog[{index}] needs only version and summary")
                continue
            if not isinstance(entry["version"], str) or not SEMVER.fullmatch(entry["version"]):
                fail(errors, manifest_path, f"changelog[{index}].version must use MAJOR.MINOR.PATCH")
            if not isinstance(entry["summary"], str) or not entry["summary"].strip():
                fail(errors, manifest_path, f"changelog[{index}].summary must be non-empty")
    if manifest.get("entrypoint") != "SKILL.md":
        fail(errors, manifest_path, "entrypoint must be SKILL.md")
    if manifest.get("audience") != "workflow_builder":
        fail(errors, manifest_path, "audience must be workflow_builder")
    hierarchy = manifest.get("hierarchy")
    if not isinstance(hierarchy, list) or not hierarchy or hierarchy[0] != "AgentWorks":
        fail(errors, manifest_path, "hierarchy must be a non-empty list starting with AgentWorks")
    if not isinstance(manifest.get("order"), int) or manifest["order"] < 1:
        fail(errors, manifest_path, "order must be a positive integer")

    for index, setup_input in enumerate(manifest.get("setup_inputs", [])):
        if not isinstance(setup_input, dict) or not {"id", "label", "required"}.issubset(setup_input):
            fail(errors, manifest_path, f"setup_inputs[{index}] needs id, label, and required")
    for index, tool in enumerate(manifest.get("recommended_tools", [])):
        if not isinstance(tool, dict) or not REQUIRED_TOOL_FIELDS.issubset(tool):
            fail(errors, manifest_path, f"recommended_tools[{index}] has an incomplete shape")
        elif tool.get("optional") is not True:
            fail(errors, manifest_path, f"recommended_tools[{index}].optional must be true")
    pulse_focus = manifest.get("pulse_focus", [])
    if not isinstance(pulse_focus, list):
        fail(errors, manifest_path, "pulse_focus must be a list")
    else:
        modules: set[object] = set()
        for index, focus in enumerate(pulse_focus):
            if not isinstance(focus, dict) or not REQUIRED_PULSE_FOCUS_FIELDS.issubset(focus):
                fail(errors, manifest_path, f"pulse_focus[{index}] has an incomplete shape")
                continue
            modules.add(focus.get("module"))
            if not isinstance(focus.get("focus_areas"), list) or not focus["focus_areas"]:
                fail(errors, manifest_path, f"pulse_focus[{index}].focus_areas must be a non-empty list")
            if not isinstance(focus.get("review_when"), list) or not focus["review_when"]:
                fail(errors, manifest_path, f"pulse_focus[{index}].review_when must be a non-empty list")
        if modules != PULSE_FOCUS_MODULES:
            fail(errors, manifest_path, "pulse_focus must define technical_review, architecture_review, and strategic_review only")
    recommended_skills = manifest.get("recommended_skills", [])
    if not isinstance(recommended_skills, list):
        fail(errors, manifest_path, "recommended_skills must be a list")
    else:
        for index, skill in enumerate(recommended_skills):
            if not isinstance(skill, dict) or not REQUIRED_SKILL_RECOMMENDATION_FIELDS.issubset(skill):
                fail(errors, manifest_path, f"recommended_skills[{index}] has an incomplete shape")
            elif skill.get("optional") is not True:
                fail(errors, manifest_path, f"recommended_skills[{index}].optional must be true")

    skill_path = package / str(manifest.get("entrypoint", "SKILL.md"))
    if not skill_path.is_file():
        fail(errors, manifest_path, "entrypoint does not exist")
        return manifest
    text = skill_path.read_text()
    metadata = frontmatter(text)
    if metadata.get("name") != playbook_id:
        fail(errors, skill_path, "frontmatter name must match playbook id")
    if not metadata.get("description"):
        fail(errors, skill_path, "frontmatter description is required")
    headings = re.findall(r"^## (.+?)\s*$", text, re.MULTILINE)
    if headings != REQUIRED_SECTIONS:
        fail(errors, skill_path, f"H2 sections must exactly match the v1 order: {REQUIRED_SECTIONS}")
    section_bodies = {
        heading: body
        for heading, body in re.findall(
            r"^## (.+?)\s*\n(.*?)(?=^## |\Z)", text, re.MULTILINE | re.DOTALL
        )
    }
    design_guidance = section_bodies.get("Read details when needed", "")
    if "workflow-design-and-outcomes.md" not in design_guidance:
        fail(errors, skill_path, "Read details when needed must link the shared workflow design and outcomes guide")
    plan_guidance = section_bodies.get("Plan and AgentWorks tools", "")
    if not re.search(r"\b(?:step|steps|route|routes)\b", plan_guidance, re.IGNORECASE):
        fail(errors, skill_path, "Plan and AgentWorks tools must explain plan steps or routes")
    reporting_guidance = section_bodies.get("Validation and reporting", "")
    if not re.search(r"\bdashboard\b", reporting_guidance, re.IGNORECASE):
        fail(errors, skill_path, "Validation and reporting must describe the reporting dashboard")
    words = word_count_without_frontmatter(text)
    if words > 500:
        fail(errors, skill_path, f"SKILL.md has {words} words; maximum is 500")
    validate_links(skill_path, text, errors)

    for markdown_path in package.rglob("*.md"):
        validate_links(markdown_path, markdown_path.read_text(), errors)
    for json_path in package.rglob("*.json"):
        try:
            json.loads(json_path.read_text())
        except json.JSONDecodeError as exc:
            fail(errors, json_path, f"invalid JSON: {exc}")
    return manifest


def main() -> int:
    errors: list[str] = []
    packages = sorted(
        path.parent
        for path in ROOT.rglob("playbook.json")
        if "templates" not in path.parts
    )
    if not packages:
        print("No playbook packages found", file=sys.stderr)
        return 1
    manifests: list[tuple[Path, dict[str, object]]] = []
    for package in packages:
        manifest = validate_package(package, errors)
        if manifest is not None:
            manifests.append((package / "playbook.json", manifest))

    by_id: dict[object, Path] = {}
    sibling_orders: dict[tuple[tuple[object, ...], object], Path] = {}
    for path, manifest in manifests:
        playbook_id = manifest.get("id")
        if playbook_id in by_id:
            fail(errors, path, f"duplicate id also used by {by_id[playbook_id].relative_to(ROOT.parent)}")
        else:
            by_id[playbook_id] = path

        hierarchy = manifest.get("hierarchy")
        if isinstance(hierarchy, list):
            key = (tuple(hierarchy), manifest.get("order"))
            if key in sibling_orders:
                fail(errors, path, f"duplicate order within hierarchy also used by {sibling_orders[key].relative_to(ROOT.parent)}")
            else:
                sibling_orders[key] = path

    known_ids = set(by_id)
    for path, manifest in manifests:
        relations: list[object] = []
        if manifest.get("setup_playbook"):
            relations.append(manifest["setup_playbook"])
        for field in ("setup_playbooks", "next_playbooks"):
            values = manifest.get(field, [])
            if isinstance(values, list):
                relations.extend(values)
            else:
                fail(errors, path, f"{field} must be a list")
        for relation in relations:
            if relation not in known_ids:
                fail(errors, path, f"unknown related playbook {relation!r}")
    if errors:
        print("Playbook validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"Validated {len(packages)} playbook packages against agentworks-playbook/v1")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
