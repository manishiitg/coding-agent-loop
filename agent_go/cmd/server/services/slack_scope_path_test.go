package services

import "testing"

func TestSameSlackScopePath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"_users/u1/Chats/Work/projects/sde", "Chats/Work/projects/sde", true},
		{"Chats/Work/projects/sde", "_users/u1/Chats/Work/projects/sde/", true},
		{"Workflow/reports", "Workflow/reports", true},
		{"_users/u1/Chats/Work/projects/sde", "_users/u2/Chats/Work/projects/sde", false},
		{"Chats/Work/projects/sde", "Chats/Work/projects/other", false},
		{"_users/u1/Chats/Work/projects/sde", "", false},
	}
	for _, tc := range cases {
		if got := SameSlackScopePath(tc.a, tc.b); got != tc.want {
			t.Errorf("SameSlackScopePath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
