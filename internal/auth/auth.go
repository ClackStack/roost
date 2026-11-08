// Package auth issues and verifies API tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ClackStack/roost/internal/store"
)

// Prefix marks Roost tokens, so secret scanners can recognize a leaked one.
const Prefix = "roost_"

// AllCollections, as a grant, covers every collection present or future.
const AllCollections = "*"

// ErrInvalid covers unknown, expired, and revoked tokens alike, so a caller
// learns nothing about which case applied.
var ErrInvalid = errors.New("invalid token")

// Principal is an authenticated caller.
type Principal struct {
	TokenID     string
	Name        string
	Collections []string
}

// Allowed intersects the caller's grant with the configured collections. A
// collection removed from the config stops being readable right away, even
// while its documents remain in the database.
func (p *Principal) Allowed(configured []string) []string {
	if slices.Contains(p.Collections, AllCollections) {
		return slices.Clone(configured)
	}
	var out []string
	for _, c := range configured {
		if slices.Contains(p.Collections, c) {
			out = append(out, c)
		}
	}
	return out
}

// Hash returns the stored form of a secret. A fast hash is sound here, unlike
// for passwords, because secrets are 256-bit random values with nothing to guess.
func Hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Create issues a token and returns its secret, the only time it is available.
func Create(ctx context.Context, st *store.Store, name string, collections []string, ttl time.Duration) (string, store.TokenRecord, error) {
	if strings.TrimSpace(name) == "" {
		return "", store.TokenRecord{}, errors.New("token name is required")
	}
	if len(collections) == 0 {
		return "", store.TokenRecord{}, errors.New("a token needs at least one collection")
	}
	if ttl < 0 {
		return "", store.TokenRecord{}, errors.New("token lifetime must be positive")
	}
	raw := make([]byte, 32)
	rand.Read(raw) // crypto/rand.Read never fails
	secret := Prefix + base64.RawURLEncoding.EncodeToString(raw)
	idb := make([]byte, 6)
	rand.Read(idb)
	rec := store.TokenRecord{
		ID: hex.EncodeToString(idb), Name: name, Hash: Hash(secret),
		Collections: collections, CreatedAt: time.Now(),
	}
	if ttl > 0 {
		exp := rec.CreatedAt.Add(ttl)
		rec.ExpiresAt = &exp
	}
	if err := st.InsertToken(ctx, rec); err != nil {
		return "", store.TokenRecord{}, fmt.Errorf("store token: %w", err)
	}
	return secret, rec, nil
}

// Verify checks a secret against the database on every call, so revocation
// applies to the very next request.
func Verify(ctx context.Context, st *store.Store, secret string) (*Principal, error) {
	if !strings.HasPrefix(secret, Prefix) {
		return nil, ErrInvalid
	}
	t, err := st.TokenByHash(ctx, Hash(secret))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	if t.RevokedAt != nil || (t.ExpiresAt != nil && !time.Now().Before(*t.ExpiresAt)) {
		return nil, ErrInvalid
	}
	return &Principal{TokenID: t.ID, Name: t.Name, Collections: t.Collections}, nil
}
