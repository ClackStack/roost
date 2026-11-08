// Package chunk splits extracted sections into searchable pieces.
package chunk

import (
	"strings"
	"unicode/utf8"

	"github.com/ClackStack/roost/internal/extract"
	"github.com/ClackStack/roost/internal/store"
)

// Target is the preferred chunk size in bytes; Max is the hard limit. Chunks
// never span sections, so every hit can cite one page, slide, or sheet. They
// also never overlap, so joining them reproduces the document.
const (
	Target = 1400
	Max    = 2000
)

// Split chunks every section of a document in order.
func Split(sections []extract.Section) []store.Chunk {
	var out []store.Chunk
	for _, s := range sections {
		for _, text := range splitText(s.Text) {
			out = append(out, store.Chunk{Section: s.Label, Text: text})
		}
	}
	return out
}

func splitText(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for _, block := range blocks(s) {
		if cur.Len() > 0 && cur.Len()+len(block) > Target {
			flush()
		}
		if len(block) > Max {
			flush()
			out = append(out, hardSplit(block)...)
			continue
		}
		cur.WriteString(block)
	}
	flush()
	return out
}

// blocks cuts text after newlines. Each block keeps its newline, so packed
// chunks retain the original line structure.
func blocks(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// hardSplit breaks one overlong line, preferring sentence and word boundaries.
func hardSplit(s string) []string {
	var out []string
	for len(s) > Max {
		cut := boundary(s[:Max])
		if t := strings.TrimSpace(s[:cut]); t != "" {
			out = append(out, t)
		}
		s = s[cut:]
	}
	if t := strings.TrimSpace(s); t != "" {
		out = append(out, t)
	}
	return out
}

func boundary(s string) int {
	floor := len(s) / 2
	for _, sep := range []string{". ", "; ", ", ", " "} {
		if i := strings.LastIndex(s, sep); i >= floor {
			return i + len(sep)
		}
	}
	// No separator, as in CJK text or base64: cut on a rune boundary.
	n := len(s)
	for n > 0 && !utf8.RuneStart(s[n-1]) {
		n--
	}
	if n > 1 {
		return n - 1
	}
	return len(s)
}
