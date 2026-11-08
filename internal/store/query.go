package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

// Every method in this file takes the caller's allowed collections as a
// required argument and applies it in SQL. Nothing that returns document
// content or metadata to a client may bypass this.

// Filter narrows a search or listing.
type Filter struct {
	FileTypes     []string
	PathPrefix    string
	ModifiedAfter int64 // unix nanoseconds; 0 means no limit
}

func (f Filter) sql() (string, []any) {
	var b strings.Builder
	var args []any
	if len(f.FileTypes) > 0 {
		in, a := inClause(f.FileTypes)
		b.WriteString(" AND d.file_type IN " + in)
		args = append(args, a...)
	}
	if f.PathPrefix != "" {
		// substr rather than LIKE, so % and _ in paths are not wildcards.
		b.WriteString(" AND substr(d.rel_path, 1, ?) = ?")
		args = append(args, utf8.RuneCountInString(f.PathPrefix), f.PathPrefix)
	}
	if f.ModifiedAfter != 0 {
		b.WriteString(" AND d.mtime_ns > ?")
		args = append(args, f.ModifiedAfter)
	}
	return b.String(), args
}

// Hit is one matching chunk.
type Hit struct {
	ChunkID    int64
	DocID      string
	Seq        int
	Section    string
	Collection string
	RelPath    string
	Title      string
	FileType   string
	MTimeNS    int64
	Snippet    string
	Score      float64
}

const hitCols = `c.id, c.doc_id, c.seq, c.section, d.collection, d.rel_path, d.title, d.file_type, d.mtime_ns`

// SearchChunks runs an FTS5 MATCH expression, best match first. Title terms
// outweigh body terms. CROSS JOIN pins the FTS table as the outer loop, which
// bm25() and snippet() require.
func (s *Store) SearchChunks(ctx context.Context, allowed []string, match string, f Filter, limit int) ([]Hit, error) {
	if len(allowed) == 0 {
		return nil, nil
	}
	in, args := inClause(allowed)
	q := `SELECT ` + hitCols + `, snippet(chunks_fts, 1, '**', '**', '…', 48), -bm25(chunks_fts, 4.0, 1.0)
		FROM chunks_fts
		CROSS JOIN chunks c ON c.id = chunks_fts.rowid
		CROSS JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ? AND d.collection IN ` + in
	fs, fa := f.sql()
	all := append([]any{match}, args...)
	all = append(all, fa...)
	all = append(all, limit)
	rows, err := s.r.QueryContext(ctx, q+fs+` ORDER BY bm25(chunks_fts, 4.0, 1.0) LIMIT ?`, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.Seq, &h.Section, &h.Collection, &h.RelPath,
			&h.Title, &h.FileType, &h.MTimeNS, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HitsByChunk loads chunks found by vector search, applying the ACL and filter
// again. Snippet is the start of the chunk. Order is not preserved.
func (s *Store) HitsByChunk(ctx context.Context, allowed []string, ids []int64, f Filter) ([]Hit, error) {
	if len(allowed) == 0 || len(ids) == 0 {
		return nil, nil
	}
	in, args := inClause(allowed)
	idArgs := make([]any, len(ids))
	for i, id := range ids {
		idArgs[i] = id
	}
	idIn := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	fs, fa := f.sql()
	all := append(append(idArgs, args...), fa...)
	rows, err := s.r.QueryContext(ctx, `SELECT `+hitCols+`, substr(c.body, 1, 300)
		FROM chunks c JOIN documents d ON d.id = c.doc_id
		WHERE c.id IN `+idIn+` AND d.collection IN `+in+fs, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.Seq, &h.Section, &h.Collection, &h.RelPath,
			&h.Title, &h.FileType, &h.MTimeNS, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListDocuments pages through documents in (collection, path) order, starting
// after the given position.
func (s *Store) ListDocuments(ctx context.Context, allowed []string, f Filter, afterCollection, afterPath string, limit int) ([]Document, error) {
	if len(allowed) == 0 {
		return nil, nil
	}
	in, args := inClause(allowed)
	fs, fa := f.sql()
	args = append(args, fa...)
	args = append(args, afterCollection, afterPath, limit)
	rows, err := s.r.QueryContext(ctx, `SELECT `+docCols+` FROM documents d
		WHERE d.collection IN `+in+fs+` AND (d.collection, d.rel_path) > (?, ?)
		ORDER BY d.collection, d.rel_path LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDocument returns ErrNotFound both for an unknown ID and for a document
// outside the allowed collections, so callers cannot probe for existence.
func (s *Store) GetDocument(ctx context.Context, allowed []string, id string) (Document, error) {
	if len(allowed) == 0 {
		return Document{}, ErrNotFound
	}
	in, args := inClause(allowed)
	d, err := scanDoc(s.r.QueryRowContext(ctx, `SELECT `+docCols+` FROM documents WHERE id = ? AND collection IN `+in,
		append([]any{id}, args...)...))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	return d, err
}

// OutlineEntry describes one section of a document.
type OutlineEntry struct {
	Section string `json:"section"`
	Chars   int    `json:"chars"`
}

// Outline lists a document's sections in order.
func (s *Store) Outline(ctx context.Context, allowed []string, id string) ([]OutlineEntry, error) {
	if _, err := s.GetDocument(ctx, allowed, id); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT section, sum(length(body)) FROM chunks
		WHERE doc_id = ? GROUP BY section ORDER BY min(seq)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutlineEntry
	for rows.Next() {
		var e OutlineEntry
		if err := rows.Scan(&e.Section, &e.Chars); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EachChunk streams a document's chunks in order until fn returns false.
func (s *Store) EachChunk(ctx context.Context, allowed []string, id string, fn func(Chunk) bool) error {
	if _, err := s.GetDocument(ctx, allowed, id); err != nil {
		return err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT section, body FROM chunks WHERE doc_id = ? ORDER BY seq`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.Section, &c.Text); err != nil {
			return err
		}
		if !fn(c) {
			return nil
		}
	}
	return rows.Err()
}

// CollectionCounts returns searchable document counts for allowed collections.
func (s *Store) CollectionCounts(ctx context.Context, allowed []string) (map[string]int, error) {
	out := map[string]int{}
	if len(allowed) == 0 {
		return out, nil
	}
	in, args := inClause(allowed)
	rows, err := s.r.QueryContext(ctx, `SELECT collection, count(*) FROM documents
		WHERE status = 'ok' AND collection IN `+in+` GROUP BY collection`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}
