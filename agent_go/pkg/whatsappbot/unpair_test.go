package whatsappbot

import (
	"context"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/whatsapptransport"
)

func openUnpairTestStore(t *testing.T) *whatsapptransport.Store {
	t.Helper()
	st, err := whatsapptransport.OpenStore(context.Background(), filepath.Join(t.TempDir(), "session.db"), "test", [3]uint32{1, 0, 0}, false)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func saveUnpairTestDevice(t *testing.T, ctx context.Context, st *whatsapptransport.Store, user string) *store.Device {
	t.Helper()
	dev := st.NewDevice()
	jid := types.JID{User: user, Server: types.DefaultUserServer}
	dev.ID = &jid
	// A fresh device has no account identity until pairing completes; a
	// placeholder satisfies the row's NOT NULL columns offline.
	dev.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{0},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	if err := dev.Save(ctx); err != nil {
		t.Fatalf("device Save: %v", err)
	}
	return dev
}

// A successful whatsmeow Logout already deletes the device row:
// Client.Logout calls Store.Delete, which removes the row and nils the
// device ID. Unpair must treat that as success — a redundant second delete
// fails with ErrDeviceIDMustBeSet and would fail the whole unpair with a
// 500 even though the logout succeeded.
func TestDeleteDeviceIfPresentSkipsPostLogoutDevice(t *testing.T) {
	ctx := context.Background()
	st := openUnpairTestStore(t)
	dev := saveUnpairTestDevice(t, ctx, st, "15551234567")
	// Exactly what whatsmeow's Client.Logout does to the store on success.
	if err := dev.Delete(ctx); err != nil {
		t.Fatalf("device Delete (logout simulation): %v", err)
	}
	// The trap the old unconditional delete fell into: the ID is already nil.
	if err := st.DeleteDevice(ctx, dev); err == nil {
		t.Fatalf("expected direct DeleteDevice after logout to fail (nil device ID)")
	}
	if err := deleteWhatsAppDeviceIfPresent(ctx, st, dev); err != nil {
		t.Fatalf("deleteWhatsAppDeviceIfPresent after logout: %v", err)
	}
}

// When the remote logout was skipped or failed (dead session, offline
// phone), the device row is still present and Unpair must remove it.
func TestDeleteDeviceIfPresentRemovesLiveDevice(t *testing.T) {
	ctx := context.Background()
	st := openUnpairTestStore(t)
	dev := saveUnpairTestDevice(t, ctx, st, "15557654321")
	if err := deleteWhatsAppDeviceIfPresent(ctx, st, dev); err != nil {
		t.Fatalf("deleteWhatsAppDeviceIfPresent: %v", err)
	}
	devices, err := st.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected no devices after delete, got %d", len(devices))
	}
}
