package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/clisecurity"
)

func TestCLISecurityRoutesDefaultCompatibilityAndFailClosedStrictMode(t *testing.T) {
	store, err := clisecurity.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	router := muxForCLISecurityTest(store)

	get := httptest.NewRequest(http.MethodGet, "/cli-security", nil)
	get = get.WithContext(context.WithValue(get.Context(), UserContextKey, &UserClaims{UserID: "user-1"}))
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	var status cliSecurityStatus
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Config.Mode != "compatibility" {
		t.Fatalf("default mode = %q", status.Config.Mode)
	}

	body := bytes.NewBufferString(`{"version":1,"mode":"verified","approved_profiles":{}}`)
	put := httptest.NewRequest(http.MethodPut, "/cli-security", body)
	put = put.WithContext(context.WithValue(put.Context(), UserContextKey, &UserClaims{UserID: "user-1"}))
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, put)
	if putRecorder.Code != http.StatusBadRequest {
		t.Fatalf("PUT strict status = %d, want 400; body = %s", putRecorder.Code, putRecorder.Body.String())
	}

	approved := bytes.NewBufferString(`{"version":1,"mode":"verified","approved_profiles":{"codex-cli":{"profile_version":"1","capabilities":["provider_identity"]}}}`)
	approvedPut := httptest.NewRequest(http.MethodPut, "/cli-security", approved)
	approvedPut = approvedPut.WithContext(context.WithValue(approvedPut.Context(), UserContextKey, &UserClaims{UserID: "user-1"}))
	approvedRecorder := httptest.NewRecorder()
	router.ServeHTTP(approvedRecorder, approvedPut)
	if approvedRecorder.Code != http.StatusOK {
		t.Fatalf("PUT approved Codex status = %d, body = %s", approvedRecorder.Code, approvedRecorder.Body.String())
	}
}

func muxForCLISecurityTest(store *clisecurity.Store) *mux.Router {
	router := mux.NewRouter()
	CLISecurityRoutes(router, store)
	return router
}
