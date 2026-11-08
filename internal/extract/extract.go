// Package extract turns documents into sectioned plain text.
//
// Everything here treats its input as hostile. Parsers run in a child process
// (see Runner), read only the file handed to them, and never follow external
// references such as remote images or linked files.
package extract

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Version changes whenever extraction output changes, which forces a re-index.
const Version = 1

// ErrUnsupported and ErrEncrypted are expected outcomes, not parser failures.
var (
	ErrUnsupported = errors.New("unsupported format")
	ErrEncrypted   = errors.New("document is password protected")
)

// Section is a labelled part of a document: a page, slide, sheet, or heading.
type Section struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Result is the output of extracting one file.
type Result struct {
	Title     string            `json:"title,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Sections  []Section         `json:"sections"`
	Truncated bool              `json:"truncated,omitempty"`
}

// Limits bounds the work done on one file.
type Limits struct {
	MaxText  int64 // text bytes to keep; extraction stops and sets Truncated past this
	MaxUnzip int64 // total bytes to decompress from a zip container
}

// Source is the input file. An *os.File satisfies it.
type Source interface {
	io.Reader
	io.ReaderAt
}

type extractor func(src Source, size int64, lim Limits) (*Result, error)

var extractors = map[string]extractor{
	".docx": extractDocx, ".docm": extractDocx,
	".pptx": extractPptx, ".pptm": extractPptx,
	".xlsx": extractXlsx, ".xlsm": extractXlsx,
	".pdf": extractPDF,
	".txt": extractText, ".text": extractText, ".log": extractText, ".rst": extractText,
	".md": extractMarkdown, ".markdown": extractMarkdown,
	".csv": extractCSV, ".tsv": extractTSV,
	".html": extractHTML, ".htm": extractHTML,
	".eml": extractEmail,
}

// Supported reports whether a lower-case extension can be extracted.
func Supported(ext string) bool {
	_, ok := extractors[ext]
	return ok
}

// Extract parses src in the current process. Use Runner for untrusted files.
func Extract(src Source, size int64, ext string, lim Limits) (res *Result, err error) {
	fn, ok := extractors[ext]
	if !ok {
		return nil, ErrUnsupported
	}
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("parser panic: %v", p)
		}
	}()
	return fn(src, size, lim)
}

// collector accumulates sections under a text budget, so a huge or malicious
// document stops early and is flagged rather than exhausting memory.
type collector struct {
	res   Result
	label string
	buf   strings.Builder
	used  int64
	limit int64
}

func newCollector(lim Limits, firstLabel string) *collector {
	return &collector{label: firstLabel, limit: lim.MaxText}
}

// full reports whether the budget is spent.
func (c *collector) full() bool { return c.res.Truncated }

// section closes the current section and starts another.
func (c *collector) section(label string) {
	c.flush()
	c.label = label
}

func (c *collector) flush() {
	if text := strings.TrimSpace(c.buf.String()); text != "" {
		c.res.Sections = append(c.res.Sections, Section{Label: c.label, Text: text})
	}
	c.buf.Reset()
}

// line appends one line of text, sanitized and counted against the budget.
func (c *collector) line(s string) {
	if c.full() {
		return
	}
	s = clean(s)
	if strings.TrimSpace(s) == "" {
		return
	}
	if c.used+int64(len(s)) > c.limit {
		s = truncateUTF8(s, int(c.limit-c.used))
		c.res.Truncated = true
	}
	c.used += int64(len(s)) + 1
	c.buf.WriteString(s)
	c.buf.WriteByte('\n')
}

// para appends a line followed by a blank line, marking a paragraph break.
func (c *collector) para(s string) {
	before := c.buf.Len()
	c.line(s)
	if c.buf.Len() > before {
		c.buf.WriteByte('\n')
	}
}

func (c *collector) result() *Result {
	c.flush()
	return &c.res
}

// clean makes text safe to store: valid UTF-8 without NULs or control codes.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func setMeta(r *Result, key, val string) {
	val = strings.TrimSpace(clean(val))
	if val == "" {
		return
	}
	if r.Meta == nil {
		r.Meta = map[string]string{}
	}
	r.Meta[key] = truncateUTF8(val, 1024)
}
