// Command roost serves internal documents to AI tools over MCP and REST,
// without making outbound network calls.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/paulhovey/roost/internal/auth"
	"github.com/paulhovey/roost/internal/config"
	"github.com/paulhovey/roost/internal/core"
	"github.com/paulhovey/roost/internal/embed"
	"github.com/paulhovey/roost/internal/extract"
	"github.com/paulhovey/roost/internal/indexer"
	"github.com/paulhovey/roost/internal/mcpserver"
	"github.com/paulhovey/roost/internal/rest"
	"github.com/paulhovey/roost/internal/store"
)

// version is set at build time with -ldflags.
var version = "dev"

const usage = `roost: internal documents for AI tools, with no outbound calls by default.

Usage: roost <command> [flags]

Commands:
  serve                  Index documents and serve MCP and REST over HTTP
  mcp                    Serve MCP over stdio (needs ROOST_TOKEN)
  index [--prune]        Run one indexing pass, then exit
  status                 Show index coverage and problem files
  audit [-n N]           Show recent access log entries
  token create           Issue a token: --name NAME --collections a,b|--all [--ttl 720h]
  token list             List tokens
  token revoke ID        Revoke a token
  version                Print the version

Every command except version takes --config PATH (default: $ROOST_CONFIG, then ./roost.toml).
`

func main() {
	// The worker path must stay first: it runs on hostile input and must not
	// touch configuration, logging, or the database.
	if len(os.Args) > 1 && os.Args[1] == extract.WorkerCommand {
		os.Exit(extract.WorkerMain(os.Args[2:]))
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "roost:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(ctx, rest)
	case "mcp":
		return cmdMCP(ctx, rest)
	case "index":
		return cmdIndex(ctx, rest)
	case "status":
		return cmdStatus(ctx, rest)
	case "audit":
		return cmdAudit(ctx, rest)
	case "token":
		return cmdToken(ctx, rest)
	case "version":
		fmt.Println("roost", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q; run `roost help`", cmd)
}

// app holds what most commands need.
type app struct {
	cfg *config.Config
	st  *store.Store
	log *slog.Logger
}

func (a *app) Close() { a.st.Close() }

func open(path string) (*app, error) {
	cfg, err := config.Load(config.Find(path))
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.Storage.DataDir)
	if err != nil {
		return nil, err
	}
	// stderr only: stdout carries the protocol in `roost mcp`.
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return &app{cfg: cfg, st: st, log: log}, nil
}

func (a *app) newIndexer() (*indexer.Indexer, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate binary: %w", err)
	}
	idx := a.cfg.Index
	return indexer.New(a.cfg, a.st, &extract.Runner{
		Exe:      exe,
		Limits:   extract.Limits{MaxText: idx.MaxTextMB << 20, MaxUnzip: 4 * (idx.MaxFileMB << 20)},
		Timeout:  idx.ExtractTimeout.Duration,
		MemoryMB: idx.WorkerMemoryMB,
	}, a.log), nil
}

// newEmbedder returns nil unless embeddings are on. It is the only place an
// outbound client can come into being.
func (a *app) newEmbedder() (*embed.Pipeline, error) {
	if !a.cfg.Embeddings.Enabled {
		return nil, nil
	}
	return embed.New(a.cfg.Embeddings, a.st, a.log)
}

func (a *app) newService(p *embed.Pipeline) (*core.Service, error) {
	var vs core.VectorSearcher
	if p != nil {
		vs = p
	}
	return core.New(a.cfg, a.st, a.log, vs)
}

func (a *app) logEgress() {
	if e := a.cfg.Embeddings; e.Enabled {
		a.log.Info("egress: embeddings endpoint only", "endpoint", e.Endpoint)
		return
	}
	a.log.Info("egress: none")
}

func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	fs.Parse(args)
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	a.logEgress()

	emb, err := a.newEmbedder()
	if err != nil {
		return err
	}
	svc, err := a.newService(emb)
	if err != nil {
		return err
	}
	defer svc.Close()
	ix, err := a.newIndexer()
	if err != nil {
		return err
	}
	if emb != nil {
		ix.AfterScan = emb.Run
	}
	go func() {
		if err := ix.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("indexer stopped", "err", err)
		}
	}()
	return rest.Serve(ctx, a.cfg, rest.Handler(a.cfg, svc, a.st, version, a.log), a.log)
}

func cmdMCP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	fs.Parse(args)
	secret := os.Getenv("ROOST_TOKEN")
	if secret == "" {
		return errors.New("ROOST_TOKEN is not set")
	}
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	a.logEgress()
	if _, err := auth.Verify(ctx, a.st, secret); err != nil {
		return fmt.Errorf("ROOST_TOKEN: %w", err)
	}
	emb, err := a.newEmbedder()
	if err != nil {
		return err
	}
	svc, err := a.newService(emb)
	if err != nil {
		return err
	}
	defer svc.Close()
	server := mcpserver.New(svc, version, func(ctx context.Context, _ *mcp.CallToolRequest) (core.Caller, error) {
		p, err := auth.Verify(ctx, a.st, secret)
		if err != nil {
			return core.Caller{}, err
		}
		return core.Caller{Principal: p, Transport: "mcp-stdio"}, nil
	}, a.log)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func cmdIndex(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	prune := fs.Bool("prune", false, "allow removing all documents of an empty collection, and of collections no longer configured")
	fs.Parse(args)
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	a.logEgress()
	ix, err := a.newIndexer()
	if err != nil {
		return err
	}
	emb, err := a.newEmbedder()
	if err != nil {
		return err
	}
	if emb != nil {
		ix.AfterScan = emb.Run
	}
	stats, err := ix.ScanAll(ctx, *prune)
	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := stats[n]
		fmt.Printf("%s: %d files: %d indexed, %d unchanged, %d deleted, %d need attention\n", n, s.Seen, s.Indexed, s.Unchanged, s.Deleted, s.Problems)
		if s.DeleteBlocked {
			fmt.Printf("%s: stale documents were kept (root missing, empty, or partly unreadable)\n", n)
		}
	}
	orphans, oerr := a.st.OrphanCollections(ctx, a.cfg.CollectionNames())
	if oerr != nil {
		return errors.Join(err, oerr)
	}
	for _, o := range orphans {
		if *prune {
			if derr := a.st.DeleteCollection(ctx, o); derr != nil {
				return errors.Join(err, derr)
			}
			fmt.Printf("%s: removed (no longer configured)\n", o)
		} else {
			fmt.Printf("%s: no longer configured; unreadable, but still in the database. Use --prune to delete it\n", o)
		}
	}
	return err
}

func cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	limit := fs.Int("problems", 50, "maximum problem files to list")
	fs.Parse(args)
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	status, err := a.st.Status(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COLLECTION\tOK\tCHUNKS\tNO TEXT\tUNSUPPORTED\tENCRYPTED\tTOO LARGE\tFAILED\tLAST SCAN")
	for _, name := range a.cfg.CollectionNames() {
		s := status[name]
		if s == nil {
			s = &store.CollectionStatus{ByStatus: map[string]int{}}
		}
		last, err := a.st.GetMeta(ctx, "last_scan:"+name)
		if err != nil {
			return err
		}
		if last == "" {
			last = "never"
		}
		b := s.ByStatus
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n", name, b[store.StatusOK], s.Chunks, b[store.StatusNoText],
			b[store.StatusUnsupported], b[store.StatusEncrypted], b[store.StatusTooLarge], b[store.StatusFailed], last)
	}
	w.Flush()
	if a.cfg.Embeddings.Enabled {
		missing, total, err := a.st.EmbeddingProgress(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("\nEmbeddings: %d of %d chunks done\n", total-missing, total)
	}
	probs, err := a.st.Problems(ctx, *limit)
	if err != nil {
		return err
	}
	if len(probs) > 0 {
		fmt.Printf("\nFiles needing attention (up to %d):\n", *limit)
		for _, d := range probs {
			state := d.Status
			if d.Truncated {
				state += " (truncated)"
			}
			line := fmt.Sprintf("  %-12s %s/%s", state, d.Collection, d.RelPath)
			if d.Error != "" {
				line += ": " + d.Error
			}
			fmt.Println(line)
		}
	}
	return nil
}

func cmdAudit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	n := fs.Int("n", 50, "entries to show")
	fs.Parse(args)
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	entries, err := a.st.RecentAudit(ctx, *n)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tTOKEN\tVIA\tREMOTE\tOP\tOUTCOME\tDOCS\tQUERY")
	for _, e := range slices.Backward(entries) {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n", e.Time.Format(time.RFC3339), e.TokenName, e.Transport, e.Remote, e.Op, e.Outcome, len(e.DocIDs), e.Query)
	}
	return w.Flush()
}

func cmdToken(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: roost token create|list|revoke")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("token "+sub, flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	name := fs.String("name", "", "who or what the token is for")
	cols := fs.String("collections", "", "comma-separated collections")
	all := fs.Bool("all", false, "grant every collection, including future ones")
	ttl := fs.Duration("ttl", 0, "lifetime, such as 720h (default: no expiry)")
	fs.Parse(rest)
	a, err := open(*cfgPath)
	if err != nil {
		return err
	}
	defer a.Close()
	switch sub {
	case "create":
		var grant []string
		switch {
		case *all && *cols != "":
			return errors.New("use --all or --collections, not both")
		case *all:
			grant = []string{auth.AllCollections}
		case *cols == "":
			return errors.New("--collections or --all is required")
		default:
			for c := range strings.SplitSeq(*cols, ",") {
				c = strings.TrimSpace(c)
				if _, ok := a.cfg.Collection(c); !ok {
					return fmt.Errorf("no collection named %q in the config", c)
				}
				grant = append(grant, c)
			}
		}
		secret, rec, err := auth.Create(ctx, a.st, *name, grant, *ttl)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Created token %s for %q. Store it now; it cannot be shown again.\n", rec.ID, rec.Name)
		fmt.Println(secret)
		return nil
	case "list":
		tokens, err := a.st.ListTokens(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tCOLLECTIONS\tCREATED\tEXPIRES\tSTATE")
		for _, t := range tokens {
			exp, state := "never", "active"
			if t.ExpiresAt != nil {
				exp = t.ExpiresAt.Format(time.DateOnly)
				if time.Now().After(*t.ExpiresAt) {
					state = "expired"
				}
			}
			if t.RevokedAt != nil {
				state = "revoked"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, strings.Join(t.Collections, ","), t.CreatedAt.Format(time.DateOnly), exp, state)
		}
		return w.Flush()
	case "revoke":
		if fs.NArg() != 1 {
			return errors.New("usage: roost token revoke ID")
		}
		if err := a.st.RevokeToken(ctx, fs.Arg(0)); err != nil {
			return fmt.Errorf("revoke %s: %w", fs.Arg(0), err)
		}
		fmt.Println("revoked", fs.Arg(0))
		return nil
	}
	return fmt.Errorf("unknown token command %q", sub)
}
