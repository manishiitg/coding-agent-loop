package step_based_workflow

import (
	"testing"
)

func TestResolveWorkshopVariableValuesAutoSelectsOnlyGroup(t *testing.T) {
	manifest := &VariablesManifest{
		Variables: []Variable{{Name: "BASE_URL", Value: "https://default.example"}},
		Groups:    []VariableGroup{{Name: "default", Values: map[string]string{"BASE_URL": "https://selected.example"}}},
	}
	values, group, ok := ResolveWorkshopVariableValues(manifest, nil)
	if !ok || group != "default" || values["BASE_URL"] != "https://selected.example" {
		t.Fatalf("resolved values = %v, group = %q, ok = %v", values, group, ok)
	}
}

func TestResolveWorkshopVariableValuesRequiresSelectionForMultipleGroups(t *testing.T) {
	manifest := &VariablesManifest{Groups: []VariableGroup{{Name: "one"}, {Name: "two"}}}
	if values, group, ok := ResolveWorkshopVariableValues(manifest, nil); ok || values != nil || group != "" {
		t.Fatalf("ambiguous groups must not resolve: values=%v group=%q ok=%v", values, group, ok)
	}
}
