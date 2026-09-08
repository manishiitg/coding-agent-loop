package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// withMemoryUserDirectory swaps the workspace-backed file for an in-memory
// one for the duration of a test and clears the read cache around it.
func withMemoryUserDirectory(t *testing.T, initial string) *string {
	t.Helper()
	content := initial
	prevRead, prevWrite := userDirectoryRead, userDirectoryWrite
	userDirectoryRead = func() (string, bool, error) { return content, content != "", nil }
	userDirectoryWrite = func(c string) error { content = c; return nil }
	invalidateUserDirectoryCache()
	t.Cleanup(func() {
		userDirectoryRead, userDirectoryWrite = prevRead, prevWrite
		invalidateUserDirectoryCache()
	})
	return &content
}

func TestRegisteredProductIDsIncludesSparkQuill(t *testing.T) {
	t.Setenv("AGENT_PRODUCTS", "")
	found := false
	for _, id := range registeredProductIDs() {
		if id == "sparkquill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registeredProductIDs() = %v, want it to include sparkquill", registeredProductIDs())
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("unexpected hash format %q", h)
	}
	if !verifyPassword(h, "correct horse battery") {
		t.Fatal("right password rejected")
	}
	if verifyPassword(h, "wrong") || verifyPassword("garbage", "x") || verifyPassword("", "") {
		t.Fatal("wrong password or malformed hash accepted")
	}
}

func TestAccessForRecordProductSemantics(t *testing.T) {
	canEdit := true
	cases := []struct {
		name       string
		rec        UserRecord
		restricted bool
		products   int
		canCreate  bool
		canEdit    bool
	}{
		{"admin ignores list", UserRecord{Admin: true, Products: []string{"video-studio"}}, false, 0, true, true},
		{"member no list = all", UserRecord{CanCreate: true}, false, 0, true, true},
		{"member with list", UserRecord{CanCreate: true, Products: []string{"video-studio"}}, true, 1, true, true},
		{"contributor with product", UserRecord{CanEdit: &canEdit, Products: []string{"agentworks"}}, true, 1, false, true},
		{"read-only no list = none", UserRecord{}, true, 0, false, false},
		{"read-only with list", UserRecord{Products: []string{"agentworks"}}, true, 1, false, false},
	}
	for _, c := range cases {
		acc := accessForRecord(&c.rec)
		if acc.ProductsRestricted != c.restricted || len(acc.Products) != c.products || acc.CanCreate != c.canCreate || acc.CanEdit != c.canEdit {
			t.Fatalf("%s: got %+v", c.name, acc)
		}
	}
}

func TestDirectoryDrivesWorkflowTierAndProducts(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[
	  {"id":"a1","username":"alice","admin":true,"can_create":true,"products":[]},
	  {"id":"b2","username":"bob","admin":false,"can_create":true,"products":["video-studio"]},
	  {"id":"c3","username":"carol","admin":false,"can_create":false,"products":[]}
	]}`)
	if got := workflowAccessForIdentity("a1", "alice", ""); got != WorkflowAccessOwner {
		t.Fatalf("admin tier = %s", got)
	}
	if got := workflowAccessForIdentity("b2", "bob", ""); got != WorkflowAccessWrite {
		t.Fatalf("member tier = %s", got)
	}
	if got := workflowAccessForIdentity("", "carol", ""); got != WorkflowAccessRead {
		t.Fatalf("read-only tier (by username) = %s", got)
	}
	bob := &UserClaims{UserID: "b2", Username: "bob"}
	if !userAllowedProduct(bob, "video-studio") || userAllowedProduct(bob, "finance") {
		t.Fatal("bob's product list not applied")
	}
	carol := &UserClaims{UserID: "c3", Username: "carol"}
	if userAllowedProduct(carol, "video-studio") {
		t.Fatal("read-only user with no products must not open a product")
	}
	fields := productAccessResponseFields(carol)
	if list, ok := fields["allowed_products"].([]string); !ok || len(list) != 0 {
		t.Fatalf("read-only user must advertise an EMPTY allowed_products, got %#v", fields["allowed_products"])
	}
	if fields := productAccessResponseFields(&UserClaims{UserID: "a1", Username: "alice"}); fields["allowed_products"] != nil {
		t.Fatal("admin must be unrestricted (null)")
	}
	// An identity the directory does not know keeps the legacy default.
	if got := workflowAccessForIdentity("zz", "zed", ""); got != WorkflowAccessOwner {
		t.Fatalf("unknown identity should fall back to legacy owner default, got %s", got)
	}
}

func TestSingleUserModeOwnerIsAdmin(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	withMemoryUserDirectory(t, "")
	acc := userAccessForClaims(&UserClaims{UserID: "default", Username: "user"})
	if acc.Known || !acc.Admin || !acc.CanCreate {
		t.Fatalf("single-user owner should be admin: %+v", acc)
	}
}

func TestBootstrapImportsAuthUsersAndAdmins(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	t.Setenv("ADMIN_USERS", "root")
	prev := hardcodedUsersSource
	hardcodedUsersSource = func() map[string]*HardcodedUser {
		return map[string]*HardcodedUser{
			"root": {Username: "root", Password: "rootpass123", UserID: userIDForUsername("root")},
			"eve":  {Username: "eve", Password: "evepass1234", UserID: userIDForUsername("eve")},
		}
	}
	t.Cleanup(func() { hardcodedUsersSource = prev })
	content := withMemoryUserDirectory(t, "")
	bootstrapUserDirectory(context.Background())
	var f userDirectoryFile
	if err := json.Unmarshal([]byte(*content), &f); err != nil {
		t.Fatalf("users.json not written: %v", err)
	}
	if len(f.Users) != 2 {
		t.Fatalf("expected 2 imported users, got %d", len(f.Users))
	}
	byName := map[string]UserRecord{}
	for _, u := range f.Users {
		byName[u.Username] = u
	}
	if !byName["root"].Admin || byName["eve"].Admin {
		t.Fatalf("ADMIN_USERS not applied: %+v", byName)
	}
	if strings.Contains(*content, "rootpass123") {
		t.Fatal("plaintext password written to users.json")
	}
	if validateDirectoryCredentials("eve", "evepass1234") == nil || validateDirectoryCredentials("eve", "nope") != nil {
		t.Fatal("imported password does not verify")
	}
	// Second run is a no-op.
	before := *content
	bootstrapUserDirectory(context.Background())
	if *content != before {
		t.Fatal("bootstrap is not idempotent")
	}
}

func adminRequest(method, path, body string, claims *UserClaims, vars map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey, claims))
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	return req
}

func TestAdminUserCRUDAndGuards(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"a1","username":"alice","admin":true,"can_create":true,"products":[]}]}`)
	api := &StreamingAPI{}
	alice := &UserClaims{UserID: "a1", Username: "alice"}

	// create
	rec := httptest.NewRecorder()
	requireAdmin(api.handleAdminCreateUser)(rec, adminRequest(http.MethodPost, "/api/admin/users",
		`{"username":"dave","password":"davepass123","can_create":false,"can_edit":true,"products":["Video-Studio","video-studio"]}`, alice, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created userAdminView
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID != userIDForUsername("dave") || created.Admin || created.CanCreate || !created.CanEdit || len(created.Products) != 1 || !created.HasPassword {
		t.Fatalf("created view: %+v", created)
	}
	// duplicate
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminCreateUser)(rec, adminRequest(http.MethodPost, "/api/admin/users", `{"username":"dave","password":"davepass123"}`, alice, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d", rec.Code)
	}
	// dave (read-only) cannot use the admin API
	dave := &UserClaims{UserID: created.ID, Username: "dave"}
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminListUsers)(rec, adminRequest(http.MethodGet, "/api/admin/users", "", dave, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only user reached admin list: %d", rec.Code)
	}
	// update: disable dave, then the middleware-facing check sees it
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminUpdateUser)(rec, adminRequest(http.MethodPut, "/api/admin/users/"+created.ID, `{"disabled":true}`, alice, map[string]string{"id": created.ID}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	invalidateUserDirectoryCache()
	if !directoryUserIsDisabled(dave) {
		t.Fatal("disabled flag not visible to the middleware check")
	}
	// alice cannot demote or disable herself
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminUpdateUser)(rec, adminRequest(http.MethodPut, "/api/admin/users/a1", `{"admin":false}`, alice, map[string]string{"id": "a1"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self-demotion allowed: %d", rec.Code)
	}
	// delete dave; list shows alice only
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminDeleteUser)(rec, adminRequest(http.MethodDelete, "/api/admin/users/"+created.ID, "", alice, map[string]string{"id": created.ID}))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	requireAdmin(api.handleAdminListUsers)(rec, adminRequest(http.MethodGet, "/api/admin/users", "", alice, nil))
	var listed struct {
		Users    []userAdminView `json:"users"`
		Products []string        `json:"products"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Users) != 1 || listed.Users[0].Username != "alice" || len(listed.Products) == 0 {
		t.Fatalf("list after delete: %+v", listed)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	h, _ := hashPassword("oldpassword1")
	withMemoryUserDirectory(t, `{"users":[{"id":"b2","username":"bob","can_create":true,"products":[],"password_hash":"`+h+`"}]}`)
	api := &StreamingAPI{}
	bob := &UserClaims{UserID: "b2", Username: "bob"}
	rec := httptest.NewRecorder()
	api.handleChangeOwnPassword(rec, adminRequest(http.MethodPost, "/api/auth/password", `{"current_password":"wrong","new_password":"newpassword1"}`, bob, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong current password accepted: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	api.handleChangeOwnPassword(rec, adminRequest(http.MethodPost, "/api/auth/password", `{"current_password":"oldpassword1","new_password":"newpassword1"}`, bob, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	invalidateUserDirectoryCache()
	if validateDirectoryCredentials("bob", "newpassword1") == nil {
		t.Fatal("new password does not verify")
	}
	// The account menu does not ask for the current password: a signed-in
	// session is the proof of identity, so an empty current password is fine.
	rec = httptest.NewRecorder()
	api.handleChangeOwnPassword(rec, adminRequest(http.MethodPost, "/api/auth/password", `{"new_password":"newpassword2"}`, bob, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("change without current password: %d %s", rec.Code, rec.Body.String())
	}
	invalidateUserDirectoryCache()
	if validateDirectoryCredentials("bob", "newpassword2") == nil {
		t.Fatal("password set without the current one does not verify")
	}
}
