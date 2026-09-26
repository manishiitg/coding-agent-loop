#!/usr/bin/env python3
"""Exercise site-bound strategist-to-search artifact joins."""

import json
import unittest
from pathlib import Path

from validate_growth_artifact import InvalidArtifact, validate_brief, validate_search

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


if __name__ == "__main__":
    unittest.main()
