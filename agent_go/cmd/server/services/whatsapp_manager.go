package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const whatsappUserSessionDBName = "session.db"

// WhatsAppServiceManager owns one WhatsAppService per workspace user while
// presenting a single "whatsapp" connector to BotConversationManager.
type WhatsAppServiceManager struct {
	baseDir string

	mu             sync.RWMutex
	services       map[string]*WhatsAppService
	messageHandler BotMessageHandler
	interaction    BotInteractionHandler
	statusProvider BotThreadStatusFunc
	started        bool
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
		userID := entry.Name()
		dbPath := filepath.Join(m.baseDir, userID, whatsappUserSessionDBName)
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		if _, err := m.ServiceForUser(ctx, userID, "", ""); err != nil {
			log.Printf("[WHATSAPP] Failed to start service for user %s: %v", userID, err)
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

func (m *WhatsAppServiceManager) ServiceForUser(ctx context.Context, userID, email, username string) (*WhatsAppService, error) {
	_, _ = email, username
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("whatsapp: user ID required")
	}
	key := sanitizeWhatsAppFileName(userID)
	if key == "" {
		return nil, fmt.Errorf("whatsapp: invalid user ID %q", userID)
	}

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

	dbPath := filepath.Join(m.baseDir, key, whatsappUserSessionDBName)
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
	svc.SetMessageHandler(func(msg BotIncomingMessage) {
		rawChannelID := msg.ChannelID
		encodedChannelID := encodeWhatsAppManagedChannelID(userID, rawChannelID)
		msg.ChannelID = encodedChannelID
		if msg.ThreadTS == rawChannelID {
			msg.ThreadTS = encodedChannelID
		}
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
	svc, err := m.ServiceForUser(ctx, userID, "", "")
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
	svc, err := m.ServiceForUser(ctx, userID, "", "")
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
	svc, err := m.ServiceForUser(ctx, userID, "", "")
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
	svc, err := m.ServiceForUser(ctx, userID, "", "")
	if err != nil {
		return ""
	}
	return svc.GetChannelName(ctx, chatJID)
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
