package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "roost.db"): 0o600} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	for range 2 {
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}

func TestReplaceAndDeleteKeepFTSInSync(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	d := Document{ID: "d1", Collection: "c", RelPath: "a.txt", Title: "A", SearchTitle: "A a.txt", FileType: "txt", Status: StatusOK}
	count := func(term string) int {
		t.Helper()
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM chunks_fts WHERE chunks_fts MATCH ?`, term).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.ReplaceDocument(ctx, d, []Chunk{{"Body", "alpha falcon"}}); err != nil {
		t.Fatal(err)
	}
	if count("falcon") != 1 {
		t.Fatal("falcon not indexed")
	}
	if err := s.ReplaceDocument(ctx, d, []Chunk{{"Body", "beta heron"}}); err != nil {
		t.Fatal(err)
	}
	if count("falcon") != 0 || count("heron") != 1 {
		t.Fatal("replace left stale index entries")
	}
	if err := s.DeleteDocuments(ctx, []string{"d1"}); err != nil {
		t.Fatal(err)
	}
	if count("heron") != 0 {
		t.Fatal("delete left stale index entries")
	}
}
