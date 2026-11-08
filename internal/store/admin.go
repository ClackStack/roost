package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// TokenRecord is a stored API token. The secret itself is never stored.
type TokenRecord struct {
	ID          string
	Name        string
	Hash        string
	Collections []string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

// InsertToken stores a new token.
func (s *Store) InsertToken(ctx context.Context, t TokenRecord) error {
	cols, err := json.Marshal(t.Collections)
	if err != nil {
		return err
	}
	var exp any
	if t.ExpiresAt != nil {
		exp = t.ExpiresAt.Unix()
	}
	_, err = s.w.ExecContext(ctx, `INSERT INTO tokens (id, name, hash, collections, created_at, expires_at)
		VALUES (?,?,?,?,?,?)`, t.ID, t.Name, t.Hash, string(cols), t.CreatedAt.Unix(), exp)
	return err
}

const tokenCols = `id, name, hash, collections, created_at, expires_at, revoked_at`

func scanToken(row interface{ Scan(...any) error }) (TokenRecord, error) {
	var t TokenRecord
	var cols string
	var created int64
	var exp, rev sql.NullInt64
	if err := row.Scan(&t.ID, &t.Name, &t.Hash, &cols, &created, &exp, &rev); err != nil {
		return t, err
	}
	t.CreatedAt = time.Unix(created, 0)
	if exp.Valid {
		v := time.Unix(exp.Int64, 0)
		t.ExpiresAt = &v
	}
	if rev.Valid {
		v := time.Unix(rev.Int64, 0)
		t.RevokedAt = &v
	}
	return t, json.Unmarshal([]byte(cols), &t.Collections)
}

// TokenByHash finds a token by the hash of its secret.
func (s *Store) TokenByHash(ctx context.Context, hash string) (TokenRecord, error) {
	t, err := scanToken(s.r.QueryRowContext(ctx, `SELECT `+tokenCols+` FROM tokens WHERE hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// ListTokens returns all tokens, oldest first.
func (s *Store) ListTokens(ctx context.Context) ([]TokenRecord, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+tokenCols+` FROM tokens ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenRecord
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken disables a token by ID. Revoking twice keeps the first time.
func (s *Store) RevokeToken(ctx context.Context, id string) error {
	res, err := s.w.ExecContext(ctx, `UPDATE tokens SET revoked_at = coalesce(revoked_at, ?) WHERE id = ?`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AuditEntry is one row of the access log.
type AuditEntry struct {
	Time      time.Time `json:"time"`
	TokenID   string    `json:"token_id"`
	TokenName string    `json:"token_name"`
	Transport string    `json:"transport"`
	Remote    string    `json:"remote,omitempty"`
	Op        string    `json:"op"`
	Query     string    `json:"query,omitempty"`
	DocIDs    []string  `json:"doc_ids,omitempty"`
	Outcome   string    `json:"outcome"`
}

// InsertAudit appends to the access log.
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	ids, err := json.Marshal(e.DocIDs)
	if err != nil {
		return err
	}
	_, err = s.w.ExecContext(ctx, `INSERT INTO audit (ts, token_id, token_name, transport, remote, op, query, doc_ids, outcome)
		VALUES (?,?,?,?,?,?,?,?,?)`, e.Time.UnixMilli(), e.TokenID, e.TokenName, e.Transport, e.Remote, e.Op, e.Query, string(ids), e.Outcome)
	return err
}

// RecentAudit returns the newest entries, newest first.
func (s *Store) RecentAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT ts, token_id, token_name, transport, remote, op, query, doc_ids, outcome
		FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		var ids string
		if err := rows.Scan(&ts, &e.TokenID, &e.TokenName, &e.Transport, &e.Remote, &e.Op, &e.Query, &ids, &e.Outcome); err != nil {
			return nil, err
		}
		e.Time = time.UnixMilli(ts)
		if err := json.Unmarshal([]byte(ids), &e.DocIDs); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PendingChunk is a chunk awaiting an embedding.
type PendingChunk struct {
	ID      int64
	Section string
	Text    string
}

// UnembeddedChunks returns up to limit chunks with no embedding.
func (s *Store) UnembeddedChunks(ctx context.Context, limit int) ([]PendingChunk, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, section, body FROM chunks WHERE embedding IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingChunk
	for rows.Next() {
		var c PendingChunk
		if err := rows.Scan(&c.ID, &c.Section, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetEmbeddings stores vectors by chunk ID, in one transaction.
func (s *Store) SetEmbeddings(ctx context.Context, vecs map[int64][]byte) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, v := range vecs {
		if _, err := tx.ExecContext(ctx, `UPDATE chunks SET embedding = ? WHERE id = ?`, v, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearEmbeddings drops all vectors, as needed when the model changes.
func (s *Store) ClearEmbeddings(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `UPDATE chunks SET embedding = NULL WHERE embedding IS NOT NULL`)
	return err
}

// EachEmbedding streams every stored vector with its chunk's collection.
func (s *Store) EachEmbedding(ctx context.Context, fn func(id int64, collection string, vec []byte) error) error {
	rows, err := s.r.QueryContext(ctx, `SELECT c.id, d.collection, c.embedding FROM chunks c
		JOIN documents d ON d.id = c.doc_id WHERE c.embedding IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var col string
		var v []byte
		if err := rows.Scan(&id, &col, &v); err != nil {
			return err
		}
		if err := fn(id, col, v); err != nil {
			return err
		}
	}
	return rows.Err()
}

// EmbeddingProgress returns the chunks lacking a vector and the total.
func (s *Store) EmbeddingProgress(ctx context.Context) (missing, total int, err error) {
	err = s.r.QueryRowContext(ctx, `SELECT coalesce(sum(embedding IS NULL), 0), count(*) FROM chunks`).Scan(&missing, &total)
	return missing, total, err
}
