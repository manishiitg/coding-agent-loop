package uiuxpromax

import (
	"strings"
	"testing"
)

func TestMaterializeProvidesPortableOptionalDesignSkill(t *testing.T) {
	skill, err := Materialize()
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "ui-ux-pro-max" || skill.Source.SourceURL != sourceURL {
		t.Fatalf("unexpected skill identity: %+v", skill)
	}
	for _, want := range []string{
		"optional design intelligence",
		"Choose the CSS framework",
		"reporting-policy.md",
		"UI_UX_SEARCH",
		"no projected script exists",
	} {
		if !strings.Contains(skill.Content, want) {
			t.Fatalf("skill content missing %q", want)
		}
	}
	files := map[string]bool{}
	for _, file := range skill.SupportingFiles {
		files[file.RelPath] = true
	}
	for _, want := range []string{"LICENSE.txt", "references/quick-reference.md", "references/pro-rules.md", "scripts/search.py", "data/charts.csv", "data/stacks/html-tailwind.csv"} {
		if !files[want] {
			t.Fatalf("skill bundle missing %q", want)
		}
	}
}
