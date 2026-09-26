package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// One person, one WhatsApp: an account's service is keyed by the account and
// stored at <user>/session.db; ids minted for a former extra phone name no
// service.
func TestWhatsAppOnePhonePerAccount(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	if got := manager.devicePath("user-1"); got != filepath.Join(manager.baseDir, "user-1", whatsappUserSessionDBName) {
		t.Fatalf("session path = %q", got)
	}
	if _, err := manager.serviceForEncodedKey(context.Background(), "user-1~phone-2"); err == nil {
		t.Fatal("a former extra phone's key still resolved to a service")
	}
	if sanitized := sanitizeWhatsAppFileName("a~b"); sanitized == "a~b" {
		t.Fatal("the old device separator survived the filename sanitizer; a user key could look like an extra phone")
	}
}

// Extra phones linked before one-phone-per-account are logged out and
// removed at startup; one that cannot be logged out now keeps its files for
// the next start.
func TestRetireExtraWhatsAppDevices(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	devices := filepath.Join(manager.baseDir, "user-1", "devices")
	for _, slot := range []string{"phone-2", "phone-3"} {
		if err := os.MkdirAll(filepath.Join(devices, slot), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(devices, slot, whatsappUserSessionDBName), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(devices, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	var loggedOut []string
	manager.logoutRetiredDevice = func(_ context.Context, dbPath string) error {
		slot := filepath.Base(filepath.Dir(dbPath))
		if slot == "phone-3" {
			return errors.New("offline")
		}
		loggedOut = append(loggedOut, slot)
		return nil
	}
	manager.retireExtraWhatsAppDevices(context.Background(), "user-1")

	if len(loggedOut) != 1 || loggedOut[0] != "phone-2" {
		t.Fatalf("logged out %v, want phone-2", loggedOut)
	}
	if _, err := os.Stat(filepath.Join(devices, "phone-2")); !os.IsNotExist(err) {
		t.Fatal("a logged-out extra phone kept its files")
	}
	if _, err := os.Stat(filepath.Join(devices, "phone-3", whatsappUserSessionDBName)); err != nil {
		t.Fatal("an extra phone that could not be logged out lost its session (no retry possible)")
	}
	if _, err := os.Stat(filepath.Join(devices, "empty")); !os.IsNotExist(err) {
		t.Fatal("an empty extra-phone folder was left behind")
	}
}

// Messages from the account's phone carry the account's managed channel id.
func TestWhatsAppManagerChannelIDsNameTheAccount(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	var got BotIncomingMessage
	manager.SetMessageHandler(func(msg BotIncomingMessage) { got = msg })
	svc := NewWhatsAppService("")
	manager.configureService("user-1", svc)
	svc.messageHandler(BotIncomingMessage{Platform: "whatsapp", ChannelID: "555@s.whatsapp.net", ThreadTS: "555@s.whatsapp.net", Text: "hi"})
	if got.ChannelID != "user-1|555@s.whatsapp.net" {
		t.Fatalf("channel = %q, want the account-keyed channel", got.ChannelID)
	}
}
