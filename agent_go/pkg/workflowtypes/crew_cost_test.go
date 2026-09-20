package workflowtypes

import "testing"

func TestCrewRunTokenUsageEmpty(t *testing.T) {
	var nilUsage *CrewRunTokenUsage
	if !nilUsage.Empty() {
		t.Fatal("nil usage must be empty")
	}
	if !(&CrewRunTokenUsage{}).Empty() {
		t.Fatal("zero usage must be empty")
	}
	if (&CrewRunTokenUsage{PromptTokens: 1}).Empty() {
		t.Fatal("usage with tokens must not be empty")
	}
}

func TestCrewRunTokenUsageAddModelKeepsTotalsInSync(t *testing.T) {
	usage := &CrewRunTokenUsage{}
	usage.AddModel("gpt-x", "prov", 10, 5, 0, 2, 1, 0.5, 1)
	usage.AddModel("gpt-x", "prov", 10, 5, 0, 2, 1, 0.5, 1)
	usage.AddModel("", "", 1, 0, 0, 0, 0, 0, 0)
	if usage.PromptTokens != 21 || usage.CompletionTokens != 10 || usage.LLMCallCount != 2 {
		t.Fatalf("totals = %+v", usage)
	}
	if usage.ByModel["gpt-x"] == nil || usage.ByModel["gpt-x"].PromptTokens != 20 {
		t.Fatalf("per-model split = %+v", usage.ByModel)
	}
	if usage.ByModel["unknown"] == nil {
		t.Fatalf("empty model must normalize to unknown: %+v", usage.ByModel)
	}
}

func TestCrewRunTokenUsageMerge(t *testing.T) {
	into := &CrewRunTokenUsage{}
	into.AddModel("gpt-x", "prov", 10, 5, 0, 0, 0, 0.5, 1)
	other := &CrewRunTokenUsage{}
	other.AddModel("gpt-y", "prov", 3, 1, 0, 0, 0, 0.1, 1)
	into.Merge(other)
	if into.PromptTokens != 13 || len(into.ByModel) != 2 {
		t.Fatalf("merged = %+v", into)
	}
	modelLess := &CrewRunTokenUsage{PromptTokens: 7}
	into.Merge(modelLess)
	if into.PromptTokens != 20 || into.ByModel["unknown"] == nil || into.ByModel["unknown"].PromptTokens != 7 {
		t.Fatalf("model-less merge = %+v", into)
	}
}
