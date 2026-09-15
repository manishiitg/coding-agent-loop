package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListWorkspaceFilesRecursiveDeduplicatesWorkspaceListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"filepath":"Workflow/demo/costs","type":"folder","children":[{"filepath":"Workflow/demo/costs/day.json","type":"file"}]},{"filepath":"Workflow/demo/costs/day.json","type":"file"}]}`))
	}))
	defer server.Close()

	previous := getWorkspaceAPIURL()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	if getWorkspaceAPIURL() == previous {
		t.Skip("workspace API URL is fixed by this test environment")
	}

	got, err := listWorkspaceFilesRecursive(context.Background(), "Workflow/demo/costs")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Workflow/demo/costs/day.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}
