package services

import (
	"strings"
	"testing"
)

// stripCodeFencesForAssert removes ```…``` blocks so tests only assert on
// prose, not on content the formatter intentionally preserves verbatim.
func stripCodeFencesForAssert(s string) string {
	return waCodeFence.ReplaceAllString(s, "")
}

// TestWhatsAppFormatterRealistic exercises the formatter against the kinds of
// markdown we expect the agent to emit. It doesn't assert exact output —
// formatting evolves — it just prints before/after so a human can spot-check
// that what WhatsApp receives is sensible. Run with:
//
//	go test -v -run TestWhatsAppFormatterRealistic ./cmd/server/services/
func TestWhatsAppFormatterRealistic(t *testing.T) {
	f := WhatsAppFormatter{}

	samples := []struct {
		name string
		in   string
	}{
		{
			name: "headers + bold + bullets",
			in: `## Summary

Here's what I found:

- **Status**: the service is up
- **Latency**: 230ms p99 (last 15 min)
- **Errors**: 3 in the last hour, all 5xx

### Recommendation

Check the database pool size. **Details**:

- Connection pool is at 80% capacity
- No obvious memory pressure`,
		},
		{
			name: "markdown links",
			in:   `See the [runbook](https://runbook.example.com/db-pool) for details. You can also check [grafana](https://grafana.example.com/d/db-pool) for live metrics.`,
		},
		{
			name: "nested formatting",
			in:   `**Note:** the *service* is running `,
		},
		{
			name: "code block preserved",
			in: `To fix this, run:

` + "```bash" + `
systemctl restart postgresql
# **this shouldn't transform**
` + "```" + `

Then check the logs.`,
		},
		{
			name: "markdown table",
			in: `| Service | Status | Latency |
|---------|--------|---------|
| api     | up     | 230ms   |
| db      | up     | 12ms    |
| cache   | down   | —       |

Details above.`,
		},
	}

	for _, tc := range samples {
		t.Run(tc.name, func(t *testing.T) {
			out := f.FormatMessage(tc.in)
			t.Logf("\n=== INPUT ===\n%s\n=== OUTPUT ===\n%s\n===========", tc.in, out)
			// Soft checks — nothing should survive from markdown that WhatsApp
			// renders literally. We only check content OUTSIDE code fences;
			// anything inside ``` is expected to be preserved verbatim.
			outside := stripCodeFencesForAssert(out)
			if strings.Contains(outside, "**") {
				t.Errorf("output still contains double-asterisk bold: %q", outside)
			}
			if strings.Contains(outside, "](") {
				t.Errorf("output still contains markdown link syntax: %q", outside)
			}
			for _, line := range strings.Split(outside, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
					t.Errorf("output still has markdown header: %q (from %q)", trimmed, tc.name)
				}
			}
		})
	}
}
