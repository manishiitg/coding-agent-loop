package step_based_workflow

import (
	"context"
	"fmt"
	"testing"
)

func TestIsWorkflowCancellationErr(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if !isWorkflowCancellationErr(ctx, fmt.Errorf("step failed")) {
			t.Fatal("expected canceled context to be recognized")
		}
	})

	t.Run("wrapped cancellation", func(t *testing.T) {
		err := fmt.Errorf("conversation cancelled after LLM generation: %w", context.Canceled) //nolint:misspell // Match provider output.
		if !isWorkflowCancellationErr(context.Background(), err) {
			t.Fatal("expected wrapped cancellation to be recognized")
		}
	})

	t.Run("deadline remains a failure", func(t *testing.T) {
		if isWorkflowCancellationErr(context.Background(), context.DeadlineExceeded) {
			t.Fatal("deadline exceeded must not be classified as an intentional cancellation")
		}
	})

	t.Run("ordinary error remains a failure", func(t *testing.T) {
		if isWorkflowCancellationErr(context.Background(), fmt.Errorf("provider failed")) {
			t.Fatal("ordinary errors must not be classified as cancellation")
		}
	})
}
