// Package indexer keeps the store in step with the configured storage paths.
//
// Periodic scans are the source of truth. fsnotify only makes changes show up
// sooner, because inotify does not fire reliably on NFS and SMB mounts.
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/paulhovey/roost/internal/chunk"
	"github.com/paulhovey/roost/internal/config"
	"github.com/paulhovey/roost/internal/extract"
	"github.com/paulhovey/roost/internal/store"
)

// Indexer scans collections into the store.
type Indexer struct {
	cfg    *config.Config
	st     *store.Store
	runner *extract.Runner
	log    *slog.Logger

	// AfterScan, if set, runs after each scan pass. Embeddings hook in here.
	AfterScan func(context.Context)
}

// New builds an Indexer.
func New(cfg *config.Config, st *store.Store, runner *extract.Runner, log *slog.Logger) *Indexer {
	return &Indexer{cfg: cfg, st: st, runner: runner, log: log}
}

// Stats counts what one scan did.
type Stats struct {
	Seen, Indexed, Unchanged, Deleted, Problems int
	Skipped                                     int      // symlinks and special files left alone
	DeleteBlocked                               bool     // stale entries kept; see Scan
	Dirs                                        []string // absolute paths, for the watcher
}

// DocID derives a stable ID from a document's location. It deliberately omits
// content, so a document keeps its ID across edits.
func DocID(collection, rel string) string {
	sum := sha256.Sum256([]byte(collection + "\x00" + rel))
	return hex.EncodeToString(sum[:16])
}

// ScanAll scans every collection, carrying on past individual failures.
func (ix *Indexer) ScanAll(ctx context.Context, prune bool) (map[string]Stats, error) {
	out := map[string]Stats{}
	var errs []error
	for _, col := range ix.cfg.Collections {
		st, err := ix.Scan(ctx, col, prune)
		out[col.Name] = st
		if err != nil {
			errs = append(errs, fmt.Errorf("collection %s: %w", col.Name, err))
			ix.log.Error("scan failed", "collection", col.Name, "err", err)
		}
	}
	if ix.AfterScan != nil && ctx.Err() == nil {
		ix.AfterScan(ctx)
	}
	return out, errors.Join(errs...)
}

// Scan brings one collection up to date.
//
// Stale entries are removed only after a complete walk. An unmounted share
// looks like a missing or empty directory, so an incomplete walk deletes
// nothing, and an entirely empty root needs prune=true: otherwise a brief
// mount failure would erase the index for the whole collection.
func (ix *Indexer) Scan(ctx context.Context, col config.Collection, prune bool) (Stats, error) {
	var stats Stats
	root, err := os.OpenRoot(col.Path)
	if err != nil {
		stats.DeleteBlocked = true
		return stats, fmt.Errorf("open collection root: %w", err)
	}
	defer root.Close()

	existing, err := ix.st.DocumentsIn(ctx, col.Name)
	if err != nil {
		return stats, err
	}

	type job struct {
		rel  string
		prev *store.Document
	}
	jobs := make(chan job)
	var seen sync.Map
	var indexed, unchanged, problems atomic.Int64
	var wg sync.WaitGroup
	for range ix.cfg.Index.Workers {
		wg.Go(func() {
			for j := range jobs {
				outcome, err := ix.process(ctx, col, root, j.rel, j.prev)
				if err != nil {
					if ctx.Err() == nil {
						ix.log.Error("index file", "collection", col.Name, "path", j.rel, "err", err)
					}
					problems.Add(1)
					continue
				}
				switch outcome {
				case outcomeUnchanged:
					unchanged.Add(1)
				case outcomeIndexed:
					indexed.Add(1)
				case outcomeProblem:
					problems.Add(1)
				}
			}
		})
	}

	complete := true
	walkErr := fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			complete = false
			ix.log.Warn("walk", "collection", col.Name, "path", rel, "err", err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if rel == "." {
			stats.Dirs = append(stats.Dirs, col.Path)
			return nil
		}
		if ignored(rel, d.Name(), col.Exclude) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			stats.Dirs = append(stats.Dirs, filepath.Join(col.Path, filepath.FromSlash(rel)))
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			// root.Stat refuses links that leave the collection. Directory
			// links are skipped too, because they can form cycles.
			fi, err := root.Stat(rel)
			if err != nil || !fi.Mode().IsRegular() {
				stats.Skipped++
				ix.log.Debug("skip symlink", "collection", col.Name, "path", rel)
				return nil
			}
		case !d.Type().IsRegular():
			stats.Skipped++
			return nil
		}
		seen.Store(rel, true)
		stats.Seen++
		j := job{rel: rel}
		if prev, ok := existing[rel]; ok {
			j.prev = &prev
		}
		select {
		case jobs <- j:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	close(jobs)
	wg.Wait()

	stats.Indexed, stats.Unchanged, stats.Problems = int(indexed.Load()), int(unchanged.Load()), int(problems.Load())
	if walkErr != nil {
		return stats, walkErr
	}

	var stale []string
	for rel, d := range existing {
		if _, ok := seen.Load(rel); !ok {
			stale = append(stale, d.ID)
		}
	}
	switch {
	case len(stale) == 0:
	case !complete:
		stats.DeleteBlocked = true
		ix.log.Warn("walk was incomplete; keeping stale documents", "collection", col.Name, "stale", len(stale))
	case stats.Seen == 0 && !prune:
		stats.DeleteBlocked = true
		ix.log.Warn("collection root is empty; it may be unmounted. Keeping documents. Run `roost index --prune` if the removal is intended",
			"collection", col.Name, "documents", len(stale))
	default:
		if err := ix.st.DeleteDocuments(ctx, stale); err != nil {
			return stats, err
		}
		stats.Deleted = len(stale)
	}
	if err := ix.st.SetMeta(ctx, "last_scan:"+col.Name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return stats, err
	}
	return stats, nil
}

// ignored skips hidden entries, Office lock files, and configured excludes.
// Patterns match a path relative to the collection, or any single name.
func ignored(rel, name string, exclude []string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return true
	}
	for _, p := range exclude {
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

type outcome int

const (
	outcomeUnchanged outcome = iota
	outcomeIndexed
	outcomeProblem
)

func (ix *Indexer) process(ctx context.Context, col config.Collection, root *os.Root, rel string, prev *store.Document) (outcome, error) {
	fi, err := root.Stat(rel)
	if err != nil {
		return outcomeProblem, err
	}
	mtime := fi.ModTime().UnixNano()
	current := prev != nil && prev.ExtractorVersion == extract.Version
	if current && prev.Size == fi.Size() && prev.MTimeNS == mtime {
		return outcomeUnchanged, nil
	}

	ext := strings.ToLower(path.Ext(rel))
	name := path.Base(rel)
	doc := store.Document{
		ID: DocID(col.Name, rel), Collection: col.Name, RelPath: rel,
		Title: strings.TrimSuffix(name, path.Ext(name)), FileType: strings.TrimPrefix(ext, "."),
		Size: fi.Size(), MTimeNS: mtime, ExtractorVersion: extract.Version,
	}
	doc.SearchTitle = searchTitle(doc.Title, rel)

	// These two skip reading the file: it may be enormous, and there is
	// nothing to gain from hashing what will not be indexed.
	if !extract.Supported(ext) {
		doc.Status = store.StatusUnsupported
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}
	if fi.Size() > ix.cfg.Index.MaxFileMB<<20 {
		doc.Status = store.StatusTooLarge
		doc.Error = fmt.Sprintf("file is %d MiB; the limit is %d MiB", fi.Size()>>20, ix.cfg.Index.MaxFileMB)
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}

	f, err := root.Open(rel)
	if err != nil {
		doc.Status, doc.Error = store.StatusFailed, err.Error()
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		doc.Status, doc.Error = store.StatusFailed, err.Error()
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}
	doc.SHA256 = hex.EncodeToString(h.Sum(nil))
	if current && prev.SHA256 == doc.SHA256 {
		// Touched, or copied over with identical content.
		return outcomeUnchanged, ix.st.TouchDocument(ctx, doc.ID, mtime)
	}

	res, err := ix.runner.Extract(ctx, f, ext)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return outcomeProblem, ctx.Err()
	case errors.Is(err, extract.ErrEncrypted):
		doc.Status = store.StatusEncrypted
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	default:
		doc.Status, doc.Error = store.StatusFailed, err.Error()
		ix.log.Warn("extraction failed", "collection", col.Name, "path", rel, "err", err)
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}

	if res.Title != "" {
		doc.Title = res.Title
		doc.SearchTitle = searchTitle(doc.Title, rel)
	}
	doc.Meta = res.Meta
	doc.Truncated = res.Truncated
	chunks := chunk.Split(res.Sections)
	if len(chunks) == 0 {
		doc.Status = store.StatusNoText
		return outcomeProblem, ix.st.ReplaceDocument(ctx, doc, nil)
	}
	doc.Status = store.StatusOK
	if err := ix.st.ReplaceDocument(ctx, doc, chunks); err != nil {
		return outcomeProblem, err
	}
	return outcomeIndexed, nil
}

func searchTitle(title, rel string) string {
	return title + " " + strings.NewReplacer("/", " ", "_", " ", "-", " ", ".", " ").Replace(rel)
}

// Run scans, then keeps watching until ctx ends.
func (ix *Indexer) Run(ctx context.Context) error {
	watcher := ix.newWatcher()
	if watcher != nil {
		defer watcher.Close()
	}
	attach := func(stats map[string]Stats) {
		if watcher == nil {
			return
		}
		for _, st := range stats {
			for _, dir := range st.Dirs {
				if err := watcher.Add(dir); err != nil {
					// Usually the inotify limit. Rescans still cover everything.
					ix.log.Warn("cannot watch directory; relying on periodic rescan", "dir", dir, "err", err)
					watcher.Close()
					watcher = nil
					return
				}
			}
		}
	}

	stats, _ := ix.ScanAll(ctx, false)
	attach(stats)

	ticker := time.NewTicker(ix.cfg.Index.RescanInterval.Duration)
	defer ticker.Stop()
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	defer debounce.Stop()
	dirty := map[string]bool{}

	for {
		var events chan fsnotify.Event
		var errs chan error
		if watcher != nil {
			events, errs = watcher.Events, watcher.Errors
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stats, _ := ix.ScanAll(ctx, false)
			attach(stats)
		case ev, ok := <-events:
			if !ok {
				watcher = nil
				continue
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			if name := ix.collectionFor(ev.Name); name != "" {
				dirty[name] = true
				debounce.Reset(ix.cfg.Index.WatchDebounce.Duration)
			}
		case err, ok := <-errs:
			if ok {
				ix.log.Warn("watcher", "err", err)
			}
		case <-debounce.C:
			for name := range dirty {
				col, _ := ix.cfg.Collection(name)
				st, err := ix.Scan(ctx, col, false)
				if err != nil {
					ix.log.Error("scan failed", "collection", name, "err", err)
					continue
				}
				attach(map[string]Stats{name: st})
			}
			clear(dirty)
			if ix.AfterScan != nil {
				ix.AfterScan(ctx)
			}
		}
	}
}

func (ix *Indexer) newWatcher() *fsnotify.Watcher {
	if !ix.cfg.Index.Watch {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		ix.log.Warn("file watching unavailable; relying on periodic rescan", "err", err)
		return nil
	}
	return w
}

func (ix *Indexer) collectionFor(p string) string {
	for _, c := range ix.cfg.Collections {
		if p == c.Path || strings.HasPrefix(p, c.Path+string(filepath.Separator)) {
			return c.Name
		}
	}
	return ""
}
