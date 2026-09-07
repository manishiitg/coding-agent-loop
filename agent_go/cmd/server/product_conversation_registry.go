package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

const (
	productConversationRegistryVersion = 1
	productConversationRegistryFile    = "product-conversations.json"
)

var (
	productConversationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	productConversationLocks      [64]sync.Mutex
)

// ProductConversationRecord is the durable identity binding for one product
// conversation. ConversationID is the logical chat; SessionID is the current
// AgentWorks history/runtime owner. They are intentionally separate so a
// future runtime migration does not change the product conversation identity.
type ProductConversationRecord struct {
	ConversationID  string `json:"conversation_id"`
	ConversationKey string `json:"conversation_key"`
	ProfileID       string `json:"profile_id"`
	ProfileVersion  int    `json:"profile_version"`
	SessionID       string `json:"session_id"`
	WorkspacePath   string `json:"workspace_path"`
	ResourceID      string `json:"resource_id,omitempty"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	// Provider is the coding-agent runtime the conversation runs on, bound by
	// its first turn. A Codex thread and a Claude Code session are separate
	// CLI state, so the other runtime would start blank: the model can change
	// mid-chat, the provider cannot — a new conversation starts unbound.
	Provider        string `json:"provider,omitempty"`
	// ModelID and ReasoningEffort are the runtime options the conversation's
	// already-running tmux-backed CLI process was last launched with. Unlike
	// Provider these may change mid-chat, but the running process does not
	// notice on its own — the interactive adapter only builds its CLI flags
	// (--model, -c model_reasoning_effort) once, at first acquire, and every
	// later turn against the same owner session id silently reuses that
	// process. bindRuntime compares against these to know when the caller
	// must close the tmux session so the next turn relaunches with the new
	// flags (see closeAllCodingCLIInteractiveSessionsForOwner).
	ModelID         string `json:"model_id,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type productConversationRegistryDocument struct {
	Version int                                  `json:"version"`
	Entries map[string]ProductConversationRecord `json:"entries"`
	// Previous keeps, per entry, the conversations a rotation or a switch
	// moved out of the live slot, newest first, so a product can list,
	// reopen or forget its earlier chats. Identities only; the transcripts
	// stay in chat_history.
	Previous map[string][]ProductConversationRecord `json:"previous,omitempty"`
}

// productConversationPreviousLimit bounds how many earlier conversations one
// slot remembers; the oldest fall off (their transcripts remain on disk).
const productConversationPreviousLimit = 200

// rememberPreviousConversation moves a record to the front of a slot's
// previous list, dropping any older copy of the same session.
func rememberPreviousConversation(document *productConversationRegistryDocument, entryKey string, record ProductConversationRecord) {
	if strings.TrimSpace(record.SessionID) == "" {
		return
	}
	if document.Previous == nil {
		document.Previous = map[string][]ProductConversationRecord{}
	}
	kept := make([]ProductConversationRecord, 0, len(document.Previous[entryKey])+1)
	kept = append(kept, record)
	for _, previous := range document.Previous[entryKey] {
		if previous.SessionID != record.SessionID {
			kept = append(kept, previous)
		}
	}
	if len(kept) > productConversationPreviousLimit {
		kept = kept[:productConversationPreviousLimit]
	}
	document.Previous[entryKey] = kept
}

func (store productConversationRegistryStore) loadDocument(ctx context.Context, path string) (productConversationRegistryDocument, error) {
	document := productConversationRegistryDocument{Version: productConversationRegistryVersion, Entries: map[string]ProductConversationRecord{}}
	raw, exists, err := store.read(ctx, path)
	if err != nil {
		return document, fmt.Errorf("read product conversation registry: %w", err)
	}
	if exists && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return document, fmt.Errorf("decode product conversation registry: %w", err)
		}
		if document.Entries == nil {
			document.Entries = map[string]ProductConversationRecord{}
		}
	}
	return document, nil
}

// history reports a slot's live conversation (ok=false when none has been
// opened yet) and the ones moved out of it, newest first.
func (store productConversationRegistryStore) history(ctx context.Context, userID string, profile agentprofiles.Profile, binding productConversationBinding) (current ProductConversationRecord, ok bool, previous []ProductConversationRecord, err error) {
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()
	document, err := store.loadDocument(ctx, path)
	if err != nil {
		return ProductConversationRecord{}, false, nil, err
	}
	entryKey := productConversationRegistryEntryKey(profile.ID, binding.ConversationKey)
	current, ok = document.Entries[entryKey]
	previous = append([]ProductConversationRecord(nil), document.Previous[entryKey]...)
	return current, ok, previous, nil
}

// liveSessionIDs is every session a profile's slots currently point at — its
// main chat and any isolated ones (a check-in, a second phone).
func (store productConversationRegistryStore) liveSessionIDs(ctx context.Context, userID, profileID string) (map[string]bool, error) {
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()
	document, err := store.loadDocument(ctx, path)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	prefix := strings.TrimSpace(profileID) + "/"
	for entryKey, record := range document.Entries {
		if strings.HasPrefix(entryKey, prefix) && strings.TrimSpace(record.SessionID) != "" {
			live[record.SessionID] = true
		}
	}
	return live, nil
}

// switchTo makes an earlier conversation the slot's live one again; the one
// it replaces joins the previous list. A session not in that list is
// accepted only when the caller verified it is this slot's (a chat rotated
// before the registry kept history), and gets a fresh conversation id.
func (store productConversationRegistryStore) switchTo(ctx context.Context, userID string, profile agentprofiles.Profile, binding productConversationBinding, sessionID string, verified bool) (ProductConversationRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ProductConversationRecord{}, fmt.Errorf("session_id is required")
	}
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()
	document, err := store.loadDocument(ctx, path)
	if err != nil {
		return ProductConversationRecord{}, err
	}
	entryKey := productConversationRegistryEntryKey(profile.ID, binding.ConversationKey)
	current, ok := document.Entries[entryKey]
	if !ok {
		return ProductConversationRecord{}, fmt.Errorf("product conversation %q has not been opened yet", entryKey)
	}
	if current.SessionID == sessionID {
		return current, nil
	}
	now := store.now().UTC().Format(time.RFC3339Nano)
	var target *ProductConversationRecord
	remaining := make([]ProductConversationRecord, 0, len(document.Previous[entryKey]))
	for _, previous := range document.Previous[entryKey] {
		if previous.SessionID == sessionID && target == nil {
			found := previous
			target = &found
			continue
		}
		remaining = append(remaining, previous)
	}
	if target == nil {
		if !verified {
			return ProductConversationRecord{}, fmt.Errorf("session %q is not one of this conversation's earlier chats", sessionID)
		}
		restored := current
		restored.ConversationID = "conversation-" + store.newID()
		restored.SessionID = sessionID
		restored.CreatedAt = now
		target = &restored
	}
	if document.Previous == nil {
		document.Previous = map[string][]ProductConversationRecord{}
	}
	document.Previous[entryKey] = remaining
	rememberPreviousConversation(&document, entryKey, current)
	target.UpdatedAt = now
	if strings.TrimSpace(binding.ManifestPath) != "" {
		if err := store.writeManifestSessionID(ctx, binding.ManifestPath, target.SessionID); err != nil {
			return ProductConversationRecord{}, err
		}
	}
	document.Version = productConversationRegistryVersion
	document.Entries[entryKey] = *target
	if err := store.writeDocument(ctx, path, document); err != nil {
		return ProductConversationRecord{}, err
	}
	return *target, nil
}

// forget drops an earlier conversation from a slot's previous list. The live
// conversation is refused: start a new chat first. A session the registry
// never listed (rotated before it kept history) is simply not there.
func (store productConversationRegistryStore) forget(ctx context.Context, userID string, profile agentprofiles.Profile, binding productConversationBinding, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()
	document, err := store.loadDocument(ctx, path)
	if err != nil {
		return err
	}
	entryKey := productConversationRegistryEntryKey(profile.ID, binding.ConversationKey)
	if current, ok := document.Entries[entryKey]; ok && current.SessionID == sessionID {
		return fmt.Errorf("this is the live conversation — start a new chat before deleting it")
	}
	remaining := make([]ProductConversationRecord, 0, len(document.Previous[entryKey]))
	for _, previous := range document.Previous[entryKey] {
		if previous.SessionID != sessionID {
			remaining = append(remaining, previous)
		}
	}
	if len(remaining) == len(document.Previous[entryKey]) {
		return nil
	}
	if document.Previous == nil {
		document.Previous = map[string][]ProductConversationRecord{}
	}
	document.Previous[entryKey] = remaining
	document.Version = productConversationRegistryVersion
	return store.writeDocument(ctx, path, document)
}

type productConversationBinding struct {
	ConversationKey        string
	WorkspacePath          string
	ManifestPath           string
	ResourceID             string
	Title                  string
	Description            string
	AuthoritativeSessionID string
}

type productConversationRegistryStore struct {
	read  func(context.Context, string) (string, bool, error)
	write func(context.Context, string, string) error
	now   func() time.Time
	newID func() string
}

func defaultProductConversationRegistryStore() productConversationRegistryStore {
	return productConversationRegistryStore{
		read:  readFileFromWorkspace,
		write: writeRawFileToWorkspace,
		now:   time.Now,
		newID: uuid.NewString,
	}
}

func productConversationRegistryPath(userID string) string {
	return filepath.ToSlash(filepath.Join(chatHistoryRoot(userID), productConversationRegistryFile))
}

func productConversationRegistryEntryKey(profileID, conversationKey string) string {
	return strings.TrimSpace(profileID) + "/" + strings.TrimSpace(conversationKey)
}

func productConversationRegistryMutex(path string) *sync.Mutex {
	var hash uint64 = 1469598103934665603
	for i := 0; i < len(path); i++ {
		hash ^= uint64(path[i])
		hash *= 1099511628211
	}
	return &productConversationLocks[hash%uint64(len(productConversationLocks))]
}

func (store productConversationRegistryStore) resolveOrCreate(
	ctx context.Context,
	userID string,
	profile agentprofiles.Profile,
	binding productConversationBinding,
	preferredSessionID string,
) (ProductConversationRecord, error) {
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()

	document := productConversationRegistryDocument{
		Version: productConversationRegistryVersion,
		Entries: map[string]ProductConversationRecord{},
	}
	if raw, exists, err := store.read(ctx, path); err != nil {
		return ProductConversationRecord{}, fmt.Errorf("read product conversation registry: %w", err)
	} else if exists && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return ProductConversationRecord{}, fmt.Errorf("decode product conversation registry: %w", err)
		}
		if document.Entries == nil {
			document.Entries = map[string]ProductConversationRecord{}
		}
	}

	entryKey := productConversationRegistryEntryKey(profile.ID, binding.ConversationKey)
	now := store.now().UTC().Format(time.RFC3339Nano)
	if existing, ok := document.Entries[entryKey]; ok {
		if strings.TrimSpace(existing.ConversationID) == "" || strings.TrimSpace(existing.SessionID) == "" {
			return ProductConversationRecord{}, fmt.Errorf("product conversation %q has an incomplete durable identity", entryKey)
		}
		changed := existing.ProfileVersion != profile.Version ||
			existing.WorkspacePath != binding.WorkspacePath ||
			existing.ResourceID != binding.ResourceID ||
			existing.Title != binding.Title ||
			existing.Description != binding.Description
		existing.ProfileVersion = profile.Version
		existing.WorkspacePath = binding.WorkspacePath
		existing.ResourceID = binding.ResourceID
		existing.Title = binding.Title
		existing.Description = binding.Description
		// A keyed project manifest owns its session identity. This also makes a
		// completed conversation rotation resilient to a process crash between
		// updating the manifest and this registry document.
		if authoritative := strings.TrimSpace(binding.AuthoritativeSessionID); authoritative != "" && existing.SessionID != authoritative {
			existing.SessionID = authoritative
			changed = true
		}
		if changed {
			existing.UpdatedAt = now
			document.Entries[entryKey] = existing
			if err := store.writeDocument(ctx, path, document); err != nil {
				return ProductConversationRecord{}, err
			}
		}
		return existing, nil
	}

	sessionID := strings.TrimSpace(binding.AuthoritativeSessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(preferredSessionID)
	}
	if sessionID == "" {
		sessionID = "product-" + store.newID()
	}
	record := ProductConversationRecord{
		ConversationID:  "conversation-" + store.newID(),
		ConversationKey: binding.ConversationKey,
		ProfileID:       profile.ID,
		ProfileVersion:  profile.Version,
		SessionID:       sessionID,
		WorkspacePath:   binding.WorkspacePath,
		ResourceID:      binding.ResourceID,
		Title:           binding.Title,
		Description:     binding.Description,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	document.Version = productConversationRegistryVersion
	document.Entries[entryKey] = record
	if err := store.writeDocument(ctx, path, document); err != nil {
		return ProductConversationRecord{}, err
	}
	return record, nil
}

// rotate starts a fresh durable conversation for a product chat. It retains
// the product/project and its files; it deliberately does not erase the old
// chat history, which remains an audit record but is no longer the active
// conversation for this product resource.
func (store productConversationRegistryStore) rotate(
	ctx context.Context,
	userID string,
	profile agentprofiles.Profile,
	binding productConversationBinding,
) (ProductConversationRecord, error) {
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()

	document := productConversationRegistryDocument{
		Version: productConversationRegistryVersion,
		Entries: map[string]ProductConversationRecord{},
	}
	if raw, exists, err := store.read(ctx, path); err != nil {
		return ProductConversationRecord{}, fmt.Errorf("read product conversation registry: %w", err)
	} else if exists && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return ProductConversationRecord{}, fmt.Errorf("decode product conversation registry: %w", err)
		}
		if document.Entries == nil {
			document.Entries = map[string]ProductConversationRecord{}
		}
	}

	now := store.now().UTC().Format(time.RFC3339Nano)
	entryKey := productConversationRegistryEntryKey(profile.ID, binding.ConversationKey)
	createdAt := now
	if existing, ok := document.Entries[entryKey]; ok && strings.TrimSpace(existing.CreatedAt) != "" {
		createdAt = existing.CreatedAt
	}
	record := ProductConversationRecord{
		ConversationID:  "conversation-" + store.newID(),
		ConversationKey: binding.ConversationKey,
		ProfileID:       profile.ID,
		ProfileVersion:  profile.Version,
		SessionID:       "product-" + store.newID(),
		WorkspacePath:   binding.WorkspacePath,
		ResourceID:      binding.ResourceID,
		Title:           binding.Title,
		Description:     binding.Description,
		CreatedAt:       createdAt,
		UpdatedAt:       now,
	}

	// A project manifest is an authoritative runtime binding. Update it before
	// exposing the new record, so an ordinary resolve after this call cannot
	// restore the previous project session.
	if strings.TrimSpace(binding.ManifestPath) != "" {
		if err := store.writeManifestSessionID(ctx, binding.ManifestPath, record.SessionID); err != nil {
			return ProductConversationRecord{}, err
		}
	}
	// The conversation this one replaces stays listed, so the product can
	// reopen it (its transcript is still in chat_history).
	if existing, ok := document.Entries[entryKey]; ok {
		rememberPreviousConversation(&document, entryKey, existing)
	}
	document.Version = productConversationRegistryVersion
	document.Entries[entryKey] = record
	if err := store.writeDocument(ctx, path, document); err != nil {
		return ProductConversationRecord{}, err
	}
	return record, nil
}

// bindRuntime records the runtime a conversation runs on the first time a
// turn names one. A different provider on an already-bound conversation is
// refused (boundProvider names what it is bound to; the caller starts a new
// chat to switch). A different model or reasoning effort is accepted and
// recorded, but restartNeeded reports that the tmux-backed CLI process this
// conversation's session already has running was launched with the old
// values — the caller must close it (closeAllCodingCLIInteractiveSessionsForOwner)
// before this turn runs, or the process silently keeps using them.
func (store productConversationRegistryStore) bindRuntime(
	ctx context.Context,
	userID string,
	profile agentprofiles.Profile,
	conversationKey string,
	provider, modelID, reasoningEffort string,
) (boundProvider string, restartNeeded bool, err error) {
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	path := productConversationRegistryPath(userID)
	mutex := productConversationRegistryMutex(path)
	mutex.Lock()
	defer mutex.Unlock()

	document := productConversationRegistryDocument{Version: productConversationRegistryVersion, Entries: map[string]ProductConversationRecord{}}
	raw, exists, readErr := store.read(ctx, path)
	if readErr != nil {
		return "", false, fmt.Errorf("read product conversation registry: %w", readErr)
	}
	if exists && strings.TrimSpace(raw) != "" {
		if decodeErr := json.Unmarshal([]byte(raw), &document); decodeErr != nil {
			return "", false, fmt.Errorf("decode product conversation registry: %w", decodeErr)
		}
		if document.Entries == nil {
			document.Entries = map[string]ProductConversationRecord{}
		}
	}
	entryKey := productConversationRegistryEntryKey(profile.ID, conversationKey)
	record, ok := document.Entries[entryKey]
	if !ok {
		return "", false, fmt.Errorf("product conversation %q is not registered", entryKey)
	}
	if bound := strings.TrimSpace(record.Provider); bound != "" {
		if !strings.EqualFold(bound, provider) {
			return bound, false, nil
		}
		restartNeeded = (modelID != "" && !strings.EqualFold(strings.TrimSpace(record.ModelID), modelID)) ||
			(reasoningEffort != "" && !strings.EqualFold(strings.TrimSpace(record.ReasoningEffort), reasoningEffort))
	}
	record.Provider = provider
	if modelID != "" {
		record.ModelID = modelID
	}
	if reasoningEffort != "" {
		record.ReasoningEffort = reasoningEffort
	}
	record.UpdatedAt = store.now().UTC().Format(time.RFC3339Nano)
	document.Version = productConversationRegistryVersion
	document.Entries[entryKey] = record
	if writeErr := store.writeDocument(ctx, path, document); writeErr != nil {
		return "", false, writeErr
	}
	return provider, restartNeeded, nil
}

func (store productConversationRegistryStore) writeManifestSessionID(ctx context.Context, manifestPath, sessionID string) error {
	raw, exists, err := store.read(ctx, manifestPath)
	if err != nil {
		return fmt.Errorf("read product manifest: %w", err)
	}
	if !exists {
		return fmt.Errorf("product manifest was not found")
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return fmt.Errorf("decode product manifest: %w", err)
	}
	encodedSessionID, err := json.Marshal(sessionID)
	if err != nil {
		return fmt.Errorf("encode product session id: %w", err)
	}
	manifest["session_id"] = encodedSessionID
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode product manifest: %w", err)
	}
	if err := store.write(ctx, manifestPath, string(encoded)+"\n"); err != nil {
		return fmt.Errorf("write product manifest: %w", err)
	}
	return nil
}

func (store productConversationRegistryStore) writeDocument(ctx context.Context, path string, document productConversationRegistryDocument) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode product conversation registry: %w", err)
	}
	if err := store.write(ctx, path, string(data)+"\n"); err != nil {
		return fmt.Errorf("write product conversation registry: %w", err)
	}
	return nil
}

type productProjectManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Product       string `json:"product"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	SessionID     string `json:"session_id"`
}

type productProjectStore struct {
	listPaths func(context.Context, string) ([]string, bool, error)
	read      func(context.Context, string) (string, bool, error)
}

func defaultProductProjectStore() productProjectStore {
	return productProjectStore{
		listPaths: func(ctx context.Context, root string) ([]string, bool, error) {
			listing, exists, err := listWorkspaceFolder(ctx, root, 3)
			if err != nil || !exists {
				return nil, exists, err
			}
			paths := []string{}
			collectWorkspaceFilePaths(listing, &paths)
			return paths, true, nil
		},
		read: readFileFromWorkspace,
	}
}

func resolveProductProjectBinding(ctx context.Context, userID string, profile agentprofiles.Profile, projectID string) (productConversationBinding, error) {
	return resolveProductProjectBindingWithStore(ctx, userID, profile, projectID, defaultProductProjectStore())
}

func resolveProductProjectBindingWithStore(
	ctx context.Context,
	userID string,
	profile agentprofiles.Profile,
	projectID string,
	store productProjectStore,
) (productConversationBinding, error) {
	projectsRoot, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.ProjectsRoot, userID)
	if err != nil {
		return productConversationBinding{}, fmt.Errorf("invalid product projects root: %w", err)
	}
	runtimeRoot := agentProfileRuntimeWorkspace(userID, projectsRoot)
	paths, exists, err := store.listPaths(ctx, runtimeRoot)
	if err != nil {
		return productConversationBinding{}, fmt.Errorf("list product projects: %w", err)
	}
	if !exists {
		return productConversationBinding{}, fmt.Errorf("product projects root does not exist")
	}
	sort.Strings(paths)

	var matched *productConversationBinding
	rootPrefix := strings.TrimSuffix(filepath.ToSlash(runtimeRoot), "/") + "/"
	seenCandidatePaths := make(map[string]struct{}, len(paths))
	for _, candidate := range paths {
		candidate = filepath.ToSlash(strings.TrimSpace(candidate))
		if !strings.HasPrefix(candidate, rootPrefix) || !strings.HasSuffix(candidate, "/product.json") {
			continue
		}
		// The workspace listing can return a folder both nested under its root
		// and as a top-level item, which repeats the same file path. That is one
		// manifest, not a duplicate project id. Different paths with the same id
		// remain a hard error below.
		if _, seen := seenCandidatePaths[candidate]; seen {
			continue
		}
		seenCandidatePaths[candidate] = struct{}{}
		raw, found, readErr := store.read(ctx, candidate)
		if readErr != nil {
			return productConversationBinding{}, fmt.Errorf("read project manifest %s: %w", candidate, readErr)
		}
		if !found {
			continue
		}
		var manifest productProjectManifest
		if json.Unmarshal([]byte(raw), &manifest) != nil ||
			strings.TrimSpace(manifest.Product) != profile.ID ||
			strings.TrimSpace(manifest.ID) != projectID {
			continue
		}
		if strings.TrimSpace(manifest.Title) == "" || strings.TrimSpace(manifest.SessionID) == "" {
			return productConversationBinding{}, fmt.Errorf("project %q has an incomplete product manifest", projectID)
		}
		if matched != nil {
			return productConversationBinding{}, fmt.Errorf("project id %q is duplicated", projectID)
		}
		workspacePath := filepath.ToSlash(filepath.Dir(candidate))
		matched = &productConversationBinding{
			ConversationKey:        projectID,
			WorkspacePath:          workspacePath,
			ManifestPath:           candidate,
			ResourceID:             projectID,
			Title:                  strings.TrimSpace(manifest.Title),
			Description:            strings.TrimSpace(manifest.Description),
			AuthoritativeSessionID: strings.TrimSpace(manifest.SessionID),
		}
	}
	if matched == nil {
		return productConversationBinding{}, fmt.Errorf("project %q was not found", projectID)
	}
	return *matched, nil
}

// resolveIsolatedScheduleBinding is resolveProductConversationBinding's
// singleton case with a fixed "pulse" key instead of "main" — a second,
// independent registry entry (its own session, workspace binding otherwise
// identical) for a schedule that declared Isolated. Not reachable from the
// client-facing conversation_key parameter: that boundary's job is to reject
// exactly this kind of caller-chosen key on a singleton profile, so this is
// a distinct, server-internal path for the scheduler alone, not a relaxation
// of that check.
func resolveIsolatedScheduleBinding(ctx context.Context, userID string, profile agentprofiles.Profile) (productConversationBinding, error) {
	return resolveIsolatedProductBinding(ctx, userID, profile, "pulse", profile.Name+" (check-in)")
}

// resolveIsolatedProductBinding gives a singleton profile a further
// conversation of its own under a server-chosen key — a schedule's, or a
// second phone's — on the same workspace, apart from the profile's main
// conversation. Server-internal: the client-facing binding keeps rejecting
// keys for singleton profiles.
func resolveIsolatedProductBinding(ctx context.Context, userID string, profile agentprofiles.Profile, key, title string) (productConversationBinding, error) {
	if mode := strings.ToLower(strings.TrimSpace(profile.Runtime.Conversation.Mode)); mode != agentprofiles.ConversationModeSingleton {
		return productConversationBinding{}, fmt.Errorf("isolated conversations are only supported for singleton-conversation profiles, got %q", mode)
	}
	key = strings.TrimSpace(key)
	if key == "" || key == "main" || !productConversationKeyPattern.MatchString(key) {
		return productConversationBinding{}, fmt.Errorf("invalid isolated conversation key %q", key)
	}
	workspacePath, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.Root, userID)
	if err != nil {
		return productConversationBinding{}, fmt.Errorf("invalid product workspace root: %w", err)
	}
	return productConversationBinding{
		ConversationKey: key,
		WorkspacePath:   workspacePath,
		Title:           firstNonEmptyTrimmed(title, profile.Name),
	}, nil
}

func resolveProductConversationBinding(ctx context.Context, userID string, profile agentprofiles.Profile, requestedKey string) (productConversationBinding, error) {
	mode := strings.ToLower(strings.TrimSpace(profile.Runtime.Conversation.Mode))
	switch mode {
	case agentprofiles.ConversationModeSingleton:
		if key := strings.TrimSpace(requestedKey); key != "" && key != "main" {
			return productConversationBinding{}, fmt.Errorf("singleton product does not accept conversation_key %q", key)
		}
		workspacePath, err := cleanAgentProfileWorkspace(profile.Runtime.Workspace.Root, userID)
		if err != nil {
			return productConversationBinding{}, fmt.Errorf("invalid product workspace root: %w", err)
		}
		return productConversationBinding{
			ConversationKey: "main",
			WorkspacePath:   workspacePath,
			Title:           profile.Name,
		}, nil
	case agentprofiles.ConversationModeKeyed:
		key := strings.TrimSpace(requestedKey)
		if !productConversationKeyPattern.MatchString(key) {
			return productConversationBinding{}, fmt.Errorf("conversation_key is required and must be a stable product resource id")
		}
		if strings.ToLower(strings.TrimSpace(profile.Runtime.Conversation.KeyType)) != agentprofiles.ConversationKeyTypeProject {
			return productConversationBinding{}, fmt.Errorf("unsupported product conversation key type")
		}
		return resolveProductProjectBinding(ctx, userID, profile, key)
	default:
		return productConversationBinding{}, fmt.Errorf("agent profile does not declare a product conversation policy")
	}
}
