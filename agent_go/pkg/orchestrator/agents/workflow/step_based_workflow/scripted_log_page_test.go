package step_based_workflow

import (
	"strings"
	"testing"
)

func TestScriptedLogPagePreservesUnicodeAndProvidesContinuation(t *testing.T) {
	text := strings.Repeat("界", 12001)
	first := scriptedLogPage(text, 0)
	if !strings.Contains(first, "log_offset=12000") || strings.Count(first, "界") != 12000 {
		t.Fatal("missing pagination or corrupted output")
	}
	last := scriptedLogPage(text, 12000)
	if strings.Count(last, "界") != 1 || strings.Contains(last, "More output") {
		t.Fatal("incorrect final page")
	}
}
