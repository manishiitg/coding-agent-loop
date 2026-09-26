package wsauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportStampsTokenOnlyForWorkspaceHost(t *testing.T) {
	var seen string
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get(HeaderName) }))
	defer workspace.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get(HeaderName) }))
	defer other.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	t.Setenv("WORKSPACE_API_TOKEN", "tok-1")
	client := &http.Client{Transport: Transport(&http.Transport{})}

	if _, err := client.Get(workspace.URL + "/api/documents/x"); err != nil || seen != "tok-1" {
		t.Fatalf("workspace request token=%q err=%v", seen, err)
	}
	if _, err := client.Get(other.URL + "/x"); err != nil || seen != "" {
		t.Fatalf("token leaked to another host: %q err=%v", seen, err)
	}
	t.Setenv("WORKSPACE_API_TOKEN", "")
	if _, err := client.Get(workspace.URL + "/x"); err != nil || seen != "" {
		t.Fatalf("empty token must send nothing: %q", seen)
	}
}
