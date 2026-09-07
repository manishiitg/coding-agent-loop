package server

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUIControlHTTPPlanReceiptRoundTrip(t *testing.T) {
	ws := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		"Workflow/test/workflow.json": `{"id":"wf-test","label":"Test","created_by":"user-a","access":{"owners":["user-a"]}}`,
	}})
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"s": {SessionID: "s", UserID: "user-a"}}}
	b := api.uiBroker()
	b.setScope("s", "Workflow/test")
	call := func(body map[string]interface{}) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := mux.SetURLVars(requestWithUserForSessionAccess("user-a"), map[string]string{"session_id": "s"})
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		w := httptest.NewRecorder()
		api.handleUIControl(w, r)
		return w
	}
	w := call(map[string]interface{}{"operation": "bind", "version": 1})
	if w.Code != 200 {
		t.Fatalf("bind: %d %s", w.Code, w.Body.String())
	}
	var binding map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	request := func(op string, extra map[string]interface{}) *httptest.ResponseRecorder {
		body := map[string]interface{}{"operation": op, "version": 1, "binding": binding["binding"], "token": binding["token"]}
		for k, v := range extra {
			body[k] = v
		}
		return call(body)
	}
	a, _, err := b.submit("s", "flow", "open", "livekit-quality", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	w = request("sync", map[string]interface{}{"state": uiSnapshot{View: "flow", Revision: 1, Visible: true}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), a.RequestID) {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	w = request("ack", map[string]interface{}{"request_id": a.RequestID, "status": "applied", "state": uiSnapshot{View: "flow", Revision: 2, Visible: true}})
	if w.Code == 200 {
		t.Fatal("view shell alone must not acknowledge selected step")
	}
	w = request("ack", map[string]interface{}{"request_id": a.RequestID, "status": "applied", "state": uiSnapshot{View: "flow", Target: "livekit-quality", Revision: 2, Visible: true}})
	if w.Code != 200 {
		t.Fatalf("ack: %d %s", w.Code, w.Body.String())
	}
	receipt, err := b.result("s", a.RequestID)
	if err != nil || receipt.Status != "applied" || !receipt.Visible {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
}

func TestUIControlHTTPRejectsForeignAndOwnerlessSessions(t *testing.T) {
	for _, owner := range []string{"other-user", ""} {
		api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"s": {SessionID: "s", UserID: owner}}}
		r := mux.SetURLVars(requestWithUserForSessionAccess("user-a"), map[string]string{"session_id": "s"})
		w := httptest.NewRecorder()
		api.handleUIControl(w, r)
		if w.Code != 403 {
			t.Fatalf("owner %q: %d", owner, w.Code)
		}
	}
}

func TestUIControlScopeChangeInvalidatesBindings(t *testing.T) {
	b, c := boundUI(t)
	b.setScope(c.session, "Workflow/first")
	a, _, _ := b.submit(c.session, "flow", "open", "", "", nil)
	b.setScope(c.session, "Workflow/second")
	if _, err := b.syncClient(c.session, c.id, c.token, uiSnapshot{}); err == nil {
		t.Fatal("old workspace binding survived")
	}
	r, _ := b.result(c.session, a.RequestID)
	if r.Status != "cancelled" {
		t.Fatal(r)
	}
}

func boundUI(t *testing.T) (*uiControlBroker, *uiBinding) {
	t.Helper()
	b := newUIControlBroker()
	c, err := b.bind("session-a")
	if err != nil {
		t.Fatal(err)
	}
	return b, c
}
func TestUIControlOnlyAdvertisesActualActions(t *testing.T) {
	if len(uiControlContract.Views) != 22 {
		t.Fatal("registry coverage changed")
	}
	for _, v := range uiControlContract.Views {
		if err := validateUIAction(v.ID, "open", ""); err != nil {
			t.Fatal(err)
		}
		if err := validateUIAction(v.ID, "delete", ""); err == nil {
			t.Fatal("mutation advertised")
		}
		if err := validateUIAction(v.ID, "refresh", ""); err == nil {
			t.Fatal("unverified refresh advertised")
		}
		if err := validateUIAction(v.ID, "open", "arbitrary target"); err == nil && v.ID != "flow" && v.ID != "report" && v.ID != "files" {
			t.Fatal("ignored target")
		}
	}
	if validateUIAction("files", "open", "code/shared/helpers.py") != nil {
		t.Fatal("workspace-relative file target missing")
	}
	for _, invalid := range []string{"/etc/passwd", "../other/file", `code\\file.py`} {
		if err := validateUIAction("files", "open", invalid); err == nil {
			t.Fatalf("unsafe file target accepted: %q", invalid)
		}
	}
	if validateUIAction("notify", "expand", "pulse_review") != nil {
		t.Fatal("notify disclosure missing")
	}
}
func TestUIControlDisconnectedAndAmbiguousClients(t *testing.T) {
	b := newUIControlBroker()
	if _, _, err := b.submit("session-a", "flow", "open", "", "", nil); err == nil || err.Error() != "browser_disconnected" {
		t.Fatal(err)
	}
	_, _ = b.bind("session-a")
	_, _ = b.bind("session-a")
	if _, _, err := b.submit("session-a", "flow", "open", "", "", nil); err == nil || err.Error() != "ambiguous_client" {
		t.Fatal(err)
	}
}
func TestUIControlDeduplicationAndReceiptIsolation(t *testing.T) {
	b, c := boundUI(t)
	a, fresh, err := b.submit(c.session, "notify", "expand", "run_summary", "same", nil)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	duplicate, fresh, err := b.submit(c.session, "notify", "expand", "run_summary", "same", nil)
	if err != nil || fresh || duplicate.RequestID != a.RequestID {
		t.Fatal("not idempotent")
	}
	if _, _, err := b.submit(c.session, "flow", "open", "", "same", nil); err == nil {
		t.Fatal("key reused for different action")
	}
	if _, err := b.result("other-session", a.RequestID); err == nil {
		t.Fatal("cross-session result")
	}
	state := uiSnapshot{View: "notify", Revision: 1, Visible: true}
	if _, err := b.syncClient("other-session", c.id, c.token, state); err == nil {
		t.Fatal("cross-session claim")
	}
	if _, err := b.syncClient(c.session, c.id, "wrong-token", state); err == nil {
		t.Fatal("unauthenticated claim")
	}
	if err := b.ack(c.session, c.id, c.token, a.RequestID, "applied", "", state); err == nil {
		t.Fatal("ack before claim")
	}
	commands, err := b.syncClient(c.session, c.id, c.token, state)
	if err != nil || len(commands) != 1 {
		t.Fatal(err, len(commands))
	}
	commands, _ = b.syncClient(c.session, c.id, c.token, state)
	if len(commands) != 0 {
		t.Fatal("duplicate claim on reconnect")
	}
	if err := b.ack(c.session, c.id, c.token, a.RequestID, "applied", "", uiSnapshot{View: "flow", Revision: 1, Visible: true}); err == nil {
		t.Fatal("wrong view accepted")
	}
	if err := b.ack(c.session, c.id, c.token, a.RequestID, "applied", "", state); err != nil {
		t.Fatal(err)
	}
	result, _ := b.result(c.session, a.RequestID)
	if result.Status != "applied" || !result.Visible {
		t.Fatal(result)
	}
	duplicate, _, _ = b.submit(c.session, "notify", "expand", "run_summary", "same", nil)
	if duplicate.Status != "applied" {
		t.Fatal("completed retry executed")
	}
}
func TestUIControlRevisionAndLeaseExpiry(t *testing.T) {
	b, c := boundUI(t)
	now := time.Now()
	b.now = func() time.Time { return now }
	_, _ = b.syncClient(c.session, c.id, c.token, uiSnapshot{View: "flow", Revision: 2})
	old := int64(1)
	if _, _, err := b.submit(c.session, "flow", "open", "", "", &old); err == nil || err.Error() != "stale_state" {
		t.Fatal(err)
	}
	current := int64(2)
	a, _, _ := b.submit(c.session, "flow", "open", "", "", &current)
	commands, err := b.syncClient(c.session, c.id, c.token, uiSnapshot{View: "flow", Revision: 3})
	if err != nil || len(commands) != 0 {
		t.Fatal(err)
	}
	r, _ := b.result(c.session, a.RequestID)
	if r.Code != "stale_state" {
		t.Fatal(r)
	}
	a, _, _ = b.submit(c.session, "flow", "open", "", "", nil)
	now = now.Add(11 * time.Second)
	r, _ = b.result(c.session, a.RequestID)
	if r.Status != "expired" || r.Visible {
		t.Fatal(r)
	}
	now = now.Add(10 * time.Minute)
	if _, err := b.result(c.session, a.RequestID); err == nil {
		t.Fatal("unbounded retention")
	}
	if _, err := b.snapshot(c.session); err == nil {
		t.Fatal("expired browser")
	}
}
func TestUIControlUnbindCancelsWithoutReplay(t *testing.T) {
	b, c := boundUI(t)
	a, _, _ := b.submit(c.session, "flow", "open", "", "", nil)
	if err := b.unbind(c.session, c.id, c.token); err != nil {
		t.Fatal(err)
	}
	r, _ := b.result(c.session, a.RequestID)
	if r.Status != "cancelled" {
		t.Fatal(r)
	}
	newClient, _ := b.bind(c.session)
	commands, _ := b.syncClient(c.session, newClient.id, newClient.token, uiSnapshot{})
	if len(commands) != 0 {
		t.Fatal("replayed old binding")
	}
}
func TestUIControlStateIsBoundedAndRedacted(t *testing.T) {
	b, c := boundUI(t)
	if _, err := b.syncClient(c.session, c.id, c.token, uiSnapshot{View: "SECRET_VALUE"}); err == nil {
		t.Fatal("unbounded state")
	}
	a, _, _ := b.submit(c.session, "secrets", "open", "", "", nil)
	text := uiJSON(a)
	if strings.Contains(text, c.token) || strings.Contains(text, c.id) || strings.Contains(text, c.session) {
		t.Fatal("private binding leaked")
	}
}
