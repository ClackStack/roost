package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/ClackStack/roost/internal/auth"
	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/store"
)

type dashTab int

const (
	tabCollections dashTab = iota
	tabTokens
	tabStatus
	tabAudit
	tabSettings
	tabExplore
)

var dashTabNames = []string{
	"1 Collections",
	"2 Tokens",
	"3 Status",
	"4 Audit",
	"5 Settings",
	"6 Explore",
}

type storeReopenedMsg struct {
	cfg *config.Config
	st  *store.Store
}

type settingKind int

const (
	kindString settingKind = iota
	kindDir
	kindBool
	kindInt
	kindDuration
)

type settingSpec struct {
	section string
	key     string
	hint    string
	kind    settingKind
	get     func(*config.Config) string
	toggle  func(*config.Config)
	set     func(*config.Config, string) error
}

func allSettings() []settingSpec {
	return []settingSpec{
		{
			section: "server", key: "listen", hint: "host:port (non-loopback needs TLS or allow_plaintext)", kind: kindString,
			get: func(c *config.Config) string { return c.Server.Listen },
			set: func(c *config.Config, v string) error { c.Server.Listen = v; return nil },
		},
		{
			section: "server", key: "tls_cert", hint: "PEM certificate path (paired with tls_key)", kind: kindString,
			get: func(c *config.Config) string { return c.Server.TLSCert },
			set: func(c *config.Config, v string) error { c.Server.TLSCert = v; return nil },
		},
		{
			section: "server", key: "tls_key", hint: "PEM private key path (paired with tls_cert)", kind: kindString,
			get: func(c *config.Config) string { return c.Server.TLSKey },
			set: func(c *config.Config, v string) error { c.Server.TLSKey = v; return nil },
		},
		{
			section: "server", key: "allow_plaintext", hint: "allow non-loopback HTTP without TLS (behind TLS proxy only)", kind: kindBool,
			get:    func(c *config.Config) string { return strconv.FormatBool(c.Server.AllowPlaintext) },
			toggle: func(c *config.Config) { c.Server.AllowPlaintext = !c.Server.AllowPlaintext },
		},
		{
			section: "storage", key: "data_dir", hint: "SQLite index & token DB directory (Ctrl+B / b to browse)", kind: kindDir,
			get: func(c *config.Config) string { return c.Storage.DataDir },
			set: func(c *config.Config, v string) error {
				if v == "" {
					return fmt.Errorf("data_dir must not be empty")
				}
				c.Storage.DataDir = expandHome(v)
				return nil
			},
		},
		{
			section: "index", key: "rescan_interval", hint: "periodic full walk interval (e.g. 15m)", kind: kindDuration,
			get: func(c *config.Config) string { return c.Index.RescanInterval.Duration.String() },
			set: func(c *config.Config, v string) error {
				d, err := time.ParseDuration(v)
				if err != nil {
					return err
				}
				c.Index.RescanInterval = config.Duration{Duration: d}
				return nil
			},
		},
		{
			section: "index", key: "watch", hint: "enable fsnotify watcher on local directories", kind: kindBool,
			get:    func(c *config.Config) string { return strconv.FormatBool(c.Index.Watch) },
			toggle: func(c *config.Config) { c.Index.Watch = !c.Index.Watch },
		},
		{
			section: "index", key: "watch_debounce", hint: "wait before re-indexing modified file (e.g. 5s)", kind: kindDuration,
			get: func(c *config.Config) string { return c.Index.WatchDebounce.Duration.String() },
			set: func(c *config.Config, v string) error {
				d, err := time.ParseDuration(v)
				if err != nil {
					return err
				}
				c.Index.WatchDebounce = config.Duration{Duration: d}
				return nil
			},
		},
		{
			section: "index", key: "workers", hint: "concurrent extraction worker processes", kind: kindInt,
			get: func(c *config.Config) string { return strconv.Itoa(c.Index.Workers) },
			set: func(c *config.Config, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil {
					return err
				}
				c.Index.Workers = n
				return nil
			},
		},
		{
			section: "index", key: "max_file_mb", hint: "maximum file size indexed before flagging too_large", kind: kindInt,
			get: func(c *config.Config) string { return strconv.FormatInt(c.Index.MaxFileMB, 10) },
			set: func(c *config.Config, v string) error {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return err
				}
				c.Index.MaxFileMB = n
				return nil
			},
		},
		{
			section: "index", key: "max_text_mb", hint: "maximum extracted text kept per document", kind: kindInt,
			get: func(c *config.Config) string { return strconv.FormatInt(c.Index.MaxTextMB, 10) },
			set: func(c *config.Config, v string) error {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return err
				}
				c.Index.MaxTextMB = n
				return nil
			},
		},
		{
			section: "index", key: "extract_timeout", hint: "per-file sandbox timeout (e.g. 2m)", kind: kindDuration,
			get: func(c *config.Config) string { return c.Index.ExtractTimeout.Duration.String() },
			set: func(c *config.Config, v string) error {
				d, err := time.ParseDuration(v)
				if err != nil {
					return err
				}
				c.Index.ExtractTimeout = config.Duration{Duration: d}
				return nil
			},
		},
		{
			section: "index", key: "worker_memory_mb", hint: "RLIMIT memory cap per extraction worker", kind: kindInt,
			get: func(c *config.Config) string { return strconv.FormatInt(c.Index.WorkerMemoryMB, 10) },
			set: func(c *config.Config, v string) error {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return err
				}
				c.Index.WorkerMemoryMB = n
				return nil
			},
		},
		{
			section: "audit", key: "log_queries", hint: "record search query text in audit log", kind: kindBool,
			get:    func(c *config.Config) string { return strconv.FormatBool(c.Audit.LogQueries) },
			toggle: func(c *config.Config) { c.Audit.LogQueries = !c.Audit.LogQueries },
		},
		{
			section: "audit", key: "jsonl_path", hint: "optional append-only JSONL audit file path (SIGHUP rotates)", kind: kindString,
			get: func(c *config.Config) string { return c.Audit.JSONLPath },
			set: func(c *config.Config, v string) error { c.Audit.JSONLPath = v; return nil },
		},
		{
			section: "embeddings", key: "enabled", hint: "OUTBOUND TRAFFIC: send chunk text to configured OpenAI-compatible API", kind: kindBool,
			get:    func(c *config.Config) string { return strconv.FormatBool(c.Embeddings.Enabled) },
			toggle: func(c *config.Config) { c.Embeddings.Enabled = !c.Embeddings.Enabled },
		},
		{
			section: "embeddings", key: "endpoint", hint: "e.g. http://127.0.0.1:11434/v1/embeddings", kind: kindString,
			get: func(c *config.Config) string { return c.Embeddings.Endpoint },
			set: func(c *config.Config, v string) error { c.Embeddings.Endpoint = v; return nil },
		},
		{
			section: "embeddings", key: "model", hint: "embedding model name (e.g. nomic-embed-text)", kind: kindString,
			get: func(c *config.Config) string { return c.Embeddings.Model },
			set: func(c *config.Config, v string) error { c.Embeddings.Model = v; return nil },
		},
		{
			section: "embeddings", key: "api_key_env", hint: "environment variable holding optional bearer key", kind: kindString,
			get: func(c *config.Config) string { return c.Embeddings.APIKeyEnv },
			set: func(c *config.Config, v string) error { c.Embeddings.APIKeyEnv = v; return nil },
		},
		{
			section: "embeddings", key: "batch_size", hint: "chunks per embedding request", kind: kindInt,
			get: func(c *config.Config) string { return strconv.Itoa(c.Embeddings.BatchSize) },
			set: func(c *config.Config, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil {
					return err
				}
				c.Embeddings.BatchSize = n
				return nil
			},
		},
		{
			section: "embeddings", key: "timeout", hint: "embedding HTTP request timeout (e.g. 60s)", kind: kindDuration,
			get: func(c *config.Config) string { return c.Embeddings.Timeout.Duration.String() },
			set: func(c *config.Config, v string) error {
				d, err := time.ParseDuration(v)
				if err != nil {
					return err
				}
				c.Embeddings.Timeout = config.Duration{Duration: d}
				return nil
			},
		},
		{
			section: "embeddings", key: "allow_public_endpoint", hint: "permit non-private embedding endpoints (off by default)", kind: kindBool,
			get:    func(c *config.Config) string { return strconv.FormatBool(c.Embeddings.AllowPublicEndpoint) },
			toggle: func(c *config.Config) { c.Embeddings.AllowPublicEndpoint = !c.Embeddings.AllowPublicEndpoint },
		},
	}
}

const auditPollInterval = 1500 * time.Millisecond

type auditTickMsg struct {
	seq int
}

var probStatusFilters = []string{
	"",
	store.StatusFailed,
	store.StatusEncrypted,
	store.StatusNoText,
	store.StatusTooLarge,
	store.StatusUnsupported,
	"truncated",
}

func probFilterLabel(code string) string {
	if code == "" {
		return "all"
	}
	return code
}

var auditOutcomeFilters = []string{"", "ok", "non-ok"}

func auditOutcomeLabel(code string) string {
	if code == "" {
		return "all"
	}
	return code
}

func auditTickCmd(seq int) tea.Cmd {
	return tea.Tick(auditPollInterval, func(time.Time) tea.Msg {
		return auditTickMsg{seq: seq}
	})
}

func auditEntrySig(e store.AuditEntry) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		e.Time.UnixNano(), e.TokenID, e.Op, e.Outcome, e.Query, strings.Join(e.DocIDs, ","))
}

type dashboardModel struct {
	ctx     context.Context
	cfgPath string
	cfg     *config.Config
	store   *store.Store
	indexFn IndexFunc
	vecFn   VectorFunc
	styles  styles
	width   int
	height  int

	tab           dashTab
	restartNeeded bool
	flashMsg      string
	flashErr      string

	// Cached data from SQLite
	statusMap    map[string]*store.CollectionStatus
	lastScans    map[string]string
	orphans      []string
	problems     []store.Document
	embMissing   int
	embTotal     int
	tokens       []store.TokenRecord
	auditEntries []store.AuditEntry

	// Collections tab state
	colCursor       int
	colEd           *collectionEditor
	rmColConfirm    bool
	rmColAlsoPrune  bool
	pruneOrphanConf bool

	// Tokens tab state
	tokCursor  int
	tokScroll  int
	tokModal   *tokenCreator
	revokeConf bool

	// Status tab state
	probCursor       int
	probScroll       int
	probFilterIdx    int
	probSearchOn     bool
	probSearchIn     textinput.Model
	filteredProblems []store.Document
	probDetail       *store.Document
	probDetailScroll int
	indexing         bool
	spin             spinner.Model
	indexReport      string

	// Audit tab state
	auditCursor           int
	auditScroll           int
	auditFilterOn         bool
	auditFilterIn         textinput.Model
	auditOutcomeFilterIdx int
	filteredAudit         []store.AuditEntry
	auditFollow           bool
	auditTickSeq          int
	auditDetail           *store.AuditEntry
	auditDetailDocs       []string
	auditDetailScroll     int

	// Settings tab state
	settingsSpecs []settingSpec
	draftCfg      *config.Config
	settingsDirty bool
	setCursor     int
	setScroll     int
	setEditing    bool
	setInput      textinput.Model
	setPicker     *folderPickerModel
	tomlPreview   []string
	tomlScroll    int

	// Explore tab state
	expl exploreModel
}

func newDashboardModel(ctx context.Context, cfgPath string, cfg *config.Config, st *store.Store, indexFn IndexFunc, vecFn VectorFunc, stStyles styles, width, height int) dashboardModel {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m := dashboardModel{
		ctx:           ctx,
		cfgPath:       cfgPath,
		cfg:           cfg.Clone(),
		store:         st,
		indexFn:       indexFn,
		vecFn:         vecFn,
		styles:        stStyles,
		width:         width,
		height:        height,
		spin:          sp,
		probSearchIn:  newTextInput("filter collection, path, status, or error...", "", max(24, width-20), stStyles.isDark),
		auditFilterIn: newTextInput("filter token, op, outcome, doc ID, or query...", "", max(24, width-20), stStyles.isDark),
		settingsSpecs: allSettings(),
		draftCfg:      cfg.Clone(),
		setInput:      newTextInput("", "", max(24, width-20), stStyles.isDark),
		expl:          newExploreModel(ctx, cfg, st, vecFn, stStyles, width, height),
	}
	m.refreshAll()
	return m
}

func (m *dashboardModel) setSize(width, height int, st styles) {
	m.width = width
	m.height = height
	m.styles = st
	iw := max(24, width-20)
	m.probSearchIn.SetWidth(iw)
	m.probSearchIn.SetStyles(textinput.DefaultStyles(st.isDark))
	m.auditFilterIn.SetWidth(iw)
	m.auditFilterIn.SetStyles(textinput.DefaultStyles(st.isDark))
	m.setInput.SetWidth(iw)
	m.setInput.SetStyles(textinput.DefaultStyles(st.isDark))
	m.expl.setSize(width, height, st)
	if m.colEd != nil {
		m.colEd.setSize(width, height, st)
	}
	if m.tokModal != nil {
		m.tokModal.setSize(width, st)
	}
	if m.setPicker != nil {
		m.setPicker.setSize(width, height, st)
	}
}

func (m *dashboardModel) refreshAll() {
	m.refreshStatus()
	m.refreshTokens()
	m.refreshAudit()
	m.expl.sync(m.cfg, m.store, m.tab == tabExplore)
}

func (m *dashboardModel) refreshStatus() {
	if m.store == nil {
		return
	}
	var errs []string
	if stMap, err := m.store.Status(m.ctx); err != nil {
		errs = append(errs, err.Error())
	} else {
		m.statusMap = stMap
	}
	scans := map[string]string{}
	for _, name := range m.cfg.CollectionNames() {
		if v, err := m.store.GetMeta(m.ctx, "last_scan:"+name); err == nil && v != "" {
			scans[name] = v
		}
	}
	m.lastScans = scans
	if orphans, err := m.store.OrphanCollections(m.ctx, m.cfg.CollectionNames()); err == nil {
		m.orphans = orphans
	}
	if probs, err := m.store.Problems(m.ctx, 500); err == nil {
		m.problems = probs
		m.applyProblemFilter()
	}
	if m.cfg.Embeddings.Enabled {
		if missing, total, err := m.store.EmbeddingProgress(m.ctx); err == nil {
			m.embMissing, m.embTotal = missing, total
		}
	}
	if len(errs) > 0 {
		m.flashErr = strings.Join(errs, "; ")
	}
}

func (m *dashboardModel) applyProblemFilter() {
	statusCode := probStatusFilters[m.probFilterIdx]
	q := strings.ToLower(strings.TrimSpace(m.probSearchIn.Value()))
	var out []store.Document
	for _, d := range m.problems {
		if !matchesProblemStatus(d, statusCode) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(d.Collection + "/" + d.RelPath + " " + d.Title + " " + d.FileType + " " + d.Status + " " + d.Error + " " + d.ID)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, d)
	}
	m.filteredProblems = out
	if m.probCursor >= len(m.filteredProblems) {
		m.probCursor = max(0, len(m.filteredProblems)-1)
	}
	if m.probScroll > m.probCursor {
		m.probScroll = m.probCursor
	}
}

func matchesProblemStatus(d store.Document, code string) bool {
	switch code {
	case "":
		return true
	case "truncated":
		return d.Truncated
	default:
		return d.Status == code
	}
}

func (m *dashboardModel) refreshTokens() {
	if m.store == nil {
		return
	}
	toks, err := m.store.ListTokens(m.ctx)
	if err != nil {
		m.flashErr = "list tokens: " + err.Error()
		return
	}
	m.tokens = toks
	if m.tokCursor >= len(m.tokens) {
		m.tokCursor = max(0, len(m.tokens)-1)
	}
}

func (m *dashboardModel) refreshAudit() {
	if m.store == nil {
		return
	}
	var prevSig string
	if m.auditCursor > 0 && m.auditCursor < len(m.filteredAudit) {
		prevSig = auditEntrySig(m.filteredAudit[m.auditCursor])
	}
	entries, err := m.store.RecentAudit(m.ctx, 500)
	if err != nil {
		m.flashErr = "read audit: " + err.Error()
		return
	}
	m.auditEntries = entries
	m.applyAuditFilter()
	if prevSig != "" {
		for i, e := range m.filteredAudit {
			if auditEntrySig(e) == prevSig {
				m.auditCursor = i
				rows := max(5, m.height-17)
				if m.auditCursor < m.auditScroll {
					m.auditScroll = m.auditCursor
				} else if m.auditCursor >= m.auditScroll+rows {
					m.auditScroll = m.auditCursor - rows + 1
				}
				break
			}
		}
	}
}

func (m *dashboardModel) applyAuditFilter() {
	outcomeCode := auditOutcomeFilters[m.auditOutcomeFilterIdx]
	q := strings.ToLower(strings.TrimSpace(m.auditFilterIn.Value()))
	var out []store.AuditEntry
	for _, e := range m.auditEntries {
		if outcomeCode == "ok" && e.Outcome != "ok" {
			continue
		}
		if outcomeCode == "non-ok" && e.Outcome == "ok" {
			continue
		}
		if q != "" {
			hay := strings.ToLower(e.TokenName + " " + e.TokenID + " " + e.Transport + " " + e.Op + " " + e.Outcome + " " + e.Query + " " + e.Remote + " " + strings.Join(e.DocIDs, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, e)
	}
	m.filteredAudit = out
	if m.auditCursor >= len(m.filteredAudit) {
		m.auditCursor = max(0, len(m.filteredAudit)-1)
	}
	if m.auditScroll > m.auditCursor {
		m.auditScroll = m.auditCursor
	}
}

func (m dashboardModel) hasOverlay() bool {
	return m.colEd != nil || m.rmColConfirm || m.pruneOrphanConf ||
		m.tokModal != nil || m.revokeConf ||
		m.probSearchOn || m.probDetail != nil ||
		m.auditFilterOn || m.auditDetail != nil ||
		m.setEditing || m.setPicker != nil || len(m.tomlPreview) > 0 ||
		(m.tab == tabExplore && m.expl.hasOverlay())
}

func (m dashboardModel) switchTab(next dashTab) (dashboardModel, tea.Cmd) {
	if m.tab == tabAudit && next != tabAudit && m.auditFollow {
		// Invalidate active polling ticks while off the Audit tab; re-arm on return.
		m.auditTickSeq++
	}
	m.tab = next
	switch next {
	case tabTokens:
		m.refreshTokens()
	case tabStatus:
		m.refreshStatus()
	case tabAudit:
		m.refreshAudit()
		if m.auditFollow {
			m.auditTickSeq++
			return m, auditTickCmd(m.auditTickSeq)
		}
	case tabExplore:
		m.expl.sync(m.cfg, m.store, true)
	}
	return m, nil
}

func (m dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.indexing {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case auditTickMsg:
		if !m.auditFollow || m.tab != tabAudit || msg.seq != m.auditTickSeq {
			return m, nil
		}
		m.refreshAudit()
		return m, auditTickCmd(m.auditTickSeq)
	case indexDoneMsg:
		m.indexing = false
		m.indexReport = formatIndexSummary(msg.stats, msg.err)
		m.refreshStatus()
		if msg.err != nil && len(msg.stats) == 0 {
			m.flashErr = msg.err.Error()
		} else {
			m.flashErr = ""
			m.flashMsg = m.indexReport
		}
		return m, nil
	}

	if m.colEd != nil {
		return m.updateCollectionModal(msg)
	}
	if m.tokModal != nil {
		return m.updateTokenModal(msg)
	}
	if m.setPicker != nil {
		return m.updateSettingsPicker(msg)
	}
	if len(m.tomlPreview) > 0 {
		return m.updateTOMLPreview(msg)
	}
	if m.probDetail != nil {
		return m.updateProblemDetail(msg)
	}
	if m.auditDetail != nil {
		return m.updateAuditDetail(msg)
	}
	if m.rmColConfirm {
		return m.updateRemoveColConfirm(msg)
	}
	if m.pruneOrphanConf {
		return m.updatePruneOrphanConfirm(msg)
	}
	if m.revokeConf {
		return m.updateRevokeConfirm(msg)
	}
	if m.probSearchOn {
		return m.updateProbSearch(msg)
	}
	if m.auditFilterOn {
		return m.updateAuditFilter(msg)
	}
	if m.setEditing {
		return m.updateSettingEdit(msg)
	}
	if m.tab == tabExplore && m.expl.hasOverlay() {
		var cmd tea.Cmd
		m.expl, cmd = m.expl.Update(msg)
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "1":
		return m.switchTab(tabCollections)
	case "2":
		return m.switchTab(tabTokens)
	case "3":
		return m.switchTab(tabStatus)
	case "4":
		return m.switchTab(tabAudit)
	case "5":
		return m.switchTab(tabSettings)
	case "6":
		return m.switchTab(tabExplore)
	case "tab", "right":
		return m.switchTab((m.tab + 1) % 6)
	case "shift+tab", "left":
		return m.switchTab((m.tab + 5) % 6)
	}

	switch m.tab {
	case tabCollections:
		return m.updateCollectionsTab(key)
	case tabTokens:
		return m.updateTokensTab(key)
	case tabStatus:
		return m.updateStatusTab(key)
	case tabAudit:
		return m.updateAuditTab(key)
	case tabSettings:
		return m.updateSettingsTab(key)
	case tabExplore:
		var cmd tea.Cmd
		m.expl, cmd = m.expl.Update(key)
		return m, cmd
	}
	return m, nil
}

func (m dashboardModel) saveConfig(next *config.Config, successNote string) (dashboardModel, tea.Cmd, bool) {
	if err := config.CheckDataDirOverlap(next.Storage.DataDir, next.Collections); err != nil {
		m.flashErr = err.Error()
		m.flashMsg = ""
		return m, nil, false
	}
	if err := config.Save(m.cfgPath, next); err != nil {
		m.flashErr = err.Error()
		m.flashMsg = ""
		return m, nil, false
	}
	loaded, err := config.Load(m.cfgPath)
	if err != nil {
		m.flashErr = err.Error()
		m.flashMsg = ""
		return m, nil, false
	}

	var reopenCmd tea.Cmd
	if m.store == nil || loaded.Storage.DataDir != m.cfg.Storage.DataDir {
		newSt, err := store.Open(loaded.Storage.DataDir)
		if err != nil {
			m.flashErr = "config saved, but failed to open new data_dir: " + err.Error()
			m.cfg = loaded
			m.draftCfg = loaded.Clone()
			m.restartNeeded = true
			return m, nil, false
		}
		if m.store != nil {
			m.store.Close()
		}
		m.store = newSt
		cfgCopy := loaded.Clone()
		reopenCmd = func() tea.Msg { return storeReopenedMsg{cfg: cfgCopy, st: newSt} }
	}

	m.cfg = loaded
	m.draftCfg = loaded.Clone()
	m.settingsDirty = false
	m.restartNeeded = true
	m.flashErr = ""
	m.flashMsg = successNote
	m.refreshAll()
	return m, reopenCmd, true
}

func (m dashboardModel) updateCollectionModal(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch res := msg.(type) {
	case collectionCanceledMsg:
		m.colEd = nil
		return m, nil
	case collectionSavedMsg:
		next := m.cfg.Clone()
		note := fmt.Sprintf("Added collection %q", res.col.Name)
		if res.editIndex >= 0 && res.editIndex < len(next.Collections) {
			oldName := next.Collections[res.editIndex].Name
			next.Collections[res.editIndex] = res.col
			if oldName != res.col.Name {
				note = fmt.Sprintf("Updated collection %q -> %q (old rows remain until pruned/re-indexed)", oldName, res.col.Name)
			} else {
				note = fmt.Sprintf("Updated collection %q", res.col.Name)
			}
		} else {
			next.Collections = append(next.Collections, res.col)
		}
		var cmd tea.Cmd
		var saved bool
		m, cmd, saved = m.saveConfig(next, note)
		if saved {
			if res.editIndex >= 0 {
				m.colCursor = res.editIndex
			} else {
				m.colCursor = len(m.cfg.Collections) - 1
			}
		}
		m.colEd = nil
		return m, cmd
	}
	ed, cmd := m.colEd.Update(msg)
	m.colEd = &ed
	return m, cmd
}

func (m dashboardModel) updateRemoveColConfirm(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "n":
		m.rmColConfirm = false
		m.rmColAlsoPrune = false
		return m, nil
	case "space", "x":
		m.rmColAlsoPrune = !m.rmColAlsoPrune
		return m, nil
	case "y", "enter":
		if m.colCursor < 0 || m.colCursor >= len(m.cfg.Collections) {
			m.rmColConfirm = false
			return m, nil
		}
		col := m.cfg.Collections[m.colCursor]
		next := m.cfg.Clone()
		next.Collections = slices.Delete(next.Collections, m.colCursor, m.colCursor+1)
		note := fmt.Sprintf("Removed collection %q from config", col.Name)
		prune := m.rmColAlsoPrune
		m.rmColConfirm = false
		m.rmColAlsoPrune = false
		var cmd tea.Cmd
		var saved bool
		m, cmd, saved = m.saveConfig(next, note)
		if saved {
			if m.colCursor >= len(m.cfg.Collections) {
				m.colCursor = max(0, len(m.cfg.Collections)-1)
			}
			if prune && m.store != nil {
				if err := m.store.DeleteCollection(m.ctx, col.Name); err != nil {
					m.flashErr = fmt.Sprintf("removed from config, but prune failed: %v", err)
				} else {
					m.flashMsg = fmt.Sprintf("Removed %q from config and deleted its indexed rows", col.Name)
					m.refreshStatus()
				}
			}
		}
		return m, cmd
	}
	return m, nil
}

func (m dashboardModel) updatePruneOrphanConfirm(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "n":
		m.pruneOrphanConf = false
	case "y", "enter":
		m.pruneOrphanConf = false
		count := len(m.orphans)
		for _, name := range m.orphans {
			if err := m.store.DeleteCollection(m.ctx, name); err != nil {
				m.flashErr = fmt.Sprintf("prune %s: %v", name, err)
				m.refreshStatus()
				return m, nil
			}
		}
		m.refreshStatus()
		m.flashErr = ""
		m.flashMsg = fmt.Sprintf("Pruned %d orphan collection(s) from database", count)
	}
	return m, nil
}

func (m dashboardModel) updateCollectionsTab(key tea.KeyPressMsg) (dashboardModel, tea.Cmd) {
	switch key.String() {
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
		m.flashErr = ""
		return m, ed.applyFocus()
	case "e", "enter":
		if len(m.cfg.Collections) > 0 && m.colCursor < len(m.cfg.Collections) {
			ed := newCollectionEditor(m.colCursor, m.cfg.Collections[m.colCursor], m.cfg.Storage.DataDir, m.cfg.Collections, m.styles, m.width, m.height)
			m.colEd = &ed
			m.flashErr = ""
			return m, ed.applyFocus()
		}
	case "d", "delete":
		if len(m.cfg.Collections) <= 1 {
			m.flashErr = "Roost requires at least one collection. Add another before removing this one."
			return m, nil
		}
		m.rmColConfirm = true
		m.rmColAlsoPrune = false
		m.flashErr = ""
	case "p":
		if len(m.orphans) > 0 {
			m.pruneOrphanConf = true
			m.flashErr = ""
		}
	case "R":
		m.refreshStatus()
		m.flashMsg = "Refreshed collections"
		m.flashErr = ""
	}
	return m, nil
}

func (m dashboardModel) updateTokenModal(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg.(type) {
	case tokenCanceledMsg:
		m.tokModal = nil
		m.refreshTokens()
		return m, nil
	case tokenDoneMsg:
		m.refreshTokens()
		m.flashErr = ""
		m.flashMsg = "Token created and active immediately"
		// Keep modal open while secret != "" so user can copy the secret & snippets.
		return m, nil
	}
	tm, cmd := m.tokModal.Update(msg, m.ctx, m.store)
	m.tokModal = &tm
	return m, cmd
}

func (m dashboardModel) updateRevokeConfirm(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "n":
		m.revokeConf = false
	case "y", "enter":
		m.revokeConf = false
		if m.tokCursor >= 0 && m.tokCursor < len(m.tokens) {
			tok := m.tokens[m.tokCursor]
			if err := m.store.RevokeToken(m.ctx, tok.ID); err != nil {
				m.flashErr = "revoke token: " + err.Error()
				return m, nil
			}
			m.refreshTokens()
			m.flashErr = ""
			m.flashMsg = fmt.Sprintf("Revoked token %s (%s) — takes effect immediately", tok.ID, tok.Name)
		}
	}
	return m, nil
}

func (m dashboardModel) updateTokensTab(key tea.KeyPressMsg) (dashboardModel, tea.Cmd) {
	rows := max(5, m.height-14)
	switch key.String() {
	case "up", "k":
		if m.tokCursor > 0 {
			m.tokCursor--
			if m.tokCursor < m.tokScroll {
				m.tokScroll = m.tokCursor
			}
		}
	case "down", "j":
		if m.tokCursor < len(m.tokens)-1 {
			m.tokCursor++
			if m.tokCursor >= m.tokScroll+rows {
				m.tokScroll = m.tokCursor - rows + 1
			}
		}
	case "n", "c", "a":
		tm := newTokenCreator("", m.cfg.CollectionNames(), m.styles, m.width)
		m.tokModal = &tm
		m.flashErr = ""
		return m, tm.applyFocus()
	case "r", "d":
		if len(m.tokens) == 0 || m.tokCursor >= len(m.tokens) {
			return m, nil
		}
		if m.tokens[m.tokCursor].RevokedAt != nil {
			m.flashErr = "Selected token is already revoked"
			return m, nil
		}
		m.revokeConf = true
		m.flashErr = ""
	case "R":
		m.refreshTokens()
		m.flashMsg = "Refreshed token list"
		m.flashErr = ""
	}
	return m, nil
}

func moveListCursor(cursor, scroll *int, total, rows int, key string) bool {
	if total <= 0 {
		*cursor, *scroll = 0, 0
		return key == "up" || key == "k" || key == "down" || key == "j" || key == "pgup" || key == "pgdown"
	}
	switch key {
	case "up", "k":
		if *cursor > 0 {
			*cursor--
		}
	case "down", "j":
		if *cursor < total-1 {
			*cursor++
		}
	case "pgup":
		*cursor = max(0, *cursor-rows)
	case "pgdown":
		*cursor = min(total-1, *cursor+rows)
	default:
		return false
	}
	if *cursor < *scroll {
		*scroll = *cursor
	} else if *cursor >= *scroll+rows {
		*scroll = *cursor - rows + 1
	}
	return true
}

func scrollLinesOffset(scroll *int, total, rows int, key string) bool {
	maxScroll := max(0, total-rows)
	switch key {
	case "up", "k":
		if *scroll > 0 {
			*scroll--
		}
	case "down", "j":
		if *scroll < maxScroll {
			*scroll++
		}
	case "pgup":
		*scroll = max(0, *scroll-rows)
	case "pgdown":
		*scroll = min(maxScroll, *scroll+rows)
	default:
		return false
	}
	return true
}

func updateFilterInput(in *textinput.Model, active *bool, apply func(), msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		apply()
		return cmd
	}
	switch key.String() {
	case "enter":
		*active = false
		in.Blur()
		apply()
		return nil
	case "esc":
		*active = false
		in.SetValue("")
		in.Blur()
		apply()
		return nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	apply()
	return cmd
}

func (m dashboardModel) startIndexAll() (dashboardModel, tea.Cmd) {
	if m.indexing || m.store == nil {
		return m, nil
	}
	m.probDetail = nil
	m.probDetailScroll = 0
	m.indexing = true
	m.flashErr = ""
	m.flashMsg = "Running indexing pass..."
	return m, tea.Batch(m.spin.Tick, runIndexCmd(m.ctx, m.indexFn, m.cfg, m.store))
}

func (m dashboardModel) startIndexCollection(name string) (dashboardModel, tea.Cmd) {
	if m.indexing || m.store == nil {
		return m, nil
	}
	col, ok := m.cfg.Collection(name)
	if !ok {
		m.flashErr = fmt.Sprintf("collection %q is not in %s", name, m.cfgPath)
		return m, nil
	}
	sub := m.cfg.Clone()
	sub.Collections = []config.Collection{col}
	m.probDetail = nil
	m.probDetailScroll = 0
	m.indexing = true
	m.flashErr = ""
	m.flashMsg = fmt.Sprintf("Indexing collection %q...", col.Name)
	return m, tea.Batch(m.spin.Tick, runIndexCmd(m.ctx, m.indexFn, sub, m.store))
}

func (m dashboardModel) updateProbSearch(msg tea.Msg) (dashboardModel, tea.Cmd) {
	cmd := updateFilterInput(&m.probSearchIn, &m.probSearchOn, m.applyProblemFilter, msg)
	return m, cmd
}

func (m dashboardModel) updateStatusTab(key tea.KeyPressMsg) (dashboardModel, tea.Cmd) {
	rows := max(4, m.height-18)
	if moveListCursor(&m.probCursor, &m.probScroll, len(m.filteredProblems), rows, key.String()) {
		return m, nil
	}
	switch key.String() {
	case "enter":
		if len(m.filteredProblems) > 0 && m.probCursor < len(m.filteredProblems) {
			d := m.filteredProblems[m.probCursor]
			m.probDetail = &d
			m.probDetailScroll = 0
		}
	case "/":
		m.probSearchOn = true
		m.probSearchIn.CursorEnd()
		return m, m.probSearchIn.Focus()
	case "s":
		m.probFilterIdx = (m.probFilterIdx + 1) % len(probStatusFilters)
		m.probCursor = 0
		m.probScroll = 0
		m.applyProblemFilter()
	case "esc":
		if m.probSearchIn.Value() != "" || m.probFilterIdx != 0 {
			m.probSearchIn.SetValue("")
			m.probFilterIdx = 0
			m.applyProblemFilter()
		}
	case "c":
		if len(m.filteredProblems) > 0 && m.probCursor < len(m.filteredProblems) {
			return m.startIndexCollection(m.filteredProblems[m.probCursor].Collection)
		}
		if len(m.cfg.Collections) == 1 {
			return m.startIndexCollection(m.cfg.Collections[0].Name)
		}
		m.flashErr = ""
		m.flashMsg = "Select a problem file to re-index its collection, or press 'i' for all."
	case "i":
		return m.startIndexAll()
	case "R":
		m.refreshStatus()
		m.flashMsg = "Refreshed status"
		m.flashErr = ""
	}
	return m, nil
}

func (m dashboardModel) updateProblemDetail(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	lines := m.problemDetailLines(*m.probDetail)
	rows := max(6, m.height-12)
	if scrollLinesOffset(&m.probDetailScroll, len(lines), rows, key.String()) {
		return m, nil
	}
	switch key.String() {
	case "esc", "enter":
		m.probDetail = nil
		m.probDetailScroll = 0
	case "c":
		return m.startIndexCollection(m.probDetail.Collection)
	case "i":
		return m.startIndexAll()
	}
	return m, nil
}

func (m dashboardModel) updateAuditFilter(msg tea.Msg) (dashboardModel, tea.Cmd) {
	cmd := updateFilterInput(&m.auditFilterIn, &m.auditFilterOn, m.applyAuditFilter, msg)
	return m, cmd
}

func (m dashboardModel) resolveAuditDocs(docIDs []string) []string {
	if len(docIDs) == 0 {
		return nil
	}
	allowed := m.cfg.CollectionNames()
	out := make([]string, 0, len(docIDs))
	for _, id := range docIDs {
		if m.store != nil {
			if d, err := m.store.GetDocument(m.ctx, allowed, id); err == nil {
				out = append(out, fmt.Sprintf("%s  ->  %s/%s (%s)", id, d.Collection, d.RelPath, d.Status))
				continue
			}
		}
		out = append(out, id+"  (not in currently indexed collections)")
	}
	return out
}

func (m *dashboardModel) openAuditDetail(e store.AuditEntry) {
	eCopy := e
	eCopy.DocIDs = slices.Clone(e.DocIDs)
	m.auditDetail = &eCopy
	m.auditDetailDocs = m.resolveAuditDocs(eCopy.DocIDs)
	m.auditDetailScroll = 0
}

func (m dashboardModel) updateAuditTab(key tea.KeyPressMsg) (dashboardModel, tea.Cmd) {
	rows := max(5, m.height-17)
	if moveListCursor(&m.auditCursor, &m.auditScroll, len(m.filteredAudit), rows, key.String()) {
		return m, nil
	}
	switch key.String() {
	case "enter":
		if len(m.filteredAudit) > 0 && m.auditCursor < len(m.filteredAudit) {
			m.openAuditDetail(m.filteredAudit[m.auditCursor])
		}
	case "/":
		m.auditFilterOn = true
		m.auditFilterIn.CursorEnd()
		return m, m.auditFilterIn.Focus()
	case "o":
		m.auditOutcomeFilterIdx = (m.auditOutcomeFilterIdx + 1) % len(auditOutcomeFilters)
		m.auditCursor = 0
		m.auditScroll = 0
		m.applyAuditFilter()
	case "f":
		m.auditFollow = !m.auditFollow
		m.auditTickSeq++
		m.flashErr = ""
		if m.auditFollow {
			m.refreshAudit()
			m.flashMsg = "Audit follow on (refreshing every 1.5s)"
			return m, auditTickCmd(m.auditTickSeq)
		}
		m.flashMsg = "Audit follow paused"
	case "esc":
		if m.auditFilterIn.Value() != "" || m.auditOutcomeFilterIdx != 0 {
			m.auditFilterIn.SetValue("")
			m.auditOutcomeFilterIdx = 0
			m.applyAuditFilter()
		}
	case "R":
		m.refreshAudit()
		m.flashMsg = "Refreshed audit log"
		m.flashErr = ""
	}
	return m, nil
}

func (m dashboardModel) updateAuditDetail(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	lines := m.auditDetailLines(*m.auditDetail, m.auditDetailDocs)
	rows := max(6, m.height-12)
	if scrollLinesOffset(&m.auditDetailScroll, len(lines), rows, key.String()) {
		return m, nil
	}
	switch key.String() {
	case "esc", "enter":
		m.auditDetail = nil
		m.auditDetailDocs = nil
		m.auditDetailScroll = 0
	}
	return m, nil
}

func (m dashboardModel) updateSettingsPicker(msg tea.Msg) (dashboardModel, tea.Cmd) {
	if res, ok := msg.(folderPickerDoneMsg); ok {
		m.setPicker = nil
		if !res.canceled && res.path != "" {
			spec := m.settingsSpecs[m.setCursor]
			if err := spec.set(m.draftCfg, res.path); err != nil {
				m.flashErr = err.Error()
			} else {
				m.settingsDirty = true
				m.flashErr = ""
			}
		}
		return m, nil
	}
	np, cmd := m.setPicker.Update(msg)
	m.setPicker = &np
	return m, cmd
}

func (m dashboardModel) updateSettingEdit(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.setInput, cmd = m.setInput.Update(msg)
		return m, cmd
	}
	spec := m.settingsSpecs[m.setCursor]
	switch key.String() {
	case "esc":
		m.setEditing = false
		m.setInput.Blur()
		return m, nil
	case "ctrl+b":
		if spec.kind == kindDir {
			m.setEditing = false
			m.setInput.Blur()
			p := newFolderPicker("Choose storage.data_dir", spec.get(m.draftCfg), true, m.styles, m.width, m.height)
			m.setPicker = &p
			return m, nil
		}
	case "enter":
		val := strings.TrimSpace(m.setInput.Value())
		if err := spec.set(m.draftCfg, val); err != nil {
			m.flashErr = err.Error()
			return m, nil
		}
		m.setEditing = false
		m.setInput.Blur()
		m.settingsDirty = true
		m.flashErr = ""
		m.flashMsg = "Updated " + spec.section + "." + spec.key + " (press 's' to save)"
		return m, nil
	}
	var cmd tea.Cmd
	m.setInput, cmd = m.setInput.Update(msg)
	return m, cmd
}

func (m dashboardModel) updateTOMLPreview(msg tea.Msg) (dashboardModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	rows := max(6, m.height-12)
	if scrollLinesOffset(&m.tomlScroll, len(m.tomlPreview), rows, key.String()) {
		return m, nil
	}
	switch key.String() {
	case "esc", "q", "v", "enter":
		m.tomlPreview = nil
		m.tomlScroll = 0
	}
	return m, nil
}

func (m dashboardModel) updateSettingsTab(key tea.KeyPressMsg) (dashboardModel, tea.Cmd) {
	rows := max(6, m.height-14)
	spec := m.settingsSpecs[m.setCursor]
	switch key.String() {
	case "up", "k":
		if m.setCursor > 0 {
			m.setCursor--
			if m.setCursor < m.setScroll {
				m.setScroll = m.setCursor
			}
		}
	case "down", "j":
		if m.setCursor < len(m.settingsSpecs)-1 {
			m.setCursor++
			if m.setCursor >= m.setScroll+rows {
				m.setScroll = m.setCursor - rows + 1
			}
		}
	case "space":
		if spec.kind == kindBool {
			spec.toggle(m.draftCfg)
			m.settingsDirty = true
			m.flashErr = ""
			m.flashMsg = fmt.Sprintf("Toggled %s.%s (press 's' to save)", spec.section, spec.key)
			return m, nil
		}
		if spec.kind == kindDir {
			p := newFolderPicker("Choose storage.data_dir", spec.get(m.draftCfg), true, m.styles, m.width, m.height)
			m.setPicker = &p
			return m, nil
		}
	case "b", "ctrl+b":
		if spec.kind == kindDir {
			p := newFolderPicker("Choose storage.data_dir", spec.get(m.draftCfg), true, m.styles, m.width, m.height)
			m.setPicker = &p
			return m, nil
		}
	case "enter", "e":
		if spec.kind == kindBool {
			spec.toggle(m.draftCfg)
			m.settingsDirty = true
			m.flashErr = ""
			m.flashMsg = fmt.Sprintf("Toggled %s.%s (press 's' to save)", spec.section, spec.key)
			return m, nil
		}
		m.setEditing = true
		m.setInput.Placeholder = spec.hint
		m.setInput.SetValue(spec.get(m.draftCfg))
		m.setInput.CursorEnd()
		return m, m.setInput.Focus()
	case "v":
		raw, err := config.Render(m.draftCfg)
		if err != nil {
			m.flashErr = err.Error()
			return m, nil
		}
		m.tomlPreview = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		m.tomlScroll = 0
		m.flashErr = ""
	case "s":
		var cmd tea.Cmd
		m, cmd, _ = m.saveConfig(m.draftCfg, "Saved "+m.cfgPath+" (mode 0600)")
		return m, cmd
	case "u":
		m.draftCfg = m.cfg.Clone()
		m.settingsDirty = false
		m.flashErr = ""
		m.flashMsg = "Reverted unsaved settings changes"
	}
	return m, nil
}

func (m dashboardModel) View() string {
	var b strings.Builder

	// Top bar
	b.WriteString(m.styles.title.Render("Roost Configurator"))
	b.WriteString("  " + m.styles.muted.Render(m.cfgPath))
	b.WriteString("  " + m.styles.badge.Render(fmt.Sprintf("%d collection(s)", len(m.cfg.Collections))))
	b.WriteString("\n")

	// Tabs
	var tabs []string
	for i, name := range dashTabNames {
		if dashTab(i) == m.tab {
			tabs = append(tabs, m.styles.tabActive.Render(name))
		} else {
			tabs = append(tabs, m.styles.tabIdle.Render(name))
		}
	}
	b.WriteString(strings.Join(tabs, " ") + "\n")
	b.WriteString(m.styles.headerRule.Render(strings.Repeat("─", max(20, min(96, m.width-2)))) + "\n")

	if m.restartNeeded {
		b.WriteString(m.styles.warn.Render("! roost.toml saved — restart `roost serve` for config changes to take effect (token changes are live).") + "\n")
	}

	var body string
	switch {
	case m.colEd != nil:
		body = m.colEd.View()
	case m.setPicker != nil:
		body = m.setPicker.View()
	case m.tokModal != nil:
		body = m.viewTokenModal()
	case len(m.tomlPreview) > 0:
		body = m.viewTOMLPreview()
	case m.probDetail != nil:
		body = m.viewProblemDetail()
	case m.auditDetail != nil:
		body = m.viewAuditDetail()
	case m.rmColConfirm:
		body = m.viewRemoveColConfirm()
	case m.pruneOrphanConf:
		body = m.viewPruneOrphanConfirm()
	case m.revokeConf:
		body = m.viewRevokeConfirm()
	default:
		switch m.tab {
		case tabCollections:
			body = m.viewCollectionsTab()
		case tabTokens:
			body = m.viewTokensTab()
		case tabStatus:
			body = m.viewStatusTab()
		case tabAudit:
			body = m.viewAuditTab()
		case tabSettings:
			body = m.viewSettingsTab()
		case tabExplore:
			body = m.expl.View()
		}
	}

	b.WriteString(body)
	errMsg := m.flashErr
	if m.tab == tabExplore && errMsg == "" {
		errMsg = m.expl.errMsg
	}
	if errMsg != "" {
		b.WriteString("\n" + m.styles.err.Render("Error: "+errMsg))
	} else if m.flashMsg != "" {
		b.WriteString("\n" + m.styles.ok.Render(m.flashMsg))
	}
	if !m.hasOverlay() {
		b.WriteString("\n" + m.styles.help.Render("1–6 / Tab switch tabs  •  q quit"))
	}
	return b.String()
}

func (m dashboardModel) viewCollectionsTab() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Configured Collections (access control unit)") + "\n\n")

	for i, c := range m.cfg.Collections {
		var okCount, probCount, chunks int
		if st := m.statusMap[c.Name]; st != nil {
			okCount = st.ByStatus[store.StatusOK]
			chunks = st.Chunks
			for k, v := range st.ByStatus {
				if k != store.StatusOK {
					probCount += v
				}
			}
		}
		last := m.lastScans[c.Name]
		if last == "" {
			last = "never scanned"
		}
		line := fmt.Sprintf("%-16s  ok:%-4d  chunks:%-5d  attn:%-3d  %s", c.Name, okCount, chunks, probCount, c.Path)
		if len(c.Exclude) > 0 {
			line += fmt.Sprintf("  [exclude: %s]", joinExcludes(c.Exclude))
		}
		line = truncateText(line, max(40, m.width-8))
		if i == m.colCursor {
			b.WriteString(m.styles.selected.Render("> "+line) + "\n")
			b.WriteString("   " + m.styles.muted.Render("Last scan: "+last) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	if len(m.orphans) > 0 {
		b.WriteString("\n" + m.styles.warn.Render(fmt.Sprintf("Orphan collections still indexed in DB: %s (press 'p' to prune)", strings.Join(m.orphans, ", "))) + "\n")
	}
	b.WriteString("\n" + m.styles.help.Render("↑/↓ select  •  a add folder  •  e/Enter edit  •  d remove  •  p prune orphans  •  R refresh"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewRemoveColConfirm() string {
	col := m.cfg.Collections[m.colCursor]
	box := "[ ]"
	if m.rmColAlsoPrune {
		box = "[x]"
	}
	var b strings.Builder
	b.WriteString(m.styles.title.Render("Remove Collection "+col.Name) + "\n\n")
	b.WriteString(fmt.Sprintf("Path: %s\n\n", col.Path))
	b.WriteString("By default Roost keeps indexed rows in roost.db (safe against temporary unmounts).\n\n")
	b.WriteString(m.styles.label.Render(box+" Also delete all indexed documents for "+col.Name+" from roost.db") + "\n\n")
	b.WriteString(m.styles.help.Render("Space toggle delete from DB  •  y/Enter confirm remove  •  n/Esc cancel"))
	return m.styles.modal.Width(max(44, min(80, m.width-6))).Render(b.String())
}

func (m dashboardModel) viewPruneOrphanConfirm() string {
	var b strings.Builder
	b.WriteString(m.styles.title.Render("Prune Orphan Collections") + "\n\n")
	b.WriteString("Delete indexed documents and chunks for unconfigured collections:\n")
	b.WriteString(m.styles.warn.Render("  "+strings.Join(m.orphans, ", ")) + "\n\n")
	b.WriteString(m.styles.help.Render("y/Enter confirm prune  •  n/Esc cancel"))
	return m.styles.modal.Width(max(44, min(80, m.width-6))).Render(b.String())
}

func (m dashboardModel) viewTokensTab() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("API Tokens (live in roost.db — revocations apply immediately)") + "\n\n")

	if len(m.tokens) == 0 {
		b.WriteString(m.styles.muted.Render("No tokens issued yet. Press 'n' to create one.") + "\n")
	} else {
		rows := max(5, m.height-14)
		end := min(len(m.tokens), m.tokScroll+rows)
		b.WriteString(m.styles.muted.Render(fmt.Sprintf("  %-12s  %-16s  %-18s  %-10s  %-10s  %s", "ID", "NAME", "GRANTS", "CREATED", "EXPIRES", "STATE")) + "\n")
		for i := m.tokScroll; i < end; i++ {
			t := m.tokens[i]
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
			grants := strings.Join(t.Collections, ",")
			if len(t.Collections) == 1 && t.Collections[0] == auth.AllCollections {
				grants = "* (all)"
			}
			line := fmt.Sprintf("%-12s  %-16s  %-18s  %-10s  %-10s  %s",
				truncateText(t.ID, 12),
				truncateText(t.Name, 16),
				truncateText(grants, 18),
				t.CreatedAt.Format(time.DateOnly),
				exp,
				state,
			)
			line = truncateText(line, max(40, m.width-8))
			if i == m.tokCursor {
				b.WriteString(m.styles.selected.Render("> "+line) + "\n")
			} else if state != "active" {
				b.WriteString(m.styles.muted.Render("  "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
	}

	b.WriteString("\n" + m.styles.help.Render("↑/↓ select  •  n create token  •  r revoke token  •  R refresh"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewTokenModal() string {
	var b strings.Builder
	b.WriteString(m.styles.title.Render("Create API Token") + "\n\n")
	b.WriteString(m.tokModal.ViewForm(true, m.cfg, m.cfgPath) + "\n")
	if m.tokModal.secret == "" {
		b.WriteString("\n" + m.styles.help.Render("Tab/↑/↓ move  •  Space toggle  •  ←/→ lifetime  •  Enter submit  •  Esc cancel"))
	}
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewRevokeConfirm() string {
	tok := m.tokens[m.tokCursor]
	var b strings.Builder
	b.WriteString(m.styles.title.Render("Revoke API Token") + "\n\n")
	b.WriteString(fmt.Sprintf("Token ID: %s\nName:     %s\nGrants:   %s\n\n", tok.ID, tok.Name, strings.Join(tok.Collections, ", ")))
	b.WriteString(m.styles.warn.Render("Clients using this token will lose access on their very next request.") + "\n\n")
	b.WriteString(m.styles.help.Render("y/Enter confirm revoke  •  n/Esc cancel"))
	return m.styles.modal.Width(max(44, min(80, m.width-6))).Render(b.String())
}

func (m dashboardModel) modalOuterWidth() int {
	return max(44, min(108, m.width-6))
}

func (m dashboardModel) modalWrapWidth() int {
	return max(28, m.modalOuterWidth()-6)
}

func appendWrappedKV(lines []string, label, value string, width int) []string {
	prefix := fmt.Sprintf("%-13s ", label+":")
	if value == "" {
		return append(lines, prefix+"(none)")
	}
	if !strings.Contains(value, "\n") && len([]rune(value)) <= max(12, width-len([]rune(prefix))) {
		return append(lines, prefix+value)
	}
	lines = append(lines, label+":")
	for _, wl := range wrapLines(value, max(16, width-2)) {
		lines = append(lines, "  "+wl)
	}
	return lines
}

func (m dashboardModel) renderDetailModal(title string, lines []string, scroll int, help string) string {
	outer := m.modalOuterWidth()
	rows := max(6, m.height-12)
	maxScroll := max(0, len(lines)-rows)
	off := min(max(0, scroll), maxScroll)
	end := min(len(lines), off+rows)

	var b strings.Builder
	b.WriteString(m.styles.title.Render(title) + "\n\n")
	b.WriteString(strings.Join(lines[off:end], "\n") + "\n")
	if len(lines) > rows {
		b.WriteString("\n" + m.styles.muted.Render(fmt.Sprintf("Lines %d–%d of %d", off+1, end, len(lines))))
	}
	b.WriteString("\n" + m.styles.help.Render(help))
	return m.styles.modal.Width(outer).Render(b.String())
}

func (m dashboardModel) problemDetailLines(d store.Document) []string {
	w := m.modalWrapWidth()
	diskPath := "(collection not in current config)"
	if col, ok := m.cfg.Collection(d.Collection); ok {
		diskPath = filepath.Join(col.Path, filepath.FromSlash(d.RelPath))
	}
	state := d.Status
	if d.Truncated {
		state += " (truncated)"
	}
	indexedAt := "unknown"
	if d.IndexedAt > 0 {
		indexedAt = time.Unix(d.IndexedAt, 0).Local().Format("2006-01-02 15:04:05")
	}

	var lines []string
	lines = appendWrappedKV(lines, "Collection", d.Collection, w)
	lines = appendWrappedKV(lines, "Rel path", d.RelPath, w)
	lines = appendWrappedKV(lines, "Disk path", diskPath, w)
	lines = appendWrappedKV(lines, "Status", state, w)
	lines = appendWrappedKV(lines, "Document ID", d.ID, w)
	lines = appendWrappedKV(lines, "Title", d.Title, w)
	lines = appendWrappedKV(lines, "File type", d.FileType, w)
	lines = appendWrappedKV(lines, "Size", formatBytes(d.Size), w)
	lines = appendWrappedKV(lines, "Indexed at", indexedAt, w)
	lines = appendWrappedKV(lines, "SHA-256", d.SHA256, w)
	if len(d.Meta) > 0 {
		keys := make([]string, 0, len(d.Meta))
		for k := range d.Meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, d.Meta[k]))
		}
		lines = appendWrappedKV(lines, "Metadata", strings.Join(parts, ", "), w)
	}
	lines = appendWrappedKV(lines, "Error", d.Error, w)
	lines = appendWrappedKV(lines, "Next step", problemGuidance(d), w)
	return lines
}

func (m dashboardModel) viewProblemDetail() string {
	d := *m.probDetail
	lines := m.problemDetailLines(d)
	help := fmt.Sprintf("↑/↓/PgUp/PgDn scroll  •  c re-index %s  •  i index all  •  Esc/Enter close", d.Collection)
	return m.renderDetailModal("Problem File Detail", lines, m.probDetailScroll, help)
}

func (m dashboardModel) auditDetailLines(e store.AuditEntry, docLines []string) []string {
	w := m.modalWrapWidth()
	q := e.Query
	if q == "" {
		q = "(none, or omitted when audit.log_queries = false)"
	}

	var lines []string
	lines = appendWrappedKV(lines, "Time", e.Time.Local().Format("2006-01-02 15:04:05 MST"), w)
	lines = appendWrappedKV(lines, "Token name", e.TokenName, w)
	lines = appendWrappedKV(lines, "Token ID", e.TokenID, w)
	lines = appendWrappedKV(lines, "Transport", e.Transport, w)
	lines = appendWrappedKV(lines, "Remote", e.Remote, w)
	lines = appendWrappedKV(lines, "Operation", e.Op, w)
	lines = appendWrappedKV(lines, "Outcome", e.Outcome, w)
	lines = appendWrappedKV(lines, "Query", q, w)
	if len(docLines) == 0 {
		lines = appendWrappedKV(lines, "Documents", "(none)", w)
	} else {
		lines = append(lines, fmt.Sprintf("Documents (%d):", len(docLines)))
		for _, dl := range docLines {
			for _, wl := range wrapLines(dl, max(16, w-2)) {
				lines = append(lines, "  "+wl)
			}
		}
	}
	return lines
}

func (m dashboardModel) viewAuditDetail() string {
	lines := m.auditDetailLines(*m.auditDetail, m.auditDetailDocs)
	return m.renderDetailModal("Audit Entry Detail", lines, m.auditDetailScroll, "↑/↓/PgUp/PgDn scroll  •  Esc/Enter close")
}

func (m dashboardModel) viewStatusTab() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Index Coverage"))
	if m.indexing {
		b.WriteString("  " + m.spin.View() + " " + m.styles.accent.Render("Indexing..."))
	}
	b.WriteString("\n\n")

	b.WriteString(m.styles.muted.Render(fmt.Sprintf("%-14s  %5s  %7s  %6s  %6s  %6s  %6s  %6s  %s",
		"COLLECTION", "OK", "CHUNKS", "NOTXT", "UNSUP", "ENCR", "LARGE", "FAIL", "LAST SCAN")) + "\n")
	for _, name := range m.cfg.CollectionNames() {
		s := m.statusMap[name]
		if s == nil {
			s = &store.CollectionStatus{ByStatus: map[string]int{}}
		}
		last := m.lastScans[name]
		if last == "" {
			last = "never"
		}
		bs := s.ByStatus
		row := fmt.Sprintf("%-14s  %5d  %7d  %6d  %6d  %6d  %6d  %6d  %s",
			truncateText(name, 14),
			bs[store.StatusOK], s.Chunks, bs[store.StatusNoText],
			bs[store.StatusUnsupported], bs[store.StatusEncrypted],
			bs[store.StatusTooLarge], bs[store.StatusFailed], last,
		)
		b.WriteString(truncateText(row, max(40, m.width-8)) + "\n")
	}

	if m.cfg.Embeddings.Enabled {
		b.WriteString("\n" + m.styles.label.Render(fmt.Sprintf("Embeddings: %d of %d chunks done", m.embTotal-m.embMissing, m.embTotal)) + "\n")
	}

	hdr := fmt.Sprintf("Files needing attention (%d)", len(m.problems))
	if len(m.filteredProblems) != len(m.problems) {
		hdr = fmt.Sprintf("Files needing attention (%d/%d)", len(m.filteredProblems), len(m.problems))
	}
	b.WriteString("\n" + m.styles.label.Render(hdr))
	if code := probStatusFilters[m.probFilterIdx]; code != "" {
		b.WriteString("  " + m.styles.badge.Render("status: "+probFilterLabel(code)))
	}
	if m.probSearchOn {
		b.WriteString("  " + m.probSearchIn.View())
	} else if f := strings.TrimSpace(m.probSearchIn.Value()); f != "" {
		b.WriteString("  " + m.styles.badge.Render("filter: "+f))
	}
	b.WriteString("\n")

	if len(m.problems) == 0 {
		b.WriteString(m.styles.muted.Render("  None.") + "\n")
	} else if len(m.filteredProblems) == 0 {
		b.WriteString(m.styles.muted.Render("  No matching problem files (press Esc to clear filters).") + "\n")
	} else {
		rows := max(4, m.height-18)
		end := min(len(m.filteredProblems), m.probScroll+rows)
		for i := m.probScroll; i < end; i++ {
			d := m.filteredProblems[i]
			state := d.Status
			if d.Truncated {
				state += "(trunc)"
			}
			line := fmt.Sprintf("%-12s %s/%s", state, d.Collection, d.RelPath)
			if d.Error != "" {
				line += ": " + d.Error
			}
			line = truncateText(line, max(40, m.width-8))
			if i == m.probCursor {
				b.WriteString(m.styles.selected.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
	}

	if m.probSearchOn {
		b.WriteString("\n" + m.styles.help.Render("Type to filter problems  •  Enter apply  •  Esc clear"))
	} else {
		b.WriteString("\n" + m.styles.help.Render("↑/↓/PgUp/PgDn scroll  •  Enter inspect  •  / search  •  s status  •  c re-index col  •  i index all  •  R refresh"))
	}
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewAuditTab() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Recent Access Audit Log"))
	if m.auditFollow {
		b.WriteString("  " + m.styles.badge.Render("FOLLOWING live"))
	}
	if code := auditOutcomeFilters[m.auditOutcomeFilterIdx]; code != "" {
		b.WriteString("  " + m.styles.badge.Render("outcome: "+auditOutcomeLabel(code)))
	}
	if m.auditFilterOn {
		b.WriteString("  " + m.auditFilterIn.View())
	} else if f := strings.TrimSpace(m.auditFilterIn.Value()); f != "" {
		b.WriteString("  " + m.styles.badge.Render("filter: "+f))
	}
	b.WriteString("\n\n")

	if len(m.filteredAudit) == 0 {
		b.WriteString(m.styles.muted.Render("No matching audit entries.") + "\n")
	} else {
		rows := max(5, m.height-17)
		end := min(len(m.filteredAudit), m.auditScroll+rows)
		b.WriteString(m.styles.muted.Render(fmt.Sprintf("  %-19s  %-12s  %-8s  %-14s  %-11s  %4s  %s",
			"TIME", "TOKEN", "VIA", "OP", "OUTCOME", "DOCS", "QUERY")) + "\n")
		for i := m.auditScroll; i < end; i++ {
			e := m.filteredAudit[i]
			line := fmt.Sprintf("%-19s  %-12s  %-8s  %-14s  %-11s  %4d  %s",
				e.Time.Local().Format("2006-01-02 15:04:05"),
				truncateText(e.TokenName, 12),
				truncateText(e.Transport, 8),
				truncateText(e.Op, 14),
				truncateText(e.Outcome, 11),
				len(e.DocIDs),
				e.Query,
			)
			line = truncateText(line, max(40, m.width-8))
			if i == m.auditCursor {
				b.WriteString(m.styles.selected.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}

		if m.auditCursor >= 0 && m.auditCursor < len(m.filteredAudit) {
			sel := m.filteredAudit[m.auditCursor]
			detail := fmt.Sprintf("Detail: token=%s(%s) remote=%s docs=[%s] query=%q",
				sel.TokenName, sel.TokenID, sel.Remote, strings.Join(sel.DocIDs, ","), sel.Query)
			b.WriteString("\n" + m.styles.muted.Render(truncateText(detail, max(40, m.width-8))) + "\n")
		}
	}

	if m.auditFilterOn {
		b.WriteString("\n" + m.styles.help.Render("Type to filter  •  Enter apply  •  Esc clear"))
	} else {
		b.WriteString("\n" + m.styles.help.Render("↑/↓/PgUp/PgDn scroll  •  Enter inspect  •  / search  •  o outcome  •  f follow  •  Esc clear  •  R refresh"))
	}
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewSettingsTab() string {
	var b strings.Builder
	b.WriteString(m.styles.label.Render("Configuration Settings"))
	if m.settingsDirty {
		b.WriteString("  " + m.styles.warn.Render("[unsaved changes — press 's' to save]"))
	}
	b.WriteString("\n\n")

	rows := max(6, m.height-14)
	end := min(len(m.settingsSpecs), m.setScroll+rows)
	for i := m.setScroll; i < end; i++ {
		sp := m.settingsSpecs[i]
		val := sp.get(m.draftCfg)
		if val == "" {
			val = "(unset)"
		}
		keyLabel := fmt.Sprintf("[%s] %s", sp.section, sp.key)
		line := fmt.Sprintf("%-34s  %s", keyLabel, val)
		line = truncateText(line, max(40, m.width-8))
		if i == m.setCursor {
			b.WriteString(m.styles.selected.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	curSpec := m.settingsSpecs[m.setCursor]
	b.WriteString("\n" + m.styles.muted.Render("Hint: "+curSpec.hint) + "\n")
	if m.setEditing {
		b.WriteString(m.styles.label.Render("Edit "+curSpec.section+"."+curSpec.key+": ") + m.setInput.View() + "\n")
		editHelp := "Enter apply  •  Esc cancel"
		if curSpec.kind == kindDir {
			editHelp = "Enter apply  •  Ctrl+B browse folders  •  Esc cancel"
		}
		b.WriteString(m.styles.help.Render(editHelp))
	} else {
		help := "↑/↓ move  •  Enter/Space edit or toggle  •  v preview TOML  •  s save  •  u revert"
		if curSpec.kind == kindDir {
			help = "↑/↓ move  •  Enter edit  •  b/Space browse folder  •  v preview TOML  •  s save  •  u revert"
		}
		b.WriteString(m.styles.help.Render(help))
	}
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}

func (m dashboardModel) viewTOMLPreview() string {
	var b strings.Builder
	b.WriteString(m.styles.title.Render("Rendered "+m.cfgPath+" Preview") + "\n\n")
	rows := max(6, m.height-12)
	end := min(len(m.tomlPreview), m.tomlScroll+rows)
	b.WriteString(m.styles.codeBox.Width(max(40, m.width-10)).Render(strings.Join(m.tomlPreview[m.tomlScroll:end], "\n")))
	b.WriteString("\n\n" + m.styles.help.Render("↑/↓/PgUp/PgDn scroll  •  Esc/v/Enter close"))
	return m.styles.panel.Width(max(44, m.width-4)).Render(b.String())
}
