package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/core"
	"github.com/ClackStack/roost/internal/indexer"
	"github.com/ClackStack/roost/internal/store"
)

type tuiHarness struct {
	t *testing.T
	m *appModel
}

func newTestHarness(t *testing.T, opts Options) *tuiHarness {
	t.Helper()
	if opts.Index == nil {
		opts.Index = func(_ context.Context, cfg *config.Config, _ *store.Store) (map[string]indexer.Stats, error) {
			out := map[string]indexer.Stats{}
			for _, col := range cfg.Collections {
				out[col.Name] = indexer.Stats{Seen: 1, Indexed: 1}
			}
			return out, nil
		}
	}
	m, err := newAppModel(t.Context(), opts)
	if err != nil {
		t.Fatalf("newAppModel: %v", err)
	}
	t.Cleanup(m.Close)
	return &tuiHarness{t: t, m: m}
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+b":
		return tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	default:
		r := []rune(s)
		return tea.KeyPressMsg{Code: r[0], Text: s}
	}
}

func (h *tuiHarness) send(msg tea.Msg, runImmediateCmd bool) {
	h.t.Helper()
	next, cmd := h.m.Update(msg)
	h.m = next.(*appModel)
	if runImmediateCmd && cmd != nil {
		h.execImmediate(cmd, 0)
	}
}

func (h *tuiHarness) execImmediate(cmd tea.Cmd, depth int) {
	h.t.Helper()
	if cmd == nil || depth > 4 {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			h.execImmediate(sub, depth+1)
		}
		return
	}
	next, nextCmd := h.m.Update(msg)
	h.m = next.(*appModel)
	if nextCmd != nil {
		h.execImmediate(nextCmd, depth+1)
	}
}

func (h *tuiHarness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyPress(k), false)
	}
}

func (h *tuiHarness) submit(k string) {
	h.t.Helper()
	h.send(keyPress(k), true)
}

func (h *tuiHarness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.send(keyPress("space"), false)
		} else {
			h.send(tea.KeyPressMsg{Code: r, Text: string(r)}, false)
		}
	}
}

func (h *tuiHarness) clearAndType(s string) {
	h.t.Helper()
	h.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, false)
	h.typeText(s)
}

func (h *tuiHarness) view() string {
	return h.m.View().Content
}

func TestIsInteractiveAndRunRefusesNonTTY(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer
	if IsInteractive(in, &out) {
		t.Fatal("expected IsInteractive to be false for Reader/Buffer")
	}
	err := Run(t.Context(), Options{
		ConfigPath: filepath.Join(t.TempDir(), "roost.toml"),
		In:         in,
		Out:        &out,
	})
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("Run error = %v, want interactive terminal error", err)
	}
}

func TestHelpersSuggestCollectionNameAndExcludes(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/srv/documents/legal", "legal"},
		{"/srv/My Legal Docs!", "my_legal_docs"},
		{"/srv/2026-Q4", "2026-q4"},
		{"/", "docs"},
		{"/srv/---", "docs"},
	}
	for _, tc := range cases {
		if got := suggestCollectionName(tc.path); got != tc.want {
			t.Errorf("suggestCollectionName(%q) = %q, want %q", tc.path, got, tc.want)
		}
		if err := config.ValidCollectionName(suggestCollectionName(tc.path)); err != nil {
			t.Errorf("suggested name for %q is invalid: %v", tc.path, err)
		}
	}

	gotEx := parseExcludes(" *.tmp , drafts , , .git ")
	wantEx := []string{"*.tmp", "drafts", ".git"}
	if len(gotEx) != len(wantEx) {
		t.Fatalf("parseExcludes len = %d, want %d (%v)", len(gotEx), len(wantEx), gotEx)
	}
	for i := range wantEx {
		if gotEx[i] != wantEx[i] {
			t.Errorf("exclude[%d] = %q, want %q", i, gotEx[i], wantEx[i])
		}
	}
}

func TestFolderPickerDirectoryNavigationAndSelection(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/sub1", "docs/sub2", ".hidden", "alpha"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	fp := newFolderPicker("Pick Dir", root, false, defaultStyles(true), 100, 30)
	v := fp.View()
	if strings.Contains(v, "readme.txt") {
		t.Error("folder picker must not display regular files in directory list")
	}
	if strings.Contains(v, ".hidden") {
		t.Error("hidden directories should be hidden by default")
	}
	if !strings.Contains(v, "alpha/") || !strings.Contains(v, "docs/") {
		t.Fatalf("missing expected directories in view:\n%s", v)
	}

	// Toggle hidden directories on and off.
	fp, _ = fp.Update(keyPress("."))
	if !strings.Contains(fp.View(), ".hidden/") {
		t.Error("expected .hidden/ after pressing '.'")
	}
	fp, _ = fp.Update(keyPress("."))
	if strings.Contains(fp.View(), ".hidden/") {
		t.Error("expected .hidden/ hidden again")
	}

	// Move down to docs/ (entries: .., alpha/, docs/) and open it.
	fp, _ = fp.Update(keyPress("down")) // alpha/
	fp, _ = fp.Update(keyPress("down")) // docs/
	fp, _ = fp.Update(keyPress("enter"))
	if fp.currentDir != filepath.Join(root, "docs") {
		t.Fatalf("currentDir = %q, want %q", fp.currentDir, filepath.Join(root, "docs"))
	}

	// Go back up via backspace, then jump via '/' path input.
	fp, _ = fp.Update(keyPress("backspace"))
	if fp.currentDir != root {
		t.Fatalf("after backspace currentDir = %q, want %q", fp.currentDir, root)
	}

	fp, _ = fp.Update(keyPress("/"))
	fp, _ = fp.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	target := filepath.Join(root, "docs", "sub1")
	for _, r := range target {
		fp, _ = fp.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	fp, _ = fp.Update(keyPress("enter"))
	if fp.currentDir != target {
		t.Fatalf("after '/' jump currentDir = %q, want %q", fp.currentDir, target)
	}

	// Select current folder with Space.
	var cmd tea.Cmd
	fp, cmd = fp.Update(keyPress("space"))
	if cmd == nil {
		t.Fatal("expected selection command on Space")
	}
	res, ok := cmd().(folderPickerDoneMsg)
	if !ok || res.canceled || res.path != target {
		t.Fatalf("got result %+v, want path %q", res, target)
	}
}

func TestWizardEndToEndCreatesConfigAndToken(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	docsDir := filepath.Join(base, "Company Policies")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, Options{ConfigPath: cfgPath})
	if !h.m.inWizard {
		t.Fatal("expected wizard mode when config file does not exist")
	}
	if !strings.Contains(h.view(), "Roost Setup Wizard") {
		t.Fatalf("unexpected welcome view:\n%s", h.view())
	}

	// Step 0 -> Step 1 (Storage)
	h.press("enter")
	if h.m.wizard.step != wizStorage {
		t.Fatalf("step = %v, want wizStorage", h.m.wizard.step)
	}
	h.clearAndType(dataDir)
	h.press("enter") // Step 1 -> Step 2 (Collections)
	if h.m.wizard.step != wizCollections {
		t.Fatalf("step = %v, want wizCollections (err=%q)", h.m.wizard.step, h.m.wizard.errMsg)
	}

	// Step 2: add collection using FolderPicker via Ctrl+B, plus direct editing
	h.press("a")
	if h.m.wizard.colEd == nil {
		t.Fatal("expected collection editor to open")
	}
	// Trigger Ctrl+B to open folder picker, jump to docsDir, and pick it with Space.
	h.press("ctrl+b")
	if h.m.wizard.colEd.picker == nil {
		t.Fatal("expected folder picker to open on Ctrl+B")
	}
	h.press("/")
	h.clearAndType(docsDir)
	h.press("enter")
	h.submit("space")
	if h.m.wizard.colEd.picker != nil {
		t.Fatal("expected folder picker to close after Space")
	}
	if h.m.wizard.colEd.path.Value() != docsDir {
		t.Fatalf("picked path = %q, want %q", h.m.wizard.colEd.path.Value(), docsDir)
	}
	if h.m.wizard.colEd.name.Value() != "company_policies" {
		t.Fatalf("auto-suggested name = %q, want company_policies", h.m.wizard.colEd.name.Value())
	}
	h.clearAndType("policies") // focus is on Name
	h.press("tab")             // -> Exclude
	h.typeText("*.tmp, drafts")
	h.submit("ctrl+s")
	if h.m.wizard.colEd != nil {
		t.Fatalf("collection editor stayed open with err: %q", h.m.wizard.colEd.err)
	}
	if len(h.m.wizard.cfg.Collections) != 1 {
		t.Fatalf("collections len = %d, want 1", len(h.m.wizard.cfg.Collections))
	}

	// Advance Step 2 -> Step 3 (Server) -> Step 4 (Embeddings) -> Step 5 (Review)
	h.press("n")
	if h.m.wizard.step != wizServer {
		t.Fatalf("step = %v, want wizServer", h.m.wizard.step)
	}
	h.press("enter") // Loopback preset saves & advances to Embeddings
	if h.m.wizard.step != wizEmbeddings {
		t.Fatalf("step = %v, want wizEmbeddings (err=%q)", h.m.wizard.step, h.m.wizard.errMsg)
	}
	h.press("down", "enter") // Disabled embeddings -> Next -> Review
	if h.m.wizard.step != wizReview {
		t.Fatalf("step = %v, want wizReview (err=%q)", h.m.wizard.step, h.m.wizard.errMsg)
	}
	h.press("pgdown")
	if !strings.Contains(h.view(), `name = "policies"`) {
		t.Fatalf("review TOML missing collection:\n%s", h.view())
	}

	// Step 5 -> Step 6 (Save config & open DB)
	h.submit("enter")
	if h.m.wizard.step != wizFinish {
		t.Fatalf("step = %v, want wizFinish (err=%q)", h.m.wizard.step, h.m.wizard.errMsg)
	}
	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("saved config missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 0600", fi.Mode().Perm())
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load saved config: %v", err)
	}
	if loaded.Storage.DataDir != dataDir || len(loaded.Collections) != 1 || loaded.Collections[0].Name != "policies" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}

	// Create first token in Finish step.
	h.press("t")
	if h.m.wizard.tokModal == nil {
		t.Fatal("expected token modal in wizFinish")
	}
	h.clearAndType("quack-dev")
	h.press("tab", "tab", "tab") // Focus Create Token button (from Name -> All -> TTL -> Submit)
	h.submit("enter")
	if h.m.wizard.lastSecret == "" || !strings.HasPrefix(h.m.wizard.lastSecret, auth.Prefix) {
		t.Fatalf("lastSecret = %q, want %s prefix", h.m.wizard.lastSecret, auth.Prefix)
	}
	if _, err := auth.Verify(t.Context(), h.m.st, h.m.wizard.lastSecret); err != nil {
		t.Fatalf("created token did not verify against DB: %v", err)
	}
	if !strings.Contains(h.view(), h.m.wizard.lastSecret) {
		t.Error("expected generated secret displayed on Finish screen")
	}

	// Trigger index completion message cleanly and verify report.
	idxMsg := runIndexCmd(t.Context(), h.m.indexFn, &h.m.wizard.cfg, h.m.st)()
	h.send(idxMsg, false)
	if !strings.Contains(h.view(), "Indexed 1 files") {
		t.Fatalf("expected index summary in Finish view:\n%s", h.view())
	}

	// Switch from Finish step to Tabbed Configurator Dashboard.
	h.submit("d")
	if h.m.inWizard {
		t.Fatal("expected switchToDashboardMsg to transition out of wizard")
	}
	if !strings.Contains(h.view(), "Roost Configurator") {
		t.Fatalf("unexpected dashboard view:\n%s", h.view())
	}
}

func TestWizardRejectsDataDirOverlap(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "docs", "data")
	docsDir := filepath.Join(base, "docs")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	h := newTestHarness(t, Options{ConfigPath: cfgPath})
	h.press("enter") // Welcome -> Storage
	h.clearAndType(dataDir)
	h.press("enter") // Storage -> Collections
	h.press("a")     // Add collection
	h.clearAndType(docsDir)
	h.submit("ctrl+s")
	if h.m.wizard.colEd == nil {
		t.Fatal("expected collection editor to stay open on data_dir overlap")
	}
	if !strings.Contains(h.m.wizard.colEd.err, "overlaps storage.data_dir") {
		t.Fatalf("colEd.err = %q, want overlap error", h.m.wizard.colEd.err)
	}
	if len(h.m.wizard.cfg.Collections) != 0 {
		t.Fatalf("collections modified despite overlap: %+v", h.m.wizard.cfg.Collections)
	}
}

func TestConfiguratorCollectionsTokensAuditStatusSettings(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	col1Dir := filepath.Join(base, "legal")
	col2Dir := filepath.Join(base, "hr")
	for _, d := range []string{col1Dir, col2Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dataDir
	cfg.Collections = []config.Collection{{Name: "legal", Path: col1Dir}}
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}

	// Seed DB with a problem doc, an orphan collection, an audit log row, and a token.
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc1", Collection: "legal", RelPath: "scan.pdf", FileType: "pdf",
		Status: store.StatusNoText, Error: "scanned image only",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc2", Collection: "oldcol", RelPath: "old.txt", FileType: "text",
		Status: store.StatusOK,
	}, []store.Chunk{{Section: "1", Text: "legacy"}}); err != nil {
		t.Fatal(err)
	}
	seededSecret, seededTok, err := auth.Create(t.Context(), st, "existing", []string{auth.AllCollections}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAudit(t.Context(), store.AuditEntry{
		Time: time.Now(), TokenID: seededTok.ID, TokenName: "existing",
		Transport: "http", Remote: "127.0.0.1", Op: "search", Query: "vacation policy",
		DocIDs: []string{"doc1"}, Outcome: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	h := newTestHarness(t, Options{ConfigPath: cfgPath})
	if h.m.inWizard {
		t.Fatal("expected dashboard mode when valid config exists")
	}

	// Resize small + light mode + back to dark mode must never panic.
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24}, false)
	h.send(tea.BackgroundColorMsg{}, false)
	h.send(tea.WindowSizeMsg{Width: 120, Height: 36}, false)

	// Tab 1: Collections — verify refusal when removing the sole collection.
	h.press("d")
	if !strings.Contains(h.view(), "requires at least one collection") {
		t.Fatalf("expected sole-collection guard in view:\n%s", h.view())
	}

	// Prune orphan collection "oldcol".
	if !strings.Contains(h.view(), "oldcol") {
		t.Fatalf("expected orphan warning for oldcol:\n%s", h.view())
	}
	h.press("p", "y")
	orphans, err := h.m.st.OrphanCollections(t.Context(), h.m.dash.cfg.CollectionNames())
	if err != nil || len(orphans) != 0 {
		t.Fatalf("orphans after prune = %v (err=%v), want none", orphans, err)
	}

	// Add second collection "hr".
	h.press("a")
	h.clearAndType(col2Dir)
	h.submit("ctrl+s")
	if len(h.m.dash.cfg.Collections) != 2 {
		t.Fatalf("collections = %+v, want 2", h.m.dash.cfg.Collections)
	}
	if !h.m.dash.restartNeeded {
		t.Error("expected restartNeeded banner after modifying roost.toml")
	}

	// Remove "hr" (cursor is on "hr") and also prune DB.
	h.press("d", "space", "y")
	if len(h.m.dash.cfg.Collections) != 1 || h.m.dash.cfg.Collections[0].Name != "legal" {
		t.Fatalf("collections after delete = %+v, want [legal]", h.m.dash.cfg.Collections)
	}

	// Tab 2: Tokens — create scoped token and revoke seeded token.
	h.press("2")
	if !strings.Contains(h.view(), "existing") {
		t.Fatalf("missing seeded token:\n%s", h.view())
	}
	h.press("n")
	h.clearAndType("scoped-bot")
	h.press("tab")    // All toggle
	h.press("space")  // Disable * so individual collections appear
	h.press("tab")    // "legal" checkbox (checked by default)
	h.press("tab")    // TTL row
	h.press("right")  // 24h preset
	h.press("tab")    // Create button
	h.submit("enter") // Create!
	if h.m.dash.tokModal == nil || h.m.dash.tokModal.secret == "" {
		t.Fatal("expected token modal to remain open showing one-time secret")
	}
	createdSecret := h.m.dash.tokModal.secret
	p, err := auth.Verify(t.Context(), h.m.st, createdSecret)
	if err != nil {
		t.Fatalf("Verify scoped token: %v", err)
	}
	if len(p.Collections) != 1 || p.Collections[0] != "legal" {
		t.Fatalf("token collections = %v, want [legal]", p.Collections)
	}
	h.submit("enter") // Close secret modal
	if h.m.dash.tokModal != nil {
		t.Fatal("expected token modal to close on Enter")
	}

	// Revoke first token ("existing") and verify immediate invalidation (selection index depends on random ID sort when created in same second).
	for i := 0; i < len(h.m.dash.tokens) && h.m.dash.tokens[h.m.dash.tokCursor].ID != seededTok.ID; i++ {
		h.press("down")
	}
	if h.m.dash.tokens[h.m.dash.tokCursor].ID != seededTok.ID {
		t.Fatalf("could not select seeded token %s", seededTok.ID)
	}
	h.press("r", "y")
	if _, err := auth.Verify(t.Context(), h.m.st, seededSecret); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("Verify revoked token = %v, want auth.ErrInvalid", err)
	}
	if _, err := auth.Verify(t.Context(), h.m.st, createdSecret); err != nil {
		t.Fatalf("Verify active scoped token = %v, want nil", err)
	}
	toks, err := h.m.st.ListTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	foundRevoked := false
	for _, tk := range toks {
		if tk.ID == seededTok.ID {
			foundRevoked = tk.RevokedAt != nil
		}
	}
	if !foundRevoked {
		t.Fatalf("expected token %s to be marked revoked in SQLite: %+v", seededTok.ID, toks)
	}

	// Tab 3: Status & Problems.
	h.press("3")
	if !strings.Contains(h.view(), "legal/scan.pdf") || !strings.Contains(h.view(), "scanned image only") {
		t.Fatalf("expected problem document on Status tab:\n%s", h.view())
	}

	// Tab 4: Audit & filter.
	h.press("4")
	if !strings.Contains(h.view(), "vacation policy") {
		t.Fatalf("expected seeded audit entry on Audit tab:\n%s", h.view())
	}
	h.press("/")
	h.typeText("nomatchxyz")
	if strings.Contains(h.view(), "vacation policy") {
		t.Fatal("expected audit filter to hide non-matching entry")
	}
	h.press("esc")

	// Tab 5: Settings — edit server.listen and toggle index.watch, preview, save.
	h.press("5")
	h.press("enter") // edit server.listen (row 0)
	h.clearAndType("127.0.0.1:9494")
	h.press("enter")
	for range 6 {
		h.press("down") // index.watch (row 6)
	}
	h.press("space") // toggle watch=false
	h.press("v")     // preview TOML
	if len(h.m.dash.tomlPreview) == 0 || !strings.Contains(h.view(), "127.0.0.1:9494") {
		t.Fatalf("expected rendered TOML preview:\n%s", h.view())
	}
	h.press("esc") // close preview
	h.press("s")   // save config

	updatedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load updated config: %v", err)
	}
	if updatedCfg.Server.Listen != "127.0.0.1:9494" || updatedCfg.Index.Watch {
		t.Fatalf("updatedCfg = %+v, want 127.0.0.1:9494 and Watch=false", updatedCfg)
	}

	// Change storage.data_dir in Settings and verify store reopens cleanly.
	newDataDir := filepath.Join(base, "state-v2")
	h.press("up", "up", "enter") // jump from index.watch (row 6) -> storage.data_dir (row 4)
	h.clearAndType(newDataDir)
	h.press("enter")
	h.submit("s")
	if _, err := os.Stat(filepath.Join(newDataDir, "roost.db")); err != nil {
		t.Fatalf("expected roost.db created in new data_dir: %v", err)
	}
}

func TestForceWizardOnExistingAndInvalidConfig(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	docsDir := filepath.Join(base, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Broken config: normal launch must error with helpful hint; ForceWizard launches wizard.
	if err := os.WriteFile(cfgPath, []byte("unknown_bad_key = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newAppModel(t.Context(), Options{ConfigPath: cfgPath}); err == nil || !strings.Contains(err.Error(), "--wizard") {
		t.Fatalf("expected load error mentioning --wizard, got %v", err)
	}
	h := newTestHarness(t, Options{ConfigPath: cfgPath, ForceWizard: true})
	if !h.m.inWizard {
		t.Fatal("expected wizard mode with ForceWizard=true")
	}
	if !strings.Contains(h.view(), "already exists") {
		t.Fatalf("expected overwrite warning when ForceWizard=true on existing file:\n%s", h.view())
	}
}

func TestStatusProblemDetailFiltersAndCollectionReindex(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	legalDir := filepath.Join(base, "legal")
	hrDir := filepath.Join(base, "hr")
	for _, d := range []string{legalDir, hrDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dataDir
	cfg.Collections = []config.Collection{
		{Name: "legal", Path: legalDir},
		{Name: "hr", Path: hrDir},
	}
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	longRel := "nested/archive/2026/scanned_contract_with_extremely_long_filename_for_wrap.pdf"
	longErr := "Corrupt PDF cross-reference table at offset 4192831 with missing root catalog object"
	for _, doc := range []store.Document{
		{
			ID: "prob-legal", Collection: "legal", RelPath: longRel, FileType: "pdf",
			Size: 1024, Status: store.StatusNoText, Error: "scanned image only",
			SHA256: "aaaa", IndexedAt: time.Now().Unix(),
		},
		{
			ID: "prob-hr-fail", Collection: "hr", RelPath: "payroll.xlsx", Title: "Payroll Q1",
			FileType: "xlsx", Size: 20480, Status: store.StatusFailed, Truncated: true,
			Error: longErr, SHA256: "bbbb", IndexedAt: time.Now().Unix(),
			Meta: map[string]string{"sheet": "Q1"},
		},
		{
			ID: "prob-hr-enc", Collection: "hr", RelPath: "secret.pdf", FileType: "pdf",
			Size: 512, Status: store.StatusEncrypted, Error: "password protected",
			SHA256: "cccc", IndexedAt: time.Now().Unix(),
		},
	} {
		if err := st.ReplaceDocument(t.Context(), doc, nil); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	var indexedRuns [][]string
	h := newTestHarness(t, Options{
		ConfigPath: cfgPath,
		Index: func(_ context.Context, icfg *config.Config, _ *store.Store) (map[string]indexer.Stats, error) {
			var cols []string
			out := map[string]indexer.Stats{}
			for _, c := range icfg.Collections {
				cols = append(cols, c.Name)
				out[c.Name] = indexer.Stats{Seen: 1, Indexed: 1}
			}
			indexedRuns = append(indexedRuns, cols)
			return out, nil
		},
	})

	// Resize small so list rows definitely truncate long path/error while modal shows full wrapped text.
	h.send(tea.WindowSizeMsg{Width: 78, Height: 22}, false)
	h.press("3") // Tab 3 Status

	// Cycle status filter ('s'): first preset after "all" is "failed".
	h.press("s")
	v := h.view()
	if !strings.Contains(v, "status: failed") || !strings.Contains(v, "payroll.xlsx") || strings.Contains(v, "secret.pdf") {
		t.Fatalf("expected status filter 'failed' to isolate payroll.xlsx:\n%s", v)
	}
	h.press("esc") // Reset filter back to all

	// Live '/' search across paths/errors.
	h.press("/")
	h.typeText("password protected")
	v = h.view()
	if !strings.Contains(v, "secret.pdf") || strings.Contains(v, "payroll.xlsx") {
		t.Fatalf("expected search filter to isolate encrypted doc:\n%s", v)
	}
	h.press("esc")

	// Move cursor to "hr/payroll.xlsx" and open Problem File Detail modal via Enter.
	for i := 0; i < len(h.m.dash.filteredProblems) && h.m.dash.filteredProblems[h.m.dash.probCursor].ID != "prob-hr-fail"; i++ {
		h.press("down")
	}
	if h.m.dash.filteredProblems[h.m.dash.probCursor].ID != "prob-hr-fail" {
		t.Fatalf("failed to select prob-hr-fail")
	}
	h.press("enter")
	if h.m.dash.probDetail == nil {
		t.Fatal("expected problem detail modal to open on Enter")
	}
	modalView := h.view()
	for _, needle := range []string{"Problem File Detail", "hr/payroll.xlsx", "20.0 KiB"} {
		if !strings.Contains(modalView, needle) {
			t.Fatalf("expected %q on first page of problem detail modal:\n%s", needle, modalView)
		}
	}
	wantDiskPath := filepath.Join(hrDir, "payroll.xlsx")
	// Page down inside the narrow modal to read metadata, wrapped full error, and remediation hint.
	h.press("pgdown")
	page2 := h.view()
	for _, needle := range []string{"sheet=Q1", "4192831", "max_text_mb"} {
		if !strings.Contains(page2, needle) {
			t.Fatalf("expected %q on scrolled page 2 of problem detail modal:\n%s", needle, page2)
		}
	}
	// Resize wider so disk path fits unwrapped cleanly too.
	h.send(tea.WindowSizeMsg{Width: 120, Height: 36}, false)
	if wideView := h.view(); !strings.Contains(wideView, wantDiskPath) {
		t.Fatalf("expected unwrapped disk path %q at 120 cols:\n%s", wantDiskPath, wideView)
	}
	h.press("pgup", "esc")
	if h.m.dash.probDetail != nil {
		t.Fatal("expected Esc to close problem detail modal")
	}

	// Re-open modal and trigger single-collection re-index ('c') on "hr" end-to-end.
	h.press("enter")
	h.submit("c")
	if h.m.dash.indexing || h.m.dash.probDetail != nil {
		t.Fatalf("expected 'c' index pass to finish and modal closed (indexing=%v)", h.m.dash.indexing)
	}
	if len(indexedRuns) != 1 || len(indexedRuns[0]) != 1 || indexedRuns[0][0] != "hr" {
		t.Fatalf("collection-scoped re-index ran %v, want [[hr]]", indexedRuns)
	}
}

func TestAuditDetailDocResolutionFiltersAndLiveFollow(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	legalDir := filepath.Join(base, "legal")
	if err := os.MkdirAll(legalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dataDir
	cfg.Collections = []config.Collection{{Name: "legal", Path: legalDir}}
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc-ok", Collection: "legal", RelPath: "handbook.pdf", FileType: "pdf",
		Status: store.StatusOK,
	}, []store.Chunk{{Section: "1", Text: "policy"}}); err != nil {
		t.Fatal(err)
	}
	_, tok, err := auth.Create(t.Context(), st, "auditor", []string{auth.AllCollections}, 0)
	if err != nil {
		t.Fatal(err)
	}
	longQuery := "confidential merger indemnity schedule clause seventeen appendix"
	if err := st.InsertAudit(t.Context(), store.AuditEntry{
		Time: time.Now().Add(-2 * time.Minute), TokenID: tok.ID, TokenName: "auditor",
		Transport: "mcp-http", Remote: "10.0.0.5", Op: "search",
		Query: longQuery, DocIDs: []string{"doc-ok", "doc-gone"}, Outcome: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAudit(t.Context(), store.AuditEntry{
		Time: time.Now().Add(-1 * time.Minute), TokenID: tok.ID, TokenName: "auditor",
		Transport: "rest", Remote: "10.0.0.6", Op: "get_document_info",
		DocIDs: []string{"doc-gone"}, Outcome: "not_found",
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	h := newTestHarness(t, Options{ConfigPath: cfgPath})
	h.press("4") // Tab 4 Audit

	// Outcome filter ('o'): all -> ok -> non-ok -> all.
	h.press("o") // ok
	v := h.view()
	if !strings.Contains(v, "outcome: ok") || !strings.Contains(v, "search") || strings.Contains(v, "not_found") {
		t.Fatalf("expected outcome=ok filter:\n%s", v)
	}
	h.press("o") // non-ok
	v = h.view()
	if !strings.Contains(v, "outcome: non-ok") || !strings.Contains(v, "not_found") || strings.Contains(v, "mcp-http") {
		t.Fatalf("expected outcome=non-ok filter:\n%s", v)
	}
	h.press("esc")

	// '/' filter matches by DocID too.
	h.press("/")
	h.typeText("doc-ok")
	v = h.view()
	if !strings.Contains(v, "mcp-http") || strings.Contains(v, "get_document_info") {
		t.Fatalf("expected DocID filter to isolate search row:\n%s", v)
	}
	// Open detail modal while filtered (first close filter input with Enter).
	h.press("enter", "enter")
	if h.m.dash.auditDetail == nil {
		t.Fatal("expected Enter to open audit detail modal")
	}
	modalView := h.view()
	for _, needle := range []string{"Audit Entry Detail", longQuery, "legal/handbook.pdf", "doc-gone"} {
		if !strings.Contains(modalView, needle) {
			t.Fatalf("expected %q in audit detail modal:\n%s", needle, modalView)
		}
	}
	h.press("esc", "esc") // close modal, clear text filter

	// Toggle Follow mode ('f') on Audit tab.
	h.press("f")
	if !h.m.dash.auditFollow || !strings.Contains(h.view(), "FOLLOWING live") {
		t.Fatalf("expected follow mode active:\n%s", h.view())
	}
	seq := h.m.dash.auditTickSeq

	// Insert a live audit entry into SQLite and deliver auditTickMsg.
	if err := h.m.st.InsertAudit(t.Context(), store.AuditEntry{
		Time: time.Now(), TokenID: tok.ID, TokenName: "auditor",
		Transport: "mcp-stdio", Op: "search", Query: "live incoming query", Outcome: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	_, cmd := h.m.dash.Update(auditTickMsg{seq: seq})
	if cmd == nil {
		t.Fatal("expected active auditTickMsg to reschedule next tick")
	}
	// Check directly via harness so h.m stays updated cleanly:
	h.send(auditTickMsg{seq: seq}, false)
	if !strings.Contains(h.view(), "live incoming query") {
		t.Fatalf("expected live follow tick to display newly inserted audit entry:\n%s", h.view())
	}

	// Scroll cursor down onto older "get_document_info" row and verify live ticks preserve selection.
	h.press("down")
	selectedOp := h.m.dash.filteredAudit[h.m.dash.auditCursor].Op
	if selectedOp != "get_document_info" {
		t.Fatalf("selectedOp = %q, want get_document_info", selectedOp)
	}
	if err := h.m.st.InsertAudit(t.Context(), store.AuditEntry{
		Time: time.Now().Add(time.Second), TokenID: tok.ID, TokenName: "auditor",
		Transport: "rest", Op: "list_documents", Outcome: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	h.send(auditTickMsg{seq: h.m.dash.auditTickSeq}, false)
	if gotOp := h.m.dash.filteredAudit[h.m.dash.auditCursor].Op; gotOp != "get_document_info" {
		t.Fatalf("cursor jumped after live refresh to %q, want get_document_info", gotOp)
	}

	// Switching away invalidates tick chain; switching back re-arms follow.
	h.press("1")
	_, offTabCmd := h.m.dash.Update(auditTickMsg{seq: seq})
	if offTabCmd != nil {
		t.Fatal("expected audit tick on another tab not to reschedule")
	}
	_, retCmd := h.m.Update(keyPress("4"))
	if retCmd == nil {
		t.Fatal("expected switching back to Audit tab while follow is enabled to re-arm tick cmd")
	}
	h.press("4") // ensure harness pointer is on Audit tab
	h.press("f") // turn follow off
	if h.m.dash.auditFollow {
		t.Fatal("expected second 'f' press to pause follow mode")
	}
	if cmd != nil && !h.m.dash.auditFollow {
		_, pausedCmd := h.m.dash.Update(auditTickMsg{seq: seq})
		if pausedCmd != nil {
			t.Fatal("expected stale auditTickMsg after pausing follow not to reschedule")
		}
	}
}

func TestExploreSearchAclReaderAndAuditTrail(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	legalDir := filepath.Join(base, "legal")
	hrDir := filepath.Join(base, "hr")
	for _, d := range []string{legalDir, hrDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dataDir
	cfg.Collections = []config.Collection{
		{Name: "legal", Path: legalDir},
		{Name: "hr", Path: hrDir},
	}
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc-legal", Collection: "legal", RelPath: "msa.pdf", Title: "Master Services Agreement",
		FileType: "pdf", Size: 4096, MTimeNS: time.Now().UnixNano(), Status: store.StatusOK,
	}, []store.Chunk{
		{Section: "Page 1", Text: "General agreement recitals and party definitions."},
		{Section: "Page 2", Text: "The supplier indemnity cap liability clause applies cleanly here."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc-hr", Collection: "hr", RelPath: "salaries.xlsx", Title: "Compensation Plan",
		FileType: "xlsx", Size: 8192, MTimeNS: time.Now().UnixNano(), Status: store.StatusOK,
	}, []store.Chunk{
		{Section: "Sheet 1", Text: "Executive indemnity cap schedule for 2026."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "doc-notext", Collection: "legal", RelPath: "scan.pdf", Title: "scan.pdf",
		FileType: "pdf", Size: 2048, MTimeNS: time.Now().UnixNano(), Status: store.StatusNoText,
		Error: "scanned image only",
	}, nil); err != nil {
		t.Fatal(err)
	}

	if _, _, err := auth.Create(t.Context(), st, "legal-bot", []string{"legal"}, 0); err != nil {
		t.Fatal(err)
	}
	_, revokedTok, err := auth.Create(t.Context(), st, "revoked-bot", []string{auth.AllCollections}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeToken(t.Context(), revokedTok.ID); err != nil {
		t.Fatal(err)
	}
	st.Close()

	h := newTestHarness(t, Options{ConfigPath: cfgPath, InitialTab: "explore"})
	if h.m.dash.tab != tabExplore {
		t.Fatalf("tab = %v, want tabExplore", h.m.dash.tab)
	}
	v := h.view()
	for _, needle := range []string{"msa.pdf", "salaries.xlsx", "scan.pdf"} {
		if !strings.Contains(v, needle) {
			t.Fatalf("expected browse view to include %q:\n%s", needle, v)
		}
	}

	// Search with '/' -> "indemnity cap" + Enter.
	h.press("/")
	h.typeText("indemnity cap")
	h.press("enter")
	v = h.view()
	for _, needle := range []string{"mode: all_terms", "msa.pdf", "salaries.xlsx", "Hit Preview"} {
		if !strings.Contains(v, needle) {
			t.Fatalf("expected search view to include %q:\n%s", needle, v)
		}
	}
	if strings.Contains(v, "**indemnity**") {
		t.Fatalf("expected FTS ** markers to be stripped/styled cleanly:\n%s", v)
	}

	// Select legal/msa.pdf hit and press Enter to open Document Reader on matching Page 2.
	for i := 0; i < len(h.m.dash.expl.hits) && h.m.dash.expl.hits[h.m.dash.expl.cursor].DocID != "doc-legal"; i++ {
		h.press("down")
	}
	if h.m.dash.expl.hits[h.m.dash.expl.cursor].DocID != "doc-legal" {
		t.Fatal("failed to locate doc-legal hit")
	}
	h.press("enter")
	if h.m.dash.expl.docReader == nil {
		t.Fatal("expected Enter on hit to open Document Reader")
	}
	rv := h.view()
	if !strings.Contains(rv, "[2/2] Page 2") || !strings.Contains(rv, "indemnity cap liability clause") {
		t.Fatalf("expected reader to jump straight to Page 2:\n%s", rv)
	}
	// Step backward in Outline sections ('left') to Page 1.
	h.press("left")
	rv = h.view()
	if !strings.Contains(rv, "[1/2] Page 1") || !strings.Contains(rv, "General agreement recitals") {
		t.Fatalf("expected left key in reader to switch to Page 1:\n%s", rv)
	}
	h.press("esc")

	// Browse back ('b'), open Reader on no_text document, confirm clear message.
	h.press("b")
	for i := 0; i < len(h.m.dash.expl.docs) && h.m.dash.expl.docs[h.m.dash.expl.cursor].DocID != "doc-notext"; i++ {
		h.press("down")
	}
	h.press("enter")
	if !strings.Contains(h.view(), "No extracted text stored") || !strings.Contains(h.view(), "scanned image only") {
		t.Fatalf("expected clear empty-text explanation on no_text doc:\n%s", h.view())
	}
	h.press("esc")

	// Search again, then cycle token ACL preview ('t') -> legal-bot must hide hr/salaries.xlsx!
	h.press("/")
	h.typeText("indemnity")
	h.press("enter", "t")
	v = h.view()
	if !strings.Contains(v, "token: legal-bot -> [legal]") || !strings.Contains(v, "msa.pdf") || strings.Contains(v, "salaries.xlsx") {
		t.Fatalf("expected legal-bot ACL preview to keep msa.pdf and hide salaries.xlsx:\n%s", v)
	}
	if strings.Contains(v, "revoked-bot") {
		t.Fatalf("revoked token should never appear in preview cycle:\n%s", v)
	}

	// Switch to Tab 4 Audit -> tui-explore events must appear in live SQLite audit log!
	h.press("4")
	av := h.view()
	if !strings.Contains(av, "tui-expl") || !strings.Contains(av, "legal-bot") || !strings.Contains(av, "indemnity") {
		t.Fatalf("expected tui-explore audit rows on Audit tab:\n%s", av)
	}
}

func TestExploreVectorFallbackPaginationRequireConfigAndWizardShortcut(t *testing.T) {
	base := t.TempDir()
	missingCfg := filepath.Join(base, "missing.toml")
	if _, err := newAppModel(t.Context(), Options{ConfigPath: missingCfg, RequireConfig: true}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected clear not-found error with RequireConfig=true, got %v", err)
	}

	cfgPath := filepath.Join(base, "roost.toml")
	dataDir := filepath.Join(base, "state")
	docsDir := filepath.Join(base, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dataDir
	cfg.Collections = []config.Collection{{Name: "docs", Path: docsDir}}
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Endpoint = "http://127.0.0.1:11434/v1/embeddings"
	cfg.Embeddings.Model = "nomic-embed-text"
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// Seed 25 docs for browse pagination + 1 long doc spanning >16k chars for reader window pagination.
	for i := range 25 {
		if err := st.ReplaceDocument(t.Context(), store.Document{
			ID: fmt.Sprintf("d-%02d", i), Collection: "docs", RelPath: fmt.Sprintf("file-%02d.txt", i),
			FileType: "txt", Size: 100, MTimeNS: time.Now().UnixNano(), Status: store.StatusOK,
		}, []store.Chunk{{Section: "Body", Text: fmt.Sprintf("document %02d keyword alpha", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	longPart1 := strings.Repeat("alpha pageone ", 1300) // ~18,200 chars > exploreReadWindow
	if err := st.ReplaceDocument(t.Context(), store.Document{
		ID: "d-long", Collection: "docs", RelPath: "z-long.txt",
		FileType: "txt", Size: 20000, MTimeNS: time.Now().UnixNano(), Status: store.StatusOK,
	}, []store.Chunk{{Section: "Body", Text: longPart1 + "TAILMARKER"}}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	// VectorFunc fails -> Explore falls back to keyword search and surfaces warning banner.
	h := newTestHarness(t, Options{
		ConfigPath: cfgPath,
		InitialTab: "explore",
		NewVectors: func(_ *config.Config, _ *store.Store) (core.VectorSearcher, error) {
			return nil, errors.New("embeddings: OPENAI_API_KEY is not set")
		},
	})
	v := h.view()
	if !strings.Contains(v, "OPENAI_API_KEY is not set") || !strings.Contains(v, "file-00.txt") {
		t.Fatalf("expected vector fallback warning + page 1 docs:\n%s", v)
	}

	// Next page ']' shows later files ("file-20.txt" / "z-long.txt"), '[' returns to page 1.
	h.press("]")
	if !strings.Contains(h.view(), "z-long.txt") {
		t.Fatalf("expected page 2 to show z-long.txt:\n%s", h.view())
	}
	for i := 0; i < len(h.m.dash.expl.docs) && h.m.dash.expl.docs[h.m.dash.expl.cursor].DocID != "d-long"; i++ {
		h.press("down")
	}
	h.press("enter")
	if h.m.dash.expl.docReader == nil || h.m.dash.expl.docReader.resp.NextOffset == 0 {
		t.Fatalf("expected reader NextOffset > 0 on long document")
	}
	if strings.Contains(h.view(), "TAILMARKER") {
		t.Fatal("TAILMARKER should not fit in first 16k reader window")
	}
	h.press("n", "pgdown") // advance to next 16k window and scroll to bottom
	if !strings.Contains(h.view(), "TAILMARKER") {
		t.Fatalf("expected TAILMARKER on second reader window:\n%s", h.view())
	}
	h.press("p") // step back to offset 0
	if h.m.dash.expl.docReader.offset != 0 {
		t.Fatalf("offset after 'p' = %d, want 0", h.m.dash.expl.docReader.offset)
	}
	h.press("esc", "[")
	if !strings.Contains(h.view(), "file-00.txt") {
		t.Fatalf("expected '[' to return to page 1:\n%s", h.view())
	}

	// Verify wizard Finish 'e' shortcut opens Explore tab directly.
	wizBase := t.TempDir()
	wizCfg := filepath.Join(wizBase, "roost.toml")
	wizData := filepath.Join(wizBase, "data")
	wizDocs := filepath.Join(wizBase, "docs")
	if err := os.MkdirAll(wizDocs, 0o755); err != nil {
		t.Fatal(err)
	}
	wh := newTestHarness(t, Options{ConfigPath: wizCfg})
	wh.press("enter")
	wh.clearAndType(wizData)
	wh.press("enter", "a")
	wh.clearAndType(wizDocs)
	wh.submit("ctrl+s")
	wh.press("n", "enter", "down", "enter") // server -> embeddings -> review
	wh.submit("enter")                      // save config & advance to finish
	if wh.m.wizard.step != wizFinish {
		t.Fatalf("wizard step = %v, want wizFinish (err=%q)", wh.m.wizard.step, wh.m.wizard.errMsg)
	}
	wh.submit("e") // shortcut to Explore tab
	if wh.m.inWizard || wh.m.dash.tab != tabExplore {
		t.Fatalf("expected 'e' on wizard Finish to jump to tabExplore (inWizard=%v tab=%v)", wh.m.inWizard, wh.m.dash.tab)
	}
}
