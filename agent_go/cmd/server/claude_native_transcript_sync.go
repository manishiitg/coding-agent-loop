package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/cursorcli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/musecli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/picli"
	"github.com/manishiitg/multi-llm-provider-go/pkg/pathidentity"
)

// claudeNativeTranscriptRuntime is the minimal subset of a persisted builder
// conversation's "runtime" object needed to locate the coding CLI's own
// on-disk transcript for the session that produced it.
type claudeNativeTranscriptRuntime struct {
	Provider           string `json:"provider"`
	ExternalSessionID  string `json:"external_session_id"`
	AgentSessionHandle *struct {
		Provider *struct {
			Provider        string `json:"provider"`
			NativeSessionID string `json:"native_session_id"`
			WorkingDir      string `json:"working_dir"`
		} `json:"provider"`
	} `json:"agent_session_handle"`
}

type claudeTranscriptEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type claudeTranscriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeTranscriptContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// scheduleWorkflowBuilderNativeTranscriptSync closes the persistence gap for
// retained live-input turns. The turn-completion observer must stay fast, so
// transcript I/O runs off-path and is coalesced per session. Claude normally
// flushes its JSONL before it signals completion; the short retries cover the
// small race where the completion event wins that flush.
func (api *StreamingAPI) scheduleWorkflowBuilderNativeTranscriptSync(sessionID string) {
	if api == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	api.sessionWorkspaceMu.RLock()
	workspacePath := strings.TrimSpace(api.sessionWorkspaceFolders[sessionID])
	api.sessionWorkspaceMu.RUnlock()
	if workspacePath == "" {
		return
	}

	api.nativeTranscriptSyncMu.Lock()
	if api.nativeTranscriptSyncInFlight == nil {
		api.nativeTranscriptSyncInFlight = make(map[string]bool)
	}
	if api.nativeTranscriptSyncInFlight[sessionID] {
		api.nativeTranscriptSyncMu.Unlock()
		return
	}
	api.nativeTranscriptSyncInFlight[sessionID] = true
	api.nativeTranscriptSyncMu.Unlock()

	go func() {
		defer func() {
			api.nativeTranscriptSyncMu.Lock()
			delete(api.nativeTranscriptSyncInFlight, sessionID)
			api.nativeTranscriptSyncMu.Unlock()
		}()

		for attempt, delay := range []time.Duration{0, 300 * time.Millisecond, time.Second} {
			if delay > 0 {
				time.Sleep(delay)
			}
			changed, supported := api.syncWorkflowBuilderConversationFromNativeTranscript(context.Background(), sessionID, workspacePath)
			if changed || !supported {
				return
			}
			log.Printf("[CHAT_HISTORY] Native transcript sync found no completed assistant reply yet; retrying session=%s attempt=%d", sessionID, attempt+1)
		}
	}()
}

// syncWorkflowBuilderConversationFromNativeTranscript reconciles one existing
// workflow builder record, then updates its metadata index in the same pass.
// The returned supported value is false for providers without a transcript
// reader (see nativeTranscriptSyncSupportedProvider), avoiding needless
// retries for formats this package cannot parse.
func (api *StreamingAPI) syncWorkflowBuilderConversationFromNativeTranscript(ctx context.Context, sessionID, workspacePath string) (changed, supported bool) {
	raw, err := ReadChatHistoryConversation("default", sessionID, workspacePath)
	if err != nil || len(raw) == 0 || !claudeNativeTranscriptSyncSupported(raw) {
		return false, false
	}
	conversationPath, found, err := findWorkflowBuilderConversationPathForSession(ctx, sessionID, workspacePath)
	if err != nil || !found || strings.TrimSpace(conversationPath) == "" {
		return false, true
	}

	var current builderConversationLog
	if err := json.Unmarshal(raw, &current); err != nil {
		return false, true
	}
	refreshed := api.refreshLatestBuilderConversationFromNativeTranscript(ctx, conversationPath, string(raw), current)
	if builderConversationHistoriesEqual(refreshed.ConversationHistory, current.ConversationHistory) && refreshed.UpdatedAt == current.UpdatedAt {
		return false, true
	}

	// refreshLatest... writes the full record while preserving runtime and other
	// opaque fields. Re-read that canonical result before rebuilding the index.
	// If the write failed, do not advertise a transcript we did not persist.
	persistedRaw, err := ReadChatHistoryConversation("default", sessionID, workspacePath)
	if err != nil || len(persistedRaw) == 0 {
		return false, true
	}
	var persistedRecord map[string]interface{}
	if err := json.Unmarshal(persistedRaw, &persistedRecord); err != nil {
		return false, true
	}
	var persistedHistory []llmtypes.MessageContent
	if history, ok := persistedRecord["conversation_history"]; ok {
		encoded, err := json.Marshal(history)
		if err != nil || json.Unmarshal(encoded, &persistedHistory) != nil {
			return false, true
		}
	}
	if err := updatePersistedChatHistoryIndex(
		"default",
		sessionID,
		stringFromRecord(persistedRecord, "agent_mode"),
		persistedHistory,
		runtimeFromRecord(persistedRecord),
		conversationPath,
		int64(len(persistedRaw)),
		time.Now(),
	); err != nil {
		log.Printf("[CHAT_HISTORY] Native transcript sync: cannot update index for %s: %v", conversationPath, err)
	}
	return true, true
}

// findWorkflowBuilderConversationPathForSession normally resolves through the
// history index. The folder-list fallback covers a newly written transcript
// before that index exists (and remote workspace deployments without the local
// directory fast path).
func findWorkflowBuilderConversationPathForSession(ctx context.Context, sessionID, workspacePath string) (string, bool, error) {
	if path, found, err := FindChatHistoryConversationPathForSession("default", sessionID, workspacePath); err != nil || found {
		return path, found, err
	}
	listing, exists, err := listWorkspaceFolder(ctx, strings.Trim(strings.TrimSpace(workspacePath), "/")+"/builder/conversation", 3)
	if err != nil || !exists {
		return "", false, err
	}
	paths := []string{}
	collectWorkspaceFilePaths(listing, &paths)
	wantFileName := "session-" + sanitizeChatHistorySessionID(sessionID) + "-conversation.json"
	for _, path := range paths {
		if filepath.Base(path) == wantFileName && isWorkflowBuilderConversationLogPath(workspacePath, path) {
			return path, true, nil
		}
	}
	return "", false, nil
}

func claudeNativeTranscriptSyncSupported(raw []byte) bool {
	var record struct {
		Runtime claudeNativeTranscriptRuntime `json:"runtime"`
	}
	if json.Unmarshal(raw, &record) != nil {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(record.Runtime.Provider))
	if provider == "" && record.Runtime.AgentSessionHandle != nil && record.Runtime.AgentSessionHandle.Provider != nil {
		provider = strings.ToLower(strings.TrimSpace(record.Runtime.AgentSessionHandle.Provider.Provider))
	}
	return nativeTranscriptSyncSupportedProvider(provider)
}

// nativeTranscriptSyncSupportedProvider: the coding CLIs whose on-disk
// transcript can be read back -- Claude Code and Codex by readers in this
// package (claude_native_transcript_sync.go, codex_native_transcript_sync.go),
// Cursor, Pi, and Muse by the adapters' own exported readers in
// multi-llm-provider-go (cursorcli.ReadNativeTranscript,
// picli.ReadNativeTranscript, musecli.ReadNativeTranscript), since those
// formats (a sqlite blob store and two session JSONL variants) are already
// parsed there for turn completion.
func nativeTranscriptSyncSupportedProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code", "codex-cli", "cursor-cli", "pi-cli", "muse-cli":
		return true
	}
	return false
}

// builderConversationMessagesFromLLMTypes projects an adapter's text-only
// transcript into the builder conversation's own shape. Messages without
// text (tool-only turns) are dropped; system messages never reach here.
func builderConversationMessagesFromLLMTypes(messages []llmtypes.MessageContent) []builderConversationMessage {
	out := make([]builderConversationMessage, 0, len(messages))
	for _, message := range messages {
		role := ""
		switch message.Role {
		case llmtypes.ChatMessageTypeHuman:
			role = "human"
		case llmtypes.ChatMessageTypeAI:
			role = "ai"
		default:
			continue
		}
		texts := make([]string, 0, len(message.Parts))
		for _, part := range message.Parts {
			if text, ok := part.(llmtypes.TextContent); ok && strings.TrimSpace(text.Text) != "" {
				texts = append(texts, strings.TrimSpace(text.Text))
			}
		}
		text := strings.TrimSpace(strings.Join(texts, "\n\n"))
		if text == "" {
			continue
		}
		out = append(out, builderConversationMessage{Role: role, Parts: []builderConversationPart{{Text: text}}})
	}
	return out
}

// nativeTranscriptMessagesForRuntime reads the CLI's own transcript for a
// builder session. ok is false when the provider has no reader or no
// transcript could be found; callers then leave the persisted record as-is.
func nativeTranscriptMessagesForRuntime(provider, nativeSessionID, workingDir string) (messages []builderConversationMessage, maxTimestamp time.Time, transcriptPath string, ok bool, err error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code":
		if nativeSessionID == "" || workingDir == "" {
			return nil, time.Time{}, "", false, nil
		}
		transcriptPath, err = resolveClaudeNativeTranscriptPath(workingDir, nativeSessionID)
		if err != nil || transcriptPath == "" {
			return nil, time.Time{}, "", false, err
		}
		messages, maxTimestamp, err = readNewClaudeTranscriptMessages(transcriptPath, time.Time{})
		return messages, maxTimestamp, transcriptPath, err == nil, err
	case "codex-cli":
		if nativeSessionID == "" {
			return nil, time.Time{}, "", false, nil
		}
		transcriptPath, err = resolveCodexNativeTranscriptPath(nativeSessionID)
		if err != nil || transcriptPath == "" {
			return nil, time.Time{}, "", false, err
		}
		messages, maxTimestamp, err = readCodexTranscriptMessages(transcriptPath)
		return messages, maxTimestamp, transcriptPath, err == nil, err
	case "cursor-cli":
		if nativeSessionID == "" || workingDir == "" {
			return nil, time.Time{}, "", false, nil
		}
		transcript, found, err := cursorcli.ReadNativeTranscript(workingDir, nativeSessionID)
		if err != nil || !found {
			return nil, time.Time{}, "", false, err
		}
		return builderConversationMessagesFromLLMTypes(transcript.Messages), transcript.UpdatedAt, transcript.Path, true, nil
	case "pi-cli":
		if nativeSessionID == "" {
			return nil, time.Time{}, "", false, nil
		}
		transcript, found, err := picli.ReadNativeTranscript(nativeSessionID)
		if err != nil || !found {
			return nil, time.Time{}, "", false, err
		}
		return builderConversationMessagesFromLLMTypes(transcript.Messages), transcript.UpdatedAt, transcript.Path, true, nil
	case "muse-cli":
		if nativeSessionID == "" {
			return nil, time.Time{}, "", false, nil
		}
		transcript, found, err := musecli.ReadNativeTranscript(nativeSessionID)
		if err != nil || !found {
			return nil, time.Time{}, "", false, err
		}
		return builderConversationMessagesFromLLMTypes(transcript.Messages), transcript.UpdatedAt, transcript.Path, true, nil
	}
	return nil, time.Time{}, "", false, nil
}

// refreshLatestBuilderConversationFromNativeTranscript catches a persisted
// builder conversation snapshot up with the coding CLI's own on-disk
// transcript (PLAT-178) -- Claude Code's project JSONL or Codex's rollout,
// per nativeTranscriptMessagesForRuntime.
//
// Best-effort throughout: any resolution failure (no runtime info, no
// matching transcript file, nothing new) returns log unchanged. A
// successful catch-up is also persisted back to path so later reads of the
// same file -- not just this one restore response -- see it too.
func (api *StreamingAPI) refreshLatestBuilderConversationFromNativeTranscript(ctx context.Context, path, rawContent string, conv builderConversationLog) builderConversationLog {
	var record map[string]interface{}
	if err := json.Unmarshal([]byte(rawContent), &record); err != nil {
		return conv
	}
	runtimeRaw, ok := record["runtime"]
	if !ok {
		return conv
	}
	runtimeBytes, err := json.Marshal(runtimeRaw)
	if err != nil {
		return conv
	}
	var runtime claudeNativeTranscriptRuntime
	if err := json.Unmarshal(runtimeBytes, &runtime); err != nil {
		return conv
	}

	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	nativeSessionID := strings.TrimSpace(runtime.ExternalSessionID)
	workingDir := ""
	if runtime.AgentSessionHandle != nil && runtime.AgentSessionHandle.Provider != nil {
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider))
		}
		if nativeSessionID == "" {
			nativeSessionID = strings.TrimSpace(runtime.AgentSessionHandle.Provider.NativeSessionID)
		}
		workingDir = strings.TrimSpace(runtime.AgentSessionHandle.Provider.WorkingDir)
	}
	// Do not use conv.UpdatedAt as a transcript cursor. The live-input path
	// advances updated_at when it persists each human message, but it cannot
	// persist the corresponding assistant reply. A later human message can
	// therefore advance updated_at past an earlier missing reply. Reading the
	// full native transcript and sequence-merging it is what recovers those
	// interleaved replies without duplicating the already-persisted humans.
	nativeMessages, maxTimestamp, transcriptPath, ok, err := nativeTranscriptMessagesForRuntime(provider, nativeSessionID, workingDir)
	if err != nil {
		log.Printf("[CHAT_HISTORY] Native transcript catch-up: failed to read %s transcript for %s: %v", provider, nativeSessionID, err)
		return conv
	}
	if !ok || len(nativeMessages) == 0 {
		return conv
	}

	persistedHistory := conv.ConversationHistory
	previousCount := len(persistedHistory)
	merged := mergeBuilderConversationHistory(persistedHistory, nativeMessages)
	if builderConversationHistoriesEqual(merged, persistedHistory) {
		return conv
	}
	conv.ConversationHistory = merged
	if current := parseBuilderConversationUpdatedAt(conv.UpdatedAt); maxTimestamp.After(current) {
		conv.UpdatedAt = maxTimestamp.Format(time.RFC3339Nano)
	}

	// Keep the original raw entries for persisted messages. builderConversationLog
	// deliberately exposes only readable Role/Parts.Text fields; serializing it
	// wholesale would erase structured tool calls/results from the durable record
	// every time a retained live turn finishes.
	record["conversation_history"] = mergeBuilderConversationRecordHistory(record, persistedHistory, nativeMessages)
	record["updated_at"] = conv.UpdatedAt
	if encoded, err := json.MarshalIndent(record, "", "  "); err == nil {
		if err := writeRawFileToWorkspace(ctx, path, string(encoded)); err != nil {
			log.Printf("[CHAT_HISTORY] Native transcript catch-up: merged %d missing message(s) from %s but failed to persist to %s: %v", len(merged)-previousCount, transcriptPath, path, err)
		} else {
			log.Printf("[CHAT_HISTORY] Native transcript catch-up: merged %d missing message(s) into %s from %s (native transcript through %s)",
				len(merged)-previousCount, path, transcriptPath, maxTimestamp.Format(time.RFC3339))
		}
	}

	return conv
}

// mergeBuilderConversationHistory returns a shortest practical union of the
// persisted and native message sequences. Persisted-only messages are kept;
// native-only messages (most importantly assistant replies omitted by the
// asynchronous live-input writer) are inserted at their native position; and
// matching live-input human messages are not appended a second time.
//
// Positions are indexed by a normalized role+text key, so matching is linear
// apart from binary searches and repeated identical messages retain their
// multiplicity in sequence order.
func mergeBuilderConversationHistory(persisted, native []builderConversationMessage) []builderConversationMessage {
	if len(persisted) == 0 {
		return append([]builderConversationMessage(nil), native...)
	}
	if len(native) == 0 {
		return append([]builderConversationMessage(nil), persisted...)
	}

	positions := make(map[string][]int, len(persisted))
	for index, message := range persisted {
		positions[builderConversationMessageKey(message)] = append(positions[builderConversationMessageKey(message)], index)
	}

	// Find the first shared anchor. Persisted history may include conversation
	// from before the native session began, so that prefix must remain first.
	firstNative, firstPersisted := -1, -1
	for nativeIndex, message := range native {
		if candidates := positions[builderConversationMessageKey(message)]; len(candidates) > 0 {
			firstNative, firstPersisted = nativeIndex, candidates[0]
			break
		}
	}
	if firstNative < 0 {
		merged := append([]builderConversationMessage(nil), persisted...)
		return append(merged, native...)
	}

	merged := make([]builderConversationMessage, 0, len(persisted)+len(native))
	merged = append(merged, persisted[:firstPersisted]...)
	merged = append(merged, native[:firstNative]...)
	merged = append(merged, persisted[firstPersisted])
	persistedCursor := firstPersisted + 1

	for _, message := range native[firstNative+1:] {
		candidates := positions[builderConversationMessageKey(message)]
		candidateIndex := sort.SearchInts(candidates, persistedCursor)
		if candidateIndex < len(candidates) {
			matchedPersisted := candidates[candidateIndex]
			merged = append(merged, persisted[persistedCursor:matchedPersisted+1]...)
			persistedCursor = matchedPersisted + 1
			continue
		}
		merged = append(merged, message)
	}
	merged = append(merged, persisted[persistedCursor:]...)
	return merged
}

// mergeBuilderConversationRecordHistory applies the same ordered union as the
// readable-message merge, but retains each persisted entry as raw JSON. Native
// messages are the only newly encoded entries. This preserves structured tool
// calls/results and provider metadata in the canonical conversation file.
func mergeBuilderConversationRecordHistory(record map[string]interface{}, persisted, native []builderConversationMessage) []json.RawMessage {
	rawHistory, ok := builderConversationRawHistory(record)
	if !ok || len(rawHistory) != len(persisted) {
		return marshalBuilderConversationMessages(mergeBuilderConversationHistory(persisted, native))
	}
	if len(native) == 0 {
		return rawHistory
	}

	positions := make(map[string][]int, len(persisted))
	for index, message := range persisted {
		key := builderConversationMessageKey(message)
		positions[key] = append(positions[key], index)
	}
	firstNative, firstPersisted := -1, -1
	for nativeIndex, message := range native {
		if candidates := positions[builderConversationMessageKey(message)]; len(candidates) > 0 {
			firstNative, firstPersisted = nativeIndex, candidates[0]
			break
		}
	}
	if firstNative < 0 {
		return append(rawHistory, marshalBuilderConversationMessages(native)...)
	}

	merged := make([]json.RawMessage, 0, len(persisted)+len(native))
	merged = append(merged, rawHistory[:firstPersisted]...)
	merged = append(merged, marshalBuilderConversationMessages(native[:firstNative])...)
	merged = append(merged, rawHistory[firstPersisted])
	persistedCursor := firstPersisted + 1
	for _, message := range native[firstNative+1:] {
		candidates := positions[builderConversationMessageKey(message)]
		candidateIndex := sort.SearchInts(candidates, persistedCursor)
		if candidateIndex < len(candidates) {
			matchedPersisted := candidates[candidateIndex]
			merged = append(merged, rawHistory[persistedCursor:matchedPersisted+1]...)
			persistedCursor = matchedPersisted + 1
			continue
		}
		merged = append(merged, marshalBuilderConversationMessages([]builderConversationMessage{message})...)
	}
	return append(merged, rawHistory[persistedCursor:]...)
}

func builderConversationRawHistory(record map[string]interface{}) ([]json.RawMessage, bool) {
	history, exists := record["conversation_history"]
	if !exists {
		return nil, false
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		return nil, false
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, false
	}
	return raw, true
}

func marshalBuilderConversationMessages(messages []builderConversationMessage) []json.RawMessage {
	encoded := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		messageJSON, err := json.Marshal(message)
		if err == nil {
			encoded = append(encoded, messageJSON)
		}
	}
	return encoded
}

func builderConversationMessageKey(message builderConversationMessage) string {
	texts := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		if text := strings.Join(strings.Fields(part.Text), " "); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.ToLower(strings.TrimSpace(message.Role)) + "\x00" + strings.Join(texts, "\n")
}

func builderConversationHistoriesEqual(left, right []builderConversationMessage) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if builderConversationMessageKey(left[index]) != builderConversationMessageKey(right[index]) {
			return false
		}
	}
	return true
}

// resolveClaudeNativeTranscriptPath locates Claude Code's own JSONL
// transcript for a session, mirroring the working-directory-to-project-slug
// scheme multi-llm-provider-go's claudecode adapter uses
// (pkg/adapters/claudecode/claudecode_transcript_path.go) plus its
// session-ID glob fallback for when that escaping scheme has changed across
// CLI versions. Duplicated here (rather than imported) because that
// resolver is unexported and this is a narrow, self-contained lookup.
func resolveClaudeNativeTranscriptPath(workingDir, sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	candidates := pathidentity.Candidates(workingDir)

	seen := make(map[string]struct{}, len(candidates))
	for _, dir := range candidates {
		slug := claudeNativeTranscriptProjectSlug(dir)
		if slug == "" {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		path := filepath.Join(home, ".claude", "projects", slug, sessionID+".jsonl")
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return path, nil
		}
	}

	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", err
	}
	return matches[0], nil
}

func claudeNativeTranscriptProjectSlug(workingDir string) string {
	workingDir = filepath.Clean(strings.TrimSpace(workingDir))
	if workingDir == "" || workingDir == "." {
		return ""
	}
	return strings.NewReplacer(
		"/", "-",
		"\\", "-",
		"_", "-",
		".", "-",
		":", "-",
	).Replace(workingDir)
}

// readNewClaudeTranscriptMessages parses Claude Code's JSONL transcript and
// returns only the human-visible user/assistant messages timestamped after
// since, converted to the same builderConversationMessage shape the rest of
// the builder conversation log already uses.
func readNewClaudeTranscriptMessages(path string, since time.Time) ([]builderConversationMessage, time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}

	var messages []builderConversationMessage
	maxTimestamp := since
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry claudeTranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if !ts.After(since) {
			continue
		}
		if len(entry.Message) == 0 {
			continue
		}
		var msg claudeTranscriptMessage
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}

		text := extractClaudeTranscriptText(msg.Content)
		if text == "" || isClaudeLocalCommandMessage(entry.Type, text) {
			continue
		}

		role := "ai"
		if entry.Type == "user" {
			role = "human"
		}
		messages = append(messages, builderConversationMessage{
			Role:  role,
			Parts: []builderConversationPart{{Text: text}},
		})
		if ts.After(maxTimestamp) {
			maxTimestamp = ts
		}
	}
	return messages, maxTimestamp, nil
}

// extractClaudeTranscriptText pulls the human-visible text out of a
// transcript entry's message.content, which is either a plain string (a
// real typed message) or a list of content blocks (thinking/text/tool_use
// for assistant turns, tool_result for a user-role entry that is actually
// just a tool's output echoed back, not something the user typed). Only
// plain-string content and "text"-typed blocks are kept -- tool_use,
// tool_result, and thinking are execution detail the chat history was never
// meant to display, and including them would clutter it, not fix it.
func extractClaudeTranscriptText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	var asString string
	if err := json.Unmarshal(content, &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	var blocks []claudeTranscriptContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		text := strings.TrimSpace(block.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
