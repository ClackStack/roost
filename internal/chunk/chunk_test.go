package chunk

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/paulhovey/roost/internal/extract"
)

func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func TestSplitPreservesTextAndBounds(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"short":      "one line\n",
		"paragraphs": strings.Repeat("A sentence of ordinary length goes here.\n\n", 200),
		"one line":   strings.Repeat("word ", 5000),
		"no spaces":  strings.Repeat("x", 7000),
		"cjk":        strings.Repeat("機密文書の内容", 900),
		"emoji":      strings.Repeat("🦅", 3000),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			chunks := Split([]extract.Section{{Label: "S", Text: in}})
			var joined strings.Builder
			for _, c := range chunks {
				if len(c.Text) > Max {
					t.Errorf("chunk of %d bytes exceeds Max", len(c.Text))
				}
				if !utf8.ValidString(c.Text) {
					t.Error("chunk is not valid UTF-8")
				}
				joined.WriteString(c.Text)
			}
			if squash(joined.String()) != squash(in) {
				t.Error("chunks do not reproduce the input")
			}
		})
	}
}

func TestChunksStayInSection(t *testing.T) {
	chunks := Split([]extract.Section{{Label: "Page 1", Text: "a"}, {Label: "Page 2", Text: "b"}})
	if len(chunks) != 2 || chunks[0].Section != "Page 1" || chunks[1].Section != "Page 2" {
		t.Fatalf("got %+v", chunks)
	}
}
