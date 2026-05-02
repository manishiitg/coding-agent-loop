package browser

import "testing"

func TestParseTabSelection(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantTab   string
		wantClear bool
		wantErr   bool
	}{
		{name: "list tabs", args: nil},
		{name: "select existing tab", args: []string{"t1"}, wantTab: "t1"},
		{name: "select labeled tab", args: []string{"daily-post"}, wantTab: "daily-post"},
		{name: "new labeled tab", args: []string{"new", "--label", "daily-post", "https://example.com"}, wantTab: "daily-post"},
		{name: "new tab requires label", args: []string{"new", "https://example.com"}, wantErr: true},
		{name: "close selected tab", args: []string{"close", "daily-post"}, wantTab: "daily-post", wantClear: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTab, gotClear, err := parseTabSelection(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotTab != tt.wantTab || gotClear != tt.wantClear {
				t.Fatalf("parseTabSelection() = (%q, %v), want (%q, %v)", gotTab, gotClear, tt.wantTab, tt.wantClear)
			}
		})
	}
}

func TestStripCDPArgs(t *testing.T) {
	got := stripCDPArgs([]string{"--cdp", "http://localhost:9222", "new", "--label", "daily-post"})
	want := []string{"new", "--label", "daily-post"}
	if len(got) != len(want) {
		t.Fatalf("stripCDPArgs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stripCDPArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
