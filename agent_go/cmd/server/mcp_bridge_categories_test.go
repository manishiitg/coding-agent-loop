package server

import "testing"

func TestMCPBridgeCategoryRouting(t *testing.T) {
	custom := []string{
		"human_tools",
		"human-tools",
		"workspace_tools",
		"workspace_advanced",
		"workflow",
		"auto_improvement",
		"knowledgebase_tools",
	}
	for _, name := range custom {
		if !isMCPBridgeCustomToolCategory(name) {
			t.Fatalf("expected %q to route to custom tool handler", name)
		}
	}

	if !isMCPBridgeVirtualToolCategory("memory") {
		t.Fatal("expected memory to route to virtual tool handler")
	}

	for _, name := range []string{"google_sheets", "playwright", "gmail"} {
		if isMCPBridgeCustomToolCategory(name) || isMCPBridgeVirtualToolCategory(name) {
			t.Fatalf("real MCP server %q must not be redirected as a built-in category", name)
		}
	}
}
