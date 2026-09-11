package services

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWhatsAppManagedChannelIDRoundTrip(t *testing.T) {
	encoded := encodeWhatsAppManagedChannelID("user-1", "12345@s.whatsapp.net")
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(encoded)
	if !ok {
		t.Fatal("expected encoded channel ID to decode")
	}
	if userID != "user-1" {
		t.Fatalf("expected user-1, got %q", userID)
	}
	if chatJID != "12345@s.whatsapp.net" {
		t.Fatalf("expected raw chat JID, got %q", chatJID)
	}
}

func TestWhatsAppManagerNamespacesIncomingMessage(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	var got BotIncomingMessage
	manager.SetMessageHandler(func(msg BotIncomingMessage) {
		got = msg
	})

	svc := NewWhatsAppService("")
	manager.configureService("user-1", svc)
	svc.messageHandler(BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "12345@s.whatsapp.net",
		ThreadTS:  "12345@s.whatsapp.net",
		Text:      "hello",
	})

	if got.ChannelID != "user-1|12345@s.whatsapp.net" {
		t.Fatalf("expected namespaced channel ID, got %q", got.ChannelID)
	}
	if got.ThreadTS != got.ChannelID {
		t.Fatalf("expected thread TS to follow namespaced channel ID, got %q", got.ThreadTS)
	}
}

func TestWhatsAppManagerSessionBindingsAreIsolatedPerAccountAndDevice(t *testing.T) {
	baseDir := t.TempDir()
	manager := NewWhatsAppServiceManager(baseDir)
	keys := []string{"user-a", "user-b", whatsappServiceKey("user-a", "phone-2")}
	for _, key := range keys {
		userKey, slot := splitWhatsAppServiceKey(key)
		dbPath := manager.devicePath(userKey, slot)
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			t.Fatalf("create device directory for %s: %v", key, err)
		}
		svc := NewWhatsAppService(dbPath)
		if err := svc.openMetaStore(context.Background()); err != nil {
			t.Fatalf("open meta store for %s: %v", key, err)
		}
		defer svc.closeMetaStore()
		manager.mu.Lock()
		manager.services[key] = svc
		manager.mu.Unlock()
	}

	chatJID := "15551234567@s.whatsapp.net"
	bindings := map[string]string{
		"user-a":                                "session-user-a-primary",
		"user-b":                                "session-user-b-primary",
		whatsappServiceKey("user-a", "phone-2"): "session-user-a-phone-2",
	}
	for key, sessionID := range bindings {
		threadID := ThreadID{Platform: "whatsapp", ChannelID: encodeWhatsAppManagedChannelID(key, chatJID)}
		if err := manager.SaveBotSessionBinding(context.Background(), threadID, BotSessionBinding{
			SessionID: sessionID,
			RouteKey:  "wf|workspace|run",
			UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("save binding for %s: %v", key, err)
		}
	}
	for key, wantSessionID := range bindings {
		threadID := ThreadID{Platform: "whatsapp", ChannelID: encodeWhatsAppManagedChannelID(key, chatJID)}
		got, ok, err := manager.LoadBotSessionBinding(context.Background(), threadID, "wf|workspace|run")
		if err != nil {
			t.Fatalf("load binding for %s: %v", key, err)
		}
		if !ok || got.SessionID != wantSessionID {
			t.Fatalf("binding for %s = (%+v, %v), want %s", key, got, ok, wantSessionID)
		}
	}
}

// PLAT (WhatsApp routing race): two concurrent routing writes for the same
// account must not interleave. LockAccountRouting is the fix; this proves it
// actually serializes rather than just existing as a no-op.
func TestLockAccountRoutingSerializesConcurrentCallers(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())

	var active int32
	var maxObservedConcurrency int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := manager.LockAccountRouting("user-1")
			defer unlock()
			n := atomic.AddInt32(&active, 1)
			for {
				max := atomic.LoadInt32(&maxObservedConcurrency)
				if n <= max || atomic.CompareAndSwapInt32(&maxObservedConcurrency, max, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()

	if maxObservedConcurrency != 1 {
		t.Fatalf("expected at most 1 concurrent holder of the same account's routing lock, observed %d", maxObservedConcurrency)
	}
}

// A different account's routing lock must not block on this one -- otherwise
// every account's routing writes would serialize behind a single global lock.
func TestLockAccountRoutingDoesNotBlockAcrossAccounts(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())

	unlockA := manager.LockAccountRouting("user-a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := manager.LockAccountRouting("user-b")
		defer unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("LockAccountRouting for a different account blocked on user-a's lock")
	}
}

// PLAT (WhatsApp unpair/lookup race): lockServiceKey is what serviceForKey's
// create path and UnpairDevice share to avoid the recreate-after-delete
// race. Proves it serializes for the same key and doesn't cross-block
// unrelated keys.
func TestLockServiceKeySerializesSameKeyAndAllowsOtherKeys(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())

	unlockA := manager.lockServiceKey("user-1|")

	blockedSameKey := make(chan struct{})
	go func() {
		unlock := manager.lockServiceKey("user-1|")
		defer unlock()
		close(blockedSameKey)
	}()
	select {
	case <-blockedSameKey:
		t.Fatal("expected lockServiceKey(\"user-1|\") to block while already held")
	case <-time.After(50 * time.Millisecond):
	}

	otherKeyDone := make(chan struct{})
	go func() {
		unlock := manager.lockServiceKey("user-2|")
		defer unlock()
		close(otherKeyDone)
	}()
	select {
	case <-otherKeyDone:
	case <-time.After(time.Second):
		t.Fatal("lockServiceKey for a different key blocked on user-1|'s lock")
	}

	unlockA()
	select {
	case <-blockedSameKey:
	case <-time.After(time.Second):
		t.Fatal("expected the same-key waiter to proceed once the first holder unlocked")
	}
}

// PLAT (SetDeviceLabel network bootstrap): saving a label for a device that
// isn't already resident in memory must not require the full StartListening
// bootstrap (a real WhatsApp connection) -- it should go through
// SetDeviceLabelOffline instead, leaving nothing resident.
func TestManagerSetDeviceLabelDoesNotCreateResidentServiceWhenNotCached(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())

	if err := manager.SetDeviceLabel(context.Background(), "user-1", "", "Primary Phone"); err != nil {
		t.Fatalf("SetDeviceLabel failed: %v", err)
	}

	manager.mu.RLock()
	residentCount := len(manager.services)
	manager.mu.RUnlock()
	if residentCount != 0 {
		t.Fatalf("expected no resident service after an offline label write, got %d", residentCount)
	}
}

// The already-resident path must still work and keep the in-memory label in
// sync (it takes a different branch than the offline path above).
func TestManagerSetDeviceLabelUsesLiveInstanceWhenResident(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	userKey, err := whatsappUserKey("user-1")
	if err != nil {
		t.Fatalf("whatsappUserKey failed: %v", err)
	}
	key := whatsappServiceKey(userKey, "")

	svc := NewWhatsAppService("")
	manager.configureService(key, svc)
	manager.mu.Lock()
	manager.services[key] = svc
	manager.mu.Unlock()

	// The resident instance has no metaDB (dbPath is ""), so a live-path call
	// is expected to fail on the actual write -- what matters here is that it
	// took the resident branch (attempted the live instance) rather than
	// silently falling through to the offline path for a key that IS cached.
	if err := manager.SetDeviceLabel(context.Background(), "user-1", "", "Primary Phone"); err == nil {
		t.Fatal("expected an error from the resident instance's unopened meta store, got nil")
	}
}
