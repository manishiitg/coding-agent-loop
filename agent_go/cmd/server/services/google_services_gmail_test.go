package services

import (
	"context"
	"testing"
)

func TestGoogleCLIAccessIncludesGmailOnlyAfterObservedReadGrant(t *testing.T) {
	connection := GmailConnection{
		ID:              "gmail_001",
		DisplayName:     "Finance",
		Email:           "finance@example.com",
		ClientName:      "finance",
		AuthBackend:     "gog",
		Enabled:         true,
		AllowReadAccess: true,
		Scopes:          []string{GmailReadonlyScope},
	}
	service := &GmailService{config: &GmailConfig{
		Connections:         []GmailConnection{connection},
		DefaultConnectionID: connection.ID,
	}}

	access, err := service.GoogleCLIAccessForConnection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if writable, ok := access.Grants["gmail"]; !ok || writable {
		t.Fatalf("gmail grant = %v, want present and read-only", access.Grants)
	}

	connection.Scopes = []string{"https://www.googleapis.com/auth/gmail.send"}
	service.config.Connections = []GmailConnection{connection}
	if _, err := service.GoogleCLIAccessForConnection(context.Background(), ""); err == nil {
		t.Fatal("stored Gmail read request without Google's observed grant must fail closed")
	}
}
