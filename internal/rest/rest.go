// Package rest serves the HTTP API, and hosts the MCP HTTP endpoint so both
// share one listener and one authentication path.
package rest

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/core"
	"github.com/ClackStack/roost/internal/mcpserver"
	"github.com/ClackStack/roost/internal/store"
)

//go:embed openapi.json
var openAPI []byte

const maxBody = 1 << 20

const principalKey = "principal"
const remoteKey = "remote"

// Handler builds the complete HTTP surface.
func Handler(cfg *config.Config, svc *core.Service, st *store.Store, version string, log *slog.Logger) http.Handler {
	// RequireBearerToken guards REST as well as MCP, so there is one auth path.
	// Roost tokens may have no expiry; Verify enforces expiry when they do.
	requireToken := sdkauth.RequireBearerToken(func(ctx context.Context, token string, r *http.Request) (*sdkauth.TokenInfo, error) {
		p, err := auth.Verify(ctx, st, token)
		if errors.Is(err, auth.ErrInvalid) {
			// Logged, not written to the database: anyone can reach this path,
			// and an unauthenticated flood must not be able to fill the disk.
			log.Warn("rejected token", "remote", remoteAddr(r))
			return nil, fmt.Errorf("%w", sdkauth.ErrInvalidToken)
		}
		if err != nil {
			log.Error("token lookup failed", "err", err)
			return nil, errors.New("internal error")
		}
		return &sdkauth.TokenInfo{UserID: p.TokenID, Extra: map[string]any{principalKey: p, remoteKey: remoteAddr(r)}}, nil
	}, &sdkauth.RequireBearerTokenOptions{AllowMissingExpiration: true})

	server := mcpserver.New(svc, version, func(_ context.Context, req *mcp.CallToolRequest) (core.Caller, error) {
		if req.Extra == nil || req.Extra.TokenInfo == nil {
			return core.Caller{}, errors.New("no token")
		}
		return callerFrom(req.Extra.TokenInfo, "mcp-http")
	}, log)
	// Stateless: every request is authenticated afresh and no session state
	// outlives a token. All tools are plain request/response, so nothing is lost.
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Behind a local reverse proxy the Host header is not localhost.
		// Bearer auth already stops the DNS rebinding this check targets.
		DisableLocalhostProtection: cfg.Server.AllowPlaintext,
	})

	api := &api{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.Handle("/mcp", requireToken(limitBody(mcpHandler)))
	protected := func(pattern string, h func(http.ResponseWriter, *http.Request, core.Caller)) {
		mux.Handle(pattern, requireToken(limitBody(api.withCaller(h))))
	}
	protected("POST /v1/search", api.search)
	protected("GET /v1/collections", api.collections)
	protected("GET /v1/documents", api.listDocuments)
	protected("GET /v1/documents/{id}", api.documentInfo)
	protected("GET /v1/documents/{id}/text", api.documentText)
	protected("GET /v1/documents/{id}/file", api.documentFile)
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAPI)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	return securityHeaders(mux)
}

// Serve runs the listener until ctx ends, then drains it.
func Serve(ctx context.Context, cfg *config.Config, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	errc := make(chan error, 1)
	go func() {
		var err error
		if cfg.Server.TLSCert != "" {
			err = srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		errc <- err
	}()
	log.Info("listening", "addr", cfg.Server.Listen, "tls", cfg.Server.TLSCert != "")
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func callerFrom(ti *sdkauth.TokenInfo, transport string) (core.Caller, error) {
	p, ok := ti.Extra[principalKey].(*auth.Principal)
	if !ok {
		return core.Caller{}, errors.New("no principal")
	}
	remote, _ := ti.Extra[remoteKey].(string)
	return core.Caller{Principal: p, Transport: transport, Remote: remote}, nil
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		next.ServeHTTP(w, r)
	})
}

type api struct {
	svc *core.Service
	log *slog.Logger
}

func (a *api) withCaller(h func(http.ResponseWriter, *http.Request, core.Caller)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := callerFrom(sdkauth.TokenInfoFromContext(r.Context()), "rest")
		if err != nil {
			a.fail(w, err)
			return
		}
		h(w, r, c)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (a *api) fail(w http.ResponseWriter, err error) {
	code, msg := http.StatusInternalServerError, "internal error"
	switch {
	case errors.Is(err, core.ErrNotFound):
		code, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, core.ErrInvalid):
		code, msg = http.StatusBadRequest, err.Error()
	default:
		a.log.Error("request failed", "err", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"message": msg}})
}

// respond writes v, or the error if there is one.
func respond[T any](a *api, w http.ResponseWriter, v T, err error) {
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, v)
}

// search is POST so that query text stays out of proxy and access logs.
func (a *api) search(w http.ResponseWriter, r *http.Request, c core.Caller) {
	var req core.SearchRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		a.fail(w, fmt.Errorf("%w: body must be a JSON search request", core.ErrInvalid))
		return
	}
	resp, err := a.svc.Search(r.Context(), c, req)
	respond(a, w, resp, err)
}

func (a *api) collections(w http.ResponseWriter, r *http.Request, c core.Caller) {
	cols, err := a.svc.ListCollections(r.Context(), c)
	respond(a, w, map[string]any{"collections": cols}, err)
}

func (a *api) listDocuments(w http.ResponseWriter, r *http.Request, c core.Caller) {
	q := r.URL.Query()
	limit, err := intParam(q.Get("limit"))
	if err != nil {
		a.fail(w, err)
		return
	}
	resp, err := a.svc.ListDocuments(r.Context(), c, core.ListRequest{
		Collections: split(q.Get("collection")), FileTypes: split(q.Get("file_type")),
		PathPrefix: q.Get("path_prefix"), Cursor: q.Get("cursor"), Limit: limit,
	})
	respond(a, w, resp, err)
}

func (a *api) documentInfo(w http.ResponseWriter, r *http.Request, c core.Caller) {
	resp, err := a.svc.GetDocumentInfo(r.Context(), c, r.PathValue("id"))
	respond(a, w, resp, err)
}

func (a *api) documentText(w http.ResponseWriter, r *http.Request, c core.Caller) {
	q := r.URL.Query()
	offset, err := intParam(q.Get("offset"))
	if err != nil {
		a.fail(w, err)
		return
	}
	maxChars, err := intParam(q.Get("max_chars"))
	if err != nil {
		a.fail(w, err)
		return
	}
	resp, err := a.svc.ReadDocument(r.Context(), c, core.ReadRequest{
		DocID: r.PathValue("id"), Section: q.Get("section"), Offset: offset, MaxChars: maxChars,
	})
	respond(a, w, resp, err)
}

// documentFile serves the original. It is always an attachment of an opaque
// type, because the file is untrusted and must not render on this origin.
func (a *api) documentFile(w http.ResponseWriter, r *http.Request, c core.Caller) {
	o, err := a.svc.OpenOriginal(r.Context(), c, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	defer o.File.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": o.Name}))
	http.ServeContent(w, r, "", o.ModTime, o.File)
}

func intParam(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", core.ErrInvalid, s)
	}
	return n, nil
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
