package agent

import (
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestPlainTextFromPartsDoesNotDoubleWrapPreviousToolResult(t *testing.T) {
	const existing = `[Previous tool result: read -> {"status":"ok"}]`
	got := plainTextFromParts("[Previous tool result]", []llmtypes.ContentPart{
		llmtypes.TextContent{Text: existing},
	})
	if got != existing {
		t.Fatalf("plainTextFromParts() = %q, want existing marker unchanged", got)
	}
}
