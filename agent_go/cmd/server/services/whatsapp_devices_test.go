package services

import (
	"os"
	"path/filepath"
	"testing"
)

// An account's extra phones are keyed "<user>~<slot>" (the separator never
// survives the filename sanitizer, so the split is unambiguous), stored
// under <user>/devices/<slot>/, and take the next free phone-N name.
func TestWhatsAppDeviceKeysAndSlots(t *testing.T) {
	if key := whatsappServiceKey("user-1", ""); key != "user-1" {
		t.Fatalf("primary key = %q, want the bare user key", key)
	}
	user, slot := splitWhatsAppServiceKey(whatsappServiceKey("user-1", "phone-2"))
	if user != "user-1" || slot != "phone-2" {
		t.Fatalf("split = (%q, %q), want (user-1, phone-2)", user, slot)
	}
	if sanitized := sanitizeWhatsAppFileName("a~b"); sanitized == "a~b" {
		t.Fatal("the device separator survived the filename sanitizer; keys would be ambiguous")
	}

	manager := NewWhatsAppServiceManager(t.TempDir())
	if got := manager.devicePath("user-1", ""); got != filepath.Join(manager.baseDir, "user-1", whatsappUserSessionDBName) {
		t.Fatalf("primary path = %q", got)
	}
	if got := manager.devicePath("user-1", "phone-2"); got != filepath.Join(manager.baseDir, "user-1", "devices", "phone-2", whatsappUserSessionDBName) {
		t.Fatalf("device path = %q", got)
	}

	if got := nextWhatsAppDeviceSlot(nil); got != "phone-2" {
		t.Fatalf("first extra slot = %q, want phone-2", got)
	}
	if got := nextWhatsAppDeviceSlot([]string{"phone-2", "phone-4"}); got != "phone-3" {
		t.Fatalf("next free slot = %q, want phone-3", got)
	}

	// Devices on disk are found even before anything started them.
	for _, slot := range []string{"phone-2", "phone-3"} {
		dir := filepath.Join(manager.baseDir, "user-1", "devices", slot)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, whatsappUserSessionDBName), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(manager.baseDir, "user-1", "devices", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if slots := manager.deviceSlots("user-1"); len(slots) != 2 || slots[0] != "phone-2" || slots[1] != "phone-3" {
		t.Fatalf("device slots = %v, want the two with a session db", slots)
	}
	if slots := manager.deviceSlots("user-2"); len(slots) != 0 {
		t.Fatalf("another user's slots = %v, want none", slots)
	}
}

// A message from an extra phone says which device it arrived on, so the
// turn can run in that phone's own conversation.
func TestWhatsAppManagerTagsTheDeviceOnIncomingMessages(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	var got BotIncomingMessage
	manager.SetMessageHandler(func(msg BotIncomingMessage) { got = msg })

	svc := NewWhatsAppService("")
	manager.configureService(whatsappServiceKey("user-1", "phone-2"), svc)
	svc.messageHandler(BotIncomingMessage{Platform: "whatsapp", ChannelID: "555@s.whatsapp.net", ThreadTS: "555@s.whatsapp.net", Text: "hi"})
	if got.DeviceSlot != "phone-2" || got.ChannelID != "user-1~phone-2|555@s.whatsapp.net" {
		t.Fatalf("message = slot %q channel %q, want phone-2 and a device-keyed channel", got.DeviceSlot, got.ChannelID)
	}

	primary := NewWhatsAppService("")
	manager.configureService("user-1", primary)
	primary.messageHandler(BotIncomingMessage{Platform: "whatsapp", ChannelID: "555@s.whatsapp.net", Text: "hi"})
	if got.DeviceSlot != "" {
		t.Fatalf("primary message slot = %q, want empty", got.DeviceSlot)
	}
}
