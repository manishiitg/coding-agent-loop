package agent

import (
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestReattachingASkillNameReplacesRatherThanDuplicating(t *testing.T) {
	w := &LLMAgentWrapper{}
	first := &llmtypes.Skill{Name: "browser-performance-validation", Description: "first"}
	second := &llmtypes.Skill{Name: "browser-performance-validation", Description: "second"}

	if err := w.AttachSkill(first); err != nil {
		t.Fatalf("first attachment: %v", err)
	}
	if err := w.AttachSkill(second); err != nil {
		t.Fatalf("second attachment: %v", err)
	}

	attached := w.AttachedSkills()
	if len(attached) != 1 {
		t.Fatalf("definition carries %d skills, want 1", len(attached))
	}
	if attached[0] != second || attached[0].Description != "second" {
		t.Fatalf("attached skill = %#v, want the later definition", attached[0])
	}
}

func TestReattachingASkillPreservesSurroundingSkills(t *testing.T) {
	w := &LLMAgentWrapper{}
	for _, name := range []string{"builder-reference", "browser-performance-validation", "workflow-commands"} {
		if err := w.AttachSkill(&llmtypes.Skill{Name: name, Description: "v1"}); err != nil {
			t.Fatalf("attach %s: %v", name, err)
		}
	}
	if err := w.AttachSkill(&llmtypes.Skill{Name: "browser-performance-validation", Description: "v2"}); err != nil {
		t.Fatalf("reattach: %v", err)
	}

	attached := w.AttachedSkills()
	if len(attached) != 3 {
		t.Fatalf("definition carries %d skills, want 3", len(attached))
	}
	want := []string{"builder-reference", "browser-performance-validation", "workflow-commands"}
	for i, name := range want {
		if attached[i].Name != name {
			t.Errorf("attached[%d] = %q, want %q", i, attached[i].Name, name)
		}
	}
	if attached[1].Description != "v2" {
		t.Errorf("replacement description = %q, want v2", attached[1].Description)
	}
}

func TestSkillAttachmentAfterFinalizeIsStillRejected(t *testing.T) {
	w := &LLMAgentWrapper{finalized: true}
	if err := w.AttachSkill(&llmtypes.Skill{Name: "late"}); err == nil {
		t.Fatal("skill attachment succeeded after finalize")
	}
}
