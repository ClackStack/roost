package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/core"
	"github.com/ClackStack/roost/internal/store"
)

// VectorFunc constructs an optional vector searcher when embeddings are enabled.
type VectorFunc func(cfg *config.Config, st *store.Store) (core.VectorSearcher, error)

type exploreMode int

const (
	exploreBrowse exploreMode = iota
	exploreSearch
)

const (
	explorePageSize   = 20
	exploreReadWindow = 16000
)

type docReaderState struct {
	info       *core.DocumentInfo
	sectionIdx int // 0 = all sections; >0 = info.Outline[sectionIdx-1]
	offset     int
	prevOffs   []int
	resp       *core.ReadResponse
	lines      []string
	scroll     int
}

type exploreModel struct {
	ctx    context.Context
	cfg    *config.Config
	store  *store.Store
	vecFn  VectorFunc
	svc    *core.Service
	styles styles
	width  int
	height int

	colNames []string
	colIdx   int // 0 = all allowed; >0 = colNames[colIdx-1]

	tokens   []store.TokenRecord
	tokenIdx int // 0 = * all configured; >0 = tokens[tokenIdx-1]

	queryIn      textinput.Model
	queryEditing bool
	lastQuery    string
	mode         exploreMode

	// Browse results
	docs        []core.DocumentSummary
	cursorStack []string
	currCursor  string
	nextCursor  string

	// Search results
	hits         []core.SearchHit
	searchOffset int
	hasMore      bool
	searchMode   string
	searchWarn   string
	vecWarn      string

	cursor int
	scroll int

	docReader *docReaderState
	errMsg    string
}

func newExploreModel(ctx context.Context, cfg *config.Config, st *store.Store, vecFn VectorFunc, stStyles styles, width, height int) exploreModel {
	m := exploreModel{
		ctx:     ctx,
		cfg:     cfg.Clone(),
		store:   st,
		vecFn:   vecFn,
		styles:  stStyles,
		width:   width,
		height:  height,
		queryIn: newTextInput(`Search terms, "exact phrase", or prefix* (Enter search, empty browse)`, "", max(24, width-18), stStyles.isDark),
	}
	m.sync(cfg, st, false)
	return m
}

func (m *exploreModel) setSize(width, height int, st styles) {
	m.width = width
	m.height = height
	m.styles = st
	m.queryIn.SetWidth(max(24, width-18))
	m.queryIn.SetStyles(textinput.DefaultStyles(st.isDark))
	if m.docReader != nil {
		m.wrapReaderLines()
	}
}

func (m *exploreModel) sync(cfg *config.Config, st *store.Store, refreshResults bool) {
	if cfg != nil {
		m.cfg = cfg.Clone()
		m.colNames = m.cfg.CollectionNames()
		if m.colIdx > len(m.colNames) {
			m.colIdx = 0
		}
	}
	m.store = st
	m.refreshTokens()
	m.rebuildService()
	if refreshResults && m.svc != nil {
		m.runCurrent()
	}
}

func (m *exploreModel) refreshTokens() {
	if m.store == nil {
		m.tokens = nil
		m.tokenIdx = 0
		return
	}
	prevID := ""
	if m.tokenIdx > 0 && m.tokenIdx <= len(m.tokens) {
		prevID = m.tokens[m.tokenIdx-1].ID
	}
	all, err := m.store.ListTokens(m.ctx)
	if err != nil {
		m.tokens = nil
		m.tokenIdx = 0
		return
	}
	now := time.Now()
	var active []store.TokenRecord
	for _, t := range all {
		if t.RevokedAt != nil {
			continue
		}
		if t.ExpiresAt != nil && now.After(*t.ExpiresAt) {
			continue
		}
		active = append(active, t)
	}
	m.tokens = active
	m.tokenIdx = 0
	if prevID != "" {
		for i, t := range m.tokens {
			if t.ID == prevID {
				m.tokenIdx = i + 1
				break
			}
		}
	}
}

func (m *exploreModel) rebuildService() {
	m.svc = nil
	m.vecWarn = ""
	if m.store == nil || m.cfg == nil {
		return
	}
	var vs core.VectorSearcher
	if m.cfg.Embeddings.Enabled && m.vecFn != nil {
		var err error
		vs, err = m.vecFn(m.cfg, m.store)
		if err != nil {
			m.vecWarn = err.Error() + " (semantic search disabled in preview)"
			vs = nil
		}
	}
	// Never open or create the production JSONL audit file from an interactive TUI
	// session; SQLite audit rows are still recorded with transport="tui-explore".
	coreCfg := m.cfg.Clone()
	coreCfg.Audit.JSONLPath = ""
	discardLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := core.New(coreCfg, m.store, discardLog, vs)
	if err != nil {
		m.errMsg = err.Error()
		return
	}
	m.svc = svc
}

func (m exploreModel) caller() (core.Caller, string, []string) {
	if m.tokenIdx > 0 && m.tokenIdx <= len(m.tokens) {
		tok := m.tokens[m.tokenIdx-1]
		p := &auth.Principal{
			TokenID:     tok.ID,
			Name:        tok.Name,
			Collections: slices.Clone(tok.Collections),
		}
		allowed := p.Allowed(m.colNames)
		return core.Caller{Principal: p, Transport: "tui-explore", Remote: "local"}, tok.Name, allowed
	}
	p := &auth.Principal{
		TokenID:     "tui-explore",
		Name:        "tui-explore",
		Collections: []string{auth.AllCollections},
	}
	return core.Caller{Principal: p, Transport: "tui-explore", Remote: "local"}, "* (all)", slices.Clone(m.colNames)
}

func (m exploreModel) selectedCollections() []string {
	if m.colIdx > 0 && m.colIdx <= len(m.colNames) {
		return []string{m.colNames[m.colIdx-1]}
	}
	return nil
}

func (m *exploreModel) runCurrent() {
	if m.mode == exploreSearch && strings.TrimSpace(m.lastQuery) != "" {
		m.execSearch()
		return
	}
	m.mode = exploreBrowse
	m.execBrowse()
}

func (m *exploreModel) execBrowse() {
	m.errMsg = ""
	m.searchWarn = ""
	m.searchMode = ""
	m.hits = nil
	if m.svc == nil {
		m.docs = nil
		m.nextCursor = ""
		return
	}
	c, _, _ := m.caller()
	resp, err := m.svc.ListDocuments(m.ctx, c, core.ListRequest{
		Collections: m.selectedCollections(),
		Limit:       explorePageSize,
		Cursor:      m.currCursor,
	})
	if err != nil {
		m.errMsg = err.Error()
		m.docs = nil
		m.nextCursor = ""
		return
	}
	m.docs = resp.Documents
	m.nextCursor = resp.NextCursor
	m.clampCursor()
}

func (m *exploreModel) execSearch() {
	m.errMsg = ""
	m.searchWarn = ""
	m.docs = nil
	if m.svc == nil {
		m.hits = nil
		m.hasMore = false
		return
	}
	c, _, _ := m.caller()
	resp, err := m.svc.Search(m.ctx, c, core.SearchRequest{
		Query:       m.lastQuery,
		Collections: m.selectedCollections(),
		Limit:       explorePageSize,
		Offset:      m.searchOffset,
	})
	if err != nil {
		m.errMsg = err.Error()
		m.hits = nil
		m.hasMore = false
		m.searchMode = ""
		return
	}
	m.hits = resp.Results
	m.hasMore = resp.HasMore
	m.searchMode = resp.Mode
	m.searchWarn = resp.Warning
	m.clampCursor()
}

func (m *exploreModel) itemCount() int {
	if m.mode == exploreSearch {
		return len(m.hits)
	}
	return len(m.docs)
}

func (m *exploreModel) clampCursor() {
	n := m.itemCount()
	if m.cursor >= n {
		m.cursor = max(0, n-1)
	}
	if m.scroll > m.cursor {
		m.scroll = m.cursor
	}
}

func (m exploreModel) listRows() int {
	return max(4, m.height-21)
}

func (m exploreModel) readerRows() int {
	return max(6, m.height-14)
}

func (m exploreModel) modalWrapWidth() int {
	outer := max(48, min(108, m.width-6))
	return max(28, outer-6)
}

func (m *exploreModel) openReader() {
	if m.svc == nil || m.itemCount() == 0 {
		return
	}
	var docID, hitSection string
	if m.mode == exploreSearch {
		h := m.hits[m.cursor]
		docID = h.DocID
		hitSection = h.Section
	} else {
		docID = m.docs[m.cursor].DocID
	}

	c, _, _ := m.caller()
	info, err := m.svc.GetDocumentInfo(m.ctx, c, docID)
	if err != nil {
		m.errMsg = err.Error()
		return
	}
	secIdx := 0
	if hitSection != "" {
		for i, entry := range info.Outline {
			if entry.Section == hitSection {
				secIdx = i + 1
				break
			}
		}
	}
	m.docReader = &docReaderState{
		info:       info,
		sectionIdx: secIdx,
	}
	m.loadReaderSection()
}

func (m *exploreModel) loadReaderSection() {
	if m.docReader == nil || m.svc == nil {
		return
	}
	dr := m.docReader
	section := ""
	if dr.sectionIdx > 0 && dr.sectionIdx <= len(dr.info.Outline) {
		section = dr.info.Outline[dr.sectionIdx-1].Section
	}
	c, _, _ := m.caller()
	resp, err := m.svc.ReadDocument(m.ctx, c, core.ReadRequest{
		DocID:    dr.info.DocID,
		Section:  section,
		Offset:   dr.offset,
		MaxChars: exploreReadWindow,
	})
	if err != nil {
		m.errMsg = err.Error()
		dr.resp = nil
		dr.lines = wrapLines("Error reading document: "+err.Error(), m.modalWrapWidth())
		dr.scroll = 0
		return
	}
	m.errMsg = ""
	dr.resp = resp
	m.wrapReaderLines()
	dr.scroll = 0
}

func (m *exploreModel) wrapReaderLines() {
	dr := m.docReader
	if dr == nil || dr.resp == nil {
		return
	}
	w := m.modalWrapWidth()
	text := strings.TrimSpace(dr.resp.Text)
	if text == "" {
		msg := fmt.Sprintf("(No extracted text stored — document status: %s", dr.info.Status)
		if dr.info.Error != "" {
			msg += ": " + dr.info.Error
		}
		msg += ")"
		dr.lines = wrapLines(msg, w)
		return
	}
	dr.lines = wrapLines(text, w)
}

func (m exploreModel) hasOverlay() bool {
	return m.queryEditing || m.docReader != nil
}

func (m exploreModel) Update(msg tea.Msg) (exploreModel, tea.Cmd) {
	if m.docReader != nil {
		return m.updateReader(msg)
	}
	if m.queryEditing {
		return m.updateQueryInput(msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	rows := m.listRows()
	if moveListCursor(&m.cursor, &m.scroll, m.itemCount(), rows, key.String()) {
		return m, nil
	}

	switch key.String() {
	case "/", "s":
		m.queryEditing = true
		m.queryIn.CursorEnd()
		return m, m.queryIn.Focus()
	case "enter":
		m.openReader()
	case "t":
		if len(m.tokens) > 0 {
			m.tokenIdx = (m.tokenIdx + 1) % (len(m.tokens) + 1)
			m.resetPaging()
			m.runCurrent()
		}
	case "c":
		if len(m.colNames) > 0 {
			m.colIdx = (m.colIdx + 1) % (len(m.colNames) + 1)
			m.resetPaging()
			m.runCurrent()
		}
	case "b", "esc":
		if m.mode == exploreSearch || m.queryIn.Value() != "" {
			m.queryIn.SetValue("")
			m.lastQuery = ""
			m.mode = exploreBrowse
			m.resetPaging()
			m.execBrowse()
		}
	case "[":
		m.prevPage()
	case "]":
		m.nextPage()
	case "r", "R":
		m.sync(m.cfg, m.store, true)
	}
	return m, nil
}

func (m *exploreModel) resetPaging() {
	m.cursor = 0
	m.scroll = 0
	m.searchOffset = 0
	m.currCursor = ""
	m.cursorStack = nil
}

func (m *exploreModel) nextPage() {
	if m.mode == exploreSearch {
		if m.hasMore {
			m.searchOffset += explorePageSize
			m.cursor = 0
			m.scroll = 0
			m.execSearch()
		}
		return
	}
	if m.nextCursor != "" {
		m.cursorStack = append(m.cursorStack, m.currCursor)
		m.currCursor = m.nextCursor
		m.cursor = 0
		m.scroll = 0
		m.execBrowse()
	}
}

func (m *exploreModel) prevPage() {
	if m.mode == exploreSearch {
		if m.searchOffset > 0 {
			m.searchOffset = max(0, m.searchOffset-explorePageSize)
			m.cursor = 0
			m.scroll = 0
			m.execSearch()
		}
		return
	}
	if len(m.cursorStack) > 0 {
		last := m.cursorStack[len(m.cursorStack)-1]
		m.cursorStack = m.cursorStack[:len(m.cursorStack)-1]
		m.currCursor = last
		m.cursor = 0
		m.scroll = 0
		m.execBrowse()
	}
}

func (m exploreModel) updateQueryInput(msg tea.Msg) (exploreModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.queryIn, cmd = m.queryIn.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "enter":
		m.queryEditing = false
		m.queryIn.Blur()
		q := strings.TrimSpace(m.queryIn.Value())
		m.lastQuery = q
		m.resetPaging()
		if q == "" {
			m.mode = exploreBrowse
			m.execBrowse()
		} else {
			m.mode = exploreSearch
			m.execSearch()
		}
		return m, nil
	case "esc":
		m.queryEditing = false
		m.queryIn.Blur()
		m.queryIn.SetValue(m.lastQuery)
		return m, nil
	}
	var cmd tea.Cmd
	m.queryIn, cmd = m.queryIn.Update(msg)
	return m, cmd
}

func (m exploreModel) updateReader(msg tea.Msg) (exploreModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	dr := m.docReader
	rows := m.readerRows()
	if scrollLinesOffset(&dr.scroll, len(dr.lines), rows, key.String()) {
		return m, nil
	}
	totalSecs := len(dr.info.Outline) + 1
	switch key.String() {
	case "esc", "enter":
		m.docReader = nil
	case "left", "[":
		if totalSecs > 1 {
			dr.sectionIdx = (dr.sectionIdx + totalSecs - 1) % totalSecs
			dr.offset = 0
			dr.prevOffs = nil
			m.loadReaderSection()
		}
	case "right", "]":
		if totalSecs > 1 {
			dr.sectionIdx = (dr.sectionIdx + 1) % totalSecs
			dr.offset = 0
			dr.prevOffs = nil
			m.loadReaderSection()
		}
	case "n", ">":
		if dr.resp != nil && dr.resp.NextOffset > 0 {
			dr.prevOffs = append(dr.prevOffs, dr.offset)
			dr.offset = dr.resp.NextOffset
			m.loadReaderSection()
		}
	case "p", "<":
		if len(dr.prevOffs) > 0 {
			dr.offset = dr.prevOffs[len(dr.prevOffs)-1]
			dr.prevOffs = dr.prevOffs[:len(dr.prevOffs)-1]
			m.loadReaderSection()
		}
	}
	return m, nil
}

func (m exploreModel) View() string {
	if m.docReader != nil {
		return m.viewReaderModal()
	}

	var b strings.Builder
	_, scopeName, allowed := m.caller()
	b.WriteString(m.styles.label.Render("Search & Extraction Explorer"))
	b.WriteString("  " + m.styles.badge.Render(fmt.Sprintf("token: %s -> [%s]", scopeName, strings.Join(allowed, ","))))
	if m.colIdx > 0 && m.colIdx <= len(m.colNames) {
		b.WriteString("  " + m.styles.badge.Render("col: "+m.colNames[m.colIdx-1]))
	}
	if m.mode == exploreSearch && m.searchMode != "" {
		b.WriteString("  " + m.styles.badge.Render("mode: "+m.searchMode))
	}
	b.WriteString("\n")

	b.WriteString(m.styles.label.Render("Query: ") + m.queryIn.View() + "\n")
	if warn := firstNonEmpty(m.searchWarn, m.vecWarn); warn != "" {
		b.WriteString(m.styles.warn.Render("! "+truncateText(warn, max(36, m.width-10))) + "\n")
	} else {
		b.WriteString("\n")
	}

	panelW := max(40, m.width-8)
	if m.mode == exploreSearch {
		m.viewSearchResults(&b, panelW)
	} else {
		m.viewBrowseDocs(&b, panelW)
	}

	if m.queryEditing {
		b.WriteString("\n" + m.styles.help.Render("Enter search (or browse if empty)  •  Esc cancel"))
	} else {
		b.WriteString("\n" + m.styles.help.Render("↑/↓ move  •  Enter read document  •  / search  •  t token ACL  •  c collection  •  [/] page  •  b browse all"))
	}
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m exploreModel) viewSearchResults(b *strings.Builder, panelW int) {
	rows := m.listRows()
	if len(m.hits) == 0 {
		b.WriteString(m.styles.muted.Render(fmt.Sprintf("No search matches for %q.", m.lastQuery)) + "\n")
		b.WriteString("\n" + m.styles.muted.Render("Tip: press 'b' to browse all indexed files, 't' to check another token's ACL, or 'c' to clear collection filter.") + "\n")
		return
	}

	b.WriteString(m.styles.muted.Render(fmt.Sprintf("  %-12s  %-16s  %-7s  %s", "COLLECTION", "SECTION", "SCORE", "DOCUMENT")) + "\n")
	end := min(len(m.hits), m.scroll+rows)
	for i := m.scroll; i < end; i++ {
		h := m.hits[i]
		line := fmt.Sprintf("%-12s  %-16s  %7.3f  %s",
			truncateText(h.Collection, 12),
			truncateText(h.Section, 16),
			h.Score,
			h.Path,
		)
		line = truncateText(line, panelW)
		if i == m.cursor {
			b.WriteString(m.styles.selected.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	// Highlighted snippet preview for the selected hit.
	if m.cursor >= 0 && m.cursor < len(m.hits) {
		sel := m.hits[m.cursor]
		b.WriteString("\n" + m.styles.label.Render(fmt.Sprintf("Hit Preview (%s/%s · %s)", sel.Collection, sel.Path, sel.Section)) + "\n")
		snipLines := wrapHighlightedSnippet(sel.Snippet, max(32, panelW-2), m.styles.accent)
		if len(snipLines) > 3 {
			snipLines = snipLines[:3]
		}
		for _, sl := range snipLines {
			b.WriteString("  " + sl + "\n")
		}
	}
}

func (m exploreModel) viewBrowseDocs(b *strings.Builder, panelW int) {
	rows := m.listRows()
	if len(m.docs) == 0 {
		b.WriteString(m.styles.muted.Render("No indexed documents in scope. Run an index pass (Tab 3 -> i) or switch token/collection filter.") + "\n")
		return
	}

	b.WriteString(m.styles.muted.Render(fmt.Sprintf("  %-12s  %-10s  %-6s  %9s  %s", "COLLECTION", "STATUS", "TYPE", "SIZE", "PATH")) + "\n")
	end := min(len(m.docs), m.scroll+rows)
	for i := m.scroll; i < end; i++ {
		d := m.docs[i]
		line := fmt.Sprintf("%-12s  %-10s  %-6s  %9s  %s",
			truncateText(d.Collection, 12),
			truncateText(d.Status, 10),
			truncateText(d.FileType, 6),
			truncateText(formatBytesShort(d.Size), 9),
			d.Path,
		)
		line = truncateText(line, panelW)
		if i == m.cursor {
			b.WriteString(m.styles.selected.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	if m.cursor >= 0 && m.cursor < len(m.docs) {
		sel := m.docs[m.cursor]
		summary := fmt.Sprintf("Selected: %s/%s  title=%q  id=%s  modified=%s",
			sel.Collection, sel.Path, sel.Title, sel.DocID, sel.Modified)
		b.WriteString("\n" + m.styles.muted.Render(truncateText(summary, panelW)) + "\n")
	}
}

func (m exploreModel) viewReaderModal() string {
	dr := m.docReader
	outer := max(48, min(108, m.width-6))
	wrapW := max(28, outer-6)

	secLabel := "All sections"
	if len(dr.info.Outline) > 0 {
		secLabel = fmt.Sprintf("All sections (%d)", len(dr.info.Outline))
	}
	if dr.sectionIdx > 0 && dr.sectionIdx <= len(dr.info.Outline) {
		entry := dr.info.Outline[dr.sectionIdx-1]
		secLabel = fmt.Sprintf("[%d/%d] %s (%d chars)", dr.sectionIdx, len(dr.info.Outline), entry.Section, entry.Chars)
	}

	var b strings.Builder
	b.WriteString(m.styles.title.Render("Document Reader") + "  " + m.styles.muted.Render(fmt.Sprintf("%s/%s", dr.info.Collection, dr.info.Path)) + "\n")
	metaLine := fmt.Sprintf("Status: %s  |  Size: %s  |  ID: %s", dr.info.Status, formatBytesShort(dr.info.Size), dr.info.DocID)
	if dr.info.Truncated {
		metaLine += "  [TRUNCATED]"
	}
	b.WriteString(m.styles.muted.Render(truncateText(metaLine, wrapW)) + "\n")
	b.WriteString(m.styles.accent.Render("Section: "+truncateText(secLabel, max(16, wrapW-9))) + "\n\n")

	rows := m.readerRows()
	maxScroll := max(0, len(dr.lines)-rows)
	off := min(max(0, dr.scroll), maxScroll)
	end := min(len(dr.lines), off+rows)
	b.WriteString(strings.Join(dr.lines[off:end], "\n") + "\n")

	statusParts := []string{}
	if len(dr.lines) > rows {
		statusParts = append(statusParts, fmt.Sprintf("Lines %d–%d of %d", off+1, end, len(dr.lines)))
	}
	if dr.resp != nil && (dr.offset > 0 || dr.resp.NextOffset > 0) {
		win := fmt.Sprintf("Window offset %d", dr.offset)
		if dr.resp.NextOffset > 0 {
			win += fmt.Sprintf(" (next: %d)", dr.resp.NextOffset)
		}
		statusParts = append(statusParts, win)
	}
	if len(statusParts) > 0 {
		b.WriteString("\n" + m.styles.muted.Render(strings.Join(statusParts, "  •  ")))
	}

	help := "↑/↓/PgUp/PgDn scroll"
	if len(dr.info.Outline) > 0 {
		help += "  •  ←/→ section"
	}
	if dr.resp != nil && (dr.resp.NextOffset > 0 || len(dr.prevOffs) > 0) {
		help += "  •  n/p window"
	}
	help += "  •  Esc/Enter close"
	b.WriteString("\n" + m.styles.help.Render(help))
	return m.styles.modal.Width(outer).Render(b.String())
}

// wrapHighlightedSnippet renders FTS5 **...** highlight spans cleanly across
// wrapped terminal lines so highlighted phrases never break mid-escape code.
func wrapHighlightedSnippet(snippet string, width int, hi lipgloss.Style) []string {
	clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\t", " ").Replace(snippet)
	type word struct {
		text string
		hl   bool
	}
	var words []word
	parts := strings.Split(clean, "**")
	// Even indices are normal text; odd indices are highlighted when paired.
	paired := len(parts) - 1
	if paired%2 == 1 {
		paired--
	}
	for i, part := range parts {
		if i > 0 && i > paired {
			// Unpaired trailing marker: treat literal "**" cleanly as plain text.
			part = "**" + part
		}
		isHL := i%2 == 1 && i <= paired
		for _, tok := range strings.Fields(part) {
			words = append(words, word{text: tok, hl: isHL})
		}
	}
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var cur strings.Builder
	col := 0
	for _, w := range words {
		wlen := len([]rune(w.text))
		rendered := w.text
		if w.hl {
			rendered = hi.Render(w.text)
		}
		if col == 0 {
			cur.WriteString(rendered)
			col = wlen
			continue
		}
		if col+1+wlen > width {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(rendered)
			col = wlen
			continue
		}
		cur.WriteByte(' ')
		cur.WriteString(rendered)
		col += 1 + wlen
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func formatBytesShort(n int64) string {
	if n < 0 {
		return "?"
	}
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"K", "M", "G", "T"}
	val := float64(n)
	for _, u := range units {
		val /= 1024
		if val < 1024 || u == units[len(units)-1] {
			return fmt.Sprintf("%.1f%s", val, u)
		}
	}
	return fmt.Sprintf("%dB", n)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
