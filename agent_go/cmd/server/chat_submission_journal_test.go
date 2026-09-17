package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestChatSubmissionDurableAcceptanceAndReplay(t *testing.T) {
	files := map[string]string{}
	store := &chatSubmissionStore{
		read:  func(_ context.Context, p string) (string, bool, error) { v, ok := files[p]; return v, ok, nil },
		write: func(_ context.Context, p, v string) error { files[p] = v; return nil },
	}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/query", nil)
		r.Header.Set("Idempotency-Key", "turn-1")
		return r
	}
	api := &StreamingAPI{internalChatSubmissionStore: store}
	result := httptest.NewRecorder()
	w, _, finish, ok := api.beginChatSubmission(result, request(), "session-a", "project-a", "hello")
	if !ok || len(files) != 1 {
		t.Fatal("must durably accept before dispatch")
	}
	for _, raw := range files {
		var record chatSubmissionRecord
		_ = json.Unmarshal([]byte(raw), &record)
		if record.State != "delivery_uncertain" || record.Message != "hello" {
			t.Fatalf("invalid predispatch record: %s", raw)
		}
	}
	pending := httptest.NewRecorder()
	if _, _, _, ok := api.beginChatSubmission(pending, request(), "session-a", "project-a", "hello"); ok || pending.Code != 409 {
		t.Fatal("in-flight retry must not dispatch")
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"delivery_status": "sent_to_cli"})
	finish()
	restarted := &StreamingAPI{internalChatSubmissionStore: store}
	replay := httptest.NewRecorder()
	if _, _, _, ok := restarted.beginChatSubmission(replay, request(), "session-a", "project-a", "hello"); ok {
		t.Fatal("restarted retry must not dispatch")
	}
	if replay.Code != 200 || replay.Body.String() != result.Body.String() {
		t.Fatalf("response was not replayed: %s", replay.Body.String())
	}
	conflict := httptest.NewRecorder()
	if _, _, _, ok := restarted.beginChatSubmission(conflict, request(), "session-b", "project-a", "hello"); ok || conflict.Code != 409 {
		t.Fatal("key cannot move between conversations")
	}
}

func TestChatSubmissionPersistenceFailuresNeverInviteResend(t *testing.T) {
	files := map[string]string{}
	fail := true
	store := &chatSubmissionStore{
		read: func(_ context.Context, p string) (string, bool, error) { v, ok := files[p]; return v, ok, nil },
		write: func(_ context.Context, p, v string) error {
			if fail {
				return errors.New("disk unavailable")
			}
			files[p] = v
			return nil
		},
	}
	api := &StreamingAPI{internalChatSubmissionStore: store}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/query", nil)
		r.Header.Set("Idempotency-Key", "turn-2")
		return r
	}
	rejected := httptest.NewRecorder()
	if _, _, _, ok := api.beginChatSubmission(rejected, request(), "s", "p", "hello"); ok || rejected.Code != 503 {
		t.Fatal("failed acceptance must prevent dispatch")
	}
	fail = false
	response := httptest.NewRecorder()
	w, _, finish, ok := api.beginChatSubmission(response, request(), "s", "p", "hello")
	if !ok {
		t.Fatal("acceptance failed")
	}
	_, _ = w.Write([]byte(`{"delivery_status":"sent_to_cli"}`))
	fail = true
	finish()
	if response.Code != 409 {
		t.Fatalf("lost outcome must be uncertain, got %d", response.Code)
	}
	fail = false
	replay := httptest.NewRecorder()
	if _, _, _, ok := api.beginChatSubmission(replay, request(), "s", "p", "hello"); ok || replay.Code != 409 {
		t.Fatal("lost acknowledgement cannot cause resend")
	}
}

func newTestChatSubmissionStore() *chatSubmissionStore {
	var mu sync.Mutex
	files := map[string]string{}
	return &chatSubmissionStore{
		resolveProject: func(owner, session string) (string, error) { return "", nil },
		read: func(_ context.Context, p string) (string, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			v, ok := files[p]
			return v, ok, nil
		},
		write: func(_ context.Context, p, v string) error { mu.Lock(); defer mu.Unlock(); files[p] = v; return nil },
	}
}

func TestChatSubmissionSameKeyIsIsolatedByOwner(t *testing.T) {
	api := &StreamingAPI{internalChatSubmissionStore: newTestChatSubmissionStore()}
	for _, owner := range []string{"alice", "bob"} {
		r := httptest.NewRequest("POST", "/api/query", nil)
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, &UserClaims{UserID: owner}))
		r.Header.Set("Idempotency-Key", "same-client-key")
		w, _, finish, ok := api.beginChatSubmission(httptest.NewRecorder(), r, "session-"+owner, "p", "hello")
		if !ok {
			t.Fatalf("another owner consumed key for %s", owner)
		}
		_, _ = w.Write([]byte(`{"status":"started"}`))
		finish()
	}
}

func TestChatSubmissionColdProjectUsesDurableReceipt(t *testing.T) {
	store := newTestChatSubmissionStore()
	store.resolveProject = func(owner, session string) (string, error) { return "project-a", nil }
	api := &StreamingAPI{internalChatSubmissionStore: store}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/sessions/s/live-input", nil)
		r.Header.Set("Idempotency-Key", "cold-project")
		return r.WithContext(context.WithValue(r.Context(), chatSubmissionUnknownProjectKey{}, true))
	}
	first := httptest.NewRecorder()
	w, _, finish, ok := api.beginChatSubmission(first, request(), "s", "", "hello")
	if !ok {
		t.Fatal("durable project resolution failed")
	}
	_, _ = w.Write([]byte(`{"delivery_status":"sent_to_cli"}`))
	finish()
	store.resolveProject = func(owner, session string) (string, error) { return "", errors.New("runtime temporarily unavailable") }
	restarted := &StreamingAPI{internalChatSubmissionStore: store}
	replay := httptest.NewRecorder()
	if _, _, _, ok := restarted.beginChatSubmission(replay, request(), "s", "", "hello"); ok || replay.Code != 200 {
		t.Fatalf("cold replay lost durable project: %d %s", replay.Code, replay.Body.String())
	}
}

func TestChatSubmissionPanicPreservesUncertainReceipt(t *testing.T) {
	api := &StreamingAPI{internalChatSubmissionStore: newTestChatSubmissionStore()}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/query", nil)
		r.Header.Set("Idempotency-Key", "panic-boundary")
		return r
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("handler panic was swallowed")
			}
		}()
		w, _, finish, ok := api.beginChatSubmission(httptest.NewRecorder(), request(), "s", "p", "hello")
		if !ok {
			t.Fatal("acceptance failed")
		}
		defer finish()
		_, _ = w.Write([]byte(`{"status":"started"}`))
		panic("crash before dispatch")
	}()
	replay := httptest.NewRecorder()
	if _, _, _, ok := api.beginChatSubmission(replay, request(), "s", "p", "hello"); ok || replay.Code != 409 {
		t.Fatalf("panic produced false acknowledgement: %d", replay.Code)
	}
}

func TestChatSubmissionInheritedTokenCannotAuthorizeAnotherConversation(t *testing.T) {
	api := &StreamingAPI{internalChatSubmissionStore: newTestChatSubmissionStore()}
	r := httptest.NewRequest("POST", "/api/query", nil)
	r.Header.Set("Idempotency-Key", "nested")
	_, nested, _, ok := api.beginChatSubmission(httptest.NewRecorder(), r, "first", "p", "hello")
	if !ok {
		t.Fatal("acceptance failed")
	}
	rejected := httptest.NewRecorder()
	if _, _, _, ok := api.beginChatSubmission(rejected, nested, "other", "p", "hello"); ok || rejected.Code != 409 {
		t.Fatal("inherited token bypassed another conversation's durable acceptance")
	}
}
