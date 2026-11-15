package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/store"
)

type wizardStep int

const (
	wizWelcome wizardStep = iota
	wizStorage
	wizCollections
	wizServer
	wizEmbeddings
	wizReview
	wizFinish
)

type switchToDashboardMsg struct {
	cfg *config.Config
	st  *store.Store
	tab dashTab
}

type wizardSavedConfigMsg struct {
	cfg *config.Config
	st  *store.Store
}

type wizardModel struct {
	ctx       context.Context
	cfgPath   string
	cfgExists bool
	indexFn   IndexFunc
	styles    styles
	width     int
	height    int

	step    wizardStep
	cfg     config.Config
	store   *store.Store
	errMsg  string
	infoMsg string

	// Step 1: Storage
	dataDirInput  textinput.Model
	storageFocus  int // 0=input, 1=browse, 2=next
	storagePicker *folderPickerModel

	// Step 2: Collections
	colCursor int
	colEd     *collectionEditor

	// Step 3: Server
	srvPreset    int // 0=loopback, 1=proxy, 2=tls, 3=custom
	srvFocus     int
	srvListen    textinput.Model
	srvCert      textinput.Model
	srvKey       textinput.Model
	srvPlaintext bool

	// Step 4: Embeddings
	embFocus        int // 0=enabled, 1=endpoint, 2=model, 3=api_key_env, 4=batch, 5=timeout, 6=allow_public, 7=next
	embEnabled      bool
	embEndpoint     textinput.Model
	embModel        textinput.Model
	embAPIKeyEnv    textinput.Model
	embBatch        textinput.Model
	embTimeout      textinput.Model
	embAllowPublic  bool

	// Step 5: Review
	reviewTOML   string
	reviewLines  []string
	reviewScroll int

	// Step 6: Finish
	tokModal    *tokenCreator
	lastSecret  string
	lastRec     store.TokenRecord
	indexing    bool
	spin        spinner.Model
	indexReport string
}

func newWizardModel(ctx context.Context, cfgPath string, seed *config.Config, exists bool, indexFn IndexFunc, st styles, width, height int) wizardModel {
	cfg := config.Default()
	if seed != nil {
		cfg = *seed.Clone()
	}

	iw := max(24, min(60, width-20))
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))

	srvPreset := 0
	if cfg.Server.TLSCert != "" || cfg.Server.TLSKey != "" {
		srvPreset = 2
	} else if cfg.Server.Listen == "0.0.0.0:8484" && cfg.Server.AllowPlaintext {
		srvPreset = 1
	} else if cfg.Server.Listen != "127.0.0.1:8484" || cfg.Server.AllowPlaintext {
		srvPreset = 3
	}

	endpoint := cfg.Embeddings.Endpoint
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1/embeddings"
	}
	model := cfg.Embeddings.Model
	if model == "" {
		model = "nomic-embed-text"
	}

	m := wizardModel{
		ctx:            ctx,
		cfgPath:        cfgPath,
		cfgExists:      exists,
		indexFn:        indexFn,
		styles:         st,
		width:          width,
		height:         height,
		step:           wizWelcome,
		cfg:            cfg,
		dataDirInput:   newTextInput("./data", cfg.Storage.DataDir, iw, st.isDark),
		srvPreset:      srvPreset,
		srvListen:      newTextInput("127.0.0.1:8484", cfg.Server.Listen, iw, st.isDark),
		srvCert:        newTextInput("/etc/roost/cert.pem", cfg.Server.TLSCert, iw, st.isDark),
		srvKey:         newTextInput("/etc/roost/key.pem", cfg.Server.TLSKey, iw, st.isDark),
		srvPlaintext:   cfg.Server.AllowPlaintext,
		embEnabled:     cfg.Embeddings.Enabled,
		embEndpoint:    newTextInput("http://127.0.0.1:11434/v1/embeddings", endpoint, iw, st.isDark),
		embModel:       newTextInput("nomic-embed-text", model, iw, st.isDark),
		embAPIKeyEnv:   newTextInput("ROOST_EMBEDDINGS_KEY (optional env var name)", cfg.Embeddings.APIKeyEnv, iw, st.isDark),
		embBatch:       newTextInput("32", strconv.Itoa(max(1, cfg.Embeddings.BatchSize)), 16, st.isDark),
		embTimeout:     newTextInput("1m0s", cfg.Embeddings.Timeout.Duration.String(), 16, st.isDark),
		embAllowPublic: cfg.Embeddings.AllowPublicEndpoint,
		spin:           sp,
	}
	return m
}

func (m *wizardModel) setSize(width, height int, st styles) {
	m.width = width
	m.height = height
	m.styles = st
	iw := max(24, min(60, width-20))
	for _, ti := range []*textinput.Model{
		&m.dataDirInput, &m.srvListen, &m.srvCert, &m.srvKey,
		&m.embEndpoint, &m.embModel, &m.embAPIKeyEnv,
	} {
		ti.SetWidth(iw)
		ti.SetStyles(textinput.DefaultStyles(st.isDark))
	}
	for _, ti := range []*textinput.Model{&m.embBatch, &m.embTimeout} {
		ti.SetStyles(textinput.DefaultStyles(st.isDark))
	}
	if m.storagePicker != nil {
		m.storagePicker.setSize(width, height, st)
	}
	if m.colEd != nil {
		m.colEd.setSize(width, height, st)
	}
	if m.tokModal != nil {
		m.tokModal.setSize(width, st)
	}
}

func (m wizardModel) Update(msg tea.Msg) (wizardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.indexing {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case indexDoneMsg:
		m.indexing = false
		m.indexReport = formatIndexSummary(msg.stats, msg.err)
		if msg.err != nil && len(msg.stats) == 0 {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
		}
		return m, nil
	}

	switch m.step {
	case wizWelcome:
		return m.updateWelcome(msg)
	case wizStorage:
		return m.updateStorage(msg)
	case wizCollections:
		return m.updateCollections(msg)
	case wizServer:
		return m.updateServer(msg)
	case wizEmbeddings:
		return m.updateEmbeddings(msg)
	case wizReview:
		return m.updateReview(msg)
	case wizFinish:
		return m.updateFinish(msg)
	}
	return m, nil
}

func (m wizardModel) updateWelcome(msg tea.Msg) (wizardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "enter", "right", "n":
		m.step = wizStorage
		m.storageFocus = 0
		m.errMsg = ""
		return m, m.dataDirInput.Focus()
	}
	return m, nil
}

func (m wizardModel) updateStorage(msg tea.Msg) (wizardModel, tea.Cmd) {
	if m.storagePicker != nil {
		if res, ok := msg.(folderPickerDoneMsg); ok {
			m.storagePicker = nil
			if !res.canceled && res.path != "" {
				m.dataDirInput.SetValue(res.path)
				m.dataDirInput.CursorEnd()
				m.errMsg = ""
			}
			if m.storageFocus == 0 {
				return m, m.dataDirInput.Focus()
			}
			return m, nil
		}
		np, cmd := m.storagePicker.Update(msg)
		m.storagePicker = &np
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		if m.storageFocus == 0 {
			m.dataDirInput, cmd = m.dataDirInput.Update(msg)
		}
		return m, cmd
	}

	switch key.String() {
	case "esc":
		m.step = wizWelcome
		m.dataDirInput.Blur()
		return m, nil
	case "ctrl+b":
		return m.openStoragePicker(), nil
	case "tab", "down":
		m.storageFocus = (m.storageFocus + 1) % 3
		return m, m.applyStorageFocus()
	case "shift+tab", "up":
		m.storageFocus = (m.storageFocus + 2) % 3
		return m, m.applyStorageFocus()
	case "enter":
		if m.storageFocus == 1 {
			return m.openStoragePicker(), nil
		}
		return m.commitStorage()
	case "space":
		if m.storageFocus == 1 {
			return m.openStoragePicker(), nil
		}
		if m.storageFocus == 2 {
			return m.commitStorage()
		}
	}
	if m.storageFocus == 0 {
		var cmd tea.Cmd
		m.dataDirInput, cmd = m.dataDirInput.Update(msg)
		m.errMsg = ""
		return m, cmd
	}
	return m, nil
}

func (m *wizardModel) applyStorageFocus() tea.Cmd {
	if m.storageFocus == 0 {
		return m.dataDirInput.Focus()
	}
	m.dataDirInput.Blur()
	return nil
}

func (m wizardModel) openStoragePicker() wizardModel {
	m.dataDirInput.Blur()
	p := newFolderPicker("Choose Index Storage Directory (data_dir)", m.dataDirInput.Value(), true, m.styles, m.width, m.height)
	m.storagePicker = &p
	return m
}

func (m wizardModel) commitStorage() (wizardModel, tea.Cmd) {
	raw := expandHome(strings.TrimSpace(m.dataDirInput.Value()))
	if raw == "" {
		m.errMsg = "storage.data_dir cannot be empty"
		return m, nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	if err := config.CheckDataDirOverlap(abs, m.cfg.Collections); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.cfg.Storage.DataDir = abs
	m.dataDirInput.SetValue(abs)
	m.dataDirInput.Blur()
	m.errMsg = ""
	m.step = wizCollections
	return m, nil
}

func (m wizardModel) updateCollections(msg tea.Msg) (wizardModel, tea.Cmd) {
	if m.colEd != nil {
		switch res := msg.(type) {
		case collectionCanceledMsg:
			m.colEd = nil
			return m, nil
		case collectionSavedMsg:
			if res.editIndex >= 0 && res.editIndex < len(m.cfg.Collections) {
				m.cfg.Collections[res.editIndex] = res.col
				m.colCursor = res.editIndex
			} else {
				m.cfg.Collections = append(m.cfg.Collections, res.col)
				m.colCursor = len(m.cfg.Collections) - 1
			}
			m.colEd = nil
			m.errMsg = ""
			return m, nil
		}
		ed, cmd := m.colEd.Update(msg)
		m.colEd = &ed
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "b":
		m.step = wizStorage
		m.storageFocus = 0
		m.errMsg = ""
		return m, m.dataDirInput.Focus()
	case "up", "k":
		if m.colCursor > 0 {
			m.colCursor--
		}
	case "down", "j":
		if m.colCursor < len(m.cfg.Collections)-1 {
			m.colCursor++
		}
	case "a":
		ed := newCollectionEditor(-1, config.Collection{}, m.cfg.Storage.DataDir, m.cfg.Collections, m.styles, m.width, m.height)
		m.colEd = &ed
		m.errMsg = ""
		return m, ed.applyFocus()
	case "e":
		if len(m.cfg.Collections) > 0 && m.colCursor < len(m.cfg.Collections) {
			ed := newCollectionEditor(m.colCursor, m.cfg.Collections[m.colCursor], m.cfg.Storage.DataDir, m.cfg.Collections, m.styles, m.width, m.height)
			m.colEd = &ed
			m.errMsg = ""
			return m, ed.applyFocus()
		}
	case "d", "delete":
		if len(m.cfg.Collections) > 0 && m.colCursor < len(m.cfg.Collections) {
			m.cfg.Collections = append(m.cfg.Collections[:m.colCursor], m.cfg.Collections[m.colCursor+1:]...)
			if m.colCursor >= len(m.cfg.Collections) && m.colCursor > 0 {
				m.colCursor--
			}
			m.errMsg = ""
		}
	case "enter", "n":
		if len(m.cfg.Collections) == 0 {
			ed := newCollectionEditor(-1, config.Collection{}, m.cfg.Storage.DataDir, m.cfg.Collections, m.styles, m.width, m.height)
			m.colEd = &ed
			return m, ed.applyFocus()
		}
		if err := config.CheckDataDirOverlap(m.cfg.Storage.DataDir, m.cfg.Collections); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.step = wizServer
		m.srvFocus = 0
		return m, m.applyServerFocus()
	}
	return m, nil
}

func (m wizardModel) serverRows() []string {
	rows := []string{"preset:0", "preset:1", "preset:2", "preset:3"}
	if m.srvPreset == 2 || m.srvPreset == 3 {
		if m.srvPreset == 3 {
			rows = append(rows, "listen", "plaintext")
		}
		rows = append(rows, "cert", "key")
	}
	if m.srvPreset == 1 {
		rows = append(rows, "listen")
	}
	rows = append(rows, "next")
	return rows
}

func (m *wizardModel) applyServerFocus() tea.Cmd {
	rows := m.serverRows()
	if m.srvFocus >= len(rows) {
		m.srvFocus = len(rows) - 1
	}
	if m.srvFocus < 0 {
		m.srvFocus = 0
	}
	m.srvListen.Blur()
	m.srvCert.Blur()
	m.srvKey.Blur()
	switch rows[m.srvFocus] {
	case "listen":
		return m.srvListen.Focus()
	case "cert":
		return m.srvCert.Focus()
	case "key":
		return m.srvKey.Focus()
	}
	return nil
}

func (m *wizardModel) selectServerPreset(p int) {
	m.srvPreset = p
	switch p {
	case 0:
		m.srvListen.SetValue("127.0.0.1:8484")
		m.srvCert.SetValue("")
		m.srvKey.SetValue("")
		m.srvPlaintext = false
	case 1:
		m.srvListen.SetValue("0.0.0.0:8484")
		m.srvCert.SetValue("")
		m.srvKey.SetValue("")
		m.srvPlaintext = true
	case 2:
		if m.srvListen.Value() == "" || m.srvListen.Value() == "127.0.0.1:8484" {
			m.srvListen.SetValue("0.0.0.0:8484")
		}
		m.srvPlaintext = false
	case 3:
		// keep current values for custom editing
	}
}

func (m wizardModel) updateServer(msg tea.Msg) (wizardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateServerInputs(msg)
	}
	rows := m.serverRows()
	cur := rows[m.srvFocus]
	switch key.String() {
	case "esc":
		m.step = wizCollections
		m.errMsg = ""
		m.srvListen.Blur()
		m.srvCert.Blur()
		m.srvKey.Blur()
		return m, nil
	case "tab", "down":
		m.srvFocus = (m.srvFocus + 1) % len(rows)
		return m, m.applyServerFocus()
	case "shift+tab", "up":
		m.srvFocus = (m.srvFocus + len(rows) - 1) % len(rows)
		return m, m.applyServerFocus()
	case "space":
		if strings.HasPrefix(cur, "preset:") {
			p, _ := strconv.Atoi(strings.TrimPrefix(cur, "preset:"))
			m.selectServerPreset(p)
			m.errMsg = ""
			return m, m.applyServerFocus()
		}
		if cur == "plaintext" {
			m.srvPlaintext = !m.srvPlaintext
			m.errMsg = ""
			return m, nil
		}
		if cur == "next" {
			return m.commitServer()
		}
	case "enter":
		if strings.HasPrefix(cur, "preset:") {
			p, _ := strconv.Atoi(strings.TrimPrefix(cur, "preset:"))
			m.selectServerPreset(p)
			m.errMsg = ""
			if p == 0 {
				return m.commitServer()
			}
			m.srvFocus++
			return m, m.applyServerFocus()
		}
		if cur == "plaintext" {
			m.srvPlaintext = !m.srvPlaintext
			m.errMsg = ""
			return m, nil
		}
		if cur == "next" {
			return m.commitServer()
		}
		m.srvFocus = (m.srvFocus + 1) % len(rows)
		return m, m.applyServerFocus()
	}
	return m.updateServerInputs(msg)
}

func (m wizardModel) updateServerInputs(msg tea.Msg) (wizardModel, tea.Cmd) {
	rows := m.serverRows()
	var cmd tea.Cmd
	switch rows[m.srvFocus] {
	case "listen":
		m.srvListen, cmd = m.srvListen.Update(msg)
	case "cert":
		m.srvCert, cmd = m.srvCert.Update(msg)
	case "key":
		m.srvKey, cmd = m.srvKey.Update(msg)
	}
	m.errMsg = ""
	return m, cmd
}

func (m wizardModel) commitServer() (wizardModel, tea.Cmd) {
	srv := config.Server{
		Listen:         strings.TrimSpace(m.srvListen.Value()),
		TLSCert:        strings.TrimSpace(m.srvCert.Value()),
		TLSKey:         strings.TrimSpace(m.srvKey.Value()),
		AllowPlaintext: m.srvPlaintext,
	}
	cand := m.cfg.Clone()
	cand.Server = srv
	if err := cand.Validate(); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.cfg.Server = srv
	m.errMsg = ""
	m.srvListen.Blur()
	m.srvCert.Blur()
	m.srvKey.Blur()
	m.step = wizEmbeddings
	m.embFocus = 0
	return m, m.applyEmbFocus()
}

func (m wizardModel) embRows() []string {
	if !m.embEnabled {
		return []string{"enabled", "next"}
	}
	return []string{"enabled", "endpoint", "model", "api_key_env", "batch", "timeout", "allow_public", "next"}
}

func (m *wizardModel) applyEmbFocus() tea.Cmd {
	rows := m.embRows()
	if m.embFocus >= len(rows) {
		m.embFocus = len(rows) - 1
	}
	if m.embFocus < 0 {
		m.embFocus = 0
	}
	for _, ti := range []*textinput.Model{&m.embEndpoint, &m.embModel, &m.embAPIKeyEnv, &m.embBatch, &m.embTimeout} {
		ti.Blur()
	}
	switch rows[m.embFocus] {
	case "endpoint":
		return m.embEndpoint.Focus()
	case "model":
		return m.embModel.Focus()
	case "api_key_env":
		return m.embAPIKeyEnv.Focus()
	case "batch":
		return m.embBatch.Focus()
	case "timeout":
		return m.embTimeout.Focus()
	}
	return nil
}

func (m wizardModel) updateEmbeddings(msg tea.Msg) (wizardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateEmbInputs(msg)
	}
	rows := m.embRows()
	cur := rows[m.embFocus]
	switch key.String() {
	case "esc":
		m.step = wizServer
		m.errMsg = ""
		return m, m.applyServerFocus()
	case "tab", "down":
		m.embFocus = (m.embFocus + 1) % len(rows)
		return m, m.applyEmbFocus()
	case "shift+tab", "up":
		m.embFocus = (m.embFocus + len(rows) - 1) % len(rows)
		return m, m.applyEmbFocus()
	case "space":
		switch cur {
		case "enabled":
			m.embEnabled = !m.embEnabled
			m.errMsg = ""
			return m, m.applyEmbFocus()
		case "allow_public":
			m.embAllowPublic = !m.embAllowPublic
			m.errMsg = ""
			return m, nil
		case "next":
			return m.commitEmbeddings()
		}
	case "enter":
		switch cur {
		case "enabled":
			m.embEnabled = !m.embEnabled
			m.errMsg = ""
			return m, m.applyEmbFocus()
		case "allow_public":
			m.embAllowPublic = !m.embAllowPublic
			m.errMsg = ""
			return m, nil
		case "next":
			return m.commitEmbeddings()
		default:
			m.embFocus = (m.embFocus + 1) % len(rows)
			return m, m.applyEmbFocus()
		}
	}
	return m.updateEmbInputs(msg)
}

func (m wizardModel) updateEmbInputs(msg tea.Msg) (wizardModel, tea.Cmd) {
	rows := m.embRows()
	var cmd tea.Cmd
	switch rows[m.embFocus] {
	case "endpoint":
		m.embEndpoint, cmd = m.embEndpoint.Update(msg)
	case "model":
		m.embModel, cmd = m.embModel.Update(msg)
	case "api_key_env":
		m.embAPIKeyEnv, cmd = m.embAPIKeyEnv.Update(msg)
	case "batch":
		m.embBatch, cmd = m.embBatch.Update(msg)
	case "timeout":
		m.embTimeout, cmd = m.embTimeout.Update(msg)
	}
	m.errMsg = ""
	return m, cmd
}

func (m wizardModel) commitEmbeddings() (wizardModel, tea.Cmd) {
	emb := m.cfg.Embeddings
	emb.Enabled = m.embEnabled
	if m.embEnabled {
		batch, err := strconv.Atoi(strings.TrimSpace(m.embBatch.Value()))
		if err != nil {
			m.errMsg = "batch_size must be a whole number"
			return m, nil
		}
		timeout, err := time.ParseDuration(strings.TrimSpace(m.embTimeout.Value()))
		if err != nil {
			m.errMsg = "timeout must be a duration such as 60s"
			return m, nil
		}
		emb.Endpoint = strings.TrimSpace(m.embEndpoint.Value())
		emb.Model = strings.TrimSpace(m.embModel.Value())
		emb.APIKeyEnv = strings.TrimSpace(m.embAPIKeyEnv.Value())
		emb.BatchSize = batch
		emb.Timeout = config.Duration{Duration: timeout}
		emb.AllowPublicEndpoint = m.embAllowPublic
	} else {
		emb.Endpoint = ""
		emb.Model = ""
		emb.APIKeyEnv = ""
		emb.AllowPublicEndpoint = false
	}

	cand := m.cfg.Clone()
	cand.Embeddings = emb
	rendered, err := config.Render(cand)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.cfg.Embeddings = emb
	m.errMsg = ""
	m.reviewTOML = string(rendered)
	m.reviewLines = strings.Split(strings.TrimRight(m.reviewTOML, "\n"), "\n")
	m.reviewScroll = 0
	m.step = wizReview
	return m, nil
}

func (m wizardModel) updateReview(msg tea.Msg) (wizardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	maxScroll := max(0, len(m.reviewLines)-max(6, m.height-14))
	switch key.String() {
	case "esc", "b":
		m.step = wizEmbeddings
		m.errMsg = ""
		return m, m.applyEmbFocus()
	case "up", "k":
		if m.reviewScroll > 0 {
			m.reviewScroll--
		}
	case "down", "j":
		if m.reviewScroll < maxScroll {
			m.reviewScroll++
		}
	case "pgup":
		m.reviewScroll = max(0, m.reviewScroll-max(1, m.height-14))
	case "pgdown":
		m.reviewScroll = min(maxScroll, m.reviewScroll+max(1, m.height-14))
	case "enter", "s":
		if err := config.CheckDataDirOverlap(m.cfg.Storage.DataDir, m.cfg.Collections); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if err := config.Save(m.cfgPath, &m.cfg); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		loaded, err := config.Load(m.cfgPath)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if m.store != nil {
			m.store.Close()
			m.store = nil
		}
		st, err := store.Open(loaded.Storage.DataDir)
		if err != nil {
			m.errMsg = "saved config, but failed to open store: " + err.Error()
			return m, nil
		}
		m.cfg = *loaded
		m.store = st
		m.cfgExists = true
		m.errMsg = ""
		m.infoMsg = fmt.Sprintf("Saved %s (mode 0600) and initialized %s", m.cfgPath, filepath.Join(loaded.Storage.DataDir, "roost.db"))
		m.step = wizFinish
		return m, func() tea.Msg { return wizardSavedConfigMsg{cfg: loaded, st: st} }
	}
	return m, nil
}

func (m wizardModel) updateFinish(msg tea.Msg) (wizardModel, tea.Cmd) {
	if m.tokModal != nil {
		switch res := msg.(type) {
		case tokenCanceledMsg:
			m.tokModal = nil
			return m, nil
		case tokenDoneMsg:
			m.lastSecret = res.secret
			m.lastRec = res.rec
			m.tokModal = nil
			m.errMsg = ""
			return m, nil
		}
		tm, cmd := m.tokModal.Update(msg, m.ctx, m.store)
		m.tokModal = &tm
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "t":
		if m.store == nil {
			m.errMsg = "store is not open"
			return m, nil
		}
		tm := newTokenCreator("default", m.cfg.CollectionNames(), m.styles, m.width)
		m.tokModal = &tm
		return m, tm.applyFocus()
	case "i":
		if m.indexing || m.store == nil {
			return m, nil
		}
		m.indexing = true
		m.errMsg = ""
		m.indexReport = ""
		return m, tea.Batch(m.spin.Tick, runIndexCmd(m.ctx, m.indexFn, &m.cfg, m.store))
	case "d", "e":
		if m.indexing || m.store == nil {
			return m, nil
		}
		targetTab := tabCollections
		if key.String() == "e" {
			targetTab = tabExplore
		}
		cfgCopy := m.cfg.Clone()
		st := m.store
		return m, func() tea.Msg { return switchToDashboardMsg{cfg: cfgCopy, st: st, tab: targetTab} }
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m wizardModel) View() string {
	stepTitles := []string{
		"1. Welcome",
		"2. Storage",
		"3. Collections",
		"4. Server",
		"5. Embeddings",
		"6. Save Config",
		"7. Finish & Token",
	}

	var hdr strings.Builder
	hdr.WriteString(m.styles.title.Render("Roost Setup Wizard"))
	hdr.WriteString("  " + m.styles.muted.Render(m.cfgPath))
	hdr.WriteString("\n")
	var crumbs []string
	for i, t := range stepTitles {
		if wizardStep(i) == m.step {
			crumbs = append(crumbs, m.styles.tabActive.Render(t))
		} else {
			crumbs = append(crumbs, m.styles.muted.Render(t))
		}
	}
	hdr.WriteString(strings.Join(crumbs, " "))
	hdr.WriteString("\n" + m.styles.headerRule.Render(strings.Repeat("─", max(20, min(96, m.width-2)))) + "\n")

	var body string
	switch m.step {
	case wizWelcome:
		body = m.viewWelcome()
	case wizStorage:
		body = m.viewStorage()
	case wizCollections:
		body = m.viewCollections()
	case wizServer:
		body = m.viewServer()
	case wizEmbeddings:
		body = m.viewEmbeddings()
	case wizReview:
		body = m.viewReview()
	case wizFinish:
		body = m.viewFinish()
	}

	if m.errMsg != "" {
		body += "\n" + m.styles.err.Render("Error: "+m.errMsg)
	}
	return hdr.String() + body
}

func (m wizardModel) viewWelcome() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Guided setup for Roost"))
	b.WriteString("\n\n")
	b.WriteString("Roost indexes local documents (PDF, Word, Excel, PowerPoint, text) and\n")
	b.WriteString("serves them to AI tools over MCP and REST with strict security defaults:\n\n")
	b.WriteString("  • Zero outbound network calls unless you explicitly enable embeddings\n")
	b.WriteString("  • Sandboxed document extraction in memory-capped child processes\n")
	b.WriteString("  • 256-bit random API tokens (SHA-256 hashed in SQLite) per collection\n")
	b.WriteString("  • Fail-closed access audit log\n\n")
	b.WriteString(m.styles.label.Render("Config destination: ") + m.styles.accent.Render(m.cfgPath) + "\n")
	if m.cfgExists {
		b.WriteString(m.styles.warn.Render("Note: this file already exists; you can review changes before saving.") + "\n")
	}
	b.WriteString("\n" + m.styles.help.Render("Enter begin setup  •  q/Esc quit"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewStorage() string {
	if m.storagePicker != nil {
		return m.storagePicker.View()
	}
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Step 2: Where should Roost store its search index & token DB?"))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render("Hold document text in plaintext (created with mode 0700). Avoid placing inside a collection."))
	b.WriteString("\n\n")

	prefix := func(idx int) string {
		if m.storageFocus == idx {
			return m.styles.cursor.Render("> ")
		}
		return "  "
	}

	b.WriteString(prefix(0) + m.styles.label.Render("Storage path (data_dir): ") + m.dataDirInput.View() + "\n")
	if raw := strings.TrimSpace(m.dataDirInput.Value()); raw != "" {
		dbPath := filepath.Join(expandHome(raw), "roost.db")
		if _, err := os.Stat(dbPath); err == nil {
			b.WriteString("    " + m.styles.ok.Render("Existing database found: "+dbPath) + "\n")
		} else {
			b.WriteString("    " + m.styles.muted.Render("Will create "+dbPath+" (mode 0600) on finish") + "\n")
		}
	}
	b.WriteString("\n")

	browseBtn := "[ Browse folders... (Ctrl+B) ]"
	if m.storageFocus == 1 {
		browseBtn = m.styles.selected.Render(browseBtn)
	} else {
		browseBtn = m.styles.accent.Render(browseBtn)
	}
	b.WriteString(prefix(1) + browseBtn + "\n\n")

	nextBtn := "[ Next: Collections -> ]"
	if m.storageFocus == 2 {
		nextBtn = m.styles.selected.Render(nextBtn)
	} else {
		nextBtn = m.styles.ok.Render(nextBtn)
	}
	b.WriteString(prefix(2) + nextBtn + "\n\n")
	b.WriteString(m.styles.help.Render("Tab/↑/↓ move  •  Ctrl+B browse folders  •  Enter confirm/next  •  Esc back"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewCollections() string {
	if m.colEd != nil {
		return m.colEd.View()
	}
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Step 3: Add Document Collections"))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render("Each collection maps a folder path to a name used for token access control."))
	b.WriteString("\n\n")

	if len(m.cfg.Collections) == 0 {
		b.WriteString(m.styles.warn.Render("No collections added yet. Press 'a' (or Enter) to pick your first folder.") + "\n")
	} else {
		for i, c := range m.cfg.Collections {
			line := fmt.Sprintf("%-18s  %s", c.Name, c.Path)
			if len(c.Exclude) > 0 {
				line += fmt.Sprintf("  (exclude: %s)", joinExcludes(c.Exclude))
			}
			line = truncateText(line, max(36, m.width-10))
			if i == m.colCursor {
				b.WriteString(m.styles.selected.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
	}

	b.WriteString("\n" + m.styles.help.Render("a add folder  •  e edit  •  d delete  •  n / Enter continue  •  Esc back"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewServer() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Step 4: HTTP / MCP Server Listener ([server])"))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render("Roost refuses cleartext on non-loopback interfaces unless TLS or allow_plaintext is chosen."))
	b.WriteString("\n\n")

	rows := m.serverRows()
	cur := rows[m.srvFocus]
	pLabels := []struct {
		id    int
		title string
	}{
		{0, "Loopback only (127.0.0.1:8484) — safest for local clients"},
		{1, "LAN / Reverse proxy (0.0.0.0:8484, allow_plaintext = true)"},
		{2, "LAN / Direct HTTPS TLS (provide cert & key files)"},
		{3, "Custom listen address & TLS options"},
	}

	prefix := func(match bool) string {
		if match {
			return m.styles.cursor.Render("> ")
		}
		return "  "
	}

	for _, p := range pLabels {
		mark := "( )"
		if m.srvPreset == p.id {
			mark = "(*)"
		}
		key := fmt.Sprintf("preset:%d", p.id)
		b.WriteString(prefix(cur == key) + mark + " " + p.title + "\n")
	}
	b.WriteString("\n")

	if m.srvPreset != 0 {
		b.WriteString(prefix(cur == "listen") + m.styles.label.Render("listen:         ") + m.srvListen.View() + "\n")
	}
	if m.srvPreset == 3 {
		box := "[ ]"
		if m.srvPlaintext {
			box = "[x]"
		}
		b.WriteString(prefix(cur == "plaintext") + box + " allow_plaintext (use only behind TLS proxy)\n")
	}
	if m.srvPreset == 2 || m.srvPreset == 3 {
		b.WriteString(prefix(cur == "cert") + m.styles.label.Render("tls_cert:       ") + m.srvCert.View() + "\n")
		b.WriteString(prefix(cur == "key") + m.styles.label.Render("tls_key:        ") + m.srvKey.View() + "\n")
	}
	b.WriteString("\n")

	nextBtn := "[ Next: Embeddings -> ]"
	if cur == "next" {
		nextBtn = m.styles.selected.Render(nextBtn)
	} else {
		nextBtn = m.styles.ok.Render(nextBtn)
	}
	b.WriteString(prefix(cur == "next") + nextBtn + "\n\n")
	b.WriteString(m.styles.help.Render("Tab/↑/↓ move  •  Space toggle/select  •  Enter next  •  Esc back"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewEmbeddings() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Step 5: Optional Semantic Embeddings ([embeddings])"))
	b.WriteString("\n")
	b.WriteString(m.styles.warn.Render("Security note: Embeddings is the ONLY feature in Roost that makes outbound calls (sending chunks to your endpoint)."))
	b.WriteString("\n\n")

	rows := m.embRows()
	cur := rows[m.embFocus]
	prefix := func(match bool) string {
		if match {
			return m.styles.cursor.Render("> ")
		}
		return "  "
	}

	box := "[ ]"
	if m.embEnabled {
		box = "[x]"
	}
	b.WriteString(prefix(cur == "enabled") + box + " Enable outbound vector embeddings (leave unchecked for offline BM25 only)\n\n")

	if m.embEnabled {
		b.WriteString(prefix(cur == "endpoint") + m.styles.label.Render("endpoint:              ") + m.embEndpoint.View() + "\n")
		b.WriteString(prefix(cur == "model") + m.styles.label.Render("model:                 ") + m.embModel.View() + "\n")
		b.WriteString(prefix(cur == "api_key_env") + m.styles.label.Render("api_key_env:           ") + m.embAPIKeyEnv.View() + "\n")
		b.WriteString(prefix(cur == "batch") + m.styles.label.Render("batch_size:            ") + m.embBatch.View() + "\n")
		b.WriteString(prefix(cur == "timeout") + m.styles.label.Render("timeout:               ") + m.embTimeout.View() + "\n")
		pubBox := "[ ]"
		if m.embAllowPublic {
			pubBox = "[x]"
		}
		b.WriteString(prefix(cur == "allow_public") + pubBox + " allow_public_endpoint (private/loopback IPs only by default)\n\n")
	}

	nextBtn := "[ Next: Review & Save -> ]"
	if cur == "next" {
		nextBtn = m.styles.selected.Render(nextBtn)
	} else {
		nextBtn = m.styles.ok.Render(nextBtn)
	}
	b.WriteString(prefix(cur == "next") + nextBtn + "\n\n")
	b.WriteString(m.styles.help.Render("Tab/↑/↓ move  •  Space toggle  •  Enter next  •  Esc back"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewReview() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Step 6: Review Generated Configuration -> " + m.cfgPath))
	b.WriteString("\n")
	if m.cfgExists {
		b.WriteString(m.styles.warn.Render("Warning: " + m.cfgPath + " already exists and will be replaced atomically (mode 0600)."))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	rows := max(6, m.height-14)
	end := min(len(m.reviewLines), m.reviewScroll+rows)
	snippet := strings.Join(m.reviewLines[m.reviewScroll:end], "\n")
	b.WriteString(m.styles.codeBox.Width(max(40, m.width-10)).Render(snippet))
	b.WriteString("\n")
	if len(m.reviewLines) > rows {
		b.WriteString(m.styles.muted.Render(fmt.Sprintf("Lines %d–%d of %d", m.reviewScroll+1, end, len(m.reviewLines))) + "\n")
	}
	b.WriteString("\n" + m.styles.ok.Render("Press Enter or 's' to save "+m.cfgPath+" and initialize roost.db"))
	b.WriteString("\n" + m.styles.help.Render("↑/↓/PgUp/PgDn scroll  •  Enter/s save config  •  Esc/b back"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m wizardModel) viewFinish() string {
	if m.tokModal != nil {
		return m.styles.panel.Width(max(44, m.width-4)).Render(
			m.styles.title.Render("Create First API Token") + "\n\n" +
				m.tokModal.ViewForm(false, nil, "") + "\n\n" +
				m.styles.help.Render("Tab/↑/↓ move  •  Space toggle  •  Enter submit  •  Esc cancel"),
		)
	}

	var b strings.Builder
	b.WriteString(m.styles.ok.Render(m.infoMsg) + "\n\n")

	if m.lastSecret != "" {
		b.WriteString(m.styles.warn.Render(fmt.Sprintf("First token %q (ID %s) — copy now, shown ONCE:", m.lastRec.Name, m.lastRec.ID)))
		b.WriteString("\n")
		b.WriteString(m.styles.secretBox.Render(m.lastSecret))
		b.WriteString("\n\n")
	} else {
		b.WriteString(m.styles.muted.Render("No API token created yet. Press 't' to issue your first token now.") + "\n\n")
	}

	if m.indexing {
		b.WriteString(m.spin.View() + " Indexing configured collections...\n\n")
	} else if m.indexReport != "" {
		b.WriteString(m.styles.ok.Render(m.indexReport) + "\n\n")
	}

	b.WriteString(m.styles.label.Render("Next commands & AI client setup:") + "\n")
	b.WriteString(m.styles.codeBox.Width(max(40, m.width-10)).Render(clientSnippets(&m.cfg, m.cfgPath, m.lastSecret)))
	b.WriteString("\n\n")
	b.WriteString(m.styles.help.Render("t create token  •  i index documents now  •  e test search  •  d open configurator  •  q quit"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}
