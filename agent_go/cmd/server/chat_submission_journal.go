package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// The journal is durable acceptance, not a native-CLI exactly-once claim. An
// interrupted dispatch remains uncertain and is never automatically resent.
// Workspace PUT and the process-local lock require a single writer per owner;
// multiple active server replicas need storage-backed compare-and-swap.
type chatSubmissionRecord struct {
	ID         string `json:"submission_id"`
	Owner      string `json:"owner"`
	Project    string `json:"project"`
	Session    string `json:"session_id"`
	Message    string `json:"message"`
	State      string `json:"state"`
	UpdatedAt  string `json:"updated_at"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Response   string `json:"response,omitempty"`
}

type chatSubmissionStore struct {
	resolveProject func(owner, session string) (string, error)
	read           func(context.Context, string) (string, bool, error)
	write          func(context.Context, string, string) error
}
type chatSubmissionContextKey struct{}
type chatSubmissionContext struct{ ID, Owner, Session, Message string }
type chatSubmissionUnknownProjectKey struct{}

func submissionDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// beginChatSubmission must run after ownership authorization and before any
// delivery. Its finish callback saves the result before exposing success.
func (api *StreamingAPI) beginChatSubmission(w http.ResponseWriter, r *http.Request, session, project, message string) (http.ResponseWriter, *http.Request, func(), bool) {
	owner := strings.TrimSpace(GetUserIDFromContext(r.Context()))
	if accepted, ok := r.Context().Value(chatSubmissionContextKey{}).(chatSubmissionContext); ok && accepted.Owner == owner && accepted.Session == session && accepted.Message == message {
		return w, r, func() {}, true
	}
	if owner == "" || strings.TrimSpace(session) == "" {
		http.Error(w, "Submission owner and session are required", http.StatusUnauthorized)
		return w, r, func() {}, false
	}
	id := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if id == "" {
		id = uuid.NewString()
	}
	if len(id) > 256 {
		http.Error(w, "Idempotency-Key exceeds 256 characters", http.StatusBadRequest)
		return w, r, func() {}, false
	}
	w.Header().Set("X-Submission-ID", id)
	r.Header.Set("Idempotency-Key", id)
	path := filepath.ToSlash(filepath.Join(chatHistoryRoot(owner), "submissions", submissionDigest(id)+".json"))
	lock := productConversationRegistryMutex(path)
	lock.Lock()
	store := chatSubmissionStore{read: readFileFromWorkspace, write: writeRawFileToWorkspace, resolveProject: durableSubmissionProject}
	if api.internalChatSubmissionStore != nil {
		store = *api.internalChatSubmissionStore
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	raw, exists, err := store.read(ctx, path)
	cancel()
	if err != nil {
		lock.Unlock()
		http.Error(w, "Cannot read durable submission journal", http.StatusServiceUnavailable)
		return w, r, func() {}, false
	}
	record := chatSubmissionRecord{ID: id, Owner: owner, Project: project, Session: session, Message: message, State: "delivery_uncertain"}
	if exists {
		var previous chatSubmissionRecord
		if json.Unmarshal([]byte(raw), &previous) != nil {
			lock.Unlock()
			http.Error(w, "Unreadable durable submission record", http.StatusServiceUnavailable)
			return w, r, func() {}, false
		}
		// A cold live-input retry has no project selection of its own. The
		// owner/session-bound receipt is its durable project identity.
		if r.Context().Value(chatSubmissionUnknownProjectKey{}) == true {
			project = previous.Project
		}
		lock.Unlock()
		if previous.Owner != owner || previous.Project != project || previous.Session != session || previous.Message != message {
			http.Error(w, "Idempotency-Key already belongs to another submission", http.StatusConflict)
			return w, r, func() {}, false
		}
		if previous.State == "delivery_uncertain" {
			writeSubmissionUncertain(w, id)
			return w, r, func() {}, false
		}
		if previous.HTTPStatus < 100 || previous.HTTPStatus > 599 || previous.Response == "" {
			http.Error(w, "Incomplete durable submission outcome", http.StatusServiceUnavailable)
			return w, r, func() {}, false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(previous.HTTPStatus)
		_, _ = w.Write([]byte(previous.Response))
		return w, r, func() {}, false
	}
	if r.Context().Value(chatSubmissionUnknownProjectKey{}) == true {
		resolver := store.resolveProject
		if resolver == nil {
			resolver = durableSubmissionProject
		}
		project, err = resolver(owner, session)
		if err != nil {
			lock.Unlock()
			http.Error(w, "Cannot resolve durable conversation project; restore the conversation before sending", http.StatusConflict)
			return w, r, func() {}, false
		}
		record.Project = project
	}
	save := func() error {
		record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		defer cancel()
		return store.write(ctx, path, string(data))
	}
	if err := save(); err != nil {
		lock.Unlock()
		http.Error(w, "Cannot durably accept submission; nothing was sent", http.StatusServiceUnavailable)
		return w, r, func() {}, false
	}
	// Release before dispatch: concurrent retries see uncertain instead of waiting
	// for a provider or risking a second send.
	lock.Unlock()
	capture := &internalResponseCapture{header: w.Header().Clone()}
	finish := func() {
		// A response buffered before a panic is not a dispatch acknowledgement.
		// Keep the durable pre-dispatch uncertainty, then preserve HTTP recovery.
		if panicValue := recover(); panicValue != nil {
			panic(panicValue)
		}
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		record.HTTPStatus = status
		record.Response = capture.body.String()
		record.State = "accepted"
		if status >= 400 {
			record.State = "rejected"
		}
		var response map[string]interface{}
		if json.Unmarshal(capture.body.Bytes(), &response) == nil {
			if response["delivery_status"] == "sent_to_cli" {
				record.State = "delivery_confirmed"
			}
			if response["delivery_status"] == "delivery_uncertain" {
				record.State = "delivery_uncertain"
			}
		}
		if record.Response == "" {
			writeSubmissionUncertain(w, id)
			return
		}
		if err := save(); err != nil {
			writeSubmissionUncertain(w, id)
			return
		}
		for key, values := range capture.header {
			w.Header()[key] = values
		}
		w.WriteHeader(status)
		_, _ = w.Write(capture.body.Bytes())
	}
	r = r.WithContext(context.WithValue(r.Context(), chatSubmissionContextKey{}, chatSubmissionContext{ID: id, Owner: owner, Session: session, Message: message}))
	return capture, r, finish, true
}

func writeSubmissionUncertain(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "delivery_uncertain", "delivery_status": "delivery_uncertain", "submission_id": id, "message": fmt.Sprintf("Submission %s is durable but delivery is uncertain. Reconcile this submission before sending it again.", id)})
}

// ListUnresolvedChatSubmissions exposes durable accepted input for recovery.
// These records are evidence to reconcile, never instructions to resend. CLI
// transport acknowledgement is not proof that an assistant answer was saved.
func ListUnresolvedChatSubmissions(ctx context.Context, owner, session string) ([]chatSubmissionRecord, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("submission owner required")
	}
	root := filepath.ToSlash(filepath.Join(chatHistoryRoot(owner), "submissions"))
	listing, exists, err := listWorkspaceFolder(ctx, root, 1)
	if err != nil || !exists {
		return nil, err
	}
	paths := []string{}
	collectWorkspaceFilePaths(listing, &paths)
	result := []chatSubmissionRecord{}
	for _, path := range paths {
		if !strings.HasSuffix(path, ".json") {
			continue
		}
		raw, exists, err := readFileFromWorkspace(ctx, path)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		var record chatSubmissionRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		if record.Owner == owner && (session == "" || record.Session == session) && (record.State == "accepted" || record.State == "delivery_uncertain") {
			result = append(result, record)
		}
	}
	return result, nil
}

// Submission lookup is scoped to the authenticated owner, regardless of the
// supplied key. It works even after the in-memory terminal registry was lost.
func (api *StreamingAPI) handleChatSubmissionStatus(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(GetUserIDFromContext(r.Context()))
	if owner == "" {
		http.Error(w, "Submission owner required", http.StatusUnauthorized)
		return
	}
	id := strings.TrimSpace(r.PathValue("submission_id"))
	if id == "" {
		id = strings.TrimSpace(mux.Vars(r)["submission_id"])
	}
	if id == "" || len(id) > 256 {
		http.Error(w, "Invalid submission ID", http.StatusBadRequest)
		return
	}
	path := filepath.ToSlash(filepath.Join(chatHistoryRoot(owner), "submissions", submissionDigest(id)+".json"))
	raw, exists, err := readFileFromWorkspace(r.Context(), path)
	if err != nil {
		http.Error(w, "Cannot read submission", http.StatusServiceUnavailable)
		return
	}
	if !exists {
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}
	var record chatSubmissionRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		http.Error(w, "Unreadable submission", http.StatusServiceUnavailable)
		return
	}
	if record.Owner != owner {
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(record)
}

func durableSubmissionProject(owner, session string) (string, error) {
	runtime, exists, err := ReadChatHistoryRuntimeForSession(owner, session, "")
	if err != nil {
		return "", err
	}
	if !exists || runtime == nil {
		return "", fmt.Errorf("conversation runtime unavailable")
	}
	return strings.TrimSpace(runtime.WorkspacePath), nil
}
