package commands

import "testing"

func TestEnsureContextPlaceholder(t *testing.T) {
	in := "---\nname: daily\ndescription: Daily\n---\nUpdate the Daily page.\n"
	got := EnsureContextPlaceholder(in)
	if got != "---\nname: daily\ndescription: Daily\n---\nUpdate the Daily page.\n\n{{context}}\n" {
		t.Fatalf("placeholder not appended: %q", got)
	}
	if EnsureContextPlaceholder(got) != got {
		t.Fatal("a command that already uses {{context}} must be unchanged")
	}
}
