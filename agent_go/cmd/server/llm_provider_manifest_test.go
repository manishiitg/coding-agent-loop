package server

import "testing"

func TestParsePiCLIModelList(t *testing.T) {
	output := `provider       model                          context  max-out  thinking  images
google         gemini-3.5-flash               1.0M     65.5K    yes       yes
google         gemma-4-26b-a4b-it             262.1K   32.8K    yes       yes
google-vertex  gemini-3.5-flash               1.0M     65.5K    yes       yes
`

	models := parsePiCLIModelList(output)
	if len(models) != 3 {
		t.Fatalf("models len = %d, want 3: %#v", len(models), models)
	}
	if models[0].ModelID != "google/gemini-3.5-flash" {
		t.Fatalf("first model id = %q", models[0].ModelID)
	}
	if !models[0].IsDefault {
		t.Fatal("google/gemini-3.5-flash should be marked default")
	}
	if models[0].ContextWindow != 1_000_000 {
		t.Fatalf("context = %d, want 1000000", models[0].ContextWindow)
	}
	if models[1].ContextWindow != 262_100 {
		t.Fatalf("context = %d, want 262100", models[1].ContextWindow)
	}
	if models[2].Group != "Google Vertex" {
		t.Fatalf("group = %q, want Google Vertex", models[2].Group)
	}
}

func TestParsePiCLIModelListNoMatches(t *testing.T) {
	if got := parsePiCLIModelList(`No models matching "sonnet"`); len(got) != 0 {
		t.Fatalf("models len = %d, want 0: %#v", len(got), got)
	}
}

func TestDynamicModelGroupsPreservesFirstSeenOrder(t *testing.T) {
	groups := dynamicModelGroups([]dynamicModelEntry{
		{Group: "Google"},
		{Group: "OpenAI"},
		{Group: "Google"},
		{},
	})
	want := []string{"Google", "OpenAI", "Other"}
	if len(groups) != len(want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
	for i := range want {
		if groups[i] != want[i] {
			t.Fatalf("groups = %#v, want %#v", groups, want)
		}
	}
}
