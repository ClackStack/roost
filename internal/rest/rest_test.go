package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/core"
	"github.com/ClackStack/roost/internal/store"
)

type env struct {
	t        *testing.T
	st       *store.Store
	srv      *httptest.Server
	legalTok string
	hrTok    string
	allTok   string
	legalRec store.TokenRecord
	legalDoc string
	hrDoc    string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	for _, n := range []string{"legal", "hr"} {
		p := filepath.Join(dir, n)
		os.Mkdir(p, 0o700)
		cfg.Collections = append(cfg.Collections, config.Collection{Name: n, Path: p})
	}
	st, err := store.Open(cfg.Storage.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := core.New(&cfg, st, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, legalDoc: "doc-legal", hrDoc: "doc-hr"}
	ctx := context.Background()
	for _, d := range []struct{ id, col, text string }{{e.legalDoc, "legal", "falcon merger terms"}, {e.hrDoc, "hr", "falcon salary bands"}} {
		os.WriteFile(filepath.Join(dir, d.col, "f.txt"), []byte(d.text), 0o600)
		doc := store.Document{ID: d.id, Collection: d.col, RelPath: "f.txt", Title: d.col, SearchTitle: d.col, FileType: "txt", Status: store.StatusOK}
		if err := st.ReplaceDocument(ctx, doc, []store.Chunk{{Section: "Body", Text: d.text}}); err != nil {
			t.Fatal(err)
		}
	}
	e.legalTok, e.legalRec, _ = auth.Create(ctx, st, "legal-user", []string{"legal"}, 0)
	e.hrTok, _, _ = auth.Create(ctx, st, "hr-user", []string{"hr"}, 0)
	e.allTok, _, _ = auth.Create(ctx, st, "admin", []string{"*"}, 0)
	e.srv = httptest.NewServer(Handler(&cfg, svc, st, "test", log))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) do(method, path, token, body string) (*http.Response, string) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// mcpClient connects the real SDK client over HTTP.
func (e *env) mcpClient(token string) (*mcp.ClientSession, error) {
	e.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearer{token}}
	s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp", HTTPClient: httpClient}, nil)
	if err == nil {
		e.t.Cleanup(func() { s.Close() })
	}
	return s, err
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func callTool(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out.WriteString(tc.Text)
		}
	}
	return out.String(), res.IsError
}

func TestUnauthenticatedIsRejectedEverywhere(t *testing.T) {
	e := setup(t)
	routes := []struct{ method, path string }{
		{"POST", "/v1/search"}, {"GET", "/v1/collections"}, {"GET", "/v1/documents"},
		{"GET", "/v1/documents/doc-legal"}, {"GET", "/v1/documents/doc-legal/text"},
		{"GET", "/v1/documents/doc-legal/file"}, {"POST", "/mcp"},
	}
	for _, r := range routes {
		for _, tok := range []string{"", "roost_bad", "not-even-close"} {
			resp, body := e.do(r.method, r.path, tok, `{"query":"falcon"}`)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s token=%q: %d", r.method, r.path, tok, resp.StatusCode)
			}
			if strings.Contains(body, "falcon") {
				t.Errorf("%s %s leaked content", r.method, r.path)
			}
		}
	}
	if _, err := e.mcpClient(""); err == nil {
		t.Error("MCP client connected with no token")
	}
	if _, err := e.mcpClient("roost_bad"); err == nil {
		t.Error("MCP client connected with a bad token")
	}
}

func TestPublicEndpoints(t *testing.T) {
	e := setup(t)
	if resp, body := e.do("GET", "/healthz", "", ""); resp.StatusCode != 200 || body != "ok\n" {
		t.Errorf("healthz: %d %q", resp.StatusCode, body)
	}
	resp, body := e.do("GET", "/openapi.json", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("openapi: %d", resp.StatusCode)
	}
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/search", "/v1/collections", "/v1/documents", "/v1/documents/{id}", "/v1/documents/{id}/text", "/v1/documents/{id}/file"} {
		if spec.Paths[p] == nil {
			t.Errorf("spec lacks %s", p)
		}
	}
}

// TestACLMatrix runs identical allow and deny cases over both interfaces.
func TestACLMatrix(t *testing.T) {
	e := setup(t)
	type iface struct {
		name string
		// each returns the visible response text and whether it was refused
		search func(tok string) string
		info   func(tok, id string) (string, bool)
		read   func(tok, id string) (string, bool)
		list   func(tok string) string
		cols   func(tok string) string
	}
	restI := iface{
		name: "rest",
		search: func(tok string) string {
			_, b := e.do("POST", "/v1/search", tok, `{"query":"falcon"}`)
			return b
		},
		info: func(tok, id string) (string, bool) {
			r, b := e.do("GET", "/v1/documents/"+id, tok, "")
			return b, r.StatusCode == 404
		},
		read: func(tok, id string) (string, bool) {
			r, b := e.do("GET", "/v1/documents/"+id+"/text", tok, "")
			return b, r.StatusCode == 404
		},
		list: func(tok string) string {
			_, b := e.do("GET", "/v1/documents", tok, "")
			return b
		},
		cols: func(tok string) string {
			_, b := e.do("GET", "/v1/collections", tok, "")
			return b
		},
	}
	sessions := map[string]*mcp.ClientSession{}
	sess := func(tok string) *mcp.ClientSession {
		if sessions[tok] == nil {
			s, err := e.mcpClient(tok)
			if err != nil {
				t.Fatal(err)
			}
			sessions[tok] = s
		}
		return sessions[tok]
	}
	mcpI := iface{
		name: "mcp",
		search: func(tok string) string {
			s, _ := callTool(t, sess(tok), "search_documents", map[string]any{"query": "falcon"})
			return s
		},
		info: func(tok, id string) (string, bool) {
			return callTool(t, sess(tok), "get_document_info", map[string]any{"doc_id": id})
		},
		read: func(tok, id string) (string, bool) {
			return callTool(t, sess(tok), "read_document", map[string]any{"doc_id": id})
		},
		list: func(tok string) string {
			s, _ := callTool(t, sess(tok), "list_documents", map[string]any{})
			return s
		},
		cols: func(tok string) string {
			s, _ := callTool(t, sess(tok), "list_collections", map[string]any{})
			return s
		},
	}
	for _, i := range []iface{restI, mcpI} {
		t.Run(i.name, func(t *testing.T) {
			if got := i.search(e.legalTok); !strings.Contains(got, "merger") || strings.Contains(got, "salary") {
				t.Errorf("search: %s", got)
			}
			if got := i.search(e.allTok); !strings.Contains(got, "merger") || !strings.Contains(got, "salary") {
				t.Errorf("admin search: %s", got)
			}
			if got := i.list(e.legalTok); !strings.Contains(got, e.legalDoc) || strings.Contains(got, e.hrDoc) {
				t.Errorf("list: %s", got)
			}
			if got := i.cols(e.hrTok); !strings.Contains(got, `"hr"`) || strings.Contains(got, `"legal"`) {
				t.Errorf("collections: %s", got)
			}
			if got, refused := i.read(e.legalTok, e.legalDoc); refused || !strings.Contains(got, "merger") {
				t.Errorf("own read: %s", got)
			}
			if got, refused := i.info(e.legalTok, e.legalDoc); refused || !strings.Contains(got, "outline") {
				t.Errorf("own info: %s", got)
			}
			// Forbidden and nonexistent documents must look identical.
			hrBody, hrRefused := i.read(e.legalTok, e.hrDoc)
			noneBody, noneRefused := i.read(e.legalTok, "no-such-document")
			if !hrRefused || !noneRefused || hrBody != noneBody {
				t.Errorf("read oracle: %q vs %q", hrBody, noneBody)
			}
			if strings.Contains(hrBody, "salary") {
				t.Errorf("leak: %s", hrBody)
			}
			hrBody, hrRefused = i.info(e.legalTok, e.hrDoc)
			noneBody, noneRefused = i.info(e.legalTok, "no-such-document")
			if !hrRefused || !noneRefused || hrBody != noneBody {
				t.Errorf("info oracle: %q vs %q", hrBody, noneBody)
			}
		})
	}
}

func TestFileDownload(t *testing.T) {
	e := setup(t)
	resp, body := e.do("GET", "/v1/documents/"+e.legalDoc+"/file", e.legalTok, "")
	if resp.StatusCode != 200 || body != "falcon merger terms" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("type = %q", ct)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	if resp, _ := e.do("GET", "/v1/documents/"+e.hrDoc+"/file", e.legalTok, ""); resp.StatusCode != 404 {
		t.Errorf("forbidden download: %d", resp.StatusCode)
	}
}

func TestRevocationAppliesToLiveSession(t *testing.T) {
	e := setup(t)
	s, err := e.mcpClient(e.legalTok)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := callTool(t, s, "search_documents", map[string]any{"query": "falcon"}); !strings.Contains(got, "merger") {
		t.Fatalf("before revoke: %s", got)
	}
	if err := e.st.RevokeToken(context.Background(), e.legalRec.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_documents", Arguments: map[string]any{"query": "falcon"}})
	if err == nil && !res.IsError {
		t.Fatal("revoked token still works on an open MCP session")
	}
	if resp, _ := e.do("GET", "/v1/collections", e.legalTok, ""); resp.StatusCode != 401 {
		t.Fatalf("revoked REST: %d", resp.StatusCode)
	}
}

func TestToolsAreReadOnly(t *testing.T) {
	e := setup(t)
	s, err := e.mcpClient(e.allTok)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"list_collections": true, "search_documents": true, "list_documents": true, "get_document_info": true, "read_document": true}
	if len(res.Tools) != len(want) {
		t.Fatalf("got %d tools", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %s", tool.Name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
}

func TestBadRequests(t *testing.T) {
	e := setup(t)
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/v1/search", `not json`, 400},
		{"POST", "/v1/search", `{"query":""}`, 400},
		{"POST", "/v1/search", `{"query":"x","nope":1}`, 400},
		{"POST", "/v1/search", `{"query":"` + strings.Repeat("a", 2<<20) + `"}`, 400},
		{"GET", "/v1/documents?limit=abc", "", 400},
		{"GET", "/v1/documents?cursor=!!", "", 400},
		{"GET", "/v1/documents/doc-legal/text?offset=-1", "", 400},
		{"GET", "/v1/documents/doc-legal/text?section=Nope", "", 400},
		{"GET", "/v1/search", "", 405},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s %.40s", tc.method, tc.path), func(t *testing.T) {
			resp, body := e.do(tc.method, tc.path, e.allTok, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("%d, want %d: %s", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func TestAuditSeesBothTransports(t *testing.T) {
	e := setup(t)
	e.do("POST", "/v1/search", e.legalTok, `{"query":"falcon"}`)
	s, err := e.mcpClient(e.legalTok)
	if err != nil {
		t.Fatal(err)
	}
	callTool(t, s, "search_documents", map[string]any{"query": "heron"})
	time.Sleep(10 * time.Millisecond)
	entries, err := e.st.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]store.AuditEntry{}
	for _, a := range entries {
		seen[a.Transport] = a
	}
	for _, tr := range []string{"rest", "mcp-http"} {
		a, ok := seen[tr]
		if !ok || a.TokenName != "legal-user" || a.Remote == "" {
			t.Errorf("%s: %+v", tr, a)
		}
	}
	for _, a := range entries {
		if strings.Contains(a.Query, "roost_") {
			t.Error("token in audit log")
		}
	}
}
