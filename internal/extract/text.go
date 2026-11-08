package extract

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/charmap"
)

// readBounded reads at most the text budget plus a margin for encoding
// overhead, and reports whether the file was longer than that.
func readBounded(src Source, lim Limits) ([]byte, bool, error) {
	max := lim.MaxText*2 + 1024
	b, err := io.ReadAll(io.LimitReader(src, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) > max {
		return b[:max], true, nil
	}
	return b, false, nil
}

// decodeText handles the encodings found in real file shares: UTF-8, UTF-16
// with a BOM (Windows Notepad), and otherwise Windows-1252.
func decodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return decodeUTF16(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], true)
	case utf8.Valid(b):
		return string(b)
	}
	s, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(s)
}

func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(u))
}

// textInput reads a text file. more reports content past the read limit.
func textInput(src Source, lim Limits) (s string, c *collector, more bool, err error) {
	b, more, err := readBounded(src, lim)
	if err != nil {
		return "", nil, false, err
	}
	s = strings.ReplaceAll(decodeText(b), "\r\n", "\n")
	return s, newCollector(lim, "Body"), more, nil
}

func extractText(src Source, _ int64, lim Limits) (*Result, error) {
	s, c, more, err := textInput(src, lim)
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) == "" {
			c.buf.WriteByte('\n')
			continue
		}
		c.line(line)
	}
	return finishText(c, more), nil
}

func finishText(c *collector, more bool) *Result {
	r := c.result()
	r.Truncated = r.Truncated || more
	return r
}

func extractMarkdown(src Source, _ int64, lim Limits) (*Result, error) {
	s, c, more, err := textInput(src, lim)
	if err != nil {
		return nil, err
	}
	fenced := false
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		if !fenced {
			if level, text := mdHeading(trimmed); level > 0 {
				if level == 1 && c.res.Title == "" {
					c.res.Title = clean(text)
				}
				c.section(firstLine(text))
			}
		}
		if trimmed == "" {
			c.buf.WriteByte('\n')
			continue
		}
		c.line(line)
	}
	return finishText(c, more), nil
}

func mdHeading(s string) (int, string) {
	n := 0
	for n < len(s) && n < 7 && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(s) || s[n] != ' ' {
		return 0, ""
	}
	return n, strings.TrimSpace(strings.TrimRight(s[n:], "# "))
}

func extractCSV(src Source, size int64, lim Limits) (*Result, error) {
	return csvWith(src, lim, ',')
}

func extractTSV(src Source, size int64, lim Limits) (*Result, error) {
	return csvWith(src, lim, '\t')
}

func csvWith(src Source, lim Limits, comma rune) (*Result, error) {
	s, c, more, err := textInput(src, lim)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = comma
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	for !c.full() {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Not really CSV. Keep the text rather than lose the file.
			c.buf.Reset()
			c.used = 0
			for line := range strings.SplitSeq(s, "\n") {
				c.line(line)
			}
			break
		}
		c.line(strings.Join(rec, " | "))
	}
	return finishText(c, more), nil
}

// --- HTML ---

var htmlSkip = map[string]bool{"script": true, "style": true, "noscript": true, "template": true, "head": true, "svg": true, "iframe": true, "object": true}

var htmlBlock = map[string]bool{
	"p": true, "div": true, "li": true, "tr": true, "br": true, "section": true, "article": true, "header": true,
	"footer": true, "ul": true, "ol": true, "table": true, "blockquote": true, "pre": true, "h4": true, "h5": true, "h6": true,
}

func extractHTML(src Source, _ int64, lim Limits) (*Result, error) {
	b, more, err := readBounded(src, lim)
	if err != nil {
		return nil, err
	}
	c := newCollector(lim, "Body")
	if err := htmlInto(bytes.NewReader(b), c, true); err != nil {
		return nil, err
	}
	r := c.result()
	r.Truncated = r.Truncated || more
	return r, nil
}

func htmlInto(in io.Reader, c *collector, sections bool) error {
	root, err := html.Parse(in)
	if err != nil {
		return fmt.Errorf("parse html: %w", err)
	}
	var line strings.Builder
	emit := func() {
		c.line(strings.Join(strings.Fields(line.String()), " "))
		line.Reset()
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			name := n.Data
			if name == "title" || (name == "head" && sections) {
				if t := findTitle(n); t != "" && c.res.Title == "" {
					c.res.Title = clean(t)
				}
			}
			if htmlSkip[name] {
				return
			}
			switch name {
			case "h1", "h2", "h3":
				emit()
				text := strings.Join(strings.Fields(nodeText(n)), " ")
				if sections && text != "" {
					c.section(firstLine(text))
				}
				c.para(text)
				return
			case "td", "th":
				if line.Len() > 0 {
					line.WriteString(" | ")
				}
			}
			if htmlBlock[name] {
				emit()
				defer emit()
			}
		}
		if n.Type == html.TextNode {
			line.WriteString(n.Data)
			line.WriteByte(' ')
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(root)
	emit()
	return nil
}

func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		return strings.Join(strings.Fields(nodeText(n)), " ")
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if t := findTitle(ch); t != "" {
			return t
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

// --- Email ---

const maxMIMEDepth = 10

func extractEmail(src Source, _ int64, lim Limits) (*Result, error) {
	b, more, err := readBounded(src, lim)
	if err != nil {
		return nil, err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse email: %w", err)
	}
	dec := mime.WordDecoder{CharsetReader: charset.NewReaderLabel}
	header := func(k string) string {
		v := msg.Header.Get(k)
		if d, err := dec.DecodeHeader(v); err == nil {
			return d
		}
		return v
	}
	c := newCollector(lim, "Headers")
	c.res.Title = clean(header("Subject"))
	for _, k := range []string{"From", "To", "Cc", "Date", "Subject"} {
		if v := header(k); v != "" {
			c.line(k + ": " + v)
			setMeta(&c.res, strings.ToLower(k), v)
		}
	}
	c.section("Body")
	var attachments []string
	walkMIME(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), "", msg.Body, c, &attachments, 0)
	if len(attachments) > 0 {
		// Attachments are listed but not extracted in this version.
		c.section("Attachments")
		for _, a := range attachments {
			c.line(a)
		}
		setMeta(&c.res, "attachments", strings.Join(attachments, ", "))
	}
	r := c.result()
	r.Truncated = r.Truncated || more
	return r, nil
}

func walkMIME(ctype, encoding, disposition string, body io.Reader, c *collector, attachments *[]string, depth int) {
	if depth > maxMIMEDepth {
		return
	}
	media, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		media, params = "text/plain", nil
	}
	if strings.HasPrefix(media, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		var plainSeen bool
		for {
			part, err := mr.NextRawPart()
			if err != nil {
				return
			}
			pt := part.Header.Get("Content-Type")
			// In multipart/alternative the HTML repeats the text version.
			if media == "multipart/alternative" && plainSeen && strings.HasPrefix(strings.ToLower(pt), "text/html") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(pt), "text/plain") {
				plainSeen = true
			}
			walkMIME(pt, part.Header.Get("Content-Transfer-Encoding"), part.Header.Get("Content-Disposition"), part, c, attachments, depth+1)
		}
	}
	if d, dp, err := mime.ParseMediaType(disposition); err == nil && d == "attachment" {
		*attachments = append(*attachments, cmpOr(dp["filename"], "(unnamed)"))
		return
	}
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, &stripNewlines{body})
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	}
	if !strings.HasPrefix(media, "text/") {
		if name := params["name"]; name != "" {
			*attachments = append(*attachments, name)
		}
		return
	}
	if cs := params["charset"]; cs != "" {
		if r, err := charset.NewReaderLabel(cs, body); err == nil {
			body = r
		}
	}
	if media == "text/html" {
		_ = htmlInto(body, c, false)
		return
	}
	b, err := io.ReadAll(io.LimitReader(body, c.limit+1))
	if err != nil && len(b) == 0 {
		return
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			c.buf.WriteByte('\n')
			continue
		}
		c.line(line)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// stripNewlines removes line breaks that the base64 decoder would reject.
type stripNewlines struct{ r io.Reader }

func (s *stripNewlines) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	out := p[:0]
	for _, b := range p[:n] {
		if b != '\r' && b != '\n' {
			out = append(out, b)
		}
	}
	return len(out), err
}
