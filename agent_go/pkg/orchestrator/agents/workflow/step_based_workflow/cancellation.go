package step_based_workflow

import (
	"context"
	"errors"
	"strings"
)

func isWorkflowCancellationErr(ctx context.Context, err error) bool {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context cancelled") || //nolint:misspell // Provider error text uses both spellings.
		strings.Contains(msg, "conversation canceled") ||
		strings.Contains(msg, "conversation cancelled") //nolint:misspell // Provider error text uses both spellings.
}
