// Package embed adds optional semantic search through an on-prem embeddings
// service. It holds the only outbound network code in Roost, and nothing here
// runs unless an operator enables it in the config.
package embed

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/paulhovey/roost/internal/config"
	"github.com/paulhovey/roost/internal/store"
)

const (
	metaModel      = "embedding_model"
	metaGeneration = "embedding_generation"
	// reloadEvery bounds how often search rebuilds its in-memory vectors while
	// indexing is adding to them.
	reloadEvery  = 30 * time.Second
	queryTimeout = 10 * time.Second
	maxResponse  = 256 << 20
)

// Pipeline embeds chunks and answers vector queries.
type Pipeline struct {
	cfg    config.Embeddings
	st     *store.Store
	log    *slog.Logger
	client *http.Client
	apiKey string

	runMu sync.Mutex

	idxMu    sync.RWMutex
	idx      *index
	idxGen   string
	idxCheck time.Time
}

// New builds a Pipeline. It makes no connection until first used.
func New(cfg config.Embeddings, st *store.Store, log *slog.Logger) (*Pipeline, error) {
	p := &Pipeline{cfg: cfg, st: st, log: log, client: newClient(cfg)}
	if cfg.APIKeyEnv != "" {
		p.apiKey = os.Getenv(cfg.APIKeyEnv)
		if p.apiKey == "" {
			return nil, fmt.Errorf("embeddings: %s is not set", cfg.APIKeyEnv)
		}
	}
	return p, nil
}

// newClient builds the one outbound client in the program.
//
//   - The dialer checks each resolved IP at connect time. Checking the hostname
//     up front would not be enough: DNS can change between check and connect.
//   - Proxy settings are ignored, since an HTTP_PROXY in the environment would
//     quietly send document text to a host nobody configured here.
//   - Redirects are refused for the same reason.
func newClient(cfg config.Embeddings) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !cfg.AllowPublicEndpoint {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !internalIP(ip) {
				return fmt.Errorf("refusing to send data to non-private address %s (see embeddings.allow_public_endpoint)", host)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: cfg.Timeout.Duration,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 4,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("embeddings endpoint redirected; refusing to follow")
		},
	}
}

// internalIP accepts loopback and RFC 1918 / ULA space. Link-local is excluded
// because that range holds cloud metadata services.
func internalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate()
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// call fetches embeddings for texts, returned in input order.
func (p *Pipeline) call(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: p.cfg.Model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body is not logged: services may echo the input back in errors.
		return nil, fmt.Errorf("embeddings endpoint returned %s", resp.Status)
	}
	var out embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("asked for %d embeddings, got %d", len(texts), len(out.Data))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) || len(d.Embedding) == 0 {
			return nil, errors.New("embeddings response is malformed")
		}
		vecs[d.Index] = normalize(d.Embedding)
	}
	return vecs, nil
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return v
}

func encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// Run embeds every pending chunk. A failure ends the pass quietly enough to
// retry on the next one: keyword search does not depend on it.
func (p *Pipeline) Run(ctx context.Context) {
	if !p.runMu.TryLock() {
		return
	}
	defer p.runMu.Unlock()
	model, err := p.st.GetMeta(ctx, metaModel)
	if err != nil {
		p.log.Error("embeddings: read state", "err", err)
		return
	}
	if model != p.cfg.Model {
		if model != "" {
			p.log.Info("embedding model changed; re-embedding everything", "from", model, "to", p.cfg.Model)
		}
		if err := p.st.ClearEmbeddings(ctx); err != nil {
			p.log.Error("embeddings: clear", "err", err)
			return
		}
		if err := p.st.SetMeta(ctx, metaModel, p.cfg.Model); err != nil {
			p.log.Error("embeddings: save model", "err", err)
			return
		}
		p.bump(ctx)
	}
	done := 0
	for ctx.Err() == nil {
		pending, err := p.st.UnembeddedChunks(ctx, p.cfg.BatchSize)
		if err != nil {
			p.log.Error("embeddings: read chunks", "err", err)
			return
		}
		if len(pending) == 0 {
			break
		}
		texts := make([]string, len(pending))
		for i, c := range pending {
			texts[i] = c.Section + "\n" + c.Text
		}
		vecs, err := p.call(ctx, texts)
		if err != nil {
			p.log.Warn("embeddings: pass stopped; will retry on the next scan", "err", err, "embedded", done)
			break
		}
		batch := make(map[int64][]byte, len(vecs))
		for i, v := range vecs {
			batch[pending[i].ID] = encode(v)
		}
		if err := p.st.SetEmbeddings(ctx, batch); err != nil {
			p.log.Error("embeddings: save", "err", err)
			return
		}
		done += len(batch)
	}
	if done > 0 {
		p.log.Info("embeddings updated", "chunks", done)
		p.bump(ctx)
	}
}

func (p *Pipeline) bump(ctx context.Context) {
	gen := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := p.st.SetMeta(ctx, metaGeneration, gen); err != nil {
		p.log.Error("embeddings: save generation", "err", err)
	}
}

// index is every vector in one flat array, for scanning without pointer chasing.
type index struct {
	dim         int
	ids         []int64
	collections []int32 // index into names, parallel to ids
	names       []string
	vecs        []float32
}

func (p *Pipeline) current(ctx context.Context) (*index, error) {
	p.idxMu.RLock()
	idx, gen, checked := p.idx, p.idxGen, p.idxCheck
	p.idxMu.RUnlock()
	if idx != nil && time.Since(checked) < reloadEvery {
		return idx, nil
	}
	p.idxMu.Lock()
	defer p.idxMu.Unlock()
	if p.idx != nil && p.idxGen != gen { // another caller just reloaded
		return p.idx, nil
	}
	latest, err := p.st.GetMeta(ctx, metaGeneration)
	if err != nil {
		return nil, err
	}
	p.idxCheck = time.Now()
	if p.idx != nil && latest == p.idxGen {
		return p.idx, nil
	}
	fresh := &index{}
	byName := map[string]int32{}
	err = p.st.EachEmbedding(ctx, func(id int64, col string, b []byte) error {
		dim := len(b) / 4
		if fresh.dim == 0 {
			fresh.dim = dim
		}
		if dim != fresh.dim {
			return nil // leftover from a dimension change; a re-embed replaces it
		}
		ci, ok := byName[col]
		if !ok {
			ci = int32(len(fresh.names))
			byName[col] = ci
			fresh.names = append(fresh.names, col)
		}
		fresh.ids = append(fresh.ids, id)
		fresh.collections = append(fresh.collections, ci)
		for i := range dim {
			fresh.vecs = append(fresh.vecs, math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	p.idx, p.idxGen = fresh, latest
	return fresh, nil
}

// Search returns the chunks nearest to query, best first. Vectors from
// collections outside allowed are never scored.
func (p *Pipeline) Search(ctx context.Context, query string, allowed []string, k int) ([]int64, error) {
	if len(allowed) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	idx, err := p.current(ctx)
	if err != nil {
		return nil, err
	}
	vecs, err := p.call(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return idx.search(vecs[0], allowed, k), nil
}

type scored struct {
	id    int64
	score float32
}

func (x *index) search(q []float32, allowed []string, k int) []int64 {
	if len(x.ids) == 0 || len(q) != x.dim {
		return nil
	}
	ok := make([]bool, len(x.names))
	for i, n := range x.names {
		ok[i] = slices.Contains(allowed, n)
	}
	workers := min(runtime.GOMAXPROCS(0), max(1, len(x.ids)/4096))
	parts := make([][]scored, workers)
	var wg sync.WaitGroup
	for w := range workers {
		lo, hi := w*len(x.ids)/workers, (w+1)*len(x.ids)/workers
		wg.Go(func() {
			best := make([]scored, 0, k+1)
			for i := lo; i < hi; i++ {
				if !ok[x.collections[i]] {
					continue
				}
				var dot float32
				v := x.vecs[i*x.dim : (i+1)*x.dim]
				for j, a := range q {
					dot += a * v[j]
				}
				if len(best) == k && dot <= best[k-1].score {
					continue
				}
				at, _ := slices.BinarySearchFunc(best, dot, func(s scored, t float32) int { return cmp.Compare(t, s.score) })
				best = slices.Insert(best, at, scored{x.ids[i], dot})
				if len(best) > k {
					best = best[:k]
				}
			}
			parts[w] = best
		})
	}
	wg.Wait()
	all := slices.Concat(parts...)
	slices.SortFunc(all, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	all = all[:min(k, len(all))]
	out := make([]int64, len(all))
	for i, s := range all {
		out[i] = s.id
	}
	return out
}
