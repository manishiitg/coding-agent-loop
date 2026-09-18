package server

import "testing"

func TestSlackCLIReadValidation(t *testing.T) {
	for _, tc := range []struct {
		method string
		params map[string]interface{}
	}{
		{"chat.delete", nil}, {"conversations.history", map[string]interface{}{"token": "secret"}},
		{"conversations.history", map[string]interface{}{"channel": "COTHER"}},
		{"conversations.history", map[string]interface{}{"limit": 1000.}},
		{"conversations.replies", nil},
	} {
		if _, err := validateSlackCLIRead(tc.method, "C123", tc.params); err == nil {
			t.Fatalf("accepted unsafe call: %s %v", tc.method, tc.params)
		}
	}
	args, err := validateSlackCLIRead("conversations.replies", "C123", map[string]interface{}{"ts": "1789711468.455929", "limit": 20.})
	if err != nil || args["channel"] != "C123" || args["limit"] != 20 {
		t.Fatalf("valid read failed: %v %v", args, err)
	}
}
