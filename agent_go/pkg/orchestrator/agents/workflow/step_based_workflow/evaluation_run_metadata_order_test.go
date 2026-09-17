package step_based_workflow

import (
	"os"
	"strings"
	"testing"
)

func TestSuccessfulTargetRunIsFinalizedBeforeAutoEvaluation(t *testing.T) {
	// Regression for LinkedIn finding 9495aef3dab65c42. This order is the
	// contract: evaluators read target run_metadata.json, so it must already
	// contain completed_at before MaybeRunAutoEvaluation starts.
	source, err := os.ReadFile("controller_batch_execution.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	successAt := strings.Index(text, "Batch execution: group %s completed successfully")
	if successAt < 0 {
		t.Fatal("successful group block not found")
	}
	tail := text[successAt:]
	finalizeAt := strings.Index(tail, "hcpo.finalizeRunMetadata(ctx, runFolder, \"completed\"")
	evaluateAt := strings.Index(tail, "hcpo.MaybeRunAutoEvaluation(ctx)")
	if finalizeAt < 0 || evaluateAt < 0 {
		t.Fatalf("finalize/evaluation calls missing after successful execution: finalize=%d evaluate=%d", finalizeAt, evaluateAt)
	}
	if finalizeAt > evaluateAt {
		t.Fatalf("target run is finalized after evaluation starts: finalize=%d evaluate=%d", finalizeAt, evaluateAt)
	}
}
