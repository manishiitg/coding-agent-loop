package agentsession

import (
	"testing"

	"github.com/manishiitg/mcpagent/llm"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
)

func TestInteractiveOwnerClosersCoverPersistentProviders(t *testing.T) {
	// Every active tmux provider with a persistent session must have an
	// owner-close binding: CloseAllInteractiveSessions drops its
	// bookkeeping first, so a missing entry silently leaks the session.
	for _, contract := range llmproviders.CodingAgentProviderContracts() {
		if contract.Deprecated || contract.Transport != llmproviders.CodingAgentTransportTmux || !contract.UsesPersistentSession {
			continue
		}
		if _, ok := interactiveOwnerClosers[contract.Provider]; !ok {
			t.Errorf("persistent provider %s has no owner-close binding", contract.Provider)
		}
	}
}

func TestCloseInteractiveOwnerDispatchesPerProvider(t *testing.T) {
	// Actual dispatch through the table with an injected recorder: every
	// bound provider's closer runs with the owner's id and reason.
	type call struct {
		id     string
		reason string
	}
	var calls []call
	saved := interactiveOwnerClosers
	defer func() { interactiveOwnerClosers = saved }()
	recording := make(map[llm.Provider]func(id, reason string), len(saved))
	for provider := range saved {
		recording[provider] = func(id, reason string) {
			calls = append(calls, call{id: id, reason: reason})
		}
	}
	interactiveOwnerClosers = recording

	for provider := range saved {
		calls = nil
		closeInteractiveOwner("owner-1", provider, "reset")
		if len(calls) != 1 || calls[0].id != "owner-1" || calls[0].reason != "reset" {
			t.Fatalf("%s dispatch calls = %+v, want one (owner-1, reset)", provider, calls)
		}
	}
}
