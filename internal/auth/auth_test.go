package auth

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/paulhovey/roost/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "d"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLifecycle(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	secret, rec, err := Create(ctx, st, "svc", []string{"legal"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, Prefix) || len(secret) < 40 {
		t.Fatalf("secret = %q", secret)
	}
	if rec.Hash == secret || strings.Contains(rec.Hash, secret) {
		t.Fatal("secret stored in the clear")
	}
	p, err := Verify(ctx, st, secret)
	if err != nil || p.Name != "svc" {
		t.Fatalf("%+v, %v", p, err)
	}
	if err := st.RevokeToken(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, st, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("revoked: %v", err)
	}
	if err := st.RevokeToken(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown revoke: %v", err)
	}
}

func TestExpiry(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	// A negative lifetime must not quietly become a token that never expires.
	if _, _, err := Create(ctx, st, "s", []string{"*"}, -time.Hour); err == nil {
		t.Fatal("negative TTL accepted")
	}
	short, _, _ := Create(ctx, st, "s", []string{"*"}, time.Second)
	if _, err := Verify(ctx, st, short); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if _, err := Verify(ctx, st, short); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	st := open(t)
	for _, s := range []string{"", "roost_", "roost_wrong", "other_token"} {
		if _, err := Verify(context.Background(), st, s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestCreateValidates(t *testing.T) {
	st := open(t)
	if _, _, err := Create(context.Background(), st, " ", []string{"a"}, 0); err == nil {
		t.Error("blank name accepted")
	}
	if _, _, err := Create(context.Background(), st, "n", nil, 0); err == nil {
		t.Error("empty grant accepted")
	}
}

func TestAllowed(t *testing.T) {
	cfg := []string{"a", "b"}
	if got := (&Principal{Collections: []string{"*"}}).Allowed(cfg); !slices.Equal(got, cfg) {
		t.Errorf("all: %v", got)
	}
	if got := (&Principal{Collections: []string{"b", "gone"}}).Allowed(cfg); !slices.Equal(got, []string{"b"}) {
		t.Errorf("subset: %v", got)
	}
}
