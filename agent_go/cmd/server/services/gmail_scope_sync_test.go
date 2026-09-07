package services

import (
	"context"
	"testing"
)

// syncConnectionScopes returns early (no SaveConfig, no workspace API call)
// when there's nothing to persist — these stay pure unit tests.

func TestSyncConnectionScopesNoopOnEmptyID(t *testing.T) {
	g := &GmailService{}
	g.syncConnectionScopes(context.Background(), "", []string{"a"})
	// No panic, no network call: success.
}

func TestSyncConnectionScopesNoopOnEmptyScopes(t *testing.T) {
	g := &GmailService{}
	g.syncConnectionScopes(context.Background(), "gmail_001", nil)
}

func TestSyncConnectionScopesNoopWhenConnectionNotFound(t *testing.T) {
	g := &GmailService{config: &GmailConfig{}}
	g.syncConnectionScopes(context.Background(), "gmail_999", []string{"a"})
}

func TestSyncConnectionScopesNoopWhenUnchanged(t *testing.T) {
	g := &GmailService{config: &GmailConfig{
		Connections: []GmailConnection{{ID: "gmail_001", Scopes: []string{"b", "a"}}},
	}}
	// Same set, different input order — must compare sorted, not skip due to
	// order alone looking different.
	g.syncConnectionScopes(context.Background(), "gmail_001", []string{"a", "b"})
}

func TestStringSlicesEqual(t *testing.T) {
	if !stringSlicesEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("expected equal")
	}
	if stringSlicesEqual([]string{"a"}, []string{"a", "b"}) {
		t.Error("expected unequal on length")
	}
	if stringSlicesEqual([]string{"a", "c"}, []string{"a", "b"}) {
		t.Error("expected unequal on content")
	}
}
