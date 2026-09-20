package accesstokens

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecyclePersistenceAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "tokens.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	ctx := context.Background()
	record, raw, err := s.Issue(ctx, Token{Name: "Claude", UserID: "owner", Scopes: []string{"workflows:read"}, AllWorkflows: true, ExpiresAt: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, raw, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, raw, now.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatal("expired token accepted", err)
	}
	if err = s.Revoke(ctx, record.ID, "other", now); !errors.Is(err, ErrInvalid) {
		t.Fatal("another owner could revoke", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	records, err := s.List(ctx, "owner")
	if err != nil || len(records) != 1 || records[0].LastUsedAt == nil {
		t.Fatalf("metadata missing: %+v %v", records, err)
	}
	// Concurrent last-used writes must not undo revocation.
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); _, _ = s.Authenticate(ctx, raw, now) }()
	}
	if err = s.Revoke(ctx, record.ID, "owner", now); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if _, err = s.Authenticate(ctx, raw, now); !errors.Is(err, ErrInvalid) {
		t.Fatal("revoked token accepted", err)
	}
	for _, p := range []string{path, path + "-wal"} {
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), raw) {
			t.Fatal("plaintext token persisted")
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatal("database permissions", info.Mode())
	}
}
func TestWriteScopesRejected(t *testing.T) {
	now := time.Now()
	for _, scopes := range [][]string{
		append([]string{}, Scopes...),
		{"workflows:read", "files:read", "files:write"},
		{"workflows:read", "plan:write"},
		{"builder:chat"},
	} {
		tkn := Token{Name: "test", UserID: "u", Scopes: scopes, AllWorkflows: true, ExpiresAt: now.Add(time.Hour)}
		if Validate(tkn, now) == nil {
			t.Fatalf("write scopes accepted: %v", scopes)
		}
	}
	tkn := Token{Name: "test", UserID: "u", Scopes: []string{"workflows:read", "files:read"}, AllWorkflows: true, ExpiresAt: now.Add(time.Hour)}
	if err := Validate(tkn, now); err != nil {
		t.Fatal(err)
	}
}
