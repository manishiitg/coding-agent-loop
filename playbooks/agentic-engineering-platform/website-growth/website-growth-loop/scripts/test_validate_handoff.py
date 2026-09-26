#!/usr/bin/env python3
"""Exercise site-bound strategist-to-search artifact joins."""

import json
import unittest
from pathlib import Path

from validate_growth_artifact import InvalidArtifact, validate_brief, validate_search, validate_content, validate_page

EXAMPLES = Path(__file__).resolve().parents[1] / "examples"


def fixture(name):
    return json.loads((EXAMPLES / name).read_text())


class WebsiteGrowthContractTests(unittest.TestCase):
    def test_good_priority_to_search_handoff(self):
        brief = fixture("growth-priority-brief.json")
        validate_brief(brief)
        validate_search(fixture("search-opportunity-list.json"), brief)

    def test_other_site_search_is_rejected(self):
        with self.assertRaises(InvalidArtifact):
            validate_search(
                fixture("invalid-search-opportunity-list.json"),
                fixture("growth-priority-brief.json"),
            )

    def test_page_must_have_been_inspected(self):
        brief = fixture("growth-priority-brief.json")
        brief["findings"][0]["page_url"] = "https://arbordesk.example/uninspected"
        with self.assertRaisesRegex(InvalidArtifact, "was not inspected"):
            validate_brief(brief)

    def test_approved_content_to_reviewable_page_handoff(self):
        brief = fixture("growth-priority-brief.json")
        search = fixture("search-opportunity-list.json")
        content = fixture("content-brief.json")
        validate_content(content, search, brief)
        validate_page(fixture("reviewable-page-draft.json"), content, search, brief)

    def test_unapproved_hypothesis_cannot_become_a_content_brief(self):
        with self.assertRaises(InvalidArtifact):
            validate_content(fixture("invalid-content-brief.json"), fixture("search-opportunity-list.json"), fixture("growth-priority-brief.json"))

    def test_owner_approval_is_required_for_sourced_content(self):
        content = fixture("content-brief.json")
        content["approved_opportunity"]["decision"] = "pending"
        with self.assertRaisesRegex(InvalidArtifact, "owner approval"):
            validate_content(content, fixture("search-opportunity-list.json"), fixture("growth-priority-brief.json"))

    def test_unverified_claim_and_false_publication_block_page(self):
        with self.assertRaises(InvalidArtifact):
            validate_page(fixture("invalid-page-draft.json"), fixture("content-brief.json"), fixture("search-opportunity-list.json"), fixture("growth-priority-brief.json"))

    def test_unverified_claim_cannot_appear_in_an_unpublished_draft(self):
        page = fixture("reviewable-page-draft.json")
        page["sections"][0]["claim_ids"] = ["C-002"]
        with self.assertRaisesRegex(InvalidArtifact, "unverified claim"):
            validate_page(page, fixture("content-brief.json"), fixture("search-opportunity-list.json"), fixture("growth-priority-brief.json"))

    def test_wrong_search_artifact_stops_content_handoff(self):
        search = fixture("search-opportunity-list.json")
        search["artifact_id"] = "different-search-run"
        with self.assertRaisesRegex(InvalidArtifact, "different search artifact"):
            validate_content(fixture("content-brief.json"), search, fixture("growth-priority-brief.json"))


if __name__ == "__main__":
    unittest.main()
