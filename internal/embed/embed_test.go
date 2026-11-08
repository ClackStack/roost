package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paulhovey/roost/internal/config"
	"github.com/paulhovey/roost/internal/store"
)

// fakeServer returns a vector chosen by keyword: 'bird' and 'raptor' texts
// point the same way, which stands in for semantic similarity.
func fakeServer(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		var resp embedResponse
		for i, text := range req.Input {
			v := []float32{0, 1, 0}
			if strings.Contains(text, "bird") || strings.Contains(text, "raptor") {
				v = []float32{3, 0, 0}
			}
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{i, v})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(s.Close)
	return s
}

func setup(t *testing.T, endpoint string) (*Pipeline, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := New(config.Embeddings{Endpoint: endpoint, Model: "m1", BatchSize: 2, Timeout: config.Duration{Duration: 5 * time.Second}}, st, log)
	if err != nil {
		t.Fatal(err)
	}
	return p, st
}

func addDoc(t *testing.T, st *store.Store, id, col string, texts ...string) {
	t.Helper()
	var chunks []store.Chunk
	for _, x := range texts {
		chunks = append(chunks, store.Chunk{Section: "S", Text: x})
	}
	d := store.Document{ID: id, Collection: col, RelPath: id, Title: id, SearchTitle: id, Status: store.StatusOK}
	if err := st.ReplaceDocument(context.Background(), d, chunks); err != nil {
		t.Fatal(err)
	}
}

func TestEmbedAndSearch(t *testing.T) {
	var calls atomic.Int64
	srv := fakeServer(t, &calls)
	p, st := setup(t, srv.URL)
	ctx := context.Background()
	addDoc(t, st, "a", "legal", "a bird of prey", "unrelated text", "more filler")
	addDoc(t, st, "b", "hr", "a raptor file")
	p.Run(ctx)

	if missing, total, _ := st.EmbeddingProgress(ctx); missing != 0 || total != 4 {
		t.Fatalf("progress %d/%d", missing, total)
	}
	if calls.Load() != 2 {
		t.Errorf("4 chunks at batch size 2 took %d calls", calls.Load())
	}
	ids, err := p.Search(ctx, "raptor", []string{"legal", "hr"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	legalOnly, err := p.Search(ctx, "raptor", []string{"legal"}, 1)
	if err != nil || len(legalOnly) != 1 || legalOnly[0] != 1 {
		t.Fatalf("legal: %v, %v", legalOnly, err)
	}
	none, err := p.Search(ctx, "raptor", nil, 5)
	if err != nil || len(none) != 0 {
		t.Fatalf("no grant: %v, %v", none, err)
	}

	// A second pass has nothing to do.
	before := calls.Load()
	p.Run(ctx)
	if calls.Load() != before {
		t.Error("second pass re-embedded")
	}

	// Changing the model re-embeds everything.
	p.cfg.Model = "m2"
	p.Run(ctx)
	if calls.Load() != before+2 {
		t.Errorf("model change: %d calls", calls.Load()-before)
	}
}

func TestEndpointDownLeavesChunksPending(t *testing.T) {
	p, st := setup(t, "http://127.0.0.1:1/v1/embeddings")
	ctx := context.Background()
	addDoc(t, st, "a", "c", "text")
	p.Run(ctx)
	if missing, _, _ := st.EmbeddingProgress(ctx); missing != 1 {
		t.Fatalf("missing = %d", missing)
	}
	if _, err := p.Search(ctx, "q", []string{"c"}, 1); err == nil {
		t.Fatal("search should report the outage so core can fall back")
	}
}

func TestPublicAddressRefused(t *testing.T) {
	// 192.0.2.0/24 is TEST-NET: public, and it never routes anywhere real.
	p, _ := setup(t, "http://192.0.2.1:80/v1/embeddings")
	_, err := p.call(context.Background(), []string{"secret text"})
	if err == nil || !strings.Contains(err.Error(), "non-private") {
		t.Fatalf("err = %v", err)
	}
}

func TestInternalIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "172.16.0.1": true, "192.168.1.1": true, "::1": true, "fd00::1": true,
		"8.8.8.8": false, "169.254.169.254": false, "0.0.0.0": false, "2001:4860:4860::8888": false, "100.64.0.1": false,
	}
	for s, want := range cases {
		if got := internalIP(net.ParseIP(s)); got != want {
			t.Errorf("%s: got %v", s, got)
		}
	}
}

// TestProxyIgnored inspects the transport, because an end-to-end test cannot
// show this: net/http never proxies loopback, and test servers are loopback.
func TestProxyIgnored(t *testing.T) {
	p, _ := setup(t, "http://10.0.0.1/v1/embeddings")
	if p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("client honors proxy settings, which could carry document text to another host")
	}
}

func TestRedirectRefused(t *testing.T) {
	var hit atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	p, _ := setup(t, srv.URL)
	if _, err := p.call(context.Background(), []string{"x"}); err == nil {
		t.Fatal("redirect was followed")
	}
	if hit.Load() != 0 {
		t.Fatal("document text reached the redirect target")
	}
}

func TestMalformedResponses(t *testing.T) {
	for name, body := range map[string]string{
		"not json":  "nope",
		"too few":   `{"data":[]}`,
		"bad index": `{"data":[{"index":9,"embedding":[1]}]}`,
		"empty vec": `{"data":[{"index":0,"embedding":[]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
			defer srv.Close()
			p, _ := setup(t, srv.URL)
			if _, err := p.call(context.Background(), []string{"x"}); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func BenchmarkVectorSearch(b *testing.B) {
	const n, dim = 200_000, 768
	x := &index{dim: dim, names: []string{"c"}, ids: make([]int64, n), collections: make([]int32, n), vecs: make([]float32, n*dim)}
	for i := range x.ids {
		x.ids[i] = int64(i)
		x.vecs[i*dim+i%dim] = 1
	}
	q := make([]float32, dim)
	q[5] = 1
	b.ResetTimer()
	for b.Loop() {
		x.search(q, []string{"c"}, 50)
	}
}
