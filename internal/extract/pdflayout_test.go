package extract

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// rawPDF assembles a PDF from object bodies, numbered from 1, computing the
// xref. Object 1 must be the catalog. Tests use it to produce constructs that
// no convenient generator will write on demand.
func rawPDF(objects ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func stream(dict, body string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(body), body)
}

// fontObj gives every printable ASCII code a width of 500, as a real font would.
func fontObj() string {
	widths := strings.TrimSpace(strings.Repeat("500 ", 95))
	return `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 126 /Encoding /WinAnsiEncoding /Widths [` + widths + `] >>`
}

func pageWith(content, xobjects string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> /XObject << ` + xobjects + ` >> >> >>`,
		stream("", content),
		fontObj(),
	}
	return rawPDF(append(objs, extra...)...)
}

func TestPDFWordBreaksWithoutSpaceCharacters(t *testing.T) {
	// This is how most generators write text: no space characters at all,
	// just a negative TJ adjustment or a new position between words.
	content := `BT /F1 10 Tf 72 700 Td [(Unified) -300 (system)] TJ ET
BT /F1 10 Tf 72 680 Td (keywords) Tj 60 0 Td (MUST) Tj ET`
	r, err := extractBytes(t, "gaps.pdf", pageWith(content, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sections) != 1 {
		t.Fatalf("sections: %+v", r.Sections)
	}
	for _, want := range []string{"Unified system", "keywords MUST"} {
		if !strings.Contains(r.Sections[0].Text, want) {
			t.Errorf("no %q in %q", want, r.Sections[0].Text)
		}
	}
}

func TestPDFKerningDoesNotSplitWords(t *testing.T) {
	r, err := extractBytes(t, "kern.pdf", pageWith(`BT /F1 10 Tf 72 700 Td [(A) 40 (V) -20 (ATAR)] TJ ET`, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Sections[0].Text; got != "AVATAR" {
		t.Fatalf("got %q", got)
	}
}

func TestPDFFormXObjectText(t *testing.T) {
	form := stream(`/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >>`,
		`BT /F1 10 Tf 72 600 Td (inside the form) Tj ET`)
	r, err := extractBytes(t, "form.pdf", pageWith(`/Fm1 Do BT /F1 10 Tf 72 700 Td (on the page) Tj ET`, "/Fm1 6 0 R", form))
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionWith(r, "inside the form"); got != "Page 1" {
		t.Fatalf("form text missing: %+v", r.Sections)
	}
}

func TestPDFRecursiveFormsTerminate(t *testing.T) {
	// A form that calls itself twice would run 2^depth times without a budget.
	self := stream(`/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /Font << /F1 5 0 R >> /XObject << /Me 6 0 R >> >>`,
		`BT /F1 10 Tf (x) Tj ET /Me Do /Me Do /Me Do /Me Do`)
	start := time.Now()
	r, err := extractBytes(t, "bomb.pdf", pageWith(`/Me Do`, "/Me 6 0 R", self))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %s", d)
	}
	if len(r.Sections) == 0 {
		t.Fatal("expected partial text")
	}
}

func TestPDFMalformedOperatorsAreSkipped(t *testing.T) {
	// Wrong operand counts and unbalanced Q must not lose the good text.
	r, err := extractBytes(t, "bad.pdf", pageWith(`Q Q 1 Tm 1 2 cm (x) (y) Tj BT /Nope 10 Tf (lost) Tj /F1 10 Tf 72 700 Td (still here) Tj ET`, ""))
	if err != nil {
		t.Fatal(err)
	}
	if sectionWith(r, "still here") == "" {
		t.Fatalf("sections: %+v", r.Sections)
	}
}
