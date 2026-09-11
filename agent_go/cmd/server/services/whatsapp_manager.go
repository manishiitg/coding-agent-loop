package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const whatsappUserSessionDBName = "session.db"

// WhatsAppServiceManager owns one WhatsAppService per workspace user while
// presenting a single "whatsapp" connector to BotConversationManager.
type WhatsAppServiceManager struct {
	baseDir string

	mu               sync.RWMutex
	services         map[string]*WhatsAppService
	messageHandler   BotMessageHandler
	interaction      BotInteractionHandler
	statusProvider   BotThreadStatusFunc
	profileRouter    ProfileRouteResolver
	voiceTranscriber VoiceTranscriberFunc
	started          bool

	// routingLocks serializes the read-primary-then-fan-out-to-every-device
	// sequence per account. Each device's own SetRouting call is locked
	// individually, but nothing spanned the whole sequence, so two concurrent
	// routing writes for the same account (double-click, two open tabs, or an
	// explicit PUT racing the throttled GET-time default-route sync) could
	// interleave their per-device writes and leave devices with divergent
	// routing maps -- reproducing the exact routes-differ-per-phone symptom
	// this endpoint exists to prevent. Keyed by userID, one *sync.Mutex each.
	routingLocks sync.Map

	// keyLocks serializes "resolve or create this service" (serviceForKey's
	// create path) against "tear this service down" (UnpairDevice) for the
	// exact same key. Without this, serviceForKey's existence check (an RLock
	// read) can find nothing, then -- while it's off constructing a new
	// WhatsAppService and starting it, entirely outside any lock -- a
	// concurrent UnpairDevice can delete the map entry and remove the
	// on-disk directory; serviceForKey's later re-check under m.mu still
	// finds the key absent (Unpair already removed it) and proceeds to
	// insert and start the new service anyway, recreating the sqlite
	// file/directory for a device that was supposed to be gone. A plain
	// tombstone won't do: slot names are reused after removal
	// (nextWhatsAppDeviceSlot fills gaps from the current on-disk listing),
	// so this must be a transient per-operation lock, not a persisted flag.
	keyLocks sync.Map
}

// LockAccountRouting acquires this account's routing lock and returns the
// unlock function. Callers must hold it for the entire read-then-fan-out
// sequence (read the primary's routing, write it to every linked device),
// not just the write step.
func (m *WhatsAppServiceManager) LockAccountRouting(userID string) func() {
	lockIface, _ := m.routingLocks.LoadOrStore(userID, &sync.Mutex{})
	lock := lockIface.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

// lockServiceKey serializes create/teardown for one exact service key
// (userKey+slot) between serviceForKey's create path and UnpairDevice.
func (m *WhatsAppServiceManager) lockServiceKey(key string) func() {
	lockIface, _ := m.keyLocks.LoadOrStore(key, &sync.Mutex{})
	lock := lockIface.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

// ProfileRouteResolver resolves a user's default product @token (see
// WhatsAppService.SetProfileRouter); set by the server layer, which owns
// profiles.
type ProfileRouteResolver func(ctx context.Context, userID, token string) (*ProfileRoute, error)

// SetProfileRouter installs the product @token resolver for every user's
// service, present and future.
func (m *WhatsAppServiceManager) SetProfileRouter(resolver ProfileRouteResolver) {
	m.mu.Lock()
	m.profileRouter = resolver
	services := make([]*WhatsAppService, 0, len(m.services))
	keys := make([]string, 0, len(m.services))
	for key, svc := range m.services {
		services = append(services, svc)
		keys = append(keys, key)
	}
	m.mu.Unlock()
	for i, svc := range services {
		m.installProfileRouter(keys[i], svc)
	}
}

// SetVoiceTranscriber installs the on-device speech transcriber for every
// device's service, present and future.
func (m *WhatsAppServiceManager) SetVoiceTranscriber(fn VoiceTranscriberFunc) {
	m.mu.Lock()
	m.voiceTranscriber = fn
	services := make([]*WhatsAppService, 0, len(m.services))
	for _, svc := range m.services {
		services = append(services, svc)
	}
	m.mu.Unlock()
	for _, svc := range services {
		svc.SetVoiceTranscriber(fn)
	}
}

func (m *WhatsAppServiceManager) installProfileRouter(key string, svc *WhatsAppService) {
	svc.SetProfileRouter(func(ctx context.Context, token string) (*ProfileRoute, error) {
		m.mu.RLock()
		resolver := m.profileRouter
		m.mu.RUnlock()
		if resolver == nil {
			return nil, nil
		}
		return resolver(ctx, whatsappOwnerUserID(svc, key), token)
	})
}

// whatsappOwnerUserID is the account a device belongs to: its owner binding,
// or the user part of its service key before anyone has claimed it.
func whatsappOwnerUserID(svc *WhatsAppService, key string) string {
	if owner := svc.GetOwner(); owner != nil && strings.TrimSpace(owner.UserID) != "" {
		return owner.UserID
	}
	userKey, _ := splitWhatsAppServiceKey(key)
	return userKey
}

func NewWhatsAppServiceManager(baseDir string) *WhatsAppServiceManager {
	return &WhatsAppServiceManager{
		baseDir:  baseDir,
		services: make(map[string]*WhatsAppService),
	}
}

func (m *WhatsAppServiceManager) Name() string { return "whatsapp" }

func (m *WhatsAppServiceManager) SupportsThreads() bool { return false }

func (m *WhatsAppServiceManager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

func (m *WhatsAppServiceManager) StartListening(ctx context.Context) error {
	if m.baseDir == "" {
		return fmt.Errorf("whatsapp: session directory not configured")
	}
	if err := os.MkdirAll(m.baseDir, 0o700); err != nil {
		return fmt.Errorf("whatsapp: mkdir session directory: %w", err)
	}

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return fmt.Errorf("whatsapp: read session directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		userKey := entry.Name()
		if _, err := os.Stat(m.devicePath(userKey, "")); err == nil {
			if _, err := m.serviceForKey(ctx, userKey, ""); err != nil {
				log.Printf("[WHATSAPP] Failed to start service for user %s: %v", userKey, err)
			}
		}
		for _, slot := range m.deviceSlots(userKey) {
			if _, err := m.serviceForKey(ctx, userKey, slot); err != nil {
				log.Printf("[WHATSAPP] Failed to start device %s for user %s: %v", slot, userKey, err)
			}
		}
	}
	return nil
}

func (m *WhatsAppServiceManager) StopListening() {
	m.mu.RLock()
	services := make([]*WhatsAppService, 0, len(m.services))
	for _, svc := range m.services {
		services = append(services, svc)
	}
	m.mu.RUnlock()
	for _, svc := range services {
		svc.StopListening()
	}
}

// ServiceForUser is the account's primary WhatsApp device: the one the
// account's default profile, routing and notifications live on.
func (m *WhatsAppServiceManager) ServiceForUser(ctx context.Context, userID, email, username string) (*WhatsAppService, error) {
	_, _ = email, username
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return nil, err
	}
	return m.serviceForKey(ctx, userKey, "")
}

// An account can link more than one phone (a second parent's): each extra
// device is its own whatsmeow session with its own pairing, stored under
// <user>/devices/<slot>/ and keyed "<user>~<slot>" in the service map and in
// managed channel ids. The primary device keeps its original key and path.
const (
	whatsappDeviceKeySeparator = "~"
	whatsappDevicesDirName     = "devices"
)

// WhatsAppDevice describes one linked (or linking) phone of an account.
type WhatsAppDevice struct {
	Slot        string    `json:"slot"`
	Label       string    `json:"label,omitempty"`
	Paired      bool      `json:"paired"`
	Connected   bool      `json:"connected"`
	OwnJID      string    `json:"own_jid,omitempty"`
	QRAvailable bool      `json:"qr_available"`
	QRExpiresAt time.Time `json:"-"`
}

func whatsappUserKey(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("whatsapp: user ID required")
	}
	key := sanitizeWhatsAppFileName(userID)
	if key == "" {
		return "", fmt.Errorf("whatsapp: invalid user ID %q", userID)
	}
	return key, nil
}

func whatsappServiceKey(userKey, slot string) string {
	if slot == "" {
		return userKey
	}
	return userKey + whatsappDeviceKeySeparator + slot
}

// splitWhatsAppServiceKey undoes whatsappServiceKey. The separator never
// appears in a sanitized user key, so the split is unambiguous.
func splitWhatsAppServiceKey(key string) (userKey, slot string) {
	userKey, slot, _ = strings.Cut(key, whatsappDeviceKeySeparator)
	return userKey, slot
}

func (m *WhatsAppServiceManager) devicePath(userKey, slot string) string {
	if slot == "" {
		return filepath.Join(m.baseDir, userKey, whatsappUserSessionDBName)
	}
	return filepath.Join(m.baseDir, userKey, whatsappDevicesDirName, slot, whatsappUserSessionDBName)
}

// ServiceForDevice is one linked phone of the account; slot "" is the primary.
func (m *WhatsAppServiceManager) ServiceForDevice(ctx context.Context, userID, slot string) (*WhatsAppService, error) {
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return nil, err
	}
	return m.serviceForKey(ctx, userKey, slot)
}

// serviceForEncodedKey resolves the service a managed channel id names.
func (m *WhatsAppServiceManager) serviceForEncodedKey(ctx context.Context, key string) (*WhatsAppService, error) {
	userKey, slot := splitWhatsAppServiceKey(key)
	return m.serviceForKey(ctx, userKey, slot)
}

// deviceSlots lists the account's extra devices, on disk and in memory.
func (m *WhatsAppServiceManager) deviceSlots(userKey string) []string {
	slots := map[string]bool{}
	if entries, err := os.ReadDir(filepath.Join(m.baseDir, userKey, whatsappDevicesDirName)); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				if _, err := os.Stat(filepath.Join(m.baseDir, userKey, whatsappDevicesDirName, entry.Name(), whatsappUserSessionDBName)); err == nil {
					slots[entry.Name()] = true
				}
			}
		}
	}
	m.mu.RLock()
	for key := range m.services {
		if user, slot := splitWhatsAppServiceKey(key); user == userKey && slot != "" {
			slots[slot] = true
		}
	}
	m.mu.RUnlock()
	out := make([]string, 0, len(slots))
	for slot := range slots {
		out = append(out, slot)
	}
	sort.Strings(out)
	return out
}

// nextWhatsAppDeviceSlot names the next extra device: phone-2, phone-3, …
// skipping slots already in use.
func nextWhatsAppDeviceSlot(taken []string) string {
	used := map[string]bool{}
	for _, slot := range taken {
		used[slot] = true
	}
	for n := 2; ; n++ {
		if slot := fmt.Sprintf("phone-%d", n); !used[slot] {
			return slot
		}
	}
}

func whatsappDeviceInfo(slot string, svc *WhatsAppService) WhatsAppDevice {
	device := WhatsAppDevice{Slot: slot, Paired: svc.IsPaired(), Connected: svc.IsConnected()}
	if label := svc.DeviceLabel(); label != "" {
		device.Label = label
	}
	if jid := svc.OwnJID(); !jid.IsEmpty() {
		device.OwnJID = jid.String()
	}
	if code, expires := svc.GetQR(); code != "" && !expires.IsZero() {
		device.QRAvailable = true
		device.QRExpiresAt = expires
	}
	return device
}

// Devices lists the account's phones, primary first.
func (m *WhatsAppServiceManager) Devices(ctx context.Context, userID string) ([]WhatsAppDevice, error) {
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return nil, err
	}
	primary, err := m.serviceForKey(ctx, userKey, "")
	if err != nil {
		return nil, err
	}
	devices := []WhatsAppDevice{whatsappDeviceInfo("", primary)}
	for _, slot := range m.deviceSlots(userKey) {
		svc, err := m.serviceForKey(ctx, userKey, slot)
		if err != nil {
			log.Printf("[WHATSAPP] device %s/%s unavailable: %v", userKey, slot, err)
			continue
		}
		devices = append(devices, whatsappDeviceInfo(slot, svc))
	}
	return devices, nil
}

// SetDeviceLabel persists a display name for one device. If the device is
// already resident in memory, the label is set on the live instance
// directly. Otherwise it's written straight to that device's local metadata
// store (SetDeviceLabelOffline) rather than going through the full
// serviceForKey bootstrap -- that path opens a real WhatsApp connection
// (whatsmeow's client.Connect()) before the cheap label upsert ever runs,
// which saving a display name has no need for.
func (m *WhatsAppServiceManager) SetDeviceLabel(ctx context.Context, userID, slot, label string) error {
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return err
	}
	if slot != "" {
		clean := sanitizeWhatsAppFileName(slot)
		if clean == "" {
			return fmt.Errorf("whatsapp: invalid device %q", slot)
		}
		slot = clean
	}
	key := whatsappServiceKey(userKey, slot)

	m.mu.RLock()
	svc := m.services[key]
	m.mu.RUnlock()
	if svc != nil {
		return svc.SetDeviceLabel(ctx, label)
	}
	return NewWhatsAppService(m.devicePath(userKey, slot)).SetDeviceLabelOffline(ctx, label)
}

// NextPairingDevice is the phone a scan would pair right now: the primary
// while it is unpaired, else an extra device still waiting for its scan,
// else a fresh one. Idempotent, so a pairing screen can poll it.
func (m *WhatsAppServiceManager) NextPairingDevice(ctx context.Context, userID string) (*WhatsAppService, string, error) {
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return nil, "", err
	}
	primary, err := m.serviceForKey(ctx, userKey, "")
	if err != nil {
		return nil, "", err
	}
	if !primary.IsPaired() {
		return primary, "", nil
	}
	slots := m.deviceSlots(userKey)
	for _, slot := range slots {
		svc, err := m.serviceForKey(ctx, userKey, slot)
		if err != nil {
			continue
		}
		if !svc.IsPaired() {
			return svc, slot, nil
		}
	}
	slot := nextWhatsAppDeviceSlot(slots)
	svc, err := m.serviceForKey(ctx, userKey, slot)
	if err != nil {
		return nil, "", err
	}
	return svc, slot, nil
}

// DeviceByJID finds the account's phone paired as jid (full JID or the bare
// number).
func (m *WhatsAppServiceManager) DeviceByJID(ctx context.Context, userID, jid string) (*WhatsAppService, string, bool) {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return nil, "", false
	}
	devices, err := m.Devices(ctx, userID)
	if err != nil {
		return nil, "", false
	}
	userKey, _ := whatsappUserKey(userID)
	for _, device := range devices {
		if device.OwnJID == "" {
			continue
		}
		if device.OwnJID == jid || strings.TrimSuffix(strings.SplitN(device.OwnJID, "@", 2)[0], "") == jid {
			svc, err := m.serviceForKey(ctx, userKey, device.Slot)
			if err != nil {
				return nil, "", false
			}
			return svc, device.Slot, true
		}
	}
	return nil, "", false
}

// UnpairDevice forgets one phone. The primary is reset to a fresh pairing
// (its slot stays); an extra device is removed entirely.
func (m *WhatsAppServiceManager) UnpairDevice(ctx context.Context, userID, slot string) error {
	userKey, err := whatsappUserKey(userID)
	if err != nil {
		return err
	}
	slot = sanitizeWhatsAppFileName(slot)
	if slot == "" {
		primary, err := m.serviceForKey(ctx, userKey, "")
		if err != nil {
			return err
		}
		return primary.Unpair(ctx)
	}
	key := whatsappServiceKey(userKey, slot)
	svc, err := m.serviceForKey(ctx, userKey, slot)
	if err != nil {
		return err
	}
	if err := svc.LogoutAndRemove(ctx); err != nil {
		return err
	}
	// Held across both the map delete and the directory removal, and shared
	// with serviceForKey's create path (see lockServiceKey), so a lookup that
	// arrives mid-teardown waits for this to finish rather than lazily
	// recreating the device from a stale "not resident" read.
	unlockKey := m.lockServiceKey(key)
	defer unlockKey()
	m.mu.Lock()
	delete(m.services, key)
	m.mu.Unlock()
	dir := filepath.Join(m.baseDir, userKey, whatsappDevicesDirName, slot)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("whatsapp: remove device %s: %w", slot, err)
	}
	log.Printf("[WHATSAPP] Removed device %s for user %s", slot, userKey)
	return nil
}

func (m *WhatsAppServiceManager) serviceForKey(ctx context.Context, userKey, slot string) (*WhatsAppService, error) {
	if slot != "" {
		clean := sanitizeWhatsAppFileName(slot)
		if clean == "" {
			return nil, fmt.Errorf("whatsapp: invalid device %q", slot)
		}
		slot = clean
	}
	key := whatsappServiceKey(userKey, slot)

	m.mu.RLock()
	svc := m.services[key]
	m.mu.RUnlock()
	if svc != nil {
		if !svc.IsEnabled() {
			if err := svc.StartListening(ctx); err != nil {
				return nil, err
			}
		}
		return svc, nil
	}

	// Not resident. Serialize against a concurrent UnpairDevice for this same
	// key before doing any of the (slow, disk-touching) creation work below --
	// otherwise a device mid-teardown can be recreated out from under it.
	unlockKey := m.lockServiceKey(key)
	defer unlockKey()

	m.mu.RLock()
	svc = m.services[key]
	m.mu.RUnlock()
	if svc != nil {
		if !svc.IsEnabled() {
			if err := svc.StartListening(ctx); err != nil {
				return nil, err
			}
		}
		return svc, nil
	}

	dbPath := m.devicePath(userKey, slot)
	svc = NewWhatsAppService(dbPath)
	m.configureService(key, svc)

	m.mu.Lock()
	if existing := m.services[key]; existing != nil {
		m.mu.Unlock()
		return existing, nil
	}
	m.services[key] = svc
	if err := svc.StartListening(ctx); err != nil {
		delete(m.services, key)
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return svc, nil
}

func (m *WhatsAppServiceManager) configureService(userID string, svc *WhatsAppService) {
	_, deviceSlot := splitWhatsAppServiceKey(userID)
	svc.SetMessageHandler(func(msg BotIncomingMessage) {
		rawChannelID := msg.ChannelID
		encodedChannelID := encodeWhatsAppManagedChannelID(userID, rawChannelID)
		msg.ChannelID = encodedChannelID
		if msg.ThreadTS == rawChannelID {
			msg.ThreadTS = encodedChannelID
		}
		msg.DeviceSlot = deviceSlot
		m.mu.RLock()
		handler := m.messageHandler
		m.mu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	})
	svc.SetInteractionHandler(func(platform, channelID, threadTS, actionID, value, senderUserID string) {
		encodedChannelID := encodeWhatsAppManagedChannelID(userID, channelID)
		encodedThreadTS := threadTS
		if encodedThreadTS == "" || encodedThreadTS == channelID {
			encodedThreadTS = encodedChannelID
		}
		m.mu.RLock()
		handler := m.interaction
		m.mu.RUnlock()
		if handler != nil {
			handler(platform, encodedChannelID, encodedThreadTS, actionID, value, senderUserID)
		}
	})
	m.installProfileRouter(userID, svc)
	m.mu.RLock()
	transcriber := m.voiceTranscriber
	m.mu.RUnlock()
	if transcriber != nil {
		svc.SetVoiceTranscriber(transcriber)
	}
	svc.SetBotThreadStatusProvider(func(threadID ThreadID) BotThreadStatus {
		rawChannelID := threadID.ChannelID
		encodedChannelID := encodeWhatsAppManagedChannelID(userID, rawChannelID)
		threadID.ChannelID = encodedChannelID
		if threadID.ThreadTS == "" || threadID.ThreadTS == rawChannelID {
			threadID.ThreadTS = encodedChannelID
		}
		m.mu.RLock()
		provider := m.statusProvider
		m.mu.RUnlock()
		if provider == nil {
			return BotThreadStatus{DetailMode: "concise"}
		}
		return provider(threadID)
	})
}

func encodeWhatsAppManagedChannelID(userID, chatJID string) string {
	return userID + "|" + chatJID
}

func decodeWhatsAppManagedChannelID(channelID string) (userID, chatJID string, ok bool) {
	userID, chatJID, ok = strings.Cut(channelID, "|")
	return userID, chatJID, ok && userID != "" && chatJID != ""
}

func (m *WhatsAppServiceManager) serviceForThread(ctx context.Context, threadID ThreadID) (*WhatsAppService, ThreadID, error) {
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(threadID.ChannelID)
	if !ok {
		return nil, threadID, fmt.Errorf("whatsapp: managed channel ID missing owner")
	}
	svc, err := m.serviceForEncodedKey(ctx, userID)
	if err != nil {
		return nil, threadID, err
	}
	threadID.ChannelID = chatJID
	if threadID.ThreadTS == "" || threadID.ThreadTS == encodeWhatsAppManagedChannelID(userID, chatJID) {
		threadID.ThreadTS = chatJID
	}
	return svc, threadID, nil
}

func (m *WhatsAppServiceManager) SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error) {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return "", err
	}
	return svc.SendThreadMessage(ctx, rawThreadID, message)
}

func (m *WhatsAppServiceManager) SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error) {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return "", err
	}
	return svc.SendThreadMessageWithBlocks(ctx, rawThreadID, message, blocks)
}

func (m *WhatsAppServiceManager) UpdateMessage(ctx context.Context, threadID ThreadID, messageID string, newText string) error {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return err
	}
	return svc.UpdateMessage(ctx, rawThreadID, messageID, newText)
}

func (m *WhatsAppServiceManager) AddReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(channelID)
	if !ok {
		return nil
	}
	svc, err := m.serviceForEncodedKey(ctx, userID)
	if err != nil {
		return err
	}
	return svc.AddReaction(ctx, chatJID, messageTS, emoji)
}

func (m *WhatsAppServiceManager) RemoveReaction(ctx context.Context, channelID, messageTS, emoji string) error {
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(channelID)
	if !ok {
		return nil
	}
	svc, err := m.serviceForEncodedKey(ctx, userID)
	if err != nil {
		return err
	}
	return svc.RemoveReaction(ctx, chatJID, messageTS, emoji)
}

func (m *WhatsAppServiceManager) GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error) {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	return svc.GetThreadHistory(ctx, rawThreadID)
}

func (m *WhatsAppServiceManager) GetChannelName(ctx context.Context, channelID string) string {
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(channelID)
	if !ok {
		return ""
	}
	svc, err := m.serviceForEncodedKey(ctx, userID)
	if err != nil {
		return ""
	}
	return svc.GetChannelName(ctx, chatJID)
}

// LoadBotSessionBinding implements botSessionBindingStore. serviceForThread
// decodes the managed channel ID, so the lookup lands in exactly one
// account/device SQLite file before using the raw WhatsApp chat JID.
func (m *WhatsAppServiceManager) LoadBotSessionBinding(ctx context.Context, threadID ThreadID, routeKey string) (BotSessionBinding, bool, error) {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return BotSessionBinding{}, false, err
	}
	binding, ok := svc.loadBotSessionBinding(rawThreadID.ChannelID, routeKey)
	return binding, ok, nil
}

// SaveBotSessionBinding implements botSessionBindingStore.
func (m *WhatsAppServiceManager) SaveBotSessionBinding(ctx context.Context, threadID ThreadID, binding BotSessionBinding) error {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return err
	}
	return svc.saveBotSessionBinding(ctx, rawThreadID.ChannelID, binding)
}

// ClearBotSessionBinding implements botSessionBindingStore.
func (m *WhatsAppServiceManager) ClearBotSessionBinding(ctx context.Context, threadID ThreadID) error {
	svc, rawThreadID, err := m.serviceForThread(ctx, threadID)
	if err != nil {
		return err
	}
	return svc.clearBotSessionBinding(ctx, rawThreadID.ChannelID)
}

func (m *WhatsAppServiceManager) SetMessageHandler(handler BotMessageHandler) {
	m.mu.Lock()
	m.messageHandler = handler
	m.mu.Unlock()
}

func (m *WhatsAppServiceManager) SetInteractionHandler(handler BotInteractionHandler) {
	m.mu.Lock()
	m.interaction = handler
	m.mu.Unlock()
}

func (m *WhatsAppServiceManager) SetBotThreadStatusProvider(provider BotThreadStatusFunc) {
	m.mu.Lock()
	m.statusProvider = provider
	m.mu.Unlock()
}

func (m *WhatsAppServiceManager) GetFormatter() MessageFormatter {
	return &WhatsAppFormatter{}
}

func (m *WhatsAppServiceManager) SendNotification(ctx context.Context, uniqueID, message, contextMsg string, opts *ButtonOptions, dest *NotificationDestination) (string, error) {
	if dest == nil || strings.TrimSpace(dest.UserID) == "" {
		return "", nil
	}
	svc, err := m.ServiceForUser(ctx, dest.UserID, "", "")
	if err != nil {
		return "", err
	}
	return svc.SendNotification(ctx, uniqueID, message, contextMsg, opts, dest)
}

func (m *WhatsAppServiceManager) SendUserNotification(ctx context.Context, message, contextMsg string, dest *NotificationDestination) (string, error) {
	if dest == nil || strings.TrimSpace(dest.UserID) == "" {
		return "", nil
	}
	svc, err := m.ServiceForUser(ctx, dest.UserID, "", "")
	if err != nil {
		return "", err
	}
	return svc.SendUserNotification(ctx, message, contextMsg, dest)
}
