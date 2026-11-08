package extract

import (
	"math"
	"strings"

	"github.com/ledongthuc/pdf"
)

// This file walks PDF content streams to recover text with its position. The
// library's own extraction was not usable on real files:
//
//   - GetPlainText drops word breaks, because PDFs rarely store spaces: a
//     generator just moves the next glyph along. Whole lines come out as one
//     unsearchable token.
//   - Content has positions, but costs about 0.5s per page, as it resolves
//     font widths through indirect objects for every glyph. It also reads
//     two-byte fonts one byte at a time, which gives wrong widths for the CID
//     fonts in PDFs exported from Office.
//   - Neither descends into Form XObjects, so that text is lost.
//
// Only pdf.Interpret, the library's tokenizer, is reused here.

// Thresholds as fractions of the font size. A word gap in ordinary text is
// around 0.25 em, and kerning is far below 0.1 em.
const (
	wordGap  = 0.15
	lineDrop = 0.5
	// colJump is how far left text must move on one baseline to count as a
	// new line, as when a table or second column restarts.
	colJump = 2.0
	// estCharWidth stands in for a glyph width when a font has no width table.
	estCharWidth = 500.0
)

// Limits against hostile files. Forms may reference each other, so without a
// budget a small file could expand exponentially.
const (
	maxFormDepth = 8
	maxFormCalls = 2000
	maxStateNest = 512
)

type mat [3][3]float64

var identity = mat{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func (x mat) mul(y mat) mat {
	var z mat
	for i := range 3 {
		for j := range 3 {
			for k := range 3 {
				z[i][j] += x[i][k] * y[k][j]
			}
		}
	}
	return z
}

func translate(tx, ty float64) mat { return mat{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}} }

// pdfFont caches what text extraction needs from a font.
type pdfFont struct {
	enc      pdf.TextEncoding
	twoByte  bool
	first    int
	widths   []float64       // simple fonts, indexed by code - first
	cid      map[int]float64 // composite fonts
	fallback float64         // width when the code is not in the table
}

func loadFont(v pdf.Value) *pdfFont {
	font := pdf.Font{V: v}
	f := &pdfFont{enc: font.Encoder(), fallback: estCharWidth}
	if v.Key("Subtype").Name() == "Type0" {
		f.twoByte = true
		f.cid = map[int]float64{}
		d := v.Key("DescendantFonts").Index(0)
		if dw := d.Key("DW"); !dw.IsNull() {
			f.fallback = dw.Float64()
		} else {
			f.fallback = 1000
		}
		parseCIDWidths(d.Key("W"), f.cid)
		return f
	}
	f.first = int(v.Key("FirstChar").Int64())
	w := v.Key("Widths")
	f.widths = make([]float64, w.Len())
	for i := range f.widths {
		f.widths[i] = w.Index(i).Float64()
	}
	return f
}

// parseCIDWidths reads the W array, which mixes two forms:
// "c [w1 w2 ...]" for consecutive codes, and "first last w" for a range.
func parseCIDWidths(w pdf.Value, out map[int]float64) {
	const maxRange = 1 << 16
	for i := 0; i+1 < w.Len(); {
		c := int(w.Index(i).Int64())
		next := w.Index(i + 1)
		if next.Kind() == pdf.Array {
			for j := range next.Len() {
				out[c+j] = next.Index(j).Float64()
			}
			i += 2
			continue
		}
		if i+2 >= w.Len() {
			return
		}
		last, width := int(next.Int64()), w.Index(i+2).Float64()
		for code := c; code <= last && code-c < maxRange; code++ {
			out[code] = width
		}
		i += 3
	}
}

// advance returns the total width of raw in thousandths of an em, and the
// number of glyphs and of single-byte spaces, which word spacing applies to.
func (f *pdfFont) advance(raw string) (width float64, glyphs, spaces int) {
	if f.twoByte {
		for i := 0; i+1 < len(raw); i += 2 {
			w, ok := f.cid[int(raw[i])<<8|int(raw[i+1])]
			if !ok {
				w = f.fallback
			}
			width += w
			glyphs++
		}
		return width, glyphs, 0
	}
	for i := 0; i < len(raw); i++ {
		code := int(raw[i]) - f.first
		if code >= 0 && code < len(f.widths) {
			width += f.widths[code]
		} else {
			width += f.fallback
		}
		if raw[i] == ' ' {
			spaces++
		}
	}
	return width, len(raw), spaces
}

// textState is the part of the graphics state that positions text.
type textState struct {
	ctm                   mat
	tm, tlm               mat
	tc, tw, th, tl, trise float64
	size                  float64
	font                  *pdfFont
}

// run is one string drawn on the page, in page coordinates.
type run struct {
	X, Y, EndX, Size float64
	Text             string
}

type pageWalker struct {
	fonts     map[string]*pdfFont
	runs      []run
	formCalls int
}

// walk interprets one content stream. Operators with bad arguments are
// skipped: real-world files are often slightly broken, and the text in the
// rest of the stream is still good.
func (w *pageWalker) walk(strm, resources pdf.Value, ctm mat, depth int) {
	g := textState{ctm: ctm, tm: identity, tlm: identity, th: 1}
	var stack []textState

	show := func(raw string) {
		if g.font == nil || raw == "" {
			return
		}
		text := raw
		if g.font.enc != nil {
			text = g.font.enc.Decode(raw)
		}
		scale := mat{{g.size * g.th, 0, 0}, {0, g.size, 0}, {0, g.trise, 1}}
		start := scale.mul(g.tm).mul(g.ctm)
		width, glyphs, spaces := g.font.advance(raw)
		tx := (width/1000*g.size + float64(glyphs)*g.tc + float64(spaces)*g.tw) * g.th
		g.tm = translate(tx, 0).mul(g.tm)
		end := scale.mul(g.tm).mul(g.ctm)
		w.runs = append(w.runs, run{
			X: start[2][0], Y: start[2][1], EndX: end[2][0],
			Size: math.Hypot(start[0][0], start[0][1]), Text: text,
		})
	}
	newline := func() {
		g.tlm = translate(0, -g.tl).mul(g.tlm)
		g.tm = g.tlm
	}

	pdf.Interpret(strm, func(stk *pdf.Stack, op string) {
		args := make([]pdf.Value, stk.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		n := len(args)
		num := func(i int) float64 { return args[i].Float64() }
		matrixArg := func() mat {
			return mat{{num(0), num(1), 0}, {num(2), num(3), 0}, {num(4), num(5), 1}}
		}
		switch {
		case op == "q" && len(stack) < maxStateNest:
			stack = append(stack, g)
		case op == "Q" && len(stack) > 0:
			g = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case op == "cm" && n == 6:
			g.ctm = matrixArg().mul(g.ctm)
		case op == "BT":
			g.tm, g.tlm = identity, identity
		case op == "Tm" && n == 6:
			g.tm = matrixArg()
			g.tlm = g.tm
		case (op == "Td" || op == "TD") && n == 2:
			if op == "TD" {
				g.tl = -num(1)
			}
			g.tlm = translate(num(0), num(1)).mul(g.tlm)
			g.tm = g.tlm
		case op == "T*":
			newline()
		case op == "Tc" && n == 1:
			g.tc = num(0)
		case op == "Tw" && n == 1:
			g.tw = num(0)
		case op == "Tz" && n == 1:
			g.th = num(0) / 100
		case op == "TL" && n == 1:
			g.tl = num(0)
		case op == "Ts" && n == 1:
			g.trise = num(0)
		case op == "Tf" && n == 2:
			name := args[0].Name()
			fv := resources.Key("Font").Key(name)
			key := fv.String() + "/" + name
			f := w.fonts[key]
			if f == nil {
				f = loadFont(fv)
				w.fonts[key] = f
			}
			g.font, g.size = f, num(1)
		case op == "Tj" && n == 1:
			show(args[0].RawString())
		case op == "'" && n == 1:
			newline()
			show(args[0].RawString())
		case op == "\"" && n == 3:
			g.tw, g.tc = num(0), num(1)
			newline()
			show(args[2].RawString())
		case op == "TJ" && n == 1:
			arr := args[0]
			for i := range arr.Len() {
				v := arr.Index(i)
				if v.Kind() == pdf.String {
					show(v.RawString())
				} else {
					g.tm = translate(-v.Float64()/1000*g.size*g.th, 0).mul(g.tm)
				}
			}
		case op == "Do" && n == 1:
			w.form(resources.Key("XObject").Key(args[0].Name()), resources, g.ctm, depth)
		}
	})
}

func (w *pageWalker) form(x, parent pdf.Value, ctm mat, depth int) {
	if x.Key("Subtype").Name() != "Form" || depth >= maxFormDepth || w.formCalls >= maxFormCalls {
		return
	}
	w.formCalls++
	if m := x.Key("Matrix"); m.Len() == 6 {
		v := func(i int) float64 { return m.Index(i).Float64() }
		ctm = mat{{v(0), v(1), 0}, {v(2), v(3), 0}, {v(4), v(5), 1}}.mul(ctm)
	}
	res := x.Key("Resources")
	if res.IsNull() {
		res = parent
	}
	w.walk(x, res, ctm, depth+1)
}

var ligatures = strings.NewReplacer("ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl", "ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "st", "ﬆ", "st")

// layoutRuns joins runs into text, turning gaps into spaces and baseline
// changes into newlines. Stream order is kept rather than sorting by position.
// Generators nearly always draw in reading order, so columns stay apart where
// a sort by row would interleave them.
func layoutRuns(runs []run) string {
	var b strings.Builder
	var last run
	for i, r := range runs {
		if r.Text == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			size := r.Size
			if size <= 0 {
				size = 10
			}
			gap := r.X - last.EndX
			switch {
			case math.Abs(r.Y-last.Y) > lineDrop*size || gap < -colJump*size:
				b.WriteByte('\n')
			case gap > wordGap*size && !strings.HasSuffix(last.Text, " ") && !strings.HasPrefix(r.Text, " "):
				b.WriteByte(' ')
			}
		}
		b.WriteString(r.Text)
		last = r
	}
	return ligatures.Replace(b.String())
}
