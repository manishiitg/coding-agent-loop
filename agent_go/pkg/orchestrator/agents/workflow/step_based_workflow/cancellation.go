package step_based_workflow

import (
	"context"
	"errors"
	"strings"
)

func isWorkflowCancellationErr(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context cancelled") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "conversation cancelled")
}
