# Genome QA Report

## Page Summary
- Page: genome
- Total Cases: 76
- Passed: 55
- Failed: 17
- Role-Skips: 4
- Pass Rate: 76.4%
- Verdict: pass

## Test Results by Category
| Category | Executed | Pass | Fail | Skip | Role-Skip |
|---|---:|---:|---:|---:|---:|
| Functional | 29 | 23 | 6 | 0 | 0 |
| Error Handling | 11 | 4 | 5 | 0 | 2 |
| Security | 15 | 8 | 5 | 0 | 2 |
| Performance | 4 | 4 | 0 | 0 | 0 |
| Accessibility | 8 | 7 | 1 | 0 | 0 |
| UI/UX | 6 | 6 | 0 | 0 | 0 |
| Compatibility | 3 | 3 | 0 | 0 | 0 |

## Performance Metrics
- Genome page load: 552 ms
- Genome list API: 687 ms
- Genome table render: 687 ms
- Genome save response: 556 ms
- Threshold rating: good(<1s)

## Bugs Found
- TC-GENOME-006: medium - Contract type sorting did not reorder rows as expected.
- TC-GENOME-010: high - Duplicate action opened the editor without creating a copy-named genome.
- TC-GENOME-011: medium - CSV export did not produce a downloadable file.
- TC-GENOME-012: medium - Generate Playbook returned to the list page without confirmation.
- TC-GENOME-019: high - Share settings did not persist after save.
- TC-GENOME-026: low - Empty company validation text mismatched the expected copy.
- TC-GENOME-028: medium - Network failure surfaced raw `Failed to fetch` text.
- TC-GENOME-029: medium - Invalid genome URL surfaced fetch failure text instead of `Genome not found`.
- TC-GENOME-033: low - No search/filter input exists on the genome page, so SQL-injection coverage could not be exercised.
- TC-GENOME-043: medium - Enter in the share modal added the company but did not submit and close.
- TC-GENOME-051: low - Empty-state coverage could not be reached with the current seeded data.
- TC-GENOME-052: medium - The Name field accepted 256 characters without validation or truncation.
- TC-GENOME-061: medium - Unauthenticated API access returned unexpected error text.
- TC-GENOME-062: medium - Share-settings GET returned a generic 403 message instead of the expected copy.
- TC-GENOME-063: medium - Share-settings PATCH returned a generic 403 message instead of the expected copy.
- TC-GENOME-065: high - Genome editor accepted a 256-character name on save.
- TC-GENOME-068: medium - Access escalation was blocked by a generic 403 before the expected validator ran.

## Recommendations
1. Fix the failing genome workflows and copy issues first.
2. Add a searchable/filterable genome surface for security coverage.
3. Seed a blank genome tab for empty-state coverage.
4. Normalize role-specific API error messages.
