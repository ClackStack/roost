package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/indexer"
	"github.com/ClackStack/roost/internal/store"
)

type indexDoneMsg struct {
	stats map[string]indexer.Stats
	err   error
}

// IndexFunc runs one synchronous indexing pass over cfg against st.
type IndexFunc func(ctx context.Context, cfg *config.Config, st *store.Store) (map[string]indexer.Stats, error)

func runIndexCmd(ctx context.Context, fn IndexFunc, cfg *config.Config, st *store.Store) tea.Cmd {
	snap := cfg.Clone()
	return func() tea.Msg {
		if fn == nil {
			return indexDoneMsg{err: errors.New("indexer is not configured")}
		}
		stats, err := fn(ctx, snap, st)
		return indexDoneMsg{stats: stats, err: err}
	}
}

func formatIndexSummary(stats map[string]indexer.Stats, err error) string {
	if len(stats) == 0 && err != nil {
		return err.Error()
	}
	var seen, indexed, unchanged, deleted, problems int
	var deleteBlocked []string
	for name, s := range stats {
		seen += s.Seen
		indexed += s.Indexed
		unchanged += s.Unchanged
		deleted += s.Deleted
		problems += s.Problems
		if s.DeleteBlocked {
			deleteBlocked = append(deleteBlocked, name)
		}
	}
	summary := fmt.Sprintf("Indexed %d files (%d new/updated, %d unchanged, %d deleted, %d need attention)",
		seen, indexed, unchanged, deleted, problems)
	if len(deleteBlocked) > 0 {
		summary += fmt.Sprintf("; stale kept for %s", strings.Join(deleteBlocked, ","))
	}
	if err != nil {
		summary += " — partial error: " + err.Error()
	}
	return summary
}

type collectionSavedMsg struct {
	editIndex int
	col       config.Collection
}

type collectionCanceledMsg struct{}

type collectionEditor struct {
	editIndex int
	dataDir   string
	existing  []config.Collection
	styles    styles
	width     int
	height    int

	path          textinput.Model
	name          textinput.Model
	exclude       textinput.Model
	suggestedName string
	focus         int // 0=path, 1=browse, 2=name, 3=exclude, 4=save
	err           string
	picker        *folderPickerModel
}

func newCollectionEditor(editIndex int, initial config.Collection, dataDir string, existing []config.Collection, st styles, width, height int) collectionEditor {
	iw := max(24, min(64, width-20))
	m := collectionEditor{
		editIndex: editIndex,
		dataDir:   dataDir,
		existing:  existing,
		styles:    st,
		width:     width,
		height:    height,
		path:      newTextInput("/srv/documents/legal", initial.Path, iw, st.isDark),
		name:      newTextInput("legal", initial.Name, iw, st.isDark),
		exclude:   newTextInput("*.tmp, drafts (optional comma-separated globs)", joinExcludes(initial.Exclude), iw, st.isDark),
	}
	if editIndex < 0 && initial.Name == "" && initial.Path != "" {
		s := suggestCollectionName(initial.Path)
		m.suggestedName = s
		m.name.SetValue(s)
	}
	m.applyFocus()
	return m
}

func (m *collectionEditor) setSize(width, height int, st styles) {
	m.width = width
	m.height = height
	m.styles = st
	iw := max(24, min(64, width-20))
	for _, ti := range []*textinput.Model{&m.path, &m.name, &m.exclude} {
		ti.SetWidth(iw)
		ti.SetStyles(textinput.DefaultStyles(st.isDark))
	}
	if m.picker != nil {
		m.picker.setSize(width, height, st)
	}
}

func (m *collectionEditor) applyFocus() tea.Cmd {
	m.path.Blur()
	m.name.Blur()
	m.exclude.Blur()
	switch m.focus {
	case 0:
		return m.path.Focus()
	case 2:
		return m.name.Focus()
	case 3:
		return m.exclude.Focus()
	}
	return nil
}

func (m *collectionEditor) openPicker() {
	start := strings.TrimSpace(m.path.Value())
	p := newFolderPicker("Choose Folder for Collection", start, false, m.styles, m.width, m.height)
	m.picker = &p
}

func (m collectionEditor) validateAndBuild() (config.Collection, error) {
	rawPath := expandHome(strings.TrimSpace(m.path.Value()))
	if rawPath == "" {
		return config.Collection{}, errors.New("collection folder path is required")
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return config.Collection{}, fmt.Errorf("collection path: %w", err)
	}
	fi, err := os.Stat(absPath)
	if err != nil {
		return config.Collection{}, fmt.Errorf("collection path: %w", err)
	}
	if !fi.IsDir() {
		return config.Collection{}, fmt.Errorf("%s is not a directory", absPath)
	}

	name := strings.TrimSpace(m.name.Value())
	if err := config.ValidCollectionName(name); err != nil {
		return config.Collection{}, err
	}
	for i, c := range m.existing {
		if i != m.editIndex && c.Name == name {
			return config.Collection{}, fmt.Errorf("collection %q already exists", name)
		}
	}
	col := config.Collection{
		Name:    name,
		Path:    absPath,
		Exclude: parseExcludes(m.exclude.Value()),
	}
	for _, pat := range col.Exclude {
		if _, err := filepath.Match(pat, ""); err != nil {
			return config.Collection{}, fmt.Errorf("bad exclude pattern %q: %w", pat, err)
		}
	}
	if err := config.CheckDataDirOverlap(m.dataDir, []config.Collection{col}); err != nil {
		return config.Collection{}, err
	}
	return col, nil
}

func (m collectionEditor) Update(msg tea.Msg) (collectionEditor, tea.Cmd) {
	if m.picker != nil {
		if res, ok := msg.(folderPickerDoneMsg); ok {
			m.picker = nil
			if !res.canceled && res.path != "" {
				m.path.SetValue(res.path)
				m.path.CursorEnd()
				m.syncSuggestedName()
				m.focus = 2
				return m, m.applyFocus()
			}
			return m, m.applyFocus()
		}
		np, cmd := m.picker.Update(msg)
		m.picker = &np
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateInputs(msg)
	}

	switch key.String() {
	case "esc":
		return m, func() tea.Msg { return collectionCanceledMsg{} }
	case "ctrl+b":
		m.err = ""
		m.openPicker()
		return m, nil
	case "ctrl+s":
		return m.submit()
	case "tab", "down":
		m.focus = (m.focus + 1) % 5
		return m, m.applyFocus()
	case "shift+tab", "up":
		m.focus = (m.focus + 4) % 5
		return m, m.applyFocus()
	case "enter":
		switch m.focus {
		case 1:
			m.err = ""
			m.openPicker()
			return m, nil
		case 3, 4:
			return m.submit()
		default:
			m.focus = (m.focus + 1) % 5
			return m, m.applyFocus()
		}
	case "space":
		if m.focus == 1 {
			m.err = ""
			m.openPicker()
			return m, nil
		}
		if m.focus == 4 {
			return m.submit()
		}
	}
	return m.updateInputs(msg)
}

func (m collectionEditor) updateInputs(msg tea.Msg) (collectionEditor, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case 0:
		m.path, cmd = m.path.Update(msg)
		m.err = ""
		m.syncSuggestedName()
	case 2:
		m.name, cmd = m.name.Update(msg)
		m.err = ""
	case 3:
		m.exclude, cmd = m.exclude.Update(msg)
		m.err = ""
	}
	return m, cmd
}

func (m *collectionEditor) syncSuggestedName() {
	if m.editIndex >= 0 {
		return
	}
	p := strings.TrimSpace(m.path.Value())
	if p == "" {
		return
	}
	cur := strings.TrimSpace(m.name.Value())
	if cur == "" || cur == m.suggestedName {
		m.suggestedName = suggestCollectionName(p)
		m.name.SetValue(m.suggestedName)
		m.name.CursorEnd()
	}
}

func (m collectionEditor) submit() (collectionEditor, tea.Cmd) {
	col, err := m.validateAndBuild()
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	idx := m.editIndex
	return m, func() tea.Msg { return collectionSavedMsg{editIndex: idx, col: col} }
}

func (m collectionEditor) View() string {
	if m.picker != nil {
		return m.picker.View()
	}
	var b strings.Builder
	title := "Add Collection"
	if m.editIndex >= 0 {
		title = "Edit Collection"
	}
	b.WriteString(m.styles.title.Render(title))
	b.WriteString("\n\n")

	renderRow := func(idx int, label, content string) {
		prefix := "  "
		if m.focus == idx {
			prefix = m.styles.cursor.Render("> ")
		}
		b.WriteString(fmt.Sprintf("%s%s\n   %s\n\n", prefix, m.styles.label.Render(label), content))
	}

	renderRow(0, "Folder path (Ctrl+B to browse)", m.path.View())

	browseBtn := "[ Browse folders... ]"
	if m.focus == 1 {
		browseBtn = m.styles.selected.Render(browseBtn)
	} else {
		browseBtn = m.styles.accent.Render(browseBtn)
	}
	b.WriteString("  " + browseBtn + "\n\n")

	renderRow(2, "Collection name (lowercase letters, digits, _ or -)", m.name.View())
	renderRow(3, "Exclude patterns (comma-separated, optional)", m.exclude.View())

	saveBtn := "[ Save Collection ]"
	if m.focus == 4 {
		saveBtn = m.styles.selected.Render(saveBtn)
	} else {
		saveBtn = m.styles.ok.Render(saveBtn)
	}
	b.WriteString("  " + saveBtn + "\n")

	if m.err != "" {
		b.WriteString("\n" + m.styles.err.Render(m.err))
	}
	b.WriteString("\n\n" + m.styles.help.Render("Tab/↑/↓ move  •  Ctrl+B browse folder  •  Ctrl+S / Enter save  •  Esc cancel"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

type tokenDoneMsg struct {
	secret string
	rec    store.TokenRecord
}

type tokenCanceledMsg struct{}

var ttlPresets = []struct {
	label string
	dur   time.Duration
}{
	{label: "never (no expiry)", dur: 0},
	{label: "24h (1 day)", dur: 24 * time.Hour},
	{label: "168h (7 days)", dur: 7 * 24 * time.Hour},
	{label: "720h (30 days)", dur: 30 * 24 * time.Hour},
	{label: "2160h (90 days)", dur: 90 * 24 * time.Hour},
	{label: "custom...", dur: -1},
}

type tokenCreator struct {
	collections    []string
	styles         styles
	width          int
	name           textinput.Model
	allCollections bool
	selected       map[string]bool
	ttlIdx         int
	customTTL      textinput.Model
	focus          int
	err            string

	secret  string
	created store.TokenRecord
}

func newTokenCreator(defaultName string, collections []string, st styles, width int) tokenCreator {
	iw := max(20, min(48, width-24))
	sel := make(map[string]bool, len(collections))
	for _, c := range collections {
		sel[c] = true
	}
	m := tokenCreator{
		collections:    collections,
		styles:         st,
		width:          width,
		name:           newTextInput("e.g. quack, claude-desktop, ci", defaultName, iw, st.isDark),
		allCollections: true,
		selected:       sel,
		ttlIdx:         0,
		customTTL:      newTextInput("e.g. 48h or 30m", "720h", max(14, iw/2), st.isDark),
	}
	m.applyFocus()
	return m
}

func (m *tokenCreator) setSize(width int, st styles) {
	m.width = width
	m.styles = st
	iw := max(20, min(48, width-24))
	m.name.SetWidth(iw)
	m.name.SetStyles(textinput.DefaultStyles(st.isDark))
	m.customTTL.SetWidth(max(14, iw/2))
	m.customTTL.SetStyles(textinput.DefaultStyles(st.isDark))
}

func (m tokenCreator) maxFocus() int {
	// 0: name
	// 1: * all toggle
	// 2..2+n-1: per-collection (only active when !allCollections)
	// ttl row
	// custom ttl row (only when custom selected)
	// create button
	rows := 2 // name + all toggle
	if !m.allCollections {
		rows += len(m.collections)
	}
	rows++ // ttl selector
	if ttlPresets[m.ttlIdx].dur < 0 {
		rows++
	}
	rows++ // submit button
	return rows - 1
}

func (m tokenCreator) roleForFocus() (kind string, colIdx int) {
	if m.focus == 0 {
		return "name", -1
	}
	if m.focus == 1 {
		return "all", -1
	}
	idx := 2
	if !m.allCollections {
		if m.focus < idx+len(m.collections) {
			return "col", m.focus - idx
		}
		idx += len(m.collections)
	}
	if m.focus == idx {
		return "ttl", -1
	}
	idx++
	if ttlPresets[m.ttlIdx].dur < 0 {
		if m.focus == idx {
			return "custom_ttl", -1
		}
		idx++
	}
	return "submit", -1
}

func (m *tokenCreator) applyFocus() tea.Cmd {
	maxF := m.maxFocus()
	if m.focus > maxF {
		m.focus = maxF
	}
	if m.focus < 0 {
		m.focus = 0
	}
	m.name.Blur()
	m.customTTL.Blur()
	kind, _ := m.roleForFocus()
	switch kind {
	case "name":
		return m.name.Focus()
	case "custom_ttl":
		return m.customTTL.Focus()
	}
	return nil
}

func (m tokenCreator) grants() []string {
	if m.allCollections {
		return []string{auth.AllCollections}
	}
	var out []string
	for _, c := range m.collections {
		if m.selected[c] {
			out = append(out, c)
		}
	}
	return out
}

func (m tokenCreator) lifetime() (time.Duration, error) {
	p := ttlPresets[m.ttlIdx]
	if p.dur >= 0 {
		return p.dur, nil
	}
	raw := strings.TrimSpace(m.customTTL.Value())
	if raw == "" || raw == "never" || raw == "0" || raw == "0s" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 24h, 720h)", raw)
	}
	if d < 0 {
		return 0, errors.New("token lifetime must be positive")
	}
	return d, nil
}

func (m tokenCreator) create(ctx context.Context, st *store.Store) (tokenCreator, tea.Cmd) {
	name := strings.TrimSpace(m.name.Value())
	grants := m.grants()
	ttl, err := m.lifetime()
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	secret, rec, err := auth.Create(ctx, st, name, grants, ttl)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	m.secret = secret
	m.created = rec
	return m, func() tea.Msg { return tokenDoneMsg{secret: secret, rec: rec} }
}

func (m tokenCreator) Update(msg tea.Msg, ctx context.Context, st *store.Store) (tokenCreator, tea.Cmd) {
	if m.secret != "" {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "enter", "esc", "q":
				return m, func() tea.Msg { return tokenCanceledMsg{} }
			}
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateInputs(msg)
	}

	kind, colIdx := m.roleForFocus()
	switch key.String() {
	case "esc":
		return m, func() tea.Msg { return tokenCanceledMsg{} }
	case "tab", "down":
		m.focus++
		if m.focus > m.maxFocus() {
			m.focus = 0
		}
		return m, m.applyFocus()
	case "shift+tab", "up":
		m.focus--
		if m.focus < 0 {
			m.focus = m.maxFocus()
		}
		return m, m.applyFocus()
	case "left":
		if kind == "ttl" {
			m.ttlIdx = (m.ttlIdx + len(ttlPresets) - 1) % len(ttlPresets)
			return m, m.applyFocus()
		}
	case "right":
		if kind == "ttl" {
			m.ttlIdx = (m.ttlIdx + 1) % len(ttlPresets)
			return m, m.applyFocus()
		}
	case "space":
		switch kind {
		case "all":
			m.allCollections = !m.allCollections
			return m, m.applyFocus()
		case "col":
			c := m.collections[colIdx]
			m.selected[c] = !m.selected[c]
			return m, nil
		case "ttl":
			m.ttlIdx = (m.ttlIdx + 1) % len(ttlPresets)
			return m, m.applyFocus()
		case "submit":
			return m.create(ctx, st)
		}
	case "enter":
		switch kind {
		case "all":
			m.allCollections = !m.allCollections
			return m, m.applyFocus()
		case "col":
			c := m.collections[colIdx]
			m.selected[c] = !m.selected[c]
			return m, nil
		case "ttl":
			m.ttlIdx = (m.ttlIdx + 1) % len(ttlPresets)
			return m, m.applyFocus()
		case "submit", "custom_ttl":
			return m.create(ctx, st)
		default:
			m.focus++
			return m, m.applyFocus()
		}
	}
	return m.updateInputs(msg)
}

func (m tokenCreator) updateInputs(msg tea.Msg) (tokenCreator, tea.Cmd) {
	kind, _ := m.roleForFocus()
	var cmd tea.Cmd
	switch kind {
	case "name":
		m.name, cmd = m.name.Update(msg)
		m.err = ""
	case "custom_ttl":
		m.customTTL, cmd = m.customTTL.Update(msg)
		m.err = ""
	}
	return m, cmd
}

func (m tokenCreator) ViewForm(showSnippets bool, cfg *config.Config, cfgPath string) string {
	var b strings.Builder
	if m.secret != "" {
		b.WriteString(m.styles.ok.Render(fmt.Sprintf("Token %q created (ID %s)", m.created.Name, m.created.ID)))
		b.WriteString("\n\n")
		b.WriteString(m.styles.warn.Render("Copy this secret now — Roost stores only its SHA-256 hash and cannot display it again:"))
		b.WriteString("\n")
		b.WriteString(m.styles.secretBox.Render(m.secret))
		b.WriteString("\n\n")
		if showSnippets && cfg != nil {
			b.WriteString(m.styles.codeBox.Render(clientSnippets(cfg, cfgPath, m.secret)))
			b.WriteString("\n\n")
		}
		b.WriteString(m.styles.help.Render("Press Enter or Esc to close"))
		return b.String()
	}

	kind, focusedCol := m.roleForFocus()
	prefix := func(match bool) string {
		if match {
			return m.styles.cursor.Render("> ")
		}
		return "  "
	}

	b.WriteString(prefix(kind == "name") + m.styles.label.Render("Token name: ") + m.name.View() + "\n\n")

	checkAll := "[ ]"
	if m.allCollections {
		checkAll = "[x]"
	}
	b.WriteString(prefix(kind == "all") + fmt.Sprintf("%s All collections (*) — includes collections added later\n", checkAll))

	if !m.allCollections {
		for i, c := range m.collections {
			box := "[ ]"
			if m.selected[c] {
				box = "[x]"
			}
			b.WriteString("  " + prefix(kind == "col" && focusedCol == i) + fmt.Sprintf("%s %s\n", box, c))
		}
	}
	b.WriteString("\n")

	b.WriteString(prefix(kind == "ttl") + m.styles.label.Render("Lifetime: ") + m.styles.accent.Render("< "+ttlPresets[m.ttlIdx].label+" >") + "\n")
	if ttlPresets[m.ttlIdx].dur < 0 {
		b.WriteString("  " + prefix(kind == "custom_ttl") + m.styles.label.Render("Duration: ") + m.customTTL.View() + "\n")
	}
	b.WriteString("\n")

	btn := "[ Create Token ]"
	if kind == "submit" {
		btn = m.styles.selected.Render(btn)
	} else {
		btn = m.styles.ok.Render(btn)
	}
	b.WriteString(prefix(kind == "submit") + btn + "\n")

	if m.err != "" {
		b.WriteString("\n" + m.styles.err.Render(m.err))
	}
	return b.String()
}
