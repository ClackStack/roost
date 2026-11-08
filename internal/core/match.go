package core

import (
	"strings"
	"unicode"
)

// buildMatch turns user text into FTS5 expressions. Input never reaches MATCH
// directly: FTS5 has its own query language, and raw text would produce syntax
// errors or column filters. Every term is emitted as a quoted string, with only
// phrases and trailing-* prefixes carried over.
//
// strict requires every term; loose accepts any. Both are empty for an unusable query.
func buildMatch(q string) (strict, loose string) {
	terms := parseTerms(q)
	if len(terms) == 0 {
		return "", ""
	}
	return strings.Join(terms, " "), strings.Join(terms, " OR ")
}

func parseTerms(q string) []string {
	var terms []string
	add := func(raw string, prefix bool) {
		// Keep letters and digits only; the tokenizer splits on the rest, so
		// a term like foo-bar becomes the phrase "foo bar".
		words := strings.FieldsFunc(raw, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
		})
		if len(words) == 0 {
			return
		}
		t := `"` + strings.Join(words, " ") + `"`
		if prefix {
			t += "*"
		}
		terms = append(terms, t)
	}
	for len(q) > 0 {
		q = strings.TrimLeftFunc(q, unicode.IsSpace)
		if q == "" {
			break
		}
		if q[0] == '"' {
			end := strings.IndexByte(q[1:], '"')
			if end < 0 {
				add(q[1:], false)
				break
			}
			add(q[1:1+end], false)
			q = q[end+2:]
			continue
		}
		i := strings.IndexFunc(q, unicode.IsSpace)
		if i < 0 {
			i = len(q)
		}
		word := q[:i]
		q = q[i:]
		add(word, strings.HasSuffix(word, "*"))
	}
	return terms
}
