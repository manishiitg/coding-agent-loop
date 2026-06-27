package server

import "testing"

func TestParsePiCLIModelList(t *testing.T) {
	output := `provider       model                          context  max-out  thinking  images
google         gemini-3.5-flash               1.0M     65.5K    yes       yes
google         gemma-4-26b-a4b-it             262.1K   32.8K    yes       yes
google-vertex  gemini-3.5-flash               1.0M     65.5K    yes       yes
zai            glm-5.2                        1.0M     65.5K    yes       yes
minimax        MiniMax-M3                     512K     131K     yes       yes
kimi-coding    k2p7                           262.1K   32.8K    yes       yes
`

	models := parsePiCLIModelList(output)
	if len(models) != 6 {
		t.Fatalf("models len = %d, want 6: %#v", len(models), models)
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
	if models[3].ModelID != "zai/glm-5.2" || models[3].Group != "Z.AI" {
		t.Fatalf("zai model = %#v, want zai/glm-5.2 in Z.AI group", models[3])
	}
	if models[4].ModelID != "minimax/MiniMax-M3" || models[4].Group != "MiniMax" {
		t.Fatalf("minimax model = %#v, want minimax/MiniMax-M3 in MiniMax group", models[4])
	}
	if models[5].ModelID != "kimi-coding/k2p7" || models[5].Group != "Kimi" {
		t.Fatalf("kimi model = %#v, want kimi-coding/k2p7 in Kimi group", models[5])
	}
}

func TestPiFallbackModelsKeepProviderShortlistsSmall(t *testing.T) {
	counts := map[string]int{}
	for _, model := range piFallbackModels() {
		group := model.Group
		if group == "" {
			group = "Other"
		}
		counts[group]++
	}

	for _, group := range []string{"Recommended Gemini", "Z.AI", "MiniMax", "Kimi", "DeepSeek"} {
		if counts[group] == 0 {
			t.Fatalf("Pi shortlist group %q is empty: %#v", group, counts)
		}
		if counts[group] > 2 {
			t.Fatalf("Pi shortlist group %q has %d models, want at most 2", group, counts[group])
		}
	}
}

func TestFetchPiCLIModelsReturnsCuratedShortlist(t *testing.T) {
	resp := fetchPiCLIModels()
	if resp == nil {
		t.Fatal("fetchPiCLIModels returned nil")
	}
	if len(resp.Models) != len(piFallbackModels()) {
		t.Fatalf("Pi model count = %d, want curated count %d", len(resp.Models), len(piFallbackModels()))
	}
	if resp.SupportsCustom != true {
		t.Fatal("Pi should still support custom model IDs")
	}
}

func TestParsePiCLIModelListNoMatches(t *testing.T) {
	if got := parsePiCLIModelList(`No models matching "sonnet"`); len(got) != 0 {
		t.Fatalf("models len = %d, want 0: %#v", len(got), got)
	}
}

func TestMergePiModelEntriesKeepsCuratedModelsFirst(t *testing.T) {
	curated := piFallbackModels()
	listed := []dynamicModelEntry{
		{ModelID: "google/gemini-3.5-flash", Group: "Google"},
		{ModelID: "anthropic/claude-sonnet-4-6", Group: "Anthropic"},
	}
	merged := mergePiModelEntries(curated, listed)
	if len(merged) != len(curated)+1 {
		t.Fatalf("merged len = %d, want %d", len(merged), len(curated)+1)
	}
	if merged[0].ModelID != "google/gemini-3.5-flash" || !merged[0].IsDefault {
		t.Fatalf("first merged model = %#v, want curated default first", merged[0])
	}
	if merged[len(merged)-1].ModelID != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("last merged model = %#v, want listed non-curated model", merged[len(merged)-1])
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
