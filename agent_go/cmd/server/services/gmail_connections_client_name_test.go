package services

import (
	"context"
	"strings"
	"testing"
)

// CreateConnection validates display_name and client_name before ever
// touching config (GetConfig/SaveConfig), so these negative paths are pure
// unit tests with no workspace API involved.

func TestCreateConnectionRequiresClientName(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
	g := &GmailService{}
	_, err := g.CreateConnection(context.Background(), GmailConnectionInput{DisplayName: "Ops"})
	if err == nil {
		t.Fatal("expected an error when client_name is missing")
	}
	if !strings.Contains(err.Error(), "client_name") {
		t.Fatalf("expected the error to name client_name, got: %v", err)
	}
}

func TestCreateConnectionValidatesClientNameExists(t *testing.T) {
	t.Setenv("GMAIL_OAUTH_CLIENTS_DIR", t.TempDir())
	g := &GmailService{}
	_, err := g.CreateConnection(context.Background(), GmailConnectionInput{
		DisplayName: "Ops",
		ClientName:  "does-not-exist",
	})
	if err == nil {
		t.Fatal("expected an error when client_name names an unregistered client")
	}
}

func TestGmailAuthKnobsEqualCoversClientName(t *testing.T) {
	base := GmailConnection{ConfigHome: "/a", CredentialsFile: "/b", ClientName: "primary"}
	same := base
	if !gmailAuthKnobsEqual(base, same) {
		t.Error("identical connections should compare equal")
	}
	renamedClient := base
	renamedClient.ClientName = "secondary"
	if gmailAuthKnobsEqual(base, renamedClient) {
		t.Error("a changed client_name must invalidate the cached auth status")
	}
}
