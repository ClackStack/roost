// Package store is the SQLite persistence layer: documents, chunks, the FTS5
// index, tokens, and the audit log.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go driver
)

// Document status values.
const (
	StatusOK          = "ok"
	StatusNoText      = "no_text"     // parsed, but no text (typically a scanned PDF)
	StatusUnsupported = "unsupported" // format Roost cannot read
	StatusEncrypted   = "encrypted"   // password protected
	StatusTooLarge    = "too_large"   // over index.max_file_mb
	StatusFailed      = "failed"      // parser error, crash, or timeout
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps separate read and write handles: SQLite allows one writer, and
// routing writes through a single connection avoids lock-upgrade failures.
type Store struct {
	r, w *sql.DB
}

// Open creates the data directory and database if needed, then migrates.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure data dir: %w", err)
	}
	path := filepath.Join(dataDir, "roost.db")
	// Create with 0600 first; SQLite gives the WAL and SHM files the same mode.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	f.Close()

	dsn := func(extra string) string {
		q := url.Values{}
		for _, p := range []string{"busy_timeout(15000)", "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)"} {
			q.Add("_pragma", p)
		}
		return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode() + extra
	}
	w, err := sql.Open("sqlite", dsn("&_txlock=immediate"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn(""))
	if err != nil {
		w.Close()
		return nil, err
	}
	s := &Store{r: r, w: w}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Close releases both handles.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}

func (s *Store) migrate() error {
	var version int
	if err := s.w.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema v%d is newer than this binary (v%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.w.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Document is one file in a collection.
type Document struct {
	ID               string            `json:"id"`
	Collection       string            `json:"collection"`
	RelPath          string            `json:"path"`
	Title            string            `json:"title"`
	FileType         string            `json:"file_type"`
	Size             int64             `json:"size"`
	MTimeNS          int64             `json:"-"`
	SHA256           string            `json:"sha256"`
	Status           string            `json:"status"`
	Error            string            `json:"error,omitempty"`
	Truncated        bool              `json:"truncated,omitempty"`
	Meta             map[string]string `json:"meta,omitempty"`
	ExtractorVersion int               `json:"-"`
	IndexedAt        int64             `json:"-"`

	// SearchTitle is what the FTS title column indexes. It adds the path to
	// the display title, because Office title properties are often junk such
	// as "PowerPoint Presentation", and people search by file name.
	SearchTitle string `json:"-"`
}

// Chunk is a searchable slice of a document.
type Chunk struct {
	Section string
	Text    string
}

const docCols = `id, collection, rel_path, title, file_type, size, mtime_ns, sha256, status, error, truncated, meta, extractor_version, indexed_at`

func scanDoc(row interface{ Scan(...any) error }) (Document, error) {
	var d Document
	var meta string
	err := row.Scan(&d.ID, &d.Collection, &d.RelPath, &d.Title, &d.FileType, &d.Size, &d.MTimeNS,
		&d.SHA256, &d.Status, &d.Error, &d.Truncated, &meta, &d.ExtractorVersion, &d.IndexedAt)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(meta), &d.Meta); err != nil {
		return d, fmt.Errorf("document %s meta: %w", d.ID, err)
	}
	return d, nil
}

// ReplaceDocument atomically swaps a document and all of its chunks, so
// searches see the old version or the new one, never a mix.
func (s *Store) ReplaceDocument(ctx context.Context, d Document, chunks []Chunk) error {
	meta, err := json.Marshal(d.Meta)
	if err != nil {
		return err
	}
	if d.Meta == nil {
		meta = []byte("{}")
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE doc_id = ?`, d.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO documents (`+docCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET title=excluded.title, file_type=excluded.file_type, size=excluded.size,
		mtime_ns=excluded.mtime_ns, sha256=excluded.sha256, status=excluded.status, error=excluded.error,
		truncated=excluded.truncated, meta=excluded.meta, extractor_version=excluded.extractor_version,
		indexed_at=excluded.indexed_at`,
		d.ID, d.Collection, d.RelPath, d.Title, d.FileType, d.Size, d.MTimeNS, d.SHA256, d.Status, d.Error,
		d.Truncated, string(meta), d.ExtractorVersion, time.Now().Unix())
	if err != nil {
		return err
	}
	ins, err := tx.PrepareContext(ctx, `INSERT INTO chunks (doc_id, seq, section, title, body) VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	for i, c := range chunks {
		if _, err := ins.ExecContext(ctx, d.ID, i, c.Section, d.SearchTitle, c.Text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TouchDocument records a new mtime for a file whose content did not change.
func (s *Store) TouchDocument(ctx context.Context, id string, mtimeNS int64) error {
	_, err := s.w.ExecContext(ctx, `UPDATE documents SET mtime_ns = ? WHERE id = ?`, mtimeNS, id)
	return err
}

// DeleteDocuments removes documents and, through triggers, their index entries.
func (s *Store) DeleteDocuments(ctx context.Context, ids []string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE doc_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DocumentsIn returns every document in a collection, keyed by relative path.
func (s *Store) DocumentsIn(ctx context.Context, collection string) (map[string]Document, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+docCols+` FROM documents WHERE collection = ?`, collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Document{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out[d.RelPath] = d
	}
	return out, rows.Err()
}

// OrphanCollections lists indexed collections that are absent from names.
func (s *Store) OrphanCollections(ctx context.Context, names []string) ([]string, error) {
	in, args := inClause(names)
	rows, err := s.r.QueryContext(ctx, `SELECT DISTINCT collection FROM documents WHERE collection NOT IN `+in, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// inClause builds "(?,?,?)" and its arguments. Callers must never interpolate
// collection names or other input into SQL; this is the supported path.
func inClause(vals []string) (string, []any) {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",") + ")", args
}

// SetMeta stores a key/value pair.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetMeta reads a key, returning "" if it is unset.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.r.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// CollectionStatus summarizes one collection for operators.
type CollectionStatus struct {
	Name     string         `json:"name"`
	ByStatus map[string]int `json:"by_status"`
	Chunks   int            `json:"chunks"`
}

// Status returns per-collection document counts.
func (s *Store) Status(ctx context.Context) (map[string]*CollectionStatus, error) {
	out := map[string]*CollectionStatus{}
	get := func(name string) *CollectionStatus {
		if out[name] == nil {
			out[name] = &CollectionStatus{Name: name, ByStatus: map[string]int{}}
		}
		return out[name]
	}
	rows, err := s.r.QueryContext(ctx, `SELECT collection, status, count(*) FROM documents GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c, st string
		var n int
		if err := rows.Scan(&c, &st, &n); err != nil {
			return nil, err
		}
		get(c).ByStatus[st] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	crows, err := s.r.QueryContext(ctx, `SELECT d.collection, count(*) FROM chunks c JOIN documents d ON d.id = c.doc_id GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var c string
		var n int
		if err := crows.Scan(&c, &n); err != nil {
			return nil, err
		}
		get(c).Chunks = n
	}
	return out, crows.Err()
}

// Problems lists documents that could not be fully indexed.
func (s *Store) Problems(ctx context.Context, limit int) ([]Document, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+docCols+` FROM documents
		WHERE status <> 'ok' OR truncated <> 0 ORDER BY collection, rel_path LIMIT ?`, limit)
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

// DeleteCollection removes every document in a collection.
func (s *Store) DeleteCollection(ctx context.Context, name string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE doc_id IN (SELECT id FROM documents WHERE collection = ?)`, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE collection = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}
