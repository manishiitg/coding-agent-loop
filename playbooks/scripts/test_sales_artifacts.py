#!/usr/bin/env python3
"""Contract checks for the Inbound Lead-to-Meeting Review handoffs."""

from __future__ import annotations

import importlib.util
import json
import unittest
from pathlib import Path


PACKAGE = Path(__file__).resolve().parents[1] / "agentic-engineering-platform/sales/inbound-lead-to-meeting-review"
SPEC = importlib.util.spec_from_file_location("sales_validator", PACKAGE / "scripts/validate_sales_artifact.py")
assert SPEC and SPEC.loader
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


class SalesHandoffContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.qualification = json.loads((PACKAGE / "examples/lead-qualification-brief.json").read_text())
        self.research = json.loads((PACKAGE / "examples/account-research-brief.json").read_text())
        self.followup = json.loads((PACKAGE / "examples/sales-followup-draft.json").read_text())

    def test_valid_required_and_optional_routes(self) -> None:
        validator.validate_qualification(self.qualification)
        validator.validate_research(self.research, self.qualification)
        validator.validate_followup(self.followup, self.qualification, self.research)
        self.followup["source_research_id"] = None
        validator.validate_followup(self.followup, self.qualification, None)

    def test_invalid_source_reference_blocks_handoff(self) -> None:
        invalid = json.loads((PACKAGE / "examples/invalid-lead-qualification-brief.json").read_text())
        with self.assertRaisesRegex(validator.InvalidArtifact, "unknown source"):
            validator.validate_qualification(invalid)

    def test_contact_and_duplicate_states_block_draft(self) -> None:
        self.followup["source_research_id"] = None
        self.qualification["duplicate_state"] = "possible"
        with self.assertRaisesRegex(validator.InvalidArtifact, "duplicate lead"):
            validator.validate_followup(self.followup, self.qualification, None)
        with self.assertRaisesRegex(validator.InvalidArtifact, "duplicate lead"):
            validator.require_ready_for_handoff(validator.validate_qualification(self.qualification))
        self.qualification["duplicate_state"] = "none"
        self.qualification["contact_policy_status"] = "blocked"
        with self.assertRaisesRegex(validator.InvalidArtifact, "contact policy"):
            validator.validate_followup(self.followup, self.qualification, None)

    def test_draft_cannot_be_presented_as_sent_or_use_other_lead(self) -> None:
        self.followup["source_research_id"] = None
        self.followup["send_state"] = "sent"
        with self.assertRaisesRegex(validator.InvalidArtifact, "unsent draft"):
            validator.validate_followup(self.followup, self.qualification, None)
        self.followup["send_state"] = "not_sent"
        self.followup["lead_id"] = "another-lead"
        with self.assertRaisesRegex(validator.InvalidArtifact, "different lead"):
            validator.validate_followup(self.followup, self.qualification, None)

    def test_optional_research_must_match_the_qualified_account(self) -> None:
        self.research["company_domain"] = "different-company.example"
        with self.assertRaisesRegex(validator.InvalidArtifact, "company_domain differs"):
            validator.validate_research(self.research, self.qualification)


if __name__ == "__main__":
    unittest.main()
