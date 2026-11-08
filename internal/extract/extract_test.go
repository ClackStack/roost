package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// TestMain lets the test binary act as the worker, like the real binary does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == WorkerCommand {
		os.Exit(WorkerMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

var testLimits = Limits{MaxText: 8 << 20, MaxUnzip: 64 << 20}

func runner(t *testing.T) *Runner {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{Exe: exe, Limits: testLimits, Timeout: 30 * time.Second, MemoryMB: testWorkerMemoryMB}
}

func extractFile(t *testing.T, path string) (*Result, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return runner(t).Extract(context.Background(), f, strings.ToLower(filepath.Ext(path)))
}

func extractBytes(t *testing.T, name string, b []byte) (*Result, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return extractFile(t, p)
}

// sectionWith returns the label of the first section containing s.
func sectionWith(r *Result, s string) string {
	for _, sec := range r.Sections {
		if strings.Contains(sec.Text, s) {
			return sec.Label
		}
	}
	return ""
}

func TestRealFiles(t *testing.T) {
	cases := []struct {
		file    string
		needle  string
		section string
	}{
		{"sample.docx", "Café budgets are €1.2M", "Overview"},
		{"sample.docx", "heron milestone", "Risks"},
		{"sample.docx", "Acme | 4200", "Risks"},
		{"sample.pptx", "osprey market", "Slide 1"},
		{"sample.pptx", "kestrel layoffs", "Slide 1"},
		{"sample.pptx", "Harrier spending", "Slide 2"},
		{"sample.xlsx", "APAC | 3400", "Sheet: book"},
		{"sample.pdf", "Northwind closes", "Page 1"},
		{"sample.pdf", "€1.2M", "Page 1"},
		{"multipage.pdf", "merlin programme", "Page 1"},
		{"multipage.pdf", "goshawk contract", "Page 2"},
	}
	for _, tc := range cases {
		t.Run(tc.file+"/"+tc.needle, func(t *testing.T) {
			r, err := extractFile(t, filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if got := sectionWith(r, tc.needle); got != tc.section {
				t.Fatalf("section = %q, want %q; sections: %+v", got, tc.section, r.Sections)
			}
		})
	}
}

// TestLayoutRuns covers PDFs that store no space characters, where word breaks
// exist only as gaps between runs.
func TestLayoutRuns(t *testing.T) {
	r := func(s string, x, y float64) run {
		return run{X: x, Y: y, EndX: x + 5*float64(len([]rune(s))), Size: 10, Text: s}
	}
	cases := map[string]struct {
		in   []run
		want string
	}{
		"gap makes a word break": {[]run{r("Shared", 100, 700), r("MIME", 132.5, 700)}, "Shared MIME"},
		"kerning does not":       {[]run{r("AV", 100, 700), r("AT", 109.5, 700)}, "AVAT"},
		"new baseline":           {[]run{r("one", 100, 700), r("two", 100, 688)}, "one\ntwo"},
		"subscript stays inline": {[]run{r("H", 100, 700), r("2", 105, 698), r("O", 110, 700)}, "H2O"},
		"column jump":            {[]run{r("left", 300, 700), r("right", 100, 700)}, "left\nright"},
		"literal space":          {[]run{r("a ", 100, 700), r("b", 112.5, 700)}, "a b"},
		"no doubled space":       {[]run{r("a", 100, 700), r(" b", 110, 700)}, "a b"},
		"ligature":               {[]run{r("speciﬁc", 100, 700)}, "specific"},
		"zero font size":         {[]run{{X: 1, Y: 1, EndX: 2, Text: "a"}, {X: 2, Y: 1, EndX: 3, Text: "b"}}, "ab"},
		"empty":                  {nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := layoutRuns(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFontAdvance(t *testing.T) {
	simple := &pdfFont{first: 65, widths: []float64{600, 700}, fallback: estCharWidth}
	if w, g, s := simple.advance("AB "); w != 600+700+estCharWidth || g != 3 || s != 1 {
		t.Errorf("simple: %v %d %d", w, g, s)
	}
	// Two-byte codes must be read in pairs. Reading single bytes, as the
	// library does, would count four glyphs here.
	cid := &pdfFont{twoByte: true, cid: map[int]float64{0x0102: 250}, fallback: 1000}
	if w, g, _ := cid.advance("\x01\x02\x01\x03"); w != 1250 || g != 2 {
		t.Errorf("cid: %v %d", w, g)
	}
}

func TestPDFMetadata(t *testing.T) {
	r, err := extractFile(t, "testdata/multipage.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Merlin Report" || r.Meta["author"] != "Finance Team" || r.Meta["pages"] != "2" {
		t.Fatalf("title=%q meta=%v", r.Title, r.Meta)
	}
}

func TestScannedPDFHasNoSections(t *testing.T) {
	r, err := extractFile(t, "testdata/scanned.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sections) != 0 {
		t.Fatalf("sections = %+v", r.Sections)
	}
}

func TestEncrypted(t *testing.T) {
	if _, err := extractFile(t, "testdata/encrypted.pdf"); !errors.Is(err, ErrEncrypted) {
		t.Errorf("pdf: err = %v", err)
	}
	ole := append(append([]byte{}, oleMagic...), make([]byte, 512)...)
	for _, name := range []string{"x.docx", "x.xlsx", "x.pptx"} {
		if _, err := extractBytes(t, name, ole); !errors.Is(err, ErrEncrypted) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestXlsxHiddenSheetAndSheets(t *testing.T) {
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "public figure")
	f.NewSheet("Secret")
	f.SetCellValue("Secret", "A1", "parked salary data")
	if err := f.SetSheetVisible("Secret", false); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	r, err := extractBytes(t, "b.xlsx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionWith(r, "parked salary"); got != "Sheet: Secret (hidden)" {
		t.Fatalf("section = %q", got)
	}
}

func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	w.Close()
	return buf.Bytes()
}

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestDocxDetails(t *testing.T) {
	doc := `<w:document ` + wordNS + `><w:body>
	<w:p><w:r><w:t>kept text</w:t></w:r><w:del><w:r><w:delText>struck text</w:delText></w:r></w:del></w:p>
	</w:body></w:document>`
	notes := `<w:footnotes ` + wordNS + `><w:footnote><w:p><w:r><w:t>footnote osprey</w:t></w:r></w:p></w:footnote></w:footnotes>`
	comments := `<w:comments ` + wordNS + `><w:comment><w:p><w:r><w:t>reviewer comment</w:t></w:r></w:p></w:comment></w:comments>`
	header := `<w:hdr ` + wordNS + `><w:p><w:r><w:t>TOP SECRET</w:t></w:r></w:p></w:hdr>`
	r, err := extractBytes(t, "d.docx", zipOf(t, map[string]string{
		"word/document.xml": doc, "word/footnotes.xml": notes, "word/comments.xml": comments,
		"word/header1.xml": header, "word/header2.xml": header,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if sectionWith(r, "struck text") != "" {
		t.Error("tracked deletion leaked into text")
	}
	for needle, want := range map[string]string{
		"kept text": "Body", "footnote osprey": "Footnotes", "reviewer comment": "Comments", "TOP SECRET": "Headers and footers",
	} {
		if got := sectionWith(r, needle); got != want {
			t.Errorf("%q in %q, want %q", needle, got, want)
		}
	}
	for _, s := range r.Sections {
		if s.Label == "Headers and footers" && strings.Count(s.Text, "TOP SECRET") != 1 {
			t.Errorf("headers not deduplicated: %q", s.Text)
		}
	}
}

// TestPptxStructure uses the layout PowerPoint writes, with slides moved so
// that presentation order differs from part numbering.
func TestPptxStructure(t *testing.T) {
	const ns = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	shape := func(ph, text string) string {
		return `<p:sp><p:nvSpPr><p:nvPr><p:ph type="` + ph + `"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp>`
	}
	table := `<p:graphicFrame><a:graphic><a:graphicData><a:tbl><a:tr><a:tc><a:txBody><a:p><a:r><a:t>Q1</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>900</a:t></a:r></a:p></a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	r, err := extractBytes(t, "s.pptx", zipOf(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation ` + ns + `><p:sldIdLst><p:sldId r:id="rId2"/><p:sldId r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Type="x/slide" Target="slides/slide1.xml"/><Relationship Id="rId2" Type="x/slide" Target="/ppt/slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<p:sld ` + ns + `><p:cSld><p:spTree>` + shape("title", "Moved Last") + table + shape("sldNum", "99") + `</p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml":           `<p:sld ` + ns + `><p:cSld><p:spTree>` + shape("ctrTitle", "Moved First") + `</p:spTree></p:cSld></p:sld>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionWith(r, "Moved First"); got != "Slide 1: Moved First" {
		t.Errorf("first slide label = %q", got)
	}
	if got := sectionWith(r, "Q1 | 900"); got != "Slide 2: Moved Last" {
		t.Errorf("table in %q", got)
	}
	if sectionWith(r, "99") != "" {
		t.Error("slide number placeholder leaked into text")
	}
}

func TestTextFormats(t *testing.T) {
	email := "From: =?UTF-8?Q?Jos=C3=A9?= <j@x.test>\r\nTo: a@x.test\r\nSubject: Merger plan\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: multipart/alternative; boundary=c\r\n\r\n" +
		"--c\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nCaf=C3=A9 meeting\r\n" +
		"--c\r\nContent-Type: text/html\r\n\r\n<p>duplicate html copy</p>\r\n--c--\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=terms.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\nAAAA\r\n--b--\r\n"
	cases := []struct {
		name, body, needle, section, title string
	}{
		{"a.txt", "plain words\n", "plain words", "Body", ""},
		{"latin.txt", "caf\xe9 au lait\n", "café au lait", "Body", ""},
		{"wide.txt", "\xff\xfeh\x00i\x00", "hi", "Body", ""},
		{"a.md", "# Top\nintro\n## Sub\ndetail\n```\n# not heading\n```\n", "not heading", "Sub", "Top"},
		{"a.csv", "a,b\n1,\"x, y\"\n", "1 | x, y", "Body", ""},
		{"a.tsv", "a\tb\n", "a | b", "Body", ""},
		{"a.html", "<title>T</title><script>evil()</script><h1>Head</h1><p>para</p><table><tr><td>c1</td><td>c2</td></tr></table>", "c1 | c2", "Head", "T"},
		{"a.eml", email, "Café meeting", "Body", "Merger plan"},
		{"b.eml", email, "José", "Headers", "Merger plan"},
		{"c.eml", email, "terms.pdf", "Attachments", "Merger plan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := extractBytes(t, tc.name, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := sectionWith(r, tc.needle); got != tc.section {
				t.Fatalf("section = %q, want %q: %+v", got, tc.section, r.Sections)
			}
			if r.Title != tc.title {
				t.Errorf("title = %q, want %q", r.Title, tc.title)
			}
			if sectionWith(r, "evil()") != "" || sectionWith(r, "duplicate html") != "" {
				t.Errorf("unwanted text: %+v", r.Sections)
			}
		})
	}
}

func TestTextBudgetTruncates(t *testing.T) {
	big := strings.Repeat("falcon word list\n", 200_000)
	p := filepath.Join(t.TempDir(), "big.txt")
	os.WriteFile(p, []byte(big), 0o600)
	f, _ := os.Open(p)
	defer f.Close()
	r := runner(t)
	r.Limits.MaxText = 1 << 16
	res, err := r.Extract(context.Background(), f, ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Error("truncation not flagged")
	}
	total := 0
	for _, s := range res.Sections {
		total += len(s.Text)
	}
	if total > 1<<16 {
		t.Errorf("kept %d bytes", total)
	}
}

func TestZipBomb(t *testing.T) {
	// Highly compressible: small on disk, 16x the budget when inflated.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	f.Write([]byte(`<w:document ` + wordNS + `><w:body><w:p><w:r><w:t>`))
	block := bytes.Repeat([]byte("A"), 1<<20)
	for range 128 {
		f.Write(block)
	}
	w.Close()
	start := time.Now()
	r := runner(t)
	r.Limits = Limits{MaxText: 1 << 20, MaxUnzip: 8 << 20}
	p := filepath.Join(t.TempDir(), "bomb.docx")
	os.WriteFile(p, buf.Bytes(), 0o600)
	file, _ := os.Open(p)
	defer file.Close()
	_, err := r.Extract(context.Background(), file, ".docx")
	if err == nil {
		t.Log("bomb stopped with partial output")
	} else if !strings.Contains(err.Error(), "decompressed size limit") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %s", d)
	}
}

func TestMalformedInputFailsCleanly(t *testing.T) {
	docx, _ := os.ReadFile("testdata/sample.docx")
	pdf, _ := os.ReadFile("testdata/sample.pdf")
	deep := `<w:document ` + wordNS + `>` + strings.Repeat("<x>", 500_000)
	cases := map[string][]byte{
		"trunc.docx":  docx[:len(docx)/2],
		"junk.docx":   []byte("this is not a zip"),
		"empty.docx":  {},
		"nopart.docx": zipOf(t, map[string]string{"a.txt": "x"}),
		"deep.docx":   zipOf(t, map[string]string{"word/document.xml": deep}),
		"trunc.pdf":   pdf[:len(pdf)/2],
		"junk.pdf":    []byte("%PDF-1.7 nonsense"),
		"empty.pdf":   {},
		"junk.xlsx":   []byte("nope"),
		"junk.pptx":   []byte("nope"),
		"junk.eml":    {0, 1, 2},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			// Any outcome is acceptable except a hang or a dead worker.
			_, err := extractBytes(t, name, body)
			if err != nil && strings.Contains(err.Error(), "crashed") {
				t.Fatalf("worker crashed: %v", err)
			}
		})
	}
}

// fakeWorker builds a Runner around a shell script standing in for the child.
func fakeWorker(t *testing.T, script string) *Runner {
	t.Helper()
	p := filepath.Join(t.TempDir(), "worker.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &Runner{Exe: p, Limits: Limits{MaxText: 1 << 10, MaxUnzip: 1 << 20}, Timeout: 500 * time.Millisecond, MemoryMB: 64}
}

func tmpFile(t *testing.T) *os.File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(p, []byte("hi"), 0o600)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestRunnerContainsBadWorkers(t *testing.T) {
	cases := map[string]struct{ script, want string }{
		"hang":    {"exec sleep 30", "timed out"},
		"crash":   {"echo boom >&2; exit 3", "crashed"},
		"garbage": {"echo not-json", "bad extractor output"},
		"flood":   {"exec cat /dev/zero", "limit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := fakeWorker(t, tc.script)
			_, err := r.Extract(context.Background(), tmpFile(t), ".txt")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkerGetsNoEnvironment(t *testing.T) {
	t.Setenv("ROOST_TOKEN", "roost_secret")
	r := fakeWorker(t, `echo "{\"result\":{\"sections\":[{\"label\":\"env\",\"text\":\"tok=${ROOST_TOKEN}\"}]}}"`)
	res, err := r.Extract(context.Background(), tmpFile(t), ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if res.Sections[0].Text != "tok=" {
		t.Fatalf("worker saw token: %q", res.Sections[0].Text)
	}
}

func TestUnsupported(t *testing.T) {
	if _, err := extractBytes(t, "old.doc", []byte("x")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func FuzzExtract(f *testing.F) {
	for _, name := range []string{"sample.docx", "sample.pptx", "sample.xlsx", "sample.pdf"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b, strings.ToLower(filepath.Ext(name)))
	}
	f.Add(pageWith("BT /F1 10 Tf 72 700 Td [(a) -300 (b)] TJ ET /Fm Do", "/Fm 4 0 R"), ".pdf")
	f.Add([]byte("<html><td>x"), ".html")
	f.Add([]byte("From: a\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\n"), ".eml")
	exts := []string{".docx", ".pptx", ".xlsx", ".pdf", ".html", ".eml", ".md", ".csv", ".txt"}
	f.Fuzz(func(t *testing.T, b []byte, ext string) {
		if !Supported(ext) {
			ext = exts[len(b)%len(exts)]
		}
		// Extract recovers panics into errors. What remains to catch here is
		// fatal errors and hangs, which the watchdog turns into saved inputs.
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Extract(bytes.NewReader(b), int64(len(b)), ext, Limits{MaxText: 1 << 20, MaxUnzip: 16 << 20})
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("extraction hung: ext=%s len=%d", ext, len(b))
		}
	})
}
