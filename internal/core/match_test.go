package core

import "testing"

func TestBuildMatch(t *testing.T) {
	cases := []struct{ in, strict, loose string }{
		{"merger plan", `"merger" "plan"`, `"merger" OR "plan"`},
		{`"exact phrase" other`, `"exact phrase" "other"`, `"exact phrase" OR "other"`},
		{"prefix*", `"prefix"*`, `"prefix"*`},
		{"foo-bar", `"foo bar"`, `"foo bar"`},
		{`unterminated "quote here`, `"unterminated" "quote here"`, `"unterminated" OR "quote here"`},
		{"café 機密", `"café" "機密"`, `"café" OR "機密"`},
		// FTS5 syntax must arrive as plain terms, never as operators.
		{"title:secret", `"title secret"`, `"title secret"`},
		{"a AND NOT b", `"a" "AND" "NOT" "b"`, `"a" OR "AND" OR "NOT" OR "b"`},
		{`x" OR "y`, `"x" "OR" "y"`, `"x" OR "OR" OR "y"`},
		{"NEAR(a b)", `"NEAR a" "b"`, `"NEAR a" OR "b"`},
		{"", "", ""},
		{`!!! ""  ***`, "", ""},
	}
	for _, tc := range cases {
		strict, loose := buildMatch(tc.in)
		if strict != tc.strict || loose != tc.loose {
			t.Errorf("buildMatch(%q) = %q, %q; want %q, %q", tc.in, strict, loose, tc.strict, tc.loose)
		}
	}
}
