#!/usr/bin/env python3
"""Exercise onboarding, observed adoption and optional health handoffs."""

import json
import unittest
from pathlib import Path

from validate_customer_success_artifact import (
    InvalidArtifact, validate_adoption, validate_health, validate_onboarding,
)

EXAMPLES = Path(__file__).resolve().parents[1] / "examples"


def fixture(name):
    return json.loads((EXAMPLES / name).read_text())


class CustomerSuccessContractTests(unittest.TestCase):
    def test_good_onboarding_to_health(self):
        onboarding = fixture("onboarding-milestone-register.json")
        adoption = fixture("first-value-readout.json")
        validate_onboarding(onboarding)
        validate_adoption(adoption, onboarding)
        validate_health(fixture("customer-health-brief.json"), onboarding, adoption)

    def test_rejected_first_value_claim(self):
        with self.assertRaises(InvalidArtifact):
            validate_adoption(
                fixture("invalid-first-value-readout.json"),
                fixture("onboarding-milestone-register.json"),
            )

    def test_wrong_account_cannot_reach_first_value(self):
        adoption = fixture("first-value-readout.json")
        adoption["account_id"] = "other-account"
        with self.assertRaisesRegex(InvalidArtifact, "account_id differs"):
            validate_adoption(adoption, fixture("onboarding-milestone-register.json"))


if __name__ == "__main__":
    unittest.main()
