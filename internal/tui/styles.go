// Package tui provides the interactive terminal wizard and configurator for Roost.
package tui

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/ClackStack/roost/internal/config"
	"github.com/ClackStack/roost/internal/store"
)

type styles struct {
	isDark     bool
	title      lipgloss.Style
	subtitle   lipgloss.Style
	tabActive  lipgloss.Style
	tabIdle    lipgloss.Style
	panel      lipgloss.Style
	modal      lipgloss.Style
	selected   lipgloss.Style
	cursor     lipgloss.Style
	label      lipgloss.Style
	value      lipgloss.Style
	muted      lipgloss.Style
	accent     lipgloss.Style
	ok         lipgloss.Style
	warn       lipgloss.Style
	err        lipgloss.Style
	secretBox  lipgloss.Style
	codeBox    lipgloss.Style
	badge      lipgloss.Style
	help       lipgloss.Style
	headerRule lipgloss.Style
}

func defaultStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)

	fg := ld(lipgloss.Color("#1F2328"), lipgloss.Color("#E6EDF3"))
	mutedFg := ld(lipgloss.Color("#59636E"), lipgloss.Color("#8B949E"))
	borderFg := ld(lipgloss.Color("#D0D7DE"), lipgloss.Color("#30363D"))
	accentFg := ld(lipgloss.Color("#B45309"), lipgloss.Color("#F59E0B"))
	accentBg := ld(lipgloss.Color("#FEF3C7"), lipgloss.Color("#3B2809"))
	selectBg := ld(lipgloss.Color("#EBF5FF"), lipgloss.Color("#1F2937"))
	okFg := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	warnFg := ld(lipgloss.Color("#A16207"), lipgloss.Color("#FACC15"))
	errFg := ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171"))
	codeBg := ld(lipgloss.Color("#F6F8FA"), lipgloss.Color("#161B22"))

	return styles{
		isDark: isDark,
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(accentFg),
		subtitle: lipgloss.NewStyle().
			Foreground(mutedFg),
		tabActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(accentFg).
			Background(accentBg).
			Padding(0, 1),
		tabIdle: lipgloss.NewStyle().
			Foreground(mutedFg).
			Padding(0, 1),
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderFg).
			Padding(0, 1),
		modal: lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(accentFg).
			Padding(1, 2),
		selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(fg).
			Background(selectBg),
		cursor: lipgloss.NewStyle().
			Bold(true).
			Foreground(accentFg),
		label: lipgloss.NewStyle().
			Bold(true).
			Foreground(fg),
		value: lipgloss.NewStyle().
			Foreground(fg),
		muted: lipgloss.NewStyle().
			Foreground(mutedFg),
		accent: lipgloss.NewStyle().
			Bold(true).
			Foreground(accentFg),
		ok: lipgloss.NewStyle().
			Bold(true).
			Foreground(okFg),
		warn: lipgloss.NewStyle().
			Bold(true).
			Foreground(warnFg),
		err: lipgloss.NewStyle().
			Bold(true).
			Foreground(errFg),
		secretBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(okFg).
			Padding(0, 1).
			Bold(true).
			Foreground(okFg),
		codeBox: lipgloss.NewStyle().
			Background(codeBg).
			Foreground(fg).
			Padding(0, 1),
		badge: lipgloss.NewStyle().
			Foreground(accentFg).
			Background(accentBg).
			Padding(0, 1),
		help: lipgloss.NewStyle().
			Foreground(mutedFg),
		headerRule: lipgloss.NewStyle().
			Foreground(borderFg),
	}
}

func newTextInput(placeholder, value string, width int, isDark bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetWidth(max(16, width))
	ti.SetStyles(textinput.DefaultStyles(isDark))
	if value != "" {
		ti.SetValue(value)
	}
	return ti
}

func suggestCollectionName(dirPath string) string {
	base := strings.ToLower(filepath.Base(filepath.Clean(dirPath)))
	var b strings.Builder
	prevSep := false
	for _, r := range base {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSep = false
		case r == '-' || r == '_':
			if b.Len() > 0 && !prevSep {
				b.WriteRune(r)
				prevSep = true
			}
		case unicode.IsSpace(r) || r == '.':
			if b.Len() > 0 && !prevSep {
				b.WriteByte('_')
				prevSep = true
			}
		}
		if b.Len() >= 64 {
			break
		}
	}
	name := strings.Trim(b.String(), "_-")
	if name == "" {
		return "docs"
	}
	if name[0] < 'a' || name[0] > 'z' {
		if name[0] < '0' || name[0] > '9' {
			name = "c_" + name
		}
	}
	if len(name) > 64 {
		name = name[:64]
	}
	if config.ValidCollectionName(name) != nil {
		return "docs"
	}
	return name
}

func parseExcludes(raw string) []string {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func joinExcludes(excludes []string) string {
	return strings.Join(excludes, ", ")
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "never"
	}
	return d.String()
}

func truncateText(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return string(r[:maxLen])
	}
	return string(r[:maxLen-1]) + "…"
}

func baseURLForConfig(cfg *config.Config) string {
	scheme := "http"
	if cfg.Server.TLSCert != "" {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return scheme + "://" + cfg.Server.Listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port))
}

func clientSnippets(cfg *config.Config, cfgPath, secret string) string {
	tok := secret
	if tok == "" {
		tok = "<ROOST_TOKEN>"
	}
	url := baseURLForConfig(cfg)
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		absCfg = cfgPath
	}

	var b strings.Builder
	fmt.Fprintf(&b, "1) Start the server:\n   roost serve --config %s\n\n", absCfg)
	fmt.Fprintf(&b, "2) Export the token:\n   export ROOST_TOKEN=%s\n\n", tok)
	fmt.Fprintf(&b, "3) Register in quack (~/.config/quack/mcp.d/roost.toml):\n")
	fmt.Fprintf(&b, "   url = \"%s/mcp\"\n", url)
	fmt.Fprintf(&b, "   headers = { Authorization = { env = \"ROOST_TOKEN\", format = \"Bearer {}\" } }\n")
	fmt.Fprintf(&b, "   # Or stdio: command = \"roost\", args = [\"mcp\", \"--config\", %q]\n\n", absCfg)
	fmt.Fprintf(&b, "4) Test over HTTP:\n   curl -H \"Authorization: Bearer $ROOST_TOKEN\" \"%s/v1/collections\"", url)
	return b.String()
}

func formatBytes(n int64) string {
	if n < 0 {
		return "unknown"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	val := float64(n)
	for _, u := range units {
		val /= 1024
		if val < 1024 || u == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s (%d bytes)", val, u, n)
		}
	}
	return fmt.Sprintf("%d bytes", n)
}

// wrapLines wraps multi-line text to width runes, breaking long unbroken tokens
// like paths or SHA-256 hashes cleanly so modals never overflow narrow panes.
func wrapLines(text string, width int) []string {
	if width <= 0 {
		return strings.Split(text, "\n")
	}
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		r := []rune(para)
		for len(r) > 0 {
			if len(r) <= width {
				out = append(out, string(r))
				break
			}
			cut := width
			if width > 12 {
				for i := width; i > width/3; i-- {
					if r[i-1] == ' ' {
						cut = i - 1
						break
					}
				}
			}
			line := strings.TrimRight(string(r[:cut]), " ")
			out = append(out, line)
			rest := r[cut:]
			for len(rest) > 0 && rest[0] == ' ' {
				rest = rest[1:]
			}
			r = rest
		}
	}
	return out
}

func problemGuidance(d store.Document) string {
	var parts []string
	switch d.Status {
	case store.StatusNoText:
		parts = append(parts, "File parsed with no extractable text (commonly a scanned PDF without embedded OCR).")
	case store.StatusEncrypted:
		parts = append(parts, "Remove password protection from the source document and re-index.")
	case store.StatusTooLarge:
		parts = append(parts, "File exceeds index.max_file_mb; raise index.max_file_mb in Settings (Tab 5) and re-index if needed.")
	case store.StatusUnsupported:
		parts = append(parts, "Unsupported file format; add an [collections] exclude glob if it should be ignored.")
	case store.StatusFailed:
		parts = append(parts, "Check file integrity and OS read permissions, then re-index.")
	}
	if d.Truncated {
		parts = append(parts, "Extracted text exceeded index.max_text_mb and was truncated; raise index.max_text_mb in Settings (Tab 5) if full content is needed.")
	}
	if len(parts) == 0 {
		return "Fix the file or collection settings, then press 'c' to re-index this collection."
	}
	return strings.Join(parts, " ")
}
