package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html/charset"
)

// oleMagic starts every OLE compound file. Office writes password-protected
// OOXML files in this container rather than as a zip.
var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

func checkEncrypted(src Source) error {
	head := make([]byte, len(oleMagic))
	if n, _ := src.ReadAt(head, 0); n == len(oleMagic) && bytes.Equal(head, oleMagic) {
		return ErrEncrypted
	}
	return nil
}

// pkg is an OOXML package with a shared decompression budget, which is what
// defeats zip bombs: limits on single entries can be beaten with many entries.
type pkg struct {
	zr     *zip.Reader
	files  map[string]*zip.File
	budget int64
}

func openPkg(src Source, size int64, lim Limits) (*pkg, error) {
	if err := checkEncrypted(src); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	p := &pkg{zr: zr, files: make(map[string]*zip.File, len(zr.File)), budget: lim.MaxUnzip}
	for _, f := range zr.File {
		p.files[f.Name] = f
	}
	return p, nil
}

var errUnzipBudget = errors.New("decompressed size limit reached")

type budgetReader struct {
	r io.Reader
	p *pkg
}

func (b *budgetReader) Read(buf []byte) (int, error) {
	if b.p.budget <= 0 {
		return 0, errUnzipBudget
	}
	if int64(len(buf)) > b.p.budget {
		buf = buf[:b.p.budget]
	}
	n, err := b.r.Read(buf)
	b.p.budget -= int64(n)
	return n, err
}

// decoder opens a part for streaming. ok is false if the part does not exist.
func (p *pkg) decoder(name string) (dec *xml.Decoder, closeFn func(), ok bool, err error) {
	f := p.files[name]
	if f == nil {
		return nil, nil, false, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, nil, true, fmt.Errorf("open %s: %w", name, err)
	}
	d := xml.NewDecoder(&budgetReader{r: rc, p: p})
	d.Strict = false
	d.CharsetReader = charset.NewReaderLabel
	return d, func() { rc.Close() }, true, nil
}

// parseInto decodes a small part into v. A missing part is not an error.
func (p *pkg) parseInto(name string, v any) error {
	d, closeFn, ok, err := p.decoder(name)
	if err != nil || !ok {
		return err
	}
	defer closeFn()
	return d.Decode(v)
}

// coreProps copies docProps/core.xml into the result.
func (p *pkg) coreProps(r *Result) {
	var cp struct {
		Title    string `xml:"title"`
		Subject  string `xml:"subject"`
		Creator  string `xml:"creator"`
		Keywords string `xml:"keywords"`
		LastBy   string `xml:"lastModifiedBy"`
		Created  string `xml:"created"`
		Modified string `xml:"modified"`
	}
	if err := p.parseInto("docProps/core.xml", &cp); err != nil {
		return // metadata is optional; a damaged part must not fail the document
	}
	r.Title = strings.TrimSpace(clean(cp.Title))
	setMeta(r, "subject", cp.Subject)
	setMeta(r, "author", cp.Creator)
	setMeta(r, "keywords", cp.Keywords)
	setMeta(r, "last_modified_by", cp.LastBy)
	setMeta(r, "created", cp.Created)
	setMeta(r, "modified", cp.Modified)
}

type relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// rels reads the relationships of a part, resolving targets to package paths.
func (p *pkg) rels(part string) []relationship {
	var doc struct {
		Rels []relationship `xml:"Relationship"`
	}
	dir, base := path.Split(part)
	if err := p.parseInto(dir+"_rels/"+base+".rels", &doc); err != nil {
		return nil
	}
	for i := range doc.Rels {
		t := doc.Rels[i].Target
		if strings.HasPrefix(t, "/") {
			doc.Rels[i].Target = strings.TrimPrefix(path.Clean(t), "/")
		} else {
			doc.Rels[i].Target = path.Join(dir, t)
		}
	}
	return doc.Rels
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// stopErr reports whether a token error is real rather than a normal end of file.
func stopErr(err error) bool {
	return err != nil && !errors.Is(err, io.EOF)
}

// --- Word ---

var headingStyle = regexp.MustCompile(`(?i)^(heading|title|titre|berschrift|titel|encabezado|titolo)`)

func extractDocx(src Source, size int64, lim Limits) (*Result, error) {
	p, err := openPkg(src, size, lim)
	if err != nil {
		return nil, err
	}
	if p.files["word/document.xml"] == nil {
		return nil, errors.New("not a Word document: word/document.xml is missing")
	}
	c := newCollector(lim, "Body")
	p.coreProps(&c.res)
	if err := walkWord(p, "word/document.xml", c, true); err != nil {
		if len(c.res.Sections) == 0 && c.buf.Len() == 0 {
			return nil, err
		}
		c.res.Truncated = true
		setMeta(&c.res, "extraction_warning", err.Error())
	}
	for _, extra := range []struct{ part, label string }{
		{"word/footnotes.xml", "Footnotes"},
		{"word/endnotes.xml", "Endnotes"},
		{"word/comments.xml", "Comments"},
	} {
		c.section(extra.label)
		_ = walkWord(p, extra.part, c, false)
	}
	// Headers and footers often carry classification markings. Repeated
	// variants are common, so keep each distinct line once.
	c.section("Headers and footers")
	names := make([]string, 0, 4)
	for name := range p.files {
		if strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		sub := newCollector(lim, "")
		_ = walkWord(p, name, sub, false)
		for _, s := range sub.result().Sections {
			for line := range strings.SplitSeq(s.Text, "\n") {
				if line = strings.TrimSpace(line); line != "" && !seen[line] {
					seen[line] = true
					c.line(line)
				}
			}
		}
	}
	return c.result(), nil
}

// walkWord streams a WordprocessingML part. Tracked deletions use w:delText
// and so are not picked up: output matches the document as currently shown.
func walkWord(p *pkg, part string, c *collector, headings bool) error {
	d, closeFn, ok, err := p.decoder(part)
	if err != nil || !ok {
		return err
	}
	defer closeFn()

	var para strings.Builder
	var cells []string
	var style string
	var outline, inText bool
	tableDepth := 0
	for !c.full() {
		tok, err := d.Token()
		if err != nil {
			if stopErr(err) {
				return err
			}
			return nil
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
				style, outline = "", false
			case "pStyle":
				style = attr(t, "val")
			case "outlineLvl":
				outline = true
			case "t":
				inText = true
			case "tab":
				if inText || para.Len() > 0 {
					para.WriteByte('\t')
				}
			case "br", "cr":
				para.WriteByte('\n')
			case "tbl":
				tableDepth++
			case "tr":
				cells = cells[:0]
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				text := strings.TrimSpace(para.String())
				para.Reset()
				switch {
				case tableDepth > 0:
					if text != "" {
						cells = append(cells, strings.ReplaceAll(text, "\n", " "))
					}
				case text == "":
				case headings && (outline || headingStyle.MatchString(style)):
					if c.res.Title == "" && strings.EqualFold(style, "title") {
						c.res.Title = clean(text)
					}
					c.section(firstLine(text))
					c.para(text)
				default:
					c.para(text)
				}
			case "tr":
				if len(cells) > 0 {
					c.line(strings.Join(cells, " | "))
				}
				cells = cells[:0]
			case "tbl":
				tableDepth--
				if tableDepth == 0 {
					c.line("")
				}
			}
		}
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return truncateUTF8(strings.TrimSpace(clean(s)), 200)
}

// --- PowerPoint ---

const notesRelSuffix = "/notesSlide"

func extractPptx(src Source, size int64, lim Limits) (*Result, error) {
	p, err := openPkg(src, size, lim)
	if err != nil {
		return nil, err
	}
	if p.files["ppt/presentation.xml"] == nil {
		return nil, errors.New("not a PowerPoint file: ppt/presentation.xml is missing")
	}
	c := newCollector(lim, "")
	p.coreProps(&c.res)
	slides := p.slideOrder()
	setMeta(&c.res, "slides", strconv.Itoa(len(slides)))
	for i, slide := range slides {
		if c.full() {
			break
		}
		title, paras, err := p.shapeText(slide)
		if err != nil {
			setMeta(&c.res, "extraction_warning", fmt.Sprintf("slide %d: %v", i+1, err))
		}
		label := fmt.Sprintf("Slide %d", i+1)
		if title != "" {
			label += ": " + firstLine(title)
		}
		c.section(label)
		for _, t := range paras {
			c.para(t)
		}
		for _, rel := range p.rels(slide) {
			if strings.HasSuffix(rel.Type, notesRelSuffix) {
				_, notes, _ := p.shapeText(rel.Target)
				if len(notes) > 0 {
					c.para("Speaker notes:")
					for _, t := range notes {
						c.para(t)
					}
				}
			}
		}
	}
	return c.result(), nil
}

// slideOrder returns slide parts in presentation order. Part numbers do not
// track order once slides have been moved, so read the sldIdLst.
func (p *pkg) slideOrder() []string {
	byID := map[string]string{}
	for _, r := range p.rels("ppt/presentation.xml") {
		byID[r.ID] = r.Target
	}
	var pres struct {
		IDs []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	var out []string
	if err := p.parseInto("ppt/presentation.xml", &pres); err == nil {
		for _, id := range pres.IDs {
			if t := byID[id.RID]; t != "" && p.files[t] != nil {
				out = append(out, t)
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	// Damaged presentation.xml: fall back to numeric part order.
	num := regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)
	for name := range p.files {
		if num.MatchString(name) {
			out = append(out, name)
		}
	}
	n := func(s string) int { v, _ := strconv.Atoi(num.FindStringSubmatch(s)[1]); return v }
	slices.SortFunc(out, func(a, b string) int { return n(a) - n(b) })
	return out
}

// shapeText returns the title placeholder and paragraphs of a slide or notes
// part, skipping auto-generated placeholders such as slide numbers.
func (p *pkg) shapeText(part string) (title string, paras []string, err error) {
	d, closeFn, ok, err := p.decoder(part)
	if err != nil || !ok {
		return "", nil, err
	}
	defer closeFn()
	var para strings.Builder
	var cells []string
	var ph string
	var inText bool
	tableDepth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			if stopErr(err) {
				return title, paras, err
			}
			return title, paras, nil
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sp":
				ph = ""
			case "ph":
				ph = attr(t, "type")
			case "p":
				para.Reset()
			case "t":
				inText = true
			case "br":
				para.WriteByte('\n')
			case "tbl":
				tableDepth++
			case "tr":
				cells = cells[:0]
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				text := strings.TrimSpace(para.String())
				para.Reset()
				switch {
				case text == "" || ph == "sldNum" || ph == "dt" || ph == "ftr" || ph == "hdr":
				case tableDepth > 0:
					cells = append(cells, strings.ReplaceAll(text, "\n", " "))
				default:
					if (ph == "title" || ph == "ctrTitle") && title == "" {
						title = text
					}
					paras = append(paras, text)
				}
			case "tr":
				if len(cells) > 0 {
					paras = append(paras, strings.Join(cells, " | "))
				}
				cells = cells[:0]
			case "tbl":
				tableDepth--
			}
		}
	}
}
