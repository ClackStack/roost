package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulhovey/roost/internal/auth"
	"github.com/paulhovey/roost/internal/config"
	"github.com/paulhovey/roost/internal/store"
)

type env struct {
	t   *testing.T
	cfg *config.Config
	st  *store.Store
	svc *Service
	all Caller
	dir string
}

func setup(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	for _, n := range []string{"legal", "hr"} {
		p := filepath.Join(dir, n)
		os.Mkdir(p, 0o700)
		cfg.Collections = append(cfg.Collections, config.Collection{Name: n, Path: p})
	}
	for _, m := range mutate {
		m(&cfg)
	}
	st, err := store.Open(cfg.Storage.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := New(&cfg, st, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return &env{t: t, cfg: &cfg, st: st, svc: svc, dir: dir, all: caller("*")}
}

func caller(cols ...string) Caller {
	return Caller{Principal: &auth.Principal{TokenID: "t", Name: "test", Collections: cols}, Transport: "test"}
}

func (e *env) add(col, rel, title string, chunks ...store.Chunk) string {
	e.t.Helper()
	id := col + "/" + rel
	d := store.Document{ID: id, Collection: col, RelPath: rel, Title: title, SearchTitle: title, FileType: strings.TrimPrefix(filepath.Ext(rel), "."),
		Status: store.StatusOK, MTimeNS: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()}
	if err := e.st.ReplaceDocument(context.Background(), d, chunks); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func paths(r *SearchResponse) []string {
	var out []string
	for _, h := range r.Results {
		out = append(out, h.Path)
	}
	return out
}

func TestSearchRanksAndStems(t *testing.T) {
	e := setup(t)
	e.add("legal", "a.pdf", "Unrelated", store.Chunk{Section: "Page 1", Text: "The acquisitions team met on Tuesday."})
	e.add("legal", "b.pdf", "Acquisition plan", store.Chunk{Section: "Page 2", Text: "Budget notes."})
	r, err := e.svc.Search(context.Background(), e.all, SearchRequest{Query: "acquisition"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 2 {
		t.Fatalf("results: %+v", r.Results)
	}
	if r.Results[0].Path != "b.pdf" {
		t.Errorf("title match should rank first: %v", paths(r))
	}
	if r.Results[1].Section != "Page 1" || !strings.Contains(r.Results[1].Snippet, "**acquisitions**") {
		t.Errorf("hit = %+v", r.Results[1])
	}
	if r.Notice != ContentNotice {
		t.Error("search results must carry the untrusted content notice")
	}
}

func TestSearchFallsBackToAnyTerm(t *testing.T) {
	e := setup(t)
	e.add("legal", "a.pdf", "A", store.Chunk{Section: "S", Text: "the merger closes soon"})
	r, err := e.svc.Search(context.Background(), e.all, SearchRequest{Query: "what is the status of the merger"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "any_term" || len(r.Results) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestSearchFilters(t *testing.T) {
	e := setup(t)
	e.add("legal", "2025/a.pdf", "A", store.Chunk{Section: "S", Text: "falcon"})
	e.add("legal", "2026/b.docx", "B", store.Chunk{Section: "S", Text: "falcon"})
	e.add("legal", "100%_done/c.pdf", "C", store.Chunk{Section: "S", Text: "falcon"})
	ctx := context.Background()
	cases := []struct {
		name string
		req  SearchRequest
		want int
	}{
		{"type", SearchRequest{Query: "falcon", FileTypes: []string{".PDF"}}, 2},
		{"prefix", SearchRequest{Query: "falcon", PathPrefix: "2026/"}, 1},
		{"prefix wildcards are literal", SearchRequest{Query: "falcon", PathPrefix: "100%"}, 1},
		{"prefix underscore is literal", SearchRequest{Query: "falcon", PathPrefix: "2_"}, 0},
		{"after", SearchRequest{Query: "falcon", ModifiedAfter: "2026-06-01T00:00:00Z"}, 0},
		{"before cutoff", SearchRequest{Query: "falcon", ModifiedAfter: "2025-06-01T00:00:00Z"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := e.svc.Search(ctx, e.all, tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Results) != tc.want {
				t.Fatalf("got %v, want %d", paths(r), tc.want)
			}
		})
	}
}

func TestSearchPagingAndPerDocCap(t *testing.T) {
	e := setup(t)
	var chunks []store.Chunk
	for i := range 10 {
		chunks = append(chunks, store.Chunk{Section: fmt.Sprint("Page ", i), Text: "falcon falcon falcon"})
	}
	e.add("legal", "big.pdf", "Big", chunks...)
	for i := range 6 {
		e.add("legal", fmt.Sprintf("d%d.pdf", i), "D", store.Chunk{Section: "S", Text: "falcon"})
	}
	ctx := context.Background()
	r, err := e.svc.Search(ctx, e.all, SearchRequest{Query: "falcon", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	big := 0
	for _, h := range r.Results {
		if h.Path == "big.pdf" {
			big++
		}
	}
	if big != maxHitsPerDoc || len(r.Results) != maxHitsPerDoc+6 {
		t.Fatalf("big=%d total=%d", big, len(r.Results))
	}
	seen := map[string]bool{}
	offset := 0
	for {
		p, err := e.svc.Search(ctx, e.all, SearchRequest{Query: "falcon", Limit: 2, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range p.Results {
			key := h.Path + h.Section
			if seen[key] {
				t.Fatalf("duplicate across pages: %s", key)
			}
			seen[key] = true
		}
		offset += 2
		if !p.HasMore {
			break
		}
	}
	if len(seen) != maxHitsPerDoc+6 {
		t.Fatalf("paged through %d, want %d", len(seen), maxHitsPerDoc+6)
	}
}

func TestSearchRejectsBadInput(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for _, r := range []SearchRequest{{Query: ""}, {Query: "!!!"}, {Query: strings.Repeat("a", maxQueryLen+1)}, {Query: "x", ModifiedAfter: "yesterday"}} {
		if _, err := e.svc.Search(ctx, e.all, r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%.20q: err = %v", r.Query, err)
		}
	}
}

func TestReadWindowsRebuildDocument(t *testing.T) {
	e := setup(t)
	id := e.add("legal", "a.pdf", "A",
		store.Chunk{Section: "Page 1", Text: strings.Repeat("機密のcafé ", 300)},
		store.Chunk{Section: "Page 2", Text: strings.Repeat("🦅 eagle ", 300)})
	ctx := context.Background()
	whole, err := e.svc.ReadDocument(ctx, e.all, ReadRequest{DocID: id, MaxChars: maxReadChars})
	if err != nil {
		t.Fatal(err)
	}
	if whole.NextOffset != 0 {
		t.Fatal("whole read reported more")
	}
	for _, size := range []int{7, 64, 1001} {
		var got strings.Builder
		offset := 0
		for {
			r, err := e.svc.ReadDocument(ctx, e.all, ReadRequest{DocID: id, Offset: offset, MaxChars: size})
			if err != nil {
				t.Fatal(err)
			}
			got.WriteString(r.Text)
			if r.NextOffset == 0 {
				break
			}
			if r.NextOffset <= offset {
				t.Fatalf("size %d: no progress at %d", size, offset)
			}
			offset = r.NextOffset
		}
		if got.String() != whole.Text {
			t.Fatalf("size %d: windows do not rebuild the document", size)
		}
	}
}

func TestReadSection(t *testing.T) {
	e := setup(t)
	id := e.add("legal", "s.pptx", "S",
		store.Chunk{Section: "Slide 3: Budget", Text: "three"},
		store.Chunk{Section: "Slide 30: Other", Text: "thirty"})
	ctx := context.Background()
	r, err := e.svc.ReadDocument(ctx, e.all, ReadRequest{DocID: id, Section: "slide 3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "three") || strings.Contains(r.Text, "thirty") {
		t.Fatalf("text = %q", r.Text)
	}
	if _, err := e.svc.ReadDocument(ctx, e.all, ReadRequest{DocID: id, Section: "Slide 9"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	info, err := e.svc.GetDocumentInfo(ctx, e.all, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Outline) != 2 || info.Outline[1].Section != "Slide 30: Other" {
		t.Fatalf("outline = %+v", info.Outline)
	}
}

func TestListPaging(t *testing.T) {
	e := setup(t)
	for i := range 5 {
		e.add("legal", fmt.Sprintf("d%d.pdf", i), "D", store.Chunk{Section: "S", Text: "x"})
	}
	e.add("hr", "h.pdf", "H", store.Chunk{Section: "S", Text: "x"})
	ctx := context.Background()
	var got []string
	cursor := ""
	for {
		r, err := e.svc.ListDocuments(ctx, e.all, ListRequest{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range r.Documents {
			got = append(got, d.Collection+"/"+d.Path)
		}
		if cursor = r.NextCursor; cursor == "" {
			break
		}
	}
	if len(got) != 6 || got[0] != "hr/h.pdf" {
		t.Fatalf("got %v", got)
	}
	if _, err := e.svc.ListDocuments(ctx, e.all, ListRequest{Cursor: "!!"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestACL(t *testing.T) {
	e := setup(t)
	legal := e.add("legal", "l.pdf", "L", store.Chunk{Section: "S", Text: "falcon"})
	hr := e.add("hr", "h.pdf", "H", store.Chunk{Section: "S", Text: "falcon"})
	ctx := context.Background()
	c := caller("legal")

	r, err := e.svc.Search(ctx, c, SearchRequest{Query: "falcon"})
	if err != nil || len(r.Results) != 1 || r.Results[0].DocID != legal {
		t.Fatalf("search: %+v, %v", r, err)
	}
	// Asking for a forbidden collection by name must not widen the grant.
	r, err = e.svc.Search(ctx, c, SearchRequest{Query: "falcon", Collections: []string{"hr"}})
	if err != nil || len(r.Results) != 0 {
		t.Fatalf("named search: %+v, %v", r, err)
	}
	l, err := e.svc.ListDocuments(ctx, c, ListRequest{Collections: []string{"hr", "legal"}})
	if err != nil || len(l.Documents) != 1 {
		t.Fatalf("list: %+v, %v", l, err)
	}
	cols, err := e.svc.ListCollections(ctx, c)
	if err != nil || len(cols) != 1 || cols[0].Name != "legal" {
		t.Fatalf("collections: %+v, %v", cols, err)
	}
	// The forbidden document is indistinguishable from a missing one.
	for _, id := range []string{hr, "no-such-doc"} {
		if _, err := e.svc.GetDocumentInfo(ctx, c, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("info %s: %v", id, err)
		}
		if _, err := e.svc.ReadDocument(ctx, c, ReadRequest{DocID: id}); !errors.Is(err, ErrNotFound) {
			t.Errorf("read %s: %v", id, err)
		}
		if _, err := e.svc.OpenOriginal(ctx, c, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("open %s: %v", id, err)
		}
	}
	// No principal and an empty grant both see nothing.
	for _, none := range []Caller{{}, caller("nonexistent")} {
		r, err := e.svc.Search(ctx, none, SearchRequest{Query: "falcon"})
		if err != nil || len(r.Results) != 0 {
			t.Errorf("empty caller: %+v, %v", r, err)
		}
	}
}

func TestRemovedCollectionIsUnreadable(t *testing.T) {
	e := setup(t)
	e.add("hr", "h.pdf", "H", store.Chunk{Section: "S", Text: "falcon"})
	e.cfg.Collections = e.cfg.Collections[:1] // drop hr from the config
	r, err := e.svc.Search(context.Background(), e.all, SearchRequest{Query: "falcon"})
	if err != nil || len(r.Results) != 0 {
		t.Fatalf("%+v, %v", r, err)
	}
}

func TestAuditRecords(t *testing.T) {
	e := setup(t)
	id := e.add("legal", "l.pdf", "L", store.Chunk{Section: "S", Text: "falcon"})
	ctx := context.Background()
	c := caller("legal")
	e.svc.Search(ctx, c, SearchRequest{Query: "falcon"})
	e.svc.ReadDocument(ctx, c, ReadRequest{DocID: id})
	e.svc.ReadDocument(ctx, c, ReadRequest{DocID: "hr/secret"})
	entries, err := e.st.RecentAudit(ctx, 10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("%+v, %v", entries, err)
	}
	probe, read, search := entries[0], entries[1], entries[2]
	if search.Op != "search" || search.Query != "falcon" || len(search.DocIDs) != 1 {
		t.Errorf("search entry: %+v", search)
	}
	if read.Op != "read_document" || read.DocIDs[0] != id || read.TokenName != "test" {
		t.Errorf("read entry: %+v", read)
	}
	if probe.Outcome != "not_found" {
		t.Errorf("failed probe entry: %+v", probe)
	}
}

func TestAuditQueryLoggingOff(t *testing.T) {
	jsonl := filepath.Join(t.TempDir(), "audit.jsonl")
	e := setup(t, func(c *config.Config) { c.Audit.LogQueries = false; c.Audit.JSONLPath = jsonl })
	e.add("legal", "l.pdf", "L", store.Chunk{Section: "S", Text: "falcon"})
	ctx := context.Background()
	if _, err := e.svc.Search(ctx, e.all, SearchRequest{Query: "falcon"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := e.st.RecentAudit(ctx, 1)
	if entries[0].Query != "" {
		t.Errorf("query was logged: %q", entries[0].Query)
	}
	b, err := os.ReadFile(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "falcon") || !strings.Contains(string(b), `"op":"search"`) {
		t.Errorf("jsonl = %s", b)
	}
	fi, _ := os.Stat(jsonl)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", fi.Mode().Perm())
	}
}

// TestAuditFailureWithholdsResults checks the fail-closed behaviour.
func TestAuditFailureWithholdsResults(t *testing.T) {
	e := setup(t)
	id := e.add("legal", "l.pdf", "L", store.Chunk{Section: "S", Text: "falcon"})
	os.WriteFile(filepath.Join(e.cfg.Collections[0].Path, "l.pdf"), []byte("x"), 0o600)
	e.svc.writeAudit = func(context.Context, store.AuditEntry) error { return errors.New("disk full") }
	ctx := context.Background()
	if _, err := e.svc.Search(ctx, e.all, SearchRequest{Query: "falcon"}); err == nil {
		t.Error("search returned without an audit record")
	}
	if _, err := e.svc.ReadDocument(ctx, e.all, ReadRequest{DocID: id}); err == nil {
		t.Error("read returned without an audit record")
	}
	if _, err := e.svc.OpenOriginal(ctx, e.all, id); err == nil {
		t.Error("download opened without an audit record")
	}
}

func TestOriginalCannotEscapeRoot(t *testing.T) {
	e := setup(t)
	id := e.add("legal", "link.pdf", "L", store.Chunk{Section: "S", Text: "x"})
	outside := filepath.Join(e.dir, "outside.txt")
	os.WriteFile(outside, []byte("secret"), 0o600)
	// A file indexed earlier is later swapped for a link out of the collection.
	os.Symlink(outside, filepath.Join(e.cfg.Collections[0].Path, "link.pdf"))
	if _, err := e.svc.OpenOriginal(context.Background(), e.all, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

type fakeVectors struct {
	ids []int64
	err error
}

func (f *fakeVectors) Search(context.Context, string, []string, int) ([]int64, error) {
	return f.ids, f.err
}

func TestHybridSearch(t *testing.T) {
	e := setup(t)
	e.add("legal", "keyword.pdf", "K", store.Chunk{Section: "S", Text: "falcon"})
	e.add("legal", "semantic.pdf", "M", store.Chunk{Section: "S", Text: "a bird of prey"})
	e.add("hr", "forbidden.pdf", "F", store.Chunk{Section: "S", Text: "a raptor"})
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Chunk IDs follow insertion order. The fake returns a forbidden chunk
	// too: the store must drop it even if vector search misbehaves.
	svc, err := New(e.cfg, e.st, log, &fakeVectors{ids: []int64{3, 2}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Search(ctx, caller("legal"), SearchRequest{Query: "falcon"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "all_terms+semantic" {
		t.Errorf("mode = %q", r.Mode)
	}
	got := paths(r)
	if len(got) != 2 {
		t.Fatalf("results = %v", got)
	}
	for _, p := range got {
		if p == "forbidden.pdf" {
			t.Fatal("ACL bypassed through vector results")
		}
	}

	// Embeddings down: keyword results still return, with a warning.
	svc, _ = New(e.cfg, e.st, log, &fakeVectors{err: errors.New("connection refused")})
	r, err = svc.Search(ctx, caller("legal"), SearchRequest{Query: "falcon"})
	if err != nil || len(r.Results) != 1 || r.Warning == "" || strings.Contains(r.Mode, "semantic") {
		t.Fatalf("%+v, %v", r, err)
	}
}
