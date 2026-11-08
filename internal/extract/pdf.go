package extract

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractPDF(src Source, size int64, lim Limits) (*Result, error) {
	r, err := pdf.NewReader(src, size)
	if err != nil {
		if errors.Is(err, pdf.ErrInvalidPassword) || strings.Contains(strings.ToLower(err.Error()), "encrypt") {
			return nil, ErrEncrypted
		}
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	c := newCollector(lim, "")
	pdfInfo(r, &c.res)
	pages := r.NumPage()
	setMeta(&c.res, "pages", strconv.Itoa(pages))
	failed := 0
	for i := 1; i <= pages && !c.full(); i++ {
		text, err := pageText(r, i)
		if err != nil {
			failed++
			continue
		}
		c.section("Page " + strconv.Itoa(i))
		for line := range strings.SplitSeq(text, "\n") {
			c.line(line)
		}
	}
	if failed > 0 {
		setMeta(&c.res, "extraction_warning", fmt.Sprintf("%d of %d pages could not be read", failed, pages))
		if failed == pages {
			return nil, errors.New("no pages could be read")
		}
	}
	return c.result(), nil
}

// pageText isolates each page, because the parser panics on malformed content
// and one bad page should not cost the rest of the document. Text gathered
// before a panic is kept.
func pageText(r *pdf.Reader, n int) (text string, err error) {
	w := &pageWalker{fonts: map[string]*pdfFont{}}
	defer func() {
		if p := recover(); p != nil {
			text = layoutRuns(w.runs)
			if text == "" {
				err = fmt.Errorf("page %d: %v", n, p)
			}
		}
	}()
	page := r.Page(n)
	if page.V.IsNull() || page.V.Key("Contents").IsNull() {
		return "", nil
	}
	w.walk(page.V.Key("Contents"), page.Resources(), identity, 0)
	return layoutRuns(w.runs), nil
}

func pdfInfo(r *pdf.Reader, res *Result) {
	defer func() { _ = recover() }() // metadata is optional
	info := r.Trailer().Key("Info")
	if info.IsNull() {
		return
	}
	res.Title = strings.TrimSpace(clean(info.Key("Title").Text()))
	setMeta(res, "author", info.Key("Author").Text())
	setMeta(res, "subject", info.Key("Subject").Text())
	setMeta(res, "keywords", info.Key("Keywords").Text())
	setMeta(res, "created", info.Key("CreationDate").Text())
	setMeta(res, "modified", info.Key("ModDate").Text())
}
