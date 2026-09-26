package step_based_workflow

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
)

// An attached secret reaches the builder's live shell in the same turn; a
// name without a stored value is not pushed as an empty variable.
func TestNotifySecretsAttachedPushesValuesAndRemovals(t *testing.T) {
	var gotSet map[string]string
	var gotRemoved []string
	notifySecretsAttached(func(set map[string]string, removed []string) {
		gotSet, gotRemoved = set, removed
	}, []orchestrator.SecretEntry{{Name: "SENTRY_AUTH_TOKEN", Value: "sntrys_x"}, {Name: "UNSET", Value: ""}}, []string{"OLD"})
	if gotSet["SENTRY_AUTH_TOKEN"] != "sntrys_x" || len(gotSet) != 1 {
		t.Fatalf("set = %v", gotSet)
	}
	if len(gotRemoved) != 1 || gotRemoved[0] != "OLD" {
		t.Fatalf("removed = %v", gotRemoved)
	}
	notifySecretsAttached(nil, nil, nil) // no callback: no-op
}
