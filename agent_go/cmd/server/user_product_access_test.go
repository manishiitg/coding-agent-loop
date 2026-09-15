package server

import (
	"net/http/httptest"
	"testing"
)

func withUserProductAccessFile(t *testing.T, content string) {
	t.Helper()
	workspace := &mockWorkspaceAPI{files: map[string]string{}}
	if content != "" {
		workspace.files[userProductAccessFilePath()] = content
	}
	server := httptest.NewServer(workspace)
	t.Cleanup(server.Close)
	t.Setenv("WORKSPACE_API_URL", server.URL)
}

func TestUserAllowedProductDefaultsToUnrestrictedWhenFileAbsent(t *testing.T) {
	withUserProductAccessFile(t, "")

	claims := &UserClaims{UserID: "u1", Username: "john"}
	if !userAllowedProduct(claims, "agentworks") {
		t.Fatal("user with no config entry should be unrestricted")
	}
	if !userAllowedWorkflowID(claims, "tectonicusadaytrading") {
		t.Fatal("user with no config entry should see every workflow")
	}
}

func TestAdminOnlyProductsAreHiddenAndRejectedForMembers(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	t.Setenv("AGENTWORKS_ADMIN_ONLY_PRODUCT_SURFACES", " work ")
	withMemoryUserDirectory(t, `{"users":[
		{"id":"admin","username":"admin","admin":true,"can_create":true,"products":[]},
		{"id":"member","username":"member","can_create":true,"products":[]}
	]}`)

	admin := &UserClaims{UserID: "admin", Username: "admin"}
	member := &UserClaims{UserID: "member", Username: "member"}
	if !userAllowedProduct(admin, "work") {
		t.Fatal("admin must retain access to an admin-only product")
	}
	if userAllowedProduct(member, "work") {
		t.Fatal("member must not reach an admin-only product")
	}
	if !userAllowedProduct(member, "agentworks") {
		t.Fatal("admin-only Work must not remove the member's AgentWorks access")
	}
	products, ok := productAccessResponseFields(member)["allowed_products"].([]string)
	if !ok {
		t.Fatal("member must receive an explicit frontend product allowlist")
	}
	for _, product := range products {
		if product == "work" {
			t.Fatalf("member frontend allowlist contains admin-only Work: %v", products)
		}
	}
	if got := productAccessResponseFields(admin)["allowed_products"]; got != nil {
		t.Fatalf("admin should remain unrestricted, got %v", got)
	}
}

func TestProductsAvailableToAllAugmentReadOnlyAccounts(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	t.Setenv("AGENTWORKS_PRODUCTS_AVAILABLE_TO_ALL", " work ")
	withMemoryUserDirectory(t, `{"users":[
		{"id":"reader","username":"reader","can_create":false,"products":["agentworks"]},
		{"id":"new-reader","username":"new-reader","can_create":false,"products":[]}
	]}`)

	for _, userID := range []string{"reader", "new-reader"} {
		claims := &UserClaims{UserID: userID, Username: userID}
		if !userAllowedProduct(claims, "work") {
			t.Fatalf("%s should receive deployment-wide Work access", userID)
		}
		products, ok := productAccessResponseFields(claims)["allowed_products"].([]string)
		hasWork := false
		for _, product := range products {
			hasWork = hasWork || product == "work"
		}
		if !ok || !hasWork {
			t.Fatalf("%s frontend products = %v, want Work", userID, products)
		}
	}
	if userAllowedProduct(&UserClaims{UserID: "new-reader", Username: "new-reader"}, "dominion") {
		t.Fatal("deployment-wide Work access must not unlock unrelated products")
	}
}

func TestUserAllowedProductRestrictsExplicitEntry(t *testing.T) {
	withUserProductAccessFile(t, `{
		"john": { "products": ["dominion"] },
		"manish": { "products": ["dominion", "agentworks"], "workflow_ids": ["tectonicusadaytrading"] }
	}`)

	john := &UserClaims{UserID: "u-john", Username: "john"}
	if !userAllowedProduct(john, "dominion") {
		t.Fatal("john should be allowed dominion")
	}
	if userAllowedProduct(john, "agentworks") {
		t.Fatal("john should not be allowed agentworks")
	}
	// john has no workflow_ids entry, so workflow access is unrestricted --
	// products and workflows are independent narrowings.
	if !userAllowedWorkflowID(john, "tectonicusadaytrading") {
		t.Fatal("john with no workflow_ids entry should be unrestricted for workflows")
	}

	manish := &UserClaims{UserID: "u-manish", Username: "manish"}
	if !userAllowedProduct(manish, "agentworks") {
		t.Fatal("manish should be allowed agentworks")
	}
	if !userAllowedWorkflowID(manish, "tectonicusadaytrading") {
		t.Fatal("manish should be allowed tectonicusadaytrading")
	}
	if userAllowedWorkflowID(manish, "some-other-workflow") {
		t.Fatal("manish should not be allowed a workflow outside his explicit list")
	}
}

func TestUserAllowedProductMatchesByUsernameCaseInsensitive(t *testing.T) {
	withUserProductAccessFile(t, `{ "John": { "products": ["dominion"] } }`)

	claims := &UserClaims{UserID: "u-john", Username: "john"}
	if userAllowedProduct(claims, "agentworks") {
		t.Fatal("normalized username match should still restrict")
	}
	if !userAllowedProduct(claims, "dominion") {
		t.Fatal("normalized username match should still allow the granted product")
	}
}

func TestFilterWorkflowManifestsForUserNarrowsList(t *testing.T) {
	withUserProductAccessFile(t, `{ "manish": { "products": ["dominion", "agentworks"], "workflow_ids": ["tectonicusadaytrading"] } }`)

	discovered := []DiscoveredWorkflow{
		{WorkspacePath: "Workflow/tectonicusadaytrading", Manifest: &WorkflowManifest{ID: "tectonicusadaytrading"}},
		{WorkspacePath: "Workflow/other", Manifest: &WorkflowManifest{ID: "other"}},
	}

	manish := &UserClaims{UserID: "u-manish", Username: "manish"}
	filtered := filterWorkflowManifestsForUser(manish, discovered)
	if len(filtered) != 1 || filtered[0].Manifest.ID != "tectonicusadaytrading" {
		t.Fatalf("filtered = %+v, want only tectonicusadaytrading", filtered)
	}

	unrestricted := &UserClaims{UserID: "u-other", Username: "someone-else"}
	if got := filterWorkflowManifestsForUser(unrestricted, discovered); len(got) != 2 {
		t.Fatalf("unrestricted user should see every workflow, got %d", len(got))
	}
}

func TestProductAccessResponseFieldsOmitsWhenUnrestricted(t *testing.T) {
	withUserProductAccessFile(t, "")

	claims := &UserClaims{UserID: "u1", Username: "someone"}
	fields := productAccessResponseFields(claims)
	if fields["allowed_products"] != nil {
		t.Fatalf("allowed_products = %v, want nil for unrestricted user", fields["allowed_products"])
	}
	if fields["allowed_workflow_ids"] != nil {
		t.Fatalf("allowed_workflow_ids = %v, want nil for unrestricted user", fields["allowed_workflow_ids"])
	}
}
