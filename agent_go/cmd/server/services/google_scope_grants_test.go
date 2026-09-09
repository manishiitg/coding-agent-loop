package services

import "testing"

func TestGoogleScopesGrantServiceAccess(t *testing.T) {
	for name, service := range googleServiceCatalog {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				granted  []string
				required string
				want     bool
			}{
				{"read permits read", []string{service.ReadScope}, service.ReadScope, true},
				{"write permits read", []string{service.WriteScope}, service.ReadScope, true},
				{"write permits write", []string{service.WriteScope}, service.WriteScope, true},
				{"read cannot write", []string{service.ReadScope}, service.WriteScope, false},
				{"missing grant", nil, service.ReadScope, false},
				{"file access is not account access", []string{"https://www.googleapis.com/auth/drive.file"}, service.ReadScope, false},
				{"unrelated grant", []string{"https://www.googleapis.com/auth/gmail.modify"}, service.ReadScope, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := GoogleScopesGrant(tc.granted, tc.required); got != tc.want {
						t.Fatalf("GoogleScopesGrant(%v, %q) = %v, want %v", tc.granted, tc.required, got, tc.want)
					}
				})
			}
		})
	}
}

func TestGoogleScopesGrantGmailRead(t *testing.T) {
	const read = "https://www.googleapis.com/auth/gmail.readonly"
	for _, tc := range []struct {
		scope string
		want  bool
	}{
		{read, true},
		{"https://www.googleapis.com/auth/gmail.modify", true},
		{"https://mail.google.com/", true},
		{"https://www.googleapis.com/auth/gmail.send", false},
		{"https://www.googleapis.com/auth/gmail.metadata", false},
		{"https://www.googleapis.com/auth/drive", false},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			if got := GoogleScopesGrant([]string{"openid", tc.scope}, read); got != tc.want {
				t.Fatalf("GoogleScopesGrant(%q, gmail.readonly) = %v, want %v", tc.scope, got, tc.want)
			}
		})
	}
}

func TestGoogleScopesGrantDriveBackedDocuments(t *testing.T) {
	drive := googleServiceCatalog["drive"]
	for _, name := range []string{"sheets", "docs", "slides"} {
		t.Run(name, func(t *testing.T) {
			service := googleServiceCatalog[name]
			if !GoogleScopesGrant([]string{drive.WriteScope}, service.ReadScope) ||
				!GoogleScopesGrant([]string{drive.WriteScope}, service.WriteScope) ||
				!GoogleScopesGrant([]string{drive.ReadScope}, service.ReadScope) {
				t.Fatal("broader Drive grants should cover document access")
			}
			if GoogleScopesGrant([]string{drive.ReadScope}, service.WriteScope) {
				t.Fatal("read-only Drive must not count as document write access")
			}
		})
	}
	if GoogleScopesGrant([]string{drive.WriteScope}, googleServiceCatalog["calendar"].ReadScope) {
		t.Fatal("Drive must not grant Calendar access")
	}
}
