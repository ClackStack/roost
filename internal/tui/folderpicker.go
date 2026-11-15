package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type folderPickerDoneMsg struct {
	canceled bool
	path     string
}

type dirEntry struct {
	name      string
	path      string
	isParent  bool
	isSymlink bool
}

type folderPickerModel struct {
	title       string
	allowCreate bool
	styles      styles

	currentDir  string
	entries     []dirEntry
	cursor      int
	scroll      int
	dirCount    int
	fileCount   int
	hasRoostDB  bool
	showHidden  bool
	readErr     string
	statusErr   string
	createMode  bool
	inputActive bool
	input       textinput.Model
	width       int
	height      int
}

func newFolderPicker(title, startDir string, allowCreate bool, st styles, width, height int) folderPickerModel {
	resolved := nearestExistingDir(startDir)
	ti := newTextInput("/path/to/directory", "", max(24, width-12), st.isDark)
	ti.ShowSuggestions = true

	m := folderPickerModel{
		title:       title,
		allowCreate: allowCreate,
		styles:      st,
		currentDir:  resolved,
		input:       ti,
		width:       width,
		height:      height,
	}
	m.loadDir(resolved)
	return m
}

func nearestExistingDir(start string) string {
	p := expandHome(strings.TrimSpace(start))
	if p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		for {
			fi, err := os.Stat(p)
			if err == nil && fi.IsDir() {
				return filepath.Clean(p)
			}
			parent := filepath.Dir(p)
			if parent == p {
				break
			}
			p = parent
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return string(filepath.Separator)
}

func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (m *folderPickerModel) setSize(width, height int, st styles) {
	m.width = width
	m.height = height
	m.styles = st
	m.input.SetWidth(max(24, width-12))
	m.input.SetStyles(textinput.DefaultStyles(st.isDark))
	m.clampScroll()
}

func (m *folderPickerModel) loadDir(dir string) {
	clean := filepath.Clean(dir)
	m.currentDir = clean
	m.entries = nil
	m.cursor = 0
	m.scroll = 0
	m.dirCount = 0
	m.fileCount = 0
	m.hasRoostDB = false
	m.readErr = ""
	m.statusErr = ""

	parent := filepath.Dir(clean)
	if parent != clean {
		m.entries = append(m.entries, dirEntry{
			name:     "..",
			path:     parent,
			isParent: true,
		})
	}

	items, err := os.ReadDir(clean)
	if err != nil {
		m.readErr = err.Error()
		return
	}

	var dirs []dirEntry
	for _, d := range items {
		name := d.Name()
		isSym := d.Type()&fs.ModeSymlink != 0
		isDir := d.IsDir()
		if isSym {
			if fi, serr := os.Stat(filepath.Join(clean, name)); serr == nil && fi.IsDir() {
				isDir = true
			}
		}
		if name == "roost.db" && !isDir {
			m.hasRoostDB = true
		}
		if isDir {
			m.dirCount++
			if !m.showHidden && strings.HasPrefix(name, ".") {
				continue
			}
			dirs = append(dirs, dirEntry{
				name:      name,
				path:      filepath.Join(clean, name),
				isSymlink: isSym,
			})
		} else {
			m.fileCount++
		}
	}

	slices.SortFunc(dirs, func(a, b dirEntry) int {
		return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})
	m.entries = append(m.entries, dirs...)
}

func (m *folderPickerModel) visibleRows() int {
	return max(5, m.height-15)
}

func (m *folderPickerModel) clampScroll() {
	rows := m.visibleRows()
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.entries) && len(m.entries) > 0 {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+rows {
		m.scroll = m.cursor - rows + 1
	}
	maxScroll := max(0, len(m.entries)-rows)
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func (m *folderPickerModel) updateSuggestions() {
	typed := m.input.Value()
	if typed == "" {
		m.input.SetSuggestions(nil)
		return
	}
	raw := expandHome(typed)
	dir := filepath.Dir(raw)
	prefix := filepath.Base(raw)
	if strings.HasSuffix(raw, string(filepath.Separator)) {
		dir = filepath.Clean(raw)
		prefix = ""
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		m.input.SetSuggestions(nil)
		return
	}
	home, _ := os.UserHomeDir()
	useTilde := home != "" && strings.HasPrefix(typed, "~/")
	var sugs []string
	for _, d := range items {
		isDir := d.IsDir()
		if !isDir && d.Type()&fs.ModeSymlink != 0 {
			if fi, serr := os.Stat(filepath.Join(dir, d.Name())); serr == nil && fi.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(d.Name()), strings.ToLower(prefix)) {
			continue
		}
		if prefix == "" && strings.HasPrefix(d.Name(), ".") {
			continue
		}
		candidate := filepath.Join(dir, d.Name()) + string(filepath.Separator)
		if useTilde && strings.HasPrefix(candidate, home+string(filepath.Separator)) {
			candidate = "~" + strings.TrimPrefix(candidate, home)
		}
		sugs = append(sugs, candidate)
		if len(sugs) >= 8 {
			break
		}
	}
	slices.Sort(sugs)
	m.input.SetSuggestions(sugs)
}

func (m folderPickerModel) Update(msg tea.Msg) (folderPickerModel, tea.Cmd) {
	if m.inputActive {
		return m.updateInput(msg)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		return m, func() tea.Msg { return folderPickerDoneMsg{canceled: true} }
	case "up", "k":
		m.cursor--
		m.clampScroll()
	case "down", "j":
		m.cursor++
		m.clampScroll()
	case "pgup":
		m.cursor -= m.visibleRows()
		m.clampScroll()
	case "pgdown":
		m.cursor += m.visibleRows()
		m.clampScroll()
	case "home", "g":
		m.cursor = 0
		m.clampScroll()
	case "end", "G":
		m.cursor = len(m.entries) - 1
		m.clampScroll()
	case "enter", "right", "l":
		if m.cursor >= 0 && m.cursor < len(m.entries) {
			target := m.entries[m.cursor]
			m.loadDir(target.path)
			m.clampScroll()
		}
	case "backspace", "left", "h":
		parent := filepath.Dir(m.currentDir)
		if parent != m.currentDir {
			oldName := filepath.Base(m.currentDir)
			m.loadDir(parent)
			for i, e := range m.entries {
				if e.name == oldName {
					m.cursor = i
					break
				}
			}
			m.clampScroll()
		}
	case ".":
		m.showHidden = !m.showHidden
		m.loadDir(m.currentDir)
		m.clampScroll()
	case "/", "ctrl+l":
		m.inputActive = true
		m.createMode = false
		m.statusErr = ""
		pref := m.currentDir
		if pref != string(filepath.Separator) {
			pref += string(filepath.Separator)
		}
		m.input.SetValue(pref)
		m.input.CursorEnd()
		m.updateSuggestions()
		return m, m.input.Focus()
	case "c":
		if m.allowCreate {
			m.inputActive = true
			m.createMode = true
			m.statusErr = ""
			m.input.SetValue(filepath.Join(m.currentDir, "roost-data"))
			m.input.CursorEnd()
			m.updateSuggestions()
			return m, m.input.Focus()
		}
	case "space", "y":
		if m.readErr != "" {
			m.statusErr = "Directory is unreadable: " + m.readErr
			return m, nil
		}
		chosen := m.currentDir
		return m, func() tea.Msg { return folderPickerDoneMsg{path: chosen} }
	}
	return m, nil
}

func (m folderPickerModel) updateInput(msg tea.Msg) (folderPickerModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "esc":
		m.inputActive = false
		m.createMode = false
		m.statusErr = ""
		m.input.Blur()
		return m, nil
	case "enter":
		target := expandHome(strings.TrimSpace(m.input.Value()))
		if target == "" {
			m.statusErr = "Enter a directory path"
			return m, nil
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			m.statusErr = err.Error()
			return m, nil
		}
		fi, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) && (m.allowCreate || m.createMode) {
				m.inputActive = false
				m.input.Blur()
				return m, func() tea.Msg { return folderPickerDoneMsg{path: abs} }
			}
			m.statusErr = "Path does not exist: " + abs
			return m, nil
		}
		if !fi.IsDir() {
			m.statusErr = "Not a directory: " + abs
			return m, nil
		}
		m.inputActive = false
		m.input.Blur()
		m.loadDir(abs)
		m.clampScroll()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.updateSuggestions()
	return m, cmd
}

func (m folderPickerModel) View() string {
	var b strings.Builder
	b.WriteString(m.styles.title.Render(m.title))
	b.WriteString("\n")

	meta := fmt.Sprintf("%d folders, %d files", m.dirCount, m.fileCount)
	if m.hasRoostDB {
		meta += "  " + m.styles.ok.Render("[existing roost.db]")
	}
	if m.showHidden {
		meta += "  " + m.styles.muted.Render("[showing hidden]")
	}
	b.WriteString(m.styles.label.Render("Current folder: ") + m.styles.accent.Render(m.currentDir))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render(meta))
	b.WriteString("\n\n")

	if m.inputActive {
		prompt := "Go to path: "
		if m.createMode {
			prompt = "New storage path: "
		}
		b.WriteString(m.styles.label.Render(prompt))
		b.WriteString(m.input.View())
		b.WriteString("\n")
		if m.allowCreate || m.createMode {
			b.WriteString(m.styles.help.Render("Tab completes suggestion • Enter opens (or selects new path) • Esc back"))
		} else {
			b.WriteString(m.styles.help.Render("Tab completes suggestion • Enter jumps to directory • Esc back"))
		}
		b.WriteString("\n")
		if m.statusErr != "" {
			b.WriteString(m.styles.err.Render(m.statusErr) + "\n")
		}
		return m.styles.panel.Width(max(40, m.width-4)).Render(b.String())
	}

	if m.readErr != "" {
		b.WriteString(m.styles.err.Render("Cannot read directory: "+m.readErr) + "\n")
		b.WriteString(m.styles.muted.Render("Press Backspace to go up or / to enter a path.") + "\n\n")
	}

	rows := m.visibleRows()
	listWidth := max(36, m.width-10)
	if len(m.entries) == 0 && m.readErr == "" {
		b.WriteString(m.styles.muted.Render("(no subdirectories)") + "\n")
	} else {
		end := min(len(m.entries), m.scroll+rows)
		for i := m.scroll; i < end; i++ {
			e := m.entries[i]
			label := e.name
			if !e.isParent {
				label += "/"
			}
			if e.isSymlink {
				label += "  [symlink - skipped inside collections]"
			}
			label = truncateText(label, listWidth-4)
			if i == m.cursor {
				b.WriteString(m.styles.selected.Render("> " + label))
			} else if e.isSymlink {
				b.WriteString(m.styles.warn.Render("  " + label))
			} else {
				b.WriteString("  " + label)
			}
			b.WriteString("\n")
		}
		if len(m.entries) > rows {
			b.WriteString(m.styles.muted.Render(fmt.Sprintf("Showing %d–%d of %d", m.scroll+1, end, len(m.entries))))
			b.WriteString("\n")
		}
	}

	if m.statusErr != "" {
		b.WriteString("\n" + m.styles.err.Render(m.statusErr))
	}

	b.WriteString("\n")
	hints := []string{
		"↑/↓ move",
		"Enter/→ open",
		"Backspace/← up",
		fmt.Sprintf("Space/y choose %s", filepath.Base(m.currentDir)),
		"/ path",
	}
	if m.allowCreate {
		hints = append(hints, "c new path")
	}
	hints = append(hints, ". hidden", "Esc cancel")
	b.WriteString(m.styles.help.Render(strings.Join(hints, "  •  ")))

	return lipgloss.NewStyle().
		Width(max(44, m.width-2)).
		Render(m.styles.panel.Width(max(42, m.width-4)).Render(b.String()))
}
