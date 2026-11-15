package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
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
