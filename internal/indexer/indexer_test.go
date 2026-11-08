package indexer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/extract"
	"github.com/ClackStack/roost/internal/store"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == extract.WorkerCommand {
		os.Exit(extract.WorkerMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

type fixture struct {
	t   *testing.T
	dir string
	cfg *config.Config
	st  *store.Store
	ix  *Indexer
	col config.Collection
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Collections = []config.Collection{{Name: "c", Path: docs, Exclude: []string{"*.tmp", "drafts"}}}
	cfg.Index.Workers = 2
	cfg.Index.WatchDebounce.Duration = 50 * time.Millisecond
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Storage.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	exe, _ := os.Executable()
	r := &extract.Runner{Exe: exe, Limits: extract.Limits{MaxText: 1 << 20, MaxUnzip: 1 << 24}, Timeout: 20 * time.Second, MemoryMB: testWorkerMemoryMB}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &fixture{t: t, dir: dir, cfg: &cfg, st: st, ix: New(&cfg, st, r, log), col: cfg.Collections[0]}
}

func (f *fixture) write(rel, body string) {
	f.t.Helper()
	p := filepath.Join(f.col.Path, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) scan(prune bool) Stats {
	f.t.Helper()
	st, err := f.ix.Scan(context.Background(), f.col, prune)
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *fixture) docs() map[string]store.Document {
	f.t.Helper()
	m, err := f.st.DocumentsIn(context.Background(), "c")
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func TestLifecycle(t *testing.T) {
	f := setup(t)
	f.write("a.txt", "first version")
	f.write("sub/b.md", "# Title\nbody")

	st := f.scan(false)
	if st.Indexed != 2 || st.Seen != 2 {
		t.Fatalf("first scan: %+v", st)
	}
	id := f.docs()["a.txt"].ID

	if st := f.scan(false); st.Unchanged != 2 || st.Indexed != 0 {
		t.Fatalf("rescan: %+v", st)
	}

	f.write("a.txt", "second version, longer")
	if st := f.scan(false); st.Indexed != 1 || st.Unchanged != 1 {
		t.Fatalf("edit: %+v", st)
	}
	d := f.docs()["a.txt"]
	if d.ID != id {
		t.Error("document ID changed across an edit")
	}

	// New mtime, same bytes: hashed, then only touched.
	later := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(f.col.Path, "a.txt"), later, later)
	if st := f.scan(false); st.Unchanged != 2 {
		t.Fatalf("touch: %+v", st)
	}
	if got := f.docs()["a.txt"].MTimeNS; got != later.UnixNano() {
		t.Error("touch did not record the new mtime")
	}

	os.Remove(filepath.Join(f.col.Path, "a.txt"))
	if st := f.scan(false); st.Deleted != 1 {
		t.Fatalf("delete: %+v", st)
	}
	if _, ok := f.docs()["a.txt"]; ok {
		t.Error("deleted file still indexed")
	}
}

func TestProblemFilesAreRecorded(t *testing.T) {
	f := setup(t)
	f.cfg.Index.MaxFileMB = 1
	f.write("old.doc", "legacy")
	f.write("broken.docx", "not a zip")
	f.write("huge.txt", string(make([]byte, 2<<20)))
	f.write("empty.txt", "   \n")
	enc, _ := os.ReadFile("../extract/testdata/encrypted.pdf")
	f.write("locked.pdf", string(enc))
	scan, _ := os.ReadFile("../extract/testdata/scanned.pdf")
	f.write("scan.pdf", string(scan))
	f.scan(false)

	want := map[string]string{
		"old.doc": store.StatusUnsupported, "broken.docx": store.StatusFailed, "huge.txt": store.StatusTooLarge,
		"empty.txt": store.StatusNoText, "locked.pdf": store.StatusEncrypted, "scan.pdf": store.StatusNoText,
	}
	docs := f.docs()
	for rel, status := range want {
		if docs[rel].Status != status {
			t.Errorf("%s: status = %q (%s), want %q", rel, docs[rel].Status, docs[rel].Error, status)
		}
	}
	probs, err := f.st.Problems(context.Background(), 100)
	if err != nil || len(probs) != len(want) {
		t.Fatalf("problems = %d, err %v", len(probs), err)
	}
}

func TestIgnoredFiles(t *testing.T) {
	f := setup(t)
	f.write("keep.txt", "x")
	f.write(".hidden.txt", "x")
	f.write(".git/config.txt", "x")
	f.write("~$lock.docx", "x")
	f.write("scratch.tmp", "x")
	f.write("drafts/wip.txt", "x")
	f.scan(false)
	docs := f.docs()
	if len(docs) != 1 || docs["keep.txt"].ID == "" {
		t.Fatalf("docs = %v", docs)
	}
}

func TestNewExcludeRemovesDocuments(t *testing.T) {
	f := setup(t)
	f.write("keep.txt", "x")
	f.write("secret.txt", "x")
	f.scan(false)
	f.col.Exclude = []string{"secret.txt"}
	if st := f.scan(false); st.Deleted != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestSymlinks(t *testing.T) {
	f := setup(t)
	outside := filepath.Join(f.dir, "outside.txt")
	os.WriteFile(outside, []byte("exfiltrated secret"), 0o600)
	f.write("real.txt", "inside")
	os.Symlink(outside, filepath.Join(f.col.Path, "escape.txt"))
	os.Symlink("../outside.txt", filepath.Join(f.col.Path, "escape-rel.txt"))
	os.Symlink("real.txt", filepath.Join(f.col.Path, "alias.txt"))
	os.Symlink(".", filepath.Join(f.col.Path, "loop"))
	st := f.scan(false)
	docs := f.docs()
	if _, ok := docs["escape.txt"]; ok {
		t.Error("absolute symlink out of root was indexed")
	}
	if _, ok := docs["escape-rel.txt"]; ok {
		t.Error("relative symlink out of root was indexed")
	}
	if _, ok := docs["alias.txt"]; !ok {
		t.Error("symlink inside root was skipped")
	}
	if st.Skipped != 3 {
		t.Errorf("skipped = %d", st.Skipped)
	}
}

func TestUnmountedShareKeepsIndex(t *testing.T) {
	f := setup(t)
	f.write("a.txt", "x")
	f.scan(false)

	// Root gone entirely.
	moved := f.col.Path + ".away"
	os.Rename(f.col.Path, moved)
	if _, err := f.ix.Scan(context.Background(), f.col, false); err == nil {
		t.Fatal("want an error for a missing root")
	}
	if len(f.docs()) != 1 {
		t.Fatal("missing root wiped the index")
	}

	// Empty mountpoint left behind.
	os.Mkdir(f.col.Path, 0o700)
	if st := f.scan(false); !st.DeleteBlocked || len(f.docs()) != 1 {
		t.Fatalf("empty root wiped the index: %+v", st)
	}

	// An operator can confirm the removal.
	if st := f.scan(true); st.Deleted != 1 || len(f.docs()) != 0 {
		t.Fatalf("prune: %+v", st)
	}
}

func TestUnreadableSubdirBlocksDeletes(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permissions")
	}
	f := setup(t)
	f.write("sub/a.txt", "x")
	f.write("b.txt", "x")
	f.scan(false)
	sub := filepath.Join(f.col.Path, "sub")
	os.Chmod(sub, 0)
	t.Cleanup(func() { os.Chmod(sub, 0o700) })
	st := f.scan(false)
	if !st.DeleteBlocked || f.docs()["sub/a.txt"].ID == "" {
		t.Fatalf("permission error caused a delete: %+v", st)
	}
}

func TestWatcherPicksUpChanges(t *testing.T) {
	f := setup(t)
	f.cfg.Index.RescanInterval.Duration = time.Hour // only the watcher can act
	f.write("a.txt", "x")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.ix.Run(ctx) }()
	defer func() { cancel(); <-done }()

	waitFor(t, func() bool { return len(f.docs()) == 1 })
	time.Sleep(100 * time.Millisecond) // let the watcher attach
	f.write("sub/new.txt", "created after start")
	waitFor(t, func() bool { return f.docs()["sub/new.txt"].ID != "" })
	// sub/ did not exist at the first scan, so this proves new dirs get watched.
	f.write("sub/second.txt", "in a new directory")
	waitFor(t, func() bool { return f.docs()["sub/second.txt"].ID != "" })
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("condition not reached")
}

func TestExtractorUpgradeReindexes(t *testing.T) {
	f := setup(t)
	f.write("a.txt", "x")
	f.scan(false)
	d := f.docs()["a.txt"]
	d.ExtractorVersion = extract.Version - 1
	d.SearchTitle = "a"
	if err := f.st.ReplaceDocument(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	if st := f.scan(false); st.Indexed != 1 {
		t.Fatalf("%+v", st)
	}
}
