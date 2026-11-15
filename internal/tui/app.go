package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/store"
)

// Options configures an interactive setup/configurator session.
type Options struct {
	ConfigPath  string
	ForceWizard bool
	Index       IndexFunc
	In          io.Reader
	Out         io.Writer
}

type fdReader interface {
	Fd() uintptr
}

// IsInteractive reports whether in and out are both attached to a terminal.
func IsInteractive(in io.Reader, out io.Writer) bool {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	fin, ok1 := in.(fdReader)
	fout, ok2 := out.(fdReader)
	if !ok1 || !ok2 {
		return false
	}
	return term.IsTerminal(int(fin.Fd())) && term.IsTerminal(int(fout.Fd()))
}

// Run launches the interactive Roost setup wizard or configurator dashboard.
func Run(ctx context.Context, opts Options) error {
	if !IsInteractive(opts.In, opts.Out) {
		return errors.New("roost setup requires an interactive terminal (use `roost init` or CLI subcommands for scripting)")
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = config.Find("")
	}

	m, err := newAppModel(ctx, opts)
	if err != nil {
		return err
	}
	defer m.Close()

	var popts []tea.ProgramOption
	popts = append(popts, tea.WithContext(ctx))
	if opts.In != nil {
		popts = append(popts, tea.WithInput(opts.In))
	}
	if opts.Out != nil {
		popts = append(popts, tea.WithOutput(opts.Out))
	}
	p := tea.NewProgram(m, popts...)
	finalModel, err := p.Run()
	if fm, ok := finalModel.(*appModel); ok {
		m = fm
	}
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return err
}

type appModel struct {
	ctx      context.Context
	cfgPath  string
	indexFn  IndexFunc
	styles   styles
	width    int
	height   int
	inWizard bool
	wizard   wizardModel
	dash     dashboardModel
	st       *store.Store
}

func newAppModel(ctx context.Context, opts Options) (*appModel, error) {
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = config.Find("")
	}

	st := defaultStyles(true)
	width, height := 100, 32

	_, statErr := os.Stat(cfgPath)
	cfgExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("stat %s: %w", cfgPath, statErr)
	}

	var loadedCfg *config.Config
	if cfgExists {
		var err error
		loadedCfg, err = config.Load(cfgPath)
		if err != nil && !opts.ForceWizard {
			return nil, fmt.Errorf("load %s: %w (fix the file or run `roost setup --wizard` to rebuild it)", cfgPath, err)
		}
	}

	m := &appModel{
		ctx:     ctx,
		cfgPath: cfgPath,
		indexFn: opts.Index,
		styles:  st,
		width:   width,
		height:  height,
	}

	if !cfgExists || opts.ForceWizard {
		m.inWizard = true
		m.wizard = newWizardModel(ctx, cfgPath, loadedCfg, cfgExists, opts.Index, st, width, height)
		return m, nil
	}

	opened, err := store.Open(loadedCfg.Storage.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", loadedCfg.Storage.DataDir, err)
	}
	m.st = opened
	m.inWizard = false
	m.dash = newDashboardModel(ctx, cfgPath, loadedCfg, opened, opts.Index, st, width, height)
	return m, nil
}

// Close releases any opened SQLite store handle.
func (m *appModel) Close() {
	if m == nil {
		return
	}
	if m.st != nil {
		m.st.Close()
		m.st = nil
	}
}

func (m *appModel) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(60, msg.Width)
		m.height = max(20, msg.Height)
		if m.inWizard {
			m.wizard.setSize(m.width, m.height, m.styles)
		} else {
			m.dash.setSize(m.width, m.height, m.styles)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		m.styles = defaultStyles(msg.IsDark())
		if m.inWizard {
			m.wizard.setSize(m.width, m.height, m.styles)
		} else {
			m.dash.setSize(m.width, m.height, m.styles)
		}
		return m, nil
	case wizardSavedConfigMsg:
		m.st = msg.st
		return m, nil
	case switchToDashboardMsg:
		m.st = msg.st
		m.inWizard = false
		m.dash = newDashboardModel(m.ctx, m.cfgPath, msg.cfg, msg.st, m.indexFn, m.styles, m.width, m.height)
		return m, nil
	case storeReopenedMsg:
		m.st = msg.st
		return m, nil
	}

	if m.inWizard {
		var cmd tea.Cmd
		m.wizard, cmd = m.wizard.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.dash, cmd = m.dash.Update(msg)
	return m, cmd
}

func (m *appModel) View() tea.View {
	var content string
	if m.inWizard {
		content = m.wizard.View()
	} else {
		content = m.dash.View()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
